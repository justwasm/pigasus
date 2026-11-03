// Package remotecontrol — conversation_config_test.go: the
// dispatcher's round-trip against the conversation-config
// domain.
//
// The domain's own tests live in
// internal/remotecontrol/convconfig; this file exercises the
// wire the dispatcher produces.
package remotecontrol

import (
	"context"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/remotecontrol/convconfig"
)

// newConversationConfigDispatcher returns a
// Dispatcher with the supplied
// convconfig.Store pre-installed. The helper
// mirrors newFastpushDispatcher / newResourcesDispatcher.
//
// When ps is nil, NewDispatcher fabricates a fresh
// empty convconfig.MemStore (the nil-guard
// pattern rc-conversation-config adds). Tests that
// want to assert "no store installed" pass nil and
// exercise the -32603 path; tests that want a
// specific store pass it directly.
func newConversationConfigDispatcher(t *testing.T, ps convconfig.Store) *Dispatcher {
	t.Helper()
	sess := NewSessionState("1.4.0")
	d, err := NewDispatcher(Config{
		Session:                 sess,
		ConversationConfigStore: ps,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	return d
}

// dispatchConversationConfig sends an
// UpsertRemoteAgentConversationConfig request
// through the dispatcher and returns the
// (result, error) pair. Mirrors dispatchFastpush /
// dispatchResources.
//
// nil params marshals to JSON `null` so the
// dispatcher's "len(params) > 0" guards see a
// present (but null) params block; jsonRPCFor
// takes a byte slice and appends verbatim, so we
// mirror the convention `[]byte("null")` rather
// than a missing-key shape.
func dispatchConversationConfig(t *testing.T, d *Dispatcher, method string, params any) (map[string]any, map[string]any) {
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

// TestUpsertRemoteAgentConversationConfig_Ok pins
// the happy-path in-process dispatch.
func TestUpsertRemoteAgentConversationConfig_Ok(t *testing.T) {
	cs := convconfig.NewMemStore(nil)
	d := newConversationConfigDispatcher(t, cs)
	res, errObj := dispatchConversationConfig(t, d, "UpsertRemoteAgentConversationConfig", map[string]any{
		"conversation_id": "conv_abc",
		"config": map[string]any{
			"model":        "claude-3-7-sonnet",
			"model_effort": "medium",
			"max_turns":    50,
		},
	})
	if errObj != nil {
		t.Fatalf("Upsert: error = %#v", errObj)
	}
	if got, _ := res["conversation_id"].(string); got != "conv_abc" {
		t.Errorf("conversation_id = %q, want conv_abc", got)
	}
	if got, _ := res["created"].(bool); !got {
		t.Errorf("created = false, want true (first upsert)")
	}
	if got, _ := res["ok"].(bool); !got {
		t.Errorf("ok = false, want true")
	}
	cfg, ok := res["config"].(convconfig.Config)
	if !ok {
		t.Fatalf("config is not a convconfig.Config, got %T", res["config"])
	}
	if cfg.Model != "claude-3-7-sonnet" {
		t.Errorf("config.model = %q, want claude-3-7-sonnet", cfg.Model)
	}
	if cfg.ModelEffort != "medium" {
		t.Errorf("config.model_effort = %q, want medium", cfg.ModelEffort)
	}
	if cfg.MaxTurns != 50 {
		t.Errorf("config.max_turns = %d, want 50", cfg.MaxTurns)
	}
}

// TestUpsertRemoteAgentConversationConfig_Update
// pins the second-call created=false semantic.
func TestUpsertRemoteAgentConversationConfig_Update(t *testing.T) {
	cs := convconfig.NewMemStore(nil)
	d := newConversationConfigDispatcher(t, cs)

	// First upsert: created=true.
	res, _ := dispatchConversationConfig(t, d, "UpsertRemoteAgentConversationConfig", map[string]any{
		"conversation_id": "conv_abc",
		"config": map[string]any{
			"model": "claude-3-7-sonnet",
		},
	})
	if got, _ := res["created"].(bool); !got {
		t.Errorf("first call created = false, want true")
	}

	// Second upsert: created=false.
	res, errObj := dispatchConversationConfig(t, d, "UpsertRemoteAgentConversationConfig", map[string]any{
		"conversation_id": "conv_abc",
		"config": map[string]any{
			"model": "gemini-3.8-flash",
		},
	})
	if errObj != nil {
		t.Fatalf("Upsert(update): error = %#v", errObj)
	}
	if got, _ := res["created"].(bool); got {
		t.Errorf("second call created = true, want false")
	}
	cfg, _ := res["config"].(convconfig.Config)
	if cfg.Model != "gemini-3.8-flash" {
		t.Errorf("update config.model = %q, want gemini-3.8-flash", cfg.Model)
	}
}

// TestUpsertRemoteAgentConversationConfig_BadModelEffort
// pins the -32602 envelope for an unknown
// model_effort value. The validator rejects the
// value before the store call.
func TestUpsertRemoteAgentConversationConfig_BadModelEffort(t *testing.T) {
	cs := convconfig.NewMemStore(nil)
	d := newConversationConfigDispatcher(t, cs)
	_, errObj := dispatchConversationConfig(t, d, "UpsertRemoteAgentConversationConfig", map[string]any{
		"conversation_id": "conv_abc",
		"config": map[string]any{
			"model_effort": "ultra",
		},
	})
	if errObj == nil {
		t.Fatalf("Upsert(bad effort): no error envelope")
	}
	if got, _ := errObj["code"].(int); got != -32602 {
		t.Errorf("error.code = %v, want -32602", got)
	}
	if got, _ := errObj["message"].(string); got != "Invalid params" {
		t.Errorf("error.message = %q, want %q", got, "Invalid params")
	}
	if got, _ := errObj["data"].(string); !strings.Contains(got, "model_effort") {
		t.Errorf("error.data = %q, want mentioning model_effort", got)
	}
}

// TestUpsertRemoteAgentConversationConfig_BadTemperature
// pins the -32602 envelope for an out-of-range
// temperature.
func TestUpsertRemoteAgentConversationConfig_BadTemperature(t *testing.T) {
	cs := convconfig.NewMemStore(nil)
	d := newConversationConfigDispatcher(t, cs)
	_, errObj := dispatchConversationConfig(t, d, "UpsertRemoteAgentConversationConfig", map[string]any{
		"conversation_id": "conv_abc",
		"config": map[string]any{
			"temperature": 3.5,
		},
	})
	if errObj == nil {
		t.Fatalf("Upsert(bad temperature): no error envelope")
	}
	if got, _ := errObj["code"].(int); got != -32602 {
		t.Errorf("error.code = %v, want -32602", got)
	}
}

// TestUpsertRemoteAgentConversationConfig_BadMaxTurns
// pins the -32602 envelope for an out-of-range
// max_turns.
func TestUpsertRemoteAgentConversationConfig_BadMaxTurns(t *testing.T) {
	cs := convconfig.NewMemStore(nil)
	d := newConversationConfigDispatcher(t, cs)
	_, errObj := dispatchConversationConfig(t, d, "UpsertRemoteAgentConversationConfig", map[string]any{
		"conversation_id": "conv_abc",
		"config": map[string]any{
			"max_turns": 9999,
		},
	})
	if errObj == nil {
		t.Fatalf("Upsert(bad max_turns): no error envelope")
	}
	if got, _ := errObj["code"].(int); got != -32602 {
		t.Errorf("error.code = %v, want -32602", got)
	}
}

// TestUpsertRemoteAgentConversationConfig_BadParams_EmptyConvID
// pins the -32602 envelope for an empty / missing
// conversation_id.
func TestUpsertRemoteAgentConversationConfig_BadParams_EmptyConvID(t *testing.T) {
	cs := convconfig.NewMemStore(nil)
	d := newConversationConfigDispatcher(t, cs)
	cases := []map[string]any{
		{"conversation_id": "", "config": map[string]any{"model": "x"}},
		{"conversation_id": "   ", "config": map[string]any{"model": "x"}},
	}
	for _, params := range cases {
		_, errObj := dispatchConversationConfig(t, d, "UpsertRemoteAgentConversationConfig", params)
		if errObj == nil {
			t.Fatalf("Upsert(empty conv id): no error envelope")
		}
		if got, _ := errObj["code"].(int); got != -32602 {
			t.Errorf("error.code = %v, want -32602", got)
		}
	}
}

// TestUpsertRemoteAgentConversationConfig_BadParams_MissingConvID
// pins the -32602 envelope for a params block
// without a conversation_id key at all.
func TestUpsertRemoteAgentConversationConfig_BadParams_MissingConvID(t *testing.T) {
	cs := convconfig.NewMemStore(nil)
	d := newConversationConfigDispatcher(t, cs)
	_, errObj := dispatchConversationConfig(t, d, "UpsertRemoteAgentConversationConfig", map[string]any{
		"config": map[string]any{"model": "x"},
	})
	if errObj == nil {
		t.Fatalf("Upsert(missing conv id): no error envelope")
	}
	if got, _ := errObj["code"].(int); got != -32602 {
		t.Errorf("error.code = %v, want -32602", got)
	}
}

// TestUpsertRemoteAgentConversationConfig_BadParams_MissingConfig
// pins the -32602 envelope for a params block
// without a config key at all (the brief requires
// config as a JSON object, not null).
func TestUpsertRemoteAgentConversationConfig_BadParams_MissingConfig(t *testing.T) {
	cs := convconfig.NewMemStore(nil)
	d := newConversationConfigDispatcher(t, cs)
	_, errObj := dispatchConversationConfig(t, d, "UpsertRemoteAgentConversationConfig", map[string]any{
		"conversation_id": "conv_abc",
	})
	if errObj == nil {
		t.Fatalf("Upsert(missing config): no error envelope")
	}
	if got, _ := errObj["code"].(int); got != -32602 {
		t.Errorf("error.code = %v, want -32602", got)
	}
}

// TestUpsertRemoteAgentConversationConfig_BadParams_NullConfig
// pins the -32602 envelope for a params block
// with config:null (the brief requires config as
// a JSON object, not null).
func TestUpsertRemoteAgentConversationConfig_BadParams_NullConfig(t *testing.T) {
	cs := convconfig.NewMemStore(nil)
	d := newConversationConfigDispatcher(t, cs)
	_, errObj := dispatchConversationConfig(t, d, "UpsertRemoteAgentConversationConfig", map[string]any{
		"conversation_id": "conv_abc",
		"config":          nil,
	})
	if errObj == nil {
		t.Fatalf("Upsert(null config): no error envelope")
	}
	if got, _ := errObj["code"].(int); got != -32602 {
		t.Errorf("error.code = %v, want -32602", got)
	}
}

// TestUpsertRemoteAgentConversationConfig_BadParams_BadConvIDType
// pins the -32602 envelope for a numeric
// conversation_id (the dispatcher rejects
// non-string field types before the empty-check).
func TestUpsertRemoteAgentConversationConfig_BadParams_BadConvIDType(t *testing.T) {
	cs := convconfig.NewMemStore(nil)
	d := newConversationConfigDispatcher(t, cs)
	// Hand-craft the JSON so the numeric value
	// doesn't get auto-coerced by Go's
	// unmarshal path.
	req := jsonRPCFor(1, "UpsertRemoteAgentConversationConfig", []byte(`{"conversation_id":42,"config":{"model":"x"}}`))
	resp := d.Dispatch(context.Background(), req)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("Upsert(numeric conv id): no error envelope, got %#v", resp)
	}
	if got, _ := errObj["code"].(int); got != -32602 {
		t.Errorf("error.code = %v, want -32602", got)
	}
}

// TestUpsertRemoteAgentConversationConfig_NoStore
// pins the -32603 envelope for a dispatcher built
// without a convconfig.Store. (The brief
// only emits -32603 when the store is nil; the
// nil-guard in NewDispatcher fabricates a fresh
// store, so this test explicitly clears the field
// post-construction.)
func TestUpsertRemoteAgentConversationConfig_NoStore(t *testing.T) {
	d := newConversationConfigDispatcher(t, nil)
	d.cfg.ConversationConfigStore = nil // explicit clear
	_, errObj := dispatchConversationConfig(t, d, "UpsertRemoteAgentConversationConfig", map[string]any{
		"conversation_id": "conv_abc",
		"config":          map[string]any{"model": "x"},
	})
	if errObj == nil {
		t.Fatalf("Upsert(no store): no error envelope")
	}
	if got, _ := errObj["code"].(int); got != -32603 {
		t.Errorf("error.code = %v, want -32603", got)
	}
}
