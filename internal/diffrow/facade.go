// Package diffrow is a backward-compatible facade over internal/ui/views/diff/row.
package diffrow

import (
	"github.com/justwasm/pigasus/internal/ui/views/diff/row"
)

const (
	Context = row.Context
	Add     = row.Add
	Remove  = row.Remove
)

type (
	Kind       = row.Kind
	Row        = row.Row
	Layout     = row.Layout
	Style      = row.Style
	HunkHeader = row.HunkHeader
)

var (
	NewLayout        = row.NewLayout
	Marker           = row.Marker
	Summary          = row.Summary
	More             = row.More
	TranscriptStyle  = row.TranscriptStyle
	FileViewStyle    = row.FileViewStyle
	Render           = row.Render
	RenderStyled     = row.RenderStyled
	ParseHunkHeader  = row.ParseHunkHeader
)
