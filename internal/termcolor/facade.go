// Package termcolor is a backward-compatible facade over internal/ui/render/color.
package termcolor

import (
	"github.com/justwasm/pigasus/internal/ui/render/color"
)

var (
	Resolve  = color.Resolve
	StripSGR = color.StripSGR
)
