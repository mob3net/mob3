# Command line

```text
mob3 [flags] [url]
```

With no URL, MOB³ opens the `-home` target (`about:home` by default). Input that
looks like a search query instead of a URL uses the `-search` fallback. Local
file pages are not supported: `file:` URLs are rejected and a bare path is
treated as a URL or search query, never read from disk.

Interactive mode starts only when stdin and stdout are TTYs. Otherwise MOB³
falls back to dump mode and prints the rendered page.

## Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `-dump` | off | Render the page to stdout and exit |
| `-png` | off | Write a PNG screenshot of the dump to stdout; implies `-dump`. PDFs render one page (page 1 unless `-pdf-page` chooses another) |
| `-width int` | terminal width or 100 | Layout width in columns |
| `-height int` | 0 | Layout viewport height and output limit; 0 uses the full page for dump/PNG and 30 rows for MCP |
| `-pdf-page string` | `auto` | PDF page for dump/PNG: `auto`, `all`, or a page number. `-png` defaults to page 1; a dump defaults to all pages |
| `-graphics string` | `none` | Image mode: `none`, `auto`, `kitty`, `iterm`, `blocks` |
| `-g string` | `none` | Alias for `-graphics` |
| `-no-js` | off | Disable page script fetching and execution |
| `-no-pdf` | off | Disable the built-in PDF text viewer and show the download prompt instead |
| `-reader` | off | Start in reader mode |
| `-dark` | off | Start in dark mode |
| `-ignore-blocks` | off | Load anti-bot/CAPTCHA pages instead of the blocked page |
| `-home string` | `about:home` | Startup and Home-button target |
| `-mem-limit int` | 0 | Soft memory limit per process in MB; in `inproc` one process is shared by all tabs, while `-engine=process` gives each tab child its own limit; derives the JS (25%), image (12%), SVG (3%), resource-cache (3%) and history-cache (12%) budgets; `0` leaves it unlimited and honors `GOMEMLIMIT` |
| `-layout-budget string` | `standard` | Layout limits: `low`, `standard`, `high`, `none` |
| `-zoom string` | `100%` | Layout zoom percent from 25 to 200; `%` is optional and `+`/`-` step the presets |
| `-text-scale string` | `auto` | Kitty scaled text: `auto` or `off` |
| `-color` | off | Force ANSI colors when dumping |
| `-no-color` | off | Disable ANSI colors in dump mode |
| `-ua string` | `MOB3/1.0 (+terminal; +https://mob3.net)` | Custom User-Agent |
| `-search string` | DuckDuckGo HTML endpoint | Search URL template with `%s`; empty disables the fallback |
| `-certs string` | `system` | TLS trust store: `system` or `embedded` |
| `-engine string` | `inproc` | Interactive engine placement: `inproc` or `process` (one child process per tab) |
| `-mcp` | off | Run the MCP automation server on stdin/stdout |
| `-version` | | Print the version and exit |
| `-h`, `-help` | | Print flags and exit |

Run `mob3 -h` for the exact defaults in your build.

## Engine placement

`-engine=process` runs one hidden engine child per interactive tab using the same static binary. The parent keeps the terminal, network policy, cookies, cache, settings and history; the child owns the page DOM, JavaScript, layout and PDF state. A wedged tab can be killed without stopping the browser, but this is fault isolation, not a security sandbox.

Child tabs share `localStorage`/`sessionStorage` with the parent through the storage broker. `-mem-limit` applies per process: in `inproc` mode all tabs share one process and one limit, while process mode gives each tab child its own limit and the parent its own. Dump, PNG and MCP modes remain in-process. The default is `-engine=inproc`.

## Images and graphics

`-graphics=none` is the default: no external image downloads and no inline SVG
rasterization. When enabled, native terminal graphics are preferred:

1. kitty graphics protocol
2. iTerm2 inline images
3. colored half-block art (`▀`/`▄`)

Auto-detection uses environment variables. Under tmux, screen or zellij it
falls back to half-block art because the multiplexer usually consumes graphics
escapes unless passthrough is configured. `-g` is an alias for `-graphics`.

Dump mode always uses half-block art for images that were loaded. An image
with page text painted over it, such as a caption or a tab strip on a hero
photo, is also drawn as half-block art: native placements composite above the
text grid and would hide that text.

Images stream in after the page text and appear progressively. A batch that
only affects images below the current viewport does not redraw the visible
frame, so the page stays steady while the rest loads; content below is laid out
when you scroll to it.

## Color and dump mode

`-dump` prints the whole document height (capped at 20,000 rows) without browser
chrome. Color is automatic on a TTY, forced by `-color`, and disabled by
`-no-color`. Output uses 24-bit color, except on macOS Terminal.app where it
falls back to the 256-color palette because that terminal does not support
24-bit SGR sequences. With `-no-color`, interactive output keeps bold and
underline attributes (links stay underlined and link hints use reverse video);
dumps stay plain text.

```sh
mob3 -dump -width 120 page.html
mob3 -dump -width 120 -no-color https://go.dev
mob3 -dump -color https://example.com
```

### Page height

`-height N` sets the layout viewport to `N` rows and limits dump/PNG output to
`N` document rows. This affects `position:fixed` boxes and other
height-dependent layout. `-height 0` keeps the full document height in dump and
PNG output, while `-mcp` uses 30 rows when no height is given. The interactive UI
always uses the real terminal height.

```sh
mob3 -dump -height 40 -width 120 page.html
mob3 -png -height 40 -width 120 page.html > page.png
mob3 -mcp -width 100 -height 60
```

### PNG screenshots

`-png` renders the dump as a PNG stream on stdout. It implies `-dump`, is
capped at 2,000 rows, and uses an 8×16 pixel cell with Go Mono glyphs:

