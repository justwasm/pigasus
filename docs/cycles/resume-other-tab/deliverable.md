# Cycle resume-other-tab — the `/resume` picker's **Other** tab and the import confirm (A-15)

## Goal

Close **A-15** of the cycle-383 docs-alignment ledger
(`docs/cycles/cycle-383-docs-alignment/slice-a-cli-commands.md:408-425`):
the `/resume` picker's **Other** tab was empty and there was no import
path at all.

Before this cycle the clone's picker answered the second tab with
`nil` and carried the comment *"the reference's 'Other' tab is a
placeholder"*. The reference does not have a placeholder — it has the
Antigravity desktop app's conversations, and importing one raises the
reference's own `[Import this conversation? (y/n)]` prompt.

## The reference's behaviour

Three independent sources agree on what the Other tab is.

**【一手】 The reference's own capture** —
`out/resume-tree/ref/tree-other-tab.plain.txt` (agy 1.4.7, tmux pane at
200×50). The tab bar reads `CLI    Other   (tab to cycle)`, the tab
carries a search row (`Type to search conversations...`), the rows are a
**flat** list (no workspace tree, no project cell), and the hint is:

```
Keyboard: ↑/↓ Navigate  enter Select  tab Switch Tab  esc Go back / Clear search
```

No page keys, no `ctrl+f` view switch, no `f2` rename, no `f4` delete —
the desktop app's conversations are imported, and the CLI's own store
holds nothing of the user's to rename or to delete.

**【一手】 The official documentation** —
`official-docs/cli/commands/resume.md:63-71`:

> 1. With the conversation picker open, press Tab to switch from the
>    **CLI** tab to the **Other** tab.
> 2. Highlight the desktop conversation you want to import.
> 3. Press Enter. A confirmation prompt
>    `[Import this conversation? (y/n)]` appears.
> 4. Press Enter (or Y) to confirm. The CLI clones the history, context,
>    and tool trajectories into your terminal session.

**【反编译】 Where the desktop store lives** — agy 1.4.12,
`backend.(*ServerBackend).fallbackConvImportDirs` (0x102776500) joins the
literal `"antigravity"` onto the backend's own path and returns a
one-element slice: the fallback import directory is the app data
directory's `antigravity` child and nothing else. The CLI's own store is
`~/.gemini/antigravity-cli`, so the desktop app's store is its **sibling**,
`~/.gemini/antigravity`.

**【strings】 The reference's own strings name the import's three steps**
— the import itself (`Importing conversation %s from %s`), the save of
the imported trajectory (`Failed to save imported trajectory %s: %v`),
the update of its summary row (`Failed to update summary for imported
trajectory %s: %v`), the failure it reports when the import cannot be
done (`Failed to import conversation %s from %s: %v`) and the event it
records when it worked (`conversation_imported`). The prompt itself is
`[Import this conversation? (%s/%s)]` — the same shape as the delete
prompt's `[Delete? (%s/%s)]`, which the picker already renders.

All six strings are now indexed in the surviving copy of that table,
`out/local-macos-1.4.11/strings-index.txt` (the full dump is retired;
the six entries are in the B 段, which locates a string by content).

## What the clone does now

### The store reads the desktop app's conversations

`internal/app/session/convstore/desktop.go` (new) owns the desktop app's
store:

- `DesktopStoreRoot()` — `$ANTIGRAVITY_HOME/antigravity`, else
  `$HOME/.gemini/antigravity`. The two stores sit side by side under
  `~/.gemini`, and the environment override keeps the tests off the
  user's own files.
- `ListDesktopConversations()` — the conversations of that store the
  CLI's own store does **not** hold yet, with their previews. A
  conversation the CLI already has is not offered again: importing it
  over itself would be a second copy of the same thread.
- `ImportConversation(meta)` — the import. It copies the conversation's
  database **byte for byte**, folds the write-ahead log into the copy
  (`PRAGMA wal_checkpoint(TRUNCATE)`), copies the annotation file that
  carries the AI-written title, and upserts a row into the CLI's own
  `conversation_summaries.db` (creating the table with the reference's
  DDL when the store has no index yet). Nothing is re-encoded, so a tool
  call, a thinking block and a file edit survive the import exactly as
  the desktop app wrote them — which is what the documentation's "clones
  the history, context, and tool trajectories" means.

