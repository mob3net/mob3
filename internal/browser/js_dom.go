package browser

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/dop251/goja"
)

func (j *jsRuntime) accessor(o *goja.Object, name string, get func() goja.Value, set func(goja.Value)) {
	var g, s goja.Value
	if get != nil {
		g = j.vm.ToValue(func(goja.FunctionCall) goja.Value { return get() })
	}
	if set != nil {
		s = j.vm.ToValue(func(call goja.FunctionCall) goja.Value {
			set(call.Argument(0))
			return goja.Undefined()
		})
	}
	_ = o.DefineAccessorProperty(name, g, s, goja.FLAG_TRUE, goja.FLAG_FALSE)
}

func (j *jsRuntime) method(o *goja.Object, name string, fn func(goja.FunctionCall) goja.Value) {
	_ = o.Set(name, j.vm.ToValue(fn))
}

func (j *jsRuntime) unwrap(v goja.Value) *Node {
	if o, ok := v.(*goja.Object); ok {
		return j.rev[o]
	}
	return nil
}

func attrValue(n *Node, name string) string {
	if n == nil {
		return ""
	}
	for i := range n.Attrs {
		if strings.EqualFold(n.Attrs[i].Key, name) {
			return n.Attrs[i].Value
		}
	}
	return ""
}

func setAttr(n *Node, name, value string) {
	if n == nil {
		return
	}
	for i := range n.Attrs {
		if strings.EqualFold(n.Attrs[i].Key, name) {
			n.Attrs[i].Value = value
			return
		}
	}
	n.Attrs = append(n.Attrs, Attr{Key: strings.ToLower(name), Value: value})
}

func removeAttr(n *Node, name string) {
	for i := range n.Attrs {
		if strings.EqualFold(n.Attrs[i].Key, name) {
			n.Attrs = append(n.Attrs[:i], n.Attrs[i+1:]...)
			return
		}
	}
}

func detach(n *Node) {
	if n == nil || n.Parent == nil {
		return
	}
	p := n.Parent
	for i := range p.Children {
		if p.Children[i] == n {
			p.Children = append(p.Children[:i], p.Children[i+1:]...)
			break
		}
	}
	n.Parent = nil
}

func nextSiblingNode(n *Node) *Node {
	if n == nil || n.Parent == nil {
		return nil
	}
	for i, c := range n.Parent.Children {
		if c == n && i+1 < len(n.Parent.Children) {
			return n.Parent.Children[i+1]
		}
	}
	return nil
}

func prevSiblingNode(n *Node) *Node {
	if n == nil || n.Parent == nil {
		return nil
	}
	for i, c := range n.Parent.Children {
		if c == n && i > 0 {
			return n.Parent.Children[i-1]
		}
	}
	return nil
}

// insertNode inserts child before ref (or appends when ref is nil). Document
// fragments insert their children and are left empty.
func insertNode(parent, child, ref *Node) {
	if parent == nil || child == nil || child == parent {
		return
	}
	for p := parent; p != nil; p = p.Parent {
		if p == child {
			return
		}
	}
	if child.Tag == "#fragment" {
		kids := append([]*Node(nil), child.Children...)
		for _, k := range kids {
			insertNode(parent, k, ref)
		}
		child.Children = nil
		return
	}
	detach(child)
	child.Parent = parent
	if ref != nil && ref.Parent == parent {
		for i, c := range parent.Children {
			if c == ref {
				parent.Children = append(parent.Children, nil)
				copy(parent.Children[i+1:], parent.Children[i:])
				parent.Children[i] = child
				return
			}
		}
	}
	parent.Children = append(parent.Children, child)
}

func cloneNode(n *Node, deep bool) *Node {
	if n == nil {
		return nil
	}
	c := &Node{Type: n.Type, Tag: n.Tag, Text: n.Text}
	c.Attrs = append([]Attr(nil), n.Attrs...)
	c.Img = n.Img
	c.ImgSrc = n.ImgSrc
	c.ImgFailed = n.ImgFailed
	c.Value = n.Value
	c.ValueSet = n.ValueSet
	c.DialogReturnValue = n.DialogReturnValue
	if deep {
		for _, ch := range n.Children {
			cc := cloneNode(ch, true)
			cc.Parent = c
			c.Children = append(c.Children, cc)
		}
	}
	return c
}

func (j *jsRuntime) parseFragment(html string) []*Node {
	wrapper := ParseHTML("<html><body>" + html + "</body></html>")
	body := wrapper.FindFirst("body")
	if body == nil {
		return nil
	}
	out := append([]*Node(nil), body.Children...)
	for _, c := range out {
		c.Parent = nil
	}
	return out
}

func isConnected(n *Node) bool {
	for p := n; p != nil; p = p.Parent {
		if p.Type == DocumentNode {
			return true
		}
	}
	return false
}

func elementChildren(n *Node) []*Node {
	var out []*Node
	if n == nil {
		return out
	}
	for _, c := range n.Children {
		if c.Type == ElementNode {
			out = append(out, c)
		}
	}
	return out
}

// wrap returns the JS wrapper object for a DOM node.
func (j *jsRuntime) wrap(n *Node) goja.Value {
	if n == nil {
		return goja.Null()
	}
	if n.Type == DocumentNode {
		if j.docObj != nil {
			return j.docObj
		}
		return goja.Null()
	}
	if o, ok := j.nodes[n]; ok {
		return o
	}
	o := j.vm.NewObject()
	j.nodes[n] = o
	j.rev[o] = n
	if n.Type == TextNode {
		j.setElementPrototype(o, n)
		j.initTextObject(o, n)
	} else {
		j.setElementPrototype(o, n)
		j.initElementObject(o, n)
	}
	return o
}

func (j *jsRuntime) initTextObject(o *goja.Object, n *Node) {
	_ = o.Set("nodeType", 3)
	_ = o.Set("nodeName", "#text")
	_ = o.Set("tagName", "")
	j.accessor(o, "textContent", func() goja.Value {
		return j.vm.ToValue(n.Text)
	}, func(v goja.Value) {
		n.Text = v.String()
		j.markDirty()
	})
	j.accessor(o, "nodeValue", func() goja.Value {
		return j.vm.ToValue(n.Text)
	}, func(v goja.Value) {
		n.Text = v.String()
		j.markDirty()
	})
	j.accessor(o, "parentNode", func() goja.Value { return j.wrap(n.Parent) }, nil)
	j.accessor(o, "nextSibling", func() goja.Value { return j.wrap(nextSiblingNode(n)) }, nil)
	j.accessor(o, "previousSibling", func() goja.Value { return j.wrap(prevSiblingNode(n)) }, nil)
	j.method(o, "remove", func(goja.FunctionCall) goja.Value {
		detach(n)
		j.markDirty()
		return goja.Undefined()
	})
	j.method(o, "cloneNode", func(call goja.FunctionCall) goja.Value {
		return j.wrap(cloneNode(n, call.Argument(0).ToBoolean()))
	})
}

