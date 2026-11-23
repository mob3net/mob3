package host

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func redirectResponse(r *http.Request, location string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusFound,
		Header:     http.Header{"Location": []string{location}},
		Body:       http.NoBody,
		Request:    r,
	}
}

func publicRequestContext(t *testing.T, kind string) context.Context {
	t.Helper()
	page, err := url.Parse("https://public.example/")
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithRequestMeta(context.Background(), 1, kind)
	return WithRequestPage(ctx, page, false)
}

func TestIsLocalIPZeroNetwork(t *testing.T) {
	for _, raw := range []string{"0.0.0.0", "0.1.2.3", "0.255.255.255"} {
		if !IsLocalIP(net.ParseIP(raw)) {
			t.Fatalf("%s was not treated as local", raw)
		}
	}
	if IsLocalIP(net.ParseIP("1.1.1.1")) {
		t.Fatal("public address was treated as local")
	}
}

func TestRedirectToLocalLiteralBlocked(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return redirectResponse(r, "https://127.0.0.1/secret"), nil
	})}
	f := &Fetcher{
		Client: client, Settings: NewSettings("ua", ""),
		Requests: NewRequestLog(), Cache: NewResourceCache(0),
	}
	u, _ := url.Parse("https://public.example/start")
	_, _, _, _, err := f.FetchRequest(publicRequestContext(t, "img"), http.MethodGet, u, nil, "")
	if !errors.Is(err, ErrLocalNetworkBlocked) {
		t.Fatalf("error = %v, want ErrLocalNetworkBlocked", err)
	}
	if calls != 1 {
		t.Fatalf("transport calls = %d, want the redirect blocked before dialing", calls)
	}
}

func TestRedirectRetargetsRequestMeta(t *testing.T) {
	var seen string
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "public.example" {
			return redirectResponse(r, "https://cdn.example/asset"), nil
		}
		seen = RequestMetaFrom(r.Context()).TargetURL
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("ok")),
			Request:    r,
		}, nil
	})}
	f := &Fetcher{
		Client: client, Settings: NewSettings("ua", ""),
		Requests: NewRequestLog(), Cache: NewResourceCache(0),
	}
	u, _ := url.Parse("https://public.example/start")
	if _, _, _, _, err := f.FetchRequest(publicRequestContext(t, "img"), http.MethodGet, u, nil, ""); err != nil {
		t.Fatal(err)
	}
	if seen != "https://cdn.example/asset" {
		t.Fatalf("redirect request meta target = %q", seen)
	}
}
