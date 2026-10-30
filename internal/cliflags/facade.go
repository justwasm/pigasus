// Package cliflags is a backward-compatible facade over internal/ui/cli/flags.
package cliflags

import (
	"github.com/justwasm/pigasus/internal/ui/cli/flags"
)

var (
	WasSet                 = flags.WasSet
	BoolValue              = flags.BoolValue
	GetterBoolValue        = flags.GetterBoolValue
	IntValue               = flags.IntValue
	StringValue            = flags.StringValue
	GetterStringValue      = flags.GetterStringValue
	StringValues           = flags.StringValues
	GetterStringValues     = flags.GetterStringValues
	ResolveBoolAlias       = flags.ResolveBoolAlias
	ResolveBoolAliasGetter = flags.ResolveBoolAliasGetter
)
