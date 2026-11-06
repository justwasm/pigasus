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

package tui

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/mattn/go-runewidth"

	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	"github.com/justwasm/pigasus/internal/core/backend"
	settings "github.com/justwasm/pigasus/internal/core/settings"
	"github.com/justwasm/pigasus/internal/features/auth"
	modelcatalog "github.com/justwasm/pigasus/internal/features/models/catalog"
	tuiviewer "github.com/justwasm/pigasus/internal/ui/foundations/viewer"
	"github.com/justwasm/pigasus/internal/ui/mdrender"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	termcolor "github.com/justwasm/pigasus/internal/ui/render/color"
	"github.com/justwasm/pigasus/internal/ui/slashcommands"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	"github.com/justwasm/pigasus/internal/ui/transcript"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/views/artifactpanel"
	"github.com/justwasm/pigasus/internal/ui/views/changelog"
	"github.com/justwasm/pigasus/internal/ui/views/contextpanel"
	featdiff "github.com/justwasm/pigasus/internal/ui/views/diff/feat"
	"github.com/justwasm/pigasus/internal/ui/views/effortpanel"
	"github.com/justwasm/pigasus/internal/ui/views/mcppanel"
	"github.com/justwasm/pigasus/internal/ui/views/modelpicker"
	"github.com/justwasm/pigasus/internal/ui/views/skillsui"
	"github.com/justwasm/pigasus/internal/ui/views/usagepanel"
	"github.com/justwasm/pigasus/palette"
)

var _ = textinput.New

// mkPanel returns a runMode with the given panel open.
func mkPanel(kind panelregistry.Kind, cmd string, cursor int) *runMode {
	m := &runMode{}
	m.overlays.panel = slashPanel{kind: kind, cmd: cmd, cursor: cursor}
	// Cycle 197: panelregistry.Model reads settings.json for the (current)
	// annotation. The panel itself owns the cursor and the effort
	// slider's position, so it is built with the host's seams and the
	// cursor is handed to it.
	if kind == panelregistry.Model {
		m.overlays.panel.mdl = m.newModelPanel()
		m.overlays.panel.mdl.SetCursor(cursor)
	}
	// Cycle 225: panelregistry.Usage's quota.state defaults to quotaLoading,
	// which forces the height-aware renderer through the loading doc
	// path. Cycle-207 tests (e.g. TestCycle207UsageDocGeometry) key
	// off the loaded doc's exact 20-line shape; they set m.viewport.height
	// directly and never arm a tick, so the loading doc would break
	// them. Pre-seed quotaLoaded (no refresh times — the cycle-207
	// tests pin the doc via m.viewport.width and ignore the live countdown,
	// and the cycle-225 tests drive the loading state through
	// quotaStubRefills / explicit state sets).
	if kind == panelregistry.Usage {
		m.overlays.panel.quota.state = quotaLoaded
	}
	// The /config panel owns its own cursor, so the panel is built with
	// the host's seams and the cursor is handed to it.
	if kind == panelregistry.Config {
		m.overlays.panel.cfg = m.newConfigPanel()
		m.overlays.panel.cfg.SetCursor(cursor)
	}
	// Cycle 381: the /permissions panel owns its own cursor too.
	if kind == panelregistry.Permissions {
		m.overlays.panel.perm = m.newPermissionsPanel()
		m.overlays.panel.perm.SetCursor(cursor)
	}
	// Cycle 381: the /hooks panel owns its own cursor as well.
	if kind == panelregistry.Hooks {
		m.overlays.panel.hk = m.newHooksPanel()
		m.overlays.panel.hk.SetCursor(cursor)
	}
	// Cycle 381: the /agents panel owns its own cursor too.
	if kind == panelregistry.Agents {
		m.overlays.panel.ag = m.newAgentsPanel()
		m.overlays.panel.ag.SetCursor(cursor)
	}
	// Cycle 381: the /feedback panel owns its own cursor as well.
	if kind == panelregistry.Feedback {
		m.overlays.panel.fb = m.newFeedbackPanel()
		m.overlays.panel.fb.SetCursor(cursor)
	}
	// Cycle 381: the /credits panel owns its own cursor too.
	if kind == panelregistry.Credits {
		m.overlays.panel.cr = m.newCreditsPanel()
		m.overlays.panel.cr.SetCursor(cursor)
	}
	// Cycle 381: the /effort panel owns the slider's marker.
	if kind == panelregistry.Effort {
		m.overlays.panel.ef = m.newEffortPanel()
		m.overlays.panel.ef.SetCursor(cursor)
	}
	if kind == panelregistry.Projects {
		m.overlays.panel.proj = m.newProjectsPanel()
	}
	if kind.IsViewer() {
		m.initViewerViewport()
	}
	return m
}

func plainLines(m *runMode) []string {
	var out []string
	for _, l := range m.panelLines() {
		out = append(out, termcolor.StripSGR(l))
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
	panels := map[string]panelregistry.Kind{
		"mcp":         panelregistry.MCP,
		"tasks":       panelregistry.Tasks,
		"effort":      panelregistry.Effort,
		"hooks":       panelregistry.Hooks,
		"permissions": panelregistry.Permissions,
		"agents":      panelregistry.Agents,
		"credits":     panelregistry.Credits,
		"codesearch":  panelregistry.CodeSearch,
		"artifact":    panelregistry.Artifact,
		"feedback":    panelregistry.Feedback,
		"rewind":      panelregistry.Rewind,
		"context":     panelregistry.Context,
		// Cycle 193: A-class panel overlays.
		"model":     panelregistry.Model,
		"config":    panelregistry.Config,
		"plugin":    panelregistry.Plugin,
		"skills":    panelregistry.Skills,
		"usage":     panelregistry.Usage,
		"changelog": panelregistry.Changelog,
		"diff":      panelregistry.Diff,
	}
	for name, want := range panels {
		got, ok := slashcommands.PanelFor(name)
		if !ok || got != want {
			t.Errorf("slashcommands.PanelFor(%q) = (%v, %v), want (%v, true)", name, got, ok, want)
		}
	}
	for _, name := range []string{"clear", "help", "fork", "btw", "voice", "record"} {
		if _, ok := slashcommands.PanelFor(name); ok {
			t.Errorf("slashcommands.PanelFor(%q) unexpectedly opens a panel", name)
		}
	}
	// Cycle charmland-component-adoption/02: `/rename` (no args) now
	// opens panelregistry.Rename — the panel is powered by bubbles/v2/textinput.
	// The with-args case still flows through renameFeedback in
	// slash_actions.go.
	if got, ok := slashcommands.PanelFor("rename"); !ok || got != panelregistry.Rename {
		t.Errorf("slashcommands.PanelFor(%q) = (%v, %v), want (%v, true)",
			"rename", got, ok, panelregistry.Rename)
	}
}

func TestOpenSlashPanel(t *testing.T) {
	m := &runMode{}
	if !m.openSlashPanel("mcp") {
		t.Fatal("openSlashPanel(\"mcp\") = false")
	}
	if m.overlays.panel.kind != panelregistry.MCP || m.overlays.panel.cmd != "/mcp" {
		t.Fatalf("panel = %+v, want kind=%v cmd=/mcp", m.overlays.panel, panelregistry.MCP)
	}
	if m.openSlashPanel("clear") {
		t.Fatal("openSlashPanel(\"clear\") = true")
	}
	// /effort opens on the current effort (high by default).
	m2 := &runMode{}
	m2.openSlashPanel("effort")
	if m2.effortPanel().Cursor() != effortpanel.EffortHigh {
		t.Errorf("effort cursor = %d, want %d (high)",
			m2.effortPanel().Cursor(), effortpanel.EffortHigh)
	}
}

// The reference's /mcp panel, verbatim from
// /tmp/agycmp/panels/ppref.mcp.raw.txt (SGR stripped). The frame itself
// is tested in internal/ui/views/mcppanel; this pins the wiring — the
// host renders the panel's own rows.
func TestPanelMCPLayout(t *testing.T) {
	m := mkPanel(panelregistry.MCP, "/mcp", 0)
	got := m.panelLines()
	want := mcppanel.Lines()
	if len(got) != len(want) {
		t.Fatalf("line count: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d:\n got: %q\nwant: %q", i, got[i], want[i])
		}
	}
}

func TestPanelTasksLayout(t *testing.T) {
	m := mkPanel(panelregistry.Tasks, "/tasks", 0)
	assertLines(t, plainLines(m), []string{
		"Tasks",
		"  No background tasks.",
		"",
		"Keyboard: ↑/↓ Navigate  ←/→ Page  enter View output  k Kill Task  x Remove Task From List",
	})
}

func TestPanelHooksLayout(t *testing.T) {
	m := mkPanel(panelregistry.Hooks, "/hooks", 0)
	assertLines(t, plainLines(m), []string{
		" Hooks",
		"   6 hook types",
		"",
		" > PreToolUse      Before tool execution",
		"   PostToolUse     After tool execution",
		"   PreInvocation   Before each LLM invocation",
		"   PostInvocation  After each LLM invocation",
		"   Stop            When agent tries to exit",
		"   SessionStart    When a session starts",
		"",
		"   ↑/↓ Navigate · enter Select",
	})
	// Moving the cursor moves the highlight, not the text. The cursor is
	// the panel's own (cycle 381), so it is moved through the panel.
	m.hooksPanel().SetCursor(4)
	got := plainLines(m)
	if !strings.HasPrefix(got[7], " > Stop") {
		t.Errorf("row 7 should be selected: %q", got[7])
	}
	if !strings.HasPrefix(got[3], "   PreToolUse") {
		t.Errorf("row 3 should be deselected: %q", got[3])
	}
}

func TestPanelPermissionsLayout(t *testing.T) {
	m := mkPanel(panelregistry.Permissions, "/permissions", 0)
	// Cycle 381: the /permissions panel itself lives in
	// internal/ui/views/permissionspanel — it owns the scope picker, the
	// per-scope lists, the add/edit form, the cursor, the keys and the
	// writes. The host only routes the keys and draws the rows, so what
	// is left here is the wiring: the host's panel answers with the
	// package's rows.
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
		m.permissionsPanel().SetCursor(cursor)
		if got := plainLines(m)[8]; got != wantBlurb {
			t.Errorf("cursor %d blurb = %q, want %q", cursor, got, wantBlurb)
		}
	}
}

func TestPanelCodeSearchLayout(t *testing.T) {
	m := mkPanel(panelregistry.CodeSearch, "/codesearch", 0)
	assertLines(t, plainLines(m), []string{
		"Code Search:",
		// Cycle 351: the usage line matches the reference byte for
		// byte again — agy 1.4.8's string table carries no
		// "f:<glob>" / "file:<glob>" literal.
		"  Usage: /codesearch [-F|--literal] <query>",
		"",
		"Keyboard: ↑/↓ Navigate  ←/→ Prev/Next File  enter Open File  ctrl+g Open in Editor  esc Close Search",
	})
	// The colon sits outside the bold title in REF. The title span is
	// the codesearchpanel's own (bold + default fg, then reset); the
	// host does not keep a copy of it.
	if got := m.panelLines()[0]; got != "\x1b[1m\x1b[39m"+"Code Search"+"\x1b[0m"+":" {
		t.Errorf("title = %q", got)
	}
}

// The /artifact frame is the reference's own; it is tested in
// internal/ui/views/artifactpanel. This pins the wiring — the host
// renders the panel's own rows.
func TestPanelArtifactLayout(t *testing.T) {
	m := mkPanel(panelregistry.Artifact, "/artifact", 0)
	got := m.panelLines()
	want := artifactpanel.Lines()
	if len(got) != len(want) {
		t.Fatalf("line count: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d:\n got: %q\nwant: %q", i, got[i], want[i])
		}
	}
}

// Other panels still clamp; the wrap behaviour is /feedback-only.
func TestHandlePanelKeyHooksClamps(t *testing.T) {
	m := mkPanel(panelregistry.Hooks, "/hooks", 0)
	up := tea.Key{Code: tea.KeyUp}
	if handled, _ := m.handlePanelKey(up); !handled || m.hooksPanel().Cursor() != 0 {
		t.Errorf("Up at 0 should clamp: handled=%v cursor=%d", handled, m.hooksPanel().Cursor())
	}
	for i := 0; i < 10; i++ {
		m.handlePanelKey(tea.Key{Code: tea.KeyDown})
	}
	if m.hooksPanel().Cursor() != 5 {
		t.Errorf("Down should clamp at last row: cursor=%d want 5", m.hooksPanel().Cursor())
	}
}

// Esc on /feedback closes the panel silently — no "⎿  Exited /feedback
// command" echo block (cycle 185's esc_probe.py on /mcp and /permissions,
// and ppfb_afteresc.raw.txt on /feedback).
func TestFeedbackExitNotice(t *testing.T) {
	if panelregistry.Feedback.ExitNotice() {
		t.Error("panelregistry.Feedback.exitNotice() = true, want false")
	}
}

