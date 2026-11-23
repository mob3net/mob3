package protocol

// GraphicsMode selects how page images are emitted.
type GraphicsMode int

const (
	// GraphicsAuto detects the best protocol from the environment.
	GraphicsAuto GraphicsMode = iota
	// GraphicsNone loads and draws no images.
	GraphicsNone
	// GraphicsBlocks uses Unicode half-block art.
	GraphicsBlocks
	// GraphicsKitty uses the kitty graphics protocol.
	GraphicsKitty
	// GraphicsITerm uses the iTerm2 inline-image protocol.
	GraphicsITerm
)
