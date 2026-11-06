package shell

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/justwasm/pigasus/internal/app/tools"
)

// PersistedSchedule is the on-disk format for active timer/cron schedules.
type PersistedSchedule struct {
	TaskID         string    `json:"task_id"`
	Prompt         string    `json:"prompt"`
	DurationSec    int       `json:"duration_sec"`
	CronExpr       string    `json:"cron_expr,omitempty"`
	TimerCondition string    `json:"timer_condition,omitempty"`
	IsDaemon       bool      `json:"is_daemon,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	TargetTime     time.Time `json:"target_time"`
	ExactTiming    bool      `json:"exact_timing,omitempty"`
}

func schedulePersistPath(home, convID string) string {
	return filepath.Join(tools.ScheduleHome(home), "brain", convID, ".schedules.json")
}

// SaveActiveSchedules persists active schedules for a conversation on disk.
func SaveActiveSchedules(home, convID string, scheds []PersistedSchedule) error {
	if convID == "" {
		return nil
	}
	path := schedulePersistPath(home, convID)
	if len(scheds) == 0 {
		_ = os.Remove(path)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(scheds)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// LoadActiveSchedules loads pending schedules for a conversation from disk.
func LoadActiveSchedules(home, convID string) []PersistedSchedule {
	if convID == "" {
		return nil
	}
	path := schedulePersistPath(home, convID)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var scheds []PersistedSchedule
	if err := json.Unmarshal(data, &scheds); err != nil {
		return nil
	}
	return scheds
}

// AddActiveSchedule adds or updates an active schedule in the conversation's on-disk sidecar.
func AddActiveSchedule(home, convID string, s PersistedSchedule) error {
	if convID == "" || s.TaskID == "" {
		return nil
	}
	existing := LoadActiveSchedules(home, convID)
	found := false
	for i, e := range existing {
		if e.TaskID == s.TaskID {
			existing[i] = s
			found = true
			break
		}
	}
	if !found {
		existing = append(existing, s)
	}
	return SaveActiveSchedules(home, convID, existing)
}

// RemoveActiveSchedule removes a specific schedule by TaskID from disk.
func RemoveActiveSchedule(home, convID, taskID string) error {
	if convID == "" || taskID == "" {
		return nil
	}
	existing := LoadActiveSchedules(home, convID)
	if len(existing) == 0 {
		return nil
	}
	var filtered []PersistedSchedule
	for _, s := range existing {
		if s.TaskID != taskID {
			filtered = append(filtered, s)
		}
	}
	return SaveActiveSchedules(home, convID, filtered)
}
