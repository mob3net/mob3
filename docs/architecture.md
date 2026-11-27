# Architecture

MOB3 is a small web browser for the terminal, written in Go. It has its own
HTML parser, CSS cascade, layout engine, renderer and JavaScript runtime, and
it can run each tab's engine in a separate process.

This page is the map: the process model, the package boundaries, the rendering
and JavaScript pipelines, and the tradeoffs behind them. It is deliberately an
overview, not a file-by-file reference.

## Design goals

- **One static binary.** No cgo, no runtime service, no helper binary. The same
  executable is the browser, the per-tab worker and the MCP server.
- **A terminal cell is the layout unit.** Pages are laid out in columns and rows
  and painted as ANSI, not as a pixel surface.
- **Memory-only operation.** History, cookies, storage, caches and logs live in
  memory. Closing MOB3 saves nothing.
- **Idle stays idle.** No polling, prefetching or perpetual repaint loops. A
  background tab pauses its timers and image work.
- **Fault isolation where it is cheap.** Interactive tabs and MCP get their
  engine in a child process by default, so a fatal engine error does not take
  down the front end. This is not a security sandbox.
- **Capability per unit of resource.** Prefer the inexpensive approximation over
  the exact but expensive implementation: text PDFs instead of rasterized pages,
  block backgrounds instead of inline ones, half-block images instead of sixel.
- **Text first.** HTML, CSS, forms, links, find, selection, reader mode and
  JavaScript matter more than pixel-perfect rendering.

## The process tree

`-process=per-tab` is the default. The parent owns the terminal and the network;
each interactive tab and each MCP automation session gets its own engine child.

```text
mob3 parent process
│  terminal, input, tabs, chrome, history
│  host: network, TLS, cookies, cache, settings
│  storage broker, history-entry IDs
│
├── tab 1 engine child
│     DOM, JavaScript VM, layout, frames, PDF
├── tab 2 engine child
│     DOM, JavaScript VM, layout, frames, PDF
└── -mcp mode instead uses one automation engine child (no tabs)
      DOM, JavaScript VM, layout, frames, PDF
```

`-process=single` keeps everything in one Go process:

```text
one mob3 process
  terminal, input, tabs, chrome, history
  host: network, TLS, cookies, cache, settings
  tab engines + MCP engine: DOM, JavaScript VM, layout, frames, PDF
  one -mem-limit, one address space
```

Dump and PNG modes stay in-process in both modes. They are one-shot jobs and
the terminal is never put into raw mode, so a process boundary buys little
there.

### How a tab starts its engine

A new tab does not start a second browser binary that has to be installed or
found. It re-executes the running binary:

```text
parent, on a new tab
  │
  ├─ os.Executable() + os.Args[1:]      same binary, same flags
  ├─ worker marker + JSON geometry      tab id, size, zoom, graphics
  ▼
child process
  ├─ parses the normal CLI flags        one option surface
  ├─ opens host and storage RPC         over stdin/stdout
  ├─ builds one engine session
  └─ sends Ready
  ▼
parent
  ├─ waits for Ready                    bounded handshake
  ├─ sends Init and Navigate
  └─ receives frames, state and events
```

The parent holds one end of two anonymous pipes. A small binary multiplexer
carries page commands and events, host requests, storage requests and liveness
pings over that pair, so no TCP port or platform-specific file descriptor is
needed. The child never writes the terminal; it returns frames and data, and
the front end remains the only terminal writer.

The same worker serves MCP: the parent runs the JSON-RPC server on stdio and
starts an automation engine child. `-process=single` replaces the child with the
in-process transport for both tabs and MCP.

### Closing and failure

A normal tab close asks the child to shut down, waits for a short grace period,
then kills it if it does not exit. A wedged tab is force-discarded without
waiting for its handler. Child stderr is captured with a bounded, sanitized head
and tail and shown in the crashed tab; it is never written directly to the
terminal.

An engine crash is contained to that child. The tab stays in the strip marked
`Crashed`, other tabs keep running, and an MCP server stays alive but reports
the crash on later tool calls until the client restarts it. In `single` mode
the engine shares the browser process, so the same fatal error takes the whole
browser down; terminal cleanup may not run.

## Isolation and containment

Each tab is one protocol session with its own page, layout, JavaScript runtime
and back/forward history. The parent shares the host, cookies, cache, settings
and origin storage, and receives each document's final URL by entry ID, so the
address bar never trusts a URL supplied by the engine.

A background tab keeps loading and updating its state, title and history, but
it does not draw, run JavaScript timers or stream deferred images.

This is **logical origin isolation plus fault isolation, not a security
boundary**. Tabs still share the parent host, storage broker and trust model. A
compromised engine can read across tabs, and there is no OS sandbox, site
isolation or Spectre mitigation. `-process=single` puts every tab back in one
address space.

## Package boundaries

The source is split so that the network, the terminal and the page engine do not
reach into each other:

