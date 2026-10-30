# Cycle 367 deliverable: the plugin RPCs, and the instance rename that does not drop the connection

**Cycle:** `cycle-367-plugin-rpcs-and-instance-rename`
**Subject:** two gaps in the remote-control surface, both against `agy` **1.4.11**
(`internal/cli.ReferenceVersion`).
1. The Web UI's **plugin panels** are driven by the language server's plugin RPCs.
   The clone answered two of them with an empty list and let the rest fall through
   to `Method not found`, so the panels had nothing to show.
2. The **instance name** — the nickname the dashboard shows for this machine — could
   only be changed by restarting the daemon. The reference renames the instance over
   the API while the tunnel session stays up.
**Evidence:** 【反编译】 from decompiled output at a confirmed address ·
【一手】 first-hand against this machine's own state and against the reference's
descriptor · 【strings】 string-table hit only · 【推测】 hypothesis

---

## 0. The short answer

| # | what | the clone |
|---|---|---|
| 1 | the language server's **plugin RPCs** | **28 methods answer** — the 27 the Web UI's bundle calls, plus `GetSkillMarketplaceLink` (§3) |
| 2 | the **plugin data** they answer from | the installed plugins (`~/.gemini/config/plugins/`) and the marketplace catalogues (`~/.gemini/antigravity-cli/cache/marketplaces/`) — the reference's own paths (§2) |
| 3 | the **cloud-only surfaces** | answer **empty** or **refuse**, on purpose — a plugin auth provider, a "build with Google" plugin and a marketplace install all need the reference's cloud (§5) |
| 4 | the **instance rename** | travels over the API, **the tunnel session stays up** — `POST {proxyURL}/v1internal:updateInstance` (§6) |
| 5 | the **shell entry point** | `pigasus remote-control rename <name>` — **hidden**, because the reference's `remote-control --help` block is the reference's, byte for byte (§7) |
| 6 | the **update endpoint** | a **named endpoint**, derived from the tunnel's own host — so a local mock answers it on the same port (§6.3) |
| 7 | the **source of truth** | the configuration file — `userSettings.cliRemoteControlHostname`, exactly the reference's model (§6.2) |

---

## 1. What was missing

### 1.1 The plugin RPCs

The Web UI's plugin page is driven by the language server's own service. The clone's
dispatcher had:

```go
case "GetCustomizationStates":
    return map[string]any{"states": []any{}}, nil, true
case "SubscribeToPlugins":
    return map[string]any{"plugins": []any{}}, nil, true
```

and nothing else — every other plugin method fell through to the alias table and
came back `-32601 Method not found`.

The method list is the page's own: `internal/rcfrontend/assets/main.js` defines a
client object whose plugin members are exactly the calls the page makes. 【一手】

| method | the page calls it |
|---|---|
| `getAllPlugins` · `getPlugin` · `deletePlugin` | yes |
| `resolveCommandPlugin` | yes |
| `getAvailableCascadePlugins` · `getBuildWithGooglePlugins` · `downloadBuildWithGooglePlugin` | yes |
| `listPluginAuthProviders` · `signInPluginAuthProvider` · `signOutPluginAuthProvider` · `startPluginAuthSignIn` · `setDefaultPluginAuthAccount` · `setPluginAuthAccountLabel` | yes |
| `listRemotePlugins` · `disconnectRemotePlugin` | yes |
| `deleteSkill` · `getAllSkills` | yes |
| `searchMarketplace` · `searchMarketplaceCustomizations` · `getMarketplaceCustomization` · `getMarketplaceBundles` · `getMarketplaceFeatured` · `installCustomization` | yes |
| `subscribeToPlugins` · `getCustomizationStates` | yes |
| `getSkillMarketplaceLink` | **no** — the CLI's own `plugin link` flow calls it (`internal/cli/marketplace_link.go`) |

Twenty-six methods. All of them answer now.

### 1.2 The instance rename

The reference's own language server does **not** restart the tunnel when the
instance's name changes. It watches the configuration and renames the instance over
the API. 【反编译】

---

## 2. The state the answers come from

The clone had a plugin reader for the `/plugin` panel — a name, a one-line
description, a version — and a marketplace reader that kept only what the panel
shows. The Web UI's surfaces need more: the logo, the author, the repository, the
suggested prompts, the marketplace an entry came from, the skills' frontmatter.

`internal/features/plugins/core/catalog.go` is the second reader. 【一手】 2026-10-30,
read off this machine:

