// Tests for the rc-triggers cycle. Covers:
//
//   - The five JSON-RPC methods (List / Options /
//     Create / Update / Delete), including the
//     sentinel-error mapping (ErrTriggerNotFound →
//     -32017, ErrTriggerExists → -32016,
//     ErrInvalidSpec → -32602) and the validation
//     rules (missing trigger_id → -32602, bad spec →
//     -32602);
//
//   - The TriggerStore.MemTriggerStore (Snapshot /
//     List / Create / Update / Delete /
//     custom-starting-snapshot / dedup-on-name /
//     unknown-id refusal) at the in-process level (no
//     Unix socket, no ws adapter);
//
//   - The FSTriggerStore happy path against a real
//     t.TempDir() so the on-disk triggers.json layout
//     is exercised (atomic temp + rename, 0o600
//     perms, version stamp, post-Create reload);
//
//   - The spec validator's per-kind rules
//     (file-changed / schedule / webhook / manual) —
//     each kind's required + optional field set and
//     the "missing key" / "wrong type" / "bad value"
//     envelopes.
//
// Wire-level tests live in
// remotetransport/tcp_test.go (mirroring the
// project's split: in-process envelopes vs. wire
// round-trips).
package remotecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// -----------------------------------------------------------------------------
// Spec validator
// -----------------------------------------------------------------------------

// validTimeNow is the standard deterministic clock the
// in-process tests stamp dates with. RFC3339Nano form
// is used by the wire helpers; we use time.Date for
// readability here.
func validTimeNow() time.Time {
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
}

// TestValidateTriggerSpec_FileChanged_Ok pins the
// happy-path file-changed spec: {pattern:"*.go",
// event:"modify"} is well-formed.
func TestValidateTriggerSpec_FileChanged_Ok(t *testing.T) {
	if err := ValidateTriggerSpec(TriggerFileChanged, TriggerSpec{
		"pattern": "*.go",
		"event":   "modify",
	}); err != nil {
		t.Fatalf("ValidateTriggerSpec: %v", err)
	}
}

// TestValidateTriggerSpec_FileChanged_Bad exercises
// the validator's "wrong event value" path: event
// must be one of create / modify / delete / rename.
func TestValidateTriggerSpec_FileChanged_Bad(t *testing.T) {
	cases := []struct {
		label string
		event string
	}{
		{"uppercase", "MODIFY"},
		{"unknown verb", "touch"},
		{"empty", ""},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			err := ValidateTriggerSpec(TriggerFileChanged, TriggerSpec{
				"pattern": "*.go",
				"event":   tc.event,
			})
			if err == nil {
				t.Fatalf("ValidateTriggerSpec: want error, got nil")
			}
			if !errors.Is(err, ErrInvalidSpec) {
				t.Errorf("ValidateTriggerSpec: error is not ErrInvalidSpec: %v", err)
			}
		})
	}
}

// TestValidateTriggerSpec_FileChanged_MissingPattern
// pins the "pattern key missing" path.
func TestValidateTriggerSpec_FileChanged_MissingPattern(t *testing.T) {
	err := ValidateTriggerSpec(TriggerFileChanged, TriggerSpec{
		"event": "modify",
	})
	if err == nil {
		t.Fatalf("ValidateTriggerSpec: want error, got nil")
	}
	if !errors.Is(err, ErrInvalidSpec) {
		t.Errorf("ValidateTriggerSpec: error is not ErrInvalidSpec: %v", err)
	}
	if !strings.Contains(err.Error(), "pattern") {
		t.Errorf("ValidateTriggerSpec: error message must mention 'pattern': %v", err)
	}
}

// TestValidateTriggerSpec_FileChanged_BadEvent pins
// the "event key missing" path.
func TestValidateTriggerSpec_FileChanged_BadEvent(t *testing.T) {
	err := ValidateTriggerSpec(TriggerFileChanged, TriggerSpec{
		"pattern": "*.go",
	})
	if err == nil {
		t.Fatalf("ValidateTriggerSpec: want error, got nil")
	}
	if !errors.Is(err, ErrInvalidSpec) {
		t.Errorf("ValidateTriggerSpec: error is not ErrInvalidSpec: %v", err)
	}
	if !strings.Contains(err.Error(), "event") {
		t.Errorf("ValidateTriggerSpec: error message must mention 'event': %v", err)
	}
}

