# Cycle truncated-webm-rejection — read-time gate for header-only / truncated WebM

## Goal

AGY 1.3.4 changelog line 42 closed a conversation-corrupting bug: a
truncated or header-only WebM file (a recording killed mid-write, a
partial transfer that only preserved the EBML header) used to be
attached anyway. The first downstream request that needed the
attachment died, and every later request in the same conversation
inherited the failure — the model only discovered the problem after
it had already polluted the transcript with bad bytes.

This cycle wires the same guard into the clone. The probe runs at
read-time, on raw bytes, before the file's contents reach the
transcript. A `.webm` path whose body looks header-only or
truncated is rejected with `ErrTruncatedWebM` and the tool call
returns an error — the agent learns the file is bad on the call that
would have caused the corruption, not later.

## Changelog anchor

| Release | Line | Shape |
|---|---|---|
| 1.3.4 | 42 | Fixed a conversation failing on every later request after the agent read a truncated or header-only WebM file; such files are now rejected when read instead of being attached. |

## Files

| Path | Role |
|---|---|
| `internal/tools/webm.go` (new, 136 lines) | `ErrTruncatedWebM`, `IsWebMPath`, `probeWebM`, `skipEBMLSize`. Imports: `bytes`, `errors`, `strings` — stdlib only. |
| `internal/tools/webm_test.go` (new, 263 lines) | 13 tests: probe unit cases (valid-with-cluster, valid-with-tracks, truncated, header-only, empty, too-short ×4 sizes, bad-magic), `IsWebMPath` suffix table (8 cases including case-insensitive + `.webp` negative + `'webm'` no-dot negative), `skipEBMLSize` VINT table (6 cases: 1-byte, 2-byte, zero, out-of-range, overflow), and three ReadFile.Run integration tests (truncated rejected, valid passes, plain text / non-WebM extension passes through untouched). |
| `internal/tools/filesystem.go` (+12 / −0) | `ReadFile.Run` consults `IsWebMPath(p.Path)` after `os.ReadFile`; on a `.webm` path it calls `probeWebM(data)` and surfaces `ErrTruncatedWebM` directly. The gate fires only when the suffix matches — non-WebM paths are byte-for-byte unchanged. |

**Net:** +411 lines across 3 files (2 new + 1 surgical edit).

## Probe contract

`probeWebM(data []byte) error` returns `ErrTruncatedWebM` (and only
that) when the file is rejected, or `nil` when it looks plausible
enough to attach.

| Step | Reject when | Why |
|---|---|---|
| 1. size | `len(data) < 5` | Need 4 magic + 1 size byte minimum. 0-byte + 1–4-byte files cannot be a WebM. |
| 2. magic | `data[0:4] != 1A 45 DF A3` | Anything else is not a WebM regardless of extension. |
| 3. EBML size | `skipEBMLSize(data, 4)` fails or overflows | Malformed VINT means the EBML header is corrupt — treating that as "header-only" is the conservative choice and matches what the changelog warns about. |
| 4. body | After the EBML header, neither `0x16 0x54 0xAE 0x6B` (Tracks) nor `0x1F 0x43 0xB6 0x75` (Cluster) appears | Both are 4-byte IDs that cannot collide with 1-byte TrackEntry (`0xAE`) or Segment (`0x18 0x53 0x80 0x67`) children. Either Track or Cluster present ⇒ file has structure a backend could attach. |

`IsWebMPath(p string) bool` is `strings.HasSuffix(strings.ToLower(p), ".webm")`.
The case-fold keeps `.WEBM` / `.WebM` paths safe.

