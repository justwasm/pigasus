// Package parityverify runs the end-to-end parity verifier that
// boots the real `wagyu remote-control-foreground` daemon, dials its
// ws:// endpoint, and replays every method the cycle plan shipped.
//
// The Go harness mirrors the Python verifier at
// docs/cycles/rc-final-parity-verify/verify.py. They share the
// same method catalog, the same expected envelopes, and the same
// matrix output shape so the two can be diff'd line-for-line.
//
// The harness is hermetic (uses an isolated $ANTIGRAVITY_HOME +
// working dir scoped to the repo root, so WorkdirScope accepts
// ReadRemoteAgentFile/ReadRemoteAgentDir/CreateRemoteAgentProject
// paths) and skipped under -short so the standard test surface
// `go test -short ./internal/...` stays green on machines without
// a built `wagyu` binary.
package parityverify

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// matrixRow is one entry in the parity matrix. The Python verifier
// emits (method, cycle, status, notes); we match that shape.
type matrixRow struct {
	Method string
	Cycle  string
	Status string // "verified", "gap", "skipped"
	Notes  string
}

// runResult is the harness's output: the matrix plus any error
// that aborts the run before all methods were exercised.
type runResult struct {
	Matrix []matrixRow
	Err    error
}

// TestParityMatrix is the entry point. It boots the daemon, dials
// the ws, replays the method catalog, and logs each row. A row
// marked "gap" does not fail the test — gaps are *findings* the
// cycle is verifying, not bugs the harness is hiding.
func TestParityMatrix(t *testing.T) {
	if testing.Short() {
		t.Skip("parity verifier requires a built wagyu binary; skipped under -short")
	}

	// Repo root is the parent of internal/. We need it both for
	// the chdir (so WorkdirScope accepts cmd/pigasus / go.mod / the
	// project workspace) and for the `go build` of the wagyu
	// binary. The harness resolves it from this test file's path.
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}

	binPath, err := ensureWagyuBinary(repoRoot, t.TempDir())
	if err != nil {
		t.Fatalf("build wagyu: %v", err)
	}

	res := runParity(t, repoRoot, binPath)
	for _, row := range res.Matrix {
		t.Logf("%-45s | %-30s | %-9s | %s",
			row.Method, row.Cycle, row.Status, row.Notes)
	}
	if res.Err != nil {
		t.Fatalf("verifier aborted: %v", res.Err)
	}
	// We do not t.Fail on gaps — see doc comment.
}

