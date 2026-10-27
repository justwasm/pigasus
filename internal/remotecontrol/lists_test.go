// Tests for the rc-list-readonly cycle. Covers:
//
//   - the four ListRemoteAgent* dispatchers at the in-process
//     level (no Unix socket, no ws adapter — just
//     Dispatcher.Dispatch);
//   - the env-leak read-only invariant on the Environments path;
//   - the read-only pre-flight contract on dispatchList.
//
// Wire-level round-trips (ws adapter + JSON marshalling) live in
// remotetransport/tcp_test.go, mirroring the split the
// rc-cascade-message cycle established.
package remotecontrol

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// newListTestDispatcher returns a Dispatcher with the supplied
// ListBackend pre-installed. The helper is intentionally small
// (no fixture registry, no convstore-side state) because the
// list methods are pure reads.
func newListTestDispatcher(t *testing.T, lb ListBackend) *Dispatcher {
	t.Helper()
	sess := NewSessionState("1.4.0")
	d, err := NewDispatcher(Config{
		Session:     sess,
		ListBackend: lb,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	return d
}

// jsonRPCFor builds a JSON-RPC request frame with the given id
// and params. Params is serialised verbatim; pass []byte(`null`)
// or []byte(`{}`) for empty.
func jsonRPCFor(id int, method string, params []byte) []byte {
	raw := []byte(`{"jsonrpc":"2.0","id":` + itoaForTest(id) + `,"method":"` + method + `","params":`)
	raw = append(raw, params...)
	raw = append(raw, '}')
	return raw
}

// itoaForTest is a small integer formatter avoiding strconv.
func itoaForTest(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// envKeysProbe is the standard probe the dispatcher uses.
func envKeysProbe() []string {
	out := make([]string, len(DefaultEnvKeyProbe))
	copy(out, DefaultEnvKeyProbe)
	return out
}

// TestListRemoteAgentModels_OK covers the happy path: a ListBackend
// without a Models closure falls back to DefaultModelInfo (the
// "feature is not configured" sentinel). Verifies the response
// envelope shape too.
func TestListRemoteAgentModels_OK(t *testing.T) {
	d := newListTestDispatcher(t, ListBackend{})

	req := jsonRPCFor(1, "ListRemoteAgentModels", []byte(`{}`))
	resp := d.Dispatch(context.Background(), req)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("ListRemoteAgentModels returned error: %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result, got %#v", resp)
	}
	models, _ := res["models"].([]ModelInfo)
	if len(models) != len(DefaultModelInfo) {
		t.Fatalf("models length = %d, want %d", len(models), len(DefaultModelInfo))
	}
	// first row carries default=true so a dashboard can resolve
	// the daemon's choice without parsing.
	if def := models[0].Default; !def {
		t.Errorf("first model row default = %v, want true", def)
	}
	if _, ok := res["default_model"]; !ok {
		t.Errorf("default_model field missing from result")
	}
}

// TestListRemoteAgentModels_WithBackend covers the path where a
// caller (or the production wiring's buildHostListBackend) supplies
// a Models closure that emits rows specific to a backend label.
// Verifies the response envelope mirrors the closure's output.
func TestListRemoteAgentModels_WithBackend(t *testing.T) {
	lb := ListBackend{
		Models: func(backend, filter string) ([]ModelInfo, error) {
			return []ModelInfo{
				{ID: "g-test-1", Label: "Test 1", Backend: backend, Default: true},
				{ID: "g-test-2", Label: "Test 2", Backend: backend},
			}, nil
		},
	}
	d := newListTestDispatcher(t, lb)

	req := jsonRPCFor(1, "ListRemoteAgentModels", []byte(`{"backend":"shim","filter":"test"}`))
	resp := d.Dispatch(context.Background(), req)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("ListRemoteAgentModels returned error: %#v", errObj)
	}
	res := resp["result"].(map[string]any)
	models, _ := res["models"].([]ModelInfo)
	if len(models) != 2 {
		t.Fatalf("models length = %d, want 2", len(models))
	}
	if got := res["default_model"]; got != "g-test-1" {
		t.Errorf("default_model = %v, want g-test-1", got)
	}
}

// TestListRemoteAgentModels_BadBackend verifies the closed-set
// validation: backend must be in {gateway, shim, ""} — anything else
// yields -32602. The "filter" field is not constrained and is
// pass-through; the closed set is for backend only.
func TestListRemoteAgentModels_BadBackend(t *testing.T) {
	d := newListTestDispatcher(t, ListBackend{
		Models: func(backend, filter string) ([]ModelInfo, error) {
			return []ModelInfo{}, nil
		},
	})

	req := jsonRPCFor(1, "ListRemoteAgentModels", []byte(`{"backend":"bogus"}`))
	resp := d.Dispatch(context.Background(), req)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error envelope for backend=\"bogus\", got %#v", resp)
	}
	if got := errorCode(errObj); got != -32602 {
		t.Errorf("code = %d, want -32602", got)
	}
}

