// Package textwrap is a backward-compatible facade over internal/ui/render/wrap.
package textwrap

import (
	"github.com/justwasm/pigasus/internal/ui/render/wrap"
)

var (
	Words = wrap.Words
	Break = wrap.Break
)