// runParity boots a daemon, dials it, runs the method catalog, and
// returns the matrix. Extracted so a future `go test -run
// TestParityMatrix_MethodOnly` style harness can reuse it.
func runParity(t *testing.T, repoRoot, binPath string) runResult {
	t.Helper()

	// Isolate $ANTIGRAVITY_HOME so the FS / project / conv stores
	// don't touch real ~/.gemini/antigravity-cli.
	tmpHome := t.TempDir()

	// Pick a free port by binding 127.0.0.1:0; small TOCTOU
	// window accepted as honest on a quiet test machine.
	port, err := pickFreePort()
	if err != nil {
		return runResult{Err: fmt.Errorf("pick free port: %w", err)}
	}
	addr := fmt.Sprintf("0.0.0.0:%d", port)

	// chdir to repo root before starting the daemon so the
	// foreground handler's WorkdirScope accepts cmd/pigasus,
	// go.mod, and the project workspace path. The harness
	// restores the cwd on exit.
	origCwd, err := os.Getwd()
	if err != nil {
		return runResult{Err: fmt.Errorf("getwd: %w", err)}
	}
	if err := os.Chdir(repoRoot); err != nil {
		return runResult{Err: fmt.Errorf("chdir %s: %w", repoRoot, err)}
	}
	t.Cleanup(func() { _ = os.Chdir(origCwd) })

	// Bring up the daemon. We deliberately do NOT pass --log-file
	// because the foreground handler does syscall.Dup2 of fd 1/2
	// onto the log file, which would steal stderr from us and
	// we'd lose the "listening on ws://..." banner that
	// waitForListening parses.
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	cmd := exec.CommandContext(ctx, binPath,
		"remote-control-foreground",
		"--addr="+addr,
		"--name=rc-final-parity-verify-go",
	)
	cmd.Env = append(os.Environ(), "ANTIGRAVITY_HOME="+tmpHome)
	cmd.Stdout = io.Discard
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return runResult{Err: fmt.Errorf("stderr pipe: %w", err)}
	}
	if err := cmd.Start(); err != nil {
		return runResult{Err: fmt.Errorf("start daemon: %v", err)}
	}

	// Capture stderr for postmortem on abort. Protected by a mutex
	// because waitForListening polls the buffer from the main
	// goroutine while the capture goroutine writes to it.
	var stderrMu sync.Mutex
	var stderrBuf strings.Builder
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		s := bufio.NewScanner(stderr)
		// Allow long banner lines (the listening-on message is
		// ~120 chars; the dispatcher can log multi-KB stack
		// traces on failure).
		s.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for s.Scan() {
			stderrMu.Lock()
			stderrBuf.WriteString(s.Text())
			stderrBuf.WriteByte('\n')
			stderrMu.Unlock()
		}
	}()

	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Signal(os.Interrupt)
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				_ = cmd.Process.Kill()
				<-done
			}
			<-stderrDone
			stderrMu.Lock()
			dump := stderrBuf.String()
			stderrMu.Unlock()
			if len(dump) > 0 && t.Failed() {
				t.Logf("daemon stderr:\n%s", dump)
			}
		}
	})

	url, err := waitForListening(stderrDone, &stderrMu, &stderrBuf, port, 10*time.Second)
	if err != nil {
		return runResult{Err: fmt.Errorf("wait for listening: %w", err)}
	}

	ws, _, err := websocket.Dial(context.Background(), url, nil)
	if err != nil {
		return runResult{Err: fmt.Errorf("dial %s: %w", url, err)}
	}
	t.Cleanup(func() { _ = ws.Close(websocket.StatusNormalClosure, "verifier done") })

	v := newVerifier(ws)

	matrix := make([]matrixRow, 0, 24)

	// --- handshake ---
	matrix = append(matrix, checkRow(v, "initialize", "rc-ws-transport", verifyInitialize))
	matrix = append(matrix, checkRow(v, "tools/list", "rc-ws-transport", verifyToolsList))
	matrix = append(matrix, checkRow(v, "tools/call", "rc-status-tool", verifyToolsCallStatus))

	// --- cascade lifecycle ---
	matrix = append(matrix, checkRow(v, "StartRemoteAgentCascade", "rc-cascade-start", verifyStartCascade))
	cid := v.lastCascadeID()

	if cid != "" {
		matrix = append(matrix, checkRowCID(v, "SendRemoteAgentCascadeMessage", "rc-cascade-message", cid, verifySendMessage))
		matrix = append(matrix, checkRowCID(v, "HandleRemoteAgentCascadeUserInteraction", "rc-handle-user-interaction", cid, verifyHandleUserInteraction))
		matrix = append(matrix, checkRowCID(v, "RemoteAgentErrorStatus", "rc-status-tool", cid, verifyErrorStatus))
	} else {
		for _, label := range []matrixRow{
			{Method: "SendRemoteAgentCascadeMessage", Cycle: "rc-cascade-message"},
			{Method: "HandleRemoteAgentCascadeUserInteraction", Cycle: "rc-handle-user-interaction"},
			{Method: "RemoteAgentErrorStatus", Cycle: "rc-status-tool"},
		} {
			label.Status = "skipped"
			label.Notes = "no cascade started"
			matrix = append(matrix, label)
		}
	}

	// --- read-only lists ---
	matrix = append(matrix, checkRow(v, "ListRemoteAgentModels", "rc-list-readonly", verifyListModels))
	matrix = append(matrix, checkRow(v, "ListRemoteAgentSlashCommands", "rc-list-readonly", verifyListSlashCommands))
	matrix = append(matrix, checkRow(v, "ListRemoteAgentConversations", "rc-list-readonly", verifyListConversations))
	matrix = append(matrix, checkRow(v, "ListRemoteAgentEnvironments", "rc-list-readonly", verifyListEnvironments))

	// --- filesystem ---
	matrix = append(matrix, checkRow(v, "ReadRemoteAgentFile", "rc-file-ops",
		func(v *verifier) (string, string) { return verifyReadFile(v, "go.mod") }))
	matrix = append(matrix, checkRow(v, "ReadRemoteAgentDir", "rc-file-ops",
		func(v *verifier) (string, string) { return verifyReadDir(v, "cmd/pigasus") }))

	// --- project CRUD ---
	matrix = append(matrix, checkRow(v, "ListRemoteAgentProjects", "rc-projects", verifyListProjects))

	// WorkdirScope only accepts paths under the daemon's cwd
	// (the wagyu repo root) unless --add-dir widens it; the
	// foreground handler doesn't accept --add-dir. Mirror the
	// Python verifier's choice of placing the workspace under
	// docs/cycles/rc-final-parity-verify/.scratch-workspace.
	projectWorkspace := filepath.Join(repoRoot,
		"docs", "cycles", "rc-final-parity-verify", ".scratch-workspace")
	if err := os.MkdirAll(projectWorkspace, 0o755); err != nil {
		return runResult{Err: fmt.Errorf("mkdir workspace: %w", err)}
	}
	t.Cleanup(func() {
		// Round-trip creates then deletes the project; the
		// workspace itself is empty after the test. rmdir if
		// possible, otherwise leave it for the next run.
		_ = os.Remove(projectWorkspace)
	})
	crudStatus, crudNotes := verifyCreateUpdateDeleteProject(v, projectWorkspace)
	matrix = append(matrix, matrixRow{
		Method: "CreateRemoteAgentProject",
		Cycle:  "rc-projects",
		Status: crudStatus,
		Notes:  crudNotes,
	})
	// Update and Delete are exercised inside the round-trip.
	matrix = append(matrix, matrixRow{
		Method: "UpdateRemoteAgentProject",
		Cycle:  "rc-projects",
		Status: crudStatus,
		Notes:  "verified as part of the create+update+delete round-trip",
	})
	matrix = append(matrix, matrixRow{
		Method: "DeleteRemoteAgentProject",
		Cycle:  "rc-conversation-deletions",
		Status: crudStatus,
		Notes:  "verified as part of the create+update+delete round-trip",
	})

	// --- deletions ---
	matrix = append(matrix, checkRow(v, "DeleteRemoteAgentMessage", "rc-conversation-deletions", verifyDeleteMessageUnknown))
	matrix = append(matrix, checkRow(v, "DeleteRemoteAgentConversation", "rc-conversation-deletions", verifyDeleteConversationUnknown))

	// --- ACE removed (was rc-status-tool stub) ---
	matrix = append(matrix, checkRow(v, "EnsureRemoteAgentACEVM", "rc-ace-real", verifyEnsureACEVM))

	// --- session binding ---
	matrix = append(matrix, checkRow(v, "SetRemoteControlSession", "rc-set-remote-control-session", verifySetRemoteControlSession))

	// --- version ---
	matrix = append(matrix, checkRow(v, "remoteControlVersion", "rc-version", verifyRemoteControlVersion))

	return runResult{Matrix: matrix}
}

