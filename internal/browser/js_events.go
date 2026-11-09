package browser

import (
	"strconv"
	"strings"

	"github.com/dop251/goja"
)

// eventConstructorScript defines the event constructor globals. The objects are
// ordinary JavaScript objects; the browser only needs their state and method
// surface so libraries can feature-detect, create and dispatch events.
const eventConstructorScript = `
(function () {
	function initEventProps(e, type, init) {
		init = init || {};
		e.type = type === undefined || type === null ? "" : String(type);
		e.bubbles = !!init.bubbles;
		e.cancelable = !!init.cancelable;
		e.composed = !!init.composed;
		e.defaultPrevented = false;
		e.cancelBubble = false;
		e.eventPhase = 0;
		e.target = null;
		e.currentTarget = null;
		e.srcElement = null;
		e.returnValue = true;
		e.isTrusted = false;
		e.timeStamp = typeof performance !== "undefined" && performance.now ? performance.now() : 0;
	}
	function Event(type, init) {
		if (!(this instanceof Event)) return new Event(type, init);
		initEventProps(this, type, init);
	}
	Event.NONE = 0;
	Event.CAPTURING_PHASE = 1;
	Event.AT_TARGET = 2;
	Event.BUBBLING_PHASE = 3;
	Event.prototype.preventDefault = function () {
		if (this.cancelable) this.defaultPrevented = true;
		this.returnValue = false;
	};
	Event.prototype.stopPropagation = function () { this.cancelBubble = true; };
	Event.prototype.stopImmediatePropagation = function () {
		this.cancelBubble = true;
		this.__mob3StopImmediate = true;
	};
	Event.prototype.initEvent = function (type, bubbles, cancelable) {
		initEventProps(this, type, { bubbles: bubbles, cancelable: cancelable });
	};
	Event.prototype.composedPath = function () {
		return this.target ? [this.target] : [];
	};

	function CustomEvent(type, init) {
		if (!(this instanceof CustomEvent)) return new CustomEvent(type, init);
		initEventProps(this, type, init);
		this.detail = init && init.detail !== undefined ? init.detail : null;
	}
	CustomEvent.prototype = Object.create(Event.prototype);
	CustomEvent.prototype.constructor = CustomEvent;
	CustomEvent.prototype.initCustomEvent = function (type, bubbles, cancelable, detail) {
		initEventProps(this, type, { bubbles: bubbles, cancelable: cancelable });
		this.detail = detail;
	};

	function UIEvent(type, init) {
		if (!(this instanceof UIEvent)) return new UIEvent(type, init);
		initEventProps(this, type, init);
		init = init || {};
		this.view = init.view !== undefined ? init.view : null;
		this.detail = init.detail || 0;
		this.which = init.which || 0;
	}
	UIEvent.prototype = Object.create(Event.prototype);
	UIEvent.prototype.constructor = UIEvent;
	UIEvent.prototype.initUIEvent = function (type, bubbles, cancelable, view, detail) {
		initEventProps(this, type, { bubbles: bubbles, cancelable: cancelable });
		this.view = view !== undefined ? view : null;
		this.detail = detail || 0;
	};

	function MouseEvent(type, init) {
		if (!(this instanceof MouseEvent)) return new MouseEvent(type, init);
		UIEvent.call(this, type, init);
		init = init || {};
		this.screenX = init.screenX || 0;
		this.screenY = init.screenY || 0;
		this.clientX = init.clientX || 0;
		this.clientY = init.clientY || 0;
		this.pageX = this.clientX;
		this.pageY = this.clientY;
		this.offsetX = this.clientX;
		this.offsetY = this.clientY;
		this.movementX = init.movementX || 0;
		this.movementY = init.movementY || 0;
		this.button = init.button || 0;
		this.buttons = init.buttons || 0;
		this.relatedTarget = init.relatedTarget !== undefined ? init.relatedTarget : null;
		this.ctrlKey = !!init.ctrlKey;
		this.shiftKey = !!init.shiftKey;
		this.altKey = !!init.altKey;
		this.metaKey = !!init.metaKey;
	}
	MouseEvent.prototype = Object.create(UIEvent.prototype);
	MouseEvent.prototype.constructor = MouseEvent;
	MouseEvent.prototype.initMouseEvent = function (type, bubbles, cancelable, view, detail, screenX, screenY, clientX, clientY, ctrlKey, altKey, shiftKey, metaKey, button, relatedTarget) {
		initEventProps(this, type, { bubbles: bubbles, cancelable: cancelable });
		this.view = view !== undefined ? view : null;
		this.detail = detail || 0;
		this.screenX = screenX || 0;
		this.screenY = screenY || 0;
		this.clientX = clientX || 0;
		this.clientY = clientY || 0;
		this.pageX = this.clientX;
		this.pageY = this.clientY;
		this.ctrlKey = !!ctrlKey;
		this.altKey = !!altKey;
		this.shiftKey = !!shiftKey;
		this.metaKey = !!metaKey;
		this.button = button || 0;
		this.relatedTarget = relatedTarget !== undefined ? relatedTarget : null;
	};

	function KeyboardEvent(type, init) {
		if (!(this instanceof KeyboardEvent)) return new KeyboardEvent(type, init);
		UIEvent.call(this, type, init);
		init = init || {};
		this.key = init.key !== undefined ? String(init.key) : "";
		this.code = init.code !== undefined ? String(init.code) : "";
		this.location = init.location || 0;
		this.repeat = !!init.repeat;
		this.isComposing = !!init.isComposing;
		this.keyCode = init.keyCode || 0;
		this.charCode = init.charCode || 0;
		this.which = init.which || this.keyCode;
		this.ctrlKey = !!init.ctrlKey;
		this.shiftKey = !!init.shiftKey;
		this.altKey = !!init.altKey;
		this.metaKey = !!init.metaKey;
	}
	KeyboardEvent.prototype = Object.create(UIEvent.prototype);
	KeyboardEvent.prototype.constructor = KeyboardEvent;
	KeyboardEvent.prototype.initKeyboardEvent = function (type, bubbles, cancelable, view, key, location, ctrlKey, altKey, shiftKey, metaKey) {
		initEventProps(this, type, { bubbles: bubbles, cancelable: cancelable });
		this.view = view !== undefined ? view : null;
		this.key = key !== undefined ? String(key) : "";
		this.location = location || 0;
		this.ctrlKey = !!ctrlKey;
		this.altKey = !!altKey;
		this.shiftKey = !!shiftKey;
		this.metaKey = !!metaKey;
	};

	function FocusEvent(type, init) {
		if (!(this instanceof FocusEvent)) return new FocusEvent(type, init);
		UIEvent.call(this, type, init);
		init = init || {};
		this.relatedTarget = init.relatedTarget !== undefined ? init.relatedTarget : null;
	}
	FocusEvent.prototype = Object.create(UIEvent.prototype);
	FocusEvent.prototype.constructor = FocusEvent;

	function InputEvent(type, init) {
		if (!(this instanceof InputEvent)) return new InputEvent(type, init);
		UIEvent.call(this, type, init);
		init = init || {};
		this.data = init.data !== undefined ? init.data : null;
		this.inputType = init.inputType || "";
		this.isComposing = !!init.isComposing;
	}
	InputEvent.prototype = Object.create(UIEvent.prototype);
	InputEvent.prototype.constructor = InputEvent;

	function PointerEvent(type, init) {
		if (!(this instanceof PointerEvent)) return new PointerEvent(type, init);
		MouseEvent.call(this, type, init);
		init = init || {};
		this.pointerId = init.pointerId || 0;
		this.pointerType = init.pointerType || "";
		this.isPrimary = !!init.isPrimary;
		this.width = init.width || 1;
		this.height = init.height || 1;
		this.pressure = init.pressure || 0;
	}
	PointerEvent.prototype = Object.create(MouseEvent.prototype);
	PointerEvent.prototype.constructor = PointerEvent;

	function ErrorEvent(type, init) {
		if (!(this instanceof ErrorEvent)) return new ErrorEvent(type, init);
		initEventProps(this, type, init);
		init = init || {};
		this.message = init.message !== undefined ? String(init.message) : "";
		this.filename = init.filename !== undefined ? String(init.filename) : "";
		this.lineno = init.lineno || 0;
		this.colno = init.colno || 0;
		this.error = init.error !== undefined ? init.error : null;
	}
	ErrorEvent.prototype = Object.create(Event.prototype);
	ErrorEvent.prototype.constructor = ErrorEvent;

	function MessageEvent(type, init) {
		if (!(this instanceof MessageEvent)) return new MessageEvent(type, init);
		initEventProps(this, type, init);
		init = init || {};
		this.data = init.data !== undefined ? init.data : null;
		this.origin = init.origin || "";
		this.lastEventId = init.lastEventId || "";
		this.source = init.source !== undefined ? init.source : null;
		this.ports = [];
	}
	MessageEvent.prototype = Object.create(Event.prototype);
	MessageEvent.prototype.constructor = MessageEvent;

	function ProgressEvent(type, init) {
		if (!(this instanceof ProgressEvent)) return new ProgressEvent(type, init);
		initEventProps(this, type, init);
		init = init || {};
		this.lengthComputable = !!init.lengthComputable;
		this.loaded = init.loaded || 0;
		this.total = init.total || 0;
	}
	ProgressEvent.prototype = Object.create(Event.prototype);
	ProgressEvent.prototype.constructor = ProgressEvent;

	function MessagePort() {
		this.onmessage = null;
		this.onmessageerror = null;
		this._listeners = [];
		this._other = null;
	}
	MessagePort.prototype.addEventListener = function (type, fn) {
		if (typeof fn === "function") this._listeners.push({ type: type, fn: fn });
	};
	MessagePort.prototype.removeEventListener = function (type, fn) {
		this._listeners = this._listeners.filter(function (l) {
			return !(l.type === type && l.fn === fn);
		});
	};
	MessagePort.prototype.start = function () {};
	MessagePort.prototype.close = function () { this._other = null; };
	MessagePort.prototype._deliver = function (ev) {
		var h = this.onmessage;
		if (typeof h === "function") h.call(this, ev);
		for (var i = 0; i < this._listeners.length; i++) {
			if (this._listeners[i].type === ev.type) this._listeners[i].fn.call(this, ev);
		}
	};
	MessagePort.prototype.postMessage = function (data) {
		var port = this._other;
		if (!port) return;
		setTimeout(function () {
			port._deliver({ type: "message", data: data, target: port, currentTarget: port });
		}, 0);
	};
	function MessageChannel() {
		this.port1 = new MessagePort();
		this.port2 = new MessagePort();
		this.port1._other = this.port2;
		this.port2._other = this.port1;
	}

	function MutationObserver(callback) {
		this._callback = typeof callback === "function" ? callback : null;
	}
	MutationObserver.prototype.observe = function () {};
	MutationObserver.prototype.disconnect = function () {};
	MutationObserver.prototype.takeRecords = function () { return []; };

	globalThis.NodeFilter = {
		FILTER_ACCEPT: 1, FILTER_REJECT: 2, FILTER_SKIP: 3,
		SHOW_ALL: 4294967295, SHOW_ELEMENT: 1, SHOW_ATTRIBUTE: 2,
		SHOW_TEXT: 4, SHOW_CDATA_SECTION: 8, SHOW_ENTITY_REFERENCE: 16,
		SHOW_ENTITY: 32, SHOW_PROCESSING_INSTRUCTION: 64,
		SHOW_COMMENT: 128, SHOW_DOCUMENT: 256, SHOW_DOCUMENT_TYPE: 512,
		SHOW_DOCUMENT_FRAGMENT: 1024, SHOW_NOTATION: 2048
	};

	globalThis.Event = Event;
	globalThis.CustomEvent = CustomEvent;
	globalThis.UIEvent = UIEvent;
	globalThis.MouseEvent = MouseEvent;
	globalThis.KeyboardEvent = KeyboardEvent;
	globalThis.FocusEvent = FocusEvent;
	globalThis.InputEvent = InputEvent;
	globalThis.PointerEvent = PointerEvent;
	globalThis.ErrorEvent = ErrorEvent;
	globalThis.MessageEvent = MessageEvent;
	globalThis.ProgressEvent = ProgressEvent;
	globalThis.MessageChannel = MessageChannel;
	globalThis.MessagePort = MessagePort;
	globalThis.MutationObserver = MutationObserver;
	if (typeof Node !== "undefined") {
		Node.ELEMENT_NODE = 1;
		Node.ATTRIBUTE_NODE = 2;
		Node.TEXT_NODE = 3;
		Node.CDATA_SECTION_NODE = 4;
		Node.ENTITY_REFERENCE_NODE = 5;
		Node.ENTITY_NODE = 6;
		Node.PROCESSING_INSTRUCTION_NODE = 7;
		Node.COMMENT_NODE = 8;
		Node.DOCUMENT_NODE = 9;
		Node.DOCUMENT_TYPE_NODE = 10;
		Node.DOCUMENT_FRAGMENT_NODE = 11;
		Node.NOTATION_NODE = 12;
		Node.DOCUMENT_POSITION_DISCONNECTED = 1;
		Node.DOCUMENT_POSITION_PRECEDING = 2;
		Node.DOCUMENT_POSITION_FOLLOWING = 4;
		Node.DOCUMENT_POSITION_CONTAINS = 8;
		Node.DOCUMENT_POSITION_CONTAINED_BY = 16;
		Node.DOCUMENT_POSITION_IMPLEMENTATION_SPECIFIC = 32;
	}
})();
`

