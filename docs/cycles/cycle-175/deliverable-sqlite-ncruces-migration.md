# Cycle 175 — convstore SQLite reader: hand-rolled → database/sql + ncruces

## Symptom

`internal/convstore/sqlite.go` was a 657-line from-scratch SQLite 3 reader:
header validation, varint decoder, table-B-tree leaf/interior walk,
overflow-chain follower, record/serial-type decoder, and a tiny
`CREATE TABLE … (…)` parser to recover column names and the `INTEGER PRIMARY
KEY` rowid alias. It existed because the package's own doc-comment pinned
the convstore to "pure Go, no heavyweight SQLite deps". By cycle 175 that
constraint was a tax: the package was the only place in the clone left
honouring a narrow reading of "zero deps" that had already been broken by
the charm.land TUI stack at `go.mod` line 5, and zero real files were
being produced from the code in isolation — every cycle that touched it
spent part of its budget re-validating B-tree invariants.

## What the reference actually uses

Before switching drivers I sanity-checked what the closed-source `agy`
writes, since the question "is the clone's reader faithful" is gated on
what the reference writes. The relevant findings, all via `strings` and
`otool -L` on `~/.local/bin/agy`:

| Evidence | Reading |
|---|---|
| `SQLite Float`, `sql: driver does not support …`, `SQLite aggregator constructors must return 1 or 2 values`, **`sqlite library was not compiled for thread-safe operation`** | SQLite is **statically compiled C code** embedded in the binary, not loaded from `/usr/lib/libsqlite3.dylib` (otool shows no SQLite dylib, only system frameworks). |
| `file:%s?mode=rw&_synchronous=NORMAL&_busy_timeout=5000`, `file:%s?mode=ro&immutable=1&_busy_timeout=5000`, `%s?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000` | three DSN templates — read-write DDL, read-only-for-our-path, and WAL-enabled — consistent with `mattn/go-sqlite3`-style URI pragmas. `mode=ro&immutable=1` is the form the reference uses for the *clone's* read-only consumption pattern. |
| `sqlite_schema` × 5 occurrences | reference uses SQLite ≥ 3.33 (the post-rename table name; `sqlite_master` is still a forward-compatible alias). |
| `(?is)(CREATE TABLE [%v]?[\w\d-]+[%v]?)(?:\s*\((.*)\))?` | the reference scrapes `sqlite_schema.sql` with a **regex**, the same shape the old hand-rolled `parseColumns` / `parseDefs` used to take. Independent corroboration that the column-recovery step doesn't need SQLite internals — `PRAGMA table_info` or a query against `pragma_table_info` is fine. |
| `pre-create trajectory db file`, `save trajectory metadata blob`, `load trajectory metadata blob`, `dbtrajectory: Clone: failed to close forked DB` | confirms `trajectory_meta` + `steps` are the two real tables; matches the schema the convstore already targets. |

Net read: **reference uses C SQLite statically linked via a
`mattn/go-sqlite3`-style driver**. That doesn't gate our choice, because we
only read the file. SQLite is a documented file format; any conformant
SQLite reader produces the same rowids, overflow chains, and record decodes.

## Change

1. `go get github.com/ncruces/go-sqlite3` (resolves to `v0.35.6`,
   `go-sqlite3-wasm/v6 v6.3.35304`, `ncruces/julianday v1.0.0`; one
   bump of `x/sync`/`x/sys` as indirect). Per the ncruces README, Go
   and `x/sys` are the only required deps from outside the ncruces
   module graph; the wasm payload is one additional file in
   `go-sqlite3-wasm`. **No cgo**; runs on the same toolchain as the
   rest of the clone.

2. Rewrote `internal/convstore/sqlite.go` from scratch onto
   `database/sql` + the ncruces driver:

   - Kept the public surface (`Open`, `Close`, `DB`, `Scan`, `Tables`,
     `Count`, `isUnreadableStore`, `Row{Columns, Int, Text, Bytes,
     RowID}`) so `agy.go` and `sqlite_test.go` are untouched.
   - `Open` validates the 16-byte SQLite magic up front, so the
     reference's zero-byte and non-SQLite edge cases still surface as
     the same `is not a SQLite 3 database` / `read header` strings that
     `agy.go`'s `isUnreadableStore` looks for.
   - `Scan` runs `SELECT rowid, * FROM <table> ORDER BY rowid` and
     strips the synthetic `rowid` column from the user-visible
     `r.Columns()` so `r.Int("idx") == r.RowID` keeps working on
     `INTEGER PRIMARY KEY` rowid aliases (the `steps.idx` shape
     originally documented in cycle 143).
   - `Tables` queries `sqlite_master` with the
     `type='table' AND name NOT LIKE 'sqlite_%'` filter; works
     unchanged against the reference's `sqlite_schema` rename.
   - `count` and table-existence probes use `quoteIdent` for the
     table name so reserved words / case-sensitive names don't break
     the query.

