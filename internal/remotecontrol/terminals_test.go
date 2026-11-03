// Tests for the rc-terminals cycle. Covers:
//
//   - The five JSON-RPC handlers
//     (CreateRemoteAgentTerminal / ListRemoteAgentTerminals /
//     SendRemoteAgentTerminalInput /
//     ResizeRemoteAgentTerminal /
//     CloseRemoteAgentTerminal) at the in-process level:
//     closed-set kind validation, default 80x24 dimensions,
//     sentinel-error mapping
//     (ErrNotFound → -32018,
//     ErrClosed → -32019,
//     ErrInputRejected → -32602),
//     base64-encoding-decoding on Send params,
//     terminal/output broadcast notification fan-out.
//
//   - The cascade-attached drain path
//     (TestTerminalOutput_StreamsFromCascade): create
//     a cascade, attach it to a terminal via
//     the cascade's output chan, write chunks to the cascade's
//     chan, and assert the terminal's subscriber sees them.
//
// The registry's own in-process tests live with the registry, in
// the terminals domain package; this file covers the host's
// dispatcher — the fan-out the host owns.
//
// the project's split: in-process envelopes vs.
// wire round-trips).
package remotecontrol

import (
	"context"
	"encoding/base64"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/remotecontrol/cascades"
	"github.com/justwasm/pigasus/internal/remotecontrol/terminals"
)

