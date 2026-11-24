# Architecture

Part of the MOB³ agent guidance. Start at [AGENTS.md](../../AGENTS.md) for the working
agreement, the non-negotiable constraints and the change protocol.

## Target boundary

The packages are split so page content (DOM, JS, layout, decoders) cannot reach
the terminal, the network policy or the trusted UI. The wire seam is canonical
in `internal/protocol`; this section records ownership.

| Package | Owns |
| --- | --- |
| `internal/protocol` | Message types, binary codec, `Session`/`Broker`/`Transport` ports, the in-process transport with validated frame handoff, limits and version. Standard library only; never imports another project package. |
| `internal/engine` | HTML/CSS parsing and style, document loading through `protocol.Host`, layout, paint into a content-rect frame, sobek and the DOM, PDF text extraction, image and SVG decoding, find, selection, hints, reader, dark palette. |
| `internal/host` | HTTP/S, TLS and certificates, cookie jar, resource cache, runtime settings and the request log. Session back/forward history lives in the front end, the download/curl hint is presentation, and the engine keeps only a memory-only page cache. |
| `internal/frontend` | Terminal raw mode and key/mouse decoding, chrome and dialogs, session history, ANSI encoding, kitty/iTerm2 placement, PNG output, clipboard. |
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
`image.Image`. Terminal capability detection is front-end-owned and cookie
parsing is host-side; JS `fetch`/XHR go through the host broker.

