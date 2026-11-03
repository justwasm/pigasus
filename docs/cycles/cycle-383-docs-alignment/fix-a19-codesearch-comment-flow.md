# A-19 — the `/codesearch` file viewer's comment flow

> **Finding.** `docs/cycles/cycle-383-docs-alignment/slice-a-cli-commands.md:462`
> — *"the `/codesearch` file-viewer comment flow does not exist"*.
> **Branch.** `fix/a19`. **Status:** closed on this branch.

## The problem

The reference's `/codesearch` command opens a file viewer for a
result. Inside the viewer the user can leave a note on a line of the
file, and the note is marked in the gutter; when the viewer is left the
CLI asks whether the pending notes travel to the agent.

The clone had **none** of it: no file-viewer subview, no comment
gutter, and no `Unsent Comments` string anywhere in the tree
(`slice-a-cli-commands.md:471`).

## The evidence

### The reference binary

| | |
|---|---|
| binary | `~/.local/bin/agy` (1.4.11, macOS arm64) |
| sha256 | `34423fc9b0af3841e7646aa72a2d40ffeab8218b22d1cb99eac7355f9eba31e3` |
| size | 197 589 952 bytes |

The findings below were taken from **that** file, not from a captured
dump of an older release; the offsets are file offsets in it.

### The wire format 【strings】

The format the comments travel in is one constant in the whole binary:

```
offset 0x359ebb2  <file>%s:%d:%s\n      (15 bytes, exactly one occurrence)
offset 0x359ebc1  Unsent Comments       (9 bytes past the format)
```

`devutils/macho_strrefs.py` finds exactly **one** ADRP+ADD reference to
that address — `0x102bed52c` — and the disassembly there loads the
length **15** (`orr x1, xzr, #0xf`) and calls the fmt helper with
**three** arguments. The function is `(*DiffModel).formatDiffComments`
(fresh seeds: `0x102bed2b0…0x102bed610`), and its callers are
`attemptDismiss`, `updateConfirm` and
`(*ToolConfirmationModel).collectDiffComments` /
`updateExpandedDiff`.

So the reference's format is

```
<file>PATH:LINE:BODY\n
```

one comment per line — the literal tag `<file>`, the file's path, the
line number and the comment's body.

The documentation's schematic (`official-docs/cli/commands/codesearch.md:95`)
is `<file>:<line>: <comment>`: it drops the file's own name and puts a
space after the line's colon. The **binary wins** — this is a wire
format and the model parses it.

### The screen 【strings】

| offset | bytes | meaning |
|---|---|---|
| `0x359ebc1` | `Unsent Comments` | the screen's header |
| `0x35594ee` | `send + close` | the `Y` answer's label |
| `0x359ead1` | `discard + close` | the `N` answer's label |
| `0x3589572`, `0x37aa529`, `0x37f5ae0` | `delete comment` | the `d` binding's label (three copies) |
| `0x3559506` | `  Search: ` | the `/` prompt |
| — | `💬` | the gutter marker (the doc names it, `codesearch.md:86`) |

### The footer 【一手 from the doc】

The footer is composed at runtime — there is no `pgup/pgdown` literal
in the binary — so the documentation is authoritative here
(`official-docs/cli/commands/codesearch.md:74-78`):

```
↑/↓ scroll · pgup/pgdown page · shift+g bottom · g top · c comment · ctrl+g editor · / search
```

## What was built

### 1. The comment model — `internal/ui/views/linecomments` (new)

A shared ui-tier package that owns, once:

* `Comment` — a line number and a body;
* `Store` — the comments of **one file**, in line order (`Add`
  replaces the comment a line carries, `Delete`, `At`, `All`, `Len`,
  `Reset`);
* `Marker` — the gutter marker `💬`;
* `Format` / `FormatAll` — the reference's wire format, byte for byte;
* the **Unsent Comments** screen: `Header` (`"Unsent Comments"`),
  `Decide` (the three-way answer), `ScreenRows`.

