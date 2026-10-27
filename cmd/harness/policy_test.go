// policy_test.go — the client's tool-call policy.
//
// The precedence the SDK's own sorter defines is the contract:
//
//	scope      specific (0)  <  prefix/* (1)  <  wildcard (2)
//	decision   DENY (0)  <  ASK_USER (1)  <  ALLOW (2)
//
// The first matching rule wins, and a rule the harness cannot interpret
// is a denial.
package main

import (
	"context"
	"encoding/json"
	"testing"

	pb "github.com/justwasm/pigasus/localharness/pb/localharness"
)

// policyRuleFor builds one rule of the client's policy.
func policyRuleFor(tool string, decision pb.PolicyDecision, ruleID string) *pb.PolicyRule {
	return pb.PolicyRule_builder{
		Tool:     &tool,
		Decision: &decision,
		RuleId:   &ruleID,
	}.Build()
}

// policyConfigFor builds a policy configuration from the rules.
func policyConfigFor(rules ...*pb.PolicyRule) *pb.PolicyConfig {
	return pb.PolicyConfig_builder{Rules: rules}.Build()
}

func TestPolicyNoConfigurationAllowsEverything(t *testing.T) {
	policy := newConfigPolicy(nil)
	if !policy.empty() {
		t.Fatal("a nil configuration is not empty")
	}
	c := &conversation{configPolicy: policy}
	reason, blocked, err := c.applyClientPolicy(context.Background(), "view_file", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("applyClientPolicy: %v", err)
	}
	if blocked {
		t.Errorf("the call was blocked (%q), want it allowed", reason)
	}
}

func TestPolicyDenyRuleBlocksTheCall(t *testing.T) {
	policy := newConfigPolicy(policyConfigFor(
		policyRuleFor("view_file", pb.PolicyDecision_POLICY_DECISION_DENY, "rule-1"),
	))
	c := &conversation{configPolicy: policy}
	reason, blocked, err := c.applyClientPolicy(context.Background(), "view_file", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("applyClientPolicy: %v", err)
	}
	if !blocked {
		t.Fatal("the call was allowed, want it blocked")
	}
	if reason == "" {
		t.Error("the block reason is empty")
	}
}

func TestPolicyTheMostSpecificScopeWins(t *testing.T) {
	// The wildcard allows, the family denies, the specific tool allows.
	// The specific rule is the one that applies.
	policy := newConfigPolicy(policyConfigFor(
		policyRuleFor("*", pb.PolicyDecision_POLICY_DECISION_ALLOW, "wildcard"),
		policyRuleFor("mcp/*", pb.PolicyDecision_POLICY_DECISION_DENY, "family"),
		policyRuleFor("view_file", pb.PolicyDecision_POLICY_DECISION_ALLOW, "specific"),
	))
	rule, ok := policy.evaluate("view_file", "")
	if !ok {
		t.Fatal("no rule matched")
	}
	if rule.ruleID != "specific" {
		t.Errorf("the rule = %q, want the specific one", rule.ruleID)
	}
}

func TestPolicyDenyBeatsAllowInsideOneScope(t *testing.T) {
	// Two rules for the same tool: the deny wins, whatever order the
	// client wrote them in.
	policy := newConfigPolicy(policyConfigFor(
		policyRuleFor("view_file", pb.PolicyDecision_POLICY_DECISION_ALLOW, "allow"),
		policyRuleFor("view_file", pb.PolicyDecision_POLICY_DECISION_DENY, "deny"),
	))
	rule, ok := policy.evaluate("view_file", "")
	if !ok {
		t.Fatal("no rule matched")
	}
	if rule.ruleID != "deny" {
		t.Errorf("the rule = %q, want the deny", rule.ruleID)
	}
}

func TestPolicyAPrefixRuleMatchesItsFamily(t *testing.T) {
	policy := newConfigPolicy(policyConfigFor(
		policyRuleFor("mcp/*", pb.PolicyDecision_POLICY_DECISION_DENY, "family"),
	))
	// The family is a literal prefix: `mcp/*` covers the names that start
	// with `mcp/`.
	if _, ok := policy.evaluate("mcp/tool", ""); !ok {
		t.Error("the prefix rule did not match its family")
	}
	if _, ok := policy.evaluate("view_file", ""); ok {
		t.Error("the prefix rule matched an unrelated tool")
	}
}

func TestPolicyARuleMayBeScopedToOneServer(t *testing.T) {
	server := "files"
	policy := newConfigPolicy(policyConfigFor(
		pb.PolicyRule_builder{
			Tool:       strptr("mcp_tool"),
			ServerName: &server,
			Decision:   policyDecisionPtr(pb.PolicyDecision_POLICY_DECISION_DENY),
		}.Build(),
	))
	if _, ok := policy.evaluate("mcp_tool", "files"); !ok {
		t.Error("the rule did not match its own server")
	}
	if _, ok := policy.evaluate("mcp_tool", "other"); ok {
		t.Error("the rule matched another server")
	}
}

func TestPolicyAnUnknownDecisionIsADenial(t *testing.T) {
	// The reference never treats a verdict it cannot interpret as an
	// allow.
	policy := newConfigPolicy(policyConfigFor(
		policyRuleFor("view_file", pb.PolicyDecision_POLICY_DECISION_UNSPECIFIED, "rule-1"),
	))
	c := &conversation{configPolicy: policy}
	reason, blocked, err := c.applyClientPolicy(context.Background(), "view_file", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("applyClientPolicy: %v", err)
	}
	if !blocked {
		t.Fatal("the call was allowed, want it blocked")
	}
	if reason == "" {
		t.Error("the block reason is empty")
	}
}

func TestPolicyDenialReasonPrefersTheRulesOwnText(t *testing.T) {
	reason := "do not read the secrets"
	tool := "view_file"
	decision := pb.PolicyDecision_POLICY_DECISION_DENY
	rule := pb.PolicyRule_builder{
		Tool:       &tool,
		Decision:   &decision,
		DenyReason: &reason,
	}.Build()
	policy := newConfigPolicy(policyConfigFor(rule))
	c := &conversation{configPolicy: policy}
	got, blocked, err := c.applyClientPolicy(context.Background(), "view_file", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("applyClientPolicy: %v", err)
	}
	if !blocked {
		t.Fatal("the call was allowed, want it blocked")
	}
	if got != reason {
		t.Errorf("the reason = %q, want %q", got, reason)
	}
}

// strptr returns a pointer to a copy of the string.
func strptr(s string) *string { return &s }

// policyDecisionPtr returns a pointer to a copy of the decision.
func policyDecisionPtr(d pb.PolicyDecision) *pb.PolicyDecision { return &d }
