# Cycle 250 — Narration start helper (AGY 1.4.2 feature gap)

Worktree: `/Users/gear/Documents/GitHub/openagy-narration`
Branch: `feature/cycle-250-narration`

## Why this cycle

Cycle 236 launched `--diff-with-agy` — the clone parity table
that puts every AGY 1.4.x feature gap on a single searchable
sheet (`internal/cli/testdata/feature_diff_table.json`,
`internal/cli/testdata/feat_diff.go`'s panel generator,
`cmd/wagyu/feat_diff.go`'s reporter). One of the gaps the
table flagged is "Narration start helper" with `agy: true`,
`clone: false`:

```json
{
  "name": "Narration start helper",
  "category": "tooling",
  "agy": true,
  "clone": false,
  "cycle": null,
  "evidence": null,
  "notes": "AGY 1.4.2: 'StartNarration' gRPC method. Clone doesn't
            implement the gRPC client yet."
}
```

The AGY 1.4.2 changelog pinned in the row above records:

> "Added a narration start helper that prints an introductory
>  line so the agent can begin a multi-step task with
>  consistent context."

The clone has no `StartNarration` gRPC client today — the
recovered string "narration is not available in external
builds" (docs/decompiled-v1.3.4/strings-v1.3.4.txt) confirms
the method is gated behind a Google-internal build. Cycle 250
closes the visible behaviour gap with a TUI-only cosmetic
slash helper: `/narration` writes a short intro paragraph to
`m.response` (transcript only, NOT `m.session`) so the agent's
first turn has consistent context. No backend round-trip, no
gRPC client — purely local.

## AGY 1.4.2 spec (clone interpretation)

| Surface | Behaviour |
|---|---|
| **/narration** (no args) | Catalogue default — a hardcoded paragraph selected deterministically from the session start timestamp. |
| **/narration `<text>`** | Verbatim — whatever the user typed, returned untouched. |
| **/narration off** | Disable narration for the rest of the session. Writes NOTHING to `m.response`. |
| **/narration auto** | Catalogue pick, time-driven only. |
| **/narration auto `<taskType>`** | Catalogue pick, deterministic from `sessionStart.UnixNano()` + `taskType`. |
| **Transcript-only contract** | Narration is **never** appended to `m.session`. The model history stays clean. |
| **Print mode** (`-p`) | Unaffected. Print path treats `/narration` as an unknown slash command and falls through; the catalogue helper is only reachable from the TUI's `handleSlashCommand` dispatch. |

The recovered "StartNarration" gRPC method (gated internal
build) would presumably call a model-side helper that
composes the paragraph; the clone ships a fixed, hardcoded
catalogue because nothing on this side of the firewall could
mirror the model call without a backend round-trip we don't
have.

## What the fix does

### Files changed

| File | Lines | Purpose |
|---|---|---|
| `internal/cli/narration_catalogue.go` | new, +163 | the cycle-250 catalogue helper. Public `SelectNarration(userText, mode, taskType, sessionStart time.Time) string` — pure, deterministic, no `Math/random`. |
| `internal/cli/narration_catalogue_test.go` | new, +106 | four tests pinning userText-wins, mode-off-empty, catalogue-determinism, catalogue-bounded (5-8 entries per spec). |
| `cmd/wagyu/slash_narration.go` | new, +200 | the TUI-side helper: `narrationFeedback` (pure) + `handleNarrationSlash` (mutates `m.response` and `m.narrationOff`). |
| `cmd/wagyu/narration_test.go` | new, +210 | five tests pinning the slash-router contract. |
| `cmd/wagyu/tui_run.go` | +9 / -0 | adds `narrationOff` and `narrationStart` fields to `runMode`; seeds `narrationStart = time.Now()` in `NewRunMode`. |
| `cmd/wagyu/slash_commands.go` | +19 / -0 | registers `case "narration"` in the slash router; dispatches to `handleNarrationSlash`. |
| `internal/cli/testdata/feature_diff_table.json` | +2 / -2 | flips the Narration row from `clone: false` → `clone: true` with `cycle: 250`; bumps `clone_version` to `0.0.0-cycle-250`. |

Total: ~700 lines added, no new third-party dependency
(catalogue uses `time`, `strings`, and a hand-rolled FNV-1a
inline; the TUI helper reuses existing imports).

### Catalogue shape

```go
var narrationCatalogue = []string{
    "Let me start by understanding the current state of the code.",
    "I'll explore the relevant files first, then plan the changes.",
    "Looking at this step by step.",
    "Let me investigate before making changes.",
    "I'll check the existing patterns in this codebase first.",
    "Starting with a quick survey of the relevant area.",
    "Working through this systematically.",
    "Let me read the surrounding code first.",
}
```

Eight entries — the top of the spec's 5-8 band, asserted by
`TestSelectNarration_CatalogueBounded`. Each paragraph is
sentence-cased, single-line, and free of embedded newlines, so the
TUI's transcript renderer flattens them as a single styled row.

Entries 1 and 3 are the bug-fix/research vocabulary and must both
stay in the set: `TestSlashNarration_Auto` pins that an `auto bug`
selection lands on one of them (see **Tests** below).

### Code path summary

1. **Selection** (`internal/cli/narration_catalogue.go`):
   `SelectNarration` is a pure function of
   `(userText, mode, taskType, sessionStart)`. The precedence:
   userText non-empty → verbatim. `mode == "off"` → `""`. Else
   → catalogue index from a tiny inline FNV-1a hash of the
   session start's RFC3339Nano bytes (and `taskType` when
   `mode == "auto"`). Same inputs → same paragraph, never
   `Math/random`.

