package browser

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestToggleCheckbox(t *testing.T) {
	b := NewBrowser(40, 10)
	n := &Node{Type: ElementNode, Tag: "input", Attrs: []Attr{{Key: "type", Value: "checkbox"}, {Key: "name", Value: "x"}}}
	b.toggleControl(n)
	if !n.HasAttr("checked") {
		t.Fatal("checkbox was not checked")
	}
	b.toggleControl(n)
	if n.HasAttr("checked") {
		t.Fatal("checkbox was not unchecked")
	}
}

func TestToggleSelect(t *testing.T) {
	sel := &Node{Type: ElementNode, Tag: "select", Attrs: []Attr{{Key: "name", Value: "text-scale"}}}
	auto := &Node{Type: ElementNode, Tag: "option", Parent: sel, Attrs: []Attr{{Key: "selected", Value: ""}}}
	off := &Node{Type: ElementNode, Tag: "option", Parent: sel}
	auto.Children = []*Node{{Type: TextNode, Text: "auto", Parent: auto}}
	off.Children = []*Node{{Type: TextNode, Text: "off", Parent: off}}
	sel.Children = []*Node{auto, off}
	b := NewBrowser(40, 10)
	if got := inputValue(sel); got != "auto" {
		t.Fatalf("select value = %q", got)
	}
	b.toggleControl(sel)
	if b.selectMenu == nil || len(b.selectMenu.options) != 2 || b.selectMenu.idx != 0 {
		t.Fatalf("select popup = %+v", b.selectMenu)
	}
	b.HandleKey("down")
	b.HandleKey("enter")
	if b.selectMenu != nil {
		t.Fatal("select popup did not close after choosing")
	}
	if got := inputValue(sel); got != "off" {
		t.Fatalf("after choosing = %q", got)
	}
	b.toggleControl(sel)
	b.HandleKey("esc")
	if b.selectMenu != nil {
		t.Fatal("escape did not close the select popup")
	}
	if got := inputValue(sel); got != "off" {
		t.Fatalf("escape changed the value to %q", got)
	}
}

func TestSelectClickOpensAllOptions(t *testing.T) {
	doc := ParseHTML(`<html><body><form action="/submit">
		<select name="choice"><option value="one">One</option><option value="two" selected>Two</option><option value="three">Three</option></select>
	</form></body></html>`)
	u, _ := url.Parse("https://example.com/")
	b := NewBrowser(60, 20)
	b.page = &Page{URL: u, Base: u, Root: doc, Title: "form"}
	b.url = u
	b.rebuild()
	sel := doc.FindFirst("select")
	var rect Rect
	for i := range b.layout.Frags {
		f := &b.layout.Frags[i]
		if f.Kind == fragText && f.Node == sel {
			rect = Rect{X: f.X, Y: f.Y, W: f.W, H: f.H}
			break
		}
	}
	if rect.W == 0 {
		t.Fatal("select widget was not rendered")
	}
	clickDoc := func(x, docY int) {
		top, _ := b.viewport()
		sx, sy := x+1, docY-b.scroll+top+1
		b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: sx, Y: sy, Press: true})
		b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: sx, Y: sy})
	}
	clickDoc(rect.X, rect.Y)
	if b.selectMenu == nil || len(b.selectMenu.options) != 3 {
		t.Fatalf("select popup = %+v", b.selectMenu)
	}
	drawQuiet(b)
	if len(b.selectHits) != 3 {
		t.Fatalf("select popup rows = %+v", b.selectHits)
	}
	if b.modalBox.W < 40 {
		t.Fatalf("select popup width = %d, want at least 40", b.modalBox.W)
	}
	hit := b.selectHits[2]
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: hit.rect.X + 1, Y: hit.rect.Y + 1, Press: true})
	if b.selectMenu != nil {
		t.Fatal("select popup did not close after clicking an option")
	}
	if got := inputValue(sel); got != "three" {
		t.Fatalf("select value = %q, want three", got)
	}
}

func TestMultiSelectShowsAndTogglesAllOptions(t *testing.T) {
	doc := ParseHTML(`<html><body><form action="/submit">
		<select name="colours" multiple><option value="red">Red</option><option value="green" selected>Green</option><option value="blue">Blue</option></select>
	</form></body></html>`)
	u, _ := url.Parse("https://example.com/")
	b := NewBrowser(60, 20)
	b.page = &Page{URL: u, Base: u, Root: doc, Title: "form"}
	b.url = u
	b.rebuild()
	sel := doc.FindFirst("select")
	rows := map[*Node]int{}
	for i := range b.layout.Frags {
		f := &b.layout.Frags[i]
		if f.Kind == fragText {
			for p := f.Node; p != nil; p = p.Parent {
				if p.Tag == "option" {
					rows[p] = f.Y
					break
				}
			}
		}
	}
	opts := selectOptions(sel)
	if len(opts) != 3 {
		t.Fatalf("options = %d", len(opts))
	}
	if rows[opts[0]] == rows[opts[1]] || rows[opts[1]] == rows[opts[2]] {
		t.Fatalf("multi-select options are not on separate rows: %v", rows)
	}
	clickOption := func(opt *Node) {
		for i := range b.layout.Frags {
			f := &b.layout.Frags[i]
			if f.Kind == fragText && f.Node == opt {
				top, _ := b.viewport()
				b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: f.X + 1, Y: f.Y - b.scroll + top + 1, Press: true})
				return
			}
		}
		t.Fatalf("option %q has no clickable row", optionLabel(opt))
	}
	clickOption(opts[0])
	clickOption(opts[2])
	got := strings.Join(selectedOptionValues(sel), ",")
	if got != "red,green,blue" {
		t.Fatalf("selected values = %q, want red,green,blue", got)
	}
	clickOption(opts[1])
	got = strings.Join(selectedOptionValues(sel), ",")
	if got != "red,blue" {
		t.Fatalf("selected values after toggle = %q, want red,blue", got)
	}
}

