# Cycle 264 — `--remote-control` × ADC: AGY_ERROR exit code + picker hide + slash error

**Branch:** `feature/cycle-264-rc-adc` (base `416a56d`).
**Worktree:** `/Users/gear/Documents/GitHub/openagy-rc-adc`.
**AGY changelog gap closed:** 1.4.1 line 79.

---

## 1. TL;DR

AGY 1.4.1 changelog line 79 (which is the same surface the
recovered shell closed at the 1.3.3 line 53 level in cycle
`remote-control-adc-exit`) tightens the `-remote-control` × ADC
contract so the user gets a clear, recovery-path-complete error in
every place the command shows up, not just on the wire:

> "Fixed `--remote-control` failing when you sign in with
>  Application Default Credentials (`AGY_ADC_AUTH`); Remote Control
>  doesn't support ADC yet, so the flag now exits with a clear
>  error and the `/remote-control` command is hidden."

Cycle 264 lifts three surfaces from the cycle-241 baseline to the
1.4.1 contract:

| # | Surface                | Where                | Behaviour             |
|---|------------------------|----------------------|-----------------------|
| 1 | `--remote-control` flag | `main.go:346` `checkRemoteControlADC` | exit **3** (AGY_ERROR), error names `AGY_ADC_AUTH` and `/logout` |
| 2 | `/remote-control` slash command | `slash_commands.go:490` | transcript error via `slashRespond` wrapper; session flag does NOT flip |
| 3 | `/remote-control` picker entry | `internal/tuilib/picker.go` `PickerState.Update` + `SetHiddenCommands` | the row is filtered out for both the `/` menu and the `/remote` substring match |

The detection signal stays where cycle 241 put it
(`cmd/wagyu/business_account.go` `IsBusinessAccount()`), so adding
or moving future "ADC-active means feature X is hidden" gates is a
one-line edit.

---

## 2. Files

| File | Status | Lines | What it does |
|---|---|---|---|
| `cmd/wagyu/remote_control_adc.go` | edit | +50 / −19 | exit code 1 → 3; new error message naming the env var + `/logout`; new `remoteControlSlashADCErrorMessage()` helper for the slash-command surface |
| `cmd/wagyu/remote_control_adc_test.go` | edit | +18 / −10 | existing 1.3.3 flag tests now pin exit code 3 and the new substrings; `TestRunRootGateReturnsExitCodeOne` renamed to `…Three` |
| `cmd/wagyu/rc_adc_test.go` | new | 320 | 6 cycle-264 tests covering the headline contract end-to-end (flag gate, slash command, picker hide + visible, env detection) |
| `cmd/wagyu/slash_commands.go` | edit | +14 | `case "remote-control"`: short-circuit under ADC via `IsBusinessAccount()`; the session flag is left untouched |
| `cmd/wagyu/tui_run.go` | edit | +37 / −4 | `pickerHiddenCommands()` helper (`IsBusinessAccount` ⇨ `{"/remote-control"}`); NewRunMode applies the hidden set once at construction |
| `internal/tuilib/picker.go` | edit | +59 / −2 | `PickerState.hiddenCommands` field + `SetHiddenCommands(names ...string)`; `Update` filters the post-`FilterSlashCommands` rows against the hidden set |
| `internal/cli/testdata/feature_diff_table.json` | edit | +7 | new "cli" cycle-264 entry pinning the gap as closed |

`internal/convstore/sqlite.go` was not touched (hard constraint).
No third-party deps added.

---

## 3. Why exit code 3 (AGY_ERROR)

AGY 1.4.1 moved auth-related "the daemon never started" failures
into the same category as the print-mode model API errors, which
have used exit code 3 since cycle 112 (see `cmd/wagyu/api_error.go`
`headlessAPIExitCode`). The print-mode error formatter emits the
structured `AGY_ERROR: {...}` JSON line on stderr and exits 3, so
`--remote-control` under ADC now lands in the same exit-code
bucket — meaning a shell script probing for `rc==3` to detect
"model/auth refused to handle this turn" catches both failure
modes.

The cycle-241 value (1) is preserved in the existing
`internal/cli` test pins that don't depend on this gate (no test
breaks from the change, see §7).

---

## 4. Error message contract

Single source of truth for the **flag** path:
`remoteControlADCErrorMessage()` in
`cmd/wagyu/remote_control_adc.go`. The string contains every
recovery-path substring a future reader might grep:

```
error: --remote-control cannot be used with Application Default
Credentials (AGY_ADC_AUTH) sign-in; Remote Control does not support
ADC yet — sign out with /logout and unset AGY_ADC_AUTH (or use a
different sign-in method) and retry.
```

Substrings the tests pin (`TestRemoteControl_FailsUnderADC`):

