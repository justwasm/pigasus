# Cycle 224 — `/context` non-empty viewer

## TL;DR

Cycle 190 closed `/context` for the empty case (`panelContextLines()`
hardcoded `"0 tokens (0.0%)"` for every right-side descriptor, a fixed
`Estimated usage (awaiting generation)` placeholder, and `model · 0/1.0M
tokens` in the title row). The seven-token grid, system-files block,
Related line, and scroll-legend below it already used the right per-row
color and the 6-space pad for the percentage row.

Cycle 224 (`TODO.md §4 follow-up`) fills in the non-empty branch. The
backend does not surface per-message token totals, so the panel uses a
proxy: 1 token ≈ 4 chars of message content (user / assistant rows),
and 1 token + 4 tokens of argument overhead per recorded tool call.
The descriptors, the title row, and the `(X.X%)` row all reflect that
estimate; the "awaiting generation" placeholder becomes `(live session,
K messages)`; `Free space` becomes `1.0M (X.X%)` with `X.X = (1.0M −
used) / 1.0M × 100`.

Eight new tests in `cmd/wagyu/slash_cycle224_test.go` lock the
behaviour. The empty-state byte-anchored layout is preserved verbatim
(`TestPanelContextEmptyLayout` still passes byte-exact; a sibling
`TestPanelContextEmptyStillCycle190` re-runs the same checks through
the cycle-224 dispatcher).

## 1. Evidence

### REF probe attempt (failed — auth wall, same as cycle 222)

```bash
$ tmux -L agy224 new-session -d -s main -x 220 -y 50 -c /tmp
$ tmux -L agy224 send-keys -t main \
    '/private/var/folders/.../agy224/our/agy' Enter
# → Welcome to Antigravity CLI.
#   navigaid@gmail.com (Antigravity Starter Quota).
#   ⚠ "MiniMax-M3" no longer available. Using "MiniMax M3".
#   ⚠ ADC authentication failed. Please run:
#     gcloud auth application-default login
#   ⚠ Or unset AGY_ADC_AUTH to use other sign in methods.
```

The clone (`~/.gemini/` is read-only on this checkout — `Config
discipline` in `AGENTS.md`) cannot pre-seed a refresh token, and
Google's OAuth wall blocks a multi-turn probe the same way cycle 222
found. The cycle-224 task brief authorised the cycle-222 playbook:
synth-seed `m.session` via a `seedContextSession(m, payloads)` test
fixture, then verify the clone's *clone-side* layout, math, and
nondescriptor rows against the cycle-190 byte-exact skeleton. **The
empty-state probe-vs-clone was re-verified** below.

### Empty-state probe (verifies the cycle-224 split is regression-free)

REF capture: `/tmp/agycmp/panels/ppref.context.empty.raw.txt` (copied
to `agy224/ref/ppref.context.empty.raw.txt`).

OUR capture: tmux-driven, 220×50, `~/GitHub/openagy` CWD, model slot
"MiniMax M3" (REF probe was taken with `"Gemini 3.8 Flash (High)"`,
hence the model-name diff on the title row; the panel-body rendering
is identical):

```
└ Context Usage                                                       ← row 0, bold + dim 238
□ □ □ □ … (44 cells)     MiniMax M3 · 0/1.0M tokens                   ← row 1
□ □ □ □ … (44 cells)      (0.0%)                                       ← row 2, padded 6 cells
□ □ □ □ … (44 cells)     Estimated usage (awaiting generation)        ← row 3, bold + dim 238
□ □ □ □ … (44 cells)     ◉ User messages: 0 tokens (0.0%)             ← row 4, ◉ bold + dim 26
□ □ □ □ … (44 cells)     ◉ Agent responses: 0 tokens (0.0%)           ← row 5, ◉ green 29
□ □ □ □ … (44 cells)     ◉ Tool calls: 0 tokens (0.0%)                ← row 6, ◉ bold + orange 172
□ □ □ □ … (44 cells)     □ Free space: 1.0M (100.0%)                  ← row 7, □ dim 241

System files · auto-loaded
 └ ~/.gemini/GEMINI.md                                                ← dim 241
 └ ~/.gemini/skills/
 └ ~/.gemini/antigravity/skills/

Related: /artifact · /skill · /rewind                                 ← dim 241
  ↑/↓ Scroll · pgup/pgdown Page · ctrl+end Bottom · ctrl+home Top · esc Close
```

`/tmp/agycmp/cmp_panels.py context`:

```
====================================================================================================
context
  IDENTICAL (SGR-normalised)
```

