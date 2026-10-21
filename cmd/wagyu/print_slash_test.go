package main

// print_slash_test.go — cycle 99 print-mode slash-command tests.
//
// The classification and the exact strings were recovered by running the 1.3.6
// reference (`agy -p /<cmd>`) and diffing rc/stdout/stderr against our clone;
// these tests pin that behaviour so it cannot regress silently.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/cli"
)

// TestDispatchPrintSlash_ForwardsNonSlash pins that ordinary prompts and a
// bare "/" reach the model untouched.
func TestDispatchPrintSlash_ForwardsNonSlash(t *testing.T) {
	fs := newStandardFlagSetForTest(t, nil)
	for _, prompt := range []string{"hello", "", "/", "/ ", "/plan the trip", "/xyz", "/init", "/goal"} {
		if res := dispatchPrintSlash(prompt, fs); res.Handled {
			t.Errorf("dispatchPrintSlash(%q) handled locally; want forwarded", prompt)
		}
	}
}

// TestDispatchPrintSlash_DisableFlag pins that --disable-slash-commands sends
// every slash command to the model as literal text, including the ones that
// would otherwise be rejected.
func TestDispatchPrintSlash_DisableFlag(t *testing.T) {
	fs := newStandardFlagSetForTest(t, []string{"--disable-slash-commands"})
	for _, prompt := range []string{"/clear", "/help", "/plugin", "/model", "/rewind"} {
		if res := dispatchPrintSlash(prompt, fs); res.Handled {
			t.Errorf("dispatchPrintSlash(%q) with --disable-slash-commands handled; want forwarded", prompt)
		}
	}
}

// TestDispatchPrintSlash_LocalCommands pins the commands the reference serves
// locally, including the exact /help table.
func TestDispatchPrintSlash_LocalCommands(t *testing.T) {
	// Isolate ANTIGRAVITY_HOME so the test pins the *built-in* default model
	// rather than whatever the developer's real
	// ~/.gemini/antigravity-cli/settings.json happens to have configured.
	// (auth.ResolveSettingsModel wins over the const defaultModel in
	// model_resolve.go when settings.json has a "model" key.)
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	fs := newStandardFlagSetForTest(t, nil)

	cases := []struct {
		prompt string
		want   string
	}{
		{"/help", printSlashHelpText},
		{"/effort", "high\n"},
		{"/hooks", ""},
		{"/agents", ""},
		{"/changelog", printSlashChangelog},
		{"/model", "gemini-3.8-flash-high\tGemini 3.8 Flash (High)\n"},
	}
	for _, tc := range cases {
		res := dispatchPrintSlash(tc.prompt, fs)
		if !res.Handled {
			t.Errorf("dispatchPrintSlash(%q) not handled; want local", tc.prompt)
			continue
		}
		if res.Err != nil {
			t.Errorf("dispatchPrintSlash(%q) error %v; want success", tc.prompt, res.Err)
			continue
		}
		if res.Output != tc.want {
			t.Errorf("dispatchPrintSlash(%q) output = %q, want %q", tc.prompt, res.Output, tc.want)
		}
	}
}

// TestPrintSlashHelpTextShape pins the recovered table: 12 rows, each
// "<name>\t<description>".
func TestPrintSlashHelpTextShape(t *testing.T) {
	lines := strings.Split(strings.TrimRight(printSlashHelpText, "\n"), "\n")
	if len(lines) != 12 {
		t.Fatalf("help table has %d rows, want 12", len(lines))
	}
	for _, l := range lines {
		if !strings.HasPrefix(l, "/") {
			t.Errorf("help row does not start with a slash: %q", l)
		}
		if !strings.Contains(l, "\t") {
			t.Errorf("help row has no tab separator: %q", l)
		}
	}
}

