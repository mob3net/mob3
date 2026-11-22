package engine

// PDF object model, cross-reference loading and stream filters.
//
// The reader is deliberately small: it parses the document structure and the
// text operators of content streams well enough to extract readable text, and
// reports everything else (encryption, image codecs, exotic filters) as
// unsupported instead of pretending to render the page. It never writes to
// disk and treats its input as hostile: every stream, object and page walk is
// bounded and every decode failure degrades to a parse error for that object.

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

const (
	// maxResourceSize is the response size cap shared with the loader.
	maxResourceSize = 12 << 20
	// maxPDFPages caps how many pages one document contributes to the view.
	maxPDFPages = 500
	// maxPDFObjects bounds object parsing and the object cache.
	maxPDFObjects = 500_000
	// maxPDFStreamSize bounds one raw stream, matching the resource cap.
	maxPDFStreamSize = maxResourceSize
	// maxPDFDecoded bounds a decoded stream so filters cannot inflate a small
	// input into gigabytes.
	maxPDFDecoded = 32 << 20
	// maxPDFText bounds the extracted text kept for one document.
	maxPDFText = 4 << 20
	// maxPDFDepth bounds page-tree and form-XObject descent.
	maxPDFDepth = 16
	// pdfParseTimeout bounds total parsing work for one document.
	pdfParseTimeout = 4 * time.Second
)

var (
	errPDFInvalid     = errors.New("invalid PDF")
	errPDFNoXref      = errors.New("PDF cross-reference is missing")
	errPDFUnsupported = errors.New("unsupported PDF feature")
	errPDFTooLarge    = errors.New("PDF resource limit exceeded")
)

// pdfName is a PDF name object without its leading slash.
type pdfName string

// pdfRef is an indirect reference to an object.
type pdfRef struct{ num, gen int }

// pdfDict is a PDF dictionary keyed by decoded name.
type pdfDict map[string]any

// pdfStream is a stream object: a dictionary plus its raw bytes.
type pdfStream struct {
	dict pdfDict
	raw  []byte
}

// xrefEntry locates one object: directly at an offset or inside an object
// stream.
type xrefEntry struct {
	kind int // 0 free, 1 offset, 2 in object stream
	off  int64
	stm  int
	idx  int
}

// PDFDoc is one parsed PDF document.
type PDFDoc struct {
	data      []byte
	xref      map[int]xrefEntry
	cache     map[int]any
	objStms   map[int]map[int]any
	loading   map[int]bool
	trailer   pdfDict
	objects   int
	encrypted bool
	// truncated reports that the page cap stopped the page-tree walk, and
	// textLimit that the extracted-text budget stopped a page.
	truncated bool
	textLimit bool
	deadline  time.Time
}

func (d *PDFDoc) expired() bool {
	return !d.deadline.IsZero() && time.Now().After(d.deadline)
}

func isPDFSpace(c byte) bool {
	switch c {
	case 0, 9, 10, 12, 13, 32:
		return true
	}
	return false
}

func isPDFDelim(c byte) bool {
	switch c {
	case '(', ')', '<', '>', '[', ']', '{', '}', '/', '%':
		return true
	}
	return false
}

func isPDFDigit(c byte) bool { return c >= '0' && c <= '9' }

func hexVal(c byte) (int, bool) {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0'), true
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10, true
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10, true
	}
	return 0, false
}

// pdfFloat converts a parsed numeric object to a float. Indirect references
// must be resolved by the caller.
func pdfFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0
}

// pdfScanner walks PDF tokens. It doubles as the content-stream scanner, where
// bare keywords are returned as pdfOp operands.
type pdfScanner struct {
	s     []byte
	pos   int
	depth int
}

func newPDFScanner(s []byte) *pdfScanner { return &pdfScanner{s: s} }

func (p *pdfScanner) eof() bool { return p.pos >= len(p.s) }

func (p *pdfScanner) skipSpace() {
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		switch {
		case isPDFSpace(c):
			p.pos++
		case c == '%':
			for p.pos < len(p.s) && p.s[p.pos] != '\n' && p.s[p.pos] != '\r' {
				p.pos++
			}
		default:
			return
		}
	}
}

func (p *pdfScanner) preview() string {
	end := p.pos + 12
	if end > len(p.s) {
		end = len(p.s)
	}
	return string(p.s[p.pos:end])
}

// readWord reads a bare keyword or operator. It never reads across a
// delimiter, so "R]" yields "R".
func (p *pdfScanner) readWord() string {
	p.skipSpace()
	start := p.pos
	for p.pos < len(p.s) && !isPDFSpace(p.s[p.pos]) && !isPDFDelim(p.s[p.pos]) {
		p.pos++
	}
	return string(p.s[start:p.pos])
}

func (p *pdfScanner) peekWord() string {
	save := p.pos
	w := p.readWord()
	p.pos = save
	return w
}

