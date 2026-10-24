# Cycle 227 — flake fixes

## Why this cycle

After cycle 226 (the AGY 1.4.2 package-adoption wave that landed
charmland-component-adoption, doublestar-glob, vito-canvas,
plugin-fsnotify, package-simplification, golden-snapshots and
osc-status-and-hyperlinks), only one of the original "7 pre-existing
flakes" still failed: `TestDispatchPrintSlash_LocalCommands`. The other
six (`TestCycle225QuotaCountdown`, `TestWrapRowsQuadratic`,
`TestCycle225QuotaStubRefills`, `TestScrollCacheHit`,
`TestRunStatusLineScript_SpawnEcho`, `TestChangelog_MatchesReferenceBinary`)
either no longer reproduce or were drift artefacts that the cycles
silently resolved. `TestStallWatcher_TimerResetsOnEachEvent` was also
flagged by the OSC subagent as a parallel-suite-load timing flake — not
in scope, not addressed.

## Root cause of `TestDispatchPrintSlash_LocalCommands`

Two compounding bugs surfaced in this single test:

### Bug 1 — test environment leak

The user's real `~/.gemini/antigravity-cli/settings.json` contains:

```json
{
  "model": "gemini-3.8-flash-low"
}
```

`auth.ResolveSettingsModel("")` reads this file via
`AntigravityConfigHome()` and returns `"gemini-3.8-flash-low"`. That
string is then looked up in `cli.ReferenceModels()` (slug match), finds
the Low entry, and is returned unchanged. So the test, which had no
`--model` flag set, dutifully reported the developer's local preference
instead of the *built-in* default.

Cycle 119 already established the convention: tests that touch
`auth.ResolveSettingsModel` must point `ANTIGRAVITY_HOME` at a temp
directory. The fix is one line of `t.Setenv("ANTIGRAVITY_HOME", t.TempDir())`
at the top of the test, mirroring `newSlashModel`'s existing
isolation in `slash_content_test.go`.

### Bug 2 — inconsistent default-model constant

Two files independently hardcoded the "no-config" fallback:

| File | Line | Value |
|---|---|---|
| `cmd/wagyu/main.go` | 692 | `"Gemini 3.8 Flash (Medium)"` (print-mode fallback) |
| `cmd/wagyu/model_resolve.go` | 57 | `"Gemini 3.8 Flash (High)"` (TUI + shared `defaultModel` const) |

These two have to agree: a clean install should pick the same model
whether the user is in `-p` print mode or in the interactive TUI.
`main.go`'s `(Medium)` is the authoritative value — it is the *only*
fallback the print path can return, so it is the one byte-level
invariant that AGY clones. Changing the const from `(High)` to
`(Medium)` aligns TUI with print.

A second stale test (`TestSlashContent_ModelAndEffort` in
`slash_content_test.go:92`) was pinning the *old* `(High)` expectation
via the TUI path. Updated to `(Medium)`.

## Hard constraint / limitations

- The fix does **not** change AGY's user-configurable behaviour: any
  developer who has `"model": "gemini-3.8-flash-low"` in their real
  `settings.json` still gets `-low` (the production code never
  overrides user config). The change only affects what a *clean*
  install with no settings.json shows.

- `TestStallWatcher_TimerResetsOnEachEvent` is left alone. The OSC
  subagent's deliverable (`docs/cycles/osc-status-and-hyperlinks/deliverable.md`)
  classifies it as a parallel-suite-load timing flake that passes 5/5
  in isolation. It is unrelated to the default-model bug, and fixing it
  requires either a clock-injection refactor or a longer timer margin
  in `internal/piagentbridge`. Out of scope here.

## Files changed

| File | Change |
|---|---|
| `cmd/wagyu/print_slash_test.go` | add `t.Setenv("ANTIGRAVITY_HOME", t.TempDir())` in `TestDispatchPrintSlash_LocalCommands` |
| `cmd/wagyu/model_resolve.go` | `defaultModel = "Gemini 3.8 Flash (Medium)"` (was `(High)`); expanded comment to record the alignment with `main.go:692` |
| `cmd/wagyu/slash_content_test.go` | update `TestSlashContent_ModelAndEffort` to expect `(Medium)` |

## Test results

```
$ go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...
56 packages OK, 0 FAIL
```

No new tests added. Two tests were made to agree with the production
default (one was isolating the test env, the other was pinning a stale
expectation). All cycle-specific tests added by the prior wave still
pass:

- `TestLine_RenderNoHighlight`, `TestLine_EmptyContent`, `TestLine_SingleHighlight`,
  `TestLine_MultipleHighlights`, `TestLine_CJKHighlight`, `TestLine_CJKMidHighlight`,
  `TestLine_OverlappingHighlights`, `TestLine_OutOfRangeHighlight`,
  `TestLine_ZeroWidthRange`, `TestLine_TrailingResetAlwaysEmitted`,
  `TestLine_DefaultStyleIsBoldReverse` (vito-canvas)
- `TestFallbackGlobDoubleStar`, `TestFallbackGlobBraceExpansion`,
  `TestFallbackGlobExcludeDoublestar`, `TestFindByNameDoublestar`,
  `TestGrepSearchIncludeDoublestar` (doublestar-glob)
- `TestCodeSearch_PCREFeatures` (5 subtests), `TestCodeSearch_NonASCIIColumns` (pkg-simplification)
- `TestCopy_OSC52Preferred_WhenSupported` (pkg-simplification)
- `TestReadFile_WebM_Valid`, `TestReadFile_NonWebMExtension_WebMMagic_NoRejection`,
  `TestScale_PNG_SmallUnchanged` (golden-snapshots)
- `TestProgramStatus*` (6 funcs, 30 sub-tests) (osc-status-and-hyperlinks)
- `TestHyperlink*` (5 funcs, 42 sub-tests) (osc-status-and-hyperlinks)
- `TestWatcher_*` (4 funcs, plugin-fsnotify)

## AGY alignment

The two bugs were *both* sources of drift from AGY 1.4.2:

1. AGY's print path always resolves the model to `(Medium)` when no
   config is set — production code now matches.
2. The test was failing because the developer's local `settings.json`
   leaked into the test process — fixing the isolation is a
   precondition for any future test that asserts "the default model is
   X" without first pinning the env.

## Open question

The `printMode` fallback (`main.go:692`) and `defaultModel` are now
both `(Medium)`. A future cleanup cycle could collapse the two into a
single exported `DefaultModel()` helper so the constant lives in
`internal/cli/` (where `ReferenceModels()` lives) rather than `cmd/wagyu/`.
Not done here — out of scope, and the current fix already achieves
byte-level parity with AGY 1.4.2 on a clean install.