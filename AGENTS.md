# AGENTS.md — Project decisions and working agreement

This file is the durable record of the requirements, decisions and instructions
for this repository. **Every important new instruction from the user must be
folded into the section it governs**, rewritten as the rule in force, before or
alongside the code change it causes. There is no history section: past entries
are merged into the rules they produced, and superseded ones are dropped.

Project: **MOB³**, a small web browser for the terminal,
written in Go

---

## 1. Working agreement

- **Ask before deciding.** Anything that needs attention, approval, or is a
  software design decision must be asked, not assumed. Preferred method: ask
  **one question at a time**, wait for the answer. Do not ask many decisions at
  once.
- **If an unanswered question matters, ask again.** The user tends to forget,
  and an answer is not implied by silence.
- **Update this file with every new decision or change in plan.** Fold the
  decision into the section it governs.
- **Tests as you go.** Add or update tests in the same change as every behavior
  change or bug fix; do not defer tests to a later pass. Tests must stay
  network-free and should cover each affected layer (parser, style, layout,
  paint, input) where practical.
- **Docs stay current.** Update `README.md` (and any other existing
  documentation) in the same change whenever user-visible behavior, flags,
  keys, build output, supported features or limitations change. The `docs/`
  tree is deferred for now, but existing docs must never go stale.
- **Contributions.** Every external contribution requires the broad
  license-grant CLA in `CONTRIBUTING.md` and the PR statement it describes.
  Never merge contributed code without that agreement. Contributors keep their
  copyright but grant the Maintainer the right to relicense under AGPL-3.0-only
  or commercial terms. The `PR` workflow checks the statement; the `CI`
  workflow runs formatting, vet and tests on pull requests and pushes to
  `main`.
- **Commit messages.** End every completed change with a short, one-line commit
  message summarizing the work. Do not commit unless explicitly asked; just
  provide the message.
- **Documentation split.** Keep `README.md` short and user-facing: the basics,
  how to run (including Docker) and what sites/features to expect. Detailed
  flags live in `mob3 -h`, and keys/mouse/forms live in the in-app `about:help`
  page; do not duplicate those lists in the README. Keep this file
  decision-facing;

## 2. Non-negotiable constraints

- **Go 1.27.** `go.mod` declares `go 1.27`; the target toolchain is Go 1.27.x
  and the code may use standard-library and language features available through
  Go 1.27.
- **Single static binary.** Build with `CGO_ENABLED=0`; `make` produces a
  statically linked binary. No cgo and no external system libraries.
- **License.** The project is licensed under AGPL-3.0-only; `LICENSE` carries
  the full GNU Affero General Public License v3 text, and the README records
  the copyright (currently `2026 Milos Stojanovic`). A separate commercial
  license is available on request; do not add commercial terms or a contact
  unless the copyright holder supplies them. Contributions use the broad
  license-grant CLA in `CONTRIBUTING.md` so both licensing options remain
  available. Keep the license file and the README License section in sync.
- **Not a secure browser.** MOB³ is a hobby engine with no sandboxing, process
  or site isolation, CSP enforcement, certificate UI or security hardening, and
  it is not audited. This warning must stay prominent in `README.md` and on the
  built-in start and help pages. Never present it as safe for passwords,
  accounts, payments or confidential data.
- **No persistence.** The running browser must not create or update
  configuration, history, cookie, cache, log or download files; closing MOB³
  saves nothing. History, cookies, `localStorage`/`sessionStorage`, the image
  cache and all JS state live in memory only. The only file write is the path
  given to `-cpuprofile`. Disk reads are limited to explicit local `file:` pages
  and their local resources, an explicitly passed `-bookmarks` file, plus
  `/proc/self/statm` for the Linux memory footer; OS-level reads performed by
  Go's TLS, DNS and timezone libraries are not application storage. Do not add
  automatic disk caching or restore-on-start behavior.
- **Dependency policy.** Standard library plus the approved
  `github.com/dop251/goja` JavaScript engine, the pure-Go SVG renderer
  `github.com/srwiley/oksvg` + `github.com/srwiley/rasterx` (user-requested SVG
  support), and their pure-Go transitive modules (`regexp2`, `sourcemap`,
  `pprof`, `golang.org/x/image`, `golang.org/x/net`, `golang.org/x/text`). No
  other third-party modules without a new explicit user decision. goja is pure Go
  and must not change the single-binary or no-cgo constraints. All dependencies
  are **vendored in `vendor/` and committed**; builds use `-mod=vendor` and need
  no network. Run `go mod vendor` after any `go.mod`/`go.sum` change and keep
  `vendor/modules.txt` in sync.
- **SVG is not in core Go.** The standard library and `golang.org/x/image`
  provide no SVG parser or renderer, and there is no known plan to add one.
  MOB³ therefore uses the vendored, pure-Go `oksvg` + `rasterx` renderer for
  SVG (partial SVG2); inline SVG decodes through the same path, with a
  label/`alt` fallback when rasterization fails.
- **JavaScript via goja.** Page scripts run through goja in a per-page runtime.
  Classic inline and external scripts execute in document order; `async`,
  `defer` and modules are ignored. `-no-js` disables both script fetching and
  execution. `<noscript>` is hidden when JS is enabled and shown with
  `-no-js`. Text inputs and textareas are editable; GET and urlencoded POST
  forms submit with Enter. File uploads, multipart forms and most other
  controls remain unsupported.
- **Terminal-only presentation.** There is no GUI toolkit and no native
  window. All output is ANSI/Unicode in the terminal, and a terminal cell is
  the layout unit.
- **Images.** Images are disabled by default for speed (`-graphics=none`):
  external image downloads and inline SVG rasterization are skipped, leaving
  `alt` text or labels. When enabled, native terminal graphics come first —
  kitty graphics protocol, then iTerm2 inline images — with colored half-block
  art (`▀`/`▄`) as the fallback. No sixel.
- Keep it bare-bones but functional: real fetching, parsing, layout, rendering,
  navigation and scrolling.

## 3. Architecture

Standard Go layout:

- `cmd/mob3/main.go` — thin entry point; calls `browser.Run(os.Args[1:])` and
  exits with its status.
- `internal/browser/` — package `browser` with the engine, CLI loop and tests.
  No `internal` package is imported from outside this module. Keep the folder
  consolidated by subsystem rather than one file per small feature; platform
  code stays in its `term_*`/`signals_*` files.

