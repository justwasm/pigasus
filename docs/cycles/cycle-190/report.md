# Cycle 190 — `/context` panel

## What REF does that OUR did not

`~/.local/bin/agy` (v1.4.0) opens `agy /context` as a **modal
information panel**, not a one-line transcript entry. Even on an empty
session it renders:

```
└ Context Usage
□ □ □ □ … (× 44 cells, 7 rows)     <descriptor right-side>
System files · auto-loaded
 └ ~/.gemini/GEMINI.md
 └ ~/.gemini/skills/
 └ ~/.gemini/antigravity/skills/

Related: /artifact · /skill · /rewind
  ↑/↓ Scroll · pgup/pgdown Page · ctrl+end Bottom · ctrl+home Top · esc Close
```

The right-side descriptor changes per row (model name + tokens /
percentage / `Estimated usage` / three `◉` bullets per category /
`□ Free space`). The 44×7 `□` matrix is in dim gray 241.

OUR shipped a one-liner:

```
  Context usage: session has 0 message(s)
```

## What this cycle ships

The empty-session `/context` panel in `cmd/wagyu/slash_panels.go`:

- **`└ Context Usage`** — bold + dim gray 238 (`\x1b[1m\x1b[38;5;238m`)
  title, matches REF ppref.context.empty.raw.txt row 14.
- **7-row × 44-cell `□` grid** — each cell wrapped in
  `\x1b[38;5;241m□\x1b[39m`, single spaces between cells, no trailing
  space — matches REF rows 15-21.
- **Per-row right-side descriptor** in plain default fg:
  - Row 1: `<model> · 0/1.0M tokens` (where `<model>` comes from
    `m.model` — the full display name with tier suffix, e.g.
    `"Gemini 3.8 Flash (High)"`).
  - Row 2: `(0.0%)` — **padded 6 spaces, not 5** (REF quirk).
  - Row 3: `Estimated usage (awaiting generation)` — bold + dim 238
    wrap.
  - Row 4: `◉ User messages: 0 tokens (0.0%)` — only the `◉` bullet
    is bolded, in dim blue 26.
  - Row 5: `◉ Agent responses: 0 tokens (0.0%)` — only the `◉` bullet
    coloured, in green 29 (no bold).
  - Row 6: `◉ Tool calls: 0 tokens (0.0%)` — only the `◉` bullet bolded,
    in orange 172.
  - Row 7: `□ Free space: 1.0M (100.0%)` — only the `□` coloured,
    in dim 241.
- **`System files · auto-loaded`** plain header (no SGR on this row,
  matching REF).
- **3 system-file paths** `\x1b[38;5;241m └ ~/.gemini/...\x1b[39m`,
  matching ppref rows 23-25.
- **`Related: /artifact · /skill · /rewind`** in dim 241.
- **Scroll dot-hint legend** using `panelDotHint` with five
  key/label pairs, matching REF row 29.

Plus:
- **`Esc closes silently`** (no `⎿ Exited /context command` echo) —
  `panelContext` joins `{panelMCP, panelPermissions, panelCodeSearch,
  panelFeedback, panelRewind}` in `panelKind.exitNotice()`.
- **No trailing blank row** between legend and status row —
  `trailingBlank()` was wrong for this panel and now returns `false`
  for `panelContext` (same exception as `panelHooks`).

## Files

| File | Δ | What |
|---|---|---|
| `cmd/wagyu/slash_panels.go`   | +76 / -8 | Add `panelContext` to kind enum, `panelForCommand`, `panelLines` switch, `exitNotice`, `trailingBlank`; new `contextGridRow` / `contextGridRowPadded` helpers; new `panelContextLines` method. |
| `cmd/wagyu/slash_panels_test.go` | +57 / -3 | `TestPanelContextEmptyLayout`, `TestContextExitNotice`, `TestContextGridRowShape`; `"context"` added to the `panelForCommand` matrix. |
| `TODO.md`                     | +2 / -2 | `/context` row rewritten with full recovered layout and `✅ cycle 190` close mark; P1 panel count 20 → 8; P1 examples drop `/context` (and the 11 other closed panels). |

## Verification

