package frontend

import (
	"net/url"
	"strconv"
	"strings"

	"browser/internal/protocol"
	"browser/internal/textedit"
	"browser/internal/textwidth"
)

// configRowKind selects how a config row is edited.
type configRowKind uint8

const (
	configToggle configRowKind = iota + 1
	configChoice
	configText
)

// configOption is one choice value and its displayed label.
type configOption struct {
	value string
	label string
}

// configDefinition is the fixed label and editor for one mutable setting. The
// current value comes from the engine.
type configDefinition struct {
	field   protocol.ConfigField
	label   string
	kind    configRowKind
	options []configOption
}

func zoomOptions() []configOption {
	out := make([]configOption, len(zoomPresets))
	for i, pct := range zoomPresets {
		value := strconv.Itoa(pct)
		out[i] = configOption{value: value, label: value + "%"}
	}
	return out
}

var configDefinitions = []configDefinition{
	{field: protocol.ConfigNoJS, label: "JavaScript", kind: configToggle},
	{field: protocol.ConfigNoPDF, label: "PDF viewer", kind: configToggle},
	{field: protocol.ConfigIgnoreBlocks, label: "Ignore blocklists", kind: configToggle},
	{field: protocol.ConfigHome, label: "Home page", kind: configText},
	{field: protocol.ConfigMemLimit, label: "Memory limit (MB)", kind: configText},
	{field: protocol.ConfigLayoutBudget, label: "Layout budget", kind: configChoice, options: []configOption{
		{value: "low", label: "low"}, {value: "standard", label: "standard"},
		{value: "high", label: "high"}, {value: "none", label: "none"},
	}},
	{field: protocol.ConfigZoom, label: "Zoom", kind: configChoice, options: zoomOptions()},
	{field: protocol.ConfigTextScale, label: "Text scaling", kind: configChoice, options: []configOption{
		{value: "auto", label: "auto"}, {value: "off", label: "off"},
	}},
	{field: protocol.ConfigUserAgent, label: "User agent", kind: configText},
	{field: protocol.ConfigSearch, label: "Search URL", kind: configText},
	{field: protocol.ConfigCerts, label: "Certificate store", kind: configChoice, options: []configOption{
		{value: "system", label: "system"}, {value: "embedded", label: "embedded"},
	}},
}

// ConfigRowView is one painted row of the settings window.
type ConfigRowView struct {
	Label string
	Value string
}

// ConfigView is the front-end state of the settings window.
type ConfigView struct {
	Rows          []ConfigRowView
	Index         int
	Editing       bool
	EditValue     string
	EditCursor    int
	EditSelectAll bool
	Err           string
	Loading       bool
}

// configOverlay is the front-end-owned settings window.
type configOverlay struct {
	configActive    bool
	configIndex     int
	configEditing   bool
	configEditValue string
	configEditCur   int
	configEditAll   bool
	configValues    map[protocol.ConfigField]string
	configErr       string
	configBase      protocol.State
	configView      ConfigView
}

func (w *configOverlay) isOpen() bool       { return w.configActive }
func (w *configOverlay) layer() windowLayer { return layerConfig }
func (w *configOverlay) persistent() bool   { return false }

func (w *configOverlay) reset(l *Tab) {
	w.configActive = false
	w.configEditing = false
	if l.Presenter != nil {
		l.Presenter.SetConfig(nil)
	}
}

func (w *configOverlay) install(l *Tab) bool {
	if l.Presenter != nil && w.configActive {
		l.Presenter.SetConfig(&w.configView)
	}
	return false
}

// start opens the settings window over the page and asks the engine for the
// current mutable values. Cached values paint immediately while the request is
// in flight.
func (w *configOverlay) start(l *Tab) {
	if w.configActive || l.dialogOverlay.isOpen() {
		return
	}
	l.dismissOverlays()
	w.configActive = true
	w.configIndex = 0
	w.configEditing = false
	w.configErr = ""
	w.configBase = l.State.Page
	w.sync(l)
	l.setFrontendOverlay(true)
	if l.Session != nil {
		_ = l.Session.Send(protocol.Command{Body: protocol.Config{Action: protocol.ConfigGet}})
	}
}

func (w *configOverlay) sync(l *Tab) {
	w.buildView()
	st := l.State.Page
	st.FrontendChrome = true
	st.KeysEngine = true
	st.Menu = protocol.MenuNone
	st.PromptActive = false
	st.FindActive = false
	l.State.Page = st
	if l.Presenter != nil {
		l.Presenter.SetState(st)
		l.Presenter.SetConfig(&w.configView)
		_ = l.Presenter.Refresh()
	}
}

