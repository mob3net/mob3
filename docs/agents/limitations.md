# Known limitations

Part of the MOB³ agent guidance. Start at [AGENTS.md](../../AGENTS.md) for the working
agreement, the non-negotiable constraints and the change protocol.

- JavaScript is a practical subset: CSP is unsupported. ES modules run, and a
  document import map resolves bare specifiers (exact and prefix entries;
  `scopes` are ignored), but module requests are not CORS-checked, there is no
  `import.meta.resolve` or `import.meta.env`, and module sources are fetched
  while the page is applied, so a slow graph briefly holds up the UI before the
  first paint. A module that fails to load, link or
  evaluate shows `Module script failed — see ! console` in the footer. A page
  that lazily imports many chunks still evaluates each one on the main loop, so
  a large client-side app can keep a core busy after the first paint. Async
  generator syntax (`async function*`) and `for await` are still a parse error,
  so a script containing them is skipped. Custom elements are constructed
  (field initializers run, `attachShadow` in the constructor works) but
  lifecycle callbacks (`connectedCallback`, `attributeChangedCallback`,
  `adoptedCallback`) are not fired, `customElements.upgrade` is a no-op, and
  shadow content is not rendered or matched by page selectors. Because the
  shadow tree is absent, light-DOM content that a component would only project
  into an unopened menu, drawer or popover (a sort or filter dropdown, an
  overflow menu) is laid out inline; only the page's own CSS can hide it.
  Selector rules using `:defined` are dropped, so a page's SSR "not yet
  upgraded" hiding rules never apply. Storage
  and cookies are in-memory only and do not persist. `localStorage` is shared
  per origin across tabs and `sessionStorage` is per tab and origin; each area
  is capped at 5 MiB and the context at 32 MiB, and opaque origins get a
  private area. Cookies are session-only; an explicit `SameSite` is enforced
  (`Strict` same-site only, `Lax` also on top-level GET/HEAD, `None` only when
  Secure over HTTPS), a cookie without one keeps the legacy send-everywhere
  behavior, and there is no `CookieStore` API.
  `XMLHttpRequest` and `fetch` are synchronous, support same-origin requests,
  simple CORS cross-origin responses and opaque `no-cors` fetches, and lack
  streaming/upload progress; CORS preflight and credentialed cross-origin
  requests are unsupported.
  `AbortController` only cancels fetches that have
  not started. `Intl` is an
  English-only subset without full CLDR locale data. `document.write` is
  supported for document fragments. JS history navigation is stubbed. Plain
  same-origin `location` redirects and meta refresh work; local files are not
  supported, so `file:` URLs are rejected. Canvas is a feature-detection stub
  with no pixels or
  WebGL, `MutationObserver` never reports mutations, and `MessageChannel`
  delivery is timer-based.
  Apps requiring missing APIs (for example Google search) may still show their
  no-JavaScript fallback.
- **CSS `:has()`:** supported with a bounded descendant/sibling search.
  Very large subtrees can exceed the search budget and are treated as
  non-matching rather than scanning without a limit. Selector matching itself
  is memoized and step-budgeted, so a pathological descendant selector is also
  treated as non-matching instead of freezing the style pass or `querySelector`.
- Dynamic `import()` loads a real module namespace and rejects on failure.
  Iframes have no browsing context and expose an inert stub document.
  Observers are synthetic: `IntersectionObserver`/`ResizeObserver` fire once
  and do not track scrolling or resizing. `ReadableStream` is an in-memory
  reader, not a streaming fetch body; `Worker` never executes.
- `-engine=process` runs one engine child per interactive tab. The parent still
  owns the terminal, host policy, cookies, cache, settings, history and shared
  `localStorage`/`sessionStorage`. `-mem-limit` is a per-tab cap in process
  mode, so the total across tabs is not bounded by one process-wide limit;
  in-process mode still divides the pools across tabs. Dump, PNG and MCP stay
  in-process.
- Tabs are implemented, but iframes are permanent inert stubs, not a pending
  target. The model is one Go process with one sobek VM per tab; iframe
  elements have inert stub documents and are postMessage-only
  (`contentDocument`/`contentWindow`
  return one shared inert stub, no message is delivered, `frameElement` is null
  and there is no DOM bridge). Layout still reserves the iframe's replaced box:
  CSS `width`/`height` (with the legacy `width`/`height` attributes mapped
  before author CSS), `aspect-ratio`, or the 300×150 CSS px default. The box is
  painted black with a gray `iframe` label, or `i` when narrower than six
  cells, and the frame's fallback children are not laid out. The document and
  its scripts are never loaded, so content-driven sizing (a child reporting its
  height back to the parent) does not happen.
  Cross-frame access is origin-checked. This is logical origin isolation only,
  not memory, site or OS isolation.
