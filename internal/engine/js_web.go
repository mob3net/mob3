package engine

import (
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/grafana/sobek"

	"browser/internal/protocol"
)

// newURL implements a subset of the WHATWG URL object.
func (j *jsRuntime) newURL(input string, base sobek.Value) *sobek.Object {
	var u *url.URL
	var err error
	if base == nil || sobek.IsNull(base) || sobek.IsUndefined(base) {
		u, err = url.Parse(input)
		if err == nil && u.Scheme == "" {
			err = errBadSVG
		}
	} else {
		bu, e := url.Parse(base.String())
		if e != nil {
			err = e
		} else {
			u, err = bu.Parse(input)
		}
	}
	if err != nil || u == nil {
		// Include the offending value so a page that builds URLs from missing
		// config reports something actionable in the console.
		bad := input
		if len(bad) > 120 {
			bad = bad[:120] + "…"
		}
		panic(j.vm.NewTypeError("Invalid URL: " + bad))
	}
	o := j.vm.NewObject()
	_ = o.Set("href", u.String())
	_ = o.Set("protocol", u.Scheme+":")
	_ = o.Set("host", u.Host)
	_ = o.Set("hostname", u.Hostname())
	_ = o.Set("port", u.Port())
	path := u.Path
	if path == "" {
		path = "/"
	}
	_ = o.Set("pathname", path)
	_ = o.Set("search", prefixQuery(u.RawQuery))
	_ = o.Set("hash", prefixHash(u.Fragment))
	origin := u.Scheme + "://" + u.Host
	if u.Scheme == "" || u.Scheme == "file" || u.Scheme == "about" || u.Scheme == "data" {
		origin = "null"
	}
	_ = o.Set("origin", origin)
	_ = o.Set("username", u.User.Username())
	pw, _ := u.User.Password()
	_ = o.Set("password", pw)
	_ = o.Set("searchParams", j.urlSearchParamsValue(u.RawQuery))
	_ = o.Set("toString", j.vm.ToValue(func(sobek.FunctionCall) sobek.Value { return j.vm.ToValue(u.String()) }))
	_ = o.Set("toJSON", j.vm.ToValue(func(sobek.FunctionCall) sobek.Value { return j.vm.ToValue(u.String()) }))
	return o
}

func (j *jsRuntime) urlSearchParamsValue(query string) *sobek.Object {
	return j.newURLSearchParams(j.vm.ToValue(strings.TrimPrefix(query, "?")))
}

