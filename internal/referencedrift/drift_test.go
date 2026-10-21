package referencedrift_test

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/referencedrift"
)

var update = flag.Bool("update", false, "rewrite reference.lock.json digests from the fixtures on disk")

// fixture loads the lock and resolves it against the repository root.
func fixture(t *testing.T) (*referencedrift.Lock, referencedrift.Status, string) {
	t.Helper()
	l, err := referencedrift.Load()
	if err != nil {
		t.Fatalf("%v", err)
	}
	root, err := referencedrift.RepoRoot(".")
	if err != nil {
		t.Fatalf("%v", err)
	}
	if *update {
		if err := referencedrift.Refresh(root, l); err != nil {
			t.Fatalf("%v", err)
		}
		if err := referencedrift.WriteLock(root, l); err != nil {
			t.Fatalf("write %s: %v", referencedrift.LockPath, err)
		}
		t.Logf("rewrote %s digests for %d artifact(s)", referencedrift.LockPath, len(l.Artifacts))
	}
	return l, referencedrift.Resolve(root, l), root
}

// TestReferenceLock_RepositoryIntegrity is the half of the gate that does not
// depend on upstream at all: every artifact the lock names must exist, and
// its bytes must still hash to the recorded digest. A failure here is a bug in
// this repository — someone edited a fixture and did not regenerate the lock,
// or deleted one outright — and it is reported regardless of whether a
// reference binary is installed.
func TestReferenceLock_RepositoryIntegrity(t *testing.T) {
	_, st, _ := fixture(t)
	if !st.RepoBroken() {
		t.Log("all artifact digests match the lock")
		return
	}
	for _, a := range st.Missing {
		t.Errorf("MISSING %s — listed in %s but not on disk", a.Path, referencedrift.LockPath)
	}
	for _, m := range st.DigestMismatch {
		t.Errorf("EDITED  %s — bytes changed without regenerating %s\n  want sha256 %s\n  got  sha256 %s",
			m.Artifact.Path, referencedrift.LockPath, m.Want, m.Got)
	}
	t.Errorf("%d artifact(s) drifted from the lock; regenerate with:\n  go test ./internal/referencedrift/ -run TestReferenceLock -update",
		len(st.Missing)+len(st.DigestMismatch))
}

// TestReferenceLock_NoUnacknowledgedDrift is the half that does depend on
// upstream. The reference binary self-updates; this is what notices.
//
// The contract is deliberately not "the live version must equal the pin" —
// lagging upstream is a legitimate state for a reverse-engineering workspace,
// and a gate that went permanently red on every Google patch release would get
// skipped and recreate the exact problem it exists to fix. Instead lagging is
// *recorded*: lag_acknowledged_through in the lock names the newest release we
// have consciously decided not to re-evidence, with a note saying why. Failing
// is reserved for drift past that ceiling, which means nobody has looked.
//
// Without a reference binary there is no verdict to give, so the gate skips
// rather than passing — the difference matters on a machine that cannot know.
func TestReferenceLock_NoUnacknowledgedDrift(t *testing.T) {
	l, st, _ := fixture(t)
	switch {
	case st.Binary == "":
		t.Skip("no reference `agy` binary found (set $AGY_REF to enable this gate); " +
			"evidence digests were still checked by TestReferenceLock_RepositoryIntegrity")
	case st.VersionErr != nil:
		t.Errorf("could not read the version of %s: %v", st.Binary, st.VersionErr)
	case st.Unacknowledged():
		// The report is already the message; log it once, not twice.
		t.Errorf("%s\nEither re-evidence the artifacts above and bump the lock, or — if chasing this "+
			"release is deliberately out of scope — raise lag_acknowledged_through to %s and say why in lag_note.",
			strings.TrimRight(referencedrift.Report(st, l.LagNote), "\n"), st.LiveVersion)
		return
	}
	if st.Empty() {
		return
	}
	// Acknowledged lag, or a machine whose binary could not be versioned:
	// nothing failed, but the evidence registry has a story worth telling.
	t.Log("\n" + strings.TrimRight(referencedrift.Report(st, l.LagNote), "\n"))
}

