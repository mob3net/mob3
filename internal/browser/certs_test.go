package browser

import (
	"crypto/x509"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/x509roots/fallback/bundle"
)

// TestEmbeddedCABundle checks that the official bundle parses into a usable
// trust store. It is the guard for a module bump that ships broken data.
func TestEmbeddedCABundle(t *testing.T) {
	pool, err := embeddedCACertPool()
	if err != nil {
		t.Fatalf("embedded bundle does not parse: %v", err)
	}
	if pool == nil {
		t.Fatal("embedded pool is nil")
	}
	count, subjects := 0, map[string]bool{}
	for root := range bundle.Roots() {
		cert, err := x509.ParseCertificate(root.Certificate)
		if err != nil {
			t.Fatalf("embedded bundle holds an unparsable certificate: %v", err)
		}
		if !cert.IsCA {
			t.Errorf("embedded bundle holds a non-CA certificate: %s", cert.Subject)
		}
		if subjects[cert.Subject.String()] {
			t.Errorf("embedded bundle repeats the subject %s", cert.Subject)
		}
		subjects[cert.Subject.String()] = true
		count++
	}
	if count < 100 {
		t.Fatalf("embedded bundle holds only %d certificates; bump x509roots/fallback", count)
	}
	if got := len(pool.Subjects()); got != count {
		t.Fatalf("pool holds %d roots, bundle holds %d", got, count)
	}
}

func TestResolveCertSource(t *testing.T) {
	for _, tc := range []struct {
		in    string
		label string
		pool  bool
	}{
		{"", CertSourceSystem, false},
		{"system", CertSourceSystem, false},
		{" SYSTEM ", CertSourceSystem, false},
		{"embedded", CertSourceEmbedded, true},
		{"Embedded", CertSourceEmbedded, true},
	} {
		label, pool, err := resolveCertSource(tc.in)
		if err != nil {
			t.Fatalf("resolveCertSource(%q): %v", tc.in, err)
		}
		if label != tc.label || (pool != nil) != tc.pool {
			t.Errorf("resolveCertSource(%q) = %q, pool=%v; want %q, pool=%v",
				tc.in, label, pool != nil, tc.label, tc.pool)
		}
	}
}

// TestSetCertSourceRejectsFiles proves that a certificate path is not read and
// leaves the active trust store untouched.
func TestSetCertSourceRejectsFiles(t *testing.T) {
	l := NewLoader()
	path := filepath.Join(t.TempDir(), "roots.pem")
	if err := os.WriteFile(path, []byte("not a certificate\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := l.SetCertSource(path)
	if err == nil {
		t.Fatalf("SetCertSource(%q) succeeded", path)
	}
	if !strings.Contains(err.Error(), "system or embedded") {
		t.Fatalf("error = %q", err)
	}
	if l.certSource != CertSourceSystem || l.certPool != nil {
		t.Fatalf("failed load changed the trust store to %q", l.certSource)
	}
	if tr := testHost(l).Client().Transport(); tr == nil || tr.TLSClientConfig != nil {
		t.Fatal("failed load rebuilt the transport")
	}
}

func TestConfigSwitchesCertSource(t *testing.T) {
	b := NewBrowser(80, 10)
	b.setLoader(NewLoader())
	b.applyConfig(url.Values{"certs": {CertSourceEmbedded}})
	if b.loader.certSource != CertSourceEmbedded || b.loader.certPool == nil {
		t.Fatalf("cert source = %q pool=%v, want embedded", b.loader.certSource, b.loader.certPool)
	}
	if b.certSourceLabel() != CertSourceEmbedded {
		t.Fatalf("config label = %q, want embedded", b.certSourceLabel())
	}
	if b.configNotice != "" {
		t.Fatalf("unexpected notice %q", b.configNotice)
	}

	b.applyConfig(url.Values{"certs": {"roots.pem"}})
	if b.loader.certSource != CertSourceEmbedded {
		t.Fatalf("failed change replaced the store with %q", b.loader.certSource)
	}
	if !strings.Contains(b.configNotice, "system or embedded") {
		t.Fatalf("path change set notice %q", b.configNotice)
	}
	page := b.configPage(mustParse(t, "about:config"), true)
	if !strings.Contains(page.RawHTML, "Certificate source not changed") {
		t.Fatal("config page does not show the rejected change")
	}
}
