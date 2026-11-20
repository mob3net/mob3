package browser

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/grafana/sobek"
)

const (
	// maxModuleGraph bounds how many modules one page may load. Module sources
	// are fetched while the page is applied, so the cap also bounds how long a
	// page can hold up the UI while its imports resolve.
	maxModuleGraph = 256
	// maxModuleBytes bounds the combined source text of one module graph.
	maxModuleBytes = maxResourceSize
	// maxModuleDepth bounds import chains so a generated graph cannot recurse
	// without end while loading.
	maxModuleDepth = 32
)

// jsModuleLoader loads, links and evaluates ES modules for one page runtime.
// Records are cached per resolved URL, so a dependency shared by several
// modules is fetched, parsed and evaluated once.
type jsModuleLoader struct {
	j         *jsRuntime
	base      *url.URL
	allowFile bool

	// imports is the document's import map. Bare specifiers that it does not
	// cover fail with a console error.
	imports map[string]string

	byURL  map[string]*sobek.SourceTextModuleRecord
	urls   map[sobek.ModuleRecord]string
	failed map[string]error
	// evaluated records the entry modules already run, so two script tags with
	// the same URL evaluate the module once, as the specification requires.
	evaluated map[sobek.ModuleRecord]bool
	count     int
	bytes     int
	inline    int
}

func newJSModuleLoader(j *jsRuntime) *jsModuleLoader {
	m := &jsModuleLoader{
		j:         j,
		byURL:     make(map[string]*sobek.SourceTextModuleRecord),
		urls:      make(map[sobek.ModuleRecord]string),
		failed:    make(map[string]error),
		evaluated: make(map[sobek.ModuleRecord]bool),
	}
	if j.page != nil {
		m.base = j.page.Base
		if m.base == nil {
			m.base = j.page.URL
		}
		m.imports = j.page.ImportMap
	}
	m.allowFile = m.base != nil && strings.EqualFold(m.base.Scheme, "file")
	m.install()
	return m
}

// install wires the host hooks the engine needs for modules: import.meta and
// dynamic import().
func (m *jsModuleLoader) install() {
	vm := m.j.vm
	vm.SetGetImportMetaProperties(func(rec sobek.ModuleRecord) []sobek.MetaProperty {
		u := m.baseString()
		if v, ok := m.urls[rec]; ok {
			u = v
		}
		return []sobek.MetaProperty{{Key: "url", Value: vm.ToValue(u)}}
	})
	vm.SetImportModuleDynamically(func(ref interface{}, spec sobek.Value, pcap interface{}) {
		m.importModule(ref, spec.String(), pcap)
	})
	vm.SetPromiseRejectionTracker(func(p *sobek.Promise, op sobek.PromiseRejectionOperation) {
		if op != sobek.PromiseRejectionReject {
			return
		}
		m.j.b.logJS("warn", "Unhandled promise rejection: "+m.j.errorText(p.Result()))
	})
}

func (m *jsModuleLoader) baseString() string {
	if m.base == nil {
		return ""
	}
	return m.base.String()
}

// importModule resolves and loads a dynamic import() target, then hands the
// record back to the engine. The source is fetched here during the enclosing
// script run, so the watchdog ignores the network time it spends blocked.
func (m *jsModuleLoader) importModule(ref interface{}, spec string, pcap interface{}) {
	m.j.ioActive.Store(true)
	rec, err := m.hostResolve(ref, spec)
	m.j.ioActive.Store(false)
	var mod sobek.ModuleRecord
	if err == nil {
		mod = rec
	}
	m.j.vm.FinishLoadingImportModule(ref, m.j.vm.ToValue(spec), pcap, mod, err)
}

// hostResolve is the host hook the engine calls while linking a module graph.
// specifiers resolve against the importing module's own URL, falling back to
// the document URL for classic scripts and inline modules.
func (m *jsModuleLoader) hostResolve(ref interface{}, spec string) (sobek.ModuleRecord, error) {
	from := m.base
	if rec, ok := ref.(sobek.ModuleRecord); ok {
		if u, ok := m.urls[rec]; ok {
			if parsed, err := url.Parse(u); err == nil {
				from = parsed
			}
		}
	}
	target, err := m.resolveSpecifier(from, spec)
	if err != nil {
		return nil, err
	}
	key := target.String()
	if rec, ok := m.byURL[key]; ok {
		return rec, nil
	}
	if err, ok := m.failed[key]; ok {
		return nil, err
	}
	return m.loadURL(target, key, 0)
}

