package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

func toolStepTitleFile(name string, args json.RawMessage) string {
	switch name {
	case "view_file", "read_file":
		return "Read(" + argStr(args, "AbsolutePath", "Path", "path", "file_path", "absolute_path", "file") + ")"
	case "write_to_file", "write_file", "create_file":
		return "Create(" + argStr(args, "TargetFile", "Path", "AbsolutePath", "path", "file_path", "absolute_path", "target_file", "file") + ")"
	case "replace_file_content", "edit_file", "multi_replace_file_content", "sed_file":
		return "Edit(" + argStr(args, "TargetFile", "Path", "AbsolutePath", "path", "file_path", "target_file", "file") + ")"
	case "list_dir":
		return "Listed " + clip(argStr(args, "DirectoryPath", "path", "directory", "dir"), 60)
	}
	return ""
}

func toolStepTitleSearch(name string, args json.RawMessage) string {
	switch name {
	case "grep_search":
		return "Searched " + clip(argStr(args, "Query", "query", "pattern", "search_term"), 60)
	case "find_by_name":
		return "Explored " + clip(argStr(args, "SearchPath", "path", "pattern", "query"), 60)
	case "codebase_search":
		return "Searched codebase for " + clip(argStr(args, "Query", "query", "search_term"), 60)
	case "read_url_content":
		return "Read " + clip(argStr(args, "Url", "url"), 60)
	case "search_web":
		return "WebSearch(" + argStr(args, "Query", "query", "q") + ")"
	}
	return ""
}