func (w *configOverlay) close(l *Tab) {
	if !w.configActive {
		return
	}
	w.configActive = false
	w.configEditing = false
	l.State.Page = w.configBase
	if l.Presenter != nil {
		l.Presenter.SetConfig(nil)
		l.Presenter.SetState(w.configBase)
		_ = l.Presenter.Refresh()
	}
	l.setFrontendOverlay(false)
}

func (w *configOverlay) applyState(l *Tab, st protocol.State) bool {
	w.configBase = st
	if st.KeysEngine {
		w.close(l)
		return true
	}
	nextPageState(l, st)
	w.sync(l)
	return true
}

func (w *configOverlay) applyConfigState(l *Tab, st protocol.ConfigState) {
	values := make(map[protocol.ConfigField]string, len(st.Values))
	for _, v := range st.Values {
		if configFieldKnown(v.Field) {
			values[v.Field] = protocol.StripControls(v.Value)
		}
	}
	w.configValues = values
	w.configErr = protocol.StripControls(st.Err)
	if !w.configActive {
		return
	}
	if w.configIndex < 0 || w.configIndex >= len(configDefinitions) {
		w.configIndex = 0
	}
	w.sync(l)
}

func (w *configOverlay) buildView() {
	rows := make([]ConfigRowView, len(configDefinitions))
	for i, def := range configDefinitions {
		rows[i] = ConfigRowView{Label: def.label, Value: w.displayValue(def)}
	}
	w.configView = ConfigView{
		Rows:          rows,
		Index:         w.configIndex,
		Editing:       w.configEditing,
		EditValue:     w.configEditValue,
		EditCursor:    w.configEditCur,
		EditSelectAll: w.configEditAll,
		Err:           w.configErr,
		Loading:       w.configValues == nil,
	}
}

func (w *configOverlay) displayValue(def configDefinition) string {
	raw := w.currentValue(def)
	if w.configValues == nil {
		return "…"
	}
	switch def.kind {
	case configToggle:
		return raw
	case configChoice:
		for _, opt := range def.options {
			if opt.value == raw {
				return opt.label
			}
		}
		if def.field == protocol.ConfigZoom && raw != "" {
			return raw + "%"
		}
		return raw
	}
	if raw == "" {
		return "(empty)"
	}
	return raw
}

func (w *configOverlay) currentValue(def configDefinition) string {
	if w.configValues == nil {
		return ""
	}
	return w.configValues[def.field]
}

func (w *configOverlay) beginEdit(l *Tab) {
	def := configDefinitions[w.configIndex]
	if def.kind != configText {
		return
	}
	w.configEditing = true
	w.configEditValue = w.currentValue(def)
	w.configEditCur = len(w.configEditValue)
	w.configEditAll = true
	w.configErr = ""
	w.sync(l)
}

func (w *configOverlay) commitEdit(l *Tab) {
	if !w.configEditing {
		return
	}
	def := configDefinitions[w.configIndex]
	value := w.configEditValue
	w.configEditing = false
	w.configEditAll = false
	if w.configValues == nil {
		w.configValues = map[protocol.ConfigField]string{}
	}
	old := w.currentValue(def)
	w.configValues[def.field] = value
	w.configErr = ""
	w.sync(l)
	if value != old && l.Session != nil {
		_ = l.Session.Send(protocol.Command{Body: protocol.Config{Action: protocol.ConfigSet, Field: def.field, Value: value}})
	}
}

func (w *configOverlay) cancelEdit(l *Tab) {
	w.configEditing = false
	w.configEditAll = false
	w.configErr = ""
	w.sync(l)
}

// change toggles a checkbox or cycles a choice by delta.
func (w *configOverlay) change(l *Tab, delta int) {
	def := configDefinitions[w.configIndex]
	cur := w.currentValue(def)
	next := cur
	switch def.kind {
	case configToggle:
		if cur == "on" {
			next = "off"
		} else {
			next = "on"
		}
	case configChoice:
		if len(def.options) == 0 {
			return
		}
		idx := optionIndex(def, cur)
		idx = (idx + delta) % len(def.options)
		if idx < 0 {
			idx += len(def.options)
		}
		next = def.options[idx].value
	default:
		return
	}
	if next == cur {
		return
	}
	if w.configValues == nil {
		w.configValues = map[protocol.ConfigField]string{}
	}
	w.configValues[def.field] = next
	w.configErr = ""
	w.sync(l)
	if l.Session != nil {
		_ = l.Session.Send(protocol.Command{Body: protocol.Config{Action: protocol.ConfigSet, Field: def.field, Value: next}})
	}
}

func optionIndex(def configDefinition, cur string) int {
	for i, opt := range def.options {
		if opt.value == cur {
			return i
		}
	}
	if def.field == protocol.ConfigZoom {
		if pct, err := strconv.Atoi(cur); err == nil {
			return zoomPresetIndex(pct)
		}
	}
	return 0
}

