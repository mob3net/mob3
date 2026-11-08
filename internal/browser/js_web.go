package browser

import (
	"net/url"
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
