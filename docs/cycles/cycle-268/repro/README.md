# Cycle 268 — reproduction harness

The cycle 268 bug is a glamour render-time cap: every
`mdrender.ANSI / Plain / User / Quiet / LineANSI /
LineANSIBounded` call site passed `width=0`, which glamour
interprets as "use the hard default of 80 cols". On a wide tmux
pane (250 cols) the user saw the rendered model replies wrap at 80
cols with the rest of the pane sitting empty. Headless `Update()`
calls can't reproduce the *visual* bug — but the *routing* bug
(width=0 always) is straightforward to pin with a unit test, and
the visual effect is straightforward to verify with a PTY capture.

## Files

| Path | Role |
|---|---|
| `wg01_pty_check.py` | Python PTY harness — runs the binary in 80-col and 250-col PTYs, strips ANSI / CSI / OSC escapes from the captured bytes, and compares the longest visible line in each run. Cycle 268 fix means the 250-col run produces a substantially longer longest line. |
| `main.go` | Go wrapper that runs the harness with the right paths. Three modes: repro1 (post-fix), repro2 (pre-fix), repro3 (headless unit tests). |

## Usage

```bash
# Build the cycle 268 binary:
go build -o /tmp/wagyu-c268 ./cmd/wagyu

# Run the post-fix reproduction:
go run ./docs/cycles/cycle-268/repro -mode repro1 -bin /tmp/wagyu-c268
# Expect exit 0 — the wide PTY renders visibly wider than the narrow PTY.

# Run the pre-fix baseline (cycle 267) — exit 1:
go run ./docs/cycles/cycle-268/repro -mode repro2 -bin /tmp/wagyu-final
# Expect exit 1 — both PTYs cap at ~80 cols (glamour's hard default).

# Run only the headless unit tests (no PTY):
go run ./docs/cycles/cycle-268/repro -mode repro3
```

## Why `-harness` is sometimes needed

`go run` copies the source files to a temp dir before compilation,
so `go list -f '{{.Dir}}' .` from inside the resulting binary may
return a path that does NOT contain `wg01_pty_check.py`. Pass
`-harness /abs/path/to/wg01_pty_check.py` to override the
auto-detection:

```bash
go run ./docs/cycles/cycle-268/repro -mode repro1 \
    -bin /tmp/wagyu-c268 \
    -harness $(pwd)/docs/cycles/cycle-268/repro/wg01_pty_check.py
```

## What the harness checks

Two PTY cases — `narrow` (80 cols, glamour's hard default cap) and
`wide` (250 cols, a real wide tmux pane) — both running
`mocksham` so the streaming reply is short and deterministic. For
each run the harness:

1. Opens a real `pty.fork()`.
2. Configures the WIN struct for `cols × 30 rows`.
3. Sets `OPENAGY_TRUST=always` to skip the on-startup workspace-trust
   dialog.
4. Waits ~2 s for the initial `View()` to settle.
5. Sends `analyze pwd\r` (CR so bubbletea v2's `tea.KeyPressMsg`
   recognises it as Enter per the cycle-253 chassis).
6. Captures ~6 s of bytes the bubbletea renderer emits on the PTY
   output fd.
7. Strips ANSI / CSI / OSC escapes and measures the longest visible
   line.

The cycle 268 contract is a single number: the `wide` PTY's longest
visible line must exceed the `narrow` PTY's by more than 20 cells.
Pre-cycle-268 both PTYs cap at ~80 cols and the diff is small;
post-cycle-268 the wide PTY renders banner / footer lines that
honour the full 250-col width and the diff is large.

## Limitations

The PTY harness uses `--backend=shim`, which truncates streaming
replies to 40 chars. This means the *rendered reply itself* never
exceeds ~40 cells regardless of terminal width — the harness
therefore measures the surrounding banner / footer / transcript
wrapper line widths, which DO scale with terminal width once the
transcript is small. For the precise width contract (e.g. "a render
at width=200 produces lines of exactly 200 cells"), see the
in-process test suite in `internal/mdrender/cycle268_width_test.go`
(13 test functions, including `TestGlamour_WidthPin_RealWidthNotCappedAt80`,
`TestGlamour_WidthPin_CacheKeyDifferentWidths`, and the
`TestGlamour_PrefixCache_WidthAffectsCacheKey` regression net).