| File | Decision / responsibility |
| --- | --- |
| `cmd/mob3/main.go` | Executable entry point only. |
| `html.go` | Hand-written tolerant HTML tokenizer + tree builder. No `x/net/html`. Handles comments, doctype, raw text elements, entities, void elements, implicit closes scoped to the current container so nested tables/lists survive. Honors XML-style self-closing tags (needed by legacy markup such as `<tr/>`). |
| `style.go` | Hand-written CSS rule/selector/declaration parser, computed styles, cascade and inheritance, unit parsing, named/hex/rgb/hsl colors, UA stylesheet written in the same CSS subset, presentation attributes. Parser supports type/universal/class/id/attribute/descendant/child/sibling selectors, `:not`, structural pseudos, grouped selectors, and `@media` rules carrying viewport conditions evaluated during styling; `@supports` contents are included. |
| `layout.go` | Block/inline flow, line wrapping, tables (collapsed 1-cell grid), lists, images, link rectangles, display list (`Frag`s) in document coordinates, plus text width/truncation helpers and wide-character measurement. |
| `render.go` | Terminal cell framebuffer, clipping, image downsampling, half-block rendering, image-placement collection, ANSI truecolor output, changed-line support; kitty/iTerm2 inline-image protocols, capability detection, PNG encoding/downscaling, placement cache; SVG decoding via the vendored renderer. |
| `fetch.go` | HTTP(S)/file/data loading, redirects, gzip, charset decoding (UTF-8/Latin-1/CP1252), stylesheet discovery, concurrent image loading with cache, request log, and the in-memory session cookie jar with public-suffix checking and an HttpOnly-hidden view for `document.cookie`. |
| `bookmarks.go` | Netscape bookmark HTML parser, size-capped file import, and the `about:bookmarks` page renderer. |
| `config.go` | Runtime config model, `about:config` form page, and applying edited values without persistence. |
| `browser.go` | History, scrolling, link focus, prompts, status/header/footer, navigation, mouse event decoding and click-to-link hit testing, and resident-memory sampling for the footer. |
| `ui.go` | JS console view, alert/confirm/prompt dialogs and their rendering. |
| `find.go` | Incremental page/HTML find bar, match computation, scrolling and match highlights. |
| `reader.go` | Reader-mode article extraction: candidate scoring, junk filtering, filtered tree cloning and the narrow reader stylesheet. |
| `dark.go` | Dark-mode post-layout palette remap: light backgrounds become dark, dark text becomes light, hues are preserved for links and accents. |
| `js.go` | goja runtime, script execution, internal event dispatch, timers and DOM mutation hooks. |
| `js_dom.go` | DOM wrappers: document/element/text objects, selectors, attributes, `innerHTML`, style and class lists, canvas placeholders. |
| `js_events.go` | Event constructors (`Event`, `CustomEvent`, `MouseEvent`, `KeyboardEvent` and friends), `document.createEvent`, `dispatchEvent`, `MessageChannel` and `MutationObserver` stubs; HTML/SVG element constructor globals and prototype chains for `instanceof` checks. |
| `js_compat.go` | Cheap web-compatibility shims: `dataset`, `outerHTML`, `insertAdjacent*`, attribute helpers, `compareDocumentPosition`, `normalize`, focus/blur/scrollIntoView/getClientRects, Range/Selection, TreeWalker/NodeIterator, TextEncoder/TextDecoder, crypto, structuredClone, AbortController, Blob/File/FileReader, FormData, URL object URLs and queueMicrotask. |
| `js_web.go` | `URL`/`URLSearchParams`, promise-based same-origin `fetch` with `Headers`/`Request`/`Response`, and same-origin `XMLHttpRequest`. |
| `js_intl.go` | English-only `Intl` formatter subset. |
| `term_unix.go` | Shared Linux/macOS key and escape-sequence decoding. |
| `term_linux.go` | Linux termios raw mode, window size and cell pixels via ioctl. |
| `term_darwin.go` | macOS termios raw mode, window size and cell pixels via ioctl. |
| `term_stub.go` | Other-platform build stub; interactive unsupported, dump still works. |
| `signals_unix.go` / `signals_other.go` | Platform-specific resize/quit signal wiring so the project cross-compiles. |
| `cli.go` | `Run`: flags, dump mode, interactive event loop, and the build-time `Version` string reported by `-version`; default `dev`. |
| `*_test.go` | Unit tests alongside the code; no network dependencies. |

## 4. Rendering and layout

- **Scope:** ambitious CSS subset rather than text-only or solid-basics — own
  CSS parser, cascade, specificity, inheritance, selectors, box model, tables,
  lists, backgrounds, borders and text styling. Basic row-direction flexbox and
  basic CSS grid are supported; floating and absolute layout are still
  approximated as block flow.
- **Responsive media queries:** `@media` conditions are stored per rule and
  matched against the layout viewport (`columns * 8` CSS px). `screen`/`all`,
  min/max-width (and device-width), and range syntax such as `(width>=960px)`
  work; `print` never matches; unsupported
  features such as `prefers-color-scheme: dark` do not match. This keeps
  desktop/mobile duplicates from rendering at the same time. Media-query
  results and element class lists are cached for a style pass, so repeated
  rebuilds do not re-parse them for every rule and element.
- **Frame budget:** style computation and layout share a cooperative 2-second
  deadline per rebuild. Rule matching and the block/inline/table/flex/grid
  layout entry points check it; expiry produces a partial layout with
  `Layout.Truncated` set and the status `Page truncated (layout limit)`. This
  is the safety net against pathological pages, alongside the fragment/row
  limits.
- **Flexbox row:** `display:flex`/`inline-flex` with row direction lays children
  side by side, honouring explicit widths, `flex`/`flex-grow`/`flex-basis`,
  shrink-to-fit items and `gap`/`column-gap`. Items keep at least their
  min-content width instead of shrinking into one-character columns.
  Wrapping, `justify-content`,
  `align-items` and ordering are not implemented. Form controls always use the
  inline widget path even when CSS gives them block/flex display, so their
  editable value is rendered inside flex containers.
- **CSS grid:** `display:grid` lays children in row-major order over tracks
  parsed from `grid-template-columns` (lengths, percentages, `fr`, `auto`,
  `repeat()`, `minmax()`), honours `gap`/`column-gap`/`row-gap`,
  `grid-column`/`span`, and sizes row heights from the tallest item. Dense grids
  whose average track would be under 6 cells fall back to block flow so text
  stays readable. Named areas, row spans, auto-fill/auto-fit and alignment are
  approximated.
- **Off-screen transforms:** a `transform` that moves content fully off-screen
  (`translateX/Y(-…)` or `scale(0…)`) marks the element hidden. This suppresses
  cloned fixed navs and closed drawers that rely on transforms instead of
  `display:none`.
- **Cell mapping:** `1 column = 8 CSS px`, `1 row = 16 CSS px`. Two image rows
  are packed into one terminal cell vertically to preserve image aspect ratio.
- **Line height:** computed with `floor(fontSize * lineHeight / 16)`, minimum
  1 row. This avoids body text with `line-height: 1.6` consuming two terminal
  rows. Rule of thumb: only fonts ≥32px produce a 2-row line box.
- **Base font:** 16px, line-height 1. UA headings use bold/color/margins; h1 is
  32px (2 rows), others are effectively 1 row.
- **Large type:** true scaled glyphs use kitty’s text-sizing protocol in real
  kitty with the minimal `OSC 66 ; s=N ; text BEL` form; layout allocates
  `round(font-size/16)` cells per character and matching line rows for runs
  scaled above 1. Do not send the optional `w` key: metadata uses `:` between
  keys, and terminals that only parse `s` render a `w=...` prefix as literal
  text. Disabled on non-kitty terminals, in dump mode, and with
  `-text-scale=off`, where the bold/line-height hierarchy remains. Control
  characters are stripped before text is embedded in the OSC sequence, so page
  text cannot inject terminal escapes.