func (p *pdfScanner) consume(word string) bool {
	save := p.pos
	if p.readWord() == word {
		return true
	}
	p.pos = save
	return false
}

func (p *pdfScanner) readInt() (int, error) {
	p.skipSpace()
	start := p.pos
	if p.pos < len(p.s) && (p.s[p.pos] == '-' || p.s[p.pos] == '+') {
		p.pos++
	}
	for p.pos < len(p.s) && isPDFDigit(p.s[p.pos]) {
		p.pos++
	}
	if start == p.pos {
		return 0, errPDFInvalid
	}
	n, err := strconv.Atoi(string(p.s[start:p.pos]))
	if err != nil {
		return 0, errPDFInvalid
	}
	return n, nil
}

func (p *pdfScanner) peekInt() (int, bool) {
	save := p.pos
	n, err := p.readInt()
	p.pos = save
	return n, err == nil
}

// object parses one PDF object. A bare keyword that is not a number, name,
// string or boolean is an error; content streams use contentValue instead.
func (p *pdfScanner) object() (any, error) {
	if p.depth >= 64 {
		return nil, errPDFTooLarge
	}
	p.depth++
	defer func() { p.depth-- }()
	p.skipSpace()
	if p.eof() {
		return nil, errPDFInvalid
	}
	c := p.s[p.pos]
	switch {
	case c == '<' && p.pos+1 < len(p.s) && p.s[p.pos+1] == '<':
		return p.dict()
	case c == '<':
		return p.hexString()
	case c == '[':
		return p.array()
	case c == '(':
		return p.literalString()
	case c == '/':
		return p.name()
	case c == '+' || c == '-' || c == '.' || isPDFDigit(c):
		return p.number()
	}
	if p.consume("true") {
		return true, nil
	}
	if p.consume("false") {
		return false, nil
	}
	if p.consume("null") {
		return nil, nil
	}
	return nil, fmt.Errorf("%w: unexpected %q", errPDFInvalid, p.preview())
}

// contentValue parses an operand or returns a bare keyword as pdfOp.
func (p *pdfScanner) contentValue() (any, error) {
	p.skipSpace()
	if p.eof() {
		return nil, io.EOF
	}
	c := p.s[p.pos]
	switch {
	case c == '<' && p.pos+1 < len(p.s) && p.s[p.pos+1] == '<':
		return p.dict()
	case c == '<':
		return p.hexString()
	case c == '[':
		return p.array()
	case c == '(':
		return p.literalString()
	case c == '/':
		return p.name()
	case c == '+' || c == '-' || c == '.' || isPDFDigit(c):
		return p.number()
	case c == ']' || c == '>' || c == ')' || c == '}':
		// Stray delimiter: skip it rather than stalling the interpreter.
		p.pos++
		return pdfOp(string(c)), nil
	}
	start := p.pos
	for p.pos < len(p.s) && !isPDFSpace(p.s[p.pos]) && !isPDFDelim(p.s[p.pos]) {
		p.pos++
	}
	if start == p.pos {
		p.pos++
		return pdfOp(p.s[start:p.pos]), nil
	}
	return pdfOp(p.s[start:p.pos]), nil
}

func (p *pdfScanner) dict() (any, error) {
	p.pos += 2
	d := pdfDict{}
	depth := 0
	for {
		p.skipSpace()
		if p.eof() {
			return nil, errPDFInvalid
		}
		if p.s[p.pos] == '>' {
			if p.pos+1 < len(p.s) && p.s[p.pos+1] == '>' {
				p.pos += 2
				return d, nil
			}
			p.pos++
			continue
		}
		if p.s[p.pos] != '/' {
			// Tolerate a stray key or value instead of failing the document.
			if _, err := p.object(); err != nil {
				return nil, err
			}
			continue
		}
		key, err := p.name()
		if err != nil {
			return nil, err
		}
		val, err := p.object()
		if err != nil {
			return nil, err
		}
		d[string(key)] = val
		depth++
		if depth > 10000 {
			return nil, errPDFTooLarge
		}
	}
}

func (p *pdfScanner) array() (any, error) {
	p.pos++
	var out []any
	for {
		p.skipSpace()
		if p.eof() {
			return nil, errPDFInvalid
		}
		if p.s[p.pos] == ']' {
			p.pos++
			return out, nil
		}
		v, err := p.object()
		if err != nil {
			return nil, err
		}
		out = append(out, v)
		if len(out) > 100000 {
			return nil, errPDFTooLarge
		}
	}
}

func (p *pdfScanner) name() (pdfName, error) {
	p.skipSpace()
	if p.eof() || p.s[p.pos] != '/' {
		return "", errPDFInvalid
	}
	p.pos++
	var b bytes.Buffer
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		if isPDFSpace(c) || isPDFDelim(c) {
			break
		}
		if c == '#' && p.pos+2 < len(p.s) {
			hi, ok1 := hexVal(p.s[p.pos+1])
			lo, ok2 := hexVal(p.s[p.pos+2])
			if ok1 && ok2 {
				b.WriteByte(byte(hi<<4 | lo))
				p.pos += 3
				continue
			}
		}
		b.WriteByte(c)
		p.pos++
	}
	return pdfName(b.String()), nil
}

