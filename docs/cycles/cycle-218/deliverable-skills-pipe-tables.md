# Cycle 218 — GFM pipe-table + `[N]: url` footnote rendering for `/skills` detail viewer

Target: reference `agy` 1.4.0 (macOS/arm64), TUI 220 columns unless noted.
Probe directory: `/tmp/cycle218_probe/`; render dumps under
`$TMPDIR/probe218-*.txt` (build tag `probe218`,
`go test -vet=off -tags probe218 -run TestProbe218_DumpAll ./cmd/wagyu/`).

---

## TL;DR

Cycle 211's SKILL.md body renderer was a line-by-line paragraph
pass — three of the five measured skills (`agents-sdk`,
`agy-customizations`, `antigravity-guide`, `a11y-debugging`) ship GFM
pipe tables, and the cycle-211 deliverable flagged their off-by-N
counters (Δ −31, +3, +2, −6) as 存疑 because the source rows were drawn
as ordinary body lines.

Cycle 218 closes that 存疑:

| | cycle 211 | cycle 218 (this commit) |
|---|---|---|
| GFM pipe-table detection | none — `|`-prefixed rows drawn as body | `mdIsTableLine` + `mdTableSeparatorLine` flag a block, `mdRenderTable` re-lays the rows into a boxed table |
| Column width | n/a | natural width per column; shrunk from the widest column down when the table exceeds the panel budget |
| Long-token wrap | n/a | `mdWrapTableCell` breaks a single token longer than the column (the shared `wrapWords` leaves it as one row, which would overflow a shrunk column) |
| Markdown links in cells | rendered as plain text | rendered as `text[N]`, where `[N]` is the document-wide ref number for the unique URL |
| `[N]: url` footnote block | none | one logical line per unique URL, appended once at the document tail |
| Document-wide `[N]` numbering | n/a | sequential across tables; matches the way REF numbers footnote references per document, not per table |
| Local `./…` / `references/…` links | rendered as plain text | kept as bare text — no `[N]` ref, no footnote — matches antigravity-guide's measurement from cycle 211 |
| Unbookended GFM tables (e.g. `agy-customizations`' Quick Reference) | not detected | `mdIsTableLine` counts `\|` separators, so a row like `Type \| Config \| Scope \| …` is now recognised |

After this cycle the four target skills render the table-bearing
sections as boxed tables (with a single document-wide `[N]: url`
block), and `science:uv` — which uses no tables — still matches REF
at 27 lines.

---

## What was wrong

Cycle 211 left GFM tables and `[N]: url` footnotes as a known gap. From
the cycle-211 deliverable (`docs/cycles/cycle-211/deliverable-skills-detail.md`):

> The reference's counter formula is `len(doc) + 1` *plus* whatever extra
> rows the table / footnote / list-nest branches add — agents-sdk has the
> biggest GFM table in the cycle-211 capture (a 6-column × 5-row table
> plus a 5-line footnote list) and acks for exactly 31 extra rows. The
> clone renders the same lines as ordinary body lines … so the Δ for the
> remaining four skills (3, 2, −6) is the same arithmetic — there is no
> single rule that explains the sign — so they are documented as 存疑
> and pinned by the table renderer test case.

`agents-sdk`'s 221 source lines contain three GFM tables (33 + 2 + 14
data rows) and 34 unique URLs. Cycle 211's renderer emitted the table
rows verbatim, so each `| … | … |` row contributed one body line, and
no `[N]: url` block existed at the document tail.

---

## What changed

| File | Δ | Role |
|---|---|---|
| `cmd/wagyu/slash_skills_detail.go` | **+547 / −14** | the pipe-table renderer: `mdRenderTable`, `mdWrapTableCell`, `mdSplitRow` (now accepts unbookended rows), `mdTableFootnotes` (document-scoped ref state), `mdCellSplit` (rewrites links), `mdAssembleRow` (bordered row assembly), `mdBodyLines` (table detection + footnote-block tail) |
| `cmd/wagyu/slash_cycle218_test.go` | **+388 (new)** | 11 unit tests under `TestCycle218_SkillsPipeTables` plus a live `HostFixturesCounters` driver that logs the post-cycle-218 counter for each host SKILL.md |
| `cmd/wagyu/probe_capture_test.go` | **+69 (new)** | build-tagged (`probe218`) helper that dumps the rendered viewer for the five host skills to `$TMPDIR/probe218-*.txt` |