func (j *jsRuntime) initElementObject(o *goja.Object, n *Node) {
	_ = o.Set("nodeType", 1)
	_ = o.Set("tagName", strings.ToUpper(n.Tag))
	_ = o.Set("nodeName", strings.ToUpper(n.Tag))
	j.accessor(o, "id", func() goja.Value { return j.vm.ToValue(attrValue(n, "id")) }, func(v goja.Value) {
		setAttr(n, "id", v.String())
		j.markDirty()
	})
	j.accessor(o, "className", func() goja.Value { return j.vm.ToValue(attrValue(n, "class")) }, func(v goja.Value) {
		setAttr(n, "class", v.String())
		j.markDirty()
	})
	j.accessor(o, "href", func() goja.Value { return j.vm.ToValue(attrValue(n, "href")) }, func(v goja.Value) {
		setAttr(n, "href", v.String())
		j.markDirty()
	})
	j.accessor(o, "value", func() goja.Value {
		return j.vm.ToValue(inputValue(n))
	}, func(v goja.Value) {
		n.Value = v.String()
		n.ValueSet = true
		j.markDirty()
	})
	j.accessor(o, "checked", func() goja.Value {
		return j.vm.ToValue(n.HasAttr("checked"))
	}, func(v goja.Value) {
		if v.ToBoolean() {
			setAttr(n, "checked", "")
		} else {
			removeAttr(n, "checked")
		}
		j.markDirty()
	})
	j.accessor(o, "form", func() goja.Value {
		return j.wrap(nearestForm(n))
	}, nil)
	for jsName, attrName := range map[string]string{
		"src": "src", "alt": "alt", "width": "width", "height": "height",
	} {
		jsName, attrName := jsName, attrName
		j.accessor(o, jsName, func() goja.Value {
			return j.vm.ToValue(attrValue(n, attrName))
		}, func(v goja.Value) {
			setAttr(n, attrName, v.String())
			j.markDirty()
		})
	}
	j.accessor(o, "complete", func() goja.Value { return j.vm.ToValue(true) }, nil)
	j.accessor(o, "naturalWidth", func() goja.Value { return j.vm.ToValue(0) }, nil)
	j.accessor(o, "naturalHeight", func() goja.Value { return j.vm.ToValue(0) }, nil)
	if n.Tag == "canvas" {
		j.initCanvasObject(o, n)
	}
	if n.Tag == "dialog" {
		j.initDialogObject(o, n)
	}
	vw, vh := j.b.width*8, j.b.height*16
	metric := func(viewportValue int) func() goja.Value {
		return func() goja.Value {
			if n.Tag == "html" || n.Tag == "body" {
				return j.vm.ToValue(viewportValue)
			}
			return j.vm.ToValue(0)
		}
	}
	j.accessor(o, "clientWidth", metric(vw), nil)
	j.accessor(o, "clientHeight", metric(vh), nil)
	j.accessor(o, "offsetWidth", metric(vw), nil)
	j.accessor(o, "offsetHeight", metric(vh), nil)
	j.accessor(o, "scrollWidth", metric(vw), nil)
	j.accessor(o, "scrollHeight", metric(vh), nil)
	j.accessor(o, "offsetTop", func() goja.Value { return j.vm.ToValue(0) }, nil)
	j.accessor(o, "offsetLeft", func() goja.Value { return j.vm.ToValue(0) }, nil)
	j.accessor(o, "offsetParent", func() goja.Value { return goja.Null() }, nil)
	j.accessor(o, "scrollTop", func() goja.Value { return j.vm.ToValue(0) }, func(goja.Value) {})
	j.accessor(o, "scrollLeft", func() goja.Value { return j.vm.ToValue(0) }, func(goja.Value) {})
	j.method(o, "getBoundingClientRect", func(call goja.FunctionCall) goja.Value {
		r := j.vm.NewObject()
		w, h := 0, 0
		if n.Tag == "html" || n.Tag == "body" {
			w, h = vw, vh
		}
		_ = r.Set("x", 0)
		_ = r.Set("y", 0)
		_ = r.Set("top", 0)
		_ = r.Set("left", 0)
		_ = r.Set("right", w)
		_ = r.Set("bottom", h)
		_ = r.Set("width", w)
		_ = r.Set("height", h)
		return r
	})
	j.accessor(o, "textContent", func() goja.Value {
		return j.vm.ToValue(n.TextContent())
	}, func(v goja.Value) {
		for _, c := range n.Children {
			c.Parent = nil
		}
		n.Children = nil
		if s := v.String(); s != "" {
			n.Children = append(n.Children, &Node{Type: TextNode, Text: s, Parent: n})
		}
		j.markDirty()
	})
	j.accessor(o, "innerText", func() goja.Value {
		return j.vm.ToValue(n.TextContent())
	}, func(v goja.Value) {
		for _, c := range n.Children {
			c.Parent = nil
		}
		n.Children = nil
		if s := v.String(); s != "" {
			n.Children = append(n.Children, &Node{Type: TextNode, Text: s, Parent: n})
		}
		j.markDirty()
	})
	j.accessor(o, "innerHTML", func() goja.Value {
		return j.vm.ToValue(serializeChildren(n))
	}, func(v goja.Value) {
		j.setInnerHTML(n, v.String())
	})
	j.accessor(o, "style", func() goja.Value {
		return j.styleObject(n)
	}, nil)
	j.accessor(o, "classList", func() goja.Value {
		return j.classListObject(n)
	}, nil)
	j.accessor(o, "onclick", func() goja.Value {
		if m := j.propListeners[n]; m != nil {
			if l, ok := m["click"]; ok {
				return l.value
			}
		}
		return goja.Null()
	}, func(v goja.Value) {
		fn, ok := goja.AssertFunction(v)
		if !ok {
			if m := j.propListeners[n]; m != nil {
				delete(m, "click")
			}
			return
		}
		m := j.propListeners[n]
		if m == nil {
			m = make(map[string]jsListener)
			j.propListeners[n] = m
		}
		m["click"] = jsListener{value: v, call: fn}
	})
	j.accessor(o, "children", func() goja.Value {
		kids := elementChildren(n)
		vals := make([]interface{}, 0, len(kids))
		for _, k := range kids {
			vals = append(vals, j.wrap(k))
		}
		return j.vm.NewArray(vals...)
	}, nil)
	j.accessor(o, "childNodes", func() goja.Value {
		vals := make([]interface{}, 0, len(n.Children))
		for _, k := range n.Children {
			vals = append(vals, j.wrap(k))
		}
		return j.vm.NewArray(vals...)
	}, nil)
	j.accessor(o, "parentNode", func() goja.Value { return j.wrap(n.Parent) }, nil)
	j.accessor(o, "nextSibling", func() goja.Value { return j.wrap(nextSiblingNode(n)) }, nil)
	j.accessor(o, "previousSibling", func() goja.Value { return j.wrap(prevSiblingNode(n)) }, nil)
	j.accessor(o, "firstChild", func() goja.Value {
		if len(n.Children) == 0 {
			return goja.Null()
		}
		return j.wrap(n.Children[0])
	}, nil)
	j.accessor(o, "lastChild", func() goja.Value {
		if len(n.Children) == 0 {
			return goja.Null()
		}
		return j.wrap(n.Children[len(n.Children)-1])
	}, nil)
	j.accessor(o, "childElementCount", func() goja.Value {
		return j.vm.ToValue(len(elementChildren(n)))
	}, nil)
	j.accessor(o, "ownerDocument", func() goja.Value { return j.docObj }, nil)
	j.accessor(o, "isConnected", func() goja.Value { return j.vm.ToValue(isConnected(n)) }, nil)
	j.accessor(o, "parentElement", func() goja.Value {
		if n.Parent != nil && n.Parent.Type == ElementNode {
			return j.wrap(n.Parent)
		}
		return goja.Null()
	}, nil)
	j.accessor(o, "firstElementChild", func() goja.Value {
		kids := elementChildren(n)
		if len(kids) == 0 {
			return goja.Null()
		}
		return j.wrap(kids[0])
	}, nil)
	j.accessor(o, "lastElementChild", func() goja.Value {
		kids := elementChildren(n)
		if len(kids) == 0 {
			return goja.Null()
		}
		return j.wrap(kids[len(kids)-1])
	}, nil)
	j.accessor(o, "nextElementSibling", func() goja.Value {
		for s := nextSiblingNode(n); s != nil; s = nextSiblingNode(s) {
			if s.Type == ElementNode {
				return j.wrap(s)
			}
		}
		return goja.Null()
	}, nil)
	j.accessor(o, "previousElementSibling", func() goja.Value {
		for s := prevSiblingNode(n); s != nil; s = prevSiblingNode(s) {
			if s.Type == ElementNode {
				return j.wrap(s)
			}
		}
		return goja.Null()
	}, nil)

	j.method(o, "getAttribute", func(call goja.FunctionCall) goja.Value {
		return j.vm.ToValue(attrValue(n, call.Argument(0).String()))
	})
	j.method(o, "setAttribute", func(call goja.FunctionCall) goja.Value {
		setAttr(n, call.Argument(0).String(), call.Argument(1).String())
		j.markDirty()
		return goja.Undefined()
	})
	j.method(o, "removeAttribute", func(call goja.FunctionCall) goja.Value {
		removeAttr(n, call.Argument(0).String())
		j.markDirty()
		return goja.Undefined()
	})
	j.method(o, "hasAttribute", func(call goja.FunctionCall) goja.Value {
		return j.vm.ToValue(n.HasAttr(call.Argument(0).String()))
	})
	j.method(o, "appendChild", func(call goja.FunctionCall) goja.Value {
		child := j.unwrap(call.Argument(0))
		if child == nil {
			return goja.Null()
		}
		insertNode(n, child, nil)
		j.markDirty()
		return call.Argument(0)
	})
	j.method(o, "insertBefore", func(call goja.FunctionCall) goja.Value {
		child := j.unwrap(call.Argument(0))
		if child == nil {
			return goja.Null()
		}
		insertNode(n, child, j.unwrap(call.Argument(1)))
		j.markDirty()
		return call.Argument(0)
	})
	j.method(o, "replaceChild", func(call goja.FunctionCall) goja.Value {
		child := j.unwrap(call.Argument(0))
		old := j.unwrap(call.Argument(1))
		if child == nil || old == nil || old.Parent != n {
			return goja.Null()
		}
		insertNode(n, child, old)
		detach(old)
		j.markDirty()
		return call.Argument(1)
	})
	j.method(o, "removeChild", func(call goja.FunctionCall) goja.Value {
		child := j.unwrap(call.Argument(0))
		if child == nil || child.Parent != n {
			return goja.Null()
		}
		detach(child)
		j.markDirty()
		return call.Argument(0)
	})
	j.method(o, "remove", func(goja.FunctionCall) goja.Value {
		detach(n)
		j.markDirty()
		return goja.Undefined()
	})
	j.method(o, "contains", func(call goja.FunctionCall) goja.Value {
		other := j.unwrap(call.Argument(0))
		for p := other; p != nil; p = p.Parent {
			if p == n {
				return j.vm.ToValue(true)
			}
		}
		return j.vm.ToValue(false)
	})
	j.method(o, "hasChildNodes", func(goja.FunctionCall) goja.Value {
		return j.vm.ToValue(len(n.Children) > 0)
	})
	j.method(o, "cloneNode", func(call goja.FunctionCall) goja.Value {
		deep := call.Argument(0).ToBoolean()
		return j.wrap(cloneNode(n, deep))
	})
	j.method(o, "matches", func(call goja.FunctionCall) goja.Value {
		sels := j.parsedSelectors(call.Argument(0).String())
		return j.vm.ToValue(matchSelectors(n, sels))
	})
	j.method(o, "closest", func(call goja.FunctionCall) goja.Value {
		sels := j.parsedSelectors(call.Argument(0).String())
		for p := n; p != nil; p = p.Parent {
			if p.Type == ElementNode && matchSelectors(p, sels) {
				return j.wrap(p)
			}
		}
		return goja.Null()
	})
	j.method(o, "append", func(call goja.FunctionCall) goja.Value {
		for _, arg := range call.Arguments {
			child := j.unwrap(arg)
			if child == nil {
				if s := arg.String(); s != "" {
					child = &Node{Type: TextNode, Text: s}
				}
			}
			if child != nil {
				insertNode(n, child, nil)
			}
		}
		j.markDirty()
		return goja.Undefined()
	})
	j.method(o, "prepend", func(call goja.FunctionCall) goja.Value {
		var ref *Node
		if len(n.Children) > 0 {
			ref = n.Children[0]
		}
		for _, arg := range call.Arguments {
			child := j.unwrap(arg)
			if child == nil {
				if s := arg.String(); s != "" {
					child = &Node{Type: TextNode, Text: s}
				}
			}
			if child != nil {
				insertNode(n, child, ref)
			}
		}
		j.markDirty()
		return goja.Undefined()
	})
	j.method(o, "before", func(call goja.FunctionCall) goja.Value {
		if n.Parent == nil {
			return goja.Undefined()
		}
		for _, arg := range call.Arguments {
			if child := j.unwrap(arg); child != nil {
				insertNode(n.Parent, child, n)
			}
		}
		j.markDirty()
		return goja.Undefined()
	})
	j.method(o, "after", func(call goja.FunctionCall) goja.Value {
		if n.Parent == nil {
			return goja.Undefined()
		}
		ref := nextSiblingNode(n)
		for _, arg := range call.Arguments {
			if child := j.unwrap(arg); child != nil {
				insertNode(n.Parent, child, ref)
			}
		}
		j.markDirty()
		return goja.Undefined()
	})
	j.method(o, "replaceWith", func(call goja.FunctionCall) goja.Value {
		if n.Parent == nil {
			return goja.Undefined()
		}
		parent := n.Parent
		for _, arg := range call.Arguments {
			if child := j.unwrap(arg); child != nil {
				insertNode(parent, child, n)
			}
		}
		detach(n)
		j.markDirty()
		return goja.Undefined()
	})
	j.method(o, "insertAdjacentHTML", func(call goja.FunctionCall) goja.Value {
		pos := strings.ToLower(call.Argument(0).String())
		html := call.Argument(1).String()
		frag := j.parseFragment(html)
		switch pos {
		case "beforebegin":
			if n.Parent != nil {
				for _, c := range frag {
					insertNode(n.Parent, c, n)
				}
			}
		case "afterbegin":
			var ref *Node
			if len(n.Children) > 0 {
				ref = n.Children[0]
			}
			for _, c := range frag {
				insertNode(n, c, ref)
			}
		case "beforeend":
			for _, c := range frag {
				insertNode(n, c, nil)
			}
		case "afterend":
			if n.Parent != nil {
				ref := nextSiblingNode(n)
				for _, c := range frag {
					insertNode(n.Parent, c, ref)
				}
			}
		}
		j.markDirty()
		return goja.Undefined()
	})
	j.method(o, "getRootNode", func(goja.FunctionCall) goja.Value {
		root := n
		for root.Parent != nil {
			root = root.Parent
		}
		return j.wrap(root)
	})
	j.method(o, "replaceChildren", func(call goja.FunctionCall) goja.Value {
		for _, c := range n.Children {
			c.Parent = nil
		}
		n.Children = nil
		for _, arg := range call.Arguments {
			if child := j.unwrap(arg); child != nil {
				detach(child)
				child.Parent = n
				n.Children = append(n.Children, child)
			} else if s := arg.String(); s != "" {
				n.Children = append(n.Children, &Node{Type: TextNode, Text: s, Parent: n})
			}
		}
		j.markDirty()
		return goja.Undefined()
	})
	j.method(o, "addEventListener", func(call goja.FunctionCall) goja.Value {
		j.addNodeListener(n, call.Argument(0).String(), call.Argument(1), call.Argument(2))
		return goja.Undefined()
	})
	j.method(o, "removeEventListener", func(call goja.FunctionCall) goja.Value {
		j.removeNodeListener(n, call.Argument(0).String(), call.Argument(1))
		return goja.Undefined()
	})
	j.method(o, "dispatchEvent", func(call goja.FunctionCall) goja.Value {
		return j.vm.ToValue(j.dispatchNodeEvent(n, call.Argument(0)))
	})
	j.method(o, "querySelector", func(call goja.FunctionCall) goja.Value {
		return j.wrap(j.querySelector(n, call.Argument(0).String(), false))
	})
	j.method(o, "querySelectorAll", func(call goja.FunctionCall) goja.Value {
		return j.nodeArray(j.querySelectorAll(n, call.Argument(0).String(), false))
	})
	j.method(o, "getElementsByTagName", func(call goja.FunctionCall) goja.Value {
		tag := strings.ToLower(call.Argument(0).String())
		nodes := n.FindAll(func(x *Node) bool { return tag == "*" || x.Tag == tag })
		return j.nodeArray(nodes)
	})
	j.method(o, "click", func(call goja.FunctionCall) goja.Value {
		j.fireNodeEvent(n, "click")
		j.markDirty()
		return goja.Undefined()
	})
	j.installElementCompat(o, n)
}

