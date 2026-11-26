package engine

import (
	"strings"
	"testing"
	"time"
)

func TestAbsoluteOutOfFlowAndOffsets(t *testing.T) {
	ly := build(`<div>first</div><div style="position:absolute;top:80px;left:24px">abs</div><div>after</div>`, 40)
	abs := textFrag(ly, "abs")
	after := textFrag(ly, "after")
	if abs == nil || after == nil {
		t.Fatalf("missing fragments: abs=%v after=%v", abs, after)
	}
	if abs.X != 3 || abs.Y != 5 {
		t.Fatalf("absolute offset = (%d,%d), want (3,5)", abs.X, abs.Y)
	}
	if after.Y > abs.Y {
		t.Fatalf("absolute box pushed following flow: after.Y=%d abs.Y=%d", after.Y, abs.Y)
	}
}

func TestAbsoluteContainingBlock(t *testing.T) {
	ly := build(`<div style="position:relative;height:160px"><div style="position:absolute;top:16px;left:16px">child</div></div><div>after</div>`, 40)
	child := textFrag(ly, "child")
	after := textFrag(ly, "after")
	if child == nil || after == nil {
		t.Fatalf("missing fragments: child=%v after=%v", child, after)
	}
	if child.X != 2 || child.Y != 1 {
		t.Fatalf("child at (%d,%d), want (2,1)", child.X, child.Y)
	}
	if after.Y < 10 {
		t.Fatalf("following flow did not clear the 160px containing block: after.Y=%d", after.Y)
	}
}

func TestAbsoluteRightBottom(t *testing.T) {
	ly := build(`<div>body</div><div style="position:absolute;right:0;bottom:0">abc</div>`, 30)
	abs := textFrag(ly, "abc")
	if abs == nil {
		t.Fatal("missing absolute fragment")
	}
	if abs.X != 27 || abs.Y != 0 {
		t.Fatalf("right/bottom position = (%d,%d), want (27,0)", abs.X, abs.Y)
	}
}

func TestAbsolutePercentageOffset(t *testing.T) {
	ly := build(`<div>body</div><div style="position:absolute;left:50%;top:0">abcd</div>`, 40)
	abs := textFrag(ly, "abcd")
	if abs == nil {
		t.Fatal("missing absolute fragment")
	}
	if abs.X != 20 {
		t.Fatalf("50%% left = %d, want 20", abs.X)
	}
}

func TestAbsoluteStretchBetweenOffsets(t *testing.T) {
	ly := build(`<div style="position:relative;height:80px;background:#eee"><div style="position:absolute;top:0;left:0;right:0;bottom:0;background:#ff0000"></div></div>`, 30)
	red := RGB(0xff, 0, 0)
	for _, f := range ly.Frags {
		if f.Kind == FragFill && f.Color == red {
			if f.X != 0 || f.Y != 0 || f.W != 30 || f.H != 5 {
				t.Fatalf("stretched box = %+v, want x=0 y=0 w=30 h=5", f)
			}
			return
		}
	}
	t.Fatal("no stretched absolute background")
}

func TestAbsolutePaintsAboveFlow(t *testing.T) {
	ly := build(`<div style="position:absolute;top:0;left:0">abs</div><p>flow</p>`, 30)
	absIdx, flowIdx := -1, -1
	for i, f := range ly.Frags {
		if f.Kind != FragText {
			continue
		}
		switch strings.TrimSpace(f.Text) {
		case "abs":
			absIdx = i
		case "flow":
			flowIdx = i
		}
	}
	if absIdx < 0 || flowIdx < 0 {
		t.Fatalf("missing fragments: abs=%d flow=%d", absIdx, flowIdx)
	}
	if absIdx < flowIdx {
		t.Fatalf("absolute painted below flow: abs=%d flow=%d", absIdx, flowIdx)
	}
}

func TestRelativeOffsetMovesOnlyItsBox(t *testing.T) {
	ly := build(`<div style="position:relative;top:16px;left:16px">shift</div><div>next</div>`, 30)
	shift := textFrag(ly, "shift")
	next := textFrag(ly, "next")
	if shift == nil || next == nil {
		t.Fatalf("missing fragments: shift=%v next=%v", shift, next)
	}
	if shift.X != 2 || shift.Y != 1 {
		t.Fatalf("relative box at (%d,%d), want (2,1)", shift.X, shift.Y)
	}
	if next.X != 0 || next.Y != 1 {
		t.Fatalf("sibling moved with relative box: (%d,%d), want (0,1)", next.X, next.Y)
	}
}

func TestAbsoluteNestedInRelative(t *testing.T) {
	ly := build(`<div style="position:relative;left:16px"><div style="position:absolute;top:0;left:0">nested</div></div>`, 30)
	nested := textFrag(ly, "nested")
	if nested == nil {
		t.Fatal("missing nested absolute fragment")
	}
	// The relative parent shifts to x=2 and its containing block follows, so
	// the child starts at the parent's new left edge.
	if nested.X != 2 || nested.Y != 0 {
		t.Fatalf("nested absolute at (%d,%d), want (2,0)", nested.X, nested.Y)
	}
}

func TestFixedDumpFallsBackToPageCoordinates(t *testing.T) {
	ly := build(`<div style="height:80px">top</div><div style="position:fixed;bottom:0">bar</div>`, 30)
	bar := textFrag(ly, "bar")
	if bar == nil {
		t.Fatal("fixed fragment missing in dump layout")
	}
	if bar.Fixed {
		t.Fatal("dump layout should not mark fixed fragments as viewport-anchored")
	}
	if ly.Height < bar.Y+1 {
		t.Fatalf("page height %d does not include fixed content at %d", ly.Height, bar.Y)
	}
}

