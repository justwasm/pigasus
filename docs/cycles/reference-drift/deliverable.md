# Cycle `reference-drift` — the parity claim had a shelf life and nothing measured it

This clone's central correctness claim is byte-level parity with `agy X.Y.Z`. That claim has an
expiry date that nothing tracked. Google ships `agy` updates automatically: between the last
cycle on this branch and the moment this cycle started, `~/.local/bin/agy` self-updated from
**1.4.2 to 1.4.3** — at 16:03, with nobody in this repository choosing to.

What happened to the test suite in response: **nothing visible.** One test out of ~3,650 went red.
The remaining 3,649 passed. That is worse than a red suite, because it is *confident* wrongness.

## What was actually stale

Three committed artifacts are scoped to a particular reference version. Before this cycle:

| Artifact | Captured at | Checked against the live binary? |
|---|---|---|
| `internal/cli/testdata/changelog_reference.txt` (160 KB) | 1.4.2 + cycle-235 stub | **yes** — `TestChangelog_MatchesReferenceBinary` compares bytes |
| `internal/agyprompt/testdata/capture_1.4.2.txt` (34 KB system prompt) | 1.4.2 | **no** — only compared against other committed artifacts |
| `internal/tools/testdata/advertised_tools_1.3.6.txt` (60 tool names) | 1.3.6 | **no** — same |

The changelog caught the drift *by accident*. Its test asserts a byte-equality property, so an
upstream edit happens to violate it. The other two are pinned against their own recorded neighbours,
which is a self-consistency check, not a parity check: they would have stayed green indefinitely no
matter what upstream did. `AGENTS.md` even said so out loud — *"releasing a new agy invalidates this
fixture, nothing detects it"* — and the detector did not exist.

## The drift itself

1.4.3 ships the real 1.4.2 release notes, which 1.4.2 did not have (the binary's notes always lag
its own version by one release). Cycle 235 had prepended a hand-written placeholder bullet for that
section because there was nothing real to capture:

```
· (release notes not yet recovered from AGY binary; see docs/cycles/cycle-235-changelog for status)
```

is now

```
· Fixed conversation history not showing in Remote Control in daemon mode.
```

One line. **Everything below it is byte-identical** — verified by diffing the fresh 1.4.3 capture
against the old golden from the `1.4.1:` header down:

```
$ diff <(tail -n +4 old) <(tail -n +4 new) && echo IDENTICAL
IDENTICAL
```

Section count (73), bullet count (796) and line count (941) are all unchanged. Byte length moved
160,422 → 160,397 and the SHA-256 moved to `dc224b98…`.

The old test also had a latent bug this exposed. It stripped a `"1.4.2:\n"` prefix from the embedded
blob before comparing to `agy changelog`, because under 1.4.2 the binary's notes started at `1.4.1`.
Under 1.4.3 they start at `1.4.2:`, so the strip was comparing live-1.4.2-against-a-1.4.1-blob —
the comparison had quietly become meaningless for a whole release. The strip is now gone; with the
placeholder retired the embedded blob is a verbatim capture, so the gate is straight byte equality.

## What this cycle ships

### 1. `internal/referencedrift` — the detector

A new package holding the evidence registry and the gate. `reference.lock.json` records:

- `version` — the reference release the clone is pinned to (1.4.2).
- `lag_acknowledged_through` — the newest upstream release we have **consciously decided not to
  re-evidence**, with a note saying why.
- `artifacts[]` — every version-scoped fixture, its `captured_at`, its SHA-256, and how to re-verify.

Three classes of finding, deliberately not equally severe:

| Finding | Meaning | Verdict |
|---|---|---|
| digest mismatch / missing artifact | this repository is internally inconsistent | **fail** |
| live binary past `lag_acknowledged_through` | nobody has looked at this release | **fail** |
| live binary within the acknowledged ceiling | we know we are behind, and said so | pass, and report which evidence is suspect |

The load-bearing design decision is the middle row. The gate is **not** "live version must equal the
pin". Lagging upstream is a legitimate, even expected, state for a reverse-engineering workspace — you
cannot re-capture a 34 KB system prompt every time Google ships a patch release. A gate phrased that
way would go permanently red, get `-short`-ed or skipped within a cycle or two, and recreate the exact
false confidence it exists to remove.

So the acknowledgement is a committed JSON field, not an env var and not a `t.Skip`. Accepting drift
is a **decision with a diff**: `lag_acknowledged_through` names the newest release we looked at and
declined, `lag_note` says why, and `git blame` names who. The failure mode is a red test that says
what to do next, not a skipped test that says nothing.

`CompareVersions` does numeric ordering, not string comparison. This is not fastidiousness — the naive
comparison makes `1.4.10 < 1.4.9`, so a patch release reads as *older* than the pin and drift walks
straight through the gate. There is a test pinning exactly that pair.

`TestReferenceLock_ArtifactCaptionsAreCoherent` enforces the one invariant that is genuinely
load-bearing: no artifact may be stamped from a release *newer* than the installed binary. Evidence
from a binary we cannot actually run is not evidence.

### 2. Re-evidencing the changelog

