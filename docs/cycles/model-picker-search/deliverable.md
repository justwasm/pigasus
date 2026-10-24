# Cycle `model-picker-search` — a live search in `/model`, and the provider type that broke the wrapper

**Status**: implemented, `go test ./cmd/... ./internal/...` green (27 packages)
**Reference**: `~/.local/bin/agy` — Antigravity CLI **1.4.5**, driven live in tmux
**Evidence**: `captures.md` in this directory — the reference's and the clone's
byte-level captures, the queries that pinned the matching rule, and the
wrapper's 502

Three complaints, one cycle:

| complaint | cause | fix |
|---|---|---|
| `agy` + a MiniMax model hangs at *generating*, `⚠ Bad Gateway (code 502): provider: models: unknown provider "agw"` | the clone's own legacy flat configuration names a provider **type** (`agw`) that a rename removed from `models.NewProvider` | the legacy type name is read as the dialect it meant (`internal/models/provider.go`) |
| `wagyu` cannot select `MiniMax M3.1 Flash` | `panelRowCount` counted the picker's rows with a hard-coded `6` | the count follows the rows (`cmd/wagyu/slash_panel_routing.go`) |
| `agy`'s menu filters on what you type — `wagyu`'s does not | the `/model` picker had a *drawn* search box but no search | a live search field, a fuzzy filter and the reference's relevance order (`cmd/wagyu/slash_panel_model.go`) |

## 1. The legacy provider type 【一手】

The clone's flat configuration format (`~/.config/openagy/models.json`)
carries a `provider` field that is the **wire dialect** — and the clone's
own earlier versions wrote the *name* of the tenant into it:

```json
{"name": "minimax-m3.1-flash", "display_name": "MiniMax M3.1 Flash",
 "provider": "agw", "api_key": "-", "api_url": "https://agw.k0s.io/v1/chat/completions"}
```

The rename that made the provider section provider-centric (`d780f075`)
dropped `agw` from `NewProvider`'s switch. Every configuration the clone
had written itself then failed to load, and the wrapper — which answers
such a failure with `502` — left the TUI generating forever:

```
provider: models: unknown provider "agw"
```

The fix is a compatibility read, not a branding regression:

```go
// legacyProviderTypes maps the provider-type names that earlier versions
// of this clone wrote into ~/.config/openagy/models.json to the dialect
// they meant.
var legacyProviderTypes = map[string]string{
	// agw.k0s.io speaks the OpenAI chat-completions dialect.
	"agw": "custom",
}
```

`NewProvider` resolves the label through the table and keeps the original
label, so `Name()` still answers `agw` and the logs still name the
endpoint the configuration points at. The header behaviour of the removed
branch is preserved too: the legacy type sends `Authorization: Bearer
<key>` even when the credential is empty, the way the pre-rename code did.

The alias is **read-only** — nothing writes `agw` any more, and the
provider-centric section (`type: "openai" | "anthropic"`) is unaffected.

## 2. The picker's row count

```go
case panelModel:
	// The row list is the reference's five static rows plus the
	// conditional 5.5 rows plus the models the user configured, so
	// the count is not a constant …
	return len(m.modelRows())
```

The picker's rows are the reference's five static ones, plus the
conditional Claude 5.5 rows, plus the models the user configured. A
constant could not be right, and the constant that was there (6) made
every row past the sixth unreachable. The count now also follows the
search filter, so it shrinks with the query.

## 3. The search field

The reference's picker is a **search field first**: every printable
character lands in the box and the rows filter live. The clone drew the
box (`Search: ▮`) but ignored the keystrokes.

### What the reference does 【一手】

Captured live against agy 1.4.5 (nine queries — the full table is in
`captures.md` §2):

- the match is a **case-insensitive subsequence** of the **display
  label** — `clauson` matches `Claude Sonnet 4.6 (Thinking)`, and
  `cl-opus` (a subsequence of the catalogue id) matches nothing;
- a printable key **filters**; it does not navigate (`j` goes into the
  box, the cursor stays);
- the rows come back in a **relevance order** that depends on the query
  (`claude` → Opus first, `s` → Sonnet first);
- no match renders `  No models match "<query>"` in 256-colour 60, and
  the legend collapses to `esc Clear search`;
- Esc clears a live query and keeps the panel open; the legend's last
  entry reads `esc Clear search` while a query is live and `esc Go Back`
  when the box is empty.

### What the clone does now

`cmd/wagyu/slash_panel_model.go`:

- `modelSearchMatch` — the subsequence matcher, returning the two
  relevance signals (how many matches landed on a word boundary, where
  the first one was);
- `modelRowsFiltered` — the filter and the sort;
- `m.modelRows()` — the rows the panel is showing right now; every
  cursor-indexed helper (the renderer, the effort slider, the selection)
  goes through it, so the cursor always points at a row that is on screen;
- `panelModelLines` — the query in the box, the empty-result line, and
  the legend's `esc Clear search`.

