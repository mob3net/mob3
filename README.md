<p align="center">
  <img width="400" alt="image" src="https://github.com/user-attachments/assets/6b64c088-529b-4eba-838e-8f68f200a2fc" />
</p>


# MOB³ - Because browsers got too complicated

> **⚠ Warning: MOB³ is not a secure browser in any way.**
> It has no sandboxing, process isolation, site isolation, CSP enforcement,
> certificate UI or security hardening, and it has not been audited. Page
> JavaScript runs in a hobby-grade engine. Do not use MOB³ to log in to
> accounts, enter passwords, make payments, or handle confidential data. Use it
> at your own risk.

MOB³ is a small terminal web browser written in Go. It fetches pages over
HTTP(S), renders HTML and a practical CSS subset in 24-bit color, and shows
images inline with kitty or iTerm2 graphics, falling back to half-block art.

JavaScript runs through the pure-Go goja engine; there are no other third-party
modules. Requires Go 1.27 or newer. The project page is <https://mob3.net>.

MOB³ keeps history, cookies, storage and caches in memory only; nothing is
saved when it exits. The only file it writes is the optional `-cpuprofile`
output.

Try it with Docker:

```
docker run --rm -it ghcr.io/mob3net/mob3
```

## Run

```
make run
```

With no URL, MOB³ opens `about:start`, which shows the project icon, the
security warning, and links for getting started.

## Build

```
make
./bin/mob3 https://example.com
```

Or directly:

```
mkdir -p bin && CGO_ENABLED=0 go build -o bin/mob3 ./cmd/mob3
```

The result is a single static binary. Dependencies are vendored under
`vendor/`, so the build needs no network access. Interactive mode runs on Linux
and macOS terminals. Windows is not a build target; use WSL. When stdin/stdout
are not a terminal, MOB³ automatically falls back to dump mode.

## Versioning

Git tags such as `v1.2.3` are the source of truth for releases. `mob3 -version`
prints the embedded version. Local builds show `dev` unless a version is
provided:

```
make VERSION=v1.2.3
./bin/mob3 -version
```

The release workflow injects the tag into both the binaries and the Docker
image. For a local Docker build:

```
docker build --build-arg VERSION=v1.2.3 -t mob3 .
```

## Docker

Build the image:

```
make docker
# or
docker build -t mob3 .
```

Run it interactively:

```
docker run --rm -it mob3 https://example.com
```

Without `-it`, stdin is not a terminal and MOB³ uses dump mode:

```
docker run --rm mob3 -dump -width 100 https://example.com
```

The image is a non-root Alpine image with CA certificates. For kitty or iTerm2
inline images, pass the terminal graphics environment variables, for example
`-e TERM -e TERM_PROGRAM -e KITTY_WINDOW_ID`; otherwise images use half-block
art.

Each release also publishes `ghcr.io/mob3net/mob3` with the release tag and
`latest`, for `linux/386`, `linux/amd64`, `linux/arm64`, `linux/arm/v6` and
`linux/arm/v7`:

```
docker run --rm -it ghcr.io/mob3net/mob3:latest https://example.com
```

The first published package is private by default; make it public in the
repository's package settings if anonymous pulls should work.

## Releases

Pushing a `v*` tag (for example `v1.0.0`) publishes one archive per platform
plus a `SHA256SUMS` file to the GitHub release for that tag:

- Linux: 386, amd64, armv6, armv7 and arm64 (`.tar.gz`)
- macOS: amd64 and arm64 (`.tar.gz`)

Assets are named `mob3_<tag>_<os>_<platform>.tar.gz`, for example
`mob3_v1.0.0_linux_armv6.tar.gz`. Extract the archive and run `./mob3`. macOS
binaries are code-signed when release signing is configured; otherwise they are
published unsigned.

Pushing the tag is enough: if no GitHub release exists for it, the workflow
creates one with generated notes, then attaches the archives. You can still
create or edit the release in the web interface first.

Verify a download with the release checksums:

```
sha256sum -c SHA256SUMS --ignore-missing
```

## Usage

```
mob3 [flags] [url|file]

  -dump          render the page to stdout and exit
  -width int     layout width (default: terminal width or 100)
  -graphics str  auto, kitty, iterm or blocks (default "auto")
  -no-js         disable page JavaScript
  -ignore-blocks load anti-bot/CAPTCHA pages instead of the blocked page
  -js-memory int  JavaScript heap limit in MB, 0 disables (default 256)
  -text-scale    kitty scaled text: auto or off (default "auto")
  -cpuprofile    write a CPU profile to this file
  -color         force ANSI color when dumping
  -no-color      disable ANSI colors in dump mode
  -ua string     custom User-Agent
  -search str    search URL template with %s (default DuckDuckGo; empty disables)
  -version       print the version and exit
```

Examples:

```
./bin/mob3 https://go.dev
./bin/mob3 ./page.html
./bin/mob3 -dump -width 120 -no-color page.html
```

`-graphics=auto` detects kitty, WezTerm, Ghostty and iTerm2. Other terminals,
tmux/screen/zellij and `-dump` use half-block art.

