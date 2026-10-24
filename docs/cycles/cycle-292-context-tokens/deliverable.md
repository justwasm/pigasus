# Cycle 292 — `/context` accounting from the reference, and slash commands during a live turn

Status: implemented, verified live against the installed **agy 1.4.4**, tests green.
Scope: `cmd/wagyu` and the wire layer in `internal/backend`.

Two things were wrong at the start of this cycle:

1. `/context` printed a uniform estimate (`len/3`) for every category, so the rows
   never agreed with the reference beyond the first decimal of the total.
2. Slash commands that rewrite the transcript were held in `streamQueue` during a
   live turn as a *safety policy*, with no evidence that the reference did that.

Both are now derived from the reference. The second turned out to be the opposite
of the policy: 1.4.3 and 1.4.4 run those commands immediately.

---

## 1. The wire envelope (what the user row is counted from)

The reference does not send the raw prompt. Every user turn goes on the wire as
【一手】 (captured request bodies, `~/.openagy/captures/`, and re-captured from
1.4.4 for this cycle):

```
<USER_REQUEST>
{raw prompt}
</USER_REQUEST>
<ADDITIONAL_METADATA>
The current local time is: {RFC3339}.
</ADDITIONAL_METADATA>
```

and, when the model selection changed, a trailing block:

```
<USER_SETTINGS_CHANGE>
The user changed setting `Model Selection` from None to {display label}. No need to comment on this change if the user doesn't ask about it. If reporting what model you are, please use a human readable name instead of the exact string.
</USER_SETTINGS_CHANGE>
```

Measured byte counts 【一手】: `read file` (9 B) with the settings block = **432 B**;
without it = **131 B**. The clone now substitutes this text in `streamPrompt`
(`backend.Message.WireContent` → `WireText()`), while the transcript and the
conversation store keep the raw prompt.

`internal/backend/userwire.go` builds the envelope (`WrapUserContent`) and the two
counters. `userwire_test.go` pins the envelope byte-for-byte against the captured
bodies and pins the formula cases.

## 2. The token accounting

Recovered by decompiling 1.4.3's `chatTokensByCategory` / `stepTokens` (Ghidra, the
1.4.3 project; the addresses shift every release, so the decompiles are the evidence,
not the addresses), then re-measured live against 1.4.4.

| Row | Rule | 1.4.4 evidence 【一手】 |
|---|---|---|
| User messages | `len(wire)/3 + 6` | envelope 445 B → 154; 434 B → 150 |
| Agent responses | `sessionCompletionTokens − lastTurnCompletionTokens`, floored at 0 | first text turn 0; second turn (after a tool call) 12 |
| Tool calls | `(len(raw result) + 1)/3 + 6` per call | `read_file` output 109 B → 42; `run_command` output 127 B → 48 |
| System prompt | `(len(full prompt) − skills − subagents)/3` | 3.4k (reference), 3.3k (clone — see §6) |
| System tools | the JSON declaration array the turn sends, /3 | reference 4.6k, clone 4.3k (see §6) |
| Skills | complete `<skills>` section, tags + newline included | 7.4k both |
| Subagents | complete `<subagents>` section | 686 reference / 684 clone |
| Free space | `1.0M` label, percentage only varies | 100.0% … 98.4% |

The "agent responses" lag is not a bug: the reference subtracts the step that is
still landing, so a completed text-only turn reports 0. This is why the completed
panel could show `Agent responses: 0` while the transcript had a full reply.

`promptSectionTokenCount` includes the section's own tags and the newline after
them (`(len(body) + 2*len(name) + 2 + 1)/3`). The `+1` is load-bearing for
`<subagents>`: a 2,083-byte body is 701 tokens on its own, and the reference prints
702.

`/context` counts the prompt the *next* request would send by rebuilding
`agyprompt.Build(m.promptOptions())`, so `<user_rules>`, discovered skills and
custom subagents all move the rows.

## 3. The streaming layout is a live estimate, not the stored numbers

1.4.4 while a turn is in flight 【一手】 (slow mock stream, 120×40):

```
└ Context Usage
◉ ◉ □ □ …                    Gemini 3.8 Flash (Medium) · 5/1.0M tokens
□ □ □ □ …                    (0.0%)
□ □ □ □ …                    Estimated usage (awaiting generation)
□ □ □ □ …                    ◉ User messages: 2 tokens (0.0%)
□ □ □ □ …                    ◉ Agent responses: 3 tokens (0.0%)
□ □ □ □ …                    ◉ Tool calls: 0 tokens (0.0%)
□ □ □ □ …                    □ Free space: 1.0M (100.0%)
```

The prompt was `hello world` (11 B → 2 tokens) and the partial reply was
"The lighthouse" (14 B → 3). Later in the same stream the rows read 2 / 28 / 0 / 30
with a 113-byte partial reply. That is `EstimateTokenCounter`'s fallback,
`max(len/4, 1)` — a step with no stored count yet is estimated at four bytes per
token, *not* the three the completed rows use and not the wire envelope.

