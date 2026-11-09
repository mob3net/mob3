package browser

import (
	"net/url"
	"strconv"
	"strings"
)

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
	if b.loader != nil {
		ua = b.loader.userAgent
		search = b.loader.searchURL
		ignore = b.loader.ignoreBlocks
	}
	return []configItem{
		{Flag: "-dump", Value: get("-dump", "off"), Note: "startup"},
		{Flag: "-width", Value: get("-width", strconv.Itoa(b.width)), Note: "startup"},
		{Flag: "-graphics", Value: get("-graphics", "none"), Note: "startup"},
		{Flag: "-no-js", Value: onOff(b.noJS), Note: "checkbox", Editable: true, Checkbox: true},
		{Flag: "-ignore-blocks", Value: onOff(ignore), Note: "checkbox", Editable: true, Checkbox: true},
		{Flag: "-bookmarks", Value: get("-bookmarks", "(none)"), Note: "startup"},
		{Flag: "-home", Value: b.home, Note: "immediate", Editable: true},
		{Flag: "-js-memory", Value: strconv.Itoa(int(b.jsMemLimit >> 20)), Note: "MB; 0 disables", Editable: true},
		{Flag: "-text-scale", Value: textScaleValue(b.noBigText), Note: "select", Editable: true, Options: []string{"auto", "off"}},
		{Flag: "-color", Value: get("-color", "off"), Note: "dump only"},
		{Flag: "-no-color", Value: get("-no-color", "off"), Note: "dump only"},
		{Flag: "-ua", Value: ua, Note: "next request", Editable: true},
		{Flag: "-search", Value: search, Note: "empty disables search", Editable: true},
		{Flag: "-cpuprofile", Value: get("-cpuprofile", "(none)"), Note: "startup"},
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
			b.jsMemLimit = int64(mb) << 20
		}
	}
	if v, ok := q["text-scale"]; ok && len(v) > 0 {
		switch strings.ToLower(strings.TrimSpace(v[0])) {
		case "off", "none", "false":
			b.noBigText = true
		case "auto", "on", "true", "":
			b.noBigText = false
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
}

// configPage builds the about:config document.
func (b *Browser) configPage(u *url.URL, updated bool) *Page {
	root := ParseHTML(configPageHTML(b.configItems(), updated))
	page := &Page{URL: u, Base: u, Root: root, Title: "Config — MOB³", Status: 200}
	if style := root.FindFirst("style"); style != nil {
		page.Author = ParseStylesheet(style.TextContent())
	}
	return page
}

func configPageHTML(items []configItem, updated bool) string {
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html><head><title>Config</title>
<style>
body { margin: 16px; max-width: 1100px; }
h1 { font-size: 32px; }
table { border-collapse: collapse; margin: 12px 0; }
th, td { padding: 5px 10px; border-bottom: 1px solid #d0d7de; text-align: left; vertical-align: top; }
th { color: #57606a; }
input { width: 34em; max-width: 100%; }
.note { color: #57606a; }
.fixed { color: #57606a; }
.updated { background: #e6f4ea; border: 1px solid #8cc59b; padding: 8px; }
</style></head><body>
<h1>Config</h1>
`)
	if updated {
		b.WriteString(`<p class="updated">Settings updated for this session.</p>`)
	}
	b.WriteString(`<p>Changes are in memory only and disappear when MOB³ exits.
Options marked <b>startup</b> cannot be changed without restarting; they are
shown read-only. Options marked <b>dump only</b> apply only to <b>-dump</b>
runs. On/off options use checkboxes; all other fields can be edited for this
session.</p>
<form action="about:config" method="get">
<input type="hidden" name="config-form" value="1">
<table>
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
<p><a href="about:start">Back to the start page</a></p>
</body></html>`)
	return b.String()
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
