package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/justwasm/pigasus/actor"
	"github.com/justwasm/pigasus/internal/app/tools"
)

func requirePython3(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not found on PATH")
	}
}

func runTool(t *testing.T, wf *RunWorkflow, ctx context.Context, payload map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	res, err := wf.Run(ctx, raw)
	if err != nil {
		t.Fatalf("Run returned unexpected Go error: %v", err)
	}
	s, ok := res.(string)
	if !ok {
		t.Fatalf("expected string result, got %T (%v)", res, res)
	}
	return s
}

func TestValidationAndMetadata(t *testing.T) {
	requirePython3(t)
	wf := &RunWorkflow{}
	if wf.Name() != "run_workflow" {
		t.Fatalf("Name() = %q, want run_workflow", wf.Name())
	}
	if wf.Description() == "" {
		t.Fatal("Description() is empty")
	}
	var schemaObj map[string]any
	if err := json.Unmarshal(wf.Schema(), &schemaObj); err != nil {
		t.Fatalf("Schema() invalid JSON: %v", err)
	}

	// Neither Script nor ScriptPath
	out := runTool(t, wf, context.Background(), map[string]any{"Description": "empty"})
	if !tools.IsFailureText(out) {
		t.Fatalf("expected failure text when neither Script nor ScriptPath set, got:\n%s", out)
	}

	// Both Script and ScriptPath
	out = runTool(t, wf, context.Background(), map[string]any{
		"Script":     "print(1)",
		"ScriptPath": "/tmp/nonexistent.py",
	})
	if !tools.IsFailureText(out) {
		t.Fatalf("expected failure text when both Script and ScriptPath set, got:\n%s", out)
	}

	// Relative ScriptPath
	out = runTool(t, wf, context.Background(), map[string]any{
		"ScriptPath": "relative.py",
	})
	if !tools.IsFailureText(out) {
		t.Fatalf("expected failure text for relative ScriptPath, got:\n%s", out)
	}
}

func TestTopLevelAwaitReturnPrintLogPhaseAndGlobals(t *testing.T) {
	requirePython3(t)

	ws1 := t.TempDir()
	ws2 := t.TempDir()

	var events []Event
	var evMu sync.Mutex

	wf := &RunWorkflow{
		Workspaces:     []string{ws1, ws2},
		ConversationID: "conv-123",
		OnEvent: func(ev Event) {
			evMu.Lock()
			events = append(events, ev)
			evMu.Unlock()
		},
	}

	ctx := actor.ContextWithToolStepIndex(context.Background(), 7)
	script := `
import asyncio, os

phase("Setup")
log("hello", args["camelKey"])
print("printed line")
print("cwd:", os.path.basename(os.getcwd()))
print("globals:", CONVERSATION_ID, STEP_INDEX, len(WORKSPACES))

async def helper(v):
    await asyncio.sleep(0.01)
    return v + 1

val = await helper(41)
return {"ok": val}
`
	out := runTool(t, wf, ctx, map[string]any{
		"Script": script,
		"Args":   map[string]any{"camelKey": "world"},
	})
	if tools.IsFailureText(out) {
		t.Fatalf("unexpected failure:\n%s", out)
	}
	if !strings.Contains(out, "Logs:\n[Setup] hello world") {
		t.Fatalf("missing expected Logs section in:\n%s", out)
	}
	if !strings.Contains(out, "Output:\nprinted line") {
		t.Fatalf("missing printed line in Output:\n%s", out)
	}
	if !strings.Contains(out, "cwd: "+filepath.Base(ws1)) {
		t.Fatalf("expected cwd %s in Output:\n%s", filepath.Base(ws1), out)
	}
	if !strings.Contains(out, "globals: conv-123 7 2") {
		t.Fatalf("missing globals in Output:\n%s", out)
	}
	if !strings.Contains(out, "{'ok': 42}") {
		t.Fatalf("missing trailing return repr in Output:\n%s", out)
	}

	evMu.Lock()
	defer evMu.Unlock()
	if len(events) < 2 || events[0].Kind != EventPhase || events[0].Phase != "Setup" || events[1].Kind != EventLog {
		t.Fatalf("unexpected events: %+v", events)
	}
}

