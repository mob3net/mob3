# Keyboard and mouse

Press `?` inside MOB³ for the built-in `about:help` page, which is the canonical
key, mouse and form reference for the running build.

## Page navigation

| Key | Action |
| --- | --- |
| `j` / `k` | Scroll one line down / up |
| `Space` / `b` | Scroll one screen down / up |
| `Ctrl-D` / `Ctrl-U` | Scroll half a screen |
| `g` / `G` | Top / bottom of the page |
| `Tab` / `Shift-Tab` | Next / previous link or form control |
| `Enter` | Follow the focused link, submit the focused field, or insert a newline in a textarea |
| `Ctrl-O` / `[` | Back |
| `Ctrl-I` / `]` | Forward (Ctrl-I needs kitty key reporting; legacy terminals send Tab, so `]` is the portable key) |
| `r` | Reload |
| `R` | Toggle reader mode |
| `D` | Toggle dark mode |
| `J` | Toggle JavaScript and reload the page without or with scripts |
| `i` | Cycle graphics modes in place: full graphics, blocks, none (no reload) |
| `+` / `-` | Zoom the page in / out (25% to 200%). Fixed pixel layout, spacing, images and large kitty headings grow; body text keeps its size and line height |
| `0` | Reset zoom to 100% |
| `X` | Open the view options window (Fixed elements, Sticky elements, Background images); `Space`/`Enter` toggles the selected switch |
| `o` / `Ctrl-L` | Open the URL prompt |
| `/` | Find in the page, or in the console HTML tab |
| `n` / `N` | Next / previous find match |
| `f` | Highlight letters in the visible links and follow one by typing them |
| `F` | Same as `f`, but show the destination and wait for `Enter`/`y` confirmation |
| `u` | Show the current URL in the footer |
| `E` | Open the full warning or error message that the footer truncated (the `E` in `Error:` is underlined) |
| `d` | Open the curl download popup for the current PDF page (PDF viewer only; does nothing elsewhere) |
| `y` | Copy selected text, the current URL when nothing is selected, the download curl command, or the whole active console section |
| `Y` | Copy the whole rendered page as plain text (no colors or browser chrome) for pasting into a bug report; the first line is a `#` metadata line with URL, sizes, zoom and modes |
| `Ctrl-Y` | Copy only the page rows visible at the current scroll, with the same metadata line |
| `H` | Session history |
| `?` | Help page |
| `!` | JavaScript console |
| `q` / `Ctrl-C` | Ask to confirm quitting (`y`/`Enter` quits; any other key cancels) |
| `:q` | Exit immediately (in the `:` prompt) |
| `Esc` | Stop loading, leave a field, clear a selection, or show the exit hint |

Back and Forward remember the scroll position you last had on each history
entry. In-page `#` links become history entries too, so `h` returns to where you
were before following the anchor, and Forward returns to the anchored section.
If only the fragment changes, the page is not reloaded.

Zoom changes how many CSS pixels one terminal cell represents, from 25% to
200% in preset steps. Use `-zoom N` at launch to start at another level. It is not text scaling: glyphs stay one cell per
character and lines keep their normal height, so text stays readable while
fixed pixel widths, padding, borders, images and kitty scaled large headings
grow or shrink. The CSS viewport shrinks or grows with the ratio, so responsive
pages reflow and percentage columns keep filling the terminal. Zoom lasts for
the session and applies to every page, including pages restored from the
back/forward cache. The URL row always shows the current level as a clickable
badge at its far right; clicking it opens a window where a preset can be chosen
with the mouse or the arrow keys and `Enter`.

## Console

`!` or the `>_` footer icon opens the console. Inside it, `:` or `Enter` focuses
the JavaScript input row, `Tab`/`Shift-Tab` or `h`/`l` switch sections, `j`/`k` and
`g`/`G` scroll the active section, and `y` copies the whole active section (the
logs, the raw page HTML, the request list, storage, or the Tools scrollback).
`Esc` leaves the input row first and closes the console on the next press; `!`
closes it immediately.

## URL prompt

`o`, `Ctrl-L`, `:` or clicking the URL bar opens the prompt prefilled with the
current URL and selected. Typing replaces the selection; `Backspace` clears it first.
`:` is a Vim-style command prompt: `q` or `:q` plus `Enter` exits immediately,
without the confirmation that `q` asks for. History of earlier URLs and search
terms is kept in memory for the session.

| Key | Action |
| --- | --- |
| `Up` / `Ctrl-P` | Recall the previous URL/search entry |
| `Down` / `Ctrl-N` | Recall the next entry, or the draft |
| `Left` / `Right` | Move one character |
| `Home` / `End` | Start / end |
| `Ctrl-B` / `Ctrl-F` | Move one character |
| `Alt-B` / `Alt-F` | Move one word |
| `Ctrl-Left` / `Ctrl-Right` | Move one word |
| `Alt-Left` / `Alt-Right` | Move one word |
| `Backspace` / `Delete` | Delete around the caret |
| `Alt-Backspace` / `Ctrl-Backspace` | Delete the previous word or URL segment |
| `Ctrl-W` | Delete the previous word or URL segment |
| `Alt-D` / `Alt-Delete` / `Ctrl-Delete` | Delete the next word plus separators |
| `Ctrl-A` | Select all |
| `Ctrl-K` | Delete to the end |
| `Ctrl-U` | Clear |
| `Ctrl-C` / `Esc` | Close without navigating |
| `Enter` | Navigate |

