# Site support

MOB³ is a content browser, not a full browser engine. It is happiest with
text-focused, server-rendered pages and gets rough on modern JavaScript apps.

## Works well

- Documentation, blogs, news articles, wikis, forums, changelogs and old or
  server-rendered HTML.
- Headings, paragraphs, lists, links, tables, `<pre>` blocks, images and common
  presentational markup.
- Responsive pages that use `@media` width queries, CSS cascade layers
  (`@layer`), basic flex rows and column flex media boxes, including framework
  columns with percentage offsets and the common `box-sizing: inherit` reset,
  basic grid tracks with explicit `grid-row`/`grid-area` placement, simple
  `calc()` and viewport-unit sizing, aspect-ratio
  `::before`/`::after` boxes and literal pseudo-element text, images sized with `object-fit`, CSS
  `background-image` photos on block containers, forms
  and simple JavaScript DOM effects.
- Older pages that declare a single-byte charset (Windows-1250/1251/1253/1257
  and ISO-8859-2/5/7 and similar) are decoded correctly from the
  `Content-Type` header or `<meta charset>`.
- Fixed-width desktop layouts keep their declared canvas width even when it is
  wider than the terminal, so columns stay in place and the page clips at the
  right edge, like a browser window narrower than the page, instead of
  collapsing into one reflowed column.
- Classic bundles that only need platform APIs to exist: `IntersectionObserver`
  and `ResizeObserver` (one synthetic entry per target), `NodeList`,
  `EventTarget`, `CSS.escape`, `customElements`, `ReadableStream`, `Path2D`,
  Web Animations stubs and inert `Worker`s. ES module scripts run with their
  relative imports, dynamic `import()` returns a real namespace, and
  `import.meta.url` reports the page URL.
- Responsive and lazy images: `srcset` candidates, `<picture>` sources and
  common `data-src`/`data-srcset` placeholders are selected, and images added
  or changed by scripts are loaded after the page runs.
- Reader mode (`R` or `-reader`) for long articles.

## PDF documents

A PDF response opens in a built-in, text-only viewer: every page is extracted
and shown as one continuous document where each page ends with its `Page n`
caption above an underscore rule. The reading column is centered and every page is padded to the same
height, so the document scrolls like a printed book. Text keeps its column alignment, so
tables and multi-column pages stay readable, and the result supports find,
selection, `-dump` and `-png` like any other page.
The footer shows `Page n/N` and a clickable `(d -> Download name)` hint; `d`
opens the same curl popup.

The viewer never rasterizes a page. Figures, images, charts, annotations and
links are not shown, scanned PDFs have no text to show, and encrypted
(password-protected) documents are refused. `-no-pdf` turns the viewer off and
restores the download prompt; an unreadable document always offers the same
curl command.

## Often rough

- Single-page applications, heavy shadow DOM, custom elements and CSS-in-JS.
- Sites that require a browser check, CAPTCHA, unusual TLS or a specific
  certificate.
- Complex grid/flex layouts, sticky positioning, z-index stacking, full float
  behavior, animations and non-text generated content (`attr()`, counters,
  images, icon-font private-use glyphs).
- Visual results from canvas, WebGL, video and WebAudio.
- Multipart/file-upload forms.

## Not supported

- Import map scopes, workers, service workers, WebSocket, IndexedDB, WebRTC,
  geolocation, clipboard APIs, notifications and browser payment APIs.
- Async generator syntax (`async function*`) and `for await`: the script
  containing them does not parse, so it is skipped.
- CSP enforcement, certificate UI and proxy configuration UI.
- Pixel screenshots for automation. MOB³ has no font rasterizer.

## Tips

- `-no-js` shows the no-script fallback, which is sometimes more useful.
  `J` toggles scripting and reloads the page for the rest of the session.
- `R` extracts the main article and removes navigation, sidebars and images.
- `D` asks sites for `prefers-color-scheme: dark` and keeps an author dark
  palette when the page already renders a dark background.
- `-layout-budget low` can keep a very large or generated page responsive.
- `-dump -width 120 -no-color` is handy for comparing layout without a terminal.
