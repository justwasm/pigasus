# Cycle 123 — hot-path harness and first-person gap notes

Built `/tmp/agycmp/hotpath.sh`, a tmux-based A/B harness that drives both
binaries through five interactive scenarios, captures the pane after each
key step, and diffs them. The harness is the cycle's deliverable, and the
gap notes below are what running it surfaced.

## Harness

`/tmp/agycmp/hotpath.sh` runs the same key sequence against both binaries
and captures the rendered pane at each step. Captures live under
`/tmp/agycmp/hotpath/<scenario>/{ref,our,diff}.<step>.txt`. Default
scenarios:

| Scenario | What it drives |
|---|---|
| `cold_start` | open with no args, idle state |
| `resume_scroll` | `-c <conv>`, then 10× PageUp, then Ctrl+End, then Ctrl+Home |
| `ctrl_o_expand` | `-c <conv>`, scroll up to land on a tool block, Ctrl+O to expand, Ctrl+O to collapse |
| `help_tabs` | type `/help` Enter, then 3× Tab to cycle, then Escape |
| `picker` | type `/`, then 5× Down, then Escape |

Each scenario is runnable on its own: `bash hotpath.sh picker`. Env
overrides for `REF`, `OUR`, `OUT`, `CONV`, `COLS`, `ROWS`.

Diff summary printed at the end:

```
--- resume_scroll ---
  000-init              ref=  30 our=  30  diff=  59
  001-pgup              ref=  30 our=  30  diff=  57
  002-bottom            ref=  30 our=  30  diff=  59
  003-top               ref=  30 our=  30  diff=  59
```

…where `diff` is `wc -l` of `diff -u ref our`. The captures are full
ANSI-preserved panes, so the diff highlights visual differences.

## What I observed

The reference was quota-exhausted (`RESOURCE_EXHAUSTED`) for the entire
run, but TUI rendering is local and doesn't need the model — so A/B on
rendering is valid. Except where it isn't.

### Gap #1 — the trust prompt blocks 4 out of 5 scenarios

On **every** cold-start scenario (`cold_start`, `resume_scroll`,
`ctrl_o_expand`, `help_tabs`, `picker`) the reference opens with this
overlay:

```
Accessing workspace:

/tmp/agycmp

Do you trust the contents of this project?

Antigravity CLI requires permission to read, edit, and execute files here.

> Yes, I trust this folder
  No, exit

  ↑/↓ Navigate · enter Confirm
                                                             Gemini 3.8 Flash · high
```

The clone **doesn't have this overlay**. It opens straight to the prompt.
This is the **largest user-facing fidelity gap** in the current cycle:
the first thing a real user sees in the reference is a workspace-trust
dialog, and the clone silently skips it.

This is on the cycle-123 backlog (`trustedWorkspaces` write on startup)
and was already known, but the harness makes the consequence concrete: **we
can't A/B /help, picker, or ctrl+O with the reference at all until we
add it**, because the reference never gets past the overlay.

This is the next thing to fix. Implementing it unblocks A/B on every
other TUI scenario.

### Gap #2 — picker Down navigation is broken

In `picker/our.000-slash.txt` and `picker/our.001-down5.txt` the picker
shows the same five entries with `/add-dir` still highlighted after five
`Down` keypresses. The picker banner says `↑/↓ Navigate · enter Select ·
tab Complete`, so the contract is documented; the handler isn't matching.
Likely cause: the bubbles textarea steals the Down key when the picker is
visible, or the picker's key handler ignores Down (handles only j/k or
the picker model isn't focused).

This is reproducible from the harness: a fresh TUI, `/`, five Downs, the
cursor doesn't move.

### Gap #3 — /help Commands tab shows MCP skills

`help_tabs/our.001-second.txt` lists ~30 commands that look like MCP
skills: `/science:literature-search-arxiv`, `/science:ncbi-sequence-fetch`,
`/terminal-browser`, `/web-perf`, `/workers-best-practices`, `/wrangler`,
etc. — these match the entries in AGENTS.md's `available_skills` list.
The Commands tab in the reference cannot be captured (trust prompt), so
this is **【推测】**: the reference likely shows the same list, since the
recovered `cliconfig.Skills` registry is loaded from the same source. But
unverified until trust is implemented.