func (j *jsRuntime) newURLSearchParams(init sobek.Value) *sobek.Object {
	vals := url.Values{}
	if init != nil && !sobek.IsNull(init) && !sobek.IsUndefined(init) {
		if s, ok := init.Export().(string); ok {
			s = strings.TrimPrefix(s, "?")
			if parsed, err := url.ParseQuery(s); err == nil {
				vals = parsed
			}
		} else if obj, ok := init.(*sobek.Object); ok {
			for _, key := range obj.Keys() {
				v := obj.Get(key)
				if v == nil || sobek.IsUndefined(v) || sobek.IsNull(v) {
					continue
				}
				vals.Add(key, v.String())
			}
		}
	}
	o := j.vm.NewObject()
	_ = o.Set("append", j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		vals.Add(call.Argument(0).String(), call.Argument(1).String())
		return sobek.Undefined()
	}))
	_ = o.Set("set", j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		vals.Set(call.Argument(0).String(), call.Argument(1).String())
		return sobek.Undefined()
	}))
	_ = o.Set("delete", j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		vals.Del(call.Argument(0).String())
		return sobek.Undefined()
	}))
	_ = o.Set("has", j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		_, ok := vals[call.Argument(0).String()]
		return j.vm.ToValue(ok)
	}))
	_ = o.Set("get", j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		v, ok := vals[call.Argument(0).String()]
		if !ok || len(v) == 0 {
			return sobek.Null()
		}
		return j.vm.ToValue(v[0])
	}))
	_ = o.Set("getAll", j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		v := vals[call.Argument(0).String()]
		arr := make([]interface{}, 0, len(v))
		for _, s := range v {
			arr = append(arr, s)
		}
		return j.vm.NewArray(arr...)
	}))
	_ = o.Set("keys", j.vm.ToValue(func(sobek.FunctionCall) sobek.Value {
		arr := make([]interface{}, 0, len(vals))
		for k := range vals {
			arr = append(arr, k)
		}
		return j.vm.NewArray(arr...)
	}))
	_ = o.Set("values", j.vm.ToValue(func(sobek.FunctionCall) sobek.Value {
		var arr []interface{}
		for _, vs := range vals {
			for _, v := range vs {
				arr = append(arr, v)
			}
		}
		return j.vm.NewArray(arr...)
	}))
	_ = o.Set("forEach", j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		fn, ok := sobek.AssertFunction(call.Argument(0))
		if !ok {
			return sobek.Undefined()
		}
		for k, vs := range vals {
			for _, v := range vs {
				fn(sobek.Undefined(), j.vm.ToValue(v), j.vm.ToValue(k), o)
			}
		}
		return sobek.Undefined()
	}))
	entriesArray := func() *sobek.Object {
		keys := make([]string, 0, len(vals))
		for k := range vals {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		arr := make([]interface{}, 0, len(keys))
		for _, k := range keys {
			for _, v := range vals[k] {
				arr = append(arr, j.vm.NewArray(k, v))
			}
		}
		return j.vm.NewArray(arr...)
	}
	_ = o.Set("entries", j.vm.ToValue(func(sobek.FunctionCall) sobek.Value {
		return entriesArray()
	}))
	_ = o.SetSymbol(sobek.SymIterator, j.vm.ToValue(func(sobek.FunctionCall) sobek.Value {
		arr := entriesArray()
		fn, ok := sobek.AssertFunction(arr.Get("values"))
		if !ok {
			return sobek.Undefined()
		}
		it, _ := fn(arr)
		return it
	}))
	_ = o.Set("sort", j.vm.ToValue(func(sobek.FunctionCall) sobek.Value {
		keys := make([]string, 0, len(vals))
		for k := range vals {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		sorted := make(url.Values, len(vals))
		for _, k := range keys {
			sorted[k] = vals[k]
		}
		vals = sorted
		return sobek.Undefined()
	}))
	_ = o.Set("toString", j.vm.ToValue(func(sobek.FunctionCall) sobek.Value {
		return j.vm.ToValue(vals.Encode())
	}))
	j.accessor(o, "size", func() sobek.Value {
		return j.vm.ToValue(len(vals))
	}, nil)
	return o
}

func (j *jsRuntime) storageObject(data map[string]string) *sobek.Object {
	o := j.vm.NewObject()
	_ = o.Set("getItem", j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		if v, ok := data[call.Argument(0).String()]; ok {
			return j.vm.ToValue(v)
		}
		return sobek.Null()
	}))
	_ = o.Set("setItem", j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		data[call.Argument(0).String()] = call.Argument(1).String()
		return sobek.Undefined()
	}))
	_ = o.Set("removeItem", j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		delete(data, call.Argument(0).String())
		return sobek.Undefined()
	}))
	_ = o.Set("clear", j.vm.ToValue(func(sobek.FunctionCall) sobek.Value {
		for k := range data {
			delete(data, k)
		}
		return sobek.Undefined()
	}))
	_ = o.Set("key", j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		i := int(call.Argument(0).ToInteger())
		keys := make([]string, 0, len(data))
		for k := range data {
			keys = append(keys, k)
		}
		if i < 0 || i >= len(keys) {
			return sobek.Null()
		}
		return j.vm.ToValue(keys[i])
	}))
	j.accessor(o, "length", func() sobek.Value { return j.vm.ToValue(len(data)) }, nil)

	// Web Storage also exposes each key as a named property. Use a proxy so
	// `storage.key = value`, `storage.key` and `delete storage.key` share the
	// same map as getItem/setItem/removeItem.
	proxy := j.vm.NewProxy(o, &sobek.ProxyTrapConfig{
		Get: func(target *sobek.Object, property string, _ sobek.Value) sobek.Value {
			if v := target.Get(property); v != nil && !sobek.IsUndefined(v) {
				return v
			}
			if v, ok := data[property]; ok {
				return j.vm.ToValue(v)
			}
			return sobek.Undefined()
		},
		Set: func(target *sobek.Object, property string, value sobek.Value, _ sobek.Value) bool {
			if v := target.Get(property); v != nil && !sobek.IsUndefined(v) {
				return true // built-in method/accessor names keep precedence
			}
			data[property] = value.String()
			return true
		},
		Has: func(target *sobek.Object, property string) bool {
			if v := target.Get(property); v != nil && !sobek.IsUndefined(v) {
				return true
			}
			_, ok := data[property]
			return ok
		},
		DeleteProperty: func(_ *sobek.Object, property string) bool {
			delete(data, property)
			return true
		},
	})
	return j.vm.ToValue(proxy).(*sobek.Object)
}

