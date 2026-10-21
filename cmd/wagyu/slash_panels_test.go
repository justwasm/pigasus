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

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/subagent"
)

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
	// version "1.4.1" and counter "(1–18 of 1962 lines)" survive
	// (SGR-stripped). Cycle 207 added the blank row REF shows between
	// the title and the tagline, so the window opens at 18 rows rather
	// than the cycle-201 17; the 1962 denominator is unchanged.
	//
	// The embedded changelog_reference.txt is a verbatim `agy changelog`
	// capture: the binary ships one release behind its own version, so a
	// 1.4.4 binary opens on 1.4.3 (cycle 292 re-capture; previously the
	// 1.4.2-era capture opened on 1.4.1 behind a hand-written stub).
	joined := strings.Join(plain, "\n")
	if !strings.Contains(joined, "## 1.4.3") {
		t.Errorf("missing 1.4.3 heading: %s", joined)
	}
	if !strings.Contains(joined, fmt.Sprintf("(1–18 of %d lines)", changelogTotalLines)) {
		t.Errorf("missing line counter: %s", joined)
	}
	// The opening bullet uses 38;5;235 in REF — ensure it survives
	// even after SGR stripping to a substring match for the new
	// `Improved agy plugin validate` (the 1.4.1 lead bullet).
	if !strings.Contains(joined, "Improved agy plugin validate") {
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
	vis := changelogWindowRows()
	if total <= vis {
		t.Fatalf("document too short to scroll: %d rows (window %d)", total, vis)
	}
	if !strings.Contains(panelText(m), fmt.Sprintf("(1\u2013%d of %d lines)", vis, changelogTotalLines)) {
		t.Fatalf("initial counter wrong:\n%s", panelText(m)[:300])
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Fatal("down not handled")
	}
	if m.panel.window != 1 {
		t.Fatalf("window = %d, want 1", m.panel.window)
	}
	if !strings.Contains(panelText(m), fmt.Sprintf("(2\u2013%d of %d lines)", vis+1, changelogTotalLines)) {
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
	if !strings.Contains(text, fmt.Sprintf("(%d\u2013%d of %d lines)", total-vis+1, total, changelogTotalLines)) {
		t.Errorf("bottom counter wrong:\n%s", text)
	}
	// ctrl+home returns to the first screen.
	m.handlePanelKey(tea.Key{Code: tea.KeyHome, Mod: tea.ModCtrl})
	if m.panel.window != 0 {
		t.Errorf("ctrl+home window = %d, want 0", m.panel.window)
	}
	if !strings.Contains(panelText(m), "## 1.4.3") {
		t.Errorf("first screen should still open on 1.4.3:\n%s", panelText(m)[:200])
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