func TestTrailingExpressionReprAndScriptPath(t *testing.T) {
	requirePython3(t)

	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "wf.py")
	err := os.WriteFile(scriptPath, []byte("x = 20\nx + 22\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	wf := &RunWorkflow{Workspace: dir}
	out := runTool(t, wf, context.Background(), map[string]any{
		"ScriptPath": scriptPath,
	})
	if tools.IsFailureText(out) {
		t.Fatalf("unexpected failure:\n%s", out)
	}
	if !strings.Contains(out, "Output:\n42") {
		t.Fatalf("expected trailing expr repr 42 in:\n%s", out)
	}
}

func TestAgentSingleShotCachedAndStatefulMultiTurn(t *testing.T) {
	requirePython3(t)

	var mu sync.Mutex
	var reqs []AgentRequest
	var singleCalls atomic.Int32

	wf := &RunWorkflow{
		AgentExecutor: func(ctx context.Context, req AgentRequest) (any, error) {
			mu.Lock()
			reqs = append(reqs, req)
			mu.Unlock()
			if req.Close {
				return nil, nil
			}
			if req.Role == "Single" {
				n := singleCalls.Add(1)
				return map[string]any{"count": n, "prompt": req.Prompt}, nil
			}
			if req.Role == "Stateful" {
				if strings.Contains(req.Prompt, "fail-turn") {
					return nil, errors.New("transient turn failure")
				}
				return fmt.Sprintf("turn-%d:%s", req.TurnIndex, req.Prompt), nil
			}
			return "ok", nil
		},
	}

	script := `
import gc

# Validation checks
for bad_ws in ("invalid", ""):
    try:
        agent("p", {"type": "string"}, workspace=bad_ws)
        raise AssertionError("expected ValueError for workspace")
    except ValueError:
        pass

try:
    agent("p", {"type": "string"}, model="gpt99")
    raise AssertionError("expected ValueError for model")
except ValueError:
    pass

# Single-shot + double-await caching + JSON string schema
h = agent("do work", '{"type": "object"}', role="Single", workspace="branch", model="flash")
r1 = await h
r2 = await h
assert r1 == r2, (r1, r2)

# Stateful multi-turn
a = agent("Standing instructions.", role="Stateful", workspace="share", model="pro")
t0 = await a("first task", {"type": "string"})
try:
    await a("fail-turn", {"type": "string"})
    raise AssertionError("expected turn failure")
except Exception as e:
    log("caught:", str(e))
t2 = await a("second task", {"type": "string"})
del a
gc.collect()

return {"single": r1, "t0": t0, "t2": t2}
`
	out := runTool(t, wf, context.Background(), map[string]any{"Script": script})
	if tools.IsFailureText(out) {
		t.Fatalf("unexpected failure:\n%s", out)
	}
	if singleCalls.Load() != 1 {
		t.Fatalf("expected single-shot agent to run once on double-await, ran %d times", singleCalls.Load())
	}
	if !strings.Contains(out, "turn-0:Standing instructions.\\n\\nfirst task") {
		t.Fatalf("expected standing instructions on turn 0 in:\n%s", out)
	}
	if !strings.Contains(out, "turn-2:second task") {
		t.Fatalf("expected agent to stay usable after failed turn in:\n%s", out)
	}

	mu.Lock()
	defer mu.Unlock()
	var sawClose bool
	var statefulID string
	for _, r := range reqs {
		if r.Role == "Stateful" {
			statefulID = r.AgentID
		}
		if r.Close && r.AgentID == statefulID && statefulID != "" {
			sawClose = true
		}
	}
	if !sawClose {
		t.Fatalf("expected Close=true RPC on del a, reqs=%+v", reqs)
	}
}

func TestParallelConcurrencyOrderAndFailFastCancellation(t *testing.T) {
	requirePython3(t)

	var cancelledCount atomic.Int32
	var active atomic.Int32
	var maxActive atomic.Int32

	wf := &RunWorkflow{
		AgentExecutor: func(ctx context.Context, req AgentRequest) (any, error) {
			cur := active.Add(1)
			for {
				prev := maxActive.Load()
				if cur <= prev || maxActive.CompareAndSwap(prev, cur) {
					break
				}
			}
			defer active.Add(-1)

			if req.Prompt == "boom" {
				time.Sleep(20 * time.Millisecond)
				return nil, errors.New("boom error")
			}
			if strings.HasPrefix(req.Prompt, "slow-") {
				select {
				case <-time.After(5 * time.Second):
					return "finished", nil
				case <-ctx.Done():
					cancelledCount.Add(1)
					return nil, ctx.Err()
				}
			}
			time.Sleep(30 * time.Millisecond)
			return "res:" + req.Prompt, nil
		},
	}

	// 1. Order preservation & concurrency
	out := runTool(t, wf, context.Background(), map[string]any{
		"Script": `
res = await parallel(["a", "b", "c"], lambda x: agent(x, {"type": "string"}))
return res
`,
	})
	if tools.IsFailureText(out) {
		t.Fatalf("unexpected failure:\n%s", out)
	}
	if !strings.Contains(out, "['res:a', 'res:b', 'res:c']") {
		t.Fatalf("expected ordered results, got:\n%s", out)
	}
	if maxActive.Load() < 2 {
		t.Fatalf("expected concurrent execution (maxActive >= 2), got %d", maxActive.Load())
	}

	// 2. Fail-fast cancellation
	start := time.Now()
	out = runTool(t, wf, context.Background(), map[string]any{
		"Script": `
await parallel(["slow-1", "boom", "slow-2"], lambda x: agent(x, {"type": "string"}))
`,
	})
	elapsed := time.Since(start)
	if !tools.IsFailureText(out) {
		t.Fatalf("expected failure on boom, got:\n%s", out)
	}
	if !strings.Contains(out, "boom error") {
		t.Fatalf("expected boom error in output:\n%s", out)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("expected fail-fast cancellation (<2s), took %v", elapsed)
	}
	if cancelledCount.Load() < 1 {
		t.Fatalf("expected sibling Go contexts to be cancelled, cancelledCount=%d", cancelledCount.Load())
	}
}

func TestPipelineSequentialOrdering(t *testing.T) {
	requirePython3(t)

	var mu sync.Mutex
	var order []string

	wf := &RunWorkflow{
		AgentExecutor: func(ctx context.Context, req AgentRequest) (any, error) {
			mu.Lock()
			order = append(order, "start:"+req.Prompt)
			mu.Unlock()
			time.Sleep(15 * time.Millisecond)
			mu.Lock()
			order = append(order, "end:"+req.Prompt)
			mu.Unlock()
			return strings.ToUpper(req.Prompt), nil
		},
	}

	out := runTool(t, wf, context.Background(), map[string]any{
		"Script": `return await pipeline(["one", "two", "three"], lambda x: agent(x, {"type": "string"}))`,
	})
	if tools.IsFailureText(out) {
		t.Fatalf("unexpected failure:\n%s", out)
	}
	if !strings.Contains(out, "['ONE', 'TWO', 'THREE']") {
		t.Fatalf("unexpected pipeline output:\n%s", out)
	}
	want := []string{"start:one", "end:one", "start:two", "end:two", "start:three", "end:three"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("pipeline did not run sequentially: got %v, want %v", order, want)
	}
}

func TestCallToolSuccessAndToolErrorDenied(t *testing.T) {
	requirePython3(t)

	wf := &RunWorkflow{
		ToolExecutor: func(ctx context.Context, name string, args map[string]any) (string, bool, error) {
			switch name {
			case "echo":
				return fmt.Sprintf("echo:%v", args["msg"]), false, nil
			case "blocked":
				return "", true, errors.New("permission denied by hook")
			case "broken":
				return "", false, errors.New("disk full")
			default:
				return "", false, fmt.Errorf("unknown tool %s", name)
			}
		},
	}

	script := `
ok = await call_tool("echo", msg="hi")
denied_info = None
try:
    await call_tool("blocked", path="/etc/passwd")
except ToolError as e:
    denied_info = (e.tool, e.denied, e.message)

err_info = None
try:
    await call_tool("broken")
except ToolError as e:
    err_info = (e.tool, e.denied, e.message)

return {"ok": ok, "denied": denied_info, "err": err_info}
`
	out := runTool(t, wf, context.Background(), map[string]any{"Script": script})
	if tools.IsFailureText(out) {
		t.Fatalf("unexpected failure:\n%s", out)
	}
	if !strings.Contains(out, "'ok': 'echo:hi'") {
		t.Fatalf("missing echo result in:\n%s", out)
	}
	if !strings.Contains(out, "'denied': ('blocked', True, 'permission denied by hook')") {
		t.Fatalf("missing denied ToolError details in:\n%s", out)
	}
	if !strings.Contains(out, "'err': ('broken', False, 'disk full')") {
		t.Fatalf("missing non-denied ToolError details in:\n%s", out)
	}
}

func TestTrajectoryFilteringAndStepAccess(t *testing.T) {
	requirePython3(t)

	var gotFilter TrajectoryFilter
	wf := &RunWorkflow{
		TrajectoryProvider: func(ctx context.Context, filter TrajectoryFilter) ([]TrajectoryStep, error) {
			gotFilter = filter
			return []TrajectoryStep{
				{
					Index:  0,
					Type:   "USER_INPUT",
					Status: "DONE",
					Text:   "fix tests",
				},
				{
					Index:  1,
					Type:   "GENERIC",
					Status: "ERROR",
					Tool:   "run_command",
					Args:   map[string]any{"command": "go test"},
					Output: "FAIL pkg/foo",
				},
				{
					Index:  2,
					Type:   "GENERIC",
					Status: "CLEARED",
					Tool:   "read_file",
				},
			}, nil
		},
	}

	script := `
steps = await trajectory(last=5, types=["USER_INPUT", "GENERIC"], tools=["run_command"], min_index=0, max_index=10)
assert isinstance(steps[0], dict)
assert steps[0].index == 0 and steps[0]["text"] == "fix tests"
assert steps[0].nonexistent_attr is None
assert steps[1].tool == "run_command" and steps[1].args["command"] == "go test"
assert steps[2].status == "CLEARED" and steps[2].output is None
return len(steps)
`
	out := runTool(t, wf, context.Background(), map[string]any{"Script": script})
	if tools.IsFailureText(out) {
		t.Fatalf("unexpected failure:\n%s", out)
	}
	if !strings.Contains(out, "Output:\n3") {
		t.Fatalf("expected 3 steps in:\n%s", out)
	}
	if gotFilter.Last != 5 || len(gotFilter.Types) != 2 || len(gotFilter.Tools) != 1 ||
		gotFilter.MinIndex == nil || *gotFilter.MinIndex != 0 ||
		gotFilter.MaxIndex == nil || *gotFilter.MaxIndex != 10 {
		t.Fatalf("unexpected TrajectoryFilter passed to provider: %+v", gotFilter)
	}
}

func TestErrorLineReporting(t *testing.T) {
	requirePython3(t)

	wf := &RunWorkflow{}

	// 1. Syntax error on line 3
	out := runTool(t, wf, context.Background(), map[string]any{
		"Script": "a = 1\nb = 2\nc = (\n",
	})
	if !tools.IsFailureText(out) {
		t.Fatalf("expected failure for syntax error, got:\n%s", out)
	}
	if !strings.Contains(out, "line 3") || !strings.Contains(out, "SyntaxError") {
		t.Fatalf("expected line 3 SyntaxError in:\n%s", out)
	}

	// 2. Runtime error on line 5 with multiline string + top-level return
	script := `s = """line1
line2"""
log("before boom")
print("stdout before boom")
x = 1 / 0
return x
`
	out = runTool(t, wf, context.Background(), map[string]any{"Script": script})
	if !tools.IsFailureText(out) {
		t.Fatalf("expected failure for ZeroDivisionError, got:\n%s", out)
	}
	if !strings.Contains(out, "line 5") || !strings.Contains(out, "ZeroDivisionError") {
		t.Fatalf("expected line 5 ZeroDivisionError in:\n%s", out)
	}
	if !strings.Contains(out, "Logs:\nbefore boom") || !strings.Contains(out, "Output:\nstdout before boom") {
		t.Fatalf("expected Logs and Output preserved on failure:\n%s", out)
	}
}

func TestWaitMsBeforeAsyncAndBackgroundHandle(t *testing.T) {
	requirePython3(t)

	// 1. Finishes within WaitMsBeforeAsync -> returns result synchronously
	wf := &RunWorkflow{}
	out := runTool(t, wf, context.Background(), map[string]any{
		"Script":            `return "fast"`,
		"WaitMsBeforeAsync": 5000,
	})
	if !strings.Contains(out, "'fast'") {
		t.Fatalf("expected fast synchronous result, got:\n%s", out)
	}

	// 2. Exceeds WaitMsBeforeAsync with AsyncLauncher
	var launched atomic.Bool
	wfAsync := &RunWorkflow{
		AsyncLauncher: func(ctx context.Context, handle *BackgroundHandle) (any, error) {
			launched.Store(true)
			res, err := handle.Result()
			if err != nil {
				return nil, err
			}
			return "bg:" + res, nil
		},
	}
	raw, err := json.Marshal(map[string]any{
		"Script": `
import asyncio
await asyncio.sleep(0.15)
return "done-bg"
`,
		"WaitMsBeforeAsync": 20,
		"Description":       "slow workflow",
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := wfAsync.Run(context.Background(), raw)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !launched.Load() {
		t.Fatal("expected AsyncLauncher to be called")
	}
	if s, ok := res.(string); !ok || !strings.Contains(s, "bg:") || !strings.Contains(s, "'done-bg'") {
		t.Fatalf("unexpected async launcher result: %v", res)
	}

	// 3. Exceeds WaitMsBeforeAsync without AsyncLauncher -> returns *BackgroundHandle
	raw2, _ := json.Marshal(map[string]any{
		"Script": `
import asyncio
await asyncio.sleep(0.15)
return "handle-done"
`,
		"WaitMsBeforeAsync": 20,
	})
	res2, err := wf.Run(context.Background(), raw2)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	handle, ok := res2.(*BackgroundHandle)
	if !ok {
		t.Fatalf("expected *BackgroundHandle, got %T (%v)", res2, res2)
	}
	finalOut, finalErr := handle.Result()
	if finalErr != nil {
		t.Fatalf("handle.Result() error: %v", finalErr)
	}
	if !strings.Contains(finalOut, "'handle-done'") {
		t.Fatalf("expected handle-done in:\n%s", finalOut)
	}
}

func TestLocalModulesBlockedAndLibraryPathsAllowed(t *testing.T) {
	requirePython3(t)

	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "localmod.py"), []byte("VAL = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	libDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(libDir, "mylib.py"), []byte("def greet():\n    log('from lib')\n    return 'lib-ok'\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	wf := &RunWorkflow{
		Workspace:    cwd,
		LibraryPaths: []string{libDir},
	}
	script := `
local_blocked = False
try:
    import localmod
except ImportError:
    local_blocked = True

import mylib
return {"local_blocked": local_blocked, "lib": mylib.greet()}
`
	out := runTool(t, wf, context.Background(), map[string]any{"Script": script})
	if tools.IsFailureText(out) {
		t.Fatalf("unexpected failure:\n%s", out)
	}
	if !strings.Contains(out, "'local_blocked': True") || !strings.Contains(out, "'lib': 'lib-ok'") {
		t.Fatalf("unexpected output:\n%s", out)
	}
	if !strings.Contains(out, "Logs:\nfrom lib") {
		t.Fatalf("expected library module to see log() primitive:\n%s", out)
	}
}
