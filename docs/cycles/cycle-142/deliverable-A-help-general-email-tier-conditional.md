# Cycle 142 — /help GENERAL email row: tier annotation conditional

## Why

After cycle 139 stripped the default tier in `NewV135RunMode`, the
banner email row became bare (`navigaid@gmail.com` instead of
`navigaid@gmail.com (Antigravity Starter Quota)`). But the
`/help` GENERAL panel still rendered the same email row with
the unconditional `email + " (" + tier + ")"` template —
producing `navigaid@gmail.com ()` when tier is empty.

Captured at `/tmp/agycmp/hotpath/help_tabs/our.000-general.txt`:

```
navigaid@gmail.com ()            ← was: navigaid@gmail.com (Antigravity Starter Quota)
Workspace:      ~/GitHub/openagy
```

The reference's `/help` general panel at
`/tmp/agycmp/hotpath/help_tabs/ref.000-general.txt` shows
`navigaid@gmail.com (Antigravity Starter Quota)` because the
reference's tier is populated at session start; the conditional
rule still applies (when the user's tier is "Starter", show the
annotation; when unset, show bare email).

## What changes

### `cmd/wagyu/tui_v135.go::renderV135HelpGeneral`

Replaced the unconditional
`email + " (" + tier + ")"` template with a cycle-139-style
conditional: `emailLine = email; if tier != "" { emailLine +=
" (" + tier + ")" }`. Same conditional as the banner email row.

The `/help` GENERAL panel now matches the banner's behaviour —
both surfaces show the tier annotation only when the tier is set.

## What does NOT change in cycle 142

- `m.tier` is still preserved on the run mode — it just isn't
  shown until something sets it.
- The cycle-138 Conversation row removal is preserved.
- The cycle-126 24-bit RGB chrome palette is preserved.
- All other /help GENERAL panel rows (Version, Workspace, Project
  Config, Log File) are unchanged.

## Hard constraints / 存疑 / limitations

- 【推测】Why does the reference always show the tier annotation
  in /help general but not always in the banner? The harness
  capture shows both surfaces populated in the reference. Cycle
  142 keeps the same conditional rule for both surfaces — when
  tier is set, both show the annotation; when unset, both hide
  it. The reference may have different rules for each surface;
  cycle 142 matches the behaviour at the harness capture state.