The Commands tab in the clone mixes native slash commands (those would be
on the picker) with skill commands (these) — so the picker and the
Commands tab have **non-overlapping lists**. That matches the recovered
split between `cliconfig.Command` (native) and `cliconfig.Skill` (MCP),
but again cannot be A/B-confirmed.

### Gap #4 — /help Shortcuts tab has binding entries

`help_tabs/our.002-third.txt` lists shortcuts with descriptions:

| Binding | Description |
|---|---|
| ctrl+k | Approve subagent fast |
| ctrl+r | Review artifact |
| f5 | Start voice dictation |
| shift+tab | Cycle mode |

These descriptions don't appear anywhere in the clone's source — they
must come from the cliconfig binding table the clone reads but doesn't
expose to its own UI yet. **The clone's own `/keybindings` is a
cycle-77 stub** (the picker shows it but it just prints "not
implemented"). The reference's `/keybindings` is **what the Shortcuts tab
shows**, i.e. the binding descriptions above. So cycle-77's `/keybindings`
is the same thing as the Shortcuts tab — implementing one is implementing
the other.

### Gap #5 — Ctrl+O expand renders correctly

`ctrl_o_expand/our.002-expanded.txt` shows a tool step expanded with a
code block intact — the wrap, the indentation, the truncation symbol
(`│`) all present. So the cycle-78 expand toggle works on a resumed
transcript; the harness just can't A/B it because of Gap #1.

### What works (clone ↔ clone, with cycle 122 fix)

- **Resume + scroll** (`resume_scroll/our.001-pgup.txt`) — scroll is
  instant, scrollbar thumb tracks proportionally, Ctrl+End returns to the
  latest content. The cycle-122 fix is end-to-end working in tmux.
- **Cold-start banner** (`cold_start/our.000-init.txt`) — banner, warning
  block (MiniMax-M3 → Gemini 3.8 Flash), prompt, status row all present
  and correctly styled. Differs from the reference only because the
  reference is in the trust overlay.
- **Prompt + status row** are byte-similar to the pre-cycle-122 captures.

## Hard constraints / 存疑

- **No live reference model turns.** Quota is exhausted; every A/B
  above is render-only. Behavioural differences under load (during
  streaming, during a long-running tool call, etc.) are not captured.

- **No captured reference render past the trust overlay.** Until Gap #1
  is fixed, every visual comparison is clone-vs-trust-prompt rather
  than clone-vs-reference-TUI. The harness IS the gap-solver here: once
  the clone implements the trust prompt, the same five scenarios can
  re-run and produce real A/B data.

- **Picker Down navigation** is observed-broken in the clone but I
  haven't confirmed the reference picker's Down behaviour. It might be
  that the reference's picker uses j/k, or that the reference is in a
  different mode (e.g., inline vs alt-screen) where Down goes to the
  terminal instead of the picker. Need the trust prompt to be
  resolved on the reference to confirm.

- **Picker vs Commands-tab overlap** in Gap #3 is an inference, not a
  measurement.

## Recommended next cycle (124)

Implement **the trust prompt** as cycle 124. Concretely:

1. Add `trustedWorkspaces` map (path → bool) to the model, persisted to
   `~/.gemini/antigravity-cli/trusted_workspaces.json` (the path the
   reference writes — recovered from pclntab `store.(*Manager)
   .LoadTrustedWorkspaces` / `SaveTrustedWorkspaces`).
2. On startup, compare `cwd` to the map. If absent, show the picker
   overlay (`Yes, I trust this folder` / `No, exit`). If present and
   trusted, skip.
3. On `Yes`, write the entry and proceed.
4. On `No`, exit (matches the reference).
5. Add a CLI flag or env var to pre-trust a path for tests
   (`OPENAGY_TRUST=/tmp/agycmp` or `--trust-cwd`).

That unblocks A/B on every other scenario in the harness. **Then**
re-run `/tmp/agycmp/hotpath.sh` and the diffs become meaningful.

Also from the cycle-123 backlog:

- Picker Down navigation fix (Gap #2) — small, can be in the same cycle.
- Real `/resume` (replace the cycle-77 stub) — most-user-visible
  non-trivial item, second priority.

## Files

- `/tmp/agycmp/hotpath.sh` — the harness.
- `/tmp/agycmp/hotpath/<scenario>/*.txt` — captures + diffs.
- This document.
