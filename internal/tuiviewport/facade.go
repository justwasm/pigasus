// Package tuiviewport is a backward-compatible facade over internal/ui/foundations/viewport.
package tuiviewport

import (
	"github.com/justwasm/pigasus/internal/ui/foundations/viewport"
)

const (
	ViewOverflowRows     = viewport.ViewOverflowRows
	PinnedPromptJumpHint = viewport.PinnedPromptJumpHint
	SelHighlightOpen     = viewport.SelHighlightOpen
	SelHighlightClose    = viewport.SelHighlightClose
)

type (
	WrapSpan     = viewport.WrapSpan
	WrapSlot     = viewport.WrapSlot
	ComposedRows = viewport.ComposedRows
)

var (
	ScreenRows                  = viewport.ScreenRows
	ScreenRowCounts             = viewport.ScreenRowCounts
	ClipToScreenRows            = viewport.ClipToScreenRows
	ClipToScreenRowsPrefix      = viewport.ClipToScreenRowsPrefix
	IsUserPromptRow             = viewport.IsUserPromptRow
	StickyUserPromptRow         = viewport.StickyUserPromptRow
	RenderPinnedPrompt          = viewport.RenderPinnedPrompt
	IsScrollKey                 = viewport.IsScrollKey
	IsScrollWheel               = viewport.IsScrollWheel
	IsScrollMsg                 = viewport.IsScrollMsg
	SelectionRange              = viewport.SelectionRange
	SelectionText               = viewport.SelectionText
	ApplySelectionHighlight     = viewport.ApplySelectionHighlight
	ApplySelectionHighlightToRow = viewport.ApplySelectionHighlightToRow
	SelectionPass               = viewport.SelectionPass
	ComputeVisibleWindow        = viewport.ComputeVisibleWindow
	FormatVisibleRows           = viewport.FormatVisibleRows
)