// The /rewind picker's frames are the reference's own (ppref.rewind.
// during.raw.txt for the empty state, cycle 222's one-row-per-turn form
// for the rest); they are tested in internal/ui/views/rewindpanel. This
// pins the wiring — the host renders the panel's own rows, the row
// count is the panel's, and the host's key router reaches the panel.
func TestPanelRewindWiring(t *testing.T) {
	m := mkPanel(panelregistry.Rewind, "/rewind", 0)
	got := m.panelLines()
	want := m.rewindPanel().Lines()
	if len(got) != len(want) {
		t.Fatalf("line count: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d:\n got: %q\nwant: %q", i, got[i], want[i])
		}
	}
	if n := (&runMode{}).panelRegistry().RowCount(panelregistry.Rewind); n != 1 {
		t.Errorf("the registry's row count for rewind on an empty session = %d, want 1", n)
	}
	// The key router hands the navigation to the panel's own cursor.
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Fatal("Down not handled")
	}
	if c := m.rewindPanel().Cursor(); c != 0 {
		t.Errorf("the panel's cursor = %d, want 0 (an empty session has one row)", c)
	}
}

// /rewind closes silently on Esc, same as /feedback.
func TestRewindExitNotice(t *testing.T) {
	if panelregistry.Rewind.ExitNotice() {
		t.Error("panelregistry.Rewind.exitNotice() = true, want false")
	}
}

// TestPanelContextRendersThroughPackage — the host's /context panel
// renders the rows the contextpanel package owns: the host gathers the
// session's facts and the package draws them. The layout itself is
// pinned by the package's own tests; this test pins the wiring.
func TestPanelContextRendersThroughPackage(t *testing.T) {
	m := mkPanel(panelregistry.Context, "/context", 0)
	m.session.model = "Gemini 3.8 Flash (High)"

	got := plainLines(m)
	want := make([]string, 0)
	for _, l := range contextpanel.Lines(m.contextState()) {
		want = append(want, termcolor.StripSGR(l))
	}
	assertLines(t, got, want)
	if len(got) == 0 || got[0] != "└ Context Usage" {
		t.Fatalf("row 0 = %q, want the title", got)
	}
}

// /context closes silently on Esc.
func TestContextExitNotice(t *testing.T) {
	if panelregistry.Context.ExitNotice() {
		t.Error("panelregistry.Context.exitNotice() = true, want false")
	}
}

// The legend rows of /hooks, /feedback and /diff do not use the
// "Keyboard:" prefix and separate their entries with " · ".
func TestPanelDotHint(t *testing.T) {
	got := termcolor.StripSGR(tuilib.PanelDotHint("   ", "↑/↓", "Navigate", "enter", "Select"))
	if got != "   ↑/↓ Navigate · enter Select" {
		t.Errorf("dot hint = %q", got)
	}
}

func TestSlashEchoLines(t *testing.T) {
	// Transcribed from /tmp/agycmp/panels/ppref.rename.raw.txt:
	//   \x1b[1m\x1b[38;5;26m> /rename\x1b[0m
	//   \x1b[1m\x1b[38;5;26m  ⎿  \x1b[0m\x1b[38;2;88;110;117mError: …\x1b[39m
	got := tuilib.SlashEchoLines("/rename", "Error: Please provide a new name for the conversation.")
	if len(got) != 3 {
		t.Fatalf("got %d lines, want 3", len(got))
	}
	if p := termcolor.StripSGR(got[0]); p != "> /rename" {
		t.Errorf("echo line = %q", p)
	}
	if p := termcolor.StripSGR(got[1]); p != "  ⎿  Error: Please provide a new name for the conversation." {
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
	// Cycle 351: the whole set was re-derived from agy 1.4.8. The
	// reference echoes "Exited /<cmd> command" for /config, /diff,
	// /skills, /agents, /effort, /credits, /artifact, /model, /hooks,
	// /tasks and /help; every other panel closes silently — /usage and
	// /add-dir among them.
	silent := map[panelregistry.Kind]bool{
		panelregistry.MCP:         true,
		panelregistry.Permissions: true,
		panelregistry.CodeSearch:  true,
		panelregistry.Feedback:    true,
		panelregistry.Rewind:      true,
		panelregistry.Context:     true,
		panelregistry.Plugin:      true,
		panelregistry.Changelog:   true,
		panelregistry.Usage:       true,
		panelregistry.AddDir:      true,
	}
	for k := panelregistry.MCP; k <= panelregistry.Diff; k++ {
		if got, want := k.ExitNotice(), !silent[k]; got != want {
			t.Errorf("%d.exitNotice() = %v, want %v", k, got, want)
		}
		if got, want := k.KeepStatusHint(), k == panelregistry.CodeSearch; got != want {
			t.Errorf("%d.keepStatusHint() = %v, want %v", k, got, want)
		}
	}
}

func TestClosePanelAppendsExitNotice(t *testing.T) {
	m := mkPanel(panelregistry.Tasks, "/tasks", 0)
	m.closePanel()
	if m.overlays.panel.kind != panelregistry.None {
		t.Fatal("panel not cleared")
	}
	if len(m.engine.response) != 3 {
		t.Fatalf("response = %q, want a 3-line echo block", m.engine.response)
	}
	if p := termcolor.StripSGR(m.engine.response[1]); p != "  ⎿  Exited /tasks command" {
		t.Errorf("exit notice = %q", p)
	}

	// /mcp closes silently.
	m2 := mkPanel(panelregistry.MCP, "/mcp", 0)
	m2.closePanel()
	if len(m2.engine.response) != 0 {
		t.Fatalf("silent close appended %q", m2.engine.response)
	}
}

func TestHandlePanelKeyMovesCursorAndClamps(t *testing.T) {
	m := mkPanel(panelregistry.Hooks, "/hooks", 0)
	for i := 0; i < 20; i++ {
		if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyDown}); !handled {
			t.Fatal("down not handled")
		}
	}
	if m.hooksPanel().Cursor() != 5 {
		t.Errorf("cursor = %d, want 5 (clamped)", m.hooksPanel().Cursor())
	}
	for i := 0; i < 20; i++ {
		m.handlePanelKey(tea.Key{Code: tea.KeyUp})
	}
	if m.hooksPanel().Cursor() != 0 {
		t.Errorf("cursor = %d, want 0 (clamped)", m.hooksPanel().Cursor())
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

func TestCurrentEffortFromFlags(t *testing.T) {
	m := &runMode{}
	if got := m.currentEffort(); got != effortpanel.EffortHigh {
		t.Errorf("nil flags -> %d, want high (%d)", got, effortpanel.EffortHigh)
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
	// Cycle 197: the /model panel reads settings.json for the (current)
	// annotation. Seed ANTIGRAVITY_HOME so the test is hermetic and
	// asserts the slug-driven mark, not a hardcoded default.
	cycle196TestHome(t, map[string]any{
		"model": "gemini-3.8-flash-high",
	})
	m := mkPanel(panelregistry.Model, "/model", 0)
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
	// Cycle 381: the cursor is the panel's own.
	m.modelPanel().SetCursor(2)
	got := plainLines(m)
	if !strings.HasPrefix(got[7], "> ") || !strings.HasPrefix(got[5], "  ") {
		t.Errorf("cursor 2 should highlight row 7: %q / %q", got[5], got[7])
	}
	// Last row carries the keyboard legend.
	last := termcolor.StripSGR(plain[len(plain)-1])
	if !strings.Contains(last, "Navigate") || !strings.Contains(last, "Effort") {
		t.Errorf("legend = %q", last)
	}
	// Cursor highlight uses brand blue (selected row carries cBlueOpen).
	lines := m.panelLines()
	if !strings.Contains(lines[7], cBlueOpen) {
		t.Errorf("selected model row should be brand blue: %q", lines[8])
	}
}

// TestPanelPluginWiring pins the host's wiring for the /plugin panel: the
// panel itself lives in internal/ui/views/pluginsui, and the host renders
// it through its own panelLines(). The layout assertions live next to the
// panel (internal/ui/views/pluginsui/panel_test.go).
func TestPanelPluginWiring(t *testing.T) {
	m := mkPanel(panelregistry.Plugin, "/plugin", 0)
	lines := m.panelLines()
	if len(lines) == 0 {
		t.Fatal("the /plugin panel rendered no lines")
	}
	if !strings.Contains(termcolor.StripSGR(lines[0]), "Plugins") {
		t.Errorf("title = %q", lines[0])
	}
	if panelregistry.Plugin.ExitNotice() {
		t.Error("panelregistry.Plugin.exitNotice() = true, want false")
	}
}

// /skills — cycle 203: the multi-source picker rebuilt on the real
// catalogue. The detailed layout assertions live in
// slash_cycle203_test.go (assertPanelSkillsLayout), keeping this shared
// file's edit to a single delegation.
func TestPanelSkillsLayout(t *testing.T) { assertPanelSkillsLayout(t) }

// /usage — quota viewer with placeholder bars and "└ Models & Quota".
func TestPanelUsageLayout(t *testing.T) {
	m := mkPanel(panelregistry.Usage, "/usage", 0)
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
	last := termcolor.StripSGR(plain[len(plain)-1])
	if !strings.Contains(last, "Scroll") || !strings.Contains(last, "Page") {
		t.Errorf("legend = %q", last)
	}
}

// /changelog — viewer heading in yellow + bullet block + counter.
func TestPanelChangelogLayout(t *testing.T) {
	if panelregistry.Changelog.ExitNotice() {
		t.Error("panelregistry.Changelog.exitNotice() = true, want false")
	}
	m := mkPanel(panelregistry.Changelog, "/changelog", 0)
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
	// capture. Re-captured against the 1.4.10 binary on 2026-10-29, the
	// feed's newest section is 1.4.9 — the 1.4.10 release notes are in the
	// public repository but the served feed does not carry them yet — so
	// the panel opens on 1.4.9.
	joined := strings.Join(plain, "\n")
	if !strings.Contains(joined, "## 1.4.11") {
		t.Errorf("missing 1.4.11 heading: %s", joined)
	}
	if !strings.Contains(joined, fmt.Sprintf("(1–18 of %d lines)", changelog.TotalLines)) {
		t.Errorf("missing line counter: %s", joined)
	}
	if !strings.Contains(joined, "Added a billing model choice after you sign in with Google Cloud") {
		t.Errorf("missing opening bullet: %s", joined)
	}
	last := termcolor.StripSGR(plain[len(plain)-1])
	if !strings.Contains(last, "Navigate") {
		t.Errorf("legend = %q", last)
	}
}

func panelText(m *runMode) string {
	return strings.Join(plainLines(m), "\n")
}

// /usage's window scrolls by ↑/↓ and its counter follows. The document
// is 20 rows with a 17-row window, so there are exactly three scroll
// steps — REF's "(1–17 of 20 lines)" at the top.
func TestPanelUsageViewerScrolls(t *testing.T) {
	m := mkPanel(panelregistry.Usage, "/usage", 0)
	if !strings.Contains(panelText(m), "(1–17 of 20 lines)") {
		t.Fatalf("initial counter wrong:\n%s", panelText(m))
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Fatal("down not handled")
	}
	if m.overlays.panel.window != 1 {
		t.Fatalf("window = %d, want 1", m.overlays.panel.window)
	}
	if !strings.Contains(panelText(m), "(2–18 of 20 lines)") {
		t.Errorf("counter did not follow the window:\n%s", panelText(m))
	}
	// ctrl+end jumps to the last window; the reconstructed tail row shows.
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnd, Mod: tea.ModCtrl}); !handled {
		t.Fatal("ctrl+end not handled")
	}
	if m.overlays.panel.window != 3 {
		t.Fatalf("ctrl+end window = %d, want 3", m.overlays.panel.window)
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
	if m.overlays.panel.window != 3 {
		t.Errorf("down at bottom moved window to %d", m.overlays.panel.window)
	}
	// ctrl+home returns to the top.
	m.handlePanelKey(tea.Key{Code: tea.KeyHome, Mod: tea.ModCtrl})
	if m.overlays.panel.window != 0 {
		t.Errorf("ctrl+home window = %d, want 0", m.overlays.panel.window)
	}
	// pgdn / pgup page by the window height and clamp the same way.
	m.handlePanelKey(tea.Key{Code: tea.KeyPgDown})
	if m.overlays.panel.window != 3 {
		t.Errorf("pgdown window = %d, want 3 (clamped)", m.overlays.panel.window)
	}
	m.handlePanelKey(tea.Key{Code: tea.KeyPgUp})
	if m.overlays.panel.window != 0 {
		t.Errorf("pgup window = %d, want 0", m.overlays.panel.window)
	}
}

// The /changelog viewer scrolls through the whole release-notes document
// and keeps REF's wrapped-line denominator (2014 as measured at 120x40
// against the 1.4.4 binary; 1962 was the 1.4.2-era fixture's).
func TestPanelChangelogViewerScrolls(t *testing.T) {
	m := mkPanel(panelregistry.Changelog, "/changelog", 0)
	total := len(changelog.Document(m.viewport.width, m.viewport.height))
	vis := changelog.WindowRows()
	if total <= vis {
		t.Fatalf("document too short to scroll: %d rows (window %d)", total, vis)
	}
	if !strings.Contains(panelText(m), fmt.Sprintf("(1\u2013%d of %d lines)", vis, changelog.TotalLines)) {
		t.Fatalf("initial counter wrong:\n%s", panelText(m)[:300])
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Fatal("down not handled")
	}
	if m.overlays.panel.window != 1 {
		t.Fatalf("window = %d, want 1", m.overlays.panel.window)
	}
	if !strings.Contains(panelText(m), fmt.Sprintf("(2\u2013%d of %d lines)", vis+1, changelog.TotalLines)) {
		t.Errorf("counter did not follow the window:\n%s", panelText(m)[:300])
	}
	// ctrl+end lands on the final window, which shows the 1.0.0 section.
	m.handlePanelKey(tea.Key{Code: tea.KeyEnd, Mod: tea.ModCtrl})
	if want := total - vis; m.overlays.panel.window != want {
		t.Fatalf("ctrl+end window = %d, want %d", m.overlays.panel.window, want)
	}
	text := panelText(m)
	if !strings.Contains(text, "Initial release of the Antigravity CLI") {
		t.Errorf("bottom of changelog should show the 1.0.0 release:\n%s", text)
	}
	if !strings.Contains(text, fmt.Sprintf("(%d\u2013%d of %d lines)", total-vis+1, total, changelog.TotalLines)) {
		t.Errorf("bottom counter wrong:\n%s", text)
	}
	// ctrl+home returns to the first screen.
	m.handlePanelKey(tea.Key{Code: tea.KeyHome, Mod: tea.ModCtrl})
	if m.overlays.panel.window != 0 {
		t.Errorf("ctrl+home window = %d, want 0", m.overlays.panel.window)
	}
	if !strings.Contains(panelText(m), "## 1.4.11") {
		t.Errorf("first screen should still open on 1.4.11:\n%s", panelText(m)[:200])
	}
}

// The viewers swallow printable input but let modified keys (ctrl+c)
// reach the global bindings; Esc closes them.
func TestPanelViewerKeySwallowAndEsc(t *testing.T) {
	m := mkPanel(panelregistry.Usage, "/usage", 0)
	if handled, _ := m.handlePanelKey(tea.Key{Text: "x"}); !handled {
		t.Error("printable input should be swallowed by a viewer")
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: 'c', Mod: tea.ModCtrl}); handled {
		t.Error("ctrl+c must fall through to the global binding")
	}
	// Cycle 351: /usage closes silently in the reference (agy 1.4.8).
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEsc}); !handled {
		t.Fatal("esc not handled")
	}
	if m.overlays.panel.kind != panelregistry.None {
		t.Fatal("esc did not close the panel")
	}
	if got := termcolor.StripSGR(strings.Join(m.engine.response, "\n")); strings.Contains(got, "Exited") {
		t.Errorf("usage esc should be silent, got %q", got)
	}
	// /changelog closes silently.
	c := mkPanel(panelregistry.Changelog, "/changelog", 0)
	c.handlePanelKey(tea.Key{Code: tea.KeyEsc})
	if len(c.engine.response) != 0 {
		t.Errorf("changelog esc should be silent, got %q", c.engine.response)
	}
}

