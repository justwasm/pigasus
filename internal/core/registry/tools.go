package registry

import (
	"context"

	"github.com/justwasm/pigasus/internal/backend"
)

// ToolProvider supplies a group of tool declarations and an executor.
type ToolProvider interface {
	Name() string
	Declarations() []backend.ToolDecl
	Run(ctx context.Context, call backend.ToolCall) (string, error)
}

// Tool represents a single executable tool with a wire declaration.
type Tool interface {
	Name() string
	Decl() backend.ToolDecl
	Run(ctx context.Context, args []byte) (string, error)
}
