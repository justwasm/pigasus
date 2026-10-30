# Cycle 365 deliverable: the clone's version number against agy 1.4.11

**Cycle:** `cycle-365-agy-1.4.11-version-alignment`
**Subject:** cycle 364 mapped agy 1.4.11's *behaviour* onto the clone. It left
the clone's **version number** behind: the behavioural pin still said `1.4.10`,
the npm wrapper still downloaded the `v1.4.10` release, the wasm runner still
called itself `1.4.10-wasm`, and the served changelog fixture was the 1.4.10
binary's capture. This cycle moves every version statement onto 1.4.11 and
re-captures the one artefact that has to come from the binary itself.
**Evidence:** 【一手】 first-hand against the 1.4.11 binary — `agy --version`,
`agy changelog`, its sha256 · 【strings】 string-table hits only · 【推测】
hypothesis

---

## 0. The short answer

| # | what | the clone |
|---|---|---|
| 1 | the behavioural pin — `internal/cli.ReferenceVersion` | **1.4.11** (§2) |
| 2 | the drift lock — `internal/referencedrift/reference.lock.json` | **re-pinned to 1.4.11**, the lag acknowledgement removed (§3) |
| 3 | the served changelog feed — `internal/cli/testdata/changelog_reference.txt` | **re-captured** from the 1.4.11 binary: 186,601 B / 1,062 lines / 81 sections / 901 bullets (§4) |
| 4 | the npm wrapper — `npm/pigasus/` | **1.4.11** (`version` + `pigasusVersion`), the hardcoded fallback moved off 1.4.6 (§5) |
| 5 | the wasm runner — `cmd/pigga` | **`1.4.11-wasm`** (§6) |
| 6 | the user-visible statements — `README.md`, `AGENTS.md` | **1.4.11** (§7) |
| 7 | the **RE pipeline** (seeds, Ghidra project, decompiled output) | **not regenerated** — every address moves on every release, so it is a separate cycle; the lock reports those artefacts as SUSPECT (§8) |

---

## 1. Why the version number is not a cosmetic edit

The clone has exactly **one** behavioural version pin:
`internal/cli.ReferenceVersion`. It is not a display string — three gates read
it:

- `internal/cli/changelog_data_test.go`'s `TestReferenceVersion_MatchesDriftLock`
  asserts `cli.ReferenceVersion` equals the drift lock's `version`. Both must
  move together or the repository does not build green.
- `internal/cli/changelog_data_test.go`'s live comparison
  (`TestChangelog_MatchesLiveBinary`) **skips** when the installed binary's
  version differs from the pin. Before this cycle the pin said 1.4.10 and the
  binary said 1.4.11, so the one test that would have caught a stale fixture
  was silently skipping. Bumping the pin **re-enables** it, and it immediately
  failed — because the fixture was the 1.4.10 binary's capture.
- `internal/agyupdate` reads the pin as the updater's `CurrentVersion`, so the
  "a newer version is available" line is computed against it.

【一手】 the installed reference: `~/.local/bin/agy --version` → `1.4.11`,
sha256 `09cefed6a1d217e14ba1751dce4442b97c2797e88c297c77fda3690f92ceddcd`.

So the bump is not "change a string in three places". It is: move the pin,
move the lock with it, and re-derive the one artefact the pin makes
testable.

## 2. The pin

`internal/cli/version.go`:

```go
const ReferenceVersion = "1.4.11"
```

Its doc comment names the cycle, the same way the 1.4.9 and 1.4.10 bumps did
(cycle 362, cycle 363). `pigasus --version` now prints `1.4.11`, byte for byte
what the reference prints.

## 3. The drift lock

`internal/referencedrift/reference.lock.json` carried:

```json
"version": "1.4.10",
"lag_acknowledged_through": "1.4.11",
```

The lag acknowledgement is how this workspace says "I know the binary moved
past my pin; I have looked at that release and this is what I chose not to
do". It is only legal while the binary is **strictly newer** than the pin —
the loader rejects a `lag_acknowledged_through` that is not newer than
`version`, because a lag that covers the pin is a lie.

Once the pin moved to 1.4.11 the acknowledgement had to go, and its
`lag_note` was rewritten to say what the gate now actually reports: the
changelog feed was re-captured from the 1.4.11 binary, and **every other
artefact keeps its own `captured_at`**, so the gate lists each of them as
SUSPECT.

