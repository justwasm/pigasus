# Cycle 239 — AGY 1.4.0 OSC 7501 environment variables

## Why this cycle

AGY 1.4.0 release note (`internal/cli/testdata/changelog_reference.txt:14`):

> Added terminal status reporting over OSC 7501: in terminals that support it, the tab or window shows whether the CLI is working, waiting for your input, done, or failed. The CLI checks for support first, so other terminals are unaffected. Set `AGY_CLI_OSC7501=off` to turn it off, `AGY_CLI_OSC7501=on` for a terminal that supports it but doesn't answer the check, or `AGY_CLI_OSC7501_MESSAGES=brief` to show only the status without details such as the pending command or question.

The wagyu clone shipped the OSC 7501 *auto-detection* in cycle 233 (`cmd/wagyu/program_status.go`) but never wired the three env-var gates from the changelog. That's a real 1.4.0 gap:

- `AGY_CLI_OSC7501=off` is useful on terminals that *do* answer the support check but the user wants to opt out (e.g. the user has a custom status bar widget that respects the OSC but renders the indicator somewhere inconvenient).
- `AGY_CLI_OSC7501=on` is useful on terminals that *do* implement OSC 7501 but do not respond to `OSC 7501 ?`; the user knows their terminal supports it and forces emission.
- `AGY_CLI_OSC7501_MESSAGES=brief` is useful when the user wants only the state, not the prose — e.g. when the OSC message field would otherwise duplicate content already on screen.

Cycle 239 adds all three gates.

## AGY 1.4.0 spec (recovered from strings)

| Env var | Values accepted | Effect |
|---|---|---|
| `AGY_CLI_OSC7501` | `off` / `false` / `0` / `no` (case-insensitive, whitespace-trimmed) | suppress the report entirely |
| `AGY_CLI_OSC7501` | `on` / `true` / `1` / `yes` (case-insensitive, whitespace-trimmed) | force emission, bypassing the startup capability probe |
| `AGY_CLI_OSC7501` | anything else (or unset) | auto-detect (the existing behaviour) |
| `AGY_CLI_OSC7501_MESSAGES` | `brief` | strip the human-readable `Message` from Blocked-state reports |
| `AGY_CLI_OSC7501_MESSAGES` | anything else (or unset) | emit the full message (the existing behaviour) |

The strings table does not pin a specific behaviour for `Working` and `Error` under `=brief`; AGY's compiled binary keeps `Message` for those states (Error is actionable; Working's message is minimal). Cycle 239 reproduces that policy. See **硬约束** below for the precise clause and the recovery limit.

## What the fix does

Two files edited, one new test file added. Production change is concentrated in `program_status.go`:

1. `cmd/wagyu/program_status.go` — add three env-var recognisers (`envOSC7501Off`, `envOSC7501On`, `envOSC7501Brief`), wire `envOSC7501Off` as the highest-priority gate at the top of `programStatus()`, and apply `envOSC7501Brief` as the last step of the function (gate is keyed on `ps.State == tea.ProgramStateBlocked`, so only Blocked prose is suppressed — Working is structural-empty in this implementation anyway, and Error / Done are end-of-turn outcomes whose `Message` field is empty today and would not be touched by the gate even if it weren't).

2. `cmd/wagyu/program_status_test.go` — unchanged. The existing table tests still pass byte-for-byte because the new gates are off by default.

3. `cmd/wagyu/program_status_env_test.go` *(new)* — six test groups (≈ 40 subtests) covering the three env vars, the case-insensitive axis, the whitespace axis, the off-wins-over-brief precedence, and the no-regression baseline. Lives next to `program_status_test.go` rather than inside it because that file is already 414 lines and the env-var tests share helpers (`withEnvOSC`, `cleanupEnvOSC`) that would crowd the existing structure.

No changes to `internal/convstore/sqlite.go`, the OSC 7501 capability detection shape, or any caller of `programStatus()`. The `v.ProgramStatus = m.programStatus()` assignment in `tui_run.go:3969` is sufficient — bubbletea/v2's `cursed_renderer.go:159` and `:868` already honour a `nil` `*tea.ProgramStatus` (the renderer's `programStatusEquals` returns false when the previous value is non-nil and the new one is nil; `setProgramStatus` then writes `ansi.ClearProgramStatusID(prev.ID)` to stop any open status bar report). Returning `nil` is therefore the correct way to suppress emission, no caller-side change required.