func (p *pdfScanner) literalString() (any, error) {
	p.pos++
	var b []byte
	depth := 1
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		p.pos++
		switch c {
		case '\\':
			if p.pos >= len(p.s) {
				return string(b), nil
			}
			e := p.s[p.pos]
			p.pos++
			switch e {
			case 'n':
				b = append(b, '\n')
			case 'r':
				b = append(b, '\r')
			case 't':
				b = append(b, '\t')
			case 'b':
				b = append(b, '\b')
			case 'f':
				b = append(b, '\f')
			case '(', ')', '\\':
				b = append(b, e)
			case '\r':
				if p.pos < len(p.s) && p.s[p.pos] == '\n' {
					p.pos++
				}
			case '\n':
				// line continuation
			default:
				if e >= '0' && e <= '7' {
					v := int(e - '0')
					for i := 0; i < 2 && p.pos < len(p.s); i++ {
						c2 := p.s[p.pos]
						if c2 < '0' || c2 > '7' {
							break
						}
						v = v*8 + int(c2-'0')
						p.pos++
					}
					b = append(b, byte(v))
				} else {
					b = append(b, e)
				}
			}
		case '(':
			depth++
			b = append(b, c)
		case ')':
			depth--
			if depth == 0 {
				return string(b), nil
			}
			b = append(b, c)
		default:
			b = append(b, c)
		}
		if len(b) > maxPDFDecoded {
			return nil, errPDFTooLarge
		}
	}
	return nil, errPDFInvalid
}

func (p *pdfScanner) hexString() (any, error) {
	p.pos++
	var b []byte
	hi := -1
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		p.pos++
		if c == '>' {
			break
		}
		if isPDFSpace(c) {
			continue
		}
		v, ok := hexVal(c)
		if !ok {
			continue
		}
		if hi < 0 {
			hi = v
			continue
		}
		b = append(b, byte(hi<<4|v))
		hi = -1
		if len(b) > maxPDFDecoded {
			return nil, errPDFTooLarge
		}
	}
	if hi >= 0 {
		b = append(b, byte(hi<<4))
	}
	return string(b), nil
}

func (p *pdfScanner) number() (any, error) {
	start := p.pos
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		if isPDFDigit(c) || c == '.' || c == '-' || c == '+' {
			p.pos++
			continue
		}
		break
	}
	tok := string(p.s[start:p.pos])
	if tok == "" {
		return nil, errPDFInvalid
	}
	if n, err := strconv.Atoi(tok); err == nil {
		// "n g R" is an indirect reference.
		save := p.pos
		if gen, ok := p.peekInt(); ok {
			p.readInt()
			if p.consume("R") {
				return pdfRef{num: n, gen: gen}, nil
			}
		}
		p.pos = save
		return float64(n), nil
	}
	f, err := strconv.ParseFloat(tok, 64)
	if err != nil {
		// Tolerate "1.2.3" and similar by keeping the leading value.
		if i := strings.Index(tok[1:], "."); i >= 0 {
			if f2, err2 := strconv.ParseFloat(tok[:i+1], 64); err2 == nil {
				return f2, nil
			}
		}
		return nil, errPDFInvalid
	}
	return f, nil
}

// pdfOp is a content-stream operator keyword.
type pdfOp string

// parsePDF builds a document from raw bytes.
func ParsePDF(data []byte) (*PDFDoc, error) {
	if len(data) == 0 {
		return nil, errPDFInvalid
	}
	head := data
	if len(head) > 1024 {
		head = head[:1024]
	}
	if !bytes.Contains(head, []byte("%PDF-")) {
		return nil, errPDFInvalid
	}
	d := &PDFDoc{
		data:     data,
		xref:     make(map[int]xrefEntry),
		cache:    make(map[int]any),
		objStms:  make(map[int]map[int]any),
		loading:  make(map[int]bool),
		deadline: time.Now().Add(pdfParseTimeout),
	}
	if err := d.loadXref(); err != nil {
		d.xref = make(map[int]xrefEntry)
		if err2 := d.scanObjects(); err2 != nil {
			return nil, err
		}
	}
	if d.trailer == nil {
		return nil, errPDFInvalid
	}
	if v, ok := d.trailer["Encrypt"]; ok && v != nil {
		d.encrypted = true
	}
	return d, nil
}

// loadXref follows the startxref chain from the end of the file.
func (d *PDFDoc) loadXref() error {
	idx := bytes.LastIndex(d.data, []byte("startxref"))
	if idx < 0 {
		return errPDFNoXref
	}
	sc := newPDFScanner(d.data)
	sc.pos = idx + len("startxref")
	off, err := sc.readInt()
	if err != nil || off <= 0 {
		return errPDFNoXref
	}
	seen := make(map[int]bool)
	for hops := 0; hops < 64 && off > 0; hops++ {
		if seen[off] || d.expired() {
			break
		}
		seen[off] = true
		next, err := d.readXref(off)
		if err != nil {
			return err
		}
		off = next
	}
	if len(d.xref) == 0 {
		return errPDFNoXref
	}
	return nil
}

