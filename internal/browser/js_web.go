package browser

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/dop251/goja"
)

// newURL implements a subset of the WHATWG URL object.
func (j *jsRuntime) newURL(input string, base goja.Value) *goja.Object {
	var u *url.URL
	var err error
	if base == nil || goja.IsNull(base) || goja.IsUndefined(base) {
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
		panic(j.vm.NewTypeError("Invalid URL"))
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
	_ = o.Set("toString", j.vm.ToValue(func(goja.FunctionCall) goja.Value { return j.vm.ToValue(u.String()) }))
	_ = o.Set("toJSON", j.vm.ToValue(func(goja.FunctionCall) goja.Value { return j.vm.ToValue(u.String()) }))
	return o
}

func (j *jsRuntime) urlSearchParamsValue(query string) *goja.Object {
	return j.newURLSearchParams(j.vm.ToValue(strings.TrimPrefix(query, "?")))
}

func (j *jsRuntime) newURLSearchParams(init goja.Value) *goja.Object {
	vals := url.Values{}
	if init != nil && !goja.IsNull(init) && !goja.IsUndefined(init) {
		if s, ok := init.Export().(string); ok {
			s = strings.TrimPrefix(s, "?")
			if parsed, err := url.ParseQuery(s); err == nil {
				vals = parsed
			}
		} else if obj, ok := init.(*goja.Object); ok {
			for _, key := range obj.Keys() {
				v := obj.Get(key)
				if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
					continue
				}
				vals.Add(key, v.String())
			}
		}
	}
	o := j.vm.NewObject()
	_ = o.Set("append", j.vm.ToValue(func(call goja.FunctionCall) goja.Value {
		vals.Add(call.Argument(0).String(), call.Argument(1).String())
		return goja.Undefined()
	}))
	_ = o.Set("set", j.vm.ToValue(func(call goja.FunctionCall) goja.Value {
		vals.Set(call.Argument(0).String(), call.Argument(1).String())
		return goja.Undefined()
	}))
	_ = o.Set("delete", j.vm.ToValue(func(call goja.FunctionCall) goja.Value {
		vals.Del(call.Argument(0).String())
		return goja.Undefined()
	}))
	_ = o.Set("has", j.vm.ToValue(func(call goja.FunctionCall) goja.Value {
		_, ok := vals[call.Argument(0).String()]
		return j.vm.ToValue(ok)
	}))
	_ = o.Set("get", j.vm.ToValue(func(call goja.FunctionCall) goja.Value {
		v, ok := vals[call.Argument(0).String()]
		if !ok || len(v) == 0 {
			return goja.Null()
		}
		return j.vm.ToValue(v[0])
	}))
	_ = o.Set("getAll", j.vm.ToValue(func(call goja.FunctionCall) goja.Value {
		v := vals[call.Argument(0).String()]
		arr := make([]interface{}, 0, len(v))
		for _, s := range v {
			arr = append(arr, s)
		}
		return j.vm.NewArray(arr...)
	}))
	_ = o.Set("keys", j.vm.ToValue(func(goja.FunctionCall) goja.Value {
		arr := make([]interface{}, 0, len(vals))
		for k := range vals {
			arr = append(arr, k)
		}
		return j.vm.NewArray(arr...)
	}))
	_ = o.Set("values", j.vm.ToValue(func(goja.FunctionCall) goja.Value {
		var arr []interface{}
		for _, vs := range vals {
			for _, v := range vs {
				arr = append(arr, v)
			}
		}
		return j.vm.NewArray(arr...)
	}))
	_ = o.Set("forEach", j.vm.ToValue(func(call goja.FunctionCall) goja.Value {
		fn, ok := goja.AssertFunction(call.Argument(0))
		if !ok {
			return goja.Undefined()
		}
		for k, vs := range vals {
			for _, v := range vs {
				fn(goja.Undefined(), j.vm.ToValue(v), j.vm.ToValue(k), o)
			}
		}
		return goja.Undefined()
	}))
	_ = o.Set("sort", j.vm.ToValue(func(goja.FunctionCall) goja.Value {
		return goja.Undefined()
	}))
	_ = o.Set("toString", j.vm.ToValue(func(goja.FunctionCall) goja.Value {
		return j.vm.ToValue(vals.Encode())
	}))
	j.accessor(o, "size", func() goja.Value {
		return j.vm.ToValue(len(vals))
	}, nil)
	return o
}

