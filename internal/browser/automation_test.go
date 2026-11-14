package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"image/png"
	"net/url"
	"strings"
	"testing"
)

// newTestAutomation wires a headless session to a fixed set of paths.
func newTestAutomation(t *testing.T, pages map[string]string) *Automation {
	t.Helper()
	a := NewAutomation(80, 24)
	a.b.loadFn = func(_ context.Context, _ string, target string, _ []byte, _ string) (*Page, error) {
		u, err := url.Parse(target)
		if err != nil {
			return nil, err
		}
		html, ok := pages[u.Path]
		if !ok {
			html = `<html><body>missing</body></html>`
		}
		title := "Page " + u.Path
		if i := strings.Index(html, "<title>"); i >= 0 {
			if j := strings.Index(html[i+7:], "</title>"); j >= 0 {
				title = html[i+7 : i+7+j]
			}
		}
		return &Page{URL: u, Base: u, Root: ParseHTML(html), RawHTML: html, Title: title, Status: 200}, nil
	}
	t.Cleanup(a.Close)
	return a
}

func TestAutomationNavigateAndSnapshot(t *testing.T) {
	a := newTestAutomation(t, map[string]string{
		"/": `<html><head><title>Home</title></head><body>
			<h1>Welcome</h1>
			<p>Hello <a href="/next">Next page</a></p>
			<input name="q" value="">
		</body></html>`,
	})
	ctx := context.Background()
	if err := a.Navigate(ctx, "https://example.com/"); err != nil {
		t.Fatal(err)
	}
	snap := a.Snapshot()
	for _, want := range []string{
		"URL: https://example.com/",
		"Title: Home",
		"Welcome",
		"[1] Next page -> https://example.com/next",
		`name="q"`,
		" at (",
	} {
		if !strings.Contains(snap, want) {
			t.Errorf("snapshot missing %q:\n%s", want, snap)
		}
	}
}

func TestAutomationClickLinkAndBack(t *testing.T) {
	a := newTestAutomation(t, map[string]string{
		"/":     `<html><body><a href="/next">Next page</a></body></html>`,
		"/next": `<html><body><h1>Second page</h1></body></html>`,
	})
	ctx := context.Background()
	if err := a.Navigate(ctx, "https://example.com/"); err != nil {
		t.Fatal(err)
	}
	if err := a.Click(ctx, ClickTarget{Link: 1}); err != nil {
		t.Fatal(err)
	}
	if a.b.url == nil || a.b.url.Path != "/next" {
		t.Fatalf("link click landed on %v", a.b.url)
	}
	if !strings.Contains(a.Snapshot(), "Second page") {
		t.Fatal("snapshot does not show the second page")
	}
	if err := a.Back(ctx); err != nil {
		t.Fatal(err)
	}
	if a.b.url == nil || a.b.url.Path != "/" {
		t.Fatalf("back landed on %v", a.b.url)
	}
}

func TestAutomationClickSelectorAndType(t *testing.T) {
	a := newTestAutomation(t, map[string]string{
		"/": `<html><body><input name="q" value=""></body></html>`,
	})
	ctx := context.Background()
	if err := a.Navigate(ctx, "https://example.com/"); err != nil {
		t.Fatal(err)
	}
	if err := a.Click(ctx, ClickTarget{Selector: `input[name="q"]`}); err != nil {
		t.Fatal(err)
	}
	if a.b.inputFocus == nil {
		t.Fatal("clicking the selector did not focus the input")
	}
	if err := a.Type(ctx, "hello"); err != nil {
		t.Fatal(err)
	}
	if got := inputValue(a.b.inputFocus); got != "hello" {
		t.Fatalf("typed value = %q", got)
	}
	if err := a.Press(ctx, "Backspace"); err != nil {
		t.Fatal(err)
	}
	if got := inputValue(a.b.inputFocus); got != "hell" {
		t.Fatalf("after Backspace = %q", got)
	}
}

func TestAutomationEvaluate(t *testing.T) {
	a := newTestAutomation(t, map[string]string{
		"/": `<html><body><p>calc</p></body></html>`,
	})
	ctx := context.Background()
	if err := a.Navigate(ctx, "https://example.com/"); err != nil {
		t.Fatal(err)
	}
	got, err := a.Evaluate(ctx, "1 + 1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "2" {
		t.Fatalf("evaluate = %q, want 2", got)
	}
}

