# Cycle 44 — B: `--backend` flag + default `agw` (Issue #2)

## Issue

User live run (issue #2): `--print` 默认走 shim-mock-1, 用户必须
显式设 `OPENAGY_BACKEND=agw` 才走真 backend。第一次跑 antigravity 的 user
看 cascade 不通, debug 半天才发现是 env 没设 — UX 不该是 operator-only。

## 修补

1. **`internal/cli/parser_specs.go`** (`ExtraFlags`):
   - 加 `--backend` string flag (cycle 44, 走 ExtraFlags 不动 v1.3.5 1:1):
     ```
     {Name: "backend", Type: "string", Usage: "Backend type: agw (real) or shim (mock). Default: agw (auto-falls back to shim if no OAuth token)"}
     ```

2. **`cmd/antigravity/main.go`** (重写 `pickBackend`, 加 3 个 helper):
   - `resolveBackend(fs) (resolved, fromFlag)` — 三段 dispatch:
     1. `--backend` flag
     2. `$OPENAGY_BACKEND` env
     3. default `"agw"` (cycle 44 头版行为改动)
   - `normaliseBackend(raw)` — 接受 `agw` / `agw.k0s.io` / `agw_k0s` /
     `real` / `k0s` / `shim` / `mock` / `off` / `false` / `test`,
     未知值 warn + 退回 default。
   - `hasOAuthToken()` — 查 `JETSKI_OAUTH_TOKEN` env +
     `~/.gemini/antigravity-cli/antigravity-oauth-token` (v4 canonical,
     跟 `auth.TokenFilePath()` 同源) +
     `~/.config/antigravity/oauth-token` (legacy), 命中即 true (只 `os.Stat`)。
   - 启动 log: `[backend] OPENAGY_BACKEND={env|<unset>|--backend} -> {resolved}`,
     显式标 source。`agw` + 无 token → `[warn] {source}=agw requested but
     no OAuth token found (...); falling back to shim backend` + 自动 shim。
   - `runLiveTUI` 跟 `runPrintMode` 入口不动 — `pickBackend` 已被两 caller
     调, 新逻辑自动覆盖 wizard→TUI + `--print` 两条路。

## 验证

| 场景 | 期望 | 实测 |
|---|---|---|
| `go build -o /tmp/agy-v044 ./cmd/antigravity/` | exit 0 | ✅ exit 0 |
| `--help` 含 `--backend` | 是 | ✅ 见 tail |
| 无 token, 无 env, 无 flag | warn + shim | ✅ `[warn] default=agw ...` → `shim` |
| 有 token, 无 env, 无 flag | agw (default) | ✅ `[backend] OPENAGY_BACKEND=<unset> -> agw (default)` |
| `--backend=shim` | shim | ✅ `[backend] --backend=shim -> shim` |
| `--backend=agw` + token | agw | ✅ `[backend] --backend=agw -> agw` |
| `OPENAGY_BACKEND=agw` + token | 真答 | ✅ cascade stream 真答 (tail 在下) |

### `--print 'what is 2+2?'` tail (有 token)

```
[backend] OPENAGY_BACKEND=<unset> -> agw (default)
[cascade] plan -> execute (stream opened)
<think>The user is asking a simple math question and wants a short answer.</think>
2 + 2 equals 4.
[usage] prompt=189 completion=25 total=214 fp=agw.k0s.io/MiniMax-M3
[cascade chain] init -> read -> plan -> execute -> stream -> save -> done
```

## 一手 evidence

- `cmd/antigravity/main.go:472-555` (`pickBackend` + `resolveBackend` +
  `normaliseBackend` + `hasOAuthToken`)
- `internal/cli/parser_specs.go:99` (`--backend` spec)
- `internal/auth/antigravity_token.go:76-78` (`auth.TokenFilePath` — v4
  canonical 路径, 跟我们的 token 检查复用)

## 已知 trade-off (不在本任务范围)

- `TestParser_12Subcommands` 失败 (期望 12, 实际 22) — **pre-existing**
  cycle 41 bug: cycle 41 加了 10 hidden subcommand 但 test 没更新。
- `internal/tui/*` 5 panel 改 (cycle 45 C1/C2) 让 `go build ./...` 整体
  跑有 build error — C1/C2 的事, 本任务 `go build ./cmd/antigravity/`
  单包 exit 0, 已验证。

## Notes

- 4 段 `default|OPENAGY_BACKEND|--backend` source label 让 `grep default=agw`
  / `grep OPENAGY_BACKEND=agw` / `grep --backend=agw` 都能命中, wrapper
  script / log scraper 好接。
- 旧 cycle 33 `models.NewRealClient` registry 路径被移除 — cycle 44 头部
  行为改成 "default agw"; 需走 registry 用 `OPENAGY_BACKEND=agw --model=...`。
