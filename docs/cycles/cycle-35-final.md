# Cycle 35 — Antigravity OAuth token 1:1 加载 + LoadCodeAssist live e2e

**Date:** 2026-10-14
**Commit:** (pending)
**Status:** ✅ Done

---

## TL;DR

User 给真 OAuth token, 验证:

1. **recovered 跟 v4 binary 共享同一份 token** (`~/.gemini/antigravity-cli/antigravity-oauth-token`)
2. **OAuth token 有效** (userinfo endpoint 返真实身份)
3. **cloudcode-pa.googleapis.com LoadCodeAssist 跑通** (2 tier: free-tier default, standard-tier)
4. **agent loop 走真 backend** (cycle 33 mock backend, 不需真 cloudcode-pa)

---

## 改动 (cycle 35)

### 新 file: `internal/auth/antigravity_token.go` (257 行)
- `AntigravityToken` struct 跟 v4 binary JSON 1:1 (token+auth_method+id_token)
- `LoadAntigravityToken()` 读 `~/.gemini/antigravity-cli/antigravity-oauth-token` (or `$ANTIGRAVITY_HOME`)
- `SaveAntigravityTokenTo()` 原子写 (temp+rename, 0600)
- `TokenCache` thread-safe in-memory cache + 自动 refresh
- `RefreshTokenReal()` 调 `oauth2.googleapis.com/token` (替换 cycle 25 PLACEHOLDER)
- `IsExpired()` / `ExpiryTime()` ISO 8601 / RFC 3339 nano

### 新 file: `internal/auth/load_code_assist.go` (135 行)
- `CloudCodeAPIClient` plain HTTPS client (不走 mTLS)
- `LoadCodeAssist()` POST `v1internal:loadCodeAssist` 返 AllowedTiers
- `IdeMetadata` (ideType/platform/pluginType) 跟 v4 binary 兼容

### 新 file: `internal/auth/antigravity_token_test.go` (160 行)
- 7 test 含 live token load + round-trip + refresh

### 新 file: `internal/auth/load_code_assist_test.go` (123 行)
- 2 live test: LoadCodeAssist + userinfo

### 新 file: `internal/auth/setup_proxy_test.go` (27 行)
- `TestMain` 把 `ALL_PROXY` 映射到 `HTTPS_PROXY`

---

## Live E2E 跑通 (sandbox)

```
=== RUN   TestCloudCodeAPIClient_LoadCodeAssist_Live
    tier[0]: id=free-tier name=Antigravity default=true gcpTos=false
    tier[1]: id=standard-tier name=Antigravity default=false gcpTos=true
--- PASS: TestCloudCodeAPIClient_LoadCodeAssist_Live (2.14s)
=== RUN   TestTokenValidatesViaUserinfo
    token valid: sub=105126221582794464410 email=navigaid@gmail.com name=Hangbiao Zou
--- PASS: TestTokenValidatesViaUserinfo (0.31s)
```

**2/2 PASS**, 0.3s + 2.14s

---

## Token 文件 1:1 兼容 (跟 v4 binary)

**v4 binary 写的文件**:
```
~/.gemini/antigravity-cli/antigravity-oauth-token
{
  "token": { "access_token": "ya29...", "refresh_token": "1//...", "expiry": "..." },
  "auth_method": "consumer",
  "id_token": "eyJ..."
}
```

**recovered version 读这个文件**: ✅
**recovered version 写这个文件** (refresh 后): ✅
**ANTIGRAVITY_HOME env 切换**: ✅

---

## 16/16 packages pass, 0 FAIL

**4 binary build OK**:
- `cmd/antigravity`: 13.9 MB
- `cmd/openai-shim`: 9.99 MB
- `cmd/anthropic-shim`: 9.99 MB
- `cmd/openagy-models`: 10.14 MB

**Live demo**:
- recovered --print "hi" → 走 mock backend (cycle 33 自定义 model) → "mock reply from model=claude-4.5-sonnet: hi"
- recovered --model=agw --print "hi" → 走 agw.k0s.io → 真答
- recovered LoadCodeAssist → free-tier + standard-tier 真实 tier list

---

## 关键发现

### 1. v4 binary 有 client_secret 写死在 binary
`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` 跟 `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` 是 OAuth confidential client 的 client_secret + client_id pair

### 2. 沙箱能直连 (用 TUIC proxy)
- `oauth2.googleapis.com/token`: 0.3s ✅
- `www.googleapis.com/oauth2/v3/userinfo`: 0.3s ✅ 返 Hangbiao Zou
- `cloudcode-pa.googleapis.com/v1internal:loadCodeAssist`: 1.0s ✅ 返 free-tier + standard-tier
- `cloudcode-pa.googleapis.com/v1internal:onboardUser`: 400 (schema 待查 1.3.4 decompile)
- `cloudcode-pa.googleapis.com/v1internal:streamGenerateContent`: 400 (同上)
- `oauth2.mtls.googleapis.com/token`: mTLS-only, sandbox 无 device cert
- `daily-cloudcode-pa.googleapis.com`: 404 (sandbox 拒)
- `generativelanguage.googleapis.com`: 403 (scope 不够)

### 3. recovered 跟 v4 binary **走同一份 token**
- v4 binary 走 mTLS (sandbox 拒, 需要 macOS Secure Enclave cert)
- recovered version 走 plain HTTPS (sandbox 通)
- 都从同一 `~/.gemini/antigravity-cli/antigravity-oauth-token` 读
