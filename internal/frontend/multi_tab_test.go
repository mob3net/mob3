package frontend

import (
	"bytes"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"browser/internal/protocol"
)

type eventSession struct {
	events chan protocol.Event
	cmds   []protocol.Command
	closed bool
}

func newEventSession() *eventSession {
	return &eventSession{events: make(chan protocol.Event, 8)}
}

func (s *eventSession) Send(c protocol.Command) error {
	s.cmds = append(s.cmds, c)
	return nil
}
func (s *eventSession) Events() <-chan protocol.Event {
	return s.events
}
func (s *eventSession) Abort() {}
func (s *eventSession) Close() error {
	s.closed = true
	return nil
}

func hasCommand(s *eventSession, want protocol.CommandBody) bool {
	for _, cmd := range s.cmds {
		if reflect.DeepEqual(cmd.Body, want) {
			return true
		}
	}
	return false
}

func multiTab(id int, title string) *Tab {
	return &Tab{
		Session: newEventSession(),
		Stderr:  io.Discard,
		State: LoopState{Page: protocol.State{
			FrontendChrome: true, Width: 80, Height: 24, HeaderRows: 2, Title: title,
		}},
	}
}

func TestSessionLoopRoutesOnlyActiveFrames(t *testing.T) {
	var out bytes.Buffer
	p := NewPresenter(&out, true)
	active := multiTab(1, "one")
	active.Presenter = p
	background := multiTab(2, "two")
	loop := &SessionLoop{Tabs: []*Tab{active, background}, Active: active, Presenter: p}
	loop.syncTabBar()
	out.Reset()

	if _, err := loop.handleEvent(background, protocol.Event{Body: protocol.Frame{
		W: 2, H: 1, Cells: []protocol.Cell{{R: 'Z'}, {R: 'Z'}},
	}}); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("background frame was drawn: %q", out.String())
	}

	if _, err := loop.handleEvent(active, protocol.Event{Body: protocol.Frame{
		W: 2, H: 1, Cells: []protocol.Cell{{R: 'A'}, {R: 'B'}},
	}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "A") {
		t.Fatalf("active frame was not drawn: %q", out.String())
	}

	out.Reset()
	if _, err := loop.handleEvent(background, protocol.Event{Body: protocol.State{
		Width: 80, Height: 24, HeaderRows: 2, FrontendChrome: true,
		Title: "changed", URL: "https://example.test/bg", Scroll: 7,
	}}); err != nil {
		t.Fatal(err)
	}
	if background.State.Page.Title != "changed" || background.State.Scroll != 7 {
		t.Fatalf("background state = %+v scroll=%d", background.State.Page, background.State.Scroll)
	}
	if strings.Contains(out.String(), "Z") {
		t.Fatalf("background page content reached the terminal: %q", out.String())
	}
}

func TestActivateTabMovesThePresenter(t *testing.T) {
	var out bytes.Buffer
	p := NewPresenter(&out, true)
	first := multiTab(1, "one")
	second := multiTab(2, "two")
	first.Presenter = p
	loop := &SessionLoop{Tabs: []*Tab{first, second}, Active: first, Presenter: p}
	loop.syncTabBar()

	loop.activateTab(1)
	if loop.Active != second {
		t.Fatal("second tab is not active")
	}
	if first.Presenter != nil {
		t.Fatal("first tab kept the presenter")
	}
	if second.Presenter != p {
		t.Fatal("second tab did not receive the presenter")
	}
	if !hasCommand(first.Session.(*eventSession), protocol.Visibility{Active: false}) {
		t.Fatal("first tab was not marked inactive")
	}
	if !hasCommand(second.Session.(*eventSession), protocol.Visibility{Active: true}) {
		t.Fatal("second tab was not marked active")
	}
	if p.state.Title != "two" {
		t.Fatalf("presenter state = %q, want the second tab", p.state.Title)
	}
	if len(loop.lastTabs) != 2 || !loop.lastTabs[1].Active || loop.lastTabs[0].Active {
		t.Fatalf("tab bar state = %+v", loop.lastTabs)
	}
}

