// Package session provides a unified domain facade for conversation sessions,
// coordinating SQLite trajectory storage, queued prompt persistence, and active
// timer schedules with 100% on-disk Antigravity compatibility.
package session

import (
	"fmt"
	"time"

	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/app/tools/shell"
)

// Snapshot represents a complete point-in-time state of a conversation session,
// aggregating stored SQLite steps, queued prompts, and active timers/cron schedules.
type Snapshot struct {
	ID              string
	Conversation    *convstore.Conversation
	QueuedPrompts   []string
	ActiveSchedules []shell.PersistedSchedule
	LoadedAt        time.Time
}

// Store provides a unified facade for managing conversation session persistence,
// ensuring 100% byte-for-byte on-disk compatibility with standard Antigravity storage.
type Store struct {
	home string
}

// NewStore returns a unified session store instance rooted at home (or default if empty).
func NewStore(home string) *Store {
	return &Store{home: home}
}

// Load loads the complete session snapshot for the given conversation ID.
// It loads the SQLite conversation record, persisted prompt queue, and active schedules.
func (s *Store) Load(convID string) (*Snapshot, error) {
	if convID == "" {
		return nil, fmt.Errorf("session: conversation ID must not be empty")
	}

	path := convstore.FindConversation(convID)
	var c *convstore.Conversation
	if path != "" {
		loaded, err := convstore.LoadConversation(path)
		if err == nil {
			c = loaded
		}
	}
	if c == nil {
		c = &convstore.Conversation{ID: convID}
	}

	queued := convstore.LoadQueuedPrompts(convID)
	scheds := shell.LoadActiveSchedules(s.home, convID)

	return &Snapshot{
		ID:              convID,
		Conversation:    c,
		QueuedPrompts:   queued,
		ActiveSchedules: scheds,
		LoadedAt:        time.Now(),
	}, nil
}

// SaveQueued updates the persisted queue of prompts for this session.
func (s *Store) SaveQueued(convID string, prompts []string) error {
	if convID == "" {
		return nil
	}
	return convstore.SaveQueuedPrompts(convID, prompts)
}

// LoadQueued retrieves the pending queue of prompts.
func (s *Store) LoadQueued(convID string) []string {
	if convID == "" {
		return nil
	}
	return convstore.LoadQueuedPrompts(convID)
}

// LoadSchedules retrieves active timer/cron schedules for this session.
func (s *Store) LoadSchedules(convID string) []shell.PersistedSchedule {
	if convID == "" {
		return nil
	}
	return shell.LoadActiveSchedules(s.home, convID)
}

// RemoveSchedule removes an active schedule from persistent storage.
func (s *Store) RemoveSchedule(convID, taskID string) error {
	if convID == "" || taskID == "" {
		return nil
	}
	return shell.RemoveActiveSchedule(s.home, convID, taskID)
}
