# Cycle 281 — the "hot-path line-count" claim: agy 1.4.2 vs. the clone

## Verdict

The gist's claim has three parts. The first two can be checked against the
shipped 1.4.2 binary, but only at the level of symbol shape — no 1.1.1 binary is
available to show what changed, and no decompilation was done. The third is a
live measurement, and it was made on the clone, not the reference.

| Claim | Result |
|---|---|
| 1.1.2 made the hot-path line-count methods pointer receivers | **Consistent with 1.4.2.** Every surviving line/row-count method on the conversation list (`ConversationModel`) is a `(*T)` method, and so is the prefix-sum updater; the value-receiver survivors are the bulk builders, not the per-frame counters. Whether 1.1.2 is the release that *changed* them cannot be proven here — no 1.1.1 binary was available to diff. 【一手】 for the 1.4.2 symbol shape, 【推测】 for the delta. |
| The per-frame prefix-sum pass was the cost | **Has a concrete carrier in 1.4.2.** `(*mouseState).updatePrefixSums` is exactly such a method, a pointer receiver in the same package as the line-count methods, sitting beside `absLineToPosition`/`SyncPosition`. The clone had the same shape — three O(transcript) passes per frame — and it cost 11.7 ms and 27.2 MB **per frame** at 4000 steps. |
| 5000+ steps ⇒ sustained 99 % CPU and keystroke lag | **Reproduced in the clone before the fix**: 98.2–103.1 % CPU while typing at 5 keys/s, 320 ms p50 keystroke→frame on a 5000-step / 9.4 MB conversation. Not measured in the reference (see 硬约束). |

What this cycle did about it: replaced the clone's per-frame rebuild with two
pointer-receiver caches (an incremental wrap slot and a composed-row cache) plus
an O(log n) prefix-sum trim, without changing a single rendered byte. A keystroke
on the 5000-step transcript went from **1.07 s / 1.32 GB allocated** to
**0.46 ms / 74 KB**.

Evidence grades follow the repo convention: 【一手】 first-hand against the binary
or a live run · 【反编译】 decompiled output · 【strings】 string table only ·
【推测】 hypothesis.

---

## 1. The claim

The recovered changelog — the reference embeds its own release notes, and the
clone carries a copy at `internal/cli/testdata/changelog_reference.txt` — has
this entry under `1.1.2:` (line 702):

> Improved responsiveness on large conversations (5000+ steps) in no flickering
> mode by switching hot-path line-count methods to pointer receivers, cutting the
> per-frame prefix-sum cost and eliminating sustained 99 % CPU and keystroke lag.

Two adjacent entries make the story more specific, and both are also in the
binary's string table:

- `1.1.3:` — "Fixed high CPU and unbounded render cost on large conversations in
  no-flickering mode by **making index rebuilds idempotent so the conversation
  index converges instead of growing on every rebuild**."
- `1.2.16:` — "Improved responsiveness in long conversations in no flicker
  (altscreen) mode."

So by the reference's own account the receiver switch was one step in a
multi-release effort, and a later release still had an index that *grew on every
rebuild*. That matters for grading the gist: the 1.1.2 note describes a real fix,
not necessarily the whole fix.

"No flickering mode" is the altscreen renderer. The binary's own settings text
describes it as: "no flickering (altscreen) is ideal for long conversations but
requires holding Shift/Option for native text selection. Native Terminal
experience (inline) preserves native terminal behavior but may truncate long
conversations and flicker when toggling views." 【strings】

---

## 2. Static evidence in agy 1.4.2

Target: `~/.local/bin/agy`, version 1.4.2, SHA-256
`df2d4fb571d9be0557c32a5ce899fc3773f43b2ee3b18bee0d0340d14dbf9590`.

Names were re-derived with the repo's own tool rather than trusted from an older
dump:

```bash
cd rea-go-seeds && go run . ~/.local/bin/agy \
  /private/var/folders/.../T/opencode/agy142-seeds-hot
```

