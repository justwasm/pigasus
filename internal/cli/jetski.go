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
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
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
	// Build a fresh flag.FlagSet. v1.3.4 binary uses flag.ContinueOnError
	// for subcommands and flag.ExitOnError for the root.
	fs := flag.NewFlagSet(j.Name, flag.ContinueOnError)
	// Suppress stdlib's own error/usage printing. The v1.3.5 binary
	// prints its own "flags provided but not defined: -X" line followed
	// by the usage block, both on stderr — see the Parse-error branch
	// below. Letting stdlib also print would duplicate the usage block
	// and emit it on the wrong stream.
	fs.SetOutput(io.Discard)
	// Make stdlib's internal usage() a no-op: on a parse error stdlib
	// would call fs.Usage() (see failf) *and* we print our own
	// reworded error + usage block below, which double-printed the
	// whole block. All usage printing now happens in the error branch.
	fs.Usage = func() {}
	j.flagSet = fs

	// Register -h/--help locally (not in j.Flags, so they never show
	// up in the flag table). The v1.3.5 binary accepts both and writes
	// the usage block to stderr, exiting 0.
	var helpFlag, hFlag bool
	fs.BoolVar(&helpFlag, "help", false, "Show help")
	fs.BoolVar(&hFlag, "h", false, "Show help")

	// Register global flags. v1.3.4 binary uses short+long dual form
	// (e.g. "-c, --continue"); stdlib flag supports this with a comma
	// separator: flag.Bool("c", ..., "alias for --continue") then
	// flag.Bool("continue", ...). But to match v1.3.4 output we
	// register them as a single combined name like "c,continue".
	// Simpler: register twice (long+short) and let the flagSet merge.
	for _, f := range j.Flags {
		dest, _ := newDestFor(f.Type, f.Def)
		f.Dest = dest
		// Long form
		switch v := dest.(type) {
		case *bool:
			fs.BoolVar(v, f.Name, *v, f.Usage)
		case *string:
			fs.StringVar(v, f.Name, *v, f.Usage)
		case *int:
			fs.IntVar(v, f.Name, *v, f.Usage)
		case *time.Duration:
			fs.Var(&durationValue{v: v}, f.Name, f.Usage)
		default:
			// stringSlice
			fs.Var(v.(flag.Value), f.Name, f.Usage)
		}
		// Short alias (only if not empty AND not equal to the long name)
		if f.Short != "" && f.Short != f.Name {
			switch v := dest.(type) {
			case *bool:
				fs.BoolVar(v, f.Short, *v, "alias for --"+f.Name)
			case *string:
				fs.StringVar(v, f.Short, *v, "alias for --"+f.Name)
			case *int:
				fs.IntVar(v, f.Short, *v, "alias for --"+f.Name)
			case *time.Duration:
				fs.Var(&durationValue{v: v}, f.Short, "alias for --"+f.Name)
			default:
				fs.Var(v.(flag.Value), f.Short, "alias for --"+f.Name)
			}
		}
	}

	if err := fs.Parse(args); err != nil {
		// v1.3.5 binary: print "<flags provided but not defined: -X>"
		// then the usage block, both to stderr, and exit 2. stdlib's
		// wording is "flag provided but not defined: -X" (singular);
		// the v1.3.5 engine rewords it to the plural "flags".
		msg := strings.Replace(err.Error(), "flag provided but not defined:", "flags provided but not defined:", 1)
		fmt.Fprintln(j.ErrOut, msg)
		j.PrintHelpTo(j.ErrOut)
		return 2
	}
	remaining := fs.Args()

	// Handle version flag explicitly (stdlib flag does not auto-print).
	if v := getBoolFlag(fs, "version"); v != nil && *v {
		fmt.Fprint(j.Out, j.Version)
		return 0
	}

	// Handle help flag explicitly.
	if helpFlag || hFlag {
		j.PrintHelpTo(j.ErrOut)
		return 0
	}

	// Cycle 101: print-mode argument validation. The recovered 1.3.6 binary's
	// -p/--print/--prompt and -i/--prompt-interactive are *string* flags whose
	// value is the prompt; a positional argument is never a prompt. Every
	// message below is captured verbatim from the reference.
	printFlag := firstSetFlag(fs, "print", "prompt", "p")
	interFlag := firstSetFlag(fs, "prompt-interactive", "i")
	if printFlag != "" || interFlag != "" {
		// (a) The value is actually a flag the user meant to pass. The
		// reference only rejects *registered* flag tokens: `-p -c` errors
		// while `-p -x`, `-p --zzz` and `-p --` are taken as the prompt.
		for _, n := range []string{printFlag, interFlag} {
			if n == "" {
				continue
			}
			v := flagStrValue(fs, n)
			if v != "" && isKnownFlagToken(fs, v) {
				fmt.Fprintf(j.ErrOut, "Error: %s took %q as its prompt, so the intended prompt was left as an argument and ignored.\n", flagDisplayName(n), v)
				fmt.Fprintf(j.ErrOut, "Attach the prompt to the flag (%s='your prompt') and move %s elsewhere on the command line.\n", flagDisplayName(n), v)
				return 2
			}
		}
		// (b) The two prompt sources are mutually exclusive.
		if printFlag != "" && interFlag != "" {
			fmt.Fprintln(j.ErrOut, "error: --print and --prompt-interactive are mutually exclusive")
			return 2
		}
		// (c) An explicit empty prompt is an error, not a no-op.
		if printFlag != "" && flagStrValue(fs, printFlag) == "" {
			fmt.Fprintf(j.ErrOut, "error: Error: empty prompt. Usage: %s --print \"your prompt here\"\n", j.Name)
			return 1
		}
		// (d) Positional arguments are never prompts.
		if len(remaining) > 0 {
			fmt.Fprintf(j.ErrOut, "Error: unexpected argument %q.\n", remaining[0])
			fmt.Fprintln(j.ErrOut, "Prompts are read only from -p/--print, -i/--prompt-interactive, or stdin, so this argument would have been ignored.")
			return 2
		}
		// (e) --input-format is validated before the run starts.
		if v := flagStrValue(fs, "input-format"); v != "" && v != "text" && v != "stream-json" {
			fmt.Fprintf(j.ErrOut, "Error: unknown --input-format %q (want text or stream-json)\n", v)
			return 2
		}
	}

	// Dispatch to subcommand if first remaining arg matches one. We
	// only do this lookup if no top-level "this is a prompt" flag was
	// set (--print, --prompt, -p). Otherwise `antigravity --print
	// "say hi"` would treat "say" as an unknown subcommand and
	// reject the rest, which is not how the v4 binary behaves. The
	// v4 binary routes the positional args straight to the root Run
	// whenever one of those flags is on.
	lookForSubcommand := printFlag == "" && interFlag == ""
	if lookForSubcommand {
		if len(remaining) > 0 {
			name := remaining[0]
			if sub, ok := j.lookup(name); ok {
				return j.dispatch(sub, remaining[1:])
			}
			// Unknown subcommand. v1.3.5 binary exits 2 with a stderr
			// error line (no help dump).
			fmt.Fprintf(j.ErrOut, "Error: unexpected argument %q.\n", name)
			fmt.Fprintln(j.ErrOut, "Prompts are read only from -p/--print, -i/--prompt-interactive, or stdin, so this argument would have been ignored.")
			return 2
		}
	}

	// No subcommand; invoke root Run if set, with the remaining
	// positional args (anything after the last flag we consumed). Cycle
	// 40: previously we passed `nil` here, which broke the --print
	// path because the prompt lives in the positional args. The
	// dispatch path for subcommands already does `s.Run(args)` with
	// the remaining slice, so this is the matching fix for the root
	// path.
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
	// -h/--help on the subcommand itself. The v1.3.5 binary prints the
	// subcommand's usage block to stderr and exits with HelpExit (0 for
	// every subcommand except `update`, which the stdlib flag package
	// exits 2).
	if len(args) > 0 && isHelpArg(args[0]) && s.Help != "" {
		fmt.Fprint(j.ErrOut, s.Help)
		return s.HelpExit
	}
	if len(s.Subs) == 0 {
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
	if len(args) == 0 {
		// Parent command with no subcommand. If the spec registered a
		// handler, let it emit the exact usage/error the v1.3.5 binary
		// prints. Otherwise a registered Help block means "print usage";
		// failing that, fall back to listing the subcommands.
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
	name := args[0]
	for _, ss := range s.Subs {
		// Cycle plugin-case-insensitive: sub-subcommand lookup is
		// case-insensitive (AGY 1.3.6 changelog line 21). The
		// reference matches `List` to `list`, `INSTALL` to `install`,
		// etc.; the case-sensitive `ss.Name == name` form rejected
		// every non-canonical spelling with `Error: unknown
		// <parent> subcommand "<X>"`. `plugin` is the named owner
		// in the changelog, but the lookup is global because
		// /statusline (1.3.0) and the mcp/remote-control families
		// benefit equally. The handler is dispatched with
		// `args[1:]` so the case the user typed is what the
		// implementation sees — plugins reading the verb as a
		// string continue to receive it verbatim.
		if ss.Name == name || strings.EqualFold(ss.Name, name) {
			// -h/--help on the sub-subcommand. A registered Help block
			// wins; when empty the Run handler inspects the args
			// itself (e.g. `plugin list --help` is an error).
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
		flags := append([]*JetskiFlag{}, j.Flags...)
		sort.Slice(flags, func(i, k int) bool { return flags[i].Name < flags[k].Name })
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for _, f := range flags {
			// Skip hidden flags: either registered with an empty Usage
			// (e.g. --version) or explicitly marked Hidden (the
			// openagy-only --backend/--alt-screen/--no-alt-screen).
			// The v1.3.5 binary's --help lists exactly its 24 flags;
			// the extras stay parseable but out of the table.
			if f.Usage == "" || f.Hidden {
				continue
			}
			// Single-char names render with a single dash (v1.3.5 stdlib
			// flag convention); multi-char names render with --. This
			// matches v1.3.5 binary's --help output where -c / -i / -p
			// are short and --continue / --prompt-interactive are long.
			var nameCol string
			if len(f.Name) == 1 {
				nameCol = "-" + f.Name
			} else {
				nameCol = "--" + f.Name
			}
			usage := f.Usage
			// Show defaults for types that have a non-zero default
			// (v1.3.5 --help shows "(default [])" / "(default text)" /
			// "(default 0s)"). bool defaults (false) are not shown.
			if f.Def != "" && f.Def != "false" {
				usage = usage + " (default " + f.Def + ")"
			}
			fmt.Fprintf(tw, "  %s\t%s\n", nameCol, usage)
		}
		tw.Flush()
	}

	if len(j.Subcommands) > 0 {
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
}

// value adapters. We don't use custom flag.Value wrappers; we just
// expose helpers that allocate the underlying primitive types the
// stdlib `flag` package wants (*bool, *string, *time.Duration) and
// return them as `any` so the switch in Parse can type-assert.

// newDestFor returns a fresh destination for a flag of the given type.
// It accepts the string form of the default and converts as needed.
func newDestFor(typ, def string) (any, error) {
	switch typ {
	case "bool":
		v := def == "true" || def == "1"
		return &v, nil
	case "int":
		// Cycle 101: the recovered hidden `-v` flag parses its value with
		// strconv.Atoi, so a bad value reads
		// `invalid value "foo" for flag -v: strconv.Atoi: parsing "foo": invalid syntax`
		// (Go's own IntVar would say "parse error"). Register a flag.Value
		// that calls strconv.Atoi directly.
		n := 0
		if def != "" {
			if _, err := strconv.Atoi(def); err != nil {
				return nil, fmt.Errorf("jetski: bad int default %q: %w", def, err)
			}
		}
		return intAtoiValue{&n}, nil
	case "duration":
		if def == "" {
			def = "0s"
		}
		d, err := time.ParseDuration(def)
		if err != nil {
			return nil, fmt.Errorf("jetski: bad duration default %q: %w", def, err)
		}
		return &d, nil
	case "stringSlice":
		ss := &stringSliceValue{}
		if def != "" {
			for _, p := range strings.Split(def, ",") {
				ss.items = append(ss.items, p)
			}
		}
		return ss, nil
	default: // "string"
		v := def
		return &v, nil
	}
}

// flagBoolValue is the *bool pointer exposed by stdlib flag when
// registered with BoolVar. We re-export it under a stable name so
// version detection in Parse stays readable.
type flagBoolValue = bool

// getBoolFlag returns *bool if the named flag exists and was registered
// as a bool, else nil.
func getBoolFlag(fs *flag.FlagSet, name string) *bool {
	if fs == nil {
		return nil
	}
	f := fs.Lookup(name)
	if f == nil {
		return nil
	}
	// stdlib flag uses a private `boolFlag` value; we duck-type by
	// checking that the string value is "true" or "false" only.
	if s := f.Value.String(); s == "true" || s == "false" {
		// Hack: stdlib flag stores the value via a *bool internally;
		// we can't reach it directly, so we re-parse the string.
		v := s == "true"
		return &v
	}
	return nil
}

// firstSetFlag returns the first of names that appeared on the command line,
// or "" if none did. Cycle 101: print-mode prompt flags are string flags now,
// so getBoolFlag no longer applies and presence must be tested via Visit.
func firstSetFlag(fs *flag.FlagSet, names ...string) string {
	if fs == nil {
		return ""
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	for _, n := range names {
		if set[n] {
			return n
		}
	}
	return ""
}

// flagStrValue returns the string form of a flag's current value.
func flagStrValue(fs *flag.FlagSet, name string) string {
	if fs == nil {
		return ""
	}
	f := fs.Lookup(name)
	if f == nil {
		return ""
	}
	return f.Value.String()
}

// isKnownFlagToken reports whether v looks like a flag this CLI registers —
// "-c", "--effort", "-v=2". Cycle 101: the reference rejects a print prompt
// that is really a flag ("-p took \"-c\" as its prompt") but accepts an
// unregistered dash token ("-x", "--zzz", "--") as the prompt.
func isKnownFlagToken(fs *flag.FlagSet, v string) bool {
	if fs == nil || !strings.HasPrefix(v, "-") {
		return false
	}
	name := strings.TrimLeft(v, "-")
	if i := strings.IndexByte(name, '='); i >= 0 {
		name = name[:i]
	}
	if name == "" {
		return false
	}
	return fs.Lookup(name) != nil
}

// flagDisplayName renders a registered flag name the way the user would type
// it: single letters get one dash, everything else two.
func flagDisplayName(name string) string {
	if len(name) == 1 {
		return "-" + name
	}
	return "--" + name
}

// intAtoiValue is a flag.Value for the recovered hidden `-v` flag. The
// reference parses it with strconv.Atoi, so the stdlib flag error reads
// `invalid value "foo" for flag -v: strconv.Atoi: parsing "foo": invalid syntax`
// (Go's own IntVar would say "parse error").
type intAtoiValue struct{ p *int }

func (v intAtoiValue) String() string {
	if v.p == nil {
		return "0"
	}
	return strconv.Itoa(*v.p)
}

func (v intAtoiValue) Set(s string) error {
	n, err := strconv.Atoi(s)
	if err != nil {
		return err
	}
	*v.p = n
	return nil
}

// durationValue is the stdlib duration flag with one difference: Set returns
// time.ParseDuration's own error instead of collapsing it to the generic
// "parse error" that flag's own durationValue substitutes. The reference
// reports the underlying error verbatim:
//
//	invalid value "abc" for flag -print-timeout: time: invalid duration "abc"
//
// (stdlib would say `…: parse error`). --print-timeout is the only duration
// flag today, but the wrapper is generic.
type durationValue struct{ v *time.Duration }

func (d durationValue) String() string {
	if d.v == nil {
		return "0s"
	}
	return d.v.String()
}

func (d durationValue) Set(s string) error {
	v, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d.v = v
	return nil
}

// stringSliceValue is a flag.Value implementation that accumulates
// repeated -tag invocations into a slice.
type stringSliceValue struct{ items []string }

func (ss *stringSliceValue) String() string     { return strings.Join(ss.items, ",") }
func (ss *stringSliceValue) Set(v string) error { ss.items = append(ss.items, v); return nil }
