package engine

import (
	"mime"
	"strings"

	"golang.org/x/text/encoding/charmap"
)

// jsMIMEEssences lists the response types a classic script may execute as.
// Missing Content-Type is handled separately and stays lenient, matching the
// way file and data resources without metadata are treated.
var jsMIMEEssences = map[string]bool{
	"application/ecmascript":   true,
	"application/javascript":   true,
	"application/x-ecmascript": true,
	"application/x-javascript": true,
	"text/ecmascript":          true,
	"text/javascript":          true,
	"text/javascript1.0":       true,
	"text/javascript1.1":       true,
	"text/javascript1.2":       true,
	"text/javascript1.3":       true,
	"text/javascript1.4":       true,
	"text/javascript1.5":       true,
	"text/jscript":             true,
	"text/livescript":          true,
	"text/x-ecmascript":        true,
	"text/x-javascript":        true,
}

// mimeEssence returns the lowercased type/subtype of a Content-Type header,
// dropping parameters. It returns "" when no usable type is present.
func mimeEssence(contentType string) string {
	essence := ""
	for _, part := range strings.Split(contentType, ",") {
		part = strings.TrimSpace(part)
		if i := strings.IndexByte(part, ';'); i >= 0 {
			part = strings.TrimSpace(part[:i])
		}
		if strings.Contains(part, "/") {
			essence = strings.ToLower(part)
		}
	}
	return essence
}

// isJavaScriptMIME reports whether a script response may execute. An empty
// (missing) type is allowed so data resources without metadata keep working;
// any explicit non-JavaScript type is rejected.
func isJavaScriptMIME(contentType string) bool {
	essence := mimeEssence(contentType)
	return essence == "" || jsMIMEEssences[essence]
}

func decodeBody(data []byte, ctype string) string {
	charset := ""
	if _, params, err := mime.ParseMediaType(ctype); err == nil {
		charset = strings.ToLower(params["charset"])
	}
	if charset == "" {
		head := strings.ToLower(string(data[:min(len(data), 2048)]))
		if i := strings.Index(head, "charset="); i >= 0 {
			rest := head[i+len("charset="):]
			rest = strings.TrimLeft(rest, `"' `)
			end := 0
			for end < len(rest) && (rest[end] == '-' || rest[end] >= 'a' && rest[end] <= 'z' || rest[end] >= '0' && rest[end] <= '9' || rest[end] == '_') {
				end++
			}
			charset = rest[:end]
		}
	}
	if cm, ok := lookupCharset(charset); ok {
		if decoded, err := cm.NewDecoder().Bytes(data); err == nil {
			return string(decoded)
		}
	}
	return strings.ToValidUTF8(string(data), "\uFFFD")
}

// singleByteCharsets maps legacy single-byte charset labels to their decoder.
// Labels are matched after lowercasing and treating '_' like '-'. Anything not
// listed here is read as UTF-8.
var singleByteCharsets = map[string]*charmap.Charmap{
	"iso-8859-1":   charmap.ISO8859_1,
	"iso8859-1":    charmap.ISO8859_1,
	"latin1":       charmap.ISO8859_1,
	"latin-1":      charmap.ISO8859_1,
	"l1":           charmap.ISO8859_1,
	"cp819":        charmap.ISO8859_1,
	"windows-1252": charmap.Windows1252,
	"cp1252":       charmap.Windows1252,
	"iso-8859-2":   charmap.ISO8859_2,
	"iso8859-2":    charmap.ISO8859_2,
	"latin2":       charmap.ISO8859_2,
	"latin-2":      charmap.ISO8859_2,
	"l2":           charmap.ISO8859_2,
	"windows-1250": charmap.Windows1250,
	"cp1250":       charmap.Windows1250,
	"iso-8859-3":   charmap.ISO8859_3,
	"latin3":       charmap.ISO8859_3,
	"iso-8859-4":   charmap.ISO8859_4,
	"latin4":       charmap.ISO8859_4,
	"iso-8859-5":   charmap.ISO8859_5,
	"cyrillic":     charmap.ISO8859_5,
	"windows-1251": charmap.Windows1251,
	"cp1251":       charmap.Windows1251,
	"iso-8859-7":   charmap.ISO8859_7,
	"greek":        charmap.ISO8859_7,
	"greek8":       charmap.ISO8859_7,
	"windows-1253": charmap.Windows1253,
	"cp1253":       charmap.Windows1253,
	"iso-8859-9":   charmap.ISO8859_9,
	"latin5":       charmap.ISO8859_9,
	"windows-1254": charmap.Windows1254,
	"cp1254":       charmap.Windows1254,
	"iso-8859-10":  charmap.ISO8859_10,
	"latin6":       charmap.ISO8859_10,
	"iso-8859-13":  charmap.ISO8859_13,
	"latin7":       charmap.ISO8859_13,
	"windows-1257": charmap.Windows1257,
	"cp1257":       charmap.Windows1257,
	"iso-8859-14":  charmap.ISO8859_14,
	"latin8":       charmap.ISO8859_14,
	"iso-8859-15":  charmap.ISO8859_15,
	"latin9":       charmap.ISO8859_15,
	"latin-9":      charmap.ISO8859_15,
	"iso-8859-16":  charmap.ISO8859_16,
	"latin10":      charmap.ISO8859_16,
}

// lookupCharset resolves a charset label to a single-byte decoder.
func lookupCharset(label string) (*charmap.Charmap, bool) {
	label = strings.ToLower(strings.TrimSpace(label))
	label = strings.ReplaceAll(label, "_", "-")
	cm, ok := singleByteCharsets[label]
	return cm, ok
}

// combineMedia joins an @import/link media condition with a rule's own @media
// condition. A comma-listed import condition is approximated.