| Substring           | Reason                                              |
|---------------------|-----------------------------------------------------|
| `remote-control`    | grep-able from CI logs                             |
| `Application Default` | the human-readable name AGY uses in its docs       |
| `Credentials`       | (paired with above)                                |
| `AGY_ADC_AUTH`      | the env var the user can unset                      |
| `/logout`           | the recovery command (AGY 1.4.1 line 79 contract)  |

Single source of truth for the **slash-command** path:
`remoteControlSlashADCErrorMessage()`. Same five substrings, shaped
to render cleanly inside the existing `slashRespond` wrapper:

```
Remote Control does not yet support Application Default
Credentials (AGY_ADC_AUTH) sign-in. Sign out with /logout and
unset AGY_ADC_AUTH (or use a different sign-in method) and retry.
```

Note the missing `error:` prefix — slash command echoes don't use
the `error: …` shape (`cmd/wagyu/slash_actions.go:80 rejected` does;
`cmd/wagyu/slash_commands.go:73 slashRespond` does not). The two
messages would diverge visually if they shared a body; instead they
share the same five substrings, which the test pins.

---

## 5. Picker filter wiring

`internal/tuilib/picker.go` gains a single instance field plus a
single setter so the picker can be told "ignore this set of names"
without a function-signature change to `FilterSlashCommands`:

```go
type PickerState struct {
    ...
    // hiddenCommands is a session-level set of slash command names
    // to exclude from the picker regardless of the buffer.
    hiddenCommands map[string]bool
}

// SetHiddenCommands replaces the picker's hidden-name set. Empty
// or nil arg list clears the set (the picker behaves like the
// cycle-83 baseline). Names are normalised to start with "/".
func (p *PickerState) SetHiddenCommands(names ...string)
```

`Update` keeps the cycle-83 contract on the empty case (no hidden
names → no per-row filter) and adds a single in-place reallocation
when the set is non-empty:

```go
p.all = FilterSlashCommands(prefix)
if len(p.hiddenCommands) > 0 && len(p.all) > 0 {
    filtered := p.all[:0:0]
    for _, c := range p.all {
        if p.hiddenCommands[c.Name] { continue }
        filtered = append(filtered, c)
    }
    p.all = filtered
}
```

`FilterSlashCommands` itself is unchanged, so the cycle-83 /
cycle-146 / cycle-184 / cycle-235 invariants pinned by the picker
tests (which compare the catalog against `DefaultSlashCommands`
alone) remain green.

`cmd/wagyu.tui_run.go` `NewRunMode` reads
`pickerHiddenCommands()` once at construction time:

```go
func pickerHiddenCommands() []string {
    if !IsBusinessAccount() { return nil }
    return []string{"/remote-control"}
}
```

…and applies it via `SetHiddenCommands(pickerHidden...)` after the
struct literal. Calling once (vs on every keystroke) is correct
because `IsBusinessAccount` reads an environment variable that the
session does not mutate in place — a `/logout` would also reset
the session, not just the env var.

The TUI slash command handler in `cmd/wagyu/slash_commands.go` is
the third surface the cycle touches: even if the user finds the
command by typing `/remote-control` and pressing Enter (the picker
is hidden, but the buffer is still open), the handler short-
circuits with the same canonical message and **does not flip**
`m.remoteControl`. A stale `true` in `m.remoteControl` could
otherwise feed the next session's banner / take-over URL link, so
this is pinned by
`TestRemoteControl_FailsUnderADCSlashCommand`.

---

## 6. Test matrix (cycle-264 surface)

| # | Test                                                  | AGY_ADC_AUTH | `--remote-control` | picker hides | Expected |
|---|-------------------------------------------------------|--------------|--------------------|--------------|----------|
| 1 | `TestRemoteControl_FailsUnderADC`                     | `1`          | set                | —            | exit 3 + substrings |
| 2 | `TestRemoteControl_FailsUnderADCSlashCommand`         | `1`          | —                  | —            | slash transcript error; `m.remoteControl == false` |
| 3 | `TestRemoteControl_AllowedUnderOAuth`                 | unset        | set                | —            | gate returns nil; slash flips `m.remoteControl == true` |
| 4 | `TestRemoteControl_HiddenUnderADCInSlashMenu`         | `1`          | —                  | yes          | bare `/` and `/remote` rows exclude `/remote-control` |
| 5 | `TestRemoteControl_VisibleUnderOAuthInSlashMenu`      | unset        | —                  | no           | bare `/` includes `/remote-control`; `/remote` filter returns exactly 1 row |
| 6 | `TestRemoteControl_ADCDetectionViaEnv`                 | varies       | —                  | —            | env-var pivot (`1` / `true` / `yes` / whitespace → bool) |

