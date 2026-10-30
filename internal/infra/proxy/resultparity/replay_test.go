package resultparity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/piagentbridge"
	"github.com/justwasm/pigasus/internal/subagenttools"
	"github.com/justwasm/pigasus/internal/tools"
)

// fixturePath is the 【一手】 capture: one "## <tool> {args}" header per call
// followed by the result text the reference sent back.
//
// The capture was re-taken at agy 1.4.7 (2026-10-25). The 1.4.4 capture is
// kept next to it as the previous evidence: every read_file, write_file and
// edit_file section came back byte-identical, and the six sections that differ
// (schedule, invoke_subagent "self", search_web, generate_image,
// read_url_content, ask_question) each carry a note saying what moved - the
// reference, the capture harness, or the capture mode.
func findFixturePath() string {
	for _, p := range []string{
		"../../../tools/testdata/result_texts_1.4.7.txt",
		"../tools/testdata/result_texts_1.4.7.txt",
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "../../../tools/testdata/result_texts_1.4.7.txt"
}

var fixturePath = findFixturePath()

// stampRE is the Created At / Completed At header the reference puts in front
// of every result. The timestamps themselves are wall-clock, so they are
// stripped before comparing.
var stampRE = regexp.MustCompile(`\ACreated At: \S+\nCompleted At: \S+\n`)

// numberedLineRE matches a content line the reference emitted as "<n>: <text>".
var numberedLineRE = regexp.MustCompile(`^[0-9]+: `)

// mode says how strictly a section's body is compared.
type mode int

const (
	// exactBody compares the whole body byte for byte (after trailing-newline
	// normalisation and the /tmp/ → fixture-root rewrite).
	exactBody mode = iota
	// proseBody compares only the non-numbered, non-elided lines, plus the
	// first and last numbered lines that survive. It exists for the 1.4.4
	// capture, which abbreviated the 800-line and 46080-byte bodies with "…"
	// and could not be compared byte for byte. The 1.4.7 capture carries
	// those bodies in full, so every section is exactBody today.
	proseBody
)

// replay is one planned replay of a captured call.
type replay struct {
	tool string
	// args is the argument object as it appears in the capture; it is compacted
	// before matching, so whitespace does not matter.
	args string
	// setup creates the files the call needs, relative to the fixture root.
	setup func(t *testing.T, root string)
	// mode defaults to exactBody.
	mode mode
	// skip, when non-empty, is the reason this section is not replayed. A skip
	// is a recorded decision, not a pass.
	skip string
}

// file writes a fixture file under root, creating parents.
func file(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

// lines builds "line 1" … "line n".
func lineFile(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	return b.String()
}

// longFile is the 100 × 1001-byte rl_long.txt: "A0000" + 995 x + newline. Its
// 100100-byte size and 100491-byte numbered form are both quoted in the capture,
// and its first line is A0000 (not A0001 — the capture's line 47 reads A0046).
func longFile() string {
	var b strings.Builder
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&b, "A%04d", i)
		b.WriteString(strings.Repeat("x", 995))
		b.WriteString("\n")
	}
	return b.String()
}

