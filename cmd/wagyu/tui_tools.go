// tui_tools.go wires the recovered tool surface into the interactive TUI.
//
// Cycle 99. Before this cycle the interactive session never advertised any
// tools: runMode.streamPrompt issued a single backend.Stream with no
// `tools` and discarded every StreamChunk.ToolCalls. A live capture of the
// 1.3.6 reference-driven session (pty driver, prompt "Read secret.txt with
// view_file") shows the model answering
//
//	I don't have a file reading tool available. There's no view_file tool in
//	my function list.
//
// i.e. the agent could not act. Print mode (--print) already drove the
// cycle-97 agent loop; this file gives the interactive session the same loop
// plus the transcript rendering for the tool steps.
//
// Recovered vocabulary (1.3.6 string table; evidence grade 【strings】)
//
//	ctrl+o to expand        collapsed tool group hint
//	ctrl+o to collapse      expanded tool group hint
//	%d steps                tool group summary line
//	<output +%d lines>      truncated expanded detail
//	Ran / Read / Wrote / Explored / Searched / Listed / Messaged /
//	Spawning / Browsing / Needs approval / Failed   step titles
//	KeyToggleExpandGroup    the ctrl+o keybinding's name
//
// The changelog (shipped in the binary, dumped via `agy -p /changelog`) names
// the same surfaces: "groups related tool calls and thoughts into concise
// summaries", "(ctrl+o to expand)", "a more specific hint on the spinner line,
// which now names the tool call the agent is currently running", and "Changed
// the step title of commands that exit with a non-zero code", with the
// recovered step-title word for non-zero exits being `Failed`.
//
// The exact glyph layout is a reconstruction (【推测】): the reference renders
// through its own conversation-view component and no offline capture of a
// tool-bearing transcript is available (the account's quota is exhausted, so
// every reference turn terminates with RESOURCE_EXHAUSTED before a step can be
// drawn). Names, hints and the expand/collapse behaviour are the recovered
// parts; the arrangement is ours.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/mdrender"
	"github.com/justwasm/pigasus/internal/tools"
)

// Bullets used by the tool-step transcript. \u23fa is the "record" glyph the
// reference uses to lead a step; \u23bf is the same detail glyph the reference
// warning block uses ("  \u23bf  \"<model>\" is no longer available."), which
// renderWarning already emits.
//
// Cycle 127: the step bullet is wrapped in the brand-blue
// `cBlueOpen`/`cBlueClose` so it visually anchors the tool step
// against the dim gray transcript body. The detail bullet was
// already wrapped in `cWarningBlueOpen` (used by the warning block)
// — cycle 127 keeps that.
const (
	// Cycle 156: tool-bullet glyph. v1.3.6 emits U+25CF BLACK CIRCLE
	// ("●") in the brand teal (256-colour 29); the cycle-127 clone
	// used U+23FA ⏺ "record" in brand blue, picked from Aider/Cursor
	// convention rather than the v1.3.6 binary itself. The recovered
	// capture-pane diff in docs/cycles/cycle-156/ confirms the byte.
	stepBulletOpen  = cToolBulletOpen + "\u25cf" + cToolBulletClose
	stepBulletPlain = "\u25cf"

	// Cycle 157: failed-tool-bullet glyph. v1.3.6 swaps the bullet
	// palette to red (256-colour 166) when the tool exited non-zero,
	// but keeps the U+25CF glyph and the same close escape.
	stepBulletErrOpen = cToolBulletErrOpen + "\u25cf" + cToolBulletClose

	// Cycle 156: thought-bullet glyph. v1.3.6 uses U+25B8 BLACK
	// RIGHT-POINTING SMALL TRIANGLE ("▸") in dim gray (256-colour
	// 238); the cycle-127 clone used U+23BF ⎿ "down tack and overbar"
	// in warning-blue, again from a non-v1.3.6 source.
	thoughtBulletOpen  = cThoughtBulletOpen + "\u25b8" + cThoughtBulletClose
	thoughtBulletPlain = "\u25b8"

	// Detail (tool-step expansion) bullet. Unchanged by cycle 156 —
	// the diff-add / diff-remove chevrons render in their own
	// colours and weren't part of the cycle-156 recovery.
	// Cycle 285: the reference paints the whole "  ⎿  " prefix in the
	// secondary gray and resets after the trailing spaces (measured:
	// "\x1b[38;2;88;110;117m  ⎿  \x1b[mCommand exited…"), so the prefix
	// owns its leading spaces too and callers append it verbatim.
	detailBulletOpen  = cDetailBulletOpen + "  \u23bf  " + "\x1b[m"
	detailBulletPlain = "\u23bf"
)

// styledStepBullet is the bullet the transcript paints for a call in the
// given state. 1.4.3 only reddens the bullet in the *collapsed* summary
// ("● Ran (… ) failed"); once expanded the head is "● Bash(…)" with the
// success green bullet even for a non-zero exit (measured raw:
// \x1b[38;2;133;153;0m● with the red left to the detail text).
func styledStepBullet(s toolStep, expanded bool) string {
	if s.kind == stepKindThought {
		return thoughtBulletOpen
	}
	if s.running {
		return cToolBulletRunningOpen + "\u25cf" + cToolBulletClose
	}
	if s.err && !expanded {
		return stepBulletErrOpen
	}
	return stepBulletOpen
}

// stepBullet is the unstyled byte-level bullet. It is kept here for
// callers that want the bare glyph (e.g. tests that don't need colour);
// the styled form above is what the rendered transcript emits.
//
// Deprecated alias: cycle 127 introduces styled variants; new code
// should reach for stepBulletOpen / stepBulletPlain directly.
const stepBullet = stepBulletPlain

// maxStepDetailLines caps how many output lines the expanded view shows before
// switching to the recovered "<output +%d lines>" overflow marker.
const maxStepDetailLines = 20

// stepKind distinguishes a tool step from a thought step in the transcript.
// The reference's conversation view treats model reasoning as a first-class
// step: the recovered symbol table has coalesceThinkingSteps,
// canMergeConsecutiveThoughts and canUpgradeTrailingThoughtToToolGroup in
// cli/store, and cli/render has collapsedThinkingSnippet / thinkingLiveSnippet /
// summarizeThinkingSteps. Both kinds share the same group/expand machinery.
type stepKind int

const (
	stepKindTool stepKind = iota
	stepKindThought
)

// toolStep is one tool call (or thought) rendered in the conversation
// transcript.
type toolStep struct {
	kind stepKind
	name string
	// title is the expanded call form without styling, e.g.
	// "Bash(echo one)" or "Read(/tmp/x)" — the shape the reference prints
	// once the step is open.
	title string
	// summary is the collapsed form, e.g. "Ran (echo one)" or
	// "Explored 1 file (agy-cont.sh)". Empty falls back to title.
	summary string
	detail  string
	err     bool
	// running marks the provisional row painted while the call executes
	// ("Running (echo one)"). It is replaced in place when the call ends.
	running bool
	// diff is the rendered unified hunk for an edit_file step (nil for
	// every other step). See editDiff.
	diff *editDiff
	// result is the raw tool output, before the renderer rewrites detail
	// (write/edit steps replace it with a line count / hunk). /context's
	// "Tool calls" row counts this text, which is what the wire carries in
	// functionResponse.response.output (cycle 292).
	result string
	// batch groups calls emitted in the same assistant message; the
	// reference renders each batch as one block and keeps the assistant
	// text between batches. 0 is the default (single batch).
	batch int
	// Thought-only fields. tokens is the gateway's reasoning-token count
	// (usage.completion_tokens_details.reasoning_tokens); dur is how long
	// the model spent inside the think block.
	tokens int
	dur    time.Duration
}

