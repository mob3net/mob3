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

**Non-goals of this step:** no subprocess, no OS sandbox, no tabs, no
per-site renderer, no behavior or visual change. A later process split is a
transport implementation, not another architecture change.

### Migration order

Each batch lands with tests green; type aliases are temporary and removed at
the end. Steps 1, 2 and 6 are landed. Step 3's normal chrome, dialogs, console,
find, history list, content geometry and the displayed URL/title ledger are
landed; host-attested final URLs, step 4's session type and step 5's Loader
orchestration remain; step 7 is Phase 6.

1. **Landed.** `internal/protocol`, codec, dependency test and the
   output/benchmark baseline.
2. **Landed.** Frame boundary: content-rect `Frame`, structured scaled text
   instead of raw escape cells, ANSI and native-image encoding in the front
   end.
3. **Partial.** Normal chrome, overlays, dialogs, hints, the console, the
   find bar, the session history list/index/scroll, content geometry and the
   displayed URL/title ledger are front-end-owned. The engine still reports the
   committed final URL; host attestation remains.
4. **Partial.** Leaf subsystems moved into `internal/engine`; the session type
   and `Browser` → `Session` split remain.
5. **Partial.** Host `Fetch`, decompression/charset/parse and MCP are split;
   Loader image/SVG/about/PDF orchestration moves with the engine session.
6. **Landed.** Every path, including `-dump`, `-png` and MCP, runs through the
   in-process transport; a stream transport exists for the later process split.
7. **Phase 6.** Fuzz the codec, benchmark against the baseline, remove
   aliases.

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
- Dependencies are checked by a test, not by review. During the migration the
  test allows `app` to import `internal/browser` and leaves `internal/browser`
  unrestricted; both exceptions end with the session split.
- The engine cannot emit chrome. Header/footer, prompts, menus, console, find
  bar, overlays and dialogs are composed by the front end from `State` or
  overlay messages; back/forward history and the download/curl hint live on the
  front end. The engine keeps only a page cache keyed by entry ID.
- Startup, RSS and frame benchmarks stay inside the recorded budget and idle
  produces no frames.

## Current layout (during migration)

This is the **actual** state, not the target. The target above is still the
destination; the tracker lists what remains.

- `cmd/mob3/main.go` — thin entry point; calls `app.Run(os.Args[1:])`.
- `internal/protocol`, `internal/engine`, `internal/host`, `internal/frontend`
  and `internal/app` exist. `app` owns flags, validation, mode dispatch and the
  interactive terminal/session boot; it still imports `internal/browser` for
  the session type, non-interactive render and MCP helpers. `app → browser` is
  the temporary wiring exception.
- `internal/browser/` still holds the session shell: the 306-method `Browser`,
  chrome/overlay composition, dialogs, navigation loads, the memory-only page
  cache, Loader image/SVG/about/PDF orchestration, automation, and most
  integration tests. `Browser` embeds `engine.Session`, which now owns page,
  layout, URL, scroll, focus/hover, reader, text-scaling and dark-mode state;
  the methods and shell fields are still being moved. MCP transport lives in
  `internal/app`.
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
  URL/title: committed history entries replace `State.URL/Title` for chrome,
  with a scheme/length check. The engine still reports the final URL, so
  host-attested authenticity remains. While a page form field holds the key
  path, `State.KeysEngine` stays true for key routing, but `State.FrontendChrome`
  stays true and the front end composes the header, footer and scrollbar. The
  engine fallback remains only for direct callers and tests.
