// failure.go — the reference's wording for a tool call that failed.
//
// # What the reference writes
//
// A call that fails is reported as a failed *step*. The model sees the
// step's own text:
//
//	Created At: 2026-10-26T04:52:19+08:00
//	Completed At: 2026-10-26T04:52:19+08:00
//	Encountered error in step execution: path file:///…/nope.txt does not exist
//
// and the frame carries the tool's own sentence:
//
//	Cannot view file file:///…/nope.txt which does not exist.
//
// The wording is **per tool**. A read says "path", a listing says
// "directory", a content search says "search path", a name search says
// "search directory". Two of the four print the path **bare** in the
// step's reason and two print it as a `file://` URI; the frame's sentence
// always prints the URI.
//
// # What is the clone's
//
// The reference's *reason* comes from its own tools, and the clone runs
// the CLI's tools — so the wording is the reference's and the reason
// inside it is the clone's, unless the reference's wording for that
// failure is known. The four tools whose wording is known are rendered
// in the reference's words; every other failure keeps the clone's error
// in the reference's shape.
//
// The error text is **not** truncated: the client's truncation budget
// applies to the tool's output, and the reference leaves a failed step's
// text whole (a 422-byte error survived a 30-byte budget).
package harnesstools

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"
)

// pathFailure is the reference's wording for a path that does not exist.
type pathFailure struct {
	// reason names the path in the step's own words.
	reason string
	// sentence is the frame's own sentence; %s is the path.
	sentence string
	// bare prints the path in the step's reason without the `file://`
	// prefix.
	bare bool
}

// pathFailures is the reference's wording, per tool.
//
// The four entries are the ones the captures pin: `view_file`,
// `list_dir`, `grep_search` and `find_by_name` all failed on a path that
// does not exist, and each said something different.
var pathFailures = map[string]pathFailure{
	ToolViewFile:   {reason: "path", sentence: "Cannot view file %s which does not exist."},
	ToolListDir:    {reason: "directory", sentence: "Cannot list directory %s which does not exist.", bare: true},
	ToolGrepSearch: {reason: "search path", sentence: "Cannot search path %s which does not exist."},
	ToolFindByName: {reason: "search directory", sentence: "Cannot search directory %s which does not exist.", bare: true},
}

// Failed renders a tool call that failed, the way the reference renders it.
//
// The reason is the tool's own error; the wording around it is the
// reference's.
func Failed(in Input, err error) Outcome {
	if in.Tool == ToolRunCommand {
		return failedRunCommand(in, err)
	}
	reason, sentence := failureWording(in, err)
	return Outcome{
		Text:         stamp(in) + "Encountered error in step execution: " + reason,
		ErrorMessage: sentence,
		Failed:       true,
	}
}

// failedRunCommand renders a command that could not run.
//
// The reference prints a command's failure through the same step string it
// prints its success through, so the model's text carries the step's error
// branch:
//
//	Created At: …
//	Completed At: …
//
//	Encountered error in step execution: error executing cascade step: CORTEX_STEP_TYPE_RUN_COMMAND: /…/nodir: no such directory
//
// and the frame carries the reason itself.
func failedRunCommand(in Input, err error) Outcome {
	reason := err.Error()
	return Outcome{
		Text:         stamp(in) + runCommandErrorString(argString(in.Args, "CommandLine", "command", "command_line"), reason),
		ErrorMessage: reason,
		Failed:       true,
	}
}

// failureWording returns the reason a failed step carries and the sentence
// the frame carries.
//
// A failure the reference's wording is known for is rendered in that
// wording; every other failure keeps the tool's own error in both places.
func failureWording(in Input, err error) (reason, sentence string) {
	failure, known := pathFailures[in.Tool]
	if !known || !isNotExist(err) {
		return in.Result, in.Result
	}
	path := failurePath(in.Tool, in.Args)
	if path == "" {
		return in.Result, in.Result
	}
	reasonPath := path
	if !failure.bare {
		reasonPath = fileURI(path)
	}
	reason = failure.reason + " " + reasonPath + " does not exist"
	return reason, strings.Replace(failure.sentence, "%s", fileURI(path), 1)
}

// failurePath returns the path a failed call was about, in the reference's
// spelling.
//
// The harness's tools spell their paths in PascalCase; the clone's own
// catalogue spells them in snake_case, and the alias table lets both
// through.
func failurePath(tool string, args json.RawMessage) string {
	switch tool {
	case ToolViewFile:
		return argString(args, "AbsolutePath", "path", "file_path")
	case ToolListDir:
		return argString(args, "DirectoryPath", "path", "directory_path")
	case ToolGrepSearch:
		return argString(args, "SearchPath", "DirectoryPath", "path", "directory_path")
	case ToolFindByName:
		return argString(args, "SearchDirectory", "SearchPath", "DirectoryPath", "path", "directory_path")
	}
	return ""
}

// isNotExist reports whether a failure is "the path does not exist".
//
// The clone's tools report the operating system's own error, so the
// reason is either the error itself or its text — the CLI's tools wrap
// the error in their own words in some places.
func isNotExist(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	return strings.Contains(err.Error(), "no such file or directory")
}

// MissingDirectoryError is the failure a command whose working directory
// does not exist produces.
//
// The reference's harness runs the command in the directory the model
// named and fails the step when there is none. Its own wording names the
// step type the command failed in:
//
//	error executing cascade step: CORTEX_STEP_TYPE_RUN_COMMAND: /…/nodir: no such directory
//
// The CLI's own runner does the opposite — it keeps the workspace when the
// directory is gone, so that a stale value does not brick the call — and
// the harness therefore checks the directory before the call.
func MissingDirectoryError(dir string) error {
	return fmt.Errorf("error executing cascade step: CORTEX_STEP_TYPE_RUN_COMMAND: %s: no such directory", dir)
}
