package toolrunner

import (
	"context"

	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/internal/app/tools/shell"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	"github.com/justwasm/pigasus/internal/core/policy/sandbox"
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
	return func(next Handler) Handler {
		return func(ctx context.Context, call backend.ToolCall) (string, error) {
			if policy == nil {
				return next(ctx, call)
			}
			target := ToolTarget(call)
			v := policy.DecideNames(target, tools.PermissionNames(call.Name)...)
			switch v {
			case permissions.Allow:
				return next(ctx, call)
			case permissions.Ask:
				if promptUser != nil {
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
func SandboxBoundaryMiddleware(sb sandbox.Sandbox) Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, call backend.ToolCall) (string, error) {
			if sb.Enabled && (call.Name == "run_command" || call.Name == "run_shell_command") {
				target := ToolTarget(call)
				if res := sb.CheckCommandWithBypass(target, shell.BypassSandboxRequested(call.Arguments)); res.Outside {
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
