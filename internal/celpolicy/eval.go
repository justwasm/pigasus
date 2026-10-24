package celpolicy

import (
	"fmt"
	"sync"

	"github.com/google/cel-go/cel"
)

// PolicyEngine wraps a CEL environment and caches compiled programs for evaluating policy rules.
type PolicyEngine struct {
	env   *cel.Env
	cache sync.Map
}

var (
	defaultEngineOnce sync.Once
	defaultEngineInstance *PolicyEngine
	defaultEngineErr      error
)

// StandardDeclarations returns the standard CEL environment declarations for policy evaluation:
// - command (string): command line string for shell/exec
// - tool (string): tool name being executed
// - path (string): file system path
// - env (map[string]string): environment variable mappings
func StandardDeclarations() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Variable("command", cel.StringType),
		cel.Variable("tool", cel.StringType),
		cel.Variable("path", cel.StringType),
		cel.Variable("env", cel.MapType(cel.StringType, cel.StringType)),
	}
}

// NewEngine creates a new PolicyEngine with standard declarations and any additional caller-specified options.
func NewEngine(declarations ...cel.EnvOption) (*PolicyEngine, error) {
	opts := append(StandardDeclarations(), declarations...)
	env, err := cel.NewEnv(opts...)
	if err != nil {
		return nil, fmt.Errorf("celpolicy: failed to create CEL environment: %w", err)
	}
	return &PolicyEngine{
		env: env,
	}, nil
}

// Env returns the underlying CEL environment.
func (e *PolicyEngine) Env() *cel.Env {
	return e.env
}

// prepareVars ensures standard variables exist with sensible zero values if omitted.
func prepareVars(vars map[string]any) map[string]any {
	result := map[string]any{
		"command": "",
		"tool":    "",
		"path":    "",
		"env":     map[string]string{},
	}
	for k, v := range vars {
		result[k] = v
	}
	return result
}

// compile compiles the given expression into a cel.Program, verifying it outputs a boolean.
func (e *PolicyEngine) compile(expression string) (cel.Program, error) {
	if cached, ok := e.cache.Load(expression); ok {
		return cached.(cel.Program), nil
	}

	ast, issues := e.env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("celpolicy: compile error: %w", issues.Err())
	}

	if ast.OutputType() != cel.BoolType {
		return nil, fmt.Errorf("celpolicy: expression must evaluate to bool, got %v", ast.OutputType())
	}

	prg, err := e.env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("celpolicy: program error: %w", err)
	}

	e.cache.Store(expression, prg)
	return prg, nil
}

// Eval compiles and evaluates a boolean policy expression against the provided variables.
func (e *PolicyEngine) Eval(expression string, vars map[string]any) (bool, error) {
	prg, err := e.compile(expression)
	if err != nil {
		return false, err
	}

	val, _, err := prg.Eval(prepareVars(vars))
	if err != nil {
		return false, fmt.Errorf("celpolicy: eval error: %w", err)
	}

	boolVal, ok := val.Value().(bool)
	if !ok {
		return false, fmt.Errorf("celpolicy: expected boolean result, got %T", val.Value())
	}
	return boolVal, nil
}

// DefaultEngine returns the package-level singleton PolicyEngine initialized with standard declarations.
func DefaultEngine() (*PolicyEngine, error) {
	defaultEngineOnce.Do(func() {
		defaultEngineInstance, defaultEngineErr = NewEngine()
	})
	if defaultEngineErr != nil {
		return nil, defaultEngineErr
	}
	return defaultEngineInstance, nil
}

// EvalRule evaluates a boolean policy rule against variables using the default PolicyEngine.
func EvalRule(rule string, vars map[string]any) (bool, error) {
	engine, err := DefaultEngine()
	if err != nil {
		return false, err
	}
	return engine.Eval(rule, vars)
}