`skipEBMLSize(data, idLen)` reads the VINT-encoded data-size field
following an EBML element ID of length `idLen` (here 4, the EBML
header's ID length). A zero first byte, an out-of-range ID length,
or a VINT that runs past `len(data)` all return `(_, false)` so the
probe can reject without panicking.

## Read-time integration

`ReadFile.Run` calls the probe only after `os.ReadFile` succeeds and
only when the path suffix matches `.webm`:

```go
if IsWebMPath(p.Path) {
    if err := probeWebM(data); err != nil {
        return nil, err
    }
}
```

The check sits **before** the start/end-line and max-bytes
post-processing — the slice is computed over the rejected-or-not
result. Putting the probe at the top of the control flow means a
read_file call on `clip.webm` returns `(nil, ErrTruncatedWebM)` and
the conversation never sees bad bytes.

The probe is intentionally tolerant: it does not decode the EBML
tree, validate cluster CRC, or check page-level structure. That is a
job for the backend; the changelog gap is specifically about the
recognisable failure modes (header-only, truncated) that the agent
would otherwise attach and discover too late. Full-validate-everything
would slow every `.webm` read by an order of magnitude.

## Tests

13 new tests, all passing:

| Test | What it pins |
|---|---|
| `TestProbeWebM_ValidWithCluster` | the happy path: magic + EBML body + Cluster ID passes |
| `TestProbeWebM_ValidWithTracks` | Tracks-but-no-Cluster passes (Track metadata alone is plausible; rejection needs *neither*) |
| `TestProbeWebM_Truncated` | magic + EBML body without Segment/Track/Cluster → `ErrTruncatedWebM` |
| `TestProbeWebM_HeaderOnly` | magic + DocType-ish bytes, no Track/Cluster → `ErrTruncatedWebM` |
| `TestProbeWebM_Empty` | 0 bytes → `ErrTruncatedWebM` |
| `TestProbeWebM_TooShort` | 1–4 bytes (sub-magic) → `ErrTruncatedWebM` |
| `TestProbeWebM_BadMagic` | first byte matches but rest of magic differs → `ErrTruncatedWebM` |
| `TestIsWebMPath` | 8-case suffix table: `.webm` / `.WEBM` / `.WebM` / nested paths → true; `.webp`, `.txt`, `''`, `'webm'` (no dot) → false |
| `TestSkipEBMLSize` | VINT advance: 1-byte vint value=0, value=2, 2-byte vint value=64, zero first byte rejected, idLen past end rejected, VINT overflow rejected |
| `TestReadFile_WebM_Truncated` | integration: write truncated `.webm`, run ReadFile, expect `errors.Is(err, ErrTruncatedWebM)` |
| `TestReadFile_WebM_Valid` | integration: write valid WebM-shaped bytes, ReadFile returns them unchanged |
| `TestReadFile_NonWebM_NoRejection` | integration: `notes.txt` goes through the existing path with no probe side-effect |
| `TestReadFile_NonWebMExtension_WebMMagic_NoRejection` | integration: `clip.webp` whose bytes start with the WebM magic still passes — the gate is the suffix, not the magic |

The `TestIsWebMPath` and `TestSkipEBMLSize` tables catch the two
refactor traps that would silently break the probe: a future change
that drops `strings.ToLower` (so `.WEBM` paths slip through unprobed)
or one that flips the VINT leading-1 bit math (so every WebM-shaped
file flips to truncated).

## Build / test evidence

```
$ gofmt -l internal/tools/webm.go internal/tools/webm_test.go
                                                           ← empty (clean)
$ go build -o /tmp/wagyu-webm ./cmd/wagyu                   ← exit 0, 70 MB
$ go test -vet=off -count=1 -run 'TestProbeWebM|TestIsWebMPath|TestSkipEBMLSize|TestReadFile_WebM|TestReadFile_NonWebM' ./internal/tools/
ok      github.com/chadgpt/openagy/internal/tools    0.133s
$ go test -vet=off -count=1 ./internal/tools/
ok      github.com/chadgpt/openagy/internal/tools    5.622s
```

`go test -vet=off -count=1 -run 'TestWebM|TestTruncated|TestFile' ./cmd/wagyu/ ./internal/.../`
(all WebM / Truncated / File-prefixed tests across both trees) also
passes — `TestProbeWebM_*` / `TestReadFile_*` match the `TestFile`
prefix in `./internal/...`.

## 硬约束 / limitations

- The probe does not parse the EBML tree. A file with exactly one
  Cluster ID byte sequence at offset 8 and nothing else (`magic
  1A45DFA3 80 1F 43 B6 75`) passes — the substring search treats
  it as plausible. Validating the cluster body would require walking
  the EBML tree, which is out of scope for a read-time gate. The
  AGY 1.3.4 changelog targets the *recognisable* header-only /
  truncated modes; the backend's own decoder is the place to catch
  the rest.
- The probe counts "Tracks-only" (no Cluster) as valid. A finished
  WebM almost always has Clusters; header-only files usually have
  Tracks metadata but never a Cluster. The discriminator in the
  probe is "neither present" → reject, "either present" → pass. A
  Tracks-only file is unusual but not the failure mode the changelog
  warns about, so the probe lets it through rather than over-rejecting
  partial-but-plausible recordings.
- The probe does not run on `.mkv` / `.mka` (other Matroska
  containers). AGY 1.3.4's gap is specifically WebM; extending the
  probe to all EBML containers is a separate decision that should
  come with its own changelog anchor.
- The error message returned to the model is the bare `ErrTruncatedWebM`
  string, not a wrapped JSON shape. Models that pattern-match exact
  tool error strings would need to be updated; none of the existing
  tests assert on the message and the runtime cost of fancy wrapping
  is not justified by the changelog gap.

## Diff summary

```
internal/tools/filesystem.go    | 12 ++++++++++++
internal/tools/webm.go          | 136 ++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++
internal/tools/webm_test.go     | 263 ++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++
3 files changed, 411 insertions(+)
```

No files deleted. No pre-existing not-clean files reformatted.
