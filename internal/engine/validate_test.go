package engine

import (
	"strings"
	"testing"
)

func TestValidateFormRequiredAndFormats(t *testing.T) {
	doc := ParseHTML(`<html><body><form>
		<input name="title" required value="">
		<input name="when" type="date" value="2026-13-40">
		<input name="code" pattern="[0-9]{3}" value="12x">
	</form></body></html>`)
	form := doc.FindFirst("form")
	field, msg := ValidateForm(form)
	if field == nil || !strings.Contains(msg, "title") || !strings.Contains(msg, "required") {
		t.Fatalf("first invalid = %v %q", field, msg)
	}

	setNodeAttr(field, "value", "ok")
	field, msg = ValidateForm(form)
	if field == nil || !strings.Contains(msg, "when") || !strings.Contains(msg, "date") {
		t.Fatalf("date invalid = %v %q", field, msg)
	}
	setNodeAttr(field, "value", "2026-01-02")
	field, msg = ValidateForm(form)
	if field == nil || !strings.Contains(msg, "code") || !strings.Contains(msg, "pattern") {
		t.Fatalf("pattern invalid = %v %q", field, msg)
	}
	setNodeAttr(field, "value", "123")
	if field, msg := ValidateForm(form); field != nil {
		t.Fatalf("valid form rejected: %v %q", field, msg)
	}
}

func setNodeAttr(n *Node, key, value string) {
	for i := range n.Attrs {
		if strings.EqualFold(n.Attrs[i].Key, key) {
			n.Attrs[i].Value = value
			return
		}
	}
	n.Attrs = append(n.Attrs, Attr{Key: key, Value: value})
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