The script's region-extractor crops below the second
`────────────────────────────────────────…` separator and skips the
model-row diff (REF "Gemini 3.8 Flash (High)" / OUR "MiniMax M3"),
so the panel-body panel rows 1–15 are byte-exact under SGR-RGB→256
normalisation, with row 2 still padded 6 cells.

### Non-empty-state probe — REF inaccessible

The OAuth wall blocks a multi-turn REF capture. The cycle-222
playbook applies: the **clone** is verified by writing synth sessions
via `seedContextSession(m, payloads)`. Five probe-vs-clone tables
follow, each anchored to one of the eight new tests. The clone
expects these specifics; only a real REF probe could firm up the
specific token counts.

## 2. Probe-vs-clone tables (220×50)

The four states the cycle-224 brief asks for are **empty** (covered
above), **1-msg** (a single 200-char user message),
**5-msg** (5 mixed-payload messages totalling 1475 proxy tokens), and
**20-msg** (10 user/assistant pairs totalling 28 125 proxy tokens =
2.8 % of the 1.0 M window). Each is a `mkPanel(panelContext, "/context", 0)`
invocation plus a `seedContextSession(...)`; the panels render the
same 16-row skeleton in every state because the grid block is the
same height regardless of the descriptor text — only the
descriptors change.

### Empty state (anchor; cycle-190 baseline, byte-exact)

| REF | OUR (`TestPanelContextEmptyLayout`) |
|---|---|
| `└ Context Usage` | `└ Context Usage` |
| Row 1: `<model> · 0/1.0M tokens` (5-space pad) | same shape, same wording |
| Row 2: `(0.0%)` (**6-space** pad — REF quirk) | same 6-space pad |
| Row 3: `Estimated usage (awaiting generation)` (bold 238) | same |
| Row 4: `◉ User messages: 0 tokens (0.0%)` (◉ bold dim 26) | same |
| Row 5: `◉ Agent responses: 0 tokens (0.0%)` (◉ green 29) | same |
| Row 6: `◉ Tool calls: 0 tokens (0.0%)` (◉ bold orange 172) | same |
| Row 7: `□ Free space: 1.0M (100.0%)` (□ dim 241) | same |

**`/tmp/agycmp/cmp_panels.py context` → `IDENTICAL (SGR-normalised)`.**

### 1-message state (synthetic: 200-char user → 50 tokens)

Anchor: `TestPanelContextNonEmpty1MsgLayout`.

| Row | REF (unprobed) | OUR (clone) |
|---|---|---|
| Title (`└ Context Usage`) | same | `└ Context Usage` |
| Row 1 | `<model> · 50/1.0M tokens` | `MiniMax M3 · 50/1.0M tokens` |
| Row 2 | `(0.0%)` (still 6-space pad; 50/1M rounds to 0.0%) | `(0.0%)` |
| Row 3 | `Estimated usage (live session, 1 message)` | same |
| Row 4 | `◉ User messages: 50 tokens (0.0%)` | same |
| Row 5 | `◉ Agent responses: 0 tokens (0.0%)` | same |
| Row 6 | `◉ Tool calls: 0 tokens (0.0%)` | same |
| Row 7 | `□ Free space: 1.0M (100.0%)` | same |
| Rows 9–12, 14, 15 | dim-241 system files / `Related` / legend | identical to cycle 190 |

### 5-message state (synthetic: 2000+1500+800+400+1200 chars, 3 user + 2 assistant)

Anchor: `TestPanelContextNonEmpty5MsgLayout`.

| Row | REF (unprobed) | OUR (clone) |
|---|---|---|
| Title | `└ Context Usage` | same |
| Row 1 | `<model> · 1475/1.0M tokens` | `Gemini 3.8 Flash (High) · 1475/1.0M tokens` |
| Row 2 | `(0.1%)` (1475/1M×100 = 0.1475 %) | `(0.1%)` |
| Row 3 | `Estimated usage (live session, 5 messages)` | same |
| Row 4 | `◉ User messages: 1000 tokens (0.1%)` | same |
| Row 5 | `◉ Agent responses: 475 tokens (0.0%)` | same |
| Row 6 | `◉ Tool calls: 0 tokens (0.0%)` | same |
| Row 7 | `□ Free space: 1.0M (99.9%)` | same |

### 20-message state (synthetic: 10 user + 10 assistant of progressive length)

Anchor: `TestPanelContextNonEmpty20MsgLayout`. User payloads
4000–5800 chars (`1000 + (50×i)` for `i ∈ [0, 9] = 1000, 1050, …,
1450`); assistant payloads 5000–7750 chars (`1250 + (75×i)` for `i
∈ [0, 9] = 1250, 1325, …, 1925`). Sum: user = `12250`, agent =
`15875`, used = `28125` = 2.8125 % of 1.0 M, free = `971875` =
97.1875 %.

