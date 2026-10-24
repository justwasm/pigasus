# Cycle 104 — Auto mode and `Shift+Tab` mode cycling

**Target:** `agy` 1.3.6 (`~/.local/bin/agy`,
SHA-256 `827ba22b4aa33557614a57aac2cc13f386e17db6b802febd3932226906ac05ab`).

**Theme:** the headline 1.3.5 feature. `--mode` already selected a policy in the
clone, but the TUI could not *change* it: `Shift+Tab` was advertised in `/help`
(`  shift+tab                       Cycle mode`) and not bound to anything.

---

## 1. What was wrong

* **`Shift+Tab` was inert.** The `/help` shortcuts panel listed it; no key
  handler existed. `Update` only understood plain `Tab` (picker completion) and
  `Tab`/`←`/`→` inside the `/help` overlay.
* **Plan mode had no banner.** `ModeBanner` returned `""` for `ModePlan`, even
  though `Plan mode: research & plan only (shift+tab to cycle)` is present in
  the 1.3.6 string table (the earlier cycle that added the other two banners had
  only recovered those two).
* **The mode could not be changed at runtime.** `permissionRunner.policy` was a
  `permissions.Policy` **value**, so the tool gate held a private copy that no
  later mutation could reach.

## 2. Recovered evidence 【一手】

| String | Role |
|---|---|
| `Press shift+tab to cycle modes (default, accept-edits, plan).` | the shift+tab hint |
| `Accept-edits mode: file edits auto-approved (shift+tab to cycle)` | accept-edits banner |
| `Auto mode: edits & safe commands auto-approved (shift+tab to cycle)` | auto banner |
| `Plan mode: research & plan only (shift+tab to cycle)` | plan banner |
| 1.3.5 changelog: *“Choose it with `--mode auto`, from `Agent Mode` in `/config`, or by pressing `Shift+Tab` to cycle modes.”* | the feature description |

### The cycle-order discrepancy

The **only** shift+tab hint in the binary names three modes —
`(default, accept-edits, plan)` — and omits `auto`, which the 1.3.5 changelog
says Shift+Tab reaches. The hint text appears not to have been updated when Auto
mode shipped.

The cycle is therefore implemented as **default → accept-edits → auto → plan →
default**, following the changelog (the stated roadmap authority) over the stale
hint. The hint string itself is still rendered verbatim, when the cycle returns
to the default mode — that is the state it describes.

## 3. Implementation

* `internal/permissions/policy.go`
  * new `PlanBanner`; `ModeBanner(ModePlan)` now returns it;
  * `CycleHint` (the recovered default-mode string);
  * `CycleModes = []string{"", ModeAcceptEdits, ModeAuto, ModePlan}` and
    `NextMode(cur)`.
* `cmd/antigravity/main.go` — `permissionRunner.policy` is now
  `*permissions.Policy`.
* `cmd/antigravity/tui_v135.go`
  * `mode string` and `policy *permissions.Policy` fields;
  * `Shift+Tab` handler (`Code == KeyTab && Mod.Contains(ModShift)`), placed
    **after** the `/help` overlay block so the overlay keeps owning `Tab`;
  * the plain-`Tab` picker-completion guard now excludes `ModShift`;
  * `cycleMode()` advances the mode, writes it into the live policy, and appends
    the recovered banner (or, for the default mode, the recovered hint) to the
    transcript;
  * `runLiveTUI` builds **one** `sessionPolicy`, hands `&sessionPolicy` to the
    tool gate and stores it on the model — so a cycle takes effect on the next
    tool call.

The banner is rendered in the **transcript**, not the status row: the status row
is byte-verified against the reference's idle capture (`harness136` compares it)
and must not change. The reference's placement is 【推测】.

## 4. Verification

Live pty probe (40×120, throwaway `ANTIGRAVITY_HOME`), four `Shift+Tab` presses
(`\x1b[Z`) then `/zzz`:

```
Accept-edits mode: file edits auto-approved (shift+tab to cycle)
Auto mode: edits & safe commands auto-approved (shift+tab to cycle)
Plan mode: research & plan only (shift+tab to cycle)
Press shift+tab to cycle modes (default, accept-edits, plan).
> /zzz
Unknown command: /zzz
```

* `go build ./cmd/antigravity`, `go vet` — clean; `gofmt` clean.
* `go test ./cmd/... ./internal/...` — all packages pass.
* `harness136.sh` — **86/87** unchanged (the documented `/plugin` pre-auth diff).
* `harness137.sh` — **23/25** unchanged (both diffs documented in cycle 101).
* New tests: `TestNextMode`, updated `TestModeBanner`, `TestShiftTabCyclesMode`,
  `TestShiftTabStartsFromFlagMode`, `TestShiftTabDoesNotCompletePickerRow`.

## 5. Divergences and limitations

* **The hint text contradicts the cycle.** See §2 — deliberate, changelog over
  stale string.
* **Banner placement is 【推测】.** The reference shows the mode banner in the
  status line; the clone appends it to the transcript, because the status row is
  byte-verified.
* **`/config` → `Agent Mode` is still read-only.** The changelog names three
  ways to choose the mode: `--mode`, `/config`, and `Shift+Tab`. Only `--mode`
  (at startup) and `Shift+Tab` (at runtime) work; `/config` renders the
  recovered key/value table but has no editor. A later cycle.
* **The mode is not persisted.** The 1.3.3 changelog mentions an `Agent Mode`
  option in the settings panel "so users can set and persist a default execution
  mode"; the clone writes nothing to `settings.json`'s `agentMode` key.
* **No live reference comparison.** The reference account's quota is exhausted,
  so the reference TUI could not be driven to capture its own Shift+Tab
  behaviour; the strings and the changelog are the evidence.
* **Windows is out of scope**, matching the changelog ("Auto mode is available
  on Linux and macOS, with Windows support coming later").
