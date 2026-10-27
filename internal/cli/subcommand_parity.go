// Package cli — subcommand_parity.go: the v1.3.5 subcommand surface (recovered from the v1.3.5 binary).
//
// The engine in jetski.go understands *how* to dispatch a subcommand;
// this file supplies the exact per-subcommand usage blocks and the Run
// handlers that reproduce the v1.3.5 binary's behaviour. Everything
// here was captured from the reference binary
//
//	Usage: <prog> plugin <command> [arguments]
//	...
//
// The single source of truth for the byte-level text is the reference
// binary itself; the constants below were transcribed from
// `agy <sub> --help` / `agy <sub>` captures. Where a handler needs the
// program name (the reference uses argv0's basename — verified by
// renaming the binary), it reads j.Name.
//
// Stream discipline (matches the reference):
//   - usage/help blocks  -> stderr, exit 0
//   - `Error: ...` lines -> stderr
//   - normal results     -> stdout
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ---- small helpers --------------------------------------------------------

// fail prints a raw message (no prefix) to stderr and returns the exit
// sentinel. Handlers do their own formatting so the message is byte
// exact.
func fail(j *JetskiCLI, code int, msg string) error {
	fmt.Fprintln(j.ErrOut, msg)
	return errExitCode(code)
}

// usageFail prints a usage block followed by a message, then exits 1.
// This is the shape the reference uses for an unrecognised flag on a
// subcommand whose only real flag is -h/--help.
func usageFail(j *JetskiCLI, usage, msg string) error {
	fmt.Fprint(j.ErrOut, usage)
	fmt.Fprintln(j.ErrOut, msg)
	return errExitCode(1)
}

// flagName normalises an argv flag token to the form the reference
// prints: "-json" for "--json", "-h" for "-h".
func flagName(a string) string {
	if strings.HasPrefix(a, "--") {
		return "-" + a[2:]
	}
	return a
}

// UsageLogo returns the usage block for the logo subcommand.
//
// The `logo` subcommand is the clone's own — the reference has no such
// command — so the block grows with it: `--seasonal` and `--scene`
// print the seasonal banner art, the five scenes the reference's
// Halloween banner draws at startup.
func UsageLogo(n string) string {
	return "Usage: " + n + " logo [flags]\n" +
		"\n" +
		"Print the startup banner logo\n" +
		"\n" +
		"Flags:\n" +
		"  --flip      Transform to apply: none|both|ref|vert|row-rev|horiz|invert (default \"none\")\n" +
		"  -h          Show help\n" +
		"  --help      Show help\n" +
		"  --scene     Seasonal scene to print: pumpkin|moon|ghost|witch-hat|bat|all|random\n" +
		"  --seasonal  Print a seasonal banner scene, the way the reference draws it at startup\n" +
		"  --style     Style of logo: default|simple (or half|block)\n"
}

// ---- models ---------------------------------------------------------------

func usageModels(n string) string {
	return "Usage: " + n + " models [flags]\n" +
		"\n" +
		"List available models\n" +
		"\n" +
		"Flags:\n" +
		"  -h      Show help\n" +
		"  --help  Show help\n"
}

// ModelEntry is one row of the reference's model catalogue.
type ModelEntry struct{ ID, Label string }

