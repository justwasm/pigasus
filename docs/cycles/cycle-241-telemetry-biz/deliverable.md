# Cycle 241 — lock the `/config` `Enable Telemetry` row for Business accounts (AGY 1.4.1 alignment)

## TL;DR

| What | Where |
|---|---|
| New `IsBusinessAccount()` helper (extracted from `isADCEnabled`) | `cmd/wagyu/business_account.go` |
| `Enable Telemetry` row locked: literal `"off (disabled)"` display when `IsBusinessAccount()` | `cmd/wagyu/slash_panels.go` (`configDisplayFor`, `configRowValueAt`) |
| `Enable Telemetry` toggle refused: ←/→ is a no-op and prints the disabled-for-your-account-type message | `cmd/wagyu/slash_panels.go` (`cycleConfigValue`) |
| Description slot under the locked row paints the AGY-recovered Business text | `cmd/wagyu/slash_panels.go` (`panelConfigLines`) |
| Existing `--remote-control` × ADC gate (1.3.3) retargeted onto the same helper | `cmd/wagyu/remote_control_adc.go` |
| 7 new regression tests pinning the 1.4.1 contract | `cmd/wagyu/slash_panels_telemetry_test.go` |
| 2 existing tests renamed (`isADCEnabled` → `IsBusinessAccount`) | `cmd/wagyu/remote_control_adc_test.go` |

Build: `go build -o /tmp/wagyu-cycle241 ./cmd/wagyu` → exit 0, 85 MB.
Tests: `go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...` → 57/57 packages green.

---

## AGY 1.4.1 spec

