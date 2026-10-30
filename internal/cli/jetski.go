// Package cli contains the jetski-style CLI engine recovered from the v1.3.4
// binary.
//
// Background: the v1.3.4 binary does not use cobra/pflag at all (zero matches
// for spf13 in `strings`). Instead it uses two layers:
//  1. The Go standard library `flag` package for actual flag parsing.
//  2. An internal package `google3/third_party/jetski/cli/...` that
//     dispatches subcommands, formats help output, manages log files,
//     redirects terminal IO, and tracks step status.
//
// This file is a 1:1 reimplementation of that internal package for our
// recovered module. The goal is byte-for-byte parity with the v1.3.4 binary's
// `antigravity --help` output, not feature parity (we don't need every
// subcommand that the binary exposes).
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/justwasm/pigasus/internal/auth"
)

// JetskiCLI mirrors the internal `jetski/cli` engine. It owns:
//   - The root command (Name + Description)
//   - The ordered list of subcommands with their aliases and sub-subs
//   - The ordered list of global flags (with optional short aliases)
//   - The output writer for help text (defaults to os.Stdout)
//
// Parse() walks os.Args, dispatches to the matched subcommand, and returns
// the exit code (0 on success, 2 on usage error, matching cobra/pflag).
type JetskiCLI struct {
	Name        string
	Description string
	Version     string

	Subcommands []*JetskiSubcommand
	Flags       []*JetskiFlag

	// RootRun is invoked when no subcommand is matched. Mirrors the
	// top-level RunE on a cobra root command. nil means "print help
	// and exit 0".
	RootRun func(args []string) error

	// Lookups populated by Add* methods.
	byName  map[string]*JetskiSubcommand
	byAlias map[string]*JetskiSubcommand

	// Per-parse state.
	flagSet *flag.FlagSet
	args    []string

	// Output destination. Defaults to os.Stdout. Subcommand *results*
	// (mcp list, plugin list, remote-control status, ...) print here.
	Out io.Writer

	// ErrOut is the destination for usage/help text and error
	// diagnostics. Defaults to os.Stderr. The v1.3.5 binary writes
	// `--help`, subcommand `--help`, and every `Error:` line to
	// stderr, while normal results go to stdout — this field mirrors
	// that split. (Before cycle 96 everything went to Out.)
	ErrOut io.Writer
}

// JetskiSubcommand mirrors the description of a single (sub)subcommand
// entry in the help table. v1.3.4 binary stores these in static
// initialization maps; we just use a slice.
type JetskiSubcommand struct {
	Name        string
	Aliases     []string
	Description string
	Subs        []*JetskiSubSubcommand
	// Run is invoked when this subcommand is selected. nil means
	// "list subcommands and exit" (matches jetski/cli default behavior).
	Run func(args []string) error
	// RunJ, when non-nil, takes precedence over Run. Unlike Run it
	// receives the JetskiCLI itself, so a handler can write results to
	// j.Out and usage/errors to j.ErrOut (the split the v1.3.5 binary
	// uses) and read j.Name for the "Usage: <name> ..." preamble.
	RunJ func(j *JetskiCLI, args []string) error
	// Hidden hides the subcommand from help output.
	Hidden bool
	// Help is the full usage block printed (to ErrOut) when the user
	// passes -h/--help (or, for parent commands, when a help arg is
	// present). Empty means "run the handler and let it decide".
	Help string
	// HelpExit is the exit code returned after printing Help. The
	// v1.3.5 binary uses 0 for almost everything; `update --help`
	// (stdlib flag) exits 2.
	HelpExit int
	// UnknownSubError, when non-empty, is the message printed for an
	// unrecognised sub-subcommand, with %s substituted by the name.
	// When set, the Help block (if any) is printed first. Empty means
	// the default `Error: unknown <name> subcommand "<x>"` line with no
	// usage dump. `plugin` overrides it to match the v1.3.5 binary's
	// `usage + Error: unknown command: <x>`.
	UnknownSubError string
}

// JetskiSubSubcommand mirrors the description of a sub-subcommand (e.g.
// `plugin list`, `mcp add`). v1.3.4 binary's subcommands with children
// dispatch to these.
type JetskiSubSubcommand struct {
	Name        string
	Description string
	Run         func(args []string) error
	// RunJ, when non-nil, takes precedence over Run and receives the
	// JetskiCLI (see JetskiSubcommand.RunJ).
	RunJ   func(j *JetskiCLI, args []string) error
	Hidden bool
	// Help is the full usage block printed (to ErrOut) on -h/--help.
	// Empty means the Run handler inspects the args itself (needed for
	// `plugin list --help`, which is an *error*, not a help request).
	Help     string
	HelpExit int
}

