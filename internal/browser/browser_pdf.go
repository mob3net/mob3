package browser

// Browser-side PDF integration: page tracking for the footer status line and
// the download shortcut. The document reader and its HTML live in pdf*.go;
// this file only maps the rendered pages back to the viewport.

import "fmt"

// updatePDFPageStarts records the layout row where each PDF page begins. The
// footer uses these offsets to report the page under the viewport as the user
// scrolls. Called after every layout pass.
func (b *Browser) updatePDFPageStarts() {
	b.pdfStarts = b.pdfStarts[:0]
	if b.Page == nil || b.Page.PDF == nil || b.Layout == nil {
		return
	}
	nodes := b.Page.PDF.PageNodes
	if len(nodes) == 0 {
		return
	}
	index := make(map[*Node]int, len(nodes))
	for i, n := range nodes {
		index[n] = i
	}
	starts := make([]int, len(nodes))
	for i := range starts {
		starts[i] = -1
	}
	for _, f := range b.Layout.Frags {
		for n := f.Node; n != nil; n = n.Parent {
			i, ok := index[n]
			if !ok {
				continue
			}
			if starts[i] < 0 || f.Y < starts[i] {
				starts[i] = f.Y
			}
			break
		}
	}
	// Empty pages have no fragments; pin them to the previous start so the
	// page number never runs backwards.
	last := 0
	for i := range starts {
		if starts[i] < 0 {
			starts[i] = last
			continue
		}
		last = starts[i]
	}
	b.pdfStarts = starts
}

// pdfPageNumber returns the page shown at the top of the viewport, or 0 when
// the current document is not a PDF.
func (b *Browser) pdfPageNumber() int {
	if b.Page == nil || b.Page.PDF == nil {
		return 0
	}
	n := len(b.Page.PDF.Pages)
	if n == 0 {
		return 0
	}
	cur := 1
	for i, y := range b.pdfStarts {
		if i >= n {
			break
		}
		if y <= b.Scroll {
			cur = i + 1
		}
	}
	if cur > n {
		cur = n
	}
	if cur < 1 {
		cur = 1
	}
	return cur
}

// downloadCurrentPDF opens the curl dialog for the current PDF document. It
// does nothing unless the PDF viewer is showing.
func (b *Browser) downloadCurrentPDF() {
	if b.Page == nil || b.Page.PDF == nil {
		return
	}
	u := b.URL
	if u == nil {
		u = b.Page.URL
	}
	if u == nil {
		b.status = "This PDF has no download URL"
		return
	}
	switch u.Scheme {
	case "http", "https", "file":
	default:
		b.status = "This PDF has no download URL"
		return
	}
	name := pdfDisplayName(u)
	if name == "" {
		name = "document.pdf"
	}
	b.showDownload(u.String(), name)
	b.status = "Download — y copies the curl command"
}

// pdfFooterLine builds the footer status line for a PDF page and returns the
// download hint separately so it can be drawn as a clickable label. The hint is
// dropped on terminals too narrow to hold it.
func (b *Browser) pdfFooterLine(avail int) (text, hint string, hintCell int, ok bool) {
	if b.Page == nil || b.Page.PDF == nil || len(b.Page.PDF.Pages) == 0 || avail <= 0 {
		return "", "", 0, false
	}
	u := b.URL
	if u == nil {
		u = b.Page.URL
	}
	name := pdfDisplayName(u)
	if name == "" {
		name = "PDF"
	}
	base := fmt.Sprintf(" Page %d/%d", b.pdfPageNumber(), len(b.Page.PDF.Pages))
	full := base + " (d -> Download " + name + ")"
	if stringWidth(full) <= avail {
		return full, "(d -> Download " + name + ")", stringWidth(base) + 1, true
	}
	// Shorten the file name inside the hint before giving the hint up.
	const prefix, suffix = "(d -> Download ", ")"
	room := avail - stringWidth(base) - 1 - stringWidth(prefix) - stringWidth(suffix)
	if room >= 6 {
		short := truncateWidth(name, room-1) + "…"
		hint := prefix + short + suffix
		if stringWidth(base+" "+hint) <= avail {
			return base + " " + hint, hint, stringWidth(base) + 1, true
		}
	}
	if hint := "(d)"; stringWidth(base+" "+hint) <= avail {
		return base + " " + hint, hint, stringWidth(base) + 1, true
	}
	if stringWidth(base) <= avail {
		return base, "", 0, true
	}
	return truncateWidth(base, avail), "", 0, true
}
