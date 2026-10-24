# Cycle 100 — thinking-mode display parity

**User request (this round).** *"还有就是 think 模式的打印风格，要和 agy 一致 现在是输出 裸
thinking"* — make the thinking display match `agy`; right now it shows bare/naked
`thinking`. Plus: keep filling in the most basic missing features from the changelog,
step by step, until the two align.

This deliverable covers the thinking-display half. The changelog-driven feature
sweep continues in later cycles.

---

## 1. What was actually wrong

Two separate things were investigated before touching code.

### 1.1 The raw ` thinking` text is already stripped everywhere we can test

The streaming sanitizer (`internal/agent/sanitize.go`, cycle 50) removes
` thinking… response` blocks from every user-visible path. Reproduced against the
live `agw.k0s.io` gateway (which *does* return the reasoning inline in
`delta.content`):

```
$ curl -sN https://agw.k0s.io/v1/chat/completions … "Think step by step: what is 17*23?"
data: {…"delta":{"content":" thinkingThe user is asking me to calculate 17 × 23…"…}}
```

`grep -c think` on the clone's output is **0** in every path checked:

| Path | Command | `think` in output |
|---|---|---|
| print, no tools | `agy -p "What is 17*23? Think it through."` | 0 |
| print, tool call | `agy -p "Read a.txt with view_file…"` | 0 |
| print, mock backend | `agy -p "hi"` | 0 |
| TUI, live pty | `tui_probe.py … "Think step by step…"` | 0 |

So the literal `Thinking` text is not leaking. What **is** missing is the
reference's *rendering* of the model's reasoning: `agy` shows it as a first-class
step in the conversation view, and the clone showed nothing at all.

### 1.2 The reference's thinking model (recovered)

Extracted from the 1.3.6 string blob and the recovered symbol table:

| Evidence | Grade | What it pins |
|---|---|---|
| `Thought for %s` / `Thought for %s, %s tokens` / `Thought for %.1fs  (%s, %s tokens)` | 【strings】 | the completed-thought step title |
| changelog: *"in-progress thinking frames … showing `Thinking... (31s)` forever"* | 【strings】 | the live in-progress frame, and that it lives in the **conversation history** |
| `%s in \| %s out \| %s thinking` | 【strings】 | reasoning tokens are surfaced separately |
| `coalesceThinkingSteps`, `canMergeConsecutiveThoughts`, `canUpgradeTrailingThoughtToToolGroup`, `isThoughtOnlyGroup` (cli/store) | 【反编译】symbols | thoughts are *steps* that merge into groups |
| `collapsedThinkingSnippet`, `thinkingLiveSnippet`, `thinkingIntentSnippet`, `summarizeThinkingSteps`, `LastThinkingSnippet` (cli/render) | 【反编译】symbols | live vs collapsed snippet rendering |
| `StripThoughts`, `makeThinkingStripper` | 【反编译】symbols | the reference also *strips* thinking on some paths (chat messages / print) |
| live capture: `completion_tokens_details.reasoning_tokens` | 【一手】 | the token count source |

The live `agy` TUI could not be captured for a byte-exact layout (account quota
exhausted — every reference turn ends `RESOURCE_EXHAUSTED`), so the **arrangement**
is a reconstruction (【推测】); the **vocabulary and behaviour** are recovered.

---

## 2. What changed

### `internal/backend` — reasoning-token plumbing

* `ChunkMeta.ReasoningTokens` (`client.go`) — the completion-token breakdown.
* `agwSSEChunk.Usage.CompletionTokensDetails.ReasoningTokens` (`agw_k0s.go`) —
  parsed from the gateway's `usage.completion_tokens_details.reasoning_tokens`.

Live shape (captured):

```json
"usage":{"total_tokens":501,"prompt_tokens":188,"completion_tokens":313,
         "completion_tokens_details":{"reasoning_tokens":0}, …}
```

Note the gateway currently reports `reasoning_tokens: 0` even when the model
emits a large think block, so the token clause is omitted in practice — the
title degrades to `Thought for Ns` exactly as the reference's `Thought for %s`
variant does.