func TestInputTypingAndSubmit(t *testing.T) {
	doc := ParseHTML(`<html><body><form action="/search" method="get">
		<input type="hidden" name="hl" value="en">
		<input name="q" value="" placeholder="try typing here">
		<input type="submit" value="Go">
	</form></body></html>`)
	u := mustParse(t, "https://www.google.com/")
	b := NewBrowser(60, 10)
	b.page = &Page{URL: u, Base: u, Root: doc}
	b.rebuild()

	q := inputByName(doc, "q")
	if q == nil {
		t.Fatal("input not found")
	}
	var field *Frag
	for i := range b.layout.Frags {
		if b.layout.Frags[i].Node == q {
			field = &b.layout.Frags[i]
			break
		}
	}
	if field == nil {
		t.Fatal("input fragment not laid out")
	}
	if !strings.Contains(formLabel(q), "try typing here") {
		t.Fatalf("unfocused label = %q", formLabel(q))
	}
	top, _ := b.viewport()
	b.HandleMouse(MouseEvent{
		Button: 0, X: field.X + 1, Y: top + field.Y - b.scroll + 1, Press: true,
	})
	b.HandleMouse(MouseEvent{
		Button: 0, X: field.X + 1, Y: top + field.Y - b.scroll + 1, Press: false,
	})
	if b.inputFocus != q || !q.InputFocused {
		t.Fatal("clicking the input did not focus it")
	}
	if strings.Contains(formLabel(q), "try typing here") {
		t.Fatalf("focused label still shows the placeholder: %q", formLabel(q))
	}
	if b.layout == nil || !b.layout.HasCursor {
		t.Fatal("focused input has no caret")
	}
	b.HandleKey("h")
	b.HandleKey("i")
	if got := inputValue(q); got != "hi" {
		t.Fatalf("typed value = %q", got)
	}
	b.HandleKey("backspace")
	if got := inputValue(q); got != "h" {
		t.Fatalf("after backspace = %q", got)
	}
	b.HandleKey("i")

	targetCh := make(chan string, 1)
	b.loadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
		select {
		case targetCh <- target:
		default:
		}
		return &Page{URL: u, Base: u, Root: ParseHTML("<html></html>")}, nil
	}
	b.HandleKey("enter")
	select {
	case target := <-targetCh:
		if !strings.Contains(target, "/search?") || !strings.Contains(target, "q=hi") ||
			!strings.Contains(target, "hl=en") {
			t.Fatalf("submitted URL = %q", target)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("form was not submitted")
	}
	if b.inputFocus != nil {
		t.Fatal("input focus was not cleared after submit")
	}
}

