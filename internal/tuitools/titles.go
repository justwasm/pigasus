// Package tuitools provides tool step formatting, argument parsing,
// category groupings, spinner animations, and unified diff hunks.
package tuitools

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/internal/agystep"
	"github.com/justwasm/pigasus/palette"
)

// ToolStepTitleFile returns the formatted title for file tools.
func ToolStepTitleFile(name string, args json.RawMessage) string {
	switch name {
	case "view_file", "read_file":
		return "Read(" + ArgStr(args, "AbsolutePath", "Path", "path", "file_path", "absolute_path", "file") + ")"
	case "write_to_file", "write_file", "create_file":
		return "Create(" + ArgStr(args, "TargetFile", "Path", "AbsolutePath", "path", "file_path", "absolute_path", "target_file", "file") + ")"
	case "replace_file_content", "edit_file", "multi_replace_file_content", "sed_file":
		return "Edit(" + ArgStr(args, "TargetFile", "Path", "AbsolutePath", "path", "file_path", "target_file", "file") + ")"
	case "list_dir":
		return "Listed " + Clip(ArgStr(args, "DirectoryPath", "path", "directory", "dir"), 60)
	}
	return ""
}

// ToolStepTitleSearch returns the formatted title for search tools.
func ToolStepTitleSearch(name string, args json.RawMessage) string {
	switch name {
	case "grep_search":
		return "Searched " + Clip(ArgStr(args, "Query", "query", "pattern", "search_term"), 60)
	case "find_by_name":
		return "Explored " + Clip(ArgStr(args, "SearchPath", "path", "pattern", "query"), 60)
	case "codebase_search":
		return "Searched codebase for " + Clip(ArgStr(args, "Query", "query", "search_term"), 60)
	case "read_url_content":
		return "Read " + Clip(ArgStr(args, "Url", "url"), 60)
	case "search_web":
		return "WebSearch(" + ArgStr(args, "Query", "query", "q") + ")"
	}
	return ""
}

// ToolStepTitleMisc returns the formatted title for miscellaneous tools.
func ToolStepTitleMisc(name string, args json.RawMessage) string {
	switch name {
	case "run_command":
		if c := ArgStr(args, "CommandLine", "command", "cmd"); c != "" {
			return "Bash(" + strings.TrimSpace(c) + ")"
		}
		return "Bash(a command)"
	case "command_status":
		return "Checked command"
	case "send_command_input":
		return "Sent input to command"
	case "generate_image":
		return "Generated image"
	case "view_content_chunk":
		return "Viewed content"
	case "finish":
		return "Finished"
	case "ask_question":
		return "Asked " + Clip(ArgStr(args, "question", "prompt"), 60)
	case "ask_permission", "ask_custom_permission":
		return "Needs approval"
	case "notify_user":
		return "Notified " + Clip(ArgStr(args, "message", "text"), 60)
	case "wait":
		return "Waited"
	case "schedule":
		return "Scheduled " + Clip(ArgStr(args, "Prompt", "prompt", "message", "cron"), 60)
	case "manage_task":
		return "Managed task"
	case "send_message":
		return "Messaged " + Clip(ArgStr(args, "message", "text"), 60)
	case "list_permissions":
		return "Listed permissions"
	case "manage_inbox":
		return "Checked inbox"
	case "notebook_edit":
		return "Edited notebook"
	case "notebook_execution":
		return "Ran notebook"
	case "list_resources":
		return "Listed resources"
	case "read_resource":
		return "Read resource"
	case "call_mcp":
		return "Called " + Clip(ArgStr(args, "server", "server_name", "tool"), 60)
	}
	return name
}

// ToolStepTitle formats a single tool step's title.
func ToolStepTitle(name string, args json.RawMessage) string {
	if t := ToolStepTitleFile(name, args); t != "" {
		return t
	}
	if t := ToolStepTitleSearch(name, args); t != "" {
		return t
	}
	return ToolStepTitleMisc(name, args)
}

