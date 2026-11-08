package browser

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/dop251/goja"
)

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
