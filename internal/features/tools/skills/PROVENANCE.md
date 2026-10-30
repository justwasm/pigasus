# Provenance — the reference's builtin skill bundle

These files are the Antigravity CLI's own, recovered verbatim from the binary.

## Source

| | |
|---|---|
| Binary | `~/.local/bin/agy` — the Antigravity CLI, agy **1.4.7** |
| Size | 192,171,376 B |
| SHA-256 | `b7a13e53c9f49921692676ef8edc2e60ceb67064c00bc732d9a364ec3ad75b32` |
| Container | `assets/external/skills/` and `assets/shared/skills/` inside the binary's `//go:embed` file system |

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
