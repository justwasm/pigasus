// slash_helpers.go — cycle 167 helpers that close the cycle-77 stub
// bodies. Each function does the smallest useful thing for the
// command that invokes it; the references for the recovered messages
// are inline so future cycles know where to tighten them.

package cliapp

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/convstore"
	"github.com/justwasm/pigasus/internal/isolation"
	"github.com/justwasm/pigasus/internal/printmode"
	"github.com/justwasm/pigasus/internal/rewindpanel"
	"github.com/justwasm/pigasus/internal/skillsui"
)

// rewindSession pops the trailing user/assistant pair from sess and
// returns the number of messages removed. Delegated to rewindpanel.RewindSession.
func rewindSession(sess []backend.Message) int {
	return rewindpanel.RewindSession(sess)
}

// forkConversation duplicates the live session into a fresh
// convstore database under a new UUID. Delegated to convstore.ForkConversation.
func forkConversation(sess []backend.Message, currentID string) (string, string) {
	return convstore.ForkConversation(sess, currentID)
}

// forkTrajectory duplicates the conversation's trajectory — the user
// inputs, the model responses and the tool calls — into a fresh
// convstore database under a new UUID.
//
// The tool calls come along because they are what the rehydration pass
// reads back: a fork that dropped them would start blind (see
// internal/rehydration).
func forkTrajectory(steps []convstore.Step, currentID string) (string, string) {
	return convstore.ForkConversationSteps(steps, currentID)
}

// sessionToSteps converts an in-memory session into the convstore
// step shape that WriteConversation emits. Delegated to convstore.SessionToSteps.
func sessionToSteps(sess []backend.Message) []convstore.Step {
	return convstore.SessionToSteps(sess)
}

// printSlashKeybindings is the /keybindings panel content. The
// reference's /keybindings tab on the /help overlay lists the same
// shortcuts in a slightly different visual layout; we render the
// same content via a multi-line text block to keep parity without
// re-implementing the panel.
func printSlashKeybindings() string {
	var b strings.Builder
	b.WriteString("Keyboard shortcuts:\n")
	for _, line := range []string{
		"  Enter            Submit prompt",
		"  Shift+Enter      Newline (multiline prompt)",
		"  Ctrl+C × 2        Exit",
		"  Ctrl+L            Clear CLI screen",
		"  Ctrl+O            Expand / collapse tool calls",
		"  Ctrl+T            Edit prompt in $EDITOR",
		"  Up / Down         History navigation",
		"  PageUp / PageDown Transcript scroll",
		"  /                 Open command picker",
		"  Esc               Close picker / overlay",
		"  Tab               Complete command",
	} {
		b.WriteString(line)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

const keybindingsFileName = "keybindings.json"

func resolveKeybindingsEditor(configuredEditor string) (name string, args []string, source string, ok bool) {
	configuredEditor = trimSpace(configuredEditor)
	if configuredEditor != "" && !strings.EqualFold(configuredEditor, "auto") {
		name, args = splitCommand(configuredEditor)
		return name, args, "settings.editor", true
	}
	if e := trimSpace(os.Getenv("EDITOR")); e != "" {
		name, args = splitCommand(e)
		return name, args, "$EDITOR", true
	}
	if v := trimSpace(os.Getenv("VISUAL")); v != "" {
		name, args = splitCommand(v)
		return name, args, "$VISUAL", true
	}
	return "", nil, "", false
}

const noEditorConfiguredMessage = "No editor configured: set $EDITOR or $VISUAL"

func openKeybindingsFile(configuredEditor string) (notice string, cmd *exec.Cmd, errMsg string) {
	home := auth.AntigravityConfigHome()
	if err := os.MkdirAll(home, 0o755); err != nil {
		return "", nil, "Failed to create " + home + ": " + err.Error()
	}
	path := filepath.Join(home, keybindingsFileName)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
			return "", nil, "Failed to create " + path + ": " + err.Error()
		}
		notice = "Created " + path
	} else if err != nil {
		return "", nil, "Failed to stat " + path + ": " + err.Error()
	}
	name, args, _, ok := resolveKeybindingsEditor(configuredEditor)
	if !ok {
		return notice, nil, noEditorConfiguredMessage
	}
	if _, err := exec.LookPath(name); err != nil {
		return notice, nil, "Failed to resolve editor: " + err.Error()
	}
	return notice, exec.Command(name, append(args, path)...), ""
}