// ReferenceModels is the catalogue `agy models` prints. The reference also
// uses it to decide whether a configured model is still available: a model
// that is not in the catalogue triggers the "is no longer available" warning
// and a fallback to Gemini 3.8 Flash (High).
//
// It is the reference's own catalogue and nothing else — the six models the
// CloudCode server advertises, plus the two Claude 5.5 entries when the
// account's project can reach them. Models the user added are not here:
// they come from the user's provider configuration, and ModelCatalogue
// merges the two.
//
// AGY 1.3.6 changelog line 1: "Added Claude Opus 5.5 and Claude Sonnet
// 5.5 for Application Default Credentials (AGY_ADC_AUTH) sign-in. Claude
// models now appear in /model only if your Google Cloud project can use
// them." The two 5.5 entries are kept in the static table for downstream
// lookups (so `agy --model claude-opus-5-5` still resolves when the gate
// is open) and conditionally dropped from the returned slice via
// claude55Available() — see claude55.go for the gate's predicate.
func ReferenceModels() []ModelEntry {
	out := []ModelEntry{
		{"gemini-3.8-flash-high", "Gemini 3.8 Flash (High)"},
		{"gemini-3.8-flash-medium", "Gemini 3.8 Flash (Medium)"},
		{"gemini-3.8-flash-low", "Gemini 3.8 Flash (Low)"},
		{"claude-sonnet-4-6", "Claude Sonnet 4.6 (Thinking)"},
		{"claude-opus-4-6-thinking", "Claude Opus 4.6 (Thinking)"},
		{"gpt-oss-120b-medium", "GPT-OSS 120B (Medium)"},
	}
	if Claude55Available() {
		out = append(out,
			ModelEntry{"claude-opus-5-5", "Claude Opus 5.5"},
			ModelEntry{"claude-sonnet-5-5", "Claude Sonnet 5.5"},
		)
	}
	return out
}

func runModelsParity(j *JetskiCLI, args []string) error {
	usage := usageModels(j.Name)
	for _, a := range args {
		if isHelpArg(a) {
			fmt.Fprint(j.ErrOut, usage)
			return nil
		}
		if strings.HasPrefix(a, "-") {
			return usageFail(j, usage, "Error: flags provided but not defined: "+flagName(a))
		}
		return fail(j, 1, fmt.Sprintf("Error: unexpected arguments: [%s]", strings.Join(args, " ")))
	}
	// The unrecognized---mode warning precedes the fetch and the sign-in
	// check (verified: `agy --mode bogus models` warns on the first
	// stderr line, then "Fetching available models...", then the error).
	WarnMode(j.ErrOut, j.FlagMode())
	if !legacySignedIn() {
		fmt.Fprintln(j.ErrOut, "Fetching available models...")
		return fail(j, 1, "Error: Please sign in to view available models. Launch the CLI without arguments to sign in.")
	}
	// 【一手】 The fetch banner goes to stderr, not stdout: a pty capture and
	// a plain redirect of agy 1.4.5 both put "Fetching available models..."
	// on stderr, and leave stdout carrying nothing but the catalogue rows.
	fmt.Fprintln(j.ErrOut, "Fetching available models...")
	return writeModelTable(j.Out, ModelCatalogue())
}

// legacySignedIn reports whether the v1.3.5 binary would consider the user
// signed in. It consults only on-disk/env token sources under the
// current HOME — the reference does not consult the macOS keychain for
// this check (verified: a scratch HOME reports signed-out even when the
// keychain holds a `gemini`/`antigravity` item).
func legacySignedIn() bool {
	if strings.TrimSpace(os.Getenv("JETSKI_OAUTH_TOKEN")) != "" {
		return true
	}
	home := strings.TrimSpace(os.Getenv("HOME"))
	if home == "" {
		return false
	}
	candidates := []string{
		filepath.Join(home, ".config", "antigravity", "oauth-token"),
		filepath.Join(home, ".config", "antigravity", "antigravity-auth.json"),
		filepath.Join(home, ".gemini", "antigravity-cli", "antigravity-oauth-token"),
	}
	for _, p := range candidates {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Size() > 0 {
			return true
		}
	}
	return false
}

// ---- help -----------------------------------------------------------------

func runHelpCommand(j *JetskiCLI, args []string) error {
	if len(args) == 0 {
		j.PrintHelpTo(j.ErrOut)
		return nil
	}
	name := args[0]
	if sub, ok := j.lookup(name); ok && sub.Help != "" {
		fmt.Fprint(j.ErrOut, sub.Help)
		return nil
	}
	return fail(j, 1, fmt.Sprintf("Error: unknown subcommand: %s", name))
}