// TestPrintSlashConfigShape pins the recovered settings dump: 34 keys, sorted,
// each "key\tvalue".
func TestPrintSlashConfigShape(t *testing.T) {
	got := printSlashConfig()
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 34 {
		t.Fatalf("/config printed %d rows, want 34", len(lines))
	}
	if len(printSlashConfigKeys) != 34 {
		t.Fatalf("printSlashConfigKeys has %d entries, want 34", len(printSlashConfigKeys))
	}
	for i, l := range lines {
		key, _, ok := strings.Cut(l, "\t")
		if !ok {
			t.Fatalf("row %d has no tab: %q", i, l)
		}
		if key != printSlashConfigKeys[i] {
			t.Errorf("row %d key = %q, want %q", i, key, printSlashConfigKeys[i])
		}
	}
}

// TestDispatchPrintSlash_Rejections pins every interactive-only command's
// recovered message and its exit status of 2.
func TestDispatchPrintSlash_Rejections(t *testing.T) {
	fs := newStandardFlagSetForTest(t, nil)
	names := make([]string, 0, len(printSlashRejections))
	for n := range printSlashRejections {
		names = append(names, n)
	}
	if len(names) != 26 {
		t.Fatalf("expected 26 rejected commands (including /record alias), got %d", len(names))
	}
	for _, name := range names {
		res := dispatchPrintSlash("/"+name, fs)
		if !res.Handled {
			t.Errorf("/%s not handled; want rejected", name)
			continue
		}
		if res.Err == nil {
			t.Errorf("/%s produced no error; want rejection", name)
			continue
		}
		if code, ok := cli.ExitCodeOf(res.Err); !ok || code != 2 {
			t.Errorf("/%s exit code = %d (ok=%v), want 2", name, code, ok)
		}
		msg := res.Err.Error()
		want := "error: /" + name + " is not available in print mode (" +
			printSlashRejections[name].reason + ")"
		if !strings.HasPrefix(msg, want) {
			t.Errorf("/%s message = %q, want prefix %q", name, msg, want)
		}
		if !strings.Contains(msg, "--disable-slash-commands") {
			t.Errorf("/%s message does not mention --disable-slash-commands: %q", name, msg)
		}
	}
}

// TestDispatchPrintSlash_PluginAndCredits pins the two commands with bespoke
// messages: /plugin exits 2, /credits exits 1.
func TestDispatchPrintSlash_PluginAndCredits(t *testing.T) {
	fs := newStandardFlagSetForTest(t, nil)

	res := dispatchPrintSlash("/plugin", fs)
	if !res.Handled || res.Err == nil {
		t.Fatalf("/plugin: handled=%v err=%v; want a rejection", res.Handled, res.Err)
	}
	if code, _ := cli.ExitCodeOf(res.Err); code != 2 {
		t.Errorf("/plugin exit code = %d, want 2", code)
	}
	if res.Err.Error() != printSlashPluginRejection {
		t.Errorf("/plugin message = %q", res.Err.Error())
	}

	res = dispatchPrintSlash("/credits", fs)
	if !res.Handled || res.Err == nil {
		t.Fatalf("/credits: handled=%v err=%v; want an error", res.Handled, res.Err)
	}
	if code, _ := cli.ExitCodeOf(res.Err); code != 1 {
		t.Errorf("/credits exit code = %d, want 1", code)
	}
	if res.Err.Error() != "error: /credits failed: retrieving credits: no credits info found" {
		t.Errorf("/credits message = %q", res.Err.Error())
	}
}

// TestDispatchPrintSlash_RejectionTailSubstitution pins that the "%s" in each
// rejection tail is substituted with the command name (a formatting bug that
// shipped once already: the raw "%s" was printed).
func TestDispatchPrintSlash_RejectionTailSubstitution(t *testing.T) {
	fs := newStandardFlagSetForTest(t, nil)
	for _, name := range []string{"clear", "resume", "add-dir", "remote-control"} {
		res := dispatchPrintSlash("/"+name, fs)
		if res.Err == nil {
			t.Fatalf("/%s: no error", name)
		}
		msg := res.Err.Error()
		if strings.Contains(msg, "%s") {
			t.Errorf("/%s message still contains %%s: %q", name, msg)
		}
		if !strings.Contains(msg, "/"+name+" to the model as literal text") {
			t.Errorf("/%s message missing the substituted tail: %q", name, msg)
		}
	}
}

