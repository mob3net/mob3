# Networking and resources

Part of the MOB³ agent guidance. Start at [AGENTS.md](../../AGENTS.md) for the working
agreement, the non-negotiable constraints and the change protocol.

- Cookie jar and redirects via `net/http`; the page’s final URL is kept for
  relative links and history. The jar is memory-only, uses the public suffix
  list for domain scoping, and is shared by page loads, resources, XHR, fetch
  and `document.cookie`. HttpOnly cookies are hidden from JavaScript through a
  separate visible jar, and a script may neither replace nor delete a
  server-set HttpOnly cookie. The SameSite attribute is stored but not enforced.
- Page/resource cap: 12 MB. Local files are read with a `LimitReader`, only
  regular files are accepted (no `/dev/*`/FIFOs), and data URIs are rejected
  when the encoded or decoded size exceeds the cap. Images are rejected before
  decoding when they exceed 25 megapixels, to avoid decompression bombs.
- **JS memory budget:** each guarded script/callback has a 2-second CPU timeout
  and a best-effort heap-delta watchdog, 256 MB above its starting heap by
  default. `-js-memory` sets the budget in MB (0 disables the watchdog, leaving
  the CPU timeout in place). Exceeding the budget interrupts the script with an
  error. Load-time scripts also share an 8-second page budget and a cap of 2,000
  executed scripts: when either is reached, the running script is interrupted
  and the remaining ones are skipped with a console notice. A bare Esc or Ctrl-C
  interrupts the script currently blocking the main loop.
- **Process memory limit:** `-mem-limit` is off by default (`0`), which leaves
  the Go runtime default in place, including `GOGC` and any `GOMEMLIMIT` from
  the environment. A positive value applies a soft heap limit with
  `debug.SetMemoryLimit`, so the runtime collects harder as the heap approaches
  it instead of letting the previous peak become the new floor. It is advice,
  not an allocation cap: it must stay above the working set (a heavy page plus
  the history cache) or the collector runs continuously. A limit set later
  (including from `about:config`) replaces `GOMEMLIMIT`. The flag is
  process-wide and applies to dump and MCP runs too.
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
  page origin or `*`; cross-origin cookies are not sent. Preflight, credentialed
  cross-origin requests and same-origin-to-cross-origin redirects are not
  supported. Cross-origin responses without a matching CORS header fail, so
  cookies cannot be read cross-site through XHR or fetch.
- **Local files:** `file:` resources (stylesheets, scripts, images) are fetched
  only when the document itself was loaded from `file:`. Remote (http/https/
  data) documents cannot load file resources, navigate the viewport to `file:`
  URLs, or XHR local files. Local documents retain access to local resources.
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
  images share a session-only LRU byte cache. The default budget is 32 MiB with
  a 2 MiB per-entry cap; `-cache` sets the budget in MB and `0` disables it.
  Only successful GET CSS/JS responses and decoded images are stored; page
  documents, POST, `data:` and `file:` reads are not cached, and
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
  Mozilla NSS bundle from `golang.org/x/crypto/x509roots/fallback`; any other
  value names a PEM bundle on disk (regular file only, 4 MiB cap, at least one
  parseable certificate required, or the load fails). The module's `fallback`
  package is imported too, so `x509.SetFallbackRoots` supplies the same roots
  when the platform has none at all; a present but stale store is not
  overridden. The embedded roots keep Mozilla's distrust-after constraints
  (`AddCertWithConstraint`), so a constrained root stops validating
  certificates issued after that date. The selected source replaces the
  platform store instead of merging, so a machine whose CA package is stale or
  missing verifies modern sites like an updated one. Each source is labelled in
  `about:config`, where `system` and `embedded` can be switched for the
  session; a PEM path is command-line-only, because a page can navigate to
  `about:config` and must not be able to make the browser read a local bundle.
  A switch applies to later requests and swaps the transport after closing the
  old idle connections; a rejected value keeps the previous store with the
  reason shown on the page. The bundle is refreshed by bumping the module and
  re-vendoring, and is only as fresh as the pinned version. Revocation is not
  checked in any mode.
