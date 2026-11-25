package engine

import (
	"strings"

	"github.com/grafana/sobek"
)

func (j *jsRuntime) accessor(o *sobek.Object, name string, get func() sobek.Value, set func(sobek.Value)) {
	var g, s sobek.Value
	if get != nil {
		g = j.vm.ToValue(func(sobek.FunctionCall) sobek.Value { return get() })
	}
	if set != nil {
		s = j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
			set(call.Argument(0))
			return sobek.Undefined()
		})
	}
	_ = o.DefineAccessorProperty(name, g, s, sobek.FLAG_TRUE, sobek.FLAG_FALSE)
}

func (j *jsRuntime) method(o *sobek.Object, name string, fn func(sobek.FunctionCall) sobek.Value) {
	_ = o.Set(name, j.vm.ToValue(fn))
}

// protoAccessor defines one shared accessor on a prototype. The node is
// resolved from the receiver, so a single getter/setter pair serves every
// element instead of allocating a closure per wrapper (the per-element cost
// dominated JS-heavy pages' memory).
func (j *jsRuntime) protoAccessor(p *sobek.Object, name string, get func(n *Node) sobek.Value, set func(n *Node, v sobek.Value)) {
	if p == nil {
		return
	}
	var g, s sobek.Value
	if get != nil {
		g = j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
			n := j.thisNode(call)
			if n == nil {
				return sobek.Undefined()
			}
			return get(n)
		})
	}
	if set != nil {
		s = j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
			n := j.thisNode(call)
			if n == nil {
				return sobek.Undefined()
			}
			set(n, call.Argument(0))
			return sobek.Undefined()
		})
	}
	_ = p.DefineAccessorProperty(name, g, s, sobek.FLAG_TRUE, sobek.FLAG_FALSE)
}

// protoMethod defines one shared method on a prototype.
func (j *jsRuntime) protoMethod(p *sobek.Object, name string, fn func(n *Node, call sobek.FunctionCall) sobek.Value) {
	if p == nil {
		return
	}
	_ = p.Set(name, j.vm.ToValue(func(call sobek.FunctionCall) sobek.Value {
		n := j.thisNode(call)
		if n == nil {
			return sobek.Undefined()
		}
		return fn(n, call)
	}))
}

// thisNode resolves the wrapper that received a prototype accessor or method.
// A custom element constructor may call DOM methods before its object has been
// bound to a node, so construction registers the node on a stack the lookup
// falls back to.
func (j *jsRuntime) thisNode(call sobek.FunctionCall) *Node {
	if o, ok := call.This.(*sobek.Object); ok {
		if n, ok := j.rev[o]; ok {
			return n
		}
	}
	if n := len(j.building); n > 0 {
		return j.building[n-1]
	}
	return nil
}

// initTextInstance sets the per-object fields of a text wrapper. Everything
// else is shared through the prototype.
func (j *jsRuntime) initTextInstance(o *sobek.Object) {
	_ = o.Set("nodeType", 3)
	_ = o.Set("nodeName", "#text")
	_ = o.Set("tagName", "")
}

// initElementInstance sets the per-object fields of an element wrapper and
// installs the tag-specific extras (canvas context, dialog/form methods).
func (j *jsRuntime) initElementInstance(o *sobek.Object, n *Node) {
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

func (j *jsRuntime) unwrap(v sobek.Value) *Node {
	if o, ok := v.(*sobek.Object); ok {
		return j.rev[o]
	}
	return nil
}

func AttrValue(n *Node, name string) string {
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

func SetAttr(n *Node, name, value string) {
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

func RemoveAttr(n *Node, name string) {
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

// constructCustomElement creates the JS object for an element whose tag has a
// registered custom element class. Reflect.construct runs the class, field
// initializers included, and the new object is bound to the node.
func (j *jsRuntime) constructCustomElement(n *Node) (sobek.Value, bool) {
	if n == nil || n.Type != ElementNode {
		return nil, false
	}
	ctor := j.customCtors[strings.ToLower(n.Tag)]
	if ctor == nil {
		return nil, false
	}
	reflectObj, ok := j.vm.Get("Reflect").(*sobek.Object)
	if !ok {
		return nil, false
	}
	construct, ok := sobek.AssertFunction(reflectObj.Get("construct"))
	if !ok {
		return nil, false
	}
	j.building = append(j.building, n)
	v, err := construct(reflectObj, ctor, j.vm.NewArray())
	j.building = j.building[:len(j.building)-1]
	if err != nil {
		j.ReportError(err)
		return nil, false
	}
	o, ok := v.(*sobek.Object)
	if !ok {
		return nil, false
	}
	j.nodes[n] = o
	j.rev[o] = n
	return o, true
}