// readXref parses the section at off and returns its /Prev offset.
func (d *PDFDoc) readXref(off int) (int, error) {
	if off < 0 || off >= len(d.data) {
		return 0, errPDFInvalid
	}
	sc := newPDFScanner(d.data)
	sc.pos = off
	sc.skipSpace()
	if sc.consume("xref") {
		return d.readXrefTable(sc)
	}
	obj, err := d.parseIndirectAt(int64(off))
	if err != nil {
		return 0, err
	}
	st, ok := obj.(*pdfStream)
	if !ok {
		return 0, errPDFInvalid
	}
	prev, err := d.readXrefStream(st)
	return int(prev), err
}

func (d *PDFDoc) mergeTrailer(t pdfDict) {
	if t == nil {
		return
	}
	if d.trailer == nil {
		d.trailer = pdfDict{}
	}
	for k, v := range t {
		if _, ok := d.trailer[k]; !ok {
			d.trailer[k] = v
		}
	}
}

func (d *PDFDoc) setXref(num int, ent xrefEntry) {
	if num < 0 {
		return
	}
	if _, ok := d.xref[num]; ok {
		return
	}
	d.xref[num] = ent
}

func (d *PDFDoc) readXrefTable(sc *pdfScanner) (int, error) {
	for entries := 0; entries < maxPDFObjects; entries++ {
		if d.expired() {
			return 0, errPDFTooLarge
		}
		sc.skipSpace()
		if sc.eof() {
			return 0, errPDFInvalid
		}
		if sc.consume("trailer") {
			obj, err := sc.object()
			if err != nil {
				return 0, err
			}
			t, ok := obj.(pdfDict)
			if !ok {
				return 0, errPDFInvalid
			}
			d.mergeTrailer(t)
			if xs, ok := t["XRefStm"]; ok {
				if soff := int64(pdfFloat(d.resolve(xs))); soff > 0 {
					if obj, err := d.parseIndirectAt(soff); err == nil {
						if st, ok := obj.(*pdfStream); ok {
							d.readXrefStream(st)
						}
					}
				}
			}
			return int(pdfFloat(d.resolve(t["Prev"]))), nil
		}
		start, err := sc.readInt()
		if err != nil {
			return 0, err
		}
		count, err := sc.readInt()
		if err != nil {
			return 0, err
		}
		if count < 0 || count > maxPDFObjects {
			return 0, errPDFTooLarge
		}
		for i := 0; i < count; i++ {
			off, err := sc.readInt()
			if err != nil {
				return 0, err
			}
			gen, err := sc.readInt()
			if err != nil {
				return 0, err
			}
			kind := sc.readWord()
			if kind == "" {
				kind = "n"
			}
			if kind[0] == 'f' {
				d.setXref(start+i, xrefEntry{kind: 0})
			} else {
				d.setXref(start+i, xrefEntry{kind: 1, off: int64(off)})
			}
			_ = gen
			entries++
		}
	}
	return 0, errPDFTooLarge
}

func (d *PDFDoc) readXrefStream(st *pdfStream) (int, error) {
	dict := st.dict
	d.mergeTrailer(dict)
	data, err := d.decodeStream(st)
	if err != nil {
		return 0, err
	}
	w := d.array(dict["W"])
	if len(w) < 3 {
		return 0, errPDFInvalid
	}
	w0, w1, w2 := int(pdfFloat(d.resolve(w[0]))), int(pdfFloat(d.resolve(w[1]))), int(pdfFloat(d.resolve(w[2])))
	if w0 < 0 || w1 < 0 || w2 < 0 || w0+w1+w2 <= 0 || w0+w1+w2 > 16 {
		return 0, errPDFInvalid
	}
	size := d.integer(dict["Size"])
	index := d.array(dict["Index"])
	if len(index) == 0 {
		index = []any{float64(0), float64(size)}
	}
	rowLen := w0 + w1 + w2
	pos := 0
	readField := func(b []byte) int64 {
		var v int64
		for _, c := range b {
			v = v<<8 | int64(c)
		}
		return v
	}
	for i := 0; i+1 < len(index); i += 2 {
		start := int(pdfFloat(d.resolve(index[i])))
		count := int(pdfFloat(d.resolve(index[i+1])))
		if start < 0 || count < 0 || count > maxPDFObjects {
			return 0, errPDFTooLarge
		}
		for j := 0; j < count; j++ {
			if pos+rowLen > len(data) {
				return int(pdfFloat(d.resolve(dict["Prev"]))), nil
			}
			row := data[pos : pos+rowLen]
			pos += rowLen
			kind := int64(1)
			if w0 > 0 {
				kind = readField(row[:w0])
			}
			f2 := readField(row[w0 : w0+w1])
			f3 := readField(row[w0+w1:])
			switch kind {
			case 0:
				d.setXref(start+j, xrefEntry{kind: 0})
			case 1:
				d.setXref(start+j, xrefEntry{kind: 1, off: f2})
			case 2:
				d.setXref(start+j, xrefEntry{kind: 2, stm: int(f2), idx: int(f3)})
			}
		}
	}
	return int(pdfFloat(d.resolve(dict["Prev"]))), nil
}

