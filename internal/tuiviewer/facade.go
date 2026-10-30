// Package tuiviewer is a backward-compatible facade over internal/ui/foundations/viewer.
package tuiviewer

import (
	"github.com/justwasm/pigasus/internal/ui/foundations/viewer"
)

const (
	ChangelogTotalLines     = viewer.ChangelogTotalLines
	UsageQuotaLoadingTicks  = viewer.UsageQuotaLoadingTicks
)

type (
	Spec                  = viewer.Spec
	UsageQuotaTickMsg     = viewer.UsageQuotaTickMsg
	CompactSpinnerTickMsg = viewer.CompactSpinnerTickMsg
)

var (
	ChangelogBody          = viewer.ChangelogBody
	ChangelogWindowRows    = viewer.ChangelogWindowRows
	ChangelogExcerptRows   = viewer.ChangelogExcerptRows
	BuildChangelogRestRows = viewer.BuildChangelogRestRows
	ChangelogLegend        = viewer.ChangelogLegend
	DiffFirstChangedPath   = viewer.DiffFirstChangedPath
	FormatTurnRow          = viewer.FormatTurnRow
	Window                 = viewer.Window
	Counter                = viewer.Counter
	MaxOffset              = viewer.MaxOffset
	QuotaCountdown         = viewer.QuotaCountdown
	UsageQuotaTick         = viewer.UsageQuotaTick
	CompactSpinnerTick     = viewer.CompactSpinnerTick
)
