// Package cli builds the 1:1 jetski-cli-based parser used by cmd/wagyu.
//
// Cycle 38 rewrite: the v1.3.4 binary does not use cobra/pflag (zero
// `spf13` strings in the binary). Instead it uses two layers:
//   - Go stdlib `flag` for actual parsing
//   - internal `jetski/cli/...` (CLI engine + logfile + termio + steps)
//
// We reimplemented the jetski/cli surface in jetski.go and rewrote
// this file to use it. The public API (SubcommandSpec, SubSubSpec,
// FlagSpec, StandardFlags, StandardSubcommands, NewParser) is
// unchanged so callers in cmd/wagyu and tests do not need to
// change.
//
// The goal is still parse-parity with the v1.3.4 binary's --help output:
//   - 31 top-level flags (long form, short form, and aliases)
//   - 10 visible subcommands (8 spec + agents alias + changelog)
//   - sub-subcommands under plugin (9), mcp (5), remote-control (3), agent (1)
package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// SubcommandSpec is the static description of a single (sub)subcommand. It
// is what the parser registers with jetski and what the test rig asserts
// against.
type SubcommandSpec struct {
	Name string
	// Aliases are extra names that resolve to the same subcommand.
	// e.g. "agents" is an alias of "agent".
	Aliases []string
	// Short is the one-line description shown in `antigravity help`.
	Short string
	// Subs is the static list of sub-subcommands (e.g. plugin's 9 children).
	// Empty for leaf commands like `install` or `update`.
	Subs []SubSubSpec
	// Run is invoked when this subcommand is selected. The first arg is
	// the output writer (parser's Out); the second is the args slice.
	Run func(w io.Writer, args []string) error
	// RunJ, when non-nil, takes precedence over Run and receives the
	// JetskiCLI itself, so the handler can write usage/errors to
	// ErrOut (the v1.3.5 binary's stdout/stderr split).
	RunJ func(j *JetskiCLI, args []string) error
	// Hidden hides the subcommand from help output. Cycle 41: used
	// for the 10 new subcommands (version/login/logout/...) that
	// wire to runXxx handlers but are not part of the v1.3.5
	// binary's --help list.
	Hidden bool
	// Help is the full usage block printed to stderr on -h/--help.
	Help string
	// HelpFn builds the usage block from the program name (argv0
	// basename). Use it instead of Help when the block embeds the
	// program name, as every v1.3.5 subcommand usage does.
	HelpFn func(prog string) string
	// HelpExit is the exit code after printing Help (0 except `update`,
	// whose stdlib flag path exits 2).
	HelpExit int
	// UnknownSubError overrides the unknown-sub-subcommand message
	// (see JetskiSubcommand.UnknownSubError).
	UnknownSubError string
}

// SubSubSpec describes a sub-subcommand (e.g. plugin list, mcp add, etc.).
type SubSubSpec struct {
	Name  string
	Short string
	Run   func(w io.Writer, args []string) error
	// RunJ, when non-nil, takes precedence over Run and receives the
	// JetskiCLI (see SubcommandSpec.RunJ).
	RunJ func(j *JetskiCLI, args []string) error
	// Help is the full usage block printed to stderr on -h/--help.
	// Empty means the Run handler decides (e.g. `plugin list --help`
	// is an error, not a help request).
	Help string
	// HelpFn builds the usage block from the program name (see
	// SubcommandSpec.HelpFn).
	HelpFn   func(prog string) string
	HelpExit int
}

// FlagSpec is the static description of a single (sub)command-level flag.
type FlagSpec struct {
	Name  string // long form, e.g. "add-dir"
	Short string // single-letter alias, e.g. "c" (empty for none)
	Type  string // "string", "bool", "duration", "stringSlice"
	Def   string // default value as a string
	Usage string // one-line description
	// Hidden keeps the flag parseable but out of the --help table.
	Hidden bool
}