158,955 names, SHA-256 of `seeds.txt`
`1ac7549c9bf9f109c296d8455e58e48c8350b5221189e8a38bbdcf3947de10d6` — byte
identical to the earlier `agy142-seeds` dump, so that dump is confirmed to be
this 1.4.2 build. 【一手】

### 2.1 The hot-path methods are pointer receivers

Go's pclntab encodes the receiver in the symbol name: `pkg.(*T).M` is emitted only
for a pointer-receiver method, `pkg.T.M` for a value receiver. Reading the names
is therefore first-hand evidence of the receiver kind (【一手】), though not of
what the method body does.

| Symbol | Address | Receiver |
|---|---|---|
| `…/cli/model/model.(*ConversationModel).messageListItemLineCount` | `0x1029054a0` | **pointer** |
| `…/cli/model/model.(*ConversationModel).messageListItemLineCount-fm` | `0x102a38930` | method-value wrapper |
| `…/cli/model/model.(*ConversationModel).messageListRow` | `0x1029124a0` | **pointer** |
| `…/cli/model/model.(*ConversationModel).messageListRow-fm` | `0x102a38980` | method-value wrapper |
| `…/cli/model/model.(*lineWidthCache).width` | `0x1029557c0` | **pointer** |
| `…/cli/model/model.(*lineWidthCache).width-fm` | `0x102a388e0` | method-value wrapper |
| `…/cli/model/model.(*detailRowsCache).sync` | `0x1029437a0` | **pointer** |
| `…/cli/model/model.(*entriesCache).invalidate` | `0x1028d8f80` | **pointer** |
| `…/cli/model/model.(*scrollDebouncer).CachedView` | `0x1029cdde0` | **pointer** |
| `…/cli/model/model.(*scrollDebouncer).CacheView` | `0x1029cde60` | **pointer** |
| `…/cli/model/model.(*scrollDebouncer).Debounce` | `0x1029cdbd0` | **pointer** |
| `…/cli/model/model.(*scrollDebouncer).Flush` | `0x1029cdd40` | **pointer** |
| `…/cli/model/model.(*mouseState).updatePrefixSums` | `0x102990910` | **pointer** |
| `…/cli/model/model.(*mouseState).absLineToPosition` | `0x102990b10` | **pointer** |
| `…/cli/model/model.(*mouseState).SyncPosition` | `0x102990490` | **pointer** |
| `…/cli/model/model.(*mouseState).RenderScrollbar` | `0x1029907f0` | **pointer** |
| `…/cli/model/model.indexForLine` | `0x102947140` | free function |
| `…/cli/model/model.listLineIndex.pos` | `0x1029cf3f0` | value |

`listLineIndex.pos` is the exception that proves the rule: a pure lookup on a
small index value can be a value method. `updatePrefixSums`, by contrast, writes
the sums into the receiver — as a value method it would compute them and throw
them away on every call.

The value-receiver survivors are the wholesale builders, which do not run per
frame:

| Symbol | Address | Receiver |
|---|---|---|
| `…/cli/model/model.ConversationModel.buildRenderedItems` | `0x102905580` | value |
| `…/cli/model/model.ConversationModel.renderMessageListItems` | `0x1029119c0` | value |
| `…/cli/model/model.ConversationModel.hasContentBelow` | `0x102912590` | value |

This is exactly the split the changelog describes: the *counters* that a scroll,
a mouse move, or a keystroke hits are pointer methods; the *builders* that
construct the list are not. The `-fm` wrappers on `messageListItemLineCount` and
`lineWidthCache.width` mean method values of those two are formed somewhere —
consistent with them being passed as callbacks into a per-item walk, but that is
an inference (【推测】) from the presence of the wrapper, not a call site we
resolved.

`indexForLine` is a **free function**, not a model method. Reading only the
symbol table, the line→index arithmetic has been lifted out of the model and
sits beside caches that own the results — which is the architectural shape the
gist is describing. 【一手】 for the shape, 【推测】 for the call graph.

### 2.2 Negative results

- **Zero symbols** match `flicker`, `noflick`, `hotpath`, `lineCache`,
  `renderCache`, or `rowCache` in the 158,955-name dump. `flicker` does appear 13
  times in `strings`, but only in release notes and the settings description — it
  is a feature name, not an identifier. 【一手】/【strings】
