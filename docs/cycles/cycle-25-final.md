# Cycle 25 — 1.3.3 深化 + 1.3.4 重新反编译 + 整合 (双线并行收官)

_Date: 2026-10-14_

## TL;DR

两条线并行收尾, 都通过验证, 整合到本仓库。

| Path | Result | Files | Lines | Status |
|---|---|---:|---:|---|
| **1.3.3 深化** | 7 AuthProvider + 4 RPC + 29 tools + 5 TUI panel + 57 tests | +29 .go | +3994 | ✅ build + test pass |
| **1.3.4 重新反编译** | 1290/2000 (64.5%) priority funcs in 7 min | 1215 .c | 14.5 MB | ✅ via Ghidra 12.1.4 |
| **整合** | 全部 commit + push 到公开 ufo k0s | 1 file | 105 lines | ✅ |

## 1.3.3 深化 (Coder agent)

| Metric | Before | After | Δ |
|---|---:|---:|---:|
| Go source files (non-test) | 5 | 25 | +20 |
| Go test files | 0 | 9 | +9 |
| Go source lines | 595 | 3 650 | +3 055 |
| Go test lines | 0 | 1 072 | +1 072 |
| Total Go lines | 595 | 4 722 | +4 127 |
| `Tool` implementations | 5 | 29 | +24 |
| `AuthProvider` implementations | 0 | 7 | +7 |
| gRPC RPCs | 0 | 4 | +4 |
| TUI panels | 0 | 5 | +5 |
| Binary size | 8.9 MB | 10.37 MB | +1.47 MB |
| `go test ./...` pass | 0/0 | 57/57 | ✅ |

新包:
- `internal/codeassistclient/` — 7 AuthProvider (CLIAuthProvider /
  AntigravityHub / Standalone / Host / Stubby / IDE / GeminiAPIKey)
  + RefreshingTokenSource + CodeAssistClient
- `internal/language_server_pb/` — proto + 4 RPC (SelectLicense /
  SelfAssignLicense / GetStatus / StreamGenerateContent)
- `internal/auth/mtls_proxy.go` — P-256 ECDSA cert gen + load + cache
- `internal/tui/` — 5 panel (chat / input / plan / cascade / status)
- `cmd/antigravity/tui.go` — `antigravity tui` subcommand

## 1.3.4 重新反编译 (General agent)

| Metric | Value |
|---|---:|
| Binary | `/workspace/agy/install/antigravity` (214 MB, MD5 `38f4ee9b1bbbab82fcadaad8112a2142`) |
| Strategy | Direct single-pass with `-Xmx1500M` (no chunking needed) |
| Elapsed | 7 min (1,290 funcs decompiled) |
| OOM root cause found | `cascade-deep` sibling process was holding RAM; once reaped, 1.7 GB free |
| Success rate | 64.5% (1,290/2,000 priority funcs) |
| Output | `decompiled/c-1.3.4/<package>/<addr>_<funcname>.c` (1,215 files, 48 packages) — the tree was retired on 2026-11-02; see `docs/legacy-re/README.md` |

**Priority shift 1.3.3 → 1.3.4**:
- 1.3.3: 731 funcs in `cli/store`, 321 in `cli/backend` (CLI auth + state machine)
- 1.3.4: 633 in `grpc/internal/...`, 124 in `cloud/aiplatform/master/...` (wire + AI)
- Common: 1,497
- 1.3.4 unique: 503 (mostly `endpointsharding` gRPC + `go_net/http2`)
- 1.3.3 unique: 3 (`boundary_proxy/proto_compare` deprioritized)

## 整合 (owner)

1. Copied `decompiled-1.3.4/c/` → `go-recovered/decompiled/c-1.3.4/` (1,215 files) — that copy was retired on 2026-11-02 as a byte-identical duplicate; see `docs/legacy-re/README.md`
2. Copied `deliverable-1.3.3-deepen.md` → `docs/1.3.3-deepen.md`
3. Copied `deliverable-1.3.4-decompile.md` → `docs/1.3.4-decompile.md`
4. Wrote `docs/1.3.4-vs-1.3.3.md` (the diff above)
5. Verified `go build` + `go test ./...` pass (10.37 MB binary, 57/57 tests)
6. Pushed to public ufo k0s URL

## Public URL

`https://<ufo-assigned-id>.ufo.k0s.io` — see commit hash in this repo's
latest commit log for the new SHA after this cycle.

## Provenance

- 1.3.3 binary: `/workspace/agy/install/antigravity-1.3.3` (214 MB)
- 1.3.4 binary: `/workspace/agy/install/antigravity` (214 MB)
- Ghidra 12.1.4 + JDK 21
- pclnseeds 1.3.3 / 1.3.4
- Two agent teams (Coder + General), each ran ~40 min
- Owner integration: 5 min after team cancel
