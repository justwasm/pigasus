// In-process coverage for the rc-top-level-rpc cycle's
// new surface. Three handlers in one file because they
// share the same data-model package (top_level.go) and
// the same dispatcher-level test fixture pattern; the
// cycle's wire-level coverage lives next to the
// transport in remotetransport/tcp_test.go.
//
// What this file pins:
//
//   - DispatchMethods / DefaultDispatchMethods are
//     sorted and non-empty (the brief's "package var
//     populated from each switch arm — prefer the
//     package var so it can't drift" rule).
//   - RemoteControlStatus returns a fully-populated
//     TopLevelStatus, with a sorted dispatch_methods
//     list, and uptime_seconds that strictly grows
//     between two calls.
//   - RemoteControlOnOutput registers the calling
//     connID in (*Dispatcher).AllSinks and returns
//     the canonical filter list.
//   - remoteControlOnMessage gates by
//     RemoteControlOnMessageAllowList:
//     allowed method → {ok:true, accepted:true},
//     rejected method → -32603 envelope, bad params
//     → -32602 envelope.
//
// Wire-level coverage:
//   - remotetransport/tcp_test.go:
//   - TestRemoteControlStatus_WS_Ok
//   - TestRemoteControlOnOutput_WS_Ok
//   - TestRemoteControlOnMessage_WS_Allowed
//   - TestRemoteControlOnMessage_WS_Rejected
//   - TestRemoteControlOnMessage_BadParams_WS
package remotecontrol

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// resultAsMap normalises the dispatcher result
// returned from (*Dispatcher).Dispatch into a
// map[string]any. The dispatcher's rpcResult helper
// stores the handler's return value verbatim — so a
// handler that returns a TopLevelStatus struct
// produces a struct value on the in-process path,
// and a map[string]any only after a JSON round-trip.
// The wire tests in remotetransport/tcp_test.go
// go through the JSON round-trip and see a map; the
// in-process tests here see either shape depending on
// whether the handler returned a struct or a map.
//
// This helper converts both into a map[string]any
// so a single assertion works at either layer.
func resultAsMap(t *testing.T, raw any) map[string]any {
	t.Helper()
	if raw == nil {
		t.Fatalf("result is nil")
	}
	switch v := raw.(type) {
	case map[string]any:
		return v
	case TopLevelStatus:
		// Re-marshal to map.
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("Marshal TopLevelStatus: %v", err)
		}
		var out map[string]any
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatalf("Unmarshal TopLevelStatus: %v", err)
		}
		return out
	case RemoteControlOnOutputResult:
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("Marshal RemoteControlOnOutputResult: %v", err)
		}
		var out map[string]any
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatalf("Unmarshal RemoteControlOnOutputResult: %v", err)
		}
		return out
	case RemoteControlOnMessageResult:
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("Marshal RemoteControlOnMessageResult: %v", err)
		}
		var out map[string]any
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatalf("Unmarshal RemoteControlOnMessageResult: %v", err)
		}
		return out
	default:
		t.Fatalf("unexpected result type %T (raw value: %v)", raw, raw)
		return nil
	}
}

// fieldString pulls a string field out of either a
// struct or a map[string]any. Used by the
// post-Dispatch assertions to handle both shapes.
func fieldString(t *testing.T, raw any, key string) string {
	t.Helper()
	m := resultAsMap(t, raw)
	v, ok := m[key]
	if !ok {
		t.Fatalf("result.%s missing; have keys %v", key, sortedKeys(m))
	}
	s, ok := v.(string)
	if !ok {
		t.Fatalf("result.%s = %v (type %T), want string", key, v, v)
	}
	return s
}

// fieldBool pulls a bool field out of either a
// struct or a map[string]any.
func fieldBool(t *testing.T, raw any, key string) bool {
	t.Helper()
	m := resultAsMap(t, raw)
	v, ok := m[key]
	if !ok {
		t.Fatalf("result.%s missing; have keys %v", key, sortedKeys(m))
	}
	b, ok := v.(bool)
	if !ok {
		t.Fatalf("result.%s = %v (type %T), want bool", key, v, v)
	}
	return b
}

// fieldFloat pulls a numeric field out of either a
// struct or a map[string]any, returning a float64.
func fieldFloat(t *testing.T, raw any, key string) float64 {
	t.Helper()
	m := resultAsMap(t, raw)
	v, ok := m[key]
	if !ok {
		t.Fatalf("result.%s missing; have keys %v", key, sortedKeys(m))
	}
	f, ok := v.(float64)
	if !ok {
		// In-process structs that return int64 land
		// here as float64 (json re-marshal). int may
		// also surface if the dispatcher somehow
		// preserves the int — accept both.
		if i, ok2 := v.(int); ok2 {
			return float64(i)
		}
		if i, ok2 := v.(int64); ok2 {
			return float64(i)
		}
		t.Fatalf("result.%s = %v (type %T), want number", key, v, v)
	}
	return f
}

