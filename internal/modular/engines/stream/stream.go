package stream

import (
	appstream "github.com/justwasm/pigasus/internal/app/engine/stream"
)

// Engine is a facade for internal/app/engine/stream.Engine.
type Engine = appstream.Engine

// NewEngine returns a configured StreamEngine from internal/app/engine/stream.
func NewEngine() *Engine {
	return appstream.NewEngine()
}