func (m *runMode) slashKeybindings(args string) (bool, tea.Cmd) {
	notice, editorCmd, errMsg := openKeybindingsFile(auth.SettingsString("editor"))
	if errMsg != "" {
		m.slashRespond("/keybindings", errMsg)
		m.slashContent(printSlashKeybindings())
		return true, nil
	}
	if notice != "" {
		m.slashRespond("/keybindings", notice)
	}
	if editorCmd == nil {
		return true, nil
	}
	return true, tea.ExecProcess(editorCmd, func(err error) tea.Msg {
		return editorFinishedMsg{err: err}
	})
}

// runIsolatedSlash wires the /branch and /share slash-command
// handlers to the isolation sandbox.
func runIsolatedSlash(m *runMode, cmd, args string, feedback isolation.FeedbackFn) {
	if m == nil || feedback == nil {
		return
	}
	parentCwd := m.cwd
	if parentCwd == "" {
		parentCwd = defaultWorkspaceForRunMode()
	}
	iw := isolation.New()
	iw.Capture(parentCwd, isolation.EnvSnapshot(os.Environ()))
	out := iw.Run(func(sandbox *isolation.Workspace) {
		sandbox.CaptureOutput(feedback(args, parentCwd)...)
		sandbox.RegisterSkill("branch-isolate:helper")
		sandbox.RegisterAgent("branch-isolate:scout")
	})
	if len(out) > 0 {
		m.slashRespond(cmd, out[0], out[1:]...)
	}
}

type jsonSkillInfo = printmode.SkillInfo

var printSlashSkillInfos = printmode.PrintSlashSkillInfos

// slashSkillCatalog is the source of the invocable-skill list. It is a var
// rather than a direct call so tests can substitute a synthetic catalog
// without depending on the host's ~/.gemini tree. Production always uses
// printSlashSkillInfos (print_slash.go) — the same enumerator /skills uses.
var slashSkillCatalog = printSlashSkillInfos

// slashSkillLookup resolves name against the skill catalog. It returns the
// winning skill and true, or a zero value and false. Names are compared
// verbatim (case-sensitive); when two skills share an alias the
// alphabetically-first (catalog order) wins.
func slashSkillLookup(name string) (jsonSkillInfo, bool) {
	if name == "" {
		return jsonSkillInfo{}, false
	}
	for _, s := range slashSkillCatalog() {
		for _, n := range printmode.SkillInvocationNames(s) {
			if n == name {
				return s, true
			}
		}
	}
	return jsonSkillInfo{}, false
}

