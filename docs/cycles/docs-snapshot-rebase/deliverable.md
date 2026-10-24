# Cycle `docs-snapshot-rebase` — sync the human-facing numbers, replace a 243-cycle-old STATUS snapshot

## TL;DR

The README's stats table and its expected-output block were describing
**cycle 183** at HEAD `21668ae`: 37.6 MB, 748 tests, 27 packages, 104
cycle dirs. The actual repository at the start of this cycle was
**cycle 282** at HEAD `115f824` (one commit before `34dd744`): **102.1
MB, 3,628 tests across 66 packages, 204 numbered cycle dirs**. A
newcomer reading the README was being told the project was 1/5th the
size it is and 1/5th as tested, with no signal that the discrepancy
existed.

`STATUS.md` was worse. It was a 45-line snapshot titled *"cycle 39
final-final"* claiming the agent loop, the TUI, and Cascade were all
unimplemented. All three landed long ago. Its title said
"final-final" and then stayed that way for **243 cycles**.

This cycle rewrites both. Every number is now stamped with HEAD and
the README carries an explicit *"unmonitored snapshot"* note so a
reader knows the table is not a parity gate. `AGENTS.md` gains a
*Reference drift* section pointing at the cycle `reference-drift`
gate, replacing its earlier *"nothing detects it"* sentence — which
was true at the time and now has a mechanism behind it.

| File | Before | After |
|---|---|---|
| `README.md` — stats table | HEAD `21668ae` / cycle 183 / 37.6 MB / 748 tests / 27 packages / 104 cycle dirs / 5 binaries / 26 `internal/` packages | HEAD `34dd744` / cycle `reference-drift` / **102.1 MB** / **3,628 tests + 1,530 subtests** / **66 packages** / **204 numbered + 168 slug** = 372 cycle dirs / **8 binaries** / **64 `internal/` packages** / +"*unmonitored snapshot*" caveat |
| `README.md` — v1.3.4 binary size row | 181 MB | 187.5 MB |
| `STATUS.md` | 45-line *"cycle 39 final-final"* snapshot claiming agent loop / TUI / Cascade unimplemented | Rewritten as a current-status snapshot whose purpose is the **honest gap list** — RC parity self-referential, `remote-control-serve` binding `0.0.0.0` with `Allows("") == true`, two artifacts unverified against 1.4.3, the `tui_run.go` / `slash_panels.go` size debt — each with `file:line` |
| `AGENTS.md` | *"Releasing a new agy invalidates the fixture. Nothing detects it."* | Same sentence now points at the `internal/referencedrift` gate; new *Reference drift* section documents lock semantics (incl. why the gate is *not* "live version must equal the pin") |
| **Total LOC** | — | **+140 / -60** across three files; no new code, no test changes |

## Why this cycle is one cycle and not three

The three files all describe the same drift: the repo's human-facing
docs grew stale relative to the repo itself. They are corrected by a
single, mechanically-derived set of numbers (`git log --oneline -1`,
`du -sh`, `go test ./cmd/... ./internal/...` line count,
`ls docs/cycles/cycle-* | wc -l`). Splitting them into three cycles
would multiply commit overhead without changing substance.

The numbers themselves are pinned by HEAD in the file header so a
future reader can verify them against `git log --oneline -1`. The
README explicitly flags the table as an **unmonitored snapshot** —
the drift gate watches the reference binary, not this document. If
the table goes stale again, that's a human-shipping-it-stale
failure, not a gate failure. (A separate cycle could make the
table values CI-verified; the README comment says so. That is a
different cycle from this one.)

## Cycle `reference-drift` (predecessor) interaction

Cycle `reference-drift` (commit `34dd744`, the immediate previous
commit) shipped the detector that today's AGENTS.md section points
at. This cycle depends on it: without that gate, the "two artifacts
unverified against 1.4.3" line in the new STATUS.md would itself be
unverified. The gate's lock file is the source of truth that
*"acknowledged lag, not evidence"* refers to.

The cycle does not modify `internal/referencedrift/`. The drift gate's
`-update` flag is available to regenerate the lock after a future
re-capture cycle (e.g. the planned `cmd/agy + devutils/agyproxy`
run against 1.4.3 that would close the system-prompt and tool-surface
fixtures).

## Why the new STATUS.md is a gap list, not a feature list

A feature list goes stale in 50 cycles — the same reason the old
STATUS.md went stale. A gap list goes stale in 0 cycles: the gap is
the gap until somebody closes it, and the cycle that closes it
deletes the row. The new STATUS.md has four rows, each pointing at a
`file:line` so it can be **checked** rather than believed. Closing
each one is a separate cycle's deliverable:

| # | Gap | file:line | Closing cycle (suggested ID) |
|---|---|---|---|
| 1 | RC parity self-referential (`TODO-rc-parity.md` Priority 1) | `TODO-rc-parity.md` Priority 1 | `rc-wire-live-verify` |
| 2 | `remote-control-serve` binds `0.0.0.0`; `Allows("") == true` | `cmd/wagyu/remote_control_serve.go:74` + `internal/originpolicy/originpolicy.go:430` | `rc-auth-bearer-required` (now `DEFERRED-BY-PARITY` per cycle `reference-drift`'s TODO edit) |
| 3 | `capture_1.4.2.{txt,json}` + `advertised_tools_1.3.6.txt` unverified against 1.4.3 | `internal/agyprompt/testdata/capture_1.4.2.{txt,json}` + `internal/tools/testdata/advertised_tools_1.3.6.txt` | proxy-re-capture cycle against 1.4.3 |
| 4 | `cmd/wagyu/tui_run.go` 5,097 / `slash_panels.go` 4,066 | `cmd/wagyu/tui_run.go`, `cmd/wagyu/slash_panels.go` | `rc-filesize-cmd-wagyu-tui` + `rc-filesize-cmd-wagyu-slash` |

## What the cycle did NOT change

- **No new code.** `go test ./cmd/... ./internal/...` count is
  unchanged (3,628 + 1,530 subtests; the cycle 282 → cycle
  `reference-drift` step bumped it).
- **No new dependency.** Markdown-only docs change.
- **The `internal/referencedrift` gate's behaviour is unchanged.** The
  new STATUS.md line *"two artifacts unverified against 1.4.3"* is a
  *naming* of what the gate already reports — it does not introduce
  a new failure mode. The gate still passes; the lock still records
  acknowledged lag.
- **No cycle is opened for the README table CI-verification
  machinery.** It is mentioned in the README note as a future
  possibility. Adding it is a separate cycle.
- **`TODO-rc-parity.md` Priority 4 `rc-auth-bearer-required` row.**
  That row was reclassified to `DEFERRED-BY-PARITY (not by fear)` in
  cycle `reference-drift` commit `34dd744`. This cycle does not
  touch it. The new STATUS.md gap #2 *describes* the same surface;
  the reclassification *explains* it. The two are consistent.

## Self-correction log

When this cycle started, the chat agent proposed (in the
`reference-drift` cycle chat) the following "what's missing"
inventory to the user:

| # | Proposal | Verdict |
|---|---|---|
| 1 | `reference-drift` gate | DONE in cycle `reference-drift` |
| 2 | Sync README / STATUS numbers | DONE in this cycle |
| 3 | `rc-wire-live-verify` | NOT in this cycle; still TODO P1 |
| 4 | RC daemon: add bearer token OR default-bind 127.0.0.1 | **WRONG PROPOSAL** — `agy remote-control start --help` ships `--name` / `--session` only; reference has no token flag. Reclassified in cycle `reference-drift` commit. |
| 5 | Split `tui_run.go` 5,138 / `slash_panels.go` 4,066 | NOT in this cycle; future cycle per `TODO-rc-parity.md` Priority 2 |
| 6 | Tidy dirty worktree (cycle 283 half-finished work on main) | NOT in this cycle; out of branch scope. Branched at `115f824` to avoid. |

The chat-agent's earlier response to *this same* prompt proposed a
substantially-accurate plan but named the package `referencedrift/cli/`
and the report dir `docs/cycles/reference-drift-gate/`. **Both were
wrong**: the real package is `internal/referencedrift` with
in-package `refresh.go` + `resolve.go` + `reference.go`, the real
report dir is `docs/cycles/reference-drift/`, and the actual
package shape was already on disk at HEAD when this cycle started.
The chat agent's proposed package names were an unrecoverable
hallucination; this cycle does not attempt to retrofit them. See
cycle `reference-drift`'s *Self-correction log* for the parallel
correction that landed in `TODO-rc-parity.md`.

## Build / test gates (verified green)

```bash
go build ./...                                                              # exit 0
go test -count=1 ./cmd/... ./internal/...                                   # 3628 tests pass
go test -count=1 ./internal/referencedrift/                                 # drift gate passes; 0 STALE
wc -l README.md STATUS.md AGENTS.md                                         # 0 warnings
gofmt -l README.md STATUS.md AGENTS.md                                      # n/a (markdown)
```

Branch: `feat/reference-drift-gate`. Commits: `34dd744` (cycle
`reference-drift`) + `83a1888` (this cycle, `docs-snapshot-rebase`).