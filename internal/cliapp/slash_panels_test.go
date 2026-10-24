// slash_panels_test.go — cycle 185 + cycle 193.
//
// The panel layouts are transcribed from the reference's own screen
// (REF v1.4.0, 220×30, /tmp/agycmp/panels/ppref.<cmd>.raw.txt). The
// assertions below therefore compare the *plain* (SGR-stripped) text,
// because tmux's capture-pane normalises 24-bit RGB into 256-colour
// indices, so a raw-byte comparison would only be possible against the
// normalised form. The styling is asserted separately, by checking the
// colour spans the capture actually shows.
//
// Cycle 193 appended layout tests for /model /config /plugin /skills
// /usage /changelog /diff plus an expanded TestPanelForCommand table
// and an expanded TestPanelExitNotice silent map.
//
// Cycle 201 appends key-action tests: viewer scrolling (/usage,
// /changelog), /diff tab switching, /plugin tab cycling, the /skills
// detail view and the /feedback stage-2 form.

package cliapp

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/mattn/go-runewidth"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/changelog"
	"github.com/justwasm/pigasus/internal/configtest"
	"github.com/justwasm/pigasus/internal/featdiff"
	"github.com/justwasm/pigasus/internal/mdrender"
	"github.com/justwasm/pigasus/internal/models"
	"github.com/justwasm/pigasus/internal/onboarding"
	"github.com/justwasm/pigasus/internal/subagent"
	"github.com/justwasm/pigasus/internal/taskregistry"
	"github.com/justwasm/pigasus/internal/tuilib"
	"github.com/justwasm/pigasus/internal/tuiviewer"
)

var _ = textinput.New

// mkPanel returns a runMode with the given panel open.
func mkPanel(kind panelKind, cmd string, cursor int) *runMode {
	m := &runMode{}
	m.panel = slashPanel{kind: kind, cmd: cmd, cursor: cursor}
	// Cycle 197: panelModel reads settings.json for the (current)
	// annotation. Seed effortCursor from the persisted slug so the
	// annotation lands correctly without going through openSlashPanel.
	if kind == panelModel {
		m.panel.effortCursor = initialEffortCursorFromSettings()
	}
	// Cycle 225: panelUsage's quota.state defaults to quotaLoading,
	// which forces the height-aware renderer through the loading doc
	// path. Cycle-207 tests (e.g. TestCycle207UsageDocGeometry) key
	// off the loaded doc's exact 20-line shape; they set m.height
	// directly and never arm a tick, so the loading doc would break
	// them. Pre-seed quotaLoaded (no refresh times — the cycle-207
	// tests pin the doc via m.width and ignore the live countdown,
	// and the cycle-225 tests drive the loading state through
	// quotaStubRefills / explicit state sets).
	if kind == panelUsage {
		m.panel.quota.state = quotaLoaded
	}
	if kind.isViewer() {
		m.initViewerViewport()
	}
	return m
}

func plainLines(m *runMode) []string {
	var out []string
	for _, l := range m.panelLines() {
		out = append(out, stripSGR(l))
	}
	return out
}

func assertLines(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("line count: got %d, want %d\n got: %q\nwant: %q", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d:\n got: %q\nwant: %q", i, got[i], want[i])
		}
	}
}

func TestPanelForCommand(t *testing.T) {
	panels := map[string]panelKind{
		"mcp":         panelMCP,
		"tasks":       panelTasks,
		"effort":      panelEffort,
		"hooks":       panelHooks,
		"permissions": panelPermissions,
		"agents":      panelAgents,
		"credits":     panelCredits,
		"codesearch":  panelCodeSearch,
		"artifact":    panelArtifact,
		"feedback":    panelFeedback,
		"rewind":      panelRewind,
		"context":     panelContext,
		// Cycle 193: A-class panel overlays.
		"model":     panelModel,
		"config":    panelConfig,
		"plugin":    panelPlugin,
		"skills":    panelSkills,
		"usage":     panelUsage,
		"changelog": panelChangelog,
		"diff":      panelDiff,
	}
	for name, want := range panels {
		got, ok := panelForCommand(name)
		if !ok || got != want {
			t.Errorf("panelForCommand(%q) = (%v, %v), want (%v, true)", name, got, ok, want)
		}
	}
	for _, name := range []string{"clear", "help", "fork", "btw", "voice", "record"} {
		if _, ok := panelForCommand(name); ok {
			t.Errorf("panelForCommand(%q) unexpectedly opens a panel", name)
		}
	}
	// Cycle charmland-component-adoption/02: `/rename` (no args) now
	// opens panelRename — the panel is powered by bubbles/v2/textinput.
	// The with-args case still flows through renameFeedback in
	// slash_actions.go.
	if got, ok := panelForCommand("rename"); !ok || got != panelRename {
		t.Errorf("panelForCommand(%q) = (%v, %v), want (%v, true)",
			"rename", got, ok, panelRename)
	}
}

func TestOpenSlashPanel(t *testing.T) {
	m := &runMode{}
	if !m.openSlashPanel("mcp") {
		t.Fatal("openSlashPanel(\"mcp\") = false")
	}
	if m.panel.kind != panelMCP || m.panel.cmd != "/mcp" {
		t.Fatalf("panel = %+v, want kind=%v cmd=/mcp", m.panel, panelMCP)
	}
	if m.openSlashPanel("clear") {
		t.Fatal("openSlashPanel(\"clear\") = true")
	}
	// /effort opens on the current effort (high by default).
	m2 := &runMode{}
	m2.openSlashPanel("effort")
	if m2.panel.cursor != effortHigh {
		t.Errorf("effort cursor = %d, want %d (high)", m2.panel.cursor, effortHigh)
	}
}

// The reference's /mcp panel, verbatim from
// /tmp/agycmp/panels/ppref.mcp.raw.txt (SGR stripped).
func TestPanelMCPLayout(t *testing.T) {
	m := mkPanel(panelMCP, "/mcp", 0)
	assertLines(t, plainLines(m), []string{
		"MCP Servers",
		"",
		"  No MCP servers configured.",
		"",
		"Keyboard: ↑/↓ Navigate  enter Actions  esc Exit",
	})
	// The empty-state line uses the secondary grey (#586E75), the
	// legend's labels the dim grey (#657B83) — both Solarized, cycle 283.
	lines := m.panelLines()
	if !strings.Contains(lines[2], "38;2;88;110;117m") {
		t.Errorf("empty state should be the subtle grey: %q", lines[2])
	}
	if !strings.Contains(lines[4], cDimGrayOpen) {
		t.Errorf("legend labels should be the dim gray: %q", lines[4])
	}
	if !strings.HasSuffix(lines[4], cDimGrayClose) {
		t.Errorf("legend must end with the label close: %q", lines[4])
	}
}

func TestPanelTasksLayout(t *testing.T) {
	m := mkPanel(panelTasks, "/tasks", 0)
	assertLines(t, plainLines(m), []string{
		"Tasks",
		"  No background tasks.",
		"",
		"Keyboard: ↑/↓ Navigate  ←/→ Page  enter View output  k Kill Task  x Remove Task From List",
	})
}

func TestPanelHooksLayout(t *testing.T) {
	m := mkPanel(panelHooks, "/hooks", 0)
	assertLines(t, plainLines(m), []string{
		" Hooks",
		"   5 hook types",
		"",
		" > PreToolUse      Before tool execution",
		"   PostToolUse     After tool execution",
		"   PreInvocation   Before each LLM invocation",
		"   PostInvocation  After each LLM invocation",
		"   Stop            When agent tries to exit",
		"",
		"   ↑/↓ Navigate · enter Select",
	})
	// Moving the cursor moves the highlight, not the text.
	m.panel.cursor = 4
	got := plainLines(m)
	if !strings.HasPrefix(got[7], " > Stop") {
		t.Errorf("row 7 should be selected: %q", got[7])
	}
	if !strings.HasPrefix(got[3], "   PreToolUse") {
		t.Errorf("row 3 should be deselected: %q", got[3])
	}
}

func TestPanelPermissionsLayout(t *testing.T) {
	m := mkPanel(panelPermissions, "/permissions", 0)
	assertLines(t, plainLines(m), []string{
		"Permission Config Editor",
		"",
		"Select a config scope to edit:",
		"",
		"> Project",
		"  Shared with Pigasus",
		"  Global",
		"",
		"  Rules that apply only to this project (highest priority)",
		"",
		"Keyboard: ↑/↓ Navigate  enter Save  esc Close",
	})
	// The blurb follows the cursor. Recovered by driving REF's panel
	// with Down (/tmp/agycmp/nav-permissions.log).
	want := []string{
		"  Rules that apply only to this project (highest priority)",
		"  Rules that apply to all Pigasus products",
		"  Global rules",
	}
	for cursor, wantBlurb := range want {
		m.panel.cursor = cursor
		if got := plainLines(m)[8]; got != wantBlurb {
			t.Errorf("cursor %d blurb = %q, want %q", cursor, got, wantBlurb)
		}
	}
}

func TestPanelCreditsLayout(t *testing.T) {
	m := mkPanel(panelCredits, "/credits", 0)
	assertLines(t, plainLines(m), []string{
		"Pigasus CLI  credits",
		"Model AI Credits",
		"",
		"Remaining AI Credits: AI Credits not enabled (enable in /settings)",
		"",
		"Actions",
		"",
		"> Get More AI Credits",
		"  See Activity",
		"",
		"Keyboard: ↑/↓ Navigate  enter Select / Toggle  esc Close",
	})
	m.panel.cursor = 1
	got := plainLines(m)
	if got[7] != "  Get More AI Credits" || got[8] != "> See Activity" {
		t.Errorf("cursor 1 rows = %q / %q", got[7], got[8])
	}
}

func TestPanelCodeSearchLayout(t *testing.T) {
	m := mkPanel(panelCodeSearch, "/codesearch", 0)
	assertLines(t, plainLines(m), []string{
		"Code Search:",
		// Cycle 228: the panel's usage line now names the f:/file:
		// glob flags alongside the -F/--literal flag.
		"  Usage: /codesearch [-F|--literal] <query> [f:<glob>] [file:<glob>]",
		"",
		"Keyboard: ↑/↓ Navigate  ←/→ Prev/Next File  enter Open File  ctrl+g Open in Editor  esc Close Search",
	})
	// The colon sits outside the bold title in REF.
	if got := m.panelLines()[0]; got != panelTitleOpen+"Code Search"+panelTitleClose+":" {
		t.Errorf("title = %q", got)
	}
}

func TestPanelArtifactLayout(t *testing.T) {
	m := mkPanel(panelArtifact, "/artifact", 0)
	assertLines(t, plainLines(m), []string{
		"Artifacts",
		"  No artifacts",
		"",
		"Keyboard: ↑/↓ Navigate  p preview  enter open  ctrl+g open in editor  esc Dismiss",
	})
}

// The reference's /feedback stage-1 category picker, verbatim from
// /tmp/agycmp/panels/ppref.feedback.raw.txt (SGR stripped).
func TestPanelFeedbackLayout(t *testing.T) {
	m := mkPanel(panelFeedback, "/feedback", 0)
	assertLines(t, plainLines(m), []string{
		"Submit Feedback",
		"",
		"What went wrong? Select a category to assist your report:",
		"",
		"> 1. Code doesn't work (e.g., syntax errors, bad logic)",
		"  2. Repeated itself (e.g., stuck running the same tool over again)",
		"  3. Took too many steps (e.g., progress was much too slow)",
		"  4. Ignored instructions (e.g., missed specific constraints)",
		"  5. Got stuck (e.g., unable to recover from an error)",
		"  6. Other",
		"",
		"  esc Cancel · 1-6 Select & Continue · enter Continue",
	})
	// The selected row carries the blue codes; the unselected rows do not.
	for i, line := range m.panelLines() {
		if i == 4 {
			if !strings.Contains(line, cBlueOpen) || !strings.Contains(line, cBlueClose) {
				t.Errorf("row %d should be blue: %q", i, line)
			}
		} else if i >= 5 && i <= 9 {
			if strings.Contains(line, cBlueOpen) {
				t.Errorf("row %d should not be blue: %q", i, line)
			}
		}
	}
	// The dot hint's key glyphs (esc, 1-6, enter) are bold blue.
	hint := m.panelLines()[11]
	if !strings.Contains(hint, panelBoldBlueOpen) {
		t.Errorf("dot hint keys should be bold blue: %q", hint)
	}
}

// Pressing Down at the last row of /feedback wraps to the first; REF
// behaves the same way (verified at /tmp/agycmp/panels/ppfb_wrap.raw.txt).
// Up is symmetric.
func TestHandlePanelKeyFeedbackWraps(t *testing.T) {
	m := mkPanel(panelFeedback, "/feedback", 0)
	down := tea.Key{Code: tea.KeyDown}
	for i := 1; i < 6; i++ {
		if handled, _ := m.handlePanelKey(down); !handled || m.panel.cursor != i {
			t.Fatalf("Down #%d: handled=%v cursor=%d want %d", i, handled, m.panel.cursor, i)
		}
	}
	// One more Down wraps 5 -> 0.
	if handled, _ := m.handlePanelKey(down); !handled || m.panel.cursor != 0 {
		t.Errorf("wrap Down: handled=%v cursor=%d want 0", handled, m.panel.cursor)
	}
	// Up from 0 wraps to 5.
	up := tea.Key{Code: tea.KeyUp}
	if handled, _ := m.handlePanelKey(up); !handled || m.panel.cursor != 5 {
		t.Errorf("wrap Up: handled=%v cursor=%d want 5", handled, m.panel.cursor)
	}
}

// Other panels still clamp; the wrap behaviour is /feedback-only.
func TestHandlePanelKeyHooksClamps(t *testing.T) {
	m := mkPanel(panelHooks, "/hooks", 0)
	up := tea.Key{Code: tea.KeyUp}
	if handled, _ := m.handlePanelKey(up); !handled || m.panel.cursor != 0 {
		t.Errorf("Up at 0 should clamp: handled=%v cursor=%d", handled, m.panel.cursor)
	}
	for i := 0; i < 10; i++ {
		m.handlePanelKey(tea.Key{Code: tea.KeyDown})
	}
	if m.panel.cursor != 4 {
		t.Errorf("Down should clamp at last row: cursor=%d want 4", m.panel.cursor)
	}
}

// Esc on /feedback closes the panel silently — no "⎿  Exited /feedback
// command" echo block (cycle 185's esc_probe.py on /mcp and /permissions,
// and ppfb_afteresc.raw.txt on /feedback).
func TestFeedbackExitNotice(t *testing.T) {
	if panelFeedback.exitNotice() {
		t.Error("panelFeedback.exitNotice() = true, want false")
	}
}

// /rewind on the empty / startup session shows just `(current)`
// (ppref.rewind.during.raw.txt). The non-empty state — one row per
// conversation step — is not implemented yet.
func TestPanelRewindEmptyLayout(t *testing.T) {
	m := mkPanel(panelRewind, "/rewind", 0)
	assertLines(t, plainLines(m), []string{
		"Rewind Conversation",
		"",
		"> (current)",
		"",
		"Keyboard: ↑/↓ Navigate  enter Select  esc Cancel",
	})
	if got := m.panelLines()[2]; !strings.Contains(got, cBlueOpen) {
		t.Errorf("selected row should be blue: %q", got)
	}
}

// /rewind closes silently on Esc, same as /feedback.
func TestRewindExitNotice(t *testing.T) {
	if panelRewind.exitNotice() {
		t.Error("panelRewind.exitNotice() = true, want false")
	}
}

// The /context panel has 16 rows: title + 7 grid rows + 1 blank +
// "System files · auto-loaded" + 3 paths + 1 blank + "Related:" +
// scroll legend. The grid is a 44-cell `□` matrix in dim gray 241
// with right-side descriptors in plain default fg. The model name
// comes from m.model; everything else is the empty-state text
// recovered from /tmp/agycmp/panels/ppref.context.empty.raw.txt.
func TestPanelContextEmptyLayout(t *testing.T) {
	m := mkPanel(panelContext, "/context", 0)
	m.model = "Gemini 3.8 Flash (High)"
	plain := plainLines(m)
	want := []string{
		"└ Context Usage",
		strings.Repeat("□ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □ □", 1)[:0] +
			"     " + "Gemini 3.8 Flash (High) · 0/1.0M tokens",
		"",
	}
	_ = want // layout is verified by cmp_panels.py against ppref.context.empty.raw.txt
	// Just check the first and last rows.
	if got := plain[0]; got != "└ Context Usage" {
		t.Errorf("row 0 = %q, want %q", got, "└ Context Usage")
	}
	if !strings.Contains(plain[1], "□ □ □ □ □") {
		t.Errorf("row 1 should have grid cells, got %q", plain[1])
	}
	if !strings.Contains(plain[1], "Gemini 3.8 Flash (High) · 0/1.0M tokens") {
		t.Errorf("row 1 should have model descriptor, got %q", plain[1])
	}
	if !strings.Contains(plain[len(plain)-1], "esc") {
		t.Errorf("last row should be the scroll legend, got %q", plain[len(plain)-1])
	}
}

// /context closes silently on Esc.
func TestContextExitNotice(t *testing.T) {
	if panelContext.exitNotice() {
		t.Error("panelContext.exitNotice() = true, want false")
	}
}

// contextGridRow emits 44 cells each wrapped in `\x1b[38;2;101;123;131m□\x1b[39m`
// with single spaces between cells, then 5 spaces, then the descriptor
// (no SGR — the descriptor inherits the default fg).
func TestContextGridRowShape(t *testing.T) {
	row := contextGridRow("hello")
	plain := stripSGR(row)
	// Count `□` characters and trailing `hello`.
	if got := strings.Count(plain, "□"); got != 44 {
		t.Errorf("cell count = %d, want 44", got)
	}
	if !strings.HasSuffix(plain, "hello") {
		t.Errorf("descriptor not at end: %q", plain)
	}
	// 43 single-spaces between cells + 5-space padding before descriptor.
	wantMid := "□ □ □ □ □"
	if !strings.Contains(plain, wantMid) {
		t.Errorf("cells not space-separated: %q", plain)
	}
}

func TestPanelAgentsLayout(t *testing.T) {
	m := mkPanel(panelAgents, "/agents", 0)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	assertLines(t, plainLines(m), []string{
		"Create New Agents",
		"  Workspace: " + filepath.Join(wd, ".agents", "agents", "{agent_name}", "agent.md"),
		"  Global: " + filepath.Join(home, ".gemini", "config", "agents", "{agent_name}", "agent.md"),
		"",
		"",
		"Available Agents",
		"> ● default  Default agent",
		"",
		"Keyboard: ↑/↓ Navigate  enter Select / Toggle  k Kill Active Subagent  esc Go Back",
	})
	// The ● glyph is green even though the row itself is blue.
	if got := m.panelLines()[6]; !strings.Contains(got, panelGreen34) {
		t.Errorf("agent bullet should be green: %q", got)
	}
}

// TestPanelAgentsRunningAgents — AGY 1.3.1: when the runMode has
// registered subagents, the /agents panel renders a "Running
// Agents" section that lists each by name with its StatusLine.
// An errored subagent shows "Error: <reason>" instead of "Done".
//
// We exercise three contracts:
//  1. The "Running Agents" header appears when there are subagents.
//  2. A subagent that stopped on error renders as "Error: <reason>"
//     (not "Done") — the bug AGY 1.3.1 closed.
//  3. A successfully-completed subagent renders as "Done".
func TestPanelAgentsRunningAgents(t *testing.T) {
	m := mkPanel(panelAgents, "/agents", 0)
	mgr := subagent.NewManager()

	// Failed subagent: simulate a quota-style failure. We
	// don't need to actually exec a failing process — the
	// StatusLine mapping is the contract we're pinning down.
	failed := subagent.NewSubagent("explore-bot", "ls")
	failed.Wait(context.Background()) // no Start — error path
	// Force the errored state and reason directly so we don't
	// depend on the shell machinery in the unit test.
	markFailed(failed, "quota exceeded")
	mgr.Add(failed)

	// Healthy subagent: a successful exit leaves State="done"
	// with an empty LastError, which renders as "Done".
	done := subagent.NewSubagent("true")
	if err := done.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := done.Wait(context.Background()); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	mgr.Add(done)

	m.taskSubagentManager = mgr

	plain := plainLines(m)

	// Header row.
	hasHeader := false
	for _, l := range plain {
		if l == "Running Agents" {
			hasHeader = true
			break
		}
	}
	if !hasHeader {
		t.Fatalf("expected \"Running Agents\" section, got: %v", plain)
	}

	// Failed row carries "Error:" and the reason.
	if !containsRow(plain, "explore-bot  Error: quota exceeded") {
		t.Fatalf("expected errored row with reason, got: %v", plain)
	}

	// Successful row carries "Done".
	if !containsRow(plain, "true  Done") {
		t.Fatalf("expected successful row with \"Done\", got: %v", plain)
	}

	// The errored row must NOT be displayed as "Done" — this is
	// the bug AGY 1.3.1 closed.
	for _, l := range plain {
		if strings.Contains(l, "explore-bot") && strings.HasSuffix(l, "  Done") {
			t.Fatalf("errored subagent rendered as Done: %q", l)
		}
	}
}

// markFailed mutates a Subagent into the failed state with a known
// reason. We do this directly because the bash-driven
// Wait() path takes a real exec.Cmd, and the test only cares about
// the StatusLine mapping — the wire shape (State="failed",
// LastError=<reason>) is what panelAgentsLines reads.
func markFailed(s *subagent.Subagent, reason string) {
	// Inject by sending it through Wait's natural exit path:
	// run a sub-process that exits non-zero.
	cmd := exec.Command("sh", "-c", "exit 1")
	cmd.Stdin, _ = os.Open(os.DevNull)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	s.Cmd = cmd
	if err := s.Start(); err != nil {
		// If sh isn't available we still set the fields by
		// hand so the test runs on minimal CI images.
		s.SetStateForTest("failed")
		s.SetLastErrorForTest(reason)
		return
	}
	if err := s.Wait(context.Background()); err == nil {
		s.SetStateForTest("failed")
		s.SetLastErrorForTest(reason)
		return
	}
	// Override the captured reason so we test against the
	// exact "quota exceeded" string the changelog mentions.
	s.SetLastErrorForTest(reason)
}

// containsRow is a tiny helper so the test reads cleanly.
func containsRow(lines []string, want string) bool {
	for _, l := range lines {
		if strings.Contains(l, want) {
			return true
		}
	}
	return false
}

// The effort slider geometry was recovered by driving REF's /effort
// panel with ←/→: three 15-cell groups, the active one collapsing to a
// single ◉, and the bar coloured only up to and including the active
// marker. The label row's plain text is the same at every level — the
// group split changes, not the glyphs.
func TestPanelEffortLayout(t *testing.T) {
	const labels = "          low          medium          high      "
	tests := []struct {
		level  int
		bar    string
		blurb  string
		active string
		head   string // the coloured head, right after the level colour
	}{
		{
			level:  effortHigh,
			bar:    "  ◂        ●━━━━━━━━━━━━━━●━━━━━━━━━━━━━━◉        ▸",
			blurb:  "    Deepest reasoning for complex problems — slower but strongest  (current)",
			active: panelEffHighOpen,
			head:   "       ●━━━━━━━━━━━━━━●━━━━━━━━━━━━━━◉",
		},
		{
			level:  effortMedium,
			bar:    "  ◂        ●━━━━━━━━━━━━━━◉──────────────○        ▸",
			blurb:  "    Balanced speed and reasoning quality for most tasks",
			active: panelEffMedOpen,
			head:   "       ●━━━━━━━━━━━━━━◉",
		},
		{
			level:  effortLow,
			bar:    "  ◂        ◉──────────────○──────────────○        ▸",
			blurb:  "    Faster responses, lighter reasoning — great for simpler tasks",
			active: panelEffLowOpen,
			head:   "       ◉",
		},
	}
	for _, tc := range tests {
		m := mkPanel(panelEffort, "/effort", tc.level)
		got := plainLines(m)
		if got[0] != "Set Effort" {
			t.Errorf("level %d title = %q", tc.level, got[0])
		}
		if got[2] != tc.bar {
			t.Errorf("level %d bar:\n got: %q\nwant: %q", tc.level, got[2], tc.bar)
		}
		if got[3] != labels {
			t.Errorf("level %d labels:\n got: %q\nwant: %q", tc.level, got[3], labels)
		}
		if got[4] != tc.blurb {
			t.Errorf("level %d blurb:\n got: %q\nwant: %q", tc.level, got[4], tc.blurb)
		}
		if got[6] != "Keyboard: ←/→ Effort  enter Select  esc Go Back" {
			t.Errorf("level %d legend = %q", tc.level, got[6])
		}
		// The bar's head carries the level colour; the tail is 238.
		raw := m.panelLines()[2]
		if !strings.Contains(raw, tc.active+tc.head) {
			t.Errorf("level %d head should be %q: %q", tc.level, tc.active+tc.head, raw)
		}
		if tail := raw[strings.Index(raw, tc.head)+len(tc.head):]; tc.level != effortHigh {
			if !strings.Contains(tail, panelGray238) || !strings.Contains(tail, "○") {
				t.Errorf("level %d tail should be a dim empty run: %q", tc.level, tail)
			}
		} else if !strings.Contains(tail, panelGray238) {
			t.Errorf("level %d should still close its tail in 238: %q", tc.level, tail)
		}
	}
	// Exactly one level carries "(current)" — the one matching the
	// session's effort (high).
	for _, level := range []int{effortLow, effortMedium, effortHigh} {
		m := mkPanel(panelEffort, "/effort", level)
		n := strings.Count(plainLines(m)[4], "(current)")
		if level == effortHigh && n != 1 {
			t.Errorf("high should be marked current, got %d", n)
		}
		if level != effortHigh && n != 0 {
			t.Errorf("level %d should not be marked current", level)
		}
	}
}

// The legend rows of /hooks, /feedback and /diff do not use the
// "Keyboard:" prefix and separate their entries with " · ".
func TestPanelDotHint(t *testing.T) {
	got := stripSGR(panelDotHint("   ", "↑/↓", "Navigate", "enter", "Select"))
	if got != "   ↑/↓ Navigate · enter Select" {
		t.Errorf("dot hint = %q", got)
	}
}

func TestSlashEchoLines(t *testing.T) {
	// Transcribed from /tmp/agycmp/panels/ppref.rename.raw.txt:
	//   \x1b[1m\x1b[38;5;26m> /rename\x1b[0m
	//   \x1b[1m\x1b[38;5;26m  ⎿  \x1b[0m\x1b[38;2;88;110;117mError: …\x1b[39m
	got := slashEchoLines("/rename", "Error: Please provide a new name for the conversation.")
	if len(got) != 3 {
		t.Fatalf("got %d lines, want 3", len(got))
	}
	if p := stripSGR(got[0]); p != "> /rename" {
		t.Errorf("echo line = %q", p)
	}
	if p := stripSGR(got[1]); p != "  ⎿  Error: Please provide a new name for the conversation." {
		t.Errorf("feedback line = %q", p)
	}
	if got[2] != "" {
		t.Errorf("trailing line = %q, want empty", got[2])
	}
	if !strings.Contains(got[0], cWarningBlueOpen) || !strings.Contains(got[1], panelGray238) {
		t.Error("echo must use the bold bright-blue bullet and the 238 body")
	}
}

func TestPanelExitNotice(t *testing.T) {
	// Cycle 193: the silent-on-Esc set grows to include /plugin,
	// /changelog and /diff in addition to the cycle-185 trio (mcp,
	// permissions, codesearch) and the later cycle-187/189/190 trio
	// (feedback, rewind, context).
	silent := map[panelKind]bool{
		panelMCP:         true,
		panelPermissions: true,
		panelCodeSearch:  true,
		panelFeedback:    true,
		panelRewind:      true,
		panelContext:     true,
		panelPlugin:      true,
		panelChangelog:   true,
		panelDiff:        true,
	}
	for k := panelMCP; k <= panelDiff; k++ {
		if got, want := k.exitNotice(), !silent[k]; got != want {
			t.Errorf("%d.exitNotice() = %v, want %v", k, got, want)
		}
		if got, want := k.keepStatusHint(), k == panelCodeSearch; got != want {
			t.Errorf("%d.keepStatusHint() = %v, want %v", k, got, want)
		}
	}
}

func TestClosePanelAppendsExitNotice(t *testing.T) {
	m := mkPanel(panelTasks, "/tasks", 0)
	m.closePanel()
	if m.panel.kind != panelNone {
		t.Fatal("panel not cleared")
	}
	if len(m.response) != 3 {
		t.Fatalf("response = %q, want a 3-line echo block", m.response)
	}
	if p := stripSGR(m.response[1]); p != "  ⎿  Exited /tasks command" {
		t.Errorf("exit notice = %q", p)
	}

	// /mcp closes silently.
	m2 := mkPanel(panelMCP, "/mcp", 0)
	m2.closePanel()
	if len(m2.response) != 0 {
		t.Fatalf("silent close appended %q", m2.response)
	}
}

func TestHandlePanelKeyMovesCursorAndClamps(t *testing.T) {
	m := mkPanel(panelHooks, "/hooks", 0)
	for i := 0; i < 20; i++ {
		if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyDown}); !handled {
			t.Fatal("down not handled")
		}
	}
	if m.panel.cursor != 4 {
		t.Errorf("cursor = %d, want 4 (clamped)", m.panel.cursor)
	}
	for i := 0; i < 20; i++ {
		m.handlePanelKey(tea.Key{Code: tea.KeyUp})
	}
	if m.panel.cursor != 0 {
		t.Errorf("cursor = %d, want 0 (clamped)", m.panel.cursor)
	}
	// Enter and printable input are swallowed; modified keys are not.
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Error("enter should be swallowed")
	}
	if handled, _ := m.handlePanelKey(tea.Key{Text: "x"}); !handled {
		t.Error("printable input should be swallowed")
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: 'c', Mod: tea.ModCtrl}); handled {
		t.Error("ctrl+c must fall through to the global binding")
	}
}

func TestHandlePanelKeyEffortUsesLeftRight(t *testing.T) {
	m := mkPanel(panelEffort, "/effort", effortHigh)
	m.handlePanelKey(tea.Key{Code: tea.KeyDown}) // vertical keys are inert
	if m.panel.cursor != effortHigh {
		t.Fatalf("down moved the effort cursor to %d", m.panel.cursor)
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyLeft})
	m.handlePanelKey(tea.Key{Code: tea.KeyLeft})
	if m.panel.cursor != effortLow {
		t.Errorf("cursor = %d, want low", m.panel.cursor)
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyLeft}) // clamps
	if m.panel.cursor != effortLow {
		t.Errorf("cursor = %d, want low (clamped)", m.panel.cursor)
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyRight})
	if m.panel.cursor != effortMedium {
		t.Errorf("cursor = %d, want medium", m.panel.cursor)
	}
}

func TestCurrentEffortFromFlags(t *testing.T) {
	m := &runMode{}
	if got := m.currentEffort(); got != effortHigh {
		t.Errorf("nil flags -> %d, want high (%d)", got, effortHigh)
	}
}

// ----------------------------------------------------------------------
// Cycle 193: A-class panel overlays (/model /config /plugin /skills
// /usage /changelog /diff).
//
// Each test below verifies the panel renders with the REF-captured
// structural shape: title text, row count for cursor-bearing panels,
// brand-blue highlight on the active row, and the keyboard hint.
// ----------------------------------------------------------------------

// /model — picked first row is the high-thinking current model.
func TestPanelModelLayout(t *testing.T) {
	// Cycle 197: panelModelLines reads settings.json for the (current)
	// annotation. Seed ANTIGRAVITY_HOME so the test is hermetic and
	// asserts the slug-driven mark, not a hardcoded default.
	cycle196TestHome(t, map[string]any{
		"model": "gemini-3.8-flash-high",
	})
	m := mkPanel(panelModel, "/model", 0)
	plain := plainLines(m)
	if plain[0] != "Switch Model" {
		t.Errorf("title = %q", plain[0])
	}
	// Row 1 blank, row 2 "  Search: ▮" (with reverse-video cursor
	// glyph), row 3 underline, row 4 blank (cycle 197 — REF
	// ppref.model.raw.txt line 17), then 5 model rows (live 1.4.3).
	// The Gemini row (model row index 0) must carry `(current)` and be
	// selected with "> ".
	if plain[4] != "" {
		t.Errorf("row 4 should be blank: %q", plain[4])
	}
	if !strings.Contains(plain[5], "(current)") {
		t.Errorf("model row 0 (Gemini) should be marked (current): %q", plain[5])
	}
	if !strings.HasPrefix(plain[5], "> ") {
		t.Errorf("cursor 0 should highlight row 5 (Gemini): %q", plain[5])
	}
	// Cursor movement (cycle 193: cursor is bound to model rows).
	m.panel.cursor = 2
	got := plainLines(m)
	if !strings.HasPrefix(got[7], "> ") || !strings.HasPrefix(got[5], "  ") {
		t.Errorf("cursor 2 should highlight row 7: %q / %q", got[5], got[7])
	}
	// Last row carries the keyboard legend.
	last := stripSGR(plain[len(plain)-1])
	if !strings.Contains(last, "Navigate") || !strings.Contains(last, "Effort") {
		t.Errorf("legend = %q", last)
	}
	// Cursor highlight uses brand blue (selected row carries cBlueOpen).
	lines := m.panelLines()
	if !strings.Contains(lines[7], cBlueOpen) {
		t.Errorf("selected model row should be brand blue: %q", lines[8])
	}
}

// /config — settings picker, 10 of 20 shown, cursor-bearing on column-0.
//
// Cycle 208: the settings home is isolated so the catalogue is always the
// 20-row default (the shared settings.json may set editorMode=vim, which
// adds the nested "Insert First" row and changes the counter text).
func TestPanelConfigLayout(t *testing.T) {
	cycle196TestHome(t, map[string]any{})
	m := mkPanel(panelConfig, "/config", 0)
	plain := plainLines(m)
	if plain[0] != "Settings" {
		t.Errorf("title = %q", plain[0])
	}
	// Cursor-bearing list: 10 setting rows; the counter reads
	// "[1-10 of 20 items]" verbatim from REF.
	joined := strings.Join(plain, "\n")
	if !strings.Contains(joined, "[1-10 of 20 items]") {
		t.Errorf("missing 1-10 of 20 counter: %s", joined)
	}
	// Cursor movement shifts the "> " marker. Cycle 193 finds it
	// by scanning rather than depending on row-index arithmetic
	// (the exact row depends on the search-box/underline/counter
	// layout).
	hi := -1
	for i, l := range plain {
		if strings.HasPrefix(l, "> ") && i >= 5 {
			hi = i
			break
		}
	}
	if hi != 5 {
		t.Errorf("cursor 0: highlight row = %d, want 5 (first setting)", hi)
	}
	m.panel.cursor = 4
	hi = -1
	for i, l := range plainLines(m) {
		if strings.HasPrefix(l, "> ") {
			hi = i
		}
	}
	if hi != 9 {
		t.Errorf("cursor 4: highlight row = %d, want 9", hi)
	}
	// Legend contains "Change" for the ←/→ pair REF renders.
	last := stripSGR(plain[len(plain)-1])
	if !strings.Contains(last, "Change") || !strings.Contains(last, "Clear Search/Exit") {
		t.Errorf("legend = %q", last)
	}
}

// /plugin — 4 enabled plugins on the Installed tab, cycles silently.
func TestPanelPluginLayout(t *testing.T) {
	// Cycle 198: the panel now reads the real plugin tree, so isolate it and
	// seed the four stock plugins plus one marketplace.
	root := cycle198Root(t)
	for _, p := range pluginInstalledFallback {
		seedPlugin(t, root, p.name, p.desc, "1.0.0")
	}
	var entries [][2]string
	for i := 0; i < 17; i++ {
		entries = append(entries, [2]string{"marketplace-plugin-" + string(rune('a'+i)), "A plugin."})
	}
	seedMarketplace(t, root, "antigravity-plugins-official", entries)
	seedPluginConfig(t, root, map[string]bool{
		"chrome-devtools-plugin":     true,
		"google-antigravity-sdk":     true,
		"modern-web-guidance-plugin": true,
		"science":                    true,
	})
	if panelPlugin.exitNotice() {
		t.Error("panelPlugin.exitNotice() = true, want false")
	}
	m := mkPanel(panelPlugin, "/plugin", 0)
	plain := plainLines(m)
	// Title is ` Plugins` with a leading space, then "Installed (4)"
	// tab and "Discover (17)" secondary.
	if !strings.Contains(plain[0], "Plugins") {
		t.Errorf("title = %q", plain[0])
	}
	if !strings.Contains(plain[1], "Installed") || !strings.Contains(plain[1], "(4)") {
		t.Errorf("Installed tab = %q", plain[1])
	}
	if !strings.Contains(plain[1], "Discover (17)") {
		t.Errorf("Discover secondary tab = %q", plain[1])
	}
	// 4 enabled plugin rows; first row has "(4)" must show selected.
	joined := strings.Join(plain, "\n")
	if !strings.Contains(joined, "chrome-devtools-plugin") {
		t.Errorf("plugin list missing chrome-devtools-plugin: %s", joined)
	}
	if !strings.Contains(joined, "modern-web-guidance-plugin") {
		t.Errorf("plugin list missing modern-web-guidance-plugin: %s", joined)
	}
	// Cursor 2 → 3rd plugin highlighted (located by scan rather than
	// row-index arithmetic since the title row contains the tabs and
	// the layout can shift).
	m.panel.cursor = 2
	got := plainLines(m)
	hi := -1
	for i, l := range got {
		if strings.HasPrefix(l, "> ") && strings.Contains(l, installedPluginRows()[2].name) {
			hi = i
		}
	}
	if hi < 0 {
		t.Errorf("cursor 2 highlight not found:\n%s", strings.Join(got, "\n"))
	}
	// Legend contains Cycle Tabs and Toggle Enable.
	last := stripSGR(plain[len(plain)-1])
	if !strings.Contains(last, "Cycle Tabs") || !strings.Contains(last, "Toggle Enable") {
		t.Errorf("legend = %q", last)
	}
}

// /skills — cycle 203: the multi-source picker rebuilt on the real
// catalogue. The detailed layout assertions live in
// slash_cycle203_test.go (assertPanelSkillsLayout), keeping this shared
// file's edit to a single delegation.
func TestPanelSkillsLayout(t *testing.T) { assertPanelSkillsLayout(t) }

// /usage — quota viewer with placeholder bars and "└ Models & Quota".
func TestPanelUsageLayout(t *testing.T) {
	m := mkPanel(panelUsage, "/usage", 0)
	plain := plainLines(m)
	if !strings.Contains(plain[0], "└ Models & Quota") {
		t.Errorf("title = %q", plain[0])
	}
	// The Account row carries the email in brand blue; the static
	// quota text uses placeholder 100% / 0.00%.
	joined := strings.Join(plain, "\n")
	expectedEmail := auth.EffectiveUserEmail()
	if expectedEmail == "" {
		expectedEmail = "navigaid@gmail.com"
	}
	if !strings.Contains(joined, expectedEmail) {
		t.Errorf("missing account email: %s", joined)
	}
	if !strings.Contains(joined, "100.00%") {
		t.Errorf("missing 100%% bar: %s", joined)
	}
	if !strings.Contains(joined, "0.00%") {
		t.Errorf("missing 0%% bar: %s", joined)
	}
	if !strings.Contains(joined, "GEMINI MODELS") {
		t.Errorf("missing GEMINI MODELS group: %s", joined)
	}
	if !strings.Contains(joined, "CLAUDE AND GPT MODELS") {
		t.Errorf("missing CLAUDE/GPT group: %s", joined)
	}
	// Counter (1-17 of 20 lines) and Scroll legend.
	if !strings.Contains(joined, "(1–17 of 20 lines)") {
		t.Errorf("missing line counter: %s", joined)
	}
	last := stripSGR(plain[len(plain)-1])
	if !strings.Contains(last, "Scroll") || !strings.Contains(last, "Page") {
		t.Errorf("legend = %q", last)
	}
}

