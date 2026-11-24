# Networking and resources

Part of the MOB³ agent guidance. Start at [AGENTS.md](../../AGENTS.md) for the working
agreement, the non-negotiable constraints and the change protocol.

- Cookie jar and redirects via `net/http`; the page’s final URL is kept for
  relative links and records it for the front-end history lookup. The jar is memory-only, uses the public suffix
  list for domain scoping, and is shared by page loads, resources, XHR, fetch
  and `document.cookie`. HttpOnly cookies are hidden from JavaScript through a
  separate visible jar, and a script may neither replace nor delete a
  server-set HttpOnly cookie. SameSite is enforced for explicitly declared
  cookies: `Strict` only goes same-site, `Lax` also allows top-level
  `GET`/`HEAD` navigations, and `None` requires `Secure` over HTTPS. A cookie
  without SameSite keeps the historical send-everywhere behavior. Because the
  standard jar cannot see request context, the host removes the client jar per
  request, sets the filtered `Cookie` header itself, and stores each response
  hop's `Set-Cookie` manually.
- Page/resource cap: 12 MB. The body is read with a `LimitReader` of cap+1 so
  an oversized response is rejected rather than silently truncated, and data
  URIs are rejected when the encoded or decoded size exceeds the cap. Images
  are rejected before decoding when they exceed 25 megapixels, to avoid
  decompression bombs.
- **JS memory budget:** each guarded script/callback has a 2-second CPU timeout,
  and one sampler per active page runtime checks an absolute heap ceiling every
  75 ms. The ceiling is measured above the heap when the runtime was created;
  exceeding it interrupts the call that is running, so a page that retains
  allocations cannot reset the budget on every callback. It is 25% of
  `-mem-limit`/`GOMEMLIMIT` when one is set, otherwise 256 MB. There is no
  separate JS memory flag; raise `-mem-limit` to raise the ceiling. A runtime
  frozen in the back/forward cache stops its sampler.
  Load-time scripts also share an 8-second page budget and a cap of 2,000
  executed scripts: when either is reached, the running script is interrupted
  and the remaining ones are skipped with a console notice. A bare Esc or Ctrl-C
  interrupts the script currently blocking the main loop.
- **Process memory limit:** `-mem-limit` is off by default (`0`), which leaves
  memory unlimited and honors any `GOMEMLIMIT` from the environment. A positive
  value applies a soft heap limit with `debug.SetMemoryLimit`, so the runtime
  collects harder as the heap approaches it instead of letting the previous peak
  become the new floor. It is advice, not an allocation cap: it must stay above
  the working set (a heavy page plus the history cache) or the collector runs
  continuously. The image and inline-SVG budgets are percentages of this limit
  when one is set (12% of bytes for images in flight, 3% for inline-SVG pixels),
  with 128 MB and 8 M pixel fallbacks when the process is unlimited. Clearing it
  restores the startup limit (including
  `GOMEMLIMIT`) rather than overwriting it. The flag is process-wide and applies
  to dump and MCP runs too.
- **Scavenge after eviction:** when the history cache evicts a page holding at
  least a quarter of its budget, the browser requests exactly one
  `debug.FreeOSMemory()` (a collection plus scavenge) and runs it between
  frames, never inside a load, so the freed pages return to the OS and the
  footer `MEM` readout drops instead of waiting for the background scavenger.
  Small evictions do not trigger it.
- **Layout budget:** style and layout stop after the selected cooperative
  deadline (2 seconds on `standard`), and layout also stops after the selected
  fragment and document-row caps, bounding CPU and memory for pathological or
  generated documents. `-layout-budget=low|standard|high|none` selects the
  preset; `none` removes the safety limits entirely. The frame is rendered
  partially and marked truncated when a limit is hit.
- **Content-Encoding:** Go's transport decodes gzip transparently. Any other
  remaining `Content-Encoding` (brotli, zstd, …) is reported as an unsupported
  encoding error instead of being parsed as raw HTML/CSS/JS, because the
  vendored dependency policy has no Brotli decoder.
