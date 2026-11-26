package permissions

import (
	"context"
	"testing"
	"time"

	corehooks "github.com/justwasm/pigasus/internal/core/policy/hooks"
)

// scenarios_test.go is the Storybook-style scenario contract for the
// permissions package.
//
// Storybook philosophy, applied to a pure Tier-1 domain package:
//   - each scenario is constructed independently (no shared mutable fixture);
//   - each scenario is *named* and table-driven;
//   - its inputs (Policy fields, the tool call, prior grants) are explicit;
//   - its output is an observable assertion over the returned Verdict and the
//     reason that explains it.
//
// Every scenario runs against in-memory state only (MemStore / Policy
// structs); none of them touch the real filesystem or environment variables.
//
// The scenarios pin the load-bearing invariants of the permission engine:
//
//	Deny > Ask > Allow precedence, mode behaviour (plan blocks writes,
//	auto / accept-edits allow the calls they name), strict default (an
//	otherwise-undecided call falls back to ask), the clone's allow-by-default
//	divergence, prior-grant (always-allow) hits, and hook-verdict overrides.

// scenario is one independently-constructed permission decision story.
type scenario struct {
	// name is the human-facing story title.
	name string
	// policy is the fully-constructed engine under test.
	policy Policy
	// target is the call's target argument.
	target string
	// names are the tool spellings the call is judged under.
	names []string
	// want is the expected verdict.
	want Verdict
	// reason documents why the verdict is what it is.
	reason string
}

// run executes the scenario and asserts both the verdict and that the reason
// was supplied (every scenario must explain itself).
func (s scenario) run(t *testing.T) {
	t.Helper()
	got := s.policy.DecideCall(s.target, nil, s.names...)
	if got != s.want {
		t.Errorf("verdict = %v, want %v — %s", got, s.want, s.reason)
	}
	if s.reason == "" {
		t.Error("scenario has no reason; every scenario must explain its verdict")
	}
}

// TestScenario_RulePrecedence covers the Deny > Ask > Allow precedence: a
// deny rule beats an ask rule and an allow rule, and an ask rule beats an
// allow rule when no deny rule matches.
func TestScenario_RulePrecedence(t *testing.T) {
	all := func() Policy {
		return Policy{
			Allow: []Rule{{Action: ActionCommand, Pattern: "git"}},
			Ask:   []Rule{{Action: ActionCommand, Pattern: "git"}},
			Deny:  []Rule{{Action: ActionCommand, Pattern: "git"}},
		}
	}
	cases := []scenario{
		{
			name:   "deny overrides ask and allow",
			policy: all(),
			target: "git status", names: []string{"run_command"},
			want:   Deny,
			reason: "a matching deny rule wins over both the ask and the allow rule",
		},
		{
			name: "ask overrides allow when no deny matches",
			policy: func() Policy {
				p := all()
				p.Deny = nil
				return p
			}(),
			target: "git status", names: []string{"run_command"},
			want:   Ask,
			reason: "without a deny rule the ask rule prompts even though an allow rule matches",
		},
		{
			name: "allow permits when neither deny nor ask matches",
			policy: func() Policy {
				p := all()
				p.Deny, p.Ask = nil, nil
				return p
			}(),
			target: "git status", names: []string{"run_command"},
			want:   Allow,
			reason: "with only the allow rule matching, the call proceeds",
		},
	}
	for _, s := range cases {
		s := s
		t.Run(s.name, func(t *testing.T) { s.run(t) })
	}
}

