// Package cortex — cortex.go: main CascadeManager interface + stub
// implementations, integrated 1:1 with v1.3.5 binary
// `google3/third_party/jetski/cortex/cortex` package.
//
// This file provides:
//
//   - CascadeManager (the core orchestrator struct, pclntab 0x7fdb120).
//   - NewCascadeManager (mirrors v1.3.5 cortex.NewCascadeManager
//     at 0x7fdb120, the constructor).
//   - (*CascadeManager).New (constructor, pclntab 0x7fdb120).
//
// Every method is a **stub** that returns a zero value (or
// `panic("TODO: cortex.<func>")` if the return type cannot be zero).
// Each stub carries a doc comment naming the v1.3.5 binary address,
// the original func name, and the signature.
package cortex

import (
	"errors"
	"sync"
)

// Errors used by the cortex package.
var (
	// ErrClosed is returned when an operation is attempted on a
	// closed CascadeManager.
	ErrClosed = errors.New("cortex: CascadeManager is closed")
	// ErrNoTrajectory is returned when a trajectory operation is
	// attempted on a CascadeManager with no stored trajectory.
	ErrNoTrajectory = errors.New("cortex: no stored trajectory")
	// ErrBattleMode is the sentinel for EndBattleModeError.
	ErrBattleMode = errors.New("cortex: battle mode end failed")
)

// CascadeManager is the recovered `(*CascadeManager)` from
// google3/third_party/jetski/cortex/cortex. It is the central
// controller for an agent run — manages trajectories, subagents,
// sensitivities, battle mode, annotations, and the underlying
// execution loop.
//
// In the 1.3.5 binary, CascadeManager contains ~60 pointer fields
// across multiple subsystems. We recover the core subset needed
// for cycle 42 + the stubs that mirror the binary's method set.
type CascadeManager struct {
	mu sync.Mutex

	// Trajectory / store
	storedTrajectory string // cached trajectory id (storedTrajectory 0x7fd8ec0)
	// Executor
	executor Executor // sdkExecutor or ordinaryExecutor
	// Subagent hub (gatedSubagentHub 0x7fe46e0, SubagentHub getter)
	subagentHub SubagentHubLike
	// Workspace
	workspaceURIs []string // resolved workspace URIs
	// Sensitivity
	sensitivityLevel SensitivityLevel
	// Battle mode
	battleMode *BattleModeState // nil when not in battle mode
	// Background runner
	bg *backgroundRunner
	// Presence tracker
	presence *presenceTracker
	// Project lookup
	projectLookup *projectLookupImpl
	// Provider URL map
	providerURLs *providerServerURLMap
	// Annotations
	annotations *AnnotationsManager
	// Summaries store
	summaries stateSyncSummariesStore
	// Settings provider (in-memory CLI settings)
	cliSettings *CLISettingsProvider
	// State
	closed bool
}

// NewCascadeManager returns a fresh CascadeManager. Mirrors v1.3.5
// cortex.NewCascadeManager at 0x7fdb120.
//
// The 1.3.5 binary takes a single *CascadeManager and returns a fresh
// one; we take options (zero or more WithXxx helpers) and
// return a fresh CascadeManager.
func NewCascadeManager(opts ...CascadeManagerOption) *CascadeManager {
	cm := &CascadeManager{
		providerURLs:     &providerServerURLMap{urls: map[string]string{}},
		projectLookup:    &projectLookupImpl{cache: map[string]string{}},
		presence:         &presenceTracker{active: map[string]*presenceEntry{}},
		bg:               &backgroundRunner{},
		annotations:      &AnnotationsManager{cache: map[string]Annotation{}},
		summaries:        stateSyncSummariesStore{},
		cliSettings:      &CLISettingsProvider{values: map[string]any{}},
		sensitivityLevel: SensitivityInternal,
	}
	for _, o := range opts {
		o(cm)
	}
	return cm
}

// New is the recovered `(*CascadeManager).New` constructor
// (pclntab 0x7fdb120, the same address as the package-level
// cortex.NewCascadeManager). It takes a `cm *CascadeManager` (the
// receiver) and returns a fresh `*CascadeManager`.
//
// v1.3.5 binary addr 0x7fdb120, name=New, signature=(cm *CascadeManager) *CascadeManager.
func (cm *CascadeManager) New() *CascadeManager {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return NewCascadeManager()
}