func TestBackgroundOverlayAppearsAfterSwitch(t *testing.T) {
	var out bytes.Buffer
	p := NewPresenter(&out, true)
	active := multiTab(1, "one")
	active.Presenter = p
	background := multiTab(2, "two")
	background.State.Page.HeaderRows = 2
	loop := &SessionLoop{Tabs: []*Tab{active, background}, Active: active, Presenter: p}
	loop.syncTabBar()

	background.startDialog(protocol.Dialog{ID: 3, Kind: protocol.DialogAlert, Text: "background alert"})
	if !background.dialogActive {
		t.Fatal("background dialog was not recorded")
	}
	if out.Len() != 0 {
		t.Fatalf("background dialog drew: %q", out.String())
	}
	loop.activateTab(1)
	if !strings.Contains(out.String(), "background alert") {
		t.Fatalf("dialog missing after switch: %q", out.String())
	}
}

func TestRemoveTabKeepsTheRemainingActiveTab(t *testing.T) {
	first := multiTab(1, "one")
	second := multiTab(2, "two")
	loop := &SessionLoop{Tabs: []*Tab{first, second}, Active: first, Presenter: NewPresenter(io.Discard, true)}
	loop.syncTabBar()
	loop.removeTab(second)
	if len(loop.Tabs) != 1 || loop.Active != first {
		t.Fatalf("after remove: tabs=%d active=%v", len(loop.Tabs), loop.Active.BarView(true).Title)
	}
	if second.Session != nil {
		t.Fatal("removed tab kept its session")
	}
}

func TestActivateTabResizesStaleBackgroundTab(t *testing.T) {
	first := multiTab(1, "one")
	second := multiTab(2, "two")
	loop := &SessionLoop{
		Tabs: []*Tab{first, second}, Active: first, Presenter: NewPresenter(io.Discard, true),
		TerminalSize: func() (int, int, bool) { return 100, 30, true },
	}
	loop.syncTabBar()
	loop.activateTab(1)
	sess := second.Session.(*eventSession)
	found := false
	for _, cmd := range sess.cmds {
		if resize, ok := cmd.Body.(protocol.Resize); ok {
			found = true
			top, contentH := ContentBounds(30)
			if resize.Width != 100 || resize.Height != 30 ||
				resize.ContentTop != top || resize.ContentHeight != contentH {
				t.Fatalf("resize = %+v, want 100x30 content %d/%d", resize, top, contentH)
			}
		}
	}
	if !found {
		t.Fatal("switching to a stale background tab did not send a resize")
	}
}

func TestActiveTabRevealedAfterResizeState(t *testing.T) {
	st := chromeState()
	st.Width = 100
	var tabs []*Tab
	for i := 0; i < 6; i++ {
		tab := multiTab(i+1, fmt.Sprintf("Tab%d", i+1))
		tab.State.Page = st
		tabs = append(tabs, tab)
	}
	loop := &SessionLoop{
		Tabs: tabs, Active: tabs[0], Presenter: NewPresenter(io.Discard, true),
		TerminalSize: func() (int, int, bool) { return 60, 24, true },
	}
	loop.syncTabBar()
	loop.activateTab(5)
	if !loop.revealPending {
		t.Fatal("switch to a tab with stale geometry did not mark a reveal")
	}
	narrow := st
	narrow.Width = 60
	if _, err := loop.handleEvent(tabs[5], protocol.Event{Body: narrow}); err != nil {
		t.Fatal(err)
	}
	visible := loop.visibleTabCount(loop.tabViews(), loop.tabOffset)
	if visible < 1 || 5 < loop.tabOffset || 5 >= loop.tabOffset+visible {
		t.Fatalf("active tab hidden after resize: offset=%d visible=%d", loop.tabOffset, visible)
	}
	if loop.revealPending {
		t.Fatal("reveal stayed pending after the new state arrived")
	}
}