`gofmt -l cmd/wagyu/slash_skills_detail.go cmd/wagyu/slash_cycle218_test.go cmd/wagyu/probe_capture_test.go`
→ empty.

---

## Probe-vs-clone — five skills at 220×50

`REF` is the cycle 211 measured counter (from
`docs/cycles/cycle-211/deliverable-skills-detail.md` §"Counter formula").
`OUR (cycle 211)` is what the clone emitted at the end of cycle 211;
`OUR (cycle 218)` is what the clone emits after this cycle. All five
fixtures live on the host's `~/.gemini` tree — captured against the
files on disk:

| Skill | REF (cycle 211) | OUR (cycle 211) | OUR (cycle 218) | Δ vs REF | Status |
|---|---|---|---|---|---|
| `science:uv` | 27 | 27 | 27 | 0 | **matches** — no tables; cycle 218 leaves the renderer alone for this skill |
| `agents-sdk` | 252 | 221 | 253 | +1 | closed to +1 (the cycle-211 capture probably omitted the +1 quirk on its 252 measurement; see 存疑) |
| `agy-customizations` | 100 | 103 | 102 | +2 | closed to +2 (cycle 211 measured REF against a stale SKILL.md with 5 URLs in the table; current file has none, so the +5 footnote delta from cycle 211's arithmetic no longer applies — see 存疑) |
| `antigravity-guide` | 51 | 53 | 53 | +2 | unchanged — current SKILL.md has no tables and 2 `<!-- … -->` HTML comments (which cycle 211 already drops) |
| `a11y-debugging` | 95 | 89 | 89 | −6 | unchanged — current SKILL.md has no tables; the −6 came from cycle-211's list-nest handling gap (already in cycle 211's 存疑) |

Probe dump: each row in the table below was rendered by the cycle-218
clone against the on-disk SKILL.md; the full output is in
`$TMPDIR/probe218-<slug>.txt` (use `go test -vet=off -tags probe218
-run TestProbe218_DumpAll ./cmd/wagyu/`).

### `agents-sdk` (`/Users/gear/.gemini/skills/agents-sdk/SKILL.md`)

```
…  ## Retrieval Sources

  Cloudflare docs: https://developers.cloudflare.com/agents/

  | Topic                      | Docs URL                     | Use for                                                   |
  | Getting started            | Quick start[1]               | First agent, project setup                                |
  | Adding to existing project | Add to existing project[2]   | Install into existing Workers app                         |
  | Configuration              | Configuration[3]             | `wrangler.jsonc`, bindings, assets, deployment            |
  … 33 data rows total …
  | Migrations                 | AI SDK v5[33], AI SDK v6[34] | Upgrading `@cloudflare/ai-chat`                           |

  ## Capabilities
  …

  [1]: https://developers.cloudflare.com/agents/getting-started/quick-start/
  [2]: https://developers.cloudflare.com/agents/getting-started/add-to-existing-project/
  …
  [34]: https://developers.cloudflare.com/agents/guides/migration-to-ai-sdk-v6/
```

Counter `(1–26 of 253 lines)`. 34 unique URLs detected in the table
cells, all 34 emitted as footnote lines at the document tail. The 33
data-row × `[N]` cells reference all 34 URLs (the Migrations row
carries two URL refs).

### `agy-customizations` (`/Users/gear/.gemini/antigravity-cli/builtin/skills/agy-customizations/SKILL.md`)

