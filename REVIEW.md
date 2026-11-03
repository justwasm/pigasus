# OpenAGY / Pigasus 架构设计评审与现代化重构演进路线 (REVIEW.md)

> 本文档针对当前代码库的整体架构、工程哲学、迭代瓶颈以及对 AI Agent 协作范式的友好度进行深度系统性评审，并提出下一阶段的重构演进方案。

---

## 目录
1. [核心矛盾：双重身份的永恒撕扯](#1-核心矛盾双重身份的永恒撕扯)
2. [违背软件工程最佳实践的架构痛点](#2-违背软件工程最佳实践的架构痛点)
   - [2.1 水平技术切片（Tiers）vs 领域内聚缺失（Shotgun Surgery）](#21-水平技术切片tiers-vs-领域内聚缺失shotgun-surgery)
   - [2.2 上帝对象（The runMode God Object）与 Bubble Tea 的伪纯函数困境](#22-上帝对象the-runmode-god-object与-bubble-tea-的伪纯函数困境)
   - [2.3 宿主（Host）与双引擎的行为漂移（Behavioral Drift）](#23-宿主host与双引擎的行为漂移behavioral-drift)
3. [对 AI Agent / 自动化协作极不友好的隐形壁垒](#3-对-ai-agent--自动化协作极不友好的隐形壁垒)
   - [3.1 认知过载：200+ 条不可违背的“承重不变量”](#31-认知过载200-条不可违背的承重不变量)
   - [3.2 工具链基线防御的“反直觉模式”](#32-工具链基线防御的反直觉模式)
   - [3.3 历史推导过程与当前系统事实的界限模糊](#33-历史推导过程与当前系统事实的界限模糊)
4. [现代化 Agent 原生架构演进路线图](#4-现代化-agent-原生架构演进路线图)
   - [4.1 物理隔离“逆向保真适配层”与“现代通用核心”](#41-物理隔离逆向保真适配层与现代通用核心)
   - [4.2 瓦解 runMode 上帝对象，迈向真正的响应式/状态机驱动](#42-瓦解-runmode-上帝对象迈向真正的响应式状态机驱动)
   - [4.3 建立 Agent-First 的开发与质量防护体系](#43-建立-agent-first-的开发与质量防护体系)

---

## 1. 核心矛盾：双重身份的永恒撕扯

当前架构面临的最深层阻力，来自于它**同时承载了两个方向相反的目标使命**：

```
[ 黑盒逆向保真 (Clean-room Binary Parity) ]  <======= 撕扯 =======>  [ 通用多后端 Agent 运行时 (Universal Harness) ]
• 追求字节级一致 (Byte-for-byte parity)                               • 追求抽象、正交、解耦与多协议兼容
• 复刻原厂一切怪癖 (奇数空格、hex em-dash、非对称逻辑)                  • 追求清晰统一的 API 与现代设计模式
• 冻结实现细节，防御任何重构                                          • 拥抱快速迭代，支持 Codex/Claude/ACP/Local
```

* **作为逆向克隆**：目标是与 Google 内部闭源二进制 `agy` 达到字节级一致。这一目标强迫系统必须保留大量原厂的历史包袱与偶发设计（例如：某些字段由于旧版本未初始化产生的空指针容错、特定的截断字符、非标的 RPC 编码等）。
* **作为通用 Agent Harness**：目标是作为下一代通用编程代理运行时，支持 Codex、Claude Code、ACP 以及本地 LLM。这一目标要求系统具备高度的模块化、声明式配置和协议通用性。

**后果**：代码库在很多地方处于“精神分裂”状态——在通用开发中属于“显而易见的坏味道”甚至“语法/拼写 Bug”的地方，在本项目中却被奉为必须誓死捍卫的“Load-bearing Invariant（承重不变量）”。

---

## 2. 违背软件工程最佳实践的架构痛点

### 2.1 水平技术切片（Tiers）vs 领域内聚缺失（Shotgun Surgery）

* **现状**：
  项目设计了严格的 5 层技术分层：
  $$\text{core (1)} < \text{features (2)} < \text{infra (3)} < \text{app (4)} < \text{ui (5)}$$
  并用专门的架构单元测试（`internal/core/architecture`）强制卡死：**绝对禁止向上跨层，绝对禁止循环依赖**。
* **批判（违背 DDD 领域内聚原则）**：
  * 这属于典型的**技术性水平切片（Technical Horizontal Slicing）**，而不是现代软件工程推荐的**垂直特性切片（Vertical Slice Architecture）**。
  * **散弹式修改（Shotgun Surgery）**：当需要新增或调整一个简单的业务功能（例如一个 slash command `/title` 或 `/tasks`）时，改动必须粉碎性地散落在 4~5 个包中：
    - `internal/core/titlefmt` (Tier 1: 格式化底层)
    - `internal/app/agentturn` (Tier 2: 引擎能力位定义)
    - `internal/app/session/slashactions` (Tier 4: 业务状态与 Prompt 组装)
    - `internal/ui/slashcommands` & `internal/ui/views/slashpicker` (Tier 5: 展现与作用域过滤)
    - `internal/cliapp/runMode` (Host 层: UI 状态挂载与事件循环)
  * 这种割裂大幅增加了代码导航难度与认知负担，修改一个业务逻辑往往需要跨越半个代码树。

### 2.2 上帝对象（The runMode God Object）与 Bubble Tea 的伪纯函数困境

* **现状**：
  Bubble Tea 采用 Elm Architecture，其设计核心是**单向数据流与不可变更新**：
  $$\text{Update}(\text{Model}, \text{Msg}) \to (\text{Model}, \text{Cmd})$$
  然而在 `internal/cliapp` 中，`runMode` 是一个拥有上百个字段、指针四处传递、挂载了海量业务逻辑的**超级上帝对象（God Object）**。
* **批判（高耦合与隐藏副作用）**：
  * `slash_commands_handlers.go`、`tui_update_stream.go`、`tui_run.go` 等几十个文件，本质上都在并发、直接变异修改同一个 `m *runMode` 的内部字段（如 `m.session`, `m.response`, `m.blocks`, `m.picker`, `m.store` 等）。
  * 缺少显式的事件与状态转移驱动，大量操作依赖深层函数内部的隐式状态突变。
  * 导致单元测试极其脆弱，测试任何一个交互逻辑都必须构建庞大的 `runMode` 实体骨架，无法进行快速、轻量的无副作用隔离测试。

### 2.3 宿主（Host）与双引擎的行为漂移（Behavioral Drift）

* **现状**：
  系统在架构上分裂为两套平行推进的执行宿主：
  1. **交互式 TUI 路径**：由 `internal/cliapp` 驱动，绑定 `bubbletea` 与 `agentturn.Local`。
  2. **无头命令行 `--print` 路径**：由 `internal/printmode` 驱动，绑定 PiG 的 `coding.Session`。
* **批判（违背 DRY 原则与单一事实源）**：
  * 两套宿主虽然共用部分 bridge，但在许多核心机制上长期存在隐式行为漂移：
    - **权限判定默认值**：Print 模式默认 `request-review`，TUI 模式默认 `allow-by-default`。
    - **Slash 命令解析语法**：Print 模式使用宽松的 `strings.Fields`，TUI 模式使用严格的前缀字符扫描。
  * 违背了现代系统的核心分层哲学：**“核心运行时应当是一个独立的 Headless Daemon / State Machine，TUI 与 CLI 仅仅是该状态机上的外部适配器（Adapters）”**。

---

## 3. 对 AI Agent / 自动化协作极不友好的隐形壁垒

如果将系统视作人类开发者与 AI Agent 共同协作的载体，当前代码库设置了很高的认知与工具壁垒：

### 3.1 认知过载：200+ 条不可违背的“承重不变量”
* `AGENTS.md` 积累了 200+ 条“load-bearing invariants”。
* **注意力预算耗尽（Context Dilution）**：LLM 在被注入如此庞杂且高度细节化的反直觉规则后，指令遵循率和全局推理深度会受到明显稀释。
* **隐形暗礁（Silent Pitfalls）**：Agent 做出的常规重构（比如规范化一段报错文案、修正一个拼写笔误、简化一段看似冗余的判断），极易在毫无察觉的情况下击碎某条历史逆向的 Byte-parity 约束。

### 3.2 工具链基线防御的“反直觉模式”
* **“禁止 `go test ./...`”**：
  现代所有 Go 语言开发者和自主 Agent 的默认本能操作均是 `go test ./...`。但在本项目中，由于存在 `scratch/` 实验目录，该命令必定编译失败。这一非标设定增加了额外的外部约束指令。
* **`gofmt -l` 故意不干净**：
  为避免大量无意义格式化 diff 淹没历史逆向补丁，项目限制了全局格式化。这使 Agent 失去了最依赖的自动化代码规范格式化守护。

### 3.3 历史推导过程与当前系统事实的界限模糊
* 仓库中的 `docs/cycles/` 积累了从 Cycle 1 到 Cycle 383 的海量报告。
* 很多早期 Cycle 中的假设和结论在后续版本中已被推翻（“newer report wins”）。Agent 在执行语义搜索或 Grep 时，极易误读过期的临时结论，造成事实幻觉。缺少一份**剥离历史考古过程、仅反映当前系统架构真实状态的单一事实源（Single Source of Truth）**。

---

## 4. 现代化 Agent 原生架构演进路线图

为了在保留逆向保真能力的同时，使代码库重获现代工程的高内聚、可测试性与 Agent 友好度，建议采取以下三步演进策略：

```
                       [ 现代分层演进目标 ]
                       
  +-------------------------------------------------------+
  |              Presentation / Adapters                  |
  |   (Bubble Tea TUI / Headless CLI / Server WebSocket)  |
  +-------------------------------------------------------+
                             |  calls / events
                             v
  +-------------------------------------------------------+
  |               Core Agent Session Engine               |
  |     (Unified State Machine, Turns, Policy, Tools)     |
  +-------------------------------------------------------+
                             |
         +-------------------+-------------------+
         |                                       |
         v                                       v
+-----------------------+              +-----------------------+
|   Modern Extensible   |              |  Agy Compat Adapter   |
|   Backends & Plugins  |              |  (Quarantined RE      |
| (Codex, Claude, ACP)  |              |   Parity & Quirks)    |
+-----------------------+              +-----------------------+
```

### 4.1 物理隔离“逆向保真适配层”与“现代通用核心”
* **Quarantine the RE Quirks（隔离逆向怪癖）**：
  将所有与原厂二进制强绑定的字节对齐逻辑、奇数空格截断、特殊 hex 字符判定，统一收敛至隔离包（如 `internal/adapter/agycompat` 或 `internal/legacy/quirks`）。
* **通用核心解毒**：
  让通用的 `session`、`tools`、`slashactions`、`policy` 彻底回归标准、清晰、类型安全的 Go 惯用法。通过明确的 Adapter 接口抹平差异，主干不再为特定版本的二进制历史妥协。

### 4.2 瓦解 runMode 上帝对象，迈向真正的响应式/状态机驱动
* **继续推进控制器下沉**：
  借鉴 `internal/app/session/slashactions` 的成功模式，将 `cliapp` 中剩余的业务逻辑抽取为无副作用的纯领域模块：
  - `SessionSupervisor`（统一管理会话上下文与历史回溯）
  - `TaskRegistryController`（统一管理后台任务）
  - `CommandDispatcher`（基于能力位路由命令）
* **使 UI 成为纯展示适配器**：
  `runMode` 应当只负责终端按键捕获、排版渲染与 `tea.Cmd` 转发，内部状态全部依托不可变事件（`tea.Msg`）驱动，彻底根绝跨文件直接修改对象字段的做法。

### 4.3 建立 Agent-First 的开发与质量防护体系
* **统一工具链入口**：
  引入 `Makefile` 或 `scripts/verify.sh`，封装安全合规的构建与测试命令，消除“不可 `go test ./...`”的操作心智负担。
* **提炼架构宪章（Condensed Architecture Primer）**：
  将 `AGENTS.md` 中的 200+ 条条目归纳整理：
  - 核心宪章（10 条最高原则，作为 Agent 的核心上下文）
  - 细节索引表（按领域划分按需检索，避免一次性塞入上下文）
* **单一事实源文档体系**：
  保持 `docs/guide/` 始终与最新代码严格对齐，历史推演与考古报告沉淀到专门的 `archive/` 目录，杜绝旧周期结论干扰 Agent 决策。
