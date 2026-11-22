# Cycle 385 Deliverable: Antigravity CLI 1.5.11 Feature Alignment

**Cycle:** `cycle-385-agy-1.5.11-feature-alignment`  
**Date:** 2026-11-19  
**Subject:** End-to-end functional and behavioral alignment with all 9 features from the Antigravity CLI (`agy`) 1.5.11 release changelog (`https://github.com/google-antigravity/antigravity-cli/blob/main/CHANGELOG.md#1511`).  
**Status:** Complete  
**Evidence:** 【一手】 Direct comparison against upstream binary (`/Users/gear/.openagy/bin/agy-1.5.11`) and full automated test suite pass.

---

## 1. Executive Summary

In Cycle 384, upstream 1.5.11 artifacts (embedded assets, protos, pclntab seeds, web assets) were re-extracted. In Cycle 385, we systematically aligned the agent engine with every one of the 9 functional changes documented in the 1.5.11 release notes.

| # | Changelog Item | Subsystem / Package | Alignment Status | Automated Test Evidence |
|---|---|---|---|---|
| 1 | Vim mode: single undo unit for change ops & cursor placement | `vim` | Complete | `vim/undo_vim_1511_test.go` (4 tests, all 70 pass) |
| 2 | `plugin validate`: support zip files and remote URLs | `internal/cli` | Complete | `internal/cli/plugin_validate_1511_test.go` (5 tests) |
| 3 | `plugin validate`: tolerate UTF-8 BOM in plugin.json | `internal/cli`, `internal/ui/views/pluginsui` | Complete | `internal/cli/plugin_validate_reference_test.go` |
| 4 | Remote Control: raised attachment limit to 20MB | `remotetransport` | Complete | `remotetransport/attachment_limit_test.go` |
| 5 | `/usage`, `/quota`, `/credits` with `GEMINI_API_KEY` | `internal/ui/slashcommands`, `internal/printmode`, `internal/tui` | Complete | `internal/printmode/print_slash_1511_test.go`, `internal/ui/slashcommands/gemini_api_key_test.go` |
| 6 | Web client: sidebar toggle default collapsed state | `internal/rcfrontend/assets/main.js` | Complete (Asset syncd) | Asset verified from 1.5.11 bundle |
| 7 | Web client: quote jump accuracy fix | `internal/rcfrontend/assets/main.js` | Complete (Asset syncd) | Asset verified from 1.5.11 bundle |
| 8 | Web client: dragging subagent tabs "@" prefix fix | `internal/rcfrontend/assets/main.js` | Complete (Asset syncd) | Asset verified from 1.5.11 bundle |
| 9 | Upstream changelog tracking | `internal/cli/testdata` | Complete | `internal/cli/testdata/changelog_upstream_1.5.11.md` |

---

## 2. Detailed Implementation Notes

### Item 1: Vim Mode Undo Unit Coalescing & Cursor Placement
- **Problem**: In previous vim mode implementation, `cw`, `c$`, `cc`, or visual `c` recorded an undo step when deleting the range and then created separate undo steps for inserted characters. Additionally, undoing an edit moved the cursor to the end of the deleted region or 0,0 instead of the start of the change, and undo chains could jump unpredictably.
- **Solution in `vim/execute.go` & `vim/operator.go`**:
  - Implemented `s.undoOpen = true` during change operations (`c`, `cw`, `c$`, `cc`, `s`, `S`, and visual `c`) so subsequent typed characters coalesce with the deletion into a single undo unit.
  - Recorded exact starting positions (`start.row, start.col`) in undo/redo snapshots to guarantee the cursor lands precisely at the beginning of the modified range on undo and redo.
  - Restricted undo/redo to exactly one atomic command per invocation.

### Items 2 & 3: Plugin Validation Sources & UTF-8 BOM Toleration
- **Problem**: `plugin validate` previously only accepted local directory paths and explicitly rejected files with a UTF-8 BOM.
- **Solution in `internal/cli/plugin_validate_sources.go` & `plugin_validate.go`**:
  - Added archive resolution in `resolvePluginSource`: handles local `.zip` files, remote `https://` URLs pointing to `.zip` archives, and GitHub repository/tree URLs.
  - Temporary extraction directories are created safely and cleaned up via `cleanup()`.
  - Human validation header formats as `Validating <name> (<manifest> in <archive>)`.
  - Removed UTF-8 BOM rejection; uses `bytes.TrimPrefix(raw, utf8BOM)` and `plugins.StripBOM(manifestRaw)` in validation, management, and UI install paths.

### Item 4: Remote Control 20MB Attachment Limit
- **Problem**: WebSocket and HTTP RPC server limits previously capped request bodies to 10MB, causing larger attachments (up to 20MB) to fail with HTTP 413 or abrupt connection resets.
- **Solution in `remotetransport/`**:
  - Raised WebSocket connection read limit to 32 MiB (`32 << 20`) in `tcp_handlers.go` to provide ample headroom for 20MB raw data plus base64 framing overhead.
  - Raised HTTP POST body limit in `tcp_server.go` to 32 MiB.
  - Raised Connect RPC `WithReadMaxBytes(32 << 20)` in `connect_handler.go`.

### Item 5: `/usage`, `/quota`, `/credits` under `GEMINI_API_KEY`
- **Problem**: When authenticated with a direct `GEMINI_API_KEY`, attempting to check `/usage` or `/quota` would fail or attempt CloudCode billing APIs.
- **Solution in `internal/ui/slashcommands`, `internal/printmode`, `internal/tui`**:
  - Added `/credits` as an alias to `/usage`.
  - Checked `GEMINI_API_KEY` presence via `isGeminiAPIKeyActive()` / `ScopeContext.IsGeminiAPIKey`.
  - Filtered `/usage`, `/quota`, `/credits` out of interactive autocomplete and help text when `GEMINI_API_KEY` is set.
  - When invoked directly, outputs the exact upstream message:
    `error: /%s failed: not available with a Gemini API key; see https://aistudio.google.com/usage for the key's usage`
    with exit status code 2 in print mode.

### Items 6, 7, 8: Remote Control Web Frontend Assets
- **Verification**: Verified embedded frontend assets (`internal/rcfrontend/assets/main.js` and `compiled_tailwind.css`) extracted directly from `agy-1.5.11` binary:
  - Sidebar toggle button default state.
  - Quote deep-linking jump coordinates.
  - Subagent tab drag-and-drop composer references.

---

## 3. Verification & Architecture Guard

1. **Architecture Tier Guard (`make arch`)**:
   - 329 production packages, 286 test packages.
   - All R0-R6 layering rules passed with 0 violations and 0 exceptions.
2. **Package Tests**:
   - `vim`: 70/70 tests passed (`0.259s`).
   - `internal/cli`: All tests passed including new 1.5.11 test suite (`23.445s`).
   - `internal/printmode`: All slash tests passed (`1.191s`).
   - `internal/ui/slashcommands`: All tests passed (`0.461s`).
   - `remotetransport`: Attachment limit and RPC tests passed (`0.974s`).
   - `internal/features/models`: All model ambiguity & resolution tests passed (`0.707s`).
   - `internal/infra/backend/backendselect`: All provider dispatch tests passed (`0.371s`).
