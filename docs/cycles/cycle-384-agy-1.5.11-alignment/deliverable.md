# Cycle 384 Deliverable: Alignment with Antigravity CLI 1.5.11

**Cycle:** `cycle-384-agy-1.5.11-alignment`
**Date:** 2026-11-19
**Subject:** Re-extraction of every reverse-engineering artifact against upstream Antigravity CLI (`agy`) `1.5.11`, and re-capture of the served changelog feed.
**Evidence:** 【一手】 first-hand against the installed `1.5.11` reference binary (`agy --version`, `agy changelog`, pclntab seeds, `//go:embed` records, embedded ZIP, descriptor protos) · 【对比】 byte-level diff of the 1.5.10 and 1.5.11 artifact sets · 【推测】 hypothesis.

---

## 0. The Short Answer

| # | What | 1.5.10 | 1.5.11 |
|---|---|---|---|
| 1 | Reference binary the artifacts come from | installed 1.5.10 | **226,097,264 B**, sha256 `62f2102784bb74b7743ce31f45e8414f1111dc99efb395a9e62690af0336c677` |
| 2 | pclntab seeds | 184,434 recovered names | **185,730** recovered names (190,954 raw entries) |
| 3 | `//go:embed` tree | 503 records / 444 files | **504 records / 445 files** (+`swarm_board/gif.js`) |
| 4 | Embedded web ZIP (index 1) | 548 files, 12.4 MB `main.js` | 548 files, **12.5 MB `main.js`** (+88,450 B) |
| 5 | Descriptor protos | `cortex` 510 msg, `remote_agent_proxy` 302 msg / 13 enums | **`cortex` 511 msg (+`CitationConfig`), `remote_agent_proxy` 306 msg / 14 enums** |
| 6 | Served changelog feed | 83 sections / 908 bullets (captured at 1.4.12) | **94 sections / 1091 bullets, 241,807 B / 1,278 lines** |
| 7 | Builtin skill bundle | 9 skills | **15 skills** |
| 8 | Behavioural pin (`cli.ReferenceVersion`) | 1.5.7 | 1.5.7 (unchanged — see §7) |

The upstream auto-updater manifest for this machine still reports `1.5.11` as the 100 %-rolled-out stable release:

```json
{
  "version": "1.5.11",
  "url": "https://storage.googleapis.com/antigravity-public/antigravity-cli/1.5.11-6132547114106880/darwin-arm/cli_mac_arm64.tar.gz",
  "sha512": "1b2c8e1f832e1be9b257c24e4ac75c9029771fef5af9b529ea295fff1aa5b9d3e1e5447200a76ac8a775889aa8399210716f188f84242b3c34a2e9a8a3c2cc7d"
}
```

---

## 1. Artifact Re-Extraction

Every command below was run against `/Users/gear/.openagy/bin/agy-1.5.11` — the versioned cache entry the `pigasus agy@1.5.11` launcher installs (`cbfbff72`, `c6bbb012`).

```bash
# seeds (pclntab → Ghidra seed file + symbol map)
(cd rea-go-seeds && go run . /Users/gear/.openagy/bin/agy-1.5.11 ../out/extracted-1.5.11/seeds)
#   gosym debug: funcs 190954 err <nil>
#   recovered 185730 function names

# //go:embed file systems (index 0) + the RC web frontend ZIP (index 1)
go run ./devutils/embedextract -bin /Users/gear/.openagy/bin/agy-1.5.11 \
  -embed-out out/extracted-1.5.11/embed \
  -zip-out  out/extracted-1.5.11/web-assets -zip-index 1
#   == //go:embed file systems: 504 records  → total: 504 files, 2,819,643 bytes
#   == embedded ZIP archives: 2
#      [0] offset=0x4581af9  entries=598  (tzdata)        sha256=b2d18a7c8fa81420
#      [1] offset=0xb3135f6  entries=548  (web frontend)  sha256=bde527ea2f814892

# descriptor protos (7 files, same set as 1.5.10)
python3 scripts/re/extract_descriptors.py /Users/gear/.openagy/bin/agy-1.5.11 \
  google/gca/aicode/v1main/remote_agent_proxy.proto \
  > out/extracted-1.5.11/protos/remote_agent_proxy_1.5.11.txt
#   … and the same for remote_agent_proxy_pb, cortex, cascade_plugins,
#   prompt, trajectory, trajectory_steps
```

