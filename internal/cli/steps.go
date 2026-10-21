// Package cli - steps.go implements a 1:1 reimplementation of
// google3/third_party/jetski/cli/steps from the v1.3.4 binary.
//
// jetski/cli/steps tracks multi-step status for shell-style command
// output, e.g.:
//
//   ✓ Initialize workspace (1.2s)
//   ⟳ Indexing source files...
//   ✗ Run tests (3 attempts)
//
// v1.3.4 binary uses this when --output-format=steps is requested
// and when piping output to a CI system.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"
)

// StepStatus is the lifecycle state of a single step. v1.3.4 binary
// uses lowercase string names in JSON output.
type StepStatus string

const (
	StepPending   StepStatus = "pending"
	StepRunning   StepStatus = "running"
	StepSucceeded StepStatus = "succeeded"
	StepFailed    StepStatus = "failed"
	StepSkipped   StepStatus = "skipped"
)

func (s StepStatus) String() string { return string(s) }

// Step is a single unit of work tracked by the steps engine.
type Step struct {
	Name      string     `json:"name"`
	Status    StepStatus `json:"status"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   time.Time  `json:"ended_at,omitempty"`
	Message   string     `json:"message,omitempty"`
}

// Steps is an ordered collection of Steps with a mutex. v1.3.4 binary
// shares a single Steps instance across goroutines (each tool run
// appends / updates its own step).
type Steps struct {
	mu     sync.Mutex
	steps  []*Step
	byName map[string]*Step
	out    io.Writer
	emit   bool // true when --output-format=steps
}

// NewSteps builds a fresh steps engine. `emit` controls whether
// step changes are written to `out` in real time.
func NewSteps(out io.Writer, emit bool) *Steps {
	return &Steps{
		byName: map[string]*Step{},
		out:    out,
		emit:   emit,
	}
}

// Add registers a new step in "pending" state. If a step with the same
// name already exists, the existing one is reused.
func (s *Steps) Add(name string) *Step {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.byName[name]; ok {
		return existing
	}
	st := &Step{Name: name, Status: StepPending}
	s.steps = append(s.steps, st)
	s.byName[name] = st
	return st
}

// Start marks the step as running. Returns the step for chaining.
func (s *Steps) Start(name string) *Step {
	st := s.Add(name)
	s.mu.Lock()
	st.Status = StepRunning
	st.StartedAt = time.Now()
	s.mu.Unlock()
	s.maybeEmit(st)
	return st
}

// Succeed marks the step as succeeded.
func (s *Steps) Succeed(name, msg string) {
	s.mu.Lock()
	if st, ok := s.byName[name]; ok {
		st.Status = StepSucceeded
		st.EndedAt = time.Now()
		st.Message = msg
		s.mu.Unlock()
		s.maybeEmit(st)
	} else {
		s.mu.Unlock()
	}
}

// Fail marks the step as failed.
func (s *Steps) Fail(name, msg string) {
	s.mu.Lock()
	if st, ok := s.byName[name]; ok {
		st.Status = StepFailed
		st.EndedAt = time.Now()
		st.Message = msg
		s.mu.Unlock()
		s.maybeEmit(st)
	} else {
		s.mu.Unlock()
	}
}

// Skip marks the step as skipped.
func (s *Steps) Skip(name, msg string) {
	s.mu.Lock()
	if st, ok := s.byName[name]; ok {
		st.Status = StepSkipped
		st.EndedAt = time.Now()
		st.Message = msg
		s.mu.Unlock()
		s.maybeEmit(st)
	} else {
		s.mu.Unlock()
	}
}

// All returns a snapshot of the steps list in insertion order.
func (s *Steps) All() []*Step {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Step, len(s.steps))
	copy(out, s.steps)
	return out
}

// BuildJSONOutput serializes the steps to JSON. v1.3.4 binary uses
// this when --output-format=json and the parent needs the full
// step history.
func (s *Steps) BuildJSONOutput() ([]byte, error) {
	return json.MarshalIndent(s.All(), "", "  ")
}

// maybeEmit writes the step to the configured output if `emit` is true.
// Format mirrors v1.3.4 binary:
//   ✓ <name> (<duration>)
//   ✗ <name> (<duration>): <message>
func (s *Steps) maybeEmit(st *Step) {
	if !s.emit || s.out == nil {
		return
	}
	sym := mapStatusToSymbol(st.Status)
	dur := ""
	if !st.StartedAt.IsZero() {
		var d time.Duration
		if st.EndedAt.IsZero() {
			d = time.Since(st.StartedAt)
		} else {
			d = st.EndedAt.Sub(st.StartedAt)
		}
		dur = fmt.Sprintf(" (%.1fs)", d.Seconds())
	}
	if st.Message != "" {
		fmt.Fprintf(s.out, "%s %s%s: %s\n", sym, st.Name, dur, st.Message)
	} else {
		fmt.Fprintf(s.out, "%s %s%s\n", sym, st.Name, dur)
	}
}

// mapStatusToSymbol mirrors v1.3.4 binary's status icons.
func mapStatusToSymbol(status StepStatus) string {
	switch status {
	case StepPending:
		return "⋯"
	case StepRunning:
		return "⟳"
	case StepSucceeded:
		return "✓"
	case StepFailed:
		return "✗"
	case StepSkipped:
		return "–"
	default:
		return "?"
	}
}
