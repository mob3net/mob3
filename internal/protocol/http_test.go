package protocol

import "testing"

func TestHTTPStatusText(t *testing.T) {
	if got := HTTPStatusText(200); got != "OK" {
		t.Fatalf("200 = %q", got)
	}
	if got := HTTPStatusText(404); got != "Not Found" {
		t.Fatalf("404 = %q", got)
	}
	if got := HTTPStatusText(599); got != "" {
		t.Fatalf("599 = %q", got)
	}
	if MethodGet != "GET" || MethodPost != "POST" {
		t.Fatalf("methods = %q, %q", MethodGet, MethodPost)
	}
}
