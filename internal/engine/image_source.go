package engine

import (
	"strconv"
	"strings"
)

// srcsetCandidate is one entry from a srcset attribute.
type srcsetCandidate struct {
	url string
	w   int
	x   float64
}

// ImageSource chooses the URL to download for an img, honouring a surrounding
// picture's matching source elements, srcset width/density candidates, and
// common data-src lazy-loading fallbacks when no standard source is present.
func ImageSource(n *Node, viewportPx int) string {
	if n == nil {
		return ""
	}
	if n.Tag != "img" {
		// A non-image element can carry a CSS background image. The style is
		// only available after the first layout pass, so these are picked up by
		// the deferred image batch.
		if n.Style == nil || n.Style.BackgroundImage == "" {
			return ""
		}
		return n.Style.BackgroundImage
	}
	preferred := viewportPx * 2 / 5
	if preferred < 160 {
		preferred = 160
	}
	if preferred > 1024 {
		preferred = 1024
	}
	if pic := pictureParent(n); pic != nil {
		for _, c := range pic.Children {
			if c.Type != ElementNode || c.Tag != "source" || !imageTypeAllowed(c.Attr("type")) {
				continue
			}
			if media := c.Attr("media"); media != "" && !mediaMatches(media, viewportPx, false) {
				continue
			}
			if src := PickSrcset(c.Attr("srcset"), preferred); src != "" {
				return src
			}
		}
	}
	if src := PickSrcset(n.Attr("srcset"), preferred); src != "" {
		return src
	}
	if src := n.Attr("src"); src != "" {
		return src
	}
	for _, attr := range []string{"data-srcset", "data-lazy-srcset"} {
		if src := PickSrcset(n.Attr(attr), preferred); src != "" {
			return src
		}
	}
	for _, attr := range []string{"data-src", "data-lazy-src", "data-original", "data-default-src"} {
		if src := n.Attr(attr); src != "" {
			return src
		}
	}
	return ""
}

func pictureParent(n *Node) *Node {
	if n == nil || n.Parent == nil || n.Parent.Tag != "picture" {
		return nil
	}
	return n.Parent
}

func imageTypeAllowed(typ string) bool {
	typ = strings.ToLower(strings.TrimSpace(typ))
	if typ == "" {
		return true
	}
	for _, bad := range []string{"avif", "jxl", "heic", "heif"} {
		if strings.Contains(typ, bad) {
			return false
		}
	}
	return true
}

// PickSrcset selects the smallest width candidate at least as wide as
// preferred, or the widest available; for density descriptors it picks the
// candidate closest to 1x.
func PickSrcset(s string, preferred int) string {
	candidates := parseSrcset(s)
	if len(candidates) == 0 {
		return ""
	}
	bestW, bestD := -1, ""
	for _, c := range candidates {
		if c.w <= 0 {
			continue
		}
		if c.w >= preferred {
			if bestW < 0 || c.w < bestW {
				bestW, bestD = c.w, c.url
			}
		}
	}
	if bestD != "" {
		return bestD
	}
	for _, c := range candidates {
		if c.w > bestW {
			bestW, bestD = c.w, c.url
		}
	}
	if bestD != "" {
		return bestD
	}
	bestDiff, bestX := -1.0, ""
	for _, c := range candidates {
		diff := c.x - 1
		if diff < 0 {
			diff = -diff
		}
		if bestX == "" || diff < bestDiff {
			bestDiff, bestX = diff, c.url
		}
	}
	return bestX
}

// isJSSpace reports the ASCII whitespace bytes a srcset candidate splits on.
func isJSSpace(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return false
}

func parseSrcset(s string) []srcsetCandidate {
	var out []srcsetCandidate
	for i := 0; i < len(s); {
		for i < len(s) && (isJSSpace(s[i]) || s[i] == ',') {
			i++
		}
		start := i
		for i < len(s) && !isJSSpace(s[i]) && s[i] != ',' {
			i++
		}
		url := s[start:i]
		for i < len(s) && isJSSpace(s[i]) {
			i++
		}
		descStart := i
		for i < len(s) && s[i] != ',' {
			i++
		}
		desc := strings.TrimSpace(s[descStart:i])
		if url == "" {
			continue
		}
		c := srcsetCandidate{url: url}
		if strings.HasSuffix(desc, "w") {
			if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(desc, "w"))); err == nil {
				c.w = n
			}
		} else if strings.HasSuffix(desc, "x") {
			if f, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(desc, "x")), 64); err == nil {
				c.x = f
			}
		}
		out = append(out, c)
		if i < len(s) && s[i] == ',' {
			i++
		}
	}
	return out
}
