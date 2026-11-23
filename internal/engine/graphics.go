package engine

import (
	"strings"

	"browser/internal/protocol"
)

// Graphics modes are shared plain values; the renderer is injected by the app.
type GraphicsMode = protocol.GraphicsMode

const (
	GraphicsAuto   = protocol.GraphicsAuto
	GraphicsNone   = protocol.GraphicsNone
	GraphicsBlocks = protocol.GraphicsBlocks
	GraphicsKitty  = protocol.GraphicsKitty
	GraphicsITerm  = protocol.GraphicsITerm
)

// Graphics is the terminal image renderer the app injects.
type Graphics interface {
	Mode() GraphicsMode
	TextSizing() bool
	BeginFrame(*strings.Builder)
	ClearScreen(*strings.Builder)
	Clear(*strings.Builder)
	PlaceAll(*strings.Builder, []protocol.Placement)
}

// serializeSVG keeps the historical browser test surface while the
// implementation lives in the
func serializeSVG(n *Node) string { return SerializeSVG(n) }
