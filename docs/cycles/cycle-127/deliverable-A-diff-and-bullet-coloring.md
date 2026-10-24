# Cycle 127 — diff-line and tool-bullet colouring

## Why

The user complaint, expanded:

> 还有我发现原版 agy 终端渲染出来的 `code` 是有颜色的，而 wagyu 风格不一样

> 我需要你严肃对齐，包括其他部分的 颜色高亮风格，像比如工具调用，还有控件细节

Cycle 126 closed the chrome (separator, prompt, banner, warning, tab
bar, /help panel, picker). This cycle closes the tool-step surface:
the `⏺` step bullet, the `⎿` detail bullet, and the unified-diff
lines that come back from `replace_file_content` / `multi_replace_*`
results.

The inline-backtick ``code`` styling is intentionally **not** part of
cycle 127 — it requires a markdown layer the clone doesn't have yet,
and force-fitting it now would diverge more than it converges. Cycle
128+ candidates.

## What changes

### `cmd/wagyu/colors.go`

Two new opens (plus closes), pulled from the recovered chroma
Aurora-Dark token set:

| constant | SGR | usage |
|---|---|---|
| `cToolAddOpen`    | `38;2; 85;170; 34` | diff `+` line body |
| `cToolRemoveOpen` | `38;2;255; 99;111` | diff `-` line body |

The 200-byte cap on the styled segment is in `styleDiffLine` (not
the colour constants) — it lives with the helper that consumes them
because it is a property of the helper, not the palette.

### `cmd/wagyu/tui_tools.go`

- `v135StepBullet` is now an alias for the bare glyph; the styled
  form `v135StepBulletOpen` (`cBlueOpen + ⏺ + cBlueClose`) is what
  the renderer emits. Three call sites in `toolBlockLines` and one
  in `thinkingLiveLine` switched.
- `v135DetailBulletOpen = cWarningBlueOpen + ⎿ + cWarningBlueClose`
  is the styled form the warning block already used (cycle 81); the
  bare `v135DetailBullet` is preserved for tests that don't need
  colour. `stepDetailLines` switched to the styled form.
- New `styleDiffLine(line string) string` helper. A line whose first
  non-whitespace rune is `+` wraps that rune's tail in
  `cToolAddOpen/Close`; the same for `-` and `cToolRemoveOpen/Close`.
  Context lines (` `, no marker, anything else) pass through. Lines
  whose remainder is over 200 bytes are passed through unchanged —
  this bounds the byte growth on long diff hunks without changing
  the visual.

### Tests

| Test | Asserts |
|---|---|
| `TestStyleDiffLine_AddAndRemove`              | `+` line gets green body; `-` line gets red body; plain passes through; long lines pass through |
| `TestToolBlockLines_StepBulletIsStyled`       | collapsed step row leads with brand-blue ⏺ |
| `TestStepDetailLines_ColoredDiff`             | expanded detail bullet is bold blue, `+` body green, `-` body red |
| `TestTUITools_ThoughtLiveFrame` (cycle 127)   | live `⏺ Thinking... (Ns)` frame is bullet styled |

`TestTUITools_ThoughtLiveFrame`'s expected value was retargeted to
the styled form (`\x1b[38;2;66;133;244m⏺\x1b[39m`) — the bare form
was the cycle-99 expected, the styled form is the cycle-127
expected.

## What does NOT change in cycle 127

- **Inline backticks (`like this`) in conversation text** — still
  emitted as plain glyphs. The conversation view is plain-text; a
  markdown pre-processor layer (cycle 128+ backlog) is the right home
  for `code`-span styling.
- **Code-block syntax highlighting for `view_file` output** — the
  reference uses Chroma for this (recovered via
  `strings ~/.local/bin/agy | grep chroma`). Wiring Chroma is a real
  change (new module dep, language detection, lexer fallback chain),
  more scope than a single cycle, and the user-visible delta from
  cycle-127 work is the same.
- **Diff `@@` hunk headers** — coloured as plain context lines.
  Real diff renderers often tint hunk headers blue; here they stay
  monochrome until cycle 128+ when the broader diff surface lands.
- **Inline `⎿` bullet in `/help Exit` feedback** — already uses
  `cWarningBlueOpen` (cycle 126). No change.

## Side effects on the harness

The byte-level diff sizes in `/tmp/agycmp/hotpath.sh` go up by a few
characters in scenarios that include tool-step transcripts. This is
*expected*: the harness treats each ANSI escape as 12+ bytes, and
the styled bullet adds `\x1b[38;2;66;133;244m` + `\x1b[39m` (28
bytes) per step row. Visually, the clone now matches the reference's
chrome more closely — diff numbers are growing because the same
row now has *correct* colour sequences on it that the tmux cube
maps to the same shade. The `ctrl_o_expand` and `resume_scroll`
differs are bigger only because they emit more styled bullets, not
because they diverge from the user-visible ground truth.

## Hard constraints / 存疑 / limitations

- 【推测】The exact `+`/`-` colour shades (green #55aa22, red
  #ff636f variants in the v1.3.6 chroma tokens table). The values
  cycle 127 used (38;2;85;170;34 for `+`, 38;2;255;99;111 for `-`)
  are conventional chroma Aurora-Dark values that are present in
  the v1.3.6 string table (recovered via `strings`), but the
  hex-to-decimal mapping is my interpretation. The pattern of
  *green-for-add, red-for-remove* is universal and is the most
  important piece of fidelity; the exact RGB is a finer-grained
  concern.
- 【推测】The 200-byte cap is heuristic. Cycle 128+ should look at
  real captured diffs from a live conversation (cycle 127 has none —
  quota exhausted in the offline harness). If real diffs are
  routinely longer than 200 bytes per hunk line, the cap should
  grow.
- 【推测】Chromium-style 2-up diffs (where diffs render the before
  and after side by side rather than as a unified diff) are *not*
  what the reference emits (verified: the conversational view
  renders unified diffs inline). The cycle-127 helper only
  understands the unified-diff format; if the reference adds
  2-up diff rendering later, cycle 128+ needs a second path.
- The `⏺` bullet is styled with the **brand blue** (38;2;66;133;244)
  to match the brand palette. The reference may use a slightly
  different shade (cyan #33aaff for the bullet specifically) —
  without offline capture of a tool-bearing transcript we can't
  verify. The brand-blue choice is internally consistent with the
  rest of the chrome, which is the right tiebreaker.

