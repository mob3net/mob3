package engine

import (
	"errors"
	"net/url"
	"strings"
)

// ResolveInput turns typed input into a URL, falling back to the search
// template when it looks like a query rather than a host or path.
func ResolveInput(input string, base *url.URL, searchURL string) (*url.URL, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return nil, errors.New("empty URL")
	}
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "file:") {
		return nil, errors.New("local files are not supported")
	}
	if strings.HasPrefix(lower, "about:") || strings.HasPrefix(lower, "data:") {
		return url.Parse(s)
	}
	for _, bad := range []string{"javascript:", "mailto:", "tel:", "ftp:"} {
		if strings.HasPrefix(lower, bad) {
			return nil, errors.New("unsupported URL scheme: " + bad[:len(bad)-1])
		}
	}
	if base != nil && (strings.HasPrefix(s, "#") || strings.HasPrefix(s, "?") || strings.HasPrefix(s, "/") || strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../")) {
		if r, err := base.Parse(s); err == nil {
			return r, nil
		}
	}
	if strings.HasPrefix(s, "//") {
		scheme := "https"
		if base != nil && base.Scheme != "" {
			scheme = base.Scheme
		}
		s = scheme + ":" + s
	}
	if strings.Contains(s, "://") {
		return url.Parse(s)
	}
	if strings.Contains(s, ":") && !strings.ContainsAny(s, " /") {
		return url.Parse("http://" + s)
	}
	if searchURL != "" && looksLikeSearch(s) {
		return searchURLFor(searchURL, s)
	}
	return url.Parse("https://" + s)
}

// ResolveURL turns typed input into a URL without search fallback.
func ResolveURL(input string, base *url.URL) (*url.URL, error) {
	return ResolveInput(input, base, "")
}

func looksLikeSearch(s string) bool {
	if strings.ContainsAny(s, " \t\n\r") {
		return true
	}
	return !strings.Contains(s, ".")
}

func searchURLFor(template, query string) (*url.URL, error) {
	q := url.QueryEscape(query)
	if strings.Contains(template, "%s") {
		return url.Parse(strings.ReplaceAll(template, "%s", q))
	}
	return url.Parse(template + q)
}
