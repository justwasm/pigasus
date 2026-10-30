// Package featdiff is a backward-compatible facade over internal/ui/views/diff/feat.
package featdiff

import (
	"github.com/justwasm/pigasus/internal/ui/views/diff/feat"
)

const (
	Aligned  = feat.Aligned
	Diverged = feat.Diverged
	Gap      = feat.Gap
)

type (
	Table  = feat.Table
	Entry  = feat.Entry
	Status = feat.Status
)

var (
	RunCLI         = feat.RunCLI
	WriteTextTable = feat.WriteTextTable
	CheckOrCross   = feat.CheckOrCross
	PanelTitle     = feat.PanelTitle
	PanelDoc       = feat.PanelDoc
	SourceLoaded   = feat.SourceLoaded
	SourceData     = feat.SourceData
	SourcePath     = feat.SourcePath
	SortedEntries  = feat.SortedEntries
)
