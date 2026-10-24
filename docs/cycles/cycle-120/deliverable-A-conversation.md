# Cycle 120 — loading the reference's conversations

**Target:** `agy` 1.3.6 (`~/.local/bin/agy`,
SHA-256 `827ba22b4aa33557614a57aac2cc13f386e17db6b802febd3932226906ac05ab`).

**Theme:** user report — *"`go run ./cmd/antigravity/ --conversation=<id>` 还是
无法加载历史对话"*. The flag was registered in
`internal/cli/parser_specs.go:83` and then never read. `-c` dropped into the TUI
with an empty transcript, and the envelope's `conversation_id` was always a
freshly minted UUID. The clone kept no conversation state of its own and had no
way to read the reference's.

This cycle makes the two binaries share one conversation store.

---

## 1. The store the reference keeps 【一手】

`~/.gemini/antigravity-cli/` holds one SQLite database per conversation, plus an
index and two caches:

| Path | Shape |
|---|---|
| `conversations/<uuid>.db` | one database per conversation (up to 108 MB) |
| `conversation_summaries.db` | one table `conversation_summaries`, a GORM model |
| `cache/last_conversations.json` | `{"<absolute dir>": "<conversation id>"}` |
| `cache/conversation_metadata.json` | `{"conversations": {"<id>": {"summary": {ID, Title, Preview, NumSteps, UpdatedAt, WorkspaceURIs, …}, "last_modified_time": …}}}` |
| `history.jsonl` | one compact JSON object per line (`display`, `timestamp` ms, `workspace`, optional `conversationId` / `type`) |

A conversation database carries `trajectory_meta`, `steps`, `gen_metadata`,
`executor_metadata`, `parent_references`, `trajectory_metadata_blob` and
`battle_mode_infos`. Everything the transcript needs lives in `steps`, whose
columns are `idx, step_type, status, has_subtrajectory, metadata, error_details,
permissions, task_details, render_info, step_payload, step_format`, with
`idx` an `INTEGER PRIMARY KEY` (i.e. a rowid alias).

The user's own conversation, `4638a7b0-e5f4-4961-80c2-d61499f06050`, holds
15,091 steps and is 108,199,936 bytes.

## 2. Reading it without a SQLite driver 【一手】

The clone is pure Go and deliberately avoids heavyweight dependencies;
`modernc.org/sqlite` is ~100 MB of generated C-to-Go translation for a read-only
job. `internal/convstore/sqlite.go` (657 lines) implements the subset of the
SQLite file format needed to walk a table in rowid order:

* the 100-byte header (page size, reserved space, text encoding, page count),
* table B-trees — leaf pages (`0x0d`) and interior pages (`0x05`),
* overflow chains, using SQLite's own `btreeParseCellPtr` local-payload rule,
* the record encoding, including the `INTEGER PRIMARY KEY` rowid alias,
* UTF-8 and UTF-16 text.

Not implemented, because reading whole tables in rowid order never needs them:
indexes, `WITHOUT ROWID` tables, the freelist and write-ahead logging.
Conversation databases ship no `-wal` sidecar (verified: zero `-wal` files under
`conversations/`), so the main file is the whole story.

Two bugs the tests caught, both of which would have failed silently in
production:

* **The B-tree page types are 13 (leaf) and 5 (interior), not 5 and 2.** With
  the codes swapped every lookup died on `page 1 is not a table B-tree page
  (type 13)`.
* **Protobuf varints are little-endian 7-bit groups; SQLite varints are
  big-endian.** Reusing the SQLite decoder for protobuf mis-reads every
  multi-byte field number — field 5 worked (one-byte key), field 19 decoded as
  3329 and the walk desynchronised.

Cross-checked against the `sqlite3` CLI on the real 108 MB database: identical
table list, identical row counts (15,091 steps, 7,159 `gen_metadata`, …) and an
identical `step_type` histogram.

## 3. The step payloads 【一手】

`step_payload` is a Google-internal protobuf. The wire format is
self-describing enough to walk without its `.proto`, which is the only reason
the transcript is recoverable at all. `internal/convstore/agy.go` decodes:

| `step_type` | meaning | path |
|---|---|---|
| 14 | user input | `19 → 3 → 1` (text) |
| 15 | model response | `20 → 1` (text) |
| 132 | tool call | `5 → 4 → {2: tool name, 3: JSON arguments}` |
| 17 | error | `24 → 3 → {1: short, 2: long}` |
| 101 | system notice | `24 → 3 → {1: message}` |
| 23 | subagent | (opaque) |

Every one of those paths was confirmed by walking real payloads, not guessed:
step 0 of the user's conversation yields the Chinese prompt
*"你现在在 tmux 里面，可以对比 ./cmd/antigravity 和 agy cli 运行的差别…"*, step 9727
yields a 72 KB ` thinking`-prefixed model response, step 6 yields
`run_command {"CommandLine":"ls /Users/gear/GitHub/openagy/","Cwd":…}`.