`out/extracted-1.5.10/` was then retired: `out/extracted-1.5.11/` (67 MB) is the single active baseline.

---

## 2. Protocol Buffer Delta (1.5.10 → 1.5.11)

### 2.1 `third_party/jetski/cortex_pb/cortex.proto` — `exa.cortex_pb`

510 → **511** messages, 87 → 87 enums, 216,586 → **217,590 B**.

New message:

```
message CitationConfig {
    1 TYPE_BOOL enabled;
    2 repeated TYPE_STRING path_prefixes;
}
```

Changed messages:

| Message | Added field(s) |
|---|---|
| `SourceMetadata` | `4 conversation_id`, `5 step_index`, `6 root_conversation_id`, `7 root_actor_id`, `8 actor_id`, `9 .ContentKind content_kind` |
| `GrepToolConfig` | `10 CitationConfig citation_config` |
| `ViewFileToolConfig` | `22 CitationConfig citation_config` |
| `StepCompactionConfig` | `11 TYPE_BOOL trim_on_trajectory_size` |

Reading: tool results now carry *where they came from* (`conversation_id` / `step_index` / `root_actor_id` / `actor_id` / `content_kind`) so the model can cite a source; `citation_config` gates that per tool with a path-prefix allowlist; `trim_on_trajectory_size` is a second compaction trigger beside the existing step-count/size knobs.

### 2.2 `google/gca/aicode/v1main/remote_agent_proxy.proto` — `google.gca.aicode.v1main`

302 → **306** messages, 13 → **14** enums, 85,699 → **89,038 B**.

New messages / enum:

```
enum AgentConnectorMode {
  AGENT_CONNECTOR_MODE_UNSPECIFIED = 0;
  AGENT_CONNECTOR_MODE_READ_WRITE  = 1;
  AGENT_CONNECTOR_MODE_READ_ONLY   = 2;
}

message AgentConnectorAccess {
    1 TYPE_STRING surface;
    2 .google.gca.aicode.v1main.AgentConnectorMode mode;
}

message CatalogExtension {
    1 TYPE_STRING id;
    2 TYPE_STRING catalog;
    3 .CatalogExtension.Type type;
    4 TYPE_STRING name;
    5 .google.protobuf.Timestamp first_sync_time;
    6 .CatalogExtension.Details details;
    7 .CatalogExtension.Metrics metrics;
}

message SearchExtensionsRequest {
    1 TYPE_STRING query;
    2 repeated TYPE_STRING catalogs;
    3 repeated TYPE_STRING categories;
    4 .SearchExtensionsRequest.Order order;
    5 TYPE_INT32 page_size;
    6 TYPE_STRING page_token;
    7 repeated TYPE_STRING extension_ids;
    8 repeated TYPE_STRING exclude_categories;
}

message SearchExtensionsResponse {
    1 repeated .CatalogExtension extensions;
    2 TYPE_STRING next_page_token;
}
```

Changed messages:

| Message | Added field(s) |
|---|---|
| `AgentConnectors` | `4 repeated AgentConnectorAccess data_sources` |
| `AgentConnectorTrigger` | `8 surface`, `9 admin_paused` |
| `WatchTopic` | `6 .WatchTopic.ConversationVersionControl conversation_version_control`, `7 .WatchTopic.AgentCapsuleInbox agent_capsule_inbox` |

