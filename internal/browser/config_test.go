package browser

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestConfigPageShowsValues(t *testing.T) {
	b := NewBrowser(80, 10)
	b.setLoader(NewLoader())
	b.home = "https://example.com/home"
	b.NoJS = true
	b.JSMemLimit = 64 << 20
	b.NoBigText = true
	b.loader.setUserAgent("TestUA")
	b.loader.setSearchURL("https://search.example/?q=%s")
	b.cliConfig = map[string]string{
		"-dump": "off", "-width": "80", "-graphics": "auto",
		"-color": "off", "-no-color": "off",
		"-version": "dev",
	}
	page := b.configPage(mustParse(t, "about:config"), false)
	if !strings.Contains(page.RawHTML, "max-width: 900px") || !strings.Contains(page.RawHTML, "margin: 8px auto") {
		t.Error("config page is not using the narrow reading column")
	}
	text := page.Root.TextContent()
	for _, want := range []string{
		"-dump", "-width", "-graphics", "-no-js", "-reader", "-dark", "-ignore-blocks",
		"-home", "-layout-budget", "-text-scale", "-color", "-no-color",
		"-ua", "-search", "-mcp", "-version", "startup",
		"Viewport:", "80x7 characters", "640x112 pixels", "8x16 per cell",
		"JS 64 MB", "cache 32 MB", "history 128 MB",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("config page missing %q", want)
		}
	}
	if strings.Contains(text, "-js-memory") {
		t.Error("config page still lists the removed -js-memory option")
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
		"no-js": "on", "home": "https://example.com/home",
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
	b.CellW, b.CellH = 10, 20
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
		"layout-budget": {"high"},
		"text-scale":    {"off"},
		"ua":            {"TestUA"},
		"search":        {""},
	}
	b.applyConfig(q)
	if !b.NoJS || !b.loader.noScripts {
		t.Fatal("-no-js was not applied")
	}
	if !b.loader.ignoreBlocks {
		t.Fatal("-ignore-blocks was not applied")
	}
	if b.home != "https://example.com/start" {
		t.Fatalf("home = %q", b.home)
	}
	if !b.NoBigText {
		t.Fatal("text-scale off was not applied")
	}
	if b.LayoutBudget.Name != "high" {
		t.Fatalf("layout budget = %q, want high", b.LayoutBudget.Name)
	}
	if b.loader.requestUserAgent() != "TestUA" {
		t.Fatalf("ua = %q", b.loader.requestUserAgent())
	}
	if b.loader.searchTemplate() != "" {
		t.Fatalf("search = %q", b.loader.searchTemplate())
	}
}

func TestConfigNavigation(t *testing.T) {
	b := NewBrowser(80, 10)
	b.setLoader(NewLoader())
	oldHome, oldMem := b.home, b.JSMemLimit
	// A typed query without the form token applies nothing.
	b.Navigate("about:config?home=https%3A%2F%2Fexample.com%2Fhome", true)
	if b.Page == nil || b.Page.Title != "Config — MOB³" {
		t.Fatalf("config page = %+v", b.Page)
	}
	if b.home != oldHome || b.JSMemLimit != oldMem {
		t.Fatal("typed about:config query changed settings without a token")
	}
	if !strings.Contains(b.Page.Root.TextContent(), "Settings not changed") {
		t.Fatal("missing notice for a token-less config query")
	}

	// The rendered form carries a token and posts its values.
	form := b.Page.Root.FindFirst("form")
	if form == nil || strings.ToLower(form.Attr("method")) != "post" {
		t.Fatalf("config form = %v, want method post", form)
	}
	token := configInputValue(b.Page, configTokenParam)
	if token == "" || token != b.configToken.Current() {
		t.Fatalf("form token = %q, browser token = %q", token, b.configToken.Current())
	}
	for _, n := range b.Page.Root.FindAll(func(n *Node) bool { return n.Tag == "input" }) {
		if n.Attr("name") == "home" {
			n.Value, n.ValueSet = "https://example.com/home", true
		}
	}
	if started, reason := b.submitForm(form); !started {
		t.Fatalf("config form did not submit: %s", reason)
	}
	if b.home != "https://example.com/home" {
		t.Fatalf("home = %q", b.home)
	}
	values := map[string]string{}
	for _, n := range b.Page.Root.FindAll(func(n *Node) bool { return n.Tag == "input" }) {
		values[n.Attr("name")] = inputValue(n)
	}
	if values["home"] != "https://example.com/home" {
		t.Fatalf("config inputs = %+v", values)
	}
	if b.configToken.Current() == token {
		t.Fatal("config token was not rotated after use")
	}

	// Replaying the used token from an old page applies nothing.
	body := url.Values{
		configTokenParam: {token},
		"config-form":    {"1"},
		"home":           {"https://evil.example/"},
	}.Encode()
	b.navigateRequest(http.MethodPost, "about:config", []byte(body), "application/x-www-form-urlencoded", true)
	if b.home == "https://evil.example/" {
		t.Fatal("replayed config token changed the home page")
	}
}

// configInputValue returns the first input value with the given name.
func configInputValue(page *Page, name string) string {
	for _, n := range page.Root.FindAll(func(n *Node) bool {
		return n.Tag == "input" && n.Attr("name") == name
	}) {
		return inputValue(n)
	}
	return ""
}

func TestConfigBlockedInReaderMode(t *testing.T) {
	b := NewBrowser(80, 10)
	u := mustParse(t, "https://example.com/article")
	b.Page = &Page{URL: u, Base: u, Title: "Article"}
	b.URL = u
	b.Reader = true
	oldPage := b.Page
	withQuietStdout(t, func() { b.Navigate("about:config", true) })
	if b.Page != oldPage {
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
	cfg.Page = &Page{URL: cfgURL, Base: cfgURL, Title: "Config — MOB³"}
	cfg.URL = cfgURL
	cfg.toggleReader()
	if cfg.Reader {
		t.Fatal("reader mode was enabled on the config page")
	}
	if cfg.status != "Reader mode is unavailable on the config page" {
		t.Fatalf("status = %q", cfg.status)
	}
}

func TestConfigCheckboxForm(t *testing.T) {
	b := NewBrowser(80, 10)
	b.setLoader(NewLoader())
	b.NoJS = true
	b.loader.ignoreBlocks = true
	b.applyConfig(url.Values{"config-form": {"1"}})
	if b.NoJS || b.loader.noScripts {
		t.Fatal("unchecked no-js did not turn JS on")
	}
	if b.loader.ignoreBlocks {
		t.Fatal("unchecked ignore-blocks did not turn it off")
	}
}

func TestConfigAppliesZoom(t *testing.T) {
	b := NewBrowser(80, 24)
	defer setLayoutZoom(1)
	b.applyConfig(url.Values{"zoom": {"150%"}})
	if b.Zoom != 1.5 {
		t.Fatalf("zoom = %g, want 1.5", b.Zoom)
	}
	if got := px(16).ResolveH(0, 16); got != 3 {
		t.Fatalf("16px at 150%% = %d cells, want 3", got)
	}
	found := false
	for _, it := range b.configItems() {
		if it.Flag == "-zoom" {
			found = true
			if it.Value != "150%" {
				t.Fatalf("-zoom value = %q, want 150%%", it.Value)
			}
		}
	}
	if !found {
		t.Fatal("about:config does not list -zoom")
	}
	// Out-of-range values are ignored.
	b.applyConfig(url.Values{"zoom": {"10"}})
	if b.Zoom != 1.5 {
		t.Fatalf("invalid zoom changed value to %g", b.Zoom)
	}
}
