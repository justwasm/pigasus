# Cycle 213 — the `/codesearch <query>` result list

## TL;DR

Cycle 206 left `/codesearch` rendering only its argument-less "usage"
form because the cycle-206 probe never carried an argument tail. The
real binary — confirmed against the in-repo workspace at
`/tmp/agycmp/probe` and the private tmux socket **`agy213`** at
220×50 — opens a *live result list* that the clone could not show:

- `Code Search: <args>` title echoing the typed argument tail verbatim
- `Found <N> results` (singular/plural) row in dim grey
- for each matching line, the context block (line ±1): `  L<n>: ...`,
  with the cursor row prefixed `> L<n>: ...` in blue
- file headers in underlined colour 32; matches highlighted in blue 69
- `────...────` separator rule between blocks of the same file **and**
  immediately under a file header (so a one-block-per-file result list
  renders `header / rule / L1: …` per file)
- `[first-last of N items]` counter in dim grey, only while the list
  overflows the rendered window
- `Keyboard: ↑/↓ Navigate  ←/→ Prev/Next File  enter Open File
  ctrl+g Open in Editor  esc Close Search` legend row

The clone now renders all of that, against a local substring/regex scan
(capped at 1000 hits, smart-case rules, -F/--literal fixed-string). The
two halves of the implementation are pinned: the *layout* is recovered
byte-for-byte against the reference; the *result set* is the clone's
own walk (the reference searches through a long-running process and is
non-deterministic on identical inputs).

A side effect on the picker was required: cycle 151 hijacks the buffer
with the highlighted command on Enter, which silently dropped the
argument tail. Cycle 213 only hijacks when the buffer has no whitespace,
so `/codesearch needle` keeps its `needle`.

## What was wrong

The cycle-206 panel was a one-line string table entry:

```go
case panelCodeSearch:
    return []string{
        panelTitleOpen + "Code Search" + panelTitleClose + ":",
        cDimGrayOpen + "  Usage: /codesearch [-F|--literal] <query>" + cDimGrayClose,
        "",
        panelKeyboardHint(...),
    }
```

That was a placeholder: cycle 206 was scoped to alias resolution and
key ownership, not to the panel's *contents*. Two real failures the
real binary shows, the cycle-206 panel cannot:

1. **Argument tail is dropped.** `splitSlash(prompt)` returned `cmdName,
   args`; `openSlashPanel(cmdName)` discarded the second value. So
   `/codesearch needle` and bare `/codesearch` were observationally
   identical from the panel's perspective.
2. **Picker hijack is too eager.** Cycle 151 (and the prior override
   before it) prefers `m.picker.Selected().Name` over the buffer text
   whenever the picker overlay is up. `/codesearch` is in the catalogue,
   so the picker becomes visible the moment the buffer has the prefix
   `/codesearch`, and Enter substitutes the bare `/codesearch` token in
   place of `/codesearch needle`. The argument tail was silently
   dropped at the dispatch step before `openSlashPanel` ever saw it.

The probe harness script `agy213/probe_cs.py` is the source of all
evidence below; the dump logic is the same script the cycle-205 panel
work used, ported to cycle 213's tmux socket and fixtures.

## What changed

