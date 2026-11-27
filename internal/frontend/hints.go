package frontend

import (
	"strings"

	"browser/internal/protocol"
	"browser/internal/textwidth"
)

// hintOverlay is the link-hint session layered over the page.
type hintOverlay struct {
	hintActive bool
	hintView   HintView
	hintBase   protocol.State
}

func (w *hintOverlay) isOpen() bool       { return w.hintActive }
func (w *hintOverlay) layer() windowLayer { return layerHint }

// persistent is true: hints are cancelled explicitly by the callers that need
// it, so a page modal cannot silently drop a pending hint.
func (w *hintOverlay) persistent() bool { return true }

func (w *hintOverlay) reset(l *Tab) {
	w.hintActive = false
	if l.Presenter != nil {
		l.Presenter.SetHint(nil)
	}
}

func (w *hintOverlay) install(l *Tab) bool {
	if l.Presenter != nil {
		l.Presenter.SetHint(&w.hintView)
	}
	return false
}

func (w *hintOverlay) start(l *Tab, v protocol.Hints) {
	l.dismissOverlays()
	w.hintActive = true
	w.hintView = HintView{Mode: v.Mode, Prefix: v.Prefix, Items: v.Items}
	w.hintBase = l.State.Page
	if w.hintView.Prefix != "" {
		l.State.Page.FooterLeft = " Choose hint: " + w.hintView.Prefix + " (Esc cancels)"
	}
	w.sync(l)
}

func (w *hintOverlay) sync(l *Tab) {
	st := l.State.Page
	st.FrontendChrome = true
	st.KeysEngine = true
	st.Menu = protocol.MenuNone
	st.PromptActive = false
	st.FindActive = false
	l.State.Page = st
	if l.Presenter != nil {
		l.Presenter.SetState(st)
		l.Presenter.SetHint(&w.hintView)
		_ = l.Presenter.Refresh()
	}
}

func (w *hintOverlay) close(l *Tab) {
	if !w.hintActive {
		return
	}
	w.hintActive = false
	l.State.Page = w.hintBase
	if l.Presenter != nil {
		l.Presenter.SetHint(nil)
		l.Presenter.SetState(w.hintBase)
		_ = l.Presenter.Refresh()
	}
}

func (w *hintOverlay) answer(l *Tab, target int) {
	w.close(l)
	_ = l.Session.Send(protocol.Command{Body: protocol.HintResult{Target: target}})
}

func (w *hintOverlay) cancel(l *Tab) {
	if !w.hintActive {
		return
	}
	w.close(l)
	_ = l.Session.Send(protocol.Command{Body: protocol.HintResult{Cancel: true}})
}

func (w *hintOverlay) applyState(l *Tab, st protocol.State) bool {
	w.hintBase = st
	if !st.KeysEngine {
		w.close(l)
		return true
	}
	nextPageState(l, st)
	w.sync(l)
	return true
}

func (w *hintOverlay) handleKey(l *Tab, k string) (handled, quit bool) {
	if w.hintView.Preview {
		switch k {
		case "enter":
			w.answer(l, w.hintView.PreviewTarget)
		case "y", "Y":
			w.copyURL(l)
		case "esc", "n", "N", "ctrl-c":
			w.cancel(l)
		default:
			w.cancel(l)
			return false, false
		}
		return true, false
	}
	if k == "esc" || k == "ctrl-c" {
		w.cancel(l)
		return true, false
	}
	if len(k) == 1 {
		c := k[0]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c >= 'a' && c <= 'z' {
			next := w.hintView.Prefix + string(c)
			matches := 0
			for _, item := range w.hintView.Items {
				if strings.HasPrefix(item.Label, next) {
					matches++
				}
			}
			if matches == 0 {
				return true, false
			}
			if matches == 1 {
				var item protocol.HintItem
				for _, it := range w.hintView.Items {
					if strings.HasPrefix(it.Label, next) {
						item = it
						break
					}
				}
				if item.Link && w.hintView.Mode == protocol.HintNewTab {
					w.openInNewTab(l, item.Href)
					return true, false
				}
				if item.Link && w.hintView.Mode == protocol.HintPreview {
					w.preview(l, item)
					return true, false
				}
				w.answer(l, item.Target)
				return true, false
			}
			w.hintView.Prefix = next
			l.State.Page.FooterLeft = " Choose hint: " + next + " (Esc cancels)"
			w.sync(l)
			return true, false
		}
	}
	w.cancel(l)
	return false, false
}

