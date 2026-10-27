// policy.go — the client's tool-call policy.
//
// # What the client sends
//
// The SDK expresses tool-call policies declaratively and hands them to the
// harness in `HarnessConfig.policy_config`:
//
//	rules[i]{tool, server_name, name, decision, deny_reason, is_dynamic, rule_id}
//	workspace_containment
//	auto_config
//
// A rule's `tool` is a tool name, `*` for every tool, or `prefix/*` for a
// family. `server_name` narrows the rule to one MCP server.
//
// # Which rule wins
//
// The SDK sorts its rules before it serialises them, and the order is the
// precedence:
//
//	scope      specific (0)  <  prefix/* (1)  <  wildcard (2)
//	decision   DENY (0)  <  ASK_USER (1)  <  APPROVE (2)
//
// The first matching rule wins. The sort is stable, so two rules of the
// same scope and decision keep the order the client wrote them in.
//
// # Static and dynamic rules
//
// A static rule — no predicate, no approval handler — is decided by the
// harness, with no round trip. A dynamic rule is one the client has to
// evaluate: the SDK marks a rule dynamic when it carries a `when`
// predicate or an `ASK_USER` decision, and expects the harness to ask.
//
// The harness asks with a `PolicyDecisionRequest` and the client answers
// with a `PolicyDecisionResponse`. The request's `rule_id` names the rule;
// the client looks it up in the map it built alongside the config.
//
// # Fail-closed
//
// A verdict the harness cannot interpret is a denial. The reference's own
// string says so:
//
//	"Unrecognized pre-tool hook decision %q; blocked execution for safety."
//
// The clone follows the same rule for the policy: an unknown outcome, a
// missing answer or an error is a denial, never an allow.
package main

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	piagent "github.com/MichaelKinsy/PiG/agent"

	pb "github.com/justwasm/pigasus/localharness/pb/localharness"
)

// policyRule is one evaluated rule of the client's policy.
type policyRule struct {
	tool       string
	serverName string
	name       string
	decision   pb.PolicyDecision
	denyReason string
	dynamic    bool
	ruleID     string
	// scope orders the rules: 0 is a specific tool, 1 a `prefix/*`
	// family, 2 the wildcard.
	scope int
}

// configPolicy evaluates the client's policy rules.
type configPolicy struct {
	rules []policyRule
	// workspaceContainment reports whether the client asked the harness to
	// keep every path inside the session's workspaces.
	workspaceContainment bool
}

// newConfigPolicy builds the evaluator from the client's configuration.
//
// A nil configuration means no policy: every call is allowed, which is
// what the client asked for by sending no rules.
func newConfigPolicy(cfg *pb.PolicyConfig) *configPolicy {
	policy := &configPolicy{}
	if cfg == nil {
		return policy
	}
	policy.workspaceContainment = cfg.GetWorkspaceContainment() == pb.PolicyConfig_WORKSPACE_CONTAINMENT_ENABLED
	for _, rule := range cfg.GetRules() {
		if rule == nil {
			continue
		}
		tool := strings.TrimSpace(rule.GetTool())
		if tool == "" {
			continue
		}
		policy.rules = append(policy.rules, policyRule{
			tool:       tool,
			serverName: strings.TrimSpace(rule.GetServerName()),
			name:       rule.GetName(),
			decision:   rule.GetDecision(),
			denyReason: rule.GetDenyReason(),
			dynamic:    rule.GetIsDynamic(),
			ruleID:     rule.GetRuleId(),
			scope:      policyScope(tool),
		})
	}
	sort.SliceStable(policy.rules, func(i, j int) bool {
		left, right := policy.rules[i], policy.rules[j]
		if left.scope != right.scope {
			return left.scope < right.scope
		}
		return policyDecisionOrder(left.decision) < policyDecisionOrder(right.decision)
	})
	return policy
}

// policyScope returns the scope band of a rule's tool pattern.
func policyScope(tool string) int {
	switch {
	case tool == "*":
		return 2
	case strings.HasSuffix(tool, "/*"):
		return 1
	default:
		return 0
	}
}

// policyDecisionOrder orders the decisions: DENY first, then ASK_USER,
// then ALLOW.
func policyDecisionOrder(decision pb.PolicyDecision) int {
	switch decision {
	case pb.PolicyDecision_POLICY_DECISION_DENY:
		return 0
	case pb.PolicyDecision_POLICY_DECISION_ASK_USER:
		return 1
	case pb.PolicyDecision_POLICY_DECISION_ALLOW:
		return 2
	default:
		return 3
	}
}

