# Debugging guide

Part of the MOB3 agent guidance. Start at [AGENTS.md](../../AGENTS.md) for the working
agreement, the non-negotiable constraints and the change protocol.

This is the working recipe for layout, rendering and engine bugs. Keep probes
temporary and remove them before handing work back.

## Reproduce first

- **Reproduce at the user's viewport.** The user runs `make run` in a
  **152×35** terminal, so the interactive layout width is **151 columns**
  (one is reserved for the scrollbar) and the page viewport is 32 rows. A
  layout that looks fine at 177 columns can still be wrong there: a fixed
  1260px page canvas is 158 cells and does not fit, so check the declared
  width against 151 before reading CSS. Wide comparisons are still useful:
  **177×42** is the common second size, and 220 shows column layouts that
  only appear when there is room. A PNG is often the quickest way to see the
  symptom:

  ```sh
  make && bin/mob3 -width 152 -height 35 -png "$URL" > bin/test.png
  ```

  A width sweep is useful for column layouts:
  `for w in 120 152 177 220; do bin/mob3 -width $w -height 42 -png "$URL" > bin/test-$w.png; done`.
  Use a dump or a layout probe instead when that is faster or when text
  ordering is the question; the PNG is a tool, not a required first step.
  Keep the site URL in the shell command only, never in code or docs.
- Save the page and its CSS locally with `curl` into `/tmp`.
- Use `-dump -no-color` to read text and confirm ordering once the visual
  symptom is understood: `./bin/mob3 -dump -width 100 -no-color "$URL"`.
- Use `-layout-budget none` to distinguish a real empty/overlapping layout from
  a budget-truncated one. Use `-graphics blocks` to see image boxes and
  `-graphics none` to inspect alt-label behavior.
- Use `grep`/ripgrep on the saved HTML and downloaded CSS for class names,
  media queries, `display`, `position`, `float`, `grid-*`, `flex*`, `calc(`,
  `transform`, `content`, and `visibility`.

## Contained engine failures

The transport validates every event after the engine produces it. If a frame
fails (for example an image placement outside the frame), the transport swaps
it for a `Crash` event instead of delivering unsafe cell data. A panic in the
engine handler goroutine is caught at the same layer and also becomes a
`Crash`; the event stream then closes, the front end unwinds normally and its
deferred cleanup restores the terminal instead of leaving raw mode on.
The front end
prints the first identical message to stderr, sends it back as a `ClientError`
command, and the engine adds `[engine] ...` to the `!` console and sets
`Error: page engine — see ! console` in the footer. Identical repeats are
suppressed until a frame presents successfully, so a per-frame fault does not
flood the terminal.

MOB3 writes no log file. When the alternate screen hides stderr, capture it
with shell redirection:

```sh
bin/mob3 "$URL" 2>/tmp/mob3.err
```

A crash line is written once and the front end forces a full repaint on the
following frame, so the screen recovers. If a frame cannot be repaired at all,
`!` still opens the console (the key path is independent of page output), and
`q` then `y` still quits. A repaired frame drops only the offending placement,
glyph or scaled run, so toolbar, footer and console remain drawable.

## Zoom probe

Zoom is a layout oracle, not just a convenience: it changes the CSS pixels per
cell without changing text metrics. At 100% a cell is 8×16 CSS px; at 200% a
cell is 4×8 CSS px, so the CSS viewport narrows, fixed-pixel boxes, padding,
borders, absolute offsets and images take more cells, percentages stay relative
to the reflowed viewport, media queries re-evaluate, and glyphs stay one cell
per character on the normal 16px line grid.

Render the same page at several levels before reading CSS:

```sh
for z in 100 150 200; do
  bin/mob3 -zoom $z -width 152 -height 35 -png "$URL" > bin/zoom-$z.png
done
```

Interactive runs use `+`/`-` and `0` for the same probe, which is often faster
when the symptom is below the fold. `-zoom` also works with `-dump` and `-mcp`.

Read the result as a separation test:

- **Invariant across every zoom level** means the cause is a placement bug, not
  a sizing or pixel-ratio artifact: the box's used width can be right while its
  X is wrong. Check `margin:auto` centering, flex `justify-content`/`align-items`
  (including `margin:auto` on a single item), grid placement, the float band,
  the absolute/fixed containing block, and inline versus block classification.
  Compare the fragment's X range with its parent's box; content that stays
  glued to one edge while its parent grows or shrinks is the tell.
