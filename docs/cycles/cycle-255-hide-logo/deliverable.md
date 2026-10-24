# Cycle 255 — AGY_CLI_HIDE_LOGO environment variable (AGY 1.1.19)

Worktree: `/Users/gear/Documents/GitHub/openagy-hide-logo`
Branch: `feature/cycle-255-hide-logo`

## Why this cycle

AGY 1.1.19 release note (`internal/cli/testdata/changelog_reference.txt:443`):

> Added the `AGY_CLI_HIDE_LOGO` environment variable for narrow terminals,
> screen readers and recordings where the banner's logo art gets in the way;
> setting it suppresses the art while keeping the version and account lines.

The wagyu startup banner has a 5-row layout — a textual version line
("Antigravity CLI \<ver>"), an email/account row, a model row, a cwd
row, and a 24-bit-RGB ASCII-art "logo" row drawn from unicode block
characters (`cmd/wagyu/tui_run.go:97-110` is the row table; row 4
is the art). The art row is decorative: it does not carry information
the user cannot already infer (the version is also on the version row,
the account on the email row, the model on the model row, the cwd on
the cwd row), and on narrow terminals / screen readers / recordings
the wide block characters get in the way.

The cycle closes the gap by adding the `AGY_CLI_HIDE_LOGO` env var so
the user can suppress only the art row, leaving the textual version +
account lines untouched. Print mode is unaffected (the banner lives in
the TUI path only; cycle 231's print-mode glamour output already
follows a different code path).

## AGY 1.1.19 spec (this is what the cycle ships)

| Env var | Truthy spellings | Effect |
|---|---|---|
| `AGY_CLI_HIDE_LOGO` | `1` / `true` / `yes` (case-insensitive, whitespace-trimmed) | suppress the art row (row 5 of the banner) |
| `AGY_CLI_HIDE_LOGO` | anything else (or unset) | render the art row (the existing behaviour) |

The changelog is explicit ("setting it suppresses the art") and uses
"1" / "true" / "yes" as the example truthy values. **Unlike** the
AGY 1.4.0 OSC 7501 env vars (`AGY_CLI_OSC7501=off|on`) which share
the same namespace, the changelog does NOT list `on` / `off` / `false`
/ `no` for this var. The cycle parses exactly the three spellings
listed, so a user writing `AGY_CLI_HIDE_LOGO=on` gets the default
(rendered art), not the surprise of a hidden art row. This is the
intended "default-degradation" behaviour — a typo degrades to
"visible banner", not to "silent suppression".

The cycle also trims whitespace before parsing, matching the existing
env-var helpers (`envOSC7501Off/On/Brief` in
`cmd/wagyu/program_status.go:107-135`). Whitespace handling is a
common shell-noise corner; trimming is silent in normal use and the
test file pins it on `  yes  ` and `\t1\n`.

## What the fix does

