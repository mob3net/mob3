package engine

// PDF text extraction: fonts, content-stream interpretation, and page line
// reconstruction.
//
// This is a text viewer, not a renderer. It reads the text-showing operators,
// recovers glyph positions from the PDF advances, and rebuilds each page as
// fixed-width lines so tables and columns keep their alignment. Vector art,
// images, shadings and transparency are ignored.

import (
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"golang.org/x/text/encoding/charmap"
)

// pdfCellPoints is one terminal cell in PDF points: 8 CSS px at 96 dpi.
const pdfCellPoints = 6.0

// pdfMatrix is a PDF transformation matrix [a b c d e f].
type pdfMatrix struct{ a, b, c, d, e, f float64 }

var pdfIdentity = pdfMatrix{a: 1, d: 1}

func (m pdfMatrix) mul(n pdfMatrix) pdfMatrix {
	return pdfMatrix{
		a: m.a*n.a + m.b*n.c,
		b: m.a*n.b + m.b*n.d,
		c: m.c*n.a + m.d*n.c,
		d: m.c*n.b + m.d*n.d,
		e: m.e*n.a + m.f*n.c + n.e,
		f: m.e*n.b + m.f*n.d + n.f,
	}
}

func (m pdfMatrix) apply(x, y float64) (float64, float64) {
	return m.a*x + m.c*y + m.e, m.b*x + m.d*y + m.f
}

// translate advances the matrix origin in its own coordinate space.
func (m *pdfMatrix) translate(x, y float64) {
	m.e += x*m.a + y*m.c
	m.f += x*m.b + y*m.d
}

func (m pdfMatrix) scale() float64 { return math.Hypot(m.a, m.b) }

func pdfMatrixFrom(v []any) (pdfMatrix, bool) {
	if len(v) < 6 {
		return pdfIdentity, false
	}
	return pdfMatrix{
		a: pdfFloat(v[0]), b: pdfFloat(v[1]),
		c: pdfFloat(v[2]), d: pdfFloat(v[3]),
		e: pdfFloat(v[4]), f: pdfFloat(v[5]),
	}, true
}

// pdfFont is the subset of a font resource needed for text positioning.
type pdfFont struct {
	name      string
	bold      bool
	italic    bool
	twoByte   bool
	widths    map[int]float64 // advance per glyph, in em
	defWidth  float64
	toUnicode map[int]string
	decode    func(code int) string
}

func (f *pdfFont) width(code int) float64 {
	if w, ok := f.widths[code]; ok && w > 0 {
		return w
	}
	return f.defWidth
}

// PDFPage is one leaf page plus the attributes it inherited.
type PDFPage struct {
	Number    int
	Resources pdfDict
	Content   []byte
}

// PDFRun is a positioned text fragment on a page.
type PDFRun struct {
	Text   string
	X, Y   float64 // baseline start in PDF user space (y grows up)
	W      float64 // advance width in user space
	Size   float64
	Bold   bool
	Italic bool
}

// PDFSpan is a run of text with one style inside a reconstructed line. Size is
// an explicit CSS font size in pixels (0 keeps the page's 14px default); large
// PDF headings use it to trigger the same scaled-text path as HTML headings.
type PDFSpan struct {
	Text   string
	Bold   bool
	Italic bool
	Size   int
}

// pages walks the page tree in document order.
func (d *PDFDoc) Pages() ([]*PDFPage, error) {
	root := d.root()
	if root == nil {
		return nil, errPDFInvalid
	}
	top := d.dict(root["Pages"])
	if top == nil {
		return nil, errPDFInvalid
	}
	var out []*PDFPage
	var walk func(node pdfDict, inh pdfInherited, depth int)
	walk = func(node pdfDict, inh pdfInherited, depth int) {
		if len(out) >= maxPDFPages {
			d.truncated = true
			return
		}
		if depth > maxPDFDepth || d.expired() {
			return
		}
		if v, ok := node["Resources"]; ok {
			if r := d.dict(v); r != nil {
				inh.Resources = r
			}
		}
		if v, ok := node["MediaBox"]; ok {
			if box := d.numberArray(v); len(box) == 4 {
				inh.MediaBox = box
			}
		}
		kids := d.array(node["Kids"])
		if d.nameOf(node["Type"]) == "Page" || len(kids) == 0 {
			p := &PDFPage{Number: len(out) + 1, Resources: inh.Resources}
			p.Content = d.pageContent(node)
			out = append(out, p)
			return
		}
		for _, kid := range kids {
			if kd := d.dict(kid); kd != nil {
				walk(kd, inh, depth+1)
			}
		}
	}
	walk(top, pdfInherited{}, 0)
	return out, nil
}

type pdfInherited struct {
	Resources pdfDict
	MediaBox  []float64
}

func (d *PDFDoc) numberArray(v any) []float64 {
	arr := d.array(v)
	out := make([]float64, 0, len(arr))
	for _, e := range arr {
		out = append(out, pdfFloat(d.resolve(e)))
	}
	return out
}

// pageContent concatenates the page's content streams.
func (d *PDFDoc) pageContent(node pdfDict) []byte {
	var parts [][]byte
	total := 0
	add := func(v any) {
		st := d.streamOf(v)
		if st == nil || total >= maxPDFDecoded {
			return
		}
		data, err := d.decodeStream(st)
		if err != nil {
			return
		}
		if len(data) > maxPDFDecoded-total {
			data = data[:maxPDFDecoded-total]
		}
		parts = append(parts, data)
		total += len(data)
	}
	if arr, ok := d.resolve(node["Contents"]).([]any); ok {
		for _, e := range arr {
			add(e)
		}
	} else {
		add(node["Contents"])
	}
	if len(parts) == 1 {
		return parts[0]
	}
	out := make([]byte, 0, total+len(parts))
	for _, p := range parts {
		out = append(out, p...)
		out = append(out, '\n')
	}
	return out
}

