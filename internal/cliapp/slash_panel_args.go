// slash_panel_args.go — opening a panel that takes an argument tail.
//
// Most panels are opened by their bare command name. A few take an
// argument tail, and the tail changes what the command does: some
// commands are direct setters when they carry one, and one panel —
// /codesearch — renders the tail.
//
// The contract is the cycle-228 one: bare `/codesearch` (no args)
// short-circuits to the `⎿  /codesearch needs a query.` echo, so the
// panel only opens when there is a query to render.

package cliapp

import (
	"strings"

	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/slashcommands"
)

// openSlashPanelArgs is openSlashPanel plus the command's argument tail,
// which only /codesearch consumes today.
//
// Cycle 383 (A-6): the rules below are rules about a *command*, so they
// are stated in the command's canonical name. The alias resolves first:
// `/plugins list` is `/plugin list`, and it must dispatch the verb
// instead of opening the Plugins Manager with the argument swallowed.
func (m *runMode) openSlashPanelArgs(cmd, args string) bool {
	if canonical, ok := slashcommands.Canonical(cmd); ok {
		cmd = canonical
	}
	if cmd == "add-dir" && args != "" {
		return false
	}
	if cmd == "codesearch" && strings.TrimSpace(args) == "" {
		return false
	}
	// Cycle effort-picker: `/effort <level>` (valid or invalid) is a direct
	// setter that goes through the slash router (case "effort" in
	// slash_commands.go) so it can echo "Effort set to <level>." / "Invalid
	// level: <level>." without opening the picker. A bare `/effort` still
	// opens the timeline-gauge panel below.
	if cmd == "effort" && args != "" {
		return false
	}
	// `/model <name>` is a direct setter: the reference changes the model
	// and echoes "Model set to <label>" instead of opening the Switch
	// Model panel. Only a bare `/model` opens the panel.
	// 【一手】 agy 1.4.6, live.
	if cmd == "model" && strings.TrimSpace(args) != "" {
		return false
	}
	// Cycle plugin-case-insensitive: `/plugin <subcmd>` (e.g.
	// `/plugin List`) is a direct command dispatch handled by
	// `case "plugin"` in slash_commands.go, NOT the panel.
	// AGY 1.3.6 changelog line 21 names this gap: the
	// pre-cycle build opened the panel for every /plugin
	// invocation and silently swallowed the argument tail,
	// leaving `/plugin List` / `/plugin LIST` / `/plugin
	// install foo` indistinguishable from bare `/plugin`.
	// Bare `/plugin` (no args) still opens the panel below.
	if cmd == "plugin" && strings.TrimSpace(args) != "" {
		return false
	}
	// `/skills reload` re-reads the skills from disk: the documented verb
	// (official-docs/cli/reference.md:47 — "Run `/skills reload` to reload
	// skills from disk"). It is a command, not a panel, so the verb
	// reaches the host's handler instead of opening the picker with the
	// argument swallowed. A bare `/skills` still opens the panel below.
	if cmd == "skills" && strings.TrimSpace(args) != "" {
		return false
	}
	if !m.openSlashPanel(cmd) {
		return false
	}
	if m.panel.kind == panelregistry.CodeSearch {
		// The panel owns the search; the host only hands it the
		// argument tail the user typed.
		m.codesearchPanel().Start(args)
	}
	return true
}