`Decide` answers only to the **capital** letters: `Y` sends and closes,
`N` discards and closes, `Esc` returns to the viewer, and a small `y`
or `n` answers nothing. The screen is **modal** — it claims every key
under it, so a small `y` is swallowed rather than reaching the viewer
(invariant 101's precedent).

### 2. The text buffer — `internal/ui/foundations/textbuffer` (new)

A small multi-line buffer with a cursor and its own reverse-video
cursor span. The comment editor and the `/` search prompt are the two
consumers; neither owns the other's rules.

### 3. The viewer — `internal/ui/views/codesearchpanel`

The viewer gained an explicit **cursor** (a line the panel owns), a
comment store **per file**, a comment editor, the `/` search prompt and
the modal Unsent screen.

* `c` adds (or edits) a comment on the cursor's line; `d` deletes it.
* The gutter shows the 💬 marker on a commented line — a three-cell
  column between the cursor arrow and the line number, so the gutter
  stays aligned.
* `Esc` leaves the viewer: with pending comments it opens the Unsent
  screen, without them it closes the file.
* `Y` sends the comments as the user's next message and closes the
  panel; `N` discards them and closes the panel; `Esc` returns to the
  viewer with the comments intact.
* `g` / `shift+g` move the cursor to the first / last line, `↑`/`↓`
  scroll, `pgup`/`pgdown` page, `ctrl+g` opens the external editor,
  `/` searches.

`c`, `d` and `/` are intercepted **before** the list-navigation
classifier, which would otherwise read `d` as "page down" and `q` as
"cancel"; `ctrl+g` is checked before the navigation keys too.

The cursor and the window are kept apart: the **offset** is the host's
fact (the panel is a viewer panel, invariant 85), the **cursor** is the
panel's, and the visible window follows the cursor (invariant 86) by
`offset = clamp(cursor - budget/2)`. The A-18 test that pins "↓ moves
the offset by one" stays green.

### 4. The host — `internal/cliapp/codesearch_host.go`

One new seam, `Seams.SendComments`, and the host method behind it.
The panel decides that the comments are sent and hands over the text in
the reference's wire format; the host closes the panel and starts the
turn with that text as the user's next message. The OS write (there is
none here) and the model turn stay host-side.

## Reuse, and why there is no duplication

The finding's premise — that the reference's `/diff` viewer already
carries this flow, and that the clone could reuse it — is **wrong for
this tree**:

* `internal/ui/views/diff/keytable/` and `internal/ui/views/diff/fullfile/`
  **do not exist**;
* `internal/ui/views/diffpanel/` has **no** comment flow — a grep for
  `comment|Unsent` over the package finds nothing.

There was therefore nothing to reuse, and the shared home had to be
**created** rather than found. The new package is shared by
construction (the reference has one comment model, one marker, one wire
format and one screen), and it is placed in the **ui tier** so that the
`/diff` panel can adopt it without an import cycle — but the `/diff`
comment flow is **not built yet** (A-21), so the package has **one
consumer** today. That is recorded here rather than hidden.

Nothing was duplicated: the diff panel's inline-action code was left
alone, and the clone's own viewer mechanics (the line list, the
highlight, the external editor) were reused as they were.

## Exact bytes

| what | bytes |
|---|---|
| the footer | `  ↑/↓ scroll · pgup/pgdown page · shift+g bottom · g top · c comment · ctrl+g editor · / search` (the two leading spaces are the panel's own indent) |
| the wire format | `<file>%s:%d:%s\n` → `<file>PATH:LINE:BODY\n` |
| the screen header | `Unsent Comments` |
| the `Y` label | `send + close` |
| the `N` label | `discard + close` |
| the gutter marker | `💬` |

## Files touched

New:

* `internal/ui/views/linecomments/linecomments.go` — the model, the
  marker, the wire format.
* `internal/ui/views/linecomments/unsent.go` — the Unsent screen.
* `internal/ui/views/linecomments/linecomments_test.go`
* `internal/ui/foundations/textbuffer/buffer.go` — the shared buffer.
* `internal/ui/views/codesearchpanel/comment_test.go`

Changed:

* `internal/ui/views/codesearchpanel/fileviewer.go` — the cursor, the
  comment store, the editor, the search prompt, the footer.
* `internal/ui/views/codesearchpanel/panel.go` — `Seams.SendComments`,
  the per-file stores, the Unsent screen, the send/discard actions.
* `internal/ui/views/codesearchpanel/panel_keys.go` — the Unsent screen
  is routed first (modal), then the viewer.
* `internal/ui/views/codesearchpanel/panel_render.go` — the Unsent
  screen's rows.
* `internal/ui/views/codesearchpanel/fileviewer_test.go` — the footer's
  assertion.
* `internal/features/tools/codesearch/row.go` — **out of the finding's
  scope, see below**: `Row` gained a sibling `MarkedRow` (the three-cell
  marker column) and the cursor arrow no longer depends on the line
  carrying a match.
* `internal/cliapp/codesearch_host.go` — the `SendComments` seam and the
  host method.

### The one out-of-scope touch

`internal/features/tools/codesearch/row.go` is **not** in the finding's
file list. It is touched because the marker column has to be rendered
by the row renderer, and the row renderer is that file's owner. The
change is additive: `Row`'s own bytes are **unchanged** (the results
list's caller passes `cursor & isMatch`, so the arrow appears exactly
where it did), and `MarkedRow` is the new sibling. The file is a tier-2
features package; the panel (tier 5) imports it as it did before.

## Tests added

`internal/ui/views/linecomments/linecomments_test.go`

* the wire format is `<file>%s:%d:%s\n` byte for byte;
* `FormatAll` emits one line per comment, in line order;
* the store's semantics: `Add` replaces, `Delete` reports, `All` is
  ordered;