// Parser is the high-level handle the rest of the openagy CLI uses. It
// holds the root JetskiCLI plus the static specs that built it, so callers
// can introspect the surface without re-parsing --help.
type Parser struct {
	root   *JetskiCLI
	specs  []SubcommandSpec
	flags  []FlagSpec
	out    io.Writer
	errOut io.Writer
	// lastCode is the exit code the most recent Execute/ExecuteArgs
	// resolved to. Callers that need the precise code (2 for usage
	// errors, 1 for handler failures) read it via ExitCode().
	lastCode int
}

// NewParser builds the root JetskiCLI from the given flag and subcommand
// specs. extraFlags are appended to the standard flag set; they're for
// openagy-specific (post-v4) flags the parse-parity tests must not reject.
func NewParser(flags []FlagSpec, subs []SubcommandSpec, use, short, long string, extraFlags ...FlagSpec) *Parser {
	jc := NewJetski(use, "")
	allFlags := append(append([]FlagSpec{}, flags...), extraFlags...)
	allFlags = append(allFlags, FlagSpec{Name: "version", Type: "bool", Usage: ""})
	registerParserFlags(jc, allFlags)

	for _, s := range subs {
		js := buildJetskiSubcommand(jc, s, use)
		jc.AddSubcommand(js)
	}
	return &Parser{root: jc, specs: subs, flags: allFlags}
}

func registerParserFlags(jc *JetskiCLI, allFlags []FlagSpec) {
	for _, f := range allFlags {
		jc.AddFlag(&JetskiFlag{
			Name:     f.Name,
			Short:    f.Short,
			Type:     f.Type,
			Def:      f.Def,
			Usage:    f.Usage,
			Required: false,
			Hidden:   f.Hidden,
		})
	}
}

func buildJetskiSubcommand(jc *JetskiCLI, s SubcommandSpec, use string) *JetskiSubcommand {
	help := s.Help
	if s.HelpFn != nil {
		help = s.HelpFn(use)
	}
	specRun := s.Run
	js := &JetskiSubcommand{
		Name:            s.Name,
		Aliases:         s.Aliases,
		Description:     s.Short,
		Hidden:          s.Hidden,
		Help:            help,
		HelpExit:        s.HelpExit,
		UnknownSubError: s.UnknownSubError,
		RunJ:            s.RunJ,
		Run: func(args []string) error {
			if specRun == nil {
				return nil
			}
			return specRun(jc.Out, args)
		},
	}
	for _, ss := range s.Subs {
		js.Subs = append(js.Subs, buildJetskiSubSubcommand(jc, ss, use))
	}
	return js
}

func buildJetskiSubSubcommand(jc *JetskiCLI, ss SubSubSpec, use string) *JetskiSubSubcommand {
	specSS := ss
	ssHelp := ss.Help
	if ss.HelpFn != nil {
		ssHelp = ss.HelpFn(use)
	}
	return &JetskiSubSubcommand{
		Name:        ss.Name,
		Description: ss.Short,
		Help:        ssHelp,
		HelpExit:    ss.HelpExit,
		RunJ:        ss.RunJ,
		Run: func(args []string) error {
			if specSS.Run == nil {
				return nil
			}
			return specSS.Run(jc.Out, args)
		},
	}
}

// SetOut redirects parser output (used by tests to capture --help).
func (p *Parser) SetOut(w io.Writer) {
	p.out = w
	p.root.Out = w
	if p.root.flagSet != nil {
		p.root.flagSet.SetOutput(w)
	}
}

// SetErr redirects parser error output. Sets both the parser's errOut
// and the jetski engine's ErrOut (used for usage/help + error lines).
func (p *Parser) SetErr(w io.Writer) { p.errOut = w; p.root.ErrOut = w }

// Root returns the underlying JetskiCLI for callers that need to install
// custom Run hooks.
func (p *Parser) Root() *JetskiCLI { return p.root }

// SubcommandSpecs returns the registered subcommand specs (read-only).
func (p *Parser) SubcommandSpecs() []SubcommandSpec { return p.specs }

// FlagSpecs returns the registered flag specs (read-only).
func (p *Parser) FlagSpecs() []FlagSpec { return p.flags }

