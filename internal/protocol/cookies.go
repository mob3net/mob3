package protocol

import "time"

// CookieView is one cookie row for the storage inspector, with host metadata
// reduced to display-safe values.
type CookieView struct {
	Name     string
	Value    string
	Domain   string
	Path     string
	SameSite string
	HttpOnly bool
	Secure   bool
	Expires  time.Time
}
