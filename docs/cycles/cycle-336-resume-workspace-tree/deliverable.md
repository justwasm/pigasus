# The /resume picker as a workspace tree — deliverable

**Cycle:** `cycle-336-resume-workspace-tree`
**Subject:** the reference CLI's (agy 1.4.7) `/resume` picker — a workspace
tree: one row per workspace group, the group's conversations indented under
it, and the flat conversation list one `ctrl+f` away.
**Evidence:** 【一手】 the reference's own picker, driven in a tmux pane at
200x50 (`out/resume-tree/ref/*.txt`); 【反编译】 the reference's own
`model` package (`model.normalizeWorkspaceURI` 0x102ac9110 …) and the
group comparator at 0x102bde610; 【一手】 the clone's own tree, rendered
against the same live store and compared cell by cell with the reference's
capture.

---

## 0. Summary

| | before | after |
|---|---|---|
| **the picker's default view** | a flat list of conversations | a **workspace tree** — group rows with their conversations indented under them |
| **the group order** | — | the group of the session's current conversation, then the other workspaces by their newest conversation, the workspace-less group last |
| **the group label** | — | the workspace's own name, refined when two workspaces share one (`tmp/agyprobe`) |
| **the row geometry** | the flat list's | a group row and a conversation row, both as wide as the window, with the reference's cell widths |
| **the colours** | the command picker's palette | the picker's own palette (`38;2;66;133;244` / `38;2;60;64;67` / `38;2;95;99;104`) |
| **`ctrl+e`** | — | expands or collapses the group under the cursor |
| **`ctrl+f`** | — | switches between the tree and the flat "Conversation View" |

---

## 1. The question

agy 1.4.2 drew a flat list of conversations. agy 1.4.7 draws a **tree**:
the conversations are grouped by the workspace they ran in, and each group
is a row with its conversations underneath it.

The question this cycle answers: **what exactly is that tree, and can the
clone draw it the way the reference does?**

Yes. The tree's shape, its labels, its group order, its cell layout, its
colours and its five keys were all measured against the reference's own
picker, and the clone now draws the same rows in the same places.

---

## 2. What the reference does

### 2.1 The tree

【一手】 `out/resume-tree/ref/tree.txt` — the reference's picker at 200
columns:

```
  ▾ gear                                                                          [1 conv]        2h ago
>   南昌天气查询                                                                   17 steps        2h ago
  ▸ dir                                                                           [3 convs]        1h ago
  ▸ openagy                                                                        [1 conv]        1d ago
  ▸ tmp/agyprobe                                                                   [1 conv]        1d ago
  ▸ tmp/agyprobe                                                                   [2 convs]        1d ago
  ▸ [No Workspace]                                                                [486 convs]        1m ago
```

* The first row of a group is the group itself: a marker (`▾` expanded,
  `▸` collapsed) and the group's label.
* The conversations of an expanded group follow, indented by two cells.
* The group row's right-hand block is the conversation count and the age of
  the group's newest conversation; the conversation row's block is the step
  count and the conversation's own age.

### 2.2 The group order

【一手】 The live picker lists `gear` first although its timestamp is older
than `dir`'s. The rule that explains it is the one the decompiled
comparator carries:

【反编译】 `0x102bde610` — the group without a workspace sorts last, then
the groups are ordered by their newest conversation (descending), then by
label (ascending).

【反编译】 `model.preferredGroup` (0x102acbf00) picks the group that holds
the **current** conversation, and `model.defaultExpandedGroup`
(0x102acc070) expands it.

So the reference's order is: the group of the current conversation first,
then the rest by recency, then the workspace-less group. The reference's
session was showing a conversation of the `/Users/gear` workspace, and that
is the group that sits on top — the "gear first" anomaly is the pin, not a
different comparator.

【一手】 The tree opens with that group expanded and the cursor on its first
conversation.

### 2.3 At most five conversations per group

【一手】 Expanding the 486-conversation `[No Workspace]` group grows the
tree from 12 rows to 15 — five of its conversations, not all 486
(`out/resume-tree/ref/tree-nows-expanded.txt`,
`out/resume-tree/ref/tree-dir-expanded.txt`). The cap is per group, not
global: with the three-conversation `dir` group also expanded the model is
15 rows (6 groups + 1 + 3 + 5).