// JetskiFlag mirrors a single (sub)command-level flag. v1.3.4 binary stores
// these as `flag.Flag` instances on a `*flag.FlagSet`; we keep a slightly
// higher-level descriptor so the help table can be rendered without
// actually registering the flag yet.
type JetskiFlag struct {
	Name     string // long form, e.g. "add-dir"
	Short    string // single-letter alias, e.g. "c" (empty for none)
	Type     string // "string", "bool", "duration"
	Def      string // default value as a string
	Usage    string // one-line description
	Required bool
	// Hidden keeps the flag out of the --help table while leaving it
	// fully parseable. Used for openagy-only extras so --help stays
	// byte-identical to the v1.3.5 binary (24 flags).
	Hidden bool
	// Dest is the actual variable the flag is bound to. After Parse
	// the caller can read the value through Dest. Internally this is
	// a *bool / *string / *time.Duration matching Type.
	Dest any
}

// NewJetski builds a root CLI with the given name and version. The
// Description is shown in the help header.
func NewJetski(name, version string) *JetskiCLI {
	return &JetskiCLI{
		Name:    name,
		Version: version,
		Out:     os.Stdout,
		ErrOut:  os.Stderr,
		byName:  map[string]*JetskiSubcommand{},
		byAlias: map[string]*JetskiSubcommand{},
	}
}

// AddSubcommand registers a subcommand with the root CLI.
func (j *JetskiCLI) AddSubcommand(s *JetskiSubcommand) {
	j.Subcommands = append(j.Subcommands, s)
	j.byName[s.Name] = s
	for _, a := range s.Aliases {
		j.byAlias[a] = s
	}
}

// AddFlag registers a global flag with the root CLI. Must be called before
// Parse.
func (j *JetskiCLI) AddFlag(f *JetskiFlag) {
	j.Flags = append(j.Flags, f)
}

// Parse walks os.Args, dispatches to the matched subcommand, and returns
// the exit code. If os.Args is empty, the root command's Run is invoked
// with no args.
func (j *JetskiCLI) Parse(args []string) int {
	fs, helpFlag, hFlag := j.initFlagSet()
	if err := fs.Parse(args); err != nil {
		msg := strings.Replace(err.Error(), "flag provided but not defined:", "flags provided but not defined:", 1)
		fmt.Fprintln(j.ErrOut, msg)
		j.PrintHelpTo(j.ErrOut)
		return 2
	}
	remaining := fs.Args()

	if v := getBoolFlag(fs, "version"); v != nil && *v {
		fmt.Fprint(j.Out, j.Version)
		return 0
	}
	if *helpFlag || *hFlag {
		j.PrintHelpTo(j.ErrOut)
		return 0
	}

	if v := flagStrValue(fs, "settings"); v != "" {
		overlay, err := auth.ParseAndValidateSettingsFlag(v)
		if err != nil {
			fmt.Fprintf(j.ErrOut, "Error: %s\n", err)
			return 1
		}
		auth.SetSessionSettingsOverlay(overlay)
	}

	if v := flagStrValue(fs, "input-format"); v != "" && v != "text" && v != "stream-json" {
		fmt.Fprintf(j.ErrOut, "Error: unknown --input-format %q (want text or stream-json)\n", v)
		return 2
	}
	if v := flagStrValue(fs, "input-format"); v == "stream-json" && flagStrValue(fs, "output-format") != "stream-json" {
		fmt.Fprintf(j.ErrOut, "Error: --input-format %s requires --output-format %s\n", v, "stream-json")
		return 2
	}

	if v := flagStrValue(fs, "json-schema"); strings.TrimSpace(v) != "" {
		outFmt := flagStrValue(fs, "output-format")
		if outFmt != "json" && outFmt != "stream-json" {
			fmt.Fprintln(j.ErrOut, "Error: --json-schema can only be used when --output-format is 'json' or 'stream-json'")
			return 1
		}
	}

	printFlag := firstSetFlag(fs, "print", "prompt", "p")
	interFlag := firstSetFlag(fs, "prompt-interactive", "i")
	if printFlag != "" || interFlag != "" {
		if code, ok := j.validatePrintFlags(fs, printFlag, interFlag, remaining); !ok {
			return code
		}
		WarnMode(j.ErrOut, j.FlagMode())
		if printFlag != "" && j.FlagMode() == "auto" {
			fmt.Fprintln(j.ErrOut, AutoUnavailableWarning("auto"))
		}
	}

	return j.dispatchArgs(remaining, printFlag == "" && interFlag == "")
}

