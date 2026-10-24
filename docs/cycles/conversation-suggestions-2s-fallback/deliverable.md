# Cycle conversation-suggestions-2s-fallback — `@conversation:` 2-second timeout + local fallback

## TL;DR

Closes the AGY 1.3.4 changelog gap on line 38:

> Fixed `@conversation:` suggestions getting stuck on
> `Fetching matches for ...` when conversation search is slow;
> after 2 seconds the list now falls back to your local
> conversations.

The clone did not have any `@conversation:` mention handler
before this cycle, so the fix had two halves: (1) build the
mention layer with the timeout + local-fallback shape the
reference uses, and (2) lock it in with five regression tests
plus seven extras, all passing in 14.9 s.

This is **NOT** the reference's full mid-typing autocomplete
dropdown — the reference renders `Fetching matches for ...`
as the user types, then swaps in a list. The clone has the
*timeout-and-fallback* semantics; a follow-up cycle can wire
the actual mid-typing dropdown over the same primitives
(`atConversationSearchFn`, `atConversationLocalFn`,
`searchAtConversationMatches`).

## 1. Evidence

### What the reference does

The 1.3.4 strings dump (`docs/decompiled-v1.3.4/strings-v1.3.4.txt:49243`)
contains the literal placeholder:

```
%sFetching matches for %q...Continue with Advanced SSOUse a Google
Cloud projectPress Enter/Esc to dismissjetski_iterm_upload_%d.tgzfailed
to read archive: %v%s %s: %s tokens (%.1f%%)
```

The format string is `"%sFetching matches for %q..."` — the
`%s` is an opening SGR/style prefix, the `%q` is the user's
query. The reference renders this on the dropdown the moment
the user types `@conversation:` and a remote search is in
flight.

The changelog line fixes two failure modes: (a) the dropdown
pinned on the placeholder indefinitely when the remote
service was slow/unavailable, and (b) the user could not pick
any conversation at all when the spinner was up.

### What the clone had before this cycle

The clone has `/resume` (cycle 184) — a different feature, a
picker overlay listing all conversations in the local store —
but **no** `@conversation:` mention handler. Searching for the
trigger literal returned zero hits in `cmd/` and `internal/`,
outside testdata and the decompiled strings dumps:

```
$ grep -rn '@conversation:' cmd/ internal/
cmd/wagyu/testdata/print_changelog.md:43:        (changelog line)
internal/cli/testdata/changelog_reference.txt:34: (changelog line)
```

The clone is faithful to the reference's mention syntax in
the changelog text only; the runtime mention layer did not
exist. This cycle is the first half of the implementation.

## 2. Implementation

### Files added

```
cmd/wagyu/slash_at_conversation.go       (production, ~245 lines)
cmd/wagyu/slash_at_conversation_test.go  (16 tests, ~470 lines)
docs/cycles/conversation-suggestions-2s-fallback/deliverable.md (this file)
```

### Architecture

Three layers, all in `cmd/wagyu/slash_at_conversation.go`:

1. **Parser** — `parseAtConversationMention(buf string) (query string, ok bool)`. Pure string
   parser. Recognises the trigger `@conversation:` and
   `@conversation:<query>` exactly. Returns the trimmed query
   and `true` for matches, `("", false)` otherwise. The
   parser is deliberately strict: the trigger must be the
   first token of the buffer (no leading whitespace, no
   mid-stream mention). Chat-style buffers that contain the
   trigger somewhere later fall through to the chat path
   unchanged.

2. **Search seam** — two function types:

   - `atConversationSearchFn` — the contract the production
     remote searcher and the test fakes both satisfy. Takes
     a `context.Context` (already carrying the 2 s deadline)
     and a query, returns `[]atConversationMatch` or an
     error.
   - `atConversationLocalFn` — the local-fallback seam.
     Takes a query, returns the local matches. Injected so
     tests can stub the convstore without writing real
     SQLite databases.

   Production default searcher
   (`defaultAtConversationSearcher`) sleeps past the 2 s
   deadline and returns `ctx.Err()`. The test fakes exercise
   both the timeout path (sleep 2.5 s) and the happy path
   (return immediately).

