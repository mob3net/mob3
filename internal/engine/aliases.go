package engine

import (
	"image"
	"net/url"
	"strings"
)

func readableFG(fg, bg Color) Color { return ReadableFG(fg, bg) }

type layouter = Layouter

const fragText = FragText
const fragFill = FragFill
const fragImage = FragImage
const fragLink = FragLink

func maxInt(a, b int) int { return MaxInt(a, b) }

func runeWidth(r rune) int { return RuneWidth(r) }

func isWideRune(r rune) bool { return IsWideRune(r) }

func stringWidth(s string) int { return StringWidth(s) }

func truncateWidth(s string, max int) string { return TruncateWidth(s, max) }

func padWidth(s string, w int) string { return PadWidth(s, w) }

func fitWidth(s string, w int) string { return FitWidth(s, w) }

func applyTextTransform(s, mode string) string { return ApplyTextTransform(s, mode) }

func expandTabs(s string) string { return ExpandTabs(s) }

func visibleText(n *Node) string { return VisibleText(n) }

func fallbackPageBackground(frags []Frag, width, viewportH, docH int) (Color, bool) {
	return FallbackPageBackground(frags, width, viewportH, docH)
}

func isFormControl(n *Node) bool { return IsFormControl(n) }

func isDisabledControl(n *Node) bool { return IsDisabledControl(n) }

func selectOptions(n *Node) []*Node { return SelectOptions(n) }

func optionValue(opt *Node) string { return OptionValue(opt) }

func selectedOptionValues(n *Node) []string { return SelectedOptionValues(n) }

func inputValue(n *Node) string { return InputValue(n) }

func isReadOnlyControl(n *Node) bool { return IsReadOnlyControl(n) }

func maxLength(n *Node) int { return MaxLength(n) }

func optionLabel(opt *Node) string { return OptionLabel(opt) }

func isTextInput(n *Node) bool { return IsTextInput(n) }

func isRangeInput(n *Node) bool { return IsRangeInput(n) }

func layoutTextOverlaps(ly *Layout) bool { return LayoutTextOverlaps(ly) }

func textareaLayoutFor(n *Node, boxW int) ([]string, int, int, bool) {
	return TextareaLayoutFor(n, boxW)
}

func formLabel(n *Node) string { return FormLabel(n) }

func inputCursorOffset(n *Node, label string) (int, bool) { return InputCursorOffset(n, label) }

func rangeParams(n *Node) (min, max, step, value float64) { return RangeParams(n) }

func formatRangeNumber(v float64) string { return FormatRangeNumber(v) }

func isPositionedStyle(st *Style) bool { return IsPositionedStyle(st) }

func nearestAnchor(n *Node) *Node { return NearestAnchor(n) }

const glyphCheckboxOn = GlyphCheckboxOn
const glyphCheckboxOff = GlyphCheckboxOff
const glyphRadioOn = GlyphRadioOn
const glyphRadioOff = GlyphRadioOff

func nearestForm(n *Node) *Node { return NearestForm(n) }

func isFileInput(n *Node) bool { return IsFileInput(n) }

type scaledRun = ScaledRun

func reuseGrid(g *Grid, w, h int, fg, bg Color) *Grid { return ReuseGrid(g, w, h, fg, bg) }

func blitGrid(dst, src *Grid, y int) { BlitGrid(dst, src, y) }

func stripTerminalControls(s string) string { return StripTerminalControls(s) }

func isSVG(ctype string, data []byte) bool { return IsSVG(ctype, data) }

func decodeSVG(data []byte) (image.Image, error) {
	return DecodeSVG(data)
}

func paintImage(g *Grid, img image.Image, crop image.Rectangle, x, y, w, h, rowStart, totalRows int) {
	PaintImage(g, img, crop, x, y, w, h, rowStart, totalRows)
}

type pdfDoc = PDFDoc

type pdfPage = PDFPage

type pdfRun = PDFRun

type pdfSpan = PDFSpan

func parsePDF(data []byte) (*pdfDoc, error) { return ParsePDF(data) }

