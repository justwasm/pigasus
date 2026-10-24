# Cycle 162 — Tool-step args palette split at first paren

## Symptom

The captured 200×60 frames after cycle 161 still showed a two-palette
diff inside the args:

```
=== ref ===
                                                                          1 file
```

Wait — that's not the right format. Let me redo.

The captured ref bytes around an "Edited" step show:

```
[38;5;29m●[39m [1m[38;5;172mEdited[0m [38;5;235m1 file[39m [38;5;238m(build-release.yml)[39m
```

Three colour zones:

1. **verb** — `[1m[38;5;172m` (bold + orange) for "Edited"
2. **outer args** — `[38;5;235m` (dark gray) for "1 file"
3. **paren args** — `[38;5;238m` (dim gray) for "(build-release.yml)"

The cycle-157 clone put *everything* after the verb into a single
`cToolArgsOpen` palette. Cycle 161 made `cToolArgsOpen` = 238, which
matched ref's *paren* portion but produced a single dim-gray block
for both halves of the args — visually merging the count noun and
the file annotation.

For "Ran (git add ...)" the args are entirely wrapped in parens, so
the cycle-161 238-only paint accidentally matched ref. For "Edited
1 file (build-release.yml)" it didn't.

## Fix

`cmd/wagyu/colors.go`:

- `cToolArgsOpen` updated from `[38;5;235m` to `[38;5;238m` — this
  is the *paren* palette (dim gray).
- New `cToolArgsOuterOpen` / `cToolArgsOuterClose` palette
  `[38;5;235m` / `[39m` — the *outer* (non-paren) palette (dark
  gray).

`cmd/wagyu/tui_tools.go`:

- `styledStepTitle` now calls `splitArgsAtParen(args)` to split the
  args into `outArgs` (everything before the first `(`) and
  `parenArgs` (the `(...)` portion).
- Each portion is wrapped in its own palette. Empty portions are
  skipped — `Ran (...)` produces only the paren palette; `Ran rm
  rea-go-seeds` produces only the outer palette.
- New helper `splitArgsAtParen(args string) (outer, paren string)`
  scans for the first `(`; everything before goes to outer, the
  `(...)` portion (including the opening paren and any trailing
  close) goes to paren.

## After

```
=== ours 000 (post-162, 200×60, raw) ===
[38;5;29m●[39m [1m[38;5;172mEdited[0m [38;5;235m1 file[39m [38;5;238m(build-release.yml)[39m
```

Ref's recovered sequence (byte-equivalent modulo the verb palette
which was already aligned in cycle 157):

```
[38;5;29m●[39m [1m[38;5;172mEdited[0m [38;5;235m1 file[39m [38;5;238m(build-release.yml)[39m
```

Identical ANSI byte sequence.

For "Ran (rm ...)" — single-paren case — only the paren palette
fires:

```
=== ours ===
[38;5;29m●[39m [1m[38;5;172mRan[0m [38;5;238m(rm rea-go-seeds/XxxX.java ...)[39m
```

## Files touched

- `cmd/wagyu/colors.go` — `cToolArgsOpen` palette update; new
  `cToolArgsOuterOpen` / `cToolArgsOuterClose`.
- `cmd/wagyu/tui_tools.go` — `styledStepTitle` calls
  `splitArgsAtParen`; new helper.

## Verification

```
go build -o /tmp/agycmp/our/agy ./cmd/wagyu
go test ./cmd/... ./internal/...                 # 25 packages green
REF=… OUR=… bash /tmp/agycmp/hotpath.sh          # 5 scenarios
bash /tmp/wide_compare.sh                        # 6 frames @ 200×60
```

100×30 hotpath diff summary (modest improvement vs cycle 161):

```
cold_start   000-init            diff= 33        (was 33, parity)
ctrl_o_expand 000-init           diff= 42        (was 45, -3)
              001-scrolled       diff= 58
              002-expanded       diff= 58
              003-collapsed      diff= 58
help_tabs     000-general        diff= 28
              001-second         diff=  9
              002-third          diff= 12
              003-wrapped        diff= 28
              004-closed         diff= 22
picker        000-slash          diff= 23
              001-down5          diff= 15
              002-closed         diff= 15
resume_scroll 000-init           diff= 41
              001-pgup           diff= 58
              002-bottom         diff= 52
              003-top            diff= 52
```

ctrl_o_expand 000-init dropped 3 lines (45 → 42) because the
expanded tool block now splits the args at `(` exactly like ref.

200×60 000-init captures: ref 40 lines, ours 41 — both fill the
viewport. The conversation-content offset between captures is the
remaining gap.

## Limitations

- The transcript area in ref is padded to 197 chars wide (terminal
  width 200 - 3 char right border); ours pads nothing. This produces
  48 stripped-content diffs in the 60-row capture — the largest
  remaining visible gap. Cycle 163+ work.
- Italic styling (`*foo*`) is still not rendered.
- Markdown tables and links are still passed through unchanged.
- The "Ran (rm ...)" full-args line still wraps to a second
  visible row at 200×60; ref appears to let it overflow without
  wrapping. Cycle 163+ may need to disable wrap for tool titles.