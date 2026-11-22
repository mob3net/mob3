package browser

import "browser/internal/protocol"

// Shell-owned geometry and state. The front end owns the terminal rows, the
// chrome reservation and the snapshot it shows; the engine only receives the
// content rectangle and the page state it is asked to render.

// frameScratch is the reusable protocol frame for the draw path.
type frameScratch = protocol.Frame

// contentRect is the page viewport in terminal cells.
type contentRect struct {
	Top, Bottom int
	Width       int
	Height      int
}

// headerRows returns the number of rows used by the top bar.
func (b *Browser) headerRows() int {
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
	if b.height < 3 {
		return 0, b.height
	}
	return b.headerRows(), b.height - 1
}

func (b *Browser) viewportHeight() int {
	top, bottom := b.viewport()
	if bottom < top {
		return 1
	}
	return bottom - top
}

// contentRect computes the content rectangle once for a frame. The engine is
// expected to receive only this rectangle, never the full terminal size.
func (b *Browser) contentRect() contentRect {
	top, bottom := b.viewport()
	return contentRect{Top: top, Bottom: bottom, Width: b.width, Height: bottom - top}
}

// stateMessage builds the trusted shell snapshot from page state. It is the
// payload of the future State event; the front end never derives these values
// from page text itself.
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
	return st
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