- **Changing with zoom** points at a sizing path instead: a fixed-pixel canvas
  that overflows and clips, the image display cap, a percentage that reflows at
  a media breakpoint, or a length resolved too early. A fixed-width canvas is
  expected to hug the left edge (no auto margin) and clip at every zoom level;
  that is correct, not the invariant bug above.
- Half-and-half symptoms usually mean a fixed-pixel child inside a correct
  container: the child keeps its position relative to the container's left
  edge while its own cells change.

Then confirm with a layout probe and, when useful, a Chromium comparison at the
same CSS viewport (`columns × cell width ÷ zoom`). Keep the site URL in the
shell command only.

## Layout probe

The fastest reliable tool is a temporary `internal/engine/zz_debug_test.go`
that builds a real layout and logs only the suspicious nodes:

```go
loader := NewLoader()
page, err := loader.Load(url, nil)
ly := BuildLayout(page.Root, page.Author, width)
```

For a saved HTML file, parse it in the test and pass the same author rules the
loader would collect. Important: **pass `page.Author`**, not `nil`; `nil` hides the
real CSS cascade and produces misleading results.

Log selectively:

- `ly.Truncated`, `len(ly.Frags)`, `len(ly.Links)`, `ly.Height`.
- For a suspicious text fragment: `Kind`, `X`, `Y`, `W`, `H`, `Text`, `Link`,
  then walk `f.Node` parents and log `ly.Styles[n]` (`Display`, `Position`,
  `Float`, `Width`, `Height`, `Margin`, `Padding`, flex/grid fields).
- For links: href plus rect count; check whether card text is inside an anchor
  or relies on an empty overlay anchor.
- For images: whether `f.Img` is nil and whether the visible text is a real
  fragment or an `[image: alt]` label.

Run with `go test -mod=vendor ./internal/engine -run TestZZ -v -count=1` and
pipe through `head`/`grep`. Delete the `zz_*` file afterwards.

## PDF extraction

- Reproduce with `bin/mob3 -dump -width 100 -no-color file.pdf`. The top bar
  shows the page count and the per-page `Page n of N` labels make missing or
  extra pages obvious. `-no-pdf` confirms which loader branch ran.
- Goldens are the fastest comparison: drop a small `.pdf` in
  `internal/engine/testdata/pdf/`, run
  `go test ./internal/engine -run TestPDFFixtures -update`, then review the
  `.txt` diff. Do not commit large or real-world PDFs; keep fixtures small and
  generic.
- Read the note first: “could not be parsed” is a structure problem (xref,
  trailer, filter), “no extractable text” is a font/scan problem, and
  “encrypted” is refused before any page is read.
- A temporary `zz_*` test can call `parsePDF(data)` and log `len(doc.xref)`,
  `doc.trailer`, `pdf.pages()`, and `pdf.extract(page)` — each run carries its
  text, X, Y, W, size and bold/italic flags.
- Text order and spacing problems are usually in `pdfPageLines` /
  `pdfLineSpans`, not the interpreter: check the raw runs before touching the
  content-stream code. Column padding assumes one cell is 6 PDF points
  (`pdfCellPoints`), and only gaps wider than about one cell become padding.
- Word spaces come from two places: a TJ offset wider than about 0.15 em ends
  the run (`pdfExtractor.adjust`), and a gap between runs wider than
  `max(1.0, 0.15*size)` points becomes one space. If words run together, check
  those thresholds before the font decoder; if letters split apart, check the
  per-run advance (`pdfRun.W`).
- Uniform page geometry is `pdfPageBox`: the tallest page sets `PageRows`, the
  widest line sets `PageCells`, `pdfPageHTML` pads every `<pre>` to `PageRows`,
  and `pdfPageWidth` turns `PageCells` into the centered `max-width`.
- Headline classification uses `pdfBodySize` (text-weighted median font size).
  A run at least 1.25× the body size becomes a 32px span (`pdfSpanSize`) and
  counts as two rows in `pdfPageRows`; smaller runs stay at the page's 14px.
  Gap blank lines come from `pdfLeading` (lower quartile of baseline deltas) in
  `pdfPageLines`, and a headline's second row cancels one inserted blank.
