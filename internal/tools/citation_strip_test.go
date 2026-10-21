// Tests for the AGY 1.2.10 Bash/Read citation-strip gate. The five
// spec-required tests (and the bonus coverage at the bottom) together
// pin the stripper's contract end to end:
//
//   - citation blocks at the end of Bash/Read results are removed
//   - non-Bash/non-Read tool results are NOT touched
//   - real `[N]` references inside prose (not citation-shaped) survive
//   - text with no citations is a no-op
//   - the strip is robust to whitespace and trailing newline conventions
package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCitationStrip_BashResultStripsTrailingFootnotes covers the headline
// case: a Bash tool result that ends with a `Sources:` block gets the
// block removed before the model sees the text. The pre-strip output is
// what run_command would have produced when the upstream pipeline ran
// citation extraction on the command output; the post-strip output is
// what AGY 1.2.10 promises the model will see.
func TestCitationStrip_BashResultStripsTrailingFootnotes(t *testing.T) {
	dir := t.TempDir()
	reg := Catalog(dir)

	// Stage a file run_command can `cat`. The pipeline ran citation
	// extraction on the cat output and appended a Sources: block.
	const fileContent = "alpha\nbeta\ngamma\n"
	if err := os.WriteFile(filepath.Join(dir, "data.txt"), []byte(fileContent), 0o644); err != nil {
		t.Fatalf("seed data.txt: %v", err)
	}

	// We can't make run_command's output carry a citation block directly,
	// so exercise the gate through Dispatch by registering a Bash-shaped
	// tool whose output IS a citation block, and verify Dispatch strips
	// it. This proves the gate fires for run_command without depending on
	// which extractor ran upstream.
	type citationBashTool struct{ payload string }

	citationBash := &fakeTool{
		name:   "run_command",
		schema: json.RawMessage(`{"type":"object"}`),
		out: strings.Join([]string{
			"alpha",
			"beta",
			"gamma",
			"Sources:",
			"[1] https://example.com/a",
			"[2] https://example.com/b",
			"",
		}, "\n"),
	}
	reg.Register(citationBash)

	out, err := reg.Dispatch(context.Background(), "run_command", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Dispatch(run_command): %v", err)
	}

	// Citation block must be gone.
	if strings.Contains(out, "Sources:") {
		t.Errorf("Sources: header survived strip:\n%s", out)
	}
	if strings.Contains(out, "[1]") || strings.Contains(out, "[2]") {
		t.Errorf("citation line survived strip:\n%s", out)
	}
	// Original content must be intact.
	for _, keep := range []string{"alpha", "beta", "gamma"} {
		if !strings.Contains(out, keep) {
			t.Errorf("legitimate output line %q was removed:\n%s", keep, out)
		}
	}
	// The trailing newline of the original `Sources:` block must not have
	// been replaced with a stray blank line — the result is exactly the
	// three original lines.
	if got, want := strings.TrimRight(out, "\n"), "alpha\nbeta\ngamma"; got != want {
		t.Errorf("stripped output = %q, want %q", got, want)
	}
	_ = citationBashTool{}
}

// TestCitationStrip_ReadResultStripsTrailingFootnotes covers the Read half
// of the changelog: read_file (and its view_file alias) must have a
// trailing citation block stripped.
func TestCitationStrip_ReadResultStripsTrailingFootnotes(t *testing.T) {
	dir := t.TempDir()
	reg := Catalog(dir)

	// Stage a file read_file can read. We then register a stub read_file
	// whose output simulates what the upstream pipeline produced for the
	// file's contents.
	reg.Register(&fakeTool{
		name:   "read_file",
		schema: json.RawMessage(`{"type":"object"}`),
		out:    "package main\n\nfunc main() {}\n\nCitations:\n[1] https://golang.org/doc/\n",
	})

	out, err := reg.Dispatch(context.Background(), "read_file", json.RawMessage(`{"path":"x.go"}`))
	if err != nil {
		t.Fatalf("Dispatch(read_file): %v", err)
	}

	if strings.Contains(out, "Citations:") {
		t.Errorf("Citations: header survived strip:\n%s", out)
	}
	if strings.Contains(out, "[1] https://golang.org/doc/") {
		t.Errorf("citation line survived strip:\n%s", out)
	}
	if !strings.Contains(out, "package main") || !strings.Contains(out, "func main()") {
		t.Errorf("legitimate file content was removed:\n%s", out)
	}

	// And the view_file alias of read_file must also be gated.
	reg.Register(&fakeTool{
		name:   "view_file",
		schema: json.RawMessage(`{"type":"object"}`),
		out:    "package main\n\nReferences:\n[1] https://golang.org/doc/\n[2] https://pkg.go.dev/\n",
	})
	outAlias, err := reg.Dispatch(context.Background(), "view_file", json.RawMessage(`{"path":"x.go"}`))
	if err != nil {
		t.Fatalf("Dispatch(view_file): %v", err)
	}
	if strings.Contains(outAlias, "References:") {
		t.Errorf("view_file: References: header survived strip:\n%s", outAlias)
	}
	if strings.Contains(outAlias, "[2]") {
		t.Errorf("view_file: second citation line survived strip:\n%s", outAlias)
	}
}

