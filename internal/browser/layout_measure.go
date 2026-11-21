package browser

import (
	"strings"
)

// measure estimates the max-content and min-content widths (in cells) of a
// subtree. It is used to size table columns.
// measureInlineText returns the max-content and min-content widths of one
// text run. Normal and nowrap text collapse whitespace runs to a single space,
// and leading, trailing and indentation-only whitespace contributes nothing,
// matching inline layout. Pre text preserves spaces and line breaks.
func measureInlineText(s, whiteSpace string, sc int) (maxW, minW int) {
	if whiteSpace == "pre" {
		for _, line := range strings.Split(s, "\n") {
			lineMax, word, wordMax := 0, 0, 0
			for _, r := range line {
				if r == ' ' || r == '\t' || r == '\r' || r == '\f' {
					if word > wordMax {
						wordMax = word
					}
					word = 0
					lineMax += sc
					continue
				}
				rw := runeWidth(r) * sc
				lineMax += rw
				word += rw
			}
			if word > wordMax {
				wordMax = word
			}
			if lineMax > maxW {
				maxW = lineMax
			}
			if wordMax > minW {
				minW = wordMax
			}
		}
		return maxW, maxW
	}
	lineMax, word, wordMax := 0, 0, 0
	pendingSpace, content := false, false
	for _, r := range s {
		if r == ' ' || r == '\n' || r == '\t' || r == '\r' || r == '\f' {
			if word > wordMax {
				wordMax = word
			}
			word = 0
			pendingSpace = content
			continue
		}
		if pendingSpace {
			lineMax += sc
			pendingSpace = false
		}
		rw := runeWidth(r) * sc
		lineMax += rw
		word += rw
		content = true
	}
	if word > wordMax {
		wordMax = word
	}
	if whiteSpace == "nowrap" {
		wordMax = lineMax
	}
	return lineMax, wordMax
}

// flowChildren returns n's children with display:contents wrappers expanded in
// place, so their children are measured in the same formatting context as the
// rest of the container instead of behaving like one nested block.
func flowChildren(n *Node) []*Node {
	var out []*Node
	for _, c := range n.Children {
		if c.Type == ElementNode && c.Style != nil && c.Style.Display == "contents" {
			out = append(out, flowChildren(c)...)
			continue
		}
		out = append(out, c)
	}
	return out
}

