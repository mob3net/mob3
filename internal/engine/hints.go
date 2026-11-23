package engine

// HintGlyph is one letter of a target's visible text, with the terminal cell
// it occupies.
type HintGlyph struct {
	Ch   byte
	R    rune
	X, Y int
}

// HintChar records the cell of one highlighted hint letter.
type HintChar struct {
	R    rune
	X, Y int
}

// LinkHint is one Vimium-style hint: the target it activates, the letters that
// follow it, and the text cells to highlight. Target indexes the hint target
// list.
type LinkHint struct {
	Target int
	Label  string
	Chars  []HintChar
}

// HintLetter lowercases an ASCII letter and reports whether r is one.
func HintLetter(r rune) (byte, bool) {
	if r >= 'a' && r <= 'z' {
		return byte(r), true
	}
	if r >= 'A' && r <= 'Z' {
		return byte(r) + 'a' - 'A', true
	}
	return 0, false
}

// AssignLinkHints assigns a unique hint to each link. Up to 26 links get a
// single unused letter from their text. Larger pages start from two adjacent
// letters but shorten any hint whose first letter is unique back to one
// letter. If a link has no text letters at all, a generated label is used and
// drawn inside its box.
func AssignLinkHints(links []int, glyphs map[int][]HintGlyph) []LinkHint {
	var hints []LinkHint
	if len(links) <= 26 {
		if single, ok := assignSingleHints(links, glyphs); ok {
			hints = single
		}
	}
	if hints == nil {
		hints = assignPairHints(links, glyphs)
	}
	shortenUniquePairs(hints)
	return hints
}

// shortenUniquePairs turns a two-letter hint into a one-letter hint when no
// other label starts with the same letter. The second letter is only needed to
// distinguish links that share a prefix, so keeping it otherwise makes simple
// links look more complicated than they are.
func shortenUniquePairs(hints []LinkHint) {
	counts := make(map[byte]int, len(hints))
	for _, h := range hints {
		if h.Label != "" {
			counts[h.Label[0]]++
		}
	}
	for i := range hints {
		h := &hints[i]
		if len(h.Label) != 2 || counts[h.Label[0]] != 1 {
			continue
		}
		h.Label = h.Label[:1]
		if len(h.Chars) > 0 {
			h.Chars = h.Chars[:1]
		}
	}
}

func assignSingleHints(links []int, glyphs map[int][]HintGlyph) ([]LinkHint, bool) {
	used := [26]bool{}
	hints := make([]LinkHint, 0, len(links))
	for _, link := range links {
		chosen := false
		for _, g := range glyphs[link] {
			idx := int(g.Ch - 'a')
			if used[idx] {
				continue
			}
			used[idx] = true
			hints = append(hints, LinkHint{
				Target: link,
				Label:  string(g.Ch),
				Chars:  []HintChar{{R: g.R, X: g.X, Y: g.Y}},
			})
			chosen = true
			break
		}
		if chosen {
			continue
		}
		if len(glyphs[link]) > 0 {
			// This link has text but no unused letter left. Let the caller
			// switch the whole page to two-letter hints.
			return nil, false
		}
		// A link with no text letters (for example an image) gets a free
		// letter from the remaining alphabet, drawn inside its box.
		free := -1
		for i := range used {
			if !used[i] {
				free = i
				break
			}
		}
		if free < 0 {
			return nil, false
		}
		used[free] = true
		hints = append(hints, LinkHint{Target: link, Label: string(byte('a' + free))})
	}
	return hints, true
}

func assignPairHints(links []int, glyphs map[int][]HintGlyph) []LinkHint {
	used := make(map[string]bool, len(links))
	hints := make([]LinkHint, 0, len(links))
	fallback := 0
	for _, link := range links {
		gs := glyphs[link]
		label, chars := pickPair(gs, used)
		if label == "" {
			for {
				label = EncodeLinkHint(fallback, 2)
				fallback++
				if !used[label] {
					break
				}
			}
			chars = nil
		}
		used[label] = true
		hints = append(hints, LinkHint{Target: link, Label: label, Chars: chars})
	}
	return hints
}

// pickPair finds the first unique two-letter label made of adjacent letters
// in the link text. Only neighbouring cells are considered, so the two
// highlighted letters never have other characters or blanks between them.
func pickPair(gs []HintGlyph, used map[string]bool) (string, []HintChar) {
	for a := 0; a+1 < len(gs); a++ {
		if gs[a].Y != gs[a+1].Y || gs[a+1].X != gs[a].X+1 {
			continue
		}
		label := string(gs[a].Ch) + string(gs[a+1].Ch)
		if used[label] {
			continue
		}
		return label, []HintChar{
			{R: gs[a].R, X: gs[a].X, Y: gs[a].Y},
			{R: gs[a+1].R, X: gs[a+1].X, Y: gs[a+1].Y},
		}
	}
	return "", nil
}

// EncodeLinkHint renders i in base 26 using letters, padded to length. Index 0
// is "a" (or "aa" for two-letter hints).
func EncodeLinkHint(i, length int) string {
	out := make([]byte, length)
	for p := length - 1; p >= 0; p-- {
		out[p] = byte('a' + i%26)
		i /= 26
	}
	return string(out)
}
