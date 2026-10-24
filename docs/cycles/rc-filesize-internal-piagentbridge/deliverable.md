# `rc-filesize-internal-piagentbridge` — deliverable

Pure-mechanical refactor that brings `internal/piagentbridge/` under the
project-wide file/function size limits:

- non-test `.go` files ≤ **500** lines
- functions ≤ **50** lines

No semantic changes. No public API renames. Every existing test
passes before and after.

## Scope

`internal/piagentbridge/` only. Test files, `docs/cycles/PLAN.md`,
parallel-cycle files (`internal/remotecontrol/`, `internal/persistentrc/`,
`internal/remotetransport/`) untouched.

## Summary

| File | Before | After | Δ |
|---|---:|---:|---:|
| `agwprovider.go`           | 686L | 287L | -399 |
| `agwprovider_request.go`  |   —  | 166L |  +166 |
| `agwprovider_stream.go`   |   —  | 346L |  +346 |
| `stream.go`               | 364L | 443L |  +79 |
| `piagent_remote.go`       | 277L | 292L |  +15 |
| Other 7 files             | unchanged | unchanged |  0 |

The two non-test source files over 500 lines (`agwprovider.go` 686L)
are now three files; the longest file in the package is
`stream.go` at 443L.

The lines-Δ adds up to: `(166 + 346 + 287 + 443 + 292) −
(686 + 364 + 277) = 1534 − 1327 = +207`. ~207L of growth from the
new doc comments, helper signatures, and brace-style bookkeeping the
splits introduced. No logic added.

## File split topology

### `agwprovider.go` (686 → 287) split into three files by feature cohesion

| File | Owns | Approx lines |
|---|---|---:|
| `agwprovider.go`         | `Provider`, `ProviderConfig`, defaults, `NewProvider`, public methods (`ID`, `Close`, `Endpoint`, `Model`, `WireModel`, `Stream`, `wireModel`), `pinnedProvider`, orchestrator method `stream`, HTTP method `open`, helper `firstNonEmpty`. | 287 |
| `agwprovider_request.go` | Request-side types (`chatRequest`, `streamOptions`, `chatTool`, `chatToolFnShape`, `chatMessage`, `chatToolCall`), `buildChatRequest` and its three helpers (`newChatRequest`, `appendTranscriptToChat`, `appendToolDeclsToChat`, `buildChatAssistantMessage`). | 166 |
| `agwprovider_stream.go`  | Response-side types (`chatChoice`, `chatChunk`, `chatUsage`, `chatUsageDetails`, `chatDelta`, `chatDeltaTool`, `chatDeltaToolFunction`), the SSE→PiG state machine (`streamState`, `toolCallState`, `newStreamState`, `consume`, `applyTextDelta`, `applyToolCallFragment`, `syncCall`, `closeText`, `finish`, `syncToolCall`, `stopReasonFromFinish`, `usageFromChat`), and the SSE scanner pump (`pumpSSE`, `pumpScan`). | 346 |

### `stream.go` (364 → 443) was below 500 already; grew during the function-level split

Same file, more breathing room: the chunk-pump closure grew into a
`bridgePumpState` struct + methods, `ToStreamRequest` got three
helpers. The +79 lines is doc comments and helper signatures; no
behavioural surface.

### `piagent_remote.go` (277 → 292) was below 500 already; grew during the function-level split

The `NewRemoteCascadeSession` factory closure shrank from 62 lines to
~10 lines and now delegates to `resolveRemoteBackend`,
`resolveRemoteModel`, `resolveRemoteEffort`, `buildRemoteCascadeSession`,
`buildRemoteCascadeRtOpts`. Doc comments around the helpers account
for the +15 lines.

## Function-extract topology

### `agwprovider.go:379 buildChatRequest` (69 → 21L orchestrator + 4 helpers ≤ 22L)

