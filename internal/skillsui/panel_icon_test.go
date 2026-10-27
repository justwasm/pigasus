package skillsui

import (
	"strings"
	"testing"
)

// TestDisplayNameRendersIcon pins the /skills panel's name rendering: the
// frontmatter icon, one space, then the bare name.
//
// 【一手】 agy 1.4.8: the bundled automation skill declares
// `name: automation` plus `metadata.icon: schedule` and lists as
// "schedule automation"; a probe skill whose frontmatter declares
// `metadata.icon: zzznotanicon` lists as "zzznotanicon zziconprobe" — the
// reference does not filter the icon against a table of known glyphs.
func TestDisplayNameRendersIcon(t *testing.T) {
	cases := []struct {
		skill SkillInfo
		want  string
	}{
		{SkillInfo{Name: "agents-sdk"}, "agents-sdk"},
		{SkillInfo{Name: "automation", Icon: "schedule"}, "schedule automation"},
		{SkillInfo{Name: "zziconprobe", Icon: "zzznotanicon"}, "zzznotanicon zziconprobe"},
		{SkillInfo{Name: "chrome-devtools-plugin:a11y-debugging"}, "chrome-devtools-plugin:a11y-debugging"},
	}
	for _, c := range cases {
		if got := DisplayName(c.skill); got != c.want {
			t.Errorf("DisplayName(%+v) = %q, want %q", c.skill, got, c.want)
		}
	}
}

// TestPanelRowRendersIcon pins that the row carries the icon-prefixed name.
func TestPanelRowRendersIcon(t *testing.T) {
	row := PanelRow(SkillInfo{Name: "automation", Icon: "schedule", Description: "A guide."}, false, 120)
	if !strings.Contains(row, "schedule automation") {
		t.Errorf("row = %q, want it to carry %q", row, "schedule automation")
	}
	plain := PanelRow(SkillInfo{Name: "automation", Description: "A guide."}, false, 120)
	if strings.Contains(plain, "schedule") {
		t.Errorf("row = %q, want no icon", plain)
	}
}