Since the changelog was the one artifact re-verifiable by invoking `agy` directly, this cycle does
that rather than only writing a detector about it:

- `changelog_reference.txt` is now a verbatim `agy changelog` capture from 1.4.3.
- The byte-length / line-count / SHA-256 constants are re-pinned.
- The version arithmetic in the live gate is deleted; it is plain byte equality now.
- `TestChangelogReference_142PlaceholderIsVerbatim` becomes
  `TestChangelogReference_142BulletIsUpstream`, pinning the reference's **real** release note. It also
  asserts the placeholder text is *gone*, so the cycle-235 TODO cannot silently come back.

A parity gate replaced a bookkeeping marker.

### 3. Deduplicated binary resolution and a pin cross-check

`internal/cli` had its own `findReferenceBinary`. There are now two copies of "where is the upstream
binary", which is the same duplication that let the version pin drift in the first place. The changelog
gate now calls `referencedrift.FindBinary`, so the two cannot disagree about which binary they are
measuring against.

`TestReferenceVersion_MatchesDriftLock` pins `cli.ReferenceVersion` (a Go constant, the string every
user-visible version in the clone renders) to `version` in the lock (JSON). Two declarations of one
fact, hand-kept-in-step, which is a failure mode this repository has already demonstrated — STATUS.md
and README.md sat ~100 cycles and ~40 versions out of date. It gets a test.

## Verifying the detector actually fires

A green gate proves nothing on its own. All three failure modes were exercised against the real
package:

```
# (a) unacknowledged upstream drift — a stub binary reporting 1.4.4
$ printf '#!/bin/sh\necho 1.4.4\n' > /tmp/fake && chmod +x /tmp/fake
$ AGY_REF=/tmp/fake go test ./internal/referencedrift/ -run TestReferenceLock_NoUnacknowledgedDrift
--- FAIL: TestReferenceLock_NoUnacknowledgedDrift
    reference drift is not acknowledged: /tmp/fake is at 1.4.4, past the recorded ceiling "1.4.3".
      reference-drift: live /tmp/fake is 1.4.4; this clone is pinned to 1.4.2 (lag acknowledged through 1.4.3).
      SUSPECT  internal/cli/testdata/changelog_reference.txt — captured at agy 1.4.3, unverified against 1.4.4
               re-evidence: agy changelog > internal/cli/testdata/changelog_reference.txt, ...
      SUSPECT  internal/agyprompt/testdata/capture_1.4.2.txt — captured at agy 1.4.2, ...
      SUSPECT  internal/tools/testdata/advertised_tools_1.3.6.txt — captured at agy 1.3.6, ...

# (b) a fixture edited without regenerating the lock
$ echo a_tool_that_does_not_exist >> internal/tools/testdata/advertised_tools_1.3.6.txt
$ go test ./internal/referencedrift/ -run TestReferenceLock_RepositoryIntegrity
--- FAIL: TestReferenceLock_RepositoryIntegrity
    EDITED  internal/tools/testdata/advertised_tools_1.3.6.txt — bytes changed without regenerating ...
      want sha256 46dcfca2…   got sha256 acd2c7bc…
    regenerate: go test ./internal/referencedrift/ -run TestReferenceLock -update

# (c) a machine with no reference binary — digests still checked, drift gate skips
$ AGY_REF= HOME=/tmp/nohome go test ./internal/referencedrift/ -run TestReferenceLock
ok
```

`-update` is idempotent: running it against an unmodified tree produces an empty diff.

## Effect

| | before | after |
|---|---|---|
| detectors for reference drift | 1 of 3 artifacts, accidental | 3 of 3, deliberate |
| version-pinned artifacts with a recorded capture version | 0 of 3 | 3 of 3 |
| artifacts with a committed SHA-256 | 1 of 3 | 3 of 3 |
| known-stale evidence | silent | named, with re-evidence instructions |
| changelog evidence | 1.4.2 + invented bullet | verbatim 1.4.3 capture |

Suite: **66 packages, 6,716 test cases, green.** `go build ./cmd/wagyu` → 102,078,354 bytes.

New code is 657 lines across four files in `internal/referencedrift`, gofmt-clean.

## Known gaps — this cycle does not close them

- **`capture_1.4.2.txt` and `advertised_tools_1.3.6.txt` are still unverified against 1.4.3.** They
  are recorded as acknowledged lag, which is honest but is not the same as verified. Closing them
  needs a cycle-274 capture-wrapper run against 1.4.3 (`cmd/agy` + `devutils/agyproxy`) — mechanical
  but a separate session's work, not something to fake from a diff.
- **`rc-wire-live-verify` remains the largest correctness risk** in the repo. Per
  `TODO-rc-parity.md` Priority 1, the RC harness replays our wire against our wire; the real field
  names, types and nullability are inferred from Go naming habits at ~64% coverage. The drift gate
  says nothing about that and cannot.
- **`cli.ReferenceVersion` is still 1.4.2**, and that is correct rather than an oversight: the
  changelog is the only surface re-evidenced at 1.4.3. Bumping the pin would claim parity across the
  other 30-odd surfaces that is not there.
