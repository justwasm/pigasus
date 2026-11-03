# Cycle 377 Deliverable: Alignment with Antigravity CLI 1.4.12

**Cycle:** `cycle-377-agy-1.4.12-alignment`  
**Date:** 2026-10-31  
**Subject:** Alignment with upstream Antigravity CLI (`agy`) version `1.4.12` release.  
**Evidence:** 【一手】 first-hand against the installed `1.4.12` reference binary (`agy --version`, `agy changelog`, pclntab seeds, binary strings) · 【反编译】 Ghidra decompilation of `readPluginAuthFile` and `TranslatePluginAuth` (`google3/third_party/jetski/cortex/utils/utils`) · 【strings】 resolved string-table hits in CLI and IDE language server · 【推测】 hypothesis.

---

## 0. The Short Answer

| # | Upstream Change (1.4.12) | Reference Mechanism | Clone Implementation |
|---|---|---|---|
| 1 | **Added:** `"optional": true` for API key providers in a plugin's `auth.json`, for services that work without a key but raise rate limits with one: when you haven't set the key, the plugin's commands now see the variable as unset and make unauthenticated requests at the free rate limit, instead of failing with 401 errors or asking you to sign in. | `cortex/utils/utils.readPluginAuthFile`, `TranslatePluginAuth`, `translateProvider`, `PluginAuthProviderInfo` struct carrying `Optional bool` | `internal/features/plugins/core/auth.go`: `PluginAuthProvider` with `Optional bool`, `ReadPluginAuth`, `ResolvePluginAuthEnv` leaving unset variable instead of erroring when `Optional: true`; documentation in `agy-customizations/docs/plugins.md` (§2) |
| 2 | **Fixed:** Markdown tables in long streaming replies leaving misaligned borders and repeated text in your terminal scrollback: a table's rows now stay in the live view until the table is complete, so they reach scrollback with their final column widths. | `google3/third_party/jetski/cli/viewport/viewport.capAtHeldLines`, `computeSplitPoint`, `prepareScrollbackContent` | `internal/ui/foundations/viewport/window.go`: `CapAtHeldLines`, `IncompleteTableLines` preventing split point from cutting across open markdown table rows (§3) |
| 3 | **Version Bump:** `1.4.11` → `1.4.12` | `~/.local/bin/agy --version` prints `1.4.12` | `internal/cli.ReferenceVersion = "1.4.12"`, `npm/pigasus` `1.4.12`, `cmd/pigga` `1.4.12-wasm`, `AGENTS.md`, `README.md` (§4) |
| 4 | **Changelog Feed Re-Capture** | `agy changelog` now serves 82 sections (including `1.4.11:` section) | `internal/cli/testdata/changelog_reference.txt` (188,429 B / 1,069 lines / 82 sections / 906 bullets), `changelog_upstream_1.4.12.md` (§4) |
| 5 | **Reference Drift Lock** | Gate in `internal/referencedrift` tracks version drift | `internal/referencedrift/reference.lock.json` re-sealed at `1.4.12` without lag (§4) |

---

## 1. Upstream 1.4.12 Release Notes Provenance

