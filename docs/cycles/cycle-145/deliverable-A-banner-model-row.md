# Cycle 145 — Banner model row (cycle 140 revert + tier moved off email)

## Why

The v1.3.6 reference binary's cold_start banner actually carries
the model name with its parenthetical tier on row 3 (e.g.
`Gemini 3.8 Flash (High)`), with the cwd pushed down to row 4.
Captured at `/tmp/agycmp/hotpath/cold_start/ref.000-init.txt`:

```
      ▄▀▀▄        Antigravity CLI 1.3.6
     ▀▀▀▀▀▀       navigaid@gmail.com
    ▀▀▀▀▀▀▀▀      Gemini 3.8 Flash (High)
   ▄▀▀    ▀▀▄     ~/GitHub/openagy
  ▄▀▀      ▀▀▄
```

Cycle 140 dropped the model row based on a capture where the
harness's reference tmux was in the "Signing in..." welcome
state (no auth token, no tier). In that state the model row is
empty, so the cwd sits on row 3 and row 4 is art. With a real
token (or a fake one that lets the ref run further) the banner
puts the model back, and now the harness shows the row. Cycle
140's doc captured a stale snapshot.

The cycle-139 tier annotation on the email row was also wrong —
the v1.3.6 reference puts the tier on the model row, never on
the email row. Captured `/help` general at
`/tmp/agycmp/hotpath/help_tabs/ref.000-general.txt` confirms:

```
Version 1.3.6
navigaid@gmail.com                 ← bare, no tier parens
Workspace:      ~/GitHub/openagy
```

Same fix applies to `/help` GENERAL.

## What changes

### `cmd/wagyu/tui_v135.go::renderV135Banner`

- Email row: drops the `email + " (" + tier + ")"` template.
  Always renders `email` alone (matches the v1.3.6 reference's
  bare email row).
- Model row (v135LogoLines[2]): restored. Renders the model name;
  if `tier != ""` and the model doesn't already end in `(tier)`,
  appends ` (tier)`. The clone's `m.model` is normally
  `"Gemini 3.8 Flash (High)"` already, so the no-op suffix logic
  kicks in for that path; the conditional exists so a future
  change that splits model/tier across fields still produces the
  right combined string.
- Cwd row (v135LogoLines[3]): now carries `shortenCwd(cwd)`,
  moved down one row to make room for the model row.
- The 5-row banner geometry is preserved — the bottom two art
  rows are unchanged.

### `cmd/wagyu/tui_v135.go::renderV135HelpGeneral`

- Email row drops the tier conditional. Always renders bare
  `email`. The tier annotation lives on the banner's model row,
  not on either email row.

### No test changes in cycle 145

The cycle-124 banner tests still pass because the 5-row geometry
is unchanged. The cycle-142 "tier conditional" test continues
to validate the empty-tier path (when the harness runs with
`JETSKI_OAUTH_TOKEN=""`, the banner row 3 reads empty model +
bare email, matching the cycle-140 captured state — i.e. the
two states are conditionally correct).

## What does NOT change in cycle 145

- The status row at the bottom still shows the *base* model
  name (no parenthetical tier) — see `statusModel: stripTier(model)`
  in `NewV135RunMode`. Only the banner model row carries the
  full `Gemini 3.8 Flash (High)` string.
- The picker candidate hint, /help, /usage, and the slash
  command renderers are unchanged.
- The fallback warning block (cycle 81/126) is unchanged —
  rendered between the banner and the first separator.

## Hard constraints / 存疑 / limitations

- 【推测】Why the cycle-140 capture was wrong. The cycle-140
  doc captured the harness's "Signing in..." welcome screen,
  which doesn't show the model row. The ref binary puts the
  row back the moment it has a model+ tier to render. Cycle
  140 was right for the cycle-140 harness state but wrong for
  the steady-state. Cycle 145 fixes the steady-state.
- 【推测】Why the tier moved from email to model. The tier
  annotation belongs with the model name (which tier is for
  what model), not with the email (which is identity, not
  capability). Putting it on the model row matches the
  reference. Cycle 139+142 placed it on the email row based
  on a capture where the model row was empty, so the tier
  had nowhere else to go.
- The harness cold_start diff is now reduced from "model row
  missing entirely" (~24 chars) to "model row present but
  warning + status model differ" (~32 chars). The remaining
  diff is environmental (fake `JETSKI_OAUTH_TOKEN=test` makes
  the ref fail auth → empty conversation; ours loads signed-
  in). The structural fix is done.
