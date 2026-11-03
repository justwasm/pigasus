// print_slash_skills.go — the print path's /skills command.
//
// The discovery itself lives in the skills feature
// (`internal/features/tools/skills`): the TUI's /skills panel, the
// `<skill>` router and the print path's `/skills` command all enumerate
// the same installed skills, and one enumerator is what keeps them in
// step — the reference's own AGY 1.2.7 content-hash dedup fix has to
// reach every surface at once.
//
// What stays here is the print path's own shape: the tab-separated
// listing `agy -p /skills` emits and the JSON payload behind it.
package printmode

import (
	"github.com/justwasm/pigasus/internal/features/tools/skills"
)

// printSlashSkillInfos enumerates the installed skills.
func printSlashSkillInfos() []skills.SkillInfo {
	return skills.Discover()
}

// printSlashSkills lists installed skills as "<name>\t<description>".
func printSlashSkills() string {
	return skills.Render()
}

// PrintSlashSkills lists installed skills as "<name>\t<description>".
func PrintSlashSkills() string {
	return skills.Render()
}

// printSlashSkillsData renders the `/skills` command payload.
func printSlashSkillsData() any {
	return map[string]any{"skills": skills.Discover()}
}

// printSlashSkillsResult answers the `/skills` command.
//
// The bare command lists the installed skills. "reload" re-scans the skill
// roots from disk and prefixes the reference's own line — "Reload skills
// from disk" — above the reloaded listing.
//
// 【strings】 agy 1.4.x: the binary's string table carries the literal
// "Reload skills from disk" next to "/skills reload"; the wording after
// that line is unrecovered (【推测】 the listing is kept).
func printSlashSkillsResult(args string) printSlashResult {
	if skills.IsReload(args) {
		list := skills.Reload()
		return printSlashResult{
			Handled: true,
			Output:  skills.ReloadMessage + "\n" + skills.RenderList(list),
			Name:    "skills",
			Data:    map[string]any{"skills": list},
		}
	}
	return printSlashResult{Handled: true, Output: printSlashSkills(), Name: "skills", Data: printSlashSkillsData()}
}