- **No font files:** the terminal font is used for all glyphs; font size affects
  line height and emphasis, not glyph scale outside kitty.
- **UA page style:** white page background, near-black text, and an `8px`
  `body` margin so pages get the usual browser gutter unless author CSS
  overrides it.
- **Auto margins:** `margin:auto` is distinguished from an unset length; a
  constrained-width block with auto left/right margins centers itself
  (`margin:0 auto`).
- **Borders/grid lines** are solid colored cell rectangles, not box-drawing
  characters. Table borders use a collapsed 1-cell grid.
- **Inline background/space handling:** inter-word spaces are emitted as text
  fragments so backgrounds, link focus highlighting and link rectangles are
  continuous. A collapsed space takes the style and node of the text node that
  contains the whitespace, not the following word, so a link never absorbs or
  underlines the space before it. Inline and inline-block backgrounds (nav
  links, buttons) are painted behind their text fragments; block backgrounds
  are painted once with the box.
- **Contrast fallback:** when resolved text and effective background colors are
  too close (for example white text whose background came from an unsupported
  gradient or CSS variable), foreground switches to near-black on light
  backgrounds or white on dark backgrounds.
- **Wide characters:** East Asian/emoji width 2, combining marks width 0,
  continuation cells marked and skipped in ANSI output.
- **Text wrapping:** greedy word wrapping; overlong words are hard-split;
  `white-space: pre` preserves spaces/newlines; `nowrap` never wraps.
- **Screen-reader-only boxes:** `overflow:hidden` with 1px width and height are
  hidden (`display:none`) to avoid rendering them as one-character columns.
- **Percentage heights are ignored** because parent heights are indefinite;
  this avoids `height:100%` on logos becoming 100 terminal rows.
- **Tables:** shrink-to-fit column widths from max-content measurements, scaled
  down to available width; `colspan` supported; `rowspan` ignored; empty rows
  are skipped.
- **Lists:** markers rendered in the list padding area; disc/circle/square,
  decimal, alpha and roman styles.
- **Images:** disabled by default (`-graphics=none`); no image requests are
  made and inline SVG is not rasterized, so `alt` text or labels are shown.
  When images are enabled, intrinsic size in CSS px → cells; CSS/HTML
  width/height honored;
  aspect ratio preserved; display is capped at 60% of the viewport width and
  24 rows, preserving the ratio; transparency composited over the underlying
  cell;
  PNG/JPEG/GIF and SVG; WebP is unsupported. SVG is rasterized with oksvg +
  rasterx (partial SVG2), capped to 1024 px. External SVG is decoded from the
  response; inline `<svg>` is serialized back to XML with SVG camelCase names
  restored and rasterized before layout. If inline SVG decoding fails it falls
  back to `aria-label`, `<title>` or visible text; `aria-hidden` SVGs are
  skipped. Broken images show `[image: alt]`, or nothing when `alt` is empty.
  In interactive mode with a detected graphics terminal, the same rectangles
  are filled with a real inline image
  (kitty/iTerm2) instead of half-block art; images clipped by the viewport are
  cropped to the visible cell rows. A focused linked image loses its border
  highlight because inline graphics paint above text; the footer still shows
  the target.
- **Graphics protocol preference:** default is `none` (no image loading or
  drawing, for speed). When enabled with `-graphics=auto|kitty|iterm|blocks`,
  kitty comes first, then iTerm2, then half-block art. Auto is selected from
  environment variables. Under tmux/screen/zellij auto-detection
  falls back to half-block art because the multiplexer consumes graphics
  escapes unless passthrough is configured.
- **Graphics encoding:** images are transmitted as PNG (kitty `f=100`, base64;
  iTerm2 `OSC 1337;File`). Text is still painted as ANSI cells; only image
  rectangles are overlaid. Kitty deletes previous placements each frame
  (`a=d`) and re-places images, scaled to the layout cell box, with `q=2` so
  replies do not pollute stdin; APC replies are drained defensively. A
  full-screen clear also clears kitty image data, so the placement cache is
  reset and images are retransmitted after `2J` (scroll, resize, mode switch). iTerm2
  saves/restores the cursor around an inline image sized in cells with
  `preserveAspectRatio=0`. Source images are downscaled to about the terminal
  cell size (default 8×16 px) before encoding. Dump mode always uses half-block
  art for the images that were loaded.
- **Output robustness:** interactive mode disables terminal autowrap
  (`\x1b[?7l`) and restores it on exit, clips double-width glyphs that would
  cross the right edge, repaints the toolbar and footer every frame, and forces
  a full text repaint whenever the scroll offset changes. The URL row is left
  to normal line diffing once drawn, so periodic memory/JS redraws do not wipe
  a terminal-native selection over it. A stray terminal wrap or scroll must
  never leave the chrome stale.
- **Terminal-output safety:** all page/network-derived text reaches the screen
  through `Grid.Put`/`Grid.Text`, which drop C0/C1 control runes (including
  ESC, BEL and C1 CSI/OSC bytes) so text cannot start a terminal escape
  sequence. The console strips controls again at ingestion for JS logs, request
  URLs, storage values and the HTML section, and kitty OSC 66 scaled text is
  stripped explicitly. Dump-mode loader errors are stripped before writing to
  stderr. The only raw escapes written are app-generated ANSI styling, mouse
  mode, clipboard OSC 52 (base64 payload), kitty/iTerm2 image protocols and
  OSC 66.
- **Dump rendering:** prints the whole document height (capped at 20,000 rows),
  no browser chrome. Color is automatic on a TTY, forced by `-color`, disabled
  by `-no-color`.

## 5. Browser / UX

- **Chrome:** a browser-like top bar, two rows on terminals at least five rows
  tall (one row on shorter terminals). The toolbar row holds `← Back`,
  `→`, `⟳`, `⌂`, the page title, and `★ Bookmarks` / `≡ History` /
  `? Help` / `⚙` pinned to the far right; forward, reload, home and config are
  icon-only. The Bookmarks button is hidden unless `-bookmarks` supplied a
  file. The URL row is a full-width field that becomes the editable
  `Open: …` prompt with a cursor marker while the prompt is open. The toolbar
  row uses a subtle horizontal gradient. The page title is centered in the
  space between the left and right buttons and uses a bold amber color for
  prominence.
- **Config:** the `⚙` toolbar icon opens `about:config`, a form listing every
  CLI option with its current value. Editable options apply immediately for the
  session (no-js, ignore-blocks, home, js-memory, text-scale, ua, search);
  on/off options are checkboxes; options labelled “startup” are read-only, and
  colour options are “dump only”. Changes are never persisted.
- **Home:** `-home URL` sets the home target (default `about:home`); the
  toolbar `⌂ Home` button and startup with no URL argument open it. `about:home`
  renders the same built-in start page as `about:start`.
- **History and bookmarks:** `about:history` lists the session's URLs as
  clickable links, most recent first, marks the current entry, caps the list at
  the latest 50 and is generated on demand from in-memory history.
  `about:bookmarks` lists bookmarks imported with `-bookmarks FILE` (Netscape
  HTML export), grouped by flattened folder path. Both pages are memory-only;
  nothing is persisted. The Bookmarks toolbar button only appears when the flag
  supplied a file.
