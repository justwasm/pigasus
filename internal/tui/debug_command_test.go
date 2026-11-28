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
	"github.com/justwasm/pigasus/internal/ui/views/debugpanel"
)

func TestDebugSlashCommandAndRetryReasonRender(t *testing.T) {
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

	// 1. Verify /debug is the command's name, that /logs and /log are the
	// aliases it kept, and that all three land on the same panel.
	for _, name := range []string{"/debug", "/logs", "/log"} {
		if !slashcommands.Known(name) {
			t.Errorf("%s is not a public catalog command", name)
		}
		if desc, ok := slashcommands.DescriptionOf(name); !ok || desc == "" {
			t.Errorf("DescriptionOf(%s) = (%q, %v), want non-empty description", name, desc, ok)
		}
		if kind, ok := slashcommands.PanelFor(name); !ok || kind != panelregistry.Debug {
			t.Errorf("PanelFor(%s) = (%v, %v), want (Debug, true)", name, kind, ok)
		}
		// "Dispatched" is asked of the canonical name, because that is
		// what Dispatch looks a handler up by: an alias never has a
		// handler of its own.
		canonical, _ := slashcommands.Canonical(name)
		if !m.dispatchesCommand(canonical) {
			t.Errorf("%s resolves to %q, which is not dispatched", name, canonical)
		}
	}
	// The alias rule: both old names resolve to the new one, so a surface
	// that keys off the canonical name cannot tell them apart.
	for _, alias := range []string{"logs", "log"} {
		if got, ok := slashcommands.Canonical(alias); !ok || got != "debug" {
			t.Errorf("Canonical(%q) = (%q, %v), want (debug, true)", alias, got, ok)
		}
	}

	// 2. Dispatching /debug opens the full-viewport panel without polluting transcript.
	handled, cmd := m.dispatchSlashLine("/debug")
	if !handled {
		t.Fatal("dispatchSlashLine(/debug) = false, want true")
	}
	if cmd == nil {
		t.Fatal("expected live-refresh tick cmd when opening /debug")
	}
	if m.overlays.panel.kind != panelregistry.Debug {
		t.Fatalf("panel kind = %v, want Debug", m.overlays.panel.kind)
	}
	if !m.isFullViewportPanel() {
		t.Fatal("expected /debug to be a full-viewport panel")
	}
	if len(m.conversation.response) != 0 {
		t.Fatalf("expected /debug not to pollute conversation transcript, got %v", m.conversation.response)
	}

	// The panel lands on its Summary view; the log file is one keypress
	// away, so the test presses it rather than asserting where it starts.
	m.handlePanelKey(tea.Key{Code: '2', Text: "2"})
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Pigasus Session Logs") {
		t.Errorf("rendered /debug view missing title:\n%s", view)
	}
	if !strings.Contains(view, "[LIVE FOLLOW]") {
		t.Errorf("rendered /debug view missing [LIVE FOLLOW]:\n%s", view)
	}
	if !strings.Contains(view, filepath.Base(logPath)) {
		t.Errorf("rendered /debug view missing log filename %q:\n%s", filepath.Base(logPath), view)
	}
	if !strings.Contains(view, "HTTP 403 permission denied") {
		t.Errorf("rendered /debug view missing latest log entry:\n%s", view)
	}

	// 3. Scrolling up pauses auto-follow; live tick keeps running without snapping away.
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyUp}); !handled {
		t.Fatal("expected Up key to be handled by /debug viewer")
	}
	if m.overlays.panel.debugFollow {
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
	if !m.overlays.panel.debugFollow {
		t.Fatal("expected 'f' to re-enable live follow")
	}
	clilog.Logf("streampipe", "live-tail-marker-%d", 999)
	_, tickCmd := m.Update(debugpanel.TickMsg{})
	if tickCmd == nil {
		t.Fatal("expected debugpanel.TickMsg to reschedule tick")
	}
	liveView := ansi.Strip(m.View().Content)
	if !strings.Contains(liveView, fmt.Sprintf("live-tail-marker-%d", 999)) {
		t.Errorf("expected live tick view to show newly appended log line:\n%s", liveView)
	}

	// 5. Closing /debug with Esc leaves zero exit notice in conversation transcript.
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEsc}); !handled {
		t.Fatal("expected Esc to close /debug panel")
	}
	if m.overlays.panel.kind != panelregistry.None {
		t.Fatalf("expected panel closed after Esc, got %v", m.overlays.panel.kind)
	}
	if len(m.conversation.response) != 0 {
		t.Fatalf("expected closing /debug not to append exit notice, got %v", m.conversation.response)
	}

	// 6. Verify rendered TUI view surfaces retry reason when streaming retry is active.
	m.conversation.streaming = true
	m.conversation.SetModelRetryWithReason(2, 10, time.Now().Add(10*time.Second), "HTTP 503: MODEL_CAPACITY_EXHAUSTED")
	retryView := m.View().Content
	if !strings.Contains(retryView, "HTTP 503: MODEL_CAPACITY_EXHAUSTED") {
		t.Errorf("rendered TUI view missing retry reason:\n%s", retryView)
	}
	if !strings.Contains(retryView, "(/debug)") {
		t.Errorf("rendered TUI view missing (/debug) hint:\n%s", retryView)
	}
}