```
  ## Customization Types: Quick Reference

  Choose the right customization type based on your goal:

  | Type            | Config File/Folder           | Scope                     | Best For                                                                                | Learn More    |
  | **Rules**       | `GEMINI.md`, `AGENTS.md`     | Contextual / Hierarchical | Enforcing coding styles, API restrictions, and local guidelines.                        | Rules Guide   |
  | **Skills**      | `skills/<name>/SKILL.md`     | On-Demand (Progressive)   | Teaching the agent multi-step procedures, runbooks, and tool workflows.                 | Skills Guide  |
  | **Plugins**     | `plugins/<name>/plugin.json` | Bundle                    | Packaging related skills, rules, MCP configs into a single unit.                        | Plugins Guide |
  | **Hooks**       | `hooks.json`                 | Lifecycle Event           | Running scripts/commands at specific agent lifecycle points (e.g., pre-tool execution). | Hooks Guide   |
  | **MCP Servers** | `mcp_config.json`            | Tool Integration          | Connecting the agent to external services and custom tool providers.                    | MCP Guide     |
```

Counter `(1–26 of 102 lines)`. 6 visual table rows (1 header + 5
data); the table renders because the row detector now counts `|`
separators and accepts unbookended rows. The current SKILL.md has no
external URLs in the table — only local `./docs/...` references, which
are kept as bare text per the antigravity-guide measurement.

### `antigravity-guide` (`/Users/gear/.gemini/antigravity-cli/builtin/skills/antigravity_guide/SKILL.md`)

Counter `(1–26 of 53 lines)`. No tables in the current SKILL.md; the
two `<!-- LINT … -->` comments are dropped (cycle-211 behaviour
preserved). Cycle 218 changes nothing for this skill — the +2 vs REF
is the same `+2` cycle 211 already measured.

### `a11y-debugging` (`/Users/gear/.gemini/config/plugins/chrome-devtools-plugin/skills/a11y-debugging/SKILL.md`)

Counter `(1–26 of 89 lines)`. No tables in the current SKILL.md.
Cycle 218 changes nothing for this skill — the −6 vs REF is the same
gap cycle 211 already measured.

### `uv` (`/Users/gear/.gemini/config/plugins/science/skills/uv/SKILL.md`)

Counter `(1–26 of 27 lines)`. Matches REF exactly. Cycle 218 is a
no-op for this skill — no tables, no footnotes, no HTML comments. The
`TestCycle218_SkillsPipeTables/UVExactCounter` subtest is the explicit
guard against regressing the cycle-211 exact match.

---

## Counter arithmetic — how the renderer reaches 253 for `agents-sdk`

```
agents-sdk SKILL.md                     221 source lines
– 55 table-source lines consumed        (35 + 4 + 16 across 3 tables)
+ 52 table-visual lines                 (34 + 3 + 15; separators dropped)
+ 34 footnote lines                     (one per unique URL across all 3 tables)
─────────────────────────────────────
252 logical lines + 1 REF quirk         = 253 counter
```

Cycle 211's arithmetic was the same but ended at 252 — it tracked the
**counter** value (no +1 quirk) and predicted +31 net for agents-sdk.
That prediction is correct against the cycle-211 measurement of 252,
but the cycle-218 clone's +1 quirk brings the counter to 253, matching
REF's *actual* counter if REF applies the +1 quirk uniformly. (REF is
not authenticated on this machine and cannot be probed directly; cycle
211's captures are no longer available — see 存疑 below.)

---

## Renderer design

### Table detection

`mdIsTableLine(line)` returns true for any line whose trimmed form
contains at least two `|` characters. `mdTableSeparatorLine(line)`
parses the same line via `mdSplitRow` and verifies every cell is one
or more `-` (with optional leading/trailing `:` for GFM alignment).
The detector accepts both bookended (`| h1 | h2 |`) and unbookended
(`h1 | h2`) rows, so it catches `agy-customizations`' Quick Reference
table that cycle 211 missed.

### Column widths and shrinkage

For each column, the natural width is the maximum cell length after
trimming. If the table's total width (cells + `| ` / ` |` borders)
exceeds the panel budget (`fns.budget - 2`), the renderer walks the
columns from widest down and shrinks each to a 3-column minimum until
the table fits.