The release notes (line 8 of `internal/cli/testdata/changelog_reference.txt`,
recovered verbatim from the AGY 1.4.2 binary's output):

> "Fixed `Enable Telemetry` in `/config` remaining editable for Business
> accounts (such as Google Cloud project and Application Default Credentials
> sign-in via `AGY_ADC_AUTH`), where prompts, content, and model responses
> are never collected: **the row now shows `off (disabled)` and explains
> why**, and first-run setup with `AGY_ADC_AUTH` now shows the Google Cloud
> Platform Terms of Service without the consumer telemetry opt-in checkbox."

Two pieces:

1. **`/config` row lock** — the `Enable Telemetry` row must paint
   `"off (disabled)"` and a Business-specific explanation when the user is
   signed in via a Business account; ←/→ must be a no-op.
2. **First-run setup** — the consumer telemetry opt-in checkbox must be
   suppressed when `AGY_ADC_AUTH` is set; the Google Cloud Platform ToS is
   shown instead.

This cycle ships piece 1 in full and pins piece 2 as a documented-but-
unimplemented gap (the first-run flow has many flags and is outside the
scope of the `/config` panel work — see Limitations).

---

## What the fix does

### Helper extraction (`cmd/wagyu/business_account.go`, new)

The clone already had a `isADCEnabled()` helper in
`cmd/wagyu/remote_control_adc.go:30-32` that read the `AGY_ADC_AUTH`
env var. Cycle 241 lifts the helper into a new file and renames it
`IsBusinessAccount()` because:

- The same env var is the signal for two different policy decisions
  (1.3.3 remote-control × ADC gate; 1.4.1 telemetry row lock), so a
  single helper prevents drift between the two call sites.
- `isADCEnabled` described *how the signal was read* (an env var named
  for ADC); `IsBusinessAccount` describes *what the signal means* in
  user-facing terms — the user's account is a Business one, so the
  consumer telemetry controls don't apply.

The new file also pins the AGY-recovered display literals as constants
so the tests can cross-check them without a string-table round-trip:

```go
const (
    telemetryProductName        = "Antigravity CLI"
    telemetryDisabledValue    = "off (disabled)"
    telemetryDisabledResponse = "Telemetry is disabled for your account type."
    telemetryRowName          = "Enable Telemetry"
)

func IsBusinessAccount() bool         // env-var check
func telemetryBusinessDescription() string  // the literal AGY paints
func isTelemetryRow(row configRow) bool     // catalogue-row check
```

### Display (`cmd/wagyu/slash_panels.go`)

- `configDisplayFor` (line 1886) now short-circuits to `"off (disabled)"`
  when the row is `Enable Telemetry` and `IsBusinessAccount()` is true.
  This is the lowest-level function the catalogue walks through, so the
  test path (`configSettingValue` → `configDisplayFor`), the panel
  render (`m.configRowValueAt` → `configSettingValue` →
  `configDisplayFor`), and any future caller all agree on the locked
  display.
- `configRowValueAt` (line 3710) carries the same short-circuit above
  the `memoryOnly` branch, so the in-memory `m.panel.configMemory` is
  never even consulted for the locked row.

### Toggle gate (`cmd/wagyu/slash_panels.go`)

- `cycleConfigValue` (line 3663) early-returns and surfaces the
  `slashRespond("/config", telemetryDisabledResponse)` line for the
  locked row. State is never mutated, so `m.panel.configMemory`,
  settings.json, and the next-paint read all stay consistent.
- The gate fires before the `cycleList < 2` length check would otherwise
  short-circuit, so the refusal is the same regardless of how many
  values the cycle list has.

### Description slot (`cmd/wagyu/slash_panels.go`)

- `panelConfigLines` (line 2013) overrides `rows[cursor].description`
  with `telemetryBusinessDescription()` when the cursor is on the
  locked row. The recovered literal (with `%s` substituted as the
  binary does):

  > "Disabled for Business accounts. Antigravity CLI does not collect any
  > of your prompts, content, or model responses, but will still collect
  > general usage patterns."

### `remote_control_adc.go` retarget

The pre-existing 1.3.3 gate now reads:

```go
func checkRemoteControlADC(fs *flag.FlagSet) error {
    if !remoteControlEnabled(fs) { return nil }
    if !IsBusinessAccount()      { return nil }   // was: if !isADCEnabled()
    return cli.ExitCodeError(1, remoteControlADCErrorMessage())
}
```

so the remote-control × ADC gate and the telemetry row lock share one
helper. The package-private `isADCEnabled` was removed; the test file
was renamed in lockstep (`TestIsADCEnabled_TruthyValues` →
`TestIsBusinessAccount_TruthyValues`, etc.).

---

## Files changed

| File | Δ | Why |
|---|---|---|
| `cmd/wagyu/business_account.go` | new, 110 lines | Cycle-241 helper, constants, recovered literals |
| `cmd/wagyu/remote_control_adc.go` | −18 / +8 lines | Remove `isADCEnabled`, retarget `checkRemoteControlADC` |
| `cmd/wagyu/remote_control_adc_test.go` | rename + 2-line comment update | Tests now call `IsBusinessAccount` |
| `cmd/wagyu/slash_panels.go` | +34 / −0 lines | Three new branches in `configDisplayFor`, `configRowValueAt`, `cycleConfigValue`, `panelConfigLines` |
| `cmd/wagyu/slash_panels_telemetry_test.go` | new, 330 lines | 7 cycle-241 regression tests |
| `docs/cycles/cycle-241-telemetry-biz/deliverable.md` | new | This file |

Net change: +478 lines, −18 lines. No deletions outside the helper
extraction.

---

## Tests

```
$ go test -vet=off -count=1 -run 'TestCycle241|TestIsBusinessAccount' -v ./cmd/wagyu/
=== RUN   TestCycle241_TelemetryBusinessLockedDisplay
--- PASS: TestCycle241_TelemetryBusinessLockedDisplay (0.00s)
=== RUN   TestCycle241_TelemetryPersonalUnchanged
--- PASS: TestCycle241_TelemetryPersonalUnchanged (0.00s)
=== RUN   TestCycle241_TelemetryToggleRefused
=== RUN   TestCycle241_TelemetryToggleRefused/AGY_ADC_AUTH_set:_toggle_is_no-op_and_surfaces_message
=== RUN   TestCycle241_TelemetryToggleRefused/AGY_ADC_AUTH_unset:_toggle_round-trips
--- PASS: TestCycle241_TelemetryToggleRefused (0.00s)
    --- PASS: TestCycle241_TelemetryToggleRefused/AGY_ADC_AUTH_set:_toggle_is_no-op_and_surfaces_message (0.00s)
    --- PASS: TestCycle241_TelemetryToggleRefused/AGY_ADC_AUTH_unset:_toggle_round-trips (0.00s)
=== RUN   TestCycle241_TelemetryBusinessPanelDescription
--- PASS: TestCycle241_TelemetryBusinessPanelDescription (0.00s)
=== RUN   TestCycle241_TelemetryBusinessPanelRow
--- PASS: TestCycle241_TelemetryBusinessPanelRow (0.00s)
=== RUN   TestCycle241_TelemetryLockedRowNotInjectedElsewhere
--- PASS: TestCycle241_TelemetryLockedRowNotInjectedElsewhere (0.00s)
=== RUN   TestCycle241_TelemetryBusinessFirstRunSkipOptIn
=== RUN   TestCycle241_TelemetryBusinessFirstRunSkipOptIn/AGY_ADC_AUTH_set:_helper_returns_true
=== RUN   TestCycle241_TelemetryBusinessFirstRunSkipOptIn/AGY_ADC_AUTH_unset:_helper_returns_false
--- PASS: TestCycle241_TelemetryBusinessFirstRunSkipOptIn (0.00s)
    --- PASS: TestCycle241_TelemetryBusinessFirstRunSkipOptIn/AGY_ADC_AUTH_set:_helper_returns_true (0.00s)
    --- PASS: TestCycle241_TelemetryBusinessFirstRunSkipOptIn/AGY_ADC_AUTH_unset:_helper_returns_false (0.00s)
=== RUN   TestCycle241_TelemetryKeyHandlerRoutesToGate
--- PASS: TestCycle241_TelemetryKeyHandlerRoutesToGate (0.00s)
=== RUN   TestIsBusinessAccount_TruthyValues
=== RUN   TestIsBusinessAccount_TruthyValues/true
=== RUN   TestIsBusinessAccount_TruthyValues/one
=== RUN   TestIsBusinessAccount_TruthyValues/uppercase_TRUE
=== RUN   TestIsBusinessAccount_TruthyValues/padded
=== RUN   TestIsBusinessAccount_TruthyValues/arbitrary_string
--- PASS: TestIsBusinessAccount_TruthyValues (0.00s)
    --- PASS: TestIsBusinessAccount_TruthyValues/true (0.00s)
    --- PASS: TestIsBusinessAccount_TruthyValues/one (0.00s)
    --- PASS: TestIsBusinessAccount_TruthyValues/uppercase_TRUE (0.00s)
    --- PASS: TestIsBusinessAccount_TruthyValues/padded (0.00s)
    --- PASS: TestIsBusinessAccount_TruthyValues/arbitrary_string (0.00s)
=== RUN   TestIsBusinessAccount_FalsyValues
=== RUN   TestIsBusinessAccount_FalsyValues/unset
=== RUN   TestIsBusinessAccount_FalsyValues/empty
=== RUN   TestIsBusinessAccount_FalsyValues/whitespace
=== RUN   TestIsBusinessAccount_FalsyValues/tab
--- PASS: TestIsBusinessAccount_FalsyValues (0.00s)
    --- PASS: TestIsBusinessAccount_FalsyValues/unset (0.00s)
    --- PASS: TestIsBusinessAccount_FalsyValues/empty (0.00s)
    --- PASS: TestIsBusinessAccount_FalsyValues/whitespace (0.00s)
    --- PASS: TestIsBusinessAccount_FalsyValues/tab (0.00s)
PASS
ok  	github.com/chadgpt/openagy/cmd/wagyu	1.305s
```

### What each test pins

| Test | Pins |
|---|---|
| `TestCycle241_TelemetryBusinessLockedDisplay` | `IsBusinessAccount()=true` ⇒ row paints `"off (disabled)"` via both `m.configRowValueAt(idx)` and `configSettingValue(idx)` |
| `TestCycle241_TelemetryPersonalUnchanged` | `IsBusinessAccount()=false` ⇒ row cycles `"on"` → `"off"` → `"on"` (no regression for personal accounts) |
| `TestCycle241_TelemetryToggleRefused/AGY_ADC_AUTH_set` | ←/→ is a no-op; transcript carries the refusal message; `m.panel.configMemory` is not mutated |
| `TestCycle241_TelemetryToggleRefused/AGY_ADC_AUTH_unset` | ←/→ round-trips normally; no refusal message on the transcript |
| `TestCycle241_TelemetryBusinessPanelDescription` | Cursor on the locked row paints the Business description (which mentions `Antigravity CLI`); the personal-account panel renders the pre-241 string |
| `TestCycle241_TelemetryBusinessPanelRow` | Cursor row paints `> Enable Telemetry` with `off (disabled)` in the value column |
| `TestCycle241_TelemetryLockedRowNotInjectedElsewhere` | Only `Enable Telemetry` gets the locked display; Agent Mode, Animation Speed, Color Scheme, Show Tips, Verbosity, Notifications, and Non-Workspace Access keep their normal cycle |
| `TestCycle241_TelemetryBusinessFirstRunSkipOptIn` | `IsBusinessAccount()` returns true under `AGY_ADC_AUTH` and false otherwise — pins the helper contract that the first-run consumer would consult (cycle-241 ships no first-run change) |
| `TestCycle241_TelemetryKeyHandlerRoutesToGate` | End-to-end path: `handlePanelKey(→)` on `/config` with cursor on `Enable Telemetry` reaches `cycleConfigValue`, paints the locked display, and surfaces the refusal message in the transcript |
| `TestIsBusinessAccount_TruthyValues` | `true` / `1` / `TRUE` / `  yes  ` / `adc-from-gcloud-cli` all flip the helper true |
| `TestIsBusinessAccount_FalsyValues` | unset / empty / `   ` / `\t` all flip the helper false |

---

## Test results

```
$ go build -o /tmp/wagyu-cycle241 ./cmd/wagyu
$ ls -lh /tmp/wagyu-cycle241
-rwxr-xr-x 1 gear staff 85M Oct 18 21:52 /tmp/wagyu-cycle241

$ go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...
ok  	github.com/chadgpt/openagy/cmd/wagyu	40.803s
ok  	github.com/chadgpt/openagy/internal/agent	0.604s
…57 packages green, 0 FAIL lines…
```

All 57 packages green, no pre-existing test regressed. The
`-vet=off` flag is the project's standing convention; the one
`go vet ./cmd/wagyu/` warning (`model_default_effort_test.go:67`,
`Errorf` arity) is pre-existing on `main` and unrelated to cycle 241.

---

## Cycle choice — helper rename vs. duplicate

Per the task, the **preferred path** was option 1: "extract the helper
from `internal/cli/remote_control_adc.go` into a new
`internal/cli/business_account.go` exposing `func IsBusinessAccount()
bool` (and rename the old caller in `remote_control_adc.go` to use it)".

(The task's prompt referenced `internal/cli/` as the location; in this
checkout the existing helper actually lives in
`cmd/wagyu/remote_control_adc.go`, so the new file was created at
`cmd/wagyu/business_account.go` to keep it co-located with the existing
helper.)

I went with the rename. The grep across non-test files touched:

1. `cmd/wagyu/remote_control_adc.go` — removed `isADCEnabled`, retargeted
   `checkRemoteControlADC` onto `IsBusinessAccount`.
2. `cmd/wagyu/remote_control_adc_test.go` — renamed the two
   `TestIsADCEnabled_*` tests to `TestIsBusinessAccount_*` and updated
   the function calls inside.

That's 2 files, under the "≥3 non-test files" threshold the task set
for the alternative path. The rename is also strictly less code than
the duplicate-and-route option (no two helpers reading the same env var).

---

## 硬约束 / Limitations

### 1. The "is this a Business account" signal is purely the env var

`IsBusinessAccount()` reads `$AGY_ADC_AUTH` and treats any non-empty
trimmed value as "Business requested". The recovered AGY binary does
not include a stronger signal in the strings/seed corpora — there is
no separate "tier" string, no OAuth-scope fragment, and no corporate-
tenancy metadata flag the clone could inspect at runtime. The
underlying auth flow itself stops at `keyringAuth.TryAuth` in the
recovered shell (cf. `out/decompile/
decompile-google3_third_party_jetski_cli_backend_auth_auth..md`) and
the ADC path is left as a stub.

A stronger detection would need either (a) the real auth-pipeline
recovery work that the cycle-241 prerequisite describes, or (b) a
recovery pass against `~/.local/bin/agy` that surfaces the
"tier-licensed" / "Business" signals the binary actually uses. This
cycle ships the env-var heuristic and notes it as a future-cycle
upgrade path.

### 2. First-run setup suppression is documented but unimplemented

The release-note sentence has two clauses:

> "the row now shows `off (disabled)` and explains why, **and first-run
> setup with `AGY_ADC_AUTH` now shows the Google Cloud Platform Terms
> of Service without the consumer telemetry opt-in checkbox**."

Cycle 241 ships the first clause (the `/config` row lock) in full and
pins the second clause only by exporting `IsBusinessAccount()` as the
helper a first-run consumer would consult. The clone's first-run
flow lives outside this cycle's file-list (it spans
`internal/onboarding/`, `cmd/wagyu/slash_first_run.go` /
similar) and has its own flags that would need to be modelled. A
follow-up cycle would import `IsBusinessAccount()` (or move the helper
to `internal/cli/` so the first-run package can read it without
depending on the binary entry point) and gate the consumer-opt-in
checkbox.