// turnBlock records where a turn's tool-step block lives inside m.response so
// ctrl+o can re-render it in place. start/lines delimit the block; steps is the
// data needed to render it at either verbosity.
type turnBlock struct {
	start int
	lines int
	steps []toolStep
}

// liveStepRun records where a single in-flight step was painted in
// m.response. Until turnDoneMsg lands, a running turn's steps live only as
// rows in the transcript (they are not a turnBlock yet), so ctrl+o needs its
// own index to re-render them in place: one run per toolStepMsg, in paint
// order. Cleared on prompt submit and when the turn finalises into a block.
type liveStepRun struct {
	start int
	lines int
	step  toolStep
}

// toolStepTitle maps a recovered tool name plus its arguments onto the
// human-readable step title the conversation view shows. Vocabulary is the
// recovered step-title set (see the file comment); the mapping from tool name
// to verb is 【推测】, since the reference derives it from its own step model.
//
// Each case accepts both the clone's own lower-case argument names and the
// capitalised ones the reference's model actually emits. The reference's key
// names were read out of its stored conversations (cycle 120): run_command
// carries CommandLine/Cwd, view_file carries AbsolutePath/StartLine/EndLine,
// grep_search carries Query/SearchPath, replace_file_content and write_to_file
// carry TargetFile, list_dir carries DirectoryPath, schedule carries Prompt.
// Without them a resumed transcript renders every step as the bare fallback
// ("Ran a command") instead of "Ran ls /tmp".
func toolStepTitle(name string, args json.RawMessage) string {
	switch name {
	case "run_command":
		if c := argStr(args, "CommandLine", "command", "cmd"); c != "" {
			// Cycle 285: the reference's expanded call form is
			// "Bash(<CommandLine>)" — no space before the paren
			// (verified byte-for-byte against agy 1.4.3:
			// "\x1b[38;2;181;137;0;1mBash\x1b[13;17H\x1b[m" followed
			// by the dim "(echo one)"). The collapsed form is
			// "Ran (<CommandLine>)" (toolStepSummary).
			return "Bash(" + strings.TrimSpace(c) + ")"
		}
		return "Bash(a command)"
	case "command_status":
		return "Checked command"
	case "send_command_input":
		return "Sent input to command"
	case "view_file", "read_file":
		// Expanded form: "Read(/tmp/x)". Collapsed: "Explored 1 file
		// (x)" — see toolStepSummary.
		return "Read(" + argStr(args, "AbsolutePath", "Path", "path", "file_path", "absolute_path", "file") + ")"
	case "write_to_file", "write_file", "create_file":
		// Expanded form for a create/append/overwrite: "Create(path)"
		// (measured on agy 1.4.3 write_file). The collapsed form is
		// "Edited 1 file (base)" — see toolStepSummary.
		return "Create(" + argStr(args, "TargetFile", "Path", "AbsolutePath", "path", "file_path", "absolute_path", "target_file", "file") + ")"
	case "replace_file_content", "edit_file", "multi_replace_file_content", "sed_file":
		// Expanded form for an in-place edit: "Edit(path)" (measured on
		// agy 1.4.3 edit_file).
		return "Edit(" + argStr(args, "TargetFile", "Path", "AbsolutePath", "path", "file_path", "target_file", "file") + ")"
	case "list_dir":
		return "Listed " + clip(argStr(args, "DirectoryPath", "path", "directory", "dir"), 60)
	case "grep_search":
		return "Searched " + clip(argStr(args, "Query", "query", "pattern", "search_term"), 60)
	case "find_by_name":
		return "Explored " + clip(argStr(args, "SearchPath", "path", "pattern", "query"), 60)
	case "codebase_search":
		return "Searched codebase for " + clip(argStr(args, "Query", "query", "search_term"), 60)
	case "read_url_content":
		return "Read " + clip(argStr(args, "Url", "url"), 60)
	case "search_web":
		// Expanded head measured on 1.4.3: "● WebSearch(golang)".
		return "WebSearch(" + argStr(args, "Query", "query", "q") + ")"
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
func isWriteTool(name string) bool {
	switch name {
	case "write_to_file", "write_file", "create_file":
		return true
	}
	return false
}

// isEditTool reports whether name is an in-place file-edit tool.
func isEditTool(name string) bool {
	switch name {
	case "replace_file_content", "edit_file", "multi_replace_file_content", "sed_file":
		return true
	}
	return false
}

// editDiff is the transcript body of an edit_file step on agy 1.4.3: a
// "+A / -B lines" header followed by a unified hunk with three lines of
// context. Changed rows carry the base3 (#EEE8D5) stripe; context rows do
// not. Measured raw:
//
//	ﾠ⎿ﾠﾠ +1 / -1 lines
//	     1    alpha
//	     3 -  gamma
//	     3 +  GAMMA
//	     4    delta
type editDiff struct {
	path           string
	added, removed int
	rows           []editDiffRow
}

type editDiffRow struct {
	kind byte // ' ', '-', '+'
	num  int
	text string
}

// editDiffFor recovers the old and new file text from the tool arguments and
// the file that is now on disk, then builds the hunk.
func editDiffFor(args json.RawMessage) *editDiff {
	path := argStr(args, "TargetFile", "Path", "AbsolutePath", "path", "file_path", "target_file", "file")
	target := argStr(args, "Target", "TargetContent", "oldString", "old_string", "target")
	replacement := argStr(args, "Replacement", "ReplacementContent", "newString", "new_string", "replacement")
	if target == "" && replacement == "" {
		return nil
	}
	oldText, newText := target, replacement
	if path != "" {
		if b, err := os.ReadFile(path); err == nil {
			cur := string(b)
			switch {
			case replacement != "" && strings.Contains(cur, replacement):
				newText = cur
				oldText = strings.Replace(cur, replacement, target, 1)
			case strings.Contains(cur, target):
				oldText = cur
				newText = strings.Replace(cur, target, replacement, 1)
			}
		}
	}
	d := buildEditDiff(oldText, newText)
	if d != nil {
		d.path = path
	}
	return d
}

// createDiffFor builds the addition-only diff for a file creation tool.
// Matches AGY 1.4.1 createFilePreview / createFileDiffEntry.
func createDiffFor(args json.RawMessage) *editDiff {
	path := argStr(args, "TargetFile", "Path", "AbsolutePath", "path", "file_path", "target_file", "file")
	content := argStr(args, "content", "Content")
	if content == "" {
		return nil
	}
	d := buildEditDiff("", content)
	if d != nil {
		d.path = path
	}
	return d
}

// buildEditDiff trims the common prefix/suffix and keeps three context lines
// around the change — the reference's exact hunk for a single contiguous
// edit. The trailing empty string of a newline-terminated file is a real
// line to the reference (it renders as line N+1), so the split is kept as is.
func buildEditDiff(oldText, newText string) *editDiff {
	oldLines := strings.Split(oldText, "\n")
	newLines := strings.Split(newText, "\n")
	p := 0
	for p < len(oldLines) && p < len(newLines) && oldLines[p] == newLines[p] {
		p++
	}
	suf := 0
	for suf < len(oldLines)-p && suf < len(newLines)-p &&
		oldLines[len(oldLines)-1-suf] == newLines[len(newLines)-1-suf] {
		suf++
	}
	oldEnd := len(oldLines) - suf
	newEnd := len(newLines) - suf
	if p == oldEnd && p == newEnd {
		return nil
	}
	d := &editDiff{added: newEnd - p, removed: oldEnd - p}
	const ctx = 3
	start := p - ctx
	if start < 0 {
		start = 0
	}
	for i := start; i < p; i++ {
		d.rows = append(d.rows, editDiffRow{' ', i + 1, oldLines[i]})
	}
	for i := p; i < oldEnd; i++ {
		d.rows = append(d.rows, editDiffRow{'-', i + 1, oldLines[i]})
	}
	for i := p; i < newEnd; i++ {
		d.rows = append(d.rows, editDiffRow{'+', i + 1, newLines[i]})
	}
	for i := 0; i < ctx && newEnd+i < len(newLines); i++ {
		d.rows = append(d.rows, editDiffRow{' ', newEnd + i + 1, newLines[newEnd+i]})
	}
	return d
}

// lines renders the hunk. The row prefix is five spaces (the reference moves
// to column 6 before the four-character line number), and the stripe on a
// changed row runs to the right edge, exactly like the reference's \x1b[K
// with the base3 background still active.
func (d *editDiff) lines(width int) []string {
	head := detailBulletOpen + cToolAddOpen + fmt.Sprintf("+%d", d.added) + "\x1b[m / " +
		cToolRemoveOpen + fmt.Sprintf("-%d", d.removed) + "\x1b[m lines"
	out := []string{head}
	pad := func(text string) string {
		if width <= 0 {
			return ""
		}
		n := width - 13 - utf8.RuneCountInString(text)
		if n <= 0 {
			return ""
		}
		return strings.Repeat(" ", n)
	}
	for _, r := range d.rows {
		// AGY 1.4.1: When path is known, render like the /diff file view:
		// untinted line-number gutter, background tints on added/removed lines,
		// and syntax-highlighted tokens.
		if d.path != "" {
			styledText := r.text
			if tokens := tokenizeDiffLine(d.path, " "+r.text); len(tokens) > 1 {
				styledText = renderDiffInlineTokenStream(tokens[1:])
			}
			gutter := "     " + cDiffNumOpen + fmt.Sprintf("%4d", r.num) + "\x1b[m"
			switch r.kind {
			case '-':
				out = append(out, gutter+cDiffRemoveTintOpen+cToolRemoveOpen+" -  "+styledText+pad(r.text)+"\x1b[m")
			case '+':
				out = append(out, gutter+cDiffAddTintOpen+cToolAddOpen+" +  "+styledText+pad(r.text)+"\x1b[m")
			default:
				out = append(out, gutter+"    "+styledText)
			}
			continue
		}

		// Backward-compatible fallback when path is empty (pre-1.4.1 captures & tests):
		switch r.kind {
		case '-':
			out = append(out, "     "+cDiffStripeOpen+fmt.Sprintf("%4d", r.num)+
				cToolRemoveOpen+" -  "+r.text+pad(r.text)+"\x1b[m")
		case '+':
			out = append(out, "     "+cDiffStripeOpen+fmt.Sprintf("%4d", r.num)+
				cToolAddOpen+" +  "+r.text+pad(r.text)+"\x1b[m")
		default:
			out = append(out, "     "+cDiffNumOpen+fmt.Sprintf("%4d", r.num)+"\x1b[m    "+r.text)
		}
	}
	if n := len(d.rows); n > 0 {
		if k := d.rows[n-1].kind; k == '+' || k == '-' {
			// Keep the hint out of the stripe: the reference starts a
			// fresh row at column 6 (four spaces plus the caller's
			// separating space).
			out = append(out, "    ")
		}
	}
	return out
}

// newRunningStep builds the provisional row painted while a call is in
// flight. The reference's verb there is "Running", and the args are the same
// ones the finished summary shows:
//
//	● Running (sleep 8; echo hi) (ctrl+o to expand)   [命令]
//	● Reading (/tmp/x) (ctrl+o to expand)             [读文件, 【推测】]
//
// run_command is 【一手】; the reading/searching verbs are inferred from the
// same step model and are not separately captured.
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
func toolBlockLines(steps []toolStep, expanded bool, width int) []string {
	if len(steps) == 0 {
		return nil
	}
	// Thought steps are not part of a tool group: the reference prints
	// them as their own line above the calls.
	var thoughts, tools []toolStep
	for _, s := range steps {
		if s.kind == stepKindThought {
			thoughts = append(thoughts, s)
		} else {
			tools = append(tools, s)
		}
	}
	var out []string
	for _, s := range thoughts {
		out = append(out, thoughtStepLines(s, expanded, width)...)
	}
	switch len(tools) {
	case 0:
	case 1:
		out = append(out, stepLines(tools[0], expanded, width)...)
	default:
		out = append(out, groupLines(tools, expanded, width)...)
	}
	if len(out) == 0 {
		return nil
	}
	// The reference keeps the hint on the collapsed summary line, and in
	// expanded mode on the very last detail row (verified against 1.4.3:
	// "● Bash(echo one)" has no hint, the trailing "one (ctrl+o to
	// collapse)" does). A step with nothing to show keeps the hint on its
	// head so it is never a bare hint.
	hint := "ctrl+o to expand"
	if expanded {
		hint = "ctrl+o to collapse"
	}
	out[len(out)-1] = out[len(out)-1] + " " + styledHint(hint)
	return out
}

// stepLines renders one call. Collapsed is the reference's one-line summary
// ("Ran (echo one)"), expanded is the call form plus its detail rows.
func stepLines(s toolStep, expanded bool, width int) []string {
	if s.running || !expanded {
		line := styledStepBullet(s, false) + " " + styledSummary(s)
		if s.err && !s.running && toolCategory(s.name) == "command" {
			// Only a non-zero *command* carries the word (measured: a
			// failed search_web reddens the bullet but prints no
			// " failed").
			line += " " + cToolArgsOpen + "failed" + cToolArgsClose
		}
		return []string{line}
	}
	out := renderStepTitle(s, true)
	return append(out, stepDetailLines(s, width)...)
}

// categoryGroups splits a batch into per-category runs, in first-appearance
// order. 1.4.3 prints one summary row per category rather than one mixed
// header (measured: a read_file + edit_file batch rendered
// "● Explored 1 file (w285b.txt)" above "● Edited 1 file (w285b.txt)").
func categoryGroups(tools []toolStep) [][]toolStep {
	var groups [][]toolStep
	idx := map[string]int{}
	for _, s := range tools {
		c := toolCategory(s.name)
		if i, ok := idx[c]; ok {
			groups[i] = append(groups[i], s)
			continue
		}
		idx[c] = len(groups)
		groups = append(groups, []toolStep{s})
	}
	return groups
}

// groupLines renders a batch of calls emitted together (parallel tool
// calls). Collapsed shows one summary row per category; expanded keeps those
// rows and lists each call underneath them. Verified against 1.4.3:
//
//	● Ran 2 commands (echo one · echo two) (ctrl+o to expand)
//	● Ran 2 commands (echo one · echo two)
//	● Bash(echo one)
//	  ⎿  Command exited with code 0. …
//	● Bash(echo two)
//	  ⎿  … two (ctrl+o to collapse)
//
// A mixed batch keeps the same shape, one row per category:
//
//	● Explored 1 file (w285b.txt)
//	● Edited 1 file (w285b.txt)
//	● Read(/tmp/w285b.txt)   ← no detail: one-line file
//	● Edit(/tmp/w285b.txt)
//	  ⎿  +1 / -1 lines …
func groupLines(tools []toolStep, expanded bool, width int) []string {
	var out []string
	for _, g := range categoryGroups(tools) {
		out = append(out, styledStepBullet(g[0], expanded)+" "+styledGroupSummary(g))
	}
	if !expanded {
		return out
	}
	for _, s := range tools {
		out = append(out, renderStepTitle(s, true)...)
		out = append(out, stepDetailLines(s, width)...)
	}
	return out
}

// thoughtStepLines renders a thought step: "▸ Thought for 3s" plus, when
// expanded, the reasoning body.
func thoughtStepLines(s toolStep, expanded bool, width int) []string {
	head := thoughtBulletOpen + " " + thoughtTitle(s.dur, s.tokens)
	if !expanded {
		return []string{head}
	}
	out := []string{head}
	return append(out, stepDetailLines(s, width)...)
}

// renderStepTitle returns the styled expanded call title as rows. A
// run_command whose CommandLine spans shell separators (&&, ||, ;, or a
// newline) wraps onto continuation rows the way the reference does; every
// other tool is a single row.
func renderStepTitle(s toolStep, expanded bool) []string {
	if s.kind == stepKindThought {
		return []string{thoughtBulletOpen + " " + thoughtTitle(s.dur, s.tokens)}
	}
	return []string{styledStepBullet(s, expanded) + " " + styledCallTitle(s)}
}

// parenArg returns the inside of the final parenthesised group in a title,
// e.g. "Bash(echo one)" -> "echo one", "Ran (no parens here)" -> "".
func parenArg(title string) string {
	i := strings.LastIndexByte(title, '(')
	if i < 0 || !strings.HasSuffix(title, ")") {
		return ""
	}
	return title[i+1 : len(title)-1]
}

// stripBashParens extracts the inner command from a "Bash (cmd)" or
// "Bash(cmd)" title; returns cmd and ok=false when the prefix doesn't.
func stripBashParens(title string) (cmd string, ok bool) {
	if !strings.HasPrefix(title, "Bash ") {
		return "", false
	}
	rest := strings.TrimPrefix(title, "Bash ")
	rest = strings.TrimSpace(rest)
	if strings.HasPrefix(rest, "(") && strings.HasSuffix(rest, ")") {
		return rest[1 : len(rest)-1], true
	}
	return "", false
}

// splitShellSegments splits a shell command string into its pipeline
// segments, breaking at `&&`, `||`, `;`, or `\n` boundaries. The
// boundaries themselves are dropped from the returned segments.
//
// Cycle 178: the reference wraps `run_command` titles at these
// boundaries (verified by capturing the live `4638a7b0-…` transcript),
// so this is the only splitting rule the renderer needs.
func splitShellSegments(s string) []string {
	var out []string
	var cur strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		// Drop any leading whitespace on a new segment.
		if cur.Len() == 0 && (c == ' ' || c == '\t') {
			continue
		}
		if c == '\n' {
			if cur.Len() > 0 {
				out = append(out, strings.TrimSpace(cur.String()))
				cur.Reset()
			}
			continue
		}
		// Two-char operators: &&, ||.
		if i+1 < len(s) && s[i+1] == c && (c == '&' || c == '|') {
			if cur.Len() > 0 {
				out = append(out, strings.TrimSpace(cur.String()))
				cur.Reset()
			}
			i++ // skip the second char
			continue
		}
		if c == ';' {
			if cur.Len() > 0 {
				out = append(out, strings.TrimSpace(cur.String()))
				cur.Reset()
			}
			continue
		}
		cur.WriteByte(c)
	}
	if cur.Len() > 0 {
		out = append(out, strings.TrimSpace(cur.String()))
	}
	return out
}

// categoryWords is the reference's recovered singular/plural pair for each
// count category. Every pair below appears verbatim in the 1.3.6 string table
// ("1 file" / "%d files", "1 search" / "%d searches", …); the changelog records
// the fix — "Fixed counts in tool steps and the `/codesearch` panel, which now
// read \"Found 1 result\", \"1 match\", \"1 file\", and \"Wrote 1 line\" instead of
// \"1 results\"".
var categoryWords = map[string][2]string{
	"file":     {"file", "files"},
	"edit":     {"file", "files"},
	"search":   {"search", "searches"},
	"command":  {"command", "commands"},
	"tool":     {"tool", "tools"},
	"task":     {"task", "tasks"},
	"cron":     {"cron", "crons"},
	"page":     {"page", "pages"},
	"resource": {"resource", "resources"},
	"question": {"question", "questions"},
	"timer":    {"timer", "timers"},
	"image":    {"image", "images"},
	"step":     {"step", "steps"},
	"result":   {"result", "results"},
	// Cycle 228: /codesearch streams matches into a counter row
	// ("Found 3 matches in 1 file"). The pair rides alongside the
	// tool-step categories because the reference shares the same
	// singular/plural rules across both surfaces.
	"match": {"match", "matches"},
}

// toolCategories maps a recovered tool name onto its count category. The
// tool-name → category mapping is 【推测】 (the reference derives the category
// from its own step model); the vocabulary itself is not.
var toolCategories = map[string]string{
	"run_command":                "command",
	"command_status":             "command",
	"send_command_input":         "command",
	"view_file":                  "file",
	"read_file":                  "file",
	"write_to_file":              "edit",
	"write_file":                 "edit",
	"create_file":                "edit",
	"replace_file_content":       "edit",
	"edit_file":                  "edit",
	"multi_replace_file_content": "edit",
	"sed_file":                   "edit",
	"list_dir":                   "file",
	"notebook_edit":              "edit",
	"grep_search":                "search",
	"find_by_name":               "search",
	"codebase_search":            "search",
	"search_web":                 "search",
	"read_url_content":           "page",
	"view_content_chunk":         "page",
	"manage_task":                "task",
	"send_message":               "task",
	"wait":                       "task",
	"schedule":                   "cron",
	"list_resources":             "resource",
	"read_resource":              "resource",
	"ask_question":               "question",
	"generate_image":             "image",
}

// categoryOrder is the stable order categories appear in a mixed group header.
var categoryOrder = []string{
	"file", "edit", "search", "command", "page", "resource", "question",
	"timer", "image", "task", "cron", "tool", "step",
}

// toolCategory maps a tool name onto its count category.
func toolCategory(name string) string {
	if c, ok := toolCategories[name]; ok {
		return c
	}
	return "tool"
}

// countLabel renders "<n> <word>" using the reference's singular form when n is
// one ("1 file", never "1 files").
func countLabel(n int, word [2]string) string {
	if n == 1 {
		return "1 " + word[0]
	}
	return fmt.Sprintf("%d %s", n, word[1])
}

// groupSummary returns the three styled pieces of a batch header:
// "Ran" + "2 commands" + "(echo one · echo two)". Verified against 1.4.3.
func groupSummary(steps []toolStep) (verb, count, detail string) {
	cat := toolCategory(steps[0].name)
	// A mixed batch keeps the per-category counts (rare; the reference's
	// own model usually emits one category per message).
	mixed := false
	for _, s := range steps[1:] {
		if toolCategory(s.name) != cat {
			mixed = true
		}
	}
	if mixed {
		counts := map[string]int{}
		for _, s := range steps {
			counts[toolCategory(s.name)]++
		}
		parts := make([]string, 0, len(counts))
		for _, c := range categoryOrder {
			if counts[c] > 0 {
				parts = append(parts, countLabel(counts[c], categoryWords[c]))
			}
		}
		return "Ran", strings.Join(parts, ", "), ""
	}
	switch cat {
	case "command":
		verb = "Ran"
		count = countLabel(len(steps), categoryWords["command"])
		detail = "(" + strings.Join(stepArgs(steps, parenArg), " \u00b7 ") + ")"
	case "file":
		verb = "Explored"
		count = countLabel(len(steps), categoryWords["file"])
		detail = "(" + strings.Join(stepArgs(steps, fileNameArg), " \u00b7 ") + ")"
	case "edit":
		// A batch of writes/edits summarises as "Edited N files (a · b)"
		// — measured for a single create ("Edited 1 file (w285b.txt)");
		// the plural/multi-file shape follows the same renderer.
		verb = "Edited"
		count = countLabel(len(steps), categoryWords["edit"])
		detail = "(" + strings.Join(stepArgs(steps, fileNameArg), " \u00b7 ") + ")"
	case "search":
		// The reference folds searches into the Explore vocabulary
		// (measured: "● Explored 1 search (golang)").
		verb = "Explored"
		count = countLabel(len(steps), categoryWords["search"])
		detail = "(" + strings.Join(stepArgs(steps, searchArg), " \u00b7 ") + ")"
	default:
		verb = "Ran"
		count = countLabel(len(steps), categoryWords[cat])
	}
	return verb, count, detail
}

// stepArgs extracts one display argument per step using pick.
func stepArgs(steps []toolStep, pick func(string) string) []string {
	out := make([]string, 0, len(steps))
	for _, s := range steps {
		if a := pick(s.title); a != "" {
			out = append(out, a)
			continue
		}
		out = append(out, s.name)
	}
	return out
}

// searchArg is the search-batch display argument: the bare query.
func searchArg(title string) string {
	if p := parenArg(title); p != "" {
		return p
	}
	return ""
}

// fileNameArg is the file-batch display argument: the basename, matching the
// reference's "Explored 1 file (agy-cont.sh)".
func fileNameArg(title string) string {
	if p := parenArg(title); p != "" {
		return filepath.Base(p)
	}
	return ""
}

// styledGroupSummary styles a batch header. "Ran" is the bold-orange verb
// (cToolVerbOpen), the count is body-coloured, and the parenthesised list
// plus the hint are dim gray — the exact split measured on 1.4.3:
//
//	\x1b[38;2;181;137;0;1mRan\x1b[m \x1b[38;2;7;54;66m2 commands\x1b[m \x1b[38;2;88;110;117m(echo one · echo two)\x1b[m
func styledGroupSummary(steps []toolStep) string {
	verb, count, detail := groupSummary(steps)
	var b strings.Builder
	b.WriteString(cToolVerbOpen)
	b.WriteString(verb)
	b.WriteString(cToolVerbClose)
	if count != "" {
		b.WriteString(" ")
		b.WriteString(cBodyOpen)
		b.WriteString(count)
		b.WriteString(cBodyClose)
	}
	if detail != "" {
		b.WriteString(" ")
		b.WriteString(cToolArgsOpen)
		b.WriteString(detail)
		b.WriteString(cToolArgsClose)
	}
	return b.String()
}

// styledSummary styles a single step's collapsed form ("Ran (echo one)").
func styledSummary(s toolStep) string {
	text := s.summary
	if text == "" {
		text = s.title
	}
	if text == "" {
		text = s.name
	}
	return styleVerbArgs(text)
}

// styledCallTitle styles a single step's expanded form ("Bash(echo one)").
func styledCallTitle(s toolStep) string {
	text := s.title
	if text == "" {
		text = s.summary
	}
	if text == "" {
		text = s.name
	}
	return styleVerbArgs(text)
}

// styleVerbArgs colours the leading verb in bold orange and the rest in the
// dim arg palette. The space between them is kept only when the title has
// one: "Ran (cmd)" keeps it, "Bash(cmd)" does not (both measured on 1.4.3).
func styleVerbArgs(text string) string {
	verb, args, sep := splitVerbArgs(text)
	var b strings.Builder
	b.WriteString(cToolVerbOpen)
	b.WriteString(verb)
	b.WriteString(cToolVerbClose)
	if args != "" {
		b.WriteString(sep)
		b.WriteString(cToolArgsOpen)
		b.WriteString(args)
		b.WriteString(cToolArgsClose)
	}
	return b.String()
}

// splitVerbArgs splits a title into verb, args and the separator that sat
// between them. A "(" before the first space means the args are glued to the
// verb ("Bash(cmd)"), otherwise the separator is the single space.
func splitVerbArgs(title string) (verb, args, sep string) {
	space := strings.IndexByte(title, ' ')
	paren := strings.IndexByte(title, '(')
	if paren >= 0 && (space < 0 || paren < space) {
		return title[:paren], title[paren:], ""
	}
	if space >= 0 {
		return title[:space], title[space+1:], " "
	}
	return title, "", ""
}

// styledHint wraps the "(ctrl+o to expand)" hint the same way ref
// does — dim gray (256-colour 238) for the parens and the verb.
// The leading "(" and trailing ")" are also wrapped, so the whole
// hint reads as one highlighted parenthetical.
//
// Cycle 159: cToolHintOpen / Close are a separate palette from
// cToolArgsOpen / Close — the hint is slightly lighter than the
// args (238 vs 235) so the eye reads it as a UI cue, not as
// content detail.
func styledHint(hint string) string {
	return cToolHintOpen + "(" + hint + ")" + cToolHintClose
}

// thoughtTitle renders the completed-thought summary. The duration uses Go's
// Duration.String() shape ("3s", "1.5s"), which matches the reference's
// "Thought for %s" / "Thought for %.1fs" pair for the common cases.
func thoughtTitle(d time.Duration, tokens int) string {
	ds := humanDuration(d)
	if tokens > 0 {
		return fmt.Sprintf("Thought for %s, %d tokens", ds, tokens)
	}
	return "Thought for " + ds
}

// humanDuration renders a duration the way the reference does: whole seconds
// stay integral ("3s"), sub-second parts keep one decimal ("1.5s"), and
// anything under a second is shown in milliseconds.
func humanDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return d.Round(100 * time.Millisecond).String()
}

