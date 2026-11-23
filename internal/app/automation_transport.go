package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"browser/internal/engine"
	"browser/internal/protocol"
)

// automationRunner is the MCP surface shared by the in-process Automation and
// the protocol client.
type automationRunner interface {
	Navigate(context.Context, string) error
	Snapshot() string
	Screenshot(int) ([]byte, error)
	RunCommand(context.Context, string, json.RawMessage) (string, error)
}

// automationHandler owns the Automation engine on the session goroutine and
// answers MCP operations that arrive through the protocol.
type automationHandler struct {
	a *engine.Automation

	mu     sync.Mutex
	cancel context.CancelFunc
}

// AbortSession cancels the operation in flight.
func (h *automationHandler) AbortSession() {
	h.mu.Lock()
	if h.cancel != nil {
		h.cancel()
	}
	h.mu.Unlock()
}

// Serve implements protocol.Handler.
func (h *automationHandler) Serve(ctx context.Context, abort <-chan struct{}, in <-chan protocol.Command, out protocol.EventSink) error {
	defer h.a.Close()
	for {
		select {
		case <-ctx.Done():
			return nil
		case cmd, ok := <-in:
			if !ok {
				return nil
			}
			switch v := cmd.Body.(type) {
			case protocol.Automation:
				actx, cancel := context.WithCancel(ctx)
				h.mu.Lock()
				h.cancel = cancel
				h.mu.Unlock()
				res := h.run(actx, v)
				cancel()
				h.mu.Lock()
				h.cancel = nil
				h.mu.Unlock()
				if err := out.Send(protocol.Event{Body: res}); err != nil {
					return err
				}
			case protocol.Shutdown:
				return nil
			}
		}
	}
}

func (h *automationHandler) run(ctx context.Context, v protocol.Automation) (res protocol.AutomationResult) {
	res.Op = v.Op
	var err error
	switch v.Op {
	case protocol.AutomationNavigate:
		err = h.a.Navigate(ctx, v.URL)
		if err == nil {
			res.Text = h.a.Snapshot()
		}
	case protocol.AutomationSnapshot:
		res.Text = h.a.Snapshot()
	case protocol.AutomationScreenshot:
		res.Data, err = h.a.Screenshot(v.Scale)
	case protocol.AutomationRunCommand:
		res.Text, err = h.a.RunCommand(ctx, v.Name, json.RawMessage(v.Args))
	default:
		err = fmt.Errorf("unknown automation operation %d", v.Op)
	}
	if err != nil {
		res.Err = err.Error()
	}
	return res
}

// automationClient is the MCP front end: it speaks only the protocol.
type automationClient struct {
	s  protocol.Session
	mu sync.Mutex
}

func (c *automationClient) call(ctx context.Context, cmd protocol.Automation) (protocol.AutomationResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.s.Send(protocol.Command{Body: cmd}); err != nil {
		return protocol.AutomationResult{}, err
	}
	for {
		select {
		case <-ctx.Done():
			c.s.Abort()
			return protocol.AutomationResult{}, ctx.Err()
		case ev, ok := <-c.s.Events():
			if !ok {
				return protocol.AutomationResult{}, fmt.Errorf("automation session closed")
			}
			switch v := ev.Body.(type) {
			case protocol.AutomationResult:
				if v.Op != cmd.Op {
					continue
				}
				if v.Err != "" {
					return v, fmt.Errorf("%s", v.Err)
				}
				return v, nil
			case protocol.Crash:
				return protocol.AutomationResult{}, fmt.Errorf("%s", v.Message)
			}
		}
	}
}

func (c *automationClient) Navigate(ctx context.Context, target string) error {
	_, err := c.call(ctx, protocol.Automation{Op: protocol.AutomationNavigate, URL: target})
	return err
}

func (c *automationClient) Snapshot() string {
	res, err := c.call(context.Background(), protocol.Automation{Op: protocol.AutomationSnapshot})
	if err != nil {
		return "automation error: " + err.Error()
	}
	return res.Text
}

func (c *automationClient) Screenshot(scale int) ([]byte, error) {
	res, err := c.call(context.Background(), protocol.Automation{Op: protocol.AutomationScreenshot, Scale: scale})
	if err != nil {
		return nil, err
	}
	return res.Data, nil
}

func (c *automationClient) RunCommand(ctx context.Context, name string, args json.RawMessage) (string, error) {
	res, err := c.call(ctx, protocol.Automation{Op: protocol.AutomationRunCommand, Name: name, Args: args})
	if err != nil {
		return "", err
	}
	return res.Text, nil
}
