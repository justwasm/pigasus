# Cycle 49: 9 subcommand byte-level 1:1 close-gap

> Date: 2026-10-15 06:49 UTC
> Source-of-truth: `/workspace/agy/install/antigravity-v1.3.5` (215 MB)
> Recovered HEAD: `80cab2e`
> Tuic proxy: `HTTPS_PROXY=socks5h://127.0.0.1:2081`

## 1. 9 subcommand byte-level diff 实证 (cycle 49 baseline)

抓两边 9 subcommand 输出, diff 列出每条差距:

| # | Subcommand | v1.3.5 binary 真值 (6.3 KB baseline) | recovered 现状 (1.5 KB baseline) | 差距 |
|---|---|---|---|---|
| 1 | `version` | `Error: unexpected argument "version". Prompts are read only from -p/--print, -i/--prompt-interactive, or stdin, so this argument would have been ignored.` | `flag provided but not defined: -version` + usage list | 错 reject path: 应是 error msg, 不是用法列表 |
| 2 | `changelog` | 完整 1.3.5 (6 项) + 1.3.4 (10 项) release notes, 16 条 `· Added/Changed/Improved/Fixed` | `[recovered] changelog: see github.com/...` 一行 stub | 16 条 release notes 完整 text 缺失 |
| 3 | `mcp list` | `NAME TYPE STATUS COMMAND/URL\ndummy http enabled http://localhost:8080\nfoo stdio enabled bar` (4-col 表格 + 2 server) | `no MCP servers configured (missing /workspace/.home/.config/antigravity/mcp.json)` | 4-col 表格 + 读 v1.3.5 真值 mcp.json + 2 server 渲染 |
| 4 | `plugin list` | `No plugins installed.` | 空 (无输出) | "No plugins installed." 一行 |
| 5 | `update` | `⟳ Checking for updates... (current version 1.3.5)\n✓ You are already on the latest version.` | `[recovered] update: not implemented in this reconstruction` | spinner + 当前 version + latest 消息 + 真 update API check |
| 6 | `install` | 完整 installer (PATH warning + binary copy + ✅ message) | `dir= skipAliases=false skipPath=false (stub)` | 跑 installer.go:27/147/184: 复制 binary 到 $HOME/.local/bin/agy + PATH warning + ✅ |
| 7 | `remote-control status` | `Daemon status: inactive\nInstance name: not assigned yet; the daemon generates one on its first start.` | 空 | 2 行: daemon status + instance name 描述 |
| 8 | `models` | `Fetching available models...\n6 行 model: gemini-3.8-flash-high/medium/low + claude-sonnet-4-6 + claude-opus-4-6 + gpt-oss-120b-medium` | `[recovered] models:\nconfig: ...\navailable (1):\n   1. agw agw agw` | "Fetching..." spinner + 走 daily-cloudcode-pa + 6 真 model 名 (从 cycle 42 实证) |
| 9 | `agents list` | `Error: unexpected arguments: [list]` (没 list 子命令, agents 直接子命令) | `  list` (列 list 子命令) | 改 agents 不接受 list 参数, error msg |

## 2. 修复路线 (5 sub-agent 并行, 30 min hard)

| Sub-agent | 改 subcommand | 工作量 |
|---|---|---|
| A | `version` + `plugin list` + `remote-control status` + `agents` (4 短, 加起来 30 min) | 30 min |
| B | `mcp list` (4-col 表格 + 读 mcp.json) | 30 min |
| C | `changelog` (1.3.5 + 1.3.4 完整 16 条 release notes) | 30 min |
| D | `update` + `models` (走 daily-cloudcode-pa + 6 真 model) | 30 min |
| E | `install` (跑 installer.go:27 复制 binary + PATH warning) | 30 min |

## 3. byte-level verify (cycle 49 verify)

跑 recovered binary 9 subcommand, diff 跟 v1.3.5 binary baseline:
- text content 1:1 (允许 ANSI 色码不同, 文字 + 表格 + 错误 msg 必须 match)
- exit code 1:1 (e.g. version/agents 返 error exit 1)
- 表格对齐 1:1 (mcp list 4-col + plugin/models N-col)

## 4. 范围

只改 `internal/cli/` 跟 subcommand handler, 不动 TUI / cascade / auth. v135 范式 TUI + unified config + token refresh 保留.

## 5. 不在范围

- 9 subcommand 跑真 backend (OAuth 注册 / plugin marketplace / update server): cycle 50+ 续
- changelog 1.3.3 跟更早 release notes: cycle 50+ 续
- install 真写 ~/.local/bin/: 沙箱需 sandbox 配 cap, cycle 50+ 续
- byte-level screenshot 渲染: cycle 47 render_agy_v2 已 ship, cycle 50+ 加 9 subcommand screenshot

## 6. 关闭标准

| 指标 | 关闭阈值 |
|---|---|
| 9 subcommand text match | ≥ 90% 字符串 match (允许 error msg wording 略变) |
| Exit code match | 9/9 (subcommand 跟 v1.3.5 binary 同 exit code) |
| 表格 col 宽 match | mcp list + plugin list + models 1:1 col 宽 |
| Update spinner | "Checking for updates..." + "current version X.Y.Z" + "✓ You are already on the latest version." 1:1 3 行 |
| Install warning | "Warning: X is not present in your active PATH." + ✅ + binary placed at X/agy 1:1 3 行 |

## 7. 实证 baseline 留底

- `/tmp/agy-cmp-049/v135-9sub-baseline.txt` (6.3 KB, v1.3.5 binary 真值)
- `/tmp/agy-cmp-049/recov-9sub-baseline.txt` (1.5 KB, recovered cycle 49 起点)