```
~/.gemini/config/plugins/
  agents-json-plugin/  chrome-devtools-plugin/  google-antigravity-sdk/
  modern-web-guidance-plugin/  science/
~/.gemini/antigravity-cli/cache/marketplaces/
  antigravity-plugins-official/   (17 entries)
  antigravity-plugins-partners/   (50 entries)
```

A `plugin.json`:

```json
{
  "name": "modern-web-guidance-plugin",
  "displayName": "Modern Web Guidance",
  "version": "0.0.195",
  "description": "Keep your coding agent up to date…",
  "logo": "assets/logo.svg",
  "suggestedPrompts": ["…"],
  "author": { "name": "Google Chrome", "url": "https://developer.chrome.com" },
  "homepage": "https://…", "repository": "https://…",
  "license": "Apache-2.0", "keywords": ["web", "html"]
}
```

A `marketplace.json`:

```json
{ "name": "antigravity-plugins-official",
  "description": "Antigravity plugins marketplace catalog.",
  "plugins": [ { "name": "…", "displayName": "…", "version": "…",
                 "source": { "type": "archive", "url": "https://…zip?v=…",
                             "sha256": "…" } } ] }
```

Two shapes the reader has to know about:

* **The author is an object in the reference's own plugins, and a plain string in
  the plugins imported from Claude Code** — the reference's own import contract
  carries the author's *name* alone. `Author` reads both.
* **A directory without a `plugin.json` is not a plugin.** The reference ignores it
  and so does this reader. The `/plugin` panel's stock fallback list (four plugins a
  fresh install ships) is deliberately **not** used here: the Web UI's list is a list
  of what is installed, and answering with plugins that are not installed would be a
  lie.

---

## 3. The answers

### 3.1 The shapes

Every shape comes from the reference's own descriptor —
`lsproto/descriptor/language_server.protoset`, extracted from the bundle the clone
serves. 【反编译】 (the descriptor *is* the schema; the numbers below are its
field numbers).

| message | members |
|---|---|
| `GetAllPluginsResponse` | `plugins` → `Plugin` |
| `Plugin` | `name(1) path(2) is_global(3) skills(4) agents(5) mcp_servers(6) rules(7) hooks_json(8) workflows(26) disabled(9) description(10) readme(11) logo(12) sidecars(13) installed_from(14) version(16) suggested_prompts(17) display_name(18) is_builtin(19) force_disabled(20) disabled_reason(21) auth_required(22) authorize_uri(23) is_authenticated(24) author(28) homepage(29) repository(30)` |
| `GetPluginResponse` | `plugin` → `Plugin` |
| `DeletePluginResponse` | `success(1) message(2)` |
| `ResolveCommandPluginResponse` | `plugin` → `PluginRef{name, display_name, logo}` |
| `GetCustomizationStatesResponse` | `states` → `LoadedCustomization{type, name, path, scope, status, plugin_name, error_detail, version, installed_from}` |
| `SubscribeToPluginsResponse` | `plugins` → `Plugin` (server-streaming) |
| `SearchMarketplaceResponse` | `customizations(1) next_page_token(2) groups(3)` |
| `GetMarketplaceCustomizationResponse` | `customization(1) plugin(2) skill(3)` |
| `GetMarketplaceBundlesResponse` | `bundles(1) customizations(2)` |
| `GetMarketplaceFeaturedResponse` | `featured(1) customizations(2)` |
| `GetAvailableCascadePluginsResponse` | `plugins` → `CascadePluginTemplate` |
| `GetBuildWithGooglePluginsResponse` | `plugins` → `BuildWithGooglePlugin` |
| `ListPluginAuthProvidersResponse` | `providers` → `PluginAuthProvider` |
| `ListRemotePluginsResponse` | `plugins` → `PluginInfo` |
| `InstallCustomizationResponse` | `success(1) message(2) install_dir(3)` |
| `GetAllSkillsResponse` | `skills` → `WorkflowSpec` |
| `DeleteSkillResponse` | `success(1) message(2)` |
| `GetAllRulesResponse` | `memories` → `CortexMemory` |
| `GetAgentScriptsResponse` | `agent_scripts` → `AgentScriptItem`, `agent_customizations` → `AgentCustomization` |

The enums the answers use:

* `RefreshCustomizationType` — `…_RULE(1) …_WORKFLOW(2) …_SKILL(3) …_AGENT(4)
  …_PLUGIN(5) …_HOOKS(6) …_MCP(7) …_FLOW(8)`.
* `LoadedCustomization.Scope` — `SCOPE_BUILTIN(1) SCOPE_GLOBAL(2) SCOPE_WORKSPACE(3)`.
* `LoadedCustomization.Status` — `STATUS_ENABLED(1) STATUS_DISABLED(2)
  STATUS_TRUNCATED(3) STATUS_ERROR(4)`.

`CustomizationSource` is a **oneof**: `google3` (a depot path) or `archive` (a URL
and a digest). The local catalogues carry archives.

### 3.2 The proto-JSON, and why the field order matters

The answers are proto-JSON in the schema's own field names (camelCase). The clone's
dispatcher answers with Go maps, and `encoding/json` writes a map's keys in
alphabetical order — so the clone's answers would carry the same data in a
different order than the reference's. The `lsproto` codec reorders them into the
descriptor's declaration order on the way out (`lsproto/order.go`).

The one thing the codec does **not** fix is a *type* error. The Web UI decodes the
answer with the reference's own descriptor and `ignoreUnknownFields: true`: an
unknown member is fine, but a **known member with the wrong JSON type fails the
whole decode**, and the panel renders an error instead of the plugin list.

`TestThePluginAnswersParseWithTheReferenceSchema` therefore encodes and decodes
every one of the twenty-eight answers through the reference's own codec. It is the
test that would have caught a `skills` list where the schema wants a message, or a
`metadata.name` where it wants a string.

### 3.3 The two halves of the plugin concept — not to be conflated

There are **two** different "plugin" notions in the reference, and the clone keeps
them apart:

| | the customization plugin | the remote plugin |
|---|---|---|
| what it is | a directory under `~/.gemini/config/plugins/` | a plugin *connected* to this host |
| the message | `exa.language_server_pb.Plugin` | `exa.remote_agent_proxy_pb.PluginInfo` |
| where the state lives | on disk | the `PluginStore` seam |
| the methods | `GetAllPlugins`, `GetPlugin`, `DeletePlugin`, … | `ListRemotePlugins`, `DisconnectRemotePlugin` |

The clone already had the remote half, under the JSON-RPC service's own names
(`ListRemoteAgentPlugins` / `DisconnectRemoteAgentPlugin`). This cycle adds the
**language server's** names for the same seam, with the language server's response
shape — `{"plugins":[{pluginId, displayName, version, …}]}` rather than the legacy
`{"ok":true,"plugins":[{plugin_id, name, …}]}`.

---

## 4. The files

| file | lines | what |
|---|---|---|
| `internal/features/plugins/core/catalog.go` | 433 | the documents: the full `plugin.json` and `marketplace.json`, the skills' frontmatter, the lookup and the search |
| `internal/remotecontrol/plugins_ls.go` | 767 | the installed half, the skills, the remote plugins, the model converters |
| `internal/remotecontrol/plugins_marketplace.go` | 644 | the catalogue half, the cascade templates, the cloud-only surfaces |
| `internal/remotecontrol/plugins_ls_test.go` | 470 | the behavioural tests and the schema round-trip |
| `internal/remotecontrol/dispatch_ls_cloud.go` | 169 | the language server's cloud surfaces: the licence, the environments, the resource registries |
| `internal/remotecontrol/ls_schema_audit_test.go` | 116 | the whole-service audit against the reference's descriptor |
| `internal/remotecontrol/dispatch_ls_cloud_test.go` | 155 | the cloud surfaces' own tests |

`internal/features/plugins/core/data.go` grew the manifest: the display name, the
logo, the author, the homepage, the repository, the license, the keywords, the
suggested prompts, the readme and the source. The `/plugin` panel ignores the extra
fields; the Web UI does not.

The reader follows the same symlink rules the panel's reader does
(`internal/pluginscan`), because a symlinked plugin directory is a real shape — a
plugin that lives in `~/work/` and is linked into the tree is installed.

---

## 5. What answers empty, and why

Three surfaces need the reference's cloud, and the clone has no account there:

| method | the answer |
|---|---|
| `ListPluginAuthProviders` | `{"providers": []}` — the clone's plugins carry no auth section at all 【一手】 (no `plugin.json` on this machine has one) |
| `StartPluginAuthSignIn` | `{"authUrl": "", "accountId": ""}` — there is no provider to sign in to |
| `SignInPluginAuthProvider` | `{"connected": false, "accountId": ""}` |
| `SignOutPluginAuthProvider` · `SetDefaultPluginAuthAccount` · `SetPluginAuthAccountLabel` | `{}` |
| `GetBuildWithGooglePlugins` | `{"plugins": []}` — a "build with Google" plugin is one Google builds and serves from its own cloud |
| `DownloadBuildWithGooglePlugin` | `{"success": false, "message": "…not supported by this build."}` |
| `InstallCustomization` | `{"success": false, "message": "…not supported by this build."}` |
| `GetMarketplaceBundles` | `{"bundles": [], "customizations": [the requested ids]}` — a bundle is the cloud's curation; the local catalogue carries none |
| `GetMarketplaceFeatured` | `{"featured": [], "customizations": []}` — the featured cards are the cloud's curation too |

The rule is the same everywhere: **an empty answer is the truth; a fabricated one
would be worse.** The panel renders "nothing featured" and "no Google-built
plugins", which is what the machine actually has.

`InstallCustomization` is the one that matters most: it *looks* like a success if
the answer is empty, so it refuses explicitly, and it never reports an install
directory it did not create.

---

## 6. The instance rename

### 6.1 The reference's mechanism

【反编译】 `remotecontrol.UpdateInstanceMetadata` at **`0x1025aad50`** in the 1.4.11
binary posts to:

```
{proxyURL}/v1internal:updateInstance
```

with `Content-Type: application/json` and a protojson body. The reference's own
error strings, all reproduced verbatim in the clone:

```
newName is empty
proxyURL is empty
failed to marshal request: %w
failed to create HTTP request: %w
failed to send request: %w
request failed with status %d: %s
```

HTTP 200 is success; the body is not read.

The body is the `UpdateInstanceRequest` proto — recovered from the
`FileDescriptorProto` the binary carries at file offset `0x3a2c566` (12,828 B):

```proto
message InstanceMetadata { string name = 1; string version = 2; bool supports_chunking = 3; }
message UpdateInstanceRequest { string instance_id = 1; InstanceMetadata metadata = 2; google.protobuf.FieldMask update_mask = 3; }
```

protojson writes a `FieldMask` as a comma-separated string, and writes the fields
in field-number order — so the body on the wire is exactly:

```json
{"instanceId":"…","metadata":{"name":"…"},"updateMask":"metadata.name"}
```

The caller is `language_server.(*Server).updateRemoteControlConnection.func1` at
**`0x1026a6020`**, and the reference's own log lines are:

```
[RemoteControl] Resolved proxyServerURL: %q
[RemoteControl] Configuration changed, restarting connection
[RemoteControl] Cannot update hostname to empty. Rejecting update.
[RemoteControl] Hostname changed from %q to %q, updating via API
[RemoteControl] Failed to update instance metadata: %v
```

The CLI's side is `jetski/cli/remotecontrol.(*Daemon).SetInstanceName` at
**`0x102d35320`** → `config.ApplyProtoUpdate` — the name is written into the
configuration proto, and the language server reads it back.

### 6.2 The clone's model — the same one

The source of truth is the configuration file:

```
~/.gemini/config/config.json
  → userSettings.cliRemoteControlHostname
```

which is exactly where the reference's config proto keeps it. The clone's daemon
already wrote it (`persistentrc.StartOfficialDaemon` → `SetInstanceName`, so
`remote-control start --name X` lands there today).

`internal/rchost/instance_name.go` is the watcher:

* it polls the **configured** name every two seconds;
* an empty configured name is **skipped** — the reference refuses an empty hostname
  outright;
* a change is pushed over the API with the reference's own log lines;
* on success the tunnel's own registration name follows — because the tunnel
  re-registers on every reconnect, and a re-registration carries the name the
  client holds. Without that, the next reconnect would put the old name back.

**The configured name, not the resolved one.** `persistentrc.ResolveInstanceName`
falls back to the machine's host name when the configuration carries none. A
watcher on the *resolved* name would see a name it did not write and push it — so
the watcher watches the raw value (`persistentrc.ConfiguredInstanceName`, added
this cycle) and resolves it only when it pushes.

**The baseline is passed in, not read inside the watcher.** It is captured by the
caller *before* the goroutine starts, so a name written between the daemon's
start-up and the watcher's first tick is not mistaken for a rename the watcher had
already seen.

### 6.3 The endpoint

