# Cycle 369 deliverable: the capture's size ceiling

**Cycle:** `cycle-369-capture-rotation`
**Subject:** the proxy's capture directory had **no ceiling, no rotation and no
purge**. Two weeks of use left 14 GiB of captured traffic on the machine, every
file carrying an unredacted OAuth bearer token in its headers. This cycle adds
a size ceiling with rotation, a `capture purge` command, and the settings that
drive them.
**Evidence:** 【一手】 first-hand against this machine's own capture directory,
its own admin commands and its own disk

---

## 0. The short answer

| # | what | the answer |
|---|---|---|
| 1 | the defect | the capture grew without a bound — 55,417 files / 14–15 GB in 11 days, while the *log* file had been capped at 4 MiB since cycle 274 |
| 2 | the exposure | **every** capture file carried `Authorization: Bearer ya29.…` — unredacted by design, in a directory that holds days of valid credentials |
| 3 | the fix | a **size ceiling** on the capture directory; the **oldest** files are deleted once the directory is over it |
| 4 | the default | **1 GiB** (`DefaultCaptureMaxBytes`); `0` disables the ceiling and restores the old behaviour |
| 5 | where it comes from | `OPENAGY_CAPTURE_MAX_BYTES` > `capture_max_bytes` in the `"wrapper"` section of `~/.openagy/config.json` > the default (the section moved out of `wrapper.json` in cycle 370) |
| 6 | what is deleted | only the files this proxy wrote — `<epochmillis>-<seq>-<method>.json`; anything else in the directory stays |
| 7 | when the pass runs | on the capture goroutine, never on the response path; amortised at one pass per 64 MiB written, plus one pass on the **first** record after a start |
| 8 | the manual escape | `pigasus proxy capture purge` deletes every captured pair and reports the count and the freed bytes |
| 9 | the one-off cleanup | 53,296 files / **13.25 GiB** deleted; the 1.4.11 batch (2,165 files / 831 MB) kept |

---

## 1. What was found

### 1.1 The measurement

【一手】 — `~/.openagy/captures`, measured on 2026-10-31:

| | |
|---|---|
| files | **55,417** |
| size | **14–15 GB** |
| span | 2026-10-20 01:06 → 2026-10-31 01:21 |
| free disk | 33 GB (94 % used) |

Per day:

| day | files | GiB |
|---|---|---|
| 2026-10-20 | 4,320 | 0.10 |
| 2026-10-21 | 12,114 | 2.53 |
| 2026-10-22 | 17,922 | 4.90 |
| 2026-10-23 | 9,460 | 2.87 |
| 2026-10-24 | 9,450 | 2.85 |
| 2026-10-30 | 1,768 | 0.64 |
| 2026-10-31 | 340 | 0.13 |

92 % of the volume is `streamGenerateContent` (12.92 of 14 GiB): each request
re-sends the same ~67 KB system prompt plus the whole conversation history, so
the files are **individually distinct and collectively repetitive** — 13,934
requests, 13,934 distinct hashes.

### 1.2 The exposure

【一手】 — a scan of the first 4 KB of every file found `Bearer ya29` in
**55,391 of 55,391** files. The capture is deliberately unredacted (it is the
RE instrument), and the directory is `0700` / `0600` — but a week-old
credential copy sitting on disk is still a week-old credential copy, and the
newest ones were live.

### 1.3 There was no ceiling

The proxy's **log** has been bounded since cycle 274:

```go
// internal/agyproxy/log.go
const logMaxBytes = 4 << 20
```

The **capture** had no equivalent. Nothing in the tree read the directory at
runtime either: `capture on|off|dir` were the only switches, and the doctor's
capture line was a path and a file count. Two weeks of use was enough to fill
the disk.

### 1.4 What the captures were still worth

