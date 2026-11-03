// Package cli — flagset.go: a standalone flag set built from the
// standard specs.
//
// The root command owns a parser; a subcommand that runs a session of its
// own — the TUI attached to a server — needs the same flags without the
// root command's parser around them.
package cli

import (
	"flag"
	"io"
)

// NewFlagSet builds a stdlib flag set from the given specs.
//
// The set parses like the root command's: the same names, the same
// types, the same defaults. Its output is discarded, so a caller that
// wants the parse error on the terminal sets the output itself.
func NewFlagSet(name string, specs []FlagSpec) *flag.FlagSet {
	j := NewJetski(name, "")
	for _, s := range specs {
		j.AddFlag(&JetskiFlag{
			Name:     s.Name,
			Short:    s.Short,
			Type:     s.Type,
			Def:      s.Def,
			Usage:    s.Usage,
			Required: false,
			Hidden:   s.Hidden,
		})
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	j.registerFlags(fs)
	return fs
}
