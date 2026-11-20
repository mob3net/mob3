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

// protoAccessor defines one shared accessor on a prototype. The node is
// resolved from the receiver, so a single getter/setter pair serves every
// element instead of allocating a closure per wrapper (the per-element cost
// dominated JS-heavy pages' memory).
func (j *jsRuntime) protoAccessor(p *goja.Object, name string, get func(n *Node) goja.Value, set func(n *Node, v goja.Value)) {
	if p == nil {
		return
	}
	var g, s goja.Value
	if get != nil {
		g = j.vm.ToValue(func(call goja.FunctionCall) goja.Value {
			n := j.thisNode(call)
			if n == nil {
				return goja.Undefined()
			}
			return get(n)
		})
	}
	if set != nil {
		s = j.vm.ToValue(func(call goja.FunctionCall) goja.Value {
			n := j.thisNode(call)
			if n == nil {
				return goja.Undefined()
			}
			set(n, call.Argument(0))
			return goja.Undefined()
		})
	}
	_ = p.DefineAccessorProperty(name, g, s, goja.FLAG_TRUE, goja.FLAG_FALSE)
}

// protoMethod defines one shared method on a prototype.
func (j *jsRuntime) protoMethod(p *goja.Object, name string, fn func(n *Node, call goja.FunctionCall) goja.Value) {
	if p == nil {
		return
	}
	_ = p.Set(name, j.vm.ToValue(func(call goja.FunctionCall) goja.Value {
		n := j.thisNode(call)
		if n == nil {
			return goja.Undefined()
		}
		return fn(n, call)
	}))
}

// thisNode resolves the wrapper that received a prototype accessor or method.
func (j *jsRuntime) thisNode(call goja.FunctionCall) *Node {
	if o, ok := call.This.(*goja.Object); ok {
		return j.rev[o]
	}
	return nil
}

// initTextInstance sets the per-object fields of a text wrapper. Everything
// else is shared through the prototype.
func (j *jsRuntime) initTextInstance(o *goja.Object) {
	_ = o.Set("nodeType", 3)
	_ = o.Set("nodeName", "#text")
	_ = o.Set("tagName", "")
}

// initElementInstance sets the per-object fields of an element wrapper and
// installs the tag-specific extras (canvas context, dialog/form methods).
func (j *jsRuntime) initElementInstance(o *goja.Object, n *Node) {
	nodeType := 1
	nodeName := strings.ToUpper(n.Tag)
	if n.Tag == "#fragment" {
		// document.createDocumentFragment returns a lightweight node stored as
		// an element; scripts (jQuery especially) branch on nodeType 11.
		nodeType = 11
		nodeName = "#document-fragment"
	}
	_ = o.Set("nodeType", nodeType)
	_ = o.Set("tagName", strings.ToUpper(n.Tag))
	_ = o.Set("nodeName", nodeName)
	switch n.Tag {
	case "canvas":
		j.initCanvasObject(o, n)
	case "dialog":
		j.initDialogObject(o, n)
	case "form":
		j.initFormObject(o, n)
	}
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
		j.initTextInstance(o)
	} else {
		j.setElementPrototype(o, n)
		j.initElementInstance(o, n)
	}
	return o
}

func (j *jsRuntime) initTextProto(p *goja.Object) {
	// Text nodes are Nodes too. jQuery (and plugins such as custom-scrollbar)
	// call `node.ownerDocument`, `childNodes` and `contains` on every node in a
	// collection, including text nodes; a missing `ownerDocument` made
	// `wrapAll` throw and left the plugin's DOM surgery half-applied.
	j.protoAccessor(p, "ownerDocument", func(n *Node) goja.Value { return j.docObj }, nil)
	j.protoAccessor(p, "parentElement", func(n *Node) goja.Value {
		if n.Parent != nil && n.Parent.Type == ElementNode {
			return j.wrap(n.Parent)
		}
		return goja.Null()
	}, nil)
	j.protoAccessor(p, "isConnected", func(n *Node) goja.Value { return j.vm.ToValue(isConnected(n)) }, nil)
	j.protoAccessor(p, "childNodes", func(n *Node) goja.Value { return j.vm.NewArray() }, nil)
	j.protoAccessor(p, "children", func(n *Node) goja.Value { return j.vm.NewArray() }, nil)
	j.protoAccessor(p, "childElementCount", func(n *Node) goja.Value { return j.vm.ToValue(0) }, nil)
	j.protoAccessor(p, "firstChild", func(n *Node) goja.Value { return goja.Null() }, nil)
	j.protoAccessor(p, "lastChild", func(n *Node) goja.Value { return goja.Null() }, nil)
	j.protoAccessor(p, "firstElementChild", func(n *Node) goja.Value { return goja.Null() }, nil)
	j.protoAccessor(p, "lastElementChild", func(n *Node) goja.Value { return goja.Null() }, nil)
	j.protoMethod(p, "hasChildNodes", func(n *Node, _ goja.FunctionCall) goja.Value { return j.vm.ToValue(false) })
	j.protoMethod(p, "contains", func(n *Node, _ goja.FunctionCall) goja.Value { return j.vm.ToValue(false) })
	j.protoMethod(p, "getRootNode", func(n *Node, _ goja.FunctionCall) goja.Value { return j.docObj })
	j.protoAccessor(p, "textContent", func(n *Node) goja.Value {
		return j.vm.ToValue(n.Text)
	}, func(n *Node, v goja.Value) {
		n.Text = v.String()
		j.markDirty()
	})
	j.protoAccessor(p, "nodeValue", func(n *Node) goja.Value {
		return j.vm.ToValue(n.Text)
	}, func(n *Node, v goja.Value) {
		n.Text = v.String()
		j.markDirty()
	})
	j.protoAccessor(p, "parentNode", func(n *Node) goja.Value { return j.wrap(n.Parent) }, nil)
	j.protoAccessor(p, "nextSibling", func(n *Node) goja.Value { return j.wrap(nextSiblingNode(n)) }, nil)
	j.protoAccessor(p, "previousSibling", func(n *Node) goja.Value { return j.wrap(prevSiblingNode(n)) }, nil)
	j.protoMethod(p, "remove", func(n *Node, _ goja.FunctionCall) goja.Value {
		detach(n)
		j.markDirty()
		return goja.Undefined()
	})
	j.protoMethod(p, "cloneNode", func(n *Node, call goja.FunctionCall) goja.Value {
		return j.wrap(cloneNode(n, call.Argument(0).ToBoolean()))
	})
}