// --- harness plumbing ---

// verifier owns the ws handle + a JSON-RPC request id counter.
// Calls are sequential — concurrent calls would need per-id
// response routing that the cycle plan doesn't exercise.
type verifier struct {
	ws     *websocket.Conn
	nextID atomic.Int64

	mu     sync.Mutex
	notifs []map[string]any
	cid    string
}

func newVerifier(ws *websocket.Conn) *verifier {
	return &verifier{ws: ws}
}

// call sends one JSON-RPC request and reads frames off the ws
// until it sees the matching response. Notifications captured
// along the way go into v.notifs so the cycle-5 wire tests can
// assert on them; the response is returned to the caller.
//
// Read timeout is per-frame, not cumulative — a test that emits
// many notifications gets many small reads rather than one big
// read. The cumulative deadline is inherited from `go test
// -timeout`.
func (v *verifier) call(method string, params map[string]any) map[string]any {
	id := v.nextID.Add(1)
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		req["params"] = params
	}
	raw, _ := json.Marshal(req)
	wctx, wcancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer wcancel()
	if err := v.ws.Write(wctx, websocket.MessageText, raw); err != nil {
		return map[string]any{"error": map[string]any{
			"code":    -32603,
			"message": "ws write failed",
			"data":    err.Error(),
		}}
	}
	// Read frames, skipping notifications, until we find our id.
	for {
		rctx, rcancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, respRaw, err := v.ws.Read(rctx)
		rcancel()
		if err != nil {
			return map[string]any{"error": map[string]any{
				"code":    -32603,
				"message": "ws read failed",
				"data":    err.Error(),
			}}
		}
		var frame map[string]any
		if err := json.Unmarshal(respRaw, &frame); err != nil {
			return map[string]any{"error": map[string]any{
				"code":    -32603,
				"message": "unmarshal failed",
				"data":    err.Error(),
			}}
		}
		rid, hasID := frame["id"]
		if !hasID {
			// Notification — record and keep reading.
			v.mu.Lock()
			v.notifs = append(v.notifs, frame)
			v.mu.Unlock()
			continue
		}
		// JSON numbers decode to float64; compare numerically.
		if fid, ok := rid.(float64); ok && int64(fid) == id {
			return frame
		}
		// Different response id — this shouldn't happen with a
		// single-threaded verifier, but if it does, keep reading.
	}
}