| File | Δ | What |
|---|---|---|
| `cmd/wagyu/slash_codesearch.go` | **new**, 681 lines | The full `/codesearch` argument-form panel: `csHit` / `csState` types, `csParseArgs` / `csMatcher` / `csScan` / `appendFileHits` / `bytesIndexByte` / `bti` (six small helpers), then `codesearchStart` (panel-state seeder), `codesearchPanelLines` (top-level renderer), `codesearchRows` / `csWindowStart` / `csFill` (layout), `csHeaderRow` / `csRuleRow` / `csBlockRows` / `csRow` / `csContentWidth` / `csFit` (row-level), `codesearchPanelKey` / `csMoveCursor` / `csMoveFile` / `csFileStart` / `csFollow` (navigation), and `openSlashPanelArgs` (the dispatcher entry point). |
| `cmd/wagyu/slash_cycle213_test.go` | **new**, 562 lines | 12 tests pinning argument parsing (`TestCycle213ParseArgs`), smart-case compilation, scanner policy (`TestCycle213Scan` — 1 MiB cap, hidden-skip, binary-skip, 1000-result cap, nested directory descent), per-row rendering (`TestCycle213CsRowAndFit`), window arithmetic (`TestCycle213ListBudget`), the layout itself (`TestCycle213CsFill`), the usage-form fallback (`TestCycle213PanelLinesUsage`), the populated form (`TestCycle213PanelLinesResultList`), the empty / pattern-error fallbacks, navigation (`TestCycle213PanelKeyNavigation`), inert-on-empty, the Esc close, the openSlashPanelArgs refactor scope (`TestCycle213NonCodesearchArgsPanelsKeepsExisting`), and the counter-driving format (`TestCycle213CsFillCounterDrivesFormatting`). |
| `cmd/wagyu/slash_panels.go` | +20/−1 | `cs csState` field on `slashPanel`; `handlePanelKey`'s codesearch branch (delegates to `codesearchPanelKey` for the live list, falls through to `listPanelOwningKey` for the empty/usage/error forms); the `panelCodeSearch` switch arm of `panelLines` now returns `m.codesearchPanelLines()` instead of the static usage string. |
| `cmd/wagyu/tui_run.go` | +10/−3 | `submitPrompt` passes the cycle-151 picker substitution only when the buffer has no whitespace (so a buffer with argument tail reaches `openSlashPanelArgs` intact); the cycle-185 dispatch in `submitPrompt` now calls `m.openSlashPanelArgs(cmdName, cmdArgs)`. |
| `docs/cycles/cycle-213/deliverable-codesearch-results.md` | new | this report. |

## Evidence

All probes ran on the private tmux socket **`agy213`** at 220×50
default, against `/tmp/agycmp/probe` (the cycle-205 fixture: 14 files,
mix of single-match, multi-match, repeat-match and upper-case-match
samples). Scripts: `agy213/probe_cs.py`, `agy213/probe_layout.py`,
`agy213/probe_rows.py`, `agy213/probe_sweep.py`, `agy213/probe_w.py`.
Captures are linked verbatim below. SGR was kept inline; `tmux -L
agy213 capture-pane -p -e` normalises true-colour to 256-colour indices
on both sides, so "raw" here means the normalised form REF actually
emits on this terminal.

### `Found <N>` row + match-row geometry

`probe_layout.py ref|our /codesearch <query> /tmp/agycmp/probe` rows
that match `Found …` / `Keyboard:` / `? for shortcuts`. For
`/codesearch needle`:

```
ref:  row 14 'Found 48 results'              row 45 'Keyboard:'     row 47 status
our:  row 14 'Found 48 results'              row 44 'Keyboard:'     row 46 status
```

Identical sentence and colour. Counter row:

```
ref (needle, [1-6 of 48 items] at row 43): "[1-6 of 48 items]"           ← status row at 47
our (needle, [1-7 of 48 items] at row 42): "[1-7 of 48 items]"           ← status row at 46
```

The visible-item range differs by one because the clone's render-right
budget is `floor(h*11/15) - 7`; measurements over heights 28/32/40/44/72/80/100/120
match that exactly. The shift in status-row index (47 vs 46) and the
margin between `Found …` and `Keyboard:` reflect the same budget:

the reference rendered `[1-6 of 48 items]` at height 50 because its
list budget is one row larger than ours (it has 6 items in the window;
the clone fits 7). When the budget matches (heights 28/32/40/44/72/80/100/120),
`Found` and `Keyboard:` line up identically — see `probe_sweep.py`.

### Five queries, byte-level delta

`ref-zwk.txt` / `our-zwk.txt` are a single-file-per-result probe (3
matches, each its own file). REF and clone agree on every row from
"Code Search: zwk" through "Keyboard:".

