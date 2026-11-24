package protocol

import (
	"net/url"
	"testing"
)

func TestOriginOf(t *testing.T) {
	cases := []struct {
		raw    string
		want   string
		header string
	}{
		{"https://example.com/a", "https://example.com", "https://example.com"},
		{"https://EXAMPLE.com:443/a", "https://example.com", "https://example.com"},
		{"http://example.com:80/a", "http://example.com", "http://example.com"},
		{"http://example.com:8080/a", "http://example.com:8080", "http://example.com:8080"},
		{"https://example.com:80/a", "https://example.com:80", "https://example.com:80"},
		{"https://[::1]:8080/a", "https://[::1]:8080", "https://[::1]:8080"},
		{"about:home", "null", ""},
		{"data:text/html,x", "null", ""},
	}
	for _, c := range cases {
		u, err := url.Parse(c.raw)
		if err != nil {
			t.Fatalf("parse %q: %v", c.raw, err)
		}
		o := OriginOf(u)
		if got := o.String(); got != c.want {
			t.Errorf("OriginOf(%q).String() = %q, want %q", c.raw, got, c.want)
		}
		if got := o.HeaderValue(); got != c.header {
			t.Errorf("OriginOf(%q).HeaderValue() = %q, want %q", c.raw, got, c.header)
		}
	}
	if got := OriginOf(nil).String(); got != "null" {
		t.Fatalf("OriginOf(nil) = %q, want null", got)
	}
}

func TestParseOrigin(t *testing.T) {
	cases := []struct {
		raw  string
		want string
		ok   bool
	}{
		{"https://example.com", "https://example.com", true},
		{"https://example.com/", "https://example.com", true},
		{"https://example.com:443", "https://example.com", true},
		{"http://example.com:8080", "http://example.com:8080", true},
		{"null", "null", true},
		{"", "", false},
		{"example.com", "", false},
		{"https://example.com/path", "", false},
		{"https://user@example.com", "", false},
	}
	for _, c := range cases {
		o, ok := ParseOrigin(c.raw)
		if ok != c.ok {
			t.Fatalf("ParseOrigin(%q) ok = %v, want %v", c.raw, ok, c.ok)
		}
		if ok && o.String() != c.want {
			t.Errorf("ParseOrigin(%q) = %q, want %q", c.raw, o.String(), c.want)
		}
	}
}

func TestSameOrigin(t *testing.T) {
	parse := func(raw string) *url.URL {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}
		return u
	}
	cases := []struct {
		a, b string
		want bool
	}{
		{"https://example.com/a", "https://example.com/b", true},
		{"https://example.com/a", "https://example.com:443/b", true},
		{"http://example.com/a", "http://example.com:8080/b", false},
		{"https://example.com/a", "http://example.com/b", false},
		{"https://a.example.com/", "https://b.example.com/", false},
		{"about:home", "about:help", false},
		{"data:text/html,x", "data:text/html,x", false},
	}
	for _, c := range cases {
		if got := SameOrigin(parse(c.a), parse(c.b)); got != c.want {
			t.Errorf("SameOrigin(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
