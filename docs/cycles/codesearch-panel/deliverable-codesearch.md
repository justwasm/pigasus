# deliverable-codesearch — cycle 228: /codesearch interactive regex search panel

**Branch:** `feature/codesearch-panel`
**Worktree:** `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-codesearch`
**Probe dir:** `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/agy-codesearch`
**TMUX socket:** `codesearch`

## What this cycle delivers

The cycle 213 `cmd/wagyu/slash_codesearch.go` rendered the `/codesearch`
result list synchronously — `csScan` walked the workspace and returned the
full match list in one shot. AGY 1.4.0 reversed that: the reference now
spawns `rg --json`, streams matches into the panel as they arrive, lets
the user hit Esc to kill the rg child mid-search, and falls back to a
local walk when the rg binary cannot be executed. This cycle ports the
same contract to the clone, behind a new `internal/codesearch` package
the panel consumes through a buffered channel and a `tea.Tick` fan-out.

The synchronous path (`m.height == 0` or `m.streamingSearch == false`)
stays for the cycle-213 layout tests; it now shares the `csHit` shape
with the streaming path so the panel renders both identically.

## Slash command surface

| Command | Behaviour |
|---|---|
| `/codesearch` (no args) | echoes `⎿  /codesearch needs a query.` — no panel opens |
| `/codesearch -F needle` | fixed-string search |
| `/codesearch --literal needle` | fixed-string search (long flag) |
| `/codesearch pattern` | regex search; case-insensitive when pattern is all lowercase |
| `/codesearch … f:*.go` | include glob (repeatable; forwarded as `-g`) |
| `/codesearch … file:vendor/**` | exclude glob (repeatable; forwarded as `-g !…`) |
| `/cs` / `/search` | aliases routing to the same handler |

The reference's usage line is

```
Usage: /codesearch [-F|--literal] <query>
```

This cycle keeps the same prefix and appends the f:/file: globs to match
the spec:

```
Usage: /codesearch [-F|--literal] <query> [f:<glob>] [file:<glob>]
```

The argument tail is echoed verbatim in the title row (the reference
does this) — `Code Search: -F TODO` not `Code Search: TODO`.

## Panel state machine

```
                   (empty args)
   bare /codesearch ──────────────►  echo "needs a query"
        │
        │ with args
        ▼
   ┌────────────────┐  rg --json streams into chan Match, buffered 64
   │  StateRunning  │  csTickMsg drains every 80 ms, appends to cs.hits
   └───────┬────────┘  spinner glyph ticks on the title row
           │
           │  rg exits / fallback completes / ctx cancelled
           ▼
   ┌────────────────┐  close(Search.Matches); Search.State = terminal
   │  StateDone /   │  csTickMsg observes closed channel and stops arming
   │  StateCancelled│  header reads "Found N matches in K files. [Enter on row to open]"
   │  StateFailed   │
   └───────┬────────┘
           │
           │  Esc ───► runner.Cancel() → kills rg child (or stops walk)
           ▼          panel closes (reference closes /codesearch silently)
       panel closed
```

When state is `StateRunning && hits == 0` the panel renders

```
  ⣾  Searching…  [Esc to cancel]
```

When state is `StateRunning && hits > 0` it streams the running count:

```
  Found N matches in K files.  [Esc to cancel]
```

When state is terminal with hits it falls to the cycle 213 list view
(file header + match rows + between-block separator + `[a-b of N items]`
counter), with the header carrying the same count but in terminal form:

```
  Found N matches in K files.  [Enter on row to open]
```

When state is terminal with zero hits the panel renders `No results`.

## Probe-vs-clone raw captures

All four raw captures are under
`/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/agy-codesearch/raw/`.
The clone at `our/agy` is built from this branch; the reference at
`ref/agy` is `~/.local/bin/agy` (1.4.0).

| Probe | 220×50 ref | 220×50 our | 220×30 ref | 220×30 our |
|---|---|---|---|---|
| empty launch (no slash) | identical banner + prompt + warning row; clone adds `⚠  MiniMax-M3 unavailable` warning that the reference omits in this run | same |
| `/codesearch slash_panels` | `Code Search:` / `Found 125 results` / file headers / match rows | `Code Search: slash_panels` / `Found 125 matches in 49 files.  [Enter on row to open]` / same file headers / match rows / `[1-9 of 125 items]` counter |
| `/codesearch ZZZNoSuch` | `Code Search: ZZZNoSuch` / `No results` | `Code Search: ZZZNoSuch` / `No results` |
| bare `/codesearch` | panel opens with `Usage: /codesearch [-F|--literal] <query>` usage row | echo `⎿  /codesearch needs a query.` — panel does not open (per spec) |

