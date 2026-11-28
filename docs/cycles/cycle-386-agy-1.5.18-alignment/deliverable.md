# Cycle 386 Deliverable: Alignment with Antigravity CLI 1.5.18

**Cycle:** `cycle-386-agy-1.5.18-alignment`
**Date:** 2026-11-26
**Subject:** Re-extraction of every reverse-engineering artifact against upstream
Antigravity CLI (`agy`) `1.5.18`, plus implementation of the four 1.5.18 fixes
that are observable in the clone.
**Evidence:** 【一手】 first-hand against the installed `1.5.18` reference binary
(`agy --version`, `agy changelog`, pclntab seeds, `//go:embed` records, embedded
ZIP, descriptor protos) · 【对比】 byte-level diff of the 1.5.17 and 1.5.18
artifact sets · 【推测】 hypothesis.

---

## 0. The Short Answer

| # | What | 1.5.17 | 1.5.18 |
|---|---|---|---|
| 1 | Reference binary | 226,898,720 B, sha256 `e6cabc85…` | **228,748,896 B**, sha256 `89d0b97a9ebcefcaebbdfe4ffeb2de877ef92ee4504de188e35a38489136be13` |
| 2 | pclntab seeds | 187,959 names | **190,274** names (+2,471 / −156) |
| 3 | `//go:embed` tree | 450 files | **450 files**, one changed (`swarm_board/app.js`) |
| 4 | Embedded web ZIP | 548 files, 12.98 MB `main.js` | **13.15 MB `main.js`** (+104,686 B) |
| 5 | Descriptor protos | `cortex` 516 msg / 90 enums, `remote_agent_proxy` 391 / 26 | **`cortex` unchanged; `remote_agent_proxy` 406 msg / 26 enums; `remote_agent_proxy_pb` 18 msg / 9 enums** |
| 6 | Served changelog feed | 259,799 B / 1,352 lines / 98 sections / 1,157 bullets | **273,878 B / 1,407 lines / 101 sections / 1,206 bullets** |
| 7 | Behavioural pin (`version.Reference`) | 1.5.17 | **1.5.18** |

The upstream auto-updater manifest for this machine reports `1.5.18` as the
100 %-rolled-out stable release:

```json
{
  "version": "1.5.18",
  "url": "https://storage.googleapis.com/antigravity-public/antigravity-cli/1.5.18-6386224290725888/darwin-arm/cli_mac_arm64.tar.gz",
  "sha512": "d10e124f5eb4da94ebb6c494cd2e7b38744f9f66e8e87145d2cbad1cbd3c4160fd88a79cd4220dfce7c9da0ddead652346bcc839914575f6447438ae8d1d5dcc"
}
```

---

## 1. Artifact Re-Extraction

```bash
# extraction (67 MB, 1,002 files)
python3 scripts/re/extract_agy.py --bin ~/.local/bin/agy
# → out/extracted-1.5.18/{identity.txt,seeds,embed,protos,web-assets,
#                          model_profiles_1.5.18.json,changelog_1.5.18.txt}

# release notes
python3 scripts/re/fetch_changelog.py --version 1.5.18
# → internal/cli/testdata/changelog_upstream_1.5.18.md (7 bullets, 1,318 B)

# the served changelog feed, captured from a throwaway HOME
HOME=$(mktemp -d) ~/.local/bin/agy changelog > internal/cli/testdata/changelog_reference.txt
# → 273,878 B / 1,407 lines, sha256 ef967da81399ec9b…
```

`identity.txt` records `version: 1.5.18`, `size_bytes: 228748896`,
`sha256: 89d0b97a9ebcefca…`.

### `agy changelog` is fetched, not embedded

Earlier cycles called this an "embedded changelog feed". It is not. The binary
fetches the vendor's public `CHANGELOG.md`, caches it, drops the newest section,
and re-renders the rest (`## X.Y.Z` → `X.Y.Z:`, `- ` → `· `, blank line between
sections). Rebuilding the 1.5.18 fetch that way is **byte-identical** to
`agy changelog` (273,878 B), which is what makes the fixture re-evidenceable on
a machine whose cache is stale — and which is why the 1.5.17 fixture was
actually a 2026-11-22 cache rather than a 1.5.17 fetch.