- **Toolbar responsiveness:** back uses a full label at 80+ columns; forward
  (`→`), reload (`⟳`), and home (`⌂`) are always icon-only.
  Bookmarks/history/help use full labels at 80+ columns and icons below; the
  bookmarks item is absent when no `-bookmarks` file was supplied; the
  config gear (`⚙`) is always an icon. Below 24 columns hide
  back/forward/reload so the URL field stays usable; home remains visible down
  to 12 columns; below 18 hide bookmarks; below 12 hide history; below 6 hide
  help and config. Every toolbar item is clickable via per-frame button hit
  regions including the row coordinate.
- **Keys (vim-first):** Vim-style bindings are the preferred convention and
  take priority over browser-style alternatives. `j/k` line scroll, `Space/b`
  page scroll, `Ctrl-D/U` half page, `g/G` top/bottom, `Tab/Shift-Tab` links,
  `Enter` follow, `h/Left` back, `l/Right` forward, `r` reload, `R` toggle
  reader mode, `D` toggle dark mode, `o` URL prompt, `/` find in page,
  `n`/`N` next/previous find match, `u` show URL,
  `y` copy selected page text and clear the highlight,
  `?` help page, `!` JS console, `q`/`Ctrl-C` quit. Do
  not move `g`/`G` or the `o` prompt to browser-style keys. `Esc` clears
  focus/selection and shows `q Quit / ? Help` in the footer; the same hint
  appears after closing the URL prompt, an input field or the JS console.
  Clicking the URL field keeps the prompt open; a press anywhere else closes
  it the same way Escape does. Inside the prompt, a click places the caret.
  Shift+drag is left to the terminal for native text selection.
- **Find:** `/` opens an incremental, case-insensitive find bar in the footer.
  Every match in the rendered page is highlighted; the current match uses a
  stronger color. `Enter` confirms the query and closes the bar, after which
  `n`/`N` navigate matches. While the bar is open, `Down` moves to the next
  match and `Up` to the previous one, with wrap-around. `Esc` also closes the
  bar but keeps highlights; a later `Esc` clears them. The same find bar
  searches the console's HTML tab (`/` while that tab is active), including
  scrolling to and highlighting matching lines. Queries may contain spaces:
  whitespace runs collapse to one space, and phrases match across word
  fragments and wrapped lines, with every rectangle of a multi-line match
  highlighted. It works on every rendered page, including `about:help` and
  `about:bookmarks`.
- **Dark mode:** `D` or the footer `Dark on`/`Dark off` label toggles a
  post-layout palette remap for every page. Light backgrounds become dark,
  dark text becomes light, and hues are preserved for links, headings and
  accents; images are left unchanged. The preference persists across
  navigation and reload, and `-dark` starts with it enabled, including in
  `-dump` mode. It also applies on top of reader mode.
- **Reader mode:** `R` or the footer `Reader on`/`Reader off` label toggles an
  extracted article view. Extraction runs on the live DOM, prefers
  `<article>`/`<main>`/`role=main`, scores other prose containers, drops
  nav/sidebar/footer/forms/scripts and common junk-class subtrees, strips page
  CSS, and renders a filtered clone in a centered `max-width:600px` column.
  Images are removed entirely, including `alt` text and `[image: ...]` labels;
  only text, headings, links, lists, tables, quotes and code remain. The
  original page tree, URL and history are untouched. Reader mode stays enabled
  across navigation and reload until `R` or the icon is used again: each new
  page is extracted automatically, and a page with no usable article is shown
  normally with `Reader mode unavailable` while the preference remains on.
  Toggling off restores the normal page and its previous scroll position.
  `-reader` starts with the preference on, including `-dump` mode, which prints
  the extracted view when one is available. Reader mode is a snapshot: live JS
  mutations are not reflected while it is active, and interactive controls are
  not available.
- **Start page:** `about:home` and `about:start` show the `docs/icon.svg`
  artwork embedded directly in `startPageHTML` (inline `<svg>`, no runtime
  fetch), rendered through the normal inline-SVG path and sized by `.brand svg`
  to `64px` (8×4 cells). Keep the inline path data in sync with
  `docs/icon.svg`; `docs/icon.png` is only a raster copy.
- **Help page:** `?` and the toolbar `?` navigate to the built-in
  `about:help` HTML page. Help lives as a normal rendered document (tables for
  command-line flags, keys, mouse and forms), not as a one-line footer status,
  so every shortcut including `!` is visible and selectable. The flag table
  must list every option accepted by `Run`. The page includes try-it links for
  alert/confirm/prompt and a demo form with text input, checkbox, radio,
  select, textarea and submit button; inline handlers work even on pages with
  no `<script>` elements.
- **Focus:** focused links use a yellow background highlight on text and a
  yellow border on images; the focused URL is shown in the footer.
- **Mouse:** enable click reporting with `\x1b[?1000h`, all-motion tracking
  with `\x1b[?1003h` (needed for link hover), and SGR extended coordinates
  with `\x1b[?1006h`; disable all three on exit. SGR (`\x1b[<b;x;yM/m`) and
  legacy X10 (`\x1b[M` + 3 bytes) events, including motion reports, become
  `mouse:` input tokens. Hovering a link shows its resolved target in the
  footer and clears when the pointer leaves it. Left press starts a drag
  selection in document coordinates; dragging updates and auto-scrolls it,
  and release keeps the highlight. A press and release on the same cell is a
  click: it follows a link using the same rectangles as keyboard focus, or
  clears focus. While the URL prompt is open, a click on its field keeps it
  open and a click anywhere else closes it; clicking inside it positions the
  caret. Shift+mouse reports are ignored and suppress the next redraw so the
  terminal can keep a native selection over the URL. A left click on the URL
  bar opens the URL prompt, toolbar
  buttons act on click, right click and the mouse back button go back, the
  mouse forward button goes forward, clicking the footer `Dark on`/`Dark off`
  or `Reader on`/`Reader off` labels toggles those modes, clicking the footer
  `>_` icon toggles the JS console (and closes it while open), and the wheel
  scrolls three lines. Long pages show a scrollbar
  on the right edge: click or drag it to jump through the document. `Esc`
  clears the selection. `y` copies the selected page text through the terminal
  OSC 52 clipboard sequence and then clears the highlight (subject to
  terminal/multiplexer support).
- **Forms:** clicking a text/search/email/URL/tel/number/password input or a
  textarea focuses it and shows a cursor; typing edits the value, Backspace
  deletes, Ctrl-U clears, Enter submits, and Esc finishes. Clicking a
  checkbox or radio toggles it (radios uncheck same-name siblings); clicking a
  select cycles its options. Submit
  buttons run their `click` handler and then submit their form on release
  unless the handler prevents default; `type=button`/`type=reset` buttons and
  buttons outside a form only run their click handlers. GET forms build a query
  from named fields
  (including hidden inputs, checked checkboxes/radios and selects); POST forms
  send `application/x-www-form-urlencoded` bodies. Both
  navigate to the resolved action, and reload reuses the last method/body.
  Focusing/blurring fires `change`, editing fires `input`, toggling fires
  `input`/`change`, and submitting fires `submit` before navigation. JS exposes
  `value`, `checked` and `form` on elements. Editable fields use a prominent
  light-blue background with bold text; the focused field switches to a solid
  blue background with white text, and its placeholder/hint disappears while
  focused.
