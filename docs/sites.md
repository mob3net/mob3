# Site support

MOB³ is a content browser, not a full browser engine. It is happiest with
text-focused, server-rendered pages and gets rough on modern JavaScript apps.

## Works well

- Documentation, blogs, news articles, wikis, forums, changelogs and old or
  server-rendered HTML.
- Headings, paragraphs, lists, links, tables, `<pre>` blocks, images and common
  presentational markup.
- Responsive pages that use `@media` width queries, basic flex rows, basic grid
  tracks, simple `calc()` and viewport-unit sizing, aspect-ratio
  `::before`/`::after` boxes, forms
  and simple JavaScript DOM effects.
- Older pages that declare a single-byte charset (Windows-1250/1251/1253/1257
  and ISO-8859-2/5/7 and similar) are decoded correctly from the
  `Content-Type` header or `<meta charset>`.
- Classic bundles that only need platform APIs to exist: `IntersectionObserver`
  and `ResizeObserver` (one synthetic entry per target), `NodeList`,
  `EventTarget`, `CSS.escape`, `customElements`, `ReadableStream`, `Path2D`,
  Web Animations stubs and inert `Worker`s. Dynamic `import()` resolves to an
  empty module namespace instead of failing the whole script.
- Reader mode (`R` or `-reader`) for long articles.

## Often rough

- Single-page applications, module-only sites, heavy shadow DOM, custom
  elements and CSS-in-JS.
- Sites that require a browser check, CAPTCHA, unusual TLS or a specific
  certificate.
- Complex grid/flex layouts, sticky positioning, z-index stacking, full float
  behavior, animations and generated content.
- Visual results from canvas, WebGL, video and WebAudio.
- Multipart/file-upload forms.

## Not supported

- ES modules, workers, service workers, WebSocket, IndexedDB, WebRTC,
  geolocation, clipboard APIs, notifications and browser payment APIs.
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
