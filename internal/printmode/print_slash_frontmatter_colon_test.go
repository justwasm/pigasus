// print_slash_frontmatter_colon_test.go — cycle `yaml-colon-description`
// regression coverage.
//
// AGY 1.3.6 changelog line 8: "Fixed agents with a colon in their
// description failing to parse due to YAML treating the colon as a key
// separator when the description is not properly quoted."
//
// yaml.v3 (the parser chosen in cycle 175) rejects plain-scalar values that
// contain a ": " (colon-space) sequence — the byte pair that YAML 1.2
// reserves for the `key:value` separator. skillFrontmatter pre-processes
// the frontmatter via normalizeFrontmatter so values like
// `description: Agent: helper` reach yaml.v3 as a double-quoted scalar
// instead of a plain one; block scalars ("|", ">-") and pre-quoted
// scalars are passed through untouched.
//
// These tests pin that behaviour so a future cycle that swaps the parser
// can't silently regress the corner cases.

package printmode

import (
	"os"
	"strings"
	"testing"
)

// TestSkillFrontmatterColonDescription is the user-visible contract that
// AGY 1.3.6 changelog line 8 promises: an unquoted description containing
// a colon-space sequence parses to the verbatim bytes the author wrote.
// All six cases the changelog brief calls out live here.
func TestSkillFrontmatterColonDescription(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := dir + "/" + name + ".md"
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	cases := []struct {
		label, file                string
		wantName, wantDesc         string
		wantParseKeepsNameNonEmpty bool
	}{
		{
			label:    "agent-helper",
			file:     write("agent", "---\nname: a\ndescription: Agent: helper\n---\n"),
			wantName: "a",
			wantDesc: "Agent: helper",
		},
		{
			label:    "uses-http",
			file:     write("http", "---\nname: h\ndescription: Uses HTTP and JSON\n---\n"),
			wantName: "h",
			wantDesc: "Uses HTTP and JSON",
		},
		{
			label:    "multiple-colons",
			file:     write("multi", "---\nname: m\ndescription: Step 1: do X. Step 2: do Y.\n---\n"),
			wantName: "m",
			wantDesc: "Step 1: do X. Step 2: do Y.",
		},
		{
			label:    "leading-colon",
			file:     write("lead", "---\nname: l\ndescription: ': leading colon'\n---\n"),
			wantName: "l",
			wantDesc: ": leading colon",
		},
		{
			label:    "already-quoted",
			file:     write("quot", "---\nname: q\ndescription: \"Already: quoted\"\n---\n"),
			wantName: "q",
			wantDesc: "Already: quoted",
		},
		{
			label:    "empty-description",
			file:     write("empty", "---\nname: e\ndescription: \"\"\n---\n"),
			wantName: "e",
			wantDesc: "",
		},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			n, d := skillFrontmatter(c.file)
			if n != c.wantName {
				t.Errorf("skillFrontmatter(%q) name = %q, want %q", c.file, n, c.wantName)
			}
			if d != c.wantDesc {
				t.Errorf("skillFrontmatter(%q) description = %q, want %q", c.file, d, c.wantDesc)
			}
		})
	}
}

// TestNormalizeFrontmatter is the unit-level test for the preprocessor.
// It pins the boundary the preprocessor observes without going through
// skillFrontmatter's whole-file scaffolding, so a future cycle that
// rewrites the parser integration can confirm the preprocessing still
// does what we documented.
func TestNormalizeFrontmatter(t *testing.T) {
	cases := []struct {
		label, in, want string
	}{
		{
			label: "plain-passes-through",
			in:    "name: plain\ndescription: One line only.\n",
			want:  "name: plain\ndescription: One line only.\n",
		},
		{
			label: "colon-end-quoted",
			in:    "name: c\ndescription: Agent: the helpful assistant\n",
			want:  "name: c\ndescription: \"Agent: the helpful assistant\"\n",
		},
		{
			label: "already-quoted-passes-through",
			in:    "name: c\ndescription: \"Already: quoted\"\n",
			want:  "name: c\ndescription: \"Already: quoted\"\n",
		},
		{
			label: "single-quoted-passes-through",
			in:    "name: c\ndescription: 'Already: quoted'\n",
			want:  "name: c\ndescription: 'Already: quoted'\n",
		},
		{
			label: "block-scalar-passes-through",
			in:    "name: c\ndescription: >-\n  Line one\n  line two.\n",
			want:  "name: c\ndescription: >-\n  Line one\n  line two.\n",
		},
		{
			label: "literal-block-passes-through",
			in:    "name: c\ndescription: |\n  Alpha\n  Beta\n",
			want:  "name: c\ndescription: |\n  Alpha\n  Beta\n",
		},
		{
			label: "multiline-plain-quoted-and-folded",
			in:    "name: c\ndescription: First line\n  continues here: with colon\n",
			want:  "name: c\ndescription: \"First line continues here: with colon\"\n",
		},
		{
			label: "no-colon-multiline-passes-through",
			in:    "name: c\ndescription: First line\n  continues here\n",
			want:  "name: c\ndescription: First line\n  continues here\n",
		},
		{
			label: "comment-passes-through",
			in:    "name: c\n# a comment line\ndescription: Agent: helper\n",
			want:  "name: c\n# a comment line\ndescription: \"Agent: helper\"\n",
		},
		{
			label: "sequence-value-passes-through",
			in:    "name: c\ndescription:\n  - one\n  - two\n",
			want:  "name: c\ndescription:\n  - one\n  - two\n",
		},
		{
			label: "flow-mapping-passes-through",
			in:    "name: c\ndescription: {a: b}\n",
			want:  "name: c\ndescription: {a: b}\n",
		},
		{
			label: "double-quote-inside-value-is-escaped",
			// The double-quote inside the value has to come back out
			// unescaped after the YAML decoder un-marshals, so the
			// preprocessor escapes it.
			in:   "name: c\ndescription: He said \"hello\": loudly\n",
			want: "name: c\ndescription: \"He said \\\"hello\\\": loudly\"\n",
		},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			got := string(normalizeFrontmatter([]byte(c.in)))
			if got != c.want {
				t.Errorf("normalizeFrontmatter(%q):\ngot:  %s\nwant: %s", c.in, got, c.want)
			}
		})
	}
}

