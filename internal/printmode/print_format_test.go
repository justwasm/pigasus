package printmode

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/core/settings"
)

// TestJSONEnvelopeFieldOrder pins the member order of the `--output-format
// json` envelope. It is the reference's wire order, captured from
// `agy -p=/help --output-format json` and `agy -p='say hi' --output-format json`:
//
//	conversation_id, status, response, error, duration_seconds, num_turns, usage, command
//
// A struct whose fields were reordered, or a map, would marshal differently.
func TestJSONEnvelopeFieldOrder(t *testing.T) {
	env := jsonEnvelope{
		ConversationID:  "c",
		Status:          printStatusSuccess,
		Response:        "r",
		Error:           "e",
		DurationSeconds: 1,
		NumTurns:        2,
		Usage:           jsonUsage{InputTokens: 3, OutputTokens: 4, ThinkingTokens: 5, CacheReadTokens: 6, TotalTokens: 7},
		Command:         &jsonCommandPayload{Name: "help", Data: map[string]any{"commands": []any{}}},
	}
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"conversation_id":"c","status":"SUCCESS","response":"r","error":"e",` +
		`"duration_seconds":1,"num_turns":2,` +
		`"usage":{"input_tokens":3,"output_tokens":4,"thinking_tokens":5,"cache_read_tokens":6,"total_tokens":7},` +
		`"command":{"name":"help","data":{"commands":[]}}}`
	if string(b) != want {
		t.Errorf("envelope =\n %s\nwant\n %s", b, want)
	}
}

// TestJSONEnvelopeOmitsEmptyMembers pins the two omitempty members: `error` is
// absent on the success path and `command` is absent when the answer did not
// come from a slash command.
func TestJSONEnvelopeOmitsEmptyMembers(t *testing.T) {
	b, err := json.Marshal(jsonEnvelope{Status: printStatusSuccess, Response: "x"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(s, `"error"`) {
		t.Errorf("success envelope carries error: %s", s)
	}
	if strings.Contains(s, `"command"`) {
		t.Errorf("envelope without a slash command carries command: %s", s)
	}
	// The reference reports an all-zero usage object rather than omitting it.
	if !strings.Contains(s, `"usage":{"input_tokens":0,"output_tokens":0,"thinking_tokens":0,"cache_read_tokens":0,"total_tokens":0}`) {
		t.Errorf("usage member missing or wrong: %s", s)
	}
}

// TestHelpTableMatchesCapturedText keeps the structured `/help` payload and the
// text answer in step: the table must render exactly the captured text, which
// is what the reference prints for `agy -p /help`.
func TestHelpTableMatchesCapturedText(t *testing.T) {
	var b strings.Builder
	for _, c := range printSlashHelpCommands {
		b.WriteString("/")
		b.WriteString(c.Name)
		if len(c.Aliases) > 0 {
			b.WriteString(" (" + strings.Join(c.Aliases, ", ") + ")")
		}
		b.WriteString("\t")
		b.WriteString(c.Description)
		b.WriteString("\n")
	}
	if got := b.String(); got != printSlashHelpText {
		t.Errorf("rendered help table =\n%q\nwant\n%q", got, printSlashHelpText)
	}
}

// TestConfigValueTypes pins the typed defaults the `/config` payload carries
// for keys the user has never set.
//
// 【一手】 agy 1.4.8, `agy -p /config` with an empty settings.json: the seven
// nullable keys (customModelsConfig, gcp, permissions, sandboxProxy,
// statusLine, title, trustedWorkspaces) are null — not an empty object and
// not an empty list — and the rest fall back to their built-in default.
func TestConfigValueTypes(t *testing.T) {
	cases := []struct {
		key  string
		want any
	}{
		{"agentMode", ""},
		{"allowNonWorkspaceAccess", false},
		{"altScreenMode", "default"},
		{"artifactReviewPolicy", "asks-for-review"},
		{"clearScrollbackOnResize", true},
		{"colorScheme", "terminal"},
		{"customModelsConfig", nil},
		{"editor", "auto"},
		{"gcp", nil},
		{"historySize", 2000},
		{"model", ""},
		{"permissions", nil},
		{"sandboxProxy", nil},
		{"statusLine", nil},
		{"title", nil},
		{"toolPermission", "request-review"},
		{"trustedWorkspaces", nil},
		{"verbosity", "medium"},
	}
	for _, c := range cases {
		got := configValue(c.key, nil)
		if c.want == nil {
			if got != nil {
				t.Errorf("configValue(%q) = %#v, want nil", c.key, got)
			}
			continue
		}
		gb, _ := json.Marshal(got)
		wb, _ := json.Marshal(c.want)
		if string(gb) != string(wb) {
			t.Errorf("configValue(%q) = %s, want %s", c.key, gb, wb)
		}
	}
}

// TestConfigValueNullDecodesToZero pins that an explicit null in
// settings.json decodes to the zero value of the key's type, not to the
// built-in default.
//
// 【一手】 agy 1.4.8: with `{"editor":null}` in settings.json the text dump
// prints an empty cell for `editor` and the JSON payload carries the empty
// string — not "auto". A key the reference does not know keeps its null.
func TestConfigValueNullDecodesToZero(t *testing.T) {
	if got := configValue("editor", json.RawMessage(`null`)); got != "" {
		t.Errorf("editor null = %#v, want \"\"", got)
	}
	if got := configValue("historySize", json.RawMessage(`null`)); got != 0 {
		t.Errorf("historySize null = %#v, want 0", got)
	}
	if got := configValue("showTips", json.RawMessage(`null`)); got != false {
		t.Errorf("showTips null = %#v, want false", got)
	}
	if got := configValue("zzzUnknownKey", json.RawMessage(`null`)); got != nil {
		t.Errorf("unknown key null = %#v, want nil", got)
	}
}

// TestConfigValueUsesRawJSON checks that a key the user *has* set contributes
// its parsed JSON value rather than the default — including types the default
// would get wrong (a string key set to a number stays a number).
func TestConfigValueUsesRawJSON(t *testing.T) {
	if got := configValue("historySize", json.RawMessage(`500`)); got != float64(500) {
		t.Errorf("historySize = %#v, want 500", got)
	}
	if got := configValue("title", json.RawMessage(`"hello"`)); got != "hello" {
		t.Errorf("title = %#v, want \"hello\"", got)
	}
	// A key present but explicitly null behaves like an absent one.
	if got := configValue("title", json.RawMessage(`null`)); got != nil {
		t.Errorf("title null = %#v, want nil", got)
	}
}

// TestPrintOutputFormat covers the flag's three accepted values and the
// fallback for anything else.
func TestPrintOutputFormat(t *testing.T) {
	cases := []struct {
		value string
		want  string
	}{
		{"", printFormatText},
		{"text", printFormatText},
		{"json", printFormatJSON},
		{"stream-json", printFormatStreamJSON},
		{"yaml", printFormatText},
	}
	for _, c := range cases {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.String("output-format", "", "")
		if c.value != "" {
			_ = fs.Parse([]string{"--output-format=" + c.value})
		}
		if got := printOutputFormat(fs); got != c.want {
			t.Errorf("printOutputFormat(%q) = %q, want %q", c.value, got, c.want)
		}
	}
}

// TestPrintOutputFormat_FormatAlias pins that --format is an exact alias for --output-format.
func TestPrintOutputFormat_FormatAlias(t *testing.T) {
	cases := []struct {
		value string
		want  string
	}{
		{"", printFormatText},
		{"text", printFormatText},
		{"json", printFormatJSON},
		{"stream-json", printFormatStreamJSON},
		{"yaml", printFormatText},
	}
	for _, c := range cases {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		RegisterPrintFlags(fs)
		if c.value != "" {
			if err := fs.Parse([]string{"--format=" + c.value}); err != nil {
				t.Fatalf("Parse(--format=%q): %v", c.value, err)
			}
		}
		if got := PrintOutputFormat(fs); got != c.want {
			t.Errorf("PrintOutputFormat(--format=%q) = %q, want %q", c.value, got, c.want)
		}
	}
}

// TestPrintOutputFormat_Precedence verifies that when both --output-format and --format
// are provided on the command line, --output-format wins.
func TestPrintOutputFormat_Precedence(t *testing.T) {
	cases := []struct {
		name         string
		outputFormat string
		format       string
		want         string
	}{
		{
			name:         "output-format json beats format stream-json",
			outputFormat: "json",
			format:       "stream-json",
			want:         printFormatJSON,
		},
		{
			name:         "output-format stream-json beats format json",
			outputFormat: "stream-json",
			format:       "json",
			want:         printFormatStreamJSON,
		},
		{
			name:         "output-format text beats format json",
			outputFormat: "text",
			format:       "json",
			want:         printFormatText,
		},
		{
			name:         "output-format json beats format text",
			outputFormat: "json",
			format:       "text",
			want:         printFormatJSON,
		},
		{
			name:         "output-format empty falls back to format stream-json",
			outputFormat: "",
			format:       "stream-json",
			want:         printFormatStreamJSON,
		},
		{
			name:         "output-format empty falls back to format json",
			outputFormat: "",
			format:       "json",
			want:         printFormatJSON,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs := flag.NewFlagSet("t", flag.ContinueOnError)
			RegisterPrintFlags(fs)
			var args []string
			if c.outputFormat != "" {
				args = append(args, "--output-format="+c.outputFormat)
			}
			if c.format != "" {
				args = append(args, "--format="+c.format)
			}
			if err := fs.Parse(args); err != nil {
				t.Fatalf("Parse(%v): %v", args, err)
			}
			if got := PrintOutputFormat(fs); got != c.want {
				t.Errorf("PrintOutputFormat(%v) = %q, want %q", args, got, c.want)
			}
		})
	}
}

// TestRegisterPrintFlags verifies flag registration and idempotency.
func TestRegisterPrintFlags(t *testing.T) {
	RegisterPrintFlags(nil)

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	RegisterPrintFlags(fs)

	if fs.Lookup("output-format") == nil {
		t.Fatal("expected --output-format to be registered")
	}
	if fs.Lookup("format") == nil {
		t.Fatal("expected --format to be registered")
	}

	// Calling again should not panic or duplicate flags.
	RegisterPrintFlags(fs)

	if err := fs.Parse([]string{"--format=json"}); err != nil {
		t.Fatalf("failed to parse registered --format: %v", err)
	}
	if got := PrintOutputFormat(fs); got != printFormatJSON {
		t.Errorf("PrintOutputFormat = %q, want %q", got, printFormatJSON)
	}
}

// TestPrintErrorMessage pins the `error` member: the same text as the stderr
// line, minus the `error: ` prefix. For an API failure it is the underlying
// message, not the short_error.
func TestPrintErrorMessage(t *testing.T) {
	if got := printErrorMessage(nil); got != "" {
		t.Errorf("nil = %q", got)
	}
	if got := printErrorMessage(errors.New("error: /clear is not available in print mode (x)")); got != "/clear is not available in print mode (x)" {
		t.Errorf("prefix not stripped: %q", got)
	}
	if got := printErrorMessage(errors.New("plain")); got != "plain" {
		t.Errorf("plain = %q", got)
	}
}

// TestModelEffortFromID pins the effort a catalogue id encodes.
func TestModelEffortFromID(t *testing.T) {
	cases := map[string]string{
		"gemini-3.8-flash-high":    "high",
		"gemini-3.8-flash-low":     "low",
		"gpt-oss-120b-medium":      "medium",
		"claude-sonnet-4-6":        "",
		"claude-opus-4-6-thinking": "",
	}
	for id, want := range cases {
		if got := modelEffortFromID(id); got != want {
			t.Errorf("modelEffortFromID(%q) = %q, want %q", id, got, want)
		}
	}
}

// TestNewConversationIDShape checks the conversation id is a v4 UUID, the shape
// the reference emits once a model turn starts.
func TestNewConversationIDShape(t *testing.T) {
	id := newEnvelopeConversationID()
	if len(id) != 36 {
		t.Fatalf("length = %d, want 36 (%q)", len(id), id)
	}
	if id[14] != '4' {
		t.Errorf("version nibble = %q, want 4 (%q)", id[14], id)
	}
	if c := id[19]; c != '8' && c != '9' && c != 'a' && c != 'b' {
		t.Errorf("variant nibble = %q, want 8/9/a/b (%q)", c, id)
	}
	if newEnvelopeConversationID() == id {
		t.Error("two calls returned the same id")
	}
}

// TestJSONStreamEventFieldOrder pins the four non-verbose stream-json
// event shapes and their member order, captured from 1.3.6:
//
//	{"event":"init","conversation_id":"…","init":{"cwd","tools","permission_mode"}}
//	{"event":"step_update","step_update":{"conversation_id","step_index","state","step_type"}}
//	{"event":"command_result","command":{"name","data"}}
//	{"event":"result","result":{…the json envelope…}}
//
// The fifth event — `assistant_message` — is emitted only under
// `--output-format stream-json --verbose` (AGY 1.2.7
// close-gap). TestAssistantMessageFieldOrder pins its wire shape, so
// the verbose path is covered by a dedicated test in this file
// rather than being mixed into the four-event table.
func TestJSONStreamEventFieldOrder(t *testing.T) {
	cases := []struct {
		name  string
		event jsonStreamEvent
		want  string
	}{
		{
			name: "init",
			event: jsonStreamEvent{
				Event:          streamEventInit,
				ConversationID: "c",
				Init:           &jsonInitPayload{CWD: "/tmp", Tools: []string{"a", "b"}, PermissionMode: "always-proceed"},
			},
			want: `{"event":"init","conversation_id":"c","init":{"cwd":"/tmp","tools":["a","b"],"permission_mode":"always-proceed"}}`,
		},
		{
			// The three override members travel after the three the clone
			// always carried, and only when the run overrode something
			// (`official-docs/cli/headless.md:159-168`).
			name: "init with overrides",
			event: jsonStreamEvent{
				Event:          streamEventInit,
				ConversationID: "c",
				Init: &jsonInitPayload{
					CWD:            "/tmp",
					Tools:          []string{"a"},
					PermissionMode: "request-review",
					Model:          "gemini-3-pro",
					Agent:          "reviewer",
					JSONSchema:     json.RawMessage(`{"type":"object"}`),
				},
			},
			want: `{"event":"init","conversation_id":"c","init":{"cwd":"/tmp","tools":["a"],"permission_mode":"request-review","model":"gemini-3-pro","agent":"reviewer","json_schema":{"type":"object"}}}`,
		},
		{
			name: "step_update",
			event: jsonStreamEvent{
				Event:      streamEventStepUpdate,
				StepUpdate: &jsonStepUpdate{ConversationID: "c", StepIndex: 0, State: "DONE", StepType: "user_input"},
			},
			want: `{"event":"step_update","step_update":{"conversation_id":"c","step_index":0,"state":"DONE","step_type":"user_input"}}`,
		},
		{
			// The reference's own member set: the ten the doc's table
			// lists (`official-docs/cli/headless.md:172-183`).
			name: "step_update with facts",
			event: jsonStreamEvent{
				Event: streamEventStepUpdate,
				StepUpdate: &jsonStepUpdate{
					ConversationID:  "c",
					StepIndex:       3,
					State:           "DONE",
					StepType:        "agent_response",
					ToolName:        "run_command",
					TextDelta:       "hi\n",
					DurationSeconds: 6.28,
					Usage:           &jsonUsage{InputTokens: 10302, OutputTokens: 582, ThinkingTokens: 551, CacheReadTokens: 8113, TotalTokens: 10884},
					ToolInfo:        &jsonToolInfo{Name: "run_command", Parameters: json.RawMessage(`{"command_line":"echo hi"}`), Output: "hi\n"},
					SubagentInfo:    &jsonSubagentInfo{Subagents: []jsonSubagent{{TypeName: "worker", Role: "user", ConversationID: "sub-1", LogURI: "file:///tmp/log", WorkspaceURIs: []string{"file:///tmp/ws"}}}},
				},
			},
			want: `{"event":"step_update","step_update":{"conversation_id":"c","step_index":3,"state":"DONE","step_type":"agent_response","tool_name":"run_command","text_delta":"hi\n","duration_seconds":6.28,"usage":{"input_tokens":10302,"output_tokens":582,"thinking_tokens":551,"cache_read_tokens":8113,"total_tokens":10884},"tool_info":{"name":"run_command","parameters":{"command_line":"echo hi"},"output":"hi\n"},"subagent_info":{"subagents":[{"type_name":"worker","role":"user","conversation_id":"sub-1","log_uri":"file:///tmp/log","workspace_uris":["file:///tmp/ws"]}]}}}`,
		},
		{
			name: "command_result",
			event: jsonStreamEvent{
				Event:   streamEventCommandResult,
				Command: &jsonCommandPayload{Name: "hooks", Data: map[string]any{"hooks": []any{}}},
			},
			want: `{"event":"command_result","command":{"name":"hooks","data":{"hooks":[]}}}`,
		},
		{
			name: "result",
			event: jsonStreamEvent{
				Event:  streamEventResult,
				Result: &jsonEnvelope{Status: printStatusSuccess, Response: ""},
			},
			want: `{"event":"result","result":{"conversation_id":"","status":"SUCCESS","response":"","duration_seconds":0,"num_turns":0,"usage":{"input_tokens":0,"output_tokens":0,"thinking_tokens":0,"cache_read_tokens":0,"total_tokens":0}}}`,
		},
	}
	for _, c := range cases {
		b, err := json.Marshal(c.event)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if string(b) != c.want {
			t.Errorf("%s =\n %s\nwant\n %s", c.name, b, c.want)
		}
	}
}

// TestEmitStep checks the step_update helper renders the captured event.
func TestEmitStep(t *testing.T) {
	var b strings.Builder
	if err := emitStep(&b, newStepUpdate("cid", 1, "error_message")); err != nil {
		t.Fatal(err)
	}
	want := `{"event":"step_update","step_update":{"conversation_id":"cid","step_index":1,"state":"DONE","step_type":"error_message"}}` + "\n"
	if b.String() != want {
		t.Errorf("emitStep =\n %q\nwant\n %q", b.String(), want)
	}
}

// TestEmitStepOmitsUnknownFacts pins the omit rule of the six members the
// step gained: a step that knows no text, no duration, no usage and no tool
// carries none of them — the reference's member set is a *possible* set, not
// a mandatory one (`official-docs/cli/headless.md:172-183`).
func TestEmitStepOmitsUnknownFacts(t *testing.T) {
	var b strings.Builder
	if err := emitStep(&b, newStepUpdate("cid", 0, "user_input")); err != nil {
		t.Fatal(err)
	}
	for _, member := range []string{"tool_name", "text_delta", "duration_seconds", "usage", "tool_info", "subagent_info"} {
		if strings.Contains(b.String(), `"`+member+`"`) {
			t.Errorf("step carries %q although the step knows no such fact: %s", member, b.String())
		}
	}
}

// permissionModeFlagSet builds the flag set the init event's
// `permission_mode` is read from.
func permissionModeFlagSet(t *testing.T, args ...string) *flag.FlagSet {
	t.Helper()
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.SetOutput(new(strings.Builder))
	fs.Bool("dangerously-skip-permissions", false, "")
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	return fs
}

// TestPrintPermissionModeDefault pins the init event's `permission_mode`
// fallback: the `toolPermission` setting's default.
//
// 【一手】 agy 1.4.8: with an empty settings.json the init event reports
// "request-review" — the value the 1.1.0 changelog calls the default
// ("Added `request-review` (default) mode as the default execution
// behavior"). The "always-proceed" the cycle-113 capture carries came from a
// capture machine whose settings.json set `toolPermission` explicitly.
func TestPrintPermissionModeDefault(t *testing.T) {
	if got := settings.DefaultString("toolPermission"); got != "request-review" {
		t.Fatalf("toolPermission default = %q, want request-review", got)
	}
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	if got := printPermissionMode(permissionModeFlagSet(t)); got != "request-review" {
		t.Errorf("printPermissionMode = %q, want request-review", got)
	}
}

// TestPrintPermissionModeReadsSettings pins that the file wins over the
// default: the reference reports the setting the user actually set.
func TestPrintPermissionModeReadsSettings(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"toolPermission":"always-proceed"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTIGRAVITY_HOME", dir)
	if got := printPermissionMode(permissionModeFlagSet(t)); got != "always-proceed" {
		t.Fatalf("printPermissionMode = %q, want always-proceed", got)
	}
}

// TestPrintPermissionModeSkipPermissions pins that the init event reports
// the mode the run is actually in under `--dangerously-skip-permissions`.
//
// The policy that flag builds skips every prompt (`setup.PolicyFrom`'s
// SkipPermissions), so the mode is "always-proceed" — which is what the
// reference's doc says the event reports under that flag
// (`official-docs/cli/headless.md:159`).
//
// 【存疑】 the reference's own value under the flag is not confirmed
// first-hand; the doc is the only source.
func TestPrintPermissionModeSkipPermissions(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	fs := permissionModeFlagSet(t, "--dangerously-skip-permissions")
	if got := printPermissionMode(fs); got != "always-proceed" {
		t.Fatalf("printPermissionMode = %q, want always-proceed", got)
	}
}

// TestPrintPermissionModeSkipPermissionsBeatsSettings pins that the flag
// wins over the settings file: the settings' `toolPermission` is what the
// *policy* falls back to, and the flag replaces the policy's answer.
func TestPrintPermissionModeSkipPermissionsBeatsSettings(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"toolPermission":"request-review"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTIGRAVITY_HOME", dir)
	fs := permissionModeFlagSet(t, "--dangerously-skip-permissions")
	if got := printPermissionMode(fs); got != "always-proceed" {
		t.Fatalf("printPermissionMode = %q, want always-proceed", got)
	}
}

// initFlagSet builds the flag set the init payload's override members are
// read from.
func initFlagSet(t *testing.T, args ...string) *flag.FlagSet {
	t.Helper()
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.SetOutput(new(strings.Builder))
	fs.String("model", "", "")
	fs.String("agent", "", "")
	fs.String("json-schema", "", "")
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	return fs
}

// writeAgentDefinition writes a markdown agent definition into the user
// customisations tree the agents catalogue reads: the sibling `config`
// directory of the app data home.
func writeAgentDefinition(t *testing.T, home, name string) {
	t.Helper()
	dir := filepath.Join(filepath.Dir(home), "config", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: a test agent\n---\n\nthe body\n"
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestInitPayloadOverrides pins the init payload's three override members:
// they travel only when the run overrode the corresponding thing — the
// doc's "`model` and `agent` appear only when set with `--model` or
// `--agent`", "`json_schema` … when set with `--json-schema`"
// (`official-docs/cli/headless.md:159-168`).
func TestInitPayloadOverrides(t *testing.T) {
	home := filepath.Join(t.TempDir(), "appdata")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	writeAgentDefinition(t, home, "reviewer")
	t.Setenv("ANTIGRAVITY_HOME", home)

	// A run that overrode nothing carries none of the three.
	fs := initFlagSet(t)
	cfg := &printRunConfig{modelName: "the default model"}
	if got := printInitModel(fs, cfg); got != "" {
		t.Errorf("printInitModel = %q, want the empty default", got)
	}
	if got := printInitAgent(fs); got != "" {
		t.Errorf("printInitAgent = %q, want the empty default", got)
	}
	if got := printInitJSONSchema(fs); got != nil {
		t.Errorf("printInitJSONSchema = %s, want nil", got)
	}

	// A run that overrode all three carries all three.
	fs = initFlagSet(t, "--model=gemini-3-pro", "--agent=reviewer", `--json-schema={"type":"object"}`)
	cfg = &printRunConfig{modelName: flagStringValue(fs, "model")}
	if got := printInitModel(fs, cfg); got != "gemini-3-pro" {
		t.Errorf("printInitModel = %q, want gemini-3-pro", got)
	}
	if got := printInitAgent(fs); got != "reviewer" {
		t.Errorf("printInitAgent = %q, want reviewer", got)
	}
	got := printInitJSONSchema(fs)
	if got == nil {
		t.Fatal("printInitJSONSchema = nil, want the schema")
	}
	if string(got) != `{"type":"object"}` {
		t.Errorf("printInitJSONSchema = %s, want the schema itself", got)
	}
}

// TestInitPayloadDefaultAgentIsNoOverride pins that `--agent=default` is
// not an override: it names the built-in agent, so the member stays absent.
// The rule is the agents catalogue's own, and the payload asks it.
func TestInitPayloadDefaultAgentIsNoOverride(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	fs := initFlagSet(t, "--agent=default")
	if got := printInitAgent(fs); got != "" {
		t.Errorf("printInitAgent = %q, want the empty default", got)
	}
}

// TestAgentResponseStepCarriesFacts pins the facts the `agent_response`
// step reports: the text the turn produced, how long it took and the tokens
// it spent — the three the doc's example carries
// (`official-docs/cli/headless.md:144`).
func TestAgentResponseStepCarriesFacts(t *testing.T) {
	var buf bytes.Buffer
	cfg := &printRunConfig{format: printFormatStreamJSON, conversationID: "c"}
	st := &printTurnState{turnStarted: true, turnBegan: true, turnStartedAt: time.Now().Add(-2 * time.Second)}
	st.responseBuf.WriteString("hello\n")
	st.turnUsage = jsonUsage{InputTokens: 3, OutputTokens: 4, TotalTokens: 7}
	env := &jsonEnvelope{ConversationID: "c", Status: printStatusSuccess, Response: "hello\n"}
	if err := emitFinalStreamJSON(&buf, nil, cfg, st, env, nil, true, false); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2:\n%s", len(lines), buf.String())
	}
	var event struct {
		Event      string `json:"event"`
		StepUpdate struct {
			StepType        string     `json:"step_type"`
			TextDelta       string     `json:"text_delta"`
			DurationSeconds float64    `json:"duration_seconds"`
			Usage           *jsonUsage `json:"usage"`
		} `json:"step_update"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &event); err != nil {
		t.Fatalf("decode: %v (%s)", err, lines[0])
	}
	if event.Event != streamEventStepUpdate {
		t.Fatalf("event = %q, want %q", event.Event, streamEventStepUpdate)
	}
	if event.StepUpdate.StepType != "agent_response" {
		t.Errorf("step_type = %q, want agent_response", event.StepUpdate.StepType)
	}
	if event.StepUpdate.TextDelta != "hello\n" {
		t.Errorf("text_delta = %q, want %q", event.StepUpdate.TextDelta, "hello\n")
	}
	if event.StepUpdate.DurationSeconds < 1.9 {
		t.Errorf("duration_seconds = %v, want the turn's elapsed seconds", event.StepUpdate.DurationSeconds)
	}
	if event.StepUpdate.Usage == nil || event.StepUpdate.Usage.TotalTokens != 7 {
		t.Errorf("usage = %+v, want the turn's usage", event.StepUpdate.Usage)
	}
}

