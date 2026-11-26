# Rendering TODO

Part of the MOB³ agent guidance. Start at [AGENTS.md](../../AGENTS.md) for the working
agreement, the non-negotiable constraints and the change protocol.

Open rendering issues, ordered by the number of affected pages in the site
audit. Remove an item when it is fixed and covered by tests. This page is for
engine work only; do not add site-specific exceptions. The wider list of
widely available CSS gaps is in [css-availability.md](css-availability.md).

## P1 — Selectors and container queries

- **`@container` queries.** Container-query blocks are dropped today and 4/11
  sampled pages use them. Only add an approximation if it can be made sound
  through size containment; do not guess from the viewport.

## P2 — Flex and float layout

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
- **Tiled background images.** `background-image:url(...)` is painted once with
  cover/stretch sizing, but `background-repeat` and repeated bitmap patterns
  are not rendered.
- **Conic gradients.** `conic-gradient()` backgrounds are not rendered;
  linear, radial and repeating linear/radial gradients are.

## P4 — JavaScript-rendered pages

- Pages whose content arrives only after client-side rendering can show a
  skeleton or blank area. Prefer a visible fallback such as the reader-mode
  hint or the page's noscript content when the rendered document has almost no
  text, instead of an empty frame.
