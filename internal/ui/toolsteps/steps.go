// Package toolsteps is the transcript's tool-step model.
//
// A turn's transcript is a list of *steps*: a tool call the model
// made, a thought it had, and — once the turn ends — the blocks those
// steps were painted as. This package owns that vocabulary:
//
//   - steps.go — the Step value type, its Kind, the tea messages the
//     turn pipeline emits, and the two constructors.
//   - render.go — the rendering: the collapsed summary, the expanded
//     detail rows, the group headers and the live "Thinking…" frame.
//   - error_row.go — the row a failed turn paints.
//   - results.go — the raw tool outputs a set of steps carries.
//
// The package is **presentation only**. It knows how a step looks; it
// does not know what a step means, who ran it, or where it was
// painted. The rules that decide *what* a step says live in
// internal/ui/tuitools — this package is the styled layer over them,
// and it holds the palette escapes the styled layer needs.
//
// The palette's colours follow the selected colour scheme, so the
// bullets this package paints are refreshed whenever the scheme
// changes (palette.OnSchemeChange).
//
// Stdlib-only plus internal/ui/tuitools, internal/ui/tuilib and the
// palette. No dependency on the TUI host: the host owns the
// transcript's *bookkeeping* (where a block lives in the response
// buffer) and this package owns the step's *content*.
package toolsteps

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/tuitools"
)

// Kind distinguishes a tool step from a thought step in the transcript.
// The reference's conversation view treats model reasoning as a first-class
// step: the recovered symbol table has coalesceThinkingSteps,
// canMergeConsecutiveThoughts and canUpgradeTrailingThoughtToToolGroup in
// cli/store, and cli/render has collapsedThinkingSnippet / thinkingLiveSnippet /
// summarizeThinkingSteps. Both kinds share the same group/expand machinery.
type Kind int

const (
	// KindTool is a step that calls a tool.
	KindTool Kind = iota
	// KindThought is a step that carries the model's reasoning.
	KindThought
)

// Step is one tool call (or thought) rendered in the conversation
// transcript.
type Step struct {
	// Kind is the step's kind — a tool call or a thought.
	Kind Kind
	// Name is the tool's name, e.g. "run_command".
	Name string
	// Title is the expanded call form without styling, e.g.
	// "Bash(echo one)" or "Read(/tmp/x)" — the shape the reference prints
	// once the step is open.
	Title string
	// Summary is the collapsed form, e.g. "Ran (echo one)" or
	// "Explored 1 file (agy-cont.sh)". Empty falls back to Title.
	Summary string
	// Detail is the step's body — the tool's output, or the reasoning.
	Detail string
	// Err marks a step that failed.
	Err bool
	// Running marks the provisional row painted while the call executes
	// ("Running (echo one)"). It is replaced in place when the call ends.
	Running bool
	// Diff is the rendered unified hunk for an edit_file step (nil for
	// every other step). See tuitools.EditDiff.
	Diff *tuitools.EditDiff
	// Result is the raw tool output, before the renderer rewrites detail
	// (write/edit steps replace it with a line count / hunk). /context's
	// "Tool calls" row counts this text, which is what the wire carries in
	// functionResponse.response.output (cycle 292).
	Result string
	// Args is the raw JSON the model produced for the call, e.g.
	// {"CommandLine":"ls"}. The trajectory stores it verbatim, so a
	// resumed conversation can rebuild the state the call produced
	// (see internal/app/session/rehydration).
	Args string
	// Batch groups calls emitted in the same assistant message; the
	// reference renders each batch as one block and keeps the assistant
	// text between batches. 0 is the default (single batch).
	Batch int
	// Tokens is the gateway's reasoning-token count
	// (usage.completion_tokens_details.reasoning_tokens).
	Tokens int
	// Dur is how long the model spent inside the think block.
	Dur time.Duration
}

// TurnDoneMsg terminates one agent turn. It replaces the cycle-50
// backendChunkMsg for the loop-driven path: it carries the reply text plus the
// tool steps the turn produced, so a headless (no *tea.Program) test can drive
// the model directly.
//
// reasoning is the model's thinking-block content the agent.Sanitizer
// stripped out of the visible stream; cycle stall-empty-step-cleanup
// adds it so the receiver can classify the turn through
// agentturn.IsEmptyAssistantTurn and skip recording a turn that produced
// only thinking with no follow-up text or tool call. The string is
// already trimmed by streamPrompt (sanitizer.Finish eats the trailing
// space the chunk-boundary stripping leaves behind), so the receiver
// passes it through verbatim.
type TurnDoneMsg struct {
	// Reply is the turn's visible text.
	Reply string
	// Steps are the tool steps the turn produced.
	Steps []Step
	// Reasoning is the model's stripped thinking-block content.
	Reasoning string
	// Err is the turn's terminal failure, if any: what agent.Loop.Send
	// returned when the turn did NOT complete (stream error, transport
	// failure, auto-continue give-up, iteration cap). Cycle 279: it used to
	// be discarded at the call site, which is why a failed turn rendered as
	// a tool block with no reply and no explanation — the reported "it just
	// stops and prints nothing" symptom. A non-nil Err always produces a
	// visible transcript row even when Reply is empty.
	Err error
	// TokenMetrics records turn token usage for low-verbosity rendering.
	TokenMetrics TokenMetrics
}

