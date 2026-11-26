# Architecture

Part of the MOB³ agent guidance. Start at [AGENTS.md](../../AGENTS.md) for the working
agreement, the non-negotiable constraints and the change protocol.

## Target boundary

The packages are split so page content (DOM, JS, layout, decoders) cannot reach
the terminal, the network policy or the trusted UI. The wire seam is canonical
in `internal/protocol`; this section records ownership.

| Package | Owns |
| --- | --- |
| `internal/protocol` | Message types, binary codec, portable typed stream mux, serializable host RPC, `Session`/`Broker`/`Transport` ports, the in-process transport with validated frame handoff, limits and version. Standard library only; never imports another project package. |
| `internal/textedit` | Shared single-line text editing for form fields, the URL prompt, JS dialogs, find and console inputs. Standard library only. |
| `internal/textwidth` | Shared terminal-cell text arithmetic: rune/string widths and the wide-rune table, truncation, padding, tail windows and wrapping. Standard library only. |
| `internal/movekeys` | Shared movement table and line/half/page step deltas for page and console scrolling, the wheel direction, and the line/top/bottom subset used by list selection. Standard library only. |
| `internal/engine` | HTML/CSS parsing and style, document loading through `protocol.Host`, layout, paint into a content-rect frame, sobek and the DOM, PDF text extraction, image and SVG decoding, find, selection, hints, reader, dark palette. |
| `internal/host` | HTTP/S, TLS and certificates, cookie jar, resource cache, runtime settings and the request log. Session back/forward history lives in the front end, the download/curl hint is presentation, and the engine keeps only a memory-only page cache. |
| `internal/frontend` | Terminal raw mode and key/mouse decoding, chrome and dialogs, session history, ANSI encoding, kitty/iTerm2 placement, PNG output, clipboard. |
| `internal/app` | Wiring: constructs one host, engine session and frontend, connects them over a transport, and runs the event loop. |

Allowed imports:

```
app         -> engine, host, frontend, protocol
frontend    -> protocol, textedit, textwidth, movekeys
engine      -> protocol, textedit, textwidth, movekeys
host        -> protocol
protocol    -> standard library only
textedit    -> standard library only
textwidth   -> standard library only
movekeys    -> standard library only
```

The split is a value boundary. Nothing in `protocol` holds a pointer to engine
state: frames, events and commands are plain data, and a codec round-trip
covers every message. Images cross as encoded bytes with size caps, never as
`image.Image`. Terminal capability detection is front-end-owned and cookie
parsing is host-side; JS `fetch`/XHR go through the host broker. Host calls are
also data: request metadata travels in `HostCall`, not through context values,
and the optional decoded-image cache is a separate `ImageCache` interface that
a process transport can omit.

