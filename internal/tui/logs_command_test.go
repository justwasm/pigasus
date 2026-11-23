package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/justwasm/pigasus/internal/core/clilog"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/slashcommands"
	"github.com/justwasm/pigasus/internal/ui/views/logspanel"
)

func TestLogsSlashCommandAndRetryReasonRender(t *testing.T) {
	clilog.ResetForTest()
	t.Cleanup(clilog.ResetForTest)

	home := t.TempDir()
	t.Setenv("OPENAGY_CONFIG_HOME", home)
	t.Setenv(clilog.EnvLog, "")

	logPath := clilog.Init()
	if logPath == "" {
		t.Fatal("clilog.Init() returned empty path")
	}
	for i := 1; i <= 30; i++ {
		clilog.Logf("cloudcode", "entry-%02d model=claude-sonnet-4-6", i)
	}
	clilog.Logf("cloudcode", "open failed model=claude-sonnet-4-6 err=HTTP 403 permission denied")

	m := newRootModel(nil, "", "", "", "", false)
	m.viewport.width = 120
	m.viewport.height = 20

	// 1. Verify /logs is a public catalog command (with /log alias), maps to panelregistry.Logs, and dispatches cleanly.
	if !slashcommands.Known("/logs") || !slashcommands.Known("/log") {
		t.Fatal("expected /logs and /log to be public catalog commands")
	}
	if desc, ok := slashcommands.DescriptionOf("logs"); !ok || desc == "" {
		t.Fatalf("DescriptionOf(logs) = (%q, %v), want non-empty description", desc, ok)
	}
	if kind, ok := slashcommands.PanelFor("logs"); !ok || kind != panelregistry.Logs {
		t.Fatalf("PanelFor(logs) = (%v, %v), want (Logs, true)", kind, ok)
	}
	if kind, ok := slashcommands.PanelFor("log"); !ok || kind != panelregistry.Logs {
		t.Fatalf("PanelFor(log) = (%v, %v), want (Logs, true)", kind, ok)
	}
	if !m.dispatchesCommand("logs") || !m.dispatchesCommand("log") {
		t.Fatal("expected /logs and /log to be dispatched")
	}

	// 2. Dispatching /logs opens the full-viewport panel without polluting transcript.
	handled, cmd := m.dispatchSlashLine("/logs")
	if !handled {
		t.Fatal("dispatchSlashLine(/logs) = false, want true")
	}
	if cmd == nil {
		t.Fatal("expected live-refresh tick cmd when opening /logs")
	}
	if m.overlays.panel.kind != panelregistry.Logs {
		t.Fatalf("panel kind = %v, want Logs", m.overlays.panel.kind)
	}
	if !m.isFullViewportPanel() {
		t.Fatal("expected /logs to be a full-viewport panel")
	}
	if len(m.conversation.response) != 0 {
		t.Fatalf("expected /logs not to pollute conversation transcript, got %v", m.conversation.response)
	}

	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Pigasus Session Logs") {
		t.Errorf("rendered /logs view missing title:\n%s", view)
	}
	if !strings.Contains(view, "[LIVE FOLLOW]") {
		t.Errorf("rendered /logs view missing [LIVE FOLLOW]:\n%s", view)
	}
	if !strings.Contains(view, filepath.Base(logPath)) {
		t.Errorf("rendered /logs view missing log filename %q:\n%s", filepath.Base(logPath), view)
	}
	if !strings.Contains(view, "HTTP 403 permission denied") {
		t.Errorf("rendered /logs view missing latest log entry:\n%s", view)
	}

	// 3. Scrolling up pauses auto-follow; live tick keeps running without snapping away.
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyUp}); !handled {
		t.Fatal("expected Up key to be handled by /logs viewer")
	}
	if m.overlays.panel.logsFollow {
		t.Fatal("expected scrolling up to pause live follow")
	}
	pausedView := ansi.Strip(m.View().Content)
	if !strings.Contains(pausedView, "[PAUSED]") {
		t.Errorf("expected [PAUSED] badge after scrolling up:\n%s", pausedView)
	}

	// 4. Pressing 'f' resumes live follow and live tick picks up newly written lines.
	if handled, _ := m.handlePanelKey(tea.Key{Code: 'f', Text: "f"}); !handled {
		t.Fatal("expected 'f' key to toggle follow")
	}
	if !m.overlays.panel.logsFollow {
		t.Fatal("expected 'f' to re-enable live follow")
	}
	clilog.Logf("streampipe", "live-tail-marker-%d", 999)
	_, tickCmd := m.Update(logspanel.TickMsg{})
	if tickCmd == nil {
		t.Fatal("expected logspanel.TickMsg to reschedule tick")
	}
	liveView := ansi.Strip(m.View().Content)
	if !strings.Contains(liveView, fmt.Sprintf("live-tail-marker-%d", 999)) {
		t.Errorf("expected live tick view to show newly appended log line:\n%s", liveView)
	}

	// 5. Closing /logs with Esc leaves zero exit notice in conversation transcript.
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEsc}); !handled {
		t.Fatal("expected Esc to close /logs panel")
	}
	if m.overlays.panel.kind != panelregistry.None {
		t.Fatalf("expected panel closed after Esc, got %v", m.overlays.panel.kind)
	}
	if len(m.conversation.response) != 0 {
		t.Fatalf("expected closing /logs not to append exit notice, got %v", m.conversation.response)
	}

	// 6. Verify rendered TUI view surfaces retry reason when streaming retry is active.
	m.conversation.streaming = true
	m.conversation.SetModelRetryWithReason(2, 10, time.Now().Add(10*time.Second), "HTTP 503: MODEL_CAPACITY_EXHAUSTED")
	retryView := m.View().Content
	if !strings.Contains(retryView, "HTTP 503: MODEL_CAPACITY_EXHAUSTED") {
		t.Errorf("rendered TUI view missing retry reason:\n%s", retryView)
	}
	if !strings.Contains(retryView, "(/logs)") {
		t.Errorf("rendered TUI view missing (/logs) hint:\n%s", retryView)
	}
}
