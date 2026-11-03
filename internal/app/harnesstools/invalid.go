// invalid.go — the reference's runtime call validation.
//
// # What the reference writes
//
// The reference checks a call's arguments *before* it dispatches the tool
// and refuses an invalid one as an invalid tool call. The call is never
// announced, the model reads a paragraph, and the frame says the model
// produced an invalid tool call:
//
//	Created At: 2026-10-26T04:51:13+08:00
//	Error invalid tool call: There was a problem parsing the tool call.
//	Error Message: model output error: invalid tool call error (invalid_signature) AbsolutePath is a required parameter. Follow the function call schema exactly.
//	Guidance: You are trying to correct your previous tool call error, you must focus on fixing the failed tool call with sequential tool calls and try again. Do not do parallel tool calls and if you are fixing multiple tool calls, do them one at a time. Do not apologize.
//	Retries remaining: 4.
//
// The frame carries the sentence and quotes the reason:
//
//	The model produced an invalid tool call. ("model output error: invalid tool call error (invalid_signature) AbsolutePath is a required parameter. Follow the function call schema exactly.")
//
// # The retry counter
//
// The counter starts at four and falls by one per refused call; a call
// that succeeds resets it. The reference lets the model retry four times
// and gives up on the fifth refusal — the session ends there.
//
// # The rules
//
// The reference's runtime schema is **not** the schema it advertises: a
// `run_command` with no `CommandLine` runs, a `list_dir` with no
// `DirectoryPath` runs, a `find_by_name` with no `Pattern` runs. The
// rules below are the ones the captures pin, and a call the reference runs
// is never refused here.
package harnesstools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// The reference's three kinds of invalid call.
const (
	// InvalidArgs is a call whose arguments do not make sense.
	InvalidArgs = "invalid_args"
	// InvalidSignature is a call that misses a field the schema requires.
	InvalidSignature = "invalid_signature"
	// UnknownTool is a call to a tool that does not exist.
	UnknownTool = "unknown_tool"
)

// InvalidRetries is how many times the reference lets the model retry a
// call it got wrong.
const InvalidRetries = 4

// InvalidMessage is the sentence a frame carries for a call the reference
// refuses for a reason it has no sentence of its own for.
const InvalidMessage = "The model produced an invalid tool call."

// InvalidCall is a call the reference refuses before it runs.
type InvalidCall struct {
	// Kind is the reference's error kind: `invalid_args`,
	// `invalid_signature` or `unknown_tool`.
	Kind string
	// Detail is the reason the reference gives.
	Detail string
	// Message is the sentence the frame carries.
	//
	// Most refusals have none, and the frame says the generic sentence
	// ("The model produced an invalid tool call."). A refusal the
	// reference's own tool words carries its own: an edit of a file that
	// is not there says "Agent cannot edit files that do not exist.".
	Message string
}

// Reason is the sentence the reference's error carries.
func (c InvalidCall) Reason() string {
	return "model output error: invalid tool call error (" + c.Kind + ") " + c.Detail
}

// Invalid renders the text the model reads for a refused call.
//
// The paragraph is the reference's own: the step's invalid branch, the
// error message, the guidance and the retry counter. Only the first
// stamp line is there — the call never ran, so there is no completion
// time.
func Invalid(reason string, retries int, at time.Time) string {
	if at.IsZero() {
		at = time.Now()
	}
	return "Created At: " + at.Format(time.RFC3339) + "\n" +
		"Error invalid tool call: There was a problem parsing the tool call. \n" +
		"Error Message: " + reason + " \n" +
		"Guidance: You are trying to correct your previous tool call error, you must focus on fixing the failed tool call with sequential tool calls and try again. Do not do parallel tool calls and if you are fixing multiple tool calls, do them one at a time. Do not apologize. \n" +
		"Retries remaining: " + strconv.Itoa(retries) + "."
}

// InvalidFrame renders the text a refused call's frame carries.
//
// The frame says the model produced an invalid tool call and quotes the
// reason, the way the frame of a refused call quotes its own. A refusal
// the reference words of its own carries that sentence instead.
func InvalidFrame(message, reason string) string {
	if message == "" {
		message = InvalidMessage
	}
	return message + ` ("` + reason + `")`
}