Reading: `SearchExtensions` is the server-side plugin/extension catalogue search the 1.5.11 RC web UI now exposes (`Filter and Sort`, `Category`, `Most popular first`, `No plugins available` — all new strings in `main.js`, see §4). `AgentConnectorAccess` generalises the previous GitHub-only connector block into per-surface read-write / read-only modes.

### 2.3 Unchanged packages

`remote_agent_proxy_pb` (11 msg / 3 enums), `trajectory` (5 msg / 1 enum), `trajectory_steps` (17 msg), `cascade_plugins` (13 msg), `prompt` (6 msg): byte-identical apart from the descriptor's file offset.

---

## 3. `//go:embed` Delta

444 → 445 files. One file added, five changed — all inside the Native Swarm plugin's `swarm_board` sidecar:

| File | Δ |
|---|---|
| `swarm_board/gif.js` | **new**, 17,527 B — the GIF89a encoder the sidecar now ships |
| `swarm_board/index.html` | +`#graph-export-gif` button, +`#gif-modal` preview dialog, +`<script src="/gif.js">` |
| `swarm_board/app.js` | +932 changed lines: graph-replay → GIF export (theme, "Hide inactive", "Expand @all", pacing, speed) |
| `swarm_board/styles.css` | +38 lines for the export button and modal |
| `swarm_board/README.md` | documents **Export GIF** and the new test ids |

Nothing else in the embed tree moved: the 279 chroma lexers, the 74 colour themes, the JSON-Schema metaschemas, the prompt templates, and the remaining skills are byte-identical.

---

## 4. Web Frontend Delta (`web-assets/`, ZIP index 1)

Two of 548 files changed:

| File | 1.5.10 | 1.5.11 | Δ |
|---|---|---|---|
| `main.js` | 12,395,847 B | 12,484,297 B | +88,450 B |
| `compiled_tailwind.css` | 344,393 B | 346,172 B | +1,779 B |

`index.html`, `jetbox.css`, the eight `voices/*.mp3` speaker profiles (27 files), the sound cues, and the workers are byte-identical.

String-set diff of `main.js` (56 new / 12 removed literals):

- **Plugin catalogue**: `Filter and Sort`, `Filter plugins`, `Category`, `Clear category`, `Most popular first`, `Most popular matches first`, `No plugins available`, `Plugins Options`, `DOWNLOAD` — the client half of `SearchExtensions`.
- **Sidebar customisation**: `Customize Sidebar`, `Manual Order`, `Idle Conversations`, `Last Activity`, `Any time`, `Only applies when grouped by status`.
- **Queue toggle copy**: `Messages now send immediately. Configure in`, `Instead of queueing, try asking the agent to do something after the current task.`
- **Removed**: `Extensions` / `UI Extensions` / `Start Extension` — the surface is renamed to *plugins* in the UI.

Both files were copied into the clone's embedded frontend (`internal/rcfrontend/assets/`), which is a byte-for-byte mirror of this directory (verified: 1.5.10's `main.js` / `compiled_tailwind.css` hashes matched the repo's copies exactly before the swap).

---

## 5. Builtin Skill Bundle: 9 → 15

New skills in the 1.5.11 embed, now bundled and installed by `internal/features/tools/skills`:

| Skill | Tree |
|---|---|
| `mcp-app-development` | `external/skills` |
| `migrate-agent-settings` | `external/skills` |
| `native_swarm` (+ `logo.svg`) | `shared/skills` |
| `swarm_monitor` | `shared/skills` |
| `swarm_narrator` | `shared/skills` |
| `swarm_pane` | `shared/skills` |

Drifted files re-synced from the 1.5.11 bytes:

| File | Drift |
|---|---|
| `agy-customizations/docs/hooks.md` | documents `PLUGIN_ROOT` / `PLUGIN_DATA` for plugin command hooks, `hookEventName` and `cwd` in the hook input JSON, `toolCall.id` in PreToolUse/PostToolUse, and `injectSteps[].systemMessage` |
| `antigravity_guide/references/app.md` | −3 lines of stale app copy |
| `generative_ui/SKILL.md` | 60 insertions / 53 deletions — the generative-UI contract rewritten |
| `plugin/SKILL.md` | 8 / 10 — plugin CLI examples updated |

