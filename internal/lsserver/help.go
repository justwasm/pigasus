// Package lsserver — help.go: the reference's help output and its
// argument-vector rules.
//
// Two things about the reference's flag handling are not Go's `flag`
// package's behaviour, and both are observable:
//
//   - The help text is the one-line form — `  -name=value: usage`.
//     Go moved the usage onto its own line in Go 1.21, so the clone
//     prints the list itself instead of calling PrintDefaults.
//
//   - The parser has its own rules. `--help` asks for the help and
//     exits 2 (Go exits 0); an unknown flag is reported as
//     "flags provided but not defined: -a -b" (Go says "flag
//     provided but not defined: -a" and stops at the first one);
//     and `--help <pattern>` prints the help of the flags whose name
//     contains the pattern.
//
// Evidence grade: 一手 — every rule below was observed against the
// reference binary on this machine (out/langserver/help.txt,
// out/langserver/flags.txt).
package lsserver

import (
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// PrintUsage writes the reference's usage to w.
//
// An empty pattern prints every flag. A pattern prints the flags
// whose name contains it — the reference's "help for one flag" mode.
// A pattern that matches nothing gets the reference's own complaint.
func PrintUsage(fs *flag.FlagSet, w io.Writer, pattern string) {
	if pattern == "" {
		fmt.Fprintf(w, "Usage of %s:\n", fs.Name())
		PrintDefaults(fs, w)
		return
	}
	matches := matchingFlags(fs, pattern)
	if len(matches) == 0 {
		fmt.Fprintf(w, "flag: no flags match help pattern %q\n", pattern)
		return
	}
	fmt.Fprintf(w, "Usage of %s:\n", fs.Name())
	for _, f := range matches {
		printFlag(w, f)
	}
}

// PrintDefaults writes the reference's flag list to w.
//
// The shape is the one Go's `flag` package used before Go 1.21:
//
//	-name=value: usage
//
// with the default quoted for a string flag.
func PrintDefaults(fs *flag.FlagSet, w io.Writer) {
	fs.VisitAll(func(f *flag.Flag) {
		printFlag(w, f)
	})
}

// printFlag writes one flag's line.
func printFlag(w io.Writer, f *flag.Flag) {
	fmt.Fprintf(w, "  -%s=%s: %s\n", f.Name, defaultLiteral(f), f.Usage)
}

// bareDefaultFlags are the flags the reference prints without quotes
// around their default.
//
// Go's one-line help — the shape the reference still uses — quotes the
// default of a string flag and leaves every other kind bare. The
// reference registers `-model_api_client_type` with a flag type of
// its own, so that one is bare too:
//
//	-model_api_client_type=ccpa: Which model client to use: ccpa or gemini. Defaults to ccpa.
//
// while its other string flags are quoted:
//
//	-app_data_dir="antigravity-ide": Path where application data is stored, relative to GeminiDir
var bareDefaultFlags = map[string]bool{
	"model_api_client_type": true,
}

// defaultLiteral renders a flag's default value the way the
// reference prints it: a string is quoted, a bool and an int are
// bare.
//
// The value comes from the flag's own type rather than from its
// name, so a flag whose kind changes does not change the rendering.
func defaultLiteral(f *flag.Flag) string {
	if bareDefaultFlags[f.Name] {
		return f.DefValue
	}
	if getter, ok := f.Value.(flag.Getter); ok {
		if value, isString := getter.Get().(string); isString {
			return strconv.Quote(value)
		}
	}
	return f.DefValue
}

// matchingFlags returns the flags whose name contains the pattern.
//
// The match is a case-sensitive substring: the reference answers
// `--help port` with every flag whose name carries "port" and
// `--help CDP_PORT` with nothing.
func matchingFlags(fs *flag.FlagSet, pattern string) []*flag.Flag {
	var out []*flag.Flag
	fs.VisitAll(func(f *flag.Flag) {
		if strings.Contains(f.Name, pattern) {
			out = append(out, f)
		}
	})
	return out
}
