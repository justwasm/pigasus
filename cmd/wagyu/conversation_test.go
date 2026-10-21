package main

import (
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// conversationFlagSet builds the flags resolveConversationSelection reads.
func conversationFlagSet(t *testing.T, args ...string) *flag.FlagSet {
	t.Helper()
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.String("conversation", "", "")
	fs.Bool("continue", false, "")
	fs.Bool("c", false, "")
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	return fs
}

// storeFixture points $ANTIGRAVITY_HOME at a fresh directory and copies the
// convstore test fixture in as conversations/<id>.db for each id.
func storeFixture(t *testing.T, ids ...string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "conversations"), 0o755); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile(filepath.Join("..", "..", "internal", "convstore", "testdata", "transcript.db"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	for _, id := range ids {
		if err := os.WriteFile(filepath.Join(dir, "conversations", id+".db"), fixture, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func writeCache(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cache", name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const (
	fixtureID = "cafe0000-0000-4000-8000-000000000001"
	otherID   = "cafe0000-0000-4000-8000-000000000009"
)

func TestConversationFlagAdoptsExisting(t *testing.T) {
	storeFixture(t, fixtureID)
	var warn strings.Builder
	sel := resolveConversationSelection(conversationFlagSet(t, "--conversation="+fixtureID), &warn)

	if sel.ID != fixtureID || sel.Source != "flag" {
		t.Fatalf("selection = %+v", sel)
	}
	if warn.String() != "" {
		t.Errorf("unexpected warning: %q", warn.String())
	}
	if sel.Store == nil || len(sel.Store.Steps) != 9 {
		t.Fatalf("store not loaded: %+v", sel.Store)
	}
	// The fixture holds two user turns.
	if sel.PriorUserTurns != 2 {
		t.Errorf("PriorUserTurns = %d, want 2", sel.PriorUserTurns)
	}
	if len(sel.History) == 0 || sel.History[0].Role != "user" {
		t.Errorf("history = %+v", sel.History)
	}
}

func TestConversationFlagMissingWarns(t *testing.T) {
	storeFixture(t)
	var warn strings.Builder
	const bogus = "deadbeef-0000-4000-8000-000000000000"
	sel := resolveConversationSelection(conversationFlagSet(t, "--conversation="+bogus), &warn)

	if sel.ID != "" {
		t.Errorf("a missing conversation must not be adopted: %+v", sel)
	}
	// Recovered verbatim from agy 1.3.6.
	want := "warning: conversation \"deadbeef-0000-4000-8000-000000000000\" not found\n"
	if warn.String() != want {
		t.Errorf("warning = %q, want %q", warn.String(), want)
	}
}

func TestConversationFlagEmptyIsSilent(t *testing.T) {
	storeFixture(t)
	var warn strings.Builder
	sel := resolveConversationSelection(conversationFlagSet(t, "--conversation="), &warn)
	if sel.ID != "" || warn.String() != "" {
		t.Errorf("empty --conversation: sel=%+v warn=%q", sel, warn.String())
	}
}

func TestConversationFlagLastWins(t *testing.T) {
	storeFixture(t, fixtureID, otherID)
	var warn strings.Builder
	sel := resolveConversationSelection(conversationFlagSet(t,
		"--conversation="+otherID, "--conversation="+fixtureID), &warn)
	if sel.ID != fixtureID {
		t.Errorf("ID = %q, want the last flag (%q)", sel.ID, fixtureID)
	}
}

func TestConversationFlagIsCaseSensitive(t *testing.T) {
	storeFixture(t, fixtureID)
	var warn strings.Builder
	sel := resolveConversationSelection(conversationFlagSet(t,
		"--conversation="+strings.ToUpper(fixtureID)), &warn)
	if sel.ID != "" || !strings.Contains(warn.String(), "not found") {
		t.Errorf("uppercase id must not match: sel=%+v warn=%q", sel, warn.String())
	}
}

func TestConversationFlagAcceptsZeroByteFile(t *testing.T) {
	dir := storeFixture(t)
	if err := os.WriteFile(filepath.Join(dir, "conversations", otherID+".db"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	var warn strings.Builder
	sel := resolveConversationSelection(conversationFlagSet(t, "--conversation="+otherID), &warn)
	if sel.ID != otherID || warn.String() != "" {
		t.Errorf("zero-byte conversation: sel=%+v warn=%q", sel, warn.String())
	}
	if len(sel.History) != 0 {
		t.Errorf("a zero-byte store has no history, got %d turns", len(sel.History))
	}
}

func TestConversationFlagRejectsDirectory(t *testing.T) {
	dir := storeFixture(t)
	if err := os.Mkdir(filepath.Join(dir, "conversations", otherID+".db"), 0o755); err != nil {
		t.Fatal(err)
	}
	var warn strings.Builder
	sel := resolveConversationSelection(conversationFlagSet(t, "--conversation="+otherID), &warn)
	if sel.ID != "" || !strings.Contains(warn.String(), "not found") {
		t.Errorf("a directory must not count as a conversation: sel=%+v warn=%q", sel, warn.String())
	}
}

func TestContinueUsesRecordedLastConversation(t *testing.T) {
	dir := storeFixture(t, fixtureID)
	work := t.TempDir()
	t.Chdir(work)
	writeCache(t, dir, "last_conversations.json", `{"`+work+`":"`+fixtureID+`"}`)

	sel := resolveConversationSelection(conversationFlagSet(t, "-c"), io.Discard)
	if sel.ID != fixtureID || sel.Source != "continue" {
		t.Fatalf("selection = %+v", sel)
	}
}

func TestContinueInheritsParentWorkspace(t *testing.T) {
	dir := storeFixture(t, fixtureID)
	work := t.TempDir()
	sub := filepath.Join(work, "nested", "deeper")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	// Only the parent workspace is recorded. The reference continues it from
	// a subdirectory (verified live: /tmp/agyconvprobe continued /tmp's
	// conversation).
	writeCache(t, dir, "last_conversations.json", `{"`+work+`":"`+fixtureID+`"}`)

	sel := resolveConversationSelection(conversationFlagSet(t, "--continue"), io.Discard)
	if sel.ID != fixtureID {
		t.Fatalf("selection = %+v, want the parent workspace's conversation", sel)
	}
}

func TestContinueFallsBackToMetadataCache(t *testing.T) {
	dir := storeFixture(t, fixtureID, otherID)
	work := t.TempDir()
	t.Chdir(work)
	// No last_conversations.json at all: the metadata cache decides.
	writeCache(t, dir, "conversation_metadata.json", `{"conversations":{
		"`+fixtureID+`":{"summary":{"ID":"`+fixtureID+`","WorkspaceURIs":["file://`+work+`"]},
			"last_modified_time":"2026-10-16T10:00:00+08:00"},
		"`+otherID+`":{"summary":{"ID":"`+otherID+`","WorkspaceURIs":["file://`+work+`"]},
			"last_modified_time":"2026-10-16T12:00:00+08:00"}}}`)

	sel := resolveConversationSelection(conversationFlagSet(t, "-c"), io.Discard)
	if sel.ID != otherID {
		t.Fatalf("selection = %+v, want the most recently modified conversation (%q)", sel, otherID)
	}
}

func TestContinuePrefersNearestWorkspace(t *testing.T) {
	dir := storeFixture(t, fixtureID, otherID)
	work := t.TempDir()
	sub := filepath.Join(work, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	writeCache(t, dir, "conversation_metadata.json", `{"conversations":{
		"`+fixtureID+`":{"summary":{"ID":"`+fixtureID+`","WorkspaceURIs":["file://`+work+`"]},
			"last_modified_time":"2026-10-16T23:00:00+08:00"},
		"`+otherID+`":{"summary":{"ID":"`+otherID+`","WorkspaceURIs":["file://`+sub+`"]},
			"last_modified_time":"2026-10-16T09:00:00+08:00"}}}`)

	sel := resolveConversationSelection(conversationFlagSet(t, "-c"), io.Discard)
	if sel.ID != otherID {
		t.Fatalf("selection = %+v, want the subdirectory's own workspace (%q)", sel, otherID)
	}
}

func TestContinueWithNothingToContinue(t *testing.T) {
	storeFixture(t)
	t.Chdir(t.TempDir())
	var warn strings.Builder
	sel := resolveConversationSelection(conversationFlagSet(t, "-c"), &warn)
	if sel.ID != "" || sel.Source != "" {
		t.Errorf("selection = %+v, want empty", sel)
	}
	// The clone prints nothing here: the reference's output for this case is
	// unrecovered, so inventing a message would be a guess.
	if warn.String() != "" {
		t.Errorf("warning = %q, want none", warn.String())
	}
}

func TestSeedConversationRendersTranscript(t *testing.T) {
	storeFixture(t, fixtureID)
	sel := resolveConversationSelection(conversationFlagSet(t, "--conversation="+fixtureID), io.Discard)
	if sel.Store == nil {
		t.Fatal("no store loaded")
	}
	m := NewRunMode(nil, "", "Gemini 3.8 Flash (High)", "", "/tmp", true)
	m.seedConversation(sel.Store)

	joined := strings.Join(m.response, "\n")
	// Cycle 271: the AGY 1.4.2 resumed transcript renders only model
	// steps; the user prompt is seeded into m.session below but not
	// echoed as a "> " row.
	if strings.Contains(joined, userPromptRow("summarise the repository layout")) {
		t.Errorf("cycle 271: resumed transcript must not echo user prompts:\n%s", joined)
	}
	if !strings.Contains(joined, "I will start by listing the top level.") {
		t.Errorf("missing the first reply:\n%s", joined)
	}
	if !strings.Contains(joined, "Ran") || !strings.Contains(joined, "ls") {
		t.Errorf("missing the tool step title:\n%s", joined)
	}
	if !strings.Contains(joined, "Done: internal/ has these packages.") {
		t.Errorf("missing the last reply:\n%s", joined)
	}
	// The subagent and empty-generation steps must not appear as rows.
	if strings.Contains(joined, "quota reached") == false {
		t.Errorf("the stored error step should be rendered:\n%s", joined)
	}
	if len(m.blocks) == 0 {
		t.Error("tool steps should register a ctrl+o block")
	}
	if len(m.session) == 0 {
		t.Error("the seeded transcript should seed the model session")
	}
}

func TestSeedConversationNilIsSafe(t *testing.T) {
	m := NewRunMode(nil, "", "Gemini 3.8 Flash (High)", "", "/tmp", true)
	m.seedConversation(nil)
	if len(m.response) != 0 {
		t.Errorf("nil store produced %d rows", len(m.response))
	}
}

// Cycle 128: inline backtick styling. A line with one paired run
// (“like this“) gets the cInlineCodeOpen/Close escapes wrapping
// the contents. Multiple pairs each get their own pair. Unpaired
// backticks pass through unchanged so tool output (which often has
// stray ` characters) is untouched.
func TestStyleInlineBackticks_BasicAndMulti(t *testing.T) {
	got := styleInlineBackticks("use `go test ./...` to verify")
	if !strings.Contains(got, cInlineCodeOpen+"go test ./..."+cInlineCodeClose) {
		t.Errorf("single inline span missing chip style: %q", got)
	}
	if strings.HasPrefix(got, cInlineCodeOpen) || strings.HasSuffix(got, cInlineCodeClose) {
		t.Errorf("non-code text should be plain: %q", got)
	}
	got2 := styleInlineBackticks("`a` and `b` and `c`")
	if strings.Count(got2, cInlineCodeOpen) != 3 || strings.Count(got2, cInlineCodeClose) != 3 {
		t.Errorf("three pairs: opens/closes each 3, got %q", got2)
	}
}

// Cycle 128: unbalanced / oversized lines pass through unchanged.
func TestStyleInlineBackticks_UnbalancedPassthrough(t *testing.T) {
	unbal := "use `go test ./..."
	if got := styleInlineBackticks(unbal); got != unbal {
		t.Errorf("unbalanced line should pass through: %q", got)
	}
	lot := "`a` `b` `c` `d` `e` `f` `g` `h` `i` rest"
	if got := styleInlineBackticks(lot); got != lot {
		t.Errorf("> 8 pairs should pass through: %q", got)
	}
	tool := "  $ ls /tmp/foo`bar`/`"
	got := styleInlineBackticks(tool)
	if got != tool {
		t.Errorf("stray backticks in tool output should pass through: %q", tool)
	}
	if got := styleInlineBackticks("hello world"); got != "hello world" {
		t.Errorf("plain text should pass through: %q", got)
	}
}

// Cycle 128: model-reply paths apply styleInlineBackticks.
func TestConversation_ModelReplyAppliesInlineStyle(t *testing.T) {
	m := NewRunMode(nil, "u@t", "m", "t", "/tmp", true)
	m.response = append(m.response, styleInlineBackticks("use `fmt.Println` here"))
	if !strings.Contains(m.response[0], cInlineCodeOpen+"fmt.Println"+cInlineCodeClose) {
		t.Errorf("expected inline chip in model reply, got %q", m.response[0])
	}
}

// Cycle 175: renderMarkdown now delegates to internal/mdrender
// (cycle 168, refined 174 / 175). The empty-input passthrough is the
// only test the cycle-155 hand-rolled pipeline explicitly pinned;
// the rest of this file exercises end-to-end paths through
// seedConversation that already pin the rendered output shape.
func TestRenderMarkdown_EmptyPassthrough(t *testing.T) {
	if got := renderMarkdown("", 0); got != "" {
		t.Errorf("renderMarkdown(\"\", 0) = %q, want \"\"", got)
	}
}

// Cycle 175: bullets get the U+2022 round-dot marker (cycle-155
// reference behaviour) — confirming mdrender's ListItem walker
// emits `\u2421 ` instead of `- `.
func TestRenderMarkdown_BulletConversion(t *testing.T) {
	got := renderMarkdown("- one\n- two\n- three", 0)
	if !strings.Contains(got, "\u2022 one") {
		t.Errorf("bullet marker lost: %q", got)
	}
	// The original `- ` must NOT survive in the output (cycle-155
	// rewrote it to `• ` and mdrender preserves that).
	if strings.Contains(got, "- one\n") || strings.Contains(got, "- two") {
		t.Errorf("raw `- ` marker survived: %q", got)
	}
}

// Cycle 175: bold spans go through the cBoldOpen / cBoldClose pair
// with no marker bytes around the content (cycle-155 parity —
// ref's recovered render emits ANSI codes only).
//
// Cycle glamour-migration: the closing escape is now \x1b[m
// (full style reset) instead of \x1b[22m (cycle-175 walker
// which closed only the bold attribute). The structural
// property — bold-on around the emphasised word, no `**`
// surviving — is preserved.
func TestRenderMarkdown_BoldSpans(t *testing.T) {
	got := renderMarkdown("this is **important** to remember", 0)
	// Pin the bold-open escape and the emphasised word; the
	// exact closing escape differs between the cycle-175
	// walker and glamour (both turn bold off; glamour also
	// resets colour, which is fine for the transcript).
	if !strings.Contains(got, "\x1b[38;2;7;54;66;1mimportant") {
		t.Errorf("bold span opening missing: %q", got)
	}
	// The marker bytes must NOT appear in the output (cycle-155
	// parity — ref emits ANSI only, no `**` markers).
	if strings.Contains(got, "**important**") {
		t.Errorf("raw `**` markers survived: %q", got)
	}
}

// Cycle 175: italic spans get the ESC[3m / ESC[23m pair (cycle 175
// bonus over cycle 155, which did not recover italic styling).
//
// Cycle glamour-migration: closing escape is \x1b[m (full
// reset) rather than \x1b[23m; structural property — italic-on
// around emphasised word — preserved.
func TestRenderMarkdown_ItalicSpans(t *testing.T) {
	got := renderMarkdown("this is *important* to remember", 0)
	if !strings.Contains(got, "\x1b[38;2;7;54;66;3mimportant") {
		t.Errorf("italic span opening missing: %q", got)
	}
	if strings.Contains(got, "*important*") {
		t.Errorf("raw `*` markers survived: %q", got)
	}
}

// Cycle 175: fenced code blocks get a 4-space indent and the
// ``` ``` marker lines are dropped from the output (cycle-155
// parity).
//
// Cycle 233: a language-tagged fence ("```go") is now chroma-
// styled by the cycle-233 chroma Restore pass. The 4-space indent
// survives chroma (indentChromaBlock wraps the chroma output
// before insertion), but chroma wraps each token in ANSI escapes,
// so a literal substring check for "    func main()" fails — the
// ANSI codes break the contiguous sequence. Strip ANSI before
// checking.
func TestRenderMarkdown_FencedCodeBlock(t *testing.T) {
	got := renderMarkdown("before\n```go\nfunc main() { println(\"hi\") }\n```\nafter", 0)
	plain := stripAnsiConv(got)
	if !strings.Contains(plain, "    func main()") {
		t.Errorf("code block not indented: %q", got)
	}
	// The fence marker lines themselves must NOT appear in the
	// output (cycle-155 parity).
	if strings.Contains(got, "```") {
		t.Errorf("fence marker survived: %q", got)
	}
	// Surrounding prose is preserved.
	if !strings.Contains(got, "before") || !strings.Contains(got, "after") {
		t.Errorf("surrounding prose lost: %q", got)
	}
}

// stripAnsiConv removes ANSI SGR sequences from s. Cycle 233
// added this helper because chroma's terminal16m formatter wraps
// each token in 24-bit RGB ANSI escapes, breaking literal
// substring matching for the cycle-175 fenced-code-block pin
// "    func main()".
func stripAnsiConv(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			if j < len(s) {
				i = j + 1
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// Cycle 175: inline backticks still get the cInlineCodeOpen /
// cInlineCodeClose pair — mdrender does not apply this pass, so
// renderMarkdown applies styleInlineBackticks on top of the
// library output (cycle 158 history).
//
// Cycle glamour-migration: glamour v2.0.1 emits inline code
// spans already wrapped in \x1b[38;5;220m…\x1b[m (gold colour
// on default bg), stripping the literal "`" bytes. The
// legacy styleInlineBackticks post-pass now sees no backtick
// bytes to wrap, so the chip colour does not apply. Pin
// glamour's gold-coloured code span and the content.
func TestRenderMarkdown_InlineBackticks(t *testing.T) {
	got := renderMarkdown("use `go test ./...` to verify", 0)
	// Either the cycle-158 chip colour OR glamour's gold code
	// colour around the same content must be present.
	want := []string{
		cInlineCodeOpen + "go test ./..." + cInlineCodeClose, // cycle-158 chip (fallback path)
		"\x1b[38;5;32;48;5;254mgo test ./...",                // cycle-271 glamour chip
	}
	found := false
	for _, w := range want {
		if strings.Contains(got, w) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("inline code chip missing (neither cycle-158 nor glamour colour):\n%s", got)
	}
}

// Cycle 175: blank-line collapse matches cycle-160 reference
// behaviour. Three consecutive newlines become one.
//
// Cycle glamour-migration: glamour's word-wrap fills lines to
// the requested width with trailing spaces ("first              "
// then "\n" then "\n" then "second"). The legacy walker kept
// the source blank-line count verbatim. We pin the property
// "no three consecutive newlines" — glamour satisfies this —
// but pin it loosely (any "\n\n\n" sequence anywhere in the
// output is the failure indicator).
func TestRenderMarkdown_BlankLineCollapse(t *testing.T) {
	got := renderMarkdown("first\n\n\n\nsecond", 0)
	if strings.Contains(got, "\n\n\n") {
		t.Errorf("blank lines did not collapse: %q", got)
	}
	// Glamour pads the visible text with trailing whitespace
	// up to the wrap width, so the literal "first\n\nsecond"
	// sequence is unlikely to appear byte-for-byte. Pin the
	// structural property (no 3+ newlines) and the words.
	if !strings.Contains(got, "first") || !strings.Contains(got, "second") {
		t.Errorf("paragraph content lost: %q", got)
	}
}

// Cycle 175: a realistic model reply with mixed bullets, bold,
// italic, and inline code all render through one renderMarkdown
// call — sanity check that the per-line styleInlineBackticks
// post-pass does not interfere with the bold/italic escapes.
//
// Cycle glamour-migration: the post-pass is a no-op now (glamour
// strips literal backticks; styleInlineBackticks never finds a
// run to wrap). We pin the structural property (ANSI around
// bold/italic, no `**` / ` ``` ` surviving) instead of the
// legacy byte-exact span shape.
func TestRenderMarkdown_RealisticReply(t *testing.T) {
	src := "Here is **what changed**:\n\n- the *parser* now handles `fenced` blocks\n- bullet markers get the round-dot glyph\n- **bold** spans are styled\n\n```\n$ go test ./...\nok\n```\n"
	got := renderMarkdown(src, 0)
	if !strings.Contains(got, "\u2022 the") {
		t.Errorf("realistic reply lost bullets: %q", got)
	}
	// Bold span: cycle-175 byte-pinned literal was the
	// "ESC[1mwhat changedESC[22m" pair; glamour closes with
	// "ESC[m". Pin the open-only and the emphasised text.
	if !strings.Contains(got, "\x1b[38;2;7;54;66;1mwhat changed") {
		t.Errorf("realistic reply lost bold opening: %q", got)
	}
	// Italic span: same property — open + content.
	if !strings.Contains(got, "\x1b[38;2;7;54;66;3mparser") {
		t.Errorf("realistic reply lost italic opening: %q", got)
	}
	// Inline code: cycle-158 chip colour OR glamour gold
	// colour around the same content.
	wantCode := []string{
		cInlineCodeOpen + "fenced" + cInlineCodeClose,
		"\x1b[38;5;32;48;5;254mfenced",
	}
	found := false
	for _, w := range wantCode {
		if strings.Contains(got, w) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("realistic reply lost inline chip (neither cycle-158 nor glamour colour): %q", got)
	}
	// Fenced block is indented.
	if !strings.Contains(stripAnsiConv(got), "    $ go test ./...") {
		t.Errorf("realistic reply lost fenced indent: %q", got)
	}
	// Fence markers themselves are gone.
	if strings.Contains(got, "```") {
		t.Errorf("realistic reply leaked fence marker: %q", got)
	}
}

// Cycle 148: a loaded conversation's tool step maps a stored
// Step.Status to the renderer's `err` flag. status=4 (errored) and
// status=6/7 (rare defensive fallback) render with the
// "(Failed)" suffix; status=3 (success) and status=5 (alternate
// success) do not. Captured against the v1.3.6 reference using
// resumed conversation 4638a7b0-e5f4-4961-80c2-d61499f06050.db
// (390 rows with status=4, 4 with status=6, 21 with status=7).
func TestLoadedStepErr_StatusMapping(t *testing.T) {
	cases := []struct {
		status  int64
		wantErr bool
	}{
		{3, false}, // success
		{4, true},  // failed (errored)
		{5, false}, // completed-skipped / alternate success
		{6, true},  // rare
		{7, true},  // rare
		{0, false}, // unknown
	}
	for _, c := range cases {
		if got := loadedStepErr(c.status); got != c.wantErr {
			t.Errorf("loadedStepErr(status=%d) = %v, want %v", c.status, got, c.wantErr)
		}
	}
}

// Cycle 148: a loaded Step renders the "(Failed)" suffix when its
// status maps to err=true. The renderer normalises the stored
// ToolArgs JSON (cycle 133 argsOf CamelCase aliases) and produces
// the same shape as a live stream'd step.
func TestLoadedToolStep_RendersFailedSuffix(t *testing.T) {
	step := loadedToolStep(convstoreStep("run_command", `{"CommandLine":"ls"}`, 4))
	if !step.err {
		t.Errorf("expected err=true for status=4, got %+v", step)
	}
	step = loadedToolStep(convstoreStep("run_command", `{"CommandLine":"ls"}`, 3))
	if step.err {
		t.Errorf("expected err=false for status=3, got %+v", step)
	}
	if convstoreStep("a", "b", 4).Status != 4 {
		t.Fatal("convstoreStep helper should retain the status field")
	}
}

// convstoreStep is a tiny helper to build a convstore.Step without
// importing the package's other helpers.
func convstoreStep(toolName, toolArgs string, status int64) struct {
	Idx        int64
	Type       int64
	Status     int64
	Text       string
	ToolName   string
	ToolArgs   string
	ToolResult string
} {
	return struct {
		Idx        int64
		Type       int64
		Status     int64
		Text       string
		ToolName   string
		ToolArgs   string
		ToolResult string
	}{Status: status, ToolName: toolName, ToolArgs: toolArgs}
}