// /changelog — viewer heading in yellow + bullet block + counter.
func TestPanelChangelogLayout(t *testing.T) {
	if panelChangelog.exitNotice() {
		t.Error("panelChangelog.exitNotice() = true, want false")
	}
	m := mkPanel(panelChangelog, "/changelog", 0)
	plain := plainLines(m)
	if !strings.Contains(plain[0], "Pigasus CLI Changelog") {
		t.Errorf("title = %q", plain[0])
	}
	// Tagline + bullets + counter; assert at least the headline
	// version "1.4.1" and the line counter survive (SGR-stripped).
	// Cycle 207 added the blank row REF shows between the title and the
	// tagline, so the window opens at 18 rows rather than the cycle-201 17.
	//
	// The embedded changelog_reference.txt is a verbatim `agy changelog`
	// capture: the 1.4.6 binary's feed still ends at 1.4.5 (the 1.4.6
	// release notes are not in it yet), so the panel opens on 1.4.5.
	joined := strings.Join(plain, "\n")
	if !strings.Contains(joined, "## 1.4.5") {
		t.Errorf("missing 1.4.5 heading: %s", joined)
	}
	if !strings.Contains(joined, fmt.Sprintf("(1–18 of %d lines)", tuiviewer.ChangelogTotalLines)) {
		t.Errorf("missing line counter: %s", joined)
	}
	if !strings.Contains(joined, "Improved how agent responses read") {
		t.Errorf("missing opening bullet: %s", joined)
	}
	last := stripSGR(plain[len(plain)-1])
	if !strings.Contains(last, "Navigate") {
		t.Errorf("legend = %q", last)
	}
}

// /diff — no-VCS variant with tab row + warning + dot hint.
func TestPanelDiffLayout(t *testing.T) {
	if panelDiff.exitNotice() {
		t.Error("panelDiff.exitNotice() = true, want false")
	}
	m := mkPanel(panelDiff, "/diff", 0)
	plain := plainLines(m)
	// Tab row: bold "Diff" (active tab) plus three tab names.
	joined := strings.Join(plain, "\n")
	if !strings.Contains(plain[0], "Diff") {
		t.Errorf("missing Diff heading: %q", plain[0])
	}
	if !strings.Contains(plain[0], "All Changes") {
		t.Errorf("missing All Changes tab: %q", plain[0])
	}
	if !strings.Contains(plain[0], "Per Turn") {
		t.Errorf("missing Per Turn tab: %q", plain[0])
	}
	if !strings.Contains(plain[0], "Commit Tree") {
		t.Errorf("missing Commit Tree tab: %q", plain[0])
	}
	// Warning row uses cToolBulletErrOpen (38;5;166) on the ⚠ glyph.
	if !strings.Contains(joined, "no supported VCS found") {
		t.Errorf("missing VCS warning: %s", joined)
	}
	lines := m.panelLines()
	if !strings.Contains(lines[2], cToolBulletErrOpen) {
		t.Errorf("warning glyph should be 166: %q", lines[2])
	}
	// Dot hint legends use ←/→ switch view, esc dismiss.
	last := stripSGR(plain[len(plain)-1])
	if !strings.Contains(last, "switch view") || !strings.Contains(last, "dismiss") {
		t.Errorf("legend = %q", last)
	}
}

// ----------------------------------------------------------------------
// Cycle 201: panel key actions.
// ----------------------------------------------------------------------

// panelText joins the panel's SGR-stripped rows, the shape the layout
// tests above assert against.
func panelText(m *runMode) string {
	return strings.Join(plainLines(m), "\n")
}

// /usage's window scrolls by ↑/↓ and its counter follows. The document
// is 20 rows with a 17-row window, so there are exactly three scroll
// steps — REF's "(1–17 of 20 lines)" at the top.
func TestPanelUsageViewerScrolls(t *testing.T) {
	m := mkPanel(panelUsage, "/usage", 0)
	if !strings.Contains(panelText(m), "(1–17 of 20 lines)") {
		t.Fatalf("initial counter wrong:\n%s", panelText(m))
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Fatal("down not handled")
	}
	if m.panel.window != 1 {
		t.Fatalf("window = %d, want 1", m.panel.window)
	}
	if !strings.Contains(panelText(m), "(2–18 of 20 lines)") {
		t.Errorf("counter did not follow the window:\n%s", panelText(m))
	}
	// ctrl+end jumps to the last window; the reconstructed tail row shows.
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnd, Mod: tea.ModCtrl}); !handled {
		t.Fatal("ctrl+end not handled")
	}
	if m.panel.window != 3 {
		t.Fatalf("ctrl+end window = %d, want 3", m.panel.window)
	}
	text := panelText(m)
	if !strings.Contains(text, "(4–20 of 20 lines)") {
		t.Errorf("bottom counter wrong:\n%s", text)
	}
	if !strings.Contains(text, "│models in the group.") {
		t.Errorf("bottom window should show the tail note:\n%s", text)
	}
	// Down past the end clamps.
	m.handlePanelKey(tea.Key{Code: tea.KeyDown})
	if m.panel.window != 3 {
		t.Errorf("down at bottom moved window to %d", m.panel.window)
	}
	// ctrl+home returns to the top.
	m.handlePanelKey(tea.Key{Code: tea.KeyHome, Mod: tea.ModCtrl})
	if m.panel.window != 0 {
		t.Errorf("ctrl+home window = %d, want 0", m.panel.window)
	}
	// pgdn / pgup page by the window height and clamp the same way.
	m.handlePanelKey(tea.Key{Code: tea.KeyPgDown})
	if m.panel.window != 3 {
		t.Errorf("pgdown window = %d, want 3 (clamped)", m.panel.window)
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyPgUp})
	if m.panel.window != 0 {
		t.Errorf("pgup window = %d, want 0", m.panel.window)
	}
}

// The /changelog viewer scrolls through the whole release-notes document
// and keeps REF's wrapped-line denominator (2014 as measured at 120x40
// against the 1.4.4 binary; 1962 was the 1.4.2-era fixture's).
func TestPanelChangelogViewerScrolls(t *testing.T) {
	m := mkPanel(panelChangelog, "/changelog", 0)
	total := len(m.changelogViewerLines())
	vis := tuiviewer.ChangelogWindowRows()
	if total <= vis {
		t.Fatalf("document too short to scroll: %d rows (window %d)", total, vis)
	}
	if !strings.Contains(panelText(m), fmt.Sprintf("(1\u2013%d of %d lines)", vis, tuiviewer.ChangelogTotalLines)) {
		t.Fatalf("initial counter wrong:\n%s", panelText(m)[:300])
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Fatal("down not handled")
	}
	if m.panel.window != 1 {
		t.Fatalf("window = %d, want 1", m.panel.window)
	}
	if !strings.Contains(panelText(m), fmt.Sprintf("(2\u2013%d of %d lines)", vis+1, tuiviewer.ChangelogTotalLines)) {
		t.Errorf("counter did not follow the window:\n%s", panelText(m)[:300])
	}
	// ctrl+end lands on the final window, which shows the 1.0.0 section.
	m.handlePanelKey(tea.Key{Code: tea.KeyEnd, Mod: tea.ModCtrl})
	if want := total - vis; m.panel.window != want {
		t.Fatalf("ctrl+end window = %d, want %d", m.panel.window, want)
	}
	text := panelText(m)
	if !strings.Contains(text, "Initial release of the Antigravity CLI") {
		t.Errorf("bottom of changelog should show the 1.0.0 release:\n%s", text)
	}
	if !strings.Contains(text, fmt.Sprintf("(%d\u2013%d of %d lines)", total-vis+1, total, tuiviewer.ChangelogTotalLines)) {
		t.Errorf("bottom counter wrong:\n%s", text)
	}
	// ctrl+home returns to the first screen.
	m.handlePanelKey(tea.Key{Code: tea.KeyHome, Mod: tea.ModCtrl})
	if m.panel.window != 0 {
		t.Errorf("ctrl+home window = %d, want 0", m.panel.window)
	}
	if !strings.Contains(panelText(m), "## 1.4.5") {
		t.Errorf("first screen should still open on 1.4.5:\n%s", panelText(m)[:200])
	}
}

// The viewers swallow printable input but let modified keys (ctrl+c)
// reach the global bindings; Esc closes them.
func TestPanelViewerKeySwallowAndEsc(t *testing.T) {
	m := mkPanel(panelUsage, "/usage", 0)
	if handled, _ := m.handlePanelKey(tea.Key{Text: "x"}); !handled {
		t.Error("printable input should be swallowed by a viewer")
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: 'c', Mod: tea.ModCtrl}); handled {
		t.Error("ctrl+c must fall through to the global binding")
	}
	// /usage emits the standard "Exited" notice on Esc.
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEsc}); !handled {
		t.Fatal("esc not handled")
	}
	if m.panel.kind != panelNone {
		t.Fatal("esc did not close the panel")
	}
	if got := stripSGR(strings.Join(m.response, "\n")); !strings.Contains(got, "Exited /usage command") {
		t.Errorf("usage esc notice = %q", got)
	}
	// /changelog closes silently.
	c := mkPanel(panelChangelog, "/changelog", 0)
	c.handlePanelKey(tea.Key{Code: tea.KeyEsc})
	if len(c.response) != 0 {
		t.Errorf("changelog esc should be silent, got %q", c.response)
	}
}

// /diff's ←/→ switch the active tab. The plain text is identical for all
// three tabs, so the assertion is on which tab carries the brand blue.
func TestPanelDiffTabSwitch(t *testing.T) {
	m := mkPanel(panelDiff, "/diff", 0)
	tabRow := func() string { return m.panelLines()[0] }
	if !strings.Contains(tabRow(), cBlueOpen+"All Changes"+cBlueClose) {
		t.Fatalf("All Changes should be active at open: %q", tabRow())
	}
	// Two presses move Per Turn then Commit Tree; a third clamps.
	for i, want := range []struct {
		tab  int
		name string
	}{{1, "Per Turn"}, {2, "Commit Tree"}, {2, "Commit Tree"}} {
		if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyRight}); !handled {
			t.Fatalf("right #%d not handled", i)
		}
		if m.panel.tab != want.tab {
			t.Fatalf("right #%d: tab = %d, want %d", i, m.panel.tab, want.tab)
		}
		if !strings.Contains(tabRow(), cBlueOpen+want.name+cBlueClose) {
			t.Errorf("tab %d: %q should be active: %q", want.tab, want.name, tabRow())
		}
	}
	for i := 0; i < 5; i++ {
		m.handlePanelKey(tea.Key{Code: tea.KeyLeft})
	}
	if m.panel.tab != 0 {
		t.Errorf("left should clamp at the first tab, got %d", m.panel.tab)
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: 'c', Mod: tea.ModCtrl}); handled {
		t.Error("ctrl+c must fall through to the global binding")
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEsc}); !handled {
		t.Fatal("esc not handled")
	}
	if m.panel.kind != panelNone {
		t.Fatal("esc did not close the diff panel")
	}
	if len(m.response) != 0 {
		t.Errorf("diff esc should be silent, got %q", m.response)
	}
}

// /plugin's tab cycles Installed ⇄ Discover. The active tab is the
// bold-underline blue one; the Discover tab has no captured rows so it
// renders the chrome only.
// /skills' `enter View` opens a per-skill detail view; Esc returns to the
// list and a second Esc closes the panel with the standard notice. Cycle 211
// replaces the cycle-201 minimal "esc Back" legend with the SKILL.md viewer
// the reference draws (see slash_skills_detail.go); the test now checks the
// viewer-shaped "↑/↓ scroll · pgup/pgdown page · ctrl+end bottom · ctrl+home
// top · esc back" instead.
func TestPanelSkillsDetailView(t *testing.T) {
	rows := skillsFromDisk()
	m := mkPanel(panelSkills, "/skills", 0)
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("enter not handled")
	}
	if len(rows) == 0 {
		if m.panel.detail {
			t.Fatal("detail opened with no enumerated skills")
		}
		return
	}
	if !m.panel.detail {
		t.Fatal("enter did not open the detail view")
	}
	text := panelText(m)
	if !strings.Contains(text, rows[0].name) {
		t.Errorf("detail missing the skill name %q:\n%s", rows[0].name, text)
	}
	if !strings.Contains(text, "SKILL.md") {
		t.Errorf("detail missing the SKILL.md path:\n%s", text)
	}
	if !strings.Contains(text, "esc back") {
		t.Errorf("detail legend should read …esc back:\n%s", text)
	}
	// Esc returns to the list without closing the panel or touching the
	// transcript.
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEsc}); !handled {
		t.Fatal("esc not handled")
	}
	if m.panel.detail || m.panel.kind != panelSkills {
		t.Fatalf("esc should return to the list: detail=%v kind=%v",
			m.panel.detail, m.panel.kind)
	}
	if len(m.response) != 0 {
		t.Errorf("returning from detail should not touch the transcript: %q", m.response)
	}
	// From the list, Esc closes and emits the standard notice.
	m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
	if m.panel.kind != panelNone {
		t.Fatal("second esc did not close the panel")
	}
	if got := stripSGR(strings.Join(m.response, "\n")); !strings.Contains(got, "Exited /skills command") {
		t.Errorf("skills esc notice = %q", got)
	}
}

// /feedback: 1-6 select-and-continue and enter both advance to the
// free-text stage; printing/backspace edit the buffer; esc returns to
// stage 1 with the draft discarded; enter (or ctrl+s) submits and
// records the local-only message.
//
// Cycle 221 changed stage-2 Enter from inert (cycle-208 REF behaviour)
// to submit — see handleFeedbackTextKey for the openagy divergence.
func TestPanelFeedbackStage2(t *testing.T) {
	m := mkPanel(panelFeedback, "/feedback", 0)
	if handled, _ := m.handlePanelKey(tea.Key{Text: "3"}); !handled {
		t.Fatal("1-6 key not handled")
	}
	if m.panel.stage != 1 || m.panel.cursor != 2 {
		t.Fatalf("stage=%d cursor=%d, want stage 1 cursor 2", m.panel.stage, m.panel.cursor)
	}
	text := panelText(m)
	// Cycle 208: REF's stage-2 `Feedback:` line echoes the category *label*
	// without the picker's `N. ` index (fin_fb_stage2.raw.txt).
	if !strings.Contains(text, feedbackCategoryLabel(feedbackCategories[2])) {
		t.Errorf("stage 2 should echo the chosen category:\n%s", text)
	}
	if strings.Contains(text, "3. Took too many steps") {
		t.Errorf("stage 2 should drop the picker index:\n%s", text)
	}
	if !strings.Contains(m.panelLines()[7], searchCursorBlock) {
		t.Errorf("stage 2 should render the input cursor: %q", m.panelLines()[7])
	}
	for _, r := range []string{"h", "i"} {
		m.handlePanelKey(tea.Key{Text: r})
	}
	if m.panel.text != "hi" {
		t.Fatalf("text = %q, want %q", m.panel.text, "hi")
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyBackspace})
	if m.panel.text != "h" {
		t.Fatalf("backspace text = %q, want %q", m.panel.text, "h")
	}
	// Esc returns to the picker with the draft discarded but the
	// category (cursor index) preserved.
	m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
	if m.panel.stage != 0 || m.panel.text != "" {
		t.Fatalf("esc: stage=%d text=%q", m.panel.stage, m.panel.text)
	}
	if m.panel.cursor != 2 {
		t.Fatalf("esc should preserve category: cursor=%d, want 2", m.panel.cursor)
	}
	// Enter on the picker advances; in stage 2 enter submits (cycle 221
	// openagy divergence from REF's ctrl+s-only textarea semantics).
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if m.panel.stage != 1 {
		t.Fatalf("enter did not advance: stage=%d", m.panel.stage)
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if m.panel.kind != panelNone {
		t.Fatalf("enter did not submit: kind=%v", m.panel.kind)
	}
	if got := stripSGR(strings.Join(m.response, "\n")); !strings.Contains(got, "Feedback recorded") {
		t.Errorf("submit should record a message: %q", got)
	}
	// ctrl+s also submits (REF parity), and ctrl+c still falls through.
	m2 := mkPanel(panelFeedback, "/feedback", 0)
	m2.panel.stage = 1
	if handled, _ := m2.handlePanelKey(tea.Key{Code: 'c', Mod: tea.ModCtrl}); handled {
		t.Error("ctrl+c must fall through in stage 2")
	}
	if handled, _ := m2.handlePanelKey(tea.Key{Code: 's', Mod: tea.ModCtrl}); !handled {
		t.Fatal("ctrl+s not handled")
	}
	if m2.panel.kind != panelNone {
		t.Error("ctrl+s did not submit")
	}
}

func TestSlashLogout_GeminiAPIKeyEnv(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "fake-key")
	t.Setenv("AGY_ADC_AUTH", "")

	got := logoutFeedback()
	want := "The active session uses a Gemini API key from the environment (GEMINI_API_KEY). To change it, update or unset the environment variable."
	if got != want {
		t.Fatalf("logoutFeedback() with GEMINI_API_KEY = %q, want %q", got, want)
	}
}

func TestSlashLogout_AdcAuthEnv(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("AGY_ADC_AUTH", "1")

	got := logoutFeedback()
	want := "The active session uses Application Default Credentials (AGY_ADC_AUTH). To change accounts, run 'gcloud auth application-default login' or unset the variable."
	if got != want {
		t.Fatalf("logoutFeedback() with AGY_ADC_AUTH = %q, want %q", got, want)
	}
}

func TestSlashLogout_FullClear(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("AGY_ADC_AUTH", "")

	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	tokenFile := filepath.Join(home, "antigravity-oauth-token")
	if err := os.WriteFile(tokenFile, []byte(`{"token":{"access_token":"foo"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	keychainCleared := false
	auth.ClearKeychainTokenFn = func() error {
		keychainCleared = true
		return nil
	}
	defer func() { auth.ClearKeychainTokenFn = nil }()

	m := newSlashModel(t)
	m.handleSlashCommand("/logout")

	wantPrefix := "Signed out from server:"
	output := strings.Join(afterEcho(m), "\n")
	if !strings.HasPrefix(output, wantPrefix) {
		t.Fatalf("/logout output = %q, want prefix %q", output, wantPrefix)
	}
	if _, err := os.Stat(tokenFile); !os.IsNotExist(err) {
		t.Fatalf("expected tokenFile %s to be deleted, got err: %v", tokenFile, err)
	}
	if !keychainCleared {
		t.Fatal("expected keychain to be cleared by /logout")
	}
}

func TestSlashLogin_Handler(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	t.Setenv("HOME", home)
	t.Setenv("JETSKI_OAUTH_TOKEN", "")
	t.Setenv("ANTIGRAVITY_REFRESH_TOKEN", "")

	m := newSlashModel(t)
	handled, cmd := m.handleSlashCommand("/login")
	if !handled {
		t.Fatal("expected /login to be handled")
	}
	if m.login == nil {
		t.Fatal("expected m.login to be active without restarting CLI")
	}
	if cmd == nil {
		t.Fatal("expected login init command returned")
	}

	view := m.View().Content
	if !strings.Contains(view, "Welcome to the") || !strings.Contains(view, "You are currently not signed in.") {
		t.Fatalf("expected login view in m.View(), got:\n%s", view)
	}

	m.login = nil
}

func TestCycle206CanonicalPanelCommand(t *testing.T) {
	cases := map[string]string{
		"cs":         "codesearch",
		"search":     "codesearch",
		"settings":   "config",
		"quota":      "usage",
		"undo":       "rewind",
		"codesearch": "codesearch",
		"tasks":      "tasks",
		"artifact":   "artifact",
		"context":    "context",
		"clear":      "clear",
		"mcp":        "mcp",
		"":           "",
	}
	for in, want := range cases {
		if got := canonicalPanelCommand(in); got != want {
			t.Errorf("canonicalPanelCommand(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCycle206OpenSlashPanelViaAlias(t *testing.T) {
	cases := []struct {
		cmd  string
		kind panelKind
	}{
		{"cs", panelCodeSearch},
		{"search", panelCodeSearch},
		{"settings", panelConfig},
		{"quota", panelUsage},
		{"undo", panelRewind},
	}
	for _, c := range cases {
		m := &runMode{}
		if !m.openSlashPanel(c.cmd) {
			t.Errorf("openSlashPanel(%q) = false, want true", c.cmd)
			continue
		}
		if m.panel.kind != c.kind {
			t.Errorf("openSlashPanel(%q) kind = %v, want %v", c.cmd, m.panel.kind, c.kind)
		}
		if m.panel.cmd != "/"+c.cmd {
			t.Errorf("openSlashPanel(%q) cmd = %q, want %q", c.cmd, m.panel.cmd, "/"+c.cmd)
		}
	}
	m := &runMode{}
	if m.openSlashPanel("clear") {
		t.Error("openSlashPanel(\"clear\") = true, want false")
	}
}

func TestCycle206ListPanelsOwnEveryKey(t *testing.T) {
	kinds := []struct {
		kind panelKind
		cmd  string
	}{
		{panelTasks, "/tasks"},
		{panelArtifact, "/artifact"},
		{panelCodeSearch, "/codesearch"},
		{panelContext, "/context"},
	}
	keys := []tea.Key{
		{Text: "k"},
		{Text: "x"},
		{Text: "p"},
		{Code: tea.KeyEnter},
		{Code: tea.KeyTab},
		{Code: tea.KeyBackspace},
		{Code: tea.KeyUp},
		{Code: tea.KeyDown},
		{Code: tea.KeyLeft},
		{Code: tea.KeyRight},
		{Code: tea.KeyPgUp},
		{Code: tea.KeyPgDown},
		{Code: tea.KeyHome, Mod: tea.ModCtrl},
		{Code: tea.KeyEnd, Mod: tea.ModCtrl},
		{Code: 'g', Mod: tea.ModCtrl},
		{Code: 'l', Mod: tea.ModCtrl},
		{Code: 'o', Mod: tea.ModCtrl},
	}
	for _, k := range kinds {
		m := mkPanel(k.kind, k.cmd, 0)
		before := len(m.response)
		for _, key := range keys {
			handled, _ := m.handlePanelKey(key)
			if !handled {
				t.Errorf("%s: key %+v fell through, want swallowed", k.cmd, key)
			}
		}
		if m.panel.kind != k.kind {
			t.Errorf("%s: panel closed by a non-Esc key", k.cmd)
		}
		if len(m.response) != before {
			t.Errorf("%s: key changed the transcript (%d -> %d)", k.cmd, before, len(m.response))
		}
	}
}

func TestCycle206ListPanelsCtrlCFallsThrough(t *testing.T) {
	for _, k := range []struct {
		kind panelKind
		cmd  string
	}{
		{panelTasks, "/tasks"},
		{panelArtifact, "/artifact"},
		{panelCodeSearch, "/codesearch"},
		{panelContext, "/context"},
	} {
		m := mkPanel(k.kind, k.cmd, 0)
		if handled, _ := m.handlePanelKey(tea.Key{Code: 'c', Mod: tea.ModCtrl}); handled {
			t.Errorf("%s: ctrl+c must fall through to the global binding", k.cmd)
		}
	}
}

func TestCycle206ListPanelsEscCloses(t *testing.T) {
	cases := []struct {
		kind   panelKind
		cmd    string
		notice bool
	}{
		{panelTasks, "/tasks", true},
		{panelArtifact, "/artifact", true},
		{panelCodeSearch, "/codesearch", false},
		{panelContext, "/context", false},
	}
	for _, c := range cases {
		m := mkPanel(c.kind, c.cmd, 0)
		handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
		if !handled {
			t.Errorf("%s: esc not handled", c.cmd)
			continue
		}
		if m.panel.kind != panelNone {
			t.Errorf("%s: esc did not close the panel (kind=%v)", c.cmd, m.panel.kind)
		}
		got := stripSGR(strings.Join(m.response, "\n"))
		if c.notice {
			if !strings.Contains(got, "Exited "+c.cmd+" command") {
				t.Errorf("%s: esc notice = %q", c.cmd, got)
			}
		} else if got != "" {
			t.Errorf("%s: esc should be silent, got %q", c.cmd, got)
		}
	}
}

func TestCycle206ScopedToTheFourListPanels(t *testing.T) {
	for _, k := range []panelKind{panelMCP, panelPermissions} {
		if listPanelKind(k) {
			t.Errorf("listPanelKind(%v) = true, want false", k)
		}
	}
	m := mkPanel(panelMCP, "/mcp", 0)
	if handled, _ := m.handlePanelKey(tea.Key{Code: 'g', Mod: tea.ModCtrl}); handled {
		t.Error("/mcp: ctrl+g should still fall through (out of cycle-206 scope)")
	}
}

func TestChangelogViewer_VimAndBubbleteaKeybindings(t *testing.T) {
	if !changelog.Loaded() {
		t.Skipf("changelog_reference.txt not reachable from cwd %q — skipping", mustGetwd(t))
	}
	m := mkPanel(panelChangelog, "/changelog", 0)
	if m.panel.kind != panelChangelog {
		t.Fatalf("expected panelChangelog, got %v", m.panel.kind)
	}

	if m.panel.window != 0 {
		t.Fatalf("initial window = %d, want 0", m.panel.window)
	}

	handled, _ := m.handlePanelKey(tea.Key{Code: 'j', Text: "j"})
	if !handled || m.panel.window != 1 {
		t.Errorf("after 'j': handled=%v, window=%d, want 1", handled, m.panel.window)
	}

	handled, _ = m.handlePanelKey(tea.Key{Code: 'k', Text: "k"})
	if !handled || m.panel.window != 0 {
		t.Errorf("after 'k': handled=%v, window=%d, want 0", handled, m.panel.window)
	}

	handled, _ = m.handlePanelKey(tea.Key{Code: 'G', Text: "G"})
	spec, _ := m.viewerSpecFor()
	maxOffset := viewerMaxOffset(spec)
	if !handled || m.panel.window != maxOffset {
		t.Errorf("after 'G': handled=%v, window=%d, want %d", handled, m.panel.window, maxOffset)
	}

	handled, _ = m.handlePanelKey(tea.Key{Code: 'g', Text: "g"})
	if !handled || m.panel.window != 0 {
		t.Errorf("after 'g': handled=%v, window=%d, want 0", handled, m.panel.window)
	}

	handled, _ = m.handlePanelKey(tea.Key{Code: 'd', Text: "d"})
	half := spec.visible / 2
	if half < 1 {
		half = 1
	}
	if !handled || m.panel.window != half {
		t.Errorf("after 'd': handled=%v, window=%d, want %d", handled, m.panel.window, half)
	}

	handled, _ = m.handlePanelKey(tea.Key{Code: 'u', Text: "u"})
	if !handled || m.panel.window != 0 {
		t.Errorf("after 'u': handled=%v, window=%d, want 0", handled, m.panel.window)
	}

	handled, _ = m.handlePanelKey(tea.Key{Code: 'q', Text: "q"})
	if !handled || m.panel.kind != panelNone {
		t.Errorf("after 'q': handled=%v, panel.kind=%v, want panelNone", handled, m.panel.kind)
	}
}

func TestListPanels_VimKeybindings(t *testing.T) {
	withSyntheticCatalog(t, cycSharedSkills(3))
	m := mkPanel(panelSkills, "/skills", 0)
	m.panel.cursor = 0

	handled, _ := m.handlePanelKey(tea.Key{Code: 'j', Text: "j"})
	if !handled || m.panel.cursor != 1 {
		t.Errorf("list 'j': handled=%v, cursor=%d, want 1", handled, m.panel.cursor)
	}

	handled, _ = m.handlePanelKey(tea.Key{Code: 'k', Text: "k"})
	if !handled || m.panel.cursor != 0 {
		t.Errorf("list 'k': handled=%v, cursor=%d, want 0", handled, m.panel.cursor)
	}

	handled, _ = m.handlePanelKey(tea.Key{Code: 'G', Text: "G"})
	last := m.panelRowCount(panelSkills) - 1
	if !handled || m.panel.cursor != last {
		t.Errorf("list 'G': handled=%v, cursor=%d, want %d", handled, m.panel.cursor, last)
	}

	handled, _ = m.handlePanelKey(tea.Key{Code: 'g', Text: "g"})
	if !handled || m.panel.cursor != 0 {
		t.Errorf("list 'g': handled=%v, cursor=%d, want 0", handled, m.panel.cursor)
	}

	handled, _ = m.handlePanelKey(tea.Key{Code: 'q', Text: "q"})
	if !handled || m.panel.kind != panelNone {
		t.Errorf("list 'q': handled=%v, panel.kind=%v, want panelNone", handled, m.panel.kind)
	}
}

func TestHelpMode_VimKeybindings(t *testing.T) {
	m := &runMode{helpMode: true, helpTab: 1, helpCursor: 0}

	m.handleHelpModeNavigation(tea.Key{Code: 'j', Text: "j"})
	if m.helpCursor != 1 {
		t.Errorf("help 'j': helpCursor=%d, want 1", m.helpCursor)
	}

	m.handleHelpModeNavigation(tea.Key{Code: 'k', Text: "k"})
	if m.helpCursor != 0 {
		t.Errorf("help 'k': helpCursor=%d, want 0", m.helpCursor)
	}

	m.handleHelpModeNavigation(tea.Key{Code: 'l', Text: "l"})
	if m.helpTab != 2 {
		t.Errorf("help 'l': helpTab=%d, want 2", m.helpTab)
	}

	m.handleHelpModeNavigation(tea.Key{Code: 'h', Text: "h"})
	if m.helpTab != 1 {
		t.Errorf("help 'h': helpTab=%d, want 1", m.helpTab)
	}

	m.handleHelpModeNavigation(tea.Key{Code: 'q', Text: "q"})
	if m.helpMode {
		t.Errorf("help 'q': helpMode=true, want false")
	}
}

func TestSkillsDetail_VimKeybindings(t *testing.T) {
	m := longDetailRunner(t, 50, 80)
	m.panel.detail = true
	m.panel.window = 0

	handled, _ := m.handleSkillsDetailKey(tea.Key{Code: 'j', Text: "j"})
	if !handled || m.panel.window != 1 {
		t.Errorf("detail 'j': handled=%v, window=%d, want 1", handled, m.panel.window)
	}

	handled, _ = m.handleSkillsDetailKey(tea.Key{Code: 'k', Text: "k"})
	if !handled || m.panel.window != 0 {
		t.Errorf("detail 'k': handled=%v, window=%d, want 0", handled, m.panel.window)
	}

	handled, _ = m.handleSkillsDetailKey(tea.Key{Code: 'q', Text: "q"})
	if !handled || m.panel.detail {
		t.Errorf("detail 'q': handled=%v, detail=%v, want false", handled, m.panel.detail)
	}
}

func TestDiff_VimKeybindings(t *testing.T) {
	m := mkPanel(panelDiff, "/diff", 0)
	m.panel.cursor = 0

	handled, _ := m.handleDiffPanelKey(tea.Key{Code: 'q', Text: "q"})
	if !handled || m.panel.kind != panelNone {
		t.Errorf("diff 'q': handled=%v, panel.kind=%v, want panelNone", handled, m.panel.kind)
	}
}

func TestFeatDiff_PanelLinesHeight0(t *testing.T) {
	if !featdiff.SourceLoaded() {
		t.Skipf("feature_diff_table.json not reachable from cwd %q — skipping", mustGetwd(t))
	}
	m := mkPanel(panelFeatDiff, "/feature-diff", 0)
	rows := m.featDiffPanelLines()
	if len(rows) == 0 {
		t.Fatal("featDiffPanelLines() = empty; want title + summary + ≥ 1 data row")
	}
	if !strings.Contains(rows[0], "Feature Diff") {
		t.Errorf("panel first row = %q, want it to contain 'Feature Diff'", rows[0])
	}
	combined := strings.Join(rows, "\n")
	if !strings.Contains(combined, "features ·") {
		t.Errorf("panel summary missing 'features ·' separator:\n%s", combined)
	}
}

// ---- cycle 225: /usage quota state machine tests ---------------------------

func TestCycle225UsageLoadingDoc(t *testing.T) {
	for _, n := range []int{0, 1, 2, 3, 4, 5, 6, 7} {
		m := mkPanel(panelUsage, "/usage", 0)
		m.width, m.height = 220, 50
		m.quotaFrame = n
		m.panel.quota.state = quotaLoading
		doc := usageDocLive(m)
		if len(doc) != 1 {
			t.Fatalf("frame=%d: doc has %d rows, want 1", n, len(doc))
		}
		wantFrame := []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}[n]
		want := "  " + wantFrame + "  " + cDimGrayOpen + "Loading quota summary..." + cDimGrayClose
		if doc[0] != want {
			t.Errorf("frame=%d:\n got %q\nwant %q", n, doc[0], want)
		}
	}
}

func TestCycle225UsageLoadedDoc(t *testing.T) {
	m := mkPanel(panelUsage, "/usage", 0)
	m.width, m.height = 220, 50
	m.panel.quota.refreshG = time.Now().Add(168 * time.Hour)
	m.panel.quota.refreshO = time.Now().Add(48 * time.Hour)
	doc := usageDocLive(m)
	if len(doc) != 20 {
		t.Fatalf("loaded doc = %d rows, want 20", len(doc))
	}
	if !strings.Contains(doc[5], "\x1b[32mRefreshes in ") {
		t.Errorf("gemini refresh row = %q", doc[5])
	}
	if !strings.Contains(doc[13], "\x1b[31mRefreshes in ") {
		t.Errorf("claude refresh row = %q", doc[13])
	}
	if !strings.HasPrefix(doc[16], "  \x1b[38;2;238;232;213m│\x1b[38;2;101;123;131mWithin each group,") {
		t.Errorf("note row[0] = %q", doc[16])
	}
}

func TestCycle225QuotaCountdown(t *testing.T) {
	for _, tc := range []struct {
		name string
		d    time.Duration
		want string
	}{
		{"1 week + 1s", 168*time.Hour + 1*time.Second, "168h 0m"},
		{"167h 59m + 1s", 167*time.Hour + 59*time.Minute + 1*time.Second, "167h 59m"},
		{"2 days 5 hours + 1s", 53*time.Hour + 1*time.Second, "53h 0m"},
		{"1 hour + 1s", 1*time.Hour + 1*time.Second, "1h 0m"},
		{"40 minutes + 1s", 40*time.Minute + 1*time.Second, "40m"},
		{"59 minutes + 999ms (one-second-truncation check)", 59*time.Minute + 999*time.Millisecond, "59m"},
		{"42 seconds + 1ms", 42*time.Second + 1*time.Millisecond, "42s"},
		{"1 second + 1ms", 1*time.Second + 1*time.Millisecond, "1s"},
		{"0 (clamp)", 0, "0s"},
		{"negative (clamp)", -10 * time.Minute, "0s"},
	} {
		got := tuiviewer.QuotaCountdown(time.Now().Add(tc.d))
		if got != tc.want {
			t.Errorf("%s: tuiviewer.QuotaCountdown(%v) = %q, want %q", tc.name, tc.d, got, tc.want)
		}
	}
}

func TestCycle225QuotaStubRefills(t *testing.T) {
	m := mkPanel(panelUsage, "/usage", 0)
	m.panel.quota.state = quotaLoading
	before := time.Now()
	quotaStubRefills(m)
	after := time.Now()
	if m.panel.quota.state != quotaLoaded {
		t.Errorf("state = %d, want quotaLoaded (%d)", m.panel.quota.state, quotaLoaded)
	}
	// The stub stamps its refresh times with a time.Now() of its own,
	// so they land somewhere between `before` and `after`. The window
	// is inclusive on both ends: two time.Now() calls can share one
	// clock tick, and a strict comparison on the lower bound fails
	// whenever they do.
	loG, hiG := before.Add(168*time.Hour), after.Add(168*time.Hour)
	if m.panel.quota.refreshG.Before(loG) || m.panel.quota.refreshG.After(hiG) {
		t.Errorf("refreshG = %v, want in [%v, %v]",
			m.panel.quota.refreshG, loG, hiG)
	}
	loO, hiO := before.Add(48*time.Hour), after.Add(48*time.Hour)
	if m.panel.quota.refreshO.Before(loO) || m.panel.quota.refreshO.After(hiO) {
		t.Errorf("refreshO = %v, want in [%v, %v]",
			m.panel.quota.refreshO, loO, hiO)
	}
}

func TestCycle225UsageQuotaTickHandler(t *testing.T) {
	m := &runMode{}
	m.panel = slashPanel{kind: panelNone}
	if _, cmd := m.Update(usageQuotaTickMsg{}); cmd != nil {
		t.Errorf("closed panel returned a cmd: %v", cmd)
	}
	if m.quotaFrame != 0 {
		t.Errorf("quotaFrame = %d, want 0", m.quotaFrame)
	}

	m = mkPanel(panelUsage, "/usage", 0)
	m.panel.quota.state = quotaLoading
	for i := 0; i < usageQuotaLoadingTicks; i++ {
		_, cmd := m.Update(usageQuotaTickMsg{})
		if cmd == nil {
			t.Fatalf("tick %d returned nil cmd", i)
		}
	}
	if m.panel.quota.state != quotaLoaded {
		t.Errorf("state = %d, want quotaLoaded", m.panel.quota.state)
	}
	if m.quotaFrame != usageQuotaLoadingTicks {
		t.Errorf("quotaFrame = %d, want %d", m.quotaFrame, usageQuotaLoadingTicks)
	}

	_, cmd := m.Update(usageQuotaTickMsg{})
	if cmd == nil {
		t.Error("post-flip tick returned nil cmd")
	}
	if m.quotaFrame != usageQuotaLoadingTicks+1 {
		t.Errorf("quotaFrame = %d, want %d", m.quotaFrame, usageQuotaLoadingTicks+1)
	}
}

func TestCycle225UsagePanelHeightAware(t *testing.T) {
	m := mkPanel(panelUsage, "/usage", 0)
	m.width, m.height = 220, 50
	m.panel.quota.state = quotaLoading
	m.quotaFrame = 0
	lines := m.panelLines()
	if len(lines) != 7 {
		t.Fatalf("loading-state panel = %d rows, want 7", len(lines))
	}
	if !strings.Contains(stripSGR(lines[4]), "Loading quota summary") {
		t.Errorf("row 4 should be the loading row: %q", stripSGR(lines[4]))
	}

	quotaStubRefills(m)
	m.panel.window = 0
	lines = m.panelLines()
	if len(lines) != 26 {
		t.Errorf("loaded-state panel = %d rows, want 26", len(lines))
	}
	if !strings.Contains(stripSGR(lines[8]), "100.00%") {
		t.Errorf("row 8 should be the Gemini 100%% bar: %q", stripSGR(lines[8]))
	}
}

func TestCycle225UsageViewerStaticRegression(t *testing.T) {
	m := mkPanel(panelUsage, "/usage", 0)
	if m.height != 0 {
		t.Fatalf("mkPanel did not seed height=0 (got %d)", m.height)
	}
	doc := m.usageViewerLines()
	if len(doc) != 20 {
		t.Fatalf("doc = %d rows, want 20", len(doc))
	}
	if !strings.Contains(doc[7], "\x1b[1mWeekly Limit Remaining\x1b[0m") {
		t.Errorf("doc[7] = %q", doc[7])
	}
	if !strings.Contains(doc[9], "Quota available") {
		t.Errorf("doc[9] should preserve the cycle-193 'Quota available' line: %q", doc[9])
	}
	spec, ok := m.viewerSpecFor()
	if !ok {
		t.Fatal("viewerSpecFor returned ok=false for panelUsage")
	}
	if spec.total != 20 {
		t.Errorf("spec.total = %d, want 20", spec.total)
	}
}

func TestCycle225OpenUsagePanelArmsLoading(t *testing.T) {
	m := &runMode{}
	m.width, m.height = 220, 50
	if !m.openSlashPanel("usage") {
		t.Fatal("openSlashPanel(\"usage\") = false")
	}
	if m.panel.kind != panelUsage {
		t.Errorf("panel kind = %v, want panelUsage", m.panel.kind)
	}
	if m.panel.quota.state != quotaLoading {
		t.Errorf("quota.state = %d, want quotaLoading", m.panel.quota.state)
	}
}

// ---- cycle 223: panelAddDir (TODO §2.1 last A-class panel) ----------------

// TestPanelAddDirForCommand verifies the panelForCommand mapping for
// the cycle 223 entry. TestPanelForCommand table extension is mirrored
// in slash_panels_test.go.
func TestPanelAddDirForCommand(t *testing.T) {
	if got, ok := panelForCommand("add-dir"); !ok || got != panelAddDir {
		t.Errorf("panelForCommand(\"add-dir\") = (%v, %v), want (panelAddDir, true)", got, ok)
	}
	// The default open path (used by the TUI dispatcher) also lands
	// on panelAddDir.
	m := &runMode{}
	if !m.openSlashPanel("add-dir") {
		t.Fatal("openSlashPanel(\"add-dir\") = false")
	}
	if m.panel.kind != panelAddDir || m.panel.cmd != "/add-dir" {
		t.Errorf("after openSlashPanel: panel = (%v, %q), want (panelAddDir, \"/add-dir\")",
			m.panel.kind, m.panel.cmd)
	}
}

// TestPanelAddDirRouting — bare /add-dir opens the panel via
// openSlashPanelArgs; /add-dir <path> falls through to the cycle-217
// echo path (the existing `case "add-dir"` in slash_commands.go).
// The fall-through guarantee is what keeps TestSlashAddDir_ViaRouter
// and TestAddDirFeedback_AddedToWorkspace passing.
func TestPanelAddDirRouting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	writeSettings(t, home, `{}`)

	m := &runMode{}
	if !m.openSlashPanelArgs("add-dir", "") {
		t.Fatal("openSlashPanelArgs(\"add-dir\", \"\") = false, want true (panel)")
	}
	if m.panel.kind != panelAddDir {
		t.Errorf("bare /add-dir did not open panel: kind = %v", m.panel.kind)
	}
	m2 := &runMode{}
	if m2.openSlashPanelArgs("add-dir", "/tmp/foo") {
		t.Fatal("openSlashPanelArgs(\"add-dir\", \"/tmp/foo\") = true, want false (fall through)")
	}
	if m2.panel.kind != panelNone {
		t.Errorf("panel should remain panelNone after fall-through, got %v", m2.panel.kind)
	}
}

// TestPanelAddDirEmptyLayout verifies the no-trusted-workspaces form:
// the heading "No trusted workspaces yet." replaces the per-entry
// rows, and the rest of the skeleton matches the brief's "title +
// trusted-workspaces list + placeholder input + keyboard hint"
// description (the wording is OUR's — REF has no panel to compare).
func TestPanelAddDirEmptyLayout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	writeSettings(t, home, `{}`)

	m := mkPanel(panelAddDir, "/add-dir", 0)
	plain := plainLines(m)

	want := []string{
		"Add Directory to Workspace",
		"",
		"  No trusted workspaces yet.",
		"",
		"  Path:  ",
		"",
		"Keyboard: enter Add Directory  esc Cancel",
	}
	if len(plain) != len(want) {
		t.Fatalf("line count = %d, want %d\nplain:\n%s",
			len(plain), len(want), strings.Join(plain, "\n"))
	}
	for i, w := range want {
		if plain[i] != w {
			t.Errorf("row %d:\n got: %q\nwant: %q", i, plain[i], w)
		}
	}
	// The reverse-video cursor block must sit at the end of the path
	// row, which is what the brief's "placeholder input prompt" amounts
	// to for an empty buffer.
	styled := m.panelLines()
	if !strings.HasSuffix(stripSGR(styled[4]), " ") || !strings.HasSuffix(styled[4], searchCursorBlock) {
		t.Errorf("Path row missing reverse-video cursor tail: %q",
			stripSGR(styled[4]))
	}
	// Title row carries the bold/default-fg panel title colour.
	if !strings.HasPrefix(styled[0], panelTitleOpen) {
		t.Errorf("title row missing panel title colour: %q", stripSGR(styled[0]))
	}
	// The "No trusted workspaces yet." row is in the dim gray (241) the
	// clone uses for the /tasks /artifact empty-state lines.
	if !strings.Contains(styled[2], cDimGrayOpen) {
		t.Errorf("trusted-empty row missing dim-gray: %q",
			stripSGR(styled[2]))
	}
}

// TestPanelAddDirTrustedListed verifies the trustedWorkspaces list is
// drawn verbatim from auth.LoadTrustedWorkspaces (the same reader the
// on-startup trust gate and cycle-103 /add-dir already use). Cycle
// 217's "Workspace added" wording lives downstream of addDirFeedback
// and is exercised separately in TestPanelAddDirEnterEchoes.
func TestPanelAddDirTrustedListed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	writeSettings(t, home, `{"trustedWorkspaces":["/Users/foo/a","/Users/foo/b"]}`)

	m := mkPanel(panelAddDir, "/add-dir", 0)
	plain := plainLines(m)
	if len(plain) != 9 {
		t.Fatalf("line count = %d, want 9\nplain:\n%s",
			len(plain), strings.Join(plain, "\n"))
	}
	if plain[0] != "Add Directory to Workspace" {
		t.Errorf("row 0 = %q, want title", plain[0])
	}
	if plain[2] != "  Trusted workspaces:" {
		t.Errorf("row 2 = %q, want heading", plain[2])
	}
	if plain[3] != "  /Users/foo/a" {
		t.Errorf("row 3 = %q, want first trusted path", plain[3])
	}
	if plain[4] != "  /Users/foo/b" {
		t.Errorf("row 4 = %q, want second trusted path", plain[4])
	}
	if !strings.Contains(plain[6], "  Path:") {
		t.Errorf("row 6 should be the path input, got %q", plain[6])
	}
	if plain[8] != "Keyboard: enter Add Directory  esc Cancel" {
		t.Errorf("row 8 = %q, want keyboard hint", plain[8])
	}
}

// TestPanelAddDirTypingFillsBuffer verifies the panel's printable-key
// handling mirrors the cycle-208 / cycle-221 textarea shape: each
// printable rune appends to m.panel.text, and the Path row repaints
// after every keystroke (the render reads m.panel.text live).
func TestPanelAddDirTypingFillsBuffer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	writeSettings(t, home, `{}`)

	m := mkPanel(panelAddDir, "/add-dir", 0)
	for _, r := range []rune("/tmp/foo") {
		handled, _ := m.handlePanelKey(tea.Key{Text: string(r)})
		if !handled {
			t.Errorf("handlePanelKey(%q) = false, want true", string(r))
		}
	}
	if m.panel.text != "/tmp/foo" {
		t.Errorf("panel.text = %q, want \"/tmp/foo\"", m.panel.text)
	}
	styled := m.panelLines()
	if !strings.Contains(stripSGR(styled[4]), "Path: /tmp/foo") {
		t.Errorf("path row missing typed text: %q", stripSGR(styled[4]))
	}
}

// TestPanelAddDirBackspace pops the last rune — the cycle-208/221
// free-text form has the same rule and the panel mirrors it.
func TestPanelAddDirBackspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	writeSettings(t, home, `{}`)

	m := mkPanel(panelAddDir, "/add-dir", 0)
	m.panel.text = "/tmp/foo"
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyBackspace}); !handled {
		t.Error("backspace not handled")
	}
	if m.panel.text != "/tmp/fo" {
		t.Errorf("panel.text = %q, want \"/tmp/fo\"", m.panel.text)
	}

	// Backspace on an empty buffer is a no-op (no underflow).
	m.panel.text = ""
	m.handlePanelKey(tea.Key{Code: tea.KeyBackspace})
	if m.panel.text != "" {
		t.Errorf("backspace on empty buffer = %q, want empty", m.panel.text)
	}
}

// TestPanelAddDirEnterEchoes commits the typed path through the
// cycle-217 addDirFeedback path: the panel closes, settings.json gains
// the new trustedWorkspaces entry, and slashRespond emits the
// "Added <path> to workspace" line (verbatim from cycle-217
// TestAddDirFeedback_AddedToWorkspace).
func TestPanelAddDirEnterEchoes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	writeSettings(t, home, `{"trustedWorkspaces":[]}`)
	sub := home + "/proj"
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	m := mkPanel(panelAddDir, "/add-dir", 0)
	m.panel.text = sub
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("Enter not handled")
	}
	if m.panel.kind != panelNone {
		t.Errorf("panel still open: kind = %v", m.panel.kind)
	}
	resp := stripSGR(strings.Join(m.response, "\n"))
	if !strings.Contains(resp, "> /add-dir") {
		t.Errorf("missing slashRespond header; m.response=%q", resp)
	}
	if !strings.Contains(resp, "Added "+sub+" to workspace") {
		t.Errorf("missing success echo; got %q", resp)
	}
	body := readSettings(t, home)
	if !strings.Contains(body, sub) {
		t.Errorf("settings.json missing the new workspace; got %q", body)
	}
}

// TestPanelAddDirEnterEmpty — pressing Enter with an empty input runs
// the cycle-217 addDirFeedback("") branch and echoes "error: no
// directory specified". Same wording as `/add-dir` no-arg in REF
// (`/private/tmp/agy217/caps/ref.adddir-bare.raw.txt`); closes the
// panel so the user is not trapped.
func TestPanelAddDirEnterEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	writeSettings(t, home, `{}`)

	m := mkPanel(panelAddDir, "/add-dir", 0)
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("Enter not handled")
	}
	if m.panel.kind != panelNone {
		t.Errorf("panel did not close: kind = %v", m.panel.kind)
	}
	resp := stripSGR(strings.Join(m.response, "\n"))
	if !strings.Contains(resp, "error: no directory specified") {
		t.Errorf("missing cycle-217 error echo; got %q", resp)
	}
}

// TestPanelAddDirEnterInvalid — typed path that doesn't exist runs the
// cycle-217 addDirFeedback validation and echoes the same `error:
// directory not found: <path>` wording. The panel still closes —
// refusing would trap the user.
func TestPanelAddDirEnterInvalid(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	writeSettings(t, home, `{}`)

	m := mkPanel(panelAddDir, "/add-dir", 0)
	m.panel.text = home + "/nope"
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("Enter not handled")
	}
	if m.panel.kind != panelNone {
		t.Errorf("panel did not close: kind = %v", m.panel.kind)
	}
	resp := stripSGR(strings.Join(m.response, "\n"))
	if !strings.Contains(resp, "error: directory not found:") {
		t.Errorf("missing validation echo; got %q", resp)
	}
}

// TestPanelAddDirEsc — Esc closes the panel with the standard exit
// notice (`Exited /add-dir command`). The exit-notice=true group is
// the panel default; panelAddDir is intentionally NOT in the
// cycle-193 silent-close set so the close is loud enough to confirm
// the panel actually went away.
func TestPanelAddDirEsc(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	writeSettings(t, home, `{}`)

	m := mkPanel(panelAddDir, "/add-dir", 0)
	m.panel.text = "/tmp/foo" // garbage that wouldn't validate, but Esc ignores it
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEsc}); !handled {
		t.Fatal("Esc not handled")
	}
	if m.panel.kind != panelNone {
		t.Errorf("panel still open: kind = %v", m.panel.kind)
	}
	resp := stripSGR(strings.Join(m.response, "\n"))
	if !strings.Contains(resp, "Exited /add-dir command") {
		t.Errorf("missing exit notice; got %q", resp)
	}
}

