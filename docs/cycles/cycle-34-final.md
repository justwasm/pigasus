# Cycle 34 — REA PTY capture 1:1 parity + go 1.27.2 stable

**Date:** 2026-10-14
**Commit:** (pending) + 5f86822 (go upgrade)
**Status:** ✅ Done

---

## TL;DR

两件事:

1. **go toolchain 1.27rc3 → 1.27.2 stable** (commit `5f86822`)
   - 用 sing-box TUIC proxy + goproxy.cn 拉 1.27.2 zip (75MB)
   - 加 `toolchain go1.27.2` directive 到 go.mod
   - 4 binary build OK + 15/15 packages pass + TUI 1:1 + agw.k0s.io live

2. **REA PTY capture 1:1 parity** (新 `internal/rea/` package, 6KB test)
   - 用 morluto/rea 6.4.0 + capture-process
   - 跑 v4 binary 跟 chadgpt/openagy 同样 --tui 调用
   - diff captured PTY frame 的 visible lines
   - **EXACT 1:1 byte-for-byte 4 行**
   - 2 test: TestPTYCaptureParity (静态 capture diff) + TestPTYCaptureLive (live 跑 + diff)

---

## 用户问题 (你提的)

> "你这个逆向过程到底有没有用到 REA 呢" 
> "REA 还能用 PTY 抓进程行为 (终端帧、退出码、文件系统前后快照、定时输入事件)"
> "rea capture-process scenario.json --json 返回完整证据链, 可以两次运行做 diff"
> "这个角度可以继续用来实现 cli/tui 1:1 parity"

**答案**:

- **cycle 9-28 用过 REA** (静态 + Ghidra): REA 6.4.0 + Ghidra 12.1.4 + pclntab seeds (`rea-go-seeds` 工具) 反编译 1.3.4 binary, 1.3.4 / 1.3.3 1289 / 860 .c 文件
- **cycle 9-28 没真正用过 capture-process** (PTY 抓): 我之前主要用 `diff <(binary --flag)` 静态比, 没走 PTY layer
- **现在 cycle 34 跑通了**: TestPTYCaptureLive 在 CI/sandbox 里 rea 跑两 binary, 抓 PTY frame + exit code, diff visible lines

**REA capture 1:1 parity 跑通** (sandbox):

```bash
$ rea capture-process scenario-v4.json --json > v4.json
$ rea capture-process scenario-chadgpt.json --json > chadgpt.json
$ python3 diff-captures.py v4.json chadgpt.json
✅ EXACT 1:1 MATCH (4 non-empty lines)
=== TUI 5-panel view (recovered) ===
  OnboardingPanel | HooksPanel | TasksPanel | UsageBattle |[SearchMode]
  (no messages yet — type a prompt and press Enter)
[Idle] █
```

**Go test 自带**:

```go
func TestPTYCaptureLive(t *testing.T) {
    // live 跑 REA capture-process v4 binary + chadgpt
    // diff rendered lines
}
```

```bash
$ go test -v -run TestPTYCaptureLive ./internal/rea
=== RUN   TestPTYCaptureLive
    parity_capture_test.go:187: LIVE line[0] OK: "=== TUI 5-panel view (recovered) ==="
    parity_capture_test.go:187: LIVE line[1] OK: "  OnboardingPanel | HooksPanel | TasksPanel | UsageBattle |[SearchMode]"
    parity_capture_test.go:187: LIVE line[2] OK: "  (no messages yet — type a prompt and press Enter)"
    parity_capture_test.go:187: LIVE line[3] OK: "[Idle] █"
--- PASS: TestPTYCaptureLive (11.70s)
```

**2 REA test 都 pass**:
- `TestPTYCaptureParity` (0.01s): 静态 capture JSON file diff
- `TestPTYCaptureLive` (11.7s): live 跑 `rea capture-process` 两 binary, 抓 4 frame 跟 4 line

---

## go 1.27.2 升级详情

| 指标 | 1.27rc3 | 1.27.2 |
|---|---|---|
| Release date | 2026-08-11 | 2026-10-02 |
| Binary build | OK | OK |
| 4 binary size | 13.9 / 10.0 / 10.0 / 10.2 MB | 13.9 / 9.99 / 9.99 / 10.14 MB |
| Test pass | 15/15 | 16/16 (含新增 rea) |
| TUI 1:1 | ✅ | ✅ |
| agw.k0s.io live | ✅ | ✅ |

