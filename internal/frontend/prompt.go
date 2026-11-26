package frontend

import (
	"strings"
	"unicode/utf8"

	"browser/internal/protocol"
	"browser/internal/textedit"
	"browser/internal/textwidth"
)

// promptOverlay is the URL and command line.
type promptOverlay struct {
	promptActive    bool
	promptVim       bool
	promptText      string
	promptCursor    int
	promptSelectAll bool
	promptBase      protocol.State
	promptHist      lineHistory
}

func (w *promptOverlay) isOpen() bool         { return w.promptActive }
func (w *promptOverlay) dismissOnModal() bool { return true }

func (w *promptOverlay) reset(l *Tab) {
	w.promptActive = false
	w.promptVim = false
	w.promptText = ""
	w.promptCursor = 0
	w.promptSelectAll = false
}

// install is a no-op: the prompt travels in protocol.State.
func (w *promptOverlay) install(l *Tab) bool { return false }

func (w *promptOverlay) start(l *Tab, vim bool) {
	w.startValue(l, l.State.Page.URL, vim, true)
}

// startEmpty opens the URL prompt with no text, used by a fresh tab so the next
// keystrokes go straight into the address.
func (w *promptOverlay) startEmpty(l *Tab) {
	w.startValue(l, "", false, false)
}

func (w *promptOverlay) startValue(l *Tab, text string, vim, selectAll bool) {
	if w.promptActive {
		return
	}
	w.promptActive = true
	w.promptVim = vim
	w.promptBase = l.State.Page
	w.promptText = text
	w.promptCursor = len(w.promptText)
	w.promptSelectAll = selectAll
	w.promptHist.reset()
	w.sync(l)
}

func (w *promptOverlay) sync(l *Tab) {
	st := l.State.Page
	st.PromptActive = true
	st.PromptText = w.promptText
	st.PromptCursor = w.promptCursor
	st.PromptSelectAll = w.promptSelectAll
	st.KeysEngine = true
	l.State.Page = st
	if l.Presenter != nil {
		l.Presenter.SetState(st)
		_ = l.Presenter.Refresh()
	}
}

func (w *promptOverlay) close(l *Tab) {
	if !w.promptActive {
		return
	}
	w.promptActive = false
	w.promptVim = false
	w.promptText = ""
	w.promptCursor = 0
	w.promptSelectAll = false
	l.State.Page = w.promptBase
	if l.Presenter != nil {
		l.Presenter.SetState(w.promptBase)
		_ = l.Presenter.Refresh()
	}
}

func (w *promptOverlay) applyState(l *Tab, st protocol.State) bool {
	w.promptBase = st
	if st.KeysEngine {
		w.close(l)
		return true
	}
	nextPageState(l, st)
	w.sync(l)
	return true
}

func (w *promptOverlay) handleKey(l *Tab, k string) (handled, quit bool) {
	switch k {
	case "enter":
		text := strings.TrimSpace(w.promptText)
		q := w.promptVim && (text == "q" || text == ":q")
		w.close(l)
		if q {
			return true, true
		}
		if text != "" {
			w.promptHist.record(text)
			l.navigate(text)
		}
		return true, false
	case "esc":
		w.close(l)
		return true, false
	case "ctrl-c":
		l.Session.Abort()
		w.close(l)
		return true, false
	case "up", "ctrl-p":
		if prev, ok := w.promptHist.up(w.promptText); ok {
			w.promptText = prev
			w.promptCursor = len(prev)
		}
		w.promptSelectAll = false
		w.sync(l)
		return true, false
	case "down", "ctrl-n":
		if next, ok := w.promptHist.down(w.promptText); ok {
			w.promptText = next
			w.promptCursor = len(next)
		}
		w.promptSelectAll = false
		w.sync(l)
		return true, false
	}
	text, cur, _ := textedit.EditLineKey(w.promptText, w.promptCursor, k, textedit.EditConfig{
		SelAll:            &w.promptSelectAll,
		CollapseSelection: true,
	})
	w.promptText = text
	if cur > len(text) {
		cur = len(text)
	}
	w.promptCursor = cur
	w.sync(l)
	return true, false
}

func (w *promptOverlay) handleMouse(l *Tab, ev MouseEvent) bool {
	if ev.Wheel != 0 {
		return false
	}
	if ev.Motion || !ev.Press {
		return true
	}
	st := l.State.Page
	x, y := ev.X-1, ev.Y-1
	for _, hit := range LayoutChromeWithTabs(st, l.TabBar, l.TabOffset).Hits {
		if x < hit.X || x >= hit.X+hit.W || y < hit.Y || y >= hit.Y+hit.H {
			continue
		}
		switch hit.Kind {
		case ChromeHitURL:
			w.promptCursor = promptOffsetAt(w.promptText, w.promptCursor, w.promptSelectAll, x-hit.X, hit.W)
			w.promptSelectAll = false
			w.sync(l)
			return true
		case ChromeHitZoom:
			w.close(l)
			_ = l.Session.Send(protocol.Command{Body: protocol.App{Action: protocol.AppOpenZoom}})
			return true
		default:
			// A click on another chrome control closes the prompt and runs that
			// control in the same event, like Esc followed by the click.
			w.close(l)
			l.activateChromeHit(hit)
			return true
		}
	}
	w.close(l)
	return true
}

func runeIndexToByte(runes []rune, i int) int {
	if i <= 0 {
		return 0
	}
	if i > len(runes) {
		i = len(runes)
	}
	return len(string(runes[:i]))
}

func promptWindow(text string, cursor, contentW int, selectAll bool) (runes []rune, start, end, cur int) {
	runes = []rune(text)
	if contentW < 1 {
		return runes, 0, 0, 0
	}
	if selectAll {
		visible := text
		if textwidth.StringWidth(visible) > contentW {
			visible = textwidth.Tail(visible, contentW)
		}
		start = len(runes) - utf8.RuneCountInString(visible)
		return runes, start, len(runes), -1
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(text) {
		cursor = len(text)
	}
	cur = utf8.RuneCountInString(text[:cursor])
	if cur < 0 {
		cur = 0
	}
	if cur > len(runes) {
		cur = len(runes)
	}
	start = cur
	used := 0
	for start > 0 {
		rw := textwidth.RuneWidth(runes[start-1])
		if used+rw > contentW-1 {
			break
		}
		used += rw
		start--
	}
	after := cur
	if cur < len(runes) {
		after = cur + 1
	}
	used++
	end = after
	for end < len(runes) {
		rw := textwidth.RuneWidth(runes[end])
		if used+rw > contentW {
			break
		}
		used += rw
		end++
	}
	return runes, start, end, cur
}

func promptOffsetAt(text string, cursor int, selectAll bool, x, width int) int {
	prefixW := textwidth.StringWidth(" Open: ")
	return textOffsetAt(text, cursor, selectAll, x-prefixW, width-prefixW)
}

// textOffsetAt maps a click at cell rel in a text field of contentW cells to a
// byte offset, using the same visible window as the field painter.
func textOffsetAt(text string, cursor int, selectAll bool, rel, contentW int) int {
	if contentW < 1 || rel <= 0 {
		return 0
	}
	runes, start, end, cur := promptWindow(text, cursor, contentW, selectAll)
	used := 0
	for i := start; i < end; i++ {
		if i == cur {
			if rel < used+1 {
				return runeIndexToByte(runes, i)
			}
			used++
			continue
		}
		rw := textwidth.RuneWidth(runes[i])
		if rel < used+rw {
			return runeIndexToByte(runes, i)
		}
		used += rw
	}
	return runeIndexToByte(runes, end)
}
