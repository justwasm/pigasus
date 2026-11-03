// Package remotecontrol — triggers_test.go: the dispatcher's
// round-trip against the triggers domain.
//
// The domain's own tests live in internal/remotecontrol/triggers;
// this file exercises the wire the dispatcher produces.
//
// Wire-level tests live in
// remotetransport/tcp_test.go (mirroring
// the project's split: in-process envelopes vs. wire
// round-trips).
package remotecontrol

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/remotecontrol/triggers"
)

// validTimeNow is the standard deterministic clock the
// in-process tests stamp dates with. RFC3339Nano form
// is used by the wire helpers; we use time.Date for
// readability here.
func validTimeNow() time.Time {
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
}

// TestValidateTriggerSpec_FileChanged_Ok pins the

// validSpecFor is a per-kind spec builder the in-process
// tests reuse. Keeping the constructor here means each
// test's nominal shape is obvious at the call site.
func validSpecFor(kind triggers.Kind) triggers.Spec {
	switch kind {
	case triggers.FileChanged:
		return triggers.Spec{"pattern": "*.go", "event": "modify"}
	case triggers.Schedule:
		return triggers.Spec{"prompt": "tick", "cron": "* * *"}
	case triggers.Webhook:
		return triggers.Spec{"path": "/hook", "prompt": "p"}
	case triggers.Manual:
		return triggers.Spec{"prompt": "p"}
	default:
		return triggers.Spec{}
	}
}

