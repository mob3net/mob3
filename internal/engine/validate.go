package engine

import (
	"regexp"
	"strings"
	"time"
)

// validateForm applies the supported HTML constraint checks in document order.
// It returns the first invalid control and a warning for the footer.
func ValidateForm(form *Node) (*Node, string) {
	var invalid *Node
	var reason string
	fail := func(x *Node, msg string) {
		if invalid == nil {
			invalid, reason = x, msg
		}
	}
	form.FindAll(func(x *Node) bool {
		if x.Tag != "input" && x.Tag != "textarea" && x.Tag != "select" {
			return false
		}
		if IsDisabledControl(x) {
			return false
		}
		typ := strings.ToLower(strings.TrimSpace(x.Attr("type")))
		required := x.HasAttr("required")
		switch typ {
		case "submit", "button", "reset", "image", "file", "hidden":
			return false
		case "checkbox":
			if required && !x.HasAttr("checked") {
				fail(x, "Form: "+formFieldName(x)+" is required")
			}
			return false
		case "radio":
			if required && !radioGroupChecked(form, x) {
				fail(x, "Form: "+formFieldName(x)+" is required")
			}
			return false
		}
		var value string
		if x.Tag == "select" {
			if x.HasAttr("multiple") {
				if required && len(SelectedOptionValues(x)) == 0 {
					fail(x, "Form: "+formFieldName(x)+" is required")
				}
				return false
			}
			value = InputValue(x)
		} else {
			value = InputValue(x)
		}
		if required && value == "" {
			fail(x, "Form: "+formFieldName(x)+" is required")
			return false
		}
		if value == "" {
			return false
		}
		if msg := invalidControlValue(x, typ, value); msg != "" {
			fail(x, "Form: "+formFieldName(x)+" "+msg)
			return false
		}
		if patternApplies(typ) {
			if pattern := strings.TrimSpace(x.Attr("pattern")); pattern != "" {
				re, err := regexp.Compile("^(?:" + pattern + ")$")
				if err == nil && !re.MatchString(value) {
					fail(x, "Form: "+formFieldName(x)+" does not match the pattern")
				}
			}
		}
		return false
	})
	return invalid, reason
}

func formFieldName(n *Node) string {
	for _, key := range []string{"name", "id", "placeholder"} {
		if v := strings.TrimSpace(n.Attr(key)); v != "" {
			return v
		}
	}
	if n != nil && n.Tag != "" {
		return n.Tag
	}
	return "field"
}

func radioGroupChecked(form *Node, x *Node) bool {
	if x == nil {
		return false
	}
	name := x.Attr("name")
	if name == "" {
		return x.HasAttr("checked")
	}
	checked := false
	form.FindAll(func(n *Node) bool {
		if n.Tag == "input" && strings.EqualFold(n.Attr("type"), "radio") &&
			n.Attr("name") == name && n.HasAttr("checked") && !IsDisabledControl(n) {
			checked = true
		}
		return false
	})
	return checked
}

func patternApplies(typ string) bool {
	switch typ {
	case "", "text", "search", "email", "url", "tel", "password":
		return true
	}
	return false
}

var (
	inputDateRE     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	inputMonthRE    = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)
	inputWeekRE     = regexp.MustCompile(`^\d{4}-W(0[1-9]|[1-4]\d|5[0-3])$`)
	inputTimeRE     = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d(:[0-5]\d(\.\d{1,3})?)?$`)
	inputDateTimeRE = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T([01]\d|2[0-3]):[0-5]\d(:[0-5]\d(\.\d{1,3})?)?$`)
	inputColorRE    = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

// invalidControlValue reports a format error for typed value controls.
func invalidControlValue(n *Node, typ, value string) string {
	switch typ {
	case "date":
		if !inputDateRE.MatchString(value) {
			return "needs a date like YYYY-MM-DD"
		}
		if _, err := time.Parse("2006-01-02", value); err != nil {
			return "has an invalid date"
		}
	case "month":
		if !inputMonthRE.MatchString(value) {
			return "needs a month like YYYY-MM"
		}
	case "week":
		if !inputWeekRE.MatchString(value) {
			return "needs a week like YYYY-W01"
		}
	case "time":
		if !inputTimeRE.MatchString(value) {
			return "needs a time like HH:MM"
		}
	case "datetime-local":
		if !inputDateTimeRE.MatchString(value) {
			return "needs a date and time like YYYY-MM-DDTHH:MM"
		}
		datePart := value[:10]
		if _, err := time.Parse("2006-01-02", datePart); err != nil {
			return "has an invalid date"
		}
	case "color":
		if !inputColorRE.MatchString(value) {
			return "needs a color like #ff0000"
		}
	}
	return ""
}
