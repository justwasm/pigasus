// Package permissions — grant_test.go: the reference's own grant
// grammar, `action(target)`.
//
// The clone's vocabulary used to be "tool:pattern", which the reference
// does not use anywhere: the reference's own parser splits the action from
// the target and validates the action against a closed set. These tests pin
// the grammar the reference speaks, the sentences it refuses a rule with,
// and the fact that the legacy form is still *read*.
//
// 【一手】 out/localharness/dec/100dc2c20_ParseGrantString.c (the action
// set and the two error sentences) and agy 1.4.11's own string table,
// which carries the format regex and the `unsandboxed` sentence (the
// surviving pruned index of that table is
// out/local-macos-1.4.11/strings-index.txt — the full dump is retired).
package permissions

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestParseGrantStringReadsTheReferenceGrammar pins the grammar the
// reference speaks: an action, a target, and nothing else.
func TestParseGrantStringReadsTheReferenceGrammar(t *testing.T) {
	cases := []struct {
		in         string
		wantAction string
		wantTarget string
	}{
		{"command(git status)", ActionCommand, "git status"},
		{"read_url(github.com)", ActionReadURL, "github.com"},
		{"read_file(/etc/**)", ActionReadFile, "/etc/**"},
		{"write_file(*.go)", ActionWriteFile, "*.go"},
		{"mcp(server:tool)", ActionMCP, "server:tool"},
		{"execute_url(https://example.com)", ActionExecuteURL, "https://example.com"},
		// A space before the parenthesis is allowed — the reference's own
		// regex allows a run of spaces.
		{"command (git status)", ActionCommand, "git status"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			action, target, err := ParseGrantString(tc.in)
			if err != nil {
				t.Fatalf("ParseGrantString(%q) = %v, want no error", tc.in, err)
			}
			if action != tc.wantAction {
				t.Errorf("action = %q, want %q", action, tc.wantAction)
			}
			if target != tc.wantTarget {
				t.Errorf("target = %q, want %q", target, tc.wantTarget)
			}
		})
	}
}

// TestParseGrantStringAcceptsEveryLiveAction pins the accepted action set:
// the six the reference's own format regex names.
func TestParseGrantStringAcceptsEveryLiveAction(t *testing.T) {
	for _, action := range GrantActions {
		s := action + "(x)"
		got, _, err := ParseGrantString(s)
		if err != nil {
			t.Errorf("ParseGrantString(%q) = %v, want no error", s, err)
			continue
		}
		if got != action {
			t.Errorf("ParseGrantString(%q) action = %q, want %q", s, got, action)
		}
	}
}

// TestParseGrantStringRefusesTheRetiredAction pins the reference's own
// refusal of `unsandboxed` — the sentence, byte for byte.
func TestParseGrantStringRefusesTheRetiredAction(t *testing.T) {
	_, _, err := ParseGrantString("unsandboxed(git)")
	if err == nil {
		t.Fatal("unsandboxed(git) was accepted")
	}
	if err.Error() != UnsandboxedRejectedMessage {
		t.Errorf("err = %q, want %q", err.Error(), UnsandboxedRejectedMessage)
	}
	if !errors.Is(err, ErrGrantUnsandboxed) {
		t.Errorf("err = %v, want ErrGrantUnsandboxed", err)
	}
}

// TestParseGrantStringRefusesAnUnknownAction pins the reference's second
// error sentence: an action outside the set is refused by name, and not by
// the "invalid grant string" one.
func TestParseGrantStringRefusesAnUnknownAction(t *testing.T) {
	_, _, err := ParseGrantString("frobnicate(x)")
	if err == nil {
		t.Fatal("frobnicate(x) was accepted")
	}
	want := `unknown action "frobnicate" in grant string: "frobnicate(x)"`
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
}

// TestParseGrantStringRefusesANonGrant pins the reference's third sentence:
// a string that is not a grant at all.
func TestParseGrantStringRefusesANonGrant(t *testing.T) {
	_, _, err := ParseGrantString("missing-colon")
	if err == nil {
		t.Fatal("missing-colon was accepted")
	}
	want := `invalid grant string: "missing-colon"`
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
}

// TestParseGrantKeyReadsTheLegacyFormToo pins that the clone's own
// "tool:pattern" form is still read: a permissions.json written before the
// grammar was corrected must not lose its rules.
func TestParseGrantKeyReadsTheLegacyFormToo(t *testing.T) {
	g, err := ParseGrantKey("run_command:go test*")
	if err != nil {
		t.Fatalf("ParseGrantKey: %v", err)
	}
	if g.Action != "" {
		t.Errorf("Action = %q, want empty (a legacy grant names no action)", g.Action)
	}
	if g.ToolName != "run_command" {
		t.Errorf("ToolName = %q, want run_command", g.ToolName)
	}
	if g.Pattern != "go test*" {
		t.Errorf("Pattern = %q, want go test*", g.Pattern)
	}
}

