package printmode

import (
	"flag"

	"github.com/justwasm/pigasus/internal/core/cliflags"
)

// RegisterPrintFlags registers the print-mode command-line flags on fs.
// It registers --output-format and its first-class alias --format if they
// are not already present on fs.
func RegisterPrintFlags(fs *flag.FlagSet) {
	if fs == nil {
		return
	}
	if fs.Lookup("output-format") == nil {
		fs.String("output-format", "", "Output format for print mode (text, json, stream-json)")
	}
	if fs.Lookup("format") == nil {
		fs.String("format", "", "Alias for --output-format (text, json, stream-json)")
	}
}

// flagBoolValue reads the bool value of a flag through its flag.Getter,
// falling back to the raw rendered string for a custom flag.Value that is
// not a Getter.
func flagBoolValue(fs *flag.FlagSet, name string) bool {
	if fs == nil {
		return false
	}
	f := fs.Lookup(name)
	if f == nil {
		return false
	}
	if b, ok := f.Value.(flag.Getter); ok {
		if val, ok := b.Get().(bool); ok {
			return val
		}
	}
	return f.Value.String() == "true"
}

func flagStringValue(fs *flag.FlagSet, name string) string {
	return cliflags.StringValue(fs, name)
}

func flagStringValues(fs *flag.FlagSet, name string) []string {
	return cliflags.GetterStringValues(fs, name)
}