// replays is the plan for every section of the capture.
func replays() []replay {
	return []replay{
		// ── read_file ──────────────────────────────────────────────────────
		{tool: "read_file", args: `{"Path": "/tmp/refread.txt"}`,
			setup: func(t *testing.T, root string) {
				file(t, root, "refread.txt", "line one\nline two\nline three\n")
			}},
		{tool: "read_file", args: `{"Path": "/tmp/refread.txt", "StartLine": 2, "EndLine": 2}`,
			setup: func(t *testing.T, root string) {
				file(t, root, "refread.txt", "line one\nline two\nline three\n")
			}},
		{tool: "read_file", args: `{"Path": "/tmp/refempty.txt"}`,
			setup: func(t *testing.T, root string) { file(t, root, "refempty.txt", "") }},
		{tool: "read_file", args: `{"Path": "/tmp/rl_big.txt"}`,
			setup: func(t *testing.T, root string) { file(t, root, "rl_big.txt", lineFile(900)) }},
		{tool: "read_file", args: `{"Path": "/tmp/rl_big.txt", "StartLine": 100}`,
			setup: func(t *testing.T, root string) { file(t, root, "rl_big.txt", lineFile(900)) }},
		{tool: "read_file", args: `{"Path": "/tmp/rl_big.txt", "StartLine": 1, "EndLine": 900}`,
			setup: func(t *testing.T, root string) { file(t, root, "rl_big.txt", lineFile(900)) }},
		{tool: "read_file", args: `{"Path": "/tmp/refread.txt", "StartLine": 10, "EndLine": 20}`,
			setup: func(t *testing.T, root string) {
				file(t, root, "refread.txt", "line one\nline two\nline three\n")
			}},
		{tool: "read_file", args: `{"Path": "/tmp/rl_long.txt"}`,
			setup: func(t *testing.T, root string) { file(t, root, "rl_long.txt", longFile()) }},
		{tool: "read_file", args: `{"Path": "/tmp/rl_long.txt", "StartLine": 1, "EndLine": 100, "ByteOffset": 46080}`,
			setup: func(t *testing.T, root string) { file(t, root, "rl_long.txt", longFile()) }},

		// ── write_file ─────────────────────────────────────────────────────
		{tool: "write_file", args: `{"Path": "/tmp/refwrite.txt", "Content": "alpha\nbeta\n"}`},
		{tool: "write_file", args: `{"Path": "/tmp/refwrite2.txt", "Content": "overwritten\n"}`,
			setup: func(t *testing.T, root string) { file(t, root, "refwrite2.txt", "alpha\nbeta\n") }},
		{tool: "write_file", args: `{"Path": "/tmp/refwrite2.txt", "Content": "delta\n", "Append": true}`,
			setup: func(t *testing.T, root string) { file(t, root, "refwrite2.txt", "alpha\nbeta\n") }},

		// ── edit_file ──────────────────────────────────────────────────────
		{tool: "edit_file", args: `{"Path": "/tmp/refedit2.txt", "Target": "beta", "Replacement": "gamma"}`,
			setup: func(t *testing.T, root string) { file(t, root, "refedit2.txt", "alpha beta\n") }},
		{tool: "edit_file", args: `{"Path": "/tmp/refmulti.txt", "Target": "alpha\nbeta", "Replacement": "ALPHA\nBETA\nGAMMA"}`,
			setup: func(t *testing.T, root string) { file(t, root, "refmulti.txt", "alpha\nbeta\n") }},
		{tool: "edit_file", args: `{"Path": "/tmp/rl_edit.txt", "Target": "ZZZ-not-there", "Replacement": "x"}`,
			setup: func(t *testing.T, root string) { file(t, root, "rl_edit.txt", "a\nb\n") }},

		// ── view_file ──────────────────────────────────────────────────────
		{tool: "view_file", args: `{"AbsolutePath": "/tmp/agentws/a.txt"}`,
			setup: func(t *testing.T, root string) { file(t, root, "agentws/a.txt", "l1\nl2\nl3\nl4\n") }},
		{tool: "view_file", args: `{"AbsolutePath": "/tmp/agentws/a.txt", "StartLine": 10}`,
			setup: func(t *testing.T, root string) { file(t, root, "agentws/a.txt", "l1\nl2\nl3\nl4\n") }},
		{tool: "view_file", args: `{"AbsolutePath": "/tmp/agentws/a.txt", "StartLine": 2, "EndLine": 3}`,
			setup: func(t *testing.T, root string) { file(t, root, "agentws/a.txt", "l1\nl2\nl3\nl4\n") }},
		{tool: "view_file", args: `{"AbsolutePath": "/tmp/rl_long.txt"}`,
			setup: func(t *testing.T, root string) { file(t, root, "rl_long.txt", longFile()) }},
		{tool: "view_file", args: `{"AbsolutePath": "/tmp/rl_long.txt", "StartLine": 1, "EndLine": 60, "ContentOffset": 46080}`,
			setup: func(t *testing.T, root string) { file(t, root, "rl_long.txt", longFile()) }},
		{tool: "view_file", args: `{"AbsolutePath": "/tmp/rl_big.txt"}`,
			setup: func(t *testing.T, root string) { file(t, root, "rl_big.txt", lineFile(900)) }},

		// ── the 60-name / 12-declaration executors ─────────────────────────
		{tool: "manage_task", args: `{"Action": "list"}`},
		{tool: "generate_image", args: `{"Prompt": "a red cube", "ImageName": "cube"}`,
			skip: "the 1.4.7 capture records a successful generation - the reference calls the non-streaming " +
				"generateContent RPC (requestType \"image_gen\", model \"gemini-3.1-flash-image\"), the capture " +
				"harness forwards it to the real CloudCode endpoint, and the reference returns the brain path " +
				"of the .jpg it saved. The clone has no image backend wired (GenerateImage.Provider is nil), " +
				"so it still returns the 1.4.4 error text; the two bodies are not comparable until a provider " +
				"exists."},

		// ── subagent tools (routed through subagenttools, not the catalogue:
		// the catalogue's entries for these names are pendingTool stubs) ────
		{tool: "send_message", args: `{"Recipient": "abc123", "Message": "hi"}`},
		{tool: "define_subagent", args: `{"Name": "cycletest", "Description": "Test subagent", "SystemPrompt": "You are a test."}`},
		{tool: "manage_subagents", args: `{"Action": "kill_all"}`},
		{tool: "invoke_subagent", args: `{"TypeName": "general", "Role": "Tester", "Prompt": "Say OK."}`},

		// ── recorded skips ─────────────────────────────────────────────────
		{tool: "search_web", args: `{"query": "golang", "domain": "go.dev"}`,
			skip: "needs the network: the live tool executes a real web query and returns formatSearchWebResult. " +
				"The 1.4.7 capture records a live grounded summary because the capture harness forwards the " +
				"model RPC to the real CloudCode endpoint; the 1.4.4 capture recorded the \"no candidates " +
				"returned from GenerateContent\" failure because the mock could not fake a grounded answer " +
				"(cycle 293 said so at the time). Neither body is reproducible."},
		{tool: "invoke_subagent", args: `{"TypeName": "self", "Role": "Tester", "Prompt": "Say OK and stop."}`,
			skip: "the captured ack is inherently async ('The subagent will send you a message when it has " +
				"completed … There is no need to poll for its response'); the clone delegates synchronously and " +
				"returns the subagent's answer in the same result instead. Emitting the ack would either drop " +
				"that answer or invent a hybrid the reference never prints. See " +
				"docs/cycles/subagent-registry-texts/deliverable.md. The 1.4.7 capture confirms the ack's " +
				"shape; only the per-run conversation id and brain path differ."},
		{tool: "schedule", args: `{"Prompt": "ping", "DurationSeconds": 3600}`,
			skip: "the captured result is a backgrounded launch: it carries a runtime task id, a brain-directory " +
				"path, and no Completed At. The clone runs the command synchronously (documented deviation, " +
				"cycle 277), so no analogue exists to compare against."},
		{tool: "read_url_content", args: `{"Url": "https://go.dev/"}`,
			skip: "needs the network: the captured body is the fetched article's Title / OG Description and the " +
				"brain-directory path it was saved to. The reference does not fetch the URL itself - it asks " +
				"CloudCode (rewriteUri, then fetchFromTrawlerCache) and gets a Trawler-cache article back."},
		{tool: "ask_question", args: `{"questions": [{"question": "Pick one", "options": ["alpha", "beta"]}]}`,
			skip: "needs an interactive answer: the 1.4.7 capture was taken in print mode, where there is no user " +
				"to answer, and records \"A1: User Skipped\"; the 1.4.4 capture was taken in the TUI and recorded " +
				"\"A1: alpha\" - the user selecting the first option."},
	}
}

