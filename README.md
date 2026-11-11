# MOB³ is a compact, self-contained web browser for the terminal

Written in Go, it combines its own HTML parser, CSS cascade and layout engine, ANSI renderer and pure-Go JavaScript runtime to display real web pages, including inline images, without a GUI toolkit or persistent profile. History, cookies, storage and caches live in memory only: closing MOB³ saves nothing.


<p align="center">
  <img width="400" alt="image" src="https://interactive32.com/images/mob3.png?ref=github.com/mob3net/mob3" />
</p>

## Warning
> **MOB³ is not a secure web browser in any way.**
> It has no sandboxing, process isolation, site isolation, CSP enforcement,
> certificate UI or security hardening, and it has not been audited. Page
> JavaScript runs in a small, unaudited engine. Do not use MOB³ to log in to
> accounts, enter passwords, make payments, or handle confidential data. Use it
> at your own risk.

## Try it with Docker:

```
docker run --rm -it ghcr.io/mob3net/mob3 -graphics blocks
```

## Download and install

[Download the latest MOB³ release](https://github.com/mob3net/mob3/releases/latest)
from GitHub Releases.

Release archives cover Linux (`386`, `amd64`, `armv6`, `armv7`, `arm64`) and
macOS (`amd64`, `arm64`). Choose the archive for your OS and architecture, for
example `mob3_vX.Y.Z_linux_amd64.tar.gz`. Each archive contains the static
`mob3` binary.

Example:

```
tar -xzf mob3_vX.Y.Z_linux_amd64.tar.gz
sudo install -m 0755 mob3 /usr/local/bin/mob3
mob3 -version
```

## Using command line options

Start reading Wikipedia in dark mode:
```
mob3 -dark -reader 'https://en.wikipedia.org/wiki/Vim_(text_editor)'
```
See all available options with -h flag

## Development

Build from source and start browsing:

```
make
./bin/mob3 https://example.com
```

Requires Go 1.27 or newer. Interactive mode runs on Linux and macOS terminals;
Windows users can run it under WSL. When stdin/stdout are not a terminal, MOB³
automatically falls back to dump mode.

Press `?` inside the app for keys, mouse and forms help. Run `mob3 -h` for the
full list of command-line flags. `mob3 -version` prints the embedded version.

## What to expect

MOB³ is a content browser, not a full browser engine. It is happiest with
text-focused pages and gets rough on modern JavaScript-heavy apps.

**Works well**

- Documentation, blogs, news articles, wikis, forums, changelogs and old or
  server-rendered HTML.
- Headings, paragraphs, lists, links, tables, `<pre>` blocks, images and common
  presentational markup.
- Text/search inputs, textareas, single-select option popups and multi-select
  list boxes, with a visible caret, arrow-key editing, date/time/month/week,
  range sliders, color swatches, HTML constraint checks, GET and urlencoded POST
  form submission.
- Common CSS: selectors, cascade and inheritance, box model, margins, borders,
  backgrounds, text styling, lists, tables, viewport-width `@media` queries and
  basic flex rows.
- PNG/JPEG/GIF/SVG/data-URI images when enabled with `-graphics` (`auto`,
  `kitty`, `iterm`, or `blocks` for half-block art). Images are off by default
  for speed.
- Session-only memory cache for CSS, scripts and decoded images, with a 32 MiB
  default budget and `-cache` to change or disable it.
- Light JavaScript: DOM queries and mutation, `classList`/`style`, click
  handlers, timers, alert/confirm/prompt and native `<dialog>` open/close,
  same-origin `fetch`/`XMLHttpRequest` plus simple CORS cross-origin requests,
  and in-memory storage/cookies. Script
  errors turn the footer `JavaScript` label into a red `JavaScript:Error`;
  click it to open the console.
- JavaScript console (`!`): tabbed view of logs, requests, cookies and
  in-memory storage, plus the raw page HTML; evaluate one-line expressions in
  the page's runtime, with Up/Down history and results appended to the log.
- Reader mode (`R`, the footer `Reader:on`/`Reader:off` label, or `-reader` at
  launch): extracts the main article into a narrow, chrome-free text column,
  stays enabled across navigation, and restores the normal page and scroll
  position when toggled off.
- Dark mode (`D`, the footer `Dark:on`/`Dark:off` label, or `-dark` at launch):
  remaps page backgrounds, text, links and borders to a dark palette; images
  keep their original colors.
- Browser-style toolbar with back/forward/reload/home, history/help buttons,
  a bookmarks button when `-bookmarks` is supplied, a config gear and a centered
  page title.
- Keyboard and mouse navigation, `/` incremental find (including multi-word
  phrases) with highlighted matches
  (also in the console HTML tab), a draggable page scrollbar, history (including
  a top-right History page
  listing the session's links), bookmarks imported with `-bookmarks` from a
  Netscape HTML export, scrolling, focus, link hover targets in the footer,
  text selection, and a `!` JavaScript console.

**Rough edges**

- Modern JavaScript apps (SPAs, dashboards, Google search and similar) may show
  a partial page, a blank page or their no-JavaScript fallback. Pages that rely
  on ES modules show a status-bar warning that modules are unsupported and
  suggest reader mode (`R`).
- Reader mode is heuristic: it can pick too little or too much content on
  unusual layouts, shows a snapshot rather than live updates, and removes
  images, forms and other interactive controls. Dark mode keeps images in
  their original colors and approximates some author color schemes.
- Anti-bot and firewall checks (Cloudflare, CAPTCHA, DataDome/PerimeterX,
  Imperva/Sucuri, access-denied pages) are generally not usable. A “Site
  blocked” page is shown instead.
- No video/audio playback, WebGL, WebAssembly, workers, WebSockets, iframes,
  Shadow DOM, custom elements, real observers or full History API.
- Basic CSS grid rows are supported, along with `position:relative`,
  `position:absolute` and `position:fixed`; floats, `position:sticky`,
  `z-index`, CSS
  variables, animations, transitions, generated content, `@import` and
  background images are unsupported or approximated as block flow.
- SVG support is partial: text inside SVG, `<use>/<symbol>`, filters, masks and
  CSS-in-SVG may be missing. WebP is not supported.
- Oversized images are capped at 60% of the viewport width and 24 rows,
  preserving their aspect ratio.
- No tabs or automatic downloads; download links and non-HTML responses open a
  popup with a copyable `curl` command instead, and multipart/file uploads are
  not implemented (clicking a file input shows an unsupported notice). Selected
  page text copies with
  `y` through OSC 52 (terminal support required), and the URL can be selected
  with the terminal.
- Very large pages, stylesheets and images are capped to keep memory and CPU
  bounded. Style and layout also run under a time budget, so a pathological
  page is shown partially with a truncation notice instead of freezing the
  browser. Pages whose timers update the DOM are redrawn a few times per
  second rather than continuously, and an idle page with no pending timers does
  not repaint at all.

## Contributing

Contributions are welcome. Every pull request must include the broad
license-grant CLA statement described in
[CONTRIBUTING.md](https://github.com/mob3net/mob3/blob/main/CONTRIBUTING.md); it
keeps MOB³ available under both AGPL-3.0-only and a commercial license. The PR
workflow checks the statement. The CI workflow runs formatting, vet and tests.

## License

MOB³ is licensed under the GNU Affero General Public License, version 3 only
(AGPL-3.0-only). Copyright (c) 2026 Milos Stojanovic. See
[LICENSE](https://github.com/mob3net/mob3/blob/main/LICENSE).

A separate commercial license is available on request for use without the
AGPL's source-disclosure requirements.

MOB³ is provided “as is”, without warranty of any kind. See the warning at the
top: this is not a security boundary.

Third-party module licenses are listed in
[THIRD_PARTY_NOTICES](https://github.com/mob3net/mob3/blob/main/THIRD_PARTY_NOTICES).