// Execute runs the parser against os.Args[1:] (the default for a
// top-level CLI invocation). Returns the jetski-style exit code
// (0 ok, 2 usage error). Cycle 40: previously this delegated to
// ExecuteArgs(nil) which always passed an empty slice, so flag.Parse
// saw no flags and the root Run got `args=[]` — making `antigravity
// --print "say hi"` silently fall through to the help banner.
func (p *Parser) Execute() error { return p.ExecuteArgs(os.Args[1:]) }

// ExecuteArgs is like Execute but with explicit args (used by tests).
// Passing nil means "use os.Args[1:]" so callers don't have to
// duplicate the slicing in every test.
func (p *Parser) ExecuteArgs(args []string) error {
	if args == nil {
		args = os.Args[1:]
	}
	code := p.root.Parse(args)
	p.lastCode = code
	if code == 0 {
		return nil
	}
	return fmt.Errorf("jetski: exit %d", code)
}

// ExitCode returns the exit code the most recent Execute/ExecuteArgs
// resolved to (0 ok, 1 handler failure, 2 usage error).
func (p *Parser) ExitCode() int { return p.lastCode }

// LookupFlagValue walks a parsed JetskiCLI's FlagSet and returns the string
// representation of the canonical flag. Empty if not set.
//
// In jetski mode the long and short forms are registered as separate
// flags; we check both.
func (p *Parser) LookupFlagValue(canonical string) string {
	fs := p.root.flagSet
	if fs == nil {
		return ""
	}
	if f := fs.Lookup(canonical); f != nil {
		return f.Value.String()
	}
	// Try short form
	for _, jf := range p.root.Flags {
		if jf.Name == canonical && jf.Short != "" {
			if f := fs.Lookup(jf.Short); f != nil {
				return f.Value.String()
			}
		}
	}
	return ""
}

// AliasMap is kept for source compatibility with cycle 33-37 callers.
type AliasMap map[string]string

// LookupFlagValueCmd is the legacy API used by cmd/wagyu. It
// preserves the (cmd, canonical) signature so callers don't have to
// change. Internally it forwards to the new method.
func (a AliasMap) LookupFlagValueCmd(p *Parser, canonical string) string {
	v := p.LookupFlagValue(canonical)
	if v != "" {
		return v
	}
	for alias, real := range a {
		if real == canonical {
			if v := p.LookupFlagValue(alias); v != "" {
				return v
			}
		}
	}
	return ""
}

// StandardAliasMap is the alias table the v1.3.5 binary implicitly
// defines. v1.3.5 --help shows -c / -i / -p short forms plus the
// --prompt long-form alias. We keep this table for cmd/wagyu
// to fold the short forms into the canonical flag values.
var StandardAliasMap = AliasMap{
	"c":      "continue",
	"i":      "prompt-interactive",
	"p":      "print",
	"prompt": "print",
}

// ParseForTest is a parse-only entry point used by the test rig. It builds
// a fresh Parser with the standard v4 specs and runs jetski against args.
// Returns the resulting Parser so tests can inspect Flag values.
func ParseForTest(args []string) (*Parser, error) {
	p := NewParser(StandardFlags(), StandardSubcommands(),
		"antigravity",
		"Recovered Pigasus CLI (1:1 parse parity with v1.3.4 binary)",
		"pigasus is a 1:1 parse-parity shell of Google Antigravity CLI v1.3.4.\n"+
			"Top-level flags, subcommands and sub-subcommands match the v1.3.4\n"+
			"binary so wrapper scripts and operator tooling are portable across\n"+
			"both. The CLI engine is jetski/cli 1:1 (cycle 38), the parser is\n"+
			"Go stdlib `flag` (no cobra/pflag), the TUI is bubbletea v2.")
	p.SetOut(io.Discard)
	p.SetErr(io.Discard)
	err := p.ExecuteArgs(args)
	return p, err
}

// keep imports referenced
var _ = time.Second
var _ = strings.Join
