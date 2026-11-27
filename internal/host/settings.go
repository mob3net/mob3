package host

import "sync"

// Settings is the host-owned runtime configuration shared by navigation,
// workers and the config window.
type Settings struct {
	mu        sync.RWMutex
	userAgent string
	searchURL string
	maxImageW int
	maxImageH int
}

func NewSettings(userAgent, searchURL string) *Settings {
	return &Settings{userAgent: userAgent, searchURL: searchURL}
}

func (s *Settings) SetUserAgent(v string) {
	s.mu.Lock()
	s.userAgent = v
	s.mu.Unlock()
}
func (s *Settings) UserAgent() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.userAgent
}
func (s *Settings) SetSearchURL(v string) {
	s.mu.Lock()
	s.searchURL = v
	s.mu.Unlock()
}
func (s *Settings) SearchURL() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.searchURL
}
func (s *Settings) SetImageCaps(w, h int) {
	s.mu.Lock()
	s.maxImageW, s.maxImageH = w, h
	s.mu.Unlock()
}
func (s *Settings) ImageCaps() (w, h int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.maxImageW, s.maxImageH
}