func TestPanelSkillsDetailView(t *testing.T) {
	m := mkPanel(panelregistry.Skills, "/skills", 0)
	// Cycle 381: the cursor and the detail view are the skills panel's
	// own, so the host parks the cursor on the first skill.
	m.skillsPanel().SetCursor(0)
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("enter not handled")
	}
	sel := m.skillsPanel().Selected()
	if sel.Name == "" {
		if m.skillsPanel().Detail() {
			t.Fatal("detail opened with no enumerated skills")
		}
		return
	}
	if !m.skillsPanel().Detail() {
		t.Fatal("enter did not open the detail view")
	}
	text := panelText(m)
	if !strings.Contains(text, skillsui.DisplayName(sel)) {
		t.Errorf("detail missing the skill name %q:\n%s", sel.Name, text)
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
	if m.skillsPanel().Detail() || m.overlays.panel.kind != panelregistry.Skills {
		t.Fatalf("esc should return to the list: detail=%v kind=%v",
			m.skillsPanel().Detail(), m.overlays.panel.kind)
	}
	if len(m.engine.response) != 0 {
		t.Errorf("returning from detail should not touch the transcript: %q", m.engine.response)
	}
	// From the list, Esc closes and emits the standard notice.
	m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
	if m.overlays.panel.kind != panelregistry.None {
		t.Fatal("second esc did not close the panel")
	}
	if got := termcolor.StripSGR(strings.Join(m.engine.response, "\n")); !strings.Contains(got, "Exited /skills command") {
		t.Errorf("skills esc notice = %q", got)
	}
}

func TestSlashLogout_GeminiAPIKeyEnv(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "fake-key")
	t.Setenv("AGY_ADC_AUTH", "")

	got := tuilib.LogoutFeedback()
	want := "The active session uses a Gemini API key from the environment (GEMINI_API_KEY). To change it, update or unset the environment variable."
	if got != want {
		t.Fatalf("logoutFeedback() with GEMINI_API_KEY = %q, want %q", got, want)
	}
}

func TestSlashLogout_AdcAuthEnv(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("AGY_ADC_AUTH", "1")

	got := tuilib.LogoutFeedback()
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
	if m.overlays.login == nil {
		t.Fatal("expected m.overlays.login to be active without restarting CLI")
	}
	if cmd == nil {
		t.Fatal("expected login init command returned")
	}

	view := m.View().Content
	if !strings.Contains(view, "Welcome to the") || !strings.Contains(view, "You are currently not signed in.") {
		t.Fatalf("expected login view in m.View(), got:\n%s", view)
	}

	m.overlays.login = nil
}

// TestCycle206OpenSlashPanelViaAlias pins the alias rule at the panel
// seam: an alias opens its command's panel, and the panel carries the
// command's *canonical* name.
//
// Cycle 383 (A-31): the panel's name is what the exit notice echoes
// ("Exited /config command"), and the reference carries one literal per
// command — not one per alias. So `/settings` closes with the same
// bytes as `/config`. The test used to pin the typed spelling, which
// is the divergence this finding is about.
func TestCycle206OpenSlashPanelViaAlias(t *testing.T) {
	cases := []struct {
		cmd       string
		canonical string
		kind      panelregistry.Kind
	}{
		{"cs", "codesearch", panelregistry.CodeSearch},
		{"search", "codesearch", panelregistry.CodeSearch},
		{"settings", "config", panelregistry.Config},
		{"quota", "usage", panelregistry.Usage},
		{"undo", "rewind", panelregistry.Rewind},
		{"plugins", "plugin", panelregistry.Plugin},
	}
	for _, c := range cases {
		m := &runMode{}
		if !m.openSlashPanel(c.cmd) {
			t.Errorf("openSlashPanel(%q) = false, want true", c.cmd)
			continue
		}
		if m.overlays.panel.kind != c.kind {
			t.Errorf("openSlashPanel(%q) kind = %v, want %v", c.cmd, m.overlays.panel.kind, c.kind)
		}
		if m.overlays.panel.cmd != "/"+c.canonical {
			t.Errorf("openSlashPanel(%q) cmd = %q, want %q", c.cmd, m.overlays.panel.cmd, "/"+c.canonical)
		}
	}
	m := &runMode{}
	if m.openSlashPanel("clear") {
		t.Error("openSlashPanel(\"clear\") = true, want false")
	}
}

func TestCycle206ListPanelsOwnEveryKey(t *testing.T) {
	kinds := []struct {
		kind panelregistry.Kind
		cmd  string
	}{
		{panelregistry.Tasks, "/tasks"},
		{panelregistry.Artifact, "/artifact"},
		{panelregistry.CodeSearch, "/codesearch"},
		{panelregistry.Context, "/context"},
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
		before := len(m.engine.response)
		for _, key := range keys {
			handled, _ := m.handlePanelKey(key)
			if !handled {
				t.Errorf("%s: key %+v fell through, want swallowed", k.cmd, key)
			}
		}
		if m.overlays.panel.kind != k.kind {
			t.Errorf("%s: panel closed by a non-Esc key", k.cmd)
		}
		if len(m.engine.response) != before {
			t.Errorf("%s: key changed the transcript (%d -> %d)", k.cmd, before, len(m.engine.response))
		}
	}
}

func TestCycle206ListPanelsCtrlCFallsThrough(t *testing.T) {
	for _, k := range []struct {
		kind panelregistry.Kind
		cmd  string
	}{
		{panelregistry.Tasks, "/tasks"},
		{panelregistry.Artifact, "/artifact"},
		{panelregistry.CodeSearch, "/codesearch"},
		{panelregistry.Context, "/context"},
	} {
		m := mkPanel(k.kind, k.cmd, 0)
		if handled, _ := m.handlePanelKey(tea.Key{Code: 'c', Mod: tea.ModCtrl}); handled {
			t.Errorf("%s: ctrl+c must fall through to the global binding", k.cmd)
		}
	}
}