// TestCitationStrip_OtherToolsUnaffected confirms the gate is scoped to
// Bash and Read only. search_web and read_url_content legitimately emit
// citations and must NOT be stripped.
func TestCitationStrip_OtherToolsUnaffected(t *testing.T) {
	dir := t.TempDir()
	reg := Catalog(dir)

	const searchResult = "Search hits:\n[1] https://example.com/a\n[2] https://example.com/b\n"
	reg.Register(&fakeTool{
		name:   "search_web",
		schema: json.RawMessage(`{"type":"object"}`),
		out:    searchResult,
	})

	out, err := reg.Dispatch(context.Background(), "search_web", json.RawMessage(`{"query":"x"}`))
	if err != nil {
		t.Fatalf("Dispatch(search_web): %v", err)
	}
	if out != searchResult {
		t.Errorf("search_web output was modified by Bash/Read gate:\n got: %q\nwant: %q", out, searchResult)
	}

	// And read_url_content — network read with citation metadata.
	const urlResult = "URL content.\nSources:\n[1] https://example.com/source\n"
	reg.Register(&fakeTool{
		name:   "read_url_content",
		schema: json.RawMessage(`{"type":"object"}`),
		out:    urlResult,
	})
	outURL, err := reg.Dispatch(context.Background(), "read_url_content", json.RawMessage(`{"url":"https://x"}`))
	if err != nil {
		t.Fatalf("Dispatch(read_url_content): %v", err)
	}
	if outURL != urlResult {
		t.Errorf("read_url_content output was modified by Bash/Read gate:\n got: %q\nwant: %q", outURL, urlResult)
	}

	// And a tool that returns JSON (the non-string branch of Dispatch)
	// — must also be left alone when it isn't a Bash/Read tool.
	const jsonResult = `{"matches": [{"ref": 1}, {"ref": 2}]}` + "\nSources:\n[1] foo\n"
	reg.Register(&fakeTool{
		name:   "grep_search",
		schema: json.RawMessage(`{"type":"object"}`),
		out:    map[string]any{"raw": jsonResult, "matches": 2},
	})
	outJSON, err := reg.Dispatch(context.Background(), "grep_search", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Dispatch(grep_search): %v", err)
	}
	if !strings.Contains(outJSON, "[1] foo") {
		t.Errorf("grep_search (non-Bash/Read) was stripped of its citations:\n%s", outJSON)
	}
}