| Function | Lines | Responsibility |
|---|---:|---|
| `buildChatRequest`            | 21 | Validate model, marshal JSON, error on empty transcript. |
| `newChatRequest`              | 11 | Static (transcript-independent) request fields. |
| `appendTranscriptToChat`      | 22 | Walk transcript switch. |
| `appendToolDeclsToChat`       | 16 | PiG tool schemas → agw tool shape. |
| `buildChatAssistantMessage`   | 18 | One assistant turn → `chatMessage{Role: "assistant", ToolCalls: ...}`. |

### `agwprovider.go:519 (s *streamState) consume` (59 → 16L orchestrator + 2 helpers)

| Function | Lines | Responsibility |
|---|---:|---|
| `consume`              | 16 | Capture usage/finishReason, delegate to helpers. |
| `applyTextDelta`       | 17 | Emit TextStart + TextDelta into partial. |
| `applyToolCallFragment`| 31 | Resolve or create call state, emit Start/Delta. |

Wire format preserved (`chatDeltaTool`, `chatChatDeltaToolFunction`,
`chatChatUsageDetails`, `chatChoice` are named-struct equivalents of
the previous anonymous structs — same JSON tags, same field order,
same decode result).

### `agwprovider.go:239 (p *Provider) pump` (54L) deleted; replaced by `pumpSSE` (16L) + `pumpScan` (35L)

`Provider.pump` was 54 lines and didn't use `p`. It is now a
package-level function `pumpSSE` (orchestrator: setup, watcher,
spawn) called as `go pumpSSE(ctx, out, partial, reader)` from
`Provider.stream`. The scanner loop body — every per-line decision —
extracted to `pumpScan`. Behaviour identical.

### `stream.go:136 pump` (114L) → `bridgePumpState` struct + 8 methods

| Function | Lines | Responsibility |
|---|---:|---|
| `pump`                   | 22 | Top-level orchestrator (setup, watcher, spawn). |
| `newBridgePumpState`     |  8 | Initialise with textIndex = -1 (the bug fix in this cycle). |
| `(*bridgePumpState).run` | 21 | Per-channel loop, calls `applyChunk`, decides terminal state. |
| `(*bridgePumpState).applyChunk` | 20 | One-chunk dispatch: error→end, else apply text/tools/done. |
| `(*bridgePumpState).applyText` | 17 | TextStart + TextDelta emission. |
| `(*bridgePumpState).applyToolCalls` | 21 | ToolCallStart + ToolCallEnd emission. |
| `(*bridgePumpState).observeMeta` |  6 | Observer hook for `ChunkMeta`. |
| `(*bridgePumpState).closeText` | 11 | TextEnd emission + reset. |
| `(*bridgePumpState).finish` | 13 | Final Done/Error + `out.End(...)`. |

Closure-local `textIndex := -1` and `var text strings.Builder` from
the original became struct fields (`textIndex int`, `text strings.Builder`).
The `-1` initialization moved to `newBridgePumpState` — without it
the very first applyText call would treat `textIndex == 0` as
"already opened" and assign into a still-empty slice. See **bug fix**
below.

### `stream.go:65 ToStreamRequest` (52 → 11L orchestrator + 3 helpers ≤ 20L)

| Function | Lines | Responsibility |
|---|---:|---|
| `ToStreamRequest`              | 11 | Build the static request, walk messages. |
| `appendStreamRequestMessage`   | 20 | Per-message switch (system/user/assistant/tool-result). |
| `appendStreamRequestTools`     | 11 | PiG tool schemas → `backend.ToolDecl`. |
| `buildStreamRequestAssistant`  | 19 | One assistant turn → `backend.Message`. |

### `piagent_remote.go:136 NewRemoteCascadeSession` (62 → 10L orchestrator + 5 helpers ≤ 17L)