// TestParseGrantKeyNeverFallsBackToTheLegacyForm pins the one rule that
// matters at the boundary: a grant-shaped string the reference refuses is
// not reinterpreted as a tool name.
func TestParseGrantKeyNeverFallsBackToTheLegacyForm(t *testing.T) {
	for _, in := range []string{"unsandboxed(git)", "frobnicate(x)"} {
		if _, err := ParseGrantKey(in); err == nil {
			t.Errorf("ParseGrantKey(%q) was accepted", in)
		}
	}
}

// TestGrantsFromReferenceKeysReadsTheReferenceGrammar pins the vocabulary
// boundary: the reference's own grant strings parse into the clone's grants.
func TestGrantsFromReferenceKeysReadsTheReferenceGrammar(t *testing.T) {
	grants, err := GrantsFromReferenceKeys([]string{"command(git status)", "read_url(github.com)"})
	if err != nil {
		t.Fatalf("GrantsFromReferenceKeys: %v", err)
	}
	if len(grants) != 2 {
		t.Fatalf("len(grants) = %d, want 2", len(grants))
	}
	if grants[0].Action != ActionCommand {
		t.Errorf("grants[0].Action = %q, want %q", grants[0].Action, ActionCommand)
	}
	if grants[0].Pattern != "git status" {
		t.Errorf("grants[0].Pattern = %q, want git status", grants[0].Pattern)
	}
	if grants[1].Action != ActionReadURL {
		t.Errorf("grants[1].Action = %q, want %q", grants[1].Action, ActionReadURL)
	}
}

// TestReferenceGrantKeysRoundTrip pins the round trip: what the reference
// sends comes back as the same strings.
func TestReferenceGrantKeysRoundTrip(t *testing.T) {
	in := []string{"command(git status)", "read_url(github.com)", "write_file(*.go)"}
	grants, err := GrantsFromReferenceKeys(in)
	if err != nil {
		t.Fatalf("GrantsFromReferenceKeys: %v", err)
	}
	out := ReferenceGrantKeys(grants)
	if len(out) != len(in) {
		t.Fatalf("len(out) = %d, want %d", len(out), len(in))
	}
	for i := range in {
		if out[i] != in[i] {
			t.Errorf("out[%d] = %q, want %q", i, out[i], in[i])
		}
	}
}

// TestGrantKeyRendersTheReferenceForm pins the dedup key: an action grant
// is keyed the reference's way, a legacy grant keeps the clone's.
func TestGrantKeyRendersTheReferenceForm(t *testing.T) {
	g := Grant{Action: ActionCommand, Pattern: "git status"}
	if got := g.GrantKey(); got != "command(git status)" {
		t.Errorf("GrantKey = %q, want command(git status)", got)
	}
	legacy := Grant{ToolName: "run_command", Pattern: "go test*"}
	if got := legacy.GrantKey(); got != "run_command:go test*" {
		t.Errorf("GrantKey = %q, want run_command:go test*", got)
	}
}

// TestMemStoreAcceptsActionGrants pins the store layer: an action grant is
// stored, carries its action, and is keyed the reference's way.
func TestMemStoreAcceptsActionGrants(t *testing.T) {
	ps := NewMemStore(nil)
	state, err := ps.AddGrants(context.Background(), []Grant{
		{Action: ActionCommand, Pattern: "git status"},
	})
	if err != nil {
		t.Fatalf("AddGrants: %v", err)
	}
	if len(state.Grants) != 1 {
		t.Fatalf("len(Grants) = %d, want 1", len(state.Grants))
	}
	if state.Grants[0].Action != ActionCommand {
		t.Errorf("Action = %q, want %q", state.Grants[0].Action, ActionCommand)
	}
	if state.Grants[0].Pattern != "git status" {
		t.Errorf("Pattern = %q, want git status", state.Grants[0].Pattern)
	}
	if got := state.Grants[0].GrantKey(); got != "command(git status)" {
		t.Errorf("GrantKey = %q, want command(git status)", got)
	}
}

