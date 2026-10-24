package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

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
		fmt.Fprintf(w, "Validating %s (%s)\n", res.HeaderName, filepath.Join(res.HeaderPath, res.Report.Manifest))
	} else {
		fmt.Fprintf(w, "Validating %s\n", res.HeaderPath)
	}
	fmt.Fprintln(w)

	for _, f := range res.Report.Findings {
		symbol := "·"
		if res.ColorOutput {
			switch f.Severity {
			case validateError:
				symbol = "\x1b[31m✗\x1b[0m"
			case validateWarning:
				symbol = "\x1b[33m⚠\x1b[0m"
			default:
				symbol = "\x1b[36m·\x1b[0m"
			}
		} else {
			switch f.Severity {
			case validateError:
				symbol = "✗"
			case validateWarning:
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
	e, w := res.ErrorCount, res.WarnCount
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
