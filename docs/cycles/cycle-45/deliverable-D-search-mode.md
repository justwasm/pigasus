# D-search-panel-lifecycle deliverable

## Summary

Cycle 45 D (Issue #7 fix): SearchMode replaced the cycle-43 hardcoded placeholders (`Auth:cli / Tier:free / Endpoint:cloudcode-pa.googleapis.com / Eligible:true`) with real values from `auth.LoadTokenUnified()` + `CloudCodeAPIClient.LoadCodeAssist`. The 6 fields (Auth/Email/Tier/Endpoint/Eligible/mTLS) now reflect the v1.3.5 binary's actual state (cycle 42 evidence: consumer + navigaid@gmail.com + INELIGIBLE + daily-cloudcode-pa + false). The cycle 44 search-bar FSM (Mode=off/searching/results, `/`/`Esc`/`Enter`/typing) is preserved; the canonical `AuthState` struct + `AuthLoader` interface + `LoadFromAuth(ctx)` entry point are the cycle 45 additions.

## Changed files

- `internal/tui/search_mode.go` — refactored 160-line placeholder → 669-line real-auth integration. Adds `AuthState` struct, `AuthLoader` interface + `defaultAuthLoader` (real `auth.LoadTokenUnified` + `auth.NewCloudCodeAPIClient.LoadCodeAssist`), `LoadFromAuth(ctx)` method, `decodeJWTEmail` JWT decoder, `detectMTLSCert` macOS keychain check, `syncAuthFromLegacy`/`syncLegacyFromAuth` helpers, and `Auth()` getter. `NewSearchMode()` defaults updated to v1.3.5 binary values (consumer / INELIGIBLE / daily-cloudcode-pa / false). `View()` reads from canonical `auth.*` with legacy field fallback.
- `internal/tui/search_mode_integration_test.go` — new file (395 lines, 6 sub-tests). `TestSearchModeIntegration_RealAuthState` asserts the real values (consumer / navigaid@gmail.com / INELIGIBLE / daily-cloudcode-pa / false) and bans the cycle-43 placeholders. `TestSearchModeIntegration_NoTokenFallsBack` covers missing-token fallback. `TestSearchModeIntegration_PremiumTier` covers the Eligible=true happy path. `TestSearchModeIntegration_SearchBarKeyboard` re-verifies the cycle 44 FSM. `TestSearchModeIntegration_DecodeEmail` + `TestSearchModeIntegration_AuthLoaderNilSafe` pin the JWT decoder and nil-loader safety. `fakeAuthLoader` struct is the hermetic test seam (no disk / no network).
- `internal/tui/parity_test.go` — `TestV135ParitySearchMode` updated to expect the new defaults (consumer / n/a / INELIGIBLE / daily-cloudcode-pa / false) plus regression guards banning the cycle-43 placeholders (`Auth:       cli`, `https://cloudcode-pa.googleapis.com`, `Eligible:   true`).
- `docs/cycles/cycle-45/deliverable-D-search-mode.md` — cycle 45 deliverable (≤100 lines, includes verification commands + grep checks per plan.yaml).

## Notes

### Verifier commands (from plan.yaml)

```bash
# 1. grep LoadTokenUnified / daily-cloudcode-pa / INELIGIBLE  ≥ 3
grep -c "LoadTokenUnified\|daily-cloudcode-pa\|INELIGIBLE" internal/tui/search_mode.go

# 2. grep Auth*Method / AuthMethod*consumer / Tier*INELIGIBLE / Endpoint*daily-cloudcode  ≥ 2
grep -E "Auth.*Method|AuthMethod.*consumer|Tier.*INELIGIBLE|Endpoint.*daily-cloudcode" internal/tui/search_mode.go

# 3. go test - TestV135ParitySearchMode + TestSearchModeIntegration
cd /workspace/agy-recovered && GOTOOLCHAIN=local GOPROXY=off \
  /workspace/agy/go126/bin/go test ./internal/tui/ -count=1 -timeout=30s \
  -run "TestV135ParitySearchMode|TestSearchModeIntegration" 2>&1 | tail -10
```

### 早期干净 build 已 PASS

在 cycle 45 partial commit 246e46c 之前, 一次干净 build 已验证 `TestV135ParitySearchMode PASS` (0.07s). 该 commit 持久化了我的 search_mode.go (669 行) + search_mode_integration_test.go (395 行) + parity_test.go 改动.

### 跟其他 worker (A/B/C/E) 并发

- 5 worker (A/B/C/D/E) 同时改 tui/ 下的不同文件
- 期间文件多次被 `git reset` 反复 (reflog 显示 3 次 "reset: moving to HEAD") — 最后 246e46c commit 持久化
- 验证者若发现本地 working tree 不一致, 用 `git checkout 246e46c -- internal/tui/search_mode.go` 拉取 known-good state

### 跟 cycle 44 search-bar FSM 兼容

`SearchState` / `SearchStateMsg` / `Mode=off|searching|results` / `handleKey` 1:1 保留. `SetOnSearch(fn func(query string) int)` 也保留 — 6 字段布局 (12-char padding) 不变. View 现在读 canonical `auth.*` (cycle 45 source of truth), fallback 走 legacy 字段保持 backward compat.

### 跟 v1.3.5 binary 1:1 (cycle 42 证据)

`Auth:       consumer` ← `token.AuthMethod` 字段 (`AuthMethod: "consumer"` per antigravity_token.go)
`Email:      navigaid@gmail.com` ← `id_token` JWT payload 的 `email` claim
`Tier:       INELIGIBLE` ← `CloudCodeAPIClient.LoadCodeAssist` 返 `allowedTiers[0].id`, 失败 / 空返 "INELIGIBLE"
`Endpoint:   https://daily-cloudcode-pa.googleapis.com` ← v1.3.5 binary base URL (cycle 42 实证)
`Eligible:   false` ← Tier 联动: INELIGIBLE → false
`mTLS:       false` ← Linux 默认 false, macOS 走 `security find-identity` 检测 Secure Enclave

### 不在范围内 (后续 cycle)

- `ProjectID` + `Model` 字段在 `AuthState` struct 里预留, 留给 cycle 46 由 backend.Manager 在 streamGenerateContent 返 project number 时填入
- v1.3.5 binary mTLS 路径 (`mtls.googleapis.com/token`) 暂未在 `defaultAuthLoader` 走 — 当前只走 daily-cloudcode-pa (跟 v1.3.5 binary 的 INELIGIBLE 路径 1:1)
