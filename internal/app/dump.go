package app

import (
	"context"
	"fmt"
	"os"

	"browser/internal/engine"
	"browser/internal/frontend"
	"browser/internal/protocol"
)

// dumpPage runs a one-shot render through the in-process transport and writes
// the frames to stdout. It preserves the byte-for-byte -dump and -png output.
func dumpPage(loader *engine.Loader, target string, width, viewportH int, noColor, forceColor, noJS, reader, dark, pngOut bool, pdfPage int, jsMem int64, budget engine.LayoutBudget, zoom float64) int {
	if width <= 0 {
		if w, _, ok := frontend.TerminalSize(os.Stdout); ok {
			width = w
		} else {
			width = 100
		}
	}
	color := !noColor && (forceColor || frontend.IsTTY(os.Stdout))
	h := engine.NewDumpHandler(engine.DumpConfig{
		Loader: loader, Target: target, Width: width, ViewportH: viewportH,
		NoJS: noJS, Reader: reader, Dark: dark, PNG: pngOut,
		PDFPage: pdfPage, JSMem: jsMem, Budget: budget, Zoom: zoom,
	})
	session, err := (&protocol.InProc{Limits: engine.DumpLimits(width, pngOut), EventQueue: 4}).Open(context.Background(), 1, h)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mob3: %s\n", cleanError(err))
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
					fmt.Fprintf(os.Stderr, "mob3: png: %s\n", cleanError(err))
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
			fmt.Fprintf(os.Stderr, "mob3: %s\n", protocol.StripControls(v.Text))
		case protocol.Crash:
			crashed = v.Message
		}
	}
	if crashed != "" {
		fmt.Fprintf(os.Stderr, "mob3: %s\n", protocol.StripControls(crashed))
		return 1
	}
	if frames == 0 {
		return 1
	}
	return 0
}