// thinkingLiveLine is the in-progress frame the reference shows in the
// conversation history while the model reasons. Recovered from the changelog:
// "Fixed in-progress thinking frames occasionally freezing permanently in the
// conversation history (for example showing `Thinking... (31s)` forever)".
func thinkingLiveLine(elapsed time.Duration) string {
	// Cycle 156: thought steps use the U+25B8 ▸ glyph in dim gray,
	// not the U+25CF ● tool glyph in teal. v1.3.6's live frame for
	// "still thinking" is the thought style — the cycle-127 clone
	// picked the tool bullet because it didn't distinguish.
	return fmt.Sprintf("%s Thinking... (%s)", thoughtBulletOpen, humanDuration(elapsed))
}

func hasDetail(s toolStep) bool {
	return strings.TrimSpace(stepDetailText(s)) != ""
}

// stepDetailText maps a tool result onto the text the reference's expanded
// step actually shows. The tool *result* and the transcript *display* are
// different strings in 1.4.3:
//
//	run_command result:  Created At: …\nCompleted At: …\n\nThe command
//	                     exited with code 0.\nOutput:\n<out>\n
//	run_command display: Command exited with code 0.\n\nCommand output:\n<out>
//	                     (empty output: "No output from the command.")
//	view_file result:    <file bytes>
//	view_file display:   Read 3 lines
//
// Verified against agy 1.4.3 with `echo one`, `true` and `false`
// (docs/cycles/cycle-285-tool-display/deliverable.md). Anything we have not
// captured (write/replace/search) keeps the raw result.
func stepDetailText(s toolStep) string {
	if s.kind == stepKindThought {
		return s.detail
	}
	switch s.name {
	case "run_command":
		if text, ok := commandDetailText(s.detail); ok {
			return text
		}
	case "view_file", "read_file":
		return readDetailText(s.detail)
	}
	return s.detail
}

