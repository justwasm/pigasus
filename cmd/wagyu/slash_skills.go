package main

// slash_skills.go — cycle 199: route "/<skill>" to a model prompt.
//
// §5 P5 of TODO.md ("skill command" gap): the reference treats every skill
// it lists as an invocable slash command. Typing "/<skill-name>" and
// pressing Enter sends that skill's instruction to the model as a prompt;
// the clone previously fell through handleSlashCommand's default branch
// and answered "Unknown command: /<skill>".
//
// ── Cycle 251 (AGY 1.4.2): skill-marketplace-link helper ────────────────
//
// AGY 1.4.2 ships a `GetSkillMarketplaceLink` language-server proto and
// the changelog line "Added a skill-marketplace-link helper". The /skills
// picker integrates it via skillsPanelMarketplaceLink below: each skill
// row whose folder exists at the antigravity-cli/skills tree and is
// *not* supplied by an installed plugin gets a one-line hyperlink to
// the Antigravity plugin marketplace search page (the URL is built by
// cli.MarketplaceLinkForSkill in internal/cli/marketplace_link.go). The
// hyperlink is rendered with OSC 8 when the running terminal advertises
// OSC 8 (cycle 2's render.HyperlinkCaps.OSC8), and falls back to
// "<label> (<url>)" plain text otherwise.
//
// The link is suppressed for:
//   - skills with no on-disk folder ("no user-installable plugin link
//     to attach"),
//   - skills shipped by an installed plugin (the plugin *is* the
//     marketplace install — pointing the user at the marketplace again
//     would be a UX tax).
//
// Print mode (`-p`) does NOT call this helper: the marketplace link is
// only meaningful in an interactive TTY where the OSC 8 frame becomes
// a clickable link.
//
// ── Reference evidence 【一手】 ──────────────────────────────────────────
//
// `~/.local/bin/agy` 1.4.0 in print mode resolves a skill without an OAuth
// completion: `agy -p /terminal-browser` returns the model's skill-framed
// answer ("`terminal-browser` (v0.5.3) is installed and available."), not
// the "not a recognized slash command or skill" fallback. The same holds
// for plugin and builtin skills. This is the routing contract the clone
// now matches.
//
// The catalog itself is the one `/skills` prints (`agy -p /skills`, 57
// entries on this machine) and is already derived by printSlashSkillInfos
// in print_slash.go — builtin bundle + ~/.gemini/skills + every plugin's
// skills/ directory. We deliberately reuse it so the two surfaces can
// never disagree: anything /skills lists is also invocable.
//
// ── Invocable names 【一手】 ────────────────────────────────────────────
//
// The reference accepts three spellings and rejects others:
//
//	science:alphafold-database-fetch-and-analyze   display name       ✓
//	alphafold-database-fetch-and-analyze           name minus prefix  ✓
//	alphafold_database_fetch_and_analyze           directory name     ✓
//	antigravity-guide / antigravity_guide          display / dir      ✓
//	chrome-devtools                                directory name     ✓
//	terminal_browser                               none of the above  ✗
//
// The last row is load-bearing: "terminal_browser" is neither the display
// name nor the directory name of that skill, so the reference does NOT
// normalise `-`/`_`. We therefore match exactly (case-sensitive) against
// the display name, the display name with its plugin prefix stripped, and
// the SKILL.md parent-directory name — and nothing else.
//
// ── The prompt 【推测】 ─────────────────────────────────────────────────
//
// The reference's outgoing wire payload is not observable: print mode shows
// only the model's answer, and cli.log records no prompt body (grepping the
// fresh log for the skill body / "a11y" yields 0 hits). Two candidate
// sources exist on disk and we pick the first-hand one:
//
//  1. the skill's SKILL.md body (the markdown after the frontmatter fence),
//     which is the skill's own instruction text — used when present;
//  2. the one-line frontmatter description — fallback when the body is
//     empty (e.g. a directory with a bare SKILL.md);
//  3. otherwise "Follow the <name> skill.".
//
// The reference may wrap the text in a template, but the body is the only
// first-hand instruction text available, so it is the closest defensible
// approximation. TODO.md §5 P5 records the residual uncertainty.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/justwasm/pigasus/internal/cli"
)