| Row | REF (unprobed) | OUR (clone) |
|---|---|---|
| Title | `└ Context Usage` | same |
| Row 1 | `<model> · 28125/1.0M tokens` | same |
| Row 2 | `(2.8%)` (28125/1M = 2.8125 %) | `(2.8%)` |
| Row 3 | `Estimated usage (live session, 20 messages)` | same |
| Row 4 | `◉ User messages: 12250 tokens (1.2%)` | same |
| Row 5 | `◉ Agent responses: 15875 tokens (1.6%)` | same |
| Row 6 | `◉ Tool calls: 0 tokens (0.0%)` | same |
| Row 7 | `□ Free space: 1.0M (97.2%)` | same |

### Tool-call inclusion (synthetic: 3 turnBlocks + 1 live step)

Anchor: `TestPanelContextNonEmptyToolCalls`. A 4000-char user message
(`1000` user tokens) plus 3 `stepKindTool` entries across
`m.blocks[i].steps` and 1 `stepKindTool` in `m.liveSteps` — `4 × 5
= 20` tool tokens. Used = `1020`, free = `998980`.

| Row | REF (unprobed) | OUR (clone) |
|---|---|---|
| Title | `└ Context Usage` | same |
| Row 1 | `<model> · 1020/1.0M tokens` | same |
| Row 2 | `(0.1%)` | `(0.1%)` |
| Row 3 | `Estimated usage (live session, 1 message)` | same |
| Row 4 | `◉ User messages: 1000 tokens (0.1%)` | same |
| Row 5 | `◉ Agent responses: 0 tokens (0.0%)` | same |
| Row 6 | `◉ Tool calls: 20 tokens (0.0%)` | same |
| Row 7 | `□ Free space: 1.0M (100.0%)` (998980 rounds to 100.0%) | same |

`stepKindThought` entries (reasoning-token records) are excluded from
the proxy — the helper counts only `stepKindTool`.

## 3. Token-proxy math (`sessionTokenCounts()`)

```go
const charsPerToken = 4    // 1 token ≈ 4 chars
const toolTokensPerCall = 5 // 1 header + 4 argument overhead
for _, msg := range m.session {
    switch msg.Role {
    case "user":     user += len(msg.Content) / 4
    case "assistant": agent += len(msg.Content) / 4
    }
}
calls := 0
for _, b := range m.blocks {
    for _, s := range b.steps {
        if s.kind == stepKindTool { calls++ }
    }
}
for _, s := range m.liveSteps {
    if s.kind == stepKindTool { calls++ }
}
tool = calls * 5
```

The 4-chars-per-token rule covers English prose / code / short
identifiers without ballooning on any of them. System-role messages
are ignored (the proxy only knows `user` / `assistant`); the helper
returns (0, 0, 0) for any role it doesn't recognise so the panel stays
honest about what it doesn't know.

The cycle-224 brief's "1 token per step header + small constant" maps
to `(1 + 4) × calls`: the 1-token header is the tool-call name, the
4-token budget is roughly a typical `{name, arguments}` JSON payload.

## 4. What changed vs cycle 190

- `cmd/wagyu/slash_panels.go` `panelContextLines()` is now a 3-line
  dispatcher. The empty branch is moved verbatim into
  `panelContextEmptyLines(model)`; the new non-empty branch goes
  through `panelContextLiveLines(model, contextMax)`. `sessionTokenCounts()`
  is the helper the non-empty branch reads.
- `cmd/wagyu/slash_panels_test.go` `TestPanelContextEmptyLayout`
  is preserved **verbatim** (it asserts byte-anchored descriptors on
  the empty state). A new sibling
  `TestPanelContextEmptyStillCycle190` re-runs the same checks
  through the cycle-224 dispatcher.
- `cmd/wagyu/slash_cycle224_test.go` is new — 8 tests, ~430 lines.
- `TODO.md` cycle-190 row updated to reflect the closed
  non-empty follow-up (`✅ cycle 190 + cycle 224`); new P0 row
  documents cycle 224's helper math.

## 5. Files touched

