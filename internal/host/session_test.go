package host

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"browser/internal/protocol"
)

func servedDoc(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("page"))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestAttestedURLCoversOverlappingLoads(t *testing.T) {
	s := NewSession()
	type doc struct {
		id  uint64
		url string
	}
	docs := []doc{{7, servedDoc(t)}, {8, servedDoc(t)}}
	for _, d := range docs {
		u, _ := url.Parse(d.url)
		ctx := protocol.WithRequestMeta(context.Background(), 1, "doc")
		ctx = protocol.WithRequestEntry(ctx, d.id)
		if _, err := s.Fetch(ctx, http.MethodGet, u, nil, ""); err != nil {
			t.Fatal(err)
		}
	}
	// The first load's attestation must survive the second completing before
	// the front end applied it.
	for _, d := range docs {
		got, ok := s.AttestedURL(d.id)
		if !ok || got != d.url {
			t.Fatalf("attestation for %d = %q, %v; want %q", d.id, got, ok, d.url)
		}
	}
}

func TestAttestedURLIsBounded(t *testing.T) {
	s := NewSession()
	for i := uint64(1); i <= attestedSlots+4; i++ {
		s.attest(i, fmt.Sprintf("https://e%d.example/", i))
	}
	if _, ok := s.AttestedURL(1); ok {
		t.Fatal("oldest attestation was not evicted")
	}
	for i := uint64(5); i <= attestedSlots+4; i++ {
		if _, ok := s.AttestedURL(i); !ok {
			t.Fatalf("recent attestation %d missing", i)
		}
	}
	if _, ok := s.AttestedURL(0); ok {
		t.Fatal("entry 0 reported an attestation")
	}
}