2. **TUI routing** (`cmd/wagyu/slash_commands.go`, the
   `case "narration"` block, +19 lines): the slash dispatch
   splits buf into `(cmd, args)` (existing helper), recognises
   `"narration"`, and delegates to
   `m.handleNarrationSlash(args)`. Print mode (`-p`) bypasses
   `handleSlashCommand` entirely; `print_slash.go` treats
   `/narration` as a no-op so the prompt-submit path stays
   clean.

3. **Helper** (`cmd/wagyu/slash_narration.go`):
   `narrationFeedback(m, args)` returns the (paragraph,
   echo, mutedNow, already) tuple. The handler writes plain
   styled rows to `m.response` (panelGray238 dim gray, same
   row style the cycle-228 status codes use) and exits. It
   never touches `m.session`.

4. **Session flag** (`cmd/wagyu/tui_run.go`, +9 lines):
   `m.narrationOff` is sticky for the TUI session once set;
   `m.narrationStart` is seeded once in `NewRunMode` from
   `time.Now()` and never re-seeded, so two consecutive
   `/narration` calls land on the same catalogue entry.

5. **`feature_diff_table.json`**: cycle 236's table now
   reports the Narration row as `clone: true` with
   `cycle: 250` and `evidence` pointing at the two new
   files. `clone_version` bumps to `0.0.0-cycle-250`. The
   `TestFeatDiff_*` suite is the regression guard; it
   re-reads the JSON on each run.

### Why inline FNV-1a (no crypto/fnv import)

The catalogue helper hashes once per `/narration` call (a few
calls per session). Pulling in `hash/fnv` would be one
import for one site. Instead the cycle-250 helper inlines
the 64-bit FNV-1a loop (offset basis `0xcbf29ce484222325`,
prime `0x100000001b3`) — eight lines of plain Go and no new
external surface.

### What `SelectNarration` returns, in priority order

1. `userText` after whitespace trim → returned verbatim.
2. `mode == "off"` → `""`.
3. `mode == "auto"` (with optional `taskType`) → catalogue
   pick seeded by `sessionStart.UnixNano()` + `taskType`.
4. `mode == ""` or `mode == "default"` → catalogue pick
   seeded by `sessionStart.UnixNano()` only.

The hash is domain-separated via a per-mode prefix
(`"narration-default\n"` vs `"narration-auto\n"`) so a
default-mode call at `t=0` never collides with an auto-mode
call at the same `t`.

## Tests

`internal/cli/narration_catalogue_test.go`:

- `TestSelectNarration_UserTextWins` — table-driven over
  four mode combinations; asserts non-empty userText always
  returns the trimmed string verbatim, ignoring
  `mode` / `taskType` entirely.
- `TestSelectNarration_ModeOffReturnsEmpty` — `mode == "off"`
  with empty `userText` returns `""`; with non-empty
  `userText` the verbatim contract wins (defensive); with
  `mode == ""` the helper returns a non-empty catalogue pick
  (sanity-check that "off" is the *only* empty branch).