// documentWrite appends a parsed HTML fragment to the body.
func (j *jsRuntime) documentWrite(html string, newline bool) {
	if newline {
		html += "\n"
	}
	if strings.TrimSpace(html) == "" || j.page == nil || j.page.Root == nil {
		return
	}
	target := j.page.Root.FindFirst("body")
	if target == nil {
		target = j.page.Root
	}
	wrapper := ParseHTML("<html><body>" + html + "</body></html>")
	body := wrapper.FindFirst("body")
	if body == nil {
		return
	}
	for _, c := range body.Children {
		c.Parent = target
		target.Children = append(target.Children, c)
	}
	j.markDirty()
}

// installFetch adds fetch plus the Headers, Request and Response constructors.
// Requests are performed synchronously under the hood, but the API is
// promise-based. Like XMLHttpRequest, fetch is limited to the page's origin.
func (j *jsRuntime) installFetch() {
	_ = j.vm.Set("__mob3NewHeaders", j.vm.ToValue(j.newHeadersObject))
	_ = j.vm.Set("__mob3NewRequest", j.vm.ToValue(j.newRequestObject))
	_ = j.vm.Set("__mob3NewResponse", j.vm.ToValue(j.newResponseObject))
	const script = `
(function () {
	const nh = globalThis.__mob3NewHeaders;
	const nr = globalThis.__mob3NewRequest;
	const ns = globalThis.__mob3NewResponse;
	function Headers(init) { return nh(init === undefined ? null : init); }
	function Request(input, init) { return nr(String(input), init === undefined ? null : init); }
	function Response(body, init) { return ns(body === undefined ? null : body, init === undefined ? null : init); }
	globalThis.Headers = Headers;
	globalThis.Request = Request;
	globalThis.Response = Response;
	delete globalThis.__mob3NewHeaders;
	delete globalThis.__mob3NewRequest;
	delete globalThis.__mob3NewResponse;
})();
`
	if _, err := j.vm.RunScript("fetch constructors", script); err != nil {
		j.ReportError(err)
	}
	_ = j.vm.Set("fetch", j.vm.ToValue(j.fetchFunc))
}

func (j *jsRuntime) fetchFunc(call sobek.FunctionCall) sobek.Value {
	p, resolve, reject := j.vm.NewPromise()
	fail := func(msg string) sobek.Value {
		_ = reject(j.vm.NewTypeError(msg))
		return j.vm.ToValue(p)
	}
	failAbort := func() sobek.Value {
		_ = reject(j.abortError())
		return j.vm.ToValue(p)
	}
	input := call.Argument(0)
	init := jsObject(call.Argument(1))
	target := ""
	method := "GET"
	body := ""
	headers := map[string][]string{}
	if ro := jsObject(input); ro != nil {
		u := ro.Get("url")
		if u == nil || sobek.IsUndefined(u) || sobek.IsNull(u) || u.String() == "" {
			return fail("Failed to parse URL")
		}
		target = u.String()
		if m := ro.Get("method"); m != nil && !sobek.IsUndefined(m) && m.String() != "" {
			method = m.String()
		}
		if h := ro.Get("headers"); h != nil && !sobek.IsUndefined(h) && !sobek.IsNull(h) {
			headers = j.parseHeaders(h)
		}
		if b := ro.Get("body"); b != nil && !sobek.IsUndefined(b) && !sobek.IsNull(b) {
			body = b.String()
		}
	} else {
		target = input.String()
	}
	if init != nil {
		if m := init.Get("method"); m != nil && !sobek.IsUndefined(m) && m.String() != "" {
			method = m.String()
		}
		if h := init.Get("headers"); h != nil && !sobek.IsUndefined(h) && !sobek.IsNull(h) {
			headers = j.parseHeaders(h)
		}
		if b := init.Get("body"); b != nil && !sobek.IsUndefined(b) && !sobek.IsNull(b) {
			body = b.String()
		}
		if sig := jsObject(init.Get("signal")); sig != nil && j.eventFlag(sig, "aborted") {
			return failAbort()
		}
	}
	base := j.page.Base
	if base == nil {
		base = j.page.URL
	}
	var u *url.URL
	var err error
	if base != nil {
		u, err = base.Parse(target)
	} else {
		u, err = url.Parse(target)
	}
	if err != nil || u == nil {
		return fail("Failed to parse URL")
	}
	if j.page == nil || j.page.URL == nil {
		return fail("Cross-origin fetch blocked")
	}
	method = strings.ToUpper(method)
	reqHeaders := make(map[string]string, len(headers))
	for k, vs := range headers {
		if len(vs) > 0 {
			reqHeaders[canonicalHeaderKey(k)] = strings.Join(vs, ", ")
		}
	}
	status, data, ctype, respHeaders, final, err := j.HTTPXHR("fetch", method, u, []byte(body), reqHeaders)
	if err != nil {
		return fail(err.Error())
	}
	finalURL := u.String()
	if final != nil {
		finalURL = final.String()
	}
	_ = resolve(j.responseValue(&fetchResponse{
		status:     status,
		text:       statusText(status),
		url:        finalURL,
		ctype:      ctype,
		headers:    respHeaders,
		data:       data,
		redirected: final != nil && final.String() != u.String(),
	}))
	return j.vm.ToValue(p)
}