// TestNeedsDoubleQuoting pins the predicate that decides which values the
// preprocessor quotes. YAML's reserved indicators are checked explicitly
// because each one changes the meaning of the line (`|` starts a block
// literal, `>` starts a block folded, `[` opens a flow sequence).
func TestNeedsDoubleQuoting(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"Agent: helper", true},
		{"Step 1: do X. Step 2: do Y.", true},
		{"http://example.com/path", false}, // no colon-space
		{"plain text", false},
		{"\"Already quoted\"", false},
		{"'single quoted'", false},
		{"|", false},
		{">-", false},
		{"[a, b]", false},
		{"{a: b}", false}, // no `:` followed by space here; colon is `{` flow context
		{"&anchor", false},
		{"*alias", false},
		{"!tag", false},
		{"-foo", false},
		{"", false},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got := needsDoubleQuoting([]byte(c.in))
			if got != c.want {
				t.Errorf("needsDoubleQuoting(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// TestEscapeDoubleQuoted pins the escape rules. The preprocessor emits
// these escapes as the only byte-level difference between
// `description: foo: bar` (which would be rejected) and the auto-quoted
// form that yaml.v3 round-trips back to the original bytes.
func TestEscapeDoubleQuoted(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`plain`, `plain`},
		{`with "quote"`, `with \"quote\"`},
		{`with \ slash`, `with \\ slash`},
		{"with\nnewline", `with\nnewline`},
		{"with\ttab", `with\ttab`},
		{"with\rcr", `with\rcr`},
		{"with\u0000null", `with\0null`},
		{"with\u0007bel", `with\x07bel`},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got := string(escapeDoubleQuoted([]byte(c.in)))
			if got != c.want {
				t.Errorf("escapeDoubleQuoted(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestSkillFrontmatterYAMLv3OnlyStillPasses is a regression guard for the
// cycle-175 yaml.v3 migration. After the colon fix the round-trip cases
// (comments, escape sequences, anchors, folded / literal blocks) must
// keep the same answers they had before — the preprocessor is supposed to
// be a no-op for everything except the plain-scalar ": " case.
func TestSkillFrontmatterYAMLv3OnlyStillPasses(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := dir + "/" + name + ".md"
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	escapes := write("escapes",
		"---\n"+
			"name: escapes\n"+
			"description: \"two\\nlines\" # a comment\n"+
			"---\n")
	n, d := skillFrontmatter(escapes)
	if n != "escapes" || d != "two\nlines" {
		t.Errorf("escapes: name=%q desc=%q", n, d)
	}

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

	folded := write("folded",
		"---\n"+
			"name: folded\n"+
			"description: >-\n"+
			"  Line one\n"+
			"  line two.\n"+
			"\n"+
			"  Paragraph two.\n"+
			"---\n")
	n, d = skillFrontmatter(folded)
	if n != "folded" {
		t.Errorf("folded name: %q", n)
	}
	if want := "Line one line two.\nParagraph two."; d != want {
		t.Errorf("folded desc: %q want %q", d, want)
	}

	literal := write("literal",
		"---\n"+
			"name: literal\n"+
			"description: |\n"+
			"  Alpha\n"+
			"  Beta\n"+
			"---\n")
	n, d = skillFrontmatter(literal)
	if n != "literal" {
		t.Errorf("literal name: %q", n)
	}
	if !strings.HasPrefix(d, "Alpha\nBeta") {
		t.Errorf("literal desc: %q want Alpha\\nBeta prefix", d)
	}
}