// TestPrintTimeout covers the --print-timeout flag. The changelog changed the
// default headless timeout from 5 minutes to unlimited, and the reference
// treats every non-positive value as "no deadline" — `--print-timeout=0` and
// `--print-timeout=-5s` both let the turn run to completion (verified live).
func TestPrintTimeout(t *testing.T) {
	cases := []struct {
		value string
		want  time.Duration
	}{
		{"", 0},    // flag absent
		{"0s", 0},  // explicit default
		{"-5s", 0}, // negative: still unlimited
		{"1ms", time.Millisecond},
		{"2s", 2 * time.Second},
		{"90s", 90 * time.Second},
	}
	for _, c := range cases {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.Duration("print-timeout", 0, "")
		if c.value != "" {
			if err := fs.Parse([]string{"--print-timeout=" + c.value}); err != nil {
				t.Fatalf("%q: %v", c.value, err)
			}
		}
		if got := printTimeout(fs); got != c.want {
			t.Errorf("printTimeout(%q) = %v, want %v", c.value, got, c.want)
		}
	}
}

// TestPrintTimeoutWarning pins the stderr line, captured live from
// `agy -p='say hi' --print-timeout=1ms`.
//
// The reference hardcodes the `[agy]` tag — it prints the same text when the
// binary is renamed. The clone derives it from argv0, which is what it does for
// the program name everywhere else, so the expectation is set up by invoking
// the renderer under the name `agy`.
func TestPrintTimeoutWarning(t *testing.T) {
	saved := os.Args[0]
	defer func() { os.Args[0] = saved }()
	os.Args[0] = "agy"

	got := printTimeoutWarning(time.Millisecond)
	want := "[agy] print timeout after 1ms with turn in progress; returning partial output"
	if got != want {
		t.Errorf("warning = %q\nwant    %q", got, want)
	}
	if !strings.Contains(printTimeoutWarning(90*time.Second), "after 1m30s") {
		t.Errorf("duration not formatted with Duration.String(): %q", printTimeoutWarning(90*time.Second))
	}
}

