# Boundary migration (B)

**Temporary working document.** It tracks the split of the single `browser`
package into a content engine, a trusted host, a terminal front end and a
protocol seam. Update it as work lands: tick items, record measurements, and
append decisions to the log. Delete it when the migration is complete, after
folding every durable rule into `AGENTS.md` and
[architecture.md](architecture.md). The routing row in `AGENTS.md` and the link
in `architecture.md` are removed in the same change.

Status: **Rewrite incomplete.** The protocol seam, host broker, engine content,
terminal front end, live session loop, app dispatch and the interactive chrome
surfaces (normal header/footer, scrollbar, prompts, find, menus, dialogs,
select, download/notice, hints, the console and the session history list) are
landed, and the front end renders the displayed URL/title from validated
history commits. A focused page form still sends keys through the engine, but
the front end keeps the chrome. Remaining: host-attested final URLs, the
`Browser` → `Session` split, direct app wiring and Phase 6 cleanup.
Branch: `core-rewrite`.
Last updated: 2026-11-23 (audited against the code).

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
| D8 | 2026-11-22 | Ownership: engine = DOM/JS/CSS/layout/paint/PDF/images/find/selection/hints/reader; host = HTTP/TLS/certs/cookies/cache/settings/request log; front end = terminal/input/chrome/ANSI/native images/PNG/clipboard. The history/downloads part is superseded by D54. | See the package table in [architecture.md](architecture.md#target-boundary). |
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
| D35 | 2026-11-22 | Move request metadata, local-network/mixed-content guards, dialers, `NewHTTPTransport` and `NewHTTPClient` into `internal/host`; browser keeps thin wrappers for fetch/tests. | These are host network policy and policy enforcement; the engine only needs brokered requests. The remaining load/parse split is the next step. |
| D36 | 2026-11-22 | Move the raw request path (`Fetcher`, data URLs, size/encoding checks, cache and request accounting) into `internal/host`. `Loader.fetchCtx/fetchRequest` are delegates that sync the host client/cache/settings/log before each call. Page assembly and parsing remain in the browser for the next slice. | Fetching is host policy and I/O; parsing is engine work. The delegate keeps test-injected clients working while the assembly split is prepared. |
| D37 | 2026-11-22 | Move HTML page assembly and stylesheet/script/import-map collection into `internal/engine` behind `PageFetcher`; the browser adapter brokers each subresource through the host fetch path. Image/SVG loading, about/PDF/blocked orchestration stay in the Loader for now. | The engine now owns parsing and document assembly, and every subresource crosses a brokered interface. Remaining Loader code is orchestration/policy, not the engine. |
| D38 | 2026-11-22 | Define `protocol.Handler` and `protocol.Transport`, and implement `protocol.InProc`: each session runs the handler on its own goroutine, and every command and event is encoded and decoded through the fixed codec before delivery. Abort is an out-of-band one-shot channel; an event that fails validation is replaced by `Crash`. | This is the first Phase 5 slice: the seam becomes executable before the browser loop moves onto it, and the codec is on the real path rather than only in tests. |
| D39 | 2026-11-22 | Run the live interactive loop through `protocol.InProc`: the engine `Browser` handler owns its own goroutine and the page loop; the front end owns terminal raw mode, key reading, resize signals and the quit signal. `protocol.Quit` tells the front end to exit. Modal JS dialogs read commands from the session stream while the loop is blocked. `protocol.Aborter` lets `Session.Abort` count the request directly on the front-end goroutine, so a busy script still stops before the loop wakes. Presentation moved in D42; chrome remains engine-composed until the shell split. | This is the real lifecycle seam: commands, events, abort and shutdown run through the transport on every interactive session, while keeping the existing frame path byte-identical. D42 superseded the presentation part. |
| D40 | 2026-11-22 | `Loader` takes a fresh `host.Fetcher` snapshot per request instead of mutating one shared value. A test or CLI can still replace the loader's client, settings, cache and request log, because the snapshot is taken at call time. | The shared fetcher raced under concurrent navigations and would be unsafe once the engine services loads on its own goroutine; the snapshot removes the shared write without changing the Loader API. |
| D41 | 2026-11-22 | Replace the handler's `chan<- Event` with `protocol.EventSink`. `Send` validates and detaches the event before returning, so the engine may immediately reuse its paint buffers. Frames use a validated copy; every other command and event keeps the wire codec. | The channel handoff left events referencing reusable engine memory, which was unsafe across the goroutine boundary. Measurement showed the wire codec costs ~320 µs per frame, so D15's measurement escape applies to the hot frame path; the semantic checks are unchanged. |
| D42 | 2026-11-22 | Text-mode presentation moves to `frontend.Presenter`: the engine emits a full-screen `Frame`, the front end encodes ANSI, keeps the row diff and writes the terminal. Native image modes keep the engine-side writer until image placements cross the seam. `StateNativeImages` tells the front end which side owns the current frame. Chrome composition stays in the engine for now. | This removes stdout from the engine for the default graphics modes while preserving kitty/iTerm2 output exactly. Chrome ownership and placement encoding are the remaining presentation steps. |
| D43 | 2026-11-22 | Move terminal graphics to `internal/frontend/gfx.go`. The engine encodes each image once into an immutable PNG blob, caches it under a stable placement key, and puts `protocol.Placement` values in the frame; the front end decodes, scales, crops and writes kitty/iTerm2 escapes. Placement bytes are shared between frames. The engine keeps only mode/capability state (`GraphicsMode`, `Mode`, `TextSizing`) for layout and the graphics cycle. | Native output is terminal work and belongs with the presenter. The bounded encode cache keeps the per-frame cost to a slice copy; the PTY checks cover kitty, iTerm2, blocks and cycling from blocks into native mode. Chrome composition is the last presentation step. |
| D44 | 2026-11-22 | The front end presents every frame, including frames with native placements. `StateNativeImages` selects image output; it does not hand text presentation back to the engine. | The first 5d wiring skipped `Present` whenever the native flag was set, so entering kitty/iTerm2 mode emitted a valid frame that was never written and the screen appeared blank. The cycle still contained the native mode, which made it look as if kitty was not recognised. A regression test now drives a native `State` plus `Frame` through the event handler. |
| D45 | 2026-11-22 | A `Crash` is reported once per distinct consecutive message: the front end sanitizes it, writes it to stderr once, and sends a `ClientError` command back to the engine. The engine adds `[engine] ...` to the `!` console and sets `Error: page engine — see ! console` in the footer. A successfully presented frame re-arms reporting. | A contained failure must be discoverable without a log file and without flooding the alternate screen on every repaint. The console is engine-owned until chrome moves, so a small command carries the sanitized message back rather than the front end drawing its own overlay. |
| D46 | 2026-11-22 | If a frame fails validation, `InProc` sends `Crash` and then a best-effort repaired frame: invalid placements are dropped, unsafe glyphs become spaces, bad scaled runs are removed and the result is re-validated. The front end resets its row diff on `Crash`, so the repaired frame is a full repaint. Stderr keeps one short line; the full message goes to the console. | A contained frame error must not leave the alternate screen unusable. Repairing only the recoverable parts preserves the boundary (the result still passes `Validate`) while keeping the toolbar, footer and `!` console reachable. |
| D47 | 2026-11-22 | Every composed screen goes through `presentGrid`, including the console and its find bar. Direct `flushGrid` output remains only for non-session tests and automation. | The console used to write stdout itself while page frames went through the presenter, leaving the presenter's row cache stale; closing the console then diffed against the old page and left a black screen. One presentation path keeps the cache truthful. |
| D48 | 2026-11-22 | `-dump` and `-png` run through `protocol.InProc` with a one-shot `dumpHandler`. The handler paints the page once; tall text dumps are split into 512-row frames after painting (preserving fixed/sticky placement and image crops), and PNG uses one frame up to the existing 2000-row cap. The front end writes the output. | This satisfies D13 for the non-interactive render paths without changing a byte of output, and exercises the transport from CLI tests. MCP and automation still call the engine directly. |
| D49 | 2026-11-22 | MCP runs through `protocol.InProc`: an `automationHandler` owns the `Automation` engine on its goroutine, and `automationClient` implements the `RunMCP` surface using `Automation`/`AutomationResult` commands. The command carries an operation, a URL or tool name, and bounded JSON arguments; results carry text or PNG bytes. | This completes the production MCP path without rewriting the 2,000-line automation engine. In a later child split the handler moves with the engine and the client stays in the front end. The automation library remains the engine-side implementation and its tests still call it directly. |
| D50 | 2026-11-22 | `InProc` delivers events through a bounded outbox. A newer `Frame` or `State` replaces the pending one, one-shot events keep their order, and `Quit`/`Crash` discard stale coalescible events first. The handler blocks only when the outbox is full of one-shot events. | A slow presenter must not stall the engine’s timers and loads on frames it will never show. Coalescing keeps the newest picture and state, while `Quit` and `Crash` are never delayed behind stale frames. |
| D51 | 2026-11-22 | Add `protocol.Chrome`: normal page frames are content-only and are accompanied by the header/footer cell rows, which the front end stacks. Overlays and the console keep the existing full-screen frame with `Chrome.Show=false`. The engine still composes the chrome cells for now; semantic chrome ownership is the next slice. | This makes the normal frame contract content-only and moves ANSI stacking to the front end without losing overlays. It is a display-list bridge, not the final trust boundary: the engine can still choose chrome cells until the front end composes them from `State`. |
| D52 | 2026-11-22 | Add a stream transport behind the same codec and ports: `ReadCommand`/`ReadEvent`, `ServeStream` for the handler child and `DialStream` for the front end. `ServeStream` uses the same coalescing outbox and event validation. The serving reader applies `Abort` directly, so a busy script stops even when the command loop is blocked. | This is the transport swap the later process split needs. It is tested over `net.Pipe` and is unused by the in-process app until the child process lands. The serving side owns and closes its connection when the session ends. |
| D53 | 2026-11-22 | Move JS `fetch`/XHR to `host.JSFetch` behind a `protocol.FetchRequest`/`FetchResponse`, and move `document.cookie` parsing to the browser adapter. The host owns same-origin/CORS, credentials, redirects, mixed content, private networks, size limits and the request log. The engine contract now exposes a cookie value list, a raw cookie setter and `JSFetch`, with no `*http.Client`. | This closes the largest remaining boundary violation: page scripts can no longer choose a destination or see host cookie types directly. The dependency test now rejects `net/http` (and subpackages) on the engine side. |
| D54 | 2026-11-22 | Session back/forward history and `about:history` stay session-owned. The download popup and its `curl` hint stay front-end presentation. The host keeps no global visited list and no download history. | User decision: the proposed host-owned global history/download records would add state and memory without a user-visible benefit, and MOB³ persists nothing. This keeps B behavior-neutral and corrects D8's over-broad ownership claim. |
| D55 | 2026-11-22 | Move the built-in page shell, shared CSS and cannot-display/load-error HTML to `internal/engine/pages.go`; the browser keeps thin wrappers. Help/history/config page bodies, the config token and view options remain session-side until the app split. The about-page part is superseded by D56. | This is a behavior-neutral move of the render templates to the engine. The remaining about-page work depended on host-supplied data and the config token, which D56 moved. |
| D56 | 2026-11-22 | Move start/help/history/blocked and config page rendering to the engine, and move the one-shot config form token into `internal/host.ConfigToken`. The session still collects history/settings data and applies submitted values. | This completes the about-page ownership part of D16: templates live with the engine, and the token is minted/validated on the trusted side. The remaining session code is data collection and applying settings, which belongs with the app split. |
| D57 | 2026-11-22 | Move terminal raw mode, size/cell-pixel probes, key/mouse decoding, capability detection and resize/quit signal wiring into `internal/frontend`. Mouse event/token types are front-end-owned with browser aliases. Clipboard OSC 52 encoding moved to the front end; the session emits `Clipboard` events when a presentation session exists and keeps the writer fallback for tests/automation. The live-loop part is superseded by D58. | These are terminal concerns, not page content. The interactive loop moved to `frontend.SessionLoop` in D58; the terminal API stays front-end-owned. |
| D58 | 2026-11-22 | Create `internal/app` and move flag parsing, validation and dispatch there. `browser.Execute(RunOptions)` keeps session setup until the session type moves. Move the live session event loop to `frontend.SessionLoop`; crash dedup, chrome state and clipboard handling now live there. | This gives the app a real wiring layer and removes the event loop from the browser CLI. The remaining app step is moving `Browser`/session construction and the session type itself. |
| D59 | 2026-11-22 | Defer the transport-level restart hook to the child-process work. `protocol` already carries the stream transport; a restart hook has no in-process caller and cannot be validated until the engine child exists. | The hook is a lifecycle concern of the later process split, not part of B's boundary. Phase 5 closes on semantic chrome ownership. |
| D60 | 2026-11-22 | The front end renders the normal header and footer from `protocol.State`; the engine keeps `Chrome` composition as the fallback for overlays, the console, the find bar and tests. Header/footer mouse hits move to the front end and send `App`/`Navigate` actions. The scrollbar, shell keyboard modes, menus, dialogs and console remain engine-side for the next batch. | This is the first chrome-ownership batch. It moves the visible normal-page chrome without changing output, but `State.URL`/`Title` still come from the engine and navigation is still engine-side, so this is display ownership, not the final trust boundary. |
| D61 | 2026-11-22 | Move base shell keys, local quit confirmation and the scrollbar to the front end. `State.KeysEngine` tells the front end when the engine owns key interpretation (prompt, find, menus, console, dialog, form field or hints); otherwise the front end maps app keys to `App`/`Navigate` and keeps page movement keys on the engine path. The quit prompt is local and confirms with `y`/`Y`/Enter. The scrollbar is drawn from `Scroll`/`MaxScroll` and drag-tested in the last content column. Prompt editing, find, menus, console, dialogs, navigation state and the engine fallback remain. | This closes the base keyboard and scrollbar part of Phase 2 without duplicating the line editor or the modal state machines. `KeysEngine` is still engine-provided; the final trust boundary needs front-end-owned navigation and dialogs. |
| D62 | 2026-11-22 | Move the URL/vim prompt state, line editing, recall history and mouse caret to the front end. `o`/`ctrl-l` and `:` open a local prompt seeded with the current URL and select-all; Enter sends `Navigate`, Esc cancels, `:q` exits, and prompt history is front-end-local. The engine prompt remains for direct `HandleKey` callers, automation and tests; `AppOpenURL`/`AppOpenVimPrompt` stay for those paths. | This removes the last text-entry mode from the engine's interactive key path, using the same line-editing rules copied for the prompt. Find, menus, console, dialogs, navigation state and the engine fallback remain. |
| D63 | 2026-11-22 | Move the page find bar, query, caret and result count to the front end. `/` opens a local bar; edits send `protocol.Find{Query}` and navigation sends `Next`/`Prev`; the engine computes matches, scrolls and paints highlights, then answers with `protocol.FindResults`. Enter/Esc close the bar but leave matches for `n`/`N`; the next base Esc clears them. Console HTML find stays engine-owned because the console is still an engine modal. | Find is the last base text-entry mode. Keeping match computation in the engine preserves layout-aware highlights while the front end owns the visible bar and input. Menus, dialogs, console and navigation state remain. |
| D64 | 2026-11-22 | Move the view and zoom windows to the front end. `X`/`Z` and the zoom badge open local windows; the front end draws the full-screen dimmed box from `State`, handles keyboard and mouse selection, and sends `AppToggleFixed`/`Sticky`/`Backgrounds` or `AppZoomSet`. The engine retains its menu code for direct callers, automation and the fallback. | This is the first modal overlay the front end composes over the full screen (header, content and footer). The select menu, dialogs and console still need page/JS/console data, so they stay engine-owned for now. |
| D65 | 2026-11-22 | Move JavaScript alert/confirm/prompt dialogs to the front end. The engine sends `Dialog` with an ID and blocks in `runTransportDialog`; the front end draws the modal, edits the prompt with the shared line editor, and answers with `DialogResult`. The engine keeps the direct-key dialog path for tests and automation. | This closes the dialog request/response seam and removes the last JS-driven modal input from the engine's interactive path. The select menu and console remain engine-owned. |
| D66 | 2026-11-22 | Move the page select popup to the front end. The engine sends `Select` with option labels and the selected index; the front end draws, navigates and answers with `SelectResult`; the engine commits the option and fires the change events. The engine keeps the old popup path for tests and automation and excludes the pending select from `overlayOpen` while the front end owns it. | This removes the last form-control popup from the engine's interactive drawing path. The console remains the last large engine-owned UI. |
| D67 | 2026-11-22 | Move download and notice modals to the front end. The engine sends `Download` (with the curl command) or `Notice`; the front end draws the modal, copies the command with OSC 52, and answers with `ModalClose`. The engine keeps its modal code for direct callers, tests and automation and excludes front-end-owned modals from `overlayOpen`. | Clipboard and modal presentation are terminal/front-end work. The console and link-hint overlay remain the last engine-owned UI pieces. |
| D68 | 2026-11-22 | Move the link-hint overlay and its prefix/confirmation input to the front end. The engine computes targets, labels, glyph cells and hrefs, sends `Hints`, and follows the chosen target on `HintResult`. The front end paints the highlighted letters/fallback labels, narrows by prefix, shows the F destination confirmation and answers. The engine keeps its hint key path for direct callers and tests. | Hints are transient presentation and input; keeping target discovery/selection in the engine preserves layout-aware labels. The console is the last large engine-owned UI. |
| D69 | 2026-11-22 | Move console **composition** to the front end. The engine sends `ConsoleView` with tabs, the active pane's visible lines, request summary, input row/cursor and footer; the front end draws the full-screen console and overlays dialogs/select/download/notice. Console key input, scrolling, tab selection, input editing and HTML find remain engine-side for the next batch, which still sends a fresh snapshot after each change. Engine drawing remains for the console HTML find fallback. | This is a display-only console batch: it moves the last large cell composition while keeping the state machine stable. The next batch moves console input and removes the engine console renderer. |
| D70 | 2026-11-22 | Move console input, tab switching, scrolling, input editing/history, copy and quit to the front end. `ConsoleAction` carries pane/cycle/scroll/top/bottom/focus/cancel/input/copy/quit/cancel-quit/close; the engine applies it and sends a fresh `ConsoleView`. Mouse tabs/wheel remain engine-routed keys. Console HTML find still uses the engine fallback and keys (`/`, `n`, `N`) are forwarded there; a normal frame clears the front-end console-active state and a new `ConsoleView` restores it. | This closes the console state machine except its find mode, which needs layout-aware match highlighting. The last remaining architecture work is navigation/ownership and the session split. |
| D71 | 2026-11-22 | Move console HTML find to the front end. `Find` gains a page/HTML scope; the engine computes HTML matches and sends visible highlight rectangles plus counts in `ConsoleView`; the front end owns the find bar, query/caret, next/previous and close. The engine keeps the old console find path for direct callers and tests. | The console is now entirely front-end-owned for interactive presentation and input; the engine only computes layout-aware HTML matches and highlights. Navigation state and the session split remain. |
| D72 | 2026-11-23 | Move session-history ownership to the front end while the engine keeps the memory-only page cache. The front end owns the ordered entry list, the current index and scroll offsets. It sends a bounded `History` snapshot (entries plus current) before opening `about:history`, and the engine renders it with the existing `HistoryPageHTML`/`AppPageHTML` shell. A dedicated `History` command keeps `Navigate` generic and lets reload or a typed `about:history` reuse the latest snapshot. The engine page cache is keyed by the front-end entry ID, and back/forward becomes a restore request. `State.URL` remains engine-produced in this batch; trusted URL ownership stays with the session split. | User decision: engine-rendered history preserves the normal page model (layout, find, selection, dark mode and dump/PNG/MCP parity) and matches the config/help pattern. Front-end ownership keeps D2/D54; the engine receives only display data and page-cache keys. |
| D73 | 2026-11-23 | Front end owns the displayed URL/title. `HistoryCommit` gains `Title`; the front end validates the URL scheme/length, stores the title/URL on the history entry and substitutes that entry for `State.URL/Title` in normal chrome. Dynamic title updates are accepted only while the engine reports the committed URL. The engine still reports the final URL; host-attested final URLs and the process split remain. | This is a small ownership step without a navigation-approval round trip. It removes engine `State` from the address bar and rejects unsupported schemes, while acknowledging that a compromised engine can still misreport the final URL until the host/process boundary exists. |
| D74 | 2026-11-23 | Keep front-end chrome active while a page form owns the keyboard. `State.KeysEngine` still routes keys through the engine, but `State.FrontendChrome` no longer turns false for form focus; the front end composes the header, footer and scrollbar. The engine fallback remains for direct callers and tests. | This closes the last live-session path where the engine emitted chrome. Form editing is still engine-side, so the session split still moves it, but it can no longer reach the trusted chrome rows. |

Append new decisions with the next ID. Do not rewrite an earlier row; supersede
it with a new one and mark the old one "superseded by Dn".

## 3. Target architecture

| Package | Owns |
| --- | --- |
| `internal/protocol` | Message types, codec, `Session`/`Broker` ports, limits, version. Standard library only. |
| `internal/engine` | HTML/CSS, style, layout, paint into a content-rect frame, sobek and the DOM, PDF text, image/SVG decoding, find, selection, hints, reader, dark palette. |
| `internal/host` | HTTP/S, TLS/certificates, cookie jar, resource cache, settings, request log. Session history and the download hint stay session/front-end (D54). |
| `internal/frontend` | Terminal raw mode and input, chrome, dialogs, session history, ANSI encoding, kitty/iTerm2 placement, PNG output, clipboard. |
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

- `app` still imports `internal/browser` for the session type and its
  non-interactive render/MCP helpers; the dependency test therefore allows any
  project import from `app`. This ends with the session split.
- None for `net/http`: JS `fetch`/XHR and `document.cookie` now go through the
  host broker. Terminal capability detection is front-end-owned.

During the migration the old `browser` package may import a moved package and
alias exported types (`type Page = engine.Page`) so each batch lands with
tests green. No new reach-through.

### File ownership

The canonical target and current file tables are in
[architecture.md](architecture.md#current-layout-during-migration). This table
tracks only the files that still have to cross the seam and the temporary
browser-side bridges. "Split" means one file becomes two declarations at the
seam; the note names which part goes where.

| Current file(s) | Final side | Remaining work |
| --- | --- | --- |
| `internal/app/app.go`, `internal/app/execute.go` | app + frontend | `app` owns flags/validation, the build `Version`, mode dispatch and the interactive boot. It still calls `browser.NewSession`/`NewConfiguredLoader`/`Dump`/`RunMCPBoot`; direct engine/host/frontend construction follows the session split. |
| `browser.go`, `shell.go`, `session.go` | engine + frontend | Page/content state, load lifecycle and command routing are engine. Content-rect geometry, navigation and shell state are frontend. `Browser` becomes `Session`. |
| `browser_chrome.go`, `ui.go`, `find.go`, `hints.go`, `browser_input.go` | frontend + engine | Normal header/footer drawing, chrome mouse routing, scrollbar, base shell keys, quit, URL/vim prompt, page find, view/zoom windows, JS dialogs, the select popup, download/notice modals, link hints, console composition/input/find are frontend (`frontend/chrome.go`, `frontend/session.go`, `frontend/prompt.go`, `frontend/menu.go`, `frontend/dialog.go`, `frontend/select.go`, `frontend/download.go`, `frontend/hints.go`, `frontend/console.go`). Engine fallback modals/hints/console drawing still live here; engine find match computation/highlights, page hit testing, form editing and hint target computation remain. |
| `browser_history.go`, `browser_pdf.go` | frontend | Back/forward list, scroll restore, `about:history` data and the download/curl hint are session/front-end state (D54). The page cache is memory-only and stays with the session. |
| `browser_forms.go` | engine + session | Constraint validation moved to `internal/engine/validate.go`; Tab order, focus, editing, range/select stepping and submission remain session-side. |
| `browser_selection.go` | engine wrapper | Selection range, extraction and painting moved to `internal/engine/selection.go`; the browser keeps drag state and clipboard policy. |
| `hints.go` | engine + session | Glyph types, label assignment, unique-pair shortening and base-26 encoding moved to `internal/engine/hints.go`; target collection, painting, activation and confirmation remain session-side. |
| `find.go` | engine + session | Match streams, whitespace normalisation and match rectangles moved to `internal/engine/find.go`; find state, navigation, painting and front-end messages remain session-side. |
| `reader.go` | done | Extraction and `ReaderRules` moved to `internal/engine/reader.go`; the browser file is a thin alias. |
| `memory.go` | host + app + engine | Process limit and budget policy are host/app; per-session enforcement is engine. |
| `fetch.go`, `browser_images.go` | engine + host | Host keeps fetch, cache, cookies, policy and accounting. Move image/SVG/about/PDF orchestration into the engine session. |
| `placement.go` | engine | Image placement encoding and the bounded immutable PNG cache; it feeds the frame contract. |
| `dump_transport.go`, `automation_transport.go` | app | One-shot dump/PNG handler and MCP client; no engine state. |
| `automation.go` | engine | Headless automation is the engine-side implementation. |
| `mcp.go` | app/frontend | MCP JSON-RPC client; consumes the transport, not engine internals. |
| `config.go` | app + engine | Host token, engine form markup and the app/session apply path. |
| `engine_compat.go`, `layout_compat.go`, `paint_compat.go`, `frontend_bridge.go`, `frame_protocol.go`, `js_compat.go`, `page_compat.go`, `pdf_compat.go`, `style_units_compat.go`, `cookie_compat.go`, `network_compat.go`, `render_gfx.go`, `js_host.go`, `js_life.go` | delete/merge | Aliases, adapters and thin wrappers. `frame_protocol.go` becomes an engine-side frame mapper; `render_gfx.go` keeps only inline-SVG loading until that moves. |
| `internal/engine/*`, `internal/host/*`, `internal/frontend/*`, `internal/protocol/*`, `dark.go`, `js*.go` | done | Already on their final side; responsibilities are in [architecture.md](architecture.md#current-layout-during-migration). |

## 4. Phases

Estimates are for one developer who knows the codebase, Linux first, ±40%.
Total: **9–13 weeks**. Phase 3 dominates the risk.

| Phase | Scope | Estimate | Status | Exit gate |
| --- | --- | --- | --- | --- |
| 0 | Contract, codec, dependency test, baseline | 1 week | Done | Protocol compiles; baseline recorded |
| 1 | Frame boundary and paint split | 1–2 weeks | Done | `-dump`/`-png` byte-identical; paint path has no ANSI |
| 2 | Chrome and state ownership | 2–3 weeks | Partial | The `State` snapshot, shell-loop split, every interactive chrome surface, the history list, content geometry and the displayed URL/title ledger are front-end-owned; host-attested final URLs remain |
| 3 | Extract `internal/engine` | 3–4 weeks | Partial | DOM/style/layout/paint/PDF/JS moved; the session split and `Browser` → `Session` rename remain |
| 4 | Extract `internal/host` and broker | 2–3 weeks | Done | Transport/cache/cookies/settings/raw fetch/JS fetch/XHR, CORS, config token and about-page rendering are split; engine cannot open sockets or parse cookies. Loader orchestration moves with 3c. |
| 5 | Transport and lifecycle | 1–2 weeks | Partial | Interactive, dump, PNG, MCP, native images, coalescing and stream transport are done; interactive chrome ownership is done apart from the form-input fallback. The restart hook is deferred to the child-process work (D59). |
| 6 | Verification, docs, cleanup | 1 week | Not started | Gates below hold; aliases removed |

\* Phase 1 is functionally complete. The `Lines`/gfx file move is folded into
Phase 3 under D19.

\*\* Phase 2 is partial only for host-attested final URLs. Every interactive
chrome surface, overlay, dialog, hint, console, find bar, the session history
list, content geometry and the displayed URL/title ledger are front-end-owned;
the engine keeps its fallback composition only for direct callers and tests.
`protocol.Dialog`/`DialogResult` are wired in the interactive path.

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
- [x] Idle-frame count: measured after the live loop landed (0 ticks over 5 s on an idle help page).
- [x] Add the routing row and architecture link for this file.
- [x] Resolve the Phase 0 open questions in §8.

### Phase 1 — Frame boundary

- [x] Engine paints into a content-rect frame, not the full-screen `Grid`.
      `draw` paints the page viewport at row 0, then blits it below the chrome;
      `toProtocolFrame` maps the result to the wire contract.
- [x] Replace `Cell.Raw` with structured scaled-text runs; `Cell` is now plain
      data and shrank from 56 to 24 bytes.
- [x] Move `Grid.Lines`/`LinesMono`, gfx and kitty/iTerm2 placement out of
      `render.go`; ANSI/PNG and native placement now live in `internal/frontend`.
- [x] Move terminal capability detection out of `render.go` to
      `internal/frontend/term_detect.go`; the paint path no longer imports
      `os`.
- [x] Reuse two frame buffers. `BenchmarkBoundaryContentFrame` is 0 B/op;
      ANSI string building still allocates as before.
- [x] Keep the row diff in the front end (`frontend.Presenter`); `flushGrid`
      remains only for tests and automation.
- [x] Frame tests: content-blit equivalence, protocol round-trip, cell-size
      guard, dump/PNG hashes.

### Phase 2 — Chrome and state ownership

- [x] Front end draws the normal header and footer from `protocol.State`.
      The engine keeps `Chrome` composition as the fallback for direct callers,
      tests.
- [x] Header/footer and scrollbar mouse are front-end-owned and send
      `App`/`Navigate`/`Scroll` actions. Page-area mouse remains engine-side.
- [x] Prompts, notices, dialogs, console, menus and the find bar are
      front-end-owned; the engine keeps fallback rendering for direct callers
      and tests.
- [x] The `State` snapshot exists (`stateMessage`), is emitted by `Serve`, and
      is consumed by `frontend.SessionLoop`.
- [x] The front end computes the content top/height (`ContentBounds`) and
      sends it in `Init`/`Resize`; the engine uses the supplied rectangle and
      keeps the legacy derivation only for direct callers and tests. Terminal
      width/height still travel for the engine fallback screen until the
      session split.
- [x] Base shell keys live in `frontend.SessionLoop`: quit confirmation, app
      actions, help/history and mode toggles. `State.KeysEngine` keeps keys on
      the engine path while a prompt, find, menu, console, dialog, form field
      or hint is active.
- [x] The scrollbar is drawn and drag-tested by the front end from
      `Scroll`/`MaxScroll`; the engine keeps its scrollbar only for the
      fallback path and tests.
- [x] URL/vim prompt state, line editing, history and mouse caret are
      front-end-owned (`frontend/prompt.go`). The engine prompt remains for
      automation/tests and the fallback.
- [x] The page find bar, query, caret and result count are front-end-owned;
      the engine computes matches, scrolls and paints highlights through
      `Find`/`FindResults`. Console HTML find has the separate `FindHTML` scope
      and the same front-end bar.
- [x] View and zoom windows are front-end-owned, including the full-screen
      dimmed overlay, keyboard/mouse selection and toggle/zoom actions.
- [x] The page select popup is front-end-owned through
      `Select`/`SelectResult`; the engine commits the chosen option.
- [x] Download and notice modals are front-end-owned; the curl command is
      copied with OSC 52 and the engine closes the modal on `ModalClose`.
- [x] The link-hint overlay and prefix/confirmation input are front-end-owned
      through `Hints`/`HintResult`; target discovery and following stay in the
      engine.
- [x] The console view, input, tab switching, scrolling, input history, copy
      and quit are front-end-owned through `ConsoleView`/`ConsoleAction`.
- [x] Console HTML find is front-end-owned: the engine sends match counts and
      visible highlight rectangles in `ConsoleView`; the front end owns the
      bar, query, caret and next/previous.
- [x] The console is fully front-end-owned for interactive presentation and
      input; engine console drawing remains only for direct callers/tests.
- [x] JS dialogs are request/response messages: the engine sends `Dialog` and
      blocks, the front end draws/edits and answers with `DialogResult`. The
      direct-key path remains for tests and automation.
- [~] The chrome contract carries geometry, footer text/level/hits, PDF hint,
      view/zoom menu state and the fallback flags. Selection stays engine-side
      and crosses only as clipboard text; image progress is part of the footer
      text, so no extra state message is needed.
- [x] A focused page form still routes keys through the engine, but
      `State.FrontendChrome` stays true; the front end composes the header,
      footer and scrollbar. The engine no longer emits chrome in a live session.
- [x] Back/forward history, scroll restore and `about:history` data moved to
      the front end (D54/D72). The front end owns the list/index/scroll and
      sends a bounded `History` snapshot; the engine keys its page cache by
      entry ID and emits `HistoryCommit`. The download/curl hint is front-end
      presentation; the engine still builds the command.
- [x] The front end owns the displayed URL/title: `HistoryCommit` carries the
      final title, the front end validates the URL scheme/length and replaces
      `State.URL/Title` with the committed history entry for chrome. Dynamic
      title updates are accepted only while the engine reports the committed
      URL.
- [ ] Host-attested final URLs remain open: a compromised engine can still
      report a different committed URL than the host fetched. Close this with
      the host/process split.
- [x] Content-rectangle geometry moved to the front end (`frontend.ContentBounds`
      → `Init`/`Resize.ContentTop/ContentHeight`); the engine no longer derives
      chrome rows for the interactive path.
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

### Phase 4a — Session/host interfaces (done)

- [x] `jsHost` interface and `browserJSHost` adapter; `jsRuntime` holds the
      interface, not `*Browser`.
- [x] Viewport, layout, scroll, focus, hit testing, dialogs, status, console,
      form submission and upload notice are methods.
- [x] Cookie reads/writes, resource fetch, request accounting, UA, HTTP client
      and the import queue are brokered through the adapter.
- [x] Move the host contract to `internal/engine`/`protocol` and remove the
      remaining browser network types from it; `JSHost` now exposes a value
      broker, not `*http.Client`.
- [x] Move `js*.go` to `internal/engine`; the adapter stays in the browser.
- [x] Split `Loader` so raw fetch, cache, cookies and policy are host-owned;
      `about:`/form/config rendering is engine-owned. Image/SVG/PDF
      orchestration remains in the Loader and moves with the engine session in
      3c.

### Phase 3b — Extract `internal/engine` (partial)

- [x] Move `Color`, `Length`, `Unit` and the CSS split helpers to
      `internal/engine`.
- [x] Move calc/min/max/clamp parsing, viewport/rem/lh resolution and font-size
      parsing.
- [x] Keep browser aliases (`style_units_compat.go`) during the move; the
      style primitives now live in the engine.
- [x] Engine import gate is active; the engine imports `protocol` and approved
      vendored modules, with no `net/http`, `net` or `syscall`.
- [x] Move `html.go` and `Node`.
- [x] Move `Style`, sheets, selectors, media queries and the UA sheet.
- [x] Move the engine tests; keep layout-dependent style tests in the browser.
- [x] Move layout and the dark palette.
- [x] Move paint and the content frame; keep native-image/terminal encoding in the browser until Phase 5.
- [x] Move the PDF parser and document model (3b4a).
- [x] Move JS/DOM (`js*.go`); the engine runtime and DOM now live in `internal/engine`.
- [ ] Split the session: page/content state to `engine.Session`, chrome and
      navigation to the front end; rename `Browser` → `Session`. Still
      `internal/browser`, 306 methods.
- [ ] Remove the browser wrappers, aliases and adapters; the ownership table
      lists them.

### Phase 3 — Extract `internal/engine`

- [x] 3a: HTML, style, layout, paint, PDF and dark moved, with aliases.
- [x] Reader extraction and its stylesheet moved to `internal/engine/reader.go`.
- [x] Selection range, extraction and painting moved to
      `internal/engine/selection.go`.
- [x] Hint label assignment, glyph types and encoding moved to
      `internal/engine/hints.go`; target collection, painting and activation
      remain session-side.
- [x] Find match streams and highlight rectangles moved to
      `internal/engine/find.go`; find navigation, painting and messages remain
      session-side.
- [x] Form constraint validation moved to `internal/engine/validate.go`;
      form control state, editing and submission remain session-side.
- [x] 3b: JS, DOM, events, compat, stubs, modules, web and Intl moved.
- [ ] 3c: split `Browser` at the seam (page state to the engine, chrome and
      navigation to the front end), then rename to `Session`.
- [ ] Move the remaining whole-app tests to seam tests in `app`.
- [ ] Remove aliases and bridges.
- [x] Engine import gate rejects `net/http`; the engine has no `net/http`
      import. (`net/url` remains, as a pure value package.)

### Phase 4 — Extract `internal/host`

- [x] Split `Loader.Load` into host `Fetch` and engine decompression/charset/
      parse; page assembly is in `internal/engine/page_loader.go`.
- [x] Cache, cookies, certs, settings, request log, network policy and raw
      fetch moved to `internal/host`. Global visited history and download
      history are deliberately not host state (D54).
- [x] Page, subresource, image, script, module, MCP and JS `fetch`/XHR
      requests are brokered. The host builds the HTTP request, applies CORS,
      credentials, redirect, mixed-content and private-network policy, and logs
      it.
- [x] Host enforces redirect/size/private-network/mixed-content policy and
      CORS through `JSFetch` and its transport.
- [x] Inline SVG and image decoding stay in the engine.
- [x] `about:` pages render in the engine from host/session data; the config
      token is `host.ConfigToken`. The session still collects history/settings
      data and applies submitted values.
- [x] Move MCP to a protocol client; the automation engine is its handler.
- [x] Move `document.cookie` parsing to the host broker; the engine receives
      name/value pairs and hands raw writes to the adapter.

### Phase 5 — Transport and lifecycle

- [x] Define `Transport`; implement the in-process transport.
- [x] Run every path through it: the interactive session, `-dump`, `-png` and
      MCP. The automation engine remains the handler behind the MCP client.
- [x] Engine runs on its own goroutine with its timer loop; front end owns the
      terminal loop.
- [x] Out-of-band `Abort` so Esc can cancel a busy script. `Aborter` runs the
      counter on the front-end goroutine; keys also count, so a ready command
      cannot swallow it.
- [x] Move text presentation: ANSI encoding, stdout and the row diff now live
      in `frontend.Presenter`; the engine emits `Frame` events for text modes.
- [~] Split chrome presentation: normal header/footer, scrollbar, prompts,
      find, menus, dialogs, select, download/notice, hints, the console and
      history are front-end-drawn. The engine `Chrome`/full-frame fallback
      remains for direct callers and tests.
      The URL/title display ledger is front-end-owned; host attestation
      remains.
- [x] Move native image placement to the front end: encode `ImagePlacement`
      into `protocol.Placement`, and move kitty/iTerm2 scaling, PNG and escape
      output to `internal/frontend/gfx.go`. Frames carry placements in every
      graphics mode.
- [x] Frame coalescing, backpressure and hostile-frame bounds: a bounded
      outbox coalesces frames/states, preserves one-shot order, and invalid
      frames are reported plus repaired or dropped.
- [~] Transport-level restart hook deferred to the child-process work (D59);
      there is no in-process caller to validate it.
- [x] Keep a stream/socket transport behind the same ports for the later child
      process: `ServeStream`/`DialStream` share the codec, validation and
      outbox, with direct abort on the serving reader.

### Phase 6 — Verification and cleanup

- [x] Fuzz both codec decoders for 60 s manually with no findings.
- [x] Wire that fuzz run into CI or a nightly job:
      `.github/workflows/fuzz.yml` runs both decoders nightly and on manual
      dispatch; `make fuzz` runs them locally.
- [ ] Consolidate benchmarks against the baseline and record the final deltas
      here; per-phase numbers exist but the Phase 6 summary is missing.
- [x] `-race` on engine/frontend (the full internal suite is green).
- [ ] Update `AGENTS.md` §4, `docs/agents/architecture.md`, `README.md` only if
      user-visible behavior changed, and `docs/agents/limitations.md` if a
      limitation changed.
- [ ] Remove aliases, shims and dead code.
- [ ] Delete this file and its links (see §10).

### What remains to close the rewrite

The host broker, codec/transport, dump/PNG/MCP paths, native image output,
terminal input and the live session loop are done. The remaining work is the
trusted-shell split, then Phase 6. Dependency order:

**A. Shell state and chrome contract**

- [x] `protocol.State` carries the normal header/footer, geometry, footer
      text/level/hits, PDF hint, view/zoom menu state and the fallback flags.
      Selection stays engine-side and crosses only as clipboard text; image
      progress is part of the footer text.
- [x] Base shell keys, history keys, quit, URL/vim prompt, page find,
      view/zoom windows, dialogs, the select popup, download/notice modals,
      link hints, console input/find and header/footer mouse are front-end-owned.
      The displayed URL/title ledger is front-end-owned; host-attested final
      URLs remain.
- [~] Toolbar, footer, scrollbar, overlays, console, find bar and menus are
      front-end-drawn on the interactive path. `Browser.drawChrome`,
      `presentComposed`, `protocol.Chrome` and the full-frame fallback remain
      for direct callers and tests.
- [x] A focused page form keeps `State.KeysEngine` true for key routing, but
      the front end keeps `FrontendChrome` true and composes the chrome.
- [x] Move content-rect geometry (`headerRows`, `viewport`, `contentRect`) to
      the front end; `ContentBounds` is computed there and sent in
      `Init`/`Resize`. The engine keeps a legacy derivation for direct callers
      and tests, and still receives terminal dimensions for the fallback
      screen.

**B. Dialogs and navigation**

- [x] Wire `protocol.Dialog`/`DialogResult` so JS alerts, confirms and prompts
      are front-end modals. The engine keeps the direct-key path for tests and
      automation.
- [x] Move back/forward history, scroll restore and `about:history` data to the
      front end; keep the page cache memory-only. D72 shape landed: the front
      end owns the list/index/scroll and sends a bounded `History` snapshot;
      the engine renders `about:history` and keys its page cache by entry ID.
      The download/curl hint is front-end presentation; the engine still
      builds the command.
- [x] Remove back/forward from the engine `App` actions; the front end sends
      `Restore` commands and the engine no longer handles history keys or side
      buttons on the direct path.
- [x] Move the displayed URL/title to the front end. `HistoryCommit` carries
      the title, the front end validates and stores the entry, and normal
      chrome renders that ledger instead of `State.URL/Title`.
- [ ] Make the final URL host-attested so a compromised engine cannot report a
      different location than the host fetched. This belongs with the
      host/process split.

**C. Session split and app wiring**

- [ ] Split `Browser` (306 methods): page/content state → `engine.Session`;
      chrome, navigation, prompts, menus, console and form focus → front end.
- [ ] Move Loader image/SVG/about/PDF orchestration into the engine session;
      the host keeps fetch, cache, cookies, settings and policy.
- [ ] Rename `Browser` → `Session` and remove `internal/browser`.
- [x] Move `browser.Execute`, `RunOptions` and mode dispatch into
      `internal/app`; the interactive boot now lives in `app/execute.go`.
- [ ] Replace the remaining `browser.NewSession`/`Dump`/`RunMCPBoot` calls
      with direct host/engine/frontend construction after the session split.
- [ ] Tighten `protocol/deps_test.go`: `app` → `engine`, `host`, `frontend`,
      `protocol`; no `browser` package.

**D. Tests and Phase 6**

- [ ] Move whole-app tests to seam tests in `app`; keep engine, layout and PDF
      fixtures with their packages and update the documented paths.
- [ ] Remove the browser-side shims, adapters and dead aliases
      (`engine_compat.go`, `layout_compat.go`, `paint_compat.go`,
      `frontend_bridge.go`, `js_compat.go`, `page_compat.go`, `pdf_compat.go`,
      `style_units_compat.go`, `cookie_compat.go`, `network_compat.go`,
      `cache.go`, `certs.go`, `render_gfx.go`); the real bridges
      (`frame_protocol.go`, `js_host.go`, `js_life.go`) move with the session.
- [ ] Remove the unused `protocol.Selection` event, or wire it if a front-end
      consumer appears.
- [ ] Record the Phase 6 benchmark deltas. The 60 s codec fuzz already runs in
      `.github/workflows/fuzz.yml` and through `make fuzz`.
- [ ] Update `AGENTS.md`, `architecture.md` and, only if user-visible behavior
      changed, `README.md` and `docs/agents/limitations.md`.
- [ ] Delete this tracker and its links.

## 5. Protocol

`internal/protocol` is canonical. The message inventory lives in
`messages.go`, the frame and cell types in `types.go`, the ports in `ports.go`,
the in-process transport in `transport.go`, and the stream transport in
`stream.go`. Limits are `DefaultLimits`. Do not maintain a second inventory
here; add a message with its codec round-trip, validation bound and transport
test.

## 6. Risks

| Risk | Mitigation |
| --- | --- |
| `Browser` is a 306-method monolith; Go cannot split its methods across packages | Move leaf subsystems first, then split the session state in one batch. The remaining engine content is smaller than the old package; keep aliases only until the batch is green. |
| Chrome state is spread across Browser fields | Define the `State`/overlay contract before moving input or drawing. Move shell key routing and chrome composition together so there is one source of truth. |
| Loader mixes host fetch, engine assembly and session orchestration | Split orchestration with the session; keep the per-request host fetcher snapshot (D40). |
| Package globals (`layoutZoom`, `cellW`, `cellH`, `DefaultLayoutBudget`, memory limits) | Turn them into session options during app wiring; required before any multi-session work. |
| Test migration (20k lines in `internal/browser`) | Move tests with their files; keep fixture goldens with the engine; build an app seam harness for whole-app cases. |
| Overlay composition | Move modal/chrome layout and input together; keep engine overlays clipped to the content rect until then. |
| Silent visual drift | `-dump` and `-png` hashes plus frame goldens and PTY checks at every batch. |
| Performance drift | Reusable buffers, no per-frame allocations, metadata only on layout changes, benchmark at every phase. |
| Scope creep | The non-goals are binding; new ideas go to `bin/todo.md`, not into B. |

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
| Idle frames/s | 0 (0 CPU ticks / 5 s) | 0 |

The codec benchmark is `BenchmarkCodecFrame` in `internal/protocol`; the parse,
paint, draw and content-frame benchmarks are in
`internal/browser/boundary_baseline_test.go`. A later process split is expected
to add a second Go runtime and IPC; those numbers are recorded separately and
are not B's budget. The per-phase sections below are historical; the Phase 6
summary is still to be recorded after the shell split.

### Chrome ownership (7a) deltas

The normal header and footer now come from `protocol.State`; the engine still
composes its `Chrome` fallback for overlays, the console and the find bar, so
the comparison is against the old composed path.

| Metric | Before 7a (Chrome layer) | After 7a (state chrome) | Delta |
| --- | --- | --- | --- |
| Full text repaint | 851–971 µs, 598.2 KB, 28 184 allocs | 915–947 µs, 598.4 KB, 28 078 allocs (7b) | flat within noise |
| Idle CPU | 0 ticks / 5 s | 0 ticks / 5 s | idle |
| `-dump`/`-png` hashes | fixed | identical | pass |

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

### Phase 5i deltas

The stream transport is not used by the in-process app, so runtime and output
are unchanged. It is the swap point for a later child process.

| Metric | Before 5i | After 5i | Delta |
| --- | --- | --- | --- |
| In-process draw/dump/PNG | unchanged | unchanged | flat |
| `-dump`/`-png` hashes | fixed | identical | pass |
| Stream round trip / abort / repair | new | `net.Pipe` tests pass | no app change |
| Full test suite and `-race` | pass | pass | no change |

### Phase 5h deltas

Normal page frames no longer carry chrome rows; a small `Chrome` event carries
the header/footer cells and the front end stacks them. Overlays and the console
still use the full-screen fallback.

| Metric | Before 5h | After 5h | Delta |
| --- | --- | --- | --- |
| Full text repaint | 841–864 µs, 593.2 KB | 860–875 µs, 585.6 KB | flat |
| Composed text repaint | new | 860–875 µs, 585.6 KB | new path |
| Engine paint + frame handoff | 147–151 µs, 74.2 KB | unchanged | flat |
| `-dump`/`-png` hashes | fixed | identical | pass |
| Full test suite and `-race` | pass | pass | no change |

PTY checks: about/help, console open/close, view-menu overlay fallback, kitty
image page and quit all behave as before.

### Phase 5f deltas

MCP now crosses the transport. This is not on the interactive or draw path, so
page rendering and dump output are unchanged. The binary `-mcp` smoke test
answered `navigate` and `snapshot` through the protocol client with exit 0.

| Metric | Before 5f | After 5f | Delta |
| --- | --- | --- | --- |
| Full draw / content paint | unchanged | unchanged | flat |
| `-dump`/`-png` hashes | fixed | identical | pass |
| MCP navigate + snapshot | direct Automation | protocol client | no visual change |
| Full test suite and `-race` | pass | pass | no change |

### Phase 5e deltas

Dump and PNG now cross the transport. The pipeline is unchanged; tall text
dumps are split after the single full paint so output is byte-identical.

| Metric | Before 5e | After 5e | Delta |
| --- | --- | --- | --- |
| Tiny `-dump about:home` | 3.9–5.2 ms | 3.9–5.7 ms | flat |
| Synthetic `-dump` | 25–36 ms | 23–29 ms | flat/faster |
| Synthetic `-png` | 30–32 ms | 27–37 ms | noisy |
| `-dump`/`-png` hashes | fixed | identical | pass |
| Full test suite and `-race` | pass | pass | no change |

### Phase 5d deltas

Native placements now cross the frame, so the engine writes no terminal
graphics in a session. The encode cache is populated on the first frame and
reused; the transport shares the immutable image bytes instead of copying them.

| Metric | Before 5d | After 5d | Delta |
| --- | --- | --- | --- |
| Engine frame + one native image (cached) | new | 73–75 µs, 74.4 KB, 13 allocs | frame + placement map |
| Text repaint, full frame | 802–836 µs, 593.1 KB | 813–834 µs, 593.1 KB | flat |
| Interactive idle CPU / RSS | 0 ticks / 5 s, 31.4 MB | 0 ticks / 5 s, 31.6 MB | idle |
| `-dump`/`-png` hashes | fixed | identical | pass |
| Full test suite and `-race` | pass | pass | no change |

PTY checks with a real image: kitty, iTerm2 and blocks all render the page text
and their expected image marker; cycling from blocks to none to native turns
kitty placements on. Alert, abort, resize and quit still pass.

### Phase 5c deltas

Text frames now cross the seam and the front end writes the terminal. The
engine-side paint plus frame handoff is much cheaper than the old combined
draw; the presenter pays the ANSI encoding and row diff that used to be in
`flushGrid`. Totals are a wash, and idle CPU is unchanged.

| Metric | Before 5c | After 5c | Delta |
| --- | --- | --- | --- |
| Full draw, forced repaint (old path) | 798–827 µs, 519.6 KB, 28 212 allocs | same benchmark retained | baseline |
| Engine paint + frame handoff | new | 150–156 µs, 74.2 KB, 12 allocs | engine side |
| Validated frame handoff (transport) | new | 18.7–19.3 µs, 73.9 KB, 3 allocs | replaces ~320 µs wire codec |
| Frontend present, forced full frame | new | 669–697 µs, 451.8 KB, 28 119 allocs | ANSI + diff + write |
| Combined text repaint | 800–834 µs, 519.6 KB, 28 212 allocs | 802–836 µs, 593.1 KB, 28 179 allocs | time flat, +73.5 KB for the detached frame |
| Interactive idle CPU / RSS | 1 tick / 5 s, 33.7 MB | 0 ticks / 5 s, 31.4 MB | idle |
| `-dump`/`-png` hashes | fixed | identical | pass |
| Full test suite and `-race` | pass | pass | no change |

PTY checks: local page load and link navigation, resize, quit, JS alert,
Esc abort, and `-graphics=blocks|kitty|iterm` all exited 0.

### Phase 5b deltas

The interactive loop now runs through the transport, with the engine on its own
goroutine. Draw and content-frame work are unchanged; each key and each state
event pays the ~1.1 µs codec round trip measured in 5a. Idle CPU stays at zero
wakes apart from the existing 2 s usage tick.

| Metric | Before 5b | After 5b | Delta |
| --- | --- | --- | --- |
| Full draw, forced repaint | 840–865 µs, 519 610 B/op | 804–811 µs, 519 590 B/op | flat |
| Content paint+blit, 100×33 | 85–105 µs, 0 B/op | 82–91 µs, 0 B/op | flat |
| Startup, tiny `-dump` | 4.5–5.0 ms | 3.9–5.2 ms | flat |
| Interactive idle CPU / RSS | not measured | 1 tick / 5 s (~0.2%), 33.7 MB RSS | idle, no frame loop |
| Key/state transport round trip | 1.12–1.17 µs | 1.12–1.17 µs | unchanged |
| `-dump`/`-png` hashes | fixed | identical | pass |
| Full test suite and `-race` | pass | pass | no change |

Interactive PTY checks: startup, local page load, link navigation, resize,
quit confirmation, a JS alert dismissed with Enter, and Esc stopping an
infinite script all keep the session responsive; each exited with status 0.

The engine still writes the terminal frame itself; the next slice moves
`drawChrome`, `flushGrid`, ANSI/PNG and native image placement to the front end
and emits `Frame` events instead of writing stdout.

### Phase 5a deltas

The transport is not on the browser draw path yet; it is exercised by tests and
a benchmark. One command plus one event costs about 1.1 µs, including both
codec passes and the channel hops. Codec frame cost is unchanged.

| Metric | Before 5a | After 5a | Delta |
| --- | --- | --- | --- |
| In-process command+event round trip | new | 1.13–1.17 µs, 256 B/op, 5 allocs | new baseline |
| Codec frame, 152×35 | 312–321 µs, 106 662 B/op | 312–321 µs, 106 662 B/op | flat |
| Full draw, forced repaint | unchanged | unchanged | flat |
| Content paint+blit | unchanged | unchanged | flat |
| `-dump`/`-png` hashes | fixed | identical | pass |
| Full test suite | pass | pass | no change |

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

### Phase 4b-5 deltas

Request policy, dialers and transport/client construction moved to
`internal/host`. No draw-path code changed.

| Metric | Before 4b-5 | After 4b-5 | Delta |
| --- | --- | --- | --- |
| Full draw, forced repaint | unchanged | unchanged | flat |
| Content paint+blit | unchanged | unchanged | flat |
| `-dump`/`-png` hashes | fixed | identical | pass |
| Full test suite | pass | pass | no change |

### Phase 4b-6 deltas

The raw HTTP/data request path moved to `internal/host`; page parsing and
assembly remain in the browser. No draw-path change.

| Metric | Before 4b-6 | After 4b-6 | Delta |
| --- | --- | --- | --- |
| Full draw, forced repaint | unchanged | unchanged | flat |
| Content paint+blit | unchanged | unchanged | flat |
| `-dump`/`-png` hashes | fixed | identical | pass |
| Full test suite | pass | pass | no change |

### Phase 4b-7 deltas

Page assembly and resource collection moved to the engine behind a brokered
`PageFetcher`. This is not on the draw path.

| Metric | Before 4b-7 | After 4b-7 | Delta |
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
go test -fuzz=FuzzDecodeCommand -fuzztime=60s ./internal/protocol
go test -fuzz=FuzzDecodeEvent -fuzztime=60s ./internal/protocol
go test ./internal/browser -run TestLayoutFixtures -update   # only when intentional
```

Also compare the baseline corpus hashes for `-dump` and `-png` before handing
work back.

## 10. Completion and deletion checklist

- [ ] Every phase checked, every gate green.
- [ ] Chrome, dialogs and history are front-end-owned; the URL/title ledger is
      front-end-rendered and host-attested final URLs remain. The engine cannot
      emit header, footer or dialog cells.
- [ ] `Browser` is split and renamed; `internal/browser` is gone.
- [ ] `app` constructs the session directly and the dependency test forbids a
      `browser` package.
- [ ] Whole-app tests and fixtures moved to their final packages.
- [ ] Durable rules folded into `AGENTS.md` §4 and
      `docs/agents/architecture.md`; no rule exists only here.
- [ ] Remove the routing row in `AGENTS.md` §1.
- [ ] Remove the link in `architecture.md`.
- [x] 60 s codec fuzz runs in CI/nightly (`.github/workflows/fuzz.yml`,
      `make fuzz`).
- [ ] Final benchmark deltas recorded after the shell split.
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
| 2026-11-22 | 4b-5 | Request metadata, local/mixed-content guards, dialers, `NewHTTPTransport`/`NewHTTPClient` moved to `internal/host`; browser wrappers; all tests and hashes green | `go test ./...`, `go vet`, `-race` on network/cert/frame, dump/PNG hashes | 4b-6: split Loader into raw Fetch + engine parse |
| 2026-11-22 | 4b-6 | Raw `Fetcher` moved to `internal/host` (data URLs, limits, encoding, cache, request log); Loader delegates; page assembly still browser; all tests and hashes green | `go test ./...`, `go vet`, `-race` on fetch/network/frame, dump/PNG hashes | Split page assembly/parse into engine |
| 2026-11-22 | 4b-7 | HTML assembly and stylesheet/script/import-map collection moved to `internal/engine` behind `PageFetcher`; browser adapter brokers host fetches; all tests and hashes green | `go test ./...`, `go vet`, `-race` on fetch/engine/frame, dump/PNG hashes | Phase 5: transport/lifecycle |
| 2026-11-22 | 5a | `protocol.Handler`/`Transport` and the codec-validating `InProc` session; abort channel, `Crash` substitution for invalid events, drain-on-close; round-trip/abort/close tests, a frontend frame seam test and a transport benchmark | `go test ./...`, `go vet`, `-race` on protocol/frontend, dump/PNG hashes | Wire the browser loop through the transport |
| 2026-11-22 | 5b | Interactive loop moved onto `InProc`: engine `Serve` goroutine, command routing, front-end terminal loop, `Quit` event, `Aborter`, transport-aware dialogs, hover coalescing; `Loader` now snapshots `host.Fetcher` per request to remove the concurrent-fetch race | `go test ./...`, `go vet`, full `-race` browser suite, PTY quit/resize smoke, 5 s idle sample, dump/PNG hashes, draw/content benchmarks | Move presentation and chrome to the front end |
| 2026-11-22 | 5c | `EventSink` with validated, detached frame handoff; `frontend.Presenter` owns ANSI, row diff and stdout for text modes; engine emits full-screen frames, `StateNativeImages` marks the engine-side native path; chrome and native placement still engine-side | `go test ./...`, `go vet`, full `-race`, frame/handoff/presenter benchmarks, PTY http/dialog/abort/graphics runs, idle sample, dump/PNG hashes | Move chrome composition and native image placement |
| 2026-11-22 | 5d | Native placements moved to the front end: engine encodes bounded immutable PNG blobs and emits `protocol.Placement`; `frontend.GFX` decodes/scales/crops and writes kitty/iTerm2; presenter combines text and placements; `StateNativeImages` drives mode ownership | `go test ./...`, `go vet`, full `-race`, placement/frame benchmarks, PTY kitty/iTerm2/blocks image and cycle checks, idle sample, dump/PNG hashes | Move chrome composition |
| 2026-11-22 | 5d-fix | Fixed native-mode presentation: the front end now presents text plus placements for every frame, instead of skipping frames when `StateNativeImages` was set; added a native event-handler regression test; engine crashes from the session are printed instead of silently dropped | `go test ./...`, `go vet`, full `-race`, PTY kitty cycle (`Graphics:kitty`, placement escapes) and quit | Move chrome composition |
| 2026-11-22 | 5d-fix2 | Clip native placements to the frame before the wire contract validates them. Negative or oversized image boxes (absolute/centered/overflowing artwork) no longer produce `placement N is outside the frame` crash frames. Added a clipping regression test. | `go test ./...`, `go vet`, `-race` on session/placement/transport, PTY kitty oversized-image page | Move chrome composition |
| 2026-11-22 | 5d-fix3 | Crash reporting is discoverable: front end deduplicates identical repeats, prints once to stderr, and sends `ClientError`; engine logs `[engine]` in the `!` console and sets a footer error; a good frame re-arms reporting. Added protocol command, codec round trip and dedup tests. | `go test ./...`, `go vet`, full `-race`, protocol/session/dedup tests | Move chrome composition |
| 2026-11-22 | 5d-fix4 | Invalid frames now produce `Crash` plus a repaired, re-validated frame; the front end resets the row diff so the next frame clears stderr damage; stderr is clipped to roughly one row. Added repair and unrepairable-frame tests. | `go test ./...`, `go vet`, full `-race`, transport repair tests | Move chrome composition |
| 2026-11-22 | 5d-fix5 | Console and console find-bar frames now go through `presentGrid`; closing the console repaints instead of leaving a black screen. Added console-frame and presenter-repaint tests; PTY checks with and without kitty placements. | `go test ./...`, `go vet`, full `-race`, console/kitty PTY runs | Move chrome composition |
| 2026-11-22 | 5e | `-dump` and `-png` moved onto `InProc` with a one-shot dump handler; tall text is split after painting and PNG uses one frame; warnings are Console events. Output hashes unchanged. | `go test ./...`, `go vet`, full `-race`, dump/PNG hashes, tall dump/PNG checks, timing samples | MCP/automation on the transport, then chrome composition |
| 2026-11-22 | 5f | MCP moved onto `InProc`: `automationHandler` owns the Automation engine, `automationClient` implements the MCP surface; added `Automation`/`AutomationResult` protocol messages with bounded JSON args and PNG results; CLI `-mcp` now opens a session and runs the client. | `go test ./...`, `go vet`, full `-race`, MCP transport round trip, binary `-mcp` navigate/snapshot smoke | Move chrome composition |
| 2026-11-22 | 5g | Replaced the direct event channel with a bounded coalescing outbox: newest frame/state wins, one-shot events keep order, Quit/Crash drop stale frames first. Added a coalescing regression test; PTY console/kitty smoke. | `go test ./...`, `go vet`, full `-race`, coalescing test, PTY smoke, dump/PNG hashes | Move chrome composition, then socket transport |
| 2026-11-22 | 5h | Normal frames are content-only with a `Chrome` header/footer layer; front end stacks it, overlays and console keep full-screen fallback; presenter composition and chrome-change resets; PTY chrome/console/overlay/kitty checks. | `go test ./...`, `go vet`, full `-race`, composed-repaint benchmark, PTY checks, dump/PNG hashes | Semantic chrome ownership, then socket transport |
| 2026-11-22 | 5i | Added the stream transport: `ReadCommand`/`ReadEvent`, `ServeStream` with coalescing outbox and direct abort handling, and `DialStream` client; tested navigation, abort, bad-frame repair and close over `net.Pipe`. Both codec fuzz targets ran 60 s with no findings. | `go test ./...`, `go vet`, full `-race`, stream transport tests, 60 s codec fuzz | Semantic chrome ownership, then Phase 6 cleanup |
| 2026-11-22 | 5j–6b | Host broker closed: JS `fetch`/XHR and `document.cookie` use `host.JSFetch` and brokered cookie values; built-in/about pages render in the engine; the config token moved to `host.ConfigToken`; Phase 4 closed. | `go test ./...`, `go vet`, full `-race`, host/JS/page tests, dump/PNG hashes | Terminal front end, then app |
| 2026-11-22 | 6c | Terminal raw mode, capability detection, key/mouse decoding, resize/quit signals and OSC 52 clipboard moved to `internal/frontend`; mouse types aliased; `Clipboard` events added. | `go test ./...`, `go vet`, full `-race`, frontend terminal tests, dump/PNG hashes | Create app and move the loop |
| 2026-11-22 | 6d | Created `internal/app` for flags/validation/dispatch; `cmd/mob3` calls `app.Run`; moved the live session event loop and crash/chrome/clipboard handling to `frontend.SessionLoop`; browser `Execute(RunOptions)` retains session setup. CLI tests moved to app. | `go test ./...`, `go vet`, full `-race`, app/frontend tests, version/help smoke, dump/PNG hashes | Move session construction/type, then semantic chrome ownership |
| 2026-11-22 | docs | Corrected and consolidated this tracker: status, phase checklists, file ownership, remaining work, protocol and risk sections now match the landed app/frontend split. | `go test ./...`, `go vet`, `gofmt`, `git diff --check` | Shell state/chrome contract, then the session split |
| 2026-11-22 | 6e | Quick closures: added the nightly/manual codec fuzz workflow and `make fuzz`; deferred the transport restart hook to the child-process work (D59); moved the build `Version` to `internal/app` and injected it into the session; removed the `pages.go` wrapper. | `go test ./...`, `go vet`, full `-race`, `make build`, `make fuzz` | Chrome-state trust contract and footer pilot |
| 2026-11-22 | 7a | Batch 1 of chrome ownership: added the `State` chrome snapshot, front-end header/footer rendering, header/footer mouse input and `App` actions for URL/zoom/console/JS/status/download/home; engine `Chrome` remains the overlay/console/find fallback. Added parity tests against engine chrome and chrome hit/input tests. | `go test ./...`, `go vet`, full `-race`, state/footer parity at several sizes, PTY header/footer click checks, dump/PNG hashes | Batch 2: shell keyboard modes, find bar, menus, scrollbar |
| 2026-11-22 | 7b | Batch 2: base shell keys and local quit live in the front end, gated by `State.KeysEngine`; added `AppOpenVimPrompt`/`AppOpenViewMenu`; moved scrollbar drawing and drag input to the front end with `Presenter.Refresh` for chrome-only updates. Prompt editing, find, menus, console and dialogs remain engine-side. | `go test ./...`, `go vet`, full `-race`, quit/action/key-gate and scrollbar parity tests, PTY quit/dark/footer checks, dump/PNG hashes | Batch 3: prompts, find bar, menus and dialogs |
| 2026-11-22 | 7c | Batch 3: URL/vim prompt state, line editing, recall history and mouse caret moved to `frontend/prompt.go`; `o`/`ctrl-l`/`:` open the local prompt, Enter navigates, Esc cancels and `:q` exits. Engine prompt remains for direct callers/automation. | `go test ./...`, `go vet`, full `-race`, prompt edit/history/mouse tests, PTY prompt navigation and `:q`, dump/PNG hashes | Batch 4: find bar, menus and dialogs |
| 2026-11-22 | 7d | Batch 4: page find bar/query/caret/results moved to the front end; added `Find` command handling and `FindResults` replies in the engine; Enter/Esc leave matches for `n`/`N`, next Esc clears. Console HTML find stays engine-owned. | `go test ./...`, `go vet`, full `-race`, find command/bar/input tests, PTY find/navigation checks, dump/PNG hashes | Batch 5: view/zoom/select menus and dialogs |
| 2026-11-22 | 7e | Batch 5: view/zoom windows moved to the front end, including the full-screen dimmed overlay, keyboard/mouse rows and `AppZoomSet`; engine keeps its menu fallback. Added `State.Menu`/`MenuIndex`/`ViewFlags`/`ZoomPercent` and menu layout/draw tests. Select menu, dialogs and console remain. | `go test ./...`, `go vet`, full `-race`, menu key/mouse/layout tests, PTY view/zoom checks, dump/PNG hashes | Batch 6: select menu, dialogs and console |
| 2026-11-22 | 7f | Batch 6: JS dialogs moved to the front end via `Dialog`/`DialogResult`; engine blocks in `runTransportDialog`, front end draws alert/confirm/prompt, edits the prompt and answers. Kept the direct-key dialog path for tests/automation. Added dialog layout/draw, transport round-trip and engine-parity tests. | `go test ./...`, `go vet`, full `-race`, dialog transport/layout/parity tests, PTY alert/confirm/prompt checks, dump/PNG hashes | Batch 7: select menu and console |
| 2026-11-22 | 7g | Batch 7: page select popup moved to the front end via `Select`/`SelectResult`; engine sends option labels and commits the answer, front end draws/navigates/mouse-selects. Kept the engine popup for tests/automation and excluded the pending select from the fallback overlay. Added select layout/draw, transport, key/mouse and engine-parity tests. | `go test ./...`, `go vet`, full `-race`, select transport/layout/parity tests, PTY select checks, dump/PNG hashes | Batch 8: console |
| 2026-11-22 | 7h | Batch 8: download and notice modals moved to the front end via `Download`/`Notice` and `ModalClose`; OSC 52 copy lives in the front end; engine keeps its fallback modal path. Added modal layout/draw, transport, copy/close and download-parity tests. | `go test ./...`, `go vet`, full `-race`, modal transport/copy/parity tests, PTY download-link copy, dump/PNG hashes | Batch 9: console and hint overlay |
| 2026-11-22 | 7i | Batch 9: link hints moved to the front end via `Hints`/`HintResult`; engine computes targets/labels/glyph cells/hrefs, front end paints, narrows prefixes, confirms F destinations and answers. Engine keeps its direct hint path. Added hint transport/input/draw tests and PTY f/F checks. | `go test ./...`, `go vet`, full `-race`, hint transport/input/draw tests, PTY f/F checks, dump/PNG hashes | Batch 10: console |
| 2026-11-22 | 7j | Batch 10: console composition moved to the front end via `ConsoleView`; engine supplies visible lines/tabs/summary/input/footer, front end draws the full screen and overlays dialogs/select/download/notice. Console input/find remains engine-side by design. Added console snapshot/draw/transport tests and PTY console checks. | `go test ./...`, `go vet`, full `-race`, console view/draw tests, PTY console tabs/input/close, dump/PNG hashes | Batch 11: console input and find |
| 2026-11-22 | 7k | Batch 11: console input, tabs, scrolling, history, copy and quit moved to the front end via `ConsoleAction`; engine applies actions and returns snapshots. HTML find remains engine fallback and `/`/`n`/`N` forward there. Added console action/input/quit tests and PTY console editing/tab/close checks. | `go test ./...`, `go vet`, full `-race`, console action/input tests, PTY console editing/tab/close, dump/PNG hashes | Batch 12: console HTML find, then navigation/session split |
| 2026-11-22 | 7l | Batch 12: console HTML find moved to the front end; `Find` has a page/HTML scope, engine sends counts/highlight rectangles in `ConsoleView`, front end owns bar/query/caret/next/close. Engine old find path remains for direct callers/tests. Added console find command/view/key tests and PTY `/help` check. | `go test ./...`, `go vet`, full `-race`, console find tests, PTY console HTML find, dump/PNG hashes | Batch 13: navigation state and session split |
| 2026-11-22 | 7m | Audit: corrected the stale status, Phase 2/5 checklists and file-ownership notes to match the landed console/find/dialog/chrome ownership. Recorded the remaining trust gap (a focused page form makes `State.FrontendChrome` false and lets the engine compose chrome), the unused `protocol.Selection` event and the unused `Navigate.RestoreScroll`, and updated the `Browser` method count to 303. No behavior change. | `go test ./...`, `go vet`, `gofmt`, `git diff --check`, dump/PNG hashes | Batch 13: navigation state, trusted URL and content-rectangle geometry |
| 2026-11-23 | 7n | Batch 13 (D72): moved the session history list, current index, scroll offsets and `about:history` snapshot to the front end. Added `History`, `Restore` and `HistoryCommit`; the engine now keys its memory-only page cache by front-end entry ID, emits final URLs, restores cached/same-document entries, and no longer owns back/forward or `App` history actions. Automation keeps its own headless history list. Trusted URL/title ownership remains deferred. | `go test ./...`, `go vet`, full `-race`, protocol/history/cache/restore tests, front-end snapshot/commit/mouse tests, PTY H/back/forward, dump/PNG hashes | Trusted URL, content-rectangle geometry, then session split |
| 2026-11-23 | 7o | Batch 14: moved content-rectangle geometry to the front end. `ContentBounds` computes the chrome reservation; `Init`/`Resize` carry `ContentTop`/`ContentHeight`; the engine uses the supplied rectangle and keeps the legacy derivation only for direct callers/tests. Terminal dimensions still travel for the fallback screen. Added geometry and protocol tests; PTY and hashes unchanged. | `go test ./...`, `go vet`, full `-race`, geometry/state/protocol tests, PTY H/back, dump/PNG hashes | Trusted URL/title, then session split |
| 2026-11-23 | 7p | Batch 15: moved the displayed URL/title ledger to the front end. `HistoryCommit` now carries the page title; the front end validates scheme/length, stores the entry, and substitutes it for `State.URL/Title` in normal chrome. Dynamic title updates are accepted only while the engine reports the committed URL. Invalid commits are ignored. Host-attested final URLs remain for the session split. | `go test ./...`, `go vet`, full `-race`, protocol/commit/ledger/validation tests, PTY H/back, dump/PNG hashes | Form chrome gap, then host-attested URL/session split |
| 2026-11-23 | 7q | Batch 16: closed the form-focus chrome gap. `frontendChromeActive` now follows the normal front-end chrome path even when `KeysEngine` is true, so a focused page form still routes keys through the engine while the front end composes header/footer/scrollbar. Added a state test for form focus and updated the chrome/ownership docs. | `go test ./...`, `go vet`, full `-race`, chrome state tests, PTY form/help, dump/PNG hashes | Host-attested URL/session split |
| 2026-11-23 | 7r | Batch 17: moved mode dispatch and the interactive session boot from `internal/browser/cli.go` to `internal/app/execute.go`. The app now builds the loader/session, owns terminal enter/leave, presenter/transport wiring and the loop; browser exposes narrow `NewConfiguredLoader`/`NewSession`/`Dump`/`RunMCPBoot` helpers. `browser.Execute`/`RunOptions` and `cli.go` are gone. | `go test ./...`, `go vet`, full `-race`, app/browser tests, PTY interactive boot, MCP tools/list smoke, dump/PNG hashes | Session split (Browser → engine.Session), then direct app wiring |
| 2026-11-23 | 7s | Batch 18: first session-split content leaf. Reader extraction, candidate scoring, junk filtering and `ReaderRules` moved to `internal/engine/reader.go`; `browser/reader.go` is a thin alias. Extraction tests moved to the engine; browser keeps reader-mode integration tests. | `go test ./...`, `go vet`, full `-race`, engine/browser reader tests, dump/PNG hashes | Continue content leaves (selection, hints, forms), then session type split |
| 2026-11-23 | 7t | Batch 19: moved selection range, ordering, text extraction, summary and viewport painting to `internal/engine/selection.go`. The browser keeps drag state and clipboard/front-end policy and calls the engine selection. Added engine selection tests; browser drag/copy tests updated. | `go test ./...`, `go vet`, full `-race`, engine/browser selection tests, dump/PNG hashes | Continue content leaves (hints, forms), then session type split |
| 2026-11-23 | 7u | Batch 20: moved link-hint label assignment into `internal/engine/hints.go`: glyph/char types, single/pair assignment, unique-pair shortening and base-26 encoding. The browser keeps target collection, painting, activation and confirmation. Pure hint tests moved to the engine; browser hint tests updated. | `go test ./...`, `go vet`, full `-race`, engine/browser hint tests, PTY f/F checks, dump/PNG hashes | Continue content leaves (find/forms), then session type split |
| 2026-11-23 | 7v | Batch 21: moved find match computation into `internal/engine/find.go`: page/HTML streams, whitespace normalisation, overlapping case-insensitive spans, match grouping and highlight rectangles. The browser keeps find state, navigation, painting, result messages and the engine console fallback. Added engine find tests; browser find tests updated. | `go test ./...`, `go vet`, full `-race`, engine/browser find tests, PTY find/n/N, dump/PNG hashes | Form state leaf, then session type split |
| 2026-11-23 | 7w | Batch 22: moved form constraint validation into `internal/engine/validate.go`: required fields, radio groups, typed date/month/week/time/datetime/color formats and pattern checks. The browser keeps form control state, editing, Tab/focus and submission. Added engine validation tests; browser form tests still cover submission integration. | `go test ./...`, `go vet`, full `-race`, engine/browser form tests, dump/PNG hashes | Session type split, or form control state next |
