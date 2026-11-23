package engine

import "testing"

func TestPickPairUsesAdjacentCells(t *testing.T) {
	glyphs := []HintGlyph{
		{Ch: 'u', R: 'U', X: 1, Y: 1},
		{Ch: 'p', R: 'p', X: 2, Y: 1},
		{Ch: 'g', R: 'g', X: 3, Y: 1},
		{Ch: 'r', R: 'r', X: 4, Y: 1},
	}
	label, chars := pickPair(glyphs, map[string]bool{"up": true})
	if label != "pg" {
		t.Fatalf("label = %q, want pg", label)
	}
	if len(chars) != 2 || chars[0].R != 'p' || chars[1].R != 'g' {
		t.Fatalf("chars = %+v, want p and g", chars)
	}
	if chars[0].Y != chars[1].Y || chars[1].X != chars[0].X+1 {
		t.Fatalf("hint letters are not adjacent: %+v", chars)
	}
}

func TestShortenUniquePairHints(t *testing.T) {
	hints := []LinkHint{
		{Label: "fi", Chars: []HintChar{{R: 'F'}, {R: 'i'}}},
		{Label: "pg", Chars: []HintChar{{R: 'p'}, {R: 'g'}}},
		{Label: "pa", Chars: []HintChar{{R: 'p'}, {R: 'a'}}},
	}
	shortenUniquePairs(hints)
	if hints[0].Label != "f" || len(hints[0].Chars) != 1 {
		t.Fatalf("unique hint = %+v, want shortened to f", hints[0])
	}
	if hints[1].Label != "pg" || hints[2].Label != "pa" {
		t.Fatalf("shared-prefix hints should stay two letters: %+v", hints)
	}
}

func TestLinkHintsNoTextFreeLetters(t *testing.T) {
	hints := AssignLinkHints([]int{0, 1}, map[int][]HintGlyph{
		0: {{Ch: 'd', R: 'D', X: 1, Y: 1}},
	})
	if len(hints) != 2 {
		t.Fatalf("hints = %d, want 2", len(hints))
	}
	if hints[0].Label != "d" {
		t.Fatalf("text link label = %q, want d", hints[0].Label)
	}
	if hints[1].Label != "a" || len(hints[1].Chars) != 0 {
		t.Fatalf("no-text link = %+v, want free letter a", hints[1])
	}
}
