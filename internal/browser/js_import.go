package browser

import (
	"net/url"

	"github.com/dop251/goja"
)

// rewriteDynamicImport replaces dynamic import() expressions and import.meta
// with names the goja parser accepts. Static import/export statements are not
// handled: the loader still skips module scripts.
func rewriteDynamicImport(src string) string {
	if len(src) < 6 || !containsImport(src) {
		return src
	}
	out := make([]byte, 0, len(src)+16)
	lastWord := ""
	lastSig := byte(0)
	for i := 0; i < len(src); {
		c := src[i]
		if c == '/' && i+1 < len(src) && src[i+1] == '/' {
			j := i + 2
			for j < len(src) && src[j] != '\n' {
				j++
			}
			out = append(out, src[i:j]...)
			i = j
			continue
		}
		if c == '/' && i+1 < len(src) && src[i+1] == '*' {
			j := i + 2
			for j+1 < len(src) && !(src[j] == '*' && src[j+1] == '/') {
				j++
			}
			if j+1 < len(src) {
				j += 2
			}
			out = append(out, src[i:j]...)
			i = j
			continue
		}
		if c == '\'' || c == '"' {
			j := scanJSString(src, i)
			out = append(out, src[i:j]...)
			lastSig, lastWord = c, ""
			i = j
			continue
		}
		if c == '`' {
			j := scanJSTemplate(src, i)
			out = append(out, src[i:j]...)
			lastSig, lastWord = '`', ""
			i = j
			continue
		}
		if c == '/' && regexMayStart(lastSig, lastWord) {
			if j := scanJSRegex(src, i); j > i {
				out = append(out, src[i:j]...)
				lastSig, lastWord = '/', ""
				i = j
				continue
			}
		}
		if isJSIdentStart(c) {
			j := i + 1
			for j < len(src) && isJSIdentPart(src[j]) {
				j++
			}
			word := src[i:j]
			if word == "import" && lastSig != '.' {
				k := j
				for k < len(src) && isJSSpace(src[k]) {
					k++
				}
				if k < len(src) && src[k] == '(' {
					out = append(out, "__mob3Import"...)
					out = append(out, src[j:k]...)
					lastSig, lastWord = 't', "__mob3Import"
					i = k
					continue
				}
				if k+4 <= len(src) && src[k] == '.' && src[k+1:k+5] == "meta" &&
					(k+5 >= len(src) || !isJSIdentPart(src[k+5])) {
					out = append(out, "__mob3ImportMeta"...)
					lastSig, lastWord = 'a', "__mob3ImportMeta"
					i = k + 5
					continue
				}
			}
			out = append(out, word...)
			lastSig, lastWord = src[j-1], word
			i = j
			continue
		}
		out = append(out, c)
		if !isJSSpace(c) {
			lastSig = c
			if !isJSIdentPart(c) {
				lastWord = ""
			}
		}
		i++
	}
	return string(out)
}

func containsImport(src string) bool {
	for i := 0; i+6 <= len(src); i++ {
		if src[i:i+6] == "import" {
			return true
		}
	}
	return false
}

func isJSSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'
}

func isJSIdentStart(c byte) bool {
	return c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

func isJSIdentPart(c byte) bool {
	return isJSIdentStart(c) || c >= '0' && c <= '9'
}

func scanJSString(src string, i int) int {
	quote := src[i]
	j := i + 1
	for j < len(src) {
		switch src[j] {
		case '\\':
			j += 2
			continue
		case quote:
			return j + 1
		case '\n':
			return j
		}
		j++
	}
	return len(src)
}

func scanJSTemplate(src string, i int) int {
	j := i + 1
	for j < len(src) {
		switch src[j] {
		case '\\':
			j += 2
			continue
		case '`':
			return j + 1
		}
		j++
	}
	return len(src)
}

func scanJSRegex(src string, i int) int {
	j := i + 1
	inClass := false
	for j < len(src) {
		switch src[j] {
		case '\\':
			j += 2
			continue
		case '\n':
			return i
		case '[':
			inClass = true
		case ']':
			inClass = false
		case '/':
			if !inClass {
				j++
				for j < len(src) && isJSIdentPart(src[j]) {
					j++
				}
				return j
			}
		}
		j++
	}
	return i
}

// regexMayStart reports whether a '/' at this point starts a regular expression
// rather than a division operator.
func regexMayStart(lastSig byte, lastWord string) bool {
	switch lastWord {
	case "return", "typeof", "instanceof", "in", "of", "new", "delete",
		"void", "throw", "case", "do", "else", "yield", "await":
		return true
	}
	switch lastSig {
	case 0, '(', '[', '{', ',', ';', ':', '=', '!', '&', '|', '?',
		'+', '-', '*', '%', '^', '~', '<', '>', '}':
		return true
	}
	return false
}

// installDynamicImport exposes the rewritten import() call and import.meta.
// Modules are still not evaluated; a dynamic import resolves to an empty
// namespace so the calling bundle keeps running.
func (j *jsRuntime) installDynamicImport() {
	base := ""
	if j.page != nil {
		if j.page.Base != nil {
			base = j.page.Base.String()
		} else if j.page.URL != nil {
			base = j.page.URL.String()
		}
	}
	resolve := func(spec string) string {
		if base == "" {
			return spec
		}
		bu, err := url.Parse(base)
		if err != nil {
			return spec
		}
		ref, err := bu.Parse(spec)
		if err != nil {
			return spec
		}
		return ref.String()
	}
	seen := make(map[string]bool)
	_ = j.vm.Set("__mob3Import", j.vm.ToValue(func(call goja.FunctionCall) goja.Value {
		target := resolve(call.Argument(0).String())
		if !seen[target] && len(seen) < 20 {
			seen[target] = true
			j.b.logJS("warn", "dynamic import is not supported: "+target)
		}
		return j.resolvedPromise(j.vm.NewObject())
	}))
	meta := j.vm.NewObject()
	_ = meta.Set("url", base)
	_ = meta.Set("resolve", j.vm.ToValue(func(call goja.FunctionCall) goja.Value {
		return j.vm.ToValue(resolve(call.Argument(0).String()))
	}))
	_ = j.vm.Set("__mob3ImportMeta", meta)
}
