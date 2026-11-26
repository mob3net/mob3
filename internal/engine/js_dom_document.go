package engine

import (
	"strings"

	"github.com/grafana/sobek"
)

func (j *jsRuntime) documentObject() *sobek.Object {
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
	// The document is also a Node. Full-document hydration (frameworks that
	// pass `document` as the root container) and focus helpers call these on
	// the document itself.
	j.method(o, "appendChild", func(call sobek.FunctionCall) sobek.Value {
		child := j.unwrap(call.Argument(0))
		if child == nil {
			return sobek.Null()
		}
		insertNode(root, child, nil)
		j.markDirty()
		return call.Argument(0)
	})
	j.method(o, "insertBefore", func(call sobek.FunctionCall) sobek.Value {
		child := j.unwrap(call.Argument(0))
		if child == nil {
			return sobek.Null()
		}
		insertNode(root, child, j.unwrap(call.Argument(1)))
		j.markDirty()
		return call.Argument(0)
	})
	j.method(o, "replaceChild", func(call sobek.FunctionCall) sobek.Value {
		child := j.unwrap(call.Argument(0))
		old := j.unwrap(call.Argument(1))
		if child == nil || old == nil || old.Parent != root {
			return sobek.Null()
		}
		insertNode(root, child, old)
		detach(old)
		j.markDirty()
		return call.Argument(1)
	})
	j.method(o, "removeChild", func(call sobek.FunctionCall) sobek.Value {
		child := j.unwrap(call.Argument(0))
		if child == nil || child.Parent != root {
			return sobek.Null()
		}
		detach(child)
		j.markDirty()
		return call.Argument(0)
	})
	j.method(o, "contains", func(call sobek.FunctionCall) sobek.Value {
		other := j.unwrap(call.Argument(0))
		for p := other; p != nil; p = p.Parent {
			if p == root {
				return j.vm.ToValue(true)
			}
		}
		return j.vm.ToValue(false)
	})
	j.method(o, "hasChildNodes", func(sobek.FunctionCall) sobek.Value {
		return j.vm.ToValue(len(root.Children) > 0)
	})
	j.method(o, "getRootNode", func(sobek.FunctionCall) sobek.Value {
		return o
	})
	j.accessor(o, "firstChild", func() sobek.Value {
		if len(root.Children) == 0 {
			return sobek.Null()
		}
		return j.wrap(root.Children[0])
	}, nil)
	j.accessor(o, "lastChild", func() sobek.Value {
		if len(root.Children) == 0 {
			return sobek.Null()
		}
		return j.wrap(root.Children[len(root.Children)-1])
	}, nil)
	j.accessor(o, "firstElementChild", func() sobek.Value {
		for _, c := range root.Children {
			if c.Type == ElementNode {
				return j.wrap(c)
			}
		}
		return sobek.Null()
	}, nil)
	j.accessor(o, "lastElementChild", func() sobek.Value {
		for i := len(root.Children) - 1; i >= 0; i-- {
			if root.Children[i].Type == ElementNode {
				return j.wrap(root.Children[i])
			}
		}
		return sobek.Null()
	}, nil)
	j.accessor(o, "children", func() sobek.Value {
		return j.nodeArray(elementChildren(root))
	}, nil)
	j.accessor(o, "childElementCount", func() sobek.Value {
		return j.vm.ToValue(len(elementChildren(root)))
	}, nil)
	j.accessor(o, "documentElement", func() sobek.Value { return j.wrap(html) }, nil)
	j.accessor(o, "doctype", func() sobek.Value { return sobek.Null() }, nil)
	j.accessor(o, "domain", func() sobek.Value {
		if j.page != nil && j.page.URL != nil {
			return j.vm.ToValue(j.page.URL.Hostname())
		}
		return j.vm.ToValue("")
	}, nil)
	j.accessor(o, "body", func() sobek.Value { return j.wrap(body) }, nil)
	j.accessor(o, "head", func() sobek.Value { return j.wrap(head) }, nil)
	j.accessor(o, "defaultView", func() sobek.Value { return j.winObj }, nil)
	_ = o.Set("implementation", j.implementationObject())
	j.accessor(o, "title", func() sobek.Value {
		if title == nil {
			return j.vm.ToValue("")
		}
		return j.vm.ToValue(strings.TrimSpace(title.TextContent()))
	}, func(v sobek.Value) {
		s, ok := j.domText(v.String())
		if !ok {
			return
		}
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
	j.accessor(o, "readyState", func() sobek.Value { return j.vm.ToValue("complete") }, nil)
	j.accessor(o, "compatMode", func() sobek.Value { return j.vm.ToValue("CSS1Compat") }, nil)
	j.accessor(o, "scrollingElement", func() sobek.Value { return j.wrap(body) }, nil)
	j.accessor(o, "currentScript", func() sobek.Value {
		if j.currentScript == nil {
			return sobek.Null()
		}
		return j.currentScript
	}, nil)
	// document.baseURI follows the <base> element the page declares, like the
	// element baseURI does. Falling back to the document URL would keep the
	// fragment in it, so a script that resolves API URLs against baseURI would
	// build ".../#/api" and request the page itself instead of the endpoint.
	if j.page != nil {
		base := ""
		if j.page.Base != nil {
			base = j.page.Base.String()
		} else if j.page.URL != nil {
			base = j.page.URL.String()
		}
		j.accessor(o, "baseURI", func() sobek.Value { return j.vm.ToValue(base) }, nil)
	}
	_ = o.Set("styleSheets", j.vm.NewArray())
	_ = o.Set("adoptedStyleSheets", j.vm.NewArray())
	fonts := j.vm.NewObject()
	fontsNoop := j.vm.ToValue(func(sobek.FunctionCall) sobek.Value { return sobek.Undefined() })
	_ = fonts.Set("addEventListener", fontsNoop)
	_ = fonts.Set("removeEventListener", fontsNoop)
	_ = fonts.Set("check", j.vm.ToValue(func(sobek.FunctionCall) sobek.Value { return j.vm.ToValue(true) }))
	if ready, err := j.vm.RunString("Promise.resolve()"); err == nil {
		_ = fonts.Set("ready", ready)
	}
	_ = o.Set("fonts", fonts)
	j.accessor(o, "cookie", func() sobek.Value {
		if j.page == nil || j.page.URL == nil || j.host == nil {
			return j.vm.ToValue("")
		}
		cookies := j.host.DocumentCookies(j.page.URL)
		parts := make([]string, 0, len(cookies))
		for _, c := range cookies {
			parts = append(parts, c.Name+"="+c.Value)
		}
		return j.vm.ToValue(strings.Join(parts, "; "))
	}, func(v sobek.Value) {
		raw := strings.TrimSpace(v.String())
		if raw == "" || j.page == nil || j.page.URL == nil || j.host == nil {
			return
		}
		j.host.SetDocumentCookie(j.page.URL, raw)
	})
	j.method(o, "write", func(call sobek.FunctionCall) sobek.Value {
		var b strings.Builder
		for _, a := range call.Arguments {
			b.WriteString(a.String())
		}
		j.documentWrite(b.String(), false)
		return sobek.Undefined()
	})
	j.method(o, "writeln", func(call sobek.FunctionCall) sobek.Value {
		var b strings.Builder
		for _, a := range call.Arguments {
			b.WriteString(a.String())
		}
		j.documentWrite(b.String(), true)
		return sobek.Undefined()
	})
	j.method(o, "open", func(sobek.FunctionCall) sobek.Value { return sobek.Undefined() })
	j.method(o, "close", func(sobek.FunctionCall) sobek.Value { return sobek.Undefined() })
	j.accessor(o, "visibilityState", func() sobek.Value { return j.vm.ToValue("visible") }, nil)
	j.accessor(o, "hidden", func() sobek.Value { return j.vm.ToValue(false) }, nil)
	_ = o.Set("characterSet", "UTF-8")
	_ = o.Set("charset", "UTF-8")
	_ = o.Set("contentType", "text/html")
	_ = o.Set("referrer", "")
	_ = o.Set("lastModified", "")
	_ = o.Set("adoptedStyleSheets", j.vm.NewArray())
	j.accessor(o, "designMode", func() sobek.Value { return j.vm.ToValue("off") }, func(sobek.Value) {})
	_ = o.Set("fullscreenEnabled", true)
	j.accessor(o, "fullscreenElement", func() sobek.Value { return sobek.Null() }, nil)
	j.method(o, "exitFullscreen", func(sobek.FunctionCall) sobek.Value { return j.resolvedPromise(nil) })
	j.method(o, "execCommand", func(sobek.FunctionCall) sobek.Value { return j.vm.ToValue(false) })
	j.method(o, "createComment", func(call sobek.FunctionCall) sobek.Value {
		return j.wrap(j.textNode(call.Argument(0).String()))
	})
	pointNode := func(px, py float64) *Node {
		if j.host == nil || j.host.Layout() == nil {
			return nil
		}
		cw, ch := j.host.CellW(), j.host.CellH()
		if cw <= 0 {
			cw = 8
		}
		if ch <= 0 {
			ch = 16
		}
		x := int(px) / cw
		y := int(py)/ch + j.host.Scroll()
		if x < 0 || x >= j.host.Width() || y < 0 {
			return nil
		}
		n := j.host.NodeAt(x, y)
		for n != nil && n.Type != ElementNode {
			n = n.Parent
		}
		return n
	}
	j.method(o, "elementFromPoint", func(call sobek.FunctionCall) sobek.Value {
		return j.wrap(pointNode(call.Argument(0).ToFloat(), call.Argument(1).ToFloat()))
	})
	j.method(o, "elementsFromPoint", func(call sobek.FunctionCall) sobek.Value {
		if n := pointNode(call.Argument(0).ToFloat(), call.Argument(1).ToFloat()); n != nil {
			return j.nodeArray([]*Node{n})
		}
		return j.nodeArray(nil)
	})
	_ = o.Set("location", j.locObj)
	if j.page.URL != nil {
		u := j.page.URL.String()
		j.accessor(o, "URL", func() sobek.Value { return j.vm.ToValue(u) }, nil)
		j.accessor(o, "documentURI", func() sobek.Value { return j.vm.ToValue(u) }, nil)
	}
	j.method(o, "getElementById", func(call sobek.FunctionCall) sobek.Value {
		id := call.Argument(0).String()
		var found *Node
		root.FindAll(func(x *Node) bool {
			if found == nil && AttrValue(x, "id") == id {
				found = x
				return true
			}
			return false
		})
		return j.wrap(found)
	})
	j.method(o, "querySelector", func(call sobek.FunctionCall) sobek.Value {
		return j.wrap(j.querySelector(root, call.Argument(0).String(), true))
	})
	j.method(o, "querySelectorAll", func(call sobek.FunctionCall) sobek.Value {
		return j.nodeArray(j.querySelectorAll(root, call.Argument(0).String(), true))
	})
	j.method(o, "getElementsByTagName", func(call sobek.FunctionCall) sobek.Value {
		tag := strings.ToLower(call.Argument(0).String())
		nodes := root.FindAll(func(x *Node) bool { return tag == "*" || x.Tag == tag })
		return j.nodeArray(nodes)
	})
	j.method(o, "getElementsByName", func(call sobek.FunctionCall) sobek.Value {
		want := call.Argument(0).String()
		nodes := root.FindAll(func(x *Node) bool { return AttrValue(x, "name") == want })
		return j.nodeArray(nodes)
	})
	collection := func(pick func(*Node) bool) func() sobek.Value {
		return func() sobek.Value {
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
	j.method(o, "createElement", func(call sobek.FunctionCall) sobek.Value {
		tag := strings.ToLower(strings.TrimSpace(call.Argument(0).String()))
		if tag == "" {
			tag = "div"
		}
		return j.wrap(&Node{Type: ElementNode, Tag: tag})
	})
	j.method(o, "createTextNode", func(call sobek.FunctionCall) sobek.Value {
		return j.wrap(j.textNode(call.Argument(0).String()))
	})
	j.method(o, "createEvent", func(call sobek.FunctionCall) sobek.Value {
		return j.createEvent(call.Argument(0).String())
	})
	j.method(o, "dispatchEvent", func(call sobek.FunctionCall) sobek.Value {
		return j.vm.ToValue(j.dispatchDocumentEvent(call.Argument(0)))
	})
	j.method(o, "createDocumentFragment", func(sobek.FunctionCall) sobek.Value {
		return j.wrap(&Node{Type: ElementNode, Tag: "#fragment"})
	})
	j.method(o, "importNode", func(call sobek.FunctionCall) sobek.Value {
		node := j.unwrap(call.Argument(0))
		if node == nil {
			return sobek.Null()
		}
		return j.wrap(j.cloneNodeValue(node, call.Argument(1).ToBoolean()))
	})
	j.method(o, "adoptNode", func(call sobek.FunctionCall) sobek.Value {
		node := j.unwrap(call.Argument(0))
		if node == nil {
			return sobek.Null()
		}
		detach(node)
		return call.Argument(0)
	})
	j.method(o, "createElementNS", func(call sobek.FunctionCall) sobek.Value {
		return j.wrap(&Node{
			Type:      ElementNode,
			Tag:       strings.ToLower(call.Argument(1).String()),
			Namespace: call.Argument(0).String(),
		})
	})
	j.method(o, "getElementsByClassName", func(call sobek.FunctionCall) sobek.Value {
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
	j.method(o, "addEventListener", func(call sobek.FunctionCall) sobek.Value {
		j.addListener(&j.docListeners, call.Argument(0).String(), call.Argument(1), call.Argument(2))
		return sobek.Undefined()
	})
	j.method(o, "removeEventListener", func(call sobek.FunctionCall) sobek.Value {
		j.removeListener(j.docListeners, call.Argument(0).String(), call.Argument(1))
		return sobek.Undefined()
	})
	j.installDocumentCompat(o, root)
	return o
}