`FilterSlashCommands` is **not** in the matrix — the test
`Test_PickerCatalogMatchesRef` still expects the cycle-147-base 95
filtered rows under the empty-prefix `/`, because `FilterSlashCommands`
is unchanged and `SetHiddenCommands` only fires under the ADC
predicate.

All six tests pass on a fresh run:

```
$ go test -vet=off -count=1 -run "TestRemoteControl_" ./cmd/wagyu/
ok  	github.com/chadgpt/openagy/cmd/wagyu	1.282s
```

---

## 7. Build / test / vet

| Check | Result |
|---|---|
| `gofmt -l cmd/wagyu/rc_adc_test.go cmd/wagyu/remote_control_adc.go cmd/wagyu/remote_control_adc_test.go cmd/wagyu/slash_commands.go cmd/wagyu/tui_run.go internal/tuilib/picker.go` | empty |
| `go build -o /tmp/wagyu-cycle264 ./cmd/wagyu` | exit 0, 89 MB binary (Mach-O arm64) |
| `go test -vet=off -count=1 ./internal/...` | all green |
| `go test -vet=off -count=1 -skip TestCycle225QuotaCountdown ./cmd/...` | all green |

Pre-existing flake: `TestCycle225QuotaCountdown` in
`cmd/wagyu/slash_cycle225_test.go:106` is wall-clock-sensitive
(the test calls `quotaCountdown(time.Now().Add(d))` and expects
the millisecond drift between `time.Now()` and the formatter's
internal time read to cross only a 1-second boundary; on a loaded
host the call sometimes loses 2 seconds of drift and gets "59m
1s" instead of "59m 0s"). The failure reproduces on the base
commit `416a56d` before any of cycle-264's files exist
(`git stash -u` + reproduce on `416a56d`), so it is unrelated to
this cycle and not fixed here. The cycle-241 deliverable
(`docs/cycles/remote-control-adc-exit/deliverable.md` §7) lists
the same flake as documented pre-existing.

---

## 8. Manual smoke

```
$ env AGY_ADC_AUTH=1 /tmp/wagyu-cycle264 --remote-control
error: --remote-control cannot be used with Application Default Credentials (AGY_ADC_AUTH) sign-in; Remote Control does not support ADC yet — sign out with /logout and unset AGY_ADC_AUTH (or use a different sign-in method) and retry.
$ echo $?
3
```

```
$ env -u AGY_ADC_AUTH /tmp/wagyu-cycle264 --help | head -3
Usage of wagyu-cycle264:
  -add-dir string
```

```
$ env AGY_ADC_AUTH=1 /tmp/wagyu-cycle264 -p /remote-control
{"error":"/remote-control is not available in print mode (the session ends when the run completes, leaving nothing for another device to control); use --remote-control instead, or pass --disable-slash-commands to send /remote-control to the model as literal text"}
$ echo $?
2
```

The `-p` print path still rejects `/remote-control` with the
cycle-99 "session ends when the run completes" reason because the
print-mode rejection has a separate surface (the rejection's text
argues print mode can't host the tunnel regardless of auth) and
because `--remote-control` plus `--print` triggers the cycle-264
flag gate first with the new error and exit 3.

---

## 9. Hard constraints / 存疑 / limitations

- `PickerState.hiddenCommands` is a session-level field set once
  at construction. A future cycle that allows `/logout` and
  `/login` mid-session will need to call `SetHiddenCommands` again
  from each handler (or wire the picker to poll
  `IsBusinessAccount()` per keystroke). Not needed today because
  in-session auth swap isn't implemented.
- The picker `Update` re-allocates `p.all` only when
  `hiddenCommands` is non-empty AND `FilterSlashCommands`
  returned ≥1 row. The slice is reallocated via `p.all[:0:0]` so
  the underlying array from `FilterSlashCommands` is reused
  (no extra GC churn) — pinned by reading
  `tuilib/commands.go:188 FilterSlashCommands` which already
  returns an owned slice.
- The `rc_adc_test.go` tests use `fakeClient` from
  `cmd/wagyu/tui_run_test.go` and call `NewRunMode` to build a
  runMode because `handleSlashCommand` resets
  `m.input` (a `bubbles/v2/textarea` Model that owns a viewport).
  A bare `&runMode{}` panics on the same line the production TUI
  uses. The `slashRemoteControlRunMode(t)` helper documents why.
- Cycle 264 adds 6 tests; the cycle-264 path on this branch
  asserts the **AGY 1.4.1 contract**, not the 1.3.3 contract.
  The 1.3.3 tests in `remote_control_adc_test.go` were edited
  in this cycle to pin the exit code + substrings; they remain
  green.
- Did not push or merge. Branch is local on
  `feature/cycle-264-rc-adc`.
