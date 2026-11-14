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
| `screenshot` | `{scale}` | PNG image of the current page viewport |
| `click` | `{link}` or `{selector}` or `{x,y}` | Snapshot after the click |
| `type` | `{text}` | `typed` |
| `press` | `{key}` | `pressed <key>` |
| `scroll` | `{lines}` or `{pages}` or `{top}` or `{bottom}` | Scroll percentage |
| `back` | `{}` | Snapshot |
| `forward` | `{}` | Snapshot |
| `reload` | `{}` | Snapshot |
| `evaluate` | `{expression}` | JavaScript result as text |
| `history` | `{}` | Session history, most recent first |

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
{"selector":"input[name=q]"}
{"x":4,"y":12}
```

- `link` uses a snapshot link number.
- `selector` uses a CSS selector. Links, inputs, checkboxes and buttons are
  clicked through their rendered cell, so normal browser click behaviour runs.
- `x`/`y` are zero-based page-viewport cells.

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

## Typical agent loop

1. `navigate` a page.
2. `snapshot` to read text and collect link numbers and field coordinates.
3. `click` a link or field.
4. `type` and `press` to fill and submit forms.
5. `snapshot` again after navigation.
6. Use `evaluate` for DOM extraction or `scroll`/`back`/`history` to move.

## Limitations

- **Text only.** There is no pixel screenshot: MOB³ renders to terminal cells
  and has no font rasterizer.
- **One browsing context.** No tabs or iframes.
- **Fixed height** of 30 rows; only `-width` is configurable.
- **JavaScript is a subset.** Module scripts, workers, service workers and
  many modern APIs are unavailable.
- **Timers are not pumped between tool calls.** Content that only appears in a
  later `setTimeout` callback may be missing from the snapshot.
- **Memory only.** Closing the server discards history, cookies and storage.
- **Local, unauthenticated server.** Use it only over a trusted local stdio
  channel.

See [Security and privacy](security.md) for the broader security model.
