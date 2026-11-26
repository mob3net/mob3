package protocol

import "fmt"

// Command is one message from the front end to a page session.
type Command struct {
	Session SessionID
	Seq     uint64
	Body    CommandBody
}

// Event is one message from a page session to the front end.
type Event struct {
	Session SessionID
	Seq     uint64
	Body    EventBody
}

// CommandBody is implemented by the protocol's command types. The interface is
// sealed so a new command is a deliberate protocol change.
type CommandBody interface {
	commandKind() Kind
}

// EventBody is implemented by the protocol's event types.
type EventBody interface {
	eventKind() Kind
}

// Capabilities are terminal features the front end has already detected.
type Capabilities uint32

const (
	// CapColor enables color output.
	CapColor Capabilities = 1 << iota
	// CapKittyGraphics enables native kitty image placements.
	CapKittyGraphics
	// CapITermGraphics enables iTerm2 image placements.
	CapITermGraphics
	// CapKittyText enables scaled text runs.
	CapKittyText
)

// Init flag bits.
const (
	InitNoJS uint32 = 1 << iota
	InitNoPDF
	InitNoImages
	InitHideFixed
	InitNoSticky
	InitHideBackgrounds
	InitNoBigText
)

// Init creates or reconfigures a session. Width and Height are the terminal
// size. ContentTop and ContentHeight are the front-end-owned page rectangle;
// zero values fall back to the engine's legacy chrome derivation for direct
// callers and tests. CellW and CellH are CSS pixels per terminal cell, already
// adjusted for zoom.
type Init struct {
	Width, Height             int
	ContentTop, ContentHeight int
	CellW, CellH              int
	Zoom                      float64
	Caps                      Capabilities
	Flags                     uint32
}

func (Init) commandKind() Kind { return CmdInit }

// Navigate starts a top-level load.
type Navigate struct {
	URL  string
	Push bool
}

func (Navigate) commandKind() Kind { return CmdNavigate }

// HistoryEntry is one front-end-owned session-history position as displayed on
// about:history. The engine does not store the list; it receives a bounded
// snapshot for the next history page.
type HistoryEntry struct {
	URL string
}

// History supplies the snapshot the engine renders for about:history. Current
// is the index of the active entry, or -1 when it is not in the snapshot.
type History struct {
	Entries []HistoryEntry
	Current int
}

func (History) commandKind() Kind { return CmdHistory }

// Restore asks the engine to return to a front-end history entry. The engine
// restores the cached page for EntryID when possible, resolves a same-document
// URL in place, or loads URL with Scroll as the restore offset.
type Restore struct {
	EntryID uint64
	URL     string
	Scroll  int
}

func (Restore) commandKind() Kind { return CmdRestore }

// Visibility tells a session whether its tab is active. An inactive session
// keeps loading and updating state, but does not draw frames, run JavaScript
// timers, stream deferred images or animate progress.
type Visibility struct {
	Active bool
}

func (Visibility) commandKind() Kind { return CmdVisibility }

// Graphics sets the image mode for one session. The front end keeps one mode
// for the whole browser and sends it to every tab.
type Graphics struct {
	Mode GraphicsMode
}

func (Graphics) commandKind() Kind { return CmdGraphics }

// Resize changes the terminal size and the front-end-owned content rectangle.
type Resize struct {
	Width, Height             int
	ContentTop, ContentHeight int
	CellW, CellH              int
}

func (Resize) commandKind() Kind { return CmdResize }

// Scroll moves the page viewport. Delta is relative; To is absolute when HasTo
// is set. Top and Bottom jump to the ends.
type Scroll struct {
	Delta  int
	To     int
	HasTo  bool
	Top    bool
	Bottom bool
}

func (Scroll) commandKind() Kind { return CmdScroll }

// Key is a decoded key token, matching the tokens the terminal decoder emits.
type Key struct {
	Token string
}

func (Key) commandKind() Kind { return CmdKey }

// Mouse is a pointer event in content-rectangle cells, zero-based. The front end
// translates terminal coordinates and strips the chrome rows before sending.
type Mouse struct {
	X, Y   int
	Button int
	Wheel  int
	Press  bool
	Motion bool
	Shift  bool
}

func (Mouse) commandKind() Kind { return CmdMouse }

// AppAction is an application-level action the engine must perform.
type AppAction uint8

const (
	AppReload AppAction = iota + 1
	AppStop
	AppToggleDark
	AppToggleReader
	AppToggleImages
	AppToggleFixed
	AppToggleSticky
	AppToggleBackgrounds
	AppZoomIn
	AppZoomOut
	AppZoomReset
	AppOpenURL
	AppOpenZoom
	AppToggleConsole
	AppToggleJS
	AppStatusDetails
	AppDownload
	AppHome
	AppOpenVimPrompt
	AppOpenViewMenu
	AppZoomSet
	// AppFrontendOverlay tells the engine a front-end window covers the page.
	// Arg is 1 while it is open and 0 when it closes; the engine then paints
	// native images as half-block art and scaled text at normal size.
	AppFrontendOverlay
)

