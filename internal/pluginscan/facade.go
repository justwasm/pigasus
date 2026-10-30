package pluginscan

import (
	"github.com/justwasm/pigasus/internal/features/plugins/scan"
)

type (
	Entry    = scan.Entry
	WalkFunc = scan.WalkFunc
)

var (
	IsDir         = scan.IsDir
	IsRegularFile = scan.IsRegularFile
	ReadEntries   = scan.ReadEntries
	Walk          = scan.Walk
	WalkLegacy    = scan.WalkLegacy
)
