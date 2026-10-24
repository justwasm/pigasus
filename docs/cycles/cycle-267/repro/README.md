# Cycle 267 — reproduction harness

The cycle 267 bug is purely visual: bubbletea v2 + ultraviolet's
diff-based terminal renderer leaves stale pixels at rows where the
streaming chunks wrote content but the post-glamour-replace
`View()` output has no touched cells. Headless `Update()` calls
can't reproduce it — the bug is in the render path, not the model
state — so the only authoritative reproducer is a real PTY capture.

## Files

| Path | Role |
|---|---|
| `wg01_pty_check.py` | Python PTY harness — opens a real pty.fork(), drives the binary, captures raw escape stream, asserts 5 signals |
| `main.go` | Go wrapper that runs the harness with the right paths. Three modes: repro1 (post-fix), repro2 (pre-fix), repro3 (headless unit tests) |

## Usage

```bash
# Build the cycle 267 binary:
go build -o /tmp/wagyu-c267 ./cmd/wagyu

# Run the post-fix reproduction:
go run ./docs/cycles/cycle-267/repro -mode repro1 -bin /tmp/wagyu-c267
# PASS=5/5 clear-sig=True no-raw-leak=True full-resp=True no-spinner=True card2=True

# Run the pre-fix baseline (cycle 266) — 3/5 with card2=False clear-sig=False:
go run ./docs/cycles/cycle-267/repro -mode repro2 -bin /tmp/wagyu-final
# PASS=3/5 clear-sig=False no-raw-leak=True full-resp=True no-spinner=True card2=False

# Run only the headless unit tests (no PTY):
go run ./docs/cycles/cycle-267/repro -mode repro3
```

## Why `-harness` is sometimes needed

`go run` copies the source files to a temp dir before compilation,
so `go list -f '{{.Dir}}' .` from inside the resulting binary may
return a path that does NOT contain `wg01_pty_check.py`. Pass
`-harness /abs/path/to/wg01_pty_check.py` to override the
auto-detection:

```bash
go run ./docs/cycles/cycle-267/repro -mode repro1 \
    -bin /tmp/wagyu-c267 \
    -harness $(pwd)/docs/cycles/cycle-267/repro/wg01_pty_check.py
```

## What the harness checks

5 signals over the bytes the bubbletea renderer emits on the PTY
output fd, all relative to the `state=done:app=wagyu` OSC 7501
recordTurnOutcome publishes:

| # | Signal | Type |
|---|---|---|
| 1 | `\x1b[H\x1b[2J` after `state=done` (tea.ClearScreen trigger) | positive |
| 2 | raw chunk `mock reply f ` (NOT followed by `rom model=`) leaked past `state=done` | negative |
| 3 | response fragments `mock reply from model=` + `Gemini 3.8 Flash (Medium)` present | positive |
| 4 | spinner glyph `\xe2\xa0` followed by `Generating` past `state=done` | negative |
| 5 | Card 2 title `Daily quota resets` visible | positive |

Pre-fix (cycle 266): signals 2 and 5 fail. Card 1 (visible at
init) never transitions to Card 2, and the chunks accumulate at
row 9 with the `mock reply from model=...` string only partially
overlaid (the bytes past the chunk's column stay as Card 1's
title pixels).

Post-fix (cycle 267): all 5 signals pass. The
`tea.Batch(queueCmd, tea.ClearScreen)` returned from the
`case turnDoneMsg:` handler triggers
`s.scr.Erase() → clearUpdate() → full-screen repaint` on the next
flush, driven by the freshly returned `View()`. One ~16 ms
full-redraw per turn boundary, imperceptible at 60 fps.