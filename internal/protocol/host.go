package protocol

import (
	"context"
	"image"
	"net/url"
	"time"
)

// HostFetch is one raw host response for a page or subresource.
type HostFetch struct {
	Data        []byte
	ContentType string
	FinalURL    *url.URL
	Status      int
	PageLocal   bool
}

// RequestRecord is one row in the console request log.
type RequestRecord struct {
	ID      int
	Gen     int
	Method  string
	URL     string
	Kind    string
	Status  int
	Size    int
	Elapsed time.Duration
	Err     string
	Done    bool
	Cached  bool
}

// Host is the host-side port used by the engine loader: raw fetching, policy,
// settings, cache, cookies, certificates and request accounting. It is
// implemented by host.Session so the browser never imports internal/host.
type Host interface {
	Fetch(ctx context.Context, method string, u *url.URL, body []byte, contentType string) (HostFetch, error)
	FetchCtx(ctx context.Context, u *url.URL) (HostFetch, error)
	JSFetch(ctx context.Context, req FetchRequest) (FetchResponse, error)
	RecordBlocked(gen int, method string, u *url.URL, kind string, err error)
	ApplyMixedContent(ctx context.Context, u *url.URL) (*url.URL, error)
	IsLocalURL(u *url.URL) bool
	IsMixedContentError(error) bool
	IsLocalNetworkError(error) bool

	SetUserAgent(string)
	UserAgent() string
	SetSearchURL(string)
	SearchURL() string
	SetImageCaps(w, h int)
	ImageCaps() (int, int)

	BeginPage() int
	CurrentPageGeneration() int
	StartRequest(gen int, method, rawURL, kind string) (id int, ok bool)
	RecordCacheHit(gen int, method, rawURL, kind string, size int)
	FinishRequest(id, gen, status, size int, elapsed time.Duration, err error)
	RequestSnapshot() []RequestRecord
	RequestCount() int

	CacheStats() (used, limit int64, entries int)
	CacheLimitBytes() int64
	SetCacheLimitBytes(int64)

	DocumentCookieValues(u *url.URL) []CookieView
	SetDocumentCookieRaw(u *url.URL, raw string) bool
	CookieViews(u *url.URL) []CookieView

	SetCertSource(source string) error
	CertSource() string
}

// ImageCache is the optional host-side decoded-image cache. It is separate
// from Host so a process transport can omit it or implement a child-local
// cache without serializing image.Image values.
type ImageCache interface {
	GetCachedImage(key string) (image.Image, int, bool, bool)
	PutCachedImage(key string, img image.Image, rawSize int, local bool)
}