func (d *PDFDoc) streamOf(v any) *pdfStream {
	if v == nil {
		return nil
	}
	st, _ := d.resolve(v).(*pdfStream)
	return st
}

// extract returns the positioned text of one page.
func (d *PDFDoc) Extract(p *PDFPage) []PDFRun {
	ex := &pdfExtractor{
		d:  d,
		st: pdfTextState{tm: pdfIdentity, tlm: pdfIdentity, ctm: pdfIdentity, tz: 1},
	}
	fonts := d.loadFonts(p.Resources)
	xobjs := pdfDict(nil)
	if p.Resources != nil {
		xobjs = d.dict(p.Resources["XObject"])
	}
	ex.interpret(p.Content, fonts, xobjs, 0)
	ex.flush()
	if ex.limitHit {
		d.textLimit = true
	}
	return ex.runs
}

func (d *PDFDoc) loadFonts(res pdfDict) map[string]*pdfFont {
	out := map[string]*pdfFont{}
	if res == nil {
		return out
	}
	for name, v := range d.dict(res["Font"]) {
		if f := d.fontFromDict(v); f != nil {
			out[name] = f
		}
	}
	return out
}

// pdfTextState is the graphics and text state that affects text placement.
type pdfTextState struct {
	tm, tlm              pdfMatrix
	font                 *pdfFont
	size, tc, tw, tz, tl float64
	rise                 float64
	ctm                  pdfMatrix
}

type pdfExtractor struct {
	d    *PDFDoc
	st   pdfTextState
	hold []pdfTextState

	runs    []PDFRun
	buf     strings.Builder
	haveRun bool
	runX    float64
	runY    float64
	runW    float64
	runSize float64
	runBold bool
	runItal bool
	nextX   float64
	nextY   float64

	textLen  int
	limitHit bool
}

func (ex *pdfExtractor) push() {
	if len(ex.hold) < 64 {
		ex.hold = append(ex.hold, ex.st)
	}
}

func (ex *pdfExtractor) pop() {
	if n := len(ex.hold); n > 0 {
		ex.st = ex.hold[n-1]
		ex.hold = ex.hold[:n-1]
	}
}

func (ex *pdfExtractor) interpret(data []byte, fonts map[string]*pdfFont, xobjs pdfDict, depth int) {
	if depth > maxPDFDepth || len(data) == 0 || ex.limitHit {
		return
	}
	sc := newPDFScanner(data)
	var operands []any
	for {
		if ex.d.expired() {
			return
		}
		v, err := sc.contentValue()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		op, isOp := v.(pdfOp)
		if !isOp {
			if len(operands) < 32 {
				operands = append(operands, v)
			}
			continue
		}
		switch string(op) {
		case "q":
			ex.push()
		case "Q":
			ex.pop()
		case "cm":
			if m, ok := pdfMatrixFrom(operands); ok {
				ex.st.ctm = m.mul(ex.st.ctm)
			}
		case "BT":
			ex.st.tm = pdfIdentity
			ex.st.tlm = pdfIdentity
		case "ET":
			ex.flush()
		case "Tf":
			if len(operands) >= 2 {
				name, _ := operands[0].(pdfName)
				ex.st.font = fonts[string(name)]
				ex.st.size = pdfFloat(operands[1])
			}
		case "Tc":
			if len(operands) >= 1 {
				ex.st.tc = pdfFloat(operands[0])
			}
		case "Tw":
			if len(operands) >= 1 {
				ex.st.tw = pdfFloat(operands[0])
			}
		case "Tz":
			if len(operands) >= 1 {
				ex.st.tz = pdfFloat(operands[0]) / 100
				if ex.st.tz == 0 {
					ex.st.tz = 1
				}
			}
		case "TL":
			if len(operands) >= 1 {
				ex.st.tl = pdfFloat(operands[0])
			}
		case "Ts":
			if len(operands) >= 1 {
				ex.st.rise = pdfFloat(operands[0])
			}
		case "Td":
			if len(operands) >= 2 {
				ex.st.tlm.translate(pdfFloat(operands[0]), pdfFloat(operands[1]))
				ex.st.tm = ex.st.tlm
			}
		case "TD":
			if len(operands) >= 2 {
				ex.st.tl = -pdfFloat(operands[1])
				ex.st.tlm.translate(pdfFloat(operands[0]), pdfFloat(operands[1]))
				ex.st.tm = ex.st.tlm
			}
		case "Tm":
			if m, ok := pdfMatrixFrom(operands); ok {
				ex.st.tm = m
				ex.st.tlm = m
			}
		case "T*":
			ex.st.tlm.translate(0, -ex.st.tl)
			ex.st.tm = ex.st.tlm
		case "Tj":
			if len(operands) >= 1 {
				if s, ok := operands[0].(string); ok {
					ex.show(s)
				}
			}
		case "TJ":
			if len(operands) >= 1 {
				if arr, ok := operands[0].([]any); ok {
					for _, e := range arr {
						switch t := e.(type) {
						case string:
							ex.show(t)
						case float64:
							ex.adjust(-t / 1000)
						}
					}
				}
			}
		case "'":
			ex.st.tlm.translate(0, -ex.st.tl)
			ex.st.tm = ex.st.tlm
			if len(operands) >= 1 {
				if s, ok := operands[0].(string); ok {
					ex.show(s)
				}
			}
		case "\"":
			if len(operands) >= 3 {
				ex.st.tw = pdfFloat(operands[0])
				ex.st.tc = pdfFloat(operands[1])
			}
			ex.st.tlm.translate(0, -ex.st.tl)
			ex.st.tm = ex.st.tlm
			if len(operands) >= 3 {
				if s, ok := operands[2].(string); ok {
					ex.show(s)
				}
			}
		case "Do":
			ex.formXObject(operands, fonts, xobjs, depth)
		case "BI":
			sc.skipInlineImage()
		}
		operands = operands[:0]
	}
}

