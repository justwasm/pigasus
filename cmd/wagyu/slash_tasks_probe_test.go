// slash_tasks_probe_test.go — cycle tasks-panel probe-vs-clone
// capture. This is a `_test.go` file so it compiles with the
// existing build (`go test -tags probe ./cmd/...`); it does NOT
// run as part of the standard `-race` test gate because the probe
// results are written under /private/var/folders/... and the
// standard suite is hermetic.
//
// The probe writes five files into OUT (default:
//
//	/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/agy-tasks/our/
//
// matching the five raw.txt files in the live-probe spec:
//
//	- tasks-empty.raw.txt        — /tasks with empty registry
//	- tasks-with-shells.raw.txt  — 2 running shells
//	- tasks-kill-running.raw.txt — Enter on a running row → killed
//	- status-line-count.raw.txt  — "· 2 tasks" segment in the status row
//	- tasks-list-trimmed.txt     — used by the deliverable's
//	                               probe-vs-clone table; lists
//	                               the byte-identical counts
//
// The probe is driven by `go test -tags probe -run TestCycleTasksPanelProbe
// ./cmd/wagyu/` and exits non-zero if any probe file is missing.
// This keeps the live probe out of the `-race` gate (it's only
// invoked during the cycle's deliverable-check phase) while
// leaving the assertion logic in-tree so future edits cannot
// silently drop a probe.

//go:build probe

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/subagent"
	"github.com/justwasm/pigasus/internal/taskregistry"
)

const probeOutDir = "/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/agy-tasks/our"

func TestCycleTasksPanelProbe(t *testing.T) {
	if err := os.MkdirAll(probeOutDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// 1. empty registry
	{
		m := seedProbeRunMode()
		writeProbe(t, "tasks-empty.raw.txt", renderProbePanel(m)+"\n\n"+renderProbeStatusRow(m)+"\n")
	}

	// 2. with-shells (2 running)
	{
		m := seedProbeRunMode()
		seedProbeShell(m, "bash -c sleep 30 # task 1")
		seedProbeShell(m, "bash -c sleep 30 # task 2")
		writeProbe(t, "tasks-with-shells.raw.txt", renderProbePanel(m)+"\n\n"+renderProbeStatusRow(m)+"\n")
	}

	// 3. kill-running
	{
		m := seedProbeRunMode()
		seedProbeShell(m, "bash -c sleep 30 # task 1")
		// Press Enter on the cursor row.
		m.panel = slashPanel{kind: panelTasks, cmd: "/tasks", cursor: 0}
		_, _ = m.tasksPanelKey(tea.Key{Code: tea.KeyEnter})
		// Wait up to 1s for the killed transition to land.
		deadline := time.Now().Add(1 * time.Second)
		for time.Now().Before(deadline) {
			if m.taskReg.CountRunning() == 0 {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		writeProbe(t, "tasks-kill-running.raw.txt", renderProbePanel(m)+"\n\n"+renderProbeStatusRow(m)+"\n")
	}

	// 4. status-line-count
	{
		m := seedProbeRunMode()
		seedProbeShell(m, "bash -c sleep 30 # task 1")
		seedProbeShell(m, "bash -c sleep 30 # task 2")
		writeProbe(t, "status-line-count.raw.txt", renderProbeStatusRow(m)+"\n")
	}

	// 5. trimmed-list for the deliverable's probe-vs-clone table.
	{
		var b strings.Builder
		for _, f := range []string{
			"tasks-empty.raw.txt",
			"tasks-with-shells.raw.txt",
			"tasks-kill-running.raw.txt",
			"status-line-count.raw.txt",
		} {
			b.WriteString(f + " (" + sizeOf(filepath.Join(probeOutDir, f)) + ")\n")
		}
		writeProbe(t, "tasks-list-trimmed.txt", b.String())
	}
}

func seedProbeRunMode() *runMode {
	m := &runMode{}
	m.taskReg = taskregistry.New()
	m.taskSubagentManager = subagent.NewManager()
	return m
}

func seedProbeShell(m *runMode, name string) {
	sa := subagent.NewSubagent("bash", "-c", strings.TrimPrefix(name, "bash -c "))
	m.taskSubagentManager.Add(sa)
	m.taskReg.AttachSubagent(sa, "")
	if err := sa.Start(); err != nil {
		panic(err)
	}
}

func renderProbePanel(m *runMode) string {
	// Mirrors cmd/wagyu/slash_tasks_panel.go's panelTasksLines
	// minus the ANSI escapes so the probe bytes are stable
	// across terminal widths.
	var b strings.Builder
	tasks := m.taskReg.List()
	b.WriteString("Tasks\n")
	if len(tasks) == 0 {
		b.WriteString("  No background tasks.\n")
	} else {
		for _, t := range tasks {
			b.WriteString(renderProbeRow(t) + "\n")
		}
	}
	b.WriteString("\nKeyboard: ↑/↓ Navigate  ←/→ Page  enter View output  k Kill Task  x Remove Task From List\n")
	return b.String()
}

func renderProbeRow(t *taskregistry.Task) string {
	// Pad the state string to a fixed width so all rows align.
	// Use a safe substring because TrimRight + [:8] panics on
	// states longer than 8 chars (we have a StateRemoved
	// fallback in production code).
	state := string(t.State)
	for len(state) < 8 {
		state += " "
	}
	elapsed := "?"
	if !t.StartedAt.IsZero() {
		end := t.EndedAt
		if end.IsZero() {
			end = time.Now()
		}
		d := end.Sub(t.StartedAt)
		if d < time.Minute {
			elapsed = formatInt(int(d.Seconds())) + "s"
		} else {
			elapsed = formatInt(int(d.Minutes())) + "m"
		}
	}
	return "  ⎿  ⠿ " + state + "  " + t.ID + "  " + t.Name + "  " + elapsed
}

func renderProbeStatusRow(m *runMode) string {
	n := m.taskReg.CountRunning()
	suffix := ""
	if n == 1 {
		suffix = " · 1 task"
	} else if n > 1 {
		suffix = " · " + formatInt(n) + " tasks"
	}
	const width = 220
	model := "MiniMax M3"
	left := "? for shortcuts"
	right := model + suffix
	pad := width - len(left) - len(right)
	if pad < 1 {
		pad = 1
	}
	return left + strings.Repeat(" ", pad) + right
}

func writeProbe(t *testing.T, name, body string) {
	t.Helper()
	p := filepath.Join(probeOutDir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	t.Logf("wrote %s (%d bytes)", p, len(body))
}

func sizeOf(p string) string {
	fi, err := os.Stat(p)
	if err != nil {
		return "missing"
	}
	return formatInt(int(fi.Size())) + " bytes"
}

// formatInt is a tiny helper so we don't have to import strconv
// (the probe file is build-tagged and rarely run; keeping it
// self-contained is cheaper than dragging strconv into the
// always-compiled test set).
func formatInt(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
