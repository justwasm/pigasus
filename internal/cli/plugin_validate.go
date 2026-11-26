package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/internal/features/plugins/validate"
)

// validateResult adds presentation and source-lifecycle state to a domain result.
// The validator owns all manifest/component rules; CLI owns fetching, rendering,
// strict-mode exit policy and cleanup.
type validateResult struct {
	validate.Result
	HeaderPath    string
	ColorOutput   bool
	IsArchive     bool
	ArchiveSource string
	cleanup       func()
}

func outputValidateHuman(j *JetskiCLI, res *validateResult, strict bool) error {
	code := validateExitCode(res.Report, strict)
	writeValidateHuman(j.Out, res)
	footer := validateFooter(res, strict)
	if code != 0 {
		// The reference sends a failing footer to stderr; a passing one
		// (including "passed with N warnings") stays on stdout.
		fmt.Fprintln(j.ErrOut, footer)
		return errExitCode(code)
	}
	if res.ColorOutput {
		footer = "\x1b[32m✔\x1b[0m" + strings.TrimPrefix(footer, "✔")
	}
	fmt.Fprintln(j.Out, footer)
	return nil
}

// writeValidateHuman renders the reference's human block:
//
//	Validating <name> (<manifest>)
//	<blank>
//	  <symbol> <file>: <message>
//	<blank>
//	  skills N · agents N · rules N · hooks N · mcpServers N · sidecars N
//	<blank>
//	<footer>
//
// The summary line is omitted when the manifest could not be resolved or
// parsed.
func writeValidateHuman(w io.Writer, res *validateResult) {
	if res.ManifestOK {
		if res.IsArchive && res.ArchiveSource != "" {
			fmt.Fprintf(w, "Validating %s (%s in %s)\n", res.Report.Name, res.Report.Manifest, res.ArchiveSource)
		} else {
			fmt.Fprintf(w, "Validating %s (%s)\n", res.Report.Name, filepath.Join(res.HeaderPath, res.Report.Manifest))
		}
	} else {
		fmt.Fprintf(w, "Validating %s\n", res.HeaderPath)
	}
	fmt.Fprintln(w)

	for _, f := range res.Report.Findings {
		symbol := "·"
		if res.ColorOutput {
			switch f.Severity {
			case validate.SeverityError:
				symbol = "\x1b[31m✗\x1b[0m"
			case validate.SeverityWarning:
				symbol = "\x1b[33m⚠\x1b[0m"
			default:
				symbol = "\x1b[36m·\x1b[0m"
			}
		} else {
			switch f.Severity {
			case validate.SeverityError:
				symbol = "✗"
			case validate.SeverityWarning:
				symbol = "⚠"
			}
		}
		fmt.Fprintf(w, "  %s %s: %s\n", symbol, f.File, f.Message)
	}
	if len(res.Report.Findings) > 0 {
		fmt.Fprintln(w)
	}

	if res.ManifestOK {
		c := res.Report.Components
		fmt.Fprintf(w, "  skills %d · agents %d · rules %d · hooks %d · mcpServers %d · sidecars %d\n",
			c.Skills, c.Agents, c.Rules, c.Hooks, c.MCPServers, c.Sidecars)
		fmt.Fprintln(w)
	}
}

