# Command line

```text
mob3 [flags] [url|file]
```

With no URL, MOB³ opens the `-home` target (`about:home` by default). Input that
looks like a search query instead of a URL uses the `-search` fallback. A local
path becomes a `file:` URL.

Interactive mode starts only when stdin and stdout are TTYs. Otherwise MOB³
falls back to dump mode and prints the rendered page.

## Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `-dump` | off | Render the page to stdout and exit |
| `-png` | off | Write a PNG screenshot of the dump to stdout; implies `-dump` |
| `-width int` | terminal width or 100 | Layout width in columns |
| `-height int` | 0 | Layout viewport height and output limit; 0 uses the full page (dump/PNG/MCP) |
| `-graphics string` | `none` | Image mode: `none`, `auto`, `kitty`, `iterm`, `blocks` |
| `-g string` | `none` | Alias for `-graphics` |
| `-no-js` | off | Disable page script fetching and execution |
| `-reader` | off | Start in reader mode |
| `-dark` | off | Start in dark mode |
| `-ignore-blocks` | off | Load anti-bot/CAPTCHA pages instead of the blocked page |
| `-bookmarks string` | none | Import a Netscape bookmark HTML file |
| `-home string` | `about:home` | Startup and Home-button target |
| `-js-memory int` | 256 | JavaScript heap limit in MB; `0` disables the watchdog |
| `-cache int` | 32 | In-memory resource cache limit in MB; `0` disables it |
| `-layout-budget string` | `standard` | Layout limits: `low`, `standard`, `high`, `none` |
| `-text-scale string` | `auto` | Kitty scaled text: `auto` or `off` |
| `-color` | off | Force ANSI colors when dumping |
| `-no-color` | off | Disable ANSI colors in dump mode |
| `-ua string` | `MOB3/1.0 (+terminal; +https://mob3.net)` | Custom User-Agent |
| `-search string` | DuckDuckGo HTML endpoint | Search URL template with `%s`; empty disables the fallback |
| `-mcp` | off | Run the MCP automation server on stdin/stdout |
| `-version` | | Print the version and exit |
| `-h`, `-help` | | Print flags and exit |

Run `mob3 -h` for the exact defaults in your build.

## Images and graphics

`-graphics=none` is the default: no external image downloads and no inline SVG
rasterization. When enabled, native terminal graphics are preferred:

1. kitty graphics protocol
2. iTerm2 inline images
3. colored half-block art (`▀`/`▄`)

Auto-detection uses environment variables. Under tmux, screen or zellij it
falls back to half-block art because the multiplexer usually consumes graphics
escapes unless passthrough is configured. `-g` is an alias for `-graphics`.

Dump mode always uses half-block art for images that were loaded.

## Color and dump mode

`-dump` prints the whole document height (capped at 20,000 rows) without browser
chrome. Color is automatic on a TTY, forced by `-color`, and disabled by
`-no-color`.

```sh
mob3 -dump -width 120 page.html
mob3 -dump -width 120 -no-color https://go.dev
mob3 -dump -color https://example.com
```

### Page height

`-height N` sets the layout viewport to `N` rows and limits dump/PNG output to
`N` document rows. This affects `position:fixed` boxes and other
height-dependent layout. `-height 0` keeps the full document height. The
interactive UI always uses the real terminal height.

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

## JavaScript and cache limits

`-js-memory` sets the per-script/callback heap budget. Every script also runs
under a 2-second CPU watchdog. `0` disables the heap watchdog but not the CPU
timeout. `-no-js` disables both script fetching and execution; `<noscript>`
content is then shown.

`-cache` is a session-only LRU budget for CSS/JS response bodies and decoded
images. Nothing is written to disk, and cache hits appear in the `!` console.

## Home, search and bookmarks

`-home` sets the page opened at startup and by the `⌂` toolbar button. The
`-search` template receives the URL-escaped query in `%s`; the default uses the
server-rendered DuckDuckGo HTML endpoint. An empty value disables search
fallback, so only URL-like input navigates.

`-bookmarks FILE` imports a Netscape bookmark export (Chrome, Firefox, Safari,
Edge). The `★ Bookmarks` toolbar button appears only when a bookmark file was
loaded. Bookmarks live in memory for the session.

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
