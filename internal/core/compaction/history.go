package compaction

import (
	"sync"
	"time"
)

// Event records one compaction that occurred during this process session.
type Event struct {
	Seq             int
	Timestamp       time.Time
	Reason          string // e.g. "threshold", "manual", "overflow"
	TokensBefore    int
	TokensAfter     int
	StepsSummarized int
	Duration        time.Duration
	Summary         string
}

// SavedTokens returns TokensBefore - TokensAfter.
func (e Event) SavedTokens() int {
	return e.TokensBefore - e.TokensAfter
}

// PercentReduced returns the percentage of tokens removed by compaction.
func (e Event) PercentReduced() float64 {
	if e.TokensBefore <= 0 {
		return 0
	}
	saved := e.TokensBefore - e.TokensAfter
	if saved <= 0 {
		return 0
	}
	return (float64(saved) / float64(e.TokensBefore)) * 100
}

var (
	historyMu sync.RWMutex
	history   []Event
	nextSeq   int
)

// RecordEvent appends an event to the global compaction history.
func RecordEvent(e Event) Event {
	historyMu.Lock()
	defer historyMu.Unlock()
	nextSeq++
	e.Seq = nextSeq
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now()
	}
	history = append(history, e)
	return e
}

// History returns a copy of recorded compaction events, oldest first.
func History() []Event {
	historyMu.RLock()
	defer historyMu.RUnlock()
	out := make([]Event, len(history))
	copy(out, history)
	return out
}

// ResetHistory clears compaction history. It exists for tests.
func ResetHistory() {
	historyMu.Lock()
	defer historyMu.Unlock()
	history = nil
	nextSeq = 0
}