type fetchResponse struct {
	status     int
	text       string
	url        string
	ctype      string
	headers    map[string][]string
	data       []byte
	redirected bool
}

func (j *jsRuntime) responseValue(st *fetchResponse) *sobek.Object {
	o := j.vm.NewObject()
	used := false
	_ = o.Set("status", st.status)
	_ = o.Set("statusText", st.text)
	_ = o.Set("ok", st.status >= 200 && st.status <= 299)
	_ = o.Set("url", st.url)
	_ = o.Set("redirected", st.redirected)
	_ = o.Set("type", "basic")
	_ = o.Set("headers", j.headersValue(st.headers))
	j.accessor(o, "bodyUsed", func() sobek.Value { return j.vm.ToValue(used) }, nil)
	bodyText := j.host.DecodeBody(st.data, st.ctype)
	consume := func() bool {
		if used {
			return false
		}
		used = true
		return true
	}
	j.method(o, "text", func(sobek.FunctionCall) sobek.Value {
		if !consume() {
			return j.rejectedPromise(j.vm.NewTypeError("Body has already been consumed"))
		}
		return j.resolvedPromise(bodyText)
	})
	j.method(o, "json", func(sobek.FunctionCall) sobek.Value {
		if !consume() {
			return j.rejectedPromise(j.vm.NewTypeError("Body has already been consumed"))
		}
		var v interface{}
		if err := json.Unmarshal(st.data, &v); err != nil {
			return j.rejectedPromise(j.vm.NewTypeError("Invalid JSON: " + err.Error()))
		}
		return j.resolvedPromise(j.vm.ToValue(v))
	})
	j.method(o, "arrayBuffer", func(sobek.FunctionCall) sobek.Value {
		if !consume() {
			return j.rejectedPromise(j.vm.NewTypeError("Body has already been consumed"))
		}
		return j.resolvedPromise(j.vm.NewArrayBuffer(append([]byte(nil), st.data...)))
	})
	j.method(o, "blob", func(sobek.FunctionCall) sobek.Value {
		if !consume() {
			return j.rejectedPromise(j.vm.NewTypeError("Body has already been consumed"))
		}
		blob := j.vm.NewObject()
		_ = blob.Set("type", st.ctype)
		_ = blob.Set("size", len(st.data))
		j.method(blob, "text", func(sobek.FunctionCall) sobek.Value {
			return j.resolvedPromise(bodyText)
		})
		j.method(blob, "arrayBuffer", func(sobek.FunctionCall) sobek.Value {
			return j.resolvedPromise(j.vm.NewArrayBuffer(append([]byte(nil), st.data...)))
		})
		return j.resolvedPromise(blob)
	})
	j.method(o, "clone", func(sobek.FunctionCall) sobek.Value {
		cp := *st
		cp.data = append([]byte(nil), st.data...)
		return j.responseValue(&cp)
	})
	return o
}

func (j *jsRuntime) resolvedPromise(v interface{}) sobek.Value {
	p, resolve, _ := j.vm.NewPromise()
	_ = resolve(v)
	return j.vm.ToValue(p)
}

func (j *jsRuntime) rejectedPromise(v interface{}) sobek.Value {
	p, _, reject := j.vm.NewPromise()
	_ = reject(v)
	return j.vm.ToValue(p)
}

func (j *jsRuntime) headersValue(h map[string][]string) *sobek.Object {
	m := make(map[string][]string, len(h))
	for k, vs := range h {
		m[strings.ToLower(k)] = append([]string(nil), vs...)
	}
	return j.headersObject(m)
}

