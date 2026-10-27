// tools.go — the harness's tool surface.
//
// # Why the tools are wrapped
//
// The reference's harness has its own tools, and they report their results
// in their own shapes (see internal/harnesstools): a command prints the
// reference's step string, an edit prints a diff, a search answers with the
// files it matched rather than with text at all.
//
// The clone runs the CLI's executors — the same code, the same sandbox, the
// same permission policy — and this file wraps them so their results come
// back in the harness's shapes.
//
// # The structured answer
//
// The wrapper is also where the reference's structured answers travel: a
// search's answer rides on the tool result's details, which the bridge
// carries into the request body (internal/piagentbridge) and the transport
// sends as it is (internal/backend).
//
// # The outcome store
//
// The frame that closes a tool's step carries what the tool produced — the
// exit code of a command, the diff of an edit, the listing of a directory —
// and that frame is written after the tool ran. The wrapper leaves the
// outcome in the store and the frame writer picks it up.
package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/harnesstools"
)

// harnessTools wraps the harness-side tools so their results come back in
// the reference's shapes.
//
// The client's own tools are not wrapped: the client prints them, and the
// harness has nothing to re-render.
func harnessTools(inner []piagent.AgentTool, store *outcomeStore, truncateTokens int) []piagent.AgentTool {
	if len(inner) == 0 {
		return nil
	}
	out := make([]piagent.AgentTool, 0, len(inner))
	for _, tool := range inner {
		if tool == nil {
			continue
		}
		out = append(out, &harnessTool{inner: tool, store: store, truncateTokens: truncateTokens})
	}
	return out
}

// harnessTool re-renders one tool's result the way the reference's harness
// renders it.
type harnessTool struct {
	inner piagent.AgentTool
	store *outcomeStore
	// truncateTokens is the client's tool-output truncation budget, in
	// tokens. Zero leaves the output whole.
	truncateTokens int
}

func (t *harnessTool) Name() string { return t.inner.Name() }

func (t *harnessTool) Label() string { return t.inner.Label() }

func (t *harnessTool) Schema() pi.ToolSchema { return t.inner.Schema() }

// ArgumentSchema forwards the inner tool's runtime schema.
//
// PiG validates a call against this schema when the tool carries one, and
// the harness's surface is the lenient one: the reference's harness runs a
// call that misses a required field, so the runtime check drops the
// `required` list while the advertised schema stays as it is.
//
// A tool that carries no runtime schema keeps the one its own declaration
// has — that is what PiG would have validated against.
func (t *harnessTool) ArgumentSchema() json.RawMessage {
	if provider, ok := t.inner.(interface{ ArgumentSchema() json.RawMessage }); ok {
		return provider.ArgumentSchema()
	}
	params := t.inner.Schema().Parameters
	if params == nil {
		return nil
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return nil
	}
	return raw
}

// PrepareArguments forwards the inner tool's argument normalisation.
//
// The reference's edit tool normalises its arguments before the schema
// check runs; the clone's tools do the same, and the wrapper must not
// stand between them and the check.
func (t *harnessTool) PrepareArguments(raw json.RawMessage) (json.RawMessage, error) {
	if preparer, ok := t.inner.(piagent.ArgumentPreparer); ok {
		return preparer.PrepareArguments(raw)
	}
	return raw, nil
}

func (t *harnessTool) ExecutionMode() piagent.ToolExecutionMode {
	return t.inner.ExecutionMode()
}

// Execute runs the tool and re-renders its result.
//
// The edit tools need the file's contents before and after the call to
// compute the diff the reference reports, so the wrapper reads the file on
// both sides of the call.
func (t *harnessTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, onUpdate piagent.ToolUpdateCallback) (piagent.AgentToolResult, error) {
	started := time.Now()
	before := readTargetFile(args)
	if err := t.checkWorkingDirectory(args); err != nil {
		return t.failed(args, piagent.AgentToolResult{}, err, started), nil
	}
	args = t.writeArgs(args, before)
	result, err := t.inner.Execute(ctx, toolCallID, args, onUpdate)
	if err != nil {
		return t.failed(args, result, err, started), nil
	}
	finished := time.Now()

	outcome := t.render(args, result, started, finished, before)
	t.store.put(t.inner.Name(), outcome)
	if outcome.Text != "" {
		result.Content = []pi.ToolResultMessageContent{pi.TextContent{Text: outcome.Text}}
	}
	if outcome.Response != nil {
		result.Details = outcome.Response
	}
	if t.inner.Name() == "finish" {
		// The `finish` call ends the interaction: the reference does not
		// ask the model again after it, and no second request reaches the
		// endpoint. The runtime stops after the batch when every result
		// asks it to.
		result.Terminate = true
	}
	return result, nil
}