// scanObjects rebuilds an object index by scanning for "N G obj" headers. It
// rescues files with a damaged or missing cross-reference table.
func (d *PDFDoc) scanObjects() error {
	data := d.data
	for i := 0; i < len(data); {
		rel := bytes.Index(data[i:], []byte("obj"))
		if rel < 0 {
			break
		}
		pos := i + rel
		i = pos + len("obj")
		if pos > 0 && !isPDFSpace(data[pos-1]) {
			continue
		}
		if pos+3 < len(data) && !isPDFSpace(data[pos+3]) && !isPDFDelim(data[pos+3]) {
			continue
		}
		k := pos
		for k > 0 && isPDFSpace(data[k-1]) {
			k--
		}
		genEnd := k
		for k > 0 && isPDFDigit(data[k-1]) {
			k--
		}
		if genEnd == k {
			continue
		}
		for k > 0 && isPDFSpace(data[k-1]) {
			k--
		}
		numEnd := k
		for k > 0 && isPDFDigit(data[k-1]) {
			k--
		}
		if numEnd == k {
			continue
		}
		num, err := strconv.Atoi(string(data[k:numEnd]))
		if err != nil || num < 0 {
			continue
		}
		if _, ok := d.xref[num]; !ok {
			d.xref[num] = xrefEntry{kind: 1, off: int64(k)}
		}
	}
	if len(d.xref) == 0 {
		return errPDFInvalid
	}
	if idx := bytes.LastIndex(data, []byte("trailer")); idx >= 0 {
		sc := newPDFScanner(data)
		sc.pos = idx + len("trailer")
		if obj, err := sc.object(); err == nil {
			if t, ok := obj.(pdfDict); ok {
				d.mergeTrailer(t)
			}
		}
	}
	if d.trailer == nil || d.trailer["Root"] == nil {
		for num, ent := range d.xref {
			if ent.kind != 1 {
				continue
			}
			obj, err := d.parseIndirectAt(ent.off)
			if err != nil {
				continue
			}
			t, ok := obj.(pdfDict)
			if !ok {
				continue
			}
			if name, ok := t["Type"].(pdfName); ok && name == "Catalog" {
				d.trailer = pdfDict{"Root": pdfRef{num: num}}
				break
			}
		}
	}
	if d.trailer == nil {
		return errPDFInvalid
	}
	return nil
}

// parseIndirectAt parses the indirect object whose header starts at off.
func (d *PDFDoc) parseIndirectAt(off int64) (any, error) {
	if off < 0 || off >= int64(len(d.data)) {
		return nil, errPDFInvalid
	}
	sc := newPDFScanner(d.data)
	sc.pos = int(off)
	if _, err := sc.readInt(); err != nil {
		return nil, err
	}
	if _, err := sc.readInt(); err != nil {
		return nil, err
	}
	if !sc.consume("obj") {
		return nil, errPDFInvalid
	}
	obj, err := sc.object()
	if err != nil {
		return nil, err
	}
	save := sc.pos
	sc.skipSpace()
	if sc.consume("stream") {
		dict, _ := obj.(pdfDict)
		if dict == nil {
			return nil, errPDFInvalid
		}
		return d.readStreamData(sc, dict)
	}
	sc.pos = save
	return obj, nil
}

func (d *PDFDoc) readStreamData(sc *pdfScanner, dict pdfDict) (*pdfStream, error) {
	if sc.pos < len(sc.s) && sc.s[sc.pos] == '\r' {
		sc.pos++
		if sc.pos < len(sc.s) && sc.s[sc.pos] == '\n' {
			sc.pos++
		}
	} else if sc.pos < len(sc.s) && sc.s[sc.pos] == '\n' {
		sc.pos++
	}
	start := sc.pos
	length := -1
	if lv, ok := dict["Length"]; ok {
		if n := int(d.number(lv)); n >= 0 && n <= maxPDFStreamSize {
			length = n
		}
	}
	if length >= 0 && start+length <= len(sc.s) {
		end := start + length
		probe := sc.s[end:]
		if len(probe) > 32 {
			probe = probe[:32]
		}
		if idx := bytes.Index(probe, []byte("endstream")); idx >= 0 {
			sc.pos = end + idx + len("endstream")
			return &pdfStream{dict: dict, raw: sc.s[start:end]}, nil
		}
	}
	idx := bytes.Index(sc.s[start:], []byte("endstream"))
	if idx < 0 {
		return nil, errPDFInvalid
	}
	raw := bytes.TrimSuffix(sc.s[start:start+idx], []byte("\n"))
	raw = bytes.TrimSuffix(raw, []byte("\r"))
	if len(raw) > maxPDFStreamSize {
		return nil, errPDFTooLarge
	}
	sc.pos = start + idx + len("endstream")
	return &pdfStream{dict: dict, raw: raw}, nil
}

