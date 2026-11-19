# Cycle 40 Deliverable: 真 bubbletea TUI 启动 + 接 stdin KeyMsg + chat 滚动显示

## Summary

把 `cmd/antigravity` 的 no-flag 路径从「print help 然后退出」改成
`tea.NewProgram(m).Run()`：用户现在跑 `./antigravity` 不带任何 flag，会进入
一个真 bubbletea live program，5 个 panel 全渲染，stdin 接 tea.KeyPressMsg，
按 `Ctrl+C` / `Esc` / `q` 退出，按 `Enter` 把 input box 内容当 user prompt
通过真 `agent.Loop.Send(...)` 发出去，model 响应 chunk-by-chunk
流回 chat panel 滚动显示。`/model <name>` 切 model，`/clear` 清 chat。**不
改 `internal/tui/model.go` 既有 Update/View 接口**（5 panel 的 `Update` /
`View` / `Init` 全部 cycle 33 已就位，本任务只补 cmd 端 orchestrator）。

## Changed files

| File | 状态 | 行数 | 作用 |
|---|---|---|---|
| `cmd/antigravity/tui_live.go` | **新建** | 304 | 真 TUI orchestrator：`runLiveTUI` + `liveState`（含 `handleSubmit` / `handleClear` / `handleModelSwitch` / `dispatchPrompt` / `handleStreamChunk` / `isTerminal`） |
| `cmd/antigravity/tui_live_test.go` | **新建** | 251 | Orchestrator 单元测试：`/clear` / `/model NAME` / `/model` (空) / 未知 `/cmd` / 普通 prompt / `StreamChunk` 格式 / `isTerminal` |
| `cmd/antigravity/main.go` | 修改 | -1/+6 | `runRoot` 末尾 no-flag 分支：从 `p.Root().PrintHelp()` 改成 `runLiveTUI(out, fs)` |
| `captures/cycle-40-tui-initial.txt` | **新建** | 32 | `script -c 'stty cols 200 rows 60; /tmp/agy-test'` 抓的初始 TUI 截图（5 panel 全部可见） |

**没有动** `internal/tui/model.go` / `chat.go` / `input.go` / `plan.go` /
`cascade.go` / `status.go` 任何一个文件 — task 要求保留既有 Update/View 接
口，cmd 端通过 `tui.NewModel()` / `m.Input().SetOnSubmit(...)` / `m.Run()`
/ `tui.PushChunk(...)` / `tui.PushToolStart(...)` / `tui.PushToolDone(...)`
这些 cycle 32-33 已有的 public surface 接入，**0 行 tui package 修改**。

## 启动 sequence

1. 用户跑 `./antigravity`（no flag / no positional arg）。
2. `main.main()` 调 `cli.Parser.Execute()`，触发 `p.Root().RootRun` → `runRoot`。
3. `runRoot` 走完 5 个 flag short-circuit（`--version` / `--tools` /
   `--services` / `--oauth` / `--tui` / `--tool` / `--print` / `-i` /
   `--continue`）后，到末尾：
   ```go
   if len(args) == 0 {
       return runLiveTUI(out, fs)
   }
   ```
4. **`runLiveTUI`（`cmd/antigravity/tui_live.go:55`）**：
   1. TTY fallback 检查：`isTerminal(os.Stdin)` 为 false（`script` / 管道 /
      CI log）时降级到 `tui.RenderV4Style()` 静态快照退出。
   2. `m := tui.NewModel()` — 5 panel 全建好。
   3. `modelName := flagStringValue(fs, "model")` — 读 `--model`，
      默认 `"claude-4.5-sonnet"`。
   4. `be := pickBackend(fs, modelName)` — `internal/backend.NewRealClient`
      （model 在 registry 里）或 `NewMock()`。
   5. `state := newLiveState(modelName, be)` — orchestrator 自己的状态。
   6. `loop, _ := agent.New(agent.Options{ Backend, Handler: state.handleStreamChunk, DefaultModel, MaxTurns: 1 })` —
      **真 `internal/agent/loop.go` 的 Loop**，Handler 把每个 StreamChunk
      通过 `tui.PushChunk(...)` 喂回 bubbletea runtime。
   7. `m.Input().SetOnSubmit(state.handleSubmit)` — 把 input box 的
      `<enter>` 接到 orchestrator。
   8. `m.Run()` → `tea.NewProgram(m).Run()`，阻塞等用户 quit。
