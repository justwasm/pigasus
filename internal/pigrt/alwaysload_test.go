package pigrt_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/justwasm/pigasus/internal/pigrt"
)

// TestParseToolsListAlwaysLoadTrue verifies that an entry whose
// `_meta` carries {"antigravity/alwaysLoad": true} lands in the
// AlwaysLoad slice, with the surrounding fields preserved.
func TestParseToolsListAlwaysLoadTrue(t *testing.T) {
	payload := []byte(`{
		"tools": [
			{
				"name": "echo",
				"description": "Echo the text back.",
				"inputSchema": {"type": "object", "properties": {"text": {"type": "string"}}},
				"_meta": {"antigravity/alwaysLoad": true}
			}
		]
	}`)
	parsed, err := pigrt.ParseToolsList(payload)
	if err != nil {
		t.Fatalf("ParseToolsList: %v", err)
	}
	if len(parsed.AlwaysLoad) != 1 {
		t.Fatalf("AlwaysLoad: got %d entries, want 1 (%+v)", len(parsed.AlwaysLoad), parsed.AlwaysLoad)
	}
	if len(parsed.Discoverable) != 0 {
		t.Fatalf("Discoverable: got %d entries, want 0 (%+v)", len(parsed.Discoverable), parsed.Discoverable)
	}
	tool := parsed.AlwaysLoad[0]
	if tool.Name != "echo" {
		t.Errorf("AlwaysLoad[0].Name = %q, want %q", tool.Name, "echo")
	}
	if tool.Description != "Echo the text back." {
		t.Errorf("AlwaysLoad[0].Description = %q, want %q", tool.Description, "Echo the text back.")
	}
	if len(tool.InputSchema) == 0 {
		t.Errorf("AlwaysLoad[0].InputSchema: empty, want the JSON schema preserved")
	}
	if !tool.AlwaysLoad {
		t.Errorf("AlwaysLoad[0].AlwaysLoad = false, want true")
	}
}

// TestParseToolsListNoMeta verifies that an entry without a `_meta`
// field lands in Discoverable and does not panic / error on a missing
// meta bag.
func TestParseToolsListNoMeta(t *testing.T) {
	payload := []byte(`{
		"tools": [
			{"name": "echo", "description": "Echo the text back."},
			{"name": "search", "inputSchema": {"type": "object"}}
		]
	}`)
	parsed, err := pigrt.ParseToolsList(payload)
	if err != nil {
		t.Fatalf("ParseToolsList: %v", err)
	}
	if len(parsed.AlwaysLoad) != 0 {
		t.Fatalf("AlwaysLoad: got %d entries, want 0 (%+v)", len(parsed.AlwaysLoad), parsed.AlwaysLoad)
	}
	if len(parsed.Discoverable) != 2 {
		t.Fatalf("Discoverable: got %d entries, want 2 (%+v)", len(parsed.Discoverable), parsed.Discoverable)
	}
	names := []string{parsed.Discoverable[0].Name, parsed.Discoverable[1].Name}
	want := []string{"echo", "search"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("Discoverable names = %v, want %v", names, want)
	}
	if parsed.Discoverable[0].AlwaysLoad || parsed.Discoverable[1].AlwaysLoad {
		t.Errorf("Discoverable entries must carry AlwaysLoad=false, got %+v", parsed.Discoverable)
	}
}

// TestParseToolsListAlwaysLoadFalse verifies that an explicit
// `antigravity/alwaysLoad: false` keeps the tool in Discoverable.
// The reference's protocol treats anything other than literal true as
// "no flag"; false must not be honoured as "off and stay off".
func TestParseToolsListAlwaysLoadFalse(t *testing.T) {
	payload := []byte(`{
		"tools": [
			{
				"name": "echo",
				"description": "Echo the text back.",
				"_meta": {"antigravity/alwaysLoad": false}
			}
		]
	}`)
	parsed, err := pigrt.ParseToolsList(payload)
	if err != nil {
		t.Fatalf("ParseToolsList: %v", err)
	}
	if len(parsed.AlwaysLoad) != 0 {
		t.Fatalf("AlwaysLoad: got %d entries, want 0 (false is not honoured)", len(parsed.AlwaysLoad))
	}
	if len(parsed.Discoverable) != 1 {
		t.Fatalf("Discoverable: got %d entries, want 1", len(parsed.Discoverable))
	}
	if parsed.Discoverable[0].AlwaysLoad {
		t.Errorf("AlwaysLoad on a flag=false entry should be false")
	}
}