func TestGraphicsModeIsSharedAcrossTabs(t *testing.T) {
	p := NewPresenter(io.Discard, true)
	first := multiTab(1, "one")
	second := multiTab(2, "two")
	loop := &SessionLoop{
		Tabs: []*Tab{first, second}, Active: first, Presenter: p,
		CellW: 8, CellH: 16,
		GraphicsModes:   []GraphicsMode{GraphicsKitty, GraphicsBlocks, GraphicsNone},
		CurrentGraphics: GraphicsKitty,
	}
	loop.syncTabBar()
	loop.cycleGraphics()
	if loop.CurrentGraphics != GraphicsBlocks {
		t.Fatalf("current graphics = %v, want blocks", loop.CurrentGraphics)
	}
	if p.gfx != nil {
		t.Fatal("presenter still has a native graphics renderer in blocks mode")
	}
	for _, tab := range loop.Tabs {
		if !hasCommand(tab.Session.(*eventSession), protocol.Graphics{Mode: GraphicsBlocks}) {
			t.Fatalf("tab %d did not receive the shared graphics mode", tab.SessionID)
		}
	}
	loop.cycleGraphics()
	if loop.CurrentGraphics != GraphicsNone {
		t.Fatalf("current graphics = %v, want none", loop.CurrentGraphics)
	}
	for _, tab := range loop.Tabs {
		if !hasCommand(tab.Session.(*eventSession), protocol.Graphics{Mode: GraphicsNone}) {
			t.Fatalf("tab %d did not receive none", tab.SessionID)
		}
	}
}

func TestSwitchPatchesZoomLabel(t *testing.T) {
	first := multiTab(1, "one")
	second := multiTab(2, "two")
	loop := &SessionLoop{
		Tabs: []*Tab{first, second}, Active: first,
		Presenter: NewPresenter(io.Discard, true),
	}
	loop.syncTabBar()
	if _, err := loop.handleEvent(first, protocol.Event{Body: protocol.State{
		Width: 80, Height: 24, HeaderRows: 2, FrontendChrome: true,
		Title: "one", ZoomPercent: 150, ZoomLabel: " 150% ",
	}}); err != nil {
		t.Fatal(err)
	}
	if loop.zoomPercent != 150 {
		t.Fatalf("tracked zoom = %d", loop.zoomPercent)
	}
	loop.activateTab(1)
	if second.State.Page.ZoomPercent != 150 || !strings.Contains(second.State.Page.ZoomLabel, "150%") {
		t.Fatalf("switched tab zoom = %d %q", second.State.Page.ZoomPercent, second.State.Page.ZoomLabel)
	}
}

func TestNewTabStartsURLPrompt(t *testing.T) {
	first := multiTab(1, "one")
	first.Presenter = NewPresenter(io.Discard, true)
	loop := &SessionLoop{
		Tabs: []*Tab{first}, Active: first, Presenter: first.Presenter,
		TerminalSize: func() (int, int, bool) { return 80, 24, true },
		OpenTab: func(id protocol.SessionID, w, h, top, contentH int, target string) (*Tab, error) {
			return multiTab(int(id), "New Tab"), nil
		},
	}
	loop.syncTabBar()
	loop.newTab()
	if !loop.Active.promptActive || loop.Active.promptText != "" {
		t.Fatalf("new tab prompt active=%v text=%q", loop.Active.promptActive, loop.Active.promptText)
	}
	if !loop.Active.State.Page.PromptActive || !loop.Active.State.Page.KeysEngine {
		t.Fatalf("new tab state = %+v", loop.Active.State.Page)
	}
	handled, quit := loop.Active.handleShellKey("u")
	if !handled || quit || loop.Active.promptText != "u" {
		t.Fatalf("typing did not go to the URL prompt: handled=%v quit=%v text=%q", handled, quit, loop.Active.promptText)
	}
}

func TestCrashedTabStaysInTheBar(t *testing.T) {
	active := multiTab(1, "one")
	background := multiTab(2, "two")
	loop := &SessionLoop{
		Tabs: []*Tab{active, background}, Active: active,
		Presenter: NewPresenter(io.Discard, true),
	}
	loop.syncTabBar()
	loop.markDead(background, "engine panic")
	if !background.Dead || background.Session != nil {
		t.Fatalf("crashed tab = dead %v session %v", background.Dead, background.Session)
	}
	if len(loop.Tabs) != 2 {
		t.Fatalf("crashed tab was removed: %d tabs", len(loop.Tabs))
	}
	if background.BarView(false).Title != "Crashed" {
		t.Fatalf("crashed tab title = %q", background.BarView(false).Title)
	}
	if active.Dead || active.Session == nil {
		t.Fatal("other tab was affected")
	}
}

