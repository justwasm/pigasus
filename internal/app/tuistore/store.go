package tuistore

import (
	"context"

	"github.com/justwasm/pigasus/internal/app/tools"
)

// Store holds session-scoped TUI state extracted from the host,
// matching the responsibilities of agy 1.4.12's store.Manager.
type Store struct {
	// Selection state (transcript viewport)
	SelDragging bool
	SelStartRow int
	SelStartCol int
	SelEndRow   int
	SelEndCol   int

	// Selection state (input field)
	InputSelDragging bool
	InputSelStart    int
	InputSelEnd      int

	// Bash mode ("!" mode)
	BashMode   bool
	BashRowIdx int
	BashCancel context.CancelFunc
	BashRun    *tools.SafeBuffer
	BashCmd    string
	BashGen    int
	BashQueue  []string

	// Compaction state
	Compaction

	// Queue mode & stream queue
	Queue

	// Tokens state
	Tokens
}

// New creates a new Store with defaults matching agy reference behaviour.
func New() *Store {
	return &Store{
		BashRowIdx: -1,
		Queue: Queue{
			QueueMode: true, // Default queuedMessages setting is "queue"
		},
	}
}