func (j *jsRuntime) storageObject(data map[string]string) *goja.Object {
	o := j.vm.NewObject()
	_ = o.Set("getItem", j.vm.ToValue(func(call goja.FunctionCall) goja.Value {
		if v, ok := data[call.Argument(0).String()]; ok {
			return j.vm.ToValue(v)
		}
		return goja.Null()
	}))
	_ = o.Set("setItem", j.vm.ToValue(func(call goja.FunctionCall) goja.Value {
		data[call.Argument(0).String()] = call.Argument(1).String()
		return goja.Undefined()
	}))
	_ = o.Set("removeItem", j.vm.ToValue(func(call goja.FunctionCall) goja.Value {
		delete(data, call.Argument(0).String())
		return goja.Undefined()
	}))
	_ = o.Set("clear", j.vm.ToValue(func(goja.FunctionCall) goja.Value {
		for k := range data {
			delete(data, k)
		}
		return goja.Undefined()
	}))
	_ = o.Set("key", j.vm.ToValue(func(call goja.FunctionCall) goja.Value {
		i := int(call.Argument(0).ToInteger())
		keys := make([]string, 0, len(data))
		for k := range data {
			keys = append(keys, k)
		}
		if i < 0 || i >= len(keys) {
			return goja.Null()
		}
		return j.vm.ToValue(keys[i])
	}))
	j.accessor(o, "length", func() goja.Value { return j.vm.ToValue(len(data)) }, nil)
	return o
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
		j.reportError(err)
	}
	_ = j.vm.Set("fetch", j.vm.ToValue(j.fetchFunc))
}