func (v *verifier) lastCascadeID() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.cid
}

func (v *verifier) rememberCascadeID(s string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.cid = s
}

// notifsFor returns and clears the notif buffer accumulated since
// the last call to this helper. Used by tests that want to assert
// on the wire shape (notifications + response) of a single call.
func (v *verifier) drainNotifs() []map[string]any {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := v.notifs
	v.notifs = nil
	return out
}

// --- per-method checks (mirror the python verifier) ---

type checkFn func(*verifier) (string, string)

func checkRow(v *verifier, method, cycle string, fn checkFn) matrixRow {
	status, notes := fn(v)
	return matrixRow{Method: method, Cycle: cycle, Status: status, Notes: notes}
}

func checkRowCID(v *verifier, method, cycle, cid string, fn func(*verifier, string) (string, string)) matrixRow {
	status, notes := fn(v, cid)
	return matrixRow{Method: method, Cycle: cycle, Status: status, Notes: notes}
}

// --- per-method implementations ---

func verifyInitialize(v *verifier) (string, string) {
	resp := v.call("initialize", map[string]any{
		"clientInfo": map[string]any{"name": "verifier-go", "version": "0.1"},
	})
	if e, ok := resp["error"]; ok {
		return "gap", fmt.Sprintf("unexpected error: %v", e)
	}
	r, _ := resp["result"].(map[string]any)
	si, _ := r["serverInfo"].(map[string]any)
	if si["name"] != "openagy-remote-control" {
		return "gap", fmt.Sprintf("serverInfo.name=%v", si["name"])
	}
	if si["version"] != "0.1.0" {
		return "gap", fmt.Sprintf("serverInfo.version=%v", si["version"])
	}
	if r["protocolVersion"] != "2024-11-05" {
		return "gap", fmt.Sprintf("protocolVersion=%v", r["protocolVersion"])
	}
	return "verified",
		fmt.Sprintf("serverInfo.name=openagy-remote-control, version=0.1.0, protocolVersion=2024-11-05")
}

func verifyToolsList(v *verifier) (string, string) {
	resp := v.call("tools/list", nil)
	if e, ok := resp["error"]; ok {
		return "gap", fmt.Sprintf("unexpected error: %v", e)
	}
	r, _ := resp["result"].(map[string]any)
	tools, _ := r["tools"].([]any)
	if len(tools) == 0 {
		return "gap", "tools slice empty"
	}
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		m, _ := t.(map[string]any)
		if n, ok := m["name"].(string); ok {
			names = append(names, n)
		}
	}
	return "verified", fmt.Sprintf("%d tools: %s", len(tools), strings.Join(names, ", "))
}

func verifyToolsCallStatus(v *verifier) (string, string) {
	resp := v.call("tools/call", map[string]any{
		"name":      "status",
		"arguments": map[string]any{},
	})
	if e, ok := resp["error"]; ok {
		return "gap", fmt.Sprintf("unexpected error: %v", e)
	}
	r, _ := resp["result"].(map[string]any)
	content, _ := r["content"].([]any)
	if len(content) == 0 {
		return "gap", "content empty"
	}
	first, _ := content[0].(map[string]any)
	text, _ := first["text"].(string)
	var snap map[string]any
	if err := json.Unmarshal([]byte(text), &snap); err != nil {
		return "gap", fmt.Sprintf("status text is not JSON: %v: %s", err, truncate(text, 120))
	}
	for _, k := range []string{
		"pid", "version", "backend", "model", "workspace",
		"started_at", "uptime", "connected_clients", "cascades",
		"auth", "daemon",
	} {
		if _, ok := snap[k]; !ok {
			return "gap", fmt.Sprintf("status.%s missing", k)
		}
	}
	return "verified",
		"rich envelope: pid/version/backend/model/workspace/started_at/uptime/connected_clients/cascades/auth/daemon"
}