func (l *layouter) measure(n *Node) (maxW, minW int) {
	if n == nil {
		return 0, 0
	}
	if n.Type == TextNode {
		if n.Style == nil {
			return 0, 0
		}
		s := expandTabs(n.Text)
		s = applyTextTransform(s, n.Style.TextTransform)
		sc := l.scale(n.Style)
		maxW, minW = measureInlineText(s, n.Style.WhiteSpace, sc)
		return maxW, minW
	}
	if n.Type != ElementNode || n.Style == nil || n.Style.Display == "none" {
		return 0, 0
	}
	st := n.Style
	if st.Display == "contents" {
		// A contents box has no box of its own: its children join the
		// parent's inline or flex run, so their widths add up instead of the
		// widest one winning. Treating it as a block used to collapse nav
		// rows to the widest link and make the neighbours overlap.
		total := 0
		space := false
		for _, c := range flowChildren(n) {
			if c.Type == ElementNode && (c.Style == nil || c.Style.Display == "none" || isOutOfFlow(c.Style)) {
				continue
			}
			mw, mi := l.measure(c)
			if space && (mw > 0 || mi > 0) {
				total++
			}
			total += mw
			if mi > minW {
				minW = mi
			}
			if c.Type == TextNode || mw > 0 || mi > 0 {
				space = true
			}
		}
		return total, minW
	}
	if n.Tag == "svg" && (n.Img == nil || l.imagesOff) {
		label, visible := svgLabel(n)
		if !visible || label == "" {
			return 0, 0
		}
		w := stringWidth(label)
		return w, w
	}
	if n.Tag == "img" || n.Tag == "svg" {
		// Percentage widths depend on the container, not on the content, so
		// they must not feed intrinsic sizing. Resolving `width:100%` against a
		// large basis inflated the min-content width (a 100x100 thumbnail
		// measured 32 cells), which then forced flex and grid items wider than
		// their slot and painted the image over the neighbouring text.
		if st.Width.Set && !st.Width.IsAuto() && (st.Width.U == UnitPercent || (st.Width.Calc && st.Width.CalcPct > 0)) {
			copied := *st
			copied.Width = Length{}
			cw, _ := imageSize(n, &copied, l.width, l.imageCapWidth())
			cw = l.limitImageMeasure(st, cw)
			if cw < 1 {
				cw = 1
			}
			// It fills its container, so it may shrink with it instead of
			// imposing its intrinsic width as a minimum.
			return cw, 1
		}
		cw, _ := imageSize(n, st, l.width, l.imageCapWidth())
		cw = l.limitImageMeasure(st, cw)
		if cw < 1 {
			cw = 1
		}
		return cw, cw
	}
	if n.Tag == "select" && n.HasAttr("multiple") {
		opts := selectOptions(n)
		w := 10
		for _, opt := range opts {
			mark := glyphCheckboxOff + " "
			if opt.HasAttr("selected") {
				mark = glyphCheckboxOn + " "
			}
			if ow := stringWidth(mark + optionLabel(opt)); ow > w {
				w = ow
			}
		}
		return w, w
	}
	if n.Tag == "input" || n.Tag == "select" || n.Tag == "textarea" || n.Tag == "button" {
		if n.Tag == "input" && strings.EqualFold(n.Attr("type"), "hidden") {
			return 0, 0
		}
		w := stringWidth(formLabel(n))
		if w < 1 {
			w = 1
		}
		return w, w
	}
	pad, _ := l.paddingH(n)
	if st.Width.Set && !st.Width.IsAuto() && !st.Width.Calc && st.Width.U != UnitPercent {
		// A definite absolute width fixes the element's used width for
		// intrinsic sizing too, so shrink-to-fit containers do not ignore
		// explicitly sized children such as logos.
		w := st.Width.resolveH(l.width, st.FontSize)
		if st.BoxSizing == "border-box" {
			w -= pad
			if w < 0 {
				w = 0
			}
		}
		return w + pad, w + pad
	}
	isBlock := false
	switch st.Display {
	case "block", "grid", "list-item", "table":
		isBlock = true
	}
	if isBlock {
		floatSum := 0
		// Normal block and list-item containers lay their inline children on
		// one line, so their max-content width adds those children up. Grid
		// and table children stay independent and take the widest one.
		inlineFlow := st.Display == "block" || st.Display == "list-item"
		lineW, lineMin := 0, 0
		space := false
		flushLine := func() {
			if lineW > maxW {
				maxW = lineW
			}
			if lineMin > minW {
				minW = lineMin
			}
			lineW, lineMin = 0, 0
			space = false
		}
		for _, c := range flowChildren(n) {
			if c.Type == ElementNode && (c.Style == nil || c.Style.Display == "none" || isOutOfFlow(c.Style)) {
				continue
			}
			// A table caption (including a figure caption) wraps to the table
			// width; its own long text must not inflate the intrinsic width.
			if st.Display == "table" && c.Type == ElementNode && (c.Tag == "caption" || c.Tag == "figcaption") {
				continue
			}
			mw, mi := l.measure(c)
			if c.Type == ElementNode && c.Style != nil && c.Style.Float != "" {
				// Floats share a line until the width runs out, so their
				// max-content contribution is the sum of their widths.
				ml, mr, _, _, _, _, _, _, _, _, _, _ := l.boxMetrics(c.Style, l.width)
				floatSum += mw + ml + mr
				if mi > minW {
					minW = mi
				}
				continue
			}
			block := !inlineFlow || (c.Type == ElementNode && l.isBlock(c))
			if block {
				flushLine()
				if mw > maxW {
					maxW = mw
				}
				if mi > minW {
					minW = mi
				}
				continue
			}
			if space && (mw > 0 || mi > 0) {
				lineW++
			}
			lineW += mw
			if mi > lineMin {
				lineMin = mi
			}
			if c.Type == TextNode || mw > 0 || mi > 0 {
				space = true
			}
		}
		flushLine()
		if floatSum > maxW {
			maxW = floatSum
		}
		if st.WhiteSpace == "nowrap" || st.WhiteSpace == "pre" {
			minW = maxW
		}
		return maxW + pad, minW + pad
	}
	// inline: content is laid out left to right, so max widths add up while
	// the minimum width is the widest unbreakable piece.
	total := 0
	space := false
	for _, c := range flowChildren(n) {
		if c.Type == ElementNode && c.Style != nil && (c.Style.Display == "none" || isOutOfFlow(c.Style)) {
			continue
		}
		if c.Type == ElementNode && c.Tag == "br" {
			if total > maxW {
				maxW = total
			}
			total = 0
			space = false
			continue
		}
		if c.Type == TextNode && c.Style != nil {
			s := expandTabs(c.Text)
			s = applyTextTransform(s, c.Style.TextTransform)
			sc := l.scale(c.Style)
			partMax, partMin := measureInlineText(s, c.Style.WhiteSpace, sc)
			total += partMax
			if space && partMax > 0 {
				total += sc
			}
			if partMin > minW {
				minW = partMin
			}
			space = true
			continue
		}
		mw, mi := l.measure(c)
		if space && (mw > 0 || mi > 0) {
			total++
		}
		total += mw
		if mi > minW {
			minW = mi
		}
		if mw > 0 || mi > 0 {
			space = true
		}
	}
	if total > maxW {
		maxW = total
	}
	if st.WhiteSpace == "nowrap" || st.WhiteSpace == "pre" {
		minW = maxW
	}
	return maxW + pad, minW + pad
}

// limitImageMeasure applies a declared max-width to an image's intrinsic
// measurement. Without it a floated thumbnail with `width:100%` and
// `max-width:110px` measured at its source size, so the float stayed 220px
// wide and pushed the text beside it into a narrow column.
func (l *layouter) limitImageMeasure(st *Style, cw int) int {
	if st == nil || !st.MaxWidth.Set || st.MaxWidth.IsAuto() || st.MaxWidth.Calc {
		return cw
	}
	mw := st.MaxWidth.resolveH(l.width, st.FontSize)
	if mw > 0 && cw > mw {
		return mw
	}
	return cw
}
