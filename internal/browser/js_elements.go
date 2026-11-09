package browser

import (
	"strconv"
	"strings"

	"github.com/dop251/goja"
)

// installElementConstructors defines the browser's element constructor globals
// and records their prototypes. The DOM wrappers are plain Goja objects, so
// setting the matching prototype here makes `instanceof HTMLElement`,
// `instanceof HTMLScriptElement` and similar checks work.
func (j *jsRuntime) installElementConstructors() {
	const script = `
(function () {
	function ctor(name, parent) {
		var f = function () {};
		if (parent) {
			Object.setPrototypeOf(f.prototype, parent.prototype);
		}
		globalThis[name] = f;
		return f;
	}
	var NodeC = ctor("Node");
	var ElementC = ctor("Element", NodeC);
	var HTMLElementC = ctor("HTMLElement", ElementC);
	var SVGElementC = ctor("SVGElement", ElementC);
	var DocumentC = ctor("Document", NodeC);
	var HTMLDocumentC = ctor("HTMLDocument", DocumentC);
	var CharacterDataC = ctor("CharacterData", NodeC);
	var TextC = ctor("Text", CharacterDataC);
	var CommentC = ctor("Comment", TextC);
	var DocumentFragmentC = ctor("DocumentFragment", NodeC);
	var tags = {
		html: "HTMLHtmlElement", head: "HTMLHeadElement", body: "HTMLBodyElement",
		title: "HTMLTitleElement", meta: "HTMLMetaElement", link: "HTMLLinkElement",
		style: "HTMLStyleElement", script: "HTMLScriptElement",
		template: "HTMLTemplateElement", div: "HTMLDivElement", span: "HTMLSpanElement",
		p: "HTMLParagraphElement", a: "HTMLAnchorElement", img: "HTMLImageElement",
		br: "HTMLBRElement", hr: "HTMLHRElement", ul: "HTMLUListElement",
		ol: "HTMLOListElement", li: "HTMLLIElement", dl: "HTMLDListElement",
		table: "HTMLTableElement", thead: "HTMLTableSectionElement",
		tbody: "HTMLTableSectionElement", tfoot: "HTMLTableSectionElement",
		tr: "HTMLTableRowElement", td: "HTMLTableCellElement",
		th: "HTMLTableCellElement", caption: "HTMLTableCaptionElement",
		form: "HTMLFormElement", input: "HTMLInputElement",
		textarea: "HTMLTextAreaElement", button: "HTMLButtonElement",
		select: "HTMLSelectElement", option: "HTMLOptionElement",
		label: "HTMLLabelElement", fieldset: "HTMLFieldSetElement",
		legend: "HTMLLegendElement", iframe: "HTMLIFrameElement",
		canvas: "HTMLCanvasElement", video: "HTMLVideoElement",
		audio: "HTMLAudioElement", source: "HTMLSourceElement",
		track: "HTMLTrackElement", object: "HTMLObjectElement",
		embed: "HTMLEmbedElement", param: "HTMLParamElement",
		pre: "HTMLPreElement", blockquote: "HTMLQuoteElement", q: "HTMLQuoteElement",
		del: "HTMLModElement", ins: "HTMLModElement", time: "HTMLTimeElement",
		data: "HTMLDataElement", progress: "HTMLProgressElement",
		meter: "HTMLMeterElement", output: "HTMLOutputElement",
		details: "HTMLDetailsElement", dialog: "HTMLDialogElement",
		slot: "HTMLSlotElement", menu: "HTMLMenuElement", map: "HTMLMapElement",
		area: "HTMLAreaElement", base: "HTMLBaseElement", svg: "SVGSVGElement"
	};
	var protos = {};
	for (var tag in tags) {
		var name = tags[tag];
		if (!globalThis[name]) {
			ctor(name, tag === "svg" ? SVGElementC : HTMLElementC);
		}
		protos[tag] = globalThis[name].prototype;
	}
	globalThis.__mob3ElementProtos = protos;
	globalThis.__mob3ElementProto = HTMLElementC.prototype;
	globalThis.__mob3TextProto = TextC.prototype;
	globalThis.__mob3DocumentProto = HTMLDocumentC.prototype;
	var names = ["Node", "Element", "HTMLElement", "SVGElement", "Document", "HTMLDocument", "CharacterData", "Text", "Comment", "DocumentFragment"];
	for (var tag2 in tags) {
		if (names.indexOf(tags[tag2]) < 0) {
			names.push(tags[tag2]);
		}
	}
	globalThis.__mob3ElementCtors = names;
})();
`
	if _, err := j.vm.RunScript("element constructors", script); err != nil {
		j.reportError(err)
		return
	}
	j.elementProtos = make(map[string]*goja.Object)
	if o, ok := j.vm.Get("__mob3ElementProtos").(*goja.Object); ok {
		for _, tag := range o.Keys() {
			if p, ok := o.Get(tag).(*goja.Object); ok {
				j.elementProtos[strings.ToLower(tag)] = p
			}
		}
	}
	j.elementProto = j.globalProto("__mob3ElementProto")
	j.textProto = j.globalProto("__mob3TextProto")
	j.docProto = j.globalProto("__mob3DocumentProto")
	if names, ok := j.vm.Get("__mob3ElementCtors").(*goja.Object); ok {
		n := int(names.Get("length").ToInteger())
		for i := 0; i < n; i++ {
			j.elementCtors = append(j.elementCtors, names.Get(strconv.Itoa(i)).String())
		}
	}
	for _, name := range []string{
		"__mob3ElementProtos", "__mob3ElementProto", "__mob3TextProto",
		"__mob3DocumentProto", "__mob3ElementCtors",
	} {
		_ = j.vm.GlobalObject().Delete(name)
	}
}

func (j *jsRuntime) globalProto(name string) *goja.Object {
	if p, ok := j.vm.Get(name).(*goja.Object); ok {
		return p
	}
	return nil
}

func (j *jsRuntime) setElementPrototype(o *goja.Object, n *Node) {
	if n.Type == TextNode {
		if j.textProto != nil {
			_ = o.SetPrototype(j.textProto)
		}
		return
	}
	if p := j.elementProtos[strings.ToLower(n.Tag)]; p != nil {
		_ = o.SetPrototype(p)
		return
	}
	if j.elementProto != nil {
		_ = o.SetPrototype(j.elementProto)
	}
}
