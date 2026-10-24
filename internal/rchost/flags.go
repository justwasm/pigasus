package rchost

import (
	"flag"

	"github.com/justwasm/pigasus/internal/cliflags"
)

func flagBoolValue(fs *flag.FlagSet, name string) bool {
	return cliflags.GetterBoolValue(fs, name)
}

func flagStringValue(fs *flag.FlagSet, name string) string {
	return cliflags.GetterStringValue(fs, name)
}

func flagStringValues(fs *flag.FlagSet, name string) []string {
	return cliflags.GetterStringValues(fs, name)
}
