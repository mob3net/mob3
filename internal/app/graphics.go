package app

import (
	"browser/internal/engine"
	"browser/internal/frontend"
	"browser/internal/protocol"
)

// nativeGraphicsMode reports the native protocol the terminal supports, or
// GraphicsNone when only half-block art is available.
func nativeGraphicsMode(requested frontend.GraphicsMode) frontend.GraphicsMode {
	current := frontend.DetectGraphics(requested)
	if current == frontend.GraphicsKitty || current == frontend.GraphicsITerm {
		return current
	}
	if detected := frontend.DetectGraphics(frontend.GraphicsAuto); detected == frontend.GraphicsKitty || detected == frontend.GraphicsITerm {
		return detected
	}
	return frontend.GraphicsNone
}

// capabilitiesFor reports the terminal features the engine may assume. Native
// image placement is still performed by the front end.
func capabilitiesFor(native frontend.GraphicsMode, cellW, cellH int) protocol.Capabilities {
	var caps protocol.Capabilities
	switch native {
	case frontend.GraphicsKitty:
		caps |= protocol.CapKittyGraphics
	case frontend.GraphicsITerm:
		caps |= protocol.CapITermGraphics
	}
	if g := frontend.NewGFXRenderer(native, cellW, cellH); g != nil && g.TextSizing() {
		caps |= protocol.CapKittyText
	}
	return caps
}

// graphicsModesFor records the i-key cycle: native inline graphics when
// available, then blocks, then none.
func graphicsModesFor(requested frontend.GraphicsMode) []engine.GraphicsMode {
	native := nativeGraphicsMode(requested)
	modes := make([]engine.GraphicsMode, 0, 3)
	if native == frontend.GraphicsKitty || native == frontend.GraphicsITerm {
		modes = append(modes, native)
	}
	return append(modes, frontend.GraphicsBlocks, frontend.GraphicsNone)
}
