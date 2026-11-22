package engine

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/sobek"
)

// blobData is the byte payload registered for a Blob/File wrapper.
type blobData struct {
	data []byte
	typ  string
}

// installCompat registers the cheap browser-compatibility tier: text codecs,
// crypto, structuredClone, AbortController, Blob/File/FileReader and FormData.
func (j *jsRuntime) installCompat() {
	j.installCrypto()
	j.installStructuredClone()
	j.installURLStatics()
	j.installBase64()
	_ = j.vm.Set("queueMicrotask", j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		return j.setTimer(sobek.FunctionCall{Arguments: []sobek.Value{call.Argument(0), j.vm.ToValue(0)}}, false)
	}))
	_ = j.vm.Set("getSelection", j.vm.ToValue(func(sobek.FunctionCall) sobek.Value {
		return j.selectionObject()
	}))
	j.installCompatConstructors()
	j.installPlatformStubs()
}

// installCompatConstructors defines JS constructor wrappers around the Go
// factories so `new TextEncoder()`, `new Blob(...)` and friends work.
func (j *jsRuntime) installCompatConstructors() {
	factories := map[string]func(sobek.FunctionCall) sobek.Value{
		"__mob3NewTextEncoder":     func(sobek.FunctionCall) sobek.Value { return j.newTextEncoder() },
		"__mob3NewTextDecoder":     func(call sobek.FunctionCall) sobek.Value { return j.newTextDecoder(call.Argument(0).String()) },
		"__mob3NewAbortController": func(sobek.FunctionCall) sobek.Value { return j.newAbortController() },
		"__mob3NewAbortSignal":     func(sobek.FunctionCall) sobek.Value { return j.newAbortController().Get("signal") },
		"__mob3NewBlob": func(call sobek.FunctionCall) sobek.Value {
			return j.newBlobObject(call.Argument(0), call.Argument(1), "", 0)
		},
		"__mob3NewFile": func(call sobek.FunctionCall) sobek.Value {
			return j.newBlobObject(call.Argument(1), call.Argument(2), call.Argument(0).String(), time.Now().UnixMilli())
		},
		"__mob3NewFileReader": func(sobek.FunctionCall) sobek.Value { return j.newFileReader() },
		"__mob3NewFormData":   func(call sobek.FunctionCall) sobek.Value { return j.newFormData(call.Argument(0)) },
		"__mob3NewRange":      func(sobek.FunctionCall) sobek.Value { r, _ := j.newRange(); return r },
		"__mob3NewSelection":  func(sobek.FunctionCall) sobek.Value { return j.selectionObject() },
	}
	for name, fn := range factories {
		_ = j.vm.Set(name, j.vm.ToValue(fn))
	}
	const script = `
(function () {
	function make(name, factory) {
		var impl = globalThis[factory];
		var f = function () { return impl.apply(null, arguments); };
		globalThis[name] = f;
		return f;
	}
	make("TextEncoder", "__mob3NewTextEncoder");
	make("TextDecoder", "__mob3NewTextDecoder");
	make("AbortController", "__mob3NewAbortController");
	make("AbortSignal", "__mob3NewAbortSignal");
	make("Blob", "__mob3NewBlob");
	make("File", "__mob3NewFile");
	make("FileReader", "__mob3NewFileReader");
	make("FormData", "__mob3NewFormData");
	make("Range", "__mob3NewRange");
	make("Selection", "__mob3NewSelection");
	for (var name in { __mob3NewTextEncoder: 1, __mob3NewTextDecoder: 1,
		__mob3NewAbortController: 1, __mob3NewAbortSignal: 1, __mob3NewBlob: 1,
		__mob3NewFile: 1, __mob3NewFileReader: 1, __mob3NewFormData: 1,
		__mob3NewRange: 1, __mob3NewSelection: 1 }) {
		delete globalThis[name];
	}
})();
`
	if _, err := j.vm.RunScript("compat constructors", script); err != nil {
		j.ReportError(err)
		return
	}
	if r, ok := j.vm.Get("Range").(*sobek.Object); ok {
		_ = r.Set("START_TO_START", 0)
		_ = r.Set("START_TO_END", 1)
		_ = r.Set("END_TO_END", 2)
		_ = r.Set("END_TO_START", 3)
	}
	if sigCtor, ok := j.vm.Get("AbortSignal").(*sobek.Object); ok {
		j.method(sigCtor, "abort", func(call sobek.FunctionCall) sobek.Value {
			ctrl := j.newAbortController()
			if abort, ok := sobek.AssertFunction(ctrl.Get("abort")); ok {
				_, _ = abort(ctrl, call.Argument(0))
			}
			return ctrl.Get("signal")
		})
	}
}

// installBase64 provides the window btoa/atob binary-string helpers used by
// many modal and image scripts.
func (j *jsRuntime) installBase64() {
	_ = j.vm.Set("btoa", j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		s := call.Argument(0).String()
		buf := make([]byte, 0, len(s))
		for _, r := range s {
			if r > 0xff {
				panic(j.vm.NewTypeError("btoa: character out of Latin-1 range"))
			}
			buf = append(buf, byte(r))
		}
		return j.vm.ToValue(base64.StdEncoding.EncodeToString(buf))
	}))
	_ = j.vm.Set("atob", j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		s := strings.Map(func(r rune) rune {
			switch r {
			case ' ', '\t', '\n', '\r', '\f':
				return -1
			}
			return r
		}, call.Argument(0).String())
		if len(s)%4 == 0 {
			if strings.HasSuffix(s, "==") {
				s = s[:len(s)-2]
			} else if strings.HasSuffix(s, "=") {
				s = s[:len(s)-1]
			}
		}
		switch len(s) % 4 {
		case 1:
			panic(j.vm.NewTypeError("atob: invalid base64 length"))
		case 2:
			s += "=="
		case 3:
			s += "="
		}
		data, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			panic(j.vm.NewTypeError("atob: invalid base64 data"))
		}
		var out strings.Builder
		out.Grow(len(data))
		for _, b := range data {
			out.WriteRune(rune(b))
		}
		return j.vm.ToValue(out.String())
	}))
}

func (j *jsRuntime) newTextEncoder() *sobek.Object {
	o := j.vm.NewObject()
	_ = o.Set("encoding", "utf-8")
	j.method(o, "encode", func(call sobek.FunctionCall) sobek.Value {
		return j.byteArrayValue([]byte(call.Argument(0).String()))
	})
	return o
}

func (j *jsRuntime) newTextDecoder(label string) *sobek.Object {
	o := j.vm.NewObject()
	label = strings.ToLower(strings.TrimSpace(label))
	if label == "" {
		label = "utf-8"
	}
	_ = o.Set("encoding", label)
	_ = o.Set("fatal", false)
	_ = o.Set("ignoreBOM", false)
	j.method(o, "decode", func(call sobek.FunctionCall) sobek.Value {
		return j.vm.ToValue(string(valueBytes(call.Argument(0))))
	})
	return o
}