// key identifies a section by tool name plus compacted arguments.
func key(name string, args []byte) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, args); err != nil {
		return name + "\x00" + string(args)
	}
	return name + "\x00" + buf.String()
}

// TestCaptureIsFullyAccountedFor fails when the capture and the replay plan
// disagree in either direction. Without it a new section could be added to the
// fixture (or a replay entry left behind after a fixture edit) and nothing
// would notice.
func TestCaptureIsFullyAccountedFor(t *testing.T) {
	sections, err := Load(fixturePath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	seen := make(map[string]bool, len(sections))
	for _, s := range sections {
		k := key(s.Name, s.Args)
		if seen[k] {
			t.Errorf("capture has duplicate section %s %s (line %d)", s.Name, s.Args, s.Line)
		}
		seen[k] = true
	}

	planned := make(map[string]bool)
	for _, c := range replays() {
		k := key(c.tool, []byte(c.args))
		if planned[k] {
			t.Errorf("replay plan lists %s %s twice", c.tool, c.args)
		}
		planned[k] = true
		if !seen[k] {
			t.Errorf("replay plan has an entry with no matching capture section: %s %s", c.tool, c.args)
		}
	}

	for _, s := range sections {
		if !planned[key(s.Name, s.Args)] {
			t.Errorf("capture section at line %d is neither replayed nor skipped: %s %s",
				s.Line, s.Name, s.Args)
		}
	}
}

// TestCapturedResultTexts replays every planned call and compares the clone's
// result text to the capture.
func TestCapturedResultTexts(t *testing.T) {
	sections, err := Load(fixturePath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	byKey := make(map[string]Section, len(sections))
	for _, s := range sections {
		byKey[key(s.Name, s.Args)] = s
	}

	for _, c := range replays() {
		c := c
		name := c.tool + " " + c.args
		if len(name) > 90 {
			name = name[:90]
		}
		t.Run(name, func(t *testing.T) {
			if c.skip != "" {
				t.Skip(c.skip)
			}
			section, ok := byKey[key(c.tool, []byte(c.args))]
			if !ok {
				t.Fatalf("no capture section for %s %s", c.tool, c.args)
			}

			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatalf("resolve temp dir: %v", err)
			}
			if c.setup != nil {
				c.setup(t, root)
			}

			// The capture uses /tmp; replay against the temp root and rewrite
			// the expected text the same way.
			args := json.RawMessage(strings.ReplaceAll(c.args, "/tmp/", root+"/"))
			want := strings.ReplaceAll(section.Body, "/tmp/", root+"/")

			got, err := runTool(t.Context(), root, c.tool, args)
			if err != nil {
				t.Fatalf("%s: %v", c.tool, err)
			}
			compare(t, section, c.mode, want, got)
		})
	}
}

// runTool executes one captured call. The subagent tools are routed through
// subagenttools.Tools — the same executors the real turn path pairs with the
// captured declarations — because the 60-name catalogue only carries
// placeholder entries for those names.
//
// The catalogue tools are called through Tool.Run, not Registry.Dispatch: the
// capture records what the *tool* returns, while Dispatch and
// piagentbridge's adapter additionally pass the text through ApplyOutputBudget.
// That budget is the clone's own (DefaultMaxResultTokens = 8000 is a cycle-248
// guess, not a measurement) and it truncates the two 46080-byte reads the
// capture shows reaching the model intact — see this cycle's report.
func runTool(ctx context.Context, root, name string, args json.RawMessage) (string, error) {
	for _, t := range subagentTools() {
		if t.Name() != name {
			continue
		}
		res, err := t.Execute(ctx, "captured-call", args, nil)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		for _, c := range res.Content {
			if tc, ok := c.(pi.TextContent); ok {
				b.WriteString(tc.Text)
			}
		}
		return b.String(), nil
	}
	tool, ok := tools.RuntimeCatalog(root).Get(name)
	if !ok {
		return "", fmt.Errorf("no executor registered for %q", name)
	}
	out, err := tool.Run(ctx, args)
	if err != nil {
		return "", err
	}
	return piagentbridge.RenderResult(out), nil
}

// subagentTools builds the subagent executors once per process. They hold no
// session state that a replay cares about: every captured subagent call is a
// pre-delegation path (unknown recipient, kill_all, undefined type).
var subagentToolCache []piagent.AgentTool

func subagentTools() []piagent.AgentTool {
	if subagentToolCache == nil {
		subagentToolCache = subagenttools.Tools(subagenttools.Deps{})
	}
	return subagentToolCache
}

// compare asserts the clone's result matches the capture.
func compare(t *testing.T, section Section, m mode, want, got string) {
	t.Helper()

	hasStamp := stampRE.MatchString(got)
	if section.HasStamp && !hasStamp {
		t.Fatalf("result has no Created At / Completed At header, but the capture does (line %d)\n--- got ---\n%s",
			section.Line, got)
	}
	if !section.HasStamp && hasStamp {
		t.Fatalf("result has a Created At / Completed At header, but the capture does not (line %d)\n--- got ---\n%s",
			section.Line, got)
	}
	want = strings.TrimRight(stampRE.ReplaceAllString(want, ""), "\n")
	got = strings.TrimRight(stampRE.ReplaceAllString(got, ""), "\n")

	switch m {
	case exactBody:
		if want == got {
			return
		}
		t.Fatalf("result text differs from the capture (line %d)\n--- want ---\n%s\n--- got ---\n%s\n%s",
			section.Line, want, got, diffHint(want, got))
	case proseBody:
		// The capture abbreviates the body, so the assertion is one-sided: every
		// prose line it kept must appear in order, and the range boundaries it
		// kept must appear at all. Extra body lines in the clone are expected —
		// that is what the elision hid.
		wantLines := wantProseLines(want)
		gotLines := strings.Split(got, "\n")
		if !containsSubsequence(gotLines, wantLines) {
			t.Fatalf("result prose differs from the capture (line %d; the captured body is elided, so only "+
				"the non-numbered lines are compared)\n--- want (in order) ---\n%s\n--- got ---\n%s",
				section.Line, strings.Join(wantLines, "\n"), got)
		}
		// The elided bodies still pin their surviving content lines (e.g.
		// "1: line 1" and "800: line 800" of a 900-line read). The capture
		// shortens some of them without a marker ("47: A0046xxxxx"), so an
		// anchor is matched as a prefix.
		for _, line := range anchorLines(want) {
			if !containsLinePrefix(gotLines, line) {
				t.Errorf("capture's content line %q is missing from the result (line %d)", line, section.Line)
			}
		}
	}
}

// wantProseLines is the capture body minus its numbered content lines and its
// "…" elisions.
func wantProseLines(want string) []string {
	var keep []string
	for _, line := range strings.Split(want, "\n") {
		if line == "…" || strings.Contains(line, "…") {
			continue
		}
		if numberedLineRE.MatchString(line) {
			continue
		}
		keep = append(keep, line)
	}
	return keep
}

// anchorLines keeps the first and last numbered line of the capture, which are
// the range boundaries (and therefore the interesting ones).
func anchorLines(want string) []string {
	var numbered []string
	for _, line := range strings.Split(want, "\n") {
		if strings.Contains(line, "…") {
			continue
		}
		if numberedLineRE.MatchString(line) {
			numbered = append(numbered, line)
		}
	}
	switch len(numbered) {
	case 0:
		return nil
	case 1:
		return numbered
	default:
		return []string{numbered[0], numbered[len(numbered)-1]}
	}
}

// containsSubsequence reports whether want appears in got in order.
func containsSubsequence(got, want []string) bool {
	i := 0
	for _, g := range got {
		if i < len(want) && g == want[i] {
			i++
		}
	}
	return i == len(want)
}

// containsLinePrefix reports whether any line starts with want.
func containsLinePrefix(lines []string, want string) bool {
	for _, l := range lines {
		if strings.HasPrefix(l, want) {
			return true
		}
	}
	return false
}

// diffHint points at the first differing line, which is far easier to read than
// two long blocks.
func diffHint(want, got string) string {
	wl := strings.Split(want, "\n")
	gl := strings.Split(got, "\n")
	for i := 0; i < len(wl) && i < len(gl); i++ {
		if wl[i] != gl[i] {
			return fmt.Sprintf("first difference at line %d:\n  want %q\n  got  %q", i+1, wl[i], gl[i])
		}
	}
	if len(wl) != len(gl) {
		return fmt.Sprintf("want %d lines, got %d", len(wl), len(gl))
	}
	return ""
}