Five constants moved in `internal/cli/changelog_data_test.go` (byte length, line
count, section count, bullet count, sha256) and the shape gate's expected first
header moved `1.5.14:` → `1.5.17:`.

### Version pin

`internal/core/version/version.go` moved to `const Reference = "1.5.18"`.
`internal/features/compat/agy` reads `version.Reference` rather than repeating
the literal (a 1.5.17 change), so no second site needed editing.

---

## 2. Descriptor & Symbol Delta

### `remote_agent_proxy` (`google.gca.aicode.v1main`): 391 → 406 messages

New: `ManageConversationSidecar`, `ListConversationSidecars`,
`GetConversationSidecarLogs`, `LaunchMcpApp`, `CallMcpAppTool`,
`GetMcpAppResource`, `ListMcpServerTools` (each with `Request`/`Response`) and
`McpServerToolInfo`.

### `remote_agent_proxy_pb` (`exa.remote_agent_proxy_pb`): 14 → 18 messages, 7 → 9 enums

New messages: `ConversationSidecarIngress`, `PluginSidecarSummary`,
`PluginVariable`, `PluginVariableSpec`.
New enums: `ConversationSidecarAction`, `ConversationSidecarCallbackMode`.

### Unchanged

`cortex` (516 messages / 90 enums), `trajectory` (5 / 1), `trajectory_steps`
(17 / 0), `cascade_plugins`.

### Language server

Gained `ListAgentWorkspacePermissions` / `SetAgentWorkspacePermissions` and the
`AgentWorkspacePermission` enum.

### Embedded `model_profiles.json`

Gained `base.remote-human-identity-survey-percent: 30` and one science-shelf
plugin (`antigravity-plugins-official/plugin/mass-spectrometry`). Everything else
is byte-identical.

### Notable new symbols

`media.decodeImageMedia` is new and `media.ResolveMediaMetadata` is gone: the
reference now fully decodes an image before attaching it, which is the
"half-written image" fix. `fileRequiredPermissions` and
`model.ReadMediaFile` / `model.parseFileURI` are also new — the URI and
permission halves of the same release.

---

## 3. The Seven 1.5.18 Bullets

| # | Bullet | Verdict |
|---|---|---|
| 1 | Reject truncated or corrupt images when viewing files | **already correct; pinned** |
| 2 | Plugins on Windows loading across drives | **not applicable** (see §4) |
| 3 | File permission rules apply to `file:///` URIs | **real gap; fixed** |
| 4 | Copy from code blocks keeps blank lines | **bundle replaced** |
| 5 | LaTeX in subagent replies renders as math | **bundle replaced** |
| 6 | Shell variables not rendered as LaTeX | **bundle replaced** |
| 7 | Quota section recovers from a failed load | **bundle replaced** |

### §4 Bullet 2: why "not applicable"

The reference's fix is about `filepath.Rel` between a workspace on drive `D:`
and a configuration directory on `C:`. The clone has no such relativisation:
plugin roots come from `confhome.Gemini()` / `confhome.Antigravity()` and are
joined with `filepath.Join`, and `pluginimport.copyTree` walks a source tree
into a destination with `filepath.Rel(src, path)` where both are under the same
import root. There is no cross-root arithmetic to correct, so there is nothing
to change and nothing to test. This is recorded rather than silently skipped.

### Bullet 1: the image probe was already right, and that was the risk

`imagery.Prepare` decodes before `view_file` returns a `MediaOutput`, so a
truncated PNG/JPEG already failed. But nothing pinned it, and "the decoder
happens to reject it" rots. Measured on the clone:

```
j_full.jpg   read_err=false view_err=<nil>            attached=false
j_90.jpg     read_err=false view_err=…short Huffman…  attached=false
p_50.png     read_err=false view_err=…not enough pixel data  attached=false
```

`internal/app/tools/filesystem/image_attach_test.go` now asserts six broken
shapes (half-written PNG, signature-only, missing IEND, missing EOI, half-written
JPEG, valid-signature-invalid-checksum) never produce an attachment, plus the
control that a complete image produces exactly one.

### Bullet 3: the `file:///` bypass, measured

On the 1.5.17 clone:

```
target="/etc/passwd"                  verdict=deny
target="file:///etc/passwd"           verdict=allow      ← the bypass
target="file:///etc/passwd%20x"       verdict=allow
target="file://localhost/etc/passwd"  verdict=allow
```