// TestCitationStrip_RealCitationInTextPreserved proves the strip is
// conservative: a `[N]` mention inside prose (not at the trailing block
// position) must NOT be removed. This pins the two important
// false-positive guards:
//
//  1. a single `[N]` line at the end of the text (bare, no header) is
//     NOT stripped, because one line is far more likely to be a
//     legitimate grep hit / printf output than a citation;
//  2. `[N]` references that sit in the middle of the text (so the
//     trailing-content test fails) are not touched.
func TestCitationStrip_RealCitationInTextPreserved(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "single bracketed line at end is preserved (grep match / printf)",
			in:   "drwxr-xr-x  3 root root 4096 Jan  1 00:00 .\n[1] /usr/local/bin",
			want: "drwxr-xr-x  3 root root 4096 Jan  1 00:00 .\n[1] /usr/local/bin",
		},
		{
			name: "year-style bracket in middle is preserved",
			in:   "the [2024] release notes say\nthis happened in [2024]\nlast line",
			want: "the [2024] release notes say\nthis happened in [2024]\nlast line",
		},
		{
			name: "bracket in middle of line is preserved",
			in:   "see Smith[1] for details\nand then Jones[2] for context\nfinal sentence",
			want: "see Smith[1] for details\nand then Jones[2] for context\nfinal sentence",
		},
		{
			name: "printf style bracket in middle is preserved",
			in:   "first line\nprintf \"[%d] %s\\n\", 1, \"one\"\nlast line",
			want: "first line\nprintf \"[%d] %s\\n\", 1, \"one\"\nlast line",
		},
		{
			name: "header followed by only prose after it is preserved",
			in:   "Sources:\nthis is not a citation block\njust a paragraph that mentions the word",
			want: "Sources:\nthis is not a citation block\njust a paragraph that mentions the word",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := StripCitationFootnotes(tc.in)
			if got != tc.want {
				t.Errorf("StripCitationFootnotes = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestCitationStrip_NoCitationsNoOp is the no-op case: a Bash / Read
// result that does not contain any citation-shaped text must come back
// byte-identical. This is the steady-state case for every well-formed
// tool call, and the stripper must not silently mutate it.
func TestCitationStrip_NoCitationsNoOp(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{name: "plain bash output", in: "total 12\ndrwxr-xr-x . .\n-rw-r--r-- file.txt\n"},
		{name: "plain file content", in: "package main\n\nfunc main() {}\n"},
		{name: "empty string", in: ""},
		{name: "single newline", in: "\n"},
		{name: "only whitespace", in: "   \n\n  \n"},
		{name: "bracketed but no citation shape", in: "[hello] world\n[year] 2024\n"},
		{name: "single [1] inside text but not at end", in: "first [1] middle\nlast line\n"},
		{name: "trailing whitespace only", in: "real content\n\n   \n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := StripCitationFootnotes(tc.in)
			if got != tc.in {
				t.Errorf("StripCitationFootnotes changed a no-op input\n got: %q\nwas: %q", got, tc.in)
			}
		})
	}
}

// TestCitationStrip_BareBlockAtEnd confirms a bare citation block (no
// header) at the trailing edge is stripped. This is the second variant
// the changelog refers to — the extractor might emit `[1]…[2]…[3]…`
// without a `Sources:` label. Two or more consecutive citation lines
// are required; one is not enough.
func TestCitationStrip_BareBlockAtEnd(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "two citation lines at end are stripped",
			in:   "first line\nsecond line\n[1] https://example.com/a\n[2] https://example.com/b",
			want: "first line\nsecond line",
		},
		{
			name: "three citation lines at end are stripped, including indented ones",
			in:   "header line\n  [1] foo\n  [2] bar\n  [3] baz",
			want: "header line",
		},
		{
			name: "single bare citation line at end is preserved (grep match / printf)",
			in:   "header line\n[1] this is the only one",
			want: "header line\n[1] this is the only one",
		},
		{
			name: "citation range line [1-3] is recognised",
			in:   "header\n[1-3] combined range citation\n[4] another",
			want: "header",
		},
		{
			name: "header with citation range is stripped",
			in:   "header\nReferences:\n[1-5] combined range\n[6] standalone",
			want: "header",
		},
		{
			name: "trailing newline is preserved when original had one",
			in:   "header\nSources:\n[1] foo\n[2] bar\n",
			want: "header\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := StripCitationFootnotes(tc.in)
			if got != tc.want {
				t.Errorf("StripCitationFootnotes\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

// TestCitationStrip_ShouldStripToolGate pins the tool-name allowlist that
// decides which Dispatch results get the gate. Anything outside the
// Bash/Read set must return false, and the explicit Bash/Read names must
// return true. This is the unit the Dispatch wiring depends on; if it
// regresses the test surface catches it before the model does.
func TestCitationStrip_ShouldStripToolGate(t *testing.T) {
	for _, name := range []string{
		"run_command", "run_shell_command", "exec_command",
		"read_file", "view_file",
	} {
		if !ShouldStripCitations(name) {
			t.Errorf("ShouldStripCitations(%q) = false, want true (Bash/Read tool)", name)
		}
	}
	for _, name := range []string{
		"search_web", "read_url_content", "call_mcp_tool",
		"grep_search", "list_dir", "write_to_file",
		"finish", "schedule", "no_such_tool",
	} {
		if ShouldStripCitations(name) {
			t.Errorf("ShouldStripCitations(%q) = true, want false (not a Bash/Read tool)", name)
		}
	}
}

// fakeTool is a tiny Tool implementation used by the dispatch-level
// tests to inject a known result without standing up a real executor.
type fakeTool struct {
	name   string
	schema json.RawMessage
	out    any
}

func (f *fakeTool) Name() string            { return f.name }
func (f *fakeTool) Schema() json.RawMessage { return f.schema }
func (f *fakeTool) Run(context.Context, json.RawMessage) (any, error) {
	return f.out, nil
}
