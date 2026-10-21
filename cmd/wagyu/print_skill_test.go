package main

// print_skill_test.go — cycle 203: print-mode "/<skill>" expansion.
//
// The reference expands "/<skill>" to the skill's instruction text in print
// mode too, not just in the TUI: `agy -p /terminal-browser` returns a
// skill-framed answer rather than the literal token. dispatchPrintSlash now
// resolves the name against the /skills catalog (slashSkillLookup) and hands
// the instruction text back in printSlashResult.Prompt, which runPrintMode
// substitutes for the literal prompt. These tests pin that contract for the
// print-mode dispatcher; runPrintMode itself needs a live backend and is not
// exercised here. See slash_skills.go for the captured aliases / prompt choice.

import (
	"testing"
)

// TestDispatchPrintSlash_SkillExpandsToPrompt pins that a known skill is not
// forwarded literally: it resolves to a Prompt (the SKILL.md body) while
// Handled stays false so the model turn still runs.
func TestDispatchPrintSlash_SkillExpandsToPrompt(t *testing.T) {
	p := writeSkillMD(t, "---\nname: demo-skill\ndescription: a demo\n---\nSkill instructions here.\n")
	withSyntheticCatalog(t, []jsonSkillInfo{{Name: "demo-skill", Description: "a demo", Path: p}})
	fs := newStandardFlagSetForTest(t, nil)

	res := dispatchPrintSlash("/demo-skill", fs)
	if res.Handled {
		t.Error("a skill must not be consumed locally; the model turn still runs")
	}
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if res.Prompt != "Skill instructions here." {
		t.Errorf("Prompt = %q, want the skill body", res.Prompt)
	}
	if res.Prompt == "/demo-skill" {
		t.Error("the skill token was forwarded literally")
	}
}

// TestDispatchPrintSlash_SkillForwardsArgs pins that trailing text reaches the
// model as a second block, and that a bare "/<skill>" (no args) still resolves.
func TestDispatchPrintSlash_SkillForwardsArgs(t *testing.T) {
	p := writeSkillMD(t, "---\nname: demo-skill\n---\nBody.\n")
	withSyntheticCatalog(t, []jsonSkillInfo{{Name: "demo-skill", Path: p}})
	fs := newStandardFlagSetForTest(t, nil)

	res := dispatchPrintSlash("/demo-skill inspect example.com", fs)
	if want := "Body.\n\ninspect example.com"; res.Prompt != want {
		t.Errorf("Prompt with args = %q, want %q", res.Prompt, want)
	}

	res = dispatchPrintSlash("/demo-skill", fs)
	if res.Prompt != "Body." {
		t.Errorf("Prompt with no args = %q, want %q", res.Prompt, "Body.")
	}
}

// TestDispatchPrintSlash_SkillAliases pins the plugin-prefix and
// SKILL.md-directory spellings through the print dispatcher, matching the TUI.
func TestDispatchPrintSlash_SkillAliases(t *testing.T) {
	p := writeSkillMD(t, "---\nname: chrome-devtools\n---\nCDP body.\n")
	withSyntheticCatalog(t, []jsonSkillInfo{{
		Name: "chrome-devtools-plugin:chrome-devtools", Path: p,
	}})
	fs := newStandardFlagSetForTest(t, nil)

	for _, token := range []string{
		"/chrome-devtools-plugin:chrome-devtools",
		"/chrome-devtools",
	} {
		res := dispatchPrintSlash(token, fs)
		if res.Prompt != "CDP body." {
			t.Errorf("%s Prompt = %q, want the skill body", token, res.Prompt)
		}
	}
}

// TestDispatchPrintSlash_UnknownNonSkillUnchanged guards the fallback: a name
// that is neither a command nor a skill keeps Handled=false with an empty
// Prompt, so it is forwarded as literal text exactly as before.
func TestDispatchPrintSlash_UnknownNonSkillUnchanged(t *testing.T) {
	withSyntheticCatalog(t, nil)
	fs := newStandardFlagSetForTest(t, nil)

	for _, prompt := range []string{"/zzznotacommand", "/plan the trip", "/init"} {
		res := dispatchPrintSlash(prompt, fs)
		if res.Handled || res.Prompt != "" || res.Err != nil {
			t.Errorf("dispatchPrintSlash(%q) = %+v, want a bare forward", prompt, res)
		}
	}
}

// TestDispatchPrintSlash_SkillDisableFlag pins that --disable-slash-commands
// short-circuits before skill resolution: the literal token is forwarded and
// no Prompt is produced.
func TestDispatchPrintSlash_SkillDisableFlag(t *testing.T) {
	p := writeSkillMD(t, "---\nname: demo-skill\n---\nBody.\n")
	withSyntheticCatalog(t, []jsonSkillInfo{{Name: "demo-skill", Path: p}})
	fs := newStandardFlagSetForTest(t, []string{"--disable-slash-commands"})

	res := dispatchPrintSlash("/demo-skill do it", fs)
	if res.Handled || res.Prompt != "" || res.Err != nil {
		t.Errorf("with --disable-slash-commands: %+v, want a bare forward", res)
	}
}

// TestDispatchPrintSlash_RejectionBeatsSkill pins that an interactive-only
// rejection still wins even when a skill shares the name: print mode refuses
// the command rather than expanding the skill.
func TestDispatchPrintSlash_RejectionBeatsSkill(t *testing.T) {
	p := writeSkillMD(t, "---\nname: clear\n---\nBody.\n")
	withSyntheticCatalog(t, []jsonSkillInfo{{Name: "clear", Path: p}})
	fs := newStandardFlagSetForTest(t, nil)

	res := dispatchPrintSlash("/clear", fs)
	if res.Err == nil {
		t.Error("/clear must still be rejected in print mode")
	}
	if res.Prompt != "" {
		t.Errorf("a rejected command must not produce a skill Prompt, got %q", res.Prompt)
	}
}