// TestMemStoreRefusesAnUnknownAction pins the store's defence-in-depth: an
// action outside the reference's set is refused by name.
func TestMemStoreRefusesAnUnknownAction(t *testing.T) {
	ps := NewMemStore(nil)
	_, err := ps.AddGrants(context.Background(), []Grant{
		{Action: "frobnicate", Pattern: "x"},
	})
	if err == nil {
		t.Fatal("an unknown action was accepted")
	}
	if !strings.Contains(err.Error(), `unknown action "frobnicate"`) {
		t.Errorf("err = %q, want the reference's unknown-action sentence", err.Error())
	}
}

// TestMemStoreRefusesTheRetiredAction pins the store's defence-in-depth for
// the retired action, with the reference's own sentence.
func TestMemStoreRefusesTheRetiredAction(t *testing.T) {
	ps := NewMemStore(nil)
	_, err := ps.AddGrants(context.Background(), []Grant{
		{Action: ActionUnsandboxed, Pattern: "git"},
	})
	if err == nil {
		t.Fatal("the retired action was accepted")
	}
	if err.Error() != UnsandboxedRejectedMessage {
		t.Errorf("err = %q, want %q", err.Error(), UnsandboxedRejectedMessage)
	}
}

// TestMemStoreDeduplicatesActionGrants pins the dedup rule for the
// reference's form: the same rule is never stored twice.
func TestMemStoreDeduplicatesActionGrants(t *testing.T) {
	ps := NewMemStore(nil)
	if _, err := ps.AddGrants(context.Background(), []Grant{
		{Action: ActionCommand, Pattern: "git status"},
	}); err != nil {
		t.Fatalf("first add: %v", err)
	}
	_, err := ps.AddGrants(context.Background(), []Grant{
		{Action: ActionCommand, Pattern: "git status"},
	})
	if !errors.Is(err, ErrGrantExists) {
		t.Fatalf("second add err = %v, want ErrGrantExists", err)
	}
}

// TestMemStoreRemovesActionGrants pins the remove path: the reference's
// spelling removes the grant the reference's form filed.
func TestMemStoreRemovesActionGrants(t *testing.T) {
	ps := NewMemStore(nil)
	if _, err := ps.AddGrants(context.Background(), []Grant{
		{Action: ActionCommand, Pattern: "git status"},
		{Action: ActionCommand, Pattern: "git push"},
	}); err != nil {
		t.Fatalf("AddGrants: %v", err)
	}
	state, err := ps.RemoveGrants(context.Background(), []string{"command(git status)"})
	if err != nil {
		t.Fatalf("RemoveGrants: %v", err)
	}
	if len(state.Grants) != 1 {
		t.Fatalf("len(Grants) = %d, want 1", len(state.Grants))
	}
	if state.Grants[0].Pattern != "git push" {
		t.Errorf("remaining grant = %q, want git push", state.Grants[0].Pattern)
	}
}

// TestMemStoreRemovesLegacyGrants pins that the legacy spelling still
// removes a legacy grant.
func TestMemStoreRemovesLegacyGrants(t *testing.T) {
	ps := NewMemStore(nil)
	if _, err := ps.AddGrants(context.Background(), []Grant{
		{ToolName: "run_command", Pattern: "go test*"},
	}); err != nil {
		t.Fatalf("AddGrants: %v", err)
	}
	state, err := ps.RemoveGrants(context.Background(), []string{"run_command:go test*"})
	if err != nil {
		t.Fatalf("RemoveGrants: %v", err)
	}
	if len(state.Grants) != 0 {
		t.Errorf("len(Grants) = %d, want 0", len(state.Grants))
	}
}

// TestFSStoreRoundTripsTheAction pins the on-disk shape: a grant that names
// an action keeps its action across the file round trip.
func TestFSStoreRoundTripsTheAction(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "permissions.json")
	store := NewFSStore(path, nil)
	ctx := context.Background()
	if _, err := store.AddGrants(ctx, []Grant{
		{Action: ActionCommand, Pattern: "git status"},
	}); err != nil {
		t.Fatalf("AddGrants: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(data), `"action": "command"`) {
		t.Errorf("on-disk JSON missing the action:\n%s", data)
	}
	state, err := NewFSStore(path, nil).Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(state.Grants) != 1 {
		t.Fatalf("len(Grants) = %d, want 1", len(state.Grants))
	}
	if state.Grants[0].Action != ActionCommand {
		t.Errorf("Action = %q, want %q", state.Grants[0].Action, ActionCommand)
	}
}

// ---- the wire ------------------------------------------------