The lock was re-sealed with the gate's own writer, not by hand:

```
go test ./internal/referencedrift/ -run TestReferenceLock -update
```

(hand-editing the JSON un-escapes the `<`/`>`/`&` that Go's
`json.MarshalIndent` escapes, and dirties the diff for no reason).

The gate's report at 1.4.11, verbatim from `go test ./internal/referencedrift/`:

```
reference-drift: live ~/.local/bin/agy is 1.4.11; this clone is pinned to 1.4.11.
  SUSPECT  internal/agyprompt/testdata/capture_1.4.6.txt — captured at agy 1.4.8, unverified against 1.4.11
  ...
```

That is the honest state: **one** artefact is current, the rest are not.

## 4. The served changelog feed — re-captured

【一手】 the 1.4.11 binary's own feed:

```
agy changelog > internal/cli/testdata/changelog_reference.txt
```

| | 1.4.10 capture | 1.4.11 capture |
|---|---|---|
| bytes | 184,882 | **186,601** |
| lines | 1,054 | **1,062** |
| sections | 80 | **81** |
| bullets | 895 | **901** |
| newest section | 1.4.9 | **1.4.10** |
| sha256 | `a9c4ce87…` | `209a0518b09896b97bb053ab3e352442f7a050bf08373105f67a23455ba411f1` |

The feed **gained the 1.4.10 section** (8 lines, 6 bullets). It still does
**not** carry the 1.4.11 section: the served feed lags the vendor's own
repository by exactly one release, which is the reference's own behaviour and
has held since 1.4.2. The 1.4.11 notes are recorded verbatim in
`internal/cli/testdata/changelog_upstream_1.4.11.md`, the way the 1.4.8, 1.4.9
and 1.4.10 notes were.

Three byte-level checks pin the capture, and all three are green:

- `agy changelog` (1.4.11 binary) == the fixture == `pigasus changelog`, all
  three sha256 `209a0518…` 【一手】;
- the five constants in `internal/cli/changelog_data_test.go` (byte length,
  line count, section count, bullet count, sha256);
- `TestChangelogReference_NewestBulletIsUpstream` in
  `internal/cli/changelog_v133_test.go`, whose expected head moved from
  `1.4.9:` to `1.4.10:`.

## 5. The npm wrapper

`npm/pigasus/package.json` carries two version fields and they answer
different questions:

- `version` — the npm package's own version;
- `pigasusVersion` — the **GitHub release tag** the wrapper downloads
  (`v${pigasusVersion}`), i.e. the release built from this clone.

Both moved to `1.4.11`. The wrapper's hardcoded fallback in
`npm/pigasus/lib/download.js` was still `'1.4.6'` — four releases behind —
and moved to `'1.4.11'` as well.

**The release asset does not exist yet.** 【一手】 the release URLs, measured:

| tag | HTTP |
|---|---|
| `v1.4.9` | 200 |
| `v1.4.10` | 200 |
| `v1.4.11` | **404** |

The GitHub release is created by the tag-push workflow
(`.github/workflows/release.yml`), which derives the tag from
`internal/cli/version.go`. So the npm bump **ships ahead of the asset**: a
user who installs the 1.4.11 wrapper before the tag exists gets a 404 from
the download. That is the same ordering the 1.4.10 bump had, and it is
accepted deliberately — the wrapper's version is the *promise*, the asset is
the fulfilment, and the tag cannot exist before the commit that bumps the
version.

## 6. The wasm runner

`cmd/pigga/main.go`'s `var version = "1.4.10-wasm"` → `"1.4.11-wasm"`, with
`cmd/pigga/pigga_test.go`'s expectation updated to match. The `-wasm` suffix
is the runner's own marker; it is not the reference's version and is not
compared against anything.

## 7. The user-visible statements

- `README.md` — two places (the header banner and the "当前对齐" line) still
  said **1.4.9**, one release behind even the old pin. Both now say
  **1.4.11**.
- `AGENTS.md` — the "The clone emulates **agy 1.4.11**" line.

## 8. What was *not* re-derived — and why that is the honest state

