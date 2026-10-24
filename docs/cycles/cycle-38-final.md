# Cycle 38: 依赖跟 v1.3.4 / v1.3.5 binary 1:1 对齐 (cobra 删 + jetski/cli 实现)

## 关键发现

**1.3.4 binary 已经被 auto-update 成 1.3.5** (md5 b7f7ed7c66bc835ee21bd9b7304b8f74, 17:46 最后修改)。我用 `strings /workspace/agy/install/antigravity` 实证 v1.3.5 实际依赖:

### 删的依赖 (4 项, 跟 v1.3.5 binary strings 0 命中)
- `github.com/spf13/cobra v1.8.0` (cycle 33 拍脑袋装的)
- `github.com/spf13/pflag v1.0.5` (cobra 间接)
- `github.com/inconshreveable/mousetrap v1.1.0` (cobra 间接)
- `github.com/aymanbagabas/go-udiff v0.4.1` (lipgloss v2 带来, 1.3.5 binary 不用)

### 降级 (1 项)
- `github.com/aymanbagabas/go-osc52/v2` → `go-osc52/v1` (v1.3.5 binary 实际用 v1)

### 新加 4 个内部 package (1:1 reimpl jetski/cli 内部)
- `internal/cli/jetski.go` (350 行) — CLI engine
- `internal/cli/logfile.go` (130 行) — logfile + CleanStale
- `internal/cli/termio.go` (60 行) — Redirect / SavedStreams
- `internal/cli/steps.go` (130 行) — Step + BuildJSONOutput

### v1.3.5 binary --help 真实 24 flag + 12 subcommand (修正 cycle 33-37 错估)
- **24 flag** (21 long + 3 short): add-dir, agent, c, continue, conversation,
  dangerously-skip-permissions, disable-slash-commands, effort, i,
  input-format, json-schema, log-file, mode, model, new-project,
  output-format, p, print, print-timeout, project, prompt,
  prompt-interactive, remote-control, sandbox
- **12 subcommand**: agent, agents, changelog, help, install, mcp, mic-serve,
  models, plugin, plugins, remote-control, update

**cycle 33-37 错估为 31 flag + 10 subcommand** (从 ghidra symbol table 推, 不是
从实际 --help)。v1.3.5 真实 --help 是 stdlib `flag.PrintDefaults` 输出:
21 long + 3 short + 12 subcommand, 跟我们 1:1 byte-for-byte (剩 1 个 `--model-config`
是 cycle 33+ openagy-specific 扩展, v4 binary 没有)。

## 改动统计

| 类型 | 数量 |
|---|---|
| 新文件 | 5 (jetski.go / logfile.go / termio.go / steps.go / cycle38 docs) |
| 改文件 | 7 (cmd/antigravity/main.go, main_test.go, internal/cli/{parser,parser_specs,parser_test}.go, internal/rea/parity_capture_test.go, go.mod, go.sum) |
| 删 dep | 4 (cobra, pflag, mousetrap, go-udiff) |
| 降级 dep | 1 (go-osc52 v2 → v1) |

## 验证

```
go test ./...   (16 packages, 100+ sub-test)
ok  cmd/antigravity
ok  cmd/openagy-models
ok  internal/cli         (TestParseAllFlags / TestSubcommandDispatch /
                          TestAliasMatching / TestValueFormat /
                          TestJetskiCLI_1to1 — all PASS)
ok  internal/rea         (8 REA capture sub-test PASS)
ok  internal/tui         (10 TUI 1:1 panel test PASS, 跟 v4 binary EXACT MATCH)
ok  internal/auth, backend, codeassistclient, grpc, keyring,
    language_server_pb, mediagen, models, subagent, tools

diff <(antigravity --help v4 binary) <(recovered --help)
  (1 diff: --model-config 是 openagy-specific extension)
```

## 4 binary build
- cmd/antigravity: 10.7 MB
- cmd/openai-shim: 10.0 MB
- cmd/anthropic-shim: 10.0 MB
- cmd/openagy-models: 10.2 MB

## 下一步
- cycle 39: 重写 internal/models / internal/auth 基于 1.3.5 binary decompile
- cycle 40+: 补 1.3.5 binary 真实 mTLS / OAuth / agent loop
