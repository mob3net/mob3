# Security and privacy

> **Warning:** **MOB³ is not a secure browser.** It has no sandboxing, process
> or site isolation, CSP enforcement, certificate UI, phishing protection or
> security audit. Treat every page as code running beside the browser. Do not
> use MOB³ for passwords, accounts, payments or confidential data.

## What it does not do

- No sandbox, process isolation, site isolation or permission prompts.
- No CSP enforcement, certificate warnings or certificate management UI.
  `-certs` changes which root bundle is trusted; it is not certificate
  inspection, revocation checking or pinning.
- No phishing, malware, tracker or fingerprinting protection.
- No proxy configuration UI.
- No private/incognito concept: every session is ephemeral, but pages and
  scripts are not isolated from each other inside the process.

## No persistence

The running browser does not create or update configuration, history, cookie,
cache, log or download files. Closing MOB³ saves nothing.

- History, cookies, `localStorage`/`sessionStorage`, the image cache and all
  JavaScript state live in memory only.
- The download popup only shows a `curl` command; it writes nothing.
- Disk reads are limited to explicit local `file:` pages and their resources,
  an explicitly supplied `-bookmarks` file, an explicitly supplied `-certs`
  PEM bundle, and `/proc/self/statm` for the Linux memory footer.
- OS-level reads by Go's TLS, DNS and timezone libraries are not application
  storage. By default TLS roots come from the platform store; `-certs` can
  switch to the CA bundle compiled into the binary or to a named PEM file.

## Cookies and storage

Cookies are session-only and shared by page loads, resources, XHR, `fetch` and
`document.cookie`. HttpOnly cookies are hidden from JavaScript, and a script
cannot replace or delete a server-set HttpOnly cookie. `SameSite` is
stored but not enforced. `localStorage` and `sessionStorage` are in-memory maps
and do not persist across restarts.

## Cross-origin requests

Pages can load cross-origin scripts, styles and images like a normal browser,
and share the cookie jar for those requests. JavaScript `fetch` and
`XMLHttpRequest` allow same-origin requests and only simple CORS cross-origin
requests with a matching `Access-Control-Allow-Origin`; preflight and
credentialed cross-origin requests are unsupported.

## Blocked sites and challenges

On 401/403/429/503 responses, MOB³ looks for common anti-bot/CAPTCHA signatures
and shows a generic “Site blocked” page with the reason, status and URL. The
site's challenge scripts are not executed unless you follow the one-shot
**Try anyway** link, or start with `-ignore-blocks`.

Challenge pages can run arbitrary script after that bypass. MOB³ cannot
complete most browser checks, and it is not a security boundary.

## Local files and downloads

Remote pages cannot navigate the viewport to `file:` URLs or load local file
resources. Local documents can load local stylesheets, scripts and images.
Only regular files are read; devices and FIFOs are rejected.

MOB³ never saves downloads. A download link opens a popup with a
ready-to-copy `curl` command without fetching the file, except for a `.pdf`
download, which is fetched and shown in the text viewer. Running the `curl`
command is your choice and outside MOB³.

## TLS trust store

HTTPS verification uses the platform root store by default. If the platform
provides no roots at all, the Mozilla NSS bundle compiled into the binary from
`golang.org/x/crypto/x509roots/fallback` is used automatically.
`-certs=embedded` selects that same bundle explicitly, replacing a stale
platform store, and `-certs=FILE.pem` uses a bundle you supply. The embedded
bundle keeps Mozilla's distrust-after dates, so a constrained root stops
validating certificates issued after that date. Revocation is still not checked
in any mode, and there is no certificate UI or warning flow.

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

PDF responses are parsed in-repo rather than by a third-party renderer. The
reader is bounded (500 pages, 4 seconds of parse time, 4 MiB of extracted
text, 32 MiB per decoded stream), rejects encrypted documents, and contains
panics, so a malformed file degrades to a note instead of a crash. It never
rasterizes a page and writes nothing to disk. Like the rest of the engine it is
not audited and is not a security boundary.

## Memory and caches

The back/forward history cache is bounded by estimated page weight
(`-history-cache`, 128 MB by default): the least recently used pages are
evicted first, and a page heavier than half the budget is never cached. When a
heavy cached page is evicted, one collection plus scavenge runs between frames
so its memory returns to the OS.

`-mem-limit` sets an optional soft limit on the Go heap, making the runtime
collect harder as memory approaches it. It is off by default (`0`), so the Go
runtime default and any `GOMEMLIMIT` environment setting apply. These limits are
best-effort resource bounds, not a sandbox.

## JavaScript safety limits

Each script and callback runs under a 2-second sobek interrupt watchdog and a
configurable heap budget (`-js-memory`, default 256 MB). A page's load-time
scripts also share an 8-second budget and a 2,000-script cap, and Esc or Ctrl-C
interrupts the script currently blocking the loop. Timer counts and per-tick
callbacks are capped. These limits protect against runaway scripts, not against
malicious pages in general.
