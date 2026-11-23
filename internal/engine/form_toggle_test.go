package engine

import "testing"

func TestToggleControl(t *testing.T) {
	doc := ParseHTML(`<html><body><form>
		<input type="checkbox" name="c">
		<input type="radio" name="r" value="a" checked><input type="radio" name="r" value="b">
		<select name="m" multiple><option value="x" selected>x</option><option value="y">y</option></select>
		<select name="s"><option value="1">1</option></select>
	</form></body></html>`)
	checkbox := doc.FindFirst("input")
	if res := ToggleControl(checkbox); res.Action != ToggleValue || !checkbox.HasAttr("checked") {
		t.Fatalf("checkbox = %+v checked=%v", res, checkbox.HasAttr("checked"))
	}
	radios := doc.FindAll(func(n *Node) bool { return n.Tag == "input" && n.Attr("type") == "radio" })
	for _, n := range radios {
		n.Style = &Style{}
	}
	if res := ToggleControl(radios[1]); res.Action != ToggleValue || !radios[1].HasAttr("checked") || radios[0].HasAttr("checked") {
		t.Fatalf("radio = %+v first=%v second=%v", res, radios[0].HasAttr("checked"), radios[1].HasAttr("checked"))
	}
	selects := doc.FindAll(func(n *Node) bool { return n.Tag == "select" })
	if res := ToggleControl(selects[1]); res.Action != ToggleOpenSelect {
		t.Fatalf("single select = %+v", res)
	}
	opt := SelectOptions(selects[0])[0]
	if res := ToggleControl(opt); res.Action != ToggleValue || res.Select != selects[0] || opt.HasAttr("selected") {
		t.Fatalf("option = %+v selected=%v", res, opt.HasAttr("selected"))
	}
}
