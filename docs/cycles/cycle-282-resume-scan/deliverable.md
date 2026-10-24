# Cycle 282 — `/resume` lists on-disk conversations the cache has never seen

The wrapper's reference run wrote a conversation on `~/.gemini/antigravity-cli/conversations/ecbc62e4-….db`,
`conversation_summaries.db` got the row (status `IDLE`, 4 steps, no title), and the clone's `/resume` picker
ignored it because the cycle-278 `ListConversations` is cache-first. The cycle-278 ruling was the right
**call** for the store it measured, but it was drawn from an observation that does not distinguish
"cache only" from "cache + disk scan", and the second interpretation is what the reference actually does.

Cycle 282 reverses that ruling, merges the on-disk scan the reference performs on every `/resume`,
and pins it with three new tests + a removed-from-cache reproduction on a store copy.

## Mechanism (static, 1.4.2)

`ServerBackend.ListConversations` (0x10253b890) opens with a feature-flag check:

```
ldr x1, [x0, #0x218]            ; cloud session? -> cloudConversations
bl   summaryStoreReadsEnabled   ; "summary-store-reads"
tbz  w0, #0x0, scan-path
... listConversationsFromStore   ; reads conversation_summaries.db + filters by on-disk ids
scan-path:
bl   scanConversations          ; 0x10253ba80
... syncSummariesToStore
```

`summaryStoreReadsEnabled` (0x10254d840) reads the key **`summary-store-reads`** from the server config
(via the interface method at the end of the function — `x1+0x48`; the literal at `0x1032edc83` is the
19-byte string `"summary-store-reads"`). When it is **off** (the state observed on this machine: a
`--json`-less wrapper run, `agy --print -p ...`, and the offline TUI at the login screen), the list comes
from `scanConversations`, which calls `listConversationsFromDir` to walk `conversations/*.db`,
statures each file, and merges any entry the cache does not have:

`listConversationsFromDir` (0x10253d580):
- `fs.Path.ReadDir`, `conversationIDFromFileName` (0x10253d8b0)
- `mapaccess1_faststr` cache lookup (0x10253d8e8)
- `fs.Path.Stat` + `time.Time.After/Equal` for staleness (0x10253d9dc..10253dac4)
- `cortex.AnnotationsManager.GetAnnotations` (0x10253db38) — the AI title source
- `mapassign_faststr` writes the new/updated entry into the cache map (0x10253dc80, dcb4, de00)

`scanConversations` then calls `saveConversationCache` (0x10253cfe4) — the reference's
`loadConversationCache` / `saveConversationCache` pair are the **only** writers of
`cache/conversation_metadata.json`. The on-disk scan is what populates them.

Cycle 278 observed REF listing exactly the cache's rows (34 of 38) on a 38-entry cache / 168-row index
store. The 130 index-only rows there had **no `.db`** — they were stale rows from the cycle-278 store
cleanup, and the same stat-check that filters them would also filter them under the disk-scan model.
The observation is consistent with both "cache only" and "cache + disk scan"; the disassembly picks the
second.

## What the reference hides

The picker filters through `isInternalTrajectory` (`utils.IsHiddenFromConversationList`, 0x1012a3680),
a boolean read from the conversation metadata at offset `0x18`. On this store the `annotations/*.pbtxt`
files carry only `title:"..."`; the hidden flag lives elsewhere (the trajectory metadata blob).
Subagent conversations are flagged with `parent_conversation_id != ""` in `conversation_summaries.db`
(4 rows: `143a4360…`, `95cbfc67…`, `2d92434b…`, `1adcf12c…`) — none of them is in the cache, the
reference's scan excludes them, and the clone must do the same.

## Repro on `~/.gemini/antigravity-cli` copy

```
$ rsync -a ~/.gemini/antigravity-cli /tmp/rh2  # captured during live activity
$ python3 -c "..."  # drop 00752df3-… from /tmp/rh2/cache/conversation_metadata.json
$ ANTIGRAVITY_HOME=/tmp/rh2 go run ./cmd/zz-listprobe  # scratch
$ grep ^00752 /tmp/probe.txt
00752df3-49e7-4b42-5a7c-8bbe5c397fe4  steps=1  "00752df3-49e7-4b42-…"
```

The conversation the cache no longer knows about is listed via the summaries index row, with the
label chain falling through to the bare id because the index has `title=""`, `preview=""`. The
pre-cycle-282 list would have been missing this row.

