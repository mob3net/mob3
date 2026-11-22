package host

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
)

// ConfigToken mints and validates the one-shot form token for about:config.
// It lives on the trusted side so a page cannot replay a settings form.
type ConfigToken struct {
	mu    sync.Mutex
	token string
}

// NewConfigToken creates an empty token holder.
func NewConfigToken() *ConfigToken { return &ConfigToken{} }

// Current returns the active token, minting one when none is pending. It
// returns an empty string if the system random source fails, which fails
// closed: the form then cannot be submitted.
func (t *ConfigToken) Current() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.token == "" {
		var buf [16]byte
		if _, err := rand.Read(buf[:]); err != nil {
			return ""
		}
		t.token = hex.EncodeToString(buf[:])
	}
	return t.token
}

// Consume accepts got only if it matches the active token and clears it so a
// replay from history cannot apply the same settings again.
func (t *ConfigToken) Consume(got string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if got == "" || t.token == "" || got != t.token {
		return false
	}
	t.token = ""
	return true
}
