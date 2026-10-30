// Package seasonal is a backward-compatible facade over internal/ui/views/seasonal.
package seasonal

import (
	"github.com/justwasm/pigasus/internal/ui/views/seasonal"
)

const (
	OptInKey       = seasonal.OptInKey
	RowName        = seasonal.RowName
	RowDescription = seasonal.RowDescription
)

type (
	Event  = seasonal.Event
	Art    = seasonal.Art
	Border = seasonal.Border
)

var (
	Current   = seasonal.Current
	Halloween = seasonal.Halloween
	Enabled   = seasonal.Enabled
)
