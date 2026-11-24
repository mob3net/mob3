package engine

import "browser/internal/protocol"

// Graphics modes are shared plain values; the renderer lives in the front end.
type GraphicsMode = protocol.GraphicsMode

const (
	GraphicsAuto   = protocol.GraphicsAuto
	GraphicsNone   = protocol.GraphicsNone
	GraphicsBlocks = protocol.GraphicsBlocks
	GraphicsKitty  = protocol.GraphicsKitty
	GraphicsITerm  = protocol.GraphicsITerm
)

// serializeSVG keeps the historical browser test surface while the
// implementation lives in inline_svg.go.
func serializeSVG(n *Node) string { return SerializeSVG(n) }