// TestScenario_PlanModeBlocksWrites covers plan mode: reads are auto-approved
// but writes (and commands) are not, so they fall through to the strict ask.
func TestScenario_PlanModeBlocksWrites(t *testing.T) {
	cases := []scenario{
		{
			name:   "plan mode allows a read",
			policy: Policy{Mode: ModePlan, Strict: true},
			target: "src/main.go", names: []string{"view_file"},
			want:   Allow,
			reason: "plan mode auto-approves a read (research is allowed)",
		},
		{
			name:   "plan mode blocks a write",
			policy: Policy{Mode: ModePlan, Strict: true},
			target: "src/main.go", names: []string{"write_to_file"},
			want:   Ask,
			reason: "plan mode does not auto-approve a file edit; strict turns the undecided call into ask",
		},
		{
			name:   "plan mode blocks a command",
			policy: Policy{Mode: ModePlan, Strict: true},
			target: "date", names: []string{"run_command"},
			want:   Ask,
			reason: "plan mode does not auto-approve a command; strict turns the undecided call into ask",
		},
	}
	for _, s := range cases {
		s := s
		t.Run(s.name, func(t *testing.T) { s.run(t) })
	}
}

// TestScenario_StrictFallbackAndDefault covers the undecided-call fallback:
// under strict an otherwise-undecided call asks; under the clone's default
// (no strict, accept-edits / auto mode that does not name the class) it is
// allowed.
func TestScenario_StrictFallbackAndDefault(t *testing.T) {
	cases := []scenario{
		{
			name:   "strict mode: undecided call falls back to ask",
			policy: Policy{Strict: true},
			target: "date", names: []string{"run_command"},
			want:   Ask,
			reason: "strict restores the reference default: an otherwise-undecided call asks",
		},
		{
			name:   "default mode: undecided call is allowed",
			policy: Policy{},
			target: "date", names: []string{"run_command"},
			want:   Allow,
			reason: "the clone's documented divergence allows an otherwise-undecided call by default",
		},
		{
			name:   "accept-edits mode: a command is undecided and allowed",
			policy: Policy{Mode: ModeAcceptEdits},
			target: "date", names: []string{"run_command"},
			want:   Allow,
			reason: "accept-edits only auto-approves edits; an undecided command falls through to allow-by-default",
		},
		{
			name:   "accept-edits mode: a file edit is auto-approved",
			policy: Policy{Mode: ModeAcceptEdits, Strict: true},
			target: "a.txt", names: []string{"write_to_file"},
			want:   Allow,
			reason: "accept-edits auto-approves a file edit before strict can turn it into ask",
		},
		{
			name:   "auto mode: a safe command is auto-approved",
			policy: Policy{Mode: ModeAuto, Strict: true},
			target: "date", names: []string{"run_command"},
			want:   Allow,
			reason: "auto mode auto-approves a command before strict can turn it into ask",
		},
	}
	for _, s := range cases {
		s := s
		t.Run(s.name, func(t *testing.T) { s.run(t) })
	}
}

// TestScenario_PriorGrantHit covers a prior "always allow" grant loaded from
// the in-memory store: the grant is converted into an allow rule that covers
// the matching call, while a non-matching call still falls back to strict ask.
func TestScenario_PriorGrantHit(t *testing.T) {
	ctx := context.Background()
	store := NewMemStore(func() time.Time { return time.Unix(0, 0).UTC() })

	// The user previously chose "always allow" for `command(git status)`.
	if _, err := store.AddGrants(ctx, []Grant{{
		Action:  ActionCommand,
		Pattern: "git status",
		Reason:  "always allow git status",
	}}); err != nil {
		t.Fatalf("AddGrants: %v", err)
	}
	state, err := store.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	policy := Policy{Strict: true, Allow: state.ToRules()}
	cases := []scenario{
		{
			name:   "prior grant covers the granted command",
			policy: policy,
			target: "git status --short", names: []string{"run_command"},
			want:   Allow,
			reason: "the prior always-allow grant for `git status` covers `git status --short`",
		},
		{
			name:   "prior grant does not cover a different command",
			policy: policy,
			target: "git push origin main", names: []string{"run_command"},
			want:   Ask,
			reason: "the grant names `git status`, so `git push` is undecided and strict asks",
		},
		{
			name:   "HasGrant agrees with the verdict",
			policy: policy,
			target: "git status", names: []string{"run_command"},
			want:   Allow,
			reason: "HasGrant reports the same grant the verdict used",
		},
	}
	for _, s := range cases {
		s := s
		t.Run(s.name, func(t *testing.T) {
			s.run(t)
			if s.name == "HasGrant agrees with the verdict" {
				if !s.policy.HasGrant(s.target, s.names...) {
					t.Error("HasGrant = false, want true for a granted target")
				}
				if s.policy.HasGrant("git push", "run_command") {
					t.Error("HasGrant = true for an ungranted target, want false")
				}
			}
		})
	}
}