// TestReferenceLock_ArtifactCaptionsAreCoherent guards the registry's own
// bookkeeping. Two invariants are worth enforcing:
//
//   - A live-verifiable artifact must carry re-verification instructions.
//     Otherwise the next person to re-evidence it has to reverse-engineer
//     the capture procedure from scratch, which is how fixtures end up
//     rotting quietly in the first place.
//   - When a reference binary is present, no artifact may be stamped from a
//     release *newer* than the one installed. That would mean the evidence
//     came from a binary this machine has never actually run -- the same
//     class of silent untrustworthiness this package exists to prevent.
//
// Deliberately NOT required: captured_at need not equal the lock's pin. The
// changelog is legitimately captured at 1.4.3 while the clone's behavioural
// pin is still 1.4.2. Re-evidencing one artifact from a newer release than
// the pin is a bonus, not a contradiction -- the two fields answer different
// questions ("what do we emulate" vs "when was this recorded"), and the
// whole reason they are separate fields is so a changelog re-capture does not
// force a lie about the other 30-odd surfaces still modelled on 1.4.2.
func TestReferenceLock_ArtifactCaptionsAreCoherent(t *testing.T) {
	l, st, _ := fixture(t)
	for _, a := range l.Artifacts {
		if a.Live && a.How == "" {
			t.Errorf("%s is marked live but carries no re-verification instructions", a.Path)
		}
		if st.LiveVersion == "" {
			continue
		}
		if referencedrift.CompareVersions(a.CapturedAt, st.LiveVersion) > 0 {
			t.Errorf("%s is stamped %s but the installed reference is %s -- evidence from a newer binary than we can run is not evidence",
				a.Path, a.CapturedAt, st.LiveVersion)
		}
	}
}

// TestLockParse_RejectsIncoherentLocks checks the shape validation that keeps
// a typo'd lock from silently disabling the gate.
func TestLockParse_RejectsIncoherentLocks(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		body string
		want string
	}{
		{"no version", `{"artifacts":[]}`, "no version"},
		{"duplicate path", `{"version":"1.4.2","artifacts":[{"path":"a","captured_at":"1.4.2"},{"path":"a","captured_at":"1.4.2"}]}`, "twice"},
		{"missing captured_at", `{"version":"1.4.2","artifacts":[{"path":"a"}]}`, "missing path or captured_at"},
		{"lag not newer than pin", `{"version":"1.4.2","lag_acknowledged_through":"1.4.1","artifacts":[]}`, "not newer than"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(dir, tc.name+".json")
			if err := os.WriteFile(p, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := referencedrift.LoadFrom(p); err == nil {
				t.Fatalf("LoadFrom(%s) accepted an incoherent lock", tc.name)
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// TestCompareVersions pins the ordering rule. The string comparison trap this
// guards against is real: "1.4.10" < "1.4.9" lexicographically, so a naive
// implementation would let a patch release read as older than the one we
// pinned and wave drift straight through.
func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.4.3", "1.4.2", 1},
		{"1.4.2", "1.4.3", -1},
		{"1.4.2", "1.4.2", 0},
		{"1.4.10", "1.4.9", 1},
		{"1.10.0", "1.9.0", 1},
		{"1.4", "1.4.0", 0},
		{"2.0.0", "1.99.99", 1},
		// A pre-release suffix is not an integer, so it sorts *before* the
		// release it leads up to -- the same direction semver puts it.
		{"1.4.2-rc1", "1.4.2", -1},
		{"1.4.2", "1.4.2-rc1", 1},
		{"", "1.0.0", -1},
		{"garbage", "1.0.0", -1},
	}
	for _, tc := range cases {
		if got := referencedrift.CompareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
