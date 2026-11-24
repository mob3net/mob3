package frontend

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"browser/internal/protocol"
)

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
	for _, want := range []string{"1 Alpha", "2 Beta"} {
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
		w := 0
		for _, hit := range layout.Hits {
			if hit.Index == i && (hit.Kind == ChromeHitTab || hit.Kind == ChromeHitTabClose) {
				w += hit.W
			}
		}
		if w != 30 {
			t.Fatalf("tab %d width = %d, want the 30-cell cap", i, w)
		}
	}
	for _, hit := range layout.Hits {
		if hit.Kind == ChromeHitTabPrev || hit.Kind == ChromeHitTabNext {
			t.Fatalf("two tabs that fit should not show scrolling arrows: %+v", hit)
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
	if strings.Contains(row.String(), "1 Tab1 ") {
		t.Fatalf("offset did not hide the first tab: %q", row.String())
	}
	if !strings.Contains(row.String(), "6 Tab6") {
		t.Fatalf("offset did not show tab 6: %q", row.String())
	}
	for _, hit := range layout.Hits {
		if hit.Kind == ChromeHitTab && hit.Index < 5 {
			t.Fatalf("offset strip drew a hidden tab: %+v", hit)
		}
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
	for _, want := range []string{"1 Alpha", "2 Beta"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("presented output does not contain %q", want)
		}
	}
}
