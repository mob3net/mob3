package frontend

import (
	"errors"
	"fmt"
	"os"
	"reflect"

	"browser/internal/protocol"
)

// SessionLoop owns the terminal side of the browser: one shared presenter,
// input channels and the active tab. It drains every tab's event stream so a
// background session never blocks on its queue, but only the active tab draws.
type SessionLoop struct {
	Tabs   []*Tab
	Active *Tab

	// Presenter is the one terminal presenter. It moves between tabs on a
	// switch; the old tab's overlay pointers and native placements are cleared.
	Presenter *Presenter

	// OpenTab creates and starts one tab with the supplied protocol session ID,
	// content geometry and target URL. An empty target opens the home page.
	// CloseTab releases the transport and per-tab engine resources. The app
	// supplies both; tests may leave them nil.
	OpenTab  func(id protocol.SessionID, w, h, contentTop, contentHeight int, target string) (*Tab, error)
	CloseTab func(*Tab)

	Keys   <-chan string
	Resize <-chan os.Signal
	Quit   <-chan os.Signal

	// TerminalSize supplies the current width/height for a resize command.
	TerminalSize func() (w, h int, ok bool)
	CellW        int
	CellH        int

	// GraphicsModes is the browser-wide image-mode cycle; CurrentGraphics is
	// the mode shared by every tab.
	GraphicsModes   []GraphicsMode
	CurrentGraphics GraphicsMode

	lastTabs      []TabView
	tabOffset     int
	lastTabOffset int
	nextSessionID protocol.SessionID
	revealPending bool
	zoomPercent   int
	history       *historyLog
	tabHistory    []*Tab // previously active tabs, most recent first
}

// Run processes keys, resizes and every tab's events until it quits or the
// terminal closes.
func (l *SessionLoop) Run() error {
	if l.Active == nil || l.Active.Session == nil {
		return errors.New("no active tab")
	}
	if l.Active.Stderr == nil {
		l.Active.Stderr = os.Stderr
	}
	if l.Presenter == nil {
		l.Presenter = l.Active.Presenter
	}
	log := l.ensureHistory()
	for _, tab := range l.Tabs {
		if tab != nil && tab.Stderr == nil {
			tab.Stderr = os.Stderr
		}
		if tab != nil {
			tab.historyLog = log
		}
		l.installTabActions(tab)
		l.sendGraphics(tab)
	}
	l.syncTabBar()
	if l.Active.Presenter == nil && l.Presenter != nil {
		l.Active.AttachPresenter(l.Presenter)
	}
	defer l.CloseAll()

	for {
		cases := make([]reflect.SelectCase, 0, 3+len(l.Tabs))
		kinds := make([]int, 0, 3+len(l.Tabs))
		eventTabs := make([]*Tab, 0, len(l.Tabs))
		add := func(ch any, kind int, tab *Tab) {
			if ch == nil {
				return
			}
			v := reflect.ValueOf(ch)
			if v.Kind() != reflect.Chan || v.IsNil() {
				return
			}
			cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: v})
			kinds = append(kinds, kind)
			eventTabs = append(eventTabs, tab)
		}
		add(l.Keys, 0, nil)
		add(l.Resize, 1, nil)
		add(l.Quit, 2, nil)
		for _, tab := range l.Tabs {
			if tab == nil || tab.Session == nil {
				continue
			}
			add(tab.Session.Events(), 3, tab)
		}
		if len(cases) == 0 {
			return nil
		}
		chosen, recv, ok := reflect.Select(cases)
		switch kinds[chosen] {
		case 0:
			if !ok {
				return nil
			}
			if l.Active == nil {
				return nil
			}
			k, _ := recv.Interface().(string)
			if l.Active.Dead {
				quit, err := l.Active.handleDeadInput(k)
				if err != nil {
					return err
				}
				if quit {
					return nil
				}
				continue
			}
			quit, err := l.Active.handleInput(k)
			if err != nil {
				return err
			}
			if quit {
				return nil
			}
		case 1:
			if !ok {
				l.Resize = nil
				continue
			}
			if l.TerminalSize == nil {
				continue
			}
			if err := l.resizeTab(l.Active); err != nil {
				return err
			}
			l.revealPending = true
		case 2:
			if !ok {
				l.Quit = nil
				continue
			}
			return nil
		case 3:
			tab := eventTabs[chosen]
			if !ok {
				l.markDead(tab, "protocol session ended")
				continue
			}
			ev, _ := recv.Interface().(protocol.Event)
			if quit, err := l.handleEvent(tab, ev); err != nil {
				return err
			} else if quit {
				return nil
			}
		}
	}
}