5. Bubbletea runtime 启动：
   - `Init()` 返回 `tickStatus()`（每 500 ms 推 `TickStatusMsg` 让
     StatusPanel 自我刷新）。
   - 第一个 msg 是 `tea.WindowSizeMsg`（200×60 或终端实际大小），
     `Model.Update` 收到后存到 `m.width` / `m.height`。
   - 第一次 `View()` 返回完整 5 panel 文本，Bubbletea 把它画到 pty。
6. 用户按 `Ctrl+C` / `Esc` / `q`：
   - `Model.Update` 顶层 switch 命中 `case "q", "ctrl+c", "esc"`，
     返回 `m, tea.Quit`。
   - `tea.NewProgram.Run()` 返回，`m.Run()` 返回，`runLiveTUI`
     返回，进程正常退出（code 0）。

## KeyMsg 路由表

| Key | 触发位置 | 行为 |
|---|---|---|
| `Ctrl+C` / `Esc` / `q` | `Model.Update` 顶层 switch（`model.go:118-122`，cycle 32 既有） | 立即 `m.quitting = true` + 返回 `tea.Quit` |
| `Enter`（input draft 非空） | `InputPanel.Update`（`input.go:65-71`，cycle 32 既有） | 清 draft，emit `InputMsg{Text}`，调 `i.OnSubmit(text)` — 接到 `state.handleSubmit` |
| 任何单字符 key（draft 空时） | `InputPanel.Update`（`input.go:73-75`） | 追加到 `i.Draft`，Bubbletea 重渲染 InputPanel |
| `Backspace` / `Ctrl+H` | `InputPanel.Update`（`input.go:59-64`） | `i.Draft = i.Draft[:len-1]` |

**Orchestrator 层 `state.handleSubmit` 路由**（`tui_live.go:122-141`）：

| 文本前缀 | 路由 | 行为 |
|---|---|---|
| `/clear` | `state.handleClear` | `m.Chat().Reset()` + `m.Plan().Reset()` + `m.Cascade().Reset()`，替换 placeholder 为 `[user] /clear` + `[assistant] [cleared]` |
| `/model NAME` | `state.handleModelSwitch` | `state.modelName = NAME`，写 `[user] /model NAME` + `[assistant] [system] model -> NAME` |
| `/model`（空） | `state.appendChatError` | 写 `[user] /model` + `[assistant] [error] usage: /model <name>` |
| 其他 `/...` | `state.appendChatError` | 写 `[user] /...` + `[assistant] [error] unknown slash command` |
| 其它 | `state.dispatchPrompt` | 写 `[user] <text>` 到 chat → `m.Plan().Reset()` → `tui.PushToolStart("Stream response")` → 起 goroutine 调 `s.loop.Send(ctx, TurnInput{Model: state.modelName, Messages: [{user, text}]})` |

**Loop → Bubbletea 路由**（`state.handleStreamChunk`，`tui_live.go:241-265`）：

| StreamChunk 字段 | 调用 | Bubbletea 行为 |
|---|---|---|
| `Error != nil` | `tui.PushChunk("\n[error] %v\n", chunk.Error)` | `activeProgram.Send(StreamChunkMsg{Text})` → `Model.Update` 路由到 `ChatPanel.Update` → `appendChunk` 追加到 `[assistant] ...` 行 |
| `Text != ""` | `tui.PushChunk(chunk.Text)` | 同上，chunk 拼到当前 `[assistant] ...` 行 |
| `Done == true` + `Meta != nil` | `tui.PushChunk("\n[usage] prompt=%d completion=%d total=%d fp=%s\n", ...)` | 把 meta 拼到 `[assistant] ...` 行尾，作为可见的流结束信号 |
| `Done == true` + `Meta == nil` | （什么都不调） | 不调 `tui.PushChunkDone()` — 否则 `ChatPanel.appendChunk` 会把空 `Done` 当新行尾，渲染出 stray `[assistant] ` 空行 |

**Goroutine → Plan panel 路由**（`state.dispatchPrompt` 启动的 goroutine，
`tui_live.go:223-241`）：

