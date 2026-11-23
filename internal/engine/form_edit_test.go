package engine

import (
	"net/url"
	"testing"
)

func TestInputLineHelpers(t *testing.T) {
	s := "ab\ncde\nf"
	if got := InputLineStart(s, 5); got != 3 {
		t.Fatalf("line start = %d, want 3", got)
	}
	if got := InputLineEnd(s, 4); got != 6 {
		t.Fatalf("line end = %d, want 6", got)
	}
	if got := TextareaVerticalOffset(s, 4, -1); got != 1 {
		t.Fatalf("up = %d, want 1", got)
	}
	if got := TextareaVerticalOffset(s, 4, 1); got != 8 {
		t.Fatalf("down = %d, want 8", got)
	}
}

func TestTabControlPredicates(t *testing.T) {
	input := &Node{Type: ElementNode, Tag: "input", Attrs: []Attr{{Key: "type", Value: "hidden"}}}
	if IsTabControl(input) {
		t.Fatal("hidden input is tabbable")
	}
	input.Attrs[0].Value = "text"
	if !IsTabControl(input) {
		t.Fatal("text input is not tabbable")
	}
	if !IsToggleControl(&Node{Type: ElementNode, Tag: "input", Attrs: []Attr{{Key: "type", Value: "checkbox"}}}) {
		t.Fatal("checkbox is not toggle")
	}
}

func TestFormRequestGetAndPost(t *testing.T) {
	base, _ := url.Parse("https://example.com/base")
	doc := ParseHTML(`<html><body><form action="/go" method="post">
		<input name="a" value="1">
		<input name="check" type="checkbox" value="yes" checked>
		<input name="off" type="checkbox">
		<select name="s"><option value="x" selected>x</option></select>
		<input name="skip" type="file">
	</form></body></html>`)
	form := doc.FindFirst("form")
	method, target, body, ctype, ok := FormRequest(form, base)
	if !ok || method != "POST" || target != "https://example.com/go" || ctype == "" {
		t.Fatalf("post = %s %s %q %v", method, target, ctype, ok)
	}
	if string(body) != "a=1&check=yes&s=x" {
		t.Fatalf("body = %q", body)
	}
	form.Attrs = []Attr{{Key: "action", Value: "/search"}}
	method, target, body, _, ok = FormRequest(form, base)
	if !ok || method != "GET" || target == "" || body != nil {
		t.Fatalf("get = %s %s %q %v", method, target, body, ok)
	}
}
