# Rendering and layout

Part of the MOB³ agent guidance. Start at [AGENTS.md](../../AGENTS.md) for the working
agreement, the non-negotiable constraints and the change protocol.

- **Scope:** ambitious CSS subset rather than text-only or solid-basics — own
  CSS parser, cascade, specificity, inheritance, selectors, box model, tables,
  lists, backgrounds, borders and text styling. A selector that fails to parse
  drops its rule instead of matching everything, including an empty
  `:is()`, `:where()` or `:not()` (generated CSS sometimes ships one, and
  treating it as a universal match would apply e.g. `position:absolute` to the
  whole document). Basic row-direction flexbox and
  basic CSS grid are supported; left/right floats are placed at the current
  row, shrink to fit, shorten following line boxes and honour `clear`, and a
  box with `position:sticky` and a `top` offset follows the scroll within its
  containing block.
- **Floats:** `float:left`/`right` (and the `inline-start`/`inline-end`
  aliases) take the box out of normal flow. Floats are placed at the current
  flow row, shrink to fit their content or declared width, and a float with a
  declared width that does not fit the band drops to the next row where it
  does (it sits under the shorter float, as in CSS, instead of collapsing to
  the remaining sliver). Successive
  inline lines wrap around their vertical range. Percentage widths and margins
  resolve against the containing block, not the remaining float band, and a
  declared width is not re-resolved against the float's own used width; a
  declared width also caps the content's min-content width, so a long word
  cannot widen the float. A one-cell shortfall in the band counts as fitting:
  percentage column pairs (75% beside 25%) are exact in a browser but their
  cell-rounded widths can exceed the row by one, and dropping the second float
  would put a sidebar below the article. A blockified `inline-block` child
  (an explicit width plus a block descendant) is still an atomic inline box,
  so beside an active float it sits at the line band's edge rather than at the
  container's left edge, where it would overlap the float and squeeze its own
  text into a one-character column; if its declared width does not fit beside
  the float it starts below it like a wrapped line. A block containing floats contributes their summed
  width as its max-content width, so shrink-to-fit and flex items reserve a
  full float row instead of one float. Lines resume the full width
  once they pass the float's bottom, and the containing flow reserves the
  float's height when the text ends early. `clear:left|right|both` moves
  following blocks below matching floats. Floats are scoped to the block flow
  that establishes them; flex and grid items ignore
  `float`, as in CSS. A float nested inside an inline wrapper is still laid
  out as an inline box. A floated `display:table` figure shrinks to its
  content (usually the image) width; its caption wraps to that width instead of
  inflating it.
- **Overflow clipping:** a box with `overflow:hidden`, `clip`, `auto` or
  `scroll` (including the `hidden auto` shorthand) clips descendant fragments
  to its content box. Text and images that only partially fit are dropped;
  fills and link boxes are trimmed. Absolutely positioned descendants are
  laid out in the later pass and are not clipped. This keeps scrollable panels
  with `max-height` from painting over following content.
- **Positioning:** `position:relative` and `position:absolute` are supported.
  Absolute boxes are taken out of flow and resolved against the nearest
  positioned ancestor's padding box, falling back to the page for root-level
  elements. `top`/`right`/`bottom`/`left` (including percentages) are honoured;
  auto width shrinks to fit, left+right stretches width, and top+bottom with
  auto height stretch vertically. Absolute boxes paint above in-flow content in
  tree order, and their fragments feed link rectangles, find and dark mode
  normally. Relative offsets shift a block and its painted subtree without
  moving siblings, and the containing block it establishes moves with it.
  The same applies to an absolute box placed by `right`/`bottom`: when its
  final position is applied after layout, the containing block it establishes
  and its auto-placed absolute descendants move with it, so nested cards and
  badges stay at the intended edge.
  `z-index` orders positioned boxes at paint time: the nearest positioned
  ancestor's layer is inherited, and fragments sort by z-index within the
  scrolling page, sticky and fixed phases. Hit testing ranks a node by the
  highest `z-index` among its positioned ancestors, so a `position:relative;
  z-index:0` child inside a fixed `z-index:9999` modal is not masked by
  lower-z positioned content in the page below; the fixed phase already paints
  that modal on top. This is still a flat approximation: nested stacking
  contexts, negative layers behind a parent's background, and contexts created
  by `opacity`, `transform` or flex/grid items are not modelled. Relative
  offsets are applied to block boxes only (inline relative is ignored).