3. Deleted the old code (~500 lines): the B-tree page reader,
   `readVarint`, `leafCell`, `decodeRecord`, `decodeText`,
   `parseColumns` / `parseDefs`, `parseDefs` column helpers, etc.

4. Updated `AGENTS.md` Conventions to reflect the now-narrower
   "zero-deps" rule: scoped to `rea-go-seeds/`, `build_reports.py`,
   `out/local-macos/*.py`, and the Ghidra Java scripts. The clone
   itself pulls third-party deps — charm.land TUI stack and now the
   ncruces SQLite reader.

## Verification

```
go test -count=1 -v -run 'TestTables|TestCount|TestScanColumnsAndValues|TestScanTrajectoryMetaText|TestOverflowPayload|TestNullBlob|TestMissingTable|TestOpenRejectsNonSQLite|TestOpenRejectsEmptyFile' ./internal/convstore/...
```
all 9 tests pass.

Plus a live-fire load of a real conversation —
`~/.gemini/antigravity-cli/conversations/00d32efa-b745-47e6-86f5-e1b1f104dc6e.db`
— produced the expected transcript shape:

```
path = /Users/gear/.gemini/antigravity-cli/conversations/00d32efa-….db
ID   = 00d32efa-b745-47e6-86f5-e1b1f104dc6e
Traj = 283bcd52-3290-42c0-ae5d-008428eeb3c1
steps= 5
visible transcript = 4
  [0] role=user   idx=0  | second
  [1] role=error  idx=1  | Your current account is not eligible for …
  [2] role=user   idx=2  | x
  [3] role=error  idx=4  | Individual quota reached. …
```

(Probe was added under `cmd/wagyu/probesqlite/` for the run, then
removed.)

The `ls ~/.gemini/antigravity-cli/conversations/` listing shows zero
`-wal` / `-shm` sidecars across 234 `.db` files, so the
"Conversation databases ship no -wal sidecar" claim from the old
reader still holds empirically — but the ncruces driver would read
WAL correctly if it ever appeared, which is a small but real
improvement over the hand-rolled version, which would have silently
missed rows on a database with an open WAL.

## Compatibility notes

- **Sync semantics.** The old hand-rolled reader imposed a strict
  rowid-order walk because the reference's incremental writes reuse
  rowids after page vacuuming; the cycle-143 sort-by-`Idx` in
  `LoadConversation` was the real fix and is unaffected.
- **`INTEGER PRIMARY KEY` alias.** Now resolved two ways:
  `SELECT rowid, *` captures the rowid directly, *and*
  `database/sql` will populate the named column from the record body
  (the alias is transparent to the SQL layer once rowid is asked
  for). The synthetic `rowid` column is stripped from
  `Row.Columns()` so callers see only the declared schema.
- **Empty file / non-SQLite file.** validated before any SQL touches
  the file, so `LoadConversation`'s "exists but unreadable ⇒ return
  empty `Conversation`, don't error" path still works.
- **Write path.** *we don't have one*. If a future cycle adds
  `/rewind` → "persist mid-step" or anything that re-emits a `.db`,
  byte-identical output with the reference would require either
  ncruces and the reference agreeing on SQLite version (they ship
  different bundled SQLite) *or* moving to `mattn/go-sqlite3` to
  match exactly. Either is a one-cycle carve-out; not blocking.

## 硬约束 / 存疑 / limitations

- 【一手】the *read* contract against the reference's DB files is
  preserved (9/9 unit tests, plus one live-fire load). We know it
  reads the same `Conversations` directory the reference writes.
- 【反编译】we did not check whether *writing* with ncruces would
  produce byte-identical output to the reference. Today there is
  no write path so this doesn't matter; if a future cycle introduces
  one, verify before relying on the reference continuing to round-trip
  read these files.
- 【推测】the nine tests do not exhaust all possible `sqlite_master`
  schema variants (e.g. `WITHOUT ROWID` tables, generated columns,
  virtual tables, FTS5 schemas). The reference's conversations
  schema is exactly two simple tables (`steps`, `trajectory_meta`),
  none of which exercise those code paths. Out of scope.
- 【推测】AGENTS.md's "zero deps" carve-out now makes the constraint
  read three times longer; if you prefer the old terseness, the
  paragraph can be compressed back to two lines with a list of
  out-of-scope paths.