- Column detection is `pdfDetectGutter`/`pdfSegments`: run coverage finds a
  low-coverage x-band, and `pdfSplitBand` requires dense text (median line width
  at least 60% of the column span) and no internal table gaps. If a two-column
  page is not reordered, check the coverage threshold and the density gate; a
  figure that crosses the gutter, or mixed sidenotes, keeps the page in flow.
- Duplicate text drawn twice at a sub-point offset (fake bold, shadows) is
  dropped when a run starts more than 3 pt before the previous run's end.
  Right-aligned values can legitimately overlap by about 2.5 pt, so do not
  lower that tolerance without checking the sample corpus.
- The footer page number comes from `session_pdf.go`: `updatePDFPageStarts`
  maps each `.pdf-page` node to its first fragment row after layout, and
  `pdfPageNumber` compares those rows with `b.Scroll`. If the number is wrong,
  check that `PDFDocument.PageNodes` was filled by `loadPDFPage` and that the
  starts are monotonic.

## Instrument only when needed

When the fragment output is not enough, add temporary `println` probes inside
`blockLayout`, `layoutAbsolute`, `flexRow` or `grid`, logging `y`, `availW`,
`contentW`, `contentH`, `totalH`, flex widths and placements. Remove every
probe before finishing.

## Chromium cross-check

If Chromium is installed locally, use it as a visual and computed-style oracle.
It is a debugging tool only, never a project dependency.

- Screenshot at the same CSS viewport as the terminal:
  `chromium --headless=new --no-sandbox --disable-gpu --hide-scrollbars
  --window-size=$((columns*8)),$((rows*16)) --screenshot=/tmp/page.png "$URL"`.
- Render the same viewport with
  `./bin/mob3 -png -width "$columns" -height "$rows" "$URL" > /tmp/mob3.png`.
- For computed styles and real rectangles without Puppeteer, start
  `chromium --headless=new --no-sandbox --disable-gpu
  --remote-debugging-port=9222 --user-data-dir=/tmp/chrome-debug
  --window-size=... about:blank`, open a target with
  `PUT /json/new?<encoded-url>`, then connect to its
  `webSocketDebuggerUrl` from Node's global `WebSocket` and use
  `Page.enable` plus `Runtime.evaluate` to return
  `getBoundingClientRect()` and `getComputedStyle()` values.
- Keep the Node script in `/tmp`; it is not part of the repository.
- Compare structure, media-query results, padding reserved for fixed headers,
  and whether nav items sit on one line. Do not expect pixel parity: Chromium
  uses real CSS pixels and fonts, while MOB3 uses terminal cells.

## Timing and limits

Use a temporary probe that times `BuildLayout` at 80/100/120 columns and logs
`ly.Truncated`. If a page is slow, suspect selector matching and layout limits
before network/JS. Compare `-layout-budget none` with the default run.

## Pitfalls learned

- `Style.Height` is the CSS declaration, not the laid-out box height. Use
  fragments and rects to reason about real geometry.
- `awk '{ print length($0) }'` counts bytes; use a short Python `len(line)` on
  decoded text for terminal columns.
- Long `[image: alt]` labels can overlap columns when images are disabled. That
  is not necessarily a layout bug; check whether enabling images removes it.
- Hidden inherited from `html`/`body` is an anti-FOUC pattern and is ignored;
  an explicitly hidden box is still hidden.
- `display:contents` and float scoping are approximations: check the computed
  styles and the layout path (`flow`, `flexRow`, `grid`, `table`) before
  changing the cascade.
- A layout symptom can disappear at narrow widths because flex/grid columns
  wrap; if a dump at 100 columns looks fine, render a wide PNG before assuming
  the bug is elsewhere.
- Do not name specific sites in tests, comments, docs or commit messages.

## Validate

After the fix: run the focused test, then `go test -mod=vendor ./...`,
`go vet -mod=vendor ./...`, `gofmt` on touched Go files, and
`git diff --check`. Build with `make` when CLI or binary behavior changed and
exercise the changed path with a dump, PNG or focused test.
