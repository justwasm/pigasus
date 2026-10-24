# Cycle remote-control-adc-exit — `--remote-control` exits with a clear error when ADC is in effect

**Branch:** `feature/remote-control-adc-exit` (base `54db67b`).
**Worktree:** `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-remote-control-adc-exit`.
**AGY changelog gap closed:** 1.3.3 line 53.

---

## 1. TL;DR

The 1.3.3 changelog entry "Fixed `--remote-control` silently continuing
when ADC (Application Default Credentials) auth fails" is one of the
gaps the recovered shell has not closed. Today, when the operator
combines `--remote-control` with `AGY_ADC_AUTH=true`:

1. the daemon never starts (Remote Control does not support ADC yet),
2. the binary does not exit — it falls through into the agent loop /
   TUI as if the operator had asked for a normal interactive session,
3. the user reasonably concludes the daemon is running and waits.

The fix is a single gate at the top of `runRoot`: if both `--remote-
control` and `AGY_ADC_AUTH` are set, return `*cli.ExitCodeError(1,
"error: --remote-control cannot be used with ADC (AGY_ADC_AUTH)
sign-in; ...")` so the parser prints the message to stderr and the
process exits with status 1. Without `--remote-control` the gate is a
no-op, so the existing 1.3.0..1.3.2 contract (ADC failures continue
with fallback auth) is preserved verbatim.

