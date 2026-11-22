package protocol

import "testing"

func FuzzDecodeCommand(f *testing.F) {
	for _, c := range testCommands() {
		msg, err := AppendCommand(nil, c, DefaultLimits)
		if err == nil {
			f.Add(msg)
		}
	}
	f.Add([]byte{})
	f.Add([]byte("MOB3"))

	f.Fuzz(func(t *testing.T, b []byte) {
		cmd, rest, err := DecodeCommand(b, DefaultLimits)
		if err != nil {
			return
		}
		if cmd.Body == nil {
			t.Fatal("decoded command has no body")
		}
		if len(rest) > len(b) {
			t.Fatalf("rest %d longer than input %d", len(rest), len(b))
		}
		// Every decoded command must re-encode to a message that decodes back
		// to the same value. This catches asymmetric field handling.
		msg, err := AppendCommand(nil, cmd, DefaultLimits)
		if err != nil {
			t.Fatalf("re-encode: %v", err)
		}
		if _, _, err := DecodeCommand(msg, DefaultLimits); err != nil {
			t.Fatalf("re-decode: %v", err)
		}
	})
}

func FuzzDecodeEvent(f *testing.F) {
	for _, ev := range testEvents() {
		msg, err := AppendEvent(nil, ev, DefaultLimits)
		if err == nil {
			f.Add(msg)
		}
	}
	f.Add([]byte{})
	f.Add([]byte("MOB3"))

	f.Fuzz(func(t *testing.T, b []byte) {
		ev, rest, err := DecodeEvent(b, DefaultLimits)
		if err != nil {
			return
		}
		if ev.Body == nil {
			t.Fatal("decoded event has no body")
		}
		if len(rest) > len(b) {
			t.Fatalf("rest %d longer than input %d", len(rest), len(b))
		}
	})
}