`streamingTokenCounts` reproduces it: completed steps keep their stored counts, the
last user message is counted at `max(len(raw)/4, 1)`, and the in-flight assistant
text at `len(liveReplyBuf)/4` on top of the completed steps.

## 4. Slash commands during a live turn

Measured on 1.4.4 with a slow mock stream, one command per session 【一手】:

| Command | Reference while streaming | Clone now |
|---|---|---|
| `/clear` | aborts the turn, clears to the banner | same (`abortTurnForLocalCommand`, `discardTurn`, counters reset) |
| `/new` | aborts the turn, clears to the banner | same |
| `/compact` | turn keeps streaming; `Nothing to compact yet.` appears **in the input row** (blue `>`, gray body) | same (`inputNotice`, rendered as the empty input's placeholder) |
| `/fork` | aborts, keeps the partial reply, prints the fork rows | same (`abortTurnForLocalCommand` → fork) |
| `/branch` | aborts during streaming | aborts; the branch itself keeps the clone's pre-existing isolated-sandbox deviation |
| `/rewind` | panel opens immediately, spinner keeps running | same |
| `/resume` | panel opens immediately | same |
| `/share`, `/quote` | not commands; the picker fuzzy-matches a skill | unchanged |

Commands that start a model turn (`/plan`, `/goal`, `/learn`, `/teamwork`,
`/boost`, `/grill-me`, skills, `continue`) still queue and run on turn end —
`slashQueuesWhileStreaming` is now exactly that set.

The `/compact` notice needed a renderer change: the placeholder is re-derived every
frame (`viewBody`), so `m.inputNotice` is a field that outranks the dismissed mode
cue and is cleared by the next submit or turn end.

## 5. Reference drift: 1.4.4

`agy` auto-updated 1.4.3 → 1.4.4 during this work. What moved, and what was done:

- `agy changelog` on 1.4.4 still starts at `1.4.3:` — the binary ships one release
  behind, so 1.4.4 documents no changes of its own. `internal/cli/testdata/changelog_reference.txt`
  was re-captured from the 1.4.4 binary: 165,586 B, 960 lines, 74 sections,
  813 bullets, sha256 `8804d34c…`.
- The changelog viewer's counter denominator was a hardcoded 1962 (the 1.4.2-era
  fixture). Live `/changelog` at 120×40 on 1.4.4 reads `(1–36 of 2014 lines)`;
  `changelogTotalLines` is now 2014.
- `reference.lock.json` keeps `version: 1.4.3` (the decompiled evidence was read
  out of the 1.4.3 binary) and sets `lag_acknowledged_through: 1.4.4` with a
  `lag_note` naming the surfaces re-verified live. Digests resealed.
- Changelog tests were generalised: the newest section is no longer assumed to be
  "pin minus one" by string, and the top bullet is checked through
  `referencedrift.CompareVersions`.

A side effect worth recording: `currentEffort` now falls back to the model label's
tier when no `--effort` is given. With a `settings.json` holding
"Gemini 3.8 Flash (Medium)" the reference's status row reads `· medium`; the clone
printed `· high` while the banner said Medium.

## 6. Live comparison (1.4.4, 120×40, cwd `/tmp`, no user rules)

Completed text turn (`hello world`):

| Row | Reference | Clone |
|---|---|---|
| Title | `16.2k/1.0M tokens`, `(1.5%)` | `15.9k/1.0M`, `(1.6%)` |
| User messages | 150 | **150** |
| Agent responses | 0 | **0** |
| Tool calls | 0 | **0** |
| System prompt | 3.4k | 3.3k |
| System tools | 4.6k | 4.3k |
| Skills | 7.4k | **7.4k** |
| Subagents | 686 | 684 |
| Free space | 1.0M | **1.0M** |

Tool turn (`read /tmp/tooltest.txt`):

| Row | Reference (read_file) | Clone (run_command) |
|---|---|---|
| User messages | 154 | **154** |
| Agent responses | 12 | **12** |
| Tool calls | 42 (109 B result) | 48 (127 B result) — same formula |

The two remaining gaps are not accounting errors:

- **System prompt 3.3k vs 3.4k.** The clone's assembled prompt is ~0.6 KB shorter
  than the reference's wire `systemInstruction`. That is a prompt-content gap from
  the system-prompt cycle, not a counting difference; the row subtracts the same
  sections.
- **System tools 4.3k vs 4.6k.** Two causes. (a) The reference's 12 declarations
  are 13,449 B on the wire and it prints 4.6k, while 13,449/3 = 4,483 → the tools
  row looks tokenizer-based, not `len/3`; the clone has no tokenizer and uses the
  same `/3` as every other row. (b) The surfaces differ: 1.4.4's default agent
  advertises **12** tools (`read_file`, `run_command`, …); the clone still sends
  **60** (13,005 B), its cycle-98/275 catalogue. The row is now honest about the
  clone's own wire — before this cycle it measured `m.tools` only and printed 3.6k.

## 7. A live parity finding outside this cycle's scope

Because the surfaces differ, the reference's own tool calls do not work in the
clone 【一手】:

- `read_file` → the loop answers `Tool read_file not found` (the clone has
  `view_file`); the model gets an error result and no tool block renders, so the
  tool row stays 0.
- `view_file` / `run_command` → the clone's reconstructed schemas require
  `toolSummary`/`toolAction` (and `Cwd`), so a reference-shaped call fails
  validation before the runner: `Validation failed for tool "view_file": - toolSummary:
  must have required property…`.

With the clone's own argument shape the block renders and the row counts correctly
(`● Ran (echo hello)`, `Tool calls: 48` from a 127-byte result). Fixing the surface
(12 declarations, `read_file`, no extra required fields) is a separate cycle; it
would also close the 0.3k tools-row gap.

## 8. Tests

- `internal/backend/userwire_test.go` — envelope bytes vs the captures; 432/131 B;
  formula cases for user and tool-result counting.
- `cmd/wagyu/slash_cycle224_test.go` — `sessionContextTokens` mirrors the new math;
  seeded history carries `WireContent`; labels derived through `contextTokenLabel`.
- `cmd/wagyu/cycle289_streaming_slash_test.go` — live streaming layout (`2` / `len/4`),
  `/clear`+`/new` abort semantics, `/compact` input notice, `/rewind` panel.
- `cmd/wagyu/slash_changelog_source_test.go`, `slash_cycle207_test.go`,
  `slash_panels_test.go` — 1.4.3 anchors, 2014 denominator.
- `go test ./cmd/... ./internal/...` — green (all 27 packages).

## 9. 硬约束

- The 1.4.3 binary is gone (auto-update overwrote it, no backup). Every "1.4.3"
  claim above comes from the committed decompiles/notes; every live claim was
  measured on **1.4.4**.
- The accounting rules are decompiled from 1.4.3's Go, not 1.4.4's. 1.4.4's
  `/context` matched them in every case measured here, but its own function bodies
  were not re-decompiled.
- No Gemini tokenizer is available locally; all `/3` and `/4` divisors are the
  reference's `EstimateTokenCounter`, not a real tokenizer.

## 10. 存疑

- `System tools` may use a real tokenizer on the reference side (4.6k for
  13,449 B). If so, matching it exactly is impossible without the tokenizer; the
  clone would still be ~0.1–0.2k low even with an identical tool surface.
- The `+6` in the user/tool formulas is empirical (it held for 131, 432, 434, 445,
  109 and 127 byte payloads); no decompiled source line pins it directly.
- `/compact` on a long conversation was not exercised — only the
  "Nothing to compact yet." path.
- `/branch` and `/new` were re-verified by unit tests and earlier 1.4.3 captures,
  not live on 1.4.4 in this cycle.
- The changelog denominator 2014 is a measurement at 120×40; the clone wraps at
  its own width, so a ctrl+end counter can stop short of 2014.
- `/fork` output wording still differs: the reference prints
  `Forked conversation.` plus `To go back, use: /resume <id>`; the clone prints one
  `Forked conversation <new> (from <old>)` line. Semantics and streaming behaviour
  match; the wording is pre-existing.

## 11. Limitations

- The cycle does not change the advertised tool surface (12 vs 60, `read_file` vs
  `view_file`, extra required fields). §7 records the consequence.
- The system prompt is still ~0.6 KB shorter than 1.4.4's wire instruction.
- `internal/agyprompt/testdata/capture_1.4.2.*` and
  `internal/tools/testdata/advertised_tools_1.3.6.txt` remain un-re-evidenced
  against 1.4.4; the lock lists them as suspect.
- `/login` work in the tree is the user's and was not touched or verified.

## Files

- `internal/backend/userwire.go`, `userwire_test.go` (new)
- `internal/backend/client.go` (`WireContent`, `WireText`)
- `internal/backend/cloudcode.go` (`buildToolDecls`, `WireToolsJSON`)
- `cmd/wagyu/slash_panels.go` (`sessionTokenCounts`, `streamingTokenCounts`,
  `toolTokenEstimate`/`wireToolDecls`, `promptSectionTokens`, `currentEffort`,
  `changelogTotalLines`, `inputNotice` rendering)
- `cmd/wagyu/tui_run.go` (`runMode` fields, `appendUserTurn`, wire substitution,
  `slashQueuesWhileStreaming`, `abortTurnForLocalCommand`,
  `resetSessionTokenCounters`, `turnDoneMsg` discard branch, placeholder switch)
- `cmd/wagyu/slash_commands.go` (`/clear`, `/new`, `/fork`, `/branch`, `/compact`,
  `sendAsPrompt`)
- `cmd/wagyu/tui_tools.go` (`toolStep.result`)
- `cmd/wagyu/slash_statusline_external.go` (`recordTurnUsage` →
  `lastTurnCompletionTokens`)
- `internal/cli/testdata/changelog_reference.txt`, `internal/cli/changelog_data_test.go`,
  `internal/cli/changelog_cycle_235_test.go`
- `internal/referencedrift/reference.lock.json` (lag through 1.4.4, resealed digest)