func TestFixedWidthInputPatchesFragments(t *testing.T) {
	doc := ParseHTML(`<html><body><form action="/go">
<input name="q" style="width:24em" value=""></form></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(60, 12)
	b.page = &Page{URL: u, Base: u, Root: doc}
	b.url = u
	b.rebuild()
	q := inputByName(doc, "q")
	b.setInputFocus(q)
	layout := b.layout
	b.setInputValue(q, "wikipedia search")
	if b.layout != layout {
		t.Fatal("fixed-width input triggered a full rebuild")
	}
	var frag *Frag
	for i := range b.layout.Frags {
		if b.layout.Frags[i].Kind == fragText && b.layout.Frags[i].Node == q {
			frag = &b.layout.Frags[i]
			break
		}
	}
	if frag == nil {
		t.Fatal("fixed-width input fragment missing")
	}
	if !strings.Contains(frag.Text, "wikipedia search") {
		t.Fatalf("fragment not updated in place: %q", frag.Text)
	}
	if stringWidth(frag.Text) != frag.W {
		t.Fatalf("fragment width changed: %d cells for %q", frag.W, frag.Text)
	}
}

func TestTabReachesAllFormControls(t *testing.T) {
	doc := ParseHTML(`<html><body>
<a href="#x">Link</a>
<form action="/go">
<input type="checkbox" name="check">
<input type="radio" name="pick" value="one" checked>
<input type="radio" name="pick" value="two">
<select name="single"><option>One</option><option>Two</option></select>
<select multiple name="multi"><option selected>Alpha</option><option>Beta</option></select>
<button type="button" name="btn">Press</button>
</form></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(60, 20)
	b.page = &Page{URL: u, Base: u, Root: doc}
	b.url = u
	b.rebuild()
	check := doc.FindFirst("input")
	radios := doc.FindAll(func(n *Node) bool { return n.Tag == "input" && n.Attr("type") == "radio" })
	single := doc.FindFirst("select")
	multi := doc.FindAll(func(n *Node) bool { return n.Tag == "select" && n.HasAttr("multiple") })[0]
	btn := doc.FindFirst("button")
	if check == nil || len(radios) != 2 || single == nil || multi == nil || btn == nil {
		t.Fatal("test form is incomplete")
	}

	b.HandleKey("tab")
	if b.focus != 0 {
		t.Fatalf("first tab focused link %d", b.focus)
	}
	b.HandleKey("tab")
	if b.controlFocus != check {
		t.Fatalf("tab did not reach checkbox: %v", b.controlFocus)
	}
	b.HandleKey(" ")
	if !check.HasAttr("checked") {
		t.Fatal("space did not toggle the checkbox")
	}
	b.HandleKey("tab")
	if b.controlFocus != radios[0] {
		t.Fatalf("tab did not reach radio group: %v", b.controlFocus)
	}
	b.HandleKey("l")
	if !radios[1].HasAttr("checked") || b.controlFocus != radios[1] {
		t.Fatal("l did not move the radio selection")
	}
	b.HandleKey("h")
	if !radios[0].HasAttr("checked") || b.controlFocus != radios[0] {
		t.Fatal("h did not move the radio selection back")
	}
	b.HandleKey("right")
	if !radios[1].HasAttr("checked") {
		t.Fatal("arrow did not move the radio selection")
	}
	b.HandleKey("tab")
	if b.controlFocus != single {
		t.Fatalf("tab from radio group reached %v", b.controlFocus)
	}
	b.HandleKey("j")
	if !selectOptions(single)[1].HasAttr("selected") {
		t.Fatal("j did not change the single select")
	}
	b.HandleKey("k")
	if !selectOptions(single)[0].HasAttr("selected") {
		t.Fatal("k did not change the single select back")
	}
	b.HandleKey("right")
	if !selectOptions(single)[1].HasAttr("selected") {
		t.Fatal("right arrow did not change the single select")
	}
	b.HandleKey("tab")
	if b.controlFocus != multi {
		t.Fatalf("tab did not reach multi-select: %v", b.controlFocus)
	}
	b.HandleKey("j")
	beta := selectOptions(multi)[1]
	if b.controlOption != beta {
		t.Fatalf("j did not move the active option: %v", b.controlOption)
	}
	b.HandleKey(" ")
	if !beta.HasAttr("selected") {
		t.Fatal("space did not toggle the multi-select option")
	}
	b.HandleKey("tab")
	if b.controlFocus != btn {
		t.Fatalf("tab did not reach button: %v", b.controlFocus)
	}
	b.HandleKey("esc")
	if b.controlFocus != nil {
		t.Fatal("escape did not clear control focus")
	}
}

func TestInputTabMovesFocus(t *testing.T) {
	doc := ParseHTML(`<html><body><form action="/go">
<input name="a" value="one">
<input name="disabled" value="skip" disabled>
<input name="hidden" type="hidden" value="skip">
<input name="b" value="two">
<textarea name="c">three</textarea>
<input name="readonly" value="skip" readonly>
<input name="d" value="four">
</form></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(60, 12)
	b.page = &Page{URL: u, Base: u, Root: doc}
	b.url = u
	b.rebuild()
	a := inputByName(doc, "a")
	b.setInputFocus(a)

	b.HandleKey("tab")
	if b.inputFocus == nil || b.inputFocus.Attr("name") != "b" {
		t.Fatalf("tab from a landed on %v", b.inputFocus)
	}
	b.HandleKey("tab")
	if b.inputFocus == nil || b.inputFocus.Attr("name") != "c" {
		t.Fatalf("tab from b landed on %v", b.inputFocus)
	}
	b.HandleKey("tab")
	if b.inputFocus != nil || b.controlFocus == nil || b.controlFocus.Attr("name") != "readonly" {
		t.Fatalf("tab from textarea landed on input=%v control=%v", b.inputFocus, b.controlFocus)
	}
	b.HandleKey("tab")
	if b.inputFocus == nil || b.inputFocus.Attr("name") != "d" {
		t.Fatalf("tab from readonly landed on %v", b.inputFocus)
	}
	b.HandleKey("tab")
	if b.inputFocus == nil || b.inputFocus.Attr("name") != "a" {
		t.Fatalf("tab did not wrap to a: %v", b.inputFocus)
	}
	b.HandleKey("backtab")
	if b.inputFocus == nil || b.inputFocus.Attr("name") != "d" {
		t.Fatalf("backtab did not wrap to d: %v", b.inputFocus)
	}
}

func TestInputCtrlASelectsAll(t *testing.T) {
	doc := ParseHTML(`<html><body><form action="/go"><input name="q" value="hello world"></form></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(60, 12)
	b.color = true
	b.page = &Page{URL: u, Base: u, Root: doc}
	b.url = u
	b.rebuild()
	q := inputByName(doc, "q")
	b.setInputFocus(q)

	b.HandleKey("ctrl-a")
	if !b.inputSelectAll {
		t.Fatal("ctrl-a did not select the field value")
	}
	drawQuiet(b)
	found := false
	for _, line := range b.prev {
		if strings.Contains(line, "48;2;31;79;159") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("selected field is not painted with the selection background: %v", b.prev)
	}

	// Typing replaces the whole selection.
	b.HandleKey("Z")
	if got := inputValue(q); got != "Z" {
		t.Fatalf("typing over selection = %q, want Z", got)
	}
	if b.inputSelectAll {
		t.Fatal("selection survived typing")
	}
	if q.InputCursor != 1 {
		t.Fatalf("cursor = %d, want 1", q.InputCursor)
	}

	// Backspace and Delete also clear the whole selection.
	b.setInputValue(q, "hello")
	q.InputCursor = 5
	b.HandleKey("ctrl-a")
	b.HandleKey("backspace")
	if got := inputValue(q); got != "" {
		t.Fatalf("backspace over selection = %q", got)
	}
	b.setInputValue(q, "hello")
	q.InputCursor = 5
	b.HandleKey("ctrl-a")
	b.HandleKey("delete")
	if got := inputValue(q); got != "" {
		t.Fatalf("delete over selection = %q", got)
	}

	// Moving the caret cancels the selection without editing.
	b.setInputValue(q, "hello")
	q.InputCursor = 5
	b.HandleKey("ctrl-a")
	b.HandleKey("left")
	if b.inputSelectAll {
		t.Fatal("cursor movement kept the selection")
	}
	if got := inputValue(q); got != "hello" {
		t.Fatalf("cursor movement edited the value: %q", got)
	}
}

func TestInputWordEditing(t *testing.T) {
	doc := ParseHTML(`<html><body><form action="/go"><input name="q" value="https://en.wikipedia.org/wiki/Wyoming"></form></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(60, 12)
	b.page = &Page{URL: u, Base: u, Root: doc}
	b.url = u
	b.rebuild()
	q := inputByName(doc, "q")
	b.setInputFocus(q)

	b.HandleKey("alt-backspace")
	if got := inputValue(q); got != "https://en.wikipedia.org/wiki/" {
		t.Fatalf("alt-backspace = %q", got)
	}
	b.HandleKey("ctrl-w")
	if got := inputValue(q); got != "https://en.wikipedia.org/" {
		t.Fatalf("ctrl-w = %q", got)
	}

	b.setInputValue(q, "https://example.com/a/b")
	q.InputCursor = len("https://example.com/a/b")
	b.HandleKey("alt-left")
	b.HandleKey("ctrl-left")
	b.HandleKey("Z")
	if got := inputValue(q); got != "https://example.com/Za/b" {
		t.Fatalf("word motion = %q", got)
	}

	b.setInputValue(q, "https://example.com/a")
	q.InputCursor = 0
	b.HandleKey("alt-d")
	if got := inputValue(q); got != "example.com/a" {
		t.Fatalf("alt-d = %q", got)
	}
}

func TestInputCursorMovementAndEditing(t *testing.T) {
	doc := ParseHTML(`<html><body><form action="/go"><input name="q" value="abcd"></form></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(60, 12)
	b.page = &Page{URL: u, Base: u, Root: doc}
	b.url = u
	b.rebuild()
	q := inputByName(doc, "q")
	if q == nil {
		t.Fatal("input not found")
	}
	b.setInputFocus(q)
	if q.InputCursor != 4 {
		t.Fatalf("focus cursor = %d, want 4", q.InputCursor)
	}
	if b.layout == nil || !b.layout.HasCursor {
		t.Fatal("focused input has no caret")
	}
	endX := b.layout.Cursor.X
	b.HandleKey("left")
	b.HandleKey("left")
	if q.InputCursor != 2 {
		t.Fatalf("cursor after two lefts = %d, want 2", q.InputCursor)
	}
	if b.layout.Cursor.X != endX-2 {
		t.Fatalf("caret did not move: x = %d, want %d", b.layout.Cursor.X, endX-2)
	}
	b.HandleKey("X")
	if got := inputValue(q); got != "abXcd" {
		t.Fatalf("value = %q, want abXcd", got)
	}
	if q.InputCursor != 3 {
		t.Fatalf("cursor after typing = %d, want 3", q.InputCursor)
	}
	b.HandleKey("home")
	if q.InputCursor != 0 {
		t.Fatalf("home cursor = %d", q.InputCursor)
	}
	b.HandleKey("delete")
	if got := inputValue(q); got != "bXcd" {
		t.Fatalf("value after delete = %q, want bXcd", got)
	}
	b.HandleKey("end")
	if q.InputCursor != len(inputValue(q)) {
		t.Fatalf("end cursor = %d", q.InputCursor)
	}
}

func TestTextareaCursorMovesBetweenRows(t *testing.T) {
	doc := ParseHTML(`<html><body><form action="/go"><textarea name="body">ab
cd</textarea></form></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(60, 12)
	b.page = &Page{URL: u, Base: u, Root: doc}
	b.url = u
	b.rebuild()
	area := doc.FindFirst("textarea")
	if area == nil {
		t.Fatal("textarea not found")
	}
	b.setInputFocus(area)
	if area.InputCursor != len(inputValue(area)) {
		t.Fatalf("focus cursor = %d", area.InputCursor)
	}
	row2 := b.layout.Cursor.Y
	b.HandleKey("up")
	if area.InputCursor != 2 {
		t.Fatalf("cursor after up = %d, want 2", area.InputCursor)
	}
	if b.layout.Cursor.Y == row2 {
		t.Fatal("textarea caret did not move to the previous row")
	}
	b.HandleKey("left")
	if area.InputCursor != 1 {
		t.Fatalf("cursor after left = %d, want 1", area.InputCursor)
	}
	b.HandleKey("down")
	if area.InputCursor != len("ab\nc") {
		t.Fatalf("cursor after down = %d", area.InputCursor)
	}
}

func TestPaintInputCursor(t *testing.T) {
	b := NewBrowser(20, 5)
	b.layout = &Layout{HasCursor: true, Cursor: Rect{X: 2, Y: 1, W: 1, H: 1}}
	g := NewGrid(20, 5, RGB(0, 0, 0), RGB(0x11, 0x22, 0x33))
	b.paintInputCursor(g, 2, 5)
	cell := g.Cells[3*20+2]
	if cell.BG != RGB(0xff, 0xff, 0xff) || cell.FG != RGB(0x2f, 0x6f, 0xbf) {
		t.Fatalf("caret cell colors = %+v", cell)
	}
}

func TestFormValidationConstraints(t *testing.T) {
	doc := ParseHTML(`<html><body><form action="/go">
		<input name="required-empty" required>
		<input name="pattern" pattern="[0-9]{3}" value="12a">
		<input name="date" type="date" value="2026-13-01">
		<input name="time" type="time" value="25:00">
		<input name="color" type="color" value="#zzzzzz">
		<input name="ok-date" type="date" value="2026-11-10">
		<input name="disabled-empty" required disabled>
	</form></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(60, 12)
	b.page = &Page{URL: u, Base: u, Root: doc}
	form := doc.FindFirst("form")

	field, msg := b.validateForm(form)
	if field == nil || field.Attr("name") != "required-empty" || !strings.Contains(msg, "required") {
		t.Fatalf("first validation = %v %q", field, msg)
	}
	setAttr(inputByName(doc, "required-empty"), "value", "ok")
	field, msg = b.validateForm(form)
	if field == nil || field.Attr("name") != "pattern" || !strings.Contains(msg, "pattern") {
		t.Fatalf("pattern validation = %v %q", field, msg)
	}
	setAttr(inputByName(doc, "pattern"), "value", "123")
	field, msg = b.validateForm(form)
	if field == nil || field.Attr("name") != "date" || !strings.Contains(msg, "date") {
		t.Fatalf("date validation = %v %q", field, msg)
	}
	setAttr(inputByName(doc, "date"), "value", "2026-11-10")
	field, msg = b.validateForm(form)
	if field == nil || field.Attr("name") != "time" || !strings.Contains(msg, "time") {
		t.Fatalf("time validation = %v %q", field, msg)
	}
	setAttr(inputByName(doc, "time"), "value", "14:30")
	field, msg = b.validateForm(form)
	if field == nil || field.Attr("name") != "color" || !strings.Contains(msg, "color") {
		t.Fatalf("color validation = %v %q", field, msg)
	}
	setAttr(inputByName(doc, "color"), "value", "#ff0000")
	if field, msg := b.validateForm(form); field != nil {
		t.Fatalf("valid form rejected: %v %q", field, msg)
	}
}

func TestInputTypeFormatValidation(t *testing.T) {
	cases := []struct {
		typ, value string
		valid      bool
	}{
		{"date", "2026-11-10", true},
		{"date", "2026-02-30", false},
		{"time", "23:59", true},
		{"time", "24:00", false},
		{"datetime-local", "2026-11-10T14:30", true},
		{"datetime-local", "2026-11-10 14:30", false},
		{"month", "2026-11", true},
		{"month", "2026-13", false},
		{"week", "2026-W45", true},
		{"week", "2026-W99", false},
		{"color", "#2f6fbf", true},
		{"color", "#2f6fb", false},
	}
	for _, tc := range cases {
		got := invalidControlValue(nil, tc.typ, tc.value) == ""
		if got != tc.valid {
			t.Errorf("%s %q valid=%v, want %v", tc.typ, tc.value, got, tc.valid)
		}
	}
}

func TestMaxlengthReadonlyDisabled(t *testing.T) {
	doc := ParseHTML(`<html><body><form action="/go">
		<input name="short" maxlength="3">
		<input name="ro" value="fixed" readonly>
		<input name="off" value="hidden" disabled>
		<textarea name="ro-area" readonly>fixed area</textarea>
		<input name="off-check" type="checkbox" checked disabled>
		<button type="submit" disabled>No</button>
	</form></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(60, 12)
	b.page = &Page{URL: u, Base: u, Root: doc}
	b.url = u
	b.rebuild()
	short := inputByName(doc, "short")
	b.setInputFocus(short)
	for _, k := range []string{"a", "b", "c", "d", "e"} {
		b.HandleKey(k)
	}
	if got := inputValue(short); got != "abc" {
		t.Fatalf("maxlength value = %q", got)
	}
	if isTextInput(inputByName(doc, "ro")) || isTextInput(doc.FindFirst("textarea")) {
		t.Fatal("readonly control is editable")
	}
	if isTextInput(inputByName(doc, "off")) || isToggleControl(inputByName(doc, "off-check")) {
		t.Fatal("disabled control is interactive")
	}
	if isSubmitControl(doc.FindFirst("button")) {
		t.Fatal("disabled button is a submit control")
	}
	if field, msg := b.validateForm(doc.FindFirst("form")); field != nil {
		t.Fatalf("disabled controls affected validation: %v %q", field, msg)
	}
}

func TestRangeKeyboardControl(t *testing.T) {
	doc := ParseHTML(`<html><body><form action="/go"><input name="r" type="range" min="0" max="10" step="2" value="4"></form></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(60, 12)
	b.page = &Page{URL: u, Base: u, Root: doc}
	b.url = u
	b.rebuild()
	r := inputByName(doc, "r")
	b.setInputFocus(r)
	if b.inputFocus != r {
		t.Fatal("range was not focused")
	}
	b.HandleKey("right")
	if got := inputValue(r); got != "6" {
		t.Fatalf("range after right = %q", got)
	}
	b.HandleKey("home")
	if got := inputValue(r); got != "0" {
		t.Fatalf("range home = %q", got)
	}
	b.HandleKey("end")
	if got := inputValue(r); got != "10" {
		t.Fatalf("range end = %q", got)
	}
	if label := formLabel(r); !strings.Contains(label, "█") || !strings.Contains(label, "10") || !strings.Contains(label, "- ") || !strings.Contains(label, " + ") {
		t.Fatalf("range label = %q", label)
	}
	var frag *Frag
	for i := range b.layout.Frags {
		if b.layout.Frags[i].Kind == fragText && b.layout.Frags[i].Node == r {
			frag = &b.layout.Frags[i]
			break
		}
	}
	if frag == nil {
		t.Fatal("range widget fragment not found")
	}
	top, _ := b.viewport()
	clickAt := func(rel int) {
		b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: frag.X + rel + 1, Y: frag.Y - b.scroll + top + 1, Press: true})
	}
	minus := strings.Index(frag.Text, "-")
	clickAt(stringWidth(frag.Text[:minus]))
	if got := inputValue(r); got != "8" {
		t.Fatalf("range after minus click = %q", got)
	}
	b.rebuild()
	for i := range b.layout.Frags {
		if b.layout.Frags[i].Kind == fragText && b.layout.Frags[i].Node == r {
			frag = &b.layout.Frags[i]
			break
		}
	}
	plus := strings.LastIndex(frag.Text, "+")
	clickAt(stringWidth(frag.Text[:plus]))
	if got := inputValue(r); got != "10" {
		t.Fatalf("range after plus click = %q", got)
	}
}

func TestLabelAssociations(t *testing.T) {
	doc := ParseHTML(`<html><body><form action="/go">
		<label for="query">Query label</label>
		<input id="query" name="q">
		<p><label><input id="agree" type="checkbox" name="agree"> Accept terms</label></p>
		<label for="off">Disabled label</label>
		<input id="off" name="off" disabled>
	</form></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(70, 20)
	b.page = &Page{URL: u, Base: u, Root: doc}
	b.url = u
	b.rebuild()
	clickText := func(text string) {
		for i := range b.layout.Frags {
			f := &b.layout.Frags[i]
			if f.Kind != fragText || !strings.Contains(f.Text, text) {
				continue
			}
			top, _ := b.viewport()
			x, y := f.X+1, f.Y-b.scroll+top+1
			b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: x, Y: y, Press: true})
			b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: x, Y: y})
			return
		}
		t.Fatalf("label text %q not found", text)
	}
	query := inputByName(doc, "q")
	clickText("Query")
	if b.inputFocus != query {
		t.Fatal("label for= did not focus the input")
	}
	agree := inputByName(doc, "agree")
	clickText("Accept")
	if !agree.HasAttr("checked") {
		t.Fatal("wrapping label did not toggle the checkbox")
	}
	b.HandleKey("esc")
	clickText("Disabled")
	if b.inputFocus != nil {
		t.Fatal("label focused a disabled control")
	}
}

func TestNonSubmitButtonClickRunsHandler(t *testing.T) {
	page := newJSPage(`<html><body><p>intro</p><button id="b" type="button">Got it</button><p id="out">visible</p></body></html>`,
		`document.getElementById("b").addEventListener("click", function () {
			document.getElementById("out").textContent = "hidden";
		});`)
	b := NewBrowser(60, 10)
	b.page = page
	withQuietStdout(t, func() { b.startJS(page) })
	b.rebuild()

	btn := page.Root.FindFirst("button")
	if btn == nil {
		t.Fatal("button not found")
	}
	var field *Frag
	for i := range b.layout.Frags {
		if b.layout.Frags[i].Node == btn {
			field = &b.layout.Frags[i]
			break
		}
	}
	if field == nil {
		t.Fatal("button fragment not laid out")
	}
	top, _ := b.viewport()
	x := field.X + 1
	y := top + field.Y - b.scroll + 1
	withQuietStdout(t, func() {
		b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: x, Y: y, Press: true})
		b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: x, Y: y, Press: false})
	})
	if got := findByID(page.Root, "out"); got != nil {
		if got.TextContent() != "hidden" {
			t.Fatalf("click handler did not run, text = %q", got.TextContent())
		}
	} else {
		t.Fatal("output paragraph missing")
	}
}

func TestMouseClickSubmitButton(t *testing.T) {
	doc := ParseHTML(`<html><body><form action="/go" method="get">
		<input name="q" value="x"><button type="submit">Go</button>
	</form></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(60, 10)
	b.page = &Page{URL: u, Base: u, Root: doc}
	b.rebuild()
	btn := doc.FindFirst("button")
	if btn == nil {
		t.Fatal("button not found")
	}
	var field *Frag
	for i := range b.layout.Frags {
		if b.layout.Frags[i].Node == btn {
			field = &b.layout.Frags[i]
			break
		}
	}
	if field == nil {
		t.Fatal("button fragment not laid out")
	}
	targetCh := make(chan string, 1)
	b.loadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
		select {
		case targetCh <- target:
		default:
		}
		return &Page{URL: u, Base: u, Root: ParseHTML("<html></html>")}, nil
	}
	top, _ := b.viewport()
	x := field.X + 1
	y := top + field.Y - b.scroll + 1
	withQuietStdout(t, func() {
		b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: x, Y: y, Press: true})
		b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: x, Y: y, Press: false})
	})
	select {
	case target := <-targetCh:
		if !strings.Contains(target, "/go?") || !strings.Contains(target, "q=x") {
			t.Fatalf("submitted URL = %q", target)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("clicking the submit button did not submit the form")
	}
}

