package engine

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

type testPageFetcher struct{}

func (testPageFetcher) FetchPageResource(context.Context, *url.URL, string) ([]byte, string, int, error) {
	return nil, "", 404, nil
}
func (testPageFetcher) DecodeBody(data []byte, _ string) string { return string(data) }
func (testPageFetcher) IsJavaScriptMIME(string) bool            { return true }

func TestAboutPageName(t *testing.T) {
	u, _ := url.Parse("about:help?x=1")
	if got := AboutPageName(u); got != "help" {
		t.Fatalf("name = %q", got)
	}
	u, _ = url.Parse("about:blank")
	if got := AboutPageName(u); got != "blank" {
		t.Fatalf("name = %q", got)
	}
	if AboutPageName(nil) != "" {
		t.Fatal("nil URL had a page name")
	}
}

func TestAssembleAboutPages(t *testing.T) {
	u, _ := url.Parse("about:home")
	page := AssembleAboutPage(context.Background(), testPageFetcher{}, u, "9.9.9", nil, -1, true)
	if page == nil || page.Title != "MOB3" {
		t.Fatalf("home page = %+v", page)
	}
	if !strings.Contains(page.RawHTML, "small terminal web browser") {
		t.Fatal("home page body missing")
	}
	if len(page.Author) == 0 {
		t.Fatal("home page reader rules were not appended")
	}

	help, _ := url.Parse("about:help")
	page = AssembleAboutPage(context.Background(), testPageFetcher{}, help, "9.9.9", nil, -1, true)
	if page.Title != "Help — MOB3" || !strings.Contains(page.RawHTML, "9.9.9") {
		t.Fatalf("help page = %q / %q", page.Title, page.RawHTML[:min(len(page.RawHTML), 80)])
	}
}

func TestBlockedResponse(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   bool
	}{
		{200, "just a moment", false},
		{403, "just a moment", true},
		{503, "access denied", true},
		{403, "ordinary page", false},
	}
	for _, c := range cases {
		if got, _ := BlockedResponse(c.status, c.body); got != c.want {
			t.Errorf("BlockedResponse(%d, %q) = %v, want %v", c.status, c.body, got, c.want)
		}
	}
}

func TestAssembleBlockedPage(t *testing.T) {
	u, _ := url.Parse("https://example.com/")
	page := AssembleBlockedPage(context.Background(), testPageFetcher{}, u, "<html></html>", "browser challenge", 403, true)
	if page == nil || !page.Blocked || page.Status != 403 || !page.Local {
		t.Fatalf("blocked page = %+v", page)
	}
	if page.Title != "Site blocked" || page.Root == nil {
		t.Fatalf("blocked page title/root = %+v", page)
	}
}