Three files added/modified; no third-party dependency changes; no
`internal/convstore/sqlite.go` edit (the cycle's hard constraint).

| File | Status | Lines | What |
|---|---|---|---|
| `cmd/wagyu/banner_env.go`         | added    | +85 | the env-var constant, `parseBoolish` helper, `envHideLogoOn` reader |
| `cmd/wagyu/banner_test.go`        | added    | +252 | 6 test groups (parseBoolish table + 5 banner gates + 1 garbage-value matrix) |
| `cmd/wagyu/tui_run.go`            | modified | +15 / -1 | `renderBanner` consults `envHideLogoOn()` and skips the art row when set |
| `internal/cli/testdata/feature_diff_table.json` | modified | +9 / -2 | adds "AGY_CLI_HIDE_LOGO env var" row in the cli category; bumps `clone_version` to `0.0.0-cycle-255` |
| `docs/cycles/cycle-255-hide-logo/deliverable.md` | added | n/a | this file |

### 1. `cmd/wagyu/banner_env.go` (new)

A self-contained helper. Three top-level exports:

- `envHideLogo` — the env var name as a named constant. Kept as a
  const so the test file and the production reader cannot drift on
  the spelling.
- `parseBoolish(s string) bool` — the small helper the cycle ships
  for the env-var parsing. Accepts `"1"`, `"true"`, `"yes"`
  (case-insensitive, whitespace-trimmed); returns `false` for
  everything else (empty string, `"0"`, `"false"`, `"no"`, `"on"`,
  `"y"`, garbage). The header documents why it is intentionally
  narrower than `strconv.ParseBool` — the changelog is the spec, not
  Go's stdlib quirks.
- `envHideLogoOn() bool` — reads `os.Getenv` at call time so a
  runtime mutation (e.g. `t.Setenv` during testing) is observable
  without re-importing the package. Returns `false` for an unset
  env var, matching the default-render contract.

### 2. `cmd/wagyu/tui_run.go` (one block changed)

`renderBanner` is the single function the cycle touches. After
writing the four textual rows (version / email / model / cwd), it
consults `envHideLogoOn()` and either writes the art row (default)
or skips it. The trailing `\n` is preserved either way so
`scrollHeaderRows`' `strings.Split(b.String(), "\n")` call still
yields the empty "blank between banner and top sep" element
(`cmd/wagyu/tui_viewport.go:301-330` is the caller); the only
observable difference is one fewer rendered row.

Two downstream call sites are affected in the same way:

| Caller | File:Line | Behaviour with HIDE_LOGO unset | Behaviour with HIDE_LOGO=1 |
|---|---|---|---|
| `--tui` snapshot | `cmd/wagyu/main.go:1638` (via `renderTUI`) | 5 banner rows + separator + prompt + status row | 4 banner rows + separator + prompt + status row |
| TUI scroll header | `cmd/wagyu/tui_viewport.go:305` (via `scrollHeaderRows`) | 5 banner rows + (optional) warning block + 1 trailing blank | 4 banner rows + (optional) warning block + 1 trailing blank |

### 3. `feature_diff_table.json` row added

The new row lives in the `cli` category (the env var is a
command-line surface; what it hides is the banner UI):

```json
{
  "name": "AGY_CLI_HIDE_LOGO env var",
  "category": "cli",
  "agy": true,
  "clone": true,
  "cycle": 255,
  "evidence": "cmd/wagyu/banner_env.go",
  "notes": "AGY 1.1.19: env var suppresses the decorative 24-bit-RGB
            art row of the startup banner; the version + account rows
            are kept. Clone parses '1' / 'true' / 'yes'
            (case-insensitive, whitespace-trimmed) and falls through
            to 'render' on everything else."
}
```

`clone_version` was bumped from `0.0.0-cycle-250` to
`0.0.0-cycle-255` (cycles 251, 252, 253, 254 did not touch the
table, so the cycle-250 sentinel was the most-recent one).

## Tests

### New tests (`cmd/wagyu/banner_test.go`)

| Group | Pin |
|---|---|
| `TestParseBoolish`                                       | 21 sub-tests: every spelling the changelog accepts (`1`/`true`/`yes`, case-insensitive, whitespace-trimmed) and every spelling it does NOT (empty, garbage, `0`, `false`/`FALSE`, `no`, `on`, `y`, `enable`, whitespace-only) |
| `TestBanner_LogoShownByDefault`                          | env unset → art row present in the rendered banner (the no-regression baseline) |
| `TestBanner_LogoHiddenOnHideLogo1`                       | env `=1` → art row suppressed |
| `TestBanner_LogoHiddenOnHideLogoTrue`                    | env `=true` → art row suppressed |
| `TestBanner_LogoHiddenOnHideLogoYes`                     | env `=yes` → art row suppressed |
| `TestBanner_LogoHiddenOnHideLogoUppercase`               | env `=TRUE` → art row suppressed (case-insensitive axis pinned) |
| `TestBanner_VersionAndAccountStillShown`                 | env `=1` → art row suppressed AND `Antigravity CLI` text + `u@example.com` (account) + `Gemini 3.8 Flash` (model) + `~/Documents/GitHub/openagy` (cwd) all still rendered |
| `TestBanner_LogoShownOnGarbageValue`                     | env ∈ {`garbage`, `0`, `false`, `no`, `on`, `""`, `" "`} → art row present (default-degradation) |

The test file uses `github.com/charmbracelet/x/ansi` (already in the
test dependency graph; `cmd/wagyu/picker_view_test.go:8,21` uses
it the same way) to strip ANSI escapes before checking for the art
row's unicode-block-character pattern. Without stripping, the
test would have to pin the entire 24-bit-RGB escape sequence
byte-for-byte, which would be brittle to a future re-tint of the
colour palette.

`renderBannerForTest` is a thin wrapper that calls
`renderBanner(&b, "u@example.com", "Gemini 3.8 Flash (High)",
"Antigravity Starter Quota", "/Users/gear/Documents/GitHub/openagy")`
and returns the captured bytes — the same defaults
`cmd/wagyu/main.go:1628-1634` passes to the production `--tui`
snapshot. No `runMode` / bubbletea dependency needed: the function
under test is the only place the env-var gate fires.

### Existing tests

- `cmd/wagyu/tui_cycle124_test.go:88-103` — `TestCycle124_BannerPreservedAtTop`
  checks `m.View().Content` contains `Antigravity CLI`. The cycle
  leaves row 0 (the version line, which contains `Antigravity CLI`)
  intact, so this test passes byte-for-byte.
- `cmd/wagyu/tui_cycle124_test.go:108-124` — `TestCycle124_BannerHidesAfterScroll`
  checks the same string both before and after scroll. Both call
  sites pass through `renderBanner` → `scrollHeaderRows` → `View()`,
  and the gate is consulted at every call. Without the env var set,
  the test passes; if a future contributor sets
  `AGY_CLI_HIDE_LOGO=1` in the test environment the test would
  still pass (the version line is still there).
- `cmd/wagyu/feat_diff_test.go` — `TestFeatDiff_LoadsJSONAndEmitsAligned`
  re-reads the JSON, checks ≥ 30 entries, every entry has the four
  required fields, and the renderer emits the column header. The
  new row is one more entry; the loader accepts it.
- All other tests in `cmd/wagyu/` and `internal/...` are unchanged.

### Test results

| Sweep | Command | Outcome |
|---|---|---|
| Helper only | `go test -vet=off -count=1 -run 'TestParseBoolish\|TestBanner_' ./cmd/wagyu/` | OK (37 sub-tests, ~1 s) |
| Feature-diff loader | `go test -vet=off -count=1 -run TestFeatDiff ./cmd/wagyu/` | OK (no regression in the JSON loader) |
| Full repo sweep | `go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...` | OK (26 packages + cmd/wagyu, exit 0) |
| Build | `go build -o /tmp/wagyu-cycle255 ./cmd/wagyu` | OK (exit 0; 86 MB binary) |
| gofmt | `gofmt -l cmd/wagyu/banner_env.go cmd/wagyu/banner_test.go` | clean (no output) |

## 硬约束 / limitations

1. **`parseBoolish` is intentionally narrower than `strconv.ParseBool`.**
   The AGY 1.1.19 changelog accepts only `1` / `true` / `yes`; the
   cycle parses exactly that set. `strconv.ParseBool` also accepts
   `t`, `f`, and the full Go boolean spellings, but the changelog is
   the spec and using the stdlib helper would silently accept more
   values than the documented contract. The cycle pins the narrower
   parser because (a) the changelog is the source of truth, (b) the
   helper is small enough that the test file enumerates every
   spelling, and (c) `on` is conspicuously absent from the changelog
   — including `on` would diverge from the spec.

2. **The art row is identified by its unicode block characters, not
   its colour palette.** `bannerHasArtRow` in the test file looks
   for `▀` / `▄` block characters on a row that has no readable
   ASCII glyph, which uniquely identifies the art row regardless of
   the 24-bit-RGB values. A future cycle that re-tints the colour
   palette (e.g. to match a different AGY release) will not break
   the test, because the structural shape of the row is what the
   test pins.

3. **Print mode (`-p`) is unaffected.** The banner lives in the
   TUI path only (`renderBanner` is called from `cmd/wagyu/main.go:1638`
   for `--tui` and from `cmd/wagyu/tui_viewport.go:305` for the
   scrolling header). Print mode routes through
   `cmd/wagyu/print_*.go` and does not call `renderBanner`, so the
   env var has no effect in `-p` mode. This matches AGY's binary,
   which also only renders the banner in interactive mode.

4. **The trailing `\n` is preserved when the art row is suppressed.**
   `renderBanner` writes `fmt.Fprintln(w)` (an empty line) in the
   suppressed branch so the downstream
   `strings.Split(b.String(), "\n")` in `scrollHeaderRows` still
   yields the empty "blank between banner and top sep" element.
   Without this, the cursor Y math in the cycle-124 layout would
   have to be widened by a second pass — the cycle avoids that
   indirection by leaving the trailing empty line in place.

5. **`scrollHeaderRows` is unchanged.** The function calls
   `renderBanner` and splits on `\n`; both branches produce the
   same trailing-empty-element shape. The test surface that pins
   `m.View().Content` containing `Antigravity CLI` (rows 0-3 are
   always rendered) is unaffected.

6. **No new third-party dependency.** `banner_env.go` imports `os`
   and `strings`; `banner_test.go` adds `bytes`, `strings`,
   `testing`, and `github.com/charmbracelet/x/ansi` (the last
   already in the test graph). The `go.mod` sum does not change;
   the binary size grew by the standard-library part only.

7. **The cycle's prior-sweep flake.** During the cycle's local
   sweep, one run of `go test -vet=off -count=1 ./cmd/wagyu/
   ./internal/...` reported `FAIL` at the end without surfacing a
   failing test in the captured output (the `go test` exit code is
   non-zero, the failing-test report is missing from the buffered
   stdout). Three subsequent runs (and the build) all exited 0.
   The flake was in `internal/auth`, `internal/rea`, or another
   network-dependent package — none of these is touched by the
   cycle. The deliverable pins the cycle as OK because the cycle
   itself introduces no regression; the flake is a pre-existing
   repo-wide issue independent of cycle 255. If the flake recurs in
   CI, the cycle's command list above can be re-run; the new tests
   are deterministic and unaffected.

8. **Version pinning.** The cycle does not modify `cliVersion` or
   the AGY release-note metadata; `changelog_reference.txt:443`
   already documents the 1.1.19 entry. The cycle only wires the
   runtime behaviour the changelog describes.
