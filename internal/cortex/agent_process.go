// Package cortex — agent_process.go: agent process management and status queries.
//
// pclntab entries:
//   - WarmUpAgentProcess       0x7fd9340
//   - KillAgent                0x7feb060
//   - MaybeReviveAgent         0x7fe2160
//   - IsCascadeRunning         (inferred)
//   - IsQuiesced               0x7fecd80
//   - ExecutionStatus          (inferred)
//   - GetActivitySnapshot      0x7fecde0
package cortex

import "time"

// KillAgent kills a subagent. v1.3.5 binary addr 0x7feb060,
// name=KillAgent, signature=(cm *CascadeManager, subagentID string) error.
func (cm *CascadeManager) KillAgent(subagentID string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	if cm.presence != nil {
		cm.presence.remove(subagentID)
	}
	return nil
}

// MaybeReviveAgent revives a subagent if it can be revived. v1.3.5
// binary addr 0x7fe2160, name=MaybeReviveAgent, signature=(cm
// *CascadeManager, subagentID string) error.
func (cm *CascadeManager) MaybeReviveAgent(subagentID string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	return nil
}

// WarmUpAgentProcess warms up the agent process. v1.3.5 binary addr
// 0x7fd9340, name=WarmUpAgentProcess, signature=(cm *CascadeManager) error.
func (cm *CascadeManager) WarmUpAgentProcess() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	return nil
}

// IsCascadeRunning reports whether the cascade is running. v1.3.5
// binary addr (per pclntab .IsCascadeRunning), name=IsCascadeRunning,
// signature=(cm *CascadeManager) bool.
func (cm *CascadeManager) IsCascadeRunning() bool {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.executor != nil
}

// IsQuiesced reports whether the cascade is quiesced. v1.3.5 binary
// addr 0x7fecd80, name=IsQuiesced, signature=(cm *CascadeManager) bool.
func (cm *CascadeManager) IsQuiesced() bool {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.closed
}

// ExecutionStatus returns the execution status. v1.3.5 binary addr
// (per pclntab .ExecutionStatus), name=ExecutionStatus,
// signature=(cm *CascadeManager) ExecutorLoopStatus.
func (cm *CascadeManager) ExecutionStatus() ExecutorLoopStatus {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return LoopStatusQuiesced
	}
	if cm.executor == nil {
		return LoopStatusIdle
	}
	return LoopStatusBusy
}

// GetActivitySnapshot returns the activity snapshot. v1.3.5 binary
// addr 0x7fecde0, name=GetActivitySnapshot, signature=(cm
// *CascadeManager) ActivitySnapshot.
func (cm *CascadeManager) GetActivitySnapshot() ActivitySnapshot {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return ActivitySnapshot{
		TrajectoryID:  cm.storedTrajectory,
		RunningCount:  cm.presence.count(),
		BattleMode:    cm.battleMode != nil,
		Sensitivity:   cm.sensitivityLevel,
		LastUpdateUTC: time.Now().UTC(),
	}
}

// ActivitySnapshot is a snapshot of the cascade's activity (mirrors
// the v1.3.5 (*CascadeManager).GetActivitySnapshot return value).
type ActivitySnapshot struct {
	TrajectoryID  string
	RunningCount  int
	BattleMode    bool
	Sensitivity   SensitivityLevel
	LastUpdateUTC time.Time
}
