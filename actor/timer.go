package actor

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type timerEntry struct {
	record   ScheduleRecord
	timer    *time.Timer
	cancel   context.CancelFunc
	cronExpr string
}

// scheduler manages time-based triggers and cron routines for an Actor.
type scheduler struct {
	mu        sync.Mutex
	entries   map[string]*timerEntry
	onWakeup  func(msg *ScheduledWakeup)
	onFired   func(rec ScheduleRecord)
	ctx       context.Context
	cancel    context.CancelFunc
	closed    bool
	timeNowFn func() time.Time
}

func newScheduler(onWakeup func(msg *ScheduledWakeup), onFired func(rec ScheduleRecord), timeNow func() time.Time) *scheduler {
	if timeNow == nil {
		timeNow = time.Now
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &scheduler{
		entries:   make(map[string]*timerEntry),
		onWakeup:  onWakeup,
		onFired:   onFired,
		ctx:       ctx,
		cancel:    cancel,
		timeNowFn: timeNow,
	}
}

// ScheduleDelay adds a one-shot timer that fires after the specified duration.
func (s *scheduler) ScheduleDelay(duration time.Duration, prompt string) (ScheduleRecord, error) {
	if duration <= 0 {
		return ScheduleRecord{}, fmt.Errorf("%w: duration must be positive", ErrInvalidSchedule)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ScheduleRecord{}, ErrActorClosed
	}

	now := s.timeNowFn()
	id := newID("sch_tim")
	nextRun := now.Add(duration)

	rec := ScheduleRecord{
		ID:        id,
		Prompt:    prompt,
		Duration:  duration.String(),
		CreatedAt: now,
		NextRun:   nextRun,
		IsDaemon:  false,
	}

	_, cancel := context.WithCancel(s.ctx)
	entry := &timerEntry{
		record: rec,
		cancel: cancel,
	}

	entry.timer = time.AfterFunc(duration, func() {
		s.handleFired(id, "timer")
	})

	s.entries[id] = entry
	return rec, nil
}

// ScheduleCron registers a recurring or single-fire cron schedule.
func (s *scheduler) ScheduleCron(cronExpr, prompt string, isDaemon bool) (ScheduleRecord, error) {
	now := s.timeNowFn()
	nextRun, err := NextCronTime(cronExpr, now)
	if err != nil {
		return ScheduleRecord{}, fmt.Errorf("%w: invalid cron: %v", ErrInvalidSchedule, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ScheduleRecord{}, ErrActorClosed
	}

	id := newID("sch_crn")
	rec := ScheduleRecord{
		ID:        id,
		Prompt:    prompt,
		CronExpr:  cronExpr,
		CreatedAt: now,
		NextRun:   nextRun,
		IsDaemon:  isDaemon,
	}

	_, cancel := context.WithCancel(s.ctx)
	entry := &timerEntry{
		record:   rec,
		cancel:   cancel,
		cronExpr: cronExpr,
	}

	dur := nextRun.Sub(now)
	if dur <= 0 {
		dur = time.Millisecond
	}

	entry.timer = time.AfterFunc(dur, func() {
		s.handleFired(id, "cron")
	})

	s.entries[id] = entry
	return rec, nil
}

func (s *scheduler) handleFired(id, source string) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}

	entry, ok := s.entries[id]
	if !ok {
		s.mu.Unlock()
		return
	}

	rec := entry.record
	cronExpr := entry.cronExpr
	isDaemon := rec.IsDaemon

	if cronExpr != "" && isDaemon {
		// Calculate next recurring run
		now := s.timeNowFn()
		nextRun, err := NextCronTime(cronExpr, now)
		if err == nil {
			entry.record.NextRun = nextRun
			dur := nextRun.Sub(now)
			if dur <= 0 {
				dur = time.Millisecond
			}
			entry.timer = time.AfterFunc(dur, func() {
				s.handleFired(id, "cron")
			})
		} else {
			delete(s.entries, id)
		}
	} else {
		// One-shot, remove from registry
		delete(s.entries, id)
	}
	s.mu.Unlock()

	// Notify fired callback
	if s.onFired != nil {
		s.onFired(rec)
	}

	// Dispatch wakeup message to mailbox
	if s.onWakeup != nil {
		msg := NewScheduledWakeup(rec.ID, rec.Prompt, source)
		s.onWakeup(msg)
	}
}

// Cancel removes and stops a scheduled timer or cron by ID.
func (s *scheduler) Cancel(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.entries[id]
	if !ok {
		return fmt.Errorf("schedule not found: %s", id)
	}

	if entry.timer != nil {
		entry.timer.Stop()
	}
	if entry.cancel != nil {
		entry.cancel()
	}

	delete(s.entries, id)
	return nil
}

// ActiveSchedules returns currently pending schedule records.
func (s *scheduler) ActiveSchedules() []ScheduleRecord {
	s.mu.Lock()
	defer s.mu.Unlock()

	list := make([]ScheduleRecord, 0, len(s.entries))
	for _, e := range s.entries {
		list = append(list, e.record)
	}
	return list
}

// Close stops all active timers.
func (s *scheduler) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}
	s.closed = true
	s.cancel()

	for id, e := range s.entries {
		if e.timer != nil {
			e.timer.Stop()
		}
		if e.cancel != nil {
			e.cancel()
		}
		delete(s.entries, id)
	}
}