func TestAutomationScrollAndHistory(t *testing.T) {
	a := newTestAutomation(t, map[string]string{
		"/": `<html><body>` + strings.Repeat("<p>line</p>", 40) + `</body></html>`,
	})
	ctx := context.Background()
	if err := a.Navigate(ctx, "https://example.com/"); err != nil {
		t.Fatal(err)
	}
	a.Scroll(0, 0, false, true)
	if a.b.scroll != a.b.maxScroll() || a.b.scroll == 0 {
		t.Fatalf("bottom scroll = %d, max %d", a.b.scroll, a.b.maxScroll())
	}
	if h := a.History(); !strings.Contains(h, "* https://example.com/") {
		t.Fatalf("history = %q", h)
	}
}

func TestAutomationScreenshot(t *testing.T) {
	a := newTestAutomation(t, map[string]string{
		"/": `<html><body><h1>shot</h1></body></html>`,
	})
	ctx := context.Background()
	if err := a.Navigate(ctx, "https://example.com/"); err != nil {
		t.Fatal(err)
	}
	data, err := a.Screenshot(1)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	top, bottom := a.b.viewport()
	if got, want := img.Bounds().Dy(), (bottom-top)*pngCellH; got != want {
		t.Fatalf("height = %d, want %d", got, want)
	}
	scaled, err := a.Screenshot(2)
	if err != nil {
		t.Fatal(err)
	}
	img2, err := png.Decode(bytes.NewReader(scaled))
	if err != nil {
		t.Fatal(err)
	}
	if img2.Bounds().Dx() != 2*img.Bounds().Dx() {
		t.Fatalf("scaled width = %d, want %d", img2.Bounds().Dx(), 2*img.Bounds().Dx())
	}
}

func TestAutomationRunCommand(t *testing.T) {
	a := newTestAutomation(t, map[string]string{
		"/": `<html><body><p>command page</p></body></html>`,
	})
	ctx := context.Background()
	if _, err := a.RunCommand(ctx, "navigate", json.RawMessage(`{"url":"https://example.com/"}`)); err != nil {
		t.Fatal(err)
	}
	if got, err := a.RunCommand(ctx, "snapshot", nil); err != nil || !strings.Contains(got, "command page") {
		t.Fatalf("snapshot = %q, %v", got, err)
	}
	if got, err := a.RunCommand(ctx, "evaluate", json.RawMessage(`{"expression":"1+1"}`)); err != nil || got != "2" {
		t.Fatalf("evaluate = %q, %v", got, err)
	}
	if _, err := a.RunCommand(ctx, "nope", nil); err == nil {
		t.Fatal("unknown command was accepted")
	}
}

func TestAutomationCoordinatesOutsideViewport(t *testing.T) {
	a := newTestAutomation(t, map[string]string{"/": `<html><body><p>x</p></body></html>`})
	ctx := context.Background()
	if err := a.Navigate(ctx, "https://example.com/"); err != nil {
		t.Fatal(err)
	}
	if err := a.Click(ctx, ClickTarget{X: 999, Y: 0, HasCoords: true}); err == nil {
		t.Fatal("out-of-range click was accepted")
	}
}

