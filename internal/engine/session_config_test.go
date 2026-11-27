package engine

import (
	"runtime/debug"
	"strings"
	"testing"

	"browser/internal/protocol"
)

func configValue(t *testing.T, values []protocol.ConfigValue, field protocol.ConfigField) string {
	t.Helper()
	for _, v := range values {
		if v.Field == field {
			return v.Value
		}
	}
	t.Fatalf("config snapshot has no field %d", field)
	return ""
}

func TestConfigSnapshotMutableOnly(t *testing.T) {
	b := newTestSession(80, 10)
	b.setLoader(newTestLoader())
	b.home = "https://example.com/home"
	b.NoJS = true
	b.JSMemLimit = 64 << 20
	b.NoBigText = true
	b.loader.SetUserAgent("TestUA")
	b.loader.SetSearchURL("https://search.example/?q=%s")

	values := b.configSnapshot()
	if len(values) != 11 {
		t.Fatalf("config snapshot has %d values, want 11", len(values))
	}
	want := map[protocol.ConfigField]string{
		protocol.ConfigNoJS:         "on",
		protocol.ConfigNoPDF:        "off",
		protocol.ConfigIgnoreBlocks: "off",
		protocol.ConfigHome:         "https://example.com/home",
		protocol.ConfigLayoutBudget: "standard",
		protocol.ConfigZoom:         "100",
		protocol.ConfigTextScale:    "off",
		protocol.ConfigUserAgent:    "TestUA",
		protocol.ConfigSearch:       "https://search.example/?q=%s",
		protocol.ConfigCerts:        "system",
	}
	for field, value := range want {
		if got := configValue(t, values, field); got != value {
			t.Errorf("field %d = %q, want %q", field, got, value)
		}
	}
}

func TestApplyConfigValues(t *testing.T) {
	b := newTestSession(80, 10)
	b.setLoader(newTestLoader())
	prevLimit := debug.SetMemoryLimit(-1)
	defer debug.SetMemoryLimit(prevLimit)
	cases := []struct {
		field protocol.ConfigField
		value string
		check func() bool
	}{
		{protocol.ConfigNoJS, "on", func() bool { return b.NoJS && b.loader.NoScripts }},
		{protocol.ConfigNoPDF, "on", func() bool { return b.NoPDF && b.loader.NoPDF }},
		{protocol.ConfigIgnoreBlocks, "on", func() bool { return b.loader.IgnoreBlocks }},
		{protocol.ConfigHome, "https://example.com/start", func() bool { return b.home == "https://example.com/start" }},
		{protocol.ConfigMemLimit, "64", func() bool { return b.memLimitBytes == 64<<20 }},
		{protocol.ConfigLayoutBudget, "high", func() bool { return b.LayoutBudget.Name == "high" }},
		{protocol.ConfigTextScale, "off", func() bool { return b.NoBigText }},
		{protocol.ConfigUserAgent, "TestUA", func() bool { return b.loader.RequestUserAgent() == "TestUA" }},
		{protocol.ConfigSearch, "", func() bool { return b.loader.SearchTemplate() == "" }},
		{protocol.ConfigCerts, "embedded", func() bool { return b.loader.CertStore == CertSourceEmbedded }},
	}
	for _, tc := range cases {
		if err := b.applyConfigValue(tc.field, tc.value); err != "" {
			t.Fatalf("field %d: %s", tc.field, err)
		}
		if !tc.check() {
			t.Fatalf("field %d did not apply %q", tc.field, tc.value)
		}
	}
}

