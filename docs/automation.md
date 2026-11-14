# Automation with MCP

`mob3 -mcp` runs MOB³ headlessly as a
[Model Context Protocol](https://modelcontextprotocol.io/) server over
stdin/stdout. It exposes the browser to scripts and AI agents through a small
set of text-oriented tools.

```sh
mob3 -mcp
mob3 -mcp -width 100 about:home
mob3 -mcp -width 120 https://en.wikipedia.org/
```

- Optional starting URL; otherwise `about:home`.
- `-width` controls wrapping and coordinates.
- The virtual viewport is 30 rows by default; `-height N` sets it explicitly
  (about `N-3` page rows after chrome).
- Images are disabled.
- `-no-js`, `-layout-budget`, `-reader`, `-dark`, bookmarks and search settings
  are honored.
- History, cookies and storage are in memory only.

## Client configuration

Claude Desktop, Cursor and other MCP clients use a `mcpServers` entry:

```json
{
  "mcpServers": {
    "mob3": {
      "command": "/path/to/mob3",
      "args": ["-mcp", "-width", "100", "about:home"]
    }
  }
}
```

MCP communication is stdio-only. Stdout carries protocol messages only;
diagnostics go to stderr. Do not expose the process as a network service; there
is no authentication.

## Protocol

Messages are newline-delimited JSON-RPC 2.0.

```json
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}
{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}
{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"navigate","arguments":{"url":"https://example.com/"}}}
```

Supported methods:

- `initialize` — returns `capabilities.tools`, `serverInfo` and short usage
  instructions.
- `ping` — empty result.
- `tools/list` — tool names, descriptions and JSON schemas.
- `tools/call` — runs one tool.
- `notifications/initialized` and other notifications — acknowledged by
  silence.

Tool failures are returned as successful JSON-RPC responses whose `content`
explains the error and whose `isError` field is `true`. Protocol errors use
standard JSON-RPC error codes.

You can test the server without an MCP client:

```sh
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"navigate","arguments":{"url":"https://example.com/"}}}' \
  | ./bin/mob3 -mcp
```

## Tools

| Tool | Arguments | Returns |
| --- | --- | --- |
| `navigate` | `{url}` | Snapshot after loading |
| `snapshot` | `{}` | Current page viewport, links and form controls |
| `links` | `{selector?}` | Every link with index, text, href and selector |
| `screenshot` | `{scale}` | PNG image of the current page viewport |
| `click` | `{link}` or `{text}` or `{selector}` or `{x,y}` | Snapshot after the click |
| `form` | `{selector?}` | Every form and control with CSS selectors and values |
| `fill` | `{selector,value}` or `{fields}` | `filled N field(s)` |
| `select` | `{selector,value}` or `{values}` or `{text}` or `{index}` | `selected …` |
| `toggle` | `{selector,checked?}` | `… is checked` / `… is unchecked` |
| `submit` | `{selector?}` | Snapshot after navigation |
| `wait` | `{ms}` | `waited <ms>ms` |
| `wait_for` | `{selector?}` and/or `{text?}`, `{timeout_ms?}` | `found … after <ms>` |
| `type` | `{text}` | `typed` |
| `press` | `{key}` | `pressed <key>` |
| `scroll` | `{lines}` or `{pages}` or `{top}` or `{bottom}` | Scroll percentage |
| `back` | `{}` | Snapshot |
| `forward` | `{}` | Snapshot |
| `reload` | `{}` | Snapshot |
| `evaluate` | `{expression}` | JavaScript result as text |
| `history` | `{}` | Session history, most recent first |
| `requests` | `{limit?}` | Current page's request log and cache usage |
| `logs` | `{limit?}` | JavaScript console messages and errors |
| `source` | `{selector?}` | Raw decoded HTML, or one element's serialized HTML |

### `navigate`

Resolves input like the URL prompt: URLs, local paths, `about:` pages, or a
search query through `-search`. Waits for the page, meta refreshes and scripted
redirects.

### `snapshot`

Returns the current page viewport as plain text:

```text
URL: https://example.com/
Title: Example Domain
Viewport: 100x27  Scroll: 0%

Example Domain
...

Links:
[1] More information... -> https://iana.org/domains/example

Inputs:
- input name="q" type="search" value="" at (4,12)
```

- `[n]` is a 1-based link index for `click({link:n})`.
- Only visible links are listed. Link numbers are stable layout indices, so a
  filtered list may skip numbers.
- `Inputs` positions are zero-based page-viewport cells, matching `click`.
- Page text is the viewport, not the whole document. Use `scroll`.

### `links`

```json
{}
{"selector":"main a"}
```

Lists every rendered anchor in the document, including links outside the
current viewport, with the same `[n]` index that `snapshot` shows and `click`
accepts:

```text
Links: 2
[1] Docs -> https://example.com/docs selector="a[href=\"/docs\"]"
[2] Sign in -> https://example.com/login selector="#nav a:nth-child(2)"
```

Empty anchors, hidden anchors and anchors with no rendered content are
omitted. `selector` filters which anchors are listed (it matches the anchor,
not its containers). Listings are capped at 200 entries.

### `form`

```json
{}
{"selector":"form#search"}
```

Lists every form and its controls, including fields that are outside the
current viewport. Each control shows its tag, name, type, current value or
checked state, flags (`required`, `readonly`, `disabled`) and a stable CSS
selector for the other form tools:

```text
Forms: 1
[1] form method="GET" action="/find" selector="#search"
  - input name="q" type="text" value="old" selector="#search input[name=\"q\"]"
  - select name="lang" value="fr" options=["en" "fr"] selector="select[name=\"lang\"]"
  - input name="safe" type="checkbox" state=checked value="on" selector="#search input[name=\"safe\"]"
```

Hidden inputs are omitted because they cannot be edited. Controls outside any
form are listed separately. Selectors prefer an `#id`, then a
`form-id name` pair, then an `nth-child` path. Pass a form or control selector
to list just that form.

### `fill`

```json
{"selector":"#search input[name=q]","value":"terminal browsers"}
{"fields":{"#search input[name=q]":"terminal browsers","textarea[name=notes]":"a note"}}
```

`fill` sets text-like inputs and textareas, replacing any existing value; an
empty string clears the field. It accepts one selector/value pair or a `fields`
map of selector to value. Values honor `maxlength`, and `range` inputs accept a
number. Checkboxes, radios and selects are rejected: use `toggle` and `select`.
`fill` never submits and never requires focus, so it also works on off-screen
fields. Each change fires `input` and `change`.

### `select`

```json
{"selector":"select[name=lang]","value":"fr"}
{"selector":"select[name=tags]","values":["a","b"]}
{"selector":"select[name=lang]","text":"French"}
{"selector":"select[name=lang]","index":1}
```

Chooses by option value, visible label, or zero-based option index. Pass
`values` for a `<select multiple>` to replace the selected set. A missing value
or an out-of-range index is a tool error that lists the available options.

### `toggle`

```json
{"selector":"input[name=remember]"}
{"selector":"input[name=remember]","checked":true}
{"selector":"option[value=b]","checked":true}
```

Checks or unchecks a checkbox or radio. Omit `checked` to flip the current
state. Selecting a radio unchecks the other radios in its group, including
groups outside a form. Multi-select options can also be toggled this way. Use
`select` to choose a single-select option or replace a whole multiple selection.

### `submit`

```json
{}
{"selector":"form#search"}
```

Submits a form: runs the supported constraint validation, fires the form's
`submit` handler, builds the GET query or urlencoded POST body, navigates, and
returns a snapshot. Without a selector it uses the focused control's form, or
the page's only form. Validation failures, invalid actions and
`preventDefault()` are returned as tool errors (validation also focuses the
first invalid field), so the page does not change on failure.

### `wait` and `wait_for`

```json
{"ms":500}
{"selector":"#results","timeout_ms":5000}
{"text":"No results found","timeout_ms":2000}
{"selector":".item","text":"price","timeout_ms":5000}
```

JavaScript timers do not run on their own between tool calls. `wait` advances
the page for a number of milliseconds (default 100, maximum 10000), running due
timers, applying the DOM rebuilds they cause and waiting for navigations they
start.

`wait_for` polls until a CSS selector matches an element and/or the given text
appears in the rendered page. Text matching is case-insensitive, ignores
markup and scripts, and sees the laid-out document rather than just the
viewport. It returns `found … after <ms>` or fails with a timeout error; the
default timeout is 5000ms and the maximum is 30000ms. When both `selector` and
`text` are supplied, both must match.

Both tools return a short confirmation rather than a snapshot; call `snapshot`
afterwards when you need the page contents.

### `screenshot`

```json
{"scale":1}
```

Returns an MCP `image/png` content block of the current page viewport, plus a
short text line with the URL. `scale` 1 uses 8×16 pixel cells; `scale` 2
doubles the image. This is a terminal-cell raster, not a browser pixel
screenshot: Go Mono glyphs are used, CJK/emoji glyphs may be missing, and MCP
disables image downloads so image regions show alt text.

### `click`

```json
{"link":1}
{"text":"Sign in"}
{"selector":"input[name=q]"}
{"x":4,"y":12}
```

- `link` uses a snapshot or `links` link number, including off-screen links.
- `text` clicks the most specific rendered element whose visible text matches,
  case-insensitively. Exact matches are preferred, then links and buttons over
  containers. It also matches submit-button `value` text and `aria-label`/`alt`
  fallbacks.
- `selector` uses a CSS selector. Links, inputs, checkboxes and buttons are
  clicked through their rendered cell, so normal browser click behaviour runs.
  A selector click scrolls an off-screen element into view first.
- `x`/`y` are zero-based page-viewport cells.
- Pass exactly one of the four targets.

### `type` and `press`

`type` requires a focused input, textarea or the URL prompt. Click a field
first. A newline presses Enter; a tab presses Tab.

`press` accepts `Enter`, `Tab`, `Escape`, `Backspace`, `Delete`, arrows,
`Home`, `End`, `PageUp`, `PageDown` and `Space`, plus single characters.

### `evaluate`

Runs JavaScript in the page realm through goja:

```json
{"expression":"document.title"}
{"expression":"JSON.stringify([...document.querySelectorAll('h2')].map(h => h.textContent))"}
```

Objects stringify as `[object Object]`; use `JSON.stringify` for structured
results. `-no-js` disables evaluation.

### `requests`, `logs` and `source`

```json
{"limit":50}
{"limit":20}
{"selector":"main article"}
```

These expose the same diagnostics as the in-browser `!` console:

- `requests` lists the current page's in-memory request log — status, method,
  size, kind (`doc`, `css`, `js`, `img`, `fetch`, `xhr`) and URL — followed by
  cache bytes used. `limit` keeps the newest entries (default 100; 0 shows all,
  up to the 512-entry cap).
- `logs` returns JavaScript console messages and uncaught errors in order, with
  `· error` in the header when the page has a script error. `limit` keeps the
  newest entries (default 200; 0 shows all retained entries). Logs and the
  error flag reset when a new page is applied and are lost on failed loads.
- `source` returns the raw decoded HTML that was fetched, before scripts ran.
  With `selector`, it returns the serialized HTML of that element instead.
  Terminal control characters are removed. Large sources may be clipped by the
  MCP transport.

### `history`

Lists session history, most recent first; `*` marks the current entry.

## Typical agent loop

1. `navigate` a page.
2. `snapshot` to read text and collect visible link numbers; use `links` when
   the target link is below the fold.
3. `form` to get every control's selector and current value.
4. `fill`, `select` and `toggle` to set fields without focusing or scrolling.
5. `submit` to send the form, then read the returned snapshot.
6. Use `wait_for` after clicks and submissions that load content with timers,
   then `snapshot` when ready.
7. Use `click`/`type`/`press` for special widgets, `evaluate` for DOM
   extraction, or `scroll`/`back`/`history` to move.
8. When a page loads wrong, check `requests` for blocked or missing resources,
   `logs` for script errors, and `source` for the markup that was fetched.

## Limitations

- **Text only.** There is no pixel screenshot: MOB³ renders to terminal cells
  and has no font rasterizer.
- **One browsing context.** No tabs or iframes.
- **Fixed height** of 30 rows; only `-width` is configurable.
- **JavaScript is a subset.** Module scripts, workers, service workers and
  many modern APIs are unavailable.
- **Forms.** `fill`, `select`, `toggle` and `submit` handle text fields,
  textareas, checkboxes, radios and selects. File uploads and multipart forms
  are not supported, and framework widgets that rerender from their own state
  may overwrite a value set directly.
- **Timers advance only during loads and waits.** JavaScript timers run while a
  page loads and during `wait`/`wait_for`; a timer that fires later needs one of
  those tools before its content appears. Unrelated tool calls do not pump
  timers.
- **Memory only.** Closing the server discards history, cookies and storage.
- **Local, unauthenticated server.** Use it only over a trusted local stdio
  channel.

See [Security and privacy](security.md) for the broader security model.