The RE pipeline is **not** regenerated at 1.4.11. Every address moves on every
release (invariant 15 in `AGENTS.md`), so re-anchoring the seeds, rebuilding
the Ghidra project and re-decompiling 2,366 priority functions is its own
cycle — that is what cycle 363 did for 1.4.10, and it is measured in hours,
not minutes.

The consequence is recorded rather than hidden: the lock artefacts
`out/local-macos/seeds/seeds.txt`, `out/local-macos/seeds/funcs.txt` and
`out/local-macos/seeds/diff_1.4.8_1.4.10.txt` carry `captured_at: 1.4.10`,
and the gate reports them — and the twenty-odd capture fixtures stamped
1.4.6/1.4.7/1.4.8 — as **SUSPECT**: recorded from an older binary,
unverified against 1.4.11.

A stale artefact is a **known unknown**; a silently re-stamped one is a
false claim. The lock's job is to keep the difference visible.

## 9. The stale `changelog_reference.txt:<line>` citations

The re-capture moved every line in the fixture by the size of the new 1.4.10
section (+8), and the *older* citations were **already** stale before this
cycle (they were written against the 1.4.10-era fixture, whose line numbers
were already one section behind). A citation like
`internal/cli/testdata/changelog_reference.txt:12` is a pointer into a file
that is regenerated on every release; the line number is a snapshot, and the
quote next to it is the durable part.

The quotes were re-checked against the new capture: every one of them still
names the same upstream bullet. The line numbers were left as they were
rather than bumped, because re-pointing them would be a second, unrelated
change spread over a dozen files — several of which belong to the other
agent working in this tree. **The quotes are the evidence; the line numbers
are a convenience.**

---

## Hard constraints

1. **`internal/cli.ReferenceVersion` and the drift lock's `version` must move
   together.** `TestReferenceVersion_MatchesDriftLock` fails otherwise.
2. **`lag_acknowledged_through` must be absent when the pin equals the live
   binary's version.** The loader rejects a lag that is not strictly newer
   than the pin.
3. **The lock file must be written by Go's `json.MarshalIndent`**, i.e.
   through the gate's `-update` path. Hand-editing un-escapes the `<`/`>`/`&`
   that Go escapes and dirties the diff.
4. **The changelog fixture and its five constants move together** — byte
   length, line count, section count, bullet count, sha256.
5. **The clone's `changelog` output must stay byte-identical to the
   reference's.** The feed lags by one release on purpose; "helpfully"
   prepending the vendor's repository notes would make the clone print
   something the reference does not.
6. **The npm wrapper's `pigasusVersion` names a GitHub release tag.** The
   tag must exist before the wrapper is published, or the download 404s.
7. **Local commits only.** Nothing in this cycle is pushed.

## Open questions / 存疑

- **The npm 1.4.11 release does not exist yet** (404 at the time of
  writing). The wrapper bump ships ahead of it by construction (§5).
- **The 1.4.11 section of the served feed is upstream's, not the
  binary's.** The binary's feed ends at 1.4.10; the vendor's repository
  carries 1.4.11. The clone prints the binary's bytes, and the repository's
  notes are recorded separately.
- **Whether the RE pipeline should be regenerated at 1.4.11** is a separate
  decision, not this cycle's. It is the last big piece of a true 1.4.11
  alignment.
- **The stale line-number citations** (§9) are left as they are. They were
  stale before this cycle; fixing them properly means making the citations
  quote-based (search the bullet, not the line), which is a refactor of the
  citation convention itself.

## Limitations

- **The RE pipeline was not re-run.** Every address in the repository still
  refers to a 1.4.10-or-older binary. The lock reports this; the repository
  does not pretend otherwise.
- **No 1.4.11 session was rendered.** The version alignment was verified
  through `--version`, `changelog`, the drift gate and the test suite, not
  through a live TUI run.
- **The npm wrapper was not end-to-end installed** against a published
  1.4.11 asset, because there is none. The `package.json` fields and the
  download URL construction were checked by hand; the 404 is a fact about
  the release, not about the wrapper.
- **The twenty-odd capture fixtures stamped 1.4.6/1.4.7/1.4.8 are
  unverified against 1.4.11** — the gate lists each of them as SUSPECT.
  Whether any of them actually moved is unknown; "unknown" is the state the
  lock exists to express.

---

Report language note: this file is English because `docs/cycles/*` is
English. The version statements themselves (`README.md`, `AGENTS.md`) follow
their own files' language.