// slashSkillCatalog is the source of the invocable-skill list. It is a var
// rather than a direct call so tests can substitute a synthetic catalog
// without depending on the host's ~/.gemini tree. Production always uses
// printSlashSkillInfos (print_slash.go) — the same enumerator /skills uses.
var slashSkillCatalog = printSlashSkillInfos

// skillInvocationNames returns every name the reference accepts for a
// skill, in priority order: display name, display name with the plugin
// prefix removed, then the SKILL.md parent-directory name. See the file
// comment for the captured evidence behind these three forms.
func skillInvocationNames(s jsonSkillInfo) []string {
	var names []string
	seen := map[string]bool{}
	add := func(n string) {
		if n == "" || seen[n] {
			return
		}
		seen[n] = true
		names = append(names, n)
	}
	add(s.Name)
	// Plugin skills are displayed as "<plugin>:<name>"; the bare <name> is
	// also invocable (verified for a11y-debugging / chrome-devtools).
	if i := strings.LastIndex(s.Name, ":"); i >= 0 {
		add(s.Name[i+1:])
	}
	// The directory that holds SKILL.md is invocable too. This is what lets
	// the underscore spelling of a builtin work ("antigravity_guide") and
	// the bare plugin directory ("chrome-devtools") resolve.
	if dir := filepath.Base(filepath.Dir(s.Path)); dir != "" && dir != "." && dir != string(filepath.Separator) {
		add(dir)
	}
	return names
}

// slashSkillLookup resolves name against the skill catalog. It returns the
// winning skill and true, or a zero value and false. Names are compared
// verbatim (case-sensitive); when two skills share an alias the
// alphabetically-first (catalog order) wins.
func slashSkillLookup(name string) (jsonSkillInfo, bool) {
	if name == "" {
		return jsonSkillInfo{}, false
	}
	for _, s := range slashSkillCatalog() {
		for _, n := range skillInvocationNames(s) {
			if n == name {
				return s, true
			}
		}
	}
	return jsonSkillInfo{}, false
}

// slashSkillPrompt builds the model prompt sent for one skill invocation.
// When args is non-empty it is appended as a separate block so the user's
// trailing text still reaches the model (the reference presumably forwards
// it too; not independently verified — 【推测】).
//
// AGY 1.2.7 contract: args are forwarded verbatim. `@path/to/file` must
// reach the named skill as the literal string `@path/to/file`; the
// caller is responsible for not running any @-file-mention expansion
// before this function is called. The opt-out point is
// `verbatimSkillArgs` (see slash_skills_noat.go); tests pinning the
// contract live alongside the helper.
func slashSkillPrompt(info jsonSkillInfo, args string) string {
	instr := skillBody(info.Path)
	if instr == "" {
		instr = strings.TrimSpace(info.Description)
	}
	if instr == "" {
		instr = "Follow the " + info.Name + " skill."
	}
	if args != "" {
		instr = strings.TrimRight(instr, "\n") + "\n\n" + args
	}
	return instr
}

