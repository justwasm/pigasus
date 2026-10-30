// Package builtinskills is a backward-compatible facade over internal/features/tools/skills.
package builtinskills

import (
	"github.com/justwasm/pigasus/internal/features/tools/skills"
)

var (
	Dirs    = skills.Dirs
	Install = skills.Install
)
