// panel_model.go — the /skills picker's catalogue model.
//
// The model is the panel's whole state: the groups the reference renders
// as headers, the flat skill list the cursor indexes, the flat item list
// the counter counts, and the item index of each skill (for the
// window-follows-cursor rule).
//
// The catalogue is threaded in as a parameter rather than read from a
// package-level var: the TUI's production catalogue is
// printmode.PrintSlashSkillInfos, while tests substitute a synthetic one.

package skillsui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// PanelDefaultWidth and PanelDefaultHeight stand in for the TUI's
	// width / height before the first tea.WindowSizeMsg reaches the
	// model. They reproduce the 220×30 class-A capture (11 items,
	// "[1-11 of 63 items]"), mirroring configVisibleRowsFor's 10 and
	// helpListWindowFor's 18 fallbacks.
	PanelDefaultWidth  = 220
	PanelDefaultHeight = 30
)

// PanelGroup is one /skills catalogue section: a header row followed by
// the group's skills, in display-name order.
type PanelGroup struct {
	Left   string // bold text before the " · " separator
	Label  string // dim text after the " · " separator
	Skills []SkillInfo
}

// PanelItem is one row in the flat item list the counter counts. A
// header and a skill each occupy exactly one item; the blank row before a
// header is not an item.
type PanelItem struct {
	Header bool
	Group  int // index into PanelModel.Groups
	Skill  int // index into PanelModel.Skills (!Header)
}

// PanelModel is the whole picker state: the groups, the flat skill list
// the cursor indexes, the flat item list the counter counts, and the item
// index of each skill (for the window-follows-cursor rule).
type PanelModel struct {
	Groups    []PanelGroup
	Skills    []SkillInfo
	Items     []PanelItem
	SkillItem []int
}

// builtinSkillPanelOrder is the 1.4.3 /skills display order for the five
// bundled skills. Captured live at 120x40; the name sort used by the
// slash picker would put "schedule automation" last.
var builtinSkillPanelOrder = map[string]int{
	"agy-customizations":  0,
	"antigravity-guide":   1,
	"schedule automation": 2,
	"migrate-workflows":   3,
	"run-workflow":        4,
}

// SortBuiltinSkills puts the bundled skills in the reference's fixed
// catalogue order, not alphabetically: the automation guide sits between
// antigravity-guide and migrate-workflows even though its display name
// starts with "s".
func SortBuiltinSkills(builtin []SkillInfo) {
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
}

// PartitionSkillsForPanel splits the catalogue into the shared, builtin
// and per-plugin buckets, plus the plugin names in plugin-name order.
func PartitionSkillsForPanel(skills []SkillInfo) ([]SkillInfo, []SkillInfo, map[string][]SkillInfo, []string) {
	var shared, builtin []SkillInfo
	byPlugin := map[string][]SkillInfo{}
	var pluginOrder []string
	for _, s := range skills {
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
	return shared, builtin, byPlugin, pluginOrder
}

// PanelGroups splits the catalogue (already sorted by display name) into
// the reference's groups: Shared, Built-in, then the plugin groups in
// plugin-name order. Each group's header reads "<left> · <label>"; the left
// side is the absolute skills directory except for the Shared group, whose
// header is the literal "Shared skills", and the label is "Built-in" for
// the bundle and "From <dir>" (tilde-shortened) for the rest.
func PanelGroups(catalog []SkillInfo) []PanelGroup {
	shared, builtin, byPlugin, pluginOrder := PartitionSkillsForPanel(catalog)

	var groups []PanelGroup
	if len(shared) > 0 {
		groups = append(groups, PanelGroup{
			Left:   "Shared skills",
			Label:  "From " + TildeHome(GroupDir(shared[0])),
			Skills: shared,
		})
	}
	if len(builtin) > 0 {
		SortBuiltinSkills(builtin)
		groups = append(groups, PanelGroup{
			Left:   GroupDir(builtin[0]),
			Label:  "Built-in",
			Skills: builtin,
		})
	}
	for _, p := range pluginOrder {
		ss := byPlugin[p]
		dir := GroupDir(ss[0])
		groups = append(groups, PanelGroup{
			Left:   dir,
			Label:  "From " + TildeHome(filepath.Dir(dir)),
			Skills: ss,
		})
	}
	return groups
}

// PanelCatalog flattens the groups back into the cursor's index space:
// skill 0 is the first Shared skill, the last skill is the last plugin
// entry. Mirrors the catalogue's order, minus the headers.
func PanelCatalog(catalog []SkillInfo) []SkillInfo {
	var out []SkillInfo
	for _, g := range PanelGroups(catalog) {
		out = append(out, g.Skills...)
	}
	return out
}

// NewPanelModel builds the model from the live catalogue.
func NewPanelModel(catalog []SkillInfo) PanelModel {
	groups := PanelGroups(catalog)
	var m PanelModel
	m.Groups = groups
	for gi, g := range groups {
		m.Items = append(m.Items, PanelItem{Header: true, Group: gi})
		for _, s := range g.Skills {
			si := len(m.Skills)
			m.Skills = append(m.Skills, s)
			m.SkillItem = append(m.SkillItem, len(m.Items))
			m.Items = append(m.Items, PanelItem{Group: gi, Skill: si})
		}
	}
	return m
}

// GroupDir is the directory a skill was discovered under: every catalogue
// path is "<skillsdir>/<skill>/SKILL.md".
func GroupDir(s SkillInfo) string {
	return filepath.Dir(filepath.Dir(s.Path))
}

// TildeHome rewrites a path under $HOME to its "~" form, the shape the
// reference uses in the plugin and shared group labels. Paths outside
// $HOME (or when $HOME is unknown) are returned unchanged.
func TildeHome(p string) string {
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

// VisibleItemsFor returns how many items the reference draws at once for
// a terminal of the given height. Measured with the caps/ref.h*.raw.txt
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
func VisibleItemsFor(height int) int {
	if height <= 0 {
		return PanelDefaultHeight*3/4 - 11 // 11
	}
	n := height*3/4 - 11
	if n < 1 {
		n = 1
	}
	return n
}
