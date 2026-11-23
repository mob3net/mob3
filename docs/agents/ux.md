# Browser and UX

Part of the MOB³ agent guidance. Start at [AGENTS.md](../../AGENTS.md) for the working
agreement, the non-negotiable constraints and the change protocol.

- **Chrome:** a browser-like top bar, two rows on terminals at least five rows
  tall (one row on shorter terminals). The toolbar row holds `← Back`,
  `→`, `⟳`, `⌂`, the page title, and `≡ History` /
  `? Help` / `⚙` pinned to the far right; forward, reload, home and config are
  icon-only. While a page is loading, the reload icon becomes `×` and clicking it
  cancels the in-flight load exactly like `Esc` (status `Stopped loading`),
  then the reload icon returns. The URL row is a full-width field that becomes the editable
  `Open: …` prompt with a cursor marker while the prompt is open. A clickable
  ` 100% ` zoom badge always keeps the far-right cells of that row (one space
  on each side), and the URL or prompt text truncates before it; clicking the
  badge opens the centered Zoom window for choosing a preset with the mouse or
  arrow keys. While loading,
  the URL row fills from the left with a blue progress tint that eases toward
  90%, completes on finish and lingers briefly before clearing; it is driven by
  an event-loop timer only while progress is active, so idle pages stay asleep.
  The toolbar
  row uses a subtle horizontal gradient. The page title is centered in the
  space between the left and right buttons and uses a bold amber color for
  prominence.
- **Config:** the `⚙` toolbar icon opens `about:config`, a form listing every
  CLI option with its current value. Editable options apply immediately for the
  session (no-js, ignore-blocks, home,
  mem-limit, layout-budget, zoom, text-scale, ua, search, certs);
  on/off options are checkboxes; options labelled “startup” are read-only, and
  colour options are “dump only”. A rejected value keeps the current setting
  and shows the reason above the table. `certs` accepts only `system` and
  `embedded`: certificate files are not supported. Changes are never
  persisted. The form posts its values with a one-shot session token, so a
  link, script or typed `about:config?...` URL without that token applies
  nothing; use the form's Apply button. The page starts
  with the current viewport in characters and pixels (`width*cellW` by
  `viewportHeight*cellH`, using the terminal cell pixels or the 8×16 default)
  and the form uses the same centered `max-width:900px` reading column as
  `about:help`.
- **MCP automation:** `-mcp` starts a headless `Automation` session and serves
  MCP over stdin/stdout instead of entering the terminal UI. It accepts an
  optional start URL, `-width` and `-height` (default 30); `-no-js`,
  `-layout-budget` and `-zoom` are honored, and images are disabled. Tools cover
  navigate, snapshot, click, link listing, form/control listing, fill, select,
  toggle, submit, wait/wait_for, type, press, scroll, history navigation,
  JS evaluation, session history, diagnostics (`requests`, `logs`, `source`) and
  a
  PNG `screenshot` of the current viewport. The snapshot is text plus visible
  `[n]` links and form-control coordinates. `links` lists every rendered anchor
  (including off-screen ones) with its `[n]` index, href and selector; `click`
  also accepts visible `text` and prefers exact matches on links and buttons.
  `form` lists every form and control
  (including off-screen ones) with stable CSS selectors; `fill`, `select` and
  `toggle` set values by selector without focus or scrolling, and `submit` runs
  constraint validation and the submit handler, waits for navigation, and
  reports validation/prevented failures as tool errors. File uploads and
  multipart forms remain unsupported. `requests`, `logs` and `source` expose the
  current page's request log, JavaScript messages/errors and fetched HTML; the
  logs and request list cover the current page, reset on navigation and are
  memory-only. The screenshot is a terminal-cell raster,
  not a browser pixel capture. Stdout is protocol-only. Sessions, cookies and
  history are memory-only, and the server is local and unauthenticated. Client
  setup for OpenCode, Claude Code, Cursor and Claude Desktop is documented in
  `docs/automation.md`.
- **Home:** `-home URL` sets the home target (default `about:home`); the
  toolbar `⌂ Home` button and startup with no URL argument open it. `about:home`
  renders the same built-in start page as `about:start`.
- **History:** `about:history` lists the session's URLs as
  clickable links, most recent first, marks the current entry, caps the list at
  the latest 50 and is generated on demand from in-memory history. `H` or the
  toolbar History button opens it. The page is memory-only; nothing is
  persisted.
- **Toolbar responsiveness:** back uses a full label at 80+ columns; forward
  (`→`), reload (`⟳`), and home (`⌂`) are always icon-only.
  History/help use full labels at 80+ columns and icons below; the
  config gear (`⚙`) is always an icon. The full History label underlines its
  `H` to advertise the keyboard shortcut. Below 24 columns hide
  back/forward/reload so the URL field stays usable; home remains visible down
  to 12 columns; below 12 hide history; below 6 hide
  help and config. Every toolbar item is clickable via per-frame button hit
  regions including the row coordinate.