// App requests an application action. Arg carries an action-specific value.
type App struct {
	Action AppAction
	Arg    int
}

func (App) commandKind() Kind { return CmdApp }

// FindScope selects the page or console HTML find.
type FindScope uint8

const (
	FindPage FindScope = iota + 1
	FindHTML
)

// Find updates the incremental find.
type Find struct {
	Scope FindScope
	Query string
	Next  bool
	Prev  bool
	Close bool
}

func (Find) commandKind() Kind { return CmdFind }

// Abort is delivered out of band so a busy script can be stopped without
// waiting for the command queue.
type Abort struct {
	Reason string
}

func (Abort) commandKind() Kind { return CmdAbort }

// Shutdown releases the session.
type Shutdown struct{}

func (Shutdown) commandKind() Kind { return CmdShutdown }

// Select asks the front end to display a page select popup. The engine blocks
// no timers; the front end answers with SelectResult.
type Select struct {
	ID       uint32
	Options  []string
	Selected int
}

func (Select) eventKind() Kind { return EvSelect }

// SelectResult answers a Select event.
type SelectResult struct {
	ID     uint32
	Index  int
	Cancel bool
}

func (SelectResult) commandKind() Kind { return CmdSelectResult }

// ConsoleOp is one front-end console action.
type ConsoleOp uint8

const (
	ConsolePane ConsoleOp = iota + 1
	ConsoleCycle
	ConsoleScroll
	ConsoleTop
	ConsoleBottom
	ConsoleFocus
	ConsoleCancelEdit
	ConsoleInput
	ConsoleCopy
	ConsoleQuit
	ConsoleCancelQuit
	ConsoleClose
)

// ConsoleAction applies one console operation.
type ConsoleAction struct {
	Op    ConsoleOp
	Pane  int
	Delta int
	Text  string
}

func (ConsoleAction) commandKind() Kind { return CmdConsoleAction }

// ConsoleView is the visible console screen: tabs, the active pane's visible
// lines, optional request summary, input row and footer.
type ConsoleHighlight struct {
	Line    int
	X, W    int
	Current bool
}

type ConsoleView struct {
	Width, Height int
	Pane          int
	Tabs          []string
	Lines         []string
	Summary       string
	Input         string
	Cursor        int
	Edit          bool
	Hint          string
	Footer        string
	Quit          bool
	FindActive    bool
	FindText      string
	FindCursor    int
	FindCurrent   int
	FindTotal     int
	Highlights    []ConsoleHighlight
}

func (ConsoleView) eventKind() Kind { return EvConsoleView }

// HintChar is one highlighted letter in a hint overlay.
type HintChar struct {
	R    rune
	X, Y int
}

// HintItem is one link or control hint.
type HintItem struct {
	Target int
	Label  string
	X, Y   int
	W, H   int
	Chars  []HintChar
	Link   bool
	Href   string
}

// HintMode selects what a completed hint does.
type HintMode uint8

const (
	// HintFollow follows the link in the current tab.
	HintFollow HintMode = iota + 1
	// HintNewTab opens a link in a background tab.
	HintNewTab
	// HintPreview shows the destination before following or copying it.
	HintPreview
)

// Hints asks the front end to display link hints.
type Hints struct {
	Mode   HintMode
	Prefix string
	Items  []HintItem
}

func (Hints) eventKind() Kind { return EvHints }

// Validate checks a Hints event from a compromised engine before the front end
// paints, copies or sends back any of its values.
func (h Hints) Validate(lim Limits) error {
	lim = lim.normalized()
	switch h.Mode {
	case HintFollow, HintNewTab, HintPreview:
	default:
		return fmt.Errorf("protocol: unknown hint mode %d", h.Mode)
	}
	if len(h.Prefix) > 64 || !SafeText(h.Prefix) {
		return fmt.Errorf("protocol: invalid hint prefix")
	}
	if len(h.Items) > lim.MaxLinks {
		return fmt.Errorf("protocol: too many hint items")
	}
	for i, item := range h.Items {
		if item.Target < 0 {
			return fmt.Errorf("protocol: hint %d has a negative target", i)
		}
		if len(item.Label) > 256 || !SafeText(item.Label) {
			return fmt.Errorf("protocol: hint %d has an invalid label", i)
		}
		if len(item.Href) > lim.MaxTextBytes || !SafeText(item.Href) {
			return fmt.Errorf("protocol: hint %d has an invalid href", i)
		}
		if item.X < 0 || item.Y < 0 || item.W < 0 || item.H < 0 ||
			item.X > lim.MaxFrameWidth || item.Y > lim.MaxFrameHeight ||
			item.W > lim.MaxFrameWidth || item.H > lim.MaxFrameHeight {
			return fmt.Errorf("protocol: hint %d is outside the frame", i)
		}
		if len(item.Chars) > lim.MaxCells {
			return fmt.Errorf("protocol: hint %d has too many characters", i)
		}
		for j, c := range item.Chars {
			if !SafeRune(c.R) || c.X < 0 || c.Y < 0 ||
				c.X > lim.MaxFrameWidth || c.Y > lim.MaxFrameHeight {
				return fmt.Errorf("protocol: hint %d character %d is invalid", i, j)
			}
		}
	}
	return nil
}

