// slash_helpers.go — cycle 167 helpers that close the cycle-77 stub
// bodies. Each function does the smallest useful thing for the
// command that invokes it; the references for the recovered messages
// are inline so future cycles know where to tighten them.

package cliapp

import (
	"github.com/justwasm/pigasus/internal/features/tools/skills"
	"github.com/justwasm/pigasus/internal/ui/editor/editorkit"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	confhome "github.com/justwasm/pigasus/internal/core/confhome"
	settings "github.com/justwasm/pigasus/internal/core/settings"
	"github.com/justwasm/pigasus/internal/core/slashparse"
	"github.com/justwasm/pigasus/internal/features/tools/isolation"
	"github.com/justwasm/pigasus/internal/printmode"
	"github.com/justwasm/pigasus/internal/ui/keybindings"
	"github.com/justwasm/pigasus/internal/ui/views/skillsui"
)

const keybindingsFileName = "keybindings.json"

func resolveKeybindingsEditor(configuredEditor string) (name string, args []string, source string, ok bool) {
	configuredEditor = slashparse.Trim(configuredEditor)
	if configuredEditor != "" && !strings.EqualFold(configuredEditor, "auto") {
		name, args = editorkit.SplitCommand(configuredEditor)
		return name, args, "settings.editor", true
	}
	if e := slashparse.Trim(os.Getenv("EDITOR")); e != "" {
		name, args = editorkit.SplitCommand(e)
		return name, args, "$EDITOR", true
	}
	if v := slashparse.Trim(os.Getenv("VISUAL")); v != "" {
		name, args = editorkit.SplitCommand(v)
		return name, args, "$VISUAL", true
	}
	return "", nil, "", false
}

const noEditorConfiguredMessage = "No editor configured: set $EDITOR or $VISUAL"

func openKeybindingsFile(configuredEditor string) (notice string, cmd *exec.Cmd, errMsg string) {
	home := confhome.Antigravity()
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

// slashKeybindings reports the configured keybindings.
//
// The report is read-only: the file is read and never written, so the
// command can never re-bind the exit action.
//
// The editor path is a separate, explicit verb ("/keybindings edit"):
// the reference opens the file in $EDITOR (official-docs/settings.md:
// "/keybindings opens the file in your editor"), but the command's own
// answer is the report — the two are different surfaces, and a command
// that silently opens an editor cannot also answer a question.
func (m *runMode) slashKeybindings(args string) (bool, tea.Cmd) {
	if strings.EqualFold(slashparse.Trim(args), "edit") {
		return m.slashKeybindingsEdit()
	}
	path := filepath.Join(confhome.Antigravity(), keybindingsFileName)
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		m.slashRespond("/keybindings", "Failed to read "+path+": "+err.Error())
		return true, nil
	}
	report, err := keybindings.Report(path, data)
	if err != nil {
		m.slashRespond("/keybindings", err.Error())
		return true, nil
	}
	m.slashContent(report)
	return true, nil
}

// slashKeybindingsEdit opens the keybindings file in the configured
// editor.
//
// The file is created with an empty object when it does not exist — the
// reference creates it only when the user asks for the customization
// command 【strings】.
func (m *runMode) slashKeybindingsEdit() (bool, tea.Cmd) {
	notice, editorCmd, errMsg := openKeybindingsFile(settings.String("editor"))
	if errMsg != "" {
		m.slashRespond("/keybindings", errMsg)
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

type jsonSkillInfo = skillsui.SkillInfo

var printSlashSkillInfos = skills.Discover

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
// The /skills panel (slash_panel_routing.go) used to hardcode "63 skills", one
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

// The picker's model, the rows, the cursor, the detail view and the keys
// all live in internal/ui/views/skillsui (cycle 381). The host keeps only
// the catalogue the panel reads — slashSkillCatalog — and the seam file
// skills_host.go.
