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
| `Tab` / `Shift-Tab` | Next / previous link |
| `Enter` | Follow the focused link, submit the focused field, or insert a newline in a textarea |
| `h` / `Left` | Back |
| `l` / `Right` | Forward |
| `r` | Reload |
| `R` | Toggle reader mode |
| `D` | Toggle dark mode |
| `I` | Cycle graphics modes: full graphics, blocks, none |
| `X` | Hide or show fixed-position elements |
| `o` | Open the URL prompt |
| `/` | Find in the page, or in the console HTML tab |
| `n` / `N` | Next / previous find match |
| `f` | Highlight letters in the visible links and follow one by typing them |
| `F` | Same as `f`, but show the destination and wait for `Enter`/`y` confirmation |
| `u` | Show the current URL in the footer |
| `y` | Copy selected text, or the current URL when nothing is selected |
| `H` | Session history |
| `?` | Help page |
| `!` | JavaScript console |
| `q` / `Ctrl-C` | Ask to confirm quitting (`y`/`Enter` quits; any other key cancels) |
| `Esc` | Stop loading, leave a field, clear a selection, or show the exit hint |

Back and Forward remember the scroll position you last had on each history
entry. In-page `#` links become history entries too, so `h` returns to where you
were before following the anchor, and Forward returns to the anchored section.
If only the fragment changes, the page is not reloaded.

## URL prompt

`o` or clicking the URL bar opens the prompt prefilled with the current URL and
selected. Typing replaces the selection; `Backspace` clears it first.

| Key | Action |
| --- | --- |
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
bar but keeps highlights; a later `Esc` clears them.

The find bar supports the same word-editing keys as the URL prompt.

## Link hints

Press `f` to follow a link. It marks the links visible in the current viewport
by highlighting letters from the link's own text with a yellow background and
an underline. Editable fields are labelled the same way; choosing one focuses
it for typing. The page text is never changed. Up to 26 links the first letter
not already used by
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
- Click a link to follow it; a download link opens the `curl` popup instead.
- Click the URL bar to open the prompt; click outside to close it.
- Click in the prompt to move the caret.
- Click `←`, `→`, `⟳`, `⌂`, History, Bookmarks, Help or `⚙` in the toolbar.
- Right click goes back; the mouse back/forward buttons navigate.
- Click `Dark:on`/`Dark:off`, `Reader:on`/`Reader:off` or the `>_` icon in the
  footer.
- The wheel scrolls three lines; drag the right-edge scrollbar to jump.

Shift+mouse reports from the terminal are ignored so the terminal can keep a
native selection over the URL. Terminal-native selection needs the terminal's
Shift override.

## Terminal support

MOB³ enables the kitty keyboard protocol in detected kitty, WezTerm, Ghostty
and foot terminals. That makes `Esc`, modified keys, `Shift-Tab` and key
releases reliable. Multiplexers (tmux, screen, zellij) are excluded because
their forwarding is inconsistent.
