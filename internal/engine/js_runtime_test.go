package engine

import (
	"net/url"
	"testing"
	"time"
)

func mustURL(t *testing.T, raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestImportMapPrefixMapping(t *testing.T) {
	base := mustURL(t, "https://example.com/app/")
	j := &jsRuntime{page: &Page{URL: base, Base: base}}
	m := &jsModuleLoader{j: j, base: base, imports: map[string]string{
		"lib/": "https://cdn.example/lib/",
	}}
	got, err := m.resolveSpecifier(base, "lib/util.js")
	if err != nil || got.String() != "https://cdn.example/lib/util.js" {
		t.Fatalf("prefix mapping = %v, %v", got, err)
	}
	if _, err := m.resolveSpecifier(base, "lib"); err == nil {
		t.Fatal("bare specifier without map entry was resolved")
	}
}

func TestModuleSpecifierResolution(t *testing.T) {
	base := mustURL(t, "https://example.com/app/index.html")
	j := &jsRuntime{page: &Page{URL: base, Base: base}}
	m := &jsModuleLoader{j: j, base: base}

	got, err := m.resolveSpecifier(base, "./dep.js")
	if err != nil || got.String() != "https://example.com/app/dep.js" {
		t.Fatalf("relative specifier = %v, %v", got, err)
	}
	got, err = m.resolveSpecifier(base, "/lib/x.js")
	if err != nil || got.String() != "https://example.com/lib/x.js" {
		t.Fatalf("root specifier = %v, %v", got, err)
	}
	if _, err := m.resolveSpecifier(base, "react"); err == nil {
		t.Fatal("bare specifier was resolved")
	}
	if _, err := m.resolveSpecifier(base, "mailto:x@example.com"); err == nil {
		t.Fatal("unsupported scheme was resolved")
	}
}

func TestJSRuntimeFreezeShiftsTimers(t *testing.T) {
	base := time.Now()
	j := &jsRuntime{timers: map[int]*jsTimer{
		1: {id: 1, due: base.Add(100 * time.Millisecond)},
	}}
	j.Freeze()
	if j.frozenAt.IsZero() {
		t.Fatal("freeze did not mark the runtime")
	}
	j.frozenAt = base.Add(-2 * time.Second)
	j.Resume()
	if !j.frozenAt.IsZero() {
		t.Fatal("resume left the runtime frozen")
	}
	want := base.Add(2100 * time.Millisecond)
	if got := j.timers[1].due; got.Sub(want) > 10*time.Millisecond || want.Sub(got) > 10*time.Millisecond {
		t.Fatalf("timer due = %v, want about %v", got, want)
	}
}