func toolStepTitleMisc(name string, args json.RawMessage) string {
	switch name {
	case "run_command":
		if c := argStr(args, "CommandLine", "command", "cmd"); c != "" {
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
		return "Asked " + clip(argStr(args, "question", "prompt"), 60)
	case "ask_permission", "ask_custom_permission":
		return "Needs approval"
	case "notify_user":
		return "Notified " + clip(argStr(args, "message", "text"), 60)
	case "wait":
		return "Waited"
	case "schedule":
		return "Scheduled " + clip(argStr(args, "Prompt", "prompt", "message", "cron"), 60)
	case "manage_task":
		return "Managed task"
	case "send_message":
		return "Messaged " + clip(argStr(args, "message", "text"), 60)
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
		return "Called " + clip(argStr(args, "server", "server_name", "tool"), 60)
	}
	return name
}

func toolStepTitle(name string, args json.RawMessage) string {
	if t := toolStepTitleFile(name, args); t != "" {
		return t
	}
	if t := toolStepTitleSearch(name, args); t != "" {
		return t
	}
	return toolStepTitleMisc(name, args)
}

// toolStepSummary returns the collapsed one-line form of a tool call — what
// the transcript shows before ctrl+o. Verified against agy 1.4.3:
//
//	● Ran (echo one) (ctrl+o to expand)
//	● Ran (echo boom; exit 2) failed (ctrl+o to expand)
//	● Explored 1 file (agy-cont.sh) (ctrl+o to expand)
//
// Only run_command and view_file have captured collapsed forms; everything
// else falls back to the expanded title ("" ⇒ the renderer uses title).
func toolStepSummary(name string, args json.RawMessage) string {
	switch name {
	case "run_command":
		if c := argStr(args, "CommandLine", "command", "cmd"); c != "" {
			return "Ran (" + strings.TrimSpace(c) + ")"
		}
		return "Ran (a command)"
	case "view_file", "read_file":
		p := argStr(args, "AbsolutePath", "Path", "path", "file_path", "absolute_path", "file")
		if p != "" {
			return "Explored 1 file (" + filepath.Base(p) + ")"
		}
		return "Explored 1 file"
	case "search_web":
		// Collapsed form measured on 1.4.3: "● Explored 1 search (golang)
		// (ctrl+o to expand)" — the verb is the shared Explore, not a
		// search-specific one.
		if q := argStr(args, "Query", "query", "q"); q != "" {
			return "Explored 1 search (" + q + ")"
		}
		return "Explored 1 search"
	case "write_to_file", "write_file", "create_file",
		"replace_file_content", "edit_file", "multi_replace_file_content", "sed_file":
		// 1.4.3 labels every file mutation "Edited", including a create
		// (measured: a write_file that created /tmp/w285b.txt rendered
		// "● Edited 1 file (w285b.txt) (ctrl+o to expand)").
		p := argStr(args, "TargetFile", "Path", "AbsolutePath", "path", "file_path", "target_file", "file")
		if p != "" {
			return "Edited 1 file (" + filepath.Base(p) + ")"
		}
		return "Edited 1 file"
	}
	return ""
}

// isWriteTool reports whether name is a file-creation/overwrite tool.
func newRunningStep(name string, args json.RawMessage) toolStep {
	return toolStep{
		kind:    stepKindTool,
		name:    name,
		title:   toolStepTitle(name, args),
		summary: runningSummary(name, args),
		running: true,
	}
}

// runningSummary is the in-flight form of the step. 1.4.3 keeps the finished
// summary's count and args and swaps the verb to its present participle
// (measured: "● Running (sleep 8; echo hi)" for a command and
// "● Exploring 1 search (golang)" for search_web, both with the amber
// running bullet). The file and edit forms follow the same rule and are
// inferred from it.
func runningSummary(name string, args json.RawMessage) string {
	base := toolStepSummary(name, args)
	for _, pair := range [][2]string{
		{"Ran ", "Running "},
		{"Explored ", "Exploring "},
		{"Edited ", "Editing "},
	} {
		if strings.HasPrefix(base, pair[0]) {
			return pair[1] + strings.TrimPrefix(base, pair[0])
		}
	}
	if t := toolStepTitle(name, args); t != "" {
		return "Running (" + t + ")"
	}
	return "Running (" + name + ")"
}

// runningSpinnerText is the spinner suffix while a call executes. 1.4.3
// prints "Running command..." for a shell call (verified byte-for-byte); the
// other categories are inferred (【推测】).
func runningSpinnerText(name string) string {
	switch toolCategory(name) {
	case "command":
		return "Running command"
	case "file":
		return "Reading file"
	case "search":
		// Measured on 1.4.3: "⣿  Searching web..." while search_web runs.
		return "Searching web"
	}
	// Unmeasured; the reference most likely reuses its generic
	// worker phrase here rather than naming the tool.
	return "Working"
}

// spinnerText renders the animated activity label the reference paints next to
// the braille spinner. Measured frame by frame from agy 1.4.3 pty captures:
//
//	phase A — the visible string grows from 6 characters to len(phrase)+3
//	          and its last five characters carry the #268BD2 highlight:
//	          "R|eadin", "Re|ading", …, "Reading f|ile..", "Reading fi|le..."
//	phase B — the phrase is repainted in base gray (#657B83) with the
//	          trailing dots cycling 0,1,2,3:
//	          "Reading file", "Reading file.", "Reading file..", "Reading file..."
//
// (The "|" marks the base/highlight boundary; it is not emitted.) The frame
// cadence follows our spinner tick, which is the same 8-glyph braille cycle
// the reference uses.
func spinnerText(phrase string, frame int) string {
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
		return cSpinnerBaseOpen + phrase + strings.Repeat(".", dots) + "\x1b[m"
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
	return cSpinnerBaseOpen + visible[:split] + cSpinnerHiOpen + visible[split:] + "\x1b[m"
}

// commandExitCode parses the exit code out of a run_command result body
// (internal/tools/command.go: "The command exited with code %d."). ok is
// false for results that do not carry that line, e.g. a tool-level failure.
func commandExitCode(result string) (code int, ok bool) {
	m := commandExitRe.FindStringSubmatch(result)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	return n, true
}

var commandExitRe = regexp.MustCompile(`The command exited with code (-?\d+)\.`)

// newToolStep builds the transcript step for one finished call. The
// reference marks a non-zero exit as "failed" (red bullet + the word
// "failed") even though the tool call itself succeeded — verified against
// 1.4.3 with `echo boom; exit 2`.
func newToolStep(name string, args json.RawMessage, result string, toolErr bool) toolStep {
	s := toolStep{
		kind:    stepKindTool,
		name:    name,
		title:   toolStepTitle(name, args),
		summary: toolStepSummary(name, args),
		detail:  result,
		result:  result,
		err:     toolErr,
	}
	if name == "run_command" {
		if code, ok := commandExitCode(result); ok && code != 0 {
			s.err = true
		}
	}
	if isWriteTool(name) {
		// 1.4.3 shows the line count of the written content, not the tool
		// result (measured: "Wrote 1 line" / "Wrote 3 lines").
		if n := fileLineCount(argStr(args, "content", "Content")); n > 0 {
			s.detail = "Wrote " + countLabel(n, fileLinePair)
		}
		if !toolErr {
			// AGY 1.4.1: file creation also renders inline diff preview
			s.diff = createDiffFor(args)
		}
	}
	if isEditTool(name) && !toolErr {
		// 1.4.3 prints a unified hunk, not the tool result. The call has
		// already run by the time the step is built, so the file on disk
		// is the post-edit text and the hunk is recovered from it.
		s.diff = editDiffFor(args)
	}
	return s
}

// fileLineCount returns the number of lines in the file-creation tool's
// content payload.
//
// Cycle count-pluralization: this recovers the AGY 1.3.3 changelog fix
// "and the file-creation step reporting one line too many". The reference
// ships the literals `Wrote %d lines` / `Wrote 1 line`, so the title needs
// a count that matches what `wc -l` would print — the model almost always
// emits files ending in "\n", so a naive `len(strings.Split(content, "\n"))`
// returns N+1 (the trailing empty element). The convention below is:
//
//   - empty content -> 0 lines (no count suffix in the title),
//   - "x"          -> 1 line,
//   - "x\n"        -> 1 line (trailing newline does not add a phantom row),
//   - "x\ny"       -> 2 lines (last line has no trailing newline),
//   - "x\ny\n"     -> 2 lines.
//
// This mirrors `countFileLines` in slash_viewer_panels.go, which counts the
// same way for untracked files in the /diff file-list panel — keeping both
// surfaces in agreement so a freshly created file and a freshly listed
// untracked file show the same line count.
func fileLineCount(content string) int {
	if content == "" {
		return 0
	}
	// Count newlines AFTER trimming trailing ones so a file ending in "\n"
	// doesn't contribute a phantom line.
	n := strings.Count(strings.TrimRight(content, "\n"), "\n") + 1
	return n
}

// fileLinePair is the singular/plural pair the changelog names as "Wrote 1
// line" / "Wrote N lines". It is not in `categoryWords` because "line" is
// a unit used by a single title template, not a category of tools; this
// keeps the changelog-named surface visible without polluting the count
// vocabulary the group header and codesearch panel share.
var fileLinePair = [2]string{"line", "lines"}

// argStr returns the first present string-ish argument among keys.
func argStr(args json.RawMessage, keys ...string) string {
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

// clip collapses newlines and truncates to n runes.
func clip(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.TrimSpace(s)
	// Cycle 161: ref never truncates run_command args — it just
	// lets the terminal wrap / overflow. The pre-161 clone
	// truncated to 60 runes with "…", which produced a visibly
	// shorter tool title. Pass n >= 1<<30 to disable truncation;
	// callers that still want a cap pass a finite n.
	if n < 0 {
		return s
	}
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "\u2026"
	}
	return s
}

// toolBlockLines renders one turn's tool steps.
//
// Layout (reconstructed — see the file comment):
//
//	single step, collapsed:  ● Bash (rm …)  (ctrl+o to expand)
//	single step, expanded:   ● Bash (rm …)  (ctrl+o to collapse)
//	                           ⎿  <output>
//	group, collapsed:        ● Ran 2 commands (+1 · git status …)  (ctrl+o to expand)
//	group, expanded:         ● Ran 2 commands (+1 · git status …)
//	                         ● Bash(rm …
//	                           echo "---deleted---")
//	                           ⎿  <output>
//	                         ● Bash(git status …)
//	                           ⎿  <output>  (ctrl+o to collapse)
//
// Cycle 178: the reference hides the per-step titles while the group is
// collapsed — only the group header (which carries the "(+N · <last>)"
// suffix so the user still sees what is inside) is shown. The
// changelog wording about "left a bare `(ctrl+o to expand)` hint with
// no tool name" is satisfied by the group header's embedded "(+1 · …)"
// not by an extra bullet per step.
//
// Cycle 178: the reference's expanded-mode hint lives on the last
// step's detail tail, not on the group header. The group header keeps
// a bare "<verb> <n> <noun> (+N · <last-title>)" line; the hint only
// follows the very last detail line in expanded form. In collapsed
// form (the only state where the hint is visible) the trailing hint
// stays on the group header so a user has something to click.
