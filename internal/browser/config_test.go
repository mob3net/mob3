package browser

import (
	"net/url"
	"strings"
	"testing"
)

func TestConfigPageShowsValues(t *testing.T) {
	b := NewBrowser(80, 10)
	b.setLoader(NewLoader())
	b.home = "https://example.com/home"
	b.noJS = true
	b.jsMemLimit = 64 << 20
	b.noBigText = true
	b.loader.userAgent = "TestUA"
	b.loader.searchURL = "https://search.example/?q=%s"
	b.cliConfig = map[string]string{
		"-dump": "off", "-width": "80", "-graphics": "auto",
		"-bookmarks": "(none)", "-color": "off", "-no-color": "off",
		"-version": "dev",
	}
	page := b.configPage(mustParse(t, "about:config"), false)
	if !strings.Contains(page.RawHTML, "max-width: 900px") || !strings.Contains(page.RawHTML, "margin: 8px auto") {
		t.Error("config page is not using the narrow reading column")
	}
	text := page.Root.TextContent()
	for _, want := range []string{
		"-dump", "-width", "-graphics", "-no-js", "-reader", "-dark", "-ignore-blocks",
		"-bookmarks", "-home", "-js-memory", "-cache", "-layout-budget", "-text-scale", "-color", "-no-color",
		"-ua", "-search", "-mcp", "-version", "startup",
		"Viewport:", "80x7 characters", "640x112 pixels", "8x16 per cell",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("config page missing %q", want)
		}
	}
	values := map[string]string{}
	checked := map[string]bool{}
	for _, n := range page.Root.FindAll(func(n *Node) bool { return n.Tag == "input" }) {
		values[n.Attr("name")] = n.Attr("value")
		checked[n.Attr("name")] = n.HasAttr("checked")
	}
	selects := map[string]string{}
	for _, n := range page.Root.FindAll(func(n *Node) bool { return n.Tag == "select" }) {
		selects[n.Attr("name")] = inputValue(n)
	}
	want := map[string]string{
		"no-js": "on", "home": "https://example.com/home", "js-memory": "64", "cache": "32",
		"ua": "TestUA", "search": "https://search.example/?q=%s",
	}
	for k, v := range want {
		if values[k] != v {
			t.Errorf("input %s = %q, want %q", k, values[k], v)
		}
	}
	if selects["text-scale"] != "off" {
		t.Errorf("text-scale select = %q, want off", selects["text-scale"])
	}
	if !checked["no-js"] {
		t.Fatal("no-js checkbox is not checked")
	}
	for _, n := range page.Root.FindAll(func(n *Node) bool { return n.Tag == "input" }) {
		if n.Attr("name") == "no-js" && n.Attr("type") != "checkbox" {
			t.Fatalf("no-js input type = %q", n.Attr("type"))
		}
	}
}

func TestConfigPageShowsPixelViewport(t *testing.T) {
	b := NewBrowser(80, 10)
	b.cellW, b.cellH = 10, 20
	page := b.configPage(mustParse(t, "about:config"), false)
	text := page.Root.TextContent()
	for _, want := range []string{"80x7 characters", "800x140 pixels", "10x20 per cell"} {
		if !strings.Contains(text, want) {
			t.Errorf("config page missing %q:\n%s", want, text)
		}
	}
}

func TestApplyConfig(t *testing.T) {
	b := NewBrowser(80, 10)
	b.setLoader(NewLoader())
	q := url.Values{
		"no-js":         {"on"},
		"ignore-blocks": {"on"},
		"home":          {"https://example.com/start"},
		"js-memory":     {"128"},
		"cache":         {"16"},
		"layout-budget": {"high"},
		"text-scale":    {"off"},
		"ua":            {"TestUA"},
		"search":        {""},
	}
	b.applyConfig(q)
	if !b.noJS || !b.loader.noScripts {
		t.Fatal("-no-js was not applied")
	}
	if !b.loader.ignoreBlocks {
		t.Fatal("-ignore-blocks was not applied")
	}
	if b.home != "https://example.com/start" {
		t.Fatalf("home = %q", b.home)
	}
	if b.jsMemLimit != 128<<20 {
		t.Fatalf("js memory = %d", b.jsMemLimit)
	}
	if got := b.loader.cacheLimitMB(); got != 16 {
		t.Fatalf("cache = %d MB, want 16", got)
	}
	if !b.noBigText {
		t.Fatal("text-scale off was not applied")
	}
	if b.layoutBudget.Name != "high" {
		t.Fatalf("layout budget = %q, want high", b.layoutBudget.Name)
	}
	if b.loader.userAgent != "TestUA" {
		t.Fatalf("ua = %q", b.loader.userAgent)
	}
	if b.loader.searchURL != "" {
		t.Fatalf("search = %q", b.loader.searchURL)
	}
}

func TestConfigNavigation(t *testing.T) {
	b := NewBrowser(80, 10)
	b.setLoader(NewLoader())
	b.Navigate("about:config?home=https%3A%2F%2Fexample.com%2Fhome&js-memory=32", true)
	if b.page == nil || b.page.Title != "Config — MOB³" {
		t.Fatalf("config page = %+v", b.page)
	}
	if b.home != "https://example.com/home" {
		t.Fatalf("home = %q", b.home)
	}
	if b.jsMemLimit != 32<<20 {
		t.Fatalf("js memory = %d", b.jsMemLimit)
	}
	values := map[string]string{}
	for _, n := range b.page.Root.FindAll(func(n *Node) bool { return n.Tag == "input" }) {
		values[n.Attr("name")] = n.Attr("value")
	}
	if values["home"] != "https://example.com/home" || values["js-memory"] != "32" {
		t.Fatalf("config inputs = %+v", values)
	}
}

func TestConfigBlockedInReaderMode(t *testing.T) {
	b := NewBrowser(80, 10)
	u := mustParse(t, "https://example.com/article")
	b.page = &Page{URL: u, Base: u, Title: "Article"}
	b.url = u
	b.reader = true
	oldPage := b.page
	withQuietStdout(t, func() { b.Navigate("about:config", true) })
	if b.page != oldPage {
		t.Fatal("config navigation replaced the page in reader mode")
	}
	want := "Config is not available in reader mode — press R to turn it off"
	if b.status != want {
		t.Fatalf("status = %q, want %q", b.status, want)
	}
	if statusLevelFor(b.status) != statusWarning {
		t.Fatal("reader-mode config notice is not a warning")
	}

	cfg := NewBrowser(80, 10)
	cfgURL := mustParse(t, "about:config")
	cfg.page = &Page{URL: cfgURL, Base: cfgURL, Title: "Config — MOB³"}
	cfg.url = cfgURL
	cfg.HandleKey("R")
	if cfg.reader {
		t.Fatal("reader mode was enabled on the config page")
	}
	if cfg.status != "Reader mode is unavailable on the config page" {
		t.Fatalf("status = %q", cfg.status)
	}
}

func TestConfigCheckboxForm(t *testing.T) {
	b := NewBrowser(80, 10)
	b.setLoader(NewLoader())
	b.noJS = true
	b.loader.ignoreBlocks = true
	b.applyConfig(url.Values{"config-form": {"1"}})
	if b.noJS || b.loader.noScripts {
		t.Fatal("unchecked no-js did not turn JS on")
	}
	if b.loader.ignoreBlocks {
		t.Fatal("unchecked ignore-blocks did not turn it off")
	}
}
