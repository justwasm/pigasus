// Package autocont is a backward-compatible facade over internal/app/session/autocont.
package autocont

import (
	appautocont "github.com/justwasm/pigasus/internal/app/session/autocont"
)

const (
	StreamInterrupted  = appautocont.StreamInterrupted
	IterationCapFormat = appautocont.IterationCapFormat
	MaxDepth           = appautocont.MaxDepth
	MaxNetworkDepth    = appautocont.MaxNetworkDepth
)

var IterationCount = appautocont.IterationCount
