package permissions

import (
	"testing"

	corehooks "github.com/justwasm/pigasus/internal/core/policy/hooks"
)

// TestHookVerdictMapsTheSixDecisions pins the mapping from a
// hooks.json decision onto the tool-permission verdict.
//
// The six decisions are the reference's own words, and the mapping is
// the clone's answer about the same call:
//
//   - "allow" lets the call proceed.
//   - "auto_approve" approves it, unless one of the user's own deny
//     rules matches.
//   - "deny" blocks it.
//   - "ask" prompts the user.
//   - "force_ask" prompts the user whatever the cached permissions say.
//   - "deny_unless_prior_grant" blocks the call unless one of the user's
//     allow rules covers it.
func TestHookVerdictMapsTheSixDecisions(t *testing.T) {
	p := Policy{}
	cases := []struct {
		decision corehooks.Decision
		want     Verdict
	}{
		{corehooks.DecisionAllow, Allow},
		{corehooks.DecisionAutoApprove, Allow},
		{corehooks.DecisionDeny, Deny},
		{corehooks.DecisionAsk, Ask},
		{corehooks.DecisionForceAsk, ForceAsk},
		{corehooks.DecisionDenyUnlessPriorGrant, Deny},
	}
	for _, c := range cases {
		if got := p.HookVerdict(c.decision, "/tmp/x", "run_command"); got != c.want {
			t.Errorf("HookVerdict(%q) = %v, want %v", c.decision, got, c.want)
		}
	}
	// A decision the documentation does not name is a prompt: an
	// unusable answer must not let a tool call through.
	if got := p.HookVerdict("maybe", "/tmp/x", "run_command"); got != Ask {
		t.Errorf("HookVerdict on an unknown decision = %v, want Ask", got)
	}
}

// TestHookVerdictAutoApproveHonoursTheDenyRules pins the one rule the
// "auto_approve" decision has to respect: the user's own deny rules
// still block the call.
func TestHookVerdictAutoApproveHonoursTheDenyRules(t *testing.T) {
	p := Policy{Deny: []Rule{{Tool: "run_command", Pattern: "/etc/"}}}
	if got := p.HookVerdict(corehooks.DecisionAutoApprove, "/etc/passwd", "run_command"); got != Deny {
		t.Errorf("auto_approve on a denied target = %v, want Deny", got)
	}
	if got := p.HookVerdict(corehooks.DecisionAutoApprove, "/tmp/x", "run_command"); got != Allow {
		t.Errorf("auto_approve on an allowed target = %v, want Allow", got)
	}
}

// TestHookVerdictDenyUnlessPriorGrant pins the conditional block: the
// call is blocked unless one of the user's own allow rules — the "always
// allow" grants — covers it.
func TestHookVerdictDenyUnlessPriorGrant(t *testing.T) {
	p := Policy{Allow: []Rule{{Tool: "run_command", Pattern: "/tmp/"}}}
	if got := p.HookVerdict(corehooks.DecisionDenyUnlessPriorGrant, "/tmp/x", "run_command"); got != Allow {
		t.Errorf("deny_unless_prior_grant with a grant = %v, want Allow", got)
	}
	if got := p.HookVerdict(corehooks.DecisionDenyUnlessPriorGrant, "/var/x", "run_command"); got != Deny {
		t.Errorf("deny_unless_prior_grant without a grant = %v, want Deny", got)
	}
}

// TestHookVerdictJudgesBothSpellings pins the rule that makes a
// permission rule reach a subagent: the call is judged under both
// names the tool is known by.
func TestHookVerdictJudgesBothSpellings(t *testing.T) {
	p := Policy{Deny: []Rule{{Tool: "write_file", Pattern: "/etc/"}}}
	if got := p.HookVerdict(corehooks.DecisionAutoApprove, "/etc/passwd", "write_to_file", "write_file"); got != Deny {
		t.Errorf("auto_approve under both spellings = %v, want Deny", got)
	}
}

// TestHasGrant pins the grant test the host asks: a grant is one of the
// user's own allow rules, and it is not the policy's default.
func TestHasGrant(t *testing.T) {
	p := Policy{Allow: []Rule{{Tool: "run_command", Pattern: "/tmp/"}}}
	if !p.HasGrant("/tmp/x", "run_command") {
		t.Error("HasGrant on a granted target = false, want true")
	}
	if p.HasGrant("/var/x", "run_command") {
		t.Error("HasGrant on an ungranted target = true, want false")
	}
	// The policy's own default is not a grant: the clone allows by
	// default, and a hook's "ask" must still ask.
	empty := Policy{}
	if empty.HasGrant("/var/x", "run_command") {
		t.Error("HasGrant on a policy without rules = true, want false")
	}
}
