# Cycle `result-text-replay` — the capture is now an executable contract

**Status**: harness added, all green (`internal/resultparity`)
**Reference**: `~/.local/bin/agy` 1.4.4, sha256 `9abb478686dc08d48699ba1604c14d15819ef6a2542d5d32173acbd5f02d712d`
**Fixture**: `internal/tools/testdata/result_texts_1.4.4.txt` (【一手】, 32 captured call/result pairs)

## Why

That fixture is the only first-hand record of what the reference returns to the model for
each of its declared tools. Until this cycle **no test read it**. The fixture was cited in
code comments while the assertions lived as hand-copied string constants, so nothing failed
when a constant drifted away from the capture. Cycle 228's
`not yet implemented (cycle 228 stub)` text reached the model for months that way and was
found by `grep`, not by a test.

`referencedrift` was not the missing guard: it hashes the file's bytes. It cannot notice that
nobody reads what it hashes.

## What was added

| File | What |
|---|---|
| `internal/resultparity/fixture.go` | `Load` — parses the capture into `Section{Name, Args, Note, Notes, Body, HasStamp, Line}`. Header args are extracted with a string/escape-aware brace scan, the `<stamp>` marker is separated from the body, `#` notes before the body are kept. |
| `internal/resultparity/replay_test.go` | the replay plan, the comparison modes, and `runTool`. |

Two tests:

- **`TestCapturedResultTexts`** — replays each planned call against a temp fixture tree,
  rewrites the capture's `/tmp/…` paths onto that tree, strips the wall-clock
  `Created At` / `Completed At` header, and compares.
- **`TestCaptureIsFullyAccountedFor`** — a bijection between the fixture's sections and the
  replay plan, keyed by tool name + compacted arguments. A new section nobody replays fails,
  and so does a plan entry whose section disappeared. This is the part that stops the fixture
  from quietly becoming decoration again.

Coverage: **32 sections → 28 replayed + 4 skipped**, each skip carrying its reason in the
failure output (`go test -v` prints it).

| tool | sections | replayed |
|---|---|---|
| `read_file` | 9 | 9 |
| `view_file` | 6 | 6 |
| `write_file` | 3 | 3 |
| `edit_file` | 3 | 3 |
| `send_message`, `define_subagent`, `manage_subagents`, `manage_task`, `search_web`, `generate_image` | 1 each | 6 |
| `invoke_subagent` | 2 | 1 (`self` is skipped: async ack) |
| `schedule`, `read_url_content`, `ask_question` | 1 each | 0 (skipped) |

Skipped, with the reasons recorded in the plan:

| section | why |
|---|---|
| `invoke_subagent {"TypeName":"self"}` | the captured ack promises a later `send_message`; the clone delegates synchronously and returns the answer in the same result. Emitting the ack would drop that answer. |
| `schedule {"DurationSeconds":3600}` | the captured result is a backgrounded launch carrying a runtime task id and brain path; the clone runs synchronously (cycle 277 deviation). |
| `read_url_content` | network. |
| `ask_question` | interactive ("A1: alpha" is the user picking the first option). |

## Two comparison modes, and why one is weaker

`exactBody` (20 sections) is byte-for-byte. `proseBody` (8 sections) exists because the
capture abridges the 800-line and 46080-byte bodies with `…`: it compares the capture's
non-numbered lines as an **ordered subsequence** of the clone's output, plus the first and
last numbered line as anchors. Prose is where the parity risk lives (range header,
truncation note, closing sentence), so the weaker mode is still the useful one; the elided
content itself is unrecoverable from the fixture.

Anchors are matched as prefixes, because the capture shortens at least one of them without
a marker (`view_file` prints `47: A0046xxxxx` for a 995-character line).

## The harness is called through `Tool.Run`, not `Dispatch`

Both `tools.Registry.Dispatch` and `piagentbridge`'s adapter pass the text through
`tools.ApplyOutputBudget` before the model sees it. The capture records what the *tool*
returned, so the harness compares at `Run`. That is a deliberate, documented split — and it
immediately exposed the finding below, which the budget would otherwise have hidden inside
the comparison.

