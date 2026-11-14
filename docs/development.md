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
go vet ./...
gofmt -w <files>
```

`make` writes a static `bin/mob3`. `make clean` removes it. `make vendor`
regenerates `vendor/` after a dependency change and must be committed.

Tests must not require network access. Use the existing fake loader and
`httptest`-free helpers where possible. Add or update tests in the same change
as behaviour.

## Documentation

- `README.md` is the short landing page: what MOB³ is, how to run it, and what
  to expect.
- `docs/` holds long-form documentation.
- `mob3 -h` and the in-app `about:help` page are canonical for flags and keys.
  `docs/cli.md` and `docs/keys.md` must stay in sync.
- `docs/automation.md` documents the MCP tools. Tool names come from
  `mcpToolDefinitions()` in `internal/browser/mcp.go`.
- The GitHub Pages site is generated from `README.md` and `docs/*.md` by
  `.github/pages/build.sh`, using pinned `marked`.

Build the site locally:

```sh
.github/pages/build.sh /tmp/mob3-site
```

The script writes `index.html`, `docs/*.html`, `search-index.js`, a `CNAME` for
`mob3.net` and `.nojekyll`. Relative `.md` links are rewritten to `.html` for
the site. Docs pages share a generated sidebar, on-page “On this page” contents
list and header search, with a light/dark theme toggle and a mobile menu.

## Repository layout

| Path | Purpose |
| --- | --- |
| `cmd/mob3/` | Executable entry point |
| `internal/browser/` | HTML, CSS, layout, rendering, fetching, JS, UI, automation and MCP |
| `docs/` | User documentation |
| `.github/pages/` | GitHub Pages template and build script |
| `vendor/` | Committed Go dependencies |
| `bin/` | Local build output (ignored) |

Key engine files are listed in `AGENTS.md`. The MCP and automation split is:

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
or a commercial license. Keep the working agreement and engine decisions in
`AGENTS.md` current whenever behaviour changes.