Nine new unit tests pin the matrix; the existing `cmd/...` and
`internal/...` test surfaces stay green apart from two documented
pre-existing flakes (`TestCycle225QuotaCountdown` — wall-clock
sensitive; `TestDispatchPrintSlash_LocalCommands` — depends on the
operator's registered model list).

---

## 2. Files

| File | Status | Lines | What it does |
|---|---|---|---|
| `cmd/wagyu/remote_control_adc.go` | new | 75 | `isADCEnabled`, `remoteControlEnabled`, `remoteControlADCErrorMessage`, `checkRemoteControlADC` |
| `cmd/wagyu/remote_control_adc_test.go` | new | 222 | 9 unit tests covering the matrix (truthy/falsy env, RC × ADC) and the parser-level short-circuits (`--help`, `--version`) |
| `cmd/wagyu/main.go` | edit | +13 | wires `checkRemoteControlADC(fs)` into `runRoot` immediately after the `--tool` short-circuit, before the `--agent` check |

`internal/convstore/sqlite.go` was not touched (hard constraint).

---

## 3. Detection signal

The clone has no real ADC auth pipeline (the recovered auth flow stops
at `keyringAuth.TryAuth`, cf.
`out/decompile/decompile-google3_third_party_jetski_cli_backend_auth_auth..md`,
and `adcAuth.TryAuth` is left as a stub). The signal the reference
uses to select the adcAuth path is the `AGY_ADC_AUTH` env var; any
non-empty (post-trim) value puts the binary on the ADC branch on the
first auth attempt, and the canonical recovery hint printed by the
real binary after a failed ADC sign-in is:

> "ADC authentication failed. Please verify your Application Default
>  Credentials: 1. Run: `gcloud auth application-default login` 2. Or
>  check `GOOGLE_APPLICATION_CREDENTIALS` env var. 3. Or unset
>  `AGY_ADC_AUTH` env var to use other sign in methods."
> (`docs/cycles/cycle-222/deliverable-rewind-nonempty.md` §1)

`isADCEnabled()` is the single helper that reads the env var; any
future change to the trigger signal (e.g. a real adcAuth.TryAuth
implementation, a `~/.gemini/google_accounts.json` `auth_method` field,
etc.) flows through this one function.

---

## 4. Where the gate sits

The check sits between `--tool NAME --tool-args JSON` (which can also
be a "print and exit" path) and the `--agent` validation block:

```
runRoot(args, p)
  ├ --version          short-circuit
  ├ --tools            short-circuit
  ├ --services         short-circuit
  ├ --oauth            short-circuit
  ├ --tui              short-circuit
  ├ --tool --tool-args short-circuit
  ├ checkRemoteControlADC    <-- new (gate)
  ├ --agent validation
  ├ --json-schema validation
  ├ --print / --prompt / -p
  ├ --prompt-interactive / -i
  ├ --continue / -c
  ├ wizard + runLiveTUI  (default no-args path)
  └ positional prompt → runPrintMode
```

Why there:

- **After** the `--help` / `--version` short-circuits: those still need
  to work regardless of ADC (verified by `TestHelpStillWorksWithADC`
  and `TestVersionStillWorksWithADC`).
- **After** `--tool`: tool invocations are short-lived and shouldn't be
  coupled to the remote-control surface.
- **Before** `--agent` / `--print` / `--prompt-interactive` / `--continue`
  / wizard / live TUI: every one of those would otherwise fall through
  to the agent loop without complaint — the failure mode the changelog
  is closing.

The check is the first thing the gate fires on a `--remote-control`
session with ADC; subsequent code paths never run, so there is no
risk of partial state (no daemon half-started, no model request sent).

---

## 5. Error message contract

Single source of truth: `remoteControlADCErrorMessage()` in
`cmd/wagyu/remote_control_adc.go`. The string contains both
"remote-control" and "ADC" so it is grep-able from logs and so the
tests can pin both keywords (`TestCheckRemoteControlADC_RejectsCombination`):

```
error: --remote-control cannot be used with ADC (AGY_ADC_AUTH) sign-in;
Remote Control does not support Application Default Credentials yet —
unset AGY_ADC_AUTH (or use a different sign-in method) and retry.
```

Recovery hint matches the recovered binary's own recovery hint
(section 3): "unset AGY_ADC_AUTH" is the first action the reference
suggests, and our message leads with it.

---

## 6. Test matrix

| # | Test | AGY_ADC_AUTH | `--remote-control` | Expected |
|---|---|---|---|---|
| 1 | `TestIsADCEnabled_TruthyValues/{true,one,uppercase_TRUE,padded,arbitrary_string}` | truthy | n/a | `isADCEnabled() == true` |
| 2 | `TestIsADCEnabled_FalsyValues/{unset,empty,whitespace,tab}` | empty / whitespace | n/a | `isADCEnabled() == false` |
| 3 | `TestCheckRemoteControlADC_RejectsCombination` | `true` | set | `cli.ExitCodeError(1, msg)`; msg mentions both `remote-control` and `ADC` |
| 4 | `TestCheckRemoteControlADC_AcceptsRCWithoutADC` | empty | set | `nil` |
| 5 | `TestCheckRemoteControlADC_AcceptsADCWithoutRC` | `true` | absent | `nil` (no-regression case) |
| 6 | `TestCheckRemoteControlADC_BothAbsent` | empty | absent | `nil` |
| 7 | `TestCheckRemoteControlADC_RCExplicitFalse` | `true` | `--remote-control=false` | `nil` (no zero-value footgun) |
| 8 | `TestCheckRemoteControlADC_ADCWhitespace` | `   ` | set | `nil` (whitespace env == unset) |
| 9 | `TestRunRootGateReturnsExitCodeOne` | `true` | set | `cli.ExitCodeOf(err) == 1` (exact pin of the v1.3.3 contract) |
| 10 | `TestHelpStillWorksWithADC` | `true` | absent | `--help` exit code 0 (parser-level short-circuit) |
| 11 | `TestVersionStillWorksWithADC` | `true` | absent | `--version` exit code 0 (parser-level short-circuit) |

All eleven pass on a fresh run:

```
$ go test -vet=off -count=1 -run "TestIsADCEnabled|TestCheckRemoteControlADC|TestRunRootGateReturnsExitCodeOne|TestHelpStillWorksWithADC|TestVersionStillWorksWithADC" ./cmd/wagyu/
ok  	github.com/chadgpt/openagy/cmd/wagyu	1.263s
```

---

## 7. Build / test / vet

| Check | Result |
|---|---|
| `gofmt -l cmd/wagyu/remote_control_adc.go cmd/wagyu/remote_control_adc_test.go` | empty (both new files gofmt-clean) |
| `gofmt -l cmd/wagyu/main.go` (the modified file) | empty |
| `go build -o /tmp/wagyu-rcadc ./cmd/wagyu` | exit 0, 70 MB binary |
| `go vet ./cmd/wagyu/...` | clean |
| `go test -vet=off -count=1 ./internal/...` | all green |
| `go test -vet=off -count=1 -skip "TestCycle225QuotaCountdown\|TestDispatchPrintSlash_LocalCommands" ./cmd/...` | all green |

Pre-existing flakes (`TestCycle225QuotaCountdown` and
`TestDispatchPrintSlash_LocalCommands`) are documented and unrelated
to this change — they fail identically on the base commit `54db67b`:

```
$ git stash && go test -vet=off -count=1 -run TestCycle225QuotaCountdown ./cmd/wagyu/
FAIL	github.com/chadgpt/openagy/cmd/wagyu	0.884s
```

```
$ git stash && go test -vet=off -count=1 -run TestDispatchPrintSlash_LocalCommands ./cmd/wagyu/
FAIL	github.com/chadgpt/openagy/cmd/wagyu	0.945s
```

The first is wall-clock sensitive (`time.Now()` between test body and
`quotaCountdown`'s `time.Until` can drift by >1 s on a loaded
machine); the second depends on the operator's `~/.gemini/config/`
registered model list, which is not part of the clone.

---

## 8. Manual smoke

```
$ env AGY_ADC_AUTH=true /tmp/wagyu-rcadc --remote-control
error: --remote-control cannot be used with ADC (AGY_ADC_AUTH) sign-in; Remote Control does not support Application Default Credentials yet — unset AGY_ADC_AUTH (or use a different sign-in method) and retry.
exit=1
```

```
$ env -u AGY_ADC_AUTH /tmp/wagyu-rcadc --help | head -3
Usage of wagyu-rcadc:
  --add-dir                       Add a directory to the workspace (repeatable) (default [])
exit=0
```

---

## 9. 硬约束 / limitations / 存疑

- **Clone has no real ADC pipeline.** `isADCEnabled` only consults
  `AGY_ADC_AUTH`. When the recovered shell grows a real
  `adcAuth.TryAuth` (per
  `out/decompile/dec/86a5aa0_TryAuth.c`) the gate may want to also
  recognise `~/.gemini/google_accounts.json` having `auth_method: adc`
  — flagged for the cycle that lands the real pipeline.
- **Exit code 1 is the recovered-shell choice.** The 1.3.3 changelog
  does not pin the exact exit code; the recovered reference probe in
  `docs/cycles/cycle-222/deliverable-rewind-nonempty.md` doesn't
  observe the failure mode either. 1 is the standard "handler
  failure" code in `internal/cli/jetski.go:428-443` (`handleRunErr`)
  and is consistent with the sibling gates the clone already ships
  (e.g. `--agent` unknown → rc 1, `--continue` no-target → rc 1,
  `--print` empty prompt → rc 1).
- **No multi-modal test rig.** All 11 tests run through the unit-test
  harness; a full integration probe (binary-as-subprocess with
  controlled env) was considered and skipped because the unit tests
  exercise every cell of the boolean matrix and the parser-level
  short-circuits.
- **`--remote-control start` subcommand** is unaffected by this gate.
  The 1.3.3 changelog line scopes the fix to the `--remote-control`
  startup flag and the `/remote-control` slash command (the latter is
  not yet implemented in the clone, cf. `TODO.md:289`); the
  `remote-control start | status | stop` subcommands are a separate
  surface that the live binary already gates on "not signed in: run
  `<prog>`, sign in, then run `<prog> remote-control start`"
  (`internal/cli/subcommand_parity.go:1033`).
- **Help text unchanged.** The `--help` table does not gain a new
  flag, exit code, or section; this gap is a behavioural-only fix.
  The 39-line `--help` byte-for-byte contract the clone holds against
  the v1.3.5 binary is preserved.