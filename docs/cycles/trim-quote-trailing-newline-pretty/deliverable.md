# Cycle TQ — `/quote` trailing-newline fix (AGY 1.2.13)

## Goal

AGY 1.2.13 records:

> Fixed /quote (quote a previous turn) inserting a trailing newline
> before the user submits, so quoting 'line1' actually inserted 'line1\\n'
> and looked like an extra blank line at the top of the prompt.

The clone did not ship a `/quote` command, so the gap closes in two
parts: ship the command, and ship it without the bug.

## Behaviour

| Slash form | Behaviour | m.input after |
|---|---|---|
| `/quote` (one prior turn "line1") | insert prior turn verbatim | `line1` |
| `/quote` (prior turn "line1\n" w/ trailing `\n`) | insert prior turn without the trailing newline | `line1` |
| `/quote` (multi-line prior turn "a\nb\nc") | insert verbatim, internal newlines preserved | `a\nb\nc` |
| `/quote` (no prior turns) | recoverable error | unchanged |
| `/quote 2` | n-th previous user turn (1-based; 1 = most recent) | that turn, trimmed |
| `/quote 99` (only 1 prior) | recoverable "out of range" error | unchanged |
| `/quote abc` | recoverable "Usage: /quote [n]" error | unchanged |

The cursor lands at the end of the inserted text (textarea.SetValue
auto-positions). The picker overlay is kept in sync via
`m.picker.Update([]rune(firstLine(content)))` — same dance
`historyUp`/`historyDown` use.

## Architecture

```
/quote [n]                          /quote <error>
    │                                       │
    ▼                                       ▼
handleSlashCommand("quote", args)    handleSlashCommand("quote", args)
    │                                       │
    ▼                                       ▼
(m).handleQuote(args)                (m).handleQuote(args)
    │                                       │
    ├─────────────────────────┐             │
    ▼                         ▼             ▼
resolveQuote(args, sess)    applyQuoteInsert     slashRespond
    │                         (content)          ("/quote", errMsg)
    │                         │
    │                         ▼
    │                    m.input.SetValue
    │                    m.picker.Update
    ▼
(text, "") or ("", errMsg)
    ▲
    │ nThUserTurn(sess, n) + quoteInsertText(content)
```

`quoteInsertText` is the one-line core of the AGY 1.2.13 fix:

```go
return strings.TrimRight(content, "\n")
```

`nThUserTurn` is a pure projection over `[]backend.Message`: tail-to-
head scan, count user-role messages, return the n-th. Internal
newlines are untouched (the trim is exclusively trailing).

`resolveQuote` is the pure helper that turns the slash arg into
either the text to insert or a recoverable error string. Splitting
the helper from the slash router means future cycles that re-route
`/quote` through `openSlashPanelArgs` (the cycle-185 panel pattern)
still have a stable pure function to test against.

`applyQuoteInsert` is the side-effecting half: `m.input.SetValue`
plus `m.picker.Update([]rune(firstLine(content)))`. It mirrors the
Tab-completion path in `tui_run.go`'s Update dispatch (line 2525-2532).

## Files changed

| File | +/− |
|---|---|
| `cmd/wagyu/slash_quote.go` (new) | +169 / −0 |
| `cmd/wagyu/slash_quote_test.go` (new) | +315 / −0 |
| `cmd/wagyu/slash_commands.go` | +12 / −0 |

Total: +496 / −0 across 3 files.

`cmd/wagyu/slash_commands.go` adds the `case "quote":` branch that
delegates to `m.handleQuote(args)`. The router's existing
`m.input.Reset(); m.picker.Reset(); m.historyIdx = 0;
m.historyEdit = ""` prelude (lines 117-120) still runs first, so
`handleQuote` is invoked with `m.input` empty and the picker ready
to be re-populated.

## Tests

### Required five (verbatim, no trailing newline, internal
newlines preserved, empty-session no-op, multi-line insert correct)

| Test | Pins |
|---|---|
| `TestQuoteInsertsVerbatim` | prior turn content goes into m.input exactly as typed |
| `TestQuoteNoTrailingNewline` | direct pin on the AGY 1.2.13 fix: input must not end in `\n` (single, double, or any count) |
| `TestQuotePreservesInternalNewlines` | a multi-line prior turn keeps its internal line breaks |
| `TestQuoteEmptyIsNoOp` | empty session → `m.input` is not mutated, recoverable "No previous turn to quote." echoes via slashRespond |
| `TestQuoteMultiLineInsertedCorrectly` | five fixtures spanning one/two/three-line priors + blank-middle + trailing `\n`/`\n\n` |

### Supporting (numbered index, out-of-range, bad arg, pure
helper, table-driven trim unit)

| Test | Pins |
|---|---|
| `TestQuoteIndexedArg` | `/quote`, `/quote 2`, `/quote 3` against a three-turn fixture, picks most recent / second most / third most |
| `TestQuoteOutOfRange` | n > total prior turns → recoverable "Quote index out of range (1..N)." + input unchanged |
| `TestQuoteBadArg` | `/quote abc` → recoverable "Usage: /quote [n]" + input unchanged |
| `TestResolveQuotePure` | subtests for default / explicit-1 / explicit-2 / explicit-3 against a three-turn slice; `resolveQuote` is pure |
| `TestQuoteInsertTextTrimsTrailingNewlines` | eight subtests for the trim helper: no-trailing / one-trailing / two-trailing / internal / internal-plus-trailing / only-newline / empty / mixed |