- **Keys (vim-first):** Vim-style bindings are the preferred convention and
  take priority over browser-style alternatives. `f` follows links; `Tab` and
  `Enter` remain fallbacks.
  `j/k` line scroll, `Space/b`
  page scroll, `Ctrl-D/U` half page, `g/G` top/bottom, `Tab/Shift-Tab`
  focusable elements (links and controls; `f` hints are the quick path),
  `Enter` follow, `Ctrl-O/[` back, `Ctrl-I/]` forward (`Ctrl-I` arrives only
  with kitty key disambiguation; legacy terminals send it as Tab, so `[`/`]`
  are the portable pair; arrow keys do not navigate), `r` reload, `R` toggle
  reader mode, `D` toggle dark mode, `J` toggle JavaScript and reload,
  `i` cycle graphics modes, `+`/`-` zoom the page in/out (25% to 200%, `0`
  resets to 100%), `Z` opens the zoom window (also via the URL row's clickable
  zoom badge), `X` open the page view options window (a switch
  list navigated with Up/Down or `j`/`k` and toggled with Space/Enter, closing
  on Esc, `q`, `Ctrl-C` or `X`; the switches are Fixed elements, Sticky
  elements (off keeps sticky boxes in normal flow so they scroll with the page)
  and Background images (hiding the bitmaps but keeping neutral placeholders),
  and the list is built from one `viewOption` registry so later view toggles
  can be added), `o`/`Ctrl-L` URL prompt (or `:` for a Vim-style command prompt where
  `q` or `:q` plus Enter quits like `q`), `/` find in page,
  `n`/`N` next/previous find match, `f` link hints, `F` link hints with a
  destination confirmation, `u` show URL, `E` open the full warning or error
  text when the footer had to truncate it (the footer status line is also
  clickable, and the `E` in `Error:` is underlined to advertise the key),
  `d` open the curl download popup for the current PDF page (only in the PDF
  viewer; elsewhere `d` does nothing),
  `y` copy selected page text, the current URL when nothing is selected,
  the download popup's curl command, or the whole active console section,
  and clear the page highlight, `Y` copy the whole rendered page as plain
  text (the `-dump` rows without colors or browser chrome, capped by the
  clipboard limit) for pasting into a bug report. `Ctrl-Y` copies only the
  page rows visible at the current scroll with the same metadata line, so a
  report can show what is actually on screen. The `#` metadata line carries
  `url`, `build`, `term` (columns×rows), `page` (layout columns×viewport rows),
  `rows` (document height), `scroll` (top visible document row), `zoom`,
  `dark`, `reader`, `js`, `graphics`, `cell` (pixels), `layout_truncated`,
  `copy_truncated` and `lines` (copied page rows),
  `H` session history, `?` help page, `!` JS console, `q`/`Ctrl-C` ask for
  `y`/Enter confirmation before quitting, while `:q` plus Enter exits
  immediately. The quit confirmation takes priority over a hovered link in the
  footer and keeps its warning color. Do
  not move `g`/`G` or the `o` prompt to browser-style keys. `Esc` clears
  focus/selection and shows `q Quit / ? Help` in the footer; the same hint
  appears after closing the URL prompt, an input field or the JS console.
  Clicking the URL field keeps the prompt open; a press anywhere else closes
  it the same way Escape does. Inside the prompt, a click places the caret.
  Shift+drag is left to the terminal for native text selection.
- **Terminal input:** legacy sequences and Alt prefixes decode as before. In
  kitty, WezTerm, Ghostty and foot (but not inside tmux/screen/zellij), MOB3
  pushes kitty keyboard flags `1|2|16` (`CSI > 19 u`) and pops them on exit.
  `CSI u` keys map to the same symbolic tokens, key releases are ignored, and
  the associated-text field handles shifted symbols and AltGr layouts.
  Functional keys that keep legacy encodings (arrows, Home/End, Delete, …) may
  carry a kitty event suffix (`CSI 1;5:3A`); event 3 is dropped as a release and
  repeat (event 2) behaves like a press, so a release cannot double-move the
  cursor or page.
- **Find:** `/` opens an incremental, case-insensitive find bar in the footer.
  Every match in the rendered page is highlighted; the current match uses a
  stronger color. `Enter` confirms the query and closes the bar, after which
  `n`/`N` navigate matches. The footer keeps showing the match count and query
  while scrolling with the keyboard, paging keys or the wheel; only `Esc`
  clears the search and that footer line. While the bar is open, `Down` moves to the next
  match and `Up` to the previous one, with wrap-around. `Esc` also closes the
  bar but keeps highlights; a later `Esc` clears them. The same find bar
  searches the console's HTML tab (`/` while that tab is active), including
  scrolling to and highlighting matching lines. Queries may contain spaces:
  whitespace runs collapse to one space, and phrases match across word
  fragments and wrapped lines, with every rectangle of a multi-line match
  highlighted. It works on every rendered page, including `about:help`.
  When the current match lies inside a link, the footer
  appends `(f → <resolved URL>)` (or `(f follows this link)` for a bare or empty
  href) and `f` follows that link directly instead of opening link hints;
  clearing the search restores normal `f` hint mode. The
  query line takes the same Ctrl/Alt word motion and
  Alt/Ctrl+Backspace, Ctrl-W and Alt-D word deletion as the URL prompt.
- **Link hints:** `f` labels every visible link and form control by
  picking letters from the target's own text and drawing them with a yellow
  background and underline; the page text itself is unchanged. Choosing an
  editable field focuses it for typing; checkboxes and radios toggle or select,
  a single select opens its option popup, multi-select options toggle, buttons
  activate, range inputs focus for arrow adjustment, and file inputs show the
  upload notice. The chosen control stays focused, so `Space`/`Enter` and the
  arrow or `h`/`j`/`k`/`l` keys keep working on it. Up to 26 targets
  use the first letter not already used by an earlier link (case-insensitive),
  so `Documentation` starts with `D` and a later `Dog` uses `o` if `d` is
  taken. On link-heavy pages a hint whose first letter is unique still shows
  only that one letter; hints that share a first letter use two adjacent
  letters from the text, never letters with a gap between them.
  Typing a letter narrows the labels, clears the already typed letters from
  the highlights and follows the link when only one matches; the key is
  case-insensitive. Unknown letters are ignored, `Esc`
  cancels, and scrolling (wheel or scrollbar) dismisses the hints. Timer-driven
  page rebuilds recompute the hints and keep the session, typed prefix and any
  pending F confirmation instead of dismissing them. Only targets intersecting
  the viewport are
  labelled; a target with no text
  letters (such as an image link or an empty field) gets free label letters drawn inside its box.
  Controls and links inside fixed bars are labelled too, at their
  viewport-anchored position.
  While hints are shown, images are drawn as half-block art
  instead of native inline graphics, so the terminal image protocol cannot
  cover the hint letters.
- **Dark mode:** `D` or the footer `Dark:on`/`Dark:off` label toggles dark
  mode. Pages are styled with `prefers-color-scheme: dark`, so sites can serve
  their own dark CSS; `matchMedia` reflects the same scheme. When the resolved
  page background is already dark, the author palette is kept as-is. Otherwise
  a post-layout remap makes light backgrounds dark, dark text light and
  preserves hues for links, headings and accents; images are left unchanged.
  The preference persists across navigation and reload, and `-dark` starts with
  it enabled, including in `-dump` mode. It also applies on top of reader
  mode.
- **JavaScript toggle:** `J` or the footer `JavaScript:on`/`JavaScript:off`
  label switches scripting off/on for the rest of the session and reloads the
  current URL, so the page's no-script fallback is applied immediately. The
  reload reuses the last method and body. The back/forward page cache is
  cleared because cached pages carry the previous mode's DOM and runtime.
  The footer reports `JavaScript off — press J to re-enable` or
  `JavaScript on`; `-no-js` and `about:config` set the starting state and
  nothing is persisted. The JS console remains available through `!` and the
  `>_` icon, showing that scripting is disabled when a command is entered.