func pdfPageLines(runs []pdfRun, bodySize float64) []pdfSpan {
	return PDFPageLines(runs, bodySize)
}

func looksLikePDF(mediaType string, data []byte) bool { return LooksLikePDF(mediaType, data) }

func buildPDFDocument(data []byte) *PDFDocument { return BuildPDFDocument(data) }

func pdfPageHTML(doc *PDFDocument, u *url.URL) string { return PDFPageHTML(doc, u) }

func pdfSpansText(spans []pdfSpan) string { return PDFSpansText(spans) }

func pdfDisplayName(u *url.URL) string { return PDFDisplayName(u) }

func pdfTextString(s string) string { return PDFTextString(s) }

func pdfPageBox(pages []PDFPageText) (rows, cells int) { return PDFPageBox(pages) }

func pdfPageRows(spans []pdfSpan) int { return PDFPageRows(spans) }

func newJSRuntime(b *Session, page *Page) *jsRuntime {
	j := NewJSRuntime(newJSHost(b), page)
	if b != nil {
		j.SetMemLimit(b.JSMemLimit)
	}
	return j
}

func setAttr(n *Node, name, value string) { SetAttr(n, name, value) }

func removeAttr(n *Node, name string) { RemoveAttr(n, name) }

func serializeNode(b *strings.Builder, n *Node) { SerializeNode(b, n) }

func textEscape(s string) string { return TextEscape(s) }

func attrValue(n *Node, name string) string { return AttrValue(n, name) }

func setLayoutZoom(z float64) {
	SetZoom(z)
	layoutZoom = Zoom()
	cellW = CellW()
	cellH = CellH()
}

func parseColor(v string, current Color) (Color, bool) { return ParseColor(v, current) }

func opaque(c Color) bool { return c.Opaque() }

func clamp(v, lo, hi float64) float64 { return Clamp(v, lo, hi) }

func splitFields(s string) []string { return SplitFields(s) }

func splitTopLevel(s string, sep byte) []string { return SplitTopLevel(s, sep) }

func px(v float64) Length { return PX(v) }

func pct(v float64) Length { return Pct(v) }

func parseLength(v string, fontPx float64) Length { return ParseLength(v, fontPx) }

func parseLengthSimple(v string) Length { return ParseLengthSimple(v) }

func parseFontSize(v string, parentPx float64) (float64, bool) {
	return ParseFontSize(v, parentPx)
}

func finiteNumber(f float64) bool { return FiniteNumber(f) }

func nonFiniteLength(val string) bool { return NonFiniteLength(val) }

func lengthProperty(prop string) bool { return LengthProperty(prop) }

func resolveLengthLH(l *Length, linePx float64) { ResolveLengthLH(l, linePx) }

func resolveLengthREM(l *Length, rootPx float64) {
	ResolveLengthREM(l, rootPx)
}

func resolveViewportLength(l *Length, vw, vh float64) {
	ResolveViewportLength(l, vw, vh)
}

const startPageHTML = StartPageHTML

func imageSource(n *Node, viewportPx int) string { return ImageSource(n, viewportPx) }

func pickSrcset(s string, preferred int) string { return PickSrcset(s, preferred) }

func imageConfigOK(cfg image.Config) bool { return ImageConfigOK(cfg) }

func safeDecodeConfig(data []byte) (image.Config, error) { return SafeDecodeConfig(data) }

func safeDecodeImage(data []byte) (image.Image, error) { return SafeDecodeImage(data) }

func collectImportMap(root *Node) map[string]string { return CollectImportMap(root) }

func blockedPageHTML(rawURL, reason string, status int) string {
	return BlockedPageHTML(rawURL, reason, status)
}

func aboutPageName(u *url.URL) string { return AboutPageName(u) }

func helpPageHTML() string { return HelpPageHTML(BuildVersion) }

func historyPageHTML(urls []string, current int) string {
	return HistoryPageHTML(urls, current)
}

func SetLayoutZoom(z float64) { SetZoom(z) }
