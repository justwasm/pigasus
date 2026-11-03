# The status line's payload members and the built-in row's status cells

Closes **B-1**, **B-2**, **B-3**, **B-4** and **B-5** of
`slice-b-tui-surfaces.md`.

Branch: `fix/b15`.

The five findings share one surface: the status line. Three of them are
about the **document** the CLI pipes to a custom status-line script
(`internal/ui/views/statusline`), two are about the **built-in row**
the TUI draws under the prompt (`internal/ui/views/statusrow`). The
document and the row are two owners of the same facts, so the facts
were given one owner each and both surfaces read them.

---

## 1. The payload carries `vim` (B-1)

`Payload.Vim` existed and nothing ever assigned it, so a script could
never read `.vim.mode`. The payload now carries it, and the **mode
names are the reference's own**: `NORMAL`, `INSERT`, `VISUAL`,
`VISUAL LINE` (`official-docs/cli/statusline.md:79`) — not the engine's
badge spelling (`-- INSERT --`).

The member is present **only when vim editor mode is enabled**, which
is the doc's own rule. The host therefore asks two questions: is vim
the editor mode (`m.vimEnabled()`), and what is the engine's mode
(`m.vimMode()`). The engine is **not built** when vim editor mode is
off — the host keeps its "no engine without vim" invariant — and
`statusline.VimModeName` maps the engine's mode to the payload's name.

## 2. The payload's four empty members (B-2)

Two of the four now carry the session's facts, and two stay empty.

| member | source | status |
|---|---|---|
| `pending_input_count` | the queued user messages | **populated** |
| `subagents` | the conversation's subagent registry | **populated** |
| `artifact_count` | — | **empty** (存疑) |
| `tool_confirmation_pending` | — | **empty** (存疑) |

**`pending_input_count`** counts the **user messages** the session has
queued: the streaming queue (`m.store.StreamQueue`) and the queue
mode's own next prompt (`m.store.QueuedPrompt`). The bash queue is
**not** counted — those are shell commands, not messages — even though
the row's hint counts them.

**`subagents`** is the registry's own list, mapped into the payload's
entry: the **name** is the type the child was launched as, the **role**
is the role the call asked for (falling back to the type's name, the
reference's own rule for its list entry), and the **status** is the
payload's two-value vocabulary (`running` / `completed`). The registry
knows more states (`canceling`, `errored`, `stopped`); the payload's
contract has no room for them, and the mapping is recorded in the
package that owns the contract.

**`artifact_count`** has no source: the clone's `/artifact` panel
answers *"No artifacts"* (`internal/cliapp/slash_commands_handlers.go:669`),
so there is no counter to read. **`tool_confirmation_pending`** has no
source either: the host's `pendingPermission` is a **test-only seam**
(`internal/cliapp/tui_run.go:510`), not a live fact. Both members stay
empty, and both are recorded in **存疑** below.

## 3. `agent_state` reaches four of the seven documented states (B-3)

The payload's `agent_state` member reported only `idle` and `working`.
It now reaches four of the seven names the documentation lists
(`official-docs/cli/statusline.md:67`):

| state | when |
|---|---|
| `idle` | between turns |
| `working` | a turn is streaming |
| `tool_use` | a tool call is in flight inside the running turn |
| `error` | the last turn ended in an error |

The precedence is the reference's: the tool call inside a running turn
is the more precise state, and a failed turn is the state the session
rests in until the next turn starts.

**Three states stay unreachable.** The clone has no start-up phase it
renders a status line for, no separate sign-in phase, and no pending
review — so `initializing`, `authenticating` and `reviewing` are never
reported. They are recorded in **存疑**.

The vocabulary is the payload package's (`statusline.StateIdle`,
`StateWorking`, `StateToolUse`, `StateError`) and the **facts** are the
host's: a streaming turn, the active tool, the recorded turn outcome.
The host's `agentState()` reads the package's `AgentState()`, so the
document and the row cannot disagree about the state.

## 4. The built-in row carries the status cells (B-4)

The row's right half now ends with three **status cells**:

```
auto · Gemini 3.8 Flash · high · working · 12.3% · AI Credits: 42
```

- the **agent state** — omitted while the session is idle, because the
  row's default needs no label;
- the **context window's usage** — `%.1f%%`, the format the clone's own
  context panel uses (`internal/ui/views/contextpanel/contextpanel.go:123`)
  and the shape the documentation's screenshot shows (`1.0%`);
- the **remaining AI credits** — see §5.