// TestParseToolsListMixedMultipleServers verifies the split survives
// a response that interleaves flag-set, flag-not-set, and flag-false
// entries, all in one payload. The relative order is preserved.
func TestParseToolsListMixedMultipleServers(t *testing.T) {
	payload := []byte(`{
		"tools": [
			{"name": "alpha", "_meta": {"antigravity/alwaysLoad": true}},
			{"name": "beta"},
			{"name": "gamma", "_meta": {"antigravity/alwaysLoad": false}},
			{"name": "delta", "_meta": {"antigravity/alwaysLoad": true}},
			{"name": "epsilon", "_meta": {"unrelated": "value"}}
		]
	}`)
	parsed, err := pigrt.ParseToolsList(payload)
	if err != nil {
		t.Fatalf("ParseToolsList: %v", err)
	}
	if len(parsed.AlwaysLoad) != 2 {
		t.Fatalf("AlwaysLoad: got %d entries, want 2 (%+v)", len(parsed.AlwaysLoad), parsed.AlwaysLoad)
	}
	if len(parsed.Discoverable) != 3 {
		t.Fatalf("Discoverable: got %d entries, want 3 (%+v)", len(parsed.Discoverable), parsed.Discoverable)
	}
	wantAlways := []string{"alpha", "delta"}
	gotAlways := make([]string, 0, len(parsed.AlwaysLoad))
	for _, spec := range parsed.AlwaysLoad {
		gotAlways = append(gotAlways, spec.Name)
	}
	if !reflect.DeepEqual(gotAlways, wantAlways) {
		t.Errorf("AlwaysLoad order = %v, want %v", gotAlways, wantAlways)
	}
	wantDisc := []string{"beta", "gamma", "epsilon"}
	gotDisc := make([]string, 0, len(parsed.Discoverable))
	for _, spec := range parsed.Discoverable {
		gotDisc = append(gotDisc, spec.Name)
	}
	if !reflect.DeepEqual(gotDisc, wantDisc) {
		t.Errorf("Discoverable order = %v, want %v", gotDisc, wantDisc)
	}
}

// TestParseToolsListAlwaysLoadWinsOverDiscoverable verifies the
// split is exclusive: a tool whose name appears once with the flag
// set never appears in Discoverable, even if a duplicate
// (fingerprint-wise) lives in another payload. The split is per
// payload; this test pins the per-payload exclusivity so a future
// refactor that conditionally adds to both slices is caught.
func TestParseToolsListAlwaysLoadWinsOverDiscoverable(t *testing.T) {
	payload := []byte(`{
		"tools": [
			{"name": "echo", "_meta": {"antigravity/alwaysLoad": true}},
			{"name": "echo", "_meta": {}}
		]
	}`)
	parsed, err := pigrt.ParseToolsList(payload)
	if err != nil {
		t.Fatalf("ParseToolsList: %v", err)
	}
	if len(parsed.AlwaysLoad) != 1 {
		t.Fatalf("AlwaysLoad: got %d entries, want 1", len(parsed.AlwaysLoad))
	}
	if len(parsed.Discoverable) != 1 {
		t.Fatalf("Discoverable: got %d entries, want 1", len(parsed.Discoverable))
	}
	if parsed.AlwaysLoad[0].Name != "echo" || !parsed.AlwaysLoad[0].AlwaysLoad {
		t.Errorf("AlwaysLoad[0] = %+v, want {echo, _, _, true}", parsed.AlwaysLoad[0])
	}
}

// TestParseToolsListEmpty verifies that an empty `tools` array is
// the empty-but-no-error case: the runtime uses it as "the server
// has not (or will not) advertise tools", not as a parse failure.
func TestParseToolsListEmpty(t *testing.T) {
	parsed, err := pigrt.ParseToolsList([]byte(`{"tools": []}`))
	if err != nil {
		t.Fatalf("ParseToolsList(empty): %v", err)
	}
	if len(parsed.AlwaysLoad) != 0 || len(parsed.Discoverable) != 0 {
		t.Errorf("empty payload produced non-empty split: %+v", parsed)
	}
	// A nil/empty payload is also a no-op (a server that has not
	// answered yet produces []byte{}).
	parsed, err = pigrt.ParseToolsList(nil)
	if err != nil {
		t.Fatalf("ParseToolsList(nil): %v", err)
	}
	if len(parsed.AlwaysLoad) != 0 || len(parsed.Discoverable) != 0 {
		t.Errorf("nil payload produced non-empty split: %+v", parsed)
	}
}

// TestParseToolsListMalformed verifies that a syntactically bad
// payload surfaces the json.Unmarshal error rather than silently
// returning an empty split. The runtime surfaces this in
// LoadedMcpConfig.Errors (or its always-load equivalent).
func TestParseToolsListMalformed(t *testing.T) {
	if _, err := pigrt.ParseToolsList([]byte(`{`)); err == nil {
		t.Errorf("ParseToolsList of a truncated object: want error, got nil")
	}
	if _, err := pigrt.ParseToolsList([]byte(`not json`)); err == nil {
		t.Errorf("ParseToolsList of garbage: want error, got nil")
	}
	if _, err := pigrt.ParseToolsList([]byte(`{"tools":"should be an array"}`)); err == nil {
		t.Errorf("ParseToolsList of an array-typed mismatch: want error, got nil")
	}
}