- The console request pane is process-wide, not per tab: any tab starting a
  document load begins a new request generation and clears the shared list.
  Each tab keeps its own JavaScript log, history, scroll, focus and overlays,
  but request tagging by tab is not implemented.
- Cross-origin scripts/stylesheets/images still load like a normal browser and
  share the cookie jar (third-party requests), with no CSP. This is a separate
  residual risk from XHR; do not treat MOB³ as a security boundary.
- **Address-bar integrity:** the host records each document's final URL and
  the front end reads it by entry ID, so an engine bug cannot forge a new
  history entry's URL through the protocol. This is not process isolation: a
  fully compromised engine shares the process and could read or corrupt
  trusted memory. The recorded target keeps the engine in-process, so this
  remains a value boundary, not memory, site or OS isolation.
- Mixed content is handled for `https` documents: HTTP image requests
  (including CSS background images and `srcset`/`<picture>` candidates) are
  upgraded to HTTPS without an HTTP fallback, while HTTP scripts, stylesheets,
  modules, `@import`, XHR/fetch and secure-subresource redirects to HTTP are
  blocked. IP-literal image hosts are blocked instead of upgraded. Audio/video
  and web fonts are not fetched, form submissions are not mixed-content checked,
  there is no per-site override, and top-level navigation to HTTP stays allowed. The local-network guard blocks public pages
  from reaching loopback, private, link-local, CGNAT and reserved addresses;
  literal local hosts and local names are rejected immediately and other names
  are checked after DNS resolution at connect time. Documents that loaded
  locally keep access, and top-level navigation is not blocked. This is
  best-effort, not a sandbox; with an HTTP proxy the target is resolved by the
  proxy, so only literal/local names are caught.
- Browser checks and anti-bot/firewall blocks cannot be completed. On
  401/403/429/503 responses, MOB³ looks for Cloudflare, CAPTCHA,
  DataDome/PerimeterX, Imperva/Sucuri, “verify you are human”, “unusual
  traffic” and access-denied signatures and shows a generic “Site blocked”
  page with the detected reason, HTTP status and URL. Challenge scripts are
  not executed unless the user follows the page’s one-shot **Try anyway** link
  or passes `-ignore-blocks`; even then most checks fail, so sites behind such
  protection are generally not usable. MOB³ is not a security boundary and
  challenge pages may run arbitrary script once explicitly retried.
- No automatic downloads. A link with a `download` attribute or a
  non-HTML response opens a popup with a copyable `curl` command instead of
  saving the file (PDFs open in the text viewer unless `-no-pdf`). Forms
  support editable text fields, checkbox/radio
  toggling, single-select popups, multi-select list boxes and urlencoded
  GET/POST submission;
  multipart/file uploads are not implemented (clicking a file input shows an
  unsupported notice). Mouse support covers
  click-to-follow, page drag selection, the URL prompt, right-click link-URL
  copy and wheel scrolling. `y` copies selected page text through OSC 52;
  terminal-native Shift+drag handles URL selection. Clipboard use is
  write-only, user-initiated, capped at 100 KiB and dependent on terminal OSC
  52 support; `navigator.clipboard` and page-triggered copies remain
  unsupported.
- TLS trust is selected by `-certs` but never inspected: there is no revocation
  checking (CRL/OCSP), certificate UI, pinning, or warning flow beyond the
  connection error a request returns. The embedded bundle carries only the
  roots current when the binary was built.
- Runtime CSS-in-JS stylesheets (`CSSStyleSheet.insertRule`,
  `replaceSync` and `document.adoptedStyleSheets`) are inert. Components
  styled only at runtime by emotion or styled-components render unstyled, and
  their visually-hidden focus controls can appear as visible text.
- Graphics-protocol detection is environment-based, not an active terminal
  query; users may need `-graphics` in unusual setups.
- No font files; no real text scaling outside kitty’s `OSC 66`.
- Page zoom deliberately leaves text at its normal cell size, so fluid
  percentage layouts barely change in cells while fixed-pixel content grows.
  There is no horizontal scrolling, so zoomed-in fixed-width content can clip
  on the right instead of scrolling sideways, and bitmaps decoded before a
  zoom-out can be upscaled. Zoom starts at `-zoom N`, is editable in
  `about:config`, and is never persisted.