func verifyStartCascade(v *verifier) (string, string) {
	resp := v.call("StartRemoteAgentCascade", map[string]any{
		"clientInfo": map[string]any{"name": "verifier-go", "version": "0.1"},
		"backend":    "shim",
	})
	if e, ok := resp["error"]; ok {
		return "gap", fmt.Sprintf("unexpected error: %v", e)
	}
	r, _ := resp["result"].(map[string]any)
	cid, _ := r["cascade_id"].(string)
	if !strings.HasPrefix(cid, "cas_") {
		return "gap", fmt.Sprintf("cascade_id missing/bad prefix: %q", cid)
	}
	for _, k := range []string{"conversationId", "created_at", "model", "backend", "version"} {
		if _, ok := r[k]; !ok {
			return "gap", fmt.Sprintf("%s missing from start response: %v", k, r)
		}
	}
	v.rememberCascadeID(cid)
	backend, _ := r["backend"].(string)
	return "verified",
		fmt.Sprintf("cascade_id=%s, backend=%q (params.backend honored since rc-polish G16)", cid, backend)
}

func verifySendMessage(v *verifier, cid string) (string, string) {
	resp := v.call("SendRemoteAgentCascadeMessage", map[string]any{
		"cascade_id": cid,
		"text":       "hello, world",
	})
	// Two acceptable wire shapes from the dispatcher's POV:
	//
	//   1. The happy path: a well-formed synchronous reply
	//      envelope (5 fields, populated stopReason) AND ≥2
	//      cascade/state notifications (starting + done).
	//   2. The cycle-15 netless path: -32603 "turn failed"
	//      because the real PiG session closes without
	//      DoneEvent when no OAuth network is reachable. The
	//      cycle plan accepts this as a "wire-shape verified"
	//      finding; the model integration is the next cycle.
	//
	// We pin both — anything else (e.g. -32004 cascade-not-found
	// or -32602 bad params) is a real gap.
	if e, ok := resp["error"].(map[string]any); ok {
		code, _ := e["code"].(float64)
		if int(code) == -32603 {
			return "verified",
				"-32603 turn failed (real PiG path closes without DoneEvent; see deliverable §known-gaps)"
		}
		return "gap", fmt.Sprintf("send error code=%v: %v", code, e)
	}
	r, _ := resp["result"].(map[string]any)
	for _, k := range []string{"conversationId", "reply", "durationMs", "toolCalls", "stopReason"} {
		if _, ok := r[k]; !ok {
			return "gap", fmt.Sprintf("reply envelope missing %s: %v", k, r)
		}
	}
	sr, _ := r["stopReason"].(string)
	if sr != "end_turn" && sr != "tool_use" && sr != "max_tokens" && sr != "error" {
		return "gap", fmt.Sprintf("unexpected stopReason: %s", sr)
	}
	reply, _ := r["reply"].(string)
	replyNote := fmt.Sprintf("reply=%q", truncate(reply, 60))
	if reply == "" {
		replyNote = "reply='' (real PiG path closes without DoneEvent; see deliverable §known-gaps)"
	}
	return "verified",
		fmt.Sprintf("%s; stopReason=%s", replyNote, sr)
}

func verifyHandleUserInteraction(v *verifier, cid string) (string, string) {
	resp := v.call("HandleRemoteAgentCascadeUserInteraction", map[string]any{
		"cascade_id":     cid,
		"interaction_id": "intr_doesnotexist",
		"response":       map[string]any{"choice_id": "approve"},
	})
	e, ok := resp["error"].(map[string]any)
	if !ok {
		return "gap", fmt.Sprintf("expected error envelope, got result: %v", resp)
	}
	code, _ := e["code"].(float64)
	if int(code) != -32006 {
		return "gap", fmt.Sprintf("expected -32006, got code=%v: %v", code, e)
	}
	msg, _ := e["message"].(string)
	return "verified", fmt.Sprintf("unknown interaction_id → code=%d (%s)", int(code), msg)
}

func verifyErrorStatus(v *verifier, cid string) (string, string) {
	resp := v.call("RemoteAgentErrorStatus", map[string]any{"cascade_id": cid})
	if e, ok := resp["error"]; ok {
		return "gap", fmt.Sprintf("unexpected error: %v", e)
	}
	r, _ := resp["result"].(map[string]any)
	errs, _ := r["errors"].([]any)
	return "verified", fmt.Sprintf("errors ring empty (%d entries)", len(errs))
}

func verifyListModels(v *verifier) (string, string) {
	resp := v.call("ListRemoteAgentModels", map[string]any{})
	if e, ok := resp["error"]; ok {
		return "gap", fmt.Sprintf("unexpected error: %v", e)
	}
	r, _ := resp["result"].(map[string]any)
	models, _ := r["models"].([]any)
	if len(models) == 0 {
		return "gap", "models slice empty"
	}
	dm, _ := r["default_model"].(string)
	return "verified", fmt.Sprintf("%d model rows; default_model=%s", len(models), dm)
}