- **Overlay links:** an empty anchor with an absolute or fixed box contributes
  an invisible `fragLink` rectangle, so card overlay links are clickable and
  appear in Tab order; a focused overlay link gets a yellow border.
- **Fixed positioning:** `position:fixed` boxes are anchored to the terminal
  viewport instead of the document. Layout receives the current viewport height
  from the browser, resolves `top`/`right`/`bottom`/`left` against it, excludes
  fixed fragments from page height and scrolling, and marks them so the draw
  path re-anchors them by the scroll offset each frame (link rectangles are
  rebuilt when the offset changes). Fixed fragments paint after the scrolling
  page, so fixed headers, bars and banners stay above content that scrolls
  underneath them; a fixed fill also clears the cells it covers so page glyphs
  cannot show through. Resizing rebuilds layout with the new viewport height. In dump mode, where no viewport exists, fixed boxes fall back
  to page-anchored absolute placement so their content still renders.
  A `position:sticky` box with a `top` offset stays in flow, is pushed down to
  that offset while its containing block is scrolled, stops at the containing
  block's bottom, paints above in-flow content but below fixed chrome, and is
  treated as an overlay by hit testing. `bottom`/`left`/`right` sticky and
  nested sticky boxes keep their block-flow position. The `X` → Sticky
  elements switch rebuilds the page with sticky boxes in normal flow, so they
  scroll with the content instead of following the offset.
- **Cascade layers:** `@layer` blocks are parsed instead of dropped, so
  component CSS wrapped in layers still applies. Layer order follows
  declaration order across the document's stylesheets, including
  `@layer a, b;` statements, so a later layer wins over an earlier one;
  unlayered rules beat every layer, and `!important` reverses that order.
  Nested layers use dotted names. Anonymous `@layer { … }` blocks get their
  own slot in the same order.
- **Responsive media queries:** `@media` conditions are stored per rule and
  matched against the layout viewport (`columns` × the current CSS pixels per
  cell, 8 at 100% zoom). `screen`/`all`,
  min/max-width (and device-width), and range syntax such as `(width>=960px)`
  work; `print` never matches; `prefers-color-scheme` follows the dark-mode
  state (`dark` when `-dark`/`D` is on, `light` otherwise), while other
  unsupported features do not match. This keeps
  desktop/mobile duplicates from rendering at the same time. Media-query
  results and element class lists are cached for a style pass, so repeated
  rebuilds do not re-parse them for every rule and element.
- **Frame budget:** style computation and layout share a cooperative deadline
  per rebuild. Rule matching and the block/inline/table/flex/grid layout entry
  points check it; expiry produces a partial layout with `Layout.Truncated`
  set and the status `Page truncated (layout limit)`. This is the safety net
  against pathological pages, alongside the fragment/row limits. The three
  limits are bundled into one `-layout-budget` preset, editable in
  `about:config`: `low` (1s, 100k fragments, 80k rows), `standard` (2s, 250k,
  200k; the default), `high` (5s, 1M, 1M) and `none` (disables all three,
  which can freeze the UI on hostile pages). A zero limit disables that
  individual check.