### 2.4 The labels

【反编译】 `model.baseWorkspaceLabel` (0x102ac9460) takes the last path
segment that is not *generic* — `google3`, `src`, `repo`, `code`, `work`,
`workspace`, `packages`, `projects` and anything ending in `.runfiles`
(`model.isGenericPathSegment`, 0x102ac9360).

【反编译】 `model.workspaceLabels` (0x102ac9610) refines the labels of
workspaces that share a base label: the window of path segments that ends
at the base segment grows one segment at a time until the two labels
differ.

【一手】 The live store holds `/tmp/agyprobe` and `/tmp/agyprobe/repo`. Both
have the base label `agyprobe` (the `repo` segment is generic) and both are
refined to `tmp/agyprobe` — the reference's own picker shows both rows
with the same label.

### 2.5 The keys

【一手】 All measured in the reference's own pane:

| key | what the reference does |
|---|---|
| `enter` on a group row | expands or collapses the group; the cursor stays on the group row |
| `enter` on a conversation | resumes it |
| `ctrl+e` | expands or collapses the group **under the cursor** — the other groups are untouched |
| `ctrl+f` | switches between the tree and the flat "Conversation View" |
| `f2` / `f4` on a group row | nothing at all |
| `esc` | clears the search, then closes the picker |

【一手】 The hint follows the group under the cursor: `ctrl+e Collapse`
while that group is expanded, `ctrl+e Expand` while it is collapsed — even
when another group is expanded at the same time
(`out/resume-tree/ref/tree-cursor-nows.txt`).

A search switches the picker to the flat filtered list; the grouped flag
stays on, so the hint then offers `ctrl+f Conversation View` and no
`ctrl+e` at all.

### 2.6 The colours and the cells

【一手】 From the capture's own escape sequences:

| | |
|---|---|
| the cursor, the keys of the hint | `38;2;66;133;244` |
| the right-hand cells of a row | `38;2;60;64;67` |
| the labels of the hint, the page indicator | `38;2;95;99;104` |
| the inactive tab, the rule above the picker | `38;2;218;220;224` |

【一手】 The cell layout at a 200-column window — both rows are 200 cells
wide, and both right-hand blocks end at the last column:

```
group row        2 cursor + 2 marker + 170 label + 12 count + 14 age
conversation row 2 cursor + 2 indent + 170 title + 12 steps + 2 + 12 age
```

The count cell ends at column 185, the age cell at column 199; on the
conversation row the step count ends at column 185 and the age at column
199 as well.

> The first measurement of this cycle was wrong: the conversation row was
> read as 194 cells wide because the *rune* length of the row was compared
> with the reference's, and the reference's row carries a CJK title. The
> cell width is 200 for both rows — the same as the group row's.

---

## 3. What the clone does now

### 3.1 `internal/resumetree` — the model

The package rebuilds the tree from the store: the workspace of a
conversation comes from the reference's own conversation index
(`conversation_summaries.db`) and from its metadata cache; the group order,
the labels and the pin are computed here.

```go
groups := resumetree.Build(items, pinnedKey)
```

`internal/resumetree/resumetree.go` holds the recovered label rule and the
comparator; `internal/resumetree/store.go` reads the index and the cache.

### 3.2 `internal/cliapp/picker_resume_tree.go` — the view

The tree's rows, the group's marker, the five-children cap, the hint, the
keys and the cursor's target (the cursor follows its conversation when a
group expands or collapses under it).

### 3.3 The colours

The clone's `/resume` picker used the *command* picker's palette
(`38;2;38;139;210` for the cursor, `38;2;88;110;117` for the cells). The
reference's own picker uses `38;2;66;133;244` and `38;2;60;64;67`; the
clone's resume picker now uses the reference's values in both views.

---

## 4. The measurement

The clone's tree, rendered against the same live store the reference was
driven on, and the reference's own capture, compared cell by cell:

| | reference | clone |
|---|---|---|
| the group row | 200 cells, count ends at 185, age at 199 | 200 cells, count ends at 185, age at 199 |
| the conversation row | 200 cells, steps end at 185, age at 199 | 200 cells, steps end at 185, age at 199 |
| the group row's escape sequences | `1m` label, `38;2;60;64;67` cells | the same |
| the conversation row's escape sequences | blue title, italic blue cells | the same |
| the flat row's escape sequences | blue title, italic blue cells | the same |

The rows differ in their *content* only where the store grew between the
two runs (the counts, the ages).

---

## 5. 硬约束 / the load-bearing rules

- **The tree is the default view.** A `ctrl+f` away is the flat list; the
  clone's `resumeGrouped` flag is the reference's own grouped flag, and the
  hint's `ctrl+f` label follows it ("Grouped View" in the flat list,
  "Conversation View" in the tree and in a search).
- **A group shows at most five conversations.** The cap is on the model,
  not on the window: the tree's page indicator counts the rows of the
  model, and the reference's says "of 15 items" for a tree with a
  486-conversation group expanded.
- **The group of the current conversation is pinned to the top** and is the
  group that opens expanded. Without the pin the reference's own order
  (the `/Users/gear` group first) cannot be reproduced.
- **The tree's rows are the flat list's conversations.** The workspace
  comes from the reference's index, the title and the step count from the
  conversation store.
- **`f2` and `f4` do nothing on a group row.** They act on a conversation.

## 6. 存疑 / open questions

- **The count cell's width is not proven to be fixed.** The reference's
  cell is 12 cells wide and the widest count in the live store is
  `[486 convs]` (11 cells) — the two readings ("a fixed 12" and "the
  widest count plus one") cannot be told apart without a store whose
  widest count is short. The clone uses a fixed 12.
- **`joinLabelSegments`' fourth branch was never observed.** A label that
  needs more than three path segments ends in `/…/`; no workspace in the
  live store needs one.
- **The cursor's fallback when its group collapses is not the reference's
  own behaviour.** The reference was only observed collapsing a group the
  cursor was *on*; the clone keeps the cursor on the group row when the
  conversation under it disappears.
- **The pin's own field in the reference's group struct was not found.**
  The pin is inferred from the live order and the decompiled
  `preferredGroup` / `defaultExpandedGroup`.
- **The clone hides its own running conversation from the list** (an
  earlier, tested clone decision), while the reference lists the
  conversation it is showing. A group whose only conversation is the
  session's own therefore disappears from the clone's tree.

## 7. Limitations

- The measurements come from **one** reference build (agy 1.4.7), **one**
  machine and **one** store. The store's shape — six groups, one of them
  with 486 conversations — is what made the five-children cap visible.
- The reference's **"Other" tab** is not implemented: it shows the hub's
  conversations, a data source the clone does not read. The tab and its
  hint are unchanged.
- The tree's **page indicator** follows the flat list's format
  (`[1-10 of N items]`), with N counting the *rows* of the tree model and
  not the conversations.
- The clone's tree reads the reference's index for the workspaces, so a
  store without that index groups everything under `[No Workspace]` — the
  same fallback the reference has.

## 8. The files

| | |
|---|---|
| `internal/resumetree/resumetree.go` | the label rule, the grouping, the comparator, the pin |
| `internal/resumetree/store.go` | the readers of the reference's conversation index and metadata cache |
| `internal/resumetree/resumetree_test.go` | the label rule, the group order, the pin, the item order |
| `internal/cliapp/picker_resume_tree.go` | the tree's rows, the hint, the keys, the cursor's target |
| `internal/cliapp/picker_resume.go` | the flat list, the picker's own colours, the hint |
| `internal/cliapp/resume_tree_test.go` | the tree's rows, the geometry, the keys, the hint |
| `internal/cliapp/resume_test.go` | the flat list's tests; the cursor test follows the tree's default |
| `internal/cliapp/tui_run.go` | the picker's tree state |
| `internal/cliapp/tui_update_keys.go` | the key dispatch |
| `internal/cliapp/slash_commands_handlers.go`, `internal/cliapp/tui_entry.go` | the picker's open and reload |
| `out/resume-tree/ref/*.txt` | the reference's own picker, captured in tmux |
| `out/resume-tree/labelrule.py` | the label rule, as a Python port of the decompiled code |
