# Antigravity CLI — 反向工程重建仓库

> ⚠️ 本仓库是 **reverse-engineering 研究工作台**, 分两部分: `out/` + `rea-go-seeds/`
> 是研究证据 (报告 / seed / 反编译产物, 不是程序), `cmd/` + `internal/` 是基于这些
> 证据从零重写的 **Antigravity CLI 克隆** (可 build 可跑). 来源二进制为 Google 内部
> 闭源 Antigravity CLI (`agy`), 当前对齐版本 **1.4.11**. 所有产出的 Go 代码均来自
> pclntab 名称恢复 + Ghidra C 反编译 + 人工整理, 仅作结构分析使用, 不复制任何
> Google 拥有源代码.

[![CI](https://github.com/chadgpt/openagy/actions/workflows/test.yml/badge.svg)](https://github.com/chadgpt/openagy/actions/workflows/test.yml)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](./LICENSE)
[![Go 1.27+](https://img.shields.io/badge/Go-1.27%2B-00ADD8.svg)](https://go.dev/)

---

## 这是什么

1. **研究工作台** —— 从 stripped Go binary 的 `pclntab` 段恢复函数名 (当前 seeds 取自 agy 1.4.6
   共 **161,286** 个), 再用 Ghidra 12.1.4 反编译 priority 函数, 产出可读的 C 伪代码与
   102 份分包报告. 全部证据在 `out/`.
2. **能跑的克隆** —— `cmd/` + `internal/` 是 Antigravity CLI 的逐字节对齐重建:
   12 个可见子命令、参考的系统提示词 (1.4.6 抓包 70,853 B, 1.4.7 复核静态段逐字节相同)、参考的工具面、TUI 的逐屏对齐.
   当前对齐 **agy 1.4.11**, 版本漂移由 `internal/referencedrift` 的 gate 盯着.
3. **仪器** —— 内置的 CloudCode 代理能把官方 `agy` 拉起来并抓它的真实请求/响应
   (`pigasus agy`), 官方二进制**不被 patch**. 这是逆向提示词与工具调用格式的手段.

> **不要用 `go test ./...`** —— `scratch/` 自建立起就无法编译. 用
> `go test ./cmd/... ./internal/...`.

---

## 快速开始

### 构建

```bash
git clone https://github.com/justwasm/pigasus.git
cd pigasus
go build -o /tmp/pigasus ./cmd/pigasus
```

或者直接装发布版 (module path 就是 `github.com/justwasm/pigasus`):

```bash
go install github.com/justwasm/pigasus/cmd/pigasus@latest
pigasus --version
```

**前置条件**: Go ≥ 1.26.2 (CI 与本机用 1.27) · 首次 build 需要能访问
`proxy.golang.org` 拉依赖 (含 `PiG`) · 纯 Go, 不需要 CGO.

### 跑一次

```bash
pigasus                       # 交互式 TUI
pigasus -p "总结这个目录"     # headless 一次
pigasus --model minimax-m3.1-flash -p "..."
```

### 命令面

`pigasus --help` 只列参考 `agy` 的 **12 个可见子命令** (这一面是逐字节钉死的):

| 子命令 | 作用 |
|---|---|
| `agent` / `agents` | 列出可用 agent (含 `--agent` 的自定义 Markdown agent) |
| `changelog` | 变更日志 |
| `help` | 子命令帮助 |
| `install` | 配置环境路径与 shell 设置 |
| `mcp` | 管理 MCP server (add / remove / list / enable / disable) |
| `mic-serve` | 把本机麦克风服务到另一台主机 |
| `models` | 列出可用模型 |
| `plugin` / `plugins` | 管理插件 |
| `remote-control` | 管理 remote-control 后台进程 |
| `update` | 更新 CLI |

另有**隐藏**的组 (不打广告, 但可用): `provider` / `model` (配置注册表),
`proxy` (CloudCode 代理), `accounts` (多账号切换), `providers` (`provider list`
的别名).

---

## 配置模型

规范文件只有一个: `~/.config/openagy/providers.json`
(`$XDG_CONFIG_HOME/openagy/providers.json`, `OPENAGY_PROVIDERS_CONFIG` 可覆盖).
**一个 provider 一个条目** —— endpoint、credential、wire protocol、以及它提供的
模型列表:

```json
{
  "providers": {
    "gateway": {
      "name": "Gateway",
      "type": "openai",
      "base_url": "https://gateway.example/v1",
      "api_key": "$OPENAGY_GATEWAY_KEY",
      "timeout_seconds": 60,
      "models": [
        {
          "id": "minimax-m3",
          "name": "MiniMax M3",
          "model": "MiniMax-M3",
          "context_window": 1000000,
          "default_max_tokens": 8192
        }
      ]
    }
  }
}
```

- **两个 id**: `id` 是目录里的名字 (`--model` 用它), `model` 是上游 id; `model` 为空
  表示两者相同.
- **credential** 可以写成 `$NAME` / `${NAME}` 从环境变量展开, 否则按字面量用.
- **endpoint 按协议推导**: `openai` → `<base>/chat/completions`, `anthropic` →
  `<base>/v1/messages`; `api_url` 可以逐字覆盖.
- **传输层没有内置 endpoint / credential / model** —— 没配置就是没配置.

用 clone 自己的 CLI 配置 (隐藏子命令):

```bash
pigasus provider add gateway --type openai \
    --base-url https://gateway.example/v1 --api-key '$OPENAGY_GATEWAY_KEY'
pigasus model add minimax-m3 --provider gateway --model MiniMax-M3 --name 'MiniMax M3'
pigasus provider list
pigasus model list
pigasus provider test gateway          # 探测 endpoint 与 credential
```

**路由规则**: 参考目录里的模型走 **CloudCode** (官方通道), 用户配置的模型走
**配置的 endpoint**, 两者都不是时走 shim 并打印一条指明 `providers.json` 的警告.
TUI 里 `/provider` 打开同一个注册表的面板 (增删改 + 验证).

> 旧的扁平文件 `~/.config/openagy/models.json` 已退休, 不再被读也不再被写.

---

## CloudCode 代理 (`pigasus agy` / `pigasus proxy …`)

clone 自带的传输层已经会说 CloudCode / OpenAI / Anthropic 三种方言, 所以代理**不是
一个功能**, 而是**逆向用的仪器**: 它把官方 `agy` 拉起来并把 `CLOUD_CODE_URL` 指到
本地, 从而 (1) 让官方二进制用上它本来没有的模型, (2) 抓到真实的请求/响应, 用来
比对 clone 的行为. 官方二进制**不被 patch** (`~/.local/bin/agy` 跑前跑后 SHA-256
相同).

```bash
pigasus agy                    # 代理起来, 然后 exec 真正的 agy
pigasus agy --help             # 打印官方 agy 自己的 help
pigasus proxy doctor           # 环境 + 上游诊断
pigasus proxy capture on|off   # 抓包开关 (落盘 ~/.openagy/captures/)
pigasus proxy log              # tail ~/.openagy/wrapper.log
pigasus proxy version
```

- 代理**只读** provider 注册表, 不写 —— 注册表的唯一写入口是 `pigasus provider …`.
- 日志写文件, **不写 stderr** (stderr 是 TUI 的终端); `OPENAGY_WRAPPER_LOG=stderr`
  是调试用的逃生口.
- `OPENAGY_WRAPPER_*` 环境变量和 `~/.openagy/wrapper.{json,log}` 路径**故意不改名**
  —— 改了会悄悄搬走已有的抓包目录.

---

## 会话数据互操作性与 Protobuf 重建

### 1. 实用级高保真互操作性 (Pragmatic High-Fidelity Interoperability)

克隆与官方 `agy` 共享底层的会话存储目录 (`~/.gemini/antigravity-cli/`), 实现了双向无缝流转:

- **多库双向对齐**:
  - 会话库 (`conversations/<uuid>.db`): 完整对齐 `trajectory_meta`、`steps` 与包含工作区路径的 `trajectory_metadata_blob` 表.
  - 全局索引库 (`conversation_summaries.db`): 双向更新与查询 `conversation_summaries` 主索引表.
  - 侧车元数据同步: 自动同步 `cache/last_conversations.json`、`cache/conversation_metadata.json` 与 `history.jsonl`.
- **纯 Go 驱动保证**: 在 Cycle 175 中废弃手工 B-Tree 解析器，迁移至 `github.com/ncruces/go-sqlite3` (纯 Go Wasm 编译，无 CGO 依赖)，彻底规避原生 C 动态库冲突与平台崩溃风险.
- **双向继续与分叉**:
  - 官方启动的会话在克隆中可通过 `-c` / `--continue` / `/resume` 直接继续无缝流转;
  - 克隆中启动或通过 `/fork` 分叉的会话，官方 `agy` 亦可直接索引并加载.
- **客观边界界定**: 明确定义为“**实用级高保真**”，而非盲目宣称“100% 绝对镜像”——针对核心交互流（User Prompt、Model Response、Tool Call/Result、Error/Notice）做到完全无损转换与重放；官方未公开的私有字段（如思考链折叠、内部审批流状态、细粒度 Telemetry 标记）采取稀疏覆盖，确保日常开发与跨客户端会话无痛衔接.

### 2. 官方 Protobuf (.proto) 定义完整逆向重建

由于 Google 内部 Go 工具链将未压缩的 `FileDescriptorProto` 直接静态链接进二进制常量段 (`__TEXT,__const`)，我们开发了自动化提取工具 (`out/local-macos/extract_descriptors.py` 与 `render_proto.py`)，成功从官方 `agy 1.4.7` 二进制中完整逆向提取出核心数据结构的原始 `.proto` 定义，保存在 [`out/local-macos/protos/`](out/local-macos/protos/):

| 官方内部 Proto 路径 | 重建文件 | 规模与内容 |
|---|---|---|
| `third_party/gemini_coder/proto/trajectory.proto` | [`trajectory_1.4.6.proto`](out/local-macos/protos/trajectory_1.4.6.proto) | 会话与轨迹核心: `Conversation`, `Trajectory`, `Step`, `TaskDetails` |
| `third_party/gemini_coder/cider/proto/trajectory_steps.proto` | [`trajectory_steps_1.4.6.proto`](out/local-macos/protos/trajectory_steps_1.4.6.proto) | Cider 轨迹步骤特化定义 |
| `third_party/jetski/cortex_pb/cortex.proto` | [`cortex_1.4.6.proto`](out/local-macos/protos/cortex_1.4.6.proto) | 核心交互模型: **487 个 message** 与 **86 个 enum** (`CortexStepType`, `CortexStepGeneric`, `CortexStepUserInput` 等) |
| `google/gca/aicode/v1main/remote_agent_proxy.proto` | [`remote_agent_proxy_1.4.7.proto`](out/local-macos/protos/remote_agent_proxy_1.4.7.proto) | 远程控制服务: **79 个 RPC 方法** 与 **174 个 message** 全量定义 |

---

## 仓库布局

| 路径 | 是什么 | 能 build 吗 |
|---|---|---|
| `cmd/`, `internal/` | 从零重写的 Antigravity CLI 克隆 (Go module root 就是 repo root) | ✅ `go build ./cmd/pigasus` |
| `out/`, `rea-go-seeds/` | 反向工程证据: 报告 / seed / 反编译产物 / 原始 dump | ❌ 是 *结果*, 不是程序 |
| `docs/guide/` | 克隆的分区手册 (架构 / agent loop / 模型 / 工具 / TUI / 逆向) | — |
| `docs/cycles/` | 每个 cycle 一份报告 (414 个目录) —— 证据在这里 | — |
| `docs/research/` | 外部参考与分析 | — |
| `devutils/` | 辅助工具 (`pigloop-demo`, `agy228probe`, `hpgen`) | ✅ 单独 build |
| `scratch/` | 从未编译过的实验草稿 | ❌ 永远不要加进 `go test ./...` |

`out/` 内部还有三棵**互不等价**的子树: 顶层 + `out/decompile/` 是 Linux/ELF 沙箱
(`/workspace/agy/`) 的产物, `out/local-macos/` 是**本机** macOS/arm64 的活流水线.
**两边的地址不可混用** —— 详见
[`docs/guide/environment.md`](docs/guide/environment.md).

---

## 文档

| 想知道 | 读 |
|---|---|
| 克隆的架构 (包图 / 两个 turn engine / 两条 transport) | [`docs/guide/architecture.md`](docs/guide/architecture.md) |
| 改 agent loop / transport / bridge 之前 | [`docs/guide/agent-loop.md`](docs/guide/agent-loop.md) |
| 改模型路由 / provider 配置 | [`docs/guide/models.md`](docs/guide/models.md) |
| 改系统提示词 / `--agent` | [`docs/guide/prompt.md`](docs/guide/prompt.md) |
| 改工具或结果文本 | [`docs/guide/tools.md`](docs/guide/tools.md) |
| 改 TUI 面板 | [`docs/guide/tui.md`](docs/guide/tui.md) |
| 跑 Ghidra 流水线 | [`docs/guide/reverse-engineering.md`](docs/guide/reverse-engineering.md) |
| 参考二进制升级了怎么办 | [`docs/guide/reference-drift.md`](docs/guide/reference-drift.md) |
| 每个 cycle 做了什么 | [`docs/cycles/`](docs/cycles/) · 最新一份 `docs/cycles/pig-bump-052/` |
| 给 AI agent 的操作手册 | [`AGENTS.md`](AGENTS.md) |
| 外部参考 | [`docs/research/REFERENCES.md`](docs/research/REFERENCES.md) |

---

## License

本仓库采用 **Apache License, Version 2.0** — 详见 [LICENSE](./LICENSE) 文件.
Copyright © 2026 chadgpt/openagy contributors.

注意: 本仓库**不包含** Google Inc. 拥有源代码. Antigravity CLI 是 Google 内部闭源
工具, 所有 Go 代码均为反向工程 (pclntab 符号恢复 + Ghidra 反编译 + 人工整理) 产出的
结构重建, 不复制 Google 任何受版权保护的源代码.

## Acknowledgements

- **rea-go-seeds** (vendored): [VallierDev/rea-go-seeds](https://github.com/VallierDev/rea-go-seeds)
  — Apache-2.0, pclntab recovery Go tool (175 行, stdlib-only)
- **PiG** (dependency): [MichaelKinsy/PiG](https://github.com/MichaelKinsy/PiG) v0.5.2
  — `internal/agent.Loop` 的 agent loop 与 `ai.Provider` 抽象 (cycle 183 起)
- **Ghidra**: U.S. National Security Agency, Apache-2.0
- **REA (Reverse-Engineering Agent)**: Node 22/24/26+, Apache-2.0
- **公开仓库托管**: chadgpt/openagy (研究) · justwasm/pigasus (发布)