// installEventConstructors defines Event and its common subclasses and records
// them for document.createEvent.
func (j *jsRuntime) installEventConstructors() {
	if _, err := j.vm.RunScript("event constructors", eventConstructorScript); err != nil {
		j.reportError(err)
		return
	}
	j.eventCtors = make(map[string]*goja.Object)
	for _, name := range []string{
		"Event", "CustomEvent", "UIEvent", "MouseEvent", "KeyboardEvent",
		"FocusEvent", "InputEvent", "PointerEvent", "ErrorEvent", "MessageEvent",
		"ProgressEvent",
	} {
		if o, ok := j.vm.Get(name).(*goja.Object); ok {
			j.eventCtors[name] = o
		}
	}
}

// eventConstructor maps a document.createEvent interface name to a constructor.
func (j *jsRuntime) eventConstructor(name string) *goja.Object {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "customevent":
		return j.eventCtors["CustomEvent"]
	case "mouseevent", "mouseevents":
		return j.eventCtors["MouseEvent"]
	case "keyboardevent", "keyboardevents":
		return j.eventCtors["KeyboardEvent"]
	case "uievent", "uievents":
		return j.eventCtors["UIEvent"]
	case "focusevent":
		return j.eventCtors["FocusEvent"]
	case "inputevent":
		return j.eventCtors["InputEvent"]
	case "pointerevent":
		return j.eventCtors["PointerEvent"]
	case "errorevent":
		return j.eventCtors["ErrorEvent"]
	case "messageevent":
		return j.eventCtors["MessageEvent"]
	case "progressevent":
		return j.eventCtors["ProgressEvent"]
	default:
		return j.eventCtors["Event"]
	}
}

