// Package bashmode is a backward-compatible facade over internal/features/tools/bashmode.
package bashmode

import (
	featurebash "github.com/justwasm/pigasus/internal/features/tools/bashmode"
)

var (
	HeadRow      = featurebash.HeadRow
	BodyRows     = featurebash.BodyRows
	OutputRows   = featurebash.OutputRows
	ContRows     = featurebash.ContRows
	BodyCap      = featurebash.BodyCap
	MarkerColour = featurebash.MarkerColour
	BlockLines   = featurebash.BlockLines
	PromptGlyph  = featurebash.PromptGlyph
	TrimOutput   = featurebash.TrimOutput
)