- The only `prefix`-sum symbol in the whole 158,955-name dump is
  `(*mouseState).updatePrefixSums`; there is no separate `prefixSum` type, and
  none of the 286 `prefix` hits is a per-frame counter in `ConversationModel`
  itself. The sums live on `mouseState`, which also owns
  `absLineToPosition`, `SyncPosition`, `ProcessSelectionIntent` and
  `RenderScrollbar` — i.e. the screen-row ↔ text-position mapping used by
  selection and the scrollbar. 【一手】 for the symbol, 【推测】 for the use: no
  call site was resolved.
- `listLineIndex` exists as a type with a single visible method, `pos`; combined
  with the free function `indexForLine` and the per-item `messageListItemLineCount`,
  this is a line-index structure that is built once and queried afterwards,
  rather than recomputed per frame. 【一手】 for the symbols, 【推测】 for the
  protocol.
- There is no second, value-receiver copy of `messageListItemLineCount`,
  `messageListRow` or `updatePrefixSums` in the dump, so 1.4.2 carries one form
  of each. 【一手】

### 2.3 What this establishes

A user scrolling a 5000-step conversation in 1.4.2 runs `(*ConversationModel)`
methods over a `lineWidthCache` keyed by width and a `detailRowsCache` that is
explicitly `sync`ed, with per-message line counts served by pointer methods. The
screen-row ↔ text-position mapping is a pointer-method `mouseState` with an
`updatePrefixSums` step. The per-frame work is designed to be cache lookups and
incremental updates, not recomputation.

The prefix-sum method is the most direct hit on the changelog's wording. It is a
*mutator*: it recomputes sums and stores them on the receiver, which is why the
pointer receiver is not stylistic. A value receiver would copy `mouseState`,
write the sums into the copy, and discard it — the sums would then be stale or
absent on the next frame, and the only way to get an answer would be to walk the
line index again. That is the "per-frame prefix-sum cost" described in 1.1.2. 【一手】
for the signature, 【推测】 for the failure mode it prevents.

What it does not establish: that any of this arrived in 1.1.2, that the 1.4.2
build still has the 99 % CPU problem, or that the pointer receiver is what made
the difference (it could have been the cache restructuring that shipped with it).
Those are graded in 存疑.

---

## 3. The clone before the change

The clone's render path is `runMode.transcriptWindow`
(`cmd/wagyu/tui_viewport.go`), reached from `View()` on every frame, scroll or
not. Before this cycle it rebuilt three transcript-sized structures per frame:

1. **`wrappedResponse`** was cached (cycle 124) for scroll messages, but
   `Update()` set `rowsCacheDirty` for *any* non-scroll message — every typed
   character, every spinner tick — and the rebuild re-wrapped **every** entry of
   `m.response` at the terminal width *and* a second time at `width-1`.
2. **Composition**: `rows := make(…, len(header)+len(resp)+len(live))` plus two
   `append`s — a fresh copy of every row string header per frame.
3. **Counts + prompt scan**: `counts := make([]int, len(rows))` filled with
   `screenRows` per row, and a full scan of `resp` for `isUserPromptRow`.

A 5000-step transcript with 4 KB replies is ~20 MB of text, 20,000 `m.response`
entries and roughly 120,000 wrapped rows at 200 columns. Per keystroke that
produced a **1.32 GB allocation** — roughly 65× the transcript, because the wrap
cache allocated a second full copy while the first was still live — and 1.07 s
of wall time. The screen-row count pass and the prompt scan each walked the same
rows again.

The cycle-124 cache made *scroll* fast (17 ms p50, unchanged by this cycle,
because scrolling already hit the cache). It did nothing for typing, which is
precisely the "keystroke lag" half of the claim.

---

## 4. The change

Two new pointer-receiver types in `cmd/wagyu/tui_viewport.go`, plus a binary
search, wired through `runMode` in `cmd/wagyu/tui_run.go`. No renderer, layout,
or glyph changed.

### 4.1 `wrapSlot` — incremental wrap