// handleEvent applies one event. Background frames are discarded, but state,
// history, titles and overlay events still update that tab's own model.
func (l *SessionLoop) handleEvent(tab *Tab, ev protocol.Event) (bool, error) {
	if tab != l.Active {
		if _, isFrame := ev.Body.(protocol.Frame); isFrame {
			return false, nil
		}
		quit, err := tab.applyEvent(ev)
		if err != nil {
			return false, err
		}
		if quit {
			// A background tab asking to quit is closed; the application keeps
			// running for the other tabs.
			l.removeTab(tab)
			return false, nil
		}
		l.syncTabBar()
		return false, nil
	}
	quit, err := l.Active.applyEvent(ev)
	if err != nil {
		return false, err
	}
	if st, ok := ev.Body.(protocol.State); ok && st.ZoomPercent > 0 {
		l.zoomPercent = st.ZoomPercent
	}
	if l.revealPending {
		if _, ok := ev.Body.(protocol.State); ok {
			l.revealActiveTab(l.tabViews())
			l.revealPending = false
		}
	}
	l.syncTabBar()
	return quit, nil
}

// markDead contains a failed tab: its transport is released, but the tab keeps
// its place in the bar and can still be selected, closed or used to open another
// tab.
func (l *SessionLoop) markDead(tab *Tab, message string) {
	if tab == nil {
		return
	}
	tab.MarkDead(message)
	l.releaseTab(tab)
	if tab == l.Active && l.Presenter != nil {
		tab.AttachPresenter(l.Presenter)
	}
	l.syncTabBar()
}

func (l *SessionLoop) removeTab(tab *Tab) {
	if tab == nil {
		return
	}
	index := -1
	for i, candidate := range l.Tabs {
		if candidate == tab {
			index = i
			break
		}
	}
	if index < 0 {
		return
	}
	active := tab == l.Active
	if active {
		tab.DetachPresenter()
		l.Active = nil
	}
	l.Tabs = append(l.Tabs[:index], l.Tabs[index+1:]...)
	l.forgetTabHistory(tab)
	l.releaseTab(tab)
	if active && len(l.Tabs) > 0 {
		if next := l.previousTab(); next != nil {
			l.activateTab(l.tabIndex(next))
		} else {
			next := index
			if next >= len(l.Tabs) {
				next = len(l.Tabs) - 1
			}
			l.activateTab(next)
		}
		return
	}
	l.syncTabBar()
}

// releaseTab closes the transport and releases any per-tab resources.
func (l *SessionLoop) releaseTab(tab *Tab) {
	if tab == nil || tab.Session == nil {
		return
	}
	if l.CloseTab != nil {
		l.CloseTab(tab)
	} else {
		_ = tab.Session.Close()
	}
	tab.Session = nil
	tab.Presenter = nil
}

// CloseAll releases every remaining tab. It is safe to call more than once.
func (l *SessionLoop) CloseAll() {
	for _, tab := range l.Tabs {
		l.releaseTab(tab)
	}
}

func (l *SessionLoop) installTabActions(tab *Tab) {
	if tab == nil {
		return
	}
	tab.Actions = TabActions{
		Select:            l.selectTab,
		Cycle:             l.cycleTab,
		Close:             l.closeTab,
		New:               l.newTab,
		NewTabURL:         l.newTabURL,
		OpenTabBackground: l.newTabBackground,
		Scroll:            l.scrollTabs,
		ToggleGraphics:    l.cycleGraphics,
	}
}

func (l *SessionLoop) sendGraphics(tab *Tab) {
	if tab == nil || tab.Session == nil || len(l.GraphicsModes) == 0 {
		return
	}
	_ = tab.Session.Send(protocol.Command{Body: protocol.Graphics{Mode: l.CurrentGraphics}})
}

func (l *SessionLoop) ensureHistory() *historyLog {
	if l.history == nil {
		l.history = newHistoryLog()
	}
	return l.history
}

