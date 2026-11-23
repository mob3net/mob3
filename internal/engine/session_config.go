package engine

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"browser/internal/protocol"
)

// configTokenParam carries the one-shot form token. It is a hidden input, so a
// config submission puts it in the POST body rather than the URL.
const configTokenParam = ConfigTokenParam

type configItem = ConfigItem

// configValues returns the values an about:config navigation submits. A GET
// form puts them in the query; the config form itself posts them.
func configValues(method string, u *url.URL, body []byte) url.Values {
	q := u.Query()
	if method == protocol.MethodPost && len(body) > 0 {
		if vals, err := url.ParseQuery(string(body)); err == nil {
			for k, v := range vals {
				q[k] = v
			}
		}
	}
	return q
}

// configHasSettings reports whether q carries any option, as opposed to just a
// token or the form marker.
func configHasSettings(q url.Values) bool {
	for k := range q {
		if k != configTokenParam && k != "config-form" {
			return true
		}
	}
	return false
}

// consumeConfigToken accepts the token minted for the rendered config page and
// clears it so a replay from history cannot apply the same settings again.
func (b *Session) consumeConfigToken(token string) bool {
	if b == nil || b.configToken == nil {
		return false
	}
	return b.configToken.Consume(token)
}

// configItems returns every CLI setting with its current value. Options that
// need a restart to change are shown read-only.
func (b *Session) configItems() []configItem {
	cli := b.cliConfig
	get := func(flag, fallback string) string {
		if cli != nil {
			if v, ok := cli[flag]; ok && v != "" {
				return v
			}
		}
		return fallback
	}
	ua, search, ignore := defaultUserAgent, DefaultSearchURL, false
	if b.loader != nil {
		ua = b.loader.RequestUserAgent()
		search = b.loader.SearchTemplate()
		ignore = b.loader.IgnoreBlocks
	}
	cacheMB := int64(0)
	if b.loader != nil {
		cacheMB = b.loader.Host.CacheLimitBytes() >> 20
	}
	return []configItem{
		{Flag: "-dump", Value: get("-dump", "off"), Note: "startup"},
		{Flag: "-png", Value: get("-png", "off"), Note: "dump only"},
		{Flag: "-width", Value: get("-width", strconv.Itoa(b.Width)), Note: "startup"},
		{Flag: "-height", Value: get("-height", "0"), Note: "startup; dump/PNG/MCP only"},
		{Flag: "-pdf-page", Value: get("-pdf-page", "auto"), Note: "dump/PNG only"},
		{Flag: "-graphics", Value: get("-graphics", "none"), Note: "startup"},
		{Flag: "-no-js", Value: onOff(b.NoJS), Note: "checkbox", Editable: true, Checkbox: true},
		{Flag: "-no-pdf", Value: onOff(b.NoPDF), Note: "checkbox", Editable: true, Checkbox: true},
		{Flag: "-reader", Value: onOff(b.Reader), Note: "startup; R toggles"},
		{Flag: "-dark", Value: onOff(b.Dark), Note: "startup; D toggles"},
		{Flag: "-ignore-blocks", Value: onOff(ignore), Note: "checkbox", Editable: true, Checkbox: true},
		{Flag: "-home", Value: b.home, Note: "immediate", Editable: true},
		{Flag: "-mem-limit", Value: strconv.Itoa(int(b.memLimitBytes >> 20)), Note: fmt.Sprintf("MB; 0 = unlimited/Go default; JS %d MB, cache %d MB, history %d MB", int(b.JSMemLimit>>20), cacheMB, int(b.Cache.LimitBytes()>>20)), Editable: true},
		{Flag: "-layout-budget", Value: b.LayoutBudget.Name, Note: "select", Editable: true, Options: []string{"low", "standard", "high", "none"}},
		{Flag: "-zoom", Value: zoomPercentLabel(b.Zoom), Note: "percent, 25-200; + - step", Editable: true},
		{Flag: "-text-scale", Value: textScaleValue(b.NoBigText), Note: "select", Editable: true, Options: []string{"auto", "off"}},
		{Flag: "-color", Value: get("-color", "off"), Note: "dump only"},
		{Flag: "-no-color", Value: get("-no-color", "off"), Note: "dump only"},
		{Flag: "-ua", Value: ua, Note: "next request", Editable: true},
		{Flag: "-search", Value: search, Note: "empty disables search", Editable: true},
		{Flag: "-certs", Value: b.certSourceLabel(), Note: "system or embedded", Editable: true},
		{Flag: "-mcp", Value: get("-mcp", "off"), Note: "startup"},
		{Flag: "-version", Value: get("-version", BuildVersion), Note: "read only"},
	}
}

func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

func textScaleValue(off bool) string {
	if off {
		return "off"
	}
	return "auto"
}