`/tmp/agycmp/cmp_panels.py context` reports `IDENTICAL (SGR-normalised)`
against `/tmp/agycmp/panels/ppref.context.empty.raw.txt`. Sweep across
all 12 panels:

| Panel | Result |
|---|---|
| `mcp`         | IDENTICAL (SGR-normalised) |
| `tasks`       | IDENTICAL (SGR-normalised) |
| `effort`      | IDENTICAL (SGR-normalised) |
| `hooks`       | IDENTICAL (SGR-normalised) |
| `permissions` | IDENTICAL (SGR-normalised) |
| `agents`      | 1 differing line — environmental (workspace path; the REF was probed from `/tmp/agycmp`, ours from `/Users/gear/GitHub/openagy`). Not a regression. |
| `credits`     | IDENTICAL (SGR-normalised) |
| `codesearch`  | IDENTICAL (SGR-normalised) |
| `artifact`    | IDENTICAL (SGR-normalised) |
| `feedback`    | IDENTICAL (SGR-normalised) |
| `rewind`      | IDENTICAL (SGR-normalised) |
| `context`     | IDENTICAL (SGR-normalised) |

Behaviour:
- `Esc` on `/context` closes silently — no echo (verified in
  `ppour.context.esc.raw.txt`).
- `Up`/`Down` are no-ops (the panel fits one screen, no scroll state).
- `Enter` does nothing (verified in `ppour.context.enter.raw.txt` —
  panel still shows `└ Context Usage`).

Tests: `go test ./cmd/... ./internal/...` — 27 packages, 0 failures.

## Hard constraints / known limitations

- **Empty session only.** All seven right-side descriptors hardcode
  the empty-state text REF emits (`0 tokens (0.0%)`, `1.0M (100.0%)`,
  `Estimated usage (awaiting generation)`). Non-empty sessions would
  require (a) per-category token counts (user / assistant / tool),
  (b) the percentage to recalculate, (c) the `Free space` to subtract
  used tokens from `1.0M`, (d) the `Estimated usage (awaiting
  generation)` line to be replaced once a model response lands. The
  clone does not yet track tokens per category, so a non-empty-session
  `/context` would be misleading. This is the next step for this
  panel and is gated on token accounting.
- **No model / token / tier rendering for non-empty sessions.** Same
  blocker: the descriptor line currently hardcodes
  `· 0/1.0M tokens`, the percentage hardcodes `(0.0%)`.
- **No content overflow / scroll behaviour tested.** The REF capture
  shows `↑/↓ Scroll · pgup/pgdown Page · ctrl+end Bottom · ctrl+home
  Top` on the legend; for the empty session the grid fits in one
  screen and scroll is a no-op. If the grid or the bullets ran long
  enough to overflow, that scroll behaviour is not implemented in
  this cycle.
- **System file paths are hardcoded** to `~/.gemini/GEMINI.md`,
  `~/.gemini/skills/`, `~/.gemini/antigravity/skills/`. REF reads
  them from `settings.json`; the clone would need to surface those
  settings through `runMode` before the paths could become dynamic.
- **The cycle's stage-2 plan was deferred** (a separate rewrite to
  compute per-category token counts) — this cycle only ships the
  empty state.

## What changed vs the previous state

- `panelKind` enum now has `panelContext` (was 12 values, now 13).
- `panelForCommand("context")` now returns `(panelContext, true)`.
- `panelKind.exitNotice()` returns `false` for `panelContext`.
- `panelKind.trailingBlank()` returns `false` for `panelContext`
  (matches `panelHooks`).
- `panelLines()` returns `m.panelContextLines()` for `panelContext`.
- TODO.md panel row for `/context` now reads ✓ cycle 190 instead of
  the bare open-spec.

## Counts

- Panels total: 13 (was 12). Closed: 13 (was 12). Open in P1: 8
  (was 9).
- Lines added in `slash_panels.go`: 76. New tests: 3
  (`TestPanelContextEmptyLayout`, `TestContextExitNotice`,
  `TestContextGridRowShape`).
- Files touched: 3 (`slash_panels.go`, `slash_panels_test.go`,
  `TODO.md`). No changes outside these three.
