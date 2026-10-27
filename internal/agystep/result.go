// result.go — reading the reference's two run_command result shapes.
//
// The reference prints a command's outcome in one of two shapes, and the
// clone has to understand both:
//
//  1. the CLI's own formatter, which is what agy 1.4.4 and agy 1.4.7 send
//     to the model and what the 1.4.7 conversation store holds (一手 — the
//     live functionResponse capture in
//     docs/cycles/command-output-truncation/captures.md and the store
//     records under ~/.gemini/antigravity-cli/conversations):
//
//     Created At: …
//     Completed At: …
//     Command exited with code 0.
//
//     Command output:
//     a
//
//     with "No output from the command." instead of the output block when
//     the command printed nothing.
//
//  2. the step string, which is what step_strings/run_command.tmpl renders
//     and what the reference produced at 1.4.2/1.4.3 (the eleven records
//     the reference's own transcript.jsonl carries from 2026-10-19/20):
//
//     Created At: …
//     Completed At: …
//
//     The command exited with code 0.
//     Output:
//     a
//
// The clone's own result is shape 1, but its TUI and its resumed-conversation
// reader were both written against shape 2 — so a command that failed was
// never marked as failed and the timestamps reached the screen (cycle 298).
// One reader for both shapes fixes that.
package agystep

import (
	"regexp"
	"strconv"
	"strings"
)

// exitLineRe matches both exit lines: the formatter's "Command exited with
// code N." and the step string's "The command exited with code N.". The
// optional "\r" is there for the CRLF payloads the reference stores.
var exitLineRe = regexp.MustCompile(`(?m)^(?:The )?[Cc]ommand exited with code (-?\d+)\.[ \t]*\r?$`)

// Result is a parsed run_command result.
type Result struct {
	// ExitCode is the command's exit status.
	ExitCode int
	// Output is the command's output, without the boilerplate.
	Output string
	// HasOutput reports whether the result carried an output block at all.
	// A command that printed nothing has an empty Output and no block.
	HasOutput bool
}

// ParseRunCommandResult reads a run_command result. ok is false when the
// text carries no exit-code line — a tool-level failure, a timeout note, or
// a result of some other tool.
//
// The result may use CRLF line endings: the reference's stored payloads do
// (the reference's command output arrives from a pty), so the headers are
// matched with either ending.
func ParseRunCommandResult(text string) (Result, bool) {
	loc := exitLineRe.FindStringSubmatchIndex(text)
	if loc == nil {
		return Result{}, false
	}
	code, err := strconv.Atoi(text[loc[2]:loc[3]])
	if err != nil {
		return Result{}, false
	}
	res := Result{ExitCode: code}

	// The body is what follows the exit line. Both shapes put the output
	// header straight after it, separated by blank lines.
	body := strings.TrimLeft(text[loc[1]:], "\r\n")
	switch {
	case hasHeader(body, "Command output:"):
		res.Output = afterHeader(body[len("Command output:"):])
		res.HasOutput = true
	case hasHeader(body, "Output:"):
		res.Output = afterHeader(body[len("Output:"):])
		res.HasOutput = true
	case strings.HasPrefix(body, "No output from the command."):
		// The formatter's empty-output case.
	case strings.HasPrefix(body, "No output"):
		// The step string's empty-output case.
	case strings.HasPrefix(body, "Here is a preview with the middle of the output truncated:"):
		// The truncation note's preview header; the exit line follows it.
		if i := strings.Index(body, "Command output:"); i >= 0 {
			res.Output = afterHeader(body[i+len("Command output:"):])
			res.HasOutput = true
		}
	}
	return res, true
}

// hasHeader reports whether body starts with header on its own line.
func hasHeader(body, header string) bool {
	if !strings.HasPrefix(body, header) {
		return false
	}
	rest := body[len(header):]
	return strings.HasPrefix(rest, "\n") || strings.HasPrefix(rest, "\r\n")
}

// afterHeader returns what follows a header's line break.
func afterHeader(s string) string {
	switch {
	case strings.HasPrefix(s, "\r\n"):
		return s[2:]
	case strings.HasPrefix(s, "\n"):
		return s[1:]
	}
	return s
}
