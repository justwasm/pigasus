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
package cliapp

import (
	"context"
	"encoding/json"
	"time"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/tools"
	"github.com/justwasm/pigasus/internal/tuilib"
	"github.com/justwasm/pigasus/internal/tuitools"
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
const maxStepDetailLines = tuitools.MaxDetailLines

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
	// args is the raw JSON the model produced for the call, e.g.
	// {"CommandLine":"ls"}. The trajectory stores it verbatim, so a
	// resumed conversation can rebuild the state the call produced
	// (see internal/rehydration).
	args string
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
	// bash is set for a bash-mode block (cycle bash-mode-preempt): its rows
	// are re-rendered from the full body so ctrl+o can expand/collapse the
	// truncated tail, the same way tool blocks do.
	bash *bashBlock
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

// editDiff is the transcript body of an edit/create step. It is the
// internal/tuitools model aliased under the clone's own name, so the
// transcript renderer and the tests read the same fields the diff
// builder writes.
type editDiff = tuitools.EditDiff

// editDiffRow is one row in a diff hunk.
type editDiffRow = tuitools.EditDiffRow

// editDiffFor recovers old and new file text and builds the hunk.
func editDiffFor(args json.RawMessage) *editDiff {
	return tuitools.EditDiffFor(args)
}

// createDiffFor builds the addition-only diff for a file creation tool.
func createDiffFor(args json.RawMessage) *editDiff {
	return tuitools.CreateDiffFor(args)
}

// buildEditDiff trims common prefix/suffix and keeps three context lines
// around change.
func buildEditDiff(oldText, newText string) *editDiff {
	return tuitools.BuildEditDiff(oldText, newText)
}

// newRunningStep builds the provisional row painted while a tool call
// executes.
func newRunningStep(name string, args json.RawMessage) toolStep {
	return toolStep{
		kind:    stepKindTool,
		name:    name,
		title:   tuitools.ToolStepTitle(name, args),
		summary: tuitools.RunningSummary(name, args),
		running: true,
	}
}

// newToolStep builds the finished row for a tool call.
func newToolStep(name string, args json.RawMessage, result string, toolErr bool) toolStep {
	s := toolStep{
		kind:    stepKindTool,
		name:    name,
		title:   tuitools.ToolStepTitle(name, args),
		summary: tuitools.ToolStepSummary(name, args),
		detail:  result,
		result:  result,
		args:    string(args),
		err:     toolErr,
	}
	if name == "run_command" {
		if code, ok := tuitools.CommandExitCode(result); ok && code != 0 {
			s.err = true
		}
	}
	if tuitools.IsWriteTool(name) {
		if n := tuilib.FileLineCount(tuitools.ArgStr(args, "content", "Content")); n > 0 {
			s.detail = "Wrote " + countLabel(n, fileLinePair)
		}
		if !toolErr {
			s.diff = createDiffFor(args)
		}
	}
	if tuitools.IsEditTool(name) && !toolErr {
		s.diff = editDiffFor(args)
	}
	return s
}

var fileLinePair = [2]string{"line", "lines"}
