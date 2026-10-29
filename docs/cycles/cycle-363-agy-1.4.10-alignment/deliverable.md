# Cycle 363 deliverable: the clone against agy 1.4.10

**Cycle:** `cycle-363-agy-1.4.10-alignment`
**Subject:** the vendor's 1.4.10 release notes carry **six** bullets. This
cycle maps every one of them onto the clone: four are implemented, one is
not applicable to the clone's architecture, and one is recorded as open
with the reason.
**Evidence:** 【一手】 first-hand against the 1.4.10 binary (its own string
table, its own recovered symbol table) · 【反编译】 a symbol recovered at a
confirmed address · 【strings】 string-table hits only · 【推测】 hypothesis

---

## 0. The short answer

| # | the upstream bullet | the clone |
|---|---|---|
| 1 | streaming writes whole runs of text and scrolls off the top into the terminal's scrollback | **open** — the clone's renderer is bubbletea v2's, and it has no scrollback; the reference owns a `viewport` package that has (§6) |
| 2 | about 100 MB of start-up memory goes back to the OS 15 s after the interactive UI opens | **done** — `internal/memtrim` (§2) |
| 3 | the after-compaction size counts what the summary carries into the next prompt | **done** — `internal/compaction` (§3) |
| 4 | a subagent keeps the task and the parent's ID through a compaction, and its summary keeps the parent's 10 most recent messages | **done** — `internal/subagenttools` (§4) |
| 5 | images are capped at 2000 px per side, large PNGs travel as JPEGs, a JPEG labelled PNG is not rejected | **done** — `imagery` (§5) |
| 6 | the `/browser` subagent gets the current page's snapshot when the page is in an isolated browser context | **not applicable** — the clone speaks no Chrome DevTools MCP and selects no page (§7) |

Four of six are done, one is not applicable, one is open.

---

## 1. Where the 1.4.10 notes came from

The *served* changelog feed — the one `agy changelog` prints — **lags**: the
feed re-captured first-hand against the 1.4.10 binary on 2026-10-29 is
184,882 bytes / 1,054 lines / 80 sections / 895 bullets and its **newest
section is still 1.4.9**. The 1.4.10 notes therefore live only in the
vendor's own repository, and they are recorded **verbatim** in
`internal/cli/testdata/changelog_upstream_1.4.10.md`, the same way the
1.4.8 and 1.4.9 notes were. The clone's changelog still prints exactly
what the reference prints.

The 1.4.10 binary itself **is** on this machine
(`~/.local/bin/agy`, sha256 `35605054a9dc4591dd7e6ea83a9392238153d4c55d265512ed9b5b7f97cc0423`,
captured 2026-10-29), so this cycle is not limited to the notes: the
recovered symbol table of that binary (`out/local-macos/seeds/seeds.txt`,
167,678 names against 166,181 at 1.4.8; 3,751 added, 2,255 removed) is the
evidence behind §6.

The version pin and the drift lock move with the binary — that is the RE
refresh's own work, in `internal/cli/version.go` and
`internal/referencedrift/reference.lock.json`, not this cycle's.

---

## 2. Item 2 — the start-up memory goes back to the operating system

> "Improved memory use shortly after startup: the CLI now returns about
> 100 MB of memory left over from startup to the operating system 15
> seconds after the interactive UI opens; print and headless modes are
> unaffected."

【一手】 the note's own numbers: 15 seconds, the *interactive* UI, and
print/headless explicitly untouched.

The clone's new `internal/memtrim` package is the whole of it:

- `memtrim.Delay` is `15 * time.Second` — the note's number, not a guess.
- `memtrim.Release` calls `runtime/debug.FreeOSMemory`, the call that
  collects and hands the freed heap pages back to the OS.
- `memtrim.After(delay)` arms the release on a timer and returns the
  cancel function, so a session that ends first leaves no timer behind.
  A delay of zero or less releases on the calling goroutine — the seam
  the tests use.

