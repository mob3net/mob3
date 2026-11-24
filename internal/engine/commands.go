package engine

import "browser/internal/protocol"

// CommandHooks are the shell effects needed to apply one front-end command.
type CommandHooks interface {
	Init(protocol.Init)
	Resize(protocol.Resize)
	Navigate(string, bool)
	SetHistoryView(protocol.History)
	RestoreEntry(entryID uint64, url string, scroll int)
	SetActive(bool)
	SetGraphicsMode(protocol.GraphicsMode)
	FollowLink(int)
	HandleKey(string) bool
	ProtocolMouse(protocol.Mouse) bool
	ApplyScroll(protocol.Scroll)
	ApplyApp(protocol.App) (bool, error)
	ApplyFind(protocol.Find)
	ApplySelect(protocol.SelectResult)
	ApplyModal(protocol.ModalClose)
	ApplyHint(protocol.HintResult)
	ApplyConsole(protocol.ConsoleAction)
	LogClientError(string)
	Abort()
}

// HandleCommand applies one front-end command. It reports whether the session
// should exit.
func (s *Session) HandleCommand(cmd protocol.Command, h CommandHooks) (bool, error) {
	switch v := cmd.Body.(type) {
	case protocol.Init:
		h.Init(v)
	case protocol.Navigate:
		h.Navigate(v.URL, v.Push)
	case protocol.History:
		h.SetHistoryView(v)
	case protocol.Restore:
		h.RestoreEntry(v.EntryID, v.URL, v.Scroll)
	case protocol.Visibility:
		h.SetActive(v.Active)
	case protocol.Graphics:
		h.SetGraphicsMode(v.Mode)
	case protocol.FollowLink:
		h.FollowLink(v.Target)
	case protocol.Resize:
		h.Resize(v)
	case protocol.Key:
		if v.Token == "esc" || v.Token == "ctrl-c" {
			h.Abort()
		}
		return h.HandleKey(v.Token), nil
	case protocol.Mouse:
		return h.ProtocolMouse(v), nil
	case protocol.Scroll:
		h.ApplyScroll(v)
	case protocol.App:
		return h.ApplyApp(v)
	case protocol.Abort:
		h.Abort()
	case protocol.Find:
		h.ApplyFind(v)
	case protocol.SelectResult:
		h.ApplySelect(v)
	case protocol.ModalClose:
		h.ApplyModal(v)
	case protocol.HintResult:
		h.ApplyHint(v)
	case protocol.ConsoleAction:
		h.ApplyConsole(v)
	case protocol.ClientError:
		h.LogClientError(v.Message)
	case protocol.Shutdown:
		return true, nil
	}
	return false, nil
}
