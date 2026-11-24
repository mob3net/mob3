package engine

import (
	"errors"
	"net/url"
	"testing"

	"browser/internal/protocol"
)

func storageOrigin(t *testing.T, raw string) protocol.Origin {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return protocol.OriginOf(u)
}

func TestStorageLocalIsSharedAcrossTabs(t *testing.T) {
	ctx := NewBrowserContext()
	a := NewSessionWithBrowserContext(80, 24, ctx)
	b := NewSessionWithBrowserContext(80, 24, ctx)
	a.URL = routerURL(t, "https://example.com/a")
	b.URL = routerURL(t, "https://example.com/b")

	av := a.storageView(StorageLocal, a.Frame)
	bv := b.storageView(StorageLocal, b.Frame)
	if err := av.Set("theme", "dark"); err != nil {
		t.Fatalf("set local: %v", err)
	}
	if got, ok := bv.Get("theme"); !ok || got != "dark" {
		t.Fatalf("localStorage not shared: %q %v", got, ok)
	}

	as := a.storageView(StorageSession, a.Frame)
	bs := b.storageView(StorageSession, b.Frame)
	if err := as.Set("tab", "a"); err != nil {
		t.Fatalf("set session: %v", err)
	}
	if _, ok := bs.Get("tab"); ok {
		t.Fatal("sessionStorage leaked into another tab")
	}

	c := NewSessionWithBrowserContext(80, 24, ctx)
	c.URL = routerURL(t, "https://other.example/")
	cv := c.storageView(StorageLocal, c.Frame)
	if _, ok := cv.Get("theme"); ok {
		t.Fatal("localStorage leaked across origins")
	}
}

func TestOpaqueStorageIsPrivatePerView(t *testing.T) {
	b := NewSession(80, 24)
	b.URL = routerURL(t, "about:home")
	first := b.storageView(StorageLocal, b.Frame)
	second := b.storageView(StorageLocal, b.Frame)
	if err := first.Set("k", "v"); err != nil {
		t.Fatalf("set opaque storage: %v", err)
	}
	if _, ok := second.Get("k"); ok {
		t.Fatal("opaque storage was shared between views")
	}
}

func TestStorageQuota(t *testing.T) {
	br := newStorageBroker(6, 10)
	one := br.View(StorageLocal, 1, storageOrigin(t, "https://one.example/"))
	two := br.View(StorageLocal, 2, storageOrigin(t, "https://two.example/"))
	three := br.View(StorageLocal, 3, storageOrigin(t, "https://three.example/"))

	if err := one.Set("a", "12345"); err != nil { // 6 bytes
		t.Fatalf("first write: %v", err)
	}
	if err := one.Set("b", "x"); !errors.Is(err, ErrStorageQuota) {
		t.Fatalf("per-origin overflow = %v", err)
	}
	if err := one.Set("a", "1"); err != nil { // shrink to 2 bytes
		t.Fatalf("shrink write: %v", err)
	}
	if err := two.Set("a", "1234"); err != nil { // total 7
		t.Fatalf("second origin: %v", err)
	}
	if err := three.Set("a", "1234"); !errors.Is(err, ErrStorageQuota) {
		t.Fatalf("total overflow = %v", err)
	}
}

func TestStorageSnapshotAndKeysAreCopies(t *testing.T) {
	br := NewStorageBroker()
	v := br.View(StorageSession, 1, storageOrigin(t, "https://example.com/"))
	if err := v.Set("b", "2"); err != nil {
		t.Fatal(err)
	}
	if err := v.Set("a", "1"); err != nil {
		t.Fatal(err)
	}
	keys := v.Keys()
	if len(keys) != 2 || keys[0] != "a" || keys[1] != "b" {
		t.Fatalf("keys = %v", keys)
	}
	snap := v.Snapshot()
	snap["a"] = "changed"
	snap["c"] = "new"
	if got, _ := v.Get("a"); got != "1" {
		t.Fatalf("snapshot mutated the area: %q", got)
	}
	if _, ok := v.Get("c"); ok {
		t.Fatal("snapshot added a key")
	}
}

func TestJSRuntimeStorageSurvivesRuntimeRecreation(t *testing.T) {
	b := NewSession(80, 24)
	b.URL = routerURL(t, "https://example.com/")
	page := &Page{URL: b.URL, Root: ParseHTML(`<html><body></body></html>`)}
	b.startJS(page)
	if _, err := b.JS.RunCode("seed", `localStorage.setItem("k", "v"); sessionStorage.setItem("s", "1");`); err != nil {
		t.Fatalf("seed storage: %v", err)
	}
	b.StopJS()
	b.startJS(page)
	v, err := b.JS.RunCode("read", `localStorage.getItem("k") + ":" + sessionStorage.getItem("s")`)
	if err != nil {
		t.Fatalf("read storage: %v", err)
	}
	if got := v.String(); got != "v:1" {
		t.Fatalf("storage after runtime recreation = %q", got)
	}
}

func TestJSRuntimeOpaqueStorageDoesNotPersist(t *testing.T) {
	b := NewSession(80, 24)
	b.URL = routerURL(t, "about:home")
	page := &Page{URL: b.URL, Root: ParseHTML(`<html><body></body></html>`)}
	b.startJS(page)
	if _, err := b.JS.RunCode("seed", `localStorage.setItem("k", "v");`); err != nil {
		t.Fatalf("seed opaque storage: %v", err)
	}
	b.StopJS()
	b.startJS(page)
	v, err := b.JS.RunCode("read", `localStorage.getItem("k")`)
	if err != nil {
		t.Fatalf("read opaque storage: %v", err)
	}
	if got := v.String(); got != "null" {
		t.Fatalf("opaque storage persisted: %q", got)
	}
}