| Package | Owns |
| --- | --- |
| `internal/protocol` | Wire types, binary codec, stream multiplexer, host and storage RPC, transports, limits. Standard library only; it never imports another project package. |
| `internal/engine` | HTML parsing, CSS, layout, painting, DOM, JavaScript, PDF extraction, document loading, find, selection, hints and reader mode. It reaches the network only through a host port. |
| `internal/host` | Sockets, TLS, DNS, proxies, cookies, the resource cache, settings and the request log. |
| `internal/frontend` | The terminal, input decoding, chrome, ANSI encoding, native images, dialogs, tabs and history. |
| `internal/app` | Wiring: flags, mode dispatch, process launch and the interactive loop. |
| `internal/textedit`, `internal/textwidth`, `internal/movekeys` | Pure line editing, terminal-cell arithmetic and viewport movement. |

The dependency direction is enforced by a test: the engine never imports the
front end, host or `net/http`; the front end never names an engine type; and the
protocol package stays independent. Outside the standard library, the engine's
only production imports are the approved vendored modules.

That boundary is also what makes the process transport possible. Everything
crossing a tab boundary is data: frames, events, placements, clipboard actions
and host calls. No DOM node, `image.Image`, certificate pool or JavaScript value
crosses it.

## Rendering

```text
HTML bytes
  → parse and build the document
  → selectors, cascade, computed styles
  → block and inline layout in terminal cells
  → paint into frames
  → front end encodes ANSI and writes the terminal
```

The engine owns text measurement, line breaking, tables, floats, flex rows,
positioning, backgrounds, borders and the text cursor. The front end owns the
terminal encoding and presents only the active tab. Scaled text and native
images are capabilities negotiated with the terminal.

Optional images use kitty or iTerm2 native graphics first, then colored
half-block art; there is no sixel. PDFs are read by an in-repo text extractor
and shown as text pages, not rasterized. Images are off by default.

## JavaScript

Page scripts run in a sobek runtime, one VM per top-level frame. Classic scripts
run in document order, ES modules and import maps are supported, and external
scripts require a JavaScript MIME type.

The API surface is a practical subset: DOM queries and mutation, events, forms,
timers, `fetch`/XHR through the host broker, in-memory storage, cookies and
common compatibility shims. There is no WebGL, no worker execution, no real
canvas pixels, no live `MutationObserver`, and no real iframe browsing context.
A liveness watchdog can interrupt a long-running script; a native regular
expression that cannot be interrupted is a documented limit.

## Networking and storage

The host is the network boundary. The engine never opens a socket: document,
CSS, script, image and form requests go through a host port, locally or over
RPC. TLS, redirects, cookies, proxies and the resource cache live on the host
side; the engine classifies the bytes and headers it receives.

The parent also owns the storage broker: `localStorage` is shared by origin
across tabs and `sessionStorage` is per tab and origin. The engine keeps a
memory-only page cache for back/forward restoration.

MOB3 writes no application files. Documents come from the network or `about:`
pages; `file:` URLs are rejected, and `data:` is only an inline subresource. The
only filesystem access is what the Go TLS, DNS and timezone libraries do, plus
the self-exec used for worker processes.

The host applies a local-network guard for subresources, upgrades or blocks
mixed content and reports blocked requests in the console. This is not a
sandbox, and top-level navigation is not blocked. See
[Security and privacy](security.md) for the full model.

## Memory and efficiency

`-mem-limit` derives the soft heap limit and the JS (25%), image (12%), SVG
(3%), resource-cache (3%) and history-cache (12%) budgets. In `single` mode one
`BudgetManager` divides those pools across tabs; in per-tab mode each engine
child has its own process and its own limit, so the browser total is not capped
by one number. Dump and PNG use the parent process.

The design avoids work that does not produce a visible result: no prefetching,
no polling, no background render loop, no automatic disk cache, and no drawing
for background tabs. Long pages and unbounded inputs are capped; the dump path
truncates by rows, the protocol has message and image limits, and the MCP tools
page large results instead of returning them without bound.

## Tradeoffs

| Decision | What it costs |
| --- | --- |
| A separate engine process per tab | Extra process memory and startup work; `-mem-limit` is per process in this mode. |
| A text-first CSS and JavaScript subset | Modern JavaScript-heavy apps may render partially or not at all. |
| Terminal-cell layout | Approximate CSS; not a pixel-perfect browser. |
| Memory-only state | Nothing survives a close, by design. |
| Images off by default | The fastest configuration is also the one without pictures. |
| Text PDFs | Scanned pages, figures and annotations are not rendered. |
| No sandbox or site isolation | The architecture is not a security boundary. |
| Report-only MCP crash recovery | The server survives an engine crash but does not respawn it. |

## Further reading

- [Command line](cli.md) for every flag and its limits.
- [Security and privacy](security.md) for the trust model and network policy.
- [Automation with MCP](automation.md) for the headless surface.
- [Development](development.md) for builds, tests and release steps.

The repository also carries file-by-file ownership notes for contributors:
[docs/agents/architecture.md](https://github.com/mob3net/mob3/blob/main/docs/agents/architecture.md).