- **XHR and fetch:** JavaScript `XMLHttpRequest` and `fetch` always allow
  same-origin requests (default ports normalized). Simple cross-origin requests
  (GET/HEAD/POST with safelisted headers) are sent with an `Origin` header and
  succeed only when the response carries `Access-Control-Allow-Origin` for the
  page origin or `*`; cross-origin cookies are not sent. `fetch` with
  `mode: "no-cors"` is sent without `Origin` and skips the CORS response check;
  the engine receives an opaque response (status 0, empty body and headers).
  Non-simple no-cors requests still fail. Preflight, credentialed cross-origin
  requests and same-origin-to-cross-origin redirects are not supported.
  CORS cross-origin responses without a matching header fail, so cookies cannot
  be read cross-site through XHR or fetch. Every script request clones the
  session `http.Client` and sets its redirect policy on the copy, because the
  per-origin policy must never leak into navigations or concurrent image loads
  and a shared client cannot be mutated from a request goroutine. Form
  submissions are document requests: a non-GET form POST carries the submitting
  page's `Origin` header (GET navigations do not), captured before the load
  starts so a later navigation cannot change it.
- **Local-network guard:** a document loaded from a public `http`/`https`
  address may not fetch subresources, `fetch` or XHR from loopback, RFC 1918 or
  ULA private, link-local, CGNAT, `0.0.0.0/8` or reserved addresses. Literal
  hosts and local names (`localhost`, `*.localhost`, `*.local`) are rejected
  before the request and again on every redirect hop; names that merely resolve
  locally are rejected after DNS resolution at connect time. A redirect also
  retargets the request metadata so the dial guard and proxy decision inspect
  the host actually being contacted. A document that itself loaded from a local
  address keeps access, and top-level navigation is never blocked. Blocked
  requests show as `ERR` in the console request list. A resource cached while browsing a local
  document is not reused by a public document. With an environment HTTP proxy
  the target is resolved by the proxy, so only literal and local names are
  caught.
- **Mixed content:** a document loaded over `https` upgrades `http` image
  requests (including CSS background images) to `https`; a failed upgrade is not
  retried over HTTP. Other `http` subresources (scripts, stylesheets, modules,
  `@import`, XHR/fetch) are blocked, and a secure subresource redirect to HTTP is
  blocked. IP-literal image hosts are blocked rather than upgraded. Top-level
  navigation and documents loaded over `http` are unaffected. Blocked requests
  show as `ERR` in the console request list.
- **Local files:** `file:` URLs are rejected and bare paths are never probed on
  disk. Documents come from `http`, `https` or `about:` only; `data:` is
  resolved only as an inline subresource (image, stylesheet, script, module),
  never as a top-level document, so a page cannot navigate itself into an
  opaque-origin document that escapes the document policy. No document can load
  a local stylesheet, script, image or module.
- **PDF responses:** `application/pdf` (and the rare `application/x-pdf` /
  `application/acrobat`) opens in the text viewer. When the media type is
  missing or a generic binary type, the first 1 KB is sniffed for the `%PDF-`
  header, so a PDF served as `application/octet-stream` or a local `.pdf` still
  opens. Every other binary type keeps the download popup. The parser is
  bounded by the 12 MB page cap plus its own object, stream, page, text and
  time limits (500 pages, 4 s, 4 MiB of extracted text).
- **ES modules:** module scripts and their static dependencies are fetched with
  the same 12 MB per-resource cap, JavaScript MIME rule, request log and
  session cache as classic scripts, and resolve against the importing module's
  URL. The graph is capped at 256 modules, 12 MB of source and 32 levels of
  import depth; records are cached per URL so a shared dependency is evaluated
  once. Bare specifiers resolve through the document import map; the first
  valid map wins and `scopes` are ignored. `import.meta.resolve` and CORS
  checks on module requests are not implemented; dynamic imports resolve
  through the same graph and the page loader, so they are not blocked by the
  same-origin rule that XHR and `fetch` follow. Module sources are fetched while the page is
  applied, so a page whose graph is slow briefly delays its first paint.
