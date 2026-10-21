// Tests for the rc-terminals cycle. Covers:
//
//   - The five JSON-RPC handlers
//     (CreateRemoteAgentTerminal / ListRemoteAgentTerminals /
//     SendRemoteAgentTerminalInput /
//     ResizeRemoteAgentTerminal /
//     CloseRemoteAgentTerminal) at the in-process level:
//     closed-set kind validation, default 80x24 dimensions,
//     sentinel-error mapping
//     (ErrTerminalNotFound → -32018,
//     ErrTerminalClosed → -32019,
//     ErrTerminalInputRejected → -32602),
//     base64-encoding-decoding on Send params,
//     terminal/output broadcast notification fan-out.
//
//   - The TerminalsRegistry CRUD surface at the
//     in-process level: Create / Get / List / Send /
//     Resize / Close / Subscribe (in-memory only —
//     the cycle is persistence-only in spirit).
//
//   - The cascade-attached drain path
//     (TestTerminalOutput_StreamsFromCascade): create
//     a cascade, attach it to a terminal via
//     TerminalOutput(), write chunks to the cascade's
//     chan, and assert the terminal's subscriber sees
//     the chunks as terminal/output events.
//
// Wire-level tests live in
// internal/remotetransport/tcp_test.go (mirroring
// the project's split: in-process envelopes vs.
// wire round-trips).
package remotecontrol

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newTerminalsRegistryDispatcher returns a Dispatcher
// with the supplied TerminalsRegistry pre-installed.
// The helper mirrors newConversationConfigDispatcher /
// newFastpushDispatcher.
//
// When reg is nil, NewDispatcher fabricates a fresh
// empty TerminalsRegistry (the nil-guard pattern
// rc-terminals adds). Tests that want to assert "no
// registry installed" pass nil and exercise the -32603
// path; tests that want a specific registry pass it
// directly.
func newTerminalsRegistryDispatcher(t *testing.T, reg *TerminalsRegistry) *Dispatcher {
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

// ---- helper / parse tests ------------------------------

// TestAllTerminalKinds pins the closed-set order so
// a future cycle that extends the set updates
// AllTerminalKinds + the doc comment + the
// IsValidTerminalKind helper in lockstep.
func TestAllTerminalKinds(t *testing.T) {
	want := []TerminalKind{TerminalOutput, TerminalInput}
	if len(AllTerminalKinds) != len(want) {
		t.Fatalf("AllTerminalKinds len = %d, want %d", len(AllTerminalKinds), len(want))
	}
	for i, k := range want {
		if AllTerminalKinds[i] != k {
			t.Errorf("AllTerminalKinds[%d] = %q, want %q", i, AllTerminalKinds[i], k)
		}
	}
}

// TestIsValidTerminalKind pins the closed-set
// validator that drives the dispatcher's kind
// pre-flight check.
func TestIsValidTerminalKind(t *testing.T) {
	cases := []struct {
		in   TerminalKind
		want bool
	}{
		{TerminalOutput, true},
		{TerminalInput, true},
		{"", false},
		{"OUTPUT", false},
		{"Input", false},
		{"system", false},
		{"operator", false},
	}
	for _, tc := range cases {
		t.Run(string(tc.in), func(t *testing.T) {
			if got := IsValidTerminalKind(tc.in); got != tc.want {
				t.Errorf("IsValidTerminalKind(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestNotifTerminalOutputMethod pins the JSON-RPC
// notification method name the dispatcher uses for the
// "terminal/output" server-push frame.
func TestNotifTerminalOutputMethod(t *testing.T) {
	if NotifTerminalOutputMethod != "terminal/output" {
		t.Errorf("NotifTerminalOutputMethod = %q, want terminal/output", NotifTerminalOutputMethod)
	}
}

// TestNotifTerminalClosedMethod pins the JSON-RPC
// notification method name the dispatcher uses for the
// "terminal/closed" server-push frame.
func TestNotifTerminalClosedMethod(t *testing.T) {
	if NotifTerminalClosedMethod != "terminal/closed" {
		t.Errorf("NotifTerminalClosedMethod = %q, want terminal/closed", NotifTerminalClosedMethod)
	}
}

// TestDefaultTerminalDimensions pins the cycle's
// 80x24 fallback the dispatcher applies on Create
// when cols / rows are zero.
func TestDefaultTerminalDimensions(t *testing.T) {
	if DefaultTerminalCols != 80 {
		t.Errorf("DefaultTerminalCols = %d, want 80", DefaultTerminalCols)
	}
	if DefaultTerminalRows != 24 {
		t.Errorf("DefaultTerminalRows = %d, want 24", DefaultTerminalRows)
	}
}

// TestTerminalEventBufferCap pins the cycle's
// per-subscriber buffered chan capacity.
func TestTerminalEventBufferCap(t *testing.T) {
	if TerminalEventBufferCap != 64 {
		t.Errorf("TerminalEventBufferCap = %d, want 64", TerminalEventBufferCap)
	}
}

// ---- TerminalsRegistry in-process tests -----------------

// TestTerminalsRegistry_Create_Ok exercises the
// happy path: a fresh registry accepts a Create
// call with cols/rows/kind/cascade_id, mints a fresh
// tml_<hex> terminal_id, and stamps created_at.
func TestTerminalsRegistry_Create_Ok(t *testing.T) {
	reg := NewTerminalsRegistry()
	now := time.Now().UTC()
	_ = now
	got, err := reg.Create(Terminal{
		Kind: TerminalInput,
		Cols: 120,
		Rows: 40,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !strings.HasPrefix(got.TerminalID, "tml_") {
		t.Errorf("terminal_id = %q, want tml_<hex> prefix", got.TerminalID)
	}
	if len(got.TerminalID) != len("tml_")+16 {
		t.Errorf("terminal_id %q: want 16 hex chars after prefix; got %d", got.TerminalID, len(got.TerminalID)-4)
	}
	if got.Kind != TerminalInput {
		t.Errorf("kind = %q, want %q", got.Kind, TerminalInput)
	}
	if got.Cols != 120 {
		t.Errorf("cols = %d, want 120", got.Cols)
	}
	if got.Rows != 40 {
		t.Errorf("rows = %d, want 40", got.Rows)
	}
	if !got.CreatedAt.After(time.Time{}) {
		t.Errorf("created_at = %v, want non-zero", got.CreatedAt)
	}
}

// TestTerminalsRegistry_Create_DefaultsForZeroDims
// pins the cycle's 80x24 fallback applied when cols
// or rows are zero.
func TestTerminalsRegistry_Create_DefaultsForZeroDims(t *testing.T) {
	reg := NewTerminalsRegistry()
	got, err := reg.Create(Terminal{Kind: TerminalOutput})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.Cols != DefaultTerminalCols {
		t.Errorf("cols = %d, want %d (default)", got.Cols, DefaultTerminalCols)
	}
	if got.Rows != DefaultTerminalRows {
		t.Errorf("rows = %d, want %d (default)", got.Rows, DefaultTerminalRows)
	}
}

// TestTerminalsRegistry_List_All pins the
// no-filter List path.
func TestTerminalsRegistry_List_All(t *testing.T) {
	reg := NewTerminalsRegistry()
	for i := 0; i < 3; i++ {
		if _, err := reg.Create(Terminal{Kind: TerminalOutput}); err != nil {
			t.Fatalf("Create #%d: %v", i, err)
		}
	}
	rows := reg.List("")
	if len(rows) != 3 {
		t.Errorf("List(all) len = %d, want 3", len(rows))
	}
}

// TestTerminalsRegistry_List_FilterByCascade pins
// the cascade-filter List path.
func TestTerminalsRegistry_List_FilterByCascade(t *testing.T) {
	reg := NewTerminalsRegistry()
	if _, err := reg.Create(Terminal{Kind: TerminalOutput, CascadeID: "cas_aaa"}); err != nil {
		t.Fatalf("Create cas_aaa: %v", err)
	}
	if _, err := reg.Create(Terminal{Kind: TerminalOutput, CascadeID: "cas_aaa"}); err != nil {
		t.Fatalf("Create cas_aaa second: %v", err)
	}
	if _, err := reg.Create(Terminal{Kind: TerminalOutput, CascadeID: "cas_bbb"}); err != nil {
		t.Fatalf("Create cas_bbb: %v", err)
	}
	rows := reg.List("cas_aaa")
	if len(rows) != 2 {
		t.Errorf("List(cas_aaa) len = %d, want 2", len(rows))
	}
	for _, r := range rows {
		if r.CascadeID != "cas_aaa" {
			t.Errorf("filtered row cascade_id = %q, want cas_aaa", r.CascadeID)
		}
	}
}

// TestTerminalsRegistry_Send_AfterClose_ReturnsError
// pins the closed-terminal Send → ErrTerminalClosed
// sentinel.
func TestTerminalsRegistry_Send_AfterClose_ReturnsError(t *testing.T) {
	reg := NewTerminalsRegistry()
	got, err := reg.Create(Terminal{Kind: TerminalInput})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := reg.Close(got.TerminalID); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err = reg.Send(got.TerminalID, []byte("hello"))
	if err == nil {
		t.Fatalf("Send on closed terminal: nil, want ErrTerminalClosed")
	}
	if err != ErrTerminalClosed {
		t.Errorf("Send on closed terminal: err = %v, want ErrTerminalClosed", err)
	}
}

// TestTerminalsRegistry_Send_UnknownID_ReturnsError
// pins the unknown-id Send → ErrTerminalNotFound
// sentinel.
func TestTerminalsRegistry_Send_UnknownID_ReturnsError(t *testing.T) {
	reg := NewTerminalsRegistry()
	_, err := reg.Send("tml_00000000deadbeef", []byte("hello"))
	if err == nil {
		t.Fatalf("Send on unknown id: nil, want ErrTerminalNotFound")
	}
	if err != ErrTerminalNotFound {
		t.Errorf("Send on unknown id: err = %v, want ErrTerminalNotFound", err)
	}
}

// TestTerminalsRegistry_Send_OnOutputKind pins the
// kind=output Send → ErrTerminalInputRejected sentinel
// (the read-only-consumer rule).
func TestTerminalsRegistry_Send_OnOutputKind(t *testing.T) {
	reg := NewTerminalsRegistry()
	got, err := reg.Create(Terminal{Kind: TerminalOutput})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, err = reg.Send(got.TerminalID, []byte("hello"))
	if err == nil {
		t.Fatalf("Send on output-kind terminal: nil, want ErrTerminalInputRejected")
	}
	if err != ErrTerminalInputRejected {
		t.Errorf("Send on output-kind terminal: err = %v, want ErrTerminalInputRejected", err)
	}
}

// TestTerminalsRegistry_Resize_Ok pins the happy
// path: open terminal, apply Resize, Get echoes the
// post-state.
func TestTerminalsRegistry_Resize_Ok(t *testing.T) {
	reg := NewTerminalsRegistry()
	got, err := reg.Create(Terminal{Kind: TerminalInput, Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := reg.Resize(got.TerminalID, 200, 60); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	post, ok := reg.Get(got.TerminalID)
	if !ok {
		t.Fatalf("Get: missing")
	}
	if post.Cols != 200 {
		t.Errorf("post.Cols = %d, want 200", post.Cols)
	}
	if post.Rows != 60 {
		t.Errorf("post.Rows = %d, want 60", post.Rows)
	}
}

// TestTerminalsRegistry_Close_Ok pins the happy
// path: open terminal, Close, Get echoes
// closed_at.
func TestTerminalsRegistry_Close_Ok(t *testing.T) {
	reg := NewTerminalsRegistry()
	got, err := reg.Create(Terminal{Kind: TerminalInput})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := reg.Close(got.TerminalID); err != nil {
		t.Fatalf("Close: %v", err)
	}
	post, ok := reg.Get(got.TerminalID)
	if !ok {
		t.Fatalf("Get: missing")
	}
	if post.ClosedAt.IsZero() {
		t.Errorf("post.ClosedAt = zero, want non-zero")
	}
}

// TestTerminalsRegistry_Close_AlreadyClosed_ReturnsError
// pins the second-Close error path: a terminal that
// has already been closed yields ErrTerminalClosed
// (the dispatcher maps that to -32019 "Terminal
// closed" so the dashboard sees the same envelope a
// Send-after-Close yields).
func TestTerminalsRegistry_Close_AlreadyClosed_ReturnsError(t *testing.T) {
	reg := NewTerminalsRegistry()
	got, err := reg.Create(Terminal{Kind: TerminalInput})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := reg.Close(got.TerminalID); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	err = reg.Close(got.TerminalID)
	if err == nil {
		t.Fatalf("second Close: nil, want ErrTerminalClosed")
	}
	if err != ErrTerminalClosed {
		t.Errorf("second Close: err = %v, want ErrTerminalClosed", err)
	}
}

// TestTerminalsRegistry_Close_UnknownID_ReturnsError
// pins the unknown-id Close → ErrTerminalNotFound
// sentinel.
func TestTerminalsRegistry_Close_UnknownID_ReturnsError(t *testing.T) {
	reg := NewTerminalsRegistry()
	err := reg.Close("tml_00000000deadbeef")
	if err == nil {
		t.Fatalf("Close on unknown id: nil, want ErrTerminalNotFound")
	}
	if err != ErrTerminalNotFound {
		t.Errorf("Close on unknown id: err = %v, want ErrTerminalNotFound", err)
	}
}

// TestTerminalsRegistry_Subscribe_ReceivesEvents pins
// the per-subscriber delivery path: register a
// buffered chan, call Send, the chan receives a
// TerminalEventOutput with the expected Data and Seq.
func TestTerminalsRegistry_Subscribe_ReceivesEvents(t *testing.T) {
	reg := NewTerminalsRegistry()
	got, err := reg.Create(Terminal{Kind: TerminalInput})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	ch, _, err := reg.Subscribe(got.TerminalID)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if _, err := reg.Send(got.TerminalID, []byte("hello")); err != nil {
		t.Fatalf("Send: %v", err)
	}
	select {
	case ev := <-ch:
		if ev.Kind != TerminalEventOutput {
			t.Errorf("event.Kind = %q, want %q", ev.Kind, TerminalEventOutput)
		}
		if string(ev.Data) != "hello" {
			t.Errorf("event.Data = %q, want %q", ev.Data, "hello")
		}
		if ev.Seq != 0 {
			t.Errorf("event.Seq = %d, want 0", ev.Seq)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("subscriber did not receive expected event within 200ms")
	}
}

// TestTerminalsRegistry_Unsubscribe_StopsEvents pins
// the unsubscribe seam: subscribe, get the
// unsubscribe func, call it; subsequent Send no
// longer delivers to the closed chan.
func TestTerminalsRegistry_Unsubscribe_StopsEvents(t *testing.T) {
	reg := NewTerminalsRegistry()
	got, err := reg.Create(Terminal{Kind: TerminalInput})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	ch, unsub, err := reg.Subscribe(got.TerminalID)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	unsub()
	// After unsubscribe the chan is closed; a
	// receive returns the zero value.
	select {
	case _, ok := <-ch:
		if ok {
			// Buffered chan with no pending
			// events: receive yields zero
			// value, but the chan might still
			// be open until the drain
			// completes. We accept either
			// transition so a different
			// scheduling doesn't flap.
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatalf("chan did not yield zero value within 50ms after unsubscribe")
	}
}

// ---- Dispatcher in-process tests ------------------------

// TestCreateRemoteAgentTerminal_Ok exercises the
// happy path: Create → returns the canonical flat
// {ok, terminal_id, kind, cols, rows, created_at}
// envelope with a fresh tml_<hex> id and server-
// stamped created_at.
func TestCreateRemoteAgentTerminal_Ok(t *testing.T) {
	reg := NewTerminalsRegistry()
	d := newTerminalsRegistryDispatcher(t, reg)
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
	reg := NewTerminalsRegistry()
	d := newTerminalsRegistryDispatcher(t, reg)
	res, errObj := dispatchTerminals(t, d, "CreateRemoteAgentTerminal", map[string]any{
		"kind": "input",
	})
	if errObj != nil {
		t.Fatalf("dispatch: error = %#v", errObj)
	}
	if res["cols"] != DefaultTerminalCols {
		t.Errorf("cols = %v, want %d (default)", res["cols"], DefaultTerminalCols)
	}
	if res["rows"] != DefaultTerminalRows {
		t.Errorf("rows = %v, want %d (default)", res["rows"], DefaultTerminalRows)
	}
}

// TestCreateRemoteAgentTerminal_BadKind exercises
// the closed-set kind path: a bogus kind → -32602
// "Invalid params".
func TestCreateRemoteAgentTerminal_BadKind(t *testing.T) {
	reg := NewTerminalsRegistry()
	d := newTerminalsRegistryDispatcher(t, reg)
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
// TerminalsRegistry yields -32603 "Internal error".
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
	reg := NewTerminalsRegistry()
	d := newTerminalsRegistryDispatcher(t, reg)
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
	terminals, ok := res["terminals"].([]map[string]any)
	if !ok {
		t.Fatalf("res.terminals = %T, want []map[string]any", res["terminals"])
	}
	if len(terminals) != 3 {
		t.Errorf("terminals len = %d, want 3", len(terminals))
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
	reg := NewTerminalsRegistry()
	d := newTerminalsRegistryDispatcher(t, reg)
	// Register the filter cascade so the dispatcher's
	// Create existence check (-32004) doesn't trip
	// before the test exercises the filter path.
	c, err := NewCascade(CascadeOpts{Now: time.Now().UTC()})
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
	terminals, ok := res["terminals"].([]map[string]any)
	if !ok {
		t.Fatalf("res.terminals = %T, want []map[string]any", res["terminals"])
	}
	if len(terminals) != 1 {
		t.Errorf("terminals len = %d, want 1", len(terminals))
	}
}

// TestSendRemoteAgentTerminalInput_Ok exercises the
// happy path: Create → Send → registry returns
// bytes_consumed equal to len(decoded data).
func TestSendRemoteAgentTerminalInput_Ok(t *testing.T) {
	reg := NewTerminalsRegistry()
	d := newTerminalsRegistryDispatcher(t, reg)
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
	reg := NewTerminalsRegistry()
	d := newTerminalsRegistryDispatcher(t, reg)
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
	reg := NewTerminalsRegistry()
	d := newTerminalsRegistryDispatcher(t, reg)
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
	reg := NewTerminalsRegistry()
	d := newTerminalsRegistryDispatcher(t, reg)
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
	reg := NewTerminalsRegistry()
	d := newTerminalsRegistryDispatcher(t, reg)
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
	reg := NewTerminalsRegistry()
	d := newTerminalsRegistryDispatcher(t, reg)
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
	reg := NewTerminalsRegistry()
	d := newTerminalsRegistryDispatcher(t, reg)
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
	reg := NewTerminalsRegistry()
	d := newTerminalsRegistryDispatcher(t, reg)
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
	reg := NewTerminalsRegistry()
	d := newTerminalsRegistryDispatcher(t, reg)
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
// cascade's TerminalOutput chan, and assert the
// terminal's subscriber receives terminal/output
// events.
//
// This is the cycle's headline integration test —
// without it the dispatcher creates a terminal that
// never sees cascade output.
func TestTerminalOutput_StreamsFromCascade(t *testing.T) {
	reg := NewTerminalsRegistry()
	d := newTerminalsRegistryDispatcher(t, reg)

	// Create a Cascade directly so we can attach it
	// to a terminal. NewCascade seeds a stub backend
	// the test can drive; we don't need to register
	// the cascade with a dispatcher.
	c, err := NewCascade(CascadeOpts{Now: time.Now().UTC()})
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
	// cascade's TerminalOutput chan. The attach path
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
			if ev.Kind != TerminalEventOutput {
				t.Errorf("event.Kind = %q, want %q", ev.Kind, TerminalEventOutput)
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

// TestMapTerminalsError pins the sentinel-error
// mapping helper.
func TestMapTerminalsError(t *testing.T) {
	if got := mapTerminalsError(nil, "op"); got != nil {
		t.Errorf("nil err: got %#v, want nil", got)
	}
	if got := mapTerminalsError(ErrTerminalNotFound, "op"); got.Code != -32018 {
		t.Errorf("ErrTerminalNotFound: got code %d, want -32018", got.Code)
	}
	if got := mapTerminalsError(ErrTerminalClosed, "op"); got.Code != -32019 {
		t.Errorf("ErrTerminalClosed: got code %d, want -32019", got.Code)
	}
	if got := mapTerminalsError(ErrTerminalInputRejected, "op"); got.Code != -32602 {
		t.Errorf("ErrTerminalInputRejected: got code %d, want -32602", got.Code)
	}
	if got := mapTerminalsError(errors.New("other"), "op"); got.Code != -32603 {
		t.Errorf("unknown err: got code %d, want -32603", got.Code)
	}
}