// object parses object num once and caches it. Objects stored in an object
// stream are unpacked on first use.
func (d *PDFDoc) Object(num int) (any, error) {
	if v, ok := d.cache[num]; ok {
		return v, nil
	}
	if d.objects > maxPDFObjects || d.expired() {
		return nil, errPDFTooLarge
	}
	ent, ok := d.xref[num]
	if !ok {
		return nil, fmt.Errorf("%w: object %d is missing", errPDFInvalid, num)
	}
	switch ent.kind {
	case 1:
		v, err := d.parseIndirectAt(ent.off)
		if err != nil {
			return nil, err
		}
		d.objects++
		d.cache[num] = v
		return v, nil
	case 2:
		m, err := d.objectStream(ent.stm)
		if err != nil {
			return nil, err
		}
		v, ok := m[num]
		if !ok {
			return nil, errPDFInvalid
		}
		d.cache[num] = v
		return v, nil
	}
	return nil, fmt.Errorf("%w: object %d is free", errPDFInvalid, num)
}

// objectStream parses and caches an object stream's members.
func (d *PDFDoc) objectStream(stmNum int) (map[int]any, error) {
	if m, ok := d.objStms[stmNum]; ok {
		return m, nil
	}
	if d.loading[stmNum] {
		return nil, errPDFInvalid
	}
	d.loading[stmNum] = true
	defer delete(d.loading, stmNum)
	obj, err := d.Object(stmNum)
	if err != nil {
		return nil, err
	}
	st, ok := obj.(*pdfStream)
	if !ok {
		return nil, errPDFInvalid
	}
	data, err := d.decodeStream(st)
	if err != nil {
		return nil, err
	}
	n := d.integer(st.dict["N"])
	first := d.integer(st.dict["First"])
	if n < 0 || n > maxPDFObjects || first < 0 {
		return nil, errPDFInvalid
	}
	sc := newPDFScanner(data)
	nums := make([]int, n)
	offs := make([]int, n)
	for i := 0; i < n; i++ {
		if nums[i], err = sc.readInt(); err != nil {
			return nil, err
		}
		if offs[i], err = sc.readInt(); err != nil {
			return nil, err
		}
	}
	m := make(map[int]any, n)
	for i := 0; i < n; i++ {
		pos := first + offs[i]
		if pos < 0 || pos >= len(data) {
			continue
		}
		ps := newPDFScanner(data)
		ps.pos = pos
		v, err := ps.object()
		if err != nil {
			continue
		}
		m[nums[i]] = v
	}
	d.objStms[stmNum] = m
	return m, nil
}

// resolve follows indirect references, returning nil when a target is missing.
func (d *PDFDoc) resolve(v any) any {
	for i := 0; i < 8; i++ {
		r, ok := v.(pdfRef)
		if !ok {
			return v
		}
		next, err := d.Object(r.num)
		if err != nil {
			return nil
		}
		v = next
	}
	return nil
}

func (d *PDFDoc) dict(v any) pdfDict {
	if v == nil {
		return nil
	}
	dd, _ := d.resolve(v).(pdfDict)
	return dd
}

func (d *PDFDoc) array(v any) []any {
	if v == nil {
		return nil
	}
	a, _ := d.resolve(v).([]any)
	return a
}

func (d *PDFDoc) number(v any) float64 {
	return pdfFloat(d.resolve(v))
}

func (d *PDFDoc) integer(v any) int {
	return int(d.number(v))
}

func (d *PDFDoc) nameOf(v any) string {
	n, _ := d.resolve(v).(pdfName)
	return string(n)
}

func (d *PDFDoc) root() pdfDict {
	return d.dict(d.trailer["Root"])
}

func (d *PDFDoc) nameList(v any) []string {
	if v == nil {
		return nil
	}
	switch t := d.resolve(v).(type) {
	case pdfName:
		return []string{string(t)}
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if n, ok := d.resolve(e).(pdfName); ok {
				out = append(out, string(n))
			}
		}
		return out
	}
	return nil
}

func (d *PDFDoc) paramList(v any, n int) []pdfDict {
	out := make([]pdfDict, n)
	if v == nil {
		return out
	}
	switch t := d.resolve(v).(type) {
	case pdfDict:
		if n > 0 {
			out[0] = t
		}
	case []any:
		for i := 0; i < n && i < len(t); i++ {
			out[i] = d.dict(t[i])
		}
	}
	return out
}