// TestTheOldLogsNameStillOpensTheSurface — /logs is an alias, not a second
// command, so typing it must reach the same panel /debug does.
//
// The rename is only safe if the old name keeps working: it is in every
// document in the tree and in every user's fingers, and a rename that broke
// it would be a rename that took a working surface away.
func TestTheOldLogsNameStillOpensTheSurface(t *testing.T) {
	clilog.ResetForTest()
	t.Cleanup(clilog.ResetForTest)
	home := t.TempDir()
	t.Setenv("OPENAGY_CONFIG_HOME", home)
	t.Setenv(clilog.EnvLog, "")
	if clilog.Init() == "" {
		t.Fatal("clilog.Init() returned empty path")
	}
	clilog.Logf("cloudcode", "an entry")

	for _, name := range []string{"/debug", "/logs", "/log"} {
		m := newRootModel(nil, "", "", "", "", false)
		m.viewport.width, m.viewport.height = 120, 20

		handled, cmd := m.dispatchSlashLine(name)
		if !handled {
			t.Fatalf("%s was not handled", name)
		}
		if cmd == nil {
			t.Errorf("%s did not arm the live-refresh tick", name)
		}
		if m.overlays.panel.kind != panelregistry.Debug {
			t.Fatalf("%s opened panel %v, want Debug", name, m.overlays.panel.kind)
		}
		// The three names are one command, so they land on one view: the
		// panel's default. An alias that opened a different view would not
		// be an alias.
		if got, want := m.debugPanel().View(), debugpanel.New(debugpanel.Seams{}).View(); got != want {
			t.Errorf("%s opened on view %v, want the default %v", name, got, want)
		}
	}
}

// TestTheViewArgumentWorksUnderBothNames — `/logs wire` and `/debug wire`
// name a view, and the argument survives the alias.
func TestTheViewArgumentWorksUnderBothNames(t *testing.T) {
	clilog.ResetForTest()
	t.Cleanup(clilog.ResetForTest)
	home := t.TempDir()
	t.Setenv("OPENAGY_CONFIG_HOME", home)
	t.Setenv(clilog.EnvLog, "")
	clilog.Init()

	for _, line := range []string{"/debug wire", "/logs wire", "/debug ctx", "/logs context"} {
		m := newRootModel(nil, "", "", "", "", false)
		m.viewport.width, m.viewport.height = 120, 20
		if !m.openSlashPanelArgs(strings.Fields(line)[0][1:], strings.Fields(line)[1]) {
			t.Fatalf("%s did not open a panel", line)
		}
		want := debugpanel.ViewWire
		if strings.Contains(line, "ctx") || strings.Contains(line, "context") {
			want = debugpanel.ViewContext
		}
		if got := m.debugPanel().View(); got != want {
			t.Errorf("%s landed on %v, want %v", line, got, want)
		}
	}
}