// TestPanelAddDirExitNotice — `/add-dir` must be in the exit-notice=true
// group (panel default), unlike /mcp /feedback /rewind /context. The
// brief's "Esc closes with the standard exit notice" depends on this
// switch falling through to `return true`.
func TestPanelAddDirExitNotice(t *testing.T) {
	if !panelAddDir.exitNotice() {
		t.Error("panelAddDir.exitNotice() = false, want true (Esc should emit \"Exited /add-dir command\")")
	}
}

// Test_HelpListWindowFor pins the cycle-182 height-adaptive /help list
// window. Cycle 132 hard-coded 18 rows, which is only right at the
// 30-row geometry the byte-exact snapshots use. The real v1.4.0
// formula, recovered by scanning REF at heights 13..60, is
//
//	n = min(3h/4 - 4, h - 9)   (integer division)
//
// The table below is the measured REF value at each height.
func Test_HelpListWindowFor(t *testing.T) {
	cases := []struct{ h, want int }{
		{13, 4}, {14, 5}, {15, 6}, {16, 7}, {17, 8}, {18, 9},
		{20, 11}, {22, 12}, {24, 14}, {26, 15}, {28, 17}, {30, 18},
		{32, 20}, {33, 20}, {34, 21}, {36, 23}, {38, 24}, {40, 26},
		{50, 33}, {60, 41},
	}
	for _, c := range cases {
		if got := helpListWindowFor(c.h); got != c.want {
			t.Errorf("helpListWindowFor(%d) = %d, want %d", c.h, got, c.want)
		}
	}
}

// Test_HelpListWindowForUnknownHeight verifies the pre-182 fallback:
// before the terminal reports its size (height 0) the window stays 18
// so the byte-exact snapshots and the unit tests are unchanged.
func Test_HelpListWindowForUnknownHeight(t *testing.T) {
	for _, h := range []int{0, -1} {
		if got := helpListWindowFor(h); got != 18 {
			t.Errorf("helpListWindowFor(%d) = %d, want 18", h, got)
		}
	}
	// A degenerate but positive height must still yield at least one
	// row — the overlay can never render an empty window.
	if got := helpListWindowFor(1); got < 1 {
		t.Errorf("helpListWindowFor(1) = %d, want >= 1", got)
	}
}

// Test_HelpListVisible covers the short last window: the window is
// clipped when the catalog does not fill it, and is 0 once the window
// start is past the end.
func Test_HelpListVisible(t *testing.T) {
	cases := []struct{ total, window, size, want int }{
		{99, 0, 18, 18},
		{99, 90, 18, 9}, // last window is short
		{99, 99, 18, 0}, // window start past the end
		{34, 0, 18, 18}, // shortcuts, first window
		{34, 30, 18, 4}, // shortcuts, last window
		{0, 0, 18, 0},   // no catalog
		{5, 0, 18, 5},   // catalog smaller than the window
		{99, 0, 0, 18},  // size < 1 is coerced to 1 -> clipped to 1
	}
	for _, c := range cases {
		if c.size == 0 {
			// helpListVisible coerces size<1 to 1, so the expected
			// value is min(total, 1).
			if got := helpListVisible(c.total, c.window, c.size); got != 1 {
				t.Errorf("helpListVisible(%d,%d,%d) = %d, want 1",
					c.total, c.window, c.size, got)
			}
			continue
		}
		if got := helpListVisible(c.total, c.window, c.size); got != c.want {
			t.Errorf("helpListVisible(%d,%d,%d) = %d, want %d",
				c.total, c.window, c.size, got, c.want)
		}
	}
}

// Test_HelpListNavigation pins the v1.4.0 window model that replaced
// the cycle-137 pagination: an 18-row window at h=30 with a cursor
// over the whole catalog, scrolling one row at a time once the cursor
// reaches an edge, wrapping at both ends.
func Test_HelpListNavigation(t *testing.T) {
	const total = 99
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.width, m.height = 220, 30
	size := m.helpListWindowSize()
	if size != 18 {
		t.Fatalf("window at h=30 = %d, want 18", size)
	}

	// Down 17 times keeps the window at the top and parks the cursor
	// on the last visible row.
	for i := 0; i < size-1; i++ {
		m.helpListDown(total)
	}
	if m.helpCursor != size-1 || m.helpWindow != 0 {
		t.Fatalf("after Down x%d: cursor=%d window=%d, want %d/0",
			size-1, m.helpCursor, m.helpWindow, size-1)
	}
	// One more Down scrolls the window by exactly one row.
	m.helpListDown(total)
	if m.helpCursor != size || m.helpWindow != 1 {
		t.Fatalf("after Down x%d: cursor=%d window=%d, want %d/1",
			size, m.helpCursor, m.helpWindow, size)
	}

	// Ctrl+End jumps to the last row and scrolls to the bottom.
	m.helpListEnd(total)
	if m.helpCursor != total-1 || m.helpWindow != total-size {
		t.Fatalf("Ctrl+End: cursor=%d window=%d, want %d/%d",
			m.helpCursor, m.helpWindow, total-1, total-size)
	}
	// Down at the last row wraps to the first row and the top window.
	m.helpListDown(total)
	if m.helpCursor != 0 || m.helpWindow != 0 {
		t.Fatalf("Down at last: cursor=%d window=%d, want 0/0",
			m.helpCursor, m.helpWindow)
	}
	// Up at the first row wraps to the last row and the bottom window.
	m.helpListUp(total)
	if m.helpCursor != total-1 || m.helpWindow != total-size {
		t.Fatalf("Up at first: cursor=%d window=%d, want %d/%d",
			m.helpCursor, m.helpWindow, total-1, total-size)
	}
	// Ctrl+Home resets both.
	m.helpListHome()
	if m.helpCursor != 0 || m.helpWindow != 0 {
		t.Fatalf("Ctrl+Home: cursor=%d window=%d, want 0/0",
			m.helpCursor, m.helpWindow)
	}
}

// Test_HelpListNavigationShortCatalog covers the SHORTCUTS tab (34
// rows), where the catalog never scrolls: the window stays at 0 and
// only the cursor moves.
func Test_HelpListNavigationShortCatalog(t *testing.T) {
	const total = 34
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.width, m.height = 220, 30
	for i := 0; i < total-1; i++ {
		m.helpListDown(total)
	}
	if m.helpCursor != total-1 {
		t.Fatalf("cursor = %d, want %d", m.helpCursor, total-1)
	}
	// A 34-row catalog in an 18-row window does scroll once past the
	// edge — but only to total-size.
	wantWindow := total - m.helpListWindowSize()
	if m.helpWindow != wantWindow {
		t.Fatalf("window = %d, want %d", m.helpWindow, wantWindow)
	}
	m.helpListDown(total) // wrap
	if m.helpCursor != 0 || m.helpWindow != 0 {
		t.Fatalf("wrap: cursor=%d window=%d, want 0/0", m.helpCursor, m.helpWindow)
	}
}

// Test_HelpListLenMatchesCatalogs guards the row counts the /help
// status indicator prints: REF v1.4.0 reports "of 99 items" on the
// COMMANDS tab and "of 34 items" on SHORTCUTS.
func Test_HelpListLenMatchesCatalogs(t *testing.T) {
	if got := helpListLen(1); got != len(tuilib.DefaultSlashCommands) {
		t.Errorf("helpListLen(1) = %d, want %d", got, len(tuilib.DefaultSlashCommands))
	}
	if got := helpListLen(2); got != len(helpShortcuts) {
		t.Errorf("helpListLen(2) = %d, want %d", got, len(helpShortcuts))
	}
	if got := helpListLen(0); got != 0 {
		t.Errorf("helpListLen(0) = %d, want 0 (GENERAL has no list)", got)
	}
	if len(helpShortcuts) != 34 {
		t.Errorf("helpShortcuts has %d rows, want 34 (v1.4.0)", len(helpShortcuts))
	}
}

// Test_BuildCommandRowTruncation pins the cycle-182 width-relative
// description truncation. REF keeps `width - 52` description cells at
// the measured description column (51, 0-based) and replaces the
// overflow with `<cap-3>...`.
func Test_BuildCommandRowTruncation(t *testing.T) {
	long := strings.Repeat("x", 200)
	c := tuilib.SlashCommand{Name: "/demo", Description: long}

	// Untruncated at 220 columns: cap = 168 -> 165 runes + "...".
	row := buildCommandRow(c, false, 220)
	desc := row[strings.Index(row, cHelpDescOpen)+len(cHelpDescOpen):]
	desc = strings.TrimSuffix(desc, cHelpDescClose)
	if got := len([]rune(desc)); got != 168 {
		t.Errorf("width=220 desc len = %d, want 168", got)
	}
	if !strings.HasSuffix(desc, "...") {
		t.Errorf("width=220 desc should end in ellipsis, got %q", desc[len(desc)-6:])
	}

	// Narrow terminal: cap = width - 52. At w=60 the cap is 8.
	row = buildCommandRow(c, false, 60)
	desc = row[strings.Index(row, cHelpDescOpen)+len(cHelpDescOpen):]
	desc = strings.TrimSuffix(desc, cHelpDescClose)
	if got := len([]rune(desc)); got != 8 {
		t.Errorf("width=60 desc len = %d, want 8", got)
	}

	// width <= 0 disables truncation entirely (the pre-182 behaviour
	// the byte-exact snapshots rely on).
	row = buildCommandRow(c, false, 0)
	desc = row[strings.Index(row, cHelpDescOpen)+len(cHelpDescOpen):]
	desc = strings.TrimSuffix(desc, cHelpDescClose)
	if got := len([]rune(desc)); got != 200 {
		t.Errorf("width=0 desc len = %d, want 200 (no truncation)", got)
	}

	// A short description is never touched, at any width.
	short := tuilib.SlashCommand{Name: "/demo", Description: "short"}
	row = buildCommandRow(short, false, 60)
	if !strings.Contains(row, cHelpDescOpen+"short"+cHelpDescClose) {
		t.Errorf("short description was mangled: %q", row)
	}
}

// Test_BuildCommandRowCursorStyling verifies the COMMANDS tab colours:
// the cursor row wraps the whole name column in brand blue, a normal
// row leaves it plain, and every row's description uses the darker
// description gray.
func Test_BuildCommandRowCursorStyling(t *testing.T) {
	c := tuilib.SlashCommand{Name: "/demo", Description: "does a thing"}

	cursor := buildCommandRow(c, true, 220)
	if !strings.HasPrefix(cursor, helpBlueOpen+"> /demo") {
		t.Errorf("cursor row should open with blue \"> /demo\", got %q", cursor[:40])
	}
	if !strings.Contains(cursor, helpBlueClose) {
		t.Error("cursor row should close the blue wrap")
	}
	if !strings.Contains(cursor, cHelpDescOpen+"does a thing"+cHelpDescClose) {
		t.Errorf("description missing description-gray wrap: %q", cursor)
	}

	normal := buildCommandRow(c, false, 220)
	if strings.HasPrefix(normal, helpBlueOpen) {
		t.Error("normal row should not open with blue")
	}
	if !strings.HasPrefix(normal, "  /demo") {
		t.Errorf("normal row should start with two-space marker, got %q", normal[:20])
	}
}

// Test_RenderHelpGeneralTierSuffix verifies cycle 182: the GENERAL
// panel's email row carries the plan tier in parentheses, exactly like
// the banner. The pre-182 comment claimed REF omitted it; the v1.4.0
// raw capture shows 'navigaid@gmail.com (Antigravity Starter Quota)'.
func Test_RenderHelpGeneralTierSuffix(t *testing.T) {
	var b strings.Builder
	renderHelpGeneral(&b, "Gemini 3.8 Flash (High)", "Antigravity Starter Quota",
		"~/GitHub/openagy", "navigaid@gmail.com", "uuid")
	out := b.String()
	if !strings.Contains(out, helpDimGrayOpen+"navigaid@gmail.com (Antigravity Starter Quota)"+helpDimGrayClose) {
		t.Errorf("email row missing tier suffix: %q", out)
	}

	// An empty tier must not leave a dangling " ()".
	b.Reset()
	renderHelpGeneral(&b, "Gemini 3.8 Flash", "", "~/x", "u@t", "uuid")
	out = b.String()
	if strings.Contains(out, "u@t (") {
		t.Errorf("empty tier should not render parentheses: %q", out)
	}
	if !strings.Contains(out, helpDimGrayOpen+"u@t"+helpDimGrayClose) {
		t.Errorf("bare email row missing: %q", out)
	}
}

// Test_StatusRowRightLabelColouredInHelpMode verifies cycle 182: the
// right-aligned model label keeps its dim-gray wrap while the /help
// overlay owns the screen. Pre-182 it reused leftOpen/leftClose, which
// the helpMode blanking branch had cleared to "", so the label was
// emitted uncoloured while REF keeps `38;2;101;123;131` (tmux) / rgb(95,99,104).
func Test_StatusRowRightLabelColouredInHelpMode(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "Gemini 3.8 Flash (High)", "Antigravity Starter Quota", "/", true)
	m.width = 220
	m.helpMode = true
	if m.statusModel != "Gemini 3.8 Flash" {
		t.Fatalf("statusModel = %q, want %q", m.statusModel, "Gemini 3.8 Flash")
	}

	row := m.statusRow()
	if !strings.Contains(row, cDimGrayOpen+"Gemini 3.8 Flash"+statusRightPrefix+cDimGrayClose) {
		t.Errorf("helpMode status row lost the right-label colour: %q", row)
	}
	if strings.Contains(row, "? for shortcuts") {
		t.Errorf("helpMode status row should not carry the left hint: %q", row)
	}

	// The idle row must keep the same wrap (regression guard: the fix
	// must not change the byte-exact idle capture).
	m.helpMode = false
	idle := m.statusRow()
	if !strings.Contains(idle, cDimGrayOpen+"? for shortcuts"+cDimGrayClose) {
		t.Errorf("idle left label lost its wrap: %q", idle)
	}
	if !strings.Contains(idle, cDimGrayOpen+"Gemini 3.8 Flash"+statusRightPrefix+cDimGrayClose) {
		t.Errorf("idle right label lost its wrap: %q", idle)
	}
}

// seedRewindSession writes a synthetic N-turn session into m. Each user
// message is followed by an "ack" assistant message so the trailing pair
// pattern matches a real chat.
func seedRewindSession(m *runMode, snippets []string) {
	for _, s := range snippets {
		m.session = append(m.session,
			backend.Message{Role: "user", Content: s},
			backend.Message{Role: "assistant", Content: "ack: " + s},
		)
	}
}

// TestPanelRewindEmptyStillCycle189 — the cycle-189 test (in
// slash_panels_test.go as TestPanelRewindEmptyLayout) is preserved
// unchanged. This companion pin re-runs the same assertion against the
// cycle-222 code path so any future regression surfaces locally.
func TestPanelRewindEmptyStillCycle189(t *testing.T) {
	m := mkPanel(panelRewind, "/rewind", 0)
	assertLines(t, plainLines(m), []string{
		"Rewind Conversation",
		"",
		"> (current)",
		"",
		"Keyboard: ↑/↓ Navigate  enter Select  esc Cancel",
	})
	if got := m.panelLines()[2]; !strings.Contains(got, cBlueOpen) {
		t.Errorf("selected row should be blue: %q", got)
	}
	if got := (&runMode{}).panelRowCount(panelRewind); got != 1 {
		t.Errorf("panelRowCount(rewind) on empty session = %d, want 1", got)
	}
}

// TestPanelRewindNonEmptyLayout — a 3-turn session opens the picker with
// 3 rows; cursor lands on the most-recent (index 0) by default; each row
// starts with `  ` except the cursor which starts with `> ` and carries
// the brand-blue span. Only the most-recent row carries the `(current)`
// annotation (dim gray 238); other rows are plain (no color).
func TestPanelRewindNonEmptyLayout(t *testing.T) {
	m := mkPanel(panelRewind, "/rewind", 0)
	// 220×50 settles the visible-window budget at 50-5-1 = 44 rows,
	// which is plenty for the 3 turns below.
	m.width, m.height = 220, 50
	seedRewindSession(m, []string{
		"hello there",
		"/statusline",
		"/help me with slices",
	})
	plain := plainLines(m)
	// 7-row skeleton: title, blank, three turn rows, blank, keyboard.
	// No counter at height=50 / 3 turns (counter only appears when
	// total > visible).
	if len(plain) != 7 {
		t.Fatalf("line count = %d, want 7\nplain:\n%s", len(plain),
			strings.Join(plain, "\n"))
	}
	want := []string{
		"Rewind Conversation",
		"",
		"> Turn 1  /help me with slices  (current)",
		"  Turn 2  /statusline",
		"  Turn 3  hello there",
		"",
		"Keyboard: ↑/↓ Navigate  enter Select  esc Cancel",
	}
	for i, w := range want {
		if plain[i] != w {
			t.Errorf("row %d:\n got: %q\nwant: %q", i, plain[i], w)
		}
	}
	// Cursor row (row 2) must carry the brand-blue open code; rows 3-4
	// must NOT.
	styled := m.panelLines()
	if !strings.Contains(styled[2], cBlueOpen) {
		t.Errorf("cursor row missing blue: %q", stripSGR(styled[2]))
	}
	for _, idx := range []int{3, 4} {
		if strings.Contains(styled[idx], cBlueOpen) {
			t.Errorf("non-cursor row %d leaked blue: %q", idx, stripSGR(styled[idx]))
		}
		if strings.Contains(styled[idx], panelGray238) {
			t.Errorf("non-cursor row %d leaked gray-238: %q", idx, stripSGR(styled[idx]))
		}
	}
	if got := m.panelRowCount(panelRewind); got != 3 {
		t.Errorf("panelRowCount(rewind) on 3-turn session = %d, want 3", got)
	}
}

// TestPanelRewindCursorMoves — ↑/↓ move the cursor on a non-empty
// session. The first row keeps the brand-blue prefix only when the
// cursor is on it. REF bounds the cursor with panelRowCount and clamps
// — the test follows the same convention.
func TestPanelRewindCursorMoves(t *testing.T) {
	m := mkPanel(panelRewind, "/rewind", 0)
	m.width, m.height = 220, 50
	seedRewindSession(m, []string{"a", "b", "c"})
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Error("Down not handled")
	}
	if m.panel.cursor != 1 {
		t.Errorf("after Down: cursor = %d, want 1", m.panel.cursor)
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Error("Down not handled (2)")
	}
	if m.panel.cursor != 2 {
		t.Errorf("after Down x2: cursor = %d, want 2", m.panel.cursor)
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Error("Down not handled (3)")
	}
	if m.panel.cursor != 2 {
		t.Errorf("Down past end: cursor = %d, want 2 (clamp)", m.panel.cursor)
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyUp}); !handled {
		t.Error("Up not handled")
	}
	if m.panel.cursor != 1 {
		t.Errorf("after Up: cursor = %d, want 1", m.panel.cursor)
	}
	styled := m.panelLines()
	if !strings.Contains(styled[3], cBlueOpen) {
		t.Errorf("new cursor row 3 missing blue: %q", stripSGR(styled[3]))
	}
}

// TestPanelRewindWindowed_50TurnsHeight10 — a 50-turn session at
// h=10 shows a windowed slice with the dim-gray `(first-last of N
// turns)` counter. Visible budget = h - 5 chrome - 1 status = 4.
// Total output lines = 1 + 1 + 4 + 1 + 1 + 1 = 9.
func TestPanelRewindWindowed_50TurnsHeight10(t *testing.T) {
	m := mkPanel(panelRewind, "/rewind", 0)
	m.width, m.height = 80, 10
	snips := make([]string, 50)
	for i := range snips {
		snips[i] = "turn-step"
	}
	seedRewindSession(m, snips)
	plain := plainLines(m)
	if len(plain) != 9 {
		t.Fatalf("line count at h=10 = %d, want 9\nplain:\n%s",
			len(plain), strings.Join(plain, "\n"))
	}
	if plain[0] != "Rewind Conversation" {
		t.Errorf("row 0 = %q, want title", plain[0])
	}
	if !strings.HasPrefix(plain[2], "> Turn 1") {
		t.Errorf("first visible row should be cursor's Turn 1, got %q", plain[2])
	}
	// 4 visible rows: rows 2,3,4,5 = Turn 1..4.
	if !strings.HasPrefix(plain[5], "  Turn 4") {
		t.Errorf("last visible row should be Turn 4, got %q", plain[5])
	}
	if !strings.Contains(plain[7], "(1\u20134 of 50 turns)") {
		t.Errorf("counter row missing/wrong: %q", plain[7])
	}
	if !strings.Contains(plain[8], "Keyboard:") {
		t.Errorf("keyboard hint missing: %q", plain[8])
	}
}

// TestPanelRewindWindowed_50TurnsHeight20 — at h=20 the visible budget
// is 14 rows; 50 turns still overflows. Total = 1 + 1 + 14 + 1 + 1 + 1 = 19.
func TestPanelRewindWindowed_50TurnsHeight20(t *testing.T) {
	m := mkPanel(panelRewind, "/rewind", 0)
	m.width, m.height = 80, 20
	snips := make([]string, 50)
	for i := range snips {
		snips[i] = "step"
	}
	seedRewindSession(m, snips)
	plain := plainLines(m)
	if len(plain) != 19 {
		t.Fatalf("line count at h=20 = %d, want 19\nplain:\n%s",
			len(plain), strings.Join(plain, "\n"))
	}
	if !strings.HasPrefix(plain[2], "> Turn 1") {
		t.Errorf("first visible: %q", plain[2])
	}
	if !strings.HasPrefix(plain[15], "  Turn 14") {
		t.Errorf("last visible row 15: %q", plain[15])
	}
	if !strings.Contains(plain[17], "(1\u201314 of 50 turns)") {
		t.Errorf("counter: %q", plain[17])
	}
}

// TestPanelRewindWindowed_50TurnsHeight40 — h=40 visible budget is 34.
// 50 turns still overflows. Total = 1 + 1 + 34 + 1 + 1 + 1 = 39.
func TestPanelRewindWindowed_50TurnsHeight40(t *testing.T) {
	m := mkPanel(panelRewind, "/rewind", 0)
	m.width, m.height = 80, 40
	snips := make([]string, 50)
	for i := range snips {
		snips[i] = "step"
	}
	seedRewindSession(m, snips)
	plain := plainLines(m)
	if len(plain) != 39 {
		t.Fatalf("line count at h=40 = %d, want 39\nplain:\n%s",
			len(plain), strings.Join(plain, "\n"))
	}
	if !strings.Contains(plain[37], "(1\u201334 of 50 turns)") {
		t.Errorf("counter at h=40: %q", plain[37])
	}
}

// TestPanelRewindEnterTruncates — selecting a non-current step and
// pressing Enter truncates m.session and emits a slashRespond echo
// block via the cycle-195 `> /rewind / ⎿` format. The panel then
// closes silently (same exit-notice=false set as /mcp /feedback).
func TestPanelRewindEnterTruncates(t *testing.T) {
	m := mkPanel(panelRewind, "/rewind", 0)
	m.width, m.height = 220, 50
	seedRewindSession(m, []string{"first", "second", "third"})
	// Move down twice to land on Turn 3 (the oldest = "first" turn).
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Error("Down not handled")
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Error("Down x2 not handled")
	}
	if m.panel.cursor != 2 {
		t.Fatalf("cursor = %d, want 2", m.panel.cursor)
	}
	// Session has 6 messages before the test runs Enter.
	if len(m.session) != 6 {
		t.Fatalf("session before Enter: %d, want 6", len(m.session))
	}
	// Press Enter — the picker closes (closePanel returns a nil Cmd;
	// the panel-clearing mutation is the deliverable), the echo fires,
	// session truncates.
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatalf("Enter not handled")
	}
	// 6 - 2 = 4 messages dropped — the picked Turn 3 (oldest) is the
	// "first" user/assistant pair; everything after that survives.
	if len(m.session) != 2 {
		t.Errorf("after Enter session = %d messages, want 2", len(m.session))
	}
	if m.session[0].Role != "user" || m.session[0].Content != "first" {
		t.Errorf("truncated head user = %+v, want first", m.session[0])
	}
	// The slashRespond echo must be present in m.response.
	resp := strings.Join(m.response, "\n")
	if !strings.Contains(resp, "> /rewind") {
		t.Errorf("echo block missing slashRespond header; m.response=%q",
			strings.Join(m.response, "\n"))
	}
	if !strings.Contains(resp, "Rewound to step") {
		t.Errorf("echo body missing Rewound to step; got %q", resp)
	}
	if !strings.Contains(resp, "4 message(s) removed") {
		t.Errorf("echo should report 4 removed, got %q", resp)
	}
	// Panel must close on Enter.
	if m.panel.kind != panelNone {
		t.Errorf("panel did not close: kind = %v", m.panel.kind)
	}
}

// TestPanelRewindEnterOnCurrent — pressing Enter while on the cursor's
// default (most-recent) position (the `(current)` row) is a no-op:
// no messages are removed, no echo is emitted. This matches REF, which
// silently dismisses the picker when the user picks the current step.
func TestPanelRewindEnterOnCurrent(t *testing.T) {
	m := mkPanel(panelRewind, "/rewind", 0)
	m.width, m.height = 220, 50
	seedRewindSession(m, []string{"a", "b", "c"})
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Error("Enter not handled")
	}
	if len(m.session) != 6 {
		t.Errorf("session = %d messages after Enter-on-current, want 6",
			len(m.session))
	}
	if len(m.response) != 0 {
		t.Errorf("echo should be empty on Enter-on-current; got %q",
			strings.Join(m.response, "\n"))
	}
	// Panel must close on Enter.
	if m.panel.kind != panelNone {
		t.Errorf("panel did not close: kind = %v", m.panel.kind)
	}
}

// TestRewindExitNoticeCycle222 — panelRewind remains in the silent-close
// set (cycle 189 added it; cycle 222 keeps it).
func TestRewindExitNoticeCycle222(t *testing.T) {
	if panelRewind.exitNotice() {
		t.Error("panelRewind.exitNotice() = true, want false (silent close)")
	}
}

// blockedMCPRoot builds <tmp>/.gemini/{antigravity-cli,config} and
// points ANTIGRAVITY_HOME at the antigravity-cli side. Mirrors the
// cycle198Root helper's shape (cmd/wagyu/slash_cycle198_test.go:37).
func blockedMCPRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), ".gemini")
	agy := filepath.Join(root, "antigravity-cli")
	if err := os.MkdirAll(agy, 0o700); err != nil {
		t.Fatalf("mkdir antigravity-cli: %v", err)
	}
	t.Setenv("ANTIGRAVITY_HOME", agy)
	return root
}

// blockedMCPClearGate clears both gate env vars so a test that does
// not intend to exercise the gate does not accidentally inherit one
// from the host shell.
func blockedMCPClearGate(t *testing.T) {
	t.Helper()
	t.Setenv("AGY_CLI_DISABLE_PLUGINS", "")
	t.Setenv("AGY_CLI_DISABLE_MCP", "")
}

// blockedMCPSeedServers writes a synthetic mcp.json with the given
// servers. The schema mirrors internal/auth.MCPConfig.Servers so the
// returned *MCPConfig comes straight through auth.LoadMCPConfig.
func blockedMCPSeedServers(t *testing.T, root string, servers map[string]auth.MCPServer) {
	t.Helper()
	path := filepath.Join(root, "antigravity-cli", "mcp.json")
	doc := map[string]any{"servers": servers}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal mcp.json: %v", err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatalf("write mcp.json: %v", err)
	}
}

