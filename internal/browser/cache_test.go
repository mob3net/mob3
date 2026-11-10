package browser

import (
	"image"
	"testing"
)

func TestResourceCacheLRUEviction(t *testing.T) {
	c := newResourceCache(8)
	c.putBytes("a", make([]byte, 4), "text/css", "https://example.com/a.css")
	c.putBytes("b", make([]byte, 4), "text/css", "https://example.com/b.css")
	if _, _, _, ok := c.getBytes("a"); !ok {
		t.Fatal("a was not cached")
	}
	c.putBytes("c", make([]byte, 4), "text/css", "https://example.com/c.css")
	if _, _, _, ok := c.getBytes("a"); !ok {
		t.Fatal("recently used entry a was evicted")
	}
	if _, _, _, ok := c.getBytes("b"); ok {
		t.Fatal("least recently used entry b survived")
	}
	if _, _, _, ok := c.getBytes("c"); !ok {
		t.Fatal("new entry c was not cached")
	}
}

func TestResourceCacheLimitAndEntryCap(t *testing.T) {
	c := newResourceCache(8)
	c.putBytes("too-big", make([]byte, 9), "text/css", "https://example.com/big.css")
	if _, _, _, ok := c.getBytes("too-big"); ok {
		t.Fatal("oversized entry was cached")
	}
	c.putBytes("fit", make([]byte, 8), "text/css", "https://example.com/fit.css")
	if _, _, _, ok := c.getBytes("fit"); !ok {
		t.Fatal("entry at the limit was not cached")
	}
	c.setLimit(0)
	if _, _, _, ok := c.getBytes("fit"); ok {
		t.Fatal("entries survived disabling the cache")
	}
	if c.limitBytes() != 0 {
		t.Fatalf("limit = %d, want 0", c.limitBytes())
	}
}

func TestResourceCacheImages(t *testing.T) {
	c := newResourceCache(1 << 20)
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	c.putImage("https://example.com/a.png", img, 123)
	got, rawSize, ok := c.getImage("https://example.com/a.png")
	if !ok || got != img {
		t.Fatal("image was not cached")
	}
	if rawSize != 123 {
		t.Fatalf("raw image size = %d", rawSize)
	}
	if _, _, _, ok := c.getBytes("https://example.com/a.png"); ok {
		t.Fatal("image entry was returned as raw bytes")
	}
}

func TestCacheControlAllows(t *testing.T) {
	if !cacheControlAllows("max-age=60") {
		t.Fatal("cacheable response was rejected")
	}
	if cacheControlAllows("public, no-store, max-age=0") {
		t.Fatal("no-store response was accepted")
	}
}
