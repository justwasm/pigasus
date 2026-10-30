package modular

import (
	"github.com/justwasm/pigasus/internal/core/registry"
)

// ToolProvider supplies a group of tool declarations and an executor.
type ToolProvider = registry.ToolProvider

// Tool represents a single executable tool with a wire declaration.
type Tool = registry.Tool
