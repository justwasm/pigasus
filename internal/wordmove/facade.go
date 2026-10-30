// Package wordmove is a backward-compatible facade over internal/ui/editor/wordmove.
package wordmove

import (
	"github.com/justwasm/pigasus/internal/ui/editor/wordmove"
)

var (
	BoundaryBack = wordmove.BoundaryBack
	BoundaryFwd  = wordmove.BoundaryFwd
	MoveLeft     = wordmove.MoveLeft
	MoveRight    = wordmove.MoveRight
	Move         = wordmove.Move
	SplitLines   = wordmove.SplitLines
)