// HintResult answers a Hints event.
type HintResult struct {
	Target int
	Cancel bool
}

func (HintResult) commandKind() Kind { return CmdHintResult }

// DialogKind identifies a script dialog.
type DialogKind uint8

const (
	DialogAlert DialogKind = iota + 1
	DialogConfirm
	DialogPrompt
)

// DialogResult answers a Dialog event.
type DialogResult struct {
	ID   uint32
	OK   bool
	Text string
}

func (DialogResult) commandKind() Kind { return CmdDialogResult }

func (Frame) eventKind() Kind { return EvFrame }

func (State) eventKind() Kind { return EvState }

// Dialog asks the front end to display a script dialog. The engine blocks until
// the matching DialogResult arrives.
type Dialog struct {
	ID      uint32
	Kind    DialogKind
	Text    string
	Default string
}

func (Dialog) eventKind() Kind { return EvDialog }

// Links is the visible-link metadata for a frame version. The front end keeps
// the last copy and replaces it only when Frame.Version changes.
type Links struct {
	Version uint32
	Items   []Link
}

func (Links) eventKind() Kind { return EvLinks }

// Clipboard is text the engine wants copied.
type Clipboard struct {
	Text string
}

func (Clipboard) eventKind() Kind { return EvClipboard }

// ConsoleLevel classifies a console line.
type ConsoleLevel uint8

const (
	ConsoleLog ConsoleLevel = iota + 1
	ConsoleWarn
	ConsoleError
)

// Console is one page-console or engine message.
type Console struct {
	Level ConsoleLevel
	Text  string
}

func (Console) eventKind() Kind { return EvConsole }

// Crash reports a contained engine failure. The transport may also observe a
// dead child directly; this is the in-band explanation.
type Crash struct {
	Message string
}

func (Crash) eventKind() Kind { return EvCrash }

// HistoryCommit reports the final URL and title of a completed load. Push
// appends a new entry (merging into the current one when the URL is unchanged);
// otherwise the entry identified by EntryID is updated in place. The front end
// validates and renders these values instead of trusting State.URL/Title.
type HistoryCommit struct {
	EntryID uint64
	URL     string
	Scroll  int
	Push    bool
}

func (HistoryCommit) eventKind() Kind { return EvHistoryCommit }

// OpenTab asks the front end to open URL in a background tab. It backs
// middle-click on a link and does not change the active tab.
type OpenTab struct {
	URL string
}

func (OpenTab) eventKind() Kind { return EvOpenTab }

// LinkPreview asks the front end to show a link destination before following
// or copying it. Target is the engine link index used by FollowLink.
type LinkPreview struct {
	URL    string
	Target int
}

func (LinkPreview) eventKind() Kind { return EvLinkPreview }

// FollowLink follows the engine link at Target in the current tab.
type FollowLink struct {
	Target int
}

func (FollowLink) commandKind() Kind { return CmdFollowLink }

// FindResults reports the current match and the total. Rects are content
// viewport rectangles for highlighting.
type FindResults struct {
	Query   string
	Current int
	Total   int
	Rects   []Rect
}

func (FindResults) eventKind() Kind { return EvFindResults }

// Download asks the front end to offer a download hint.
type Download struct {
	URL     string
	Name    string
	Command string
}

func (Download) eventKind() Kind { return EvDownload }

// Notice asks the front end to show a small informational modal.
type Notice struct {
	Title   string
	Message string
}

func (Notice) eventKind() Kind { return EvNotice }

// ModalClose answers a Download or Notice modal.
type ModalClose struct {
	Kind   uint8
	Copied bool
	Chars  int
}

func (ModalClose) commandKind() Kind { return CmdModalClose }

const (
	ModalDownload uint8 = 1
	ModalNotice   uint8 = 2
)

// Quit tells the front end that the session wants the application to exit,
// after a confirmed quit key or a shutdown command.
type Quit struct{}

func (Quit) eventKind() Kind { return EvQuit }

// ClientError reports a contained front-end observation to the engine so it
// can show it in the in-app console. The message is already sanitized by the
// front end and may only be displayed, never interpreted as page content.
type ClientError struct {
	Message string
}

func (ClientError) commandKind() Kind { return CmdClientError }

// AutomationOp selects one MCP operation.
type AutomationOp uint8

const (
	AutomationNavigate AutomationOp = iota + 1
	AutomationSnapshot
	AutomationScreenshot
	AutomationRunCommand
)

// Automation asks the engine to run one MCP operation. Args is the JSON
// argument object for AutomationRunCommand; the engine validates it.
type Automation struct {
	Op    AutomationOp
	URL   string
	Scale int
	Name  string
	Args  []byte
}

func (Automation) commandKind() Kind { return CmdAutomation }

// AutomationResult answers one Automation command. Exactly one of Text or Data
// is meaningful for a successful operation; Err carries a handled failure.
type AutomationResult struct {
	Op   AutomationOp
	Text string
	Data []byte
	Err  string
}

func (AutomationResult) eventKind() Kind { return EvAutomationResult }
