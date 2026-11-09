# MOB³ is a compact, self-contained browser for the terminal

Written in Go, it combines its own HTML parser, CSS cascade and layout engine, ANSI renderer and pure-Go JavaScript runtime to display real web pages, including inline images, without a GUI toolkit or persistent profile. History, cookies, storage and caches live in memory only: closing MOB³ saves nothing.


<p align="center">
  <img width="400" alt="image" src="https://interactive32.com/images/mob3.png?ref=github.com/mob3net/mob3" />
</p>

## Warning
> **MOB³ is not a secure browser in any way.**
> It has no sandboxing, process isolation, site isolation, CSP enforcement,
> certificate UI or security hardening, and it has not been audited. Page
> JavaScript runs in a hobby-grade engine. Do not use MOB³ to log in to
> accounts, enter passwords, make payments, or handle confidential data. Use it
> at your own risk.

## Try it with Docker:

```
docker run --rm -it ghcr.io/mob3net/mob3
```

With no URL, MOB³ opens `about:home` (the start page). Use `-home` to point it
somewhere else.

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
- Text/search inputs and textareas, with GET and urlencoded POST form
  submission.
- Common CSS: selectors, cascade and inheritance, box model, margins, borders,
  backgrounds, text styling, lists, tables, viewport-width `@media` queries and
  basic flex rows.
- PNG/JPEG/GIF/SVG/data-URI images when enabled with `-graphics` (`auto`,
  `kitty`, `iterm`, or `blocks` for half-block art). Images are off by default
  for speed.
- Light JavaScript: DOM queries and mutation, `classList`/`style`, click
  handlers, timers, dialogs, same-origin `fetch`/`XMLHttpRequest`, and in-memory
  storage/cookies. Script errors show a short `JS: Error` footer hint; `!` opens
  the full console, with collapsible logs, requests, a `>` row for JavaScript
  expressions and a storage inspector.
- Browser-style toolbar with back/forward/reload/home, bookmarks/history/help
  buttons, a config gear and a centered page title.
- Keyboard and mouse navigation, a draggable page scrollbar, history (including
  a top-right History page
  listing the session's links), bookmarks imported with `-bookmarks` from a
  Netscape HTML export, scrolling, focus, link hover targets in the footer,
  text selection, and a `!` console with the current page's request log,
  cookies and in-memory storage.

**Rough edges**

- Modern JavaScript apps (SPAs, dashboards, Google search and similar) may show
  a partial page, a blank page or their no-JavaScript fallback. Pages that rely
  on ES modules show a status-bar warning that modules are unsupported.
- Anti-bot and firewall checks (Cloudflare, CAPTCHA, DataDome/PerimeterX,
  Imperva/Sucuri, access-denied pages) are generally not usable. A “Site
  blocked” page is shown instead.
- No video/audio playback, WebGL, WebAssembly, workers, WebSockets, iframes,
  Shadow DOM, custom elements, real observers or full History API.
- Basic CSS grid rows are supported; floats, absolute positioning, CSS
  variables, animations, transitions, generated content, `@import` and
  background images are unsupported or approximated as block flow.
- SVG support is partial: text inside SVG, `<use>/<symbol>`, filters, masks and
  CSS-in-SVG may be missing. WebP is not supported.
- Oversized images are capped at 60% of the viewport width and 24 rows,
  preserving their aspect ratio.
- No tabs, downloads or multipart/file uploads. Selected page text copies with
  `y` through OSC 52 (terminal support required), and the URL can be selected
  with the terminal.
- Very large pages, stylesheets and images are capped to keep memory and CPU
  bounded. Style and layout also run under a time budget, so a pathological
  page is shown partially with a truncation notice instead of freezing the
  browser. Pages whose timers update the DOM are redrawn a few times per
  second rather than continuously.

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
top: this is a hobby browser, not a security boundary.

Third-party module licenses are listed in
[THIRD_PARTY_NOTICES](https://github.com/mob3net/mob3/blob/main/THIRD_PARTY_NOTICES).
