# Provenance — the reference's builtin skill bundle

These files are the Antigravity CLI's own, recovered verbatim from the binary.

## Source

| | |
|---|---|
| Binary | `~/.local/bin/agy` — the Antigravity CLI, agy **1.4.12** |
| Size | 197,589,952 B |
| SHA-256 | `34423fc9b0af3841e7646aa72a2d40ffeab8218b22d1cb99eac7355f9eba31e3` |
| Container | `assets/external/skills/` and `assets/shared/skills/` inside the binary's `//go:embed` file system |

Re-verified at 1.4.12 (2026-11-01). Two files had drifted and were
re-synced from the 1.4.12 binary:

| File | Drift |
|---|---|
| `plugin/SKILL.md` | the `GetAllPlugins` jq example read `.agents[]?.name`; 1.4.12 reads `.agents[]?.agent.name` |
| `agy-customizations/docs/plugins.md` | the 1.4.12 binary documents the plugin `auth.json` provider block and its `"optional": true` flag — the clone's copy had the text hand-added ahead of the binary (commit `8f6b7d36`) and has now been replaced with the binary's own bytes |

## How they were recovered

```bash
go build -o /tmp/embedextract ./devutils/embedextract
/tmp/embedextract -bin ~/.local/bin/agy -embed-out /tmp/agy-embed
cp -R /tmp/agy-embed/assets/external/skills/* internal/builtinskills/skills/
cp -R /tmp/agy-embed/assets/shared/skills/*  internal/builtinskills/skills/
```

The bundle is flattened on the way in: the reference's asset paths split the
skills into `external/` and `shared/`, but the tree it unpacks on disk
(`<AppDataDir>/builtin/skills/`) is flat, one directory per skill.

## The reference's own install, observed on this machine

`~/.gemini/antigravity-cli/builtin/` holds the bundle the reference unpacked,
and it has exactly these nine skill directories:

```
agy-customizations   antigravity_guide   automation   generative_ui
migrate-workflows    permissioned-github plugin      run-workflow
ui-plugin-navigation
```

next to a `keep.txt` (also an embedded asset) and a `.checksum` file holding
`builtin:3eb968c5db879786311097d37a19b81f7ce97d4e76a572864d84b1b6a2942eda`.

Both `SKILL.md` files the reference advertises in its prompt —
`agy-customizations` and `antigravity_guide` — are byte-identical to the
embedded copies, frontmatter included, so the descriptions the prompt prints
come out the same.

`agy-customizations/docs/hooks.md` is the one file where the installed copy
differs from the embedded one: the installed copy is from an older build of
the reference. It is documentation, not prompt text.

## Which skills the prompt lists

The reference advertises two of the nine in the CLI's `<skills>` section. The
other seven sit behind server-side feature flags the CLI does not turn on, so
`agyprompt.BuiltinSkillDirs` is the allowlist of those two.
