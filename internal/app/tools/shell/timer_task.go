package shell

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	"github.com/justwasm/pigasus/internal/app/tools"
)

// ScheduledTaskOptions configures a scheduled timer or cron background task.
type ScheduledTaskOptions struct {
	Home           string
	ConvID         string
	TaskID         string
	LogPath        string
	Prompt         string
	Duration       time.Duration
	CronExpr       string
	TimerCondition string
	IsDaemon       bool
	MaxIterations  int
	ExactTiming    bool
	CreatedAt      time.Time
	TargetTime     time.Time
}

type activeTimerRecord struct {
	id             string
	timerCondition string
	task           *BackgroundTask
	cancel         context.CancelFunc
}

var (
	schedTimersMu sync.Mutex
	schedTimers   = make(map[string]*activeTimerRecord)
)

// StartScheduledTask registers and arms a scheduled timer or cron job as an active BackgroundTask.
func StartScheduledTask(opts ScheduledTaskOptions) (*BackgroundTask, error) {
	fullID := opts.ConvID + "/" + opts.TaskID

	var cmdName string
	if opts.Duration > 0 {
		cmdName = fmt.Sprintf("timer (%v): %s", opts.Duration, opts.Prompt)
	} else if opts.CronExpr != "" {
		cmdName = fmt.Sprintf("cron (%s): %s", opts.CronExpr, opts.Prompt)
	} else {
		return nil, fmt.Errorf("scheduled task must have duration or cron expression")
	}

	if opts.CreatedAt.IsZero() {
		opts.CreatedAt = time.Now()
	}
	if opts.TargetTime.IsZero() && opts.Duration > 0 {
		opts.TargetTime = opts.CreatedAt.Add(opts.Duration)
	}

	ctx, cancel := context.WithCancel(context.Background())

	t := &BackgroundTask{
		id:       fullID,
		command:  cmdName,
		started:  opts.CreatedAt,
		logPath:  opts.LogPath,
		cancel:   cancel,
		finished: make(chan struct{}),
	}

	t.killFn = func() error {
		cancel()
		schedTimersMu.Lock()
		delete(schedTimers, fullID)
		schedTimersMu.Unlock()
		_ = RemoveActiveSchedule(opts.Home, opts.ConvID, opts.TaskID)
		return nil
	}

	if !backgroundTasks.register(t) {
		cancel()
		return nil, errDuplicateTask
	}

	record := &activeTimerRecord{
		id:             fullID,
		timerCondition: opts.TimerCondition,
		task:           t,
		cancel:         cancel,
	}

	schedTimersMu.Lock()
	schedTimers[fullID] = record
	schedTimersMu.Unlock()

	if reg := tools.SharedTaskRegistry(); reg != nil {
		reg.Register(&taskregistry.Task{
			ID:         fullID,
			Name:       cmdName,
			Kind:       taskregistry.KindTimer,
			State:      taskregistry.StateRunning,
			StartedAt:  opts.CreatedAt,
			TargetTime: opts.TargetTime,
			Command:    cmdName,
		})
		reg.RegisterHandle(fullID, &taskregistry.TaskHandle{
			Abort: func() {
				_, _ = t.Kill()
			},
			Cancel: func() {
				_, _ = t.Kill()
			},
		})
	}

	if opts.Duration > 0 {
		go runOneShotTimer(ctx, t, record, opts)
	} else {
		go runRecurringCron(ctx, t, record, opts)
	}

	return t, nil
}

func runOneShotTimer(ctx context.Context, t *BackgroundTask, record *activeTimerRecord, opts ScheduledTaskOptions) {
	defer func() {
		schedTimersMu.Lock()
		delete(schedTimers, t.id)
		schedTimersMu.Unlock()
		_ = RemoveActiveSchedule(opts.Home, opts.ConvID, opts.TaskID)
	}()

	timer := time.NewTimer(opts.Duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		t.mu.Lock()
		t.done = true
		t.completed = true
		t.killed = true
		t.mu.Unlock()
		select {
		case <-t.finished:
		default:
			close(t.finished)
		}
		return

	case <-timer.C:
		t.mu.Lock()
		t.done = true
		t.completed = true
		t.mu.Unlock()
		select {
		case <-t.finished:
		default:
			close(t.finished)
		}

		_ = appendTaskLogFile(opts.LogPath, fmt.Sprintf("Timer fired at %s\n", time.Now().Format(time.RFC3339)))

		// Deliver high-priority task notification to wake up the agent
		NotifyTask(TaskNotification{
			TaskID:   t.id,
			Priority: TaskPriorityHigh,
			Body:     opts.Prompt,
			LogPath:  opts.LogPath,
			At:       time.Now(),
		})
	}
}

func runRecurringCron(ctx context.Context, t *BackgroundTask, record *activeTimerRecord, opts ScheduledTaskOptions) {
	defer func() {
		schedTimersMu.Lock()
		delete(schedTimers, t.id)
		schedTimersMu.Unlock()
	}()

	iterations := 0
	for {
		now := time.Now()
		next, err := NextCronTime(opts.CronExpr, now)
		if err != nil {
			t.mu.Lock()
			t.err = err
			t.done = true
			t.completed = true
			t.mu.Unlock()
			select {
			case <-t.finished:
			default:
				close(t.finished)
			}
			return
		}

		delay := next.Sub(now)
		if delay < 0 {
			delay = 0
		}
		timer := time.NewTimer(delay)

		select {
		case <-ctx.Done():
			timer.Stop()
			t.mu.Lock()
			t.done = true
			t.completed = true
			t.killed = true
			t.mu.Unlock()
			select {
			case <-t.finished:
			default:
				close(t.finished)
			}
			return

		case <-timer.C:
			iterations++
			NotifyTask(TaskNotification{
				TaskID:   t.id,
				Priority: TaskPriorityHigh,
				Body:     opts.Prompt,
				LogPath:  opts.LogPath,
				At:       time.Now(),
			})

			_ = appendTaskLogFile(opts.LogPath, fmt.Sprintf("Cron iteration %d fired at %s\n", iterations, time.Now().Format(time.RFC3339)))

			if opts.MaxIterations > 0 && iterations >= opts.MaxIterations {
				t.mu.Lock()
				t.done = true
				t.completed = true
				t.mu.Unlock()
				select {
				case <-t.finished:
				default:
					close(t.finished)
				}
				return
			}
		}
	}
}

// checkTimerEarlyCancellation checks if any active timer's condition matches an incoming notification.
func checkTimerEarlyCancellation(n TaskNotification) {
	schedTimersMu.Lock()
	var toCancel []*activeTimerRecord
	for _, rec := range schedTimers {
		cond := strings.TrimSpace(rec.timerCondition)
		if cond == "" || cond == "never" {
			continue
		}
		// "any": cancels on any message from any sender
		if cond == "any" {
			toCancel = append(toCancel, rec)
			continue
		}
		// <sender-id>: cancels if notification came from that sender ID
		if cond == n.TaskID || strings.HasSuffix(n.TaskID, "/"+cond) {
			toCancel = append(toCancel, rec)
		}
	}
	schedTimersMu.Unlock()

	for _, rec := range toCancel {
		_, _ = rec.task.Kill()
	}
}

func appendTaskLogFile(logPath, text string) error {
	if logPath == "" {
		return nil
	}
	_ = os.MkdirAll(filepath.Dir(logPath), 0o755)
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(text)
	return err
}
