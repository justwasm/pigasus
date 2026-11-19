# Provenance — the reference's builtin skill bundle

These files are the Antigravity CLI's own, recovered verbatim from the binary.

## Source

| | |
|---|---|
| Binary | `~/.openagy/bin/agy-1.5.11` — the Antigravity CLI, agy **1.5.11** |
| Size | 226,097,264 B |
| SHA-256 | `62f2102784bb74b7743ce31f45e8414f1111dc99efb395a9e62690af0336c677` |
| Container | `assets/external/skills/` and `assets/shared/skills/` inside the binary's `//go:embed` file system |

Re-synced at 1.5.11 (2026-11-19) from `out/extracted-1.5.11/embed/assets/`. Six
skills are new since the 1.4.12 bundle, and four files had drifted:

| Change | File(s) |
|---|---|
| **New** | `external/skills/mcp-app-development/SKILL.md`, `external/skills/migrate-agent-settings/SKILL.md`, `shared/skills/native_swarm/` (`SKILL.md` + `logo.svg`), `shared/skills/swarm_monitor/SKILL.md`, `shared/skills/swarm_narrator/SKILL.md`, `shared/skills/swarm_pane/SKILL.md` |
| **Drift** | `agy-customizations/docs/hooks.md` — plugin hooks now document `PLUGIN_ROOT` / `PLUGIN_DATA`, and the input JSON gained `hookEventName`, `cwd`, `toolCall.id` and `injectSteps[].systemMessage` |
| **Drift** | `antigravity_guide/references/app.md` — 3 lines of stale app copy dropped |
| **Drift** | `generative_ui/SKILL.md` — the generative-UI contract rewritten (60 insertions / 53 deletions) |
| **Drift** | `plugin/SKILL.md` — plugin CLI examples updated (8 / 10) |

Earlier re-verification at 1.4.12 (2026-11-01) had re-synced two files:

| File | Drift |
|---|---|
| `plugin/SKILL.md` | the `GetAllPlugins` jq example read `.agents[]?.name`; 1.4.12 reads `.agents[]?.agent.name` |
| `agy-customizations/docs/plugins.md` | the 1.4.12 binary documents the plugin `auth.json` provider block and its `"optional": true` flag — the clone's copy had the text hand-added ahead of the binary (commit `8f6b7d36`) and has now been replaced with the binary's own bytes |

## How they were recovered

```bash
go build -o /tmp/embedextract ./devutils/embedextract
/tmp/embedextract -bin ~/.openagy/bin/agy-1.5.11 -embed-out /tmp/agy-embed
cp -R /tmp/agy-embed/assets/external/skills/* internal/features/tools/skills/skills/
cp -R /tmp/agy-embed/assets/shared/skills/*  internal/features/tools/skills/skills/
```

The bundle is flattened on the way in: the reference's asset paths split the
skills into `external/` and `shared/`, but the tree it unpacks on disk
(`<AppDataDir>/builtin/skills/`) is flat, one directory per skill.

## The reference's own install, observed on this machine

`~/.gemini/antigravity-cli/builtin/` holds the bundle the reference unpacked,
and at 1.5.11 it has fifteen skill directories:

```
agy-customizations     migrate-agent-settings  swarm_monitor
antigravity_guide      migrate-workflows       swarm_narrator
automation             native_swarm            swarm_pane
generative_ui          permissioned-github     ui-plugin-navigation
mcp-app-development    plugin
run-workflow
```

next to a `keep.txt` (also an embedded asset) and a `.checksum` file holding
`builtin:f900c2beded56d722d3385cf8ed71bb443695def8d0a1b0652c2ab8dc354ecaa` —
the reference re-unpacks the bundle whenever that checksum changes, which is
how a version bump replaces the tree on disk.

`agy-customizations/docs/hooks.md` used to be the one file where the installed
copy differed from the embedded one (the installed copy came from an older
build of the reference). At 1.5.11 the embedded copy is the newer one, and the
bundle here carries it.

## Which skills the prompt lists

The reference advertises two of the fifteen in the CLI's `<skills>` section. The
others sit behind server-side feature flags the CLI does not turn on, so
`agyprompt.BuiltinSkillDirs` is the allowlist of those two.