Two refusals are hard rules: the import never overwrites a conversation
the CLI's own store already holds, and it refuses a file that is not
inside the desktop store.

The store's readers were generalised so the same rules (cache first,
then the summary index, then the files on disk) apply to both stores:
`ListConversationsIn(root)` and the new
`ListConversationsWithPreviewsIn(root)` take the root, and the internal
`scanSummaries` now returns the previews beside the rows. The CLI's own
readers behave exactly as before.

### The picker answers the Tab key

`internal/ui/resumepicker/`:

- `switchTab()` moves between the two tabs and resets the cursor and the
  window. The **Other** tab is read the first time it is opened — a user
  who never opens it never pays for the scan of another product's
  directory.
- `Filtered()` is tab-aware: the CLI tab holds the store's own
  conversations, the Other tab the desktop app's. The search reaches the
  title, the first line of the first prompt and the conversation id on
  both tabs (A-16's predicate, already in the tree, is now shared
  between them).
- The Other tab is always a **flat** list: `treeActive()` requires
  `tab == 0`, so the workspace tree never draws there.
- The Other tab's hint is the reference's own — navigation, select, tab
  switch, esc — and it offers no page, no view switch, no rename and no
  delete. `ctrl+f`, `f2` and `f4` are inert on that tab.
- Enter on an Other row **arms** the import confirmation and draws
  `[Import this conversation? (y/n)]` next to the row's title, exactly
  where the delete confirmation draws `[Delete? (y/n)]`. Nothing is
  imported until the confirmation is answered.
- The confirmation is a stage of its own, like the delete confirmation:
  it owns the keys it answers with (Enter / Y confirm, Esc / N cancel)
  and the rest of the picker ignores them, so a typed letter never lands
  in the search field.
- A successful import closes the picker and resumes the imported
  conversation through the picker's existing `ResumeConversation` seam —
  the host loads it from `meta.Path` like any other conversation, so
  **no host change was needed**.

### The prompt rendering was generalised

`titleCell(title, width, prompt)` and `writePrompt(b, prompt)` take the
prompt as a value instead of knowing about the delete stage, so the two
confirmations render through one code path and the row keeps its width
(the right-hand cells do not move). `keyboardHintParts` takes a
`hintOptions` struct instead of three positional booleans, so the two
tabs' hints come from one place.

## The files

| File | What changed |
|---|---|
| `internal/app/session/convstore/desktop.go` | **new** — the desktop app's store: root, listing, import |
| `internal/app/session/convstore/desktop_test.go` | **new** — six cases over the store and the import |
| `internal/app/session/convstore/list.go` | the readers take a root; the previews travel beside the rows |
| `internal/app/session/convstore/scan.go` | `scanSummaries` returns a `summaryScan` (rows, hidden set, previews) |
| `internal/app/session/convstore/append.go` | `WriteSummaryRow` split; `upsertSummaryRow` writes into an open table |
| `internal/app/session/convstore/list_order_test.go` | `annotationTitle` now takes the store's root |
| `internal/ui/resumepicker/picker.go` | the tab-aware state, `switchTab`, `loadOther`, `rowPrompt` |
| `internal/ui/resumepicker/keys.go` | Tab → `switchTab`, Enter arms the import, the confirmation stage |
| `internal/ui/resumepicker/render.go` | `importPrompt`, the generalised prompt rendering, `otherHint()` |
| `internal/ui/resumepicker/tree.go` | the tree's Tab → `switchTab`, the child's prompt string |
| `internal/ui/resumepicker/other_test.go` | **new** — nine cases over the Other tab and the import |
| `out/local-macos-1.4.11/strings-index.txt` | the six import strings indexed; the counts updated |
| `docs/cycles/resume-other-tab/deliverable.md` | **new** — this report |

No file under `internal/cliapp/` changed: the picker already had every
seam the import needs.

## Behaviour changes

1. The **Other** tab lists the Antigravity desktop app's conversations
   instead of nothing.
2. Enter on an Other row raises `[Import this conversation? (y/n)]`.
3. Enter / Y imports the conversation into the CLI's own store and
   resumes it; Esc / N leaves it alone.
4. The Other tab's search reaches the first prompt, and its hint is the
   reference's own.
5. The **CLI** tab is unchanged: same rows, same search, same hint, same
   delete confirmation.

