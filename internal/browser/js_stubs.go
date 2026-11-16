package browser

import (
	"github.com/dop251/goja"
)

// installPlatformStubs registers the browser-platform interfaces that pages
// commonly feature-detect or call before doing anything else. They are shaped
// like the real interfaces but deliberately small: observers report one
// synthetic entry, workers and performance observers are inert, and the stream
// type is a simple in-memory reader. Shadow DOM and iframe browsing contexts
// are still not implemented.
func (j *jsRuntime) installPlatformStubs() {
	const script = `
(function () {
	function define(name, value) {
		if (typeof globalThis[name] !== "undefined") {
			return globalThis[name];
		}
		try {
			Object.defineProperty(globalThis, name, {
				value: value, writable: true, configurable: true
			});
		} catch (e) {
			globalThis[name] = value;
		}
		return value;
	}
	function ctor(name, proto) {
		var f = function () {};
		if (proto) {
			Object.setPrototypeOf(f.prototype, proto);
		}
		return define(name, f);
	}

	// EventTarget and the collection/interface constructors that libraries use
	// for instanceof checks and for installing polyfills on their prototypes.
	var EventTargetC = ctor("EventTarget");
	EventTargetC.prototype.addEventListener = function () {};
	EventTargetC.prototype.removeEventListener = function () {};
	EventTargetC.prototype.dispatchEvent = function () { return true; };
	try {
		if (typeof Node !== "undefined" && Node.prototype) {
			Object.setPrototypeOf(Node.prototype, EventTargetC.prototype);
		}
	} catch (e) {}

	var NodeListC = ctor("NodeList");
	NodeListC.prototype.item = function (i) { return this[i] === undefined ? null : this[i]; };
	NodeListC.prototype.forEach = function (fn, thisArg) {
		for (var i = 0; i < this.length; i++) { fn.call(thisArg, this[i], i, this); }
	};
	NodeListC.prototype.entries = function () {
		var i = 0, self = this;
		return { next: function () {
			if (i >= self.length) { return { value: undefined, done: true }; }
			var n = i++;
			return { value: [n, self[n]], done: false };
		} };
	};
	NodeListC.prototype.keys = function () {
		var i = 0, self = this;
		return { next: function () {
			return i < self.length ? { value: i++, done: false } : { value: undefined, done: true };
		} };
	};
	NodeListC.prototype.values = function () {
		var i = 0, self = this;
		return { next: function () {
			return i < self.length ? { value: self[i++], done: false } : { value: undefined, done: true };
		} };
	};
	var HTMLCollectionC = ctor("HTMLCollection");
	HTMLCollectionC.prototype.item = NodeListC.prototype.item;
	HTMLCollectionC.prototype.namedItem = function (name) {
		for (var i = 0; i < this.length; i++) {
			var el = this[i];
			if (el && (el.id === name || el.getAttribute && el.getAttribute("name") === name)) {
				return el;
			}
		}
		return null;
	};
	ctor("DOMStringMap");
	ctor("ShadowRoot");
	ctor("Attr");
	ctor("NamedNodeMap");

	// CSS escape/supports are used by selector-building libraries and by
	// feature detection. supports() is permissive: layout support is separate.
	var css = define("CSS", {});
	css.escape = css.escape || function (value) {
		var s = String(value), out = "";
		for (var i = 0; i < s.length; i++) {
			var c = s.charCodeAt(i), ch = s.charAt(i);
			if (c === 0) {
				out += "\uFFFD";
			} else if ((c >= 1 && c <= 31) || c === 127 ||
				(i === 0 && c >= 48 && c <= 57) ||
				(i === 1 && c >= 48 && c <= 57 && s.charCodeAt(0) === 45)) {
				out += "\\" + c.toString(16) + " ";
			} else if (c >= 128 || c === 45 || c === 95 ||
				(c >= 48 && c <= 57) || (c >= 65 && c <= 90) || (c >= 97 && c <= 122)) {
				out += ch;
			} else {
				out += "\\" + ch;
			}
		}
		return out;
	};
	css.supports = css.supports || function () { return true; };
	css.registerProperty = css.registerProperty || function () {};
	css.px = css.px || function (v) { return Number(v) + "px"; };

	// Custom elements registry. Definitions are remembered so get/whenDefined
	// work, but elements are not upgraded.
	var customRegistry = {};
	define("customElements", {
		define: function (name, ctor) { customRegistry[name] = ctor; },
		get: function (name) { return customRegistry[name]; },
		whenDefined: function (name) { return Promise.resolve(customRegistry[name]); },
		upgrade: function () {}
	});

	// Observers report a single synthetic entry. This lets IntersectionObserver
	// driven lazy loading and analytics continue instead of throwing, without
	// pretending to track real viewport changes.
	function rectOf(target) {
		if (target && typeof target.getBoundingClientRect === "function") {
			return target.getBoundingClientRect();
		}
		return { x: 0, y: 0, top: 0, left: 0, right: 0, bottom: 0, width: 0, height: 0 };
	}
	function IntersectionObserver(callback) {
		this._callback = callback;
		this._targets = [];
	}
	IntersectionObserver.prototype.observe = function (target) {
		if (!target || this._targets.indexOf(target) >= 0) { return; }
		this._targets.push(target);
		var self = this;
		setTimeout(function () {
			if (self._targets.indexOf(target) < 0) { return; }
			var r = rectOf(target);
			var entry = {
				target: target, isIntersecting: true, intersectionRatio: 1,
				boundingClientRect: r, intersectionRect: r, rootBounds: null,
				time: typeof performance !== "undefined" && performance.now ? performance.now() : 0
			};
			try {
				if (typeof self._callback === "function") { self._callback([entry], self); }
			} catch (e) {
				if (typeof console !== "undefined" && console.error) { console.error(e); }
			}
		}, 0);
	};
	IntersectionObserver.prototype.unobserve = function (target) {
		var i = this._targets.indexOf(target);
		if (i >= 0) { this._targets.splice(i, 1); }
	};
	IntersectionObserver.prototype.disconnect = function () { this._targets = []; };
	IntersectionObserver.prototype.takeRecords = function () { return []; };
	define("IntersectionObserver", IntersectionObserver);
	ctor("IntersectionObserverEntry");

	function ResizeObserver(callback) {
		this._callback = callback;
		this._targets = [];
	}
	ResizeObserver.prototype.observe = function (target) {
		if (!target || this._targets.indexOf(target) >= 0) { return; }
		this._targets.push(target);
		var self = this;
		setTimeout(function () {
			if (self._targets.indexOf(target) < 0) { return; }
			var r = rectOf(target);
			var entry = {
				target: target, contentRect: r,
				borderBoxSize: [{ inlineSize: r.width, blockSize: r.height }],
				contentBoxSize: [{ inlineSize: r.width, blockSize: r.height }]
			};
			try {
				if (typeof self._callback === "function") { self._callback([entry], self); }
			} catch (e) {
				if (typeof console !== "undefined" && console.error) { console.error(e); }
			}
		}, 0);
	};
	ResizeObserver.prototype.unobserve = IntersectionObserver.prototype.unobserve;
	ResizeObserver.prototype.disconnect = IntersectionObserver.prototype.disconnect;
	define("ResizeObserver", ResizeObserver);

	function PerformanceObserver(callback) { this._callback = callback; }
	PerformanceObserver.prototype.observe = function () {};
	PerformanceObserver.prototype.disconnect = function () {};
	PerformanceObserver.prototype.takeRecords = function () { return []; };
	define("PerformanceObserver", PerformanceObserver);

	// Canvas path objects are accepted as no-ops because the canvas itself has
	// no pixel buffer.
	function Path2D() {}
	var pathNoop = function () {};
	var pathMethods = ["moveTo", "lineTo", "arc", "arcTo", "rect", "roundRect",
		"bezierCurveTo", "quadraticCurveTo", "closePath", "addPath", "ellipse"];
	for (var i = 0; i < pathMethods.length; i++) {
		Path2D.prototype[pathMethods[i]] = pathNoop;
	}
	define("Path2D", Path2D);

	// A minimal in-memory ReadableStream. fetch bodies are still not streamed.
	function ReadableStream(source) {
		this._chunks = [];
		this._closed = false;
		this._error = null;
		this.locked = false;
		var self = this;
		var controller = {
			desiredSize: 1,
			enqueue: function (chunk) { self._chunks.push(chunk); },
			close: function () { self._closed = true; },
			error: function (err) { self._error = err; self._closed = true; }
		};
		this._controller = controller;
		if (source && typeof source.start === "function") {
			try { source.start(controller); } catch (e) { self._error = e; self._closed = true; }
		}
	}
	ReadableStream.prototype.getReader = function () {
		var self = this;
		this.locked = true;
		return {
			read: function () {
				if (self._chunks.length > 0) {
					return Promise.resolve({ value: self._chunks.shift(), done: false });
				}
				if (self._error) { return Promise.reject(self._error); }
				return Promise.resolve({ value: undefined, done: true });
			},
			cancel: function () { self._closed = true; return Promise.resolve(); },
			releaseLock: function () { self.locked = false; },
			closed: Promise.resolve()
		};
	};
	ReadableStream.prototype.cancel = function () { this._closed = true; return Promise.resolve(); };
	ReadableStream.prototype.pipeTo = function () { return Promise.resolve(); };
	ReadableStream.prototype.tee = function () { return [this, this]; };
	define("ReadableStream", ReadableStream);

	// Workers never run script; they only need to be constructible.
	function Worker() { this.onmessage = null; this.onerror = null; }
	Worker.prototype.postMessage = function () {};
	Worker.prototype.terminate = function () {};
	Worker.prototype.addEventListener = function () {};
	Worker.prototype.removeEventListener = function () {};
	define("Worker", Worker);

	// Web Animations are stubbed with already-finished animations.
	function animation() {
		return {
			finished: Promise.resolve(), onfinish: null,
			play: function () {}, pause: function () {}, cancel: function () {},
			finish: function () {}, reverse: function () {},
			updatePlaybackRate: function () {}, commitStyles: function () {},
			persist: function () {}, addEventListener: function () {},
			removeEventListener: function () {}
		};
	}
	if (typeof Element !== "undefined" && Element.prototype) {
		Element.prototype.animate = Element.prototype.animate || animation;
		Element.prototype.getAnimations = Element.prototype.getAnimations || function () { return []; };
		// Polyfills often save Element.prototype.matches/closest and call them
		// with a DOM node as the receiver. The node wrappers carry their own
		// methods, so the prototype entries only need to delegate to them.
		function delegateMethod(name, fallback) {
			if (Element.prototype[name]) { return; }
			Element.prototype[name] = function () {
				var own = this[name];
				if (typeof own === "function" && own !== Element.prototype[name]) {
					return own.apply(this, arguments);
				}
				return fallback;
			};
		}
		delegateMethod("matches", false);
		delegateMethod("closest", null);
		delegateMethod("getBoundingClientRect", null);
		if (!Element.prototype.msMatchesSelector) {
			Element.prototype.msMatchesSelector = Element.prototype.matches;
		}
		if (!Element.prototype.webkitMatchesSelector) {
			Element.prototype.webkitMatchesSelector = Element.prototype.matches;
		}
	}
	if (typeof document !== "undefined") {
		document.getAnimations = document.getAnimations || function () { return []; };
		if (!document.startViewTransition) {
			document.startViewTransition = function (callback) {
				if (typeof callback === "function") {
					try { callback(); } catch (e) {}
				}
				return {
					ready: Promise.resolve(), finished: Promise.resolve(),
					updateCallbackDone: Promise.resolve(),
					skipTransition: function () {}
				};
			};
		}
	}


	// Constructors commonly checked with instanceof or used to create values.
	ctor("DOMTokenList");
	ctor("CSSStyleDeclaration");
	ctor("CSSStyleSheet");
	ctor("StyleSheet");
	ctor("MediaQueryList");
	ctor("NodeIterator");
	ctor("TreeWalker");
	ctor("CanvasRenderingContext2D");

	function ImageData(width, height, data) {
		if (width && typeof width === "object") {
			this.width = width.width | 0;
			this.height = width.height | 0;
			this.data = width.data || new Uint8ClampedArray(this.width * this.height * 4);
			return;
		}
		this.width = width | 0;
		this.height = height | 0;
		this.data = data || new Uint8ClampedArray(this.width * this.height * 4);
	}
	define("ImageData", ImageData);

	// Event subclasses that pages construct directly.
	function eventSubclass(name) {
		if (typeof globalThis[name] !== "undefined") { return; }
		var C = function (type, init) {
			if (!(this instanceof C)) { return new C(type, init); }
			Event.call(this, type, init);
			if (init) {
				for (var k in init) {
					if (k === "bubbles" || k === "cancelable" || k === "composed") { continue; }
					try { this[k] = init[k]; } catch (e) {}
				}
			}
		};
		C.prototype = Object.create(Event.prototype);
		C.prototype.constructor = C;
		define(name, C);
	}
	["DragEvent", "ClipboardEvent", "WheelEvent", "SubmitEvent", "StorageEvent",
		"PopStateEvent", "HashChangeEvent", "BeforeUnloadEvent", "PageTransitionEvent",
		"AnimationEvent", "TransitionEvent", "MediaQueryListEvent",
		"TouchEvent", "CompositionEvent", "FormDataEvent"].forEach(eventSubclass);

	function DataTransfer() {
		this.dropEffect = "none";
		this.effectAllowed = "all";
		this.items = [];
		this.types = [];
		this.files = [];
		this._data = {};
	}
	DataTransfer.prototype.setData = function (type, data) {
		this._data[type] = String(data);
		if (this.types.indexOf(type) < 0) { this.types.push(type); }
	};
	DataTransfer.prototype.getData = function (type) { return this._data[type] || ""; };
	DataTransfer.prototype.clearData = function (type) {
		if (type === undefined) { this._data = {}; this.types = []; }
		else { delete this._data[type]; this.types = this.types.filter(function (t) { return t !== type; }); }
	};
	DataTransfer.prototype.setDragImage = function () {};
	define("DataTransfer", DataTransfer);

	// DOMParser parses HTML fragments into a detached document. XML and SVG
	// modes use the same HTML parser; XMLSerializer uses our serialized HTML.
	function DOMParser() {}
	DOMParser.prototype.parseFromString = function (text, type) {
		var doc = document.implementation.createHTMLDocument("");
		try {
			doc.body.innerHTML = String(text);
		} catch (e) {}
		return doc;
	};
	define("DOMParser", DOMParser);
	function XMLSerializer() {}
	XMLSerializer.prototype.serializeToString = function (node) {
		if (!node) { return ""; }
		if (node.nodeType === 9 && node.documentElement) { node = node.documentElement; }
		if (node.outerHTML !== undefined) { return String(node.outerHTML); }
		return String(node.textContent || "");
	};
	define("XMLSerializer", XMLSerializer);

	// An iframe has no browsing context. Give it an isolated, inert document so
	// scripts that write into it do not touch the real page.
	var frameDoc = null;
	function makeFrameDocument() {
		var doc = {
			readyState: "complete", title: "", cookie: "",
			URL: typeof location !== "undefined" ? location.href : "about:blank",
			baseURI: typeof location !== "undefined" ? location.href : "about:blank",
			open: function () { return doc; },
			close: function () {},
			write: function () {},
			writeln: function () {},
			getElementById: function () { return null; },
			getElementsByTagName: function () { return []; },
			getElementsByClassName: function () { return []; },
			querySelector: function () { return null; },
			querySelectorAll: function () { return []; },
			addEventListener: function () {},
			removeEventListener: function () {},
			dispatchEvent: function () { return true; }
		};
		if (typeof document !== "undefined") {
			doc.createElement = function (tag) { return document.createElement(tag); };
			doc.createTextNode = function (text) { return document.createTextNode(text); };
			doc.documentElement = document.createElement("html");
			doc.head = document.createElement("head");
			doc.body = document.createElement("body");
		}
		var win = {
			document: doc,
			location: typeof location !== "undefined" ? location : {},
			frames: [], frameElement: null,
			addEventListener: function () {},
			removeEventListener: function () {},
			postMessage: function () {}
		};
		if (typeof window !== "undefined") {
			win.parent = window;
			win.top = window;
			win.self = win;
		}
		doc.defaultView = win;
		return doc;
	}
	if (typeof HTMLIFrameElement !== "undefined" && HTMLIFrameElement.prototype) {
		Object.defineProperty(HTMLIFrameElement.prototype, "contentDocument", {
			configurable: true,
			get: function () {
				if (!frameDoc) { frameDoc = makeFrameDocument(); }
				return frameDoc;
			}
		});
		Object.defineProperty(HTMLIFrameElement.prototype, "contentWindow", {
			configurable: true,
			get: function () {
				if (!frameDoc) { frameDoc = makeFrameDocument(); }
				return frameDoc.defaultView;
			}
		});
	}
})();
`
	if _, err := j.vm.RunScript("platform stubs", script); err != nil {
		j.reportError(err)
		return
	}
	protoOf := func(name string) *goja.Object {
		v := j.vm.Get(name)
		o, ok := v.(*goja.Object)
		if !ok {
			return nil
		}
		proto, _ := o.Get("prototype").(*goja.Object)
		return proto
	}
	j.domStringMapProto = protoOf("DOMStringMap")
	j.domTokenListProto = protoOf("DOMTokenList")
	j.cssStyleProto = protoOf("CSSStyleDeclaration")
	j.mediaQueryProto = protoOf("MediaQueryList")
	j.treeWalkerProto = protoOf("TreeWalker")
	j.nodeIteratorProto = protoOf("NodeIterator")
}