- **Reader mode:** `R` or the footer `Reader:on`/`Reader:off` label toggles an
  extracted article view. Extraction runs on the live DOM, prefers
  `<article>`/`<main>`/`role=main`, scores other prose containers, drops
  nav/sidebar/footer/forms/scripts and common junk-class subtrees, strips page
  CSS, and renders a filtered clone in a centered `max-width:600px` column.
  Prose paragraphs are justified, but headings stay left-aligned so a wrapped
  headline is not stretched word by word, and headings use the body text color
  without link underlines even when the source wraps them in an `<a>`. Images
  are removed entirely,
  including `alt` text and `[image: ...]` labels; only text, headings, links,
  lists, tables, quotes and code remain. The
  original page tree, URL and history are untouched. Reader mode stays enabled
  across navigation and reload until `R` or the icon is used again: each new
  page is extracted automatically, and a page with no usable article is shown
  normally with `Reader mode unavailable` while the preference remains on.
  PDF pages behave the same way: they keep their full extracted document
  instead of being reduced to an article. The built-in history page is never
  extracted and is shown normally with `Reader mode unavailable`, because its
  links are its content; pressing `R` there reports
  `Reader mode is unavailable on the history page`.
  Toggling off restores the normal page and its previous scroll position.
  `-reader` starts with the preference on, including `-dump` mode, which prints
  the extracted view when one is available. `about:config` is unavailable while
  reader mode is on: opening it keeps the current page and shows the warning
  `Config is not available in reader mode — press R to turn it off`. Reader
  mode is a snapshot: live JS mutations are not reflected while it is active,
  and interactive controls are not available. Reader link clicks do not run the
  page's JavaScript click handlers, because the extracted clone is detached
  from the live document; document-level handlers therefore cannot cancel
  reader navigation. Same-document links with no
  fragment or a missing target (common on modal triggers) do not navigate or
  jump to the top; the footer shows `Reader mode: link is not available`
  instead.
- **Start page:** `about:home` and `about:start` render the built-in
  `startPageHTML` (no runtime fetch), with its content wrapped in `<article>`
  and the reader-mode stylesheet (`readerRules`) applied exactly. It therefore
  gets the same centered `max-width:600px` reading column and typography as an
  extracted article. Its text is justified, and it includes a native GET form
  with a full-width `q` search/URL box beside a `Go` button; submitting it
  resolves the text exactly like the `o` prompt (URL first, then the `-search`
  fallback) and works with JavaScript disabled.
  `about:history` uses the centered `max-width:900px` column shared with
  `about:help`.
