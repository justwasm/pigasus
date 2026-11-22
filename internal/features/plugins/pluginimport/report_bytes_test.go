// report_bytes_test.go — B-25: the plugin-import report's bytes.
//
// 【一手】 agy 1.4.12 (sha256 34423fc9b0af3841e7646aa72a2d40ffeab8218b22d1cb99eac7355f9eba31e3)
// was measured on this machine with a synthetic $HOME:
//
//	HOME=$TMP agy plugin import claude
//	  [ok]    full
//	          ✔ skills      : 1 installed (sa)
//	          ✔ agents      : 1 installed (ag)
//	          ✔ commands    : 1 installed (converted to skills) (hello)
//	          ✔ mcpServers  : 1 installed (srv)
//	          ⚠ MCP server "bad" skipped: the legacy SSE transport isn't supported
//
//	Staged to $TMP/.gemini/config
//
// 【strings】 the binary's string table carries that component line's own
// format string — "          %s %-12s: %d %s (%s)" — beside the literal
// "installed (converted to skills)", and the skip line's own literal
// "  [skip]  %s (already imported, use --force to re-import)".
//
// The official documentation (official-docs/cli/gcli-migration.md:33-44)
// shows a different "Expected import output" block — "[ok]   <name>", a "-"
// marker and the details "skipped (none detected)", "N legacy commands
// converted to skills", "N skills processed" and "N server definition
// migrated to mcp_config.json". That block is a documentation defect, not
// the reference's output: none of its phrases exists in the binary ("none
// detected", "legacy command", "server definition" and "skills processed"
// are absent from the whole file), and the measured output of both
// importers — and of `agy plugin install` — is the block above. Finding
// B-25 of the cycle-383 docs-alignment ledger is closed on that
// measurement, and the tests below pin the reference's bytes so that a
// change to the report has to be a deliberate one.
package pluginimport

import (
	"path/filepath"
	"strings"
	"testing"
)

// The whole report of a plugin with every component and one warning, byte
// for byte: the reference's bytes, line for line.
func TestReportBytesAreTheReferences(t *testing.T) {
	roots, _ := testRoots(t)
	dir := claudePlugin(t, roots, "full", `{"name":"full"}`)
	writeFile(t, dir, "commands/hello.md", "hello\n")
	writeFile(t, dir, "skills/sa/SKILL.md", "s\n")
	writeFile(t, dir, "agents/ag.md", "a\n")
	writeFile(t, dir, ".mcp.json", `{"mcpServers":{"srv":{"command":"node"},"bad":{"type":"sse","url":"u"}}}`)

	out, errOut := run(t, roots, false, SourceClaude)
	if errOut != "" {
		t.Fatalf("stderr = %q, want empty", errOut)
	}
	want := "  [ok]    full\n" +
		"          ✔ skills      : 1 installed (sa)\n" +
		"          ✔ agents      : 1 installed (ag)\n" +
		"          ✔ commands    : 1 installed (converted to skills) (hello)\n" +
		"          ✔ mcpServers  : 1 installed (srv)\n" +
		"          ⚠ MCP server \"bad\" skipped: the legacy SSE transport isn't supported\n" +
		"\nStaged to " + roots.ConfigHome + "\n"
	if out != want {
		t.Fatalf("stdout =\n%q\nwant\n%q", out, want)
	}
}

// A component that staged nothing gets no line at all: the reference does
// not name the components it skipped, so the documentation's
// "- skills     : skipped (none detected)" is not a line of the report.
func TestReportOmitsTheComponentsThatStagedNothing(t *testing.T) {
	roots, _ := testRoots(t)
	claudePlugin(t, roots, "bare", `{"name":"bare"}`)

	out, _ := run(t, roots, false, SourceClaude)
	want := "  [ok]    bare\n" +
		"\nStaged to " + roots.ConfigHome + "\n"
	if out != want {
		t.Fatalf("stdout =\n%q\nwant\n%q", out, want)
	}
	if strings.Contains(out, "skipped (none detected)") {
		t.Errorf("the report names a component it skipped:\n%s", out)
	}
}

// The Gemini importer's report, byte for byte: a plugin with two skills
// and one agent, and a commands directory the importer does not convert.
func TestGeminiReportBytesAreTheReferences(t *testing.T) {
	roots, _ := testRoots(t)
	ext := filepath.Join(roots.ExtensionsDir, "gem1")
	writeFile(t, ext, "gemini-extension.json", `{"name":"gem1","version":"1.0.0","description":"Test."}`)
	writeFile(t, ext, "skills/sk1/SKILL.md", "s\n")
	writeFile(t, ext, "skills/sk2/SKILL.md", "s\n")
	writeFile(t, ext, "agents/a1.md", "a\n")
	writeFile(t, ext, "commands/c1.md", "c\n")

	out, errOut := run(t, roots, false, SourceGemini)
	if errOut != "" {
		t.Fatalf("stderr = %q, want empty", errOut)
	}
	want := "  [ok]    gem1\n" +
		"          ✔ skills      : 2 installed (sk1, sk2)\n" +
		"          ✔ agents      : 1 installed (a1)\n" +
		"\nStaged to " + roots.ConfigHome + "\n"
	if out != want {
		t.Fatalf("stdout =\n%q\nwant\n%q", out, want)
	}
}

// The skip line's bytes: the plugin's own line and nothing else.
func TestSkipLineBytesAreTheReferences(t *testing.T) {
	roots, _ := testRoots(t)
	claudePlugin(t, roots, "cp", `{"name":"cp"}`)
	run(t, roots, false, SourceClaude)

	out, _ := run(t, roots, false, SourceClaude)
	want := "  [skip]  cp (already imported, use --force to re-import)\n" +
		"\nStaged to " + roots.ConfigHome + "\n"
	if out != want {
		t.Fatalf("stdout =\n%q\nwant\n%q", out, want)
	}
}
