# Windowing and surfaces

This page records the front-end window model and its migration. Start at
[AGENTS.md](../../AGENTS.md) for the working agreement and at
[architecture.md](architecture.md) for package ownership.

The front end gives every tab its own dialogs, menus, prompts and console. The
first migration stages are landed: key, mouse, state, install and close
dispatch come from the tab's `windowStack` layers instead of a fixed list
position, and the presenter composes a base surface (page or full-screen
console) and then paints an ordered `windowPainter` list. A dialog, menu or
settings window is therefore painted over whatever surface is current.

The goal remains one per-tab screen composed from a base surface and an ordered
stack of windows. The console stays full-screen; the pane split was tried and
abandoned in Stage 3.

## Screen composition

One tab is one screen: a base surface filling the terminal, then the window
stack painted above it. Only the active tab draws.

```text
mob3 frontend
 │  one terminal; only the active tab draws
 │
 ├── tab 1 screen
 │    ├── base surface (full terminal)
 │    │    ├── page: toolbar + tab strip, content, footer
 │    │    └── console: console view and input row
 │    └── window stack, paint order bottom to top:
 │         hint letters -> find bar -> URL prompt -> link preview  [inline chrome]
 │         -> menu -> history -> config -> select -> notice
 │         -> download -> dialog                                    [window painters]
 ├── tab 2 screen
 │    └── same shape, its own windows
 └── tab 3 screen
      └── same shape, its own windows
```

The base surface is the whole terminal, not a window. The page and console each
fill it, and the console replaces the page while open. Splitting them was
abandoned because focus, geometry and tab chrome cannot be shared cleanly by the
engine page and the front-end console. The tab strip is page header chrome, not
a window.

The stack is the input order too, topmost first: dialog, download, notice,
select, config, history, menu, link preview, URL prompt, find bar, hint. The
first four layers paint into the base surface (header, footer or page cells);
the rest paint through `windowPainter`. A JavaScript dialog is always top, and
the persistent console resumes under a modal.

## Model

- **Screen:** one per tab. It owns a base surface and a window stack. Only the
  active tab draws.
- **Surface:** the content underneath the windows. The surfaces are the page and
  the full-screen console. A surface fills the terminal and owns its scroll and
  input when no window claims the event.
- **Window:** a dialog, menu, settings window, history list, URL prompt, find
  bar, download hint, notice, select popup, link preview or hint layer. Each
  window has:
  - a layer (z-index),
  - a rectangle or anchor geometry,
  - `modal` and `persistent` flags,
  - key and mouse handling,
  - one paint path: inline into the base surface, or as a `windowPainter` over
    it,
  - the existing open, close, install, reset and page-state hooks.
- **Input:** the topmost modal owns input first. Otherwise the topmost window
  under the pointer (mouse) or above the active surface (keys) owns it. A
  window that does not consume a key may pass it down; the existing consume
  semantics stay until each window is migrated. When no window claims an
  event, the active surface handles it.
- **Painting:** the presenter composes the base surface into one full-screen
  frame, then paints open windows in ascending layer order, then encodes and
  presents once. A window never has its own terminal path and never encodes a
  frame itself.
- **Modality:** opening a modal closes every non-persistent window below it.
  The console is persistent, so a page dialog can appear over it; the dialog
  still takes input first and the console resumes when the dialog is answered.
  JavaScript dialogs are always above every surface and every other window.
- **Focus:** the page or the open console owns the base surface. A modal
  temporarily takes focus and restores it on close.
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

### Stage 1: window stack and dispatch (landed)

- Add layer constants and the `persistent` flag to the window interface.
  Replace `dismissOnModal` with the general modal rule. A separate `modal`
  classification is not needed until input is split from the base surface.
- Derive the tab's open windows as one ordered stack. Replace `overlayList`
  with that stack query for key, mouse, page-state, install and close dispatch;
  crash reset still walks every registered window.
- Keep the current painting code in this stage so the change is input-only and
  easy to verify.
- Exit gate: every dispatch path reads the stack in layer order; a dialog, menu
  or other window opened over the console owns input first; painting is
  unchanged; tests cover stacking, modal dismissal and a dialog over the
  console.

### Stage 2: generic composition (landed)

- Introduce `windowPainter` and one ordered painter list in the presenter.
  Remove the `Compose*Frame` wrappers; the existing `Draw*` functions become
  the painters' draw calls.
- Compose the page chrome or the console surface into a full frame, then paint
  the ordered windows over it. Remove `applyConsoleOverlays` and the overlay
  `if`/`else` chain from `Present`; `PresentConsole` remains only the console
  surface entry point and shares the window painter path.
- Keep the console full-screen, but model it as a surface. It requests the same
  `ConsoleView` width and height as today.
- Exit gate: one window painter path composes page or console plus any open
  windows; frame snapshots cover page-only, console-only, dialog-over-page,
  dialog-over-console and multiple non-modal windows; existing console and
  dialog goldens stay unchanged.

### Stage 3: pane split (abandoned)

The split was implemented and then rolled back. It made the page and console
peers in one tab, but the page is engine-owned and the console is front-end
owned. Every tab-level feature then needed a pane decision: focus, the URL
prompt, find, the scrollbar, `X`/`Z` windows, mouse coordinates, form editing,
closing geometry and native-image/scaled-text placement. Each fix exposed
another edge case, and the terminal UI gained more state than the feature was
worth.

No pane geometry remains. The presenter keeps the internal `surfaceFrame`
writer because it puts page and console output on one composition path, but both
surfaces are full-terminal. Do not add a split or dock without a first-class
focus/geometry protocol and a full tab-chrome redesign.

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
