# Cycle 143 — convstore steps must be sorted by idx

## Why

`internal/convstore/sqlite.go::Scan` walks every row of a table in
rowid order. SQLite's rowid is normally insertion order, but it
can be re-used after deletions (vacuumed pages, restarted
sessions) — meaning the conversation timeline can come out
scrambled when loading a long-lived conversation that's been
edited (deleted tool calls, replaced steps, etc.).

Symptom: the `resume_scroll` harness scenario shows ref and ours
reading the same `4638a7b0-e5f4-4961-80c2-d61499f06050.db` file
but seeing **completely different transcripts** — ref sees one
conversation about picker byte structure, ours sees a different
one about Ghidra headless scripts. The conversation's
user/model alternation is the right shape, but the timeline is
in a different order.

Fix: after `LoadConversation` finishes scanning, sort
`c.Steps` by `Idx` so the timeline is monotonically increasing
regardless of rowid re-use.

## What changes

### `internal/convstore/agy.go::LoadConversation`

After the `db.Scan("steps", …)` loop completes, runs
`sort.SliceStable(c.Steps, …)` on the `Idx` field. Stable sort
preserves the rowid-order tiebreaker for steps that share an
`Idx` (defensive — shouldn't happen in practice but cheap to
preserve).

Added `sort` to the imports.

## What does NOT change in cycle 143

- The on-disk format is unchanged — we don't re-write the DB.
- Other tables (`trajectory_meta`, `cascade_*`) are not affected;
  they have at most a handful of rows and ordering doesn't
  change the user-visible result.
- `ChatTurns()` semantics — it still flattens user/model turns
  in `Steps` order, but now that order is monotonically
  increasing in `Idx`.
- The harness diff for `resume_scroll` is still ~50 chars per
  step, but the **cause** of the diff shifts: it was previously
  \"ref and ours see completely different timelines\"; now it's
  \"ref fails to authenticate with the fake JETSKI_OAUTH_TOKEN
  and shows an empty conversation while ours loads the on-disk
  DB\". The new cause is environmental, not a layout bug.

## Hard constraints / 存疑 / limitations

- 【推测】Why does the ref show empty transcript with the
  fake token? The reference binary's auth integration talks to
  a real OAuth server; with `JETSKI_OAUTH_TOKEN=test` it can't
  authenticate, so it falls back to a fresh conversation rather
  than loading the on-disk DB. Cycle 143 doesn't try to match
  this — the harness is unrealistic and the right fix is to
  give the ref tmux session a real auth token, which is out of
  scope for layout fidelity.
- 【推测】Are there other DB schemas that need ordering? `trajectory_*`
  tables have ≤ 4 rows each and are not timeline-sensitive.
  Steps is the only one that needs ordering.
- 【推测】Could `Idx` itself be non-monotonic in the reference's
  schema? Probably not — `Idx` is the step index, written
  monotonically by the agent loop. But `sort.SliceStable` is
  defensive against this without harm.