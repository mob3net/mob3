package browser

import (
	"net/http"
	"net/url"

	"browser/internal/host"
)

type cookieJar struct{ *host.CookieJar }

func newCookieJar() *cookieJar { return &cookieJar{host.NewCookieJar()} }

func (j *cookieJar) documentCookies(u *url.URL) []*http.Cookie {
	return j.DocumentCookies(u)
}
func (j *cookieJar) cookiesForDisplay(u *url.URL) []*http.Cookie {
	return j.CookiesForDisplay(u)
}
func (j *cookieJar) setDocumentCookie(u *url.URL, c *http.Cookie) {
	j.SetDocumentCookie(u, c)
}