func (j *jsRuntime) initElementProto(p *goja.Object) {
	j.protoAccessor(p, "id", func(n *Node) goja.Value { return j.vm.ToValue(attrValue(n, "id")) }, func(n *Node, v goja.Value) {
		setAttr(n, "id", v.String())
		j.markDirty()
	})
	j.protoAccessor(p, "className", func(n *Node) goja.Value { return j.vm.ToValue(attrValue(n, "class")) }, func(n *Node, v goja.Value) {
		setAttr(n, "class", v.String())
		j.markDirty()
	})
	reflectString := func(jsName, attr string) {
		j.protoAccessor(p, jsName, func(n *Node) goja.Value { return j.vm.ToValue(attrValue(n, attr)) }, func(n *Node, v goja.Value) {
			setAttr(n, attr, v.String())
			j.markDirty()
		})
	}
	reflectBool := func(jsName, attr string) {
		j.protoAccessor(p, jsName, func(n *Node) goja.Value { return j.vm.ToValue(n.HasAttr(attr)) }, func(n *Node, v goja.Value) {
			if v.ToBoolean() {
				setAttr(n, attr, "")
			} else {
				removeAttr(n, attr)
			}
			j.markDirty()
		})
	}
	reflectString("title", "title")
	reflectString("lang", "lang")
	reflectString("dir", "dir")
	reflectString("accessKey", "accesskey")
	reflectString("slot", "slot")
	reflectBool("hidden", "hidden")
	reflectBool("inert", "inert")
	j.protoAccessor(p, "draggable", func(n *Node) goja.Value {
		return j.vm.ToValue(attrValue(n, "draggable") == "true")
	}, func(n *Node, v goja.Value) {
		if v.ToBoolean() {
			setAttr(n, "draggable", "true")
		} else {
			setAttr(n, "draggable", "false")
		}
		j.markDirty()
	})
	j.protoAccessor(p, "spellcheck", func(n *Node) goja.Value {
		if attrValue(n, "spellcheck") == "false" {
			return j.vm.ToValue(false)
		}
		return j.vm.ToValue(true)
	}, func(n *Node, v goja.Value) {
		if v.ToBoolean() {
			setAttr(n, "spellcheck", "true")
		} else {
			setAttr(n, "spellcheck", "false")
		}
		j.markDirty()
	})
	j.protoAccessor(p, "translate", func(n *Node) goja.Value {
		return j.vm.ToValue(attrValue(n, "translate") != "no")
	}, func(n *Node, v goja.Value) {
		if v.ToBoolean() {
			setAttr(n, "translate", "yes")
		} else {
			setAttr(n, "translate", "no")
		}
		j.markDirty()
	})
	j.protoAccessor(p, "contentEditable", func(n *Node) goja.Value {
		if v := attrValue(n, "contenteditable"); v != "" {
			return j.vm.ToValue(v)
		}
		return j.vm.ToValue("inherit")
	}, func(n *Node, v goja.Value) {
		setAttr(n, "contenteditable", v.String())
		j.markDirty()
	})
	j.protoAccessor(p, "isContentEditable", func(n *Node) goja.Value {
		return j.vm.ToValue(isEditableNode(n))
	}, nil)
	j.protoAccessor(p, "tabIndex", func(n *Node) goja.Value {
		return j.vm.ToValue(nodeTabIndex(n))
	}, func(n *Node, v goja.Value) {
		setAttr(n, "tabindex", strconv.Itoa(int(v.ToInteger())))
		j.markDirty()
	})
	j.protoAccessor(p, "outerText", func(n *Node) goja.Value {
		return j.vm.ToValue(n.TextContent())
	}, func(n *Node, v goja.Value) {
		replaceNodeWithText(n, v.String())
		j.markDirty()
	})
	j.protoAccessor(p, "href", func(n *Node) goja.Value { return j.vm.ToValue(attrValue(n, "href")) }, func(n *Node, v goja.Value) {
		setAttr(n, "href", v.String())
		j.markDirty()
	})
	j.protoAccessor(p, "value", func(n *Node) goja.Value {
		return j.vm.ToValue(inputValue(n))
	}, func(n *Node, v goja.Value) {
		n.Value = v.String()
		n.ValueSet = true
		j.markDirty()
	})
	j.protoAccessor(p, "checked", func(n *Node) goja.Value {
		return j.vm.ToValue(n.HasAttr("checked"))
	}, func(n *Node, v goja.Value) {
		if v.ToBoolean() {
			setAttr(n, "checked", "")
		} else {
			removeAttr(n, "checked")
		}
		j.markDirty()
	})
	j.protoAccessor(p, "form", func(n *Node) goja.Value {
		return j.wrap(nearestForm(n))
	}, nil)
	for jsName, attrName := range map[string]string{
		"src": "src", "alt": "alt", "width": "width", "height": "height",
	} {
		jsName, attrName := jsName, attrName
		j.protoAccessor(p, jsName, func(n *Node) goja.Value {
			return j.vm.ToValue(attrValue(n, attrName))
		}, func(n *Node, v goja.Value) {
			setAttr(n, attrName, v.String())
			j.markDirty()
		})
	}
	j.protoAccessor(p, "complete", func(n *Node) goja.Value { return j.vm.ToValue(true) }, nil)
	j.protoAccessor(p, "naturalWidth", func(n *Node) goja.Value { return j.vm.ToValue(0) }, nil)
	j.protoAccessor(p, "naturalHeight", func(n *Node) goja.Value { return j.vm.ToValue(0) }, nil)
	metric := func(width bool) func(n *Node) goja.Value {
		return func(n *Node) goja.Value {
			vw, vh := j.b.width*8, j.b.height*16
			if width {
				return j.vm.ToValue(j.elementWidthPx(n, vw))
			}
			return j.vm.ToValue(j.elementHeightPx(n, vh))
		}
	}
	j.protoAccessor(p, "clientWidth", metric(true), nil)
	j.protoAccessor(p, "clientHeight", metric(false), nil)
	j.protoAccessor(p, "offsetWidth", metric(true), nil)
	j.protoAccessor(p, "offsetHeight", metric(false), nil)
	j.protoAccessor(p, "scrollWidth", metric(true), nil)
	j.protoAccessor(p, "scrollHeight", metric(false), nil)
	j.protoAccessor(p, "offsetTop", func(n *Node) goja.Value { return j.vm.ToValue(0) }, nil)
	j.protoAccessor(p, "offsetLeft", func(n *Node) goja.Value { return j.vm.ToValue(0) }, nil)
	j.protoAccessor(p, "offsetParent", func(n *Node) goja.Value { return goja.Null() }, nil)
	j.protoAccessor(p, "scrollTop", func(n *Node) goja.Value { return j.vm.ToValue(0) }, func(n *Node, _ goja.Value) {})
	j.protoAccessor(p, "scrollLeft", func(n *Node) goja.Value { return j.vm.ToValue(0) }, func(n *Node, _ goja.Value) {})
	j.protoMethod(p, "getBoundingClientRect", func(n *Node, call goja.FunctionCall) goja.Value {
		vw, vh := j.b.width*8, j.b.height*16
		w := j.elementWidthPx(n, vw)
		h := j.elementHeightPx(n, vh)
		x, y := 0, 0
		if j.b.layout != nil {
			if r, ok := j.b.layout.Boxes[n]; ok {
				x, y = r.X*8, r.Y*16
			}
		}
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
	})
	j.protoAccessor(p, "textContent", func(n *Node) goja.Value {
		return j.vm.ToValue(n.TextContent())
	}, func(n *Node, v goja.Value) {
		for _, c := range n.Children {
			c.Parent = nil
		}
		n.Children = nil
		if s := v.String(); s != "" {
			n.Children = append(n.Children, &Node{Type: TextNode, Text: s, Parent: n})
		}
		j.markDirty()
	})
	j.protoAccessor(p, "innerText", func(n *Node) goja.Value {
		return j.vm.ToValue(n.TextContent())
	}, func(n *Node, v goja.Value) {
		for _, c := range n.Children {
			c.Parent = nil
		}
		n.Children = nil
		if s := v.String(); s != "" {
			n.Children = append(n.Children, &Node{Type: TextNode, Text: s, Parent: n})
		}
		j.markDirty()
	})
	j.protoAccessor(p, "innerHTML", func(n *Node) goja.Value {
		return j.vm.ToValue(serializeChildren(n))
	}, func(n *Node, v goja.Value) {
		j.setInnerHTML(n, v.String())
	})
	j.protoAccessor(p, "style", func(n *Node) goja.Value {
		return j.styleObject(n)
	}, nil)
	j.protoAccessor(p, "classList", func(n *Node) goja.Value {
		return j.classListObject(n)
	}, nil)
	j.protoAccessor(p, "attributes", func(n *Node) goja.Value {
		return j.attributesObject(n)
	}, nil)
	j.protoAccessor(p, "onclick", func(n *Node) goja.Value {
		if m := j.propListeners[n]; m != nil {
			if l, ok := m["click"]; ok {
				return l.value
			}
		}
		return goja.Null()
	}, func(n *Node, v goja.Value) {
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
	j.protoAccessor(p, "children", func(n *Node) goja.Value {
		kids := elementChildren(n)
		vals := make([]interface{}, 0, len(kids))
		for _, k := range kids {
			vals = append(vals, j.wrap(k))
		}
		return j.vm.NewArray(vals...)
	}, nil)
	j.protoAccessor(p, "childNodes", func(n *Node) goja.Value {
		vals := make([]interface{}, 0, len(n.Children))
		for _, k := range n.Children {
			vals = append(vals, j.wrap(k))
		}
		return j.vm.NewArray(vals...)
	}, nil)
	j.protoAccessor(p, "parentNode", func(n *Node) goja.Value { return j.wrap(n.Parent) }, nil)
	j.protoAccessor(p, "nextSibling", func(n *Node) goja.Value { return j.wrap(nextSiblingNode(n)) }, nil)
	j.protoAccessor(p, "previousSibling", func(n *Node) goja.Value { return j.wrap(prevSiblingNode(n)) }, nil)
	j.protoAccessor(p, "firstChild", func(n *Node) goja.Value {
		if len(n.Children) == 0 {
			return goja.Null()
		}
		return j.wrap(n.Children[0])
	}, nil)
	j.protoAccessor(p, "lastChild", func(n *Node) goja.Value {
		if len(n.Children) == 0 {
			return goja.Null()
		}
		return j.wrap(n.Children[len(n.Children)-1])
	}, nil)
	j.protoAccessor(p, "childElementCount", func(n *Node) goja.Value {
		return j.vm.ToValue(len(elementChildren(n)))
	}, nil)
	j.protoAccessor(p, "ownerDocument", func(n *Node) goja.Value { return j.docObj }, nil)
	j.protoAccessor(p, "isConnected", func(n *Node) goja.Value { return j.vm.ToValue(isConnected(n)) }, nil)
	j.protoAccessor(p, "parentElement", func(n *Node) goja.Value {
		if n.Parent != nil && n.Parent.Type == ElementNode {
			return j.wrap(n.Parent)
		}
		return goja.Null()
	}, nil)
	j.protoAccessor(p, "firstElementChild", func(n *Node) goja.Value {
		kids := elementChildren(n)
		if len(kids) == 0 {
			return goja.Null()
		}
		return j.wrap(kids[0])
	}, nil)
	j.protoAccessor(p, "lastElementChild", func(n *Node) goja.Value {
		kids := elementChildren(n)
		if len(kids) == 0 {
			return goja.Null()
		}
		return j.wrap(kids[len(kids)-1])
	}, nil)
	j.protoAccessor(p, "nextElementSibling", func(n *Node) goja.Value {
		for s := nextSiblingNode(n); s != nil; s = nextSiblingNode(s) {
			if s.Type == ElementNode {
				return j.wrap(s)
			}
		}
		return goja.Null()
	}, nil)
	j.protoAccessor(p, "previousElementSibling", func(n *Node) goja.Value {
		for s := prevSiblingNode(n); s != nil; s = prevSiblingNode(s) {
			if s.Type == ElementNode {
				return j.wrap(s)
			}
		}
		return goja.Null()
	}, nil)

	j.protoMethod(p, "getAttribute", func(n *Node, call goja.FunctionCall) goja.Value {
		return j.vm.ToValue(attrValue(n, call.Argument(0).String()))
	})
	j.protoMethod(p, "setAttribute", func(n *Node, call goja.FunctionCall) goja.Value {
		setAttr(n, call.Argument(0).String(), call.Argument(1).String())
		j.markDirty()
		return goja.Undefined()
	})
	j.protoMethod(p, "removeAttribute", func(n *Node, call goja.FunctionCall) goja.Value {
		removeAttr(n, call.Argument(0).String())
		j.markDirty()
		return goja.Undefined()
	})
	j.protoMethod(p, "hasAttribute", func(n *Node, call goja.FunctionCall) goja.Value {
		return j.vm.ToValue(n.HasAttr(call.Argument(0).String()))
	})
	j.protoMethod(p, "appendChild", func(n *Node, call goja.FunctionCall) goja.Value {
		child := j.unwrap(call.Argument(0))
		if child == nil {
			return goja.Null()
		}
		insertNode(n, child, nil)
		j.markDirty()
		return call.Argument(0)
	})
	j.protoMethod(p, "insertBefore", func(n *Node, call goja.FunctionCall) goja.Value {
		child := j.unwrap(call.Argument(0))
		if child == nil {
			return goja.Null()
		}
		insertNode(n, child, j.unwrap(call.Argument(1)))
		j.markDirty()
		return call.Argument(0)
	})
	j.protoMethod(p, "replaceChild", func(n *Node, call goja.FunctionCall) goja.Value {
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
	j.protoMethod(p, "removeChild", func(n *Node, call goja.FunctionCall) goja.Value {
		child := j.unwrap(call.Argument(0))
		if child == nil || child.Parent != n {
			return goja.Null()
		}
		detach(child)
		j.markDirty()
		return call.Argument(0)
	})
	j.protoMethod(p, "remove", func(n *Node, _ goja.FunctionCall) goja.Value {
		detach(n)
		j.markDirty()
		return goja.Undefined()
	})
	j.protoMethod(p, "contains", func(n *Node, call goja.FunctionCall) goja.Value {
		other := j.unwrap(call.Argument(0))
		for p := other; p != nil; p = p.Parent {
			if p == n {
				return j.vm.ToValue(true)
			}
		}
		return j.vm.ToValue(false)
	})
	j.protoMethod(p, "hasChildNodes", func(n *Node, _ goja.FunctionCall) goja.Value {
		return j.vm.ToValue(len(n.Children) > 0)
	})
	j.protoMethod(p, "cloneNode", func(n *Node, call goja.FunctionCall) goja.Value {
		deep := call.Argument(0).ToBoolean()
		return j.wrap(cloneNode(n, deep))
	})
	j.protoMethod(p, "matches", func(n *Node, call goja.FunctionCall) goja.Value {
		sels := j.parsedSelectors(call.Argument(0).String())
		return j.vm.ToValue(matchSelectors(n, sels))
	})
	j.protoMethod(p, "closest", func(n *Node, call goja.FunctionCall) goja.Value {
		sels := j.parsedSelectors(call.Argument(0).String())
		for p := n; p != nil; p = p.Parent {
			if p.Type == ElementNode && matchSelectors(p, sels) {
				return j.wrap(p)
			}
		}
		return goja.Null()
	})
	j.protoMethod(p, "append", func(n *Node, call goja.FunctionCall) goja.Value {
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
	j.protoMethod(p, "prepend", func(n *Node, call goja.FunctionCall) goja.Value {
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
	j.protoMethod(p, "before", func(n *Node, call goja.FunctionCall) goja.Value {
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
	j.protoMethod(p, "after", func(n *Node, call goja.FunctionCall) goja.Value {
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
	j.protoMethod(p, "replaceWith", func(n *Node, call goja.FunctionCall) goja.Value {
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
	j.protoMethod(p, "insertAdjacentHTML", func(n *Node, call goja.FunctionCall) goja.Value {
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
	j.protoMethod(p, "getRootNode", func(n *Node, _ goja.FunctionCall) goja.Value {
		root := n
		for root.Parent != nil {
			root = root.Parent
		}
		return j.wrap(root)
	})
	j.protoMethod(p, "replaceChildren", func(n *Node, call goja.FunctionCall) goja.Value {
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
	j.protoMethod(p, "addEventListener", func(n *Node, call goja.FunctionCall) goja.Value {
		j.addNodeListener(n, call.Argument(0).String(), call.Argument(1), call.Argument(2))
		return goja.Undefined()
	})
	j.protoMethod(p, "removeEventListener", func(n *Node, call goja.FunctionCall) goja.Value {
		j.removeNodeListener(n, call.Argument(0).String(), call.Argument(1))
		return goja.Undefined()
	})
	j.protoMethod(p, "dispatchEvent", func(n *Node, call goja.FunctionCall) goja.Value {
		return j.vm.ToValue(j.dispatchNodeEvent(n, call.Argument(0)))
	})
	j.protoMethod(p, "querySelector", func(n *Node, call goja.FunctionCall) goja.Value {
		return j.wrap(j.querySelector(n, call.Argument(0).String(), false))
	})
	j.protoMethod(p, "querySelectorAll", func(n *Node, call goja.FunctionCall) goja.Value {
		return j.nodeArray(j.querySelectorAll(n, call.Argument(0).String(), false))
	})
	j.protoMethod(p, "getElementsByTagName", func(n *Node, call goja.FunctionCall) goja.Value {
		tag := strings.ToLower(call.Argument(0).String())
		nodes := n.FindAll(func(x *Node) bool { return tag == "*" || x.Tag == tag })
		return j.nodeArray(nodes)
	})
	j.protoMethod(p, "getElementsByClassName", func(n *Node, call goja.FunctionCall) goja.Value {
		want := strings.Fields(call.Argument(0).String())
		if len(want) == 0 {
			return j.nodeArray(nil)
		}
		nodes := n.FindAll(func(x *Node) bool {
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
			return true
		})
		return j.nodeArray(nodes)
	})
	j.protoMethod(p, "click", func(n *Node, call goja.FunctionCall) goja.Value {
		j.fireNodeEvent(n, "click")
		j.markDirty()
		return goja.Undefined()
	})
	j.protoAccessor(p, "baseURI", func(n *Node) goja.Value {
		if j.page != nil && j.page.Base != nil {
			return j.vm.ToValue(j.page.Base.String())
		}
		return j.vm.ToValue("")
	}, nil)
	j.protoAccessor(p, "nodeValue", func(n *Node) goja.Value { return goja.Null() }, func(n *Node, _ goja.Value) {})
	j.protoAccessor(p, "clientTop", func(n *Node) goja.Value { return j.vm.ToValue(0) }, nil)
	j.protoAccessor(p, "clientLeft", func(n *Node) goja.Value { return j.vm.ToValue(0) }, nil)
	j.protoAccessor(p, "shadowRoot", func(n *Node) goja.Value { return goja.Null() }, nil)
	j.protoMethod(p, "hasAttributes", func(n *Node, _ goja.FunctionCall) goja.Value {
		return j.vm.ToValue(len(n.Attrs) > 0)
	})
	j.protoMethod(p, "getAttributeNode", func(n *Node, call goja.FunctionCall) goja.Value {
		name := call.Argument(0).String()
		for i := range n.Attrs {
			if strings.EqualFold(n.Attrs[i].Key, name) {
				return j.attributeObject(n, n.Attrs[i])
			}
		}
		return goja.Null()
	})
	j.protoMethod(p, "setAttributeNode", func(n *Node, call goja.FunctionCall) goja.Value {
		attr := jsObject(call.Argument(0))
		if attr == nil {
			return goja.Null()
		}
		name, value := attr.Get("name"), attr.Get("value")
		if name == nil || value == nil {
			return goja.Null()
		}
		setAttr(n, name.String(), value.String())
		j.markDirty()
		return goja.Null()
	})
	j.protoMethod(p, "removeAttributeNode", func(n *Node, call goja.FunctionCall) goja.Value {
		attr := jsObject(call.Argument(0))
		if attr == nil {
			return goja.Null()
		}
		name := attr.Get("name")
		if name == nil {
			return goja.Null()
		}
		for i := range n.Attrs {
			if strings.EqualFold(n.Attrs[i].Key, name.String()) {
				removeAttr(n, n.Attrs[i].Key)
				j.markDirty()
				return call.Argument(0)
			}
		}
		return goja.Null()
	})
	j.protoMethod(p, "isSameNode", func(n *Node, call goja.FunctionCall) goja.Value {
		return j.vm.ToValue(j.unwrap(call.Argument(0)) == n)
	})
	j.protoMethod(p, "isEqualNode", func(n *Node, call goja.FunctionCall) goja.Value {
		return j.vm.ToValue(nodesEqual(n, j.unwrap(call.Argument(0))))
	})
	j.protoMethod(p, "lookupNamespaceURI", func(n *Node, _ goja.FunctionCall) goja.Value { return goja.Null() })
	j.protoMethod(p, "checkVisibility", func(n *Node, _ goja.FunctionCall) goja.Value {
		visible := !n.HasAttr("hidden")
		if st := n.Style; st != nil && (st.Display == "none" || st.Hidden) {
			visible = false
		}
		return j.vm.ToValue(visible)
	})
	j.protoMethod(p, "scroll", func(n *Node, _ goja.FunctionCall) goja.Value { return goja.Undefined() })
	j.protoMethod(p, "scrollTo", func(n *Node, _ goja.FunctionCall) goja.Value { return goja.Undefined() })
	j.protoMethod(p, "scrollBy", func(n *Node, _ goja.FunctionCall) goja.Value { return goja.Undefined() })
	j.protoMethod(p, "setPointerCapture", func(n *Node, _ goja.FunctionCall) goja.Value { return goja.Undefined() })
	j.protoMethod(p, "releasePointerCapture", func(n *Node, _ goja.FunctionCall) goja.Value { return goja.Undefined() })
	j.protoMethod(p, "hasPointerCapture", func(n *Node, _ goja.FunctionCall) goja.Value { return j.vm.ToValue(false) })
	j.protoMethod(p, "requestFullscreen", func(n *Node, _ goja.FunctionCall) goja.Value {
		return j.resolvedPromise(nil)
	})
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

func (j *jsRuntime) initFormObject(o *goja.Object, n *Node) {
	j.method(o, "submit", func(goja.FunctionCall) goja.Value {
		j.b.queueFormSubmit(n)
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
	j.accessor(doc, "doctype", func() goja.Value { return goja.Null() }, nil)
	j.accessor(doc, "domain", func() goja.Value { return j.vm.ToValue("") }, nil)
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
	j.method(doc, "getElementsByName", func(call goja.FunctionCall) goja.Value {
		want := call.Argument(0).String()
		nodes := html.FindAll(func(x *Node) bool { return attrValue(x, "name") == want })
		return j.nodeArray(nodes)
	})
	collection := func(pick func(*Node) bool) func() goja.Value {
		return func() goja.Value {
			return j.nodeArray(html.FindAll(pick))
		}
	}
	j.accessor(doc, "forms", collection(func(x *Node) bool { return x.Tag == "form" }), nil)
	j.accessor(doc, "images", collection(func(x *Node) bool { return x.Tag == "img" }), nil)
	j.accessor(doc, "links", collection(func(x *Node) bool {
		return (x.Tag == "a" || x.Tag == "area") && x.HasAttr("href")
	}), nil)
	j.accessor(doc, "anchors", collection(func(x *Node) bool {
		return x.Tag == "a" && x.HasAttr("name")
	}), nil)
	j.accessor(doc, "scripts", collection(func(x *Node) bool { return x.Tag == "script" }), nil)
	j.method(doc, "querySelector", func(call goja.FunctionCall) goja.Value {
		return j.wrap(j.querySelector(html, call.Argument(0).String(), true))
	})
	j.method(doc, "querySelectorAll", func(call goja.FunctionCall) goja.Value {
		return j.nodeArray(j.querySelectorAll(html, call.Argument(0).String(), true))
	})
	j.method(doc, "getElementsByClassName", func(call goja.FunctionCall) goja.Value {
		want := strings.Fields(call.Argument(0).String())
		nodes := html.FindAll(func(x *Node) bool {
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
	return doc
}

// computedStyleObject returns a getComputedStyle-like snapshot. Values come
// from the resolved style when layout has run; detached or not-yet-styled
// elements get sensible defaults.
func (j *jsRuntime) computedStyleObject(n *Node) *goja.Object {
	o := j.vm.NewObject()
	if j.cssStyleProto != nil {
		_ = o.SetPrototype(j.cssStyleProto)
	}
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
		"top":              lengthCSS(st.Top, "auto"),
		"right":            lengthCSS(st.Right, "auto"),
		"bottom":           lengthCSS(st.Bottom, "auto"),
		"left":             lengthCSS(st.Left, "auto"),
		"z-index":          "auto",
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
	j.accessor(o, "doctype", func() goja.Value { return goja.Null() }, nil)
	j.accessor(o, "domain", func() goja.Value {
		if j.page != nil && j.page.URL != nil {
			return j.vm.ToValue(j.page.URL.Hostname())
		}
		return j.vm.ToValue("")
	}, nil)
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
	_ = o.Set("characterSet", "UTF-8")
	_ = o.Set("charset", "UTF-8")
	_ = o.Set("contentType", "text/html")
	_ = o.Set("referrer", "")
	_ = o.Set("lastModified", "")
	_ = o.Set("adoptedStyleSheets", j.vm.NewArray())
	j.accessor(o, "designMode", func() goja.Value { return j.vm.ToValue("off") }, func(goja.Value) {})
	_ = o.Set("fullscreenEnabled", true)
	j.accessor(o, "fullscreenElement", func() goja.Value { return goja.Null() }, nil)
	j.method(o, "exitFullscreen", func(goja.FunctionCall) goja.Value { return j.resolvedPromise(nil) })
	j.method(o, "execCommand", func(goja.FunctionCall) goja.Value { return j.vm.ToValue(false) })
	j.method(o, "createComment", func(call goja.FunctionCall) goja.Value {
		return j.wrap(&Node{Type: TextNode, Text: call.Argument(0).String()})
	})
	pointNode := func(px, py float64) *Node {
		if j.b == nil || j.b.layout == nil {
			return nil
		}
		cw, ch := j.b.cellW, j.b.cellH
		if cw <= 0 {
			cw = 8
		}
		if ch <= 0 {
			ch = 16
		}
		x := int(px) / cw
		y := int(py)/ch + j.b.scroll
		if x < 0 || x >= j.b.width || y < 0 {
			return nil
		}
		n := j.b.nodeAt(x, y)
		for n != nil && n.Type != ElementNode {
			n = n.Parent
		}
		return n
	}
	j.method(o, "elementFromPoint", func(call goja.FunctionCall) goja.Value {
		return j.wrap(pointNode(call.Argument(0).ToFloat(), call.Argument(1).ToFloat()))
	})
	j.method(o, "elementsFromPoint", func(call goja.FunctionCall) goja.Value {
		if n := pointNode(call.Argument(0).ToFloat(), call.Argument(1).ToFloat()); n != nil {
			return j.nodeArray([]*Node{n})
		}
		return j.nodeArray(nil)
	})
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
	j.method(o, "getElementsByName", func(call goja.FunctionCall) goja.Value {
		want := call.Argument(0).String()
		nodes := root.FindAll(func(x *Node) bool { return attrValue(x, "name") == want })
		return j.nodeArray(nodes)
	})
	collection := func(pick func(*Node) bool) func() goja.Value {
		return func() goja.Value {
			return j.nodeArray(root.FindAll(pick))
		}
	}
	j.accessor(o, "forms", collection(func(x *Node) bool { return x.Tag == "form" }), nil)
	j.accessor(o, "images", collection(func(x *Node) bool { return x.Tag == "img" }), nil)
	j.accessor(o, "links", collection(func(x *Node) bool {
		return (x.Tag == "a" || x.Tag == "area") && x.HasAttr("href")
	}), nil)
	j.accessor(o, "anchors", collection(func(x *Node) bool {
		return x.Tag == "a" && x.HasAttr("name")
	}), nil)
	j.accessor(o, "scripts", collection(func(x *Node) bool { return x.Tag == "script" }), nil)
	embeds := collection(func(x *Node) bool { return x.Tag == "embed" || x.Tag == "object" })
	j.accessor(o, "embeds", embeds, nil)
	j.accessor(o, "plugins", embeds, nil)
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
	"position":                 "position",
	"top":                      "top",
	"right":                    "right",
	"bottom":                   "bottom",
	"left":                     "left",
	"zIndex":                   "z-index",
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

// cssLengthPx resolves a CSS length string to whole CSS pixels for the DOM
// measurement APIs. Percentages use the supplied viewport basis.
func cssLengthPx(v string, font, basis float64) (int, bool) {
	if strings.TrimSpace(v) == "" {
		return 0, false
	}
	l := parseLength(v, font)
	if !l.Set || l.Auto {
		return 0, false
	}
	return lengthPx(l, font, basis)
}

func lengthPx(l Length, font, basis float64) (int, bool) {
	if !l.Set || l.Auto {
		return 0, false
	}
	switch {
	case l.Calc:
		return int(l.CalcPx + basis*l.CalcPct/100 + 0.5), true
	case l.U == UnitPercent:
		return int(basis*l.V/100 + 0.5), true
	}
	return int(l.cssPx(font) + 0.5), true
}

func (j *jsRuntime) elementWidthPx(n *Node, viewport int) int {
	if n == nil {
		return 0
	}
	if n.Tag == "html" || n.Tag == "body" {
		return viewport
	}
	font := 16.0
	if n.Style != nil && n.Style.FontSize > 0 {
		font = n.Style.FontSize
	}
	if v := styleGet(n, "width"); v != "" {
		if px, ok := cssLengthPx(v, font, float64(viewport)); ok {
			return px
		}
	}
	if n.Style != nil && n.Style.Width.Set && !n.Style.Width.IsAuto() {
		basis := 0.0
		if n.Style.Width.U == UnitPercent {
			basis = float64(viewport)
		}
		if px, ok := lengthPx(n.Style.Width, font, basis); ok {
			return px
		}
	}
	if j.b.layout != nil {
		if r, ok := j.b.layout.Boxes[n]; ok && r.W > 0 {
			return r.W * 8
		}
	}
	// Before the first layout pass (scripts run ahead of it) a block-level box
	// with auto width still fills its containing block. Carousels read
	// clientWidth to size their slides; reporting 0 made them write
	// `width:0px` into the DOM and collapse the whole slider.
	if blockLevelNode(n) {
		return j.containingWidthPx(n, viewport)
	}
	return 0
}

var blockLevelTags = map[string]bool{
	"html": true, "body": true, "div": true, "p": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"ul": true, "ol": true, "li": true, "dl": true, "dt": true, "dd": true,
	"blockquote": true, "pre": true, "table": true, "thead": true,
	"tbody": true, "tfoot": true, "tr": true, "td": true, "th": true,
	"form": true, "fieldset": true, "legend": true, "figure": true,
	"figcaption": true, "header": true, "footer": true, "nav": true,
	"main": true, "section": true, "article": true, "aside": true,
	"address": true, "hr": true, "center": true, "details": true,
	"summary": true, "hgroup": true, "search": true, "dialog": true,
}

// blockLevelNode reports whether a box fills its containing block's width.
func blockLevelNode(n *Node) bool {
	if n == nil || n.Type != ElementNode {
		return false
	}
	if n.Style != nil {
		if n.Style.Float != "" || n.Style.Position == "absolute" || n.Style.Position == "fixed" {
			return false
		}
		switch n.Style.Display {
		case "block", "list-item", "flex", "grid", "table":
			return true
		}
		return false
	}
	return blockLevelTags[strings.ToLower(n.Tag)]
}

// containingWidthPx estimates the width of a block-level box's containing
// block from ancestor widths and max-widths, defaulting to the viewport.
func (j *jsRuntime) containingWidthPx(n *Node, viewport int) int {
	w := viewport
	for p := n.Parent; p != nil && p.Type == ElementNode; p = p.Parent {
		if p.Tag == "html" || p.Tag == "body" {
			break
		}
		font := 16.0
		if p.Style != nil && p.Style.FontSize > 0 {
			font = p.Style.FontSize
		}
		if px, ok := cssLengthPx(styleGet(p, "width"), font, float64(viewport)); ok && px > 0 && px < w {
			w = px
		}
		if px, ok := cssLengthPx(styleGet(p, "max-width"), font, float64(viewport)); ok && px > 0 && px < w {
			w = px
		}
		if p.Style != nil {
			if px, ok := lengthPx(p.Style.Width, font, float64(viewport)); ok && px > 0 && px < w {
				w = px
			}
			if px, ok := lengthPx(p.Style.MaxWidth, font, float64(viewport)); ok && px > 0 && px < w {
				w = px
			}
		}
	}
	return w
}

func (j *jsRuntime) elementHeightPx(n *Node, viewport int) int {
	if n == nil {
		return 0
	}
	if n.Tag == "html" || n.Tag == "body" {
		return viewport
	}
	font := 16.0
	if n.Style != nil && n.Style.FontSize > 0 {
		font = n.Style.FontSize
	}
	if v := styleGet(n, "height"); v != "" {
		if px, ok := cssLengthPx(v, font, float64(viewport)); ok {
			return px
		}
	}
	if n.Style != nil && n.Style.Height.Set && !n.Style.Height.IsAuto() {
		basis := 0.0
		if n.Style.Height.U == UnitPercent {
			basis = float64(viewport)
		}
		if px, ok := lengthPx(n.Style.Height, font, basis); ok {
			return px
		}
	}
	if j.b.layout != nil {
		if r, ok := j.b.layout.Boxes[n]; ok && r.H > 0 {
			return r.H * 16
		}
	}
	return 0
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
	if j.cssStyleProto != nil {
		_ = o.SetPrototype(j.cssStyleProto)
	}
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
	j.method(o, "getPropertyPriority", func(goja.FunctionCall) goja.Value {
		return j.vm.ToValue("")
	})
	j.method(o, "item", func(call goja.FunctionCall) goja.Value {
		idx := int(call.Argument(0).ToInteger())
		decls := ParseDeclarations(attrValue(n, "style"))
		if idx >= 0 && idx < len(decls) {
			return j.vm.ToValue(decls[idx].Prop)
		}
		return j.vm.ToValue("")
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

func (j *jsRuntime) attributeObject(n *Node, a Attr) *goja.Object {
	o := j.vm.NewObject()
	_ = o.Set("name", a.Key)
	_ = o.Set("nodeName", a.Key)
	_ = o.Set("localName", a.Key)
	_ = o.Set("value", a.Value)
	_ = o.Set("nodeValue", a.Value)
	_ = o.Set("specified", true)
	_ = o.Set("namespaceURI", goja.Null())
	_ = o.Set("ownerElement", j.wrap(n))
	return o
}

// attributesObject returns a NamedNodeMap-like array over the element's
// attributes. It is rebuilt on each access so later attribute changes are
// reflected, matching the live DOM map closely enough for libraries.
func (j *jsRuntime) attributesObject(n *Node) *goja.Object {
	attrs := make([]*goja.Object, len(n.Attrs))
	vals := make([]interface{}, len(n.Attrs))
	for i := range n.Attrs {
		o := j.attributeObject(n, n.Attrs[i])
		attrs[i] = o
		vals[i] = o
	}
	m := j.vm.NewArray(vals...)
	j.method(m, "item", func(call goja.FunctionCall) goja.Value {
		idx := int(call.Argument(0).ToInteger())
		if idx >= 0 && idx < len(attrs) {
			return attrs[idx]
		}
		return goja.Null()
	})
	j.method(m, "getNamedItem", func(call goja.FunctionCall) goja.Value {
		name := call.Argument(0).String()
		for i := range n.Attrs {
			if strings.EqualFold(n.Attrs[i].Key, name) {
				return attrs[i]
			}
		}
		return goja.Null()
	})
	j.method(m, "removeNamedItem", func(call goja.FunctionCall) goja.Value {
		name := call.Argument(0).String()
		for i := range attrs {
			if strings.EqualFold(n.Attrs[i].Key, name) {
				removed := attrs[i]
				removeAttr(n, n.Attrs[i].Key)
				j.markDirty()
				return removed
			}
		}
		return goja.Null()
	})
	j.method(m, "setNamedItem", func(call goja.FunctionCall) goja.Value {
		attr := jsObject(call.Argument(0))
		if attr == nil {
			return goja.Null()
		}
		name := attr.Get("name")
		value := attr.Get("value")
		if name == nil || value == nil {
			return goja.Null()
		}
		setAttr(n, name.String(), value.String())
		j.markDirty()
		return goja.Null()
	})
	return m
}

func (j *jsRuntime) classListObject(n *Node) *goja.Object {
	o := j.vm.NewObject()
	if j.domTokenListProto != nil {
		_ = o.SetPrototype(j.domTokenListProto)
	}
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

func isEditableNode(n *Node) bool {
	for x := n; x != nil; x = x.Parent {
		v := strings.ToLower(attrValue(x, "contenteditable"))
		switch v {
		case "false":
			return false
		case "", "true", "plaintext-only":
			if x.HasAttr("contenteditable") {
				return true
			}
		}
	}
	return false
}

func nodeTabIndex(n *Node) int {
	if n == nil {
		return -1
	}
	if n.HasAttr("tabindex") {
		if v, err := strconv.Atoi(strings.TrimSpace(attrValue(n, "tabindex"))); err == nil {
			return v
		}
	}
	if isEditableNode(n) {
		return 0
	}
	switch strings.ToLower(n.Tag) {
	case "a", "area":
		if n.HasAttr("href") {
			return 0
		}
	case "button", "select", "textarea", "iframe", "summary", "object", "embed":
		return 0
	case "input":
		if !strings.EqualFold(attrValue(n, "type"), "hidden") {
			return 0
		}
	case "audio", "video":
		if n.HasAttr("controls") {
			return 0
		}
	}
	return -1
}

func replaceNodeWithText(n *Node, text string) {
	if n == nil {
		return
	}
	if n.Parent == nil {
		n.Children = nil
		if text != "" {
			n.Children = []*Node{{Type: TextNode, Text: text, Parent: n}}
		}
		return
	}
	parent := n.Parent
	if text == "" {
		detach(n)
		return
	}
	textNode := &Node{Type: TextNode, Text: text, Parent: parent}
	insertNode(parent, textNode, n)
	detach(n)
}

func nodesEqual(a, b *Node) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Type != b.Type || a.Tag != b.Tag || a.Text != b.Text || len(a.Attrs) != len(b.Attrs) || len(a.Children) != len(b.Children) {
		return false
	}
	for i := range a.Attrs {
		found := false
		for j := range b.Attrs {
			if a.Attrs[i] == b.Attrs[j] {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	for i := range a.Children {
		if !nodesEqual(a.Children[i], b.Children[i]) {
			return false
		}
	}
	return true
}