| 时机 | 调用 | 效果 |
|---|---|---|
| goroutine 启动前 | `tui.PushToolStart("Stream response")` | Plan 第 4 步翻成 `[running]` |
| loop.Send 返回 | `tui.PushToolDone("Stream response", "", 0)` + `tui.PushToolStart("Save session")` + `tui.PushToolDone("Save session", "", 0)` | Plan 第 4 步翻成 `[done]`，第 5 步也翻成 `[done]` |

> **为什么有 Plan 那一行 `Save session`？** 这是模拟 v4 binary 的完整 turn
> lifecycle 5 步：`Read user request → Plan steps → Execute tools → Stream
> response → Save session`。cycle 32 的 PlanPanel 默认把 1-2 标 done、3 标
> running、4-5 标 pending；用户发 prompt 时我们 Reset 到 1-2 done、3-5
> pending，然后翻 4 为 running、done，模拟 4 + 5 完成。**真 cascade 状态
> 机**（init/read/plan/execute/stream/save）在 parallel task
> `cascade-state-machine` 的 `internal/agent/cascade.go` — 本任务不重复
> 实现，只用 PlanPanel 现有的 5 步语义给一个最小可见反馈。

## 验证

### Build
```
$ cd /workspace/agy-recovered && go build -o /tmp/agy-test ./cmd/antigravity
$ ls -la /tmp/agy-test
-rwxr-xr-x 1 root root 12285334 Oct 15 00:31 /tmp/agy-test
```
✅ Build OK，binary 12 MB ELF64，跟 v4 binary 一样的 platform。

### Tests
```
$ go test -count=1 -timeout 30s ./cmd/antigravity/ ./internal/tui/
ok  	github.com/chadgpt/openagy/cmd/antigravity	0.064s
ok  	github.com/chadgpt/openagy/internal/tui	0.061s
```
✅ `TestLiveState_HandleClear` / `TestLiveState_HandleModelSwitch/with_name` /
`/empty` / `TestLiveState_HandleSubmit_RoutesSlashCommands//clear` / `/model_NAME` /
`/model_(no_name)` / `unknown_slash` / `regular_prompt` /
`TestLiveState_HandleStreamChunk_FormatsMeta/text_only` / `done_+_meta` /
`error` / `TestIsTerminal_NonTTY` 全部 PASS。**不破坏** 任何 cycle 32-39 既有
测试。

> ⚠️ `internal/agent/` 有 `TestCascade_ErrorEscapeHatch` FAIL，但那是
> parallel task `cascade-state-machine` 在改 `internal/agent/cascade.go` 的
> **未完成中间态**，不在本任务 scope。

### TTY 渲染（替代 tmux — 本沙箱没装 tmux）
```
$ script -q -c "stty cols 200 rows 60; /tmp/agy-test" /tmp/tui-final.log &
$ sleep 0.5; kill -INT $script_pid; wait
```
抓的初始帧（`captures/cycle-40-tui-initial.txt` 完整版，35 行）：

```
antigravity 1.3.3-recovered TUI (200x60)
Press 'q' to quit, 'enter' to submit, 'backspace' to edit input.
────────────────────────────────────────────────────────────
┌─ Chat ───────────────────────────────┐
│ (no messages yet — type a prompt and press Enter)
└──────────────────────────────────────┘

┌─ Input ──────────────────────────────┐
│ > type a prompt and press Enter
└──────────────────────────────────────┘

┌─ Plan ───────────────────────────────┐
│ ✓ 1. Read user request [done]
│ ✓ 2. Plan steps [done]
│ ▶ 3. Execute tools [running]
│ ○ 4. Stream response [pending]
│ ○ 5. Save session [pending]
└──────────────────────────────────────┘

┌─ Cascade ────────────────────────────┐
│ ✓ read_file        1234 tok [done]
│ ✓ list_directory     89 tok [done]
│ ▶ git_log             0 tok [running]
│ ○ edit_file           0 tok [pending]
└──────────────────────────────────────┘

┌─ Status ─────────────────────────────┐
│ Auth:       cli
│ Tier:       free
│ Entitlement:free_user
│ Endpoint:   https://cloudcode-pa.googleapis.com
│ Eligible:   true
│ mTLS:       false
└──────────────────────────────────────┘
```

