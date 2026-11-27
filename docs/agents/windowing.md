# Windowing and panes

This page records the front-end window model and its migration. Start at
[AGENTS.md](../../AGENTS.md) for the working agreement and at
[architecture.md](architecture.md) for package ownership.

The current front end already gives every tab its own dialogs, menus, prompts
and console. The weakness is that the tab does not have a real window stack:
`overlayList` is a fixed priority list reused for key, mouse, state, install
and reset dispatch, `Presenter.Present` selects one page overlay through an
`if`/`else` chain, and the console is a separate full-screen paint path
(`PresentConsole` plus `applyConsoleOverlays`). That makes a dialog over the
console a special case rather than one consequence of layering.

The goal is one per-tab screen composed from a base surface and an ordered
stack of windows. The console then becomes a surface, not a mode, and a dialog,
menu or settings window is painted over whatever surface is current. A future
resizable or split console only changes surface geometry, not window rules.

## Model

- **Screen:** one per tab. It owns a base surface and a window stack. Only the
  active tab draws.
- **Surface:** the content underneath the windows. The initial surfaces are the
  page and the full-screen console; a later split surface holds page and console
  panes. A surface provides a frame for its rectangle and owns its scroll and
  input when no window claims the event.
- **Window:** a dialog, menu, settings window, history list, URL prompt, find
  bar, download hint, notice, select popup, link preview or hint layer. Each
  window has:
  - a layer (z-index),
  - a rectangle or anchor geometry,
  - `modal` and `persistent` flags,
  - key and mouse handling,
  - one paint method that draws over the composed base frame,
  - the existing open, close, install, reset and page-state hooks.
- **Input:** the topmost modal owns input first. Otherwise the topmost window
  under the pointer (mouse) or above the focused surface (keys) owns it. A
  window that does not consume a key may pass it down; the existing consume
  semantics stay until each window is migrated. When no window claims an
  event, the focused surface handles it.
- **Painting:** the presenter composes the base surface into one full-screen
  frame, then paints open windows in ascending layer order, then encodes and
  presents once. A window never has its own terminal path and never encodes a
  frame itself.
- **Modality:** opening a modal closes every non-persistent window below it.
  The console is persistent, so a page dialog can appear over it; the dialog
  still takes input first and the console resumes when the dialog is answered.
  JavaScript dialogs are always above every surface and every other window.
- **Focus:** a surface can have focus. A modal temporarily takes focus and
  restores it on close. A split surface has one focused pane; moving focus
  between panes is a surface concern, not a window concern.
- **Geometry:** a window is laid out in terminal cells from the terminal size
  and its anchor. Modal windows are centered over the whole tab. The URL prompt
  and find bar stay chrome strips pinned to their rows.

This keeps the existing rules: the URL prompt and find bar hand clicks on other
chrome controls through in the same click; a page input still lets tabs and
toolbar buttons work in one click; hints and link previews keep their current
dismissal rules; and every page-derived string stays bounded and stripped of
terminal controls before painting.

## Migration

### Stage 0: stopgap (landed)

The fixed order now puts a JavaScript dialog above the console for input while
the dialog is still painted over it. The engine applies console open, close and
window requests that arrive while a dialog blocks its loop. This fixes the
blocked-console case but is not the target model.

### Stage 1: window stack and dispatch

- Add layer constants and the `modal`/`persistent` flags to the window
  interface. Replace `dismissOnModal` with the general modal rule.
- Store the tab's open windows in one ordered stack. Replace `overlayList` with
  a stack query used by key, mouse, page-state, install, reset and close
  dispatch.
- Keep the current painting code in this stage so the change is input-only and
  easy to verify.
- Exit gate: the old behavior is unchanged except the landed dialog rule; no
  dispatch path reads a fixed list; tests cover stacking, modal dismissal and a
  dialog over the console.

### Stage 2: generic composition

- Introduce the base `surface` and a window `paint` method. Move the
  `Compose*Frame`/`Draw*` pairs behind that method.
- Make the presenter take a base frame plus the ordered window painters.
  Remove `PresentConsole`, `applyConsoleOverlays` and the overlay `if`/`else`
  chain from `Present`.
- Keep the console full-screen, but model it as a surface. It requests the same
  `ConsoleView` width and height as today.
- Exit gate: one paint path composes page or console plus any open windows;
  frame snapshots cover page-only, console-only, dialog-over-page,
  dialog-over-console and multiple non-modal windows; existing console and
  dialog goldens stay unchanged.

### Stage 3: panes (later)

- Give surfaces pane rectangles and a focused pane. A split screen paints the
  page and console side by side or above/below; the console's `ConsoleView`
  geometry follows its pane instead of the terminal.
- Add pane focus movement, resize handling and scrollbar/native-image rules.
- Exit gate: the console can be a resizable split pane while dialogs, menus and
  settings still open over the whole tab and input follows focus correctly.

## Invariants

- One tab has one screen, one base surface and one ordered window stack.
- Input and paint order come from the same stack; no new fixed priority list.
- A JavaScript dialog is answerable over every surface and window.
- The console is persistent under a modal and resumes where it was.
- The engine never paints chrome. Surfaces and windows are front-end-owned.
- Painting a window is pure over the composed frame: no engine round trip and
  no separate terminal write.
- Only open windows are visited. Idle CPU stays at zero and composing a frame
  remains one pass over the terminal grid.
- Window state and page-derived strings keep the existing caps and terminal
  sanitization.

## Non-goals

- Floating, draggable or freely overlapping windows.
- Multiple consoles per tab.
- A GUI toolkit or native window.
- Moving page data ownership into the front end.