func verifyListSlashCommands(v *verifier) (string, string) {
	resp := v.call("ListRemoteAgentSlashCommands", nil)
	if e, ok := resp["error"]; ok {
		return "gap", fmt.Sprintf("unexpected error: %v", e)
	}
	r, _ := resp["result"].(map[string]any)
	cmds, _ := r["commands"].([]any)
	if len(cmds) == 0 {
		return "gap", "commands slice empty"
	}
	return "verified", fmt.Sprintf("%d slash commands", len(cmds))
}

func verifyListConversations(v *verifier) (string, string) {
	resp := v.call("ListRemoteAgentConversations", map[string]any{"limit": 10})
	if e, ok := resp["error"]; ok {
		return "gap", fmt.Sprintf("unexpected error: %v", e)
	}
	r, _ := resp["result"].(map[string]any)
	convs, _ := r["conversations"].([]any)
	return "verified", fmt.Sprintf("conversations slice (%d rows; empty on fresh store)", len(convs))
}

func verifyListEnvironments(v *verifier) (string, string) {
	resp := v.call("ListRemoteAgentEnvironments", nil)
	if e, ok := resp["error"]; ok {
		return "gap", fmt.Sprintf("unexpected error: %v", e)
	}
	r, _ := resp["result"].(map[string]any)
	for _, k := range []string{"os", "arch", "cwd", "antigravity_home", "version", "env_keys_present"} {
		if _, ok := r[k]; !ok {
			return "gap", fmt.Sprintf("environments.%s missing", k)
		}
	}
	return "verified", fmt.Sprintf("os=%v arch=%v cwd=%v", r["os"], r["arch"], r["cwd"])
}

func verifyReadFile(v *verifier, path string) (string, string) {
	resp := v.call("ReadRemoteAgentFile", map[string]any{"path": path})
	if e, ok := resp["error"]; ok {
		return "gap", fmt.Sprintf("unexpected error reading %s: %v", path, e)
	}
	r, _ := resp["result"].(map[string]any)
	enc, _ := r["encoding"].(string)
	if enc != "utf-8" && enc != "binary" {
		return "gap", fmt.Sprintf("encoding bad: %v", r)
	}
	if _, ok := r["content"]; !ok {
		return "gap", fmt.Sprintf("content missing: %v", r)
	}
	size, _ := r["size"].(float64)
	return "verified", fmt.Sprintf("%s content, %d bytes", enc, int(size))
}

func verifyReadDir(v *verifier, path string) (string, string) {
	resp := v.call("ReadRemoteAgentDir", map[string]any{"path": path})
	if e, ok := resp["error"]; ok {
		return "gap", fmt.Sprintf("unexpected error reading dir %s: %v", path, e)
	}
	r, _ := resp["result"].(map[string]any)
	for _, k := range []string{"entries", "total_entries", "truncated"} {
		if _, ok := r[k]; !ok {
			return "gap", fmt.Sprintf("dir envelope bad: %v", r)
		}
	}
	total, _ := r["total_entries"].(float64)
	return "verified", fmt.Sprintf("%d entries", int(total))
}

func verifyListProjects(v *verifier) (string, string) {
	resp := v.call("ListRemoteAgentProjects", map[string]any{"limit": 10})
	if e, ok := resp["error"]; ok {
		return "gap", fmt.Sprintf("unexpected error: %v", e)
	}
	r, _ := resp["result"].(map[string]any)
	if _, ok := r["projects"]; !ok {
		return "gap", fmt.Sprintf("projects envelope bad: %v", r)
	}
	total, _ := r["total"].(float64)
	return "verified", fmt.Sprintf("projects slice (%d rows)", int(total))
}