// commandDetailText reformats a run_command result body into the transcript
// display form. ok is false when the body carries no exit-code line (a
// tool-level failure or a timeout note), in which case the caller keeps the
// raw text.
func commandDetailText(result string) (string, bool) {
	code, ok := commandExitCode(result)
	if !ok {
		return "", false
	}
	out := ""
	if i := strings.Index(result, "Output:"); i >= 0 {
		out = strings.TrimPrefix(result[i+len("Output:"):], "\n")
	}
	out = strings.TrimRight(out, "\n")
	var b strings.Builder
	fmt.Fprintf(&b, "Command exited with code %d.", code)
	if strings.TrimSpace(out) == "" {
		b.WriteString("\n\nNo output from the command.")
	} else {
		b.WriteString("\n\nCommand output:\n")
		b.WriteString(out)
	}
	return b.String(), true
}

// readDetailText is the view_file display body: the reference shows the
// line count, not the file contents ("Read 3 lines").
func readDetailText(result string) string {
	// Cycle 293: the tool result now starts with the reference's timestamp
	// header and a "Showing lines A-B of N" line, so counting newlines would
	// count the header and the line-number prefixes too. The range's width is
	// the number of lines the call read.
	if i := strings.Index(result, "Showing lines "); i >= 0 {
		var first, last, total int
		if _, err := fmt.Sscanf(result[i:], "Showing lines %d-%d of %d", &first, &last, &total); err == nil {
			if n := last - first + 1; n >= 2 {
				return "Read " + countLabel(n, [2]string{"line", "lines"})
			}
			return ""
		}
	}
	// view_file (the custom-agent tool) reports the same range as
	// "Showing lines A to B" and a File Path/Total Lines header.
	if i := strings.Index(result, "Showing lines "); i >= 0 {
		if j := strings.Index(result[i:], " to "); j >= 0 {
			var first, last int
			if _, err := fmt.Sscanf(result[i:], "Showing lines %d", &first); err == nil {
				if _, err := fmt.Sscanf(result[i+j:], " to %d", &last); err == nil {
					if n := last - first + 1; n >= 2 {
						return "Read " + countLabel(n, [2]string{"line", "lines"})
					}
					return ""
				}
			}
		}
	}
	// A byte-capped read reports its range in the partial-read note instead
	// ("Partial read (lines 1-100 of 100). Showing bytes 0-46080 of …"), and
	// the body is a raw 46080-byte slice, so counting newlines is wrong.
	if i := strings.Index(result, "Partial read (lines "); i >= 0 {
		var first, last, total int
		if _, err := fmt.Sscanf(result[i:], "Partial read (lines %d-%d of %d)", &first, &last, &total); err == nil {
			if n := last - first + 1; n >= 2 {
				return "Read " + countLabel(n, [2]string{"line", "lines"})
			}
			return ""
		}
	}
	if strings.Contains(result, "File is empty") {
		return ""
	}
	trimmed := strings.TrimRight(result, "\n")
	n := 0
	if strings.TrimSpace(trimmed) != "" {
		n = strings.Count(trimmed, "\n") + 1
	}
	if n < 2 {
		// 1.4.3 prints no "⎿" row for a single-line read; the hint moves
		// onto the call head (measured: "● Read(/tmp/w285d.txt)
		// (ctrl+o to collapse)"). The same row is skipped for an empty
		// read.
		return ""
	}
	return "Read " + countLabel(n, [2]string{"line", "lines"})
}

