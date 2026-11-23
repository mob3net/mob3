package engine

import "testing"

func TestStateKeysEngineFlag(t *testing.T) {
	b := newTestSession(80, 24)
	b.emitFrames = true
	b.frontendChrome = true
	if st := b.stateMessage(); st.KeysEngine {
		t.Fatal("base page marked engine keys")
	}
	b.FindActive = true
	if st := b.stateMessage(); !st.KeysEngine {
		t.Fatal("find did not mark engine keys")
	}
	b.FindActive = false
	b.Forms.InputFocus = &Node{}
	if st := b.stateMessage(); !st.KeysEngine {
		t.Fatal("input focus did not mark engine keys")
	}
}

func TestFormFocusKeepsFrontendChrome(t *testing.T) {
	b := newTestSession(80, 24)
	b.emitFrames = true
	b.frontendChrome = true
	b.Forms.InputFocus = &Node{Tag: "input"}
	st := b.stateMessage()
	if !st.KeysEngine {
		t.Fatal("form focus did not keep engine keys")
	}
	if !st.FrontendChrome {
		t.Fatal("form focus handed chrome back to the engine")
	}
}
