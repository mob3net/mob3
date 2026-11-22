# Architecture

Part of the MOB³ agent guidance. Start at [AGENTS.md](../../AGENTS.md) for the working
agreement, the non-negotiable constraints and the change protocol.

## Target boundary

The single `browser` package is being split so page content (DOM, JS, layout,
decoders) cannot reach the terminal, the network policy or the trusted UI. The
wire seam is canonical in `internal/protocol`; this section records ownership
and the migration order. Progress, decisions and measurements are tracked in
[boundary-migration.md](boundary-migration.md).

| Package | Owns |
| --- | --- |
| `internal/protocol` | Message types, binary codec, `Session`/`Broker`/`Transport` ports, the in-process transport with validated frame handoff, limits and version. Standard library only; never imports another project package. |
| `internal/engine` | HTML/CSS parsing and style, layout, paint into a content-rect frame, sobek and the DOM, PDF text extraction, image and SVG decoding, find, selection, hints, reader, dark palette. |
| `internal/host` | HTTP/S, TLS and certificates, cookie jar, resource cache, runtime settings, global visited history, download detection, and the request log. |
| `internal/frontend` | Terminal raw mode and key/mouse decoding, chrome and dialogs, ANSI encoding, kitty/iTerm2 placement, PNG output, clipboard. |
| `internal/app` | Wiring: constructs one host, engine session and frontend, connects them over a transport, and runs the event loop. |

Allowed imports:

```
app      -> engine, host, frontend, protocol
frontend -> protocol
engine   -> protocol
host     -> protocol
protocol -> standard library only
```

The split is a value boundary. Nothing in `protocol` holds a pointer to engine
state: frames, events and commands are plain data, and a codec round-trip
covers every message. Images cross as encoded bytes with size caps, never as
`image.Image`. Terminal capability detection lives on the front end and cookie
parsing on the host side.

**Non-goals of this step:** no subprocess, no OS sandbox, no tabs, no
per-site renderer, no behavior or visual change. A later process split is a
transport implementation, not another architecture change.

### Migration order

Each batch lands with tests green; type aliases are temporary and removed at
the end.

1. `internal/protocol`, codec, dependency test and the output/benchmark
   baseline.
2. Frame boundary: content-rect `Frame`, structured scaled text instead of
   raw escape cells, ANSI and native-image encoding in the front end.
3. Chrome and shell state owned by the front end; engine state arrives as
   events; the engine learns only the content rectangle.
4. Move leaf subsystems, then the session type, into `internal/engine`.
5. Split `Loader.Load` into host `Fetch` (status, headers, final URL, raw
   bytes) and engine decompression/charset/parse; move automation and MCP to a
   protocol client.
6. Run every path, including `-dump`, `-png` and tests, through the in-process
   transport; keep a socket transport for the later process split.
7. Fuzz the codec, benchmark against the baseline, remove aliases.

### Exit gates

- `-dump` and `-png` are byte-identical to the baseline over the synthetic
  page corpus.
- `frontend` and `host` do not import `engine`; `engine` imports only
  `protocol`, approved modules and the standard library, with no `net`,
  `syscall` or `os` left. Current exceptions to remove: terminal capability
  detection in `render.go` (`os.Getenv`) and `document.cookie` parsing in
  `js_dom.go` (`net/http`).
- Frame goldens cover layout and rendering, plus new frame-snapshot tests for
  the seam.
- Dependencies are checked by a test, not by review.
- Startup, RSS and frame benchmarks stay inside the recorded budget and idle
  produces no frames.

## Current layout (during migration)

Standard Go layout:

- `cmd/mob3/main.go` — thin entry point; calls `browser.Run(os.Args[1:])` and
  exits with its status.
- `internal/browser/` — package `browser` with the engine, CLI loop and tests.
  No `internal` package is imported from outside this module. Keep the folder
  consolidated by subsystem rather than one file per small feature; platform
  code stays in its `term_*`/`signals_*` files.
