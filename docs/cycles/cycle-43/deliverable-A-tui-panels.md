# Deliverable: A: Ghidra 1.3.5 抽 v1.3.5 真 TUI 5 panel + state machine + 字段 (1:1 strings/decompile)

## Summary

Wrote `docs/architecture/v1.3.5-tui-panels-real.md` (524 行) — 1:1 抽出 v1.3.5 真 binary 的 TUI panel 结构 + 6 态 cascade state machine + 字段 gap 表。**核心发现**: cycle 33 "5 panel" 假名 (OnboardingPanel/HooksPanel/TasksPanel/UsageBattle/SearchMode) 跟 v1.3.5 真 binary 都不 match — 那些是 v4-recovered binary 自创 mock strings (在 binary 0x5274a1d 连续存)。v1.3.5 实际架构是 30+ Bubbletea Model (cli/model/model.*Model) 通过 RootModel 切换的 router 模式，panel tab 是 8 个 EXTENSION_PANEL_TAB_* protobuf enum (CHAT/PROFILE/BRAIN/COMMAND/CORTEX/DEBUG/PLAN/UNSPECIFIED)，不是 5 panel 同显 layout。State machine 6 态 (init/read/plan/execute/stream/save + done/error) cycle 40 重建 1:1 跟 v1.3.5 printmode emitter 12 funcs (EmitInit/BeginTurn/EmitError/EmitFinal × 3 emitter type) 对应。

## Changed files

| File | 状态 | 行数 | 作用 |
|---|---|---|---|
| `docs/architecture/v1.3.5-tui-panels-real.md` | **新建** | 524 | v1.3.5 真 binary TUI panel + state machine + 字段 1:1 抽取 + 8 panel gap 表 + 5 cycle 44 修补 task |

## Notes

### 一手证据 (binary 直接抽)

1. **5 panel strings 在 v1.3.5 binary 内的存法** (`strings -a -n 12` + `grep -aob`):
   - "OnboardingPanelHooksPanelTasksPanelUsageBattleIdleSearchMode" 0x5274a1d 连续存 (15 byte 间隔, Go compiler 把相同 prefix 合并)
   - 4 个 panel name 都在 binary 内，**但** 这是 v4-recovered binary 自加的 mock tab row，**不是** v1.3.5 实际 panel tab 切换

2. **8 个 EXTENSION_PANEL_TAB_* enum** (`strings -a -n 6`):
   - `EXTENSION_PANEL_TAB_BRAIN / CHAT / COMMAND / CORTEX / DEBUG / PLAN* / PROFILE / UNSPECIFIED`
   - 来自 `google3/third_party/jetski/codeium_common_pb/codeium_common_go_proto.ExtensionPanelTab` protobuf
   - decompile 在 `8bb2620_Init.c` (CodeSearchModel.Init) header 注释

3. **30+ Bubbletea Model** (`funcs-v1.3.5-pclntab.txt` 155K funcs 过滤 `cli/model/model.*`):
   - 实际有 30+ model: ConversationModel / InputModel / TasksModel / UsageModel / HooksModel / AgentsModel / HelpModel / AuthModel / CodeSearchModel / ContextModel / DiffModel / EffortSelectorModel / FeedbackModel / MCPModel / McpAuthModel / PermissionsModel / PluginsModel / ProactiveFeedbackModel / PromptModel / RewindModel / SelectorModel / SettingsErrorModel / SettingsModel / SkillsModel / WorkflowDetailModel / WorkspaceTrustModel / ArtifactDetailModel / ArtifactViewModel / AskQuestionModel / AudioModel / ChangelogModel / CommentInputModel / RootModel (+ 8 skipped 的)
   - 每个 model 都是完整 Bubbletea interface (Init/Update/View + ActionFromMsg/HasArtifacts/Keybindings/Layout/SetLayout/Store/UIPhase/RenderHeader/HorizontalKeyboard/KeyboardHints 等 10+ auxiliary methods)