// TestPrintContext checks the run context is bounded only when a timeout was
// asked for.
func TestPrintContext(t *testing.T) {
	unbounded, cancel := printContext(0)
	defer cancel()
	if _, ok := unbounded.Deadline(); ok {
		t.Error("printContext(0) has a deadline; the reference's default is unlimited")
	}

	bounded, cancel2 := printContext(time.Minute)
	defer cancel2()
	d, ok := bounded.Deadline()
	if !ok {
		t.Fatal("printContext(time.Minute) has no deadline")
	}
	if remaining := time.Until(d); remaining <= 0 || remaining > time.Minute {
		t.Errorf("deadline %v out of range", remaining)
	}
}

// ---- cycle headless-jsonl-mode-trailing-newline -------------------------
//
// AGY 1.2.7 close-gap: `--output-format stream-json --verbose` headless
// mode must emit an `assistant_message` event whose terminator is a
// single `\n`, matching the AGY 1.2.7 wire shape and keeping the next
// shell prompt from gluing to the last byte of stdout. The tests below
// pin the trailing-newline contract on the helpers (writeJSONLine,
// emitAssistantMessage) and on the higher-level format selectors
// (text/json/stream-json), so a future refactor that drops the
// appended newline through any path is caught by `go test`.

// TestAssistantMessageFieldOrder pins the wire shape and member order
// of the `assistant_message` event payload emitted by
// `--output-format stream-json --verbose`:
//
//	{"event":"assistant_message","assistant_message":{"message":"…","model":"…"}}
//
// A struct whose fields were reordered, or a map, would marshal
// differently. The trailing-newline contract — which the AGY 1.2.7
// changelog bug fix calls out — is the property emitAssistantMessage
// enforces by routing through writeJSONLine; TestEmitAssistantMessage
// below covers it explicitly.
func TestAssistantMessageFieldOrder(t *testing.T) {
	ev := jsonStreamEvent{
		Event: streamEventAssistantMessage,
		AssistantMessage: &jsonAssistantMessage{
			Message: "hello world",
			Model:   "gpt-test",
		},
	}
	want := `{"event":"assistant_message","assistant_message":{"message":"hello world","model":"gpt-test"}}`
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != want {
		t.Errorf("assistant_message =\n %s\nwant\n %s", b, want)
	}

	// With Model empty, the omitempty tag drops the field so the
	// emitted shape does not carry a stray "model":"".
	ev2 := jsonStreamEvent{
		Event: streamEventAssistantMessage,
		AssistantMessage: &jsonAssistantMessage{
			Message: "no model",
		},
	}
	b2, _ := json.Marshal(ev2)
	want2 := `{"event":"assistant_message","assistant_message":{"message":"no model"}}`
	if string(b2) != want2 {
		t.Errorf("assistant_message (no model) =\n %s\nwant\n %s", b2, want2)
	}
}