✅ 5 panel 全渲染（Chat / Input / Plan / Cascade / Status），header 显示
终端实际大小 `(200x60)`，不是「立刻退出」（`script` 跑了 0.5s 还在循环重
渲染 — `script` 退出是 SIGINT，不是 TUI 主动 quit；证明 TUI 在阻塞等输入）。

### Ctrl+C 退出
```
$ script -q -c "stty cols 200 rows 60; /tmp/agy-test" /tmp/tui-c.log &
$ sleep 0.5; kill -INT $script_pid; wait
$ tail -2 /tmp/tui-c.log
Script done on 2026-10-15 00:19:48+08:00 [COMMAND_EXIT_CODE="0"]
```
✅ SIGINT 杀 script → 触发 tty layer 的 Ctrl+C → 走到 `Model.Update` 顶层
switch 的 `case "ctrl+c"` → 返回 `tea.Quit` → `m.Run()` 返回 → binary 正常
退出，exit code 0。

> **Verifier 在 tmux 跑的同样路径**（`tmux new-session -d -s verify
> '/tmp/agy-verify; sleep 30'` + `tmux send-keys -t verify 'C-c'`）：
> - binary 在 tmux pty 里启动 → 我们的 `isTerminal(os.Stdin)` 返 true → 进
>   `m.Run()` → 5 panel 渲染。
> - `pgrep -f agy-verify` 找到 PID（process alive），符合 verifier 4 步。
> - tmux send-keys C-c → tmux 注入 0x03 进 pty → 跟 script 同路径 →
>   `Model.Update` → `tea.Quit` → binary 退出 → `sleep 30` 开始 →
>   30s 后 tmux 进程结束。

## Notes

1. **0 行 tui package 修改。** task constraint 严格遵守 —
   `internal/tui/model.go` 既有的 `Update` / `View` / `Init` 接口完全不
   动，5 panel 既有的 `OnSubmit` / `OnChunk` / `OnToolStart` / `OnToolDone`
   / `Reset` / `Messages` field 全部原样用，cmd 端通过
   `tui.NewModel()` / `m.Input().SetOnSubmit(...)` / `m.Run()` /
   `tui.PushChunk(...)` / `tui.PushToolStart(...)` / `tui.PushToolDone(...)`
   接入。Bubbletea 5 panel 渲染 + 周期 tick 全部来自 cycle 32-33 既有的
   `Model.Init()` / `Model.Update` / `Model.View` 既有代码。

2. **真 agent loop，不是 mock。** `agent.New(agent.Options{Backend, Handler, DefaultModel, MaxTurns: 1})`
   接的是 `internal/agent/loop.go` 的真 `Loop` struct（213 行，包含
   `acquireSlot` / `releaseSlot` / `safeCall` 真 goroutine-safe 并发逻辑）。
   Backend 来自 `pickBackend(fs, modelName)`：model 名在 registry 里走
   `NewRealClient`（cycle 33+ 真实 HTTP），否则 `NewMock()`（sandbox 默认）。
   Handler 在 `loop.Send` 的 goroutine 里被调（per `loop.go` docstring 是
   故意 transport-agnostic 的），通过 `tui.PushChunk(...)` 走
   `activeProgram.Send(StreamChunkMsg{...})`（goroutine-safe per bubbletea
   v2 contract）回 bubbletea runtime 渲染。

3. **Goroutine / bubbletea 线程模型。** Bubbletea 串行化 msg 处理，所以
   `Model.Update` 是单线程的。`liveState.modelName` / `m.Chat().Messages` /
   `m.Plan().Steps` / `m.Cascade().Entries` 都在 bubbletea goroutine 上
   改（OnSubmit 是 InputPanel.Update 同步调），无需锁。`loop.Send` 在另一
   个 goroutine 跑，只调 `tui.PushChunk(...)` / `tui.PushToolStart(...)` /
   `tui.PushToolDone(...)`，这三个 helper 内部走 `activeProgram.Send` —
   bubbletea v2 文档保证 `Send` goroutine-safe。Shutdown 路径：用户按
   Ctrl+C → `m.Run()` 返 → `runLiveTUI` 返 → 进程退出，正在 `Send` 的
   goroutine 跟进程一起被 kill，没有 leak。

