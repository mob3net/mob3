package browser

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// configTokenParam carries the one-shot form token. It is a hidden input, so a
// config submission puts it in the POST body rather than the URL.
const configTokenParam = "config-token"

// configValues returns the values an about:config navigation submits. A GET
// form puts them in the query; the config form itself posts them.
func configValues(method string, u *url.URL, body []byte) url.Values {
	q := u.Query()
	if method == http.MethodPost && len(body) > 0 {
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
func (b *Browser) consumeConfigToken(token string) bool {
	if token == "" || b.configToken == "" || token != b.configToken {
		return false
	}
	b.configToken = ""
	return true
}

// newConfigToken returns an unpredictable per-session form token. It is
// rotated after each successful apply. It returns an empty string if the system
// random source fails, which fails closed: the form then cannot be submitted.
func newConfigToken() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(buf[:])
}

// configItem is one row on the about:config page.
type configItem struct {
	Flag     string
	Value    string
	Note     string
	Editable bool
	Checkbox bool
	Options  []string
}

// configItems returns every CLI setting with its current value. Options that
// need a restart to change are shown read-only.
func (b *Browser) configItems() []configItem {
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
	cacheMB := int(defaultResourceCacheLimit >> 20)
	if b.loader != nil {
		ua = b.loader.userAgent
		search = b.loader.searchURL
		ignore = b.loader.ignoreBlocks
		cacheMB = b.loader.cacheLimitMB()
	}
	return []configItem{
		{Flag: "-dump", Value: get("-dump", "off"), Note: "startup"},
		{Flag: "-png", Value: get("-png", "off"), Note: "dump only"},
		{Flag: "-width", Value: get("-width", strconv.Itoa(b.width)), Note: "startup"},
		{Flag: "-height", Value: get("-height", "0"), Note: "startup; dump/PNG/MCP only"},
		{Flag: "-pdf-page", Value: get("-pdf-page", "auto"), Note: "dump/PNG only"},
		{Flag: "-graphics", Value: get("-graphics", "none"), Note: "startup"},
		{Flag: "-no-js", Value: onOff(b.noJS), Note: "checkbox", Editable: true, Checkbox: true},
		{Flag: "-no-pdf", Value: onOff(b.noPDF), Note: "checkbox", Editable: true, Checkbox: true},
		{Flag: "-reader", Value: onOff(b.reader), Note: "startup; R toggles"},
		{Flag: "-dark", Value: onOff(b.dark), Note: "startup; D toggles"},
		{Flag: "-ignore-blocks", Value: onOff(ignore), Note: "checkbox", Editable: true, Checkbox: true},
		{Flag: "-bookmarks", Value: get("-bookmarks", "(none)"), Note: "startup"},
		{Flag: "-home", Value: b.home, Note: "immediate", Editable: true},
		{Flag: "-js-memory", Value: strconv.Itoa(int(b.jsMemLimit >> 20)), Note: "MB; 0 disables; default from -mem-limit", Editable: true},
		{Flag: "-cache", Value: strconv.Itoa(cacheMB), Note: "MB; 0 disables", Editable: true},
		{Flag: "-history-cache", Value: strconv.Itoa(int(b.historyCacheBytes >> 20)), Note: "MB; 0 disables", Editable: true},
		{Flag: "-mem-limit", Value: strconv.Itoa(int(b.memLimitBytes >> 20)), Note: "MB; 0 = unlimited/Go default", Editable: true},
		{Flag: "-layout-budget", Value: b.layoutBudget.Name, Note: "select", Editable: true, Options: []string{"low", "standard", "high", "none"}},
		{Flag: "-zoom", Value: zoomPercentLabel(b.zoom), Note: "percent, 25-200; + - step", Editable: true},
		{Flag: "-text-scale", Value: textScaleValue(b.noBigText), Note: "select", Editable: true, Options: []string{"auto", "off"}},
		{Flag: "-color", Value: get("-color", "off"), Note: "dump only"},
		{Flag: "-no-color", Value: get("-no-color", "off"), Note: "dump only"},
		{Flag: "-ua", Value: ua, Note: "next request", Editable: true},
		{Flag: "-search", Value: search, Note: "empty disables search", Editable: true},
		{Flag: "-certs", Value: b.certSourceLabel(), Note: "system or embedded; PEM path at startup", Editable: true},
		{Flag: "-mcp", Value: get("-mcp", "off"), Note: "startup"},
		{Flag: "-version", Value: get("-version", Version), Note: "read only"},
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
func (b *Browser) applyConfig(q url.Values) {
	// Every visit or apply starts without a leftover error notice.
	b.configNotice = ""
	if len(q) == 0 {
		return
	}
	form := q.Has("config-form")
	if v, ok := q["no-js"]; (ok && len(v) > 0) || form {
		if form {
			b.noJS = q.Has("no-js")
			if b.loader != nil {
				b.loader.noScripts = b.noJS
			}
		} else if on, ok := parseOnOff(v[0]); ok {
			b.noJS = on
			if b.loader != nil {
				b.loader.noScripts = on
			}
		}
	}
	if v, ok := q["no-pdf"]; (ok && len(v) > 0) || form {
		if form {
			b.noPDF = q.Has("no-pdf")
			if b.loader != nil {
				b.loader.noPDF = b.noPDF
			}
		} else if on, ok := parseOnOff(v[0]); ok {
			b.noPDF = on
			if b.loader != nil {
				b.loader.noPDF = on
			}
		}
	}
	if _, ok := q["ignore-blocks"]; (ok && len(q["ignore-blocks"]) > 0) || (form && b.loader != nil) {
		if form && b.loader != nil {
			b.loader.ignoreBlocks = q.Has("ignore-blocks")
		} else if b.loader != nil {
			if on, ok := parseOnOff(q["ignore-blocks"][0]); ok {
				b.loader.ignoreBlocks = on
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
	if v, ok := q["js-memory"]; ok && len(v) > 0 {
		if mb, err := strconv.Atoi(strings.TrimSpace(v[0])); err == nil && mb >= 0 {
			limit := int64(mb) << 20
			// The config form always submits this field. Only a value that
			// differs from the displayed ceiling pins it; rounding to whole
			// MB must not turn a derived value into an explicit one.
			if b.jsMemExplicit || limit != b.jsMemLimit {
				if b.jsMemExplicit || limit>>20 != b.jsMemLimit>>20 {
					b.jsMemExplicit = true
				}
				b.jsMemLimit = limit
				if b.js != nil {
					b.js.setMemLimit(b.jsMemLimit)
				}
			}
		}
	}
	if v, ok := q["cache"]; ok && len(v) > 0 {
		if mb, err := strconv.Atoi(strings.TrimSpace(v[0])); err == nil && mb >= 0 && b.loader != nil {
			b.loader.setCacheLimitMB(mb)
		}
	}
	if v, ok := q["history-cache"]; ok && len(v) > 0 {
		if mb, err := strconv.Atoi(strings.TrimSpace(v[0])); err == nil && mb >= 0 {
			b.setHistoryCacheLimitMB(mb)
		}
	}
	if v, ok := q["mem-limit"]; ok && len(v) > 0 {
		if mb, err := strconv.Atoi(strings.TrimSpace(v[0])); err == nil && mb >= 0 {
			b.setMemoryLimitMB(mb)
		}
	}
	if v, ok := q["layout-budget"]; ok && len(v) > 0 {
		if budget, ok := ParseLayoutBudget(v[0]); ok {
			b.layoutBudget = budget
		}
	}
	if v, ok := q["zoom"]; ok && len(v) > 0 {
		if pct, valid := parseZoomPercent(v[0]); valid {
			b.zoom = pct / 100
			setLayoutZoom(b.zoom)
			b.bigTextBlocked = false
		}
	}
	if v, ok := q["text-scale"]; ok && len(v) > 0 {
		switch strings.ToLower(strings.TrimSpace(v[0])) {
		case "off", "none", "false":
			b.noBigText = true
		case "auto", "on", "true", "":
			b.noBigText = false
			b.bigTextBlocked = false
		}
	}
	if v, ok := q["ua"]; ok && len(v) > 0 && b.loader != nil {
		ua := strings.TrimSpace(v[0])
		if ua == "" {
			ua = defaultUserAgent
		}
		b.loader.userAgent = ua
	}
	if v, ok := q["search"]; ok && len(v) > 0 && b.loader != nil {
		b.loader.searchURL = strings.TrimSpace(v[0])
	}
	if v, ok := q["certs"]; (ok && len(v) > 0) || (form && b.loader != nil) {
		if b.loader != nil {
			val := ""
			if len(v) > 0 {
				val = v[0]
			}
			// Only the built-in sources are accepted here. A PEM path stays a
			// start-up option so a page that navigates to about:config cannot
			// make the browser read a local bundle.
			if !isBuiltinCertSource(val) {
				b.configNotice = "Certificate source not changed: about:config sets system or embedded; pass a PEM bundle to -certs at startup"
			} else if err := b.loader.SetCertSource(val); err != nil {
				b.configNotice = "Certificate source not changed: " + err.Error()
			}
		}
	}
}

// configPage builds the about:config document.
func (b *Browser) configPage(u *url.URL, updated bool) *Page {
	if b.configToken == "" {
		b.configToken = newConfigToken()
	}
	doc := configPageHTML(b.configItems(), updated, b.configNotice, b.configToken, b.width, b.viewportHeight(), b.cellW, b.cellH)
	root := ParseHTML(doc)
	page := &Page{URL: u, Base: u, Root: root, Title: "Config — MOB³", RawHTML: doc, Status: 200}
	applyPageStyles(root, page)
	return page
}

func configPageHTML(items []configItem, updated bool, notice, token string, width, height, cellW, cellH int) string {
	var b strings.Builder
	b.WriteString(`<h1>Config</h1>
`)
	if cellW < 1 {
		cellW = 8
	}
	if cellH < 1 {
		cellH = 16
	}
	b.WriteString(fmt.Sprintf(
		`<p class="viewport"><b>Viewport:</b> %dx%d characters (%dx%d pixels; %dx%d per cell)</p>`,
		width, height, width*cellW, height*cellH, cellW, cellH))
	if updated {
		b.WriteString(`<p class="updated">Settings updated for this session.</p>`)
	}
	if notice != "" {
		b.WriteString(`<p class="warn">`)
		b.WriteString(EscapeHTML(notice))
		b.WriteString(`</p>`)
	}
	b.WriteString(`<p>Changes are in memory only and disappear when MOB³ exits.
Options marked <b>startup</b> cannot be changed without restarting; they are
shown read-only. Options marked <b>dump only</b> apply only to <b>-dump</b>
runs. On/off options use checkboxes; all other fields can be edited for this
session.</p>
<form action="about:config" method="post">
<input type="hidden" name="config-form" value="1">
`)
	if token != "" {
		b.WriteString(`<input type="hidden" name="` + configTokenParam + `" value="` + EscapeHTML(token) + `">
`)
	}
	b.WriteString(`<table>
<tr><th>Option</th><th>Current value</th><th>Notes</th></tr>
`)
	for _, it := range items {
		b.WriteString(`<tr><td>`)
		if it.Editable {
			id := "cfg" + it.Flag
			b.WriteString(`<label for="`)
			b.WriteString(EscapeHTML(id))
			b.WriteString(`">`)
			b.WriteString(EscapeHTML(it.Flag))
			b.WriteString(`</label>`)
			b.WriteString(`</td><td>`)
			if len(it.Options) > 0 {
				b.WriteString(`<select id="`)
				b.WriteString(EscapeHTML(id))
				b.WriteString(`" name="`)
				b.WriteString(EscapeHTML(strings.TrimPrefix(it.Flag, "-")))
				b.WriteString(`">`)
				for _, opt := range it.Options {
					b.WriteString(`<option value="`)
					b.WriteString(EscapeHTML(opt))
					b.WriteString(`"`)
					if opt == it.Value {
						b.WriteString(` selected`)
					}
					b.WriteString(`>`)
					b.WriteString(EscapeHTML(opt))
					b.WriteString(`</option>`)
				}
				b.WriteString(`</select>`)
			} else if it.Checkbox {
				b.WriteString(`<input type="checkbox" id="`)
				b.WriteString(EscapeHTML(id))
				b.WriteString(`" name="`)
				b.WriteString(EscapeHTML(strings.TrimPrefix(it.Flag, "-")))
				b.WriteString(`" value="on"`)
				if on, _ := parseOnOff(it.Value); on {
					b.WriteString(` checked`)
				}
				b.WriteString(`>`)
			} else {
				b.WriteString(`<input id="`)
				b.WriteString(EscapeHTML(id))
				b.WriteString(`" name="`)
				b.WriteString(EscapeHTML(strings.TrimPrefix(it.Flag, "-")))
				b.WriteString(`" value="`)
				b.WriteString(EscapeHTML(it.Value))
				b.WriteString(`">`)
			}
			b.WriteString(`</td><td class="note">`)
			b.WriteString(EscapeHTML(it.Note))
			b.WriteString(`</td></tr>`)
			continue
		}
		b.WriteString(`<b>`)
		b.WriteString(EscapeHTML(it.Flag))
		b.WriteString(`</b></td><td><span class="fixed">`)
		b.WriteString(EscapeHTML(it.Value))
		b.WriteString(`</span></td><td class="note">`)
		b.WriteString(EscapeHTML(it.Note))
		b.WriteString(`</td></tr>`)
	}
	b.WriteString(`</table>
<p><button name="apply" value="1">Apply changes</button></p>
</form>
<p><a href="about:start">Back to the start page</a></p>`)
	return appPageHTML("Config — MOB³", `input { width: 34em; max-width: 100%; }
.note { color: #57606a; }
.fixed { color: #57606a; }
.viewport { background: #f6f8fa; border: 1px solid #d0d7de; padding: 8px 10px; }
.updated { background: #e6f4ea; border: 1px solid #8cc59b; padding: 8px; }
`, b.String())
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
