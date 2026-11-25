# JavaScript and web API compatibility

Part of the MOB³ agent guidance. Start at [AGENTS.md](../../AGENTS.md) for the working
agreement, the non-negotiable constraints and the change protocol.

MOB³ targets core DOM plus jQuery-era libraries, not a modern browser
platform. Keep this list current when the JS surface changes.

Works well:
- goja language level: ES5.1 plus much of ES6 (classes, arrows, generators,
  async/await, Promise, Map/Set, Symbol, Proxy, typed arrays).
- DOM core: tree queries and mutation, attributes, `classList`, inline
  `style`, `textContent`/`innerText`/`innerHTML`, cloning, selectors,
  `getBoundingClientRect` (coarse), `getComputedStyle`, `ownerDocument`,
  `defaultView`, `document.implementation`. `offsetWidth`/`offsetHeight` and
  the rectangle read inline and computed CSS first, then a laid-out box, so
  scripted measurements work before the next layout (used by slide-out menus).
- Events: Event/CustomEvent/UI/Mouse/Keyboard/Focus/Input/Pointer/Error/
  Message/Progress constructors, `createEvent`, bubbling `dispatchEvent`,
  `preventDefault`, `composedPath` on scripted and browser-generated events,
  `element.click()`.
- Network: `fetch`, `Headers`/`Request`/`Response`, `XMLHttpRequest`, cookies,
  same-origin, simple CORS cross-origin requests and `fetch` with
  `mode: "no-cors"` (opaque response), `URL`/`URLSearchParams`.
- Storage: in-memory `localStorage`/`sessionStorage` (`getItem`/`setItem`/
  `removeItem`/`clear`, `length`, and named property get/set/delete) and
  `document.cookie`.
- Canvas: `getContext("2d")` is a no-op context with the common method surface;
  no pixels and no WebGL.
- Native `<dialog>` open/close state: `open`, `returnValue`, `show()`,
  `showModal()` and `close()`.
- Compatibility shims (`js_compat.go`): `dataset`, `outerHTML`,
  `insertAdjacentElement/Text`, `getAttributeNames`, `toggleAttribute`,
  `compareDocumentPosition`, `normalize`, `focus`/`blur`/`scrollIntoView`,
  `getClientRects`, `Range`/`Selection`/`getSelection`,
  `createTreeWalker`/`createNodeIterator`, `TextEncoder`/`TextDecoder`,
  `crypto.getRandomValues`/`randomUUID`, `structuredClone`, `AbortController`/
  `AbortSignal`, `btoa`/`atob`, `Blob`/`File`/`FileReader`, `FormData`,
  `URL.createObjectURL` and `queueMicrotask`.
- Platform stubs (`js_stubs.go`): `EventTarget`, `NodeList`/`HTMLCollection`,
  `DOMStringMap`, `CSS` (`escape`/`supports`), `ShadowRoot`, a
  `customElements` registry that upgrades defined elements,
  `IntersectionObserver`/`ResizeObserver` (one synthetic entry),
  `PerformanceObserver` (inert), `Path2D`, an in-memory `ReadableStream`,
  inert `Worker`, Web Animations, and isolated iframe content documents.
- Modules (`js_modules.go`): `<script type="module">` and dynamic `import()`
  load a linked graph with real namespaces and `import.meta.url`. Relative and
  absolute specifiers work, and bare package specifiers resolve through the
  document import map (exact and trailing-slash entries; scopes ignored).

Intentional limits of the shims:
- `structuredClone` covers primitives, plain objects, arrays, Date, Map, Set
  and ArrayBuffer; typed arrays become plain arrays. Functions throw.
- `AbortController` only rejects a `fetch` that has not started yet; requests
  are synchronous under the hood and cannot be cancelled mid-flight.
- `FormData` and Blob object URLs are in-memory only. Multipart uploads, Blob
  image loading and request-body streaming are not implemented.
- `MutationObserver` is a no-op, `MessageChannel` is timer-backed,
  `getSelection`/`Range` are informational, and `TreeWalker` rebuilds the
  preorder list on each step.
- Measurement APIs are coarse: `getBoundingClientRect`/`getClientRects` union
  the element's laid-out fragments and block box, so they are exact to the
  terminal cell; `getComputedStyle` and `offsetWidth`/`offsetHeight` compute an
  element's style on demand when a script created it after the last layout,
  using the layout's cached rule index and the declared `min-width` as the
  width fallback. An element created and measured in one turn still has no
  laid-out box until the next rebuild.
- Observers are synthetic: `IntersectionObserver`/`ResizeObserver` fire once
  and do not follow scrolling or resizing. `ReadableStream` is an in-memory
  reader, `Worker` never runs, and iframe documents are inert stubs.
- Inline `style` exposes `transition`/`transform`, animation properties and
  their vendor-prefixed aliases so libraries' CSS feature detection succeeds,
  but transitions and animations are not rendered.
- Dynamically inserted `<script>` elements do not execute, and `defer`/`async`
  ordering is ignored: every script runs after load in document order.

Known unsupported families (do not present these as available):
- Platform: WebAssembly, real Workers/SharedWorker/ServiceWorker threads
  (`Worker` is an inert stub), WebSocket, EventSource, IndexedDB, Cache API,
  WebAudio, WebGL, WebRTC, Notifications, geolocation, clipboard, permissions,
  mediaDevices, PaymentRequest.
- DOM: Shadow DOM (`attachShadow` and rendered shadow content), custom element
  lifecycle callbacks and attribute reactivity, real asynchronous
  Intersection/Resize/Performance observers (observers fire one synthetic
  entry), iframe browsing contexts (inert stub), video/audio playback,
  `form.elements` (HTMLFormControlsCollection) and `form.requestSubmit()`.
- Network/browser: streaming fetch, upload progress, CORS preflight,
  credentialed cross-origin requests, readable `no-cors` responses (they are
  opaque: status 0, empty body and headers), `sendBeacon` (the call is accepted
  but delivers nothing),
  full History API (`pushState`/`replaceState` are no-ops, `back`/`forward`
  are stubs), multipart forms.
- CSS/layout: named grid rows fall back to block flow; full grid row spans,
  floats inside inline wrappers and across nested blocks, float edge cases
  that cross nested formatting contexts, sticky offsets other than `top`,
  nested `z-index` stacking contexts, animations,
  transitions, generated content beyond literal `::before`/`::after` strings
  (`attr()`, counters, images), background gradients/repeat, full flexbox.

Do not add a stub just to silence an error when a library would then depend on
behavior MOB³ cannot provide; document it here instead or implement the real
subset.