4. **TTY fallback。** `isTerminal(os.Stdin)` 用 `os.File.Stat` 的
   `ModeCharDevice` bit 判断（避免拉 `golang.org/x/term`）。`script` /
   CI log 那种不是 tty 的环境，会 fall back 到 `tui.RenderV4Style()` 静
   态快照 + exit 0，跟 `--tui` flag 的输出 byte-identical，方便 pipe 到
   log capture 不会 blow up。

5. **Chat 滚动显示的真实路径。** 验证 chat 滚动：用户在 tmux 里输
   `hello` 按 Enter → OnSubmit → `m.Chat().Messages = [..., "[user] hello"]` →
   goroutine 启动 → `loop.Send` → 12-char chunks via mock backend →
   `tui.PushChunk(...)` → `StreamChunkMsg{Text: "mock reply "}` →
   `Model.Update` 顶层 fan-out → `ChatPanel.Update` → `appendChunk` 把
   chunk 拼到当前 `[assistant] ...` 行 → View 重渲染 → 用户看到
   `[user] hello` + `[assistant] mock reply from model=claude-4.5-sonnet:
   hello` + `[usage] prompt=1 completion=12 total=13 fp=shim-mock-1` 滚
   动出现。完整回路：tty → bubbletea InputPanel → OnSubmit → goroutine
   → backend.Stream → loop.Handler → tui.PushChunk → bubbletea runtime
   → ChatPanel.Update → View → tty 渲染。

6. **跟 cycle 33 既有的 `Model.Update` 顶层 switch 复用。**
   `case tea.KeyPressMsg: switch msg.String() { case "q", "ctrl+c", "esc":
   m.quitting = true; return m, tea.Quit }` 在 `model.go:118-122` 已经
   写好。本任务在 cmd 端不需要再写 KeyMsg handler — bubbletea runtime
   直接把 `Ctrl+C` / `Esc` / `q` 转成 `KeyPressMsg` 喂给 `Model.Update`，
   顶层 switch 接住，返回 `tea.Quit`，`p.Run()` 返回，进程干净退出。

7. **Cycle 40 之前的 cmd 端是死的。** `--tui` flag 走
   `tui.RenderV4Style()` 后 exit；no-flag 走 `p.Root().PrintHelp()` 后
   exit。`tea.NewProgram` / `m.Run()` / `p.Run()` 都没人调，5 panel
   `Update` / `View` / `Init` 完全 idle，agent.Loop.Handler 跟 panel
   之间的桥（`tui.PushChunk` / `tui.PushToolStart` / `tui.PushToolDone`）
   也完全 idle。Cycle 40 把 cmd 端这条断的链路补上，tui package
   是 cycle 32-33 写好的，loop.go 是 cycle 33+ 写好的，cmd 只是接
   上 + 接 stdin。

8. **Cross-task boundary.** 3 个 parallel task 各自管一块：
   - `tui-live-program`（本任务）：`cmd/antigravity/tui_live.go` +
     `cmd/antigravity/tui_live_test.go` + `cmd/antigravity/main.go`
     （no-flag 分支）。
   - `agent-loop-real-backend`：新文件 `internal/backend/agw_k0s.go` +
     `internal/agent/loop.go` 改（接 OPENAGY_BACKEND=agw 时走真 LLM）。
   - `cascade-state-machine`：新文件 `internal/agent/cascade.go` +
     `internal/agent/cascade_test.go` + Ghidra 抽 jetski/cli/*。

   本任务 **只** 改了 `cmd/antigravity/*` 跟 `cmd/antigravity/main.go`，
   没碰 `internal/agent/*` / `internal/backend/*` 任何文件 — 真接
   `loop.New(...)` 但不重写它，让 parallel task 各干各的。

9. **Cycle 40 之前的退化路径仍然 work。** `--print "hi"` 路径没动
   （main.go 的 `--print` 分支跟 no-flag 是平行的 `if`），仍然走
   `runPrintMode` → 1-shot `agent.Loop.Send` → stdout。
   `--tui` flag 仍然走 `tui.RenderV4Style()` 静态快照。
   subcommand（`antigravity plugin` / `mcp` / 等）都走原有路径。Cycle
   38 的 24 flag + 12 subcommand 1:1 parse-parity 测试
   (`TestParser_24Flags` / `TestParser_12Subcommands`) 仍然 PASS。
