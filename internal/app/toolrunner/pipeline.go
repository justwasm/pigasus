package toolrunner

import (
	"context"
	"strings"

	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/internal/app/tools/shell"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	"github.com/justwasm/pigasus/internal/core/policy/sandbox"
	"github.com/justwasm/pigasus/internal/features/assessor"
)

// Handler is a function that executes a tool call.
type Handler func(ctx context.Context, call backend.ToolCall) (string, error)

// Middleware wraps a Handler to intercept or augment tool execution.
type Middleware func(next Handler) Handler

// Pipeline chains tool execution middlewares in order.
type Pipeline struct {
	middlewares []Middleware
}

// NewPipeline creates a new Middleware pipeline.
func NewPipeline(mws ...Middleware) *Pipeline {
	return &Pipeline{middlewares: mws}
}

// Wrap wraps the terminal handler with the configured middleware chain.
func (p *Pipeline) Wrap(handler Handler) Handler {
	if p == nil || len(p.middlewares) == 0 {
		return handler
	}
	cur := handler
	for i := len(p.middlewares) - 1; i >= 0; i-- {
		cur = p.middlewares[i](cur)
	}
	return cur
}

// PermissionGateMiddleware enforces permission policy and handles user approval.
func PermissionGateMiddleware(
	policy *permissions.Policy,
	promptUser func(ctx context.Context, call backend.ToolCall) (bool, error),
	onDenial func(toolName string),
) Middleware {
	return PermissionGateMiddlewareWithAmend(policy, promptUser, nil, onDenial)
}

// PermissionGateMiddlewareWithAmend enforces permission policy and allows amending tool call arguments.
func PermissionGateMiddlewareWithAmend(
	policy *permissions.Policy,
	promptUser func(ctx context.Context, call backend.ToolCall) (bool, error),
	promptAmend func(ctx context.Context, call *backend.ToolCall) (bool, error),
	onDenial func(toolName string),
) Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, call backend.ToolCall) (string, error) {
			if call.Name == "terminal" {
				return next(ctx, call)
			}
			if policy == nil {
				return next(ctx, call)
			}
			target := ToolTarget(call)
			v := policy.DecideCall(target, call.Arguments, tools.PermissionNames(call.Name)...)
			v = assessCommandVerdict(policy, call, target, v)
			switch v {
			case permissions.Allow:
				return next(ctx, call)
			case permissions.Ask:
				if promptAmend != nil {
					approved, err := promptAmend(ctx, &call)
					if err == nil && approved {
						return next(ctx, call)
					}
				} else if promptUser != nil {
					approved, err := promptUser(ctx, call)
					if err == nil && approved {
						return next(ctx, call)
					}
				}
				if onDenial != nil {
					onDenial(call.Name)
				}
				return permissions.DeniedToolResult(call.Name, target), nil
			case permissions.Deny:
				if onDenial != nil {
					onDenial(call.Name)
				}
				return permissions.DeniedToolResult(call.Name, target), nil
			default:
				return next(ctx, call)
			}
		}
	}
}

// SandboxBoundaryMiddleware enforces filesystem and path boundaries on command execution.
//
// onDenial, when set, records the call the sandbox refused. The runner's
// denial list feeds the reference's end-of-run "N tool(s) required
// approval" notice, so a sandbox refusal counts like a policy refusal.
func SandboxBoundaryMiddleware(sb sandbox.Sandbox, onDenial func(toolName string)) Middleware {
	return SandboxBoundaryMiddlewareWithMode(sb, "", nil, onDenial)
}

