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
| `o` | Open the URL prompt |
| `/` | Find in the page, or in the console HTML tab |
| `n` / `N` | Next / previous find match |
| `u` | Show the current URL in the footer |
| `H` | Session history |
| `?` | Help page |
| `!` | JavaScript console |
| `q` / `Ctrl-C` | Ask to confirm quitting (`y`/`Enter` quits; any other key cancels) |
| `Esc` | Stop loading, leave a field, clear a selection, or show the exit hint |

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
- Drag to select page text; `y` copies it through OSC 52.
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