func TestHomeSearchFormUsesURLResolution(t *testing.T) {
	newHome := func(value string) (*Browser, *Node, chan string) {
		u := mustParse(t, "about:home")
		page := &Page{URL: u, Base: u, Root: ParseHTML(startPageHTML), Title: "MOB³"}
		b := NewBrowser(80, 20)
		b.page = page
		b.url = u
		b.loader.searchURL = "https://search.example/?q=%s"
		input := page.Root.FindFirst("input")
		if input == nil || input.Attr("name") != "q" {
			t.Fatal("home page has no search input")
		}
		input.Value = value
		input.ValueSet = true
		form := page.Root.FindFirst("form")
		if form == nil {
			t.Fatal("home page has no search form")
		}
		ch := make(chan string, 1)
		b.loadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
			ch <- target
			return &Page{URL: mustParse(t, "https://example.com/"), Base: u, Root: ParseHTML("<html></html>")}, nil
		}
		return b, form, ch
	}
	submit := func(b *Browser, form *Node, ch chan string) string {
		t.Helper()
		b.submitForm(form)
		select {
		case target := <-ch:
			return target
		case <-time.After(2 * time.Second):
			t.Fatal("home search form did not navigate")
			return ""
		}
	}

	b, form, ch := newHome("golang tutorial")
	if got := submit(b, form, ch); got != "https://search.example/?q=golang+tutorial" {
		t.Fatalf("search target = %q", got)
	}

	b, form, ch = newHome("example.com")
	if got := submit(b, form, ch); got != "https://example.com" {
		t.Fatalf("URL target = %q", got)
	}

	b, form, ch = newHome("   ")
	b.submitForm(form)
	if b.status != "Enter a URL or search query" {
		t.Fatalf("empty status = %q", b.status)
	}
	select {
	case target := <-ch:
		t.Fatalf("empty search navigated to %q", target)
	default:
	}
}