func (j *jsRuntime) byteArrayValue(data []byte) sobek.Value {
	if ctor, ok := j.vm.Get("Uint8Array").(*sobek.Object); ok {
		if o, err := j.vm.New(ctor, j.vm.ToValue(len(data))); err == nil {
			for i, b := range data {
				_ = o.Set(strconv.Itoa(i), int(b))
			}
			return o
		}
	}
	return j.vm.ToValue(string(data))
}

// valueBytes copies a string, ArrayBuffer, typed array or Blob payload.
func valueBytes(v sobek.Value) []byte {
	if v == nil || sobek.IsUndefined(v) || sobek.IsNull(v) {
		return nil
	}
	switch x := v.Export().(type) {
	case string:
		return []byte(x)
	case []byte:
		return append([]byte(nil), x...)
	case sobek.ArrayBuffer:
		return append([]byte(nil), x.Bytes()...)
	}
	o := jsObject(v)
	if o == nil {
		return []byte(v.String())
	}
	if length := o.Get("length"); length != nil && !sobek.IsUndefined(length) {
		n := int(length.ToInteger())
		if n > 0 {
			out := make([]byte, n)
			for i := range out {
				out[i] = byte(o.Get(strconv.Itoa(i)).ToInteger())
			}
			return out
		}
	}
	return []byte(v.String())
}

func (j *jsRuntime) installCrypto() {
	crypto := j.vm.NewObject()
	j.method(crypto, "getRandomValues", func(call sobek.FunctionCall) sobek.Value {
		arr := jsObject(call.Argument(0))
		if arr == nil {
			return call.Argument(0)
		}
		n := int(arr.Get("length").ToInteger())
		if n < 0 {
			n = 0
		}
		buf := make([]byte, n)
		_, _ = rand.Read(buf)
		for i, b := range buf {
			_ = arr.Set(strconv.Itoa(i), int(b))
		}
		return arr
	})
	j.method(crypto, "randomUUID", func(sobek.FunctionCall) sobek.Value {
		var b [16]byte
		_, _ = rand.Read(b[:])
		b[6] = (b[6] & 0x0f) | 0x40
		b[8] = (b[8] & 0x3f) | 0x80
		return j.vm.ToValue(fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]))
	})
	_ = j.vm.Set("crypto", crypto)
}

func (j *jsRuntime) installStructuredClone() {
	_ = j.vm.Set("structuredClone", j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		v, err := j.cloneValue(call.Argument(0), make(map[*sobek.Object]sobek.Value))
		if err != nil {
			panic(j.vm.NewTypeError(err.Error()))
		}
		return v
	}))
}

func (j *jsRuntime) cloneValue(v sobek.Value, seen map[*sobek.Object]sobek.Value) (sobek.Value, error) {
	if v == nil || sobek.IsUndefined(v) || sobek.IsNull(v) {
		return v, nil
	}
	o, ok := v.(*sobek.Object)
	if !ok {
		return v, nil
	}
	if _, ok := sobek.AssertFunction(o); ok {
		return nil, fmt.Errorf("%s could not be cloned", o.ClassName())
	}
	if c, ok := seen[o]; ok {
		return c, nil
	}
	switch o.ClassName() {
	case "Array":
		out := j.vm.NewArray()
		seen[o] = out
		for _, k := range o.Keys() {
			child, err := j.cloneValue(o.Get(k), seen)
			if err != nil {
				return nil, err
			}
			_ = out.Set(k, child)
		}
		return out, nil
	case "Date":
		if t, ok := o.Export().(time.Time); ok {
			out := j.vm.ToValue(t)
			seen[o] = out
			return out, nil
		}
	case "Map":
		if m, ok := j.buildCloneMap(o, seen); ok {
			return m, nil
		}
	case "Set":
		if s, ok := j.buildCloneSet(o, seen); ok {
			return s, nil
		}
	case "ArrayBuffer":
		if ab, ok := o.Export().(sobek.ArrayBuffer); ok {
			out := j.vm.ToValue(j.vm.NewArrayBuffer(append([]byte(nil), ab.Bytes()...)))
			seen[o] = out
			return out, nil
		}
	default:
		if strings.HasSuffix(o.ClassName(), "Array") {
			n := int(o.Get("length").ToInteger())
			out := j.vm.NewArray()
			seen[o] = out
			for i := 0; i < n; i++ {
				_ = out.Set(strconv.Itoa(i), o.Get(strconv.Itoa(i)))
			}
			return out, nil
		}
	}
	out := j.vm.NewObject()
	seen[o] = out
	for _, k := range o.Keys() {
		child, err := j.cloneValue(o.Get(k), seen)
		if err != nil {
			return nil, err
		}
		_ = out.Set(k, child)
	}
	return out, nil
}

func (j *jsRuntime) buildCloneMap(o *sobek.Object, seen map[*sobek.Object]sobek.Value) (sobek.Value, bool) {
	ctor, ok := j.vm.Get("Map").(*sobek.Object)
	if !ok {
		return nil, false
	}
	out, err := j.vm.New(ctor)
	if err != nil {
		return nil, false
	}
	seen[o] = out
	set, ok := sobek.AssertFunction(out.Get("set"))
	if !ok {
		return nil, false
	}
	forEach, ok := sobek.AssertFunction(o.Get("forEach"))
	if !ok {
		return nil, false
	}
	var cloneErr error
	_, _ = forEach(o, sobek.Undefined(), j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		k, err := j.cloneValue(call.Argument(1), seen)
		if err != nil {
			cloneErr = err
			return sobek.Undefined()
		}
		val, err := j.cloneValue(call.Argument(0), seen)
		if err != nil {
			cloneErr = err
			return sobek.Undefined()
		}
		_, _ = set(out, out, k, val)
		return sobek.Undefined()
	}))
	if cloneErr != nil {
		return nil, false
	}
	return out, true
}

func (j *jsRuntime) buildCloneSet(o *sobek.Object, seen map[*sobek.Object]sobek.Value) (sobek.Value, bool) {
	ctor, ok := j.vm.Get("Set").(*sobek.Object)
	if !ok {
		return nil, false
	}
	out, err := j.vm.New(ctor)
	if err != nil {
		return nil, false
	}
	seen[o] = out
	add, ok := sobek.AssertFunction(out.Get("add"))
	if !ok {
		return nil, false
	}
	forEach, ok := sobek.AssertFunction(o.Get("forEach"))
	if !ok {
		return nil, false
	}
	var cloneErr error
	_, _ = forEach(o, sobek.Undefined(), j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		val, err := j.cloneValue(call.Argument(0), seen)
		if err != nil {
			cloneErr = err
			return sobek.Undefined()
		}
		_, _ = add(out, out, val)
		return sobek.Undefined()
	}))
	if cloneErr != nil {
		return nil, false
	}
	return out, true
}

func (j *jsRuntime) abortError() sobek.Value {
	o := j.vm.NewObject()
	_ = o.Set("name", "AbortError")
	_ = o.Set("message", "The operation was aborted")
	_ = o.Set("code", 20)
	return o
}

