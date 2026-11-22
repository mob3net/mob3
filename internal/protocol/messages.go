package protocol

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

// Init creates or reconfigures a session. Width and Height are the content
// rectangle, excluding chrome. CellW and CellH are CSS pixels per terminal
// cell, already adjusted for zoom.
type Init struct {
	Width, Height int
	CellW, CellH  int
	Zoom          float64
	Caps          Capabilities
	Flags         uint32
}

func (Init) commandKind() Kind { return CmdInit }

// Navigate starts a top-level load.
type Navigate struct {
	URL              string
	Push             bool
	RestoreScroll    int
	HasRestoreScroll bool
}

func (Navigate) commandKind() Kind { return CmdNavigate }

// Resize changes the content rectangle.
type Resize struct {
	Width, Height int
	CellW, CellH  int
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
	AppBack AppAction = iota + 1
	AppForward
	AppReload
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
)

// App requests an application action. Arg carries an action-specific value.
type App struct {
	Action AppAction
	Arg    int
}

func (App) commandKind() Kind { return CmdApp }

// Find updates the incremental page find.
type Find struct {
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

// Selection is the text of the current page selection.
type Selection struct {
	Text string
}

func (Selection) eventKind() Kind { return EvSelection }

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
	URL  string
	Name string
}

func (Download) eventKind() Kind { return EvDownload }

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
