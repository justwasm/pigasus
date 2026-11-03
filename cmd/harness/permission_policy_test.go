// permission_policy_test.go — the harness's permission policy.
//
// The harness enforces the same rules the CLI does: the settings.json under
// the agent home carries the reference's three lists, and the harness loads
// all three. An `ask` rule is not an allow — the reference's precedence is
// Deny > Ask > Allow.
package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/justwasm/pigasus/internal/core/policy/permissions"
)

// TestHarnessPolicyLoadsTheThreeLists pins that the harness loads the
// reference's `allow`, `ask` and `deny` lists out of settings.json.
func TestHarnessPolicyLoadsTheThreeLists(t *testing.T) {
	dir := t.TempDir()
	settings := `{
	  "permissions": {
	    "allow": ["command(git status)"],
	    "ask":   ["command(git push)"],
	    "deny":  ["write_file(.env)"]
	  }
	}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(settings), 0o600); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}

	policy := harnessPolicy(dir)

	if len(policy.Allow) != 1 || policy.Allow[0].Action != "command" || policy.Allow[0].Pattern != "git status" {
		t.Errorf("allow = %+v, want one command(git status) rule", policy.Allow)
	}
	if len(policy.Ask) != 1 || policy.Ask[0].Action != "command" || policy.Ask[0].Pattern != "git push" {
		t.Errorf("ask = %+v, want one command(git push) rule", policy.Ask)
	}
	if len(policy.Deny) != 1 || policy.Deny[0].Action != "write_file" || policy.Deny[0].Pattern != ".env" {
		t.Errorf("deny = %+v, want one write_file(.env) rule", policy.Deny)
	}
}

// TestHarnessPolicyAnAskRuleIsNotAnAllow pins the precedence the harness
// enforces: a call an `ask` rule names is not permitted, even when an
// `allow` rule covers it too.
func TestHarnessPolicyAnAskRuleIsNotAnAllow(t *testing.T) {
	dir := t.TempDir()
	settings := `{
	  "permissions": {
	    "allow": ["command(git)"],
	    "ask":   ["command(git push)"]
	  }
	}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(settings), 0o600); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}

	policy := harnessPolicy(dir)

	if got := policy.Decide(permissions.Request{Tool: "run_command", Target: "git push"}); got == permissions.Allow {
		t.Errorf("an ask rule permitted the call, want it not permitted")
	}
	if got := policy.Decide(permissions.Request{Tool: "run_command", Target: "git status"}); got != permissions.Allow {
		t.Errorf("the allow rule did not permit the call, got %v", got)
	}
}
