// Tests for the AGY 1.3.4 changelog line 40 fix: "Always allow" rules
// approved from a Remote Control browser session are now saved only
// to the shared config.json. The five tests below pin the contract:
//
//  1. RC approval writes to shared.json and nothing else (project
//     config is left untouched).
//  2. Local approval preserves existing behaviour: a rule added from
//     the TUI still lives in shared.json when the Shared scope is
//     targeted, and is not written to the project config.
//  3. Deletion from the browser's rules editor is observable as a
//     RemoveAllow with the matching source — the rule is gone from
//     shared.json after the call.
//  4. Multiple approvals accumulate (the file is append-only across
//     calls; later rules sit at the end of the list).
//  5. RC approval is idempotent: the same (Rule, Source) pair is
//     added at most once.
//
// Bonus tests (6–8) cover the rule-string helpers and the Allow
// filter used by the Shared tab of /permissions.
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAlwaysAllow_RemoteControlWritesSharedOnly pins the AGY 1.3.4
// line 40 contract: a rule approved with SourceRemoteControl lands
// in shared.json and never touches the project-level config. The
// project path is created ahead of time so a regression that
// accidentally writes to it would surface as a populated file.
func TestAlwaysAllow_RemoteControlWritesSharedOnly(t *testing.T) {
	home := t.TempDir()
	shared := filepath.Join(home, "shared.json")
	project := filepath.Join(home, "default-cli-project.json")
	// Pre-populate the project file with a sentinel value so the
	// test can prove the RC approval does not touch it.
	if err := os.WriteFile(project, []byte(`{"sentinel": true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := AlwaysAllow(shared, SourceRemoteControl, Rule{Tool: "run_command", Pattern: "rm"}); err != nil {
		t.Fatalf("AlwaysAllow: %v", err)
	}

	cfg, err := Load(shared)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Allow) != 1 {
		t.Fatalf("Allow = %+v, want one entry", cfg.Allow)
	}
	got := cfg.Allow[0]
	if got.Source != SourceRemoteControl {
		t.Errorf("Source = %q, want %q", got.Source, SourceRemoteControl)
	}
	if got.Rule != (Rule{Tool: "run_command", Pattern: "rm"}) {
		t.Errorf("Rule = %+v, want run_command:rm", got.Rule)
	}

	data, err := os.ReadFile(project)
	if err != nil {
		t.Fatalf("project file disappeared: %v", err)
	}
	if !strings.Contains(string(data), `"sentinel": true`) {
		t.Errorf("project file was rewritten: %s", data)
	}
}

// TestAlwaysAllow_LocalPreservesExistingBehaviour: a Local approval
// writes to shared.json when the Shared scope is targeted, and does
// not touch the project config. This is the contract the pre-fix
// code already kept for the TUI.
func TestAlwaysAllow_LocalPreservesExistingBehaviour(t *testing.T) {
	home := t.TempDir()
	shared := filepath.Join(home, "shared.json")
	project := filepath.Join(home, "default-cli-project.json")
	if err := os.WriteFile(project, []byte(`{"sentinel": true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := AlwaysAllow(shared, SourceLocal, Rule{Pattern: "view_file"}); err != nil {
		t.Fatalf("AlwaysAllow: %v", err)
	}
	cfg, err := Load(shared)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Allow) != 1 || cfg.Allow[0].Source != SourceLocal {
		t.Fatalf("Allow = %+v, want one Local entry", cfg.Allow)
	}
	if cfg.Allow[0].Rule.Pattern != "view_file" {
		t.Errorf("Rule.Pattern = %q, want view_file", cfg.Allow[0].Rule.Pattern)
	}

	data, err := os.ReadFile(project)
	if err != nil {
		t.Fatalf("project file disappeared: %v", err)
	}
	if !strings.Contains(string(data), `"sentinel": true`) {
		t.Errorf("Local approval rewrote the project file: %s", data)
	}
}

// TestRemoveAllow_BrowserEditorRemovesFromShared pins the deletion
// path the AGY 1.3.4 fix relies on: the browser's rules editor
// deletes by calling RemoveAllow with SourceRemoteControl, and the
// rule disappears from shared.json. Without this, a user who removes
// the rule in the browser would still see it apply.
func TestRemoveAllow_BrowserEditorRemovesFromShared(t *testing.T) {
	home := t.TempDir()
	shared := filepath.Join(home, "shared.json")
	if err := AlwaysAllow(shared, SourceRemoteControl, Rule{Tool: "run_command", Pattern: "rm"}); err != nil {
		t.Fatal(err)
	}
	if err := AlwaysAllow(shared, SourceRemoteControl, Rule{Pattern: "view_file"}); err != nil {
		t.Fatal(err)
	}

	if err := RemoveAllow(shared, SourceRemoteControl, Rule{Tool: "run_command", Pattern: "rm"}); err != nil {
		t.Fatalf("RemoveAllow: %v", err)
	}

	cfg, err := Load(shared)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Allow) != 1 {
		t.Fatalf("Allow = %+v, want one entry (view_file)", cfg.Allow)
	}
	if cfg.Allow[0].Rule.Pattern != "view_file" {
		t.Errorf("survivor = %+v, want view_file", cfg.Allow[0].Rule)
	}
}

// TestAlwaysAllow_MultipleApprovalsAccumulate: a sequence of approvals
// from either source appends in call order. The list grows, no rule
// is dropped, and RemoteControl entries do not displace Local
// entries (the per-source de-duplication keeps both).
func TestAlwaysAllow_MultipleApprovalsAccumulate(t *testing.T) {
	home := t.TempDir()
	shared := filepath.Join(home, "shared.json")
	sequence := []struct {
		src  Source
		rule Rule
	}{
		{SourceLocal, Rule{Pattern: "view_file"}},
		{SourceRemoteControl, Rule{Tool: "run_command", Pattern: "rm"}},
		{SourceLocal, Rule{Tool: "grep_search", Pattern: "src/"}},
		{SourceRemoteControl, Rule{Pattern: "list_dir"}},
	}
	for _, s := range sequence {
		if err := AlwaysAllow(shared, s.src, s.rule); err != nil {
			t.Fatalf("AlwaysAllow(%v): %v", s.rule, err)
		}
	}

	cfg, err := Load(shared)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Allow) != len(sequence) {
		t.Fatalf("Allow len = %d, want %d", len(cfg.Allow), len(sequence))
	}
	for i, want := range sequence {
		got := cfg.Allow[i]
		if got.Source != want.src {
			t.Errorf("[%d] Source = %q, want %q", i, got.Source, want.src)
		}
		if got.Rule != want.rule {
			t.Errorf("[%d] Rule = %+v, want %+v", i, got.Rule, want.rule)
		}
	}
}