// formXObject interprets the content of a form XObject, which many documents
// use for headers, footers and table cells.
func (ex *pdfExtractor) formXObject(operands []any, fonts map[string]*pdfFont, xobjs pdfDict, depth int) {
	if len(operands) < 1 || xobjs == nil || depth >= maxPDFDepth {
		return
	}
	name, _ := operands[0].(pdfName)
	st := ex.d.streamOf(xobjs[string(name)])
	if st == nil || ex.d.nameOf(st.dict["Subtype"]) != "Form" {
		return
	}
	data, err := ex.d.decodeStream(st)
	if err != nil {
		return
	}
	save := ex.st
	if arr := ex.d.array(st.dict["Matrix"]); len(arr) >= 6 {
		if m, ok := pdfMatrixFrom(arr); ok {
			ex.st.ctm = m.mul(ex.st.ctm)
		}
	}
	sub := fonts
	subX := xobjs
	if res := ex.d.dict(st.dict["Resources"]); res != nil {
		sub = ex.d.loadFonts(res)
		if x := ex.d.dict(res["XObject"]); x != nil {
			subX = x
		}
	}
	holdLen := len(ex.hold)
	ex.interpret(data, sub, subX, depth+1)
	ex.flush()
	ex.st = save
	ex.hold = ex.hold[:holdLen]
}

// skipInlineImage consumes an inline image after the BI operator, including
// its binary payload.
func (p *pdfScanner) skipInlineImage() {
	for {
		if p.eof() {
			return
		}
		w := p.readWord()
		if w == "" {
			p.pos++
			continue
		}
		if w == "ID" {
			break
		}
	}
	if p.pos < len(p.s) && isPDFSpace(p.s[p.pos]) {
		p.pos++
	}
	for p.pos+1 < len(p.s) {
		if p.s[p.pos] == 'E' && p.s[p.pos+1] == 'I' {
			before := p.pos == 0 || isPDFSpace(p.s[p.pos-1])
			after := p.pos+2 >= len(p.s) || isPDFSpace(p.s[p.pos+2]) || isPDFDelim(p.s[p.pos+2])
			if before && after {
				p.pos += 2
				return
			}
		}
		p.pos++
	}
}

// show draws a PDF string, one code at a time.
func (ex *pdfExtractor) show(s string) {
	if s == "" {
		return
	}
	f := ex.st.font
	if f == nil {
		// Without a font, keep the pen moving so later runs stay placed.
		adv := 0.5 * ex.st.size * ex.st.tz
		for i := 0; i < len(s); i++ {
			ex.st.tm.translate(adv, 0)
		}
		return
	}
	if f.twoByte {
		for i := 0; i+1 < len(s); i += 2 {
			ex.glyph(f, int(s[i])<<8|int(s[i+1]))
			if ex.limitHit {
				return
			}
		}
		return
	}
	for i := 0; i < len(s); i++ {
		ex.glyph(f, int(s[i]))
		if ex.limitHit {
			return
		}
	}
}

// glyph advances the text matrix by one code and appends its text to the
// current run when it continues the previous one.
func (ex *pdfExtractor) glyph(f *pdfFont, code int) {
	text := f.decode(code)
	adv := (f.width(code)*ex.st.size + ex.st.tc) * ex.st.tz
	if code == 32 {
		adv += ex.st.tw * ex.st.tz
	}
	x, y := ex.st.tm.apply(0, ex.st.rise)
	ux, uy := ex.st.ctm.apply(x, y)
	size := math.Abs(ex.st.size * ex.st.tm.scale() * ex.st.ctm.scale())
	ex.st.tm.translate(adv, 0)
	nx, ny := ex.st.tm.apply(0, ex.st.rise)
	nux, nuy := ex.st.ctm.apply(nx, ny)
	if text == "" {
		if ex.haveRun {
			ex.nextX, ex.nextY = nux, nuy
		}
		return
	}
	if !ex.haveRun ||
		math.Abs(ux-ex.nextX) > 1.0 ||
		math.Abs(uy-ex.nextY) > 1.0 ||
		f.bold != ex.runBold ||
		f.italic != ex.runItal ||
		math.Abs(size-ex.runSize) > 0.6 {
		ex.flush()
		ex.runX, ex.runY, ex.runSize = ux, uy, size
		ex.runBold, ex.runItal = f.bold, f.italic
		ex.haveRun = true
	}
	ex.buf.WriteString(text)
	ex.runW += math.Hypot(nux-ux, nuy-uy)
	ex.textLen += len(text)
	if ex.textLen > maxPDFText {
		ex.limitHit = true
	}
	ex.nextX, ex.nextY = nux, nuy
}

// adjust moves the pen by a TJ offset expressed in thousandths of an em.
// A positive offset wide enough to be a word gap ends the current run, so line
// reconstruction can insert the space from the resulting gap; small kerning
// offsets keep the run together.
func (ex *pdfExtractor) adjust(v float64) {
	adv := v * ex.st.size * ex.st.tz
	if adv > math.Max(1.0, ex.st.size*0.15) {
		ex.flush()
	}
	ex.st.tm.translate(adv, 0)
	if ex.haveRun {
		x, y := ex.st.tm.apply(0, ex.st.rise)
		ex.nextX, ex.nextY = ex.st.ctm.apply(x, y)
	}
}