- **Footer:** shows the hovered link target or status on the left and, on the
  right, an optional `JS: Error (! console)` hint, current resident memory
  (`MEM 18 MB`), the scroll percentage, clickable `Dark on`/`Dark off` and
  `Reader on`/`Reader off` labels before the memory readout, and a clickable
  `>_` console icon pinned to the far right edge. Each label is blue and bold
  while its mode is on; the console icon toggles the JS console and stays
  visible and clickable inside the console view. Status text is amber and bold for warnings (unsupported ES modules, layout truncation, blocked pages/HTTP
  errors, blocked local navigation, unsupported protocols/JS links), red and
  bold for errors (`Error:`, invalid URL/form action, copy failure), and green
  for successful copy messages. The error hint clears
  when the JS console opens or a new page loads, and drops before memory on
  narrow terminals. The readout refreshes every two seconds.
  Memory is `/proc/self/statm` RSS on Linux
  with `runtime.MemStats.Sys` as the fallback. The readout is not repainted
  while the JS console is open, so terminal selection is not disturbed.
- **History:** a URL string stack with forward truncation; fragment-only links
  scroll within the page instead of pushing history.
- **Loading:** navigation runs asynchronously so the UI stays responsive.
  `Esc` cancels the in-flight page, stylesheet, script and image requests and
  shows “Stopped loading”. Late results from canceled navigations are ignored
  by sequence number. Loading another URL cancels the previous load.
- **Refused schemes:** `javascript:`, `mailto:` and `tel:` links show a status
  message.
- **URL input rules:** opening the URL prompt (`o` or clicking the URL field)
  prefills the current URL and selects it all, so typing replaces it like a
  normal browser address bar; Backspace clears the selection first. The prompt
  has a real cursor: Left/Right, Home/End move it, Backspace/Delete remove
  around it, Ctrl-A selects all, Ctrl-K deletes to the end, Ctrl-U clears and
  Ctrl-W deletes the previous word. A mouse click places the caret. Shift+drag
  is not app-managed selection: Shift+mouse reports that do arrive are ignored
  and no redraw is written, so terminals that handle Shift locally can select
  and copy the URL text normally.
  The caret is an overlay block cursor, not
  an inserted cell, so moving it never shifts the URL text. Existing local paths become `file://`;
  bare host-like input becomes `https://`; `host:port` becomes `http://`;
  relative references against a base only for `/`, `./`, `../`, `#`, `?`.
- **Search fallback:** input that contains whitespace or has no dot (for
  example `golang tutorial` or `golang`) is treated as a search query.
  `-search` takes a URL template whose `%s` is replaced with the URL-escaped
  query; the default is `https://html.duckduckgo.com/html/?q=%s` (the
  server-rendered endpoint; the main DuckDuckGo page is JS-only and renders
  blank without scripts), and an empty value disables the fallback. Valid URLs,
  `example.com`, `host:port`, local files, `about:`/`data:` and base-relative
  references still resolve as URLs. The fallback applies only to typed
  navigation, not link hrefs or form actions.
- **Non-HTML content:** a “cannot display” page is shown (plain text is shown in
  a `<pre>`); binaries are never rendered as HTML.
- **JavaScript scope:** classic inline and external scripts run in document
  order after load and before layout. DOM subset: `getElementById`,
  `querySelector(All)`, `getElementsByTagName`, `createElement`,
  `createTextNode`, attributes, `textContent`/`innerText`/`innerHTML`,
  `classList`, `style` (common properties plus `setProperty`/`cssText`),
  `appendChild`/`removeChild`/`replaceChildren`, `children`/`parentNode`, and
  `document.title`/`URL`. Element wrappers use constructor prototypes, so
  `instanceof HTMLElement`, `instanceof HTMLScriptElement`, `instanceof Node`
  and similar checks work; document, text, SVG, `CharacterData` and common HTML
  element constructors are exposed globally and on `window`. `<dialog>`
  elements expose `open`, `returnValue`, `show()`, `showModal()` and `close()`;
  only dialogs with the `open` attribute render, and `close()` fires `close`.
- **Events:** `Event`, `CustomEvent`, `UIEvent`, `MouseEvent`, `KeyboardEvent`,
  `FocusEvent`, `InputEvent`, `PointerEvent`, `ErrorEvent`, `MessageEvent` and
  `ProgressEvent` constructors exist; `document.createEvent` returns the
  matching type with `initEvent`/`initCustomEvent`/`initMouseEvent`, and
  `dispatchEvent` (with bubbling and `preventDefault`) works on elements,
  `document` and `window`. `element.click()` dispatches a click. `NodeFilter`,
  `Node.*` constants and `performance.timing`/`navigation` are present.
  `MessageChannel` delivers `postMessage` through the timer queue;
  `MutationObserver` is a no-op observer. Canvas `getContext("2d")` returns a
  no-op context (no pixel buffer); WebGL returns `null`.
- **JS globals:** `window` exposes `document`, `navigator` (matching User-Agent,
  language, platform), `location`, `performance`, `innerWidth`/`innerHeight`,
  `screen`, `self`/`top`/`parent`, timers, `requestAnimationFrame`, events,
  `getComputedStyle` and `console`. `Image`/`Audio` construct DOM elements. `document` exposes
  `location`, visibility state, `defaultView`, `document.implementation`
  (`hasFeature`/`createHTMLDocument`), and image elements expose
  `src`/`alt`/`width`/
  `height`/`complete`/`natural*`. `URL`/`URLSearchParams`,
  `document.currentScript`/`baseURI`, `document.write`/`writeln`,
  `document.cookie`, and in-memory `localStorage`/`sessionStorage` are
  available. Storage supports `getItem`/`setItem`/`removeItem`/`clear`/`key`/
  `length` plus named property reads, writes and `delete` against the same
  map. `document.cookie` reads and writes the same in-memory session jar
  as HTTP requests, honors Domain/Path/Expires/Max-Age/Secure and hides
  HttpOnly cookies; `navigator.cookieEnabled` is true. Cookies are not
  persisted. `XMLHttpRequest` supports
  basic same-origin HTTP methods, headers, `responseText`/`response` (text/
  json/arraybuffer), status and callbacks. Cross-origin requests and redirects
  are blocked (no CORS). It executes synchronously even when `async=true` and
  does not implement streaming, upload progress or timeouts. `fetch` is
  available with `Headers`, `Request` and `Response`; it follows the same
  same-origin rule, resolves a promise, and supports `text`, `json`,
  `arrayBuffer` and `blob` bodies. It is likewise synchronous under the hood
  and has no streaming or upload progress; a basic `AbortController` only
  rejects a fetch that has not started. `Intl` provides
  an English-only practical subset: `NumberFormat`, `DateTimeFormat`,
  `Collator`, `PluralRules`, `RelativeTimeFormat`, `ListFormat`,
  `getCanonicalLocales` and `supportedValuesOf`, without full CLDR locale
  data. `<noscript>` is removed before styles and scripts are collected when
  JS is enabled, so no-JS styles/scripts cannot affect the JS page.
  `console.*` and uncaught errors append to a bounded JS log. Module scripts
  remain unsupported, so globals defined only by modules (for example a
  third-party `google` object) stay undefined.
