# AGENTS.md — Project decisions and working agreement

MOB3 is a small web browser for the terminal, written in Go.

Read this file first. It carries the working agreement, the non-negotiable
constraints, the commands and the change protocol. Detailed engine, UI, network,
build, JS and debugging notes live in `docs/agents/` and are opened only when a
task needs them, which keeps this page short enough to read in full every time.

**Record each decision where it governs.** Fold a new rule into the section it
belongs to here, or into the routed page it belongs to, rewritten as the rule in
force. Do not append history, and do not leave a superseded rule standing.

## 1. Open on demand

| Page | Read it when |
| --- | --- |
| [docs/agents/architecture.md](docs/agents/architecture.md) | choosing or adding a file, moving code, checking what a subsystem owns |
| [docs/agents/rendering.md](docs/agents/rendering.md) | touching style, layout, paint, text, images, backgrounds or PDF display |
| [docs/agents/css-availability.md](docs/agents/css-availability.md) | checking which widely available CSS features are missing, or choosing CSS support work |
| [docs/agents/ux.md](docs/agents/ux.md) | touching chrome, keys, mouse, forms, history, reader, dark mode, dialogs or the JS console |
| [docs/agents/networking.md](docs/agents/networking.md) | touching fetching, caching, cookies, TLS, content types or resource limits |
| [docs/agents/js-web-api.md](docs/agents/js-web-api.md) | touching the JS surface or judging whether a library can work |
| [docs/agents/build-release.md](docs/agents/build-release.md) | build, Docker, CI, releases or the GitHub Pages site |
| [docs/agents/debugging.md](docs/agents/debugging.md) | diagnosing a layout, rendering, engine or PDF extraction bug |
| [docs/agents/limitations.md](docs/agents/limitations.md) | deciding whether something is supported, or writing user docs |
| [docs/agents/rendering-todo.md](docs/agents/rendering-todo.md) | picking engine work from the open issues |

User-facing docs stay separate: `README.md` is the short landing page and
`docs/*.md` holds the long-form material. The routed agent pages are not
published by the Pages build.

## 2. Working agreement

- **Efficiency is the product goal.** Target: most of the features people
  actually use, for a small fraction of the CPU and memory of a mainstream
  browser — capability per unit of resource, not parity at any price. This
  decides ambiguous feature work. Prefer the cheap approximation over the exact
  but expensive implementation whenever the visible result is close (block
  backgrounds instead of inline ones, half-block art instead of sixel, text
  PDFs instead of rasterized pages, partial SVG instead of a full renderer).
  Do not add background loops, perpetual repaints, prefetching, polling or a
  dependency for a marginal feature. Idle stays at zero CPU, startup stays
  instant, first paint stays ahead of image work, and every unbounded input
  path keeps a cap. When two features have similar value, take the one with the
  smaller steady-state footprint, and do not regress idle CPU, resident memory
  or first-paint latency for a new feature: measure before and after when a
  change touches the draw loop, timers, layout, the style pass or the JS
  runtime, comparing on one machine and the same pages (recipe in the
  Efficiency target section of `docs/development.md`). User-facing docs state
  the goal qualitatively and do not publish measured figures or baseline
  tables, which go stale with hardware and page corpus.
- **Ask before deciding.** Anything needing attention, approval, or that is a
  software design decision must be asked, not assumed. Ask **one question at a
  time** and wait for the answer. Never bundle several decisions.
- **If an unanswered question matters, ask again.** An answer is not implied by
  silence.
- **Tests as you go.** Add or update tests in the same change as every behavior
  change or bug fix. Tests stay network-free and cover each affected layer
  (parser, style, layout, paint, input) where practical. **Always add or update
  a golden layout fixture when fixing or changing layout or rendering-engine
  behaviour**, even when unit tests exist. Fixtures live in
  `internal/engine/testdata/pages/` and render through `TestLayoutFixtures`;
  refresh with
  `go test ./internal/engine -run TestLayoutFixtures -update` and review the
  diff. Fixtures use inline `<style>` only, no external assets, generic names
  and content. Imported-stylesheet goldens live in
  `internal/engine/testdata/import/`; PDF goldens are a small `.pdf` plus its
  `.txt` in `internal/engine/testdata/pdf/`, refreshed the same way with
  `-run TestPDFFixtures -update`.
