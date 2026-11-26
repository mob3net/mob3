package protocol

import (
	"errors"
	"net/url"
	"reflect"
	"testing"
	"time"
)

func testHostCall() HostCall {
	u, _ := url.Parse("https://example.test/page")
	return HostCall{
		Op: HostOpFetch, ID: 7,
		Meta: RequestMeta{
			Gen: 3, Kind: "doc", EntryID: 42, PageURL: "https://example.test/",
			TargetURL: "https://example.test/page", BlockLocal: true,
			PageSecure: true, Origin: "https://example.test",
		},
		URL: u.String(), Method: MethodGet, Body: []byte("body"),
		ContentType: "text/plain",
		JS: FetchRequest{
			URL: "https://example.test/api", Method: MethodPost,
			Headers: []Header{{Name: "Accept", Value: "text/plain"}},
			Body:    []byte("js"), Purpose: FetchFetch, Origin: "https://example.test",
			Credentials: true, NoCORS: true, Kind: "fetch", PageLocal: true,
		},
		Gen: 3, Kind: "doc", RequestID: 9, Status: 200, Size: 12,
		Elapsed: 2 * time.Second, ErrKind: HostErrMixedContent,
		ErrText: "blocked", Limit: 1 << 20, UserAgent: "ua", SearchURL: "https://search/",
		Width: 800, Height: 600, CookieRaw: "a=b", Source: "embedded", Token: "tok",
	}
}

func testHostResult() HostResult {
	final, _ := url.Parse("https://example.test/final")
	return HostResult{
		ID: 7,
		Fetch: HostFetch{
			Data: []byte("data"), ContentType: "text/html", FinalURL: final,
			Status: 200, PageLocal: true,
		},
		JS: FetchResponse{
			Status: 201, Headers: []Header{{Name: "X-Test", Value: "1"}},
			Body: []byte("js"), FinalURL: "https://example.test/final", ContentType: "text/plain",
		},
		URL: "https://example.test/resolved", Bool: true, Int: 12, Width: 800, Height: 600,
		Used: 10, Limit: 20, Entries: 2,
		Records: []RequestRecord{{
			ID: 1, Gen: 3, Method: MethodGet, URL: "https://example.test/", Kind: "doc",
			Status: 200, Size: 12, Elapsed: time.Second, Err: "e", Done: true, Cached: true,
		}},
		Cookies: []CookieView{{
			Name: "a", Value: "b", Domain: "example.test", Path: "/", SameSite: "Lax",
			HttpOnly: true, Secure: true, Expires: time.Date(2027, 1, 2, 3, 4, 5, 6, time.UTC),
		}},
		Text: "token", ErrKind: HostErrOther, ErrText: "failed",
	}
}

func TestHostCallRoundTrip(t *testing.T) {
	in := testHostCall()
	body, err := AppendHostCall(nil, in, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	out, err := DecodeHostCall(body, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("host call:\n in: %#v\nout: %#v", in, out)
	}
}

func TestHostResultRoundTrip(t *testing.T) {
	in := testHostResult()
	body, err := AppendHostResult(nil, in, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	out, err := DecodeHostResult(body, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("host result:\n in: %#v\nout: %#v", in, out)
	}
}

func TestHostErrorIdentity(t *testing.T) {
	for _, err := range []error{
		ErrLocalNetworkBlocked, ErrMixedContentBlocked, ErrResourceTooLarge, ErrInvalid, ErrLimit,
		errors.New("other"),
	} {
		kind := hostErrorKind(err)
		got := hostError(kind, err.Error())
		if !errors.Is(got, err) && got.Error() != err.Error() {
			t.Fatalf("%v mapped to %v (kind %d)", err, got, kind)
		}
	}
}

func FuzzDecodeHostCall(f *testing.F) {
	body, _ := AppendHostCall(nil, testHostCall(), DefaultLimits)
	f.Add(body)
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = DecodeHostCall(data, DefaultLimits)
	})
}

func FuzzDecodeHostResult(f *testing.F) {
	body, _ := AppendHostResult(nil, testHostResult(), DefaultLimits)
	f.Add(body)
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = DecodeHostResult(data, DefaultLimits)
	})
}