func (j *jsRuntime) fetchFunc(call goja.FunctionCall) goja.Value {
	p, resolve, reject := j.vm.NewPromise()
	fail := func(msg string) goja.Value {
		_ = reject(j.vm.NewTypeError(msg))
		return j.vm.ToValue(p)
	}
	failAbort := func() goja.Value {
		_ = reject(j.abortError())
		return j.vm.ToValue(p)
	}
	input := call.Argument(0)
	init := jsObject(call.Argument(1))
	target := ""
	method := http.MethodGet
	body := ""
	headers := map[string][]string{}
	if ro := jsObject(input); ro != nil {
		target = ro.Get("url").String()
		if m := ro.Get("method"); m != nil && !goja.IsUndefined(m) && m.String() != "" {
			method = m.String()
		}
		if h := ro.Get("headers"); h != nil && !goja.IsUndefined(h) && !goja.IsNull(h) {
			headers = j.parseHeaders(h)
		}
		if b := ro.Get("body"); b != nil && !goja.IsUndefined(b) && !goja.IsNull(b) {
			body = b.String()
		}
	} else {
		target = input.String()
	}
	if init != nil {
		if m := init.Get("method"); m != nil && !goja.IsUndefined(m) && m.String() != "" {
			method = m.String()
		}
		if h := init.Get("headers"); h != nil && !goja.IsUndefined(h) && !goja.IsNull(h) {
			headers = j.parseHeaders(h)
		}
		if b := init.Get("body"); b != nil && !goja.IsUndefined(b) && !goja.IsNull(b) {
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
	if j.page == nil || j.page.URL == nil || !sameOrigin(u, j.page.URL) {
		j.b.logJS("error", "fetch blocked: cross-origin request")
		return fail("Cross-origin fetch blocked")
	}
	method = strings.ToUpper(method)
	reqHeaders := make(map[string]string, len(headers))
	for k, vs := range headers {
		if len(vs) > 0 {
			reqHeaders[http.CanonicalHeaderKey(k)] = strings.Join(vs, ", ")
		}
	}
	status, data, ctype, respHeaders, final, err := j.httpXHR(method, u, []byte(body), reqHeaders)
	if err != nil {
		return fail(err.Error())
	}
	finalURL := u.String()
	if final != nil {
		finalURL = final.String()
	}
	_ = resolve(j.responseValue(&fetchResponse{
		status:     status,
		text:       http.StatusText(status),
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
	headers    http.Header
	data       []byte
	redirected bool
}

func (j *jsRuntime) responseValue(st *fetchResponse) *goja.Object {
	o := j.vm.NewObject()
	used := false
	_ = o.Set("status", st.status)
	_ = o.Set("statusText", st.text)
	_ = o.Set("ok", st.status >= 200 && st.status <= 299)
	_ = o.Set("url", st.url)
	_ = o.Set("redirected", st.redirected)
	_ = o.Set("type", "basic")
	_ = o.Set("headers", j.headersValue(st.headers))
	j.accessor(o, "bodyUsed", func() goja.Value { return j.vm.ToValue(used) }, nil)
	bodyText := decodeBody(st.data, st.ctype)
	consume := func() bool {
		if used {
			return false
		}
		used = true
		return true
	}
	j.method(o, "text", func(goja.FunctionCall) goja.Value {
		if !consume() {
			return j.rejectedPromise(j.vm.NewTypeError("Body has already been consumed"))
		}
		return j.resolvedPromise(bodyText)
	})
	j.method(o, "json", func(goja.FunctionCall) goja.Value {
		if !consume() {
			return j.rejectedPromise(j.vm.NewTypeError("Body has already been consumed"))
		}
		var v interface{}
		if err := json.Unmarshal(st.data, &v); err != nil {
			return j.rejectedPromise(j.vm.NewTypeError("Invalid JSON: " + err.Error()))
		}
		return j.resolvedPromise(j.vm.ToValue(v))
	})
	j.method(o, "arrayBuffer", func(goja.FunctionCall) goja.Value {
		if !consume() {
			return j.rejectedPromise(j.vm.NewTypeError("Body has already been consumed"))
		}
		return j.resolvedPromise(j.vm.NewArrayBuffer(append([]byte(nil), st.data...)))
	})
	j.method(o, "blob", func(goja.FunctionCall) goja.Value {
		if !consume() {
			return j.rejectedPromise(j.vm.NewTypeError("Body has already been consumed"))
		}
		blob := j.vm.NewObject()
		_ = blob.Set("type", st.ctype)
		_ = blob.Set("size", len(st.data))
		j.method(blob, "text", func(goja.FunctionCall) goja.Value {
			return j.resolvedPromise(bodyText)
		})
		j.method(blob, "arrayBuffer", func(goja.FunctionCall) goja.Value {
			return j.resolvedPromise(j.vm.NewArrayBuffer(append([]byte(nil), st.data...)))
		})
		return j.resolvedPromise(blob)
	})
	j.method(o, "clone", func(goja.FunctionCall) goja.Value {
		cp := *st
		cp.data = append([]byte(nil), st.data...)
		return j.responseValue(&cp)
	})
	return o
}

func (j *jsRuntime) resolvedPromise(v interface{}) goja.Value {
	p, resolve, _ := j.vm.NewPromise()
	_ = resolve(v)
	return j.vm.ToValue(p)
}

func (j *jsRuntime) rejectedPromise(v interface{}) goja.Value {
	p, _, reject := j.vm.NewPromise()
	_ = reject(v)
	return j.vm.ToValue(p)
}

func (j *jsRuntime) headersValue(h http.Header) *goja.Object {
	m := make(map[string][]string, len(h))
	for k, vs := range h {
		m[strings.ToLower(k)] = append([]string(nil), vs...)
	}
	return j.headersObject(m)
}

func (j *jsRuntime) headersObject(m map[string][]string) *goja.Object {
	o := j.vm.NewObject()
	j.headerObjs[o] = m
	lower := func(v goja.Value) string { return strings.ToLower(strings.TrimSpace(v.String())) }
	j.method(o, "get", func(call goja.FunctionCall) goja.Value {
		vs := m[lower(call.Argument(0))]
		if len(vs) == 0 {
			return goja.Null()
		}
		return j.vm.ToValue(strings.Join(vs, ", "))
	})
	j.method(o, "has", func(call goja.FunctionCall) goja.Value {
		_, ok := m[lower(call.Argument(0))]
		return j.vm.ToValue(ok)
	})
	j.method(o, "set", func(call goja.FunctionCall) goja.Value {
		m[lower(call.Argument(0))] = []string{call.Argument(1).String()}
		return goja.Undefined()
	})
	j.method(o, "append", func(call goja.FunctionCall) goja.Value {
		k := lower(call.Argument(0))
		m[k] = append(m[k], call.Argument(1).String())
		return goja.Undefined()
	})
	j.method(o, "delete", func(call goja.FunctionCall) goja.Value {
		delete(m, lower(call.Argument(0)))
		return goja.Undefined()
	})
	j.method(o, "getSetCookie", func(goja.FunctionCall) goja.Value {
		return j.vm.ToValue(m["set-cookie"])
	})
	j.method(o, "forEach", func(call goja.FunctionCall) goja.Value {
		fn, ok := goja.AssertFunction(call.Argument(0))
		if !ok {
			return goja.Undefined()
		}
		for _, k := range sortedHeaderKeys(m) {
			j.call(fn, o, j.vm.ToValue(strings.Join(m[k], ", ")), j.vm.ToValue(k), o)
		}
		return goja.Undefined()
	})
	j.method(o, "keys", func(goja.FunctionCall) goja.Value {
		keys := sortedHeaderKeys(m)
		return j.vm.ToValue(keys)
	})
	j.method(o, "values", func(goja.FunctionCall) goja.Value {
		var vals []string
		for _, k := range sortedHeaderKeys(m) {
			vals = append(vals, strings.Join(m[k], ", "))
		}
		return j.vm.ToValue(vals)
	})
	j.method(o, "entries", func(goja.FunctionCall) goja.Value {
		out := j.vm.NewArray()
		for i, k := range sortedHeaderKeys(m) {
			_ = out.Set(strconv.Itoa(i), j.vm.NewArray(k, strings.Join(m[k], ", ")))
		}
		return out
	})
	return o
}

func jsArrayLength(o *goja.Object) int {
	if o == nil {
		return 0
	}
	v := o.Get("length")
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
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

func (j *jsRuntime) newHeadersObject(call goja.FunctionCall) goja.Value {
	return j.headersObject(j.parseHeaders(call.Argument(0)))
}

func (j *jsRuntime) parseHeaders(v goja.Value) map[string][]string {
	out := make(map[string][]string)
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return out
	}
	if o, ok := v.(*goja.Object); ok {
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
				key := strings.ToLower(strings.TrimSpace(pair.Get("0").String()))
				if key != "" {
					out[key] = append(out[key], pair.Get("1").String())
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

func (j *jsRuntime) newRequestObject(call goja.FunctionCall) goja.Value {
	input := call.Argument(0)
	init := jsObject(call.Argument(1))
	target := input.String()
	method := http.MethodGet
	body := ""
	headers := map[string][]string{}
	if ro := jsObject(input); ro != nil {
		if v := ro.Get("url"); v != nil && !goja.IsUndefined(v) {
			target = v.String()
		}
		if v := ro.Get("method"); v != nil && !goja.IsUndefined(v) && v.String() != "" {
			method = v.String()
		}
		if v := ro.Get("headers"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
			headers = j.parseHeaders(v)
		}
		if v := ro.Get("body"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
			body = v.String()
		}
	}
	if init != nil {
		if v := init.Get("method"); v != nil && !goja.IsUndefined(v) && v.String() != "" {
			method = v.String()
		}
		if v := init.Get("headers"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
			headers = j.parseHeaders(v)
		}
		if v := init.Get("body"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
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
	j.method(o, "clone", func(goja.FunctionCall) goja.Value {
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

func (j *jsRuntime) newResponseObject(call goja.FunctionCall) goja.Value {
	init := jsObject(call.Argument(1))
	status := 200
	statusText := ""
	headers := map[string][]string{}
	if init != nil {
		if v := init.Get("status"); v != nil && !goja.IsUndefined(v) && v.ToInteger() != 0 {
			status = int(v.ToInteger())
		}
		statusText = jsOptionString(init, "statusText", "")
		if v := init.Get("headers"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
			headers = j.parseHeaders(v)
		}
	}
	data := []byte(call.Argument(0).String())
	if goja.IsUndefined(call.Argument(0)) || goja.IsNull(call.Argument(0)) {
		data = nil
	}
	h := make(http.Header)
	for k, vs := range headers {
		h[http.CanonicalHeaderKey(k)] = vs
	}
	ctype := h.Get("Content-Type")
	return j.responseValue(&fetchResponse{
		status:  status,
		text:    statusText,
		ctype:   ctype,
		headers: h,
		data:    data,
	})
}

type xhrState struct {
	obj          *goja.Object
	method       string
	url          *url.URL
	reqHeaders   map[string]string
	resHeaders   http.Header
	status       int
	statusText   string
	readyState   int
	text         string
	response     goja.Value
	ctype        string
	responseURL  string
	err          error
	responseType string
	listeners    map[string][]goja.Value
	blocked      bool
}

func (j *jsRuntime) xmlHTTPRequestObject() *goja.Object {
	o := j.vm.NewObject()
	st := &xhrState{obj: o, reqHeaders: make(map[string]string), listeners: make(map[string][]goja.Value), responseType: ""}
	get := func() goja.Value {
		if st.responseType == "json" {
			if st.response != nil {
				return st.response
			}
			var v interface{}
			if err := json.Unmarshal([]byte(st.text), &v); err == nil {
				st.response = j.vm.ToValue(v)
			} else {
				st.response = goja.Null()
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
	j.accessor(o, "readyState", func() goja.Value { return j.vm.ToValue(st.readyState) }, nil)
	j.accessor(o, "status", func() goja.Value { return j.vm.ToValue(st.status) }, nil)
	j.accessor(o, "statusText", func() goja.Value { return j.vm.ToValue(st.statusText) }, nil)
	j.accessor(o, "responseText", func() goja.Value { return j.vm.ToValue(st.text) }, nil)
	j.accessor(o, "response", func() goja.Value { return get() }, nil)
	j.accessor(o, "responseURL", func() goja.Value { return j.vm.ToValue(st.responseURL) }, nil)
	j.accessor(o, "responseType", func() goja.Value { return j.vm.ToValue(st.responseType) }, func(v goja.Value) {
		st.responseType = v.String()
		st.response = nil
	})
	j.accessor(o, "timeout", func() goja.Value { return j.vm.ToValue(0) }, func(goja.Value) {})
	j.accessor(o, "withCredentials", func() goja.Value { return j.vm.ToValue(false) }, func(goja.Value) {})
	j.accessor(o, "upload", func() goja.Value { return j.vm.NewObject() }, nil)

	j.method(o, "open", func(call goja.FunctionCall) goja.Value {
		st.method = strings.ToUpper(call.Argument(0).String())
		if st.method == "" {
			st.method = http.MethodGet
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
		st.blocked = st.url == nil || j.page == nil || j.page.URL == nil ||
			!sameOrigin(st.url, j.page.URL)
		st.readyState = 1
		j.fireXHR(st, "readystatechange")
		return goja.Undefined()
	})
	j.method(o, "setRequestHeader", func(call goja.FunctionCall) goja.Value {
		st.reqHeaders[call.Argument(0).String()] = call.Argument(1).String()
		return goja.Undefined()
	})
	j.method(o, "getResponseHeader", func(call goja.FunctionCall) goja.Value {
		if st.resHeaders == nil {
			return goja.Null()
		}
		v := st.resHeaders.Get(call.Argument(0).String())
		if v == "" {
			return goja.Null()
		}
		return j.vm.ToValue(v)
	})
	j.method(o, "getAllResponseHeaders", func(goja.FunctionCall) goja.Value {
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
	j.method(o, "overrideMimeType", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
	j.method(o, "abort", func(goja.FunctionCall) goja.Value {
		st.readyState = 0
		j.fireXHR(st, "abort")
		return goja.Undefined()
	})
	j.method(o, "addEventListener", func(call goja.FunctionCall) goja.Value {
		typ := call.Argument(0).String()
		st.listeners[typ] = append(st.listeners[typ], call.Argument(1))
		return goja.Undefined()
	})
	j.method(o, "removeEventListener", func(call goja.FunctionCall) goja.Value {
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
		return goja.Undefined()
	})
	j.method(o, "send", func(call goja.FunctionCall) goja.Value {
		if st.url == nil || st.blocked {
			j.b.logJS("error", "XMLHttpRequest blocked: cross-origin request")
			st.readyState = 4
			st.status = 0
			st.err = errBadSVG
			j.fireXHR(st, "readystatechange")
			j.fireXHR(st, "error")
			j.fireXHR(st, "loadend")
			return goja.Undefined()
		}
		var body []byte
		if len(call.Arguments) > 0 && !goja.IsNull(call.Argument(0)) && !goja.IsUndefined(call.Argument(0)) {
			body = []byte(call.Argument(0).String())
		}
		st.readyState = 2
		j.fireXHR(st, "readystatechange")
		st.readyState = 3
		j.fireXHR(st, "readystatechange")

		status, data, ctype, headers, final, err := j.httpXHR(st.method, st.url, body, st.reqHeaders)
		st.readyState = 4
		if err != nil {
			st.status = 0
			st.statusText = ""
			st.err = err
			j.fireXHR(st, "readystatechange")
			j.fireXHR(st, "error")
			j.fireXHR(st, "loadend")
			return goja.Undefined()
		}
		st.status = status
		st.statusText = http.StatusText(status)
		st.ctype = ctype
		st.resHeaders = headers
		st.text = decodeBody(data, ctype)
		st.response = nil
		if final != nil {
			st.responseURL = final.String()
		} else {
			st.responseURL = st.url.String()
		}
		j.fireXHR(st, "readystatechange")
		j.fireXHR(st, "load")
		j.fireXHR(st, "loadend")
		return goja.Undefined()
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
	if handler != nil && !goja.IsNull(handler) && !goja.IsUndefined(handler) {
		if fn, ok := goja.AssertFunction(handler); ok {
			j.call(fn, st.obj, ev)
		}
	}
	for _, l := range st.listeners[typ] {
		if fn, ok := goja.AssertFunction(l); ok {
			j.call(fn, st.obj, ev)
		}
	}
}

func (j *jsRuntime) httpXHR(method string, u *url.URL, body []byte, reqHeaders map[string]string) (status int, data []byte, ctype string, headers http.Header, final *url.URL, err error) {
	if len(body) > maxResourceSize {
		return 0, nil, "", nil, nil, errBadSVG
	}
	origin := j.page.URL
	if origin == nil || !sameOrigin(u, origin) {
		return 0, nil, "", nil, nil, errors.New("cross-origin request blocked")
	}
	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, u.String(), reader)
	if err != nil {
		return 0, nil, "", nil, nil, err
	}
	req.Header.Set("User-Agent", j.b.loader.userAgent)
	req.Header.Set("Accept", "*/*")
	for k, v := range reqHeaders {
		req.Header.Set(k, v)
	}
	client := *j.b.loader.client
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if !sameOrigin(next.URL, origin) {
			return errors.New("cross-origin redirect blocked")
		}
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		return nil
	}
	j.dialogActive.Store(true)
	resp, err := client.Do(req)
	j.dialogActive.Store(false)
	j.vm.ClearInterrupt()
	if err != nil {
		return 0, nil, "", nil, nil, err
	}
	defer resp.Body.Close()
	data, err = io.ReadAll(io.LimitReader(resp.Body, maxResourceSize))
	if err != nil {
		return resp.StatusCode, nil, "", resp.Header, resp.Request.URL, err
	}
	return resp.StatusCode, data, resp.Header.Get("Content-Type"), resp.Header, resp.Request.URL, nil
}