`v1internal:updateInstance` lives on the **tunnel's own host** — only the RPC path
differs from `v1internal:connectInstanceV2`. So the endpoint is a named endpoint
(`update_instance`) that is **derived from the tunnel's endpoint** unless the
configuration file names one:

```go
func UpdateInstanceURLFor(webChannel string) string
```

A proxy URL without a scheme is read as `https` — the reference's own rule. The
derivation is what keeps a local mock working: a tunnel pointed at `127.0.0.1`
answers the rename on the same port.

### 6.4 The shell entry point

`pigasus remote-control rename <name>` writes the configuration file. It is
**hidden**: the reference's `remote-control --help` lists `start`, `status` and
`stop` only, and that block is the reference's, byte for byte. `rename` is
*accepted*, not advertised — the same way `serve` and `run` are.

The reference has no such subcommand: it renames the instance from the Web UI's
settings panel. The clone offers the same thing from the shell, because the shell
is where the user is when the dashboard is on another machine.

---

## 7. The tests

| test | what it pins |
|---|---|
| `internal/remotecontrol/plugins_ls_test.go` | 16 tests: the installed list, the skills/agents/rules, one plugin and the unknown one, the delete and the unknown delete, the path-traversal guard, the command resolution, the rules, the agent scripts, the customization states, the catalogue search, the bundle and featured answers, the skill link, the stream's first frame |
| `…` `TestThePluginAnswersParseWithTheReferenceSchema` | all twenty-eight answers encode **and decode** through the reference's own codec |
| `internal/features/plugins/core/catalog_test.go` | 9 tests: the plugin reader, the two author shapes, the skill frontmatter, the catalogue reader, the lookup, the search, the BOM |
| `internal/rctunnel/update_instance_test.go` | 5 tests: the wire body, the status failure, the empty name, the endpoint derivation, the JSON shape |
| `internal/rchost/instance_name_test.go` | 3 tests: the rename travels, the tunnel stays up, an empty name is not pushed |
| `internal/cli/rc_rename_test.go` | 4 tests: the configuration is written, its other members survive, the empty and the two-name cases are refused, the subcommand is hidden |
| `internal/remotecontrol/ls_schema_audit_test.go` | the **whole-service** audit: every method of the 1.4.8 fixture against every answer the clone gives |
| `internal/remotecontrol/dispatch_ls_cloud_test.go` | the cloud surfaces: the licence's shape, the empty registries, the members the schema knows |

---

## 8. The schema audit

The plugin answers above are hand-written maps, and so is every other answer the
clone gives for the language server. A member the reference's descriptor does not
know is dropped on the way to the Web UI — **silently, in two places**:

* the browser's decoder ignores an unknown member (the transport parses with
  `ignoreUnknownFields: true`), so the page renders and the member's data is
  simply not there; 【一手】
* `protojson` — the clone's own encoder (`lsproto/convert.go`) — rejects a member
  whose *type* does not fit, and drops the **whole list** with it when one of its
  rows carries one. 【反编译】

Neither failure leaves a trace. So the cycle added an audit: walk every answer
against the reference's own descriptor (the `FileDescriptorProto` read out of the
agy 1.4.8 binary, committed at `lsproto/descriptor/language_server.protoset`) and
report a member the schema does not know — and an enum value the schema does not
*name*.

The audit ran over **the whole service**: every method of the 1.4.8 fixture
(`testdata/language_server_rpcs_1.4.8.txt`, 358 methods) against every answer the
clone gives. Four answers were in the wrong shape.

| the answer | the schema | what the Web UI saw |
|---|---|---|
| `{"textMemory":{"text":…}}` — the plugin's rules | `CortexMemoryText` has one member, `content` (1) | the rule's text **dropped**: the panel showed a rule with no body |
| `{"type":"PLUGIN"}` — the customization states | `RefreshCustomizationType` — `REFRESH_CUSTOMIZATION_TYPE_PLUGIN` (5) | the **whole list** dropped: the plugin panel showed no customization at all |
| `{"authResult":{…}}` — `SelfAssignLicense` | `SelfAssignLicenseResponse`: `license` (1), `assigned` (2), `error_message` (3), `data_sharing_disclaimer` (4) | `assigned` and `license` undefined: the onboarding reported "no licence" for an entitled account |
| the legacy dashboard shapes — `ListEnvironments`, `ListResourceProviders`, `ListAvailableResources` | `EnvironmentInfo` / `ResourceProviderInfo` / `RemoteResource` | every member dropped: the resource picker read `providers` and `resources` as undefined |

