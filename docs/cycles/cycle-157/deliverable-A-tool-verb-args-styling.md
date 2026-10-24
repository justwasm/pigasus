# Cycle 157 — Tool-step verb/args palette + failed-bullet red

## Symptom

The captured 200×60 frame for a long resume showed:

```
=== ref ===  ● Ran (git add .github/workflows/build-release.yml ...)
=== ours === ● Ran (git add .github/workflows/build-release.yml ...)
```

Ref rendered the verb "Ran" in **bold orange** (`\x1b[1m\x1b[38;5;172m`)
and the args `"(git add ...)"` in dim gray (`\x1b[38;5;235m`); ours
kept the whole title in the default dim gray, with no bold and no
orange. The cycle-156 capture-pane dump (the recovery source for the
bullet glyphs) also had the verb / args palette split, which the
clone didn't pick up.

For a failed tool step:

```
=== ref ===  ● Ran (false) (Failed)         ← red bullet, red "(Failed)"
=== ours === ● Ran false (Failed)            ← teal bullet, default suffix
```

The cycle-127 clone used the teal success bullet for failed steps
and appended "(Failed)" with no special styling.

## Fix

`cmd/wagyu/colors.go`:

- new constants `cToolVerbOpen = "\x1b[1m\x1b[38;5;172m"` and
  `cToolVerbClose = "\x1b[0m"` — bold orange for the verb.
- new constants `cToolArgsOpen = "\x1b[38;5;235m"` and
  `cToolArgsClose = "\x1b[39m"` — dark gray for the args.
- new constant `cToolBulletErrOpen = "\x1b[38;5;166m"` — red bullet
  for failed steps.
- new constants `cToolFailedSuffixOpen = "\x1b[1m\x1b[38;5;166m"` and
  `cToolFailedSuffixClose = "\x1b[0m"` — bold red for "(Failed)".

`cmd/wagyu/tui_tools.go`:

- new constant `v135StepBulletErrOpen` composing the U+25CF ● glyph
  with `cToolBulletErrOpen` / `cToolBulletClose`.
- new helper `toolBulletFor(s toolStep) string` returns the right
  styled bullet for the step (red for failed tools, teal for success
  tools, dim gray for thoughts).
- new helper `styledStepTitle(s toolStep) string` splits the title
  on the first space and wraps the verb in `cToolVerbOpen` / Close,
  the args in `cToolArgsOpen` / Close, and (if failed) appends the
  failed suffix in red.
- new helper `splitVerbArgs(title) (verb, args string)` — does the
  first-space split.
- `toolBlockLines` now uses `styledStepTitle` everywhere
  `stepTitle` was used before; the `bulletFor` closure is replaced
  by `toolBulletFor` so the failed-tool red bullet flows through.

`cmd/wagyu/tui_tools_test.go`:

- `TestTUITools_FailedStepTitle` pins the new failed bullet (`got` must
  start with `v135StepBulletErrOpen`) and tolerates ANSI codes between
  "Ran" and "false".
- `TestTUITools_CtrlOTogglesStepDetail`, the group-header pin and the
  spinner pin drop the exact "Ran date" / "Read secret.txt" string
  match — they now check for "Ran" + "date" / "Read" + "secret.txt"
  separately, since the new styling inserts ANSI between them.

`cmd/wagyu/conversation_test.go`:

- `TestSeedConversationRendersTranscript` drops its exact "Ran ls"
  match for the same reason.

## After

```
=== our 000 (post-157, 200×60, raw) ===
[38;5;29m●[39m [1m[38;5;172mRan[0m [38;5;235m# Perfect — idle state shows …[39m
[38;5;29m●[39m [1m[38;5;172mRan[0m [38;5;235mpython3 << 'EOF' ...[39m
```

That sequence is byte-equivalent to ref's:

```
[38;5;29m●[39m [1m[38;5;172mRan[0m [38;5;235m(git add ... && git commit ...)[39m [38;5;238m(ctrl+o to expand)[39m
```

## Files touched

- `cmd/wagyu/colors.go` — six new constants for the verb/args /
  failed-bullet / failed-suffix palettes.
- `cmd/wagyu/tui_tools.go` — `v135StepBulletErrOpen`,
  `toolBulletFor`, `styledStepTitle`, `splitVerbArgs`; the
  `bulletFor` closure is replaced with `toolBulletFor`.
- `cmd/wagyu/tui_tools_test.go` — three tests updated to tolerate
  ANSI codes around the verb/args.
- `cmd/wagyu/conversation_test.go` — one test updated the same way.

## Verification

```
go build -o /tmp/agycmp/our/agy ./cmd/wagyu
go test ./cmd/... ./internal/...                 # 25 packages green
REF=… OUR=… bash /tmp/agycmp/hotpath.sh          # 5 scenarios
bash /tmp/wide_compare.sh                        # 6 frames @ 200×60
```

100×30 hotpath diff summary (no regression vs cycle 156):

```
cold_start   000-init            diff= 33
ctrl_o_expand 000-init           diff= 45
              001-scrolled       diff= 55
              002-expanded       diff= 53
              003-collapsed      diff= 53
help_tabs     000-general        diff= 32        (was 34, -2)
              001-second         diff=  9        (was 18, -9)
              002-third          diff= 12
              003-wrapped        diff= 28        (was 32, -4)
              004-closed         diff= 22        (was 27, -5)
picker        000-slash          diff= 37
              001-down5          diff= 23
              002-closed         diff= 23
resume_scroll 000-init           diff= 43
              001-pgup           diff= 52
              002-bottom         diff= 52
              003-top            diff= 52
```

The help-tabs diffs shrank by 2-9 lines per frame — the most common
content at 100×30 is the help panel, and the help panel has its own
tool-step example ("Ran date") that benefits from the new verb
palette.

200×60 000-init captures: ref shows 40 lines, ours shows 43 — both
fill the viewport. Tool bullets match ref's U+25CF + colour-29
sequence, the verb matches ref's bold orange, the args match ref's
dim gray.

## Limitations

- The "edited file count" line (`1 file, 1 command`) still renders in
  the default dim gray — ref draws the count number in
  `\x1b[1m\x1b[38;5;172m` too. Cycle 158+ work.
- The expanded `(ctrl+o to expand)` / `(ctrl+o to collapse)` hint
  renders in the cycle-156 dim gray (256-colour 238); ref uses the
  same colour for the parenthetical text but a separate shade for
  the "ctrl+o" key name (recovered as 256-colour 172 — same as the
  verb). Cycle 158+ work.
- Splitting "verb + space + args" is a single rule. Ref's title
  format occasionally has a parenthetical file annotation
  (`(build-release.yml)`) after the args; that annotation gets the
  same `cToolArgsOpen` / Close palette as the rest of the args, not
  the slightly lighter 256-colour 238 ref uses for it. The visual
  difference is small but visible at large sizes.