func (j *jsRuntime) nodeArray(nodes []*Node) goja.Value {
	vals := make([]interface{}, 0, len(nodes))
	for _, n := range nodes {
		vals = append(vals, j.wrap(n))
	}
	return j.vm.NewArray(vals...)
}

func (j *jsRuntime) setInnerHTML(n *Node, html string) {
	for _, c := range n.Children {
		c.Parent = nil
	}
	n.Children = nil
	wrapper := ParseHTML("<html><body>" + html + "</body></html>")
	if body := wrapper.FindFirst("body"); body != nil {
		for _, c := range body.Children {
			c.Parent = n
			n.Children = append(n.Children, c)
		}
	}
	j.markDirty()
}

// initCanvasObject adds the minimal canvas surface. There is no real pixel
// buffer: getContext("2d") returns a no-op context so libraries that feature
// test or color-parse canvas do not throw. WebGL contexts are unsupported.
func (j *jsRuntime) initCanvasObject(o *goja.Object, n *Node) {
	dim := func(attr string, def int) (func() goja.Value, func(goja.Value)) {
		get := func() goja.Value {
			if s := attrValue(n, attr); s != "" {
				if v, err := strconv.Atoi(s); err == nil {
					return j.vm.ToValue(v)
				}
			}
			return j.vm.ToValue(def)
		}
		set := func(v goja.Value) {
			setAttr(n, attr, strconv.Itoa(int(v.ToInteger())))
			j.markDirty()
		}
		return get, set
	}
	gw, sw := dim("width", 300)
	gh, sh := dim("height", 150)
	j.accessor(o, "width", gw, sw)
	j.accessor(o, "height", gh, sh)
	j.method(o, "getContext", func(call goja.FunctionCall) goja.Value {
		kind := strings.ToLower(strings.TrimSpace(call.Argument(0).String()))
		if kind != "2d" {
			return goja.Null()
		}
		if ctx := j.canvasContexts[n]; ctx != nil {
			return ctx
		}
		ctx := j.newCanvas2DContext(o)
		j.canvasContexts[n] = ctx
		return ctx
	})
	j.method(o, "toDataURL", func(goja.FunctionCall) goja.Value {
		return j.vm.ToValue("data:image/png;base64,")
	})
}

