package engine

import "browser/internal/frontend"

func ParseGraphicsMode(s string) GraphicsMode { return frontend.ParseGraphicsMode(s) }

func DetectGraphics(mode GraphicsMode) GraphicsMode { return frontend.DetectGraphics(mode) }