// validateFooter builds the reference's footer text.
func validateFooter(res *validateResult, strict bool) string {
	e, w := res.Report.Counts()
	switch {
	case e > 0:
		return fmt.Sprintf("Error: validation failed: %d %s, %d %s", e, plural(e, "error", "errors"), w, plural(w, "warning", "warnings"))
	case w > 0 && strict:
		return fmt.Sprintf("Error: validation failed in strict mode: %d %s", w, plural(w, "warning", "warnings"))
	case w > 0:
		return fmt.Sprintf("✔ Validation passed with %d %s", w, plural(w, "warning", "warnings"))
	default:
		return "✔ Validation passed"
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// isTerminalWriter reports whether w is a character device, which is when the
// reference enables ANSI colour.
func isTerminalWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

type validateFlags struct {
	strict bool
	asJSON bool
	path   string
	// help is set by -h / --help: the reference prints the usage
	// block on stderr and exits 0.
	help bool
}

// usagePluginValidate is the reference's `plugin validate` usage block,
// byte for byte (the program name is the only variable part).
//
// The reference prints it on stderr and exits 0 for `--help` / `-h`.
func usagePluginValidate(n string) string {
	return "Usage: " + n + " plugin validate [path] [--strict] [--json]\n" +
		"\n" +
		"Checks a plugin the way the plugin loader reads it, and reports the files and\n" +
		"fields that the loader skips or ignores.\n" +
		"\n" +
		"Arguments:\n" +
		"  path        A plugin directory, or its plugin.json (default: the current\n" +
		"              directory)\n" +
		"\n" +
		"Flags:\n" +
		"  --strict    Also fail on warnings\n" +
		"  --json      Print the report as JSON\n" +
		"  -h, --help  Show this help\n" +
		"\n" +
		"Each finding is an error (the loader skips a file or the plugin, or an item\n" +
		"can never work), a warning (a field or file is ignored or falls back to a\n" +
		"default) or a note (how the plugin loads). Exits non-zero if there are errors,\n" +
		"or warnings with --strict.\n"
}

// runPluginValidateReference is the entry point for `agy plugin validate`.
//
// Reference surface (probed against the 1.4.6 binary):
//
//	agy plugin validate [path] [--strict] [--json]
//
// `--help` / `-h` print the usage block on stderr and exit 0. Any other
// argument is rejected with
// `Error: unknown validate argument "<arg>": usage is validate [path] [--strict] [--json]. See validate --help`
// on stderr and exit 1.
func runPluginValidateReference(j *JetskiCLI, args []string) error {
	flags, err := parseValidateArgs(args)
	if err != nil {
		return fail(j, 1, err.Error())
	}
	if flags.help {
		fmt.Fprint(j.ErrOut, usagePluginValidate(j.Name))
		return nil
	}

	res := buildValidateResultWithOut(flags.path, j.Out)
	if res.cleanup != nil {
		defer res.cleanup()
	}
	res.ColorOutput = isTerminalWriter(j.Out)

	if flags.asJSON {
		return outputValidateJSON(j, res, flags.strict)
	}
	return outputValidateHuman(j, res, flags.strict)
}

func parseValidateArgs(args []string) (validateFlags, error) {
	var flags validateFlags
	var positional []string
	for _, a := range args {
		switch a {
		case "--strict":
			flags.strict = true
			continue
		case "--json":
			flags.asJSON = true
			continue
		case "-h", "--help":
			flags.help = true
			continue
		}
		if strings.HasPrefix(a, "-") || len(positional) > 0 {
			return flags, fmt.Errorf("Error: unknown validate argument %q: usage is validate [path] [--strict] [--json]. See validate --help", a)
		}
		positional = append(positional, a)
	}
	flags.path = "."
	if len(positional) == 1 {
		flags.path = positional[0]
	}
	return flags, nil
}

func outputValidateJSON(j *JetskiCLI, res *validateResult, strict bool) error {
	enc := json.NewEncoder(j.Out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res.Report); err != nil {
		return err
	}
	code := validateExitCode(res.Report, strict)
	if code != 0 {
		fmt.Fprintln(j.ErrOut, validateFooter(res, strict))
	}
	return errExitCode(code)
}

func buildValidateResultWithOut(inputPath string, out io.Writer) *validateResult {
	dir, manifestRel, isArchive, archiveSource, cleanup, errRes := resolveValidateSource(inputPath, out)
	if errRes != nil {
		return errRes
	}
	headerPath := dir
	if isArchive && archiveSource != "" {
		headerPath = archiveSource
	}
	res := &validateResult{
		Result:        validate.Check(validate.Source{Files: os.DirFS(dir), Dir: dir, Manifest: filepath.ToSlash(manifestRel)}),
		HeaderPath:    headerPath,
		IsArchive:     isArchive,
		ArchiveSource: archiveSource,
		cleanup:       cleanup,
	}
	return res
}

func resolveValidateDirAndManifest(input string) (string, string, *validateResult) {
	info, statErr := os.Stat(input)
	if statErr != nil {
		return "", "", validateDirStatError(input)
	}

	dir := input
	manifestRel := ""
	if !info.IsDir() {
		dir = filepath.Dir(input)
		if dir == "." {
			if abs, err := filepath.Abs("."); err == nil {
				dir = abs
			}
		}
		manifestRel = filepath.Base(input)
	} else {
		manifestRel = locateManifestInSubdirs(dir)
	}
	if manifestRel == "" {
		return "", "", validateManifestNotFoundError(dir)
	}
	return dir, manifestRel, nil
}

func locateManifestInSubdirs(dir string) string {
	return filepath.FromSlash(validate.FindManifest(os.DirFS(dir)))
}

func validateDirStatError(input string) *validateResult {
	return validateFileError(input, input+" is not a directory")
}

func validateManifestNotFoundError(dir string) *validateResult {
	return &validateResult{
		Result: validate.Result{Report: validate.Report{
			Dir:      dir,
			Findings: []validate.Finding{{Severity: validate.SeverityError, File: "plugin.json", Message: "no plugin.json found at the plugin root or in .antigravity-plugin or .gemini-plugin"}},
		}},
		HeaderPath: dir,
	}
}

// validateExitCode mirrors the reference: errors are fatal, warnings only
// fatal under --strict.
func validateExitCode(rep validate.Report, strict bool) int {
	var e, w int
	for _, f := range rep.Findings {
		switch f.Severity {
		case validate.SeverityError:
			e++
		case validate.SeverityWarning:
			w++
		}
	}
	if e > 0 || (strict && w > 0) {
		return 1
	}
	return 0
}