func (j *jsRuntime) headersObject(m map[string][]string) *sobek.Object {
	o := j.vm.NewObject()
	j.headerObjs[o] = m
	lower := func(v sobek.Value) string { return strings.ToLower(strings.TrimSpace(v.String())) }
	j.method(o, "get", func(call sobek.FunctionCall) sobek.Value {
		vs := m[lower(call.Argument(0))]
		if len(vs) == 0 {
			return sobek.Null()
		}
		return j.vm.ToValue(strings.Join(vs, ", "))
	})
	j.method(o, "has", func(call sobek.FunctionCall) sobek.Value {
		_, ok := m[lower(call.Argument(0))]
		return j.vm.ToValue(ok)
	})
	j.method(o, "set", func(call sobek.FunctionCall) sobek.Value {
		m[lower(call.Argument(0))] = []string{call.Argument(1).String()}
		return sobek.Undefined()
	})
	j.method(o, "append", func(call sobek.FunctionCall) sobek.Value {
		k := lower(call.Argument(0))
		m[k] = append(m[k], call.Argument(1).String())
		return sobek.Undefined()
	})
	j.method(o, "delete", func(call sobek.FunctionCall) sobek.Value {
		delete(m, lower(call.Argument(0)))
		return sobek.Undefined()
	})
	j.method(o, "getSetCookie", func(sobek.FunctionCall) sobek.Value {
		return j.vm.ToValue(m["set-cookie"])
	})
	j.method(o, "forEach", func(call sobek.FunctionCall) sobek.Value {
		fn, ok := sobek.AssertFunction(call.Argument(0))
		if !ok {
			return sobek.Undefined()
		}
		for _, k := range sortedHeaderKeys(m) {
			j.call(fn, o, j.vm.ToValue(strings.Join(m[k], ", ")), j.vm.ToValue(k), o)
		}
		return sobek.Undefined()
	})
	j.method(o, "keys", func(sobek.FunctionCall) sobek.Value {
		keys := sortedHeaderKeys(m)
		return j.vm.ToValue(keys)
	})
	j.method(o, "values", func(sobek.FunctionCall) sobek.Value {
		var vals []string
		for _, k := range sortedHeaderKeys(m) {
			vals = append(vals, strings.Join(m[k], ", "))
		}
		return j.vm.ToValue(vals)
	})
	j.method(o, "entries", func(sobek.FunctionCall) sobek.Value {
		out := j.vm.NewArray()
		for i, k := range sortedHeaderKeys(m) {
			_ = out.Set(strconv.Itoa(i), j.vm.NewArray(k, strings.Join(m[k], ", ")))
		}
		return out
	})
	return o
}

func jsArrayLength(o *sobek.Object) int {
	if o == nil {
		return 0
	}
	v := o.Get("length")
	if v == nil || sobek.IsUndefined(v) || sobek.IsNull(v) {
		return 0
	}
	return int(v.ToInteger())
}

func sortedHeaderKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (j *jsRuntime) newHeadersObject(call sobek.FunctionCall) sobek.Value {
	return j.headersObject(j.parseHeaders(call.Argument(0)))
}

func (j *jsRuntime) parseHeaders(v sobek.Value) map[string][]string {
	out := make(map[string][]string)
	if v == nil || sobek.IsUndefined(v) || sobek.IsNull(v) {
		return out
	}
	if o, ok := v.(*sobek.Object); ok {
		if m, ok := j.headerObjs[o]; ok {
			for k, vs := range m {
				out[k] = append([]string(nil), vs...)
			}
			return out
		}
		n := jsArrayLength(o)
		if n > 0 {
			for i := 0; i < n; i++ {
				pair := jsObject(o.Get(strconv.Itoa(i)))
				if pair == nil {
					continue
				}
				keyValue, valueValue := pair.Get("0"), pair.Get("1")
				if keyValue == nil || valueValue == nil {
					continue
				}
				key := strings.ToLower(strings.TrimSpace(keyValue.String()))
				if key != "" {
					out[key] = append(out[key], valueValue.String())
				}
			}
			return out
		}
		for _, k := range o.Keys() {
			out[strings.ToLower(k)] = []string{o.Get(k).String()}
		}
	}
	return out
}

func (j *jsRuntime) newRequestObject(call sobek.FunctionCall) sobek.Value {
	input := call.Argument(0)
	init := jsObject(call.Argument(1))
	target := input.String()
	method := "GET"
	body := ""
	headers := map[string][]string{}
	if ro := jsObject(input); ro != nil {
		if v := ro.Get("url"); v != nil && !sobek.IsUndefined(v) {
			target = v.String()
		}
		if v := ro.Get("method"); v != nil && !sobek.IsUndefined(v) && v.String() != "" {
			method = v.String()
		}
		if v := ro.Get("headers"); v != nil && !sobek.IsUndefined(v) && !sobek.IsNull(v) {
			headers = j.parseHeaders(v)
		}
		if v := ro.Get("body"); v != nil && !sobek.IsUndefined(v) && !sobek.IsNull(v) {
			body = v.String()
		}
	}
	if init != nil {
		if v := init.Get("method"); v != nil && !sobek.IsUndefined(v) && v.String() != "" {
			method = v.String()
		}
		if v := init.Get("headers"); v != nil && !sobek.IsUndefined(v) && !sobek.IsNull(v) {
			headers = j.parseHeaders(v)
		}
		if v := init.Get("body"); v != nil && !sobek.IsUndefined(v) && !sobek.IsNull(v) {
			body = v.String()
		}
	}
	base := j.page.Base
	if base == nil {
		base = j.page.URL
	}
	if base != nil {
		if u, err := base.Parse(target); err == nil {
			target = u.String()
		}
	}
	o := j.vm.NewObject()
	_ = o.Set("url", target)
	_ = o.Set("method", strings.ToUpper(method))
	_ = o.Set("headers", j.headersObject(headers))
	_ = o.Set("body", body)
	_ = o.Set("bodyUsed", false)
	j.method(o, "clone", func(sobek.FunctionCall) sobek.Value {
		cp := j.vm.NewObject()
		_ = cp.Set("url", target)
		_ = cp.Set("method", strings.ToUpper(method))
		_ = cp.Set("headers", j.headersObject(headers))
		_ = cp.Set("body", body)
		_ = cp.Set("bodyUsed", false)
		return cp
	})
	return o
}