The wiring is in `internal/cliapp/tui_entry.go`, in `runLiveTUI` only:
the trim is armed right before `prog.Run()` and cancelled by a `defer`.
**The print path never arms it** — the headless run never builds the
interactive UI's start-up state (the tool catalogue, the plugin registry,
the prompt and the banner), so there is nothing of that size to give back.
That is the note's "print and headless modes are unaffected", and it is a
property of *where* the call sits, not of a flag.

`runtime/debug.FreeOSMemory` is in the reference's own symbol table at
1.4.10 (`runtime/debug.FreeOSMemory`, `0x100486a70`; the runtime's own
`runtime/debug.freeOSMemory`, `0x100088a50`), so the mechanism the clone
uses is the mechanism the reference has.

---

## 3. Item 3 — the after-compaction size counts what the summary carries

> "Improved the after-compaction size shown in the compaction marker and
> in `/context` to also count what the summary carries into the next
> prompt, such as your earlier requests and artifact, subagent, and
> running-task snapshots, so it is closer to the real size; the `(N
> summary)` label still shows the summary alone."

Two halves, and the second half is the one that is easy to break: the
*after* figure grows, the *summary* figure does not.

The clone's resume block is now split in two:

- `compaction.CarriedBlock(userRequests)` — the header and the `# User
  Requests` list: the part that carries the conversation forward **besides**
  the summary. This is what the after-figure counts.
- `compaction.ResumingBlock(userRequests, summary)` — the carried block's
  bytes followed by the summary. **The bytes are unchanged**: the split is
  a refactor, not a reformat, so the prompt the model sees after a
  compaction is byte-for-byte what it was before this cycle.

`compaction.ComputeCompactTokens(..., carriedTokens)` then adds the
carried size to the *after* figure only:

```go
a := afterTokens
if overhead > 0 {
    a = overhead + st + carriedTokens   // st is the summary alone
}
return b, a, st                        // st is what "(N summary)" shows
```

The call site is `internal/cliapp/slash_compact.go`:
`applyGenericCompactSuccess` computes
`carriedTokens = compaction.EstimateTokens(compaction.CarriedBlock(requests))`
from the *same* request list it puts into the resume block, so the figure
and the block cannot drift apart.

The clone has no artifact, subagent or running-task snapshots — those are
the reference's own checkpoint sections — so what is counted here is the
header and the request list. The reference's note names four kinds of
carried content; the clone carries one.

`/context` shows the live token grid (`internal/contextpanel`), which
counts the session's own messages; the compaction marker is the row this
cycle changed (`compaction.StatsRow`). The note's "`/context`" clause has
no separate after-compaction row in the clone's panel to change.

---

## 4. Item 4 — a subagent keeps its task, the parent's ID, and the parent's recent messages

> "Fixed subagents losing the task their parent agent gave them, and the
> parent's ID, after their context was compacted, including with
> `/compact`; a subagent's summary now keeps the parent agent's 10 most
> recent messages."

【一手】 the note's own numbers: the parent's **10** most recent messages.

A subagent's conversation is two conversations in one: the parent's half
(the task it was given, the messages it was sent afterwards) and the
child's own half (the turns it worked through). A compaction rewrites the
child's half into a summary — and a summary that only knows about the
child's own turns leaves the child without its task and without the id it
has to answer on, because those are the *oldest* messages of the
conversation and therefore the first ones a summary drops.

**What the clone keeps.** `internal/subagenttools/parentcontext.go` holds
the parent's half:

- `ParentContext.ID` — the parent's conversation id (the registry's
  `ParentID()`, and the dependency's own id as the fallback).
- `ParentContext.Task` / `TaskAt` — the task the parent gave.
- `ParentContext.Messages` — the parent's later messages, oldest first,
  **at most `MaxParentMessages` (10)** of them: `Record` drops the oldest
  as it appends, so the ten the note names are the ten the summary
  carries.

**What the summary carries.** `internal/subagenttools/compaction.go`
renders it in two parts:

- the parent's half, in the reference's own message formats — the task in
  the `<SYSTEM_MESSAGE>` envelope the reference delivers it in
  (`agentmessage.Render`), the later messages in the reference's own
  one-line format (`agentmessage.RenderLine`, new in this cycle);
- a digest of the subagent's own work: what it said, what it called, what
  the call answered, bounded by `MaxDigestLines` (40).