func (ex *pdfExtractor) flush() {
	if !ex.haveRun || ex.buf.Len() == 0 {
		ex.buf.Reset()
		ex.haveRun = false
		return
	}
	text := sanitizePDFText(ex.buf.String())
	ex.buf.Reset()
	ex.haveRun = false
	if strings.TrimSpace(text) == "" {
		return
	}
	ex.runs = append(ex.runs, PDFRun{
		Text: text, X: ex.runX, Y: ex.runY, W: ex.runW, Size: ex.runSize,
		Bold: ex.runBold, Italic: ex.runItal,
	})
	ex.runW = 0
}

func sanitizePDFText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == 0xFFFD:
			b.WriteRune('?')
		case r < 0x20 || (r >= 0x7f && r < 0xa0):
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// PDFPageLines groups runs into lines and turns them into styled spans with
// column padding, so tables stay aligned in the terminal. bodySize is the
// document's typical font size; runs clearly larger than it become headline
// spans and get the same treatment as large HTML headings. Vertical gaps wider
// than the page's own leading become blank lines, which restores the spacing
// after titles and between blocks that the PDF layout implies.
//
// Two-column prose is emitted column by column instead of row by row: a
// gutter that most lines respect splits the page into left and right sequences,
// so reading order follows the text rather than the printed page.
func PDFPageLines(runs []PDFRun, bodySize float64) []PDFSpan {
	if len(runs) == 0 {
		return nil
	}
	lines := pdfGroupLines(runs)
	var out []PDFSpan
	for _, seg := range pdfSegments(lines, bodySize) {
		if len(out) > 0 && seg.breakBefore {
			out = append(out, PDFSpan{Text: "\n"})
		}
		out = append(out, pdfFlowSpans(seg.lines, bodySize)...)
	}
	return out
}

// pdfGroupLines groups runs into visual lines, top to bottom.
func pdfGroupLines(runs []PDFRun) [][]PDFRun {
	sorted := make([]PDFRun, len(runs))
	copy(sorted, runs)
	sort.SliceStable(sorted, func(i, j int) bool {
		if math.Abs(sorted[i].Y-sorted[j].Y) > 2 {
			return sorted[i].Y > sorted[j].Y
		}
		return sorted[i].X < sorted[j].X
	})
	var lines [][]PDFRun
	for _, r := range sorted {
		if n := len(lines); n > 0 && math.Abs(lines[n-1][0].Y-r.Y) <= 2 {
			lines[n-1] = append(lines[n-1], r)
			continue
		}
		lines = append(lines, []PDFRun{r})
	}
	for _, line := range lines {
		sort.SliceStable(line, func(i, j int) bool { return line[i].X < line[j].X })
	}
	return lines
}

// pdfSegment is one run of lines emitted together: either a flow of full-width
// lines, or one half of a two-column band.
type pdfSegment struct {
	lines       [][]PDFRun
	breakBefore bool
}

// pdfSegments splits the page into flow and column segments. Full-width lines
// (headings, titles, rules) stay in flow and break a column band.
func pdfSegments(lines [][]PDFRun, bodySize float64) []pdfSegment {
	gutter, ok := pdfDetectGutter(lines)
	if !ok {
		return []pdfSegment{{lines: lines}}
	}
	var segs []pdfSegment
	var flow, band [][]PDFRun
	flushFlow := func() {
		if len(flow) > 0 {
			segs = append(segs, pdfSegment{lines: flow})
			flow = nil
		}
	}
	flushBand := func() {
		if len(band) == 0 {
			return
		}
		left, right, ok := pdfSplitBand(band, gutter)
		if !ok {
			flow = append(flow, band...)
			band = nil
			return
		}
		segs = append(segs, pdfSegment{lines: left})
		segs = append(segs, pdfSegment{lines: right, breakBefore: true})
		band = nil
	}
	maxGap := bodySize * 2.5
	if maxGap <= 0 {
		maxGap = 27.5
	}
	for _, line := range lines {
		if pdfCrossesGutter(line, gutter) {
			flushBand()
			flow = append(flow, line)
			continue
		}
		// A large vertical gap separates a header or colophon from the
		// columns below; keep it in flow rather than dragging it into a
		// column.
		if n := len(band); n > 0 && band[n-1][0].Y-line[0].Y > maxGap {
			flushBand()
		}
		flushFlow()
		band = append(band, line)
	}
	flushBand()
	flushFlow()
	return segs
}

// pdfGutterMin is the narrowest gutter between columns, in PDF points.
const pdfGutterMin = 9.0 // 1.5 terminal cells

