package host

import (
	"net/http"
	"strings"
	"testing"
)

func TestSettingsRoundTrip(t *testing.T) {
	s := NewSettings("ua-one", "https://search.test/?q=%s")
	s.SetUserAgent("ua-two")
	s.SetSearchURL("")
	s.SetImageCaps(800, 600)
	if s.UserAgent() != "ua-two" || s.SearchURL() != "" {
		t.Fatalf("strings = %q %q", s.UserAgent(), s.SearchURL())
	}
	if w, h := s.ImageCaps(); w != 800 || h != 600 {
		t.Fatalf("caps = %dx%d", w, h)
	}
}

func TestResolveCertSource(t *testing.T) {
	for _, source := range []string{"", "system", "SYSTEM", " System "} {
		label, pool, err := ResolveCertSource(source)
		if err != nil || label != CertSourceSystem || pool != nil {
			t.Fatalf("%q: label=%q pool=%v err=%v", source, label, pool, err)
		}
	}
	label, pool, err := ResolveCertSource("embedded")
	if err != nil || label != CertSourceEmbedded || pool == nil {
		t.Fatalf("embedded: label=%q pool=%v err=%v", label, pool, err)
	}
	if _, _, err := ResolveCertSource("file"); err == nil {
		t.Fatal("invalid source was accepted")
	}
}

func TestClientCertSourceAndNilFallbacks(t *testing.T) {
	var nilClient *Client
	if nilClient.HTTP() != http.DefaultClient || nilClient.CertSource() != CertSourceSystem || nilClient.CertPool() != nil || nilClient.Transport() != nil {
		t.Fatal("nil client fallbacks are wrong")
	}
	c := NewClientFromHTTP(nil)
	if c.HTTP() == nil {
		t.Fatal("nil http client was not replaced")
	}
	if err := c.SetCertSource("embedded"); err != nil {
		t.Fatal(err)
	}
	if c.CertSource() != CertSourceEmbedded || c.CertPool() == nil {
		t.Fatal("embedded source was not installed")
	}
	if err := c.SetCertSource("system"); err != nil || c.CertPool() != nil || c.CertSource() != CertSourceSystem {
		t.Fatalf("system source = %q %v %v", c.CertSource(), c.CertPool(), err)
	}
	if err := c.SetCertSource("nope"); err == nil {
		t.Fatal("invalid source was accepted")
	}
}

func TestCertTransportSwapAndRoundTrip(t *testing.T) {
	tr := NewCertTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 204, Body: http.NoBody, Request: r, Header: make(http.Header)}, nil
	}))
	req, _ := http.NewRequest(http.MethodGet, "https://example.test/", nil)
	resp, err := tr.RoundTrip(req)
	if err != nil || resp.StatusCode != 204 {
		t.Fatalf("round trip = %v %v", resp, err)
	}
	next := NewHTTPTransport(nil)
	tr.Swap(next)
	if tr.Current() != next {
		t.Fatal("swap did not install the transport")
	}
}

func TestNewHTTPClientRedirectLimit(t *testing.T) {
	c := NewHTTPClient(nil, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: http.NoBody, Request: r, Header: make(http.Header)}, nil
	}))
	req, _ := http.NewRequest(http.MethodGet, "https://example.test/", nil)
	if err := c.CheckRedirect(req, make([]*http.Request, 9)); err != nil {
		t.Fatalf("nine redirects rejected: %v", err)
	}
	if err := c.CheckRedirect(req, make([]*http.Request, 10)); err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("ten redirects = %v", err)
	}
}
