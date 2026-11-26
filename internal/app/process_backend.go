package app

import (
	"context"
	"encoding/json"
	"errors"
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

type processTabBackend struct {
	session protocol.Session
	cmd     *exec.Cmd
	pipes   *processPipes
	done    chan struct{}
	once    sync.Once

	userClosed atomic.Bool
	errMu      sync.Mutex
	exitErr    error
}

func (b *processTabBackend) Session() protocol.Session { return b.session }

func (b *processTabBackend) Close() {
	b.userClosed.Store(true)
	b.once.Do(func() {
		_ = b.session.Send(protocol.Command{Body: protocol.Shutdown{}})
		_ = b.pipes.w.Close()
		select {
		case <-b.done:
		case <-time.After(processCloseGrace):
			b.killProcess()
		}
		_ = b.pipes.Close()
	})
}

func (b *processTabBackend) Kill() {
	b.userClosed.Store(true)
	b.once.Do(func() {
		b.killProcess()
		_ = b.pipes.Close()
	})
}

// Done is closed after the child has exited and been reaped.
func (b *processTabBackend) Done() <-chan struct{} { return b.done }

// ExitError reports the child's wait error, if any.
func (b *processTabBackend) ExitError() error {
	b.errMu.Lock()
	defer b.errMu.Unlock()
	return b.exitErr
}

// UserClosed reports whether Close or Kill initiated the exit.
func (b *processTabBackend) UserClosed() bool { return b.userClosed.Load() }

func (b *processTabBackend) setExitError(err error) {
	b.errMu.Lock()
	b.exitErr = err
	b.errMu.Unlock()
}

func (b *processTabBackend) killProcess() {
	if b.cmd != nil && b.cmd.Process != nil {
		_ = b.cmd.Process.Kill()
	}
}

// newProcessBackendFactory returns a backend factory that starts one hidden
// engine child per tab. The child receives the same CLI flags as the parent and
// a small JSON geometry/config blob.
func newProcessBackendFactory(hostSess *host.Session, storage protocol.StorageProvider, args []string, caps protocol.Capabilities, zoom float64, graphics int) tabBackendFactory {
	self, err := os.Executable()
	if err != nil || self == "" {
		self = os.Args[0]
	}
	return startProcessBackend(hostSess, storage, self, args, nil, caps, zoom, graphics)
}

// startProcessBackend is the spawn seam used by tests. execPath and extraEnv
// let a test run the hidden worker from the test binary itself.
func startProcessBackend(hostSess *host.Session, storage protocol.StorageProvider, execPath string, args, extraEnv []string, caps protocol.Capabilities, zoom float64, graphics int) tabBackendFactory {
	return func(_ *engine.Loader, id protocol.SessionID, init protocol.Init, target string) (tabBackend, error) {
		cfg := processWorkerConfig{
			Width: init.Width, Height: init.Height,
			CellW: init.CellW, CellH: init.CellH, Zoom: zoom,
			Graphics: graphics, TabID: uint64(id), SessionID: uint32(id),
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
		cmd.Stderr = os.Stderr
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
		backend := &processTabBackend{cmd: cmd, pipes: pipes, done: make(chan struct{})}
		peer := protocol.NewMuxPeerReadWriter(pipes, protocol.DefaultLimits)
		_ = protocol.NewHostServerPeer(peer, hostSess)
		_ = protocol.NewStorageServerPeer(peer, storage)
		backend.session = protocol.DialPeer(context.Background(), peer, id, protocol.DefaultLimits, processWorkerQueue)
		go func() {
			err := cmd.Wait()
			backend.setExitError(err)
			_ = pipes.Close()
			close(backend.done)
		}()
		if err := waitWorkerReady(backend.session, 5*time.Second); err != nil {
			backend.Kill()
			return nil, err
		}
		init.Caps = caps
		if err := backend.session.Send(protocol.Command{Body: init}); err != nil {
			backend.Kill()
			return nil, err
		}
		if err := backend.session.Send(protocol.Command{Body: protocol.Navigate{URL: target, Push: true}}); err != nil {
			backend.Kill()
			return nil, err
		}
		return backend, nil
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