// stepDetailLines renders the expanded output of one step, capping at
// maxStepDetailLines and appending the recovered "<output +%d lines>" marker.
//
// Cycle 127: a detail line whose first non-space byte is `+` or `-`
// is treated as a unified-diff line and its first column is wrapped
// in the corresponding add/remove colour. This is the
// `replace_file_content` / `multi_replace_file_content` tool-result
// surface; the reference uses Chroma with the Aurora-Dark style
// (recovery: `strings ~/.local/bin/agy | grep chroma` confirmed the
// Chroma init function; the green and red hex tokens from the same
// `strings` dump are recoverable as `\x1b[38;2;85;170;34m` for adds
// and `\x1b[38;2;255;99;111m` for removes). The clone stays
// monochrome for non-diff lines.
//
// Cycle 176: when the detail body looks like Markdown (heuristic in
// looksLikeMarkdown) the content is rendered through
// internal/mdrender before the per-line diff colouring pass runs.
// This means a `cat README.md` tool result gets bold / italic /
// bullet / code-fence styling in the transcript instead of the raw
// `**` / `*` / ` ``` ` / `# ` markers.
//
// Cycle 178: the per-line diff colouring pass is now itself gated on
// looksLikeDiff. Before cycle 178 a `- pattern` line in a
// `cat .gitignore` payload got coloured red as a spurious diff
// line — the cycle-127 alphabet collided with Markdown's `- `
// bullet. With cycle 178 the diff colouring only runs when the
// whole body actually looks like a unified diff (hunk markers or
// both file headers present), so cycle-176 can safely widen the
// Markdown heuristic to include `- ` and `* ` at line start.
func stepDetailLines(s toolStep, width int) []string {
	if s.diff != nil {
		return s.diff.lines(width)
	}
	detail := strings.TrimRight(normalizeDetailNewlines(stepDetailText(s)), "\n")
	if strings.TrimSpace(detail) == "" {
		return nil
	}
	body := detail
	diffMode := looksLikeDiff(detail)
	markdownMode := !diffMode && looksLikeMarkdown(detail)
	if markdownMode {
		// Cycle glamour-migration: route through
		// charm.land/glamour/v2 v2.0.1 (the version AGY 1.4.2
		// ships). The four-step pipeline is:
		//
		//  1. Pre-process file-link URLs through
		//     PreprocessFileLinks so `#L<n>` anchors become
		//     `%23L<n>` before glamour parses the source.
		//  2. Strip fenced mermaid / math blocks into
		//     placeholders so the legacy MermaidRenderBlock /
		//     expandLaTeX can render them with the cycle's
		//     existing frontmatter-aware logic, and glamour
		//     does not mangle the body.
		//  3. Render the placeholder-laden source through
		//     glamour.
		//  4. Restore the placeholders into glamour's output.
		//
		// The glamour call falls back to the legacy walker on
		// any parser error (cycle 1.4.2 "fallback to raw text
		// with warning banner" spirit).
		pre := mdrender.PreprocessFileLinks(detail)
		stripped, placeholders := mdrender.StripMermaidAndMath(pre)
		// Cycle 268: thread the TUI's terminal width through to
		// glamour. Before this, the chat transcript and step-detail
		// panel capped word-wrap at glamour's hard default of 80
		// columns regardless of how wide the tmux pane was —
		// verified by a 250-col PTY observation. width is the
		// runMode's m.width pinned on tea.WindowSizeMsg. When width
		// is 0 (the very first View() before any resize message),
		// fall back to mdrender.CurrentWidth() which reads $COLUMNS
		// / x/term so the line lands at a usable width even on the
		// first paint.
		toolWidth := width
		if toolWidth <= 0 {
			toolWidth = mdrender.CurrentWidth()
		}
		rendered, err := mdrender.ANSI(stripped, toolWidth)
		if err != nil || rendered == "" {
			var buf bytes.Buffer
			if e := mdrender.RenderANSI(detail, &buf); e == nil {
				rendered = buf.String()
			}
		} else {
			rendered = mdrender.RestoreMermaidAndMath(rendered, placeholders)
		}
		if rendered != "" {
			body = strings.TrimRight(rendered, "\n")
		}
		// On mdrender error we leave body = detail and skip the
		// diff colouring pass too (markdownMode is true so diffMode
		// is false anyway).
	}
	lines := strings.Split(body, "\n")
	extra := 0
	if len(lines) > maxStepDetailLines {
		extra = len(lines) - maxStepDetailLines
		lines = lines[:maxStepDetailLines]
	}
	out := make([]string, 0, len(lines)+1)
	for i, l := range lines {
		// Cycle 178: styleDiffLine only runs in diff mode. The
		// helper itself still colours any leading +/- (cycle 127
		// preserved verbatim) — the gating happens here so that
		// non-diff content (Markdown bullets, .gitignore patterns,
		// shell command output) does not get spuriously coloured.
		var styled string
		if diffMode {
			styled = styleDiffLine(l)
		} else {
			styled = l
		}
		if i == 0 {
			// Cycle 178: the reference renders the first detail
			// line with two trailing spaces after the bullet ("  ⎿
			//  content"), so the visual column of the content sits
			// at column 6 not 5.
			out = append(out, detailBulletOpen+styled)
		} else {
			// Cycle 178: continuation lines are indented 12 spaces
			// in the reference (2 deeper than the first line), so
			// the content visually nests below the "⎿" rather than
			// the content after it.
			out = append(out, "            "+styled)
		}
	}
	if extra > 0 {
		out = append(out, fmt.Sprintf("            <output +%d lines>", extra))
	}
	return out
}

