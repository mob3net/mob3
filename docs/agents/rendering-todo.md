# Rendering TODO

Part of the MOB³ agent guidance. Start at [AGENTS.md](../../AGENTS.md) for the working
agreement, the non-negotiable constraints and the change protocol.

Open rendering issues, ordered by the number of affected pages in the site
audit. Remove an item when it is fixed and covered by tests. This page is for
engine work only; do not add site-specific exceptions.

## P1 — Overflow and hidden content

- **Visually hidden UI.** Extend detection beyond `clip:rect(1px…)` and
  `inset(50%)`: off-screen absolute/fixed skip links and mobile menus can still
  occupy cells or wrap into narrow columns. Suppress only boxes that are fully
  outside the layout box and cannot become visible.

## P1 — Selectors and container queries

- **`@container` queries.** Container-query blocks are dropped today and 4/11
  sampled pages use them. Only add an approximation if it can be made sound
  through size containment; do not guess from the viewport.

## P2 — Flex and float layout

- **Shrinking and min-content.** Audit flex/grid items that collapse to one
  character per line. Respect `min-width:0`, automatic minimum sizes,
  `overflow` and wrapping so text never becomes a vertical
  column (`flex-shrink` is honoured, and `flex-shrink:0` items keep their
  size).
- **Float edge cases.** Floats crossing nested block flows and
  block background/border interplay are approximated; floats inside inline
  wrappers are laid out inline. Add coverage before changing the float pass.
- **Flex alignment edge cases.** `align-items:stretch` and `align-self` are
  still approximate; verify icon/label rows and cards with mixed heights.
- **Grid alignment edge cases.** `align-content`, baseline alignment, `fr`
  row tracks inside a definite-height grid container, and percentage heights
  inside stretched items remain approximated.

## P3 — Images and backgrounds

- **Image candidate fallback.** When a `<picture>` source candidate fails to
  decode (for example an AVIF URL without a `type` hint), the `img` fallback is
  not retried, and `srcset` URLs containing literal commas are split as
  candidates.
- **Background gradients and repeat.** `background-image:url(...)` is
  rendered (see the images section), but gradients and tiled repeat patterns
  are not; such values leave the box's background colour only.

## P4 — JavaScript-rendered pages

- Pages whose content arrives only after client-side rendering can show a
  skeleton or blank area. Prefer a visible fallback such as the reader-mode
  hint or the page's noscript content when the rendered document has almost no
  text, instead of an empty frame.