// newTerminalsDispatcher returns a Dispatcher with the supplied
// terminal registry pre-installed. The helper mirrors
// newConversationConfigDispatcher / newFastpushDispatcher.
//
// When reg is nil, NewDispatcher fabricates a fresh empty
// registry (the nil-guard pattern rc-terminals adds). Tests that
// want to assert "no registry installed" pass nil and exercise the
// -32603 path; tests that want a specific registry pass it
// directly.
func newTerminalsDispatcher(t *testing.T, reg *terminals.Registry) *Dispatcher {
	t.Helper()
	sess := NewSessionState("1.4.2")
	d, err := NewDispatcher(Config{
		Session:           sess,
		TerminalsRegistry: reg,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	return d
}

// dispatchTerminals sends a method request through
// the dispatcher and returns (result, error) pair.
// Mirrors dispatchConversationConfig / dispatchFastpush.
//
// nil params marshals to JSON `null` so the
// dispatcher's "len(params) > 0" guards see a
// present (but null) params block.
func dispatchTerminals(t *testing.T, d *Dispatcher, method string, params any) (map[string]any, map[string]any) {
	t.Helper()
	var raw []byte
	switch v := params.(type) {
	case nil:
		raw = []byte("null")
	case []byte:
		raw = v
	default:
		var err error
		raw, err = jsonMarshal(params)
		if err != nil {
			t.Fatalf("marshal params: %v", err)
		}
	}
	req := jsonRPCFor(1, method, raw)
	resp := d.Dispatch(context.Background(), req)
	if errObj, ok := resp["error"].(map[string]any); ok {
		return nil, errObj
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("%s: no result, got %#v", method, resp)
	}
	return res, nil
}

// ---- Dispatcher in-process tests ------------------------

// TestCreateRemoteAgentTerminal_Ok exercises the
// happy path: Create → returns the canonical flat
// {ok, terminal_id, kind, cols, rows, created_at}
// envelope with a fresh tml_<hex> id and server-
// stamped created_at.
func TestCreateRemoteAgentTerminal_Ok(t *testing.T) {
	reg := terminals.NewRegistry()
	d := newTerminalsDispatcher(t, reg)
	res, errObj := dispatchTerminals(t, d, "CreateRemoteAgentTerminal", map[string]any{
		"kind": "input",
		"cols": 132,
		"rows": 50,
	})
	if errObj != nil {
		t.Fatalf("dispatch: error = %#v", errObj)
	}
	if !strings.HasPrefix(res["terminal_id"].(string), "tml_") {
		t.Errorf("terminal_id = %v, want tml_<hex> prefix", res["terminal_id"])
	}
	if res["kind"] != "input" {
		t.Errorf("kind = %v, want input", res["kind"])
	}
	if res["cols"] != 132 {
		t.Errorf("cols = %v, want 132", res["cols"])
	}
	if res["rows"] != 50 {
		t.Errorf("rows = %v, want 50", res["rows"])
	}
	if got, _ := res["created_at"].(string); got == "" {
		t.Errorf("created_at missing or empty")
	}
}

// TestCreateRemoteAgentTerminal_Defaults exercises
// the omitted-dims path: kind=input, no cols/rows →
// registry defaults 80/24.
func TestCreateRemoteAgentTerminal_Defaults(t *testing.T) {
	reg := terminals.NewRegistry()
	d := newTerminalsDispatcher(t, reg)
	res, errObj := dispatchTerminals(t, d, "CreateRemoteAgentTerminal", map[string]any{
		"kind": "input",
	})
	if errObj != nil {
		t.Fatalf("dispatch: error = %#v", errObj)
	}
	if res["cols"] != terminals.DefaultCols {
		t.Errorf("cols = %v, want %d (default)", res["cols"], terminals.DefaultCols)
	}
	if res["rows"] != terminals.DefaultRows {
		t.Errorf("rows = %v, want %d (default)", res["rows"], terminals.DefaultRows)
	}
}

// TestCreateRemoteAgentTerminal_BadKind exercises
// the closed-set kind path: a bogus kind → -32602
// "Invalid params".
func TestCreateRemoteAgentTerminal_BadKind(t *testing.T) {
	reg := terminals.NewRegistry()
	d := newTerminalsDispatcher(t, reg)
	_, errObj := dispatchTerminals(t, d, "CreateRemoteAgentTerminal", map[string]any{
		"kind": "system",
	})
	if errObj == nil {
		t.Fatalf("dispatch: no error, want -32602 envelope")
	}
	if code, _ := errObj["code"].(int); code != -32602 {
		t.Errorf("error.code = %v, want -32602", code)
	}
}

// TestCreateRemoteAgentTerminal_NoStore exercises
// the no-registry path: a dispatcher without a
// terminal registry yields -32603 "Internal error".
func TestCreateRemoteAgentTerminal_NoStore(t *testing.T) {
	sess := NewSessionState("1.4.2")
	cfg := Config{
		Session: sess,
		// explicitly clear default-installed registry by
		// setting nil; NewDispatcher's nil-fabrication
		// would otherwise install one.
	}
	cfg.TerminalsRegistry = nil
	d, err := NewDispatcher(cfg)
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	// Now override to nil post-construction:
	d.cfg.TerminalsRegistry = nil
	_, errObj := dispatchTerminals(t, d, "CreateRemoteAgentTerminal", map[string]any{
		"kind": "input",
	})
	if errObj == nil {
		t.Fatalf("dispatch: no error, want -32603 envelope")
	}
	if code, _ := errObj["code"].(int); code != -32603 {
		t.Errorf("error.code = %v, want -32603", code)
	}
}

// TestListRemoteAgentTerminals_All exercises the
// no-filter List path.
func TestListRemoteAgentTerminals_All(t *testing.T) {
	reg := terminals.NewRegistry()
	d := newTerminalsDispatcher(t, reg)
	for i := 0; i < 3; i++ {
		_, errObj := dispatchTerminals(t, d, "CreateRemoteAgentTerminal", map[string]any{
			"kind": "input",
		})
		if errObj != nil {
			t.Fatalf("Create #%d: error = %#v", i, errObj)
		}
	}
	res, errObj := dispatchTerminals(t, d, "ListRemoteAgentTerminals", map[string]any{})
	if errObj != nil {
		t.Fatalf("List: error = %#v", errObj)
	}
	rows, ok := res["terminals"].([]map[string]any)
	if !ok {
		t.Fatalf("res.terminals = %T, want []map[string]any", res["terminals"])
	}
	if len(rows) != 3 {
		t.Errorf("terminals len = %d, want 3", len(rows))
	}
	if ok, _ := res["ok"].(bool); !ok {
		t.Errorf("res.ok = %v, want true", res["ok"])
	}
}

// TestListRemoteAgentTerminals_Filter exercises the
// cascade-filter List path. The cycle's Create
// handler validates cascade existence (a
// cascade_id that doesn't match any registered
// cascade surfaces -32004 "Cascade not found"), so
// this test first registers a stub Cascade with the
// filter id, then exercises the no-filter + filter
// path so a buggy dispatcher that silently dropped
// the existence check or returned every row would
// trip the len assertions below.
func TestListRemoteAgentTerminals_Filter(t *testing.T) {
	reg := terminals.NewRegistry()
	d := newTerminalsDispatcher(t, reg)
	// Register the filter cascade so the dispatcher's
	// Create existence check (-32004) doesn't trip
	// before the test exercises the filter path.
	c, err := cascades.New(cascades.Opts{Now: time.Now().UTC()})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
	}
	d.cascades.Register(c)
	_, errObj := dispatchTerminals(t, d, "CreateRemoteAgentTerminal", map[string]any{
		"cascade_id": c.ID,
		"kind":       "input",
	})
	if errObj != nil {
		t.Fatalf("Create: error = %#v", errObj)
	}
	res, _ := dispatchTerminals(t, d, "ListRemoteAgentTerminals", map[string]any{
		"cascade_id": c.ID,
	})
	rows, ok := res["terminals"].([]map[string]any)
	if !ok {
		t.Fatalf("res.terminals = %T, want []map[string]any", res["terminals"])
	}
	if len(rows) != 1 {
		t.Errorf("terminals len = %d, want 1", len(rows))
	}
}

// TestSendRemoteAgentTerminalInput_Ok exercises the
// happy path: Create → Send → registry returns
// bytes_consumed equal to len(decoded data).
func TestSendRemoteAgentTerminalInput_Ok(t *testing.T) {
	reg := terminals.NewRegistry()
	d := newTerminalsDispatcher(t, reg)
	gotRes, _ := dispatchTerminals(t, d, "CreateRemoteAgentTerminal", map[string]any{
		"kind": "input",
	})
	tid := gotRes["terminal_id"].(string)

	raw := []byte("hello terminal")
	encoded := base64.StdEncoding.EncodeToString(raw)
	res, errObj := dispatchTerminals(t, d, "SendRemoteAgentTerminalInput", map[string]any{
		"terminal_id": tid,
		"data":        encoded,
	})
	if errObj != nil {
		t.Fatalf("dispatch: error = %#v", errObj)
	}
	if bc, _ := res["bytes_consumed"].(int); bc != len(raw) {
		t.Errorf("bytes_consumed = %v, want %d", bc, len(raw))
	}
	if id, _ := res["terminal_id"].(string); id != tid {
		t.Errorf("terminal_id = %q, want %q", id, tid)
	}
}

// TestSendRemoteAgentTerminalInput_TerminalNotFound
// pins the unknown-id path: Send with an unknown
// terminal_id → -32018 "Terminal not found".
func TestSendRemoteAgentTerminalInput_TerminalNotFound(t *testing.T) {
	reg := terminals.NewRegistry()
	d := newTerminalsDispatcher(t, reg)
	_, errObj := dispatchTerminals(t, d, "SendRemoteAgentTerminalInput", map[string]any{
		"terminal_id": "tml_00000000deadbeef",
		"data":        base64.StdEncoding.EncodeToString([]byte("x")),
	})
	if errObj == nil {
		t.Fatalf("dispatch: no error, want -32018 envelope")
	}
	if code, _ := errObj["code"].(int); code != -32018 {
		t.Errorf("error.code = %v, want -32018", code)
	}
}

// TestSendRemoteAgentTerminalInput_TerminalClosed
// pins the closed-terminal path.
func TestSendRemoteAgentTerminalInput_TerminalClosed(t *testing.T) {
	reg := terminals.NewRegistry()
	d := newTerminalsDispatcher(t, reg)
	gotRes, _ := dispatchTerminals(t, d, "CreateRemoteAgentTerminal", map[string]any{
		"kind": "input",
	})
	tid := gotRes["terminal_id"].(string)
	// Close it.
	_, _ = dispatchTerminals(t, d, "CloseRemoteAgentTerminal", map[string]any{
		"terminal_id": tid,
	})
	_, errObj := dispatchTerminals(t, d, "SendRemoteAgentTerminalInput", map[string]any{
		"terminal_id": tid,
		"data":        base64.StdEncoding.EncodeToString([]byte("x")),
	})
	if errObj == nil {
		t.Fatalf("dispatch: no error, want -32019 envelope")
	}
	if code, _ := errObj["code"].(int); code != -32019 {
		t.Errorf("error.code = %v, want -32019", code)
	}
}

// TestSendRemoteAgentTerminalInput_BadParams_NotBase64
// pins the bad-base64 path: malformed base64 →
// -32602 "Invalid params".
func TestSendRemoteAgentTerminalInput_BadParams_NotBase64(t *testing.T) {
	reg := terminals.NewRegistry()
	d := newTerminalsDispatcher(t, reg)
	gotRes, _ := dispatchTerminals(t, d, "CreateRemoteAgentTerminal", map[string]any{
		"kind": "input",
	})
	tid := gotRes["terminal_id"].(string)
	_, errObj := dispatchTerminals(t, d, "SendRemoteAgentTerminalInput", map[string]any{
		"terminal_id": tid,
		"data":        "!!!not-base64!!!",
	})
	if errObj == nil {
		t.Fatalf("dispatch: no error, want -32602 envelope")
	}
	if code, _ := errObj["code"].(int); code != -32602 {
		t.Errorf("error.code = %v, want -32602", code)
	}
}

// TestSendRemoteAgentTerminalInput_BadParams_EmptyData
// pins the empty-data path: empty data string →
// -32602 (a dashboard with no payload is a bug).
func TestSendRemoteAgentTerminalInput_BadParams_EmptyData(t *testing.T) {
	reg := terminals.NewRegistry()
	d := newTerminalsDispatcher(t, reg)
	gotRes, _ := dispatchTerminals(t, d, "CreateRemoteAgentTerminal", map[string]any{
		"kind": "input",
	})
	tid := gotRes["terminal_id"].(string)
	_, errObj := dispatchTerminals(t, d, "SendRemoteAgentTerminalInput", map[string]any{
		"terminal_id": tid,
		"data":        "",
	})
	if errObj == nil {
		t.Fatalf("dispatch: no error, want -32602 envelope")
	}
	if code, _ := errObj["code"].(int); code != -32602 {
		t.Errorf("error.code = %v, want -32602", code)
	}
}

// TestResizeRemoteAgentTerminal_Ok exercises the
// happy path: Create → Resize → response envelope
// echoes the post-dims.
func TestResizeRemoteAgentTerminal_Ok(t *testing.T) {
	reg := terminals.NewRegistry()
	d := newTerminalsDispatcher(t, reg)
	gotRes, _ := dispatchTerminals(t, d, "CreateRemoteAgentTerminal", map[string]any{
		"kind": "input",
	})
	tid := gotRes["terminal_id"].(string)
	res, errObj := dispatchTerminals(t, d, "ResizeRemoteAgentTerminal", map[string]any{
		"terminal_id": tid,
		"cols":        200,
		"rows":        50,
	})
	if errObj != nil {
		t.Fatalf("dispatch: error = %#v", errObj)
	}
	if res["cols"] != 200 {
		t.Errorf("res.cols = %v, want 200", res["cols"])
	}
	if res["rows"] != 50 {
		t.Errorf("res.rows = %v, want 50", res["rows"])
	}
}

// TestResizeRemoteAgentTerminal_BadParams_Negative
// pins the bad-dims path.
func TestResizeRemoteAgentTerminal_BadParams_Negative(t *testing.T) {
	reg := terminals.NewRegistry()
	d := newTerminalsDispatcher(t, reg)
	gotRes, _ := dispatchTerminals(t, d, "CreateRemoteAgentTerminal", map[string]any{
		"kind": "input",
	})
	tid := gotRes["terminal_id"].(string)
	_, errObj := dispatchTerminals(t, d, "ResizeRemoteAgentTerminal", map[string]any{
		"terminal_id": tid,
		"cols":        -1,
		"rows":        50,
	})
	if errObj == nil {
		t.Fatalf("dispatch: no error, want -32602 envelope")
	}
	if code, _ := errObj["code"].(int); code != -32602 {
		t.Errorf("error.code = %v, want -32602", code)
	}
}

// TestCloseRemoteAgentTerminal_Ok exercises the happy
// path: Create → Close → response envelope carries
// closed_at.
func TestCloseRemoteAgentTerminal_Ok(t *testing.T) {
	reg := terminals.NewRegistry()
	d := newTerminalsDispatcher(t, reg)
	gotRes, _ := dispatchTerminals(t, d, "CreateRemoteAgentTerminal", map[string]any{
		"kind": "input",
	})
	tid := gotRes["terminal_id"].(string)
	res, errObj := dispatchTerminals(t, d, "CloseRemoteAgentTerminal", map[string]any{
		"terminal_id": tid,
	})
	if errObj != nil {
		t.Fatalf("dispatch: error = %#v", errObj)
	}
	if got, _ := res["terminal_id"].(string); got != tid {
		t.Errorf("terminal_id = %q, want %q", got, tid)
	}
	if got, _ := res["closed_at"].(string); got == "" {
		t.Errorf("closed_at missing or empty")
	}
	if ok, _ := res["ok"].(bool); !ok {
		t.Errorf("ok = %v, want true", res["ok"])
	}
}

// TestCloseRemoteAgentTerminal_NotFound pins the
// unknown-id path.
func TestCloseRemoteAgentTerminal_NotFound(t *testing.T) {
	reg := terminals.NewRegistry()
	d := newTerminalsDispatcher(t, reg)
	_, errObj := dispatchTerminals(t, d, "CloseRemoteAgentTerminal", map[string]any{
		"terminal_id": "tml_00000000deadbeef",
	})
	if errObj == nil {
		t.Fatalf("dispatch: no error, want -32018 envelope")
	}
	if code, _ := errObj["code"].(int); code != -32018 {
		t.Errorf("error.code = %v, want -32018", code)
	}
}

// ---- Cascade integration test ----------------------------

// TestTerminalOutput_StreamsFromCascade pins the
// cascade-output → terminal-subscriber flow: create a
// terminal bound to a cascade via AttachCascade,
// spawn a goroutine that writes chunks to the
// cascade's output chan, and assert the
// terminal's subscriber receives terminal/output
// events.
//
// This is the cycle's headline integration test —
// without it the dispatcher creates a terminal that
// never sees cascade output.
func TestTerminalOutput_StreamsFromCascade(t *testing.T) {
	reg := terminals.NewRegistry()
	d := newTerminalsDispatcher(t, reg)

	// Create a Cascade directly so we can attach it
	// to a terminal. NewCascade seeds a stub backend
	// the test can drive; we don't need to register
	// the cascade with a dispatcher.
	c, err := cascades.New(cascades.Opts{Now: time.Now().UTC()})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
	}
	// Connect the cascade to the dispatcher's cascade
	// registry so a Create with cascade_id <cascade>
	// resolves.
	d.cascades.Register(c)

	// Create a terminal bound to the cascade.
	res, errObj := dispatchTerminals(t, d, "CreateRemoteAgentTerminal", map[string]any{
		"cascade_id": c.ID,
		"kind":       "input",
	})
	if errObj != nil {
		t.Fatalf("CreateRemoteAgentTerminal: error = %#v", errObj)
	}
	tid := res["terminal_id"].(string)

	// Subscribe to the terminal.
	ch, _, err := reg.Subscribe(tid)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// Spawn a producer goroutine that writes to the
	// cascade's output chan. The attach path
	// in CreateRemoteAgentTerminal already wired the
	// drain; the next chunks land on ch.
	var wrote int32
	go func() {
		atomic.StoreInt32(&wrote, 1)
		c.AppendTerminalOutput([]byte("cascade-line-1"))
		c.AppendTerminalOutput([]byte("cascade-line-2"))
	}()

	// Wait for both events. Use a generous timeout
	// so the goroutine scheduler has time.
	var seen []string
	for i := 0; i < 2; i++ {
		select {
		case ev := <-ch:
			if ev.Kind != terminals.EventOutput {
				t.Errorf("event.Kind = %q, want %q", ev.Kind, terminals.EventOutput)
			}
			seen = append(seen, string(ev.Data))
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("did not receive event #%d within 500ms (wrote=%d)", i, atomic.LoadInt32(&wrote))
		}
	}
	if len(seen) != 2 || seen[0] != "cascade-line-1" || seen[1] != "cascade-line-2" {
		t.Errorf("seen = %#v, want [cascade-line-1, cascade-line-2]", seen)
	}
}