- **Flexbox:** `display:flex` with row direction lays children
  side by side, honouring explicit widths, `flex`/`flex-grow`/`flex-basis`,
  `flex-shrink` (items shrink in proportion to their factor and their slack
  above min-content; `flex-shrink:0` items keep their size and overflow, so
  carousel slides and fixed toolbars do not collapse), shrink-to-fit items and
  `gap`/`column-gap`. A flex item is also bounded by its `min-width` and
  `max-width`, applied to its base size before free space is distributed, so a
  wide descendant with a fixed pixel width (a carousel stage, a wide table)
  cannot inflate the item past a `max-width:75%` column and push the next
  column off the row. Space a `max-width` item cannot take is redistributed to
  the other growing items, so a `flex:0 0 100%;max-width:312px` rail beside a
  `flex:1` column does not leave the column half empty. `inline-flex` and `inline-grid`
  are treated as atomic inline-block boxes so buttons and badges stay inside
  the surrounding line instead of becoming full-width rows. Items keep at least their
  min-content width instead of shrinking into one-character columns.
  Empty `::before`/`::after` boxes on a flex or grid container are items, so the
  common responsive-media pattern (`::after{content:"";padding-bottom:56.25%}`
  beside an absolutely positioned image) gives its box a height. Column flex
  stacks blockified children with a vertical gap: an inline `<picture>` with
  `width`/`aspect-ratio` becomes a full-width block with a height instead of an
  inline box with none; column gaps use the row cell height and column
  `justify-content`/`align-items` are not applied.
  `flex-wrap:wrap` starts a new row when the next item's base width would
  overflow; each wrapped row is sized independently and wrapped rows stack
  without an extra gap. Each length rounds to whole cells on its own, so a row
  that fits exactly in CSS pixels can measure one cell over here (a 940px
  column, a 300px rail and a 20px gap are 118 + 38 + 3 cells in a 158-cell
  container); a one-cell shortfall counts as fitting, as it does for floats,
  and the shrink pass absorbs the difference instead of wrapping a whole
  column below the other. `flex-wrap:nowrap` (including `!important` overrides)
  keeps items on one line. A flex item's assigned width is used as its box
  width without re-resolving percentage widths inside block layout, so padded
  percentage columns keep their slots and do not overlap the next column.
  Items are blockified, so an inline element becomes a block container and its
  width and block children apply. `order` reorders items stably;
  `justify-content`
  supports start/end/center/space-between/around/evenly, `margin:auto` on a
  flex item absorbs the free space before justify-content (so
  `margin-left:auto` pushes an item to the right), and `align-items` supports
  start/center/end cross-axis placement. A flex item's percentage margins,
  padding, borders and min/max sizes resolve against the flex container's
  content box, not the slot the line assigns it, so percentage offset columns
  (a quarter-width offset before a quarter-width rail) keep their gutter.
  Cross-axis alignment shifts
  only that item's own fragments, so later siblings and following content keep
  their positions. Form controls always use the
  inline widget path even when CSS
  gives them block/flex display, so their editable value is rendered inside
  flex containers. An `inline-block` box with an explicit width and block
  descendants is laid out as a block container so page wrappers and cards keep
  their internal grid/flex layout; an inline-block without a width stays an
  atomic inline box.
- **CSS grid:** `display:grid` lays children in row-major order over tracks
  parsed from `grid-template-columns` (lengths, percentages, `fr`, `auto`,
  `repeat()`, `minmax()`, named lines), honours `gap`/`column-gap`/`row-gap`,
  `grid-column`/`span`, and sizes row heights from the tallest item. An `auto`
  track takes the max-content width of the items placed in it (spanned items
  spread their deficit over the columns they cover), shrinks toward its
  min-content width when the row is too narrow, and the `fr` tracks then split
  what remains. That keeps the common `auto 1fr` thumbnail-beside-text row from
  collapsing the thumbnail column to one cell and stretching the image
  vertically. With no `fr` track, leftover space is shared between the `auto`
  tracks so plain `auto auto` rows still fill the width. Dense grids
  whose average track would be under 6 cells fall back to block flow so text
  stays readable, but only when the placed items themselves are also under 6
  cells: a 24-track page grid whose cards span 6-12 tracks keeps its columns.
  An explicit numeric `grid-row` places the item in that row even when it
  appears after a card to its right in the DOM, so a centre lead card and side
  rails written in a different order stay on one row. Named `grid-template-areas`
  place children by their `grid-area` name; a name spanning several columns
  starts a new row when it
  would move backwards, and an `auto` track used by a named area is sized to
  that area's content instead of collapsing to one cell. Horizontal
  `justify-items`/`justify-self` (and the `place-items`/`place-self`
  shorthands) align an item's margin box start/center/end/stretch within its
  column, including a grid with no explicit template, which becomes one
  implicit column when it centers or end-aligns its items. Row spans beyond
  that new-row rule, `grid-template-rows`, named row lines, auto-fill/auto-fit
  and vertical alignment remain approximated.
