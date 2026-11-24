package engine

import "browser/internal/protocol"

// clipboardSink records the front-end clipboard event emitted by the engine.
type clipboardSink struct {
	text string
}

func (s *clipboardSink) Send(ev protocol.Event) error {
	if v, ok := ev.Body.(protocol.Clipboard); ok {
		s.text = v.Text
	}
	return nil
}