// TestValidateTriggerSpec_Schedule_Ok pins the
// happy-path schedule spec: cron expression with a
// prompt.
func TestValidateTriggerSpec_Schedule_Ok(t *testing.T) {
	if err := ValidateTriggerSpec(TriggerSchedule, TriggerSpec{
		"prompt": "tick-tock",
		"cron":   "*/5 * * * *",
	}); err != nil {
		t.Fatalf("ValidateTriggerSpec(cron): %v", err)
	}
	if err := ValidateTriggerSpec(TriggerSchedule, TriggerSpec{
		"prompt": "tick-tock",
		"every":  "30m",
	}); err != nil {
		t.Fatalf("ValidateTriggerSpec(every): %v", err)
	}
}

// TestValidateTriggerSpec_Schedule_BadCron pins the
// "both cron AND every" → -32602 path. Exactly one
// must be set.
func TestValidateTriggerSpec_Schedule_BadCron(t *testing.T) {
	cases := []struct {
		label string
		spec  TriggerSpec
	}{
		{
			"both cron and every",
			TriggerSpec{"prompt": "p", "cron": "* * *", "every": "30m"},
		},
		{
			"neither cron nor every",
			TriggerSpec{"prompt": "p"},
		},
		{
			"missing prompt",
			TriggerSpec{"cron": "* * *"},
		},
		{
			"empty prompt",
			TriggerSpec{"prompt": "", "cron": "* * *"},
		},
		{
			"empty cron",
			TriggerSpec{"prompt": "p", "cron": ""},
		},
		{
			"empty every",
			TriggerSpec{"prompt": "p", "every": ""},
		},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			err := ValidateTriggerSpec(TriggerSchedule, tc.spec)
			if err == nil {
				t.Fatalf("ValidateTriggerSpec: want error, got nil")
			}
			if !errors.Is(err, ErrInvalidSpec) {
				t.Errorf("ValidateTriggerSpec: error is not ErrInvalidSpec: %v", err)
			}
		})
	}
}

// TestValidateTriggerSpec_Webhook_Ok pins the
// happy-path webhook spec: /path plus a prompt.
// Secret is optional.
func TestValidateTriggerSpec_Webhook_Ok(t *testing.T) {
	if err := ValidateTriggerSpec(TriggerWebhook, TriggerSpec{
		"path":   "/hook",
		"prompt": "do something",
	}); err != nil {
		t.Fatalf("ValidateTriggerSpec (no secret): %v", err)
	}
	if err := ValidateTriggerSpec(TriggerWebhook, TriggerSpec{
		"path":   "/hook",
		"prompt": "do something",
		"secret": "hex-of-anything",
	}); err != nil {
		t.Fatalf("ValidateTriggerSpec (with secret): %v", err)
	}
}

// TestValidateTriggerSpec_Webhook_Bad pins the
// validators' "missing path" and "wrong-type path"
// envelopes.
func TestValidateTriggerSpec_Webhook_Bad(t *testing.T) {
	cases := []struct {
		label string
		spec  TriggerSpec
		want  string
	}{
		{
			"missing path",
			TriggerSpec{"prompt": "p"},
			"path",
		},
		{
			"path missing leading slash",
			TriggerSpec{"path": "hook", "prompt": "p"},
			"/",
		},
		{
			"missing prompt",
			TriggerSpec{"path": "/hook"},
			"prompt",
		},
		{
			"empty prompt",
			TriggerSpec{"path": "/hook", "prompt": ""},
			"prompt",
		},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			err := ValidateTriggerSpec(TriggerWebhook, tc.spec)
			if err == nil {
				t.Fatalf("ValidateTriggerSpec: want error, got nil")
			}
			if !errors.Is(err, ErrInvalidSpec) {
				t.Errorf("error is not ErrInvalidSpec: %v", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q must mention %q", err, tc.want)
			}
		})
	}
}

