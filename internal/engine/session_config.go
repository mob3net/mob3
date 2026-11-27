package engine

import (
	"math"
	"net/url"
	"strconv"
	"strings"

	"browser/internal/protocol"
)

// configState is the home target used by the Home action.
type configState struct {
	home string
}

// configSnapshot returns the mutable runtime settings shown by the front-end
// config window. Startup and dump-only options are intentionally absent.
func (b *Session) configSnapshot() []protocol.ConfigValue {
	ua, search, ignore := defaultUserAgent, DefaultSearchURL, false
	if b.loader != nil {
		ua = b.loader.RequestUserAgent()
		search = b.loader.SearchTemplate()
		ignore = b.loader.IgnoreBlocks
	}
	return []protocol.ConfigValue{
		{Field: protocol.ConfigNoJS, Value: onOff(b.NoJS)},
		{Field: protocol.ConfigNoPDF, Value: onOff(b.NoPDF)},
		{Field: protocol.ConfigIgnoreBlocks, Value: onOff(ignore)},
		{Field: protocol.ConfigHome, Value: b.home},
		{Field: protocol.ConfigMemLimit, Value: strconv.Itoa(int(b.memLimitBytes >> 20))},
		{Field: protocol.ConfigLayoutBudget, Value: b.LayoutBudget.Name},
		{Field: protocol.ConfigZoom, Value: strconv.Itoa(int(math.Round(b.Zoom * 100)))},
		{Field: protocol.ConfigTextScale, Value: textScaleValue(b.NoBigText)},
		{Field: protocol.ConfigUserAgent, Value: ua},
		{Field: protocol.ConfigSearch, Value: search},
		{Field: protocol.ConfigCerts, Value: b.certSourceLabel()},
	}
}

// applyConfigValue applies one setting and returns a user-facing rejection
// message, or the empty string when the value was accepted. A rejected value
// leaves the current setting in place.
func (b *Session) applyConfigValue(field protocol.ConfigField, raw string) string {
	value := strings.TrimSpace(raw)
	switch field {
	case protocol.ConfigNoJS:
		on, ok := parseOnOff(value)
		if !ok {
			return "JavaScript must be on or off"
		}
		b.NoJS = on
		if b.loader != nil {
			b.loader.NoScripts = on
		}
	case protocol.ConfigNoPDF:
		on, ok := parseOnOff(value)
		if !ok {
			return "PDF viewer must be on or off"
		}
		b.NoPDF = on
		if b.loader != nil {
			b.loader.NoPDF = on
		}
	case protocol.ConfigIgnoreBlocks:
		on, ok := parseOnOff(value)
		if !ok {
			return "Blocklist override must be on or off"
		}
		if b.loader != nil {
			b.loader.IgnoreBlocks = on
		}
	case protocol.ConfigHome:
		if value == "" {
			value = "about:home"
		}
		b.home = value
	case protocol.ConfigMemLimit:
		mb, err := strconv.Atoi(value)
		if err != nil || mb < 0 {
			return "Memory limit must be a whole number of MB"
		}
		b.setMemoryLimitMB(mb)
	case protocol.ConfigLayoutBudget:
		budget, ok := ParseLayoutBudget(value)
		if !ok {
			return "Layout budget must be low, standard, high or none"
		}
		b.LayoutBudget = budget
		b.rebuild()
	case protocol.ConfigZoom:
		pct, ok := parseZoomPercent(value)
		if !ok || pct < 25 || pct > 200 {
			return "Zoom must be between 25% and 200%"
		}
		b.Zoom = pct / 100
		SetZoom(b.Zoom)
		b.zoomGen = zoomGen()
		b.BigTextBlocked = false
		b.rebuild()
	case protocol.ConfigTextScale:
		switch strings.ToLower(value) {
		case "off", "none", "false":
			b.NoBigText = true
		case "auto", "on", "true", "":
			b.NoBigText = false
			b.BigTextBlocked = false
		default:
			return "Text scaling must be auto or off"
		}
		b.rebuild()
	case protocol.ConfigUserAgent:
		if b.loader == nil {
			return "User agent is unavailable"
		}
		if value == "" {
			value = defaultUserAgent
		}
		b.loader.SetUserAgent(value)
	case protocol.ConfigSearch:
		if b.loader == nil {
			return "Search is unavailable"
		}
		b.loader.SetSearchURL(value)
	case protocol.ConfigCerts:
		if b.loader == nil {
			return "Certificate source is unavailable"
		}
		// Only the built-in sources exist: the binary reads no certificate
		// files, so the config window and -certs accept the same two values.
		if err := b.loader.SetCertSource(value); err != nil {
			return "Certificate source not changed: " + err.Error()
		}
	default:
		return "Unknown setting"
	}
	return ""
}

// applyConfigCommand reads or changes one setting for the front-end window and
// answers with the full mutable snapshot.
func (b *Session) applyConfigCommand(v protocol.Config) {
	errText := ""
	if v.Action == protocol.ConfigSet {
		errText = b.applyConfigValue(v.Field, v.Value)
	}
	if b.sessionOut != nil {
		_ = b.sessionOut.Send(protocol.Event{Body: protocol.ConfigState{
			Values: b.configSnapshot(),
			Err:    errText,
		}})
	}
}

// emitOpenConfig tells the front end to open its settings window for a
// navigation that started inside the engine.
func (b *Session) emitOpenConfig() {
	if b.sessionOut == nil {
		return
	}
	_ = b.sessionOut.Send(protocol.Event{Body: protocol.OpenConfig{}})
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