The `<skills>` prompt section still advertises only `agy-customizations` and `antigravity_guide` (`agyprompt.BuiltinSkillDirs`), so prompt parity is unaffected; `internal/app/prompt/agyprompt` stays green.

---

## 6. Changelog Re-Capture

The 1.5.11 binary still ships the release-notes feed **one release behind** (`agy changelog` tops out at `1.5.10:`), and no public surface carries 1.5.11's own notes yet — `https://antigravity.google/docs/changelog.md` stops at CLI `1.5.9`, and the release pages are client-rendered with no 1.5.11 body. So there is deliberately **no** `changelog_upstream_1.5.11.md`: that file exists only when the published notes run ahead of the served feed.

What the feed did gain:

| | 1.4.12 capture (was) | 1.5.11 capture (now) |
|---|---|---|
| bytes | 189,039 | **241,807** |
| lines | 1,073 | **1,278** |
| version sections | 83 | **94** |
| top-level bullets | 908 | **1091** |
| newest section | `1.4.12:` | **`1.5.10:`** |
| sha256 | `34d2421b…` | **`3f25bfd0d6a3f7da01d0c1cd1f7e6656c6deba00fb70babb83cff87250cf5874`** |

The 94 sections are `1.5.10 → 1.0.0`; the whole `1.5.x` series (`1.5.0` … `1.5.10`) and the `1.3.7` / `1.3.8` / `1.3.9` sections that every 1.4.x feed skipped are all present now. Files updated together so the gate cannot drift:

- `internal/cli/testdata/changelog_reference.txt` (verbatim `agy changelog` capture)
- `internal/cli/changelog_data_test.go` (five constants, the `1.5.10:` shape prefix, the header comment)
- `internal/cli/changelog_v133_test.go` (`TestChangelogReference_NewestBulletIsUpstream` now pins the `accept-edits` bullet)
- `internal/infra/referencedrift/reference.lock.json` (`captured_at` `1.5.11`, new digest, `lag_acknowledged_through` `1.5.11`)

`TestChangelog_MatchesReferenceBinary` compares the embedded blob byte-for-byte against `~/.local/bin/agy changelog` when a binary of the pinned version is present; with the pin at 1.5.7 and 1.5.11 installed it skips by design (§7).

---

## 7. What Deliberately Did Not Move

- **`internal/cli.ReferenceVersion` stays `"1.5.7"`.** It is the clean-room compatibility contract for observable behaviour — help panel, update check, remote-control handshake — not "the newest binary on disk". Bumping it is only meaningful together with re-evidencing the 20-odd artifacts in `reference.lock.json`; the lock records the 1.5.11 artifact set under `lag_acknowledged_through: "1.5.11"` instead, so the drift gate stays honest without pretending.
- **The language-server RPC list, tool descriptions, tool variants, and the implicit-trajectory step** remain 1.4.7/1.4.8 evidence. The drift gate reports them as `SUSPECT … unverified against 1.5.11` in its log — the intended signal, not a failure.
- **The 1.5.11 release notes** do not exist upstream yet (§6).

---

## 8. Verification

```bash
# descriptor extraction sanity
head -n 2 out/extracted-1.5.11/protos/cortex_1.5.11.txt
#   # package exa.cortex_pb  offset 0x44d8bbf  bytes 217590  messages 511  enums 87

# changelog gate + drift gate
go test ./internal/cli/ -run "TestChangelog|TestReferenceVersion"
go test ./internal/infra/referencedrift/ -run "TestReferenceLock|TestLockParse"

# skills bundle / prompt parity / embedded frontend
go test ./internal/features/tools/skills/ ./internal/app/prompt/agyprompt/ ./internal/rcfrontend/

# whole-tree guards
make arch
make quick
```
