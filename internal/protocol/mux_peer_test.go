package protocol

import (
	"errors"
	"net"
	"testing"
	"time"
)

func TestMuxPeerWriteIsBounded(t *testing.T) {
	blocked, writer := net.Pipe()
	defer blocked.Close()
	defer writer.Close()
	peer := NewMuxPeer(nil, writer, DefaultLimits)
	defer peer.Close()

	// The pipe has no reader, so the writer goroutine blocks on the first
	// frame. The queue must still return ErrSessionBusy instead of blocking the
	// caller.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 256; i++ {
			if err := peer.WriteFrame(MuxPing, uint64(i), nil); err != nil {
				if !errors.Is(err, ErrSessionBusy) {
					t.Errorf("write error = %v, want ErrSessionBusy", err)
				}
				close(done)
				return
			}
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("WriteFrame blocked on a stalled peer")
	}
}