```go
type wrapSlot struct {
    built  bool
    width  int
    src    []string
    spans  []wrapSpan   // off/n into rows, one per src entry
    rows   []string
    counts []int
    scratchRows   []string
    scratchCounts []int
}
func (s *wrapSlot) rebuild(resp []string, width int) bool
```

`rebuild` walks `resp` and, for every entry whose string is unchanged since the
last build, copies the previous span instead of calling `wrapRows`. Go string
equality short-circuits on the data pointer, so an unchanged 4 KB reply costs one
comparison. Streaming an appended chunk re-wraps the tail; invalidating on a
keystroke that only touched the input re-wraps nothing at all and returns
`false`. The scratch slices are swapped with the live ones, so a rebuild reuses
the previous buffers instead of allocating two new O(rows) slices.

The eager `width-1` rebuild was **dropped**. It had been dead since cycle 134
(the overflow path no longer reserves a scrollbar column) and was a second full
wrap on every invalidation. The narrower slot still exists and is built lazily if
some caller explicitly asks for a width other than `m.width`.

### 4.2 `composedRows` — the per-frame layout cache

```go
type composedRows struct {
    valid  bool
    gen    int
    width  int
    header []string
    live   []string
    rows   []string
    counts []int
    prefix []int   // prefix[i] == sum(counts[:i]); len == len(rows)+1
    prompt []int   // index into rows of each user prompt
    …
}
func (c *composedRows) fresh(gen, width int, header, live []string) bool
func (c *composedRows) rebuild(gen, width int, header, resp, live []string, respCounts []int)
```

A warm frame is now: compare the header and live rows (a handful of strings),
compare `rowsGen`/width, and use the cached `rows`, `counts`, `prefix`, `prompt`.
Nothing proportional to the transcript is touched.

Invalidation is one integer. `(*wrapSlot).rebuild` bumps `m.rowsGen` **only when
it actually changed a row**, so a keystroke that does not alter the transcript
leaves the composition valid; a streamed chunk invalidates it once. Header
changes that never go through `m.response` — the fallback warning block, model
switch, spinner text — are caught by the `header`/`live` snapshot comparison
rather than by generation.

`m.userPromptRows` now aliases `c.prompt` (double-buffered, so it stays valid
until the next rebuild). `transcriptWindow` must never append to it; that is
noted at the assignment.

### 4.3 `clipToScreenRowsPrefix`

The cycle-124 trim rescanned the visible window per trimmed row
(O(window²), window-bounded but still the top of a profile). With `prefix` cached
it is a `sort.Search` — O(log window) — and the reference
`clipToScreenRows` is kept as the test-visible implementation.
`TestClipToScreenRowsPrefixMatches` exhaustively compares the two over every
offset/length/limit in a 11-row fixture.

The viewport slice is now copied out of the cache before padding
(`append([]string(nil), rows[scroll:scroll+avail]...)`), because `visible` used
to be a fresh slice and `append` must not write into the next cached row.

### 4.4 `stickyUserPromptRow`

The sticky-bar lookup walked `userPromptRows` from the start on every frame
while scrolled. The list is sorted by construction, so it is now
`sort.SearchInts`.

### 4.5 Why pointer receivers are the right shape here

Both types are mutated in place: `rebuild` replaces six slices, two ints and a
bool, and keeps scratch buffers for the next call. A value receiver would copy
the struct — six slice headers plus the scratch — and silently throw the rebuild
away, leaving the caller with a cache that never fills. That failure mode is
invisible at small sizes and only shows up as "it got slow again" on a long
transcript, which is the same class of bug the changelog describes. Making the
mutators pointer methods is not cosmetic; it is what makes the cache a cache.

---

## 5. Measurements

Machine: Apple M4 (10 cores), macOS, Go 1.27.0. Clone built with
`go build -o /tmp/wagyu-hot ./cmd/wagyu`.

### 5.1 Go benchmarks

Same benchmarks, same binary shape, pre-fix tree restored with
`git stash` around the two source files (the untracked bench file compiles
against both). `-benchtime 1s` except the pre-fix keystroke case, which is 1 s
per iteration and was run `-benchtime 3x`.