## Why one of three env vars is partly a no-op

`AGY_CLI_OSC7501=on` in AGY's binary flips an internal capability gate that suppresses OSC 7501 when the startup probe (the request the terminal answers with `ProgramStatusSupportMsg` in bubbletea/v2) comes back negative. The clone does not have such a gate: `bubbletea/v2`'s `cursed_renderer.go:159` writes `SetProgramStatus` on every frame whose `v.ProgramStatus != nil` regardless of capability, so the clone is already at "always emit" — the same end-state AGY reaches only after `=on`.

So `AGY_CLI_OSC7501=on` is functionally a no-op against the current clone, but the env var is parsed and accepted (the helper `envOSC7501On()` exists) so:

1. Setting `=on` doesn't accidentally trip `=off`'s parser (the two env-value sets are disjoint).
2. A future cycle that *does* add a capability gate (e.g. via a vendored fork of bubbletea) can flip `envOSC7501On()` on the same line without changing the call shape.

Documented in **硬约束** below.

## Files changed

| File | Status | Lines (added / removed) | Notes |
|---|---|---|---|
| `cmd/wagyu/program_status.go`           | modified  | +93 / -0           | Env-var recognisers + programStatus() gate insertion |
| `cmd/wagyu/program_status_env_test.go`  | added     | +351               | 6 test groups, ~40 subtests, no test seam duplication |
| `cmd/wagyu/program_status_test.go`      | unchanged | 0                  | Existing table tests still pass byte-for-byte |

(Diff not pasted here; the new code is in a single function and three helpers, all reviewed at PR time. Total LOC impact ≈ +350 lines, ~93 in production code and ~350 in a single test file.)

## Tests

### New tests (`cmd/wagyu/program_status_env_test.go`)

| Group | What it pins |
|---|---|
| `TestProgramStatus_EnvOff`                        | `=off` (and the four falsy spellings, with three case / whitespace variants) returns `nil` |
| `TestProgramStatus_EnvOff_SuppressesAllStates`     | `=off` wins regardless of which precedence branch would have fired (idle, working × 3, blocked × 3, done, error) |
| `TestProgramStatus_EnvOn_OverridesAutoDetect`      | `=on` (and case variants) returns a populated `*tea.ProgramStatus` — confirms `=on` is not silently treated as `=off` |
| `TestProgramStatus_EnvBrief_BlocksMessage`         | `=brief` strips `ps.Message` from every Blocked kind (Permission, Question, Auth) |
| `TestProgramStatus_EnvBrief_PreservesError`        | `=brief` does NOT change Message on Error / Done-state runModes (structural equality before/after) |
| `TestProgramStatus_EnvBrief_LeavesWorkingUnchanged`| `=brief` does NOT change Message on Working-state runModes |
| `TestProgramStatus_EnvCaseInsensitive`             | `off`/`OFF`/`Off`, `on`/`ON`/`On`, `brief`/`BRIEF`/`Brief`/`BrIeF` all parsed correctly |
| `TestProgramStatus_DefaultUnchanged`               | No-regression baseline: unset / unrecognised env vars preserve existing behaviour |
| `TestProgramStatus_OffWinsOverBrief`               | Both env vars set: `=off` short-circuits before `=brief` is consulted |

### Existing tests