func configFieldKnown(field protocol.ConfigField) bool {
	for _, def := range configDefinitions {
		if def.field == field {
			return true
		}
	}
	return false
}

func (w *configOverlay) handleKey(l *Tab, k string) (handled, quit bool) {
	if w.configEditing {
		switch k {
		case "enter":
			w.commitEdit(l)
			return true, false
		case "esc", "ctrl-c":
			w.cancelEdit(l)
			return true, false
		}
		text, cur, _ := textedit.EditLineKey(w.configEditValue, w.configEditCur, k, textedit.EditConfig{
			SelAll:            &w.configEditAll,
			CollapseSelection: true,
			MaxRunes:          8192,
		})
		w.configEditValue = text
		if cur > len(text) {
			cur = len(text)
		}
		w.configEditCur = cur
		w.sync(l)
		return true, false
	}
	if k == "ctrl-c" {
		if l.Session != nil {
			l.Session.Abort()
		}
	}
	switch k {
	case "esc", "q", "ctrl-c":
		w.close(l)
		return true, false
	case "enter", " ":
		if configDefinitions[w.configIndex].kind == configText {
			w.beginEdit(l)
		} else {
			w.change(l, 1)
		}
		return true, false
	case "left":
		if configDefinitions[w.configIndex].kind == configChoice {
			w.change(l, -1)
		}
		return true, false
	case "right":
		if configDefinitions[w.configIndex].kind == configChoice {
			w.change(l, 1)
		}
		return true, false
	}
	if next, ok := moveList(k, w.configIndex, len(configDefinitions)); ok {
		w.configIndex = next
		w.sync(l)
	}
	return true, false
}

func (w *configOverlay) handleMouse(l *Tab, ev MouseEvent) bool {
	if ev.Wheel != 0 {
		delta := 1
		if ev.Wheel < 0 {
			delta = -1
		}
		w.configIndex = moveChoice(w.configIndex, delta, len(configDefinitions))
		w.sync(l)
		return true
	}
	layout := LayoutConfig(l.State.Page, &w.configView)
	if layout.BoxW <= 0 {
		return true
	}
	x, y := ev.X-1, ev.Y-1
	if ev.Motion {
		if w.configEditing {
			return true
		}
		if index, ok := configRowAt(layout, x, y); ok && index != w.configIndex {
			w.configIndex = index
			w.sync(l)
		}
		return true
	}
	if !ev.Press || ev.Button != MouseButtonLeft {
		return true
	}
	if x >= layout.Close.X && x < layout.Close.X+layout.Close.W &&
		y >= layout.Close.Y && y < layout.Close.Y+layout.Close.H {
		w.close(l)
		return true
	}
	if index, ok := configRowAt(layout, x, y); ok {
		if w.configEditing && index == w.configIndex && x >= layout.ValueX {
			w.configEditCur = textOffsetAt(w.configEditValue, w.configEditCur, w.configEditAll, x-layout.ValueX, layout.ValueW)
			w.configEditAll = false
			w.sync(l)
			return true
		}
		if w.configEditing {
			w.commitEdit(l)
		}
		w.configIndex = index
		def := configDefinitions[index]
		if def.kind == configText {
			w.beginEdit(l)
		} else {
			w.change(l, 1)
		}
		return true
	}
	w.close(l)
	return true
}

// ConfigLayout describes the settings window in terminal cells.
type ConfigLayout struct {
	Title, Hint string
	Index, Top  int
	BoxX, BoxY  int
	BoxW, BoxH  int
	LabelW      int
	ValueX      int
	ValueW      int
	Rects       []protocol.Rect
	Close       protocol.Rect
}

func LayoutConfig(st protocol.State, v *ConfigView) ConfigLayout {
	if v == nil || len(v.Rows) == 0 || st.Width < 8 || st.Height < 4 {
		return ConfigLayout{}
	}
	boxW := minInt(st.Width-4, 100)
	if boxW < 48 {
		boxW = st.Width
	}
	if boxW < 8 {
		return ConfigLayout{}
	}
	inner := boxW - 4
	labelW := 0
	for _, row := range v.Rows {
		if n := textwidth.StringWidth(row.Label); n > labelW {
			labelW = n
		}
	}
	if labelW > inner/2 {
		labelW = inner / 2
	}
	if labelW < 1 {
		labelW = 1
	}
	valueW := inner - labelW - 2
	if valueW < 1 {
		valueW = 1
		labelW = inner - 3
		if labelW < 1 {
			labelW = 1
		}
	}
	active := v.Index
	if active < 0 {
		active = 0
	}
	if active >= len(v.Rows) {
		active = len(v.Rows) - 1
	}
	maxRows := st.Height - 4
	if maxRows < 1 {
		maxRows = 1
	}
	visible := len(v.Rows)
	if visible > maxRows {
		visible = maxRows
	}
	top := active
	if top+visible > len(v.Rows) {
		top = len(v.Rows) - visible
	}
	if top < 0 {
		top = 0
	}
	boxH := visible + 4
	if boxH > st.Height {
		boxH = st.Height
	}
	x := (st.Width - boxW) / 2
	y := (st.Height - boxH) / 2
	rects := make([]protocol.Rect, 0, visible)
	for i := 0; i < visible; i++ {
		rects = append(rects, protocol.Rect{X: x + 1, Y: y + 2 + i, W: boxW - 2, H: 1})
	}
	return ConfigLayout{
		Title: "Settings", Index: active, Top: top,
		BoxX: x, BoxY: y, BoxW: boxW, BoxH: boxH,
		LabelW: labelW, ValueX: x + 2 + labelW + 2, ValueW: valueW,
		Rects: rects,
		Close: protocol.Rect{X: x + boxW - 3, Y: y, W: 3, H: 1},
	}
}