func TestCycle206ListPanelsEscCloses(t *testing.T) {
	cases := []struct {
		kind   panelregistry.Kind
		cmd    string
		notice bool
	}{
		{panelregistry.Tasks, "/tasks", true},
		{panelregistry.Artifact, "/artifact", true},
		{panelregistry.CodeSearch, "/codesearch", false},
		{panelregistry.Context, "/context", false},
	}
	for _, c := range cases {
		m := mkPanel(c.kind, c.cmd, 0)
		handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
		if !handled {
			t.Errorf("%s: esc not handled", c.cmd)
			continue
		}
		if m.overlays.panel.kind != panelregistry.None {
			t.Errorf("%s: esc did not close the panel (kind=%v)", c.cmd, m.overlays.panel.kind)
		}
		got := termcolor.StripSGR(strings.Join(m.engine.response, "\n"))
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
	for _, k := range []panelregistry.Kind{panelregistry.MCP, panelregistry.Permissions} {
		if listPanelKind(k) {
			t.Errorf("listPanelKind(%v) = true, want false", k)
		}
	}
	m := mkPanel(panelregistry.MCP, "/mcp", 0)
	if handled, _ := m.handlePanelKey(tea.Key{Code: 'g', Mod: tea.ModCtrl}); handled {
		t.Error("/mcp: ctrl+g should still fall through (out of cycle-206 scope)")
	}
}

func TestChangelogViewer_VimAndBubbleteaKeybindings(t *testing.T) {
	if !changelog.Loaded() {
		t.Skipf("changelog_reference.txt not reachable from cwd %q — skipping", mustGetwd(t))
	}
	m := mkPanel(panelregistry.Changelog, "/changelog", 0)
	if m.overlays.panel.kind != panelregistry.Changelog {
		t.Fatalf("expected panelregistry.Changelog, got %v", m.overlays.panel.kind)
	}

	if m.overlays.panel.window != 0 {
		t.Fatalf("initial window = %d, want 0", m.overlays.panel.window)
	}

	handled, _ := m.handlePanelKey(tea.Key{Code: 'j', Text: "j"})
	if !handled || m.overlays.panel.window != 1 {
		t.Errorf("after 'j': handled=%v, window=%d, want 1", handled, m.overlays.panel.window)
	}

	handled, _ = m.handlePanelKey(tea.Key{Code: 'k', Text: "k"})
	if !handled || m.overlays.panel.window != 0 {
		t.Errorf("after 'k': handled=%v, window=%d, want 0", handled, m.overlays.panel.window)
	}

	handled, _ = m.handlePanelKey(tea.Key{Code: 'G', Text: "G"})
	spec, _ := m.getPanelRegistry().ViewerSpec(m.overlays.panel.kind)
	maxOffset := tuiviewer.MaxOffset(spec)
	if !handled || m.overlays.panel.window != maxOffset {
		t.Errorf("after 'G': handled=%v, window=%d, want %d", handled, m.overlays.panel.window, maxOffset)
	}

	handled, _ = m.handlePanelKey(tea.Key{Code: 'g', Text: "g"})
	if !handled || m.overlays.panel.window != 0 {
		t.Errorf("after 'g': handled=%v, window=%d, want 0", handled, m.overlays.panel.window)
	}

	handled, _ = m.handlePanelKey(tea.Key{Code: 'd', Text: "d"})
	half := spec.Visible / 2
	if half < 1 {
		half = 1
	}
	if !handled || m.overlays.panel.window != half {
		t.Errorf("after 'd': handled=%v, window=%d, want %d", handled, m.overlays.panel.window, half)
	}

	handled, _ = m.handlePanelKey(tea.Key{Code: 'u', Text: "u"})
	if !handled || m.overlays.panel.window != 0 {
		t.Errorf("after 'u': handled=%v, window=%d, want 0", handled, m.overlays.panel.window)
	}

	handled, _ = m.handlePanelKey(tea.Key{Code: 'q', Text: "q"})
	if !handled || m.overlays.panel.kind != panelregistry.None {
		t.Errorf("after 'q': handled=%v, panel.kind=%v, want panelregistry.None", handled, m.overlays.panel.kind)
	}
}

func TestFeatDiff_PanelLines(t *testing.T) {
	if !featdiff.SourceLoaded() {
		t.Skipf("feature_diff_table.json not reachable from cwd %q — skipping", mustGetwd(t))
	}
	rows := featdiff.PanelLines()
	if len(rows) == 0 {
		t.Fatal("featdiff.PanelLines() = empty; want title + summary + ≥ 1 data row")
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

// TestUsageStateGathers — the host's side of the /usage panel: it gathers
// the panel's facts and the panel draws them. The document's own rows are
// pinned by the usagepanel package's tests.
func TestUsageStateGathers(t *testing.T) {
	m := mkPanel(panelregistry.Usage, "/usage", 0)
	m.session.email = "a@b.c"
	m.viewport.width, m.viewport.height = 220, 50
	m.engine.quotaFrame = 3

	st := m.usageState()
	if st.Email != "a@b.c" {
		t.Errorf("email = %q", st.Email)
	}
	if st.Width != 220 || st.Height != 50 {
		t.Errorf("size = (%d, %d), want (220, 50)", st.Width, st.Height)
	}
	if st.Loading {
		t.Error("loading = true, want false before the panel is opened")
	}
	if st.Frame != 3 {
		t.Errorf("frame = %d, want 3", st.Frame)
	}
	if st.Summary != nil {
		t.Errorf("summary = %v, want nil", st.Summary)
	}

	// The quota call is in flight: the panel shows the spinner row.
	m.overlays.panel.quota.state = quotaLoading
	if st = m.usageState(); !st.Loading {
		t.Error("loading = false, want true while the quota call is in flight")
	}

	// The two buckets' refresh times reach the panel.
	g := time.Now().Add(168 * time.Hour)
	o := time.Now().Add(48 * time.Hour)
	m.overlays.panel.quota.refreshG = g
	m.overlays.panel.quota.refreshO = o
	m.overlays.panel.quota.state = quotaLoaded
	st = m.usageState()
	if st.Loading {
		t.Error("loading = true, want false once the answer landed")
	}
	if !st.RefreshG.Equal(g) || !st.RefreshO.Equal(o) {
		t.Errorf("refresh times = (%v, %v), want (%v, %v)", st.RefreshG, st.RefreshO, g, o)
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
		got := usagepanel.QuotaCountdown(time.Now().Add(tc.d))
		if got != tc.want {
			t.Errorf("%s: usagepanel.QuotaCountdown(%v) = %q, want %q", tc.name, tc.d, got, tc.want)
		}
	}
}

func TestCycle225QuotaStubRefills(t *testing.T) {
	m := mkPanel(panelregistry.Usage, "/usage", 0)
	m.overlays.panel.quota.state = quotaLoading
	before := time.Now()
	quotaStubRefills(m)
	after := time.Now()
	if m.overlays.panel.quota.state != quotaLoaded {
		t.Errorf("state = %d, want quotaLoaded (%d)", m.overlays.panel.quota.state, quotaLoaded)
	}
	// The stub stamps its refresh times with a time.Now() of its own,
	// so they land somewhere between `before` and `after`. The window
	// is inclusive on both ends: two time.Now() calls can share one
	// clock tick, and a strict comparison on the lower bound fails
	// whenever they do.
	loG, hiG := before.Add(168*time.Hour), after.Add(168*time.Hour)
	if m.overlays.panel.quota.refreshG.Before(loG) || m.overlays.panel.quota.refreshG.After(hiG) {
		t.Errorf("refreshG = %v, want in [%v, %v]",
			m.overlays.panel.quota.refreshG, loG, hiG)
	}
	loO, hiO := before.Add(48*time.Hour), after.Add(48*time.Hour)
	if m.overlays.panel.quota.refreshO.Before(loO) || m.overlays.panel.quota.refreshO.After(hiO) {
		t.Errorf("refreshO = %v, want in [%v, %v]",
			m.overlays.panel.quota.refreshO, loO, hiO)
	}
}

func TestCycle225UsageQuotaTickHandler(t *testing.T) {
	m := &runMode{}
	m.overlays.panel = slashPanel{kind: panelregistry.None}
	if _, cmd := m.Update(usageQuotaTickMsg{}); cmd != nil {
		t.Errorf("closed panel returned a cmd: %v", cmd)
	}
	if m.engine.quotaFrame != 0 {
		t.Errorf("quotaFrame = %d, want 0", m.engine.quotaFrame)
	}

	m = mkPanel(panelregistry.Usage, "/usage", 0)
	m.overlays.panel.quota.state = quotaLoading
	for i := 0; i < usageQuotaLoadingTicks; i++ {
		_, cmd := m.Update(usageQuotaTickMsg{})
		if cmd == nil {
			t.Fatalf("tick %d returned nil cmd", i)
		}
	}
	if m.overlays.panel.quota.state != quotaLoaded {
		t.Errorf("state = %d, want quotaLoaded", m.overlays.panel.quota.state)
	}
	if m.engine.quotaFrame != usageQuotaLoadingTicks {
		t.Errorf("quotaFrame = %d, want %d", m.engine.quotaFrame, usageQuotaLoadingTicks)
	}

	_, cmd := m.Update(usageQuotaTickMsg{})
	if cmd == nil {
		t.Error("post-flip tick returned nil cmd")
	}
	if m.engine.quotaFrame != usageQuotaLoadingTicks+1 {
		t.Errorf("quotaFrame = %d, want %d", m.engine.quotaFrame, usageQuotaLoadingTicks+1)
	}
}

func TestCycle225UsagePanelHeightAware(t *testing.T) {
	m := mkPanel(panelregistry.Usage, "/usage", 0)
	m.viewport.width, m.viewport.height = 220, 50
	m.overlays.panel.quota.state = quotaLoading
	m.engine.quotaFrame = 0
	lines := m.panelLines()
	if len(lines) != 7 {
		t.Fatalf("loading-state panel = %d rows, want 7", len(lines))
	}
	if !strings.Contains(termcolor.StripSGR(lines[4]), "Loading quota summary") {
		t.Errorf("row 4 should be the loading row: %q", termcolor.StripSGR(lines[4]))
	}

	quotaStubRefills(m)
	m.overlays.panel.window = 0
	lines = m.panelLines()
	if len(lines) != 26 {
		t.Errorf("loaded-state panel = %d rows, want 26", len(lines))
	}
	if !strings.Contains(termcolor.StripSGR(lines[8]), "100.00%") {
		t.Errorf("row 8 should be the Gemini 100%% bar: %q", termcolor.StripSGR(lines[8]))
	}
}

func TestCycle225UsageViewerStaticRegression(t *testing.T) {
	m := mkPanel(panelregistry.Usage, "/usage", 0)
	if m.viewport.height != 0 {
		t.Fatalf("mkPanel did not seed height=0 (got %d)", m.viewport.height)
	}
	// The document is the panel's own: at height 0 the viewer spec hands
	// out the static cycle-193 rows.
	spec, ok := m.getPanelRegistry().ViewerSpec(m.overlays.panel.kind)
	if !ok {
		t.Fatal("viewerSpecFor returned ok=false for panelregistry.Usage")
	}
	doc := spec.Lines
	if len(doc) != 20 {
		t.Fatalf("doc = %d rows, want 20", len(doc))
	}
	if !strings.Contains(doc[7], "\x1b[1mWeekly Limit Remaining\x1b[0m") {
		t.Errorf("doc[7] = %q", doc[7])
	}
	if !strings.Contains(doc[9], "Quota available") {
		t.Errorf("doc[9] should preserve the cycle-193 'Quota available' line: %q", doc[9])
	}
	if spec.Total != 20 {
		t.Errorf("spec.Total = %d, want 20", spec.Total)
	}
}

func TestCycle225OpenUsagePanelArmsLoading(t *testing.T) {
	m := &runMode{}
	m.viewport.width, m.viewport.height = 220, 50
	if !m.openSlashPanel("usage") {
		t.Fatal("openSlashPanel(\"usage\") = false")
	}
	if m.overlays.panel.kind != panelregistry.Usage {
		t.Errorf("panel kind = %v, want panelregistry.Usage", m.overlays.panel.kind)
	}
	if m.overlays.panel.quota.state != quotaLoading {
		t.Errorf("quota.state = %d, want quotaLoading", m.overlays.panel.quota.state)
	}
}

// ---- cycle 223: panelregistry.AddDir (TODO §2.1 last A-class panel) ----------------

// TestPanelAddDirForCommand verifies the command → panel mapping for
// the cycle 223 entry. The mapping is the vocabulary's now; the panel
// registry still owns the table it reads.
func TestPanelAddDirForCommand(t *testing.T) {
	if got, ok := slashcommands.PanelFor("add-dir"); !ok || got != panelregistry.AddDir {
		t.Errorf("slashcommands.PanelFor(\"add-dir\") = (%v, %v), want (panelregistry.AddDir, true)", got, ok)
	}
	// The default open path (used by the TUI dispatcher) also lands
	// on panelregistry.AddDir.
	m := &runMode{}
	if !m.openSlashPanel("add-dir") {
		t.Fatal("openSlashPanel(\"add-dir\") = false")
	}
	if m.overlays.panel.kind != panelregistry.AddDir || m.overlays.panel.cmd != "/add-dir" {
		t.Errorf("after openSlashPanel: panel = (%v, %q), want (panelregistry.AddDir, \"/add-dir\")",
			m.overlays.panel.kind, m.overlays.panel.cmd)
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
	if m.overlays.panel.kind != panelregistry.AddDir {
		t.Errorf("bare /add-dir did not open panel: kind = %v", m.overlays.panel.kind)
	}
	m2 := &runMode{}
	if m2.openSlashPanelArgs("add-dir", "/tmp/foo") {
		t.Fatal("openSlashPanelArgs(\"add-dir\", \"/tmp/foo\") = true, want false (fall through)")
	}
	if m2.overlays.panel.kind != panelregistry.None {
		t.Errorf("panel should remain panelregistry.None after fall-through, got %v", m2.overlays.panel.kind)
	}
}

// TestPanelAddDirTrustedListed verifies the trustedWorkspaces list is
// drawn verbatim from trust.LoadTrustedWorkspaces (the same reader the
// on-startup trust gate and cycle-103 /add-dir already use). Cycle
// 217's "Workspace added" wording lives downstream of addDirFeedback
// and is exercised separately in TestPanelAddDirEnterEchoes.
func TestPanelAddDirTrustedListed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	writeSettings(t, home, `{"trustedWorkspaces":["/Users/foo/a","/Users/foo/b"]}`)

	m := mkPanel(panelregistry.AddDir, "/add-dir", 0)
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

	m := mkPanel(panelregistry.AddDir, "/add-dir", 0)
	m.addDirPanel().SetText(sub)
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("Enter not handled")
	}
	if m.overlays.panel.kind != panelregistry.None {
		t.Errorf("panel still open: kind = %v", m.overlays.panel.kind)
	}
	resp := termcolor.StripSGR(strings.Join(m.engine.response, "\n"))
	if !strings.Contains(resp, "> /add-dir") {
		t.Errorf("missing slashRespond header; m.engine.response=%q", resp)
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

	m := mkPanel(panelregistry.AddDir, "/add-dir", 0)
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("Enter not handled")
	}
	if m.overlays.panel.kind != panelregistry.None {
		t.Errorf("panel did not close: kind = %v", m.overlays.panel.kind)
	}
	resp := termcolor.StripSGR(strings.Join(m.engine.response, "\n"))
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

	m := mkPanel(panelregistry.AddDir, "/add-dir", 0)
	m.addDirPanel().SetText(home + "/nope")
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("Enter not handled")
	}
	if m.overlays.panel.kind != panelregistry.None {
		t.Errorf("panel did not close: kind = %v", m.overlays.panel.kind)
	}
	resp := termcolor.StripSGR(strings.Join(m.engine.response, "\n"))
	if !strings.Contains(resp, "error: directory not found:") {
		t.Errorf("missing validation echo; got %q", resp)
	}
}

// TestPanelAddDirEsc — Esc closes the panel. Cycle 351: the reference's
// /add-dir is not a panel at all (it answers "error: no directory
// specified" inline) and it never carries an "Exited /add-dir command"
// notice, so the close is silent.
func TestPanelAddDirEsc(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	writeSettings(t, home, `{}`)

	m := mkPanel(panelregistry.AddDir, "/add-dir", 0)
	// Garbage that wouldn't validate, but Esc ignores it.
	m.addDirPanel().SetText("/tmp/foo")
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEsc}); !handled {
		t.Fatal("Esc not handled")
	}
	if m.overlays.panel.kind != panelregistry.None {
		t.Errorf("panel still open: kind = %v", m.overlays.panel.kind)
	}
	resp := termcolor.StripSGR(strings.Join(m.engine.response, "\n"))
	if strings.Contains(resp, "Exited /add-dir command") {
		t.Errorf("add-dir esc should be silent; got %q", resp)
	}
}

// TestPanelAddDirExitNotice — `/add-dir` closes silently: the 1.4.8
// binary carries no "Exited /add-dir command" literal, and the live
// reference answers /add-dir inline instead of opening a panel.
func TestPanelAddDirExitNotice(t *testing.T) {
	if panelregistry.AddDir.ExitNotice() {
		t.Error("panelregistry.AddDir.exitNotice() = true, want false (the reference's /add-dir closes silently)")
	}
}