- **Built-in page styling:** the generated help, config, history,
  blocked (“Site blocked”) and error pages share `appPageCSS`/`appPageHTML`:
  one centered 900px reading column, a 32px `h1`, and the same table, muted,
  warning and `pre` styles. Page-specific rules are appended (config form
  controls, the blocked page's red heading and blue retry box), and
  page-derived text is escaped by the shell. The start page keeps
  its reader-mode column and the PDF viewer keeps its content-sized page boxes.
- **Help page:** `?` and the toolbar `?` navigate to the built-in
  `about:help` HTML page. Help lives as a normal rendered document (tables for
  command-line flags, keys, mouse and forms) and shows the current build
  version from `Version`, not as a one-line footer status,
  so every shortcut including `!` is visible and selectable. The flag table
  must list every option accepted by `Run`. The page includes try-it links for
  alert/confirm/prompt, a `download` example that opens the curl popup, and a
  demo form with text input, checkbox, radio, a three-option select, a
  multi-select, textarea, a file-upload
  control (shown but unsupported) and a submit button; inline handlers work
  even on pages with no `<script>` elements.
- **Focus:** focused links use a yellow background highlight on text and a
  yellow border on images; the focused URL is shown in the footer.
- **Mouse:** enable click reporting with `\x1b[?1000h`, all-motion tracking
  with `\x1b[?1003h` (needed for link hover), and SGR extended coordinates
  with `\x1b[?1006h`; disable all three on exit. SGR (`\x1b[<b;x;yM/m`) and
  legacy X10 (`\x1b[M` + 3 bytes) events, including motion reports, become
  `mouse:` input tokens. Hovering a link shows its resolved target in the
  footer and clears when the pointer leaves it. Hover also marks the element
  under the pointer and its ancestors as `:hover` and fires
  mouseover/mouseenter/mouseout/mouseleave, so CSS and JavaScript hover menus
  open. Pointer hit testing uses text fragments and laid-out element boxes, so
  padded menu items stay hovered while the pointer moves between them. Hover
  style passes are coalesced with a short delay, so fast pointer motion and
  wheel scrolling do not rebuild the page per cell; the menu opens once the
  pointer settles. Leaving the page area clears the hover chain.
  Left press starts a drag
  selection in document coordinates; dragging updates and auto-scrolls it,
  and release keeps the highlight. The highlight tints only rendered text
  fragments, not empty margins or unrelated columns on the same row. A press
  and release on the same cell is a
  click: it follows a link using the same rectangles as keyboard focus, or
  clears focus; a download link opens the curl popup, except a PDF download,
  which is fetched and opens in the viewer. While the URL
  prompt is open, a click on its field keeps it
  open and a click anywhere else closes it; clicking inside it positions the
  caret. Shift+mouse reports are ignored and suppress the next redraw so the
  terminal can keep a native selection over the URL. A left click on the URL
  bar opens the URL prompt, toolbar
  buttons act on click, right-clicking a link copies its resolved URL (OSC 52,
  status `Copied URL`, like `y`, which replaces the hovered-link line and holds
  until the pointer moves or the page scrolls) and does nothing elsewhere, the
  mouse back button goes back, the mouse forward button goes forward, clicking
  the footer
  `Dark:on`/`Dark:off` or `Reader:on`/`Reader:off` labels toggles those modes,
  clicking the footer
  `JavaScript:on`/`JavaScript:off` label toggles scripting for the session and
  reloads the page, clicking the footer status line opens the full warning or
  error text (also `E`), clicking the footer
  `>_` icon toggles the JS console (and closes it while open), and the wheel
  scrolls three lines. Long pages show a scrollbar
  on the right edge: click or drag it to jump through the document. Any button
  release ends a scrollbar or selection drag, even when the release report also
  carries the motion bit or lands on the toolbar, footer or outside the page,
  so a released drag can never keep following the pointer. Interactive
  layout is one column narrower than the terminal so page text, images and
  overlays never paint underneath the scrollbar column; overflowing fragments
  that still reach it are blanked before the scrollbar is drawn. `Esc`
  clears the selection. `y` copies the selected page text through the terminal
  OSC 52 clipboard sequence and then clears the highlight (subject to
  terminal/multiplexer support).
- **Forms:** clicking a text/search/email/URL/tel/number/password input or a
  textarea focuses it and shows a block caret. Left/Right move by one
  character, Home/End by line, and in a textarea Up/Down move between logical
  lines (keeping the column); typing and Backspace edit at the caret, and a
  long focused input scrolls around the caret. Ctrl-B/F move by character;
  Alt-B/F or Ctrl/Alt+Left/Right move by word; Alt/Ctrl+Backspace, Ctrl-W and
  Alt-D delete words; Delete removes forward. Ctrl-A selects the whole value
  and paints it with a selection background: typing, Backspace or Delete
  replaces the value, and any caret move or leaving the field cancels the
  selection. Ctrl-U clears, Ctrl-K deletes to the end, and Esc finishes;
  Enter submits from other fields but inserts a newline at the caret in a
  textarea. Textareas render as
  at least two rows (an explicit `rows` value up to 12 is honoured) and wrap
  their value; only the visible rows are produced, so typing into a huge value
  stays cheap. A text-like control with an explicit CSS width pads its widget
  to that width; in a flex row it fills the width allocated by flex (the home
  search box is `flex:1` beside the `Go` button). `Tab`/`Shift-Tab` move
  through links and form controls in document order, wrapping at the ends;
  disabled and hidden elements are skipped, read-only fields are focusable,
  and a radio group takes one stop with arrows moving within it. `Space` or
  `Enter` activates the focused control: checkboxes and radios toggle/select,
  buttons click, links navigate, and single selects open their popup. Arrows
  change a focused single select or range slider; in a multi-select they move
  the active option and `Space` toggles it. `h`/`j`/`k`/`l` act as left/down/up/right
  alternatives on focused radios, selects and range sliders, while arrows keep
  working.
  Required controls,
  `pattern`, `maxlength`, `readonly` and
  `disabled` are enforced; disabled controls are not submitted. Date, time,
  datetime-local, month and week inputs are edited as typed text with format
  validation, range inputs are sliders with clickable `-`/`+` ends and arrow
  adjustment, and color inputs accept
  `#rrggbb` and paint the field with a live swatch. Clicking a file
  input opens a modal explaining that uploads are unsupported. Checkboxes
  render as `☑`/`☐` and radios as `◉`/`○` (one cell each); clicking one toggles
  it (radios uncheck same-name siblings); clicking a
  select opens a centered popup listing every option, navigated with Up/Down
  and chosen with Enter or a click, with Esc/backdrop/`×` closing it without a
  change. `<select multiple>` renders as an always-visible list box with every
  option on its own row; clicking an option toggles it. Submit
  buttons run their `click` handler and then submit their form on release
  unless the handler prevents default; `type=button`/`type=reset` buttons and
  buttons outside a form only run their click handlers. Clicking a `<label for>`
  or a wrapping label activates its associated control. GET forms build a query
  from named fields
  (including hidden inputs, checked checkboxes/radios and selects); POST forms
  send `application/x-www-form-urlencoded` bodies. Both
  navigate to the resolved action, and reload reuses the last method/body.
  Focusing/blurring fires `change`, editing fires `input`, toggling fires
  `input`/`change`, and submitting fires `submit` before navigation.
  `form.submit()` queues the same submission path without re-running
  validation or the `submit` event. JS exposes
  `value`, `checked` and `form` on elements. Editable fields use a prominent
  light-blue background with bold text; the focused field switches to a solid
  blue background with white text, and its placeholder/hint disappears while
  focused. A fixed-width or flex-stretched control keeps its box while typing,
  so value changes patch only that control's fragments; an auto-width control
  relayouts when its box grows or shrinks.
- **Footer:** shows the hovered link target, status or the PDF page line on
  the left and, on the
  right, current resident memory (`MEM 18MB`),
  resolved `Graphics:<mode>` notices, `JavaScript:on`/`JavaScript:off` (which
  becomes red bold `JavaScript:Error` when a script fails), clickable
  `Graphics:<mode>`, `Dark:on`/`Dark:off` and `Reader:on`/`Reader:off` labels
  before the memory
  readout, and a clickable `>_` console icon pinned to the far right edge. The
  scroll percentage is not shown in the footer; the `scroll` automation tool
  still reports it.
  Clicking the `JavaScript:on`/`JavaScript:off` label toggles scripting for the
  session and reloads the current page, exactly like `J`; the console is
  opened by the `>_` icon or `!`.
  Clicking the `Graphics:<mode>` label cycles graphics modes exactly like `i`,
  and the `i` in that label is underlined to advertise the shortcut.
  Right-side items collapse from left to right as the terminal narrows:
  `Graphics` goes first, then `JavaScript`, `Dark:…`, `Reader:…` and finally
  memory. The right-side budget depends only on
  terminal width (it may use at most half the footer), so at least half is
  always kept for the left status/URL and a long message never collapses the
  right state.
  At least three blank cells
  separate the left status/link text from the right-side state. The first letter
  of the JavaScript, Dark
  and Reader labels is underlined to suggest the `J`/`D`/`R` shortcuts, even
  when the JavaScript label is in its red error state. The `E` in an `Error:`
  status is underlined to suggest the `E` details shortcut. Each
  label is blue and bold
  while its mode is on (JavaScript counts as on while scripts are enabled); the
  console icon toggles the JS console and stays
  visible and clickable inside the console view. Status text is amber and bold for warnings (module script failures, layout truncation, blocked pages/HTTP
  errors, unsupported protocols/JS links), red and
  bold for errors (`Error:`, invalid URL/form action, copy failure), and green
  for successful copy messages. The JavaScript error state clears when the JS
  console opens or a new page loads. The readout refreshes
  every two seconds.
  Memory is `runtime.MemStats.Sys`, Go's total OS memory; no `/proc` file is
  read. The readout is not repainted
  while the JS console is open, so terminal selection is not disturbed.
- **History:** the session history stores each URL with its last scroll
  position. Fragment-only `#` links push a history entry and scroll in place;
  `h`/Back and Forward return to the stored scroll position, using an in-place
  update when only the fragment changes. Visiting a different URL starts at the
  top unless it is a history entry being restored. Applied documents are kept in
  an in-memory back/forward cache, with their DOM, styles, decoded images and
  JavaScript runtime; Back and Forward restore a cached document without
  fetching, parsing or re-running scripts. Its timers pause while cached and
  resume where they left off. The cache is memory-aware: each suspended page is
  weighed once (raw source, script text, POST body, DOM with attributes and
  text, decoded image bytes, plus a conservative allowance for a live JS
  runtime and its node wrappers), pages evicted least-recently-used first until
  the total fits the derived history budget (12% of `-mem-limit`/`GOMEMLIMIT`,
  or 128 MB when unlimited), and a page heavier than half the budget is never
  cached at all, so
  one heavy document cannot pin the allowance. The newest entry is kept when
  the budget is tight, `maxCachedPages` (10) stays as a second guard, and the
  budget follows live changes to `-mem-limit`. When an evicted page held at
  least a quarter of the budget, one `debug.FreeOSMemory()` is requested and
  run between frames, returning its memory to the OS at once.
  `about:` pages and downloads are never cached.
  `location.replace` and `location.reload` update the active history entry
  instead of pushing a new one. HTTP 3xx redirects are followed inside one
  load, so only the final URL is recorded. Client redirects that happen while
  the page is still being applied are coalesced the way browsers do it: the
  redirecting document is recorded first so an entry exists, then a meta
  refresh or a load-time scripted `location.href`/`assign` replaces it, so
  Back skips the redirect page instead of landing on it and being forwarded
  again. A navigation queued after the page settled (a click or timer handler)
  still pushes a new entry. Consecutive scripted and meta-refresh redirects
  stop after 20 hops with `Redirect loop stopped` instead of loading and
  growing the session forever.
  Nothing is persisted.
- **Loading:** navigation runs asynchronously so the UI stays responsive.
  `Esc` cancels the in-flight page, stylesheet, script and image requests and
  shows “Stopped loading”. Late results from canceled navigations are ignored
  by sequence number. Loading another URL cancels the previous load.
  In interactive mode page documents are applied and painted before their
  images: `Loader.deferImages` skips image downloads during the page load, the
  text (with alt labels) is shown immediately, and a background batch fetches
  and decodes the images without touching the DOM. Pending images are ordered
  viewport-first (visible, then within one screen, then by distance with DOM
  order as the tie-breaker) and six workers pull from that queue, taking the
  next image as soon as one finishes. Decoded results stream back in batches of
  up to 16; the event loop applies each batch and redraws, so images appear
  progressively. A batch whose images all start below the viewport does not
  reflow, repaint or blink the visible frame: the decoded images are stored on
  their nodes, and the layout is deferred until a frame's viewport reaches
  their topmost row or reaches the end of the document, where later content may
  be waiting. A batch that touches the viewport rebuilds at once; half-block
  text modes repaint the affected rows through the normal line diff, while
  native terminal placements or kitty scaled text force the same full repaint
  as scrolling so stale graphics cannot survive; the front end triggers it
  whenever a frame's scaled-run set differs from the previous frame, because
  the cells under a run are skipped by the line diff. Offscreen images are still
  queued after the visible ones and load eventually. While a batch is pending the URL-row progress bar stays
  visible at 90% and advances toward completion as images finish, and an idle
  footer shows `Loading images n/total` until the batch is done (`Esc` completes
  the bar and stops the batch). Navigation cancels the batch and stale results
  are discarded by page sequence. Timer-driven rebuilds queue
  another background batch instead of loading images synchronously, so scripts
  that dirty the DOM cannot block input. `Esc` cancels a running image batch
  (status `Stopped loading images`) and pauses further background loads for the
  current page; the next navigation or graphics change resumes loading. Dump
  and automation loads stay synchronous.
- **Refused schemes:** `javascript:`, `mailto:` and `tel:` links show a status
  message. `data:` is not accepted as a top-level document (`data: pages are not
  supported`); it stays available for inline images, stylesheets, scripts and
  modules.
- **URL input rules:** opening the URL prompt (`o`, `Ctrl-L`, `:` or clicking
  the URL field) prefills the current URL and selects it all, so typing replaces it
  like a normal browser address bar; Backspace clears the selection first. A
  prompt opened with `:` quits immediately when `q` or `:q` is confirmed with
  Enter, and the
  prompt keeps a session-only history of submitted URLs and search terms:
  Up/Down and Ctrl-P/Ctrl-N recall earlier entries, Down past the newest
  restores the draft, and consecutive duplicates are dropped. History is never
  persisted. The prompt
  has a real cursor: Left/Right, Home/End move it, Backspace/Delete remove
  around it, Ctrl-A selects all, Ctrl-K deletes to the end, Ctrl-U clears and
  Ctrl-W deletes the previous word. Emacs/readline chords also work: Ctrl-B/F
  move by character; Alt-B/F (or Ctrl/Alt+Left/Right) move by URL word;
  Alt-Backspace, Ctrl-Backspace and Ctrl-W delete the previous word/segment;
  Alt-D, Alt-Delete and Ctrl-Delete delete the next word plus its following
  separators. A mouse click places the caret. Shift+drag
  is not app-managed selection: Shift+mouse reports that do arrive are ignored
  and no redraw is written, so terminals that handle Shift locally can select
  and copy the URL text normally.
  The caret is an overlay block cursor, not
  an inserted cell, so moving it never shifts the URL text. Bare host-like
  input becomes `https://`; `host:port` becomes `http://`; local files are not
  supported, so `file:` URLs are rejected and bare paths are never read from
  disk; relative references against a base only for `/`, `./`, `../`, `#`, `?`.
- **Search fallback:** input that contains whitespace or has no dot (for
  example `golang tutorial` or `golang`) is treated as a search query.
  `-search` takes a URL template whose `%s` is replaced with the URL-escaped
  query; the default is `https://html.duckduckgo.com/html/?q=%s` (the
  server-rendered endpoint; the main DuckDuckGo page is JS-only and renders
  blank without scripts), and an empty value disables the fallback. Valid URLs,
  `example.com`, `host:port`, `about:` and base-relative references still
  resolve as URLs. The fallback applies only to typed
  navigation and the start page's `q` search box, not link hrefs or ordinary
  form actions. Local files are not supported.
- **Non-HTML content:** a “cannot display” page is shown (plain text is shown in
  a `<pre>`); binaries are never rendered as HTML.
- **PDF pages:** a PDF response opens in the built-in text viewer instead of the
  download popup. Every page is extracted into one continuous document. The
  viewport holds only the page text and a separator after each page: a
  right-aligned, muted `Page n` caption above a thin rule of underscores. The
  page's CSS border is not used, so the separator reads as a drawn line rather
  than a filled cell row, and there is no in-page header or header-style page
  label. The reading column is centered at the width of the widest line, and
  every page is padded to the same number of text rows (`pdfPageBox`), so the
  document scrolls like a printed book instead of jumping between differently
  sized pages. Vertical gaps wider than the page's own leading become blank
  lines, which restores the spacing after headings and between blocks that the
  PDF layout implies. Two-column prose is read column by column: a vertical
  gutter that most lines respect (wide, dense text on both sides, no internal
  table gaps) splits the page into left and right sequences, while full-width
  lines such as titles stay in flow and multi-column tables keep their printed
  row order. Runs at least 1.25× the document's body font size are
  emitted as 32px headline spans, so the existing large-type path applies: two
  rows, and kitty-scaled glyphs when graphics and text scaling are enabled.
  Text keeps its column alignment inside `pre-wrap` blocks so tables and
  multi-column pages stay readable; bold and italic come from the font name and
  flags. The footer's left status becomes
  `Page n/N (d -> Download name)`, where `n` is the page under the viewport as
  the user scrolls; the `(d -> Download name)` part is a clickable label and
  `d` does the same, both opening the curl download popup. Outside the PDF
  viewer `d` does nothing. On narrow terminals
  the file name inside the hint is shortened first, then the hint becomes
  `(d)`, and finally only `Page n/N` remains. `-pdf-page auto|all|N` selects the
  page for `-dump`/`-png`: a dump keeps every page, `-png` renders page 1 by
  default, and `-pdf-page=all` restores the whole-document image. Both caps
  report truncation on stderr (2,000 PNG rows, 20,000 dump rows).
  Find, selection, `-dump`, `-png` and the automation snapshot treat the result
  as an ordinary page; reader mode keeps the full extracted document and
  reports `Reader mode unavailable` while the preference stays on. Notes
  replace empty output when the document is encrypted or has no readable
  pages, a truncation note is kept above the pages, and a document with no
  extractable text at all shows a per-page placeholder, while figure-only pages
  in a text document stay blank. `-no-pdf` (also in
  `about:config`) turns the viewer off for the session and restores the old
  “Cannot display” page and popup. Nothing is written to disk.
- **Downloads:** MOB³ never saves files. A link with a `download` attribute
  opens the modal download popup without making a request, because the
  attribute means "save" in a browser. The exception is a PDF: when the URL
  path or the attribute filename ends in `.pdf`, the link is fetched so the
  text viewer can show it, and the attribute value is kept as the fallback
  filename if the response turns out not to be a PDF. The popup presents a
  ready-to-copy `curl -L -o 'name' -- 'url'` command shown as plain text, using
  the link's `download` attribute value when non-empty and otherwise the URL's
  last path segment as the filename; a filename starting with `-` is prefixed
  with `./`. `file:` download targets are rejected, and `--` keeps a
  `-`-prefixed target from reading as a curl option. `y` copies the command with
  OSC 52 and
  closes the popup, while `Esc`/`Enter`/`q` close it without copying. Nothing is
  written to disk; the user runs the command in their own terminal.
  `Content-Disposition` is not inspected, so attachment HTML pages still render
  normally.
- **JavaScript scope:** classic inline and external scripts run in document
  order after load and before layout. `<script type="module">` scripts are
  linked and evaluated in document order through sobek's module records:
  static `import`/`export`, relative and absolute specifiers, dynamic `import()`
  with a real namespace, and `import.meta.url`. A document `<script
  type="importmap">` resolves bare package specifiers (exact entries and
  trailing-slash prefixes; `scopes` are ignored); a specifier the map does not
  cover fails with `bare module specifier ... (no import map entry)` in the
  console instead of becoming a same-origin 404 request. Module fetches use the
  same loader, MIME rule, request log and session cache as classic scripts and
  share the module graph cache, so a dependency imported by several entry
  modules is evaluated once. In interactive mode a dynamic `import()` issued
  after the page settled loads its graph in the background and the event loop
  completes it, so a slow chunk does not block input, drawing or the image
  batch; during page load and in dump/automation runs imports stay synchronous.
  `nomodule` scripts are skipped. Custom elements
  are upgraded: `customElements.define` registers the class, and
  `document.createElement` or the first access to a parsed element of that tag
  runs the class constructor with the element node bound, so field initializers
  and `attachShadow` in the constructor see a real element (lifecycle callbacks
  are not fired and shadow content is not rendered). `template.content` is a
  real inert fragment, so markup built there clones into the page instead of
  rendering in place. DOM subset: `getElementById`,
  `querySelector(All)`, `getElementsByTagName`, `createElement`,
  `createTextNode`, attributes, `textContent`/`innerText`/`innerHTML`,
  `classList`, `style` (common properties plus `setProperty`/`cssText`),
  `appendChild`/`removeChild`/`replaceChildren`, `children`/`parentNode`, and
  `document.title`/`URL`. Element wrappers use constructor prototypes, so
  `instanceof HTMLElement`, `instanceof HTMLScriptElement`, `instanceof Node`
  and similar checks work; document, text, SVG, `CharacterData`,
  `DocumentType` and common HTML
  element constructors are exposed globally and on `window`. `<dialog>`
  elements expose `open`, `returnValue`, `show()`, `showModal()` and `close()`;
  only dialogs with the `open` attribute render, and `close()` fires `close`.
- **Events:** `Event`, `CustomEvent`, `UIEvent`, `MouseEvent`, `KeyboardEvent`,
  `FocusEvent`, `InputEvent`, `PointerEvent`, `ErrorEvent`, `MessageEvent` and
  `ProgressEvent` constructors exist; `document.createEvent` returns the
  matching type with `initEvent`/`initCustomEvent`/`initMouseEvent`, and
  `dispatchEvent` (with bubbling and `preventDefault`) works on elements,
  `document` and `window`. `stopPropagation` ends bubbling to ancestors but
  still runs the element's remaining listeners; only
  `stopImmediatePropagation` skips them, as in the DOM. `element.click()`
  dispatches a click, and clicking an `<input type=file>` from script shows the
  unsupported-upload notice (pages open the picker through a hidden input).
  `NodeFilter`,
  `Node.*` constants and `performance.timing`/`navigation` are present.
  Elements expose `baseURI`, `clientTop`/`clientLeft`, `nodeValue` (null),
  `isSameNode`/`isEqualNode`, `hasAttributes`, attribute-node methods,
  `checkVisibility`, element scroll no-ops, pointer-capture no-ops,
  `requestFullscreen`, and `shadowRoot` (null). `document.baseURI` follows the
  document's `<base>` element (the same URL element baseURI uses, without the
  page fragment), so script-built API URLs such as
  `new URL("api/i18n", document.baseURI)` stay under the mount point even when
  the page was opened at `#/`. `DOMParser` parses HTML into a
  detached document; `XMLSerializer` uses the HTML serializer; `DataTransfer`
  is in-memory; `document.startViewTransition` runs its callback immediately.
  `MessageChannel` delivers `postMessage` through the timer queue;
  `MutationObserver` is a no-op observer. `IntersectionObserver` and
  `ResizeObserver` report one synthetic entry per observed target via the timer
  queue (intersecting/current layout rect); `PerformanceObserver` is inert.
  `NodeList`, `HTMLCollection`, `EventTarget`, `DOMStringMap`, `CSS`,
  `ShadowRoot`, `customElements`, `Path2D`, `ReadableStream` (in-memory reader),
  `Worker` (inert) and Web Animations (`Element.animate` returns a finished
  animation) exist so feature detection succeeds. Iframes expose an isolated,
  inert `contentDocument`/`contentWindow`; writing into it never touches the
  page. Canvas `getContext("2d")` returns a
  no-op context (no pixel buffer); WebGL returns `null`. `reportError()`
  reports a value to the JS console without throwing.
