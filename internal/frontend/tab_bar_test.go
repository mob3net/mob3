package frontend

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"browser/internal/protocol"
)

var testANSI = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

func TestTabStripSharesToolbarRow(t *testing.T) {
	st := chromeState()
	st.Width = 120
	st.Title = "Page title"
	tabs := []TabView{{Title: "Alpha", Active: true}, {Title: "Beta"}}

	layout := LayoutChromeWithTabs(st, tabs, 0)
	if len(layout.Header) != st.Width*st.HeaderRows {
		t.Fatalf("header cells = %d, want %d", len(layout.Header), st.Width*st.HeaderRows)
	}
	var row strings.Builder
	for _, c := range layout.Header[:st.Width] {
		if c.R != 0 {
			row.WriteRune(c.R)
		}
	}
	for _, want := range []string{"1:Alpha", "2:Beta"} {
		if !strings.Contains(row.String(), want) {
			t.Errorf("toolbar %q does not contain %q", row.String(), want)
		}
	}
	if strings.Contains(row.String(), "Page title") {
		t.Errorf("toolbar still contains the page title: %q", row.String())
	}

	seen := map[ChromeHitKind]map[int]bool{}
	var newHit ChromeHit
	for _, hit := range layout.Hits {
		if hit.Kind == ChromeHitTab || hit.Kind == ChromeHitTabClose || hit.Kind == ChromeHitNewTab {
			if hit.Y != 0 {
				t.Errorf("%v hit is on row %d, want the toolbar row", hit.Kind, hit.Y)
			}
		}
		if hit.Kind == ChromeHitNewTab {
			newHit = hit
		}
		if hit.Kind == ChromeHitTab || hit.Kind == ChromeHitTabClose {
			if seen[hit.Kind] == nil {
				seen[hit.Kind] = map[int]bool{}
			}
			seen[hit.Kind][hit.Index] = true
		}
	}
	for i := range tabs {
		if !seen[ChromeHitTab][i] {
			t.Errorf("missing tab hit %d", i)
		}
		if !seen[ChromeHitTabClose][i] {
			t.Errorf("missing close hit %d", i)
		}
	}
	if newHit.W == 0 {
		t.Fatal("missing new-tab hit")
	}

	// The tab strip must not overlap the fixed controls at either end.
	for _, hit := range layout.Hits {
		if hit.Kind != ChromeHitTab && hit.Kind != ChromeHitTabClose {
			continue
		}
		for _, control := range layout.Hits {
			switch control.Kind {
			case ChromeHitBack, ChromeHitForward, ChromeHitReload, ChromeHitHome,
				ChromeHitHistory, ChromeHitHelp, ChromeHitConfig:
			default:
				continue
			}
			if hit.X < control.X+control.W && control.X < hit.X+hit.W {
				t.Errorf("%v hit %+v overlaps %v hit %+v", hit.Kind, hit, control.Kind, control)
			}
		}
	}

	// The active tab is highlighted and the inactive one is not.
	for _, c := range layout.Header[:st.Width] {
		if c.R == '1' && c.BG != colSelectedBG {
			t.Fatalf("active tab BG = %+v, want %+v", c.BG, colSelectedBG)
		}
	}
}

func TestTabStripKeepsTitleForOneTab(t *testing.T) {
	st := chromeState()
	st.Width = 120
	st.Title = "Page title"
	layout := LayoutChromeWithTabs(st, []TabView{{Title: "Only", Active: true}}, 0)
	var row strings.Builder
	for _, c := range layout.Header[:st.Width] {
		if c.R != 0 {
			row.WriteRune(c.R)
		}
	}
	if !strings.Contains(row.String(), "Page title") {
		t.Fatalf("single-tab toolbar lost the title: %q", row.String())
	}
	hasNew := false
	for _, hit := range layout.Hits {
		if hit.Kind == ChromeHitTab || hit.Kind == ChromeHitTabClose {
			t.Fatalf("single-tab toolbar has a tab hit: %+v", hit)
		}
		if hit.Kind == ChromeHitNewTab {
			hasNew = true
		}
	}
	if !hasNew {
		t.Fatal("single-tab toolbar has no + button")
	}
}

