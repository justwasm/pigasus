package modular

import (
	"github.com/justwasm/pigasus/internal/core/registry"
)

// TurnInput carries input parameters for one agent execution turn.
type TurnInput = registry.TurnInput

// TurnResult summarizes the result of an agent execution turn.
type TurnResult = registry.TurnResult

// Engine defines the pluggable agent loop engine interface.
type Engine = registry.Engine