- **JS globals:** `window` exposes `document`, `navigator` (matching User-Agent,
  language, platform), `location`, `performance`, `innerWidth`/`innerHeight`,
  `screen`, `self`/`top`/`parent`, `outerWidth`/`outerHeight`, `scrollX`/
  `scrollY`/`pageXOffset`/`pageYOffset`, `origin`, `isSecureContext`,
  `frames`/`length`, timers, `requestAnimationFrame`, events, `scroll`/
  `scrollTo`/`scrollBy`, `getComputedStyle` and `console`. `navigator`
  includes `sendBeacon` (accepted, not delivered). `Image`/`Audio` construct DOM elements. `document` exposes
  `location`, visibility state, `defaultView`, `domain`,
  `document.implementation`
  (`hasFeature`/`createHTMLDocument`), and image elements expose
  `src`/`alt`/`width`/
  `height`/`complete`/`natural*`. `URL`/`URLSearchParams`,
  `document.currentScript`/`baseURI`, `characterSet`/`charset`,
  `contentType`, `referrer`, `lastModified`, `adoptedStyleSheets`,
  `embeds`/`plugins`, `fullscreenEnabled`, `createComment`,
  `elementFromPoint`/`elementsFromPoint`, `document.write`/`writeln`,
  `document.cookie`, and in-memory `localStorage`/`sessionStorage` are
  available. Storage supports `getItem`/`setItem`/`removeItem`/`clear`/`key`/
  `length` plus named property reads, writes and `delete` against the same
  map. `document.cookie` reads and writes the same in-memory session jar
  as HTTP requests, honors Domain/Path/Expires/Max-Age/Secure and hides
  HttpOnly cookies; it also cannot replace or delete a server-set HttpOnly
  cookie. `navigator.cookieEnabled` is true. Cookies are not
  persisted. `XMLHttpRequest` supports
  basic HTTP methods, headers, `responseText`/`response` (text/
  json/arraybuffer), status and callbacks. Same-origin requests always work;
  cross-origin requests are limited to simple CORS requests (GET/HEAD/POST
  with safelisted headers) whose response carries `Access-Control-Allow-Origin`
  for the page origin or `*`. Preflight and cross-origin credentials are not
  supported, and cross-origin cookies are not sent. It executes synchronously
  even when `async=true` and
  does not implement streaming, upload progress or timeouts. `fetch` is
  available with `Headers`, `Request` and `Response`; it follows the same
  CORS rules, resolves a promise, and supports `text`, `json`,
  `arrayBuffer` and `blob` bodies. It is likewise synchronous under the hood
  and has no streaming or upload progress; a basic `AbortController` only
  rejects a fetch that has not started. `Intl` provides
  an English-only practical subset: `NumberFormat`, `DateTimeFormat`,
  `Collator`, `PluralRules`, `RelativeTimeFormat`, `ListFormat`, `Segmenter`,
  `getCanonicalLocales` and `supportedValuesOf`, without full CLDR locale
  data. `Segmenter` supports grapheme (code points), word (letter/digit/
  underscore runs) and sentence (punctuation plus whitespace) granularities,
  with `segment()` and `containing()`. `<noscript>` is removed before styles
  and scripts are collected when JS is enabled, so no-JS styles/scripts cannot
  affect the JS page.
  `console.*`, uncaught errors and unhandled promise rejections append to a
  bounded JS log. Module scripts execute, so globals created by an entry module
  appear; an app whose entry module is only reachable through an import map or
  a bare specifier still leaves them undefined.