func TestAutomationFormFields(t *testing.T) {
	a := newTestAutomation(t, map[string]string{
		"/": `<html><body>
			<form id="search" action="/find" method="get">
				<input name="q" value="old">
				<select name="lang"><option value="en">English</option><option value="fr" selected>French</option></select>
				<input type="checkbox" name="safe" checked>
				<textarea name="notes">hi there</textarea>
				<input type="hidden" name="token" value="secret">
				<button type="submit">Go</button>
			</form>
			<form action="/second">
				<input name="q" value="two">
				<input type="radio" name="r" value="a" checked>
				<input type="radio" name="r" value="b">
			</form>
			<input name="loose" value="z">
		</body></html>`,
	})
	ctx := context.Background()
	if err := a.Navigate(ctx, "https://example.com/"); err != nil {
		t.Fatal(err)
	}
	out, err := a.FormFields("")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Forms: 2",
		`[1] form method="GET" action="/find"`,
		`#search`,
		`name="q"`,
		`#search input[name=`,
		`value="fr"`,
		`options=["en" "fr"]`,
		`state=checked`,
		`value="hi there"`,
		`type="submit"`,
		`text="Go"`,
		"Controls outside any form: 1",
		`name="loose"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("form listing missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, `name="token"`) {
		t.Errorf("hidden input should not be listed:\n%s", out)
	}
	scoped, err := a.FormFields("form#search")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(scoped, "Forms: 1") || strings.Contains(scoped, "/second") {
		t.Fatalf("scoped listing = %q", scoped)
	}
	if _, err := a.FormFields("#nope"); err == nil {
		t.Fatal("unknown form selector was accepted")
	}
}

func TestAutomationFillAndSubmit(t *testing.T) {
	a := newTestAutomation(t, map[string]string{
		"/": `<html><body>
			<form action="/find" method="get">
				<input name="q" value="old">
				<input name="empty" value="x">
				<textarea name="notes">old notes</textarea>
				<input type="checkbox" name="safe">
			</form>
		</body></html>`,
		"/find": `<html><body><h1>Results</h1></body></html>`,
	})
	ctx := context.Background()
	if err := a.Navigate(ctx, "https://example.com/"); err != nil {
		t.Fatal(err)
	}
	n, err := a.Fill(map[string]string{
		`input[name="q"]`:        "new value",
		`input[name="empty"]`:    "",
		`textarea[name="notes"]`: "fresh notes",
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("filled = %d, want 3", n)
	}
	for sel, want := range map[string]string{
		`input[name="q"]`:        "new value",
		`input[name="empty"]`:    "",
		`textarea[name="notes"]`: "fresh notes",
	} {
		node, err := findSelectorNode(a.b.page.Root, sel)
		if err != nil {
			t.Fatal(err)
		}
		if got := inputValue(node); got != want {
			t.Errorf("%s = %q, want %q", sel, got, want)
		}
	}
	yes := true
	if _, err := a.Toggle(`input[name="safe"]`, &yes); err != nil {
		t.Fatal(err)
	}
	if err := a.Submit(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if a.b.url == nil || a.b.url.Path != "/find" {
		t.Fatalf("submitted to %v", a.b.url)
	}
	q := a.b.url.Query()
	if got := q.Get("q"); got != "new value" {
		t.Errorf("q = %q", got)
	}
	if got := q.Get("empty"); got != "" || !q.Has("empty") {
		t.Errorf("empty field missing from submission: %v", q)
	}
	if got := q.Get("notes"); got != "fresh notes" {
		t.Errorf("notes = %q", got)
	}
	if got := q.Get("safe"); got != "on" {
		t.Errorf("safe = %q", got)
	}
}

func TestAutomationFillRejectsNonTextControls(t *testing.T) {
	a := newTestAutomation(t, map[string]string{
		"/": `<html><body>
			<input type="checkbox" name="cb">
			<select name="sel"><option value="a">A</option></select>
			<input name="ro" value="fixed" readonly>
			<input name="dis" value="x" disabled>
		</body></html>`,
	})
	ctx := context.Background()
	if err := a.Navigate(ctx, "https://example.com/"); err != nil {
		t.Fatal(err)
	}
	for sel, want := range map[string]string{
		`input[name="cb"]`:   "use toggle",
		`select[name="sel"]`: "use select",
		`input[name="ro"]`:   "read-only",
		`input[name="dis"]`:  "disabled",
	} {
		if _, err := a.Fill(map[string]string{sel: "x"}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("fill %s error = %v, want %q", sel, err, want)
		}
	}
}

func TestAutomationSelectAndToggle(t *testing.T) {
	a := newTestAutomation(t, map[string]string{
		"/": `<html><body>
			<select name="one">
				<option value="a">Alpha</option>
				<option value="b">Beta</option>
				<option value="c">Gamma</option>
			</select>
			<select name="multi" multiple>
				<option value="x">Xray</option>
				<option value="y">Yankee</option>
				<option value="z">Zulu</option>
			</select>
			<input id="cb" type="checkbox">
			<input id="r1" type="radio" name="r" value="1" checked>
			<input id="r2" type="radio" name="r" value="2">
		</body></html>`,
	})
	ctx := context.Background()
	if err := a.Navigate(ctx, "https://example.com/"); err != nil {
		t.Fatal(err)
	}
	selectNode := func(sel string) *Node {
		t.Helper()
		n, err := findSelectorNode(a.b.page.Root, sel)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	selected := func(sel, value string) bool {
		t.Helper()
		for _, o := range selectOptions(selectNode(sel)) {
			if optionValue(o) == value {
				return o.HasAttr("selected")
			}
		}
		t.Fatalf("option %q not found", value)
		return false
	}
	beta := "b"
	if _, err := a.Select(`select[name="one"]`, &beta, nil, "", nil); err != nil {
		t.Fatal(err)
	}
	if !selected(`select[name="one"]`, "b") || selected(`select[name="one"]`, "a") {
		t.Fatal("value selection did not replace the selected option")
	}
	if _, err := a.Select(`select[name="one"]`, nil, nil, "Gamma", nil); err != nil {
		t.Fatal(err)
	}
	if !selected(`select[name="one"]`, "c") {
		t.Fatal("label selection failed")
	}
	zero := 0
	if _, err := a.Select(`select[name="one"]`, nil, nil, "", &zero); err != nil {
		t.Fatal(err)
	}
	if !selected(`select[name="one"]`, "a") {
		t.Fatal("index selection failed")
	}
	if _, err := a.Select(`select[name="multi"]`, nil, []string{"x", "z"}, "", nil); err != nil {
		t.Fatal(err)
	}
	if got := selectedOptionValues(selectNode(`select[name="multi"]`)); len(got) != 2 || got[0] != "x" || got[1] != "z" {
		t.Fatalf("multi selection = %v", got)
	}
	if _, err := a.Select(`select[name="one"]`, nil, []string{"a", "b"}, "", nil); err == nil {
		t.Fatal("multi-value selection on a single select was accepted")
	}
	missing := "nope"
	if _, err := a.Select(`select[name="one"]`, &missing, nil, "", nil); err == nil {
		t.Fatal("unknown option value was accepted")
	}

	// Checkbox flips, explicit state is idempotent.
	if _, err := a.Toggle("#cb", nil); err != nil {
		t.Fatal(err)
	}
	if !selectNode("#cb").HasAttr("checked") {
		t.Fatal("checkbox did not toggle on")
	}
	yes := true
	if _, err := a.Toggle("#cb", &yes); err != nil {
		t.Fatal(err)
	}
	if !selectNode("#cb").HasAttr("checked") {
		t.Fatal("explicit checked state changed the checkbox")
	}
	// Radio selection unchecks its sibling.
	if _, err := a.Toggle("#r2", &yes); err != nil {
		t.Fatal(err)
	}
	if selectNode("#r1").HasAttr("checked") || !selectNode("#r2").HasAttr("checked") {
		t.Fatal("radio group state is wrong")
	}
	if _, err := a.Toggle("#r2", new(bool)); err == nil {
		t.Fatal("unchecking a checked radio was accepted")
	}
	// Multi-select option toggles.
	if _, err := a.Toggle(`option[value="x"]`, &yes); err != nil {
		t.Fatal(err)
	}
	if !selected(`select[name="multi"]`, "x") {
		t.Fatal("option toggle failed")
	}
}

func TestAutomationSubmitValidationAndPrevented(t *testing.T) {
	a := newTestAutomation(t, map[string]string{
		"/": `<html><body>
			<form action="/find"><input name="q" required></form>
		</body></html>`,
		"/find": `<html><body>ok</body></html>`,
	})
	ctx := context.Background()
	if err := a.Navigate(ctx, "https://example.com/"); err != nil {
		t.Fatal(err)
	}
	err := a.Submit(ctx, "")
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("submit error = %v, want a required-field error", err)
	}
	if a.b.url == nil || a.b.url.Path != "/" {
		t.Fatalf("invalid form navigated to %v", a.b.url)
	}

	prevented := newTestAutomation(t, map[string]string{
		"/":     `<html><body><form action="/find"><input name="q" value="x"></form></body></html>`,
		"/find": `<html><body>ok</body></html>`,
	})
	if err := prevented.Navigate(ctx, "https://example.com/"); err != nil {
		t.Fatal(err)
	}
	if _, err := prevented.Evaluate(ctx, `document.querySelector('form').addEventListener('submit', function (e) { e.preventDefault(); })`); err != nil {
		t.Fatal(err)
	}
	err = prevented.Submit(ctx, "form")
	if err == nil || !strings.Contains(err.Error(), "prevented") {
		t.Fatalf("prevented submit error = %v", err)
	}
	if prevented.b.url == nil || prevented.b.url.Path != "/" {
		t.Fatalf("prevented form navigated to %v", prevented.b.url)
	}
}

func TestAutomationSubmitPOST(t *testing.T) {
	a := newTestAutomation(t, map[string]string{
		"/": `<html><body><form action="/post" method="post"><input name="q" value="old"></form></body></html>`,
	})
	var gotMethod, gotBody string
	baseLoad := a.b.loadFn
	a.b.loadFn = func(ctx context.Context, method, target string, body []byte, contentType string) (*Page, error) {
		u, err := url.Parse(target)
		if err != nil {
			return nil, err
		}
		if u.Path != "/post" {
			return baseLoad(ctx, method, target, body, contentType)
		}
		gotMethod, gotBody = method, string(body)
		return &Page{URL: u, Base: u, Root: ParseHTML(`<html><body>posted</body></html>`), Title: "posted", Status: 200}, nil
	}
	ctx := context.Background()
	if err := a.Navigate(ctx, "https://example.com/"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Fill(map[string]string{`input[name="q"]`: "new"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Submit(ctx, "form"); err != nil {
		t.Fatal(err)
	}
	if gotMethod != "POST" {
		t.Fatalf("method = %q", gotMethod)
	}
	vals, err := url.ParseQuery(gotBody)
	if err != nil {
		t.Fatal(err)
	}
	if got := vals.Get("q"); got != "new" {
		t.Fatalf("posted q = %q", got)
	}
}

func TestAutomationFormCommands(t *testing.T) {
	a := newTestAutomation(t, map[string]string{
		"/": `<html><body><form id="f" action="/find">
			<input name="q" value="old">
			<input type="checkbox" name="safe">
			<select name="lang"><option value="en">English</option><option value="fr">French</option></select>
		</form></body></html>`,
		"/find": `<html><body><h1>Found</h1></body></html>`,
	})
	ctx := context.Background()
	if _, err := a.RunCommand(ctx, "navigate", json.RawMessage(`{"url":"https://example.com/"}`)); err != nil {
		t.Fatal(err)
	}
	if got, err := a.RunCommand(ctx, "form", nil); err != nil || !strings.Contains(got, "Forms: 1") {
		t.Fatalf("form = %q, %v", got, err)
	}
	if got, err := a.RunCommand(ctx, "fill", json.RawMessage(`{"fields":{"input[name=\"q\"]":"new"}}`)); err != nil || !strings.Contains(got, "filled 1") {
		t.Fatalf("fill = %q, %v", got, err)
	}
	if _, err := a.RunCommand(ctx, "toggle", json.RawMessage(`{"selector":"input[name=\"safe\"]","checked":true}`)); err != nil {
		t.Fatal(err)
	}
	if got, err := a.RunCommand(ctx, "select", json.RawMessage(`{"selector":"select[name=\"lang\"]","value":"fr"}`)); err != nil || !strings.Contains(got, "selected") {
		t.Fatalf("select = %q, %v", got, err)
	}
	if got, err := a.RunCommand(ctx, "submit", json.RawMessage(`{"selector":"#f"}`)); err != nil || !strings.Contains(got, "Found") {
		t.Fatalf("submit = %q, %v", got, err)
	}
	q := a.b.url.Query()
	if q.Get("q") != "new" || q.Get("safe") != "on" || q.Get("lang") != "fr" {
		t.Fatalf("submitted query = %v", q)
	}
}

func TestAutomationClickSelectorScrollsIntoView(t *testing.T) {
	a := newTestAutomation(t, map[string]string{
		"/": `<html><body>` + strings.Repeat("<p>filler line</p>", 60) + `<input name="q" value=""></body></html>`,
	})
	ctx := context.Background()
	if err := a.Navigate(ctx, "https://example.com/"); err != nil {
		t.Fatal(err)
	}
	if err := a.Click(ctx, ClickTarget{Selector: `input[name="q"]`}); err != nil {
		t.Fatal(err)
	}
	if a.b.scroll == 0 {
		t.Fatal("selector click did not scroll the field into view")
	}
	if a.b.inputFocus == nil {
		t.Fatal("selector click did not focus the off-screen field")
	}
}

func TestAutomationWaitPumpsTimers(t *testing.T) {
	a := newTestAutomation(t, map[string]string{
		"/": `<html><body><p>start</p></body></html>`,
	})
	ctx := context.Background()
	if err := a.Navigate(ctx, "https://example.com/"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Evaluate(ctx, `setTimeout(function () {
		var p = document.createElement('p');
		p.id = 'late';
		p.textContent = 'late content';
		document.body.appendChild(p);
	}, 30)`); err != nil {
		t.Fatal(err)
	}
	if snap := a.Snapshot(); strings.Contains(snap, "late content") {
		t.Fatalf("timer ran before wait:\n%s", snap)
	}
	waited, err := a.Wait(ctx, 80)
	if err != nil {
		t.Fatal(err)
	}
	if waited != 80 {
		t.Fatalf("waited = %d", waited)
	}
	if snap := a.Snapshot(); !strings.Contains(snap, "late content") {
		t.Fatalf("wait did not apply the timer's DOM change:\n%s", snap)
	}
}

func TestAutomationWaitAppliesTimerNavigation(t *testing.T) {
	a := newTestAutomation(t, map[string]string{
		"/":     `<html><body><p>first</p></body></html>`,
		"/next": `<html><body><p>second</p></body></html>`,
	})
	ctx := context.Background()
	if err := a.Navigate(ctx, "https://example.com/"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Evaluate(ctx, `setTimeout(function () { location.href = "https://example.com/next"; }, 20)`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Wait(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if a.b.url == nil || a.b.url.Path != "/next" {
		t.Fatalf("timer navigation landed on %v", a.b.url)
	}
}

func TestAutomationWaitFor(t *testing.T) {
	a := newTestAutomation(t, map[string]string{
		"/": `<html><body><p>start</p></body></html>`,
	})
	ctx := context.Background()
	if err := a.Navigate(ctx, "https://example.com/"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Evaluate(ctx, `setTimeout(function () {
		var d = document.createElement('div');
		d.id = 'ready';
		d.textContent = 'ready now';
		document.body.appendChild(d);
	}, 30)`); err != nil {
		t.Fatal(err)
	}
	msg, err := a.WaitFor(ctx, "#ready", "", 2000)
	if err != nil || !strings.Contains(msg, "found") {
		t.Fatalf("wait_for selector = %q, %v", msg, err)
	}
	if msg, err := a.WaitFor(ctx, "", "READY NOW", 2000); err != nil || !strings.Contains(msg, "found") {
		t.Fatalf("wait_for text = %q, %v", msg, err)
	}
	if _, err := a.WaitFor(ctx, "#missing", "", 40); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout error = %v", err)
	}
	if _, err := a.WaitFor(ctx, "input[", "", 50); err == nil || !strings.Contains(err.Error(), "bad selector") {
		t.Fatalf("invalid selector error = %v", err)
	}
	if _, err := a.WaitFor(ctx, "", "", 50); err == nil {
		t.Fatal("wait_for without a target was accepted")
	}
}

func TestAutomationWaitCommands(t *testing.T) {
	a := newTestAutomation(t, map[string]string{
		"/": `<html><body><p>start</p></body></html>`,
	})
	ctx := context.Background()
	if _, err := a.RunCommand(ctx, "navigate", json.RawMessage(`{"url":"https://example.com/"}`)); err != nil {
		t.Fatal(err)
	}
	if got, err := a.RunCommand(ctx, "wait", json.RawMessage(`{"ms":10}`)); err != nil || got != "waited 10ms" {
		t.Fatalf("wait = %q, %v", got, err)
	}
	if got, err := a.RunCommand(ctx, "wait_for", json.RawMessage(`{"selector":"p","timeout_ms":500}`)); err != nil || !strings.Contains(got, "found") {
		t.Fatalf("wait_for = %q, %v", got, err)
	}
}