func (j *jsRuntime) newAbortController() *sobek.Object {
	ctrl := j.vm.NewObject()
	sig := j.vm.NewObject()
	var listeners []sobek.Value
	aborted := false
	var reason sobek.Value = sobek.Undefined()
	fire := func() {
		ev := j.vm.NewObject()
		_ = ev.Set("type", "abort")
		_ = ev.Set("target", sig)
		if h := sig.Get("onabort"); h != nil && !sobek.IsNull(h) && !sobek.IsUndefined(h) {
			if fn, ok := sobek.AssertFunction(h); ok {
				j.call(fn, sig, ev)
			}
		}
		for _, l := range listeners {
			if fn, ok := sobek.AssertFunction(l); ok {
				j.call(fn, sig, ev)
			}
		}
	}
	_ = sig.Set("aborted", false)
	_ = sig.Set("reason", sobek.Undefined())
	_ = sig.Set("onabort", sobek.Null())
	j.method(sig, "addEventListener", func(call sobek.FunctionCall) sobek.Value {
		if call.Argument(0).String() == "abort" {
			listeners = append(listeners, call.Argument(1))
		}
		return sobek.Undefined()
	})
	j.method(sig, "removeEventListener", func(call sobek.FunctionCall) sobek.Value {
		want := call.Argument(1)
		kept := listeners[:0]
		for _, l := range listeners {
			if l != want {
				kept = append(kept, l)
			}
		}
		listeners = kept
		return sobek.Undefined()
	})
	j.method(sig, "throwIfAborted", func(sobek.FunctionCall) sobek.Value {
		if aborted {
			panic(j.vm.NewTypeError("The operation was aborted"))
		}
		return sobek.Undefined()
	})
	j.method(ctrl, "abort", func(call sobek.FunctionCall) sobek.Value {
		if aborted {
			return sobek.Undefined()
		}
		aborted = true
		reason = call.Argument(0)
		if reason == nil || sobek.IsUndefined(reason) || sobek.IsNull(reason) {
			reason = j.abortError()
		}
		_ = sig.Set("aborted", true)
		_ = sig.Set("reason", reason)
		fire()
		return sobek.Undefined()
	})
	_ = ctrl.Set("signal", sig)
	return ctrl
}

func (j *jsRuntime) installURLStatics() {
	u, ok := j.vm.Get("URL").(*sobek.Object)
	if !ok {
		return
	}
	j.method(u, "createObjectURL", func(call sobek.FunctionCall) sobek.Value {
		blob := jsObject(call.Argument(0))
		if blob == nil {
			return j.vm.ToValue("")
		}
		j.blobSeq++
		id := "blob:mob3/" + strconv.FormatInt(j.blobSeq, 10)
		j.blobURLs[id] = blob
		return j.vm.ToValue(id)
	})
	j.method(u, "revokeObjectURL", func(call sobek.FunctionCall) sobek.Value {
		delete(j.blobURLs, call.Argument(0).String())
		return sobek.Undefined()
	})
}

func (j *jsRuntime) newBlobObject(parts, opts sobek.Value, name string, lastModified int64) *sobek.Object {
	data := j.blobParts(parts)
	typ := ""
	if o := jsObject(opts); o != nil {
		if t := o.Get("type"); t != nil && !sobek.IsUndefined(t) && !sobek.IsNull(t) {
			typ = t.String()
		}
	}
	return j.newBlobFromData(data, typ, name, lastModified)
}