// skillBody returns the markdown body of a SKILL.md — the bytes after the
// closing `---` frontmatter fence, trimmed. A file with no frontmatter is
// returned whole (some skills ship a plain markdown body). Missing files
// and unterminated frontmatter yield "".
func skillBody(path string) string {
	if path == "" {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	lines := strings.Split(s, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return strings.TrimSpace(s)
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
		}
	}
	return ""
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

// skillsPanelDefaultWidth / Height stand in for m.width / m.height before
// the first tea.WindowSizeMsg reaches the model (mkPanel leaves both 0).
// They reproduce the 220×30 class-A capture (ppref.skills.raw.txt:
// 11 items, "[1-11 of 63 items]"), mirroring configVisibleRowsFor's 10 and
// helpListWindowFor's 18 fallbacks.
const (
	skillsPanelDefaultWidth  = 220
	skillsPanelDefaultHeight = 30
)

// skillsPanelGroup is one /skills catalogue section: a header row followed
// by the group's skills, in display-name order.
type skillsPanelGroup struct {
	left   string // bold text before the " · " separator
	label  string // dim text after the " · " separator
	skills []jsonSkillInfo
}

// skillsPanelItem is one row in the flat item list the counter counts. A
// header and a skill each occupy exactly one item; the blank row before a
// header is not an item.
type skillsPanelItem struct {
	header bool
	group  int // index into skillsPanelModel.groups
	skill  int // index into skillsPanelModel.skills (!header)
}

// skillsPanelModel is the whole picker state: the groups, the flat skill
// list the cursor indexes, the flat item list the counter counts, and the
// item index of each skill (for the window-follows-cursor rule).
type skillsPanelModel struct {
	groups    []skillsPanelGroup
	skills    []jsonSkillInfo
	items     []skillsPanelItem
	skillItem []int
}

// newSkillsPanelModel builds the model from the live catalogue.
func newSkillsPanelModel() skillsPanelModel {
	groups := skillsPanelGroups()
	var m skillsPanelModel
	m.groups = groups
	for gi, g := range groups {
		m.items = append(m.items, skillsPanelItem{header: true, group: gi})
		for _, s := range g.skills {
			si := len(m.skills)
			m.skills = append(m.skills, s)
			m.skillItem = append(m.skillItem, len(m.items))
			m.items = append(m.items, skillsPanelItem{group: gi, skill: si})
		}
	}
	return m
}

// builtinSkillPanelOrder is the 1.4.3 /skills display order for the
// five bundled skills. Captured live at 120x40; the name sort used by
// the slash picker would put "schedule automation" last.
var builtinSkillPanelOrder = map[string]int{
	"agy-customizations":  0,
	"antigravity-guide":   1,
	"schedule automation": 2,
	"migrate-workflows":   3,
	"run-workflow":        4,
}

// skillsPanelGroups splits the catalogue (already sorted by display name)
// into the reference's groups: Shared, Built-in, then the plugin groups in
// plugin-name order. Each group's header reads "<left> · <label>"; the left
// side is the absolute skills directory except for the Shared group, whose
// header is the literal "Shared skills", and the label is "Built-in" for
// the bundle and "From <dir>" (tilde-shortened) for the rest.
func skillsPanelGroups() []skillsPanelGroup {
	var shared, builtin []jsonSkillInfo
	byPlugin := map[string][]jsonSkillInfo{}
	var pluginOrder []string
	for _, s := range slashSkillCatalog() {
		switch {
		case s.Builtin:
			builtin = append(builtin, s)
		case s.Plugin != "":
			if _, ok := byPlugin[s.Plugin]; !ok {
				pluginOrder = append(pluginOrder, s.Plugin)
			}
			byPlugin[s.Plugin] = append(byPlugin[s.Plugin], s)
		default:
			shared = append(shared, s)
		}
	}
	sort.Strings(pluginOrder)

	var groups []skillsPanelGroup
	if len(shared) > 0 {
		groups = append(groups, skillsPanelGroup{
			left:   "Shared skills",
			label:  "From " + tildeHome(skillsGroupDir(shared[0])),
			skills: shared,
		})
	}
	if len(builtin) > 0 {
		// 1.4.3 lists the bundled skills in a fixed catalogue order,
		// not alphabetically: the automation guide sits between
		// antigravity-guide and migrate-workflows even though its
		// display name starts with "s".
		sort.SliceStable(builtin, func(i, j int) bool {
			ri, iok := builtinSkillPanelOrder[builtin[i].Name]
			rj, jok := builtinSkillPanelOrder[builtin[j].Name]
			if iok != jok {
				return iok
			}
			if iok && ri != rj {
				return ri < rj
			}
			return builtin[i].Name < builtin[j].Name
		})
		groups = append(groups, skillsPanelGroup{
			left:   skillsGroupDir(builtin[0]),
			label:  "Built-in",
			skills: builtin,
		})
	}
	for _, p := range pluginOrder {
		ss := byPlugin[p]
		dir := skillsGroupDir(ss[0])
		groups = append(groups, skillsPanelGroup{
			left:   dir,
			label:  "From " + tildeHome(filepath.Dir(dir)),
			skills: ss,
		})
	}
	return groups
}

// skillsPanelCatalog flattens the groups back into the cursor's index
// space: skill 0 is the first Shared skill, the last skill is the last
// plugin entry. Mirrors slashSkillCatalog's order, minus the headers.
func skillsPanelCatalog() []jsonSkillInfo {
	var out []jsonSkillInfo
	for _, g := range skillsPanelGroups() {
		out = append(out, g.skills...)
	}
	return out
}

// skillsGroupDir is the directory a skill was discovered under: every
// catalogue path is "<skillsdir>/<skill>/SKILL.md".
func skillsGroupDir(s jsonSkillInfo) string {
	return filepath.Dir(filepath.Dir(s.Path))
}

// tildeHome rewrites a path under $HOME to its "~" form, the shape the
// reference uses in the plugin and shared group labels. Paths outside
// $HOME (or when $HOME is unknown) are returned unchanged.
func tildeHome(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || home == "/" {
		return p
	}
	if p == home {
		return "~"
	}
	if strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + p[len(home):]
	}
	return p
}

