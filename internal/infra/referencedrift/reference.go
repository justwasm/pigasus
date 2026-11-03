// Package referencedrift detects when the reference `agy` binary has moved
// away from the version this clone's committed evidence was captured under.
//
// # The problem
//
// The clone's central correctness claim is byte-level parity with `agy X.Y.Z`.
// That claim has a shelf life. Google ships `agy` updates automatically — the
// binary on this machine went 1.4.2 -> 1.4.3 on its own, without anyone in
// this repository choosing to. Every golden under `internal/*/testdata/` was
// captured at one particular reference version and silently becomes wrong the
// moment the installed binary moves past it.
//
// Before this package, exactly one test noticed: `TestChangelog_MatchesReferenceBinary`
// in `internal/cli` compares a *byte-equality* property, so upstream drift in
// the changelog happens to fail it. The other two large evidence artifacts --
// the 34 KB system-prompt capture and the 60-entry advertised-tool list -- are
// only ever compared against other committed artifacts, never against the live
// binary. Those tests stayed green through the 1.4.2 -> 1.4.3 move, which is
// worse than a red test: it is confident, silent wrongness.
//
// # The contract
//
// `reference.lock.json` pins two things:
//
//   - Version -- the reference release the clone's behaviour is measured
//     against. This is the same value as `cli.ReferenceVersion`; the lock
//     duplicates it so the evidence registry is self-contained.
//   - LagAcknowledgedThrough -- the highest upstream version we have consciously
//     decided *not* to re-evidence, and why.
//
// Three classes of problem are reported, and they are not equally severe:
//
//   - Self-inconsistency (fixture digest != the lock's record, or a fixture
//     whose capture version disagrees with the pinned version) is always a bug
//     in *this* repository, independent of what upstream does. It fails.
//   - Unacknowledged upstream drift (live binary newer than the ack ceiling)
//     means the parity claim is simply no longer verified. It fails, because
//     the honest state is a red test, not a green one.
//   - Acknowledged drift (live binary at or below the ack ceiling) passes, but
//     reports which artifacts are now suspect so the record stays honest.
//
// The acknowledgement is deliberately a committed, reviewable file rather than
// an environment variable or a `t.Skip`. Accepting drift is a decision; `git
// blame` on that line should name who made it and when.
package referencedrift

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Lock is the on-disk shape of reference.lock.json. The file lives beside this
// source rather than in a testdata directory because it is part of the
// repository's contract with upstream, not evidence *of* upstream behaviour.
type Lock struct {
	// Version is the reference release the clone pins. Kept equal to
	// cli.ReferenceVersion; Load does not enforce that so this package stays
	// importable from `internal/cli`'s own tests without an import cycle.
	Version string `json:"version"`

	// LagAcknowledgedThrough is the highest upstream version we have decided
	// not to re-evidence. Empty means "no lag tolerated at all". Compare
	// with CompareVersions, not string equality -- "1.4.10" > "1.4.9".
	LagAcknowledgedThrough string `json:"lag_acknowledged_through,omitempty"`

	// LagNote explains what is known to have moved. Surfaced verbatim in the
	// gate's report so a reader who did not write it still learns something.
	LagNote string `json:"lag_note,omitempty"`

	// Artifacts is the evidence registry: every committed fixture whose
	// validity is scoped to a particular reference version.
	Artifacts []Artifact `json:"artifacts"`
}

// Artifact is one committed piece of evidence captured from the reference
// binary at a known version.
type Artifact struct {
	// Path is repo-relative so the lock reads the same from any checkout.
	Path string `json:"path"`

	// CapturedAt is the reference version this fixture was taken from. It
	// should agree with the version embedded in the filename where the
	// repo already follows that convention (capture_1.4.4.txt,
	// advertised_tools_1.3.6.txt).
	CapturedAt string `json:"captured_at"`

	// Digest is the lowercase hex sha256 of the file's bytes. It catches
	// silent fixture edits and is regenerated with `-update`.
	Digest string `json:"digest"`

	// Live is true when the artifact's bytes are re-verifiable against the
	// reference binary at CapturedAt. Only the changelog is: `agy changelog`
	// is a plain command whose output we can diff. The system-prompt capture
	// and the advertised-tool list come from an instrumented proxy run
	// (cycle 274) and cannot be reproduced by invoking `agy` directly, so
	// they are pinned by digest alone and flagged as suspect on drift.
	Live bool `json:"live,omitempty"`

	// How is remediation guidance shown when the artifact goes suspect.
	How string `json:"how,omitempty"`
}

// Load reads reference.lock.json and validates its shape. It does not touch
// the filesystem beyond the lock itself; Resolve does that.
func Load() (*Lock, error) {
	root, err := RepoRoot(".")
	if err != nil {
		return nil, err
	}
	return LoadFrom(filepath.Join(root, filepath.FromSlash(LockPath)))
}

// LoadFrom reads and validates a lock file at an arbitrary path. Load uses it
// for the committed lock; the shape-validation tests use it directly so they
// can feed in deliberately incoherent documents without writing to the repo.
func LoadFrom(path string) (*Lock, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("referencedrift: read lock: %w", err)
	}
	var l Lock
	if err := json.Unmarshal(raw, &l); err != nil {
		return nil, fmt.Errorf("referencedrift: parse %s: %w", path, err)
	}
	if l.Version == "" {
		return nil, fmt.Errorf("referencedrift: %s has no version", path)
	}
	seen := make(map[string]bool, len(l.Artifacts))
	for i, a := range l.Artifacts {
		if a.Path == "" || a.CapturedAt == "" {
			return nil, fmt.Errorf("referencedrift: artifact %d in %s is missing path or captured_at", i, path)
		}
		if seen[a.Path] {
			return nil, fmt.Errorf("referencedrift: %s lists %s twice", path, a.Path)
		}
		seen[a.Path] = true
	}
	if l.LagAcknowledgedThrough != "" {
		if c := CompareVersions(l.LagAcknowledgedThrough, l.Version); c <= 0 {
			return nil, fmt.Errorf("referencedrift: lag_acknowledged_through %q is not newer than the pinned version %q",
				l.LagAcknowledgedThrough, l.Version)
		}
	}
	return &l, nil
}

// CompareVersions orders two dot-separated integer versions. It returns -1, 0
// or +1, and treats any missing component as 0 so "1.4" == "1.4.0". A
// component that is not a non-negative integer sorts before any that is, which
// keeps a garbage value from ever reading as "newer".
func CompareVersions(a, b string) int {
	as, aok := splitVersion(a)
	bs, bok := splitVersion(b)
	for i := 0; i < 3; i++ {
		switch {
		case !aok:
			if !bok {
				continue
			}
			return -1
		case !bok:
			return 1
		case as[i] != bs[i]:
			if as[i] < bs[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

func splitVersion(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimSpace(v), ".")
	if len(parts) == 0 || len(parts) > 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return [3]int{}, false
		}
		out[i] = n
	}
	return out, true
}

// Digest returns the lowercase hex sha256 of b. Used to seal the lock.
func Digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// RepoRoot walks up from start looking for the module's go.mod. The evidence
// registry is repo-relative, but `go test` runs each package in its own
// directory.
func RepoRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("referencedrift: resolve %s: %w", start, err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("referencedrift: no go.mod above %s", dir)
		}
		dir = parent
	}
}
