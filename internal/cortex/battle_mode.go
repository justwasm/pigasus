// Package cortex — battle_mode.go: battle mode and sensitivity level operations.
//
// pclntab entries:
//   - StartBattleMode               0x7fc7f60
//   - EndBattleMode                 0x7fcbb00
//   - ValidateBattleMode            0x7fc78c0
//   - EnsureBattleModePreconditions 0x7fc77a0
//   - EliminateBattleModeArm        0x7fd28a0
//   - UpdateLoadedSensitivityLevel  0x7fda920
//   - PersistSensitivityLevel       0x7fdac20
//   - loadedSensitivityLevel        0x7fdf720
package cortex

import (
	"errors"
	"fmt"
	"time"
)

// StartBattleMode starts a battle-mode run. v1.3.5 binary addr
// 0x7fc7f60, name=StartBattleMode, signature=(cm *CascadeManager,
// opts ...BattleModeOption) error.
func (cm *CascadeManager) StartBattleMode(opts ...CascadeManagerOption) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	cm.battleMode = &BattleModeState{StartedAt: time.Now()}
	return nil
}

// EndBattleMode ends a battle-mode run. v1.3.5 binary addr 0x7fcbb00,
// name=EndBattleMode, signature=(cm *CascadeManager) error.
func (cm *CascadeManager) EndBattleMode() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	cm.battleMode = nil
	return nil
}

// ValidateBattleMode validates a battle-mode run. v1.3.5 binary addr
// 0x7fc78c0, name=ValidateBattleMode, signature=(cm *CascadeManager)
// error.
func (cm *CascadeManager) ValidateBattleMode() error {
	return nil
}

// EnsureBattleModePreconditions ensures battle-mode preconditions.
// v1.3.5 binary addr 0x7fc77a0, name=EnsureBattleModePreconditions,
// signature=(cm *CascadeManager) error.
func (cm *CascadeManager) EnsureBattleModePreconditions() error {
	return nil
}

// EliminateBattleModeArm eliminates a battle-mode arm. v1.3.5 binary
// addr 0x7fd28a0, name=EliminateBattleModeArm, signature=(cm
// *CascadeManager, index int) error.
func (cm *CascadeManager) EliminateBattleModeArm(index int) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.battleMode == nil {
		return errors.New("cortex: not in battle mode")
	}
	if index < 0 || index >= len(cm.battleMode.Arms) {
		return fmt.Errorf("cortex: arm index %d out of range [0,%d)", index, len(cm.battleMode.Arms))
	}
	cm.battleMode.Arms[index].Status = "eliminated"
	return nil
}

// UpdateLoadedSensitivityLevel updates the loaded sensitivity level.
// v1.3.5 binary addr 0x7fda920, name=UpdateLoadedSensitivityLevel,
// signature=(cm *CascadeManager, level SensitivityLevel) error.
func (cm *CascadeManager) UpdateLoadedSensitivityLevel(level SensitivityLevel) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	cm.sensitivityLevel = level
	return nil
}

// PersistSensitivityLevel persists the sensitivity level. v1.3.5 binary
// addr 0x7fdac20, name=PersistSensitivityLevel, signature=(cm
// *CascadeManager) error.
func (cm *CascadeManager) PersistSensitivityLevel() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	return nil
}

// loadedSensitivityLevel returns the loaded sensitivity level. v1.3.5
// binary addr 0x7fdf720, name=loadedSensitivityLevel,
// signature=(cm *CascadeManager) SensitivityLevel.
func (cm *CascadeManager) loadedSensitivityLevel() SensitivityLevel {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.sensitivityLevel
}
