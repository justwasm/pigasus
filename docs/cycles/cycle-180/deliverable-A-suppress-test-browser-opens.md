# Cycle 180 — deliverable A: suppress browser opens during `go test`

## Scope

`go test ./...` on this machine was launching the user's default
browser to **`https://example.test/device`** on every `runLogin`
end-to-end run, plus once per `TestOpenBrowser_NoPanic`. The
side-effect was independent of the test logic — it came from the
production OAuth path correctly calling `OpenBrowser(verificationURL)`
when `OPENAGY_NO_BROWSER` was not set. Cycle 180 closes the leak in
two places: gate the legacy `auth.OpenBrowser` so it honours the same
env var that `cli.BrowserAutoOpen` reads, and have the runLogin glue
tests opt into the gate.

This is a pure test-hygiene fix. No user-facing behaviour change in
the production CLI; before cycle 180 the env-var gate was honoured
in `cli.OpenBrowser`'s dispatch caller (runLogin), but the legacy
`auth.OpenBrowser` shim (kept for backwards-compat with cycle 25
callers, none of which exist in this repo) ignored it.

## What changed

| File | What | Net |
|---|---|---|
| `internal/auth/oauth.go` | `OpenBrowser` now reads `$OPENAGY_NO_BROWSER` the same way `cli.OpenBrowser`'s `BrowserAutoOpen` does (recognises `1` / `true` / `TRUE` / `True` / `yes` / `YES`). Empty-URL early-returns nil so the cross-platform launcher never spawns for the noop case. | +21 / −2 |
| `internal/auth/oauth_test.go` | `TestOpenBrowser_NoPanic` (which literally invoked `open https://example.com` on macOS, leaving a stray browser tab every test run) is replaced by `TestOpenBrowser_RespectsNoBrowserEnvVar` (six truthy values, all gated) and `TestOpenBrowser_EmptyURLIsNoop` (no env, empty URL, no spawn). `time` import dropped — unused after the rewrite. | +30 / −9 |
| `internal/cli/login_flow_test.go` | `TestRunLogin_EndToEnd`, `TestRunLogin_DeviceFlowError`, and `TestRunLogin_RendersMarkdownPrompt` all `t.Setenv("OPENAGY_NO_BROWSER", "1")` before calling `runLogin`. They still verify the printed prompt, the verification URL rendering, the user-code line, and the dispatch-table registration; only the platform-launcher side-effect is suppressed. | +19 / −1 |
| `cmd/wagyu/tui_tools_test.go` | `TestLooksLikeMarkdown/inline_link` fixture URL swapped from `https://example.com` to `https://example.invalid/page`. `.invalid` is reserved by RFC 6761 (together with `.test`, `.example`, `.localhost`) to be guaranteed non-resolvable, which keeps the test string out of any personal-domain-name context the editor / terminal might smart-link-detect during a test run. | +1 / −1 |

## The two leak paths and why they were both triggering on this machine

### Path 1 — `internal/auth/oauth_test.go::TestOpenBrowser_NoPanic`

The cycle-25 shim test called `OpenBrowser("https://example.com")`
unconditionally. On macOS the function routes through
`exec.Command("open", url).Start()`, which is the platform's
"open this URL in the user's default browser" launcher. So every
`go test ./internal/auth/` invocation spawned `open
https://example.com` and left a tab in the user's running browser.
The function had no env-var guard — it predated the cycle 172
`OPENAGY_NO_BROWSER` contract added to `cli.OpenBrowser`.

Cycle 180 fix:

```go
func OpenBrowser(url string) error {
    if url == "" {
        return nil
    }
    if v := os.Getenv("OPENAGY_NO_BROWSER"); v != "" {
        switch v {
        case "1", "true", "TRUE", "True", "yes", "YES":
            return nil
        }
    }
    // … unchanged cross-platform launcher dispatch
}
```

`TestOpenBrowser_RespectsNoBrowserEnvVar` exercises every
recognised truthy value and confirms the gate returns nil; the
existing `TestBrowserAutoOpen_*` and
`TestRunLogin_BrowserOpenHonoursEnvVar` in `internal/cli/open_browser_test.go`
remain the authoritative coverage for `cli.BrowserAutoOpen`.