// TestScenario_HookVerdictOverrides covers the hook-decision-to-verdict
// mapping and the two ways a hook defers to the user's own rules: auto_approve
// yields to a matching deny rule, and deny_unless_prior_grant yields to a
// matching allow rule.
func TestScenario_HookVerdictOverrides(t *testing.T) {
	type hookScenario struct {
		name     string
		policy   Policy
		decision corehooks.Decision
		target   string
		names    []string
		want     Verdict
		reason   string
	}
	cases := []hookScenario{
		{
			name:     "hook allow lets the call proceed",
			policy:   Policy{},
			decision: corehooks.DecisionAllow,
			target:   "/tmp/x", names: []string{"run_command"},
			want:     Allow,
			reason:   "the allow decision lets the call proceed",
		},
		{
			name:     "hook deny blocks the call",
			policy:   Policy{},
			decision: corehooks.DecisionDeny,
			target:   "/tmp/x", names: []string{"run_command"},
			want:     Deny,
			reason:   "the deny decision blocks the call",
		},
		{
			name:     "hook force_ask ignores cached permissions",
			policy:   Policy{Allow: []Rule{{Action: ActionCommand, Pattern: "*"}}},
			decision: corehooks.DecisionForceAsk,
			target:   "/tmp/x", names: []string{"run_command"},
			want:     ForceAsk,
			reason:   "force_ask prompts whatever the cached allow grant says",
		},
		{
			name:     "hook auto_approve yields to a matching deny rule",
			policy:   Policy{Deny: []Rule{{Action: ActionCommand, Pattern: "/etc/"}}},
			decision: corehooks.DecisionAutoApprove,
			target:   "/etc/passwd", names: []string{"run_command"},
			want:     Deny,
			reason:   "auto_approve approves unless a user deny rule matches; here one does",
		},
		{
			name:     "hook auto_approve approves an uncovered target",
			policy:   Policy{Deny: []Rule{{Action: ActionCommand, Pattern: "/etc/"}}},
			decision: corehooks.DecisionAutoApprove,
			target:   "/tmp/x", names: []string{"run_command"},
			want:     Allow,
			reason:   "auto_approve approves a target no deny rule covers",
		},
		{
			name:     "hook deny_unless_prior_grant allows a granted call",
			policy:   Policy{Allow: []Rule{{Action: ActionCommand, Pattern: "/tmp/"}}},
			decision: corehooks.DecisionDenyUnlessPriorGrant,
			target:   "/tmp/x", names: []string{"run_command"},
			want:     Allow,
			reason:   "deny_unless_prior_grant allows a call a user allow rule covers",
		},
		{
			name:     "hook deny_unless_prior_grant blocks an ungranted call",
			policy:   Policy{Allow: []Rule{{Action: ActionCommand, Pattern: "/tmp/"}}},
			decision: corehooks.DecisionDenyUnlessPriorGrant,
			target:   "/var/x", names: []string{"run_command"},
			want:     Deny,
			reason:   "deny_unless_prior_grant blocks a call no user allow rule covers",
		},
		{
			name:     "unknown hook decision prompts",
			policy:   Policy{},
			decision: corehooks.Decision("maybe"),
			target:   "/tmp/x", names: []string{"run_command"},
			want:     Ask,
			reason:   "a decision the documentation does not name is a prompt",
		},
	}
	for _, s := range cases {
		s := s
		t.Run(s.name, func(t *testing.T) {
			got := s.policy.HookVerdict(s.decision, s.target, s.names...)
			if got != s.want {
				t.Errorf("HookVerdict(%q) = %v, want %v — %s", s.decision, got, s.want, s.reason)
			}
		})
	}
}