// TestEmitAssistantMessage pins that emitAssistantMessage appends a
// single `\n` after the marshalled JSON. Two newlines would re-split
// the event into two lines (and break NDJSON consumers), zero
// newlines is the original AGY 1.2.7 bug — the cycle
// headless-jsonl-mode-trailing-newline exists specifically to ensure
// the emitted stream always lands on exactly one trailing `\n`.
func TestEmitAssistantMessage(t *testing.T) {
	var b strings.Builder
	if err := emitAssistantMessage(&b, "hello\nworld", ""); err != nil {
		t.Fatalf("emitAssistantMessage: %v", err)
	}
	got := b.String()
	want := `{"event":"assistant_message","assistant_message":{"message":"hello\nworld"}}` + "\n"
	if got != want {
		t.Errorf("emitAssistantMessage =\n %q\nwant\n %q", got, want)
	}
	// Exactly one trailing newline (no double-newline).
	if strings.Count(got, "\n") != 1 {
		t.Errorf("got %d '\\n' bytes, want exactly 1: %q", strings.Count(got, "\n"), got)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("output does not end with '\\n': %q", got)
	}
}

// TestEmitAssistantMessageWithModel exercises the variant that
// carries the model id; the AGY wire shape omits "model" only when
// the empty default applies. The trailing newline still applies.
func TestEmitAssistantMessageWithModel(t *testing.T) {
	var b strings.Builder
	if err := emitAssistantMessage(&b, "hi", "claude-test"); err != nil {
		t.Fatalf("emitAssistantMessage: %v", err)
	}
	got := b.String()
	want := `{"event":"assistant_message","assistant_message":{"message":"hi","model":"claude-test"}}` + "\n"
	if got != want {
		t.Errorf("emitAssistantMessage(with model) =\n %q\nwant\n %q", got, want)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("output does not end with '\\n': %q", got)
	}
}