| Benchmark (200×40) | Before | After | Δ |
|---|---|---|---|
| `BenchmarkViewWarm` — 4000 steps × 70 KB reply, one scroll frame | 11.74 ms · 27.16 MB · 747 allocs | 0.262–0.265 ms · 45.1 KB · 742 allocs | **44× faster, 600× fewer bytes** |
| `BenchmarkViewWarmScroll5000Steps` — 5000 steps × 200 B | 0.510 ms · 590 KB · 799 | 0.309–0.336 ms · 99.5 KB · 798 | 1.6× faster, 5.9× fewer bytes |
| `BenchmarkViewWarmScroll5000LongSteps` — 5000 × 4 KB (~20 MB) | 1.413 ms · 2.97 MB · 756 | 0.408–0.410 ms · 77.2 KB · 755 | 3.5× faster, 38× fewer bytes |
| `BenchmarkViewKeystrokeRewrap5000LongSteps` — same transcript, cache invalidated as a keystroke does | 1069 ms · 1.32 GB · 270,793 | 0.456–0.464 ms · 74.3 KB · 751 | **~2300× faster, 17,700× fewer bytes** |

The 5000-short row is the honest one: at that size the per-frame *chrome*
(~100 KB, 798 allocations: separators, input line, status, title, padding) is
already larger than the transcript work, so the speedup is only 1.6×. The long
row is where the transcript dominates, and the keystroke row is the claim's own
shape.

Allocation is the number that matters, because it is what turns a keystroke into
a GC pause. The keystroke path no longer allocates a wrapped copy of the
conversation at all.

### 5.2 Scaling guard

`TestWarmFrameAllocsIndependentOfTranscriptLength` renders 200 warm frames at
1000 and 8000 steps:

| | 1000 steps | 8000 steps | ratio |
|---|---|---|---|
| before, bytes/frame | 202 KB | 890 KB | **4.41×** |
| after, bytes/frame | 99,847 B | 99,849 B | **1.00×** |
| after, time/frame | 311 µs | 308 µs | **0.99×** |

The test fails above 1.6×. The pre-fix measurements (202 KB vs 890 KB, 4.41×)
would fail it; the current tree passes at 1.00×. This is the actual regression
guard — the benchmarks document the numbers, this pins the shape.

### 5.3 End-to-end TUI

Synthetic store `hp-synth-5000-long`: 5000 steps × 4 KB replies, 9.4 MB SQLite
database, written by `cmd/hpgen` into a throwaway `ANTIGRAVITY_HOME`. Two tmux
sessions at 200×40, `/tmp/wagyu-base` vs `/tmp/wagyu-hot`, same store, keys sent
with `tmux send-keys`; latency measured by polling `tmux capture-pane -p` until
the frame changes.

| Metric | Before | After |
|---|---|---|
| keystroke→frame, p50 | 320.0 ms (n=10) | **16.5 ms** (n=15) |
| keystroke→frame, p95 | 344.4 ms | **32.5 ms** |
| CPU while typing 5 keys/s | 98.2–103.1 % (10 samples) | **0.5–0.9 %** |
| CPU idle at the bottom | 0.0–0.2 % | 0.0–0.2 % |
| PgUp/PgDn→frame, p50 | 17.0 ms | 16.6 ms (unchanged — already cached) |
| RSS | 140 MB | 147 MB |

The scroll number is deliberately included: this cycle did **not** speed up
scrolling, because cycle 124 had already cached it. The 99 % CPU figure is the
pre-fix binary rebuilding a 20 MB transcript on every typed character; at that
rate the TUI never idles, which is exactly the "sustained" part of the claim.

RSS rose ~7 MB, and the arithmetic matches: 120,000 retained rows at 16 B (slice
header) + 8 B (count) + 8 B (prefix) = 3.8 MB, and the composition is
double-buffered, so ~7.6 MB. That is the trade the fix makes — a cache bounded by
the transcript, in exchange for not copying the transcript per keystroke.

---

## 6. Byte identity

