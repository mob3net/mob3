package engine

import (
	"time"

	"browser/internal/protocol"
)

// statusState is the current status line and the one-shot error/hover flags.
type statusState struct {
	// hoverRebuildPending coalesces the style pass for pointer hover changes so
	// moving across a large page cannot rebuild the whole document per cell.
	hoverRebuildPending bool
	hoverRebuildAt      time.Time
	jsErr               bool
	// moduleErr records that an ES module script failed to load, link or
	// evaluate, so the footer can point at the console.
	moduleErr   bool
	pageStopped bool
	status      string
	// frontendOverlay is true while a front-end-owned window (the view or zoom
	// menu) covers the page. Native images and scaled text are disabled so
	// neither can composite above the window.
	frontendOverlay bool
	usage           procUsage
	quitPending     bool
}

// Shell geometry and state. The front end owns the terminal rows and the
// chrome reservation; moving this content-rectangle geometry to the front end
// is the remaining Phase 2 step.

// frameScratch is the reusable protocol frame for the draw path.
type frameScratch = protocol.Frame

// contentRect is the page viewport in terminal cells.
type contentRect struct {
	Top, Bottom int
	Width       int
	Height      int
}

// headerRows returns the number of rows used by the top bar. The front end
// supplies the content rectangle; the legacy rule remains for direct callers
// and tests.
func (b *Session) headerRows() int {
	if b.ContentTop >= 0 {
		return b.ContentTop
	}
	if b.Height < 3 {
		return 0
	}
	if b.Height < 5 {
		return 1
	}
	return 2
}

// viewport returns the first content row and the exclusive end row.
func (b *Session) viewport() (int, int) {
	if b.ContentTop >= 0 && b.ContentHeight > 0 {
		return b.ContentTop, b.ContentTop + b.ContentHeight
	}
	if b.Height < 3 {
		return 0, b.Height
	}
	return b.headerRows(), b.Height - 1
}

// normalizeContentRect validates the front-end rectangle against the terminal
// height. Zero values select the legacy derivation.
func normalizeContentRect(h, top, contentH int) (int, int) {
	if top < 0 || contentH < 1 || top >= h {
		return -1, 0
	}
	if top+contentH > h {
		contentH = h - top
	}
	return top, contentH
}

func (b *Session) viewportHeight() int {
	top, bottom := b.viewport()
	if bottom < top {
		return 1
	}
	return bottom - top
}

// frontendChromePath reports whether the normal front-end chrome path is in
// use: no overlay, console or find bar, and enough rows for a header/footer.
func (b *Session) frontendChromePath() bool {
	return b.emitFrames && b.frontendChrome && b.Height >= 3 &&
		!b.overlayOpen() && !b.consoleOpen && !b.FindActive
}

// keysEngine reports whether the engine currently owns key interpretation.
func (b *Session) keysEngine() bool {
	return b.FindActive || b.consoleOpen || b.dialog != nil ||
		b.download != nil || b.notice != nil || b.selectMenu != nil ||
		b.quitPending ||
		b.Forms.InputFocus != nil || b.Forms.ControlFocus != nil || b.LinkHints || b.HintConfirm
}

// frontendChromeActive reports whether the front end composes the page chrome.
// A focused page form still sends keys through the engine, but the chrome and
// scrollbar stay front-end-owned so the engine cannot emit header/footer cells
// in a live session.
func (b *Session) frontendChromeActive() bool {
	return b.frontendChromePath()
}

// contentRect computes the content rectangle once for a frame. Target state:
// the front end computes it and the engine receives only this rectangle.
func (b *Session) contentRect() contentRect {
	top, bottom := b.viewport()
	return contentRect{Top: top, Bottom: bottom, Width: b.Width, Height: bottom - top}
}

// stateMessage builds the trusted shell snapshot from page state. It is the
// payload of the State event; the front end never derives these values from
// page text itself.
func (b *Session) stateMessage() protocol.State {
	status := b.status
	if b.pageStopped {
		status = "Page stopped"
	}
	st := protocol.State{
		Status:    status,
		Loading:   b.Loading,
		Scroll:    b.Scroll,
		MaxScroll: b.maxScroll(),
	}
	if b.URL != nil {
		st.URL = b.URL.String()
	}
	if b.Page != nil {
		st.Title = b.Page.Title
	}
	if b.Progress > 0 {
		p := int(b.Progress*100 + 0.5)
		if p < 0 {
			p = 0
		}
		if p > 100 {
			p = 100
		}
		st.Progress = uint8(p)
	}
	st.Kind = b.pageKind()
	if b.Dark {
		st.Flags |= protocol.StateDark
	}
	if b.Reader {
		st.Flags |= protocol.StateReader
	}
	if b.jsErr {
		st.Flags |= protocol.StateJSError
	}
	if b.moduleErr {
		st.Flags |= protocol.StateModuleError
	}
	if b.PageHasHover {
		st.Flags |= protocol.StateHover
	}
	if b.loader != nil && !b.loader.NoImages {
		st.Flags |= protocol.StateImages
	}
	if b.BigText {
		st.Flags |= protocol.StateBigText
	}
	if b.LoopStopped {
		st.Flags |= protocol.StateLoopStopped
	}
	if b.nativeGraphicsEnabled() {
		st.Flags |= protocol.StateNativeImages
	}
	if d, ok := b.NextJSTimerDelay(b); ok && d <= time.Minute {
		st.Flags |= protocol.StateBusy
	}

	// Chrome state. The front end draws the normal header and footer from this
	// snapshot; overlays, the console and the find bar keep the full-frame or
	// engine-composed fallback for now.
	st.Width, st.Height = b.Width, b.Height
	st.HeaderRows = b.headerRows()
	st.FrontendChrome = b.frontendChromeActive()
	st.ZoomLabel = b.zoomBadge()
	st.ZoomPercent = int(Zoom()*100 + 0.5)
	if !b.HideFixed {
		st.ViewFlags |= protocol.ViewFlagFixed
	}
	if !b.NoSticky {
		st.ViewFlags |= protocol.ViewFlagSticky
	}
	if !b.HideBackgrounds {
		st.ViewFlags |= protocol.ViewFlagBackgrounds
	}
	st.FindActive = b.FindActive
	st.QuitPending = b.quitPending
	st.KeysEngine = b.keysEngine()
	fm := b.footerModel()
	st.FooterLeft, st.FooterRight = fm.left, fm.right
	st.FooterLevel = footerLevelMessage(fm.level)
	st.FooterStatusHit = fm.statusHit
	st.PDFHint, st.PDFHintX = fm.pdfHint, fm.pdfHintX
	return st
}

func footerLevelMessage(l statusLevel) protocol.FooterLevel {
	switch l {
	case statusWarning:
		return protocol.FooterWarning
	case statusError:
		return protocol.FooterError
	case statusSuccess:
		return protocol.FooterSuccess
	default:
		return protocol.FooterNormal
	}
}

func (b *Session) pageKind() protocol.PageKind { return b.PageKind() }