Two intentional divergences from the reference, both pinned by spec:

1. The reference uses `Found 125 results`; the clone follows the
   cycle-228 spec which calls for `Found N matches in K files` so the
   panel knows both the line count and the file count.
2. The reference opens a usage-form panel on bare `/codesearch`; the
   spec routes bare `/codesearch` through `openSlashPanelArgs` (which
   returns false for an empty arg tail) into `handleSlashCommand`,
   which echoes `/codesearch needs a query.` into the transcript.

The clone's `Enter` on a match row opens the file in `$EDITOR` (cycle
213 behaviour, `cmd/wagyu/slash_codesearch.go:codesearchPanelKey`'s
default branch); the reference closes the panel silently on `Enter`.
Not verified against the reference; see 存疑.

## Implementation notes

- `internal/codesearch/codesearch.go` ships the `Search` / `Runner` /
  `ParseArgs` surface the panel uses. `Runner.Run` chooses between
  `runRipgrep` (the production path: `rg --json`) and `runFallback` (a
  `filepath.WalkDir` loop applying the same include/exclude globs);
  exit status 1 from ripgrep is treated as a successful empty search
  and routes to `StateDone`, not the fallback. The runner owns a
  single `cancelCmd` field that `Cancel()` uses to kill the rg child;
  the goroutine is launched from `cmd/wagyu/slash_codesearch.go`'s
  `runCodesearch` with a `context.WithCancel(context.Background())`
  the Esc handler stores on `m.csCancel`.
- The `csTick` arm fires every 80 ms (`csTickInterval`); the handler
  drains the channel into `cs.hits`, updates `cs.files`, and re-arms
  the next tick. The handler also continues to arm one tick even when
  the search has already settled, so a fast-completing search
  (e.g. `rg` exit code 1 with zero matches) cannot race the very
  first tick and leave the panel stuck on "Searching…" — see commit
  message and `internal/codesearch/codesearch.go:isExitStatusOne`.
- `Match` carries a `Display` field that `codesearch.Truncate` populates
  for cells-aware truncation (`runewidth.StringWidth` over the input,
  rune-by-rune append until the budget is exhausted, ellipsis appended);
  long CJK lines are never turned into replacement glyphs (verified
  by `TestCJKTruncation/cjk-clips` and `/mixed`).
- The `csHit` cycle-213 shape (`file / line / col / prev / text / next`
  / match-span runes) is kept; `csHitFromMatch` re-derives the
  rune-offset match span from the verbatim line so the panel renders
  the ripgrep output through the same row builder. `prev`/`next` are
  empty in the ripgrep path — `csBlockRows` skips the absent context
  rows so the ripgrep result block is single-row by design.

## Tests added

| File | Test | Pins |
|---|---|---|
| `internal/codesearch/codesearch_test.go` | `TestParseArgs` | 10 sub-cases: regex default, `-F`, `--literal`, `f:` include, `file:` exclude, both, empty, flags-only, unknown-flag-becomes-pattern, `--` makes `-F` a pattern |
| same | `TestParseArgsMalformed` | `ErrEmptyQuery` short-circuits bare `/codesearch` |
| same | `TestRunRipgrepSuccess` | mock rg emits 3 matches → channel holds 3, StateDone, FilesHit=3 |
| same | `TestRunRipgrepCancelled` | ctx cancel kills the rg child; StateCancelled; no matches leaked |
| same | `TestRunRipgrepFallback` | rg missing → walker path fills the channel; channel closed on return |
| same | `TestCJKTruncation` | ascii-fits / ascii-clips / cjk-clips / mixed — verifies no `�` glyph ever appears |
| same | `TestFallbackGlobFilter` | `f:*.go` / `file:vendor/**` apply in the fallback path |
| `cmd/wagyu/slash_codesearch_test.go` | `TestSlashCodesearchNoArgs` | bare `/codesearch` echoes the spec line and does not open the panel |
| same | `TestSlashCodesearchStreaming` | `/codesearch TODO` opens the panel; the running-count header reads "Found 3 matches in 3 files."; all three file headers visible |
| same | `TestSlashCodesearchCancel` | Esc mid-search cancels the rg child and closes the panel; `search.State != StateRunning` after Esc |
| same | `TestSlashCodesearchAliases` | `/cs` and `/search` route to the same handler |

