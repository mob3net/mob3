package frontend

import (
	"sync"
	"testing"
	"time"

	"browser/internal/protocol"
)

type probeTestSession struct {
	events chan protocol.Event
	sent   chan protocol.Command
	once   sync.Once
}

func newProbeTestSession() *probeTestSession {
	return &probeTestSession{
		events: make(chan protocol.Event, 8),
		sent:   make(chan protocol.Command, 8),
	}
}

func (s *probeTestSession) Send(c protocol.Command) error {
	s.sent <- c
	return nil
}
func (s *probeTestSession) Events() <-chan protocol.Event { return s.events }
func (s *probeTestSession) Abort()                        {}
func (s *probeTestSession) Close() error {
	s.once.Do(func() { close(s.events) })
	return nil
}

func TestProbeSessionPongKeepsTabAlive(t *testing.T) {
	inner := newProbeTestSession()
	stuck := make(chan struct{}, 1)
	probe := NewProbeSession(inner, 30*time.Millisecond, 50*time.Millisecond, func() { close(stuck) })
	defer probe.Close()

	select {
	case cmd := <-inner.sent:
		ping, ok := cmd.Body.(protocol.Ping)
		if !ok {
			t.Fatalf("probe command = %T, want Ping", cmd.Body)
		}
		inner.events <- protocol.Event{Body: protocol.Pong{ID: ping.ID}}
	case <-time.After(time.Second):
		t.Fatal("probe did not send a Ping")
	}
	select {
	case <-stuck:
		t.Fatal("healthy session was marked stuck")
	case <-time.After(120 * time.Millisecond):
	}
}

func TestProbeSessionMissingPongDiscards(t *testing.T) {
	inner := newProbeTestSession()
	stuck := make(chan struct{}, 1)
	probe := NewProbeSession(inner, 20*time.Millisecond, 20*time.Millisecond, func() { close(stuck) })
	defer probe.Close()

	select {
	case <-stuck:
	case <-time.After(time.Second):
		t.Fatal("wedged session was not marked stuck")
	}
}

func TestProbeSessionEventDisarms(t *testing.T) {
	inner := newProbeTestSession()
	stuck := make(chan struct{}, 1)
	probe := NewProbeSession(inner, 30*time.Millisecond, 30*time.Millisecond, func() { close(stuck) })
	defer probe.Close()

	inner.events <- protocol.Event{Body: protocol.State{Title: "ready"}}
	select {
	case cmd := <-inner.sent:
		t.Fatalf("probe sent %T after activity", cmd.Body)
	case <-time.After(80 * time.Millisecond):
	}
	select {
	case <-stuck:
		t.Fatal("active session was marked stuck")
	default:
	}
}
