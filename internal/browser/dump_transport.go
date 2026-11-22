package browser

import (
	"context"
	"fmt"
	"os"
	"time"

	"browser/internal/frontend"
	"browser/internal/protocol"
)

// dumpChunkRows bounds one text-dump frame. The protocol limits are lowered to
// the frame size, and a tall page is delivered in several frames.
const dumpChunkRows = 512

// dumpPNGRows is the existing PNG row cap; one frame is enough for it.
const dumpPNGRows = 2000

// dumpOptions carries everything a one-shot -dump or -png run needs.
type dumpOptions struct {
	loader    *Loader
	target    string
	width     int
	viewportH int
	noJS      bool
	reader    bool
	dark      bool
	pngOut    bool
	pdfPage   int
	jsMem     int64
	budget    LayoutBudget
	zoom      float64
}

// dumpHandler runs the existing one-shot load and layout pipeline behind the
// same transport the interactive session uses.
type dumpHandler struct{ o dumpOptions }

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
	setLayoutZoom(o.zoom)
	o.loader.imageViewportPx = cssWidthPx(o.width)
	// A PNG has no browsing context, so it shows one page: page 1 unless the
	// caller selected another. Dumps keep every page.
	if o.pdfPage == 0 && o.pngOut {
		o.pdfPage = 1
	}
	if o.pdfPage < 0 {
		o.pdfPage = 0
	}
	o.loader.pdfPage = o.pdfPage
	page, err := o.loader.Load(o.target, nil)
	if err != nil {
		return err
	}
	if page.PDF != nil && o.pdfPage > 0 && page.PDF.TotalPages > 0 && len(page.PDF.Pages) == 0 {
		return fmt.Errorf("-pdf-page %d is out of range (document has %d pages)", o.pdfPage, page.PDF.TotalPages)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !o.noJS {
		runner := NewBrowser(o.width, 3)
		// NewBrowser resets the package-wide cell ratio; restore the run's zoom
		// before scripts run so script-visible viewport sizes match the dump.
		runner.zoom = o.zoom
		setLayoutZoom(o.zoom)
		runner.noJS = false
		runner.jsMemLimit = o.jsMem
		runner.startJS(page)
		defer runner.stopJS()
		page.CollectDynamicStyles()
		if !o.loader.noImages {
			o.loader.loadImages(context.Background(), page.Root, page.Base)
		}
	}
	setLayoutZoom(o.zoom)
	root, author := page.Root, page.Author
	if o.reader && page.PDF == nil {
		if extracted, ok := extractReaderPage(page.Root, page.Title); ok {
			root, author = extracted, readerRules
		}
	}
	deadline := time.Time{}
	if o.budget.Time > 0 {
		deadline = time.Now().Add(o.budget.Time)
	}
	layout := BuildLayoutBudgetDeadline(root, author, o.width, o.viewportH, false, o.dark, deadline, o.budget)
	// CSS background images are only known after the style pass, so a dump
	// loads them in a second image pass and rebuilds the layout once.
	if !o.loader.noImages {
		if o.loader.loadImages(context.Background(), root, page.Base) {
			if o.budget.Time > 0 {
				deadline = time.Now().Add(o.budget.Time)
			}
			layout = BuildLayoutBudgetDeadline(root, author, o.width, o.viewportH, false, o.dark, deadline, o.budget)
		}
	}
	full := layout.Height
	height := full
	if o.viewportH > 0 && height > o.viewportH {
		height = o.viewportH
	}
	if height < 1 {
		height = 1
	}
	rowCap := 20000
	if o.pngOut {
		rowCap = dumpPNGRows
	}
	if (o.viewportH == 0 || o.viewportH >= rowCap) && full > rowCap {
		switch {
		case o.pngOut && page.PDF != nil:
			h.warn(out, "PNG truncated to %d rows; use -pdf-page=N to export one page", rowCap)
		case o.pngOut:
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
	g := NewGrid(o.width, height, pageFG, pageBG)
	PaintLayout(g, layout, 0, 0, height, nil, pageFG)

	if o.pngOut {
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
		sub := Grid{W: o.width, H: rows, Cells: g.Cells[start*o.width : (start+rows)*o.width]}
		if err := out.Send(protocol.Event{Body: toProtocolFrame(&sub, 0)}); err != nil {
			return err
		}
	}
	return nil
}

// dumpLimits sizes the protocol limits to the frames a one-shot run emits.
func dumpLimits(width int, pngOut bool) protocol.Limits {
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

// dumpPage runs a one-shot render through the in-process transport and writes
// the frames to stdout. It preserves the byte-for-byte -dump and -png output.
func dumpPage(loader *Loader, target string, width, viewportH int, noColor, forceColor, noJS, reader, dark, pngOut bool, pdfPage int, jsMemLimit int64, budget LayoutBudget, zoom float64) int {
	if width <= 0 {
		if w, _, ok := terminalSize(os.Stdout); ok {
			width = w
		} else {
			width = 100
		}
	}
	color := !noColor && (forceColor || isTTY(os.Stdout))
	h := &dumpHandler{o: dumpOptions{
		loader: loader, target: target, width: width, viewportH: viewportH,
		noJS: noJS, reader: reader, dark: dark, pngOut: pngOut,
		pdfPage: pdfPage, jsMem: jsMemLimit, budget: budget, zoom: zoom,
	}}
	session, err := (&protocol.InProc{Limits: dumpLimits(width, pngOut), EventQueue: 4}).Open(context.Background(), 1, h)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mob3: %v\n", err)
		return 1
	}
	defer session.Close()

	crashed := ""
	frames := 0
	for ev := range session.Events() {
		switch v := ev.Body.(type) {
		case protocol.Frame:
			frames++
			if pngOut {
				data, err := frontend.RenderPNG(v, 1)
				if err != nil {
					fmt.Fprintf(os.Stderr, "mob3: png: %v\n", err)
					return 1
				}
				if _, err := os.Stdout.Write(data); err != nil {
					return 1
				}
				continue
			}
			for _, line := range frontend.EncodeFrame(v, color, false) {
				fmt.Println(line)
			}
		case protocol.Console:
			fmt.Fprintf(os.Stderr, "mob3: %s\n", stripTerminalControls(v.Text))
		case protocol.Crash:
			crashed = v.Message
		}
	}
	if crashed != "" {
		fmt.Fprintf(os.Stderr, "mob3: %s\n", stripTerminalControls(crashed))
		return 1
	}
	if frames == 0 {
		return 1
	}
	return 0
}
