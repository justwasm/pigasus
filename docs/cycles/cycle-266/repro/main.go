// Command wagyu-loop-repro is a focused harness for reproducing the
// agent-loop early-termination bug the user reported AND for verifying
// the cycle 266 fix:
//
//   "the agent loop frequently terminates, and even with 3 retries it
//    cannot support long-running tasks; tool calls that succeed show
//    as FAIL."
//
// Run (from the cycle-266 worktree):
//
//   go run ./docs/cycles/cycle-266/repro -mode repro1
//   go run ./docs/cycles/cycle-266/repro -mode repro2
//   go run ./docs/cycles/cycle-266/repro -mode repro3
//
// Three modes:
//
//   repro1 — tool success then transient-stream-error (broken pipe /
//            connection reset). Under cycle 266 the loop retries up
//            to AutoContMaxNetworkDepth (=8) times before giving
//            up with ErrAutoContNetworkFailed.
//   repro2 — 3 tool successes then end-of-stream error. Same fix.
//   repro3 — quota reached. Must still fail fast (cycle 266 only
//            widens the network budget; everything else keeps
//            AutoContMaxDepth=3).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/backend"
)

// scriptedBackend replays a canned sequence of chunk sets, one per Stream
// call. Each entry in `turns` is the chunk list returned for that Stream.
// Calls past the last entry return a single {Done:true} chunk.
type scriptedBackend struct {
	turns [][]backend.StreamChunk
	idx   int
	reqs  []backend.StreamRequest
}

func (s *scriptedBackend) Stream(_ context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
	s.reqs = append(s.reqs, req)
	var chunks []backend.StreamChunk
	if s.idx < len(s.turns) {
		chunks = s.turns[s.idx]
	} else {
		chunks = []backend.StreamChunk{{Done: true}}
	}
	s.idx++
	out := make(chan backend.StreamChunk, len(chunks)+1)
	for _, c := range chunks {
		out <- c
	}
	close(out)
	return out, nil
}

// recordingRunner runs a tool call and records the call. The simulated
// result is deterministic: every tool succeeds with the prefix
// "RESULT(" + name + ")". This is the "tool call succeeded at the
// runner level" path; any "FAIL" displayed by the UI must come from
// somewhere else, not from the runner.
type recordingRunner struct {
	calls []backend.ToolCall
}

func (r *recordingRunner) Run(_ context.Context, c backend.ToolCall) (string, error) {
	r.calls = append(r.calls, c)
	return "RESULT(" + c.Name + ": success)", nil
}

func main() {
	mode := flag.String("mode", "repro1", `repro1 (transient stream-error, 8-budget) | repro2 (3 tool calls then stream error, 8-budget) | repro3 (quota, fail-fast)`)
	flag.Parse()

	switch *mode {
	case "repro1":
		runRepro1()
	case "repro2":
		runRepro2()
	case "repro3":
		runRepro3_quotaFastFail()
	default:
		fmt.Fprintf(os.Stderr, "unknown mode %q\n", *mode)
		os.Exit(2)
	}
}

func runRepro1() {
	// Each entry is one PiG stream call; PiG internally drives the
	// model -> tool -> model cycle so an agent turn may make
	// multiple Stream calls. The retry loop wraps sendPig, so the
	// cycles here are: turn 1 (tool call) succeeds, turn 2 (tool
	// call AND transient stream error) and turn 3..10 (transient
	// stream errors) all fail and trigger auto-continue.
	be := &scriptedBackend{turns: [][]backend.StreamChunk{
		toolCallChunks("c1", "read_file", `{"path":"/Users/gear/Documents/GitHub/openagy/README.md"}`),
		toolCallChunks("c2", "write_to_file", `{"path":"/tmp/openagy.html"}`),
		streamErrorWithToolCall("c3", "run_command", `{"command":"echo done"}`, "fake stream error: connection reset by peer"),
		streamErrorOnly("fake stream error: connection reset by peer (retry 1)"),
		streamErrorOnly("fake stream error: connection reset by peer (retry 2)"),
		streamErrorOnly("fake stream error: connection reset by peer (retry 3)"),
		streamErrorOnly("fake stream error: connection reset by peer (retry 4)"),
		streamErrorOnly("fake stream error: connection reset by peer (retry 5)"),
		streamErrorOnly("fake stream error: connection reset by peer (retry 6)"),
		streamErrorOnly("fake stream error: connection reset by peer (retry 7)"),
		streamErrorOnly("fake stream error: connection reset by peer (retry 8)"),
	}}
	runner := &recordingRunner{}
	runHarness(be, runner, "repro1: tool success → transient network stream error → 8 retries (cycle 266 budget)")
}