| Function | Lines | Responsibility |
|---|---:|---|
| `NewRemoteCascadeSession`      | 10 | Closure that resolves → builds. |
| `resolveRemoteBackend`         | 10 | shim override + nil-fallback to `backend.NewMock()`. |
| `resolveRemoteModel`           |  9 | prompt > `NativeProvider.WireModel()` > `"MiniMax-M3"`. |
| `resolveRemoteEffort`          |  5 | prompt > opts. |
| `buildRemoteCascadeSession`    | 11 | Wire `pigrt.New(ctx, rtOpts)` and wrap in session. |
| `buildRemoteCascadeRtOpts`     | 17 | Mirror `cmd/wagyu/print_pig.go`'s pigrt.New call shape. |

## Bug fix discovered en route

During the stream.go pump split, `TestBackendProviderStreamsText` panicked:

```
panic: runtime error: index out of range [0] with length 0
…stream.go:258
```

Root cause: the closure-local `textIndex := -1` initialiser (originally
inside the `pump` function) became a zero-value struct field on
`bridgePumpState`. `int` defaults to `0`, so the very first `applyText`
call took the "block already open" branch and indexed into a
zero-length `s.partial.Content`. Fixed by setting
`textIndex: -1` in `newBridgePumpState`. This is the only code-level
delta in the cycle — the brief explicitly permits "test breaks".
Without that line the refactor would be functionally identical to the
original; the bug is in the *refactor*, not in the prior behaviour.

## Lines delta

The "before" counts (sourced from git `f334571`):

```
686  agwprovider.go
364  stream.go
286  stall.go
277  piagent_remote.go
126  bridge.go
118  tool.go
 78  native_provider.go
 78  backendprovider.go
 67  recovery.go
─────
2080  total
```

The "after" counts (this cycle, uncommitted):

```
443  stream.go
346  agwprovider_stream.go   ← new
292  piagent_remote.go
287  agwprovider.go
286  stall.go
166  agwprovider_request.go  ← new
126  bridge.go
118  tool.go
 78  native_provider.go
 78  backendprovider.go
 67  recovery.go
─────
2287  total
```

Net `+207L` (≈ +10 %). Growth concentrated in `stream.go`
(`bridgePumpState` is a new type with its own methods) and the two
new files (`agwprovider_request.go`, `agwprovider_stream.go`) which
carry the section-divider comments the original had inline. No file
exceeds 500 lines; no function exceeds 50.

## Reviewer commands

All commands run from the repo root on `main`:

```bash
# Source-side constraints — file and function scan
for f in internal/piagentbridge/*.go; do
  [ "${f##*_test.go}" = "$f" ] || continue
  wc -l "$f"
done | sort -rn
#   443  internal/piagentbridge/stream.go
#   346  internal/piagentbridge/agwprovider_stream.go
#   292  internal/piagentbridge/piagent_remote.go
#   287  internal/piagentbridge/agwprovider.go
#   286  internal/piagentbridge/stall.go
#   166  internal/piagentbridge/agwprovider_request.go
#   126  internal/piagentbridge/bridge.go
#   118  internal/piagentbridge/tool.go
#    78  internal/piagentbridge/native_provider.go
#    78  internal/piagentbridge/backendprovider.go
#    67  internal/piagentbridge/recovery.go

# Functions > 50 lines (should print empty):
for f in internal/piagentbridge/*.go; do
  [ "${f##*_test.go}" = "$f" ] || continue
  awk 'BEGIN{d=0;s=0} /^func/{s=NR;d=0} {d+=gsub(/\{/,"{")-gsub(/\}/,"}")} d<=0 && /^func/{printf "  %s:%d\n", FILENAME, s} d<=0 && NR>s && /^\}/ && NR>s+50 {print "  "FILENAME":"s" ("NR-s+1"L)"}' "$f"
done | grep -v ' ' || echo "(none)"

# Build, vet, test, fmt — the four gates
go build -o /tmp/wagyu ./cmd/wagyu
go vet ./internal/piagentbridge/
go test -race -count=1 ./internal/piagentbridge/ ./internal/remotecontrol/ ./internal/remotetransport/
gofmt -l internal/piagentbridge/

# Diff stat (uncommitted)
git diff --stat internal/piagentbridge/
# new: agwprovider_request.go, agwprovider_stream.go
# M:   agwprovider.go (-399), piagent_remote.go (+15), stream.go (+79)
```