// matches reports whether the rule applies to a tool call.
//
// The rule's tool pattern matches the tool's name; a rule that names a
// server matches only that server's tools.
func (r policyRule) matches(toolName, serverName string) bool {
	switch {
	case r.tool == "*":
	case strings.HasSuffix(r.tool, "/*"):
		prefix := strings.TrimSuffix(r.tool, "*")
		if !strings.HasPrefix(toolName, prefix) {
			return false
		}
	default:
		if r.tool != toolName {
			return false
		}
	}
	if r.serverName != "" && r.serverName != serverName {
		return false
	}
	return true
}

// evaluate returns the first rule that applies to a tool call.
func (p *configPolicy) evaluate(toolName, serverName string) (policyRule, bool) {
	for _, rule := range p.rules {
		if rule.matches(toolName, serverName) {
			return rule, true
		}
	}
	return policyRule{}, false
}

// empty reports whether the client sent no rules at all.
func (p *configPolicy) empty() bool { return p == nil || len(p.rules) == 0 }

// apply evaluates the client's policy for one tool call.
//
// It returns the block reason and true when the call must not run.
func (c *conversation) applyClientPolicy(ctx context.Context, toolName string, args json.RawMessage) (string, bool, error) {
	policy := c.configPolicy
	if policy.empty() {
		return "", false, nil
	}
	rule, ok := policy.evaluate(toolName, "")
	if !ok {
		return "", false, nil
	}
	switch rule.decision {
	case pb.PolicyDecision_POLICY_DECISION_ALLOW:
		if rule.dynamic {
			return c.askClientPolicy(ctx, rule, toolName, args)
		}
		return "", false, nil
	case pb.PolicyDecision_POLICY_DECISION_DENY:
		if rule.dynamic {
			return c.askClientPolicy(ctx, rule, toolName, args)
		}
		return policyDenialReason(rule, toolName), true, nil
	case pb.PolicyDecision_POLICY_DECISION_ASK_USER:
		return c.askClientPolicy(ctx, rule, toolName, args)
	default:
		// An undecided rule is a denial: the reference never treats a
		// verdict it cannot interpret as an allow.
		return policyDenialReason(rule, toolName), true, nil
	}
}

// askClientPolicy sends a `PolicyDecisionRequest` and waits for the
// client's answer.
func (c *conversation) askClientPolicy(ctx context.Context, rule policyRule, toolName string, args json.RawMessage) (string, bool, error) {
	requestID := "policy-" + randomID()

	toolArgs := &pb.PreToolArgs{}
	toolArgs.SetToolName(toolName)
	toolArgs.SetArgumentsJson(string(args))
	toolArgs.SetServerName(rule.serverName)

	request := &pb.PolicyDecisionRequest{}
	request.SetRequestId(requestID)
	request.SetRuleId(rule.ruleID)
	request.SetToolArgs(toolArgs)
	request.SetReason(rule.denyReason)

	ev := &pb.OutputEvent{}
	ev.SetPolicyDecisionRequest(request)

	answer, err := c.await(ctx, requestID, func() error { return c.send(ctx, ev) })
	if err != nil {
		return "the policy decision request failed: " + err.Error(), true, err
	}
	response := answer.GetPolicyDecisionResponse()
	if response == nil {
		return "the client did not answer the policy decision request", true, nil
	}
	switch response.GetOutcome() {
	case pb.PolicyEvaluationOutcome_POLICY_EVALUATION_OUTCOME_ALLOW:
		return "", false, nil
	case pb.PolicyEvaluationOutcome_POLICY_EVALUATION_OUTCOME_DENY:
		reason := strings.TrimSpace(response.GetDenyReason())
		if reason == "" {
			reason = policyDenialReason(rule, toolName)
		}
		return reason, true, nil
	default:
		// NO_MATCH and UNSPECIFIED are not an allow.
		return policyDenialReason(rule, toolName), true, nil
	}
}

// policyDenialReason renders the text the model sees when a call is
// denied.
func policyDenialReason(rule policyRule, toolName string) string {
	if reason := strings.TrimSpace(rule.denyReason); reason != "" {
		return reason
	}
	name := rule.name
	if name == "" {
		name = rule.tool
	}
	return "The tool call to " + toolName + " was denied by the policy rule " + name + "."
}

// blockResult renders a blocked tool call.
func blockResult(reason string) piagent.ToolCallHookResult {
	return piagent.ToolCallHookResult{Block: true, Reason: reason}
}
