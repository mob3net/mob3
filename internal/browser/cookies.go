package browser

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"

	"golang.org/x/net/publicsuffix"
)

// cookieJar keeps all HTTP cookies in memory and maintains a second jar
// containing only cookies visible to document.cookie. Keeping the two jars
// separate makes HttpOnly enforcement fall out of the standard cookie rules.
type cookieJar struct {
	all     http.CookieJar
	visible http.CookieJar
}

func newCookieJar() *cookieJar {
	options := &cookiejar.Options{PublicSuffixList: publicsuffix.List}
	all, _ := cookiejar.New(options)
	visible, _ := cookiejar.New(options)
	return &cookieJar{all: all, visible: visible}
}

func (j *cookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.all.SetCookies(u, cookies)
	visible := make([]*http.Cookie, 0, len(cookies))
	for _, c := range cookies {
		if c.HttpOnly {
			// A server may replace a JS-visible cookie with an HttpOnly one.
			// Remove any previous visible entry with the same identity.
			j.visible.SetCookies(u, []*http.Cookie{{
				Name:   c.Name,
				Domain: c.Domain,
				Path:   c.Path,
				MaxAge: -1,
			}})
			continue
		}
		visible = append(visible, c)
	}
	if len(visible) > 0 {
		j.visible.SetCookies(u, visible)
	}
}

func (j *cookieJar) Cookies(u *url.URL) []*http.Cookie {
	return j.all.Cookies(u)
}

func (j *cookieJar) documentCookies(u *url.URL) []*http.Cookie {
	return j.visible.Cookies(u)
}

// setDocumentCookie applies a document.cookie assignment. JavaScript cannot
// set HttpOnly cookies, so that attribute is ignored.
func (j *cookieJar) setDocumentCookie(u *url.URL, c *http.Cookie) {
	c.HttpOnly = false
	j.SetCookies(u, []*http.Cookie{c})
}