The default User-Agent is `MOB3/1.0 (+terminal; +https://mob3.net)`. Override it with `-ua`.

When URL input is not URL-shaped — it contains whitespace or has no dot, so
`golang tutorial` or `golang` — MOB³ opens DuckDuckGo's server-rendered
search page instead of failing. Inputs such as `example.com`, `localhost:8080`,
`/path`, `about:start`, `file` paths and full URLs still navigate normally. Use
`-search` with a `%s` template to choose another engine, for example
`-search 'https://www.bing.com/search?q=%s'`, or `-search ''` to disable the
fallback.

## Keys and mouse

| Key | Action |
| --- | --- |
| `j` / `k` | scroll one line |
| `Space` / `b` | scroll one screen |
| `Ctrl-D` / `Ctrl-U` | scroll half a screen |
| `g` / `G` | top / bottom |
| `Tab` / `Shift-Tab` | next / previous link |
| `Enter` | follow link |
| `h` / `l` | back / forward |
| `r` | reload |
| `o` | open URL (current URL selected) |
| `u` | show current URL |
| `?` | open the help page |
| `!` | JavaScript console (mouse selection enabled) |
| `Esc` | stop loading, clear selection, or show the exit hint |
| `q` / `Ctrl-C` | quit |

| Mouse | Action |
| --- | --- |
| drag over text | select (highlight, auto-scroll) |
| click a text field | focus it; type to edit, Enter submits, Esc finishes |
| click link | follow |
| click URL field | open URL prompt |
| click `←` / `⟳` / `?` | back / reload / help |
| right click | back |
| wheel | scroll |

`Esc` stops an in-flight page load or clears a selection; in normal browsing,
or when leaving the URL prompt, an input field or the JS console, it also shows
`q Quit / ? Help` in the footer so the exit keys are easy to find. The top
bar shows back, reload, the page title, help and a full-width URL field. `?` or the
toolbar help button opens the built-in `about:help` page, which documents keys,
mouse, forms and every command-line flag. Opening the URL field
prefills and selects the current URL; typing replaces it. Inside the field,
Left/Right, Home/End, Backspace/Delete, Ctrl-A, Ctrl-K, Ctrl-U and Ctrl-W edit
the URL; the block caret overlays text without shifting it.

## What works

- Forgiving HTML parsing: headings, paragraphs, lists, links, tables, `<pre>`,
  entities and common presentational attributes.
- Editable text/search inputs and textareas with prominent field styling;
  focusing a field highlights it and clears its placeholder hint. Enter submits
  GET and urlencoded POST forms. Multipart/form-data, selects and file uploads
  are not supported.
- CSS: selectors, cascade and inheritance, box model, margins (including
  `margin:0 auto` centering), borders, backgrounds, text styling, lists,
  tables, viewport-width `@media` queries and basic flex rows.
- PNG/JPEG/GIF/SVG/data-URI images with aspect ratio and transparency; native
  kitty/iTerm2 graphics with half-block fallback. Inline SVG is rasterized too.
- Kitty text sizing for genuinely larger heading glyphs; elsewhere font size
  affects line height and emphasis.
- JavaScript through goja: inline and external scripts, common DOM queries and
  mutation, `classList`/`style`, element constructor prototypes
  (`HTMLElement`, `HTMLScriptElement` and friends), `navigator`/`window` basics
  including `location`, `performance`, `Image`, `requestAnimationFrame`, click
  handlers, `DOMContentLoaded`/`load`, setTimeout/setInterval, console output
  and modal `alert`/`confirm`/`prompt` dialogs. Event constructors
  (`Event`, `CustomEvent`, `MouseEvent`, `KeyboardEvent` and friends),
  `document.createEvent` and `dispatchEvent` with bubbling are available.
  `document.defaultView`, `ownerDocument` and
  `document.implementation` (`hasFeature`/`createHTMLDocument`) back common
  library feature detection, and `getComputedStyle` exposes resolved
  display/font/box values with `getPropertyValue`. Includes
  `URL`/`URLSearchParams`,
  `document.currentScript`/`write`, in-memory storage and a session cookie jar
  shared by `document.cookie`, HTTP requests, XHR and fetch,
  `XMLHttpRequest`, promise-based `fetch` with `Headers`/`Request`/`Response`,
  and an English-only `Intl` subset (`NumberFormat`, `DateTimeFormat`,
  `Collator`, `PluralRules`, `RelativeTimeFormat`, `ListFormat`).
  `-no-js` disables it; `!` opens the copyable JS console. Each script and
  callback has a 2 s CPU budget and a 256 MB heap-delta budget; raise or
  remove the latter with `-js-memory=MB` (0 disables it).
- Canvas is a compatibility stub: `getContext("2d")` returns a no-op context so
  feature detection and color parsing polyfills do not throw, but nothing is
  drawn and WebGL is unavailable.
- Cookie jar, redirects, gzip and charset decoding; pages up to 12 MB.
- Keyboard and mouse navigation, history, scrolling, focus and text selection.

## Renderer

MOB³ is a cell-based renderer, not a pixel browser:

- HTML is parsed, styled and laid out into a display list. One terminal cell
  represents 8 CSS pixels horizontally and 16 vertically.