// pdfDetectGutter finds a vertical gutter that most lines respect. Candidates
// come from the vertical coverage histogram of the page: a gutter is a narrow
// x-band crossed by few lines, with real text on both sides. Requiring dense
// text on each side rejects table columns and figure labels.
func pdfDetectGutter(lines [][]PDFRun) (float64, bool) {
	if len(lines) < 6 {
		return 0, false
	}
	minX, maxX := math.Inf(1), math.Inf(-1)
	for _, line := range lines {
		for _, r := range line {
			minX = math.Min(minX, r.X)
			maxX = math.Max(maxX, r.X+r.W)
		}
	}
	if !(maxX-minX >= 40) {
		return 0, false
	}
	bins := int(math.Ceil(maxX-minX)) + 1
	cover := make([]int, bins)
	for _, line := range lines {
		for _, r := range line {
			lo := int(r.X - minX)
			hi := int(r.X + r.W - minX)
			if lo < 0 {
				lo = 0
			}
			if hi >= bins {
				hi = bins - 1
			}
			for b := lo; b <= hi; b++ {
				cover[b]++
			}
		}
	}
	// A gutter may be crossed by a few full-width lines (titles, rules,
	// table rows), but its coverage stays far below the columns. The
	// threshold follows the page's median coverage so sparse pages work too.
	var covered []int
	for _, c := range cover {
		if c > 0 {
			covered = append(covered, c)
		}
	}
	sort.Ints(covered)
	limit := 2
	if len(covered) > 0 {
		if l := covered[len(covered)/2] / 2; l > limit {
			limit = l
		}
	}
	bestX, bestScore := 0.0, 0
	for b := 0; b < bins; b++ {
		if cover[b] > limit {
			continue
		}
		startBin := b
		for b < bins && cover[b] <= limit {
			b++
		}
		if b-startBin < int(pdfGutterMin) {
			continue
		}
		leftOK, rightOK := false, false
		for i := 0; i < startBin; i++ {
			if cover[i] > limit {
				leftOK = true
				break
			}
		}
		for i := b; i < bins; i++ {
			if cover[i] > limit {
				rightOK = true
				break
			}
		}
		if !leftOK || !rightOK {
			continue
		}
		x := minX + float64(startBin+b)/2
		left, right, ok := pdfSplitBand(lines, x)
		if !ok {
			continue
		}
		score := MinInt(len(left), len(right))
		if score > bestScore {
			bestScore, bestX = score, x
		}
	}
	if bestScore < 5 {
		return 0, false
	}
	return bestX, true
}

// pdfSplitLine divides a line's runs at x.
func pdfSplitLine(line []PDFRun, x float64) (left, right []PDFRun) {
	for _, run := range line {
		switch {
		case run.X+run.W <= x+0.5:
			left = append(left, run)
		case run.X >= x-0.5:
			right = append(right, run)
		default:
			left = append(left, run)
		}
	}
	return left, right
}

// pdfContiguous reports that runs sit next to each other without a wide gap, so
// a side is one text column rather than several table columns.
func pdfContiguous(runs []PDFRun) bool {
	for i := 1; i < len(runs); i++ {
		if runs[i].X-(runs[i-1].X+runs[i-1].W) >= pdfGutterMin {
			return false
		}
	}
	return true
}

func pdfCrossesGutter(line []PDFRun, x float64) bool {
	for _, r := range line {
		if r.X < x && r.X+r.W > x {
			return true
		}
	}
	return false
}

// pdfSplitBand turns a band of lines into left and right column sequences when
// it really is two columns of prose.
func pdfSplitBand(band [][]PDFRun, x float64) (left, right [][]PDFRun, ok bool) {
	leftN, rightN := 0, 0
	var leftAll, rightAll [][]PDFRun
	for _, line := range band {
		if pdfCrossesGutter(line, x) {
			// Full-width lines belong to flow, not to either column.
			continue
		}
		l, r := pdfSplitLine(line, x)
		if len(l) > 0 {
			leftN++
			leftAll = append(leftAll, l)
		}
		if len(r) > 0 {
			rightN++
			rightAll = append(rightAll, r)
		}
	}
	if leftN < 5 || rightN < 5 {
		return nil, nil, false
	}
	// Prose columns fill their width and have no internal column gaps; table
	// columns and figure labels fail one of the two checks.
	if !pdfColumnDense(leftAll) || !pdfColumnDense(rightAll) {
		return nil, nil, false
	}
	if pdfColumnGappy(leftAll) || pdfColumnGappy(rightAll) {
		return nil, nil, false
	}
	return leftAll, rightAll, true
}

// pdfColumnGappy reports that many lines contain a wide internal gap, which
// means the side holds more than one table column.
func pdfColumnGappy(groups [][]PDFRun) bool {
	gappy := 0
	for _, g := range groups {
		if !pdfContiguous(g) {
			gappy++
		}
	}
	return gappy*5 > len(groups)
}

// pdfColumnDense reports that the lines fill most of their column's width, as
// wrapped prose does. Sparse groups are more likely to be table cells or
// figure labels.
func pdfColumnDense(groups [][]PDFRun) bool {
	if len(groups) < 5 {
		return false
	}
	minStart, maxEnd := math.Inf(1), math.Inf(-1)
	widths := make([]float64, 0, len(groups))
	for _, g := range groups {
		lo := g[0].X
		hi := g[len(g)-1].X + g[len(g)-1].W
		minStart = math.Min(minStart, lo)
		maxEnd = math.Max(maxEnd, hi)
		widths = append(widths, hi-lo)
	}
	span := maxEnd - minStart
	if span < 72 { // 12 terminal cells
		return false
	}
	// The median ignores short lines (headings, list items, figure labels) that
	// a column of prose also contains, while table cells and key labels stay
	// well below the threshold.
	sort.Float64s(widths)
	return widths[len(widths)/2] >= 0.6*span
}

// pdfFlowSpans emits one contiguous sequence of lines with gap blank lines.
func pdfFlowSpans(lines [][]PDFRun, bodySize float64) []PDFSpan {
	leading := pdfLeading(lines, bodySize)
	var out []PDFSpan
	prevY := 0.0
	prevHeadline := false
	for i, line := range lines {
		if i > 0 && leading > 0 {
			extra := int(math.Round((prevY-line[0].Y)/leading)) - 1
			if prevHeadline {
				// A headline line already occupies two rows, so it supplies one
				// row of the gap by itself.
				extra--
			}
			if extra > 6 {
				extra = 6
			}
			for j := 0; j < extra; j++ {
				out = append(out, PDFSpan{Text: "\n"})
			}
		}
		out = append(out, pdfLineSpans(line, bodySize)...)
		out = append(out, PDFSpan{Text: "\n"})
		prevY = line[0].Y
		prevHeadline = pdfLineHeadline(line, bodySize)
	}
	return out
}

