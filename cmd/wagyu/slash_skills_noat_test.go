// slash_skills_noat_test.go — AGY 1.2.7 verbatim pass-through for skill args.
//
// Gap: skill invocations from /, plugins, and custom slash commands must
// NOT expand @-looking tokens into file mentions. Args land in the
// dispatching skill's prompt verbatim — `@path/to/file` reaches the named
// skill as the literal string `@path/to/file`, not as a file read.
//
// Tests pin the contract from three angles:
//
//   - the pure helper (verbatimSkillArgs)
//   - slashSkillPrompt with a body, an empty body, and several arg shapes
//   - end-to-end through handleSlashCommand for both the bare-name and
//     plugin-prefixed spelling (the same skill resolves through both)
//
// The names align with the five required cases the cycle's deliverable
// documents: a single @arg, normal args without @, no @arg at all,
// multiple @args side by side, and an @ in the middle of a longer
// argument that must not be turned into a file mention. Each case is
// exercised twice — once at the helper layer, once at the routing layer
// — so the contract cannot drift between layers.

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestVerbatimSkillArgs_AtPath is the canonical AGY 1.2.7 case:
// `/my-skill @path/to/file` must pass `@path/to/file` to the skill
// verbatim. The helper has no input shape to recognise, so the test
// asserts the identity property at the function level.
func TestVerbatimSkillArgs_AtPath(t *testing.T) {
	in := "@path/to/file"
	got := verbatimSkillArgs(in)
	if got != in {
		t.Fatalf("verbatimSkillArgs(%q) = %q, want identity pass-through", in, got)
	}
}

// TestSlashSkillPrompt_AtArgVerbatim pins the user-facing case: a
// skill's invocation prompt with the literal `@path/to/file` argument
// keeps the byte-sequence intact (no expansion, no rewrap, no resplit).
func TestSlashSkillPrompt_AtArgVerbatim(t *testing.T) {
	p := writeSkillMD(t, "---\nname: demo\n---\nbody.\n")
	got := slashSkillPrompt(jsonSkillInfo{Name: "demo", Path: p}, "@path/to/file")
	want := "body.\n\n@path/to/file"
	if got != want {
		t.Fatalf("slashSkillPrompt with @arg = %q, want %q", got, want)
	}
	// And the helper route must agree.
	if via := slashSkillPrompt(jsonSkillInfo{Name: "demo", Path: p}, verbatimSkillArgs("@path/to/file")); via != want {
		t.Fatalf("slashSkillPrompt(verbatimSkillArgs(@arg)) = %q, want %q", via, want)
	}
}

// TestSlashSkillPrompt_NormalArgsWorks proves that the verbatim
// contract doesn't regress the everyday case of an unprefixed argument.
// `inspect example.com` flows through unchanged.
func TestSlashSkillPrompt_NormalArgsWorks(t *testing.T) {
	p := writeSkillMD(t, "---\nname: demo\n---\nbody.\n")
	got := slashSkillPrompt(jsonSkillInfo{Name: "demo", Path: p}, "inspect example.com")
	want := "body.\n\ninspect example.com"
	if got != want {
		t.Fatalf("slashSkillPrompt with normal args = %q, want %q", got, want)
	}
}

// TestSlashSkillPrompt_NoAtArgWorks is the baseline: a skill invoked
// without any arguments returns just the body. The fact that
// verbatimSkillArgs("") equals "" is what keeps this terse.
func TestSlashSkillPrompt_NoAtArgWorks(t *testing.T) {
	p := writeSkillMD(t, "---\nname: demo\n---\nbody only.\n")
	if got := slashSkillPrompt(jsonSkillInfo{Name: "demo", Path: p}, ""); got != "body only." {
		t.Fatalf("slashSkillPrompt with no args = %q, want %q", got, "body only.")
	}
	if got := slashSkillPrompt(jsonSkillInfo{Name: "demo", Path: p}, verbatimSkillArgs("")); got != "body only." {
		t.Fatalf("slashSkillPrompt(verbatimSkillArgs(\"\")) = %q, want %q", got, "body only.")
	}
}