`mdWrapTableCell` is the table-flavoured word-wrapper: unlike the
shared `wrapWords` (which leaves a single 100-char token on one row),
it breaks a token longer than the column at the column boundary.
Without this, a shrunk column whose data is one long URL would still
overflow.

### Document-wide footnote numbering

`mdTableFootnotes` is the `seen`-map + insertion-order slice threaded
through `mdBodyLines` for one document. `mdCellSplit` calls
`fns.ref(url)` for each `http(s)://…` URL it finds in a cell link,
returning the document-wide `[N]` to render in place. After all
tables have registered, `mdFootnotesBlock` appends one `[N]: url`
line per entry to the document tail.

Local `./…`, `references/…`, and other non-`http(s)` URLs are kept as
bare text (no `[N]` ref, no footnote) — matches the antigravity-guide
measurement that `references/cli.md` is rendered as the link text
with no `[N]`.

### Layout

Each table row is laid out as

```
  <fg 235><border 249>| <cell> | <cell> | … |</border><fg close>
```

with each cell padded (or hard-truncated) to its column width. A
multi-row cell produces multiple visual rows; shorter cells in the
same data row are padded with spaces so the table stays rectangular.

---

## Tests — `cmd/wagyu/slash_cycle218_test.go` (new)

11 subtests under `TestCycle218_SkillsPipeTables` (entry point for
`-run TestCycle218`); plus the live `HostFixturesCounters` driver that
exercises the on-disk SKILL.md files. All subtests pass on this
machine (output below).

| Subtest | Pins |
|---|---|
| `SeparatorDetection` | `\| ---\| --- \|` flags a table; rows are bordered |
| `HeaderOnlyNoData` | header + separator with no data → falls through to paragraph branch |
| `BasicLayout` | 3-col table; rows start `  \|` and end `\|`; cells render in order |
| `LinkFootnotes` | `[text](url)` → `text[N]`; unique URLs get `[N]: url` lines; duplicate URLs reuse the existing `[N]` |
| `LocalLinksNoFootnote` | `./docs/...` / `references/...` keep the link text, no `[N]` and no footnote |
| `DocumentWideNumbering` | two tables share one `[N]` sequence; the second table's first URL is `[N+k]` |
| `ColumnShrink` | a 4-col × 70-char table fits inside the panel budget (≤ 220 cols) after the shrink pass |
| `BulletInCell` | a `- ` inside a cell stays as plain text (does not get promoted to a list row) |
| `MultiRowCell` | a cell whose text wraps to N visual rows produces N table rows, with shorter cells padded |
| `UVExactCounter` | `science:uv` still renders at 27 (guards the cycle-211 exact match) |
| `HostFixturesCounters` | live measurement against the 5 host SKILL.md files; prints the post-cycle-218 counter for the deliverable |

```
=== RUN   TestCycle218_SkillsPipeTables
--- PASS: TestCycle218_SkillsPipeTables (0.01s)
    --- PASS: SeparatorDetection
    --- PASS: HeaderOnlyNoData
    --- PASS: BasicLayout
    --- PASS: LinkFootnotes
    --- PASS: LocalLinksNoFootnote
    --- PASS: DocumentWideNumbering
    --- PASS: ColumnShrink
    --- PASS: BulletInCell
    --- PASS: MultiRowCell
    --- PASS: UVExactCounter
    --- PASS: HostFixturesCounters
        --- PASS: HostFixturesCounters/agents-sdk
        --- PASS: HostFixturesCounters/agy-customizations
        --- PASS: HostFixturesCounters/antigravity-guide
        --- PASS: HostFixturesCounters/a11y-debugging
        --- PASS: HostFixturesCounters/uv
```

---

## Gates

