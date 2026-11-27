package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"browser/internal/engine"
	"browser/internal/host"
	"browser/internal/protocol"
)

var errWorkerNotReady = errors.New("process worker did not become ready")

func waitWorkerReady(session protocol.Session, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case ev, ok := <-session.Events():
			if !ok {
				return errWorkerNotReady
			}
			switch body := ev.Body.(type) {
			case protocol.Ready:
				return nil
			case protocol.Crash:
				return errors.New(body.Message)
			case protocol.Quit:
				return errWorkerNotReady
			}
		case <-timer.C:
			return errWorkerNotReady
		}
	}
}

const processCloseGrace = time.Second

type processPipes struct {
	r    io.ReadCloser
	w    io.WriteCloser
	once sync.Once
}

func (p *processPipes) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p *processPipes) Write(b []byte) (int, error) { return p.w.Write(b) }
func (p *processPipes) Close() error {
	var err error
	p.once.Do(func() {
		if p.r != nil {
			err = p.r.Close()
		}
		if p.w != nil {
			if werr := p.w.Close(); err == nil {
				err = werr
			}
		}
	})
	return err
}

// processEngine owns one engine child process and its protocol session. The
// interactive tab backend and the MCP automation runner share it.
type processEngine struct {
	session protocol.Session
	cmd     *exec.Cmd
	pipes   *processPipes
	stderr  *boundedStderr
	done    chan struct{}
	once    sync.Once

	userClosed atomic.Bool
	errMu      sync.Mutex
	exitErr    error
}

func (b *processEngine) Session() protocol.Session { return b.session }

func (b *processEngine) Close() {
	b.userClosed.Store(true)
	b.once.Do(func() {
		_ = b.session.Send(protocol.Command{Body: protocol.Shutdown{}})
		if flusher, ok := b.session.(protocol.Flusher); ok {
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			_ = flusher.Flush(ctx)
			cancel()
		}
		_ = b.pipes.w.Close()
		select {
		case <-b.done:
		case <-time.After(processCloseGrace):
			b.killProcess()
		}
		_ = b.pipes.Close()
	})
}

func (b *processEngine) Kill() {
	b.userClosed.Store(true)
	b.once.Do(func() {
		b.killProcess()
		_ = b.pipes.Close()
	})
}

// Done is closed after the child has exited and been reaped.
func (b *processEngine) Done() <-chan struct{} { return b.done }

// ExitError reports the child's wait error and the sanitized stderr tail, if
// any, without exposing raw terminal bytes to the front end.
func (b *processEngine) ExitError() error {
	b.errMu.Lock()
	waitErr := b.exitErr
	b.errMu.Unlock()
	tail := ""
	if b.stderr != nil {
		tail = b.stderr.Text()
	}
	if tail != "" {
		if waitErr != nil {
			return fmt.Errorf("%w: %s", waitErr, tail)
		}
		return errors.New(tail)
	}
	return waitErr
}

// UserClosed reports whether Close or Kill initiated the exit.
func (b *processEngine) UserClosed() bool { return b.userClosed.Load() }

func (b *processEngine) setExitError(err error) {
	b.errMu.Lock()
	b.exitErr = err
	b.errMu.Unlock()
}

func (b *processEngine) killProcess() {
	if b.cmd != nil && b.cmd.Process != nil {
		_ = b.cmd.Process.Kill()
	}
}

// processTabBackend adapts one engine child to the interactive tab contract.
type processTabBackend struct {
	*processEngine
}

// executablePath returns the binary the worker should re-exec.
func executablePath() string {
	self, err := os.Executable()
	if err != nil || self == "" {
		return os.Args[0]
	}
	return self
}

// spawnProcessEngine starts one engine child, wires the shared host and storage
// RPC peers, and waits for its ready handshake. The caller owns the returned
// handle and must Close or Kill it.
func spawnProcessEngine(hostSess *host.Session, storage protocol.StorageProvider, execPath string, args, extraEnv []string, cfg processWorkerConfig) (*processEngine, error) {
	if cfg.Kind == "" {
		cfg.Kind = workerKindTab
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(execPath, args...)
	env := workerEnv(os.Environ())
	env = append(env, extraEnv...)
	env = append(env,
		processWorkerEnv+"=1",
		processConfigEnv+"="+string(raw),
	)
	cmd.Env = env
	stderr := newBoundedStderr()
	cmd.Stderr = stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, err
	}
	pipes := &processPipes{r: stdout, w: stdin}
	eng := &processEngine{cmd: cmd, pipes: pipes, stderr: stderr, done: make(chan struct{})}
	peer := protocol.NewMuxPeerReadWriter(pipes, protocol.DefaultLimits)
	_ = protocol.NewHostServerPeer(peer, hostSess)
	_ = protocol.NewStorageServerPeer(peer, storage)
	eng.session = protocol.DialPeer(context.Background(), peer, protocol.SessionID(cfg.SessionID), protocol.DefaultLimits, processWorkerQueue)
	go func() {
		err := cmd.Wait()
		eng.setExitError(err)
		_ = pipes.Close()
		close(eng.done)
	}()
	if err := waitWorkerReady(eng.session, 5*time.Second); err != nil {
		eng.Kill()
		select {
		case <-eng.done:
		case <-time.After(processCloseGrace):
		}
		if tail := stderr.Text(); tail != "" {
			return nil, fmt.Errorf("%w: %s", err, tail)
		}
		return nil, err
	}
	return eng, nil
}

// newProcessBackendFactory returns a backend factory that starts a dedicated
// engine process per tab. The child receives the same CLI flags as the parent and
// a small JSON geometry/config blob.
func newProcessBackendFactory(hostSess *host.Session, storage protocol.StorageProvider, args []string, caps protocol.Capabilities, zoom float64, graphics int) tabBackendFactory {
	return startProcessBackend(hostSess, storage, executablePath(), args, nil, caps, zoom, graphics)
}

// startProcessBackend is the spawn seam used by tests. execPath and extraEnv
// let a test run the worker process from the test binary itself.
func startProcessBackend(hostSess *host.Session, storage protocol.StorageProvider, execPath string, args, extraEnv []string, caps protocol.Capabilities, zoom float64, graphics int) tabBackendFactory {
	return func(_ *engine.Loader, id protocol.SessionID, init protocol.Init, target string) (tabBackend, error) {
		cfg := processWorkerConfig{
			Kind:  workerKindTab,
			Width: init.Width, Height: init.Height,
			CellW: init.CellW, CellH: init.CellH, Zoom: zoom,
			Graphics: graphics, TabID: uint64(id), SessionID: uint32(id),
		}
		eng, err := spawnProcessEngine(hostSess, storage, execPath, args, extraEnv, cfg)
		if err != nil {
			return nil, err
		}
		init.Caps = caps
		if err := eng.session.Send(protocol.Command{Body: init}); err != nil {
			eng.Kill()
			return nil, err
		}
		if err := eng.session.Send(protocol.Command{Body: protocol.Navigate{URL: target, Push: true}}); err != nil {
			eng.Kill()
			return nil, err
		}
		return &processTabBackend{processEngine: eng}, nil
	}
}

// workerEnv removes an inherited worker marker so a child cannot see itself as
// the parent's engine.
func workerEnv(env []string) []string {
	out := env[:0]
	for _, item := range env {
		if strings.HasPrefix(item, processWorkerEnv+"=") || strings.HasPrefix(item, processConfigEnv+"=") {
			continue
		}
		out = append(out, item)
	}
	return out
}
