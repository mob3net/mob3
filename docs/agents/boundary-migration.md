# Boundary migration (B)

**Temporary working document.** It tracks the split of the single `browser`
package into a content engine, a trusted host, a terminal front end and a
protocol seam. Update it as work lands: tick items, record measurements, and
append decisions to the log. Delete it when the migration is complete, after
folding every durable rule into `AGENTS.md` and
[architecture.md](architecture.md). The routing row in `AGENTS.md` and the link
in `architecture.md` are removed in the same change.

Status: **Phase 4b in progress (cache, cookies, request log, settings, certs in the host).**
Branch: `core-rewrite`.
Last updated: 2026-11-22.

## 1. Goal

Protect the terminal UI and the trusted application state (address, settings,
cookies, history, network policy) from page content and its JavaScript, and
make that protection enforceable later without a second architectural rewrite.

This step builds the **boundary**, not the process split:

- the engine produces a serializable frame and receives commands;
- the front end owns the terminal, chrome, dialogs and all terminal encoding;
- the host owns the network, cookies, settings and other policy;
- the engine can be moved into a child process later by swapping the
  transport, not by rewriting the UI.

Success: an engine bug can only corrupt or crash page content, never write a
terminal escape, forge the address bar, read the cookie jar or choose a
network destination.

## 2. Decisions