func (j *jsRuntime) newCanvas2DContext(canvas *goja.Object) goja.Value {
	ctx := j.vm.NewObject()
	_ = ctx.Set("canvas", canvas)
	_ = ctx.Set("fillStyle", "#000000")
	_ = ctx.Set("strokeStyle", "#000000")
	_ = ctx.Set("globalAlpha", 1)
	_ = ctx.Set("globalCompositeOperation", "source-over")
	_ = ctx.Set("lineWidth", 1)
	_ = ctx.Set("lineCap", "butt")
	_ = ctx.Set("lineJoin", "miter")
	_ = ctx.Set("font", "10px sans-serif")
	_ = ctx.Set("textAlign", "start")
	_ = ctx.Set("textBaseline", "alphabetic")
	_ = ctx.Set("shadowBlur", 0)
	_ = ctx.Set("shadowColor", "rgba(0, 0, 0, 0)")
	noop := j.vm.ToValue(func(goja.FunctionCall) goja.Value { return goja.Undefined() })
	for _, name := range []string{
		"save", "restore", "scale", "rotate", "translate", "transform",
		"setTransform", "resetTransform", "clearRect", "fillRect", "strokeRect",
		"beginPath", "closePath", "moveTo", "lineTo", "bezierCurveTo",
		"quadraticCurveTo", "arc", "arcTo", "ellipse", "rect", "fill",
		"stroke", "clip", "drawImage", "putImageData", "setLineDash",
		"fillText", "strokeText", "drawFocusIfNeeded", "scrollPathIntoView",
		"createPattern", "isPointInPath", "isPointInStroke",
	} {
		_ = ctx.Set(name, noop)
	}
	j.method(ctx, "getLineDash", func(goja.FunctionCall) goja.Value { return j.vm.NewArray() })
	j.method(ctx, "measureText", func(call goja.FunctionCall) goja.Value {
		m := j.vm.NewObject()
		_ = m.Set("width", 0)
		_ = m.Set("actualBoundingBoxAscent", 0)
		_ = m.Set("actualBoundingBoxDescent", 0)
		_ = m.Set("actualBoundingBoxLeft", 0)
		_ = m.Set("actualBoundingBoxRight", 0)
		return m
	})
	makeGradient := func(goja.FunctionCall) goja.Value {
		g := j.vm.NewObject()
		_ = g.Set("addColorStop", noop)
		return g
	}
	j.method(ctx, "createLinearGradient", makeGradient)
	j.method(ctx, "createRadialGradient", makeGradient)
	j.method(ctx, "createConicGradient", makeGradient)
	j.method(ctx, "getImageData", func(call goja.FunctionCall) goja.Value {
		w := int(call.Argument(2).ToInteger())
		h := int(call.Argument(3).ToInteger())
		return j.newImageData(w, h)
	})
	j.method(ctx, "createImageData", func(call goja.FunctionCall) goja.Value {
		return j.newImageData(int(call.Argument(0).ToInteger()), int(call.Argument(1).ToInteger()))
	})
	return ctx
}