// createEvent implements document.createEvent.
func (j *jsRuntime) createEvent(name string) goja.Value {
	ctor := j.eventConstructor(name)
	if ctor == nil {
		return j.vm.NewObject()
	}
	o, err := j.vm.New(ctor)
	if err != nil || o == nil {
		return j.vm.NewObject()
	}
	return o
}

func (j *jsRuntime) eventType(ev *goja.Object) string {
	if ev == nil {
		return ""
	}
	if v := ev.Get("type"); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
		return strings.TrimSpace(v.String())
	}
	return ""
}

func (j *jsRuntime) eventFlag(ev *goja.Object, name string) bool {
	if ev == nil {
		return false
	}
	if v := ev.Get(name); v != nil && !goja.IsUndefined(v) && !goja.IsNull(v) {
		return v.ToBoolean()
	}
	return false
}

func (j *jsRuntime) setEventCurrent(ev *goja.Object, this goja.Value) {
	if ev == nil {
		return
	}
	_ = ev.Set("currentTarget", this)
}

func (j *jsRuntime) eventStopped(ev *goja.Object) bool {
	return j.eventFlag(ev, "cancelBubble")
}

// callEventListeners runs one listener list for a script-created event.
func (j *jsRuntime) callEventListeners(m map[string][]jsListener, typ string, this goja.Value, ev *goja.Object) {
	list := m[typ]
	for i := 0; i < len(list); i++ {
		l := list[i]
		j.call(l.call, this, ev)
		if l.once {
			list = append(list[:i], list[i+1:]...)
			m[typ] = list
			i--
		}
		if j.eventStopped(ev) {
			return
		}
	}
}