- **Box sizing and constraints:** `box-sizing:border-box` makes declared
  widths/heights/min/max include padding and borders in block and absolute
  layout and in flex item widths/basis. A declared fixed `width` (px, em,
  rem, pt, viewport units) is used even when it is wider than the containing
  block, so a fixed desktop canvas overflows to the right and clips instead of
  collapsing to the terminal width and reflowing the page; percentage and
  `calc()` widths only shrink the box, so padded percentage columns stay
  inside the parent. An explicit `height` is the used
  height, not a minimum: overflowing children keep their fragments and may
  paint outside the box unless `overflow` clips them. `min-width`, `max-width`,
  `min-height` and `max-height` apply to block boxes; a percentage height
  resolves only when the containing block height is definite (a declared
  height, a percentage with a definite ancestor, or a stretched absolute box)
  and stays auto otherwise, and `min` wins over `max` when they conflict.
- **Inline alignment:** `vertical-align: top|bottom|text-top|text-bottom`
  places inline runs and atomic boxes at the line-box edge; `middle` centers
  them. `baseline`, `super` and `sub` are approximated as middle because
  terminal rows have no usable baseline.
- **Off-screen transforms:** a `transform` that moves content fully off-screen
  (`translateX/Y(-…)` or `scale(0…)`) marks the element hidden. This suppresses
  cloned fixed navs and closed drawers that rely on transforms instead of
  `display:none`.
- **Anti-FOUC visibility:** `visibility:hidden` on `html` or `body` is ignored
  so the page and its fixed chrome render; explicit hidden boxes remain hidden.
- **Cell mapping and zoom:** the base mapping is `1 column = 8 CSS px` and
  `1 row = 16 CSS px`. Two image rows are packed into one terminal cell
  vertically to preserve image aspect ratio. `+`/`-` step a session layout zoom
  through 25, 50, 67, 80, 100, 125, 150, 175 and 200%, and `0` resets it.
  `-zoom N` (25-200, `%` optional) starts interactive, dump, PNG and MCP runs
  at that level. Zoom
  divides the CSS pixels per cell, so fixed pixel widths,
  padding, borders, absolute offsets, images and kitty scaled headings grow or
  shrink while text metrics stay on the base cell size: glyphs remain one cell
  per character and line heights keep their normal rows. Percentages stay
  relative to the CSS viewport, which narrows or widens with the ratio, and
  media queries and script-visible viewport sizes follow it, so responsive
  pages reflow. Zoom is memory-only session state shared by every page,
  including pages restored from the back/forward cache. The URL row always
  shows the current zoom as a clickable ` 100% ` badge at its far right (one
  space on each side); clicking it opens a centered Zoom window listing the
  presets, which can be chosen with the mouse or arrow keys and `Enter`, and
  `Esc` closes it.
- **Line height:** computed with `floor(fontSize * lineHeight / 16)`, minimum
  1 row, using the base 16px row regardless of zoom. This avoids body text with
  `line-height: 1.6` consuming two terminal rows. Rule of thumb: only fonts
  ≥32px produce a 2-row line box.
- **Base font:** 16px, line-height 1. UA headings use bold/color/margins; h1 is
  32px (2 rows), others are effectively 1 row.
- **Large type:** true scaled glyphs use kitty’s text-sizing protocol in real
  kitty with the minimal `OSC 66 ; s=N ; text BEL` form; layout allocates
  `round(font-size/16)` cells per character at 100%, multiplied by the zoom and
  capped at kitty’s `s=7`, with matching line rows for runs scaled above 1. Do not send the optional `w` key: metadata uses `:` between
  keys, and terminals that only parse `s` render a `w=...` prefix as literal
  text. Disabled on non-kitty terminals, in dump mode, and with
  `-text-scale=off`, where the bold/line-height hierarchy remains. A page whose
  scaled runs overlap other text in the same cells falls back to normal-size
  text for that page, so dense multi-column layouts are not garbled. Control
  characters are stripped before text is embedded in the OSC sequence, so page
  text cannot inject terminal escapes. Whitespace-only runs are painted as
  plain blank cells instead of scaled text, because scaling a space makes
  terminals draw its underline at the unscaled baseline as stray lines inside
  or below large headings; the surrounding words keep their scaled underline.
  A scaled run that would extend past the last page row (over the footer or
  another chrome row) is painted at normal size, because the terminal draws
  scaled glyphs outside the cell grid. A scaled run whose scaled box intersects
  an opaque fixed-chrome fill (a cookie bar or fixed header) is likewise
  painted at normal size: terminal-side scaled text composites above the cell
  grid and would spill large glyphs over the fixed fill that is painted later.
  Cells covered by a run are skipped when its rows are painted, so a plain line
  diff cannot erase what an earlier run left there. Any frame whose run set
  differs from the previous frame's (a reflow can move headings without a
  scroll) therefore forces a whole-screen repaint.
  While a modal window, popup or front-end menu is open the page is rebuilt
  without scaled text and restored when it closes, so terminal-side glyphs cannot paint over
  the modal; this is the text counterpart of native images falling back to
  half-block art.