func TestApplyConfigRejectsInvalidValues(t *testing.T) {
	b := newTestSession(80, 10)
	b.setLoader(newTestLoader())
	prevLimit := debug.SetMemoryLimit(-1)
	defer debug.SetMemoryLimit(prevLimit)
	b.setMemoryLimitMB(32)
	b.LayoutBudget = DefaultLayoutBudget
	oldZoom := b.Zoom
	oldCerts := b.loader.CertStore

	cases := []struct {
		name  string
		field protocol.ConfigField
		value string
		want  string
	}{
		{"javascript", protocol.ConfigNoJS, "maybe", "on or off"},
		{"pdf", protocol.ConfigNoPDF, "maybe", "on or off"},
		{"blocks", protocol.ConfigIgnoreBlocks, "maybe", "on or off"},
		{"memory", protocol.ConfigMemLimit, "many", "whole number"},
		{"budget", protocol.ConfigLayoutBudget, "tiny", "low, standard, high or none"},
		{"zoom", protocol.ConfigZoom, "10", "between 25% and 200%"},
		{"text", protocol.ConfigTextScale, "huge", "auto or off"},
		{"certs", protocol.ConfigCerts, "roots.pem", "system or embedded"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := b.applyConfigValue(tc.field, tc.value); !strings.Contains(got, tc.want) {
				t.Fatalf("error = %q, want %q", got, tc.want)
			}
		})
	}
	if b.memLimitBytes != 32<<20 || b.LayoutBudget.Name != "standard" || b.Zoom != oldZoom || b.loader.CertStore != oldCerts {
		t.Fatal("a rejected value changed the setting")
	}
}

func TestConfigAppliesZoom(t *testing.T) {
	b := newTestSession(80, 24)
	defer SetZoom(1)
	if err := b.applyConfigValue(protocol.ConfigZoom, "150"); err != "" {
		t.Fatal(err)
	}
	if b.Zoom != 1.5 {
		t.Fatalf("zoom = %g, want 1.5", b.Zoom)
	}
	if got := PX(16).ResolveH(0, 16); got != 3 {
		t.Fatalf("16px at 150%% = %d cells, want 3", got)
	}
	if got := configValue(t, b.configSnapshot(), protocol.ConfigZoom); got != "150" {
		t.Fatalf("zoom value = %q, want 150", got)
	}
}

type configSink struct {
	state  protocol.ConfigState
	opened bool
}

func (s *configSink) Send(ev protocol.Event) error {
	switch v := ev.Body.(type) {
	case protocol.ConfigState:
		s.state = v
	case protocol.OpenConfig:
		s.opened = true
	}
	return nil
}

func TestConfigCommandAnswersWithSnapshot(t *testing.T) {
	b := newTestSession(80, 10)
	b.setLoader(newTestLoader())
	sink := &configSink{}
	b.sessionOut = sink

	b.applyConfigCommand(protocol.Config{Action: protocol.ConfigGet})
	if len(sink.state.Values) != 11 || sink.state.Err != "" {
		t.Fatalf("config response = %+v", sink.state)
	}
	if got := configValue(t, sink.state.Values, protocol.ConfigHome); got != "about:home" {
		t.Fatalf("home = %q", got)
	}

	b.applyConfigCommand(protocol.Config{Action: protocol.ConfigSet, Field: protocol.ConfigHome, Value: "https://example.com/"})
	if sink.state.Err != "" {
		t.Fatal(sink.state.Err)
	}
	if got := configValue(t, sink.state.Values, protocol.ConfigHome); got != "https://example.com/" {
		t.Fatalf("home = %q", got)
	}

	b.applyConfigCommand(protocol.Config{Action: protocol.ConfigSet, Field: protocol.ConfigZoom, Value: "10"})
	if !strings.Contains(sink.state.Err, "between 25% and 200%") {
		t.Fatalf("error = %q", sink.state.Err)
	}
	if got := configValue(t, sink.state.Values, protocol.ConfigZoom); got != "100" {
		t.Fatalf("zoom = %q, want the retained value", got)
	}
}

func TestAboutConfigAsksFrontendToOpenSettings(t *testing.T) {
	b := newTestSession(80, 10)
	b.setLoader(newTestLoader())
	sink := &configSink{}
	b.sessionOut = sink
	oldPage := b.Page
	b.Navigate("about:config?home=https%3A%2F%2Fexample.com%2F", true)
	if !sink.opened {
		t.Fatal("about:config did not ask the front end to open settings")
	}
	if b.Page != oldPage {
		t.Fatal("about:config replaced the page in the engine")
	}
	if b.home != "about:home" {
		t.Fatalf("about:config query changed home to %q", b.home)
	}
}