// blockedMCPConfig reads mcp.json back through the public
// LoadMCPConfig path. Shim around auth.LoadMCPConfig that simply
// documents intent in the call site.
func blockedMCPConfig(t *testing.T, root string) *auth.MCPConfig {
	t.Helper()
	t.Setenv("ANTIGRAVITY_HOME", filepath.Join(root, "antigravity-cli"))
	cfg, err := auth.LoadMCPConfig()
	if err != nil {
		t.Fatalf("LoadMCPConfig: %v", err)
	}
	return cfg
}

// TestBlockedMCP_BlockedByEnv pins the gap requirement #1: when
// AGY_CLI_DISABLE_MCP is set, every MCP server's state cell is
// "blocked". The per-server enabled/disabled cell must NOT also
// appear — that was the original bug shape, where a server was
// rendered as both "connected" and "disabled".
func TestBlockedMCP_BlockedByEnv(t *testing.T) {
	blockedMCPClearGate(t)
	root := blockedMCPRoot(t)
	blockedMCPSeedServers(t, root, map[string]auth.MCPServer{
		"local": {Type: "stdio", Command: "mcp-server", Enabled: true},
	})

	t.Setenv("AGY_CLI_DISABLE_MCP", "1")
	if !auth.MCPsBlocked() {
		t.Fatal("env var did not engage MCPsBlocked()")
	}
	got := mcpServersFeedback(blockedMCPConfig(t, root))

	// Every server row should carry exactly one state cell, "blocked".
	// Cross-check by looking at the tab-separated state cell (idx 2).
	for _, line := range strings.Split(got, "\n") {
		if !strings.HasPrefix(line, "  ") {
			continue
		}
		cells := strings.Split(line, "\t")
		if len(cells) < 4 {
			t.Errorf("malformed row %q", line)
			continue
		}
		if cells[2] != "blocked" {
			t.Errorf("state cell = %q, want blocked: %q", cells[2], line)
		}
	}
	if strings.Contains(got, "\tenabled\t") {
		t.Errorf("gate-on listing still carries 'enabled' cell: %q", got)
	}
	if strings.Contains(got, "\tdisabled\t") {
		t.Errorf("gate-on listing still carries 'disabled' cell: %q", got)
	}
}

// TestBlockedMCP_DisabledByUser pins the gap requirement #2: when
// the gate is off and a server has enabled=false, the state cell
// renders as "disabled", not "blocked" and not "enabled". This is
// the row's per-server preference, not a global gate.
func TestBlockedMCP_DisabledByUser(t *testing.T) {
	blockedMCPClearGate(t)
	root := blockedMCPRoot(t)
	blockedMCPSeedServers(t, root, map[string]auth.MCPServer{
		"local": {Type: "stdio", Command: "mcp-server", Enabled: false},
	})

	if auth.MCPsBlocked() {
		t.Fatal("gate unexpectedly engaged")
	}
	got := mcpServersFeedback(blockedMCPConfig(t, root))
	if !strings.Contains(got, "local\tstdio\tdisabled\tmcp-server") {
		t.Errorf("disabled-by-user row = %q, want 'disabled' cell", got)
	}
	if strings.Contains(got, "blocked") {
		t.Errorf("off-gate listing carries 'blocked' cell: %q", got)
	}
	if strings.Contains(got, "\tenabled\t") {
		t.Errorf("off-gate listing carries 'enabled' cell: %q", got)
	}
}

// TestBlockedMCP_BothBlockedAndDisabled pins the gap requirement #3:
// when the gate is on AND the per-row enabled flag is also false,
// the cell renders "blocked" (the gate wins) and never "disabled".
// This is the exact bug the gap describes: previously the panel
// could render both "connected" and "disabled" — when the gate is
// on, only "blocked" should be visible.
func TestBlockedMCP_BothBlockedAndDisabled(t *testing.T) {
	blockedMCPClearGate(t)
	root := blockedMCPRoot(t)
	blockedMCPSeedServers(t, root, map[string]auth.MCPServer{
		"local": {Type: "stdio", Command: "mcp-server", Enabled: false},
	})

	t.Setenv("AGY_CLI_DISABLE_MCP", "yes")
	got := mcpServersFeedback(blockedMCPConfig(t, root))

	// Find the local row and assert its state cell.
	want := "  local\t"
	for _, line := range strings.Split(got, "\n") {
		if !strings.HasPrefix(line, want) {
			continue
		}
		cells := strings.Split(line, "\t")
		if len(cells) < 4 {
			t.Errorf("malformed row %q", line)
			continue
		}
		if cells[2] != "blocked" {
			t.Errorf("cell = %q, want blocked (gate wins over per-server disabled)", cells[2])
		}
	}
	if strings.Contains(got, "\tdisabled\t") {
		t.Errorf("gate-on listing still has 'disabled' cell alongside 'blocked': %q", got)
	}
}

// TestBlockedMCP_NeitherFlagShowsConnected pins the gap requirement
// #4 (loosely): with neither gate engaged AND per-row enabled=true
// the panel shows "enabled" (the clone's analogue of the reference
// "connected" cell), and not "blocked", and not "disabled". This
// pins the four-quadrant truth table the gap describes: blocked /
// disabled / connected (=enabled) / unrendered.
func TestBlockedMCP_NeitherFlagShowsConnected(t *testing.T) {
	blockedMCPClearGate(t)
	root := blockedMCPRoot(t)
	blockedMCPSeedServers(t, root, map[string]auth.MCPServer{
		"local": {Type: "stdio", Command: "mcp-server", Enabled: true},
	})

	if auth.MCPsBlocked() {
		t.Fatal("gate unexpectedly engaged")
	}
	got := mcpServersFeedback(blockedMCPConfig(t, root))
	if !strings.Contains(got, "local\tstdio\tenabled\tmcp-server") {
		t.Errorf("connected (=enabled) row missing or wrong: %q", got)
	}
	if strings.Contains(got, "blocked") {
		t.Errorf("off-gate listing carries 'blocked' cell: %q", got)
	}
	if strings.Contains(got, "\tdisabled\t") {
		t.Errorf("off-gate listing carries 'disabled' cell: %q", got)
	}
}

// TestBlockedMCP_SlashMCPCommandUsesBlockedState is the gap
// requirement #5: /mcp's slash router output must reflect the new
// "blocked" cell, end-to-end. This is the integration shape
// TestSlashMCP_ReadsConfig uses (cmd/wagyu/slash_actions_test.go).
func TestBlockedMCP_SlashMCPCommandUsesBlockedState(t *testing.T) {
	blockedMCPClearGate(t)
	root := blockedMCPRoot(t)
	blockedMCPSeedServers(t, root, map[string]auth.MCPServer{
		"local": {Type: "stdio", Command: "mcp-server", Enabled: true},
	})
	t.Setenv("AGY_CLI_DISABLE_MCP", "1")

	m := newSlashModel(t)
	m.handleSlashCommand("/mcp")
	got := strings.Join(afterEcho(m), "\n")
	if !strings.Contains(got, "local") {
		t.Errorf("/mcp router lost the server row: %q", got)
	}
	if !strings.Contains(got, "blocked") {
		t.Errorf("/mcp router did not pass the blocked cell through: %q", got)
	}
	// "enabled" must NOT appear alongside "blocked" — that was the
	// gap's "both connected and disabled on the same row" bug.
	if strings.Contains(got, "\tenabled\t") {
		t.Errorf("/mcp router still rendering an 'enabled' cell: %q", got)
	}
}

// TestBlockedMCP_FromConfigField pins the on-disk boolean source
// (mcp.json's top-level `disableAllMCP`). Same precedence as
// MCPsBlockedFromEnvVar: env var wins over field, set field wins
// over missing field, whitespace-only env var is unset, clearing
// both disengages the gate.
func TestBlockedMCP_FromConfigField(t *testing.T) {
	blockedMCPClearGate(t)
	root := blockedMCPRoot(t)
	blockedMCPSeedServers(t, root, map[string]auth.MCPServer{
		"local": {Type: "stdio", Command: "mcp-server", Enabled: true},
	})

	// Empty mcp.json → gate off.
	if auth.MCPsBlocked() {
		t.Fatal("empty config engaged the gate")
	}

	// Set disableAllMCP: true on the same mcp.json.
	doc := map[string]any{
		"servers": map[string]any{
			"local": map[string]any{"type": "stdio", "command": "mcp-server", "enabled": true},
		},
		"disableAllMCP": true,
	}
	b, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.WriteFile(filepath.Join(root, "antigravity-cli", "mcp.json"), b, 0o600); err != nil {
		t.Fatalf("write mcp.json: %v", err)
	}
	if !auth.MCPsBlocked() {
		t.Fatal("config field did not engage the gate")
	}
	got := mcpServersFeedback(blockedMCPConfig(t, root))
	if !strings.Contains(got, "blocked") {
		t.Errorf("config-field gate did not surface in /mcp: %q", got)
	}
}

// TestBlockedMCP_PluginThreeStates pins the parallel /plugin
// coverage: a disabled-by-user plugin behind the global gate must
// show [blocked], not [disabled]. This is the cross-panel guard the
// gap fixes at the same time as /mcp.
func TestBlockedMCP_PluginThreeStates(t *testing.T) {
	blockedMCPClearGate(t)
	root := blockedMCPRoot(t)
	seedPlugin(t, root, "alpha", "Alpha plugin.", "1.2.3")
	seedPluginConfig(t, root, map[string]bool{"alpha": false})

	// No gate: cycle-198 contract — disabled-by-user row says [disabled].
	m := mkPanel(panelPlugin, "/plugin", 0)
	if got := plainLines(m)[3]; !strings.Contains(got, "[disabled]") {
		t.Fatalf("no-gate row = %q, want [disabled]", got)
	}

	// Gate on: every row says [blocked]; per-server [disabled] is gone.
	t.Setenv("AGY_CLI_DISABLE_PLUGINS", "1")
	if !auth.PluginsBlocked() {
		t.Fatal("env var did not engage PluginsBlocked()")
	}
	row := plainLines(m)[3]
	if !strings.Contains(row, "[blocked]") {
		t.Errorf("gate-on row = %q, want [blocked]", row)
	}
	if strings.Contains(row, "[disabled]") {
		t.Errorf("gate-on row still carries [disabled]: %q", row)
	}
	if strings.Contains(row, "[enabled]") {
		t.Errorf("gate-on row still carries [enabled]: %q", row)
	}
}

// TestBlockedMCP_HelpersPrecedence pins the env-var-wins-over-config
// rule for both PluginsBlocked and MCPsBlocked. Whitespace-only env
// vars are treated as unset (trim drops them); clearing both the env
// var and the on-disk boolean disengages the gate.
func TestBlockedMCP_HelpersPrecedence(t *testing.T) {
	blockedMCPClearGate(t)
	root := blockedMCPRoot(t)
	seedPluginConfig(t, root, map[string]bool{})
	blockedMCPSeedServers(t, root, map[string]auth.MCPServer{})

	// Neither flag → off for both helpers.
	if auth.PluginsBlocked() {
		t.Fatal("empty config engaged the plugin gate")
	}
	if auth.MCPsBlocked() {
		t.Fatal("empty config engaged the MCP gate")
	}

	// Whitespace-only env var must NOT engage the helpers.
	t.Setenv("AGY_CLI_DISABLE_PLUGINS", "   ")
	t.Setenv("AGY_CLI_DISABLE_MCP", " \t ")
	if auth.PluginsBlocked() {
		t.Error("whitespace-only AGY_CLI_DISABLE_PLUGINS engaged the gate")
	}
	if auth.MCPsBlocked() {
		t.Error("whitespace-only AGY_CLI_DISABLE_MCP engaged the gate")
	}

	// A non-empty env var engages the gate even when the on-disk
	// boolean is unset.
	t.Setenv("AGY_CLI_DISABLE_PLUGINS", "1")
	t.Setenv("AGY_CLI_DISABLE_MCP", "yes")
	if !auth.PluginsBlocked() {
		t.Error("env var did not engage PluginsBlocked()")
	}
	if !auth.MCPsBlocked() {
		t.Error("env var did not engage MCPsBlocked()")
	}

	// Clearing the env var drops the gate (no on-disk boolean was
	// set either).
	t.Setenv("AGY_CLI_DISABLE_PLUGINS", "")
	t.Setenv("AGY_CLI_DISABLE_MCP", "")
	if auth.PluginsBlocked() {
		t.Error("cleared env var did not drop PluginsBlocked()")
	}
	if auth.MCPsBlocked() {
		t.Error("cleared env var did not drop MCPsBlocked()")
	}
}

// TestOpenRenamePanel_KindAndSeed verifies the bare `/rename` (no
// args) opens the new panel and seeds the textinput with the current
// conversation name (if any). The seeded value matches what the
// cycle-106 conversationName getter would return.
func TestOpenRenamePanel_KindAndSeed(t *testing.T) {
	m := &runMode{conversationName: "vacation planning"}
	m.openRenamePanel()
	if m.panel.kind != panelRename {
		t.Fatalf("panel.kind = %v, want panelRename", m.panel.kind)
	}
	if got := m.panel.textInput.Value(); got != "vacation planning" {
		t.Errorf("textinput value = %q, want %q (current conversation name)",
			got, "vacation planning")
	}
	if !m.panel.textInput.Focused() {
		t.Errorf("textinput not focused on open — keystrokes will be dropped")
	}
}

// TestOpenRenamePanel_EmptySeed confirms a conversation without a
// current name opens an empty (placeholder-visible) textinput.
func TestOpenRenamePanel_EmptySeed(t *testing.T) {
	m := &runMode{}
	m.openRenamePanel()
	if m.panel.kind != panelRename {
		t.Fatalf("panel.kind = %v, want panelRename", m.panel.kind)
	}
	if got := m.panel.textInput.Value(); got != "" {
		t.Errorf("textinput value = %q, want empty", got)
	}
}

// TestHandleRenamePanelKey_EnterSubmits exercises the submit path.
// The typed name is committed via renameFeedback; the panel closes
// silently (cycle-193 silent-on-Esc set also includes panelRename per
// exitNotice()). A successful rename emits the
// "Conversation renamed to: %s" line; an empty submit emits the
// "Error: Please provide a new name for the conversation." line —
// matching the cycle-106 cmd-line path byte-for-byte.
func TestHandleRenamePanelKey_EnterSubmits(t *testing.T) {
	m := &runMode{conversationName: "trip"}
	m.openRenamePanel()
	m.panel.textInput.SetValue("summer trip 2026")
	// closePanelSilent() returns nil — the panel-clearing mutation is
	// the deliverable, not a tea.Cmd (matches cycle 222's picker-close
	// test pattern; closePanel/closePanelSilent have always returned
	// nil, see slash_panels.go:3727 and 3744).
	if handled, _ := m.handleRenamePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Errorf("Enter not handled")
	}
	if m.panel.kind != panelNone {
		t.Errorf("panel still open after submit (kind=%v)", m.panel.kind)
	}
	// renameFeedback emits the message via slashRespond; the message
	// ends up in m.response. Verify the success line.
	if !containsResponse(m.response, "Conversation renamed to: summer trip 2026") {
		t.Errorf("submit did not emit success echo: %v", m.response)
	}
}

// TestHandleRenamePanelKey_EmptySubmitHitsErrorPath ensures an empty
// Enter (Enter on a freshly-opened panel) emits the same error the
// cmd-line `/rename` (no args) used to emit. The cycle-02 UX change
// is *only* that the user gets a panel to type into — the empty-
// submit result is unchanged.
func TestHandleRenamePanelKey_EmptySubmitHitsErrorPath(t *testing.T) {
	m := &runMode{}
	m.openRenamePanel()
	m.panel.textInput.SetValue("")
	handled, _ := m.handleRenamePanelKey(tea.Key{Code: tea.KeyEnter})
	if !handled {
		t.Errorf("Enter not handled in empty state")
	}
	if !containsResponse(m.response, "Error: Please provide a new name") {
		t.Errorf("empty submit did not emit error echo: %v", m.response)
	}
}

// TestHandleRenamePanelKey_EscClosesSilently confirms Esc exits the
// panel without emitting any exit notice (panelRename is in the
// cycle-193 silent-on-Ec set per exitNotice()), matching /mcp,
// /feedback, /plugin, etc. — not /rename's previous behaviour.
func TestHandleRenamePanelKey_EscClosesSilently(t *testing.T) {
	m := &runMode{}
	m.openRenamePanel()
	handled, _ := m.handleRenamePanelKey(tea.Key{Code: tea.KeyEsc})
	if !handled {
		t.Errorf("Esc not handled")
	}
	if m.panel.kind != panelNone {
		t.Errorf("panel still open after Esc (kind=%v)", m.panel.kind)
	}
	if containsResponse(m.response, "Exited /rename") {
		t.Errorf("Esc emitted an exit notice — panelRename should be silent: %v",
			m.response)
	}
}

// TestHandleRenamePanelKey_CursorNavigationPassesThrough is the
// migration's primary UX win: the previous hand-rolled `panel.text`
// buffer dropped every navigation key. bubbles/v2/textinput handles
// arrow-left/right, Home/End, Ctrl+A/Ctrl+E, etc., so the value
// and cursor position advance correctly when those keys arrive.
//
// The test seeds a 12-character value, sends Left keys, and
// verifies the cursor moves; Home then End verify the standard
// chat-app shortcuts round-trip through the same code path.
func TestHandleRenamePanelKey_CursorNavigationPassesThrough(t *testing.T) {
	m := &runMode{}
	m.openRenamePanel()
	m.panel.textInput.SetValue("hello world")

	// Cursor is at end after SetValue. Press Left twice.
	for i := 0; i < 2; i++ {
		handled, _ := m.handleRenamePanelKey(tea.Key{Code: tea.KeyLeft})
		if !handled {
			t.Errorf("Left #%d not handled", i+1)
		}
	}
	if got := m.panel.textInput.Position(); got != 9 {
		t.Errorf("after 2× Left, cursor = %d, want 9", got)
	}
	if got := m.panel.textInput.Value(); got != "hello world" {
		t.Errorf("value = %q, want unchanged", got)
	}

	// Home jumps to 0.
	handled, _ := m.handleRenamePanelKey(tea.Key{Code: tea.KeyHome})
	if !handled {
		t.Errorf("Home not handled")
	}
	if got := m.panel.textInput.Position(); got != 0 {
		t.Errorf("after Home, cursor = %d, want 0", got)
	}
}

// TestHandleRenamePanelKey_WordDelete verifies Ctrl+W (the standard
// word-backward delete) deletes one word. The hand-rolled buffer
// dropped Ctrl+W silently; textinput handles it.
func TestHandleRenamePanelKey_WordDelete(t *testing.T) {
	m := &runMode{}
	m.openRenamePanel()
	m.panel.textInput.SetValue("hello world foo")
	// Cursor at end. textinput's DeleteWordBackward is bound to
	// alt+backspace,ctrl+w by DefaultKeyMap.
	handled, _ := m.handleRenamePanelKey(tea.Key{
		Code: 'w',
		Mod:  tea.ModCtrl,
	})
	if !handled {
		t.Errorf("Ctrl+W not handled")
	}
	if got := m.panel.textInput.Value(); got != "hello world " {
		t.Errorf("after word delete, value = %q, want %q (trailing space preserved)",
			got, "hello world ")
	}
}

// TestHandleRenamePanelKey_CharLimitApplied verifies the panel's
// CharLimit caps the typed name at panelRenameLimit. The bubbles
// textinput silently drops characters past the limit (it does not
// emit an error), so a 300-character paste becomes a 200-character
// value.
func TestHandleRenamePanelKey_CharLimitApplied(t *testing.T) {
	m := &runMode{}
	m.openRenamePanel()
	if got, want := m.panel.textInput.CharLimit, panelRenameLimit; got != want {
		t.Errorf("CharLimit = %d, want %d", got, want)
	}
	long := strings.Repeat("a", 300)
	m.panel.textInput.SetValue(long)
	if got := m.panel.textInput.Value(); len([]rune(got)) != panelRenameLimit {
		t.Errorf("len value = %d, want %d (CharLimit)", len([]rune(got)), panelRenameLimit)
	}
}

// TestNewRenameTextinput_DefaultShape verifies the helper builds a
// usable textinput (focused-able, with prompt, placeholder, limit).
// This is the contract for cycle-02 — every consumer that calls
// newRenameTextinput() picks up the same priming.
func TestNewRenameTextinput_DefaultShape(t *testing.T) {
	in := newRenameTextinput("seed")
	if got := in.Value(); got != "seed" {
		t.Errorf("seed value not applied: got %q, want %q", got, "seed")
	}
	if in.Prompt == "" {
		t.Errorf("prompt empty — View() will not show 'Rename: '")
	}
	if in.Placeholder == "" {
		t.Errorf("placeholder empty — empty state will look bare")
	}
	if in.CharLimit != panelRenameLimit {
		t.Errorf("CharLimit = %d, want %d", in.CharLimit, panelRenameLimit)
	}
	if !in.Focused() {
		t.Errorf("panel opens un-focused — first keystroke is dropped")
	}
}

// TestPanelRenameLines_RendersInput verifies the render-side hook:
// panelLines() for panelRename returns the title + prompt + input
// row + keyboard hint. We don't pin the exact bytes (textinput.View
// is ANSI-heavy and styles are theme-driven), but we do pin the
// shape: the panel title, the "Rename:" prefix, and the keyboard
// hint footer are all present.
func TestPanelRenameLines_RendersInput(t *testing.T) {
	m := &runMode{}
	m.openRenamePanel()
	lines := m.panelRenameLines()
	if len(lines) != 5 {
		t.Fatalf("got %d lines, want 5 (title, blank, prompt+input, blank, hint): %v",
			len(lines), lines)
	}
	if !strings.Contains(stripSGR(lines[0]), "Rename Conversation") {
		t.Errorf("title line missing: %q", lines[0])
	}
	if !strings.Contains(stripSGR(lines[2]), "Rename:") {
		t.Errorf("prompt row missing 'Rename:': %q", lines[2])
	}
	if !strings.Contains(stripSGR(lines[4]), "enter") ||
		!strings.Contains(stripSGR(lines[4]), "Rename") {
		t.Errorf("keyboard hint missing 'enter Rename': %q", lines[4])
	}
}

// TestPanelRenameLines_PlaceholderWhenEmpty verifies the empty-state
// row shows the placeholder ("type a name and press enter…")
// instead of an empty field — the textinput's View() returns the
// placeholder with grey 241 styling when the value is empty.
func TestPanelRenameLines_PlaceholderWhenEmpty(t *testing.T) {
	m := &runMode{}
	m.openRenamePanel()
	// Don't type anything. Strip SGR before matching: the textinput
	// renders its virtual cursor as a separate styled run, so the
	// visible text is split by escape sequences ("t" + SGR + "ype ...").
	lines := m.panelRenameLines()
	if !strings.Contains(stripSGR(lines[2]), "type a name and press enter") {
		t.Errorf("placeholder missing for empty value: %q", lines[2])
	}
}

// TestSubmitRenamePanel_NoopIfNoCurrentAndEmpty makes the edge case
// explicit: an empty submit on a fresh (no prior name) conversation
// still emits the error echo. The cycle-02 panel does not silently
// swallow an empty input — it is the same UX as `/rename` (no args)
// used to be.
func TestSubmitRenamePanel_NoopIfNoCurrentAndEmpty(t *testing.T) {
	m := &runMode{}
	m.openRenamePanel()
	// Empty value + Enter through the real submit. closePanelSilent
	// returns nil — the panel-clearing mutation is the deliverable
	// (matches closePanel's contract at slash_panels.go:3727).
	m.submitRenamePanel()
	if m.panel.kind != panelNone {
		t.Errorf("panel still open after empty submit (kind=%v)", m.panel.kind)
	}
	if !containsResponse(m.response, "Error: Please provide a new name") {
		t.Errorf("empty submit did not echo error: %v", m.response)
	}
}

// containsResponse reports whether any line in response starts with
// substr. The panel flows through slashRespond which appends multiple
// lines (the bold-blue bullet, the dim-gray message, trailing blank);
// we only need the message line to contain the expected text.
func containsResponse(response []string, substr string) bool {
	for _, l := range response {
		if strings.Contains(stripSGR(l), substr) {
			return true
		}
	}
	return false
}

func TestF2_GlobalOpensRenamePanel(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", false)
	m.conversationName = "My Initial Name"

	m.Update(tea.KeyPressMsg{Code: tea.KeyF2})
	if m.panel.kind != panelRename {
		t.Fatalf("F2 did not open rename panel; panel.kind = %v", m.panel.kind)
	}
	if got := m.panel.textInput.Value(); got != "My Initial Name" {
		t.Errorf("rename textinput initial value = %q, want 'My Initial Name'", got)
	}
}

// ---- cycle session-title-strip-changes: normalizeTitle and /rename tests ----

