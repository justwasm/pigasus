package registry

import (
	"context"
	"flag"
	"io"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/core/lifecycle"
)

// TurnInput carries input parameters for one agent execution turn.
type TurnInput struct {
	Prompt            string
	Model             string
	Effort            string
	Messages          []backend.Message
	Tools             []backend.ToolDecl
	SystemPrompt      string
	FlagSet           *flag.FlagSet
	Backend           backend.Client
	Out               io.Writer
	ErrOut            io.Writer
	OnChunk           func(chunk backend.StreamChunk) error
	OnStateTransition func(from, to lifecycle.CascaderState, note string)
	ToolRunner        func(ctx context.Context, call backend.ToolCall) (string, error)
}

// TurnResult summarizes the result of an agent execution turn.
type TurnResult struct {
	Response     string
	Model        string
	InputTokens  int
	OutputTokens int
	TurnCount    int
}

// Engine defines the pluggable agent loop engine interface.
type Engine interface {
	Name() string
	Send(ctx context.Context, input TurnInput) (*TurnResult, error)
}