func (j *jsRuntime) newImageData(w, h int) *goja.Object {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	img := j.vm.NewObject()
	_ = img.Set("width", w)
	_ = img.Set("height", h)
	data := j.vm.NewArray()
	if ctor, ok := j.vm.Get("Uint8ClampedArray").(*goja.Object); ok {
		if arr, err := j.vm.New(ctor, j.vm.ToValue(w*h*4)); err == nil {
			data = arr
		}
	}
	_ = img.Set("data", data)
	return img
}

// initDialogObject adds the HTMLDialogElement subset pages use to open and
// dismiss dialogs. MOB³ does not paint a modal backdrop, but tracking the open
// state keeps the UA dialog rules and close events working.
func (j *jsRuntime) initDialogObject(o *goja.Object, n *Node) {
	j.accessor(o, "open", func() goja.Value {
		return j.vm.ToValue(n.HasAttr("open"))
	}, func(v goja.Value) {
		if v.ToBoolean() {
			setAttr(n, "open", "")
		} else {
			removeAttr(n, "open")
		}
		j.markDirty()
	})
	j.accessor(o, "returnValue", func() goja.Value {
		return j.vm.ToValue(n.DialogReturnValue)
	}, func(v goja.Value) {
		n.DialogReturnValue = v.String()
	})
	open := func(goja.FunctionCall) goja.Value {
		setAttr(n, "open", "")
		j.markDirty()
		return goja.Undefined()
	}
	j.method(o, "show", open)
	j.method(o, "showModal", open)
	j.method(o, "close", func(call goja.FunctionCall) goja.Value {
		if !n.HasAttr("open") {
			return goja.Undefined()
		}
		if v := call.Argument(0); !goja.IsUndefined(v) && !goja.IsNull(v) {
			n.DialogReturnValue = v.String()
		}
		removeAttr(n, "open")
		j.markDirty()
		j.fireNodeEvent(n, "close")
		return goja.Undefined()
	})
}

// implementationObject backs document.implementation. hasFeature reports true
// for legacy feature checks (Raphael uses it for SVG detection), and
// createHTMLDocument returns a detached document for jQuery-style parsing.
func (j *jsRuntime) implementationObject() *goja.Object {
	imp := j.vm.NewObject()
	j.method(imp, "hasFeature", func(goja.FunctionCall) goja.Value { return j.vm.ToValue(true) })
	j.method(imp, "createHTMLDocument", func(call goja.FunctionCall) goja.Value {
		return j.newDetachedDocument(call.Argument(0).String())
	})
	j.method(imp, "createDocument", func(goja.FunctionCall) goja.Value {
		return j.newDetachedDocument("")
	})
	return imp
}

func (j *jsRuntime) newDetachedDocument(title string) *goja.Object {
	html := &Node{Type: ElementNode, Tag: "html"}
	head := &Node{Type: ElementNode, Tag: "head", Parent: html}
	body := &Node{Type: ElementNode, Tag: "body", Parent: html}
	html.Children = []*Node{head, body}
	if title != "" {
		titleNode := &Node{Type: ElementNode, Tag: "title", Parent: head}
		titleNode.Children = []*Node{{Type: TextNode, Text: title, Parent: titleNode}}
		head.Children = append(head.Children, titleNode)
	}
	doc := j.vm.NewObject()
	_ = doc.Set("nodeType", 9)
	_ = doc.Set("nodeName", "#document")
	j.accessor(doc, "documentElement", func() goja.Value { return j.wrap(html) }, nil)
	j.accessor(doc, "head", func() goja.Value { return j.wrap(head) }, nil)
	j.accessor(doc, "body", func() goja.Value { return j.wrap(body) }, nil)
	j.accessor(doc, "defaultView", func() goja.Value { return j.winObj }, nil)
	j.accessor(doc, "title", func() goja.Value { return j.vm.ToValue(title) }, nil)
	_ = doc.Set("implementation", j.implementationObject())
	j.method(doc, "createElement", func(call goja.FunctionCall) goja.Value {
		tag := strings.ToLower(strings.TrimSpace(call.Argument(0).String()))
		if tag == "" {
			tag = "div"
		}
		return j.wrap(&Node{Type: ElementNode, Tag: tag})
	})
	j.method(doc, "createElementNS", func(call goja.FunctionCall) goja.Value {
		return j.wrap(&Node{Type: ElementNode, Tag: strings.ToLower(call.Argument(1).String())})
	})
	j.method(doc, "createTextNode", func(call goja.FunctionCall) goja.Value {
		return j.wrap(&Node{Type: TextNode, Text: call.Argument(0).String()})
	})
	j.method(doc, "createComment", func(call goja.FunctionCall) goja.Value {
		return j.wrap(&Node{Type: TextNode, Text: call.Argument(0).String()})
	})
	j.method(doc, "createDocumentFragment", func(goja.FunctionCall) goja.Value {
		return j.wrap(&Node{Type: ElementNode, Tag: "#fragment"})
	})
	j.method(doc, "getElementById", func(call goja.FunctionCall) goja.Value {
		id := call.Argument(0).String()
		var found *Node
		html.FindAll(func(x *Node) bool {
			if found == nil && attrValue(x, "id") == id {
				found = x
				return true
			}
			return false
		})
		return j.wrap(found)
	})
	j.method(doc, "getElementsByTagName", func(call goja.FunctionCall) goja.Value {
		tag := strings.ToLower(call.Argument(0).String())
		nodes := html.FindAll(func(x *Node) bool { return tag == "*" || x.Tag == tag })
		return j.nodeArray(nodes)
	})
	return doc
}