func configRowAt(layout ConfigLayout, x, y int) (int, bool) {
	for i, r := range layout.Rects {
		if x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H {
			return layout.Top + i, true
		}
	}
	return 0, false
}

// DrawConfig paints the settings window on a full-screen canvas.
func DrawConfig(c *chromeCanvas, st protocol.State, v *ConfigView) ConfigLayout {
	layout := LayoutConfig(st, v)
	if layout.BoxW <= 0 {
		return layout
	}
	modalFrame(c, MenuLayout{
		Title: layout.Title, BoxX: layout.BoxX, BoxY: layout.BoxY,
		BoxW: layout.BoxW, BoxH: layout.BoxH, Close: layout.Close,
	})
	fg := rgb(0x1f, 0x23, 0x28)
	activeBG := rgb(0x1f, 0x4f, 0x9f)
	activeFG := rgb(0xff, 0xff, 0xff)
	for i := 0; i < len(layout.Rects); i++ {
		idx := layout.Top + i
		if idx >= len(v.Rows) {
			break
		}
		row := v.Rows[idx]
		y := layout.BoxY + 2 + i
		rowBG, rowFG := colModalBG, fg
		if idx == layout.Index {
			rowBG, rowFG = activeBG, activeFG
		}
		c.fill(layout.BoxX+1, y, layout.BoxW-2, 1, rowBG)
		c.text(layout.BoxX+2, y,
			textwidth.Pad(textwidth.Truncate(row.Label, layout.LabelW), layout.LabelW),
			rowFG, protocol.AttrBold)
		if idx == layout.Index && v.Editing {
			c.fill(layout.ValueX, y, layout.ValueW, 1, rgb(0xe8, 0xf0, 0xfe))
			head, at, tail, show := promptParts("", v.EditValue, v.EditCursor, v.EditSelectAll, layout.ValueW)
			c.text(layout.ValueX, y, head, fg, protocol.AttrBold)
			cx := layout.ValueX + textwidth.StringWidth(head)
			if show {
				c.put(cx, y, at, rgb(0xff, 0xff, 0xff), protocol.AttrBold)
				if cx < c.w {
					c.cells[y*c.w+cx].BG = rgb(0x2f, 0x6f, 0xbf)
				}
				cx++
			}
			c.text(cx, y, tail, fg, protocol.AttrBold)
			continue
		}
		value := row.Value
		if v.Loading {
			value = "…"
		}
		c.text(layout.ValueX, y,
			textwidth.Pad(textwidth.Truncate(value, layout.ValueW), layout.ValueW),
			rowFG, protocol.AttrBold)
	}
	hint := " ↑↓ move · Enter change · Space toggle · Esc close "
	hintFG := fg
	if v.Editing {
		hint = " Enter apply · Esc cancel "
	} else if v.Err != "" {
		hint = " ! " + v.Err
		hintFG = rgb(0xb3, 0x1d, 0x1d)
	}
	c.text(layout.BoxX+2, layout.BoxY+layout.BoxH-2,
		textwidth.Pad(textwidth.Truncate(hint, layout.BoxW-4), layout.BoxW-4), hintFG, protocol.AttrBold)
	modalDim(c, MenuLayout{
		BoxX: layout.BoxX, BoxY: layout.BoxY, BoxW: layout.BoxW, BoxH: layout.BoxH,
	})
	return layout
}

// isAboutConfigTarget reports whether a typed or followed URL asks for the
// front-end settings window.
func isAboutConfigTarget(target string) bool {
	u, err := url.Parse(strings.TrimSpace(target))
	if err != nil || !strings.EqualFold(u.Scheme, "about") {
		return false
	}
	name := u.Opaque
	if name == "" {
		name = strings.TrimPrefix(u.Path, "/")
	}
	return strings.EqualFold(name, "config")
}