- **Modals:** alert/confirm/prompt, download and upload-notice modals share a
  centered box over a dimmed page backdrop. The body is fully opaque white
  (never blended with the page), with page glyphs and text attributes cleared
  beneath it, and the backdrop outside dims both the page background and its
  text (link underlines are dropped) so content does not leak into the modal
  edges. The body also has a blank spacer row below the title bar and
  above the hint, and a `×` in the top-right corner that closes the modal with
  the mouse. Clicking the dimmed backdrop also closes the modal. While a modal
  or select popup is open, native inline images are disabled in favour of
  half-block art so terminal image placements cannot paint over the modal;
  native images return when it closes. Kitty scaled text is likewise disabled
  while a modal is open (the layout is rebuilt without it), because scaled
  glyphs are terminal-side and would be composited over the modal. The
  front-end-owned view and zoom menus report their open state to the engine, so
  the page behind them is repainted with half-block images and normal-size text
  before the window is drawn.
- **JS dialogs:** `alert`, `confirm` and `prompt` open a centered modal and
  block script execution until answered. Alert closes on Enter/Space/Esc;
  confirm uses Enter/Y for yes and N/Esc for no; prompt supports the same text
  editing keys as the URL bar. In non-interactive/dump runs, alert returns,
  confirm returns false and prompt returns null. Modals use keyboard input;
  clicking the `×` or the dimmed backdrop closes them with the mouse
  (cancelling confirm/prompt).