// normalizeDetailNewlines makes a tool-result body safe to split into
// terminal rows.
//
// Cycle 278: `run_command` results are CRLF-terminated on purpose — the
// reference's Output text is `stripANSI` plus a naive LF→CRLF replace
// (cycle 277), and that same string is what the model receives, so it
// must not change here. But it is not safe to *display*: stepDetailLines
// splits on "\n", which leaves a bare "\r" at the end of every row, and
// ultraviolet treats a bare CR as "move to column 0". The viewport pads
// each row *after* that CR, so the padding repaints the line from column
// 0 and the expanded output renders blank. That is why the detail only
// looked broken after ctrl+o — while collapsed the body is not drawn.
//
// This mirrors internal/convstore.cleanToolResult (cycle 178), which was
// the same fix on the resumed-conversation path. Display-only: never
// write the normalised string back into s.detail / m.response / m.session.
func normalizeDetailNewlines(s string) string {
	if !strings.ContainsRune(s, '\r') {
		return s
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "")
}

// looksLikeDiff returns true if s contains markers that identify
// the body as a unified-diff payload. Cycle 178 uses this in
// stepDetailLines to gate the per-line diff colouring pass —
// before cycle 178 a `- pattern` line in a non-diff payload
// (cat .gitignore, a YAML list, shell command output) was
// spuriously coloured red.
//
// The check is purely structural: any of:
//
//   - "@@" hunk marker on its own line (the canonical
//     unified-diff body delimiter), at line start or after a
//     newline
//   - both "--- " and "+++ " file headers (the canonical
//     unified-diff file delimiter pair) anywhere in the body
//
// fires it. Each alone is not enough — a markdown file might
// contain a "---" thematic break and a shell transcript might
// contain a "+++" arithmetic fragment, but the combination of
// both is unambiguous in practice. "@@" alone is unambiguous.
func looksLikeDiff(s string) bool {
	if strings.HasPrefix(s, "@@") || strings.Contains(s, "\n@@") {
		return true
	}
	hasMinus := strings.Contains(s, "--- ") || strings.HasPrefix(s, "--- ")
	hasPlus := strings.Contains(s, "+++ ") || strings.HasPrefix(s, "+++ ")
	return hasMinus && hasPlus
}

