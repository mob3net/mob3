package browser

import (
	"net/url"

	"browser/internal/host"
	"browser/internal/protocol"
)

// cookieJar is the host-owned session cookie jar with the browser's old API.
type cookieJar struct{ *host.CookieJar }

func newCookieJar() *cookieJar { return &cookieJar{host.NewCookieJar()} }

func (j *cookieJar) setDocumentCookieRaw(u *url.URL, raw string) bool {
	return j.SetDocumentCookieRaw(u, raw)
}

func (j *cookieJar) cookieViews(u *url.URL) []protocol.CookieView {
	return j.CookieViews(u)
}