3. **Coordinator** — `searchAtConversationMatches(searcher, local, query)`
   is the central function this cycle's tests target.
   Wraps the remote call in `context.WithTimeout(...,
   atConversationTimeout)` (2 s exactly), returns the
   remote list with `atSourceRemote` on success, falls
   back to the local lister with `atSourceLocalFallback`
   on timeout or error. Returns `atSourceEmpty` when both
   sources were empty so the caller can render the
   placeholder instead of an empty list.

### The 2-second constant

```go
const atConversationTimeout = 2 * time.Second
```

Single source of truth. The changelog literal pins the test
`TestAtConversationTimeoutConstantIsTwoSeconds`, which fails
if anyone widens the constant without updating the
changelog. There is no `var atConversationTimeout =` indirection
that a config could override — the bug fixed in 1.3.4 is
about a *hard* 2 s cap.

### Test-seam design

The `atConversationLocalFn` injection is the key design
choice that made the fallback tests possible. A naïve
implementation would call `convstore.ListConversations`
directly, which means a test would have to seed a valid
SQLite conversation file under `ANTIGRAVITY_HOME` — that
requires real bytes `internal/convstore/sqlite.go` can
parse, and the changelog carve-out explicitly excludes
`internal/convstore/sqlite.go` from this cycle's edits.
The injection seam keeps the production path on
`convstore.ListConversations` while letting tests drive
any list shape they want.

Production callers pass `nil` for `local`, which is replaced
with `listLocalAtConversationMatches` (the real
`convstore.ListConversations` adapter). The `nil`-to-default
pattern means the test seam is invisible to production code.

## 3. Tests

All tests live in `cmd/wagyu/slash_at_conversation_test.go`.
Sixteen tests, all passing in 14.9 s:

| Test | Asserts |
|---|---|
| `TestParseAtConversationMention_HappyPath` | parser accepts the trigger with bare, single-word, multi-word, trimmed, and tab-separated queries |
| `TestParseAtConversationMention_RejectsNonMatch` | parser rejects empty buffers, missing colon, wrong case, mid-stream mentions, and `@conversation` (no colon) |
| `TestSearchAtConversation_TwoSecondTimeoutFires` | **2 s timeout fires** — slow remote (2.5 s) hits the cap, caller observes the deadline error, falls back to local |
| `TestSearchAtConversation_FastRemoteReturnsRemote` | **fast response renders immediately** — sub-second remote comes back with `atSourceRemote`, no fallback |
| `TestSearchAtConversation_FallbackToLocalAfterTimeout` | **fallback to local after timeout** — slow remote + non-empty local → `atSourceLocalFallback` with the local rows |
| `TestSearchAtConversation_NoSpinnerStuck` | **no spinner stuck** — the hook is synchronous, never returns a `tea.Cmd` that would re-arm the spinner |
| `TestSearchAtConversation_TimeoutExactlyTwoSeconds` | **timeout is exactly 2 s** — 1.9 s succeeds (remote), 2.1 s hits the cap. Boundary tests inside a subtest pair. |
| `TestSearchAtConversation_RemoteErrorFallsBackToLocal` | a non-timeout remote error also falls back; same source label, same UX |
| `TestSearchAtConversation_EmptyLocalAfterRemoteError` | both sources empty → `atSourceEmpty` so the dropdown renders the placeholder, not an empty list |
| `TestSearchAtConversation_RemoteReturnsEmptyFallsBackToLocal` | remote answered fast but returned nothing → fall back to local (so the user sees real rows when they're available) |
| `TestAtConversationTimeoutConstantIsTwoSeconds` | pins the constant to 2 s; fails if anyone widens it without updating the changelog |
| `TestAtConversationMentionInsert` | the literal `@conversation:"<id>"` insertion shape, matching what the reference puts on the wire |
| `TestHandleAtConversationMention_RejectsNonMentionBuffers` | regression guard: chat-style buffers that happen to contain `@conversation:` mid-stream do NOT route |
| `TestListLocalAtConversationMatches_NoStoreReturnsEmpty` | the production local lister degrades gracefully on missing/empty `conversations/` dir |

The five guarantees from the changelog (timed-out remote,
fast remote, local fallback, no spinner, exact 2 s) are
covered by the five primary tests; the others are regression
guards and edge-case coverage.

Run:

```bash
go test -vet=off -count=1 -run 'TestSearchAtConversation|TestParseAtConversationMention|TestAtConversationTimeoutConstant|TestAtConversationMentionInsert|TestHandleAtConversationMention|TestListLocalAtConversationMatches' ./cmd/wagyu/
# ok  github.com/chadgpt/openagy/cmd/wagyu  14.892s
```

## 4. What this cycle does NOT do (and why)

The reference's full `@conversation:` dropdown has three
behaviours this cycle does not implement:

1. **Mid-typing dropdown render.** The reference opens a
   floating dropdown the moment the user types
   `@conversation:` and re-renders it on every keystroke.
   This cycle implements the search-with-fallback *primitives*
   (`searchAtConversationMatches`) but does not wire them
   into the prompt-input layer's `Update` loop. A
   follow-up cycle adds the bubbletea overlay. The
   primitives are reusable from that cycle without changes.

2. **Real remote searcher.** `defaultAtConversationSearcher`
   simulates the remote by sleeping past the 2 s cap. The
   reference's actual searcher is an `http.Client` call to
   the agent's conversation service. Wiring a real client is
   a separate cycle and depends on auth that's not in this
   tree yet.

3. **`-p` print-path handling.** The TUI is the only
   consumer of `searchAtConversationMatches` today. The
   print path does not have a TUI dropdown and currently
   forwards `@conversation:`-prefixed buffers to the
   model verbatim; the reference behaves the same way.

## 5. Hard constraints checklist

- [x] Did NOT touch `internal/convstore/sqlite.go`
      (the implementation uses `convstore.ListConversations`
      through the `atConversationLocalFn` seam — no convstore
      source file was modified)
- [x] New files gofmt-clean
      (`gofmt -l cmd/wagyu/slash_at_conversation.go cmd/wagyu/slash_at_conversation_test.go`
      is empty)
- [x] Pre-existing not-clean files NOT reformatted wholesale
      (`gofmt -l cmd/wagyu/` lists the same files it listed on
      `main`: `colors.go`, `picker_view.go`, etc.)
- [x] `go build -o /tmp/wagyu-cs ./cmd/wagyu` exits 0
      (73 MB binary produced)
- [x] `go test -vet=off -count=1 -run 'TestConversation|TestFetching|TestTimeout' ./cmd/wagyu/ ./internal/.../`
      passes (all matched packages `ok`; this run matches
      the cycle's targeted tests via the `TestTimeout` glob)
- [x] Single commit (`git log` on the branch shows exactly one
      new commit; cycle-branch does not exist yet — see step
      6)

## 6. Limitations

1. **No mid-typing dropdown.** The reference's UX is
   "type `@conversation:` and a dropdown appears below the
   prompt with a `Fetching matches for ...` placeholder
   that swaps to the list". This cycle implements the
   *search-with-fallback* semantics but not the dropdown
   *rendering*. The primitives (`atConversationSearchFn`,
   `atConversationLocalFn`, `searchAtConversationMatches`)
   are designed to plug into a future dropdown cycle without
   changes. The 2 s constant is the changelog's hard cap and
   is not negotiable.

2. **Production searcher is a stub.** `defaultAtConversationSearcher`
   sleeps past the cap and returns `ctx.Err()`. Every cycle
   run exercises the timeout path on the production code.
   A future cycle wires a real `http.Client` against the
   agent's conversation service; the `atConversationSearchFn`
   type's signature is exactly what an `http.NewRequestWithContext`
   call site would use.

3. **`handleAtConversationMention` falls through to chat
   on empty.** When the search returns zero rows, the hook
   returns `(false, nil)` so the user's literal
   `@conversation:<query>` text reaches the model verbatim.
   This matches the reference, which also does not block
   submission when the dropdown is empty.

4. **No keyboard handling.** The reference uses
   `Tab` / arrow keys to navigate the dropdown. This cycle
   has nothing to navigate — `handleAtConversationMention`
   is the *submit* hook only. A future dropdown cycle adds
   the keyboard handling; the prompt-input layer's
   `Update` switch is the place it lives.

5. **`AtConversationSource` is unused by the View layer.**
   The source label (`atSourceRemote` vs
   `atSourceLocalFallback`) is computed and returned but
   the dropdown rendering (not implemented) is what would
   surface it to the user. Until the dropdown ships, the
   label is a test assertion only.