func TestTabStripSurvivesNarrowToolbar(t *testing.T) {
	st := chromeState()
	st.Width = 30
	tabs := []TabView{{Title: "One", Active: true}, {Title: "Two"}, {Title: "Three"}, {Title: "Four"}}
	layout := LayoutChromeWithTabs(st, tabs, 0)
	if len(layout.Header) != st.Width*st.HeaderRows {
		t.Fatalf("header cells = %d", len(layout.Header))
	}
	for _, hit := range layout.Hits {
		if hit.Kind != ChromeHitTab && hit.Kind != ChromeHitTabClose && hit.Kind != ChromeHitNewTab {
			continue
		}
		if hit.X < 0 || hit.X+hit.W > st.Width {
			t.Fatalf("tab hit outside the toolbar: %+v", hit)
		}
	}
}

func TestTabStripWidthAndScrolling(t *testing.T) {
	st := chromeState()
	st.Width = 200
	wide := []TabView{{Title: "One", Active: true}, {Title: "Two"}}
	layout := LayoutChromeWithTabs(st, wide, 0)
	for i := range wide {
		var tabHit, closeHit ChromeHit
		for _, hit := range layout.Hits {
			if hit.Index != i {
				continue
			}
			switch hit.Kind {
			case ChromeHitTab:
				tabHit = hit
			case ChromeHitTabClose:
				closeHit = hit
			}
		}
		if tabHit.W != 17 || closeHit.W != 2 {
			t.Fatalf("tab %d hit widths = %d/%d, want a 20-cell tab", i, tabHit.W, closeHit.W)
		}
	}
	for _, hit := range layout.Hits {
		if hit.Kind == ChromeHitTabPrev || hit.Kind == ChromeHitTabNext {
			t.Fatalf("two tabs that fit should not show scrolling arrows: %+v", hit)
		}
	}

	st.Width = 100
	two := []TabView{{Title: "Alpha", Active: true}, {Title: "Beta"}}
	layout = LayoutChromeWithTabs(st, two, 0)
	for i := range two {
		var tabHit, closeHit ChromeHit
		for _, hit := range layout.Hits {
			if hit.Index != i {
				continue
			}
			switch hit.Kind {
			case ChromeHitTab:
				tabHit = hit
			case ChromeHitTabClose:
				closeHit = hit
			}
		}
		if tabHit.W != 17 || closeHit.W != 2 {
			t.Fatalf("tab %d hit widths = %d/%d, want a 20-cell tab", i, tabHit.W, closeHit.W)
		}
	}
	for _, hit := range layout.Hits {
		if hit.Kind == ChromeHitTabPrev || hit.Kind == ChromeHitTabNext {
			t.Fatalf("two tabs should fit without arrows: %+v", hit)
		}
	}

	st.Width = 120
	many := make([]TabView, 12)
	for i := range many {
		many[i] = TabView{Title: fmt.Sprintf("Tab%d", i+1), Active: i == 0}
	}
	layout = LayoutChromeWithTabs(st, many, 0)
	visible := 0
	for _, hit := range layout.Hits {
		if hit.Kind == ChromeHitTab {
			visible++
		}
	}
	if visible < 1 || visible >= len(many) {
		t.Fatalf("visible tabs = %d, want a scrolling subset", visible)
	}
	prev, next := false, false
	for _, hit := range layout.Hits {
		switch hit.Kind {
		case ChromeHitTabPrev:
			prev = true
		case ChromeHitTabNext:
			next = true
		}
	}
	if !prev || !next {
		t.Fatalf("overflowing strip arrows: prev=%v next=%v", prev, next)
	}

	layout = LayoutChromeWithTabs(st, many, 5)
	var row strings.Builder
	for _, c := range layout.Header[:st.Width] {
		if c.R != 0 {
			row.WriteRune(c.R)
		}
	}
	if strings.Contains(row.String(), "1:Tab1 x") {
		t.Fatalf("offset did not hide the first tab: %q", row.String())
	}
	if !strings.Contains(row.String(), "6:Tab6") {
		t.Fatalf("offset did not show tab 6: %q", row.String())
	}
	for _, hit := range layout.Hits {
		if hit.Kind == ChromeHitTab && hit.Index < 5 {
			t.Fatalf("offset strip drew a hidden tab: %+v", hit)
		}
	}
}