### `internal/agent/sanitize.go` — observe the reasoning without forwarding it

New optional `Sanitizer.OnThought func(string)`:

* called with the text *inside* a think block (never the boundary tags), per
  fragment, so a live view can stream it;
* `nil` by default → **print mode is byte-identical to before** (it only wants
  the answer).

The `inside` branch now reports the consumed run to `OnThought` before dropping
it; the no-op `_ = holdStart` leftover was removed.

### `cmd/antigravity/tui_tools.go` — thought steps

* `stepKind` (`stepKindTool` / `stepKindThought`); `toolStep` gained `kind`,
  `tokens`, `dur`.
* `thoughtTitle(d, tokens)` → `Thought for 3s, 24 tokens` / `Thought for 3s`
  (recovered `Thought for %s, %s tokens`).
* `humanDuration` → `3s`, `1.5s`, `400ms`.
* `thinkingLiveLine(elapsed)` → `⏺ Thinking... (3s)` (recovered `Thinking... (31s)`).
* Thought steps reuse the existing group/expand machinery, so ctrl+o expands the
  reasoning and a thought that precedes tool calls heads the same `N steps` block
  (the reference's `canUpgradeTrailingThoughtToToolGroup`).

### `cmd/antigravity/tui_v135.go` — live frame + turn wiring

* New `thinking` / `thinkingSince` state, `thoughtStartMsg` tea message.
* While the model reasons, `View()` renders `⏺ Thinking... (Ns)` above the
  spinner row; `computeCursorY` accounts for the extra row.
* `streamPrompt` installs `sanitizer.OnThought`, captures the usage `ChunkMeta`,
  and prepends a `stepKindThought` step to the turn's block with the elapsed
  duration and reasoning-token count.
* `turnDoneMsg` / `backendDoneMsg` / `backendChunkMsg` / a new turn all clear the
  live frame.

Print mode is deliberately unchanged: `OnThought` stays `nil`, so `agy -p` keeps
emitting only the answer (the cycle-49/50 byte-parity invariant).

---

## 3. Verification

| Check | Result |
|---|---|
| `go build ./cmd/antigravity ./internal/...` | exit 0 |
| `go vet ./cmd/antigravity ./internal/agent ./internal/backend` | clean |
| `go test ./internal/agent ./internal/backend ./cmd/antigravity` | ok |
| parity harness (`harness136.sh`, 87 cases) | **86/87** — unchanged; the single diff is the known `/plugin` pre-auth divergence |
| live TUI capture (`tui_probe.py`, agw backend) | `⏺ Thinking... (Ns)` during the block, then `⏺ Thought for 3.2s  (ctrl+o to expand)` before the answer |

New tests: `TestSanitizer_OnThoughtReportsReasoning`,
`TestSanitizer_OnThoughtSpansChunks`, `TestAgwK0sReasoningTokens`,
`TestTUITools_ThoughtStepFromStream`, `TestTUITools_ThoughtPrecedesToolSteps`,
`TestTUITools_ThoughtLiveFrame`.

---

## 4. 硬约束 / limitations

* **Layout is a reconstruction.** The live reference TUI cannot be captured (quota
  exhausted), so the *placement* of the thinking frame and the exact separator
  between `Thought for …` and the `(ctrl+o to expand)` hint are 【推测】. The
  strings (`Thought for %s`, `Thought for %s, %s tokens`, `Thinking... (31s)`) and
  the merge/group semantics are recovered.
* **`reasoning_tokens` is currently always 0** on the `agw.k0s.io` transport, so
  the `, N tokens` clause does not appear in live captures. The plumbing is in
  place for a transport that reports it.
* **Only ` thinking` is recognised** as a think delimiter. `<thinking>` /
  `</thinking>` are *not* stripped, because the reference's stripper works on
  structured thought parts, not text tags, and widening the tag set would change
  the byte-verified print-mode output. If a model on another transport emits a
  different tag, it will pass through verbatim — flagged, not silently handled.
* **Print mode still hides thinking entirely.** If the user's "裸 thinking"
  observation came from `-p`, it was not reproducible here; the evidence above is
  the full set of paths checked.
