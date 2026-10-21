package referencedrift

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// LockPath is the evidence registry, relative to the repository root, so a
// caller in any package can reach it with a single join against RepoRoot.
const LockPath = "internal/referencedrift/reference.lock.json"

// Status is the resolved outcome of comparing the installed reference binary
// and the committed evidence against reference.lock.json.
type Status struct {
	// Binary is the resolved reference binary path, or "" when none is
	// installed. A machine without the upstream binary cannot be told
	// anything about drift, so every caller treats that as "no verdict"
	// rather than "no drift".
	Binary string

	// LiveVersion is what `agy --version` printed, or "" if unavailable.
	LiveVersion string

	// VersionErr records why LiveVersion could not be read, if it could not.
	VersionErr error

	// Pinned is the lock's Version; AckedThrough its lag ceiling.
	Pinned       string
	AckedThrough string

	// LagAcknowledged is true when a live binary exists and is newer than
	// Pinned but no newer than AckedThrough.
	LagAcknowledged bool

	// Suspect lists evidence invalidated by upstream drift — i.e. captured
	// at or before Pinned while the live binary has moved past it.
	Suspect []Artifact

	// Missing and DigestMismatch are repository-internal: the lock describes
	// a file that is gone, or a file whose bytes changed without the lock
	// being regenerated.
	Missing        []Artifact
	DigestMismatch []Mismatch
}

// Mismatch is one artifact whose bytes no longer match its recorded digest.
type Mismatch struct {
	Artifact Artifact
	Want     string
	Got      string
}

// Empty reports whether Status carries no findings at all — no missing
// fixtures, no digest mismatches, no unacknowledged drift, and no acknowledged
// drift either. Callers use it to decide between "pass quietly" and "pass but
// say something".
func (s Status) Empty() bool {
	return len(s.Missing) == 0 && len(s.DigestMismatch) == 0 &&
		len(s.Suspect) == 0 && !s.LagAcknowledged
}

// RepoBroken reports findings that are this repository's own fault, true
// regardless of what upstream did.
func (s Status) RepoBroken() bool {
	return len(s.Missing) > 0 || len(s.DigestMismatch) > 0
}

// Unacknowledged reports that the live binary is past the ack ceiling, which
// means the clone's parity claim is currently unverified and cannot be.
func (s Status) Unacknowledged() bool {
	return s.Binary != "" && s.LiveVersion != "" && s.VersionErr == nil &&
		CompareVersions(s.LiveVersion, s.AckedThrough) > 0
}

// Resolve fills in a Status for lock l, reading evidence artifacts relative to
// repoRoot. It never fails: every finding is data on the returned Status, so a
// single call produces a complete report rather than a sequence of errors.
func Resolve(repoRoot string, l *Lock) Status {
	st := Status{Pinned: l.Version, AckedThrough: l.Version}
	if l.LagAcknowledgedThrough != "" {
		st.AckedThrough = l.LagAcknowledgedThrough
	}
	for _, a := range l.Artifacts {
		st.classify(repoRoot, a)
	}
	st.Binary = FindBinary()
	if st.Binary == "" {
		return st
	}
	v, err := BinaryVersion(st.Binary)
	if err != nil {
		st.VersionErr = err
		return st
	}
	st.LiveVersion = v
	// An artifact goes suspect when it was captured from a release *older*
	// than the one installed: whatever it pins may have changed silently.
	// This is decided per artifact, not per lock, because the changelog can
	// be re-captured from a newer binary while the system-prompt capture
	// still only exists for the older one.
	for _, a := range l.Artifacts {
		if CompareVersions(a.CapturedAt, v) < 0 {
			st.Suspect = append(st.Suspect, a)
		}
	}
	st.LagAcknowledged = CompareVersions(v, st.Pinned) > 0 && CompareVersions(v, st.AckedThrough) <= 0
	return st
}

// classify checks one artifact's presence and digest against the lock.
func (s *Status) classify(root string, a Artifact) {
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(a.Path)))
	if err != nil {
		s.Missing = append(s.Missing, a)
		return
	}
	if got := Digest(b); got != a.Digest {
		s.DigestMismatch = append(s.DigestMismatch, Mismatch{Artifact: a, Want: a.Digest, Got: got})
	}
}

// FindBinary returns the first reference `agy` path that exists, or "".
//
// Lookup order is $AGY_REF, ~/.local/bin/agy, then the Linux sandbox path
// /workspace/agy/install/antigravity used by the committed evidence under
// out/. This is the same order `internal/cli`'s changelog gate uses; both call
// this function so the two cannot drift apart.
func FindBinary() string {
	if p := os.Getenv("AGY_REF"); p != "" {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	var cands []string
	if home, err := os.UserHomeDir(); err == nil {
		cands = append(cands, filepath.Join(home, ".local", "bin", "agy"))
	}
	cands = append(cands, "/workspace/agy/install/antigravity")
	for _, c := range cands {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c
		}
	}
	return ""
}

// BinaryVersion runs `<bin> --version` and returns the trimmed output. The
// reference prints exactly `<semver>\n` (6 bytes for 1.4.2, verified by xxd in
// the rc-polish cycle); trailing newlines are trimmed rather than parsed so a
// future build that adds a prefix still resolves instead of panicking.
func BinaryVersion(bin string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("referencedrift: %s --version: %w", bin, err)
	}
	return trimVersion(string(out)), nil
}

func trimVersion(s string) string {
	for _, cut := range []byte{'\n', '\r', ' ', '\t'} {
		for len(s) > 0 && s[len(s)-1] == cut {
			s = s[:len(s)-1]
		}
	}
	return s
}

// Report renders a Status as the operator-facing text the gate prints. It is
// exported so the `-update` path and any future `wagyu doctor` subcommand
// share one phrasing.
func Report(s Status, lagNote string) string {
	var b []byte
	add := func(f string, a ...any) { b = append(b, fmt.Sprintf(f, a...)...) }

	switch {
	case s.Binary == "":
		add("reference-drift: no reference `agy` binary found (set $AGY_REF to enable this gate).\n")
		add("  Evidence digests for %d artifact(s) were still verified against %s.\n", len(s.Suspect)+len(s.Missing)+len(s.DigestMismatch), s.Pinned)
	case s.VersionErr != nil:
		add("reference-drift: could not read the version of %s: %v\n", s.Binary, s.VersionErr)
	case !s.Empty():
		add("reference-drift: live %s is %s; this clone is pinned to %s (lag acknowledged through %s).\n",
			s.Binary, s.LiveVersion, s.Pinned, s.AckedThrough)
		if lagNote != "" {
			add("  %s\n", lagNote)
		}
	}
	for _, a := range s.Suspect {
		add("  SUSPECT  %s — captured at agy %s, unverified against %s\n", a.Path, a.CapturedAt, s.LiveVersion)
		if a.How != "" {
			add("           re-evidence: %s\n", a.How)
		}
	}
	for _, a := range s.Missing {
		add("  MISSING  %s — listed in %s but not on disk\n", a.Path, LockPath)
	}
	for _, m := range s.DigestMismatch {
		add("  EDITED   %s — bytes changed without regenerating %s\n", m.Artifact.Path, LockPath)
		add("           want sha256 %s\n", m.Want)
		add("           got  sha256 %s\n", m.Got)
		add("           regenerate: go test ./internal/referencedrift/ -run TestReferenceLock -update\n")
	}
	return string(b)
}