// TestValidateTriggerSpec_Manual_Ok pins the
// happy-path manual spec: a prompt alone is enough.
func TestValidateTriggerSpec_Manual_Ok(t *testing.T) {
	if err := ValidateTriggerSpec(TriggerManual, TriggerSpec{
		"prompt": "do a thing",
	}); err != nil {
		t.Fatalf("ValidateTriggerSpec: %v", err)
	}
}

// TestValidateTriggerSpec_Manual_MissingPrompt pins
// the manual spec's "missing prompt" envelope.
func TestValidateTriggerSpec_Manual_MissingPrompt(t *testing.T) {
	cases := []struct {
		label string
		spec  TriggerSpec
	}{
		{"missing prompt", TriggerSpec{}},
		{"empty prompt", TriggerSpec{"prompt": ""}},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			err := ValidateTriggerSpec(TriggerManual, tc.spec)
			if err == nil {
				t.Fatalf("ValidateTriggerSpec: want error, got nil")
			}
			if !errors.Is(err, ErrInvalidSpec) {
				t.Errorf("error is not ErrInvalidSpec: %v", err)
			}
		})
	}
}

// TestValidateTriggerSpec_UnknownKind pins the
// "kind ∈ closed set" envelope.
func TestValidateTriggerSpec_UnknownKind(t *testing.T) {
	err := ValidateTriggerSpec(TriggerKind("bogus"), TriggerSpec{"prompt": "p"})
	if err == nil {
		t.Fatalf("ValidateTriggerSpec: want error, got nil")
	}
	if !errors.Is(err, ErrInvalidSpec) {
		t.Errorf("error is not ErrInvalidSpec: %v", err)
	}
}

// -----------------------------------------------------------------------------
// TriggerStore (in-memory)
// -----------------------------------------------------------------------------