- PDF support is text only. Pages are extracted, never rasterized: figures,
  images, chart art, shadings, annotations and link targets do not appear,
  scanned documents show no text, and encrypted (password-protected) files are
  refused with a note. Text keeps column alignment but loses font sizes and
  colours; bold/italic come from the font name and flags. Symbol and ligature
  glyphs are only recovered when the font carries a `/ToUnicode` map, so some
  PDFs show `fi`/`fl` ligatures and math symbols as spaces. Rotated pages are
  read in their unrotated text order.
- Flexbox supports basic rows and column stacking (blockified children with a
  vertical gap), `flex-wrap`, `order`, row `justify-content` and
  basic row `align-items`; column `justify-content`/`align-items` are not
  applied. Grid supports
  basic row-major tracks, named-area column placement, explicit numeric
  `grid-row` starts and horizontal `justify-items`/`justify-self`, but not
  `grid-template-rows`, named row lines or row spans; vertical alignment stays
  top-aligned and `grid-auto-flow`/`order` are approximated. CSS multi-column
  is limited to block children: `columns`/`column-count`/`column-width` are
  distributed into columns, but inline content, column spans, column rules and
  inline fragmentation are not modelled. Left/right
  floats shrink to fit, wrap following lines and honour
  `clear`; floats are scoped to their block flow, lines that cross
  a float's bottom resume the full width, and floats inside inline wrappers
  remain inline. `position:sticky` follows the scroll for a `top` offset;
  `bottom`/`left`/`right` offsets and nested sticky boxes keep their block-flow
  position. `display:contents` is
  expanded so its children participate directly in the parent's flow, flex row
  or grid. Absolute, relative and
  fixed positioning work; `z-index` sorts positioned boxes by layer at paint
  and hit-test time, but nested stacking contexts, negative layers behind an
  ancestor's background, and contexts created by `opacity`, `transform` or
  flex/grid items are approximated. Relative offsets are not applied to inline
  boxes.
- SVG support is partial: paths (including compact arc flags and arcs with
  several parameter sets), shapes, gradients, transforms, `clipPath`,
  `fill-rule` and `<use>` of paths/groups work; SVG text, `<use>` resolving to
  `<symbol>` (the common sprite pattern), filters, masks, CSS-in-SVG and
  animation are not rendered. Transform-scaled strokes and bounded dash strokes
  are handled. Inline SVG falls back to an accessible label if rasterization
  fails. Static WebP is supported; animated WebP is not.
- Reader mode is heuristic: unusual layouts can lose content or retain
  clutter, it renders a snapshot rather than live JS updates, and images,
  forms and other interactive controls are removed.
- `calc()` supports `+`, `-`, `*`, `/`, percentages, px/em/rem/pt/ch/lh,
  viewport units and parentheses; `var()` and `env()` inside a `calc()`
  expression are unsupported. `min()`, `max()` and `clamp()` accept plain
  lengths, percentages, viewport units, `calc()` arguments and nested length
  functions. Viewport units work as direct lengths and resolve against the
  layout viewport (a 24-row default when no viewport is known). Literal
  `::before`/`::after` strings are generated as ordinary text,
  including inline, block and flex/grid pseudo-elements; empty boxes (the
  `padding-top` aspect-ratio pattern) still work. Private-use icon-font
  codepoints are dropped because terminals draw them as missing glyphs, and
  `attr()`, `counter()`, `url()` and the quote keywords generate nothing.
  `-webkit-line-clamp` is approximated by clipping after N lines.
  CSS nesting resolves nested style rules against their parent selector
  (`&`, descendant and combinator children); nested at-rules are skipped.
  Selector identifiers support backslash escapes, so utility classes such as
  `.desktop\:grid-col-3` and `.\32 col` match their `desktop:grid-col-3` and
  `2col` class names.
  CSS custom properties support simple `var()` substitution, including
  fallbacks; registered properties and Houdini APIs are not supported. Inline
  `<style>` rules injected by scripts are collected after load, but dynamically
  inserted `<link rel=stylesheet>` elements are not fetched, and a style
  element that is removed or edited keeps its previously parsed rules. The
  inherited property map is shared down the tree and copied only when an
  element declares a custom property itself, so a large `:root` token set
  costs one map per declaring element instead of one full copy per node
  (measured: 349 MB → 44 MB live heap on a token-heavy page). No
  animations, transitions, gradients or repeating background tiles. `<dialog>` open
  state controls rendering, but modal
  dialogs are laid out in normal block flow without a backdrop, focus trap or
  top-layer stacking.
- Modern CSS-heavy sites may look rough; content-focused pages, docs, and old
  HTML render best.
- Interactive mode needs a real Linux or macOS terminal. Windows is not a build
  target; use WSL.
