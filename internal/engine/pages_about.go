package engine

import (
	"strconv"
	"strings"
)

const helpPageBody = `<h1>MOB3 Help</h1>
<p class="muted">Version <b>{{VERSION}}</b> · a small terminal browser. Keys are vim-first.</p>
<p class="muted">MOB3 keeps history, cookies, storage and caches in memory only.
Nothing is written to disk.</p>

<h2>Command line</h2>
<p><b>mob3 [flags] [url]</b> opens the start page with no arguments.</p>
<table>
<tr><th>Option</th><th>Description</th></tr>
<tr><td><b>-dump</b></td><td>render the page to stdout and exit</td></tr>
<tr><td><b>-png</b></td><td>write a PNG screenshot of the dump to stdout (implies -dump)</td></tr>
<tr><td><b>-width</b> int</td><td>layout width in columns (default: terminal width or 100)</td></tr>
<tr><td><b>-height</b> int</td><td>layout viewport height in rows and output limit; 0 uses the full page (dump/PNG/MCP)</td></tr>
<tr><td><b>-pdf-page</b> string</td><td>PDF page for dump/PNG: auto, all, or a page number; -png defaults to page 1 (default "auto")</td></tr>
<tr><td><b>-graphics</b> string</td><td>image mode: none, auto, kitty, iterm or blocks (default "none")</td></tr>
<tr><td><b>-g</b> string</td><td>alias for -graphics</td></tr>
<tr><td><b>-no-js</b></td><td>disable page JavaScript</td></tr>
<tr><td><b>-no-pdf</b></td><td>disable the built-in PDF text viewer and show the download prompt instead</td></tr>
<tr><td><b>-reader</b></td><td>start in reader mode</td></tr>
<tr><td><b>-dark</b></td><td>start in dark mode</td></tr>
<tr><td><b>-ignore-blocks</b></td><td>load anti-bot/CAPTCHA pages instead of the blocked page</td></tr>
<tr><td><b>-home</b> string</td><td>home URL opened at startup and by the Home button (default about:home)</td></tr>
<tr><td><b>-mem-limit</b> int</td><td>soft process memory limit in MB; also sets the 25% JS, 12% image, 3% SVG, 3% cache and 12% history budgets, 0 leaves it unlimited and honors GOMEMLIMIT (default 0)</td></tr>
<tr><td><b>-layout-budget</b> string</td><td>style/layout safety limits: low, standard, high or none (default "standard")</td></tr>
<tr><td><b>-zoom</b> string</td><td>layout zoom percent from 25 to 200, with or without % (default "100"); + and - step the presets</td></tr>
<tr><td><b>-text-scale</b> string</td><td>kitty scaled text: auto or off (default "auto")</td></tr>
<tr><td><b>-color</b></td><td>force ANSI color when dumping</td></tr>
<tr><td><b>-no-color</b></td><td>disable ANSI colors in dump mode</td></tr>
<tr><td><b>-ua</b> string</td><td>custom User-Agent (default MOB3/1.0 (+terminal; +https://mob3.net))</td></tr>
<tr><td><b>-search</b> string</td><td>search URL template with %s (default DuckDuckGo; empty disables)</td></tr>
<tr><td><b>-certs</b> string</td><td>TLS trust store: system or embedded (default "system")</td></tr>
<tr><td><b>-mcp</b></td><td>run the MCP automation server on stdin/stdout</td></tr>
<tr><td><b>-version</b></td><td>print the version and exit</td></tr>
<tr><td><b>-h</b>, <b>-help</b></td><td>print the flags and exit</td></tr>
</table>
<p class="muted">The start page's search box works like the <b>o</b> prompt.
The URL prompt recalls earlier URLs and searches with <b>Up</b>/<b>Down</b> or
<b>Ctrl-P</b>/<b>Ctrl-N</b>.</p>

<h2>Keyboard</h2>
<table>
<tr><th>Key</th><th>Action</th></tr>
<tr><td><b>j</b> / <b>k</b></td><td>scroll down / up one line</td></tr>
<tr><td><b>Space</b> / <b>b</b></td><td>scroll down / up one screen</td></tr>
<tr><td><b>d</b> / <b>u</b></td><td>scroll down / up half a page (Vimium-style aliases for <b>Ctrl-D</b>/<b>Ctrl-U</b>)</td></tr>
<tr><td><b>Ctrl-D</b> / <b>Ctrl-U</b></td><td>scroll half a screen</td></tr>
<tr><td><b>g</b> / <b>G</b></td><td>top / bottom of page</td></tr>
<tr><td><b>Tab</b> / <b>Shift-Tab</b></td><td>next / previous link or form control</td></tr>
<tr><td><b>Enter</b></td><td>follow a link, submit a field, or insert a textarea newline</td></tr>
<tr><td><b>Ctrl-O</b> / <b>[</b></td><td>back</td></tr>
<tr><td><b>Ctrl-I</b> / <b>]</b></td><td>forward; Ctrl-I needs kitty key reporting, legacy terminals send Tab</td></tr>
<tr><td><b>r</b> / <b>Ctrl-R</b></td><td>reload</td></tr>
<tr><td><b>R</b></td><td>toggle reader mode</td></tr>
<tr><td><b>D</b></td><td>toggle dark mode</td></tr>
<tr><td><b>J</b></td><td>toggle JavaScript and reload without or with scripts</td></tr>
<tr><td><b>i</b></td><td>cycle graphics: full graphics, blocks, none</td></tr>
<tr><td><b>+</b> / <b>-</b></td><td>zoom the page in / out (25% to 200%); fixed pixel layout, spacing, images and large kitty headings grow, body text keeps its size</td></tr>
<tr><td><b>0</b></td><td>reset zoom to 100%</td></tr>
<tr><td><b>Z</b></td><td>open the zoom window to choose a preset with the mouse, arrow keys, mouse wheel or pointer hover (the badge at the right end of the URL row does the same with the mouse)</td></tr>
<tr><td><b>X</b></td><td>open the view options window (Fixed elements, Sticky elements, Background images); the mouse wheel, pointer hover or arrow keys move the highlight, and <b>Space</b>/<b>Enter</b> toggles the selected switch</td></tr>
<tr><td><b>o</b> / <b>Ctrl-L</b> / <b>:</b></td><td>open a URL prompt; <b>:</b> also accepts <b>q</b> to quit immediately</td></tr>
<tr><td><b>/</b> / <b>Ctrl-F</b></td><td>find in the page, or in the console HTML tab</td></tr>
<tr><td><b>f</b></td><td>highlight letters in the visible links, then type them to follow a link</td></tr>
<tr><td><b>F</b></td><td>same as <b>f</b>, but opens the chosen link in a background tab after the current one</td></tr>
<tr><td><b>p</b></td><td>preview the chosen link: <b>Enter</b> follows, <b>y</b> copies, <b>Esc</b> cancels</td></tr>
<tr><td><b>Enter</b> / <b>n</b></td><td>close the find bar / next find match afterwards</td></tr>
<tr><td><b>Down</b> / <b>N</b></td><td>next match in the find bar / previous match after closing it</td></tr>
<tr><td><b>S</b></td><td>open the curl download popup for the current PDF (PDF viewer only; does nothing elsewhere)</td></tr>
<tr><td><b>y</b></td><td>copy selected text, the current URL when nothing is selected, the download curl command, or the active console section</td></tr>
<tr><td><b>Y</b></td><td>copy the whole rendered page as plain text, without colors or browser chrome, for pasting into a bug report; the first line carries URL, sizes, zoom and modes</td></tr>
<tr><td><b>Ctrl-Y</b></td><td>copy only the page rows visible at the current scroll, with the same metadata line</td></tr>
<tr><td><b>H</b> / <b>Ctrl-H</b></td><td>session history overlay; Enter restores an entry</td></tr>
<tr><td><b>t</b> / <b>Ctrl-T</b></td><td>open a new tab at the end; the URL prompt is focused and empty</td></tr>
<tr><td><b>W</b> / <b>Ctrl-W</b></td><td>close the active tab and return to the previously used tab; closing the last tab exits without confirmation, and the URL prompt keeps Ctrl-W for word delete</td></tr>
<tr><td><b>Ctrl-N</b> / <b>Ctrl-P</b></td><td>next / previous tab, stopping at the first and last; <b>Alt-Right</b> / <b>Alt-Left</b> work too</td></tr>
<tr><td><b>1</b>-<b>9</b> / <b>Alt-1</b>-<b>Alt-9</b></td><td>select a tab by its number in the tab bar</td></tr>
<tr><td><b>E</b></td><td>open the full warning or error message that the footer truncated; the E in Error: is underlined</td></tr>
<tr><td><b>?</b></td><td>open this help page in a new tab</td></tr>
<tr><td><b>!</b></td><td>JavaScript console / errors</td></tr>
<tr><td><b>Esc</b></td><td>stop loading or leave the current mode</td></tr>
<tr><td><b>q</b> / <b>Ctrl-C</b></td><td>quit after confirmation; <b>Esc</b> or any click cancels</td></tr>
</table>
<p class="muted">Kitty, WezTerm, Ghostty and foot get enhanced key reporting automatically.</p>

<h2>Tabs</h2>
<p><b>t</b>/<b>Ctrl-T</b> opens a tab and focuses an empty URL prompt. New tabs from
<b>t</b>, the <b>+</b> button and help are added at the end; a link
opened in a background tab (<b>F</b> or middle click) is inserted directly after
the current tab.
<b>W</b>/<b>Ctrl-W</b>
closes the active tab and returns to the tab used before it; closing the last
tab exits without the quit
confirmation. <b>Ctrl-N</b>/<b>Ctrl-P</b> or
<b>Alt-Right</b>/<b>Alt-Left</b> move next and previous without wrapping, and
<b>1</b>-<b>9</b> or <b>Alt-1</b>-<b>Alt-9</b> select a number from the bar.
While an overlay, dialog, hint or the console is open it owns
the keyboard and pointer; close it before switching or closing tabs. The URL
prompt or the find bar hands a click on another chrome control to that control
in the same click.
Each tab keeps its own history, scroll, focus and overlays, while cookies follow
normal domain rules across tabs, <b>localStorage</b> is shared by tabs on the
same origin, and <b>sessionStorage</b> is per tab and origin. Graphics
mode, zoom and the memory readout are browser-wide. A background tab keeps
loading and updates its title, but its timers and image work are paused and it
never draws. A crashed tab stays marked <b>Crashed</b> and can be closed without
stopping the others.</p>

<h2>Find</h2>
<p><b>/</b> or <b>Ctrl-F</b> opens the find bar. Typing searches case-insensitively; <b>Enter</b>
closes it and <b>n</b>/<b>N</b> move between matches; the footer shows the
match position and the <b>n</b>/<b>N</b> key hint. <b>Esc</b> closes it; a
second <b>Esc</b> clears the highlights. In the console HTML tab, <b>/</b> or
<b>Ctrl-F</b> searches the raw source. After the bar closes, <b>f</b> keeps its
normal link-hint behavior.</p>

<h2>Link hints</h2>
<p>Press <b>f</b> to follow a link: it highlights a letter from each visible
link, and typing it follows that link. Form controls are highlighted too:
choosing a text field focuses it, checkboxes and radios toggle, a select opens
its options, multi-select options toggle, and buttons activate. The chosen
control stays focused for follow-up keys. Links sharing a
first letter use two adjacent letters. <b>F</b> opens the chosen link in a
background tab without leaving the current page. <b>p</b> previews the
destination and waits: <b>Enter</b> follows, <b>y</b> copies, and
<b>Esc</b>/<b>n</b> cancels. <b>Esc</b> or scrolling dismisses the hints.</p>

<h2>Reader mode</h2>
<p><b>R</b> toggles a clean article view and stays on across pages. Start with
<b>-reader</b> to enable it at launch.</p>

<h2>Dark mode</h2>
<p><b>D</b> toggles a dark palette and asks sites for
<b>prefers-color-scheme: dark</b>. Start with <b>-dark</b> to enable it at
launch.</p>

<h2>JavaScript</h2>
<p><b>J</b> or clicking the footer <b>JavaScript:on</b>/<b>JavaScript:off</b>
label toggles scripting for the rest of the session and reloads the page, so
the no-script fallback is shown immediately. Start with <b>-no-js</b> to begin
with scripting disabled. The current state stays visible in the footer;
<b>JavaScript:Error</b> means a script failed. A page's load-time scripts share
an 8-second budget and a 2,000-script cap; the running script is interrupted and
the rest are skipped with a notice in the <b>!</b> console when either is
reached. Press <b>Esc</b> or <b>Ctrl-C</b> to interrupt a script that is blocking
the terminal.</p>

<h2>History</h2>
<p><b>H</b> or <b>Ctrl-H</b> opens a browser-wide history overlay,
newest first. Use <b>j</b>/<b>k</b> or <b>Up</b>/<b>Down</b> to move,
<b>Enter</b> or <b>Space</b> to open the selected page in the current tab, and
<b>Esc</b>/<b>q</b> to close; clicking a row works too. Each tab keeps its own
<b>Back</b>/<b>Forward</b> stack and scroll position. <b>#</b> anchors and scroll
positions are kept. Up to ten visited pages stay in an in-memory back/forward
cache, so <b>Back</b> and <b>Forward</b> restore a page, its JavaScript state and
your scroll position without reloading it. The rendered <b>about:history</b> page
is still available by URL. Nothing is saved after MOB3 exits. Script and
meta-refresh navigations stop after 20 automatic hops with
<b>Redirect loop stopped</b>; the count clears when you type, click, scroll or
navigate, so a page that redirects to itself cannot loop forever. Each tab
keeps at most 500 back/forward entries.</p>

<h2>Config</h2>
<p>The <b>⚙</b> icon or <b>about:config</b> opens a settings window over the
current page. Use <b>↑</b>/<b>↓</b> to choose a row, <b>Enter</b> or
<b>Space</b> to toggle a checkbox or cycle a choice, <b>Enter</b> to edit a
text value, and <b>Esc</b> to close. Changes apply immediately to this session
only; startup and dump-only options are not shown.</p>

<h2>Mouse</h2>
<table>
<tr><th>Input</th><th>Action</th></tr>
<tr><td>hover a link</td><td>show its target in the footer</td></tr>
<tr><td>drag over page text</td><td>select it; <b>y</b> copies and clears it</td></tr>
<tr><td>click a link</td><td>follow it; a download link shows the curl popup, except a .pdf download, which opens in the viewer</td></tr>
<tr><td>middle-click a link</td><td>open it in a background tab after the current one</td></tr>
<tr><td>click a text field</td><td>focus and edit it</td></tr>
<tr><td>click the URL bar</td><td>open the URL prompt</td></tr>
<tr><td>click a tab</td><td>switch to it; middle-click closes it</td></tr>
<tr><td>click a tab's x or the + button</td><td>close that tab or open a new one</td></tr>
<tr><td>click &lt; / &gt;</td><td>scroll the tab strip when tabs do not fit</td></tr>
<tr><td>click outside the URL prompt</td><td>close it</td></tr>
<tr><td>click in the URL prompt</td><td>place the caret</td></tr>
<tr><td>click the zoom badge in the URL bar</td><td>open the zoom window and choose a preset with the mouse; the current level is always shown</td></tr>
<tr><td>Shift+drag over the URL</td><td>select it with the terminal</td></tr>
<tr><td>click &#8592; / &#8635; / ?</td><td>back / reload / help in a new tab; while a page loads, &#8635; becomes &#215; and clicking it cancels the load</td></tr>
<tr><td>right click a link</td><td>preview its URL; <b>Enter</b> follows, <b>y</b> copies, <b>Esc</b> cancels</td></tr>
<tr><td>side back/forward button</td><td>go back / forward</td></tr>
<tr><td>click the footer status message</td><td>open the full warning or error text, like <b>E</b></td></tr>
<tr><td>click the footer <b>Graphics:…</b> label</td><td>cycle graphics mode, like <b>i</b></td></tr>
<tr><td>click the footer <b>JavaScript:on</b>/<b>JavaScript:off</b> label</td><td>toggle JavaScript and reload the page, like <b>J</b></td></tr>
<tr><td>click the footer <b>Dark:on</b>/<b>Dark:off</b> label</td><td>toggle dark mode</td></tr>
<tr><td>click the footer <b>Reader:on</b>/<b>Reader:off</b> label</td><td>toggle reader mode</td></tr>
<tr><td>click the footer &gt;_ icon</td><td>open or close the JS console</td></tr>
<tr><td>click the <b>×</b> icon</td><td>close the console from its top-right corner</td></tr>
<tr><td>click a console tab</td><td>switch console sections; <b>y</b> copies the active section</td></tr>
<tr><td>click the console input row</td><td>focus the JavaScript or Tools input</td></tr>
<tr><td>click the ⚙ icon</td><td>open the settings window</td></tr>
<tr><td>click the × or the dimmed backdrop</td><td>close the modal</td></tr>
<tr><td>wheel or hover over a select, zoom or view window</td><td>move the highlighted option or row</td></tr>
<tr><td>wheel</td><td>scroll</td></tr>
<tr><td>click/drag the right-edge scrollbar</td><td>jump through a long page</td></tr>
</table>

<h2>Downloads</h2>
<p>MOB3 never saves files. Download links and non-HTML responses open a popup
with a <b>curl</b> command; <b>y</b> copies it, and <b>Esc</b>/<b>Enter</b>/<b>q</b>
closes it.</p>

<h2>PDF documents</h2>
<p>A PDF opens in a built-in, text-only viewer: every page is extracted into
one continuous document where each page ends with its <b>Page n</b> caption above
an underscore rule. The reading column is centered, every page is padded to the
same height so it scrolls like a book, two-column prose is read column by
column, vertical gaps become blank lines, and runs larger than the body text
become 32px headlines (two rows, scaled glyphs in kitty). Columns keep their
alignment. The footer shows <b>Page n/N</b> with a clickable
<b>(S -&gt; Download name)</b> hint; <b>S</b> opens the same curl popup. Find,
selection, <b>-dump</b> and <b>-png</b> work as on any other page. Figures,
images and annotations are not rendered, scanned PDFs have no text to show,
and encrypted PDFs are refused. <b>-no-pdf</b> disables the viewer and shows
the download popup instead.</p>

<h2>Forms</h2>
<p>Click inside a text field or textarea to focus it and place the caret at the
clicked character; click the input row of a JavaScript prompt dialog the same
way. Click a field to edit, or use <b>Tab</b>/<b>Shift-Tab</b> to move through
links and form controls. <b>Ctrl-A</b> selects the whole value; typing,
<b>Backspace</b> or <b>Delete</b> replaces it, and moving the caret cancels the
selection. <b>Ctrl-U</b> clears the field, <b>Ctrl-K</b> deletes to the end,
and <b>Ctrl-B</b>/<b>Ctrl-F</b>, <b>Alt-B</b>/<b>Alt-F</b> and <b>Ctrl-W</b>
edit by word. <b>Enter</b> submits, <b>Esc</b> leaves, <b>Space</b>
or <b>Enter</b> activates a focused checkbox, radio, select, button or link,
and arrows change radio groups, selects and ranges (<b>h</b>/<b>l</b> and
<b>j</b>/<b>k</b> also work there). Checkboxes,
radios, selects, date/time/month/week, range and color fields work. GET and
urlencoded POST forms submit; multipart and file uploads are not supported.</p>

<h2>Try it</h2>
<p>These links run JavaScript in this page:</p>
<ul>
<li><a href="#" onclick="alert('Hello from MOB3!'); return false;">show an alert</a></li>
<li><a href="#" onclick="return confirm('Do you like MOB3?')">ask a yes/no question</a></li>
<li><a href="#" onclick="var n = prompt('What is your name?', 'friend'); if (n) { alert('Hello, ' + n + '!'); } return false;">ask for a name</a></li>
</ul>
<h3>Download and upload</h3>
<p><a href="https://mob3.net/index.html" download="index.html">Download a sample file</a>
opens a popup with a <b>curl</b> command; press <b>y</b> to copy it and close
the popup, then run it in your terminal. MOB3 itself never saves the file.</p>
<p>File uploads are not supported. Click the file control below to see a notice
explaining this; it is rendered for layout only and is skipped when a form is
submitted.</p>
<h3>Form controls</h3>
<form action="about:help" method="get">
<p><label>Text input: <input name="demo-text" placeholder="try typing here"></label></p>
<p><label><input type="checkbox" name="demo-check" value="on" checked> Checkbox</label></p>
<p><label><input type="radio" name="demo-radio" value="a" checked> Radio A</label>
<label><input type="radio" name="demo-radio" value="b"> Radio B</label></p>
<p><label>Select: <select name="demo-select"><option value="one">One</option><option value="two" selected>Two</option><option value="three">Three</option></select></label></p>
<p><label>Multi-select:<br><select name="demo-multi" multiple><option value="red">Red</option><option value="green" selected>Green</option><option value="blue">Blue</option></select></label></p>
<p><label>Date: <input type="date" name="demo-date" value="2026-11-10" placeholder="YYYY-MM-DD"></label></p>
<p><label>Time: <input type="time" name="demo-time" value="14:30" placeholder="HH:MM"></label></p>
<p><label>Date and time: <input type="datetime-local" name="demo-datetime" value="2026-11-10T14:30" placeholder="YYYY-MM-DDTHH:MM"></label></p>
<p><label>Month: <input type="month" name="demo-month" value="2026-11" placeholder="YYYY-MM"></label></p>
<p><label>Week: <input type="week" name="demo-week" value="2026-W45" placeholder="YYYY-W01"></label></p>
<p><label>Range: <input type="range" name="demo-range" min="0" max="100" step="5" value="50"></label></p>
<p><label>Color: <input type="color" name="demo-color" value="#2f6fbf" maxlength="7"></label></p>
<p><label>Required (clear it to see the error): <input name="demo-required" value="ready" required></label></p>
<p><label>Pattern (three digits): <input name="demo-pattern" pattern="[0-9]{3}" maxlength="3" value="123"></label></p>
<p><label>Read-only: <input name="demo-readonly" value="cannot edit" readonly></label></p>
<p><label>Disabled: <input name="demo-disabled" value="not submitted" disabled></label></p>
<p><label>Textarea:<br><textarea name="demo-textarea">Some editable text</textarea></label></p>
<p><label>File upload: <input type="file" name="demo-file"></label> (not supported)</p>
<p><button name="demo-submit" value="1">Submit demo</button></p>
</form>

<h2>JavaScript console</h2>
<p><b>!</b> opens logs, requests, storage, the page HTML and Tools. Use
<b>Tab</b> or <b>h</b>/<b>l</b> to switch tabs, <b>:</b>/<b>Enter</b> to focus the input, <b>Enter</b>
to run JavaScript, <b>j</b>/<b>k</b>, <b>d</b>/<b>u</b> or <b>Space</b>/<b>b</b>
to scroll by a line, half a screen or a screen, <b>g</b>/<b>G</b> for the ends,
and <b>Esc</b> or the <b>×</b> in the console menu to close. <b>y</b>
copies the whole active tab — the logs, the raw HTML source, the request list
or the Tools scrollback — because selecting long output by dragging is
awkward. Use <b>Shift</b>-drag when you only want part of it. Script
<b>alert</b>, <b>confirm</b> and <b>prompt</b> calls open modal dialogs.</p>

<p><a href="about:start">Back to the start page</a></p>`