// ════════════════════════════════════════════════════════════════════════
// cycle 203 — the /skills picker
// ════════════════════════════════════════════════════════════════════════
//
// panelSkillsLines (slash_panels.go) used to hardcode "63 skills", one
// group header, eleven invented rows and a fabricated description for
// every entry ("Skill installed under ~/.gemini/skills/<name>"). Cycle 203
// rebuilds the panel on the real catalogue — slashSkillCatalog, i.e.
// printSlashSkillInfos (print_slash.go), the same enumerator that already
// byte-matches `agy -p /skills` — plus the reference layout recovered from
// the caps/ref.h*.raw.txt probes (agy 1.4.0, 220 wide):
//
//   - bold "Skills" title; a "<N> skills" summary with no indent (N is the
//     catalogue size); a blank row; then the fixed "Create new skills"
//     block (Workspace = <logical cwd>, Global / Shared literal);
//   - one group per catalogue root — "Shared skills" (~/.gemini/skills),
//     the builtin bundle (absolute path · "Built-in"), then every plugin
//     group in plugin-name order (absolute path · "From <plugin dir>");
//   - a blank row before every group header except the first rendered;
//   - per-skill rows "  <name>: <desc>", the cursor row using "› " inside
//     the blue name span, the description truncated to width-1 columns
//     with an ASCII "..." (a builtin row reserves a trailing
//     " · built-in");
//   - a "  [a-b of N items]" counter (dim gray 241) only while the window
//     is shorter than the item list; N counts the group headers as items;
//   - the standard "Keyboard: ↑/↓ Navigate  enter View  esc Cancel" legend.
//
// Everything below is measured, not guessed:
//
//	visible items  n = height*3/4 - 11 (integer division), floor 1
//	                 (h=50→26, 24→7, 20→4, 30→11, 98→62, h≥99→63=all)
//	cursor         indexes skills only (headers are unselectable) and
//	                 ranges over -1 (no selection — the initial state) ..
//	                 len(skills)-1; ↓ from -1 → 0, and both ends clamp
//	                 (↑ on the first skill stays put, it does not clear)
//	window start   max(0, cursorItem-(n-1)); the counter follows it
//	truncation     whole rendered line ≤ width-1 columns, mid-word cut

// The picker's model now lives in internal/skillsui (panel_model.go).
// The aliases below keep the clone's own vocabulary — and its tests —
// reading the same fields the panel renderer writes.

// skillsPanelDefaultWidth / Height stand in for m.width / m.height before
// the first tea.WindowSizeMsg reaches the model (mkPanel leaves both 0).
// They reproduce the 220×30 class-A capture (ppref.skills.raw.txt:
// 11 items, "[1-11 of 63 items]"), mirroring configVisibleRowsFor's 10 and
// helpListWindowFor's 18 fallbacks.
const (
	skillsPanelDefaultWidth  = skillsui.PanelDefaultWidth
	skillsPanelDefaultHeight = skillsui.PanelDefaultHeight
)

// skillsPanelGroup is one /skills catalogue section: a header row followed
// by the group's skills, in display-name order.
type skillsPanelGroup = skillsui.PanelGroup

// skillsPanelItem is one row in the flat item list the counter counts. A
// header and a skill each occupy exactly one item; the blank row before a
// header is not an item.
type skillsPanelItem = skillsui.PanelItem

// skillsPanelModel is the whole picker state: the groups, the flat skill
// list the cursor indexes, the flat item list the counter counts, and the
// item index of each skill (for the window-follows-cursor rule).
type skillsPanelModel = skillsui.PanelModel

// newSkillsPanelModel builds the model from the live catalogue.
func newSkillsPanelModel() skillsPanelModel {
	return skillsui.NewPanelModel(slashSkillCatalog())
}

// skillsPanelGroups splits the catalogue (already sorted by display name)
// into the reference's groups: Shared, Built-in, then the plugin groups in
// plugin-name order. The rule itself lives in internal/skillsui.
func skillsPanelGroups() []skillsPanelGroup {
	return skillsui.PanelGroups(slashSkillCatalog())
}

// skillsPanelCatalog flattens the groups back into the cursor's index
// space: skill 0 is the first Shared skill, the last skill is the last
// plugin entry. Mirrors slashSkillCatalog's order, minus the headers.
func skillsPanelCatalog() []jsonSkillInfo {
	return skillsui.PanelCatalog(slashSkillCatalog())
}

// skillsPanelRow renders one skill row. The name is brand blue; the cursor
// row moves the "  " prefix inside the blue span as "› ". The description
// is dim gray 238 and is cut mid-word to keep the rendered line within
// width-1 columns, an ASCII "..." marking the cut. A built-in entry
// reserves a trailing " · built-in" (italic gray 241) before truncating.
// skillsPanelRow, skillsPanelMarketplaceLink, skillsPanelLines, moveSkillsCursor,
// and skillsPanelDetailLines are in slash_skills_panel_view.go
