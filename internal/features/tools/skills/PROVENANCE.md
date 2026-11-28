# Provenance — the reference's builtin skill bundle

These files are the Antigravity CLI's own, recovered verbatim from the binary.

## Source

| | |
|---|---|
| Binary | `~/.local/bin/agy` — the Antigravity CLI, agy **1.5.18** |
| Size | 228,748,896 B |
| SHA-256 | `89d0b97a9ebcefcaebbdfe4ffeb2de877ef92ee4504de188e35a38489136be13` |
| Container | `assets/external/skills/` and `assets/shared/skills/` inside the binary's `//go:embed` file system |

Re-synced at 1.5.18 (2026-11-26) from `out/extracted-1.5.18/embed/assets/`.
Two files had drifted, and — unlike the drift fixed at 1.5.11 — these two were
stale against **every** release since the bundle was first committed
(`749e3d6c`, 2026-10-30), not against a single version bump:

| File | Drift | Now |
|---|---|---|
| `shared/skills/automation/SKILL.md` | the sidecar recipe named `~/.gemini/jetski/sidecars/<slug>/sidecar.json` and `"args": {"schedule": …, "prompt": …}`; the reference writes `<configDir>/sidecars/<sidecar-id>/sidecar.json` with `"args": ["CRON_TZ=<zone> <cron>", "agentapi", "new-conversation", "--", "<prompt>"]`. Other rewordings: an ephemeral *CitC* workspace rather than "workspace or temporary directory", and `mcp(buganizer/get_bugs)` rather than `mcp(github/list_issues)` as the example. sha256 now `6e44bfaa…` | 1.5.18 bytes |
| `shared/skills/run-workflow/SKILL.md` | missing the sentence that a top-level trailing `return <expr>` is evaluated as the workflow's result. sha256 now `4a524107…` | 1.5.18 bytes |

The other thirteen skills were already byte-identical, which is why the
divergence went unnoticed: a spot check of the tree looks perfect. The
`TestEmbeddedBundleMatchesTheExtraction` gate in `embed_parity_test.go` now
compares every file in both directions, so the next drift fails a test instead
of surviving a release.

Earlier re-syncs, for the record:

- **1.5.11** (2026-11-19) from `out/extracted-1.5.11/embed/assets/`. Six skills
  were new since 1.4.12 (`mcp-app-development`, `migrate-agent-settings`,
  `native_swarm`, `swarm_monitor`, `swarm_narrator`, `swarm_pane`) and four
  files had drifted (`agy-customizations/docs/hooks.md`,
  `antigravity_guide/references/app.md`, `generative_ui/SKILL.md`,
  `plugin/SKILL.md`). The two files above were *not* part of that re-sync's
  drift list, and that is the gap this cycle closes.
- **1.4.12** (2026-11-01) re-synced `plugin/SKILL.md` and
  `agy-customizations/docs/plugins.md`, the latter replacing text that had been
  hand-added ahead of the binary (commit `8f6b7d36`).

## How they were recovered

```bash
go build -o /tmp/embedextract ./devutils/embedextract
/tmp/embedextract -bin ~/.local/bin/agy -embed-out /tmp/agy-embed
cp -R /tmp/agy-embed/assets/external/skills/* internal/features/tools/skills/skills/
cp -R /tmp/agy-embed/assets/shared/skills/*  internal/features/tools/skills/skills/
go test ./internal/features/tools/skills/ -run TestEmbeddedBundleMatchesTheExtraction
```

The bundle is flattened on the way in: the reference's asset paths split the
skills into `external/` and `shared/`, but the tree it unpacks on disk
(`<AppDataDir>/builtin/skills/`) is flat, one directory per skill.

## The reference's own install, observed on this machine

`~/.gemini/antigravity-cli/builtin/` holds the bundle the reference unpacked,
and at 1.5.18 it has sixteen skill directories (fifteen at 1.5.11 —
`conversations` arrived later):

```
agy-customizations     migrate-agent-settings  swarm_monitor
antigravity_guide      migrate-workflows       swarm_narrator
automation             native_swarm            swarm_pane
generative_ui          permissioned-github     ui-plugin-navigation
mcp-app-development    plugin
run-workflow
```

next to a `keep.txt` (also an embedded asset) and a `.checksum` file holding
`builtin:e274d1adccd5eb649b4e3c3ab1f44aed51403c45f446cbffeff84efa0ba0567e` at
1.5.18 (`builtin:f900c2beded56d722d3385cf8ed71bb443695def8d0a1b0652c2ab8dc354ecaa`
at 1.5.11) —
the reference re-unpacks the bundle whenever that checksum changes, which is
how a version bump replaces the tree on disk.

The installed tree is a second, independent witness to this bundle: at 1.5.18
every file under `~/.gemini/antigravity-cli/builtin/skills/` is byte-identical
to the copy here, which is stronger evidence than the extraction alone — the
extraction could in principle be misread, but the reference's own unpacking
cannot.

## Which skills the prompt lists

The reference advertises two of the sixteen in the CLI's `<skills>` section. The
others sit behind server-side feature flags the CLI does not turn on, so
`agyprompt.BuiltinSkillDirs` is the allowlist of those two.