// SandboxBoundaryMiddlewareWithMode enforces sandbox boundaries and, in Auto mode (AGY 1.5.4),
// requests user approval before executing commands that leave the sandbox.
//
// TODO(auto-mode-model-review): the reference reviews an escaping command with
// a lightweight model before it runs (permissions.ReviewerModelForBase records
// which one, and is currently uncalled). This clone prompts the user instead,
// which is a deliberate, working behaviour — not a stub — but it is not the
// reference's. The two paths are distinguishable: a model review would not
// block on a human, so a session with nobody watching is where the difference
// shows. Implement the review here and pass promptUser as the fallback, so an
// unattended session degrades to asking rather than to denial.
func SandboxBoundaryMiddlewareWithMode(
	sb sandbox.Sandbox,
	mode permissions.AgentMode,
	promptUser func(ctx context.Context, call backend.ToolCall) (bool, error),
	onDenial func(toolName string),
) Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, call backend.ToolCall) (string, error) {
			if sb.Enabled && (call.Name == "run_command" || call.Name == "run_shell_command") {
				target := ToolTarget(call)
				// Judge the command as if no bypass were asked for: the bypass
				// skips the check entirely, so asking with it on says nothing
				// about whether the command needs to leave the sandbox — and
				// letting that answer stand ran every bypassed call through
				// unexamined.
				bypassed := shell.BypassSandboxRequested(call.Arguments)
				if res := sb.CheckCommand(target); res.Outside {
					// A bypass the model asked for, and a command that cannot
					// run sandboxed, is the one case the prompt says "requires
					// manual user approval" about: ask the user, whichever mode
					// the session is in. AGY 1.5.4's rule (Auto mode asks rather
					// than failing outright) is the same gesture, so one branch
					// covers both.
					ask := (bypassed || mode == permissions.ModeAuto) && promptUser != nil
					if ask {
						approved, err := promptUser(ctx, call)
						if err == nil && approved {
							return next(ctx, call)
						}
					}
					if onDenial != nil {
						onDenial(call.Name)
					}
					if bypassed {
						return BypassSandboxAskReason(call.Name, target), nil
					}
					if mode == permissions.ModeAuto {
						return permissions.DeniedToolResult(call.Name, target), nil
					}
					return SandboxDeniedToolResult(call.Name, target, sb.AllowedPaths()), nil
				}
			}
			return next(ctx, call)
		}
	}
}

// OutputBudgetMiddleware enforces token length and budget restrictions on tool results.
func OutputBudgetMiddleware(maxTokens int) Middleware {
	if maxTokens <= 0 {
		maxTokens = tools.DefaultMaxResultTokens
	}
	return func(next Handler) Handler {
		return func(ctx context.Context, call backend.ToolCall) (string, error) {
			res, err := next(ctx, call)
			if err == nil {
				budget := tools.ApplyOutputBudget(res, "", maxTokens)
				res = budget.Content
			}
			return res, err
		}
	}
}

func assessCommandVerdict(policy *permissions.Policy, call backend.ToolCall, target string, v permissions.Verdict) permissions.Verdict {
	if policy == nil || policy.SkipPermissions || v != permissions.Allow {
		return v
	}
	if call.Name != "run_command" && call.Name != "run_shell_command" {
		return v
	}
	if policy.ToolPermission == permissions.ToolPermissionAlwaysProceed {
		return v
	}
	explicit := *policy
	explicit.Mode = ""
	explicit.Strict = true
	if explicit.DecideNames(target, tools.PermissionNames(call.Name)...) == permissions.Allow {
		a := assessor.Assess(target)
		if a.Verdict == assessor.NeedsExactMatch || a.HasSubstitution || a.HasRedirection {
			if !hasExactOrWildcardCommandAllow(policy.Allow, target) {
				return permissions.Ask
			}
		}
		return permissions.Allow
	}
	if policy.Mode == permissions.ModeAuto {
		allow, ask, _ := assessor.EvaluateForAutoMode(target)
		if allow {
			return permissions.Allow
		}
		if ask {
			return permissions.Ask
		}
		return permissions.Deny
	}
	return v
}

func hasExactOrWildcardCommandAllow(rules []permissions.Rule, target string) bool {
	for _, r := range rules {
		if r.Action != permissions.ActionCommand {
			continue
		}
		if r.Pattern == "" || r.Pattern == "*" || r.Pattern == target || strings.HasPrefix(r.Pattern, "regex:") {
			return true
		}
	}
	return false
}