// decodeStream applies the stream's filter chain and predictors.
func (d *PDFDoc) decodeStream(s *pdfStream) ([]byte, error) {
	data := s.raw
	filters := d.nameList(s.dict["Filter"])
	parms := d.paramList(s.dict["DecodeParms"], len(filters))
	for i, f := range filters {
		var err error
		switch f {
		case "", "Crypt", "Identity":
			// Encrypted documents are rejected before extraction, so any Crypt
			// filter still reached here is the identity filter.
		case "FlateDecode", "Fl":
			data, err = inflatePDF(data)
		case "LZWDecode", "LZW":
			early := 1
			if parms[i] != nil {
				if v, ok := parms[i]["EarlyChange"]; ok {
					early = int(pdfFloat(v))
				}
			}
			data, err = lzwPDF(data, early)
		case "ASCIIHexDecode", "AHx":
			data, err = asciiHexPDF(data)
		case "ASCII85Decode", "A85":
			data, err = ascii85PDF(data)
		case "RunLengthDecode", "RL":
			data, err = runLengthPDF(data)
		default:
			return nil, fmt.Errorf("%w: filter %s", errPDFUnsupported, f)
		}
		if err != nil {
			return nil, err
		}
		if len(data) > maxPDFDecoded {
			return nil, errPDFTooLarge
		}
		if parms[i] != nil {
			if data, err = pdfPredictor(data, parms[i]); err != nil {
				return nil, err
			}
		}
	}
	if len(data) > maxPDFDecoded {
		return nil, errPDFTooLarge
	}
	return data, nil
}

// inflatePDF decodes zlib data, tolerating streams with a raw deflate body.
func inflatePDF(data []byte) ([]byte, error) {
	zr, err := zlib.NewReader(bytes.NewReader(data))
	if err == nil {
		out, err := io.ReadAll(io.LimitReader(zr, maxPDFDecoded+1))
		zr.Close()
		if err == nil {
			if len(out) > maxPDFDecoded {
				return nil, errPDFTooLarge
			}
			return out, nil
		}
	}
	fr := flate.NewReader(bytes.NewReader(data))
	out, err := io.ReadAll(io.LimitReader(fr, maxPDFDecoded+1))
	fr.Close()
	if err != nil {
		return nil, fmt.Errorf("%w: flate: %v", errPDFInvalid, err)
	}
	if len(out) > maxPDFDecoded {
		return nil, errPDFTooLarge
	}
	return out, nil
}

func lzwPDF(data []byte, early int) ([]byte, error) {
	const clearCode, eodCode = 256, 257
	out := make([]byte, 0, len(data)*3)
	dict := make([][]byte, 4096)
	reset := func() int {
		for i := range dict {
			dict[i] = nil
		}
		for i := 0; i < 256; i++ {
			dict[i] = []byte{byte(i)}
		}
		return 258
	}
	next := reset()
	width := 9
	var prev []byte
	var bitBuf uint32
	bits := 0
	pos := 0
	readCode := func() (int, bool) {
		for bits < width {
			if pos >= len(data) {
				return 0, false
			}
			bitBuf = bitBuf<<8 | uint32(data[pos])
			pos++
			bits += 8
		}
		code := int(bitBuf>>(uint32(bits-width))) & (1<<width - 1)
		bits -= width
		return code, true
	}
	add := func(b []byte) {
		if next >= len(dict) {
			return
		}
		cp := make([]byte, len(b))
		copy(cp, b)
		dict[next] = cp
		next++
		switch {
		case width < 9:
			width = 9
		case next+early > (1<<width)-1 && width < 12:
			width++
		}
	}
	appendEntry := func(code int) ([]byte, bool) {
		if code >= 0 && code < len(dict) && dict[code] != nil {
			return dict[code], true
		}
		return nil, false
	}
	for {
		code, ok := readCode()
		if !ok {
			break
		}
		switch code {
		case clearCode:
			next = reset()
			width = 9
			prev = nil
			continue
		case eodCode:
			return out, nil
		}
		var entry []byte
		if code == next {
			if prev == nil {
				return nil, errPDFInvalid
			}
			entry = append(append([]byte{}, prev...), prev[0])
		} else {
			e, ok := appendEntry(code)
			if !ok {
				return nil, errPDFInvalid
			}
			entry = e
		}
		out = append(out, entry...)
		if len(out) > maxPDFDecoded {
			return nil, errPDFTooLarge
		}
		if prev != nil {
			add(append(append([]byte{}, prev...), entry[0]))
		}
		prev = append([]byte{}, entry...)
	}
	return out, nil
}

func asciiHexPDF(data []byte) ([]byte, error) {
	out := make([]byte, 0, len(data)/2)
	hi := -1
	for _, c := range data {
		if c == '>' {
			break
		}
		if isPDFSpace(c) {
			continue
		}
		v, ok := hexVal(c)
		if !ok {
			continue
		}
		if hi < 0 {
			hi = v
			continue
		}
		out = append(out, byte(hi<<4|v))
		hi = -1
	}
	if hi >= 0 {
		out = append(out, byte(hi<<4))
	}
	return out, nil
}

