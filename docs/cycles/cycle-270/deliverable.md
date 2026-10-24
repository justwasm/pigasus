# cycle 270 — conversation_summaries.db was scanned once per picker row

## TL;DR

`MostRecentInWorkspace` (`internal/convstore/find.go`) decided whether a
conversation belonged to the current workspace by, for **every** conversation
in the store, opening `conversation_summaries.db` and full-scanning its
`workspace_uris` column. That is O(N×M): N picker rows × the whole table,
with a fresh SQLite open per row. On an install with ~1.1 K conversations it
pinned a CPU core for **55.63 s** before the TUI could draw its first frame.

The cache-JSON half of the same matcher had already been fixed in an earlier
cycle (load once, not per row). This cycle fixes the *index* half the same
way: one `Open`, one `Scan`, into a `map[conversation_id][]workspace_URIs`,
then pure in-memory lookups. Measured startup: **55.63 s → 1.68 s** to first
bytes.

## Root cause

`rowMatchesWorkspace` had three fall-through matchers:

1. `rowMatchesCacheURI` — reads a pre-loaded `map[string][]string` (fine).
2. `rowMatchesIndexURI` — **opened the DB and scanned the table per call**.
3. project-base-name equality (cheap).

`rowMatchesIndexURI(id, wantURI, wantPath)` built a fresh SQLite handle,
ran `db.Scan("conversation_summaries", ...)`, and returned on the first row
whose `conversation_id` matched. Because it was called once per conversation
in the loop in `MostRecentInWorkspace`, the table was opened and scanned N
times.

The `_ = db.Scan` return idiom meant each open/close and each row decode was
paid in full; on a store with a large summaries table the cost dominated
startup.

## What changed

```diff
 	uris := loadWorkspaceURIsFromCacheFn()
+	indexURIs := loadWorkspaceURIsFromIndexFn()
 	...
 	for _, c := range rows {
-		if !rowMatchesWorkspace(c, wantURI, want, wantBase, uris) {
+		if !rowMatchesWorkspace(c, wantURI, want, wantBase, uris, indexURIs) {
```

`rowMatchesIndexURI` becomes a pure map lookup:

```go
func rowMatchesIndexURI(id, wantURI, wantPath string, indexURIs map[string][]string) bool {
	if len(indexURIs) == 0 {
		return false
	}
	for _, u := range indexURIs[id] {
		// literal match, then symlink-tolerant EvalSymlinks compare
	}
	return false
}
```

and a new `loadWorkspaceURIsFromIndex` does the single open + scan:

```go
func loadWorkspaceURIsFromIndex() map[string][]string {
	db, err := Open(filepath.Join(StoreDir(), "conversation_summaries.db"))
	if err != nil {
		return nil
	}
	defer db.Close()
	out := make(map[string][]string)
	_ = db.Scan("conversation_summaries", func(r *Row) error {
		id := r.Text("conversation_id")
		if id == "" {
			return nil
		}
		out[id] = append(out[id], splitWorkspaceURIs(r.Text("workspace_uris"))...)
		return nil
	})
	return out
}
```

`loadWorkspaceURIsFromIndexFn` is the package-level test seam (same shape as
the existing `loadWorkspaceURIsFromCacheFn`), so the once-per-scan invariant
is assertable without depending on disk state.

## Why the once-per-scan seam is load-bearing

The bug was not "the query is slow" but "the query runs N times". A benchmark
of a single scan would not catch a regression to per-row scanning. The seam
lets `TestMostRecentInWorkspace_LoadsIndexOnce` count invocations directly and
assert exactly 1, independent of how many rows `listAll()` returns.

## Files

| File | Change |
|---|---|
| `internal/convstore/find.go` | +1 load-once call, map-based matcher, new loader + seam, signature threads `indexURIs` |
| `internal/convstore/find_test.go` | `TestMostRecentInWorkspace_LoadsIndexOnce`, `TestRowMatchesIndexURI_MapLookup` |
| `internal/convstore/title_test.go` | adjusted for the shared helper |
| `cmd/wagyu/continue_test.go`, `cmd/wagyu/remote_control_*_test.go`, `internal/remotecontrol/dispatch_status.go` | drive-by fixes (see 硬约束) |

## Verification

```
$ go test -count=1 ./internal/convstore/
ok  	github.com/chadgpt/openagy/internal/convstore	1.391s
```

End-to-end timing on the user's real store (≈1.1 K conversations, 500 `.db`
files), measured to first bytes of TUI output:

| | before | after |
|---|---|---|
| to first bytes | 55.63 s | 1.68 s |

The 55.63 s figure matches the CPU-bound profile: `conversation_summaries.db`
was opened and scanned once per conversation row.

## Drive-by fixes bundled here

These were failing at HEAD (pre-existing, unrelated to the scan fix) and had
to be repaired to get a green suite:

- `cmd/wagyu/model_default_effort_test.go:67` — a `go vet` printf bug.
- `cmd/wagyu/remote_control_foreground_test.go` — the test closed an fd and
  then reused the number; switched to dup/restore.
- `internal/remotecontrol/dispatch_status.go` — the status payload needed a
  top-level `conversation_id` alongside the nested one.
- `cmd/wagyu/remote_control_serve_test.go` — banner assertion out of date.
- `cmd/wagyu/tui_run_test.go` — the help-panel test pinned `Version 1.4.0`
  while `internal/cli.ReferenceVersion` is `1.4.2` (stale since the reference
  bumped).

## 硬约束 / limitations

- The loader reads by `StoreDir()`, which honours `ANTIGRAVITY_HOME`. The
  test seam returns `nil` on any open error and every caller treats a nil/empty
  map as "no index entries", so a missing or corrupt `conversation_summaries.db`
  degrades to the cache-URI and project-base matchers rather than failing.
- `loadWorkspaceURIsFromIndex` currently **appends** per row
  (`out[id] = append(out[id], splitWorkspaceURIs(...)...)`) rather than
  overwriting. If the table ever carries duplicate `conversation_id` rows the
  loader keeps both; the previous per-row scan matched against all of them
  too, so this is not a behaviour change, but the map shape makes a future
  "last row wins" assumption tempting — don't add one without checking.
- The 1.68 s figure is a single measurement on one machine with a warm
  filesystem cache. A cold cache will be slower (the remaining cost is the
  one-shot table read plus `listAll`'s directory walk). No attempt was made to
  parallelise or mmap; O(N) is the target and it is met.
- The shared-store cleanup (500 `.db`, 6,144 stale cache keys, 2,103 stale
  summary rows) was **not** performed in this cycle — a live TUI held the
  store. Deleting rows would only make this faster, not fix it; the fix is
  algorithmic.