// skillsVisibleItemsFor returns how many items the reference draws at once
// for a terminal of the given height. Measured with the caps/ref.h*.raw.txt
// probes (220 wide):
//
//	h : 16 17 18 20 24 28 30 32 36 40 44 46 50 60 70 80 98  99+
//	n :  1  1  2  4  7 10 11 13 16 19 22 23 26 34 41 49 62  63(all)
//
// every sample satisfies n = height*3/4 - 11 (integer division); below
// h=16 the reference's layout solver drops chrome in a way this formula
// cannot express, so n is floored at 1 and heights ≤15 are left as an
// approximation (see the 存疑 section of the cycle report). A height of 0
// means no WindowSizeMsg has arrived yet — fall back to the 220×30 capture
// (11), the pre-203 snapshot the byte-exact tests assert.
func skillsVisibleItemsFor(height int) int {
	if height <= 0 {
		return skillsPanelDefaultHeight*3/4 - 11 // 11
	}
	n := height*3/4 - 11
	if n < 1 {
		n = 1
	}
	return n
}

// skillsPanelWorkspacePath is the first "Create new skills" line: the
// logical working directory + "/.agents/skills/{skill_name}/SKILL.md".
// os.Getwd honours $PWD when it names the same directory, which is how the
// reference prints "/tmp/…" (not the resolved "/private/tmp/…").
func skillsPanelWorkspacePath() string {
	cwd, err := os.Getwd()
	if err != nil || cwd == "" {
		cwd = "."
	}
	// The reference shortens a workspace under $HOME to `~/...` in the
	// Create-new-skills block (the group headers stay absolute). Verified
	// live at 120x40 on agy 1.4.3.
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if rel, err := filepath.Rel(home, cwd); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			cwd = filepath.Join("~", rel)
		}
	}
	return filepath.Join(cwd, ".agents", "skills", "{skill_name}", "SKILL.md")
}

// skillsPanelRow renders one skill row. The name is brand blue; the cursor
// row moves the "  " prefix inside the blue span as "› ". The description
// is dim gray 238 and is cut mid-word to keep the rendered line within
// width-1 columns, an ASCII "..." marking the cut. A built-in entry
// reserves a trailing " · built-in" (italic gray 241) before truncating.
func skillsPanelRow(s jsonSkillInfo, cursor bool, width int) string {
	prefix := "  "
	if cursor {
		prefix = "› "
	}
	suffixPlain, suffixRaw := "", ""
	if s.Builtin {
		suffixPlain = " · built-in"
		suffixRaw = " · \x1b[3m\x1b[38;2;101;123;131mbuilt-in\x1b[0m"
	}
	desc := s.Description
	fixed := len([]rune(prefix)) + len([]rune(s.Name)) + 2 + len([]rune(suffixPlain))
	if avail := width - 1 - fixed; avail < len([]rune(desc)) {
		dr := []rune(desc)
		if avail < 0 {
			avail = 0
		}
		if avail >= 3 {
			desc = string(dr[:avail-3]) + "..."
		} else {
			desc = strings.Repeat(".", avail)
		}
	}
	// The cursor glyph sits *inside* the blue name span; the blank prefix
	// of a plain row sits outside it (caps/ref.down1.raw.txt).
	var head string
	if cursor {
		head = cBlueOpen + prefix + s.Name + cBlueClose + ": "
	} else {
		head = prefix + cBlueOpen + s.Name + cBlueClose + ": "
	}
	return head + panelGray238 + desc + panelGray238C + suffixRaw
}

