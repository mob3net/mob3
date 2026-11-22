package browser

import "browser/internal/engine"

// Temporary style-primitive aliases to internal/engine. They keep the browser
// package compiling while the rest of the style and layout engine moves.

type Color = engine.Color
type Length = engine.Length
type Unit = engine.Unit
type LengthFunc = engine.LengthFunc

const (
	UnitNone    = engine.UnitNone
	UnitPX      = engine.UnitPX
	UnitEM      = engine.UnitEM
	UnitREM     = engine.UnitREM
	UnitPercent = engine.UnitPercent
	UnitPT      = engine.UnitPT
	UnitCH      = engine.UnitCH
	UnitLH      = engine.UnitLH
	UnitVW      = engine.UnitVW
	UnitVH      = engine.UnitVH
	UnitVMin    = engine.UnitVMin
	UnitVMax    = engine.UnitVMax
)

// cellW, cellH and layoutZoom mirror the engine values for the browser-side
// text metrics and JS measurement. setLayoutZoom is the only mutator.
var (
	cellW      = engine.CellW()
	cellH      = engine.CellH()
	layoutZoom = engine.Zoom()
)

func setLayoutZoom(z float64) {
	engine.SetZoom(z)
	layoutZoom = engine.Zoom()
	cellW = engine.CellW()
	cellH = engine.CellH()
}

func RGB(r, g, b uint8) Color { return engine.RGB(r, g, b) }
func Transparent() Color      { return engine.Transparent() }

func parseColor(v string, current Color) (Color, bool) { return engine.ParseColor(v, current) }
func opaque(c Color) bool                              { return c.Opaque() }
func clamp(v, lo, hi float64) float64                  { return engine.Clamp(v, lo, hi) }

func splitFields(s string) []string             { return engine.SplitFields(s) }
func splitTopLevel(s string, sep byte) []string { return engine.SplitTopLevel(s, sep) }

func px(v float64) Length  { return engine.PX(v) }
func pct(v float64) Length { return engine.Pct(v) }

func parseLength(v string, fontPx float64) Length { return engine.ParseLength(v, fontPx) }
func parseLengthSimple(v string) Length           { return engine.ParseLengthSimple(v) }
func parseFontSize(v string, parentPx float64) (float64, bool) {
	return engine.ParseFontSize(v, parentPx)
}

func finiteNumber(f float64) bool { return engine.FiniteNumber(f) }

func nonFiniteLength(val string) bool { return engine.NonFiniteLength(val) }
func lengthProperty(prop string) bool { return engine.LengthProperty(prop) }

func resolveLengthLH(l *Length, linePx float64) { engine.ResolveLengthLH(l, linePx) }
func resolveLengthREM(l *Length, rootPx float64) {
	engine.ResolveLengthREM(l, rootPx)
}
func resolveViewportLength(l *Length, vw, vh float64) {
	engine.ResolveViewportLength(l, vw, vh)
}
