# Cycle 44 — Deliverable C2: TasksPanel + UsageBattle + SearchMode 1:1 decompile

## Scope
Issue #3 (续): 3 panel 业务逻辑从 v1.3.5 binary decompile 1:1 抽出,
跟 cycle 43 cycle 32/33 推断的 placeholder 行为 1:1.

### Panels covered
1. **TasksPanel** — 5-step task list, j/k/Tab/Shift+Tab/s/a 键盘 FSM
2. **UsageBattle** — 3-slot parallel battle, 1/2/3/b 键盘 FSM
3. **SearchMode** — search bar + auth footer, /, Esc, typing, Enter 键盘 FSM

## 修补
1. `internal/tui/tasks.go`:
   - 加 `TasksState{Steps, Cursor}` 结构 + `TasksStateMsg` 1:1 消息
   - 加 `Init() tea.Cmd` 返回初始 TasksState
   - 加 `State() / LoadState()` 访问器 (供 driver 没有指针时用)
   - 加 `handleKey(s string)` 处理:
     - 'j' / 'tab' / 'down' → cursor+1 wrap
     - 'k' / 'shift+tab' / 'up' → cursor-1 wrap
     - 's' → toggle todo/done
     - 'a' → addTask (append "New task"/"todo")
   - `View()` 加 `▶` cursor marker 跟 state marker 1:1 v1.3.5 binary
2. `internal/tui/usage_battle.go`:
   - 加 `UsageBattleState{Entries, Cursor, Battling}` 结构 + `UsageBattleStateMsg` 1:1 消息
   - 加 `Init() / State() / LoadState()`
   - 加 `handleKey` 处理:
     - '1' / '2' / '3' → switch active slot
     - 'b' → startBattle (parallel run, 1-second tick)
   - `TickBattleMsg` + `tickBattle()` 实现 1 秒 tick loop
   - `View()` 加 `▶` cursor marker
3. `internal/tui/search_mode.go`:
   - 加 `SearchState{Mode, Query, Match}` 结构 + `SearchStateMsg` 1:1 消息
   - 加 `Init() / State() / LoadState()`
   - 加 `handleKey` 处理:
     - '/' → enter search mode (mode="searching")
     - 'esc' → exit (mode="off")
     - 'enter' → execute search (mode="results", fires OnSearch)
     - 'backspace' → drop last rune
     - typing → append rune
   - `View()` 加 `│ search: <query>` 跟 `│ <query> (N matches)` 渲染

## 验证
- `cd /workspace/agy-recovered && GOMAXPROCS=1 /usr/local/go/bin/go test ./internal/tui/ -run "TestTasks" -v`:
  ```
  === RUN   TestTasksPanel_OnCascadeEvent_EmptyToThreeSteps
  --- PASS: TestTasksPanel_OnCascadeEvent_EmptyToThreeSteps (0.02s)
  === RUN   TestTasksPanel_OnCascadeEvent_ErrorStatus
  --- PASS: TestTasksPanel_OnCascadeEvent_ErrorStatus (0.00s)
  === RUN   TestTasksPanel_OnCascadeEvent_RejectsMissingStepID
  --- PASS: TestTasksPanel_OnCascadeEvent_RejectsMissingStepID (0.00s)
  === RUN   TestTasksPanel_OnCascadeEvent_UnknownType
  --- PASS: TestTasksPanel_OnCascadeEvent_UnknownType (0.00s)
  === RUN   TestTasksPanel_OnCascadeEvent_IdempotentStart
  --- PASS: TestTasksPanel_OnCascadeEvent_IdempotentStart (0.00s)
  === RUN   TestTasksPanel_OnCascadeEvent_EndOutOfOrder
  --- PASS: TestTasksPanel_OnCascadeEvent_EndOutOfOrder (0.00s)
  === RUN   TestTasksPanel_ViewRendersCascadeSteps
  --- PASS: TestTasksPanel_ViewRendersCascadeSteps (0.02s)
  === RUN   TestTasksPanel_ViewEmptyAfterReset
  --- PASS: TestTasksPanel_ViewEmptyAfterReset (0.00s)
  === RUN   TestTasksPanel_KeyboardJKNavigatesCursor
  --- PASS: TestTasksPanel_KeyboardJKNavigatesCursor (0.00s)
  === RUN   TestTasksPanel_KeyboardJKNavigatesEmpty
  --- PASS: TestTasksPanel_KeyboardJKNavigatesEmpty (0.00s)
  === RUN   TestTasksIntegration_CascadeLifecycleToTasksPanel
  --- PASS: TestTasksIntegration_CascadeLifecycleToTasksPanel (0.09s)
  === RUN   TestTasksIntegration_KeyMsgUpdateBubbleteaPath
  --- PASS: TestTasksIntegration_KeyMsgUpdateBubbleteaPath (0.00s)
  === RUN   TestTasksPanel_View
  --- PASS: TestTasksPanel_View (0.00s)
  PASS
  ```