**How it reaches the compaction.** The clone's subagent runs on PiG's
`coding.Session`, and PiG compacts a session by asking the model to
summarize the history that no longer fits. PiG's `session_before_compact`
hook can **replace** that summary, but there is no way to influence the
model's summarization prompt (`internal/codingagent/compaction` is PiG's
own internal package) or to amend a summary after the fact. So the clone
authors the summary itself: an inline PiG extension
(`ParentContextExtensionName` = `subagent-parent-context`) answers the
hook with `extension.CompactionResult{Summary, FirstKeptEntryID,
TokensBefore}` — the parent's half beside the digest.

Both compaction paths go through that hook in PiG
(`coding/session.go`: the manual `compact` and the auto-compaction), so
the fix covers `/compact` and the automatic compaction alike, and it
takes effect from the next request in the same turn.

The extension is loaded into the subagent's **own** session
(`internal/subagenttools/session.go`, `createSubagentSession`), through
the same `*extension.ExtensionRuntime` the built-in extensions were loaded
into — the runtime must be the one the session's runner uses, or the
handler never runs (the clone's own invariant #2). The parent context
itself is built in `runChild` (`internal/subagenttools/launch.go`) before
the session is created, and every message the parent sends the child
afterwards is recorded into it.

【推测】 The reference's own summary is written by its model, and its
exact shape is not recoverable from the release notes. The clone's
summary is therefore its own document: the three things the note names,
plus a mechanical digest of the subagent's own work.

---

## 5. Item 5 — images are capped at 2000 px per side, and the media type follows the bytes

> "Fixed conversations with many or large images, such as screenshots or
> pasted images, being rejected for exceeding the model's request size
> limit: images are now capped at 2000 pixels per side and large PNGs
> are sent as JPEGs when that is much smaller, and an image saved as JPEG
> but labeled PNG, such as a pasted image or browser screenshot, is no
> longer rejected for a mismatched type."

【一手】 the note's own numbers: **2000** pixels per side (the cap was
2576 — the value the 1.3.2 notes put in place), and the PNG→JPEG rule.

The public `imagery` package carries all three rules:

- `imagery.MaxSide` is **2000** — the ceiling moves *down*.
- `imagery.LargePNGBytes` (64 KiB) and `imagery.JPEGMuchSmaller` (2) pin
  "large" and "much smaller": a PNG above the size is offered to the JPEG
  encoder and wins only when the JPEG is at most half the PNG's size.
  【推测】 the note names neither threshold.
- `imagery.Prepare(data)` returns the bytes **and the format it actually
  produced**, and the callers use that format for the media type. This is
  what makes "a JPEG saved as a PNG" work: the bytes say JPEG, so the
  request says `image/jpeg` — the *name* the bytes arrived under is never
  consulted. The conversion flattens transparency onto white, because a
  JPEG has no alpha channel.

`imagery.Prepare` is the single entry point of the three attachment
paths:

- `internal/clipboard/media.go` (`mediaFromBytes`) — a pasted picture;
- `internal/clipboard/paste.go` (`AcceptPaste`) — the same path for the
  drag-and-drop route;
- `internal/remotecontrol/message_image.go` — the Web UI's attachment.

`imagery.ScaleIfOversized` survives as the scale-only wrapper it became:
the format-changing half moved into `Prepare`.

---

## 6. Item 1 — the streaming scrollback (open)

> "Improved streaming of agent replies: lines that scroll off the top of
> the screen are now scrolled into your terminal's scrollback instead of
> repainting the whole screen, and text wrapping writes whole runs of
> text at a time, so streaming writes about 70% fewer bytes to the
> terminal and stays much smoother over slow SSH connections."

【反编译】 the 1.4.10 binary's own symbol table names the machinery the
note describes:

| address | symbol |
|---|---|
| `0x102aeb830` | `jetski/cli/viewport/viewport.(*InlineStrategy).FlushToScrollback` |
| `0x102aec2f0` | `jetski/cli/viewport/viewport.(*InlineStrategy).CommitScrollback` |
| `0x102aec370` | `jetski/cli/viewport/viewport.(*InlineStrategy).TakeDeferredPush` |
| `0x102aec380` | `jetski/cli/viewport/viewport.computeSplitPoint` |
| `0x102aec400` | `jetski/cli/viewport/viewport.buildScrollbackContent` |
| `0x102aecaa0` | `jetski/cli/viewport/viewport.prepareScrollbackContent` |
| `0x102b68840` | `jetski/cli/model/model.(*ConversationModel).deferScrollbackPush` |
| `0x102b68a00` | `jetski/cli/model/model.(*ConversationModel).pushToScrollback` |

So the reference owns a `viewport` package with two strategies
(`InlineStrategy`, `AltScreenStrategy`), and the inline one *writes* the
lines that scroll off the top into the terminal's scrollback instead of
repainting the screen.

**Why the clone does not have it.** The clone does not own its renderer:
every byte the TUI writes goes through `charm.land/bubbletea/v2` v2.1.0's
renderer (`cursed_renderer.go`), and that module has **no** scrollback
notion at all — the string `scrollback` does not occur in it. Bubbletea's
renderer repaints the screen it owns; it never writes into the terminal's
scrollback, and it never wraps "whole runs of text at a time" either.

The clone's own transcript machinery is closer to the reference than the
renderer is: the TUI keeps a viewport model of its own
(`internal/tuiviewport`, and the inline/alt-screen switch in
`internal/cliapp/tui_view.go`), and the cycle-256 seam
(`internal/cliapp/renderer.go`) already documents that a real fix would
have to "wrap or replace the bubbletea/v2 renderer".

**What it would take.** A renderer the clone owns, or a fork of
bubbletea's, with two behaviours: (a) when the inline view's height grows
past the terminal's, the lines that leave the top are written *to the
terminal* (into the normal buffer, so they land in the terminal's
scrollback) instead of being repainted — the reference's
`FlushToScrollback`/`CommitScrollback`/`buildScrollbackContent` trio;
(b) the text wrapper writes whole runs at a time.

This is deliberately **not** approximated here: a half-implementation
would move the transcript into the terminal's scrollback while the
alternate-screen mode still repaints it, and the two modes must agree.
The cycle that takes it on should start from the symbol table above.

---

## 7. Item 6 — the browser snapshot in an isolated context (not applicable)

> "Fixed the `/browser` subagent not receiving the current page's
> snapshot when the selected page belongs to an isolated browser context,
> such as a page opened with `isolatedContext` or attached from an
> Incognito window."

The reference's browser agent drives the browser through the
**chrome-devtools MCP server**, and the snapshot it asks for carries a
**page id**; the bug was that the request did not reach the page when the
page lived in an isolated (incognito) browser context.

The clone's browser surface is its own CDP client, not that MCP server:

- `internal/browsersubagent` launches a subagent whose tool set is the
  clone's own browser tools (`internal/tools/browser.go` over
  `browsercdp`);
- `browsercdp` attaches to **one** page — the tab the clone opened in the
  user's Chrome, or the one it started itself — and every page-scoped
  command goes to that page. `browsercdp.Actions.ListPages` reads
  `Target.getTargets` and *lists* the pages with their target ids, but
  there is no selection: no tool acts on a page other than the attached
  one, and there is no page id in any request the clone makes;
- there is no snapshot API and no isolated browser context anywhere in
  `browsercdp`, `agentbrowser` or `internal/tools` — the word
  `isolatedContext` does not occur in the clone.

