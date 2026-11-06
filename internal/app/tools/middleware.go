package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Middleware wraps a Tool to intercept execution, adding cross-cutting concerns
// such as input validation, panic recovery, audit logging, and timeout bounding.
type Middleware func(next Tool) Tool

// Chain combines multiple middlewares into a single middleware function.
// Middlewares execute in outer-to-inner order: the first middleware passed
// executes first on request and last on response.
func Chain(mws ...Middleware) Middleware {
	return func(final Tool) Tool {
		for i := len(mws) - 1; i >= 0; i-- {
			if mws[i] != nil {
				final = mws[i](final)
			}
		}
		return final
	}
}

// Wrap applies a sequence of middlewares to a Tool.
func Wrap(t Tool, mws ...Middleware) Tool {
	if t == nil || len(mws) == 0 {
		return t
	}
	return Chain(mws...)(t)
}

// UnwrapTool uncurries any middleware wrappers, returning the innermost underlying Tool.
func UnwrapTool(t Tool) Tool {
	for {
		if u, ok := t.(interface{ Unwrap() Tool }); ok {
			inner := u.Unwrap()
			if inner == nil || inner == t {
				return t
			}
			t = inner
		} else {
			return t
		}
	}
}

// CustomMiddleware creates a Middleware using a functional execution interceptor.
func CustomMiddleware(fn func(ctx context.Context, next Tool, args json.RawMessage) (any, error)) Middleware {
	return func(next Tool) Tool {
		return &middlewareTool{
			inner: next,
			run: func(ctx context.Context, args json.RawMessage) (any, error) {
				if fn != nil {
					return fn(ctx, next, args)
				}
				return next.Run(ctx, args)
			},
		}
	}
}

type middlewareTool struct {
	inner Tool
	run   func(ctx context.Context, args json.RawMessage) (any, error)
}

func (m *middlewareTool) Name() string            { return m.inner.Name() }
func (m *middlewareTool) Schema() json.RawMessage { return m.inner.Schema() }

func (m *middlewareTool) Run(ctx context.Context, args json.RawMessage) (any, error) {
	if m.run != nil {
		return m.run(ctx, args)
	}
	return m.inner.Run(ctx, args)
}

func (m *middlewareTool) Description() string {
	return Describe(m.inner)
}

func (m *middlewareTool) Unwrap() Tool {
	return m.inner
}

// ---- Built-in Middlewares --------------------------------------------------

// ValidationMiddleware checks that arguments form well-formed JSON when non-empty.
// If arguments are malformed, it returns a descriptive error immediately without
// executing the downstream tool.
func ValidationMiddleware() Middleware {
	return func(next Tool) Tool {
		return &middlewareTool{
			inner: next,
			run: func(ctx context.Context, args json.RawMessage) (any, error) {
				trimmed := string(args)
				if len(args) > 0 && trimmed != "" && !json.Valid(args) {
					return nil, fmt.Errorf("tool %s: invalid JSON arguments: %s", next.Name(), trimmed)
				}
				return next.Run(ctx, args)
			},
		}
	}
}

// RecoveryMiddleware catches any panic occurring during tool execution and converts
// it into a clean, model-reportable error rather than crashing the process.
func RecoveryMiddleware(onPanic ...func(tool string, p any)) Middleware {
	return func(next Tool) Tool {
		return &middlewareTool{
			inner: next,
			run: func(ctx context.Context, args json.RawMessage) (res any, err error) {
				defer func() {
					if p := recover(); p != nil {
						for _, fn := range onPanic {
							if fn != nil {
								fn(next.Name(), p)
							}
						}
						err = fmt.Errorf("tool %s panicked: %v", next.Name(), p)
					}
				}()
				return next.Run(ctx, args)
			},
		}
	}
}

// AuditRecord captures metadata for one tool execution invocation.
type AuditRecord struct {
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
	Duration  time.Duration   `json:"duration_ns"`
	Success   bool            `json:"success"`
	Error     error           `json:"error,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
}

// AuditLogger receives audit records for executed tools.
type AuditLogger interface {
	LogToolExecution(rec AuditRecord)
}

// AuditLogFn is an adapter allowing a bare function to serve as an AuditLogger.
type AuditLogFn func(rec AuditRecord)

func (fn AuditLogFn) LogToolExecution(rec AuditRecord) {
	if fn != nil {
		fn(rec)
	}
}

// LoggingMiddleware records execution duration, argument payloads, and outcomes
// to the provided AuditLogger.
func LoggingMiddleware(logger AuditLogger) Middleware {
	return func(next Tool) Tool {
		return &middlewareTool{
			inner: next,
			run: func(ctx context.Context, args json.RawMessage) (any, error) {
				start := time.Now()
				res, err := next.Run(ctx, args)
				if logger != nil {
					logger.LogToolExecution(AuditRecord{
						Tool:      next.Name(),
						Arguments: args,
						Duration:  time.Since(start),
						Success:   err == nil,
						Error:     err,
						Timestamp: start,
					})
				}
				return res, err
			},
		}
	}
}

// TimeoutMiddleware bounds tool execution with a timeout if the context does
// not already define a shorter deadline.
func TimeoutMiddleware(timeout time.Duration) Middleware {
	return func(next Tool) Tool {
		return &middlewareTool{
			inner: next,
			run: func(ctx context.Context, args json.RawMessage) (any, error) {
				if timeout <= 0 {
					return next.Run(ctx, args)
				}
				ctxWithTimeout, cancel := context.WithTimeout(ctx, timeout)
				defer cancel()
				return next.Run(ctxWithTimeout, args)
			},
		}
	}
}