## Finding: the clone truncates results the reference delivers whole

`DefaultMaxResultTokens = 8000` with a `/4` chars-per-token estimate caps model-visible tool
text at roughly **32 KB**, replacing anything larger with the clone's own wrapper:

```
_truncated: true
_preview: |
  Created At: …
```

Its own doc comment admits the provenance: *"The reference binary ships a
model-class-dependent value; this constant is the cycle-248 default for the clone."* It is a
guess.

The capture contradicts it. `read_file {"Path": "/tmp/rl_long.txt"}` returns

```
Partial read (lines 1-100 of 100). Showing bytes 0-46080 of 100491 in this line range, …
```

— the reference handed the model **46080 bytes** of body plus the note, unwrapped. The
`view_file` long read is another ~46 KB, also whole. So for file reads the reference's
per-step budget is at least ~50 KB, and the wrapper text (`_truncated` / `_preview`) does not
appear anywhere in the capture.

Consequence today: any read, command output, or MCP result above ~32 KB is silently replaced
by a preview the reference never prints, and the model is told the content was truncated at a
threshold that was invented. This is the largest known divergence in the tool-result path and
the obvious next cycle: measure the real per-step budget from a captured large result, then
decide whether the wrapper exists at all.

## Fixture-fidelity traps this harness surfaced

Writing the replay forced three readings of the fixture that had been wrong or unnoticed:

1. `rl_long.txt` starts at **`A0000`**, not `A0001` — the capture's line 47 is `A0046`. A
   first cut generated `A0001…A0100`, which shifts every line number; the anchor check caught
   it.
2. `refmulti.txt` is `"alpha\nbeta\n"` (two lines), not `"alpha beta\n"` — the capture's
   `+3/-2 lines` only makes sense with a two-line `Target`.
3. Two `rl_big` sections (`StartLine: 100`, `StartLine: 1, EndLine: 900`) kept **only the
   range header** and dropped the body with no `…` marker. They are prose-mode for that
   reason, not because the clone differs.

## 硬约束

- The harness must not become a second implementation. It only calls the real executors
  (`tools.RuntimeCatalog(...).Run`, `subagenttools.Tools(...)`); no result text is duplicated
  in the test.
- Do not "fix" a failing replay by relaxing the comparison. A `exactBody` mismatch means the
  clone and the capture disagree; the capture wins unless it is itself elided, in which case
  the section moves to `proseBody` **with a note saying why**.
- Every skip must carry a reason. A bare `t.Skip("TODO")` is how this fixture became
  decorative the first time.

## 存疑

1. `proseBody` cannot distinguish a correct body from a missing one — by construction. The
   eight elided sections are checked only for their prose skeleton and range anchors.
2. The 46080-byte cap *within* `read_file`/`view_file` is the reference's own and is matched;
   whether the reference applies any budget *on top* is unknown above ~50 KB, since the
   capture's largest result is 46 KB.
3. Whether the reference's budget is per-tool (file reads exempt) or global-but-large could
   not be separated: the capture contains no over-budget case.

## limitations

- No live capture was made; this cycle binds existing evidence, it does not add any.
- `ask_question`, `read_url_content` and `schedule` are skipped, so three of the twelve
  declarations still have no replay. Two of them need network/interaction; `schedule` needs a
  decision about the synchronous deviation first.
- The replay runs the subagent executors with `Deps{}`: every captured subagent call is a
  pre-delegation path (unknown recipient, `kill_all`, undefined type), so no session is
  constructed. A `TypeName: "self"` replay would need the mock-backend harness that is not
  committed.
- `internal/tools`' own `result_text_test.go` still hand-copies bodies. Those constants are
  now redundant with the replay for the sections it covers, and are harmless — but they are
  exactly the shape of thing that drifts, so new assertions belong in `internal/resultparity`.