// Verbosity controls how much detail is shown for tool calls, commands,
// and thoughts in the conversation transcript.
//
// agy 1.4.11 / 1.4.12:
//   - "medium" (default): groups related tool calls and thoughts into
//     concise summaries while keeping commands and responses visible.
//   - "low": collapses tool calls, commands, and thoughts into a token
//     metrics table ("%s in | %s out | %s thinking").
//   - "high": shows every tool call, command, and thought in full.
type Verbosity int

const (
	VerbosityMedium Verbosity = iota
	VerbosityLow
	VerbosityHigh
)

// ParseVerbosity parses a verbosity string from settings.json.
func ParseVerbosity(s string) Verbosity {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "low":
		return VerbosityLow
	case "high":
		return VerbosityHigh
	default:
		return VerbosityMedium
	}
}

func (v Verbosity) String() string {
	switch v {
	case VerbosityLow:
		return "low"
	case VerbosityHigh:
		return "high"
	default:
		return "medium"
	}
}

// TokenMetrics records turn token usage for low-verbosity rendering.
type TokenMetrics struct {
	Input     int
	Output    int
	Reasoning int
}

// HasTokens reports whether any token metric was recorded.
func (m TokenMetrics) HasTokens() bool {
	return m.Input > 0 || m.Output > 0 || m.Reasoning > 0
}

// Summary returns the reference's token metrics summary row:
// "%s in | %s out | %s thinking"
func (m TokenMetrics) Summary() string {
	return fmt.Sprintf("%s in | %s out | %s thinking",
		FormatTokens(m.Input),
		FormatTokens(m.Output),
		FormatTokens(m.Reasoning),
	)
}

// FormatTokens formats token counts the way the reference renders them:
//   - < 1,000: verbatim decimal (e.g. "450")
//   - 1,000 - 999,999: single-digit decimal "k" (e.g. "12.3k")
//   - >= 1,000,000: single-digit decimal "M" (e.g. "1.2M")
func FormatTokens(n int) string {
	if n < 0 {
		n = 0
	}
	if n < 1000 {
		return strconv.Itoa(n)
	}
	if n < 1_000_000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000.0)
	}
	return fmt.Sprintf("%.1fM", float64(n)/1_000_000.0)
}

// ToolRunningMsg paints the provisional transcript row for a call that is
// in flight and sets the spinner hint. Verified against agy 1.4.3:
//
//	● Running (sleep 8; echo hi) (ctrl+o to expand)
//	⢿  Running command...
//
// The row is replaced in place when ToolStepMsg lands.
type ToolRunningMsg struct{ Step Step }

// ToolStepMsg appends a finished step to the live transcript.
type ToolStepMsg struct{ Step Step }

// ThoughtStartMsg marks the moment the model entered its thinking block, so the
// conversation view can render the live thinking frame until the
// turn finalises into a "Thought for Ns" step. Sent at most once per turn.
type ThoughtStartMsg struct{}

// ThoughtChunkMsg delivers streaming reasoning text as it arrives from the model.
type ThoughtChunkMsg struct {
	Delta string
}

// fileLinePair is the singular/plural pair the write step's "Wrote N
// lines" summary uses.
var fileLinePair = [2]string{"line", "lines"}

// NewRunningStep builds the provisional row painted while a tool call
// executes.
func NewRunningStep(name string, args json.RawMessage) Step {
	return Step{
		Kind:    KindTool,
		Name:    name,
		Title:   tuitools.ToolStepTitle(name, args),
		Summary: tuitools.RunningSummary(name, args),
		Running: true,
	}
}

// NewToolStep builds the finished row for a tool call.
func NewToolStep(name string, args json.RawMessage, result string, toolErr bool) Step {
	s := Step{
		Kind:    KindTool,
		Name:    name,
		Title:   tuitools.ToolStepTitle(name, args),
		Summary: tuitools.ToolStepSummary(name, args),
		Detail:  result,
		Result:  result,
		Args:    string(args),
		Err:     toolErr,
	}
	if name == "run_command" {
		if code, ok := tuitools.CommandExitCode(result); ok && code != 0 {
			s.Err = true
		}
	}
	if tuitools.IsWriteTool(name) {
		if n := tuilib.FileLineCount(tuitools.ArgStr(args, "content", "Content")); n > 0 {
			s.Detail = "Wrote " + tuitools.CountLabel(n, fileLinePair)
		}
		if !toolErr {
			s.Diff = tuitools.CreateDiffFor(args)
		}
	}
	if tuitools.IsEditTool(name) && !toolErr {
		s.Diff = tuitools.EditDiffFor(args)
	}
	return s
}