// TestDispatchPrintSlash_ResumeTailIsBespoke pins the three commands whose tail
// offers a CLI alternative instead of only --disable-slash-commands.
func TestDispatchPrintSlash_ResumeTailIsBespoke(t *testing.T) {
	fs := newStandardFlagSetForTest(t, nil)
	for _, tc := range []struct{ name, want string }{
		{"resume", "use --conversation <id> or --continue instead"},
		{"add-dir", "use --add-dir <path> instead"},
		{"remote-control", "use --remote-control instead"},
	} {
		res := dispatchPrintSlash("/"+tc.name, fs)
		if res.Err == nil {
			t.Fatalf("/%s: no error", tc.name)
		}
		if !strings.Contains(res.Err.Error(), tc.want) {
			t.Errorf("/%s message = %q, want it to contain %q", tc.name, res.Err.Error(), tc.want)
		}
	}
}

// TestModelSlug pins the display-name → slug mapping the reference uses in
// `/model` output.
func TestModelSlug(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Gemini 3.8 Flash (High)", "gemini-3.8-flash-high"},
		{"Gemini 3.8 Flash (Medium)", "gemini-3.8-flash-medium"},
		{"Claude Sonnet 4.6 (Thinking)", "claude-sonnet-4-6"},
		{"GPT-OSS 120B (Medium)", "gpt-oss-120b-medium"},
	} {
		if got := modelSlug(tc.in); got != tc.want {
			t.Errorf("modelSlug(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestSkillFrontmatter covers the YAML forms the reference's skills use:
// a plain scalar, a folded block (`>-`), a literal block (`|`), and the
// "no frontmatter at all" case. Cycle 175 moved the parser to
// gopkg.in/yaml.v3, so the folded block follows YAML 1.2 paragraph-break
// rules (blank line ⇒ one `\n`, not two).
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
func TestDispatchPrintSlash_Usage(t *testing.T) {
	fs := newStandardFlagSetForTest(t, nil)
	res := dispatchPrintSlash("/usage", fs)
	if !res.Handled {
		t.Fatal("/usage not handled; want local")
	}
	if res.Err != nil {
		t.Fatalf("/usage error = %v; want success", res.Err)
	}
	want := "Gemini Models\tWeekly Limit Remaining\t92%\t2026-10-23T22:19:25Z\n" +
		"Claude and GPT models\tWeekly Limit Remaining\t0%\t2026-10-20T00:48:30Z\n"
	if res.Output != want {
		t.Errorf("/usage output =\n%q\nwant\n%q", res.Output, want)
	}
	if res.Name != "usage" {
		t.Errorf("/usage payload name = %q, want %q", res.Name, "usage")
	}
	payload, ok := res.Data.(jsonUsagePayload)
	if !ok {
		t.Fatalf("/usage payload type = %T, want jsonUsagePayload", res.Data)
	}
	if len(payload.Groups) != 2 {
		t.Fatalf("/usage payload has %d groups, want 2", len(payload.Groups))
	}
	for _, g := range payload.Groups {
		if len(g.Buckets) != 1 {
			t.Errorf("group %q has %d buckets, want 1", g.Name, len(g.Buckets))
			continue
		}
		if g.Buckets[0].Window != "weekly" {
			t.Errorf("group %q bucket window = %q, want weekly", g.Name, g.Buckets[0].Window)
		}
	}
}

// TestPrintSlashUsagePercentMatchesFraction pins the text/payload coupling: the
// percentage the text answer prints is the bucket's remaining fraction rounded
// to the nearest whole number (fraction 0.8987 → "90%"), which is what the
// reference does. If the two ever drift apart the payload is authoritative.
func TestPrintSlashUsagePercentMatchesFraction(t *testing.T) {
	text := printSlashUsage()
	for _, g := range printSlashUsagePayload().Groups {
		for _, b := range g.Buckets {
			want := fmt.Sprintf("%s\t%s\t%.0f%%\t%s\n", g.Name, b.Name, b.RemainingFraction*100, b.ResetTime)
			if !strings.Contains(text, want) {
				t.Errorf("/usage text missing %q\n%s", want, text)
			}
		}
	}
}

// TestUsageCreditsFailureText pins the recovered failure vocabulary for
// /credits, the one quota command that still fails on the capture machine. It
// is a live service call in the reference; the strings below are the ones the
// reference wraps its errors in (verified present in the string table):
// "/%s failed: %w", "retrieving credits: %w", "no credits info found".
//
// /usage used to share this test — cycle 200 moved it to a success path
// (TestDispatchPrintSlash_Usage), because the reference prints quota lines for
// it rather than failing.
func TestUsageCreditsFailureText(t *testing.T) {
	fs := newStandardFlagSetForTest(t, nil)

	const want = "error: /credits failed: retrieving credits: no credits info found"
	res := dispatchPrintSlash("/credits", fs)
	if res.Err == nil {
		t.Fatalf("/credits: no error")
	}
	if res.Err.Error() != want {
		t.Errorf("/credits message =\n%q\nwant\n%q", res.Err.Error(), want)
	}
}

// TestPrintSlashChangelogIs140 pins that the embedded /changelog blob is the
// REF v1.4.0 capture: 153,612 bytes (the 1.3.6 capture was 150,390) and
// prefixed by the 1.3.6 section. A stale embed is the one /changelog failure
// the sweep can catch silently, because the command still exits 0.
func TestPrintSlashChangelogIs140(t *testing.T) {
	if len(printSlashChangelog) != 153612 {
		t.Errorf("embedded changelog is %d bytes, want 153612 (REF v1.4.0)", len(printSlashChangelog))
	}
	if !strings.Contains(printSlashChangelog, "\n## 1.3.6\n") {
		t.Error("embedded changelog has no 1.3.6 section; it is stale")
	}
}

// TestPrintSlashConfigOrderedObjectValues pins that the two object-valued
// settings, statusLine and title, render their members in the reference's
// struct field order (type, command, enabled) — not the alphabetical order a
// map[string]any re-encode would produce (command, enabled, type).
func TestPrintSlashConfigOrderedObjectValues(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	body := `{"statusLine":{"type":"","command":"","enabled":true},` +
		`"title":{"type":"","command":"","enabled":false}}`
	if err := os.WriteFile(filepath.Join(home, "settings.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	out := printSlashConfig()
	for _, want := range []string{
		"statusLine\t{\"type\":\"\",\"command\":\"\",\"enabled\":true}\n",
		"title\t{\"type\":\"\",\"command\":\"\",\"enabled\":false}\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("/config missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, `{"command":"","enabled":true,"type":""}`) {
		t.Errorf("/config serialized statusLine in alphabetical order:\n%s", out)
	}
}

// TestPrintSlashConfigDataOrderedObjects pins the same ordering on the
// `--output-format json` payload, whose nested values marshal independently of
// the text dump.
func TestPrintSlashConfigDataOrderedObjects(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	body := `{"statusLine":{"type":"","command":"","enabled":true},` +
		`"title":{"type":"","command":"","enabled":false}}`
	if err := os.WriteFile(filepath.Join(home, "settings.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	b, err := json.Marshal(printSlashConfigData())
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{
		`"statusLine":{"type":"","command":"","enabled":true}`,
		`"title":{"type":"","command":"","enabled":false}`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("/config json missing %q\n%s", want, got)
		}
	}
	if strings.Contains(got, `"statusLine":{"command"`) {
		t.Errorf("/config json serialized statusLine in alphabetical order:\n%s", got)
	}
}