func ascii85PDF(data []byte) ([]byte, error) {
	out := make([]byte, 0, len(data)*4/5)
	var group [5]byte
	n := 0
	for i := 0; i < len(data); i++ {
		c := data[i]
		switch {
		case c == '~':
			if i+1 < len(data) && data[i+1] == '>' {
				i = len(data)
			}
		case isPDFSpace(c):
		case c == 'z' && n == 0:
			out = append(out, 0, 0, 0, 0)
		case c >= '!' && c <= 'u':
			group[n] = c - '!'
			n++
			if n == 5 {
				var v uint32
				for _, g := range group {
					v = v*85 + uint32(g)
				}
				out = append(out, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
				n = 0
				if len(out) > maxPDFDecoded {
					return nil, errPDFTooLarge
				}
			}
		}
	}
	if n > 0 {
		for i := n; i < 5; i++ {
			group[i] = 84
		}
		var v uint32
		for _, g := range group {
			v = v*85 + uint32(g)
		}
		full := []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
		out = append(out, full[:n-1]...)
	}
	return out, nil
}

func runLengthPDF(data []byte) ([]byte, error) {
	out := make([]byte, 0, len(data)*2)
	for i := 0; i < len(data); {
		n := int(data[i])
		i++
		switch {
		case n == 128:
			return out, nil
		case n < 128:
			count := n + 1
			if i+count > len(data) {
				count = len(data) - i
			}
			out = append(out, data[i:i+count]...)
			i += count
		default:
			if i >= len(data) {
				return out, nil
			}
			count := 257 - n
			for j := 0; j < count; j++ {
				out = append(out, data[i])
			}
			i++
		}
		if len(out) > maxPDFDecoded {
			return nil, errPDFTooLarge
		}
	}
	return out, nil
}

func paeth(a, b, c int) int {
	p := a + b - c
	pa, pb, pc := absInt(p-a), absInt(p-b), absInt(p-c)
	if pa <= pb && pa <= pc {
		return a
	}
	if pb <= pc {
		return b
	}
	return c
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// pdfPredictor undoes TIFF and PNG row predictors.
func pdfPredictor(data []byte, parms pdfDict) ([]byte, error) {
	pred := int(pdfFloat(parms["Predictor"]))
	if pred <= 1 {
		return data, nil
	}
	colors := int(pdfFloat(parms["Colors"]))
	if colors <= 0 {
		colors = 1
	}
	bpc := int(pdfFloat(parms["BitsPerComponent"]))
	if bpc <= 0 {
		bpc = 8
	}
	columns := int(pdfFloat(parms["Columns"]))
	if columns <= 0 {
		columns = 1
	}
	if bpc != 8 && bpc != 16 {
		return nil, fmt.Errorf("%w: predictor bits %d", errPDFUnsupported, bpc)
	}
	bpp := (colors*bpc + 7) / 8
	if bpp < 1 {
		bpp = 1
	}
	rowLen := (colors*bpc*columns + 7) / 8
	if rowLen <= 0 || rowLen > maxPDFDecoded {
		return nil, errPDFTooLarge
	}
	switch {
	case pred == 2:
		if bpc != 8 {
			return nil, fmt.Errorf("%w: TIFF predictor bits %d", errPDFUnsupported, bpc)
		}
		for r := 0; r+rowLen <= len(data); r += rowLen {
			row := data[r : r+rowLen]
			for i := bpp; i < len(row); i++ {
				row[i] += row[i-bpp]
			}
		}
		return data, nil
	case pred >= 10:
		out := make([]byte, 0, len(data))
		prev := make([]byte, rowLen)
		for r := 0; r+1+rowLen <= len(data); r += 1 + rowLen {
			ft := data[r]
			row := make([]byte, rowLen)
			copy(row, data[r+1:r+1+rowLen])
			switch ft {
			case 0:
			case 1:
				for i := bpp; i < rowLen; i++ {
					row[i] += row[i-bpp]
				}
			case 2:
				for i := 0; i < rowLen; i++ {
					row[i] += prev[i]
				}
			case 3:
				for i := 0; i < rowLen; i++ {
					var left int
					if i >= bpp {
						left = int(row[i-bpp])
					}
					row[i] += byte((left + int(prev[i])) / 2)
				}
			case 4:
				for i := 0; i < rowLen; i++ {
					var a, c int
					if i >= bpp {
						a = int(row[i-bpp])
						c = int(prev[i-bpp])
					}
					row[i] += byte(paeth(a, int(prev[i]), c))
				}
			default:
				return nil, fmt.Errorf("%w: PNG predictor %d", errPDFInvalid, ft)
			}
			out = append(out, row...)
			prev = row
			if len(out) > maxPDFDecoded {
				return nil, errPDFTooLarge
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("%w: predictor %d", errPDFUnsupported, pred)
}
