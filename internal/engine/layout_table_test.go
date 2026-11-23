package engine

import (
	"strings"
	"testing"
)

func TestTableColumnsAndCells(t *testing.T) {
	ly := build(`<table><tr><th>Name</th><th>Value</th></tr><tr><td>alpha</td><td>1</td></tr></table>`, 40)
	var texts []string
	for _, f := range ly.Frags {
		if f.Kind == FragText {
			texts = append(texts, f.Text)
		}
	}
	joined := strings.Join(texts, " ")
	for _, want := range []string{"Name", "Value", "alpha"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %q", want, joined)
		}
	}
}

func TestTableCellPercentageWidthAndNowrap(t *testing.T) {
	ly := build(`<table><tr><td width="25%"></td><td>search</td><td width="25%" nowrap><a href="/x">Advanced search</a></td></tr></table>`, 60)
	linkLines := map[int]bool{}
	var linkText string
	for _, f := range ly.Frags {
		if f.Kind != FragText || f.Link == nil || f.Link.Href != "/x" {
			continue
		}
		linkLines[f.Y] = true
		linkText += f.Text
	}
	if len(linkLines) != 1 {
		t.Fatalf("legacy nowrap cell wrapped over %d lines", len(linkLines))
	}
	if !strings.Contains(linkText, "Advanced") {
		t.Fatalf("advanced link text missing: %q", linkText)
	}
}

func TestTableCellWidthDoesNotClampContent(t *testing.T) {
	ly := build(`<table><tr><td width="10%">abcdefgh</td><td>zzzzzzzzzzzzzzzzzzzz</td></tr></table>`, 60)
	for _, f := range ly.Frags {
		if f.Kind == FragText && f.Text == "abcdefgh" {
			return
		}
	}
	t.Fatal("cell content was wrapped by its own width hint")
}

func TestTableCenteredByCenterTag(t *testing.T) {
	left := build(`<table><tr><td>Name</td><td>Value</td></tr></table>`, 40)
	center := build(`<center><table><tr><td>Name</td><td>Value</td></tr></table></center>`, 40)
	leftX, centerX := -1, -1
	for _, f := range left.Frags {
		if f.Kind == FragText && f.Text == "Name" {
			leftX = f.X
		}
	}
	for _, f := range center.Frags {
		if f.Kind == FragText && f.Text == "Name" {
			centerX = f.X
		}
	}
	if leftX < 0 || centerX < 0 {
		t.Fatalf("left=%d center=%d", leftX, centerX)
	}
	if centerX <= leftX {
		t.Fatalf("centered table at %d should be right of left-aligned %d", centerX, leftX)
	}
}

func TestHiddenInputDoesNotWidenTableCell(t *testing.T) {
	plain := build(`<table><tr><td>ab</td></tr></table>`, 40)
	hidden := build(`<table><tr><td><input type="hidden" name="x" value="long-value">ab</td></tr></table>`, 40)
	tableWidth := func(ly *Layout) int {
		for _, f := range ly.Frags {
			if f.Kind == FragFill && f.W > 1 {
				return f.W
			}
		}
		return 0
	}
	if pw, hw := tableWidth(plain), tableWidth(hidden); pw != hw {
		t.Fatalf("hidden input changed table width: plain=%d hidden=%d", pw, hw)
	}
}

func TestTableCellVerticalAlign(t *testing.T) {
	table := func(align string) *Layout {
		return build(`<table><tr><td style="`+align+`">short</td>`+
			`<td>top<br>middle<br>bottom</td></tr></table>`, 40)
	}
	shortY := func(ly *Layout) int {
		f := textFrag(ly, "short")
		if f == nil {
			t.Fatal("short cell text missing")
		}
		return f.Y
	}
	if got, want := shortY(table("")), textFrag(table(""), "middle").Y; got != want {
		t.Errorf("default vertical-align Y=%d, want middle Y=%d", got, want)
	}
	if got, want := shortY(table("vertical-align:top")), textFrag(table(""), "top").Y; got != want {
		t.Errorf("vertical-align:top Y=%d, want top line Y=%d", got, want)
	}
	if got, want := shortY(table("vertical-align:bottom")), textFrag(table(""), "bottom").Y; got != want {
		t.Errorf("vertical-align:bottom Y=%d, want bottom line Y=%d", got, want)
	}
}

func TestTableCellBackgroundNotShifted(t *testing.T) {
	ly := build(`<table><tr><td style="background:#ff0000;vertical-align:bottom">short</td>`+
		`<td>top<br>middle<br>bottom</td></tr></table>`, 40)
	top := textFrag(ly, "top")
	if top == nil {
		t.Fatal("reference text missing")
	}
	for _, f := range ly.Frags {
		if f.Kind == FragFill && f.Color == RGB(0xff, 0, 0) {
			if f.Y != top.Y {
				t.Fatalf("cell background Y=%d, want cell top %d", f.Y, top.Y)
			}
			return
		}
	}
	t.Fatal("cell background fill missing")
}

func firstTableFillWidth(ly *Layout) int {
	width := 0
	for _, f := range ly.Frags {
		if f.Kind == FragFill && f.W > width {
			width = f.W
		}
	}
	return width
}

func TestTableWidthDeclaration(t *testing.T) {
	ly := build(`<table style="width:100%"><tr><td>a</td><td>b</td></tr></table>`, 40)
	if got := firstTableFillWidth(ly); got != 40 {
		t.Fatalf("width:100%% table = %d, want 40", got)
	}
}

func TestTablePercentageColumnsFit(t *testing.T) {
	ly := build(`<table style="width:100%"><tr><th style="width:70%">A</th><th style="width:70%">B</th></tr></table>`, 40)
	if got := firstTableFillWidth(ly); got != 40 {
		t.Fatalf("overflowing percentage table = %d, want 40", got)
	}
}
