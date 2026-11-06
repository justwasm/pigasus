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
// carries into the request body and the transport sends as it is.
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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/actor"
	"github.com/justwasm/pigasus/internal/app/harnesstools"
)

// harnessTools wraps the harness-side tools so their results come back in
// the reference's shapes.
//
// The client's own tools are not wrapped: the client prints them, and the
// harness has nothing to re-render.
func harnessTools(inner []actor.Tool, conv *conversation, store *outcomeStore, truncateTokens int) []actor.Tool {
	if len(inner) == 0 {
		return nil
	}
	out := make([]actor.Tool, 0, len(inner))
	for _, tool := range inner {
		if tool == nil {
			continue
		}
		out = append(out, &harnessTool{inner: tool, conv: conv, store: store, truncateTokens: truncateTokens})
	}
	return out
}

// harnessTool re-renders one tool's result the way the reference's harness
// renders it.
type harnessTool struct {
	inner          actor.Tool
	conv           *conversation
	store          *outcomeStore
	truncateTokens int
}

func (t *harnessTool) Name() string { return t.inner.Name() }

func (t *harnessTool) Description() string { return t.inner.Description() }

func (t *harnessTool) Definition() actor.ToolDefinition { return t.inner.Definition() }

func (t *harnessTool) RequiresApproval(input string) bool { return t.inner.RequiresApproval(input) }

// Execute runs the tool and re-renders its result.
func (t *harnessTool) Execute(ctx context.Context, input string) (actor.ToolResult, error) {
	callID := actor.ToolCallIDFromContext(ctx)
	args := json.RawMessage(input)

	if t.conv != nil {
		verdict := t.conv.evaluateToolCallOnce(ctx, callID, t.inner.Name(), args)
		if verdict.Blocked {
			outcome := harnesstools.Failed(harnesstools.Input{
				Tool:           t.inner.Name(),
				Args:           args,
				Result:         verdict.Result,
				Started:        time.Now(),
				Finished:       time.Now(),
				TruncateTokens: t.truncateTokens,
			}, errors.New(verdict.Reason))
			if t.store != nil {
				t.store.put(t.inner.Name(), outcome)
			}
			return actor.ToolResult{
				ToolCallID: callID,
				Output:     verdict.Result,
				Error:      verdict.Reason,
			}, nil
		}
		if len(verdict.Args) > 0 {
			args = verdict.Args
		}
	}

	started := time.Now()
	before := readTargetFile(args)
	if err := t.checkWorkingDirectory(args); err != nil {
		return t.failed(args, actor.ToolResult{ToolCallID: callID}, err, started), nil
	}
	args = t.writeArgs(args, before)
	result, err := t.inner.Execute(ctx, string(args))
	result.ToolCallID = callID
	if err != nil {
		return t.failed(args, result, err, started), nil
	}
	finished := time.Now()

	outcome := t.render(args, result, started, finished, before)
	if t.store != nil {
		t.store.put(t.inner.Name(), outcome)
	}
	if outcome.Text != "" {
		result.Output = outcome.Text
	}
	if t.inner.Name() == "finish" {
		result.Terminate = true
	}
	return result, nil
}

// checkWorkingDirectory fails a command whose working directory is gone.
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
func (t *harnessTool) failed(args json.RawMessage, result actor.ToolResult, err error, started time.Time) actor.ToolResult {
	outcome := harnesstools.Failed(harnesstools.Input{
		Tool:           t.inner.Name(),
		Args:           args,
		Result:         err.Error(),
		Started:        started,
		Finished:       time.Now(),
		TruncateTokens: t.truncateTokens,
	}, err)
	if t.store != nil {
		t.store.put(t.inner.Name(), outcome)
	}
	result.Output = outcome.Text
	result.Error = err.Error()
	return result
}

// render re-renders the tool's result.
func (t *harnessTool) render(args json.RawMessage, result actor.ToolResult, started, finished time.Time, before string) harnesstools.Outcome {
	resText := result.Output
	if resText == "" && result.Error != "" {
		resText = result.Error
	}
	in := harnesstools.Input{
		Tool:           t.inner.Name(),
		Args:           args,
		Result:         resText,
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
func (s *outcomeStore) get(name string) (harnesstools.Outcome, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	outcome, ok := s.outcomes[name]
	return outcome, ok
}