// newTriggerStoreDispatcher returns a Dispatcher
// with the supplied TriggerStore pre-installed. The
// helper mirrors newPluginsDispatcher.
func newTriggerStoreDispatcher(t *testing.T, ts triggers.Store) *Dispatcher {
	t.Helper()
	sess := NewSessionState("1.4.2")
	d, err := NewDispatcher(Config{
		Session:       sess,
		TriggersStore: ts,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	return d
}

// dispatchTrigger sends a request through the dispatcher
// and returns the (result, error) pair. Mirrors
// dispatchPlugins.
//
// nil params marshals to JSON `null` so the dispatcher's
// "len(params) > 0" guards see a present (but null)
// params block.

func dispatchTrigger(t *testing.T, d *Dispatcher, method string, params any) (map[string]any, map[string]any) {
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

// (jsonRPCFor is defined in lists_test.go.)

// TestTriggerStore_DefaultState_Empty pins the
// fresh-store default state: empty triggers slice,
// version=1, updated_at set.

func TestTriggerStore_Create_NoStore(t *testing.T) {
	ts := triggers.NewMemStore(validTimeNow)
	d := newTriggerStoreDispatcher(t, ts)
	// Clear the seam post-construction. The
	// dispatcher checks d.cfg.TriggersStore at
	// every handle call, so this exercises the
	// "store was set then unset" path. The
	// nil-fabrication only runs in NewDispatcher.
	d.cfg.TriggersStore = nil
	raw, _ := json.Marshal(map[string]any{
		"trigger": map[string]any{
			"project_id": "p", "name": "n", "kind": "manual",
			"spec": map[string]any{"prompt": "p"},
		},
	})
	req := jsonRPCFor(1, "CreateRemoteAgentTrigger", raw)
	resp := d.Dispatch(context.Background(), req)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("Create with no store: no error envelope, got %#v", resp)
	}
	if got := codeAsInt(t, errObj["code"]); got != -32603 {
		t.Errorf("error.code = %d, want -32603", got)
	}
}

// -----------------------------------------------------------------------------
// Dispatcher round-trips
// -----------------------------------------------------------------------------

// TestListRemoteAgentTriggers_Ok pins the
// List happy path: an empty store returns
// {ok:true, triggers:[]}.
func TestListRemoteAgentTriggers_Ok(t *testing.T) {
	ts := triggers.NewMemStore(validTimeNow)
	d := newTriggerStoreDispatcher(t, ts)
	res, errObj := dispatchTrigger(t, d, "ListRemoteAgentTriggers", []byte(`{}`))
	if errObj != nil {
		t.Fatalf("List: error = %#v", errObj)
	}
	if got, _ := res["ok"].(bool); !got {
		t.Errorf("ok = %v, want true", res["ok"])
	}
	triggers, ok := res["triggers"].([]any)
	if !ok {
		t.Fatalf("triggers = %T, want []any", res["triggers"])
	}
	if len(triggers) != 0 {
		t.Errorf("triggers length = %d, want 0", len(triggers))
	}
}

// TestListRemoteAgentTriggers_WithProject pins the
// "project_id filter" envelope.
func TestListRemoteAgentTriggers_WithProject(t *testing.T) {
	ts := triggers.NewMemStore(validTimeNow)
	_, _ = ts.Create(context.Background(), triggers.Trigger{
		ProjectID: "proj-1", Name: "a", Kind: triggers.Manual,
		Spec: validSpecFor(triggers.Manual),
	})
	_, _ = ts.Create(context.Background(), triggers.Trigger{
		ProjectID: "proj-2", Name: "b", Kind: triggers.Manual,
		Spec: validSpecFor(triggers.Manual),
	})
	d := newTriggerStoreDispatcher(t, ts)
	res, errObj := dispatchTrigger(t, d, "ListRemoteAgentTriggers",
		[]byte(`{"project_id":"proj-1"}`))
	if errObj != nil {
		t.Fatalf("List: error = %#v", errObj)
	}
	triggers, _ := res["triggers"].([]any)
	if len(triggers) != 1 {
		t.Errorf("triggers length = %d, want 1", len(triggers))
	}
	if len(triggers) > 0 {
		t0 := triggers[0].(map[string]any)
		if got, _ := t0["project_id"].(string); got != "proj-1" {
			t.Errorf("triggers[0].project_id = %q, want proj-1", got)
		}
	}
}

// TestListRemoteAgentTriggerOptions_Ok pins the
// fixed-shape options envelope.
func TestListRemoteAgentTriggerOptions_Ok(t *testing.T) {
	d := newTriggerStoreDispatcher(t, triggers.NewMemStore(validTimeNow))
	res, errObj := dispatchTrigger(t, d, "ListRemoteAgentTriggerOptions", []byte(`{}`))
	if errObj != nil {
		t.Fatalf("Options: error = %#v", errObj)
	}
	if got, _ := res["ok"].(bool); !got {
		t.Errorf("ok = %v, want true", res["ok"])
	}
	kinds, ok := res["kinds"].([]any)
	if !ok {
		t.Fatalf("kinds = %T, want []any", res["kinds"])
	}
	want := []string{"file-changed", "schedule", "webhook", "manual"}
	if len(kinds) != len(want) {
		t.Errorf("kinds length = %d, want %d", len(kinds), len(want))
	}
	for i, w := range want {
		if got, _ := kinds[i].(string); got != w {
			t.Errorf("kinds[%d] = %q, want %q", i, got, w)
		}
	}
	schemas, ok := res["spec_schemas"].(map[string]any)
	if !ok {
		t.Fatalf("spec_schemas = %T, want map[string]any", res["spec_schemas"])
	}
	for _, k := range want {
		if _, ok := schemas[k]; !ok {
			t.Errorf("spec_schemas missing %q", k)
		}
	}
}

// TestCreateRemoteAgentTrigger_Ok pins the
// Create happy path: response carries
// trigger_id and a full trigger object.
func TestCreateRemoteAgentTrigger_Ok(t *testing.T) {
	ts := triggers.NewMemStore(validTimeNow)
	d := newTriggerStoreDispatcher(t, ts)
	raw, _ := json.Marshal(map[string]any{
		"trigger": map[string]any{
			"project_id": "p", "name": "n", "kind": "manual",
			"enabled": true,
			"spec":    map[string]any{"prompt": "p"},
		},
	})
	res, errObj := dispatchTrigger(t, d, "CreateRemoteAgentTrigger", raw)
	if errObj != nil {
		t.Fatalf("Create: error = %#v", errObj)
	}
	if got, _ := res["trigger_id"].(string); !strings.HasPrefix(got, triggers.IDPrefix) {
		t.Errorf("trigger_id = %q, want prefix %q", got, triggers.IDPrefix)
	}
	trig, ok := res["trigger"].(map[string]any)
	if !ok {
		t.Fatalf("trigger = %T, want map[string]any", res["trigger"])
	}
	if got, _ := trig["project_id"].(string); got != "p" {
		t.Errorf("trigger.project_id = %q, want p", got)
	}
	if got, _ := trig["name"].(string); got != "n" {
		t.Errorf("trigger.name = %q, want n", got)
	}
	if got, _ := trig["kind"].(string); got != "manual" {
		t.Errorf("trigger.kind = %q, want manual", got)
	}
}

// TestCreateRemoteAgentTrigger_Duplicate pins the
// (project_id, name) duplicate → -32016 envelope.
func TestCreateRemoteAgentTrigger_Duplicate(t *testing.T) {
	ts := triggers.NewMemStore(validTimeNow)
	_, _ = ts.Create(context.Background(), triggers.Trigger{
		ProjectID: "p", Name: "n", Kind: triggers.Manual,
		Spec: validSpecFor(triggers.Manual),
	})
	d := newTriggerStoreDispatcher(t, ts)
	raw, _ := json.Marshal(map[string]any{
		"trigger": map[string]any{
			"project_id": "p", "name": "n", "kind": "manual",
			"spec": map[string]any{"prompt": "p"},
		},
	})
	resp := d.Dispatch(context.Background(), jsonRPCFor(1, "CreateRemoteAgentTrigger", raw))
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("Create (dup): no error envelope, got %#v", resp)
	}
	if got := codeAsInt(t, errObj["code"]); got != -32016 {
		t.Errorf("error.code = %d, want -32016", got)
	}
}

// TestCreateRemoteAgentTrigger_BadSpec pins the
// bad-spec → -32602 envelope.
func TestCreateRemoteAgentTrigger_BadSpec(t *testing.T) {
	d := newTriggerStoreDispatcher(t, triggers.NewMemStore(validTimeNow))
	raw, _ := json.Marshal(map[string]any{
		"trigger": map[string]any{
			"project_id": "p", "name": "n", "kind": "manual",
			"spec": map[string]any{}, // missing prompt
		},
	})
	resp := d.Dispatch(context.Background(), jsonRPCFor(1, "CreateRemoteAgentTrigger", raw))
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("Create (bad spec): no error envelope, got %#v", resp)
	}
	if got := codeAsInt(t, errObj["code"]); got != -32602 {
		t.Errorf("error.code = %d, want -32602", got)
	}
}