* `Decide` accepts `Y`, `N` and `Esc` and **rejects** a small `y` and a
  small `n`;
* `ScreenRows` carries the `Unsent Comments` header, the wire line and
  the two answer labels.

`internal/ui/views/codesearchpanel/comment_test.go`

* a comment marks its line with the 💬 marker;
* `d` deletes the comment;
* `ctrl+d` is **not** "delete comment";
* `Esc` with a pending comment opens the Unsent screen, `Esc` without
  one closes the file;
* a small `y` / a small `n` do **not** answer the screen;
* `Y` sends the wire text to the host seam;
* `N` discards and closes;
* `Esc` returns to the viewer;
* a comment survives a cancelled Unsent screen;
* the stores are per file;
* the comment editor claims every key under it;
* the Unsent screen claims every key under it;
* `d` beats the pager;
* `/` finds the next line.

`internal/ui/views/codesearchpanel/fileviewer_test.go`

* the footer's exact bytes (the assertion was widened from the A-18
  fragment to the whole legend).

## Verification

```
$ go build ./cmd/... ./internal/...
(clean)

$ go test ./internal/ui/views/... ./internal/cliapp/... ./internal/core/architecture/ ./internal/features/tools/codesearch/... -count=1
(clean)

$ go test ./cmd/... ./internal/... ./endpoints/... ./configdir/... -count=1
```

The full run is green apart from load-induced flakes that are
unrelated to this change and pass in isolation:

| test | verdict |
|---|---|
| `internal/piagentbridge` `TestStallWatcher_TimerResetsOnEachEvent` | timing test; passes in isolation (`-count=3`) |
| `internal/remotecontrol` `TestLanguageServer23StreamingMethodsParity` | timing test; passes in isolation (`-count=2`) |
| `internal/cliapp` `TestWrapRowsQuadratic` | machine-load timing test; green on the second full run |
| `internal/infra/agyproxy` `TestBalanceMaxAttemptsExhausted`, `TestBridge_AggregateAccountFailover` | known flakes (tempdir) |

`gofmt -l` is clean on every file this branch touched.

The layering guard (`internal/core/architecture`) is green: the two new
packages are tier 1 (`internal/ui/foundations/textbuffer`) and tier 5
(`internal/ui/views/linecomments`), and no ui-tier package imports a
host.

## 硬约束

* The wire format is the **binary's**, not the doc's schematic:
  `<file>%s:%d:%s\n`.
* The Unsent screen answers to the **capital** letters only.
* The screen is modal and claims every key under it.
* The panel owns its cursor, its comment stores, its rows and its keys;
  the host owns the facts it cannot know (the width, the editor, the
  close, the model turn).
* The `d` binding is the panel's own; it is checked **before** the
  list-navigation classifier, which would otherwise read `d` as
  "page down".
* The comment editor's keys are claimed by the editor; a key the editor
  does not own never reaches the viewer.
* The viewer's offset is the host's fact; the panel's cursor is the
  panel's; the visible window follows the cursor.

## 存疑

* **The doc and the binary disagree about the wire format.** The doc
  says `<file>:<line>: <comment>`, the binary says
  `<file>%s:%d:%s\n`. The binary wins here, but the *doc* may describe a
  different (earlier or later) surface than the 1.4.11 binary, and the
  doc was crawled from a release whose exact build is not pinned in
  this tree. The clone follows the binary it was measured against.
* **The exact bytes of the reference's Unsent screen rows are unknown.**
  The binary carries the header (`Unsent Comments`) and the two answer
  labels (`send + close`, `discard + close`), but not the rows
  themselves. The clone shows the wire lines under the header — that is
  【推测】, not a recovered byte.
* **"Send" may have a destination other than the agent's next
  message.** The clone sends the comments as the user's next message
  (the doc's own wording, `codesearch.md:97`). The binary's
  `collectDiffComments` collects them into a tool-confirmation, which
  is the *app* review flow, not the CLI's — whether the CLI's own "send"
  reaches the model the same way is not recovered. 【推测】
* **The `/diff` comment flow (A-21) is not built.** The shared package
  therefore has one consumer, and the "shared by construction" claim is
  a statement about the reference's shape, not about the clone's
  current tree.
* **`internal/features/tools/codesearch/row.go` was outside the
  finding's file list.** It is touched for the marker column; `Row`'s
  bytes are unchanged and the change is additive.

## limitations

* The reference was read statically: the binary's strings and their
  ADRP+ADD references, plus a disassembly of the one reference to the
  format constant. **No live run of the reference's own viewer was
  made**, so the *key order* of the reference's screen (does `Y` come
  before `N` in the prompt line?) is not recovered.
* The clone's own footer is asserted byte for byte against the
  **documentation**, not against a captured reference screen.
* The comment editor's own key set (`ctrl+g` opens the editor, `esc`
  cancels) follows the footer and the doc's prose; the reference's
  editor is not recovered byte for byte.
