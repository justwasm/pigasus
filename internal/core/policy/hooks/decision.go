package hooks

import "strings"

// Decision is the answer a PreToolUse hook gives.
//
// The six values are the reference's own: they are the strings the
// documentation names for the hook's `decision` field.
type Decision string

const (
	// DecisionAllow lets the tool call proceed, delegating to the next
	// hook or to the default policy.
	DecisionAllow Decision = "allow"
	// DecisionAutoApprove approves the tool call when the default
	// policy would otherwise prompt, while honouring the rules the
	// user configured explicitly.
	DecisionAutoApprove Decision = "auto_approve"
	// DecisionDeny blocks the execution immediately.
	DecisionDeny Decision = "deny"
	// DecisionAsk prompts the user for approval, but respects the
	// "always allow" settings.
	DecisionAsk Decision = "ask"
	// DecisionForceAsk always prompts the user for approval, ignoring
	// the cached permissions.
	DecisionForceAsk Decision = "force_ask"
	// DecisionDenyUnlessPriorGrant blocks the execution unless the
	// resource was approved in a prior grant.
	DecisionDenyUnlessPriorGrant Decision = "deny_unless_prior_grant"
)

// Decisions returns the six documented decisions, in the order the
// documentation lists them.
func Decisions() []Decision {
	return []Decision{
		DecisionAllow,
		DecisionAutoApprove,
		DecisionDeny,
		DecisionAsk,
		DecisionForceAsk,
		DecisionDenyUnlessPriorGrant,
	}
}

// DecisionNames returns the six decision names as plain strings.
func DecisionNames() []string {
	decisions := Decisions()
	out := make([]string, 0, len(decisions))
	for _, d := range decisions {
		out = append(out, string(d))
	}
	return out
}

// ParseDecision reads a decision name. The second value is false when the
// name is not one of the six.
func ParseDecision(s string) (Decision, bool) {
	s = strings.TrimSpace(s)
	for _, d := range Decisions() {
		if string(d) == s {
			return d, true
		}
	}
	return "", false
}

// StopDecision is the answer a Stop hook gives.
//
// The documentation names one value: "continue" keeps the execution loop
// running. Any other value lets the agent stop.
type StopDecision string

// StopContinue is the Stop hook's "keep going" answer.
const StopContinue StopDecision = "continue"

// ParseStopDecision reads a Stop answer. The second value is false when
// the answer is not "continue".
func ParseStopDecision(s string) (StopDecision, bool) {
	if StopDecision(strings.TrimSpace(s)) == StopContinue {
		return StopContinue, true
	}
	return "", false
}

// decisionRank orders the decisions by how much they hold the tool call
// back. The strongest answer of a set of handlers is the one that counts,
// so the rank is what the aggregation compares.
//
// The order is Deny, ForceAsk, DenyUnlessPriorGrant, Ask, AutoApprove,
// Allow: a hard block beats everything, a forced prompt beats a
// conditional block (the user can still approve it), and an automatic
// approval beats nothing but a plain allow.
func decisionRank(d Decision) int {
	switch d {
	case DecisionDeny:
		return 5
	case DecisionForceAsk:
		return 4
	case DecisionDenyUnlessPriorGrant:
		return 3
	case DecisionAsk:
		return 2
	case DecisionAutoApprove:
		return 1
	}
	return 0
}

// String returns the decision's name.
func (d Decision) String() string { return string(d) }