// computedStyleObject returns a getComputedStyle-like snapshot. Values come
// from the resolved style when layout has run; detached or not-yet-styled
// elements get sensible defaults.
func (j *jsRuntime) computedStyleObject(n *Node) *goja.Object {
	o := j.vm.NewObject()
	st := (*Style)(nil)
	if n != nil {
		st = n.Style
	}
	if st == nil {
		st = &Style{}
	}
	font := st.FontSize
	if font <= 0 {
		font = 16
	}
	line := st.LineHeight
	if line <= 0 {
		line = 1
	}
	display := st.Display
	if display == "" {
		display = "block"
	}
	weights := "400"
	if st.Bold {
		weights = "700"
	}
	style := "normal"
	if st.Italic {
		style = "italic"
	}
	decoration := "none"
	switch {
	case st.Underline && st.Strike:
		decoration = "underline line-through"
	case st.Underline:
		decoration = "underline"
	case st.Strike:
		decoration = "line-through"
	}
	props := map[string]string{
		"display":          display,
		"visibility":       "visible",
		"position":         styleDefault(st.Position, "static"),
		"float":            "none",
		"css-float":        "none",
		"box-sizing":       "content-box",
		"opacity":          "1",
		"color":            computedColor(st.Color, "rgb(0, 0, 0)"),
		"background-color": computedColor(st.Background, "rgba(0, 0, 0, 0)"),
		"font-size":        cssPxString(font),
		"font-weight":      weights,
		"font-style":       style,
		"line-height":      cssPxString(line * font),
		"text-align":       styleDefault(st.TextAlign, "start"),
		"text-transform":   styleDefault(st.TextTransform, "none"),
		"text-indent":      lengthCSS(st.TextIndent, "0px"),
		"text-decoration":  decoration,
		"white-space":      styleDefault(st.WhiteSpace, "normal"),
		"overflow":         styleDefault(st.Overflow, "visible"),
		"overflow-x":       styleDefault(st.Overflow, "visible"),
		"overflow-y":       styleDefault(st.Overflow, "visible"),
		"width":            lengthCSS(st.Width, "auto"),
		"height":           lengthCSS(st.Height, "auto"),
		"max-width":        lengthCSS(st.MaxWidth, "none"),
		"list-style-type":  styleDefault(st.ListStyle, "disc"),
		"flex-direction":   styleDefault(st.FlexDir, "row"),
		"flex-grow":        strconv.FormatFloat(st.FlexGrow, 'f', -1, 64),
		"gap":              lengthCSS(st.Gap, "normal"),
		"border-color":     computedColor(st.BorderColor, "rgb(0, 0, 0)"),
	}
	sides := []string{"top", "right", "bottom", "left"}
	for i, side := range sides {
		props["margin-"+side] = lengthCSS(st.Margin[i], "0px")
		props["padding-"+side] = lengthCSS(st.Padding[i], "0px")
		props["border-"+side+"-width"] = lengthCSS(st.Border[i], "0px")
	}
	for k, v := range props {
		_ = o.Set(k, v)
		if camel := kebabToCamel(k); camel != k {
			_ = o.Set(camel, v)
		}
	}
	j.method(o, "getPropertyValue", func(call goja.FunctionCall) goja.Value {
		name := strings.ToLower(strings.TrimSpace(call.Argument(0).String()))
		if v, ok := props[name]; ok {
			return j.vm.ToValue(v)
		}
		if v, ok := props[camelToKebab(name)]; ok {
			return j.vm.ToValue(v)
		}
		return j.vm.ToValue("")
	})
	j.method(o, "getPropertyPriority", func(goja.FunctionCall) goja.Value {
		return j.vm.ToValue("")
	})
	j.method(o, "item", func(call goja.FunctionCall) goja.Value {
		return j.vm.ToValue("")
	})
	_ = o.Set("length", len(props))
	return o
}

func styleDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func computedColor(c Color, def string) string {
	if !c.Set {
		return def
	}
	if c.A >= 255 {
		return "rgb(" + strconv.Itoa(int(c.R)) + ", " + strconv.Itoa(int(c.G)) + ", " + strconv.Itoa(int(c.B)) + ")"
	}
	return "rgba(" + strconv.Itoa(int(c.R)) + ", " + strconv.Itoa(int(c.G)) + ", " +
		strconv.Itoa(int(c.B)) + ", " + strconv.FormatFloat(float64(c.A)/255, 'f', 3, 64) + ")"
}

func cssPxString(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64) + "px"
}

func lengthCSS(l Length, def string) string {
	if !l.Set || l.Auto {
		return def
	}
	switch l.U {
	case UnitPercent:
		return strconv.FormatFloat(l.V, 'f', -1, 64) + "%"
	case UnitEM:
		return strconv.FormatFloat(l.V, 'f', -1, 64) + "em"
	case UnitREM:
		return strconv.FormatFloat(l.V, 'f', -1, 64) + "rem"
	case UnitPT:
		return strconv.FormatFloat(l.V, 'f', -1, 64) + "pt"
	case UnitCH:
		return strconv.FormatFloat(l.V, 'f', -1, 64) + "ch"
	default:
		return cssPxString(l.V)
	}
}