// resolveSpecifier resolves one import specifier. The document's import map is
// consulted first, so a bare package name such as "react" maps to the URL the
// site shipped; a bare specifier the map does not cover fails with a clear
// message instead of being turned into a same-origin URL and fetched as a 404.
func (m *jsModuleLoader) resolveSpecifier(from *url.URL, spec string) (*url.URL, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, errors.New("empty module specifier")
	}
	if mapped, ok := m.resolveImportMap(spec); ok {
		// Import map addresses are parsed against the document base URL.
		return m.resolveURL(m.base, mapped)
	}
	if isBareModuleSpecifier(spec) {
		return nil, fmt.Errorf("bare module specifier %q (no import map entry)", spec)
	}
	return m.resolveURL(from, spec)
}

// resolveImportMap applies the import map: an exact entry wins, otherwise the
// longest trailing-slash prefix mapping is used, as the specification says.
func (m *jsModuleLoader) resolveImportMap(spec string) (string, bool) {
	if len(m.imports) == 0 {
		return "", false
	}
	if target, ok := m.imports[spec]; ok && target != "" {
		return target, true
	}
	best, bestLen := "", 0
	for key, target := range m.imports {
		if !strings.HasSuffix(key, "/") || target == "" || len(key) > len(spec) {
			continue
		}
		if strings.HasPrefix(spec, key) && len(key) > bestLen {
			best, bestLen = target+spec[len(key):], len(key)
		}
	}
	if bestLen > 0 {
		return best, true
	}
	return "", false
}

// resolveURL resolves a URL reference against a base and rejects schemes the
// loader cannot fetch.
func (m *jsModuleLoader) resolveURL(base *url.URL, spec string) (*url.URL, error) {
	if base == nil {
		base = &url.URL{}
	}
	ref, err := base.Parse(spec)
	if err != nil {
		return nil, fmt.Errorf("invalid module specifier %q", spec)
	}
	switch strings.ToLower(ref.Scheme) {
	case "http", "https", "file", "data":
	default:
		return nil, fmt.Errorf("unsupported module scheme %q", ref.Scheme)
	}
	ref.Fragment = ""
	return ref, nil
}

// isBareModuleSpecifier reports a specifier that is neither absolute nor
// explicitly relative, such as "react" or "@scope/pkg".
func isBareModuleSpecifier(spec string) bool {
	if spec == "" || strings.HasPrefix(spec, "/") || strings.HasPrefix(spec, ".") ||
		strings.HasPrefix(spec, "#") {
		return false
	}
	if i := strings.IndexByte(spec, ':'); i >= 0 {
		return false
	}
	return true
}

// runModule loads a module script and evaluates it with its dependencies.
func (j *jsRuntime) runModule(sc Script) error {
	if j.modules == nil {
		j.modules = newJSModuleLoader(j)
	}
	m := j.modules
	var rec *sobek.SourceTextModuleRecord
	var err error
	if sc.Src == "" {
		rec, err = m.loadInline(sc.Code)
	} else {
		u, perr := url.Parse(sc.Src)
		if perr != nil {
			return fmt.Errorf("invalid module URL %q", sc.Src)
		}
		if cached, ok := m.byURL[u.String()]; ok {
			rec = cached
		} else {
			// The loader already fetched this module while collecting the
			// page's scripts, so reuse that body instead of a second request.
			rec, err = m.parseModule(u, u.String(), sc.Code, 0)
		}
	}
	if err != nil {
		return err
	}
	return m.evaluate(rec)
}

// loadInline parses an inline module. Its imports resolve against the document
// URL, matching the browser rule that an inline module's base URL is the
// document's.
func (m *jsModuleLoader) loadInline(code string) (*sobek.SourceTextModuleRecord, error) {
	if err := m.reserve(len(code)); err != nil {
		return nil, err
	}
	m.inline++
	name := fmt.Sprintf("inline module %d", m.inline)
	rec, err := sobek.ParseModule(name, code, m.hostResolve)
	if err != nil {
		return nil, err
	}
	m.urls[rec] = m.baseString()
	if err := m.prefetch(rec, m.base, 0); err != nil {
		return nil, err
	}
	return rec, nil
}