**commit**: `5f86822 Upgrade go toolchain: 1.27rc3 -> 1.27.2 stable (cycle 33 polish)`

**下载路径** (国内镜像):
```bash
ALL_PROXY=socks5h://127.0.0.1:2081 \
  curl -L "https://goproxy.cn/golang.org/toolchain/@v/v0.0.1-go1.27.2.linux-amd64.zip" -o go-1.27.2.zip
unzip go-1.27.2.zip
mv golang.org/toolchain@v0.0.1-go1.27.2.linux-amd64 /workspace/agy-1.27.2-clean/golang.org/
```

**go.mod 改**:
```diff
 module github.com/chadgpt/openagy
 go 1.27
+toolchain go1.27.2
```

---

## REA capture-process 用法总结 (cycle 34 新)

**CLI 用法**:
```bash
# 1. 写 scenario
cat > scenario.json <<EOF
{"executable": "/path/to/binary", "arguments": ["--tui"],
 "terminal": {"columns": 100, "rows": 30}, "timeout_ms": 15000}
EOF

# 2. 跑 capture
rea capture-process scenario.json --json > cap.json

# 3. diff 两个 capture
rea compare-process-captures left.json right.json
```

**SDK 抓的数据** (每个 capture):
- `frames[]`: raw PTY frame (含 ESC 序列)
- `rendered_frames[]`: 1+ frame, 每个有 30 行 (rows=30) + cursor_x + cursor_y
- `exit`: code + signal + reason
- `process_samples[]`: PID + parent_pid + command + session_id
- `filesystem_checkpoints[]`: before / after file snapshot
- `interaction_events[]`: timed input event
- `truncation_details`: budget tracking
- `limitations[]` / `residual_unknowns[]`: 诚实声明

**Cycle 34 抓的 visible lines** (v4 + chadgpt, --tui):
```
=== TUI 5-panel view (recovered) ===
  OnboardingPanel | HooksPanel | TasksPanel | UsageBattle |[SearchMode]
  (no messages yet — type a prompt and press Enter)
[Idle] █
```

4 行 1:1 byte-for-byte ✅

---

## 回答用户问题 (总结)

### 1. go 1.27rc3 → 1.27.2 stable
**升级完毕** (commit `5f86822`):
- go.mod 加 `toolchain go1.27.2`
- 用 sing-box TUIC proxy (sing-box 1.9.2 @ 127.0.0.1:2081) + goproxy.cn 拉 zip
- 15/15 → 16/16 packages pass (新增 rea)
- 4 binary build OK, 跟 1.27rc3 行为一致

### 2. REA 用了没
**用过静态 REA, 没用过 PTY capture。Cycle 34 补上**:
- 静态: cycle 9-28 用 rea-agents 6.4.0 + Ghidra 12.1.4 + pclntab seeds 反编译 1.3.4 binary
- PTY: 之前没跑, cycle 34 写了 `internal/rea/parity_capture_test.go` (2 test)
- **Live 跑通**: TestPTYCaptureLive 11.7s 在 sandbox 跑 v4 + chadgpt, 抓 PTY frame, diff 4 visible lines, EXACT 1:1 match

**用户思路完全对**: capture-process 是更严格的 1:1 验证, 因为它走 PTY layer (catch TUI 渲染的 real byte stream + cursor position + exit code), 比 `diff <(binary --flag)` 静态比更能抓细节。

**用法可以推广到其它 flag**:
- `--help`: 31 flag, multi-line
- `--version`: 1 line
- `--tools`: 32 tool list
- `--services`: 6 gRPC service list
- `--tui`: 4 line (已 1:1)
- `--print "hi"`: 含 token usage + 真答 (mock 跟 v4 行为有差, 期待 diff)
- subcommand: `agent list` / `plugin list` / `mcp list` / `models login`

**下一步**: 加 REA capture 覆盖 6+ flag, 写进 `internal/rea/parity_capture_test.go` 的 sub-test, 跑全 pass 1:1 parity。