A mouse click places the caret. Shift+drag is left to the terminal for native
selection. Input is resolved as a URL first, then through the `-search`
fallback.

## Find

`/` opens the incremental find bar. Typing searches case-insensitively and
highlights every match. `Enter` confirms and closes the bar; `n`/`N` then move
between matches. `Down`/`Up` also move while the bar is open. `Esc` closes the
bar but keeps highlights; a later `Esc` clears them. If the current match is
inside a link, the footer shows `(f → <resolved URL>)` and `f` follows that
link directly; clear the search to get normal `f` link hints back.

The find bar supports the same word-editing keys as the URL prompt.

## Link hints

Press `f` to follow a link. It marks the links visible in the current viewport
by highlighting letters from the link's own text with a yellow background and
an underline. Form controls are labelled the same way: text fields focus,
checkboxes and radios toggle, a select opens its options, multi-select options
toggle, and buttons activate. The chosen control stays focused for Space/Enter
and arrow or h/j/k/l keys. The page text is never changed. Up to 26 links
the first letter not already used by
another link is highlighted (case-insensitive), so `Documentation` uses `D` and
a later `Dog` uses `o` if `d` is taken. On link-heavy pages a hint whose first
letter is unique still shows only that one letter; hints sharing a first letter
use two adjacent letters, with no gap between them. Type the highlighted letters to follow the link; either case works. Press `F`
instead of `f` to see the resolved destination highlighted in yellow in the
footer and confirm with `Enter` or `y` before navigating; `Esc` or `n`
cancels.
If several labels share a first letter, the others disappear, the typed letter
is no longer highlighted, and only the remaining letters stay marked. Unknown letters are ignored. `Esc`
cancels hint mode, and scrolling with the wheel or scrollbar also dismisses the
hints. Links outside the viewport are never labeled. A link with no text, such
as an image link, gets free label letters drawn inside its box without
brackets. While hints are shown, images are rendered as half-block art so the
terminal's inline image protocol does not cover the hint letters.

## Forms

`Tab`/`Shift-Tab` move through links and form controls in document order.
A focused checkbox, radio, button or link is activated with `Space`/`Enter`;
arrows change radio groups, single selects and ranges, and move the active
option in a multi-select, where `Space` toggles it. On radios, selects and
ranges, `h`/`l` work like Left/Right and `j`/`k` like Down/Up.

Click a text input or textarea to focus it. The focused field shows a block
caret.

| Key | Action |
| --- | --- |
| `Left` / `Right` | Move by character |
| `Home` / `End` | Start / end of line |
| `Up` / `Down` | Move between lines in a textarea |
| `Alt-B` / `Alt-F` | Move by word |
| `Alt/Ctrl+Backspace`, `Ctrl-W` | Delete the previous word |
| `Alt-D` / `Alt-Delete` / `Ctrl-Delete` | Delete the next word |
| `Backspace` | Delete backwards |
| `Delete` | Delete forwards |
| `Ctrl-A` | Select all; typing, `Backspace` or `Delete` replaces the whole value |
| `Ctrl-K` | Delete to the end |
| `Ctrl-U` | Clear |
| `Enter` | Submit (or insert a newline in a textarea) |
| `Esc` | Leave the field |

Checkboxes and radios toggle on click. A `<select>` opens a popup; use
`Up`/`Down` and `Enter`, or click an option. `<select multiple>` renders as a
list box; clicking an option toggles it. A file input opens a notice that
uploads are unsupported.

## Mouse

- Hover a link to show its target in the footer.
- Hovering also applies CSS `:hover` and fires mouse-over/enter events, so
  hover menus open and stay open while moving between padded items; style
  updates wait briefly for the pointer to settle. Leaving the page clears the
  hover state, and wheel scrolling never blocks on a hover rebuild.
- Drag to select page text; `y` copies it through OSC 52. With no
  selection, `y` copies the current URL.
- Click a link to follow it; a download link opens the `curl` popup without fetching, except a `.pdf` download, which opens in the viewer.
- Click the URL bar to open the prompt; click outside to close it.
- Click in the prompt to move the caret.
- Click the zoom badge at the right end of the URL row to open the zoom window
  and choose a preset.
- Click `←`, `→`, `⟳`, `⌂`, History, Bookmarks, Help or `⚙` in the toolbar.
- Right click a link to copy its URL (OSC 52); elsewhere it does nothing.
- The mouse back/forward buttons navigate.
- Click `Dark:on`/`Dark:off`, `Reader:on`/`Reader:off`,
  `JavaScript:on`/`JavaScript:off` or the `>_` icon in the footer.
- The wheel scrolls three lines; drag the right-edge scrollbar to jump.

Shift+mouse reports from the terminal are ignored so the terminal can keep a
native selection over the URL. Terminal-native selection needs the terminal's
Shift override.

## Terminal support

MOB³ enables the kitty keyboard protocol in detected kitty, WezTerm, Ghostty
and foot terminals. That makes `Esc`, modified keys, `Shift-Tab` and key
releases reliable. Multiplexers (tmux, screen, zellij) are excluded because
their forwarding is inconsistent.
