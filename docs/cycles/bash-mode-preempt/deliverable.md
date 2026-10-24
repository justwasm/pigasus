# Bash mode: interruption, preemption and Ctrl+C

Cycle `bash-mode-preempt` — follow-up to `bash-mode`.

## The report

> 我发现连续输入 `!top` / `!pwd`，然后再按 ctrl+c 或者 esc 的行为，wagyu 和 agy
> 不一样，agy 还是会显示 top 的输出，而 wagyu 不会。

Reproduced exactly. The clone showed neither the `top` output nor any hint that a
command had been interrupted; the reference showed `top`'s dump and then `pwd`'s block.

## What the reference actually does

All captures are byte-level pty recordings (`captures.md`). **The installed `agy`
auto-updated to 1.4.5 during this session**; the clone still pins 1.4.4
(`internal/cli.ReferenceVersion`). Where the two differ it is called out.

### 1. A new shell command preempts a running one — it does not queue

`!sh -c 'echo first; sleep 30'`, then `!echo queued2` one second later, **without
pressing anything else**:

```text
○ ! sh -c 'echo first; sleep 30'
  ⎿  first

● ! echo queued2
  ⎿  queued2
```

The raw chunk that appears at the moment `!echo queued2` is submitted (t = 13.15 s,
the command still had 29 s of `sleep` left):

```text
\r\x1b[38;2;38;139;210;1m  ⎿  \x1b[38;2;88;110;117;22mfirst\x1b[m\r\n\n
\x1b[38;2;133;153;0m●\x1b[m \x1b[38;2;38;139;210;1m! echo queued2\x1b[m\r\n
\x1b[38;2;38;139;210;1m  ⎿  \x1b[38;2;88;110;117;22mqueued2\x1b[m
```

So:

- the running process tree is **stopped at submission time** (not at Esc);
- the output it had already produced is kept and rendered as its body, using the
  ordinary success styling (`⎿` blue bold, `#586E75;22` text);
- the interrupted block's head is **never repainted** — it keeps the hollow amber
  `○` (confirmed in the exit re-render: `\x1b[38;2;181;137;0m○\x1b[m … ! sh -c …`);
- the new command runs immediately.

Only a command submitted while a **model turn** streams queues (`▸ <cmd>`), which the
previous cycle already matched.

### 2. Esc and Ctrl+C cancel, and the block keeps no body

`!sh -c 'echo first; sleep 30'`, wait three seconds, then Esc (or Ctrl+C):

- the block is finalised as the head row alone — the `first` that had been produced is
  **not** shown;
- on 1.4.5 the interrupt bullet is **amber** `●` (`\x1b[38;2;181;137;0m`); the 1.4.4
  capture from the previous cycle showed **green** (`\x1b[38;2;133;153;0m`);
- Ctrl+C does both things on one press: cancel, and arm the two-step exit
  confirmation (`press ctrl+c again to exit`).

### 3. Long output is truncated, and ctrl+o expands it

`!seq 1 200` at 120×40:

```text
● ! seq 1 200
  ⎿  <output +180 lines>
181
…
200 (ctrl+o to expand)
```

Raw first row: `\x1b[38;2;38;139;210;1m  ⎿  \x1b[38;2;88;110;117;22m<output +180 lines>\x1b[m`,
then the **last 20** lines (continuation style, no second `⎿`) with
` ` `\x1b[38;2;88;110;117m(ctrl+o to expand)\x1b[m` appended to the final one.

The cap is **half the terminal height**, not a fixed 20: `!seq 1 40` shows 30 rows
(eliding 10) at 120×60 and 25 rows (eliding 15) at 120×50. A body of exactly 20 rows
at 40 is never truncated and shows no hint.

ctrl+o expands the block: the `<output +N lines>` marker disappears, the full body is
rendered and the hint becomes ` (ctrl+o to collapse)`. Measured on `!seq 1 200`
(the 1..165 rows are not repainted because the viewport scrolls to the tail, which is
also what the reference does).

## Why the clone diverged

Three independent bugs, all measured:

1. **`submitBashMode` queued unconditionally.** Any command submitted while
   `m.streaming` went to `bashQueue`, so `!pwd` waited behind `top` instead of
   preempting it. The queue only flushed on the first command's completion, i.e. after
   an Esc.
2. **`execBashCommand` discarded everything until exit.** It used
   `cmd.CombinedOutput()`, which returns only after `Wait`; on cancel it returned `""`.
   Nothing existed to keep even if the preemption had been implemented.
3. **Cancel killed only the shell.** `exec.CommandContext` signals the direct child
   (`zsh -c …`). A grandchild (`sh`, `sleep`, `top`) inherits the stdout pipe, so
   `Wait` blocked until that grandchild exited on its own — before the fix,
   `!sh -c 'echo first; sleep 30'` + Esc left the spinner running for the full **30 s**.
   `top` would have survived the cancel entirely.

Ctrl+C was a fourth, smaller one: it only armed the exit confirmation and never looked
at `bashCancel`.

## What changed