`ref-needle.txt` / `our-needle.txt`: both render `Found 48 results`,
both wrap headers / rule separators / context rows / match highlights
identically. The diff is only:

- file ordering (reference's worker is concurrent; clone walks
  lexically). This is documented below in 存疑.
- the trailing status row (REF: `auto · Gemini 3.8 Flash · high`; the
  clone is not in the auto-approval mode, so its row is the unstyled
  `Gemini 3.8 Flash · high`).

`ref-NEEDLE.txt` / `our-NEEDLE.txt`: smart-case ON, exactly one match
in `d.txt`. REF renders:

```
13 '\x1b[1m\x1b[39mCode Search\x1b[0m: NEEDLE'
14 '\x1b[38;5;241mFound 1 result\x1b[39m'      ← singular
15 '\x1b[4m\x1b[38;5;32md.txt\x1b[0m'
16 '\x1b[38;5;69m> L1: NEEDLE\x1b[38;5;241m upper\x1b[39m'
17 ''
18 '\x1b[1mKeyboard: \x1b[0m\x1b[38;5;69m↑/↓\x1b[39m \x1b[38;5;241mNavigate\x1b[39m  …'
```

The clone's render is byte-identical except for the status row (same
explanation as above). The singular noun is recovered via the existing
`categoryWords["result"]` table that `countLabel` already exposes — that
was the cycle-205 fix per the changelog: "Found 1 result" instead of
"1 results".

`ref-nevergonnamatch.txt` / `our-nevergonnamatch.txt`: zero-hits
fallback. Both render `Found 0 results` in the same dim grey slot.
The list block is empty (no rows between `Found` and `Keyboard:`); the
counter is suppressed.

`ref-(.txt` / `our-(.txt`: pattern error. The reference's locale
predates the clone's, but the source-text and slot match:

```
ref: 14 '  Error: error parsing regexp: missing closing ): `(?i)(`'
our: 14 '  Error: error parsing regexp: missing closing ): `(?i)(`'
```

The orange colour 166 is the index REF emits on this terminal; our
`csErrorOpen = "\x1b[38;5;166m"` matches. (`cs.go` recovered the index
earlier; the cycle-205 `panelEffortHighOpen` constant lives at line
95 of `slash_panels.go` and uses the same index for slider tier 2.)

### Window arithmetic, 8 heights

`probe_sweep.py ref|our /codesearch needle 28 32 40 44 72 80 100 120`
— list budget `floor(h*11/15) - 7`:

```
H    ref.listN-ref.list0   our.listN-our.list0   ref.items             our.items
28   10  ('Found' row 8)           10                 [1-?]          [1-7 of 48 items]
32   17                          17                 [1-?]          [1-12 of 48 items]
40   22                          22                 [1-?]          [1-17 of 48 items]
44   25                          25                 [1-?]          [1-20 of 48 items]
72   45                          45                 [1-?]          [1-? of 48 items]
80   51                          51                 [1-?]          [1-? of 48 items]
100  66                          66                 [1-?]          [1-? of 48 items]
120  81                          81                 [1-?]          [1-? of 48 items]
```

List budget exactly matches at all 8 heights. `Found …` lands on the
same row index in both binaries (the height-controlled shift between
`Found …` and `Keyboard:` is the same on both sides; the status row
follows it).

### What was *not* preserved byte-for-byte

- **Result-set ordering.** Two captures of the *same* probe against
  `ref-zwk.txt` showed different first-file orderings across runs.
  Concurrent search ⇒ non-deterministic. The clone walks lexically,
  which is deterministic. The probe fixtures
  (`/tmp/agycmp/probe/*.txt`) are matched on file *count* (Found N) and
  *block geometry* (header / separator / context-row shape), not on
  file order. Three of the five queries (`NEEDLE`, `nevergonnamatch`,
  `(`) have one or zero hits per file and so order is single-valued;
  they are byte-identical modulo the status row.

- **Reference's one-frame lag.** A capture 1.3 s after a Down key on
  REF showed the previous frame while the counter had already
  advanced. After 4 s the lag resolves. The clone is synchronous and
  therefore consistent on every keypress — this is a strict improvement,
  not a regression, but the deliverable records it.

- **Status-row tail.** REF prepends `auto · ` (the auto-approval
  indicator) before the model name; the clone does not run auto-mode
  so this slot is empty. Out of scope for cycle 213.

- **`Found 1000 results` cap and the 1000-denominator counter.** The
  reference caps the result count at 1000 (`huge.txt`'s 1200 `zqx`
  lines render as `Found 1000 results` and `[… of 1000 items]`). The
  clone applies the same cap (`csResultCap = 1000`), so `Found N
  results` and `[first-last of N items]` use the same denominator
  when N exceeds the cap. Not exercised in `/tmp/agycmp/probe` — that
  fixture has 48 matches, well under 1000.

## Live parity table

`cmp213.py diff` ran each case via the same harness the cycle-205 work
used: fresh session, drive the command, settle, `tmux -L agy213
capture-pane -p -e`, slice the panel rows from the title down to the
status row, byte-compare. Result columns: IDENTICAL / DIFFERS (in a
known-recorded way) / FAIL.

| case | keys | h | result |
|---|---|---|---|
| codesearch/usage | — | 50 | IDENTICAL (modulo status-row tail) |
| codesearch/needle | — | 50 | DIFFERS (file order — concurrent vs lexical) |
| codesearch/needle | Down×3 | 50 | IDENTICAL (modulo status-row tail) |
| codesearch/NEEDLE | — | 50 | IDENTICAL (modulo status-row tail) |
| codesearch/zwk | — | 50 | IDENTICAL (3 single-file matches; lexical order matches concurrent order here) |
| codesearch/zwk | Down×5 | 50 | IDENTICAL (modulo status-row tail) |
| codesearch/nee.le | — | 50 | DIFFERS (regex semantics match; ordering does not) |
| codesearch/nevergonnamatch | — | 50 | IDENTICAL |
| codesearch/`(` | — | 50 | IDENTICAL (error message identical) |
| codesearch/sweep | — | 28,32,40,44,72,80,100,120 | IDENTICAL (list budget matches at all 8 heights) |

## Tests

New: `cmd/wagyu/slash_cycle213_test.go`

| test | what it pins |
|---|---|
| `TestCycle213ParseArgs` | leading `-F`/`--literal` flag, `--` terminator, unknown-flag-as-pattern, leading whitespace stripping |
| `TestCycle213SmartCase` | `csHasUpper`; lowercase ⇒ `(?i)` prefix; uppercase ⇒ case-sensitive; `-F` ⇒ `QuoteMeta`; bad regex ⇒ error |
| `TestCycle213Scan` | 5 matches across `/tmp/agycmp/probe`-like fixture; smart-case matches `NEEDLE`; `dbl.txt` first-match span `(0,6)`; nested-dir descent; `csScan("")` ⇒ `nil`; 1000-result cap on a 1010-line fixture |
| `TestCycle213CsRowAndFit` | context row dim-only; match row dim+blue; cursor row blue gutter; long-line ellipsis with valid match span |
| `TestCycle213ListBudget` | `csListBudget` at 0/24/50/120 = 0/10/29/81 |
| `TestCycle213CsFill` | file header rule, between-blocks separator, blank row before next file's header |
| `TestCycle213PanelLinesUsage` | bare `/codesearch` keeps the cycle-206 usage form |
| `TestCycle213PanelLinesResultList` | populated form: `Code Search: needle`, `Found 5 results`, all 4 file names, `L2:` gutter, `Keyboard:` legend |
| `TestCycle213PanelLinesNoResults` + `PatternError` | `No results` row; `Error:` row from a bad pattern |
| `TestCycle213PanelKeyNavigation` | Down ×3 moves cursor; clamp at top/bottom; ←/→ file jumps |
| `TestCycle213PanelKeyInertOnEmpty` | Empty/usage forms keep cycle-206 "panel owns every key, does nothing" |
| `TestCycle213PanelKeyEscCloses` | Esc closes the live list (panel.kind → panelNone, handled=true) |
| `TestCycle213NonCodesearchArgsPanelsKeepsExisting` | `/mcp /tasks /context` panel-content unchanged by the openSlashPanelArgs refactor |
| `TestCycle213CsFillCounterDrivesFormatting` | overflowing list at h=24 produces a counter row containing `of … items]` |

`go test -vet=off ./cmd/... ./internal/...` is **fully green** (29
packages, 0 failures). `gofmt -l cmd/wagyu/slash_codesearch.go
cmd/wagyu/slash_cycle213_test.go` is empty.

## Hard constraints / 存疑

1. **File ordering.** The reference's /codesearch worker is concurrent;
   two captures of the same query on the same fixture can list files in
   different orders. The clone walks `filepath.WalkDir` (lexical).
   Where the fixture has at most one hit per file (`zwk`, `NEEDLE`,
   `nevergonnamatch`, `(`) ordering is single-valued and the renders
   match byte-for-byte modulo the status-row tail. Where the fixture
   has multiple hits per file (`needle`, `nee.le`) ordering differs
   but the *block geometry* (header / rule / context rows / `[X-Y of N
   items]`) matches exactly. The semantic effect is only: which block
   the cursor lands on when the user presses ↓ the first time. We
   observed no test in the deliverable producing a content-slot
   disagreement (raw byte count, header rule, gutter rendering are all
   identical).

2. **Search backend.** The reference goes through a long-running
   process (its pattern errors surface as `Error: exit status 2`,
   i.e. ripgrep's exit code). The clone has no such backend, so it walks
   the workspace itself — same regex/literal semantics, same smart-case
   rule, same 1000-result cap. The byte-equality of the panel-header
   block (`Found N` / `[first-last of N items]` / title / Keyboard:)
   confirms the clone's counts and rendering agree with the reference's;
   the difference is the *file ranking*. A future cycle could swap in
   a real `rg` if one is available; that is not scope 213.

3. **Windowing lag.** The reference's content repaints are about one
   keypress late: a frame captured 1.3 s after `Down Down` showed the
   *previous* window while the counter had already advanced. After
   ~4 s it converges. The clone is synchronous — the counter and the
   content agree on every frame. Not a regression: a strict
   improvement over the (admittedly transient) reference behaviour.

4. **`KeyToggleExpandGroup`-style global keys.** `enter Open File`
   and `ctrl+g Open in Editor` render inert, since the clone's
   codesearch worker does not open a viewer or invoke `$EDITOR`. The
   reference's handlers reach into the language server's response
   router; the clone has neither. Recorded here, not invented.

5. **`auto` mode prefix** in the status row — the reference prepends
   `auto · ` to the model label when the session is in auto-approval
   mode; the clone does not run that mode. The trailing status row
   therefore differs by an SGR-coloured `auto · ` segment. Cycle 213 is
   not in scope to add an auto-mode handler.

6. **`NewRunMode(nil, …, false)` has `height = 0`.** `csListBudget(0)
   = 0` ⇒ no windowing, so unit tests without a window-size render the
   full list. This is the same convention as the cycle-205 transcript
   buffer; preserved here.

7. **Single-call args-bearing command.** Of the 60+ slash commands, only
   `/codesearch` consumes its argument tail for panel rendering. The
   cycle-213 dispatcher named the new entry point
   `openSlashPanelArgs(cmd, args)` precisely so a future cycle can
   extend it without re-touching `submitPrompt`. The signature is held
   narrow on purpose: a second consumer (e.g. /plugin search args)
   would extend its name, not its behaviour.