// skillsPanelMarketplaceLink renders the AGY 1.4.2 skill-marketplace-link
// line that lives below a skill row when the user has the skill
// installed locally but no installed plugin supplies it. Returns ""
// (no extra line) when the helper is suppressed.
//
// The line shape is:
//
//	↳ <hyperlink-or-label>     (when emitted, dim gray-238 surroundings)
//
// where the hyperlink is one of:
//
//	OSC 8 supported:    ESC ] 8 ; ; <URL> ST <label> ESC ] 8 ; ; ST
//	OSC 8 unsupported:  <label> (<URL>)   (e.g. "Marketplace (…/skills?q=alpha)")
//
// The directory name (not the displayed name) is what the URL points at.
// For plugin-suffixed entries (e.g. "chrome-devtools-plugin:a11y") the
// helper checks "a11y" — the bare skill name with the plugin prefix
// stripped — because that's what a marketplace install would write.
//
// Cycle 251: the dim-gray framing matches the rest of the panel's
// secondary-text style (panelGray238 / panelGray238C). The line is
// rendered with two extra spaces of leading indent so it stays out of
// the visual column the cursor block lives in.
func skillsPanelMarketplaceLink(s jsonSkillInfo, osc8Supported bool) string {
	name := s.Name
	// The marketplace server sees the bare skill name, not the plugin
	// suffix — "a11y" is what a marketplace search picks up, not
	// "chrome-devtools-plugin:a11y". Plugin prefixes are a UI detail
	// for the installed view; the marketplace doesn't know about
	// them.
	if i := strings.LastIndex(name, ":"); i >= 0 && !s.Builtin {
		name = name[i+1:]
	}
	// Skill-name -> marketplace URL: cli.MarketplaceLinkForSkill
	// returns "" when (a) the skill folder is absent, or (b) an
	// installed plugin already supplies the same name. Both gates
	// are part of the helper's contract.
	url := cli.MarketplaceLinkForSkill(name)
	if url == "" {
		return ""
	}
	// Emit with OSC 8 when the terminal supports it; the helper falls
	// back to "label (url)" plain text otherwise. The default label
	// is the literal "Marketplace" — AGY 1.4.2's release-note
	// wording — and the panel keeps it unchanged.
	rendered := cli.EmitMarketplaceLink(url, "", osc8Supported)
	if rendered == "" {
		return ""
	}
	// Two leading spaces of indent + the dim-grey-238 wrapper that
	// every panel secondary-text line uses (the panel's "Description"
	// / "Path:" / counter rows all open with panelGray238 and close
	// with panelGray238C).
	return "  " + panelGray238 + "↳ " + rendered + panelGray238C
}

