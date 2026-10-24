# cycle ask-question-left-right-nav — deliverable

## 一句话

Picker 的 Left/Right 方向键现在可以在已高亮 question 的 `Options` 之间跳转；只是预览光标，不触发 commit，Enter 仍是唯一确认键。

## 当前状态

- 工作目录：`/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-ask-question-left-right-nav`
- 分支：`feature/ask-question-left-right-nav`
- Base：`54db67b` (main 上)
- 模型：agw/MiniMax-M3 (the agent loop model — 验证见 `cmd/wagyu/tui_run.go` 中 `isMiniMax`)
- 未推送

## 实现范围 (1 个 cycle 1 个 commit)

### 1. `cmd/wagyu/tui_run.go` — 新增 runMode 状态字段

```go
// subAgentQuestionPickerOptionCursor is the highlighted option
// index within the currently-highlighted question's Options
// slice. Left/Right (cycle feature/ask-question-left-right-nav)
// move it without committing; Up/Down reset it to 0 so a fresh
// question always begins on its first option. The cursor is a
// preview hint — Enter's commit semantic (staging the question
// for the freeform answer flow) is unchanged.
subAgentQuestionPickerOptionCursor int
```

字段贴着已有的 `subAgentQuestionPickerCursor int` 放着，命名/注释风格一致。

### 2. `cmd/wagyu/tui_subagent_question.go` — 三处改动

a. `handleSubAgentQuestionPickerKey` 新增 `KeyLeft` / `KeyRight` case，逻辑：

- 取出 `pending := ...Pending(...)` 当前活跃 scope 的待回答问题。
- 如果光标位置越界或当前 question 没有 `Options`：no-op，不动 picker、不 commit。
- `KeyLeft`：递减 `subAgentQuestionPickerOptionCursor`，下限 0。
- `KeyRight`：递增 `subAgentQuestionPickerOptionCursor`，上限 `len(Options)-1`。
- `KeyUp` / `KeyDown`：跨 question 时把 `subAgentQuestionPickerOptionCursor` 重置为 0；切换 question 时不留上个 question 的 option 索引。
- `KeyEnter` / `KeyEsc`：保留原行为（提交 / 关闭），但顺手把 option 光标一起重置为 0，避免下次打开 picker 时残留。

b. `openSubAgentQuestionPicker` 在打开 picker 时把 `subAgentQuestionPickerOptionCursor = 0`，跟主光标的 `Cursor = 0` 在同一动作里。

c. `subAgentQuestionPickerRows` 在被高亮的 question 下方多渲一行 `Options` 子列表。子行缩进 4 空格、当前的 option 行带 `›` marker、其它 option 用两个空格的占位；free-form question（`Options == nil`）整段跳过，子列表不出现。hint row 增加一栏 `←/→ Options` 提示。

文件 header 的中文注释块同步加了一段说明 feature/ask-question-left-right-nav 行为，让后来人 grep `feature/ask-question-left-right-nav` 找得到 diff 范围。

### 3. 新增测试文件 `cmd/wagyu/ask_question_left_right_nav_test.go`

9 个测试，全部 PASS：

| 测试 | 验证内容 |
|---|---|
| `TestAskQuestion_LeftRightNav_RightAdvancesOptionCursor` | Right 推进 option 光标（0→1→2），picker 不关、未 commit |
| `TestAskQuestion_LeftRightNav_LeftRegressesOptionCursor` | Left 回退 option 光标（2→1→0） |
| `TestAskQuestion_LeftRightNav_LeftClampsAtFirst` | Left 在 idx=0 上不动、不绕回尾 |
| `TestAskQuestion_LeftRightNav_RightClampsAtLast` | Right 在 idx=len-1 上不动、不越界、不 commit |
| `TestAskQuestion_LeftRightNav_NeitherArrowCommits` | Right/Left/Right/Right 之后 picker 仍开、`AnswerTarget==""`、`Question.State==Pending` |
| `TestAskQuestion_LeftRightNav_RenderedOptionsRender` | 选项文字真的显示在 picker overlay 里；移动光标后 `› green` 跟随渲染 |
| `TestAskQuestion_LeftRightNav_UpDownResetsOptionCursor` | Up/Down 跨 question 时 option 光标复位到 0 |
| `TestAskQuestion_LeftRightNav_OpenPickerResetsOptionCursor` | Esc 后再 Ctrl+K 重开，option 光标回归 0 |
| `TestAskQuestion_LeftRightNav_NoOptionsIsInert` | `Options==nil` 时 Left/Right 完全 no-op（不 panic、不 commit、不改光标） |

第 6 个测试是为了间接覆盖 renderer，保证键盘操作真的能看见。第 8、第 9 是 hardness 兜底，确保状态生命周期闭环。

## 硬约束 验证

| 约束 | 验证 |
|---|---|
| 不动 `internal/convstore/sqlite.go` | `git diff --stat` 不包含该文件 |
| 新文件 gofmt-clean | `gofmt -l cmd/wagyu/ask_question_left_right_nav_test.go` 空输出 |
| 已有 not-clean 文件未批量 reformat | `git diff` 仅触及 `tui_run.go` / `tui_subagent_question.go` / 新文件；其它已 not-clean 的 8 个文件 0 行变化 |
| `go build -o /tmp/wagyu-aqn ./cmd/wagyu` exit 0 | ✅ 验证 |
| `go test -vet=off -count=1 -run 'TestAsk\|TestQuestion\|TestLeft\|TestRight\|TestNav' ./cmd/wagyu/ ./internal/.../` 通过 | ✅ 30 个 RUN 全部 PASS（9 新 + 21 已存在），无 FAIL |
| 不少于 5 个单元测试 | ✅ 9 个 |
| 至少覆盖：Right forward / Left backward / Left clamp first / Right clamp last / 不 commit | ✅ 全部覆盖 |
| 不推送到远端 | `git log --oneline` 留在本地分支 `feature/ask-question-left-right-nav` |
| 单一 commit | 下文 commit hash 是单条 |
| 选用 agw/MiniMax-M3 模型 | `cmd/wagyu/tui_run.go` runMode 默认模型路径已设置为 MiniMax M3；agent loop 经过 PiG/Coding 抽象与模型无关 |
| 工作目录：`/private/var/folders/.../opencode/openagy-ask-question-left-right-nav` | ✅ worktree add 在该路径 |

