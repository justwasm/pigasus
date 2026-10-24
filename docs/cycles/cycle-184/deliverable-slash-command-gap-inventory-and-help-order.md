# Cycle 184 — slash/TUI 差距盘点 + `/help` COMMANDS 排序修正

## Why

用户要求：把 `agy` 与 `wagyu` 在 **TUI / slash 命令** 上所有能探到的差异逐条探完、
写进 `TODO.md`，然后开始关掉它们。cycle 39 时代的 `TODO.md`（Ghidra 抽取笔记）
已经过期，整个文件重写。

本 cycle 交付两件东西：

1. **`TODO.md` —— 一份带实测证据的 gap 清单**（37 条本地命令逐条对照 + 5 类归因）。
2. **第一条真正关掉的 gap**：`/help` 的 COMMANDS 页排序。

## 1. 探针方法

REF = `~/.local/bin/agy` v1.4.0（185,665,248 B）。全部在 tmux 里以 `220×30` 驱动，
`capture-pane -p` / `-p -e` 抓屏。脚本落在 `/tmp/agycmp/`（只读，不入库）：

- `panel_probe.py` —— 每条命令开**新 session**，敲 `/cmd` + Enter，抓原始 SGR。
- `esc_probe.py` —— 抓 Esc 退出面板后 transcript 里留下的内容。
- `dump_all.py` —— 产出 37 条命令的 REF/OUR 对照 `ALL.txt`。

三个非显然的实现约束（都踩过，见 `TODO.md` §0）：

1. **必须用私有 tmux socket**。另一个 agent 在同一台机器上跑
   `tmux kill-server`，会连坐杀掉探针 session（第一次 sweep 因此丢了 16 个抓取，
   重跑才补齐）。
2. **每次命令重启 session**。REF 的面板 Esc 不一定关得掉（`/model` 的 Esc
   先清 search），复用 session 会让下一条命令敲进上一条命令的搜索框。
3. **tmux 会把 24-bit RGB 降级成 256 色**（`38;2;66;133;244` → `38;5;69`）。
   所以抓取只能判到 256 索引，不能判 REF 原发的是 truecolor 还是 index——
   实现面板颜色时沿用仓库既有的 24-bit 常量（`cBlueOpen` 等），不另造。

## 2. 盘点结论

37 条本地命令里，**每条都有差距**。归成 5 类（`TODO.md` §1）：

| 类 | 含义 | 条数 |
|---|---|---|
| A | REF 开整屏面板，OUR 只打一行 | 20 |
| B | REF 留 `> /cmd` + `  ⎿  <msg>`，OUR 只留 `<msg>` | ~25 |
| C | 两边都打一行，但文案不同 | 8 |
| D | REF 做的是别的事（发 prompt / 开 vim） | 7 |
| E | 排序 | 1 |

两条最值得记的发现：

### 2.1 类 B 是与 cycle 149 结论冲突的

`slash_commands.go` 里 cycle 149 的注释写着「v1.3.6 的 REF 不 echo slash 命令」，
依据是 `/changelog` 与 `/help` 的抓取。但这两个都是**全屏面板**，面板盖住
transcript，所以看不到 echo。v1.4.0 实测 `/rename` `/title` `/statusline` `/copy`
`/fork` `/btw` `/add-dir` `/open` `/remote-control` `/voice` **全都有** echo，
格式逐字节为：

```
\x1b[1m\x1b[38;5;26m> /rename\x1b[0m
\x1b[1m\x1b[38;5;26m  ⎿  \x1b[0m\x1b[38;5;238mError: Please provide a new name for the conversation.\x1b[39m
```

（后接一个空行。）**cycle 149 的结论需要推翻。** 但这条改动要动
`tui_run.go` + `slash_commands.go` 的渲染路径，而 `slash_commands.go` 正被另一个
agent 占用，所以本次不落地，列为 P0。

### 2.2 `/keybindings` 根本不是打表格

REF 的 `/keybindings` 用 `$EDITOR` 打开 `~/.gemini/antigravity-cli/keybindings.json`
——抓取里留下的是 vim 的状态行 `"…keybindings.json" 39L, 1360B`。OUR 打了一张
11 行的自造表格（`slash_helpers.go:printSlashKeybindings`）。这条从头到尾是错的。