func TestTabNumberColonAndCloseAreBold(t *testing.T) {
	st := chromeState()
	st.Width = 120
	tabs := []TabView{{Title: "Alpha", Active: true}, {Title: "Beta"}}
	layout := LayoutChromeWithTabs(st, tabs, 0)
	var tabHit, closeHit ChromeHit
	for _, hit := range layout.Hits {
		if hit.Kind == ChromeHitTab && hit.Index == 0 {
			tabHit = hit
		}
		if hit.Kind == ChromeHitTabClose && hit.Index == 0 {
			closeHit = hit
		}
	}
	cell := func(x int) protocol.Cell { return layout.Header[x] }
	if cell(tabHit.X+1).R != '1' || cell(tabHit.X+1).Attrs&protocol.AttrBold == 0 {
		t.Fatalf("tab number is not bold: %+v", cell(tabHit.X+1))
	}
	if cell(tabHit.X+2).R != ':' || cell(tabHit.X+2).Attrs&protocol.AttrBold == 0 {
		t.Fatalf("tab colon is not bold: %+v", cell(tabHit.X+2))
	}
	titleX := tabHit.X + 3
	if cell(titleX).R == ' ' {
		titleX++
	}
	if cell(titleX).R != 'A' || cell(titleX).Attrs&protocol.AttrBold != 0 {
		t.Fatalf("tab title styling = %+v", cell(titleX))
	}
	closeX := closeHit.X + 1
	if cell(closeX).R != 'x' || cell(closeX).Attrs&protocol.AttrBold == 0 {
		t.Fatalf("tab close is not bold: %+v", cell(closeX))
	}
}

func TestInactiveTabGradientAndDimTitle(t *testing.T) {
	st := chromeState()
	st.Width = 120
	tabs := []TabView{{Title: "Alpha", Active: true}, {Title: "Beta"}}
	layout := LayoutChromeWithTabs(st, tabs, 0)
	starts := map[int]int{}
	var inactiveClose ChromeHit
	for _, hit := range layout.Hits {
		if hit.Kind == ChromeHitTab {
			starts[hit.Index] = hit.X
		}
		if hit.Kind == ChromeHitTabClose && hit.Index == 1 {
			inactiveClose = hit
		}
	}
	active, inactive := starts[0], starts[1]
	for _, x := range []int{active, active + 19} {
		if layout.Header[x].BG != colSelectedBG {
			t.Fatalf("active tab background = %+v, want solid blue", layout.Header[x].BG)
		}
	}
	from, to := layout.Header[inactive].BG, layout.Header[inactive+19].BG
	if from == to || from == colSelectedBG || to == colSelectedBG {
		t.Fatalf("inactive tab gradient = %+v -> %+v", from, to)
	}
	titleX := inactive + 3
	if layout.Header[titleX].R != 'B' || layout.Header[titleX].FG != colTabDimFG {
		t.Fatalf("inactive title styling = %+v", layout.Header[titleX])
	}
	if layout.Header[inactive+1].FG != colHeaderFG || layout.Header[inactive+1].Attrs&protocol.AttrBold == 0 {
		t.Fatalf("inactive number styling = %+v", layout.Header[inactive+1])
	}
	if layout.Header[inactiveClose.X+1].R != 'x' || layout.Header[inactiveClose.X+1].Attrs&protocol.AttrBold == 0 {
		t.Fatalf("inactive close styling = %+v", layout.Header[inactiveClose.X+1])
	}
}

func TestPresenterDrawsTabStrip(t *testing.T) {
	st := chromeState()
	st.Width = 120
	var out bytes.Buffer
	p := NewPresenter(&out, true)
	p.SetState(st)
	p.SetTabs([]TabView{{Title: "Alpha", Active: true}, {Title: "Beta"}})
	p.SetTabOffset(0)
	f := protocol.Frame{W: st.Width, H: st.Height - st.HeaderRows - 1, Cells: make([]protocol.Cell, st.Width*(st.Height-st.HeaderRows-1))}
	for i := range f.Cells {
		f.Cells[i] = protocol.Cell{R: ' ', BG: protocol.Color{R: 255, G: 255, B: 255, A: 255, Set: true}}
	}
	if err := p.Present(f, 0, false); err != nil {
		t.Fatal(err)
	}
	text := testANSI.ReplaceAllString(out.String(), "")
	for _, want := range []string{"Alpha", "Beta"} {
		if !strings.Contains(text, want) {
			t.Errorf("presented output does not contain %q", want)
		}
	}
}