`cmd/wagyu/slash_panel_keymap.go`: `handleModelKey` no longer uses
`tuilib.ClassifyListNavKey`, which would read `j`/`k`/`h`/`l`/`g`/`q` as
navigation. The panel handles the arrow keys, Enter, Esc and Backspace
explicitly and sends every other printable character to the box.

### The relevance order

The reference's score function was not recovered. A single rule
reproduces all nine measured queries:

1. a row the user **cannot select** (the dim `Gemini 4 Argon`) sorts
   **last**, even though its label is the shortest;
2. then the rows whose match lands on a **word boundary** — more is
   better;
3. then the **shorter label**;
4. then the earlier first match.

The rule is applied to the rows that are on screen, so the clone's extra
rows (the configured models) sort by the same rule.

## 4. One byte-level rendering fix that came with it

The cursor row is now padded to column 30 before the colour reset, the
way the reference pads it:

```
reference:  [38;5;32m> Claude Opus 4.6 (Thinking)  [39m
clone:      [38;5;32m> Claude Opus 4.6 (Thinking)  [39m     (before: no padding)
```

The clone only padded a row that was also the *current* model, so the
difference was invisible while the cursor sat on the Gemini row — and
visible as soon as it moved. With the search field, the cursor lands on
arbitrary rows, so this had to be fixed.

`modelRowPad` no longer forces a minimum of one space: a label that
already reaches column 30 is not padded at all (the reference's
`> Claude Sonnet 4.6 (Thinking)` has nothing after it).

## 5. Verification

- **Unit**: 10 new tests — the subsequence matcher, the label-only rule,
  the reference's order, the dim row last, typing filters, printable keys
  do not navigate, Backspace, Esc clears then closes, the empty-result
  line, the legend, the row count (and that the seventh row is
  reachable). Plus 4 in `internal/models` for the legacy type (accepted,
  case-insensitive, the bearer header, unknown types still rejected).
- **Live, clone vs reference**: the same query in both panes, captured
  with `tmux capture-pane -e -p`, diffed. The panel region is
  **byte-identical** except for one pre-existing trailing reset on the
  underline (see the limitations).
- **Live, end to end**: the clone's print path and the wrapper's print
  path both answer through the user's legacy `agw` configuration
  (`pong`), and the wrapper's TUI answers too.

## 硬约束

- **The provider type alias is a read, never a write.** `models.Editor`
  and the provider-centric configuration keep writing the generic names;
  only the legacy flat file's `provider` field is read leniently.
- **`agy --help` still lists twelve visible subcommands, `agy models` is
  still byte-pinned, the slash-command catalogue still holds 99
  entries.** Nothing user-visible was added to any of them.
- **The reference's `/model` list still wins.** The clone's picker shows
  the reference's five rows plus the configured ones — the search filters
  that list, it does not extend it.
- **The `Search:` box keeps its reverse-video cursor cell** and the
  20-cell underline; the query is inserted in front of the cell, exactly
  as the reference does it.
- **`/config`'s search was not touched.** Its field, its matcher (a
  plain substring match over the name, the description and the values)
  and its panel are unchanged.

## 存疑

- **The reference's relevance score was not recovered.** The rule in §3
  reproduces every query that was measured; a query with a different
  shape could order differently. The reference's own order is a *score*,
  and the clone's is an approximation of it.
- **The dim-row rule is an inference.** The reference keeps `Gemini 4
  Argon` last in every filtered list that matches it, but that could be
  "unavailable rows last" *or* an artefact of the reference's scoring.
  The clone implements the first reading.
- **The legacy type list is `agw` only.** It is the only name the
  clone's own earlier versions wrote (the old `Model.Provider` doc
  comment lists the generic names, and the old doc example shows
  `"provider": "agw"`). Another tenant name from an even earlier build
  would still be rejected.

## limitations

- The panel was exercised on one machine, on 2026-10-23, against agy
  1.4.5. The reference's catalogue is fetched over the network, so the
  reference's captures depend on the account's catalogue.
- One raw-byte difference remains in the `/model` panel: the clone emits
  a trailing `\x1b[39m` after the search underline, the reference does
  not. It is invisible on screen, and it is pre-existing (the constant is
  shared with `/config`, whose rows would inherit the separator colour
  if the reset were dropped). It was left alone rather than destabilise
  the `/config` submenu.
- The legacy `agw` alias was verified against the user's own endpoint
  (`https://agw.k0s.io/v1/chat/completions`, the `Bearer -` dev
  sentinel) and against a local test server. It was **not** verified
  against a second endpoint that speaks the same dialect.
- The wrapper's TUI was exercised with one short prompt. The proxy's
  error path for *other* provider types (a genuinely unknown type) still
  answers `502` with the same wording — that is intended.
- The clone's picker's search matches the *display label* only, exactly
  like the reference. The clone's configured rows carry a note
  ("configured model") that the reference has no equivalent of, and the
  note is **not** searchable — the reference's matching rule says the
  label only.
- No `internal/referencedrift` artifact was added: a live UI behaviour
  is not a version-scoped fixture, and the reference's own panel layout
  is re-captured by `repro/wg02_reference_model_picker.sh`.