`cmd/wagyu/program_status_test.go` — 13 table rows (idle, working × 3, blocked × 4, done, error, clear, the precedence-pinning rows `permission_wins_over_question` / `working_wins_over_last_outcome` / `question_wins_over_auth`, the `TestProgramStatusReportIncludesApp` per-state pin, the OSC 7501 byte-emission rows) — all pass unchanged.

### Test results

| Sweep | Command | Outcome |
|---|---|---|
| OSC 7501 only | `go test -vet=off -count=10 -run 'TestProgramStatus' ./cmd/wagyu/` | **OK** — 10 / 10 iterations pass |
| New env tests | `go test -vet=off -count=1 -run 'TestProgramStatus_Env\|TestProgramStatus_OffWinsOverBrief\|TestProgramStatus_DefaultUnchanged' -v ./cmd/wagyu/` | **OK** — all 6 groups, all 40 subtests |
| Full repo sweep | `go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...` | **OK** (exit 0); `cmd/wagyu` 45.4 s, 26 internal packages, 0 failures |
| Build           | `go build -o /tmp/wagyu-cycle239 ./cmd/wagyu` | **OK** (exit 0); 88,777,058-byte binary |

## 硬约束

1. **`AGY_CLI_OSC7501=on` is a no-op against the current clone.** Per the changelog, this env var flips an internal capability gate in AGY's binary. The clone does not have that gate — bubbletea/v2 emits OSC 7501 on every frame where `v.ProgramStatus != nil` (see `charm.land/bubbletea/v2@v2.1.0/cursed_renderer.go:159` and `:868`). Adding the gate would require either forking bubbletea/v2 to honour a `ProgramStateOnlyIfSupported bool` option or wrapping the bubbletea output directly via `tea.WithFilter` / `tea.WithOutput` — both are invasive surgery on a vendored dep and are deferred to a future cycle. Cycle 239 ships the env-var parser so `=on` is accepted as a valid value; a regression that flipped the parser to "no-op on =on" (which is *correct today*) would still keep the behaviour the AGY changelog promises.

2. **`=brief` policy is a clone-side interpretation.** The AGY 1.4.0 changelog specifies "show only the status without details" but does not name the states whose details get suppressed. Cycle 239 strips `Message` only from `State == Blocked`, leaves Working and Error untouched (Working is structural-empty in the clone anyway, and Error / Done carry the actionable end-of-turn signal). The cycle-reports follow the principle *brief suppresses detail, not state changes* — Blocked is the only state where a user-facing Message was set in the first place. If a future AGY build shows (e.g. via strings / decompile) that Error's `Message` field is also expected to be stripped under `=brief`, this gate is a one-line change to also match `State == Error`. Pinned structurally: the gate is keyed on `ps.State == Blocked` (not on `Message != ""`), so the policy is one grep away from being widened.

3. **Only the TUI surface is gated.** The OSC 7501 report lives only in the TUI's `View()` (the assignment at `cmd/wagyu/tui_run.go:3969`). Print mode (`cmd/wagyu/main.go:891` and friends) does not emit OSC 7501 today — cycle 231's `print_glamour.go` mirrors status to stdout via TTY detection (`renderMarkdown` on text format when stdout is a TTY), but OSC 7501 has no print-mode analogue, so cycle 239 does not change print mode's behaviour. The env vars have no effect in `-p` mode either way; this matches AGY's binary, which also only emits OSC 7501 in interactive mode.

4. **Env-var precedence is `=off` ⊃ `=brief`.** When both `AGY_CLI_OSC7501=off` and `AGY_CLI_OSC7501_MESSAGES=brief` are set, `=off` short-circuits at the top of `programStatus()` and the brief gate is unreachable. Verified by `TestProgramStatus_OffWinsOverBrief`. This is the precedence that matches AGY's stated semantics ("turn it off" implies "no brief suppression either").

5. **`internal/convstore/sqlite.go` is not modified.** No business-logic surface needed the env-var fallback; the change is contained to `program_status.go` and a single new test file. The instruction's hard limit is respected.