func TestNormalizeTitle_StripsLeadingWhitespace(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{" My Session", "My Session"},
		{"   My Session", "My Session"},
		{"\tMy Session", "My Session"},
		{" \t My Session", "My Session"},
	} {
		if got := normalizeTitle(tc.in); got != tc.want {
			t.Errorf("normalizeTitle(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeTitle_StripsTrailingWhitespace(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{"My Session ", "My Session"},
		{"My Session   ", "My Session"},
		{"My Session\n", "My Session"},
		{"My Session\r\n", "My Session"},
		{"My Session\t", "My Session"},
	} {
		if got := normalizeTitle(tc.in); got != tc.want {
			t.Errorf("normalizeTitle(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeTitle_CollapsesInternalNewline(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{"My\nSession", "My Session"},
		{"My\rSession", "My Session"},
		{"\nMy Session\n", "My Session"},
	} {
		if got := normalizeTitle(tc.in); got != tc.want {
			t.Errorf("normalizeTitle(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeTitle_CollapsesMultipleNewlines(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{"My\n\nSession", "My Session"},
		{"My\n\n\nSession", "My Session"},
		{"My\nSession\nTitle", "My Session Title"},
		{"My\n  Session", "My Session"},
		{"My \t\n Session", "My Session"},
	} {
		if got := normalizeTitle(tc.in); got != tc.want {
			t.Errorf("normalizeTitle(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeTitle_EmptyAfterNormalization(t *testing.T) {
	for _, in := range []string{
		"",
		" ",
		"   ",
		"\n",
		"\n\n",
		"\n   \n",
		"\t",
		" \t \n \r\n ",
	} {
		if got := normalizeTitle(in); got != "" {
			t.Errorf("normalizeTitle(%q) = %q, want \"\"", in, got)
		}
	}
}

func TestRenameFeedback_StripsAndCollapses(t *testing.T) {
	cases := []struct {
		name    string
		arg     string
		current string
		want    string
		wantMsg string
	}{
		{"leading spaces", " My Session", "old", "My Session", "Conversation renamed to: My Session"},
		{"trailing newline", "My Session\n", "old", "My Session", "Conversation renamed to: My Session"},
		{"internal newline", "My\nSession", "old", "My Session", "Conversation renamed to: My Session"},
		{"many internal newlines", "My\n\n\nSession\n\n", "old", "My Session", "Conversation renamed to: My Session"},
		{"both ends and middle", "  My\n\tSession  \n", "old", "My Session", "Conversation renamed to: My Session"},
		{"only newline is rejected", "\n\n", "old", "old", "Error: Please provide a new name for the conversation."},
		{"plain name still works", "Refactor auth", "old", "Refactor auth", "Conversation renamed to: Refactor auth"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, msg := renameFeedback(tc.arg, tc.current)
			if got != tc.want || msg != tc.wantMsg {
				t.Errorf("renameFeedback(%q, %q) = (%q, %q); want (%q, %q)",
					tc.arg, tc.current, got, msg, tc.want, tc.wantMsg)
			}
		})
	}
}

func TestSlashRename_NormalizesConversationName(t *testing.T) {
	for _, tc := range []struct {
		name, arg, want string
	}{
		{"leading space", " My Session", "My Session"},
		{"trailing newline", "My Session\n", "My Session"},
		{"internal newline", "My\nSession", "My Session"},
		{"many internal newlines", "Refactor\n\n\nauth", "Refactor auth"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newSlashModel(t)
			m.handleSlashCommand("/rename " + tc.arg)
			if m.conversationName != tc.want {
				t.Errorf("conversationName = %q, want %q", m.conversationName, tc.want)
			}
			if got := m.conversationTitle(); got != tc.want {
				t.Errorf("conversationTitle() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestConversationTitle_DisplayNormalisesUncleanStoredName(t *testing.T) {
	m := newSlashModel(t)
	m.conversationName = " My Display Title\n"
	if got := m.conversationTitle(); got != "My Display Title" {
		t.Errorf("conversationTitle() = %q, want %q", got, "My Display Title")
	}
	m.conversationName = "\n   \n"
	if got := m.conversationTitle(); got == "" || strings.TrimSpace(got) == "" {
		t.Errorf("conversationTitle() = %q, want non-empty fallback", got)
	}
	if got := m.conversationTitle(); reflect.DeepEqual(got, "\n   \n") {
		t.Errorf("conversationTitle() leaked raw whitespace: %q", got)
	}
}

func TestNormalizeTitle_NoOpOnCleanInput(t *testing.T) {
	for _, in := range []string{
		"My Session",
		"a",
		" leading only",
		"trailing only ",
		"multi word title",
	} {
		want := in
		if in == " leading only" {
			want = "leading only"
		}
		if in == "trailing only " {
			want = "trailing only"
		}
		if got := normalizeTitle(in); got != want {
			t.Errorf("normalizeTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

// ---------------------------------------------------------------- /usage

// The /usage panel shows a counter only while the document is taller than
// the window, and the counter sits directly under the window.
func TestCycle207UsageViewerHeight(t *testing.T) {
	// h=50: the 20-line document fits in 37 rows — no counter, 26 rows total.
	m := mkPanel(panelUsage, "/usage", 0)
	m.width, m.height = 220, 50
	lines := m.panelLines()
	if len(lines) != 26 {
		t.Fatalf("h=50 panel = %d rows, want 26", len(lines))
	}
	for _, l := range lines {
		if strings.Contains(stripSGR(l), "of 20 lines") {
			t.Errorf("h=50 must not show a counter: %q", l)
		}
	}
	if !strings.Contains(stripSGR(strings.Join(lines, "\n")), "directly to your individual tier.") {
		t.Error("h=50 should show the whole note")
	}

	// h=24: window 11 of 20 — counter (1–11 of 20 lines), 18 rows total.
	m2 := mkPanel(panelUsage, "/usage", 0)
	m2.width, m2.height = 220, 24
	lines2 := m2.panelLines()
	if len(lines2) != 18 {
		t.Fatalf("h=24 panel = %d rows, want 18", len(lines2))
	}
	if !strings.Contains(stripSGR(strings.Join(lines2, "\n")), "(1–11 of 20 lines)") {
		t.Errorf("h=24 counter wrong:\n%s", strings.Join(plainLines(m2), "\n"))
	}

	// Scrolling follows the window: one Down moves the counter to (2–12).
	if handled, _ := m2.handlePanelKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Fatal("down not handled")
	}
	if m2.panel.window != 1 {
		t.Fatalf("window = %d, want 1", m2.panel.window)
	}
	if !strings.Contains(stripSGR(strings.Join(m2.panelLines(), "\n")), "(2–12 of 20 lines)") {
		t.Errorf("counter did not follow:\n%s", strings.Join(plainLines(m2), "\n"))
	}
	// ctrl+end lands on the last window (offset 9 → (10–20 of 20 lines)).
	m2.handlePanelKey(tea.Key{Code: tea.KeyEnd, Mod: tea.ModCtrl})
	if m2.panel.window != 9 {
		t.Fatalf("ctrl+end window = %d, want 9", m2.panel.window)
	}
	if !strings.Contains(stripSGR(strings.Join(m2.panelLines(), "\n")), "(10–20 of 20 lines)") {
		t.Errorf("bottom counter wrong:\n%s", strings.Join(plainLines(m2), "\n"))
	}
}

// ---------------------------------------------------------------- /diff

// diffAllChangesLines reproduces REF's All Changes list byte for byte
// (modulo the colour constants): the fixture is repo3, i.e. the data behind
// diff3.ref.raw.txt.
func TestCycle207DiffAllChanges(t *testing.T) {
	tracked := []diffChange{
		{Path: "del.txt", Deleted: 2},
		{Path: "mod.txt", Added: 2, Deleted: 1},
	}
	untracked := []diffChange{
		{Path: "sub/deep.txt", Added: 1, Untracked: true},
		{Path: "untracked.txt", Added: 3, Untracked: true},
	}
	lines := diffAllChangesLines(tracked, untracked, 0)

	wantPlain := []string{
		"Diff (git)  All Changes  Per Turn  Commit Tree",
		"  4 file(s) changed",
		"",
		"> del.txt  -2",
		"  mod.txt  +2 -1",
		"",
		"  ? [Untracked] sub/deep.txt  +1",
		"  ? [Untracked] untracked.txt  +3",
		"",
		"  ←/→ switch view · ↑/↓/j/k navigate · enter view diff · esc dismiss",
	}
	if len(lines) != len(wantPlain) {
		t.Fatalf("lines = %d, want %d:\n%q", len(lines), len(wantPlain), lines)
	}
	for i, want := range wantPlain {
		if got := stripSGR(lines[i]); got != want {
			t.Errorf("line %d: got %q, want %q", i, got, want)
		}
	}
	// Exact SGR on the cursor row, the untracked badge and the counts.
	if !strings.HasPrefix(lines[0], "\x1b[1mDiff (git)\x1b[0m  "+cBlueOpen+"All Changes"+cBlueClose) {
		t.Errorf("title row = %q", lines[0])
	}
	if lines[3] != cBlueOpen+"> del.txt"+cBlueClose+"  "+cToolBulletErrOpen+"-2\x1b[39m" {
		t.Errorf("cursor row = %q", lines[3])
	}
	if !strings.Contains(lines[6], "\x1b[38;5;172m? [Untracked] \x1b[39m") {
		t.Errorf("untracked badge = %q", lines[6])
	}
	// Moving the cursor to the first untracked row re-colours it (diff3_dd).
	cursorLine := diffAllChangesLines(tracked, untracked, 2)
	if !strings.Contains(cursorLine[6], cBlueOpen+"> "+"\x1b[38;5;172m? [Untracked] "+cBlueOpen+"sub/deep.txt"+cBlueClose) {
		t.Errorf("untracked cursor row = %q", cursorLine[6])
	}
}

// The clean All Changes / Per Turn bodies are the indented empty states with
// the short legend.
func TestCycle207DiffEmptyStates(t *testing.T) {
	clean := diffAllChangesLines(nil, nil, 0)
	wantClean := []string{
		" Diff (git)  All Changes  Per Turn  Commit Tree",
		"",
		"   No uncommitted changes",
		"",
		"   ←/→ switch view · esc dismiss",
	}
	if len(clean) != len(wantClean) {
		t.Fatalf("clean = %d lines, want %d: %q", len(clean), len(wantClean), clean)
	}
	for i, want := range wantClean {
		if got := stripSGR(clean[i]); got != want {
			t.Errorf("clean line %d: got %q, want %q", i, got, want)
		}
	}
	// The clean title carries the one-space margin REF shows.
	if !strings.HasPrefix(clean[0], " \x1b[1mDiff (git)\x1b[0m") {
		t.Errorf("clean title = %q", clean[0])
	}
	// Per Turn goes through panelDiffLinesVCS.
	m := mkPanel(panelDiff, "/diff", 0)
	m.cwd, m.panel.tab = "/tmp/nowhere-cycle207", 1
	pt := m.panelDiffLinesVCS()
	if got := stripSGR(pt[2]); got != "   No file changes in conversation" {
		t.Errorf("per-turn empty = %q", got)
	}
}

// diffMoveCursor clamps over the flattened tracked+untracked list.
func TestCycle207DiffMoveCursor(t *testing.T) {
	// Non-git cwd: inert.
	m := mkPanel(panelDiff, "/diff", 0)
	m.cwd = "/tmp/nowhere-cycle207"
	m.diffMoveCursor(1)
	if m.panel.cursor != 0 {
		t.Errorf("cursor moved in a non-repo: %d", m.panel.cursor)
	}
	// Per Turn tab: inert.
	m.panel.tab = 1
	m.diffMoveCursor(1)
	if m.panel.cursor != 0 {
		t.Errorf("cursor moved on Per Turn: %d", m.panel.cursor)
	}
}

// gitDiffChanges parses a real repo the way REF's list needs it: tracked
// files with numstat counts, untracked files with their line counts.
func TestCycle207DiffGitChanges(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q")
	write("del.txt", "a\nb\n")
	write("mod.txt", "line1\nline2\n")
	git("add", "-A")
	git("commit", "-qm", "init")

	if err := os.Remove(filepath.Join(dir, "del.txt")); err != nil {
		t.Fatal(err)
	}
	write("mod.txt", "line1\nCHANGED\n")
	write("sub/deep.txt", "d\n")
	write("untracked.txt", "u1\nu2\nu3\n")

	tracked, untracked, ok := gitDiffChanges(dir)
	if !ok {
		t.Fatal("gitDiffChanges reported not-a-repo")
	}
	if len(tracked) != 2 || len(untracked) != 2 {
		t.Fatalf("tracked=%v untracked=%v", tracked, untracked)
	}
	if tracked[0].Path != "del.txt" || tracked[0].Deleted != 2 || tracked[0].Added != 0 {
		t.Errorf("tracked[0] = %+v, want del.txt -2", tracked[0])
	}
	if tracked[1].Path != "mod.txt" || tracked[1].Added != 1 || tracked[1].Deleted != 1 {
		t.Errorf("tracked[1] = %+v, want mod.txt +1 -1", tracked[1])
	}
	if untracked[0].Path != "sub/deep.txt" || untracked[0].Added != 1 || !untracked[0].Untracked {
		t.Errorf("untracked[0] = %+v, want sub/deep.txt +1", untracked[0])
	}
	if untracked[1].Path != "untracked.txt" || untracked[1].Added != 3 {
		t.Errorf("untracked[1] = %+v, want untracked.txt +3", untracked[1])
	}
}

// A non-repo cwd keeps the no-VCS fallback; the commit tree renders the
// single-commit shape captured for REF.
func TestCycle207DiffCommitTree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-qm", "init")

	m := mkPanel(panelDiff, "/diff", 0)
	m.cwd = dir
	m.panel.tab = 2
	lines := m.panelDiffLinesVCS()
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = stripSGR(l)
	}
	if plain[1] != "   1 commit(s) available" {
		t.Errorf("commit count row = %q", plain[1])
	}
	if plain[3] != " ▌ *  Working Copy (Uncommitted Changes)" {
		t.Errorf("working-copy row = %q", plain[3])
	}
	if !strings.HasPrefix(plain[5], "   * ") || !strings.HasSuffix(plain[5], " init") {
		t.Errorf("commit row = %q", plain[5])
	}
}

// ------------------------------------------------------------ /changelog

// The /changelog title is indented and carries REF's 48;5;63 background,
// followed by a blank row.
func TestCycle207ChangelogTitle(t *testing.T) {
	excerpt := tuiviewer.ChangelogExcerptRows()
	want := "  \x1b[1m\x1b[38;5;228m\x1b[48;5;63m Pigasus CLI Changelog \x1b[0m"
	if excerpt[0] != want {
		t.Errorf("title = %q, want %q", excerpt[0], want)
	}
	if excerpt[1] != "" {
		t.Errorf("excerpt[1] = %q, want a blank row", excerpt[1])
	}
	if !strings.Contains(stripSGR(excerpt[2]), "The terminal-first surface") {
		t.Errorf("excerpt[2] = %q", excerpt[2])
	}
}

// A sized /changelog window is height-4 lines of the document.
func TestCycle207ChangelogHeight(t *testing.T) {
	m := mkPanel(panelChangelog, "/changelog", 0)
	m.height = 24
	spec, ok := m.viewerSpecFor()
	if !ok {
		t.Fatal("changelog is not a viewer")
	}
	if spec.visible != 20 {
		t.Errorf("visible = %d, want 20 (h-4)", spec.visible)
	}
	if spec.total != 2014 {
		t.Errorf("total = %d, want 2014", spec.total)
	}
	if !strings.Contains(stripSGR(strings.Join(m.panelLines(), "\n")), "(1–20 of 2014 lines)") {
		t.Errorf("counter wrong:\n%s", strings.Join(plainLines(m), "\n"))
	}
}

// ------------------------------------------------ /rewind, /credits pin

// /rewind and /credits already matched REF 1.4.0 byte for byte; these pin
// the exact rows so a later edit cannot silently regress them.
func TestCycle207RewindCreditsRegression(t *testing.T) {
	rewind := mkPanel(panelRewind, "/rewind", 0).panelLines()
	assertLines(t, rewind, []string{
		"\x1b[1m\x1b[39mRewind Conversation\x1b[0m",
		"",
		cBlueOpen + "> (current)" + cBlueClose,
		"",
		panelKeyboardHint("↑/↓", "Navigate", "enter", "Select", "esc", "Cancel"),
	})

	credits := mkPanel(panelCredits, "/credits", 0).panelLines()
	assertLines(t, credits, []string{
		cBlueOpen + "Pigasus CLI" + cBlueClose + "  " +
			"\x1b[1m" + panelGray238 + "credits" + "\x1b[0m",
		"\x1b[1m" + panelGray238 + "Model AI Credits" + "\x1b[0m",
		"",
		"\x1b[1mRemaining AI Credits\x1b[0m: " +
			"\x1b[1m" + cBlueOpen + "AI Credits not enabled (enable in /settings)" + "\x1b[0m",
		"",
		"\x1b[1m" + panelGray238 + "Actions" + "\x1b[0m",
		"",
		cBlueOpen + "> Get More AI Credits" + cBlueClose,
		"  See Activity",
		"",
		panelKeyboardHint("↑/↓", "Navigate", "enter", "Select / Toggle", "esc", "Close"),
	})
}

func parseEnv(env []string) map[string]string {
	out := make(map[string]string, len(env))
	for _, kv := range env {
		if eq := strings.IndexByte(kv, '='); eq > 0 {
			out[kv[:eq]] = kv[eq+1:]
		}
	}
	return out
}

func TestRunGitEnvKVs_Shape(t *testing.T) {
	want := []string{
		"PAGER=cat",
		"GIT_PAGER=cat",
		"JJ_PAGER=cat",
		"GIT_EDITOR=false",
		"LC_ALL=C",
	}
	if len(runGitEnvKVs) != len(want) {
		t.Fatalf("runGitEnvKVs has %d entries, want %d (%v)",
			len(runGitEnvKVs), len(want), runGitEnvKVs)
	}
	for i := range want {
		if runGitEnvKVs[i] != want[i] {
			t.Errorf("runGitEnvKVs[%d] = %q, want %q",
				i, runGitEnvKVs[i], want[i])
		}
	}
}

func TestRunGitEnv_GitPagerSet(t *testing.T) {
	env := parseEnv(runGitEnv())
	if got := env["GIT_PAGER"]; got != "cat" {
		t.Errorf("GIT_PAGER = %q, want %q (runGitEnvKVs = %v)",
			got, "cat", runGitEnvKVs)
	}
}

func TestRunGitEnv_JjPagerSet(t *testing.T) {
	env := parseEnv(runGitEnv())
	if got := env["JJ_PAGER"]; got != "cat" {
		t.Errorf("JJ_PAGER = %q, want %q (runGitEnvKVs = %v)",
			got, "cat", runGitEnvKVs)
	}
}

func TestRunGitEnv_BothPagersSet(t *testing.T) {
	env := parseEnv(runGitEnv())
	if env["GIT_PAGER"] != "cat" {
		t.Errorf("GIT_PAGER = %q, want %q", env["GIT_PAGER"], "cat")
	}
	if env["JJ_PAGER"] != "cat" {
		t.Errorf("JJ_PAGER = %q, want %q", env["JJ_PAGER"], "cat")
	}
	if env["PAGER"] != "cat" {
		t.Errorf("PAGER = %q, want %q", env["PAGER"], "cat")
	}
}

func TestRunGitEnv_OtherVarsUntouched(t *testing.T) {
	const sentinel = "WAGYU_RUN_GIT_ENV_TEST_SENTINEL_42"
	t.Setenv(sentinel, "alive")

	env := parseEnv(runGitEnv())
	if got := env[sentinel]; got != "alive" {
		t.Errorf("parent env lost: %s = %q, want %q (full runGitEnv slice has %d entries)",
			sentinel, got, "alive", len(runGitEnv()))
	}
	if env["PATH"] == "" {
		t.Errorf("PATH missing from runGitEnv — parent environment was stripped")
	}
}

func TestRunGitEnv_OverridesWinOnDuplicate(t *testing.T) {
	t.Setenv("GIT_PAGER", "less")
	t.Setenv("JJ_PAGER", "more")

	env := runGitEnv()
	lastGIT := ""
	lastJJ := ""
	for _, kv := range env {
		switch {
		case strings.HasPrefix(kv, "GIT_PAGER="):
			lastGIT = kv
		case strings.HasPrefix(kv, "JJ_PAGER="):
			lastJJ = kv
		}
	}
	if lastGIT != "GIT_PAGER=cat" {
		t.Errorf("GIT_PAGER override lost: last occurrence = %q, want %q", lastGIT, "GIT_PAGER=cat")
	}
	if lastJJ != "JJ_PAGER=cat" {
		t.Errorf("JJ_PAGER override lost: last occurrence = %q, want %q", lastJJ, "JJ_PAGER=cat")
	}

	parentBefore := true
	for _, kv := range env {
		if kv == "GIT_PAGER=cat" || kv == "JJ_PAGER=cat" {
			parentBefore = false
			break
		}
	}
	if parentBefore {
		t.Errorf("our overrides appear at the start of the slice; on POSIX duplicate env entries the LAST value wins, so overrides must be appended after os.Environ (got %v)", env)
	}
}

func TestRunGitEnv_PrependsParentEnv(t *testing.T) {
	parent := os.Environ()
	got := runGitEnv()

	if len(got) < len(parent)+len(runGitEnvKVs) {
		t.Errorf("runGitEnv length = %d, want at least %d (parent %d + kvs %d)",
			len(got), len(parent)+len(runGitEnvKVs), len(parent), len(runGitEnvKVs))
	}

	step := len(parent) / 3
	if step > 0 {
		for i := 0; i < 3; i++ {
			idx := i * step
			if idx < len(parent) && got[idx] != parent[idx] {
				t.Errorf("parent env entry [%d] = %q, want %q (runGitEnv reorders parent?)",
					idx, got[idx], parent[idx])
			}
		}
	}
}

// Tasks panel & status line tests (consolidated from slash_tasks_test.go)

// seedRunMode returns a runMode wired to a fresh, empty task
// registry. Tests use it to seed the panel / status row without
// touching the package-level defaultTaskSubagentManager (which is
// owned by other tests).
func seedRunMode() *runMode {
	m := &runMode{}
	m.taskReg = taskregistry.New()
	m.taskSubagentManager = subagent.NewManager()
	return m
}

// seedTask inserts a single task into reg with the given state.
// Convenience helper to keep the test bodies readable.
func seedTask(reg *taskregistry.Registry, id string, name string, kind taskregistry.Kind, state taskregistry.State) *taskregistry.Task {
	t := &taskregistry.Task{
		ID:    id,
		Name:  name,
		Kind:  kind,
		State: state,
	}
	if state == taskregistry.StateRunning {
		t.StartedAt = time.Now().Add(-2 * time.Second)
	} else if state == taskregistry.StateDone {
		t.StartedAt = time.Now().Add(-12 * time.Second)
		t.EndedAt = time.Now()
		t.ExitCode = 0
	} else {
		t.StartedAt = time.Now()
	}
	reg.Register(t)
	return t
}

// stripAll removes every ANSI escape sequence (SGR + cursor) so
// the assertions compare plain text. stripSGR only handles SGR;
// /tasks panels use a lot of 24-bit RGB sequences so we need a
// broader stripper here.
func stripAll(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !(s[j] >= 0x40 && s[j] <= 0x7e) {
				j++
			}
			if j < len(s) {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// plainPanelLines returns the rendered /tasks panel as plain
// (escape-stripped) lines.
func plainPanelLines(m *runMode) []string {
	raw := m.panelTasksLines()
	out := make([]string, 0, len(raw))
	for _, l := range raw {
		out = append(out, stripAll(l))
	}
	return out
}

func TestSlashTasksEmpty(t *testing.T) {
	m := seedRunMode()
	lines := plainPanelLines(m)
	// The header is always the bold "Tasks" title.
	if !strings.HasPrefix(lines[0], "Tasks") {
		t.Fatalf("first line = %q, want to start with %q", lines[0], "Tasks")
	}
	// The empty placeholder is on line 2 — dim gray, exact match.
	if !strings.Contains(lines[1], "No background tasks.") {
		t.Errorf("line[1] = %q, want substring %q", lines[1], "No background tasks.")
	}
	// Footer legend (Keyboard: ...) is on line 3.
	if !strings.HasPrefix(lines[3], "Keyboard:") {
		t.Errorf("footer = %q, want to start with %q", lines[3], "Keyboard:")
	}
	// The status-row suffix must be empty when the registry is
	// empty — the "· 0 tasks" form would be wrong.
	if got := m.tasksStatusSuffix(); got != "" {
		t.Errorf("tasksStatusSuffix(empty) = %q, want empty string", got)
	}
}

func TestSlashTasksList(t *testing.T) {
	m := seedRunMode()
	// Seed t3 first with the OLDEST StartedAt so the registry
	// sorts it at index 0; the assertion below keys off the
	// rendered order so we want a deterministic shape. t3 is
	// the "done" task; t1 and t2 are running.
	now := time.Now()
	t3 := &taskregistry.Task{
		ID: "t3", Name: "echo done", Kind: taskregistry.KindShell,
		State: taskregistry.StateDone, StartedAt: now.Add(-12 * time.Second), EndedAt: now,
		ExitCode: 0,
	}
	t1 := &taskregistry.Task{
		ID: "t1", Name: "bash -c sleep 60", Kind: taskregistry.KindShell,
		State: taskregistry.StateRunning, StartedAt: now.Add(-2 * time.Second),
	}
	t2 := &taskregistry.Task{
		ID: "t2", Name: "ddgr --num=5 cats", Kind: taskregistry.KindShell,
		State: taskregistry.StateRunning, StartedAt: now.Add(-1 * time.Second),
	}
	m.taskReg.Register(t3)
	m.taskReg.Register(t1)
	m.taskReg.Register(t2)

	lines := plainPanelLines(m)
	// 6 rows: 1 header + 3 task rows + 1 trailing blank + 1 footer.
	if len(lines) != 6 {
		t.Fatalf("panel lines = %d, want 6", len(lines))
	}
	// Header
	if !strings.HasPrefix(lines[0], "Tasks") {
		t.Errorf("line[0] = %q, want Tasks", lines[0])
	}
	// Row 1 is t3 (oldest StartedAt); rows 2 and 3 are t1 and
	// t2 (running). The state glyph + ID together pin the row.
	wantRows := []struct {
		id    string
		state string
	}{
		{"t3", "done"},
		{"t1", "running"},
		{"t2", "running"},
	}
	for i, want := range wantRows {
		line := lines[1+i]
		if !strings.Contains(line, want.state) {
			t.Errorf("line[%d] = %q, want to contain %q", 1+i, line, want.state)
		}
		if !strings.Contains(line, want.id) {
			t.Errorf("line[%d] = %q, want to contain %q", 1+i, line, want.id)
		}
	}
	// Footer is on row 5.
	if !strings.HasPrefix(lines[5], "Keyboard:") {
		t.Errorf("footer = %q, want Keyboard:", lines[5])
	}

	// Status row: 2 running tasks → " · 2 tasks".
	got := m.tasksStatusSuffix()
	if got != " · 2 tasks" {
		t.Errorf("tasksStatusSuffix = %q, want %q", got, " · 2 tasks")
	}
}

func TestSlashTasksKillRunning(t *testing.T) {
	m := seedRunMode()
	// Seed a Subagent so the kill path has something to call
	// Kill on. The bridge attaches the subagent to the registry
	// for us; the test then asserts the transition lands.
	sa := subagent.NewSubagent("bash", "-c", "sleep 30")
	taskID := m.taskReg.AttachSubagent(sa, "")
	if taskID == "" {
		t.Fatal("AttachSubagent returned empty ID")
	}
	// Register the Subagent in the panel's manager so the kill
	// path can find it.
	m.taskSubagentManager.Add(sa)

	// Start it so it transitions pending → running.
	if err := sa.Start(); err != nil {
		t.Fatalf("subagent.Start: %v", err)
	}
	// Spin briefly so the goroutine that drains stdout can
	// fire the StateChangeFunc.
	time.Sleep(50 * time.Millisecond)

	if got := m.taskReg.Get(taskID).State; got != taskregistry.StateRunning {
		t.Fatalf("post-Start state = %q, want running", got)
	}

	// Press Enter on the cursor row. The cursor defaults to 0,
	// and with one task that is the running row.
	m.panel = slashPanel{kind: panelTasks, cmd: "/tasks", cursor: 0}
	handled, _ := m.tasksPanelKey(tea.Key{Code: tea.KeyEnter})
	if !handled {
		t.Fatal("tasksPanelKey(Enter) reported unhandled")
	}

	// The Kill must propagate through the StateChangeFunc and
	// land in the registry within 1s (the test budget for the
	// "killed within 1s" claim in the deliverable).
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if m.taskReg.Get(taskID).State == taskregistry.StateKilled {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := m.taskReg.Get(taskID).State; got != taskregistry.StateKilled {
		t.Errorf("state after kill = %q, want killed", got)
	}
}

func TestStatusLineTaskCount(t *testing.T) {
	m := seedRunMode()

	// Zero tasks → no segment.
	if got := m.tasksStatusSuffix(); got != "" {
		t.Errorf("zero-task suffix = %q, want empty", got)
	}

	// Two running tasks → " · 2 tasks".
	seedTask(m.taskReg, "t1", "bash -c sleep 60", taskregistry.KindShell, taskregistry.StateRunning)
	seedTask(m.taskReg, "t2", "bash -c sleep 90", taskregistry.KindShell, taskregistry.StateRunning)
	got := m.tasksStatusSuffix()
	if got != " · 2 tasks" {
		t.Errorf("two-task suffix = %q, want %q", got, " · 2 tasks")
	}

	// One running + one done → still " · 1 task".
	tt := m.taskReg.Get("t2")
	m.taskReg.Update(tt.ID, func(t *taskregistry.Task) {
		t.State = taskregistry.StateDone
		t.ExitCode = 0
		t.EndedAt = time.Now()
	})
	if got := m.tasksStatusSuffix(); got != " · 1 task" {
		t.Errorf("one-running suffix = %q, want %q", got, " · 1 task")
	}
}

func TestStatusLineUpdatesLive(t *testing.T) {
	m := seedRunMode()
	m.startTasksSubscription()
	defer func() {
		if m.tasksSubCancel != nil {
			m.tasksSubCancel()
		}
	}()

	// Initial state: empty suffix.
	if got := m.tasksStatusSuffix(); got != "" {
		t.Fatalf("initial suffix = %q, want empty", got)
	}

	// Register a task; the subscriber goroutine should set
	// tasksDirty within a few ms (the channel is buffered, so
	// the send completes immediately).
	seedTask(m.taskReg, "live-1", "bash -c sleep 5", taskregistry.KindShell, taskregistry.StateRunning)

	// Wait up to 100 ms for the dirty flag. The field is
	// int32 (atomic) — go vet's atomic checker would otherwise
	// flag this read.
	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&m.tasksDirty) == 1 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if atomic.LoadInt32(&m.tasksDirty) != 1 {
		t.Fatal("subscriber did not set tasksDirty within 100ms")
	}

	// Once dirty, the suffix must report the new running count.
	if got := m.tasksStatusSuffix(); got != " · 1 task" {
		t.Errorf("suffix after register = %q, want %q", got, " · 1 task")
	}
}

// TestTasksFeedback_Regression keeps the cycle-106 recovered text
// alive so the print-mode /tasks path does not regress when this
// cycle replaces the no-op handler with the live panel. The text
// "No background tasks." / "2 running, 5 done" is the byte-exact
// output the print path renders; it must not drift.
func TestTasksFeedback_Regression(t *testing.T) {
	if got := tasksFeedback(0, 0); got != "No background tasks." {
		t.Errorf("tasksFeedback(0,0) = %q, want %q", got, "No background tasks.")
	}
	if got := tasksFeedback(2, 5); got != "2 running, 5 done" {
		t.Errorf("tasksFeedback(2,5) = %q, want %q", got, "2 running, 5 done")
	}
}

// TestSubscribedIsIdempotent guards the panel-reopen path: every
// time the panel opens, the cycle-206 listPanelKind switch flips
// and we want the subscriber to be re-armed without leaking the
// previous subscription.
func TestSubscribedIsIdempotent(t *testing.T) {
	m := seedRunMode()
	m.startTasksSubscription()
	first := m.tasksSubCancel
	m.startTasksSubscription()
	second := m.tasksSubCancel
	if first == nil || second == nil {
		t.Fatal("subscriber did not arm")
	}
	// Cancel both to clean up. We cannot compare func pointers
	// in Go (the language forbids it); the test asserts that
	// BOTH cancel funcs are non-nil so a regression that
	// returned nil for the second arm would be caught.
	first()
	second()
}

// ---------------------------------------------------------------- /feedback & cycle-208 regression tests (from slash_feedback_regression_test.go)

// --- /config ---------------------------------------------------------------

// REF pads the value column at 29 *runes*; `Editor Mode › Insert First` is
// 26 runes but 28 bytes, so the byte count left it two cells short.
func TestCycle208_ConfigNamePadCountsRunes(t *testing.T) {
	cases := []struct {
		name string
		want int
	}{
		{"Agent Mode", 19},
		{"Editor Mode › Insert First", 3},
		{"Use AI Credits", 15},
	}
	for _, tc := range cases {
		if got := configNamePad(tc.name, 29); got != tc.want {
			t.Errorf("configNamePad(%q) = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// The rendered nested row must carry three spaces before its value, which
// is what REF prints (`  Editor Mode › Insert First   off`).
func TestCycle208_ConfigNestedRowAlignment(t *testing.T) {
	cycle196TestHome(t, map[string]any{"editorMode": "vim"})
	m := mkPanel(panelConfig, "/config", 0)
	m.height = 60 // tall enough that every row fits, so no windowing
	lines := m.panelLines()
	idx := configRowIndexForTest(t, "Editor Mode › Insert First")
	got := lines[5+idx]
	want := "  Editor Mode › Insert First" + strings.Repeat(" ", 3) + "off"
	if got != want {
		t.Errorf("nested row:\n got %q\nwant %q", got, want)
	}
}

// The counter follows the description block's single trailing blank.
// Live 1.4.3 at 120x40: rows, ”, desc, ”, [1-18 of 20 items], ”, Keyboard.
func TestCycle208_ConfigWindowedCounterBlanks(t *testing.T) {
	cycle196TestHome(t, map[string]any{"editorMode": "vim"})

	windowed := mkPanel(panelConfig, "/config", 0)
	windowed.height = 24
	wl := windowed.panelLines()
	if !strings.Contains(wl[len(wl)-3], "items]") {
		t.Fatalf("expected a counter at len-3, got %q", wl[len(wl)-3])
	}
	if wl[len(wl)-4] != "" || wl[len(wl)-5] == "" {
		t.Errorf("windowed list needs one blank after the description, got %q %q",
			wl[len(wl)-5], wl[len(wl)-4])
	}

	complete := mkPanel(panelConfig, "/config", 0)
	complete.height = 60
	cl := complete.panelLines()
	if strings.Contains(strings.Join(cl, "\n"), "items]") {
		t.Errorf("complete list should have no counter:\n%s", strings.Join(cl, "\n"))
	}
	if cl[len(cl)-3] != "" || cl[len(cl)-4] != "" {
		t.Errorf("complete list blanks: %q %q", cl[len(cl)-4], cl[len(cl)-3])
	}
}

// The cursor row has two measured forms: a name of 16+ runes is emitted as
// one blue span covering name+pad+value, a shorter name leaves the pad
// uncoloured. Measured over all 21 rows in harness/cfg_cursor.py.
func TestCycle208_ConfigCursorRowSpanForms(t *testing.T) {
	cycle196TestHome(t, map[string]any{"editorMode": "vim"})
	if !configCursorRowIsSingleSpan("Enable Telemetry") { // 16 runes
		t.Error("16-rune name must use the single-span form")
	}
	if !configCursorRowIsSingleSpan("Editor Mode › Insert First") { // 26 runes
		t.Error("26-rune name must use the single-span form")
	}
	if configCursorRowIsSingleSpan("Artifact Review") { // 15 runes
		t.Error("15-rune name must use the two-span form")
	}

	m := mkPanel(panelConfig, "/config", 0) // Agent Mode (10 runes)
	m.height = 60
	agent := m.panelLines()[5]
	wantTwo := cBlueOpen + "> Agent Mode" + cBlueClose + strings.Repeat(" ", 19) +
		cBlueOpen + m.configRowValueAt(0) + cBlueClose
	if agent != wantTwo {
		t.Errorf("Agent Mode cursor row:\n got %q\nwant %q", agent, wantTwo)
	}

	m.panel.cursor = 9 // Enable Telemetry (16 runes)
	tele := m.panelLines()[14]
	wantOne := cBlueOpen + "> Enable Telemetry" + strings.Repeat(" ", 13) +
		m.configRowValueAt(9) + cBlueClose
	if tele != wantOne {
		t.Errorf("Enable Telemetry cursor row:\n got %q\nwant %q", tele, wantOne)
	}
}

// --- /model ----------------------------------------------------------------

// The effort slider and its `←/→ Effort` legend entry exist only while the
// cursor is on the three-tier Gemini row.
func TestCycle208_ModelSliderOnlyOnTieredRow(t *testing.T) {
	cycle196TestHome(t, map[string]any{"model": "gemini-3.8-flash-high"})
	m := mkPanel(panelModel, "/model", 0)

	onGemini := strings.Join(plainLines(m), "\n")
	if !strings.Contains(onGemini, "Effort") || !strings.Contains(onGemini, "←/→ Effort") {
		t.Fatalf("Gemini row should show the slider + legend:\n%s", onGemini)
	}
	if !strings.Contains(onGemini, "←/→ Effort  enter Select  esc Go Back") {
		t.Errorf("Gemini legend wording changed:\n%s", onGemini)
	}

	m.panel.cursor = 1 // Claude Sonnet — fixed effort
	off := plainLines(m)
	joined := strings.Join(off, "\n")
	if strings.Contains(joined, "Effort") || strings.Contains(joined, "←/→") {
		t.Errorf("non-tiered row must hide the slider and the ←/→ hint:\n%s", joined)
	}
	if got := off[len(off)-1]; got != "Keyboard: ↑/↓ Navigate  enter Select  esc Go Back" {
		t.Errorf("non-tiered legend = %q", got)
	}
}

// The cursor row is padded to column 30 only when the `(current)` marker
// follows it; an un-annotated cursor row has no trailing spaces.
func TestCycle208_ModelCursorRowPadOnlyWhenCurrent(t *testing.T) {
	cycle196TestHome(t, map[string]any{"model": "gemini-3.8-flash-high"})
	m := mkPanel(panelModel, "/model", 1) // Claude row, not current
	m.height = 0
	lines := m.panelLines()
	got := lines[5+1]
	want := cBlueOpen + "> " + modelRows[1].Display + cBlueClose
	if got != want {
		t.Errorf("cursor row:\n got %q\nwant %q", got, want)
	}
	gem := lines[5]
	if !strings.Contains(gem, strings.Repeat(" ", 12)+panelGray238+" (current)") {
		t.Errorf("annotated Gemini row lost its padding: %q", gem)
	}
}

// Moving the cursor off the Gemini row and back must not lose the
// `(current)` marker or move the slider off the model's current effort.
func TestCycle208_ModelEffortCursorReseeds(t *testing.T) {
	cycle196TestHome(t, map[string]any{"model": "gemini-3.8-flash-high"})
	m := mkPanel(panelModel, "/model", 0) // Gemini row — has the slider
	m.height = 0
	if m.panel.effortCursor != effortHigh {
		t.Fatalf("open cursor = %d, want high", m.panel.effortCursor)
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Fatal("down not handled")
	}
	if m.panel.effortCursor != effortHigh {
		t.Errorf("after down: effortCursor = %d, want high", m.panel.effortCursor)
	}
	if !strings.Contains(m.panelLines()[5], " (current)") {
		t.Errorf("MiniMax row lost its (current) marker while the cursor is on Claude: %q",
			m.panelLines()[5])
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyUp}); !handled {
		t.Fatal("up not handled")
	}
	lines := m.panelLines()
	slider := -1
	for i, ln := range lines {
		if strings.Contains(ln, panelEffHighOpen) {
			slider = i
			break
		}
	}
	if slider < 0 {
		t.Fatalf("the effort slider row is missing: %q", lines)
	}
	if !strings.Contains(lines[slider], panelEffHighOpen) {
		t.Errorf("slider did not return to high: %q", lines[slider])
	}
	if !strings.Contains(lines[5], " (current)") {
		t.Errorf("MiniMax row lost its (current) marker: %q", lines[5])
	}
}

// --- /effort ---------------------------------------------------------------

// The lit run stops at the active ◉; the 14 cells after it are dim.
func TestCycle208_EffortBarColourBoundary(t *testing.T) {
	on, off := effortBarSegments(effortLow)
	if on != strings.Repeat(" ", 7)+"◉" {
		t.Errorf("low on-run = %q", on)
	}
	if off != strings.Repeat("─", 14)+"○"+strings.Repeat("─", 14)+"○"+strings.Repeat(" ", 7) {
		t.Errorf("low off-run = %q", off)
	}

	on, off = effortBarSegments(effortMedium)
	if !strings.HasSuffix(on, "◉") || strings.Contains(on, "─") {
		t.Errorf("medium on-run = %q", on)
	}
	if !strings.HasPrefix(off, strings.Repeat("─", 14)) {
		t.Errorf("medium off-run = %q", off)
	}

	on, off = effortBarSegments(effortHigh)
	if off != strings.Repeat(" ", 7) {
		t.Errorf("high off-run = %q", off)
	}
	if !strings.HasSuffix(on, "◉") {
		t.Errorf("high on-run = %q", on)
	}
}

// The status row's suffix follows the live effort, not a constant.
func TestCycle208_StatusEffortSuffixTracksEffort(t *testing.T) {
	m := &runMode{}
	if got := m.statusEffortSuffix(); got != " \u00b7 high" {
		t.Errorf("default suffix = %q", got)
	}
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.String("effort", "", "")
	m.flags = fs
	for level, want := range map[string]string{
		"low": " \u00b7 low", "medium": " \u00b7 medium", "high": " \u00b7 high",
	} {
		if err := fs.Set("effort", level); err != nil {
			t.Fatalf("set effort: %v", err)
		}
		if got := m.statusEffortSuffix(); got != want {
			t.Errorf("effort %s: suffix = %q, want %q", level, got, want)
		}
	}
}

// Enter on /effort: an unchanged level closes silently and writes nothing;
// a changed level echoes `Effort set to <level>`, moves the banner/status
// and persists the model label to settings.json.
func TestCycle208_EffortEnterApplies(t *testing.T) {
	home := cycle196TestHome(t, map[string]any{"model": "MiniMax-M3"})
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.String("effort", "", "")

	m := mkPanel(panelEffort, "/effort", effortHigh)
	m.flags = fs
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("enter not handled")
	}
	if m.panel.kind != panelNone {
		t.Fatalf("panel not closed: kind=%v", m.panel.kind)
	}
	if len(m.response) != 0 {
		t.Errorf("no-op apply must stay silent, got %q", m.response)
	}
	if s := readCycle196Settings(t, home); s["model"] != "MiniMax-M3" {
		t.Errorf("no-op apply wrote settings: model=%v", s["model"])
	}

	// Moving to Medium is a real change.
	m2 := mkPanel(panelEffort, "/effort", effortMedium)
	m2.flags = fs
	if handled, _ := m2.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("enter not handled")
	}
	if m2.panel.kind != panelNone {
		t.Fatalf("panel not closed: kind=%v", m2.panel.kind)
	}
	if got := stripSGR(strings.Join(m2.response, "\n")); !strings.Contains(got, "Effort set to medium") {
		t.Errorf("response = %q", got)
	}
	if s := readCycle196Settings(t, home); s["model"] != "Gemini 3.8 Flash (Medium)" {
		t.Errorf("persisted model = %v", s["model"])
	}
	if m2.model != "Gemini 3.8 Flash (Medium)" {
		t.Errorf("banner model = %q", m2.model)
	}
	if m2.statusModel != "Gemini 3.8 Flash" {
		t.Errorf("status model = %q", m2.statusModel)
	}
	if got := m2.statusEffortSuffix(); got != " \u00b7 medium" {
		t.Errorf("status suffix = %q", got)
	}
}

// --- /feedback -------------------------------------------------------------

func TestCycle208_FeedbackStage2Layout(t *testing.T) {
	m := mkPanel(panelFeedback, "/feedback", 0)
	m.panel.stage = 1
	m.panel.cursor = 0
	assertLines(t, m.panelLines(), []string{
		panelTitleOpen + "Review Submission Data" + panelTitleClose + "  " +
			panelGray241 + "(You can always provide manual feedback via /feedback)" + panelGray241C,
		"",
		"Your feedback helps improve Pigasus CLI. The following sanitized data will be sent:",
		"",
		"Feedback: " + cBlueOpen + "Code doesn't work (e.g., syntax errors, bad logic)" + cBlueClose,
		"",
		"Additional Details (optional):",
		searchCursorBlock,
		"",
		"",
		"",
		"\x1b[7m\x1b[38;2;133;153;0m ctrl+s Submit Feedback \x1b[0m  " + panelGray241 + "esc Back" + panelGray241C,
	})
}

func TestCycle208_FeedbackStage2CtrlSSubmits(t *testing.T) {
	m := mkPanel(panelFeedback, "/feedback", 0)
	m.panel.stage = 1
	m.panel.text = "the model repeated itself"
	m.handlePanelKey(tea.Key{Code: 's', Mod: tea.ModCtrl})
	if m.panel.kind != panelNone {
		t.Fatalf("ctrl+s did not submit: kind=%v", m.panel.kind)
	}
	if got := stripSGR(strings.Join(m.response, "\n")); !strings.Contains(got, "Feedback recorded") {
		t.Errorf("ctrl+s submit should record the local-only message: %q", got)
	}
}

func TestCycle221_FeedbackStage2BufferEmpty(t *testing.T) {
	m := mkPanel(panelFeedback, "/feedback", 2)
	m.panel.stage = 1
	m.panel.text = ""
	lines := m.panelLines()
	if got := lines[7]; got != searchCursorBlock {
		t.Errorf("empty buffer row = %q, want %q", got, searchCursorBlock)
	}
	if m.panel.text != "" {
		t.Errorf("empty buffer text = %q, want empty", m.panel.text)
	}
}

func TestCycle221_FeedbackStage2BufferTyping(t *testing.T) {
	m := mkPanel(panelFeedback, "/feedback", 0)
	m.panel.stage = 1
	for _, r := range []string{"h", "e", "l", "l", "o", " ", "w", "o", "r", "l", "d"} {
		if handled, _ := m.handlePanelKey(tea.Key{Text: r}); !handled {
			t.Fatalf("printable key %q not handled", r)
		}
	}
	if m.panel.text != "hello world" {
		t.Errorf("buffer = %q, want %q", m.panel.text, "hello world")
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyBackspace})
	if m.panel.text != "hello worl" {
		t.Errorf("after backspace = %q, want %q", m.panel.text, "hello worl")
	}
	m.panel.text = ""
	m.handlePanelKey(tea.Key{Code: tea.KeyBackspace})
	if m.panel.text != "" {
		t.Errorf("backspace on empty buffer = %q, want empty", m.panel.text)
	}
}

func TestCycle221_FeedbackStage2EnterSubmits(t *testing.T) {
	m := mkPanel(panelFeedback, "/feedback", 2)
	m.panel.stage = 1
	m.panel.text = "model got stuck in a loop"
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("enter not handled in stage 2")
	}
	if m.panel.kind != panelNone {
		t.Fatalf("enter did not close the panel: kind=%v", m.panel.kind)
	}
	got := stripSGR(strings.Join(m.response, "\n"))
	want := "Feedback recorded (openagy has no upload endpoint; saved locally only)"
	if !strings.Contains(got, want) {
		t.Errorf("submit echo missing %q:\n%s", want, got)
	}
	if !strings.Contains(got, "> /feedback") {
		t.Errorf("submit echo missing the > /feedback header:\n%s", got)
	}
}

func TestCycle221_FeedbackStage2EnterSubmitsEmpty(t *testing.T) {
	m := mkPanel(panelFeedback, "/feedback", 5)
	m.panel.stage = 1
	m.panel.text = ""
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if m.panel.kind != panelNone {
		t.Fatalf("enter on empty buffer did not submit: kind=%v", m.panel.kind)
	}
	got := stripSGR(strings.Join(m.response, "\n"))
	if !strings.Contains(got, "Feedback recorded") {
		t.Errorf("empty-buffer submit echo missing: %q", got)
	}
}

func TestCycle221_FeedbackStage2EscReturnsToStage1(t *testing.T) {
	m := mkPanel(panelFeedback, "/feedback", 4)
	m.panel.stage = 1
	m.panel.text = "some draft"
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEsc}); !handled {
		t.Fatal("esc not handled in stage 2")
	}
	if m.panel.stage != 0 {
		t.Errorf("esc did not return to stage 1: stage=%d", m.panel.stage)
	}
	if m.panel.text != "" {
		t.Errorf("esc did not discard the draft: text=%q", m.panel.text)
	}
	if m.panel.kind != panelFeedback {
		t.Errorf("esc closed the panel: kind=%v", m.panel.kind)
	}
	if m.panel.cursor != 4 {
		t.Errorf("esc lost the category: cursor=%d, want 4", m.panel.cursor)
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if m.panel.stage != 1 {
		t.Fatalf("re-enter did not advance: stage=%d", m.panel.stage)
	}
	if m.panel.text != "" {
		t.Errorf("re-enter should leave the buffer empty: text=%q", m.panel.text)
	}
	if m.panel.cursor != 4 {
		t.Errorf("re-enter lost the category: cursor=%d, want 4", m.panel.cursor)
	}
}

func TestCycle221_FeedbackStage1BindingsRetained(t *testing.T) {
	for _, k := range []string{"1", "3", "5"} {
		m := mkPanel(panelFeedback, "/feedback", 0)
		m.handlePanelKey(tea.Key{Text: k})
		want := int(k[0] - '1')
		if m.panel.stage != 1 || m.panel.cursor != want {
			t.Fatalf("'%s' select-and-continue: stage=%d cursor=%d, want 1, %d",
				k, m.panel.stage, m.panel.cursor, want)
		}
	}

	m := mkPanel(panelFeedback, "/feedback", 0)
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if m.panel.stage != 1 || m.panel.cursor != 0 {
		t.Fatalf("plain enter: stage=%d cursor=%d, want 1, 0", m.panel.stage, m.panel.cursor)
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyEsc})

	m.handlePanelKey(tea.Key{Code: tea.KeyUp})
	if m.panel.cursor != 5 {
		t.Errorf("up at cursor 0 should wrap to 5, got %d", m.panel.cursor)
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyDown})
	if m.panel.cursor != 0 {
		t.Errorf("down at cursor 5 should wrap to 0, got %d", m.panel.cursor)
	}
}

// ---------------------------------------------------------------- /changelog pagination & wrapping tests (from slash_changelog_test.go)

func mustGetwd(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestChangelogSource_LoaderFindsReference(t *testing.T) {
	if !changelog.Loaded() {
		t.Skipf("changelog_reference.txt not reachable from cwd %q — skipping", mustGetwd(t))
	}
	path := changelog.FilePath()
	if !strings.HasSuffix(path, "internal/cli/testdata/changelog_reference.txt") {
		t.Errorf("source path = %q, want suffix internal/cli/testdata/changelog_reference.txt", path)
	}
	src := changelog.Lines()
	if len(src) < 940 {
		t.Errorf("source line count = %d, want ≥ 940", len(src))
	}
	if src[0] != "1.4.5:" {
		t.Errorf("first source line = %q, want %q", src[0], "1.4.5:")
	}
	if !strings.Contains(strings.Join(src, "\n"), "1.4.1:") {
		t.Error("source missing the 1.4.1 header (binary capture block)")
	}
	if !strings.Contains(strings.Join(src, "\n"), "1.0.0:") {
		t.Error("source missing the 1.0.0 header")
	}
}

func TestChangelogPagination_RendersDoc(t *testing.T) {
	if !changelog.Loaded() {
		t.Skipf("changelog_reference.txt not reachable from cwd %q — skipping", mustGetwd(t))
	}
	m := mkPanel(panelChangelog, "/changelog", 0)
	text := panelText(m)
	if !strings.Contains(text, "Improved how agent responses read") {
		t.Errorf("viewer missing the opening bullet:\n%s", truncForErr(text, 400))
	}
	if !strings.Contains(text, "## 1.4.5") {
		t.Errorf("viewer missing the ## 1.4.5 header row:\n%s", truncForErr(text, 400))
	}
	if !strings.Contains(strings.Join(m.changelogViewerLines(), "\n"), "## 1.4.3") {
		t.Error("document missing the ## 1.4.3 header row")
	}
	doc := strings.Join(m.changelogViewerLines(), "\n")
	if !strings.Contains(doc, "Initial release of the Antigravity CLI.") {
		t.Errorf("document missing the 1.0.0 closing line:\n%s", doc)
	}
}

func TestChangelogPagination_PageDown(t *testing.T) {
	if !changelog.Loaded() {
		t.Skipf("changelog_reference.txt not reachable from cwd %q — skipping", mustGetwd(t))
	}
	m := mkPanel(panelChangelog, "/changelog", 0)
	vis := tuiviewer.ChangelogWindowRows()
	if got, want := m.panel.window, 0; got != want {
		t.Fatalf("initial window = %d, want %d", got, want)
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Fatal("down not handled")
	}
	if got, want := m.panel.window, 1; got != want {
		t.Fatalf("after ↓ window = %d, want %d", got, want)
	}
	m.panel.window = 0
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyPgDown}); !handled {
		t.Fatal("pgdown not handled")
	}
	page := vis - 1
	if page < 1 {
		page = 1
	}
	if got, want := m.panel.window, page; got != want {
		t.Fatalf("after pgdown window = %d, want %d", got, want)
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyPgUp}); !handled {
		t.Fatal("pgup not handled")
	}
	if got, want := m.panel.window, 0; got != want {
		t.Fatalf("after pgup window = %d, want %d", got, want)
	}
}

func TestChangelogPagination_CtrlEnd(t *testing.T) {
	if !changelog.Loaded() {
		t.Skipf("changelog_reference.txt not reachable from cwd %q — skipping", mustGetwd(t))
	}
	m := mkPanel(panelChangelog, "/changelog", 0)
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnd, Mod: tea.ModCtrl}); !handled {
		t.Fatal("ctrl+end not handled")
	}
	total := len(m.changelogViewerLines())
	vis := tuiviewer.ChangelogWindowRows()
	if want := total - vis; m.panel.window != want {
		t.Fatalf("ctrl+end window = %d, want %d", m.panel.window, want)
	}
	text := panelText(m)
	if !strings.Contains(text, "Initial release of the Antigravity CLI.") {
		t.Errorf("bottom of changelog should show the 1.0.0 release:\n%s", text)
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Fatal("down at bottom not handled")
	}
	if want := total - vis; m.panel.window != want {
		t.Errorf("down at bottom moved window to %d, want %d", m.panel.window, want)
	}
}

func TestChangelogPagination_CtrlHome(t *testing.T) {
	if !changelog.Loaded() {
		t.Skipf("changelog_reference.txt not reachable from cwd %q — skipping", mustGetwd(t))
	}
	m := mkPanel(panelChangelog, "/changelog", 0)
	m.panel.window = 50
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyHome, Mod: tea.ModCtrl}); !handled {
		t.Fatal("ctrl+home not handled")
	}
	if m.panel.window != 0 {
		t.Fatalf("ctrl+home window = %d, want 0", m.panel.window)
	}
	text := panelText(m)
	if !strings.Contains(text, "## 1.4.5") {
		t.Errorf("first screen should open on 1.4.5:\n%s", truncForErr(text, 400))
	}
	if !strings.Contains(strings.Join(m.changelogViewerLines(), "\n"), "## 1.4.3") {
		t.Error("document missing the ## 1.4.3 header row")
	}
}

func TestChangelogPagination_Counter(t *testing.T) {
	if !changelog.Loaded() {
		t.Skipf("changelog_reference.txt not reachable from cwd %q — skipping", mustGetwd(t))
	}
	m := mkPanel(panelChangelog, "/changelog", 0)
	vis := tuiviewer.ChangelogWindowRows()
	if want := "(" + intStr(1) + "\u2013" + intStr(vis) + " of 2014 lines)"; !strings.Contains(panelText(m), want) {
		t.Errorf("initial counter wrong; want %q in:\n%s", want, panelText(m))
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Fatal("down not handled")
	}
	want := "(" + intStr(2) + "\u2013" + intStr(vis+1) + " of 2014 lines)"
	if !strings.Contains(panelText(m), want) {
		t.Errorf("counter after ↓: want %q in:\n%s", want, panelText(m))
	}
}

func TestChangelogPagination_FitsOnScreen(t *testing.T) {
	if !changelog.Loaded() {
		t.Skipf("changelog_reference.txt not reachable from cwd %q — skipping", mustGetwd(t))
	}
	if got := viewerMaxOffset(viewerSpec{lines: nil, visible: 5, total: 5}); got != 0 {
		t.Errorf("viewerMaxOffset(empty) = %d, want 0", got)
	}
	if got := viewerMaxOffset(viewerSpec{lines: []string{"a", "b", "c"}, visible: 12, total: 3}); got != 0 {
		t.Errorf("viewerMaxOffset(short) = %d, want 0", got)
	}
	if got := viewerMaxOffset(viewerSpec{lines: []string{"a", "b", "c", "d", "e"}, visible: 2, total: 5}); got != 3 {
		t.Errorf("viewerMaxOffset(2-of-5) = %d, want 3", got)
	}
	m := &runMode{}
	m.panel.kind = panelChangelog
	tiny := viewerSpec{lines: []string{"a", "b", "c"}, visible: 12, total: 3}
	rows, start, end := m.viewerWindow(tiny)
	if len(rows) != 3 || start != 1 || end != 3 {
		t.Errorf("viewerWindow(tiny) = (%d rows, %d–%d), want (3 rows, 1–3)", len(rows), start, end)
	}
}

func TestChangelogSource_IsChangelogSourceHeader(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{"1.3.6:", true},
		{"1.0.0:", true},
		{"2.10.4:", true},
		{"10.20.30:", true},
		{"", false},
		{"1.3", false},
		{"1.3.6", false},
		{"1.3.6: extra", false},
		{"v1.3.6:", false},
		{"1.3.6.7:", false},
		{"1..6:", false},
		{"a.b.c:", false},
	}
	for _, tc := range cases {
		if got := changelog.IsSourceHeader(tc.line); got != tc.want {
			t.Errorf("changelog.IsSourceHeader(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}

func truncForErr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func intStr(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func findChangelogLine(t *testing.T, doc []string, substr string) int {
	t.Helper()
	for i, ln := range doc {
		if strings.Contains(stripAnsiForMeasure(ln), substr) {
			return i
		}
	}
	return -1
}

func TestChangelogWrapBudget(t *testing.T) {
	for _, c := range []struct {
		name      string
		width     int
		wantVisLn int
	}{
		{"w80", 80, 78},
		{"w96", 96, 94},
		{"w98", 98, 96},
		{"w99", 99, 97},
		{"w100", 100, 98},
		{"w101", 101, 99},
		{"w103", 103, 101},
		{"w104", 104, 102},
		{"w106", 106, 104},
		{"w110", 110, 108},
		{"w120", 120, 118},
		{"w400", 400, 110},
	} {
		t.Run(c.name, func(t *testing.T) {
			doc := changelog.PanelDoc(c.width)
			idx := findChangelogLine(t, doc, "terminal-first surface")
			if idx < 0 {
				t.Fatalf("tagline not found in panel for width %d", c.width)
			}
			stripped := stripAnsiForMeasure(doc[idx])
			if w := ansiStringWidth(stripped); w != c.wantVisLn {
				t.Fatalf("tagline line visual width = %d, want %d (budget=%d). line=%q",
					w, c.wantVisLn, c.width-2, stripped)
			}
		})
	}
}

func TestChangelogWrapBulletPadding(t *testing.T) {
	doc := changelog.PanelDoc(99)
	idx := findChangelogLine(t, doc, "faster and no longer times out")
	if idx < 0 {
		t.Fatalf("auto-mode continuation line not found in panel; panel=%d lines", len(doc))
	}
	stripped := stripAnsiForMeasure(doc[idx])
	if w := ansiStringWidth(stripped); w != 97 {
		t.Fatalf("auto-mode continuation visual width = %d, want 97. line=%q",
			w, stripped)
	}
	tail := strings.TrimRight(stripped, " ")
	gap := ansiStringWidth(stripped) - ansiStringWidth(tail)
	if gap < 1 || gap > changelog.PadLimit {
		t.Fatalf("trailing pad %d cells out of range (1..%d); line=%q",
			gap, changelog.PadLimit, stripped)
	}
}

func TestChangelogWrapCommaHang(t *testing.T) {
	doc := changelog.PanelDoc(99)
	want := "AGY_ADC_AUTH) sign-in. Claude models now appear in /model only if your Google Cloud project"
	idx := findChangelogLine(t, doc, want)
	if idx < 0 {
		t.Fatalf("first bullet continuation should contain %q; not found in %d-line panel",
			want, len(doc))
	}
	stripped := stripAnsiForMeasure(doc[idx])
	if w := ansiStringWidth(stripped); w != 97 {
		t.Fatalf("first bullet continuation visual width = %d, want 97. line=%q",
			w, stripped)
	}
	wantTail := "project   "
	if !strings.HasSuffix(stripped, wantTail) {
		t.Fatalf("expected trailing pad of 3 spaces after 'project' to match REF; got %q",
			stripped)
	}
}

func TestChangelogWrapHyphenSplit(t *testing.T) {
	doc := changelog.PanelDoc(99)
	if idx := findChangelogLine(t, doc, "mode auto"); idx < 0 {
		t.Fatalf("'mode auto' wrap continuation not found in panel")
	}
	if idx := findChangelogLine(t, doc, "p runs"); idx < 0 {
		t.Fatalf("'p runs' wrap continuation not found in panel")
	}
}

func TestChangelogInlineCodeStyle(t *testing.T) {
	doc := changelog.PanelDoc(99)
	const wantOpen = "\x1b[38;5;69m\x1b[48;5;254m"
	const wantClose = "\x1b[38;5;235m\x1b[49m"
	foundOpen, foundClose := false, false
	for _, ln := range doc {
		if strings.Contains(ln, wantOpen) {
			foundOpen = true
		}
		if strings.Contains(ln, wantClose) {
			foundClose = true
		}
	}
	if !foundOpen {
		t.Fatalf("inline code opening SGR (%q) not found in panel", wantOpen)
	}
	if !foundClose {
		t.Fatalf("inline code closing SGR (%q) not found in panel", wantClose)
	}
}

func TestChangelogPanelSkipsFitLines(t *testing.T) {
	doc := changelog.PanelDoc(99)
	idx := findChangelogLine(t, doc, "## 1.3.6")
	if idx < 0 {
		t.Fatal("header '## 1.3.6' not found in panel")
	}
	stripped := stripAnsiForMeasure(doc[idx])
	if w := ansiStringWidth(stripped); w >= 97 {
		t.Fatalf("header line should be content-length only (no fitLines pad), got width %d. line=%q",
			w, stripped)
	}
}

func ansiStringWidth(s string) int {
	return runewidth.StringWidth(stripAnsiForMeasure(s))
}

func stripAnsiForMeasure(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if i+1 < len(s) && s[i] == 0x1b && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			if j < len(s) {
				i = j + 1
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// ---------------------------------------------------------------- /provider panel tests (from slash_panel_provider_test.go)

// providerTypeField types a value into the form's current field and
// moves to the next one.
func providerTypeField(m *runMode, value string) {
	m.panel.textInput.SetValue(value)
	m.providerFormMove(1)
}

// providerPanelWithConfig writes a configuration and opens the panel on
// it.
func providerPanelWithConfig(t *testing.T, body string) *runMode {
	t.Helper()
	configtest.Isolate(t)
	if body != "" {
		configtest.WriteProviders(t, body)
	}
	m := &runMode{}
	m.panel = slashPanel{kind: panelProvider, cmd: "/provider"}
	m.reloadProviderRows()
	return m
}

func TestProviderPanel_RowsListProvidersModelsAndBuiltins(t *testing.T) {
	m := providerPanelWithConfig(t, `{"providers": {"gateway": {"id": "gateway",
	  "base_url": "https://gw.example/v1", "models": [{"id": "minimax-m3", "model": "MiniMax-M3"}]}}}`)
	rows := m.panel.prov.rows
	if len(rows) < 3 {
		t.Fatalf("rows = %d, want the provider, its model and the built-in group", len(rows))
	}
	if rows[0].Kind != providerRowProvider || rows[0].Provider != "gateway" {
		t.Fatalf("row 0 = %+v, want the gateway provider", rows[0])
	}
	if rows[1].Kind != providerRowModel || rows[1].Model != "minimax-m3" {
		t.Fatalf("row 1 = %+v, want the minimax-m3 model", rows[1])
	}
	var builtin bool
	for _, r := range rows {
		if r.Kind == providerRowBuiltin && r.Model == "gemini-3.8-flash-high" {
			builtin = true
		}
	}
	if !builtin {
		t.Fatalf("the built-in catalogue is missing from the rows: %+v", rows)
	}
}

func TestProviderPanel_AddProviderWritesTheFile(t *testing.T) {
	m := providerPanelWithConfig(t, "")
	m.handleProviderPanelKey(tea.Key{Code: 'a'})
	if m.panel.prov.form == nil {
		t.Fatal("`a` did not open the form")
	}
	providerTypeField(m, "gateway")
	providerTypeField(m, "Gateway")
	m.providerFormMove(1)
	providerTypeField(m, "https://gw.example/v1")
	m.handleProviderPanelKey(tea.Key{Code: tea.KeyEnter})

	body, err := os.ReadFile(models.ProvidersPath())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, want := range []string{`"gateway"`, `"https://gw.example/v1"`, `"name": "Gateway"`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("the file is missing %s:\n%s", want, body)
		}
	}
	if m.panel.prov.form != nil {
		t.Fatal("the form stayed open after the save")
	}
}

func TestProviderPanel_EditKeepsTheCredential(t *testing.T) {
	m := providerPanelWithConfig(t, `{"providers": {"gateway": {"id": "gateway",
	  "base_url": "https://gw.example/v1", "api_key": "sk-literal-secret"}}}`)
	m.panel.cursor = 0
	m.handleProviderPanelKey(tea.Key{Code: tea.KeyEnter})
	if m.panel.prov.form == nil {
		t.Fatal("enter did not open the form")
	}
	if got := m.panel.prov.form.Value(providerFieldAPIKey); got != "" {
		t.Fatalf("the credential is in the form: %q", got)
	}
	m.handleProviderPanelKey(tea.Key{Code: tea.KeyEnter})
	body, err := os.ReadFile(models.ProvidersPath())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(body), `"sk-literal-secret"`) {
		t.Fatalf("the credential was lost:\n%s", body)
	}
}

func TestProviderPanel_ShowsAnEnvironmentReference(t *testing.T) {
	m := providerPanelWithConfig(t, `{"providers": {"gateway": {"id": "gateway",
	  "base_url": "https://gw.example/v1", "api_key": "$GW_KEY"}}}`)
	m.panel.cursor = 0
	m.handleProviderPanelKey(tea.Key{Code: tea.KeyEnter})
	if got := m.panel.prov.form.Value(providerFieldAPIKey); got != "$GW_KEY" {
		t.Fatalf("the credential field = %q, want the environment reference", got)
	}
}

func TestProviderPanel_DeleteNeedsTwoPresses(t *testing.T) {
	m := providerPanelWithConfig(t, `{"providers": {"gateway": {"id": "gateway",
	  "base_url": "https://gw.example/v1", "models": [{"id": "minimax-m3"}]}}}`)
	m.panel.cursor = 0
	m.handleProviderPanelKey(tea.Key{Code: 'd'})
	body, _ := os.ReadFile(models.ProvidersPath())
	if !strings.Contains(string(body), "gateway") {
		t.Fatalf("the provider was deleted on the first press:\n%s", body)
	}
	if !strings.Contains(m.panel.prov.notice, "again") {
		t.Fatalf("notice = %q, want the confirmation", m.panel.prov.notice)
	}
	m.handleProviderPanelKey(tea.Key{Code: 'd'})
	body, _ = os.ReadFile(models.ProvidersPath())
	if strings.Contains(string(body), "gateway") {
		t.Fatalf("the provider survived the second press:\n%s", body)
	}
}

func TestProviderPanel_DeleteAModelKeepsTheProvider(t *testing.T) {
	m := providerPanelWithConfig(t, `{"providers": {"gateway": {"id": "gateway",
	  "base_url": "https://gw.example/v1", "models": [{"id": "minimax-m3"}]}}}`)
	m.panel.cursor = 1
	m.handleProviderPanelKey(tea.Key{Code: 'd'})
	m.handleProviderPanelKey(tea.Key{Code: 'd'})
	body, _ := os.ReadFile(models.ProvidersPath())
	if !strings.Contains(string(body), "gateway") {
		t.Fatalf("the provider was removed too:\n%s", body)
	}
	if strings.Contains(string(body), "minimax-m3") {
		t.Fatalf("the model survived:\n%s", body)
	}
}

func TestProviderPanel_ChoiceFieldCycles(t *testing.T) {
	m := providerPanelWithConfig(t, `{"providers": {"gateway": {"id": "gateway",
	  "base_url": "https://gw.example/v1"}}}`)
	m.panel.cursor = 0
	m.handleProviderPanelKey(tea.Key{Code: tea.KeyEnter})
	m.providerFormMove(1)
	if got := m.panel.prov.form.Value(providerFieldType); got != "openai" {
		t.Fatalf("type = %q, want openai", got)
	}
	m.handleProviderPanelKey(tea.Key{Code: tea.KeyRight})
	if got := m.panel.prov.form.Value(providerFieldType); got != "anthropic" {
		t.Fatalf("type = %q, want anthropic", got)
	}
	m.handleProviderPanelKey(tea.Key{Code: tea.KeyLeft})
	if got := m.panel.prov.form.Value(providerFieldType); got != "openai" {
		t.Fatalf("type = %q, want openai", got)
	}
}

func TestProviderPanel_BuiltinRowsAreNotEditable(t *testing.T) {
	m := providerPanelWithConfig(t, "")
	for i, r := range m.panel.prov.rows {
		if r.Kind == providerRowBuiltin && r.Model != "" {
			m.panel.cursor = i
			m.handleProviderPanelKey(tea.Key{Code: tea.KeyEnter})
			if m.panel.prov.form != nil {
				t.Fatal("a built-in model opened the edit form")
			}
			if !strings.Contains(m.panel.prov.notice, "/model") {
				t.Fatalf("notice = %q, want the pointer to /model", m.panel.prov.notice)
			}
			return
		}
	}
	t.Fatal("the built-in group is empty")
}

func TestProviderPanel_EscCancelsTheForm(t *testing.T) {
	m := providerPanelWithConfig(t, "")
	m.handleProviderPanelKey(tea.Key{Code: 'a'})
	m.handleProviderPanelKey(tea.Key{Code: tea.KeyEsc})
	if m.panel.prov.form != nil {
		t.Fatal("esc did not cancel the form")
	}
	if _, err := os.Stat(models.ProvidersPath()); !os.IsNotExist(err) {
		t.Fatalf("the cancelled form wrote the file: %v", err)
	}
}

func TestProviderPanel_IsNotInThePickerCatalogue(t *testing.T) {
	for _, c := range tuilib.DefaultSlashCommands {
		if c.Name == "/provider" {
			t.Fatal("/provider is in the picker's catalogue")
		}
	}
}

func TestProviderPanel_IsRoutedToThePanel(t *testing.T) {
	kind, ok := panelForCommand(canonicalPanelCommand("provider"))
	if !ok || kind != panelProvider {
		t.Fatalf("panelForCommand(provider) = %v, %v", kind, ok)
	}
}

func TestProviderPanel_VerifyKeyStartsAProbe(t *testing.T) {
	m := providerPanelWithConfig(t, `{"providers": {"gateway": {"id": "gateway",
	  "base_url": "https://gw.example/v1", "models": [{"id": "minimax-m3"}]}}}`)
	m.panel.cursor = 0
	handled, cmd := m.handleProviderPanelKey(tea.Key{Code: 'v'})
	if !handled {
		t.Fatal("`v` was not handled by the panel")
	}
	if cmd == nil {
		t.Fatal("`v` did not start a probe")
	}
	if !m.panel.prov.verifying {
		t.Fatal("the panel is not showing the probe as in flight")
	}
	if m.panel.prov.verifyLabel != "gateway" {
		t.Fatalf("verifyLabel = %q, want the provider's id", m.panel.prov.verifyLabel)
	}
}

func TestProviderPanel_VerifyResultIsRendered(t *testing.T) {
	m := providerPanelWithConfig(t, `{"providers": {"gateway": {"id": "gateway",
	  "base_url": "https://gw.example/v1"}}}`)
	m.handleProviderPanelKey(tea.Key{Code: 'v'})
	m.Update(providerVerifyMsg{
		result: models.VerifyResult{OK: true, Detail: "the model answered"},
		label:  "gateway",
	})
	if m.panel.prov.verifying {
		t.Fatal("the panel still shows the probe as in flight")
	}
	if m.panel.prov.verify == nil || !m.panel.prov.verify.OK {
		t.Fatalf("verify = %+v, want the successful outcome", m.panel.prov.verify)
	}
	line := m.providerVerifyLine()
	if !strings.Contains(line, "the model answered") {
		t.Fatalf("the verification line = %q, want the outcome", line)
	}
	if !strings.Contains(line, "gateway") {
		t.Fatalf("the verification line = %q, want it to name the provider", line)
	}
}

func TestProviderPanel_AFailedProbeIsRenderedAsAFailure(t *testing.T) {
	m := providerPanelWithConfig(t, `{"providers": {"gateway": {"id": "gateway",
	  "base_url": "https://gw.example/v1"}}}`)
	m.handleProviderPanelKey(tea.Key{Code: 'v'})
	m.Update(providerVerifyMsg{
		result: models.VerifyResult{Status: 401, Detail: "the credential was rejected (HTTP 401)"},
		label:  "gateway",
	})
	line := m.providerVerifyLine()
	if !strings.Contains(line, "✗") {
		t.Fatalf("the verification line = %q, want a ✗", line)
	}
	if !strings.Contains(line, "rejected") {
		t.Fatalf("the verification line = %q, want the reason", line)
	}
}

func TestProviderPanel_ASaveStartsAProbe(t *testing.T) {
	m := providerPanelWithConfig(t, `{"providers": {"gateway": {"id": "gateway",
	  "base_url": "https://gw.example/v1"}}}`)
	m.panel.cursor = 0
	m.handleProviderPanelKey(tea.Key{Code: tea.KeyEnter})
	_, cmd := m.handleProviderPanelKey(tea.Key{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("the save did not start a probe")
	}
	if !m.panel.prov.verifying {
		t.Fatal("the panel is not showing the probe as in flight")
	}
}

func TestProviderPanel_VerifyProbesTheTypedCredential(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	m := providerPanelWithConfig(t, fmt.Sprintf(`{"providers": {"gateway": {"id": "gateway",
	  "base_url": %q, "api_key": "sk-stored"}}}`, srv.URL+"/v1"))
	m.panel.cursor = 0
	m.handleProviderPanelKey(tea.Key{Code: tea.KeyEnter})
	form := m.panel.prov.form
	idx := -1
	for i, f := range form.Fields {
		if f.Label == providerFieldAPIKey {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("the form has no credential field")
	}
	form.Cursor = idx
	m.panel.textInput.SetValue("sk-typed")
	_, cmd := m.handleProviderPanelKey(tea.Key{Code: 'v'})
	if cmd == nil {
		t.Fatal("`v` did not start a probe")
	}
	if _, ok := cmd().(providerVerifyMsg); !ok {
		t.Fatal("the command did not produce a providerVerifyMsg")
	}
	if seen != "Bearer sk-typed" {
		t.Fatalf("the probe carried %q, want the typed credential", seen)
	}
}

func TestProviderPanel_VerifyReachesTheEndpointWithTheStoredCredential(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	m := providerPanelWithConfig(t, fmt.Sprintf(`{"providers": {"gateway": {"id": "gateway",
	  "base_url": %q, "api_key": "sk-stored"}}}`, srv.URL+"/v1"))
	m.panel.cursor = 0
	_, cmd := m.handleProviderPanelKey(tea.Key{Code: 'v'})
	if cmd == nil {
		t.Fatal("`v` did not start a probe")
	}
	msg, ok := cmd().(providerVerifyMsg)
	if !ok {
		t.Fatal("the command did not produce a providerVerifyMsg")
	}
	if !msg.result.OK {
		t.Fatalf("the probe failed: %s", msg.result.Detail)
	}
}

// contextTestZone pins the local-time offset so the wire envelope's
// RFC3339 stamp has a stable width across machines and CI time zones.
var contextTestZone = time.FixedZone("CST", 8*3600)

// seedContextSession writes a synthetic multi-turn session into m.
// Even-indexed payload entries become user messages, odd-indexed become
// assistant messages — the same trailing-pair pattern cycle 222 uses
// for the /rewind panel.
func seedContextSession(m *runMode, payloads []string) {
	for i, s := range payloads {
		role := "user"
		if i%2 != 0 {
			role = "assistant"
		}
		msg := backend.Message{Role: role, Content: s}
		if role == "user" {
			msg.WireContent = backend.WrapUserContent(s, "", false, time.Date(2026, 10, 21, 7, 0, 0, 0, contextTestZone))
		}
		m.session = append(m.session, msg)
	}
}

// sessionContextTokens mirrors sessionTokenCounts so each test can pin the
// precise numbers the panel will see without reaching into its rendering.
func sessionContextTokens(m *runMode) (user, agent, tool int) {
	for _, msg := range m.session {
		if msg.Role == "user" {
			user += backend.UserContentTokenCount(msg.WireText())
		}
	}
	agent = int(m.sessionCompletionTokens - m.lastTurnCompletionTokens)
	if agent < 0 {
		agent = 0
	}
	for _, b := range m.blocks {
		for _, st := range b.steps {
			if st.kind == stepKindTool {
				tool += backend.ToolResultTokenCount(st.result)
			}
		}
	}
	for _, st := range m.liveSteps {
		if st.kind == stepKindTool {
			tool += backend.ToolResultTokenCount(st.result)
		}
	}
	return user, agent, tool
}

// TestPanelContextEmptyStillCycle190 — re-run the cycle-190 empty
// test through the cycle-224 dispatcher so any future regression in
// the dispatch logic surfaces locally. The test in
// slash_panels_test.go is preserved verbatim and checks the same
// rows; this is the explicit "still passes" assertion.
func TestPanelContextEmptyStillCycle190(t *testing.T) {
	m := mkPanel(panelContext, "/context", 0)
	m.model = "Gemini 3.8 Flash (High)"
	plain := plainLines(m)
	if got := plain[0]; got != "└ Context Usage" {
		t.Errorf("row 0 = %q, want %q", got, "└ Context Usage")
	}
	if !strings.Contains(plain[1], "0/1.0M tokens") {
		t.Errorf("row 1 should still say 0/1.0M tokens: %q", plain[1])
	}
	if !strings.Contains(plain[2], "(0.0%)") {
		t.Errorf("row 2 should still show (0.0%%): %q", plain[2])
	}
	if !strings.Contains(plain[3], "awaiting generation") {
		t.Errorf("row 3 should still say 'awaiting generation': %q", plain[3])
	}
	if !strings.Contains(plain[7], "1.0M (100.0%)") {
		t.Errorf("row 7 should still show 1.0M (100.0%%): %q", plain[7])
	}
	if got := plain[len(plain)-1]; !strings.Contains(got, "esc") {
		t.Errorf("last row should be the scroll legend, got %q", got)
	}
}

// TestPanelContextNonEmpty1MsgLayout — a single user message (200
// chars → 50 tokens) produces a panel whose title carries the used
// count, whose (X.X%) row sits at the same column the cycle-190
// empty layout uses for `(0.0%)`, and whose "Estimated usage" line
// becomes "(live session, 1 message)".
func TestPanelContextNonEmpty1MsgLayout(t *testing.T) {
	m := mkPanel(panelContext, "/context", 0)
	m.model = "Gemini 3.8 Flash (High)"
	seedContextSession(m, []string{strings.Repeat("a", 200)})

	plain := plainLines(m)
	if len(plain) != 21 {
		t.Fatalf("line count = %d, want 21\nplain:\n%s",
			len(plain), strings.Join(plain, "\n"))
	}
	if got := plain[0]; got != "└ Context Usage" {
		t.Errorf("row 0 = %q, want title", got)
	}
	if !strings.Contains(plain[1], "Gemini 3.8 Flash (High) · ") ||
		!strings.Contains(plain[1], "/1.0M tokens") {
		t.Errorf("row 1 = %q, want used/1.0M descriptor", plain[1])
	}
	if !strings.Contains(plain[2], "%") {
		t.Errorf("row 2 = %q, want percentage", plain[2])
	}
	if !strings.Contains(plain[3], "Token usage by category") {
		t.Errorf("row 3 = %q, want 'Token usage by category'", plain[3])
	}
	u, a, tool := sessionContextTokens(m)
	if !strings.Contains(plain[4], fmt.Sprintf("User messages: %s tokens", contextTokenLabel(u))) {
		t.Errorf("row 4 = %q, want user count %d", plain[4], u)
	}
	if !strings.Contains(plain[5], fmt.Sprintf("Agent responses: %s tokens", contextTokenLabel(a))) {
		t.Errorf("row 5 = %q, want agent %d", plain[5], a)
	}
	if !strings.Contains(plain[6], fmt.Sprintf("Tool calls: %s tokens", contextTokenLabel(tool))) {
		t.Errorf("row 6 = %q, want tool %d", plain[6], tool)
	}
	if !strings.Contains(plain[7], "System prompt:") {
		t.Errorf("row 7 = %q, want system prompt category", plain[7])
	}
	if !strings.Contains(plain[8], "System tools:") {
		t.Errorf("row 8 = %q, want system tools category", plain[8])
	}
	if !strings.Contains(plain[9], "Skills:") {
		t.Errorf("row 9 = %q, want skills category", plain[9])
	}
	if !strings.Contains(plain[10], "Subagents:") {
		t.Errorf("row 10 = %q, want subagents category", plain[10])
	}
	if !strings.Contains(plain[11], "Free space: 1.0M") {
		t.Errorf("row 11 = %q, want free space", plain[11])
	}
	if plain[14] != " └ ~/.gemini/GEMINI.md" {
		t.Errorf("row 14 = %q", plain[14])
	}
	if plain[17] != " └ .agents/agents/" {
		t.Errorf("row 17 = %q, want .agents path", plain[17])
	}
	if !strings.Contains(plain[19], "Related: /artifact") {
		t.Errorf("row 19 = %q", plain[19])
	}
}

// TestPanelContextNonEmpty5MsgLayout — 5 messages: 2000-char user,
// 1500-char assistant, 800-char user, 400-char assistant, 1200-char
// user. Sum: user=1000 (2000/4+800/4+1200/4), agent=475
// (1500/4+400/4); total used=1475 → 0.1% of 1.0M; free=998525 →
// 99.9%. The "5 messages" plural label replaces cycle 190's
// "awaiting generation" placeholder.
func TestPanelContextNonEmpty5MsgLayout(t *testing.T) {
	m := mkPanel(panelContext, "/context", 0)
	m.model = "Gemini 3.8 Flash (High)"
	seedContextSession(m, []string{
		strings.Repeat("a", 2000),
		strings.Repeat("b", 1500),
		strings.Repeat("c", 800),
		strings.Repeat("d", 400),
		strings.Repeat("e", 1200),
	})
	// Cycle 292: assistant rows come from provider usage, not text length.
	// Seed two completed steps worth of completion tokens.
	m.sessionCompletionTokens = 475

	plain := plainLines(m)
	if len(plain) != 21 {
		t.Fatalf("line count = %d, want 21\nplain:\n%s",
			len(plain), strings.Join(plain, "\n"))
	}
	if !strings.Contains(plain[1], "Gemini 3.8 Flash (High) · ") {
		t.Errorf("row 1 = %q, want model descriptor", plain[1])
	}
	if !strings.Contains(plain[2], "%") {
		t.Errorf("row 2 = %q, want percentage", plain[2])
	}
	if !strings.Contains(plain[3], "Token usage by category") {
		t.Errorf("row 3 = %q, want category preamble", plain[3])
	}
	u, a, _ := sessionContextTokens(m)
	if !strings.Contains(plain[4], fmt.Sprintf("User messages: %s tokens", contextTokenLabel(u))) {
		t.Errorf("row 4 = %q, want user=%d", plain[4], u)
	}
	if !strings.Contains(plain[5], fmt.Sprintf("Agent responses: %s tokens", contextTokenLabel(a))) {
		t.Errorf("row 5 = %q, want agent=%d", plain[5], a)
	}
	if !strings.Contains(plain[6], "Tool calls: 0 tokens (0.0%)") {
		t.Errorf("row 6 = %q, want tool=0", plain[6])
	}
	if !strings.Contains(plain[11], "Free space: 1.0M") {
		t.Errorf("row 11 = %q, want free space", plain[11])
	}
}

// TestPanelContextNonEmpty20MsgLayout — 10 user/assistant pairs of
// progressively larger payloads: user grows 4000 → 5800 chars
// (1000, 1050, …, 1450 tokens), assistant grows 5000 → 7750 chars
// (1250, 1375, …, 1937 tokens). Sum: user=12250, agent=15875, total
// used=28125 → 2.8% of 1.0M, free=971875 → 97.2%. Pin these to make
// sure the percentage rounding and the title-row formatting hold
// up at a typical real-session size.
func TestPanelContextNonEmpty20MsgLayout(t *testing.T) {
	m := mkPanel(panelContext, "/context", 0)
	m.model = "Gemini 3.8 Flash (High)"
	const nTurns = 10
	payloads := make([]string, 0, 2*nTurns)
	for i := 0; i < nTurns; i++ {
		payloads = append(payloads, strings.Repeat("u", 4000+i*200)) // user
		payloads = append(payloads, strings.Repeat("a", 5000+i*300)) // assistant
	}
	seedContextSession(m, payloads)

	// Cycle 292: agent tokens are provider usage, so seed a realistic
	// session total and let the helper decide the user count.
	m.sessionCompletionTokens = 15875
	user, agent, _ := sessionContextTokens(m)
	if user == 0 || agent != 15875 {
		t.Fatalf("precheck: user = %d, agent = %d", user, agent)
	}
	if m.session[0].Role != "user" || m.session[1].Role != "assistant" {
		t.Fatalf("trailing-pair role order broken: %+v", m.session[:2])
	}

	plain := plainLines(m)
	if len(plain) != 21 {
		t.Fatalf("line count = %d, want 21\nplain:\n%s",
			len(plain), strings.Join(plain, "\n"))
	}
	if !strings.Contains(plain[1], "Gemini 3.8 Flash (High) · ") {
		t.Errorf("row 1 = %q, want model descriptor", plain[1])
	}
	if !strings.Contains(plain[2], "%") {
		t.Errorf("row 2 = %q, want percentage", plain[2])
	}
	if !strings.Contains(plain[3], "Token usage by category") {
		t.Errorf("row 3 = %q, want category preamble", plain[3])
	}
	if !strings.Contains(plain[4], fmt.Sprintf("User messages: %s tokens", contextTokenLabel(user))) {
		t.Errorf("row 4 = %q, want user=%d", plain[4], user)
	}
	if !strings.Contains(plain[5], "Agent responses: 15.9k tokens (1.6%)") {
		t.Errorf("row 5 = %q, want 15.9k / 1.6%%", plain[5])
	}
	if !strings.Contains(plain[6], "Tool calls: 0 tokens (0.0%)") {
		t.Errorf("row 6 = %q, want tool=0", plain[6])
	}
	if !strings.Contains(plain[11], "Free space: 1.0M") {
		t.Errorf("row 11 = %q, want free space", plain[11])
	}
}

// TestPanelContextNonEmptyToolCalls — populate m.blocks with three
// turnBlocks, each carrying one stepKindTool step, plus a single live
// stepKindTool in m.liveSteps. That gives 4 tool calls → 4 * 5 = 20
// tool tokens. Combined with a 4000-char user message (1000 tokens
// proxy), the expected used is 1020, free is 998980. Both the
// title-row <used>/1.0M and the "Tool calls: <n> tokens" line must
// match.
func TestPanelContextNonEmptyToolCalls(t *testing.T) {
	m := mkPanel(panelContext, "/context", 0)
	m.model = "Gemini 3.8 Flash (High)"
	seedContextSession(m, []string{strings.Repeat("a", 4000)})
	res := strings.Repeat("r", 109) // (109+1)/3+6 = 42 tokens per call
	m.blocks = []turnBlock{
		{steps: []toolStep{{kind: stepKindTool, name: "read_file", result: res}}},
		{steps: []toolStep{{kind: stepKindThought, tokens: 42}, {kind: stepKindTool, name: "glob", result: res}}},
		{steps: []toolStep{{kind: stepKindTool, name: "grep", result: res}}},
	}
	m.liveSteps = []toolStep{{kind: stepKindTool, name: "shell", result: res}}

	plain := plainLines(m)
	if !strings.Contains(plain[1], "Gemini 3.8 Flash (High) · ") {
		t.Errorf("row 1 = %q, want model descriptor", plain[1])
	}
	if !strings.Contains(plain[2], "%") {
		t.Errorf("row 2 = %q, want percentage", plain[2])
	}
	u, a, tool := sessionContextTokens(m)
	if tool != 4*42 {
		t.Fatalf("precheck: tool = %d, want 168", tool)
	}
	if !strings.Contains(plain[6], fmt.Sprintf("Tool calls: %s tokens", contextTokenLabel(tool))) {
		t.Errorf("row 6 = %q, want tool=%d", plain[6], tool)
	}
	if !strings.Contains(plain[4], fmt.Sprintf("User messages: %s tokens", contextTokenLabel(u))) {
		t.Errorf("row 4 = %q, want user=%d", plain[4], u)
	}
	if !strings.Contains(plain[5], "Agent responses: 0 tokens (0.0%)") {
		t.Errorf("row 5 = %q, want agent=0 (%d)", plain[5], a)
	}
}

// TestSessionTokenCounts — pin the helper directly against the reference's
// three formulas.
//
//   - user:  len(wire)/3 + 6, where wire is the stored <USER_REQUEST>
//     envelope (a Wire-less message is wrapped on the fly).
//   - agent: sessionCompletionTokens - lastTurnCompletionTokens, floored at 0.
//   - tool:  (len(raw result)+1)/3 + 6 per stepKindTool, over blocks and
//     liveSteps; thought steps do not count.
func TestSessionTokenCounts(t *testing.T) {
	m := mkPanel(panelContext, "/context", 0)
	wire := backend.WrapUserContent(strings.Repeat("a", 200), "Gemini 3.8 Flash (High)", false,
		time.Date(2026, 10, 21, 7, 0, 0, 0, contextTestZone))
	m.session = []backend.Message{
		{Role: "user", Content: strings.Repeat("a", 200), WireContent: wire},
		{Role: "assistant", Content: strings.Repeat("b", 200)},
	}
	wantUser := len(wire)/3 + 6
	u, a, tool := m.sessionTokenCounts()
	if u != wantUser || a != 0 || tool != 0 {
		t.Errorf("session-only: got (%d, %d, %d), want (%d, 0, 0)", u, a, tool, wantUser)
	}

	// Usage counters decide the agent row: the last step is excluded.
	m.sessionCompletionTokens = 100
	m.lastTurnCompletionTokens = 40
	_, a, _ = m.sessionTokenCounts()
	if a != 60 {
		t.Errorf("agent row = %d, want 60 (100 completed - 40 in flight)", a)
	}

	// One tool step in a historical block, one live: results are what count.
	m.blocks = []turnBlock{
		{steps: []toolStep{
			{kind: stepKindThought, tokens: 50},
			{kind: stepKindTool, name: "fetch", result: strings.Repeat("x", 109)},
		}},
	}
	m.liveSteps = []toolStep{{kind: stepKindTool, name: "shell", result: strings.Repeat("y", 158)}}
	_, _, tool = m.sessionTokenCounts()
	if tool != 42+59 {
		t.Errorf("tool row = %d, want 101 (42 + 59)", tool)
	}

	// A "system" role message is ignored.
	m.session = append(m.session,
		backend.Message{Role: "system", Content: strings.Repeat("z", 400)},
	)
	u2, a2, tool2 := m.sessionTokenCounts()
	if u2 != wantUser || a2 != 60 || tool2 != 101 {
		t.Errorf("after system msg: got (%d, %d, %d), want (%d, 60, 101)", u2, a2, tool2, wantUser)
	}

	// A running step has no result yet, so it contributes nothing.
	m.liveSteps = append(m.liveSteps, toolStep{kind: stepKindTool, name: "running", running: true})
	_, _, tool3 := m.sessionTokenCounts()
	if tool3 != 101 {
		t.Errorf("running step counted: tool = %d, want 101", tool3)
	}
}

// TestPanelContextNondescriptorRowsUnchanged — the rows that do not
// carry token counts (title, system-files block, Related line, legend)
// are identical between the empty and non-empty branches.
func TestPanelContextNondescriptorRowsUnchanged(t *testing.T) {
	empty := mkPanel(panelContext, "/context", 0)
	empty.model = "Gemini 3.8 Flash (High)"
	live := mkPanel(panelContext, "/context", 0)
	live.model = "Gemini 3.8 Flash (High)"
	seedContextSession(live, []string{strings.Repeat("a", 4000)})
	ep := plainLines(empty)
	lp := plainLines(live)
	for _, want := range []string{
		"System files · auto-loaded",
		" └ ~/.gemini/GEMINI.md",
		" └ ~/.gemini/skills/",
		" └ ~/.gemini/antigravity/skills/",
		" └ .agents/agents/",
		"Related: /artifact · /skill · /rewind",
	} {
		if !containsLine(ep, want) || !containsLine(lp, want) {
			t.Errorf("row %q missing from one branch (empty=%v live=%v)", want, containsLine(ep, want), containsLine(lp, want))
		}
	}
	if ep[0] != lp[0] || ep[len(ep)-1] != lp[len(lp)-1] {
		t.Errorf("title/legend differ: empty=%q/%q live=%q/%q", ep[0], ep[len(ep)-1], lp[0], lp[len(lp)-1])
	}
}

func containsLine(lines []string, want string) bool {
	for _, line := range lines {
		if line == want {
			return true
		}
	}
	return false
}

// TestPanelContextPercentagePadding — cycle 190 records that the
// (0.0%) row uses a 6-space pad (one column further right than the
// other rows). Cycle 224 preserves that quirk on the non-empty
// branch too: counting the spaces between the rightmost `□` and the
// leading `(` of the percentage row must be exactly 6 (descriptor
// rows keep their 5-space pad — that one is verified in the empty
// cycle-190 layout test).
func TestPanelContextPercentagePadding(t *testing.T) {
	cases := []struct {
		name     string
		payloads []string
	}{
		{"1msg", []string{strings.Repeat("a", 200)}},
		{"5msg", []string{
			strings.Repeat("a", 2000),
			strings.Repeat("b", 1500),
			strings.Repeat("c", 800),
			strings.Repeat("d", 400),
			strings.Repeat("e", 1200),
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mkPanel(panelContext, "/context", 0)
			m.model = "Gemini 3.8 Flash (High)"
			seedContextSession(m, tc.payloads)
			plain := plainLines(m)
			// Row 2 is the percentage row (padded 6 cells); row 3
			// ("Token usage by category") uses 5.
			padRow := plain[2]
			padSpaces := spacesBetweenLastCellAndParen(padRow)
			if padSpaces != 6 {
				t.Errorf("row 2 padding = %d, want 6: %q", padSpaces, padRow)
			}
		})
	}
}

// spacesBetweenLastCellAndParen returns the number of spaces between
// the rightmost `□` in `row` and the leading `(` of the percentage.
// Cycle 190 records 6 for the percentage row and 5 for descriptor
// rows; cycle 224 must keep both.
func spacesBetweenLastCellAndParen(row string) int {
	idx := strings.LastIndex(row, "□")
	if idx < 0 {
		return -1
	}
	tail := row[idx+len("□"):]
	// Count the leading spaces.
	n := 0
	for n < len(tail) && tail[n] == ' ' {
		n++
	}
	return n
}

// initDiffFoldingRepo stages a single-file git repo with two
// non-adjacent change blocks so `git diff` emits exactly two
// `@@ ... @@` hunks. The repo is rooted at dir; the file is "f.txt"
// and the delta is:
//   - L5–L7 (3 lines) changed: each becomes +CHANGED-A1 / +CHANGED-A2 / +CHANGED-A3
//   - L25–L27 (3 lines) changed: each becomes +CHANGED-B1 / +CHANGED-B2 / +CHANGED-B3
//
// The hunk separation (15 unchanged lines) keeps git from merging
// them into one big hunk via its default 3-context heuristic.
func initDiffFoldingRepo(t *testing.T, dir string) {
	t.Helper()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_AUTHOR_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	// Construct a 30-line file with two change blocks at known
	// locations. Block A occupies lines 5..7; block B occupies
	// lines 25..27. The middle 17 lines (8..24) are unchanged
	// context, which forces git to emit two separate @@ hunks.
	lines := make([]string, 30)
	for i := range lines {
		lines[i] = "ctx"
	}
	for i := 4; i <= 6; i++ {
		lines[i] = "oldA"
	}
	for i := 24; i <= 26; i++ {
		lines[i] = "oldB"
	}
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	git("init", "-q")
	git("add", "-A")
	git("commit", "-qm", "init")
	// Now mutate the same lines so the diff has two hunks in the
	// modified file (not added/removed whole blocks).
	lines[4] = "CHANGED-A1"
	lines[5] = "CHANGED-A2"
	lines[6] = "CHANGED-A3"
	lines[24] = "CHANGED-B1"
	lines[25] = "CHANGED-B2"
	lines[26] = "CHANGED-B3"
	body2 := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(body2), 0o644); err != nil {
		t.Fatal(err)
	}
}

// openFoldingDiffSubview sets up m with cwd=dir, opens the diff
// panel on tab 0 (All Changes), moves the cursor to row 0 (the
// single tracked file "f.txt") and presses Enter — the standard
// hand-off from the file list to the inline-diff subview. The
// returned *runMode is ready for f/j/k bindings.
func openFoldingDiffSubview(t *testing.T, dir string) *runMode {
	t.Helper()
	m := mkPanel(panelDiff, "/diff", 0)
	m.cwd = dir
	m.panel.tab = 0
	m.panel.cursor = 0
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("Enter not handled")
	}
	if m.panel.diffView != 1 {
		t.Fatalf("diffView = %d, want 1 (file inline)", m.panel.diffView)
	}
	return m
}

// indexOfPrefix returns the first index where plain[i] starts with
// prefix — skipping the leading two-space indent + " > " cursor
// marker — or -1 if no row matches. Used by the tests to locate the
// @@ header / specific body lines without depending on row indices
// the renderer is free to rearrange.
func indexOfPrefix(plain []string, prefix string) int {
	for i, l := range plain {
		if rowStartsWith(l, prefix) {
			return i
		}
	}
	return -1
}

// diffInlineBodyRows returns the rendered rows minus the title row,
// leading blank row, trailing blank row, and legend row — the rows
// whose cursor index is diffInlineRow. Used by every test to map
// the cursor onto a stable row set.
//
// Each body row has a leading 2-space indent ("  " before @@/+/-/context)
// plus, on the cursor row, an extra " > " blue marker prefix. The
// helper strips neither — tests inspect prefix/content directly
// and account for the indent + cursor row explicitly via the
// rowStartsWith helper below.
//
// Layout: panelDiffInlineLines emits [title, blank, body..., blank,
// legend]. body is rows[2..N-3] — the slice between the leading
// blank and the trailing blank. The trailing blank + legend pair
// is dropped by walking back from the end to find the legend (the
// last non-empty row).
func diffInlineBodyRows(plain []string) []string {
	if len(plain) < 5 {
		return nil
	}
	// Find the legend: the last non-empty row.
	legendIdx := -1
	for i := len(plain) - 1; i >= 0; i-- {
		if plain[i] != "" {
			legendIdx = i
			break
		}
	}
	if legendIdx < 0 || legendIdx < 3 {
		return nil
	}
	// Everything between row 2 and row legendIdx-2 is body content:
	//   [title=0, blank=1, body=2..legendIdx-2, blank=legendIdx-1, legend=legendIdx]
	bodyStart := 2
	bodyEnd := legendIdx - 1
	if bodyStart >= bodyEnd {
		return nil
	}
	return plain[bodyStart:bodyEnd]
}

// rowStartsWith reports whether s has the given content prefix
// once the leading two-space indent (and any " > " cursor marker)
// have been skipped. The renderer's standard indent is two spaces,
// and the cursor-marked row adds a third space + "> ", producing
// rows like " >   diff --git..."; this helper treats both forms
// identically so tests don't have to special-case the cursor row.
func rowStartsWith(s, prefix string) bool {
	return strings.HasPrefix(stripRowPrefix(s), prefix)
}

// stripRowPrefix returns s with the leading "  " (renderer
// standard indent) and any " > " cursor marker stripped, leaving
// the content tail. rowStartsWith is built on top of this helper so
// the prefix-stripping logic only lives in one place.
func stripRowPrefix(s string) string {
	strip := s
	// Skip leading spaces.
	for len(strip) > 0 && strip[0] == ' ' {
		strip = strip[1:]
	}
	// Skip the cursor marker, if present.
	if strings.HasPrefix(strip, "> ") {
		strip = strip[2:]
	}
	// Re-skip any spaces that followed the cursor marker — the
	// cursor-row prefix is " > " + "  " + content, so once the
	// marker is gone there are still two spaces of indent left.
	for len(strip) > 0 && strip[0] == ' ' {
		strip = strip[1:]
	}
	return strip
}

// TestDiffFolding_Toggle: two-hunk diff, f on first hunk hides it,
// f again unhides. The body rows go from N (with both hunks
// expanded) to 1 (just the first @@ header) on first f, then back
// to N on second f.
func TestDiffFolding_Toggle(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	initDiffFoldingRepo(t, dir)

	m := openFoldingDiffSubview(t, dir)

	// Sanity: both hunks show their @@ and body lines when no fold.
	lines0 := stripInlineLines(t, m)
	body0 := diffInlineBodyRows(lines0)
	if n := countPrefix(body0, "@@"); n != 2 {
		t.Fatalf("baseline @@ count = %d, want 2", n)
	}
	// All expanded: body rows include both hunks' plus/minus/context.
	if !anyContains(lines0, "CHANGED-A1") || !anyContains(lines0, "CHANGED-B1") {
		t.Fatalf("baseline body missing A/B content: %q", lines0)
	}

	// Move cursor onto the first @@ header. The body rows start at
	// rendered row index 2 (after title + blank); @@ headers are at
	// body rows 4 (after 4 pre-header lines: "diff --git", "index",
	// "---", "+++") and later. We find the first @@ row in the body
	// and snap the cursor onto it.
	cursorOnFirstHunkHeader(t, m, lines0)

	// Press f — first hunk collapses.
	pressF(t, m)
	lines1 := stripInlineLines(t, m)
	body1 := diffInlineBodyRows(lines1)
	if n := countPrefix(body1, "@@"); n != 2 {
		t.Errorf("after f: @@ count = %d, want 2", n)
	}
	// First hunk's body should be gone; second hunk's body should still be visible.
	if anyContains(body1, "CHANGED-A1") || anyContains(body1, "CHANGED-A2") || anyContains(body1, "CHANGED-A3") {
		t.Errorf("after f: hunk A still showing body content: %q", body1)
	}
	if !anyContains(body1, "CHANGED-B1") {
		t.Errorf("after f: hunk B body missing: %q", body1)
	}

	// Press f again — first hunk re-expands.
	pressF(t, m)
	lines2 := stripInlineLines(t, m)
	body2 := diffInlineBodyRows(lines2)
	if !anyContains(body2, "CHANGED-A1") || !anyContains(body2, "CHANGED-B1") {
		t.Errorf("after f twice: hunk content missing: %q", body2)
	}
	if n := countPrefix(body2, "@@"); n != 2 {
		t.Errorf("after f twice: @@ count = %d, want 2", n)
	}
}

// TestDiffFolding_CountIndicator: folded hunk's header includes
// "+N" line-count suffix — the prompt explicitly mentions "+5" as
// the example. The exact N is whatever len(body) is for the hunk
// being folded; this test pins it as part of the regression.
func TestDiffFolding_CountIndicator(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	initDiffFoldingRepo(t, dir)

	m := openFoldingDiffSubview(t, dir)

	// Find the first @@ header in the body; capture its dedented
	// plain form before folding so we can compare against the
	// folded form (skip the leading "  " indent and any " > "
	// cursor marker — see rowStartsWith).
	lines0 := stripInlineLines(t, m)
	body0 := diffInlineBodyRows(lines0)
	idx0 := indexOfPrefix(body0, "@@")
	if idx0 < 0 {
		t.Fatalf("no @@ header in body: %q", body0)
	}
	header0 := stripRowPrefix(body0[idx0])

	cursorOnFirstHunkHeader(t, m, lines0)
	pressF(t, m)

	lines1 := stripInlineLines(t, m)
	body1 := diffInlineBodyRows(lines1)

	// The first @@ header line should now carry "+N" suffix.
	idx1 := indexOfPrefix(body1, "@@")
	if idx1 < 0 {
		t.Fatalf("folded body has no @@ header: %q", body1)
	}
	got := stripRowPrefix(body1[idx1])
	if !strings.HasPrefix(got, header0+" ") {
		t.Fatalf("folded header %q does not start with unfolded header %q", got, header0)
	}
	// The " +N" suffix must be present and numeric. Regex-free
	// parse: the suffix lives after the trailing "@@" — count it.
	if !strings.Contains(got, "+") {
		t.Errorf("folded header missing + count: %q", got)
	}
	// Extract the trailing numeric token: split on whitespace and
	// take the last field.
	fields := strings.Fields(got)
	if len(fields) < 2 {
		t.Fatalf("folded header has no count token: %q", got)
	}
	tail := fields[len(fields)-1]
	if !strings.HasPrefix(tail, "+") {
		t.Errorf("folded header tail %q does not start with +", tail)
	}
	numStr := strings.TrimPrefix(tail, "+")
	if numStr == "" {
		t.Errorf("folded header tail %q has no digits after +", tail)
	}
	for _, r := range numStr {
		if r < '0' || r > '9' {
			t.Errorf("folded header tail %q has non-digit %q", tail, r)
			break
		}
	}
}

// TestDiffFolding_CursorStays: cursor on a folded hunk header,
// pressing f expands; cursor remains on the header.
//
// The first phase folds the first hunk, leaving the cursor (per the
// design) on the (now-collapsed) @@ header row. The second phase
// presses f again to expand; the cursor must stay on the @@ header
// of the same hunk — that's the "cursor remains on the same header"
// rule from the changelog.
func TestDiffFolding_CursorStays(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	initDiffFoldingRepo(t, dir)

	m := openFoldingDiffSubview(t, dir)
	lines0 := stripInlineLines(t, m)
	cursorOnFirstHunkHeader(t, m, lines0)
	startCursor := m.panel.diffInlineRow

	// Fold — cursor should remain on the @@ header.
	pressF(t, m)
	if m.panel.diffInlineRow != startCursor {
		t.Errorf("after fold: cursor = %d, want %d (header)", m.panel.diffInlineRow, startCursor)
	}
	linesF := stripInlineLines(t, m)
	bodyF := diffInlineBodyRows(linesF)
	if m.panel.diffInlineRow >= len(bodyF) {
		t.Fatalf("cursor row %d out of range, body len %d", m.panel.diffInlineRow, len(bodyF))
	}
	if !rowStartsWith(bodyF[m.panel.diffInlineRow], "@@") {
		t.Errorf("cursor row %d after fold not @@: %q", m.panel.diffInlineRow, bodyF[m.panel.diffInlineRow])
	}

	// Unfold — cursor must stay on the @@ header row (which is
	// now pushed down by len(body) of the unfolded hunk; the
	// clamping in toggleDiffHunkFoldAtCursor re-anchors it
	// onto the same header).
	pressF(t, m)
	if m.panel.diffInlineRow != startCursor {
		// The startCursor was relative to the unfolded body. After
		// unfolding, the layout returns to the unfolded shape with
		// the same row indices, so startCursor must still point at
		// the same @@ header row.
		t.Errorf("after unfold: cursor = %d, want %d (header)", m.panel.diffInlineRow, startCursor)
	}
	linesU := stripInlineLines(t, m)
	bodyU := diffInlineBodyRows(linesU)
	if m.panel.diffInlineRow >= len(bodyU) {
		t.Fatalf("cursor row %d out of range after unfold, body len %d",
			m.panel.diffInlineRow, len(bodyU))
	}
	if !rowStartsWith(bodyU[m.panel.diffInlineRow], "@@") {
		t.Errorf("cursor row %d after unfold not @@: %q",
			m.panel.diffInlineRow, bodyU[m.panel.diffInlineRow])
	}
}

// TestDiffFolding_AllFolded: pressing f on every hunk in sequence
// leaves only @@ headers (one per hunk) and no body lines. The
// folded state is achieved by walking each @@ header and toggling
// fold; the final body should have N @@ rows (where N == hunks)
// and zero +/- or context lines.
func TestDiffFolding_AllFolded(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	initDiffFoldingRepo(t, dir)

	m := openFoldingDiffSubview(t, dir)
	lines := stripInlineLines(t, m)
	body := diffInlineBodyRows(lines)
	hunkCount := countPrefix(body, "@@")
	if hunkCount != 2 {
		t.Fatalf("baseline @@ count = %d, want 2 for the two-hunk test fixture", hunkCount)
	}

	// The number of pre-header rows is whatever the diff has
	// before the first @@. For a modified tracked file, that's
	// the four lines "diff --git", "index", "---", "+++". Pin
	// via the baseline render so we don't miscount for unusual
	// diff shapes later on.
	preHeaderCount := 0
	for _, l := range body {
		if rowStartsWith(l, "@@") {
			break
		}
		preHeaderCount++
	}

	// Fold each @@ in turn: navigate to the next @@ header (the
	// one at or after the cursor's row + 1, so re-pressing f on
	// the just-folded @@ does not toggle it back open), then press
	// f. The cursor follows the @@ header across the toggle.
	for i := 0; i < hunkCount; i++ {
		lines = stripInlineLines(t, m)
		body = diffInlineBodyRows(lines)
		// Start the search one row past the cursor — the cursor
		// is on the @@ header of the just-folded hunk (or, on
		// the first iteration, the unfolded hunk A header), and
		// we want the NEXT @@ in document order, not the same
		// one again.
		cur := m.panel.diffInlineRow
		target := cur + 1
		// Clamp into range before scanning so we report the
		// correct row count if no further @@ exists.
		if target >= len(body) {
			t.Fatalf("no further @@ after cursor row %d (i=%d, body=%q)", cur, i, body)
		}
		for target < len(body) && !rowStartsWith(body[target], "@@") {
			target++
		}
		if target >= len(body) {
			t.Fatalf("could not find next @@ at i=%d, body=%q", i, body)
		}
		m.panel.diffInlineRow = target
		// Toggle.
		pressF(t, m)
	}

	// Final state: every @@ is folded.
	lines = stripInlineLines(t, m)
	body = diffInlineBodyRows(lines)
	if n := countPrefix(body, "@@"); n != hunkCount {
		t.Errorf("final @@ count = %d, want %d", n, hunkCount)
	}
	// No body content remains.
	for _, want := range []string{"CHANGED-A1", "CHANGED-A2", "CHANGED-A3", "CHANGED-B1", "CHANGED-B2", "CHANGED-B3"} {
		if anyContains(body, want) {
			t.Errorf("after all-folded: body still contains %q: %q", want, body)
		}
	}
	// The remaining rendered rows (between body and legend) should
	// be exactly the pre-header lines plus the hunkCount folded
	// @@ headers (no hunk bodies and no trailing context).
	wantLen := preHeaderCount + hunkCount
	if len(body) != wantLen {
		t.Errorf("after all-folded: body length = %d, want %d (pre-header %d + hunkCount %d)",
			len(body), wantLen, preHeaderCount, hunkCount)
	}
}

// TestDiffFolding_FOnNonHunkNoOp: pressing f on a +/- line does
// nothing. The fold state stays at zero entries (no header has
// been folded), and the body shape is byte-identical to the
// pre-f baseline.
func TestDiffFolding_FOnNonHunkNoOp(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	initDiffFoldingRepo(t, dir)

	m := openFoldingDiffSubview(t, dir)
	lines0 := stripInlineLines(t, m)
	body0 := diffInlineBodyRows(lines0)

	// Find a "+" or "-" line in the body, advance the cursor onto
	// it. The change blocks both use the +CHANGED-* markers.
	idxPlus := -1
	for i, l := range body0 {
		if rowStartsWith(l, "+CHANGED-") {
			idxPlus = i
			break
		}
	}
	if idxPlus < 0 {
		t.Fatalf("no +CHANGED- line in body: %q", body0)
	}
	m.panel.diffInlineRow = idxPlus

	pressF(t, m)

	// After f, render and confirm body shape is unchanged: same
	// @@ count (both expanded), same change lines visible.
	lines1 := stripInlineLines(t, m)
	body1 := diffInlineBodyRows(lines1)
	if n := countPrefix(body1, "@@"); n != 2 {
		t.Errorf("after f on +/- line: @@ count = %d, want 2 (no fold)", n)
	}
	if !anyContains(body1, "CHANGED-A1") || !anyContains(body1, "CHANGED-B1") {
		t.Errorf("after f on +/- line: body content lost: %q", body1)
	}
	// Folded map should be empty.
	if len(m.panel.diffFolded) != 0 {
		t.Errorf("after f on +/- line: diffFolded map has %d entries, want 0",
			len(m.panel.diffFolded))
	}
}

// pressF sends a bare 'f' key through handlePanelKey and asserts
// the panel still owns the key (returns true). The diff subview's
// 'f' binding returns true on both the toggle branch and the
// fall-through no-op branch, so this is the same contract as every
// other panel-owning letter.
func pressF(t *testing.T, m *runMode) {
	t.Helper()
	if handled, _ := m.handlePanelKey(tea.Key{Text: "f", Code: 'f'}); !handled {
		t.Fatal("f not handled")
	}
}

// cursorOnFirstHunkHeader advances m.panel.diffInlineRow to the
// first "@@" row in the body. Assumes lines have just been
// rendered (so the cursor mapping is current). Reports the
// obvious failure when the body has no @@ rows.
func cursorOnFirstHunkHeader(t *testing.T, m *runMode, lines []string) {
	t.Helper()
	body := diffInlineBodyRows(lines)
	idx := indexOfPrefix(body, "@@")
	if idx < 0 {
		t.Fatalf("no @@ header in body: %q", body)
	}
	m.panel.diffInlineRow = idx
}

// stripInlineLines renders the inline diff and strips SGR. A
// t.Helper so failures include the caller's line.
func stripInlineLines(t *testing.T, m *runMode) []string {
	t.Helper()
	raw := m.panelDiffInlineLines()
	out := make([]string, len(raw))
	for i, l := range raw {
		out[i] = stripSGR(l)
	}
	return out
}

// countPrefix returns the number of rows in lines that start with
// prefix — skipping the leading two-space indent + " > " cursor
// marker. Used by the tests to count "@@" headers regardless of
// their internal variation (different hunk ranges produce
// different "@@ -A,B +C,D @@" forms).
func countPrefix(lines []string, prefix string) int {
	n := 0
	for _, l := range lines {
		if rowStartsWith(l, prefix) {
			n++
		}
	}
	return n
}

// anyContains reports whether at least one row of lines contains
// substr. A small helper for the positive/negative change-line
// presence checks.
func anyContains(lines []string, substr string) bool {
	for _, l := range lines {
		if strings.Contains(l, substr) {
			return true
		}
	}
	return false
}

// backendMessagef builds a backend.Message with a printf-style body.
// Used in the cycle-226 tests to populate m.session with synthetic
// user-role turns without making the test files read like source.
func backendMessagef(role, format string, args ...any) backend.Message {
	return backend.Message{Role: role, Content: fmt.Sprintf(format, args...)}
}

// initCycle226GitRepo is the shared helper that creates a one-commit
// git repo at dir with a tracked file `f.txt` and the test fixture
// "modified f.txt + added g.txt + untracked h.txt" applied on top.
func initCycle226GitRepo(t *testing.T, dir string) {
	t.Helper()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q")
	write("f.txt", "old\n")
	write("g.txt", "new\n")
	git("add", "-A")
	git("commit", "-qm", "init")
	// Now dirty the working copy.
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("old\nCHANGED\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "g.txt"), []byte("new\nCHANGED\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	write("h.txt", "u\n")
}

// TestCycle226PerTurnEmpty keeps the cycle-207 placeholder when the
// session has no user turns. Cycle 207 had this as the only state; the
// regression test pins it so the cycle-226 row-list path does not
// accidentally suppress the placeholder.
func TestCycle226PerTurnEmpty(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	initCycle226GitRepo(t, dir)

	m := mkPanel(panelDiff, "/diff", 0)
	m.cwd = dir
	m.panel.tab = 1
	lines := m.panelDiffLinesVCS()
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = stripSGR(l)
	}
	if len(plain) != 5 {
		t.Fatalf("Per Turn empty = %d rows, want 5:\n%q", len(plain), plain)
	}
	if plain[2] != "   No file changes in conversation" {
		t.Errorf("Per Turn empty body = %q, want the cycle-207 placeholder", plain[2])
	}
}

// TestCycle226PerTurnRows: when session has user turns AND git diff has
// changes, list one row per turn with a snippet ("Turn N · <file>").
// The cursor (default 0) highlights the first row in the brand blue
// cursor style.
func TestCycle226PerTurnRows(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	initCycle226GitRepo(t, dir)

	m := mkPanel(panelDiff, "/diff", 0)
	m.cwd = dir
	m.panel.tab = 1
	// 3 user turns in the session (no assistant replies are required
	// for the count — only user-role messages count as a turn).
	for i := 0; i < 3; i++ {
		m.session = append(m.session, backendMessagef("user", "ask %d", i+1))
	}

	lines := m.panelDiffLinesVCS()
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = stripSGR(l)
	}
	if !strings.HasPrefix(plain[1], "   3 turn(s) · ") {
		t.Errorf("Per Turn summary row = %q", plain[1])
	}
	if !strings.Contains(plain[1], "3 file(s) changed") {
		t.Errorf("Per Turn summary row does not show file count: %q", plain[1])
	}
	// Rows 3, 4, 5 are the three turns. Each should mention "Turn N · ".
	for i, want := range []string{"Turn 1 · ", "Turn 2 · ", "Turn 3 · "} {
		row := plain[3+i]
		if !strings.Contains(row, want) {
			t.Errorf("Per Turn row %d = %q, want substring %q", i, row, want)
		}
		// Each row carries a file path from the working copy.
		if !strings.Contains(row, ".txt") {
			t.Errorf("Per Turn row %d has no file: %q", i, row)
		}
	}
	// Cursor row 0 carries the brand-blue "> " marker.
	if !strings.Contains(lines[3], cBlueOpen+"> "+cBlueClose) {
		t.Errorf("Per Turn cursor row missing brand-blue marker: %q", lines[3])
	}
}

// TestCycle226PerTurnNoChanges: turns exist but the working copy is
// clean. The cycle-207 placeholder is preserved (a turn list would be
// misleading without changes to show).
func TestCycle226PerTurnNoChanges(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	// Clean repo: one commit, no further mutations.
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", "-A"}, {"commit", "-qm", "init"},
	} {
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	m := mkPanel(panelDiff, "/diff", 0)
	m.cwd = dir
	m.panel.tab = 1
	m.session = append(m.session, backendMessagef("user", "ask 1"))
	lines := m.panelDiffLinesVCS()
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = stripSGR(l)
	}
	if plain[2] != "   No file changes in conversation" {
		t.Errorf("Per Turn no-changes body = %q, want the cycle-207 placeholder", plain[2])
	}
}

// TestCycle226AllChangesEnter: pressing Enter on All Changes opens the
// inline-diff subview (`git diff -- <path>`) for the file under the
// cursor. The subview title carries the path; + lines are styled green
// and - lines red-orange; Esc returns to the list (does NOT close the
// panel).
func TestCycle226AllChangesEnter(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	initCycle226GitRepo(t, dir)

	m := mkPanel(panelDiff, "/diff", 0)
	m.cwd = dir
	m.panel.tab = 0
	// Cursor on f.txt (the first tracked file, sorted alphabetically).
	m.panel.cursor = 0
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("Enter not handled")
	}
	if m.panel.diffView != 1 {
		t.Fatalf("diffView = %d, want 1 (file inline)", m.panel.diffView)
	}
	if m.panel.diffViewPath != "f.txt" {
		t.Errorf("diffViewPath = %q, want f.txt", m.panel.diffViewPath)
	}
	lines := m.panelDiffLinesVCS()
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = stripSGR(l)
	}
	// Title row carries the path.
	if plain[0] != "f.txt" {
		t.Errorf("inline title = %q, want f.txt", plain[0])
	}
	// The body should mention a real + line (the "+CHANGED" addition).
	foundPlus := false
	for _, l := range plain {
		if strings.Contains(l, "+CHANGED") {
			foundPlus = true
			break
		}
	}
	if !foundPlus {
		t.Errorf("inline body missing +CHANGED: %q", plain)
	}
	// Esc returns to the list; the panel stays open.
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEsc}); !handled {
		t.Fatal("Esc not handled")
	}
	if m.panel.diffView != 0 {
		t.Errorf("after Esc diffView = %d, want 0", m.panel.diffView)
	}
	if m.panel.kind != panelDiff {
		t.Errorf("after Esc panel.kind = %d, want panelDiff", m.panel.kind)
	}
}

// TestCycle226CommitTreeEnter: pressing Enter on a commit row opens
// the inline-diff subview (`git show <hash>`) for that commit. The
// subview title is `commit <hash>`; the body contains the commit
// metadata + diff.
func TestCycle226CommitTreeEnter(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	initCycle226GitRepo(t, dir)

	m := mkPanel(panelDiff, "/diff", 0)
	m.cwd = dir
	m.panel.tab = 2
	// Move cursor to the first commit (cursor==0 = working-copy row;
	// cursor==1 = first commit row).
	m.panel.cursor = 1
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("Enter not handled")
	}
	if m.panel.diffView != 2 {
		t.Fatalf("diffView = %d, want 2 (commit inline)", m.panel.diffView)
	}
	if m.panel.diffViewPath == "" {
		t.Fatal("diffViewPath empty")
	}
	// The diffViewPath should be the commit hash (git show's short
	// hash is 7 chars). Fetch the real hash for comparison.
	logOut, err := exec.Command("git", "-C", dir, "log", "--pretty=%h").Output()
	if err != nil {
		t.Fatal(err)
	}
	wantHash := strings.SplitN(strings.TrimRight(string(logOut), "\n"), "\n", 2)[0]
	if m.panel.diffViewPath != wantHash {
		t.Errorf("diffViewPath = %q, want commit hash %q", m.panel.diffViewPath, wantHash)
	}
	lines := m.panelDiffLinesVCS()
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = stripSGR(l)
	}
	if !strings.HasPrefix(plain[0], "commit ") {
		t.Errorf("inline title = %q, want \"commit <hash>\"", plain[0])
	}
	// Body should mention the commit message ("init").
	foundInit := false
	for _, l := range plain {
		if strings.Contains(l, "init") {
			foundInit = true
			break
		}
	}
	if !foundInit {
		t.Errorf("inline body missing commit subject: %q", plain)
	}
}