The rendered output is unchanged by construction (the caches return the same
slices the old code rebuilt), and was checked two ways.

**Unit level.** `TestComposedCacheMatchesColdRender` builds a warm model,
renders it, and compares against a freshly built model with identical state at
scroll offsets 0, 1, 7, 100 and 400, with and without the fallback warning block,
then appends a streamed row and asserts it appears. Warm and cold renders are
compared as whole strings.

**Live level.** Three screens captured from each binary on the same 5000-step
store — bottom-anchored, scrolled one page, scrolled further with the sticky
prompt bar visible:

| Frame | Bytes | `diff` |
|---|---|---|
| bottom | 6,627 | empty |
| after PgUp | 6,627 | empty |
| scrolled, sticky bar | 7,203 | empty |

`tmux capture-pane` output, pre-fix vs post-fix: byte-identical in all three.

The one intentional difference is invisible: `visible` is copied out of the
cache instead of being a fresh slice, so `append` in the padding step cannot
write past the window.

---

## 7. Tests

New file `cmd/wagyu/tui_hotpath_bench_test.go`:

| Test | What it pins |
|---|---|
| `TestClipToScreenRowsPrefixMatches` | prefix-sum trim ≡ reference trim, exhaustively over offsets/lengths/limits |
| `TestComposedCacheMatchesColdRender` | warm render ≡ cold render, including header-only changes and an appended stream row |
| `TestWarmFrameAllocsIndependentOfTranscriptLength` | 8× steps must not grow bytes/frame beyond 1.6× |
| `TestWarmFrameTimeIndependentOfTranscriptLength` | same for wall time (3× bound, scheduler-tolerant) |

Plus four benchmarks used above.

`go build -o /tmp/wagyu-hot ./cmd/wagyu` — clean.
`go test ./cmd/... ./internal/...` with a throwaway `ANTIGRAVITY_HOME`:

```
--- FAIL: TestPanelSkillsDetailView (0.00s)
FAIL	github.com/chadgpt/openagy/cmd/wagyu
FAIL	github.com/chadgpt/openagy/internal/agent [build failed]
```

Both are pre-existing on the snapshot and unrelated: `TestPanelSkillsDetailView`
fails identically with the cycle stashed, and `internal/agent` does not compile
because `loop_tools_test.go` is missing imports (`errors`, `fmt`, `piagent`,
`textChunks`) — a test file this cycle did not touch. Every other package
passes. `gofmt -l` is clean on all four touched files.

---

## 8. 硬约束

- **The reference was not measured live.** `HOME=<throwaway> agy
  --conversation=…` stops at the login screen (`You are not logged into
  Antigravity`); the credential is keychain-backed and the real home was not
  touched. All reference claims in this report are static (symbol dump +
  embedded changelog). The pre-fix CPU/latency numbers are the *clone's*, not the
  reference's.
- **No 1.1.1 binary.** The claim that 1.1.2 *changed* the receivers cannot be
  checked directly; only the 1.4.2 end state is observable.
- **No decompilation.** Receiver kinds come from pclntab symbol names, which is
  authoritative for the receiver kind but says nothing about method bodies,
  call sites, or hotness. No call graph was resolved for these symbols.
- **The fix is an analogue, not a port.** The clone's cache types are modelled on
  the reference's shape (`lineWidthCache`, `detailRowsCache`, `entriesCache`) but
  are not the same code and were not derived from its disassembly.
- **Rendered output must stay byte-identical**, and does (section 6). No gist
  feature was implemented: no no-flickering mode, no OSC 7501, no `/compact`.
- Only the clone was modified: `cmd/wagyu/tui_viewport.go`,
  `cmd/wagyu/tui_run.go`, the new `cmd/wagyu/tui_hotpath_bench_test.go` and the
  new `cmd/hpgen/`. `rea-go-seeds/`, `out/`, the reference store, and the sibling
  worktree were not touched.

## 9. 存疑

- **Whether 1.4.2 still has the problem.** Its own 1.1.3 note says index rebuilds
  were made idempotent *after* the 1.1.2 receiver change, which implies the
  receiver change alone did not stop the index from growing. The 1.4.2 cache
  types look like the later state of that work; we cannot say when each landed.