func (l *SessionLoop) cycleGraphics() {
	if len(l.GraphicsModes) == 0 {
		return
	}
	next := l.GraphicsModes[0]
	for i, mode := range l.GraphicsModes {
		if mode == l.CurrentGraphics {
			next = l.GraphicsModes[(i+1)%len(l.GraphicsModes)]
			break
		}
	}
	l.CurrentGraphics = next
	if l.Presenter != nil {
		l.Presenter.SetGraphics(next, l.CellW, l.CellH)
	}
	for _, tab := range l.Tabs {
		l.sendGraphics(tab)
	}
}

func (l *SessionLoop) selectTab(index int) {
	if index < 0 || index >= len(l.Tabs) {
		return
	}
	l.activateTab(index)
}

func (l *SessionLoop) cycleTab(delta int) {
	if len(l.Tabs) < 2 {
		return
	}
	index := l.activeIndex()
	if index < 0 {
		return
	}
	next := index + delta
	if next < 0 {
		next = 0
	}
	if next >= len(l.Tabs) {
		next = len(l.Tabs) - 1
	}
	if next == index {
		return
	}
	l.activateTab(next)
}

func (l *SessionLoop) activeIndex() int {
	for i, tab := range l.Tabs {
		if tab == l.Active {
			return i
		}
	}
	return -1
}

func (l *SessionLoop) tabIndex(tab *Tab) int {
	for i, candidate := range l.Tabs {
		if candidate == tab {
			return i
		}
	}
	return -1
}

// rememberPreviousTab records the tab that was active before a switch. The most
// recently used tab stays at the front, so closing the active tab can return to
// it.
func (l *SessionLoop) rememberPreviousTab(tab *Tab) {
	if tab == nil {
		return
	}
	l.forgetTabHistory(tab)
	l.tabHistory = append(l.tabHistory, nil)
	copy(l.tabHistory[1:], l.tabHistory)
	l.tabHistory[0] = tab
}

func (l *SessionLoop) forgetTabHistory(tab *Tab) {
	out := l.tabHistory[:0]
	for _, candidate := range l.tabHistory {
		if candidate != tab {
			out = append(out, candidate)
		}
	}
	l.tabHistory = out
}

// previousTab returns the most recently used tab that is still open and removes
// it from the history stack.
func (l *SessionLoop) previousTab() *Tab {
	for len(l.tabHistory) > 0 {
		tab := l.tabHistory[0]
		l.tabHistory = l.tabHistory[1:]
		if tab != nil && l.tabIndex(tab) >= 0 {
			return tab
		}
	}
	return nil
}

func (l *SessionLoop) currentSize() (int, int) {
	if l.TerminalSize != nil {
		if w, h, ok := l.TerminalSize(); ok {
			return w, h
		}
	}
	if l.Active != nil && l.Active.State.Page.Width > 0 && l.Active.State.Page.Height > 0 {
		return l.Active.State.Page.Width, l.Active.State.Page.Height
	}
	return 100, 30
}

// resizeTab applies the current terminal and content rectangle to one tab. A
// background tab is resized when it becomes active, so switching after a
// window resize cannot present a frame laid out for the old size.
func (l *SessionLoop) resizeTab(tab *Tab) error {
	if tab == nil || tab.Session == nil {
		return nil
	}
	w, h := l.currentSize()
	if err := tab.Session.Send(protocol.Command{Body: l.resizeCommand(w, h)}); err != nil && !errors.Is(err, protocol.ErrSessionClosed) {
		return err
	}
	return nil
}

func (l *SessionLoop) setVisible(tab *Tab, active bool) {
	if tab == nil || tab.Session == nil {
		return
	}
	_ = tab.Session.Send(protocol.Command{Body: protocol.Visibility{Active: active}})
}

func (l *SessionLoop) allocateSessionID() protocol.SessionID {
	if l.nextSessionID == 0 {
		for _, tab := range l.Tabs {
			if tab != nil && tab.SessionID > l.nextSessionID {
				l.nextSessionID = tab.SessionID
			}
		}
	}
	l.nextSessionID++
	return l.nextSessionID
}

// newTab opens the configured home page in a new tab and switches to it.
func (l *SessionLoop) newTab() {
	l.newTabAt("", true, false)
}

// newTabURL opens target in a new tab and switches to it. An empty target opens
// the home page and focuses its URL prompt.
func (l *SessionLoop) newTabURL(target string) {
	l.newTabAt(target, true, false)
}

// newTabBackground opens target without moving away from the current tab. Link
// tabs go directly after the current tab; explicit new-tab actions use newTab.
func (l *SessionLoop) newTabBackground(target string) {
	l.newTabAt(target, false, true)
}

