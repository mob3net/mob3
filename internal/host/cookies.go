package host

import (
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/publicsuffix"
)

// CookieJar keeps all HTTP cookies in memory and maintains a second jar
// containing only cookies visible to document.cookie. Keeping the two jars
// separate makes HttpOnly enforcement fall out of the standard cookie rules.
type CookieJar struct {
	// mu guards cookies and is held across jar updates so a server response
	// arriving on a background goroutine and a document.cookie write on the
	// main goroutine cannot interleave between the visibility check and the
	// write.
	mu      sync.Mutex
	all     http.CookieJar
	visible http.CookieJar
	// cookies tracks the metadata cookiejar drops on read (domain, path,
	// flags, expiry) for the storage inspector, and marks HttpOnly cookies so
	// document.cookie can refuse to replace or delete them.
	cookies map[cookieIdentity]cookieMeta
}

// cookieIdentity is the (effective domain, path, name) triple cookiejar uses
// to replace a stored cookie.
type cookieIdentity struct {
	domain string
	path   string
	name   string
}

type cookieMeta struct {
	value    string
	httpOnly bool
	secure   bool
	sameSite http.SameSite
	hostOnly bool
	expires  time.Time // zero for a session cookie
	// asciiDomain reports that domain can be compared exactly. Cookies on a
	// non-ASCII host are stored punycoded by cookiejar, so their identity is
	// compared conservatively instead.
	asciiDomain bool
}

func NewCookieJar() *CookieJar {
	options := &cookiejar.Options{PublicSuffixList: publicsuffix.List}
	all, _ := cookiejar.New(options)
	visible, _ := cookiejar.New(options)
	return &CookieJar{all: all, visible: visible, cookies: make(map[cookieIdentity]cookieMeta)}
}

// cookieIdentityFor computes the identity cookiejar will store for c on u.
// ok is false for a cookie the jar rejects, such as a foreign or public-suffix
// Domain attribute.
func cookieIdentityFor(u *url.URL, c *http.Cookie) (id cookieIdentity, ascii bool, ok bool) {
	if u == nil || c == nil {
		return id, false, false
	}
	path := c.Path
	if path == "" || path[0] != '/' {
		path = defaultCookiePath(u.Path)
	}
	domain := c.Domain
	if domain != "" {
		domain = strings.TrimPrefix(domain, ".")
		if domain == "" || domain[0] == '.' || domain[len(domain)-1] == '.' ||
			!isASCIIDomain(domain) || !cookieDomainMatches(u.Hostname(), domain) {
			return id, false, false
		}
		ascii = true
	} else {
		domain = strings.TrimSuffix(u.Hostname(), ".")
		ascii = isASCIIDomain(domain)
	}
	if !ascii {
		domain = strings.ToLower(domain)
	}
	return cookieIdentity{domain: strings.ToLower(domain), path: path, name: c.Name}, ascii, true
}

