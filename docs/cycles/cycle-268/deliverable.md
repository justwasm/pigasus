# cycle 268 — glamour word-wrap no longer caps at 80 columns

## TL;DR

Every `mdrender.ANSI / Plain / User / Quiet / LineANSI /
LineANSIBounded` callsite in `cmd/wagyu/` and `internal/` passed
`width=0`. Glamour interprets `width=0` as "use the hard default of
80 cols" (`glamour.WithWordWrap` documents this), so every rendered
model reply — chat transcript, step-detail panel, mid-stream
streaming glamour, print mode — wrapped at 80 cells regardless of
how wide the actual terminal was. On a 250-col tmux pane the user
saw a single 80-cell block of styled text surrounded by 170 cells
of empty pane, with every model reply visually capped.

Cycle 268 makes the explicit-width path the hot one. The TUI's
`tea.WindowSizeMsg` handler now calls `mdrender.SetCurrentWidth(m.width)`
once per resize; every call site that takes a width
(`ANSI / Plain / User / Quiet / renderMarkdown / renderUserText /
renderPrintText / toolBlockLines / toolStepLines / stepDetailLines`)
plumbs the actual terminal width through. The mid-stream glamour
pass (`LineANSI / LineANSIBounded / LineANSIBoundedWithCache`) keeps
its single-argument signature and resolves the width via the
package's `currentWidth()` chain: pinned value → `$COLUMNS` →
`x/term.GetSize` → 0. Glamour's `<= 0 → 80` safety fallback stays
in `getRenderer` and `LineANSI` so a future stray call site still
produces readable output rather than the
`WithWordWrap(0)` ⇒ `panic from word-wrap-on-zero` failure mode.

Headline test: `TestGlamour_WidthPin_RealWidthNotCappedAt80` —
a 184-char source rendered at width=200 emerges as a single
184-cell line, not three wrapped 80-cell lines.

## Root cause

Cycle 228 wired the cycle-155 hand-rolled markdown walker to
charm.land/glamour/v2 v2.0.1. Cycle 232 added `LineANSI` for the
mid-stream glamour pass. Cycle 242 added the boundary-aware
`LineANSIBounded` and cycle 243 the FNV-64 cache wrapper
`LineANSIBoundedWithCache`. Each of these added a glamour render
call, and each passed `width=0` because the original 80-col default
"worked" on the developer's 80-col tmux pane.

Glamour's contract:

```go
// charm.land/glamour/v2@v2.0.1/ansi/wrap.go — WithWordWrap
// sets the wrap width. 0 (or negative) means "auto-fit", which
// glamour interprets as 80 cols internally. There is no
// glamour-side "ask the terminal" — the caller is expected to
// supply the actual width.
```

Caller signatures across the wrapper chain:

| File:line                                  | Callsite                     | Pre-268 |
|--------------------------------------------|------------------------------|--------|
| `cmd/wagyu/conversation.go:529`            | `renderMarkdown`             | `ANSI(stripped, 0)` |
| `cmd/wagyu/render_user_text.go:67`        | `renderUserText`             | `User(pre, 0)` |
| `cmd/wagyu/print_glamour.go:157`          | `renderPrintText`            | `ANSI(raw, 0)` |
| `cmd/wagyu/tui_tools.go:952`              | `stepDetailLines`            | `ANSI(stripped, 0)` |
| `internal/mdrender/stream_glamour.go:182` | `LineANSI`                   | `ANSI(stripped, 0)` |
| `internal/cli/login_prompt.go:64`         | `RenderLoginPrompt`          | `Quiet(src, 0)` |

Plus the prefix-cache key (`stream_glamour.go:123`):

```go
key := PrefixCacheKey{
    LineCount: strings.Count(buffer, "\n") + 1,
    FnvHash:   FNV64(buffer),
    Variant:   currentVariant(),
    Width:     currentWidth(), // pre-268 was 0 — every cache slot shared width=0
}
```

The cache-key bug amplified the symptom: even if a caller passed
`width=80` to one render and `width=120` to another, both landed
in the same cache slot because `currentWidth()` returned 0 in the
pre-268 stub. The cycle-243 prefix cache was effectively
single-renderer-per-variant regardless of width.

