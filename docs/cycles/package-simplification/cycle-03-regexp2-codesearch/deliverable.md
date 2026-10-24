# Cycle 3 — regexp2 for /codesearch regex engine

**Branch:** `feature/package-simplification`
**Commit:** _pending_

AGY 1.4.2's `/codesearch` runs the in-process preview against
`rg`. ripgrep's pattern language is PCRE-flavored — lookarounds
(`(?=...)`, `(?<=...)`), named groups (`(?P<n>...)`), atomic
groups (`(?>...)`), inline flags (`(?i:...)`) — and it accepts
those without complaint. The clone's preview, by contrast, used
the standard library `regexp` (RE2), which rejects lookarounds
outright. The result: a user typed a questionably-formed pattern
the preview reported "no results"; the actual `rg` invocation
found matches. Or vice versa.

This cycle ports the in-process preview to
`github.com/dlclark/regexp2 v1.12.0` and adds the PCRE-feature
tests that pin the contract so a future rewrite that drops back
into stdlib regex is noticed.

The dep was already in the graph (PiG pulls it), so `go.mod`
only swaps the comment from indirect to direct. Binary delta:
_±0 MiB_; the code carries the same regex state.

## What changed

`cmd/wagyu/slash_actions.go`:

| Symbol | Before | After |
|---|---|---|
| `codeSearchMatcher` | `regexp.Compile` / `regexp.QuoteMeta` | `regexp2.Compile(query, regexp2.None)` / `regexp2.Escape` |
| `searchWorkspace` | `re.FindStringIndex(line)` (returns `[start,end]`) | `re.FindStringMatch(line)` → `m.Index` (single rune offset) |
| `copyToClipboard` | unconditional atotto shell-out | OSC-52-first, shell-out fallback |

The `copyToClipboard` edit was a small cycle-2 follow-up: the
test I committed in cycle 2 (`TestCopy_OSC52Preferred_WhenSupported`)
expected the OSC-52 path to run before the shell-out, but the
production wiring wasn't added in cycle 2. This commit closes
that gap (the cycle-2 deliverable describes the integrated path).

`cmd/wagyu/slash_actions_test.go` (+107 lines):

| Test | What it pins |
|---|---|
| `TestCodeSearch_PCREFeatures` | 5 sub-tests for look-arounds, look-behinds, non-capture groups, anchored alternation, word boundaries, plus a "compiles but doesn't match" case |
| `TestCodeSearch_NonASCIIColumns` | Documents the rune-vs-byte caveat of `regexp2.Match.Index` (see Caveats below) |

The existing tests (`TestCodeSearchFeedback`, `TestCodeSearch_Truncation`,
`TestCodeSearch_BinarySkipped`) pass unchanged: ASCII content
makes rune offset = byte offset, so the column number is stable
on the common path.

`go.mod`: `github.com/dlclark/regexp2 v1.12.0` is now a direct
require (was indirect via PiG).

## Caveats

### `regexp2.Match.Index` is a rune offset, not a byte offset

This is a **documented** design choice of regexp2 (the engine is
rune-based throughout). The clone's `/codesearch` preview reports
the column at `m.Index + 1`, matching the ripgrep "n:line:col:
text" format. For ASCII content (the common case in code), rune
offset equals byte offset and the column is correct. For
non-ASCII content with a multi-byte rune between line-start and
the match, the column will be counted in runes. `TestCodeSearch_NonASCIIColumns`
pins this behavior so a future migration to a byte-counting
engine is noticed.

The fix, if it ever matters, is to compute `utf8.RuneCountInString(line[:m.Index])`
explicitly. Worth doing the day /codesearch is asked to surface
columns in non-ASCII code; not done here because it's not a
correctness regression for the current usage.

### Compile constants

`regexp2.None` is the zero value (`0x0`); `regexp2.NoneDefault`
does not exist. The first attempt at this cycle used the
non-existent constant and the cycle 3 build failed with
`undefined: regexp2.NoneDefault`. The corrected call is
`regexp2.Compile(query, regexp2.None)`.

`m, err := re.FindStringMatch(line)` returns a `(nil, nil)`
pair when the pattern compiled cleanly but did not match the
line. The clone's `if err != nil || m == nil { continue }`
treats this as "no hit"; an in-stream error (very rare —
catastrophic backtracking on non-greedy quantifiers) is also
treated as "no hit" so the search still produces a partial
result set.

### Literal-mode semantics

`-F` / `--literal` still wraps the query in `regexp2.Escape`,
the package's `QuoteMeta` equivalent. The cycle-103 contract
that `-F` does a fixed-string match is preserved. The test
fixture for that lives in `TestCodeSearchFeedback`:

```go
if got := codeSearchFeedback("-F n.edle", dir); got != "No results" {
    t.Errorf("literal search = %q", got)
}
```

## Test results

```
ok  github.com/chadgpt/openagy/cmd/wagyu  1.337s
```

Cycle-specific subset:

| Test | Result |
|---|---|
| `TestCodeSearch_PCREFeatures/lookahead` | PASS |
| `TestCodeSearch_PCREFeatures/lookbehind` | PASS |
| `TestCodeSearch_PCREFeatures/non-capture-group` | PASS |
| `TestCodeSearch_PCREFeatures/anchored-alt` | PASS |
| `TestCodeSearch_PCREFeatures/word-boundary` | PASS |
| `TestCodeSearch_PCREFeatures/no-match-pattern` | PASS |
| `TestCodeSearch_NonASCIIColumns` | PASS |
| `TestCopy_OSC52Preferred_WhenSupported` | PASS (cycle 2 follow-up) |

Two pre-existing flakes remain (out of scope, see `AGENTS.md`):

- `TestDispatchPrintSlash_LocalCommands` — flake
- `TestChangelog_MatchesReferenceBinary` — flake

## Files touched

```
cmd/wagyu/slash_actions.go       | 50 ++++++++++++++++++++++++-----
cmd/wagyu/slash_actions_test.go  | 107 +++++++++++++++++++++++++++++++++++++
go.mod                            |   2 +-
3 files changed, 151 insertions(+), 8 deletions(-)
```

## Why regexp2 and not stdlib regexp+ lookaround polyfill

Three shorter paths were considered and rejected:

1. **Stay on `regexp` (RE2)** — RE2 is what most Go projects
   use, but it deliberately omits lookarounds. Adding a
   lookaround polyfill would have required porting PCRE's
   backtracking simulator; that's a larger surface than
   swapping the engine.
2. **`re2` (creachadair/regex)** — same grammar as stdlib,
   same omissions.
3. **RE2 with a custom `.+?` engine in front of `rg`** — the
   preview would still disagree when the user uses lookarounds,
   since the *real* `rg` invocation accepts them.

`regexp2` ships PCRE with a sensible Go API: `Compile`,
`Escape`, `FindStringMatch`, `ReplaceAllString`. Same external
interface as stdlib for the parts we use; wider grammar.
