package engine

import (
	"context"
	"net/url"
	"testing"

	"browser/internal/host"
	"browser/internal/protocol"
)

// fixtureDoc is one in-memory document served at a synthetic URL.
type fixtureDoc struct {
	data   []byte
	ctype  string
	status int
}

// fixtureHost serves in-memory documents for tests that need the real loader
// path without a network. Top-level data: documents are rejected, and engine
// tests stay network-free, so fixtures use a synthetic https origin. Other
// URLs (for example data: image subresources) fall through to the real host.
type fixtureHost struct {
	*host.Session
	docs map[string]fixtureDoc
}

func (h *fixtureHost) fixture(u *url.URL) (protocol.HostFetch, bool) {
	if u == nil {
		return protocol.HostFetch{}, false
	}
	f, ok := h.docs[u.String()]
	if !ok {
		return protocol.HostFetch{}, false
	}
	return protocol.HostFetch{
		Data: f.data, ContentType: f.ctype, FinalURL: u,
		Status: f.status, PageLocal: false,
	}, true
}

func (h *fixtureHost) Fetch(ctx context.Context, method string, u *url.URL, body []byte, contentType string) (protocol.HostFetch, error) {
	if r, ok := h.fixture(u); ok {
		return r, nil
	}
	return h.Session.Fetch(ctx, method, u, body, contentType)
}

func (h *fixtureHost) FetchCtx(ctx context.Context, u *url.URL) (protocol.HostFetch, error) {
	if r, ok := h.fixture(u); ok {
		return r, nil
	}
	return h.Session.FetchCtx(ctx, u)
}

// fixtureLoader returns a loader whose https://fixture.test/ document serves
// data with ctype from memory.
func fixtureLoader(t *testing.T, data []byte, ctype string) (*Loader, string) {
	t.Helper()
	u, err := url.Parse("https://fixture.test/")
	if err != nil {
		t.Fatal(err)
	}
	h := &fixtureHost{
		Session: host.NewSession(),
		docs: map[string]fixtureDoc{
			u.String(): {data: data, ctype: ctype, status: 200},
		},
	}
	return NewLoader(h), u.String()
}

func htmlFixtureLoader(t *testing.T, html string) (*Loader, string) {
	t.Helper()
	return fixtureLoader(t, []byte(html), "text/html; charset=utf-8")
}
