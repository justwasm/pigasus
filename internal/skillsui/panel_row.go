package skillsui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/palette"
)

const (
	PanelGray238C = "\x1b[39m"
)

// PanelGray238 is the dim grey the row's descriptions are painted in —
// the selected scheme's muted foreground.
var PanelGray238 = palette.SubtleOpen

func init() {
	palette.OnSchemeChange(refreshColours)
}

// refreshColours re-reads the palette after the selected colour scheme
// changed.
func refreshColours() {
	PanelGray238 = palette.SubtleOpen
}

// DisplayName is the name the reference's /skills panel renders for a skill:
// the frontmatter `metadata.icon` (when the skill declares one), a space, and
// the bare name.
//
// 【一手】 agy 1.4.8: the bundled automation skill lists as
// "schedule automation" (its frontmatter declares `name: automation` plus
// `metadata.icon: schedule`), a probe skill with `metadata.icon: rocket`
// lists as "rocket zziconprobe", and an icon the reference does not know
// ("zzznotanicon") is rendered verbatim — there is no glyph table and no
// filtering. The same string heads the skill's detail view.
func DisplayName(s SkillInfo) string {
	if s.Icon == "" {
		return s.Name
	}
	return s.Icon + " " + s.Name
}

// PanelRow formats one skill row in the /skills picker list.
func PanelRow(s SkillInfo, cursor bool, width int) string {
	prefix := "  "
	if cursor {
		prefix = "› "
	}
	suffixPlain, suffixRaw := "", ""
	if s.Builtin {
		suffixPlain = " · built-in"
		suffixRaw = " · \x1b[3m\x1b[38;2;101;123;131mbuilt-in\x1b[0m"
	}
	name := DisplayName(s)
	desc := s.Description
	fixed := len([]rune(prefix)) + len([]rune(name)) + 2 + len([]rune(suffixPlain))
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
	var head string
	if cursor {
		head = palette.BlueOpen + prefix + name + palette.BlueClose + ": "
	} else {
		head = prefix + palette.BlueOpen + name + palette.BlueClose + ": "
	}
	return head + PanelGray238 + desc + PanelGray238C + suffixRaw
}

// MarketplaceLink renders a clickable marketplace hyperlink for a skill if applicable.
func MarketplaceLink(s SkillInfo, osc8Supported bool) string {
	name := s.Name
	if i := strings.LastIndex(name, ":"); i >= 0 && !s.Builtin {
		name = name[i+1:]
	}
	url := cli.MarketplaceLinkForSkill(name)
	if url == "" {
		return ""
	}
	rendered := cli.EmitMarketplaceLink(url, "", osc8Supported)
	if rendered == "" {
		return ""
	}
	return "  " + PanelGray238 + "↳ " + rendered + PanelGray238C
}

// WorkspaceSkillsPath returns the display path for new workspace skills.
func WorkspaceSkillsPath() string {
	cwd, err := os.Getwd()
	if err != nil || cwd == "" {
		cwd = "."
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if rel, err := filepath.Rel(home, cwd); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			cwd = filepath.Join("~", rel)
		}
	}
	return filepath.Join(cwd, ".agents", "skills", "{skill_name}", "SKILL.md")
}

// HeaderLines renders the header block of the skills panel.
func HeaderLines(skillsCount int) []string {
	return []string{
		"\x1b[1m\x1b[39mSkills\x1b[0m",
		palette.DimGrayOpen + fmt.Sprintf("%d skills", skillsCount) + palette.DimGrayClose,
		"",
		"\x1b[1mCreate new skills\x1b[0m",
		"  " + palette.DimGrayOpen + "Workspace: " + WorkspaceSkillsPath() + palette.DimGrayClose,
		"  " + palette.DimGrayOpen + "Global:    ~/.gemini/antigravity-cli/skills/{skill_name}/SKILL.md" + palette.DimGrayClose,
		"  " + palette.DimGrayOpen + "Shared:    ~/.gemini/skills/{skill_name}/SKILL.md" + palette.DimGrayClose,
		"",
	}
}