func TestFormPOSTSubmission(t *testing.T) {
	doc := ParseHTML(`<html><body><form action="/submit" method="post">
		<input type="hidden" name="hl" value="en">
		<input name="q" value="hi">
	</form></body></html>`)
	u := mustParse(t, "https://example.com/form")
	b := NewBrowser(40, 10)
	b.page = &Page{URL: u, Base: u, Root: doc}
	b.rebuild()

	type req struct {
		method, target, ctype, body string
	}
	ch := make(chan req, 1)
	b.loadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
		ch <- req{method: method, target: target, ctype: contentType, body: string(body)}
		return &Page{URL: u, Base: u, Root: ParseHTML("<html></html>")}, nil
	}
	b.submitForm(doc.FindFirst("form"))
	select {
	case got := <-ch:
		if got.method != "POST" {
			t.Errorf("method = %q", got.method)
		}
		if got.target != "https://example.com/submit" {
			t.Errorf("target = %q", got.target)
		}
		if got.body != "hl=en&q=hi" {
			t.Errorf("body = %q", got.body)
		}
		if got.ctype != "application/x-www-form-urlencoded" {
			t.Errorf("content type = %q", got.ctype)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("POST form was not submitted")
	}
}

func TestFileInputClickShowsUnsupportedNotice(t *testing.T) {
	u := mustParse(t, "https://example.com/form")
	doc := ParseHTML(`<form action="/upload" method="post"><input type="file" name="f"><button>Send</button></form>`)
	b := NewBrowser(60, 15)
	b.page = &Page{URL: u, Base: u, Root: doc}
	b.url = u
	b.rebuild()
	file := inputByName(doc, "f")
	if file == nil {
		t.Fatal("file input not found")
	}
	var field *Frag
	for i := range b.layout.Frags {
		if b.layout.Frags[i].Node == file {
			field = &b.layout.Frags[i]
			break
		}
	}
	if field == nil {
		t.Fatal("file input fragment not laid out")
	}
	top, _ := b.viewport()
	x := field.X + 1
	y := top + field.Y - b.scroll + 1
	withQuietStdout(t, func() {
		b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: x, Y: y, Press: true})
		b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: x, Y: y, Press: false})
	})
	if b.notice == nil {
		t.Fatal("clicking the file input did not open a notice")
	}
	if !strings.Contains(b.notice.title, "not supported") ||
		!strings.Contains(b.notice.message, "not supported") {
		t.Fatalf("notice = %+v", b.notice)
	}
	drawQuiet(b)
	if screen := strings.Join(b.prev, "\n"); !strings.Contains(screen, "not supported") {
		t.Fatalf("upload notice not rendered:\n%s", screen)
	}
	b.HandleKey("esc")
	if b.notice != nil {
		t.Fatal("Esc did not close the upload notice")
	}
}