// TestEntryAlwaysLoadVariants is the truth-table the reference
// pinned in its decoder: only a JSON-true under
// `antigravity/alwaysLoad` counts. Strings, numbers, null, missing
// keys, and malformed _meta bags all default to "not always-loaded".
func TestEntryAlwaysLoadVariants(t *testing.T) {
	cases := []struct {
		name string
		meta string
		want bool
	}{
		{"true", `{"antigravity/alwaysLoad": true}`, true},
		{"false", `{"antigravity/alwaysLoad": false}`, false},
		{"string_true", `{"antigravity/alwaysLoad": "true"}`, false},
		{"string_other", `{"antigravity/alwaysLoad": "yes"}`, false},
		{"number_one", `{"antigravity/alwaysLoad": 1}`, false},
		{"number_zero", `{"antigravity/alwaysLoad": 0}`, false},
		{"null", `{"antigravity/alwaysLoad": null}`, false},
		{"missing", `{"other": "value"}`, false},
		{"empty_object", `{}`, false},
		{"not_object", `"not an object"`, false},
		{"empty_object_literal", `null`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entry := pigrt.ToolsListEntry{Meta: json.RawMessage(tc.meta)}
			// Reuse the package-internal helper indirectly: drive
			// it through SplitToolsList so the test does not depend
			// on entryAlwaysLoad being exported.
			parsed := pigrt.SplitToolsList([]pigrt.ToolsListEntry{entry})
			got := false
			if len(parsed.AlwaysLoad) == 1 {
				got = parsed.AlwaysLoad[0].AlwaysLoad
			}
			if got != tc.want {
				t.Errorf("meta=%s: AlwaysLoad=%v, want %v", tc.meta, got, tc.want)
			}
		})
	}
}

// TestMCPName verifies the canonical PiG name composition is stable
// and exactly `mcp__<server>__<tool>`. Several test scripts (the
// MCP integration test, the cross-version binary probes) match on
// this prefix, so any change is a breaking change for those.
func TestMCPName(t *testing.T) {
	cases := []struct {
		server, tool, want string
	}{
		{"docs", "echo", "mcp__docs__echo"},
		{"docs", "search", "mcp__docs__search"},
		{"browser", "screenshot", "mcp__browser__screenshot"},
	}
	for _, tc := range cases {
		t.Run(tc.server+"_"+tc.tool, func(t *testing.T) {
			if got := pigrt.MCPName(tc.server, tc.tool); got != tc.want {
				t.Errorf("MCPName(%q, %q) = %q, want %q", tc.server, tc.tool, got, tc.want)
			}
		})
	}
}

// TestAlwaysLoadHintsFromConfigEmpty verifies the static-hint
// reader's behaviour for a config map with no server entries — an
// expected steady-state on a fresh install.
func TestAlwaysLoadHintsFromConfigEmpty(t *testing.T) {
	got := pigrt.AlwaysLoadHintsFromConfig(map[string]pigrt.AgyMCPServer{})
	if len(got) != 0 {
		t.Errorf("empty config produced non-empty hints: %+v", got)
	}
}

// TestMergeAlwaysLoadHints covers the dynamic-wins-on-tie rule and
// verifies that a tool mentioned in both sources ends up with
// Source="dynamic". The runtime uses this label to decide which
// hint's name to display in the audit log.
func TestMergeAlwaysLoadHints(t *testing.T) {
	static := []pigrt.AlwaysLoadHint{
		{Server: "docs", Tool: "echo", Source: "static"},
		{Server: "docs", Tool: "search", Source: "static"},
	}
	dynamic := []pigrt.AlwaysLoadHint{
		{Server: "docs", Tool: "echo", Source: "dynamic"},
		{Server: "browser", Tool: "screenshot", Source: "dynamic"},
	}
	merged := pigrt.MergeAlwaysLoadHints(static, dynamic)
	if len(merged) != 3 {
		t.Fatalf("len(merged) = %d, want 3 (%+v)", len(merged), merged)
	}
	want := []pigrt.AlwaysLoadHint{
		{Server: "browser", Tool: "screenshot", Source: "dynamic"},
		{Server: "docs", Tool: "echo", Source: "dynamic"},
		{Server: "docs", Tool: "search", Source: "static"},
	}
	if !reflect.DeepEqual(merged, want) {
		t.Errorf("merged = %+v, want %+v", merged, want)
	}
}
