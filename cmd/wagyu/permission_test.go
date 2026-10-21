// Tests for the cycle 98 headless tool-permission gate.
//
// The gate wraps agent.ToolRunner in print mode. Its default is the clone's
// documented divergence (allow), so the tests drive it with Strict set, which
// is the OPENAGY_PERMISSION_POLICY=strict behaviour and reproduces the
// reference: an undecided call cannot be prompted for, so it is soft-denied
// with the recovered text and the run reports the recovered notice.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/permissions"
	"github.com/justwasm/pigasus/internal/tools"
)

// recordingRunner stands in for the real registry and records what ran.
type recordingRunner struct {
	ran []string
}

func (r *recordingRunner) Run(_ context.Context, call backend.ToolCall) (string, error) {
	r.ran = append(r.ran, call.Name)
	return "ran " + call.Name, nil
}

func TestPermissionRunner_DefaultAllows(t *testing.T) {
	inner := &recordingRunner{}
	gate := &permissionRunner{inner: inner, policy: &permissions.Policy{}}
	out, err := gate.Run(context.Background(), backend.ToolCall{
		Name: "run_command", Arguments: json.RawMessage(`{"command":"date"}`),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "ran run_command" {
		t.Errorf("out = %q, want the tool to have run", out)
	}
	if len(gate.Denials) != 0 {
		t.Errorf("denials = %v, want none", gate.Denials)
	}
}

func TestPermissionRunner_StrictSoftDenies(t *testing.T) {
	inner := &recordingRunner{}
	gate := &permissionRunner{inner: inner, policy: &permissions.Policy{Strict: true}}
	out, err := gate.Run(context.Background(), backend.ToolCall{
		Name: "run_command", Arguments: json.RawMessage(`{"command":"date"}`),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(inner.ran) != 0 {
		t.Errorf("the tool ran despite strict mode: %v", inner.ran)
	}
	want := permissions.DeniedToolResult("run_command", "date")
	if out != want {
		t.Errorf("soft-deny text =\n  %q\nwant\n  %q", out, want)
	}
	if len(gate.Denials) != 1 || gate.Denials[0] != "run_command" {
		t.Errorf("denials = %v", gate.Denials)
	}
	if !strings.HasPrefix(permissions.HeadlessNotice(len(gate.Denials)), "the 1 tool(s) required approval") {
		t.Error("notice prefix changed")
	}
}

func TestPermissionRunner_SkipPermissionsAllowsUnderStrict(t *testing.T) {
	inner := &recordingRunner{}
	gate := &permissionRunner{
		inner:  inner,
		policy: &permissions.Policy{Strict: true, SkipPermissions: true},
	}
	if _, err := gate.Run(context.Background(), backend.ToolCall{
		Name: "run_command", Arguments: json.RawMessage(`{"command":"date"}`),
	}); err != nil {
		t.Fatal(err)
	}
	if len(inner.ran) != 1 {
		t.Errorf("--dangerously-skip-permissions did not allow: %v", gate.Denials)
	}
}

func TestPermissionRunner_AllowRuleAllowsUnderStrict(t *testing.T) {
	inner := &recordingRunner{}
	gate := &permissionRunner{inner: inner, policy: &permissions.Policy{
		Strict: true,
		Allow:  []permissions.Rule{{Tool: "run_command", Pattern: "date"}},
	}}
	if _, err := gate.Run(context.Background(), backend.ToolCall{
		Name: "run_command", Arguments: json.RawMessage(`{"command":"date"}`),
	}); err != nil {
		t.Fatal(err)
	}
	if len(inner.ran) != 1 {
		t.Errorf("allow rule did not permit: %v", gate.Denials)
	}
	// a call outside the rule is still denied
	if _, err := gate.Run(context.Background(), backend.ToolCall{
		Name: "run_command", Arguments: json.RawMessage(`{"command":"rm -rf /"}`),
	}); err != nil {
		t.Fatal(err)
	}
	if len(inner.ran) != 1 || len(gate.Denials) != 1 {
		t.Errorf("out-of-rule call: ran=%v denials=%v", inner.ran, gate.Denials)
	}
}

// TestPermissionRunner_ModeAutoAllowsCommands covers the Auto mode claim
// "edits & safe commands auto-approved".
func TestPermissionRunner_ModeAutoAllowsCommands(t *testing.T) {
	inner := &recordingRunner{}
	gate := &permissionRunner{inner: inner, policy: &permissions.Policy{
		Mode: permissions.ModeAuto, Strict: true,
	}}
	if _, err := gate.Run(context.Background(), backend.ToolCall{
		Name: "run_command", Arguments: json.RawMessage(`{"command":"date"}`),
	}); err != nil {
		t.Fatal(err)
	}
	if len(inner.ran) != 1 {
		t.Errorf("auto mode did not allow the command: %v", gate.Denials)
	}
}

// TestToolTarget pins the field the settings rules match against.
func TestToolTarget(t *testing.T) {
	cases := []struct {
		name string
		args string
		want string
	}{
		{"run_command", `{"command":"go test ./..."}`, "go test ./..."},
		{"view_file", `{"path":"src/a.go"}`, "src/a.go"},
		{"write_to_file", `{"path":"a.txt","content":"x"}`, "a.txt"},
		// Cycle 275: the reference's wire keys are capitalised. Before this
		// the gate matched nothing for a real call and waved it through.
		{"run_command", `{"CommandLine":"go test ./...","Cwd":"/ws"}`, "go test ./..."},
		{"view_file", `{"AbsolutePath":"/ws/src/a.go"}`, "/ws/src/a.go"},
		{"write_to_file", `{"TargetFile":"/ws/a.txt","CodeContent":"x"}`, "/ws/a.txt"},
		{"finish", `{"summary":"done"}`, ""},
		{"broken", `not json`, ""},
	}
	for _, c := range cases {
		if got := toolTarget(backend.ToolCall{Name: c.name, Arguments: json.RawMessage(c.args)}); got != c.want {
			t.Errorf("toolTarget(%s, %s) = %q, want %q", c.name, c.args, got, c.want)
		}
	}
}

// newStandardFlagSetForTest builds a parsed standard flag set for `args`.
func newStandardFlagSetForTest(t *testing.T, args []string) *flag.FlagSet {
	t.Helper()
	p := cli.NewParser(cli.StandardFlags(), cli.StandardSubcommands(),
		"antigravity", "recovered", "recovered")
	if code := p.Root().Parse(args); code != 0 {
		t.Fatalf("Parse(%v) = %d", args, code)
	}
	return p.Root().FlagSet()
}

// writeSettingsJSON writes a settings.json into an Antigravity home.
func writeSettingsJSON(t *testing.T, home, body string) error {
	t.Helper()
	return os.WriteFile(filepath.Join(home, "settings.json"), []byte(body), 0o644)
}

// TestPrintModePolicy_FlagsAndEnv wires the flags and OPENAGY_PERMISSION_POLICY
// through to the policy.
func TestPrintModePolicy_FlagsAndEnv(t *testing.T) {
	fs := newStandardFlagSetForTest(t, []string{"--mode", "auto", "--dangerously-skip-permissions"})
	t.Setenv("OPENAGY_PERMISSION_POLICY", "strict")
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	p := printModePolicy(fs)
	if p.Mode != permissions.ModeAuto {
		t.Errorf("Mode = %q, want auto", p.Mode)
	}
	if !p.SkipPermissions {
		t.Error("SkipPermissions = false")
	}
	if !p.Strict {
		t.Error("Strict = false despite OPENAGY_PERMISSION_POLICY=strict")
	}
}

// TestPrintModePolicy_LoadsSettingsRules proves settings.json reaches the
// policy.
func TestPrintModePolicy_LoadsSettingsRules(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	t.Setenv("OPENAGY_PERMISSION_POLICY", "")
	if err := writeSettingsJSON(t, home, `{"permissions":{"allow":["view_file:src/"]}}`); err != nil {
		t.Fatal(err)
	}
	p := printModePolicy(newStandardFlagSetForTest(t, nil))
	if len(p.Allow) != 1 || p.Allow[0] != (permissions.Rule{Tool: "view_file", Pattern: "src/"}) {
		t.Errorf("Allow = %+v", p.Allow)
	}
}

// TestCatalogToolNamesUnchangedInPrintMode keeps the advertised surface stable.
func TestCatalogToolNamesUnchangedInPrintMode(t *testing.T) {
	reg := tools.Catalog(t.TempDir())
	for _, want := range []string{"run_command", "view_file", "grep_search", "finish", "notebook_edit"} {
		if _, ok := reg.Get(want); !ok {
			t.Errorf("catalog lost %q", want)
		}
	}
}

// scriptedBackend replays canned chunk sets, one per Stream call.
type scriptedBackend struct {
	turns [][]backend.StreamChunk
	reqs  []backend.StreamRequest
	i     int
}

func (s *scriptedBackend) Stream(_ context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
	s.reqs = append(s.reqs, req)
	var chunks []backend.StreamChunk
	if s.i < len(s.turns) {
		chunks = s.turns[s.i]
	} else {
		chunks = []backend.StreamChunk{{Done: true}}
	}
	s.i++
	out := make(chan backend.StreamChunk, len(chunks)+1)
	for _, c := range chunks {
		out <- c
	}
	close(out)
	return out, nil
}

// TestGateThroughAgentLoop is the composition check: a soft-denied call must
// come back to the model as an ordinary tool result so the turn completes,
// and the denied tool must never reach the registry.
func TestGateThroughAgentLoop(t *testing.T) {
	inner := &recordingRunner{}
	gate := &permissionRunner{inner: inner, policy: &permissions.Policy{Strict: true}}

	be := &scriptedBackend{turns: [][]backend.StreamChunk{
		{{ToolCalls: []backend.ToolCall{{
			ID: "c1", Name: "run_command",
			Arguments: json.RawMessage(`{"command":"date"}`),
		}}}, {Done: true}},
		{{Text: "I could not run that."}, {Done: true}},
	}}
	var final strings.Builder
	loop, err := agent.New(agent.Options{
		Backend: be,
		Handler: func(chunk backend.StreamChunk) error {
			if chunk.Text != "" {
				final.WriteString(chunk.Text)
			}
			return nil
		},
		Tools:      []backend.ToolDecl{{Name: "run_command"}},
		ToolRunner: gate,
	})
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}
	if err := loop.Send(context.Background(), agent.TurnInput{
		Messages: []backend.Message{{Role: "user", Content: "what time is it?"}},
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(inner.ran) != 0 {
		t.Errorf("the denied tool ran: %v", inner.ran)
	}
	if len(gate.Denials) != 1 {
		t.Errorf("denials = %v, want one", gate.Denials)
	}
	if final.String() != "I could not run that." {
		t.Errorf("final text = %q", final.String())
	}
	// The second request must carry the denial text back as the tool result.
	if len(be.reqs) != 2 {
		t.Fatalf("backend saw %d requests, want 2", len(be.reqs))
	}
	var found bool
	for _, m := range be.reqs[1].Messages {
		if m.Role == "tool" && strings.Contains(m.Content, "was denied.") {
			found = true
		}
	}
	if !found {
		t.Errorf("denial text was not fed back to the model: %+v", be.reqs[1].Messages)
	}
}