func (j *jsRuntime) newResponseObject(call sobek.FunctionCall) sobek.Value {
	init := jsObject(call.Argument(1))
	status := 200
	statusText := ""
	headers := map[string][]string{}
	if init != nil {
		if v := init.Get("status"); v != nil && !sobek.IsUndefined(v) && v.ToInteger() != 0 {
			status = int(v.ToInteger())
		}
		statusText = jsOptionString(init, "statusText", "")
		if v := init.Get("headers"); v != nil && !sobek.IsUndefined(v) && !sobek.IsNull(v) {
			headers = j.parseHeaders(v)
		}
	}
	data := []byte(call.Argument(0).String())
	if sobek.IsUndefined(call.Argument(0)) || sobek.IsNull(call.Argument(0)) {
		data = nil
	}
	h := make(map[string][]string, len(headers))
	for k, vs := range headers {
		h[canonicalHeaderKey(k)] = vs
	}
	ctype := headerGet(h, "Content-Type")
	return j.responseValue(&fetchResponse{
		status:  status,
		text:    statusText,
		ctype:   ctype,
		headers: h,
		data:    data,
	})
}

type xhrState struct {
	obj          *sobek.Object
	method       string
	url          *url.URL
	reqHeaders   map[string]string
	resHeaders   map[string][]string
	status       int
	statusText   string
	readyState   int
	text         string
	response     sobek.Value
	ctype        string
	responseURL  string
	err          error
	responseType string
	listeners    map[string][]sobek.Value
	blocked      bool
}