// dispatchNodeEvent implements element.dispatchEvent. Events bubble only when
// their bubbles flag is set, then reach document and window.
func (j *jsRuntime) dispatchNodeEvent(n *Node, ev goja.Value) bool {
	obj, _ := ev.(*goja.Object)
	if obj == nil || n == nil {
		return true
	}
	typ := j.eventType(obj)
	if typ == "" {
		return true
	}
	bubbles := j.eventFlag(obj, "bubbles")
	_ = obj.Set("target", j.wrap(n))
	for p := n; p != nil && !j.eventStopped(obj); p = p.Parent {
		if p != n && !bubbles {
			break
		}
		if p.Type != ElementNode {
			continue
		}
		j.setEventCurrent(obj, j.wrap(p))
		if m := j.nodeListeners[p]; m != nil {
			j.callEventListeners(m, typ, j.wrap(p), obj)
		}
		if m := j.propListeners[p]; m != nil {
			if l, ok := m[typ]; ok {
				j.call(l.call, j.wrap(p), obj)
			}
		}
		if typ == "click" {
			j.inlineHandler(p, "onclick", obj, nil)
		}
	}
	if bubbles && !j.eventStopped(obj) {
		j.setEventCurrent(obj, j.docObj)
		j.callEventListeners(j.docListeners, typ, j.docObj, obj)
	}
	if bubbles && !j.eventStopped(obj) {
		j.setEventCurrent(obj, j.winObj)
		j.callEventListeners(j.winListeners, typ, j.winObj, obj)
	}
	return !j.eventFlag(obj, "defaultPrevented")
}

// dispatchDocumentEvent implements document.dispatchEvent.
func (j *jsRuntime) dispatchDocumentEvent(ev goja.Value) bool {
	obj, _ := ev.(*goja.Object)
	typ := j.eventType(obj)
	if typ == "" {
		return true
	}
	_ = obj.Set("target", j.docObj)
	j.setEventCurrent(obj, j.docObj)
	j.callEventListeners(j.docListeners, typ, j.docObj, obj)
	if j.eventFlag(obj, "bubbles") && !j.eventStopped(obj) {
		j.setEventCurrent(obj, j.winObj)
		j.callEventListeners(j.winListeners, typ, j.winObj, obj)
	}
	return !j.eventFlag(obj, "defaultPrevented")
}

// dispatchWindowEvent implements window.dispatchEvent.
func (j *jsRuntime) dispatchWindowEvent(ev goja.Value) bool {
	obj, _ := ev.(*goja.Object)
	typ := j.eventType(obj)
	if typ == "" {
		return true
	}
	_ = obj.Set("target", j.winObj)
	j.setEventCurrent(obj, j.winObj)
	j.callEventListeners(j.winListeners, typ, j.winObj, obj)
	return !j.eventFlag(obj, "defaultPrevented")
}

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