func (j *jsRuntime) newBlobFromData(data []byte, typ, name string, lastModified int64) *sobek.Object {
	o := j.vm.NewObject()
	_ = o.Set("size", len(data))
	_ = o.Set("type", typ)
	if name != "" {
		_ = o.Set("name", name)
		_ = o.Set("lastModified", lastModified)
	}
	j.blobs[o] = &blobData{data: data, typ: typ}
	j.method(o, "slice", func(call sobek.FunctionCall) sobek.Value {
		start := int(call.Argument(0).ToInteger())
		end := len(data)
		if len(call.Arguments) > 1 && !sobek.IsUndefined(call.Argument(1)) {
			end = int(call.Argument(1).ToInteger())
		}
		if start < 0 {
			start = len(data) + start
		}
		if end < 0 {
			end = len(data) + end
		}
		start = clampInt(start, 0, len(data))
		end = clampInt(end, start, len(data))
		sliceType := typ
		if t := call.Argument(2); t != nil && !sobek.IsUndefined(t) && !sobek.IsNull(t) {
			sliceType = t.String()
		}
		return j.newBlobFromData(append([]byte(nil), data[start:end]...), sliceType, "", 0)
	})
	j.method(o, "text", func(sobek.FunctionCall) sobek.Value {
		return j.resolvedPromise(string(data))
	})
	j.method(o, "arrayBuffer", func(sobek.FunctionCall) sobek.Value {
		return j.resolvedPromise(j.vm.NewArrayBuffer(append([]byte(nil), data...)))
	})
	return o
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (j *jsRuntime) blobParts(parts sobek.Value) []byte {
	if parts == nil || sobek.IsUndefined(parts) || sobek.IsNull(parts) {
		return nil
	}
	o := jsObject(parts)
	if o == nil {
		switch x := parts.Export().(type) {
		case string:
			return []byte(x)
		case int64:
			return []byte{byte(x)}
		case int:
			return []byte{byte(x)}
		case float64:
			return []byte{byte(x)}
		}
		return []byte(parts.String())
	}
	if blob, ok := j.blobs[o]; ok {
		return append([]byte(nil), blob.data...)
	}
	if ab, ok := o.Export().(sobek.ArrayBuffer); ok {
		return append([]byte(nil), ab.Bytes()...)
	}
	var out []byte
	if length := o.Get("length"); length != nil && !sobek.IsUndefined(length) {
		n := int(length.ToInteger())
		for i := 0; i < n; i++ {
			out = append(out, j.blobParts(o.Get(strconv.Itoa(i)))...)
		}
		return out
	}
	return valueBytes(o)
}

func (j *jsRuntime) blobBytes(v sobek.Value) ([]byte, string, bool) {
	o := jsObject(v)
	if o == nil {
		return nil, "", false
	}
	if blob, ok := j.blobs[o]; ok {
		return append([]byte(nil), blob.data...), blob.typ, true
	}
	return nil, "", false
}

func (j *jsRuntime) newFileReader() *sobek.Object {
	o := j.vm.NewObject()
	listeners := make(map[string][]sobek.Value)
	_ = o.Set("readyState", 0)
	_ = o.Set("result", sobek.Null())
	_ = o.Set("error", sobek.Null())
	_ = o.Set("onload", sobek.Null())
	_ = o.Set("onerror", sobek.Null())
	_ = o.Set("onloadend", sobek.Null())
	_ = o.Set("onabort", sobek.Null())
	fire := func(typ string) {
		ev := j.vm.NewObject()
		_ = ev.Set("type", typ)
		_ = ev.Set("target", o)
		if h := o.Get("on" + typ); h != nil && !sobek.IsNull(h) && !sobek.IsUndefined(h) {
			if fn, ok := sobek.AssertFunction(h); ok {
				j.call(fn, o, ev)
			}
		}
		for _, l := range listeners[typ] {
			if fn, ok := sobek.AssertFunction(l); ok {
				j.call(fn, o, ev)
			}
		}
	}
	j.method(o, "addEventListener", func(call sobek.FunctionCall) sobek.Value {
		typ := call.Argument(0).String()
		listeners[typ] = append(listeners[typ], call.Argument(1))
		return sobek.Undefined()
	})
	j.method(o, "removeEventListener", func(call sobek.FunctionCall) sobek.Value {
		typ := call.Argument(0).String()
		want := call.Argument(1)
		kept := listeners[typ][:0]
		for _, l := range listeners[typ] {
			if l != want {
				kept = append(kept, l)
			}
		}
		listeners[typ] = kept
		return sobek.Undefined()
	})
	finish := func(result sobek.Value) {
		_ = o.Set("result", result)
		_ = o.Set("readyState", 2)
		j.setTimer(sobek.FunctionCall{Arguments: []sobek.Value{j.vm.ToValue(func(sobek.FunctionCall) sobek.Value {
			fire("load")
			fire("loadend")
			return sobek.Undefined()
		}), j.vm.ToValue(0)}}, false)
	}
	read := func(call sobek.FunctionCall, mode string) sobek.Value {
		data, typ, ok := j.blobBytes(call.Argument(0))
		if !ok {
			_ = o.Set("error", j.vm.NewTypeError("The argument is not a Blob"))
			_ = o.Set("readyState", 2)
			fire("error")
			fire("loadend")
			return sobek.Undefined()
		}
		switch mode {
		case "arrayBuffer":
			finish(j.vm.ToValue(j.vm.NewArrayBuffer(append([]byte(nil), data...))))
		case "dataURL":
			finish(j.vm.ToValue("data:" + typ + ";base64," + base64.StdEncoding.EncodeToString(data)))
		default:
			finish(j.vm.ToValue(string(data)))
		}
		return sobek.Undefined()
	}
	j.method(o, "readAsText", func(call sobek.FunctionCall) sobek.Value {
		return read(call, "text")
	})
	j.method(o, "readAsArrayBuffer", func(call sobek.FunctionCall) sobek.Value {
		return read(call, "arrayBuffer")
	})
	j.method(o, "readAsDataURL", func(call sobek.FunctionCall) sobek.Value {
		return read(call, "dataURL")
	})
	j.method(o, "abort", func(sobek.FunctionCall) sobek.Value {
		_ = o.Set("readyState", 2)
		_ = o.Set("result", sobek.Null())
		_ = o.Set("error", j.abortError())
		fire("abort")
		fire("loadend")
		return sobek.Undefined()
	})
	return o
}

type formEntry struct {
	name  string
	value string
	blob  *sobek.Object
}

func (j *jsRuntime) newFormData(form sobek.Value) *sobek.Object {
	var entries []formEntry
	if n := j.unwrap(form); n != nil && n.Tag == "form" {
		n.FindAll(func(x *Node) bool {
			if x.Tag != "input" && x.Tag != "textarea" && x.Tag != "select" {
				return false
			}
			name := AttrValue(x, "name")
			if name == "" {
				return false
			}
			typ := strings.ToLower(AttrValue(x, "type"))
			if (typ == "checkbox" || typ == "radio") && !x.HasAttr("checked") {
				return false
			}
			value := x.Value
			if !x.ValueSet {
				value = AttrValue(x, "value")
			}
			entries = append(entries, formEntry{name: name, value: value})
			return false
		})
	}
	o := j.vm.NewObject()
	add := func(call sobek.FunctionCall, replace bool) {
		name := call.Argument(0).String()
		blob := jsObject(call.Argument(1))
		val := ""
		if blob != nil && j.blobs[blob] != nil {
			// Blob/File value.
		} else {
			blob = nil
			val = call.Argument(1).String()
		}
		if replace {
			first := -1
			for i, e := range entries {
				if e.name == name {
					first = i
					break
				}
			}
			entry := formEntry{name: name, value: val, blob: blob}
			if first < 0 {
				entries = append(entries, entry)
				return
			}
			kept := entries[:0]
			for i, e := range entries {
				if e.name != name {
					kept = append(kept, e)
					continue
				}
				if i == first {
					kept = append(kept, entry)
				}
			}
			entries = kept
			return
		}
		entries = append(entries, formEntry{name: name, value: val, blob: blob})
	}
	j.method(o, "append", func(call sobek.FunctionCall) sobek.Value {
		add(call, false)
		return sobek.Undefined()
	})
	j.method(o, "set", func(call sobek.FunctionCall) sobek.Value {
		add(call, true)
		return sobek.Undefined()
	})
	j.method(o, "get", func(call sobek.FunctionCall) sobek.Value {
		name := call.Argument(0).String()
		for _, e := range entries {
			if e.name == name {
				if e.blob != nil {
					return e.blob
				}
				return j.vm.ToValue(e.value)
			}
		}
		return sobek.Null()
	})
	j.method(o, "getAll", func(call sobek.FunctionCall) sobek.Value {
		name := call.Argument(0).String()
		out := j.vm.NewArray()
		for _, e := range entries {
			if e.name == name {
				if e.blob != nil {
					_ = out.Set(strconv.Itoa(len(out.Keys())), e.blob)
				} else {
					_ = out.Set(strconv.Itoa(len(out.Keys())), e.value)
				}
			}
		}
		return out
	})
	j.method(o, "has", func(call sobek.FunctionCall) sobek.Value {
		name := call.Argument(0).String()
		for _, e := range entries {
			if e.name == name {
				return j.vm.ToValue(true)
			}
		}
		return j.vm.ToValue(false)
	})
	j.method(o, "delete", func(call sobek.FunctionCall) sobek.Value {
		name := call.Argument(0).String()
		kept := entries[:0]
		for _, e := range entries {
			if e.name != name {
				kept = append(kept, e)
			}
		}
		entries = kept
		return sobek.Undefined()
	})
	j.method(o, "forEach", func(call sobek.FunctionCall) sobek.Value {
		fn, ok := sobek.AssertFunction(call.Argument(0))
		if !ok {
			return sobek.Undefined()
		}
		for _, e := range entries {
			val := j.vm.ToValue(e.value)
			if e.blob != nil {
				val = e.blob
			}
			_, _ = fn(sobek.Undefined(), val, j.vm.ToValue(e.name), o)
		}
		return sobek.Undefined()
	})
	entriesValue := func(withValue bool) sobek.Value {
		items := make([]sobek.Value, 0, len(entries))
		for _, e := range entries {
			val := j.vm.ToValue(e.value)
			if e.blob != nil {
				val = e.blob
			}
			if withValue {
				items = append(items, j.vm.NewArray(j.vm.ToValue(e.name), val))
			} else {
				items = append(items, j.vm.ToValue(e.name))
			}
		}
		return j.iteratorObject(items)
	}
	j.method(o, "entries", func(sobek.FunctionCall) sobek.Value { return entriesValue(true) })
	j.method(o, "keys", func(sobek.FunctionCall) sobek.Value { return entriesValue(false) })
	j.method(o, "values", func(sobek.FunctionCall) sobek.Value {
		items := make([]sobek.Value, 0, len(entries))
		for _, e := range entries {
			if e.blob != nil {
				items = append(items, e.blob)
			} else {
				items = append(items, j.vm.ToValue(e.value))
			}
		}
		return j.iteratorObject(items)
	})
	return o
}

func (j *jsRuntime) iteratorObject(items []sobek.Value) sobek.Value {
	it := j.vm.NewObject()
	idx := 0
	j.method(it, "next", func(sobek.FunctionCall) sobek.Value {
		out := j.vm.NewObject()
		if idx >= len(items) {
			_ = out.Set("done", true)
			_ = out.Set("value", sobek.Undefined())
			return out
		}
		_ = out.Set("done", false)
		_ = out.Set("value", items[idx])
		idx++
		return out
	})
	if symObj := jsObject(j.vm.Get("Symbol")); symObj != nil {
		if sym, ok := symObj.Get("iterator").(*sobek.Symbol); ok {
			_ = it.SetSymbol(sym, j.vm.ToValue(func(sobek.FunctionCall) sobek.Value { return it }))
		}
	}
	return it
}

// installElementCompat adds the cheap DOM surface that libraries commonly
// feature-detect.
func (j *jsRuntime) installElementCompatProto(p *sobek.Object) {
	j.protoAccessor(p, "dataset", func(n *Node) sobek.Value {
		return j.datasetObject(n)
	}, nil)

	j.protoAccessor(p, "outerHTML", func(n *Node) sobek.Value {
		var b strings.Builder
		SerializeNode(&b, n)
		return j.vm.ToValue(b.String())
	}, func(n *Node, v sobek.Value) {
		if n.Parent == nil || n.Tag == "html" || n.Tag == "body" {
			return
		}
		ref := nextSiblingNode(n)
		parent := n.Parent
		for _, c := range j.parseFragment(v.String()) {
			insertNode(parent, c, ref)
		}
		detach(n)
		j.markDirty()
	})

	insertAdjacent := func(n *Node, where string, child *Node) {
		if child == nil {
			return
		}
		switch strings.ToLower(where) {
		case "beforebegin":
			if n.Parent != nil {
				insertNode(n.Parent, child, n)
			}
		case "afterbegin":
			var first *Node
			if len(n.Children) > 0 {
				first = n.Children[0]
			}
			insertNode(n, child, first)
		case "beforeend":
			insertNode(n, child, nil)
		case "afterend":
			if n.Parent != nil {
				insertNode(n.Parent, child, nextSiblingNode(n))
			}
		}
		j.markDirty()
	}
	j.protoMethod(p, "insertAdjacentElement", func(n *Node, call sobek.FunctionCall) sobek.Value {
		child := j.unwrap(call.Argument(1))
		insertAdjacent(n, call.Argument(0).String(), child)
		return call.Argument(1)
	})
	j.protoMethod(p, "insertAdjacentText", func(n *Node, call sobek.FunctionCall) sobek.Value {
		insertAdjacent(n, call.Argument(0).String(), &Node{Type: TextNode, Text: call.Argument(1).String()})
		return sobek.Undefined()
	})
	j.protoMethod(p, "getAttributeNames", func(n *Node, _ sobek.FunctionCall) sobek.Value {
		names := make([]interface{}, 0, len(n.Attrs))
		for _, a := range n.Attrs {
			names = append(names, a.Key)
		}
		return j.vm.NewArray(names...)
	})
	j.protoMethod(p, "toggleAttribute", func(n *Node, call sobek.FunctionCall) sobek.Value {
		name := call.Argument(0).String()
		force := call.Argument(1)
		apply := !n.HasAttr(name)
		if force != nil && !sobek.IsUndefined(force) {
			apply = force.ToBoolean()
		}
		if apply {
			SetAttr(n, name, "")
		} else {
			RemoveAttr(n, name)
		}
		j.markDirty()
		return j.vm.ToValue(n.HasAttr(name))
	})
	j.protoMethod(p, "compareDocumentPosition", func(n *Node, call sobek.FunctionCall) sobek.Value {
		return j.vm.ToValue(int(compareDocumentPosition(n, j.unwrap(call.Argument(0)))))
	})
	j.protoMethod(p, "normalize", func(n *Node, _ sobek.FunctionCall) sobek.Value {
		normalizeNode(n)
		j.markDirty()
		return sobek.Undefined()
	})
	j.protoMethod(p, "focus", func(n *Node, _ sobek.FunctionCall) sobek.Value {
		if n.Tag == "input" || n.Tag == "textarea" {
			j.host.SetInputFocus(n)
		}
		j.markDirty()
		j.FireNodeEvent(n, "focus")
		return sobek.Undefined()
	})
	j.protoMethod(p, "blur", func(n *Node, _ sobek.FunctionCall) sobek.Value {
		if j.host.InputFocus() == n {
			j.host.SetInputFocus(nil)
		}
		j.markDirty()
		j.FireNodeEvent(n, "blur")
		return sobek.Undefined()
	})
	j.protoMethod(p, "scrollIntoView", func(n *Node, _ sobek.FunctionCall) sobek.Value { return sobek.Undefined() })
	j.protoMethod(p, "getClientRects", func(n *Node, _ sobek.FunctionCall) sobek.Value {
		return j.vm.NewArray(j.vm.ToValue(j.clientRectObject(n)))
	})
}

// datasetObject lazily builds (and caches) the dataset proxy for one element.
// Building a proxy with its traps for every element at wrap time was a large
// part of the per-element cost; most elements never touch dataset.
func (j *jsRuntime) datasetObject(n *Node) sobek.Value {
	if v, ok := j.datasetObjs[n]; ok {
		return v
	}
	target := j.vm.NewObject()
	if j.domStringMapProto != nil {
		_ = target.SetPrototype(j.domStringMapProto)
	}
	proxy := j.vm.NewProxy(target, &sobek.ProxyTrapConfig{
		Has: func(target *sobek.Object, prop string) bool {
			return n.HasAttr("data-" + camelToKebab(prop))
		},
		Get: func(target *sobek.Object, prop string, receiver sobek.Value) sobek.Value {
			name := "data-" + camelToKebab(prop)
			if !n.HasAttr(name) {
				return sobek.Undefined()
			}
			return j.vm.ToValue(AttrValue(n, name))
		},
		Set: func(target *sobek.Object, prop string, value sobek.Value, receiver sobek.Value) bool {
			SetAttr(n, "data-"+camelToKebab(prop), value.String())
			j.markDirty()
			return true
		},
		DeleteProperty: func(target *sobek.Object, prop string) bool {
			RemoveAttr(n, "data-"+camelToKebab(prop))
			j.markDirty()
			return true
		},
		OwnKeys: func(target *sobek.Object) *sobek.Object {
			var keys []interface{}
			for _, a := range n.Attrs {
				key := strings.ToLower(a.Key)
				if strings.HasPrefix(key, "data-") {
					keys = append(keys, kebabToCamel(strings.TrimPrefix(key, "data-")))
				}
			}
			return j.vm.NewArray(keys...)
		},
	})
	v := j.vm.ToValue(proxy)
	j.datasetObjs[n] = v
	return v
}

func (j *jsRuntime) clientRectObject(n *Node) *sobek.Object {
	x, y, w, h, _ := j.elementRect(n)
	r := j.vm.NewObject()
	_ = r.Set("x", x)
	_ = r.Set("y", y)
	_ = r.Set("top", y)
	_ = r.Set("left", x)
	_ = r.Set("right", x+w)
	_ = r.Set("bottom", y+h)
	_ = r.Set("width", w)
	_ = r.Set("height", h)
	return r
}

// elementRect returns a viewport-relative CSS-pixel rectangle for a node. It
// unions the element's laid-out block box, when there is one, with the
// fragments of its subtree, because inline content, form widgets and text runs
// carry the fragments while a block container may have no fragment of its own.
// Scripts position popovers and dropdowns from this rectangle; returning zeros
// made every such menu open at the page corner.
func (j *jsRuntime) elementRect(n *Node) (x, y, w, h int, ok bool) {
	if n == nil || j.host == nil {
		return 0, 0, 0, 0, false
	}
	if n.Tag == "html" || n.Tag == "body" {
		return 0, 0, CSSWidthPx(j.host.Width()), CSSHeightPx(j.host.Height()), true
	}
	ly := j.host.Layout()
	if ly == nil {
		return 0, 0, 0, 0, false
	}
	scroll := j.host.Scroll()
	minX, minY, maxX, maxY := 0, 0, 0, 0
	add := func(fx, fy, fw, fh int) {
		if fw <= 0 || fh <= 0 {
			return
		}
		if !ok {
			minX, minY, maxX, maxY = fx, fy, fx+fw, fy+fh
			ok = true
			return
		}
		if fx < minX {
			minX = fx
		}
		if fy < minY {
			minY = fy
		}
		if fx+fw > maxX {
			maxX = fx + fw
		}
		if fy+fh > maxY {
			maxY = fy + fh
		}
	}
	if r, found := ly.Boxes[n]; found {
		add(CSSWidthPx(r.X), CSSHeightPx(r.Y-scroll), CSSWidthPx(r.W), CSSHeightPx(r.H))
	}
	inSubtree := func(p *Node) bool {
		for ; p != nil; p = p.Parent {
			if p == n {
				return true
			}
		}
		return false
	}
	for i := range ly.Frags {
		f := &ly.Frags[i]
		if f.Node == nil || !inSubtree(f.Node) {
			continue
		}
		add(CSSWidthPx(f.X), CSSHeightPx(f.Y-scroll), CSSWidthPx(f.W), CSSHeightPx(f.H))
	}
	if !ok {
		return 0, 0, 0, 0, false
	}
	return minX, minY, maxX - minX, maxY - minY, true
}

func nodeRoot(n *Node) *Node {
	for n != nil && n.Parent != nil {
		n = n.Parent
	}
	return n
}

func nodePath(n *Node) []*Node {
	var path []*Node
	for x := n; x != nil; x = x.Parent {
		path = append(path, x)
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

func compareDocumentPosition(a, b *Node) uint16 {
	const (
		disconnected = 1
		preceding    = 2
		following    = 4
		contains     = 8
		containedBy  = 16
	)
	if a == nil || b == nil || a == b {
		return 0
	}
	if nodeRoot(a) != nodeRoot(b) {
		return disconnected
	}
	for x := b.Parent; x != nil; x = x.Parent {
		if x == a {
			return containedBy | following
		}
	}
	for x := a.Parent; x != nil; x = x.Parent {
		if x == b {
			return contains | preceding
		}
	}
	pa, pb := nodePath(a), nodePath(b)
	i := 0
	for i < len(pa) && i < len(pb) && pa[i] == pb[i] {
		i++
	}
	if i == 0 || i >= len(pa) || i >= len(pb) {
		return following
	}
	parent := pa[i-1]
	idxA, idxB := -1, -1
	for idx, c := range parent.Children {
		if c == pa[i] {
			idxA = idx
		}
		if c == pb[i] {
			idxB = idx
		}
	}
	if idxA >= 0 && idxB >= 0 && idxA < idxB {
		return following
	}
	return preceding
}

func normalizeNode(n *Node) {
	var kept []*Node
	for _, c := range n.Children {
		normalizeNode(c)
		if c.Type == TextNode {
			if c.Text == "" {
				c.Parent = nil
				continue
			}
			if len(kept) > 0 && kept[len(kept)-1].Type == TextNode {
				kept[len(kept)-1].Text += c.Text
				c.Parent = nil
				continue
			}
		}
		kept = append(kept, c)
	}
	n.Children = kept
}

func (j *jsRuntime) installDocumentCompat(o *sobek.Object, root *Node) {
	j.method(o, "createRange", func(sobek.FunctionCall) sobek.Value {
		r, _ := j.newRange()
		return r
	})
	j.method(o, "createTreeWalker", func(call sobek.FunctionCall) sobek.Value {
		n := j.unwrap(call.Argument(0))
		if n == nil {
			n = root
		}
		return j.newTreeWalker(n, call.Argument(1), call.Argument(2), false)
	})
	j.method(o, "createNodeIterator", func(call sobek.FunctionCall) sobek.Value {
		n := j.unwrap(call.Argument(0))
		if n == nil {
			n = root
		}
		return j.newTreeWalker(n, call.Argument(1), call.Argument(2), true)
	})
	j.method(o, "getSelection", func(sobek.FunctionCall) sobek.Value { return j.selectionObject() })
	j.method(o, "hasFocus", func(sobek.FunctionCall) sobek.Value { return j.vm.ToValue(true) })
	j.accessor(o, "activeElement", func() sobek.Value {
		if j.host != nil && j.host.InputFocus() != nil {
			return j.wrap(j.host.InputFocus())
		}
		if j.page != nil && j.page.Root != nil {
			if body := j.page.Root.FindFirst("body"); body != nil {
				return j.wrap(body)
			}
		}
		return sobek.Null()
	}, nil)
}

func (j *jsRuntime) walkerAccept(whatToShow, filter sobek.Value) func(*Node) bool {
	mask := int64(0xFFFFFFFF)
	if whatToShow != nil && !sobek.IsUndefined(whatToShow) && !sobek.IsNull(whatToShow) {
		mask = whatToShow.ToInteger()
	}
	var fn sobek.Callable
	if filter != nil && !sobek.IsUndefined(filter) && !sobek.IsNull(filter) {
		if f, ok := sobek.AssertFunction(filter); ok {
			fn = f
		} else if o := jsObject(filter); o != nil {
			if f, ok := sobek.AssertFunction(o.Get("acceptNode")); ok {
				fn = f
			}
		}
	}
	return func(n *Node) bool {
		switch n.Type {
		case ElementNode:
			if mask&1 == 0 {
				return false
			}
		case TextNode:
			if mask&4 == 0 {
				return false
			}
		}
		if fn == nil {
			return true
		}
		v, ok := j.call(fn, sobek.Undefined(), j.wrap(n))
		return ok && v != nil && !sobek.IsUndefined(v) && v.ToInteger() == 1
	}
}

func preorderNodes(root *Node) []*Node {
	var out []*Node
	var walk func(*Node)
	walk = func(n *Node) {
		out = append(out, n)
		for _, c := range n.Children {
			walk(c)
		}
	}
	if root != nil {
		walk(root)
	}
	return out
}

func (j *jsRuntime) newTreeWalker(root *Node, whatToShow, filter sobek.Value, iterator bool) *sobek.Object {
	o := j.vm.NewObject()
	if iterator {
		if j.nodeIteratorProto != nil {
			_ = o.SetPrototype(j.nodeIteratorProto)
		}
	} else if j.treeWalkerProto != nil {
		_ = o.SetPrototype(j.treeWalkerProto)
	}
	accept := j.walkerAccept(whatToShow, filter)
	var cur *Node
	if !iterator {
		cur = root
	}
	_ = o.Set("root", j.wrap(root))
	j.accessor(o, "currentNode", func() sobek.Value { return j.wrap(cur) }, func(v sobek.Value) {
		if n := j.unwrap(v); n != nil {
			cur = n
		}
	})
	j.accessor(o, "whatToShow", func() sobek.Value { return j.vm.ToValue(int64(0xFFFFFFFF)) }, nil)
	_ = o.Set("filter", filter)
	_ = o.Set("expandEntityReferences", false)
	j.method(o, "nextNode", func(sobek.FunctionCall) sobek.Value {
		nodes := preorderNodes(root)
		start := 0
		if iterator {
			start = 0
		}
		if cur != nil {
			for i, n := range nodes {
				if n == cur {
					start = i + 1
					break
				}
			}
		}
		for i := start; i < len(nodes); i++ {
			if accept(nodes[i]) {
				cur = nodes[i]
				return j.wrap(cur)
			}
		}
		return sobek.Null()
	})
	j.method(o, "previousNode", func(sobek.FunctionCall) sobek.Value {
		nodes := preorderNodes(root)
		start := len(nodes) - 1
		if cur != nil {
			for i, n := range nodes {
				if n == cur {
					start = i - 1
					break
				}
			}
		}
		for i := start; i >= 0; i-- {
			if !iterator && nodes[i] == root {
				continue
			}
			if accept(nodes[i]) {
				cur = nodes[i]
				return j.wrap(cur)
			}
		}
		return sobek.Null()
	})
	j.method(o, "parentNode", func(sobek.FunctionCall) sobek.Value {
		for p := cur.Parent; p != nil; p = p.Parent {
			if accept(p) {
				cur = p
				return j.wrap(cur)
			}
			if p == root {
				break
			}
		}
		return sobek.Null()
	})
	j.method(o, "firstChild", func(sobek.FunctionCall) sobek.Value {
		if cur == nil {
			return sobek.Null()
		}
		for _, c := range cur.Children {
			if accept(c) {
				cur = c
				return j.wrap(cur)
			}
		}
		return sobek.Null()
	})
	j.method(o, "lastChild", func(sobek.FunctionCall) sobek.Value {
		if cur == nil {
			return sobek.Null()
		}
		for i := len(cur.Children) - 1; i >= 0; i-- {
			if accept(cur.Children[i]) {
				cur = cur.Children[i]
				return j.wrap(cur)
			}
		}
		return sobek.Null()
	})
	j.method(o, "nextSibling", func(sobek.FunctionCall) sobek.Value {
		if cur == nil {
			return sobek.Null()
		}
		for s := nextSiblingNode(cur); s != nil; s = nextSiblingNode(s) {
			if accept(s) {
				cur = s
				return j.wrap(cur)
			}
		}
		return sobek.Null()
	})
	j.method(o, "previousSibling", func(sobek.FunctionCall) sobek.Value {
		if cur == nil {
			return sobek.Null()
		}
		for s := prevSiblingNode(cur); s != nil; s = prevSiblingNode(s) {
			if accept(s) {
				cur = s
				return j.wrap(cur)
			}
		}
		return sobek.Null()
	})
	j.method(o, "detach", func(sobek.FunctionCall) sobek.Value { return sobek.Undefined() })
	return o
}

type jsRange struct {
	start, end       *Node
	startOff, endOff int
}

func (j *jsRuntime) newRange() (*sobek.Object, *jsRange) {
	st := &jsRange{}
	o := j.vm.NewObject()
	j.accessor(o, "startContainer", func() sobek.Value { return j.wrap(st.start) }, nil)
	j.accessor(o, "endContainer", func() sobek.Value { return j.wrap(st.end) }, nil)
	j.accessor(o, "startOffset", func() sobek.Value { return j.vm.ToValue(st.startOff) }, nil)
	j.accessor(o, "endOffset", func() sobek.Value { return j.vm.ToValue(st.endOff) }, nil)
	j.accessor(o, "collapsed", func() sobek.Value {
		return j.vm.ToValue(st.start != nil && st.start == st.end && st.startOff == st.endOff)
	}, nil)
	j.accessor(o, "commonAncestorContainer", func() sobek.Value {
		return j.wrap(commonAncestor(st.start, st.end))
	}, nil)
	set := func(call sobek.FunctionCall, isStart bool) sobek.Value {
		n := j.unwrap(call.Argument(0))
		if n == nil {
			return sobek.Undefined()
		}
		off := int(call.Argument(1).ToInteger())
		if isStart {
			st.start, st.startOff = n, off
		} else {
			st.end, st.endOff = n, off
		}
		return sobek.Undefined()
	}
	j.method(o, "setStart", func(call sobek.FunctionCall) sobek.Value { return set(call, true) })
	j.method(o, "setEnd", func(call sobek.FunctionCall) sobek.Value { return set(call, false) })
	j.method(o, "setStartBefore", func(call sobek.FunctionCall) sobek.Value {
		st.start = j.unwrap(call.Argument(0))
		st.startOff = 0
		return sobek.Undefined()
	})
	j.method(o, "setStartAfter", func(call sobek.FunctionCall) sobek.Value {
		st.start = nextSiblingNode(j.unwrap(call.Argument(0)))
		st.startOff = 0
		return sobek.Undefined()
	})
	j.method(o, "setEndBefore", func(call sobek.FunctionCall) sobek.Value {
		st.end = j.unwrap(call.Argument(0))
		st.endOff = 0
		return sobek.Undefined()
	})
	j.method(o, "setEndAfter", func(call sobek.FunctionCall) sobek.Value {
		st.end = nextSiblingNode(j.unwrap(call.Argument(0)))
		st.endOff = 0
		return sobek.Undefined()
	})
	j.method(o, "selectNode", func(call sobek.FunctionCall) sobek.Value {
		st.start, st.end = j.unwrap(call.Argument(0)), j.unwrap(call.Argument(0))
		st.startOff, st.endOff = 0, 0
		return sobek.Undefined()
	})
	j.method(o, "selectNodeContents", func(call sobek.FunctionCall) sobek.Value {
		st.start, st.end = j.unwrap(call.Argument(0)), j.unwrap(call.Argument(0))
		st.startOff, st.endOff = 0, 0
		if st.end != nil {
			st.endOff = len(st.end.Children)
		}
		return sobek.Undefined()
	})
	j.method(o, "collapse", func(call sobek.FunctionCall) sobek.Value {
		if call.Argument(0).ToBoolean() {
			st.end, st.endOff = st.start, st.startOff
		} else {
			st.start, st.startOff = st.end, st.endOff
		}
		return sobek.Undefined()
	})
	j.method(o, "cloneRange", func(sobek.FunctionCall) sobek.Value {
		clone, cst := j.newRange()
		cst.start, cst.end = st.start, st.end
		cst.startOff, cst.endOff = st.startOff, st.endOff
		return clone
	})
	j.method(o, "detach", func(sobek.FunctionCall) sobek.Value { return sobek.Undefined() })
	j.method(o, "toString", func(sobek.FunctionCall) sobek.Value {
		if st.start == nil {
			return j.vm.ToValue("")
		}
		if st.start == st.end && st.start.Type == TextNode {
			runes := []rune(st.start.Text)
			lo := clampInt(st.startOff, 0, len(runes))
			hi := clampInt(st.endOff, lo, len(runes))
			return j.vm.ToValue(string(runes[lo:hi]))
		}
		if ca := commonAncestor(st.start, st.end); ca != nil {
			return j.vm.ToValue(ca.TextContent())
		}
		return j.vm.ToValue("")
	})
	j.method(o, "cloneContents", func(sobek.FunctionCall) sobek.Value {
		frag := &Node{Type: ElementNode, Tag: "#fragment"}
		if st.start != nil {
			frag.Children = append(frag.Children, cloneNode(st.start, true))
		}
		return j.wrap(frag)
	})
	j.method(o, "deleteContents", func(sobek.FunctionCall) sobek.Value {
		if st.start != nil && st.start == st.end && st.start.Type == TextNode {
			st.start.Text = ""
			st.startOff, st.endOff = 0, 0
		}
		return sobek.Undefined()
	})
	j.method(o, "insertNode", func(call sobek.FunctionCall) sobek.Value {
		n := j.unwrap(call.Argument(0))
		if n != nil && st.start != nil {
			if st.start.Type == TextNode && st.start.Parent != nil {
				insertNode(st.start.Parent, n, st.start)
			} else {
				insertNode(st.start, n, nil)
			}
			j.markDirty()
		}
		return sobek.Undefined()
	})
	j.method(o, "createContextualFragment", func(call sobek.FunctionCall) sobek.Value {
		frag := &Node{Type: ElementNode, Tag: "#fragment"}
		for _, c := range j.parseFragment(call.Argument(0).String()) {
			c.Parent = frag
			frag.Children = append(frag.Children, c)
		}
		return j.wrap(frag)
	})
	j.method(o, "getBoundingClientRect", func(sobek.FunctionCall) sobek.Value {
		return j.clientRectObject(st.start)
	})
	j.method(o, "getClientRects", func(sobek.FunctionCall) sobek.Value {
		return j.vm.NewArray(j.vm.ToValue(j.clientRectObject(st.start)))
	})
	j.ranges[o] = st
	return o, st
}

func commonAncestor(a, b *Node) *Node {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	seen := make(map[*Node]bool)
	for x := a; x != nil; x = x.Parent {
		seen[x] = true
	}
	for x := b; x != nil; x = x.Parent {
		if seen[x] {
			return x
		}
	}
	return nil
}

func (j *jsRuntime) selectionObject() *sobek.Object {
	if j.selection != nil {
		return j.selection
	}
	o := j.vm.NewObject()
	var rng *jsRange
	var rngObj *sobek.Object
	emptyRange := func() *sobek.Object {
		r, st := j.newRange()
		rng, rngObj = st, r
		return r
	}
	j.accessor(o, "rangeCount", func() sobek.Value {
		if rng == nil {
			return j.vm.ToValue(0)
		}
		return j.vm.ToValue(1)
	}, nil)
	j.accessor(o, "isCollapsed", func() sobek.Value {
		if rng == nil {
			return j.vm.ToValue(true)
		}
		return j.vm.ToValue(rng.start == rng.end && rng.startOff == rng.endOff)
	}, nil)
	j.accessor(o, "anchorNode", func() sobek.Value {
		if rng == nil {
			return sobek.Null()
		}
		return j.wrap(rng.start)
	}, nil)
	j.accessor(o, "focusNode", func() sobek.Value {
		if rng == nil {
			return sobek.Null()
		}
		return j.wrap(rng.end)
	}, nil)
	j.accessor(o, "anchorOffset", func() sobek.Value {
		if rng == nil {
			return j.vm.ToValue(0)
		}
		return j.vm.ToValue(rng.startOff)
	}, nil)
	j.accessor(o, "focusOffset", func() sobek.Value {
		if rng == nil {
			return j.vm.ToValue(0)
		}
		return j.vm.ToValue(rng.endOff)
	}, nil)
	j.method(o, "getRangeAt", func(call sobek.FunctionCall) sobek.Value {
		if call.Argument(0).ToInteger() != 0 {
			return sobek.Null()
		}
		if rng == nil {
			return emptyRange()
		}
		if rngObj == nil {
			rngObj, _ = j.newRange()
			j.ranges[rngObj] = rng
		}
		return rngObj
	})
	j.method(o, "addRange", func(call sobek.FunctionCall) sobek.Value {
		if obj := jsObject(call.Argument(0)); obj != nil {
			if st, ok := j.ranges[obj]; ok {
				rng, rngObj = st, obj
			}
		}
		return sobek.Undefined()
	})
	j.method(o, "removeAllRanges", func(sobek.FunctionCall) sobek.Value {
		rng, rngObj = nil, nil
		return sobek.Undefined()
	})
	j.method(o, "removeRange", func(sobek.FunctionCall) sobek.Value {
		rng, rngObj = nil, nil
		return sobek.Undefined()
	})
	j.method(o, "collapse", func(call sobek.FunctionCall) sobek.Value {
		st := &jsRange{start: j.unwrap(call.Argument(0)), end: j.unwrap(call.Argument(0))}
		st.startOff = int(call.Argument(1).ToInteger())
		st.endOff = st.startOff
		rng, rngObj = st, nil
		return sobek.Undefined()
	})
	j.method(o, "selectAllChildren", func(call sobek.FunctionCall) sobek.Value {
		n := j.unwrap(call.Argument(0))
		if n != nil {
			rng = &jsRange{start: n, end: n, startOff: 0, endOff: len(n.Children)}
			rngObj = nil
		}
		return sobek.Undefined()
	})
	j.method(o, "toString", func(sobek.FunctionCall) sobek.Value {
		if rng == nil || rng.start == nil {
			return j.vm.ToValue("")
		}
		if rng.start == rng.end && rng.start.Type == TextNode {
			runes := []rune(rng.start.Text)
			lo := clampInt(rng.startOff, 0, len(runes))
			hi := clampInt(rng.endOff, lo, len(runes))
			return j.vm.ToValue(string(runes[lo:hi]))
		}
		if ca := commonAncestor(rng.start, rng.end); ca != nil {
			return j.vm.ToValue(ca.TextContent())
		}
		return j.vm.ToValue("")
	})
	j.method(o, "containsNode", func(call sobek.FunctionCall) sobek.Value {
		n := j.unwrap(call.Argument(0))
		for p := n; p != nil; p = p.Parent {
			if rng != nil && p == rng.start {
				return j.vm.ToValue(true)
			}
		}
		return j.vm.ToValue(false)
	})
	j.selection = o
	return o
}
