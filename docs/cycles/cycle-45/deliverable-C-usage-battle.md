# Cycle 45 — Deliverable C: UsageBattle 真接 lifecycle.UsageTracker (Issue #7 真 1:1)

## Summary

把 cycle-43 占位符 4 fake tool (`read_file / list_directory / git_log / edit_file`)
替换为 3 真 agent slot (`claude-4.5-sonnet / agw-MiniMax-M3 / shim-mock-1`),
真接 `backend.UsageTracker` (新加) 累计 prompt / completion / total tokens,
真接 cascade `[usage] prompt=N completion=N total=N fp=...` 行
(`ParseUsageLine` 新加), 跨 backend 路由 (`backend=agw → slot 1 / shim → slot 2`),
'r' key 单 slot reset, View 渲染真 slot list + 总用量 + prompt/completion 列,
保留 cycle 43 C view 格式 (status icon + tok count + battle total)。

## Changed files

| File | 状态 | 行数 | 作用 |
|---|---|---|---|
| `internal/backend/lifecycle.go` | 改 | +144 | 新加 `UsageTracker` / `UsageSnapshot` / `UsageTrackerOwner`; `Lifecycle` 持 `usage *UsageTracker`, `Run` 在 terminal chunk `chunk.Meta != nil` 时调 `usage.Add(PromptTokens, CompletionTokens, TotalTokens)`, 新加 `Usage()` accessor |
| `internal/tui/usage_battle.go` | 改 | +689 (相对 cycle 43) | 真接: 3 agent slot + per-slot `UsageTracker`, `UsageLineMsg`, `ParseUsageLine`, 'r' reset, `AgentBackendBySlot` + `SlotForBackend` 跨 backend 路由, `TotalUsage()` / `SlotTracker()`, View 渲染 prompt/completion/total 三列 |
| `internal/tui/usage_tracker_test.go` | 新 | 6 sub-test | `TestUsageTracker_Accumulate` / `TestUsageTracker_BackendRouting` / `TestUsageTracker_SlotSwitch` / `TestUsageTracker_ViewRendersSlots` / `TestParseUsageLine` |
| `internal/tui/tui_test.go` | 改 | -2 +6 | `TestUsageBattle_View` 改测 3 真 agent slot 名字 |
| `internal/tui/parity_test.go` | 改 | -10 +12 | `TestV135ParityUsageBattle` + `TestV135ParityTasksUsageBattleSmoke` 改测 `AgentSlotNames` |
| `internal/tui/tui_integration_test.go` | 改 | -4 +5 | `TestTUIIntegration_SingleHelloSubmission` UsageBattle 测 3 slot names |

## go test 输出 (final, 6/6 PASS in 0.020s)

```
$ cd /workspace/agy-recovered && GOMODCACHE=/workspace/.home/go/pkg/mod \
    GOCACHE=/tmp/gocache_FINAL GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off \
    /workspace/agy/go126/bin/go test -count=1 -p 1 -timeout=30s \
    -run "TestUsage|TestParseUsageLine" -v ./internal/tui/

=== RUN   TestUsageBattle_View
--- PASS: TestUsageBattle_View (0.00s)
=== RUN   TestUsageTracker_Accumulate
--- PASS: TestUsageTracker_Accumulate (0.00s)
=== RUN   TestUsageTracker_BackendRouting
--- PASS: TestUsageTracker_BackendRouting (0.00s)
=== RUN   TestUsageTracker_SlotSwitch
--- PASS: TestUsageTracker_SlotSwitch (0.00s)
=== RUN   TestUsageTracker_ViewRendersSlots
--- PASS: TestUsageTracker_ViewRendersSlots (0.00s)
=== RUN   TestParseUsageLine
--- PASS: TestParseUsageLine (0.00s)
PASS
ok  	github.com/chadgpt/openagy/internal/tui	0.020s
```

## Notes

### 1. 跟 cycle 42 close-gap 实证对齐

`TestUsageTracker_Accumulate` 用 cycle 45 spec 的 3 events (10+5+15, 20+10+30,
50+15+65) 累计 → `prompt=80, completion=30, total=110` (跟 task 标的
"total = 50 + 30 = 80 prompt, 30 completion, 110 total" 1:1)。

`TestUsageTracker_BackendRouting` 用 cycle 42 close-gap 实证的 3 backend 值
(agw=179p/30c/209t = 跟 task 引用 "prompt=179 completion=30 total=209" 1:1)。

### 2. View 格式保留 cycle 43 C

`▶` cursor marker + `○`/`▶`/`✓`/`✗` status marker (cycle 32 沿用) +
`pNcM tT` 三列 (cycle 45 新) + `battle: total=N tok` (cycle 43 C 沿用)。
Panel 宽度 40 col (PanelWidth)。

### 3. 跨 backend routing

`AgentBackendBySlot = ["agw", "shim", "mock"]` 跟 cmd/antigravity/main.go::
pickBackend 的 dispatch 命名一致 (cycle 44 B 加的 `--backend` flag)。
`SlotForBackend(backend) int` 返 -1 表示未识别 → silent drop。

### 4. Build 卡住的根因 (给后续 cycle)

NAS 5 sibling task 同步跑 `go test ./internal/tui/` 抢 I/O, 每次 cache
invalidation 触发 full rebuild (charm.land/bubbletea/v2 + ultraviolet +
x/ansi + x/term + colorful + cancelreader + displaywidth + uax29 +
runewidth + fsnotify + x/sys + x/termios + x/windows + uniseg + x/sync +
terminfo 等 transitive deps 共 ~50 个 .a 文件, NAS 76MB cache 每次 link 30s+)。
06:00 cache 干净时 0.020s 跑过 6/6 test。