func (l *SessionLoop) newTabAt(target string, activate, afterActive bool) {
	if l.OpenTab == nil {
		return
	}
	w, h := l.currentSize()
	top, contentH := ContentBounds(h)
	id := l.allocateSessionID()
	tab, err := l.OpenTab(id, w, h, top, contentH, target)
	if err != nil {
		l.showStatusError("New tab failed: " + err.Error())
		return
	}
	if tab == nil {
		l.showStatusError("New tab failed")
		return
	}
	tab.SessionID = id
	tab.historyLog = l.ensureHistory()
	l.installTabActions(tab)
	l.sendGraphics(tab)
	insert := len(l.Tabs)
	if afterActive {
		if current := l.activeIndex(); current >= 0 {
			insert = current + 1
		}
	}
	l.Tabs = append(l.Tabs, nil)
	copy(l.Tabs[insert+1:], l.Tabs[insert:])
	l.Tabs[insert] = tab
	if activate {
		if target == "" {
			tab.promptOverlay.startEmpty(tab)
		}
		l.activateTab(insert)
		return
	}
	l.setVisible(tab, false)
	l.syncTabBar()
}

// closeTab removes one tab. Closing the final tab exits the browser
// immediately instead of asking for the normal quit confirmation. Index -1
// means the active tab.
func (l *SessionLoop) closeTab(index int) bool {
	if index < 0 {
		index = l.activeIndex()
	}
	if index < 0 || index >= len(l.Tabs) {
		return false
	}
	tab := l.Tabs[index]
	if len(l.Tabs) == 1 {
		return true
	}
	active := tab == l.Active
	if active {
		tab.DetachPresenter()
		l.Active = nil
	}
	l.Tabs = append(l.Tabs[:index], l.Tabs[index+1:]...)
	l.forgetTabHistory(tab)
	l.releaseTab(tab)
	if active {
		if next := l.previousTab(); next != nil {
			l.activateTab(l.tabIndex(next))
		} else {
			next := index
			if next >= len(l.Tabs) {
				next = len(l.Tabs) - 1
			}
			l.activateTab(next)
		}
		return false
	}
	l.syncTabBar()
	return false
}

// scrollTabs moves the visible window of a tab strip that cannot fit every
// tab at the minimum width.
func (l *SessionLoop) scrollTabs(delta int) {
	views := l.tabViews()
	if len(views) < 2 {
		return
	}
	visible := l.visibleTabCount(views, l.tabOffset)
	maxOffset := len(views) - visible
	if maxOffset < 0 {
		maxOffset = 0
	}
	offset := l.tabOffset + delta
	if offset < 0 {
		offset = 0
	}
	if offset > maxOffset {
		offset = maxOffset
	}
	if offset == l.tabOffset {
		return
	}
	l.tabOffset = offset
	l.revealPending = false
	l.syncTabBar()
}

func (l *SessionLoop) visibleTabCount(views []TabView, offset int) int {
	st := l.chromeState()
	if st.Width <= 0 || st.Height <= 0 {
		return len(views)
	}
	layout := LayoutChromeWithTabs(st, views, offset)
	count := 0
	for _, hit := range layout.Hits {
		if hit.Kind == ChromeHitTab {
			count++
		}
	}
	if count < 1 {
		return len(views)
	}
	return count
}

// chromeState returns a state snapshot with usable geometry. A newly opened
// tab has no state until its first event, so the strip measures against the
// active tab or any sibling that already knows the terminal size.
func (l *SessionLoop) chromeState() protocol.State {
	if l.Active != nil && l.Active.State.Page.Width > 0 && l.Active.State.Page.Height > 0 {
		return l.Active.State.Page
	}
	for _, tab := range l.Tabs {
		if tab != nil && tab.State.Page.Width > 0 && tab.State.Page.Height > 0 {
			return tab.State.Page
		}
	}
	return protocol.State{}
}

// revealActiveTab scrolls the strip so the active tab is visible.
func (l *SessionLoop) revealActiveTab(views []TabView) {
	if len(views) < 2 || l.Active == nil {
		l.tabOffset = 0
		return
	}
	index := l.activeIndex()
	if index < 0 {
		return
	}
	visible := l.visibleTabCount(views, l.tabOffset)
	if l.tabOffset > len(views)-visible {
		l.tabOffset = len(views) - visible
	}
	if l.tabOffset < 0 {
		l.tabOffset = 0
	}
	if index < l.tabOffset {
		l.tabOffset = index
	} else if index >= l.tabOffset+visible {
		l.tabOffset = index - visible + 1
	}
}