func TestFixedAnchorsToViewportBottomRight(t *testing.T) {
	ly := buildViewport(`<div>top</div><div style="position:fixed;bottom:0;right:0">bar</div>`, 30, 10)
	bar := textFrag(ly, "bar")
	if bar == nil {
		t.Fatal("fixed fragment not rendered")
	}
	if bar.X != 27 || bar.Y != 9 {
		t.Fatalf("fixed box at (%d,%d), want (27,9)", bar.X, bar.Y)
	}
	if !bar.Fixed || bar.FixedY != 9 {
		t.Fatalf("fixed marker = %v FixedY=%d", bar.Fixed, bar.FixedY)
	}
	if ly.Height != 1 {
		t.Fatalf("fixed box changed page height: %d", ly.Height)
	}
}

func TestFixedIgnoresRelativeAncestor(t *testing.T) {
	ly := buildViewport(`<div style="position:relative;margin-left:40px;padding-top:40px"><div style="position:fixed;bottom:0;right:0">bar</div></div>`, 30, 10)
	bar := textFrag(ly, "bar")
	if bar == nil {
		t.Fatal("fixed fragment not rendered")
	}
	if bar.X != 27 || bar.Y != 9 {
		t.Fatalf("fixed box at (%d,%d), want viewport (27,9)", bar.X, bar.Y)
	}
}

func TestFixedScrollKeepsViewportPosition(t *testing.T) {
	ly := buildViewport(`<div>top</div><div style="position:fixed;bottom:0;right:0">bar</div>`, 30, 10)
	ly.SetFixedScroll(4)
	bar := textFrag(ly, "bar")
	if bar == nil {
		t.Fatal("fixed fragment missing after scroll")
	}
	// Document coordinates shift with scroll so the painted viewport row stays
	// at 9 once PaintLayout subtracts the scroll offset.
	if bar.Y != 13 {
		t.Fatalf("scrolled fixed Y = %d, want 13", bar.Y)
	}
}

func TestHideFixedElements(t *testing.T) {
	doc := ParseHTML(`<html><body><p>body</p><div id="fx" style="position:fixed;top:0;left:0">fixed</div></body></html>`)
	show := BuildLayoutBudgetDeadlineOptions(doc, nil, 40, 10, false, false, false, time.Time{}, DefaultLayoutBudget)
	hidden := BuildLayoutBudgetDeadlineOptions(doc, nil, 40, 10, false, false, true, time.Time{}, DefaultLayoutBudget)
	fixedFrags := func(ly *Layout) int {
		n := 0
		for _, f := range ly.Frags {
			if f.Fixed {
				n++
			}
		}
		return n
	}
	if fixedFrags(show) == 0 {
		t.Fatal("normal layout has no fixed fragments")
	}
	if got := fixedFrags(hidden); got != 0 {
		t.Fatalf("hidden layout has %d fixed fragments", got)
	}
}

func TestAbsoluteWidthResolvedOnce(t *testing.T) {
	doc := ParseHTML(`<div id="abs" style="position:absolute;width:25%;height:20px;padding:0 8px;box-sizing:border-box"></div>`)
	ly := BuildLayout(doc, nil, 100)
	n := doc.FindFirst("div")
	if n == nil {
		t.Fatal("absolute box missing")
	}
	if box := ly.Boxes[n]; box.W != 25 {
		t.Fatalf("absolute width = %d, want 25 (25%% of 100, resolved once)", box.W)
	}
}

func TestOffscreenAbsoluteAndFixedBoxesAreSuppressed(t *testing.T) {
	doc := ParseHTML(`<html><body style="margin:0">
<a id="skip" style="position:absolute;left:-9999px;top:0">skip link</a>
<div id="menu" style="position:absolute;left:-100%;top:0;width:100%">off-screen menu</div>
<div id="fixed" style="position:fixed;left:0;top:-9999px">fixed menu</div>
<div id="partial" style="position:absolute;left:-2px;top:48px;width:80px">partial</div>
<p>visible content</p>
</body></html>`)
	ly := BuildLayout(doc, nil, 40)
	var all strings.Builder
	for _, f := range ly.Frags {
		if f.Kind == FragText {
			all.WriteString(f.Text)
			all.WriteByte('\n')
		}
	}
	text := all.String()
	for _, hidden := range []string{"skip link", "off-screen menu", "fixed menu"} {
		if strings.Contains(text, hidden) {
			t.Fatalf("off-screen %q rendered", hidden)
		}
	}
	if !strings.Contains(text, "partial") {
		t.Fatal("partially visible absolute box was suppressed")
	}
	if !strings.Contains(text, "visible") || !strings.Contains(text, "content") {
		t.Fatal("visible content was suppressed")
	}
}

func TestAbsoluteChildOfShiftedAbsoluteBox(t *testing.T) {
	doc := ParseHTML(`<div style="position:relative;width:400px;height:64px">` +
		`<div id="w" style="position:absolute;right:0;top:0">` +
		`<div id="c" style="position:absolute;right:0;top:0;width:40px;height:16px">X</div>` +
		`</div></div>`)
	ly := BuildLayout(doc, nil, 100)
	for _, f := range ly.Frags {
		if f.Kind == FragText && strings.Contains(f.Text, "X") {
			if f.X < 40 {
				t.Fatalf("absolute child x = %d, want near the parent's right edge", f.X)
			}
			return
		}
	}
	t.Fatal("absolute child text missing")
}