// skillsPanelLines renders the /skills picker. See the block comment above
// for the recovered layout; panelSkillsLines (slash_panels.go) delegates
// here.
func (m *runMode) skillsPanelLines() []string {
	model := newSkillsPanelModel()
	width := m.width
	if width <= 0 {
		width = skillsPanelDefaultWidth
	}
	lines := []string{
		panelTitleOpen + "Skills" + panelTitleClose,
		cDimGrayOpen + fmt.Sprintf("%d skills", len(model.skills)) + cDimGrayClose,
		"",
		"\x1b[1mCreate new skills\x1b[0m",
		"  " + cDimGrayOpen + "Workspace: " + skillsPanelWorkspacePath() + cDimGrayClose,
		"  " + cDimGrayOpen + "Global:    ~/.gemini/antigravity-cli/skills/{skill_name}/SKILL.md" + cDimGrayClose,
		"  " + cDimGrayOpen + "Shared:    ~/.gemini/skills/{skill_name}/SKILL.md" + cDimGrayClose,
		"",
	}
	if len(model.items) == 0 {
		lines = append(lines, "", panelKeyboardHint("↑/↓", "Navigate", "enter", "View", "esc", "Cancel"))
		return lines
	}

	cursor := m.panel.cursor
	if cursor < -1 || cursor >= len(model.skills) {
		cursor = -1
	}
	n := skillsVisibleItemsFor(m.height)
	if n > len(model.items) {
		n = len(model.items)
	}
	start := 0
	if cursor >= 0 {
		start = model.skillItem[cursor] - (n - 1)
		if start < 0 {
			start = 0
		}
	}
	end := start + n
	if end > len(model.items) {
		end = len(model.items)
	}

	first := true
	for i := start; i < end; i++ {
		it := model.items[i]
		if it.header {
			if !first {
				lines = append(lines, "")
			}
			g := model.groups[it.group]
			lines = append(lines, "\x1b[1m"+g.left+" · "+panelGray238+g.label+"\x1b[0m")
		} else {
			s := model.skills[it.skill]
			lines = append(lines, skillsPanelRow(s, it.skill == cursor, width))
			// Cycle 251 (AGY 1.4.2): the skill-marketplace-link helper.
			// Only emitted when the user has the skill folder installed
			// locally *and* no installed plugin already supplies the
			// same name (cli.MarketplaceLinkForSkill has both gates in
			// one call). The helper returns "" otherwise, so this
			// block is a no-op when the link is suppressed.
			if link := skillsPanelMarketplaceLink(s, m.hyperlinkCaps.OSC8); link != "" {
				lines = append(lines, link)
			}
		}
		first = false
	}
	if n < len(model.items) {
		lines = append(lines, cDimGrayOpen+
			fmt.Sprintf("  [%d-%d of %d items]", start+1, end, len(model.items))+
			cDimGrayClose)
	}
	lines = append(lines, "", panelKeyboardHint("↑/↓", "Navigate", "enter", "View", "esc", "Cancel"))
	return lines
}

// moveSkillsCursor advances /skills' selection for a ↑/↓ key. The cursor
// indexes skills only (the group headers are unselectable) and admits a
// "no selection" state of -1, which is where the panel opens: ↓ from -1
// selects the first skill, and both keys then clamp at the ends (↑ on the
// first skill stays put — it does not return to -1). Captured in
// caps/ref.down*.raw.txt / ref.up*.raw.txt: eight Down,Up trials across
// 140/220 × 24/50 all left the cursor on agents-sdk, while a lone ↑ (and
// repeated ↑) leaves the panel unselected.
func (m *runMode) moveSkillsCursor(down bool) {
	n := len(skillsPanelCatalog())
	if n == 0 {
		m.panel.cursor = -1
		return
	}
	if down {
		if m.panel.cursor < n-1 {
			m.panel.cursor++
		}
		return
	}
	if m.panel.cursor > 0 {
		m.panel.cursor--
	}
}

// skillsPanelDetailLines renders the per-skill view /skills' `enter View`
// opens when a row is selected. Cycle 211 wires it to the full SKILL.md
// document viewer (title, wrapped description, path, the file body with a
// "(a–b of T lines)" window and the ↑/↓ scroll · pgup/pgdown page · ctrl+end
// bottom · ctrl+home top · esc back legend) implemented in
// skillsDetailViewLines in slash_skills_detail.go. The earlier
// cycle-201/cycle-203 shape (title + description + path + esc only) is the
// fallback when no real SKILL.md content can be located (no Path set,
// unreadable file). See cycle-211/deliverable-skills-detail.md for the
// evidence table.
func (m *runMode) skillsPanelDetailLines() []string {
	if m.skillsDetailSelected().Path == "" {
		return m.skillsDetailLegacyLines()
	}
	return m.skillsDetailViewLines()
}

// skillsDetailLegacyLines is the cycle-203 minimal shape: title, description,
// path, esc-only legend. Used when a catalogue row points at no SKILL.md
// (rare; mainly a defensive fallback for paths that get read off disk during
// the test fixtures).
func (m *runMode) skillsDetailLegacyLines() []string {
	s := m.skillsDetailSelected()
	return []string{
		panelTitleOpen + "Skills" + panelTitleClose,
		"",
		"  " + "\x1b[1m" + cBlueOpen + s.Name + "\x1b[0m",
		"",
		"  " + panelGray238 + s.Description + panelGray238C,
		"",
		"  " + panelGray238 + "Path: " + s.Path + panelGray238C,
		"",
		panelKeyboardHint("esc", "Back"),
	}
}
