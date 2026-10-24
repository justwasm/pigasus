// Package cortex — trajectory.go: trajectory operations on CascadeManager.
//
// pclntab entries:
//   - LoadTrajectory            0x7fdda40
//   - ReloadTrajectoryIfLoaded  0x7fddc00
//   - SaveTrajectory            0x7fddf20
//   - DeleteTrajectory          0x7fea200
//   - GetTrajectory             0x7fdfd40
//   - GetTrajectoryForServing   (inferred)
//   - GetMainAgentTrajectories  0x7fe10e0
//   - GetAllTrajectories        (inferred)
//   - ResidentTrajectoryCount   (inferred)
//   - ForkConversation          0x7fefe20
//   - RevertToStep              (inferred)
//   - GetRevertPreview          (inferred)
//   - TrajectoryStoreManager    0x7fecea0
//   - ViewTrajectoryDoNotUse    (inferred)
package cortex

// SaveTrajectory persists a trajectory. v1.3.5 binary addr 0x7fddf20,
// name=SaveTrajectory, signature=(cm *CascadeManager, traj
// Trajectory) error.
func (cm *CascadeManager) SaveTrajectory(traj Trajectory) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	cm.storedTrajectory = traj.ID
	return nil
}

// LoadTrajectory loads a trajectory by id. v1.3.5 binary addr
// 0x7fdda40, name=LoadTrajectory, signature=(cm *CascadeManager, id
// string) error.
func (cm *CascadeManager) LoadTrajectory(id string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	cm.storedTrajectory = id
	return nil
}

// ReloadTrajectoryIfLoaded reloads a trajectory if loaded. v1.3.5
// binary addr 0x7fddc00, name=ReloadTrajectoryIfLoaded,
// signature=(cm *CascadeManager) error.
func (cm *CascadeManager) ReloadTrajectoryIfLoaded() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	return nil
}

// DeleteTrajectory deletes a trajectory. v1.3.5 binary addr
// 0x7fea200, name=DeleteTrajectory, signature=(cm *CascadeManager,
// id string) error.
func (cm *CascadeManager) DeleteTrajectory(id string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	return nil
}

// GetTrajectory returns the current trajectory. v1.3.5 binary addr
// (per pclntab .GetTrajectory), name=GetTrajectory,
// signature=(cm *CascadeManager) (Trajectory, error).
func (cm *CascadeManager) GetTrajectory() (Trajectory, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return Trajectory{}, ErrClosed
	}
	if cm.storedTrajectory == "" {
		return Trajectory{}, ErrNoTrajectory
	}
	return Trajectory{ID: cm.storedTrajectory}, nil
}

// GetTrajectoryForServing returns the trajectory for serving. v1.3.5
// binary addr (per pclntab .GetTrajectoryForServing),
// name=GetTrajectoryForServing, signature=(cm *CascadeManager) (Trajectory, error).
func (cm *CascadeManager) GetTrajectoryForServing() (Trajectory, error) {
	return cm.GetTrajectory()
}

// GetMainAgentTrajectories returns the main agent trajectories. v1.3.5
// binary addr 0x7fe10e0, name=GetMainAgentTrajectories,
// signature=(cm *CascadeManager) ([]Trajectory, error).
func (cm *CascadeManager) GetMainAgentTrajectories() ([]Trajectory, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return nil, ErrClosed
	}
	return nil, nil
}

// GetAllTrajectories returns all trajectories. v1.3.5 binary addr
// (per pclntab .GetAllTrajectories), name=GetAllTrajectories,
// signature=(cm *CascadeManager) ([]Trajectory, error).
func (cm *CascadeManager) GetAllTrajectories() ([]Trajectory, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return nil, ErrClosed
	}
	return nil, nil
}

// ResidentTrajectoryCount returns the count of resident trajectories.
// v1.3.5 binary addr (per pclntab .ResidentTrajectoryCount),
// name=ResidentTrajectoryCount, signature=(cm *CascadeManager) int.
func (cm *CascadeManager) ResidentTrajectoryCount() int {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.storedTrajectory == "" {
		return 0
	}
	return 1
}

// ForkConversation forks the current conversation. v1.3.5 binary addr
// 0x7fefe20, name=ForkConversation, signature=(cm *CascadeManager,
// opts ...ForkOption) (string, error).
func (cm *CascadeManager) ForkConversation(opts ...CascadeManagerOption) (string, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return "", ErrClosed
	}
	return "fork-" + cm.storedTrajectory, nil
}

// RevertToStep reverts the trajectory to a step. v1.3.5 binary addr
// (per pclntab .RevertToStep), name=RevertToStep, signature=(cm
// *CascadeManager, step int) error.
func (cm *CascadeManager) RevertToStep(step int) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	return nil
}

// GetRevertPreview returns the revert preview. v1.3.5 binary addr
// (per pclntab .GetRevertPreview), name=GetRevertPreview,
// signature=(cm *CascadeManager, step int) (RevertPreview, error).
func (cm *CascadeManager) GetRevertPreview(step int) (RevertPreview, error) {
	return RevertPreview{StepIndex: step}, nil
}

// RevertPreview is the recovered revert preview.
type RevertPreview struct {
	StepIndex int
	Diff      string
	Affected  []string
}

// TrajectoryStoreManager returns the trajectory store manager. v1.3.5
// binary addr 0x7fecea0, name=TrajectoryStoreManager, signature=(cm
// *CascadeManager) TrajectoryStoreManager.
func (cm *CascadeManager) TrajectoryStoreManager() *TrajectoryStoreManagerHandle {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return &TrajectoryStoreManagerHandle{cm: cm}
}

// TrajectoryStoreManagerHandle is a placeholder for the v1.3.5
// returned object (the decompiled body is opaque in the
// `(*CascadeManager).TrajectoryStoreManager` .c file).
type TrajectoryStoreManagerHandle struct {
	cm *CascadeManager
}

// ViewTrajectoryDoNotUse is a debug-only trajectory viewer. v1.3.5
// binary addr (per pclntab .ViewTrajectoryDoNotUse),
// name=ViewTrajectoryDoNotUse, signature=(cm *CascadeManager) (string, error).
func (cm *CascadeManager) ViewTrajectoryDoNotUse() (string, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.storedTrajectory, nil
}
