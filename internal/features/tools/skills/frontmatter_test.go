// frontmatter_test.go — the skill frontmatter reader.
//
// The reader parses a SKILL.md's YAML frontmatter into the name,
// description and icon the reference's /skills surface shows. It moved
// out of the print path with the discovery itself: three surfaces read
// it (the print path's /skills, the TUI's /skills panel and the
// `<skill>` router), and one reader is what keeps them in step.
//
// 【一手】 agy 1.4.8: a probe skill whose frontmatter declares
// `metadata.icon: zzznotanicon` lists as `zzznotanicon zziconprobe` in
// the /skills panel and in the skill's detail header, and prints as
// `zziconprobe` in `agy -p /skills`.
package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSkillFrontmatterReadsIcon(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name+".md")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	withIcon := write("withicon", "---\nname: automation\ndescription: A guide.\nmetadata:\n  icon: schedule\n---\n\nbody\n")
	f := skillFrontmatterFields(withIcon)
	if f.Name != "automation" || f.Description != "A guide." || f.Icon != "schedule" {
		t.Errorf("withicon: name=%q desc=%q icon=%q", f.Name, f.Description, f.Icon)
	}

	noIcon := write("noicon", "---\nname: plain\ndescription: One line only.\n---\n\nbody\n")
	f = skillFrontmatterFields(noIcon)
	if f.Name != "plain" || f.Icon != "" {
		t.Errorf("noicon: name=%q icon=%q", f.Name, f.Icon)
	}

	// A `metadata` that is not a mapping must not cost the document its
	// name — the icon is decoded leniently on purpose.
	scalarMeta := write("scalarmeta", "---\nname: scalar\ndescription: One line only.\nmetadata: none\n---\n\nbody\n")
	f = skillFrontmatterFields(scalarMeta)
	if f.Name != "scalar" {
		t.Errorf("scalarmeta: name=%q", f.Name)
	}
}

// TestSkillFrontmatter calls the two-value wrapper on a SKILL.md with an
// icon, to pin that the wrapper keeps its old contract.
func TestSkillFrontmatter(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := dir + "/" + name + ".md"
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	plain := write("plain", "---\nname: plain\ndescription: One line only.\n---\n\nbody\n")
	n, d := skillFrontmatter(plain)
	if n != "plain" || d != "One line only." {
		t.Errorf("plain: name=%q desc=%q", n, d)
	}

	folded := write("folded", "---\nname: folded\ndescription: >-\n  Line one\n  line two.\n\n  Paragraph two.\n---\n")
	n, d = skillFrontmatter(folded)
	if n != "folded" {
		t.Errorf("folded: name=%q", n)
	}
	// YAML spec for a folded block: a blank line folds to a single `\n`,
	// not `\n\n`. The previous hand-rolled reader inserted an extra newline
	// at every paragraph break; the bug is fixed by moving to yaml.v3.
	if d != "Line one line two.\nParagraph two." {
		t.Errorf("folded: desc=%q", d)
	}

	literal := write("literal", "---\nname: literal\ndescription: |\n  Alpha\n  Beta\n---\n")
	n, d = skillFrontmatter(literal)
	if n != "literal" || d != "Alpha\nBeta" {
		t.Errorf("literal: name=%q desc=%q", n, d)
	}

	none := write("none", "no frontmatter here\n")
	if n, d := skillFrontmatter(none); n != "" || d != "" {
		t.Errorf("no-frontmatter: name=%q desc=%q", n, d)
	}
}

// TestSkillFrontmatterYAMLv3Only documents features yaml.v3 handles that the
// old hand-rolled reader could not: YAML 1.2 comments, escape sequences
// inside double-quoted scalars, anchors/aliases, and flow-style mappings.
// The clone does not rely on these today, but the migration makes them
// portable so a SKILL.md using them reads the way the reference would.
func TestSkillFrontmatterYAMLv3Only(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := dir + "/" + name + ".md"
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	// 1) A comment line and a double-quoted scalar with escape sequences.
	//    The previous reader kept `# trailing comment` and left the literal
	//    `\n` in the string.
	escapes := write("escapes",
		"---\n"+
			"name: escapes\n"+
			"description: \"two\\nlines\" # a comment\n"+
			"---\n")
	n, d := skillFrontmatter(escapes)
	if n != "escapes" {
		t.Errorf("escapes: name=%q", n)
	}
	if d != "two\nlines" {
		t.Errorf("escapes: desc=%q want %q", d, "two\nlines")
	}

	// 2) An anchor + alias pair — a single root name referenced twice.
	anchor := write("anchor",
		"---\n"+
			"base: &b \"shared\"\n"+
			"name: anchor\n"+
			"description: *b\n"+
			"---\n")
	n, d = skillFrontmatter(anchor)
	if n != "anchor" || d != "shared" {
		t.Errorf("anchor: name=%q desc=%q", n, d)
	}
}

// TestDispatchPrintSlash_Usage pins the /usage answer's shape. REF v1.4.0
// resolves it from a live weekly-quota service, so the clone emits the
// recovered payload with frozen placeholder figures (see
// printSlashUsagePayload): the format is byte-exact but the percentage and the
// reset timestamps are the values captured at one instant and cannot track the
// reference, which recomputes them on every run.
