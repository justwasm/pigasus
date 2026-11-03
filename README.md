# Pigasus (OpenAGY) — 现代高保真 Antigravity 平台与通用 Agent 运行时

> **Pigasus**（开源工作台代号 **OpenAGY**）是一个**现代、高保真复刻且功能超越原厂**的 Antigravity 生态平台与通用 Agent 运行时。它采用 **TUI / Engine 彻底分离**的解耦架构，既像素级还原了 Google 内部闭源 `agy` CLI 的终端交互与原生能力，又作为通用 Agent 客户端与增强外挂，打破了原厂的封闭生态与功能局限。

[![CI](https://github.com/chadgpt/openagy/actions/workflows/test.yml/badge.svg)](https://github.com/chadgpt/openagy/actions/workflows/test.yml)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](./LICENSE)
[![Go 1.27+](https://img.shields.io/badge/Go-1.27%2B-00ADD8.svg)](https://go.dev/)

> **仓库与模块名称**: 本项目的研究与开发托管在 `chadgpt/openagy`，Go module path 为 `github.com/justwasm/pigasus` —— 发布与 `go install` 均走后者。

---

## 核心定位：不止于黑盒逆向

Pigasus 不仅仅是对 Antigravity CLI 的简单黑盒逆向克隆，而是一个**以 Antigravity 为标杆、全面增强的现代 Agent 基础设施平台**：

```
                             +----------------------------------------------+
                             |              Pigasus TUI (UI)                |
                             |   Antigravity 像素级交互 / 状态行 / 视图     |
                             +----------------------------------------------+
                                    |                      |
                     (Unified / ACP / NDJSON)       (Agentlink WS)
                                    |                      |
            +-----------------------+                      v
            |                       |          +----------------------------+
            v                       v          |     Pigasus Server /       |
+-----------------------+ +------------------+ |     Local Engine           |
| Third-Party Agents    | | External ACP     | | (Native Tools, Sandbox,    |
| • Codex (RPC/NDJSON)  | | • Zed ACP Server | |  Prompt Templates, Hooks) |
| • Claude Code (NDJSON)| | • Custom ACP Svr | +----------------------------+
| • Reference agy       | +------------------+                |
+-----------------------+                                     |
                                                              v
+---------------------------------------------------------------------------+
|                          Antigravity 生态全产品矩阵                        |
| • cmd/harness (localharness - Python SDK 后端)                            |
| • cmd/langserver (IDE Language Server 协议对齐)                            |
| • remote-control (Web UI / Connect / WebRTC Mesh 原生兼容)                |
+---------------------------------------------------------------------------+
|                          外挂增强体系 (Enhancements)                       |
| • Multi-Account Pool (多账号配额轮换与智能负载均衡)                        |
| • Standalone Local Web Mode (本地化 Web 控制台)                           |
| • Aggregate Proxy (CloudCode / OpenAI / Anthropic 协议聚合与模型路由)       |
+---------------------------------------------------------------------------+
```

### 1. TUI / Engine 彻底分离架构
- **TUI 完美复刻 Antigravity CLI**：
  像素级对齐官方 `agy` 的交互动效、颜色调色板、快捷键绑定（含 Vim 模式）、状态行（Statusline）、多面板系统（`/diff`, `/context`, `/resume`, `/codesearch` 等）及官方 Markdown 渲染。
- **通用 Agent 客户端**：
  TUI 不受限于原生引擎，可作为通用的 **ACP Client** 或 **NDJSON Client** 自由接入第三方的 Agent 后端（如 **Codex**、**Claude Code**、**官方 agy** 及各类 ACP 服务），并内置基于能力位（Capability）的作用域自动隔离机制。

### 2. Harness / Engine 完整对齐 Antigravity 核心能力
- **完整对齐官方交互能力**：内置完整的系统提示词流水线、12 项核心工具面、动态 MCP 加载、分级权限策略（Deny > Ask > Allow）及 `hooks.json` 运行时。
- **原生兼容 Remote Control 协议**：无缝对接官方 Web UI，支持 Connect、gRPC-Web、WebRTC Mesh 隧道与 PTY 交互。

### 3. 全面覆盖 Antigravity 产品线形态
Pigasus 对 Antigravity 在 Google 内部不同产品形态进行了一体化覆盖与模拟：
- **`cmd/pigasus`**：Antigravity CLI 主程序及全功能交互式终端与服务宿主。
- **`cmd/harness` (`localharness`)**：对齐 Python SDK (`google-antigravity`) 依赖的专用 Agent 运行时，支持 stdio + WebSocket 协议与动态 MCP 工具合成。
- **`agy-acp-server`**：提供标准的 Agent Client Protocol (ACP) 服务端能力，支持接入 Zed 等现代编辑器。
- **`cmd/langserver`**：对齐 IDE 插件所依赖的 Antigravity Language Server 后台。

### 4. 强力外挂增强（弥补 Antigravity 原厂短板）
- **多账号智能路由与池化 (`accountpool`)**：打破官方单账号配额限制，支持多个 Google 账号的配额感知、故障自动熔断与轮换。
- **独立本地 Web 控制台 (`pigasus web`)**：无需依赖外部云端中转，一键启动本地/局域网的高性能 Web 界面与交互式终端。
- **智能聚合代理 (`Aggregate Proxy`)**：内置 CloudCode / OpenAI / Anthropic / 本地大模型协议转换桥梁，提供透明捕获诊断、模型映射与自动故障转移。

---

## 快速开始

### 安装与构建

```bash
# 推荐使用预编译包或通过 Go 安装
go install github.com/justwasm/pigasus/cmd/pigasus@latest
pigasus --version
```

或从源码编译：

```bash
git clone https://github.com/justwasm/pigasus.git
cd pigasus
go build -o /tmp/pigasus ./cmd/pigasus
```

**前置条件**: Go ≥ 1.26.2 (推荐 1.27) · 纯 Go，无 CGO 依赖。

> **测试运行提示**: 请勿执行 `go test ./...`（`scratch/` 为历史实验留存）。
> 请使用：`go test ./cmd/... ./internal/... ./endpoints/... ./configdir/...` 或验证架构分层：`go test ./internal/core/architecture/ -count=1`。

### 运行模式

```bash
# 1. 默认交互式 TUI (原生高保真 Antigravity 模式)
pigasus

# 2. 作为通用的 Codex / Claude Code 前端客户端
pigasus --backend codex
pigasus --backend claude

# 3. 无头批处理运行 (Headless Mode)
pigasus -p "总结当前项目的架构设计"
pigasus --model minimax-m3 -p "运行代码审查"

# 4. 启动 Agent 服务端并远程挂接 TUI
pigasus server --port 7777                 # 启动 Agentlink 守护进程
pigasus tui --server ws://127.0.0.1:7777    # 纯客户端挂接交互

# 5. 启动独立本地 Web 控制台
pigasus web --desktop --port 8080
```

---

## 命令面与扩展能力

`pigasus --help` 提供完整命令集，包含官方对齐命令与 Pigasus 独有的外挂扩展命令：

| 子命令 | 作用 | 归属类别 |
|---|---|---|
| `agent` / `agents` | 查看与管理可用智能体及自定义 Markdown agent | 官方对齐 |
| `changelog` | 查看系统变更日志 | 官方对齐 |
| `help` | 子命令帮助与快捷键索引 | 官方对齐 |
| `install` | 自动配置环境路径与 Shell 自动补全 | 官方对齐 |
| `mcp` | 管理 MCP 扩展服务 (add / remove / list / enable / disable) | 官方对齐 |
| `models` | 查看支持的模型目录与推理层级 | 官方对齐 |
| `plugin` / `plugins` | 官方与第三方生态插件管理 | 官方对齐 |
| `remote-control` | 远程控制后台守护进程管理 (start / status / stop) | 官方对齐 |
| `update` | 检查与更新 CLI 版本 | 官方对齐 |
| `accounts` | **外挂增强**：Google 多账号管理与配额查看 (list / switch / remove) | 增强扩展 |
| `model` | **外挂增强**：自定义 Provider 模型映射配置 (list / add / remove) | 增强扩展 |
| `provider` | **外挂增强**：第三方供应商端点与密钥管理 (OpenAI / Anthropic / 本地) | 增强扩展 |
| `proxy` | **外挂增强**：CloudCode 智能聚合代理与流量捕获诊断 | 增强扩展 |
| `web` | **外挂增强**：本地 Web UI 服务器 (局域网直连，无需云端中转) | 增强扩展 |
| `server` | **架构解耦**：启动独立的 Agentlink 无状态代理服务 | 架构组件 |

---

## 统一配置与多模型接入

Pigasus 采用**统一配置中心**：全部配置位于 `~/.openagy/`（或 `$OPENAGY_CONFIG_HOME`），彻底收敛了历史遗留路径。

- **`~/.openagy/providers.json`**：负责模型供应商与协议映射（OpenAI、Anthropic、自定义网关、本地 Ollama/vLLM）。
- **`~/.openagy/config.json`**：负责服务端点、多账号路由策略（`accountpool`）与代理设置。

### 添加自定义模型供应商示例

```bash
# 添加一个 OpenAI 兼容的 Gateway 供应商
pigasus provider add gateway --type openai \
    --base-url https://api.openai.com/v1 --api-key '$OPENAI_API_KEY'

# 注册模型
pigasus model add gpt-4o --provider gateway --model gpt-4o --name 'GPT-4o'

# 测试连通性与模型列表
pigasus provider test gateway
pigasus model list
```

**智能路由机制**：
- Antigravity 官方目录内的原生模型（Gemini 3.8 / 3.7 / 3.6 / Claude / GPT-OSS）自动走 **CloudCode** 高速通道；
- 自定义配置模型直连指定供应商；
- 未配置模型提供友好诊断与回退指引。

---

## 会话数据互操作性与 Protobuf 资产

Pigasus 与官方 Antigravity CLI 共享底层的会话存储架构（`~/.gemini/antigravity-cli/`），实现双向无缝流转：

- **双向继续与分叉**：官方启动的会话在 Pigasus 中可通过 `-c` / `/resume` 无缝接续；在 Pigasus 中创建或 `/fork` 的会话，官方 `agy` 亦可无缝索引与载入。
- **纯 Go SQLite 引擎驱动**：采用 `github.com/ncruces/go-sqlite3`（Wasm 纯 Go 实现，无 CGO），避免原生动态库冲突。
- **官方 Protobuf 结构逆向重建**：仓库在 [`out/local-macos/protos/`](out/local-macos/protos/) 中完整逆向提取并重建了官方核心 `.proto` 定义，包括 `cortex.proto`（交互步骤状态机）、`trajectory.proto`（轨迹核心）与 `remote_agent_proxy.proto`（79 个远程控制 RPC 服务全量定义）。

---

## 仓库布局与公共组件

Pigasus 拥有干净的**五层架构**（`core` → `features` → `infra` → `app` → `ui`），所有早期过渡的 Facade 壳已全部清理，各模块按领域内聚组织：

| 模块目录 | 说明 |
|---|---|
| `cmd/pigasus` | CLI 主入口（交互式 TUI、CLI 派发、Server 宿主） |
| `cmd/harness` | `localharness` 独立服务端（Python SDK 后端实现） |
| `cmd/langserver` | IDE Language Server 对齐服务 |
| `internal/app/agentturn/` | 统一引擎抽象（Local, Remote, Codex, Claude, ACP 适配器） |
| `internal/app/session/slashactions/` | 领域下沉的 Slash Command 纯解析与 Prompt 状态机 (Tier 4) |
| `internal/ui/slashcommands/` | 基于能力（Capability）与作用域（Scope）的命令过滤目录 |
| `geminicode/`, `agentlink/`, `endpoints/`, `configdir/`, `accountpool/` 等 | **30+ 个独立公开包**，无 `internal/` 依赖，供第三方开发者直接引用 |
| `docs/guide/` | 详尽的开发与维护指南（Agent loop、模型路由、权限策略、工具调用等） |
| `REVIEW.md` | 项目架构深度评审、工程哲学分析与现代化演进路线图 |

---

## License

本仓库采用 **Apache License, Version 2.0** — 详见 [LICENSE](./LICENSE) 文件。
Copyright © 2026 chadgpt/openagy contributors.

注意: 本仓库**不包含** Google Inc. 拥有之私有源代码。Pigasus 是基于协议分析、符号恢复与纯 Go 从零构建的开放独立实现。