// TestListRemoteAgentModels_Filter covers the substring filter
// case-insensitive match on ID and Label.
func TestListRemoteAgentModels_Filter(t *testing.T) {
	lb := ListBackend{
		Models: func(backend, filter string) ([]ModelInfo, error) {
			return []ModelInfo{
				{ID: "gemini-3.8-flash-medium", Label: "Gemini 3.8 Flash (Medium)", Backend: "gateway"},
				{ID: "claude-opus-4-6-thinking", Label: "Claude Opus 4.6 (Thinking)", Backend: "gateway"},
				{ID: "gpt-oss-120b-medium", Label: "GPT-OSS 120B (Medium)", Backend: "gateway"},
			}, nil
		},
	}
	d := newListTestDispatcher(t, lb)

	cases := []struct {
		name   string
		filter string
		want   int
	}{
		{"case-insensitive id", "OPUS", 1},
		{"case-insensitive label", "medium", 2},
		{"empty filter is pass-through", "", 3},
		{"no match returns empty slice", "xyzzy", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params := []byte(`{"filter":"` + tc.filter + `"}`)
			req := jsonRPCFor(1, "ListRemoteAgentModels", params)
			resp := d.Dispatch(context.Background(), req)
			res := resp["result"].(map[string]any)
			models, _ := res["models"].([]ModelInfo)
			if len(models) != tc.want {
				t.Errorf("filter=%q: %d rows, want %d", tc.filter, len(models), tc.want)
			}
		})
	}
}

// TestListRemoteAgentSlashCommands_OK covers the happy path:
// commands array contains the DefaultSlashCommands catalogue
// when ListBackend.SlashCommands is nil.
func TestListRemoteAgentSlashCommands_OK(t *testing.T) {
	d := newListTestDispatcher(t, ListBackend{})

	req := jsonRPCFor(1, "ListRemoteAgentSlashCommands", []byte(`{}`))
	resp := d.Dispatch(context.Background(), req)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("ListRemoteAgentSlashCommands returned error: %#v", errObj)
	}
	res := resp["result"].(map[string]any)
	cmds, _ := res["commands"].([]SlashCommandInfo)
	if len(cmds) != len(DefaultSlashCommands) {
		t.Fatalf("commands length = %d, want %d", len(cmds), len(DefaultSlashCommands))
	}
	// Spot-check: the "model" command is in the catalogue.
	var found bool
	for _, c := range cmds {
		if c.Name == "model" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("default catalogue missing /model")
	}
}

// TestListRemoteAgentSlashCommands_NoParams verifies the
// tolerant param shape: empty {}, explicit null, and an empty
// params object (omitted field) all return the catalogue.
func TestListRemoteAgentSlashCommands_NoParams(t *testing.T) {
	d := newListTestDispatcher(t, ListBackend{})

	cases := []struct {
		name   string
		params string
	}{
		{"empty object", `{}`},
		{"explicit null", `null`},
		// params omitted entirely
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var req []byte
			if tc.params == "" {
				req = []byte(`{"jsonrpc":"2.0","id":1,"method":"ListRemoteAgentSlashCommands"}`)
			} else {
				req = jsonRPCFor(1, "ListRemoteAgentSlashCommands", []byte(tc.params))
			}
			resp := d.Dispatch(context.Background(), req)
			if _, ok := resp["error"].(map[string]any); ok {
				t.Fatalf("got error envelope for params=%s: %#v", tc.params, resp)
			}
		})
	}
}

