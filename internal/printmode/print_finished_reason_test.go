package printmode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The 1.4.11 envelope carries `finished_reason` as its **last** member,
// and omits it when the turn finishes normally.
//
// 【一手】 the 1.4.11 binary, captured on the error path:
//
//	{"conversation_id":"","status":"ERROR","response":"",
//	 "error":"authentication failed or timed out","duration_seconds":0,
//	 "num_turns":0,"usage":{…},"finished_reason":"error"}
func TestFinishedReasonFieldOrder(t *testing.T) {
	b, err := json.Marshal(jsonEnvelope{
		Status:         printStatusError,
		Error:          "boom",
		FinishedReason: finishedReasonError,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"conversation_id":"","status":"ERROR","response":"","error":"boom","duration_seconds":0,"num_turns":0,"usage":{"input_tokens":0,"output_tokens":0,"thinking_tokens":0,"cache_read_tokens":0,"total_tokens":0},"finished_reason":"error"}`
	if got := string(b); got != want {
		t.Fatalf("envelope wire shape:\n got %s\nwant %s", got, want)
	}
}

// A turn that finishes normally carries no `finished_reason` at all.
func TestFinishedReasonOmittedOnSuccess(t *testing.T) {
	b, err := json.Marshal(jsonEnvelope{Status: printStatusSuccess, Response: "hi"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "finished_reason") {
		t.Fatalf("a finished turn must not carry finished_reason: %s", b)
	}
}

// emitFinalPrintEnvelope reports why the turn ended early.
func TestEmitFinalPrintEnvelopeFinishedReason(t *testing.T) {
	cases := []struct {
		name        string
		runErr      error
		timedOut    bool
		interrupted bool
		want        string
	}{
		{name: "normal", want: ""},
		{name: "timeout", timedOut: true, want: finishedReasonPrintTimeout},
		{name: "interrupted", interrupted: true, runErr: context.Canceled, want: finishedReasonInterrupted},
		{name: "error", runErr: errors.New("boom"), want: finishedReasonError},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			cfg := &printRunConfig{format: printFormatJSON, conversationID: "c"}
			st := &printTurnState{}
			if err := emitFinalPrintEnvelope(&buf, nil, cfg, st, c.runErr, c.timedOut, c.interrupted); err != nil {
				t.Fatalf("emit: %v", err)
			}
			var env map[string]any
			if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
				t.Fatalf("decode: %v (%s)", err, buf.String())
			}
			got, _ := env["finished_reason"].(string)
			if got != c.want {
				t.Fatalf("finished_reason = %q, want %q (envelope %s)", got, c.want, buf.String())
			}
		})
	}
}

// The stream-json `result` event carries the same envelope, so a script
// reading the stream sees the reason too.
func TestStreamJSONResultCarriesFinishedReason(t *testing.T) {
	var buf bytes.Buffer
	cfg := &printRunConfig{format: printFormatStreamJSON, conversationID: "c"}
	st := &printTurnState{}
	if err := emitFinalPrintEnvelope(&buf, nil, cfg, st, errors.New("boom"), false, false); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if !strings.Contains(buf.String(), `"finished_reason":"error"`) {
		t.Fatalf("stream result misses finished_reason: %s", buf.String())
	}
}

// A Ctrl+C cancels the turn instead of killing the process, and the guard
// remembers that it was the interrupt.
func TestPrintInterruptGuardCancelsTheTurn(t *testing.T) {
	guard := newPrintInterruptGuard()
	defer guard.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	guard.cancel(cancel)

	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatalf("kill: %v", err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the interrupt did not cancel the turn's context")
	}
	if !guard.Interrupted() {
		t.Fatal("the guard did not record the interrupt")
	}
}