- Charset from Content-Type, falling back to `<meta charset>` sniffing.
  UTF-8 is the default. Legacy single-byte labels decode through the vendored
  `x/text/encoding/charmap` tables: Latin-1/Windows-1252, ISO-8859-2/3/4/5/7/9/
  10/13/14/15/16 and Windows-1250/1251/1253/1254/1257, with common aliases
  (`latin2`, `cp1250`, `greek`, `cyrillic`, …). Unlisted charsets are read as
  UTF-8.
- Stylesheets from `<style>`, `<link rel=stylesheet>`, and inline styles.
  `@import` is followed recursively: imported sheets resolve against the
  importing sheet's URL, load before its own rules, combine their media
  condition with `@media` rules, skip cycles, and are capped at depth 8 and 40
  imported sheets. Semicolons inside a quoted `url(...)` (common in font URLs)
  stay part of the import URL and never end the statement. Parsing is cancellable and capped at 4 MiB of source, 50k
  rules, 256 KiB per declaration block, 4 KiB per selector and 1 second of
  parse time; repeated selector text is parsed once per sheet. This bounds
  CPU/GC cost on generated or hostile CSS. Inline `<style>` elements added or
  changed by scripts after load are collected before the next rebuild and
  ordered after the existing author rules; a dynamic `<link rel=stylesheet>` is
  not fetched, and removing or rewriting a style leaves its earlier rules in
  place.
- **Session resource cache:** CSS and JavaScript response bodies and decoded
  images share a session-only LRU byte cache. Its budget is 3% of
  `-mem-limit`/`GOMEMLIMIT`, or 32 MiB when the process is unlimited, with a
  2 MiB per-entry cap. Only successful GET CSS/JS responses and decoded images
  are stored; page
  documents, POST and `data:` reads are not cached, and
  `Cache-Control: no-store` responses are skipped. Hits avoid network requests;
  nothing is written to disk. The limit is editable in `about:config`. The
  separate back/forward page cache is described under History.
- Images load concurrently (max 6) and use the session resource cache per
  resolved URL for reloads. Initial loading also selects `srcset`/`picture`
  candidates, and images added or changed by page scripts (including lazy
  `data-src` hydration) are loaded after scripts and timer-driven rebuilds.
- Blocked responses are replaced with a locally generated “Site blocked” page
  with a `about:retry` **Try anyway** link; by default they do not run the
  site’s challenge scripts or load its assets. The generated page's inline
  stylesheet is collected through the normal author-stylesheet path, so it uses
  the shared built-in page shell instead of bare user-agent styles. Clicking the
  link re-fetches the page once with block detection disabled, and
  `-ignore-blocks` disables the detection for all loads.
- Default User-Agent: **`MOB3/1.0 (+terminal; +https://mob3.net)`**. MOB³ identifies itself
  rather than impersonating another browser; UA-sniffing sites may therefore
  serve simplified or no-JavaScript pages. `-ua` overrides it. Keep this string
  documented in `docs/cli.md`, the built-in help page and this page whenever it
  changes.
- HTTPS uses Go’s TLS; there is no certificate UI or proxy configuration UI.
- **TLS trust store:** `-certs` selects the roots used for HTTPS. `system`
  (the default) keeps the platform store Go reads; `embedded` uses the official
  Mozilla NSS bundle from `golang.org/x/crypto/x509roots/fallback`. Certificate
  files are not read: any other value is rejected. The module's `fallback`
  package is imported too, so `x509.SetFallbackRoots` supplies the same roots
  when the platform has none at all; a present but stale store is not
  overridden. The embedded roots keep Mozilla's distrust-after constraints
  (`AddCertWithConstraint`), so a constrained root stops validating
  certificates issued after that date. The selected source replaces the
  platform store instead of merging, so a machine whose CA package is stale or
  missing verifies modern sites like an updated one. Each source is labelled in
  `about:config`, where `system` and `embedded` can be switched for the
  session.
  A switch applies to later requests and swaps the transport after closing the
  old idle connections; a rejected value keeps the previous store with the
  reason shown on the page. The bundle is refreshed by bumping the module and
  re-vendoring, and is only as fresh as the pinned version. Revocation is not
  checked in any mode.
