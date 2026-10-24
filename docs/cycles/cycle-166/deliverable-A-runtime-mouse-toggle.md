# Cycle 166 — runtime mouse-tracking toggle (so wheel scroll AND select-and-copy both work)

## Problem

Cycle 165 (`6e64dcc`) disabled `MouseModeCellMotion` outright so terminal
select-and-copy would work. The cost: mouse-wheel scroll inside the
alt-screen buffer stopped working, and long-output conversations were
un-traversable. The reference binary accepts the documented trade-off
"shift/alt+click Select and copy text" — most modern terminals
(xterm, iTerm, kitty) pass shift-modified clicks through even with
DECSET 1006 enabled. tmux and a handful of embedded terminals do not.

## Crush's approach (the hint)

`crush/internal/ui/model/ui.go:3757-3773` — `mouseMode(enabled, wantsMotion)`
returns one of three modes based on a per-session flag and the
current view's hover needs:

```go
func mouseMode(enabled, wantsMotion bool) tea.MouseMode {
    switch {
    case !enabled:
        return tea.MouseModeNone
    case wantsMotion:
        return tea.MouseModeAllMotion
    default:
        return tea.MouseModeCellMotion
    }
}
```

The `enabled` flag is `Options.TUI.Mouse` (a config-file bool), and
the user can flip it at runtime via `dialog.ActionToggleMouseSupport`
which writes `options.tui.mouse` back to the global config.

## Our take

Crush's full config-persistence path is heavier than the clone needs.
We borrow the *idea* — a runtime-togglable bool + a flag-set seed —
but keep it session-only (the clone has no settings.json for this)
and add a slash command so the user can flip it without restarting:

1. **Flag seed**: `--mouse` (default true, matches reference) +
   `--no-mouse` (overrides). Same pattern as the existing
   `--alt-screen` / `--no-alt-screen` pair.
2. **Slash command**: `/mouse` (toggle), `/mouse on`, `/mouse off`.
   Session-only — survives restarts via the command-line flag.
3. **View() logic**: `v.MouseMode = MouseModeCellMotion` only when
   `(altScreen && mouseEnabled)`. When alt-screen is off (Ctrl+L),
   mouse tracking is off regardless — bubbletea handles scrollback
   differently in inline mode.

## Files

- `internal/cli/parser_specs.go` — adds `--mouse` (def `true`) and
  `--no-mouse` (def `false`). Both hidden, openagy-only.
- `cmd/wagyu/tui_v135.go` — adds `mouseEnabled` field on
  `v135RunMode`, threads through `NewV135RunMode`, and updates
  View() to set `v.MouseMode` only when both flags are true.
- `cmd/wagyu/slash_commands.go` — new `case "mouse":` block with
  the three forms.
- `cmd/wagyu/tui_v135_test.go` — `TestV135_MouseMode` covers all
  four transitions (default → Ctrl+L → Ctrl+L → /mouse off → /mouse).

## Verification (script(1) raw capture, `/tmp/our_script2.bin`)

| Sequence | Present? |
|---|---|
| `[?1002h` (button-event tracking) | ✅ enabled at startup |
| `[?1006h` (SGR mouse mode)        | ✅ enabled at startup |
| `[?1002l[?1003l[?1006l` (clean up)| ✅ on exit |

`tmux capture-pane` does NOT show those DECSET sequences because
tmux intercepts the modes itself and tracks them in its own pane
state, but script(1) catches the raw bytes. Wheel events now
arrive in `Update()` as `tea.MouseWheelMsg`, and the cycle-119
viewport code path (`isScrollWheel` + `ViewportMouseWheel`)
scrolls the transcript.

For users in tmux with a terminal that does NOT pass
shift/click through (rare, but real):

```
/mouse off   # → MouseModeNone; terminal-native selection works
/mouse on    # → MouseModeCellMotion; wheel scroll works
```

## Tests

`go test ./cmd/wagyu -run TestV135_MouseMode` — green. The full
test suite is otherwise unchanged (cycle 165's only diff was the
mouse-mode assignment; cycle 166's diff is a small extension that
brings the tests along).

The pre-existing `TestChangelog_MatchesReferenceBinary` drift
(3232-byte byte mismatch) is unrelated — same failure on `main`
without my changes.

## Reference cross-check

Reference binary at `~/.local/bin/agy` (v1.3.6) emits `[?1002h[?1006h`
on startup, identical to ours — confirms the documented default
behaviour. The reference has no `/mouse` escape hatch; users with
broken shift/click must use a different terminal. We surface that
escape hatch because the clone is built on macOS+iTerm+tmux and
the failure mode is real here even though it's not in the
reference's design matrix.