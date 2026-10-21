// turn_error_test.go — cycle 279: a failed turn must leave a visible row.
//
// The defect this pins: streamPrompt ended with
//
//	_ = loop.Send(ctx, agent.TurnInput{Messages: msgs})
//	return turnDoneMsg{reply: out, steps: steps, reasoning: …}
//
// so the error that told the user *why* the turn stopped never reached the
// transcript. Measured against MiniMax M3: a turn that died at 1m0s produced
//
//	● 3 files, 3 commands, 1 tool (ctrl+o to expand)
//
// and nothing else — no reply, no error, no clue. The reference, on the same
// prompt, waits and answers.
package main

import (
	"errors"
	"flag"
	"io"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/backend"
)

// TestTurnDoneMsg_ErrorRowSurvivesTurnBlock pins the shape the user sees: the
// tool block is still collapsed, and the failure is spelled out underneath it
// instead of being replaced by it.
func TestTurnDoneMsg_ErrorRowSurvivesTurnBlock(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.response = append(m.response, "> do a long thing")
	m.liveStart = len(m.response)
	m.session = append(m.session, backend.Message{Role: "user", Content: "do a long thing"})

	_, _ = m.Update(turnDoneMsg{
		reply: "",
		steps: []toolStep{{
			kind:   stepKindTool,
			name:   "Bash",
			title:  "Bash",
			detail: "sh chain.sh start",
		}},
		err: errors.New("context deadline exceeded"),
	})

	joined := strings.Join(m.response, "\n")
	if !strings.Contains(joined, "Error: context deadline exceeded") {
		t.Fatalf("transcript has no error row:\n%s", joined)
	}
	if !strings.Contains(joined, "ctrl+o to expand") {
		t.Errorf("tool block vanished; transcript:\n%s", joined)
	}
}

// TestTurnDoneMsg_ErrorRowUsesReferenceCopy pins the API-error path: when the
// failure carries a *backend.HTTPError, the row uses the same message the
// headless `error:` line and AGY_ERROR envelope use, not the raw transport
// string.
func TestTurnDoneMsg_ErrorRowUsesReferenceCopy(t *testing.T) {
	api := &backend.HTTPError{StatusCode: 429, Body: []byte(`{"error":{"message":"Individual quota reached."}}`)}
	row := turnErrorRow(api)
	if !strings.HasPrefix(row, "Error: ") {
		t.Errorf("row = %q, want an Error: prefix", row)
	}
	if !strings.Contains(row, "Individual quota reached.") {
		t.Errorf("row = %q, want the API message", row)
	}
	if strings.Contains(row, "429") && !strings.Contains(row, "quota") {
		t.Errorf("row leaked the transport shape: %q", row)
	}
	if got := turnErrorRow(nil); got != "" {
		t.Errorf("turnErrorRow(nil) = %q, want empty", got)
	}
}

// TestPickBackend_InteractiveHasNoTotalRequestDeadline pins the transport half
// of the same defect.
//
// The interactive backend used to take the agw client's 60 s *total*
// per-request timeout, so even after the turn deadline was removed a single
// long model response would still have been cut at 60 s with no override.
// print mode already passed AgwTimeoutUnlimited; the live TUI now matches.
// A stalled stream is still bounded by piagentbridge's 5-minute idle watcher,
// which only fires when no frames arrive.
func TestPickBackend_InteractiveHasNoTotalRequestDeadline(t *testing.T) {
	t.Setenv("JETSKI_OAUTH_TOKEN", "test-token")
	t.Setenv("OPENAGY_BACKEND", "agw")

	be := pickBackend(nil, "MiniMax M3")
	agw, ok := be.(interface{ AgwConfig() backend.AgwK0sConfig })
	if !ok {
		t.Fatalf("pickBackend returned %T, want an agw client", be)
	}
	if got := agw.AgwConfig().TimeoutSeconds; got != backend.AgwTimeoutUnlimited {
		t.Errorf("interactive agw TimeoutSeconds = %d, want %d (no total deadline)",
			got, backend.AgwTimeoutUnlimited)
	}
}

// TestPickBackend_HonoursExplicitBackendFlag is the guard for the test above:
// it proves the resolution path actually went through the agw branch (a shim
// fallback would make the timeout assertion vacuous).
func TestPickBackend_HonoursExplicitBackendFlag(t *testing.T) {
	t.Setenv("JETSKI_OAUTH_TOKEN", "test-token")
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.String("backend", "", "")
	if err := fs.Parse([]string{"--backend", "agw"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := pickBackendWithLog(fs, "m", io.Discard, backend.AgwTimeoutUnlimited).(interface{ AgwConfig() backend.AgwK0sConfig }); !ok {
		t.Fatal("--backend agw did not yield an agw client")
	}
}