func TestTextareaEnterInsertsNewlineAndEscExits(t *testing.T) {
	u := mustParse(t, "https://example.com/form")
	doc := ParseHTML(`<form action="/submit" method="post"><textarea name="t"></textarea></form>`)
	b := NewBrowser(60, 15)
	b.page = &Page{URL: u, Base: u, Root: doc}
	b.url = u
	b.rebuild()
	area := doc.FindFirst("textarea")
	if area == nil {
		t.Fatal("textarea not found")
	}
	b.setInputFocus(area)
	b.HandleKey("a")
	b.HandleKey("enter")
	b.HandleKey("b")
	if got := inputValue(area); got != "a\nb" {
		t.Fatalf("value = %q, want %q", got, "a\nb")
	}
	if b.inputFocus != area {
		t.Fatal("Enter left the textarea")
	}
	if b.url.String() != "https://example.com/form" {
		t.Fatalf("Enter navigated to %s", b.url)
	}
	b.HandleKey("esc")
	if b.inputFocus != nil || area.InputFocused {
		t.Fatal("Esc did not exit the textarea")
	}
}

func TestTextareaTypingPatchesLayout(t *testing.T) {
	u := mustParse(t, "https://example.com/form")
	doc := ParseHTML(`<form action="/submit"><textarea name="t">` + strings.Repeat("a", 50) + `</textarea></form>`)
	b := NewBrowser(60, 15)
	b.page = &Page{URL: u, Base: u, Root: doc}
	b.url = u
	b.rebuild()
	area := doc.FindFirst("textarea")
	b.setInputFocus(area)
	before := b.layout
	b.HandleKey("x")
	if b.layout != before {
		t.Fatal("typing triggered a full relayout")
	}
	found := false
	for _, f := range b.layout.Frags {
		if f.Kind == fragText && f.Node == area && strings.Contains(f.Text, "x") {
			found = true
		}
	}
	if !found {
		t.Fatal("patched layout does not show the typed character")
	}
}