// HelpPageHTML wraps the help body in the shared page shell.
func HelpPageHTML(version string) string {
	return AppPageHTML("Help — MOB3", "",
		strings.Replace(helpPageBody, "{{VERSION}}", EscapeHTML(version), 1))
}

// HistoryPageHTML renders the in-memory session history as clickable links.
func HistoryPageHTML(urls []string, current int) string {
	var b strings.Builder
	b.WriteString(`<h1>Session history</h1>
<p>Pages visited in this session, most recent first. History is kept in
memory only and disappears when MOB3 exits.</p>
`)
	const limit = 50
	start := 0
	if len(urls) > limit {
		start = len(urls) - limit
	}
	count := 0
	for i := len(urls) - 1; i >= start; i-- {
		raw := urls[i]
		if raw == "" || raw == "about:history" {
			continue
		}
		count++
		b.WriteString(`<li><a href="`)
		b.WriteString(EscapeHTML(raw))
		b.WriteString(`">`)
		b.WriteString(EscapeHTML(raw))
		b.WriteString(`</a>`)
		if i == current {
			b.WriteString(` <b>(current)</b>`)
		}
		b.WriteString(`</li>`)
	}
	if count == 0 {
		b.WriteString(`<p>No pages in this session yet.</p>`)
	}
	if len(urls) > limit {
		b.WriteString(`<p>Only the most recent 50 entries are shown.</p>`)
	}
	return AppPageHTML("History — MOB3", "li { margin: 6px 0; }\n", b.String())
}