// looksLikeMarkdown returns true if s contains Markdown markers that
// goldmark would recognise on parse. Cycle 178 introduced this in
// stepDetailLines; cycle 178 widens it.
//
// Cycle 178: the heuristic now also fires on `- ` and `* ` at line
// start (the standard Markdown bullet markers). Cycle 178 had to
// exclude those because they collided with the cycle-127 diff-style
// alphabet. Cycle 178 tightens the diff side (looksLikeDiff now
// requires hunk markers or both file headers) so the Markdown side
// can safely widen. A bullet-only Markdown payload like `cat
// .gitignore` (the original cycle-176 regression case) still does
// NOT trip the heuristic — it has only `- ` lines, no other markers
// — so its `- pattern` rows are now rendered verbatim (no
// bullet-conversion, no spurious red colouring), which is the
// correct output for a `.gitignore` payload.
//
// The six markers checked are:
//
//   - "**"               paired-asterisk strong (most Markdown
//     prose has at least one bold run)
//   - "```"              fenced code block delimiter (readme /
//     changelog files almost always have one)
//   - "# " at line start ATX heading
//   - "]("               inline / reference link
//   - "- " at line start unordered list item (cycle 178 addition)
//   - "* " at line start unordered list item (cycle 178 addition)
//
// The check is purely lexical — it does not parse the content with
// goldmark first. A pathological input that contains `**` as
// literal text (e.g. someone running `printf 'a**b' | cat`) would
// be mis-rendered; in practice such content never appears in tool
// output.
func looksLikeMarkdown(s string) bool {
	if strings.Contains(s, "**") {
		return true
	}
	if strings.Contains(s, "```") {
		return true
	}
	if strings.HasPrefix(s, "# ") || strings.Contains(s, "\n# ") {
		return true
	}
	if strings.Contains(s, "](") {
		return true
	}
	// Cycle 178: `- ` and `* ` at line start are now Markdown
	// bullet markers (cycle 178 had to exclude them because the
	// diff side was over-broad). The diff side is now tightened
	// (looksLikeDiff requires `@@` or both `--- ` / `+++ `), so a
	// payload that genuinely is bulleted Markdown (or .gitignore,
	// which has `- pattern` rows) renders correctly: bullet rows
	// become `␡ ` round dots, never get the spurious red colour.
	if strings.HasPrefix(s, "- ") || strings.Contains(s, "\n- ") {
		return true
	}
	if strings.HasPrefix(s, "* ") || strings.Contains(s, "\n* ") {
		return true
	}
	return false
}

