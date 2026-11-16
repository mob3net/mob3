package browser

import "testing"

func TestRewriteDynamicImport(t *testing.T) {
	cases := []struct{ in, want string }{
		{`import("./x")`, `__mob3Import("./x")`},
		{`import ( "./x" )`, `__mob3Import ( "./x" )`},
		{`import.meta.url`, `__mob3ImportMeta.url`},
		{`var s = "import('x')";`, `var s = "import('x')";`},
		{`// import("x")`, `// import("x")`},
		{`/* import("x") */ import("y")`, `/* import("x") */ __mob3Import("y")`},
		{"var t = `import('x')`;", "var t = `import('x')`;"},
		{`var r = /import\(/;`, `var r = /import\(/;`},
		{`importer("x")`, `importer("x")`},
		{`obj.import("x")`, `obj.import("x")`},
		{`a = b / import("c")`, `a = b / __mob3Import("c")`},
	}
	for _, c := range cases {
		if got := rewriteDynamicImport(c.in); got != c.want {
			t.Errorf("rewrite(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestJSDynamicImportStub(t *testing.T) {
	page := newJSPage(`<html><body></body></html>`, `
		var metaURL = import.meta.url;
		import("/mod.js").then(function (m) {
			document.title = typeof m + ":" + (m.default === undefined) + ":" + metaURL;
		});
	`)
	b := NewBrowser(40, 10)
	b.startJS(page)
	if page.Title != "object:true:https://example.com/" {
		t.Fatalf("title = %q", page.Title)
	}
	found := false
	for _, entry := range b.jsLog {
		if containsString(entry, "dynamic import is not supported") {
			found = true
		}
	}
	if !found {
		t.Fatalf("dynamic import warning missing: %v", b.jsLog)
	}
}

func containsString(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