and the tool half disagreed with the permission half:

```
read_file("file:///…/secret.txt")
  → open …/ws/file:/…/secret.txt: no such file or directory
```

i.e. the URI was joined onto the workspace as a relative name, so the engine
judged one file while the tool opened another.

The fix is a new **Tier-1** package, `internal/core/fileuri`, with one function,
`fileuri.Path`. It is deliberately syntactic — it decodes percent escapes,
collapses the empty and `localhost` authorities, keeps a named host spelled as a
UNC path so the credential-leak gate can still reject it, and never touches the
filesystem (permissions are decided before anything is opened).

Three call sites consume it:

| Layer | Site | Why |
|---|---|---|
| Permissions | `permissions.normalisePath` | a deny rule written against a path must cover the URI spelling |
| Permissions | `Policy.DecideCall`'s UNC gate | `file://server/share` is only a UNC path once resolved |
| Tools | `tools.ReadPath`, `tools.WorkspacePath` | the tool must act on the file the engine judged |

The lock records the reference's own error wording
(`invalid %s: header found at byte offset %d after leading non-image bytes`,
still present in both binaries) so the image probe's message stays a capture
rather than a paraphrase.

### Bullets 4–7: the bundle

`internal/rcfrontend/assets/main.js` and `compiled_tailwind.css` were replaced
verbatim from `out/extracted-1.5.18/web-assets/`
(13,149,963 B / 396,154 B; digests `fb6f082d…` / `f9296fe4…`, both matching the
extraction). `index.html` is the one file that is **not** a straight copy: the
upstream page has no `voice_client.js` tag and this repo serves that script, so
the tag was re-added by hand.

Each of the four bullets has an assertion in
`internal/rcfrontend/bundle_1_5_18_test.go`, and each is a *delta* between the
two releases rather than a string present in both:

| Bullet | 1.5.17 | 1.5.18 |
|---|---|---|
| Copy keeps blank lines | chrome selector without `[data-empty-line="true"]` | marker kept, plus a `.code-line` re-join pass |
| LaTeX in subagent replies | `F.createElement(g,{disableMath:!0},a)` | `F.createElement(h,{disableMath:f},a)` + `disableMath:!Scd(a)` |
| `$var` not math | `` c===")"||c==="/"||c==="*"? `` | `` …||c==="'" `` |
| Quota recovers | two `useState(void 0)`, spinner on `undefined` | `useGetModelQuotaQuery` + `isInitialized` + empty state + Refresh |

The bundle digest is also asserted, so a future hand patch into the old bundle
fails rather than producing a file that is neither release.

---

## 4. Verification

All commands run in this repository at `HEAD`, with
`CLOUD_CODE_URL=http://127.0.0.1:51558` set in the shell (the hostile
environment the 1.5.17 cycle established — endpoint resolution must survive it).

```
go test ./internal/core/... ./internal/app/tools/...            → ok
go test ./internal/core/policy/permissions/                     → ok
go test ./internal/core/fileuri/                                → ok
go test ./internal/app/tools/filesystem/                        → ok
go test ./internal/rcfrontend/                                  → ok (30.4s)
go test ./internal/cli/                                         → ok (23.7s)
go test ./internal/infra/referencedrift/                        → ok
go test ./internal/infra/referencedrift/ -run …NoUnacknowledgedDrift -v
    → PASS, 15 SUSPECT (all live-session-only captures; see the lock's lag_note)
```

`gofmt -l` on every file this cycle touched is empty; `go vet` is clean.

The drift gate now passes for a reason worth stating: `version` and the
`captured_at` ceiling moved together, six artifacts were re-evidenced against
the 1.5.18 extraction, and the fifteen that still need a live session are
reported as SUSPECT rather than claimed.

---

## 5. Deferred

1. **Conversation-sidecar and MCP-app RPCs** (seven), **`List/SetAgentWorkspacePermissions`**,
   and the `PolicyGuardian` additions. Each needs wire evidence and a live
   reference session; the account is suspended.
2. **Fifteen artifacts** still stamped `captured_at` ≤ 1.5.16. `lag_note` lists
   what each needs.
3. **`out/extracted-1.5.17/`** was retired in the same change as the pin, per
   the cycle's precedent. Every durable difference is in
   `docs/guide/reference-drift.md`.