// TestCreateRemoteAgentTrigger_BadParams pins the
// "missing required fields" → -32602 envelope
// (different code path from ErrInvalidSpec but same
// envelope so dashboards can render uniformly).
func TestCreateRemoteAgentTrigger_BadParams(t *testing.T) {
	cases := []struct {
		label  string
		params map[string]any
	}{
		{"missing name", map[string]any{"trigger": map[string]any{"project_id": "p", "kind": "manual", "spec": map[string]any{"prompt": "p"}}}},
		{"missing project_id", map[string]any{"trigger": map[string]any{"name": "n", "kind": "manual", "spec": map[string]any{"prompt": "p"}}}},
		{"missing kind", map[string]any{"trigger": map[string]any{"project_id": "p", "name": "n", "spec": map[string]any{"prompt": "p"}}}},
		{"empty name", map[string]any{"trigger": map[string]any{"project_id": "p", "name": "", "kind": "manual", "spec": map[string]any{"prompt": "p"}}}},
		{"unknown kind", map[string]any{"trigger": map[string]any{"project_id": "p", "name": "n", "kind": "bogus", "spec": map[string]any{"prompt": "p"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			d := newTriggerStoreDispatcher(t, triggers.NewMemStore(validTimeNow))
			raw, _ := json.Marshal(tc.params)
			resp := d.Dispatch(context.Background(), jsonRPCFor(1, "CreateRemoteAgentTrigger", raw))
			errObj, ok := resp["error"].(map[string]any)
			if !ok {
				t.Fatalf("no error envelope, got %#v", resp)
			}
			if got := codeAsInt(t, errObj["code"]); got != -32602 {
				t.Errorf("error.code = %d, want -32602", got)
			}
		})
	}
}

// TestUpdateRemoteAgentTrigger_Ok pins the
// Update happy path: response carries the full
// post-mutation trigger.
func TestUpdateRemoteAgentTrigger_Ok(t *testing.T) {
	ts := triggers.NewMemStore(validTimeNow)
	created, _ := ts.Create(context.Background(), triggers.Trigger{
		ProjectID: "p", Name: "old", Kind: triggers.Manual,
		Spec: validSpecFor(triggers.Manual),
	})
	d := newTriggerStoreDispatcher(t, ts)
	newName := "new"
	raw, _ := json.Marshal(map[string]any{
		"trigger_id": created.TriggerID,
		"patch":      map[string]any{"name": newName},
	})
	res, errObj := dispatchTrigger(t, d, "UpdateRemoteAgentTrigger", raw)
	if errObj != nil {
		t.Fatalf("Update: error = %#v", errObj)
	}
	trig := res["trigger"].(map[string]any)
	if got, _ := trig["name"].(string); got != "new" {
		t.Errorf("trigger.name = %q, want new", got)
	}
}

// TestUpdateRemoteAgentTrigger_NotFound pins the
// unknown-id → -32017 envelope.
func TestUpdateRemoteAgentTrigger_NotFound(t *testing.T) {
	d := newTriggerStoreDispatcher(t, triggers.NewMemStore(validTimeNow))
	enabled := true
	raw, _ := json.Marshal(map[string]any{
		"trigger_id": "tri_does_not_exist",
		"patch":      map[string]any{"enabled": enabled},
	})
	resp := d.Dispatch(context.Background(), jsonRPCFor(1, "UpdateRemoteAgentTrigger", raw))
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("Update (not found): no error envelope, got %#v", resp)
	}
	if got := codeAsInt(t, errObj["code"]); got != -32017 {
		t.Errorf("error.code = %d, want -32017", got)
	}
}

// TestUpdateRemoteAgentTrigger_BadParams pins the
// missing / empty trigger_id → -32602 envelope.
func TestUpdateRemoteAgentTrigger_BadParams(t *testing.T) {
	cases := []struct {
		label  string
		params map[string]any
	}{
		{"empty trigger_id", map[string]any{"trigger_id": "", "patch": map[string]any{}}},
		{"missing trigger_id", map[string]any{"patch": map[string]any{}}},
		{"non-string trigger_id", map[string]any{"trigger_id": 42, "patch": map[string]any{}}},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			d := newTriggerStoreDispatcher(t, triggers.NewMemStore(validTimeNow))
			raw, _ := json.Marshal(tc.params)
			resp := d.Dispatch(context.Background(), jsonRPCFor(1, "UpdateRemoteAgentTrigger", raw))
			errObj, ok := resp["error"].(map[string]any)
			if !ok {
				t.Fatalf("no error envelope, got %#v", resp)
			}
			if got := codeAsInt(t, errObj["code"]); got != -32602 {
				t.Errorf("error.code = %d, want -32602", got)
			}
		})
	}
}