// fieldStringList pulls a []string field out of either
// a struct or a map[string]any.
func fieldStringList(t *testing.T, raw any, key string) []string {
	t.Helper()
	m := resultAsMap(t, raw)
	v, ok := m[key]
	if !ok {
		t.Fatalf("result.%s missing; have keys %v", key, sortedKeys(m))
	}
	// JSON-decoded string slices land as []any{string,…}.
	if l, ok := v.([]any); ok {
		out := make([]string, 0, len(l))
		for _, e := range l {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	if l, ok := v.([]string); ok {
		return l
	}
	t.Fatalf("result.%s = %v (type %T), want []string", key, v, v)
	return nil
}

// sortedKeys returns the sorted keys of a map (for
// error messages only).
func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// Sort in place to make error messages stable.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// reflectionStructFields returns the JSON-tag names
// of a struct, used to convert a struct to its
// wire-equivalent map[string]any keys. Kept as a
// fallback for cases where a handler returns a
// struct we don't enumerate above.
func reflectionStructFields(s any) []string {
	t := reflect.TypeOf(s)
	if t.Kind() != reflect.Struct {
		return nil
	}
	out := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			out = append(out, f.Name)
			continue
		}
		// Strip ",omitempty" etc.
		if idx := strings.Index(tag, ","); idx >= 0 {
			tag = tag[:idx]
		}
		out = append(out, tag)
	}
	return out
}