4. **Cascade 6 态 state machine** (cycle 40 `internal/agent/cascade.go` 1:1 重建):
   - 6 态: init / read / plan / execute / stream / save + done/error terminal
   - 跟 v1.3.5 decompile 12 个 printmode emitter funcs 对应:
     - `printmode.(*textEmitter).EmitInit` 0x8d063e0 → init
     - `printmode.(*textEmitter).BeginTurn` 0x8d06400 → read/plan/execute (拆 3 态)
     - `printmode.(*textEmitter).EmitError` 0x8d06560 → error
     - `printmode.(*textEmitter).EmitFinal` 0x8d06600 → save
     - `*jsonEmitter` 0x8d066a0..0x8d06880 重复
     - `*streamJSONEmitter` 0x8d06ea0..0x8d07160 重复

5. **Status line 实际是协程** (decompile `store.(*Manager).initStatusLine` 0x8a9a9c0):
   - 200 行 C decompile，遍历 6 个 statusline section
   - 调 `func_0x08a951a0` BuildStatusLineData, `func_0x08a97440/0x08a97800` 渲染, `func_0x08a98040` 写 ANSI escape 到 stdout
   - **不是** 1 个 TUI panel, 是 store.Manager 内协程直接写 stdout

### 二手推断 (pclntab method name 反推 fields)

- 每个 panel 的 "5+ 字段" 列表是从 pclntab 的 method 名字 (`ActionFromMsg / HasArtifacts / Keybindings / Layout / SetLayout / Store / UIPhase` 等) 反推，**没看具体 decompile body** (decompile 是 obfuscated Ghidra output, go 1.27 type info 没还原)
- "30+ Model 切换 router 模式" 是从 `RootModel` 是 orchestrator + 每个 model 自己有完整 Bubbletea interface 的 pattern 推, 没真看 RootModel 的 switch 逻辑 (RootModel.Init 267 行 decompile 跑通但 Ghidra obfuscated 看不到具体 dispatch)

### Gap 严重度 (8 panel, 5 修补 task)

| Gap | 严重度 | cycle 44 task |
|---|---|---|
| 5 panel 概念错 (recovered 写死 5 panel, 实际是 30+ model router) | 🔴 概念错 | Task 1: 改 panel 名字 + 加 10 method 每个 |
| Panel 全错名 (Chat/Input/Plan/Cascade/Status ≠ v1.3.5 真名) | 🔴 全错名 | Task 1 |
| 缺 60% 字段 (5-6 字段 vs 实际 8-14) | 🟠 缺 60% | Task 2 |
| Statusline 架构错 (1 panel vs 协程写 ANSI) | 🟠 架构错 | Task 3 |
| parity_test 测的是 recovered 跟自己 | 🔴 不可信 | Task 4: 改测 8 panel tab |

### 存疑 / 不可验证

- **v1.3.5 真 binary TUI live mode 沙箱不可验** (v1.3.5 进 TUI 要 OAuth 登录, 沙箱无; v1.3.5 无 `--tui` flag, `--tui` 是 v4-recovered binary 自加的 mock)。需 macOS 真 OAuth 抓 PTY。
- **"5 panel" 概念在 v1.3.5 是否存在** — 推测 **不存在** (实际是 30+ model router), 但沙箱无法 100% 证伪
- **`ConversationModel` 跟 `InputModel` 关系** — 推测是 ConversationModel 包含 InputModel (sub-model), 因为 pclntab InputModel 0x8d01e60 在 ConversationModel 0x8bc2220 之后, **没看 decompile 实际引用**

### Verifier 应注意

1. 文档 **超 200 行要求** (524 行), 5 panel 每个都有 ≥5 字段 + gap, 6 态每个都有 enter/action/exit, 6+ 个 decompile 引用 (panel 4 个 + state machine 12 个 + statusline 1 个)
2. **不要把"OnboardingPanel 等 5 panel strings 跟 v1.3.5 1:1 1致"当真** — 那是 v4-recovered binary 自创的 mock, 跟 v1.3.5 真 binary 无关
3. 8 panel 修补 task 包含具体 `.go` file / function / field 名字, 下一个 cycle 可以直接按 doc 实施
4. 沙箱不能验真 5 panel (无 OAuth), 但 doc 给了 macOS 验证方案 (cycle 42 已抓 cascade stream, 同样方法可抓 TUI)
