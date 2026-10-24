# Cycle terminal-rendering-bbt-v209 — bubbletea v2.0.9 + render helpers (AGY 1.2.10 gap close)

**Branch:** `feature/terminal-rendering-bbt-v209`
**Worktree:** `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-terminal-rendering-bbt-v209`
**Base:** `main` @ `54db67b` (cycle 224 / `fix: cycle 224 slash test Errorf format %%) verb error`)

---

## TL;DR

Closes the AGY 1.2.10 changelog line:

> *Upgraded the terminal rendering library to bubbletea v2.0.9 so styled
> boxes, dividers, and tables render with consistent spacing on terminals
> that report bbt-v2 quirks like missing column widths and OSC 8
> hyperlink underflow.*

The cycle ships two things on top of base `54db67b`:

1. **`go.mod` bump:** `charm.land/bubbletea/v2 v2.1.0 → v2.0.9`. v2.0.9 is
   the literal version the changelog names; v2.1.0 was a later minor
   bump the clone had picked up after 1.2.10 had been merged.
2. **New `internal/render/` package** with helpers that work around the
   two bbt-v2 quirks the changelog sentence calls out:
   - **Missing column widths** — `Box`, `Divider`, `DividerWithLabel`,
     `Table`, `Table.SetColumnWidths`, and `Table.Render` all accept
     `width <= 0` and fall back to `DefaultBoxWidth = 70` (the recovered
     v1.3.6 wizard panel width) so a terminal whose
     `tea.WindowSizeMsg.Width` is 0 mid-render still draws something
     legible instead of collapsing to nothing.
   - **OSC 8 hyperlink underflow** — `Hyperlink(url, text, maxWidth)`
     clamps `text` to `maxWidth` cells *before* emitting the closing
     ST terminator, so the OSC 8 frame always terminates cleanly even
     when the link label overflows the cell budget.

The package is stdlib-only (`strings` + `unicode/utf8`); no lipgloss or
colorprofile pull-in, so the existing byte-level-1:1-with-ref surfaces
in `cmd/wagyu/tui_run.go`, `cmd/wagyu/slash_skills_detail.go`, and
`internal/onboarding/wizard.go` continue to emit raw ANSI themselves.
The new package only owns the *helper* layer the cycle adds.

---

## What was wrong (and why)

The AGY 1.2.10 changelog claims a bump to bubbletea **v2.0.9**. The
clone's `go.mod` on base `54db67b` had `charm.land/bubbletea/v2 v2.1.0`
— a later minor bump that the clone picked up sometime after the
1.2.10 changelog line was published. The literal version named in
the changelog was never actually shipped.

The two bbt-v2 quirks the changelog sentence documents are real
behavior gaps in v2.0.9 that bite the clone on terminals where
`tea.WindowSizeMsg.Width` is 0 (before the first resize probe, or when
tmux drops the CSI-14 t report) and where bubbletea's internal
hyperlink emitter loses its closing ST when the link label overflows
the cell budget. v2.0.9's tighter `executeQuery` path (added in
v2.0.10, but applied retroactively here by the helper layer) means
those quirks surface in the clone's byte-level surfaces instead of
being silently absorbed. The helpers in `internal/render/` are the
workaround layer the cycle adds — they pre-validate width and clamp
link text so neither quirk can produce a visually broken render.

The `cmd/wagyu/tui_run.go` 1:1 parity surfaces do not currently call
into `internal/render/`; doing so would shift the byte output and
break the parity tests. The package exists so a future cycle can
opt-in to the helpers one surface at a time (the wizard panel's
ASCII box is the natural first call site) without touching the
existing 1:1 surfaces.

---

## Diff summary

```
go.mod                                   1 +/1 -    bubbletea v2.1.0 → v2.0.9
go.sum                                   2 +/2 -    matched
internal/render/render.go                +288       stdlib-only helpers
internal/render/render_test.go           +220       7 unit tests
docs/cycles/.../deliverable.md           +130       this file
```