// pdfLineHeadline reports whether any run on the line is large enough to be a
// headline span.
func pdfLineHeadline(runs []PDFRun, bodySize float64) bool {
	if bodySize <= 0 {
		return false
	}
	for _, r := range runs {
		if r.Size >= bodySize*pdfHeadlineRatio {
			return true
		}
	}
	return false
}

// pdfLeading estimates the page's normal baseline advance. Body lines are the
// large majority, so the lower quartile ignores gaps around headings,
// headers and page numbers. Two columns whose baselines are offset by half a
// line would drag the quartile below the real leading, so the body font size
// provides a floor.
func pdfLeading(lines [][]PDFRun, bodySize float64) float64 {
	leading := 0.0
	if len(lines) >= 2 {
		deltas := make([]float64, 0, len(lines)-1)
		for i := 0; i+1 < len(lines); i++ {
			d := lines[i][0].Y - lines[i+1][0].Y
			if d > 0.5 && d < 500 {
				deltas = append(deltas, d)
			}
		}
		if len(deltas) > 0 {
			sort.Float64s(deltas)
			leading = deltas[len(deltas)/4]
		}
	}
	if floor := bodySize * 1.1; floor > leading {
		leading = floor
	}
	return leading
}

func pdfLineSpans(runs []PDFRun, bodySize float64) []PDFSpan {
	left := runs[0].X
	var out []PDFSpan
	add := func(text string, bold, italic bool, size int) {
		if text == "" {
			return
		}
		if n := len(out); n > 0 && out[n-1].Bold == bold && out[n-1].Italic == italic && out[n-1].Size == size {
			out[n-1].Text += text
			return
		}
		out = append(out, PDFSpan{Text: text, Bold: bold, Italic: italic, Size: size})
	}
	col := 0
	endX := runs[0].X
	prevSpace := false
	for i, r := range runs {
		if i > 0 {
			// Text drawn twice at a sub-point offset (fake bold, shadows) is
			// invisible on screen; keep the first copy and drop the overlap.
			// The tolerance allows the small advance overshoot that separate
			// runs for right-aligned values commonly have.
			if r.X < endX-3.0 {
				continue
			}
			// Only a real gap in user space becomes padding. Comparing run
			// widths in points avoids inventing a space per glyph when the
			// text is wider in cells than in points.
			if gap := r.X - endX; gap > 1.0 {
				target := int(math.Round((r.X - left) / pdfCellPoints))
				if target < 0 {
					target = 0
				}
				switch {
				case target > col+1 && gap > pdfCellPoints:
					// A jump of at least one cell: pad to the column so
					// tables and multi-column layouts stay aligned.
					pad := target - col
					if pad > 160 {
						pad = 160
					}
					add(strings.Repeat(" ", pad), false, false, 0)
					col = target
				case !prevSpace && gap > math.Max(1.0, 0.15*r.Size):
					// An inter-word gap that does not span a whole cell. A
					// trailing space glyph already supplies the separator, so
					// this only fires when the PDF omitted it.
					add(" ", false, false, 0)
					col++
				}
			}
		}
		add(r.Text, r.Bold, r.Italic, PDFSpanSize(r.Size, bodySize))
		prevSpace = strings.HasSuffix(r.Text, " ")
		col += StringWidth(r.Text)
		width := r.W
		if width <= 0 {
			width = float64(StringWidth(r.Text)) * pdfCellPoints
		}
		endX = r.X + width
		if col > 400 {
			break
		}
	}
	// Trim trailing spaces from the final span.
	for len(out) > 0 {
		last := &out[len(out)-1]
		last.Text = strings.TrimRight(last.Text, " ")
		if last.Text != "" {
			break
		}
		out = out[:len(out)-1]
	}
	return out
}

// fontFromDict builds the text-relevant view of a font resource.
func (d *PDFDoc) fontFromDict(v any) *pdfFont {
	dict := d.dict(v)
	if dict == nil {
		return nil
	}
	f := &pdfFont{widths: map[int]float64{}, defWidth: 0.5}
	subtype := d.nameOf(dict["Subtype"])
	f.name = d.nameOf(dict["BaseFont"])
	if subtype == "Type0" {
		f.twoByte = true
		f.defWidth = 1
		desc := d.dict(firstPDFElem(d.array(dict["DescendantFonts"])))
		if desc != nil {
			if dw := d.number(desc["DW"]); dw > 0 {
				f.defWidth = dw / 1000
			}
			f.widths = d.cidWidths(desc["W"])
			if f.name == "" {
				f.name = d.nameOf(desc["BaseFont"])
			}
		}
	} else {
		first := d.integer(dict["FirstChar"])
		for i, w := range d.array(dict["Widths"]) {
			f.widths[first+i] = pdfFloat(d.resolve(w)) / 1000
		}
		if fdesc := d.dict(dict["FontDescriptor"]); fdesc != nil {
			if mw := d.number(fdesc["MissingWidth"]); mw > 0 {
				f.defWidth = mw / 1000
			}
			flags := int(d.number(fdesc["Flags"]))
			f.italic = flags&64 != 0
			f.bold = flags&(1<<18) != 0
			if f.name == "" {
				f.name = d.nameOf(fdesc["FontName"])
			}
		}
	}
	lower := strings.ToLower(f.name)
	if strings.Contains(lower, "bold") || strings.Contains(lower, "black") ||
		strings.Contains(lower, "heavy") || strings.Contains(lower, "semibold") {
		f.bold = true
	}
	if strings.Contains(lower, "italic") || strings.Contains(lower, "oblique") {
		f.italic = true
	}
	if st := d.streamOf(dict["ToUnicode"]); st != nil {
		if data, err := d.decodeStream(st); err == nil {
			f.toUnicode = parseToUnicode(data)
		}
	}
	f.decode = d.fontDecoder(dict, f)
	return f
}