// preview shows a chosen link's destination and waits for Enter or y.
func (w *hintOverlay) preview(l *Tab, item protocol.HintItem) {
	w.hintView.Preview = true
	w.hintView.PreviewTarget = item.Target
	w.hintView.PreviewHref = item.Href
	w.hintView.Items = nil
	l.State.Page.FooterLeft = " " + item.Href + "  (Enter follows · y copies · Esc cancels)"
	w.sync(l)
}

// copyURL cancels the pending hint and copies its destination.
func (w *hintOverlay) copyURL(l *Tab) {
	href := w.hintView.PreviewHref
	w.close(l)
	if l.Session != nil {
		_ = l.Session.Send(protocol.Command{Body: protocol.HintResult{Cancel: true}})
	}
	l.copyURLToClipboard(href)
}

// openInNewTab clears the engine hint state and opens a resolved link in a
// background tab.
func (w *hintOverlay) openInNewTab(l *Tab, href string) {
	w.close(l)
	if l.Session != nil {
		_ = l.Session.Send(protocol.Command{Body: protocol.HintResult{Cancel: true}})
	}
	if l.Actions.OpenTabBackground != nil && validDisplayURL(href) {
		l.Actions.OpenTabBackground(href)
	}
}

func (w *hintOverlay) handleMouse(l *Tab, ev MouseEvent) bool {
	w.cancel(l)
	return false
}

// HintView is the front-end state of one link-hint session.
type HintView struct {
	Mode          protocol.HintMode
	Prefix        string
	Items         []protocol.HintItem
	Preview       bool
	PreviewTarget int
	PreviewHref   string
}

// ApplyHints paints the matching hint letters and fallback labels on a content
// frame. Coordinates are content-viewport cells.
func ApplyHints(f *protocol.Frame, v *HintView, color bool) {
	if v == nil || f.W <= 0 || f.H <= 0 || len(f.Cells) < f.W*f.H {
		return
	}
	bg := rgb(0xff, 0xd7, 0x00)
	fg := rgb(0x1f, 0x23, 0x28)
	attrs := uint8(protocol.AttrBold | protocol.AttrUnderline)
	if !color {
		attrs |= protocol.AttrReverse
	}
	for _, item := range v.Items {
		if !strings.HasPrefix(item.Label, v.Prefix) {
			continue
		}
		if len(item.Chars) > 0 {
			chars := item.Chars
			if len(v.Prefix) < len(chars) {
				chars = chars[len(v.Prefix):]
			} else {
				chars = nil
			}
			for _, c := range chars {
				paintHintCell(f, c.X, c.Y, c.R, fg, bg, attrs)
			}
			continue
		}
		label := item.Label
		if len(v.Prefix) < len(label) {
			label = label[len(v.Prefix):]
		} else {
			continue
		}
		x, y := item.X, item.Y
		if y < 0 || y >= f.H {
			continue
		}
		if x+len(label) > f.W {
			x = f.W - len(label)
		}
		if x < 0 {
			continue
		}
		for _, r := range label {
			paintHintCell(f, x, y, r, fg, bg, attrs)
			x += textwidth.RuneWidth(r)
		}
	}
}

func paintHintCell(f *protocol.Frame, x, y int, r rune, fg, bg protocol.Color, attrs uint8) {
	if x < 0 || x >= f.W || y < 0 || y >= f.H || textwidth.RuneWidth(r) == 0 {
		return
	}
	c := &f.Cells[y*f.W+x]
	c.R = r
	c.FG = fg
	c.BG = bg
	c.Attrs = attrs
	c.Flags &^= protocol.CellCont | protocol.CellSkip
	c.Run = 0
}
