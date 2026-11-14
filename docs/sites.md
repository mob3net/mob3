# Site support

MOB³ is a content browser, not a full browser engine. It is happiest with
text-focused, server-rendered pages and gets rough on modern JavaScript apps.

## Works well

- Documentation, blogs, news articles, wikis, forums, changelogs and old or
  server-rendered HTML.
- Headings, paragraphs, lists, links, tables, `<pre>` blocks, images and common
  presentational markup.
- Responsive pages that use `@media` width queries, basic flex rows, basic grid
  tracks, forms and simple JavaScript DOM effects.
- Reader mode (`R` or `-reader`) for long articles.

## Often rough

- Single-page applications, module-only sites, heavy shadow DOM, custom
  elements and CSS-in-JS.
- Sites that require a browser check, CAPTCHA, unusual TLS or a specific
  certificate.
- Complex grid/flex layouts, sticky positioning, z-index stacking, floats,
  animations and generated content.
- Visual results from canvas, WebGL, video and WebAudio.
- Multipart/file-upload forms.

## Not supported

- ES modules, workers, service workers, WebSocket, IndexedDB, WebRTC,
  geolocation, clipboard APIs, notifications and browser payment APIs.
- CSP enforcement, certificate UI and proxy configuration UI.
- Pixel screenshots for automation. MOB³ has no font rasterizer.

## Popular-site shortlist

These are useful for real-world testing. Several are JavaScript apps and will
show fallbacks, consent screens or blocked pages rather than a full render.

1. Google — <https://www.google.com/>
2. YouTube — <https://www.youtube.com/>
3. Facebook — <https://www.facebook.com/>
4. Instagram — <https://www.instagram.com/>
5. X — <https://x.com/>
6. TikTok — <https://www.tiktok.com/>
7. Reddit — <https://www.reddit.com/>
8. Wikipedia — <https://en.wikipedia.org/wiki/Main_Page>
9. Amazon — <https://www.amazon.com/>
10. ChatGPT — <https://chatgpt.com/>
11. WhatsApp Web — <https://web.whatsapp.com/>
12. Yahoo — <https://www.yahoo.com/>
13. Bing — <https://www.bing.com/>
14. LinkedIn — <https://www.linkedin.com/>
15. Netflix — <https://www.netflix.com/>
16. Gmail — <https://mail.google.com/>
17. GitHub — <https://github.com/>
18. Stack Overflow — <https://stackoverflow.com/>
19. BBC News — <https://www.bbc.com/news>
20. CNN — <https://www.cnn.com/>

## Better layout tests

Start with pages that render server-side:

- <https://en.wikipedia.org/wiki/Main_Page>
- <https://github.com/>
- <https://stackoverflow.com/>
- <https://www.bbc.com/news>
- <https://lite.cnn.com/>
- <https://old.reddit.com/>
- <https://text.npr.org/>
- <https://news.ycombinator.com/>

## Tips

- `-no-js` shows the no-script fallback, which is sometimes more useful.
- `R` extracts the main article and removes navigation, sidebars and images.
- `D` asks sites for `prefers-color-scheme: dark` and keeps an author dark
  palette when the page already renders a dark background.
- `-layout-budget low` can keep a very large or generated page responsive.
- `-dump -width 120 -no-color` is handy for comparing layout without a terminal.