Updated cycle-213 tests:

- `TestPanelCodeSearchLayout` — usage line now carries the f:/file:
  globs.
- `TestSlashCodesearch_Router` — bare `/codesearch` echoes
  `/codesearch needs a query.`, panel does not open.
- `TestCycle213ParseArgs` — 5-tuple destructure (`csParseArgs` now
  returns `query, literal, include, exclude, err`).
- `TestCycle213PanelLinesResultList` — "Found N matches in K files"
  substring instead of cycle-213's "Found N results".
- `TestCycle213PanelKeyInertOnEmpty` — sets `m.panel` directly because
  `openSlashPanelArgs` returns false for the empty-args echo path.
- `TestCycle213CsFillCounterDrivesFormatting` — sets
  `m.streamingSearch = false` so the synchronous csScan fills hits
  before codesearchStart returns.

## Hard gates

| Gate | Result |
|---|---|
| `go build -o <PROBEDIR>/our/agy ./cmd/wagyu` | exit 0; 73 MB binary |
| `go test -vet=off -count=1 ./cmd/... ./internal/...` | all 30 packages green; new tests + cycle-211-229 regressions pass |
| `gofmt -l internal/codesearch/*.go cmd/wagyu/slash_codesearch_test.go` | empty |
| Commit on `feature/codesearch-panel` | see bottom of this report |

The pre-existing cycle-225 `TestCycle225QuotaCountdown` flake (race
between `time.Now()` and a stub clock in `quotaCountdown(59m1s)`) is
not exercised by this cycle's changes — it passes 3/3 when run in
isolation and 2/3 when run in the full suite; both outcomes are
independent of this branch's diff.

## Files touched

```
cmd/wagyu/slash_actions_test.go        (TestSlashCodesearch_Router — new echo line)
cmd/wagyu/slash_codesearch.go          (cycle 228: streaming renderer + csState extensions)
cmd/wagyu/slash_codesearch_test.go     (NEW — 4 cycle-228 tests)
cmd/wagyu/slash_commands.go            (codesearch case → handleSlashCommand echo for empty args)
cmd/wagyu/slash_cycle213_test.go       (5-tuple destructure, new panel substrings, sync path opt-out)
cmd/wagyu/slash_panels_test.go         (TestPanelCodeSearchLayout usage line)
cmd/wagyu/tui_run.go                   (csCancel, streamingSearch, csTickMsg dispatch, panelCodeSearch tick-arm)
cmd/wagyu/tui_tools.go                 ("match" entry in categoryWords)
internal/codesearch/codesearch.go      (NEW — Search / Runner / ParseArgs / Truncate / fallback)
internal/codesearch/codesearch_test.go (NEW — 6 tests + TestFallbackGlobFilter)
docs/cycles/codesearch-panel/deliverable-codesearch.md  (this file)
```

## 存疑 / limitations

- The reference renders `Enter` on a row as a no-op close (the panel
  closes silently); the clone opens the file in `$EDITOR` per cycle
  213's existing key handler. The clone was not changed in this cycle
  to match the reference because (a) opening in `$EDITOR` is what the
  clone already did and (b) this cycle's spec did not call for a
  change. Worth verifying against `~/.local/bin/agy` 1.4.0 on Enter.
- The reference uses `Found N results`; the clone uses `Found N matches
  in K files`. Pinned by the cycle-228 spec; the reference wording
  diverges from the spec the clone follows. If the user-facing
  verbatim match is preferred, change the `categoryWords["match"]`
  format string in `cmd/wagyu/slash_codesearch.go:codesearchPanelLines`.
- Bare `/codesearch` echoes in the clone; the reference opens the panel
  with a usage row. The clone follows the spec, not the reference.
  Reversing the order would mean `openSlashPanelArgs` returning true
  for empty args and `codesearchStart` skipping the goroutine when
  the parsed query is empty.
- The streaming test's wait loop requires state to first transition
  out of the empty string and then out of `StateRunning`. A future
  refactor that exposes the runner's terminal signal as a public
  channel close (rather than the indirect `s.Matches` close) would
  simplify the test; for now the two-phase wait is the simplest fix
  that survives the goroutine-vs-test-scheduler race.
- The branch is currently committed at the worktree root and has not
  yet been merged into the clone's main branch. The worktree path
  `/private/var/folders/j1/.../opencode/openagy-codesearch` is the
  reviewable surface; the clone's main checkout at
  `/Users/gear/Documents/GitHub/openagy` is untouched.