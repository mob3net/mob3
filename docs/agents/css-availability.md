# CSS availability

Part of the MOB3 agent guidance. Start at [AGENTS.md](../../AGENTS.md) for the working
agreement, the non-negotiable constraints and the change protocol.

This page tracks widely available CSS features (the MDN CSS reference's
baseline features) that MOB3 does not implement, ordered by the impact their
absence has on real pages. It is a gap list, not a full support matrix;
supported behaviour and exact approximations live in
[rendering.md](rendering.md). Priority means impact, not implementation cost.

- **P1:** can hide, clip or misplace page content or break the layout on
  mainstream pages.
- **P2:** common readability, interaction or responsive-layout gap.
- **P3:** visual polish or a less common layout feature.
- **P4:** deliberate non-goal for a terminal text browser.

Basic CSS multi-column (`columns`, `column-count`, `column-width` with
`column-gap`) is supported for block children; its remaining limits are in
[rendering.md](rendering.md).

| Priority | Feature | Current behaviour | Impact |
| --- | --- | --- | --- |
| P1 | `@container` size queries | Containers are not evaluated and their blocks are dropped. | Component layouts that switch at a container width stay in their base state and can overflow or stack wrongly. |
| P1 | `@supports` | The condition is not evaluated; the block is always applied. | A page's `@supports not (...)` fallback can override its modern layout, undoing the feature it is guarding. |
| P1 | `transform` (`translate`, `scale`, `rotate`) | Only a transform that moves content fully off-screen marks it hidden; transforms do not move or scale boxes. | Absolutely centred overlays (`translate(-50%,-50%)`), carousels and badges are offset by half their size. |
| P1 | `inset` and logical inset properties | Ignored; only `top`/`right`/`bottom`/`left` are read. | `inset:0` modals, overlays and fixed panels shrink to their content or sit at the static position. |
| P1 | `text-overflow`, `overflow-wrap`, `word-break`, `hyphens` | Ignored; overflow clipping only trims whole fragments. | Long titles, URLs and unbroken words overflow, clip without an ellipsis, or widen narrow cards. |
| P1 | `animation` and `transition` | Not rendered; base declarations remain. | Content whose base state is `opacity:0` and is revealed by an entrance animation stays invisible. |
| P2 | `direction` and `writing-mode` | Ignored; layout stays left-to-right and horizontal. | RTL and vertical-writing pages lay out from the wrong edge or in the wrong axis. |
| P2 | `:focus-visible`, `:focus-within` (and `:focus`) | Never match; MOB3 paints its own focus rectangle. | Menus and search fields that open on focus do not appear, and focus-only styling is lost. |
| P2 | `@media (hover)`, `(pointer)`, `(orientation)`, `(prefers-reduced-motion)`, `(resolution)` | Unsupported media features do not match. | Mobile, hover-capable, portrait and reduced-motion variants never activate. |
| P2 | `pointer-events` | Ignored by hit testing. | Click-through overlays, disabled cards and modal backdrops receive clicks they should not. |
| P2 | `align-content` and `place-content` | Ignored; only `align-items`, `justify-content` and `place-items`/`place-self` apply. | Flex and grid rows and packed tracks lose their vertical or cross-axis spacing. |
| P2 | `grid-auto-rows` and `grid-auto-columns` | Ignored; implicit tracks use their default sizing. | News and card grids with implicit rows get the wrong row height or track width. |
| P2 | `background-repeat`, `background-clip`/`origin`/`attachment`, multiple layers and sprite `background-position` | One background image is painted once with cover/stretch sizing. | Tiled textures, clipped or attached backgrounds and sprite-sheet icons render wrong. |
| P2 | `oklch()`, `lab()`, `lch()`, `hwb()`, `color()`, `color-mix()` and `light-dark()` | Not parsed; the declaration is dropped. | Modern palette tokens fall back to inherited colours, flattening themes and contrast. |
| P3 | `letter-spacing` and `word-spacing` | Ignored. | Wide tracking on headings, labels and small caps is not reflected in cell widths. |
| P3 | `text-wrap: balance` and `text-wrap: pretty` | Ignored. | Headings and captions wrap unevenly instead of balancing lines. |
| P3 | `border-radius` | Ignored; boxes stay square. | Cards, buttons and pills lose their rounded shape. |
| P3 | `box-shadow` and `text-shadow` | Ignored. | Depth, emphasis and outlined text disappear. |
| P3 | `outline` and `outline-offset` | Ignored; focus and selection use MOB3's own rectangle. | Author focus rings and decorative outlines are missing. |
| P3 | `text-decoration-color`, `text-decoration-style` and `text-decoration-thickness` | Only `text-decoration-line` is read. | Underline colour, style and thickness on links and headings are lost. |
| P3 | `filter`, `backdrop-filter` and `mix-blend-mode` | Ignored. | Dimmed images, blurred backdrops and blended overlays are not approximated. |
| P3 | `subgrid` | Treated as no template. | Nested cards and form rows do not share their parent's tracks. |
| P3 | `table-layout: fixed` and `border-spacing` | Ignored; tables use content-based columns and collapsed borders. | Fixed dashboard tables and spaced table layouts are sized differently. |
| P3 | `list-style-position`, `list-style-image`, `content: attr()`, `counter()`/`counters()` | Only literal `content` strings and list marker types are supported. | Custom bullets, generated labels and numbered headings disappear. |
| P3 | `@property` | Ignored. | Typed custom properties, their initial values and fallbacks are unavailable. |
| P3 | `position:sticky` with `bottom`/`left`/`right` | Only a `top` offset follows the scroll. | Sidebars and footers anchored on other edges scroll away. |
| P3 | `color-scheme`, `accent-color` and `appearance` | Ignored; MOB3 uses its own palette and form widgets. | Native light/dark hints and accent-coloured controls are not reflected. |
| P3 | `user-select` | Ignored; MOB3 selection is always available. | Page-declared selection exclusions are not honoured. |
| P4 | `@font-face`, `font-feature-settings` and `font-variant-*` | No font files; the terminal font is used. | Web fonts, ligatures, tabular numerals and small caps cannot be reproduced. |
| P4 | `mask` and shaped `clip-path` | Only full-inset/rect hiding is detected; shapes are not clipped. | Shaped masks and decorative clips are ignored. |
| P4 | `cursor` | Irrelevant without a pointer. | The terminal owns the pointer. |
| P4 | `scroll-snap`, `scroll-behavior`, `overscroll-behavior` and `scrollbar-*` | Ignored; the terminal owns the scrollbar. | Scrolling stays line- and page-based. |
| P4 | `border-image`, 3D transforms, `box-decoration-break` and `ruby` | Ignored. | Decorative borders, 3D effects and ruby annotation are not modelled. |