`LoadConversation` reads all 15,091 steps in **300 ms** and produces 7,917
transcript entries and 445 user/assistant turns.

**Bonus finding — the reference's own argument names.** Stored tool calls carry
Google-style capitalised keys, which the clone's lower-case schemas did not
match:

```
run_command            CommandLine, Cwd, WaitMsBeforeAsync, toolAction, toolSummary
view_file              AbsolutePath, StartLine, EndLine, ContentOffset
grep_search            Query, SearchPath, CaseInsensitive, IsRegex, MatchPerLine
replace_file_content   TargetFile, TargetContent, ReplacementContent, StartLine, EndLine, Instruction
write_file             AbsolutePath, Content, Append
write_to_file          TargetFile, CodeContent, Overwrite
list_dir               DirectoryPath
schedule               Prompt, DurationSeconds
read_url_content       Url
```

`toolStepTitle` now accepts both spellings. Without them every step in a resumed
transcript rendered as the bare fallback (`Ran a command`) instead of
`Ran ls /tmp`.

`toolAction` / `toolSummary` are **model-supplied** labels (5,218 of 5,346
`run_command` calls carry them: `toolAction: "Listing workspace directory"`,
`toolSummary: "Directory listing"`, 5,542 distinct `toolAction` values). They are
not step titles — the recovered step-title vocabulary (`Ran / Read / Wrote /
Explored / Searched / Listed / Messaged / Spawning / Browsing / Needs approval /
Failed`) is derived from the tool name, and `toolAction` reads like the recovered
spinner hint ("names the tool call the agent is currently running"). The clone
therefore keeps deriving the title and does not consume them yet.

## 4. `--conversation` semantics 【一手】

Probed against the live reference (its quota is exhausted, but this path is
local):

```
$ agy --conversation=deadbeef-0000-4000-8000-000000000000 -p hi
warning: conversation "deadbeef-0000-4000-8000-000000000000" not found
{"conversation_id":"f65cfba7-542b-40a0-8888-795150e6cbed", …}

$ agy --conversation=F65CFBA7-542B-40A0-8888-795150E6CEBED -p hi
warning: conversation "F65CFBA7-542B-40A0-8888-795150E6CEBED" not found
```

The second probe is the interesting one: `conversations/f65cfba7-….db` **exists**
and this is a case-insensitive APFS volume, so `os.Stat` would have found it. The
reference compares directory entries, not paths — the lookup is case-sensitive on
a case-insensitive filesystem. `convstore.HasConversation` now does the same
(`os.ReadDir` + exact name match), which also gives the recovered
zero-byte-file-counts / directory-does-not behaviour for free.

The clone reproduces both lines byte-for-byte, including the fresh
`conversation_id` on the not-found path.

## 5. `-c` semantics 【一手 + 推测】

Probed:

| cwd | reference picks |
|---|---|
| `/tmp` | `22b2f823-88b8-47cf-b4a4-5571b545d00e` |
| `/tmp/agyconvprobe` (no key of its own yet) | `22b2f823-…` — the parent `/tmp`'s conversation |
| `/tmp/agyconvprobe` (after a run recorded a key) | `f65cfba7-…` |

So `-c` is workspace-scoped and a directory **inherits its parent's workspace**
when it has none of its own. The clone resolves, in order:

1. `cache/last_conversations.json`, exact directory;
2. the same file, nearest ancestor;
3. `cache/conversation_metadata.json`, most recent `last_modified_time` whose
   `WorkspaceURIs` contains the directory or an ancestor.

`last_conversations.json` is a cache the reference **rewrites from
`conversation_metadata.json` on startup** — a run in `/tmp/agyconvprobe` added
its key to the file, and the file contains entries for conversations injected
into `conversation_summaries.db` during earlier probing. The metadata cache is
therefore treated as authoritative and the map as a fast path. The ancestor rule
is graded 【推测】: it is inferred from the observed picks, and the reference's own
source is unrecovered.

**`num_turns` counts resumed turns.** A fresh run reports `num_turns: 1`; `-c`
on the one-user-turn conversation `f65cfba7` reported **2**. The clone therefore
reports `1 + prior user turns`. Confirmed by the clone's own run on that exact
conversation: `num_turns: 2`.

## 6. Wiring 【一手】

Print mode (`runPrintMode`):

* `--conversation` / `-c` resolve **before** the slash-command dispatch, so a
  missing conversation warns even on the local-command path;
* the envelope's `conversation_id` is the adopted id, not a fresh UUID;
* the loaded user/assistant turns are replayed ahead of the new prompt;
* `num_turns` includes the resumed turns.

Verified live against MiniMax-M3 through the agw transport:

```
$ antigravity --conversation=4638a7b0-e5f4-4961-80c2-d61499f06050 \
      -p 'Reply with exactly: RESUMED OK' --output-format json
{"conversation_id":"4638a7b0-e5f4-4961-80c2-d61499f06050","status":"SUCCESS",
 "response":"RESUMED OK","num_turns":39,
 "usage":{"input_tokens":7621,…}}

$ cd /tmp/agyconvprobe && antigravity -c -p 'Reply with exactly: CONTINUED OK' --output-format json
{"conversation_id":"f65cfba7-542b-40a0-8888-795150e6cbed",…,"num_turns":2,…}
```