- **No font files:** the terminal font is used for all glyphs; font size affects
  line height and emphasis, not glyph scale outside kitty.
- **UA page style:** the grid defaults to a white page background and
  near-black text, with an `8px` `body` margin so pages get the usual browser
  gutter unless author CSS overrides it. The UA sheet hides `[hidden]` with
  `display:none`, matching the HTML presentational hint. The UA sheet does not paint `html` or
  `body` backgrounds itself, so an author wrapper or an unsupported gradient
  leaves room for the fallback below.
- **Page background fallback:** when neither `html` nor `body` has an opaque
  background, the largest opaque fill covering at least 60% of the first
  viewport (or of a short document) becomes `PageBG`. This rescues full-page
  wrapper colors and dark themes that would otherwise leave the grid white.
- **Auto margins:** `margin:auto` is distinguished from an unset length; a
  constrained-width block with auto left/right margins centers itself
  (`margin:0 auto`).
- **Borders/grid lines** are solid colored cell rectangles, not box-drawing
  characters. Table borders use a collapsed 1-cell grid. Per-side
  `border-style` is tracked; a side whose style is `none`/`hidden` reserves no
  border width even when `border-width` is declared, and a `border` shorthand
  without a style keyword leaves the side borderless. Absolute boxes resolve
  their used width once against the containing block instead of re-applying
  `width` to the already-constrained box.
- **Inline boxes with block content:** an inline element whose subtree contains
  a block-level descendant is laid out as a block, so grids, flex rows and
  tables nested inside an inline wrapper (common in card anchors and section
  wrappers) still run their own layout instead of being flattened into one
  paragraph. An explicit `inline-block` remains an atomic inline box, so menu
  items and buttons stay on one line.
- **`<details>`:** a closed `<details>` renders only its `<summary>`; its other
  children are taken out of layout until the element is opened (or JS sets the
  `open` attribute). An open `<details>` renders its full content.
- **Inline background/space handling:** inter-word spaces are emitted as text
  fragments so backgrounds, link focus highlighting and link rectangles are
  continuous. A collapsed space takes the style and node of the text node that
  contains the whitespace, not the following word, so a link never absorbs or
  underlines the space before it. Inline and inline-block backgrounds (nav
  links, buttons) are painted behind their text fragments; block backgrounds
  are painted once with the box. An inter-word space before an inline box is
  emitted before that box's left margin/border/padding, so the padding cells
  never appear ahead of the space. Inline horizontal margins, borders and padding
  reserve cells in the line; the three edges on each side are summed and
  rounded to cells once, so a 1px border plus 4px padding and 4px margin take
  one cell rather than one cell each. A positive total still rounds up to at
  least one cell, so adjacent menu links do not glue together.
- **Translucent fills:** a background fill with alpha is composited over the
  cell's current background instead of replacing it, so a page's scrim,
  divider or dimmed overlay reads as a tint over the page background rather
  than as a solid black band. Repeated fills stack.
- **Contrast fallback:** when resolved text and effective background colors are
  too close (for example white text whose background came from an unsupported
  gradient or CSS variable), foreground switches to near-black on light
  backgrounds or white on dark backgrounds.
- **Wide characters:** East Asian/emoji width 2, combining marks width 0,
  continuation cells marked and skipped in ANSI output.
