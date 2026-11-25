package engine

import (
	"strings"

	"github.com/grafana/sobek"
)

// initDialogObject adds the HTMLDialogElement subset pages use to open and
// dismiss dialogs. MOB³ does not paint a modal backdrop, but tracking the open
// state keeps the UA dialog rules and close events working.
func (j *jsRuntime) initDialogObject(o *sobek.Object, n *Node) {
	j.accessor(o, "open", func() sobek.Value {
		return j.vm.ToValue(n.HasAttr("open"))
	}, func(v sobek.Value) {
		if v.ToBoolean() {
			SetAttr(n, "open", "")
		} else {
			RemoveAttr(n, "open")
		}
		j.markDirty()
	})
	j.accessor(o, "returnValue", func() sobek.Value {
		return j.vm.ToValue(n.DialogReturnValue)
	}, func(v sobek.Value) {
		n.DialogReturnValue = v.String()
	})
	open := func(sobek.FunctionCall) sobek.Value {
		SetAttr(n, "open", "")
		j.markDirty()
		return sobek.Undefined()
	}
	j.method(o, "show", open)
	j.method(o, "showModal", open)
	j.method(o, "close", func(call sobek.FunctionCall) sobek.Value {
		if !n.HasAttr("open") {
			return sobek.Undefined()
		}
		if v := call.Argument(0); !sobek.IsUndefined(v) && !sobek.IsNull(v) {
			n.DialogReturnValue = v.String()
		}
		RemoveAttr(n, "open")
		j.markDirty()
		j.FireNodeEvent(n, "close")
		return sobek.Undefined()
	})
}

func (j *jsRuntime) initFormObject(o *sobek.Object, n *Node) {
	j.method(o, "submit", func(sobek.FunctionCall) sobek.Value {
		j.host.QueueFormSubmit(n)
		return sobek.Undefined()
	})
}

// implementationObject backs document.implementation. hasFeature reports true
// for legacy feature checks (Raphael uses it for SVG detection), and
// createHTMLDocument returns a detached document for jQuery-style parsing.
func (j *jsRuntime) implementationObject() *sobek.Object {
	imp := j.vm.NewObject()
	j.method(imp, "hasFeature", func(sobek.FunctionCall) sobek.Value { return j.vm.ToValue(true) })
	j.method(imp, "createHTMLDocument", func(call sobek.FunctionCall) sobek.Value {
		return j.newDetachedDocument(call.Argument(0).String())
	})
	j.method(imp, "createDocument", func(sobek.FunctionCall) sobek.Value {
		return j.newDetachedDocument("")
	})
	return imp
}

func (j *jsRuntime) newDetachedDocument(title string) *sobek.Object {
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
	j.accessor(doc, "documentElement", func() sobek.Value { return j.wrap(html) }, nil)
	j.accessor(doc, "doctype", func() sobek.Value { return sobek.Null() }, nil)
	j.accessor(doc, "domain", func() sobek.Value { return j.vm.ToValue("") }, nil)
	j.accessor(doc, "head", func() sobek.Value { return j.wrap(head) }, nil)
	j.accessor(doc, "body", func() sobek.Value { return j.wrap(body) }, nil)
	j.accessor(doc, "defaultView", func() sobek.Value { return j.winObj }, nil)
	j.accessor(doc, "title", func() sobek.Value { return j.vm.ToValue(title) }, nil)
	_ = doc.Set("implementation", j.implementationObject())
	j.method(doc, "createElement", func(call sobek.FunctionCall) sobek.Value {
		tag := strings.ToLower(strings.TrimSpace(call.Argument(0).String()))
		if tag == "" {
			tag = "div"
		}
		return j.wrap(&Node{Type: ElementNode, Tag: tag})
	})
	j.method(doc, "createElementNS", func(call sobek.FunctionCall) sobek.Value {
		return j.wrap(&Node{
			Type:      ElementNode,
			Tag:       strings.ToLower(call.Argument(1).String()),
			Namespace: call.Argument(0).String(),
		})
	})
	j.method(doc, "createTextNode", func(call sobek.FunctionCall) sobek.Value {
		return j.wrap(&Node{Type: TextNode, Text: call.Argument(0).String()})
	})
	j.method(doc, "createComment", func(call sobek.FunctionCall) sobek.Value {
		return j.wrap(&Node{Type: TextNode, Text: call.Argument(0).String()})
	})
	j.method(doc, "createDocumentFragment", func(sobek.FunctionCall) sobek.Value {
		return j.wrap(&Node{Type: ElementNode, Tag: "#fragment"})
	})
	j.method(doc, "getElementById", func(call sobek.FunctionCall) sobek.Value {
		id := call.Argument(0).String()
		var found *Node
		html.FindAll(func(x *Node) bool {
			if found == nil && AttrValue(x, "id") == id {
				found = x
				return true
			}
			return false
		})
		return j.wrap(found)
	})
	j.method(doc, "getElementsByTagName", func(call sobek.FunctionCall) sobek.Value {
		tag := strings.ToLower(call.Argument(0).String())
		nodes := html.FindAll(func(x *Node) bool { return tag == "*" || x.Tag == tag })
		return j.nodeArray(nodes)
	})
	j.method(doc, "getElementsByName", func(call sobek.FunctionCall) sobek.Value {
		want := call.Argument(0).String()
		nodes := html.FindAll(func(x *Node) bool { return AttrValue(x, "name") == want })
		return j.nodeArray(nodes)
	})
	collection := func(pick func(*Node) bool) func() sobek.Value {
		return func() sobek.Value {
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
	j.method(doc, "querySelector", func(call sobek.FunctionCall) sobek.Value {
		return j.wrap(j.querySelector(html, call.Argument(0).String(), true))
	})
	j.method(doc, "querySelectorAll", func(call sobek.FunctionCall) sobek.Value {
		return j.nodeArray(j.querySelectorAll(html, call.Argument(0).String(), true))
	})
	j.method(doc, "getElementsByClassName", func(call sobek.FunctionCall) sobek.Value {
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
