// command.go — reading the clone's own run_command result.
//
// The clone's `run_command` prints its outcome in its own shape:
//
//	Created At: 2026-10-22T11:14:02+08:00
//	Completed At: 2026-10-22T11:14:03+08:00
//	Command exited with code 0.
//
//	Command output:
//	a
//
// and replaces the last block with "No output from the command." when the
// command printed nothing.
//
// The harness prints a command's outcome through the reference's step
// string instead, and that shape needs the exit code and the output apart.
// This file reads them back out of the clone's own text.

package harnesstools

import (
	"strconv"
	"strings"
)

// CommandResult is the outcome a command reported.
type CommandResult struct {
	// ExitCode is the command's exit status.
	ExitCode int
	// Output is what the command printed.
	Output string
	// HasOutput says whether the command printed anything.
	HasOutput bool
}

// commandExitMarker introduces the exit code in the clone's result.
const commandExitMarker = "Command exited with code "

// commandOutputMarker introduces the output block in the clone's result.
const commandOutputMarker = "\n\nCommand output:\n"

// commandNoOutput is the clone's wording for a command that printed
// nothing.
const commandNoOutput = "No output from the command."

// commandTruncated is the clone's note for a result whose output was
// spilled to a file.
const commandTruncated = "The tool's output was truncated"

// ParseCommand reads the exit code and the output out of the clone's own
// run_command result.
//
// The second result is false when the text is not a plain run_command
// result — a truncated result, for instance, carries a preview rather than
// the command's output, and the harness keeps the clone's own text there.
func ParseCommand(result string) (CommandResult, bool) {
	if strings.Contains(result, commandTruncated) {
		return CommandResult{}, false
	}
	marker := strings.Index(result, commandExitMarker)
	if marker < 0 {
		return CommandResult{}, false
	}
	rest := result[marker+len(commandExitMarker):]
	digits := 0
	for digits < len(rest) && rest[digits] >= '0' && rest[digits] <= '9' {
		digits++
	}
	if digits == 0 || digits >= len(rest) || rest[digits] != '.' {
		return CommandResult{}, false
	}
	code, err := strconv.Atoi(rest[:digits])
	if err != nil {
		return CommandResult{}, false
	}
	out := CommandResult{ExitCode: code}
	switch {
	case strings.Contains(rest, commandOutputMarker):
		out.Output = rest[strings.Index(rest, commandOutputMarker)+len(commandOutputMarker):]
		out.HasOutput = true
	case strings.Contains(rest, commandNoOutput):
		out.Output = ""
		out.HasOutput = false
	default:
		return CommandResult{}, false
	}
	return out, true
}