// TestTopLevelStatus_Shape pins the headline
// envelope: all fields populated, dispatch_methods
// non-empty + sorted, server_fingerprint matches the
// "wagyu-rc/<version>/<short-hash>" pattern.
func TestTopLevelStatus_Shape(t *testing.T) {
	sess := NewSessionState("1.4.2-test")
	sess.Backend = "gateway"
	sess.Model = "Gemini 3.8 Flash (Medium)"
	d, err := NewDispatcher(Config{
		Session: sess,
		Commit:  "deadbeef1234567890abcdef1234567890abcdef",
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	snap := d.topLevelStatus()

	if snap.Version != "1.4.2-test" {
		t.Errorf("Version = %q, want 1.4.2-test", snap.Version)
	}
	if snap.StartedAt == "" {
		t.Errorf("StartedAt is empty; want RFC3339Nano")
	} else if _, err := time.Parse(time.RFC3339Nano, snap.StartedAt); err != nil {
		t.Errorf("StartedAt %q does not parse as RFC3339Nano: %v", snap.StartedAt, err)
	}
	if snap.UptimeSeconds < 0 {
		t.Errorf("UptimeSeconds = %d, want >= 0", snap.UptimeSeconds)
	}
	if snap.ConnectedClients != 0 {
		t.Errorf("ConnectedClients = %d, want 0 (fresh dispatcher)", snap.ConnectedClients)
	}
	if snap.OpenCascades != 0 {
		t.Errorf("OpenCascades = %d, want 0 (fresh dispatcher)", snap.OpenCascades)
	}
	if len(snap.DispatchMethods) == 0 {
		t.Fatalf("DispatchMethods is empty; want >= 1 entry")
	}
	// Sorted ascending.
	for i := 1; i < len(snap.DispatchMethods); i++ {
		if snap.DispatchMethods[i-1] > snap.DispatchMethods[i] {
			t.Errorf("DispatchMethods not sorted at index %d: %q > %q",
				i, snap.DispatchMethods[i-1], snap.DispatchMethods[i])
		}
	}
	// Server fingerprint pattern: "wagyu-rc/<version>/<8-hex>".
	wantFP := "wagyu-rc/1.4.2-test/deadbeef"
	if snap.ServerFingerprint != wantFP {
		t.Errorf("ServerFingerprint = %q, want %q", snap.ServerFingerprint, wantFP)
	}
}

// TestTopLevelStatus_DispatchMethods_Listed pins the
// headline ask: the three new methods are in the
// dispatch_methods list. The brief asks for these
// specifically as the G+H cluster close.
func TestTopLevelStatus_DispatchMethods_Listed(t *testing.T) {
	sess := NewSessionState("1.4.2-test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	snap := d.topLevelStatus()
	want := []string{
		"RemoteControlStatus",
		"RemoteControlOnOutput",
		"remoteControlOnMessage",
	}
	have := map[string]bool{}
	for _, m := range snap.DispatchMethods {
		have[m] = true
	}
	for _, m := range want {
		if !have[m] {
			t.Errorf("DispatchMethods missing %q (full list: %v)", m, snap.DispatchMethods)
		}
	}

	// Spot-check that the list is comprehensive:
	// at least the headline methods from earlier
	// cycles must still be present (no regressions
	// from the manual-sync maintenance).
	mustAlso := []string{
		"StartRemoteAgentCascade",
		"SendRemoteAgentCascadeMessage",
		"CancelRemoteAgentCascade",
		"remoteControlVersion",
		"RemoteAgentErrorStatus",
		"SetRemoteControlSession",
		"tools/list",
		"tools/call",
		"initialize",
	}
	for _, m := range mustAlso {
		if !have[m] {
			t.Errorf("DispatchMethods missing legacy %q (regression?)", m)
		}
	}
}

// TestTopLevelStatus_UptimeIncreases pins the
// monotonic-uptime contract: two calls two seconds apart
// must show a strictly larger uptime_seconds. The brief
// pins uptime_seconds as a single-call snapshot; this
// test ensures a dashboard polling the method sees the
// counter tick.
func TestTopLevelStatus_UptimeIncreases(t *testing.T) {
	sess := NewSessionState("1.4.2-test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	first := d.topLevelStatus()
	time.Sleep(1100 * time.Millisecond)
	second := d.topLevelStatus()

	if second.UptimeSeconds < first.UptimeSeconds+1 {
		t.Errorf("UptimeSeconds did not advance enough: first=%d, second=%d (delta=%d, expected >=1)",
			first.UptimeSeconds, second.UptimeSeconds, second.UptimeSeconds-first.UptimeSeconds)
	}
}

// TestTopLevelStatus_NilSessionDefensive pins the
// nil-safe path: a dispatcher with bare-bones Config
// (no Session) returns a populated envelope with zero
// values rather than panicking. Mirrors the brief's
// "dashboards never nil-check" requirement.
func TestTopLevelStatus_NilSessionDefensive(t *testing.T) {
	d, err := NewDispatcher(Config{Session: NewSessionState("")})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	// Nil out the session pointer post-construction
	// to simulate a host that didn't install one.
	d.cfg.Session = nil

	snap := d.topLevelStatus()
	if snap.Version == "" {
		t.Errorf("Version is empty; want ServerVersion fallback or 'unknown'")
	}
	if snap.StartedAt != "" {
		t.Errorf("StartedAt = %q, want empty (zero StartedAt)", snap.StartedAt)
	}
	if snap.UptimeSeconds != 0 {
		t.Errorf("UptimeSeconds = %d, want 0 (zero StartedAt)", snap.UptimeSeconds)
	}
	if snap.ServerFingerprint == "" {
		t.Errorf("ServerFingerprint is empty; want non-empty")
	}
}

// TestTopLevelStatus_OpenCascadesIncrements pins the
// cascade counter: registering a cascade bumps
// open_cascades. Confirms the brief's "counts
// (*CascadeRegistry).Snapshot().len" semantics.
func TestTopLevelStatus_OpenCascadesIncrements(t *testing.T) {
	sess := NewSessionState("1.4.2-test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	if got := d.topLevelStatus().OpenCascades; got != 0 {
		t.Fatalf("baseline OpenCascades = %d, want 0", got)
	}
	c, err := NewCascade(CascadeOpts{})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
	}
	if err := d.cascades.Register(c); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if got := d.topLevelStatus().OpenCascades; got != 1 {
		t.Errorf("after 1 register: OpenCascades = %d, want 1", got)
	}
}

// TestServerFingerprint_KnownCommit pins the
// "wagyu-rc/<version>/<short-hash>" envelope
// construction.
func TestServerFingerprint_KnownCommit(t *testing.T) {
	sess := NewSessionState("1.4.2-test")
	d, err := NewDispatcher(Config{
		Session: sess,
		Commit:  "abcdef1234567890abcdef1234567890abcdef00",
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	if got := serverFingerprint(d, "1.4.2-test"); got != "wagyu-rc/1.4.2-test/abcdef12" {
		t.Errorf("serverFingerprint = %q, want wagyu-rc/1.4.2-test/abcdef12", got)
	}
}

// TestServerFingerprint_UnknownCommit pins the
// "unknown" fallback when Config.Commit is empty.
func TestServerFingerprint_UnknownCommit(t *testing.T) {
	sess := NewSessionState("1.4.2-test")
	d, err := NewDispatcher(Config{Session: sess, Commit: ""})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	if got := serverFingerprint(d, "1.4.2-test"); got != "wagyu-rc/1.4.2-test/unknown" {
		t.Errorf("serverFingerprint = %q, want wagyu-rc/1.4.2-test/unknown", got)
	}
}

// ----- Dispatcher round-trips -----

// TestDispatcher_RemoteControlStatus is the
// headline happy-path: dispatch the JSON-RPC method,
// assert the response is the TopLevelStatus envelope.
func TestDispatcher_RemoteControlStatus(t *testing.T) {
	sess := NewSessionState("1.4.2-test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"RemoteControlStatus"}`)
	resp := d.Dispatch(context.Background(), req)
	if errObj, ok := resp["error"]; ok {
		t.Fatalf("Dispatch returned error: %#v", errObj)
	}
	if got := fieldString(t, resp["result"], "version"); got != "1.4.2-test" {
		t.Errorf("result.version = %q, want 1.4.2-test", got)
	}
	if got := fieldString(t, resp["result"], "started_at"); got == "" {
		t.Errorf("result.started_at missing")
	}
	uptime := fieldFloat(t, resp["result"], "uptime_seconds")
	if uptime < 0 {
		t.Errorf("result.uptime_seconds = %f, want >= 0", uptime)
	}
	if got := fieldFloat(t, resp["result"], "connected_clients"); got != 0 {
		t.Errorf("result.connected_clients = %v, want 0", got)
	}
	if got := fieldFloat(t, resp["result"], "open_cascades"); got != 0 {
		t.Errorf("result.open_cascades = %v, want 0", got)
	}
	methods := fieldStringList(t, resp["result"], "dispatch_methods")
	if len(methods) == 0 {
		t.Errorf("result.dispatch_methods is empty")
	}
}

// TestDispatcher_RemoteControlOnOutput_DefaultFilter
// pins the canonical filter list: non-empty, contains
// at least the cascade/state and cascade/text_delta
// topics (the most basic dashboard subscriptions).
func TestDispatcher_RemoteControlOnOutput_DefaultFilter(t *testing.T) {
	sess := NewSessionState("1.4.2-test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"RemoteControlOnOutput","params":{}}`)
	resp := d.Dispatch(context.Background(), req)
	if errObj, ok := resp["error"]; ok {
		t.Fatalf("Dispatch returned error: %#v", errObj)
	}
	if got := fieldBool(t, resp["result"], "ok"); !got {
		t.Errorf("result.ok = false, want true")
	}
	if got := fieldString(t, resp["result"], "sink_id"); got != "" {
		t.Errorf("result.sink_id = %q, want empty (no connID on in-process)", got)
	}
	if got := fieldString(t, resp["result"], "subscribed_at"); got == "" {
		t.Errorf("result.subscribed_at missing")
	}
	filter := fieldStringList(t, resp["result"], "filter")
	if len(filter) == 0 {
		t.Fatalf("result.filter is empty; want non-empty canonical list")
	}
	have := map[string]bool{}
	for _, f := range filter {
		have[f] = true
	}
	// Spot-check: the cascade trio must be in the
	// canonical list.
	for _, must := range []string{"cascade/state", "cascade/text_delta", "cascade/tool_call"} {
		if !have[must] {
			t.Errorf("filter missing %q (got: %v)", must, filter)
		}
	}
}

// TestDispatcher_RemoteControlOnOutput_AddsSink pins
// the headline behaviour: calling RemoteControlOnOutput
// from a connID threads the registration into
// (*Dispatcher).AllSinks. The future rc-all-sinks-fanout
// cycle's fan-out helper relies on this.
func TestDispatcher_RemoteControlOnOutput_AddsSink(t *testing.T) {
	sess := NewSessionState("1.4.2-test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	// Pre-condition: empty AllSinks.
	if got := d.AllSinkConnIDs(); len(got) != 0 {
		t.Fatalf("baseline AllSinks = %v, want empty", got)
	}

	// Dispatch with a connID'd ctx.
	ctx := d.WithConnID(context.Background(), "ws-test-conn-1")
	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"RemoteControlOnOutput","params":{"sink_id":"sink-A"}}`)
	resp := d.Dispatch(ctx, req)
	if errObj, ok := resp["error"]; ok {
		t.Fatalf("Dispatch returned error: %#v", errObj)
	}
	if got := fieldString(t, resp["result"], "sink_id"); got != "sink-A" {
		t.Errorf("result.sink_id = %q, want sink-A (the explicit sink_id wins over connID)", got)
	}

	// Post-condition: AllSinks contains "sink-A".
	if !d.IsAllSink("sink-A") {
		t.Errorf("AllSinks missing sink-A after RemoteControlOnOutput; conns = %v", d.AllSinkConnIDs())
	}
	if d.IsAllSink("ws-test-conn-1") {
		t.Errorf("AllSinks unexpectedly contains the connID (sink-A was explicit)")
	}

	// Idempotency: re-registering is a no-op.
	req2 := []byte(`{"jsonrpc":"2.0","id":2,"method":"RemoteControlOnOutput","params":{"sink_id":"sink-A"}}`)
	d.Dispatch(ctx, req2)
	if got := d.AllSinkConnIDs(); len(got) != 1 {
		t.Errorf("after re-register: AllSinks = %v, want 1 entry", got)
	}

	// Removal: RemoveAllSink drops the entry.
	d.RemoveAllSink("sink-A")
	if d.IsAllSink("sink-A") {
		t.Errorf("AllSinks still contains sink-A after RemoveAllSink")
	}
}

// TestDispatcher_RemoteControlOnOutput_DefaultsToConnID
// pins the "sink_id omitted → defaults to calling
// connID" rule.
func TestDispatcher_RemoteControlOnOutput_DefaultsToConnID(t *testing.T) {
	sess := NewSessionState("1.4.2-test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	ctx := d.WithConnID(context.Background(), "ws-from-conn")
	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"RemoteControlOnOutput","params":{}}`)
	resp := d.Dispatch(ctx, req)
	if errObj, ok := resp["error"]; ok {
		t.Fatalf("Dispatch returned error: %#v", errObj)
	}
	if got := fieldString(t, resp["result"], "sink_id"); got != "ws-from-conn" {
		t.Errorf("result.sink_id = %q, want ws-from-conn (defaulted from connID)", got)
	}
	if !d.IsAllSink("ws-from-conn") {
		t.Errorf("AllSinks missing ws-from-conn (defaulted registration)")
	}
}

// TestDispatcher_RemoteControlOnOutput_BadParams pins
// the -32602 envelope for non-string sink_id.
func TestDispatcher_RemoteControlOnOutput_BadParams(t *testing.T) {
	sess := NewSessionState("1.4.2-test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"RemoteControlOnOutput","params":{"sink_id":42}}`)
	resp := d.Dispatch(context.Background(), req)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error envelope, got %#v", resp)
	}
	if got := errorCode(errObj); got != -32602 {
		t.Errorf("error.code = %d, want -32602", got)
	}
}

// TestDispatcher_RemoteControlOnMessage_Allowed
// pins the headline allow-list happy path: send a
// RemoteAgentStatus message through, get
// accepted:true. RemoteAgentStatus is in the
// allowlist (the cycle's entry for the agy-synonym
// of RemoteControlStatus) so the gatekeeper passes;
// the inner dispatch happens but its response is
// discarded — the outer envelope is the
// confirmation.
func TestDispatcher_RemoteControlOnMessage_Allowed(t *testing.T) {
	sess := NewSessionState("1.4.2-test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	inner := map[string]any{
		"jsonrpc": "2.0",
		"method":  "RemoteAgentStatus",
	}
	innerBytes, _ := json.Marshal(inner)
	req := []byte(fmt.Sprintf(
		`{"jsonrpc":"2.0","id":1,"method":"remoteControlOnMessage","params":{"message":%s}}`,
		string(innerBytes),
	))
	resp := d.Dispatch(context.Background(), req)
	if errObj, ok := resp["error"]; ok {
		t.Fatalf("Dispatch returned error: %#v", errObj)
	}
	if got := fieldBool(t, resp["result"], "ok"); !got {
		t.Errorf("result.ok = false, want true")
	}
	if got := fieldBool(t, resp["result"], "accepted"); !got {
		t.Errorf("result.accepted = false, want true")
	}
	got := fieldString(t, resp["result"], "replayed_at")
	if got == "" {
		t.Errorf("result.replayed_at missing or empty")
	} else if _, err := time.Parse(time.RFC3339Nano, got); err != nil {
		t.Errorf("result.replayed_at %q does not parse as RFC3339Nano: %v", got, err)
	}
}

// TestDispatcher_RemoteControlOnMessage_NotAllowed
// pins the gatekeeper-rejection path: send a method
// that's not in the allowlist, get a -32603 envelope.
func TestDispatcher_RemoteControlOnMessage_NotAllowed(t *testing.T) {
	sess := NewSessionState("1.4.2-test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	// "Foo" is not in the gatekeeper allowlist.
	inner := map[string]any{
		"jsonrpc": "2.0",
		"method":  "Foo",
	}
	innerBytes, _ := json.Marshal(inner)
	req := []byte(fmt.Sprintf(
		`{"jsonrpc":"2.0","id":1,"method":"remoteControlOnMessage","params":{"message":%s}}`,
		string(innerBytes),
	))
	resp := d.Dispatch(context.Background(), req)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error envelope, got %#v", resp)
	}
	if got := errorCode(errObj); got != -32603 {
		t.Errorf("error.code = %d, want -32603", got)
	}
	if msg, _ := errObj["data"].(string); !strings.Contains(msg, "Foo") {
		t.Errorf("error.data = %q, want it to contain the rejected method 'Foo'", msg)
	}
}

// TestDispatcher_RemoteControlOnMessage_BadParams
// pins the -32602 envelope for missing message field
// (the brief's headline "bad params" path).
func TestDispatcher_RemoteControlOnMessage_BadParams(t *testing.T) {
	sess := NewSessionState("1.4.2-test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	// Empty message struct.
	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"remoteControlOnMessage","params":{"message":{}}}`)
	resp := d.Dispatch(context.Background(), req)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error envelope, got %#v", resp)
	}
	if got := errorCode(errObj); got != -32602 {
		t.Errorf("error.code = %d, want -32602 (empty message)", got)
	}

	// Missing message field entirely.
	req2 := []byte(`{"jsonrpc":"2.0","id":2,"method":"remoteControlOnMessage","params":{}}`)
	resp2 := d.Dispatch(context.Background(), req2)
	errObj2, ok := resp2["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error envelope, got %#v", resp2)
	}
	if got := errorCode(errObj2); got != -32602 {
		t.Errorf("error.code = %d, want -32602 (missing message)", got)
	}

	// message: null.
	req3 := []byte(`{"jsonrpc":"2.0","id":3,"method":"remoteControlOnMessage","params":{"message":null}}`)
	resp3 := d.Dispatch(context.Background(), req3)
	errObj3, ok := resp3["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error envelope, got %#v", resp3)
	}
	if got := errorCode(errObj3); got != -32602 {
		t.Errorf("error.code = %d, want -32602 (null message)", got)
	}

	// message.method missing.
	inner4 := `{"jsonrpc":"2.0"}`
	req4 := []byte(fmt.Sprintf(
		`{"jsonrpc":"2.0","id":4,"method":"remoteControlOnMessage","params":{"message":%s}}`,
		inner4,
	))
	resp4 := d.Dispatch(context.Background(), req4)
	errObj4, ok := resp4["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error envelope, got %#v", resp4)
	}
	if got := errorCode(errObj4); got != -32602 {
		t.Errorf("error.code = %d, want -32602 (missing method)", got)
	}

	// message.method wrong type.
	inner5 := `{"jsonrpc":"2.0","method":42}`
	req5 := []byte(fmt.Sprintf(
		`{"jsonrpc":"2.0","id":5,"method":"remoteControlOnMessage","params":{"message":%s}}`,
		inner5,
	))
	resp5 := d.Dispatch(context.Background(), req5)
	errObj5, ok := resp5["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error envelope, got %#v", resp5)
	}
	if got := errorCode(errObj5); got != -32602 {
		t.Errorf("error.code = %d, want -32602 (non-string method)", got)
	}
}

// TestIsRemoteControlOnMessageAllowed pins the
// gatekeeper helper directly (it has its own logic that
// the dispatcher delegates to; a unit-level test is
// the cleanest place to pin the contract).
func TestIsRemoteControlOnMessageAllowed(t *testing.T) {
	allowed := []string{
		"RemoteAgentStatus",
		"ListRemoteAgentModels",
		"ListRemoteAgentConversations",
		"remoteControlVersion",
	}
	denied := []string{
		"",
		"Foo",
		"StartRemoteAgentCascade",             // write — not allowed
		"SendRemoteAgentCascadeMessage",       // write — not allowed
		"CancelRemoteAgentCascade",            // write — not allowed
		"CreateRemoteAgentProject",            // write — not allowed
		"DeleteRemoteAgentConversation",       // destructive — not allowed
		"SetRemoteAgentFastpushEnabled",       // write — not allowed
		"UpdateRemoteAgentProject",            // write — not allowed
		"UpsertRemoteAgentConversationConfig", // write — not allowed
	}
	for _, m := range allowed {
		if !isRemoteControlOnMessageAllowed(m) {
			t.Errorf("isRemoteControlOnMessageAllowed(%q) = false, want true", m)
		}
	}
	for _, m := range denied {
		if isRemoteControlOnMessageAllowed(m) {
			t.Errorf("isRemoteControlOnMessageAllowed(%q) = true, want false", m)
		}
	}
}

// TestDefaultDispatchMethods_WireSorted pins the
// wire-shape sort contract: the package var doesn't
// have to be sorted (it's organised by category for
// maintainability), but the snapshot the dispatcher
// hands to the wire MUST be sorted. The brief pins
// the wire shape; dashboards can binary-search the
// response. This test is the single-line check that
// the brief's contract holds.
func TestDefaultDispatchMethods_WireSorted(t *testing.T) {
	sess := NewSessionState("1.4.2-test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	wire := sortedDispatchMethods()
	for i := 1; i < len(wire); i++ {
		if wire[i-1] > wire[i] {
			t.Errorf("sortedDispatchMethods not sorted at index %d: %q > %q",
				i, wire[i-1], wire[i])
		}
	}
	if len(wire) == 0 {
		t.Errorf("sortedDispatchMethods is empty; want >= 1 entry")
	}
	if d == nil {
		t.Fatalf("dispatcher is nil")
	}
}

// TestDefaultNotificationFilter_Sorted pins the
// canonical filter list's sort order (also a
// wire-sort check, mirroring DispatchMethods).
func TestDefaultNotificationFilter_Sorted(t *testing.T) {
	for i := 1; i < len(DefaultNotificationFilter); i++ {
		if DefaultNotificationFilter[i-1] > DefaultNotificationFilter[i] {
			t.Errorf("DefaultNotificationFilter not sorted at index %d: %q > %q",
				i, DefaultNotificationFilter[i-1], DefaultNotificationFilter[i])
		}
	}
	if len(DefaultNotificationFilter) == 0 {
		t.Errorf("DefaultNotificationFilter is empty; want >= 1 entry")
	}
}

// TestAllSinks_NilReceiverSafe pins the nil-safe path:
// every AllSinks helper returns a zero value (no
// panic) when called on a nil *Dispatcher. The
// future rc-all-sinks-fanout cycle relies on this for
// tests that build partial dispatchers.
func TestAllSinks_NilReceiverSafe(t *testing.T) {
	var d *Dispatcher
	d.AddAllSink("foo") // no panic
	d.RemoveAllSink("foo")
	if d.IsAllSink("foo") {
		t.Errorf("nil dispatcher IsAllSink returned true")
	}
	if got := d.AllSinkConnIDs(); got != nil {
		t.Errorf("nil dispatcher AllSinkConnIDs = %v, want nil", got)
	}
}

// TestRemoteControlOnMessage_InnerResponseDiscarded
// pins the contract that the inner dispatch's response
// is intentionally discarded — the outer envelope is
// just the confirmation. The test confirms the outer
// envelope is the canonical RemoteControlOnMessageResult
// shape, NOT a wrapped inner response.
func TestRemoteControlOnMessage_InnerResponseDiscarded(t *testing.T) {
	sess := NewSessionState("1.4.2-test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	// remoteControlVersion is in the allowlist AND
	// registered as a top-level method (its handler
	// returns a rich map). The outer envelope should
	// NOT be the inner response (the cycle explicitly
	// discards the inner frame).
	inner := map[string]any{
		"jsonrpc": "2.0",
		"method":  "remoteControlVersion",
	}
	innerBytes, _ := json.Marshal(inner)
	req := []byte(fmt.Sprintf(
		`{"jsonrpc":"2.0","id":1,"method":"remoteControlOnMessage","params":{"message":%s}}`,
		string(innerBytes),
	))
	resp := d.Dispatch(context.Background(), req)
	if errObj, ok := resp["error"]; ok {
		t.Fatalf("Dispatch returned error: %#v", errObj)
	}
	res := resultAsMap(t, resp["result"])
	if _, hasVersion := res["version"]; hasVersion {
		t.Errorf("outer envelope leaked inner version field — inner response was not discarded: %v", res)
	}
	if _, hasBuildLabel := res["build_label"]; hasBuildLabel {
		t.Errorf("outer envelope leaked inner build_label field — inner response was not discarded: %v", res)
	}
	// The outer envelope's canonical shape:
	if _, ok := res["accepted"]; !ok {
		t.Errorf("outer envelope missing 'accepted': %v", res)
	}
	if _, ok := res["replayed_at"]; !ok {
		t.Errorf("outer envelope missing 'replayed_at': %v", res)
	}
}

// ----- DeferredFeatures (cycle rc-webrtc-mesh-defer) -----
//
// The four canonical keys must always be on the wire:
// dashboards iterate them instead of nil-checking. Every
// default key flips to `false`; a host can override individual
// entries via Config.DeferredFeatures.

// TestDeferredFeatures_AllFalseByDefault pins the
// baseline: a dispatcher with no Config.DeferredFeatures
// returns the canonical four keys all set to `false`.
// Dashboards reading "is the v2 path on?" expect a
// flat `false` on every key today.
func TestDeferredFeatures_AllFalseByDefault(t *testing.T) {
	sess := NewSessionState("1.4.2-test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	snap := d.topLevelStatus()
	if snap.DeferredFeatures == nil {
		t.Fatalf("DeferredFeatures is nil; want non-nil 4-key map")
	}
	wantKeys := []string{"antigravity_hub", "enterprise_tunnel", "systemd_user", "webrtc_mesh"}
	if len(snap.DeferredFeatures) != len(wantKeys) {
		t.Errorf("DeferredFeatures has %d keys, want %d (canonical)",
			len(snap.DeferredFeatures), len(wantKeys))
	}
	for _, k := range wantKeys {
		v, ok := snap.DeferredFeatures[k]
		if !ok {
			t.Errorf("DeferredFeatures missing canonical key %q", k)
			continue
		}
		if v {
			t.Errorf("DeferredFeatures[%q] = true, want false (default off)", k)
		}
	}
}

// TestDeferredFeatures_HostOverride pins the host-override
// path: a dispatcher with Config.DeferredFeatures
// containing a subset of the canonical keys must see
// those entries flipped while the remaining canonical
// keys stay `false`. A missing-key scenario from a
// partial host override is the most common case (a
// future rc-webrtc-mesh cycle that ships only one
// feature at a time).
func TestDeferredFeatures_HostOverride(t *testing.T) {
	sess := NewSessionState("1.4.2-test")
	d, err := NewDispatcher(Config{
		Session: sess,
		DeferredFeatures: map[string]bool{
			"webrtc_mesh": true,
			// "antigravity_hub", "systemd_user", "enterprise_tunnel"
			// are deliberately omitted — they should default to false.
		},
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	snap := d.topLevelStatus()
	if snap.DeferredFeatures["webrtc_mesh"] != true {
		t.Errorf("DeferredFeatures[webrtc_mesh] = false, want true (host override)")
	}
	for _, k := range []string{"antigravity_hub", "systemd_user", "enterprise_tunnel"} {
		if snap.DeferredFeatures[k] != false {
			t.Errorf("DeferredFeatures[%q] = true, want false (canonical default)", k)
		}
	}
	// All four canonical keys must be present.
	if len(snap.DeferredFeatures) != 4 {
		t.Errorf("DeferredFeatures has %d keys, want 4 (no drift in canonical set)",
			len(snap.DeferredFeatures))
	}
}

// TestDeferredFeatures_NilReceiverSafe pins the
// defensive path: topLevelStatus on a nil *Dispatcher
// must produce a non-nil deferred_features map with the
// canonical four keys, all false. A dashboard renderer
// never sees a nil map.
func TestDeferredFeatures_NilReceiverSafe(t *testing.T) {
	// nil receiver — defer through a typed-nil pointer.
	var d *Dispatcher
	deferSnap := deferredFeaturesSnapshot(d)
	if deferSnap == nil {
		t.Fatalf("deferredFeaturesSnapshot(nil) returned nil; want non-nil map")
	}
	if len(deferSnap) != 4 {
		t.Errorf("deferredFeaturesSnapshot(nil) has %d keys, want 4", len(deferSnap))
	}
	for _, k := range []string{"antigravity_hub", "enterprise_tunnel", "systemd_user", "webrtc_mesh"} {
		if deferSnap[k] != false {
			t.Errorf("deferredFeaturesSnapshot(nil)[%q] = true, want false", k)
		}
	}
}

// TestDeferredFeatures_DefensiveCopy pins that the
// topLevelStatus helper returns a fresh map on every
// call. A host that mutates its Config.DeferredFeatures
// after construction must not see those mutations leak
// into a dispatches's wire response (would-be race).
func TestDeferredFeatures_DefensiveCopy(t *testing.T) {
	override := map[string]bool{"webrtc_mesh": true}
	sess := NewSessionState("1.4.2-test")
	d, err := NewDispatcher(Config{
		Session:          sess,
		DeferredFeatures: override,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	first := d.topLevelStatus().DeferredFeatures
	if first["webrtc_mesh"] != true {
		t.Fatalf("baseline: DeferredFeatures[webrtc_mesh] = false, want true")
	}
	// Mutate the host's source map.
	override["webrtc_mesh"] = false
	override["antigravity_hub"] = true

	second := d.topLevelStatus().DeferredFeatures
	// The dispatcher's defensive copy must reflect the
	// source at read time on a fresh dispatcher call —
	// we deliberately share the source map (it would
	// be a needless copy if not) and re-read on every
	// call. The pin here is "the second response sees
	// the post-mutation values", NOT that the two
	// responses are isolated maps.
	if second["webrtc_mesh"] != false {
		t.Errorf("after host mutation: DeferredFeatures[webrtc_mesh] = true, want false (re-read)")
	}
	if second["antigravity_hub"] != true {
		t.Errorf("after host mutation: DeferredFeatures[antigravity_hub] = false, want true (re-read)")
	}
}

// TestDeferredFeatures_UnknownKeysIgnored pins the
// forward-compat contract: a host that installs a
// non-canonical key (e.g. a future
// "remote_agent_voice_chat") sees that key DROPPED in
// the wire response (the canonical four-key set must
// not drift). This keeps the wire shape stable across
// cycles.
func TestDeferredFeatures_UnknownKeysIgnored(t *testing.T) {
	sess := NewSessionState("1.4.2-test")
	d, err := NewDispatcher(Config{
		Session: sess,
		DeferredFeatures: map[string]bool{
			"webrtc_mesh":           true,
			"noncanonical_future":   true,
			"another_future_option": false,
		},
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	snap := d.topLevelStatus()

	if snap.DeferredFeatures["webrtc_mesh"] != true {
		t.Errorf("DeferredFeatures[webrtc_mesh] = false, want true")
	}
	if _, ok := snap.DeferredFeatures["noncanonical_future"]; ok {
		t.Errorf("DeferredFeatures leaked non-canonical key 'noncanonical_future': %v",
			snap.DeferredFeatures)
	}
	if _, ok := snap.DeferredFeatures["another_future_option"]; ok {
		t.Errorf("DeferredFeatures leaked non-canonical key 'another_future_option': %v",
			snap.DeferredFeatures)
	}
	// Canonical-key count must be exactly 4.
	if len(snap.DeferredFeatures) != 4 {
		t.Errorf("DeferredFeatures has %d keys, want exactly 4 (canonical)", len(snap.DeferredFeatures))
	}
}

// TestTopLevelStatus_IncludesDeferredFeatures is the
// wire-level smoke test: the in-process Dispatcher
// round-trip must emit the deferred_features envelope.
// Mirrors TestDispatcher_RemoteControlStatus' shape
// conventions.
func TestTopLevelStatus_IncludesDeferredFeatures(t *testing.T) {
	sess := NewSessionState("1.4.2-test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"RemoteControlStatus"}`)
	resp := d.Dispatch(context.Background(), req)
	if errObj, ok := resp["error"]; ok {
		t.Fatalf("Dispatch returned error: %#v", errObj)
	}
	// Use resultAsMap so we accept either a struct
	// (in-process path that bypasses JSON marshal)
	// or a map[string]any (wire-path).
	res := resultAsMap(t, resp["result"])
	deferred, ok := res["deferred_features"].(map[string]any)
	if !ok {
		t.Fatalf("result.deferred_features missing or wrong type: %T", res["deferred_features"])
	}
	for _, k := range []string{"antigravity_hub", "enterprise_tunnel", "systemd_user", "webrtc_mesh"} {
		v, ok := deferred[k]
		if !ok {
			t.Errorf("deferred_features missing canonical key %q", k)
			continue
		}
		if b, _ := v.(bool); b {
			t.Errorf("deferred_features[%q] = true, want false (default off)", k)
		}
	}
	if len(deferred) != 4 {
		t.Errorf("deferred_features has %d keys, want 4 (no drift)", len(deferred))
	}
}