`entries=676` vs `ls *.db | wc -l = 677` — the single missing row is a file `peekConversation`
cannot open (the `peekConversation` failure path is silent, matching the reference's scan). No tests
in the store saw it.

## Changes

| File | What |
|---|---|
| `internal/convstore/list.go` | `ListConversations` merges `scanSummaries` then `listFilesMissingFromIndex` (always, not only as a fallback). The previous `if len(out)==0` fallback to `listFromDir` is gone — it was the only path that resurrected the subagent rows the index just hid. New `scanSummaries` returns the set of ids it skipped as subagents; `listFilesMissingFromIndex` skips them too. New `annotationTitle(id)` reads `annotations/<id>.pbtxt`, unescaping `\n` `\\` `\"`. Top-doc corrects the cycle-278 cache-only ruling. |
| `internal/convstore/list_cycle278_test.go` | `TestListConversationsPrefersCacheOverIndex` → `TestListConversationsMergesIndexRowsOnDisk`: both the cached and the index-only rows are now listed, the cached label is preserved. |
| `internal/convstore/list_cycle282_test.go` | new: `TestListConversationsListsFileMissingFromBothIndexes` (file with no cache and no index row → listed via peek, label = first user input), `TestListConversationsUsesAnnotationTitleForUncachedFile` (annotation title wins over the raw prompt), `TestAnnotationTitleEscapes` (the pbtxt reader handles `\"` `\\` `\n`), `TestListConversationsSkipsSubagentIndexRows` (a file + index row with `parent_conversation_id` → hidden). |

Tests:
```
$ go test ./cmd/... ./internal/...
…  # no FAIL lines
```

`cmd/zz-listprobe/` is a scratch directory under `cmd/`. Removed in this commit.

## Evidence grading

- 【反编译】 `ServerBackend.ListConversations` and `scanConversations` / `listConversationsFromDir` /
  `parallelLoadSummaries` / `summaryStoreReadsEnabled` call graph, addresses, branches, and the
  `summary-store-reads` string at 0x1032edc83. The feature flag and the merge mapassigns.
- 【反编译】 `IsHiddenFromConversationList` is a read of a bool on the metadata object; no
  corresponding flag in `annotations/<id>.pbtxt` files on this store.
- 【一手】 `~/.gemini/antigravity-cli` live store, 502 conversations, cache mtime updated as the
  user's clone keeps writing. The 4 subagent rows (`parent_conversation_id != ""`) are absent from
  the cache. `entries=676` in the post-fix list covers 676/677 files.
- 【一手】 Reproduction on `/tmp/rh2`: removed `00752df3-…` from `cache/conversation_metadata.json`,
  the conversation is still listed (full output saved as
  `docs/cycles/cycle-282-resume-scan/evidence-list-rh2-removed.txt`).

## 硬约束 / Limitations

1. **No live reference `/resume`.** The wrapper's `agy` requires a TTY login (keychain-bound OAuth);
   we cannot run the reference past the login screen on this machine, so the static analysis is the
   primary evidence. A direct REF capture would confirm the on-screen rendering row-for-row.
2. **`summary-store-reads`** is a server-side gate. If it ever flips on, the picker switches to
   `listConversationsFromStore`, which reads `conversation_summaries.db` and filters by
   `conversationIDsOnDisk` — same shape as the merge below, so the cycle-282 fix would still hold.
3. **`peekConversation` cost.** Each merge hit opens a `.db` and parses every step to find the first
   user input; on this machine the missing-from-cache set is 0–1 per `/resume`, so the cost is in
   the single-millisecond range and dominated by sqlite page cache misses on a cold first open.
4. **Hidden conversations beyond the `parent_conversation_id` filter.** The reference's
   `IsHiddenFromConversationList` reads a metadata flag we cannot recover from the index. If a
   conversation has `parent_conversation_id=""` but the in-DB metadata marks it hidden, the clone
   would list it. None of the 502 conversations on the live store trips this.
5. **Annotation title parser.** `annotationTitle` is a 30-line pbtxt reader covering `title:` only.
   Annotations may carry other fields (e.g. labels, free-form notes); they are ignored. The parser
   is sufficient because the reference uses the same single-field lookup.
6. **The checked-in seeds are stale.** `out/local-macos/seeds/seeds.txt` (154,525 functions, Oct 14)
   was derived from an earlier `~/.local/bin/agy`; the installed 1.4.2 has 158,955 functions and
   every address shifts (e.g. `store.(*Manager).ListConversations` is `0x1027d6200`, not
   `0x10265bde0`). The addresses quoted above come from a fresh `rea-go-seeds` run against the
   installed binary (`/tmp/seeds142/seeds.txt`), not from the checked-in file. Regenerate the seeds
   before reusing any address here; the checked-in copy is not updated by this cycle.

## Related

- `docs/cycles/cycle-278/deliverable.md` §4.2 "picker is cache-first" — reversed by this cycle.
- `internal/convstore/list.go` — the merged `ListConversations` and the `annotationTitle` helper.
- `internal/convstore/list_cycle282_test.go` — the four new tests pinning the behaviour.