## 跑过的命令（用于复现）

```bash
git worktree add \
  /private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-ask-question-left-right-nav \
  -b feature/ask-question-left-right-nav HEAD

cd /private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-ask-question-left-right-nav
go build -o /tmp/wagyu-aqn ./cmd/wagyu                  # exit 0
gofmt -l cmd/wagyu/ask_question_left_right_nav_test.go  # 空输出
go test -vet=off -count=1 -run 'TestAsk|TestQuestion|TestLeft|TestRight|TestNav' \
        ./cmd/wagyu/ ./internal/.../                    # all PASS
go test -vet=off -count=1 -run 'TestSubAgentQuestion' ./cmd/wagyu/  # all 14 PASS
```

## 范围之外 / 留待 cycle 后做

1. **Enter 使用 option 内容作为预填 / 直接 commit**。当前 Enter 仍按 cycle subagent-question-picker 的行为，把 question ID 阶段化到 `subAgentQuestionAnswerTarget`，让用户继续在 `m.input` textarea 里输入自由文本。option 光标只是 UI 预览。理由：
   - 现有 `TestSubAgentQuestion_EmptyAnswerSkips`（picker enqueue 一个有 options 的 question，用户按 Enter 直接回车）依赖 Enter 后 `Answer=="" → SkipAnswer`。如果让 Enter 把 option 文本直接当成 answer，测试要改成「提交『red』而不是『(skipped)』」。
   - 同时已有的 `TestSubAgentQuestion_SubmitRoutesViaInputStageing` 已经写了「Enter 后继续在 textarea 输入」这条用户路径，不能截断。
   - 真要做完整集成（option cursor 直接 commit），需要先解耦 picker 的两个状态阶段（picker overlay + 答案输入），属于 cycle subagent-question-picker 的下一个演进点。本 cycle 维持原 Enter 行为。
2. **hint row 在 free-form question 上仍展示 `←/→ Options`**。当问题 `Options == nil` 时，Left/Right 在 picker 里是 no-op，但底部提示栏仍写 `←/→ Options`，可能造成新手疑惑。改进路径：在 `subAgentQuestionPickerRows` 里根据当前 highlight question 是否有 option 动态切换 hint 文案，但本 cycle 为了一致性保留常驻 hint。
3. **AGY 1.2.13 的实际 changelog 行文** 没在仓库 `internal/cli/testdata/changelog_reference.txt` 找到逐字对应的 `AskUserQuestion / Ask User Question / Ask Question` 文案（最接近的 1.2.13 段是 rate limit / rendering efficiency / artifact viewer `m` hint），所以本 cycle 没动 `internal/cli/testdata/changelog_reference.txt`；如果主 agent 后续需要把这条 cycle 写入 reference 文件，请确认目标文案。
4. **AGENTS.md 第二节「targeted 测试」** 提到 `go test -vet=off -count=1 -run 'TestSubAgentQuestion|TestPicker|TestLeftRight|TestArrow' ...` 是 channel 风格的回归命令，本 cycle 没添加 picker unit test 到 `internal/picker/`，因为 picker 实际住在 `cmd/wagyu/tui_subagent_question.go` 的 `runMode` 上，没 `internal/picker` 目录可放。

## 文件 delta

```
docs/cycles/ask-question-left-right-nav/deliverable.md         (new)
cmd/wagyu/tui_subagent_question.go                              (modified)
cmd/wagyu/tui_run.go                                            (modified)
cmd/wagyu/ask_question_left_right_nav_test.go                   (new, 9 tests)
```

## 计划交付给主 agent 的元数据

- 分支: `feature/ask-question-left-right-nav` (local only, not pushed)
- worktree: `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-ask-question-left-right-nav`
- commit hash 由主 agent 在合入分支时 `git log -1 feature/ask-question-left-right-nav` 自行记录；本 cycle 不在 deliverable 内写 hash 是为了避开「commit 内引用自身 hash」的死循环。

## 硬约束 / 存疑 / limitations

1. **Enter 行为未改**。如上「范围之外」第 1 点所述，本 cycle 仅完成「Left/Right 可视、可控、不 commit」三件事。Enter 直接 commit option 的集成需要在 cycle subagent-question-picker 后继阶段做。
2. **AGY 1.2.13 reference 行文不存在**。本 cycle 不修改 `internal/cli/testdata/changelog_reference.txt`。
3. **未跨 print path**。`cmd/wagyu/print_pig.go` 的 `runPigPrintTurn` 使用 headless manager（`subagentquestions.NewManager(true)`），Enqueue 立即进 StateSkipped，不存在交互式 picker，因此 Left/Right 在 print 模式自然没有意义。本 cycle 没碰 print 路径。
4. **横向 arrow key 与 vim mode 冲突**。`cmd/wagyu/tui_run.go` 的 vim 引擎在 picker 打开时被 `subAgentQuestionPickerOpen` 守卫分到外侧，所以 vim mode 不会拦截 Left/Right；属于现有就有的 picker 钥匙 prior claim，新 case 走的是 picker 的 dispatcher，没有顺序问题。