**Every cell is omitted when its fact is absent.** A cell without a
fact would be a guess, and the row's bytes are pinned.

**The documentation's rich line describes the example script, not the
built-in row.** `official-docs/cli/statusline.md:148` describes the
*downloadable* `statusline.sh` example ("This script shows the agent
state, the model, a context window usage bar, and counts of artifacts,
subagents, and background tasks"). The reference's own **built-in** row
carries the mode, the model and the effort tier and nothing else
(【一手】 agy 1.4.7's own status row: `? for shortcuts … auto · Gemini
3.8 Flash · high`, `out/changelog-147/ref/seasonal-prompt.txt`). The
cells above are therefore the **clone's own** reading of the
documentation, and the payload carries everything a script needs.

## 5. The AI-credits indicator (B-5)

The documentation states the indicator's own shape:

> "**Statusline indicator**: The right side of the CLI statusline
> displays your remaining credit count (for example, `AI Credits: 42`)."
> — `official-docs/cli/credits.md:11`

and the reference binary carries the matching format string:
【strings】 `"AI Credits: %d"` in agy 1.4.12's string table
(`~/.local/bin/agy`), together with the release note that introduced it
(【strings】 1.0.3: *"a real-time display of remaining credits in the
status bar"*).

The row renders the cell — `AI Credits: 42` — and the **host leaves the
balance unknown**: the clone has **no G1 credit balance** in the TUI.
The only reader of the balance is the print mode's on-demand
`/credits` (`internal/printmode/print_slash_credits.go`), and the
tier-2 account state carries the plan tier, not an amount
(`internal/features/auth/code_assist_state.go`). The cell is therefore
implemented and pinned, but it does not appear yet; the plumbing a
follow-up needs is recorded in **存疑**.

The documentation's second sentence — the low-quota highlight — is
**not** implemented: the reference's warning threshold is not recovered,
and a threshold the clone invents would be a made-up fact.

---

## The bytes

The status row's bytes were pinned by an **out-of-tree** two-tree dump
(cycle 381, tranche 46: `docs/cycles/cycle-381-cliapp-split/deliverable.md:817`).
That dump compared the host's rendering before and after the row's move
and lived **outside the tree**, so nothing in the suite failed when the
row changed.

The pin is now **in the tree**: `internal/ui/views/statusrow/testdata/statusrow.golden`,
rendered from the fixture set in `internal/ui/views/statusrow/golden_test.go`.
The set covers the same ground as the old dump — the idle row, a narrow
terminal, an unknown width, a streaming turn, the bash mode, a queued
prompt, a non-empty composer, the exit confirmation, the four modes, the
`--agent`, the `--agent` with the queue mode, the queue mode alone, a
running task, the two open panels, the `/help` overlay, the two picker
states, the three compaction shapes, the vim badge, the vim badge with a
buffer and the static non-tty row — and it adds the three status cells.

Two of the old dump's fixtures were **host-level** facts that the row
cannot tell apart: a queued stream prompt and a queued shell command are
both `Queued > 0` at the row's level, so they are one fixture here. The
other twenty-two are the row's own.

**One cell is masked**: the running compaction's elapsed time, because
it is read off the wall clock. The finished compaction's own elapsed
time is a fixture and is not masked.

**The two reference asymmetries are preserved.**

- The right-padding count **excludes** `" · Queue mode"` in the
  built-in case and **includes** it in the `--agent` case.
- The static non-tty row keeps its own literal grey
  (`38;2;154;160;166`) rather than the palette's.

**What changed in the row's bytes**: the status cells, and nothing
else. Every fixture that carried no new fact is byte for byte what it
was — the cells appear only in the three new fixtures — while the
**host** now fills the agent state and the context percentage, so a
live streaming session shows `· working` and a session with prompt
tokens shows `· 12.3%`.

## The confined files

The tranche was scoped to the status line's own packages. Two files
outside that scope were touched, and both are recorded here:

1. **`internal/cliapp/tui_update_stream.go`** — the failed turn's
   outcome. `recordTurnCompletion` recorded `tea.ProgramStateDone` for
   **every** turn, so the host had no way to know that a turn had
   failed. It now records `tea.ProgramStateError` for a failed turn
   (and `Done` for a turn the user cancelled with Esc, which is not a
   failure). Without this the `error` state is unreachable.

   **Knock-on effect**: the OSC 7501 report to the terminal reads the
   same recorded outcome (`internal/cliapp/tui_view.go:355`), so the
   report now says **Error** after a failed turn where it said **Done**
   before. That is the more truthful answer — the Program Status
   Protocol has an error state for exactly this — but it is a change
   in a surface this tranche did not target.

2. **`internal/cliapp/statusline_payload_test.go`** and the new test
   files beside the packages — the tests that pin the new facts.

## Behaviour changes

- The status-line **payload** now carries `vim`, `pending_input_count`
  and `subagents`; a script that reads them by name gets real values.
- The payload's `agent_state` now reports `tool_use` and `error`
  besides `idle` and `working`.
- The **built-in row** shows the agent state, the context window's
  usage and — once the host has a balance — the remaining AI credits.
- A **failed turn** is recorded as an error outcome, so the terminal's
  program status says `Error` instead of `Done`.

## 硬约束

- **硬约束**: the `vim` member is present **only when vim editor mode
  is enabled**, and the **engine is never built** when it is off.
- **硬约束**: the agent state's vocabulary is the payload package's.
  The host does not spell `idle` / `working` / `tool_use` / `error` in
  its own literals.
- **硬约束**: every status cell is **omitted when its fact is
  absent**. A cell without a fact would be a guess.
- **硬约束**: the context percentage has **one owner**
  (`statusline.ContextUsedPercent`); the payload's `used_percentage`
  and the row's cell are the same number.
- **硬约束**: the reference's two asymmetries — the queue mode's
  suffix excluded from the built-in padding count and included in the
  `--agent` one, and the static row's literal grey — are **preserved**.
- **硬约束**: the row's bytes are pinned **in the tree**
  (`internal/ui/views/statusrow/testdata/statusrow.golden`).

## 存疑

- **存疑**: **the AI-credits cell never appears yet.** The row renders
  it and the golden pins it, but the host has no balance: the only
  reader of the G1 balance is the print mode's `/credits`
  (`internal/printmode/print_slash_credits.go`), and the tier-2
  account state carries the plan tier, not an amount. A follow-up
  needs three things: a tier-2 reader for the balance (the print
  mode's reader is above tier 5 and may not be imported by the host),
  a cached per-account fact the TUI can read, and a refresh command
  (the reference refreshes the balance "less often" — 【strings】 1.4.x
  release notes).
- **存疑**: **the low-quota highlight is not implemented.** The
  documentation says the indicator highlights when the balance drops
  below a warning threshold; the threshold itself is not recovered, and
  an invented threshold would be a made-up fact.
- **存疑**: **`artifact_count` stays empty.** The clone has no artifact
  counter — its `/artifact` panel answers *"No artifacts"*.
- **存疑**: **`tool_confirmation_pending` stays empty.** The host's
  `pendingPermission` is a test-only seam, not a live fact.
- **存疑**: **`reviewing`, `authenticating` and `initializing` are
  unreachable.** The clone has no pending-review state, no separate
  sign-in phase and no start-up phase it renders a status line for. The
  reference's own "in-progress status line" work is about the **hint**
  area — 【strings】 *"Improved the in-progress status line to show what
  the agent is waiting on, such as initializing MCP servers or retrying
  an unavailable model"* — not about a right-segment state cell.
- **存疑**: **the status cells' placement is the clone's own.** The
  reference's built-in row carries no cell at all (【一手】 agy 1.4.7),
  and the documentation's rich line describes the *example script*.
  The clone puts the cells at the end of the right segment, after the
  queue mode's marker.

## limitations

- **limitations**: **the survey was a source reading, and so is this
  fix.** No claim here rests on running the reference binary. The
  evidence is the documentation, the clone's own 【一手】 1.4.7 capture
  of the built-in row, and the 1.4.12 string table read off the live
  `~/.local/bin/agy`.
- **limitations**: **the string-table evidence is a string table.**
  `"AI Credits: %d"` proves the format string exists; it does not
  prove where the reference draws it. The documentation says the right
  side of the status line, and the clone follows the documentation.
- **limitations**: **the row's cells are not the reference's.** They
  are the clone's reading of a documentation sentence that describes
  the example script. The reference's built-in row is unchanged by
  this tranche and is still pinned by its own capture.
- **limitations**: **the OSC 7501 report's change is a side effect.**
  The failed turn's outcome is one fact with two readers, and the
  report now says `Error` after a failed turn. It was not re-verified
  against the reference's own program-status behaviour.
- **limitations**: **the payload's `subagents` status vocabulary is
  lossy.** The registry's `canceling` / `errored` / `stopped` states
  are folded into `completed`, because the payload's contract has two
  values.
