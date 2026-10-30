// Package agentmessage is a backward-compatible facade over internal/ui/headless/msg.
package agentmessage

import (
	"github.com/justwasm/pigasus/internal/ui/headless/msg"
)

const (
	Preamble     = msg.Preamble
	Wrapper      = msg.Wrapper
	LineFormat   = msg.LineFormat
	PriorityHigh = msg.PriorityHigh
)

var (
	Render     = msg.Render
	RenderLine = msg.RenderLine
)