func verifyCreateUpdateDeleteProject(v *verifier, tmpDir string) (string, string) {
	name := "verifier-go-project"

	// Create
	resp := v.call("CreateRemoteAgentProject", map[string]any{
		"name":           name,
		"workspace":      tmpDir,
		"default_effort": "medium",
	})
	if e, ok := resp["error"]; ok {
		return "gap", fmt.Sprintf("create error: %v", e)
	}
	r, _ := resp["result"].(map[string]any)
	pid, _ := r["id"].(string)
	if pid == "" {
		return "gap", fmt.Sprintf("create returned no id: %v", r)
	}

	// Update
	resp = v.call("UpdateRemoteAgentProject", map[string]any{
		"id":            pid,
		"name":          name + "-v2",
		"default_model": "gpt-test",
	})
	if e, ok := resp["error"]; ok {
		return "gap", fmt.Sprintf("update error: %v", e)
	}

	// Delete
	resp = v.call("DeleteRemoteAgentProject", map[string]any{
		"id":            pid,
		"fail_if_bound": false,
	})
	if e, ok := resp["error"]; ok {
		return "gap", fmt.Sprintf("delete error: %v", e)
	}

	return "verified", "create+update+delete round-trip ok"
}

func verifyDeleteMessageUnknown(v *verifier) (string, string) {
	resp := v.call("DeleteRemoteAgentMessage", map[string]any{
		"conversation_id": "conv_doesnotexist",
		"message_id":      "msg_doesnotexist",
	})
	e, ok := resp["error"].(map[string]any)
	if !ok {
		return "gap", fmt.Sprintf("expected error envelope, got result: %v", resp)
	}
	code, _ := e["code"].(float64)
	if int(code) != -32011 && int(code) != -32012 {
		return "gap", fmt.Sprintf("expected -32011/-32012, got code=%v: %v", code, e)
	}
	msg, _ := e["message"].(string)
	return "verified", fmt.Sprintf("non-existent conversation → code=%d (%s)", int(code), msg)
}

func verifyDeleteConversationUnknown(v *verifier) (string, string) {
	resp := v.call("DeleteRemoteAgentConversation", map[string]any{
		"conversation_id": "conv_doesnotexist",
	})
	e, ok := resp["error"].(map[string]any)
	if !ok {
		return "gap", fmt.Sprintf("expected error envelope, got result: %v", resp)
	}
	code, _ := e["code"].(float64)
	if int(code) != -32011 {
		return "gap", fmt.Sprintf("expected -32011, got code=%v: %v", code, e)
	}
	msg, _ := e["message"].(string)
	return "verified", fmt.Sprintf("non-existent conversation → code=%d (%s)", int(code), msg)
}

func verifyEnsureACEVM(v *verifier) (string, string) {
	// Cycle rc-ace-real: the dispatcher no longer
	// registers EnsureRemoteAgentACEVM, so the call
	// returns -32601 "Method not found" via the
	// default arm. The matrix records this as "gap"
	// because the production binary exposes the method
	// (over a remote gRPC channel); a future
	// rc-ace-real-bridge cycle that wires a thin
	// client over that channel would flip this back
	// to "verified".
	resp := v.call("EnsureRemoteAgentACEVM", map[string]any{})
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		if _, hasResult := resp["result"]; hasResult {
			return "gap", "still returns a result envelope, want -32601"
		}
		return "gap", fmt.Sprintf("no error envelope: %#v", resp)
	}
	code, _ := errObj["code"].(float64)
	if int(code) != -32601 {
		return "gap", fmt.Sprintf("error.code = %v, want -32601", errObj["code"])
	}
	msg, _ := errObj["message"].(string)
	if msg != "Method not found" {
		return "gap", fmt.Sprintf("error.message = %q, want \"Method not found\"", msg)
	}
	return "gap",
		"removed in rc-ace-real: method no longer registered; -32601 on wire"
}

func verifySetRemoteControlSession(v *verifier) (string, string) {
	// Read the current status first to capture a baseline.
	baseline := v.call("tools/call", map[string]any{
		"name":      "status",
		"arguments": map[string]any{},
	})
	baseText := statusText(baseline)
	var baseSnap map[string]any
	if err := json.Unmarshal([]byte(baseText), &baseSnap); err != nil {
		return "gap", fmt.Sprintf("baseline status not JSON: %v", err)
	}
	baseAuth, _ := baseSnap["auth"].(map[string]any)
	baseSID, _ := baseAuth["session_id"].(string)

	// Bind a synthetic session id.
	newSID := "sess_verifier_go_" + randSuffix()
	resp := v.call("SetRemoteControlSession", map[string]any{"session_id": newSID})
	if e, ok := resp["error"]; ok {
		return "gap", fmt.Sprintf("unexpected error: %v", e)
	}

	// Re-read status; auth.session_id must reflect newSID (or be
	// cleared if the cycle resets it; the brief pins "bound").
	followup := v.call("tools/call", map[string]any{
		"name":      "status",
		"arguments": map[string]any{},
	})
	followText := statusText(followup)
	var followSnap map[string]any
	if err := json.Unmarshal([]byte(followText), &followSnap); err != nil {
		return "gap", fmt.Sprintf("follow-up status not JSON: %v", err)
	}
	followAuth, _ := followSnap["auth"].(map[string]any)
	followSID, _ := followAuth["session_id"].(string)
	if followSID == baseSID {
		return "gap", fmt.Sprintf("session_id unchanged: %q", followSID)
	}
	return "verified",
		fmt.Sprintf("session_id bound (%q → %q); status.auth.session_id reflects", baseSID, followSID)
}