- **JS console view:** `!` opens a plain-text console in the viewport. A
  one-row menu at the top has five tabs: JavaScript logs, Requests, Storage,
  HTML and Tools. The active tab is highlighted blue; only its section body is
  shown below the menu, and the logs tab is active by default. The JavaScript
  input row is part of the logs body, pinned to its bottom. Clicking a tab
  switches sections, and `Tab`/`Shift-Tab` or `h`/`l` cycle through the same
  five tabs.
  Mouse reporting stays enabled so tabs and the input row can be clicked, and
  the wheel scrolls the active section (plain terminal drag-selection needs the
  Shift override).
  `:` or `Enter` (or a click on the input row) focuses the input; typing edits a
  one-line expression and `Enter` evaluates it in the page runtime under the
  usual JS timeout/memory guards. The input row supports the same Ctrl/Alt
  word-motion and word-deletion keys as the URL prompt. It keeps its gray
  background while
  focused. Up/Down or Ctrl-P/Ctrl-N recall earlier inputs from the in-memory
  console history (consecutive duplicates are dropped, capped at 200 entries);
  Down past the newest entry restores the draft, and refocusing or clicking the
  input row does not reset the history position. The JS logs and Tools tabs
  keep separate histories. History is not reset by navigation.
  Input and results are appended to the log as
  `[input]` and `[result]`, the result appears above the input row, dirty DOM
  mutations rebuild the page, and `-no-js` logs that JavaScript is disabled.
  The Tools tab has its own input row at the bottom of its body and runs the
  read-only inspection subset against the live page: `help`, `form [selector]`,
  `links [selector]`, `requests [limit]`, `logs [limit]`, `source [selector]`,
  `snapshot` and `history`. `help` is shown the first time the tab opens;
  commands and results append to a scrollback that keeps the newest 500 lines
  and survives page loads; new output replaces the oldest lines, a single
  oversized result keeps its beginning with a truncation note, and
  `clear`/`cls` empties it. Long tool output wraps to the pane width with a
  hanging indent; copying keeps it unwrapped. The JS logs input accepts
  `clear`/`cls` too and empties the JS log (and its error state) instead of
  evaluating JavaScript. These commands must never start or wait for a
  navigation, because the interactive event loop owns load results;
  navigational and mutating tools stay in `-mcp`.
  `Esc` leaves the input row when it is focused and closes the console on the
  next press; `!` closes immediately, and `q` asks to confirm quitting when
  the input row is not focused (`q` is literal text while typing). With the
  input row inactive, `y` copies the whole active section to the clipboard:
  the JavaScript log, the raw page HTML (not the wrapped display lines), the
  request list, storage, or the Tools scrollback, so long output can be taken
  without dragging a terminal selection, and the console footer hint
  advertises `y: copy all` instead of the old Shift-drag note. While the input
  row is focused `y` is ordinary text. The footer
  keeps its clickable `>_` icon while the console is open; clicking it closes
  the console the same way `Esc` does (including the usual exit hint).
  The JS log and input draft reset when a new page is successfully applied,
  before that page's scripts run; failed loads keep the previous page's log.
  Requests list status, method, size, kind and URL (plain text, not clickable)
  for the page, CSS, scripts, images, `fetch` and XHR. Cache hits replace the
  size column with `cached`, and a pinned bottom line shows cache bytes used,
  total budget and entry count; the list is in memory
  only, resets on navigation and is capped at the latest 512 entries. A
  local-network or mixed-content request blocked by the guard shows as `ERR`
  with its reason; upgraded image requests show the HTTPS URL. Long request and
  storage lines wrap to the pane width with a hanging indent. Storage
  lists the current page's cookies, including HttpOnly cookies, one per line:
  `name=value`, `[HttpOnly]`/`[Secure]`/`[SameSite=…]` flags, the domain (a
  leading dot marks a domain cookie), path, and `session` or
  `expires YYYY-MM-DD`. The in-memory `localStorage`/`sessionStorage`
  key/value pairs follow; long values wrap with a hanging indent to the pane
  width, while `y` copies them unwrapped.
  HTML shows the raw decoded page source (pre-JavaScript DOM, wrapped to the
  console width and opening at the top) retained as `Page.RawHTML`, cached per
  page and width.
  Timers and timer-driven redraws pause while the console is open, so a
  terminal selection is not cleared by redraws. The full error text is kept
  there; the footer JavaScript label turns into red `JavaScript:Error` instead.