- **Text wrapping:** greedy word wrapping; overlong words are hard-split;
  `white-space: pre` preserves spaces/newlines. `nowrap` prevents breaks inside
  a run, but separate nowrap inline boxes may wrap between each other, and a
  nowrap run wider than its line is hard-split unless the containing block is
  nowrap, so terminal lines do not overflow.
  `text-align: justify` stretches inter-word spaces on wrapped lines; the last
  line and forced breaks (`<br>`, newlines) stay left-aligned. Intrinsic width
  measurement collapses whitespace runs to one space and ignores leading,
  trailing and indentation-only whitespace, matching inline layout, and an
  element's explicit absolute width fixes its measured width so shrink-to-fit
  containers do not ignore it. Inside a block or list-item container,
  consecutive inline children are measured as one line (their widths add),
  while block children stack and only the widest counts, so a flex item or
  shrink-to-fit box holding an inline menu reserves the whole menu row.
  `display:contents` wrappers are flattened before measurement, so their
  children add like direct children instead of collapsing the container to the
  widest child and letting neighbours overlap. Whitespace crossing an
  inline-box boundary collapses to one space.
- **Screen-reader-only boxes:** `overflow:hidden` with 1px width and height,
  `clip:rect(…1px…)` with all sides at most 1px, and `clip-path:inset(50%)`
  are hidden (`display:none`) to avoid rendering them as one-character columns
  or visible “skip to content” links.
- **Percentage heights** resolve against the containing block's definite
  content height: a parent with a declared height (including one resolved from
  a percentage), a stretched absolute box, or a flex/grid cell with a known
  height. A `min-height` is only a floor and does not make the height definite;
  resolving percentages against it used to cap a `height:100%` wrapper chain at
  the floor and clip the rest of the page. `height:100%` and `calc(…% …)` therefore work in
  fixed-height sliders and cards. An indefinite (auto-height) parent leaves
  them auto, so `height:100%` on a logo cannot become 100 terminal rows. An
  absolutely positioned image whose containing block height is known also
  resolves `height:100%` for `object-fit` fill/crop.
- **Tables:** shrink-to-fit column widths from max-content measurements, scaled
  down to available width while keeping each column at its min-content width
  and shrinking only columns that have slack. A table `width` declaration
  (including `width:100%`) sets the used table width and expands narrow tables;
  when even the min-content total does not fit, columns scale down proportionally
  instead of overflowing the page. Legacy cell `width` hints size the column,
  not the cell content box: percentage columns are normalised against the
  space left by auto columns (so percentages totalling over 100% still fit) and
  fixed widths act as minimums. A spanning cell constrains the sum of its
  columns, with the deficit shared in proportion to the columns' other content
  rather than forcing every column to an equal share. Internal column borders
  are skipped across a `colspan` cell. A table narrower than its container is
  centred or right-aligned when the inherited `text-align` says so (so
  `<center><table>` works), and the legacy `nowrap` attribute maps to
  `white-space:nowrap`. `colspan` supported; `rowspan` ignored; empty rows are
  skipped. Cells honour
  `vertical-align: top|middle|bottom` (`middle` is the browser-like default;
  `baseline`/`text-*` map to top), shifting shorter content after the row height
  is final. The cell's own background stays put. Hidden inputs take no space,
  and `<br>` breaks the measured line when sizing a cell.
- **Lists:** markers rendered in the list padding area; disc/circle/square,
  decimal, alpha and roman styles.