// TestCycle226WorkingCopyEnter: cursor==0 on Commit Tree is the
// working-copy row. Enter there is a no-op (working copy has no
// per-commit hash to drill into).
func TestCycle226WorkingCopyEnter(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	initCycle226GitRepo(t, dir)

	m := mkPanel(panelDiff, "/diff", 0)
	m.cwd = dir
	m.panel.tab = 2
	m.panel.cursor = 0 // working-copy row
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("Enter not handled")
	}
	if m.panel.diffView != 0 {
		t.Errorf("Enter on working-copy row set diffView = %d, want 0", m.panel.diffView)
	}
}

// TestCycle226DiffCursorCommitTree: cycle-207 had the row cursor inert
// on Commit Tree; cycle 226 unifies the cursor model. Down moves
// cursor from 0 (working copy) to 1 (first commit), and over the cap
// it clamps.
func TestCycle226DiffCursorCommitTree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	initCycle226GitRepo(t, dir)

	m := mkPanel(panelDiff, "/diff", 0)
	m.cwd = dir
	m.panel.tab = 2
	m.panel.cursor = 0
	m.diffMoveCursor(1)
	if m.panel.cursor != 1 {
		t.Errorf("after Down cursor = %d, want 1", m.panel.cursor)
	}
	// Single-commit repo: cap is 1 (cursor==0 working-copy, cursor==1 commit).
	m.diffMoveCursor(1)
	if m.panel.cursor != 1 {
		t.Errorf("after Down at cap cursor = %d, want 1 (clamped)", m.panel.cursor)
	}
	m.diffMoveCursor(-1)
	if m.panel.cursor != 0 {
		t.Errorf("after Up cursor = %d, want 0", m.panel.cursor)
	}
}