func kebabToCamel(s string) string {
	parts := strings.Split(s, "-")
	for i := 1; i < len(parts); i++ {
		if parts[i] != "" {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

func camelToKebab(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			b.WriteByte('-')
			b.WriteRune(r + ('a' - 'A'))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (j *jsRuntime) documentObject() *goja.Object {
	o := j.vm.NewObject()
	root := j.page.Root
	html := root.FindFirst("html")
	var body, head, title *Node
	if html != nil {
		body = html.FindFirst("body")
		head = html.FindFirst("head")
	}
	if head != nil {
		title = head.FindFirst("title")
	}
	_ = o.Set("nodeType", 9)
	_ = o.Set("nodeName", "#document")
	j.accessor(o, "documentElement", func() goja.Value { return j.wrap(html) }, nil)
	j.accessor(o, "body", func() goja.Value { return j.wrap(body) }, nil)
	j.accessor(o, "head", func() goja.Value { return j.wrap(head) }, nil)
	j.accessor(o, "defaultView", func() goja.Value { return j.winObj }, nil)
	_ = o.Set("implementation", j.implementationObject())
	j.accessor(o, "title", func() goja.Value {
		if title == nil {
			return j.vm.ToValue("")
		}
		return j.vm.ToValue(strings.TrimSpace(title.TextContent()))
	}, func(v goja.Value) {
		s := v.String()
		if title == nil {
			if head == nil {
				if html == nil {
					return
				}
				head = &Node{Type: ElementNode, Tag: "head", Parent: html}
				html.Children = append([]*Node{head}, html.Children...)
			}
			title = &Node{Type: ElementNode, Tag: "title", Parent: head}
			head.Children = append(head.Children, title)
		}
		title.Children = []*Node{{Type: TextNode, Text: s, Parent: title}}
		if j.titleSet != nil {
			j.titleSet(s)
		}
		j.markDirty()
	})
	j.accessor(o, "readyState", func() goja.Value { return j.vm.ToValue("complete") }, nil)
	j.accessor(o, "compatMode", func() goja.Value { return j.vm.ToValue("CSS1Compat") }, nil)
	j.accessor(o, "scrollingElement", func() goja.Value { return j.wrap(body) }, nil)
	j.accessor(o, "currentScript", func() goja.Value {
		if j.currentScript == nil {
			return goja.Null()
		}
		return j.currentScript
	}, nil)
	if j.page != nil && j.page.URL != nil {
		base := j.page.URL.String()
		j.accessor(o, "baseURI", func() goja.Value { return j.vm.ToValue(base) }, nil)
	}
	_ = o.Set("styleSheets", j.vm.NewArray())
	fonts := j.vm.NewObject()
	fontsNoop := j.vm.ToValue(func(goja.FunctionCall) goja.Value { return goja.Undefined() })
	_ = fonts.Set("addEventListener", fontsNoop)
	_ = fonts.Set("removeEventListener", fontsNoop)
	_ = fonts.Set("check", j.vm.ToValue(func(goja.FunctionCall) goja.Value { return j.vm.ToValue(true) }))
	if ready, err := j.vm.RunString("Promise.resolve()"); err == nil {
		_ = fonts.Set("ready", ready)
	}
	_ = o.Set("fonts", fonts)
	j.accessor(o, "cookie", func() goja.Value {
		if j.page == nil || j.page.URL == nil || j.b == nil || j.b.loader == nil || j.b.loader.cookies == nil {
			return j.vm.ToValue("")
		}
		cookies := j.b.loader.cookies.documentCookies(j.page.URL)
		parts := make([]string, 0, len(cookies))
		for _, c := range cookies {
			parts = append(parts, c.Name+"="+c.Value)
		}
		return j.vm.ToValue(strings.Join(parts, "; "))
	}, func(v goja.Value) {
		raw := strings.TrimSpace(v.String())
		if raw == "" || j.page == nil || j.page.URL == nil || j.b == nil || j.b.loader == nil || j.b.loader.cookies == nil {
			return
		}
		c, err := http.ParseSetCookie(raw)
		if err != nil || c.Name == "" {
			return
		}
		j.b.loader.cookies.setDocumentCookie(j.page.URL, c)
	})
	j.method(o, "write", func(call goja.FunctionCall) goja.Value {
		var b strings.Builder
		for _, a := range call.Arguments {
			b.WriteString(a.String())
		}
		j.documentWrite(b.String(), false)
		return goja.Undefined()
	})
	j.method(o, "writeln", func(call goja.FunctionCall) goja.Value {
		var b strings.Builder
		for _, a := range call.Arguments {
			b.WriteString(a.String())
		}
		j.documentWrite(b.String(), true)
		return goja.Undefined()
	})
	j.method(o, "open", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
	j.method(o, "close", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
	j.accessor(o, "visibilityState", func() goja.Value { return j.vm.ToValue("visible") }, nil)
	j.accessor(o, "hidden", func() goja.Value { return j.vm.ToValue(false) }, nil)
	_ = o.Set("location", j.locObj)
	if j.page.URL != nil {
		u := j.page.URL.String()
		j.accessor(o, "URL", func() goja.Value { return j.vm.ToValue(u) }, nil)
		j.accessor(o, "documentURI", func() goja.Value { return j.vm.ToValue(u) }, nil)
	}
	j.method(o, "getElementById", func(call goja.FunctionCall) goja.Value {
		id := call.Argument(0).String()
		var found *Node
		root.FindAll(func(x *Node) bool {
			if found == nil && attrValue(x, "id") == id {
				found = x
				return true
			}
			return false
		})
		return j.wrap(found)
	})
	j.method(o, "querySelector", func(call goja.FunctionCall) goja.Value {
		return j.wrap(j.querySelector(root, call.Argument(0).String(), true))
	})
	j.method(o, "querySelectorAll", func(call goja.FunctionCall) goja.Value {
		return j.nodeArray(j.querySelectorAll(root, call.Argument(0).String(), true))
	})
	j.method(o, "getElementsByTagName", func(call goja.FunctionCall) goja.Value {
		tag := strings.ToLower(call.Argument(0).String())
		nodes := root.FindAll(func(x *Node) bool { return tag == "*" || x.Tag == tag })
		return j.nodeArray(nodes)
	})
	j.method(o, "createElement", func(call goja.FunctionCall) goja.Value {
		tag := strings.ToLower(strings.TrimSpace(call.Argument(0).String()))
		if tag == "" {
			tag = "div"
		}
		return j.wrap(&Node{Type: ElementNode, Tag: tag})
	})
	j.method(o, "createTextNode", func(call goja.FunctionCall) goja.Value {
		return j.wrap(&Node{Type: TextNode, Text: call.Argument(0).String()})
	})
	j.method(o, "createEvent", func(call goja.FunctionCall) goja.Value {
		return j.createEvent(call.Argument(0).String())
	})
	j.method(o, "dispatchEvent", func(call goja.FunctionCall) goja.Value {
		return j.vm.ToValue(j.dispatchDocumentEvent(call.Argument(0)))
	})
	j.method(o, "createDocumentFragment", func(goja.FunctionCall) goja.Value {
		return j.wrap(&Node{Type: ElementNode, Tag: "#fragment"})
	})
	j.method(o, "importNode", func(call goja.FunctionCall) goja.Value {
		node := j.unwrap(call.Argument(0))
		if node == nil {
			return goja.Null()
		}
		return j.wrap(cloneNode(node, call.Argument(1).ToBoolean()))
	})
	j.method(o, "adoptNode", func(call goja.FunctionCall) goja.Value {
		node := j.unwrap(call.Argument(0))
		if node == nil {
			return goja.Null()
		}
		detach(node)
		return call.Argument(0)
	})
	j.method(o, "createElementNS", func(call goja.FunctionCall) goja.Value {
		return j.wrap(&Node{Type: ElementNode, Tag: strings.ToLower(call.Argument(1).String())})
	})
	j.method(o, "getElementsByClassName", func(call goja.FunctionCall) goja.Value {
		want := strings.Fields(call.Argument(0).String())
		nodes := root.FindAll(func(x *Node) bool {
			classes := x.ClassList()
			for _, w := range want {
				found := false
				for _, c := range classes {
					if c == w {
						found = true
						break
					}
				}
				if !found {
					return false
				}
			}
			return len(want) > 0
		})
		return j.nodeArray(nodes)
	})
	j.method(o, "addEventListener", func(call goja.FunctionCall) goja.Value {
		j.addListener(&j.docListeners, call.Argument(0).String(), call.Argument(1), call.Argument(2))
		return goja.Undefined()
	})
	j.method(o, "removeEventListener", func(call goja.FunctionCall) goja.Value {
		j.removeListener(j.docListeners, call.Argument(0).String(), call.Argument(1))
		return goja.Undefined()
	})
	j.installDocumentCompat(o, root)
	return o
}

// parsedSelectors caches querySelector selector parsing per page runtime.
func (j *jsRuntime) parsedSelectors(sel string) []*Selector {
	if s, ok := j.selectors[sel]; ok {
		return s
	}
	s := parseSelectors(sel)
	j.selectors[sel] = s
	return s
}

// parseSelectors parses a comma-separated selector list, ignoring invalid
// entries.
func parseSelectors(sel string) []*Selector {
	var out []*Selector
	for _, part := range splitTopLevel(sel, ',') {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if s, err := ParseSelector(part); err == nil {
			out = append(out, s)
		}
	}
	return out
}

func matchSelectors(n *Node, sels []*Selector) bool {
	for _, s := range sels {
		if s.Match(n) {
			return true
		}
	}
	return false
}

func (j *jsRuntime) querySelector(root *Node, sel string, includeRoot bool) *Node {
	sels := j.parsedSelectors(sel)
	if len(sels) == 0 || root == nil {
		return nil
	}
	var found *Node
	var walk func(*Node)
	walk = func(x *Node) {
		if found != nil {
			return
		}
		for _, c := range x.Children {
			if c.Type != ElementNode {
				continue
			}
			if matchSelectors(c, sels) {
				found = c
				return
			}
			walk(c)
			if found != nil {
				return
			}
		}
	}
	if includeRoot && root.Type == ElementNode && matchSelectors(root, sels) {
		return root
	}
	walk(root)
	return found
}

func (j *jsRuntime) querySelectorAll(root *Node, sel string, includeRoot bool) []*Node {
	sels := j.parsedSelectors(sel)
	if len(sels) == 0 || root == nil {
		return nil
	}
	var out []*Node
	if includeRoot && root.Type == ElementNode && matchSelectors(root, sels) {
		out = append(out, root)
	}
	root.FindAll(func(x *Node) bool {
		if matchSelectors(x, sels) {
			out = append(out, x)
		}
		return false
	})
	return out
}

var styleProps = map[string]string{
	"color":                    "color",
	"background":               "background",
	"backgroundColor":          "background-color",
	"display":                  "display",
	"fontSize":                 "font-size",
	"fontWeight":               "font-weight",
	"fontStyle":                "font-style",
	"fontFamily":               "font-family",
	"lineHeight":               "line-height",
	"textAlign":                "text-align",
	"textDecoration":           "text-decoration",
	"textTransform":            "text-transform",
	"margin":                   "margin",
	"padding":                  "padding",
	"border":                   "border",
	"width":                    "width",
	"height":                   "height",
	"maxWidth":                 "max-width",
	"opacity":                  "opacity",
	"whiteSpace":               "white-space",
	"visibility":               "visibility",
	"transition":               "transition",
	"transitionProperty":       "transition-property",
	"transitionDuration":       "transition-duration",
	"transitionTimingFunction": "transition-timing-function",
	"transitionDelay":          "transition-delay",
	"transform":                "transform",
	"transformOrigin":          "transform-origin",
	"transformStyle":           "transform-style",
	"animation":                "animation",
	"animationName":            "animation-name",
	"animationDuration":        "animation-duration",
	"animationTimingFunction":  "animation-timing-function",
	"animationDelay":           "animation-delay",
	"animationIterationCount":  "animation-iteration-count",
	"animationDirection":       "animation-direction",
	"animationFillMode":        "animation-fill-mode",
	"animationPlayState":       "animation-play-state",
	"willChange":               "will-change",
	"scrollBehavior":           "scroll-behavior",
	"WebkitTransition":         "transition",
	"WebkitTransform":          "transform",
	"webkitTransition":         "transition",
	"webkitTransform":          "transform",
	"MozTransition":            "transition",
	"MozTransform":             "transform",
}

func styleGet(n *Node, prop string) string {
	for _, d := range ParseDeclarations(attrValue(n, "style")) {
		if strings.EqualFold(d.Prop, prop) {
			return d.Value
		}
	}
	return ""
}

func styleSet(n *Node, prop, value string) {
	decls := ParseDeclarations(attrValue(n, "style"))
	found := false
	for i := range decls {
		if strings.EqualFold(decls[i].Prop, prop) {
			found = true
			if value == "" {
				decls = append(decls[:i], decls[i+1:]...)
			} else {
				decls[i].Value = value
			}
			break
		}
	}
	if !found && value != "" {
		decls = append(decls, Declaration{Prop: prop, Value: value})
	}
	parts := make([]string, 0, len(decls))
	for _, d := range decls {
		parts = append(parts, d.Prop+": "+d.Value)
	}
	setAttr(n, "style", strings.Join(parts, "; "))
}

func (j *jsRuntime) styleObject(n *Node) *goja.Object {
	o := j.vm.NewObject()
	j.accessor(o, "cssText", func() goja.Value {
		return j.vm.ToValue(attrValue(n, "style"))
	}, func(v goja.Value) {
		setAttr(n, "style", v.String())
		j.markDirty()
	})
	j.method(o, "setProperty", func(call goja.FunctionCall) goja.Value {
		styleSet(n, call.Argument(0).String(), call.Argument(1).String())
		j.markDirty()
		return goja.Undefined()
	})
	j.method(o, "getPropertyValue", func(call goja.FunctionCall) goja.Value {
		return j.vm.ToValue(styleGet(n, call.Argument(0).String()))
	})
	j.method(o, "removeProperty", func(call goja.FunctionCall) goja.Value {
		prop := call.Argument(0).String()
		old := styleGet(n, prop)
		styleSet(n, prop, "")
		j.markDirty()
		return j.vm.ToValue(old)
	})
	for jsName, cssName := range styleProps {
		jsName, cssName := jsName, cssName
		j.accessor(o, jsName, func() goja.Value {
			return j.vm.ToValue(styleGet(n, cssName))
		}, func(v goja.Value) {
			styleSet(n, cssName, v.String())
			j.markDirty()
		})
	}
	return o
}

func (j *jsRuntime) classListObject(n *Node) *goja.Object {
	o := j.vm.NewObject()
	get := func() []string { return n.ClassList() }
	put := func(list []string) {
		setAttr(n, "class", strings.Join(list, " "))
		j.markDirty()
	}
	j.accessor(o, "value", func() goja.Value {
		return j.vm.ToValue(strings.Join(get(), " "))
	}, func(v goja.Value) {
		put(strings.Fields(v.String()))
	})
	j.accessor(o, "length", func() goja.Value {
		return j.vm.ToValue(len(get()))
	}, nil)
	j.method(o, "contains", func(call goja.FunctionCall) goja.Value {
		name := call.Argument(0).String()
		for _, c := range get() {
			if c == name {
				return j.vm.ToValue(true)
			}
		}
		return j.vm.ToValue(false)
	})
	j.method(o, "add", func(call goja.FunctionCall) goja.Value {
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
		return goja.Undefined()
	})
	j.method(o, "remove", func(call goja.FunctionCall) goja.Value {
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
		return goja.Undefined()
	})
	j.method(o, "toggle", func(call goja.FunctionCall) goja.Value {
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
		if !goja.IsUndefined(force) && !goja.IsNull(force) {
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

func serializeChildren(n *Node) string {
	var b strings.Builder
	for _, c := range n.Children {
		serializeNode(&b, c)
	}
	return b.String()
}

func serializeNode(b *strings.Builder, n *Node) {
	if n == nil {
		return
	}
	if n.Type == TextNode {
		b.WriteString(textEscape(n.Text))
		return
	}
	if n.Type != ElementNode {
		return
	}
	b.WriteString("<")
	b.WriteString(n.Tag)
	for _, a := range n.Attrs {
		b.WriteString(" ")
		b.WriteString(a.Key)
		b.WriteString(`="`)
		b.WriteString(EscapeHTML(a.Value))
		b.WriteString(`"`)
	}
	b.WriteString(">")
	if isVoidTag(n.Tag) {
		return
	}
	for _, c := range n.Children {
		serializeNode(b, c)
	}
	b.WriteString("</")
	b.WriteString(n.Tag)
	b.WriteString(">")
}

func textEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func isVoidTag(tag string) bool {
	switch tag {
	case "area", "base", "br", "col", "embed", "hr", "img", "input",
		"link", "meta", "param", "source", "track", "wbr":
		return true
	}
	return false
}
