package task

import (
	"context"
	"errors"
	"time"
)

// ErrTaskNotResumable is returned when attempting to resume a task that does not support resumption
// or is not in a resumable state.
var ErrTaskNotResumable = errors.New("task is not resumable")

// TaskHandle encapsulates the runtime control surface for an active task or subagent actor.
type TaskHandle struct {
	ID         string
	Cancel     context.CancelFunc
	Abort      func()
	Resume     func(ctx context.Context) error
	OnActivity func()
}

// RegisterHandle attaches a control handle to the task with the given ID.
func (r *Registry) RegisterHandle(id string, h *TaskHandle) {
	if r == nil || id == "" || h == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.handles == nil {
		r.handles = make(map[string]*TaskHandle)
	}
	r.handles[id] = h
}

// GetHandle returns the control handle attached to the task, if any.
func (r *Registry) GetHandle(id string) *TaskHandle {
	if r == nil || id == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.handles[id]
}

// RecordActivity touches the LastActivityAt timestamp for the task and invokes its handle callback.
func (r *Registry) RecordActivity(id string) {
	if r == nil || id == "" {
		return
	}
	now := time.Now()
	var onAct func()
	r.mu.Lock()
	if t, ok := r.tasks[id]; ok && !t.hidden {
		t.LastActivityAt = now
	}
	if r.recording == nil {
		r.recording = make(map[string]bool)
	}
	if !r.recording[id] {
		r.recording[id] = true
		if h, ok := r.handles[id]; ok && h != nil {
			onAct = h.OnActivity
		}
	}
	r.mu.Unlock()

	if onAct != nil {
		defer func() {
			r.mu.Lock()
			delete(r.recording, id)
			r.mu.Unlock()
		}()
		onAct()
	}
}

// Kill terminates the task with the given ID using its registered handle.
func (r *Registry) Kill(id string) bool {
	if r == nil || id == "" {
		return false
	}
	r.mu.Lock()
	h := r.handles[id]
	t := r.tasks[id]
	r.mu.Unlock()

	if h != nil && h.Cancel != nil {
		h.Cancel()
	}
	if h != nil && h.Abort != nil {
		h.Abort()
	}

	if t != nil {
		r.Update(id, func(t *Task) {
			t.State = StateKilled
			t.EndedAt = time.Now()
			t.ExitCode = 137
		})
		return true
	}
	return false
}

// Abort triggers graceful abort of the active turn for the given task without killing the handle.
func (r *Registry) Abort(id string) bool {
	if r == nil || id == "" {
		return false
	}
	r.mu.Lock()
	h := r.handles[id]
	r.mu.Unlock()

	if h != nil && h.Abort != nil {
		h.Abort()
		return true
	}
	return false
}

// Resume attempts to resume a stopped, paused, or stalled task.
func (r *Registry) Resume(ctx context.Context, id string) error {
	if r == nil || id == "" {
		return ErrTaskNotResumable
	}
	r.mu.Lock()
	h := r.handles[id]
	t := r.tasks[id]
	r.mu.Unlock()

	if t == nil {
		return errors.New("task not found")
	}
	if h == nil || h.Resume == nil {
		return ErrTaskNotResumable
	}
	return h.Resume(ctx)
}
