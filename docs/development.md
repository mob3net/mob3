# Development

## Requirements

- Go 1.27 or newer
- `make`
- no C toolchain: builds use `CGO_ENABLED=0`
- dependencies are committed under `vendor/`, so builds need no network

## Build and test

```sh
make
make test
make fuzz
go vet ./...
gofmt -w <files>
```

`make` writes a static `bin/mob3`. `make clean` removes it. `make vendor`
regenerates `vendor/` after a dependency change and must be committed.
`make hooks` points Git at `.githooks/`; its pre-commit hook rejects a staged
file above 2 MB (the generated `vendor/` tree is exempt), and CI runs
`.githooks/check-large-files --all` over every tracked file so `--no-verify`
cannot slip one through. Compress or split the file instead.

Tests must not require network access. Use the existing fake loader and
`httptest`-free helpers where possible. Add or update tests in the same change
as behaviour.

## Efficiency target

The product goal is to cover most of the features people actually use while
using far less CPU and memory than a mainstream browser. Concretely, when a
choice is ambiguous, prefer the cheap approximation over the exact but
expensive implementation, keep idle pages asleep, cap unbounded work, and never
trade a large steady-state footprint for a marginal feature. See the working
agreement in `AGENTS.md` for the rule in force.

Measure locally, before and after, whenever a change touches the draw loop,
timers, layout, the style pass or the JavaScript runtime. Comparing wall time,
peak RSS and idle CPU on the same machine and the same pages is what matters;
do not record hardware- or corpus-specific figures in the docs, because they go
stale and invite false comparisons.

```sh
make
ls -l bin/mob3
TIMEFORMAT='%3R s'; time (for i in $(seq 20); do bin/mob3 -version >/dev/null; done)
/usr/bin/time -v bin/mob3 -dump -width 177 -no-color page.html >/dev/null
```

Peak RSS in `-dump` includes Go GC headroom and is higher than a settled
interactive session, so compare like with like. For settled interactive RSS and
idle CPU, run the browser inside a pseudo terminal
(`script -qec "bin/mob3 FILE" /dev/null`), take the `mob3` PID from `pgrep`,
and compare fields 14+15 of `/proc/<pid>/stat` (utime + stime) over a window: an
idle page must not consume measurable CPU. Per-phase parse, layout and live-heap
attribution comes from a temporary `zz_*_test.go` benchmark following the
debugging recipe in `docs/agents/debugging.md`; delete it afterwards.

## Documentation

- `README.md` is the short landing page: what MOB³ is, how to run it, and what
  to expect.
- `docs/` holds long-form documentation.
- `mob3 -h` and the in-app `about:help` page are canonical for flags and keys.
  `docs/cli.md` and `docs/keys.md` must stay in sync.
- `docs/automation.md` documents the MCP tools. Tool names come from
  `mcpToolDefinitions()` in `internal/browser/mcp.go`.
- `AGENTS.md` is the agent entry point; domain-specific engine, UI, network,
  build and JS decisions live under `docs/agents/` and are not published by the
  site build.
- The GitHub Pages site is generated from `README.md` and `docs/*.md` by
  `.github/pages/build.sh`, using pinned `marked`.

Build the site locally:

```sh
.github/pages/build.sh /tmp/mob3-site
```

The script writes `index.html`, `docs/*.html`, `search-index.js`, `robots.txt`,
`sitemap.xml`, the favicon and social-preview files under `assets/`, a `CNAME`
for `mob3.net` and `.nojekyll`. Relative `.md` links are rewritten to `.html`
for the site. Docs pages share a generated sidebar, on-page “On this page”
contents list and a compact search box at the right edge of the header, with
Docs, GitHub, Releases and Contact links, a light/dark theme toggle after the
search and a mobile menu. On phones the search is dropped so the whole header
stays on one line. Every page carries a canonical URL, description, Open
Graph and Twitter card tags and JSON-LD structured data, so search results and
link previews use the intended title, summary and 1200×630 image. The assets
under `.github/pages/assets/` and the README screenshots under
`docs/screenshots/` are committed; replace `og-image.png` when the hero
screenshot changes.

## Repository layout

| Path | Purpose |
| --- | --- |
| `cmd/mob3/` | Executable entry point |
| `internal/browser/` | HTML, CSS, layout, rendering, fetching, JS, UI, automation and MCP |
| `docs/` | User documentation |
| `.github/pages/` | GitHub Pages template and build script |
| `vendor/` | Committed Go dependencies |
| `bin/` | Local build output (ignored) |

Key engine files are listed in `docs/agents/architecture.md`. The MCP and
automation split is:

- `automation.go` — headless browser commands and text snapshots
- `mcp.go` — MCP/JSON-RPC framing; delegates to `Automation.RunCommand`
- `cli.go` — flags, dump mode, interactive loop and `-mcp` startup

## Workflows

- `CI` runs gofmt, `go vet` and tests on pushes to `main` and pull requests.
- `PR` requires the exact CLA statement described in `CONTRIBUTING.md`.
- `Release binaries` builds seven targets on `v*` tags.
- `Docker` publishes the multi-arch image after a successful release.
- `GitHub Pages` rebuilds `mob3.net` from `README.md` and `docs/` when they
  change.

## Contributing

Read `CONTRIBUTING.md` before opening a pull request. Contributions require the
broad license-grant CLA so the project can remain available under AGPL-3.0-only
or a commercial license. Keep the working agreement in `AGENTS.md` and the
engine decisions under `docs/agents/` current whenever behaviour changes.