Almost nothing, at the bulk level. The findings were extracted long ago and
committed: the prompt ground truth (`internal/agyprompt/testdata/capture_1.4.{4,6}.{json,txt}`),
the tool declarations (`internal/tools/catalog.go`), the quota wire
(`internal/auth/quota.go`), the SSE frames (`geminicode/`), the account-pool
shapes (`internal/agyproxy/aggregate.go`). The tree referenced **six** capture
file names by name, and five of them were already gone.

The **newest** batch was worth keeping: it closed cycle 365's open item
(`SUSPECT internal/agyprompt/testdata/capture_1.4.6.txt — captured at agy
1.4.8, unverified against 1.4.11`). The comparison, against the live 1.4.11
request:

| section | 1.4.6 ground truth vs 1.4.11 live |
|---|---|
| `identity`, `artifacts`, `communication_style`, `conversation_transcript`, `guidelines`, `messaging`, `slash_commands`, `user_rules` | **byte-identical** |
| `subagents` | one clause differs (the `research` agent's wording) |
| `terminal_sandbox` | **absent** from the live 1.4.11 request, and no `BypassSandbox` anywhere in it |
| `RULE`, `skills`, `user_information` | the differences are the *user's own* files (AGENTS.md, installed skills, local paths) |

---

## 2. The one-off cleanup

【一手】 — the cutoff was the first capture whose `User-Agent` names
`antigravity/cli/1.4.11` (epoch `1793357697321` = 2026-10-30 18:54:57):

| | files | size |
|---|---|---|
| deleted (before the cutoff) | 53,296 | **13.25 GiB** |
| kept (1.4.11 and later) | 2,165 | 831 MB |

Free disk went from **33 GB (94 % used)** to **45 GB (91 % used)**.

---

## 3. The implementation

### 3.1 The ceiling

`internal/agyproxy/capture.go`:

```go
const DefaultCaptureMaxBytes int64 = 1 << 30
const capturePruneInterval int64 = 64 << 20
```

The capture carries the ceiling and a byte counter:

```go
type capture struct {
	dir      string
	log      func(string, ...any)
	ch       chan captureRecord
	wg       sync.WaitGroup
	seq      atomic.Int64
	maxBytes int64 // the ceiling; 0 means no ceiling
	since    int64 // bytes written since the last prune pass
}
```

`noteBytes` runs after every write and prunes when enough has accumulated:

```go
func (c *capture) noteBytes(n int64) {
	if c.maxBytes <= 0 { return }
	c.since += n
	if c.since < capturePruneInterval { return }
	c.since = 0
	c.prune()
}
```

`prune` deletes the **oldest** files until the directory is under the ceiling:

```go
func (c *capture) prune() {
	files, total := scanCaptures(c.dir)
	if total <= c.maxBytes { return }
	sort.Slice(files, func(i, j int) bool { return files[i].modTime.Before(files[j].modTime) })
	for _, f := range files {
		if total <= c.maxBytes { break }
		if err := os.Remove(f.path); err != nil { continue }
		total -= f.size
	}
}
```

**The first record written after a start prunes.** `newCapture` seeds the
counter at the interval, so a directory that is *already* over its ceiling is
trimmed as soon as the proxy sees traffic — not after the next 64 MiB.

### 3.2 The name rule

The rotation and the purge share one rule, so both can only ever remove the
files this proxy wrote:

```go
// isCaptureName reports whether a file name is one of ours.
//
// The shape is "<epochmillis>-<seq>-<method>.json". The method may itself
// contain dashes, so only the first two fields are split off.
func isCaptureName(name string) bool { … }
```

The legacy `-v1internal-cascadeNuxes.json` names match it too — the method
field carries the dashes.

### 3.3 The settings

`internal/agyproxy/fileconfig.go`:

| layer | name | meaning |
|---|---|---|
| environment | `OPENAGY_CAPTURE_MAX_BYTES` | the ceiling, in bytes or with a suffix |
| file | `capture_max_bytes` in the `"wrapper"` section of `~/.openagy/config.json` | the same, persisted |
| default | `DefaultCaptureMaxBytes` | 1 GiB |

The file member is a **pointer**, like the file's booleans: absent means the
default, an explicit `0` means *no ceiling*. `resolveCaptureMax` applies the
order environment > file > default; an unparseable environment value is
reported on stderr and ignored, so a typo cannot stop the proxy.

`parseBytes` accepts a plain byte count, the decimal suffixes (`KB`, `MB`,
`GB`, `TB` — powers of 1000), the binary suffixes (`KiB`, `MiB`, `GiB`, `TiB`)
and the bare letters (`K`, `M`, `G`, `T` — binary, the way `ls -h` means
them).

### 3.4 The commands

```sh
pigasus proxy capture            # status: on/off, dir, max, config
pigasus proxy capture max        # show the ceiling
pigasus proxy capture max 500MB  # set it (0 = unlimited)
pigasus proxy capture purge      # delete every captured pair
```

`purge` uses the same name rule as the rotation, so a capture directory that
holds anything else keeps it. It reports the count and the freed bytes.

The doctor grew the ceiling onto its capture line:

```
capture     /Users/gear/.openagy/captures (on)
            2165 files, 831.0 MiB (max 1.0 GiB)
```

---

## 4. The tests

`internal/agyproxy/capture_test.go` (new):

| test | what it pins |
|---|---|
| `TestCapturePruneRemovesOldestFirst` | the ceiling is honoured and the **oldest** file goes |
| `TestCapturePruneUnderCeilingKeepsEverything` | a directory below the ceiling loses nothing |
| `TestCapturePruneLeavesForeignFiles` | a file that is not a capture survives |
| `TestCaptureNoCeilingWritesEverything` | a ceiling of 0 keeps the unbounded behaviour |
| `TestCaptureRotationEndToEnd` | the proxy's **own HTTP surface**: a request through the server prunes the oldest file and leaves the directory under the ceiling |
| `TestPurgeCaptures` | the purge removes the capture files, reports the count and the bytes, and leaves the rest |
| `TestIsCaptureName` | the name rule, case by case |
| `TestParseBytes` | the size syntax, and the values it must reject |
| `TestResolveCaptureMaxPrecedence` | environment > file > default; `0` is unlimited from either source |
| `TestCaptureMaxRoundTrip` | `capture max` persists, the environment overrides it, `capture purge` empties the directory |

`go test ./cmd/... ./internal/...` is green at this commit.

---

## 5. The files

| File | What changed |
|---|---|
| `internal/agyproxy/capture.go` | **the rotation**: `DefaultCaptureMaxBytes`, `capturePruneInterval`, `capture.maxBytes`/`since`, `noteBytes`, `prune`, `captureFile`, `scanCaptures`, `isCaptureName`, `isDigits`, `purgeCaptures` |
| `internal/agyproxy/fileconfig.go` | `EnvCaptureMax`, `fileConfig.CaptureMaxBytes`, `settings.CaptureMaxBytes`, `resolveCaptureMax`, `parseBytes`, `byteSuffixes`, `byteSuffixOrder` |
| `internal/agyproxy/server.go` | `Config.CaptureMaxBytes` (a pointer: nil = the default, 0 = no ceiling) and the `newCapture` call |
| `internal/agyproxy/launch.go` | the resolved ceiling reaches the server |
| `internal/agyproxy/admin.go` | `capture max`, `capture purge`, the status line, the doctor's ceiling |
| `internal/agyproxy/ctl.go` | the usage lines and the new environment variable |
| `internal/agyproxy/capture_test.go` | **new** — the ten tests above |
| `internal/agyproxy/ctl_test.go` | the new environment variable is cleared in the precedence test |
| `docs/guide/proxy.md` | the ceiling, the rotation, the purge |
| `internal/agyproxy/README.md` | the capture section and the environment table |

---

## 硬约束 / Hard constraints

- **The rotation only removes the files this proxy wrote.** The name rule
  (`<epochmillis>-<seq>-<method>.json`) is shared by the rotation and the
  purge; a capture directory pointed at something else keeps its other files,
  and those files do not count towards the ceiling either.
- **The oldest goes, the newest stays.** The order is the file's mtime, and
  the prune stops as soon as the directory is under the ceiling.
- **Pruning never happens at construction.** `newCapture` only starts the
  goroutine; the first *write* is what prunes. `pigasus proxy doctor` builds a
  `Server`, so it stays read-only with respect to the user's files.
- **`0` means no ceiling, at every layer** — the environment, the file and the
  `Config` pointer. The default (1 GiB) applies only when the setting is
  **absent**.
- **The prune pass is amortised.** It runs once per 64 MiB written, not once
  per record, because a pass is a directory scan. The directory may therefore
  overshoot its ceiling by up to ~64 MiB plus the record in flight.
- **The first record written after a start prunes.** An oversized directory is
  trimmed as soon as the proxy sees traffic.
- **The pass runs on the capture goroutine**, never on the response path, and
  a failure to delete a file is skipped, not fatal.
- **The names do not move.** `OPENAGY_CAPTURE_MAX_BYTES` is new; the existing
  `OPENAGY_CAPTURE_DIR` and the `capture_dir` member keep their names, and
  the proxy's state gains one member (`capture_max_bytes`) without losing any.
  (Cycle 370 moved that state out of `wrapper.json` into the `"wrapper"`
  section of `config.json`; the member names did not change.)
- **The size suffixes have two families.** `KB`/`MB`/`GB`/`TB` are powers of
  1000; `KiB`/`MiB`/`GiB`/`TiB` and the bare `K`/`M`/`G`/`T` are powers of
  1024. The doctor and the status line print both the human form and the
  exact byte count, because `250MB` reads back as `238.4 MiB`.

## Open questions / 存疑

- **The ceiling is a guess, not a measurement.** 1 GiB is the number that
  keeps roughly the last day of this machine's traffic; a different workload
  (a long `streamGenerateContent` with a large tool result) has a different
  natural size. The setting exists so the number does not have to be right.
- **The mtime is the only ordering.** Two files written inside the same
  filesystem timestamp tick are ordered arbitrarily, so a prune that lands
  exactly on a tie can keep either. The window is milliseconds and the cost
  is one file out of thousands.
- **A ceiling below the size of a single record leaves the directory empty.**
  The prune deletes until the total is under the ceiling, and the newest
  record is a candidate like any other. 【推测】 — a "never delete the newest"
  rule would be friendlier, but it would also make the ceiling a lie.
- **Whether the rotation should count *all* files in the directory, rather
  than only the capture files, is open.** Counting everything would make the
  ceiling a property of the directory; counting only ours keeps the promise
  that the proxy never touches a file it did not write. The second won.

## Limitations

- **The rotation is the proxy's, not the clone's.** It only applies to the
  vendor `agy` behind `pigasus agy`; the clone's own transports do not
  capture.
- **A capture is still unredacted.** The ceiling bounds how much credential
  material is on disk; it does not redact what is captured. Turning the
  capture off (`pigasus proxy capture off`) is still the answer when the
  machine is not the machine.
- **The purge is not reversible.** It deletes the files; it does not archive
  them.
- **The 1.4.11 batch was kept by hand**, not by the rotation — the rotation
  keeps what is *newest*, and the batch that was kept was chosen by the
  version in the `User-Agent`. A later start would have kept the same files
  only by accident of timing.
- **The `terminal_sandbox` question is still open.** The live 1.4.11 request
  does not carry the section, and the capture cannot tell "1.4.11 dropped it"
  from "this run was not sandboxed". A sandboxed turn would settle it; that
  was not run in this cycle.

---

Report language note: this file is English because `docs/cycles/*` is English.
The evidence files under `out/` are Chinese, per the repository's convention.
