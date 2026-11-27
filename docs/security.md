# Security and privacy

> **Warning:** **MOB3 is not a secure browser.** It has no sandboxing, process
> or site isolation, CSP enforcement, certificate UI, phishing protection or
> security audit. Treat every page as code running beside the browser. Do not
> use MOB3 for passwords, accounts, payments or confidential data.

## What it does not do

- No sandbox, site isolation or permission prompts.
- No memory, site or OS isolation. By default tabs are separate engine child
  processes with their own JavaScript runtime and history, over parent-owned
  shared origin storage, but they still trust the same parent host and protocol
  values; iframes remain inert stubs. The child process is fault isolation,
  not a security boundary, and `-process=single` puts every tab back in one
  address space. An engine bug or a sobek escape can still cross tab boundaries.
- No CSP enforcement, certificate warnings or certificate management UI.
  `-certs` changes which root bundle is trusted; it is not certificate
  inspection, revocation checking or pinning.
- No phishing, malware, tracker or fingerprinting protection.
- No proxy configuration UI. Standard `HTTP_PROXY`, `HTTPS_PROXY` and
  `NO_PROXY` environment variables are honored by the host transport; see
  [Proxies](#proxies).
- No private/incognito concept: every session is ephemeral. Tabs may run in
  separate engine processes by default, but that is fault isolation only, not
  memory or site isolation.

## Need more isolation?

MOB3 does not sandbox pages itself. If you need a more isolated environment,
run MOB3 in a container or a dedicated virtual machine with only the network
and filesystem access you intend. Docker is a common way to do this, but
container and VM configuration is outside this project's support scope.

## Tab isolation (iframes are inert)

Interactive tabs run one engine child process per tab by default, and an MCP
automation session runs its engine in a child too; `-process=single` keeps them
inside one Go process. The process layout and spawn handshake are described in
[the process model](architecture.md#the-process-tree). This is fault isolation,
not a sandbox:

- A tab is one protocol session with its own engine session, page, layout,
  JavaScript runtime and back/forward history.
- `localStorage` is shared per origin across tabs; `sessionStorage` is per tab
  and origin; cookies remain host-owned and domain-scoped.
- Single mode uses one shared budget for all JS runtimes, page caches,
  image/SVG work and the MCP engine. Per-tab mode gives each tab child and the
  MCP engine child its own process-wide `-mem-limit` cap. Zoom stays
  browser-global.
- Only the active tab draws. A background tab keeps loading and updates its own
  state, history and title, but its timers and deferred image work pause.
- A crashed MCP engine does not stop the MCP server, but the server does not
  respawn it: later tool calls report the crash until the client restarts the
  server.

No DOM or sobek value crosses a tab boundary. This is logical isolation, not
memory or site isolation. A compromised engine or a JavaScript-engine escape
can read across tabs, and there is no OS sandbox or Spectre mitigation.

Iframes are a permanent non-goal. Iframe elements have inert stub documents,
`contentDocument` is null even for a same-origin frame, `frameElement` is null,
and there is no DOM bridge. There is no child browsing context, frame tree or
cross-frame DOM access, and none is planned.

## No persistence

The running browser does not create or update configuration, history, cookie,
cache, log or download files. Closing MOB3 saves nothing.

- History, cookies, `localStorage`/`sessionStorage`, the image cache and all
  JavaScript state live in memory only.
- The download popup only shows a `curl` command; it writes nothing.
- The binary opens no application files for reading or writing: there are no
  config, bookmark, certificate, cache, history or download files. It writes
  nothing to disk and never probes or reads a page, bundle or path.
- OS-level reads by Go's TLS, DNS and timezone libraries are not application
  storage. By default TLS roots come from the platform store; `-certs` can
  switch to the CA bundle compiled into the binary. No PEM file is read.
- The memory footer uses Go's runtime statistics for the active engine
  process; no `/proc` file is sampled. Per-tab mode therefore shows the active
  tab's child process, not the whole browser.

## Cookies and storage

Cookies are session-only and shared by page loads, resources, XHR, `fetch` and
`document.cookie`. HttpOnly cookies are hidden from JavaScript, and a script
cannot replace or delete a server-set HttpOnly cookie. Explicit `SameSite`
values are enforced: `Strict` cookies are sent only to the same site, `Lax`
also on top-level `GET`/`HEAD` navigations, and `None` only when `Secure` over
HTTPS. A cookie with no `SameSite` keeps the legacy send-everywhere behavior.
Non-GET form submissions carry the submitting page's `Origin`, so a server can
reject cross-site posts. A `Secure` cookie is accepted only over HTTPS, and the
`__Secure-`/`__Host-` prefixes are enforced: the former needs `Secure`, the
latter needs `Secure`, `Path=/` and no `Domain`. `localStorage` and
`sessionStorage` are in-memory and do not persist across restarts:
`localStorage` is shared per origin across tabs, while `sessionStorage` is per
tab and origin. Each area is capped at 5 MiB and the whole browser context at
32 MiB. Opaque origins get a private area that is not shared with any other
document. One cookie is capped at 4 KiB accounting size, a domain at 180
cookies, and the jar at 1024 cookies / 4 MiB; a cookie that would exceed a bound
is not stored.

## Clipboard

Clipboard use is write-only and user-initiated: `y`/`Y`/`Ctrl-Y`, a
right-click link URL, console `y`, or the download prompt write through OSC 52.
Page JavaScript cannot read or write the clipboard (`navigator.clipboard` is
absent and `document.execCommand("copy")` is false). The engine emits plain text;
the front end allows one clipboard event per copy action, drops unsolicited
events, strips terminal and display-format controls, caps the payload at
100 KiB and reports "Clipboard unavailable" when the terminal write fails. MOB3
never reads the clipboard. Terminal support for OSC 52 still varies.

## Cross-origin requests

Pages can load cross-origin scripts, styles and images like a normal browser,
and share the cookie jar for those requests. JavaScript `fetch` and
`XMLHttpRequest` allow same-origin requests and only simple CORS cross-origin
requests with a matching `Access-Control-Allow-Origin`; preflight and
credentialed cross-origin requests are unsupported.

A document loaded from a public `http`/`https` address cannot fetch
subresources, `fetch` or XHR from loopback, private, link-local, CGNAT,
`0.0.0.0/8` or reserved addresses. Literal local hosts and local names are
rejected before the request and on every redirect hop; names that only resolve
locally are rejected after DNS resolution, and every connection is re-checked
before it is reused from the pool. Only the final document address decides local
access, so a public page reached through a local redirect hop is still public.
Documents that themselves loaded from a local address keep access, and top-level
navigation is not blocked.
Blocked requests appear as `ERR` in the console request list.

MOB3 upgrades HTTP image requests on HTTPS pages to HTTPS and does not retry
them over HTTP; HTTP scripts, stylesheets, modules, `@import`, XHR/fetch and
secure-subresource redirects to HTTP are blocked. IP-literal image hosts are
blocked rather than upgraded. Top-level navigation to HTTP remains allowed.

## Blocked sites and challenges

On 401/403/429/503 responses, MOB3 looks for common anti-bot/CAPTCHA signatures
and shows a generic “Site blocked” page with the reason, status and URL. The
site's challenge scripts are not executed unless you follow the one-shot
**Try anyway** link, or start with `-ignore-blocks`.

Challenge pages can run arbitrary script after that bypass. MOB3 cannot
complete most browser checks, and it is not a security boundary.

## Local files and downloads

Local file pages are not supported. `file:` URLs are rejected, bare paths are
never probed on disk, and every document comes from `http`, `https` or
`about:`. `data:` is accepted only as an inline subresource (images,
stylesheets, scripts), never as a document. Remote pages therefore cannot use
the viewport, a stylesheet, a script, an image or XHR to read a local file.

MOB3 never saves downloads. A download link opens a popup with a
ready-to-copy `curl` command without fetching the file, except for a `.pdf`
download, which is fetched and shown in the text viewer. Running the `curl`
command is your choice and outside MOB3.

## TLS trust store

HTTPS verification uses the platform root store by default. If the platform
provides no roots at all, the Mozilla NSS bundle compiled into the binary from
`golang.org/x/crypto/x509roots/fallback` is used automatically.
`-certs=embedded` selects that same bundle explicitly, replacing a stale
platform store. No certificate file is read. The embedded
bundle keeps Mozilla's distrust-after dates, so a constrained root stops
validating certificates issued after that date. Revocation is still not checked
in any mode, and there is no certificate UI or warning flow.

## Proxies

The host transport uses Go's `http.ProxyFromEnvironment`, so
`HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY` work without a CLI flag or UI. HTTP
proxies, HTTPS proxies and SOCKS5 proxy URLs are supported by the transport.
The variables are read at process start.

With a proxy, the proxy resolves and contacts the destination. The
local-network guard cannot inspect that target, so the proxy is a deliberate
exception to the private-network check. The proxy can see plain HTTP traffic
and destination hostnames; HTTPS remains end-to-end encrypted unless the proxy
terminates TLS. Proxy credentials in environment variables are visible to
child processes and may be visible through process-environment inspection.
This is best-effort networking, not a security boundary.

## Automation

`-mcp` runs a local, unauthenticated automation server over stdin/stdout. Any
process connected to that stdin/stdout can navigate, click, type and evaluate
JavaScript in the page. Do not expose the MCP server as a network service and
do not connect untrusted clients. Sessions and storage remain memory-only.

## Media decoding

Images and inline SVG are treated as untrusted input. Responses are capped at
12 MB, raster images above 25 megapixels are rejected before decoding, and SVG
rasterization is capped at 1024 px. Decoder dependencies are kept at patched
releases and scanned with `govulncheck`; a panic inside a raster or SVG decoder
is contained and shown as a broken image instead of crashing the browser. SVG
strokes with a pathological dash pattern fall back to a solid stroke, and a
stroke outline beyond a fixed point budget is dropped. These limits bound
resource use and crashes, not the general risk of rendering attacker-controlled
content.

CSS selector matching memoizes repeated ancestor states and runs under a
per-selector or per-query step budget. A pathological selector is treated as
non-matching instead of freezing the engine, and the style deadline and script
abort flag are checked inside the match. UA rules have their own small budget,
so exhausting author matching cannot expose script/style raw text.

PDF responses are parsed in-repo rather than by a third-party renderer. The
reader is bounded (500 pages, 4 seconds of parse time, 4 MiB of extracted
text, 32 MiB per decoded stream, 64 MiB of retained page content), stops on
the requesting page's cancellation, rejects encrypted documents, and contains
panics, so a malformed file degrades to a note instead of a crash. It never
rasterizes a page and writes nothing to disk. A panic in the engine goroutine
is recovered by the session transport and shown in the crashed tab, and an
engine that stops answering the liveness probe is force-killed and shown the
same way. Fatal
runtime errors, such as a stack overflow, are not recoverable: per-tab mode
contains one to the tab's child process and shows its captured message, while
`-process=single` terminates the browser and may leave the terminal in raw or
alternate-screen mode. Like the rest of the engine it is not audited and is not
a security boundary.

## Memory and caches

The back/forward history-cache pool is 12% of `-mem-limit`/`GOMEMLIMIT` (or
128 MB when unlimited), divided across active tabs. Each tab evicts its least
recently used pages first, and a page heavier than half its share is never
cached. When a heavy cached page is evicted, one collection plus scavenge runs
between frames so its memory returns to the OS. The separate subresource cache
keys CSS, JavaScript and raw image bodies by top-level site as well as URL, so
one site cannot reuse another site's cached response, and a response that varies
on `Cookie` (or `Vary: *`) is not cached at all. Decoded images remain keyed by
URL.

`-mem-limit` sets an optional soft limit on the Go heap, making the runtime
collect harder as memory approaches it. It is off by default (`0`), so memory is
unlimited and any `GOMEMLIMIT` environment setting applies. Single-mode tabs
divide the image/SVG pools (12% and 3% of the limit when set, with conservative
fallbacks otherwise) because they share one process. With `-process=per-tab`,
every tab child is its own process and receives the full `-mem-limit`, as does
the parent, so the browser total is not bounded by one limit. These limits are
best-effort resource bounds, not a sandbox.

## JavaScript safety limits

Each script and callback runs under a 2-second sobek interrupt watchdog and a
per-runtime absolute heap ceiling drawn from the shared JS pool (25% of
`-mem-limit`/`GOMEMLIMIT`, else 256 MB, divided across active sessions). The
interrupt is observed by bytecode, not by a native regular-expression match, so
catastrophic backtracking can exceed it until the liveness probe force-stops the
tab (see the known limitations). One sampler per active runtime checks the
ceiling every 75 ms and interrupts only a call that is running, so a page
cannot reset its budget on every timer callback. A page's load-time scripts also
share an 8-second budget and a 2,000-script cap, and Esc or Ctrl-C interrupts the
script currently blocking the loop and cancels its in-flight requests; closing
the tab does the same before
releasing it. Timer counts and per-tick callbacks are capped. Go-side copies of
page data are capped too: the console log at 8 KiB per line and 512 KiB total,
DOM text and attribute values at 1 MiB each and 64 MiB per runtime, script-visible
nodes at 200,000, and request-log URLs at 2 KiB. Interactive
`alert`/`confirm`/`prompt` dialogs are limited to 20 per page load and 8 KiB
each; later calls return the non-interactive defaults, so a script cannot reopen
a modal forever. These limits protect against runaway scripts, not against
malicious pages in general.