func runRepro2() {
	be := &scriptedBackend{turns: [][]backend.StreamChunk{
		toolCallChunks("c1", "read_file", `{"path":"/Users/gear/Documents/GitHub/openagy/README.md"}`),
		toolCallChunks("c2", "write_to_file", `{"path":"/tmp/openagy.html"}`),
		toolCallChunks("c3", "run_command", `{"command":"ls /tmp/openagy.html"}`),
		{{Text: "I have read the README and saved the HTML."}, {Error: fmt.Errorf("fake stream error: broken pipe")}, {Done: true}},
		{{Error: fmt.Errorf("fake stream error: broken pipe (retry 1)")}, {Done: true}},
		{{Error: fmt.Errorf("fake stream error: broken pipe (retry 2)")}, {Done: true}},
		{{Error: fmt.Errorf("fake stream error: broken pipe (retry 3)")}, {Done: true}},
	}}
	runner := &recordingRunner{}
	runHarness(be, runner, "repro2: 3 tool successes → broken-pipe end-error → 8 retries (cycle 266 budget)")
}

func runRepro3_quotaFastFail() {
	// A real-world quota error wraps a *backend.HTTPError with
	// status 429 and "quota" in the body. Auto-continue's quota
	// bail-out (Pre-existing; cycle 237 closed it.) honours this
	// typed error, so the loop exits after the first 429 instead
	// of burning 3 retries on a quota the user can't lift mid-
	// session. Note: quota is NOT a transient network error, so
	// it stays on the AutoContMaxDepth=3 budget — but the quota
	// bail-out typically short-circuits on attempt 0/1.
	quotaErr := &backend.HTTPError{
		StatusCode: 429,
		Body:       []byte(`{"error":{"message":"individual quota reached"}}`),
		URL:        "https://agw.k0s.io/v1/stream",
	}
	be := &scriptedBackend{turns: [][]backend.StreamChunk{
		toolCallChunks("c1", "read_file", `{"path":"/Users/gear/Documents/GitHub/openagy/README.md"}`),
		{{Error: quotaErr}, {Done: true}},
		{{Error: quotaErr}, {Done: true}},
	}}
	runner := &recordingRunner{}
	runHarness(be, runner, "repro3: HTTP 429 quota → fail-fast, no auto-continue wrap")
}

func runHarness(be *scriptedBackend, runner *recordingRunner, label string) {
	fmt.Println("=== " + label + " ===")
	var transcript strings.Builder
	var stopReasons []string
	l, err := agent.New(agent.Options{
		Backend:      be,
		DefaultModel: "test-model",
		Handler: func(c backend.StreamChunk) error {
			if c.Text != "" {
				transcript.WriteString(c.Text)
			}
			if c.Error != nil {
				stopReasons = append(stopReasons, "ERROR:"+c.Error.Error())
			}
			return nil
		},
		Tools: []backend.ToolDecl{
			{Name: "read_file"},
			{Name: "write_to_file"},
			{Name: "run_command"},
		},
		ToolRunner:        runner,
		AutoCont:          true,
		AutoContBanner:    func(depth int) { fmt.Printf("  ↻ auto-continuing (depth=%d)\n", depth) },
		MaxToolIterations: 8,
	})
	if err != nil {
		fmt.Println("New:", err)
		os.Exit(1)
	}
	defer l.Close()

	sendErr := l.Send(context.Background(), agent.TurnInput{
		Messages: []backend.Message{{Role: "user", Content: "research current project, generate doc, save as web page in /tmp"}},
	})

	fmt.Println()
	fmt.Println("RESULTS")
	fmt.Println("-------")
	fmt.Printf("tool calls executed by runner: %d\n", len(runner.calls))
	for i, c := range runner.calls {
		fmt.Printf("  [%d] %s %s\n", i, c.Name, string(c.Arguments))
	}
	fmt.Printf("backend Stream calls: %d\n", len(be.reqs))
	fmt.Printf("stop-reasons observed in handler: %v\n", stopReasons)
	fmt.Printf("transcript length: %d chars\n", transcript.Len())
	fmt.Println()
	fmt.Printf("Send error: %v\n", sendErr)
	if sendErr != nil {
		fmt.Printf("Send error type: %T\n", sendErr)
	}
}

func toolCallChunks(id, name, args string) []backend.StreamChunk {
	return []backend.StreamChunk{
		{ToolCalls: []backend.ToolCall{{ID: id, Name: name, Arguments: json.RawMessage(args)}}},
		{Done: true},
	}
}

func streamErrorWithToolCall(id, name, args, errMsg string) []backend.StreamChunk {
	return []backend.StreamChunk{
		{ToolCalls: []backend.ToolCall{{ID: id, Name: name, Arguments: json.RawMessage(args)}}},
		{Error: fmt.Errorf("%s", errMsg)},
		{Done: true},
	}
}

func streamErrorOnly(errMsg string) []backend.StreamChunk {
	return []backend.StreamChunk{
		{Error: fmt.Errorf("%s", errMsg)},
		{Done: true},
	}
}