- **Report site quirks; do not chase them.** When a site relies on
  non-standard HTML, invalid markup or undocumented browser behavior, report
  what it does. Never add engine behavior for a site-specific quirk; fix the
  engine only for standard CSS/HTML behavior.
- **No debug leftovers.** Temporary `zz_*` debug test files are deleted before
  handing work back.
- **No site names in the codebase.** Do not mention specific websites in code,
  comments, tests, commit messages or documentation. Describe behavior
  generically.
- **No large files.** A tracked file must stay at or under 2 MB, except the
  generated `vendor/` tree. `.githooks/pre-commit` (installed once with
  `make hooks`) rejects a staged file above the cap, and CI runs the same script
  with `--all` over every tracked file, so `--no-verify` cannot land one.
  Shrink, compress or split the file; raising the cap is a user decision
  recorded here.
- **Docs stay current.** Update `README.md` and the relevant `docs/` page in the
  same change whenever user-visible behavior, flags, keys, build output,
  supported features or limitations change. `mob3 -h` and `about:help` are
  canonical for flags and keys; `mcpToolDefinitions()` is canonical for
  automation tools. `README.md` stays short and links to the docs; keep the
  agent detail in this file and `docs/agents/`, not in the README.
- **Contributions.** Every external contribution requires the broad
  license-grant CLA in `CONTRIBUTING.md` and the PR statement it describes.
  Never merge contributed code without that agreement. Contributors keep their
  copyright but grant the Maintainer the right to relicense under AGPL-3.0-only
  or commercial terms. The `PR` workflow checks the statement; the `CI`
  workflow runs formatting, vet and tests. Bugs go to the repository issues
  tracker; vulnerabilities go privately through GitHub Security Advisories. The
  bug report form in `.github/ISSUE_TEMPLATE/bug_report.yml` stays deliberately
  short — page URL, current and expected result with a screenshot of each, and
  the build and terminal — and must stay aligned with the CLI. Security reports
  stay on the private advisory link, never the public form.
- **Commit messages.** End every completed change with a short, one-line commit
  message summarizing the work. Do not commit unless explicitly asked.
- **Documentation split.** `README.md` is short and user-facing; detailed flags
  live in `mob3 -h`, keys/mouse/forms in `about:help`; do not duplicate those
  lists in the README. Do not hard-wrap README prose: one line per paragraph or
  list item, letting the viewer reflow it; only code blocks, tables and HTML
  stay structured. Long-form material goes in the matching `docs/` page.

## 3. Non-negotiable constraints

- **Go 1.27.** `go.mod` declares `go 1.27`; the target toolchain is Go 1.27.x,
  and the code may use standard-library and language features through it.
- **Single static binary.** Build with `CGO_ENABLED=0`; `make` produces a
  statically linked binary. No cgo and no external system libraries.
- **License.** AGPL-3.0-only. `LICENSE` carries the full GNU AGPL v3 text, and
  the README records the copyright (currently `2026 Milos Stojanovic`). A
  separate commercial license is available on request; do not add commercial
  terms or a contact unless the copyright holder supplies them. Keep the
  license file and the README License section in sync.
- **Not a secure browser.** No sandboxing, memory, site or OS isolation, CSP
  enforcement, certificate UI or security hardening, and it is not audited.
  Tab separation is logical only (separate JS runtimes and origin checks).
  The default per-tab child process is fault isolation, not a security
  boundary: tabs still share the parent host, storage broker and address space
  trust model. This warning stays prominent in `README.md` and on the built-in
  start and help pages. Never present MOB3 as safe for passwords, accounts,
  payments or confidential data.
