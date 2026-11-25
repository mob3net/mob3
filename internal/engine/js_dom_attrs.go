package engine

import (
	"strings"

	"github.com/grafana/sobek"
)

func (j *jsRuntime) attributeObject(n *Node, a Attr) *sobek.Object {
	o := j.vm.NewObject()
	_ = o.Set("name", a.Key)
	_ = o.Set("nodeName", a.Key)
	_ = o.Set("localName", a.Key)
	_ = o.Set("value", a.Value)
	_ = o.Set("nodeValue", a.Value)
	_ = o.Set("specified", true)
	_ = o.Set("namespaceURI", sobek.Null())
	_ = o.Set("ownerElement", j.wrap(n))
	return o
}

// attributesObject returns a NamedNodeMap-like array over the element's
// attributes. It is rebuilt on each access so later attribute changes are
// reflected, matching the live DOM map closely enough for libraries.
func (j *jsRuntime) attributesObject(n *Node) *sobek.Object {
	attrs := make([]*sobek.Object, len(n.Attrs))
	vals := make([]interface{}, len(n.Attrs))
	for i := range n.Attrs {
		o := j.attributeObject(n, n.Attrs[i])
		attrs[i] = o
		vals[i] = o
	}
	m := j.vm.NewArray(vals...)
	j.method(m, "item", func(call sobek.FunctionCall) sobek.Value {
		idx := int(call.Argument(0).ToInteger())
		if idx >= 0 && idx < len(attrs) {
			return attrs[idx]
		}
		return sobek.Null()
	})
	j.method(m, "getNamedItem", func(call sobek.FunctionCall) sobek.Value {
		name := call.Argument(0).String()
		for i := range n.Attrs {
			if strings.EqualFold(n.Attrs[i].Key, name) {
				return attrs[i]
			}
		}
		return sobek.Null()
	})
	j.method(m, "removeNamedItem", func(call sobek.FunctionCall) sobek.Value {
		name := call.Argument(0).String()
		for i := range attrs {
			if strings.EqualFold(n.Attrs[i].Key, name) {
				removed := attrs[i]
				RemoveAttr(n, n.Attrs[i].Key)
				j.markDirty()
				return removed
			}
		}
		return sobek.Null()
	})
	j.method(m, "setNamedItem", func(call sobek.FunctionCall) sobek.Value {
		attr := jsObject(call.Argument(0))
		if attr == nil {
			return sobek.Null()
		}
		name := attr.Get("name")
		value := attr.Get("value")
		if name == nil || value == nil {
			return sobek.Null()
		}
		SetAttr(n, name.String(), value.String())
		j.markDirty()
		return sobek.Null()
	})
	return m
}

func (j *jsRuntime) classListObject(n *Node) *sobek.Object {
	o := j.vm.NewObject()
	if j.domTokenListProto != nil {
		_ = o.SetPrototype(j.domTokenListProto)
	}
	get := func() []string { return n.ClassList() }
	put := func(list []string) {
		SetAttr(n, "class", strings.Join(list, " "))
		j.markDirty()
	}
	j.accessor(o, "value", func() sobek.Value {
		return j.vm.ToValue(strings.Join(get(), " "))
	}, func(v sobek.Value) {
		put(strings.Fields(v.String()))
	})
	j.accessor(o, "length", func() sobek.Value {
		return j.vm.ToValue(len(get()))
	}, nil)
	j.method(o, "contains", func(call sobek.FunctionCall) sobek.Value {
		name := call.Argument(0).String()
		for _, c := range get() {
			if c == name {
				return j.vm.ToValue(true)
			}
		}
		return j.vm.ToValue(false)
	})
	j.method(o, "add", func(call sobek.FunctionCall) sobek.Value {
		list := get()
		for _, arg := range call.Arguments {
			name := strings.TrimSpace(arg.String())
			if name == "" {
				continue
			}
			found := false
			for _, c := range list {
				if c == name {
					found = true
					break
				}
			}
			if !found {
				list = append(list, name)
			}
		}
		put(list)
		return sobek.Undefined()
	})
	j.method(o, "remove", func(call sobek.FunctionCall) sobek.Value {
		drop := map[string]bool{}
		for _, arg := range call.Arguments {
			drop[strings.TrimSpace(arg.String())] = true
		}
		list := get()
		out := list[:0]
		for _, c := range list {
			if !drop[c] {
				out = append(out, c)
			}
		}
		put(out)
		return sobek.Undefined()
	})
	j.method(o, "toggle", func(call sobek.FunctionCall) sobek.Value {
		name := strings.TrimSpace(call.Argument(0).String())
		if name == "" {
			return j.vm.ToValue(false)
		}
		force := call.Argument(1)
		list := get()
		has := false
		for _, c := range list {
			if c == name {
				has = true
				break
			}
		}
		remove := func() {
			out := list[:0]
			for _, c := range list {
				if c != name {
					out = append(out, c)
				}
			}
			put(out)
		}
		if !sobek.IsUndefined(force) && !sobek.IsNull(force) {
			want := force.ToBoolean()
			switch {
			case want && !has:
				put(append(list, name))
			case !want && has:
				remove()
			}
			return j.vm.ToValue(want)
		}
		if has {
			remove()
			return j.vm.ToValue(false)
		}
		put(append(list, name))
		return j.vm.ToValue(true)
	})
	return o
}