**Non-goals of this step:** the package split itself added no subprocess, OS
sandbox, tabs or per-site renderer, and no behavior or visual change. The
default target in [Tabs, frames and isolation](#tabs-frames-and-isolation)
uses one engine child per interactive tab; `-process=single` keeps the whole
browser in one process. Both sit behind the same frame router and ownership
model.

### Migration order

All steps are landed. The temporary aliases, test-only host factory and the
migration tracker are gone. The host records each document's final URL and the
front end reads it by entry ID. The top-level browsing frame model is complete;
real iframes are an explicit non-goal, not pending work. The engine child
process is not part of the target.

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
  `protocol`, `textedit`, `textwidth`, approved modules and the
  standard library, with no
  `net/http`, `syscall` or `os` left. `net/url` is a pure value package and
  remains.
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
  `HistoryCommit` URL never reaches the address bar. An attested URL that is
  too long or carries control bytes is cleaned and truncated inside the
  display cap instead of being dropped, so a commit never leaves the previous
  page's URL in the bar.
- Startup, RSS and frame benchmarks stay inside the recorded budget and idle
  produces no frames.

## Tabs, frames and isolation

The default target puts each interactive tab's engine in a child process for
fault isolation. `-process=single` keeps the whole browser in one Go process.
Neither mode is a security boundary: tab separation is logical origin
isolation, not memory, site or OS isolation, and no sandbox or per-site
renderer is part of the model.

- A tab is one protocol session (`SessionID`). The front end owns the tab list,
  the active tab, one shared visited-page list and per-tab back/forward stacks
  and one terminal presenter.
- The tab manager creates one loader, engine `Session` and protocol session per
  tab. Tabs share the host, cookies, cache, TLS state and origin storage; each
  tab owns its loader counters and page cache. Single-mode tabs divide the pools
  through `BudgetManager`; in per-tab mode every tab child is a separate process
  with its own `-mem-limit`.
- The session loop drains every tab's event stream. Only the active tab's frames
  are presented; a background tab's state, history and overlay events still
  update its own model. `protocol.Visibility` tells the engine which tab is
  active; an inactive tab stops drawing, JavaScript timers, deferred images,
  progress ticks and usage wakeups.
- A failed tab is contained: its transport is released, the tab stays in the
  bar marked crashed, and the other tabs keep running.
- A tab owns one top-level browsing frame. Iframes stay inert stub documents
  with no browsing context; there is no child frame tree and none is planned.
  The frame owns its document/`Page`, `Layout`, URL and origin, viewport,
  scroll, timers and one sobek VM.
- There is one sobek VM per top-level frame. No `*sobek.Object`, `*Node` or
  other engine value crosses a tab boundary. The frame router remains the only
  path for the limited cross-frame surface the stubs expose and for a future
  process transport; it copies data and checks the committed origin.
- A frame's origin comes from the URL the host and loader committed for that
  document, never from a script-supplied value. Every origin-scoped API uses
  that origin.
- Iframes are inert postMessage-only stubs. `contentWindow` is a WindowProxy
  with `postMessage` and the limited Window surface; `contentDocument` is null
  even for a same-origin frame, `frameElement` is null, and there is no DOM
  bridge. `parent` and `top` are WindowProxies, not references to another
  document. Iframe navigation, child layout, child scripts and a cross-frame DOM
  bridge are not planned.
- A process-wide context owns the shared origin storage, settings and globally
  unique history-entry IDs. `localStorage` is keyed by origin and shared across
  tabs; `sessionStorage` is keyed by tab and origin.
- Single mode uses one budget manager across all runtimes, page caches and
  image/SVG work, so N tabs divide the pools of that one process.
  `-process=per-tab` applies `-mem-limit` per process: every tab child and the
  parent each get their own limit.
- Zoom and the image graphics mode stay browser-global across tabs; the
  terminal cell size is process-wide.
- The front end is the only terminal writer. The engine has no terminal
  writer, ANSI encoder, graphics renderer or clipboard sink; it returns frames,
  placements and `Clipboard` events.
- The front end presents only the active tab; background tabs do not write the
  terminal.

The frame router is a data boundary by design, so a future process transport
could move behind it without changing the ownership or security model.

Current state: the tab manager and tab bar are implemented. Each tab is a
protocol session with its own engine `Session` and loader over the shared
`BrowserContext`; the loop drains all sessions and presents only the active one.
A background tab keeps loading and updates its own state, history, title and
overlays but receives no frames, runs no JavaScript timers and does not stream
deferred images. Closing a tab closes its transport and unregisters its budget
share. Iframes are inert stub documents that reserve only a labeled black
placeholder box for their declared size; they are a permanent non-goal, not
pending architecture work. The `Session` embeds the top-level `Frame` that owns
the page, layout, URL, scroll and JS runtime; the tab loop asks the frame for
its timers.
`localStorage` and `sessionStorage` use the shared `StorageBroker` behind the
`protocol.StorageProvider` port, and the
shared `BudgetManager` divides the JS, page-cache, image and SVG pools across
sessions.

## Current layout

This is the **actual** state. Tabs are implemented. Real iframe browsing
contexts are a permanent non-goal; iframes stay inert stubs. Interactive tabs
run one engine child per tab by default; `-process=single` keeps them in one
process.

- `cmd/mob3/main.go` — thin entry point; calls `app.Run(os.Args[1:])`.
- `internal/protocol`, `internal/engine`, `internal/host`, `internal/frontend`
  and `internal/app` exist. `app` owns flags, validation, mode dispatch and the
  interactive terminal/session boot; it constructs `host.Session` and
  `engine.NewConfiguredLoader`, then `engine.NewConfiguredSession`,
  `engine.NewAutomationWithConfig` or `engine.NewDumpHandler`.
- `internal/engine/` holds the single 393-method `Session`: page, layout, URL,
  scroll, load lifecycle and application, forms, JS lifecycle, hit testing,
  find/hint/selection/reader state, text scaling, dark mode, content input and
  drawing, console/dialog state, the document loader, headless automation and
  the one-shot dump handler. Its state is grouped into unexported value structs
  declared in the file that owns each subsystem (viewport, options, render,
  display, load, images, placements, find, hints, console, modal, history,
  transport, status, config); the structs are embedded, so the fields stay
  promoted and call sites keep using `b.Field`. It draws only the page content
  rectangle; chrome, console, dialogs, the find bar and the hint overlay are
  front-end-owned. MCP transport lives in `internal/app`. Production code
  imports only `internal/protocol`, `internal/textedit` and
  `internal/textwidth` (plus approved vendored modules).
- The front end owns terminal raw mode, capability detection, key/mouse
  decoding, resize/quit signals, ANSI/PNG output, presentation, native image
  placement, clipboard encoding, the terminal loop and active-tab routing
  (`frontend.SessionLoop`, `frontend/tabs.go`), per-tab state
  (`frontend.Tab`, `frontend/session.go`), normal header/footer composition,
  header/footer and scrollbar mouse input, base shell keys, local quit confirmation, the
  URL/vim prompt, the page find bar, the view/zoom windows, JS dialogs, the
  select popup, download/notice modals, link hints and console
  pane/scroll/input/history/quit/find. It also owns the session history list, current
  index, scroll offsets, the `about:history` display snapshot and the
  content-rectangle geometry (`ContentBounds` → `Init`/`Resize`); the engine
  keys its page cache by the front-end entry ID. It also owns the displayed
  URL: committed history entries replace `State.URL` for chrome, with a
  scheme/length check; the title stays engine-supplied. The host records the final URL and the front end
  reads it by entry ID, so the engine's `HistoryCommit` URL is not trusted. While a page form field holds the key
  path, `State.KeysEngine` stays true for key routing, but `State.FrontendChrome`
  stays true and the front end composes the header, footer and scrollbar.
- The engine imports only `internal/protocol`, `internal/textedit` and
  `internal/textwidth` (plus approved vendored
  modules), and the dependency test enforces that. It does
  not import
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
| `internal/engine/style_selectors.go` | Selector parsing and matching: type/universal/class/id/attribute/descendant/child/sibling compounds, `:not`, `:is`, `:where`, `:has` relative selectors with a bounded traversal, `:hover`, structural pseudos, grouped selectors, specificity and hover detection. Matching memoizes `(compound, node)` states for complex selectors, resets a per-selector step budget and checks the style deadline/abort inside one match; UA matching keeps a separate budget so raw script/style text cannot surface when author matching is exhausted. A pseudo-class the engine cannot evaluate invalidates its selector instead of evaluating to false, because inside `:not()` a false result would invert into a match for every element. |
| `internal/engine/style_sheet.go` | Declaration and stylesheet parsing (comments are skipped inside blocks as well as between them, so a quote or brace in a comment cannot truncate the rest of the sheet, and a backslash escape is skipped while scanning selectors, so an escaped quote in a generated utility class such as `.before\:content-\[\'\'\]` cannot start a phantom string and drop every later rule): tolerant statement scanning, `@import` resolution hooks, `@media`/`@supports` wrapping, CSS cascade layers (`@layer` blocks and `@layer a, b;` statements ranked in declaration order across the document, unlayered rules winning over every layer, `!important` reversing the order, nested layers using dotted names), CSS nesting (`&` replaced by the parent, descendant and combinator children, nested at-rules skipped, depth capped at 8), the parse budgets (4 MiB, 50k rules, 256 KiB per block, 1 s) and the scanner helpers. |
| `internal/engine/style_engine.go` | Style engine: UA/author rule collection, rightmost-compound indexing so large stylesheets do not scan every rule for every element, cascade application, `::before`/`::after` computation (empty boxes and literal text, with private-use icon glyphs and functional values rejected), and the style deadline. |
| `internal/engine/style_media.go` | `@media` evaluation against the layout viewport: `screen`/`all`, min/max-width and range syntax, and `prefers-color-scheme` following the dark-mode state. |
| `internal/engine/style_ua.go` | The UA stylesheet written in the same CSS subset, and its compiled rules. |
| `internal/engine/layout.go` | Layout entry points, the `-layout-budget` deadline/fragment/row limits, `Frag`/`LinkInfo`/`Layout` types, the fragment builders, the box model, block flow and relative offsets. |
| `internal/engine/layout_absolute.go` | Out-of-flow absolute/fixed boxes: deferral, containing-block resolution, placement, and suppression of boxes fully outside the page width, above the document, or outside a known viewport. |
| `internal/engine/layout_flow.go` | Flow scaffolding: children, floats and `clear`, fragment trimming and overflow clipping, list markers, generated `::before`/`::after` boxes with their literal text, and inline pseudo content. |
| `internal/engine/layout_inline.go` | Inline formatting: line boxes, item collection, inline edges/margins, text and word emission, atomic inline boxes, scaled-text overlap handling. |
| `internal/engine/layout_forms.go` | Form widget layout: labels, inputs, textareas, selects, ranges, checkbox glyphs and control predicates. |
| `internal/engine/layout_images.go` | Image layout: intrinsic sizing, display caps, inline percentage widths resolved against sized inline ancestors, fitted/`object-fit` placement, alt labels, block-level inline SVG (image or accessibility fallback), and CSS background painting (block boxes only, cover crop, never native placements so text stays above). |
| `internal/engine/gradient.go` | CSS background gradients: linear/radial and repeating forms, angle/center parsing, color-stop normalization, per-cell sampling over the visible box and alpha compositing against the existing background. |
| `internal/engine/layout_iframe.go` | Iframe boxes: the replaced size from CSS/legacy attributes/`aspect-ratio` or the 300×150 CSS px default, inline and block reservation, and the black placeholder with its gray `iframe`/`i` label. The browsing context is never loaded. |
| `internal/engine/layout_table.go` | Table layout: column sizing, spans, collapsed borders and cell alignment. |
| `internal/engine/layout_grid.go` | CSS grid: template/track parsing for rows and columns (including track values joined by `var()` substitution such as `389pxminmax(0,1fr)`, and `max()`/`min()` rail sizes), named lines, row spans, numeric row/column placement from `grid-column`/`grid-row` and the `grid-area` shorthand, named-area row and column spans, occupancy-based auto placement with `row`/`column` and dense flow, `order`, horizontal `justify-items`/`justify-self`, vertical `align-items`/`align-self` with stretch/center/end, automatic min-content floors for plain `fr` tracks with `minmax(0,…)`, `min-width:0` and non-visible overflow opt-outs, and cell layout. `auto` tracks are sized to their items' max-content width, shrinking toward min-content before the `fr` tracks take the remainder. |
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
| `internal/engine/graphics.go` | Protocol graphics-mode aliases. The engine records native-image and scaled-text capabilities from `protocol.Capabilities`; terminal image placement and ANSI encoding are front-end-owned. |
| `internal/engine/encoders.go` | Injected PNG encoder hook for MCP screenshots. ANSI frame encoding is front-end-owned; the engine does not import `internal/frontend`. |
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
| `internal/protocol/transport.go` | `Handler`/`Transport` contract and the in-process session: handler goroutine, out-of-band abort, abandon-without-wait for a discarded tab, validated frame handoff, bounded coalescing outbox. |
| `internal/protocol/mux.go` | Portable typed frame envelope over one byte stream (`command`, `event`, host-call/result, ping/pong, close), so a page session can run over separate stdin/stdout pipes with no platform-specific file descriptors. |
| `internal/protocol/mux_peer.go` | One read loop per stream that dispatches commands, events, host calls/results and ping/pong to registered handlers. Page and host RPC share one stdin/stdout pair. Writes use a bounded asynchronous queue, so a child that stops reading returns `ErrSessionBusy` instead of blocking the front end. |
| `internal/protocol/stream.go` | Stream transport: `ServeStream`/`ServePeer` handler side with direct abort and `DialStream`/`DialPeer` front-end session over the portable peer. Used by tests and available for a future process transport. |
| `internal/protocol/stream_inbox.go` | Bounded command inbox between the shared peer reader and the engine command loop, so a command burst cannot block host-result delivery. |
| `internal/protocol/hostrpc.go` | Serializable host-broker call/result types, request metadata, error identity codes and the bounded codec. Flat payloads cover every `protocol.Host` method; no `image.Image`, certificate pool or context value crosses the boundary. |
| `internal/protocol/hostrpc_client.go` | `HostClient` multiplexes concurrent host calls by ID, sends cancellation for an aborted context, and `RemoteHost` implements `protocol.Host` over it. `ConfigToken` calls are forwarded. It can own a dedicated peer or mount on the session peer. |
| `internal/protocol/hostrpc_server.go` | `HostServer` dispatches calls concurrently to the trusted host, cancels in-flight work on a cancel frame or shutdown, maps errors to sentinel kinds and contains panics as host errors. It can own a dedicated peer or mount on the session peer. |
| `internal/protocol/storage.go`, `storage_rpc.go` | `Storage`/`StorageProvider` ports and the storage RPC client/server. A process-backed engine resolves `localStorage`/`sessionStorage` through the parent without changing the JS storage surface. |
| `internal/protocol/ports.go` | Shared `EntryIDSource`, `BudgetShare`, `Abandoner` and `Waiter`. The parent can own the history-entry sequence and divide memory pools, and a discarded tab can drop a session without waiting for its handler. |
| `internal/frontend/present.go` | `Presenter`: ANSI frame encoding, row diff, scroll/native resets, state-driven chrome composition and terminal writes. |
| `internal/frontend/session.go` | `Tab`: one tab's protocol session and core per-tab state. It applies events, owns history, routes keys/mouse, runs shell keys and scrollbar/chrome hit testing, handles crashes and the one-shot clipboard permission/failure path. |
| `internal/frontend/overlay.go` | The overlay contract and the one ordered window list. Each front-end window keeps its state in its subsystem file; the list drives key, mouse, page-state, dismissal, tab-switch and crash handling in a single priority order. |
| `internal/frontend/tabs.go` | `SessionLoop`: the terminal-owned side of the browser. It owns the input/resize/quit channels, the stuck-tab channel, cell size, the active tab and the main select loop; the active tab holds the shared presenter pointer. `CloseTab` is graceful; `KillTab` and `DiscardTab` remove a wedged tab without waiting for its session. |
| `internal/frontend/probe.go` | Liveness probe around one page session: commands, loading and a due JS timer keep a deadline armed; a missing event triggers a `Ping`, a missing `Pong` reports the tab as stuck, and an idle state disarms the probe. |
| `internal/frontend/prompt.go` | URL/command prompt state and input, line editing, recall history, visible window and mouse caret mapping. |
| `internal/frontend/linehistory.go` | Shell-style recall list for the URL prompt and console input rows: bounded entries, consecutive-duplicate removal, draft preservation and Up/Down. |
| `internal/frontend/history.go` | Front-end-owned session history and displayed URL ledger: entries, each tab's own back/forward stack and scroll tracking, the shared visited-page log, host-attested URL lookup, validated `HistoryCommit` scroll updates, `about:history` snapshot and back/forward restore requests. |
| `internal/frontend/history_overlay.go` | Front-end history overlay state and input: builds the newest-first popup from the shared visited-page log, handles keys/mouse through the shared choice layout, and opens the selected URL in the current tab. |
| `internal/frontend/chrome.go` | Normal-page header/footer layout from `protocol.State`, header/footer hit regions, `ContentBounds` (the front-end-owned content rectangle) and the scrollbar painter hook. A focused page form still sends keys through the engine but does not hand it chrome. |
| `internal/frontend/menu.go` | View/zoom menu state and input, full-screen dimmed choice box, row/close hit regions. |
| `internal/frontend/dialog.go` | JS alert/confirm/prompt state and input, modal layout, prompt editing and answer routing. |
| `internal/frontend/select.go` | Page select popup state and input, layout and `SelectResult` answer. |
| `internal/frontend/download.go` | Download curl hint state and input, layout and drawing. |
| `internal/frontend/notice.go` | Informational notice state and input, layout and drawing. |
| `internal/frontend/hints.go` | Link-hint state and input, prefix matching and overlay painting. |
| `internal/frontend/console.go` | Console pane, per-section scroll, input, history, quit and find state; layout, renderer, input row, tabs, HTML highlight plus the shared find bar, window requests and console overlay stacking. |
| `internal/frontend/find.go` | In-page find bar state, results status and its footer row. |
| `internal/frontend/findbar.go` | Shared find query, caret and key handling for the page find bar and the console HTML find bar; the engine owns matching. |
| `internal/frontend/listnav.go` | Shared list-selection movement for the history, view/zoom menu and select popups: the line/top/bottom subset of the movement key table plus clamped index moves. |
| `internal/frontend/clipboard.go` | Clipboard policy and OSC 52 encoding: a one-shot permission set by user copy actions, a 100 KiB cap, control/format sanitization and a visible "Clipboard unavailable" failure. |
| `internal/frontend/mouse.go` | Aliases to the shared protocol mouse event, button constants and SGR/X10/token codecs. |
| `internal/protocol/mouse.go` | Shared terminal mouse event, button constants, SGR/X10 decoding and token encoding. |
| `internal/frontend/gfx.go` | Graphics renderer, capability detection, PNG decode/scale/crop and kitty/iTerm2 placement output; the mode type aliases `protocol.GraphicsMode`. |
| `internal/protocol/graphics.go` | Shared graphics mode values (`auto`, `none`, `blocks`, `kitty`, `iterm`). |
| `internal/protocol/origin.go` | Canonical tuple origin: lowercase scheme and host, default port omitted, opaque for non-HTTP(S), `String`/`HeaderValue`/`Equal`/`SameOrigin` helpers shared by the engine and host. |
| `internal/protocol/http.go` | HTTP method constants and standard status reason phrases shared by navigation and the loader. |
| `internal/protocol/cookies.go` | Display-safe cookie rows for the storage inspector. |
| `internal/protocol/clipboard.go` | `SanitizeClipboard`: drops C0/C1 and display-format controls from copied text while keeping newlines and tabs. |
| `internal/protocol/host.go` | `Host` port, host fetch result, `AttestedURL`, request record and config-token interfaces. |
| `internal/protocol/netctx.go` | Shared request metadata, page-space and local-network/mixed-content error values used by loader and host. |
| `internal/host/client.go` | Session HTTP client and swappable TLS transport wrapper; the browser reaches it through `protocol.Host`. |
| `internal/host/session.go` | `protocol.Host` implementation: fetch, policy, settings, cache, cookies, certificates, request log, config token and the final-URL attestation lookup. |
| `internal/engine/session_hooks_impl.go` | `SessionHooks` implementation on `Session`: JS host/runtime effects, timer/hover state, console/modal reset, rebuild/invalidate, status, download, history-commit, form navigation, hint activation and select-menu effects. |
| `internal/engine/context.go` | `BrowserContext`: state shared by tab sessions inside one process. It allocates the opaque history-entry ID sequence and tab IDs, owns the local `StorageBroker` and `BudgetManager`, accepts injectable `protocol.StorageProvider`/`protocol.EntryIDSource` ports, and can reserve a tab-scoped entry-ID prefix for a child engine. |
| `internal/engine/storage.go` | `StorageBroker`/`StorageView`: memory-only `localStorage` keyed by origin and shared across tabs, `sessionStorage` keyed by tab and origin, private areas for opaque origins, and per-area (5 MiB) and context (32 MiB) quotas. |
| `internal/engine/frame.go` | `Frame`: the tab's top-level browsing context. It holds the tab's `Page`, `Layout`, `URL`, `Scroll`, JavaScript runtime and router ID. Iframes are inert stubs with no child frame, so no frame tree exists. `Session` embeds the top-level frame and remains the tab-level façade. |
| `internal/engine/frame_router.go` | `FrameRouter`: the only cross-frame path in a tab. It assigns frame IDs, derives each frame's committed origin, enforces the postMessage target-origin rule and copies the payload, so no node, layout or sobek value crosses an iframe boundary. It holds the top-level frame; iframes are inert stubs. |
| `internal/engine/session_new.go` | `SessionConfig`, `NewConfiguredSession`, `NewAutomationWithConfig` and `AutomationConfig`; the app supplies flags, the shared `BrowserContext` and terminal setup. |
| `internal/engine/version.go` | Injected build version and the help-page wrapper. |
| `internal/engine/session_certs.go` | Config-page trust-store label. |
| `internal/engine/dump.go` | One-shot `-dump`/`-png` handler: load, layout, paint, split tall text frames and emit them through `protocol.InProc`. Terminal sizing/encoding lives in the app. |
| `internal/app/automation_transport.go` | MCP protocol client and `automationHandler`; `engine.Automation` is the handler implementation. |
| `internal/engine/session_loop.go` | `Serve`/`handleCommand` wrappers, the out-of-band abort generation, the cancellable context for synchronous script requests, the front-end hint/modal/select result appliers and pending-frame emission. Abort cancels in-flight script requests and interrupts the running runtime directly, so closing a tab cannot wait on a busy page. The transport loop and command dispatch live in `serve.go`/`commands.go`. |
| `internal/app/execute.go` | Mode dispatch only: process memory limit, interactive detection, `host.Session`/`engine.Loader`/`BrowserContext` construction, budget and zoom validation, then MCP, dump or interactive. The build version is `internal/app.Version`. |
| `internal/app/run_options.go` | The validated `runOptions` surface, `onOff` and `cleanError`. |
| `internal/app/graphics.go` | Native graphics detection, engine capabilities and the `i`-key graphics-mode cycle. |
| `internal/app/interactive.go` | Interactive session boot: terminal raw mode/size/cell detection, `SessionConfig`, presenter, start/cleanup sequences, key/signal goroutines, `Init`/`Navigate` and the session loop. Falls back to dump when raw mode is unavailable. |
| `internal/app/interactive_tabs.go` | Interactive tab registry: creates a loader and one `tabBackend` per tab, keeps the tab-to-backend map, and exposes graceful close and force kill to the session loop. A close that exceeds the grace period is force-discarded. |
| `internal/app/interactive_backend.go` | `tabBackend` interface and the in-process implementation. `Kill` abandons a wedged handler without waiting, and release runs later if it returns. |
| `internal/app/process_worker.go` | Hidden child mode: parses the normal CLI flags, opens the host and storage RPC clients, constructs one engine session with a tab-scoped entry-ID prefix, and serves the page protocol until the parent closes the pipes. |
| `internal/app/process_backend.go` | Process-backed `tabBackend`: self-exec, stdin/stdout pipes, shared host and storage RPC peers, worker-ready handshake, graceful close and process kill. |
| `internal/app/run_mcp.go` | Starts the automation session and serves the MCP transport. |
| `internal/app/dump.go` | One-shot `runDump`/`dumpPage` transport loop for `-dump`/`-png`: terminal sizing/color, protocol session, frame encoding and diagnostics. |
| `internal/engine/page.go` | `Page` and `Script`: the loaded document model, author rules, import map, PDF/download state and post-load inline-style collection. |
| `internal/engine/loader.go`, `loader_config.go` | Document loader: `Loader` over `protocol.Host`, fetch/response classification, charset decode, about/blocked/PDF dispatch, request accounting and the app-facing config constructor. |
| `internal/engine/loader_images.go` | Image loading subsystem: task collection, request context, concurrent fetch/decode, fit, viewport ordering and the in-flight byte budget. |
| `internal/engine/loader_page.go` | `PageFetcher` implementation for page assembly, script/stylesheet collection, body decoding and JavaScript MIME checks. |
| `internal/engine/loader_pdf.go` | PDF page bridge over `AssemblePDFPage`. |
| `internal/engine/loader_svg.go` | Per-page inline-SVG rasterization with pixel/count budgets. |
| `internal/engine/loader_certs.go` | Loader trust-store selection and label. |
| `internal/engine/loader_decode.go` | Charset decoding, single-byte charset table and JavaScript MIME classification. |
| `internal/engine/budget_manager.go` | `BudgetManager`: in-process division of the JS, history-cache, image-byte and inline-SVG pools across active sessions; the host resource cache stays shared. One session gets the old percentages, two sessions halve each pool. `Session.applyBudgetShare` installs a share on the runtime, page cache and loader. Per-tab children skip the split because each is its own process with its own `-mem-limit`. |
| `internal/engine/budget.go` | Effective memory limit, total percentage budgets, `ByteLimiter`, and loader content/resource-cache budget application. |
| `internal/engine/version.go` | Injected build version for built-in pages. |
| `internal/host/cookies.go` | In-memory cookie jar with public-suffix checking, HttpOnly protection for `document.cookie`, and the storage-inspector view. |
| `internal/host/cache.go` | Session-only LRU resource cache shared by raw CSS/JS bodies and decoded images, with a 3% derived budget (32 MiB fallback), 2 MiB per-entry cap, and immediate limit changes from config. |
| `internal/engine/pages.go`, `pages_about.go`, `config_page.go` | Built-in page shell/CSS, start/help/history/blocked/error/config HTML and the config form token field. The session supplies history/settings data. |
| `internal/host/certs.go` | TLS trust store for `-certs`: `system` (the platform store, the default) or `embedded` (the official Mozilla NSS root bundle from `golang.org/x/crypto/x509roots/fallback`, parsed once with its distrust-after constraints attached through `AddCertWithConstraint`). Certificate files are not read. The module's `fallback` package is also imported, which registers the same roots with `x509.SetFallbackRoots`: the default source still verifies HTTPS on a machine with no platform roots, while a present but stale store is left alone (that is what `-certs=embedded` replaces). The selected source replaces the platform store rather than extending it. A stable `certTransport` wrapper swaps the underlying `http.Transport` (and closes its idle connections) when the source changes mid-session, so a change never races an in-flight request or keeps a pooled connection on the old roots. Refresh the bundle by bumping the module and re-vendoring. |
| `internal/engine/pdf_object.go` | PDF object lexer, classic/xref-stream/object-stream cross-reference loading, damaged-xref recovery, and the stream filters (Flate, LZW, ASCIIHex, ASCII85, RunLength, TIFF/PNG predictors) with parse budgets. The deadline and requesting context are checked in the catalog scan, xref rows, object-stream members and predictor rows. |
| `internal/engine/pdf_text.go` | PDF page tree, content-stream interpreter for text operators and form XObjects, font widths and encodings (WinAnsi/MacRoman, `/Differences`, `/ToUnicode` CMaps), inline-image skipping, and line/column reconstruction into styled spans. Page content is charged against a document-wide retained-content budget. |
| `internal/engine/pdf_document.go` | PDF document model on `Page.PDF`, per-page HTML assembly with page rules and `Page n of N` labels, title/metadata, note messages, and panic containment. |
| `internal/engine/session_config.go` | Runtime config model and apply path; the form HTML is in the engine and the one-shot token is `internal/host.ConfigToken`. |
| `internal/engine/session_shell_core.go`, `session.go` | The single `Session` type: content and shell state, input resolution and the config-page branch, hint/status effects around a rebuild, mode toggles, scrolling and link focus, hover chain, link following, notices/downloads, anchor scrolling and `EscapeHTML`. |
| `internal/engine/session_chrome.go` | Content-only draw path: paints the page rectangle, control/selection/find highlights and image placements, then emits the frame. Footer text/level for `State`, status levels, graphics cycling and the memory readout remain here. |
| `internal/engine/session_shell_state.go` | `protocol.State` snapshot and the engine-side view of the front-end content rectangle (`contentRect`, `headerRows`, `viewport`). The front end computes the rectangle; the legacy derivation remains for direct callers and tests. |
| `internal/engine/session_forms.go` | Form wrappers plus the control-focus highlight painter. Tab stepping, control activation, text/range editing, submission, label targets and form requests live in `forms.go` and the other form helpers. |
| `internal/engine/session_input.go` | Content input: `HandleKey`/`HandleMouse` for page movement, forms, find navigation, hint activation, selection and content clicks, plus mouse escape decoding and `MouseEvent`. |
| `internal/engine/session_history.go` | Restore/reload/commit wrappers. The page cache, limits, suspend/cache, restore application and scavenge request are engine-owned; the ordered list/index/scroll live in the front end. |
| `internal/engine/memory.go` | Process soft heap limit (`-mem-limit`, `debug.SetMemoryLimit`) and the fallback JS/history totals. The shared split lives in `budget_manager.go`; image/SVG totals and the byte limiter live in `budget.go`. |
| `internal/engine/session_pdf.go` | PDF page tracking for the footer status line (`Page n/N`), the clickable `(S -> Download name)` hint, and the `S` download shortcut. |
| `internal/engine/session_images.go` | Deferred-image bridge: task/scheduling types, batch application, pending-row layout and refresh after script-driven rebuilds. The engine owns task collection, viewport ordering, the background stream and the `ImageLoader` port. |
| `internal/engine/session_selection.go` | Drag state and clipboard policy; ordering, text extraction and painting call `Selection`. Copy emits a `protocol.Clipboard` event; OSC 52 encoding and the terminal write live in the front end. |
| `internal/engine/automation.go` | Headless `Automation`: construction/close, navigation and load waits. Commands are serialized; `internal/app/automation_transport.go` exposes the type through the MCP protocol client. |
| `internal/engine/automation_snapshot.go` | Snapshot text, viewport grid, page text and screenshots. |
| `internal/engine/automation_click.go` | Click resolution by selector, text or cell, and node description helpers. |
| `internal/engine/automation_input.go` | Typing, key presses and page scrolling. |
| `internal/engine/automation_history.go` | Automation back/forward/reload and its own session history list and output. |
| `internal/engine/automation_eval.go` | `Evaluate` in the page realm. |
| `internal/engine/automation_forms.go` | Form field listing, fill/select/toggle/submit and form-state helpers. |
| `internal/engine/automation_wait.go` | `Wait`/`WaitFor`, timer pumping, dirty flush and pending-navigation waits. |
| `internal/engine/automation_selector.go` | Stable CSS selector generation and matching. |
| `internal/engine/automation_inspect.go` | Links, request log, JS logs and page source. |
| `internal/engine/automation_command.go` | `RunCommand` dispatch and JSON argument decoding. |
| `internal/app/mcp.go` | Newline-delimited JSON-RPC 2.0 MCP server over stdin/stdout; tool calls go through the `automationRunner` interface, which in production is the protocol client. |
| `internal/engine/session_console.go` | Console data, evaluation, tools, copy and stateless `ConsoleView` window slices; JS dialog request/response state; download/notice state. The front end owns pane, scroll, layout, input, history, quit, rendering and the find bar. |
| `internal/engine/session_find.go` | Find state, navigation, result messages and page highlight painting; match computation calls `FindPageMatches`/`FindHTMLMatches`. The visible find bar, query, caret, next/previous and console HTML find scrolling are front-end-owned. |
| `internal/engine/session_hints.go` | Hint-mode state, key handling, following, background-tab opening and the destination-preview step; target discovery, glyph collection and label assignment call `BuildHintTargets`, `CollectHintGlyphs` and `AssignLinkHints`. Overlay painting and input are front-end-owned (`frontend/hints.go`). |
| `internal/engine/reader.go` | Reader-mode article extraction: candidate scoring, junk filtering, filtered tree cloning and the narrow `ReaderRules` stylesheet. |
| `internal/engine/selection.go` | Selection range ordering, text extraction, summary and viewport painting. |
| `internal/engine/hints.go` | Link-hint target discovery, glyph collection, label assignment, unique-pair shortening, base-26 encoding and label painting. |
| `internal/engine/find.go` | Page and console HTML find streams, whitespace normalisation, overlapping case-insensitive spans and match/highlight rectangles. |
| `internal/engine/validate.go` | HTML form constraint validation: required, radio groups, typed date/month/week/time/datetime/color formats and pattern checks. |
| `internal/engine/form_state.go` | Focused text control, select/checkbox/radio/button control, active option, focus transitions and clear helpers. |
| `internal/engine/form_edit.go` | Form control predicates, Tab-status text, radio/select grouping, label lookup, form request encoding, input-fragment updates, range hit testing and input-line/caret helpers. |
| `internal/textedit/textedit.go` | Shared single-line editor: caret and word motion, deletion, selection replacement, insertion and length caps, used by page form fields and the front-end URL, dialog, find and console inputs. |
| `internal/textwidth/textwidth.go` | Shared terminal-cell text arithmetic: rune/string widths and the wide-rune table, truncation, padding, tail windows, wrapping and hanging indent, used by engine layout/paint and front-end chrome, dialogs and console. |
| `internal/movekeys/movekeys.go` | Shared movement: key-token lookup, line/half/page step deltas and the wheel-step direction, used by page scrolling and the front-end console; list overlays reuse the line/top/bottom subset. |
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
| `internal/engine/js_life.go` | Frame-owned JS runtime lifecycle: start/stop, registration of the running top-level runtime for out-of-band abort, timer deadlines, redraw throttling, hover-rebuild deadlines, pending-navigation drain and dynamic-import completion. The tab loop asks the top-level frame for its next timer. |
| `internal/engine/commands.go` | Front-end command dispatch: navigation, history, resize, keys, mouse, scroll, app, find, select, modal, hint, console and error commands, with shell effects supplied by `CommandHooks`. |
| `internal/engine/serve.go` | Transport event loop: load/image/import/timer/progress/usage events, draw/state/frame emission and shutdown, with terminal effects supplied by `RuntimeHooks`. |
| `internal/engine/session.go` | The engine-side tab state: the embedded top-level `Frame`, form/selection state and session dependencies, plus the unexported state groups embedded in place (viewport, options, render, display, load, images, placements, find, hints, console, modal, history, transport, status, config) declared in the file that owns each subsystem. Promoted fields keep the existing `b.Field` call sites; scroll/page/find/hint/PDF helpers stay here. |
| `internal/engine/page_cache.go` | Bounded memory-only back/forward page cache: cached pages, byte/page budgets, eviction callbacks, page/DOM/image weight estimates. |
| `internal/engine/js.go` | sobek runtime, script execution, module script dispatch, internal event dispatch, timers and DOM mutation hooks. |
| `internal/engine/js_host.go` | Per-frame `JSHost` contract and `ImportTask`: layout, scroll and hit testing from the frame, with viewport, focus, dialogs, status, form/upload, cookie/fetch brokering and the import queue from the tab. |
| `internal/engine/js_life_adapter.go` | Thin JS lifecycle wrappers over `StartJSWithHooks`, timers and import completion. |
| `internal/engine/js_host_adapter.go` | Frame-scoped implementation of the engine `JSHost` contract: layout, scroll and node lookup come from the frame; terminal viewport, loader, dialogs and form focus stay tab-level. |
| `internal/engine/js_dom.go` | DOM wrapper plumbing: attribute and tree mutation helpers, fragment parsing, wrapper/unwrap and instance initialization, and custom-element construction. The document wrapper also carries the Node mutation methods and element-child views because frameworks that hydrate with `document` as the root container call them on the document itself. |
| `internal/engine/js_dom_proto.go` | Element and text prototypes: the property/method API, reflected HTMLElement properties (`hidden`, `title`, `lang`, `dir`, `tabIndex`, `contentEditable`, `draggable`, `inert`, …), `innerHTML`, class list and attribute accessors. The API is defined once on the prototypes and resolves its node from the receiver, so a wrapper costs only its node fields; the `dataset` proxy is built lazily and cached per element. |
| `internal/engine/js_dom_html.go` | Node arrays and `innerHTML` parsing. |
| `internal/engine/js_dom_element.go` | Element namespace URIs, a real inert `<template>.content` fragment and node cloning/values. |
| `internal/engine/js_dom_canvas.go` | Canvas and ImageData placeholders and the 2D context object. |
| `internal/engine/js_dom_form.go` | Dialog, form, DOMParser implementation and detached document objects. |
| `internal/engine/js_dom_style.go` | Inline and computed style objects, CSS value conversion and kebab/camel casing. |
| `internal/engine/js_dom_document.go` | The document wrapper and its behavior, including the Node mutation methods and element-child views it exposes. |
| `internal/engine/js_dom_query.go` | Selector parsing and `querySelector`/`querySelectorAll`, with one step budget and abort/deadline check per DOM query. |
| `internal/engine/js_dom_metrics.go` | Element width/height measurement resolved from laid-out fragments (a script-created element gets its style computed on demand so `getComputedStyle`/`offsetWidth` see its rules before the next rebuild), the `styleProps` table and inline style get/set. |
| `internal/engine/js_dom_attrs.go` | Attribute and class-list objects. |
| `internal/engine/js_dom_serialize.go` | Node serialization, void/editability/tab-index helpers, text replacement and node equality. |
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