func TestCrashedActiveTabKeepsShellKeys(t *testing.T) {
	tab := multiTab(1, "one")
	tab.Presenter = NewPresenter(io.Discard, true)
	loop := &SessionLoop{Tabs: []*Tab{tab}, Active: tab, Presenter: tab.Presenter}
	loop.installTabActions(tab)
	loop.syncTabBar()
	loop.markDead(tab, "engine panic")
	if !tab.Dead || tab.Session != nil || tab.Presenter == nil {
		t.Fatalf("crashed active tab = dead %v session %v presenter %v", tab.Dead, tab.Session, tab.Presenter)
	}
	if !strings.Contains(tab.State.Page.FooterLeft, "Page engine stopped") {
		t.Fatalf("crash footer = %q", tab.State.Page.FooterLeft)
	}
	quit, err := tab.handleDeadInput("q")
	if err != nil || quit || !tab.quitPending {
		t.Fatalf("q on dead tab: quit=%v pending=%v err=%v", quit, tab.quitPending, err)
	}
	quit, err = tab.handleDeadInput("y")
	if err != nil || !quit {
		t.Fatalf("y on dead tab: quit=%v err=%v", quit, err)
	}
}

func TestBackgroundCrashDoesNotWriteStderr(t *testing.T) {
	var stderr strings.Builder
	tab := multiTab(2, "two")
	tab.Stderr = &stderr
	if _, err := tab.applyEvent(protocol.Event{Body: protocol.Crash{Message: "boom"}}); err != nil {
		t.Fatal(err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("background crash wrote to stderr: %q", stderr.String())
	}
	if tab.CrashMessage != "boom" {
		t.Fatalf("crash message = %q", tab.CrashMessage)
	}
}

func TestTabsKeepSeparateHistoryScrollAndClipboard(t *testing.T) {
	first := &Tab{AttestedURL: func(id uint64) (string, bool) {
		if id == 1 {
			return "https://example.test/one", true
		}
		return "", false
	}, Stderr: io.Discard}
	second := &Tab{AttestedURL: func(id uint64) (string, bool) {
		if id == 2 {
			return "https://example.test/two", true
		}
		return "", false
	}, Stderr: io.Discard}
	first.applyHistoryCommit(protocol.HistoryCommit{EntryID: 1, Scroll: 3, Push: true})
	second.applyHistoryCommit(protocol.HistoryCommit{EntryID: 2, Scroll: 9, Push: true})
	if len(first.history) != 1 || len(second.history) != 1 {
		t.Fatalf("history lengths = %d/%d", len(first.history), len(second.history))
	}
	if first.history[0].url == second.history[0].url {
		t.Fatal("tabs share a history entry")
	}
	if _, err := first.applyEvent(protocol.Event{Body: protocol.State{Scroll: 3}}); err != nil {
		t.Fatal(err)
	}
	if _, err := second.applyEvent(protocol.Event{Body: protocol.State{Scroll: 9}}); err != nil {
		t.Fatal(err)
	}
	if first.State.Scroll != 3 || second.State.Scroll != 9 {
		t.Fatalf("scroll = %d/%d", first.State.Scroll, second.State.Scroll)
	}

	var one, two bytes.Buffer
	first.State.Clipboard = &one
	second.State.Clipboard = &two
	first.expectClipboard()
	second.expectClipboard()
	if _, err := first.applyEvent(protocol.Event{Body: protocol.Clipboard{Text: "one"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := second.applyEvent(protocol.Event{Body: protocol.Clipboard{Text: "two"}}); err != nil {
		t.Fatal(err)
	}
	if got := decodeOSC52(t, one.String()); got != "one" {
		t.Fatalf("first clipboard = %q", got)
	}
	if got := decodeOSC52(t, two.String()); got != "two" {
		t.Fatalf("second clipboard = %q", got)
	}
}

func TestHelpAndConfigOpenNewTabs(t *testing.T) {
	tab := &Tab{State: LoopState{Page: protocol.State{FrontendChrome: true}}}
	var opened []string
	tab.Actions = TabActions{NewTabURL: func(url string) { opened = append(opened, url) }}
	if handled, quit := tab.handleShellKey("?"); !handled || quit {
		t.Fatal("? was not handled")
	}
	tab.activateChromeHit(ChromeHit{Kind: ChromeHitConfig})
	tab.activateChromeHit(ChromeHit{Kind: ChromeHitHelp})
	want := []string{"about:help", "about:config", "about:help"}
	if fmt.Sprint(opened) != fmt.Sprint(want) {
		t.Fatalf("opened = %v, want %v", opened, want)
	}
}

func TestNewTabURLShowsTargetWithoutPrompt(t *testing.T) {
	first := multiTab(1, "one")
	first.Presenter = NewPresenter(io.Discard, true)
	var openedTarget string
	loop := &SessionLoop{
		Tabs: []*Tab{first}, Active: first, Presenter: first.Presenter,
		TerminalSize: func() (int, int, bool) { return 80, 24, true },
		OpenTab: func(id protocol.SessionID, w, h, top, contentH int, target string) (*Tab, error) {
			openedTarget = target
			return multiTab(int(id), "Help"), nil
		},
	}
	loop.syncTabBar()
	loop.newTabURL("about:help")
	if openedTarget != "about:help" {
		t.Fatalf("target = %q", openedTarget)
	}
	if loop.Active.promptActive {
		t.Fatal("targeted new tab opened the URL prompt")
	}
}

func TestHomePageKeepsItsTitleButTabSaysNewTab(t *testing.T) {
	tab := multiTab(1, "MOB³")
	tab.State.Page.URL = "about:home"
	if got := tab.BarView(true).Title; got != "New Tab" {
		t.Fatalf("home tab label = %q", got)
	}
	tab.State.Page.URL = "https://example.test/"
	tab.State.Page.Title = "Example"
	if got := tab.BarView(true).Title; got != "Example" {
		t.Fatalf("page tab label = %q", got)
	}
}

func TestOpenTabEventOpensBackgroundTab(t *testing.T) {
	tab := &Tab{Stderr: io.Discard}
	opened := ""
	tab.Actions = TabActions{OpenTabBackground: func(url string) { opened = url }}
	if _, err := tab.applyEvent(protocol.Event{Body: protocol.OpenTab{URL: "https://example.test/next"}}); err != nil {
		t.Fatal(err)
	}
	if opened != "https://example.test/next" {
		t.Fatalf("opened = %q", opened)
	}
	opened = ""
	if _, err := tab.applyEvent(protocol.Event{Body: protocol.OpenTab{URL: "javascript:alert(1)"}}); err != nil {
		t.Fatal(err)
	}
	if opened != "" {
		t.Fatalf("unsafe URL opened: %q", opened)
	}
}

func TestBackgroundNewTabKeepsCurrentTab(t *testing.T) {
	first := multiTab(1, "one")
	first.Presenter = NewPresenter(io.Discard, true)
	var openedTarget string
	loop := &SessionLoop{
		Tabs: []*Tab{first}, Active: first, Presenter: first.Presenter,
		TerminalSize: func() (int, int, bool) { return 80, 24, true },
		OpenTab: func(id protocol.SessionID, w, h, top, contentH int, target string) (*Tab, error) {
			openedTarget = target
			return multiTab(int(id), "Linked"), nil
		},
	}
	loop.syncTabBar()
	loop.newTabBackground("https://example.test/next")
	if openedTarget != "https://example.test/next" {
		t.Fatalf("target = %q", openedTarget)
	}
	if loop.Active != first || len(loop.Tabs) != 2 {
		t.Fatalf("active changed to %v with %d tabs", loop.Active, len(loop.Tabs))
	}
	if !hasCommand(loop.Tabs[1].Session.(*eventSession), protocol.Visibility{Active: false}) {
		t.Fatal("background tab was not suspended")
	}
	if loop.Tabs[1].promptActive {
		t.Fatal("background link tab opened the URL prompt")
	}
}

func TestCycleTabStopsAtFirstAndLast(t *testing.T) {
	var tabs []*Tab
	for i := 0; i < 3; i++ {
		tabs = append(tabs, multiTab(i+1, fmt.Sprintf("Tab%d", i+1)))
	}
	loop := &SessionLoop{
		Tabs: tabs, Active: tabs[0], Presenter: NewPresenter(io.Discard, true),
		TerminalSize: func() (int, int, bool) { return 80, 24, true },
	}
	loop.syncTabBar()
	loop.cycleTab(-1)
	if loop.Active != tabs[0] {
		t.Fatal("previous from the first tab wrapped")
	}
	loop.cycleTab(1)
	if loop.Active != tabs[1] {
		t.Fatal("next did not move to the second tab")
	}
	loop.cycleTab(1)
	if loop.Active != tabs[2] {
		t.Fatal("next did not move to the last tab")
	}
	loop.cycleTab(1)
	if loop.Active != tabs[2] {
		t.Fatal("next from the last tab wrapped")
	}
	loop.cycleTab(-1)
	if loop.Active != tabs[1] {
		t.Fatal("previous did not move back")
	}
}

func TestTabKeysRouteToActions(t *testing.T) {
	tab := &Tab{State: LoopState{Page: protocol.State{FrontendChrome: true}}}
	var got []int
	tab.Actions = TabActions{
		Select: func(index int) { got = append(got, 100+index) },
		Cycle:  func(delta int) { got = append(got, 200+delta) },
		Close: func(index int) bool {
			got = append(got, 300+index)
			return false
		},
		New: func() { got = append(got, 400) },
	}
	cases := []struct {
		key  string
		want int
	}{
		{"t", 400}, {"ctrl-t", 400},
		{"W", 299}, {"ctrl-w", 299},
		{"ctrl-n", 201}, {"alt-right", 201},
		{"ctrl-p", 199}, {"alt-left", 199},
		{"2", 101}, {"alt-3", 102},
	}
	for _, tc := range cases {
		got = nil
		handled, quit := tab.handleShellKey(tc.key)
		if !handled || quit {
			t.Fatalf("%q handled=%v quit=%v", tc.key, handled, quit)
		}
		if len(got) != 1 || got[0] != tc.want {
			t.Fatalf("%q actions = %v, want %d", tc.key, got, tc.want)
		}
	}
}

func TestTabBarMouseRoutesActions(t *testing.T) {
	st := chromeState()
	st.Width = 120
	tabs := []TabView{{Title: "One", Active: true}, {Title: "Two"}}
	tab := &Tab{State: LoopState{Page: st}, TabBar: tabs}
	var selected, closed, created, scrolled = -1, -1, 0, 0
	tab.Actions = TabActions{
		Select: func(index int) { selected = index },
		Close: func(index int) bool {
			closed = index
			return false
		},
		New: func() { created++ },
		Scroll: func(delta int) {
			scrolled += delta
		},
	}
	layout := LayoutChromeWithTabs(st, tabs, 0)
	click := func(kind ChromeHitKind, index int) bool {
		for _, hit := range layout.Hits {
			if hit.Kind != kind || (kind == ChromeHitTab || kind == ChromeHitTabClose) && hit.Index != index {
				continue
			}
			return tab.handleChromeMouse(MouseEvent{X: hit.X + 1, Y: hit.Y + 1, Press: true})
		}
		t.Fatalf("missing hit %v index %d", kind, index)
		return false
	}
	if !click(ChromeHitTab, 1) || selected != 1 {
		t.Fatalf("tab click selected=%d", selected)
	}
	if !click(ChromeHitTabClose, 0) || closed != 0 {
		t.Fatalf("close click closed=%d", closed)
	}
	if !click(ChromeHitNewTab, -1) || created != 1 {
		t.Fatalf("new click created=%d", created)
	}
	many := make([]TabView, 12)
	for i := range many {
		many[i] = TabView{Title: fmt.Sprintf("Tab%d", i+1)}
	}
	tab.TabBar = many
	layout = LayoutChromeWithTabs(st, many, 0)
	for _, kind := range []ChromeHitKind{ChromeHitTabPrev, ChromeHitTabNext} {
		found := false
		for _, hit := range layout.Hits {
			if hit.Kind != kind {
				continue
			}
			found = tab.handleChromeMouse(MouseEvent{X: hit.X + 1, Y: hit.Y + 1, Press: true})
			break
		}
		if !found {
			t.Fatalf("missing %v hit", kind)
		}
	}
	if scrolled != 0 {
		t.Fatalf("scroll clicks = %d", scrolled)
	}
}

func TestNewTabFactoryActivatesTheTab(t *testing.T) {
	p := NewPresenter(io.Discard, true)
	first := multiTab(1, "one")
	first.Presenter = p
	first.SessionID = 1
	var gotID protocol.SessionID
	var gotW, gotH, gotTop, gotContent int
	var gotTarget string
	loop := &SessionLoop{
		Tabs: []*Tab{first}, Active: first, Presenter: p,
		TerminalSize:  func() (int, int, bool) { return 80, 24, true },
		GraphicsModes: []GraphicsMode{GraphicsBlocks, GraphicsNone},
		OpenTab: func(id protocol.SessionID, w, h, top, contentH int, target string) (*Tab, error) {
			gotID, gotW, gotH, gotTop, gotContent, gotTarget = id, w, h, top, contentH, target
			tab := multiTab(int(id), "New Tab")
			tab.State.Page.Width = w
			tab.State.Page.Height = h
			return tab, nil
		},
		CurrentGraphics: GraphicsBlocks,
	}
	loop.syncTabBar()
	loop.newTab()
	if gotID != 2 || gotW != 80 || gotH != 24 || gotTarget != "" {
		t.Fatalf("factory args id=%d size=%dx%d target=%q", gotID, gotW, gotH, gotTarget)
	}
	if top, contentH := ContentBounds(24); gotTop != top || gotContent != contentH {
		t.Fatalf("factory geometry = %d/%d, want %d/%d", gotTop, gotContent, top, contentH)
	}
	if len(loop.Tabs) != 2 || loop.Active != loop.Tabs[1] {
		t.Fatalf("tabs=%d active=%v", len(loop.Tabs), loop.Active)
	}
	if loop.Active.SessionID != 2 || loop.Active.Presenter != p {
		t.Fatalf("new tab id=%d presenter=%v", loop.Active.SessionID, loop.Active.Presenter)
	}
	if !hasCommand(loop.Active.Session.(*eventSession), protocol.Graphics{Mode: GraphicsBlocks}) {
		t.Fatal("new tab did not receive the current browser graphics mode")
	}
	if loop.Active.historyLog != loop.history {
		t.Fatal("new tab did not join the shared history log")
	}
}

func TestCloseLastTabAsksToQuit(t *testing.T) {
	tab := multiTab(1, "one")
	tab.State.Page = chromeState()
	closed := false
	loop := &SessionLoop{
		Tabs: []*Tab{tab}, Active: tab, Presenter: NewPresenter(io.Discard, true),
		CloseTab: func(*Tab) { closed = true },
	}
	loop.syncTabBar()
	if loop.closeTab(-1) {
		t.Fatal("closing the last tab returned quit immediately")
	}
	if len(loop.Tabs) != 1 || !tab.quitPending {
		t.Fatalf("last-tab close did not ask to quit: tabs=%d pending=%v", len(loop.Tabs), tab.quitPending)
	}
	if closed {
		t.Fatal("last tab was released before confirmation")
	}
}

func TestCloseTabReleasesAndActivatesNeighbor(t *testing.T) {
	first := multiTab(1, "one")
	second := multiTab(2, "two")
	var released *Tab
	loop := &SessionLoop{
		Tabs: []*Tab{first, second}, Active: first, Presenter: NewPresenter(io.Discard, true),
		CloseTab: func(tab *Tab) { released = tab },
	}
	loop.syncTabBar()
	loop.closeTab(0)
	if released != first || first.Session != nil {
		t.Fatal("closed tab was not released")
	}
	if len(loop.Tabs) != 1 || loop.Active != second {
		t.Fatalf("tabs=%d active=%v", len(loop.Tabs), loop.Active)
	}
}

func TestScrollTabsClampsTheWindow(t *testing.T) {
	st := chromeState()
	st.Width = 120
	var tabs []*Tab
	for i := 0; i < 12; i++ {
		tab := multiTab(i+1, fmt.Sprintf("Tab%d", i+1))
		tab.State.Page = st
		tabs = append(tabs, tab)
	}
	loop := &SessionLoop{Tabs: tabs, Active: tabs[0]}
	loop.syncTabBar()
	visible := loop.visibleTabCount(loop.tabViews(), 0)
	if visible <= 1 || visible >= len(tabs) {
		t.Fatalf("visible tabs = %d", visible)
	}
	maxOffset := len(tabs) - visible
	loop.scrollTabs(1)
	if loop.tabOffset != 1 {
		t.Fatalf("offset = %d, want 1", loop.tabOffset)
	}
	loop.scrollTabs(100)
	if loop.tabOffset != maxOffset {
		t.Fatalf("offset = %d, want %d", loop.tabOffset, maxOffset)
	}
	loop.scrollTabs(-100)
	if loop.tabOffset != 0 {
		t.Fatalf("offset = %d, want 0", loop.tabOffset)
	}
	for _, tab := range tabs {
		if tab.TabOffset != 0 {
			t.Fatalf("tab bar offset not mirrored: %d", tab.TabOffset)
		}
	}
}