// cookieDomainMatches reports whether a Domain attribute is one cookiejar will
// accept for host. A non-ASCII host cannot be compared against the jar's
// punycoded form here, so it is accepted and the identity check falls back to
// a conservative comparison.
func cookieDomainMatches(host, domain string) bool {
	if !isASCIIDomain(host) {
		return true
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	domain = strings.ToLower(domain)
	if net.ParseIP(host) != nil {
		return host == domain
	}
	if host == domain {
		return true
	}
	if publicsuffix.List.PublicSuffix(domain) == domain {
		return false
	}
	return strings.HasSuffix(host, "."+domain)
}

func isASCIIDomain(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// defaultCookiePath is RFC 6265 section 5.1.4, matching cookiejar.
func defaultCookiePath(path string) string {
	if len(path) == 0 || path[0] != '/' {
		return "/"
	}
	if i := strings.LastIndex(path, "/"); i > 0 {
		return path[:i]
	}
	return "/"
}

// cookieDeletes reports whether c removes a stored cookie rather than setting
// it, matching cookiejar's MaxAge/Expires rules.
func cookieDeletes(c *http.Cookie) bool {
	if c.MaxAge < 0 {
		return true
	}
	if c.MaxAge > 0 {
		return false
	}
	return !c.Expires.IsZero() && !c.Expires.After(time.Now())
}

func (j *CookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.setCookiesLocked(u, cookies)
}

func (j *CookieJar) setCookiesLocked(u *url.URL, cookies []*http.Cookie) {
	j.all.SetCookies(u, cookies)
	visible := make([]*http.Cookie, 0, len(cookies))
	for _, c := range cookies {
		if id, ascii, ok := cookieIdentityFor(u, c); ok {
			if cookieDeletes(c) {
				delete(j.cookies, id)
			} else {
				m := cookieMeta{
					value:       c.Value,
					httpOnly:    c.HttpOnly,
					secure:      c.Secure,
					sameSite:    c.SameSite,
					hostOnly:    c.Domain == "",
					asciiDomain: ascii,
				}
				if c.MaxAge > 0 {
					m.expires = time.Now().Add(time.Duration(c.MaxAge) * time.Second)
				} else if c.MaxAge == 0 {
					m.expires = c.Expires
				}
				j.cookies[id] = m
			}
		}
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

func (j *CookieJar) Cookies(u *url.URL) []*http.Cookie {
	return j.all.Cookies(u)
}

func (j *CookieJar) DocumentCookies(u *url.URL) []*http.Cookie {
	return j.visible.Cookies(u)
}

// CookiesForDisplay returns every cookie that applies to u with its stored
// metadata, for the storage inspector. cookiejar drops the flags and scope on
// read, so the tracked copy is the source here.
func (j *CookieJar) CookiesForDisplay(u *url.URL) []*http.Cookie {
	j.mu.Lock()
	defer j.mu.Unlock()
	now := time.Now()
	out := make([]*http.Cookie, 0, len(j.cookies))
	for id, m := range j.cookies {
		if !m.expires.IsZero() && !m.expires.After(now) {
			delete(j.cookies, id)
			continue
		}
		if !cookieApplies(u, id, m) {
			continue
		}
		c := &http.Cookie{
			Name:     id.name,
			Value:    m.value,
			Path:     id.path,
			HttpOnly: m.httpOnly,
			Secure:   m.secure,
			SameSite: m.sameSite,
			Expires:  m.expires,
		}
		if m.hostOnly {
			c.Domain = id.domain
		} else {
			c.Domain = "." + id.domain
		}
		out = append(out, c)
	}
	return out
}

// cookieApplies reports whether a stored cookie would be sent to u.
func cookieApplies(u *url.URL, id cookieIdentity, m cookieMeta) bool {
	if u == nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	if m.secure && u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if m.hostOnly {
		if host != id.domain {
			return false
		}
	} else if m.asciiDomain {
		if !cookieDomainMatches(host, id.domain) {
			return false
		}
	} else if host != id.domain && !strings.HasSuffix(host, "."+id.domain) {
		return false
	}
	return cookiePathMatches(u.Path, id.path)
}

// cookiePathMatches is RFC 6265 section 5.1.4.
func cookiePathMatches(requestPath, cookiePath string) bool {
	if requestPath == "" {
		requestPath = "/"
	}
	if requestPath == cookiePath {
		return true
	}
	if !strings.HasPrefix(requestPath, cookiePath) {
		return false
	}
	if strings.HasSuffix(cookiePath, "/") {
		return true
	}
	return len(requestPath) > len(cookiePath) && requestPath[len(cookiePath)] == '/'
}

// SetDocumentCookie applies a document.cookie assignment. A script may neither
// replace nor delete a server-set HttpOnly cookie, per RFC 6265bis.
func (j *CookieJar) SetDocumentCookie(u *url.URL, c *http.Cookie) {
	c.HttpOnly = false
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.httpOnlyProtectedLocked(u, c) {
		return
	}
	j.setCookiesLocked(u, []*http.Cookie{c})
}

// httpOnlyProtectedLocked reports whether c has the same identity as a stored
// HttpOnly cookie. Callers hold j.mu.
func (j *CookieJar) httpOnlyProtectedLocked(u *url.URL, c *http.Cookie) bool {
	id, ascii, ok := cookieIdentityFor(u, c)
	if !ok {
		return false
	}
	for key, e := range j.cookies {
		if !e.httpOnly || key.name != id.name || key.path != id.path {
			continue
		}
		if ascii && e.asciiDomain && key.domain != id.domain {
			continue
		}
		return true
	}
	return false
}
