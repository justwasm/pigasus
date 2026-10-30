// Package difffold is a backward-compatible facade over internal/ui/views/diff/fold.
package difffold

import (
	"github.com/justwasm/pigasus/internal/ui/views/diff/fold"
)

type (
	Hunk   = fold.Hunk
	Layout = fold.Layout
)

var (
	Count         = fold.Count
	FoldHint      = fold.FoldHint
	SplitHunks    = fold.SplitHunks
	RenderBody    = fold.RenderBody
	HunkIdxForRow = fold.HunkIdxForRow
)