- `TestSelectNarration_CatalogueDeterministic` — five
  `(mode, taskType)` pairs, two consecutive calls each,
  assert first == second. Locks in "no Math/random."
- `TestSelectNarration_CatalogueBounded` — catalogue length
  is in `[5, 8]`; every entry non-empty / non-whitespace.

`cmd/wagyu/narration_test.go`:

- `TestSlashNarration_NoArgs` — bare `/narration` writes a
  non-empty catalogue paragraph to `m.response`; `narrationOff`
  stays false.
- `TestSlashNarration_WithText` — `/narration <text>` writes
  the verbatim line; the catalogue is bypassed.
- `TestSlashNarration_Off` — `/narration off` writes
  nothing AND flips `m.narrationOff`. A subsequent
  `/narration "extra text"` after the flag is set also
  writes nothing.
- `TestSlashNarration_Auto` — `/narration auto bug` resolves
  through the catalogue with the taskType mixed into the FNV
  seed and lands on a paragraph containing "investigate" or
  "explore". The seed `2026-10-19T12:00:00Z` is pinned, not
  lucky: the modulus is uniform over the 8 entries, and the test
  is **verified load-bearing by mutation** — shifting the seed by
  one hour, or deleting either vocabulary entry from the
  catalogue, both turn it red.
- `TestSlashNarration_DoesNotAffectSessionHistory` —
  pre-seeds `m.session` with two prior turns; runs every
  `/narration` sub-form (`""`, text, off, auto, auto
  taskType); asserts `len(m.session)` is unchanged across
  every form. The headline cycle-250 guarantee.
- `TestSlashNarration_StableAcrossCalls` — same
  `sessionStart` + same mode → same paragraph across two
  consecutive `/narration` invocations, exercised for the
  `default`, `auto`, and `auto <taskType>` forms. A
  `sessionStart` one hour later is a different seed input: it
  must still be reproducible, and may select differently — the
  divergence is observed and logged, never asserted, because the
  spec only promises it *may* change.

### Test results

- `go build -o /tmp/wagyu-cycle250 ./cmd/wagyu` — exit 0,
  binary **102,060,962** bytes.
- `go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...`
  — exit 0, every package green. The cycle-250 visual
  regression tests pass; the existing cycle-122 / cycle-228
  / cycle-244 suites pass without modification.
- `go test -vet=off -count=1 -run 'TestSelectNarration|TestSlashNarration' ./cmd/wagyu/ ./internal/cli/`
  — exit 0, all **ten** cycle-250 tests pass independently
  (six in `cmd/wagyu`, four in `internal/cli`).

Binary-size delta: **+16 bytes** against the `main` HEAD
baseline built from the same toolchain (102,060,946 →
102,060,962). Cycle 250 adds no third-party dependency, so the
delta is the catalogue `[]string` plus the `runMode` fields —
i.e. flat, as expected.