**The enum is the sharp edge.** `protojson` names an enum by its **full** name. The
short name (`"PLUGIN"`) is not a value the enum knows, and the whole list that
carries it goes with it — so the customization states reached the browser as an
empty list, with no error anywhere. The bundle's own `RefreshCustomizationType` is
a *numeric* JS enum (`{UNSPECIFIED:0, RULE:1, …, PLUGIN:5}`) used for the display
labels, which is why the short name looks right in the source and is wrong on the
wire. 【一手】

**The alias table was a shape mismatch, not an equivalence.** `ListEnvironments`,
`ListResourceProviders` and `ListAvailableResources` are the *language server's*
methods; the alias sent them to the clone's legacy dashboard methods, whose
contracts are a host snapshot (`os` / `arch` / `hostname` / `cwd`) and a registry
row (`provider_id` / `name` / `kind` / `configured`). Not one of those members is a
member the language server's schema knows. The three are answered by the language
server's own table now, and the alias entries are gone; the clone's legacy methods
keep their own names and their own contracts.

**The cloud registries are Google's.** A capsule is a cloud sandbox, a resource
provider is an account the user connected to the cloud. The clone runs on the user's
own machine and owns none of them, so the three lists are **empty** — a true
answer, and the one the Web UI expects (the page reads `environments ?? []`). The
clone's *own* resource registry — the one the dashboard's project panel binds
against — stays on the dashboard's own surface.

The audit is now a **permanent test** over the whole service
(`internal/remotecontrol/ls_schema_audit_test.go`), so the next answer that drifts
fails there rather than in the browser.

Two more methods were closed on the way: `GetAllRules` (the plugin's rules, as
`CortexMemory` memories) and `GetAgentScripts` (the plugin's agents, as
`AgentScriptItem`s and `AgentCustomization`s). The customization states carry one
entry per loaded customization — a plugin, a skill, a rule, an agent.

---

## 硬约束 / Hard constraints

1. **The rename must not tear the tunnel down.** The whole point of the
   `UpdateInstance` path is that the dashboard follows the new name while the
   session stays up. A reconnect would restore the *old* name, because the
   re-registration carries the name the client holds — which is why a successful
   rename also moves `Client`'s own name.
2. **The configuration file is the source of truth.** The reference's own model:
   the CLI writes the name into the config, the language server reads it back. A
   second store of the name would drift from the one the reference reads.
3. **The reference's `remote-control --help` block is byte for byte the
   reference's.** `rename` — like `serve` and `run` — is accepted, not advertised.
4. **An empty answer is the truth; a fabricated one is worse.** The cloud-only
   surfaces answer empty, and the two methods that would report a false success
   (`InstallCustomization`, `DownloadBuildWithGooglePlugin`) refuse explicitly.
5. **A known member with the wrong JSON type fails the Web UI's decode.** The
   schema round-trip test is not decoration — it is the only thing that catches a
   member the reference's descriptor knows and the clone got wrong.
6. **A plugin name is a directory name.** A delete or a skill delete that could
   escape the plugins tree is refused — a crafted id must not delete something
   else on the machine.
7. **The two plugin notions stay apart.** The customization plugin
   (`exa.language_server_pb.Plugin`, on disk) and the remote plugin
   (`exa.remote_agent_proxy_pb.PluginInfo`, in the `PluginStore`) are different
   messages with different states.
8. **Every answer member must be a member the descriptor knows, and every enum
   value must be a value the enum names.** An unknown *member* is dropped by the
   browser's decoder without an error; an unknown *enum name* takes the whole list
   with it. A short enum name (`"PLUGIN"`) is not a value — the wire carries
   `REFRESH_CUSTOMIZATION_TYPE_PLUGIN`. The audit is a test over the whole service
   (`ls_schema_audit_test.go`).
9. **The two services' contracts are not each other's.** The language server's
   `ListEnvironments` and the clone's `ListRemoteAgentEnvironments` are different
   answers, not two spellings of one; an alias between them drops every member.
10. **Local commits only.** Nothing in this cycle is pushed.

## Open questions / 存疑