// styleDiffLine returns line with its leading `+`/`-` column wrapped
// in the recovered diff add/remove colour, when applicable. Lines
// without a leading diff marker (and lines that exceed the maximum
// styling width to keep the byte budget bounded) pass through
// unchanged. Cycle 127 limits the styled column to the first 200 bytes
// of the line to avoid runaway byte growth on long diffs.
//
// Cycle 178: the cycle-127 heuristic was over-broad — a run_command
// stdout that starts with "---deleted---" or "---git status---"
// would also be styled as a diff and lose the leading dash to the
// red colour. The recovered reference rendering treats "---"/"+++"
// (more than one marker at column 0) as section separators, not as
// diff content, and only styles single-marker lines.
func styleDiffLine(line string) string {
	// Strip a single leading horizontal-tab or space block to find the
	// diff marker — replace_file_content usually emits the marker at
	// column 0, but multi_replace_file_content sometimes pads with a
	// tab. Anything past that is content; only the marker is coloured.
	trimmed := line
	j := 0
	for j < len(trimmed) && (trimmed[j] == ' ' || trimmed[j] == '\t') {
		j++
	}
	if j >= len(trimmed) {
		return line
	}
	marker := trimmed[j]
	// Skip "---" / "+++" / "++++" / "----" — these are diff file
	// headers or section separators, not content rows. The reference
	// leaves them in the default palette.
	if j+1 < len(trimmed) && trimmed[j+1] == marker {
		return line
	}
	switch marker {
	case '+':
		if len(trimmed) > j+1+200 {
			return line
		}
		return trimmed[:j+1] + cToolAddOpen + trimmed[j+1:] + cToolAddClose
	case '-':
		if len(trimmed) > j+1+200 {
			return line
		}
		return trimmed[:j+1] + cToolRemoveOpen + trimmed[j+1:] + cToolRemoveClose
	}
	return line
}

// toolStepLines renders a single step in isolation (used for the live,
// in-flight transcript before the turn finalises into a block).
func toolStepLines(s toolStep, expanded bool, width int) []string {
	return toolBlockLines([]toolStep{s}, expanded, width)
}

// tuiToolRunner decorates an agent.ToolRunner so the TUI can render a step as
// the call starts (spinner hint) and finishes (transcript step).
type tuiToolRunner struct {
	inner   agent.ToolRunner
	onStart func(backend.ToolCall)
	onEnd   func(backend.ToolCall, string, error)
}

func (r *tuiToolRunner) Run(ctx context.Context, call backend.ToolCall) (string, error) {
	if r.onStart != nil {
		r.onStart(call)
	}
	res, err := r.inner.Run(ctx, call)
	if err == nil {
		budget := tools.ApplyOutputBudget(res, "", tools.DefaultMaxResultTokens)
		res = budget.Content
	}
	if r.onEnd != nil {
		r.onEnd(call, res, err)
	}
	return res, err
}

// --- tea messages -----------------------------------------------------------

// turnDoneMsg terminates one agent turn. It replaces the cycle-50
// backendChunkMsg for the loop-driven path: it carries the reply text plus the
// tool steps the turn produced, so a headless (no *tea.Program) test can drive
// the model directly.
//
// reasoning is the model's thinking-block content the agent.Sanitizer
// stripped out of the visible stream; cycle stall-empty-step-cleanup
// adds it so the receiver can classify the turn through
// agent.IsEmptyAssistantTurn and skip recording a turn that produced
// only thinking with no follow-up text or tool call. The string is
// already trimmed by streamPrompt (sanitizer.Finish eats the trailing
// space the chunk-boundary stripping leaves behind), so the receiver
// passes it through verbatim.
type turnDoneMsg struct {
	reply     string
	steps     []toolStep
	reasoning string
	// err is the turn's terminal failure, if any: what agent.Loop.Send
	// returned when the turn did NOT complete (stream error, transport
	// failure, auto-continue give-up, iteration cap). Cycle 279: it used to
	// be discarded at the call site, which is why a failed turn rendered as
	// a tool block with no reply and no explanation — the reported "it just
	// stops and prints nothing" symptom. A non-nil err always produces a
	// visible transcript row even when reply is empty.
	err error
}

// toolRunningMsg paints the provisional transcript row for a call that is
// in flight and sets the spinner hint. Verified against agy 1.4.3:
//
//	● Running (sleep 8; echo hi) (ctrl+o to expand)
//	⢿  Running command...
//
// The row is replaced in place when toolStepMsg lands.
type toolRunningMsg struct{ step toolStep }

// toolStepMsg appends a finished step to the live transcript.
type toolStepMsg struct{ step toolStep }

// thoughtStartMsg marks the moment the model entered its thinking block, so the
// conversation view can render the live "⏺ Thinking... (Ns)" frame until the
// turn finalises into a "Thought for Ns" step. Sent at most once per turn.
type thoughtStartMsg struct{}