- **JavaScript events and timers:** `addEventListener`/`removeEventListener`
  on nodes, `document` and `window`; bubbling `click`; inline `onclick`
  attributes and the `onclick` property; `DOMContentLoaded` and `load`.
  Following a link by keyboard or mouse dispatches its `click` handlers first;
  a handler that returns false or calls `preventDefault()` suppresses
  navigation, so `href="#" onclick="…; return false"` actions work from `f`,
  Tab/Enter and clicks. If a handler throws during the click and the link has a
  real href, navigation continues anyway, so a broken page script cannot leave
  normal links dead. `setTimeout`/`setInterval`
  are pumped by a 50 ms interactive tick with a 10 ms interval floor; console
  messages go to the `!` console, `console.error` turns the footer JavaScript
  label red, and
  `alert` appears in the footer. Timer callbacks that dirty the DOM do not
  rebuild on every tick: timer-driven rebuilds are coalesced to at most once
  per `max(250 ms, 2× the previous rebuild time)`, so a page whose timers fire
  every 50 ms cannot consume every core with full style/layout passes.
  User-input redraws remain immediate. Idle pages do not repaint: the event
  loop draws only after input, a load result, a resize, a JS timer that actually
  rebuilt the page, or a changed memory readout. Pending timers arm a one-shot
  deadline for the next due timer instead of waking on a fixed tick, so a page
  with no timers sleeps; this also stops graphics-protocol image placements
  from being re-sent every frame. In headless automation timers run only while
  a load is applied and during the `wait`/`wait_for` tools, which pump due
  timers, force the deferred rebuild when one is pending, and apply timer-driven
  navigations; unrelated tool calls do not advance timers.
- **JavaScript safety:** every script and callback runs under a 2-second goja
  interrupt watchdog and a per-page absolute heap ceiling checked by one sampler
  (25% of `-mem-limit`/`GOMEMLIMIT`, else 256 MB; raise `-mem-limit` to raise
  it). A page's load-time scripts share an 8-second budget
  and run at most 2,000 scripts; the rest are skipped with a console notice. Esc
  or Ctrl-C interrupts the script currently blocking the loop. Timer count and
  per-tick callbacks are capped. Script
  errors turn the footer JavaScript label into red `JavaScript:Error` and are
  logged for the `!` console; they do not replace the left status text. Navigation via
  `location.href`,
  `location.assign`/`replace`/`reload` is queued until the script or callback
  finishes, then starts a normal load. `<meta http-equiv="refresh">` redirects
  are followed immediately. Storage and the cookie jar are in-memory only and
  nothing is persisted. `XMLHttpRequest` and `fetch` use the shared cookie jar
  and User-Agent but are synchronous under the hood. There is no CSP; ES modules
  are loaded by the page loader, and `history.back`/`forward` are stubs.
- **Dump mode with JS:** `-dump` runs scripts and `DOMContentLoaded`/`load`
  synchronously so output reflects DOM changes; timers do not run in dump mode.