- **Images:** disabled by default (`-graphics=none`); no image requests are
  made and inline SVG is not rasterized, so `alt` text or labels are shown.
  When images are enabled, intrinsic size in CSS px → cells; CSS/HTML
  width/height honored;
  aspect ratio preserved; display is capped at 40% of the viewport width and
  16 rows, preserving the ratio; transparency composited over the underlying
  cell. A percentage width is container-relative: intrinsic measurement ignores
  it (returning the intrinsic width with a one-cell minimum), so a
  `width:100%` thumbnail can never force its flex or grid slot wider than the
  slot itself and paint over neighbouring text; the image then fills the slot
  and shrinks with it. An inline image's percentage width resolves against the
  nearest sized inline ancestor (a fixed-size avatar ring or wrapper), not the
  terminal line, so a `width:100%` icon inside `w-xl` stays small instead of
  growing to the display cap; the line width is the fallback when no ancestor
  has a definite width. A declared `max-width` caps that intrinsic measurement
  too, so a floated thumbnail styled `width:100%;max-width:110px` measures as
  the 110px cap instead of its source size and does not leave the adjacent
  text wrapped in a narrow column. A decoded image is downscaled once at load to the largest box the
  terminal can paint — the viewport in CSS pixels, floored at 1024×512 — with
  aspect ratio preserved, and only that copy is stored on the node and in the
  image cache, so a many-megapixel photo never keeps its full bitmap alive.
  A panic raised by a raster or SVG decoder is recovered and shown as a broken
  image, so malformed media cannot crash the browser; that is containment, not
  a substitute for patched decoders.
  Images already inside the cap are stored untouched. A definite CSS box (both
  width and height, including an absolutely positioned percentage height) is
  filled according to `object-fit` (`fill`,
  `cover`, `contain`, `none`, `scale-down`) with `object-position`, bypassing
  those display caps; `aspect-ratio` supplies an auto block height from the
  box width;
  PNG/JPEG/GIF/WebP/BMP and SVG. `srcset` candidates are chosen for the
  40%-wide terminal target (smallest width at least as wide, else the widest),
  `<picture>` `<source>` media/type entries are honoured, and common
  `data-src`/`data-srcset` lazy placeholders are used when no standard source is
  present. After page scripts and timer rebuilds, images whose `src`/`srcset`
  changed are loaded again. SVG is rasterized with oksvg +
  rasterx (partial SVG2), capped to 1024 px. External SVG is decoded from the
  response; inline `<svg>` is serialized back to XML with SVG camelCase names
  restored and rasterized before layout. If inline SVG decoding fails it falls
  back to a `<title>` or visible text; an icon-only SVG with only an
  `aria-label` renders a one-cell `▣` marker instead of its accessibility name.
  `aria-hidden` SVGs are skipped. An inline SVG that becomes a block box — a
  flex or grid item, `display:block`, or an absolutely positioned box — goes
  through the same image path and keeps that fallback instead of being treated
  as an empty element. Broken images show `[image: alt]` clipped to
  the image's box width and reserve the image's CSS box height, so a missing
  logo does not collapse its header row (a box narrower than 8 cells uses a
  `▣` marker); an
  empty-`alt` image with explicit dimensions of at least 2 cells shows a `░`
  marker, while 1-cell decorative/tracking images render nothing. Images laid out as blocks or as
  absolute/fixed boxes produce the same image fragments as inline images.
  Percentage top/bottom padding and margins resolve against the containing
  block width, so aspect-ratio wrappers keep their intended shape.
  In interactive mode with a detected graphics terminal, the same rectangles
  are filled with a real inline image
  (kitty/iTerm2) instead of half-block art; images clipped by the viewport are
  cropped to the visible cell rows. A focused linked image loses its border
  highlight because inline graphics paint above text; the footer still shows
  the target. A page image whose visible rows intersect an opaque fixed-chrome
  fill falls back to half-block art for that frame, because terminal image
  placements are composited above the text grid and cannot be erased by the
  fixed fill; fixed images (such as a logo inside a fixed header) stay native.
  The same fallback applies to an image with page text painted over it (a
  caption, a tab strip over a hero photo), since a placement would hide that
  text.
- **Iframes:** an iframe has no browsing context and its document is never
  fetched. It still behaves as a replaced box: CSS `width`/`height` (legacy
  `width`/`height` attributes are mapped before author CSS), `aspect-ratio` for
  the missing side, or the browser's 300×150 CSS px default, converted to
  terminal cells. The reserved area is painted black with a gray `iframe`
  label, or a single `i` when narrower than six cells. An inline box caps its
  width to the line band while keeping its declared height; a block iframe uses
  the block width and falls back to the default height when auto. Children
  (the frame's fallback content) are not laid out, and content-driven resizing
  never reaches the parent page.
- **Background images:** `background-image:url(...)` is fetched through the
  same image cache and painted behind the box's content, so text, links and
  form controls stay above it. Only block-level boxes are painted (`block`,
  `list-item`, `flex`, `grid`, `table`, `inline-block`, and absolute/fixed
  boxes); inline boxes are skipped because they would need per-line-fragment
  painting. The image fills the box with `background-size:cover` semantics
  (an explicit `100% 100%` stretches it), cropped per `background-position`
  (default centre). `html`/`body` backgrounds are skipped because CSS
  propagates them to the canvas and painting one across the document box would
  stretch it over the whole page. Gradients, repeat patterns and
  `background-attachment` are not rendered. Backgrounds are always drawn as
  half-block art, never as kitty/iTerm2 placements: terminal placements
  composite above the cell grid and would hide the text sitting on the image.
  They are skipped with `-graphics=none` (no image requests) and can be
  toggled at runtime with `X` → Background images. Switching them off keeps
  the images loaded and laid out and paints each background box with a
  neutral mid-grey placeholder, so the site's black or white text (and the
  contrast fallback) stays readable while the hidden bitmap is obvious.
