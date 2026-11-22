# Boundary migration (B)

**Temporary working document.** It tracks the split of the single `browser`
package into a content engine, a trusted host, a terminal front end and a
protocol seam. Update it as work lands: tick items, record measurements, and
append decisions to the log. Delete it when the migration is complete, after
folding every durable rule into `AGENTS.md` and
[architecture.md](architecture.md). The routing row in `AGENTS.md` and the link
in `architecture.md` are removed in the same change.

Status: **Phase 0 not started.**
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

## 4. Phases

Estimates are for one developer who knows the codebase, Linux first, ±40%.
Total: **9–13 weeks**. Phase 3 dominates the risk.

| Phase | Scope | Estimate | Status | Exit gate |
| --- | --- | --- | --- | --- |
| 0 | Contract, codec, dependency test, baseline | 1 week | Not started | Protocol compiles; baseline recorded |
| 1 | Frame boundary and paint split | 1–2 weeks | Not started | `-dump`/`-png` byte-identical; engine has no ANSI writer |
| 2 | Chrome and state ownership | 2–3 weeks | Not started | UI tests pass; engine has no terminal or chrome concepts |
| 3 | Extract `internal/engine` | 3–4 weeks | Not started | Engine import gate passes; engine tests green |
| 4 | Extract `internal/host` and broker | 2–3 weeks | Not started | Engine cannot open sockets or read cookies |
| 5 | Transport and lifecycle | 1–2 weeks | Not started | All paths run through the in-process transport |
| 6 | Verification, docs, cleanup | 1 week | Not started | Gates below hold; aliases removed |

### Phase 0 — Contract and baseline

- [ ] Write the ownership table for every file (engine / host / frontend / app).
- [ ] Create `internal/protocol` with `Frame`, `Cell`, `Placement`, `Meta`,
      `Command`, `Event`, `State`, `Limits`, `SessionID`, `Version`.
- [ ] Define the `Session` and `Broker` ports in `protocol`.
- [ ] Implement a fixed binary codec; no reflection and no `gob`.
- [ ] Round-trip test for every message; fuzz the decoder with size/count
      bounds.
- [ ] Add the dependency test (allowed graph above), initially scoped to the
      new packages.
- [ ] Build the baseline corpus from synthetic pages: text-heavy, deep DOM,
      table, grid, many links, images off, PDF, long script.
- [ ] Record baseline: startup ms, RSS MB, `-dump` hash, `-png` hash, frame
      paint/encode time, allocations per frame, idle frames.
- [x] Add the routing row and architecture link for this file.
- [ ] Resolve the Phase 0 open questions in §8.

### Phase 1 — Frame boundary

- [ ] Engine paints into a content-rect `Frame`, not the full-screen `Grid`.
- [ ] Replace `Cell.Raw` with structured scaled-text runs; `Cell` becomes
      plain data.
- [ ] Move `Grid.Lines`/`LinesMono`, gfx, kitty/iTerm2 placement and
      `png.go` to the front end.
- [ ] Move terminal capability detection out of `render.go`.
- [ ] Reuse two frame buffers; no steady-state allocation per frame.
- [ ] Keep the existing row diff on the front-end side.
- [ ] Frame-snapshot golden tests.

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

Fill the baseline in Phase 0; the gate columns are the working limits.

| Metric | Baseline | Gate |
| --- | --- | --- |
| Startup | TBD | +2 ms max |
| RSS | TBD | +2 MB max |
| `-dump` output | TBD | byte-identical |
| `-png` output | TBD | byte-identical |
| Full-frame encode | TBD | no regression |
| Full-frame paint | TBD | ≤5% regression |
| Allocations per frame | TBD | zero in steady state |
| Idle frames/s | TBD | 0 |

A later process split is expected to add a second Go runtime and IPC; those
numbers are recorded separately and are not B's budget.

## 8. Open questions

Resolve in Phase 0 and record the answer in the decision log.

1. Do `-dump`, `-png` and MCP run through the in-process transport, or call the
   engine directly? (Current plan: through the transport, so the seam is
   exercised by default.)
2. Keep the type name `Browser` inside the engine, or rename to `Session` as
   part of Phase 3?
3. In-process transport: always encode/decode, or direct struct handoff with
   the codec exercised only in tests? (Current plan: encode by default, with a
   debug direct mode if benchmarks demand it.)
4. Where are `about:` pages generated: engine from host data, or host HTML
   parsed by the engine? (Current plan: engine renders, host supplies data and
   validates the config token.)
5. Does the request log live in the host or in the engine? (Current plan:
   host; front end displays it.)

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
