# Boundary migration (B)

**Temporary working document.** It tracks the split of the single `browser`
package into a content engine, a trusted host, a terminal front end and a
protocol seam. Update it as work lands: tick items, record measurements, and
append decisions to the log. Delete it when the migration is complete, after
folding every durable rule into `AGENTS.md` and
[architecture.md](architecture.md). The routing row in `AGENTS.md` and the link
in `architecture.md` are removed in the same change.

Status: **Phase 1 complete; Phase 2 next.**
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
| `html.go` | engine | Tokenizer and tree builder. |
| `style*.go` | engine | All style, selector, sheet, media and UA files. |
| `layout*.go` | engine | All layout files, including forms, images, tables, grid, flex and measurement. |
| `render.go` | engine + frontend | Grid, paint and image placement stay in the engine; `Lines`/`LinesMono`, ANSI and kitty/iTerm2 move to the frontend in Phase 3 (D19). |
| `frame_protocol.go` | engine | Maps the internal grid to `protocol.Frame` at the seam. |
| `term_detect.go` | frontend | Environment capability detection; the paint path takes capabilities as input. |
| `png.go` | frontend | Cells to PNG. |
| `fetch.go` | host + engine | HTTP/TLS/redirect/cookies/size caps host; decompression, charset, parsing and decoder budgets engine. |
| `cache.go` | host | Resource cache. |
| `pages.go` | engine | About/help/error HTML from host data; the config token is validated by the host. |
| `certs.go` | host | Trust store selection. |
| `pdf_object.go`, `pdf_text.go`, `pdf.go` | engine | Text extraction and page assembly. |
| `config.go` | host + engine | Model and apply host; `about:config` form markup engine. |
| `browser.go` | engine | Session state, navigation and load lifecycle; shell state moves to the frontend. |
| `browser_chrome.go` | frontend | Toolbar, footer, status and overlays. |
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
| `dark.go` | engine | Palette remap. |
| `js*.go` | engine | Runtime, DOM, events, compat, stubs, modules, web APIs, Intl. |
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
| 2 | Chrome and state ownership | 2–3 weeks | Next | UI tests pass; engine has no terminal or chrome concepts |
| 3 | Extract `internal/engine` | 3–4 weeks | Not started | Engine import gate passes; engine tests green |
| 4 | Extract `internal/host` and broker | 2–3 weeks | Not started | Engine cannot open sockets or read cookies |
| 5 | Transport and lifecycle | 1–2 weeks | Not started | All paths run through the in-process transport |
| 6 | Verification, docs, cleanup | 1 week | Not started | Gates below hold; aliases removed |

\* Phase 1 is functionally complete. The `Lines`/gfx file move is folded into
Phase 3 under D19.

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

- [ ] Front end draws toolbar, footer, prompts, notices, dialogs, console,
      view/zoom menus, find bar and hint overlay.
- [ ] Engine emits a state event: URL, title, loading/progress,
      scroll/max-scroll, mode flags, errors.
- [ ] Front end computes the content rectangle; engine knows only that size.
- [ ] Page keys are forwarded; app keys, prompts and quit are local.
- [ ] JS dialogs become request/response messages.
- [ ] Engine overlays are clipped to the content rect; everything that covers
      chrome moves to the front end.
- [ ] Automation still drives the same commands.

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
