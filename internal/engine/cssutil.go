package engine

import "strings"

// SplitTopLevel splits on sep, ignoring separators inside quotes, brackets and
// parentheses.
func SplitTopLevel(s string, sep byte) []string {
	var out []string
	start := 0
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"', '\'':
			i = skipString(s, i) - 1
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		default:
			if s[i] == sep && depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, s[start:])
	return out
}

// SplitFields splits a CSS value on whitespace at the top level.
func SplitFields(s string) []string {
	var out []string
	var b strings.Builder
	depth := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"', '\'':
			j := skipString(s, i)
			b.WriteString(s[i:j])
			i = j - 1
			continue
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		}
		if (c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f') && depth == 0 {
			if b.Len() > 0 {
				out = append(out, b.String())
				b.Reset()
			}
			continue
		}
		b.WriteByte(c)
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out
}

// skipString returns the index just past the string starting at i.
func skipString(s string, i int) int {
	q := s[i]
	i++
	for i < len(s) {
		if s[i] == '\\' {
			i += 2
			continue
		}
		if s[i] == q {
			return i + 1
		}
		i++
	}
	return len(s)
}

// hexVal decodes one hexadecimal digit.
func hexVal(c byte) (int, bool) {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0'), true
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10, true
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10, true
	}
	return 0, false
}