| File | Change |
|---|---|
| `cmd/wagyu/tui_bash_mode.go` | `bashOutput` (mutex-guarded sink, so a preemption can snapshot it); `bashDoneMsg.gen`; `execBashCommand` writes into the sink, sets `Setpgid` + a group-killing `Cancel` + `WaitDelay`; `finishBashCommand` drops the body when `cancelled`; new `preemptBashCommand`. |
| `cmd/wagyu/tui_bash_sysproc_unix.go` | `bashSetProcessGroup` / `bashKillProcessGroup` (`kill(-pgid, SIGKILL)`). |
| `cmd/wagyu/tui_bash_sysproc_windows.go` | Same two helpers, direct-child fallback. |
| `cmd/wagyu/tui_prompt_submit.go` | `submitBashMode`: preempt when `bashCancel != nil`, queue only for a model turn. |
| `cmd/wagyu/tui_run.go` | `bashRun *bashOutput`, `bashCmd string`, `bashGen int`. |
| `cmd/wagyu/tui_update_keys.go` | Ctrl+C cancels the running command before the exit-confirm logic. |
| `cmd/wagyu/tui_update_ticks.go` | `updateBashDoneMsg` ignores a stale generation. |
| `cmd/wagyu/tui_tools.go` | `turnBlock.bash *bashBlock` — a bash block joins the ctrl+o re-render path next to tool blocks. |
| `cmd/wagyu/tui_turn_blocks.go` | `rerenderBlocks` renders `bashBlockLines` for bash blocks. |

`preemptBashCommand` snapshots the sink, kills the tree, places the partial body as a
finished block whose head keeps the hollow amber `○` (the reference never repaints it),
and starts the next command. The old command's `bashDoneMsg` arrives later carrying the
previous generation and is dropped, so it cannot repaint or re-flush anything.

A finished block is a `bashBlock{cmd, body, failed, running}` registered in `m.blocks`,
so the existing ctrl+o path re-renders it from the full body: collapsed it shows
`  ⎿  <output +N lines>` + the last `height/2` rows + the expand hint, expanded it shows
everything + the collapse hint. The cap was measured at three heights (40/50/60 → 20/25/30).

## Verification

Byte-level pty, `agy` 1.4.5 vs the rebuilt clone, same script (`captures.md` has the
scripts and the raw dumps):

| Scenario | reference | clone after the fix |
|---|---|---|
| `!sh -c 'echo first; sleep 30'`, then `!echo queued2` | `○ …` + `⎿ first`, then `● ! echo queued2` + `⎿ queued2` | identical (head still `○`, body `first`, second block complete) |
| same, then Esc | head only, no body | head only, no body (green vs amber bullet — 1.4.4 vs 1.4.5) |
| same, then Ctrl+C | head only, no body, `press ctrl+c again to exit` | identical, plus the exit confirm |
| user's `!top`, `!pwd`, Esc | top dump as the block body, then `● ! pwd` + path | identical |
| `!seq 1 200` collapsed | `<output +180 lines>` + 181…200 + `(ctrl+o to expand)` | identical (same rows, same hint span) |
| `!seq 1 200` + ctrl+o | marker gone, full body, `(ctrl+o to collapse)` | identical |
| `!seq 1 40` at 120×50 / 60 | 25 / 30 kept rows (`+15` / `+10` lines) | cap is `height/2`, pinned by test |
| preempted `sh -c 'seq 1 100; sleep 30'` + `!echo x` | `<output +80 lines>` + 81…100 + hint | identical, and ctrl+o expands it |
| cancel latency | immediate | immediate; no orphan `sleep`/`top` (`pgrep` clean) |

Tests (`go test ./cmd/wagyu/ -run 'TestBash|TestExecBashCommand'`): green. New cases pin
preemption (body + untouched head + queue empty + `bashGen` bump), the stale-generation
guard, Ctrl+C (cancel **and** exit confirm), cancel dropping the body, and a bounded
cancel that asserts the whole call returns in ≤ 5 s with `first` still in the sink.

## 硬约束 (hard constraints)

- The preemption rule was measured on **agy 1.4.5**, not 1.4.4. The 1.4.4 captures only
  ever covered a *single* running command, so whether 1.4.4 preempted the same way is
  **not verified**. The clone implements the 1.4.5 behaviour because that is the binary
  on this machine and the one the report was written against.
- The interrupt bullet colour follows **1.4.4** (green). 1.4.5 paints it amber; the
  clone deliberately does not, because `internal/cli.ReferenceVersion` is 1.4.4 and
  that constant is pinned to `reference.lock.json`.
- The truncation cap is implemented as `height/2`, derived from three measurements
  (20/25/30 at 40/50/60). A different rule that happens to agree on those three values
  would not be caught.
- The clone's bash block is still not written to the conversation store (the
  reference's implicit trajectory) — unchanged from cycle `bash-mode`.

## 存疑 (open questions)

- Does the collapsed/expanded state survive a resize, or is the cap re-evaluated only
  when the block is (re-)rendered? The clone re-renders on ctrl+o using the *current*
  height; the reference was not probed after a resize.
- Does an expanded block stay expanded for blocks finished later in the same session?
  The clone renders new blocks with the current ctrl+o state; unmeasured upstream.
- Does preemption also apply when the new command arrives while a *model* turn and a
  shell command overlap (queued command flushed into a running shell command)? Not
  measured; the clone routes it through the same preempt branch.

## limitations

- Every capture in this cycle is from a pty driver that strips nothing: layout claims
  are byte-level, but the *visual* screen was not reconstructed through a terminal
  emulator (the same method as the previous bash-mode cycle).
- The reference auto-updated to 1.4.5 mid-session, so the clone is now one release
  behind the installed binary. `go test ./internal/referencedrift/` reports it; the lock
  was **not** resealed in this cycle.
- `internal/tools` and `cmd/wagyu/tui_viewport.go` were being edited by another agent
  during this cycle; the full `go test ./cmd/... ./internal/...` run was blocked by
  transient duplicate declarations in `tui_viewport.go`, not by these changes. The
  bash-mode package build and the focused test set are green.