// TestDeleteRemoteAgentTrigger_Ok pins the
// Delete happy path: response carries a
// deleted_at RFC3339Nano stamp. The broadcast
// helper is exercised at the wire level (see the
// tcp_test counterparts).
func TestDeleteRemoteAgentTrigger_Ok(t *testing.T) {
	ts := triggers.NewMemStore(validTimeNow)
	created, _ := ts.Create(context.Background(), triggers.Trigger{
		ProjectID: "p", Name: "n", Kind: triggers.Manual,
		Spec: validSpecFor(triggers.Manual),
	})
	d := newTriggerStoreDispatcher(t, ts)
	raw, _ := json.Marshal(map[string]any{"trigger_id": created.TriggerID})
	res, errObj := dispatchTrigger(t, d, "DeleteRemoteAgentTrigger", raw)
	if errObj != nil {
		t.Fatalf("Delete: error = %#v", errObj)
	}
	if got, _ := res["trigger_id"].(string); got != created.TriggerID {
		t.Errorf("trigger_id = %q, want %q", got, created.TriggerID)
	}
	if got, _ := res["deleted_at"].(string); got == "" {
		t.Errorf("deleted_at empty: %#v", res)
	}
	if _, err := time.Parse(time.RFC3339Nano, res["deleted_at"].(string)); err != nil {
		t.Errorf("deleted_at not RFC3339Nano: %v", err)
	}
}

// TestDeleteRemoteAgentTrigger_NotFound pins the
// unknown-id → -32017 envelope.
func TestDeleteRemoteAgentTrigger_NotFound(t *testing.T) {
	d := newTriggerStoreDispatcher(t, triggers.NewMemStore(validTimeNow))
	raw, _ := json.Marshal(map[string]any{"trigger_id": "tri_does_not_exist"})
	resp := d.Dispatch(context.Background(), jsonRPCFor(1, "DeleteRemoteAgentTrigger", raw))
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("Delete (not found): no error envelope, got %#v", resp)
	}
	if got := codeAsInt(t, errObj["code"]); got != -32017 {
		t.Errorf("error.code = %d, want -32017", got)
	}
}

// TestDeleteRemoteAgentTrigger_BadParams pins the
// missing / empty / non-string trigger_id →
// -32602 envelope.
func TestDeleteRemoteAgentTrigger_BadParams(t *testing.T) {
	cases := []struct {
		label  string
		params map[string]any
	}{
		{"empty trigger_id", map[string]any{"trigger_id": ""}},
		{"missing trigger_id", map[string]any{}},
		{"non-string trigger_id", map[string]any{"trigger_id": 42}},
		{"non-string bool", map[string]any{"trigger_id": true}},
		{"whitespace trigger_id", map[string]any{"trigger_id": "   "}},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			d := newTriggerStoreDispatcher(t, triggers.NewMemStore(validTimeNow))
			raw, _ := json.Marshal(tc.params)
			resp := d.Dispatch(context.Background(), jsonRPCFor(1, "DeleteRemoteAgentTrigger", raw))
			errObj, ok := resp["error"].(map[string]any)
			if !ok {
				t.Fatalf("no error envelope, got %#v", resp)
			}
			if got := codeAsInt(t, errObj["code"]); got != -32602 {
				t.Errorf("error.code = %d, want -32602", got)
			}
		})
	}
}