### 3. The toggle refusal message text is a clone-side interpretation

The binary does not appear to print a dedicated line for the failed
toggle (the panel's row value does not change and the user can infer
the lock from the dim grey "off (disabled)" value plus the explanation
in the description slot). The clone-side message
`telemetryDisabledResponse = "Telemetry is disabled for your account
type."` is the clearest, most visible way to surface the lock and
matches the `/command` "Exited /cmd command" / `⎿` shape other panels
emit when rejecting user input. A binary-recovery pass before a
future release should tighten this to whatever AGY actually prints
(if anything); the constant is a one-line change.

### 4. The description literal's product name is pinned

The AGY-recovered string is
`Disabled for Business accounts. %s does not collect any of your
prompts, content, or model responses, but will still collect general
usage patterns.` with `%s = "Antigravity CLI"`. The clone pins both
the placeholder-substituted final form (in `telemetryBusinessDescription`)
and the product name as a separate constant (`telemetryProductName`).
If the recovered product name ever changes (e.g. "Antigravity CLI"
becomes "Gemini CLI"), only the one constant needs to flip. No test
relies on the literal product-name string outside the helper's own
existence check; the panel test only checks that the description
mentions the constant value, so a rename propagates without touching
the test.

### 5. `go vet` warning is pre-existing, not cycle-241

`go vet ./cmd/wagyu/` reports
`model_default_effort_test.go:67:3: (*testing.common).Errorf call needs
2 args but has 3 args`. This is the project-wide `-vet=off` reason;
the warning exists on `main` and is unrelated to cycle 241.