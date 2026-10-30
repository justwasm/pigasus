// Package builtinsubagent is a backward-compatible facade over internal/app/subagent/builtin.
package builtinsubagent

import (
	"github.com/justwasm/pigasus/internal/app/subagent/builtin"
)

type Kind = builtin.Kind

const (
	KindBrowser                           = builtin.KindBrowser
	KindImageGenerator                    = builtin.KindImageGenerator
	KindDeepCoder                         = builtin.KindDeepCoder
	KindDeepCoderWorkerL0                 = builtin.KindDeepCoderWorkerL0
	KindDeepCoderImprovementWorker         = builtin.KindDeepCoderImprovementWorker
	KindDeepInvestigator                  = builtin.KindDeepInvestigator
	KindDeepInvestigatorWorkerL0          = builtin.KindDeepInvestigatorWorkerL0
	KindDeepInvestigatorImprovementWorker = builtin.KindDeepInvestigatorImprovementWorker
)

const (
	RoleDeepCoder                         = builtin.RoleDeepCoder
	RoleDeepCoderWorkerL0                 = builtin.RoleDeepCoderWorkerL0
	RoleDeepCoderImprovementWorker         = builtin.RoleDeepCoderImprovementWorker
	RoleDeepInvestigator                  = builtin.RoleDeepInvestigator
	RoleDeepInvestigatorWorkerL0          = builtin.RoleDeepInvestigatorWorkerL0
	RoleDeepInvestigatorImprovementWorker = builtin.RoleDeepInvestigatorImprovementWorker
)

type Spec = builtin.Spec
type Handler = builtin.Handler
type Result = builtin.Result
type Registry = builtin.Registry

var (
	NewRegistry          = builtin.NewRegistry
	ErrUnknownName       = builtin.ErrUnknownName
	ErrAlreadyRegistered = builtin.ErrAlreadyRegistered
	Default              = builtin.Default
	ImageGeneratorHandler = builtin.ImageGeneratorHandler

	OrchestratorPrompt                  = builtin.OrchestratorPrompt
	DeepCoderCoordinatorPrompt          = builtin.DeepCoderCoordinatorPrompt
	DeepCoderWorkerL0Prompt             = builtin.DeepCoderWorkerL0Prompt
	DeepCoderImprovementWorkerPrompt     = builtin.DeepCoderImprovementWorkerPrompt
	DeepInvestigatorCoordinatorPrompt   = builtin.DeepInvestigatorCoordinatorPrompt
	DeepInvestigatorWorkerL0Prompt      = builtin.DeepInvestigatorWorkerL0Prompt
	DeepInvestigatorImprovementWorkerPrompt = builtin.DeepInvestigatorImprovementWorkerPrompt
)