## 硬约束

- **硬约束**: **a panel owns its state, its rows and its keys.** The
  Other tab's rows, the confirmation stage and the tab switch are the
  picker's; the host keeps only the `ResumeConversation` seam it already
  had.
- **硬约束**: **a panel is rebuilt on every open.** `Open()` clears the
  Other tab's rows, the confirmation and the error; the desktop store is
  read again the next time the tab is opened.
- **硬约束**: **a confirmation stage owns the keys it answers with.**
  The armed import confirmation answers Enter, Esc, `y` and `n`, and the
  picker's other keys do not reach it — a typed letter must not land in
  the search field.
- **硬约束**: **the ui tier imports no host.** The picker (tier 5) calls
  `internal/app/session/convstore` (tier 4) directly, the way it already
  called `convstore.DeleteConversation`; no `internal/cliapp` import was
  added.
- **硬约束**: **the store owns the record's assembly.** The import's
  summary row is written by the store, not by the picker; the picker
  only asks for the import and resumes what it gets back.
- **硬约束**: **a literal is a second owner.** The prompt's text lives
  in one constant (`importPrompt`), the hint's shape in one place
  (`hintOptions` / `keyboardHintParts`), and the store's root in one
  place (`DesktopStoreRoot()`).
- **硬约束**: **a citation names the string, not a deleted line.** Every
  comment quotes the string and names the surviving copy of the table
  (`out/local-macos-1.4.11/strings-index.txt`); no comment points at a
  line of the retired dump.

## 存疑

- **存疑**: **the two `%s` of `[Import this conversation? (%s/%s)]` are
  inferred.** The reference's format string has two substitutions; the
  clone renders `(y/n)`, the shape the official documentation prints
  verbatim. That the two substitutions are the *key labels* ("y" and
  "n") is an inference from the delete prompt's identical shape
  (`[Delete? (%s/%s)]` → `[Delete? (y/n)]`), not a fact read out of the
  binary.
- **存疑**: **the desktop store's location is inferred from one
  disassembly.** `fallbackConvImportDirs` joins `"antigravity"` onto the
  backend's own path — a *fallback*, and the backend's own path is not
  read in the same breath. The clone resolves it to
  `$HOME/.gemini/antigravity`, the sibling of the CLI's own
  `~/.gemini/antigravity-cli`, because the two stores are side by side on
  the machine and the reference's own capture shows a populated Other
  tab. The name of the *backend's* path is not confirmed.
- **存疑**: **the import's semantics are a copy, not a re-encode.** The
  documentation says the CLI "clones the history, context, and tool
  trajectories"; the clone copies the database file and folds its
  write-ahead log, which preserves those three by construction. Whether
  the reference re-encodes the trajectory into its own protobuf form
  (and therefore produces a byte-different file) is not known — the
  reference's own strings name a *save* of the imported trajectory
  ("Successfully saved imported trajectory %s"), which suggests a
  re-encode, but the format it writes is not established.
- **存疑**: **the desktop app's index is not the CLI's index.** The
  desktop store's `conversation_summaries.db` carries rows the CLI's own
  store does not have the files for (1387 rows, 4 with a `.db` on disk
  on the machine this was measured on); the clone's reader drops a row
  whose conversation file is absent, exactly as it does for the CLI's own
  store. Whether the reference shows those rows is not established.

## limitations

- The reference's **Other tab was captured on one machine**, with one
  conversation in it. The row shape, the search row and the hint are
  pinned against that capture; the *empty* Other tab's bytes are not
  observed, and the clone draws the search row and the empty state by
  analogy with the CLI tab.
- The reference's **import was never observed end to end** — the
  reference binary was not run against a desktop store with a known
  outcome. The prompt's bytes are pinned by the documentation and by the
  string table; the import's *result* is the clone's own design, checked
  by the nine cases in `other_test.go`.
- The reference's **desktop-app store layout** (`conversations/<id>.db`,
  `annotations/<id>.pbtxt`, `conversation_summaries.db`) was read off the
  machine, not out of the binary. It matches the CLI's own layout, which
  is the strongest available evidence, but it is a first-hand observation
  of one installation.
- The import **writes into the CLI's own store**: it copies a file and
  upserts a summary row. It never deletes anything and never overwrites
  an existing conversation.
