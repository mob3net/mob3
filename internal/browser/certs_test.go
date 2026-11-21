package browser

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// writeTestRoot writes a self-signed CA certificate as a PEM bundle.
func writeTestRoot(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "MOB3 Test Root"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "roots.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSetCertSourceFileReplacesPlatformStore(t *testing.T) {
	path := writeTestRoot(t)
	l := NewLoader()
	if err := l.SetCertSource(path); err != nil {
		t.Fatalf("SetCertSource(%q): %v", path, err)
	}
	if l.certSource != path {
		t.Fatalf("certSource = %q, want the bundle path", l.certSource)
	}
	if got := len(l.certPool.Subjects()); got != 1 {
		t.Fatalf("pool holds %d roots, want only the named bundle's 1", got)
	}
	tr := l.transport.current()
	if tr == nil || tr.TLSClientConfig == nil || tr.TLSClientConfig.RootCAs != l.certPool {
		t.Fatal("transport was not rebuilt with the selected roots")
	}
	if clientTr, ok := l.client.Transport.(*certTransport); !ok || clientTr != l.transport {
		t.Fatal("client transport is not the stable wrapper")
	}

	// Switching back to the platform store clears the custom roots.
	if err := l.SetCertSource(CertSourceSystem); err != nil {
		t.Fatal(err)
	}
	if l.certSource != CertSourceSystem || l.certPool != nil {
		t.Fatalf("system source = %q pool=%v, want system and nil", l.certSource, l.certPool)
	}
	if tr := l.transport.current(); tr == nil || tr.TLSClientConfig != nil {
		t.Fatal("platform store should leave TLSClientConfig unset")
	}
}

// TestCertSourceTrustsOnlyItsBundle proves that the selected store is used and
// that it replaces the platform store: a local test server is trusted through
// its own certificate, then rejected after switching back to the platform.
func TestCertSourceTrustsOnlyItsBundle(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "server.pem")
	bundle := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(path, bundle, 0o600); err != nil {
		t.Fatal(err)
	}

	l := NewLoader()
	if err := l.SetCertSource(path); err != nil {
		t.Fatal(err)
	}
	resp, err := l.client.Get(srv.URL)
	if err != nil {
		t.Fatalf("request with the server's certificate as root failed: %v", err)
	}
	resp.Body.Close()

	if err := l.SetCertSource(CertSourceSystem); err != nil {
		t.Fatal(err)
	}
	if _, err := l.client.Get(srv.URL); err == nil {
		t.Fatal("the platform store unexpectedly trusted the test root")
	}
}

func TestSetCertSourceErrors(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.pem")
	if err := os.WriteFile(empty, []byte("not a certificate\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, source := range map[string]string{
		"missing":   filepath.Join(dir, "missing.pem"),
		"directory": dir,
		"empty pem": empty,
	} {
		l := NewLoader()
		err := l.SetCertSource(source)
		if err == nil {
			t.Fatalf("%s: SetCertSource(%q) succeeded", name, source)
		}
		if l.certSource != CertSourceSystem || l.certPool != nil {
			t.Fatalf("%s: failed load changed the trust store to %q", name, l.certSource)
		}
		if tr := l.transport.current(); tr == nil || tr.TLSClientConfig != nil {
			t.Fatalf("%s: failed load rebuilt the transport", name)
		}
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

	b.applyConfig(url.Values{"certs": {writeTestRoot(t)}})
	if b.loader.certSource != CertSourceEmbedded {
		t.Fatalf("failed change replaced the store with %q", b.loader.certSource)
	}
	if !strings.Contains(b.configNotice, "at startup") {
		t.Fatalf("path change set notice %q", b.configNotice)
	}
	page := b.configPage(mustParse(t, "about:config"), true)
	if !strings.Contains(page.RawHTML, "Certificate source not changed") {
		t.Fatal("config page does not show the rejected change")
	}
}
