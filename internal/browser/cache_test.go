package browser

import (
	"image"
	"testing"
)

func TestResourceCacheLRUEviction(t *testing.T) {
	c := newResourceCache(8)
	c.PutBytes("a", make([]byte, 4), "text/css", "https://example.com/a.css", false)
	c.PutBytes("b", make([]byte, 4), "text/css", "https://example.com/b.css", false)
	if _, _, _, _, ok := c.GetBytes("a"); !ok {
		t.Fatal("a was not cached")
	}
	c.PutBytes("c", make([]byte, 4), "text/css", "https://example.com/c.css", false)
	if _, _, _, _, ok := c.GetBytes("a"); !ok {
		t.Fatal("recently used entry a was evicted")
	}
	if _, _, _, _, ok := c.GetBytes("b"); ok {
		t.Fatal("least recently used entry b survived")
	}
	if _, _, _, _, ok := c.GetBytes("c"); !ok {
		t.Fatal("new entry c was not cached")
	}
}

func TestResourceCacheLimitAndEntryCap(t *testing.T) {
	c := newResourceCache(8)
	c.PutBytes("too-big", make([]byte, 9), "text/css", "https://example.com/big.css", false)
	if _, _, _, _, ok := c.GetBytes("too-big"); ok {
		t.Fatal("oversized entry was cached")
	}
	c.PutBytes("fit", make([]byte, 8), "text/css", "https://example.com/fit.css", false)
	if _, _, _, _, ok := c.GetBytes("fit"); !ok {
		t.Fatal("entry at the limit was not cached")
	}
	c.SetLimit(0)
	if _, _, _, _, ok := c.GetBytes("fit"); ok {
		t.Fatal("entries survived disabling the cache")
	}
	if c.LimitBytes() != 0 {
		t.Fatalf("limit = %d, want 0", c.LimitBytes())
	}
}

func TestResourceCacheImages(t *testing.T) {
	c := newResourceCache(1 << 20)
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	c.PutImage("https://example.com/a.png", img, 123, false)
	got, rawSize, local, ok := c.GetImage("https://example.com/a.png")
	if !ok || got != img || local {
		t.Fatal("image was not cached as public")
	}
	if rawSize != 123 {
		t.Fatalf("raw image size = %d", rawSize)
	}
	if _, _, _, _, ok := c.GetBytes("https://example.com/a.png"); ok {
		t.Fatal("image entry was returned as raw bytes")
	}
}

func TestResourceCacheMarksLocalEntries(t *testing.T) {
	c := newResourceCache(1 << 20)
	c.PutBytes("local", make([]byte, 4), "text/css", "https://intranet.example/a.css", true)
	if _, _, _, local, ok := c.GetBytes("local"); !ok || !local {
		t.Fatal("local byte entry was not marked local")
	}
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	c.PutImage("local.png", img, 10, true)
	if _, _, local, ok := c.GetImage("local.png"); !ok || !local {
		t.Fatal("local image entry was not marked local")
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