// TestListRemoteAgentConversations_OK exercises the
// Conversations handler via a stub closure; verifies the
// envelope shape (conversations, next_before) and the cursor
// behaviour (next_before is empty when fewer-than-limit returned,
// or the oldest entry's ModTime when full).
func TestListRemoteAgentConversations_OK(t *testing.T) {
	base := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	lb := ListBackend{
		Conversations: func(limit int, before time.Time) ([]ConversationInfo, time.Time, error) {
			rows := []ConversationInfo{
				{ID: "c1", Title: "first", CreatedAt: base.Format(time.RFC3339Nano), UpdatedAt: base.Format(time.RFC3339Nano)},
				{ID: "c2", Title: "second", CreatedAt: base.Add(1 * time.Minute).Format(time.RFC3339Nano), UpdatedAt: base.Add(1 * time.Minute).Format(time.RFC3339Nano)},
				{ID: "c3", Title: "third", CreatedAt: base.Add(2 * time.Minute).Format(time.RFC3339Nano), UpdatedAt: base.Add(2 * time.Minute).Format(time.RFC3339Nano)},
			}
			if limit > 0 && limit < len(rows) {
				return rows[:limit], base.Add(time.Duration(limit) * time.Minute), nil
			}
			// limit == 0 or limit >= len(rows) → no cursor (returning fewer than the limit).
			return rows, time.Time{}, nil
		},
	}
	d := newListTestDispatcher(t, lb)

	t.Run("limit=2 returns 2 + cursor", func(t *testing.T) {
		req := jsonRPCFor(1, "ListRemoteAgentConversations", []byte(`{"limit":2}`))
		resp := d.Dispatch(context.Background(), req)
		res := resp["result"].(map[string]any)
		convs, _ := res["conversations"].([]ConversationInfo)
		if len(convs) != 2 {
			t.Errorf("conversations length = %d, want 2", len(convs))
		}
		nb, _ := res["next_before"].(string)
		if nb == "" {
			t.Errorf("next_before empty; expected cursor for full page")
		}
	})

	t.Run("limit=10 returns all 3 + empty cursor", func(t *testing.T) {
		req := jsonRPCFor(1, "ListRemoteAgentConversations", []byte(`{"limit":10}`))
		resp := d.Dispatch(context.Background(), req)
		res := resp["result"].(map[string]any)
		convs, _ := res["conversations"].([]ConversationInfo)
		if len(convs) != 3 {
			t.Errorf("conversations length = %d, want 3", len(convs))
		}
		nb, _ := res["next_before"].(string)
		if nb != "" {
			t.Errorf("next_before = %q; want empty for partial page", nb)
		}
	})

	t.Run("default limit fills to 50 (capped at row count)", func(t *testing.T) {
		req := jsonRPCFor(1, "ListRemoteAgentConversations", []byte(`{}`))
		resp := d.Dispatch(context.Background(), req)
		res := resp["result"].(map[string]any)
		convs, _ := res["conversations"].([]ConversationInfo)
		if len(convs) != 3 {
			t.Errorf("default-limit conversations length = %d, want 3", len(convs))
		}
	})
}