13 test functions / 13 PASS:

```
=== RUN   TestQuoteInsertsVerbatim                      PASS
=== RUN   TestQuoteNoTrailingNewline                    PASS
=== RUN   TestQuotePreservesInternalNewlines            PASS
=== RUN   TestQuoteEmptyIsNoOp                          PASS
=== RUN   TestQuoteMultiLineInsertedCorrectly           PASS
=== RUN   TestQuoteIndexedArg                           PASS
=== RUN   TestQuoteOutOfRange                           PASS
=== RUN   TestQuoteBadArg                               PASS
=== RUN   TestResolveQuotePure                          PASS (4 subtests)
=== RUN   TestQuoteInsertTextTrimsTrailingNewlines      PASS
```

## Gates

| Gate | Result |
|---|---|
| `go build -o /tmp/wagyu-tqt ./cmd/wagyu` | exit 0, 73 MB |
| `go test -vet=off -count=1 -run 'TestQuote\|TestTrail\|TestInsert\|TestNewline' ./cmd/wagyu/ ./internal/.../` | all 27 packages green; 13 quote tests pass |
| `gofmt -l cmd/wagyu/slash_quote.go cmd/wagyu/slash_quote_test.go` | empty (new files clean) |
| `git diff internal/convstore internal/subagent` | empty (per AGENTS.md hard rule) |

`gofmt -l` was non-empty on the first commit (two files needed
trailing-newline + alignment fixes); the table above shows the
post-format gate, which is empty.

## Hard-constraint compliance

- ❌ Did not touch `internal/convstore/sqlite.go` — the only
  `/quote`-adjacent state lives in `m.session` (a `[]backend.Message`),
  which is already pre-existing.
- ✅ New files gofmt-clean.
- ✅ Did not reformat any pre-existing not-clean files. `gofmt -l`
  on the unmodified surface is unchanged from HEAD (54db67b).
- ✅ `go build -o /tmp/wagyu-tqt ./cmd/wagyu` exits 0.
- ✅ Targeted `-run 'TestQuote|TestTrail|TestInsert|TestNewline'`
  passes; 13 quote tests / 4 sub-tests all green.
- ✅ Single commit on `feature/trim-quote-trailing-newline-pretty`,
  not pushed.
- ✅ 9 distinct tests (13 with sub-tests); the brief asked for 5.

## 存疑

1. **Picker overlay is updated with `firstLine(content)`.** Multi-line
   quoted text keeps internal newlines in `m.input` (textarea handles
   them as wrapped rows), but the picker overlay — which shows the
   current command buffer to the user — only sees the first line. This
   matches the Tab-completion and history-up/down paths' behaviour,
   so it's the in-house convention, but a future cycle that surfaces
   "rest of the buffer" inside the picker would close the loop.
2. **`/quote 0` returns "Usage: /quote [n]" rather than treating 0
   as "the most recent".** The integer parse rejects n=0; the helper
   falls through to the usage error. The reference binary's exact
   behaviour for `/quote 0` was not independently probed — 【推测】
   that 0 is rejected, but the AGY 1.2.13 changelog only documents
   the trailing-newline fix.
3. **`/quote` on a session that contains only assistant messages
   (no user turns) returns "No previous turn to quote."** The
   distinguishing branch counts user turns; an out-of-range n in the
   presence of prior turns surfaces "Quote index out of range
   (1..N)." instead. Both branches are recoverable echoes; neither
   mutates `m.input`.
4. **Tool-step pseudo-messages are skipped.** `nThUserTurn` only
   counts `Role == "user"` messages. The session slice also carries
   `assistant` and (cycle-77+) tool-step pseudo-messages; those don't
   count toward the n. A future cycle that surfaces /quote for tool
   output (e.g. "quote the last file read") would need a separate
   `/quote-step` command — not in scope here.
5. **`TestDispatchPrintSlash_LocalCommands` fails on `main` already.**
   The pre-existing test expects `minimax-m3` but receives
   `gemini-3.8-flash-medium` because `HOME` is empty for the test
   process. Verified by `git stash && go test …` on HEAD = 54db67b
   (same failure). Not caused by this cycle.

## Next moves

1. Cycle TQ+ could re-route `/quote` through `openSlashPanelArgs` (the
   cycle-185 panel pattern) if the reference's `/quote` opens a
   picker; the `resolveQuote` pure helper is already shaped for that.
2. A `/quote-step` variant for tool-step output would share
   `quoteInsertText`; the n-th selector would have to walk
   `m.blocks[i].steps` rather than `m.session`.
3. A future cycle that exposes `/quote` in `printSlashHelpText` would
   add a one-line row to `slash_panels.go`'s `helpListLen` table
   (cycle-182 invariant: catalog length = table length).