- **JS dialogs:** `alert`, `confirm` and `prompt` open a centered modal and
  block script execution until answered. Alert closes on Enter/Space/Esc;
  confirm uses Enter/Y for yes and N/Esc for no; prompt supports the same text
  editing keys as the URL bar. In non-interactive/dump runs, alert returns,
  confirm returns false and prompt returns null. Dialogs use keyboard input
  (mouse is not handled while modal).
- **JS console view:** `!` opens a plain-text console in the viewport. A
  one-row menu at the top has four tabs: JavaScript logs, Requests, Storage and
  HTML. The active tab is highlighted blue; only its section body is shown
  below the menu, and the logs tab is active by default. The JavaScript input
  row is part of the logs body, pinned to its bottom. Clicking a tab switches
  sections, and `Tab`/`Shift-Tab` cycle through the same four tabs. Mouse
  reporting stays enabled so tabs and the input row can be clicked (plain
  terminal drag-selection needs the Shift override).
  `i` or `Enter` (or a click on the input row) focuses the input; typing edits a
  one-line expression and `Enter` evaluates it in the page runtime under the
  usual JS timeout/memory guards. The input row keeps its gray background while
  focused. Up/Down recall earlier expressions from the in-memory console
  history (consecutive duplicates are dropped, capped at 200 entries); Down
  past the newest entry restores the draft. History is not reset by navigation.
  Input and results are appended to the log as
  `[input]` and `[result]`, the result appears above the input row, dirty DOM
  mutations rebuild the page, and `-no-js` logs that JavaScript is disabled.
  `Esc`/`!` close and `q` quits when the input row is not focused (`q` is
  literal text while typing). The footer keeps its clickable `>_` icon while
  the console is open; clicking it closes the console the same way `Esc` does
  (including the usual exit hint).
  The JS log and input draft reset when a new page is successfully applied,
  before that page's scripts run; failed loads keep the previous page's log.
  Requests list status, method, size, kind and URL (plain text, not clickable)
  for the page, CSS, scripts, images, `fetch` and XHR; the list is in memory
  only, resets on navigation and is capped at the latest 512 entries. Storage
  lists the current page's cookies — including HttpOnly cookies, marked as
  such — and the in-memory `localStorage`/`sessionStorage` key/value pairs.
  HTML shows the raw decoded page source (pre-JavaScript DOM, wrapped to the
  console width and opening at the top) retained as `Page.RawHTML`, cached per
  page and width.
  Timers and timer-driven redraws pause while the console is open, so a
  terminal selection is not cleared by redraws. The full error text is kept
  there; the footer only carries a short `JS: Error (! console)` hint.
- **JavaScript events and timers:** `addEventListener`/`removeEventListener`
  on nodes, `document` and `window`; bubbling `click`; inline `onclick`
  attributes and the `onclick` property; `DOMContentLoaded` and `load`.
  `preventDefault()` suppresses link navigation. `setTimeout`/`setInterval`
  are pumped by a 50 ms interactive tick with a 10 ms interval floor; console
  messages go to the `!` console, `console.error` sets the footer hint, and
  `alert` appears in the footer. Timer callbacks that dirty the DOM do not
  rebuild on every tick: timer-driven rebuilds are coalesced to at most once
  per `max(250 ms, 2× the previous rebuild time)`, so a page whose timers fire
  every 50 ms cannot consume every core with full style/layout passes.
  User-input redraws remain immediate.
- **JavaScript safety:** every script and callback runs under a 2-second goja
  interrupt watchdog and the `-js-memory` heap budget (256 MB by default; 0
  disables). Timer count and per-tick callbacks are capped. Script
  errors set the right-side `JS: Error (! console)` footer hint and are logged
  for the `!` console; they do not replace the left status text. Navigation via
  `location.href`,
  `location.assign`/`replace`/`reload` is queued until the script or callback
  finishes, then starts a normal load. `<meta http-equiv="refresh">` redirects
  are followed immediately. Storage and the cookie jar are in-memory only and
  nothing is persisted. `XMLHttpRequest` and `fetch` use the shared cookie jar
  and User-Agent but are synchronous under the hood. There is no CSP or module
  loading; `history.back`/`forward` are stubs.
- **Dump mode with JS:** `-dump` runs scripts and `DOMContentLoaded`/`load`
  synchronously so output reflects DOM changes; timers do not run in dump mode.

## 6. Networking / resources

- Cookie jar and redirects via `net/http`; the page’s final URL is kept for
  relative links and history. The jar is memory-only, uses the public suffix
  list for domain scoping, and is shared by page loads, resources, XHR, fetch
  and `document.cookie`. HttpOnly cookies are hidden from JavaScript through a
  separate visible jar. The SameSite attribute is stored but not enforced.
- Page/resource cap: 12 MB. Local files are read with a `LimitReader`, only
  regular files are accepted (no `/dev/*`/FIFOs), and data URIs are rejected
  when the encoded or decoded size exceeds the cap. Images are rejected before
  decoding when they exceed 25 megapixels, to avoid decompression bombs.
- **JS memory budget:** each guarded script/callback has a 2-second CPU timeout
  and a best-effort heap-delta watchdog, 256 MB above its starting heap by
  default. `-js-memory` sets the budget in MB (0 disables the watchdog, leaving
  the CPU timeout in place). Exceeding the budget interrupts the script with an
  error.
- **Layout budget:** style and layout stop after a 2-second cooperative
  deadline, and layout also stops after 250k fragments or 200k document rows,
  bounding CPU and memory for pathological or generated documents. The frame
  is rendered partially and marked truncated when a limit is hit.
- **Same-origin XHR and fetch:** JavaScript `XMLHttpRequest` and `fetch` may
  only request the page's own scheme/host/port (default ports normalized).
  Cross-origin requests and cross-origin redirects fail; no CORS is
  implemented. Cookies therefore cannot be read cross-site through XHR or
  fetch.
- **Local files:** `file:` resources (stylesheets, scripts, images) are fetched
  only when the document itself was loaded from `file:`. Remote (http/https/
  data) documents cannot load file resources, navigate the viewport to `file:`
  URLs, or XHR local files. Local documents retain access to local resources.
- Charset from Content-Type, falling back to `<meta charset>` sniffing.
- Stylesheets from `<style>`, `<link rel=stylesheet>`, and inline styles;
  `@import` is not supported. Parsing is cancellable and capped at 4 MiB of
  source, 50k rules, 256 KiB per declaration block, 4 KiB per selector and
  1 second of parse time; repeated selector text is parsed once per sheet.
  This bounds CPU/GC cost on generated or hostile CSS.
- Images load concurrently (max 6) and are cached per resolved URL for reloads.
- Blocked responses are replaced with a locally generated “Site blocked” page
  with a `about:retry` **Try anyway** link; by default they do not run the
  site’s challenge scripts or load its assets. Clicking the link re-fetches the
  page once with block detection disabled, and `-ignore-blocks` disables the
  detection for all loads.