### Path 2 — `internal/cli/login_flow_test.go::TestRunLogin_*`

`runLogin` (cycle 170) drives the OAuth device-flow and on the
`OnAuthRequest` callback calls `OpenBrowser(verificationURL)`
when `BrowserAutoOpen(false)` returns true. The fake device
server at `newFakeDeviceServer` returns
`"verification_uri": "https://example.test/device"`, so on every
test run the production path spawns `open
https://example.test/device`. Three test functions exercise this
branch:

- `TestRunLogin_EndToEnd` (asserts the prompt, URL, code, and
  token-on-disk)
- `TestRunLogin_DeviceFlowError` (asserts the access_denied error
  surfaces)
- `TestRunLogin_RendersMarkdownPrompt` (asserts the cycle 171
  Markdown-rendered heading)

Each test now begins with `t.Setenv("OPENAGY_NO_BROWSER", "1")`
(Go's `t.Setenv` cleans up automatically via `t.Cleanup`, so the
env var is scoped to the test lifetime and never leaks between
tests or between runs). `TestRunLogin_RegisteredInDispatchTable`
does not call `runLogin` so it needs no env change.

The companion `TestRunLogin_BrowserOpenHonoursEnvVar` in
`internal/cli/open_browser_test.go` already sets the env var
explicitly — that is the test the cycle 172 contract was built
around, and it stays green.

## Verification

```text
$ go test ./internal/auth/ ./internal/cli/ -count=1
ok      github.com/chadgpt/openagy/internal/auth      26.5s
ok      github.com/chadgpt/openagy/internal/cli       20.9s   (pre-existing TestChangelog_MatchesReferenceBinary still fails; unrelated)
```

Manual confirmation on the user's machine: with `OPENAGY_NO_BROWSER`
unset (the pre-cycle-180 default state), `go test ./...` no longer
opens a `https://example.test/device` tab or a
`https://example.com` tab. The test results above were produced on
the user's actual machine; prior to the fix those runs left
browser tabs open in Chrome.

## 硬约束 / 存疑

- **Production callers of `auth.OpenBrowser`**: a repo-wide grep
  for `auth.OpenBrowser` (production code, not tests) finds no
  callers. The function is a backwards-compat shim for cycle 25
  callers that have since been deleted. Adding the env-var gate is
  therefore risk-free with respect to any in-repo production path;
  the change matches what `cli.OpenBrowser` already does via
  `BrowserAutoOpen`. Verified: `grep -R 'auth\.OpenBrowser' --include='*.go'` returns only `internal/auth/oauth.go`
  (definition), `internal/auth/oauth_test.go` (test), `go-recovered/`
  + `re-docs/` (static recovery notes from the decompile of the
  original `google3/third_party/jetski/cli/backend/auth/auth.OpenBrowser`).
- **Coverage loss on actual `cmd.Start()` plumbing**: the original
  `TestOpenBrowser_NoPanic` was the only test that verified the
  cross-platform launcher dispatch at the unit level (it didn't
  actually verify the browser closed, just that the spawn
  succeeded or failed without panic). Cycle 180's replacement
  tests verify only the env-var gate. The actual
  `exec.Command(name, args...).Start()` plumbing for
  `cli.OpenBrowser` is covered by the integration-style
  `TestRunLogin_BrowserOpenHonoursEnvVar` and by
  `TestBrowserLauncher_KnownGOOS` (which checks the launcher name
  per GOOS). The cross-platform launch for `auth.OpenBrowser`
  specifically is now an uncovered edge — acceptable trade for
  tests that don't open browser tabs.
- **`example.test` strings elsewhere in the repo**: a repo-wide
  grep returns seven additional `.go` files that contain
  `example.com` / `example.test` / `example.invalid` strings.
  None of them are routed through `OpenBrowser` in tests — the
  strings are fixture data for git author emails, MCP server
  configs, and Markdown rendering previews (which copy text
  through mdrender without any network). They do not trigger
  browser opens. Cycle 180 only sanitises the one fixture URL
  that the user reported was rousing the editor's smart-link
  detector; the other six are intentionally left as-is because
  they are far from any platform-launcher call site.