func firstPDFElem(v []any) any {
	if len(v) == 0 {
		return nil
	}
	return v[0]
}

// cidWidths parses the /W array of a CID font.
func (d *PDFDoc) cidWidths(v any) map[int]float64 {
	out := map[int]float64{}
	arr := d.array(v)
	for i := 0; i+1 < len(arr); i++ {
		first := int(d.number(arr[i]))
		if list, ok := d.resolve(arr[i+1]).([]any); ok {
			for j, w := range list {
				out[first+j] = d.number(w) / 1000
			}
			i++
			continue
		}
		if i+2 >= len(arr) {
			break
		}
		last := int(d.number(arr[i+1]))
		width := d.number(arr[i+2]) / 1000
		if last >= first && last-first <= 65535 {
			for c := first; c <= last; c++ {
				out[c] = width
			}
		}
		i += 2
	}
	return out
}

// fontDecoder returns the code-to-text mapping for a font: ToUnicode first,
// then the declared encoding, then printable ASCII.
func (d *PDFDoc) fontDecoder(dict pdfDict, f *pdfFont) func(int) string {
	base := pdfBaseEncoding("")
	if enc := d.resolve(dict["Encoding"]); enc != nil {
		switch t := enc.(type) {
		case pdfName:
			base = pdfBaseEncoding(string(t))
		case pdfDict:
			base = pdfBaseEncoding(d.nameOf(t["BaseEncoding"]))
			code := 0
			for _, v := range d.array(t["Differences"]) {
				switch rv := d.resolve(v).(type) {
				case float64:
					code = int(rv)
				case pdfName:
					if r, ok := pdfGlyphRune(string(rv)); ok {
						base[code] = string(r)
					}
					code++
				}
			}
		}
	}
	return func(code int) string {
		if s, ok := f.toUnicode[code]; ok && s != "" {
			return s
		}
		if f.twoByte {
			return ""
		}
		if s, ok := base[code]; ok {
			return s
		}
		if code >= 32 && code < 127 {
			return string(rune(code))
		}
		return ""
	}
}

// pdfBaseEncoding returns a code-to-text table for a simple font encoding.
// Standard and PDFDoc encodings are approximated with Windows-1252, which
// matches their printable ASCII range.
func pdfBaseEncoding(name string) map[int]string {
	m := make(map[int]string, 256)
	cm := charmap.Windows1252
	if name == "MacRomanEncoding" {
		cm = charmap.Macintosh
	}
	for i := 0; i < 256; i++ {
		r := cm.DecodeByte(byte(i))
		if r == 0 || r == 0xFFFD {
			continue
		}
		m[i] = string(r)
	}
	return m
}

// pdfGlyphRune maps a glyph name to a rune for /Differences arrays.
func pdfGlyphRune(name string) (rune, bool) {
	if r, ok := pdfGlyphNames[name]; ok {
		return r, true
	}
	if strings.HasPrefix(name, "uni") && len(name) >= 7 {
		if v, err := strconv.ParseUint(name[3:7], 16, 32); err == nil {
			return rune(v), true
		}
	}
	if len(name) > 1 && name[0] == 'u' {
		if v, err := strconv.ParseUint(name[1:], 16, 32); err == nil && v <= 0x10FFFF {
			return rune(v), true
		}
	}
	return 0, false
}

// pdfGlyphNames covers the glyph names seen in Differences arrays of simple
// fonts, including the standard Latin set and common punctuation.
var pdfGlyphNames = map[string]rune{
	"space": ' ', "exclam": '!', "quotedbl": '"', "numbersign": '#',
	"dollar": '$', "percent": '%', "ampersand": '&', "quotesingle": '\'',
	"parenleft": '(', "parenright": ')', "asterisk": '*', "plus": '+',
	"comma": ',', "hyphen": '-', "period": '.', "slash": '/',
	"zero": '0', "one": '1', "two": '2', "three": '3', "four": '4',
	"five": '5', "six": '6', "seven": '7', "eight": '8', "nine": '9',
	"colon": ':', "semicolon": ';', "less": '<', "equal": '=',
	"greater": '>', "question": '?', "at": '@',
	"A": 'A', "B": 'B', "C": 'C', "D": 'D', "E": 'E', "F": 'F', "G": 'G',
	"H": 'H', "I": 'I', "J": 'J', "K": 'K', "L": 'L', "M": 'M', "N": 'N',
	"O": 'O', "P": 'P', "Q": 'Q', "R": 'R', "S": 'S', "T": 'T', "U": 'U',
	"V": 'V', "W": 'W', "X": 'X', "Y": 'Y', "Z": 'Z',
	"bracketleft": '[', "backslash": '\\', "bracketright": ']',
	"asciicircum": '^', "underscore": '_', "grave": '`',
	"a": 'a', "b": 'b', "c": 'c', "d": 'd', "e": 'e', "f": 'f', "g": 'g',
	"h": 'h', "i": 'i', "j": 'j', "k": 'k', "l": 'l', "m": 'm', "n": 'n',
	"o": 'o', "p": 'p', "q": 'q', "r": 'r', "s": 's', "t": 't', "u": 'u',
	"v": 'v', "w": 'w', "x": 'x', "y": 'y', "z": 'z',
	"braceleft": '{', "bar": '|', "braceright": '}', "asciitilde": '~',
	"quoteleft": '\u2018', "quoteright": '\u2019',
	"quotedblleft": '\u201c', "quotedblright": '\u201d',
	"quotesinglbase": '\u201a', "quotedblbase": '\u201e',
	"guillemotleft": '\u00ab', "guillemotright": '\u00bb',
	"guilsinglleft": '\u2039', "guilsinglright": '\u203a',
	"endash": '\u2013', "emdash": '\u2014', "bullet": '\u2022',
	"ellipsis": '\u2026', "dagger": '\u2020', "daggerdbl": '\u2021',
	"periodcentered": '\u00b7', "section": '\u00a7', "paragraph": '\u00b6',
	"copyright": '\u00a9', "registered": '\u00ae', "trademark": '\u2122',
	"degree": '\u00b0', "plusminus": '\u00b1', "multiply": '\u00d7',
	"divide": '\u00f7', "minus": '\u2212', "fraction": '\u2044',
	"sterling": '\u00a3', "yen": '\u00a5', "cent": '\u00a2',
	"Euro": '\u20ac', "currency": '\u00a4', "florin": '\u0192',
	"nbspace": '\u00a0', "sfthyphen": '\u00ad',
	"germandbls": '\u00df', "ae": '\u00e6', "AE": '\u00c6',
	"oe": '\u0153', "OE": '\u0152', "oslash": '\u00f8', "Oslash": '\u00d8',
	"aring": '\u00e5', "Aring": '\u00c5', "ccedilla": '\u00e7',
	"Ccedilla": '\u00c7', "eth": '\u00f0', "Eth": '\u00d0',
	"thorn": '\u00fe', "Thorn": '\u00de',
	"fi": '\ufb01', "fl": '\ufb02', "ff": '\ufb00',
	"ffi": '\ufb03', "ffl": '\ufb04',
	"dotlessi": '\u0131', "lslash": '\u0142', "Lslash": '\u0141',
	"exclamdbl": '\u203c', "perthousand": '\u2030',
}

