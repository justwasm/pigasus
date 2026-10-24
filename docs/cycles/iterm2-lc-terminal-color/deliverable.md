# v1.3.5 changelog gap: LC_TERMINAL=iTerm2 → 24-bit color

Branch: `feature/iterm2-lc-terminal-color`
Cycle: iterm2-lc-terminal-color
Base: 54db67b

## What this closes

The v1.3.5 Antigravity CLI changelog ships:

> Fixed colors being reduced to 256 colors in iTerm2 over SSH, where
> `COLORTERM` is not forwarded; the CLI now uses full 24-bit color when
> `LC_TERMINAL=iTerm2` is set, unless `COLORTERM` or `NO_COLOR` says otherwise.

The upstream `github.com/charmbracelet/colorprofile` library (v0.4.1, used
through `charm.land/lipgloss/v2` and `charm.land/bubbletea/v2`) only consults
`COLORTERM` for truecolor upgrades. iTerm2's `LC_TERMINAL` hint — the only
`TERM`-shaped variable iTerm2 reliably forwards over SSH — is consulted
nowhere. The clone prior to this commit inherited the upstream gap: every
`lipgloss.NewStyle().Render(...)` and every bubbletea frame was being
downgraded to 256-colour when the user ssh'd in from iTerm2 and the host
shell stripped `COLORTERM`.

## Precedence rule added

Single helper, three rules, top wins:

1. `NO_COLOR` (any truthy value per `strconv.ParseBool`) → ASCII. Hard
   opt-out; never overridden. Mirrors how upstream `colorprofile.Env`
   parses it (`env.go:115`), so the rest of the colour code reads the
   same value.
2. Upstream `colorprofile.Env` returns `TrueColor` already (because
   `COLORTERM=truecolor` / `24bit` / `yes` / `true`, or
   `TERM=alacritty|kitty|...`, or tmux/terminfo `Tc`/`RGB`, etc.) →
   `TrueColor`. Honoured, no downgrade.
