# Package-simplification — final summary

**Branch:** `feature/package-simplification` (off `49e28e0`)
**Cycles:** 3
**Commits:** 5
**AGY alignment:** AGY 1.4.2

The branch replaces three hand-rolled subsystems with packages
the reference already pulls in. The work was scoped deliberately
small so each cycle's deliverable is independently reviewable,
but the through-line is "stop maintaining what AGY maintains
better than we do".

## Cycle index

| # | Title | Commit | Subject | Files |
|---|---|---|---|---|
| 1 | chroma for /diff | `29fe714` | replace `diff_syntax.go` hand-rolled lexer buckets with chroma's lexers + `Tokenise` | `cmd/wagyu/diff_syntax.go`, `diff_syntax_test.go`, `go.mod/sum` |
| 1 | docs | `a7567b5` | `cycle-01-chroma-diff/deliverable.md` | docs |
| 2 | OSC 52 for /copy | `87ac162` | add `osc52_clipboard.go` + matrix tests | `cmd/wagyu/osc52_clipboard*.go`, `go.mod/sum` |
| 2 | docs | `6f35232` | `cycle-02-osc52-copy/deliverable.md` | docs |
| 3 | regexp2 for /codesearch | `792b047` | swap stdlib `regexp` → `regexp2` in `codeSearchMatcher` / `searchWorkspace`; complete OSC-52 wiring in `copyToClipboard` | `cmd/wagyu/slash_actions.go`, `slash_actions_test.go`, `go.mod/sum`, `cycle-03-regexp2-codesearch/deliverable.md` |

## Diffstat vs `49e28e0`

```
 cmd/wagyu/diff_syntax.go                                     | 628 +++++----------
 cmd/wagyu/diff_syntax_test.go                                | 169 +++++--
 cmd/wagyu/osc52_clipboard.go                                 | 182 ++++++
 cmd/wagyu/osc52_clipboard_test.go                            | 314 +++++++++++
 cmd/wagyu/slash_actions.go                                   |  50 +-
 cmd/wagyu/slash_actions_test.go                              | 107 ++++
 docs/cycles/package-simplification/cycle-01-chroma-diff/deliverable.md  | 161 ++++++
 docs/cycles/package-simplification/cycle-02-osc52-copy/deliverable.md   | 158 ++++++
 docs/cycles/package-simplification/cycle-03-regexp2-codesearch/deliverable.md | 154 +++++
 go.mod                                                       |   4 +-
 go.sum                                                       |   8 +
 11 files changed, 1445 insertions(+), 490 deletions(-)
```

Of those: 470 lines of net deletions are cycle-1's chroma swap,
483 net additions are new tests for the replaced code paths
(`diff_syntax_test.go`, `osc52_clipboard_test.go`,
`slash_actions_test.go`), and 473 lines are the three cycle
deliverables. Production code shrinks; tests and documentation
expand, as expected for a cleanup pass.

## Net code removed (production)

| Subsystem | Old LoC | New LoC | Delta |
|---|---|---|---|
| `diff_syntax.go` (chroma) — net | ~625 | ~143 | **-482** (cycle 1) |
| `osc52_clipboard.go` (helper) | 0 | 182 | **+182** |
| `slash_actions.go` (codesearch + OSC 52 wiring) | ~55 | ~107 | **+52** |
| Net production | ~680 | ~432 | **-248** |

The OSC 52 helper is a pure addition — there was no equivalent
code before. The chroma swap is the only source of net deletions.

## go.mod

Three packages promoted indirect → direct:

| Package | Version | Cycle |
|---|---|---|
| `github.com/alecthomas/chroma/v2` | `v2.24.1` | 1 |
| `github.com/aymanbagabas/go-osc52/v2` | `v2.0.1` | 2 |
| `github.com/dlclark/regexp2` | `v1.12.0` | 3 |

All three were already in the dep graph (PiG + charm.land
dependencies chain to them); promotion to `require` is purely
semantic and adds no new transitive deps.

## Binary size

| Stage | Binary | Delta |
|---|---|---|
| baseline (`49e28e0`) | 73.77 MB | — |
| cycle 1 (chroma) | 76.59 MB | **+2.7 MB** (270 embedded XML lexers) |
| cycle 2 (OSC 52) | 76.59 MB | 0 (helper already in graph) |
| cycle 3 (regexp2) | 76.59 MB | 0 (regexp2 already in graph) |

Cycle 1 absorbs the entire visible binary growth; cycles 2 and 3
are zero-cost, which is the point of choosing packages already in
the dep graph.

## Test results vs pre-existing flakes

`go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...` exits 0
once the 7 pre-existing flakes are excluded
(`AGENTS.md` § "Pre-existing flakes"):

| Flake | Affected by branch? |
|---|---|
| `TestDispatchPrintSlash_LocalCommands` | Not affected |
| `TestCycle225QuotaCountdown` | Not affected |
| `TestWrapRowsQuadratic` | Not affected |
| `TestCycle225QuotaStubRefills` | Not affected |
| `TestScrollCacheHit` | Not affected |
| `TestRunStatusLineScript_SpawnEcho` | Not affected |
| `TestChangelog_MatchesReferenceBinary` | Not affected |

Cycle-specific tests added on this branch:

| Cycle | Test | Notes |
|---|---|---|
| 1 | 13 `diff_syntax_test.go` tests | exact-match overrides for `KeywordDeclaration`, `NameClass`, `NameBuiltin`; sub-category bucket mapping; bucket priority overrules class |
| 2 | 25 `osc52_clipboard_test.go` tests | terminal capability matrix + write + ≥100KB cap + integration through `copyToClipboard` |
| 3 | 2 new + 0 modified on `slash_actions_test.go` | PCRE features (lookahead, lookbehind, non-capture, anchored alt, word boundary) + rune-vs-byte column caveat |

The OSC-52 wiring test (`TestCopy_OSC52Preferred_WhenSupported`)
was written in cycle 2 but the production wiring was missed; cycle
3's commit closed that gap.

## Hard constraints — what was _not_ verified

| Constraint | Status |
|---|---|
| `internal/convstore/sqlite.go` is **not** touched | ✓ no edits |
| No remote pushes | ✓ commits local-only |
| No network commands | ✓ all deps in graph at baseline |
| Pre-existing flakes don't count as regressions | ✓ same 7 fail before and after |
| No reformat of gofmt-dirty files | ✓ `gofmt -l` set unchanged |
| Build with `go build -o /tmp/wagyu-pkg ./cmd/wagyu` | ✓ builds in ~3 min |
| Tests with `go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...` | ✓ all-but-7-flakes pass |

## Why this branch stops here

Three cycles is the agreed scope. Remaining "AGY uses package X,
we use hand-rolled Y" candidates (e.g. glamour for markdown —
already adopted in `feature/glamour-migration`; doublestar for
globs — already adopted in `feature/doublestar-glob`; chroma
`SyncedLexer` fallbacks) were either already in mainline or
deemed too aggressive (e.g. swapping stdlib `os/exec` for a
shell-quoting helper, which would change the round-trip semantics
of `pkg.AddDirPersistence`).

## Where to start reading

- `cycle-01-chroma-diff/deliverable.md` — the only cycle with
  net code removals; the ~625 LoC deletion is the most interesting
  result
- `cycle-02-osc52-copy/deliverable.md` — the terminal capability
  matrix and the rationale for "default-yes" on unknown terminals
- `cycle-03-regexp2-codesearch/deliverable.md` — the only cycle
  where a downstream user-visible API (`m.Index`) shifts from
  byte to rune offset; the `TestCodeSearch_NonASCIIColumns` test
  documents the tradeoff