// parseToUnicode reads the bfchar and bfrange sections of a ToUnicode CMap.
func parseToUnicode(data []byte) map[int]string {
	out := map[int]string{}
	sc := newPDFScanner(data)
	for !sc.eof() {
		sc.skipSpace()
		if sc.eof() {
			break
		}
		word := sc.readWord()
		switch word {
		case "beginbfchar":
			sc.consume("beginbfchar")
			for i := 0; i < 100000; i++ {
				sc.skipSpace()
				if sc.eof() || sc.peekWord() == "endbfchar" {
					break
				}
				src, err := sc.object()
				if err != nil {
					break
				}
				dst, err := sc.object()
				if err != nil {
					break
				}
				out[pdfCodeValue(src)] = pdfUnicodeValue(dst)
			}
			sc.consume("endbfchar")
		case "beginbfrange":
			sc.consume("beginbfrange")
			for i := 0; i < 100000; i++ {
				sc.skipSpace()
				if sc.eof() || sc.peekWord() == "endbfrange" {
					break
				}
				loObj, err := sc.object()
				if err != nil {
					break
				}
				hiObj, err := sc.object()
				if err != nil {
					break
				}
				dstObj, err := sc.object()
				if err != nil {
					break
				}
				lo, hi := pdfCodeValue(loObj), pdfCodeValue(hiObj)
				if hi < lo || hi-lo > 65535 {
					continue
				}
				if arr, ok := dstObj.([]any); ok {
					for i, d := range arr {
						if lo+i > hi {
							break
						}
						out[lo+i] = pdfUnicodeValue(d)
					}
					continue
				}
				base := pdfUnicodeValue(dstObj)
				runes := []rune(base)
				if len(runes) == 0 {
					continue
				}
				prefix := string(runes[:len(runes)-1])
				for c := lo; c <= hi; c++ {
					out[c] = prefix + string(runes[len(runes)-1]+rune(c-lo))
				}
			}
			sc.consume("endbfrange")
		default:
			if word == "" {
				// A delimiter that starts an unrelated object; skip it.
				sc.pos++
			}
		}
	}
	return out
}

func pdfCodeValue(v any) int {
	s, ok := v.(string)
	if !ok {
		return int(pdfFloat(v))
	}
	n := 0
	for i := 0; i < len(s) && i < 4; i++ {
		n = n<<8 | int(s[i])
	}
	return n
}

func pdfUnicodeValue(v any) string {
	s, ok := v.(string)
	if !ok || s == "" {
		return ""
	}
	if len(s)%2 != 0 {
		var b strings.Builder
		for i := 0; i < len(s); i++ {
			b.WriteRune(rune(s[i]))
		}
		return b.String()
	}
	units := make([]uint16, 0, len(s)/2)
	for i := 0; i+1 < len(s); i += 2 {
		units = append(units, uint16(s[i])<<8|uint16(s[i+1]))
	}
	return string(utf16.Decode(units))
}

const (
	// pdfBodyPx is the CSS font size used for ordinary PDF text.
	pdfBodyPx = 14
	// pdfHeadlinePx is the size given to clearly larger PDF runs, matching the
	// HTML heading size where layout switches to two rows and kitty can scale
	// the glyphs.
	pdfHeadlinePx = 32

	// pdfHeadlineRatio is how much larger than the body text a run must be to
	// count as a heading.
	pdfHeadlineRatio = 1.25
)

// PDFHeadlinePx is the size given to clearly larger PDF runs.
const PDFHeadlinePx = pdfHeadlinePx

// PDFSpanSize maps a PDF font size to an explicit span size. Runs within a
// quarter of the body size keep the default; clearly larger runs use the
// headline size so they take two rows and scale in kitty.
func PDFSpanSize(size, bodySize float64) int {
	if bodySize > 0 && size >= bodySize*pdfHeadlineRatio {
		return pdfHeadlinePx
	}
	return 0
}