- `internal/browser` has no import rule during the migration; the dependency
  test covers the five target packages. Both temporary exceptions end with the
  session split.
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
  (`layout.go` → `layout_grid.go`, `layout_flex.go`; `browser.go` →
  `browser_chrome.go`, `browser_forms.go`), which matters when a bare name
  would be ambiguous (`forms`, `images`). Standalone subsystems keep their own
  name (`ui.go`, `config.go`, `find.go`, `reader.go`, `hints.go`).

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
| `render_gfx.go` | Browser bridge: graphics-mode aliases/constructors plus inline-SVG loading. Kitty/iTerm2 output moved to `internal/frontend/gfx.go`; placement encoding is in `placement.go`. |
| `paint_compat.go` | Browser aliases and wrappers for the engine grid/paint API and SVG decoders. Temporary. |
| `frontend_bridge.go` | Temporary bridges to `internal/frontend`: grid-to-frame conversion for `Lines`/`LinesMono`/`PNG`, color modes and code helpers. Removed when the engine package makes the seam direct. |
| `internal/frontend/frame.go` | Protocol-only ANSI encoder: color modes, SGR, 256-color mapping, structured scaled runs, mono attributes. |
| `internal/frontend/png.go` | Protocol-frame-to-PNG renderer for `-png` and the MCP `screenshot` tool. Uses Go Mono faces from the approved `golang.org/x/image` module, 8×16 pixel cells, per-cell colors/attributes, half-block image cells, and underline/strike. |
| `frame_protocol.go` | Browser-side mapper from the painted content grid to `protocol.Frame`; the only place internal cell fields become wire values. Moves to the engine session with the split. |
| `internal/host/cache.go` | Session LRU resource cache for raw bodies and decoded images. |
| `internal/host/requests.go` | In-memory console request log, page generation, start/finish accounting and snapshots. |
| `internal/host/settings.go` | User agent, search template and decoded-image caps, shared by navigation and image workers. |
| `internal/host/certs.go` | `system`/`embedded` trust-store selection, the embedded Mozilla CA pool and the stable `CertTransport` wrapper. |
| `internal/host/network.go` | Request metadata, local-network and mixed-content policy, dialers, pooled transport and HTTP client construction. |
| `internal/host/fetch.go` | `Fetcher`: raw HTTP/data requests, size and encoding limits, cache use and request accounting. Page parsing/assembly is engine-owned; the host returns raw bytes. |
| `internal/engine/page_loader.go` | `PageFetcher` contract and HTML page assembly: parse, base/title, stylesheets, scripts and import map. Image/SVG loading and about/PDF orchestration stay in the Loader. |
| `internal/protocol/transport.go` | `Handler`/`Transport` contract and the in-process session: handler goroutine, out-of-band abort, validated frame handoff, bounded coalescing outbox. |
| `internal/protocol/stream.go` | Stream transport: framed reads, `ServeStream` handler side with direct abort, and `DialStream` front-end session. Used by the later child process. |
| `internal/frontend/present.go` | `Presenter`: ANSI frame encoding, row diff, scroll/native resets, state-driven normal chrome composition, chrome-layer fallback and terminal writes. |
| `internal/frontend/session.go` | Live session loop: base shell keys, history keys/back/forward/header actions, local quit, URL/vim prompt, page find, view/zoom windows, JS dialogs, select popup, download/notice modals, link hints, console view/input/find, keys/abort/resize, events, chrome state, header/footer and scrollbar mouse routing, crash dedup and clipboard writes. |
| `internal/frontend/prompt.go` | Prompt line editing, recall history, visible window and mouse caret mapping. |
| `internal/frontend/history.go` | Front-end-owned session history and displayed URL/title ledger: entries, current index, scroll tracking, validated `HistoryCommit` updates, `about:history` snapshot and back/forward restore requests. |
| `internal/frontend/chrome.go` | Normal-page header/footer layout from `protocol.State`, header/footer hit regions, `ContentBounds` (the front-end-owned content rectangle) and the scrollbar painter hook. The engine fallback remains for direct callers and tests; a focused page form still sends keys through the engine but no longer hands it chrome. |
| `internal/frontend/menu.go` | View/zoom menu model, full-screen dimmed choice box, row/close hit regions. |
| `internal/frontend/dialog.go` | JS alert/confirm/prompt view, modal layout, prompt editing and answer routing. |
| `internal/frontend/select.go` | Page select popup model, layout and `SelectResult` answer. |
| `internal/frontend/download.go` | Download curl hint and notice modal layout/drawing. |
| `internal/frontend/hints.go` | Link-hint overlay painting and prefix matching data. |
| `internal/frontend/console.go` | Console snapshot renderer, input row, tabs, scrolling data, HTML highlight/find bar and console overlay stacking. |
| `internal/frontend/clipboard.go` | OSC 52 clipboard encoding. |
| `internal/frontend/mouse.go` | Mouse event/token types and SGR/X10 decoding. |
| `internal/protocol` `Chrome` message | Fallback display-list bridge: header/footer cell layer for direct callers and tests. Normal frames, interactive overlays and form-focused chrome are composed by the front end; removed when the remaining fallback moves. |
| `internal/frontend/gfx.go` | Graphics modes and capability detection, PNG decode/scale/crop and kitty/iTerm2 placement output. |
| `internal/browser/placement.go` | `ImagePlacement` to `protocol.Placement`, bounded encoded-image cache and page image scaling. |
| `internal/browser/dump_transport.go` | One-shot `-dump`/`-png` handler: load, layout, paint, split tall text frames and emit them through `protocol.InProc`. |
| `internal/app/automation_transport.go` | MCP protocol client and `automationHandler`; the browser automation library remains the handler implementation. |
| `internal/browser/session.go` | Interactive `Serve` loop: frame stashing with placements, command routing, load/image/import/timer/progress/usage events, state emission and the fallback direct-key dialog path. Presentation is front-end-owned. |
| `internal/app/execute.go` | Mode dispatch and interactive session boot: loader/session construction, terminal enter/leave, presenter/transport wiring and loop start. The build version is `internal/app.Version`. |
| `internal/engine/page.go` | `Page` and `Script`: the loaded document model, author rules, import map, PDF/download state and post-load inline-style collection. |
| `fetch.go` | Browser-side loader orchestration: image/SVG/about/PDF/blocked handling, decoding and budgets. Raw HTTP/data fetch, cache, cookies, TLS, policy and request accounting are delegated to `internal/host`. |
| `internal/host/cookies.go` | In-memory cookie jar with public-suffix checking, HttpOnly protection for `document.cookie`, and the storage-inspector view. |
| `internal/host/cache.go` | Session-only LRU resource cache shared by raw CSS/JS bodies and decoded images, with a 3% derived budget (32 MiB fallback), 2 MiB per-entry cap, and immediate limit changes from config. |
| `internal/engine/pages.go`, `pages_about.go`, `config_page.go` | Built-in page shell/CSS, start/help/history/blocked/error/config HTML and the config form token field. The session supplies history/settings data. |
| `internal/host/certs.go` | TLS trust store for `-certs`: `system` (the platform store, the default) or `embedded` (the official Mozilla NSS root bundle from `golang.org/x/crypto/x509roots/fallback`, parsed once with its distrust-after constraints attached through `AddCertWithConstraint`). Certificate files are not read. The module's `fallback` package is also imported, which registers the same roots with `x509.SetFallbackRoots`: the default source still verifies HTTPS on a machine with no platform roots, while a present but stale store is left alone (that is what `-certs=embedded` replaces). The selected source replaces the platform store rather than extending it. A stable `certTransport` wrapper swaps the underlying `http.Transport` (and closes its idle connections) when the source changes mid-session, so a change never races an in-flight request or keeps a pooled connection on the old roots. Refresh the bundle by bumping the module and re-vendoring. |
| `internal/engine/pdf_object.go` | PDF object lexer, classic/xref-stream/object-stream cross-reference loading, damaged-xref recovery, and the stream filters (Flate, LZW, ASCIIHex, ASCII85, RunLength, TIFF/PNG predictors) with parse budgets. |
| `internal/engine/pdf_text.go` | PDF page tree, content-stream interpreter for text operators and form XObjects, font widths and encodings (WinAnsi/MacRoman, `/Differences`, `/ToUnicode` CMaps), inline-image skipping, and line/column reconstruction into styled spans. |
| `internal/engine/pdf_document.go` | PDF document model on `Page.PDF`, per-page HTML assembly with page rules and `Page n of N` labels, title/metadata, note messages, and panic containment. |
| `config.go` | Browser-side runtime config model and apply path; the form HTML is in the engine and the one-shot token is `internal/host.ConfigToken`. |
| `browser.go` | Session shell around `engine.Session`: the `Browser` struct, navigation and load lifecycle (progress, cancel, `finishLoad`), page rebuild and mode toggles, scrolling and link focus, hover chain, link following, notices/downloads, anchor scrolling, `EscapeHTML`. Page/layout/URL/scroll live in `engine.Session`. |
| `browser_chrome.go` | Session-side fallback composition: `draw` paints content, composes chrome/overlays, and emits either a content-only frame plus `Chrome` or a full-screen fallback. Status/footer labels, graphics cycling, selection/control overlays, the fallback scrollbar and memory readout remain here. |
| `shell.go` | `protocol.State` snapshot and the engine-side view of the front-end content rectangle (`contentRect`, `headerRows`, `viewport`). The front end computes the rectangle; the legacy derivation remains for direct callers and tests. |
| `browser_forms.go` | Form controls: Tab stepping, control activation, input value editing and caret handling, and GET/POST submission. Focus state, validation, control predicates, Tab-target discovery, radio/select stepping, label lookup, form requests, input-fragment updates, range hit testing and input-line helpers live in `engine` (`form_state.go`, `validate.go`, `form_edit.go`, `tab_order.go`, `form_step.go`). |
| `browser_input.go` | Engine key and mouse input: `HandleKey`/`HandleMouse`, click-to-node and click-to-link hit testing, URL/command prompt editing for direct callers, mouse escape decoding and `MouseEvent`. Base shell keys, history keys, quit, the interactive prompt, page and console find, menus and dialogs are front-end-owned; page form fields still use the engine input path. The shared line-editing core is `engine.EditLineKey`; the front end keeps its own copy in `frontend/prompt.go`, while Enter, Escape, Tab, Up/Down, history and textarea line movement stay with each caller. |
| `browser_history.go` | Memory-aware page cache keyed by front-end history entry ID (per-page weight, budget and per-page cap, suspend/cache/restore/trim, scavenge request after a heavy eviction), restore requests, history commits, reload and meta-refresh targets. The ordered list/index/scroll live in the front end. |
| `memory.go` | Process soft heap limit (`-mem-limit`, `debug.SetMemoryLimit`), the percentage-derived JS/image/SVG budgets, the weighted decoded-image byte limiter, and the one-shot scavenge request consumed between frames. |
| `pdf_compat.go` | Browser aliases and wrappers for the engine PDF parser/document model. |
| `browser_pdf.go` | PDF page tracking for the footer status line (`Page n/N`), the clickable `(d -> Download name)` hint, and the `d` download shortcut. |
| `browser_images.go` | Deferred image batching: viewport-first task sorting, worker batch start/apply, and refresh after script-driven rebuilds. |
| `browser_selection.go` | Drag state and clipboard policy; ordering, text extraction and painting call `engine.Selection`. |
| `automation.go` | Headless `Automation` implementation: navigation, snapshot, click/type/press/scroll, its own history list, JS evaluation and `RunCommand`. Browser-side today; moves with the session. `automation_transport.go` exposes it through the MCP protocol client. |
| `internal/app/mcp.go` | Newline-delimited JSON-RPC 2.0 MCP server over stdin/stdout; tool calls go through the `automationRunner` interface, which in production is the protocol client. |
| `ui.go` | Engine-side fallback for the JS console, alert/confirm/prompt dialogs, the page select popup, the `X` view/`Z` zoom windows and their rendering. Interactive presentation and input for all of these are front-end-owned. |
| `find.go` | Find state, navigation, scrolling, result messages and match highlights; match computation calls `engine.FindPageMatches`/`FindHTMLMatches`. The visible find bar, query, caret and next/previous are front-end-owned. |
| `hints.go` | Hint-mode state, key handling, following and the F destination-confirmation step; target discovery, glyph collection, label assignment and painting call `engine` (`BuildHintTargets`, `CollectHintGlyphs`, `AssignLinkHints`, `PaintHintLabels`). The overlay input is front-end-owned (`frontend/hints.go`). |
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
| `reader.go` | Browser aliases for `engine.ExtractReaderPage`/`engine.ReaderRules`; temporary until the session split. |
| `internal/engine/dark.go` | Dark-mode post-layout palette remap: light backgrounds become dark, dark text becomes light, hues are preserved for links and accents. |
| `internal/engine/session.go` | The engine-side page session state: `Page`, `Layout`, `URL`, `Scroll`, JS runtime, PDF page starts, load lifecycle, deferred-image counters, focus/hover, reader state, text scaling, dark mode, form/selection state, find state and hint state, plus scroll/page/find/hint/PDF helpers. |
| `internal/engine/js.go` | sobek runtime, script execution, module script dispatch, internal event dispatch, timers and DOM mutation hooks. |
| `internal/engine/js_host.go` | `JSHost` contract and `ImportTask`: viewport, layout, scroll, focus, hit testing, dialogs, status, form/upload, cookie/fetch brokering and the import queue. The JS runtime holds this interface, not `*Browser`. |
| `js_compat.go` | Browser aliases and constructor for the engine runtime. |
| `js_life.go` | Browser JS lifecycle: `startJS`, stop, timer loop and dynamic-import completion; moves with the engine session. |
| `js_host.go` | `browserJSHost`: the browser implementation of the engine contract; becomes the engine session adapter after the split. |
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
| `*_test.go` | Unit tests alongside the code; no network dependencies. Test files mirror the source files with the same names and prefixes (`browser_chrome_test.go`, `browser_forms_test.go`, `browser_input_test.go`, `layout_grid_test.go`, `layout_flex_test.go`, `config_test.go`, `ui_test.go`, …), keeping shared helpers and cross-cutting cases in the base file (`browser_test.go`, `layout_test.go`). |
