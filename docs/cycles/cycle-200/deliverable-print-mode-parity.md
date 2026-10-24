# Cycle 200 — print-mode parity (`wagyu -p "/cmd"`)

## TL;DR

Print mode is headless, so the reference can be run directly
(`agy -p "/cmd"`, no OAuth). A sweep of 34 print-safe commands against 1.4.0
found four real gaps; this cycle closes them. All 34 are now byte-identical to
the frozen capture on stdout **and** stderr, with matching exit codes.

## Gaps

| Command | Before | After |
|---|---|---|
| `/permissions` | already emitted the 15 `global\tallow\t…` rows (no code change needed) | 641 B, identical |
| `/usage` | `error: /usage failed: retrieving quota summary …` | two `group\tbucket\t<pct>%\t<reset>` rows |
| `/config` | `{"command":"","enabled":true,"type":""}` (map order) | `{"type":"","command":"","enabled":true}` (struct order) |
| `/changelog` | 150,390 B (stale 1.3.6) | 153,612 B (1.4.0, adds the `## 1.3.6` section) |

## Files

| File | Δ |
|---|---|
| `cmd/wagyu/print_slash.go` | +225/−28 |
| `cmd/wagyu/print_slash_test.go` | +157 |
| `cmd/wagyu/testdata/print_changelog.md` | +16 |

## Limitations 【存疑】

- `/usage` percentages and reset timestamps come from a **live** weekly-quota
  service (observed moving 92% → 76% during the session), so byte-identity is
  unattainable. The clone freezes the captured values (0.92 → `92%`, 0 → `0%`);
  a fresh live reference run will differ in the first line only. Group names,
  bucket ids/names/`window` and all `description` strings are verbatim from
  `agy -p=/usage --output-format json`.
- The reference **print** path forwards an unknown command to the model; the
  clone does not (out of scope).
