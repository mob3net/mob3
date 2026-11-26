package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"reflect"
	"testing"
)

func muxHeader(kind MuxKind, id uint64, length int) []byte {
	var h [muxHeaderSize]byte
	binary.LittleEndian.PutUint32(h[0:4], muxMagic)
	h[4] = Version
	h[5] = byte(kind)
	binary.LittleEndian.PutUint64(h[8:16], id)
	binary.LittleEndian.PutUint32(h[16:20], uint32(length))
	return h[:]
}

func readMuxFrame(t *testing.T, m *Mux) MuxFrame {
	t.Helper()
	f, err := m.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestMuxSeparateStreams(t *testing.T) {
	commandR, commandW := io.Pipe()
	eventR, eventW := io.Pipe()
	defer commandW.Close()
	defer eventW.Close()

	client := NewMux(commandR, eventW, DefaultLimits)
	server := NewMux(eventR, commandW, DefaultLimits)

	wantCommand := Command{Session: 3, Seq: 9, Body: Navigate{URL: "https://example.test/", Push: true}}
	writeErr := make(chan error, 1)
	go func() { writeErr <- client.WriteCommand(wantCommand) }()
	gotCommand, err := server.ReadCommand()
	if err != nil {
		t.Fatal(err)
	}
	if err := <-writeErr; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotCommand, wantCommand) {
		t.Fatalf("command = %#v, want %#v", gotCommand, wantCommand)
	}

	wantEvent := Event{Session: 4, Seq: 10, Body: State{URL: "https://example.test/", Title: "separate"}}
	go func() { writeErr <- server.WriteEvent(wantEvent) }()
	gotEvent, err := client.ReadEvent()
	if err != nil {
		t.Fatal(err)
	}
	if err := <-writeErr; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotEvent, wantEvent) {
		t.Fatalf("event = %#v, want %#v", gotEvent, wantEvent)
	}
}

func TestMuxCommandEventRoundTrip(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	serverMux := NewMuxReadWriter(server, DefaultLimits)
	clientMux := NewMuxReadWriter(client, DefaultLimits)

	commands := testCommands()
	writeErr := make(chan error, 1)
	go func() {
		for _, c := range commands {
			if err := clientMux.WriteCommand(c); err != nil {
				writeErr <- err
				return
			}
		}
		writeErr <- nil
	}()
	for _, want := range commands {
		got, err := serverMux.ReadCommand()
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%T command = %#v, want %#v", want.Body, got, want)
		}
	}
	if err := <-writeErr; err != nil {
		t.Fatal(err)
	}

	events := testEvents()
	go func() {
		for _, ev := range events {
			if err := serverMux.WriteEvent(ev); err != nil {
				writeErr <- err
				return
			}
		}
		writeErr <- nil
	}()
	for _, want := range events {
		got, err := clientMux.ReadEvent()
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%T event = %#v, want %#v", want.Body, got, want)
		}
	}
	if err := <-writeErr; err != nil {
		t.Fatal(err)
	}
}

func TestMuxFrameKindAndID(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	serverMux := NewMuxReadWriter(server, DefaultLimits)
	clientMux := NewMuxReadWriter(client, DefaultLimits)

	go func() { _ = serverMux.WriteFrame(MuxPing, 42, []byte("ping")) }()
	got := readMuxFrame(t, clientMux)
	if got.Kind != MuxPing || got.ID != 42 || !bytes.Equal(got.Body, []byte("ping")) {
		t.Fatalf("frame = %#v", got)
	}
}

func TestMuxRejectsWrongFrameKind(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	serverMux := NewMuxReadWriter(server, DefaultLimits)
	clientMux := NewMuxReadWriter(client, DefaultLimits)

	go func() { _ = serverMux.WriteEvent(Event{Body: State{}}) }()
	if _, err := clientMux.ReadCommand(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("ReadCommand error = %v, want ErrInvalid", err)
	}
}

func TestMuxRejectsBadHeaders(t *testing.T) {
	good := muxHeader(MuxCommand, 0, 0)
	tests := []struct {
		name string
		hdr  []byte
		want error
	}{
		{name: "magic", hdr: func() []byte { h := append([]byte(nil), good...); h[0] = 0; return h }(), want: ErrInvalid},
		{name: "version", hdr: func() []byte { h := append([]byte(nil), good...); h[4]++; return h }(), want: ErrInvalid},
		{name: "kind", hdr: func() []byte { h := append([]byte(nil), good...); h[5] = 0; return h }(), want: ErrInvalid},
		{name: "flags", hdr: func() []byte { h := append([]byte(nil), good...); h[6] = 1; return h }(), want: ErrInvalid},
		{name: "reserved", hdr: func() []byte { h := append([]byte(nil), good...); h[7] = 1; return h }(), want: ErrInvalid},
		{name: "oversize", hdr: muxHeader(MuxCommand, 0, DefaultLimits.MaxMessageBytes+1), want: ErrLimit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMux(bytes.NewReader(tt.hdr), io.Discard, DefaultLimits)
			if _, err := m.ReadFrame(); !errors.Is(err, tt.want) {
				t.Fatalf("ReadFrame error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestMuxWriteLimits(t *testing.T) {
	m := NewMux(bytes.NewReader(nil), io.Discard, DefaultLimits)
	if err := m.WriteFrame(MuxCommand, 0, make([]byte, DefaultLimits.MaxMessageBytes+1)); !errors.Is(err, ErrLimit) {
		t.Fatalf("WriteFrame error = %v, want ErrLimit", err)
	}
	if err := m.WriteFrame(MuxKind(0), 0, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("WriteFrame kind error = %v, want ErrInvalid", err)
	}
}

func FuzzMuxReadFrame(f *testing.F) {
	f.Add(muxHeader(MuxCommand, 0, 0))
	f.Add(append(muxHeader(MuxEvent, 7, 3), 1, 2, 3))
	f.Fuzz(func(t *testing.T, data []byte) {
		m := NewMux(bytes.NewReader(data), io.Discard, DefaultLimits)
		_, _ = m.ReadFrame()
	})
}