func parseOnOff(s string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "on", "true", "1", "yes", "enabled":
		return true, true
	case "off", "false", "0", "no", "disabled":
		return false, true
	}
	return false, false
}

// applyConfig updates mutable settings from an about:config query.
func (b *Session) applyConfig(q url.Values) {
	// Every visit or apply starts without a leftover error notice.
	b.configNotice = ""
	if len(q) == 0 {
		return
	}
	form := q.Has("config-form")
	if v, ok := q["no-js"]; (ok && len(v) > 0) || form {
		if form {
			b.NoJS = q.Has("no-js")
			if b.loader != nil {
				b.loader.NoScripts = b.NoJS
			}
		} else if on, ok := parseOnOff(v[0]); ok {
			b.NoJS = on
			if b.loader != nil {
				b.loader.NoScripts = on
			}
		}
	}
	if v, ok := q["no-pdf"]; (ok && len(v) > 0) || form {
		if form {
			b.NoPDF = q.Has("no-pdf")
			if b.loader != nil {
				b.loader.NoPDF = b.NoPDF
			}
		} else if on, ok := parseOnOff(v[0]); ok {
			b.NoPDF = on
			if b.loader != nil {
				b.loader.NoPDF = on
			}
		}
	}
	if _, ok := q["ignore-blocks"]; (ok && len(q["ignore-blocks"]) > 0) || (form && b.loader != nil) {
		if form && b.loader != nil {
			b.loader.IgnoreBlocks = q.Has("ignore-blocks")
		} else if b.loader != nil {
			if on, ok := parseOnOff(q["ignore-blocks"][0]); ok {
				b.loader.IgnoreBlocks = on
			}
		}
	}
	if v, ok := q["home"]; ok && len(v) > 0 {
		home := strings.TrimSpace(v[0])
		if home == "" {
			home = "about:home"
		}
		b.home = home
	}
	if v, ok := q["mem-limit"]; ok && len(v) > 0 {
		if mb, err := strconv.Atoi(strings.TrimSpace(v[0])); err == nil && mb >= 0 {
			b.setMemoryLimitMB(mb)
		}
	}
	if v, ok := q["layout-budget"]; ok && len(v) > 0 {
		if budget, ok := ParseLayoutBudget(v[0]); ok {
			b.LayoutBudget = budget
		}
	}
	if v, ok := q["zoom"]; ok && len(v) > 0 {
		if pct, valid := parseZoomPercent(v[0]); valid {
			b.Zoom = pct / 100
			SetZoom(b.Zoom)
			b.BigTextBlocked = false
		}
	}
	if v, ok := q["text-scale"]; ok && len(v) > 0 {
		switch strings.ToLower(strings.TrimSpace(v[0])) {
		case "off", "none", "false":
			b.NoBigText = true
		case "auto", "on", "true", "":
			b.NoBigText = false
			b.BigTextBlocked = false
		}
	}
	if v, ok := q["ua"]; ok && len(v) > 0 && b.loader != nil {
		ua := strings.TrimSpace(v[0])
		if ua == "" {
			ua = defaultUserAgent
		}
		b.loader.SetUserAgent(ua)
	}
	if v, ok := q["search"]; ok && len(v) > 0 && b.loader != nil {
		b.loader.SetSearchURL(strings.TrimSpace(v[0]))
	}
	if v, ok := q["certs"]; (ok && len(v) > 0) || (form && b.loader != nil) {
		if b.loader != nil {
			val := ""
			if len(v) > 0 {
				val = v[0]
			}
			// Only the built-in sources exist: the binary reads no certificate
			// files, so about:config and -certs accept the same two values.
			if err := b.loader.SetCertSource(val); err != nil {
				b.configNotice = "Certificate source not changed: " + err.Error()
			}
		}
	}
}

// configPage builds the about:config document.
func (b *Session) configPage(u *url.URL, updated bool) *Page {
	token := ""
	if b.configToken != nil {
		token = b.configToken.Current()
	}
	doc := configPageHTML(b.configItems(), updated, b.configNotice, token, b.Width, b.viewportHeight(), b.CellW, b.CellH)
	root := ParseHTML(doc)
	page := &Page{URL: u, Base: u, Root: root, Title: "Config — MOB³", RawHTML: doc, Status: 200}
	ApplyPageStyles(root, page)
	return page
}

func isAboutConfigURL(u *url.URL) bool {
	if u == nil || !strings.EqualFold(u.Scheme, "about") {
		return false
	}
	name := u.Opaque
	if name == "" {
		name = strings.TrimPrefix(u.Path, "/")
	}
	return strings.EqualFold(name, "config")
}

// configPageHTML moved to the engine; the wrapper keeps the session-side
// caller and tests stable.
func configPageHTML(items []configItem, updated bool, notice, token string, width, height, cellW, cellH int) string {
	return ConfigPageHTML(items, updated, notice, token, width, height, cellW, cellH)
}
