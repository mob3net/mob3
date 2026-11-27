package engine

import (
	"fmt"
	"strings"
)

// ConfigItem is one row on the about:config page.
type ConfigItem struct {
	Flag     string
	Value    string
	Note     string
	Editable bool
	Checkbox bool
	Options  []string
}

// ConfigTokenParam carries the one-shot config form token.
const ConfigTokenParam = "config-token"

// configCheckboxOn reports whether a checkbox value is on.
func configCheckboxOn(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "on", "true", "1", "yes", "enabled":
		return true
	}
	return false
}

func ConfigPageHTML(items []ConfigItem, updated bool, notice, token string, width, height, cellW, cellH int) string {
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
	b.WriteString(`<p>Changes are in memory only and disappear when MOB3 exits.
Options marked <b>startup</b> cannot be changed without restarting; they are
shown read-only. Options marked <b>dump only</b> apply only to <b>-dump</b>
runs. On/off options use checkboxes; all other fields can be edited for this
session.</p>
<form action="about:config" method="post">
<input type="hidden" name="config-form" value="1">
`)
	if token != "" {
		b.WriteString(`<input type="hidden" name="` + ConfigTokenParam + `" value="` + EscapeHTML(token) + `">
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
				if configCheckboxOn(it.Value) {
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
	return AppPageHTML("Config — MOB3", `input { width: 34em; max-width: 100%; }
.note { color: #57606a; }
.fixed { color: #57606a; }
.viewport { background: #f6f8fa; border: 1px solid #d0d7de; padding: 8px 10px; }
.updated { background: #e6f4ea; border: 1px solid #8cc59b; padding: 8px; }
`, b.String())
}
