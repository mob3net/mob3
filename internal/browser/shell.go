package browser

import "browser/internal/protocol"

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
func (b *Browser) headerRows() int {
	if b.contentTop >= 0 {
		return b.contentTop
	}
	if b.height < 3 {
		return 0
	}
	if b.height < 5 {
		return 1
	}
	return 2
}

// viewport returns the first content row and the exclusive end row.
func (b *Browser) viewport() (int, int) {
	if b.contentTop >= 0 && b.contentHeight > 0 {
		return b.contentTop, b.contentTop + b.contentHeight
	}
	if b.height < 3 {
		return 0, b.height
	}
	return b.headerRows(), b.height - 1
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

func (b *Browser) viewportHeight() int {
	top, bottom := b.viewport()
	if bottom < top {
		return 1
	}
	return bottom - top
}

// frontendChromePath reports whether the normal front-end chrome path is in
// use: no overlay, console or find bar, and enough rows for a header/footer.
func (b *Browser) frontendChromePath() bool {
	return b.emitFrames && b.frontendChrome && b.height >= 3 &&
		!b.overlayOpen() && !b.consoleOpen && !b.findActive
}

// keysEngine reports whether the engine currently owns key interpretation.
func (b *Browser) keysEngine() bool {
	return b.prompt || b.findActive || b.consoleOpen || b.dialog != nil ||
		b.download != nil || b.notice != nil || b.selectMenu != nil ||
		b.viewMenu != nil || b.zoomMenu != nil || b.quitPending ||
		b.inputFocus != nil || b.controlFocus != nil || b.linkHints || b.hintConfirm
}

// frontendChromeActive is the normal path while the front end also owns the
// base key/mouse state. Page form fields and engine-owned fallback modals keep
// the engine on the chrome and key path, which is the remaining trust gap.
func (b *Browser) frontendChromeActive() bool {
	return b.frontendChromePath() && !b.keysEngine()
}

// contentRect computes the content rectangle once for a frame. Target state:
// the front end computes it and the engine receives only this rectangle.
func (b *Browser) contentRect() contentRect {
	top, bottom := b.viewport()
	return contentRect{Top: top, Bottom: bottom, Width: b.width, Height: bottom - top}
}

// stateMessage builds the trusted shell snapshot from page state. It is the
// payload of the State event; the front end never derives these values from
// page text itself.
func (b *Browser) stateMessage() protocol.State {
	st := protocol.State{
		Status:    b.status,
		Loading:   b.loading,
		Scroll:    b.scroll,
		MaxScroll: b.maxScroll(),
	}
	if b.url != nil {
		st.URL = b.url.String()
	}
	if b.page != nil {
		st.Title = b.page.Title
	}
	if b.progress > 0 {
		p := int(b.progress*100 + 0.5)
		if p < 0 {
			p = 0
		}
		if p > 100 {
			p = 100
		}
		st.Progress = uint8(p)
	}
	st.Kind = b.pageKind()
	if b.dark {
		st.Flags |= protocol.StateDark
	}
	if b.reader {
		st.Flags |= protocol.StateReader
	}
	if b.jsErr {
		st.Flags |= protocol.StateJSError
	}
	if b.moduleErr {
		st.Flags |= protocol.StateModuleError
	}
	if b.pageHasHover {
		st.Flags |= protocol.StateHover
	}
	if b.loader != nil && !b.loader.noImages {
		st.Flags |= protocol.StateImages
	}
	if b.bigText {
		st.Flags |= protocol.StateBigText
	}
	if b.loopStopped {
		st.Flags |= protocol.StateLoopStopped
	}
	if b.gfx != nil {
		st.Flags |= protocol.StateNativeImages
	}

	// Chrome state. The front end draws the normal header and footer from this
	// snapshot; overlays, the console and the find bar keep the full-frame or
	// engine-composed fallback for now.
	st.Width, st.Height = b.width, b.height
	st.HeaderRows = b.headerRows()
	st.FrontendChrome = b.frontendChromeActive()
	st.ZoomLabel = b.zoomBadge()
	st.ZoomPercent = int(b.zoom*100 + 0.5)
	if !b.hideFixed {
		st.ViewFlags |= protocol.ViewFlagFixed
	}
	if !b.noSticky {
		st.ViewFlags |= protocol.ViewFlagSticky
	}
	if !b.hideBackgrounds {
		st.ViewFlags |= protocol.ViewFlagBackgrounds
	}
	st.PromptActive = b.prompt
	st.PromptText = b.promptText
	st.PromptCursor = b.promptCursor
	st.PromptSelectAll = b.promptSelectAll
	st.FindActive = b.findActive
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

func (b *Browser) pageKind() protocol.PageKind {
	if b.page == nil {
		return protocol.PageHTML
	}
	switch {
	case b.page.PDF != nil:
		return protocol.PagePDF
	case b.page.Download:
		return protocol.PageDownload
	case b.url != nil && b.url.Scheme == "about":
		return protocol.PageAbout
	}
	return protocol.PageHTML
}