// TestAlwaysAllow_RemoteControlIdempotent pins the dedup contract:
// the same (Rule, Source) pair is added at most once. A regression
// that removed the dedup would let a rapid stream of approvals grow
// the file unbounded.
func TestAlwaysAllow_RemoteControlIdempotent(t *testing.T) {
	home := t.TempDir()
	shared := filepath.Join(home, "shared.json")
	rule := Rule{Tool: "run_command", Pattern: "rm"}
	for i := 0; i < 5; i++ {
		if err := AlwaysAllow(shared, SourceRemoteControl, rule); err != nil {
			t.Fatalf("AlwaysAllow #%d: %v", i, err)
		}
	}
	cfg, err := Load(shared)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Allow) != 1 {
		t.Fatalf("Allow len = %d, want 1 (idempotent)", len(cfg.Allow))
	}
}

// TestAlwaysAllow_RemoteControlNotWrittenToProject: even if the
// caller mistakenly resolves the project path (e.g. a regression that
// routes RC approvals through writeProjectPermList), the project file
// stays untouched. The AlwaysAllow function never takes a project
// path argument — this is the contract.
func TestAlwaysAllow_RemoteControlNotWrittenToProject(t *testing.T) {
	home := t.TempDir()
	shared := filepath.Join(home, "shared.json")
	project := filepath.Join(home, "default-cli-project.json")
	before := `{"permissionGrants":{"permissionGrants":{"allow":["old:rule"]}}}`
	if err := os.WriteFile(project, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := AlwaysAllow(shared, SourceRemoteControl, Rule{Tool: "run_command", Pattern: "rm"}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(project)
	if err != nil {
		t.Fatalf("project file vanished: %v", err)
	}
	if string(data) != before {
		t.Errorf("project file changed:\nbefore: %s\nafter:  %s", before, data)
	}
}

// TestParseRuleAndString covers the rule-string helpers.
func TestParseRuleAndString(t *testing.T) {
	cases := []struct {
		in   string
		want Rule
	}{
		{"run_command:go test", Rule{Tool: "run_command", Pattern: "go test"}},
		{"view_file", Rule{Pattern: "view_file"}},
		{"!run_command:rm -rf", Rule{Tool: "run_command", Pattern: "rm -rf"}},
		{"  run_command:go test  ", Rule{Tool: "run_command", Pattern: "go test"}},
	}
	for _, c := range cases {
		got := ParseRule(c.in)
		if got != c.want {
			t.Errorf("ParseRule(%q) = %+v, want %+v", c.in, got, c.want)
		}
		if got.String() != strings.TrimPrefix(strings.TrimSpace(c.in), "!") {
			// ParseRule strips the leading "!" and the surrounding
			// spaces; String() reproduces the un-!d form. Skip
			// the case that came in with a "!" or spaces.
			if strings.HasPrefix(strings.TrimSpace(c.in), "!") {
				continue
			}
			if c.in != got.String() {
				t.Errorf("String() round-trip: in=%q out=%q", c.in, got.String())
			}
		}
	}
}

// TestAllowedByFilter: the Shared tab of /permissions filters the
// list by source. RemoteControl-only returns the RC entries;
// Local-only returns the Local entries; "" returns everything.
func TestAllowedByFilter(t *testing.T) {
	cfg := SharedConfig{
		Allow: []Entry{
			{Rule: Rule{Pattern: "a"}, Source: SourceLocal},
			{Rule: Rule{Pattern: "b"}, Source: SourceRemoteControl},
			{Rule: Rule{Pattern: "c"}, Source: SourceLocal},
		},
	}
	if got := cfg.AllowedBy(""); len(got) != 3 {
		t.Errorf("AllowedBy(\"\") len = %d, want 3", len(got))
	}
	if got := cfg.AllowedBy(SourceRemoteControl); len(got) != 1 || got[0].Rule.Pattern != "b" {
		t.Errorf("AllowedBy(RemoteControl) = %+v, want the single RC entry", got)
	}
	if got := cfg.AllowedBy(SourceLocal); len(got) != 2 {
		t.Errorf("AllowedBy(Local) len = %d, want 2", len(got))
	}
}

// TestLoadMissingFileIsEmpty: a missing file is not an error, the
// returned SharedConfig has zero entries. This is the contract the
// Shared tab of /permissions relies on for fresh installs.
func TestLoadMissingFileIsEmpty(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("Load(missing): %v", err)
	}
	if len(cfg.Allow) != 0 || len(cfg.Deny) != 0 || len(cfg.Ask) != 0 {
		t.Errorf("missing file yielded entries: %+v", cfg)
	}
}

// TestLoadCorruptFileIsEmpty: a file that fails to decode returns an
// empty SharedConfig. The AGY 1.3.4 line 40 fix cannot block startup
// on a malformed file.
func TestLoadCorruptFileIsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load(corrupt): %v", err)
	}
	if len(cfg.Allow) != 0 {
		t.Errorf("corrupt file yielded entries: %+v", cfg.Allow)
	}
}

// TestSchemaBumpedOnWrite covers the schema-version contract: a
// successful Write sets Schema to currentSchemaVersion. An old file
// (Schema = 0) gets migrated on first write.
func TestSchemaBumpedOnWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := AlwaysAllow(path, SourceLocal, Rule{Pattern: "view_file"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("post-write decode: %v", err)
	}
	schema, ok := raw["schema"].(float64)
	if !ok {
		t.Fatalf("schema field missing or wrong type: %v", raw["schema"])
	}
	if int(schema) != currentSchemaVersion {
		t.Errorf("schema = %d, want %d", int(schema), currentSchemaVersion)
	}
}