// Test_StatusRowRightLabelColouredInHelpMode verifies cycle 182: the
// right-aligned model label keeps its dim-gray wrap while the /help
// overlay owns the screen. Pre-182 it reused leftOpen/leftClose, which
// the /help blanking branch had cleared to "", so the label was
// emitted uncoloured while REF keeps `38;2;101;123;131` (tmux) / rgb(95,99,104).
func Test_StatusRowRightLabelColouredInHelpMode(t *testing.T) {
	m := newRunMode(&fakeClient{}, "u@t", "Gemini 3.8 Flash (High)", "Antigravity Starter Quota", "/", true)
	m.viewport.width = 220
	m.viewport.helpPanel.Open(0)
	if m.session.statusModel != "Gemini 3.8 Flash" {
		t.Fatalf("statusModel = %q, want %q", m.session.statusModel, "Gemini 3.8 Flash")
	}

	// The " · high" label is the model's effort suffix; the row paints
	// it inside the same dim-gray wrap as the model name.
	suffix := m.statusEffortSuffix()
	if suffix == "" {
		t.Fatalf("the effort suffix is empty for %q", m.session.statusModel)
	}

	row := m.statusRow()
	if !strings.Contains(row, cDimGrayOpen+"Gemini 3.8 Flash"+suffix+cDimGrayClose) {
		t.Errorf("the /help status row lost the right-label colour: %q", row)
	}
	if strings.Contains(row, "? for shortcuts") {
		t.Errorf("the /help status row should not carry the left hint: %q", row)
	}

	// The idle row must keep the same wrap (regression guard: the fix
	// must not change the byte-exact idle capture).
	m.viewport.helpPanel.Cancel()
	idle := m.statusRow()
	if !strings.Contains(idle, cDimGrayOpen+"? for shortcuts"+cDimGrayClose) {
		t.Errorf("idle left label lost its wrap: %q", idle)
	}
	if !strings.Contains(idle, cDimGrayOpen+"Gemini 3.8 Flash"+suffix+cDimGrayClose) {
		t.Errorf("idle right label lost its wrap: %q", idle)
	}
}

// seedRewindSession writes a synthetic N-turn session into m. Each user
// message is followed by an "ack" assistant message so the trailing pair
// pattern matches a real chat.
func seedRewindSession(m *runMode, snippets []string) {
	for _, s := range snippets {
		m.engine.session = append(m.engine.session,
			backend.Message{Role: "user", Content: s},
			backend.Message{Role: "assistant", Content: "ack: " + s},
		)
	}
}

// The /rewind picker's own frames — the empty state, the one-row-per-
// turn form, the windowed forms and the cursor's movement — are tested
// in internal/ui/views/rewindpanel, with the package that owns them.
// The host's own facts — the rewind's effect on the session and the
// transcript's echo — are pinned below.

// TestPanelRewindEnterTruncates — selecting a non-current step and
// pressing Enter truncates m.engine.session and emits a slashRespond echo
// block via the cycle-195 `> /rewind / ⎿` format. The panel then
// closes silently (same exit-notice=false set as /mcp /feedback).
func TestPanelRewindEnterTruncates(t *testing.T) {
	m := mkPanel(panelregistry.Rewind, "/rewind", 0)
	m.viewport.width, m.viewport.height = 220, 50
	seedRewindSession(m, []string{"first", "second", "third"})
	// Move down twice to land on Turn 3 (the oldest = "first" turn).
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Error("Down not handled")
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Error("Down x2 not handled")
	}
	if m.rewindPanel().Cursor() != 2 {
		t.Fatalf("cursor = %d, want 2", m.rewindPanel().Cursor())
	}
	// Session has 6 messages before the test runs Enter.
	if len(m.engine.session) != 6 {
		t.Fatalf("session before Enter: %d, want 6", len(m.engine.session))
	}
	// Press Enter — the picker closes (closePanel returns a nil Cmd;
	// the panel-clearing mutation is the deliverable), the echo fires,
	// session truncates.
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatalf("Enter not handled")
	}
	// 6 - 2 = 4 messages dropped — the picked Turn 3 (oldest) is the
	// "first" user/assistant pair; everything after that survives.
	if len(m.engine.session) != 2 {
		t.Errorf("after Enter session = %d messages, want 2", len(m.engine.session))
	}
	if m.engine.session[0].Role != "user" || m.engine.session[0].Content != "first" {
		t.Errorf("truncated head user = %+v, want first", m.engine.session[0])
	}
	// The slashRespond echo must be present in m.engine.response.
	resp := strings.Join(m.engine.response, "\n")
	if !strings.Contains(resp, "> /rewind") {
		t.Errorf("echo block missing slashRespond header; m.engine.response=%q",
			strings.Join(m.engine.response, "\n"))
	}
	if !strings.Contains(resp, "Rewound to step") {
		t.Errorf("echo body missing Rewound to step; got %q", resp)
	}
	if !strings.Contains(resp, "4 message(s) removed") {
		t.Errorf("echo should report 4 removed, got %q", resp)
	}
	// Panel must close on Enter.
	if m.overlays.panel.kind != panelregistry.None {
		t.Errorf("panel did not close: kind = %v", m.overlays.panel.kind)
	}
}

// TestPanelRewindEnterOnCurrent — pressing Enter while on the cursor's
// default (most-recent) position (the `(current)` row) is a no-op:
// no messages are removed, no echo is emitted. This matches REF, which
// silently dismisses the picker when the user picks the current step.
func TestPanelRewindEnterOnCurrent(t *testing.T) {
	m := mkPanel(panelregistry.Rewind, "/rewind", 0)
	m.viewport.width, m.viewport.height = 220, 50
	seedRewindSession(m, []string{"a", "b", "c"})
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Error("Enter not handled")
	}
	if len(m.engine.session) != 6 {
		t.Errorf("session = %d messages after Enter-on-current, want 6",
			len(m.engine.session))
	}
	if len(m.engine.response) != 0 {
		t.Errorf("echo should be empty on Enter-on-current; got %q",
			strings.Join(m.engine.response, "\n"))
	}
	// Panel must close on Enter.
	if m.overlays.panel.kind != panelregistry.None {
		t.Errorf("panel did not close: kind = %v", m.overlays.panel.kind)
	}
}