// loadURL fetches, parses and prefetches one module by URL. The record is
// cached before its dependencies are visited, so import cycles terminate.
func (m *jsModuleLoader) loadURL(u *url.URL, key string, depth int) (*sobek.SourceTextModuleRecord, error) {
	if depth > maxModuleDepth {
		return nil, fmt.Errorf("module import depth limit reached at %s", key)
	}
	code, err := m.fetchModule(u)
	if err != nil {
		m.failed[key] = err
		return nil, err
	}
	return m.parseModule(u, key, code, depth)
}

// parseModule parses and caches one module body, then loads its static
// dependencies.
func (m *jsModuleLoader) parseModule(u *url.URL, key, code string, depth int) (*sobek.SourceTextModuleRecord, error) {
	if err := m.reserve(len(code)); err != nil {
		return nil, err
	}
	rec, err := sobek.ParseModule(key, code, m.hostResolve)
	if err != nil {
		m.failed[key] = err
		return nil, err
	}
	m.byURL[key] = rec
	m.urls[rec] = key
	if err := m.prefetch(rec, u, depth); err != nil {
		return nil, err
	}
	return rec, nil
}

// prefetch loads every static dependency of a parsed module before linking
// starts, so linking and evaluation never need the network.
func (m *jsModuleLoader) prefetch(rec *sobek.SourceTextModuleRecord, base *url.URL, depth int) error {
	for _, spec := range rec.RequestedModules() {
		target, err := m.resolveSpecifier(base, spec)
		if err != nil {
			return err
		}
		key := target.String()
		if _, ok := m.byURL[key]; ok {
			continue
		}
		if err, ok := m.failed[key]; ok {
			return err
		}
		if _, err := m.loadURL(target, key, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// reserve applies the graph-wide module and byte caps.
func (m *jsModuleLoader) reserve(size int) error {
	if m.count >= maxModuleGraph {
		return fmt.Errorf("module limit reached (%d modules)", maxModuleGraph)
	}
	if m.bytes+size > maxModuleBytes {
		return errors.New("module graph exceeds the size limit")
	}
	m.count++
	m.bytes += size
	return nil
}

// fetchModule reads one module source with the same request accounting, MIME
// rule and session cache as classic scripts.
func (m *jsModuleLoader) fetchModule(u *url.URL) (string, error) {
	if m.j.b == nil || m.j.b.loader == nil {
		return "", errors.New("no loader for module fetch")
	}
	ctx := withRequestKind(context.Background(), "js")
	data, ctype, _, status, err := m.j.b.loader.fetchCtx(ctx, u, m.allowFile)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 400 {
		return "", fmt.Errorf("module %s: HTTP %d", u.Redacted(), status)
	}
	if !isJavaScriptMIME(ctype) {
		return "", fmt.Errorf("module %s is not JavaScript (%s)", u.Redacted(), mimeEssence(ctype))
	}
	return decodeBody(data, ctype), nil
}

// evaluate links and evaluates a module graph under the usual script guard.
func (m *jsModuleLoader) evaluate(rec *sobek.SourceTextModuleRecord) error {
	if rec == nil {
		return errors.New("module was not loaded")
	}
	if m.evaluated == nil {
		m.evaluated = make(map[sobek.ModuleRecord]bool)
	}
	if m.evaluated[rec] {
		return nil
	}
	m.evaluated[rec] = true
	return m.j.guard(func() error {
		if err := rec.Link(); err != nil {
			return err
		}
		p := m.j.vm.CyclicModuleRecordEvaluate(rec, m.hostResolve)
		if p == nil || p.State() != sobek.PromiseStateRejected {
			return nil
		}
		return jsThrownError(p.Result())
	})
}

// jsThrownError turns a thrown JavaScript value into an error for the console.
func jsThrownError(v sobek.Value) error {
	if v == nil {
		return errors.New("module evaluation failed")
	}
	if ex, ok := v.Export().(error); ok {
		return ex
	}
	return errors.New(jsValueString(v))
}

// jsValueString renders a thrown value the way the console reports errors.
func jsValueString(v sobek.Value) string {
	if v == nil {
		return "undefined"
	}
	if ex, ok := v.Export().(*sobek.Exception); ok {
		return ex.Error()
	}
	if s := strings.TrimSpace(v.String()); s != "" {
		return s
	}
	return "module evaluation failed"
}