So the failure the note describes cannot arise in the clone: the clone's
"current page" is the page it is attached to, and it is never in an
isolated context. The *precondition* is missing too — the clone's browser
agent is never handed a page snapshot to begin with, so there is nothing
for an isolated context to break. This is the same conclusion cycle 362
reached for the 1.4.9 sibling of this bullet ("the clone does not speak
Chrome DevTools MCP").

---

## 8. 硬约束 / hard constraints

- **The trim is armed in the interactive TUI only.** `memtrim.After` is
  called from `runLiveTUI` and nowhere else. A headless run must not
  pay for it (the note: "print and headless modes are unaffected").
- **The resume block's bytes did not change.** `CarriedBlock` +
  `summary` is byte-for-byte the old `ResumingBlock`. A compaction must
  not change what the model sees, only what the stats row counts.
- **The summary figure stays the summary alone.** `(N summary)` must not
  grow; only the *after* figure counts the carried block.
- **The media type follows the bytes.** No caller may label an image by
  the name it arrived under; the format `imagery.Prepare` reports is the
  one that is sent.
- **The parent context lives in the subagent's session.** The extension
  is loaded into the session that will be compacted, through the shared
  `*extension.ExtensionRuntime`; a context kept in the parent process
  would not reach the child's compaction.
- **The parent's ten most recent messages are a hard bound.** The
  summary carries ten; it does not carry all of them.
- **The clone does not reformat `internal/`.** `gofmt -l` is not clean
  for `internal/` and was not run over it; the files this cycle wrote
  are formatted, and the files it edited that were already non-clean on
  `HEAD` were left as they were.

## 9. 存疑 / open questions

- **The PNG→JPEG thresholds.** The note says "large PNGs" and "much
  smaller" without numbers. The clone's 64 KiB and 2× are its own
  choice (【推测】); the reference's own constants are not recoverable
  from the notes.
- **The subagent's summary shape.** The clone writes its own summary
  (the parent's half plus a digest) because PiG's hook cannot amend the
  model's summary. The reference's summary is the model's own, and its
  shape is unknown.
- **Whether the reference's after-compaction figure counts the *prompt
  overhead* as well.** The clone adds the carried block to the same
  "overhead + summary" base the old figure used; the note only says the
  carried content is now counted.
- **Whether the reference's scrollback flush is per-turn or per-frame.**
  The symbol table (`deferScrollbackPush`, `TakeDeferredPush`) says the
  push is *deferred*, but when the deferral is flushed is not in the
  names.
- **The `/context` half of item 3.** The clone's `/context` panel shows
  the live token grid and has no after-compaction row, so only the
  compaction marker changed. Whether the reference's `/context` shows a
  second figure the clone does not have is unknown.

## Limitations

- **The 1.4.10 binary was not driven.** It was read: its string table
  and its recovered symbol table (`out/local-macos/seeds/seeds.txt`,
  regenerated by the RE refresh in this tree). No 1.4.10 session was
  started, so nothing in this report is a first-hand *rendering* of the
  reference at 1.4.10.
- **The upstream notes are the vendor's own words, not the reference's
  behaviour.** Every "the reference does X" in this report is either a
  symbol in the 1.4.10 table or the note itself.
- **The clone's own behaviour was not compared against a live reference**
  for any of the four implemented items: the images, the memory trim, the
  compaction figure and the subagent summary are the clone's rules
  applied to the clone's own surfaces.
- **The scrollback (item 1) is not implemented at all**, and the
  browser snapshot (item 6) is not applicable — see §6 and §7 for why.
- `internal/cliapp` carries **pre-existing failures** (colour assertions
  from another cycle's palette change). The count is the same before and
  after this cycle's change — verified against `HEAD` in a scratch
  worktree — and none of them is in a file this cycle wrote. The
  changelog-panel failures (`TestPanelChangelogLayout`,
  `TestPanelChangelogViewerScrolls`) are **not** this cycle's either: they
  follow the RE refresh's re-capture of the changelog feed, whose newest
  section moved to 1.4.9, and they live in the second agent's paths
  (`internal/cli`, the feed capture).
- The **two `internal/cli` changelog tests** (`TestChangelog_Shape`,
  `TestChangelogReference_NewestBulletIsUpstream`) and the version pin test
  all pass against the 1.4.10 capture (`internal/cli/testdata/changelog_reference.txt`).

---

Reports: the upstream notes are in
[`internal/cli/testdata/changelog_upstream_1.4.10.md`](../../../internal/cli/testdata/changelog_upstream_1.4.10.md);
the 1.4.10 symbol table is in
[`out/local-macos/seeds/seeds.txt`](../../../out/local-macos/seeds/seeds.txt),
and its diff against 1.4.8 in
[`out/local-macos/seeds/diff_1.4.8_1.4.10.txt`](../../../out/local-macos/seeds/diff_1.4.8_1.4.10.txt).