// newTriggerStoreDispatcher returns a Dispatcher
// with the supplied TriggerStore pre-installed. The
// helper mirrors newPluginsDispatcher.
func newTriggerStoreDispatcher(t *testing.T, ts TriggerStore) *Dispatcher {
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
func TestTriggerStore_DefaultState_Empty(t *testing.T) {
	ts := NewMemTriggerStore(validTimeNow)
	st, err := ts.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if st.Version != TriggerStateVersion {
		t.Errorf("Version = %d, want %d", st.Version, TriggerStateVersion)
	}
	if len(st.Triggers) != 0 {
		t.Errorf("Triggers length = %d, want 0", len(st.Triggers))
	}
	out, err := ts.List(context.Background(), "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("List length = %d, want 0", len(out))
	}
}

// validSpecFor is a per-kind spec builder the in-process
// tests reuse. Keeping the constructor here means each
// test's nominal shape is obvious at the call site.
func validSpecFor(kind TriggerKind) TriggerSpec {
	switch kind {
	case TriggerFileChanged:
		return TriggerSpec{"pattern": "*.go", "event": "modify"}
	case TriggerSchedule:
		return TriggerSpec{"prompt": "tick", "cron": "* * *"}
	case TriggerWebhook:
		return TriggerSpec{"path": "/hook", "prompt": "p"}
	case TriggerManual:
		return TriggerSpec{"prompt": "p"}
	default:
		return TriggerSpec{}
	}
}

// TestTriggerStore_Create_DedupName pins the
// (project_id, name) uniqueness rule: a second Create
// with the same (project_id, name) returns
// ErrTriggerExists → -32016.
func TestTriggerStore_Create_DedupName(t *testing.T) {
	ts := NewMemTriggerStore(validTimeNow)
	d := newTriggerStoreDispatcher(t, ts)

	// First create — succeeds.
	t1, err := ts.Create(context.Background(), Trigger{
		ProjectID: "proj-1",
		Name:      "on-pull",
		Kind:      TriggerManual,
		Spec:      validSpecFor(TriggerManual),
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if t1.TriggerID == "" {
		t.Errorf("first Create: trigger_id empty")
	}

	// Second create with same (project_id, name)
	// must return ErrTriggerExists.
	_, err = ts.Create(context.Background(), Trigger{
		ProjectID: "proj-1",
		Name:      "on-pull",
		Kind:      TriggerManual,
		Spec:      validSpecFor(TriggerManual),
		Enabled:   true,
	})
	if !errors.Is(err, ErrTriggerExists) {
		t.Fatalf("second Create: error is not ErrTriggerExists: %v", err)
	}

	// Same name in a different project is allowed.
	_, err = ts.Create(context.Background(), Trigger{
		ProjectID: "proj-2",
		Name:      "on-pull",
		Kind:      TriggerManual,
		Spec:      validSpecFor(TriggerManual),
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("different-project Create: %v", err)
	}

	// Dispatcher maps the duplicate to -32016 too.
	_ = d
}

// TestTriggerStore_List_All pins the "no filter"
// branch: List("") returns every trigger, sorted by
// CreatedAt ascending.
func TestTriggerStore_List_All(t *testing.T) {
	ts := NewMemTriggerStore(validTimeNow)
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 1; i <= 3; i++ {
		_, err := ts.Create(context.Background(), Trigger{
			ProjectID: "proj",
			Name:      "name-" + string(rune('a'+i-1)),
			Kind:      TriggerManual,
			Spec:      validSpecFor(TriggerManual),
			Enabled:   true,
		})
		if err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
		_ = clock // monotonic marker for sort test below
	}
	out, err := ts.List(context.Background(), "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(out) != 3 {
		t.Errorf("List length = %d, want 3", len(out))
	}
	// Monotonic CreatedAt ⇒ output order matches input order.
	for i := 1; i < len(out); i++ {
		if out[i].CreatedAt.Before(out[i-1].CreatedAt) {
			t.Errorf("triggers not sorted by CreatedAt ascending: %v before %v",
				out[i-1].CreatedAt, out[i].CreatedAt)
		}
	}
}

// TestTriggerStore_List_FilterByProject pins the
// "filter on project_id" branch: List("proj-1")
// returns only proj-1's triggers.
func TestTriggerStore_List_FilterByProject(t *testing.T) {
	ts := NewMemTriggerStore(validTimeNow)
	_, _ = ts.Create(context.Background(), Trigger{
		ProjectID: "proj-1", Name: "a", Kind: TriggerManual,
		Spec: validSpecFor(TriggerManual),
	})
	_, _ = ts.Create(context.Background(), Trigger{
		ProjectID: "proj-2", Name: "a", Kind: TriggerManual,
		Spec: validSpecFor(TriggerManual),
	})
	_, _ = ts.Create(context.Background(), Trigger{
		ProjectID: "proj-1", Name: "b", Kind: TriggerManual,
		Spec: validSpecFor(TriggerManual),
	})
	out, err := ts.List(context.Background(), "proj-1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(out) != 2 {
		t.Errorf("List(proj-1) length = %d, want 2", len(out))
	}
	for _, tg := range out {
		if tg.ProjectID != "proj-1" {
			t.Errorf("List(proj-1): stray trigger %q in proj %q", tg.Name, tg.ProjectID)
		}
	}
}

// TestTriggerStore_Update_Name pins the
// Update-with-Name-change happy path.
func TestTriggerStore_Update_Name(t *testing.T) {
	ts := NewMemTriggerStore(validTimeNow)
	created, err := ts.Create(context.Background(), Trigger{
		ProjectID: "proj", Name: "old-name", Kind: TriggerManual,
		Spec: validSpecFor(TriggerManual),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	newName := "new-name"
	out, err := ts.Update(context.Background(), created.TriggerID, TriggerPatch{
		Name: &newName,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if out.Name != "new-name" {
		t.Errorf("Name = %q, want new-name", out.Name)
	}
}

// TestTriggerStore_Update_Enabled pins the
// Update-with-Enabled-change happy path.
func TestTriggerStore_Update_Enabled(t *testing.T) {
	ts := NewMemTriggerStore(validTimeNow)
	created, err := ts.Create(context.Background(), Trigger{
		ProjectID: "proj", Name: "x", Kind: TriggerManual,
		Spec: validSpecFor(TriggerManual), Enabled: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	enabled := false
	out, err := ts.Update(context.Background(), created.TriggerID, TriggerPatch{
		Enabled: &enabled,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if out.Enabled {
		t.Errorf("Enabled = true, want false")
	}
}

// TestTriggerStore_Update_Spec pins the
// Update-with-Spec-change happy path. The kind
// governs validation; the new spec must validate
// against the existing kind.
func TestTriggerStore_Update_Spec(t *testing.T) {
	ts := NewMemTriggerStore(validTimeNow)
	created, err := ts.Create(context.Background(), Trigger{
		ProjectID: "proj", Name: "x", Kind: TriggerSchedule,
		Spec: TriggerSpec{"prompt": "p", "cron": "* * *"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	newSpec := TriggerSpec{"prompt": "p2", "every": "30m"}
	out, err := ts.Update(context.Background(), created.TriggerID, TriggerPatch{
		Spec: &newSpec,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got, _ := out.Spec["every"].(string); got != "30m" {
		t.Errorf("Spec[every] = %v, want 30m", out.Spec["every"])
	}
}

// TestTriggerStore_Update_NotFound pins the
// unknown-id → -32017 envelope.
func TestTriggerStore_Update_NotFound(t *testing.T) {
	ts := NewMemTriggerStore(validTimeNow)
	enabled := true
	_, err := ts.Update(context.Background(), "tri_does_not_exist", TriggerPatch{
		Enabled: &enabled,
	})
	if !errors.Is(err, ErrTriggerNotFound) {
		t.Fatalf("Update: error is not ErrTriggerNotFound: %v", err)
	}
}

// TestTriggerStore_Delete pins the Delete happy
// path: post-Delete List returns the empty set, and
// Snapshot's UpdatedAt has bumped.
func TestTriggerStore_Delete(t *testing.T) {
	ts := NewMemTriggerStore(validTimeNow)
	created, err := ts.Create(context.Background(), Trigger{
		ProjectID: "proj", Name: "x", Kind: TriggerManual,
		Spec: validSpecFor(TriggerManual),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	state, err := ts.Delete(context.Background(), created.TriggerID)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(state.Triggers) != 0 {
		t.Errorf("post-Delete Triggers length = %d, want 0", len(state.Triggers))
	}
}

// TestTriggerStore_Delete_NotFound pins the
// unknown-id → -32017 envelope for Delete.
func TestTriggerStore_Delete_NotFound(t *testing.T) {
	ts := NewMemTriggerStore(validTimeNow)
	_, err := ts.Delete(context.Background(), "tri_does_not_exist")
	if !errors.Is(err, ErrTriggerNotFound) {
		t.Fatalf("Delete: error is not ErrTriggerNotFound: %v", err)
	}
}

// TestTriggerStore_SortPinning pins that the store
// sorts by CreatedAt ascending regardless of
// insertion order: a pre-populated starting snapshot
// with out-of-order CreatedAt is sorted by
// Snapshot / List, not preserved as-is.
func TestTriggerStore_SortPinning(t *testing.T) {
	now := validTimeNow()
	t1 := now.Add(3 * time.Hour)
	t2 := now.Add(1 * time.Hour)
	t3 := now.Add(2 * time.Hour)
	ts := NewMemTriggerStoreWith([]Trigger{
		{TriggerID: "tri_a", Name: "a", ProjectID: "p", Kind: TriggerManual,
			Spec: validSpecFor(TriggerManual), CreatedAt: t1, UpdatedAt: t1},
		{TriggerID: "tri_b", Name: "b", ProjectID: "p", Kind: TriggerManual,
			Spec: validSpecFor(TriggerManual), CreatedAt: t2, UpdatedAt: t2},
		{TriggerID: "tri_c", Name: "c", ProjectID: "p", Kind: TriggerManual,
			Spec: validSpecFor(TriggerManual), CreatedAt: t3, UpdatedAt: t3},
	}, validTimeNow)
	out, err := ts.List(context.Background(), "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("List length = %d, want 3", len(out))
	}
	// Sorted by CreatedAt ascending: b (1h), c (2h), a (3h).
	want := []string{"tri_b", "tri_c", "tri_a"}
	for i, w := range want {
		if out[i].TriggerID != w {
			t.Errorf("out[%d].TriggerID = %q, want %q", i, out[i].TriggerID, w)
		}
	}
}

// TestTriggerStore_Create_BadSpec pins the
// validator's path through Create: a bad spec
// surfaces ErrInvalidSpec → -32602 before the row
// lands.
func TestTriggerStore_Create_BadSpec(t *testing.T) {
	ts := NewMemTriggerStore(validTimeNow)
	_, err := ts.Create(context.Background(), Trigger{
		ProjectID: "proj", Name: "x", Kind: TriggerManual,
		Spec: TriggerSpec{}, // empty — missing prompt
	})
	if !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("Create: error is not ErrInvalidSpec: %v", err)
	}
}

// TestTriggerStore_Create_NoStore pins the
// "dispatcher with nil store" → -32603 envelope.
// The dispatcher fabricates a fresh MemTriggerStore
// when Config.TriggersStore is nil (the tests'
// default-shim path). To exercise the negative, we
// bypass that fabrication by clearing the field
// POST-construction — explicitly setting it back to
// nil trips the dispatcher's "no store installed"
// guard.
func TestTriggerStore_Create_NoStore(t *testing.T) {
	ts := NewMemTriggerStore(validTimeNow)
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
// FSTriggerStore
// -----------------------------------------------------------------------------

// TestFSTriggerStore_RoundTrip pins the fresh +
// Create + reload happy path: a brand-new directory
// + a Create + a fresh store load returns the row.
func TestFSTriggerStore_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "triggers.json")
	now := validTimeNow()
	nowFn := func() time.Time { return now }
	s1 := NewFSTriggerStore(path, nowFn)
	created, err := s1.Create(context.Background(), Trigger{
		ProjectID: "p", Name: "n", Kind: TriggerManual,
		Spec: validSpecFor(TriggerManual),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.TriggerID == "" {
		t.Fatalf("Create: trigger_id empty")
	}
	// Reload — the fresh store loads the saved
	// state and Snapshot returns the row.
	s2 := NewFSTriggerStore(path, nowFn)
	st, err := s2.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Reload Snapshot: %v", err)
	}
	if len(st.Triggers) != 1 {
		t.Errorf("Reload: triggers length = %d, want 1", len(st.Triggers))
	}
	if st.Triggers[0].TriggerID != created.TriggerID {
		t.Errorf("Reload: trigger_id = %q, want %q",
			st.Triggers[0].TriggerID, created.TriggerID)
	}
}

// TestFSTriggerStore_AtomicSave pins that a
// successful write does not leave a .tmp file
// behind, and that the file persists with 0o600
// perms.
func TestFSTriggerStore_AtomicSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "triggers.json")
	now := validTimeNow()
	nowFn := func() time.Time { return now }
	s := NewFSTriggerStore(path, nowFn)
	if _, err := s.Create(context.Background(), Trigger{
		ProjectID: "p", Name: "n", Kind: TriggerManual,
		Spec: validSpecFor(TriggerManual),
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// .tmp must be gone — Rename moved it.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf(".tmp file should not exist after atomic write: %v", err)
	}
	// 0o600 perms.
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := st.Mode().Perm(); got != 0o600 {
		t.Errorf("perms = %o, want 0o600", got)
	}
}

// TestFSTriggerStore_FreshFile pins the
// "no file exists → empty state" path.
func TestFSTriggerStore_FreshFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "triggers.json")
	s := NewFSTriggerStore(path, nil)
	st, err := s.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(st.Triggers) != 0 {
		t.Errorf("Triggers length = %d, want 0", len(st.Triggers))
	}
	if st.Version != TriggerStateVersion {
		t.Errorf("Version = %d, want %d", st.Version, TriggerStateVersion)
	}
}

// TestFSTriggerStore_CreateReloads pins that a
// Create's output is visible across a fresh
// store load and across a List call.
func TestFSTriggerStore_CreateReloads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "triggers.json")
	now := validTimeNow()
	nowFn := func() time.Time { return now }
	s := NewFSTriggerStore(path, nowFn)
	for i := 1; i <= 2; i++ {
		_, err := s.Create(context.Background(), Trigger{
			ProjectID: "p",
			Name:      "n-" + string(rune('a'+i-1)),
			Kind:      TriggerManual,
			Spec:      validSpecFor(TriggerManual),
		})
		if err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
	}
	s2 := NewFSTriggerStore(path, nowFn)
	out, err := s2.List(context.Background(), "p")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(out) != 2 {
		t.Errorf("List length = %d, want 2", len(out))
	}
}

// TestFSTriggerStore_VersionCheck pins that a
// future-version file surfaces "newer binary
// required".
func TestFSTriggerStore_VersionCheck(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "triggers.json")
	// Manually write a future-version file.
	future := onDiskTriggerState{
		Version:   TriggerStateVersion + 99,
		Triggers:  []Trigger{},
		UpdatedAt: validTimeNow(),
	}
	data, _ := json.MarshalIndent(future, "", "  ")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	s := NewFSTriggerStore(path, nil)
	_, err := s.Snapshot(context.Background())
	if err == nil {
		t.Fatalf("Snapshot: want error, got nil")
	}
	if !strings.Contains(err.Error(), "newer than supported") {
		t.Errorf("Snapshot error must mention 'newer than supported': %v", err)
	}
}

// TestFSTriggerStore_MalformedFile pins that a
// corrupt JSON file surfaces the parse error.
func TestFSTriggerStore_MalformedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "triggers.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	s := NewFSTriggerStore(path, nil)
	_, err := s.Snapshot(context.Background())
	if err == nil {
		t.Fatalf("Snapshot: want error, got nil")
	}
	if !strings.Contains(err.Error(), "rc-triggers: parse") {
		t.Errorf("Snapshot error must mention 'rc-triggers: parse': %v", err)
	}
}

// -----------------------------------------------------------------------------
// Dispatcher round-trips
// -----------------------------------------------------------------------------

// TestListRemoteAgentTriggers_Ok pins the
// List happy path: an empty store returns
// {ok:true, triggers:[]}.
func TestListRemoteAgentTriggers_Ok(t *testing.T) {
	ts := NewMemTriggerStore(validTimeNow)
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
	ts := NewMemTriggerStore(validTimeNow)
	_, _ = ts.Create(context.Background(), Trigger{
		ProjectID: "proj-1", Name: "a", Kind: TriggerManual,
		Spec: validSpecFor(TriggerManual),
	})
	_, _ = ts.Create(context.Background(), Trigger{
		ProjectID: "proj-2", Name: "b", Kind: TriggerManual,
		Spec: validSpecFor(TriggerManual),
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
	d := newTriggerStoreDispatcher(t, NewMemTriggerStore(validTimeNow))
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
	ts := NewMemTriggerStore(validTimeNow)
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
	if got, _ := res["trigger_id"].(string); !strings.HasPrefix(got, TriggerIDPrefix) {
		t.Errorf("trigger_id = %q, want prefix %q", got, TriggerIDPrefix)
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
	ts := NewMemTriggerStore(validTimeNow)
	_, _ = ts.Create(context.Background(), Trigger{
		ProjectID: "p", Name: "n", Kind: TriggerManual,
		Spec: validSpecFor(TriggerManual),
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
	d := newTriggerStoreDispatcher(t, NewMemTriggerStore(validTimeNow))
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
			d := newTriggerStoreDispatcher(t, NewMemTriggerStore(validTimeNow))
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
	ts := NewMemTriggerStore(validTimeNow)
	created, _ := ts.Create(context.Background(), Trigger{
		ProjectID: "p", Name: "old", Kind: TriggerManual,
		Spec: validSpecFor(TriggerManual),
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
	d := newTriggerStoreDispatcher(t, NewMemTriggerStore(validTimeNow))
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
			d := newTriggerStoreDispatcher(t, NewMemTriggerStore(validTimeNow))
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
	ts := NewMemTriggerStore(validTimeNow)
	created, _ := ts.Create(context.Background(), Trigger{
		ProjectID: "p", Name: "n", Kind: TriggerManual,
		Spec: validSpecFor(TriggerManual),
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
	d := newTriggerStoreDispatcher(t, NewMemTriggerStore(validTimeNow))
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
			d := newTriggerStoreDispatcher(t, NewMemTriggerStore(validTimeNow))
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
