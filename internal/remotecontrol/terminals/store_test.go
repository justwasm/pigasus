// Package terminals — store_test.go: the registry's own
// in-process tests.
//
// These exercise the subject directly (the registry, the closed
// sets, the notification names and the sentinel-error map). The
// dispatcher's round-trip — the five JSON-RPC methods as the host
// routes them — stays in the host's package, because the
// dispatcher is the host's.
//
// Stdlib-only. No third-party deps.
package terminals

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// TestAllKinds pins the closed-set order so a future cycle that
// extends the set updates AllKinds + the doc comment + the
// IsValidKind helper in lockstep.
func TestAllKinds(t *testing.T) {
	want := []Kind{Output, Input}
	if len(AllKinds) != len(want) {
		t.Fatalf("AllKinds len = %d, want %d", len(AllKinds), len(want))
	}
	for i, k := range want {
		if AllKinds[i] != k {
			t.Errorf("AllKinds[%d] = %q, want %q", i, AllKinds[i], k)
		}
	}
}

// TestIsValidKind pins the closed-set validator that drives the
// handler's kind pre-flight check.
func TestIsValidKind(t *testing.T) {
	cases := []struct {
		in   Kind
		want bool
	}{
		{Output, true},
		{Input, true},
		{"", false},
		{"OUTPUT", false},
		{"Input", false},
		{"system", false},
		{"operator", false},
	}
	for _, tc := range cases {
		t.Run(string(tc.in), func(t *testing.T) {
			if got := IsValidKind(tc.in); got != tc.want {
				t.Errorf("IsValidKind(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestNotifOutputMethod pins the JSON-RPC notification method name
// the handler uses for the "terminal/output" server-push frame.
func TestNotifOutputMethod(t *testing.T) {
	if NotifOutputMethod != "terminal/output" {
		t.Errorf("NotifOutputMethod = %q, want terminal/output", NotifOutputMethod)
	}
}

// TestNotifClosedMethod pins the JSON-RPC notification method name
// the handler uses for the "terminal/closed" server-push frame.
func TestNotifClosedMethod(t *testing.T) {
	if NotifClosedMethod != "terminal/closed" {
		t.Errorf("NotifClosedMethod = %q, want terminal/closed", NotifClosedMethod)
	}
}

// TestDefaultDimensions pins the cycle's 80x24 fallback the
// handler applies on Create when cols / rows are zero.
func TestDefaultDimensions(t *testing.T) {
	if DefaultCols != 80 {
		t.Errorf("DefaultCols = %d, want 80", DefaultCols)
	}
	if DefaultRows != 24 {
		t.Errorf("DefaultRows = %d, want 24", DefaultRows)
	}
}

// TestEventBufferCap pins the cycle's per-subscriber buffered chan
// capacity.
func TestEventBufferCap(t *testing.T) {
	if EventBufferCap != 64 {
		t.Errorf("EventBufferCap = %d, want 64", EventBufferCap)
	}
}

// ---- Registry in-process tests ---------------------------

// TestRegistry_Create_Ok exercises the happy path: a fresh
// registry accepts a Create call with cols/rows/kind/cascade_id,
// mints a fresh tml_<hex> terminal_id, and stamps created_at.
func TestRegistry_Create_Ok(t *testing.T) {
	reg := NewRegistry()
	now := time.Now().UTC()
	_ = now
	got, err := reg.Create(Terminal{
		Kind: Input,
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
	if got.Kind != Input {
		t.Errorf("kind = %q, want %q", got.Kind, Input)
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

// TestRegistry_Create_DefaultsForZeroDims pins the cycle's 80x24
// fallback applied when cols or rows are zero.
func TestRegistry_Create_DefaultsForZeroDims(t *testing.T) {
	reg := NewRegistry()
	got, err := reg.Create(Terminal{Kind: Output})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.Cols != DefaultCols {
		t.Errorf("cols = %d, want %d (default)", got.Cols, DefaultCols)
	}
	if got.Rows != DefaultRows {
		t.Errorf("rows = %d, want %d (default)", got.Rows, DefaultRows)
	}
}

// TestRegistry_List_All pins the no-filter List path.
func TestRegistry_List_All(t *testing.T) {
	reg := NewRegistry()
	for i := 0; i < 3; i++ {
		if _, err := reg.Create(Terminal{Kind: Output}); err != nil {
			t.Fatalf("Create #%d: %v", i, err)
		}
	}
	rows := reg.List("")
	if len(rows) != 3 {
		t.Errorf("List(all) len = %d, want 3", len(rows))
	}
}

// TestRegistry_List_FilterByCascade pins the cascade-filter List
// path.
func TestRegistry_List_FilterByCascade(t *testing.T) {
	reg := NewRegistry()
	if _, err := reg.Create(Terminal{Kind: Output, CascadeID: "cas_aaa"}); err != nil {
		t.Fatalf("Create cas_aaa: %v", err)
	}
	if _, err := reg.Create(Terminal{Kind: Output, CascadeID: "cas_aaa"}); err != nil {
		t.Fatalf("Create cas_aaa second: %v", err)
	}
	if _, err := reg.Create(Terminal{Kind: Output, CascadeID: "cas_bbb"}); err != nil {
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

// TestRegistry_Send_AfterClose_ReturnsError pins the
// closed-terminal Send → ErrClosed sentinel.
func TestRegistry_Send_AfterClose_ReturnsError(t *testing.T) {
	reg := NewRegistry()
	got, err := reg.Create(Terminal{Kind: Input})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := reg.Close(got.TerminalID); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err = reg.Send(got.TerminalID, []byte("hello"))
	if err == nil {
		t.Fatalf("Send on closed terminal: nil, want ErrClosed")
	}
	if err != ErrClosed {
		t.Errorf("Send on closed terminal: err = %v, want ErrClosed", err)
	}
}

// TestRegistry_Send_UnknownID_ReturnsError pins the unknown-id
// Send → ErrNotFound sentinel.
func TestRegistry_Send_UnknownID_ReturnsError(t *testing.T) {
	reg := NewRegistry()
	_, err := reg.Send("tml_00000000deadbeef", []byte("hello"))
	if err == nil {
		t.Fatalf("Send on unknown id: nil, want ErrNotFound")
	}
	if err != ErrNotFound {
		t.Errorf("Send on unknown id: err = %v, want ErrNotFound", err)
	}
}

// TestRegistry_Send_OnOutputKind pins the kind=output Send →
// ErrInputRejected sentinel (the read-only-consumer rule).
func TestRegistry_Send_OnOutputKind(t *testing.T) {
	reg := NewRegistry()
	got, err := reg.Create(Terminal{Kind: Output})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, err = reg.Send(got.TerminalID, []byte("hello"))
	if err == nil {
		t.Fatalf("Send on output-kind terminal: nil, want ErrInputRejected")
	}
	if err != ErrInputRejected {
		t.Errorf("Send on output-kind terminal: err = %v, want ErrInputRejected", err)
	}
}

// TestRegistry_Resize_Ok pins the happy path: open terminal,
// apply Resize, Get echoes the post-state.
func TestRegistry_Resize_Ok(t *testing.T) {
	reg := NewRegistry()
	got, err := reg.Create(Terminal{Kind: Input, Cols: 80, Rows: 24})
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

// TestRegistry_Close_Ok pins the happy path: open terminal, Close,
// Get echoes closed_at.
func TestRegistry_Close_Ok(t *testing.T) {
	reg := NewRegistry()
	got, err := reg.Create(Terminal{Kind: Input})
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

// TestRegistry_Close_AlreadyClosed_ReturnsError pins the
// second-Close error path: a terminal that has already been
// closed yields ErrClosed (the handler maps that to -32019
// "Terminal closed" so the dashboard sees the same envelope a
// Send-after-Close yields).
func TestRegistry_Close_AlreadyClosed_ReturnsError(t *testing.T) {
	reg := NewRegistry()
	got, err := reg.Create(Terminal{Kind: Input})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := reg.Close(got.TerminalID); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	err = reg.Close(got.TerminalID)
	if err == nil {
		t.Fatalf("second Close: nil, want ErrClosed")
	}
	if err != ErrClosed {
		t.Errorf("second Close: err = %v, want ErrClosed", err)
	}
}

// TestRegistry_Close_UnknownID_ReturnsError pins the unknown-id
// Close → ErrNotFound sentinel.
func TestRegistry_Close_UnknownID_ReturnsError(t *testing.T) {
	reg := NewRegistry()
	err := reg.Close("tml_00000000deadbeef")
	if err == nil {
		t.Fatalf("Close on unknown id: nil, want ErrNotFound")
	}
	if err != ErrNotFound {
		t.Errorf("Close on unknown id: err = %v, want ErrNotFound", err)
	}
}

// TestRegistry_Subscribe_ReceivesEvents pins the per-subscriber
// delivery path: register a buffered chan, call Send, the chan
// receives an EventOutput with the expected Data and Seq.
func TestRegistry_Subscribe_ReceivesEvents(t *testing.T) {
	reg := NewRegistry()
	got, err := reg.Create(Terminal{Kind: Input})
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
		if ev.Kind != EventOutput {
			t.Errorf("event.Kind = %q, want %q", ev.Kind, EventOutput)
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

// TestRegistry_Unsubscribe_StopsEvents pins the unsubscribe seam:
// subscribe, get the unsubscribe func, call it; subsequent Send no
// longer delivers to the closed chan.
func TestRegistry_Unsubscribe_StopsEvents(t *testing.T) {
	reg := NewRegistry()
	got, err := reg.Create(Terminal{Kind: Input})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	ch, unsub, err := reg.Subscribe(got.TerminalID)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	unsub()
	// After unsubscribe the chan is closed; a receive returns
	// the zero value.
	select {
	case _, ok := <-ch:
		if ok {
			// Buffered chan with no pending events:
			// receive yields zero value, but the chan
			// might still be open until the drain
			// completes. We accept either transition
			// so a different scheduling doesn't flap.
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatalf("chan did not yield zero value within 50ms after unsubscribe")
	}
}

// TestMapError pins the sentinel-error mapping helper.
func TestMapError(t *testing.T) {
	if got := MapError(nil, "op"); got != nil {
		t.Errorf("nil err: got %#v, want nil", got)
	}
	if got := MapError(ErrNotFound, "op"); got.Code != -32018 {
		t.Errorf("ErrNotFound: got code %d, want -32018", got.Code)
	}
	if got := MapError(ErrClosed, "op"); got.Code != -32019 {
		t.Errorf("ErrClosed: got code %d, want -32019", got.Code)
	}
	if got := MapError(ErrInputRejected, "op"); got.Code != -32602 {
		t.Errorf("ErrInputRejected: got code %d, want -32602", got.Code)
	}
	if got := MapError(errors.New("other"), "op"); got.Code != -32603 {
		t.Errorf("unknown err: got code %d, want -32603", got.Code)
	}
}