The same run without `--conversation` sends 3,366 input tokens; the resumed run
sends 7,621, i.e. the history is really on the wire.

TUI (`runLiveTUI`): the adopted id replaces the freshly minted one and
`seedConversation` renders the stored transcript into the viewport, reusing the
live renderers — prompts via the shared `userPromptRow` (bold blue `> `),
replies as-is, and consecutive tool calls coalesced into one step group that
`ctrl+o` expands (registered as a `turnBlock`). A 100×30 tmux capture of
`--conversation=4638a7b0-…` shows the previous turns, the grouped tool steps
(`⏺ 2 commands  (ctrl+o to expand)`), the scrollbar thumb and the viewport pinned
to the bottom.

## 7. Tests

* `internal/convstore/sqlite_test.go` — 9 tests against a checked-in fixture
  (`testdata/small.db`), including a 70,000-byte and a 90,000-byte overflow blob
  and the rowid-alias column.
* `internal/convstore/agy_test.go` — 8 tests against `testdata/transcript.db`,
  a nine-step fixture generated by `testdata/gen_fixture.py` (the generator is
  committed so the fixture can be rebuilt and audited).
* `cmd/antigravity/conversation_test.go` — 14 tests: flag adoption, the exact
  warning text, empty/last-wins/case-sensitivity, zero-byte files, directories,
  the three `-c` resolution paths, and transcript seeding.

`go vet`, `gofmt` and the full suite (`go test ./cmd/... ./internal/...`) are
clean; 188 tests pass in the two touched packages.

A/B harnesses, re-run after the change with the reference SHA verified unchanged:

| harness | result | note |
|---|---|---|
| `harness137.sh` | 23/25 | the same two known diffs (`/usage`, `disable_slash`) |
| `harness136.sh` | 85/87 | both diffs are `124/…` — the reference timing out under its own degradation |

## 8. 硬约束 / limitations

* **The clone does not write the store.** It reads `conversations/*.db`,
  `cache/last_conversations.json` and `cache/conversation_metadata.json`, and
  writes none of them. A conversation started by the clone is therefore not
  offered to `-c` afterwards, and the reference cannot resume it. Writing
  `history.jsonl` and the two caches is straightforward; writing a
  `conversations/<uuid>.db` is not — the payloads are protobuf with unknown
  message numbers.
* **Tool results are not restored.** A stored tool call decodes to its name and
  arguments, but the result the tool returned lives in a step shape this cycle
  does not decode, so an expanded loaded step shows its title only.
* **The resumed-transcript layout is 【推测】.** The reference needs a live model
  turn to draw one and the account's quota is exhausted
  (`RESOURCE_EXHAUSTED`, "Resets in ~86h"), so the arrangement is assembled from
  the recovered per-step renderers rather than compared byte-for-byte. Errors and
  system notices are rendered as indented lines; the reference's rendering of
  those is unrecovered.
* **The history replay is bounded** (`40` turns / `100,000` characters, both
  【推测】). The reference feeds the whole conversation; the agw transport has no
  server-side state, so every replayed turn is re-sent in full and real
  conversations hold model replies of tens of kilobytes.
* **`-c` with nothing to continue prints nothing.** The reference's output for
  that case is unrecovered — the live binary could not be probed that far — and
  the clone prefers silence to an invented message.
* **`conversation_summaries.db` is still unused.** `-c` is answered from the two
  caches instead. The summaries table is a GORM model with no literal SQL in the
  binary, so its `workspace` column semantics are unrecovered; the caches carry
  the same information in a readable form.
* The reference's `--conversation` probe ran with an exhausted quota, so every
  probe terminated in `RESOURCE_EXHAUSTED`. Only the local warning path was
  observable; the success-path envelope shape comes from earlier cycles.

## 9. Backlog

* Write the store: append `history.jsonl`, update `cache/*.json`, and create
  `conversations/<uuid>.db` so the reference can resume clone conversations.
* Decode tool results and attach them to loaded steps so `ctrl+o` shows output.
* Add `toolAction` / `toolSummary` to the clone's tool schemas so the live spinner
  hint matches the reference's.
* Replace the `/resume` cycle-77 stub with a real conversation list built from
  `conversation_metadata.json`, and seed the TUI's up-arrow history from
  `history.jsonl`.
* `--input-format stream-json`; the remaining cycle-77 stubs (`/keybindings`,
  `/voice`, `/rewind`, `/fork`, `/feedback`, `/artifact`, `/btw`, `/context`,
  `/plugin`); `/usage`; verbosity settings; the `Interrupted` hint; `/config` →
  `Agent Mode`; `trustedWorkspaces`; `denied_actions` in the JSON output.
* Refresh `out/local-macos/` evidence to 1.3.6 (user-deferred).
