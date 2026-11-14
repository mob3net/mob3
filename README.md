<p align="center">
  <img width="400" alt="image" src="https://interactive32.com/images/mob3-screenshot-1.png?ref=github.com/mob3net/mob3" />
</p>

# MOB³ is a small indie web browser for the terminal

Written in Go, MOB³ features its own HTML rendering engine with JavaScript support, an ANSI renderer, and inline image support. No GUI toolkit or persistent browser profile required. Built as a true terminal web browser for humans and AI agents.

Project page: [mob3.net](https://mob3.net)

GitHub: [github.com/mob3net/mob3](https://github.com/mob3net/mob3)

## Documentation

- [Documentation index](docs/index.md)
- [Use cases](docs/use-cases.md) — developers, AI agents, terminal users and
  automation
- [Install](docs/install.md) — releases, Docker and source builds
- [Command line](docs/cli.md) — every flag, default and limit
- [Keyboard and mouse](docs/keys.md)
- [Automation with MCP](docs/automation.md) — headless control for scripts and AI tools
- [Security and privacy](docs/security.md), [site support](docs/sites.md) and
  [development](docs/development.md)

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

## Screenshots
<div align="center">
<a href="https://github.com/user-attachments/assets/8588bbd5-0b97-47cd-95e3-83c443cf761d" target="_blank" rel="noopener"><img width="32%" src="https://github.com/user-attachments/assets/8588bbd5-0b97-47cd-95e3-83c443cf761d" /></a>
<a href="https://github.com/user-attachments/assets/0dcd6d0b-f0ff-405f-bc1f-5e4c07e2cf10" target="_blank" rel="noopener"><img width="32%" src="https://github.com/user-attachments/assets/0dcd6d0b-f0ff-405f-bc1f-5e4c07e2cf10" /></a>
<a href="https://github.com/user-attachments/assets/376014a5-96ac-4b4e-bb80-6c9f82a8454d" target="_blank" rel="noopener"><img width="32%" src="https://github.com/user-attachments/assets/376014a5-96ac-4b4e-bb80-6c9f82a8454d" /></a>
<br />
<a href="https://github.com/user-attachments/assets/e570359d-0665-4467-add4-b380555f3dba" target="_blank" rel="noopener"><img width="32%" src="https://github.com/user-attachments/assets/e570359d-0665-4467-add4-b380555f3dba" /></a>
<a href="https://github.com/user-attachments/assets/21dfff33-2280-4f4b-938c-4bc233da0667" target="_blank" rel="noopener"><img width="32%" src="https://github.com/user-attachments/assets/21dfff33-2280-4f4b-938c-4bc233da0667" /></a>
<a href="https://github.com/user-attachments/assets/6453a4b6-6628-4a3e-bb55-02ff6d724bc2" target="_blank" rel="noopener"><img width="32%" src="https://github.com/user-attachments/assets/6453a4b6-6628-4a3e-bb55-02ff6d724bc2" /></a>
<br />
<a href="https://github.com/user-attachments/assets/a0486574-b646-4981-be59-a60eff284140" target="_blank" rel="noopener"><img width="32%" src="https://github.com/user-attachments/assets/a0486574-b646-4981-be59-a60eff284140" /></a>
<a href="https://github.com/user-attachments/assets/7f0becc1-0658-4cac-a893-7d864c0b4be3" target="_blank" rel="noopener"><img width="32%" src="https://github.com/user-attachments/assets/7f0becc1-0658-4cac-a893-7d864c0b4be3" /></a>
<a href="https://github.com/user-attachments/assets/50d20cc6-cb34-4733-8168-9b12c838056e" target="_blank" rel="noopener"><img width="32%" src="https://github.com/user-attachments/assets/50d20cc6-cb34-4733-8168-9b12c838056e" /></a>
</div>

## Show your support
If MOB³ has been useful to you, please consider
[giving the project a star on GitHub](https://github.com/mob3net/mob3). It helps
others discover it and supports the continued development of the project.

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
  form submission. The start page has justified text and a full-width
  search/URL box that resolves input the same way as the `o` prompt, including
  the `-search` fallback.
- Common CSS: selectors, cascade and inheritance, box model (including
  `box-sizing` and min/max sizes), margins, borders, backgrounds, text styling,
  lists, tables, viewport-width `@media` queries and basic flex rows.
- PNG/JPEG/GIF/WebP/BMP/SVG/data-URI images when enabled with `-graphics` (`auto`,
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
  in-memory storage, the raw page HTML, and a Tools tab for read-only page
  inspection (`help`, `form`, `links`, `requests`, `logs`, `source`,
  `snapshot`, `history`, `clear`); evaluate one-line expressions in the page's
  runtime, with Up/Down or Ctrl-P/Ctrl-N history and results appended to the
  log. `clear`/`cls` empties either the JS log or the Tools scrollback.
- Reader mode (`R`, the footer `Reader:on`/`Reader:off` label, or `-reader` at
  launch): extracts the main article into a narrow, chrome-free text column,
  stays enabled across navigation, and restores the normal page and scroll
  position when toggled off.
- Dark mode (`D`, the footer `Dark:on`/`Dark:off` label, or `-dark` at launch):
  asks sites for their dark styles with `prefers-color-scheme: dark`. Pages that
  already render a dark background keep their own palette; otherwise
  backgrounds, text, links and borders are remapped to a dark palette. Images
  keep their original colors.
- Headless MCP automation (`-mcp`): navigate, snapshot page text and links,
  click links or elements by text, list and fill forms, select options, submit,
  wait for async content, type, press keys, scroll, evaluate JavaScript, inspect
  history and read request logs, JS errors or page source. Screen captures are
  terminal-cell rasters; sessions are memory-only.
- Browser-style toolbar with back/forward/reload/home, history/help buttons,
  a bookmarks button when `-bookmarks` is supplied, a config gear and a centered
  page title.
- Keyboard and mouse navigation, including the kitty keyboard protocol in
  kitty, WezTerm, Ghostty and foot, `/` incremental find (including multi-word
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
  CSS-in-SVG may be missing. Animated WebP is not supported.
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

## Using command line options

Start reading Wikipedia in dark mode:
```
mob3 -dark -reader 'https://en.wikipedia.org/wiki/Vim_(text_editor)'
```
See all available options with -h flag. Every flag and default is listed in the
[command-line reference](docs/cli.md).

Capture a PNG screenshot:

```sh
mob3 -png -width 120 bbc.com/news > bbc.png
```

## Automation (MCP)

`mob3 -mcp` runs a headless browser as a Model Context Protocol server over
stdin/stdout. It accepts an optional starting URL and fixed `-width`:

```json
{
  "mcpServers": {
    "mob3": {
      "command": "/path/to/mob3",
      "args": ["-mcp", "-width", "100", "about:home"]
    }
  }
}
```

Tools: `navigate`, `snapshot`, `screenshot`, `click` (link number, CSS
selector or `x`/`y`), `type`, `press`, `scroll`, `back`, `forward`, `reload`,
`evaluate` and `history`. `snapshot` returns the current page viewport as text, visible links
as `[n]` and form controls with coordinates. Results are text only: there is no
pixel screenshot because MOB3 renders to terminal cells.

The server is local and unauthenticated, sessions and storage are memory-only,
and nothing is written to disk. Do not expose it as a network service. See the
[full MCP reference](docs/automation.md) for the protocol, snapshot format and
tool details.

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