// TestLongSelectMenuScrolls pins the shared choice-menu painter's scroll
// window: a long option list follows the active row, and a click maps to the
// option actually drawn under the pointer rather than the first one.
func TestLongSelectMenuScrolls(t *testing.T) {
	var opts strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&opts, `<option value="v%d">Option %d</option>`, i, i)
	}
	doc := ParseHTML(`<html><body><form action="/submit"><select name="n">` + opts.String() + `</select></form></body></html>`)
	u, _ := url.Parse("https://example.com/")
	b := NewBrowser(60, 12)
	b.page = &Page{URL: u, Base: u, Root: doc, Title: "form"}
	b.url = u
	b.rebuild()
	sel := doc.FindFirst("select")
	b.openSelectMenu(sel)
	if b.selectMenu == nil || len(b.selectMenu.options) != 40 {
		t.Fatalf("select popup = %+v", b.selectMenu)
	}
	for i := 0; i < 39; i++ {
		b.HandleKey("down")
	}
	drawQuiet(b)
	m := b.selectMenu
	if m == nil || m.idx != 39 {
		t.Fatalf("popup = %+v, want the last option active", m)
	}
	if m.scroll == 0 {
		t.Fatal("popup did not scroll a long option list")
	}
	if len(b.selectHits) == 0 || b.selectHits[0].option != selectOptions(sel)[m.scroll] {
		t.Fatal("first visible row does not map to the scrolled option")
	}
	scroll := m.scroll
	first := b.selectHits[0].rect
	b.HandleMouse(MouseEvent{Button: MouseButtonLeft, X: first.X + 1, Y: first.Y + 1, Press: true})
	if b.selectMenu != nil {
		t.Fatal("popup stayed open after clicking a row")
	}
	if got, want := inputValue(sel), fmt.Sprintf("v%d", scroll); got != want {
		t.Fatalf("select value = %q, want %q", got, want)
	}
}