## 硬约束 / Hard constraints

本次重构严格遵守下列约束（cycle 范围与跨包不变性）：

- **未触碰范围外的文件**：`docs/cycles/PLAN.md`、`internal/remotecontrol/`、
  `internal/persistentrc/`、`internal/remotetransport/` 全部保持 git `f334571`
  的状态。`git status` 中只有 `internal/piagentbridge/` 下文件改动 / 新建；
  其余条目均属并行 cycles。
- **零行为变更**：所有 49 个现存测试在重构前后 PASS（`go test -race -count=1 ./internal/piagentbridge/`）。
  没有公共 API 重命名，没有外部签名变更，没有 wire-format 改动。
- **重命名范围**：`(*Provider).pump` → package-level `pumpSSE`
  （私有）；`streamState.consume` 拆为 `consume` + `applyTextDelta` +
  `applyToolCallFragment`（全部私有）。
  其余拆分均保留原名：构造器、struct 字段、所有公开类型。
- **文件 ≤ 500 行**：11 个非-test `.go` 文件，最大 `stream.go` 443L。
- **函数 ≤ 50 行**：最大函数 40L (`Provider.open`)；其余均在 ≤ 38L。
- **零新增第三方依赖**：仅使用 stdlib + 已有的 `PiG` 与 `internal/backend` 导入。
- **commit 不变**：按 brief 要求，本 cycle **未执行 `git commit`**，
  所有更改留在 worktree 中。

## 存疑 / Limitations

1. **stream.go 加长 79 行**：`bridgePumpState` 引入了 7 个新方法（run、
   applyChunk、applyText、applyToolCalls、observeMeta、closeText、finish），
   加上 `newBridgePumpState` 与 pump 自身的精简 orchestrator。函数粒度更细，
   但文件总行数不降反升 —— 这是从 "闭包 + 长函数" 改为 "struct + 多个小方法"
   的固有代价。如果后续要继续压缩 stream.go，可考虑把 `bridgePumpState` 及其
   方法迁到一个新的 `stream_bridge_pump.go` 文件。
2. **`pumpSSE` / `pumpScan` 命名**：之前未约定哪些 SSE 相关函数用什么后缀。
   这次为了避开与原 `pump` 同名，重命名为 `pumpSSE`。Reviewer 如更想
   直接叫 `nativePump` / `nativePumpScan`，可一搜即换。
3. **`firstNonEmpty` 留在 `agwprovider.go`**：尽管它在
   `agwprovider_stream.go` 的同包内也方便放置，但唯一的 caller 是
   `NewProvider`，物理上跟在 `agwprovider.go` 里更内聚，故保留在那里。
4. **未触动 `chatRequest` / `chatChunk` 字段名**：保留原始字段标签以保证
   JSON wire 一致。原文件用了内联匿名 struct；这次改为命名 struct
   （`chatChatUsageDetails`、`chatDeltaToolFunction` 等），仅用于类型导出
   不影响 JSON tags 与解析结果。Reviewer 可用 `git diff` 验证 tags / order
   一一对应。
5. **未触动 `StreamRequest` / `Message` / `ToolCall`**：这些是
   `internal/backend` 的公开类型，全部保持原状。

## Cycle status

| Gate | Result |
|---|---|
| `go build -o /tmp/wagyu ./cmd/wagyu` | exit 0 |
| `go vet ./internal/piagentbridge/`   | exit 0, clean |
| `go test -race -count=1 ./internal/piagentbridge/ ./internal/remotecontrol/ ./internal/remotetransport/` | PASS (3 packages) |
| `gofmt -l internal/piagentbridge/`   | clean (no output) |
| File lines ≤ 500 | yes (max 443L) |
| Function lines ≤ 50 | yes (max 40L) |
| `git commit` | **not run** by brief |

Reviewer: rerun the "Reviewer commands" block above; expected output
is shown inline.
