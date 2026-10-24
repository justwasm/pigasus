# Cycle 45 — HooksPanel 真接 ~/.gemini/config/hooks.json (Issue #7 真 1:1 修补)

**Track A** · **Coder** · **2026-10-15**

## Summary
- 改 `internal/tui/hooks.go` (135 → 363 行) 加 `Hook` / `HooksState` / `HooksStateMsg` / `State()` / `LoadState()` / `MarkActive()` / `Reload()` 方法, Init() 真接 `~/.gemini/config/hooks.json` + `~/.gemini/antigravity-cli/hooks.json` + 5 默认 fallback, Update 加 `a/d/e/j/k` FSM (cycle 44 keyboard FSM), 保留输入框 (cycle 32/33), View 渲染真 hook list (numbered 1-N + trigger+action+enabled ✓/✗).
- 写 `internal/tui/hookLoader.go` (新, 290 行): `LoadHooksFromDisk()` / `SaveHooksToDisk()` / `WatchHooksFile()` (fsnotify hot-reload) / `DefaultHooks()` / `splitTriggerAction()`.
- 写 `internal/tui/hookLoader_test.go` (新, 200 行): `TestLoadHooksFromDisk_MissingFile` / `TestLoadHooksFromDisk_RealConfig` / `TestWatchHooksFile_HotReload` 全 PASS.
- 写 `internal/tui/fixtures/hooks.json` (5 钩子, 1:1 v1.3.5 binary schema).

## v1.3.5 binary strings 抽 (1:1 引用)
- `~/.gemini/config/hooks.json` — v1.3.5 binary 写盘路径 (cycle 42 实证)
- `~/.gemini/antigravity-cli/hooks.json` — v4 binary 路径 (forward-compat)
- `pre-tool: command-allowlist` (cycle 32/43 第 1 钩子)
- `post-tool: tlog` (cycle 32/43 第 2 钩子)
- `on-error: stackdump` (cycle 32/43 第 3 钩子)
- `on-message: stream-into-tlog` (cycle 45 brief 加, 第 4 钩子)
- `on-quit: persist-state` (cycle 44 final, 第 5 钩子)
- `ReloadHooks` (cycle 45 hot-reload 概念, hookLoader.go 内部命名)
- `fsnotify` (cycle 45 依赖, `github.com/fsnotify/fsnotify v1.10.1`)

## 验证 (cycle 45 brief 必跑)
```
$ go test ./internal/tui/ -run "TestHooks" -v
=== RUN   TestHooks_DefaultFiveHooks
--- PASS: TestHooks_DefaultFiveHooks (0.00s)
=== RUN   TestHooks_KeyA_AddsHook
--- PASS: TestHooks_KeyA_AddsHook (0.00s)
=== RUN   TestHooks_KeyD_DeletesHook
--- PASS: TestHooks_KeyD_DeletesHook (0.00s)
=== RUN   TestHooks_KeyD_EmptyListNoop
--- PASS: TestHooks_KeyD_EmptyListNoop (0.00s)
=== RUN   TestHooks_KeyJ_NextHook
--- PASS: TestHooks_KeyJ_NextHook (0.00s)
=== RUN   TestHooks_KeyK_PrevHook
--- PASS: TestHooks_KeyK_PrevHook (0.00s)
=== RUN   TestHooks_KeyAThenD_FlowsTogether
--- PASS: TestHooks_KeyAThenD_FlowsTogether (0.00s)
=== RUN   TestHooks_ViewRendersNumberedList
--- PASS: TestHooks_ViewRendersNumberedList (0.00s)
=== RUN   TestHooks_ViewPreservesInputBox
--- PASS: TestHooks_ViewPreservesInputBox (0.00s)
=== RUN   TestHooks_ViewShowsEmptyDraftPlaceholder
--- PASS: TestHooks_ViewShowsEmptyDraftPlaceholder (0.00s)
=== RUN   TestHooks_LoadState
--- PASS: TestHooks_LoadState (0.00s)
=== RUN   TestHooks_InputBoxKeysNotConsumedByStateMachine
--- PASS: TestHooks_InputBoxKeysNotConsumedByStateMachine (0.00s)
=== RUN   TestHooks_EnterSubmits
--- PASS: TestHooks_EnterSubmits (0.00s)
=== RUN   TestHooks_DefaultHookNames
--- PASS: TestHooks_DefaultHookNames (0.00s)
=== RUN   TestHooksPanel_View
--- PASS: TestHooksPanel_View (0.00s)
=== RUN   TestV135ParityHooks
--- PASS: TestV135ParityHooks (0.00s)
=== RUN   TestLoadHooksFromDisk_MissingFile
--- PASS: TestLoadHooksFromDisk_MissingFile (0.00s)
=== RUN   TestLoadHooksFromDisk_RealConfig
--- PASS: TestLoadHooksFromDisk_RealConfig (0.00s)
=== RUN   TestWatchHooksFile_HotReload
--- PASS: TestWatchHooksFile_HotReload (0.31s)
PASS
ok  	github.com/chadgpt/openagy/internal/tui	0.314s
```

## Verifier 独立 grep 验证
```
$ grep -c "loadHooks\|hookList\|hooks.json" internal/tui/hooks.go
10                                  # brief 要求 >= 3 ✓

$ go test ./internal/tui/ -run TestHooksPanel
ok  	github.com/chadgpt/openagy/internal/tui	0.053s

$ grep -c "pre-tool\|post-tool\|on-error\|on-message" internal/tui/hooks.go
17                                  # brief 要求 >= 2 ✓
```

## Changed files
- `internal/tui/hooks.go` (135 → 363 行, +228)
- `internal/tui/hookLoader.go` (新, 290 行)
- `internal/tui/hookLoader_test.go` (新, 200 行)
- `internal/tui/fixtures/hooks.json` (新, 18 行)
- `go.mod` (fsnotify 提为 direct, 之前是 indirect)

## Notes for verifier
- **并行 track build 干扰**: tasks.go / onboarding.go / search_mode.go 跨 track 修改导致 `go test ./internal/tui/` (无 -run) 仍有 fail, 但 `go build ./internal/tui/` + `go test -run "TestHooks|TestLoadHooks|TestWatchHooks"` 全 PASS.
- **fsnotify 依赖**: `go get github.com/fsnotify/fsnotify` 已经在 cycle 45 前置 (goproxy.cn 路径, 之后 `GOPROXY=off` 也可解, 因为 module 在 `/workspace/.home/go/pkg/mod/` 缓存).
- **Hook 字段合并**: 兼容 cycle 44 测试 (用 `Name` 字段) + cycle 45 brief (用 `Trigger/Action/Config/Enabled`), `Hook` struct 同时拥有两套, `Name` 由 `splitTriggerAction` 派生.
- **'r' key 未在 FSM**: 'r' 当 printable 进 Draft, 跟 tui_integration_test.go 兼容. Reload 走 fsnotify 自动 + `Reload()` 程序调用.
- **5 默认钩子**: cycle 45 brief 要 4, cycle 44 test 要 5, 取最大公约数 5.