// TestEmitAssistantMessageEmpty guards against polluttering the
// stream-json output with zero-byte `assistant_message` frames. A
// turn that produced no visible assistant text (the chunk handler's
// buffered text was empty) must not emit an event, so consumers
// downstream never see an event whose `message` is "" followed by
// the closing newline. The runner-level guard (`env.Response != ""`)
// wraps this contract; the helper itself is permissive so callers
// can decide their policy.
func TestEmitAssistantMessageEmpty(t *testing.T) {
	var b strings.Builder
	if err := emitAssistantMessage(&b, "", ""); err != nil {
		t.Fatalf("emitAssistantMessage: %v", err)
	}
	// Even with an empty message, the helper still emits a single
	// event line — the newline contract still applies. The runner's
	// gate, not the helper, decides to skip emission entirely.
	got := b.String()
	want := `{"event":"assistant_message","assistant_message":{"message":""}}` + "\n"
	if got != want {
		t.Errorf("emitAssistantMessage(empty) =\n %q\nwant\n %q", got, want)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("output does not end with '\\n': %q", got)
	}
}

// TestStreamJSONTrailingNewline pins the headline property the AGY
// 1.2.7 changelog bug fix documents: every stream-json event line
// ends with a single `\n`, including the assistant_message event
// emitted under `--verbose`, so the shell prompt that runs after the
// CLI does not glue to the last byte of stdout.
func TestStreamJSONTrailingNewline(t *testing.T) {
	// Drive a representative sequence: an init event + a model
	// turn's assistant_message + the closing result envelope. All
	// three must land on a trailing newline.
	var b strings.Builder
	if err := writeJSONLine(&b, jsonStreamEvent{
		Event: streamEventInit,
		Init:  &jsonInitPayload{CWD: "/tmp", Tools: []string{"a"}, PermissionMode: "always-proceed"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := emitAssistantMessage(&b, "hello", ""); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONLine(&b, jsonStreamEvent{
		Event:  streamEventResult,
		Result: &jsonEnvelope{Status: printStatusSuccess, Response: "hello"},
	}); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d non-empty lines, want 3:\n%s", len(lines), out)
	}
	for i, line := range lines {
		if line == "" {
			t.Errorf("line %d is empty:\n%s", i, out)
		}
		if strings.Contains(line, "\n") {
			t.Errorf("line %d contains embedded newline:\n%s", i, line)
		}
	}
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("output does not end with '\\n':\n%s", out)
	}
	// Exactly three newline bytes: each event's terminator.
	if n := strings.Count(out, "\n"); n != 3 {
		t.Errorf("got %d '\\n' bytes, want 3:\n%s", n, out)
	}
}

// TestStreamJSONVerboseNoDoubleNewline is the regression test the AGY
// 1.2.7 changelog maps directly to: under `--verbose`, the assistant
// message line followed by the closing result line must each carry
// exactly one trailing `\n`. Two consecutive newlines would split
// the assistant_message frame into two empty lines (NDJSON
// consumers see it as a record whose `event` is ""), and zero
// trailing newlines would glue the shell prompt onto the closing
// `}` of the result envelope.
func TestStreamJSONVerboseNoDoubleNewline(t *testing.T) {
	var b strings.Builder
	if err := emitAssistantMessage(&b, "hi there", "gpt-x"); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONLine(&b, jsonStreamEvent{
		Event:  streamEventResult,
		Result: &jsonEnvelope{Status: printStatusSuccess, Response: "hi there"},
	}); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	if strings.Contains(got, "\n\n") {
		t.Errorf("double newline detected in stream-json output: %q", got)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("stream-json output does not end with '\\n': %q", got)
	}
	// And the assistant_message line precedes the result line, each
	// terminated by a single `\n`.
	want := `{"event":"assistant_message","assistant_message":{"message":"hi there","model":"gpt-x"}}` + "\n" +
		`{"event":"result","result":{"conversation_id":"","status":"SUCCESS","response":"hi there","duration_seconds":0,"num_turns":0,"usage":{"input_tokens":0,"output_tokens":0,"thinking_tokens":0,"cache_read_tokens":0,"total_tokens":0}}}` + "\n"
	if got != want {
		t.Errorf("verbose stream-json =\n %q\nwant\n %q", got, want)
	}
}

// TestJSONEnvelopeValidAfterAssistantMessage exercises the json path
// the existing clones have always emitted: the single-line envelope
// the AGY 1.2.7 changelog fix does *not* touch. Adding the verbose
// path must not change the json envelope's wire shape, byte order,
// or omitempty semantics, so the existing TestJSONEnvelopeFieldOrder
// and TestJSONEnvelopeOmitsEmptyMembers still apply verbatim. This
// test re-runs their checks in the wake of the new event type's
// arrival, so a struct rename or field reorder does not silently
// change the unverbose path.
func TestJSONEnvelopeValidAfterAssistantMessage(t *testing.T) {
	env := jsonEnvelope{
		ConversationID:  "cid",
		Status:          printStatusSuccess,
		Response:        "ok",
		DurationSeconds: 1,
		NumTurns:        1,
		Usage:           jsonUsage{InputTokens: 1, OutputTokens: 2, TotalTokens: 3},
	}
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"conversation_id":"cid","status":"SUCCESS","response":"ok","duration_seconds":1,"num_turns":1,"usage":{"input_tokens":1,"output_tokens":2,"thinking_tokens":0,"cache_read_tokens":0,"total_tokens":3}}`
	if string(b) != want {
		t.Errorf("json envelope =\n %s\nwant\n %s", b, want)
	}
	// Round-trip through writeJSONLine and confirm the trailing
	// newline is still there — even the pre-existing helpers must
	// keep their terminator contract when the new event type has
	// joined the family.
	var buf strings.Builder
	if err := writeJSONLine(&buf, env); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(buf.String(), "\n") {
		t.Errorf("json envelope line has no trailing '\\n': %q", buf.String())
	}
}

func TestJSONEnvelopeStructuredOutputFields(t *testing.T) {
	env := jsonEnvelope{
		ConversationID:   "cid",
		Status:           printStatusSuccess,
		Response:         `{"major":2,"minor":14,"patch":3}` + "\n",
		DurationSeconds:  4,
		NumTurns:         1,
		StructuredOutput: map[string]any{"major": float64(2), "minor": float64(14), "patch": float64(3)},
		JSONSchema:       json.RawMessage(`{"type":"object"}`),
		Usage:            jsonUsage{InputTokens: 10, OutputTokens: 20, TotalTokens: 30},
	}
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"conversation_id":"cid","status":"SUCCESS","response":"{\"major\":2,\"minor\":14,\"patch\":3}\n","duration_seconds":4,"num_turns":1,"structured_output":{"major":2,"minor":14,"patch":3},"json_schema":{"type":"object"},"usage":{"input_tokens":10,"output_tokens":20,"thinking_tokens":0,"cache_read_tokens":0,"total_tokens":30}}`
	if string(b) != want {
		t.Errorf("got =\n %s\nwant\n %s", b, want)
	}
}