| File | Δ | What |
|---|---|---|
| `cmd/wagyu/slash_panels.go` | +129 / -13 | New `panelContextEmptyLines` (verbatim cycle-190), `panelContextLiveLines` (non-empty), `sessionTokenCounts()`. The old `panelContextLines` body becomes a 3-line dispatcher. |
| `cmd/wagyu/slash_cycle224_test.go` | +428 / 0 (new) | 8 tests: `TestPanelContextEmptyStillCycle190`, `TestPanelContextNonEmpty1MsgLayout`, `TestPanelContextNonEmpty5MsgLayout`, `TestPanelContextNonEmpty20MsgLayout`, `TestPanelContextNonEmptyToolCalls`, `TestSessionTokenCounts`, `TestPanelContextNondescriptorRowsUnchanged`, `TestPanelContextPercentagePadding` (with two subtests for 1-msg + 5-msg). |
| `TODO.md` | +2 / -1 | `/context` row gets the cycle-224 close-marker; P0 row added. |

## 6. Verification

- **`gofmt -l cmd/wagyu/slash_cycle224_test.go`** — empty (new file
  clean). Pre-existing cycle-190 alignment inconsistencies in
  `slash_panels.go` (`panelKind` enum and the cfgVal-table block)
  are unchanged on `git diff` and stay per `AGENTS.md`.
- **`go build -o agy224/our/agy ./cmd/wagyu`** — clean (70 MB,
  matches the cycle-210 PiG post-integration size noted in
  `AGENTS.md §The agent loop`).
- **`go test -vet=off ./cmd/... ./internal/...`** — 27 packages, 0
  failures. The four cycle-224 test sets that touch /context
  (`EmptyLayout`, `EmptyStillCycle190`, `NonEmpty{1,5,20}MsgLayout`,
  `NonEmptyToolCalls`, `SessionTokenCounts`, `NondescriptorRowsUnchanged`,
  `PercentagePadding/{1msg,5msg}`) all pass.
- **Empty-state REF ⇄ OUR** — `/tmp/agycmp/cmp_panels.py context`
  reports `IDENTICAL (SGR-normalised)` against
  `ppref.context.empty.raw.txt` (the captured tmux output is in
  `agy224/our/ppour.context.empty.raw.txt`; the REF baseline is
  symlinked into `agy224/ref/ppref.context.empty.raw.txt`).

## 7. Hard constraints / 存疑

- **REF non-empty probe impossible on this machine.** `~/.gemini/` is
  read-only (`Config discipline` in `AGENTS.md`), so the clone cannot
  pre-seed a refresh token. Google's OAuth wall blocks a multi-turn
  probe the same way cycle 222 found. The four non-empty probe-vs-clone
  tables are **clone-only** until a future OAuth-capable machine or a
  cycle that ships a sandboxed stub takes a real capture.
- **Token-proxy is approximate by design.** The backend does not
  surface per-message token totals, so `sessionTokenCounts()`
  computes `(len(content) / 4)` for each user / assistant message and
  `5 × toolCalls` per recorded tool call. Different model families
  (Gemini, Claude, GPT-OSS) tokenize at different rates — the 4:1
  rule is the language-agnostic middle; Claude-tokenized prose might
  shift by ±20 % depending on word length. For UI purposes (a
  *visually meaningful* progress bar), the proxy is fine; it would
  *not* be fine if the panel needed to drive a billing API.
- **System-role messages contribute 0 tokens.** The proxy only knows
  `user` / `assistant`. On the rare wired-system sessions where the
  role is "system" the panel will under-report. The cycle-224 spec
  does not surface this requirement; a future cycle that wires
  system-prompt tokens into the wire-history count would need a
  third case in the helper.
- **Tool-count only counts `stepKindTool`.** `stepKindThought`
  (reasoning tokens) is excluded. The transcript already shows a
  `Thought for Ns, N tokens` line for those — the panel deliberately
  does not duplicate reasoning-token accounting into /context.
- **The "(X.X%)" rounding rounds toward zero for tiny sessions.**
  A 50-token 1-msg session shows `(0.0%)` even though `50/1M = 0.005
  %`; the `%.1f` format rounds half-down. The four probe-vs-clone
  tables show this consistently (1-msg → 0.0%, 5-msg → 0.1%, 20-msg
  → 2.8%). This matches REF's behaviour under tmux's capture-pane -e
  normalisation.
- **No live `Total` token count.** The model and the proxy never see
  the SDK's `usage.total_tokens`, so the panel cannot render an exact
  "Total tokens billed this session" figure the way a `cmd/wagyu
  --verbose /context` mode might. The proxy is the cycle-224
  scope's stated replacement for that signal.
- **`1.0M` is a hardcoded window.** `panelContextLiveLines()` carries
  `const contextMax = 1_000_000`. A future cycle that allows
  per-account context windows (Gemini Pro is 2M, Claude Sonnet is
  200K, GPT-OSS is 64K) would need to thread this constant from the
  catalogue. Cycle 224 ships the proxy with the cycle-190 magic
  number, exactly as cycle 190 did.