- **No filesystem I/O.** The binary must not read or write files: no
  configuration, bookmark, certificate, cache, history, log or download files,
  and no local page files (`file:` URLs are rejected and bare paths are never
  probed). Documents come from the network or `about:`; `data:` is accepted
  only as an inline subresource, never as a page, and every cache and piece of
  state is memory-only; closing MOB3 saves nothing. The
  download popup only copies a `curl` command. The memory footer uses Go's
  runtime statistics, not `/proc`. OS-level reads by Go's TLS, DNS and timezone
  libraries, `os.Executable`/self-exec for the per-tab process transport, and
  anonymous pipes between those processes are the only filesystem access and
  are not application storage. Do not add automatic disk caching or
  restore-on-start behavior.
- **Dependency policy.** Standard library plus the approved
  `github.com/grafana/sobek` JavaScript engine (maintained `goja` fork adding ES
  module records and dynamic imports), the pure-Go SVG renderer
  `github.com/go-gfx/oksvg` (maintained fork adding `clipPath`, `fill-rule`,
  compact arc flags, transform-scaled strokes and bounded dash strokes) plus
  `github.com/srwiley/rasterx` (user-requested SVG support), their pure-Go
  transitive modules (`regexp2/v2`, `sourcemap`, `pprof`,
  `golang.org/x/image`, `golang.org/x/net`, `golang.org/x/text`,
  `golang.org/x/sys` for `x/image`'s SIMD rasterizer), and
  `golang.org/x/crypto/x509roots/fallback` (user-requested current TLS trust
  bundle, carrying Mozilla's roots and distrust-after constraints). No other
  third-party modules without a new explicit user decision. All are pure Go and
  must not change the single-binary or no-cgo constraints. Everything is
  **vendored in `vendor/` and committed**; builds use `-mod=vendor`, need no
  network, and `go mod vendor` runs after any `go.mod`/`go.sum` change with
  `vendor/modules.txt` kept in sync.
- **Patched dependencies.** `x/image` and `x/net` track current releases
  (`v0.48.0`, `v0.63.0`) after the 2026 security audit, and `go.mod` pins
  `toolchain go1.27.4`. Re-run
  `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` after any dependency
  or toolchain change; the scan must report no reachable vulnerabilities. The
  nightly `Vulnerability scan` workflow runs the same command.
  `sobek` carries one local patch in `func.go` (`await` attaches reactions to a
  native promise directly, so a page replacing `Promise.prototype` cannot leave
  the wrapper pending); re-apply it after `go mod vendor`. Bump
  `x/crypto/x509roots/fallback` separately with
  `go get golang.org/x/crypto/x509roots/fallback@latest` and re-vendor. Media
  decoders treat input as hostile: a decoder panic is recovered and shown as a
  broken image. Details in `docs/agents/build-release.md`.
- **Terminal-only presentation.** No GUI toolkit, no native window. All output
  is ANSI/Unicode in the terminal, and a terminal cell is the layout unit.
- **PDF viewer.** PDF responses are read by an in-repo, standard-library-only
  parser (`internal/engine/pdf_object.go`, `pdf_text.go`, `pdf_document.go`) and
  shown as extracted text pages; no rasterizer, no cgo, no new module. Scanned
  pages, figures, images, annotations and links are not rendered, and encrypted
  documents are refused.
  `-no-pdf` restores the download prompt. Do not add a PDF rendering library
  without a new explicit user decision.
- **Images.** Disabled by default for speed (`-graphics=none`): no external
  image downloads and no inline SVG rasterization, leaving `alt` text or
  labels. When enabled, native terminal graphics come first — kitty, then
  iTerm2 — with colored half-block art (`▀`/`▄`) as the fallback. No sixel.
- **JavaScript via sobek.** Page scripts run in a per-page sobek runtime;
  classic scripts execute in document order, `async`/`defer` are ignored, and
  ES modules (static, dynamic `import()`, import maps) are evaluated in order.
  External scripts and modules require a JavaScript MIME type. `-no-js`
  disables fetching and execution. The supported API surface and its intentional
  limits are in `docs/agents/js-web-api.md`; do not add a stub that would make a
  library depend on behavior MOB3 cannot provide.
- **Keep it bare-bones but functional:** real fetching, parsing, layout,
  rendering, navigation and scrolling.

## 4. Repository layout

- `cmd/mob3/main.go` — thin entry point; calls `app.Run(os.Args[1:])` and
  exits with its status.
- `internal/app/` — flag parsing, validation, mode dispatch and the
  interactive session boot. It constructs `host.Session` and
  `engine.NewConfiguredLoader`, then `engine.NewConfiguredSession`,
  `engine.NewAutomationWithConfig` or `engine.NewDumpHandler`. Interactive tabs
  go through the `tabBackend` seam, which owns one session's lifecycle and
  exposes graceful close plus force kill. MCP transport and the dump/PNG output
  loop live in `internal/app`.
- `internal/engine/` — the 393-method `Session`: page/layout/URL/scroll, the
  load lifecycle and application, forms, JS lifecycle, hit testing,
  find/hint/selection/reader state, text scaling, dark mode, content input and
  drawing, console data/evaluation/window state and dialog state, the document
  loader, headless automation and
  the one-shot dump handler. It reaches the network only through
  `protocol.Host` and has no terminal or chrome output of its own. Production
  imports only `internal/protocol`, `internal/textedit` and
  `internal/textwidth` (plus approved vendored modules). The engine
  has no fallback chrome, console, dialog, find-bar or hint-overlay rendering
  and no fallback shell/prompt input; the front end owns those. Nothing under
  `internal` is imported from outside this module.
- `internal/textedit/` — pure single-line editing shared by the engine's form
  controls and the front end's URL, dialog, find and console inputs.
- `internal/textwidth/` — pure terminal-cell text arithmetic shared by the
  engine's layout/paint and the front end's chrome, dialogs and console:
  rune/string widths, truncation, padding, tail windows and wrapping. Standard
  library only.
- `internal/movekeys/` — pure movement input table shared by page and console
  scrolling and the line/top/bottom subset of list selection: key lookup, line,
  half-page and page step deltas, and the wheel-step direction. Standard library
  only.
- **Target boundary.** The code splits into `internal/protocol` (wire types,
  portable stream mux, single-stream page/host dispatcher, serializable host
  and storage RPC, liveness Ping/Pong, codec, shared-state ports and limits;
  standard library only),
  `internal/textedit` (pure
  line editing; standard library only), `internal/textwidth` (pure
  terminal-cell text arithmetic; standard library only), `internal/movekeys`
  (pure viewport movement; standard library only), `internal/engine` (HTML,
  CSS, layout, paint, JS/DOM, PDF, images, document loading, find, selection,
  hints, reader),
  `internal/host` (network, cookies, cache, certificates, settings, request
  log), `internal/frontend` (terminal, input, chrome, ANSI encoding, native
  images, dialogs) and `internal/app` (wiring). Session back/forward history
  lives in the front end and the download/curl hint is presentation; the
  engine keeps only a memory-only page cache. MOB3 keeps no global visited list
  or download history. The allowed
  imports are `app` → `engine`, `host`, `frontend`, `protocol`, and each of
  `engine`, `frontend` → `protocol`, `textedit`, `textwidth`, `movekeys`,
  plus `host` → `protocol`; the
  front end and the host never
  import `engine`, and `engine` never imports `frontend`, `host`, `net` or
  `syscall`. A dependency test enforces the graph. The front end never names an
  engine type, and `internal/protocol` is canonical for the message set. The
  engine no longer imports `net/http`: JS `fetch`/XHR and cookie parsing use the
  host broker. The engine owns session construction and headless automation;
  the terminal loop and active-tab routing are `frontend.SessionLoop`
  (`frontend/tabs.go`). Per-tab state, normal header/footer chrome, the
  displayed URL ledger, content geometry, base shell keys, scrollbar, URL/vim
  prompt, page find, view/zoom windows, JS dialogs, the select popup,
  download/notice modals, link hints, the console (pane, scroll, composition,
  input, history, quit and find)
  and session history (list, index, scroll and the `about:history` snapshot)
  are `frontend.Tab` (`frontend/session.go`, with the shared window list in
  `frontend/overlay.go`) plus `frontend/chrome.go`,
  `frontend/prompt.go`, `frontend/find.go`, `frontend/menu.go`, `frontend/dialog.go`,
  `frontend/select.go`, `frontend/download.go`, `frontend/notice.go`, `frontend/hints.go`,
  `frontend/console.go` and `frontend/history.go`. The front end validates the committed URL scheme and
  renders only committed history entries for chrome. The host records each
  document fetch's final URL and exposes it to the front end by entry ID; the
  engine's `HistoryCommit` URL is ignored. With `-process=single` the engine
  stays in one Go process with the front end and host; the default
  `-process=per-tab` gives each interactive tab an engine child. In both modes
  tabs have separate JS runtimes and frames get origin checks, which is logical
  isolation, not memory, site or OS isolation. A compromised engine process is
  still inside the same trust model.
  Ownership, migration order and exit gates are in
  [docs/agents/architecture.md](docs/agents/architecture.md#target-boundary).
- **Files by subsystem within a package.** Every file belongs to one package
  and one subsystem. Large subsystems get their own file; moving code between
  files is a pure declaration move (keep doc comments, give each file only the
  imports it uses). Files carved out of a base file share its prefix
  (`layout.go` → `layout_grid.go`, `layout_flex.go`; `session.go` →
  `session_pdf.go`, `session_forms.go`), which matters when a bare name
  would be ambiguous (`forms`, `images`). Standalone subsystems keep their own
  name (`config_page.go`, `find.go`, `reader.go`, `hints.go`).
- Test files mirror their source names (`layout_grid_test.go`,
  `session_find_test.go`, …), with shared helpers in the base file
  (`session_test.go`, `layout_test.go`).

The ownership table for every file is in
[docs/agents/architecture.md](docs/agents/architecture.md); check it before
adding a file or moving a subsystem.

## 5. Commands

```sh
make                                                  # static binary at bin/mob3
make test                                             # go test -mod=vendor ./...
make fuzz                                             # 60 s per codec decoder
go vet -mod=vendor ./...
gofmt -w <touched files>                              # never vendor/
make vendor                                           # after go.mod/go.sum changes
make hooks                                            # install pre-commit size check
make docker                                           # multi-arch image

go test ./internal/engine -run TestLayoutFixtures -update   # goldens
go test ./internal/engine -run TestPDFFixtures -update      # PDF goldens
```

Build with `make` when CLI or binary behavior changes, then exercise the changed
path with a dump, PNG or focused test:

```sh
bin/mob3 -dump -width 100 -no-color "$URL"
bin/mob3 -width 152 -height 35 -png "$URL" > bin/test.png
```

Before handing work back: focused test, `go test -mod=vendor ./...`,
`go vet -mod=vendor ./...`, `gofmt` on touched Go files, and
`git diff --check`. Go 1.27 only; no new modules or cgo without an explicit user
decision.

## 6. Change protocol for agents

1. **Ask before deciding**, one question at a time, and ask again if an
   unanswered question still matters.
2. **Update the governing doc first** (or in the same change) when a
   requirement changes or a decision is made: `AGENTS.md`, the matching
   `docs/agents/` page, and `README.md`/`docs/` for user-visible changes.
3. **Keep the canonical sources canonical.** `mob3 -h`, `about:help` and
   `mcpToolDefinitions()` define flags, keys and tools; docs must match them.
4. **Keep Known limitations honest** (`docs/agents/limitations.md`); remove an
   item only when it is actually implemented.
5. **Add or update tests** in the same change, including the golden fixture for
   layout/rendering behavior, then run the validation in section 5. Delete
   `zz_*` debug tests.
6. **Report non-standard site behavior** instead of changing the engine to
   mimic it; fix engine behavior only for standard CSS/HTML.
7. **Never introduce external Go modules or cgo** without an explicit change to
   section 3.
8. **End every completed change with a one-line commit message**; do not create
   the commit unless asked.
