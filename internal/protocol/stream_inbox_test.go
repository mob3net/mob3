package protocol

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCommandInboxQueuesWithoutBlocking(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan Command, 1)
	inbox := newCommandInbox(ctx, out, 2)
	go inbox.run()

	if err := inbox.push(Command{Body: Key{Token: "a"}}); err != nil {
		t.Fatal(err)
	}
	if err := inbox.push(Command{Body: Key{Token: "b"}}); err != nil {
		t.Fatal(err)
	}
	if err := inbox.push(Command{Body: Key{Token: "c"}}); !errors.Is(err, ErrLimit) {
		t.Fatalf("overflow error = %v, want ErrLimit", err)
	}

	select {
	case cmd := <-out:
		if cmd.Body.(Key).Token != "a" {
			t.Fatalf("first command = %#v", cmd.Body)
		}
	case <-time.After(time.Second):
		t.Fatal("first command was not delivered")
	}
	cancel()
	select {
	case <-inbox.done:
	case <-time.After(time.Second):
		t.Fatal("inbox did not stop")
	}
}
