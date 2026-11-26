package host

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

func TestResourceCacheBytesAndImages(t *testing.T) {
	c := NewResourceCache(1024)
	c.PutBytes("css", []byte("body{}"), "text/css", "https://example.test/a.css", true)
	data, ctype, final, local, ok := c.GetBytes("css")
	if !ok || string(data) != "body{}" || ctype != "text/css" || final != "https://example.test/a.css" || !local {
		t.Fatalf("bytes = %q %q %q %v %v", data, ctype, final, local, ok)
	}
	if _, _, _, ok := c.GetImage("css"); ok {
		t.Fatal("byte entry was returned as an image")
	}

	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(0, 0, color.RGBA{R: 1, A: 255})
	c.PutImage("img", img, 99, false)
	got, raw, local, ok := c.GetImage("img")
	if !ok || got == nil || raw != 99 || local {
		t.Fatalf("image = %v raw=%d local=%v ok=%v", got, raw, local, ok)
	}
	if _, _, _, _, ok := c.GetBytes("img"); ok {
		t.Fatal("image entry was returned as bytes")
	}
	used, limit, entries := c.Stats()
	if limit != 1024 || entries != 2 || used <= 0 {
		t.Fatalf("stats = used %d limit %d entries %d", used, limit, entries)
	}
}

func TestResourceCacheCountsKeys(t *testing.T) {
	c := NewResourceCache(1 << 20)
	key := strings.Repeat("k", 1000)
	c.PutBytes(key, []byte("x"), "text/css", "", false)
	used, _, _ := c.Stats()
	if used < int64(len(key)+1) {
		t.Fatalf("cache used %d bytes, want key accounted", used)
	}
}

func TestResourceCacheEvictsLRUAndRejectsOversized(t *testing.T) {
	c := NewResourceCache(230)
	c.PutBytes("a", make([]byte, 100), "text/plain", "a", false)
	c.PutBytes("b", make([]byte, 100), "text/plain", "b", false)
	c.PutBytes("c", make([]byte, 100), "text/plain", "c", false)
	if _, _, _, _, ok := c.GetBytes("a"); ok {
		t.Fatal("least recently used entry was not evicted")
	}
	if _, _, _, _, ok := c.GetBytes("b"); !ok {
		t.Fatal("recent entry was evicted")
	}
	if _, _, _, _, ok := c.GetBytes("c"); !ok {
		t.Fatal("newest entry is missing")
	}

	c.PutBytes("huge", make([]byte, 501), "text/plain", "huge", false)
	if _, _, _, _, ok := c.GetBytes("huge"); ok {
		t.Fatal("oversized entry was stored")
	}

	c.SetLimit(0)
	used, limit, entries := c.Stats()
	if used != 0 || limit != 0 || entries != 0 {
		t.Fatalf("after SetLimit(0): %d %d %d", used, limit, entries)
	}
}

func TestResourceCacheIgnoresInvalidInput(t *testing.T) {
	var nilCache *ResourceCache
	if nilCache.LimitBytes() != 0 {
		t.Fatal("nil cache limit")
	}
	if used, limit, entries := nilCache.Stats(); used != 0 || limit != 0 || entries != 0 {
		t.Fatal("nil cache stats")
	}
	if _, _, _, _, ok := nilCache.GetBytes("x"); ok {
		t.Fatal("nil cache returned bytes")
	}

	c := NewResourceCache(100)
	c.PutBytes("empty", nil, "", "", false)
	c.PutImage("nil", nil, 0, false)
	if _, _, entries := c.Stats(); entries != 0 {
		t.Fatalf("invalid entries stored: %d", entries)
	}
	c.SetLimit(-1)
	if c.LimitBytes() != 0 {
		t.Fatalf("negative limit = %d", c.LimitBytes())
	}
}

func TestCacheVaryAllows(t *testing.T) {
	for _, header := range []string{"", "Accept-Encoding", "Accept-Encoding, Cookie", "*", "Cookie"} {
		want := header != "Accept-Encoding, Cookie" && header != "*" && header != "Cookie"
		if got := CacheVaryAllows(header); got != want {
			t.Errorf("CacheVaryAllows(%q) = %v, want %v", header, got, want)
		}
	}
}

func TestCacheControlAllows(t *testing.T) {
	for _, tc := range []struct {
		header string
		want   bool
	}{
		{"", true},
		{"public, max-age=60", true},
		{"no-store", false},
		{"public, NO-STORE, max-age=1", false},
		{"no-cache", true},
	} {
		if got := CacheControlAllows(tc.header); got != tc.want {
			t.Errorf("CacheControlAllows(%q) = %v, want %v", tc.header, got, tc.want)
		}
	}
}