- Layout supports block and inline flow, line wrapping, tables, lists,
  viewport-aware `@media` queries and basic flex rows. The page is painted into
  a grid of terminal cells with 24-bit foreground/background colors, then only
  changed lines are written back with ANSI escapes.
- Text uses the terminal's font. Headings are expressed with weight, color and
  line height; in real kitty, large text is rendered with the text-sizing
  protocol (`OSC 66`) at integer multiples of the terminal font size.
- Images are decoded as PNG/JPEG/GIF and drawn with kitty graphics first, then
  iTerm2 inline images, falling back to colored `▀`/`▄` half-block art.
  Graphics images are downscaled to roughly the terminal cell size and
  re-placed on each frame; `-dump` always uses half-block art.
- Inline link/button backgrounds are painted behind their text, and text with
  near-invisible contrast is automatically switched to dark-on-light or
  light-on-dark.
- The top bar, footer, focus highlights and mouse selection are painted into
  the same cell grid. The footer shows MOB³'s resident memory use, with the
  scroll percentage pinned to the far right. Scrolling shifts document
  coordinates rather than moving terminal content. Autowrap is disabled while
  running, the chrome is repainted every frame, and scrolling forces a full
  text repaint so a stray terminal wrap cannot leave the top bar stale.
- JavaScript runs before layout, so DOM changes appear in the rendered page.
- SVG and WebP are not decoded, and there are no font files: real glyph shapes
  and arbitrary font sizes would require a pixel-based text renderer.

## Limits

- JavaScript is a practical subset: modules and CSP are unsupported; JS storage
  and cookies are in-memory only and never persisted. Cookies are session-only,
  `SameSite` is not enforced, and there is no `CookieStore` API.
  `XMLHttpRequest` and `fetch` are same-origin-only (cross-origin
  requests/redirects are blocked), synchronous under the hood, and lack
  streaming/upload progress; `fetch` has no abort controller. `Intl` is an
  English-only subset without full CLDR locale data. JS history navigation is
  stubbed. `location` redirects and meta refresh work. JS-heavy apps that
  require missing APIs (for example Google search) may still show their
  no-JavaScript fallback.
- Nothing is persisted: history, cookies, JS storage and the image cache are
  in-memory only and disappear on exit. The optional `-cpuprofile` file is the
  only disk write; there are no config, history, cookie, cache or log files.
- Remote pages cannot load `file:` resources, navigate to `file:` URLs, or XHR
  local files. Local `file:` pages can still load local resources.
- Flexbox supports basic rows only; grid, floats and absolute positioning are
  approximated as block flow.
- SVG rendering is partial: text inside SVG, `<use>/<symbol>`, filters, masks
  and CSS-in-SVG may be missing. Inline SVG falls back to its accessible label
  if rasterization fails. WebP is not decoded; broken images show their `alt`.
- No font files; outside kitty, text always uses one glyph size.
- Mouse selection is highlight-only; there is no clipboard copy yet.
- Very large stylesheets are capped (4 MiB, 50k rules, 1 second of parsing),
  images above 25 megapixels are skipped, local files must be regular files and
  are read with a 12 MB cap, JS runs under CPU/memory watchdogs, and layout
  stops after 250k fragments/200k rows.
- Browser checks and anti-bot/firewall blocks cannot be completed. Common
  roadblocks (Cloudflare, CAPTCHA, DataDome/PerimeterX, Imperva/Sucuri,
  access-denied pages) show a generic “Site blocked” page with the detected
  reason. The page's **Try anyway** link (or `-ignore-blocks`) re-fetches it
  without block detection and runs its scripts, but the check will usually
  still fail; MOB³ is not a secure browser.
- No tabs or downloads. Forms support text entry and urlencoded GET/POST;
  multipart forms, selects, file uploads and checkbox/radio toggling are not
  supported. No certificate or proxy UI.
- Interactive mode requires a Linux or macOS terminal. Windows is not supported
  directly; use WSL.

## Tests

```
go test ./...
```

The project uses `cmd/mob3` for the executable and `internal/browser` for the
engine; tests live next to the code.

## Contributing

Contributions are welcome. Every pull request must include the broad
license-grant CLA statement described in [CONTRIBUTING.md](https://github.com/mob3net/mob3/blob/main/CONTRIBUTING.md);
it keeps MOB³ available under both AGPL-3.0-only and a commercial license.
The PR workflow checks the statement. The CI workflow runs formatting, vet and
tests on pull requests and pushes to `main`.

## License

MOB³ is licensed under the GNU Affero General Public License, version 3 only
(AGPL-3.0-only). Copyright (c) 2026 Milos Stojanovic. See [LICENSE](https://github.com/mob3net/mob3/blob/main/LICENSE).

A separate commercial license is available on request for use without the
AGPL's source-disclosure requirements. Ask the copyright holder if you need
one.

MOB³ is provided “as is”, without warranty of any kind. See the warning at the
top: this is a hobby browser, not a security boundary.

Third-party module licenses are listed in
[THIRD_PARTY_NOTICES](https://github.com/mob3net/mob3/blob/main/THIRD_PARTY_NOTICES).
