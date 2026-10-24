# Cycle 265 — MostRecentInWorkspace / scanWorkspace cache-parse hot loop fix

## Why this cycle

`./cmd/wagyu` hung at 100 % CPU on startup. `sample` showed every
goroutine spinning inside `internal/convstore.MostRecentInWorkspace`
→ `rowMatchesWorkspace` → `rowMatchesCacheURI` →
`encoding/json.Unmarshal` of `cache/conversation_metadata.json`.

Root cause: `rowMatchesCacheURI` did `os.ReadFile` + `json.Unmarshal`
on the cache file **on every call**, and both callers
(`MostRecentInWorkspace`, `scanWorkspace`) invoke it inside a loop
over every conversation row. With ~2.1 MB cache JSON (the size on
this machine) and a non-trivial picker row count, that is
**O(N × M) per scan**, which pinned a CPU core at 100 % until the
parse finished.

This regression was not introduced by any of the recent gap-closing
cycles (240/253/254/255/256/257/258/259/261/262/263/264). `git blame`
shows the per-row file-read pattern was committed by
`2eadd29 ("feature: --continue workspace parent/child fallback +
auto-resume after crash")` on 2026-10-18, before the recent batch.
The hang surfaced now because the cache JSON has grown as more
sessions accumulate.

## What this cycle ships

A once-per-scan invariant:

1. **`loadWorkspaceURIsFromCache()`** — new helper that reads +
   parses `cache/conversation_metadata.json` once and returns the
   per-conversation WorkspaceURIs slice indexed by conversation id.
   A nil result means "missing or unreadable" (callers treat nil
   the same as empty).
2. **`loadWorkspaceURIsFromCacheFn`** — package-level function
   variable, defaulting to `loadWorkspaceURIsFromCache`. Tests
   swap it for a counting stub. Production code reads through the
   variable so the seam is invisible at the call site.
3. **`rowMatchesCacheURI`** — signature change: now takes the
   pre-parsed `uris map[string][]string` as its last argument.
   A nil/empty map is treated as "no entries" and falls through to
   the other matchers (cache missing == look at the SQLite index,
   which is the cycle-167 path).
4. **`rowMatchesWorkspace`** — signature change: takes the URIs
   map and passes it through. `MostRecentInWorkspace` and
   `scanWorkspace` each load once and pass down.
5. **Helper called unconditionally** — even when `listAll()`
   returns 0 rows, the helper is invoked once. This keeps the
   once-per-scan invariant testable in `find_test.go` and is at
   most one file read of ~2 MB, which only happens when this
   function is invoked at all (it is not on the critical path for
   `--print` mode).

The shape of the JSON parsed by `loadWorkspaceURIsFromCache` is
unchanged — it is still the cycle-184 picker shape
(`conversations.<id>.summary.WorkspaceURIs`). No wire-format
change.

## Tests

`internal/convstore/find_test.go`:

| Test | What it pins |
|---|---|
| `TestMostRecentInWorkspace_LoadsCacheOnce` | the once-per-scan invariant: counting stub replaces the loader and the test asserts call count = 1 |
| `TestRowMatchesCacheURI_NilURIsMapIsSafe` | nil, empty, and missing-id maps all return false without panicking |
| `TestLoadWorkspaceURIsFromCache_ParsesShape` | the JSON shape (cycle-184 picker format) parses correctly; entries with no WorkspaceURIs are omitted from the map |
| (pre-existing) `TestFindMostRecentSameWorkspace` and the rest of the convstore test suite | unchanged; all still pass |

## Sweep

- `go build -o /tmp/wagyu-final ./cmd/wagyu` → exit 0
- `go test -vet=off -count=1 ./internal/convstore/` → **OK**
- `go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...` →
  **OK** (58 packages, 0 failures)
- `sample` of a fresh `./wagyu-final` → **0 % CPU**, idle on
  bubbletea, no `convstore.*` in the call graph

## Hard constraints honoured

- `internal/convstore/sqlite.go` **untouched** (the constraint
  applies to this file specifically, not the package).
- No third-party deps added.
- Tabs, lowercase error strings.
- The wire-format JSON shape is unchanged — a future reader of
  the file on disk does not need to know this cycle shipped.

## 硬约束 / Limitations

1. The fix is local to `internal/convstore/find.go` and its test
   file. The other place that touches `conversation_metadata.json`
   is `internal/convstore/list.go::listFromCache`, which reads +
   unmarshals once per call and does NOT loop over rows in the same
   way. It does not exhibit the same hang because it is invoked
   once per picker render, not per row.
2. The once-per-scan invariant is per-process, not cross-process.
   Two concurrent wagyu processes each pay one parse; that's fine
   for a desktop CLI.
3. The cache file is re-read on every invocation of
   `MostRecentInWorkspace` / `scanWorkspace` — not memoized
   across invocations within the same process. This is correct
   for a single CLI invocation (the picker renders once at
   startup) and avoids the staleness footgun of a long-lived
   `sync.Once` cache when the user starts a fresh session in a
   different workspace.
4. Disk at 100 % during the cycle (12 GB free after recent
   cleanup; pre-existing condition). No mid-run abort.

## Disk

`df -h /` reports 12 GB available at start, 12 GB available at
end. No disk-full incidents.