package browser

import (
	"context"
	"fmt"
	"time"

	"browser/internal/protocol"
)

// dumpChunkRows bounds one text-dump frame. The protocol limits are lowered to
// the frame size, and a tall page is delivered in several frames.
const dumpChunkRows = 512

// dumpPNGRows is the existing PNG row cap; one frame is enough for it.
const dumpPNGRows = 2000

// DumpConfig carries everything a one-shot -dump or -png run needs.
type DumpConfig struct {
	Loader    *Loader
	Target    string
	Width     int
	ViewportH int
	NoJS      bool
	Reader    bool
	Dark      bool
	PNG       bool
	PDFPage   int
	JSMem     int64
	Budget    LayoutBudget
	Zoom      float64
}

// DumpHandler runs the existing one-shot load and layout pipeline behind the
// same transport the interactive session uses.
type dumpHandler struct{ o DumpConfig }

// NewDumpHandler creates the one-shot load/layout/paint handler used by the
// app's -dump and -png paths.
func NewDumpHandler(o DumpConfig) protocol.Handler { return &dumpHandler{o: o} }

// Serve implements protocol.Handler.
func (h *dumpHandler) Serve(ctx context.Context, abort <-chan struct{}, in <-chan protocol.Command, out protocol.EventSink) error {
	return h.run(ctx, out)
}

func (h *dumpHandler) warn(out protocol.EventSink, format string, args ...any) {
	_ = out.Send(protocol.Event{Body: protocol.Console{
		Level: protocol.ConsoleWarn,
		Text:  fmt.Sprintf(format, args...),
	}})
}

func (h *dumpHandler) run(ctx context.Context, out protocol.EventSink) error {
	o := h.o
	setLayoutZoom(o.Zoom)
	o.Loader.ViewportPx = cssWidthPx(o.Width)
	// A PNG has no browsing context, so it shows one page: page 1 unless the
	// caller selected another. Dumps keep every page.
	if o.PDFPage == 0 && o.PNG {
		o.PDFPage = 1
	}
	if o.PDFPage < 0 {
		o.PDFPage = 0
	}
	o.Loader.PDFPage = o.PDFPage
	page, err := o.Loader.Load(o.Target, nil)
	if err != nil {
		return err
	}
	if page.PDF != nil && o.PDFPage > 0 && page.PDF.TotalPages > 0 && len(page.PDF.Pages) == 0 {
		return fmt.Errorf("-pdf-page %d is out of range (document has %d pages)", o.PDFPage, page.PDF.TotalPages)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !o.NoJS {
		runner := NewBrowser(o.Width, 3)
		if o.Loader != nil {
			runner.setLoader(o.Loader)
		}
		// NewBrowser resets the package-wide cell ratio; restore the run's zoom
		// before scripts run so script-visible viewport sizes match the dump.
		runner.Zoom = o.Zoom
		setLayoutZoom(o.Zoom)
		runner.NoJS = false
		runner.JSMemLimit = o.JSMem
		runner.startJS(page)
		defer runner.stopJS()
		page.CollectDynamicStyles()
		if !o.Loader.NoImages {
			o.Loader.LoadImages(context.Background(), page.Root, page.Base)
		}
	}
	setLayoutZoom(o.Zoom)
	root, author := page.Root, page.Author
	if o.Reader && page.PDF == nil {
		if extracted, ok := extractReaderPage(page.Root, page.Title); ok {
			root, author = extracted, readerRules
		}
	}
	deadline := time.Time{}
	if o.Budget.Time > 0 {
		deadline = time.Now().Add(o.Budget.Time)
	}
	layout := BuildLayoutBudgetDeadline(root, author, o.Width, o.ViewportH, false, o.Dark, deadline, o.Budget)
	// CSS background images are only known after the style pass, so a dump
	// loads them in a second image pass and rebuilds the layout once.
	if !o.Loader.NoImages {
		if o.Loader.LoadImages(context.Background(), root, page.Base) {
			if o.Budget.Time > 0 {
				deadline = time.Now().Add(o.Budget.Time)
			}
			layout = BuildLayoutBudgetDeadline(root, author, o.Width, o.ViewportH, false, o.Dark, deadline, o.Budget)
		}
	}
	full := layout.Height
	height := full
	if o.ViewportH > 0 && height > o.ViewportH {
		height = o.ViewportH
	}
	if height < 1 {
		height = 1
	}
	rowCap := 20000
	if o.PNG {
		rowCap = dumpPNGRows
	}
	if (o.ViewportH == 0 || o.ViewportH >= rowCap) && full > rowCap {
		switch {
		case o.PNG && page.PDF != nil:
			h.warn(out, "PNG truncated to %d rows; use -pdf-page=N to export one page", rowCap)
		case o.PNG:
			h.warn(out, "PNG truncated to %d rows", rowCap)
		default:
			h.warn(out, "output truncated to %d rows", rowCap)
		}
	}
	if height > rowCap {
		height = rowCap
	}
	pageFG := RGB(0x1f, 0x23, 0x28)
	if layout.Body != nil && layout.Body.Style != nil && layout.Body.Style.Color.Set {
		pageFG = layout.Body.Style.Color
	}
	pageBG := layout.PageBG
	if !pageBG.Set {
		pageBG = RGB(255, 255, 255)
	}
	g := NewGrid(o.Width, height, pageFG, pageBG)
	PaintLayout(g, layout, 0, 0, height, nil, pageFG)

	if o.PNG {
		f := toProtocolFrame(g, 0)
		return out.Send(protocol.Event{Body: f})
	}
	// Split the finished grid instead of repainting chunks, so fixed and sticky
	// elements and image crops stay exactly as the single full paint produced
	// them.
	for start := 0; start < height; start += dumpChunkRows {
		rows := height - start
		if rows > dumpChunkRows {
			rows = dumpChunkRows
		}
		sub := Grid{W: o.Width, H: rows, Cells: g.Cells[start*o.Width : (start+rows)*o.Width]}
		if err := out.Send(protocol.Event{Body: toProtocolFrame(&sub, 0)}); err != nil {
			return err
		}
	}
	return nil
}

// DumpLimits sizes the protocol limits to the frames a one-shot run emits.
func DumpLimits(width int, pngOut bool) protocol.Limits {
	h := dumpChunkRows
	if pngOut {
		h = dumpPNGRows
	}
	lim := protocol.DefaultLimits
	if width > lim.MaxFrameWidth {
		lim.MaxFrameWidth = width
	}
	if h > lim.MaxFrameHeight {
		lim.MaxFrameHeight = h
	}
	if cells := width * h; cells > lim.MaxCells {
		lim.MaxCells = cells
	}
	return lim
}
