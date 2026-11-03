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
	"github.com/justwasm/pigasus/internal/features/models/catalog"
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
	return writeModelTable(j.Out, catalog.ModelCatalogue())
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
