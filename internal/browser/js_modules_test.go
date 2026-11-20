package browser

import (
	"context"
	"strings"
	"testing"
	"time"
)

func moduleTestPage(html, code string) *Page {
	page := newJSPage(html, "")
	page.Scripts = []Script{{Module: true, Code: code}}
	return page
}

func TestInlineModuleImportsAndRuns(t *testing.T) {
	page := moduleTestPage(`<html><body><p id="out">before</p></body></html>`,
		`import { msg } from "data:text/javascript,export const msg = 'from module';";`+
			`document.getElementById("out").textContent = msg;`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	if b.js == nil {
		t.Fatal("runtime was not created")
	}
	if got := page.Root.FindFirst("p").TextContent(); got != "from module" {
		t.Fatalf("module import did not run: %q", got)
	}
	if b.moduleErr {
		t.Fatalf("module error recorded: %s", strings.Join(b.jsLog, "; "))
	}
}

func TestInlineModuleImportMetaURL(t *testing.T) {
	page := moduleTestPage(`<html><body><p id="out"></p></body></html>`,
		`document.getElementById("out").textContent = import.meta.url;`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	if got := page.Root.FindFirst("p").TextContent(); got != "https://example.com/" {
		t.Fatalf("import.meta.url = %q", got)
	}
}

func TestExternalModuleUsesCollectedBody(t *testing.T) {
	// The page loader already fetched the module script, so the runtime must
	// parse that body rather than fetching the URL again, and import.meta.url
	// must be the module's own URL.
	page := newJSPage(`<html><body><p id="out"></p></body></html>`, "")
	page.Scripts = []Script{{
		Module: true,
		Src:    "https://example.com/app/main.js",
		Code:   `document.getElementById("out").textContent = import.meta.url;`,
	}}
	b := NewBrowser(40, 10)
	b.startJS(page)
	if got := page.Root.FindFirst("p").TextContent(); got != "https://example.com/app/main.js" {
		t.Fatalf("external module = %q", got)
	}
}

func TestDynamicImportResolvesNamespace(t *testing.T) {
	page := moduleTestPage(`<html><body><p id="out"></p></body></html>`,
		`import("data:text/javascript,export const n = 7;").then(function (m) {`+
			`document.getElementById("out").textContent = String(m.n);});`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	if got := page.Root.FindFirst("p").TextContent(); got != "7" {
		t.Fatalf("dynamic import result = %q, want 7 (log: %s)", got, strings.Join(b.jsLog, "; "))
	}
}

func TestModuleGraphRunsOnceForSharedDependency(t *testing.T) {
	// The second module re-exports a binding from the same dependency URL, so
	// the graph must reuse the parsed record instead of evaluating it twice.
	counter := "data:text/javascript,globalThis.hits = (globalThis.hits || 0) + 1;export const n = 1;"
	page := newJSPage(`<html><body><p id="out"></p></body></html>`, "")
	page.Scripts = []Script{
		{Module: true, Code: `import { n } from "` + counter + `";` +
			`document.getElementById("out").textContent = "a" + n;`},
		{Module: true, Code: `import { n as m } from "` + counter + `";` +
			`document.getElementById("out").textContent += ":" + String(globalThis.hits) + ":" + String(m);`},
	}
	b := NewBrowser(40, 10)
	b.startJS(page)
	if got := page.Root.FindFirst("p").TextContent(); got != "a1:1:1" {
		t.Fatalf("shared dependency = %q, want a1:1:1 (log: %s)", got, strings.Join(b.jsLog, "; "))
	}
}

func TestAsyncImportsShareModuleRecords(t *testing.T) {
	// Two dynamic imports of the same URL must evaluate the module once; the
	// background loader shares the page's module cache instead of parsing a
	// fresh graph per import.
	dep := "data:text/javascript,globalThis.hits = (globalThis.hits || 0) + 1;export const n = 1;"
	page := newJSPage(`<html><body><p id="out"></p></body></html>`, "")
	b := NewBrowser(40, 10)
	b.interactive = true
	b.startJS(page)
	for i := 0; i < 2; i++ {
		if _, err := b.js.runCode("test",
			`import("`+dep+`").then(function (m) {`+
				`document.getElementById("out").textContent = String(globalThis.hits) + ":" + String(m.n);});`); err != nil {
			t.Fatal(err)
		}
		select {
		case task := <-b.importCh:
			b.finishJSImport(task)
		case <-time.After(5 * time.Second):
			t.Fatal("background import did not complete")
		}
	}
	if got := page.Root.FindFirst("p").TextContent(); got != "1:1" {
		t.Fatalf("shared module evaluated %q, want 1:1", got)
	}
}

func TestAsyncDynamicImportCompletes(t *testing.T) {
	// A dynamic import after the page settled loads in the background; the
	// event loop completes it and the promise continuation runs.
	page := newJSPage(`<html><body><p id="out"></p></body></html>`, "")
	b := NewBrowser(40, 10)
	b.interactive = true
	b.startJS(page)
	if _, err := b.js.runCode("test",
		`import("data:text/javascript,export const v = 9;").then(function (m) {`+
			`document.getElementById("out").textContent = String(m.v);});`); err != nil {
		t.Fatal(err)
	}
	select {
	case task := <-b.importCh:
		b.finishJSImport(task)
	case <-time.After(5 * time.Second):
		t.Fatal("background import did not complete")
	}
	if got := page.Root.FindFirst("p").TextContent(); got != "9" {
		t.Fatalf("async import = %q, want 9 (log: %s)", got, strings.Join(b.jsLog, "; "))
	}
}

func TestClassicScriptCanDynamicImport(t *testing.T) {
	// The module hooks exist on every runtime, so a classic bundle that loads a
	// chunk through import() works on a page with no module script of its own.
	page := newJSPage(`<html><body><p id="out"></p></body></html>`,
		`import("data:text/javascript,export const n = 5;").then(function (m) {`+
			`document.getElementById("out").textContent = String(m.n);});`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	if got := page.Root.FindFirst("p").TextContent(); got != "5" {
		t.Fatalf("classic dynamic import = %q, want 5 (log: %s)", got, strings.Join(b.jsLog, "; "))
	}
}

func TestDuplicateModuleTagsEvaluateOnce(t *testing.T) {
	dep := "data:text/javascript,globalThis.runs = (globalThis.runs || 0) + 1;"
	page := newJSPage(`<html><body><p id="out"></p></body></html>`, "")
	page.Scripts = []Script{
		{Module: true, Code: `import "` + dep + `";`},
		{Module: true, Code: `import "` + dep + `";` +
			`document.getElementById("out").textContent = String(globalThis.runs);`},
	}
	b := NewBrowser(40, 10)
	b.startJS(page)
	if got := page.Root.FindFirst("p").TextContent(); got != "1" {
		t.Fatalf("dependency runs = %q, want 1", got)
	}
}

func TestCollectImportMap(t *testing.T) {
	root := ParseHTML(`<html><head>` +
		`<script type="importmap">{"imports":{"react":"https://cdn.example/react.js","pkg/":"https://cdn.example/pkg/"}}</script>` +
		`</head></html>`)
	m := collectImportMap(root)
	if m["react"] != "https://cdn.example/react.js" {
		t.Fatalf("react mapping = %q", m["react"])
	}
	if m["pkg/"] != "https://cdn.example/pkg/" {
		t.Fatalf("prefix mapping = %q", m["pkg/"])
	}
	if collectImportMap(ParseHTML(`<html><head><script type="importmap">{bad json</script></head></html>`)) != nil {
		t.Fatal("malformed import map was accepted")
	}
	if collectImportMap(ParseHTML(`<html><head><script type="module">1</script></head></html>`)) != nil {
		t.Fatal("module script was read as an import map")
	}
}

func TestBareSpecifierUsesImportMap(t *testing.T) {
	page := moduleTestPage(`<html><body><p id="out"></p></body></html>`,
		`import { v } from "react";`+
			`document.getElementById("out").textContent = v;`)
	page.ImportMap = map[string]string{
		"react": "data:text/javascript,export const v = 'mapped';",
	}
	b := NewBrowser(40, 10)
	b.startJS(page)
	if b.moduleErr {
		t.Fatalf("import map module failed: %s", strings.Join(b.jsLog, "; "))
	}
	if got := page.Root.FindFirst("p").TextContent(); got != "mapped" {
		t.Fatalf("import map result = %q, want mapped", got)
	}
}

func TestImportMapPrefixMapping(t *testing.T) {
	base := mustParse(t, "https://example.com/app/")
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

func TestModuleFailureShowsStatus(t *testing.T) {
	page := moduleTestPage(`<html><body><p>Loading...</p></body></html>`, `import "react";`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	if !b.moduleErr {
		t.Fatal("bare module specifier did not fail")
	}
	if got := b.pageLoadStatus(page, false); got != moduleFailedStatus {
		t.Fatalf("status = %q, want %q", got, moduleFailedStatus)
	}
	if !strings.Contains(strings.Join(b.jsLog, "\n"), "bare module specifier") {
		t.Fatalf("console does not explain the failure: %v", b.jsLog)
	}
}

func TestModuleWithWrongMIMEFails(t *testing.T) {
	// A module must arrive as JavaScript; a text/plain dependency is rejected
	// like a classic script would be.
	page := moduleTestPage(`<html><body><p>Loading...</p></body></html>`,
		`import "data:text/plain,export const n = 1;";`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	if !b.moduleErr {
		t.Fatal("non-JavaScript module did not fail")
	}
	if log := strings.Join(b.jsLog, "\n"); !strings.Contains(log, "is not JavaScript") {
		t.Fatalf("console does not explain the MIME failure: %s", log)
	}
}

func TestCollectScriptsMarksModulesAndSkipsNoModule(t *testing.T) {
	u := mustParse(t, "https://example.com/app/")
	root := ParseHTML(`<html><head>` +
		`<script nomodule>var legacy = 1</script>` +
		`<script type="module">import "./dep.js"</script>` +
		`<script>var classic = 1</script>` +
		`</head></html>`)
	scripts := NewLoader().collectScripts(context.Background(), root, u, false)
	if len(scripts) != 2 {
		t.Fatalf("collected %d scripts, want 2", len(scripts))
	}
	if !scripts[0].Module {
		t.Fatal("module script was not marked as a module")
	}
	if scripts[0].Src != "" {
		t.Fatalf("inline module src = %q, want empty", scripts[0].Src)
	}
	if scripts[1].Module {
		t.Fatal("classic script was marked as a module")
	}
}

func TestModuleSpecifierResolution(t *testing.T) {
	base := mustParse(t, "https://example.com/app/index.html")
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