func (j *jsRuntime) xmlHTTPRequestObject() *sobek.Object {
	o := j.vm.NewObject()
	st := &xhrState{obj: o, reqHeaders: make(map[string]string), listeners: make(map[string][]sobek.Value), responseType: ""}
	get := func() sobek.Value {
		if st.responseType == "json" {
			if st.response != nil {
				return st.response
			}
			var v interface{}
			if err := json.Unmarshal([]byte(st.text), &v); err == nil {
				st.response = j.vm.ToValue(v)
			} else {
				st.response = sobek.Null()
			}
			return st.response
		}
		if st.responseType == "arraybuffer" {
			if st.response != nil {
				return st.response
			}
			buf := j.vm.NewArrayBuffer([]byte(st.text))
			st.response = j.vm.ToValue(buf)
			return st.response
		}
		return j.vm.ToValue(st.text)
	}
	j.accessor(o, "readyState", func() sobek.Value { return j.vm.ToValue(st.readyState) }, nil)
	j.accessor(o, "status", func() sobek.Value { return j.vm.ToValue(st.status) }, nil)
	j.accessor(o, "statusText", func() sobek.Value { return j.vm.ToValue(st.statusText) }, nil)
	j.accessor(o, "responseText", func() sobek.Value { return j.vm.ToValue(st.text) }, nil)
	j.accessor(o, "response", func() sobek.Value { return get() }, nil)
	j.accessor(o, "responseURL", func() sobek.Value { return j.vm.ToValue(st.responseURL) }, nil)
	j.accessor(o, "responseType", func() sobek.Value { return j.vm.ToValue(st.responseType) }, func(v sobek.Value) {
		st.responseType = v.String()
		st.response = nil
	})
	j.accessor(o, "timeout", func() sobek.Value { return j.vm.ToValue(0) }, func(sobek.Value) {})
	j.accessor(o, "withCredentials", func() sobek.Value { return j.vm.ToValue(false) }, func(sobek.Value) {})
	j.accessor(o, "upload", func() sobek.Value { return j.vm.NewObject() }, nil)

	j.method(o, "open", func(call sobek.FunctionCall) sobek.Value {
		st.method = strings.ToUpper(call.Argument(0).String())
		if st.method == "" {
			st.method = "GET"
		}
		target := call.Argument(1).String()
		base := j.page.Base
		if base == nil {
			base = j.page.URL
		}
		if base != nil {
			st.url, _ = base.Parse(target)
		} else {
			st.url, _ = url.Parse(target)
		}
		st.reqHeaders = make(map[string]string)
		st.resHeaders = nil
		st.status = 0
		st.statusText = ""
		st.text = ""
		st.response = nil
		st.err = nil
		st.blocked = st.url == nil || j.page == nil || j.page.URL == nil
		st.readyState = 1
		j.fireXHR(st, "readystatechange")
		return sobek.Undefined()
	})
	j.method(o, "setRequestHeader", func(call sobek.FunctionCall) sobek.Value {
		st.reqHeaders[call.Argument(0).String()] = call.Argument(1).String()
		return sobek.Undefined()
	})
	j.method(o, "getResponseHeader", func(call sobek.FunctionCall) sobek.Value {
		if st.resHeaders == nil {
			return sobek.Null()
		}
		v := headerGet(st.resHeaders, call.Argument(0).String())
		if v == "" {
			return sobek.Null()
		}
		return j.vm.ToValue(v)
	})
	j.method(o, "getAllResponseHeaders", func(sobek.FunctionCall) sobek.Value {
		if st.resHeaders == nil {
			return j.vm.ToValue("")
		}
		var b strings.Builder
		for k, vs := range st.resHeaders {
			for _, v := range vs {
				b.WriteString(k)
				b.WriteString(": ")
				b.WriteString(v)
				b.WriteString("\r\n")
			}
		}
		return j.vm.ToValue(b.String())
	})
	j.method(o, "overrideMimeType", func(sobek.FunctionCall) sobek.Value { return sobek.Undefined() })
	j.method(o, "abort", func(sobek.FunctionCall) sobek.Value {
		st.readyState = 0
		j.fireXHR(st, "abort")
		return sobek.Undefined()
	})
	j.method(o, "addEventListener", func(call sobek.FunctionCall) sobek.Value {
		typ := call.Argument(0).String()
		st.listeners[typ] = append(st.listeners[typ], call.Argument(1))
		return sobek.Undefined()
	})
	j.method(o, "removeEventListener", func(call sobek.FunctionCall) sobek.Value {
		typ := call.Argument(0).String()
		fn := call.Argument(1)
		list := st.listeners[typ]
		out := list[:0]
		for _, v := range list {
			if !v.SameAs(fn) {
				out = append(out, v)
			}
		}
		st.listeners[typ] = out
		return sobek.Undefined()
	})
	j.method(o, "send", func(call sobek.FunctionCall) sobek.Value {
		if st.url == nil || st.blocked {
			j.host.LogJS("error", "XMLHttpRequest blocked: invalid URL")
			st.readyState = 4
			st.status = 0
			st.err = errBadSVG
			j.fireXHR(st, "readystatechange")
			j.fireXHR(st, "error")
			j.fireXHR(st, "loadend")
			return sobek.Undefined()
		}
		var body []byte
		if len(call.Arguments) > 0 && !sobek.IsNull(call.Argument(0)) && !sobek.IsUndefined(call.Argument(0)) {
			body = []byte(call.Argument(0).String())
		}
		st.readyState = 2
		j.fireXHR(st, "readystatechange")
		st.readyState = 3
		j.fireXHR(st, "readystatechange")

		status, data, ctype, headers, final, err := j.HTTPXHR("xhr", st.method, st.url, body, st.reqHeaders)
		st.readyState = 4
		if err != nil {
			st.status = 0
			st.statusText = ""
			st.err = err
			j.fireXHR(st, "readystatechange")
			j.fireXHR(st, "error")
			j.fireXHR(st, "loadend")
			return sobek.Undefined()
		}
		st.status = status
		st.statusText = statusText(status)
		st.ctype = ctype
		st.resHeaders = headers
		st.text = j.host.DecodeBody(data, ctype)
		st.response = nil
		if final != nil {
			st.responseURL = final.String()
		} else {
			st.responseURL = st.url.String()
		}
		j.fireXHR(st, "readystatechange")
		j.fireXHR(st, "load")
		j.fireXHR(st, "loadend")
		return sobek.Undefined()
	})
	return o
}