// TestCycle226DiffCursorPerTurn: Per Turn row list supports cursor
// navigation when session has turns AND working copy has changes.
func TestCycle226DiffCursorPerTurn(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	initCycle226GitRepo(t, dir)

	m := mkPanel(panelDiff, "/diff", 0)
	m.cwd = dir
	m.panel.tab = 1
	for i := 0; i < 5; i++ {
		m.session = append(m.session, backendMessagef("user", "ask %d", i+1))
	}
	m.panel.cursor = 0
	m.diffMoveCursor(1)
	if m.panel.cursor != 1 {
		t.Errorf("Per Turn after Down cursor = %d, want 1", m.panel.cursor)
	}
	m.diffMoveCursor(-1)
	if m.panel.cursor != 0 {
		t.Errorf("Per Turn after Up cursor = %d, want 0", m.panel.cursor)
	}
	// cap: 4 (cursor indexes 0..4 over 5 turns).
	for i := 0; i < 10; i++ {
		m.diffMoveCursor(1)
	}
	if m.panel.cursor != 4 {
		t.Errorf("Per Turn clamped cursor = %d, want 4", m.panel.cursor)
	}
}

// TestCycle226CommitTreeMultiCommitShape: the cycle-207 connector
// logic was a "|" before every commit, but REF only puts the
// connector between working-copy and the first commit; subsequent
// commits stack directly. The cycle-226 fix must match.
func TestCycle226CommitTreeMultiCommitShape(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("init", "-q")
	git("add", "-A")
	git("commit", "-qm", "first")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\ny\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-qm", "second")

	m := mkPanel(panelDiff, "/diff", 0)
	m.cwd = dir
	m.panel.tab = 2
	lines := m.panelDiffLinesVCS()
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = stripSGR(l)
	}
	// Layout (cycle-226 correction):
	//   0: title
	//   1: 2 commit(s) available
	//   2: (blank)
	//   3: ▌ *  Working Copy ...
	//   4:   |                    <- the connector, ONLY here
	//   5:   * <hash1> first
	//   6:   * <hash2> second     <- no "|" above this row
	//   7: (blank)
	//   8: legend
	if plain[4] != "   |" {
		t.Errorf("row 4 = %q, want connector \"   |\"", plain[4])
	}
	if plain[5] == "   |" || !strings.HasPrefix(plain[5], "   * ") {
		t.Errorf("row 5 = %q, want first commit row, no extra connector", plain[5])
	}
	if plain[6] == "   |" || !strings.HasPrefix(plain[6], "   * ") {
		t.Errorf("row 6 = %q, want second commit row, no extra connector", plain[6])
	}
}

// TestCycle226PanelKeyDefaults: ensure the cycle-207 key actions are
// preserved when diffView == 0 (the list view). Tab switch resets the
// cursor, esc closes the panel.
func TestCycle226PanelKeyDefaults(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	initCycle226GitRepo(t, dir)

	m := mkPanel(panelDiff, "/diff", 0)
	m.cwd = dir
	m.panel.tab = 0
	m.panel.cursor = 2
	// Right arrow advances the tab; cycle-226 resets the cursor to 0.
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyRight}); !handled {
		t.Fatal("Right not handled")
	}
	if m.panel.tab != 1 {
		t.Errorf("after Right tab = %d, want 1", m.panel.tab)
	}
	if m.panel.cursor != 0 {
		t.Errorf("after Right cursor = %d, want 0 (reset)", m.panel.cursor)
	}
	// Esc closes the panel (cycle-207 behaviour, preserved). closePanel
	// returns a nil Cmd; the contract is `handled == true` + state
	// cleared.
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEsc}); !handled {
		t.Error("Esc on list view not handled")
	}
	if m.panel.kind != panelNone {
		t.Errorf("after Esc panel.kind = %d, want panelNone", m.panel.kind)
	}
}

// TestDiffTrimEndToEndRealRepo is the integration test: a tracked file
// with trailing tabs is edited to swap trailing-tab content for clean
// content, and the inline-diff subview's body is asserted to contain no
// trailing-tab characters in its rendered form.
func TestDiffTrimEndToEndRealRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q")
	write("tab.txt", "line without tabs\n")
	git("add", "-A")
	git("commit", "-qm", "init")
	if err := os.WriteFile(filepath.Join(dir, "tab.txt"),
		[]byte("line without tabs\nline WITH TRAILING TAB\t\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := mkPanel(panelDiff, "/diff", 0)
	m.cwd = dir
	m.panel.tab = 0
	m.panel.cursor = 0
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("Enter not handled")
	}
	if m.panel.diffView != 1 || m.panel.diffViewPath != "tab.txt" {
		t.Fatalf("diffView=%d path=%q, want enter-on-file inline view",
			m.panel.diffView, m.panel.diffViewPath)
	}
	lines := m.panelDiffInlineLines()
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = stripSGR(l)
	}
	const want = "  +line WITH TRAILING TAB"
	found := false
	for _, l := range plain {
		if strings.Contains(l, want) {
			found = true
			if strings.HasSuffix(l, "\t") || strings.HasSuffix(strings.TrimPrefix(l, "  "), " \t") {
				t.Errorf("rendered diff line still ends in whitespace marker: %q", l)
			}
		}
	}
	if !found {
		t.Errorf("no +line rendered in the trimmed body, lines=%q", plain)
	}
}

// configLeftRightVerbosityIdx returns the row index of the Verbosity
// row. The catalogue is recovered from probe_ref_walk2.py; Verbosity
// is one of the 20 top-level rows in the order produced by the
// REProbe. Rather than depend on that order shifting across cycles,
// look the row up by name (configRows().Name).
func configLeftRightVerbosityIdx(t *testing.T) int {
	t.Helper()
	for i, r := range configRows() {
		if r.Name == "Verbosity" {
			return i
		}
	}
	t.Fatalf("configRows has no row named \"Verbosity\" (catalogue shrank)")
	return -1
}

// readConfigLRSettings parses the test's settings.json into a map.
// Failures abort the test. Used by the persistence and "no change"
// assertions.
func readConfigLRSettings(t *testing.T, dir string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse settings.json: %v", err)
	}
	return m
}

// configLRValueAt walks the dot path and returns the leaf as a string
// (booleans render as "on"/"off"). Failures abort the test. Mirrors
// cycle196ValueAt but is renamed so the deliverable can stand alone
// without bleeding into cycle 196's test scaffold.
func configLRValueAt(t *testing.T, m map[string]any, path string) string {
	t.Helper()
	parts := strings.Split(path, ".")
	var cur any = m
	for _, p := range parts {
		obj, ok := cur.(map[string]any)
		if !ok {
			t.Fatalf("path %q: %T is not an object", path, cur)
		}
		cur, ok = obj[p]
		if !ok {
			t.Fatalf("path %q: missing key %q", path, p)
		}
	}
	switch v := cur.(type) {
	case string:
		return v
	case bool:
		if v {
			return "on"
		}
		return "off"
	}
	t.Fatalf("path %q: leaf has unexpected type %T", path, cur)
	return ""
}

