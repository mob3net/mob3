package browser

import (
	"context"
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