- Default User-Agent: **`MOB3/1.0 (+terminal; +https://mob3.net)`**. MOB³ identifies itself
  rather than impersonating another browser; UA-sniffing sites may therefore
  serve simplified or no-JavaScript pages. `-ua` overrides it. Keep this string
  documented in `README.md`, the built-in help page and `AGENTS.md` whenever it
  changes.
- HTTPS uses Go’s TLS; there is no certificate UI or proxy configuration UI.

## 7. Build, test and compatibility

- `make` = `mkdir -p bin && CGO_ENABLED=0 go build -mod=vendor -trimpath -ldflags="-s -w" -o bin/mob3 ./cmd/mob3`
- Dependencies are vendored in `vendor/`; `make vendor` regenerates it and the
  tree must build without network access. `vendor/` is committed, never cleaned.
- Build output lives in `bin/` (ignored by git); `make clean` removes `bin/`.
- `Dockerfile` is a multi-stage build: `golang:1.27-bookworm` cross-compiles
  the vendored static binary from `$BUILDPLATFORM`, and `alpine:3` runs it as a
  non-root user with CA certificates. Alpine is required so the image can cover
  `linux/arm/v6`; distroless has no armv6 manifest. The build stage copies only
  `go.mod`, `go.sum`, `vendor/`, `cmd/` and `internal/`; do not use `COPY . .` or
  a `.dockerignore`. `make docker` builds `mob3`, and the image includes
  `LICENSE` and `THIRD_PARTY_NOTICES` under `/usr/share/doc/mob3`.
- `make test` = `go test -mod=vendor ./...`; tests must not require network
  access.
- Binaries and test/coverage output are covered by `.gitignore`; keep it in sync
  when the layout or build output changes.
- `go vet ./...` must stay clean, and `gofmt` is applied to project files before
  handing work back. Do not reformat files under `vendor/`.
- Interactive mode is supported on Linux (`term_linux.go`) and macOS
  (`term_darwin.go`), sharing key decoding from `term_unix.go`; other platforms
  fall back to dump output. Windows is not a build target; Windows users run
  the Linux binary under WSL.
- Keep the binary out of layout decisions: it may be committed/present locally,
  but `make clean` removes it.
- The Git tag (`vX.Y.Z`) is the release version source of truth.
  `internal/browser.Version` defaults to `dev` and is set at build time with
  `-ldflags "-X browser/internal/browser.Version=<tag>"`; `mob3 -version` prints
  it. Release binaries and the Docker image both receive `github.ref_name`.
  Local builds use `make VERSION=vX.Y.Z` or
  `docker build --build-arg VERSION=vX.Y.Z`.
- `.github/workflows/ci.yml` runs gofmt, `go vet` and tests on pushes to `main`
  and on open, synchronize, reopen and edit pull-request events.
  `.github/workflows/pr.yml` runs only for pull requests and requires the exact
  statement `I have read and agree to the MOB³ Contributor License Agreement.`
  in the PR description.
- The GitHub Pages one-pager lives on the generated `gh-pages` branch.
  `.github/pages/build.sh` converts `README.md` with pinned `marked` (via
  `npx`) and wraps it in `.github/pages/template.html`;
  `.github/workflows/pages.yml` rebuilds and force-pushes the branch on `main`
  pushes touching the README, page files or workflow, and on manual dispatch.
  The build writes a `CNAME` for **`mob3.net`**; the site is served at
  `https://mob3.net` (apex A/AAAA DNS to GitHub Pages, `www` CNAME to
  `mob3net.github.io`, HTTPS enforced in the Pages settings). Never edit
  `gh-pages` by hand; change the README, template or build script.
- Releases are built by `.github/workflows/release.yml` on any `v*` tag push,
  not on `release: published`, because release-triggered workflows run from the
  default branch. The workflow checks gofmt, `go vet` and tests first, then
  builds seven targets with vendored dependencies and `CGO_ENABLED=0`: Linux
  386/amd64/armv6/armv7/arm64 and macOS amd64/arm64.
- Release assets are named `mob3_<tag>_<os>_<platform>.tar.gz` and include
  `SHA256SUMS`. Every archive contains `mob3`. The release job verifies that
  exactly seven archives were produced, creates the GitHub release from the tag
  with generated notes when it is missing, and uploads with `--clobber` so
  re-runs replace assets. The release job checks out full history so
  `gh release create --generate-notes` can find tags.
- `.github/workflows/docker.yml` runs after the `Release binaries` workflow
  completes successfully (`workflow_run`) and publishes a multi-arch image to
  `ghcr.io/mob3net/mob3` for `linux/386`, `linux/amd64`, `linux/arm64`,
  `linux/arm/v6` and `linux/arm/v7`, tagged with the release tag and `latest`.
  It resolves the tag from the triggering run and uses `GITHUB_TOKEN` with
  `packages: write`. The first GHCR package is private by default; making it
  public is a manual repository setting.
- Release jobs use native runners (Ubuntu, macOS) so signing tools are
  available. Signing is optional and driven by repository secrets; missing
  secrets publish unsigned archives rather than failing the release. macOS uses
  a Developer ID certificate plus `codesign` and optional `notarytool`
  notarization from `MACOS_CERT_P12_BASE64`, `MACOS_CERT_PASSWORD`,
  `MACOS_KEYCHAIN_PASSWORD`, `MACOS_SIGNING_IDENTITY` (first valid identity when
  unset), `MACOS_NOTARY_APPLE_ID`, `MACOS_NOTARY_TEAM_ID` and
  `MACOS_NOTARY_PASSWORD`.

## 8. Known limitations (accepted)

- JavaScript is a practical subset: modules and CSP are unsupported. A page
  containing `<script type="module">` is shown with the status
  `Page requires ES modules (unsupported)` so an SPA boot/loading screen does
  not look like a hang; the module scripts are still skipped. Storage
  and cookies are in-memory only and do not persist. Cookies are session-only,
  `SameSite` is not enforced, and there is no `CookieStore` API.
  `XMLHttpRequest` and `fetch` are synchronous, same-origin-only and lack
  streaming/upload progress; `AbortController` only cancels fetches that have
  not started. `Intl` is an
  English-only subset without full CLDR locale data. `document.write` is
  supported for document fragments. JS history navigation is stubbed. Plain
  same-origin `location` redirects and meta refresh work; remote pages cannot
  navigate to `file:`. Canvas is a feature-detection stub with no pixels or
  WebGL, `MutationObserver` never reports mutations, `TreeWalker` and the custom
  elements registry are absent, and `MessageChannel` delivery is timer-based.
  Apps requiring missing APIs (for example Google search) may still show their
  no-JavaScript fallback.
- Cross-origin scripts/stylesheets/images still load like a normal browser and
  share the cookie jar (third-party requests), with no CSP. This is a separate
  residual risk from XHR; do not treat MOB³ as a security boundary.
