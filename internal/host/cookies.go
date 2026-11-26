package host

import (
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"

	"browser/internal/protocol"
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
}

func NewCookieJar() *CookieJar {
	options := &cookiejar.Options{PublicSuffixList: publicsuffix.List}
	all, _ := cookiejar.New(options)
	visible, _ := cookiejar.New(options)
	return &CookieJar{all: all, visible: visible, cookies: make(map[cookieIdentity]cookieMeta)}
}

// cookieIdentityFor computes the identity cookiejar will store for c on u.
// ok is false for a cookie the jar rejects, such as a foreign or public-suffix
// Domain attribute. Both the request host and the Domain attribute are
// canonicalized to the jar's lower-case punycode form before comparing, so an
// IDN host cannot claim a Domain it does not own.
func cookieIdentityFor(u *url.URL, c *http.Cookie) (id cookieIdentity, hostOnly, ok bool) {
	if u == nil || c == nil {
		return id, false, false
	}
	host, ok := canonicalCookieHost(u.Hostname())
	if !ok {
		return id, false, false
	}
	path := c.Path
	if path == "" || path[0] != '/' {
		path = defaultCookiePath(u.Path)
	}
	domain := c.Domain
	if domain == "" {
		// Host-only: the jar scopes the cookie to the canonical request host.
		return cookieIdentity{domain: host, path: path, name: c.Name}, true, true
	}
	// A Domain attribute is an ASCII wire value; the jar rejects a non-ASCII
	// one outright, so mirror that rather than inventing a mapping.
	domain = strings.TrimPrefix(domain, ".")
	if domain == "" || domain[0] == '.' || domain[len(domain)-1] == '.' || !isASCIIDomain(domain) {
		return id, false, false
	}
	domain = strings.ToLower(domain)
	if net.ParseIP(host) != nil {
		// The jar treats an IP Domain attribute as host-only and accepts it
		// only when it names the host exactly.
		if host != domain {
			return id, false, false
		}
		return cookieIdentity{domain: host, path: path, name: c.Name}, true, true
	}
	if domain == host {
		// The jar stores a Domain naming the exact host as host-only when the
		// name is also a public suffix, and as a domain cookie otherwise.
		hostOnly = publicsuffix.List.PublicSuffix(domain) == domain
		return cookieIdentity{domain: domain, path: path, name: c.Name}, hostOnly, true
	}
	if publicsuffix.List.PublicSuffix(domain) == domain {
		return id, false, false
	}
	if !strings.HasSuffix(host, "."+domain) {
		return id, false, false
	}
	return cookieIdentity{domain: domain, path: path, name: c.Name}, false, true
}

// canonicalCookieHost returns the lower-case ASCII host the standard jar keys
// cookies by. An ASCII host is passed through after trimming the trailing dot;
// an internationalized host is converted to its punycode form.
func canonicalCookieHost(host string) (string, bool) {
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return "", false
	}
	if !isASCIIDomain(host) {
		ascii, err := idna.Lookup.ToASCII(host)
		if err != nil || ascii == "" {
			return "", false
		}
		host = ascii
	}
	return strings.ToLower(host), true
}