// checkWorkingDirectory fails a command whose working directory is gone.
//
// The reference's harness runs the command in the directory the model named
// and fails the step when there is none. The CLI's own runner keeps the
// workspace instead — a stale value must not brick the call — so the
// harness checks the directory before the call reaches it.
//
// Only an absolute directory is checked: the harness's reference takes the
// working directory as it is, and a relative one is the reference's own
// business (it refuses such a call, which the clone does not yet).
func (t *harnessTool) checkWorkingDirectory(args json.RawMessage) error {
	if t.inner.Name() != harnesstools.ToolRunCommand {
		return nil
	}
	dir := strings.TrimSpace(argString(args, "Cwd", "cwd"))
	if dir == "" || !filepath.IsAbs(dir) {
		return nil
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return harnesstools.MissingDirectoryError(dir)
	}
	return nil
}

// failed renders a call that failed.
//
// The reference reports a failed call as a failed *step*: the model sees
// the step's text, and the frame carries the tool's own sentence. The
// runtime is told the call failed, but it is handed the step's text as
// the result — a Go error would reach the model as the error's own
// words.
func (t *harnessTool) failed(args json.RawMessage, result piagent.AgentToolResult, err error, started time.Time) piagent.AgentToolResult {
	outcome := harnesstools.Failed(harnesstools.Input{
		Tool:           t.inner.Name(),
		Args:           args,
		Result:         err.Error(),
		Started:        started,
		Finished:       time.Now(),
		TruncateTokens: t.truncateTokens,
	}, err)
	t.store.put(t.inner.Name(), outcome)
	result.Content = []pi.ToolResultMessageContent{pi.TextContent{Text: outcome.Text}}
	result.IsError = true
	return result
}

// render re-renders the tool's result.
//
// The command's outcome is read back out of the clone's own result text,
// because the reference's step string needs the exit code and the output
// apart.
func (t *harnessTool) render(args json.RawMessage, result piagent.AgentToolResult, started, finished time.Time, before string) harnesstools.Outcome {
	in := harnesstools.Input{
		Tool:           t.inner.Name(),
		Args:           args,
		Result:         toolResultText(result),
		Before:         before,
		After:          readTargetFile(args),
		Started:        started,
		Finished:       finished,
		TruncateTokens: t.truncateTokens,
	}
	if in.Tool == harnesstools.ToolRunCommand {
		if parsed, ok := harnesstools.ParseCommand(in.Result); ok {
			in.ExitCode = parsed.ExitCode
			in.Output = parsed.Output
		}
	}
	return harnesstools.Render(in)
}

// readTargetFile reads the file a call is about.
//
// The reference spells the argument `TargetFile`; the clone's own catalogue
// spells it `path`. A path that names nothing readable reads as empty.
func readTargetFile(args json.RawMessage) string {
	path := argString(args, "TargetFile", "AbsolutePath", "path", "file_path")
	if path == "" {
		return ""
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(content)
}

// outcomeStore remembers the last outcome of each tool.
type outcomeStore struct {
	mu       sync.Mutex
	outcomes map[string]harnesstools.Outcome
}

// newOutcomeStore returns an empty store.
func newOutcomeStore() *outcomeStore {
	return &outcomeStore{outcomes: map[string]harnesstools.Outcome{}}
}

// put records a tool's outcome.
func (s *outcomeStore) put(name string, outcome harnesstools.Outcome) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.outcomes == nil {
		s.outcomes = map[string]harnesstools.Outcome{}
	}
	s.outcomes[name] = outcome
}

// get returns the last outcome of a tool.
//
// The second result is false when the tool never ran — the frame writer
// then writes the action without the extras.
func (s *outcomeStore) get(name string) (harnesstools.Outcome, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	outcome, ok := s.outcomes[name]
	return outcome, ok
}