// activateTab moves the shared presenter to another tab. The old tab keeps its
// own state and overlay models but no longer owns any presenter pointers.
func (l *SessionLoop) activateTab(index int) {
	if index < 0 || index >= len(l.Tabs) {
		return
	}
	target := l.Tabs[index]
	if target == nil || target == l.Active {
		return
	}
	if l.Active != nil {
		l.rememberPreviousTab(l.Active)
		l.Active.DetachPresenter()
		l.setVisible(l.Active, false)
	}
	l.forgetTabHistory(target)
	l.Active = target
	l.patchZoom(target)
	_ = l.resizeTab(target)
	l.revealPending = l.needsResize(target)
	l.setVisible(target, true)
	l.revealActiveTab(l.tabViews())
	l.syncTabBar()
	if l.Presenter != nil {
		target.AttachPresenter(l.Presenter)
	}
}

func (l *SessionLoop) needsResize(tab *Tab) bool {
	if tab == nil || tab.Session == nil {
		return false
	}
	w, h := l.currentSize()
	top, contentH := ContentBounds(h)
	st := tab.State.Page
	return st.Width != w || st.Height != h || st.HeaderRows != top || st.Height-st.HeaderRows-1 != contentH
}

// patchZoom updates a tab's cached chrome snapshot before it is presented so a
// switch cannot briefly show the previous tab's zoom percentage.
func (l *SessionLoop) patchZoom(tab *Tab) {
	if tab == nil || l.zoomPercent <= 0 || tab.State.Page.ZoomPercent == l.zoomPercent {
		return
	}
	st := tab.State.Page
	st.ZoomPercent = l.zoomPercent
	st.ZoomLabel = fmt.Sprintf(" %d%% ", l.zoomPercent)
	tab.State.Page = st
}

func (l *SessionLoop) tabViews() []TabView {
	views := make([]TabView, 0, len(l.Tabs))
	for _, tab := range l.Tabs {
		if tab == nil {
			continue
		}
		views = append(views, tab.BarView(tab == l.Active))
	}
	return views
}

func (l *SessionLoop) syncTabBar() {
	views := l.tabViews()
	if len(views) < 2 {
		l.tabOffset = 0
	} else if visible := l.visibleTabCount(views, l.tabOffset); visible > 0 {
		if max := len(views) - visible; l.tabOffset > max {
			l.tabOffset = max
		}
		if l.tabOffset < 0 {
			l.tabOffset = 0
		}
	}
	offsetChanged := l.tabOffset != l.lastTabOffset
	viewsChanged := !tabsEqual(l.lastTabs, views)
	if !viewsChanged && !offsetChanged {
		return
	}
	if viewsChanged {
		l.lastTabs = append(l.lastTabs[:0], views...)
	}
	l.lastTabOffset = l.tabOffset
	for _, tab := range l.Tabs {
		if tab != nil {
			tab.TabBar = l.lastTabs
			tab.TabOffset = l.tabOffset
		}
	}
	if l.Presenter == nil {
		return
	}
	if viewsChanged {
		l.Presenter.SetTabs(views)
	}
	l.Presenter.SetTabOffset(l.tabOffset)
	if l.Active != nil && l.Active.Presenter == l.Presenter {
		_ = l.Presenter.Refresh()
	}
}

func (l *SessionLoop) showStatusError(message string) {
	if l.Active == nil {
		return
	}
	st := l.Active.State.Page
	st.FrontendChrome = true
	st.FooterLeft = " " + protocol.StripControls(message)
	st.FooterLevel = protocol.FooterError
	st.FooterStatusHit = false
	l.Active.State.Page = st
	if l.Presenter != nil {
		l.Presenter.SetState(st)
		_ = l.Presenter.Refresh()
	}
}

// resizeCommand builds the terminal resize plus the front-end-owned content
// rectangle.
func (l *SessionLoop) resizeCommand(w, h int) protocol.Resize {
	top, contentH := ContentBounds(h)
	return protocol.Resize{
		Width: w, Height: h, ContentTop: top, ContentHeight: contentH,
		CellW: l.CellW, CellH: l.CellH,
	}
}