// cookieDomainMatches reports whether a stored domain applies to a canonical
// request host, rejecting a public-suffix domain even if a stored identity
// were ever corrupted into one.
func cookieDomainMatches(host, domain string) bool {
	if host == domain {
		return true
	}
	if net.ParseIP(host) != nil {
		return false
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
		if id, hostOnly, ok := cookieIdentityFor(u, c); ok {
			if cookieDeletes(c) {
				delete(j.cookies, id)
			} else {
				m := cookieMeta{
					value:    c.Value,
					httpOnly: c.HttpOnly,
					secure:   c.Secure,
					sameSite: c.SameSite,
					hostOnly: hostOnly,
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

// RequestCookiePolicy is the SameSite context for one request chain. TopSite
// is the schemeful site of the initiating/top-level document (empty when
// unknown, which disables the comparison). TopLevel marks a document
// navigation, for which SameSite=Lax allows safe GET/HEAD requests.
type RequestCookiePolicy struct {
	TopSite  string
	TopLevel bool
}

// siteOf returns scheme://registrable-domain (or scheme://host for an IP or a
// host without a public suffix) for SameSite comparisons.
func siteOf(u *url.URL) string {
	if u == nil {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "" {
		return ""
	}
	if net.ParseIP(host) == nil {
		if reg, err := publicsuffix.EffectiveTLDPlusOne(host); err == nil {
			host = reg
		}
	}
	return scheme + "://" + host
}

// RequestCookies returns the cookies to attach to a request, applying the
// stored SameSite attribute against p. An unset SameSite keeps the historical
// behavior (sent on every matching request). SameSite=None is sent only when
// the cookie is Secure.
func (j *CookieJar) RequestCookies(u *url.URL, method string, p RequestCookiePolicy) []*http.Cookie {
	if j == nil || u == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	now := time.Now()
	site := siteOf(u)
	sameSite := p.TopSite == "" || site == p.TopSite
	safeTop := p.TopLevel && (strings.EqualFold(method, http.MethodGet) || strings.EqualFold(method, http.MethodHead))
	type selected struct {
		c    *http.Cookie
		path string
	}
	var out []selected
	for id, m := range j.cookies {
		if !m.expires.IsZero() && !m.expires.After(now) {
			delete(j.cookies, id)
			continue
		}
		if !cookieApplies(u, id, m) {
			continue
		}
		switch m.sameSite {
		case http.SameSiteStrictMode:
			if !sameSite {
				continue
			}
		case http.SameSiteLaxMode:
			if !sameSite && !safeTop {
				continue
			}
		case http.SameSiteNoneMode:
			if !m.secure || u.Scheme != "https" {
				continue
			}
		}
		out = append(out, selected{c: &http.Cookie{Name: id.name, Value: m.value}, path: id.path})
	}
	// RFC 6265 5.4: longer paths first. Map iteration order is otherwise
	// unspecified, so sort for a deterministic header.
	sort.SliceStable(out, func(i, k int) bool {
		if len(out[i].path) != len(out[k].path) {
			return len(out[i].path) > len(out[k].path)
		}
		return out[i].c.Name < out[k].c.Name
	})
	cookies := make([]*http.Cookie, len(out))
	for i := range out {
		cookies[i] = out[i].c
	}
	return cookies
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

// DocumentCookieValues returns the document.cookie name/value pairs visible to
// page scripts.
func (j *CookieJar) DocumentCookieValues(u *url.URL) []protocol.CookieView {
	cookies := j.DocumentCookies(u)
	out := make([]protocol.CookieView, 0, len(cookies))
	for _, c := range cookies {
		out = append(out, protocol.CookieView{Name: c.Name, Value: c.Value})
	}
	return out
}

// CookieViews returns display-safe cookie rows for the storage inspector.
func (j *CookieJar) CookieViews(u *url.URL) []protocol.CookieView {
	j.mu.Lock()
	defer j.mu.Unlock()
	now := time.Now()
	out := make([]protocol.CookieView, 0, len(j.cookies))
	for id, m := range j.cookies {
		if !m.expires.IsZero() && !m.expires.After(now) {
			delete(j.cookies, id)
			continue
		}
		if !cookieApplies(u, id, m) {
			continue
		}
		domain := id.domain
		if !m.hostOnly {
			domain = "." + id.domain
		}
		out = append(out, protocol.CookieView{
			Name:     id.name,
			Value:    m.value,
			Domain:   domain,
			Path:     id.path,
			SameSite: sameSiteLabel(m.sameSite),
			HttpOnly: m.httpOnly,
			Secure:   m.secure,
			Expires:  m.expires,
		})
	}
	return out
}

func sameSiteLabel(v http.SameSite) string {
	switch v {
	case http.SameSiteLaxMode:
		return "lax"
	case http.SameSiteStrictMode:
		return "strict"
	case http.SameSiteNoneMode:
		return "none"
	}
	return ""
}

// SetDocumentCookieRaw parses and applies a document.cookie assignment. It
// returns false when the assignment is malformed or empty.
func (j *CookieJar) SetDocumentCookieRaw(u *url.URL, raw string) bool {
	if u == nil {
		return false
	}
	c, err := http.ParseSetCookie(strings.TrimSpace(raw))
	if err != nil || c.Name == "" {
		return false
	}
	j.SetDocumentCookie(u, c)
	return true
}

// cookieApplies reports whether a stored cookie would be sent to u.
func cookieApplies(u *url.URL, id cookieIdentity, m cookieMeta) bool {
	if u == nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	if m.secure && u.Scheme != "https" {
		return false
	}
	host, ok := canonicalCookieHost(u.Hostname())
	if !ok {
		return false
	}
	if m.hostOnly {
		if host != id.domain {
			return false
		}
	} else if !cookieDomainMatches(host, id.domain) {
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
	id, _, ok := cookieIdentityFor(u, c)
	if !ok {
		return false
	}
	for key, e := range j.cookies {
		if e.httpOnly && key == id {
			return true
		}
	}
	return false
}