// TestTextModeUnchangedByVerbose is the negative control: a body
// line that `--output-format text` writes to stdout is *plain text*,
// never JSON, and must not be touched by the verbose path. AGY 1.2.7
// only described the stream-json case, so any text-format emission
// must remain identical to what it was before the cycle. The test
// pin the existing behaviour: printFormatText does not exist as a
// helper to call directly here, so we instead pin the JSON path does
// not also write a text-mode body — i.e. the two paths are disjoint
// at the wire level.
func TestTextModeUnchangedByVerbose(t *testing.T) {
	// text format identifier is "text"; verify printOutputFormat
	// treats the empty / explicit values identically regardless of
	// whether --verbose would have been admitted. The two paths are
	// kept disjoint at this level: text mode never produces JSON,
	// verbose has no text-format side-channel.
	for _, want := range []string{"text", "json", "stream-json"} {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.String("output-format", "", "")
		if err := fs.Parse([]string{"--output-format=" + want}); err != nil {
			t.Fatalf("%q: %v", want, err)
		}
		if got := printOutputFormat(fs); got != want {
			t.Errorf("printOutputFormat(%q) = %q, want %q", want, got, want)
		}
	}
	// The verbose flag does not alter the resolved format — it only
	// changes what the stream-json handler does once the turn ends,
	// so printOutputFormat must be insensitive to its value. This
	// pins the negative control: text mode stays text, JSON stays
	// JSON, stream-json stays stream-json, and verbose is irrelevant
	// for the format resolver itself.
	for _, format := range []string{"text", "json", "stream-json"} {
		for _, verbose := range []string{"true", "false"} {
			fs := flag.NewFlagSet("t", flag.ContinueOnError)
			fs.String("output-format", "", "")
			fs.Bool("verbose", false, "")
			if err := fs.Parse([]string{
				"--output-format=" + format,
				"--verbose=" + verbose,
			}); err != nil {
				t.Fatalf("format=%q verbose=%q: %v", format, verbose, err)
			}
			if got := printOutputFormat(fs); got != format {
				t.Errorf("format=%q verbose=%q -> %q, want %q", format, verbose, got, format)
			}
		}
	}
}

// TestPrintVerboseFlagResolution pins the boolean reading helper
// introduced alongside the headless-jsonl-mode-trailing-newline flag
// spec. Unset is false (the safe default), explicit `--verbose=true`
// is true, `--verbose=false` is false (does not flip the default),
// and an unknown value is also false (the helper duck-types — the
// jetski parser has already validated the bool).
func TestPrintVerboseFlagResolution(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{nil, false},                         // flag absent
		{[]string{"--verbose=true"}, true},   // explicit true
		{[]string{"--verbose=false"}, false}, // explicit false
		{[]string{"--verbose"}, true},        // bool shorthand
	}
	for _, c := range cases {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.Bool("verbose", false, "")
		if len(c.args) > 0 {
			if err := fs.Parse(c.args); err != nil {
				t.Fatalf("%v: %v", c.args, err)
			}
		}
		if got := printVerbose(fs); got != c.want {
			t.Errorf("args=%v: printVerbose = %v, want %v", c.args, got, c.want)
		}
	}
}