- **Graphics protocol preference:** default is `none` (no image loading or
  drawing, for speed). When enabled with `-graphics=auto|kitty|iterm|blocks`,
  kitty comes first, then iTerm2, then half-block art. Auto is selected from
  environment variables. Under tmux/screen/zellij auto-detection
  falls back to half-block art because the multiplexer consumes graphics
  escapes unless passthrough is configured. `i` cycles native graphics →
  blocks → none (or blocks → none when native graphics are unavailable),
  rebuilding the current page in place while keeping the scroll row. It does
  not reload the page or re-run its scripts: decoded images stay on the page
  and layout hides them in `none` mode, and images not yet loaded are fetched
  on the first switch that enables them.
- **Graphics encoding:** images are transmitted as PNG (kitty `f=100`, base64;
  iTerm2 `OSC 1337;File`). Text is still painted as ANSI cells; only image
  rectangles are overlaid. Kitty deletes previous placements each frame
  (`a=d`) and re-places images, scaled to the layout cell box, with `q=2` so
  replies do not pollute stdin; APC replies are drained defensively. A
  scroll/resize/mode-switch repaint clears text with `ED 1` + `ED 0` instead of
  `2J` in kitty, because those erase placements but keep stored image data; the
  cached image is then re-placed by id, so scrolling an image-heavy page does
  not re-scale, re-encode and re-upload every image per step. A true `2J` clear
  still frees kitty data and resets the placement cache. Image caches are
  bounded by evicting only images that are absent from the current frame; a
  frame showing more images than the soft cap keeps all of them, so periodic
  redraws can never fall into a reset-and-rebuild-every-image loop. iTerm2
  saves/restores the cursor around an inline image sized in cells with
  `preserveAspectRatio=0`. Source images are downscaled to about the terminal
  cell size (default 8×16 px) before encoding. Dump mode always uses half-block
  art for the images that were loaded.
- **PNG screenshots:** `-png` implies dump mode and writes a PNG stream to
  stdout; the file is capped at 2,000 rows and truncation is reported on stderr.
  For a PDF it renders one page (page 1 by default) so the image is a page, not
  a thumbnail strip; `-pdf-page=N` picks another page and `-pdf-page=all`
  restores the whole document. The MCP `screenshot` tool renders
  the current page viewport and returns an `image/png` content block plus a
  short text line. Both use 8×16 pixel cells and bundled Go Mono faces, include
  text colors/backgrounds, bold/italic/underline/strike and half-block image
  cells, and omit browser chrome. They are terminal-cell rasters: CJK and emoji
  glyphs may be missing, and they are not pixel screenshots of a browser.
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
- **Color depth:** cell output uses 24-bit `38;2`/`48;2` SGR by default. macOS
  Terminal.app (`TERM_PROGRAM=Apple_Terminal`) does not implement those
  sequences and garbles the toolbar and footer, so it gets xterm 256-color
  output (`38;5`/`48;5`), with each RGB mapped to the nearest 6x6x6 cube or
  grayscale ramp entry. A `COLORTERM` value of `truecolor` or `24bit` keeps
  truecolor output. `-no-color` disables color but interactive output keeps
  bold/italic/underline/strike attributes so links stay marked, and link hint
  letters use reverse video; dump and automation output remain plain text.
  A no-color row that shrinks clears the rest of its terminal line.
- **Dump rendering:** prints the whole document height (capped at 20,000 rows),
  no browser chrome; `-height N` sets an `N`-row layout viewport and limits the
  output to `N` rows. Color is automatic on a TTY, forced by `-color`, disabled
  by `-no-color`.