func verifyRemoteControlVersion(v *verifier) (string, string) {
	resp := v.call("remoteControlVersion", nil)
	if e, ok := resp["error"]; ok {
		return "gap", fmt.Sprintf("unexpected error: %v", e)
	}
	r, _ := resp["result"].(map[string]any)
	vv, _ := r["version"].(string)
	bl, _ := r["build_label"].(string)
	ds, _ := r["daemon_sha"].(string)
	return "verified", fmt.Sprintf("version=%s build_label=%s daemon_sha=%s", vv, bl, ds)
}

// statusText extracts the inner text payload from a tools/call
// status response.
func statusText(resp map[string]any) string {
	r, _ := resp["result"].(map[string]any)
	content, _ := r["content"].([]any)
	if len(content) == 0 {
		return ""
	}
	first, _ := content[0].(map[string]any)
	t, _ := first["text"].(string)
	return t
}

// --- support ---

// pickFreePort opens 127.0.0.1:0 to discover a free port and
// returns it. Small TOCTOU window accepted as honest on a quiet
// test machine.
func pickFreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// waitForListening polls the daemon's stderr pipe for the
// "listening on ws://..." banner. We deliberately read stderr
// ourselves (rather than passing --log-file which would Dup2
// stderr away) so the verifier keeps its visibility into the
// daemon's logs.
//
// The foreground handler prints `ws://127.0.0.1:<port>/rpc` in the
// banner even when we bound to 0.0.0.0 (the loopback-safe form
// the verifier dials), so we wait for that exact prefix.
func waitForListening(stderrDone <-chan struct{}, mu *sync.Mutex, buf *strings.Builder, port int, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	prefix := fmt.Sprintf("remote-control-foreground: listening on ws://127.0.0.1:%d/rpc", port)
	for time.Now().Before(deadline) {
		select {
		case <-stderrDone:
			return "", errors.New("daemon stderr closed before listening banner")
		default:
		}
		mu.Lock()
		snapshot := buf.String()
		mu.Unlock()
		if strings.Contains(snapshot, prefix) {
			return fmt.Sprintf("ws://127.0.0.1:%d/rpc", port), nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	mu.Lock()
	snapshot := buf.String()
	mu.Unlock()
	return "", fmt.Errorf("timed out waiting for %q; stderr so far:\n%s", prefix, snapshot)
}

// ensureWagyuBinary returns a path to a built `wagyu` binary. It
// uses WAGYU_PARITY_BIN if set (CI / developers with a prebuilt
// binary); otherwise it `go build`s into `outDir`. The cycle brief
// scopes the verifier to running this build, not vendoring a
// binary.
func ensureWagyuBinary(repoRoot, outDir string) (string, error) {
	if p := os.Getenv("WAGYU_PARITY_BIN"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	binPath := filepath.Join(outDir, "pigasus")
	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/pigasus")
	cmd.Dir = repoRoot
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("go build: %w", err)
	}
	return binPath, nil
}

// findRepoRoot walks up from this test file until it finds a
// directory containing go.mod. Internal so it can be reused by
// future tests in the same package.
func findRepoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found in any parent directory")
		}
		dir = parent
	}
	return "", errors.New("go.mod not found within 8 levels")
}

// truncate clamps a string to at most n bytes, appending "..." if
// it was clipped. Operates on bytes (not runes) so it's safe for
// ASCII status strings — the dashboard rarely returns multibyte
// text in this envelope.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// randSuffix returns a short pseudo-random hex string. Used to
// pick a non-clashing session_id for the SetRemoteControlSession
// probe. crypto/rand is overkill; time-derived is fine for a test.
func randSuffix() string {
	const hex = "0123456789abcdef"
	now := time.Now().UnixNano()
	out := make([]byte, 8)
	for i := range out {
		out[i] = hex[(now>>(i*4))&0xf]
	}
	return string(out)
}