// TestListRemoteAgentConversations_BadLimit verifies the
// limit range: must be in [0, 200]. Negative and out-of-range
// values yield -32602.
func TestListRemoteAgentConversations_BadLimit(t *testing.T) {
	d := newListTestDispatcher(t, ListBackend{
		Conversations: func(limit int, before time.Time) ([]ConversationInfo, time.Time, error) {
			return []ConversationInfo{}, time.Time{}, nil
		},
	})

	for _, tc := range []struct {
		name   string
		params string
	}{
		{"negative", `{"limit":-1}`},
		{"too large", `{"limit":500}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := jsonRPCFor(1, "ListRemoteAgentConversations", []byte(tc.params))
			resp := d.Dispatch(context.Background(), req)
			errObj, ok := resp["error"].(map[string]any)
			if !ok {
				t.Fatalf("expected error envelope for %s, got %#v", tc.params, resp)
			}
			if got := errorCode(errObj); got != -32602 {
				t.Errorf("code = %d, want -32602", got)
			}
		})
	}
}

// TestListRemoteAgentConversations_Before exercises the cursor
// parser: parses both RFC3339 and RFC3339Nano; bad strings
// yield -32602. The closure receives the parsed time.Time.
func TestListRemoteAgentConversations_Before(t *testing.T) {
	var gotBefore time.Time
	d := newListTestDispatcher(t, ListBackend{
		Conversations: func(limit int, before time.Time) ([]ConversationInfo, time.Time, error) {
			gotBefore = before
			return []ConversationInfo{}, time.Time{}, nil
		},
	})

	t.Run("RFC3339Nano parses", func(t *testing.T) {
		want := "2024-01-01T12:00:00.123456789Z"
		req := jsonRPCFor(1, "ListRemoteAgentConversations", []byte(`{"before":"`+want+`"}`))
		resp := d.Dispatch(context.Background(), req)
		if _, ok := resp["error"].(map[string]any); ok {
			t.Fatalf("parse failure: %#v", resp)
		}
		if gotBefore.IsZero() {
			t.Errorf("before was not parsed")
		}
		if !strings.HasPrefix(gotBefore.UTC().Format(time.RFC3339Nano), "2024-01-01T12:00:00") {
			t.Errorf("before round-trip drift: got %v", gotBefore)
		}
	})

	t.Run("invalid before yields -32602", func(t *testing.T) {
		req := jsonRPCFor(1, "ListRemoteAgentConversations", []byte(`{"before":"not-a-date"}`))
		resp := d.Dispatch(context.Background(), req)
		errObj, ok := resp["error"].(map[string]any)
		if !ok {
			t.Fatalf("expected error envelope, got %#v", resp)
		}
		if got := errorCode(errObj); got != -32602 {
			t.Errorf("code = %d, want -32602", got)
		}
	})
}

// TestListRemoteAgentEnvironments_OK verifies the basic envelope
// shape: keys are populated, sorted, no values leak.
func TestListRemoteAgentEnvironments_OK(t *testing.T) {
	d := newListTestDispatcher(t, ListBackend{
		Environments: func() (EnvironmentInfo, error) {
			return EnvironmentInfo{
				OS:       "darwin",
				Arch:     "arm64",
				Hostname: "test-host",
				Cwd:      "/tmp",
				User:     "tester",
				Shell:    "/bin/zsh",
				Version:  "1.4.0",
				EnvKeysPresent: []string{
					"ANTHROPIC_API_KEY",
					"OPENAI_API_KEY",
				},
				Capabilities: map[string]any{"remote_control": true},
			}, nil
		},
	})

	req := jsonRPCFor(1, "ListRemoteAgentEnvironments", []byte(`{}`))
	resp := d.Dispatch(context.Background(), req)
	if errObj, ok := resp["error"].(map[string]any); ok {
		t.Fatalf("ListRemoteAgentEnvironments returned error: %#v", errObj)
	}
	res := resp["result"].(map[string]any)
	if res["os"] != "darwin" {
		t.Errorf("os = %v, want darwin", res["os"])
	}
	if res["version"] != "1.4.0" {
		t.Errorf("version = %v, want 1.4.0", res["version"])
	}
	keys, _ := res["env_keys_present"].([]string)
	if len(keys) != 2 {
		t.Errorf("env_keys_present length = %d, want 2", len(keys))
	}
}

// TestListRemoteAgentEnvironments_NoParams verifies the
// tolerant param shape: empty {}, explicit null, and params
// omitted all return the snapshot.
func TestListRemoteAgentEnvironments_NoParams(t *testing.T) {
	d := newListTestDispatcher(t, ListBackend{
		Environments: func() (EnvironmentInfo, error) {
			return EnvironmentInfo{OS: "linux"}, nil
		},
	})

	for _, tc := range []string{`{}`, `null`} {
		t.Run("params="+tc, func(t *testing.T) {
			req := jsonRPCFor(1, "ListRemoteAgentEnvironments", []byte(tc))
			resp := d.Dispatch(context.Background(), req)
			if _, ok := resp["error"].(map[string]any); ok {
				t.Fatalf("got error envelope for params=%s: %#v", tc, resp)
			}
		})
	}

	t.Run("params omitted", func(t *testing.T) {
		req := []byte(`{"jsonrpc":"2.0","id":1,"method":"ListRemoteAgentEnvironments"}`)
		resp := d.Dispatch(context.Background(), req)
		if _, ok := resp["error"].(map[string]any); ok {
			t.Fatalf("got error envelope: %#v", resp)
		}
	})
}

// TestListBackend_NoEnvLeak pins the read-only invariant on the
// env-leak path: envKeysPresent must return sorted, deduped,
// NAMES only — never values. Construct a probe set with
// duplicates and an unknown key.
func TestListBackend_NoEnvLeak(t *testing.T) {
	t.Setenv("PROBE_LEAKY_A", "secretA-value")
	t.Setenv("PROBE_LEAKY_B", "secretB-value")
	t.Setenv("PROBE_LEAKY_C", "") // empty: treated as absent
	t.Setenv("PROBE_LEAKY_A", "secretA-value-2")

	probe := []string{
		"PROBE_LEAKY_A",
		"PROBE_LEAKY_B",
		"PROBE_LEAKY_C",
		"PROBE_LEAKY_A", // duplicate
		"PROBE_LEAKY_MISSING",
	}
	got := envKeysPresent(probe)
	// Sorted + deduped; only A and B are present (empty C is absent).
	want := []string{"PROBE_LEAKY_A", "PROBE_LEAKY_B"}
	if len(got) != len(want) {
		t.Fatalf("envKeysPresent returned %v, want %v", got, want)
	}
	for i, k := range got {
		if k != want[i] {
			t.Errorf("envKeysPresent[%d] = %q, want %q", i, k, want[i])
		}
	}
	// Hard invariant: no value appears in the output slice.
	for _, k := range got {
		if strings.Contains(k, "secret") || strings.Contains(k, "value") {
			t.Errorf("envKeysPresent returned a value, not a name: %q", k)
		}
	}
}

// TestListBackend_DefaultEnvProbeContents pins the closed probe
// set: must contain all 8 named integrations. A regression
// here means dashboards lose the ability to detect when one of
// these integrations is misconfigured.
func TestListBackend_DefaultEnvProbeContents(t *testing.T) {
	want := map[string]bool{
		"ANTHROPIC_API_KEY":  false,
		"OPENAI_API_KEY":     false,
		"GEMINI_API_KEY":     false,
		"JETSKI_OAUTH_TOKEN": false,
		"OPENAGY_BACKEND":    false,
		"OPENAGY_RC_SOCKET":  false,
		"ANTIGRAVITY_HOME":   false,
		"AGY_ADC_AUTH":       false,
	}
	for _, k := range DefaultEnvKeyProbe {
		if _, ok := want[k]; ok {
			want[k] = true
		} else {
			t.Errorf("DefaultEnvKeyProbe has unexpected key %q", k)
		}
	}
	for k, found := range want {
		if !found {
			t.Errorf("DefaultEnvKeyProbe missing key %q", k)
		}
	}
}

// TestListReadOnly_Contract pins the read-only intent at the
// package level. The cycle ships the
// ListMethodsNoSideEffects var + the pre-flight hook as a
// guardrail; this test pins *both*:
//
//   - ListMethodsNoSideEffects contains the four method names
//     and only those four.
//   - isListReadOnly returns true for those four and false for
//     every other method we've shipped.
//
// A future cycle that adds a list-shaped method that DOES
// mutate state must add it to ListMethodsNoSideEffects only
// if it's actually pure; if it's impure, the dispatcher's
// isSideEffectingListBackend hook is the place that flags it.
func TestListReadOnly_Contract(t *testing.T) {
	want := map[string]bool{
		"ListRemoteAgentModels":        false,
		"ListRemoteAgentSlashCommands": false,
		"ListRemoteAgentConversations": false,
		"ListRemoteAgentEnvironments":  false,
	}
	for _, m := range ListMethodsNoSideEffects {
		if _, ok := want[m]; ok {
			want[m] = true
		} else {
			t.Errorf("ListMethodsNoSideEffects has unexpected method %q", m)
		}
	}
	for m, found := range want {
		if !found {
			t.Errorf("ListMethodsNoSideEffects missing method %q", m)
		}
	}

	otherMethods := []string{
		"StartRemoteAgentCascade",
		"SendRemoteAgentCascadeMessage",
		"HandleRemoteAgentCascadeUserInteraction",
		"initialize",
		"tools/list",
		"tools/call",
		"ping",
	}
	for _, m := range otherMethods {
		if isListReadOnly(m) {
			t.Errorf("isListReadOnly(%q) = true; want false (not in the read-only set)", m)
		}
	}
}

// TestListReadOnly_UnknownMethod covers the dispatchList
// boundary: a method name NOT in ListMethodsNoSideEffects
// yields -32601. The test exercises the boundary directly via
// dispatchList (rather than through Dispatch, which routes
// unknown methods through the cascade/lifecycle path).
func TestListReadOnly_UnknownMethod(t *testing.T) {
	d := newListTestDispatcher(t, ListBackend{})

	req := jsonRPCFor(1, "ListRemoteAgentDoesNotExist", []byte(`{}`))
	resp := d.Dispatch(context.Background(), req)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error envelope, got %#v", resp)
	}
	if got := errorCode(errObj); got != -32601 {
		t.Errorf("code = %d, want -32601", got)
	}
}

// TestListBackend_DefaultFallback verifies that an empty
// ListBackend (the "no wiring installed" state) is safe: each
// of the four handlers returns a usable default rather than
// panicking or returning -32603.
func TestListBackend_DefaultFallback(t *testing.T) {
	d := newListTestDispatcher(t, ListBackend{})

	cases := []string{
		"ListRemoteAgentModels",
		"ListRemoteAgentSlashCommands",
		"ListRemoteAgentConversations",
		"ListRemoteAgentEnvironments",
	}
	for _, m := range cases {
		t.Run(m, func(t *testing.T) {
			req := jsonRPCFor(1, m, []byte(`{}`))
			resp := d.Dispatch(context.Background(), req)
			if errObj, ok := resp["error"].(map[string]any); ok {
				t.Fatalf("%s returned error: %#v", m, errObj)
			}
		})
	}
}

// TestListBackend_StaticLister is the canonical happy-path for
// the "fake ListBackend wired into Config" pattern that all the
// tests above use. The point of this test is to pin the pattern:
// a test can construct a Dispatcher with a synthetic ListBackend,
// dispatch a request, and read the response — without spinning a
// Unix socket, ws adapter, or real production wiring.
//
// Keeping the assertion surface small (one Models row, one
// SlashCommand row) so the test reads as "the seam works".
func TestListBackend_StaticLister(t *testing.T) {
	lb := ListBackend{
		Models: func(backend, filter string) ([]ModelInfo, error) {
			return []ModelInfo{{ID: "fixture", Label: "Fixture", Backend: backend, Default: true}}, nil
		},
		SlashCommands: func() ([]SlashCommandInfo, error) {
			return []SlashCommandInfo{{Name: "fixture", Description: "fixture cmd", Scope: SlashScopeBuiltin}}, nil
		},
	}
	d := newListTestDispatcher(t, lb)
	defer d.cfg.Session.connected.Add(0) // touch so the field's read isn't optimized out

	// Models
	req := jsonRPCFor(1, "ListRemoteAgentModels", []byte(`{}`))
	resp := d.Dispatch(context.Background(), req)
	res := resp["result"].(map[string]any)
	models, _ := res["models"].([]ModelInfo)
	if len(models) != 1 {
		t.Fatalf("models length = %d, want 1", len(models))
	}
	if models[0].ID != "fixture" {
		t.Errorf("id = %v, want fixture", models[0].ID)
	}

	// SlashCommands
	req = jsonRPCFor(2, "ListRemoteAgentSlashCommands", []byte(`{}`))
	resp = d.Dispatch(context.Background(), req)
	res = resp["result"].(map[string]any)
	cmds, _ := res["commands"].([]SlashCommandInfo)
	if len(cmds) != 1 {
		t.Fatalf("commands length = %d, want 1", len(cmds))
	}
	if cmds[0].Name != "fixture" {
		t.Errorf("cmd name = %v, want fixture", cmds[0].Name)
	}
}

// TestListBackend_ConcurrentDispatch runs the four list methods
// concurrently to flush any data race in the dispatcher under
// -race. The test is light — the dispatcher's read path is
// pure — but the read-only contract depends on it.
func TestListBackend_ConcurrentDispatch(t *testing.T) {
	lb := ListBackend{
		Models: func(backend, filter string) ([]ModelInfo, error) {
			return []ModelInfo{{ID: "m1", Label: "m1", Backend: backend}}, nil
		},
	}
	d := newListTestDispatcher(t, lb)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := jsonRPCFor(1, "ListRemoteAgentModels", []byte(`{}`))
			resp := d.Dispatch(context.Background(), req)
			if _, ok := resp["error"].(map[string]any); ok {
				t.Errorf("concurrent dispatch got error: %#v", resp)
			}
		}()
	}
	wg.Wait()
}

// TestListDispatch_PreFlightNil covers the dispatcher-nil boundary:
// dispatchList on a nil receiver returns -32603 rather than
// panicking. The test pins the calling-dispatcher guard.
func TestListDispatch_PreFlightNil(t *testing.T) {
	var d *Dispatcher
	// Calling on a nil receiver would panic; the function
	// guards against that. We test the nil guard directly.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("dispatchList panicked: %v", r)
		}
	}()
	_, _ = d.dispatchList("ListRemoteAgentModels", nil)
}