// ToolStepSummary returns the collapsed one-line form of a tool call.
func ToolStepSummary(name string, args json.RawMessage) string {
	switch name {
	case "run_command":
		if c := ArgStr(args, "CommandLine", "command", "cmd"); c != "" {
			return "Ran (" + strings.TrimSpace(c) + ")"
		}
		return "Ran (a command)"
	case "view_file", "read_file":
		p := ArgStr(args, "AbsolutePath", "Path", "path", "file_path", "absolute_path", "file")
		if p != "" {
			return "Explored 1 file (" + filepath.Base(p) + ")"
		}
		return "Explored 1 file"
	case "search_web":
		if q := ArgStr(args, "Query", "query", "q"); q != "" {
			return "Explored 1 search (" + q + ")"
		}
		return "Explored 1 search"
	case "write_to_file", "write_file", "create_file",
		"replace_file_content", "edit_file", "multi_replace_file_content", "sed_file":
		p := ArgStr(args, "TargetFile", "Path", "AbsolutePath", "path", "file_path", "target_file", "file")
		if p != "" {
			return "Edited 1 file (" + filepath.Base(p) + ")"
		}
		return "Edited 1 file"
	}
	return ""
}

// RunningSummary returns the in-flight summary string for a step.
func RunningSummary(name string, args json.RawMessage) string {
	base := ToolStepSummary(name, args)
	for _, pair := range [][2]string{
		{"Ran ", "Running "},
		{"Explored ", "Exploring "},
		{"Edited ", "Editing "},
	} {
		if strings.HasPrefix(base, pair[0]) {
			return pair[1] + strings.TrimPrefix(base, pair[0])
		}
	}
	if t := ToolStepTitle(name, args); t != "" {
		return "Running (" + t + ")"
	}
	return "Running (" + name + ")"
}

// RunningSpinnerText returns the spinner suffix for an executing tool.
func RunningSpinnerText(name string) string {
	switch ToolCategory(name) {
	case "command":
		return "Running command"
	case "file":
		return "Reading file"
	case "search":
		return "Searching web"
	}
	return "Working"
}

// SpinnerText renders the animated activity label next to the braille spinner.
func SpinnerText(phrase string, frame int) string {
	if phrase == "" {
		phrase = "Working"
	}
	total := len(phrase) + 3
	phaseA := total - 5
	if phaseA < 1 {
		phaseA = 1
	}
	cycle := phaseA + 4
	f := frame % cycle
	if f < 0 {
		f += cycle
	}
	if f >= phaseA {
		dots := f - phaseA
		if dots > 3 {
			dots = 3
		}
		return palette.SpinnerBaseOpen + phrase + strings.Repeat(".", dots) + "\x1b[m"
	}
	n := 6 + f
	if n > total {
		n = total
	}
	visible := phrase
	if n > len(phrase) {
		visible += strings.Repeat(".", n-len(phrase))
	}
	visible = visible[:n]
	split := n - 5
	if split < 0 {
		split = 0
	}
	return palette.SpinnerBaseOpen + visible[:split] + palette.SpinnerHiOpen + visible[split:] + "\x1b[m"
}

// CommandExitCode parses the exit code out of a run_command result body.
//
// The reference prints the exit line in one of two shapes — "Command exited
// with code N." (the CLI's own formatter, which is what agy 1.4.4 and 1.4.7
// send) and "The command exited with code N." (the step string, which is
// what the reference produced at 1.4.2 and 1.4.3). One reader for both
// lives in internal/agystep; this is its thin wrapper.
func CommandExitCode(result string) (code int, ok bool) {
	res, ok := agystep.ParseRunCommandResult(result)
	if !ok {
		return 0, false
	}
	return res.ExitCode, true
}

// ArgStr returns the first present string-ish argument among keys in JSON args.
func ArgStr(args json.RawMessage, keys ...string) string {
	if len(args) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(args, &m); err != nil {
		return ""
	}
	for _, k := range keys {
		v, ok := m[k]
		if !ok {
			continue
		}
		switch t := v.(type) {
		case string:
			return t
		case float64:
			return fmt.Sprintf("%v", t)
		case bool:
			return fmt.Sprintf("%v", t)
		}
	}
	return ""
}

// Clip collapses newlines and truncates to n runes.
func Clip(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.TrimSpace(s)
	if n < 0 {
		return s
	}
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "\u2026"
	}
	return s
}
