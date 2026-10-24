# Cycle 156 — Tool-bullet ● and thought-bullet ▸

## Symptom

The captured 200×60 frames at the bottom of a long conversation showed
the tool step glyph was wrong:

```
=== ref ===  ● Ran (git add .github/workflows/build-release.yml && ...)
=== ours === ⏺ Ran (git add .github/workflows/build-release.yml && ...)
```

```
=== ref ===  ▸ Thought for 12s
=== ours === ⏺ Thinking... (31s)
```

Cycle 127 had picked U+23FA ⏺ "record" in brand blue for both — the
choice was inherited from Aider/Cursor conventions, not recovered from
the v1.3.6 binary. Recovering the actual byte sequence from the raw
capture shows ref uses two distinct glyphs:

| step kind | glyph    | bytes         |
|-----------|----------|---------------|
| tool      | U+25CF ● | `\u25cf`      |
| thought   | U+25B8 ▸ | `\u25b8`      |

and the palette around them is also distinct — 256-colour 29 (deep
teal/green) for tool, 256-colour 238 (dim gray) for thought. Cycle 156
applies both.

## Fix

`cmd/wagyu/colors.go`:

- new constants `cToolBulletOpen = "\x1b[38;5;29m"` and
  `cToolBulletClose = "\x1b[39m"` — the tool-step palette recovered
  from the v1.3.6 binary.
- new constants `cThoughtBulletOpen = "\x1b[38;5;238m"` and
  `cThoughtBulletClose = "\x1b[39m"` — the thought-step palette.

`cmd/wagyu/tui_tools.go`:

- `v135StepBulletOpen` / `v135StepBulletPlain` now compose the U+25CF
  "●" glyph with the cToolBullet palette. U+23FA ⏺ is gone.
- new constants `v135ThoughtBulletOpen` / `v135ThoughtBulletPlain`
  compose the U+25B8 "▸" glyph with the cThoughtBullet palette.
- `toolBlockLines` picks the bullet per-step via a `bulletFor` closure
  that returns `v135ThoughtBulletOpen` for thought steps and
  `v135StepBulletOpen` for everything else.
- `thinkingLiveLine` uses `v135ThoughtBulletOpen` — the live "still
  thinking" frame is a thought frame, not a tool frame.

`cmd/wagyu/tui_tools_test.go`:

- `TestTUITools_ThoughtLiveFrame` pins the new thought bullet
  `\x1b[38;5;238m\u25b8\x1b[39m`.
- `TestToolBlockLines_StepBulletIsStyled` pins the new tool bullet
  `\x1b[38;5;29m\u25cf\x1b[39m`.

## After

```
=== our 000 (post-156, 200×60) ===
 0 "    ?? rea-go-seeds/analyzeAgyV135Tui.java.bak..."     (file 2)
 1 "    ```"                                               (4-space indent on fence)
 3 "  都是 cycle 39 时期..."                              (bold stripped)
 5 "  • `XxxX.java` — 类名就叫 `XxxX`..."               (• bullet + backticks)
17 "  • `git log --all -- ...` → 空..."
…
  N "● Ran (git add ... && git commit ...)"                (● tool bullet)
  N "▸ Thought for 12s"                                    (▸ thought bullet)
```

The bullet characters now match ref's recovery. The colour difference
is recoverable but masked by the human eye — both styles read as
"highlighted status line" on a real terminal.

## Files touched

- `cmd/wagyu/colors.go` — `cToolBulletOpen`, `cToolBulletClose`,
  `cThoughtBulletOpen`, `cThoughtBulletClose`.
- `cmd/wagyu/tui_tools.go` — `v135StepBulletOpen` (U+25CF + teal),
  new `v135ThoughtBulletOpen` (U+25B8 + dim gray), `toolBlockLines`
  closure, `thinkingLiveLine` body.
- `cmd/wagyu/tui_tools_test.go` — bullet-bytes in the two pinning
  tests.

## Verification

```
go build -o /tmp/agycmp/our/agy ./cmd/wagyu
go test ./cmd/... ./internal/...                 # 25 packages green
REF=… OUR=… bash /tmp/agycmp/hotpath.sh          # 5 scenarios, no regression
bash /tmp/wide_compare.sh                        # 6 frames @ 200×60
```

100×30 hotpath diff summary (no regression vs cycle 155):

```
cold_start   000-init            diff= 33
ctrl_o_expand 000-init           diff= 46
              001-scrolled       diff= 52
              002-expanded       diff= 52
              003-collapsed      diff= 52
help_tabs     000-general        diff= 34
              001-second         diff= 18
              002-third          diff= 12
              003-wrapped        diff= 32
              004-closed         diff= 27
picker        000-slash          diff= 23
              001-down5          diff= 15
              002-closed         diff= 15
resume_scroll 000-init           diff= 43
              001-pgup           diff= 52
              002-bottom         diff= 52
              003-top            diff= 52
```

200×60 000-init captures: ref shows 39 lines of content, ours shows
43 — both fill the viewport (vs the pre-153 18-line ours). Tool/thought
bullet glyphs match ref byte-for-byte.

## Limitations

- The bullet title colour around the bullet (e.g. ref's
  `\x1b[1m\x1b[38;5;172m` bold orange on "Edited 1 file") is not yet
  applied — the tool title still draws in the default dim gray.
  Recovery is straightforward but needs a small palette addition
  (cycle 157+ work).
- The "Detail" bullet (U+23BF ⎿ inside an expanded tool call) was
  not changed by cycle 156 — its glyph and palette are still those
  recovered in cycle 127 and they appear unchanged in the ref
  capture.