func (j *JetskiCLI) initFlagSet() (*flag.FlagSet, *bool, *bool) {
	fs := flag.NewFlagSet(j.Name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	j.flagSet = fs

	var helpFlag, hFlag bool
	fs.BoolVar(&helpFlag, "help", false, "Show help")
	fs.BoolVar(&hFlag, "h", false, "Show help")

	j.registerFlags(fs)
	return fs, &helpFlag, &hFlag
}

func (j *JetskiCLI) validatePrintFlags(fs *flag.FlagSet, printFlag, interFlag string, remaining []string) (int, bool) {
	for _, n := range []string{printFlag, interFlag} {
		if n == "" {
			continue
		}
		v := flagStrValue(fs, n)
		if v != "" && isKnownFlagToken(fs, v) {
			fmt.Fprintf(j.ErrOut, "Error: %s took %q as its prompt, so the intended prompt was left as an argument and ignored.\n", flagDisplayName(n), v)
			fmt.Fprintf(j.ErrOut, "Attach the prompt to the flag (%s='your prompt') and move %s elsewhere on the command line.\n", flagDisplayName(n), v)
			return 2, false
		}
	}
	if printFlag != "" && interFlag != "" {
		fmt.Fprintln(j.ErrOut, "error: --print and --prompt-interactive are mutually exclusive")
		return 2, false
	}
	if printFlag != "" && flagStrValue(fs, printFlag) == "" {
		fmt.Fprintf(j.ErrOut, "error: Error: empty prompt. Usage: %s --print \"your prompt here\"\n", j.Name)
		return 1, false
	}
	if len(remaining) > 0 {
		fmt.Fprintf(j.ErrOut, "Error: unexpected argument %q.\n", remaining[0])
		fmt.Fprintln(j.ErrOut, "Prompts are read only from -p/--print, -i/--prompt-interactive, or stdin, so this argument would have been ignored.")
		return 2, false
	}
	if v := flagStrValue(fs, "input-format"); v != "" && v != "text" && v != "stream-json" {
		fmt.Fprintf(j.ErrOut, "Error: unknown --input-format %q (want text or stream-json)\n", v)
		return 2, false
	}
	if v := flagStrValue(fs, "input-format"); v == "stream-json" && flagStrValue(fs, "output-format") != "stream-json" {
		fmt.Fprintf(j.ErrOut, "Error: --input-format %s requires --output-format %s\n", v, "stream-json")
		return 2, false
	}
	return 0, true
}

func (j *JetskiCLI) dispatchArgs(remaining []string, lookForSubcommand bool) int {
	if lookForSubcommand {
		if len(remaining) > 0 {
			name := remaining[0]
			if sub, ok := j.lookup(name); ok {
				return j.dispatch(sub, remaining[1:])
			}
			fmt.Fprintf(j.ErrOut, "Error: unexpected argument %q.\n", name)
			fmt.Fprintln(j.ErrOut, "Prompts are read only from -p/--print, -i/--prompt-interactive, or stdin, so this argument would have been ignored.")
			return 2
		}
		WarnMode(j.ErrOut, j.FlagMode())
	}

	if j.RootRun != nil {
		if err := j.RootRun(remaining); err != nil {
			return j.handleRunErr(err)
		}
		return 0
	}
	return 0
}

// isHelpArg reports whether arg is the stdlib/jetski help form.
func isHelpArg(arg string) bool { return arg == "-h" || arg == "--help" }

// dispatch runs the subcommand, with optional sub-sub dispatch.
func (j *JetskiCLI) dispatch(s *JetskiSubcommand, args []string) int {
	if len(args) > 0 && isHelpArg(args[0]) && s.Help != "" {
		fmt.Fprint(j.ErrOut, s.Help)
		return s.HelpExit
	}
	if len(s.Subs) == 0 {
		return j.dispatchLeaf(s, args)
	}
	if len(args) == 0 {
		return j.dispatchParentNoArgs(s)
	}
	return j.dispatchSubcommand(s, args)
}

func (j *JetskiCLI) dispatchLeaf(s *JetskiSubcommand, args []string) int {
	if s.RunJ != nil {
		return j.handleRunErr(s.RunJ(j, args))
	}
	if s.Run == nil {
		fmt.Fprintf(j.ErrOut, "%s %s: no behavior registered\n", j.Name, s.Name)
		return 2
	}
	if err := s.Run(args); err != nil {
		return j.handleRunErr(err)
	}
	return 0
}

func (j *JetskiCLI) dispatchParentNoArgs(s *JetskiSubcommand) int {
	if s.RunJ != nil {
		return j.handleRunErr(s.RunJ(j, nil))
	}
	if s.Run != nil {
		if err := s.Run(nil); err != nil {
			return j.handleRunErr(err)
		}
		return 0
	}
	if s.Help != "" {
		fmt.Fprint(j.ErrOut, s.Help)
		return s.HelpExit
	}
	for _, ss := range s.Subs {
		if !ss.Hidden {
			fmt.Fprintf(j.Out, "  %s\n", ss.Name)
		}
	}
	return 0
}

func (j *JetskiCLI) dispatchSubcommand(s *JetskiSubcommand, args []string) int {
	name := args[0]
	for _, ss := range s.Subs {
		if ss.Name == name || strings.EqualFold(ss.Name, name) {
			if len(args) > 1 && isHelpArg(args[1]) && ss.Help != "" {
				fmt.Fprint(j.ErrOut, ss.Help)
				return ss.HelpExit
			}
			if ss.RunJ != nil {
				return j.handleRunErr(ss.RunJ(j, args[1:]))
			}
			if err := ss.Run(args[1:]); err != nil {
				return j.handleRunErr(err)
			}
			return 0
		}
	}
	if s.UnknownSubError != "" {
		if s.Help != "" {
			fmt.Fprint(j.ErrOut, s.Help)
		}
		fmt.Fprintf(j.ErrOut, s.UnknownSubError+"\n", name)
		return 1
	}
	fmt.Fprintf(j.ErrOut, "Error: unknown %s subcommand %q\n", s.Name, name)
	return 1
}

// handleRunErr renders a Run-handler error and returns the process
// exit code. The exitCodeError sentinel carries an explicit code;
// everything else is a plain failure (exit 1) printed to stderr.
func (j *JetskiCLI) handleRunErr(err error) int {
	if err == nil {
		return 0
	}
	var ece *exitCodeError
	if errors.As(err, &ece) {
		if ece.msg != "" {
			fmt.Fprintln(j.ErrOut, ece.msg)
		}
		return ece.code
	}
	fmt.Fprintln(j.ErrOut, err.Error())
	return 1
}

func (j *JetskiCLI) lookup(name string) (*JetskiSubcommand, bool) {
	if s, ok := j.byName[name]; ok {
		return s, true
	}
	if s, ok := j.byAlias[name]; ok {
		return s, true
	}
	return nil, false
}

// FlagSet returns the parsed *flag.FlagSet from the most recent Parse call.
// Callers (cmd/wagyu) use this to read flag values via fs.GetBool /
// fs.GetString. Returns nil before Parse.
func (j *JetskiCLI) FlagSet() *flag.FlagSet { return j.flagSet }

// PrintHelp renders the help table to j.Out (kept for direct callers
// such as tests). The `-h/--help` paths call PrintHelpTo(j.ErrOut).
func (j *JetskiCLI) PrintHelp() { j.PrintHelpTo(j.Out) }

// PrintHelpTo renders the help table to w. Format mirrors v1.3.5 binary
// (which uses Go stdlib `flag.PrintDefaults`):
//
//	Usage of antigravity:
//	  --add-dir              description (default [])
//	  --agent                description
//	  ...
//	Available subcommands:
//	  agent         description
//	  ...
//
// We mimic this layout because the recovered module's test rig
// (TestJetskiCLI_1to1) compares line counts to v1.3.5 binary.
func (j *JetskiCLI) PrintHelpTo(w io.Writer) {
	fmt.Fprintf(w, "Usage of %s:\n", j.Name)
	if len(j.Flags) > 0 {
		j.printHelpFlags(w)
	}
	if len(j.Subcommands) > 0 {
		j.printHelpSubcommands(w)
	}
}

func (j *JetskiCLI) printHelpFlags(w io.Writer) {
	flags := append([]*JetskiFlag{}, j.Flags...)
	sort.Slice(flags, func(i, k int) bool { return flags[i].Name < flags[k].Name })
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, f := range flags {
		if f.Usage == "" || f.Hidden {
			continue
		}
		var nameCol string
		if len(f.Name) == 1 {
			nameCol = "-" + f.Name
		} else {
			nameCol = "--" + f.Name
		}
		usage := f.Usage
		if f.Def != "" && f.Def != "false" {
			usage = usage + " (default " + f.Def + ")"
		}
		fmt.Fprintf(tw, "  %s\t%s\n", nameCol, usage)
	}
	tw.Flush()
}

func (j *JetskiCLI) printHelpSubcommands(w io.Writer) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Available subcommands:")
	visible := []*JetskiSubcommand{}
	for _, s := range j.Subcommands {
		if !s.Hidden {
			visible = append(visible, s)
		}
	}
	sort.Slice(visible, func(i, k int) bool { return visible[i].Name < visible[k].Name })
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, s := range visible {
		fmt.Fprintf(tw, "  %s\t%s\n", s.Name, s.Description)
	}
	tw.Flush()
}