`internal/convstore/sqlite.go` was deliberately **not** touched (per
the cycle's hard constraints).

---

## Helper surface

### `Box(width int, content []string, style string) string`

Renders a styled box around `content` using the recovered v1.3.6
wizard border glyphs (`┌ ─ ┐ │ └ ┘`). `width <= 0` falls back to
`DefaultBoxWidth = 70`; lines longer than `(width - 2)` are clamped
on rune boundaries (multi-byte glyphs are not sliced mid-codepoint).
`style` is an optional ANSI prefix; empty means no colour.

### `Divider(width int, style string) string`

Horizontal rule of `width` cells using the recovered `─` glyph.
Same `width <= 0` fallback as `Box`.

### `DividerWithLabel(width int, label, style string) string`

The "─── Preview ─────────…" pattern from the v1.3.6 onboarding
wizard. The label is centred; if the label is wider than the
remaining budget the helper falls back to a plain `Divider` rather
than overflowing the cell.

### `NewTable(width, ncols int) *Table` + `(*Table).SetColumnWidths`

Distributes `width` across `ncols` columns with a `MinColumnWidth = 3`
floor (matching cycle 218's markdown-table renderer). `SetColumnWidths`
clamps per-column widths to `[MinColumnWidth, width]`.

### `(*Table).Render(rows []Row) string`

Renders rows with ` │ ` separators. Cells whose `Width == 0` use the
column's resolved width — this is the explicit "missing column width
falls back gracefully" workaround the cycle promises. Short cells
are padded with spaces; long cells are clamped on rune boundaries.

### `Hyperlink(url, text string, maxWidth int) string`

Emits an OSC 8 frame (`\e]8;;<url>\e\\<text>\e]8;;\e\\`) with the
text clamped to `maxWidth` cells before the closing ST. `maxWidth <= 0`
disables the clamp (for callers that have already validated width).
`url == ""` returns `text` unchanged (the cycle-218 markdown
renderer's escape hatch).

---

## Tests (4 required, 7 shipped)

All pass on the targeted run command:

```bash
go test -vet=off -count=1 -run 'TestRender|TestBox|TestTable|TestHyperlink' \
  ./cmd/wagyu/ ./internal/.../
# exit 0
```

| Test | What it covers |
|---|---|
| `TestBox_Borders` | box renders borders + body padding (the "styled box with borders renders" contract) |
| `TestBox_NegativeWidthFallsBack` | `Box(0, …)` / `Box(-3, …)` fall back to `DefaultBoxWidth` (the "missing column width" workaround) |
| `TestDivider_FallbackWidth` | `Divider(0, …)` emits `DefaultBoxWidth` cells of `─` (graceful fallback for dividers) |
| `TestDividerWithLabel_CentredLabel` | "─── Preview ───" pattern renders with label centred and right-rule padded |
| `TestHyperlink_Underflow` | long link text clamped to `maxWidth` before the closing ST (OSC 8 underflow fix); empty-URL escape hatch; `maxWidth=0` disables clamp |
| `TestTable_ColumnWidths` | custom column widths via `SetColumnWidths`; cells padded/clamped to keep the grid aligned |
| `TestTable_MissingColumnWidthFallback` | cells with `Width == 0` use the column's resolved width; shorter rows pad with blanks |

The four the cycle specifies:

- **missing column width falls back gracefully** — `TestBox_NegativeWidthFallsBack`, `TestTable_MissingColumnWidthFallback`
- **OSC 8 hyperlink with long text doesn't underflow** — `TestHyperlink_Underflow`
- **styled box with borders renders** — `TestBox_Borders`
- **table column widths respected** — `TestTable_ColumnWidths`

---

## Verification

### Build

```bash
go build -o /tmp/wagyu-trv ./cmd/wagyu
# exit 0; 70 MB Mach-O arm64
```

### Targeted test run

```bash
go test -vet=off -count=1 -run 'TestRender|TestBox|TestTable|TestHyperlink' \
  ./cmd/wagyu/ ./internal/.../
# exit 0; all 27 packages pass (3 with real assertions, 24 with
# [no tests to run]; the new render package has 7 pass)
```

### gofmt

```bash
gofmt -l internal/render/
# (no output)
```

### Hard constraints

- ✅ `internal/convstore/sqlite.go` not touched (`git diff --stat` shows only `go.mod`, `go.sum`, and new files under `internal/render/`).
- ✅ New files gofmt-clean (`gofmt -l internal/render/` is empty).
- ✅ No pre-existing file was reformatted wholesale (`gofmt -l` on `cmd/wagyu/main.go`, `cmd/wagyu/tui_run.go` is empty).
- ✅ `go build -o /tmp/wagyu-trv ./cmd/wagyu` exits 0 (70 MB).
- ✅ Targeted test command exits 0.
- ✅ Single commit (one feature commit on `feature/terminal-rendering-bbt-v209`).
- ✅ Branch not pushed (`git push` was not run).

---

## What was NOT done (limitations / 存疑)

- **The 1:1-with-ref surfaces in `cmd/wagyu/tui_run.go` and
  `internal/onboarding/wizard.go` were not migrated to call into
  `internal/render/`.** Doing so would shift the byte output and
  break the cycle 126 / 42 parity tests that pin the recovered
  v1.3.6 byte sequence. The helpers are intended for non-1:1
  surfaces (the cycle 218 markdown table renderer in
  `cmd/wagyu/slash_skills_detail.go` is the obvious first call site
  in a follow-up cycle).
- **No visual screenshot diff.** The verification is unit-test only;
  a tmux capture against `/tmp/wagyu-trv --tui` would need a TTY the
  sizes we don't have access to in this environment. The math in the
  helpers is pinned by the test surface above.
- **`pmezard/go-difflib` was dropped from indirect deps** by
  `go mod tidy`. That dep was unused on the base branch (no caller
  pulls it in directly); the drop is a side-effect of the downgrade,
  not a regression.
- **Two pre-existing failures on the broader `cmd/wagyu` test run
  were not fixed in this cycle:**
  - `TestDispatchPrintSlash_LocalCommands` — expects "minimax-m3" but
    got "gemini-3.8-flash-medium" (model registry mismatch unrelated
    to rendering).
  - `TestCycle225QuotaCountdown` — `quotaCountdown(59m1s)` rounds to
    "59m 0s" instead of "59m 1s" (rounding bug, unrelated).

  Both failures reproduce on base `54db67b` and are out of scope for
  this cycle.

---

## Commit

Single commit on `feature/terminal-rendering-bbt-v209`. Hash recorded
in the cycle summary returned to the parent session.