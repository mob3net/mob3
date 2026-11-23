package engine

import "browser/internal/frontend"

func NewGFXRenderer(mode GraphicsMode, cellW, cellH int) *frontend.GFX {
	return frontend.NewGFXRenderer(mode, cellW, cellH)
}

func ParseGraphicsMode(s string) GraphicsMode { return frontend.ParseGraphicsMode(s) }

func DetectGraphics(mode GraphicsMode) GraphicsMode { return frontend.DetectGraphics(mode) }

func newTestGraphics(mode GraphicsMode, cellW, cellH int) Graphics {
	g := NewGFXRenderer(mode, cellW, cellH)
	if g == nil {
		return nil
	}
	return g
}
