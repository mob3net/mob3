package browser

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/dop251/goja"
)

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