## 3. 本次关掉的：`/help` COMMANDS 排序

REF 的两个列表用**不同的顺序**：

| 面 | 顺序 | 证据 |
|---|---|---|
| `/` picker | 注册序（内置命令先，skills 后） | `hotpath/picker/ref.000-slash.txt`：第 2/3 行 `/agents`、`/artifact` |
| `/help` COMMANDS | 整目录按名字**字节升序** | `ref_catalog.txt`：第 2/3 行 `/agents`、`/agents-sdk` |

OUR 之前两个面都用 `tuilib.DefaultSlashCommands` 的注册序，所以 `/help` 的
COMMANDS 页顺序是错的（`/goal` `/compact` `/schedule` `/browser` `/plan`
`/grill-me` `/learn` 被排在 `/voice` 之后）。

### 改动

- `internal/tuilib/commands.go`：新增 `SortedSlashCommands()`，返回按 `Name`
  字节升序的副本。`DefaultSlashCommands` 保持原样，picker 不受影响。
- `cmd/wagyu/tui_run.go`：`renderHelpCommands` 改用 `tuilib.SortedSlashCommands()`。
- `internal/tuilib/commands_order_test.go`（新）：4 个测试——
  头 12 行与 REF 抓取逐条对齐、整体升序、不修改 `DefaultSlashCommands`、
  两个顺序确实不同（picker 第 2 行 `/artifact`，help 第 2 行 `/agents-sdk`）。

排序键用存储的 `Name`（即显示形式 `/clear (new)` 的 `/clear`），与 REF 一致。

## 4. 验证

```
go build ./cmd/... ./internal/...                       # ok
go test ./internal/tuilib/ ./cmd/wagyu/                 # ok
```

`gofmt -l` 仍报 `internal/tuilib/commands.go` 与 `cmd/wagyu/tui_run.go`——**两者在
HEAD 上就已经不干净**（`git show HEAD:<f> | gofmt -d` 非空），本次新增的行不在
gofmt 的 diff hunk 里，按仓库既有约定不做全文件重排。

## 5. 未做的（诚实清单）

- **没有实现任何一个 REF 面板**。`/model` `/config` `/permissions` `/skills`
  `/plugin` `/usage` `/context` `/rewind` `/artifact` `/effort` `/hooks` `/agents`
  `/credits` `/mcp` `/tasks` `/diff` `/codesearch` `/feedback` `/changelog` 全都
  还是文本输出。规格已逐条记录在 `TODO.md` §2.1/§3，留给后续 cycle。
- **没有改 echo 格式**（P0）：`cmd/wagyu/slash_commands.go` 与
  `cmd/wagyu/tui_run.go` 在本 cycle 期间被另一个 agent 持有未提交改动
  （`/resume` picker 的 `resumeTab`/`resumeQuery`/`resumeWindow` 状态 + Update
  分支重写）。为避免把对方的 WIP 一起提交，`tui_run.go` 的改动只用
  `git apply --cached` 单独入索引，**没有**动工作区。
- **面板内的按键基本没测**：只测了 Esc 与 Tab。`←/→ Change`、`enter Save`、
  `ctrl+s Uninstall`、`space Toggle Enable` 都没实测。
- **60 条 skill 命令没测**（`/science:*` 等）；归为「同 `/goal` 的 prompt 型」是
  【推测】。
- **配色没从二进制求证**：面板颜色按仓库既有约定映射到 24-bit（见 `TODO.md` §6）。
- `/skills` 的条目数差异（REF 63 项 vs OUR 的硬编码快照）未解决——REF 是**读磁盘**的。

## 6. 与并发 agent 的边界

本 cycle 只触碰三个文件：`TODO.md`、`internal/tuilib/commands.go`、
`internal/tuilib/commands_order_test.go`，以及 `cmd/wagyu/tui_run.go` 的**一行**。
另一个 agent 同时在改 `cmd/wagyu/{slash_commands,picker_view}.go`、
`internal/auth/compat.go`、`internal/convstore/{agy,list}.go` —— 零文件重叠
（`tui_run.go` 上双方改动位于不同区域，且对方的改动留在工作区未提交）。