// TestAddGrantsAcceptsTheReferenceGrammar pins the dashboard method: a
// grant string in the reference's own grammar is accepted and comes back
// with its action.
func TestAddGrantsAcceptsTheReferenceGrammar(t *testing.T) {
	seams := Seams{Store: NewMemStore(nil)}
	res, errObj := HandleAddGrants(seams, json.RawMessage(`{"tool_patterns":["command(git status)"]}`))
	if errObj != nil {
		t.Fatalf("HandleAddGrants: %v", errObj)
	}
	grants, _ := res.(map[string]any)["grants"].([]any)
	if len(grants) != 1 {
		t.Fatalf("len(grants) = %d, want 1", len(grants))
	}
	entry, _ := grants[0].(map[string]any)
	if entry["action"] != ActionCommand {
		t.Errorf("action = %v, want %q", entry["action"], ActionCommand)
	}
	if entry["pattern"] != "git status" {
		t.Errorf("pattern = %v, want git status", entry["pattern"])
	}
}

// TestAddGrantsRefusesTheRetiredAction pins the refusal on the wire: the
// reference's own sentence travels in the `data` field.
func TestAddGrantsRefusesTheRetiredAction(t *testing.T) {
	seams := Seams{Store: NewMemStore(nil)}
	_, errObj := HandleAddGrants(seams, json.RawMessage(`{"tool_patterns":["unsandboxed(git)"]}`))
	if errObj == nil {
		t.Fatal("the retired action was accepted")
	}
	if errObj.Code != -32602 {
		t.Errorf("code = %d, want -32602", errObj.Code)
	}
	if data, _ := errObj.Data.(string); !strings.Contains(data, UnsandboxedRejectedMessage) {
		t.Errorf("data = %q, want it to carry the reference's sentence", data)
	}
}

// TestAddGrantsRefusesAWrongShapeWithTheFormatHint pins the hint: a string
// that is not a grant at all is refused with the reference's sentence and
// the reference's format hint.
func TestAddGrantsRefusesAWrongShapeWithTheFormatHint(t *testing.T) {
	seams := Seams{Store: NewMemStore(nil)}
	_, errObj := HandleAddGrants(seams, json.RawMessage(`{"tool_patterns":["missing-colon"]}`))
	if errObj == nil {
		t.Fatal("a string that is not a grant was accepted")
	}
	if errObj.Code != -32602 {
		t.Errorf("code = %d, want -32602", errObj.Code)
	}
	data, _ := errObj.Data.(string)
	if !strings.Contains(data, `invalid grant string: "missing-colon"`) {
		t.Errorf("data = %q, want the reference's sentence", data)
	}
	if !strings.Contains(data, GrantFormatHint) {
		t.Errorf("data = %q, want the reference's format hint", data)
	}
}

// TestRemoveGrantsAcceptsTheReferenceGrammar pins the remove path on the
// wire: the reference's spelling removes the grant.
func TestRemoveGrantsAcceptsTheReferenceGrammar(t *testing.T) {
	store := NewMemStore(nil)
	if _, err := store.AddGrants(context.Background(), []Grant{
		{Action: ActionCommand, Pattern: "git status"},
	}); err != nil {
		t.Fatalf("AddGrants: %v", err)
	}
	seams := Seams{Store: store}
	res, errObj := HandleRemoveGrants(seams, json.RawMessage(`{"tool_patterns":["command(git status)"]}`))
	if errObj != nil {
		t.Fatalf("HandleRemoveGrants: %v", errObj)
	}
	grants, _ := res.(map[string]any)["grants"].([]any)
	if len(grants) != 0 {
		t.Errorf("len(grants) = %d, want 0", len(grants))
	}
}

// TestLSGetSettingsRendersTheReferenceGrammar pins the language-server
// surface: a grant the reference wrote comes back as the reference's own
// grant string.
func TestLSGetSettingsRendersTheReferenceGrammar(t *testing.T) {
	store := NewMemStore(nil)
	if _, err := store.AddGrants(context.Background(), []Grant{
		{Action: ActionCommand, Pattern: "git status"},
	}); err != nil {
		t.Fatalf("AddGrants: %v", err)
	}
	seams := Seams{Store: store}
	res, errObj, handled := LSGetSettings(seams, context.Background())
	if !handled || errObj != nil {
		t.Fatalf("LSGetSettings: handled=%v err=%v", handled, errObj)
	}
	answer, _ := res.(map[string]any)
	globalGrants, _ := answer["globalGrants"].(map[string]any)
	allow, _ := globalGrants["allow"].([]string)
	if len(allow) != 1 || allow[0] != "command(git status)" {
		t.Errorf("allow = %v, want [command(git status)]", allow)
	}
}
