# Reverse-Engineering SOP: Extraction, Distribution & Baseline Alignment

> **标准化逆向工程提取作业规程 (SOP)**
> 本文规范了 OpenAGY (`pigasus`) 针对 Google Antigravity 上游三大核心组件的一手资产提取、网络在线包抓取、跨平台分发与可重现性 (Reproducibility) 验证流程。

---

## 1. 上游三大核心组件概览与官方在线分发端点

为了摆脱对宿主机本地安装环境的强依赖并确保**全局可重现性 (Reproducibility)**，工程支持直接从 Google 官方分发端点在线抓取最新二进制安装包：

| 目标组件 | 官方在线更新/抓取端点 | 默认提取目标目录 | 承载的关键一手契约 |
|---|---|---|---|
| **`agy` (CLI)** | `https://antigravity-cli-auto-updater-974169037036.us-central1.run.app/manifests/{os}_{arch}.json`<br>（下载 GCS `cli_{os}_{arch}.tar.gz` 并比对 SHA-512） | `out/extracted-<version>/` | pclntab 符号、Prompt 模板、内置 Skill、Web 控制台前端资源、Cortex / RemoteAgent Protobuf 描述符 |
| **`language_server` (IDE)** | Hub 释放列表：`https://antigravity-hub-auto-updater-974169037036.us-central1.run.app/releases`<br>IDE 安装源：`https://antigravity-ide-auto-updater-974169037036.us-central1.run.app/api/update/{platformKey}/stable/latest` | `out/ide-language-server-<version>/`<br>(软链接 `out/ide-language-server`) | pclntab 符号、IDE 内嵌资源、LanguageServerService Protobuf 完整描述符集 (`.protoset`)、Actuator/SSH Bridge 核心 JS 逻辑 |
| **`localharness` (SDK)** | PyPI 官方发布 JSON API：`https://pypi.org/pypi/google-antigravity/json`<br>（直接获取多平台 wheel 下载 URL 并比对 SHA-256） | `out/localharness-<version>/`<br>(软链接 `out/localharness`) | 本地测试/执行 Harness 符号、GenAI 与 Localharness 交互 Protobuf 协议 (`*.proto`) |

---

## 2. 干净提交准则 (Anti-Bloat Standards)

所有逆向提取工作**严格遵循 1.5.13 资产规范**：

### ✅ 允许跟踪的核心契约
1. `seeds/` (`seeds.txt`, `funcs.txt`): 函数符号映射与 pclntab 入口表；
2. `embed/` / `embed-fs/`: `//go:embed` 解包的系统提示词、内置技能、插件配置；
3. `protos/`: 二进制 `FileDescriptorSet` (`.protoset`) 及渲染出的 Protobuf 结构文本；
4. `web-assets/` / `ide-js/`: 解压自 ZIP 或 `app.asar` 的纯前端与运行时脚本（排除了 `node_modules`）；
5. `identity.txt` / `stamp.txt`: SHA-256 指纹、文件字节数、编译 CL 时间戳。

### ❌ 严禁提交的临时产物
1. Ghidra 项目数据库 (`proj/`, `proj-*/`, `*.gpr`, `*.rep`)；
2. 批量反编译生成的数千个 `decompile-*.md` 或 `.c` 伪代码文本；
3. Python 字节码缓存 (`__pycache__/`, `*.pyc`)；
4. 抓取的原始大包与本地缓存文件（统一落入受 `.gitignore` 保护的 `.cache/upstream/` 与 `/bin/`）。

---

## 3. 一键工具链与 Makefile Targets

工程提供了标准化的 Makefile Target 与自动化工具：

| Makefile 目标 | 对应底层脚本 | 作用说明 |
|---|---|---|
| `make fetch-upstream` | `scripts/re/fetch_binaries.py --target all` | 从官方更新服务并发抓取三大组件的最新二进制，缓存至 `.cache/upstream/` |
| `make fetch-changelog` | `scripts/re/fetch_changelog.py` | 从官方 GitHub `CHANGELOG.md` 抓取最新（或 `--version` 指定）版本变更条目至 `internal/cli/testdata/changelog_upstream_<version>.md` |
| `make extract-agy` | `scripts/re/extract_agy.py` | 提取 `agy` CLI 资产至 `out/extracted-<version>` |
| `make extract-ls` | `scripts/re/extract_ls.py` | 提取 IDE language_server 资产至 `out/ide-language-server-<version>` |
| `make extract-localharness` | `scripts/re/extract_localharness.py` | 从 PyPI 快速下载 wheel 并提取符号及重构 `.proto` 至 `out/localharness-<version>` |
| `make extract-all` | `scripts/re/extract_all.py` | 串行执行全量提取任务 |
| `make check-reproducibility` | `scripts/re/verify_reproducibility.py` | **验证 100% 可重现性**：独立二次提取并进行字节级一致性断言 |

---

## 4. 跨平台抓取与独立工具 (`scripts/re/fetch_binaries.py`)