- **Whether pointer receivers were the fix, or the cache restructuring shipped
  alongside them.** For `updatePrefixSums` a pointer receiver is mechanically
  required — it stores the sums — so a value→pointer switch there is exactly the
  difference between "recompute and keep" and "recompute and lose". For the
  line-count methods the argument is weaker: a pointer receiver only helps if the
  method caches into the model/item. Both were likely one change, but the method
  bodies were not read, so which half carried the win is unknown.
- **What `messageListItemLineCount` actually counts.** The name says "line
  count"; whether it walks wrapped rows or source lines is not established
  without decompilation. The clone's analogue is `screenRows` per wrapped row.
- **Whether `indexForLine` is O(log n) or O(n).** The name and its position
  beside the caches suggest a lookup, not a scan, but the body was not read.
- **The clone's remaining ~100 KB / 798 allocations per warm frame.** That is
  chrome (separators, input, status, padding) and does not scale with the
  transcript, but it is now the dominant per-frame cost at 5000 short steps.
  Reducing it is a separate, visual-risk change.
- **RSS +7 MB** is expected from retaining the composition, but only one store
  shape was measured; a 20 MB transcript holds more than a 1 MB one, and the
  caches are double-buffered.

## 10. limitations

- Single machine, single architecture (macOS/arm64, M4), no CI. Benchmark
  numbers are two runs of `-benchtime 1s` and vary by ±10 % between runs; the
  allocation counts are stable and are the load-bearing numbers.
- The synthetic store is uniform (4 KB replies, one shape of markdown). Real
  sessions mix short tool chatter and long diffs; the 5000-short benchmark is the
  conservative case, the 5000-long one the pessimistic case.
- Latency was measured through tmux (`send-keys` + `capture-pane` polling), which
  has a floor of roughly one poll interval (~10–16 ms). The pre-fix 320 ms is far
  above it; the post-fix 16.5 ms is at it, so the after-number is an upper bound.
- CPU was sampled with `ps` while a script typed at 5 keys/s, not by a human at a
  keyboard; "sustained 99 %" is a reproduction of the shape, not a field report.
- The render benchmarks call `m.View()` on a synthetic `m.response`; they do not
  exercise markdown rendering or syntax highlighting, which happen when rows are
  constructed, not when they are composed. The cycle's claim is about the compose
  path only.
- The reference store used for the byte-identity check is the clone's synthetic
  store, not a real 5000-step agy conversation. Frame identity was checked
  between two clone binaries, not against the reference.

## 11. Reproduction

```bash
# build
/Users/gear/go/bin/go build -o /tmp/wagyu-hot ./cmd/wagyu

# synthetic 5000-step store (refuses to run without a throwaway home)
export ANTIGRAVITY_HOME=$(mktemp -d)
/Users/gear/go/bin/go run ./cmd/hpgen -steps 5000 -reply 4096 -id hp-synth-5000-long

# guards + benchmarks
/Users/gear/go/bin/go test ./cmd/wagyu -run 'TestClipToScreenRowsPrefixMatches|TestComposedCacheMatchesColdRender|TestWarmFrame'
/Users/gear/go/bin/go test ./cmd/wagyu -run '^$' -bench 'ViewWarm|Keystroke' -benchmem

# full surface
/Users/gear/go/bin/go test ./cmd/... ./internal/...
```

Files touched:

| File | Change |
|---|---|
| `cmd/wagyu/tui_viewport.go` | `wrapSlot`, `composedRows`, `clipToScreenRowsPrefix`, binary-search sticky bar; `transcriptWindow`/`wrappedResponse` rewired |
| `cmd/wagyu/tui_run.go` | `runMode` fields: `rowsCache`/`rowsCacheN1` are `wrapSlot`, plus `rowsGen` and `rowsView composedRows` |
| `cmd/wagyu/tui_hotpath_bench_test.go` | 4 tests + 4 benchmarks (new) |
| `cmd/hpgen/main.go` | synthetic store generator (new, throwaway measurement tool) |
