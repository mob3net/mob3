package browser

import (
	"time"

	"browser/internal/engine"
)

// Temporary layout aliases. Paint, JS, automation and the remaining browser
// tests keep their old API while layout lives in internal/engine.

type Frag = engine.Frag
type Layout = engine.Layout
type LinkInfo = engine.LinkInfo
type Rect = engine.Rect
type LayoutBudget = engine.LayoutBudget
type FragKind = engine.FragKind
type layouter = engine.Layouter
type Layouter = engine.Layouter

const (
	FragText  = engine.FragText
	FragFill  = engine.FragFill
	FragImage = engine.FragImage
	FragLink  = engine.FragLink

	fragText  = engine.FragText
	fragFill  = engine.FragFill
	fragImage = engine.FragImage
	fragLink  = engine.FragLink
)

var DefaultLayoutBudget = engine.DefaultLayoutBudget

func ParseLayoutBudget(name string) (LayoutBudget, bool) { return engine.ParseLayoutBudget(name) }

func BuildLayout(root *Node, author []Rule, width int) *Layout {
	return engine.BuildLayout(root, author, width)
}
func BuildLayoutBig(root *Node, author []Rule, width int, bigText bool) *Layout {
	return engine.BuildLayoutBig(root, author, width, bigText)
}
func BuildLayoutDeadline(root *Node, author []Rule, width int, bigText bool, deadline time.Time) *Layout {
	return engine.BuildLayoutDeadline(root, author, width, bigText, deadline)
}
func BuildLayoutViewportDeadline(root *Node, author []Rule, width, viewportH int, bigText bool, deadline time.Time) *Layout {
	return engine.BuildLayoutViewportDeadline(root, author, width, viewportH, bigText, deadline)
}
func BuildLayoutDarkDeadline(root *Node, author []Rule, width int, bigText bool, deadline time.Time) *Layout {
	return engine.BuildLayoutDarkDeadline(root, author, width, bigText, deadline)
}
func BuildLayoutDarkViewportDeadline(root *Node, author []Rule, width, viewportH int, bigText bool, deadline time.Time) *Layout {
	return engine.BuildLayoutDarkViewportDeadline(root, author, width, viewportH, bigText, deadline)
}
func BuildLayoutBudgetDeadline(root *Node, author []Rule, width, viewportH int, bigText, dark bool, deadline time.Time, budget LayoutBudget) *Layout {
	return engine.BuildLayoutBudgetDeadline(root, author, width, viewportH, bigText, dark, deadline, budget)
}
func BuildLayoutBudgetDeadlineOptions(root *Node, author []Rule, width, viewportH int, bigText, dark, hideFixed bool, deadline time.Time, budget LayoutBudget) *Layout {
	return engine.BuildLayoutBudgetDeadlineOptions(root, author, width, viewportH, bigText, dark, hideFixed, deadline, budget)
}
func BuildLayoutBudgetDeadlineImages(root *Node, author []Rule, width, viewportH int, bigText, dark, hideFixed, noSticky, imagesOff bool, deadline time.Time, budget LayoutBudget) *Layout {
	return engine.BuildLayoutBudgetDeadlineImages(root, author, width, viewportH, bigText, dark, hideFixed, noSticky, imagesOff, deadline, budget)
}

func maxInt(a, b int) int                      { return engine.MaxInt(a, b) }
func minInt(a, b int) int                      { return engine.MinInt(a, b) }
func runeWidth(r rune) int                     { return engine.RuneWidth(r) }
func isWideRune(r rune) bool                   { return engine.IsWideRune(r) }
func stringWidth(s string) int                 { return engine.StringWidth(s) }
func truncateWidth(s string, max int) string   { return engine.TruncateWidth(s, max) }
func padWidth(s string, w int) string          { return engine.PadWidth(s, w) }
func fitWidth(s string, w int) string          { return engine.FitWidth(s, w) }
func applyTextTransform(s, mode string) string { return engine.ApplyTextTransform(s, mode) }
func expandTabs(s string) string               { return engine.ExpandTabs(s) }
func visibleText(n *Node) string               { return engine.VisibleText(n) }
func fallbackPageBackground(frags []Frag, width, viewportH, docH int) (Color, bool) {
	return engine.FallbackPageBackground(frags, width, viewportH, docH)
}

func darkForeground(c Color, cache map[Color]Color) Color {
	return engine.DarkForeground(c, cache)
}

func isFormControl(n *Node) bool            { return engine.IsFormControl(n) }
func isDisabledControl(n *Node) bool        { return engine.IsDisabledControl(n) }
func selectOptions(n *Node) []*Node         { return engine.SelectOptions(n) }
func optionValue(opt *Node) string          { return engine.OptionValue(opt) }
func selectedOptionValues(n *Node) []string { return engine.SelectedOptionValues(n) }
func inputValue(n *Node) string             { return engine.InputValue(n) }

func isReadOnlyControl(n *Node) bool     { return engine.IsReadOnlyControl(n) }
func maxLength(n *Node) int              { return engine.MaxLength(n) }
func optionLabel(opt *Node) string       { return engine.OptionLabel(opt) }
func isTextInput(n *Node) bool           { return engine.IsTextInput(n) }
func isRangeInput(n *Node) bool          { return engine.IsRangeInput(n) }
func layoutTextOverlaps(ly *Layout) bool { return engine.LayoutTextOverlaps(ly) }

func textareaLayoutFor(n *Node, boxW int) ([]string, int, int, bool) {
	return engine.TextareaLayoutFor(n, boxW)
}
func formLabel(n *Node) string                            { return engine.FormLabel(n) }
func inputCursorOffset(n *Node, label string) (int, bool) { return engine.InputCursorOffset(n, label) }
func rangeParams(n *Node) (min, max, step, value float64) { return engine.RangeParams(n) }
func formatRangeNumber(v float64) string                  { return engine.FormatRangeNumber(v) }
func isPositionedStyle(st *Style) bool                    { return engine.IsPositionedStyle(st) }
func nearestAnchor(n *Node) *Node                         { return engine.NearestAnchor(n) }

func parseGridTracks(raw string, avail int, font float64) []engine.GridTrack {
	return engine.ParseGridTracks(raw, avail, font)
}
func resolveGridTracks(tracks []engine.GridTrack, avail, gap int, content, minContent []int) []int {
	return engine.ResolveGridTracks(tracks, avail, gap, content, minContent)
}
func gridPlacement(raw string, cols int) (start, span int, explicit bool) {
	return engine.GridPlacement(raw, cols)
}

const maxImageRows = engine.MaxImageRows

func imageSize(n *Node, st *Style, maxW, capW int) (w, h int) {
	return engine.ImageSize(n, st, maxW, capW)
}
func darkBackground(c Color, cache map[Color]Color) Color { return engine.DarkBackground(c, cache) }

const (
	glyphCheckboxOn  = engine.GlyphCheckboxOn
	glyphCheckboxOff = engine.GlyphCheckboxOff
	glyphRadioOn     = engine.GlyphRadioOn
	glyphRadioOff    = engine.GlyphRadioOff
)

func distributeGrow(widths []int, grow []float64, maxTotals []int, remaining int) {
	engine.DistributeGrow(widths, grow, maxTotals, remaining)
}

func measureInlineText(s, whiteSpace string, sc int) (maxW, minW int) {
	return engine.MeasureInlineText(s, whiteSpace, sc)
}

func nearestForm(n *Node) *Node { return engine.NearestForm(n) }
func isFileInput(n *Node) bool  { return engine.IsFileInput(n) }