// CheckCall reports why the reference refuses a call before it runs.
//
// The second result says whether the call must be refused. `known` is the
// set of tool names the session advertises: a call to a tool that is not
// among them is an unknown tool.
//
// The order of the checks is the reference's own, and it differs per tool:
// the write tools check their own arguments first ("argument TargetFile
// not found"), then the schema's required fields, then the path.
func CheckCall(tool string, args json.RawMessage, known map[string]bool) (InvalidCall, bool) {
	if len(known) > 0 && !known[tool] {
		return InvalidCall{
			Kind:   UnknownTool,
			Detail: "unknown tool name: `" + tool + "`",
		}, true
	}
	switch tool {
	case ToolViewFile:
		if !argFilled(args, "AbsolutePath", "path", "file_path") {
			return InvalidCall{
				Kind:   InvalidSignature,
				Detail: "AbsolutePath is a required parameter. Follow the function call schema exactly.",
			}, true
		}
		return checkPath(args, "AbsolutePath", "path", "file_path")
	case ToolListDir:
		return checkPath(args, "DirectoryPath", "path", "directory_path")
	case ToolGrepSearch:
		if !argFilled(args, "Query", "query", "pattern") {
			return InvalidCall{Kind: InvalidSignature, Detail: "Query is required"}, true
		}
		if !argFilled(args, "SearchPath", "DirectoryPath", "path", "directory_path") {
			return InvalidCall{Kind: InvalidSignature, Detail: "SearchPath is required"}, true
		}
		return checkPath(args, "SearchPath", "DirectoryPath", "path", "directory_path")
	case ToolFindByName:
		// The reference's name search has no path check of its own: a
		// relative directory runs and the step fails.
		if !argFilled(args, "SearchDirectory", "DirectoryPath", "path", "directory_path") {
			return InvalidCall{Kind: InvalidSignature, Detail: "SearchDirectory is required"}, true
		}
	case ToolWriteToFile:
		if !argPresent(args, "TargetFile", "AbsolutePath", "path", "file_path") {
			return InvalidCall{Kind: InvalidArgs, Detail: "argument TargetFile not found"}, true
		}
		if !argPresent(args, "Overwrite", "overwrite") {
			return InvalidCall{Kind: InvalidArgs, Detail: "argument Overwrite not found"}, true
		}
		if !argFilled(args, "CodeContent", "code_content", "content") {
			return InvalidCall{
				Kind:   InvalidSignature,
				Detail: "CodeContent is a required parameter. Please follow the function call schema exactly.",
			}, true
		}
		return checkPath(args, "TargetFile", "AbsolutePath", "path", "file_path")
	case ToolReplaceFile:
		if !argPresent(args, "TargetFile", "AbsolutePath", "path", "file_path") {
			return InvalidCall{Kind: InvalidArgs, Detail: "argument TargetFile not found"}, true
		}
		if !argFilled(args, "TargetContent", "ReplacementContent", "target_content", "replacement_content") {
			return InvalidCall{Kind: InvalidSignature, Detail: "ReplacementChunk is required"}, true
		}
		path := argString(args, "TargetFile", "AbsolutePath", "path", "file_path")
		if !filepath.IsAbs(path) {
			return InvalidCall{
				Kind:   InvalidArgs,
				Detail: path + " must be an absolute path: path is not absolute",
			}, true
		}
		if _, err := os.Stat(path); err != nil {
			return InvalidCall{
				Kind:    InvalidArgs,
				Detail:  path + " does not exist in the current location. Make sure the file path correct. In addition, the user may have moved, renamed, or deleted file.",
				Message: "Agent cannot edit files that do not exist.",
			}, true
		}
	}
	return InvalidCall{}, false
}

// checkPath refuses a call whose path is not absolute.
//
// The reference's wording names the path as the model wrote it:
//
//	rel.txt must be an absolute path: path is not absolute
//
// An empty path is not a path at all: the tools that take an optional
// directory run with one.
func checkPath(args json.RawMessage, names ...string) (InvalidCall, bool) {
	path := argString(args, names...)
	if path == "" || filepath.IsAbs(path) {
		return InvalidCall{}, false
	}
	return InvalidCall{
		Kind:   InvalidArgs,
		Detail: path + " must be an absolute path: path is not absolute",
	}, true
}

// argPresent reports whether an argument was given at all.
//
// A boolean that is false is still an argument: `Overwrite: false` is the
// model's answer, not a missing one.
func argPresent(args json.RawMessage, names ...string) bool {
	if len(args) == 0 {
		return false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		return false
	}
	for _, name := range names {
		if _, ok := fields[name]; ok {
			return true
		}
	}
	return false
}

// argFilled reports whether an argument was given and carries something.
//
// The reference's own wording for a missing field is the field's name, so
// an empty string does not fill the field: `Query: ""` is still a call
// without a query.
func argFilled(args json.RawMessage, names ...string) bool {
	if len(args) == 0 {
		return false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		return false
	}
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var text string
		if err := json.Unmarshal(raw, &text); err == nil {
			if text != "" {
				return true
			}
			continue
		}
		// A field that is not a string is filled by its presence.
		return true
	}
	return false
}