**Non-goals of this step:** the package split itself added no subprocess, OS
sandbox, tabs or per-site renderer, and no behavior or visual change. The
target in [Tabs, frames and in-process isolation](#tabs-frames-and-in-process-isolation)
also keeps one process: a future process transport would sit behind the frame
router, not change the ownership model.

### Migration order

All steps are landed. The temporary aliases, test-only host factory and the
migration tracker are gone. The host records each document's final URL and the
front end reads it by entry ID. The remaining architecture work is the
in-process tab and frame model; the engine child process is not part of the
target.

1. **Landed.** `internal/protocol`, codec, dependency test and the
   output/benchmark baseline.
2. **Landed.** Frame boundary: content-rect `Frame`, structured scaled text
   instead of raw escape cells, ANSI and native-image encoding in the front
   end.
3. **Landed.** Normal chrome, overlays, dialogs, hints, the console, the find
   bar, the session history list/index/scroll, content geometry and the
   displayed URL ledger are front-end-owned. The host records the
   committed final URL and the front end reads it by entry ID; the engine's URL
   is ignored.
4. **Landed.** `internal/engine` owns one `Session` type, the loader, headless
   automation and the dump handler; `internal/browser` is gone.
5. **Landed.** Host `Fetch`, decompression/charset/parse and MCP are split;
   the document loader is engine-owned and reaches the network only through
   `protocol.Host`.
6. **Landed.** Every path, including `-dump`, `-png` and MCP, runs through the
   in-process transport; a stream transport exists for tests and a future
   process transport.
7. **Landed.** Fuzzing, final benchmarks and cleanup: no aliases, no browser
   package, no unused `Selection` event.

### Exit gates

- `-dump` and `-png` are byte-identical to the baseline over the synthetic
  page corpus.
- `frontend` and `host` do not import `engine`; `engine` imports only
  `protocol`, approved modules and the standard library, with no `net/http`,
  `syscall` or `os` left. `net/url` is a pure value package and remains.
  The dependency test rejects `net/http` and its subpackages on the engine
  side.
- Frame goldens cover layout and rendering, plus new frame-snapshot tests for
  the seam.
- Dependencies are checked by a test, not by review. `app` may import
  `engine`, `host`, `frontend` and `protocol`; each boundary package may import
  only `protocol`.
- The engine cannot emit chrome. Header/footer, prompts, menus, console, find
  bar, overlays and dialogs are composed by the front end from `State` or
  overlay messages; back/forward history and the download/curl hint live on the
  front end. The engine keeps only a page cache keyed by entry ID.
- A new history entry's URL comes only from the host via `AttestedURL(entryID)`.
  The engine supplies the title, scroll and push flag; a forged
  `HistoryCommit` URL never reaches the address bar.
- Startup, RSS and frame benchmarks stay inside the recorded budget and idle
  produces no frames.

## Tabs, frames and in-process isolation

The target keeps one Go process. It is logical origin isolation, not memory,
site or OS isolation. No subprocess, sandbox, per-site renderer or
OS-specific isolation mechanism is part of the model.

- A tab is one protocol session (`SessionID`). The front end owns the tab list,
  the active tab, per-tab history and one terminal presenter.
- A tab owns a frame tree. The top-level document and every iframe are frames.
  Each frame owns its document/`Page`, `Layout`, URL and origin, viewport,
  scroll, timers and one sobek VM.
- There is one sobek VM per frame. No `*sobek.Object`, `*Node` or other engine
  value crosses a frame or tab boundary. Cross-frame access goes through a
  frame router that copies data and checks the frame's origin.
- A frame's origin comes from the URL the host and loader committed for that
  document, never from a script-supplied value. Every cross-frame and
  origin-scoped API uses that origin.
- Iframes are postMessage-only. `contentWindow` is a WindowProxy with
  `postMessage` and the limited Window surface; `contentDocument` is null even
  for a same-origin frame, `frameElement` is null, and there is no DOM bridge.
  `parent` and `top` are WindowProxies, not references to the other frame's
  document.
- A process-wide context owns the shared origin storage, memory budgets,
  settings and globally unique history-entry IDs. `localStorage` is keyed by
  origin and shared across tabs; `sessionStorage` is keyed by tab and origin.
- One budget manager covers all runtimes, page caches and image/SVG work, so N
  tabs do not each take a per-session percentage of the heap.
- Zoom stays browser-global across tabs; the terminal cell size is
  process-wide.
- The front end presents only the active tab; background tabs do not write the
  terminal.

The frame router is a data boundary by design, so a future process transport
could move behind it without changing the ownership or security model.

Current state: one `Session` holds one page and one JS runtime, iframe elements
have inert stub documents, and there are no tabs. A shared `BrowserContext`
now allocates history-entry IDs, and the `Session` embeds a top-level `Frame`
that owns the page, layout, URL and scroll state; shared storage and budgets
remain per session. This section records the target; it is not implemented yet.

## Current layout

This is the **actual** state. The tab and frame model is the next architecture
step; it is not implemented yet and there is no engine child process.

- `cmd/mob3/main.go` — thin entry point; calls `app.Run(os.Args[1:])`.
- `internal/protocol`, `internal/engine`, `internal/host`, `internal/frontend`
  and `internal/app` exist. `app` owns flags, validation, mode dispatch and the
  interactive terminal/session boot; it constructs `host.Session` and
  `engine.NewConfiguredLoader`, then `engine.NewConfiguredSession`,
  `engine.NewAutomationWithConfig` or `engine.NewDumpHandler`.
- `internal/engine/` holds the single 383-method `Session`: page, layout, URL,
  scroll, load lifecycle and application, forms, JS lifecycle, hit testing,
  find/hint/selection/reader state, text scaling, dark mode, content input and
  drawing, console/dialog state, the document loader, headless automation and
  the one-shot dump handler. It draws only the page content rectangle; chrome,
  console, dialogs, the find bar and the hint overlay are front-end-owned. MCP
  transport lives in `internal/app`. Production code imports only
  `internal/protocol` (plus approved vendored modules).
- The front end owns terminal raw mode, capability detection, key/mouse
  decoding, resize/quit signals, ANSI/PNG output, presentation, native image
  placement, clipboard encoding, the live session loop
  (`frontend.SessionLoop`), normal header/footer composition, header/footer
  and scrollbar mouse input, base shell keys, local quit confirmation, the
  URL/vim prompt, the page find bar, the view/zoom windows, JS dialogs, the
  select popup, download/notice modals, link hints and console
  composition/input/find. It also owns the session history list, current
  index, scroll offsets, the `about:history` display snapshot and the
  content-rectangle geometry (`ContentBounds` → `Init`/`Resize`); the engine
  keys its page cache by the front-end entry ID. It also owns the displayed
  URL: committed history entries replace `State.URL` for chrome, with a
  scheme/length check; the title stays engine-supplied. The host records the final URL and the front end
  reads it by entry ID, so the engine's `HistoryCommit` URL is not trusted. While a page form field holds the key
  path, `State.KeysEngine` stays true for key routing, but `State.FrontendChrome`
  stays true and the front end composes the header, footer and scrollbar.
- The engine imports only `internal/protocol` (plus approved vendored
  modules), and the dependency test enforces that. It does not import
  `internal/frontend`, `internal/host`, `net/http` or `os`: encoders and the
  graphics renderer are injected by the app, and host state arrives through
  `protocol.Host`. `app` may import only the four boundary packages.
- The host owns HTTP transport policy, raw fetch, cache, cookies, settings,
  certificates, the request log, JS fetch/XHR and the config token. There is no
  global visited list or download history by design (D54): back/forward history
  is front-end state and the download/curl hint is presentation.
- The engine contains the DOM/JS/style/layout/paint/PDF work. Its JS
  `fetch`/XHR and cookie surface now use the host broker and the engine no
  longer imports `net/http`.
- **Files by subsystem within a package** remains the rule. Moving code
  between files is a pure declaration move (keep doc comments, give each file
  only the imports it uses). Files carved out of a base file share its prefix
  (`layout.go` → `layout_grid.go`, `layout_flex.go`; `session.go` →
  `session_chrome.go`, `session_forms.go`), which matters when a bare name
  would be ambiguous (`forms`, `images`). Standalone subsystems keep their own
  name (`loader.go`, `automation.go`, `dump.go`, `find.go`, `hints.go`).

| File | Decision / responsibility |
| --- | --- |
| `cmd/mob3/main.go` | Executable entry point only. |
| `internal/engine/html.go` | Hand-written tolerant HTML tokenizer + tree builder. No `x/net/html`. Handles comments, doctype, raw text elements, entities, void elements, implicit closes scoped to the current container so nested tables/lists survive. Honors XML-style self-closing tags (needed by legacy markup such as `<tr/>`). |
| `internal/engine/style.go` | The `Style` struct and computed styles: defaults, inheritance, text scale and line metrics, `applyDecl`/`applyDecls` (including the `font` shorthand, which keeps numeric weights (`400`) separate from the size (`12px/18px`)), border shorthands, presentation attributes, `var()` substitution, `background-image`/`background-size`/`background-position`, `object-position`/`aspect-ratio`, `box-sizing` (including `inherit`, so the common `html{box-sizing:border-box} *{box-sizing:inherit}` reset reaches every element), min/max sizing, logical `margin`/`padding` aliases for horizontal-tb, approximate `-webkit-line-clamp`, and screen-reader-only clip detection. |
| `internal/engine/units.go` | `Length`/`Unit`, `px`/`%`/`auto`, horizontal/vertical resolution (including `lh`, resolved to px after the cascade, and `rem`), viewport units (`vw`/`vh`/`vmin`/`vmax`, including inside `calc()`), `calc()` arithmetic, and `min()`/`max()`/`clamp()` with percentages, viewport units, `calc()` arguments and nesting, plus font-size keywords. Non-finite values (`NaN`, `Infinity`, division by zero) are rejected. The engine owns the cell-size/zoom globals. They are process-wide by design: MOB³ runs one session per process, and multi-session isolation is not a goal. |
| `internal/engine/color.go` | `Color`, named/hex/rgb/hsl parsing, HSL conversion, alpha and opacity. |
| `internal/engine/cssutil.go` | `SplitFields`/`SplitTopLevel` and the string scanner shared by color and sheet parsing. |
| `internal/engine/style_app.go` | App-level style helpers: zoom percentage parsing/formatting and terminal-to-CSS pixel conversion. |
| `internal/engine/style_resolve.go` | `StyleEngine.resolveStyleUnits`: rem, viewport, lh and calc resolution against the final computed style. |
| `internal/engine/style_selectors.go` | Selector parsing and matching: type/universal/class/id/attribute/descendant/child/sibling compounds, `:not`, `:hover`, structural pseudos, grouped selectors, specificity and hover detection. A pseudo-class the engine cannot evaluate invalidates its selector instead of evaluating to false, because inside `:not()` a false result would invert into a match for every element. |
| `internal/engine/style_sheet.go` | Declaration and stylesheet parsing (comments are skipped inside blocks as well as between them, so a quote or brace in a comment cannot truncate the rest of the sheet, and a backslash escape is skipped while scanning selectors, so an escaped quote in a generated utility class such as `.before\:content-\[\'\'\]` cannot start a phantom string and drop every later rule): tolerant statement scanning, `@import` resolution hooks, `@media`/`@supports` wrapping, CSS cascade layers (`@layer` blocks and `@layer a, b;` statements ranked in declaration order across the document, unlayered rules winning over every layer, `!important` reversing the order, nested layers using dotted names), CSS nesting (`&` replaced by the parent, descendant and combinator children, nested at-rules skipped, depth capped at 8), the parse budgets (4 MiB, 50k rules, 256 KiB per block, 1 s) and the scanner helpers. |
| `internal/engine/style_engine.go` | Style engine: UA/author rule collection, rightmost-compound indexing so large stylesheets do not scan every rule for every element, cascade application, `::before`/`::after` computation (empty boxes and literal text, with private-use icon glyphs and functional values rejected), and the style deadline. |
| `internal/engine/style_media.go` | `@media` evaluation against the layout viewport: `screen`/`all`, min/max-width and range syntax, and `prefers-color-scheme` following the dark-mode state. |
| `internal/engine/style_ua.go` | The UA stylesheet written in the same CSS subset, and its compiled rules. |
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
| `internal/engine/image_source.go` | Image source selection: `srcset`/density candidates, `<picture>` sources and common lazy-load data attributes. |
| `internal/engine/image_task.go` | Image task/result/batch model, the `ImageLoader` port, deferred-load scheduling, viewport-first ordering and visible-change checks. |
| `internal/engine/inline_svg.go` | Inline SVG serialization back to XML, including restoration of SVG camelCase tag and attribute names. |
| `internal/engine/placement.go` | Native-image placement cache, PNG encoding, image scaling and frame clipping into `protocol.Placement`. |
| `internal/engine/image_decode.go` | Hostile-input image decoding: panic containment, decompression-bomb checks, SVG/raster dispatch, decode weight and aspect-preserving downscale. |
| `internal/engine/graphics.go` | `Graphics` interface and protocol graphics-mode aliases; the app injects the frontend renderer. |
| `internal/engine/encoders.go` | Injected frame/PNG encoder hooks; the engine does not import `internal/frontend`. |
| `internal/frontend/frame.go` | Protocol-only ANSI encoder: color modes, SGR, 256-color mapping, structured scaled runs, mono attributes. |
| `internal/frontend/png.go` | Protocol-frame-to-PNG renderer for `-png` and the MCP `screenshot` tool. Uses Go Mono faces from the approved `golang.org/x/image` module, 8×16 pixel cells, per-cell colors/attributes, half-block image cells, and underline/strike. |
| `internal/engine/frame_protocol.go` | Mapper from the painted content grid to `protocol.Frame`; the only place internal cell fields become wire values. |
| `internal/host/requests.go` | In-memory console request log, page generation, start/finish accounting and snapshots. |
| `internal/host/settings.go` | User agent, search template and decoded-image caps, shared by navigation and image workers. |
| `internal/host/certs.go` | `system`/`embedded` trust-store selection, the embedded Mozilla CA pool and the stable `CertTransport` wrapper. |
| `internal/host/network.go` | Request metadata, local-network and mixed-content policy, dialers, pooled transport and HTTP client construction. |
| `internal/host/fetch.go` | `Fetcher`: raw HTTP/data requests, size and encoding limits, cache use and request accounting. Page parsing/assembly is engine-owned; the host returns raw bytes. |
| `internal/engine/page_loader.go` | `PageFetcher` contract and HTML page assembly: parse, base/title, stylesheets, scripts and import map. The engine loader implements the contract. |
| `internal/engine/page_assemble.go` | About/blocked/PDF page assembly, the anti-bot response heuristic and `AboutPageName`. |
| `internal/protocol/transport.go` | `Handler`/`Transport` contract and the in-process session: handler goroutine, out-of-band abort, validated frame handoff, bounded coalescing outbox. |
| `internal/protocol/stream.go` | Stream transport: framed reads, `ServeStream` handler side with direct abort, and `DialStream` front-end session. Used by tests and available for a future process transport; the target keeps one process. |
| `internal/frontend/present.go` | `Presenter`: ANSI frame encoding, row diff, scroll/native resets, state-driven chrome composition and terminal writes. |
| `internal/frontend/session.go` | Live session loop: base shell keys, history keys/back/forward/header actions, local quit, URL/vim prompt, page find, view/zoom windows, JS dialogs, select popup, download/notice modals, link hints, console view/input/find, keys/abort/resize, events, chrome state, header/footer and scrollbar mouse routing, crash dedup and clipboard writes. |
| `internal/frontend/prompt.go` | Prompt line editing, recall history, visible window and mouse caret mapping. |
| `internal/frontend/history.go` | Front-end-owned session history and displayed URL ledger: entries, current index, scroll tracking, host-attested URL lookup, validated `HistoryCommit` scroll updates, `about:history` snapshot and back/forward restore requests. |
| `internal/frontend/chrome.go` | Normal-page header/footer layout from `protocol.State`, header/footer hit regions, `ContentBounds` (the front-end-owned content rectangle) and the scrollbar painter hook. A focused page form still sends keys through the engine but does not hand it chrome. |
| `internal/frontend/menu.go` | View/zoom menu model, full-screen dimmed choice box, row/close hit regions. |
| `internal/frontend/dialog.go` | JS alert/confirm/prompt view, modal layout, prompt editing and answer routing. |
| `internal/frontend/select.go` | Page select popup model, layout and `SelectResult` answer. |
| `internal/frontend/download.go` | Download curl hint and notice modal layout/drawing. |
| `internal/frontend/hints.go` | Link-hint overlay painting and prefix matching data. |
| `internal/frontend/console.go` | Console snapshot renderer, input row, tabs, scrolling data, HTML highlight/find bar and console overlay stacking. |
| `internal/frontend/clipboard.go` | OSC 52 clipboard encoding. |
| `internal/frontend/mouse.go` | Aliases to the shared protocol mouse event, button constants and SGR/X10/token codecs. |
| `internal/protocol/mouse.go` | Shared terminal mouse event, button constants, SGR/X10 decoding and token encoding. |
| `internal/frontend/gfx.go` | Graphics renderer, capability detection, PNG decode/scale/crop and kitty/iTerm2 placement output; the mode type aliases `protocol.GraphicsMode`. |
| `internal/protocol/graphics.go` | Shared graphics mode values (`auto`, `none`, `blocks`, `kitty`, `iterm`). |
| `internal/protocol/http.go` | HTTP method constants and standard status reason phrases shared by navigation and the loader. |
| `internal/protocol/cookies.go` | Display-safe cookie rows for the storage inspector. |
| `internal/protocol/host.go` | `Host` port, host fetch result, `AttestedURL`, request record and config-token interfaces. |
| `internal/protocol/netctx.go` | Shared request metadata, page-space and local-network/mixed-content error values used by loader and host. |
| `internal/host/client.go` | Session HTTP client and swappable TLS transport wrapper; the browser reaches it through `protocol.Host`. |
| `internal/host/session.go` | `protocol.Host` implementation: fetch, policy, settings, cache, cookies, certificates, request log, config token and the final-URL attestation lookup. |
| `internal/engine/output.go` | Injected terminal writer for the direct draw path; the app installs `os.Stdout`, tests install their own hook. |
| `internal/engine/session_hooks_impl.go` | `SessionHooks` implementation on `Session`: JS host/runtime effects, timer/hover state, console/modal reset, rebuild/invalidate, status, download, history-commit, form navigation, hint activation and select-menu effects. |
| `internal/engine/context.go` | `BrowserContext`: state shared by every tab session in one process. It currently allocates the opaque history-entry ID sequence, so sessions in different tabs cannot collide in the host's URL-attestation record or the engine page cache; shared storage, budgets and settings join it with the tab and frame model. |
| `internal/engine/frame.go` | `Frame`: one browsing context. It currently holds the tab's top-level `Page`, `Layout`, `URL` and `Scroll`; child frames and the frame tree join it with the iframe work. `Session` embeds the top-level frame and remains the tab-level façade. |
| `internal/engine/session_new.go` | `SessionConfig`, `NewConfiguredSession`, `NewAutomationWithConfig` and `AutomationConfig`; the app supplies flags, the shared `BrowserContext` and terminal setup. |
| `internal/engine/version.go` | Injected build version and the help-page wrapper. |
| `internal/engine/session_certs.go` | Config-page trust-store label. |
| `internal/engine/dump.go` | One-shot `-dump`/`-png` handler: load, layout, paint, split tall text frames and emit them through `protocol.InProc`. Terminal sizing/encoding lives in the app. |
| `internal/app/automation_transport.go` | MCP protocol client and `automationHandler`; `engine.Automation` is the handler implementation. |
| `internal/engine/session_loop.go` | `Serve`/`handleCommand` wrappers, the front-end hint/modal/select result appliers and pending-frame emission. The transport loop and command dispatch live in `serve.go`/`commands.go`. |
| `internal/app/execute.go` | Mode dispatch and interactive session boot: `host.Session`/`engine.Loader` construction, session construction, terminal enter/leave, presenter/transport wiring and loop start. The build version is `internal/app.Version`. |
| `internal/app/dump.go` | One-shot transport loop for `-dump`/`-png`: terminal sizing/color, protocol session, frame encoding and diagnostics. |
| `internal/engine/page.go` | `Page` and `Script`: the loaded document model, author rules, import map, PDF/download state and post-load inline-style collection. |
| `internal/engine/loader.go`, `loader_config.go` | Document loader: `Loader` over `protocol.Host`, fetch/response classification, charset decode, about/blocked/PDF dispatch, request accounting and the app-facing config constructor. |
| `internal/engine/loader_images.go` | Image loading subsystem: task collection, request context, concurrent fetch/decode, fit, viewport ordering and the in-flight byte budget. |
| `internal/engine/loader_page.go` | `PageFetcher` implementation for page assembly, script/stylesheet collection, body decoding and JavaScript MIME checks. |
| `internal/engine/loader_pdf.go` | PDF page bridge over `AssemblePDFPage`. |
| `internal/engine/loader_svg.go` | Per-page inline-SVG rasterization with pixel/count budgets. |
| `internal/engine/loader_certs.go` | Loader trust-store selection and label. |
| `internal/engine/loader_decode.go` | Charset decoding, single-byte charset table and JavaScript MIME classification. |
| `internal/engine/budget.go` | Effective memory limit, percentage budgets, `ByteLimiter`, loader memory/cache budget application. |
| `internal/engine/version.go` | Injected build version for built-in pages. |
| `internal/host/cookies.go` | In-memory cookie jar with public-suffix checking, HttpOnly protection for `document.cookie`, and the storage-inspector view. |
| `internal/host/cache.go` | Session-only LRU resource cache shared by raw CSS/JS bodies and decoded images, with a 3% derived budget (32 MiB fallback), 2 MiB per-entry cap, and immediate limit changes from config. |
| `internal/engine/pages.go`, `pages_about.go`, `config_page.go` | Built-in page shell/CSS, start/help/history/blocked/error/config HTML and the config form token field. The session supplies history/settings data. |
| `internal/host/certs.go` | TLS trust store for `-certs`: `system` (the platform store, the default) or `embedded` (the official Mozilla NSS root bundle from `golang.org/x/crypto/x509roots/fallback`, parsed once with its distrust-after constraints attached through `AddCertWithConstraint`). Certificate files are not read. The module's `fallback` package is also imported, which registers the same roots with `x509.SetFallbackRoots`: the default source still verifies HTTPS on a machine with no platform roots, while a present but stale store is left alone (that is what `-certs=embedded` replaces). The selected source replaces the platform store rather than extending it. A stable `certTransport` wrapper swaps the underlying `http.Transport` (and closes its idle connections) when the source changes mid-session, so a change never races an in-flight request or keeps a pooled connection on the old roots. Refresh the bundle by bumping the module and re-vendoring. |
| `internal/engine/pdf_object.go` | PDF object lexer, classic/xref-stream/object-stream cross-reference loading, damaged-xref recovery, and the stream filters (Flate, LZW, ASCIIHex, ASCII85, RunLength, TIFF/PNG predictors) with parse budgets. |
| `internal/engine/pdf_text.go` | PDF page tree, content-stream interpreter for text operators and form XObjects, font widths and encodings (WinAnsi/MacRoman, `/Differences`, `/ToUnicode` CMaps), inline-image skipping, and line/column reconstruction into styled spans. |
| `internal/engine/pdf_document.go` | PDF document model on `Page.PDF`, per-page HTML assembly with page rules and `Page n of N` labels, title/metadata, note messages, and panic containment. |
| `internal/engine/session_config.go` | Runtime config model and apply path; the form HTML is in the engine and the one-shot token is `internal/host.ConfigToken`. |
| `internal/engine/session_shell_core.go`, `session.go` | The single `Session` type: content and shell state, input resolution and the config-page branch, hint/status effects around a rebuild, mode toggles, scrolling and link focus, hover chain, link following, notices/downloads, anchor scrolling and `EscapeHTML`. |
| `internal/engine/session_chrome.go` | Content-only draw path: paints the page rectangle, control/selection/find highlights and image placements, then emits the frame. Footer text/level for `State`, status levels, graphics cycling and the memory readout remain here. |
| `internal/engine/session_shell_state.go` | `protocol.State` snapshot and the engine-side view of the front-end content rectangle (`contentRect`, `headerRows`, `viewport`). The front end computes the rectangle; the legacy derivation remains for direct callers and tests. |
| `internal/engine/session_forms.go` | Form wrappers plus the control-focus highlight painter. Tab stepping, control activation, text/range editing, submission, label targets and form requests live in `forms.go` and the other form helpers. |
| `internal/engine/session_input.go` | Content input: `HandleKey`/`HandleMouse` for page movement, forms, find navigation, hint activation, selection and content clicks, plus mouse escape decoding and `MouseEvent`. |
| `internal/engine/session_history.go` | Restore/reload/commit wrappers. The page cache, limits, suspend/cache, restore application and scavenge request are engine-owned; the ordered list/index/scroll live in the front end. |
| `internal/engine/memory.go` | Process soft heap limit (`-mem-limit`, `debug.SetMemoryLimit`) and the JS/history cache shares. Image/SVG budgets and the byte limiter live in `budget.go`. |
| `internal/engine/session_pdf.go` | PDF page tracking for the footer status line (`Page n/N`), the clickable `(d -> Download name)` hint, and the `d` download shortcut. |
| `internal/engine/session_images.go` | Deferred-image bridge: task/scheduling types, batch application, pending-row layout and refresh after script-driven rebuilds. The engine owns task collection, viewport ordering, the background stream and the `ImageLoader` port. |
| `internal/engine/session_selection.go` | Drag state and clipboard policy; ordering, text extraction and painting call `Selection`. |
| `internal/engine/automation.go` | Headless `Automation`: navigation, snapshot, click/type/press/scroll, its own history list, JS evaluation and `RunCommand`. `internal/app/automation_transport.go` exposes it through the MCP protocol client. |
| `internal/app/mcp.go` | Newline-delimited JSON-RPC 2.0 MCP server over stdin/stdout; tool calls go through the `automationRunner` interface, which in production is the protocol client. |
| `internal/engine/session_console.go` | Console state, actions and `ConsoleView` snapshots; JS dialog request/response state; download/notice state. All console, dialog, download and notice rendering and input are front-end-owned. |
| `internal/engine/session_find.go` | Find state, navigation, scrolling, result messages and page highlight painting; match computation calls `FindPageMatches`/`FindHTMLMatches`. The visible find bar, query, caret, next/previous and console HTML find are front-end-owned. |
| `internal/engine/session_hints.go` | Hint-mode state, key handling, following and the F destination-confirmation step; target discovery, glyph collection and label assignment call `BuildHintTargets`, `CollectHintGlyphs` and `AssignLinkHints`. Overlay painting and input are front-end-owned (`frontend/hints.go`). |
| `internal/engine/reader.go` | Reader-mode article extraction: candidate scoring, junk filtering, filtered tree cloning and the narrow `ReaderRules` stylesheet. |
| `internal/engine/selection.go` | Selection range ordering, text extraction, summary and viewport painting. |
| `internal/engine/hints.go` | Link-hint target discovery, glyph collection, label assignment, unique-pair shortening, base-26 encoding and label painting. |
| `internal/engine/find.go` | Page and console HTML find streams, whitespace normalisation, overlapping case-insensitive spans and match/highlight rectangles. |
| `internal/engine/validate.go` | HTML form constraint validation: required, radio groups, typed date/month/week/time/datetime/color formats and pattern checks. |
| `internal/engine/form_state.go` | Focused text control, select/checkbox/radio/button control, active option, focus transitions and clear helpers. |
| `internal/engine/form_edit.go` | Form control predicates, Tab-status text, radio/select grouping, label lookup, form request encoding, input-fragment updates, range hit testing and input-line/caret helpers. |
| `internal/engine/edit_line.go` | Shared line-editor key handling: caret and word motion, deletion, selection replacement, insertion and length caps. |
| `internal/engine/tab_order.go` | Standard Tab-order target discovery: links, controls and one stop per radio group. |
| `internal/engine/form_step.go` | Radio-group, single-select and multi-select stepping, including disabled/hidden skips and selected-option updates. |
| `internal/engine/dark.go` | Dark-mode post-layout palette remap: light backgrounds become dark, dark text becomes light, hues are preserved for links and accents. |
| `internal/engine/grid_text.go` | Plain-text grid rows used by headless snapshots and copy-page output. |
| `internal/engine/load.go` | Load lifecycle: `LoadFunc`/`LoadResult`, history entry ID allocation, background request start/cancel, loading progress and the result channel. |
| `internal/engine/url_input.go` | Typed-URL resolution: absolute/relative input, scheme rejection, host:port handling, data/about URLs and search-template fallback. |
| `internal/engine/layout_rebuild.go` | Engine-side rebuild: dynamic styles, image refresh, layout pass, scaled-text fallback, PDF page starts, hover-rule detection, build timing, fixed scroll and deferred-image scheduling. |
| `internal/engine/hit.go` | Page hit testing (`LinkAt`, `NodeAt`), overlay layering, scroll stepping and href resolution. |
| `internal/engine/history_cache.go` | Back/forward cache glue: cache limit and page cap, cache/suspend the current page, entry removal, drop-all and the one-shot scavenge request. |
| `internal/engine/session_apply.go` | Load-result application and cached-page restore, driven through the `SessionHooks` trusted-shell effects (JS, console, modals, rebuild, status, download and history commit). |
| `internal/engine/forms.go` | Form editing and focus: Tab order, control activation/stepping, text/range editing, submission, label targets and form requests, with UI/JS effects supplied by `SessionHooks`. |
| `internal/engine/js_life.go` | JS runtime lifecycle: start/stop, timer deadlines, redraw throttling, hover-rebuild deadlines, pending-navigation drain and dynamic-import completion. |
| `internal/engine/commands.go` | Front-end command dispatch: navigation, history, resize, keys, mouse, scroll, app, find, select, modal, hint, console and error commands, with shell effects supplied by `CommandHooks`. |
| `internal/engine/serve.go` | Transport event loop: load/image/import/timer/progress/usage events, draw/state/frame emission and shutdown, with terminal effects supplied by `RuntimeHooks`. |
| `internal/engine/session.go` | The engine-side tab state: the embedded top-level `Frame` (`Page`, `Layout`, `URL`, `Scroll`) plus terminal/content geometry, rendering configuration, JS runtime, PDF page starts, page cache, load lifecycle, deferred-image counters, focus/hover, reader state, text scaling, dark mode, form/selection state, find state and hint state, plus scroll/page/find/hint/PDF helpers. |
| `internal/engine/page_cache.go` | Bounded memory-only back/forward page cache: cached pages, byte/page budgets, eviction callbacks, page/DOM/image weight estimates. |
| `internal/engine/js.go` | sobek runtime, script execution, module script dispatch, internal event dispatch, timers and DOM mutation hooks. |
| `internal/engine/js_host.go` | `JSHost` contract and `ImportTask`: viewport, layout, scroll, focus, hit testing, dialogs, status, form/upload, cookie/fetch brokering and the import queue. The JS runtime holds this interface, not `*Browser`. |
| `internal/engine/js_life_adapter.go` | Thin JS lifecycle wrappers over `StartJSWithHooks`, timers and import completion. |
| `internal/engine/js_host_adapter.go` | `Session` implementation of the engine `JSHost` contract. |
| `internal/engine/js_dom.go` | DOM wrappers: document/element/text objects, selectors, attributes plus a live NamedNodeMap-style `element.attributes`, reflected HTMLElement properties (`hidden`, `title`, `lang`, `dir`, `tabIndex`, `contentEditable`, `draggable`, `inert`, …), `innerHTML`, style and class lists, canvas placeholders. Measurement resolves from the laid-out fragments, and a script-created element gets its style computed on demand so `getComputedStyle`/`offsetWidth` see its rules before the next rebuild. The property and method API is defined once on the element/text prototypes and resolves its node from the receiver, so a wrapper costs only its node fields; the `dataset` proxy is built lazily and cached per element. The document wrapper also carries the Node mutation methods and element-child views (`appendChild`, `insertBefore`, `removeChild`, `replaceChild`, `contains`, `hasChildNodes`, `firstChild`/`lastChild`, `firstElementChild`/`lastElementChild`, `children`), because frameworks that hydrate with `document` as the root container call them on the document itself. `<template>.content` is a real inert fragment, and a node whose tag has a registered custom element class is constructed with that class when first wrapped. |
| `internal/engine/js_events.go` | Event constructors (`Event`, `CustomEvent`, `MouseEvent`, `KeyboardEvent` and friends), `document.createEvent`, `dispatchEvent`, `MessageChannel` and `MutationObserver` stubs; HTML/SVG element constructor globals and prototype chains for `instanceof` checks. The DOM API is installed on `Element.prototype`, so SVG elements (which extend `Element`, not `HTMLElement`) inherit `appendChild`, `setAttribute` and the rest. |
| `internal/engine/js_compat.go` | Cheap web-compatibility shims: `dataset`, `outerHTML`, `insertAdjacent*`, attribute helpers, `compareDocumentPosition`, `normalize`, focus/blur/scrollIntoView/getClientRects, Range/Selection, TreeWalker/NodeIterator, TextEncoder/TextDecoder, crypto, structuredClone, AbortController, Blob/File/FileReader, FormData, URL object URLs and queueMicrotask. |
| `internal/engine/js_stubs.go` | Platform interface stubs: `EventTarget`, `NodeList`, `HTMLCollection`, `DOMStringMap`, `CSS`, `ShadowRoot`, `customElements`, `IntersectionObserver`, `ResizeObserver`, `PerformanceObserver`, `Path2D`, `ReadableStream`, `Worker`, Web Animations no-ops, `DOMParser`/`XMLSerializer`, `DataTransfer`, extra event subclasses, interface constructors (`DOMTokenList`, `CSSStyleDeclaration`, `Storage`, `MediaQueryList`, `TreeWalker`, …), `Element.prototype.matches`/`closest` /vendor-alias delegates, and isolated inert iframe `contentDocument`/`contentWindow`. `localStorage`/`sessionStorage` use the `Storage` prototype so `instanceof` and prototype patches work. Constructable `CSSStyleSheet` accepts `insertRule`/`deleteRule`/`replaceSync`, and `PerformanceObserver.supportedEntryTypes` is an empty list so feature detection that reads it does not throw. |
| `internal/engine/js_modules.go` | ES modules: the module graph cache, specifier resolution against the importing module's URL, size/depth caps, `import.meta.url`, dynamic import finishing, and linking/evaluation under the script guard. After the page's own scripts finish, an interactive dynamic import fetches and parses its graph on a background goroutine and the event loop links and evaluates it, so a slow chunk cannot freeze drawing, input or image loading; the graph shares the page's record cache, so a dependency imported by several chunks is fetched and evaluated once. Page-load and headless imports stay synchronous. |
| `internal/engine/js_web.go` | `URL`/`URLSearchParams`, promise-based `fetch` with `Headers`/`Request`/`Response`, and `XMLHttpRequest`; requests are same-origin or simple CORS cross-origin responses. |
| `internal/engine/js_intl.go` | English-only `Intl` formatter subset. |
| `internal/frontend/term_detect.go` | Terminal capability detection from the environment (`COLORTERM`, `TERM`, `TERM_PROGRAM`, multiplexer variables). The paint path receives capabilities instead of reading the environment. |
| `internal/frontend/term_unix.go` | Shared Linux/macOS key and escape-sequence decoding: legacy sequences, `alt-<key>` ESC prefixes, modified arrows/keys (`ctrl-left`, `alt-delete`, …), and kitty keyboard protocol keys with press/repeat/release filtering for both `CSI u` and legacy-encoded functional keys (`CSI 1;mods:event A`). |
| `internal/frontend/term_linux.go` | Linux termios raw mode, window size and cell pixels via ioctl. |
| `internal/frontend/term_darwin.go` | macOS termios raw mode, window size and cell pixels via ioctl. |
| `internal/frontend/term_stub.go` | Other-platform build stub; interactive unsupported, dump still works. |
| `internal/frontend/signals_unix.go` / `signals_other.go` | Platform-specific resize/quit signal wiring so the project cross-compiles. |
| `internal/app/app.go` | Flags, validation, the build `Version` and dispatch into `execute`; direct engine/host/frontend wiring is the remaining app split. |
| `*_test.go` | Unit tests alongside the code; no network dependencies. Test files mirror the source files with the same names and prefixes (`session_chrome_test.go`, `session_forms_test.go`, `session_input_test.go`, `layout_grid_test.go`, `layout_flex_test.go`, `session_config_test.go`, `session_console_test.go`, …), keeping shared helpers and cross-cutting cases in the base file (`session_shell_test.go`, `layout_test.go`). |