From [official changelog](https://github.com/google-antigravity/antigravity-cli/blob/main/CHANGELOG.md#1412) (recorded verbatim in `internal/cli/testdata/changelog_upstream_1.4.12.md`):

> - **Added:** `"optional": true` for API key providers in a plugin's `auth.json`, for services that work without a key but raise rate limits with one: when you haven't set the key, the plugin's commands now see the variable as unset and make unauthenticated requests at the free rate limit, instead of failing with 401 errors or asking you to sign in.
> - **Fixed:** Markdown tables in long streaming replies leaving misaligned borders and repeated text in your terminal scrollback: a table's rows now stay in the live view until the table is complete, so they reach scrollback with their final column widths.

---

## 2. Item 1: Plugin `auth.json` Optional API Keys

### 2.1 Binary Evidence

Binary symbols in `~/.local/bin/agy` (v1.4.12):
- `google3/third_party/jetski/cortex/utils/utils.readPluginAuthFile`
- `google3/third_party/jetski/cortex/utils/utils.TranslatePluginAuth`
- `google3/third_party/jetski/cortex/utils/utils.translateProvider`
- `slices.SortFunc[go.shape.[]google3/third_party/jetski/cortex/utils/utils.PluginAuthProviderInfo,go.shape.struct { Plugin string; Provider string; Type string; Domains []string; Scopes []string; StorageKey string; Connected bool; CLIs []string; Accounts []...; Optional bool }]`

Recovered strings:
- `"unsupported provider type %q (want \"oauth2\" or \"api_key\")"`
- `"auth.json declares no providers"`
- `"empty entry in \"clis\""`
- `"%s needs authentication (%s). Ask the user to sign in to %s in the Plugins Manager, then try again"`

### 2.2 Implementation

Implemented in `internal/features/plugins/core/auth.go`:
- `PluginAuthDoc`: root structure of `<plugin_dir>/auth.json`.
- `PluginAuthProvider`:
  ```go
  type PluginAuthProvider struct {
      Type       string              `json:"type"` // "api_key" or "oauth2"
      Optional   bool                `json:"optional,omitempty"`
      Env        map[string]string   `json:"env,omitempty"`
      Domains    []string            `json:"domains,omitempty"`
      Scopes     []string            `json:"scopes,omitempty"`
      CLIs       []string            `json:"clis,omitempty"`
      MCPServers []string            `json:"mcpServers,omitempty"`
      OAuth2     *PluginOAuth2Config `json:"oauth2,omitempty"`
  }
  ```
- `ReadPluginAuth(pluginDir string) (*PluginAuthDoc, error)`:
  Reads and validates `<pluginDir>/auth.json` using disallow-unknown-fields JSON decoding.
- `ResolvePluginAuthEnv(pluginName, commandName, providerName string, provider *PluginAuthProvider, getSecret func(string) (string, bool)) (map[string]string, error)`:
  - If a secret is absent and `provider.Optional == false`: returns reference error message `"%s needs authentication (%s). Ask the user to sign in to %s in the Plugins Manager, then try again"`.
  - If a secret is absent and `provider.Optional == true`: leaves variable unset in the output map without error, allowing unauthenticated requests at the free rate limit.
- Documented in `internal/features/tools/skills/skills/agy-customizations/docs/plugins.md` under `## Authentication (auth.json)`.

---

## 3. Item 2: Markdown Tables in Live View / Scrollback

### 3.1 Binary Evidence

Binary symbols in `~/.local/bin/agy` (v1.4.12):
- `google3/third_party/jetski/cli/viewport/viewport.capAtHeldLines`
- `google3/third_party/jetski/cli/viewport/viewport.computeSplitPoint`
- `google3/third_party/jetski/cli/viewport/viewport.prepareScrollbackContent`
- `google3/third_party/jetski/cli/viewport/viewport.buildScrollbackContent`

### 3.2 Implementation

Implemented in `internal/ui/foundations/viewport/window.go`:
- `CapAtHeldLines(splitPoint, totalLines, heldLines int) int`:
  Caps the scrollback split point so that lines at the bottom of the buffer (such as incomplete table rows) remain in the live viewport rather than pushing to scrollback before final column widths are calculated.
- `IncompleteTableLines(rows []string) int`:
  Detects trailing markdown table lines (delimited with `|` and not terminated by a blank line) to ensure open table rows stay in the live viewport.
- Tested in `internal/ui/foundations/viewport/tuiviewport_test.go` (`TestCapAtHeldLines`, `TestIncompleteTableLines`).

---

## 4. Version Bump & Reference Drift Lock

- `internal/cli/version.go`: bumped `ReferenceVersion = "1.4.12"`.
- `internal/cli/testdata/changelog_reference.txt`: re-captured from installed `agy 1.4.12`.
  - Size: `188,429` bytes
  - Lines: `1,069` lines
  - Sections: `82` (`1.4.11:` down to `1.0.0:`)
  - Bullets: `906`
  - SHA256: `47d411f035301b9e1b2ae35f6c51e49b06915ca0b01d441ca7765f88f6b8b70e`
- `internal/cli/changelog_data_test.go`: updated constants and section start expectation (`1.4.11:\n`).
- `internal/cli/changelog_v133_test.go`: updated newest bullet check.
- `internal/referencedrift/reference.lock.json`: re-sealed at `1.4.12`.
- `npm/pigasus/package.json` & `npm/pigasus/lib/download.js`: bumped to `1.4.12`.
- `cmd/pigga/main.go` & `cmd/pigga/pigga_test.go`: bumped to `1.4.12-wasm`.
- `README.md` & `AGENTS.md`: updated references to `1.4.12`.

---

## 5. Verification

1. `go test -v ./internal/cli/ -run 'TestChangelog|TestReferenceVersion'`: **PASS**
2. `go test -v ./internal/referencedrift/`: **PASS**
3. `go test -v ./internal/features/plugins/core/`: **PASS**
4. `go test -v ./internal/ui/foundations/viewport/`: **PASS**
5. `go test -v ./cmd/pigga/ -run TestPiggaNativeVersion`: **PASS**
6. `go build -o /tmp/pigasus ./cmd/pigasus`: **PASS** (`/tmp/pigasus --version` prints `1.4.12`).
