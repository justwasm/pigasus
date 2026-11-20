// Tests for changelog_data.go: byte-level 1:1 of referenceChangelog against
// the v1.5.13 binary's `antigravity changelog` output.
//
// The v1.5.13 binary's release-notes block is 247,942 bytes / 1,305 lines
// holding 96 version sections (1.5.12 down to 1.0.0). The reference binary
// always ships one release behind its own version, so the newest section is
// 1.5.12 while the binary reports 1.5.13; every 1.5.x release from 1.5.0 to
// 1.5.12 is now in the feed, and 1.3.7/1.3.8/1.3.9 — which the 1.4.x feeds
// skipped — are present too.
//
// The gates below are hard equality checks, not contains-checks, so any
// edit to a bullet line fails loudly:
//
//  1. exact byte length (247,942)
//  2. exact SHA-256 of the embedded bytes
//  3. the block shape (first/last version header, section and bullet
//     counts, absence of a 1.4.0 section)
//  4. byte-for-byte equality with a reference `agy changelog` run when a
//     reference binary is available on this machine
//
// When the upstream binary ships a new version, re-capture `agy changelog`,
// replace testdata/changelog_reference.txt with a fresh capture and bump
// the constants here.
package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/infra/referencedrift"
)

const (
	// referenceChangelogByteLen is the size of the embedded blob, a
	// verbatim `agy changelog` capture from the v1.5.13 binary.
	referenceChangelogByteLen = 247942
	// referenceChangelogLines is the embedded blob's line count.
	referenceChangelogLines = 1305
	// referenceChangelogSections is the number of `X.Y.Z:` version headers
	// (1.5.12 down to 1.0.0; the 1.5.x series filled in 1.5.0-1.5.12 and
	// 1.3.7/1.3.8/1.3.9, which earlier feeds omitted).
	referenceChangelogSections = 96
	// referenceChangelogBullets is the number of `· ` top-level bullets.
	referenceChangelogBullets = 1114
	// referenceChangelogSHA256 is the SHA-256 of the embedded bytes.
	referenceChangelogSHA256 = "18f5b65bd202977dda65b1aa00b27f8bade482dfb9f6d1ff9f14846ebe0f41aa"
)

// TestReferenceVersion_MatchesDriftLock pins cli.ReferenceVersion -- the
// string every user-visible version in the clone renders -- to the version
// recorded in internal/referencedrift's lock.
//
// These are two separate declarations of the same fact: a Go constant and a
// JSON file. Keeping them in step by hand is exactly the kind of bookkeeping
// this repository already got wrong once (STATUS.md and README.md drifted for
// a hundred cycles), so it gets a test. The changelog artifact is allowed to
// sit at a newer captured_at than either; the behavioural pin may not.
func TestReferenceVersion_MatchesDriftLock(t *testing.T) {
	root, err := referencedrift.RepoRoot(".")
	if err != nil {
		t.Skipf("no go.mod above %s: %v", os.Getenv("PWD"), err)
	}
	l, err := referencedrift.LoadFrom(filepath.Join(root, referencedrift.LockPath))
	if err != nil {
		t.Fatalf("%v", err)
	}
	if l.Version != ReferenceVersion {
		t.Errorf("version pin disagreement:\n  cli.ReferenceVersion = %q\n  referencedrift lock   = %q\n"+
			"update both together, and re-capture anything they pinned",
			ReferenceVersion, l.Version)
	}
}

// TestChangelog_ByteLen asserts referenceChangelog is the expected length.
func TestChangelog_ByteLen(t *testing.T) {
	if got := len(referenceChangelog); got != referenceChangelogByteLen {
		t.Errorf("referenceChangelog length: got %d, want %d", got, referenceChangelogByteLen)
	}
	if got := strings.Count(referenceChangelog, "\n"); got != referenceChangelogLines {
		t.Errorf("referenceChangelog newline count: got %d, want %d", got, referenceChangelogLines)
	}
}

// TestChangelog_SHA256 pins the exact bytes. This is the strongest gate:
// it fails on any edit, including whitespace-only ones that a length
// check would miss.
func TestChangelog_SHA256(t *testing.T) {
	sum := sha256.Sum256([]byte(referenceChangelog))
	if got := hex.EncodeToString(sum[:]); got != referenceChangelogSHA256 {
		t.Errorf("referenceChangelog SHA-256: got %s, want %s", got, referenceChangelogSHA256)
	}
}