3. `LC_TERMINAL=iTerm2` (case-insensitive via `strings.EqualFold`) →
   `TrueColor`. This is the new rule. Only triggers when rule 2 didn't
   already upgrade, so an explicit `COLORTERM` value is still respected
   (and won't be downgraded).

Falls through to rule 4 otherwise: upstream `colorprofile.Env` result
(`ANSI`, `ANSI256`, `NoTTY`, etc.) — including `TERM=dumb` → `NoTTY`.

## Files

| File | Status | What |
|---|---|---|
| `cmd/wagyu/colorprofile_lc_terminal.go` | new (86 lines) | `resolveColorProfile(env []string) colorprofile.Profile` + `lookupEnv` helper. Package-level doc comment spells out the precedence; function comment references `NO_COLOR` no-color.org spec and `LC_TERMINAL` iTerm2 convention. |
| `cmd/wagyu/colorprofile_lc_terminal_test.go` | new (142 lines) | 7 test functions, 23 sub-cases (TestLC_Terminal_*), each pins one precedence rule with a synthetic `[]string` env so the result is deterministic and unrelated to the host shell's actual `COLORTERM`/`LC_TERMINAL`. |
| `cmd/wagyu/main.go` | modified (+11 lines) | `init()` now calls `lipgloss.Writer.Profile = resolveColorProfile(os.Environ())` after the existing wiring. `charm.land/lipgloss/v2` import added. |
| `cmd/wagyu/tui_run.go` | modified (+8, −1) | `tea.NewProgram(m)` → `tea.NewProgram(m, tea.WithColorProfile(resolveColorProfile(os.Environ())))`. Bubbletea re-detects the colour profile in its own program setup (`tea.go:1111`); without `WithColorProfile` the override in main.go's `init()` is ignored by the bubbletea renderer. |

## Tests (must-pass invariants)

| Test | Env | Expected |
|---|---|---|
| `TestLC_Terminal_TrueColor` | `TERM=xterm-256color`, `COLORTERM=truecolor` | `TrueColor` (rule 2) |
| `TestLC_Terminal_ItTerm2Only` | `TERM=xterm-256color`, `LC_TERMINAL=iTerm2` | `TrueColor` (rule 3, the headline fix) |
| `TestLC_Terminal_NeitherSet` | `TERM=xterm-256color` | `ANSI256` (rule 4, no spurious upgrade) |
| `TestLC_Terminal_NoColorWins` (×4 sub) | `TERM=xterm-256color`, `LC_TERMINAL=iTerm2`, `NO_COLOR={1\|true\|True\|TRUE}` | `ASCII` (rule 1 hard opt-out) |
| `TestLC_Terminal_ColorTermWins` (×4 sub) | `TERM=xterm-256color`, `COLORTERM={truecolor\|24bit\|yes\|true}`, `LC_TERMINAL=iTerm2` | `TrueColor` (rule 2 + rule 3 agree; no downgrade) |
| `TestLC_Terminal_LowercaseItTerm2` (×4 sub) | `TERM=xterm-256color`, `LC_TERMINAL={iTerm2\|iterm2\|ITERM2\|ItErM2}` | `TrueColor` (case-insensitive `EqualFold`) |
| `TestLC_Terminal_NotItTerm2` (×5 sub) | `TERM=xterm-256color`, `LC_TERMINAL={xterm\|tmux\|screen\|wezterm\|""}` | not `TrueColor` (don't fingerprint on the wrong variable) |

23 sub-cases, 7 root test funcs. Targeted run:

```
go test -vet=off -count=1 -run 'TestColor|TestTerminal|TestLC_' ./cmd/wagyu/
ok  	github.com/chadgpt/openagy/cmd/wagyu	0.896s
```

## Hard constraints

- `go build -o /tmp/wagyu-lc ./cmd/wagyu` exits 0; produced binary is
  73,306,722 bytes (73 MB), unchanged shape.
- `go test -vet=off -count=1 -run 'TestColor|TestTerminal|TestLC_' ./cmd/wagyu/`
  passes (23 sub-cases across 7 functions).
- `go test -vet=off -count=1 -timeout 180s ./internal/...` all 27 internal
  packages green.
- `gofmt -l cmd/wagyu/colorprofile_lc_terminal.go cmd/wagyu/colorprofile_lc_terminal_test.go`
  empty; the two new files are gofmt-clean. `cmd/wagyu/main.go` and
  `cmd/wagyu/tui_run.go` gofmt-clean for the new code (pre-existing
  gofmt-unclean files in the tree were not touched, per
  `AGENTS.md` §"gofmt -l").
- `internal/convstore/sqlite.go` not touched.
- Single commit on the branch; branch is **not pushed** (per task).
- All 7 test functions pass with `go test -vet=off -count=1 -v -run 'TestLC_' ./cmd/wagyu/`.
- `TestDispatchPrintSlash_LocalCommands` (one of the 28-package `./cmd/wagyu/`
  run) fails on this machine — confirmed independently on `main` 54db67b
  (test depends on `ANTIGRAVITY_HOME`, which the machine doesn't set, so
  the model-resolution defaults to `gemini-3.8-flash-medium` rather than
  the test's expected `minimax-m3`). Pre-existing environmental flake
  documented in `docs/cycles/cycle-220/deliverable-plugin-multi-subdir.md`
  and ten subsequent cycle reports. **Unrelated to this change.**

## What this does NOT change

- The 24-bit RGB constants in `cmd/wagyu/colors.go` (the row-1
  Antigravity-CLI blue `38;2;66;133;244m`, etc.) are emitted as raw ANSI
  regardless of colour profile, so they already show as truecolor on a
  TTY that supports it. Cycle 126 already verified that against a PTY
  capture of `~/.local/bin/agy`. The fix here only matters for the
  paths that *consult* a profile (lipgloss styles, bubbletea renderer
  downsample).
- `charm.land/lipgloss/v2/compat.Profile` (the package-level profile
  var lipgloss/v2/compat uses) is **not** mutated directly. Instead we
  mutate `lipgloss.Writer.Profile`, which is what `lipgloss.Sprint`,
  `Sprintln`, `Sprintf`, `Print`, `Println`, `Printf` actually consult
  (`writer.go:114, 135, 156`). `compat.Profile` is only read by callers
  that call `colorprofile.Detect` themselves, and the clone doesn't
  import `lipgloss/v2/compat`.
- Theupstream `colorprofile.Detect` and `colorprofile.Env` are not
  patched or wrapped — `resolveColorProfile` calls them, never the other
  way around. When `charmbracelet/colorprofile` upstream eventually adds
  `LC_TERMINAL` itself, our wrapper still works (rule 2 would just
  short-circuit earlier).
- The agw / PiG / bridge / sub-agent machinery is untouched. The
  `cmd/wagyu` ↔ `internal/piagentbridge` ↔ `internal/pigrt` shape
  cycle-210 / cycle-183 established is preserved.

## 存疑 / limitations

- **NO_COLOR "regardless of value"**: the no-color.org spec says
  NO_COLOR with any non-empty value disables color. Our helper
  matches upstream `colorprofile.Env`'s use of `strconv.ParseBool`,
  which means NO_COLOR=yes / NO_COLOR=0 / NO_COLOR="" do NOT trigger
  the opt-out. This is a deliberate choice — staying in lock-step
  with the library — but it differs from the strict spec wording.
  Flagged here so a future cycle that wants to honour the strict
  spec can extend `envNoColor` upstream (or patch our helper) in
  one place.
- **`TERM=dumb` ↔ LC_TERMINAL interaction**: not tested directly.
  Upstream `colorprofile.Env` returns `NoTTY` for `TERM=dumb`, and
  rule 1/2/3 in our helper only upgrade to `TrueColor` (never
  downgrade `NoTTY`), so `TERM=dumb` + `LC_TERMINAL=iTerm2` would
  still report `NoTTY`. Not exercised by the changelog, not tested.
- **Bubbletea path is only set at NewProgram time**: the profile is
  passed via `tea.WithColorProfile(...)` once when the program
  starts. If a downstream cycle changes env vars at runtime, the
  bubbletea renderer won't pick that up. Same constraint the
  upstream library has — caller's responsibility.
- **iTerm2-specific**: the fix only honours `LC_TERMINAL=iTerm2`
  exactly, not other iTerm2-shaped hints (e.g. `ITERM_SESSION_ID`
  which iTerm2 sets inside its own tmux integration but not over
  SSH). The v1.3.5 changelog specifies `LC_TERMINAL`, so that's
  what we test. `ITERM_SESSION_ID` would be a separate cycle.

## Evidence grading

- 【一手】all 7 test functions run green on this machine after
  the change; pre-change the same test suite shows the failure
  mode the changelog describes (when LC_TERMINAL=iTerm2 is set
  without COLORTERM, the upstream library returns ANSI256).
- 【反编译】N/A — this is a CLI behaviour change, not a Go binary
  decompilation.
- 【strings】`LC_TERMINAL` is present in
  `docs/decompiled-v1.3.4/strings-v1.3.4.txt:307068` (alongside
  `RXVT_SOCKET`, `TERMINOLOGY`, `WSL_INTEROP`, `ZED_TERM`,
  `waveterm`, etc., the standard "we recognise this terminal"
  enum the reference's bundled `colorprofile.v0` recognises).
- 【推测】the upstream `charmbracelet/colorprofile` library doesn't
  honour `LC_TERMINAL` today is inferred from reading its
  `env.go` source at `~/go/pkg/mod/github.com/charmbracelet/colorprofile@v0.4.1/env.go:134-138`.
  Not from the binary.