- Browser checks and anti-bot/firewall blocks cannot be completed. On
  401/403/429/503 responses, MOB³ looks for Cloudflare, CAPTCHA,
  DataDome/PerimeterX, Imperva/Sucuri, “verify you are human”, “unusual
  traffic” and access-denied signatures and shows a generic “Site blocked”
  page with the detected reason, HTTP status and URL. Challenge scripts are
  not executed unless the user follows the page’s one-shot **Try anyway** link
  or passes `-ignore-blocks`; even then most checks fail, so sites behind such
  protection are generally not usable. MOB³ is not a security boundary and
  challenge pages may run arbitrary script once explicitly retried.
- No tabs or downloads. Forms support editable text fields, checkbox/radio
  toggling, click-to-cycle selects and urlencoded GET/POST submission;
  multipart/file uploads are not implemented. Mouse support covers
  click-to-follow, page drag selection, the URL prompt, right-click back and
  wheel scrolling. `y` copies selected page text through OSC 52; terminal-native
  Shift+drag handles URL selection. The `navigator.clipboard` JavaScript API
  remains unsupported.
- Graphics-protocol detection is environment-based, not an active terminal
  query; users may need `-graphics` in unusual setups.
- No font files; no real text scaling outside kitty’s `OSC 66`.
- Flexbox supports basic rows only (no wrap/justify/align/order); grid supports
  basic row-major tracks but not named areas or row spans. Floats and absolute
  positioning are approximated as block flow.
- SVG support is partial: paths, shapes, gradients and common transforms work,
  but SVG text, `<use>/<symbol>`, filters, masks, CSS-in-SVG and animation may be
  missing. Inline SVG falls back to an accessible label if rasterization fails.
  WebP is still unsupported.
- Reader mode is heuristic: unusual layouts can lose content or retain
  clutter, it renders a snapshot rather than live JS updates, and images,
  forms and other interactive controls are removed.
- No CSS variables, animations, transitions, generated content, `@import`, or
  background images. `<dialog>` open state controls rendering, but modal
  dialogs are laid out in normal block flow without a backdrop, focus trap or
  top-layer stacking.
- Modern CSS-heavy sites may look rough; content-focused pages, docs, and old
  HTML render best.
- Interactive mode needs a real Linux or macOS terminal. Windows is not a build
  target; use WSL.

## 9. Change protocol for agents

1. **Ask before deciding**, one question at a time, and ask again if a
   previously unanswered question still matters.
2. **Update this file first** (or in the same change) whenever the user gives a
   new important requirement, changes a previous decision, or chooses between
   alternatives. Fold it into the section it governs as the rule in force; do
   not append a dated history entry and drop what it supersedes. Update
   `README.md` for any user-visible change in the same edit.
3. **Docs are deferred.** There is no `docs/` tree for now; do not create or
   maintain a documentation mirror until the user asks for one.
4. Keep the “Known limitations” section honest; remove items only when actually
   implemented.
5. Add or update tests in the same change as the behavior, then run `gofmt`,
   `go vet ./...`, and `go test ./...` before handing work back.
6. Never introduce external Go modules or cgo without an explicit user change
   to section 2. `github.com/dop251/goja` is the only currently approved
   third-party module.
7. Keep the README user-facing; keep this file decision-facing.
8. End every completed change with a short one-line commit message (do not
   create the commit unless the user asks).

## 10. JavaScript/web API compatibility reference

MOB³ targets core DOM plus jQuery-era libraries, not a modern browser
platform. Keep this list current when the JS surface changes.

Works well:
- goja language level: ES5.1 plus much of ES6 (classes, arrows, generators,
  async/await, Promise, Map/Set, Symbol, Proxy, typed arrays).
- DOM core: tree queries and mutation, attributes, `classList`, inline
  `style`, `textContent`/`innerText`/`innerHTML`, cloning, selectors,
  `getBoundingClientRect` (coarse), `getComputedStyle`, `ownerDocument`,
  `defaultView`, `document.implementation`.
- Events: Event/CustomEvent/UI/Mouse/Keyboard/Focus/Input/Pointer/Error/
  Message/Progress constructors, `createEvent`, bubbling `dispatchEvent`,
  `preventDefault`, `element.click()`.
- Network: same-origin `fetch`, `Headers`/`Request`/`Response`, same-origin
  `XMLHttpRequest`, cookies, `URL`/`URLSearchParams`.
- Storage: in-memory `localStorage`/`sessionStorage` (`getItem`/`setItem`/
  `removeItem`/`clear`, `length`, and named property get/set/delete) and
  `document.cookie`.
- Canvas: `getContext("2d")` is a no-op context with the common method surface;
  no pixels and no WebGL.
- Native `<dialog>` open/close state: `open`, `returnValue`, `show()`,
  `showModal()` and `close()`.
- Compatibility shims (`js_compat.go`): `dataset`, `outerHTML`,
  `insertAdjacentElement/Text`, `getAttributeNames`, `toggleAttribute`,
  `compareDocumentPosition`, `normalize`, `focus`/`blur`/`scrollIntoView`,
  `getClientRects`, `Range`/`Selection`/`getSelection`,
  `createTreeWalker`/`createNodeIterator`, `TextEncoder`/`TextDecoder`,
  `crypto.getRandomValues`/`randomUUID`, `structuredClone`, `AbortController`/
  `AbortSignal`, `Blob`/`File`/`FileReader`, `FormData`, `URL.createObjectURL`
  and `queueMicrotask`.

Intentional limits of the shims:
- `structuredClone` covers primitives, plain objects, arrays, Date, Map, Set
  and ArrayBuffer; typed arrays become plain arrays. Functions throw.
- `AbortController` only rejects a `fetch` that has not started yet; requests
  are synchronous under the hood and cannot be cancelled mid-flight.
- `FormData` and Blob object URLs are in-memory only. Multipart uploads, Blob
  image loading and request-body streaming are not implemented.
- `MutationObserver` is a no-op, `MessageChannel` is timer-backed,
  `getSelection`/`Range` are informational, and `TreeWalker` rebuilds the
  preorder list on each step.
- Measurement APIs are approximate: scripts run before layout, so
  `getComputedStyle` and rectangles may reflect defaults until after load.
- Inline `style` exposes `transition`/`transform`, animation properties and
  their vendor-prefixed aliases so libraries' CSS feature detection succeeds,
  but transitions and animations are not rendered.
- Dynamically inserted `<script>` elements do not execute; modules, `defer`
  and `async` are ignored.

Known unsupported families (do not present these as available):
- Platform: WebAssembly, Workers/SharedWorker/ServiceWorker, WebSocket,
  EventSource, IndexedDB, Cache API, WebAudio, WebGL, WebRTC, Notifications,
  geolocation, clipboard, permissions, mediaDevices, PaymentRequest.
- DOM: Shadow DOM/`attachShadow`, custom elements registry, real
  Intersection/Resize/Performance observers, iframe browsing contexts,
  `<template>.content`, video/audio playback.
- Network/browser: streaming fetch, upload progress, CORS, `sendBeacon`,
  full History API (`pushState`/`replaceState` are no-ops, `back`/`forward`
  are stubs), multipart forms.
- CSS/layout: named grid areas, grid row spans, floats, absolute positioning,
  CSS variables, animations, transitions, generated content, `@import`,
  background images, full flexbox.

Do not add a stub just to silence an error when a library would then depend on
behavior MOB³ cannot provide; document it here instead or implement the real
subset.