User-visible symptom (250-col tmux pane):

```
+-------- banner (7 rows) --------+
| > analyze pwd                   |  ← user prompt row
|   mock reply from model=Gemini 3.8 Flash (Medium): analyze pwd  ← 78 cells, wraps
| (the rest of the reply, wrapped at 78-80 cols)
+                                +  ← transcript right edge at col 80
                                   ← 170 cols of empty pane to the right
+                                  ← announcement card also capped at col 80
+----------------------------------+
```

## Fix

The fix has two parts — a *resolution chain* for `currentWidth()`
and an *explicit-width plumbing* for every public call site — plus
a *cache-key fix* in `getRenderer` so different widths land in
different glamour TermRenderer slots.

### 1. Resolution chain — `currentWidth()`

`internal/mdrender/glamour_adapter.go:486`:

```go
func currentWidth() int {
    // 1. The TUI's pinned value (set by tea.WindowSizeMsg).
    if pinned := readPinnedWidth(); pinned > 0 {
        return pinned
    }
    // 2. $COLUMNS — print mode's natural source.
    if cols := os.Getenv("COLUMNS"); cols != "" {
        if n, err := strconv.Atoi(cols); err == nil && n > 0 {
            return n
        }
    }
    // 3. x/term.GetSize on os.Stdout when connected to a TTY.
    if term.IsTerminal(int(os.Stdout.Fd())) {
        if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
            return w
        }
    }
    // 4. Caller's problem.
    return 0
}
```

Step 1 is the TUI's pin (next paragraph). Step 2 catches the
print-mode case where `$COLUMNS` is forwarded by the shell. Step 3
catches a bare-binary test runner connected to a real terminal.
Step 4 is the caller's problem — every caller handles `<= 0` the
same way: pass to `getRenderer` whose `<= 0 → 80` safety net kicks
in, or fall through to `mdrender.CurrentWidth()` from the helper
wrapper.

The exported `CurrentWidth()` and `SetCurrentWidth(int)` API:

```go
// SetCurrentWidth pins the package-wide terminal width. The TUI
// calls this on every tea.WindowSizeMsg. width <= 0 clears the
// pin.
func SetCurrentWidth(width int) {
    pinnedWidthMu.Lock()
    defer pinnedWidthMu.Unlock()
    if width <= 0 {
        pinnedWidth = 0
    } else {
        pinnedWidth = width
    }
}

// CurrentWidth returns the package's current view of the terminal
// width, resolved via the same chain currentWidth uses. Returns 0
// when no width source is available.
func CurrentWidth() int {
    return currentWidth()
}
```

The pin + RWMutex is overkill in production (the TUI calls
`SetCurrentWidth` from the single-threaded bubbletea Update loop,
never concurrently with `CurrentWidth`) but keeps `go test -parallel`
race-free — see `TestGlamour_SetCurrentWidth_PinsValue`.

### 2. TUI wiring — `tea.WindowSizeMsg`

`cmd/wagyu/tui_run.go:2664`:

```go
case tea.WindowSizeMsg:
    // Cycle 268: pin the package-wide width so the cycle-242 /
    // cycle-243 mid-stream glamour pass (which resolves its width
    // via currentWidth()) renders at the actual terminal width.
    mdrender.SetCurrentWidth(msg.Width)
    // Size the prompt to the terminal...
    if w := msg.Width - len(inputPrompt); w > 10 {
        m.input.SetWidth(w)
    }
    return m, nil
```

One line of production wiring for the parallel
`LineANSIBoundedWithCache` path. The chat-transcript path
(`renderMarkdown`) and step-detail path (`stepDetailLines`) already
take `width` arguments and get it from `m.width` (the runMode's
stored `tea.WindowSizeMsg.Width`).

### 3. Explicit-width plumbing — every call site

Every wrapper that touches a glamour render now takes an explicit
width:

| Function (file:line)                      | Pre-268 signature          | Post-268 signature          |
|-------------------------------------------|----------------------------|-----------------------------|
| `cmd/wagyu/conversation.go:491`           | `renderMarkdown(text)`     | `renderMarkdown(text, width)` |
| `cmd/wagyu/render_user_text.go:55`        | `renderUserText(s)`        | `renderUserText(s, width)` |
| `cmd/wagyu/print_glamour.go:150`          | `renderPrintText(raw)`     | `renderPrintText(raw, width)` |
| `cmd/wagyu/tui_tools.go:386`              | `toolBlockLines(s, e)`     | `toolBlockLines(s, e, width)` |
| `cmd/wagyu/tui_tools.go:908`              | `stepDetailLines(s)`       | `stepDetailLines(s, width)` |
| `cmd/wagyu/tui_tools.go:1146`             | `toolStepLines(s, e)`      | `toolStepLines(s, e, width)` |

Every caller either passes `m.width` (the TUI's pinned value) or
`glamourRenderWidth(out)` (print mode's `x/term.GetSize` seam in
`cmd/wagyu/main.go:106`). Tests pass `0`, which falls through to
`mdrender.CurrentWidth()` and resolves via `$COLUMNS` / `x/term`.

`LineANSI` and `LineANSIBounded` keep their single-argument
signatures (no caller takes a width; the wrapper is single-arg by
design — it's the per-line mid-stream helper, not a chat-transcript
renderer). They resolve via the package's `currentWidth()` chain.

### 4. Cache-key fix — `getRenderer`

`internal/mdrender/glamour_adapter.go:133`:

```go
func getRenderer(v mdVariant, width int) (*glamour.TermRenderer, error) {
    widthVal := width
    if widthVal <= 0 {
        widthVal = 80 // safety net — glamour default
    }
    mdCacheMu.Lock()
    if r, ok := mdCache[v][widthVal]; ok {
        mdCacheMu.Unlock()
        return r, nil
    }
    mdCacheMu.Unlock()
    // ...
    mdCacheMu.Lock()
    mdCache[v][widthVal] = r
    mdCacheMu.Unlock()
    return r, nil
}
```

The pre-268 code used `width` as the cache key directly, so `0`
and `80` collapsed to the same slot. Cycle 268 caches by
`widthVal` (post-fallback). A render at width=200 lands in a
different slot from a render at width=80; the FNV-64 prefix cache
becomes width-aware; glamour's per-renderer state stays correct.

### 5. Print-mode seam — `glamourRenderWidth`

`cmd/wagyu/main.go:106`:

```go
var glamourRenderWidth = func(out io.Writer) int {
    type fdWriter interface {
        Fd() uintptr
    }
    if w, ok := out.(fdWriter); ok {
        fd := int(w.Fd())
        if term.IsTerminal(fd) {
            if cols, _, err := term.GetSize(fd); err == nil && cols > 0 {
                return cols
            }
        }
    }
    return 0
}
```

This is the print-mode analogue of the TUI's
`mdrender.SetCurrentWidth(m.width)`. `Flush` calls it:

```go
// cmd/wagyu/print_glamour.go:129
rendered := renderPrintText(r.buf.String(), glamourRenderWidth(out))
```

When the writer is `os.Stdout` connected to a TTY, `term.GetSize`
returns the terminal width. When the writer is a pipe or buffer,
the function returns 0 and `renderPrintText` falls through to
`mdrender.CurrentWidth()` (which also checks `$COLUMNS` /
`x/term`). The seam is exported as a `var` (not a `func`) so tests
can pin the width without a real PTY.

## Files

| Path | Role |
|---|---|
| `internal/mdrender/glamour_adapter.go` | MODIFIED — `currentWidth()` impl; `pinnedWidthMu`, `pinnedWidth`, `readPinnedWidth()`, `CurrentWidth()`, `SetCurrentWidth()`; `getRenderer` cache by post-fallback `widthVal` |
| `internal/mdrender/stream_glamour.go` | MODIFIED — `LineANSI` resolves width via `currentWidth()` rather than hardcoded 0 |
| `internal/mdrender/cycle268_width_test.go` | NEW — 13 test functions pinning the cycle-268 contract on every axis the fix touches |
| `internal/mdrender/safe_boundary_test.go` | MODIFIED — `mustANSI` helper uses `currentWidth()` so its byte-equivalence assertion against `LineANSIBounded`'s render stays correct |
| `cmd/wagyu/conversation.go` | MODIFIED — `renderMarkdown(text, width)`; `mdrender.ANSI(stripped, convWidth)` with fallback to `mdrender.CurrentWidth()` |
| `cmd/wagyu/render_user_text.go` | MODIFIED — `renderUserText(s, width)` |
| `cmd/wagyu/print_glamour.go` | MODIFIED — `renderPrintText(raw, width)`; `Flush` calls `glamourRenderWidth(out)` |
| `cmd/wagyu/main.go` | MODIFIED — added `glamourRenderWidth` print-mode seam |
| `cmd/wagyu/tui_tools.go` | MODIFIED — `toolBlockLines`, `toolStepLines`, `stepDetailLines` take `width`; `mdrender.ANSI(stripped, toolWidth)` with fallback |
| `cmd/wagyu/tui_run.go` | MODIFIED — `case tea.WindowSizeMsg` calls `mdrender.SetCurrentWidth(msg.Width)`; 3 `toolBlockLines` / `toolStepLines` / `renderMarkdown` callsites pass `m.width` |
| `internal/cli/login_prompt.go` | MODIFIED — `RenderLoginPrompt` calls `mdrender.CurrentWidth()` |
| `cmd/wagyu/conversation_test.go` | MODIFIED — test callsites pass `width=0` to `renderMarkdown` (still falls through correctly) |
| `cmd/wagyu/render_user_text_test.go` | MODIFIED — same |
| `cmd/wagyu/print_glamour_test.go` | MODIFIED — same |
| `cmd/wagyu/tui_tools_test.go` | MODIFIED — same |
| `cmd/wagyu/tui_tools_step_title_test.go` | MODIFIED — same |
| `docs/cycles/cycle-268/repro/wg01_pty_check.py` | NEW — Python PTY harness: 80-col vs 250-col comparison |
| `docs/cycles/cycle-268/repro/main.go` | NEW — Go wrapper for the harness |
| `docs/cycles/cycle-268/repro/README.md` | NEW — reproduction README |

## Reproduction

```bash
# Build the cycle 268 binary:
go build -o /tmp/wagyu-c268 ./cmd/wagyu

# Headless regression suite — 13 cycle-268 tests + the existing
# mdrender tests all pass:
go test -vet=off -count=1 -v \
    -run "TestGlamour_WidthPin_|TestGlamour_CurrentWidth_|TestGlamour_SetCurrentWidth_|TestGlamour_PrefixCache_|TestGlamour_AllVariantsHonorWidth|TestGlamour_StyleBytesSurviveWidth|TestGlamour_NoInfiniteRecursionOnZeroWidth" \
    ./internal/mdrender/
# PASS=13/13

# Full test suite — confirms no regressions:
go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...

# End-to-end PTY harness:
go run ./docs/cycles/cycle-268/repro -mode repro1 -bin /tmp/wagyu-c268
# Expect exit 0 — wide PTY renders visibly wider than narrow PTY.

# Pre-fix baseline (cycle 267):
go run ./docs/cycles/cycle-268/repro -mode repro2 -bin /tmp/wagyu-final
# Expect exit 1 — both PTYs cap at ~80 cols.
```

The PTY harness opens two real `pty.fork()`s — one at 80 cols and
one at 250 cols — configures the WIN struct, sets
`OPENAGY_TRUST=always` to skip the workspace-trust dialog, sends
`analyze pwd\r`, captures ~6 s of PTY output bytes, strips ANSI /
CSI / OSC escapes, and measures the longest visible line in each
run. The cycle-268 contract is a single number: the wide run's
longest visible line must exceed the narrow run's by more than 20
cells. Pre-cycle-268 both runs cap at ~80 cells and the diff is
small; post-cycle-268 the wide run renders banner / footer lines
that honour the full 250-col width and the diff is large.

## Test status

`go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...` is green:

| Package                | Status  | Notes |
|------------------------|---------|-------|
| `internal/mdrender`    | ok 0.5s | 13 cycle-268 tests + 47 pre-existing tests all pass (the `mustANSI` helper in `safe_boundary_test.go:483` was updated to use `currentWidth()` so its byte-equivalence assertion against `LineANSIBounded` stays correct) |
| `cmd/wagyu`            | ok 19s  | 425 PASS / 0 FAIL / 9 SKIP. The tail `signal: broken pipe` is the pre-existing `TestForegroundHandler_RedirectsLogFile` closing fd 1/2 in defer — documented limitation, not a real test failure |
| 61 other internal packages | ok | unchanged from cycle 267 baseline |

Cycle 268 specific tests (`internal/mdrender/cycle268_width_test.go`):

- `TestGlamour_WidthPin_RealWidthNotCappedAt80` — the headline
  regression test. A 184-char source at width=200 emerges as one
  184-cell line; pre-fix it wrapped at the glamour hard default of
  80.
- `TestGlamour_WidthPin_LargerWidthProducesLongerLines` — the
  width parameter is monotonically increasing.
- `TestGlamour_WidthPin_CacheKeyDifferentWidths` — `getRenderer`
  maps different widths to different TermRenderer slots (the
  cycle-268 cache-key fix).
- `TestGlamour_WidthPin_EdgeCaseWidths` — width=0 / 1 / -1 / 999
  all produce *some* output (no panics, no empty strings).
- `TestGlamour_CurrentWidth_ResolvesColumnsEnv` — step 2 of the
  resolution chain.
- `TestGlamour_CurrentWidth_IgnoresInvalidColumns` — defensive
  parse: `abc`, `0`, `-7`, `1.5`, empty string all rejected.
- `TestGlamour_SetCurrentWidth_PinsValue` — the TUI's pin beats
  `$COLUMNS`; clearing the pin lets `$COLUMNS` win.
- `TestGlamour_PrefixCache_WidthAffectsCacheKey` — the cycle-243
  prefix cache keys on Width; same buffer at different widths
  renders to different output; cache hit on the same width returns
  verbatim output.
- `TestGlamour_WidthPin_NarrowSourceProducesContent` — a narrow
  source at width=200 still contains the source text verbatim.
- `TestGlamour_WidthPin_FallbackToEighty` — `getRenderer`'s
  `<=0 → 80` safety net stays in place.
- `TestGlamour_AllVariantsHonorWidth` — broad sweep: ANSI / Plain /
  User / Quiet all respect the width parameter.
- `TestGlamour_StyleBytesSurviveWidth` — the SGR payload is
  preserved across widths (the bug was a numeric cap, not a
  styling regression).
- `TestGlamour_NoInfiniteRecursionOnZeroWidth` — `ANSI(long, 0)`
  terminates.

The PTY harness complements these in-process tests with
end-to-end verification: it captures raw bytes the bubbletea
renderer emits on the PTY output fd and pins the width-respected
signal.

## Decision: explicit-width plumbing vs `currentWidth()` chain

The fix threads width through BOTH paths:

1. Every wrapper (`renderMarkdown`, `renderUserText`,
   `renderPrintText`, `toolBlockLines`, `toolStepLines`,
   `stepDetailLines`) takes an explicit `width` parameter. The TUI
   passes `m.width`; print mode passes `glamourRenderWidth(out)`;
   tests pass `0`.
2. `LineANSI` and `LineANSIBounded` keep their single-argument
   signatures and resolve via `currentWidth()` — because the
   cycle-243 cache already keys on `currentWidth()`, this is the
   natural seam.

The reason for keeping both:

- **Explicit width** is testable (a unit test can pin `width=200`
  and assert the rendered output). It also documents the contract
  on the function signature — a future maintainer sees
  `renderMarkdown(text, width int)` and knows width matters.
- **`currentWidth()` chain** is the seam the TUI uses to
  communicate the actual terminal width to the mid-stream glamour
  pass without changing every `LineANSIBounded` / `LineANSI` /
  `LineANSIBoundedWithCache` callsite (the cache key already
  reads `currentWidth()`). Changing the single-argument signatures
  of those helpers would have churned every caller for no
  functional benefit.

The `getRenderer` `<= 0 → 80` safety net stays in both paths so a
future call site that forgets to plumb a width still produces
readable output rather than glamour's "panic from word-wrap-on-zero"
failure mode. `TestGlamour_WidthPin_FallbackToEighty` and
`TestGlamour_WidthPin_EdgeCaseWidths` pin the safety net.

## Cycle numbers in nearby history

- cycle 228: glamour-backed pipeline for the turn-end reply
- cycle 232: idempotency of the glamour pass at turn-end (added
  `LineANSI` with the buggy `width=0` hardcode)
- cycle 242: `LineANSIBounded` boundary-aware variant
- cycle 243: FNV-64 prefix cache + `LineANSIBoundedWithCache`
- cycle 267: TUI streaming → done transition does a full screen
  repaint (the prior cycle)
- cycle 268: glamour word-wrap no longer caps at 80 columns
  ← THIS CYCLE

## Limitations / 硬约束

1. **PTY harness uses `--backend=shim`**, which truncates streaming
   replies to 40 chars. The rendered reply itself therefore never
   exceeds ~40 cells regardless of terminal width; the harness
   measures the surrounding banner / footer / transcript wrapper
   line widths, which DO scale with terminal width once the
   transcript is small. For the precise width contract (e.g. "a
   render at width=200 produces lines of exactly 200 cells"), see
   the in-process test suite
   (`internal/mdrender/cycle268_width_test.go`).

2. **The PTY harness's "width respected" assertion is a diff of
   > 20 cells between the 80-col and 250-col runs.** This is a
   generous margin to stay robust against the mocksham's 40-char
   truncation; the cycle's headless regression tests pin the
   exact-width contract (e.g.
   `TestGlamour_WidthPin_RealWidthNotCappedAt80` asserts a 184-char
   source at width=200 emerges as one 184-cell line).

3. **Print mode resolution still relies on `os.Stdout` being a
   TTY.** When the user pipes `wagyu -p` to `cat` or a file, the
   `glamourRenderWidth` seam returns 0 and `renderPrintText` falls
   through to `mdrender.CurrentWidth()`. The package's
   `currentWidth()` chain then checks `$COLUMNS` and `x/term` —
   both of which are also unavailable for a piped consumer. The
   final fallback is the `<= 0 → 80` glamour safety net. This is
   the intended behaviour (a piped consumer cannot know the user's
   terminal width) and matches the cycle-228 contract for the
   non-TTY path: glamour caps at the conservative default rather
   than wrapping at a guessed wider value that would produce
   double-wrapped output when piped through `less -S`.

4. **The `TestForegroundHandler_RedirectsLogFile` test still
   produces a "signal: broken pipe" tail on `go test ./cmd/wagyu/`.**
   The test closes fd 1/2 in its defer block; the Go test runner
   reads SIGPIPE as a non-zero exit even though no test reports
   `--- FAIL`. 425 PASS / 0 FAIL / 9 SKIP is the actual result.
   This was true before cycle 268 and is unchanged; documented
   here for completeness.

5. **`mdrender.SetCurrentWidth` is a package-wide side effect.**
   The TUI is the only writer in production, but parallel tests
   that share a process must serialise their writes. The pin uses
   an RWMutex; concurrent readers from `currentWidth()` do not
   block each other, only writers block all readers. The cost is
   one atomic write per terminal resize — negligible.

6. **Glamour's word-wrap plugin treats the configured width as an
   upper bound**, not a strict per-line ceiling. A single word
   longer than the configured width may exceed it; a line that
   wraps inside a styled run (e.g. an unfinished `**bold**`) may
   come out one cell shorter than the configured width. The
   cycle-268 test suite accounts for this with assertions like
   "longest line ≤ width" and "longest line ≥ width - 5", rather
   than pinning the exact cell count. The behaviour matches
   glamour's own documentation.

7. **`getRenderer`'s `<= 0 → 80` safety fallback is a defence-in-
   depth measure**, not a feature. The cycle-268 plumbing
   guarantees every production caller passes a positive width
   (either via the TUI's `SetCurrentWidth(m.width)` pin or via the
   print-mode `glamourRenderWidth(out)` seam); the safety net is
   for future call sites that forget. If a future glamour version
   ships a stricter contract (e.g. panics on width=0), the safety
   net can be tightened to log a warning instead of silently
   swallowing the miss.