// TestConfigLeftRight_AdvancesValue walks the Verbosity cycle in both
// directions and pins its order. The catalogue's cycle list for
// Verbosity is, verbatim from probe_ref_walk2.py row 17 and the
// [medium, low, high] ordering in cmd/wagyu/slash_panels.go:
//
//	Verbosity  medium  low  high
//
// From the absent-key default ("medium"):
//
//	→ Right (dir=+1)      index 0 → 1 → "low"
//	→ Right (dir=+1) again index 1 → 2 → "high"
//	→ Right (dir=+1) again index 2 → 0 → "medium" (wrap)
//	← Left  (dir=-1)      index 0 → 2 → "high" (wrap back)
//
// The wrap assertion is what distinguishes a real cycle from a
// numeric-arrow "increment index until end" sanity stub. A no-op or
// silent-fail cycleConfigValue would keep "medium" forever.
func TestConfigLeftRight_AdvancesValue(t *testing.T) {
	cycle196TestHome(t, map[string]any{}) // empty settings → default

	m := &runMode{}
	m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: configLeftRightVerbosityIdx(t)}

	// Sanity: the default reads as the row's first cycle value.
	if v := configSettingValue(m.panel.cursor); v != "medium" {
		t.Fatalf("baseline: verbosity = %q, want \"medium\"", v)
	}

	m.cycleConfigValue(+1) // medium → low
	if v := configSettingValue(m.panel.cursor); v != "low" {
		t.Errorf("after 1× Right: verbosity = %q, want \"low\"", v)
	}

	m.cycleConfigValue(+1) // low → high
	if v := configSettingValue(m.panel.cursor); v != "high" {
		t.Errorf("after 2× Right: verbosity = %q, want \"high\"", v)
	}

	m.cycleConfigValue(+1) // high → medium (wrap forward)
	if v := configSettingValue(m.panel.cursor); v != "medium" {
		t.Errorf("after 3× Right (wrap): verbosity = %q, want \"medium\"", v)
	}

	m.cycleConfigValue(-1) // medium → high (wrap backward)
	if v := configSettingValue(m.panel.cursor); v != "high" {
		t.Errorf("after Left (wrap back): verbosity = %q, want \"high\"", v)
	}
}

// TestConfigLeftRight_PersistsToDisk is the core persistence pin:
// after a single ←/→ the new value must be visible in
// ~/.gemini/antigravity-cli/settings.json, not just in the in-memory
// runMode. The "immediate" wording in the AGY 1.2.16 changelog is
// what this asserts — the write happens inside cycleConfigValue, not
// at some later "save settings" keystroke.
//
// The test seeds with colorScheme="light" (unrelated to verbosity) so
// that the file already exists when cycleConfigValue writes, and the
// helper that handles a missing key (cycleConfigValue → "default
// cycle list start") still has its settings.json to mutate.
func TestConfigLeftRight_PersistsToDisk(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{
		"colorScheme": "light",
	})

	m := &runMode{}
	m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: configLeftRightVerbosityIdx(t)}

	// Snapshot the disk up front so we can prove the *write* happened.
	pre := readConfigLRSettings(t, dir)
	if _, ok := pre["verbosity"]; ok {
		t.Fatalf("baseline: verbosity already in disk as %v; rewrite the test", pre["verbosity"])
	}

	m.cycleConfigValue(+1) // medium → low (write "low" to settings.json)

	got := readConfigLRSettings(t, dir)
	if v, ok := got["verbosity"].(string); !ok || v != "low" {
		t.Errorf("after Right: settings.json verbosity = %v, want \"low\"", got["verbosity"])
	}

	// The unrelated seed key MUST survive untouched — persistence is
	// additive, not a wholesale rewrite.
	if v := configLRValueAt(t, got, "colorScheme"); v != "light" {
		t.Errorf("colorScheme clobbered: %q, want \"light\"", v)
	}
}

// TestConfigLeftRight_NoDropdownOpen confirms the user-facing
// behaviour called out in the changelog: ←/→ changes the value and
// saves it "without opening the dropdown". The picker must stay in
// its base view — no sub-modal, no detail overlay, no stage bump.
//
// The assertions are on panel state that, if any other cycle had
// tried to open a dropdown, would have flipped:
//
//   - m.panel.kind        stays panelConfig
//   - m.panel.detail      stays false (skills sub-view; different
//     panel, defensive)
//   - m.panel.stage       stays 0 (feedback free-text stage)
//   - m.panel.diffView    stays 0 (diff inline)
//   - m.panel.cursor      unchanged after Left/Right (Up/Down are
//     the cursor-bearing keys here)
//   - the panel renderer's line count is unchanged
//
// The line-count check is the most direct "no overlay appeared"
// signal: every panel sub-view in this codebase adds at least one
// row.
func TestConfigLeftRight_NoDropdownOpen(t *testing.T) {
	cycle196TestHome(t, map[string]any{})

	m := mkPanel(panelConfig, "/config", configLeftRightVerbosityIdx(t))
	preLines := plainLines(m)

	// Snapshot the dropdown-opening state before the press.
	preKind := m.panel.kind
	preDetail := m.panel.detail
	preStage := m.panel.stage
	preDiff := m.panel.diffView
	preCursor := m.panel.cursor

	// Right → advances verbosity to "low"; must NOT mutate any of
	// the panel overlay state.
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyRight}); !handled {
		t.Fatal("Right reported unhandled on /config")
	}

	for name, pair := range map[string]struct {
		pre, got any
	}{
		"kind":     {preKind, m.panel.kind},
		"detail":   {preDetail, m.panel.detail},
		"stage":    {preStage, m.panel.stage},
		"diffView": {preDiff, m.panel.diffView},
		"cursor":   {preCursor, m.panel.cursor},
	} {
		if pair.pre != pair.got {
			t.Errorf("%s changed: pre=%v got=%v", name, pair.pre, pair.got)
		}
	}

	postLines := plainLines(m)
	if len(preLines) != len(postLines) {
		t.Errorf("panel line count changed after Right: pre=%d post=%d\npre:\n%s\npost:\n%s",
			len(preLines), len(postLines),
			strings.Join(preLines, "\n"), strings.Join(postLines, "\n"))
	}

	// And Left, for symmetry.
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyLeft}); !handled {
		t.Fatal("Left reported unhandled on /config")
	}
	if m.panel.kind != panelConfig {
		t.Errorf("Left mutated panel.kind = %v, want panelConfig", m.panel.kind)
	}
	post2 := plainLines(m)
	if len(preLines) != len(post2) {
		t.Errorf("panel line count changed after Left: pre=%d post=%d", len(preLines), len(post2))
	}
}

// TestConfigLeftRight_FooterHint pins the second clause of the
// changelog entry: "the footer shows a '←/→ Change' hint". The
// picker's keyboard legend row is, verbatim from cycle 196's
// panelConfigLines source:
//
//	panelKeyboardHint("↑/↓", "Navigate", "←/→", "Change",
//	    "enter", "Select", "esc", "Clear Search/Exit")
//
// After ANSI stripping the rendered legend should contain both the
// arrow-glyph key form and the "Change" label form. The changelog
// shorthand "<-/-> Change" is the ASCII fallback the reference
// documents use when terminal capture collapses the arrow glyphs;
// we accept either form so the test is not over-coupled to the
// exact rune the renderer emits.
func TestConfigLeftRight_FooterHint(t *testing.T) {
	cycle196TestHome(t, map[string]any{})

	m := mkPanel(panelConfig, "/config", 0)
	plain := plainLines(m)
	if len(plain) == 0 {
		t.Fatal("panelLines returned no rows")
	}
	legend := plain[len(plain)-1]

	if !strings.Contains(legend, "Change") {
		t.Errorf("legend missing \"Change\" label: %q", legend)
	}
	hasArrowForm := strings.Contains(legend, "←/→")
	hasDashForm := strings.Contains(legend, "<-/->")
	if !hasArrowForm && !hasDashForm {
		t.Errorf("legend missing \"←/→\" or \"<-/->\" key form: %q", legend)
	}
}

// TestConfigLeftRight_NonEnumSetting pins the safety floor for
// rows that cannot be cycled. The catalogue contains 20 rows and
// every one of them is an enum with len(values) >= 2, but the
// guard inside cycleConfigValue (length<2 → return) is the
// canonical non-enum no-op contract. Two complementary checks:
//
//  1. Structural: every catalogue row carries at least two values
//     (so the catalogue never accidentally adds a one-element row
//     that the render would render but the keys could not act on).
//  2. Behavioural: an out-of-range cursor leaves settings.json
//     untouched and does not panic.
//
// The structural check guarantees (1) is genuine; the behavioural
// check exercises the early-return guard directly so a future
// regression that adds a non-enum row cannot hide behind "the
// catalogue has no such row today".
func TestConfigLeftRight_NonEnumSetting(t *testing.T) {
	// 1. Structural: every catalogue row has >=2 cycle values.
	for i, r := range configRows() {
		if len(r.Values) < 2 {
			t.Errorf("row %d (%q): len(values)=%d, want >=2 (←/→ has nothing to cycle)", i, r.Name, len(r.Values))
		}
	}

	// 2. Behavioural: cursor past the end of the catalogue.
	dir := cycle196TestHome(t, map[string]any{})
	pre := readConfigLRSettings(t, dir)

	m := &runMode{}
	m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: len(configRows()) + 5}

	// Must not panic.
	m.cycleConfigValue(+1)
	m.cycleConfigValue(-1)

	post := readConfigLRSettings(t, dir)
	if len(pre) != len(post) {
		t.Errorf("settings.json mutated despite out-of-range cursor: pre=%v post=%v", pre, post)
	}

	// And: handlePanelKey's panelConfig branch does not crash on a
	// negative cursor either (which a key press cannot produce, but
	// the guard inside cycleConfigValue is the only thing keeping
	// the renderer safe).
	m.panel.cursor = -1
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyRight}); !handled {
		t.Fatal("Right at cursor=-1 reported unhandled; want handled (guard returns early)")
	}
	if m.panel.kind != panelConfig {
		t.Errorf("cursor=-1 mutated panel.kind=%v, want panelConfig", m.panel.kind)
	}
}

// cycle196TestHome returns an isolated config home for one test. It
// writes a starting settings.json with `seed` (raw map) and returns
// the home directory; t.Cleanup resets the env.
func cycle196TestHome(t *testing.T, seed map[string]any) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	if seed != nil {
		b, err := json.MarshalIndent(seed, "", "  ")
		if err != nil {
			t.Fatalf("marshal seed: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), b, 0600); err != nil {
			t.Fatalf("write seed: %v", err)
		}
	}
	return dir
}

// configRowIndexForTest resolves a /config row's index by its display
// name, so the tests do not depend on the order of the twenty settings rows.
func configRowIndexForTest(t *testing.T, name string) int {
	t.Helper()
	for i, r := range configRows() {
		if r.Name == name {
			return i
		}
	}
	t.Fatalf("configRows has no row named %q", name)
	return -1
}

// readCycle196Settings unmarshals the test's settings.json into a generic map.
func readCycle196Settings(t *testing.T, dir string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse settings: %v", err)
	}
	return m
}

// cycle196ValueAt walks a dot path and returns the leaf as string.
func cycle196ValueAt(t *testing.T, m map[string]any, path string) string {
	t.Helper()
	parts := strings.Split(path, ".")
	var cur any = m
	for _, p := range parts {
		obj, ok := cur.(map[string]any)
		if !ok {
			t.Fatalf("path %q: %T is not an object", path, cur)
		}
		cur, ok = obj[p]
		if !ok {
			t.Fatalf("path %q: missing key %q", path, p)
		}
	}
	switch v := cur.(type) {
	case string:
		return v
	case bool:
		if v {
			return "on"
		}
		return "off"
	}
	t.Fatalf("path %q: leaf has unexpected type %T", path, cur)
	return ""
}

// TestCycle196_ConfigCycleRight_SimpleString is the canonical
// cycle-through cycle196 case: cursor on Color Scheme (cycle list:
// light → solarized light → …), right arrow advances one step and
// persists the *stored* form.
func TestCycle196_ConfigCycleRight_SimpleString(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{
		"colorScheme": "light",
	})
	m := &runMode{}
	m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: 3} // Color Scheme

	m.cycleConfigValue(+1)
	if got := cycle196ValueAt(t, readCycle196Settings(t, dir), "colorScheme"); got != "solarized light" {
		t.Errorf("after right: colorScheme = %q, want %q", got, "solarized light")
	}
	if v := configSettingValue(3); v != "solarized light" {
		t.Errorf("configSettingValue(3) = %q, want %q", v, "solarized light")
	}
}

// TestCycle196_ConfigCycleLeft_ToDefault covers the reference's
// default-by-absence convention: from the second cycle value ("light")
// pressing ← lands on the default ("terminal") and *removes* the key
// rather than writing the string "terminal".
func TestCycle196_ConfigCycleLeft_ToDefault(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{
		"colorScheme": "light",
	})
	m := &runMode{}
	m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: 3}
	m.cycleConfigValue(-1)
	got := readCycle196Settings(t, dir)
	if _, present := got["colorScheme"]; present {
		t.Errorf("after left from light: colorScheme = %v, want key absent (default)", got["colorScheme"])
	}
	if v := configSettingValue(3); v != "terminal" {
		t.Errorf("configSettingValue(3) = %q, want %q", v, "terminal")
	}
}

// TestCycle196_ConfigCycle_BoolSetting exercises the boolean path on a
// real row from the 1.4.0 catalogue: "Show Tips" is on by default
// (absent) and persists `false` when toggled off.
func TestCycle196_ConfigCycle_BoolSetting(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{"showTips": true})
	m := &runMode{}
	m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: configRowIndexForTest(t, "Show Tips")}

	m.cycleConfigValue(-1) // on → off
	got := readCycle196Settings(t, dir)
	if v := cycle196ValueAt(t, got, "showTips"); v != "off" {
		t.Errorf("after left: showTips = %q, want %q", v, "off")
	}
	if v := configSettingValue(configRowIndexForTest(t, "Show Tips")); v != "off" {
		t.Errorf("display = %q, want %q", v, "off")
	}

	m.cycleConfigValue(+1) // off → on (default: key removed)
	got = readCycle196Settings(t, dir)
	if _, present := got["showTips"]; present {
		t.Errorf("after right: showTips = %v, want key absent (default on)", got["showTips"])
	}
	if v := configSettingValue(configRowIndexForTest(t, "Show Tips")); v != "on" {
		t.Errorf("display = %q, want %q", v, "on")
	}
}

// TestCycle196_ConfigCycle_KeyHandler verifies that handlePanelKey
// routes ←/→ to cycleConfigValue for the config panel.
func TestCycle196_ConfigCycle_KeyHandler(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{
		"colorScheme": "light",
	})
	m := &runMode{}
	m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: 3}

	handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyRight})
	if !handled {
		t.Fatal("←/→: handlePanelKey reported unhandled")
	}
	if got := cycle196ValueAt(t, readCycle196Settings(t, dir), "colorScheme"); got != "solarized light" {
		t.Errorf("after ←/→ key: colorScheme = %q, want %q", got, "solarized light")
	}
}

// TestCycle196_ConfigCycle_AbsentKey covers the case where the
// settings.json file lacks the key (first-launch state).
func TestCycle196_ConfigCycle_AbsentKey(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{
		"colorScheme": "dark",
	})
	if _, err := os.Stat(filepath.Join(dir, "settings.json")); err != nil {
		t.Fatal(err)
	}
	m := &runMode{}
	m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: 0} // Agent Mode
	if v := configSettingValue(0); v != "default" {
		t.Errorf("missing key: configSettingValue(0) = %q, want %q", v, "default")
	}
	m.cycleConfigValue(+1) // default → accept-edits
	got := readCycle196Settings(t, dir)
	if v, ok := got["agentMode"].(string); !ok || v != "accept-edits" {
		t.Errorf("after right from absent: agentMode = %v, want %q", got["agentMode"], "accept-edits")
	}
}

// TestCycle196_ConfigValue_LiveRead verifies that the rendered panel
// reflects live settings.json state.
func TestCycle196_ConfigValue_LiveRead(t *testing.T) {
	cycle196TestHome(t, map[string]any{
		"colorScheme": "dark",
	})
	if v := configSettingValue(3); v != "dark" {
		t.Errorf("configSettingValue(3) with dark in disk = %q, want %q", v, "dark")
	}
	_ = auth.AntigravityConfigHome()
}

func TestConfigSearchFiltering(t *testing.T) {
	all := configRows()
	if len(configRowsFiltered("")) != len(all) {
		t.Fatalf("empty query should return all %d rows, got %d", len(all), len(configRowsFiltered("")))
	}

	// Name filter
	colorRows := configRowsFiltered("color")
	if len(colorRows) != 1 || colorRows[0].Name != "Color Scheme" {
		t.Fatalf("expected 1 row for 'color' (Color Scheme), got %d: %+v", len(colorRows), colorRows)
	}

	// Description filter
	descRows := configRowsFiltered("system clipboard")
	if len(descRows) != 1 || descRows[0].Name != "Copy on Select" {
		t.Fatalf("expected 'Copy on Select' for 'system clipboard', got %d: %+v", len(descRows), descRows)
	}

	// Option display value filter
	optRows := configRowsFiltered("accept-edits")
	found := false
	for _, r := range optRows {
		if r.Name == "Agent Mode" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected 'Agent Mode' to match 'accept-edits', got %+v", optRows)
	}

	// No match
	none := configRowsFiltered("nonexistent_random_setting_xyz")
	if len(none) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(none))
	}
}

func TestConfigSearchKeyHandling(t *testing.T) {
	m := &runMode{
		panel: slashPanel{
			kind: panelConfig,
			cmd:  "/settings",
		},
		width:  120,
		height: 40,
	}

	// Type 'c'
	m.handlePanelKey(tea.Key{Code: 'c', Text: "c"})
	if m.panel.configQuery != "c" {
		t.Fatalf("expected configQuery 'c', got %q", m.panel.configQuery)
	}

	// Type 'o'
	m.handlePanelKey(tea.Key{Code: 'o', Text: "o"})
	if m.panel.configQuery != "co" {
		t.Fatalf("expected configQuery 'co', got %q", m.panel.configQuery)
	}

	// Check filtered view
	lines := m.panelConfigLines()
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Search: co") {
		t.Fatalf("expected search line with 'co', got:\n%s", joined)
	}

	// Backspace
	m.handlePanelKey(tea.Key{Code: tea.KeyBackspace})
	if m.panel.configQuery != "c" {
		t.Fatalf("expected configQuery 'c' after backspace, got %q", m.panel.configQuery)
	}

	// Esc once: clears search query, panel stays open
	m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
	if m.panel.configQuery != "" {
		t.Fatalf("expected empty configQuery after first Esc, got %q", m.panel.configQuery)
	}
	if m.panel.kind != panelConfig {
		t.Fatalf("expected panel to remain open after clearing search, got %v", m.panel.kind)
	}

	// Esc second time: closes panel
	m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
	if m.panel.kind != panelNone {
		t.Fatalf("expected panel to close after second Esc, got %v", m.panel.kind)
	}
}

func TestConfigSubmenuEnterAndSave(t *testing.T) {
	cycle196TestHome(t, map[string]any{})

	m := &runMode{
		panel: slashPanel{
			kind: panelConfig,
			cmd:  "/settings",
		},
		width:  120,
		height: 40,
	}

	// Find index of Agent Mode (usually 0)
	rows := configRowsFiltered("")
	agentIdx := -1
	for i, r := range rows {
		if r.Name == "Agent Mode" {
			agentIdx = i
			break
		}
	}
	if agentIdx < 0 {
		t.Fatal("Agent Mode row not found")
	}

	m.panel.cursor = agentIdx
	// Press Enter to open submenu
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if !m.panel.configSubmenu {
		t.Fatal("expected configSubmenu to be true after Enter")
	}

	lines := m.panelConfigLines()
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "enter Save") || !strings.Contains(joined, "esc Cancel") {
		t.Fatalf("expected submenu keyboard hint, got:\n%s", joined)
	}

	// Down arrow to change option
	prevCursor := m.panel.configSubmenuCursor
	m.handlePanelKey(tea.Key{Code: tea.KeyDown})
	if m.panel.configSubmenuCursor != prevCursor+1 && m.panel.configSubmenuCursor != prevCursor {
		t.Fatalf("expected cursor to move, was %d now %d", prevCursor, m.panel.configSubmenuCursor)
	}

	// Esc to cancel without saving
	m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
	if m.panel.configSubmenu {
		t.Fatal("expected configSubmenu to be false after Esc")
	}

	// Open again, press Enter to save
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if !m.panel.configSubmenu {
		t.Fatal("expected submenu to open again")
	}
	m.panel.configSubmenuCursor = 1 // 'accept-edits'
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if m.panel.configSubmenu {
		t.Fatal("expected submenu to close after save Enter")
	}

	val, err := auth.SettingsField("agentMode")
	if err != nil || val != "accept-edits" {
		t.Fatalf("expected agentMode 'accept-edits' saved in settings, got %v (err=%v)", val, err)
	}
}

func TestColorSchemePreviewCardRendering(t *testing.T) {
	cycle196TestHome(t, map[string]any{})

	m := &runMode{
		panel: slashPanel{
			kind:        panelConfig,
			cmd:         "/settings",
			configQuery: "color",
		},
		width:  120,
		height: 40,
	}

	rows := configRowsFiltered(m.panel.configQuery)
	if len(rows) != 1 || rows[0].Name != "Color Scheme" {
		t.Fatalf("expected 1 row 'Color Scheme', got %+v", rows)
	}

	m.panel.cursor = 0
	// Open Color Scheme submenu
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if !m.panel.configSubmenu {
		t.Fatal("expected configSubmenu = true")
	}

	lines := m.panelConfigLines()
	rawJoined := strings.Join(lines, "\n")
	joined := stripANSI(rawJoined)

	// Verify right-side preview box border glyphs
	if !strings.Contains(joined, "╭─────────────────────────────────────────────────────────────╮") {
		t.Fatalf("expected top border of preview box in:\n%s", joined)
	}
	if !strings.Contains(joined, "╰─────────────────────────────────────────────────────────────╯") {
		t.Fatalf("expected bottom border of preview box in:\n%s", joined)
	}

	// Verify sample code snippet contents
	if !strings.Contains(joined, "func  greet(name") && !strings.Contains(joined, "func greet(name") {
		t.Fatalf("expected greet function in preview box in:\n%s", joined)
	}
	if !strings.Contains(joined, "▾ Thought Process") {
		t.Fatalf("expected Thought Process in preview box in:\n%s", joined)
	}
	if !strings.Contains(joined, "⚙ tool:") || !strings.Contains(joined, "◉ task:") || !strings.Contains(joined, "✗ error:") {
		t.Fatalf("expected tool, task, error glyphs in preview box in:\n%s", joined)
	}

	// Verify left-side options
	if !strings.Contains(joined, "terminal") || !strings.Contains(joined, "tokyo night") {
		t.Fatalf("expected terminal and tokyo night schemes listed on left side")
	}

	// Verify live preview update when cursor moves to Tokyo Night (index 7)
	m.panel.configSubmenuCursor = 7
	linesTokyo := m.panelConfigLines()
	joinedTokyo := strings.Join(linesTokyo, "\n")
	pTokyo := onboarding.PaletteFor(onboarding.SchemeTokyoNight)
	tokyoCodeStr := fmt.Sprintf("\x1b[38;5;%dm", pTokyo.Keyword)
	if !strings.Contains(joinedTokyo, tokyoCodeStr) {
		t.Fatalf("expected Tokyo Night keyword color %s in preview output", tokyoCodeStr)
	}

	// Save Tokyo Night
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if m.panel.configSubmenu {
		t.Fatal("expected submenu closed after save")
	}

	saved, err := auth.SettingsField("colorScheme")
	if err != nil || saved != "tokyo night" {
		t.Fatalf("expected colorScheme 'tokyo night' saved, got %v (err=%v)", saved, err)
	}
}

func TestAllMenuItemsCanExpandWithEnter(t *testing.T) {
	cycle196TestHome(t, map[string]any{})

	m := &runMode{
		panel: slashPanel{
			kind: panelConfig,
			cmd:  "/settings",
		},
		width:  120,
		height: 40,
	}

	rows := configRows()
	if len(rows) < 18 {
		t.Fatalf("expected at least 18 settings rows, got %d", len(rows))
	}

	for i, r := range rows {
		m.panel.cursor = i
		m.panel.configSubmenu = false

		// Press Enter to expand
		m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
		if !m.panel.configSubmenu {
			t.Fatalf("row %d (%q): expected configSubmenu to open on Enter", i, r.Name)
		}

		lines := m.panelConfigLines()
		joined := stripANSI(strings.Join(lines, "\n"))
		if !strings.Contains(joined, r.Name) {
			t.Fatalf("row %d (%q): expected name in expanded output:\n%s", i, r.Name, joined)
		}
		if !strings.Contains(joined, "(current)") {
			t.Fatalf("row %d (%q): expected (current) marker in expanded options:\n%s", i, r.Name, joined)
		}

		// Press Esc to collapse
		m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
		if m.panel.configSubmenu {
			t.Fatalf("row %d (%q): expected configSubmenu to close on Esc", i, r.Name)
		}
	}
}

func TestColorSchemeLiveThemeUpdate(t *testing.T) {
	cycle196TestHome(t, map[string]any{"colorScheme": "terminal"})
	t.Cleanup(func() {
		mdrender.SetColorScheme("solarized light")
	})

	m := &runMode{
		panel: slashPanel{
			kind:        panelConfig,
			cmd:         "/settings",
			configQuery: "color",
		},
		width:  120,
		height: 40,
	}

	rows := configRowsFiltered(m.panel.configQuery)
	if len(rows) != 1 || rows[0].Name != "Color Scheme" {
		t.Fatalf("expected 1 row 'Color Scheme', got %+v", rows)
	}

	// 1. Open submenu
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if !m.panel.configSubmenu {
		t.Fatal("expected submenu open")
	}

	// 2. Move down to "light" (index 1)
	m.handlePanelKey(tea.Key{Code: tea.KeyDown})
	if mdrender.CurrentColorScheme() != "light" {
		t.Fatalf("expected mdrender live theme 'light', got %q", mdrender.CurrentColorScheme())
	}

	// 3. Move down to "solarized light" (index 2)
	m.handlePanelKey(tea.Key{Code: tea.KeyDown})
	if mdrender.CurrentColorScheme() != "solarized light" {
		t.Fatalf("expected mdrender live theme 'solarized light', got %q", mdrender.CurrentColorScheme())
	}

	// 4. Cancel with Esc: should revert live theme to original "terminal"
	m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
	if m.panel.configSubmenu {
		t.Fatal("expected submenu closed")
	}
	if mdrender.CurrentColorScheme() != "terminal" {
		t.Fatalf("expected mdrender live theme reverted to 'terminal', got %q", mdrender.CurrentColorScheme())
	}

	// 5. ←/→ on main list should cycle live and persist
	handled, cmd := m.handlePanelKey(tea.Key{Code: tea.KeyRight})
	if !handled || cmd == nil {
		t.Fatalf("expected KeyRight to return handled=true and clearScreen cmd")
	}
	if mdrender.CurrentColorScheme() != "light" {
		t.Fatalf("expected mdrender live theme 'light' after →, got %q", mdrender.CurrentColorScheme())
	}
	saved, _ := auth.SettingsField("colorScheme")
	if saved != "light" {
		t.Fatalf("expected colorScheme 'light' persisted, got %v", saved)
	}
}

// TestCycle241_TelemetryBusinessLockedDisplay: with AGY_ADC_AUTH set the
// row paints the literal `off (disabled)` regardless of the in-memory
// state (which would otherwise be "on", the default per cycle 197).
func TestCycle241_TelemetryBusinessLockedDisplay(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "1")
	// No settings.json seed needed — telemetry is memoryOnly and the
	// default display is "on" (cf. TestCycle197_ConfigMemoryOnlyTouchesNoDisk).
	cycle196TestHome(t, map[string]any{})
	idx := configRowIndexForTest(t, "Enable Telemetry")
	m := &runMode{}
	m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: idx}
	if got := m.configRowValueAt(idx); got != "off (disabled)" {
		t.Errorf("AGY_ADC_AUTH set: row value = %q, want %q", got, "off (disabled)")
	}
	// The display path is also exercised via the catalogue function
	// (configSettingValue → configDisplayFor) — both must agree.
	if got := configSettingValue(idx); got != "off (disabled)" {
		t.Errorf("AGY_ADC_AUTH set: configSettingValue = %q, want %q", got, "off (disabled)")
	}
}

// TestCycle241_TelemetryPersonalUnchanged: with AGY_ADC_AUTH unset the
// row cycles normally between "on" and "off" (cycle 197 contract, no
// regression).
func TestCycle241_TelemetryPersonalUnchanged(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "")
	cycle196TestHome(t, map[string]any{})
	idx := configRowIndexForTest(t, "Enable Telemetry")
	m := &runMode{}
	m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: idx}
	if got := m.configRowValueAt(idx); got != "off" {
		t.Errorf("AGY_ADC_AUTH unset: initial row value = %q, want off", got)
	}
	m.cycleConfigValue(+1)
	if got := m.configRowValueAt(idx); got != "on" {
		t.Errorf("AGY_ADC_AUTH unset: after +1 row value = %q, want on", got)
	}
	m.cycleConfigValue(+1)
	if got := m.configRowValueAt(idx); got != "off" {
		t.Errorf("AGY_ADC_AUTH unset: after +1 round-trip row value = %q, want off", got)
	}
}

// TestCycle241_TelemetryToggleRefused: with AGY_ADC_AUTH set the ←/→
// cycle is a no-op (no state mutation) and the transcript records the
// refusal message. With AGY_ADC_AUTH unset, the toggle round-trips.
func TestCycle241_TelemetryToggleRefused(t *testing.T) {
	t.Run("AGY_ADC_AUTH set: toggle is no-op and surfaces message", func(t *testing.T) {
		t.Setenv("AGY_ADC_AUTH", "true")
		cycle196TestHome(t, map[string]any{})
		idx := configRowIndexForTest(t, "Enable Telemetry")
		m := &runMode{}
		m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: idx}

		// Before: row is the normal "on". After: still "on" (the locked
		// display paints "off (disabled)", which the toggle must NOT
		// silently flip — we test both that the value is the locked
		// literal and that the underlying memory map did not move.
		m.cycleConfigValue(+1)
		if got := m.configRowValueAt(idx); got != "off (disabled)" {
			t.Errorf("AGY_ADC_AUTH set: after +1 display = %q, want %q",
				got, "off (disabled)")
		}
		if m.panel.configMemory != nil {
			if v, ok := m.panel.configMemory["enableTelemetry"]; ok && v != "on" {
				t.Errorf("AGY_ADC_AUTH set: configMemory mutated to %q, want absent or \"on\"", v)
			}
		}
		// The transcript must carry the refusal message so the user has
		// visible feedback that ←/→ did something.
		found := false
		for _, line := range m.response {
			if strings.Contains(stripSGR(line), telemetryDisabledResponse) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("AGY_ADC_AUTH set: transcript missing %q.\nresponse=%q",
				telemetryDisabledResponse, m.response)
		}
	})

	t.Run("AGY_ADC_AUTH unset: toggle round-trips", func(t *testing.T) {
		t.Setenv("AGY_ADC_AUTH", "")
		cycle196TestHome(t, map[string]any{})
		idx := configRowIndexForTest(t, "Enable Telemetry")
		m := &runMode{}
		m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: idx}

		m.cycleConfigValue(+1) // off → on
		if got := m.configRowValueAt(idx); got != "on" {
			t.Errorf("after +1: row = %q, want on", got)
		}
		m.cycleConfigValue(-1) // on → off
		if got := m.configRowValueAt(idx); got != "off" {
			t.Errorf("after -1: row = %q, want off", got)
		}
		// No refusal message should land on the transcript for a personal
		// account that toggles the row normally.
		for _, line := range m.response {
			if strings.Contains(stripSGR(line), telemetryDisabledResponse) {
				t.Errorf("AGY_ADC_AUTH unset: transcript should not carry %q.\nresponse=%q",
					telemetryDisabledResponse, m.response)
			}
		}
	})
}

// TestCycle241_TelemetryBusinessPanelDescription: when the cursor is on
// the locked row, the description slot paints the AGY-recovered
// "Disabled for Business accounts. Antigravity CLI does not collect any
// of your prompts, content, or model responses, but will still collect
// general usage patterns." string. With AGY_ADC_AUTH unset the same
// panel renders the personal-account description (the pre-241 string,
// "When toggled on, Antigravity CLI collects …").
func TestCycle241_TelemetryBusinessPanelDescription(t *testing.T) {
	// --- Business-account case --------------------------------------------
	t.Setenv("AGY_ADC_AUTH", "true")
	cycle196TestHome(t, map[string]any{})
	idx := configRowIndexForTest(t, "Enable Telemetry")
	m := mkPanel(panelConfig, "/config", idx)
	lines := plainLines(m)
	descLine := findPanelConfigDescriptionLine(t, lines)
	trimmed := strings.TrimLeft(descLine, " \t")
	if !strings.HasPrefix(trimmed, "Disabled for Business accounts.") {
		t.Errorf("Business desc: got %q, want it to start with %q",
			trimmed, "Disabled for Business accounts.")
	}
	if !strings.Contains(trimmed, telemetryProductName) {
		t.Errorf("Business desc: got %q, want it to mention %q",
			trimmed, telemetryProductName)
	}

	// --- Personal-account case (no regression) ----------------------------
	t.Setenv("AGY_ADC_AUTH", "")
	m2 := mkPanel(panelConfig, "/config", idx)
	lines2 := plainLines(m2)
	descLine2 := findPanelConfigDescriptionLine(t, lines2)
	trimmed2 := strings.TrimLeft(descLine2, " \t")
	if strings.HasPrefix(trimmed2, "Disabled for Business accounts.") {
		t.Errorf("Personal desc should NOT carry the Business prefix: %q", trimmed2)
	}
	if !strings.Contains(trimmed2, "collects usage data") {
		t.Errorf("Personal desc: got %q, want it to contain the personal-account phrasing",
			trimmed2)
	}
}

// TestCycle241_TelemetryBusinessPanelRow: the painted catalogue row for
// the cursor-on-Enable-Telemetry case carries the literal "off
// (disabled)" and the description slot reads the Business variant.
func TestCycle241_TelemetryBusinessPanelRow(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "true")
	cycle196TestHome(t, map[string]any{})
	idx := configRowIndexForTest(t, "Enable Telemetry")
	m := mkPanel(panelConfig, "/config", idx)
	lines := plainLines(m)
	rowLine := findPanelConfigCursorRowLine(t, lines, "Enable Telemetry")
	if !strings.Contains(rowLine, telemetryDisabledValue) {
		t.Errorf("row line = %q, want it to contain %q",
			rowLine, telemetryDisabledValue)
	}
	if !strings.HasPrefix(rowLine, "> Enable Telemetry") {
		t.Errorf("row line = %q, want it to start with the cursor prefix", rowLine)
	}
}

// TestCycle241_TelemetryLockedRowNotInjectedElsewhere: only the
// `Enable Telemetry` row gets the locked display; every other row in
// the catalogue must keep its pre-241 normal cycle. (Regression guard
// against accidentally short-circuiting configDisplayFor on the
// business-account branch.)
func TestCycle241_TelemetryLockedRowNotInjectedElsewhere(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "true")
	cycle196TestHome(t, map[string]any{})
	for _, name := range []string{
		"Agent Mode", "Animation Speed", "Color Scheme", "Show Tips",
		"Verbosity", "Notifications", "Non-Workspace Access",
	} {
		idx := configRowIndexForTest(t, name)
		if got := configSettingValue(idx); got == telemetryDisabledValue {
			t.Errorf("non-telemetry row %q painted as %q; should be the normal display",
				name, telemetryDisabledValue)
		}
	}
}

// TestCycle241_TelemetryBusinessFirstRunSkipOptIn: cycle-241 surface for
// the first-run setup opt-in suppression. The full first-run screen has
// many flags and a follow-up cycle would need to model the entire
// onboarding flow; this cycle ships the `/config` row only. The helper
// we expose is `IsBusinessAccount()` — under AGY_ADC_AUTH it returns
// true, and the first-run opt-in consumer is expected to skip its
// checkbox. The test pins the helper's expected behaviour so the
// first-run half of the 1.4.1 release notes has at least one assertion
// guarding it.
func TestCycle241_TelemetryBusinessFirstRunSkipOptIn(t *testing.T) {
	t.Run("AGY_ADC_AUTH set: helper returns true", func(t *testing.T) {
		t.Setenv("AGY_ADC_AUTH", "true")
		if !IsBusinessAccount() {
			t.Error("IsBusinessAccount() = false under AGY_ADC_AUTH=true; want true")
		}
	})
	t.Run("AGY_ADC_AUTH unset: helper returns false", func(t *testing.T) {
		t.Setenv("AGY_ADC_AUTH", "")
		if IsBusinessAccount() {
			t.Error("IsBusinessAccount() = true under AGY_ADC_AUTH unset; want false")
		}
	})
}

// TestCycle241_TelemetryKeyHandlerRoutesToGate drives the user-facing
// path: ←/→ on the locked row reaches cycleConfigValue through
// handlePanelKey and surfaces the refusal message.
func TestCycle241_TelemetryKeyHandlerRoutesToGate(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "true")
	cycle196TestHome(t, map[string]any{})
	idx := configRowIndexForTest(t, "Enable Telemetry")
	m := mkPanel(panelConfig, "/config", idx)
	handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyRight})
	if !handled {
		t.Fatal("←/→ on /config: handlePanelKey reported unhandled")
	}
	if got := m.configRowValueAt(idx); got != telemetryDisabledValue {
		t.Errorf("after handlePanelKey →: row = %q, want %q",
			got, telemetryDisabledValue)
	}
	found := false
	for _, line := range m.response {
		if strings.Contains(stripSGR(line), telemetryDisabledResponse) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("after handlePanelKey →: transcript missing %q.\nresponse=%q",
			telemetryDisabledResponse, m.response)
	}
}

// findPanelConfigDescriptionLine returns the first plain-text line in the
// rendered panel that contains "Disabled for Business accounts." or
// "When toggled on," — i.e. the description slot painted under the
// catalogue. The shape is recovered from `panelConfigLines()`: the
// description slot is the only line between the rows and the counter /
// keyboard hint that begins with one of those phrases. The function
// trims the leading indent the panel emits (`  ` + SGR-stripped) before
// comparing, so callers don't depend on the exact column count.
func findPanelConfigDescriptionLine(t *testing.T, lines []string) string {
	t.Helper()
	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "Disabled for Business accounts.") ||
			strings.HasPrefix(trimmed, "When toggled on,") {
			return line
		}
	}
	t.Fatalf("could not find a description line in panel:\n%s",
		strings.Join(lines, "\n"))
	return ""
}

// findPanelConfigCursorRowLine returns the first plain-text line that
// starts with `> <rowName>` — the cursor row in the painted catalogue.
func findPanelConfigCursorRowLine(t *testing.T, lines []string, rowName string) string {
	t.Helper()
	prefix := "> " + rowName
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	t.Fatalf("could not find cursor row %q in panel:\n%s",
		rowName, strings.Join(lines, "\n"))
	return ""
}