- **The reference's Web UI says "Changing it reconnects" / "Changing this will
  restart the connection",** while the language server also ships the live
  `UpdateInstance` path. Both facts are in the binary. This cycle implements the
  live path — the one the reference's own code takes, and the only one that
  answers "不用断开连接". The UI's wording may describe a *different* trigger (the
  Web UI's own settings panel may restart the connection for its own reasons).
  Which of the two the user sees in the reference's panel is **not established**.
- **The marketplace's own skill listings are not in the local catalogue.** A
  `marketplace.json` lists *plugins*; the skills inside them are known only after
  an install. So `SearchMarketplaceCustomizations` answers with the extensions the
  installed plugins contribute — the customization set this machine actually has —
  and not with the cloud's marketplace skill index.
- **A bundle and a featured card have no local representation.** The catalogue
  carries neither, so both answers are empty. Whether the reference's cloud serves
  them from the same catalogue the CLI caches is **not established** — the cache
  here is a plain `marketplace.json` with no bundle key.
- **`GetSkillMarketplaceLink`'s URL shape is the clone's own.** The clone answers
  `https://antigravity.google.com/marketplace/<mkt>/<plugin>`; the reference's own
  URL was not recovered from the binary. The method's *response member*
  (`marketplace_url`) is the reference's.
- **`GetAvailableCascadePlugins` renders the installed plugins as cascade
  templates.** The reference's cascade catalogue is the cloud's; the clone's answer
  is the plugins it has.
- **The `--name` flag and the configuration file are two spellings of one thing.**
  `remote-control start --name X` already writes the configuration file
  (`StartOfficialDaemon` → `SetInstanceName`), so the watcher sees it — but the
  *ordering* is the starter's, not the daemon's. A `--name` given to a daemon
  that is already running does not reach it.
- **`SelfAssignLicense` answers "assigned" for a project the clone cannot
  license.** The reference assigns a licence tier to a *cloud* project; the clone
  has no cloud project, and reports the account's own tier — the same
  entitlement `Login`, `SelectLicense` and the account chip already report. The
  reference's own answer for a *local* backend was **not recovered**, so whether
  it says "assigned" or refuses is not established. 【推测】
- **The three cloud registries answer empty.** Whether the reference's own local
  backend answers them empty as well is **not established** — the reference's
  answers were not captured over the wire. The clone's answer is the true one for
  a machine that owns no capsule and no cloud account, and it is the one the Web
  UI expects.

## Limitations

- **The audit is static.** It walks the answers against the descriptor; it does
  not run the browser's decoder. The decoder's behaviour (an unknown member
  ignored, an unknown enum name skipping the field, a *known* member with the
  wrong type throwing) was read out of the bundle's own protobuf-es code, not
  executed against the clone's answers.
- **No page was rendered.** The plugin surface was identified from the bundle's own
  client object (`main.js`) and from the descriptor — not from a browser
  screenshot. The DOM-level proof that the panel renders the list is missing.
- **No live reference was compared.** The reference's own answers for the twenty-eight
  methods were not captured over the wire; the shapes come from the descriptor and
  the paths from the reference's own on-disk state. The *content* of an answer —
  which plugins the reference would list, in which order — is the clone's own
  reading of the same files.
- **The reference's plugin *order* is not established.** The clone lists the
  installed plugins in directory-name order, and the catalogue entries in file
  order. The reference's own ordering was not recovered.
- **The plugin's `hooksJson`, `workflows`, `user_config`, `themes`, `accounts` and
  `depot_path` are answered empty.** The clone's plugins do not carry them, and
  the reference's shapes for them were not recovered.
- **The MCP server's `env` and `url` are read from `mcp_config.json` but the
  clone does not start them.** The `McpServerSpec` answer describes the
  configuration; the servers themselves are the daemon's own business.
- **The rename was not exercised against the real cloud.** The watcher is pinned
  against a local HTTP server that answers the update endpoint. The reference's
  real endpoint was not called, and no dashboard was observed showing the new
  name.
- **The decompiled output is a snapshot.** The four addresses cited here
  (`0x1025aad50`, `0x1026a6020`, `0x1026928b0`, `0x102622c70`, `0x102d35320`) are
  from the 1.4.11 binary; every address moves on every release. They were added
  to `out/local-macos/priority-funcs.txt` as decompile seeds.
- **The Ghidra run this cycle decompiled 2,418 priority functions with 0 errors**,
  and the new `out/local-macos/dec/*.c` files are under a git-ignored path.

---

Report language note: this file is English because `docs/cycles/*` is English.
The evidence files under `out/` are Chinese, per the repository's convention.