| ID | Date | Decision | Rationale / status |
| --- | --- | --- | --- |
| D1 | 2026-11-22 | Cut the boundary at the **frame**, not at the DOM. | A DOM boundary is chatty and protects nothing; a frame boundary is ~20 messages and lets the front end own the trusted chrome. |
| D2 | 2026-11-22 | The front end owns chrome, prompts, dialogs and **navigation state**. The engine may only fill a clipped content rectangle. | If the engine composes the address bar, a compromised engine can spoof it. |
| D3 | 2026-11-22 | Target layout is `internal/protocol`, `internal/engine`, `internal/host`, `internal/frontend`, `internal/app`. | Approved. Replaces the old "one package, files by subsystem" rule; files by subsystem still applies inside a package. |
| D4 | 2026-11-22 | The seam is a **value boundary**: plain data only, binary codec, no engine pointers, images as encoded bytes with caps. | Required for a later process split and for hostile-input validation. |
| D5 | 2026-11-22 | This step is B only: **no subprocess, no sandbox, no tabs, no per-site renderer, no behavior or visual change.** | Keep the efficiency profile and land in batches. |
| D6 | 2026-11-22 | The kernel sandbox (Landlock/pledge/AppContainer) is deferred and will be applied to the engine child when the process split happens. | The audit's application-level fixes are already in the log; a single-process sandbox would also sandbox the trusted UI. Landlock is not present in the tree today. Do not describe MOB³ as sandboxed. |
| D7 | 2026-11-22 | Tabs are deferred, but the protocol carries a `SessionID` from day one. | Nearly free now; avoids a protocol break when tabs arrive. |
| D8 | 2026-11-22 | Ownership: engine = DOM/JS/CSS/layout/paint/PDF/images/find/selection/hints/reader; host = HTTP/TLS/certs/cookies/cache/settings/global history/downloads/request log; front end = terminal/input/chrome/ANSI/native images/PNG/clipboard. | See the package table in [architecture.md](architecture.md#target-boundary). |
| D9 | 2026-11-22 | No new dependencies. The codec is fixed binary, not JSON or reflection-based encoding. | Vendored, single binary, and cheap on the frame path. |
| D10 | 2026-11-22 | Parser fuzzing (HTML, stylesheet, PDF, image) is recommended but **not part of B**; only the protocol codec is fuzzed in B. | Separate follow-up; record if it is scheduled. |
| D11 | 2026-11-22 | This file is tracked during the rewrite and deleted at the end. | The user asked for a progress tracker; durable rules move to `AGENTS.md`/`architecture.md` before deletion. |
| D12 | 2026-11-22 | Work happens on `core-rewrite`; the Pages publish job is guarded to `refs/heads/main`, so this branch cannot publish the site. | Keeps in-progress docs off the live site. Pushes to `main` still publish; nothing needs restoring at the end. |
| D13 | 2026-11-22 | `-dump`, `-png`, MCP and automation run through the in-process transport. | The seam is exercised by every path, not only tests. A direct engine call would leave the transport untested until C. |
| D14 | 2026-11-22 | Keep the type name `Browser` during the move; rename it to `Session` at the end of Phase 3. | Avoids a 268-method rename while files are still crossing packages. |
| D15 | 2026-11-22 | The in-process transport encodes and decodes every message by default. A direct struct fast path exists only if measurements demand it, and it must use the same validation. | Keeps the codec on the real path; a test-only codec is a fake seam. |
| D16 | 2026-11-22 | The engine renders `about:` pages from host-supplied data; the host mints and validates the config token. | Keeps trusted state and policy on the host while reusing the HTML engine. |
| D17 | 2026-11-22 | The request log lives in the host; the front end displays it. | The engine does not need to see network policy records. |
| D18 | 2026-11-22 | Wire v1 is a 24-byte binary header plus fixed-value bodies; `Cell` is 20 bytes; scaled text is a structured run; control runes and scaled-run text are rejected on decode. | No reflection, no JSON, a small hostile-input surface, and no escape sequence can cross the boundary. |
| D19 | 2026-11-22 | Phase 1 leaves `Lines`/`LinesMono` and the gfx renderer physically in `render.go`; they are called only from the frontend path and move with the package split in Phase 3. | The functional gate is that the paint path has no ANSI writer, no `os` and no terminal state. Moving ~600 lines now would add risk without changing behavior. |
| D20 | 2026-11-22 | Phase 2 lands shell state, key routing and the content rectangle at the value/API level; the frontend goroutine and the live `State`/`Dialog` event loop land in Phase 5. | Without a separate loop an "emitted" event is read synchronously by `draw`; the payload, ownership and tests are ready for the transport. |
| D21 | 2026-11-22 | Phase 3 starts with `internal/frontend` (ANSI and PNG), which imports only `protocol`; the engine package follows. Browser wrappers keep the old API during the move. | It creates the first real side of the DAG and forces the wire frame onto the output path. A forced full repaint pays a conversion (~10%); row-diff/zero-copy optimization is tracked for Phase 5. |
| D22 | 2026-11-22 | Engine extraction starts with the style primitives: `Color`, `Length`, `Unit`, calc/min/max/clamp parsing, and the CSS split helpers. Browser aliases and wrappers keep the old names. Node and Style follow. | These are leaf types with no Browser dependency, so the package and import gate land with near-zero behavioral risk. |
| D23 | 2026-11-22 | Move `html.go`/`Node`, `Style` and the style sheet/selector/engine/media/UA files into `internal/engine`. Page style injection stays in the browser; style tests move with the engine, while layout-dependent tests stay in the browser. | The DOM/style layer is the prerequisite for layout. Keeping layout tests in the browser avoids exporting layout internals before layout moves. |
| D24 | 2026-11-22 | Move all `layout*.go` files and the dark palette into `internal/engine`; paint, `Grid` and the content frame stay in the browser with aliases. Layout tests stay in the browser because they exercise `PaintLayout`. | Layout is the last engine layer before paint. Keeping paint tests in the browser avoids moving the terminal framebuffer until its frontend/engine split is ready. |
| D25 | 2026-11-22 | Move `Grid`, `PaintLayout*`, `ImagePlacement` and SVG decoding into `internal/engine`; keep kitty/iTerm2 placement, PNG encoding and image scaling in the browser until Phase 5. ANSI/PNG remain in `internal/frontend`. | Paint is page content and crosses no trusted policy. Terminal placement is frontend work and needs the image/placement bridge, so it waits for the transport phase. |
| D26 | 2026-11-22 | Move the PDF object lexer, text extractor and document assembly to `internal/engine`; keep `Loader.loadPDFPage` in the browser. JS/DOM cannot move yet: `jsRuntime` holds `*Browser` and reaches the loader, cookies, dialogs and forms directly. | PDF is a self-contained engine leaf. JS needs the session/host split first, so the next step is that split rather than a forced JS move. |
| D27 | 2026-11-22 | `jsRuntime` no longer holds `*Browser`; it depends on a narrow `jsHost` interface (viewport, layout, scroll, focus, hit testing, dialogs, status, form/upload, cookies, fetch, request accounting, import queue). A `browserJSHost` adapter implements it. | This is the first session/host split step. The interface still lives in the browser and exposes brokered I/O, but JS can no longer reach arbitrary Browser fields. Next: move the contract to engine/protocol and broker the loader calls. |
| D28 | 2026-11-22 | Move `JSHost` and `ImportTask` to `internal/engine`; move `Page`/`Script` and the page style collection there too. The browser adapter still implements the contract, and `js*.go` stays in the browser until its tests can move with it. | The engine now owns the session-facing contract and the document model. JS code itself still depends on browser test surfaces and the loader broker, so moving it now would break test ownership and expose host types. |
| D29 | 2026-11-22 | Extend `JSHost` with request-context/decode/MIME brokering, and move `NearestForm`/`IsFileInput` into the engine. Do not move `js*.go` yet. | `js.go` still mixes runtime code with Browser lifecycle (`startJS`, timer loop, import completion) and its tests read unexported runtime state. That split must happen before the files move, or the tests and event loop move with them. |
| D30 | 2026-11-22 | Move `js*.go` into `internal/engine`; keep `startJS`/`stopJS`/timer-loop/import completion and the host adapter in the browser. Export the runtime accessors the browser and tests need; move pure runtime/module tests to the engine and keep Browser integration tests in the browser. | The runtime is page content and belongs on the engine side. The event loop is session/shell orchestration and still needs Browser state, so it stays on the trusted side for now. |
| D31 | 2026-11-22 | Move the session resource cache and the whole cookie jar (including HttpOnly/document.cookie policy) into `internal/host`; keep browser shims for the existing Loader and tests. | These are host-owned session state and the first pieces that can leave the browser without touching parsing or the draw path. HTTP/TLS, settings and the request log follow. |
| D32 | 2026-11-22 | Move the console request log (`RequestLog`, `Record`, generation/start/finish/snapshot) into `internal/host`; Loader delegates. | The log is host-owned accounting and independent of parsing. HTTP/TLS, settings and cert transport follow. |
| D33 | 2026-11-22 | Move user-agent, search template and image pixel caps into `host.Settings` with one mutex; Loader delegates through its existing methods. Flags and view options stay on the session for now. | Settings are host policy shared by navigation and image workers; the mutex removes the old ad-hoc locking. TLS/HTTP transport and certs follow. |
| D34 | 2026-11-22 | Move the TLS trust-store selection, embedded CA pool and stable `CertTransport` into `internal/host`; keep `newHTTPTransport` and the dial guard in the browser until the network broker. | The cert store is host policy and independent of parsing. The transport construction still needs the browser’s local-network dial guard, so it remains for the next slice. |

Append new decisions with the next ID. Do not rewrite an earlier row; supersede
it with a new one and mark the old one "superseded by Dn".

## 3. Target architecture

| Package | Owns |
| --- | --- |
| `internal/protocol` | Message types, codec, `Session`/`Broker` ports, limits, version. Standard library only. |
| `internal/engine` | HTML/CSS, style, layout, paint into a content-rect frame, sobek and the DOM, PDF text, image/SVG decoding, find, selection, hints, reader, dark palette. |
| `internal/host` | HTTP/S, TLS/certificates, cookie jar, resource cache, settings, global history, downloads, request log. |
| `internal/frontend` | Terminal raw mode and input, chrome, dialogs, ANSI encoding, kitty/iTerm2 placement, PNG output, clipboard. |
| `internal/app` | Wiring and the event loop. |

Allowed imports, enforced by a test:

```
app      -> engine, host, frontend, protocol
frontend -> protocol
engine   -> protocol
host     -> protocol
protocol -> standard library only
```

Exit invariant for the engine: no `frontend`, `host`, `net` or `syscall`
imports, and no `os` left once terminal detection and cookie parsing have
moved.

Known exceptions to remove during the migration:

- `render.go` uses `os.Getenv` for terminal capability detection (Phase 1/2).
- `js_dom.go` uses `net/http.ParseSetCookie` for `document.cookie` (Phase 4).

During the migration the old `browser` package may import a moved package and
alias exported types (`type Page = engine.Page`) so each batch lands with
tests green. No new reach-through.

### File ownership

"Split" means the file is separated at the seam during Phase 3; the note names
which part goes where.

| Current file(s) | Side | Note |
| --- | --- | --- |
| `cmd/mob3/main.go` | app | Wiring only. |
| `html.go` | `internal/engine/html.go` | Tokenizer, tree builder and Node. |
| `style*.go` | `internal/engine` | `Style`, sheets, selectors, cascade, media and UA moved in 3b1; primitives were 3b0. |
| `style_units_compat.go` | browser bridge | Aliases and wrappers for the moved style primitives. |
| `engine_compat.go` | browser bridge | DOM/style aliases, constructors, cascade accessors and layer/import helpers. |
| `js_host.go` | `internal/engine/js_host.go` + browser adapter | Engine-owned host contract/import task; the adapter implements it. |
| `style_app.go` | browser | Zoom percentage parsing/formatting and terminal-to-CSS pixel conversion. |
| `page_styles.go` | `internal/engine/page.go` | `Page`/`Script` and inline-style collection after load. |
| `layout*.go` | `internal/engine` | All layout files, including forms, images, tables, grid, flex and measurement. |
| `layout_compat.go` | browser bridge | Layout aliases, builders, utilities and form predicates. |
| `render.go` | `internal/engine/render.go` | Grid, paint, image placement and SVG decode. |
| `render_gfx.go` | browser bridge | Kitty/iTerm2 placement, cache, PNG encode, image scaling and inline-SVG loading; moves to frontend in Phase 5. |
| `paint_compat.go` | browser bridge | Grid/paint aliases and SVG decoder wrappers. |
| `frame_protocol.go` | browser bridge | Maps the engine grid to `protocol.Frame`; moves with the transport seam. |
| `frontend_bridge.go` | browser bridge | Temporary wrappers for `Lines`, `PNG`, color modes and code helpers during the package move. |
| `term_detect.go` | frontend | Terminal capability detection; `DetectColorMode` moved to `internal/frontend`. |
| `png.go` | `internal/frontend/png.go` | Protocol frame to PNG. |
| `fetch.go` | host + engine | HTTP/TLS/redirect/cookies/size caps host; decompression, charset, parsing and decoder budgets engine. `Page`/`Script` now live in `internal/engine/page.go`. |
| `cache.go` | `internal/host/cache.go` | Session resource cache; browser shim preserves the Loader API. |
| `cookie` section of `fetch.go` | `internal/host/cookies.go` | Cookie jar, HttpOnly/document.cookie policy and the storage-inspector view; browser shim. |
| `requestRecord`/request-log section of `fetch.go` | `internal/host/requests.go` | Console request log and page-generation accounting; Loader delegates. |
| Loader settings fields | `internal/host/settings.go` | User agent, search template and image pixel caps behind one lock. |
| `certs.go` | `internal/host/certs.go` | Cert source selection, embedded CA pool and stable transport; browser keeps `newHTTPTransport`/dial guard. |
| `pages.go` | engine | About/help/error HTML from host data; the config token is validated by the host. |
| `certs.go` | host | Trust store selection. |
| `pdf_object.go`, `pdf_text.go`, `pdf.go` | `internal/engine` | Object/text parser and document assembly; `pdf_compat.go` keeps the browser API. |
| `config.go` | host + engine | Model and apply host; `about:config` form markup engine. |
| `browser.go` | engine | Session state, navigation and load lifecycle; shell state moves to the frontend. |
| `browser_chrome.go` | frontend | `drawChrome` composes toolbar, footer, status and app overlays; `flushGrid` only encodes. |
| `shell.go` | frontend | Content rectangle, header reservation and the `protocol.State` snapshot. |
| `browser_forms.go` | engine | Form state, validation and submission; prompt editing frontend. |
| `browser_input.go` | frontend + engine | Key/mouse decoding, prompts and app keys frontend; hit testing and field editing engine. |
| `browser_history.go` | engine + host | Per-session back/forward engine; global visited list host. |
| `memory.go` | host + engine | Budget policy and process limit host; per-session enforcement engine. |
| `browser_pdf.go` | engine | PDF footer state and download shortcut. |
| `browser_images.go` | engine | Deferred batch scheduling; fetching host. |
| `browser_selection.go` | engine + frontend | Selection state and text engine; clipboard and OSC 52 frontend. |
| `automation.go` | app | Protocol client. |
| `mcp.go` | app | Protocol client. |
| `ui.go` | frontend + engine | Console/dialog drawing frontend; console data and JS state engine. |
| `find.go` | engine + frontend | Search engine; find bar frontend. |
| `hints.go` | engine + frontend | Hint computation engine; overlay drawing frontend. |
| `reader.go` | engine | Article extraction. |
| `dark.go` | `internal/engine/dark.go` | Palette remap. |
| `js*.go` | `internal/engine` | Runtime, DOM, events, compat, stubs, modules, web APIs, Intl. |
| `js_compat.go` | browser bridge | Runtime/import-task aliases and constructor. |
| `js_life.go` | browser | `startJS`, stop, timer loop, import completion; the session/shell orchestration. |
| `js_host.go` | browser bridge | `browserJSHost` adapter for the engine contract. |
| `term_unix.go`, `term_linux.go`, `term_darwin.go`, `term_stub.go` | frontend | Terminal modes, size and cell pixels. |
| `signals_unix.go`, `signals_other.go` | frontend | Resize and quit signals. |
| `cli.go` | app + frontend | Flags and wiring app; terminal event loop frontend. |

## 4. Phases

Estimates are for one developer who knows the codebase, Linux first, ±40%.
Total: **9–13 weeks**. Phase 3 dominates the risk.

| Phase | Scope | Estimate | Status | Exit gate |
| --- | --- | --- | --- | --- |
| 0 | Contract, codec, dependency test, baseline | 1 week | Done | Protocol compiles; baseline recorded |
| 1 | Frame boundary and paint split | 1–2 weeks | Done* | `-dump`/`-png` byte-identical; paint path has no ANSI |
| 2 | Chrome and state ownership | 2–3 weeks | Done** | UI tests pass; engine has no terminal or chrome concepts |
| 3 | Extract `internal/engine` | 3–4 weeks | Paused before JS/session | Engine import gate passes; engine tests green |
| 4 | Extract `internal/host` and broker | 2–3 weeks | In progress (4b-4) | Engine cannot open sockets or read cookies |
| 5 | Transport and lifecycle | 1–2 weeks | Not started | All paths run through the in-process transport |
| 6 | Verification, docs, cleanup | 1 week | Not started | Gates below hold; aliases removed |

\* Phase 1 is functionally complete. The `Lines`/gfx file move is folded into
Phase 3 under D19.

\*\* Phase 2 lands the shell state/key/content-rect ownership. The live event
loop and dialog request/response transport land in Phase 5 under D20.

### Phase 0 — Contract and baseline

- [x] Write the ownership table for every file (engine / host / frontend / app).
- [x] Create `internal/protocol` with `Frame`, `Cell`, `Placement`, `Command`,
      `Event`, `State`, `Limits`, `SessionID`, `Version`.
- [x] Define the `Session` and `Broker` ports in `protocol`.
- [x] Implement a fixed binary codec; no reflection and no `gob`.
- [x] Round-trip test for every message; fuzz the decoder with size/count
      bounds.
- [x] Add the dependency test (allowed graph above), initially scoped to the
      new packages.
- [x] Build the baseline corpus from synthetic pages. The current page covers
      text, table, grid, many links and images off; add deep-DOM, PDF and
      long-script baselines with the phases that touch those paths.
- [x] Record baseline: startup ms, RSS MB, `-dump` hash, `-png` hash, codec and
      paint timing, allocations per frame.
- [ ] Idle-frame count: measure in Phase 5, when the frame loop exists.
- [x] Add the routing row and architecture link for this file.
- [x] Resolve the Phase 0 open questions in §8.

### Phase 1 — Frame boundary

- [x] Engine paints into a content-rect frame, not the full-screen `Grid`.
      `draw` paints the page viewport at row 0, then blits it below the chrome;
      `toProtocolFrame` maps the result to the wire contract.
- [x] Replace `Cell.Raw` with structured scaled-text runs; `Cell` is now plain
      data and shrank from 56 to 24 bytes.
- [ ] Move `Grid.Lines`/`LinesMono`, gfx and kitty/iTerm2 placement out of
      `render.go`. Deferred to Phase 3 (D19); `png.go` is already separate.
- [x] Move terminal capability detection out of `render.go` to
      `term_detect.go`; the paint path no longer imports `os`.
- [x] Reuse two frame buffers. `BenchmarkBoundaryContentFrame` is 0 B/op;
      ANSI string building still allocates as before.
- [x] Keep the existing row diff on the front-end side (`flushGrid`).
- [x] Frame tests: content-blit equivalence, protocol round-trip, cell-size
      guard, dump/PNG hashes.

### Phase 2 — Chrome and state ownership

- [x] Front end composes toolbar, footer, prompts, notices, dialogs, console,
      view/zoom menus and the find bar in `drawChrome`; `flushGrid` is output
      only.
- [x] The `State` snapshot exists (`stateMessage`) with URL, title,
      loading/progress, scroll/max-scroll, page kind and mode flags. Emission
      waits for the Phase 5 loop (D20).
- [x] The content rectangle is shell-owned (`contentRect`, `headerRows`,
      `viewport`); the engine receives only its size.
- [x] Page keys are forwarded; app keys, prompts, menus and quit are local
      (`handleShellKey` / `handlePageKey`).
- [ ] JS dialogs become request/response messages. Deferred to Phase 5 (D20).
- [x] Engine overlays are clipped to the content grid; chrome and modals
      compose above it.
- [x] Automation still drives the same commands; full suite green.

### Phase 3a — Extract `internal/frontend` (done)

- [x] Create `internal/frontend` with `frame.go` (ANSI encoding, color modes)
      and `png.go` (frame to PNG).
- [x] Keep the package protocol-only; the dependency test enforces it.
- [x] Add browser bridges (`frontend_bridge.go`) so the old `Grid.Lines`,
      `LinesMono`, `PNG`, color-mode and code helpers keep working.
- [x] Reuse the browser protocol frame buffer on the draw path; a repaint does
      not allocate a second cell grid.
- [x] Frontend tests for color/mono encoding, scaled runs, invalid frames and
      PNG dimensions.
- [x] Dump and PNG hashes unchanged.

Next: 3b, the engine package.

### Phase 4a — Session/host interfaces (in progress)

- [x] `jsHost` interface and `browserJSHost` adapter; `jsRuntime` holds the
      interface, not `*Browser`.
- [x] Viewport, layout, scroll, focus, hit testing, dialogs, status, console,
      form submission and upload notice are methods.
- [x] Cookie reads/writes, resource fetch, request accounting, UA, HTTP client
      and the import queue are brokered through the adapter.
- [ ] Move the host contract to `internal/engine`/`protocol` and remove the
      remaining browser types from it.
- [ ] Move `js*.go` to `internal/engine`; keep the adapter in the browser.
- [ ] Split `Loader` into the host package and migrate `about:`/form/config
      policy.

### Phase 3b — Extract `internal/engine` (in progress)

- [x] Move `Color`, `Length`, `Unit` and the CSS split helpers to
      `internal/engine`.
- [x] Move calc/min/max/clamp parsing, viewport/rem/lh resolution and font-size
      parsing.
- [x] Keep browser aliases (`style_units_compat.go`) and the
      `resolveStyleUnits` method in the browser until `Style` moves.
- [x] Engine import gate is active; engine imports only the standard library
      today.
- [x] Move `html.go` and `Node`.
- [x] Move `Style`, sheets, selectors, media queries and the UA sheet.
- [x] Move the engine tests; keep layout-dependent style tests in the browser.
- [x] Move layout and the dark palette.
- [x] Move paint and the content frame; keep native-image/terminal encoding in the browser until Phase 5.
- [x] Move the PDF parser and document model (3b4a).
- [ ] Move JS/DOM and the session; blocked on the session/host interface split (D26).
- [ ] Remove the browser wrappers.

### Phase 3 — Extract `internal/engine`

- [ ] 3a: move HTML, style, layout, paint, find, selection, hints, reader,
      dark in batches, with aliases.
- [ ] 3b: move JS, DOM, events, compat, stubs, modules, web, Intl.
- [ ] 3c: move `Browser` and all its methods in one batch; rename to `Session`
      at the end.
- [ ] Move engine tests; convert whole-app tests into seam tests in `app`.
- [ ] Remove aliases.
- [ ] Engine import gate passes.

### Phase 4 — Extract `internal/host`

- [ ] Split `Loader.Load` into host `Fetch` (status, headers, final URL, raw
      bytes) and engine decompression/charset/parse.
- [ ] Move loader, cookies, cache, certs, settings, global history, downloads
      and request log to `internal/host`.
- [ ] Broker: page/subresource/image/script/module/XHR/fetch requests, cookie
      get/set, settings snapshot, clipboard, download report.
- [ ] Host enforces CORS, private-network, mixed-content, redirect and size
      limits; engine enforces decompression, charset, parse, pixel and time
      budgets.
- [ ] Inline SVG/image decoding stays in the engine.
- [ ] `about:` pages render in the engine from host-provided data; the config
      token stays host-side.
- [ ] Move automation/MCP to a protocol client.
- [ ] Move `document.cookie` parsing to the host broker.

### Phase 5 — Transport and lifecycle

- [ ] Define `Transport`; implement the in-process transport.
- [ ] Run every path through it, including `-dump`, `-png`, tests and MCP.
- [ ] Engine runs on its own goroutine with its timer loop; front end owns the
      terminal loop.
- [ ] Out-of-band `Abort` so Esc can cancel a busy script.
- [ ] Frame coalescing, backpressure and hostile-frame bounds.
- [ ] Transport-level restart hook (unused in-process, used by the later
      child).
- [ ] Keep a socket transport behind the same interface for that later step.

### Phase 6 — Verification and cleanup

- [ ] Fuzz the codec for 60 s in CI or nightly.
- [ ] Benchmarks against the baseline; record deltas here.
- [ ] `-race` on engine/frontend.
- [ ] Update `AGENTS.md` §4, `docs/agents/architecture.md`, `README.md` only if
      user-visible behavior changed, and `docs/agents/limitations.md` if a
      limitation changed.
- [ ] Remove aliases, shims and dead code.
- [ ] Delete this file and its links (see §10).

## 5. Protocol draft

`internal/protocol` is canonical once created; this list is the starting
inventory.

**Front end → session:** `Init` (viewport, cell pixels, capabilities, flags),
`Navigate`, `Resize`, `Scroll`, `Key`, `Mouse`, `Focus`, `Command`
(back/forward/reload/zoom/dark/reader/find/hints/select/copy), `Automation`,
`Abort`, `Shutdown`.

**Session → front end:** `Frame` (content cells + placements), `State`,
`Links`, `FindResults`, `SelectionText`, `Dialog`, `Clipboard`,
`NavigateRequest`, `Download`, `Console`, `Crash`.

**Session → host (broker):** `Fetch`, `Cancel`, `GetCookie`, `SetCookie`,
`Settings`, `LogRequest`, `ReportDownload`.

**Host → front end:** `RequestLog`, `SettingsChanged`.

Limits to define: max viewport cells, max metadata entries, max text bytes,
max encoded image bytes, max message size, frame rate/backpressure.

## 6. Risks

| Risk | Mitigation |
| --- | --- |
| `Browser` has 268 methods; Go cannot split its methods across packages | Move leaf subsystems first, then the whole shell in one batch; use type aliases between batches. |
| `Loader.Load` returns a parsed `*Page` | Split network/parse early in Phase 4; update about pages, PDF detection, error pages and downloads together. |
| Package globals (`layoutZoom`, `cellW`, `cellH`, `DefaultLayoutBudget`, memory limits) | Turn them into session options; required before any multi-session work. |
| Automation/MCP are 2,000+ lines of direct state access | Rewrite as a protocol client; count it in Phase 4. |
| Console reads runtime internals directly | Snapshot events with caps; no direct struct access from the front end. |
| Test migration (21k lines) | Move tests with their files; build an `app` seam harness; keep layout fixtures on the engine side. |
| Overlay composition | Decide modal ownership in Phase 2; clip engine overlays to the content rect. |
| Silent visual drift | `-dump` and `-png` hashes plus frame goldens at every phase. |
| Performance drift | Reusable buffers, no per-frame allocations, metadata only on layout changes, benchmarks at every phase. |
| Scope creep | This file's non-goals are binding; new ideas go to `bin/todo.md`, not into B. |

## 7. Performance budget

Baseline recorded 2026-11-22 on linux/amd64, i7-1255U, Go 1.27. Absolute values
are machine-specific; the gate is the delta on the same machine. The synthetic
page is `boundaryBaselineHTML()` in
`internal/browser/boundary_baseline_test.go`.

| Metric | Baseline | Gate |
| --- | --- | --- |
| Startup (tiny `-dump`) | 4.7–4.9 ms | +2 ms max |
| RSS (tiny `-dump`) | 17–19 MB | +2 MB max |
| Synthetic `-dump` | 34–38 ms, 44–45 MB | no unreviewed increase |
| `-dump` hash | `320723811c02b681c11db45eaa2860b72ef653697165b5f92bc8d55ad793a830` | byte-identical |
| `-png` hash | `5a5adb26413007dc5a35abd99cd7144823fb46f2f0f946609901ec54f82256aa` | byte-identical |
| Codec frame, 152×35, encode+decode | 320–328 µs, 106 668 B/op, 2 allocs | no regression |
| Synthetic parse | 0.61–0.78 ms, 1.00 MB, 6 101 allocs | no regression |
| Layout+paint+ANSI, 100×35 | 12.4–13.3 ms, 19.93 MB, 56 858 allocs | ≤5% regression |
| Full draw, forced repaint | 898–905 µs, 716 078 B/op, 28 213 allocs | no regression |
| Idle frames/s | pending Phase 5 | 0 |

The codec benchmark is `BenchmarkCodecFrame` in `internal/protocol`; the parse,
paint, draw and content-frame benchmarks are in
`internal/browser/boundary_baseline_test.go`. A later process split is expected
to add a second Go runtime and IPC; those numbers are recorded separately and
are not B's budget.

### Phase 1 deltas

Before values were measured in a temporary worktree at the Phase 0 commit with
the same synthetic page and the same benchmark bodies; the worktree was removed
after measuring.

| Metric | Before Phase 1 | After Phase 1 | Delta |
| --- | --- | --- | --- |
| `Cell` size | 56 B | 24 B | −57% per cell |
| Full draw, forced repaint | 898–905 µs, 716 078 B/op, 28 213 allocs | 821–824 µs, 519 539 B/op, 28 212 allocs | −8.7% time, −27.4% B/op, allocs flat |
| Layout+paint+ANSI, 100×35 | 12.4–13.3 ms, 19.93 MB, 56 858 allocs | 12.8–13.7 ms, 19.83 MB, 56 858 allocs | flat within noise |
| Content paint+blit, 100×33 | new path | 90.9–100.8 µs, 0 B/op | frame buffers are allocation-free |
| Synthetic parse | 0.61–0.78 ms, 1.00 MB | 0.57–0.58 ms, 1.00 MB | noise, no regression |
| Startup (tiny `-dump`) | 4.7–4.9 ms | 4.5–5.0 ms | flat |
| Synthetic `-dump` | 34–38 ms, 44–45 MB | 25–36 ms, 40–42 MB | noisy, no regression |
| `-dump`/`-png` hashes | fixed | identical | pass |

The draw win is the reused grids: the old path allocated a full 100×35 grid
per repaint, which at 56 bytes per cell is about 196 KB. The new path reuses a
content grid and a screen grid; the remaining draw allocations are ANSI line
building.

### Phase 2 deltas

This phase is structural: it moves composition, geometry and key routing, but
does not add work to the hot path. `stateMessage` is not called by `draw` until
the Phase 5 transport.

| Metric | Before Phase 2 | After Phase 2 | Delta |
| --- | --- | --- | --- |
| Full draw, forced repaint | 821–824 µs, 519 539 B/op, 28 212 allocs | 774–820 µs, 519 550 B/op, 28 212 allocs | within noise |
| Content paint+blit, 100×33 | 91–101 µs, 0 B/op | 94–99 µs, 0 B/op | within noise |
| `-dump`/`-png` hashes | fixed | identical | pass |
| Startup / RSS | 4.5–5.0 ms / 19.3 MB | not re-measured | no new startup or heap work |

### Phase 3a deltas

The frontend package is protocol-only. The draw path now converts the screen
grid to `protocol.Frame` before encoding, which is the measured cost below.
The frame buffer is reused, so allocations are flat.

| Metric | Before Phase 3a | After Phase 3a | Delta |
| --- | --- | --- | --- |
| Full draw, forced repaint | 774–820 µs, 519 550 B/op, 28 212 allocs | 850–875 µs, 519 595 B/op, 28 212 allocs | +5–10% time, bytes/allocs flat |
| Content paint+blit, 100×33 | 94–99 µs, 0 B/op | 92–100 µs, 0 B/op | flat |
| Synthetic `-dump` | 25–36 ms | 32–44 ms | noisy; full-page frame conversion |
| Synthetic `-png` | not measured | 30–32 ms | new |
| `-dump`/`-png` hashes | fixed | identical | pass |

### Phase 3b1 deltas

Moving the DOM and style model adds cross-package calls on cold paths only;
the layout pass and draw are unchanged within noise. One layout run showed an
outlier at 15–17 ms under load; reruns were stable at the values below.

| Metric | Before 3b1 | After 3b1 | Delta |
| --- | --- | --- | --- |
| Full draw, forced repaint | 863–874 µs, 519 609 B/op | 812–857 µs, 519 591 B/op | flat |
| Layout+paint+ANSI | 12.9–13.4 ms, 19.90 MB | 13.0–13.4 ms, 19.90 MB | flat |
| Content paint+blit | 97–101 µs, 0 B/op | 95–101 µs, 0 B/op | flat |
| `-dump`/`-png` hashes | fixed | identical | pass |

### Phase 3b2 deltas

Layout now crosses the package boundary, but paint still reads the same value
types through aliases. One layout run showed an outlier at 16.9 ms under load;
reruns were stable at the values below.

| Metric | Before 3b2 | After 3b2 | Delta |
| --- | --- | --- | --- |
| Full draw, forced repaint | 812–857 µs, 519 591 B/op | 828–845 µs, 519 594 B/op | flat |
| Layout+paint+ANSI | 13.0–13.4 ms, 19.90 MB | 12.8–13.0 ms, 19.90 MB | flat |
| Content paint+blit | 95–101 µs, 0 B/op | 79–102 µs, 0 B/op | flat/noisy |
| `-dump`/`-png` hashes | fixed | identical | pass |

### Phase 3b3 deltas

Grid, paint and SVG decode moved to the engine; the gfx/terminal layer stayed
in the browser with aliases. One layout run showed 14–16 ms under load; reruns
were stable at the values below.

| Metric | Before 3b3 | After 3b3 | Delta |
| --- | --- | --- | --- |
| Full draw, forced repaint | 828–845 µs, 519 594 B/op | 829–876 µs, 519 600 B/op | flat |
| Layout+paint+ANSI | 12.8–13.0 ms, 19.90 MB | 13.1–14.2 ms, 19.90 MB | flat/noisy |
| Content paint+blit | 79–102 µs, 0 B/op | 93–102 µs, 0 B/op | flat |
| `-dump`/`-png` hashes | fixed | identical | pass |

### Phase 3b4a deltas

PDF parsing and document assembly moved to the engine. It is not on the draw
path, so performance is unchanged; PDF fixture and text-extraction tests pass.

| Metric | Before 3b4a | After 3b4a | Delta |
| --- | --- | --- | --- |
| Full draw, forced repaint | 829–876 µs, 519 600 B/op | unchanged | flat |
| Layout+paint+ANSI | 13.1–14.2 ms, 19.90 MB | unchanged | flat |
| `-dump`/`-png` hashes | fixed | identical | pass |
| PDF extraction tests | pass | pass | no change |

### Phase 4a deltas

The JS runtime now calls an interface instead of touching Browser fields. Draw
and layout do not run JS, so their numbers are unchanged; a rerun after the
change was 839–880 µs for a forced repaint and 96–100 µs for the content
frame, inside the existing range. JS-heavy paths pay interface dispatch, which
is small relative to the sobek work they already do.

| Metric | Before 4a | After 4a | Delta |
| --- | --- | --- | --- |
| Full draw, forced repaint | 829–876 µs, 519 600 B/op | 839–880 µs, 519 599 B/op | flat |
| Content paint+blit | 93–102 µs, 0 B/op | 96–100 µs, 0 B/op | flat |
| `-dump`/`-png` hashes | fixed | identical | pass |
| Full test suite | pass | pass | no change |

### Phase 4a-2 deltas

The host contract, `ImportTask` and `Page`/`Script` moved to the engine. Draw
and layout are unchanged; JS still runs in the browser behind the contract.

| Metric | Before 4a-2 | After 4a-2 | Delta |
| --- | --- | --- | --- |
| Full draw, forced repaint | 839–880 µs, 519 599 B/op | unchanged | flat |
| Content paint+blit | 96–100 µs, 0 B/op | unchanged | flat |
| `-dump`/`-png` hashes | fixed | identical | pass |
| Full test suite | pass | pass | no change |

### Phase 4a-3 deltas

The broker surface gained request context, body decoding and JS MIME checks;
form predicates moved to the engine. This is not on the draw path.

| Metric | Before 4a-3 | After 4a-3 | Delta |
| --- | --- | --- | --- |
| Full draw, forced repaint | unchanged | unchanged | flat |
| Content paint+blit | unchanged | unchanged | flat |
| `-dump`/`-png` hashes | fixed | identical | pass |
| Full test suite | pass | pass | no change |

### Phase 4a-4 deltas

The JS runtime moved to the engine; the Browser keeps the adapter and the
event-loop orchestration. Draw/layout do not run JS, so they are unchanged.

| Metric | Before 4a-4 | After 4a-4 | Delta |
| --- | --- | --- | --- |
| Full draw, forced repaint | 839–880 µs, 519 599 B/op | 840–865 µs, 519 610 B/op | flat |
| Content paint+blit | 96–100 µs, 0 B/op | 85–105 µs, 0 B/op | flat/noisy |
| `-dump`/`-png` hashes | fixed | identical | pass |
| Full test suite | pass | pass | no change |

### Phase 4b-1 deltas

The session cache and cookie jar moved to `internal/host`; neither is on the
draw path. Browser shims keep the Loader and tests unchanged.

| Metric | Before 4b-1 | After 4b-1 | Delta |
| --- | --- | --- | --- |
| Full draw, forced repaint | unchanged | unchanged | flat |
| Content paint+blit | unchanged | unchanged | flat |
| `-dump`/`-png` hashes | fixed | identical | pass |
| Full test suite | pass | pass | no change |

### Phase 4b-2 deltas

The request log moved to `internal/host`; it is console-only and not on the
draw path.

| Metric | Before 4b-2 | After 4b-2 | Delta |
| --- | --- | --- | --- |
| Full draw, forced repaint | unchanged | unchanged | flat |
| Content paint+blit | unchanged | unchanged | flat |
| `-dump`/`-png` hashes | fixed | identical | pass |
| Full test suite | pass | pass | no change |

### Phase 4b-3 deltas

User agent, search template and image caps moved to the host settings object.
No draw-path code changed.

| Metric | Before 4b-3 | After 4b-3 | Delta |
| --- | --- | --- | --- |
| Full draw, forced repaint | unchanged | unchanged | flat |
| Content paint+blit | unchanged | unchanged | flat |
| `-dump`/`-png` hashes | fixed | identical | pass |
| Full test suite | pass | pass | no change |

### Phase 4b-4 deltas

The TLS trust store and stable transport wrapper moved to `internal/host`.
No draw-path code changed.

| Metric | Before 4b-4 | After 4b-4 | Delta |
| --- | --- | --- | --- |
| Full draw, forced repaint | unchanged | unchanged | flat |
| Content paint+blit | unchanged | unchanged | flat |
| `-dump`/`-png` hashes | fixed | identical | pass |
| Full test suite | pass | pass | no change |

The conversion is a straight cell copy plus attribute mapping, not a second
paint. It is paid only when a frame is encoded. Phase 5 can remove it by making
the transport carry the frame directly, or by encoding dirty rows only.

### Phase 3b0 deltas

Moving the style primitives adds only cross-package calls; the hot path is
unchanged. One layout run showed an outlier at 16–21 ms under load; reruns were
stable at the values below.

| Metric | Before Phase 3b0 | After Phase 3b0 | Delta |
| --- | --- | --- | --- |
| Full draw, forced repaint | 850–875 µs, 519 595 B/op, 28 212 allocs | 863–874 µs, 519 609 B/op, 28 212 allocs | flat |
| Layout+paint+ANSI, 100×35 | 12.8–13.7 ms, 19.83 MB, 56 858 allocs | 12.9–13.4 ms, 19.90 MB, 56 859 allocs | flat |
| Content paint+blit | 92–100 µs, 0 B/op | 97–101 µs, 0 B/op | flat |
| `-dump`/`-png` hashes | fixed | identical | pass |

## 8. Open questions

All Phase 0 questions are resolved in the decision log: D13 (transport path),
D14 (`Session` rename), D15 (encode by default), D16 (`about:` pages) and D17
(request log). Add new questions here as they arise, and move each to §2 once
decided.

## 9. Validation per batch

```sh
go test -mod=vendor ./...
go vet -mod=vendor ./...
gofmt -l <touched files>
git diff --check
go test -race ./internal/...
go test -fuzz=FuzzProtocolDecode -fuzztime=60s ./internal/protocol
go test ./internal/browser -run TestLayoutFixtures -update   # only when intentional
```

Also compare the baseline corpus hashes for `-dump` and `-png` before handing
work back.

## 10. Completion and deletion checklist

- [ ] Every phase checked, every gate green.
- [ ] Durable rules folded into `AGENTS.md` §4 and
      `docs/agents/architecture.md`; no rule exists only here.
- [ ] Remove the routing row in `AGENTS.md` §1.
- [ ] Remove the link in `architecture.md`.
- [ ] Delete this file with `git rm`.
- [ ] One-line commit message records the completion.

## 11. Progress log

Append one entry per work session: date, phase, what landed, tests and
measurements, next step. Do not rewrite old entries.

| Date | Phase | What landed | Validation | Next |
| --- | --- | --- | --- | --- |
| 2026-11-22 | — | Plan and decisions recorded; docs updated; Pages publish guarded to `main` | `git diff --check` | Phase 0 |
| 2026-11-22 | 0 | `internal/protocol` types, ports, binary codec, round-trip/truncation/limit tests, decoder fuzz targets, import-boundary test, codec benchmark; baseline hash/timing tests and measurements | `go test ./internal/protocol`, fuzz 8 s each, `go test ./internal/browser -run TestBoundary`, `go vet` | Phase 1: frame boundary |
| 2026-11-22 | 0 | Codec hardening: 32-bit body-length guard, scaled-run scale cap, control-text rejection; full suite and race green | `go test ./...`, `go test -race ./internal/protocol`, fuzz 8 s each | Phase 1: frame boundary |
| 2026-11-22 | 1 | Content-rect painting with reused screen/content grids; `Cell.Raw` replaced by structured runs; `Cell` 56→24 B; terminal detection moved to `term_detect.go`; `toProtocolFrame` mapping and round-trip test; content-blit equivalence, cell-size guard; draw and content-frame benchmarks | `go test ./...`, `go vet`, `go test -race` on the frame tests, baseline hashes | Phase 2: chrome and state ownership |
| 2026-11-22 | 2 | Shell geometry and `State` snapshot in `shell.go`; `drawChrome` is composition-only and `flushGrid` is output-only; `paintShellOverlays` covers content and console; `handleShellKey`/`handlePageKey` split; tests for content rect, key routing, state flags and console modals | `go test ./...`, `go vet`, `-race` on shell/chrome/frame tests, baseline hashes, draw benchmarks | Phase 3: extract `internal/engine` |
| 2026-11-22 | 3a | `internal/frontend` with ANSI/PNG, protocol-only; browser bridges; reusable draw-path frame buffer; frontend tests; import gate now covers the package | `go test ./...`, `go vet`, `-race` on frontend/frame tests, dump/PNG hashes, draw benchmarks | Phase 3b: engine package |
| 2026-11-22 | 3b0 | `internal/engine` with Color, Length, Unit, calc/min/max/clamp parsing, resolution and CSS split helpers; browser aliases/wrappers; engine import gate active; all tests and hashes still green | `go test ./...`, `go vet`, dump/PNG hashes, draw and layout benchmarks | Phase 3b1: Node and Style |
| 2026-11-22 | 3b1 | HTML/Node, Style, sheets, selectors, cascade, media and UA moved to `internal/engine`; engine tests moved; layout-dependent tests stayed; browser compat/page-style bridges; all hashes and tests green | `go test ./...`, `go vet`, `-race` on style/layout, dump/PNG hashes, draw and layout benchmarks | Phase 3b2: layout |
| 2026-11-22 | 3b2 | All layout files and the dark palette moved to `internal/engine`; browser layout compat aliases/builders/utilities/form predicates; js_dom and selection use exported accessors; all tests and hashes green | `go test ./...`, `go vet`, `-race` on layout/frame, dump/PNG hashes, draw and layout benchmarks | Phase 3b3: paint and the content frame |
| 2026-11-22 | 3b3 | Grid, paint, `PaintLayout*`, `ImagePlacement` and SVG decode moved to `internal/engine`; gfx/terminal placement and inline-SVG loading stay in `render_gfx.go`; paint compat aliases; all tests and hashes green | `go test ./...`, `go vet`, `-race` on paint/layout/frame, dump/PNG hashes, draw and layout benchmarks | Phase 3b4: JS/DOM and session |
| 2026-11-22 | 3b4a | PDF object lexer, text extractor and document assembly moved to `internal/engine`; browser `pdf_compat.go` and `Loader.loadPDFPage` remain; tests and hashes green | `go test ./...`, `go vet`, `-race` on PDF/frame, dump/PNG hashes | Next: session/host split before JS |
| 2026-11-22 | 4a | `jsRuntime` now holds `jsHost`, not `*Browser`; adapter covers viewport/layout/scroll/focus/hit-testing/dialogs/status/forms/cookies/fetch/requests/import queue; tests and hashes green | `go test ./...`, `go vet`, `-race` on JS/console/frame, dump/PNG hashes, draw benchmarks | Move the host contract and JS to engine |
| 2026-11-22 | 4a-2 | `JSHost`/`ImportTask` moved to `internal/engine`; `Page`/`Script` and page style collection moved; browser adapter remains; tests and hashes green | `go test ./...`, `go vet`, `-race` on JS/page/frame, dump/PNG hashes | Move `js*.go` and its tests |
| 2026-11-22 | 4a-3 | `JSHost` gained request-context/decode/MIME brokering; `NearestForm`/`IsFileInput` moved to the engine; JS file move deferred after finding the lifecycle/test coupling | `go test ./...`, `go vet`, `-race` on JS/form/frame, dump/PNG hashes | Split the JS event loop and tests before moving `js*.go` |
| 2026-11-22 | 4a-4 | `js*.go` moved to `internal/engine`; browser keeps `js_host.go`, `js_life.go` and `js_compat.go`; pure runtime/module tests moved to the engine; all tests and hashes green | `go test ./...`, `go vet`, `-race` on JS/module/frame, dump/PNG hashes, draw benchmarks | 4b: host loader/HTTP/cookies split |
| 2026-11-22 | 4b-1 | Session resource cache and cookie jar moved to `internal/host`; browser shims for Loader/tests; all tests and hashes green | `go test ./...`, `go vet`, `-race` on cookie/cache/frame, dump/PNG hashes | 4b-2: HTTP/TLS/settings |
| 2026-11-22 | 4b-2 | Console request log moved to `internal/host`; Loader delegates; tests and hashes green | `go test ./...`, `go vet`, `-race` on request/cache/frame, dump/PNG hashes | 4b-3: HTTP/TLS/settings |
| 2026-11-22 | 4b-3 | User agent, search template and image caps moved to `host.Settings`; Loader delegates and tests use accessors; all green | `go test ./...`, `go vet`, `-race` on config/image/frame, dump/PNG hashes | 4b-4: TLS/HTTP transport |
| 2026-11-22 | 4b-4 | Cert source selection, embedded CA pool and `CertTransport` moved to `internal/host`; browser keeps transport construction/dial guard; all tests and hashes green | `go test ./...`, `go vet`, `-race` on cert/config/frame, dump/PNG hashes | 4b-5: HTTP client/dial guard |