// TestRewindExitNoticeCycle222 — panelregistry.Rewind remains in the silent-close
// set (cycle 189 added it; cycle 222 keeps it).
func TestRewindExitNoticeCycle222(t *testing.T) {
	if panelregistry.Rewind.ExitNotice() {
		t.Error("panelregistry.Rewind.exitNotice() = true, want false (silent close)")
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
	m := &runMode{session: sessionState{conversationName: "vacation planning"}}
	m.openRenamePanel()
	if m.overlays.panel.kind != panelregistry.Rename {
		t.Fatalf("panel.kind = %v, want panelregistry.Rename", m.overlays.panel.kind)
	}
	if got := m.renamePanel().Value(); got != "vacation planning" {
		t.Errorf("textinput value = %q, want %q (current conversation name)",
			got, "vacation planning")
	}
	if !m.renamePanel().Focused() {
		t.Errorf("textinput not focused on open — keystrokes will be dropped")
	}
}

// TestSlashRenameBareOpensSeededPanel is the cycle-381 regression
// test for the bare `/rename` path.
//
// The slash router opens the panel through openSlashPanel, not through
// openRenamePanel — so the panel has to be built there. It was not:
// the bare command opened a panel with an *empty, unfocused* input and
// no prompt, and the typed name went nowhere.
func TestSlashRenameBareOpensSeededPanel(t *testing.T) {
	m := newRunMode(nil, "u@t", "m", "t", t.TempDir(), false)
	m.session.conversationName = "trip"
	m.dispatchSlashLine("/rename")
	if m.overlays.panel.kind != panelregistry.Rename {
		t.Fatalf("panel.kind = %v, want panelregistry.Rename", m.overlays.panel.kind)
	}
	if got := m.renamePanel().Value(); got != "trip" {
		t.Errorf("the panel's value = %q, want %q (the current name)", got, "trip")
	}
	if !m.renamePanel().Focused() {
		t.Error("the panel must open focused — the first keystroke would be dropped")
	}
	lines := m.panelLines()
	if len(lines) == 0 || !strings.Contains(termcolor.StripSGR(lines[0]), "Rename Conversation") {
		t.Errorf("the panel must draw its own rows; got %q", lines)
	}
}

// TestOpenRenamePanel_EmptySeed confirms a conversation without a
// current name opens an empty (placeholder-visible) textinput.
func TestOpenRenamePanel_EmptySeed(t *testing.T) {
	m := &runMode{}
	m.openRenamePanel()
	if m.overlays.panel.kind != panelregistry.Rename {
		t.Fatalf("panel.kind = %v, want panelregistry.Rename", m.overlays.panel.kind)
	}
	if got := m.renamePanel().Value(); got != "" {
		t.Errorf("textinput value = %q, want empty", got)
	}
}

// TestHandleRenamePanelKey_EnterSubmits exercises the submit path.
// The typed name is committed via renameFeedback; the panel closes
// silently (cycle-193 silent-on-Esc set also includes panelregistry.Rename per
// exitNotice()). A successful rename emits the
// "Conversation renamed to: %s" line; an empty submit emits the
// "Error: Please provide a new name for the conversation." line —
// matching the cycle-106 cmd-line path byte-for-byte.
func TestHandleRenamePanelKey_EnterSubmits(t *testing.T) {
	m := &runMode{session: sessionState{conversationName: "trip"}}
	m.openRenamePanel()
	m.renamePanel().SetValue("summer trip 2026")
	// closePanelSilent() returns nil — the panel-clearing mutation is
	// the deliverable, not a tea.Cmd (matches cycle 222's picker-close
	// test pattern; closePanel/closePanelSilent have always returned
	// nil, see slash_panels.go:3727 and 3744).
	if handled, _ := m.renamePanel().HandleKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Errorf("Enter not handled")
	}
	if m.overlays.panel.kind != panelregistry.None {
		t.Errorf("panel still open after submit (kind=%v)", m.overlays.panel.kind)
	}
	// renameFeedback emits the message via slashRespond; the message
	// ends up in m.engine.response. Verify the success line.
	if !containsResponse(m.engine.response, "Conversation renamed to: summer trip 2026") {
		t.Errorf("submit did not emit success echo: %v", m.engine.response)
	}
	if m.session.conversationName != "summer trip 2026" {
		t.Errorf("conversationName = %q, want %q", m.session.conversationName, "summer trip 2026")
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
	m.renamePanel().SetValue("")
	handled, _ := m.renamePanel().HandleKey(tea.Key{Code: tea.KeyEnter})
	if !handled {
		t.Errorf("Enter not handled in empty state")
	}
	if !containsResponse(m.engine.response, "Error: Please provide a new name") {
		t.Errorf("empty submit did not emit error echo: %v", m.engine.response)
	}
}

// TestHandleRenamePanelKey_EscClosesSilently confirms Esc exits the
// panel without emitting any exit notice (panelregistry.Rename is in the
// cycle-193 silent-on-Ec set per exitNotice()), matching /mcp,
// /feedback, /plugin, etc. — not /rename's previous behaviour.
func TestHandleRenamePanelKey_EscClosesSilently(t *testing.T) {
	m := &runMode{}
	m.openRenamePanel()
	handled, _ := m.renamePanel().HandleKey(tea.Key{Code: tea.KeyEsc})
	if !handled {
		t.Errorf("Esc not handled")
	}
	if m.overlays.panel.kind != panelregistry.None {
		t.Errorf("panel still open after Esc (kind=%v)", m.overlays.panel.kind)
	}
	if containsResponse(m.engine.response, "Exited /rename") {
		t.Errorf("Esc emitted an exit notice — panelregistry.Rename should be silent: %v",
			m.engine.response)
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
	m.renamePanel().HandleKey(tea.Key{Code: tea.KeyEnter})
	if m.overlays.panel.kind != panelregistry.None {
		t.Errorf("panel still open after empty submit (kind=%v)", m.overlays.panel.kind)
	}
	if !containsResponse(m.engine.response, "Error: Please provide a new name") {
		t.Errorf("empty submit did not echo error: %v", m.engine.response)
	}
}

// containsResponse reports whether any line in response starts with
// substr. The panel flows through slashRespond which appends multiple
// lines (the bold-blue bullet, the dim-gray message, trailing blank);
// we only need the message line to contain the expected text.
func containsResponse(response []string, substr string) bool {
	for _, l := range response {
		if strings.Contains(termcolor.StripSGR(l), substr) {
			return true
		}
	}
	return false
}

func TestF2_GlobalOpensRenamePanel(t *testing.T) {
	m := newRunMode(&fakeClient{}, "u@t", "m", "t", "/", false)
	m.session.conversationName = "My Initial Name"

	m.Update(tea.KeyPressMsg{Code: tea.KeyF2})
	if m.overlays.panel.kind != panelregistry.Rename {
		t.Fatalf("F2 did not open rename panel; panel.kind = %v", m.overlays.panel.kind)
	}
	if got := m.renamePanel().Value(); got != "My Initial Name" {
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
		if got := tuilib.NormalizeTitle(tc.in); got != tc.want {
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
		if got := tuilib.NormalizeTitle(tc.in); got != tc.want {
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
		if got := tuilib.NormalizeTitle(tc.in); got != tc.want {
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
		if got := tuilib.NormalizeTitle(tc.in); got != tc.want {
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
		if got := tuilib.NormalizeTitle(in); got != "" {
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
			got, msg := tuilib.RenameFeedback(tc.arg, tc.current)
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
			if m.session.conversationName != tc.want {
				t.Errorf("conversationName = %q, want %q", m.session.conversationName, tc.want)
			}
			if got := m.conversationTitle(); got != tc.want {
				t.Errorf("conversationTitle() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestConversationTitle_DisplayNormalisesUncleanStoredName(t *testing.T) {
	m := newSlashModel(t)
	m.session.conversationName = " My Display Title\n"
	if got := m.conversationTitle(); got != "My Display Title" {
		t.Errorf("conversationTitle() = %q, want %q", got, "My Display Title")
	}
	m.session.conversationName = "\n   \n"
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
		if got := tuilib.NormalizeTitle(in); got != want {
			t.Errorf("normalizeTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

// ---------------------------------------------------------------- /usage

// The /usage panel shows a counter only while the document is taller than
// the window, and the counter sits directly under the window.
func TestCycle207UsageViewerHeight(t *testing.T) {
	// h=50: the 20-line document fits in 37 rows — no counter, 26 rows total.
	m := mkPanel(panelregistry.Usage, "/usage", 0)
	m.viewport.width, m.viewport.height = 220, 50
	lines := m.panelLines()
	if len(lines) != 26 {
		t.Fatalf("h=50 panel = %d rows, want 26", len(lines))
	}
	for _, l := range lines {
		if strings.Contains(termcolor.StripSGR(l), "of 20 lines") {
			t.Errorf("h=50 must not show a counter: %q", l)
		}
	}
	if !strings.Contains(termcolor.StripSGR(strings.Join(lines, "\n")), "directly to your individual tier.") {
		t.Error("h=50 should show the whole note")
	}

	// h=24: window 11 of 20 — counter (1–11 of 20 lines), 18 rows total.
	m2 := mkPanel(panelregistry.Usage, "/usage", 0)
	m2.viewport.width, m2.viewport.height = 220, 24
	lines2 := m2.panelLines()
	if len(lines2) != 18 {
		t.Fatalf("h=24 panel = %d rows, want 18", len(lines2))
	}
	if !strings.Contains(termcolor.StripSGR(strings.Join(lines2, "\n")), "(1–11 of 20 lines)") {
		t.Errorf("h=24 counter wrong:\n%s", strings.Join(plainLines(m2), "\n"))
	}

	// Scrolling follows the window: one Down moves the counter to (2–12).
	if handled, _ := m2.handlePanelKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Fatal("down not handled")
	}
	if m2.overlays.panel.window != 1 {
		t.Fatalf("window = %d, want 1", m2.overlays.panel.window)
	}
	if !strings.Contains(termcolor.StripSGR(strings.Join(m2.panelLines(), "\n")), "(2–12 of 20 lines)") {
		t.Errorf("counter did not follow:\n%s", strings.Join(plainLines(m2), "\n"))
	}
	// ctrl+end lands on the last window (offset 9 → (10–20 of 20 lines)).
	m2.handlePanelKey(tea.Key{Code: tea.KeyEnd, Mod: tea.ModCtrl})
	if m2.overlays.panel.window != 9 {
		t.Fatalf("ctrl+end window = %d, want 9", m2.overlays.panel.window)
	}
	if !strings.Contains(termcolor.StripSGR(strings.Join(m2.panelLines(), "\n")), "(10–20 of 20 lines)") {
		t.Errorf("bottom counter wrong:\n%s", strings.Join(plainLines(m2), "\n"))
	}
}

// ---------------------------------------------------------------- /diff

func TestCycle207ChangelogTitle(t *testing.T) {
	excerpt := changelog.ExcerptRows()
	want := "  \x1b[1m\x1b[38;5;228m\x1b[48;5;63m Pigasus CLI Changelog \x1b[0m"
	if excerpt[0] != want {
		t.Errorf("title = %q, want %q", excerpt[0], want)
	}
	if excerpt[1] != "" {
		t.Errorf("excerpt[1] = %q, want a blank row", excerpt[1])
	}
	if !strings.Contains(termcolor.StripSGR(excerpt[2]), "The terminal-first surface") {
		t.Errorf("excerpt[2] = %q", excerpt[2])
	}
}

// A sized /changelog window is height-4 lines of the document.
func TestCycle207ChangelogHeight(t *testing.T) {
	m := mkPanel(panelregistry.Changelog, "/changelog", 0)
	m.viewport.height = 24
	spec, ok := m.getPanelRegistry().ViewerSpec(m.overlays.panel.kind)
	if !ok {
		t.Fatal("changelog is not a viewer")
	}
	if spec.Visible != 20 {
		t.Errorf("visible = %d, want 20 (h-4)", spec.Visible)
	}
	if spec.Total != 2014 {
		t.Errorf("total = %d, want 2014", spec.Total)
	}
	if !strings.Contains(termcolor.StripSGR(strings.Join(m.panelLines(), "\n")), "(1–20 of 2014 lines)") {
		t.Errorf("counter wrong:\n%s", strings.Join(plainLines(m), "\n"))
	}
}

// ------------------------------------------------ /rewind, /credits pin

// /rewind already matched REF 1.4.0 byte for byte; this pins the exact
// rows so a later edit cannot silently regress them.
//
// (The /credits half of this pin moved to
// internal/ui/views/creditspanel in cycle 381.)
func TestCycle207RewindRegression(t *testing.T) {
	rewind := mkPanel(panelregistry.Rewind, "/rewind", 0).panelLines()
	assertLines(t, rewind, []string{
		"\x1b[1m\x1b[39mRewind Conversation\x1b[0m",
		"",
		cBlueOpen + "> (current)" + cBlueClose,
		"",
		tuilib.PanelKeyboardHint("↑/↓", "Navigate", "enter", "Select", "esc", "Cancel"),
	})

}

func TestStatusLineTaskCount(t *testing.T) {
	m := seedRunMode()

	// Zero tasks → no segment.
	if got := m.tasksStatusSuffix(); got != "" {
		t.Errorf("zero-task suffix = %q, want empty", got)
	}

	// Two running tasks → " · 2 tasks".
	seedTask(m.engine.taskReg, "t1", "bash -c sleep 60", taskregistry.KindShell, taskregistry.StateRunning)
	seedTask(m.engine.taskReg, "t2", "bash -c sleep 90", taskregistry.KindShell, taskregistry.StateRunning)
	got := m.tasksStatusSuffix()
	if got != " · 2 tasks" {
		t.Errorf("two-task suffix = %q, want %q", got, " · 2 tasks")
	}

	// One running + one done → still " · 1 task".
	tt := m.engine.taskReg.Get("t2")
	m.engine.taskReg.Update(tt.ID, func(t *taskregistry.Task) {
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
		if m.engine.tasksSubCancel != nil {
			m.engine.tasksSubCancel()
		}
	}()

	// Initial state: empty suffix.
	if got := m.tasksStatusSuffix(); got != "" {
		t.Fatalf("initial suffix = %q, want empty", got)
	}

	// Register a task; the subscriber goroutine should set
	// tasksDirty within a few ms (the channel is buffered, so
	// the send completes immediately).
	seedTask(m.engine.taskReg, "live-1", "bash -c sleep 5", taskregistry.KindShell, taskregistry.StateRunning)

	// Wait up to 100 ms for the dirty flag. The field is
	// int32 (atomic) — go vet's atomic checker would otherwise
	// flag this read.
	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&m.engine.tasksDirty) == 1 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if atomic.LoadInt32(&m.engine.tasksDirty) != 1 {
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
	if got := tuilib.TasksFeedback(0, 0); got != "No background tasks." {
		t.Errorf("tasksFeedback(0,0) = %q, want %q", got, "No background tasks.")
	}
	if got := tuilib.TasksFeedback(2, 5); got != "2 running, 5 done" {
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
	first := m.engine.tasksSubCancel
	m.startTasksSubscription()
	second := m.engine.tasksSubCancel
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

// The effort slider and its `←/→ Effort` legend entry exist only while the
// cursor is on the three-tier Gemini row.
func TestCycle208_ModelSliderOnlyOnTieredRow(t *testing.T) {
	cycle196TestHome(t, map[string]any{"model": "gemini-3.8-flash-high"})
	m := mkPanel(panelregistry.Model, "/model", 0)

	onGemini := strings.Join(plainLines(m), "\n")
	if !strings.Contains(onGemini, "Effort") || !strings.Contains(onGemini, "←/→ Effort") {
		t.Fatalf("Gemini row should show the slider + legend:\n%s", onGemini)
	}
	if !strings.Contains(onGemini, "←/→ Effort  enter Select  esc Go Back") {
		t.Errorf("Gemini legend wording changed:\n%s", onGemini)
	}

	m.modelPanel().SetCursor(1) // Claude Sonnet — fixed effort
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
	m := mkPanel(panelregistry.Model, "/model", 1) // Claude row, not current
	m.viewport.height = 0
	lines := m.panelLines()
	got := lines[5+1]
	want := cBlueOpen + "> " + modelpicker.Rows[1].Display + cBlueClose
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
	m := mkPanel(panelregistry.Model, "/model", 0) // Gemini row — has the slider
	m.viewport.height = 0
	if m.modelPanel().EffortCursor() != effortpanel.EffortHigh {
		t.Fatalf("open cursor = %d, want high", m.modelPanel().EffortCursor())
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Fatal("down not handled")
	}
	if m.modelPanel().EffortCursor() != effortpanel.EffortHigh {
		t.Errorf("after down: effort cursor = %d, want high", m.modelPanel().EffortCursor())
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
		if strings.Contains(ln, effortpanel.PanelEffHighOpen) {
			slider = i
			break
		}
	}
	if slider < 0 {
		t.Fatalf("the effort slider row is missing: %q", lines)
	}
	if !strings.Contains(lines[slider], effortpanel.PanelEffHighOpen) {
		t.Errorf("slider did not return to high: %q", lines[slider])
	}
	if !strings.Contains(lines[5], " (current)") {
		t.Errorf("MiniMax row lost its (current) marker: %q", lines[5])
	}
}

// --- /effort ---------------------------------------------------------------

// The status row's suffix follows the live effort, not a constant.
func TestCycle208_StatusEffortSuffixTracksEffort(t *testing.T) {
	m := &runMode{}
	if got := m.statusEffortSuffix(); got != " \u00b7 high" {
		t.Errorf("default suffix = %q", got)
	}
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.String("effort", "", "")
	m.session.flags = fs
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

	m := mkPanel(panelregistry.Effort, "/effort", effortpanel.EffortHigh)
	m.session.flags = fs
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("enter not handled")
	}
	if m.overlays.panel.kind != panelregistry.None {
		t.Fatalf("panel not closed: kind=%v", m.overlays.panel.kind)
	}
	if len(m.engine.response) != 0 {
		t.Errorf("no-op apply must stay silent, got %q", m.engine.response)
	}
	if s := readCycle196Settings(t, home); s["model"] != "MiniMax-M3" {
		t.Errorf("no-op apply wrote settings: model=%v", s["model"])
	}

	// Moving to Medium is a real change.
	m2 := mkPanel(panelregistry.Effort, "/effort", effortpanel.EffortMedium)
	m2.session.flags = fs
	if handled, _ := m2.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("enter not handled")
	}
	if m2.overlays.panel.kind != panelregistry.None {
		t.Fatalf("panel not closed: kind=%v", m2.overlays.panel.kind)
	}
	if got := termcolor.StripSGR(strings.Join(m2.engine.response, "\n")); !strings.Contains(got, "Effort set to medium") {
		t.Errorf("response = %q", got)
	}
	if s := readCycle196Settings(t, home); s["model"] != "Gemini 3.8 Flash (Medium)" {
		t.Errorf("persisted model = %v", s["model"])
	}
	if m2.session.model != "Gemini 3.8 Flash (Medium)" {
		t.Errorf("banner model = %q", m2.session.model)
	}
	if m2.session.statusModel != "Gemini 3.8 Flash" {
		t.Errorf("status model = %q", m2.session.statusModel)
	}
	if got := m2.statusEffortSuffix(); got != " \u00b7 medium" {
		t.Errorf("status suffix = %q", got)
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
	// The feed's newest section is 1.4.11.
	if src[0] != "1.4.11:" {
		t.Errorf("first source line = %q, want %q", src[0], "1.4.11:")
	}
	if !strings.Contains(strings.Join(src, "\n"), "1.4.1:") {
		t.Error("source missing the 1.4.1 header (binary capture block)")
	}
	if !strings.Contains(strings.Join(src, "\n"), "1.0.0:") {
		t.Error("source missing the 1.0.0 header")
	}
}

// TestConfigPanel_OverrideIndicator tests B-8: setting overrides indicated in /config.
func TestConfigPanel_OverrideIndicator(t *testing.T) {
	cycle196TestHome(t, map[string]any{
		"enableTerminalSandbox": false,
		"toolPermission":        "request-review",
	})
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.Bool("sandbox", true, "")
	fs.Bool("dangerously-skip-permissions", true, "")
	fs.String("mode", "", "")
	_ = fs.Parse([]string{"--sandbox", "--dangerously-skip-permissions", "--mode=auto"})

	m := &runMode{session: sessionState{flags: fs}}
	p := m.newConfigPanel()

	sbIdx := configRowIndexForTest(t, m, "Sandbox Mode")
	p.SetCursor(sbIdx)
	val := p.SettingValue(sbIdx)
	if !strings.Contains(val, "off (overridden by --sandbox)") {
		t.Errorf("Sandbox Mode value = %q, want override indicator", val)
	}

	tpIdx := configRowIndexForTest(t, m, "Tool Permission")
	p.SetCursor(tpIdx)
	tpVal := p.SettingValue(tpIdx)
	if !strings.Contains(tpVal, "(overridden by --dangerously-skip-permissions)") {
		t.Errorf("Tool Permission value = %q, want override indicator", tpVal)
	}

	modeIdx := configRowIndexForTest(t, m, "Agent Mode")
	p.SetCursor(modeIdx)
	modeVal := p.SettingValue(modeIdx)
	if !strings.Contains(modeVal, "(overridden by --mode)") {
		t.Errorf("Agent Mode value = %q, want override indicator", modeVal)
	}
}

func TestChangelogPagination_RendersDoc(t *testing.T) {
	if !changelog.Loaded() {
		t.Skipf("changelog_reference.txt not reachable from cwd %q — skipping", mustGetwd(t))
	}
	m := mkPanel(panelregistry.Changelog, "/changelog", 0)
	text := panelText(m)
	if !strings.Contains(text, "Added a billing model choice after you sign in with Google Cloud") {
		t.Errorf("viewer missing the opening bullet:\n%s", truncForErr(text, 400))
	}
	if !strings.Contains(text, "## 1.4.11") {
		t.Errorf("viewer missing the ## 1.4.11 header row:\n%s", truncForErr(text, 400))
	}
	if !strings.Contains(strings.Join(changelog.Document(m.viewport.width, m.viewport.height), "\n"), "## 1.4.3") {
		t.Error("document missing the ## 1.4.3 header row")
	}
	doc := strings.Join(changelog.Document(m.viewport.width, m.viewport.height), "\n")
	if !strings.Contains(doc, "Initial release of the Antigravity CLI.") {
		t.Errorf("document missing the 1.0.0 closing line:\n%s", doc)
	}
}

func TestChangelogPagination_PageDown(t *testing.T) {
	if !changelog.Loaded() {
		t.Skipf("changelog_reference.txt not reachable from cwd %q — skipping", mustGetwd(t))
	}
	m := mkPanel(panelregistry.Changelog, "/changelog", 0)
	vis := changelog.WindowRows()
	if got, want := m.overlays.panel.window, 0; got != want {
		t.Fatalf("initial window = %d, want %d", got, want)
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Fatal("down not handled")
	}
	if got, want := m.overlays.panel.window, 1; got != want {
		t.Fatalf("after ↓ window = %d, want %d", got, want)
	}
	m.overlays.panel.window = 0
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyPgDown}); !handled {
		t.Fatal("pgdown not handled")
	}
	page := vis - 1
	if page < 1 {
		page = 1
	}
	if got, want := m.overlays.panel.window, page; got != want {
		t.Fatalf("after pgdown window = %d, want %d", got, want)
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyPgUp}); !handled {
		t.Fatal("pgup not handled")
	}
	if got, want := m.overlays.panel.window, 0; got != want {
		t.Fatalf("after pgup window = %d, want %d", got, want)
	}
}

func TestChangelogPagination_CtrlEnd(t *testing.T) {
	if !changelog.Loaded() {
		t.Skipf("changelog_reference.txt not reachable from cwd %q — skipping", mustGetwd(t))
	}
	m := mkPanel(panelregistry.Changelog, "/changelog", 0)
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnd, Mod: tea.ModCtrl}); !handled {
		t.Fatal("ctrl+end not handled")
	}
	total := len(changelog.Document(m.viewport.width, m.viewport.height))
	vis := changelog.WindowRows()
	if want := total - vis; m.overlays.panel.window != want {
		t.Fatalf("ctrl+end window = %d, want %d", m.overlays.panel.window, want)
	}
	text := panelText(m)
	if !strings.Contains(text, "Initial release of the Antigravity CLI.") {
		t.Errorf("bottom of changelog should show the 1.0.0 release:\n%s", text)
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Fatal("down at bottom not handled")
	}
	if want := total - vis; m.overlays.panel.window != want {
		t.Errorf("down at bottom moved window to %d, want %d", m.overlays.panel.window, want)
	}
}

func TestChangelogPagination_CtrlHome(t *testing.T) {
	if !changelog.Loaded() {
		t.Skipf("changelog_reference.txt not reachable from cwd %q — skipping", mustGetwd(t))
	}
	m := mkPanel(panelregistry.Changelog, "/changelog", 0)
	m.overlays.panel.window = 50
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyHome, Mod: tea.ModCtrl}); !handled {
		t.Fatal("ctrl+home not handled")
	}
	if m.overlays.panel.window != 0 {
		t.Fatalf("ctrl+home window = %d, want 0", m.overlays.panel.window)
	}
	text := panelText(m)
	if !strings.Contains(text, "## 1.4.11") {
		t.Errorf("first screen should open on 1.4.11:\n%s", truncForErr(text, 400))
	}
	if !strings.Contains(strings.Join(changelog.Document(m.viewport.width, m.viewport.height), "\n"), "## 1.4.3") {
		t.Error("document missing the ## 1.4.3 header row")
	}
}

func TestChangelogPagination_Counter(t *testing.T) {
	if !changelog.Loaded() {
		t.Skipf("changelog_reference.txt not reachable from cwd %q — skipping", mustGetwd(t))
	}
	m := mkPanel(panelregistry.Changelog, "/changelog", 0)
	vis := changelog.WindowRows()
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
	if got := tuiviewer.MaxOffset(tuiviewer.Spec{Lines: nil, Visible: 5, Total: 5}); got != 0 {
		t.Errorf("MaxOffset(empty) = %d, want 0", got)
	}
	if got := tuiviewer.MaxOffset(tuiviewer.Spec{Lines: []string{"a", "b", "c"}, Visible: 12, Total: 3}); got != 0 {
		t.Errorf("MaxOffset(short) = %d, want 0", got)
	}
	if got := tuiviewer.MaxOffset(tuiviewer.Spec{Lines: []string{"a", "b", "c", "d", "e"}, Visible: 2, Total: 5}); got != 3 {
		t.Errorf("MaxOffset(2-of-5) = %d, want 3", got)
	}
	m := &runMode{}
	m.overlays.panel.kind = panelregistry.Changelog
	tiny := tuiviewer.Spec{Lines: []string{"a", "b", "c"}, Visible: 12, Total: 3}
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

// ---------------------------------------------------------------- /provider panel (host side)

// The /provider panel itself — its rows, its edit form, its keys, its
// writes and its login dialog — lives in internal/ui/views/providerpanel
// and is tested there. The host only routes the command to the panel and
// answers the panel's four seams (the browser, the clipboard, the current
// model and the way out), so only the routing is tested here.

func TestProviderPanel_IsInPickerCatalogue(t *testing.T) {
	found := false
	for _, c := range slashcommands.Default {
		if c.Name == "/provider" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("/provider must be in the picker's catalogue")
	}
}

func TestProviderPanel_IsRoutedToThePanel(t *testing.T) {
	kind, ok := slashcommands.PanelFor("provider")
	if !ok || kind != panelregistry.Provider {
		t.Fatalf("slashcommands.PanelFor(provider) = %v, %v", kind, ok)
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
		m.engine.session = append(m.engine.session, msg)
	}
}

// sessionContextTokens mirrors sessionTokenCounts so each test can pin the
// precise numbers the panel will see without reaching into its rendering.
func sessionContextTokens(m *runMode) (user, agent, tool int) {
	for _, msg := range m.engine.session {
		if msg.Role == "user" {
			user += backend.UserContentTokenCount(msg.WireText())
		}
	}
	agent = int(m.session.store.SessionCompletionTokens - m.session.store.LastTurnCompletionTokens)
	if agent < 0 {
		agent = 0
	}
	for _, b := range m.engine.blocks {
		for _, st := range b.Steps {
			if st.Kind == toolsteps.KindTool {
				tool += backend.ToolResultTokenCount(st.Result)
			}
		}
	}
	for _, st := range m.engine.liveSteps {
		if st.Kind == toolsteps.KindTool {
			tool += backend.ToolResultTokenCount(st.Result)
		}
	}
	return user, agent, tool
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
	m := mkPanel(panelregistry.Context, "/context", 0)
	wire := backend.WrapUserContent(strings.Repeat("a", 200), "Gemini 3.8 Flash (High)", false,
		time.Date(2026, 10, 21, 7, 0, 0, 0, contextTestZone))
	m.engine.session = []backend.Message{
		{Role: "user", Content: strings.Repeat("a", 200), WireContent: wire},
		{Role: "assistant", Content: strings.Repeat("b", 200)},
	}
	wantUser := len(wire)/3 + 6
	u, a, tool := m.sessionTokenCounts()
	if u != wantUser || a != 0 || tool != 0 {
		t.Errorf("session-only: got (%d, %d, %d), want (%d, 0, 0)", u, a, tool, wantUser)
	}

	// Usage counters decide the agent row: the last step is excluded.
	m.session.store.SessionCompletionTokens = 100
	m.session.store.LastTurnCompletionTokens = 40
	_, a, _ = m.sessionTokenCounts()
	if a != 60 {
		t.Errorf("agent row = %d, want 60 (100 completed - 40 in flight)", a)
	}

	// One tool step in a historical block, one live: results are what count.
	m.engine.blocks = []transcript.Block{
		{Steps: []toolsteps.Step{
			{Kind: toolsteps.KindThought, Tokens: 50},
			{Kind: toolsteps.KindTool, Name: "fetch", Result: strings.Repeat("x", 109)},
		}},
	}
	m.engine.liveSteps = []toolsteps.Step{{Kind: toolsteps.KindTool, Name: "shell", Result: strings.Repeat("y", 158)}}
	_, _, tool = m.sessionTokenCounts()
	if tool != 42+59 {
		t.Errorf("tool row = %d, want 101 (42 + 59)", tool)
	}

	// A "system" role message is ignored.
	m.engine.session = append(m.engine.session,
		backend.Message{Role: "system", Content: strings.Repeat("z", 400)},
	)
	u2, a2, tool2 := m.sessionTokenCounts()
	if u2 != wantUser || a2 != 60 || tool2 != 101 {
		t.Errorf("after system msg: got (%d, %d, %d), want (%d, 60, 101)", u2, a2, tool2, wantUser)
	}

	// A running step has no result yet, so it contributes nothing.
	m.engine.liveSteps = append(m.engine.liveSteps, toolsteps.Step{Kind: toolsteps.KindTool, Name: "running", Running: true})
	_, _, tool3 := m.sessionTokenCounts()
	if tool3 != 101 {
		t.Errorf("running step counted: tool = %d, want 101", tool3)
	}
}

// TestContextStateAgentTokensComeFromUsage — cycle 292: the agent row
// counts the provider's completion tokens, not the length of the
// assistant text. A 20-message session with 15875 completion tokens
// gathers 15875 agent tokens.
func TestContextStateAgentTokensComeFromUsage(t *testing.T) {
	m := mkPanel(panelregistry.Context, "/context", 0)
	m.session.model = "Gemini 3.8 Flash (High)"
	payloads := make([]string, 0, 20)
	for i := 0; i < 10; i++ {
		payloads = append(payloads, strings.Repeat("u", 4000+i*200))
		payloads = append(payloads, strings.Repeat("a", 5000+i*300))
	}
	seedContextSession(m, payloads)
	m.session.store.SessionCompletionTokens = 15875

	st := m.contextState()
	if st.Agent != 15875 {
		t.Errorf("agent = %d, want 15875 (the provider's completion tokens)", st.Agent)
	}
	if st.User == 0 {
		t.Error("user tokens = 0, want the wire envelope's count")
	}
}

// TestContextStateEmpty — an empty session gathers the empty layout's
// facts: no counts, and the reference's own model default.
func TestContextStateEmpty(t *testing.T) {
	m := mkPanel(panelregistry.Context, "/context", 0)
	st := m.contextState()
	if st.Mode != contextpanel.ModeEmpty {
		t.Errorf("mode = %v, want ModeEmpty", st.Mode)
	}
	if st.Model != modelcatalog.DefaultModel {
		t.Errorf("model = %q, want the catalogue's default %q", st.Model, modelcatalog.DefaultModel)
	}
	if st.User != 0 || st.Agent != 0 || st.Tool != 0 {
		t.Errorf("counts = (%d, %d, %d), want all zero", st.User, st.Agent, st.Tool)
	}
}

// TestContextStateLive — a session with messages gathers the
// completed-turn layout's facts.
func TestContextStateLive(t *testing.T) {
	m := mkPanel(panelregistry.Context, "/context", 0)
	m.session.model = "Gemini 3.8 Flash (High)"
	seedContextSession(m, []string{
		strings.Repeat("a", 2000),
		strings.Repeat("b", 1500),
		strings.Repeat("c", 800),
		strings.Repeat("d", 400),
		strings.Repeat("e", 1200),
	})
	m.session.store.SessionCompletionTokens = 475

	st := m.contextState()
	if st.Mode != contextpanel.ModeLive {
		t.Fatalf("mode = %v, want ModeLive", st.Mode)
	}
	if st.Model != "Gemini 3.8 Flash (High)" {
		t.Errorf("model = %q", st.Model)
	}
	u, a, tool := sessionContextTokens(m)
	if st.User != u {
		t.Errorf("user = %d, want %d", st.User, u)
	}
	if st.Agent != a {
		t.Errorf("agent = %d, want %d", st.Agent, a)
	}
	if st.Tool != tool {
		t.Errorf("tool = %d, want %d", st.Tool, tool)
	}
}

// TestContextStateStreaming — a turn in flight gathers the in-flight
// layout's facts: the user row counts the in-flight message and the
// agent row counts the reply buffer.
func TestContextStateStreaming(t *testing.T) {
	m := mkPanel(panelregistry.Context, "/context", 0)
	m.session.model = "Gemini 3.8 Flash (High)"
	m.engine.streaming = true
	m.engine.session = append(m.engine.session, backend.Message{Role: "user", Content: "slow sea story please"})
	m.engine.liveReplyBuf = "The lighthouse stood at the edge of the harbor."

	st := m.contextState()
	if st.Mode != contextpanel.ModeStreaming {
		t.Fatalf("mode = %v, want ModeStreaming", st.Mode)
	}
	wantUser := len("slow sea story please") / 4
	if st.User != wantUser {
		t.Errorf("user = %d, want %d", st.User, wantUser)
	}
	wantAgent := len(m.engine.liveReplyBuf) / 4
	if st.Agent != wantAgent {
		t.Errorf("agent = %d, want %d", st.Agent, wantAgent)
	}
}

// TestContextStateToolCalls — the tool row counts the tool results of
// the historical blocks and of the live steps.
func TestContextStateToolCalls(t *testing.T) {
	m := mkPanel(panelregistry.Context, "/context", 0)
	m.session.model = "Gemini 3.8 Flash (High)"
	seedContextSession(m, []string{strings.Repeat("a", 4000)})
	res := strings.Repeat("r", 109) // (109+1)/3+6 = 42 tokens per call
	m.engine.blocks = []transcript.Block{
		{Steps: []toolsteps.Step{{Kind: toolsteps.KindTool, Name: "read_file", Result: res}}},
		{Steps: []toolsteps.Step{{Kind: toolsteps.KindThought, Tokens: 42}, {Kind: toolsteps.KindTool, Name: "glob", Result: res}}},
		{Steps: []toolsteps.Step{{Kind: toolsteps.KindTool, Name: "grep", Result: res}}},
	}
	m.engine.liveSteps = []toolsteps.Step{{Kind: toolsteps.KindTool, Name: "shell", Result: res}}

	st := m.contextState()
	if st.Mode != contextpanel.ModeLive {
		t.Fatalf("mode = %v, want ModeLive", st.Mode)
	}
	if st.Tool != 4*42 {
		t.Errorf("tool = %d, want 168", st.Tool)
	}
}

func TestConfigPanel_IsRoutedToThePanel(t *testing.T) {
	kind, ok := slashcommands.PanelFor("config")
	if !ok || kind != panelregistry.Config {
		t.Fatalf("slashcommands.PanelFor(config) = %v, %v", kind, ok)
	}
}

// TestConfigPanel_OpensWithThePanelBound pins the host's wiring: opening
// /config builds the panel (with the host's seams) and the panel's own
// rows reach the screen.
func TestConfigPanel_OpensWithThePanelBound(t *testing.T) {
	cycle196TestHome(t, map[string]any{})
	m := &runMode{}
	if !m.openSlashPanel("config") {
		t.Fatal("openSlashPanel(config) = false, want true")
	}
	if m.overlays.panel.cfg == nil {
		t.Fatal("the /config panel was not built on open")
	}
	plain := plainLines(m)
	if len(plain) == 0 || plain[0] != "Settings" {
		t.Fatalf("the panel's first line = %q, want %q", plain, "Settings")
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
//   - m.overlays.panel.kind        stays panelregistry.Config
//   - the skills panel's  stays closed (skills sub-view; different
//     detail                panel, defensive)
//   - the /diff panel is never built (another panel's sub-view;
//     defensive)
//   - m.overlays.panel.cursor      unchanged after Left/Right (Up/Down are
//     the cursor-bearing keys here)
//   - the panel renderer's line count is unchanged
//
// The line-count check is the most direct "no overlay appeared"
// signal: every panel sub-view in this codebase adds at least one
// row.
func TestConfigLeftRight_NoDropdownOpen(t *testing.T) {
	cycle196TestHome(t, map[string]any{})

	m := mkPanel(panelregistry.Config, "/config", 0)
	m.overlays.panel.cfg.SetCursor(configRowIndexForTest(t, m, "Verbosity"))
	preLines := plainLines(m)

	// Snapshot the dropdown-opening state before the press.
	preKind := m.overlays.panel.kind
	preDetail := m.skillsPanel().Detail()
	preDiffPanel := m.overlays.panel.df
	preCursor := m.overlays.panel.cfg.Cursor()

	// Right → advances verbosity to "low"; must NOT mutate any of
	// the panel overlay state.
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyRight}); !handled {
		t.Fatal("Right reported unhandled on /config")
	}

	for name, pair := range map[string]struct {
		pre, got any
	}{
		"kind":      {preKind, m.overlays.panel.kind},
		"detail":    {preDetail, m.skillsPanel().Detail()},
		"diffPanel": {preDiffPanel, m.overlays.panel.df},
		"cursor":    {preCursor, m.overlays.panel.cfg.Cursor()},
	} {
		if pair.pre != pair.got {
			t.Errorf("%s changed: pre=%v got=%v", name, pair.pre, pair.got)
		}
	}
	if m.overlays.panel.cfg.SubmenuOpen() {
		t.Error("Right opened the option submenu")
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
	if m.overlays.panel.kind != panelregistry.Config {
		t.Errorf("Left mutated panel.kind = %v, want panelregistry.Config", m.overlays.panel.kind)
	}
	post2 := plainLines(m)
	if len(preLines) != len(post2) {
		t.Errorf("panel line count changed after Left: pre=%d post=%d", len(preLines), len(post2))
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

// configRowIndexForTest resolves a /config row's index in the panel's
// own catalogue by its display name, so the tests do not depend on the
// order of the twenty settings rows.
//
// The index is the catalogue index the panel's own SettingValue reads, so
// a caller can hand it straight to the panel.
func configRowIndexForTest(t *testing.T, m *runMode, name string) int {
	t.Helper()
	for i, r := range m.configPanel().Catalogue() {
		if r.Name == name {
			return i
		}
	}
	t.Fatalf("the settings catalogue has no row named %q", name)
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

// TestCycle196_ConfigCycle_KeyHandler verifies that handlePanelKey
// routes ←/→ to the /config panel's own cycle.
func TestCycle196_ConfigCycle_KeyHandler(t *testing.T) {
	origScheme, _ := palette.CurrentScheme()
	t.Cleanup(func() {
		palette.SetScheme(origScheme)
		mdrender.SetColorScheme(origScheme)
	})
	dir := cycle196TestHome(t, map[string]any{
		"colorScheme": "light",
	})
	m := &runMode{}
	m.overlays.panel = slashPanel{kind: panelregistry.Config, cmd: "/config"}
	m.overlays.panel.cfg = m.newConfigPanel()
	m.overlays.panel.cfg.SetCursor(configRowIndexForTest(t, m, "Color Scheme"))

	handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyRight})
	if !handled {
		t.Fatal("←/→: handlePanelKey reported unhandled")
	}
	if got := cycle196ValueAt(t, readCycle196Settings(t, dir), "colorScheme"); got != "solarized light" {
		t.Errorf("after ←/→ key: colorScheme = %q, want %q", got, "solarized light")
	}
}

// TestConfigSearchKeyHandling pins the host's half of the search box:
// the keys reach the panel, and the second Esc leaves the panel.
func TestConfigSearchKeyHandling(t *testing.T) {
	m := &runMode{
		overlays: overlayState{
			panel: slashPanel{
				kind: panelregistry.Config,
				cmd:  "/settings",
			},
		},
		viewport: uiState{
			width:  120,
			height: 40,
		},
	}

	// Type 'c'
	m.handlePanelKey(tea.Key{Code: 'c', Text: "c"})
	if got := m.overlays.panel.cfg.Query(); got != "c" {
		t.Fatalf("expected the query 'c', got %q", got)
	}

	// Type 'o'
	m.handlePanelKey(tea.Key{Code: 'o', Text: "o"})
	if got := m.overlays.panel.cfg.Query(); got != "co" {
		t.Fatalf("expected the query 'co', got %q", got)
	}

	// Check the filtered view
	joined := strings.Join(m.panelLines(), "\n")
	if !strings.Contains(joined, "Search: co") {
		t.Fatalf("expected a search line with 'co', got:\n%s", joined)
	}

	// Esc once: clears the search query, the panel stays open.
	m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
	if got := m.overlays.panel.cfg.Query(); got != "" {
		t.Fatalf("expected an empty query after the first Esc, got %q", got)
	}
	if m.overlays.panel.kind != panelregistry.Config {
		t.Fatalf("expected the panel to remain open after clearing the search, got %v", m.overlays.panel.kind)
	}

	// Esc second time: closes the panel.
	m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
	if m.overlays.panel.kind != panelregistry.None {
		t.Fatalf("expected the panel to close after the second Esc, got %v", m.overlays.panel.kind)
	}
}

// TestColorSchemeLiveThemeUpdate pins the host's seam: the panel asks
// the host to repaint, and the host's live theme follows.
func TestColorSchemeLiveThemeUpdate(t *testing.T) {
	origScheme, _ := palette.CurrentScheme()
	cycle196TestHome(t, map[string]any{"colorScheme": "terminal"})
	t.Cleanup(func() {
		palette.SetScheme(origScheme)
		mdrender.SetColorScheme(origScheme)
	})

	m := newRunMode(nil, "test@example.com", "gemini-test", "Pro", "/tmp", true)
	m.viewport.width = 120
	m.viewport.height = 40
	m.overlays.panel = slashPanel{kind: panelregistry.Config, cmd: "/settings"}
	m.overlays.panel.cfg = m.newConfigPanel()
	m.overlays.panel.cfg.SetCursor(configRowIndexForTest(t, m, "Color Scheme"))

	// 1. Open the submenu.
	m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if !m.overlays.panel.cfg.SubmenuOpen() {
		t.Fatal("expected the submenu to be open")
	}

	// 2. Move down to "light" (index 1).
	m.handlePanelKey(tea.Key{Code: tea.KeyDown})
	if mdrender.CurrentColorScheme() != "light" {
		t.Fatalf("expected the mdrender live theme 'light', got %q", mdrender.CurrentColorScheme())
	}

	// 3. Move down to "solarized light" (index 2).
	m.handlePanelKey(tea.Key{Code: tea.KeyDown})
	if mdrender.CurrentColorScheme() != "solarized light" {
		t.Fatalf("expected the mdrender live theme 'solarized light', got %q", mdrender.CurrentColorScheme())
	}

	// 4. Cancel with Esc: the original theme comes back.
	m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
	if m.overlays.panel.cfg.SubmenuOpen() {
		t.Fatal("expected the submenu to be closed")
	}
	if mdrender.CurrentColorScheme() != "terminal" {
		t.Fatalf("expected the mdrender live theme reverted to 'terminal', got %q", mdrender.CurrentColorScheme())
	}

	// 5. ←/→ on the main list cycles live and persists.
	handled, cmd := m.handlePanelKey(tea.Key{Code: tea.KeyRight})
	if !handled || cmd == nil {
		t.Fatalf("expected KeyRight to return handled=true and a clear-screen command")
	}
	if mdrender.CurrentColorScheme() != "light" {
		t.Fatalf("expected the mdrender live theme 'light' after →, got %q", mdrender.CurrentColorScheme())
	}
	saved, _ := settings.Field("colorScheme")
	if saved != "light" {
		t.Fatalf("expected colorScheme 'light' persisted, got %v", saved)
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
		if !auth.IsBusinessAccount() {
			t.Error("IsBusinessAccount() = false under AGY_ADC_AUTH=true; want true")
		}
	})
	t.Run("AGY_ADC_AUTH unset: helper returns false", func(t *testing.T) {
		t.Setenv("AGY_ADC_AUTH", "")
		if auth.IsBusinessAccount() {
			t.Error("IsBusinessAccount() = true under AGY_ADC_AUTH unset; want false")
		}
	})
}

// TestCycle241_TelemetryKeyHandlerRoutesToGate drives the user-facing
// path: ←/→ on the locked row reaches the panel's cycle through
// handlePanelKey and surfaces the refusal message in the transcript.
func TestCycle241_TelemetryKeyHandlerRoutesToGate(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "true")
	cycle196TestHome(t, map[string]any{})
	m := mkPanel(panelregistry.Config, "/config", 0)
	idx := configRowIndexForTest(t, m, "Enable Telemetry")
	m.overlays.panel.cfg.SetCursor(idx)
	handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyRight})
	if !handled {
		t.Fatal("←/→ on /config: handlePanelKey reported unhandled")
	}
	if got := m.overlays.panel.cfg.SettingValue(idx); got != auth.TelemetryDisabledValue {
		t.Errorf("after handlePanelKey →: row = %q, want %q",
			got, auth.TelemetryDisabledValue)
	}
	found := false
	for _, line := range m.engine.response {
		if strings.Contains(termcolor.StripSGR(line), auth.TelemetryDisabledResponse) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("after handlePanelKey →: transcript missing %q.\nresponse=%q",
			auth.TelemetryDisabledResponse, m.engine.response)
	}
}