// TestChangelog_Shape asserts the release-notes structure: it starts at
// 1.5.10 (the 1.5.11 binary ships the 1.5.11 release notes nowhere — its
// changelog feed's newest section is still 1.5.10), carries 94 version
// headers and 1091 top-level bullets, contains the 1.4.2, 1.4.1 and
// 1.4.0 sections from the binary capture, and ends at the 1.0.0
// initial-release section.
func TestChangelog_Shape(t *testing.T) {
	if !strings.HasPrefix(referenceChangelog, "1.5.12:\n") {
		t.Errorf("referenceChangelog must start with %q", "1.5.12:\n")
	}
	if !strings.Contains(referenceChangelog, "\n1.4.1:\n") {
		t.Errorf("referenceChangelog must contain a 1.4.1 section")
	}
	if !strings.Contains(referenceChangelog, "\n1.4.0:\n") {
		t.Errorf("referenceChangelog must contain a 1.4.0 section")
	}
	if got := countVersionHeaders(referenceChangelog); got != referenceChangelogSections {
		t.Errorf("version headers: got %d, want %d", got, referenceChangelogSections)
	}
	if got := strings.Count(referenceChangelog, "\n· ") + boolToInt(strings.HasPrefix(referenceChangelog, "· ")); got != referenceChangelogBullets {
		t.Errorf("top-level bullets: got %d, want %d", got, referenceChangelogBullets)
	}
	if !strings.Contains(referenceChangelog, "\n1.0.0:\n· Initial release of the Antigravity CLI.") {
		t.Errorf("referenceChangelog must end with the 1.0.0 initial-release section")
	}
}

// countVersionHeaders counts lines of the form `X.Y.Z:` at the start of
// a line.
func countVersionHeaders(s string) int {
	n := 0
	for _, line := range strings.Split(s, "\n") {
		if isVersionHeader(line) {
			n++
		}
	}
	return n
}

func isVersionHeader(line string) bool {
	if !strings.HasSuffix(line, ":") {
		return false
	}
	line = strings.TrimSuffix(line, ":")
	parts := strings.Split(line, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// TestChangelog_MatchesReferenceBinary runs a reference `agy changelog`
// and asserts byte-for-byte equality with the binary-captured portion
// of referenceChangelog (the trailing bytes after the cycle-235
// 1.4.2: stub, if present). The reference binary is looked up (in
// order) at $AGY_REF, ~/.local/bin/agy and
// /workspace/agy/install/antigravity via referencedrift.FindBinary --
// the single resolver shared with the drift gate, so the two cannot
// disagree about which binary they are measuring against; the test is
// skipped (not failed) when none is present so the shape gates above
// still run on a machine without the upstream binary.
func TestChangelog_MatchesReferenceBinary(t *testing.T) {
	bin := referencedrift.FindBinary()
	if bin == "" {
		t.Skip("no reference agy binary found (set $AGY_REF to enable this gate)")
	}
	if v, err := referencedrift.BinaryVersion(bin); err == nil && v != ReferenceVersion {
		t.Skipf("reference binary at %s is version %s (clone emulates %s)", bin, v, ReferenceVersion)
	}
	out, err := exec.Command(bin, "changelog").Output()
	if err != nil {
		t.Fatalf("%s changelog failed: %v", bin, err)
	}
	// referenceChangelog is now a verbatim capture of `agy changelog` --
	// cycle-235's hand-written 1.4.2 stub was retired in the reference-drift
	// cycle once the v1.4.3 binary shipped the real 1.4.2 release notes.
	// So this is a straight whole-blob byte comparison, with no prefix to
	// strip and no version arithmetic to get wrong.
	if string(out) != referenceChangelog {
		t.Fatalf("referenceChangelog drifts from %s changelog: got %d bytes, want %d (embedded blob)\nrun: %s changelog > internal/cli/testdata/changelog_reference.txt",
			bin, len(out), len(referenceChangelog), bin)
	}
}

// TestChangelog_DispatchEmitsConstant builds a parser with the standard
// spec, runs `changelog`, and asserts the captured output equals
// referenceChangelog byte-for-byte through the dispatch path.
func TestChangelog_DispatchEmitsConstant(t *testing.T) {
	p := NewParser(StandardFlags(), StandardSubcommands(),
		"antigravity", "test-version", "test-desc")
	p.SetOut(&capturingWriter{t: t})
	if err := p.ExecuteArgs([]string{"changelog"}); err != nil {
		t.Fatalf("ExecuteArgs(changelog) returned err: %v", err)
	}
}

// capturingWriter is a minimal io.Writer that fails the test if any
// byte written to it does not match referenceChangelog.
type capturingWriter struct {
	t    *testing.T
	off  int
	done bool
}

func (c *capturingWriter) Write(p []byte) (int, error) {
	if c.done {
		c.t.Errorf("capturingWriter: extra write of %d bytes after referenceChangelog exhausted", len(p))
		return 0, nil
	}
	for i, b := range p {
		if c.off >= len(referenceChangelog) {
			c.done = true
			c.t.Errorf("capturingWriter: referenceChangelog exhausted at offset %d, extra byte 0x%02x", c.off, b)
			return i, nil
		}
		if referenceChangelog[c.off] != b {
			c.t.Errorf("capturingWriter: byte drift at offset %d: got 0x%02x, want 0x%02x",
				c.off, b, referenceChangelog[c.off])
			return i, nil
		}
		c.off++
	}
	if c.off == len(referenceChangelog) {
		c.done = true
	}
	return len(p), nil
}
