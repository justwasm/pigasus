package celpolicy

import (
	"github.com/justwasm/pigasus/internal/core/policy/cel"
)

// PolicyEngine wraps a CEL environment and caches compiled programs for evaluating policy rules.
//
// Deprecated: this type is a backward-compatible facade for internal/core/policy/cel.PolicyEngine.
type PolicyEngine = cel.PolicyEngine

// StandardDeclarations returns the standard CEL environment declarations for policy evaluation.
var StandardDeclarations = cel.StandardDeclarations

// NewEngine creates a new PolicyEngine with standard declarations and any additional caller-specified options.
var NewEngine = cel.NewEngine

// DefaultEngine returns the package-level singleton PolicyEngine initialized with standard declarations.
var DefaultEngine = cel.DefaultEngine

// EvalRule evaluates a boolean policy rule against variables using the default PolicyEngine.
var EvalRule = cel.EvalRule