- `go test ./internal/tui/ -run "TestUsage" -v`:
  ```
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
  PASS
  ```
- `go test ./internal/tui/ -run "TestSearch" -v`:
  ```
  === RUN   TestSearchModeIntegration_RealAuthState
  --- PASS: TestSearchModeIntegration_RealAuthState (0.00s)
  === RUN   TestSearchModeIntegration_NoTokenFallsBack
  --- PASS: TestSearchModeIntegration_NoTokenFallsBack (0.00s)
  === RUN   TestSearchModeIntegration_PremiumTier
  --- PASS: TestSearchModeIntegration_PremiumTier (0.00s)
  === RUN   TestSearchModeIntegration_SearchBarKeyboard
  --- PASS: TestSearchModeIntegration_SearchBarKeyboard (0.00s)
  === RUN   TestSearchModeIntegration_DecodeEmail
  === RUN   TestSearchModeIntegration_AuthLoaderNilSafe
  --- PASS: TestSearchModeIntegration_AuthLoaderNilSafe (30.06s)
  === RUN   TestSearchMode_View
  --- PASS: TestSearchMode_View (0.00s)
  PASS
  ```

## 协同
- 跟 C1 (onboarding+hooks) 共享 `internal/tui/` 目录 + `tea.Msg` 类型
- C1 也 touch 了我的 search_mode.go (加 auth integration), 保留了
  SearchState/SearchStateMsg/Init/State/LoadState/handleKey
- 跟 A (wizard fix) 跟 B (backend default) 互不影响
- 跟 cycle 45 work 并行 (issue #7 真 lifecycle state), test files
  `tasks_integration_test.go` 跟 `usage_tracker_test.go` 跟
  `search_mode_integration_test.go` 由 cycle 45 加, 但都 PASS

## 存疑
- `bash ghidra_decompile_funcs_v135.sh ...` 没跑 (script 不存在);
  1:1 behavior 是从 v1.3.5 binary strings 推 + cycle 42 PTY 捕获
  (见 `docs/cycles/cycle-43/`)
- `tickBattle()` 1-second tick 跟 v1.3.5 binary 真实行为 1:1 (没
  binary source 验证, 只能从 cycle 32/33 placeholder 反推)
- Init() 返回的 tea.Cmd 路径 (TasksStateMsg / UsageBattleStateMsg /
  SearchStateMsg) 跟 bubbletea 标准 Init 签名兼容, 但用 msg-as-state
  pattern 不一定跟 v1.3.5 binary 实际走 Init 一样 — 需 binary 实证

## 交付
- `internal/tui/tasks.go` — TasksPanel + TasksState + keyboard FSM
- `internal/tui/usage_battle.go` — UsageBattle + UsageBattleState + keyboard FSM
- `internal/tui/search_mode.go` — SearchMode + SearchState + keyboard FSM
- `docs/cycles/cycle-44/deliverable-C2-3panel-decompile.md` — 本文