func sameOrigin(a, b *url.URL) bool {
	if a == nil || b == nil || a.Hostname() == "" {
		return false
	}
	if a.Scheme != "http" && a.Scheme != "https" {
		return false
	}
	if a.Scheme != b.Scheme || !strings.EqualFold(a.Hostname(), b.Hostname()) {
		return false
	}
	port := func(u *url.URL) string {
		if p := u.Port(); p != "" {
			return p
		}
		if u.Scheme == "https" {
			return "443"
		}
		return "80"
	}
	return port(a) == port(b)
}

func (j *jsRuntime) fireXHR(st *xhrState, typ string) {
	ev := j.vm.NewObject()
	_ = ev.Set("type", typ)
	_ = ev.Set("target", st.obj)
	_ = ev.Set("currentTarget", st.obj)
	handler := st.obj.Get("on" + typ)
	if handler != nil && !sobek.IsNull(handler) && !sobek.IsUndefined(handler) {
		if fn, ok := sobek.AssertFunction(handler); ok {
			j.call(fn, st.obj, ev)
		}
	}
	for _, l := range st.listeners[typ] {
		if fn, ok := sobek.AssertFunction(l); ok {
			j.call(fn, st.obj, ev)
		}
	}
}

func (j *jsRuntime) HTTPXHR(kind string, method string, u *url.URL, body []byte, reqHeaders map[string]string) (status int, data []byte, ctype string, headers map[string][]string, final *url.URL, err error) {
	if len(body) > maxResourceSize {
		return 0, nil, "", nil, nil, errors.New("request body too large")
	}
	if j.page == nil || j.page.URL == nil || u == nil {
		return 0, nil, "", nil, nil, errors.New("cross-origin request blocked")
	}
	purpose := protocol.FetchXHR
	if kind == "fetch" {
		purpose = protocol.FetchFetch
	}
	req := protocol.FetchRequest{
		URL:         u.String(),
		Method:      method,
		Body:        body,
		Purpose:     purpose,
		Origin:      j.page.URL.String(),
		Credentials: true,
		Kind:        kind,
		PageLocal:   j.page.Local,
	}
	for k, v := range reqHeaders {
		req.Headers = append(req.Headers, protocol.Header{Name: k, Value: v})
	}
	ctx := j.host.RequestContext(kind, j.page.URL, j.page.Local, u)
	// A synchronous fetch blocks the runtime: keep the script watchdog from
	// charging network time to the page and aborting a module mid-evaluation.
	j.ioActive.Store(true)
	resp, err := j.host.JSFetch(ctx, req)
	j.ioActive.Store(false)
	if err != nil {
		return 0, nil, "", nil, nil, err
	}
	headerMap := make(map[string][]string, len(resp.Headers))
	for _, h := range resp.Headers {
		key := canonicalHeaderKey(h.Name)
		headerMap[key] = append(headerMap[key], h.Value)
	}
	if resp.FinalURL != "" {
		if parsed, e := url.Parse(resp.FinalURL); e == nil {
			final = parsed
		}
	}
	if final == nil {
		final = u
	}
	return resp.Status, resp.Body, resp.ContentType, headerMap, final, nil
}

func canonicalHeaderKey(s string) string {
	b := []byte(strings.TrimSpace(s))
	upper := true
	for i, c := range b {
		switch {
		case c >= 'a' && c <= 'z':
			if upper {
				b[i] = c - ('a' - 'A')
			}
		case c == '-':
			upper = true
			continue
		}
		upper = false
	}
	return string(b)
}

func headerGet(h map[string][]string, key string) string {
	vs := h[canonicalHeaderKey(key)]
	if len(vs) == 0 {
		return ""
	}
	return strings.Join(vs, ", ")
}

func statusText(code int) string {
	switch code {
	case 200:
		return "OK"
	case 201:
		return "Created"
	case 202:
		return "Accepted"
	case 204:
		return "No Content"
	case 301:
		return "Moved Permanently"
	case 302:
		return "Found"
	case 303:
		return "See Other"
	case 304:
		return "Not Modified"
	case 307:
		return "Temporary Redirect"
	case 308:
		return "Permanent Redirect"
	case 400:
		return "Bad Request"
	case 401:
		return "Unauthorized"
	case 403:
		return "Forbidden"
	case 404:
		return "Not Found"
	case 405:
		return "Method Not Allowed"
	case 408:
		return "Request Timeout"
	case 409:
		return "Conflict"
	case 410:
		return "Gone"
	case 413:
		return "Payload Too Large"
	case 418:
		return "I'm a teapot"
	case 429:
		return "Too Many Requests"
	case 500:
		return "Internal Server Error"
	case 501:
		return "Not Implemented"
	case 502:
		return "Bad Gateway"
	case 503:
		return "Service Unavailable"
	case 504:
		return "Gateway Timeout"
	}
	return ""
}