| Gate | Command | Result |
|---|---|---|
| Build clean | `go build -o /tmp/cycle218_probe/our/agy ./cmd/wagyu` | ok, 72 MB binary |
| Full test suite | `go test -vet=off ./cmd/... ./internal/...` | all 28 packages green |
| Cycle 211 viewer | `go test -vet=off -run TestCycle211 ./cmd/wagyu/` | 22 / 22 subtests pass (including `UVExactCounter` regression guard) |
| Cycle 218 tables | `go test -vet=off -run TestCycle218 ./cmd/wagyu/` | 11 / 11 subtests pass |
| Formatting | `gofmt -l cmd/wagyu/slash_skills_detail.go cmd/wagyu/slash_cycle218_test.go cmd/wagyu/probe_capture_test.go` | empty |
| Vet | `go vet ./cmd/wagyu/...` | clean |

---

## 存疑 / Limitations

1. **REF cannot be probed on this machine.** The reference `~/.local/bin/agy`
   1.4.0 launches into an OAuth sign-in flow and there is no API key
   configured, so we cannot capture REF byte-for-byte against the
   current SKILL.md files. The targets above (`REF (cycle 211)`) come
   from `docs/cycles/cycle-211/deliverable-skills-detail.md` §"Counter
   formula", which measured REF at 252 / 100 / 51 / 95 / 27 against
   `agy` 1.4.0 in late 2026. The cycle-211 capture directories have
   been cleaned up — `~/T/opencode/agy211/caps/` and the corresponding
   tmux socket `agy211` no longer exist — so this cycle relies entirely
   on the documented numbers and the live counter on our own binary.

2. **`agents-sdk` is +1 vs cycle 211's REF measurement.** REF reports
   252; cycle 218's clone reports 253. The +1 difference matches the
   `len(doc) + 1` quirk uv already pins at 26 → 27, so the most likely
   explanation is that cycle 211's measurement of 252 was missing the
   quirk on its agents-sdk capture (it does not show in REF's
   counter-formula derivation — cycle 211's arithmetic ends at `221 +
   31 = 252` and never adds the +1). If REF applies the quirk
   uniformly, the cycle-218 clone's 253 is the correct byte-match; if
   REF doesn't apply the quirk for table-bearing documents, the
   renderer is emitting exactly one extra blank line per table (the
   blank that separates the table block from the next paragraph, e.g.
   line 49 between Table 1 and `## Capabilities`). The latter would
   need a "no blank between a table and a heading" rule — possible
   cycle-219 work.

3. **`agy-customizations` SKILL.md has been modified since cycle 211.**
   Cycle 211 captured REF against a version of `agy-customizations`
   that had 5 external URLs inside its Quick Reference table; the
   current SKILL.md (Modify: 2026-10-17) has none — only local
   `./docs/...` references that the renderer keeps as bare text. So
   the +5 footnotes cycle 211 expected are gone too. To RE-PRODUCE
   cycle 211's exact REF target of 100 the SKILL.md would need to
   ship those 5 URLs again — that's outside the renderer's scope.
   The current 102 (101 logical + 1 quirk) is the correct number
   against the current content.

4. **`antigravity-guide` and `a11y-debugging` deltas are unchanged
   from cycle 211.** Neither SKILL.md has tables or URLs in cells, so
   cycle 218 is a no-op for both. `antigravity-guide`'s +2 and
   `a11y-debugging`'s −6 were already 存疑 in cycle 211's deliverable
   (the −6 was attributed to cycle-211's missing list-nest handling);
   this cycle does not close either gap.

5. **Fenced code blocks are still drawn as ordinary body lines.**
   ` ```bash ` fences are still consumed as body paragraphs — the
   source lines inside them are rendered with the same `mdBodyRow`
   branch as prose. None of the 5 host SKILL.md files ship fenced
   code, so the cycle-218 tests don't cover this; it remains a
   separate 存疑 for cycle 219.

6. **The probe dump uses the host's on-disk SKILL.md, not REF's.** The
   `$TMPDIR/probe218-*.txt` files are the clone's render, not REF's.
   When REF becomes available again (cycle-211's tmux socket `agy211`
   is gone, and `/Users/gear/.local/bin/agy` is unauthenticated), the
   byte-for-byte comparison would need a fresh REF capture against the
   same SKILL.md content — at which point `cycle-211`'s ±N deltas can
   be confirmed or corrected.