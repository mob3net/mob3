package engine

import (
	"strconv"
	"strings"

	"github.com/grafana/sobek"
)

// wrap returns the JS wrapper object for a DOM node.
func (j *jsRuntime) wrap(n *Node) sobek.Value {
	if n == nil {
		return sobek.Null()
	}
	if n.Type == DocumentNode {
		if j.docObj != nil {
			return j.docObj
		}
		return sobek.Null()
	}
	if o, ok := j.nodes[n]; ok {
		return o
	}
	if !j.reserveDOMNode(1) {
		return sobek.Null()
	}
	if n.Type == ElementNode {
		if o, ok := j.constructCustomElement(n); ok {
			return o
		}
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

func (j *jsRuntime) initTextProto(p *sobek.Object) {
	// Text nodes are Nodes too. jQuery (and plugins such as custom-scrollbar)
	// call `node.ownerDocument`, `childNodes` and `contains` on every node in a
	// collection, including text nodes; a missing `ownerDocument` made
	// `wrapAll` throw and left the plugin's DOM surgery half-applied.
	j.protoAccessor(p, "ownerDocument", func(n *Node) sobek.Value { return j.docObj }, nil)
	j.protoAccessor(p, "parentElement", func(n *Node) sobek.Value {
		if n.Parent != nil && n.Parent.Type == ElementNode {
			return j.wrap(n.Parent)
		}
		return sobek.Null()
	}, nil)
	j.protoAccessor(p, "isConnected", func(n *Node) sobek.Value { return j.vm.ToValue(isConnected(n)) }, nil)
	j.protoAccessor(p, "childNodes", func(n *Node) sobek.Value { return j.vm.NewArray() }, nil)
	j.protoAccessor(p, "children", func(n *Node) sobek.Value { return j.vm.NewArray() }, nil)
	j.protoAccessor(p, "childElementCount", func(n *Node) sobek.Value { return j.vm.ToValue(0) }, nil)
	j.protoAccessor(p, "firstChild", func(n *Node) sobek.Value { return sobek.Null() }, nil)
	j.protoAccessor(p, "lastChild", func(n *Node) sobek.Value { return sobek.Null() }, nil)
	j.protoAccessor(p, "firstElementChild", func(n *Node) sobek.Value { return sobek.Null() }, nil)
	j.protoAccessor(p, "lastElementChild", func(n *Node) sobek.Value { return sobek.Null() }, nil)
	j.protoMethod(p, "hasChildNodes", func(n *Node, _ sobek.FunctionCall) sobek.Value { return j.vm.ToValue(false) })
	j.protoMethod(p, "contains", func(n *Node, _ sobek.FunctionCall) sobek.Value { return j.vm.ToValue(false) })
	j.protoMethod(p, "getRootNode", func(n *Node, _ sobek.FunctionCall) sobek.Value { return j.docObj })
	j.protoAccessor(p, "textContent", func(n *Node) sobek.Value {
		return j.vm.ToValue(n.Text)
	}, func(n *Node, v sobek.Value) {
		if s, ok := j.domText(v.String()); ok {
			n.Text = s
			j.markDirty()
		}
	})
	j.protoAccessor(p, "nodeValue", func(n *Node) sobek.Value {
		return j.vm.ToValue(n.Text)
	}, func(n *Node, v sobek.Value) {
		if s, ok := j.domText(v.String()); ok {
			n.Text = s
			j.markDirty()
		}
	})
	j.protoAccessor(p, "parentNode", func(n *Node) sobek.Value { return j.wrap(n.Parent) }, nil)
	j.protoAccessor(p, "nextSibling", func(n *Node) sobek.Value { return j.wrap(nextSiblingNode(n)) }, nil)
	j.protoAccessor(p, "previousSibling", func(n *Node) sobek.Value { return j.wrap(prevSiblingNode(n)) }, nil)
	j.protoMethod(p, "remove", func(n *Node, _ sobek.FunctionCall) sobek.Value {
		detach(n)
		j.markDirty()
		return sobek.Undefined()
	})
	j.protoMethod(p, "cloneNode", func(n *Node, call sobek.FunctionCall) sobek.Value {
		return j.wrap(cloneNode(n, call.Argument(0).ToBoolean()))
	})
}

func (j *jsRuntime) initElementProto(p *sobek.Object) {
	// template.content holds an inert fragment, so markup built there is not
	// rendered until it is cloned into the document.
	if tmpl := j.elementProtos["template"]; tmpl != nil {
		j.protoAccessor(tmpl, "content", func(n *Node) sobek.Value {
			if frag := j.templateContent(n); frag != nil {
				return j.wrap(frag)
			}
			return sobek.Null()
		}, nil)
	}
	j.protoAccessor(p, "namespaceURI", func(n *Node) sobek.Value {
		return j.vm.ToValue(elementNamespaceURI(n))
	}, nil)
	j.protoAccessor(p, "id", func(n *Node) sobek.Value { return j.vm.ToValue(AttrValue(n, "id")) }, func(n *Node, v sobek.Value) {
		j.setAttr(n, "id", v.String())
		j.markDirty()
	})
	j.protoAccessor(p, "className", func(n *Node) sobek.Value { return j.vm.ToValue(AttrValue(n, "class")) }, func(n *Node, v sobek.Value) {
		j.setAttr(n, "class", v.String())
		j.markDirty()
	})
	reflectString := func(jsName, attr string) {
		j.protoAccessor(p, jsName, func(n *Node) sobek.Value { return j.vm.ToValue(AttrValue(n, attr)) }, func(n *Node, v sobek.Value) {
			j.setAttr(n, attr, v.String())
			j.markDirty()
		})
	}
	reflectBool := func(jsName, attr string) {
		j.protoAccessor(p, jsName, func(n *Node) sobek.Value { return j.vm.ToValue(n.HasAttr(attr)) }, func(n *Node, v sobek.Value) {
			if v.ToBoolean() {
				j.setAttr(n, attr, "")
			} else {
				RemoveAttr(n, attr)
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
	j.protoAccessor(p, "draggable", func(n *Node) sobek.Value {
		return j.vm.ToValue(AttrValue(n, "draggable") == "true")
	}, func(n *Node, v sobek.Value) {
		if v.ToBoolean() {
			j.setAttr(n, "draggable", "true")
		} else {
			j.setAttr(n, "draggable", "false")
		}
		j.markDirty()
	})
	j.protoAccessor(p, "spellcheck", func(n *Node) sobek.Value {
		if AttrValue(n, "spellcheck") == "false" {
			return j.vm.ToValue(false)
		}
		return j.vm.ToValue(true)
	}, func(n *Node, v sobek.Value) {
		if v.ToBoolean() {
			j.setAttr(n, "spellcheck", "true")
		} else {
			j.setAttr(n, "spellcheck", "false")
		}
		j.markDirty()
	})
	j.protoAccessor(p, "translate", func(n *Node) sobek.Value {
		return j.vm.ToValue(AttrValue(n, "translate") != "no")
	}, func(n *Node, v sobek.Value) {
		if v.ToBoolean() {
			j.setAttr(n, "translate", "yes")
		} else {
			j.setAttr(n, "translate", "no")
		}
		j.markDirty()
	})
	j.protoAccessor(p, "contentEditable", func(n *Node) sobek.Value {
		if v := AttrValue(n, "contenteditable"); v != "" {
			return j.vm.ToValue(v)
		}
		return j.vm.ToValue("inherit")
	}, func(n *Node, v sobek.Value) {
		j.setAttr(n, "contenteditable", v.String())
		j.markDirty()
	})
	j.protoAccessor(p, "isContentEditable", func(n *Node) sobek.Value {
		return j.vm.ToValue(isEditableNode(n))
	}, nil)
	j.protoAccessor(p, "tabIndex", func(n *Node) sobek.Value {
		return j.vm.ToValue(nodeTabIndex(n))
	}, func(n *Node, v sobek.Value) {
		j.setAttr(n, "tabindex", strconv.Itoa(int(v.ToInteger())))
		j.markDirty()
	})
	j.protoAccessor(p, "outerText", func(n *Node) sobek.Value {
		return j.vm.ToValue(n.TextContent())
	}, func(n *Node, v sobek.Value) {
		s, ok := j.domText(v.String())
		if !ok || !j.reserveDOMNode(1) {
			return
		}
		replaceNodeWithText(n, s)
		j.markDirty()
	})
	j.protoAccessor(p, "href", func(n *Node) sobek.Value { return j.vm.ToValue(AttrValue(n, "href")) }, func(n *Node, v sobek.Value) {
		j.setAttr(n, "href", v.String())
		j.markDirty()
	})
	j.protoAccessor(p, "value", func(n *Node) sobek.Value {
		return j.vm.ToValue(InputValue(n))
	}, func(n *Node, v sobek.Value) {
		if s, ok := j.domText(v.String()); ok {
			n.Value = s
			n.ValueSet = true
			j.markDirty()
		}
	})
	j.protoAccessor(p, "checked", func(n *Node) sobek.Value {
		return j.vm.ToValue(n.HasAttr("checked"))
	}, func(n *Node, v sobek.Value) {
		if v.ToBoolean() {
			j.setAttr(n, "checked", "")
		} else {
			RemoveAttr(n, "checked")
		}
		j.markDirty()
	})
	j.protoAccessor(p, "form", func(n *Node) sobek.Value {
		return j.wrap(NearestForm(n))
	}, nil)
	for jsName, attrName := range map[string]string{
		"src": "src", "alt": "alt", "width": "width", "height": "height",
	} {
		jsName, attrName := jsName, attrName
		j.protoAccessor(p, jsName, func(n *Node) sobek.Value {
			return j.vm.ToValue(AttrValue(n, attrName))
		}, func(n *Node, v sobek.Value) {
			j.setAttr(n, attrName, v.String())
			j.markDirty()
		})
	}
	j.protoAccessor(p, "complete", func(n *Node) sobek.Value { return j.vm.ToValue(true) }, nil)
	j.protoAccessor(p, "naturalWidth", func(n *Node) sobek.Value { return j.vm.ToValue(0) }, nil)
	j.protoAccessor(p, "naturalHeight", func(n *Node) sobek.Value { return j.vm.ToValue(0) }, nil)
	metric := func(width bool) func(n *Node) sobek.Value {
		return func(n *Node) sobek.Value {
			if _, _, w, h, ok := j.elementRect(n); ok {
				if width {
					return j.vm.ToValue(w)
				}
				return j.vm.ToValue(h)
			}
			vw, vh := CSSWidthPx(j.host.Width()), CSSHeightPx(j.host.Height())
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
	j.protoAccessor(p, "offsetTop", func(n *Node) sobek.Value { return j.vm.ToValue(0) }, nil)
	j.protoAccessor(p, "offsetLeft", func(n *Node) sobek.Value { return j.vm.ToValue(0) }, nil)
	j.protoAccessor(p, "offsetParent", func(n *Node) sobek.Value { return sobek.Null() }, nil)
	j.protoAccessor(p, "scrollTop", func(n *Node) sobek.Value { return j.vm.ToValue(0) }, func(n *Node, _ sobek.Value) {})
	j.protoAccessor(p, "scrollLeft", func(n *Node) sobek.Value { return j.vm.ToValue(0) }, func(n *Node, _ sobek.Value) {})
	j.protoMethod(p, "getBoundingClientRect", func(n *Node, call sobek.FunctionCall) sobek.Value {
		x, y, w, h, ok := j.elementRect(n)
		if !ok {
			vw, vh := CSSWidthPx(j.host.Width()), CSSHeightPx(j.host.Height())
			x, y = 0, 0
			w = j.elementWidthPx(n, vw)
			h = j.elementHeightPx(n, vh)
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
	j.protoAccessor(p, "textContent", func(n *Node) sobek.Value {
		return j.vm.ToValue(n.TextContent())
	}, func(n *Node, v sobek.Value) {
		s, ok := j.domText(v.String())
		if !ok {
			return
		}
		for _, c := range n.Children {
			c.Parent = nil
		}
		n.Children = nil
		if s != "" {
			if !j.reserveDOMNode(1) {
				return
			}
			n.Children = append(n.Children, &Node{Type: TextNode, Text: s, Parent: n})
		}
		j.markDirty()
	})
	j.protoAccessor(p, "innerText", func(n *Node) sobek.Value {
		return j.vm.ToValue(n.TextContent())
	}, func(n *Node, v sobek.Value) {
		s, ok := j.domText(v.String())
		if !ok {
			return
		}
		for _, c := range n.Children {
			c.Parent = nil
		}
		n.Children = nil
		if s != "" {
			if !j.reserveDOMNode(1) {
				return
			}
			n.Children = append(n.Children, &Node{Type: TextNode, Text: s, Parent: n})
		}
		j.markDirty()
	})
	j.protoAccessor(p, "innerHTML", func(n *Node) sobek.Value {
		return j.vm.ToValue(serializeChildren(j.contentTarget(n)))
	}, func(n *Node, v sobek.Value) {
		j.setInnerHTML(j.contentTarget(n), v.String())
	})
	j.protoAccessor(p, "style", func(n *Node) sobek.Value {
		return j.styleObject(n)
	}, nil)
	j.protoAccessor(p, "classList", func(n *Node) sobek.Value {
		return j.classListObject(n)
	}, nil)
	j.protoAccessor(p, "attributes", func(n *Node) sobek.Value {
		return j.attributesObject(n)
	}, nil)
	j.protoAccessor(p, "onclick", func(n *Node) sobek.Value {
		if m := j.propListeners[n]; m != nil {
			if l, ok := m["click"]; ok {
				return l.value
			}
		}
		return sobek.Null()
	}, func(n *Node, v sobek.Value) {
		fn, ok := sobek.AssertFunction(v)
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
	j.protoAccessor(p, "children", func(n *Node) sobek.Value {
		kids := elementChildren(n)
		vals := make([]interface{}, 0, len(kids))
		for _, k := range kids {
			vals = append(vals, j.wrap(k))
		}
		return j.vm.NewArray(vals...)
	}, nil)
	j.protoAccessor(p, "childNodes", func(n *Node) sobek.Value {
		vals := make([]interface{}, 0, len(n.Children))
		for _, k := range n.Children {
			vals = append(vals, j.wrap(k))
		}
		return j.vm.NewArray(vals...)
	}, nil)
	j.protoAccessor(p, "parentNode", func(n *Node) sobek.Value { return j.wrap(n.Parent) }, nil)
	j.protoAccessor(p, "nextSibling", func(n *Node) sobek.Value { return j.wrap(nextSiblingNode(n)) }, nil)
	j.protoAccessor(p, "previousSibling", func(n *Node) sobek.Value { return j.wrap(prevSiblingNode(n)) }, nil)
	j.protoAccessor(p, "firstChild", func(n *Node) sobek.Value {
		if len(n.Children) == 0 {
			return sobek.Null()
		}
		return j.wrap(n.Children[0])
	}, nil)
	j.protoAccessor(p, "lastChild", func(n *Node) sobek.Value {
		if len(n.Children) == 0 {
			return sobek.Null()
		}
		return j.wrap(n.Children[len(n.Children)-1])
	}, nil)
	j.protoAccessor(p, "childElementCount", func(n *Node) sobek.Value {
		return j.vm.ToValue(len(elementChildren(n)))
	}, nil)
	j.protoAccessor(p, "ownerDocument", func(n *Node) sobek.Value { return j.docObj }, nil)
	j.protoAccessor(p, "isConnected", func(n *Node) sobek.Value { return j.vm.ToValue(isConnected(n)) }, nil)
	j.protoAccessor(p, "parentElement", func(n *Node) sobek.Value {
		if n.Parent != nil && n.Parent.Type == ElementNode {
			return j.wrap(n.Parent)
		}
		return sobek.Null()
	}, nil)
	j.protoAccessor(p, "firstElementChild", func(n *Node) sobek.Value {
		kids := elementChildren(n)
		if len(kids) == 0 {
			return sobek.Null()
		}
		return j.wrap(kids[0])
	}, nil)
	j.protoAccessor(p, "lastElementChild", func(n *Node) sobek.Value {
		kids := elementChildren(n)
		if len(kids) == 0 {
			return sobek.Null()
		}
		return j.wrap(kids[len(kids)-1])
	}, nil)
	j.protoAccessor(p, "nextElementSibling", func(n *Node) sobek.Value {
		for s := nextSiblingNode(n); s != nil; s = nextSiblingNode(s) {
			if s.Type == ElementNode {
				return j.wrap(s)
			}
		}
		return sobek.Null()
	}, nil)
	j.protoAccessor(p, "previousElementSibling", func(n *Node) sobek.Value {
		for s := prevSiblingNode(n); s != nil; s = prevSiblingNode(s) {
			if s.Type == ElementNode {
				return j.wrap(s)
			}
		}
		return sobek.Null()
	}, nil)

	j.protoMethod(p, "getAttribute", func(n *Node, call sobek.FunctionCall) sobek.Value {
		return j.vm.ToValue(AttrValue(n, call.Argument(0).String()))
	})
	j.protoMethod(p, "setAttribute", func(n *Node, call sobek.FunctionCall) sobek.Value {
		j.setAttr(n, call.Argument(0).String(), call.Argument(1).String())
		j.markDirty()
		return sobek.Undefined()
	})
	j.protoMethod(p, "removeAttribute", func(n *Node, call sobek.FunctionCall) sobek.Value {
		RemoveAttr(n, call.Argument(0).String())
		j.markDirty()
		return sobek.Undefined()
	})
	j.protoMethod(p, "hasAttribute", func(n *Node, call sobek.FunctionCall) sobek.Value {
		return j.vm.ToValue(n.HasAttr(call.Argument(0).String()))
	})
	j.protoMethod(p, "appendChild", func(n *Node, call sobek.FunctionCall) sobek.Value {
		child := j.unwrap(call.Argument(0))
		if child == nil {
			return sobek.Null()
		}
		insertNode(n, child, nil)
		j.markDirty()
		return call.Argument(0)
	})
	j.protoMethod(p, "insertBefore", func(n *Node, call sobek.FunctionCall) sobek.Value {
		child := j.unwrap(call.Argument(0))
		if child == nil {
			return sobek.Null()
		}
		insertNode(n, child, j.unwrap(call.Argument(1)))
		j.markDirty()
		return call.Argument(0)
	})
	j.protoMethod(p, "replaceChild", func(n *Node, call sobek.FunctionCall) sobek.Value {
		child := j.unwrap(call.Argument(0))
		old := j.unwrap(call.Argument(1))
		if child == nil || old == nil || old.Parent != n {
			return sobek.Null()
		}
		insertNode(n, child, old)
		detach(old)
		j.markDirty()
		return call.Argument(1)
	})
	j.protoMethod(p, "removeChild", func(n *Node, call sobek.FunctionCall) sobek.Value {
		child := j.unwrap(call.Argument(0))
		if child == nil || child.Parent != n {
			return sobek.Null()
		}
		detach(child)
		j.markDirty()
		return call.Argument(0)
	})
	j.protoMethod(p, "remove", func(n *Node, _ sobek.FunctionCall) sobek.Value {
		detach(n)
		j.markDirty()
		return sobek.Undefined()
	})
	j.protoMethod(p, "contains", func(n *Node, call sobek.FunctionCall) sobek.Value {
		other := j.unwrap(call.Argument(0))
		for p := other; p != nil; p = p.Parent {
			if p == n {
				return j.vm.ToValue(true)
			}
		}
		return j.vm.ToValue(false)
	})
	j.protoMethod(p, "hasChildNodes", func(n *Node, _ sobek.FunctionCall) sobek.Value {
		return j.vm.ToValue(len(n.Children) > 0)
	})
	j.protoMethod(p, "cloneNode", func(n *Node, call sobek.FunctionCall) sobek.Value {
		return j.wrap(j.cloneNodeValue(n, call.Argument(0).ToBoolean()))
	})
	j.protoMethod(p, "matches", func(n *Node, call sobek.FunctionCall) sobek.Value {
		sels := j.ParsedSelectors(call.Argument(0).String())
		return j.vm.ToValue(matchSelectors(n, sels))
	})
	j.protoMethod(p, "closest", func(n *Node, call sobek.FunctionCall) sobek.Value {
		sels := j.ParsedSelectors(call.Argument(0).String())
		for p := n; p != nil; p = p.Parent {
			if p.Type == ElementNode && matchSelectors(p, sels) {
				return j.wrap(p)
			}
		}
		return sobek.Null()
	})
	j.protoMethod(p, "append", func(n *Node, call sobek.FunctionCall) sobek.Value {
		for _, arg := range call.Arguments {
			child := j.unwrap(arg)
			if child == nil {
				if s := arg.String(); s != "" {
					child = j.textNode(s)
				}
			}
			if child != nil {
				insertNode(n, child, nil)
			}
		}
		j.markDirty()
		return sobek.Undefined()
	})
	j.protoMethod(p, "prepend", func(n *Node, call sobek.FunctionCall) sobek.Value {
		var ref *Node
		if len(n.Children) > 0 {
			ref = n.Children[0]
		}
		for _, arg := range call.Arguments {
			child := j.unwrap(arg)
			if child == nil {
				if s := arg.String(); s != "" {
					child = j.textNode(s)
				}
			}
			if child != nil {
				insertNode(n, child, ref)
			}
		}
		j.markDirty()
		return sobek.Undefined()
	})
	j.protoMethod(p, "before", func(n *Node, call sobek.FunctionCall) sobek.Value {
		if n.Parent == nil {
			return sobek.Undefined()
		}
		for _, arg := range call.Arguments {
			if child := j.unwrap(arg); child != nil {
				insertNode(n.Parent, child, n)
			}
		}
		j.markDirty()
		return sobek.Undefined()
	})
	j.protoMethod(p, "after", func(n *Node, call sobek.FunctionCall) sobek.Value {
		if n.Parent == nil {
			return sobek.Undefined()
		}
		ref := nextSiblingNode(n)
		for _, arg := range call.Arguments {
			if child := j.unwrap(arg); child != nil {
				insertNode(n.Parent, child, ref)
			}
		}
		j.markDirty()
		return sobek.Undefined()
	})
	j.protoMethod(p, "replaceWith", func(n *Node, call sobek.FunctionCall) sobek.Value {
		if n.Parent == nil {
			return sobek.Undefined()
		}
		parent := n.Parent
		for _, arg := range call.Arguments {
			if child := j.unwrap(arg); child != nil {
				insertNode(parent, child, n)
			}
		}
		detach(n)
		j.markDirty()
		return sobek.Undefined()
	})
	j.protoMethod(p, "insertAdjacentHTML", func(n *Node, call sobek.FunctionCall) sobek.Value {
		pos := strings.ToLower(call.Argument(0).String())
		html, ok := j.domText(call.Argument(1).String())
		if !ok {
			return sobek.Undefined()
		}
		frag := j.parseFragment(html)
		count := 0
		for _, c := range frag {
			count += countNodes(c)
		}
		if !j.reserveDOMNode(count) {
			return sobek.Undefined()
		}
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
		return sobek.Undefined()
	})
	j.protoMethod(p, "getRootNode", func(n *Node, _ sobek.FunctionCall) sobek.Value {
		root := n
		for root.Parent != nil {
			root = root.Parent
		}
		return j.wrap(root)
	})
	j.protoMethod(p, "replaceChildren", func(n *Node, call sobek.FunctionCall) sobek.Value {
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
				if child := j.textNode(s); child != nil {
					child.Parent = n
					n.Children = append(n.Children, child)
				}
			}
		}
		j.markDirty()
		return sobek.Undefined()
	})
	j.protoMethod(p, "addEventListener", func(n *Node, call sobek.FunctionCall) sobek.Value {
		j.addNodeListener(n, call.Argument(0).String(), call.Argument(1), call.Argument(2))
		return sobek.Undefined()
	})
	j.protoMethod(p, "removeEventListener", func(n *Node, call sobek.FunctionCall) sobek.Value {
		j.removeNodeListener(n, call.Argument(0).String(), call.Argument(1))
		return sobek.Undefined()
	})
	j.protoMethod(p, "dispatchEvent", func(n *Node, call sobek.FunctionCall) sobek.Value {
		return j.vm.ToValue(j.dispatchNodeEvent(n, call.Argument(0)))
	})
	j.protoMethod(p, "querySelector", func(n *Node, call sobek.FunctionCall) sobek.Value {
		return j.wrap(j.querySelector(n, call.Argument(0).String(), false))
	})
	j.protoMethod(p, "querySelectorAll", func(n *Node, call sobek.FunctionCall) sobek.Value {
		return j.nodeArray(j.querySelectorAll(n, call.Argument(0).String(), false))
	})
	j.protoMethod(p, "getElementsByTagName", func(n *Node, call sobek.FunctionCall) sobek.Value {
		tag := strings.ToLower(call.Argument(0).String())
		nodes := n.FindAll(func(x *Node) bool { return tag == "*" || x.Tag == tag })
		return j.nodeArray(nodes)
	})
	j.protoMethod(p, "getElementsByClassName", func(n *Node, call sobek.FunctionCall) sobek.Value {
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
	j.protoMethod(p, "click", func(n *Node, call sobek.FunctionCall) sobek.Value {
		// A page usually opens the file picker by calling click() on a hidden
		// file input (an Upload button wrapping it). There is no picker here,
		// so show the same unsupported-upload notice a direct click does.
		if IsFileInput(n) && j.host != nil {
			j.host.ShowUploadNotice()
		}
		j.FireNodeEvent(n, "click")
		j.markDirty()
		return sobek.Undefined()
	})
	j.protoAccessor(p, "baseURI", func(n *Node) sobek.Value {
		if j.page != nil && j.page.Base != nil {
			return j.vm.ToValue(j.page.Base.String())
		}
		return j.vm.ToValue("")
	}, nil)
	j.protoAccessor(p, "nodeValue", func(n *Node) sobek.Value { return sobek.Null() }, func(n *Node, _ sobek.Value) {})
	j.protoAccessor(p, "clientTop", func(n *Node) sobek.Value { return j.vm.ToValue(0) }, nil)
	j.protoAccessor(p, "clientLeft", func(n *Node) sobek.Value { return j.vm.ToValue(0) }, nil)
	j.protoAccessor(p, "shadowRoot", func(n *Node) sobek.Value { return sobek.Null() }, nil)
	j.protoMethod(p, "hasAttributes", func(n *Node, _ sobek.FunctionCall) sobek.Value {
		return j.vm.ToValue(len(n.Attrs) > 0)
	})
	j.protoMethod(p, "getAttributeNode", func(n *Node, call sobek.FunctionCall) sobek.Value {
		name := call.Argument(0).String()
		for i := range n.Attrs {
			if strings.EqualFold(n.Attrs[i].Key, name) {
				return j.attributeObject(n, n.Attrs[i])
			}
		}
		return sobek.Null()
	})
	j.protoMethod(p, "setAttributeNode", func(n *Node, call sobek.FunctionCall) sobek.Value {
		attr := jsObject(call.Argument(0))
		if attr == nil {
			return sobek.Null()
		}
		name, value := attr.Get("name"), attr.Get("value")
		if name == nil || value == nil {
			return sobek.Null()
		}
		j.setAttr(n, name.String(), value.String())
		j.markDirty()
		return sobek.Null()
	})
	j.protoMethod(p, "removeAttributeNode", func(n *Node, call sobek.FunctionCall) sobek.Value {
		attr := jsObject(call.Argument(0))
		if attr == nil {
			return sobek.Null()
		}
		name := attr.Get("name")
		if name == nil {
			return sobek.Null()
		}
		for i := range n.Attrs {
			if strings.EqualFold(n.Attrs[i].Key, name.String()) {
				RemoveAttr(n, n.Attrs[i].Key)
				j.markDirty()
				return call.Argument(0)
			}
		}
		return sobek.Null()
	})
	j.protoMethod(p, "isSameNode", func(n *Node, call sobek.FunctionCall) sobek.Value {
		return j.vm.ToValue(j.unwrap(call.Argument(0)) == n)
	})
	j.protoMethod(p, "isEqualNode", func(n *Node, call sobek.FunctionCall) sobek.Value {
		return j.vm.ToValue(nodesEqual(n, j.unwrap(call.Argument(0))))
	})
	j.protoMethod(p, "lookupNamespaceURI", func(n *Node, _ sobek.FunctionCall) sobek.Value { return sobek.Null() })
	j.protoMethod(p, "checkVisibility", func(n *Node, _ sobek.FunctionCall) sobek.Value {
		visible := !n.HasAttr("hidden")
		if st := n.Style; st != nil && (st.Display == "none" || st.Hidden) {
			visible = false
		}
		return j.vm.ToValue(visible)
	})
	j.protoMethod(p, "scroll", func(n *Node, _ sobek.FunctionCall) sobek.Value { return sobek.Undefined() })
	j.protoMethod(p, "scrollTo", func(n *Node, _ sobek.FunctionCall) sobek.Value { return sobek.Undefined() })
	j.protoMethod(p, "scrollBy", func(n *Node, _ sobek.FunctionCall) sobek.Value { return sobek.Undefined() })
	j.protoMethod(p, "setPointerCapture", func(n *Node, _ sobek.FunctionCall) sobek.Value { return sobek.Undefined() })
	j.protoMethod(p, "releasePointerCapture", func(n *Node, _ sobek.FunctionCall) sobek.Value { return sobek.Undefined() })
	j.protoMethod(p, "hasPointerCapture", func(n *Node, _ sobek.FunctionCall) sobek.Value { return j.vm.ToValue(false) })
	j.protoMethod(p, "requestFullscreen", func(n *Node, _ sobek.FunctionCall) sobek.Value {
		return j.resolvedPromise(nil)
	})
}