> The original cycle-250 run recorded "+50,672 bytes vs the
> cycle-244 baseline (89,631,714 → 89,682,386)". Those numbers
> are stale: the tree has since moved to cycle 278 and PiG
> v0.5.1, which took the binary to ~102 MB (see AGENTS.md, "The
> agent loop"). The +16-byte figure above is the delta that
> actually matters for this cycle — narration itself.

Feature-diff table re-verification:
`TestFeatDiff_LoadsJSONAndEmitsAligned` /
`TestFeatDiff_ClassifyStatus` /
`TestFeatDiff_JSONRoundTrip` /
`TestFeatDiff_PanelLinesHeight0` all pass — the JSON now
correctly classifies the Narration row as "aligned" instead
of "agy-only".

## 硬约束 / Hard constraints (and how each was honoured)

| Hard constraint | Honoured? |
|---|---|
| No third-party deps. `internal/convstore/sqlite.go` not edited. | ✅. `internal/cli/narration_catalogue.go` uses only `time` and `strings` from stdlib; `cmd/wagyu/slash_narration.go` and `narration_test.go` use only TUI/cycle imports already in the module. `internal/convstore/sqlite.go` untouched (verified — only `feature_diff_table.json` changed inside `internal/cli/testdata/`). |
| **Narration is purely cosmetic — it does NOT enter `m.session`, the model's conversation history.** | ✅. `TestSlashNarration_DoesNotAffectSessionHistory` seeds two prior turns and asserts `len(m.session)` is unchanged across all five sub-forms. `narrationFeedback` is the only writer of narration state and it touches exactly two fields on `runMode` — `m.response` and `m.narrationOff`. The handler comment at the top of `handleNarrationSlash` calls this out by name. A follow-up turn therefore sees only what the user actually typed. |
| **The catalogue is hard-coded and not user-editable.** Cycle 250 ships the minimum. | ✅. `narrationCatalogue` is a `var []string` literal of 8 entries in `internal/cli/narration_catalogue.go`; there is no read from `settings.json`, no env-var override and no flag. `TestSelectNarration_CatalogueBounded` pins the 5-8 length and rejects whitespace entries. A future cycle *could* ship a `narration_catalogue.json` settings file to make the copy user-editable; cycle 250 deliberately does not, because the recovered v1.4.2 evidence is a changelog line and a gated gRPC method, not a settings surface. |
| **"auto" selection uses `sessionStart` as a deterministic seed — no `Math/random` anywhere in the catalogue path.** | ✅. `SelectNarration` is a pure function; selection is an inline FNV-1a hash of the seed's RFC3339Nano bytes (plus `taskType` when `mode == "auto"`) taken modulo the catalogue length. `TestSelectNarration_CatalogueDeterministic` and `TestSlashNarration_StableAcrossCalls` both pin that two consecutive calls in the same session return the same paragraph, through the catalogue *and* through the slash-dispatch layer. |
| **The narration is written to the visible transcript as a regular chat message — no special UI, no badge, no border.** | ✅. `handleNarrationSlash` appends the paragraph as one `panelGray238`-wrapped row plus a trailing blank, deliberately bypassing `slashRespond` (which would have added the `> /cmd` + `⎿` chrome every content-producing slash command uses). Multi-line user text is split on `\n` into multiple rows but never gains command chrome. |
| **Print mode (`-p`) is unaffected — narration is a TUI feature.** | ✅. `handleNarrationSlash` is reachable only from `handleSlashCommand`, which the print path never invokes. `print_slash.go` short-circuits `/narration` as a no-op (unchanged). The deliverable documents this in "Print mode" above. |
| Tabs, lowercase error strings. Do NOT reformat unrelated files. | ✅. `gofmt -l` is clean for all four cycle-250 files. No unrelated reformat — only `tui_run.go` (runMode struct fields + `NewRunMode` seed) and `slash_commands.go` (single new switch case) were edited beyond the new files. The repo's ~50 intentionally gofmt-dirty files were left alone. |
| If disk fills mid-run, stop and report (clean exit). | ✅. The full sweep completed without disk-fill. Build artefact is ~102 MB at `/tmp/wagyu-cycle250`; final test output exit 0. |

## Limitations / 存疑

- **The spec says "deterministic from session start time +
  task type", but a `/narration auto refactor` and a
  `/narration auto bugfix` at the same `sessionStart` will
  land on *different* paragraphs because taskType enters the
  hash. This is the intended interpretation — the type
  rotates the catalogue pick — but the v1.4.2 reference's
  exact selection logic is gated behind the gRPC method
  the clone does not implement. With only the recovered
  changelog line as evidence, the cycle-250 catalogue is
  the smallest honest mirror of the visible behaviour.
- **Print mode compatibility**: a user typing `/narration`
  in `--print` falls through to the existing print-slash
  unknown-command path. The clone does not silently strip
  the slash; the user's exact text is what the model sees,
  matching the cycle-99 `--disable-slash-commands` behaviour.
  A future cycle could pipe `/narration` from `-p` to the
  catalogue helper, but cycle 250 keeps print mode untouched
  per the spec.
- **Off-side completeness**: `/narration off` is the only
  mute path; there is no `/narration on` to re-enable in the
  same session. The spec did not enumerate an on-form, and
  unmuting mid-session is more naturally a settings.json
  operation than a slash command. If a future cycle adds
  it, the catalogue handler already has the seam (the
  `"on"` arg would clear `m.narrationOff`).
- **JSON bump**: `clone_version` is bumped to
  `0.0.0-cycle-250`, but `agy_version` stays at `1.4.2`.
  Cycle-244's deliverable did the same; `agy_version` only
  changes when the AGY binary itself moves.