[scripts/re/fetch_binaries.py](file:///Users/gear/GitHub/openagy/scripts/re/fetch_binaries.py) 是专用于网络抓取 upstream 二进制的分发工具，完全支持跨平台下载：

```bash
# 抓取 Linux x86_64 版的 agy CLI
python3 scripts/re/fetch_binaries.py --target agy --os linux --arch amd64

# 抓取 Linux aarch64 版的 localharness wheel
python3 scripts/re/fetch_binaries.py --target localharness --os linux --arch arm64

# 抓取指定版本 (例如 1.5.13 / 0.1.23)
python3 scripts/re/fetch_binaries.py --target all --version 1.5.13
```

所有抓取产物自动校验官方公布的校验和（SHA-512 / SHA-256），并缓存于 `.cache/upstream/` 中，下次运行秒级命中。

---

## 5. 可重现性验证机制 (Reproducibility Verification)

运行以下命令可自动验证当前提取流程的确定性与一致性：

```bash
make check-reproducibility
```

该测试将：
1. **`agy` 校验**：在独立沙箱临时目录中重新解构 `agy`，比对文件与 `out/extracted-1.5.18` 的 SHA-256，断言 0 差异；
2. **`language_server` 校验**：在临时沙箱中重新解包，验证 `seeds`、`embed-fs`、`protos`、`ide-js` 与 `out/ide-language-server-2.34.0` 完全一致；
3. **`localharness` 校验**：进行两次独立解包并比对 Run A 与 Run B，同时与 `out/localharness-0.1.24` 黄金基线逐字节比对，断言 8 个 `.proto` 文件与函数符号表完全重现。

---

## 6. 上游版本漂移感知与巡检 SOP (`scripts/re/drift_check.sh`)

当上游发布新版本（或本地升级 Antigravity 二进制）时，必须执行自动化版本漂移巡检与契约差异比对：

```bash
# 运行标准漂移巡检并自动更新契约报告
make drift-check
# 或直接指定候选二进制与输出
./scripts/re/drift_check.sh --binary /path/to/new-agy --report-json out/drift.json
```

### 巡检与比对流水线包括：
1. **AST 与符号差异自动化分析**：
   - 通过 `rea-go-seeds` 与 Go AST 提取新版本 pclntab 函数符号与源码声明；
   - 细粒度比对 `cortex`、`remote_agent_proxy`、`tools_pb`、`subagent_swarm` 等核心子系统符号增删；
2. **Schema & Flag 漂移报警**：
   - **CLI Flags**：自动比对上游命令行 Flag 与 `internal/cli/parser.go` / `internal/core/cliflags`；
   - **Slash Commands**：比对上游斜杠命令目录与 `internal/ui/slashcommands/commands.go`；
   - **Tool Schemas 深度比对**：比对模型工具定义（`internal/app/tools/surface/model_tools_1.5.14.json` 及 `Swarm` 工具），检测新增工具、新增必填参数（CRITICAL 破坏性变更）、新增可选参数（WARNING）以及字段类型/枚举变更；
3. **自动同步契约文档**：
   - 自动格式化 Markdown 报告并幂等更新 [`docs/guide/reference-drift.md`](reference-drift.md)；
4. **金丝雀模拟与 CI 防御**：
   - 支持 `--simulate-canary` 注入模拟破坏性变更用于 SOP 测试；
   - 支持 `--fail-on-drift` 在发现未对齐的 CRITICAL 漂移时以非零状态码退出，作为 CI 门禁。

---

## 7. 上游 Changelog 获取与逐条功能对齐 SOP

`agy` 二进制内置的 `agy changelog` 始终**滞后一个版本**（例如 `1.5.18` 二进制打印的 feed 最高只包含 `1.5.17:` 节），而当前版本自身的完整发布说明发布在官方 GitHub 仓库的 `CHANGELOG.md` 中。因此每次版本对齐必须同时维护两份 Changelog 契约：

1. **二进制内置 Feed（`internal/cli/testdata/changelog_reference.txt`）**：
   - 运行新版本 `agy changelog > internal/cli/testdata/changelog_reference.txt`；
   - 同步更新 `internal/cli/changelog_data_test.go` 中的 5 个校验常量（字节数、行数、节数、bullet 数、SHA-256）及 `internal/infra/referencedrift/reference.lock.json`。
2. **官方发布说明（`internal/cli/testdata/changelog_upstream_<version>.md`）**：
   - 运行 `make fetch-changelog`（或 `python3 scripts/re/fetch_changelog.py --version <version>`），从 `https://raw.githubusercontent.com/google-antigravity/antigravity-cli/main/CHANGELOG.md` 抽取目标版本小节；
   - 将该文件中的每一条 bullet 作为**逐条功能对齐清单**，结合 `seeds/funcs.txt` 符号 diff 与 `embed/` / `protos/` / `web-assets/` 差异逐项实现并补充回归测试。
