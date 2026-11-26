package tui

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/charmbracelet/x/ansi"
	taskregistry "github.com/justwasm/pigasus/internal/app/task"
)

func TestTasksRefreshCoalescesNotificationBurst(t *testing.T) {
	m := seedRootModel()
	seedTask(m.tasks.reg, "live", "synthetic running task", taskregistry.KindShell, taskregistry.StateRunning)
	first := m.ensureTasksTick()
	if first == nil {
		t.Fatal("initial timer missing")
	}
	for i := 0; i < 100; i++ {
		atomic.StoreInt32(&m.tasks.dirty, 1)
		_, cmd, _ := m.updateTickMsg(tasksChangedMsg{})
		if cmd != nil {
			t.Fatalf("notification %d added a timer", i)
		}
		if m.ensureTasksTick() != nil {
			t.Fatal("detail/open entry point duplicated timer")
		}
	}
	// Execute the real tea.Tick once: subsequent generations are driven
	// deterministically so the regression does not depend on wall time.
	old, ok := first().(tasksTickMsg)
	if !ok || old.generation != m.tasks.tickGeneration {
		t.Fatal("timer lost its generation")
	}
	_, next, _ := m.updateTickMsg(old)
	if next == nil || !m.tasks.tickArmed {
		t.Fatal("timer did not renew")
	}
	generation := m.tasks.tickGeneration
	_, dup, _ := m.updateTickMsg(old)
	if dup != nil || m.tasks.tickGeneration != generation {
		t.Fatal("stale/duplicate tick renewed chain")
	}
	m.tasks.reg.Update("live", func(task *taskregistry.Task) { task.State = taskregistry.StateDone })
	atomic.StoreInt32(&m.tasks.dirty, 1)
	_, done, _ := m.updateTickMsg(tasksTickMsg{generation: generation})
	if done != nil || m.tasks.tickArmed {
		t.Fatal("finished task kept timer alive")
	}
	seedTask(m.tasks.reg, "next", "next task", taskregistry.KindShell, taskregistry.StateRunning)
	_, restart, _ := m.updateTickMsg(tasksChangedMsg{})
	if restart == nil || !m.tasks.tickArmed {
		t.Fatal("new task did not wake stopped chain")
	}
}

func TestTasksRefreshCoalescesConcurrentWakeups(t *testing.T) {
	var model TasksModel
	var sent atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if model.queueWake() {
				sent.Add(1)
			}
		}()
	}
	wg.Wait()
	if sent.Load() != 1 {
		t.Fatalf("queued %d wakeups, want 1", sent.Load())
	}
	atomic.StoreInt32(&model.wakePending, 0)
	if !model.queueWake() {
		t.Fatal("new event could not wake after Update consumed pending")
	}
}

func TestTasksRefreshStaleTickPreservesRenderedFrame(t *testing.T) {
	m := warmModel(20, 100)
	m.viewport.rowsCacheDirty = false
	before := m.View()
	m.viewport.viewCacheValid = true
	_, cmd := m.Update(tasksTickMsg{generation: 99})
	if cmd != nil || !m.viewport.viewCacheValid || m.viewport.rowsCacheDirty {
		t.Fatal("stale tick invalidated caches or armed a timer")
	}
	after := m.View()
	if before.Content != after.Content {
		t.Fatal("stale tick changed rendered ANSI")
	}
}

func TestTasksRefreshUpdatesRenderedStatus(t *testing.T) {
	m := newBenchModel()
	m.viewport.width, m.viewport.height = 100, 40
	m.tasks.ctrl = nil
	m.tasks.reg = taskregistry.New()
	_ = m.View()
	seedTask(m.tasks.reg, "live", "synthetic running task", taskregistry.KindShell, taskregistry.StateRunning)
	atomic.StoreInt32(&m.tasks.dirty, 1)
	_, _ = m.Update(tasksChangedMsg{})
	if m.viewport.rowsCacheDirty {
		t.Fatal("task notification dirtied transcript before render")
	}
	view := m.View()
	if !strings.Contains(ansi.Strip(view.Content), "synthetic running task") && !strings.Contains(ansi.Strip(view.Content), "1 task") {
		t.Fatalf("new task missing from rendered frame: %q", view.Content)
	}
	if m.viewport.rowsCacheDirty {
		t.Fatal("task status change dirtied transcript wrapping")
	}
}

func BenchmarkTasksRefreshNotificationBurst(b *testing.B) {
	m := seedRootModel()
	seedTask(m.tasks.reg, "live", "task", taskregistry.KindShell, taskregistry.StateRunning)
	_ = m.ensureTasksTick()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, cmd, _ := m.updateTickMsg(tasksChangedMsg{})
		if cmd != nil {
			b.Fatal("duplicate timer")
		}
	}
}