```sh
mob3 -png -width 120 bbc.com/news > bbc.png
mob3 -png -g blocks -width 120 bbc.com/news > bbc-images.png
```

Image cells render as colored half-block art. The renderer is text-oriented:
CJK and emoji glyphs are missing from the bundled Go Mono font, and there is no
browser chrome or true pixel screenshot.

## Layout budget

`-layout-budget` bundles the style/layout deadline and fragment/document-row
caps:

| Preset | Deadline | Fragments | Rows |
| --- | --- | --- | --- |
| `low` | 1 s | 100,000 | 80,000 |
| `standard` | 2 s | 250,000 | 200,000 |
| `high` | 5 s | 1,000,000 | 1,000,000 |
| `none` | disabled | disabled | disabled |

`none` can freeze the UI on hostile or generated pages. The preset is editable
in `about:config`; a zero individual limit disables that check. When a limit is
hit, the page renders partially and the status says
`Page truncated (layout limit)`.

## Page zoom

`-zoom 150` starts at 150% (values 25 to 200, `%` optional). In the interactive
browser `+` and `-` step through 25, 50, 67, 80, 100, 125, 150, 175 and 200%,
and `0` returns to 100%. Zoom changes how many CSS pixels one terminal cell
represents, not the text size: body text keeps one cell per character and its
normal line height, while fixed pixel widths, spacing, borders, images and
kitty scaled large headings grow or shrink. The CSS viewport follows the ratio,
so responsive pages reflow. It applies to dump, PNG and MCP runs, is editable in
`about:config`, and is never persisted. In the interactive browser the URL row
always shows the current level as a clickable badge at its far right; clicking
it opens a window for choosing a preset with the mouse or arrow keys and
`Enter`.

## TLS certificate store

`-certs` chooses where HTTPS trust comes from:

| Value | Trust store |
| --- | --- |
| `system` (default) | The operating system's root certificates, read by Go when a TLS connection needs them; if the system has no roots at all, the embedded bundle is used automatically |
| `embedded` | The Mozilla NSS root set, compiled into the binary from `golang.org/x/crypto/x509roots/fallback` |

The embedded bundle is the official one maintained by the Go project: it is
parsed from Mozilla's NSS trust store and keeps Mozilla's distrust-after dates,
so a constrained root stops validating certificates issued after that date. It
is refreshed by bumping the module and re-vendoring, not by editing a data file:

```sh
go get golang.org/x/crypto/x509roots/fallback@latest
go mod vendor
```

The chosen store replaces the operating system store instead of adding to it,
so the same roots are trusted on every machine. This matters on an operating
system the vendor no longer updates: its CA package may be missing or years out
of date, and `-certs=embedded` restores modern HTTPS without touching the rest
of the system. The automatic fallback covers only a system with *no* roots; an
old but present CA package is not overridden, which is exactly when the flag is
needed.

The value can be switched between `system` and `embedded` for the session in
`about:config`. It applies to every later request, and pooled connections made
with the previous store are closed. Certificate files are not supported: the
binary reads no local bundle. An unknown value at startup is an error and the
run stops; a rejected value in `about:config` keeps the current store and shows
the reason on the page.

Trust-store choice is not a security boundary: revocation is not checked, and
there is no certificate UI, pinning or warning flow beyond the connection error
the request returns.

## JavaScript and cache limits

Each page has an absolute heap ceiling above the heap at runtime creation. One
sampler per active runtime checks it every 75 ms and interrupts the running call;
a page that retains allocations cannot reset the budget on each callback. The
ceiling is 25% of `-mem-limit`/`GOMEMLIMIT` (for example 256 MB at a 1 GB limit),
or 256 MB when the process is unlimited; raise `-mem-limit` to raise it. Every
script also runs under a 2-second CPU watchdog. There is no separate per-page
flag and no way to disable the memory check while keeping JavaScript on. Each
page's load-time scripts share an 8-second wall-clock budget and run at most
2,000 scripts; on either limit the running script is interrupted and the
remaining ones are skipped with a console notice. Esc or Ctrl-C interrupts the
script currently blocking the terminal and cancels its in-flight requests;
closing the tab does the same. Decoded image bytes in flight and
inline-SVG rasters are bounded by percentages of the same memory limit and by
absolute fallbacks. `-no-js` disables both script fetching and execution;
`<noscript>` content is then shown. Press `J` (or click the footer JavaScript
label) to toggle this for the session and reload the current page.

The resource cache and the back/forward history cache follow the same limit:
3% and 12% of `-mem-limit`/`GOMEMLIMIT`, with 32 MB and 128 MB fallbacks when
the process is unlimited. The resource cache is a session-only LRU for CSS/JS
response bodies and decoded images; nothing is written to disk, and cache hits
appear in the `!` console. The history cache weighs suspended pages and evicts
the least recently used ones.

## Home and search

`-home` sets the page opened at startup and by the `⌂` toolbar button. The
`-search` template receives the URL-escaped query in `%s`; the default uses the
server-rendered DuckDuckGo HTML endpoint. An empty value disables search
fallback, so only URL-like input navigates.

## Automation

`-mcp` starts the headless MCP server instead of the terminal UI:

```sh
mob3 -mcp -width 100 about:home
```

See [Automation with MCP](automation.md) for the client configuration, protocol
and tools.

## `about:config`

The `⚙` toolbar icon opens `about:config`, a form listing every CLI option and
its current value. Editable options change for the session only; startup
options are read-only; dump-only colour options do not affect the interactive
UI. The top shows the current viewport in characters and pixels (using the
terminal's reported cell size, or 8×16 by default). Nothing is persisted.

The form posts its values with a one-shot session token. A link, script or
typed `about:config?...` query without that token applies nothing, so page
content cannot rewrite session settings; use the form's **Apply changes**
button.