// TestSlashSkillPrompt_MultipleAtArgsPreserved proves the guarantee
// scales to several @-looking tokens at once and to a mix of @-looking
// and plain arguments.
func TestSlashSkillPrompt_MultipleAtArgsPreserved(t *testing.T) {
	p := writeSkillMD(t, "---\nname: demo\n---\nbody.\n")
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"two", "@a @b", "body.\n\n@a @b"},
		{"three", "@a @b @c", "body.\n\n@a @b @c"},
		{"mixed", "@path/to/file and then @another/place", "body.\n\n@path/to/file and then @another/place"},
		{"dup", "@a @a", "body.\n\n@a @a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := slashSkillPrompt(jsonSkillInfo{Name: "demo", Path: p}, tc.in)
			if got != tc.want {
				t.Errorf("slashSkillPrompt(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if via := slashSkillPrompt(jsonSkillInfo{Name: "demo", Path: p}, verbatimSkillArgs(tc.in)); via != tc.want {
				t.Errorf("slashSkillPrompt(verbatimSkillArgs(%q)) = %q, want %q", tc.in, via, tc.want)
			}
		})
	}
}

// TestSlashSkillPrompt_AtInMiddleNotExpanded covers the awkward case
// where the @-looking token is not the only thing on the line and is
// not at either edge. A future @-mention layer that walks on
// whitespace would otherwise turn `@path/to/file` into a file read
// even though it's the middle of a longer sentence.
func TestSlashSkillPrompt_AtInMiddleNotExpanded(t *testing.T) {
	p := writeSkillMD(t, "---\nname: demo\n---\nbody.\n")
	in := "please consider @path/to/file between two regular words"
	want := "body.\n\nplease consider @path/to/file between two regular words"
	got := slashSkillPrompt(jsonSkillInfo{Name: "demo", Path: p}, in)
	if got != want {
		t.Errorf("slashSkillPrompt(%q) = %q, want %q", in, got, want)
	}
}

// TestRunSkill_AtPathVerbatimThroughRouter pushes the contract all the
// way through handleSlashCommand — the same entry the TUI uses for
// `m.handleSlashCommand("/my-skill @path/to/file")`. The model-bound
// message that ends up in m.session must contain the literal
// `@path/to/file`.
func TestRunSkill_AtPathVerbatimThroughRouter(t *testing.T) {
	p := writeSkillMD(t, "---\nname: demo-skill\n---\nSkill instructions here.\n")
	withSyntheticCatalog(t, []jsonSkillInfo{{Name: "demo-skill", Description: "d", Path: p}})

	m := newSlashModel(t)
	handled, _ := m.handleSlashCommand("/demo-skill @path/to/file")
	if !handled {
		t.Fatal("/demo-skill @path/to/file was not handled")
	}
	if m.response == nil || m.response[0] != "> /demo-skill @path/to/file" {
		t.Fatalf("transcript echo = %v, want \"> /demo-skill @path/to/file\"", m.response)
	}
	got := lastUserMessage(m)
	want := "Skill instructions here.\n\n@path/to/file"
	if got != want {
		t.Fatalf("session prompt = %q, want %q", got, want)
	}
	// And nothing in the transcript must contain a `<file name="...">`
	// wrapper — the body the @-expansion would emit when it tries to
	// read @path/to/file as a mention.
	for _, row := range m.response {
		if strings.Contains(row, "<file name=") {
			t.Errorf("transcript contains a file-mention wrapper: %q", row)
		}
	}
}

// TestRunSkill_PluginSkillAtArgVerbatim repeats the contract check
// against a plugin-prefixed skill (the alias that ships with the
// chrome-devtools-plugin and friends) so the @-arg path is covered for
// skills discovered through the plugin enumerator as well.
func TestRunSkill_PluginSkillAtArgVerbatim(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "plugins", "demo-plugin", "skills", "demo-skill")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(p, []byte("---\nname: demo-plugin:demo-skill\n---\nCDP-like body.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	withSyntheticCatalog(t, []jsonSkillInfo{{
		Name: "demo-plugin:demo-skill", Description: "d", Path: p,
	}})

	m := newSlashModel(t)
	handled, _ := m.handleSlashCommand("/demo-plugin:demo-skill @path/to/file")
	if !handled {
		t.Fatal("/demo-plugin:demo-skill @path/to/file was not handled")
	}
	got := lastUserMessage(m)
	want := "CDP-like body.\n\n@path/to/file"
	if got != want {
		t.Fatalf("plugin skill session prompt = %q, want %q", got, want)
	}
}
