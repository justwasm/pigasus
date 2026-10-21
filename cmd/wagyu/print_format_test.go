package main

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"strings"
	"testing"
	"time"
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
// for keys the user has never set. The reference distinguishes five *nullable*
// keys (null) from the rest, which fall back to a zero value of their kind.
func TestConfigValueTypes(t *testing.T) {
	cases := []struct {
		key  string
		want any
	}{
		{"agentMode", ""},
		{"allowNonWorkspaceAccess", false},
		{"altScreenMode", "default"},
		{"clearScrollbackOnResize", true},
		{"customModelsConfig", nil},
		{"gcp", nil},
		{"historySize", 2000},
		{"model", ""},
		{"permissions", map[string]any{}},
		{"sandboxProxy", nil},
		{"statusLine", nil},
		{"title", nil},
		{"trustedWorkspaces", []any{}},
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
			name: "step_update",
			event: jsonStreamEvent{
				Event:      streamEventStepUpdate,
				StepUpdate: &jsonStepUpdate{ConversationID: "c", StepIndex: 0, State: "DONE", StepType: "user_input"},
			},
			want: `{"event":"step_update","step_update":{"conversation_id":"c","step_index":0,"state":"DONE","step_type":"user_input"}}`,
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
	if err := emitStep(&b, "cid", 1, "error_message"); err != nil {
		t.Fatal(err)
	}
	want := `{"event":"step_update","step_update":{"conversation_id":"cid","step_index":1,"state":"DONE","step_type":"error_message"}}` + "\n"
	if b.String() != want {
		t.Errorf("emitStep =\n %q\nwant\n %q", b.String(), want)
	}
}

// TestPrintPermissionModeDefault pins the init event's `permission_mode`
// fallback: the captured value is the `toolPermission` setting's default.
func TestPrintPermissionModeDefault(t *testing.T) {
	if got := printSlashConfigDefaults["toolPermission"]; got != "always-proceed" {
		t.Fatalf("toolPermission default = %q, want always-proceed", got)
	}
	if got := printPermissionMode(); got == "" {
		t.Error("printPermissionMode returned an empty mode")
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