const StartPageHTML = `<!DOCTYPE html>
<html><head><title>MOB3</title></head>
<body><article>
<h1>MOB³ - a small terminal web browser</h1>
<h3>Getting started</h3>
<ul>
<li>Press <b>o</b> and type a URL, then Enter.</li>
<li>Press <b>f</b> to follow a link.</li>
<li>Scroll with <b>j</b>/<b>k</b>, <b>Space</b>/<b>b</b>, or the arrow keys.</li>
<li><b>Ctrl-O</b> or <b>[</b> goes back, <b>Ctrl-I</b> or <b>]</b> goes forward, <b>r</b> reloads, <b>q</b> asks before quitting.</li>
<li>Press <b>?</b> for the full help page.</li>
<li>History, cookies, storage and caches are memory-only.</li>
</ul>
<form action="about:home" method="get">
<input type="search" name="q" placeholder="Search or enter address">
<button type="submit">Go</button>
</form>
<p><b>Warning:</b> MOB3 is not a secure browser in any way. Do not use it for passwords, accounts, payments or confidential data.</p>
</article></body></html>`

func BlockedPageHTML(rawURL, reason string, status int) string {
	if reason == "" {
		reason = "site protection"
	}
	body := `<h1>Site blocked</h1>
<p class="warn">This site blocked MOB3 or requires a browser check that it
cannot complete. Common causes are Cloudflare, CAPTCHA, anti-bot or firewall
protection, and login protections that need a full browser with
JavaScript/WASM, fingerprinting and browser APIs.</p>
<p>Detected: <b>` + EscapeHTML(reason) + `</b> (HTTP ` + strconv.Itoa(status) + `).</p>
<p class="try"><a href="about:retry">Try anyway</a> — reload this URL without block
detection and run the challenge page's scripts. This is a one-shot bypass; the
site may still refuse access. Challenge pages can run arbitrary script, and
MOB3 is not a secure browser, so only do this if you trust the site.</p>
<p>The requested page was not loaded. Try a different source, use the site's
direct or alternative URL, or open it in a full browser.</p>
<p>URL: <b>` + EscapeHTML(rawURL) + `</b></p>`
	const css = `h1 { color: #b42318; }
.try { margin: 16px 0; padding: 8px; border: 1px solid #2f6feb; background: #eef4ff; }
`
	return AppPageHTML("Site blocked", css, body)
}
