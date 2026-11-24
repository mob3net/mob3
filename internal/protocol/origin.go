package protocol

import (
	"net/url"
	"strings"
)

// Origin is a serialized web origin. An opaque origin never compares equal to
// another origin, and serializes as "null".
type Origin struct {
	Scheme string
	Host   string
	Port   string
	Opaque bool
}

// OriginOf derives the canonical origin of u. Only http and https have a
// tuple origin; about, data and every other scheme are opaque. Host names are
// lowercased and a default port is omitted.
func OriginOf(u *url.URL) Origin {
	if u == nil {
		return Origin{Opaque: true}
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return Origin{Opaque: true}
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return Origin{Opaque: true}
	}
	port := u.Port()
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}
	return Origin{Scheme: scheme, Host: host, Port: port}
}

// ParseOrigin parses a serialized origin, accepting an optional trailing
// slash. "null" parses as an opaque origin; an empty string is invalid.
func ParseOrigin(raw string) (Origin, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "null" {
		return Origin{Opaque: true}, true
	}
	if raw == "" {
		return Origin{}, false
	}
	u, err := url.Parse(strings.TrimRight(raw, "/"))
	if err != nil || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return Origin{}, false
	}
	o := OriginOf(u)
	if o.Opaque {
		return Origin{}, false
	}
	return o, true
}

// IsOpaque reports whether the origin is opaque.
func (o Origin) IsOpaque() bool {
	return o.Opaque || o.Scheme == "" || o.Host == ""
}

// String returns the serialized origin, or "null" when opaque.
func (o Origin) String() string {
	if o.IsOpaque() {
		return "null"
	}
	host := o.Host
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if o.Port != "" {
		return o.Scheme + "://" + host + ":" + o.Port
	}
	return o.Scheme + "://" + host
}

// HeaderValue returns the origin as an Origin header or CORS value. Opaque
// origins return an empty string.
func (o Origin) HeaderValue() string {
	if o.IsOpaque() {
		return ""
	}
	return o.String()
}

// Equal reports whether two tuple origins are the same. Opaque origins never
// compare equal.
func (o Origin) Equal(other Origin) bool {
	if o.IsOpaque() || other.IsOpaque() {
		return false
	}
	return o.Scheme == other.Scheme && o.Host == other.Host && o.Port == other.Port
}

// SameOrigin reports whether two URLs share a tuple origin.
func SameOrigin(a, b *url.URL) bool {
	return OriginOf(a).Equal(OriginOf(b))
}