- **One package, files by subsystem.** Every file under `internal/browser/`
  belongs to package `browser`; `Browser`'s methods share one struct, so
  splitting into subpackages would force exported state or accessor layers for
  no benefit. Large subsystems therefore get their own file (see the table
  below), and moving code between those files is a pure declaration move with
  no behavior change: keep whole declaration blocks with their doc comments,
  give each new file only the imports it actually uses, and let the compiler
  and tests confirm nothing changed. Naming follows ownership: files carved
  out of a base file share that file's prefix, so `layout.go` produces
  `layout_grid.go`, `layout_flex.go` and `layout_table.go`, and `browser.go`
  produces `browser_chrome.go`, `browser_forms.go` and `browser_history.go`.
  Prefixing matters once a bare name would be ambiguous: `forms` and `images`
  exist in both families, so the prefix says which one a file belongs to.
  Standalone subsystems that are their own base file keep their own name
  (`ui.go`, `config.go`, `find.go`, `reader.go`, `hints.go`).

| File | Decision / responsibility |
| --- | --- |
| `cmd/mob3/main.go` | Executable entry point only. |
| `internal/engine/html.go` | Hand-written tolerant HTML tokenizer + tree builder. No `x/net/html`. Handles comments, doctype, raw text elements, entities, void elements, implicit closes scoped to the current container so nested tables/lists survive. Honors XML-style self-closing tags (needed by legacy markup such as `<tr/>`). |
| `internal/engine/style.go` | The `Style` struct and computed styles: defaults, inheritance, text scale and line metrics, `applyDecl`/`applyDecls` (including the `font` shorthand, which keeps numeric weights (`400`) separate from the size (`12px/18px`)), border shorthands, presentation attributes, `var()` substitution, `background-image`/`background-size`/`background-position`, `object-position`/`aspect-ratio`, `box-sizing` (including `inherit`, so the common `html{box-sizing:border-box} *{box-sizing:inherit}` reset reaches every element), min/max sizing, logical `margin`/`padding` aliases for horizontal-tb, approximate `-webkit-line-clamp`, and screen-reader-only clip detection. |
| `internal/engine/units.go` | `Length`/`Unit`, `px`/`%`/`auto`, horizontal/vertical resolution (including `lh`, resolved to px after the cascade, and `rem`), viewport units (`vw`/`vh`/`vmin`/`vmax`, including inside `calc()`), `calc()` arithmetic, and `min()`/`max()`/`clamp()` with percentages, viewport units, `calc()` arguments and nesting, plus font-size keywords. Non-finite values (`NaN`, `Infinity`, division by zero) are rejected. The engine owns the cell-size/zoom globals. |
| `internal/engine/color.go` | `Color`, named/hex/rgb/hsl parsing, HSL conversion, alpha and opacity. |
| `internal/engine/cssutil.go` | `SplitFields`/`SplitTopLevel` and the string scanner shared by color and sheet parsing. |
| `engine_compat.go` | Browser aliases and wrappers for the engine DOM/style API, constructors, cascade accessors and layer/import helpers. Temporary until layout and paint move. |
| `style_app.go` | App-level style helpers that stay in the browser: zoom percentage parsing/formatting and terminal-to-CSS pixel conversion. |
| `style_units_compat.go` | Browser aliases and wrappers for the engine style primitives; temporary until the remaining style files move. |
| `internal/engine/style_resolve.go` | `StyleEngine.resolveStyleUnits`: rem, viewport, lh and calc resolution against the final computed style. |
| `internal/engine/style_selectors.go` | Selector parsing and matching: type/universal/class/id/attribute/descendant/child/sibling compounds, `:not`, `:hover`, structural pseudos, grouped selectors, specificity and hover detection. A pseudo-class the engine cannot evaluate invalidates its selector instead of evaluating to false, because inside `:not()` a false result would invert into a match for every element. |
| `internal/engine/style_sheet.go` | Declaration and stylesheet parsing (comments are skipped inside blocks as well as between them, so a quote or brace in a comment cannot truncate the rest of the sheet, and a backslash escape is skipped while scanning selectors, so an escaped quote in a generated utility class such as `.before\:content-\[\'\'\]` cannot start a phantom string and drop every later rule): tolerant statement scanning, `@import` resolution hooks, `@media`/`@supports` wrapping, CSS cascade layers (`@layer` blocks and `@layer a, b;` statements ranked in declaration order across the document, unlayered rules winning over every layer, `!important` reversing the order, nested layers using dotted names), CSS nesting (`&` replaced by the parent, descendant and combinator children, nested at-rules skipped, depth capped at 8), the parse budgets (4 MiB, 50k rules, 256 KiB per block, 1 s) and the scanner helpers. |
| `internal/engine/style_engine.go` | Style engine: UA/author rule collection, rightmost-compound indexing so large stylesheets do not scan every rule for every element, cascade application, `::before`/`::after` computation (empty boxes and literal text, with private-use icon glyphs and functional values rejected), and the style deadline. |
| `internal/engine/style_media.go` | `@media` evaluation against the layout viewport: `screen`/`all`, min/max-width and range syntax, and `prefers-color-scheme` following the dark-mode state. |
| `internal/engine/style_ua.go` | The UA stylesheet written in the same CSS subset, and its compiled rules. |
| `layout_compat.go` | Browser aliases and wrappers for the engine layout types, builders, utilities and form predicates. Temporary until paint and the frame move. |
| `internal/engine/layout.go` | Layout entry points, the `-layout-budget` deadline/fragment/row limits, `Frag`/`LinkInfo`/`Layout` types, the fragment builders, the box model, block flow and relative offsets. |
| `internal/engine/layout_absolute.go` | Out-of-flow absolute/fixed boxes: deferral, containing-block resolution and placement. |
| `internal/engine/layout_flow.go` | Flow scaffolding: children, floats and `clear`, fragment trimming and overflow clipping, list markers, generated `::before`/`::after` boxes with their literal text, and inline pseudo content. |
| `internal/engine/layout_inline.go` | Inline formatting: line boxes, item collection, inline edges/margins, text and word emission, atomic inline boxes, scaled-text overlap handling. |
| `internal/engine/layout_forms.go` | Form widget layout: labels, inputs, textareas, selects, ranges, checkbox glyphs and control predicates. |
| `internal/engine/layout_images.go` | Image layout: intrinsic sizing, display caps, inline percentage widths resolved against sized inline ancestors, fitted/`object-fit` placement, alt labels, block-level inline SVG (image or accessibility fallback), and CSS background painting (block boxes only, cover crop, never native placements so text stays above). |
| `internal/engine/layout_table.go` | Table layout: column sizing, spans, collapsed borders and cell alignment. |
| `internal/engine/layout_grid.go` | CSS grid: template/track parsing (including track values joined by `var()` substitution such as `389pxminmax(0,1fr)`, and `max()`/`min()` rail sizes), numeric row/column placement from `grid-column`/`grid-row` and the `grid-area` shorthand, named areas, horizontal `justify-items`/`justify-self` (including an implicit single-column grid) and cell layout. `auto` tracks are sized to their items' max-content width, shrinking toward min-content before the `fr` tracks take the remainder. |
| `internal/engine/layout_flex.go` | Flex rows: basis, growing, shrinking honouring `flex-shrink` (an item with `flex-shrink:0` keeps its size and overflows instead of collapsing), wrapping and line placement. Column flex stacks blockified children with a vertical gap, so a `<picture>` media box gets its `aspect-ratio` height instead of staying an empty inline. |
| `internal/engine/layout_measure.go` | Intrinsic measurement: `measure`, max/min-content widths and inline text measurement. A percentage-width image is measured at its intrinsic width (the percentage is container-relative) with a one-cell minimum, so it cannot force a flex or grid item wider than its slot. |
| `internal/engine/layout_links.go` | Link rectangles, overlay anchors, and fixed/sticky scroll re-anchoring (sticky groups clamp against their containing block). |
| `internal/engine/layout_util.go` | Width/rune helpers, text transforms, tab expansion, visible-text extraction and page-background fallback. |
| `internal/engine/render.go` | Terminal cell framebuffer, clipping, image downsampling, half-block rendering, z-index paint ordering, image-placement collection, `PaintLayout*`, SVG decoding via the vendored renderer. |
| `render_gfx.go` | Browser-side native image layer: kitty/iTerm2 placement protocols, placement cache, PNG encoding, image downscaling and inline-SVG loading. Moves to the frontend in Phase 5. |
| `paint_compat.go` | Browser aliases and wrappers for the engine grid/paint API and SVG decoders. Temporary. |
| `frontend_bridge.go` | Temporary bridges to `internal/frontend`: grid-to-frame conversion for `Lines`/`LinesMono`/`PNG`, color modes and code helpers. Removed when the engine package makes the seam direct. |
| `internal/frontend/frame.go` | Protocol-only ANSI encoder: color modes, SGR, 256-color mapping, structured scaled runs, mono attributes. |
| `internal/frontend/png.go` | Protocol-frame-to-PNG renderer for `-png` and the MCP `screenshot` tool. Uses Go Mono faces from the approved `golang.org/x/image` module, 8×16 pixel cells, per-cell colors/attributes, half-block image cells, and underline/strike. |
| `frame_protocol.go` | Maps the painted content grid to `protocol.Frame` for the engine/frontend seam. The only place internal cell fields become wire values. |
| `term_detect.go` | Terminal capability detection from the environment (`COLORTERM`, `TERM`, `TERM_PROGRAM`, multiplexer variables). Frontend-owned; the paint path receives capabilities instead of reading the environment. |
| `internal/host/cache.go` | Session LRU resource cache for raw bodies and decoded images. |
| `internal/host/requests.go` | In-memory console request log, page generation, start/finish accounting and snapshots. |
| `internal/host/settings.go` | User agent, search template and decoded-image caps, shared by navigation and image workers. |
| `internal/host/certs.go` | `system`/`embedded` trust-store selection, the embedded Mozilla CA pool and the stable `CertTransport` wrapper. |
| `internal/host/network.go` | Request metadata, local-network and mixed-content policy, dialers, pooled transport and HTTP client construction. |
| `internal/host/fetch.go` | `Fetcher`: raw HTTP/data requests, size and encoding limits, cache use and request accounting. Page parsing/assembly stays in the browser for now. |
| `internal/engine/page_loader.go` | `PageFetcher` contract and HTML page assembly: parse, base/title, stylesheets, scripts and import map. Image/SVG loading and about/PDF orchestration stay in the Loader. |
| `internal/protocol/transport.go` | `Handler`/`Transport` contract and the in-process session: handler goroutine, out-of-band abort, validated frame handoff, bounded coalescing outbox. |
| `internal/protocol/stream.go` | Stream transport: framed reads, `ServeStream` handler side with direct abort, and `DialStream` front-end session. Used by the later child process. |
| `internal/frontend/present.go` | `Presenter`: ANSI frame encoding, row diff, scroll/native resets and terminal writes. |
| `internal/protocol` `Chrome` message | Header/footer cell layer for content-only frames; overlay and console frames use the full-screen fallback. |
| `internal/frontend/gfx.go` | Graphics modes and capability detection, PNG decode/scale/crop and kitty/iTerm2 placement output. |
| `internal/browser/placement.go` | `ImagePlacement` to `protocol.Placement`, bounded encoded-image cache and page image scaling. |
| `internal/browser/dump_transport.go` | One-shot `-dump`/`-png` handler: load, layout, paint, split tall text frames and emit them through `protocol.InProc`. |
| `internal/browser/automation_transport.go` | MCP protocol client and engine-side `automationHandler`; the automation library remains the handler implementation. |
| `internal/browser/session.go` | Interactive `Serve` loop: frame stashing with placements, command routing, load/image/import/timer/progress/usage events and engine-side dialog input while the transport owns the front end. |
| `internal/browser/cli.go` | Front-end terminal loop for interactive runs: raw mode, key reading, resize and quit signals, command sends and `Quit` handling. |
| `internal/engine/page.go` | `Page` and `Script`: the loaded document model, author rules, import map, PDF/download state and post-load inline-style collection. |
| `fetch.go` | HTTP(S)/data loading, redirects, gzip, charset decoding, stylesheet discovery, module script collection and import map parsing, image loading, request log and the local-network/mixed-content guards. The cookie jar moved to `internal/host/cookies.go`. |
| `internal/host/cookies.go` | In-memory cookie jar with public-suffix checking, HttpOnly protection for `document.cookie`, and the storage-inspector view. |
| `internal/host/cache.go` | Session-only LRU resource cache shared by raw CSS/JS bodies and decoded images, with a 3% derived budget (32 MiB fallback), 2 MiB per-entry cap, and immediate limit changes from config. |
| `pages.go` | The shell and shared stylesheet (`appPageCSS`) for MOB³'s own pages: one centered 900px reading column, a 32px `h1`, and table, muted, warning and `pre` styles, with page-specific CSS appended by `appPageHTML`. Help, config, history, the blocked page and the cannot-display and load-error pages use it; the start page keeps the reader column and PDF pages keep their content-sized boxes. |
| `internal/host/certs.go` | TLS trust store for `-certs`: `system` (the platform store, the default) or `embedded` (the official Mozilla NSS root bundle from `golang.org/x/crypto/x509roots/fallback`, parsed once with its distrust-after constraints attached through `AddCertWithConstraint`). Certificate files are not read. The module's `fallback` package is also imported, which registers the same roots with `x509.SetFallbackRoots`: the default source still verifies HTTPS on a machine with no platform roots, while a present but stale store is left alone (that is what `-certs=embedded` replaces). The selected source replaces the platform store rather than extending it. A stable `certTransport` wrapper swaps the underlying `http.Transport` (and closes its idle connections) when the source changes mid-session, so a change never races an in-flight request or keeps a pooled connection on the old roots. Refresh the bundle by bumping the module and re-vendoring. |
| `internal/engine/pdf_object.go` | PDF object lexer, classic/xref-stream/object-stream cross-reference loading, damaged-xref recovery, and the stream filters (Flate, LZW, ASCIIHex, ASCII85, RunLength, TIFF/PNG predictors) with parse budgets. |
| `internal/engine/pdf_text.go` | PDF page tree, content-stream interpreter for text operators and form XObjects, font widths and encodings (WinAnsi/MacRoman, `/Differences`, `/ToUnicode` CMaps), inline-image skipping, and line/column reconstruction into styled spans. |
| `internal/engine/pdf_document.go` | PDF document model on `Page.PDF`, per-page HTML assembly with page rules and `Page n of N` labels, title/metadata, note messages, and panic containment. |
| `config.go` | Runtime config model, `about:config` form page, and applying edited values without persistence. |
| `browser.go` | Core view state: the `Browser` struct, navigation and load lifecycle (progress, cancel, `finishLoad`), page rebuild and mode toggles, scrolling and link focus, hover chain, link following, notices/downloads, anchor scrolling, `EscapeHTML`. |
| `browser_chrome.go` | Frame composition: `draw` paints the content rect and blits it, `drawChrome` composes toolbar, footer, dialogs and menus, and `flushGrid` only encodes/writes. Also status classification, footer mode labels and graphics-mode cycling, selection/cursor/control-focus overlays, scrollbar rendering, and the resident-memory readout. |
| `shell.go` | Shell-owned geometry (`contentRect`, `headerRows`, `viewport`) and the `protocol.State` snapshot used by the chrome. |
| `browser_forms.go` | Form controls: Tab order and focus, control activation, radio/select/range stepping, input value editing and caret handling, constraint validation, label resolution, and GET/POST submission. |
| `browser_input.go` | Key and mouse input: `HandleKey`/`HandleMouse`, click-to-node and click-to-link hit testing, URL/command prompt editing, mouse escape decoding and `MouseEvent`. The shared line-editing core (`editLineKey`) serves the prompt, find bar, form fields, console input and JS dialogs, so caret motion, word motion, deletion, selection replacement and the length cap behave the same everywhere; Enter, Escape, Tab, Up/Down, history and textarea line movement stay with each caller. |
| `browser_history.go` | Session history and the memory-aware back/forward page cache (per-page weight, budget and per-page cap, suspend/cache/restore/trim, scavenge request after a heavy eviction), back/forward/reload, and meta-refresh targets. |
| `memory.go` | Process soft heap limit (`-mem-limit`, `debug.SetMemoryLimit`), the percentage-derived JS/image/SVG budgets, the weighted decoded-image byte limiter, and the one-shot scavenge request consumed between frames. |
| `pdf_compat.go` | Browser aliases and wrappers for the engine PDF parser/document model. |
| `browser_pdf.go` | PDF page tracking for the footer status line (`Page n/N`), the clickable `(d -> Download name)` hint, and the `d` download shortcut. |
| `browser_images.go` | Deferred image batching: viewport-first task sorting, worker batch start/apply, and refresh after script-driven rebuilds. |
| `browser_selection.go` | Drag selection: anchor/cursor ordering, text extraction, OSC 52 copying, and selection painting. |
| `automation.go` | Headless `Automation` session for scripted clients: serialized navigation, text snapshot, click by link/selector/coordinates, typing, key presses, scrolling, history, JS evaluation and session close. `RunCommand` is the shared named-command dispatcher used by protocol front ends. Uses a virtual text viewport and sets `Browser.noDraw` so nothing is written to the terminal. |
| `mcp.go` | Newline-delimited JSON-RPC 2.0 MCP server over stdin/stdout. Implements `initialize`, `ping`, `tools/list` and `tools/call`; tool calls delegate to `Automation.RunCommand`. Stdout is protocol-only, diagnostics go to stderr, and tool failures use `isError` content. |
| `ui.go` | JS console view, alert/confirm/prompt dialogs, the `X` page view options window (fixed elements and background images) and their rendering. |
| `find.go` | Incremental page/HTML find bar, match computation, scrolling and match highlights. |
| `hints.go` | Vimium-style link hints chosen from each visible link's own text: letters with a yellow background and underline (the first unused letter, or two adjacent letters when hints share a prefix), case-insensitive prefix filtering, following the chosen link, and the F destination-confirmation step. |
| `reader.go` | Reader-mode article extraction: candidate scoring, junk filtering, filtered tree cloning and the narrow reader stylesheet. |
| `internal/engine/dark.go` | Dark-mode post-layout palette remap: light backgrounds become dark, dark text becomes light, hues are preserved for links and accents. |
| `internal/engine/js.go` | sobek runtime, script execution, module script dispatch, internal event dispatch, timers and DOM mutation hooks. |
| `internal/engine/js_host.go` | `JSHost` contract and `ImportTask`:
| `js_compat.go` | Browser aliases and constructor for the engine runtime. |
| `js_life.go` | Browser JS lifecycle: `startJS`, stop, timer loop and dynamic-import completion. |
| `js_host.go` | `browserJSHost`: the browser implementation of the engine contract. | viewport, layout, scroll, focus, hit testing, dialogs, status, form/upload, cookie/fetch brokering and the import queue. The JS runtime holds this interface, not `*Browser`. |
| `internal/engine/js_dom.go` | DOM wrappers: document/element/text objects, selectors, attributes plus a live NamedNodeMap-style `element.attributes`, reflected HTMLElement properties (`hidden`, `title`, `lang`, `dir`, `tabIndex`, `contentEditable`, `draggable`, `inert`, …), `innerHTML`, style and class lists, canvas placeholders. Measurement resolves from the laid-out fragments, and a script-created element gets its style computed on demand so `getComputedStyle`/`offsetWidth` see its rules before the next rebuild. The property and method API is defined once on the element/text prototypes and resolves its node from the receiver, so a wrapper costs only its node fields; the `dataset` proxy is built lazily and cached per element. The document wrapper also carries the Node mutation methods and element-child views (`appendChild`, `insertBefore`, `removeChild`, `replaceChild`, `contains`, `hasChildNodes`, `firstChild`/`lastChild`, `firstElementChild`/`lastElementChild`, `children`), because frameworks that hydrate with `document` as the root container call them on the document itself. `<template>.content` is a real inert fragment, and a node whose tag has a registered custom element class is constructed with that class when first wrapped. |
| `internal/engine/js_events.go` | Event constructors (`Event`, `CustomEvent`, `MouseEvent`, `KeyboardEvent` and friends), `document.createEvent`, `dispatchEvent`, `MessageChannel` and `MutationObserver` stubs; HTML/SVG element constructor globals and prototype chains for `instanceof` checks. The DOM API is installed on `Element.prototype`, so SVG elements (which extend `Element`, not `HTMLElement`) inherit `appendChild`, `setAttribute` and the rest. |
| `internal/engine/js_compat.go` | Cheap web-compatibility shims: `dataset`, `outerHTML`, `insertAdjacent*`, attribute helpers, `compareDocumentPosition`, `normalize`, focus/blur/scrollIntoView/getClientRects, Range/Selection, TreeWalker/NodeIterator, TextEncoder/TextDecoder, crypto, structuredClone, AbortController, Blob/File/FileReader, FormData, URL object URLs and queueMicrotask. |
| `internal/engine/js_stubs.go` | Platform interface stubs: `EventTarget`, `NodeList`, `HTMLCollection`, `DOMStringMap`, `CSS`, `ShadowRoot`, `customElements`, `IntersectionObserver`, `ResizeObserver`, `PerformanceObserver`, `Path2D`, `ReadableStream`, `Worker`, Web Animations no-ops, `DOMParser`/`XMLSerializer`, `DataTransfer`, extra event subclasses, interface constructors (`DOMTokenList`, `CSSStyleDeclaration`, `Storage`, `MediaQueryList`, `TreeWalker`, …), `Element.prototype.matches`/`closest` /vendor-alias delegates, and isolated inert iframe `contentDocument`/`contentWindow`. `localStorage`/`sessionStorage` use the `Storage` prototype so `instanceof` and prototype patches work. Constructable `CSSStyleSheet` accepts `insertRule`/`deleteRule`/`replaceSync`, and `PerformanceObserver.supportedEntryTypes` is an empty list so feature detection that reads it does not throw. |
| `internal/engine/js_modules.go` | ES modules: the module graph cache, specifier resolution against the importing module's URL, size/depth caps, `import.meta.url`, dynamic import finishing, and linking/evaluation under the script guard. After the page's own scripts finish, an interactive dynamic import fetches and parses its graph on a background goroutine and the event loop links and evaluates it, so a slow chunk cannot freeze drawing, input or image loading; the graph shares the page's record cache, so a dependency imported by several chunks is fetched and evaluated once. Page-load and headless imports stay synchronous. |
| `internal/engine/js_web.go` | `URL`/`URLSearchParams`, promise-based `fetch` with `Headers`/`Request`/`Response`, and `XMLHttpRequest`; requests are same-origin or simple CORS cross-origin responses. |
| `internal/engine/js_intl.go` | English-only `Intl` formatter subset. |
| `term_unix.go` | Shared Linux/macOS key and escape-sequence decoding: legacy sequences, `alt-<key>` ESC prefixes, modified arrows/keys (`ctrl-left`, `alt-delete`, …), and kitty keyboard protocol keys with press/repeat/release filtering for both `CSI u` and legacy-encoded functional keys (`CSI 1;mods:event A`). |
| `term_linux.go` | Linux termios raw mode, window size and cell pixels via ioctl. |
| `term_darwin.go` | macOS termios raw mode, window size and cell pixels via ioctl. |
| `term_stub.go` | Other-platform build stub; interactive unsupported, dump still works. |
| `signals_unix.go` / `signals_other.go` | Platform-specific resize/quit signal wiring so the project cross-compiles. |
| `cli.go` | `Run`: flags, dump mode, interactive event loop, and the build-time `Version` string reported by `-version`; default `dev`. |
| `*_test.go` | Unit tests alongside the code; no network dependencies. Test files mirror the source files with the same names and prefixes (`browser_chrome_test.go`, `browser_forms_test.go`, `browser_input_test.go`, `layout_grid_test.go`, `layout_flex_test.go`, `config_test.go`, `ui_test.go`, …), keeping shared helpers and cross-cutting cases in the base file (`browser_test.go`, `layout_test.go`). |
