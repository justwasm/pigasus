// changelog_cycle_235_test.go — regression tests for cycle 235.
//
// Cycle 235 prepends a `1.4.2:` placeholder block ahead of the
// captured AGY 1.4.2 binary's `changelog` output (which itself only
// goes up to `1.4.1:` — the binary ships one release behind). The
// file is byte-for-byte embedded via `changelog_data.go`
// (`//go:embed testdata/changelog_reference.txt`), so these tests
// read the same on-disk bytes the embed would and pin the cycle-235
// contract:
//
//   - The file starts with a `1.4.2:` header at line 0, followed by
//     the reference's own 1.4.2 bullet, a blank line, then the
//     captured `1.4.1:` header.
//   - That 1.4.2 bullet is upstream's real release note, captured
//     verbatim. Cycle 235 shipped a hand-written placeholder in its
//     place (the v1.4.2 binary had no 1.4.2 section to copy); the
//     reference-drift cycle replaced it once the v1.4.3 binary shipped
//     the genuine notes. See docs/cycles/reference-drift/.
//   - The original 1.4.1 → 1.0.0 trail (the captured binary output)
//     remains byte-for-byte unchanged after the stub: the 1.4.1 lead
//     bullet, the 1.4.0 section, and the 1.0.0 closing line are all
//     present at their expected positions.
//
// The package is `cli` (alongside `changelog_data.go` /
// `changelog_data_test.go`) so the embed test in `changelog_data.go`
// and these regression tests share the same view of the bytes.
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/referencedrift"
)

// changelogReferencePath walks up from dir looking for go.mod and
// returns the absolute path to internal/cli/testdata/changelog_reference.txt,
// or "" when no project root is found. Mirrors the lookup the runtime
// loader uses (cmd/wagyu/slash_changelog_source.go::findChangelogReferencePath)
// so this test exercises the same on-disk bytes the embed would, but
// without relying on the package-level `referenceChangelog` constant
// (those byte-level SHA / length pins live in `changelog_data_test.go`).
func changelogReferencePath(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	cur, err := filepath.Abs(cwd)
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(cur, "go.mod")); err == nil {
			candidate := filepath.Join(cur, "internal", "cli", "testdata", "changelog_reference.txt")
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
			return ""
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return ""
		}
		cur = parent
	}
}

// TestChangelogReference_TopsOutAtNewest pins the shape cycle 235
// introduced: the loaded /changelog source starts with a version header,
// and that header is the highest version the file contains. The reference
// binary always ships one release behind its own version, so the newest
// block here is the release before the installed binary (1.4.3 while the
// binary reports 1.4.4).
func TestChangelogReference_TopsOutAt142(t *testing.T) {
	path := changelogReferencePath(t)
	if path == "" {
		t.Skipf("changelog_reference.txt not reachable from cwd %q — skipping", os.Getenv("PWD"))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	// The newest section must be the very first line: no stub, no
	// preamble, no blank line ahead of it.
	first := firstNonEmpty(lines)
	if first != lines[0] || !isVersionHeader(first) {
		t.Fatalf("first line = %q, want a version header such as %q", first, "1.4.3:")
	}
	newest := strings.TrimSuffix(first, ":")
	for _, l := range lines {
		if !isVersionHeader(l) {
			continue
		}
		if v := strings.TrimSuffix(l, ":"); referencedrift.CompareVersions(v, newest) > 0 {
			t.Errorf("section %s appears below %s; the file must start at its newest release", v, newest)
		}
	}
	// And 1.4.1: must follow somewhere after the 1.4.2: stub. The
	// binary-captured block is the only `1.4.1:` content in the
	// file, so a missing 1.4.1: means the cycle-235 stub has
	// replaced the binary capture entirely — a regression.
	saw141 := false
	for _, l := range lines {
		if strings.HasPrefix(l, "1.4.1:") {
			saw141 = true
			break
		}
	}
	if !saw141 {
		t.Error("no 1.4.1: block in changelog_reference.txt (binary-captured section should follow the cycle-235 stub)")
	}
}

// firstNonEmpty returns the first non-empty line in lines, or "" when
// lines is empty / all blank. Used by TestChangelogReference_TopsOutAt142
// to pin the cycle-235 contract that the 1.4.2: header is the first
// non-empty line of the file.
func firstNonEmpty(lines []string) string {
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			return l
		}
	}
	return ""
}

// TestChangelogReference_NewestBulletIsUpstream pins the newest section's
// lead bullet to the reference's own release notes.
//
// Cycle 235 shipped a hand-written placeholder here, because the v1.4.2
// binary's `changelog` only reached back to 1.4.1 and the 1.4.2 notes did
// not exist anywhere to be captured from. Later binaries ship the real
// notes, so the reference-drift cycles replaced the placeholder with an
// actual capture. This assertion pins genuine upstream bytes rather than a
// TODO, which makes it a parity gate instead of a bookkeeping marker.
func TestChangelogReference_NewestBulletIsUpstream(t *testing.T) {
	path := changelogReferencePath(t)
	if path == "" {
		t.Skipf("changelog_reference.txt not reachable from cwd %q \u2014 skipping", os.Getenv("PWD"))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	text := string(data)
	const want = "1.4.7:\n\u00b7 Added `lastUserInput` to the JSON that hook commands receive on stdin"
	if !strings.HasPrefix(text, want) {
		t.Errorf("newest section is not the upstream capture:\n  want prefix: %q\n  file head: %.200q\u2026", want, text)
	}
	if strings.Contains(text, "release notes not yet recovered") {
		t.Error("the cycle-235 placeholder text is still present; the real release notes supersede it")
	}
}

// TestChangelogReference_BinaryCaptureBlockUnchanged pins that the
// captured block is not corrupted
// below it: the embedded 1.4.1: → 1.0.0 trail must remain byte-for-byte
// identical to the v1.4.2 binary's `agy changelog` capture. The
// combined blob's SHA-256 matches `changelog_data_test.go`'s
// `referenceChangelogSHA256` constant; this test asserts the
// substantive content of the binary-captured portion directly so a
// future edit to the lead bullet / closing line / 1.4.0 section
// shows up here even if the SHA test gets re-pinned elsewhere.
func TestChangelogReference_BinaryCaptureBlockUnchanged(t *testing.T) {
	path := changelogReferencePath(t)
	if path == "" {
		t.Skipf("changelog_reference.txt not reachable from cwd %q — skipping", os.Getenv("PWD"))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	text := string(data)
	// The cycle-235 stub occupies the first ~109 bytes (header +
	// bullet + blank + newline + next-header). The binary capture
	// starts just past the stub. Pin the lead bullet of the 1.4.1
	// section so any edit to the trail shows up here.
	const lead141Bullet = "· Improved `agy plugin validate` to also check a plugin's skills, subagents, and rules"
	if !strings.Contains(text, lead141Bullet) {
		t.Errorf("1.4.1 lead bullet not found in file — was the binary-capture block corrupted?")
	}
	// The closing 1.0.0 release line: the other end of the embedded
	// capture.
	const closeLine = "· Initial release of the Antigravity CLI."
	if !strings.Contains(text, closeLine) {
		t.Errorf("1.0.0 closing line not found — was the binary-capture block truncated?")
	}
	// The 1.4.0: section must also still be present (binary capture).
	if !strings.Contains(text, "\n1.4.0:\n") {
		t.Errorf("1.4.0: section not found — was the binary-capture block truncated?")
	}
}
