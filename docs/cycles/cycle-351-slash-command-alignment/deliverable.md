# Cycle 351 deliverable: the slash-command surface, item by item against agy 1.4.8

**Cycle:** `cycle-351-slash-command-alignment`
**Subject:** the official Antigravity docs publish **23** slash commands. This
cycle compares each one, item by item, against the reference binary
(`~/.local/bin/agy` 1.4.8) and against the clone, and closes the behavioural
gaps the comparison found.
**Evidence:** 【一手】 first-hand against the binary — a 23-command print-mode
sweep of both binaries, live tmux captures of the reference's panels, and
`settings.json` swaps with the file restored afterwards · 【strings】
string-table hits only · 【推测】 hypothesis

---

## 0. The short answer

| | |
|---|---|
| the documented commands | **23** |
| the commands the reference answers by name | **22** — the 23rd, `/teamwork-preview`, is not a command in 1.4.8 (the reference forwards it to the model as literal text, exactly as the clone does) |
| the commands the clone answers by name | **23** — the same 22 plus `/teamwork`, the reference's own name |
| the print-mode divergences found | **1** (`/credits`) |
| the behavioural gaps closed | **11** |

The eleven fixes:

| # | what | where |
|---|---|---|
| 1 | a bare `/statusline` **toggles** the status line | `internal/cliapp/slash_commands_handlers.go`, `internal/tuilib/feedback.go` |
| 2 | the exit-notice set — ten commands echo `Exited /<cmd> command`, the rest close silently | `internal/cliapp/slash_panels.go` |
| 3 | the picker's catalogue: the reference's **105** rows, no `chrome-devtools-plugin:*` filter; the clone's **108** (105 + `/feature-diff`, `/accounts`, `/provider`) | `internal/tuilib/commands.go` |
| 4 | `/codesearch`'s usage line is the reference's, without the clone's two extra filters | `internal/cliapp/slash_codesearch_render.go` |
| 5 | `/skills`: the icon travels **out of band** — the payload keeps the bare name, the panel renders `<icon> <name>` | `internal/printmode/print_slash_skills.go`, `internal/skillsui/panel_row.go` |
| 6 | `/config`: the **union** of the known keys and the file's keys, with the **1.4.8** defaults | `internal/printmode/print_slash_config.go`, `internal/printmode/print_format_config.go` |
| 7 | `init.permission_mode` falls back to `request-review`, not `always-proceed` | `internal/printmode/print_format.go` |
| 8 | `/usage`'s text answer is the payload rendered — tab-separated, four columns | `internal/printmode/print_slash_usage.go` |
| 9 | `/changelog` embeds the 1.4.8 blob (176 788 bytes) | `internal/printmode/testdata/print_changelog.md` |
| 10 | `/credits` answers with the account's **live** G1 credit state | `internal/printmode/print_slash_credits.go` (new) |
| 11 | the TUI's `/credits` opens the credits panel instead of printing an error | `internal/cliapp/slash_commands_handlers.go` |

---

## 1. Method

### 1.1 The documented list

Two pages of the official docs were read: the slash-command catalogue
(<https://antigravity.google/docs/slash-commands/>) and the CLI reference
(<https://antigravity.google/docs/cli/reference/>). Between them they
enumerate the 23 commands this cycle checks:

    /agents  /boost  /browser  /btw  /changelog  /codesearch  /context
    /credits  /diff  /effort  /goal  /grill-me  /learn  /permissions
    /plan  /resume  /schedule  /statusline  /teamwork-preview  /title
    /usage  /voice  /plugin

The CLI reference also carries the settings table (34 keys with their system
defaults) — it corroborates every default this cycle re-derived first-hand
(§3.6).

### 1.2 The reference is the authority for names

The docs and the binary disagree about one name. The docs' primary name for
the teamwork command is `/teamwork-preview` (with `/teamwork` as its alias);
**agy 1.4.8 ships `/teamwork`** 【一手 — the picker's row is `/teamwork`, and
`/teamwork-preview` is only a *changelog line* and a packed string blob in the
binary's string table]. The clone keeps `/teamwork`, and the two binaries
behave the same way for the docs' name: neither treats `/teamwork-preview` as
a command — both forward it to the model as literal text 【一手 — a 100-second
run of `agy -p /teamwork-preview` produced no output of its own, while
`agy -p /teamwork` answered in 12.7 s with the model's own question].

### 1.3 The print-mode sweep

Every documented command was run against both binaries in print mode, and the
exit code, stdout and stderr were captured:

    for c in <the 23 names>; do
      timeout -s KILL 14 "$REF"  --print="/$c" > ref.$c.out  2> ref.$c.err
      timeout -s KILL 14 "$CLONE" --print="/$c" > clone.$c.out 2> clone.$c.err
    done

`SIGKILL` is required: the clone ignores `SIGTERM` in print mode and the
first attempt hung on `/boost` until it was killed by hand. The raw captures
are in the operator's scratch directory, not in this repository.

### 1.4 The TUI probes

The reference's panels were read live, in a `tmux` session at 220×50, with
`capture-pane -p`. The `/skills` probe added a throw-away skill to
`~/.gemini/skills/` (`zziconprobe`) to see how an unknown icon renders; the
skill was removed afterwards and the directory is back to its nine entries.

### 1.5 The settings probes

The reference ignores `ANTIGRAVITY_HOME` and always reads
`~/.gemini/antigravity-cli/settings.json`; the clone honours the variable,
which is what makes the new tests hermetic. Every probe of the reference
backed the user's `settings.json` up, wrote a probe state, read it back and
restored the file (the backups are `settings.json.bakC`…`settings.json.bakG`
in the operator's scratch directory).

---

## 2. The item-by-item table

`TUI` is the reference's own behaviour; `print` is the result of the sweep
above ("identical" = the same bytes and the same exit code).

| documented | reference 1.4.8 | clone | TUI | print mode | verdict |
|---|---|---|---|---|---|
| `/agents` | `/agents` | `/agents` | the agents panel | identical (rc 0, no output) | aligned |
| `/boost` | `/boost` | `/boost` | runs the routine (model-backed) | identical (both model-backed) | aligned |
| `/browser` | `/browser` | `/browser` | runs the browser agent (model-backed) | identical (both model-backed) | aligned |
| `/btw` | `/btw` | `/btw` | the side question | identical — the rejection message is byte for byte the reference's | aligned |
| `/changelog` | `/changelog` | `/changelog` | the release-notes viewer | identical — 176 788 bytes, byte for byte | aligned (fixed: the blob was the 1.4.0 capture) |
| `/codesearch` | `/codesearch` | `/codesearch` | the Code Search panel | identical — the rejection message is byte for byte the reference's | aligned (fixed: the usage line) |
| `/context` | `/context` | `/context` | the context breakdown | identical — the rejection message is byte for byte the reference's | aligned |
| `/credits` | `/credits` | `/credits` | the credits panel | **fixed** — was rc 1 with an error, now the reference's two records, byte for byte | aligned (fixed) |
| `/diff` | `/diff` | `/diff` | the diff viewer | identical — the rejection message is byte for byte the reference's | aligned |
| `/effort` | `/effort` | `/effort` | the effort slider | identical (rc 0, `high`) | aligned |
| `/goal` | `/goal` | `/goal` | goal mode (model-backed) | identical (both model-backed) | aligned |
| `/grill-me` | `/grill-me` | `/grill-me` | the interview (model-backed) | identical (both model-backed) | aligned |
| `/learn` | `/learn` | `/learn` | the learning proposal (model-backed) | identical (both model-backed) | aligned |
| `/permissions` | `/permissions` | `/permissions` | the permissions manager | identical — the same `global\tallow\t<rule>` rows | aligned |
| `/plan` | `/plan` | `/plan` | plan mode (model-backed) | identical (both model-backed) | aligned |
| `/plugin` | `/plugin` | `/plugin` | the plugins manager | identical — the rejection message is byte for byte the reference's | aligned |
| `/resume` | `/resume` | `/resume` | the conversation picker | identical — the rejection message is byte for byte the reference's | aligned |
| `/schedule` | `/schedule` | `/schedule` | the schedule setup (model-backed) | identical (both model-backed) | aligned |
| `/statusline` | `/statusline` | `/statusline` | the status-line overlay | identical — the rejection message is byte for byte the reference's | aligned (fixed: the bare command toggles) |
| `/teamwork-preview` | **not a command** — forwarded to the model as literal text | the same | — | identical | aligned (the **docs** name is the one the binary does not implement) |
| `/title` | `/title` | `/title` | the title overlay | identical — the rejection message is byte for byte the reference's | aligned |
| `/usage` | `/usage` | `/usage` | the quota viewer | identical in shape — four tab-separated columns; the live figures differ by the point the quota moved | aligned (fixed: the text shape) |
| `/voice` | `/voice` | `/voice` | dictation | identical — the rejection message is byte for byte the reference's | aligned |

The reference's own name for the teamwork command is **`/teamwork`**, and the
clone answers it too (the sweep's `teamwork-preview` row is the *docs'* name;
both binaries treat it as text).

---

## 3. The fixes

### 3.1 A bare `/statusline` toggles

agy 1.4.8 changed the command: a bare `/statusline` no longer prints the
usage block, it **toggles** the status line.

| input | answer |
|---|---|
| `/statusline` (off → on) | `Statusline on.` |
| `/statusline` (on → off) | `Statusline off. Run /statusline to re-enable.` |
| `/statusline on` / `/statusline enable` | `Statusline on.` |
| `/statusline off` / `/statusline disable` | `Statusline off.` |

The explicit `off` answers with the **short** form; only the bare toggle
answers with the long one, which also tells the user how to switch it back
on. The clone had the 1.4.7 behaviour (the usage block) and now has the
1.4.8 one (`internal/tuilib/feedback.go`,
`internal/cliapp/slash_commands_handlers.go`).

### 3.2 The exit-notice set

Escaping a panel leaves a `⎿  Exited /<cmd> command` line in the transcript —
for **ten** commands only. The reference's binary carries ten literal
messages ("Exited /diff command", "/help", "/hooks", "/model", "/tasks",
"/agents", "/effort", "/skills", "/credits", "/artifact") plus one generic
format string ("Exited %-*s %s"), which is how `/config`'s own message is
built. Every other panel closes silently.

| echoes | closes silently |
|---|---|
| `/config`, `/diff`, `/skills`, `/agents`, `/effort`, `/credits`, `/artifact`, `/model`, `/hooks`, `/tasks`, `/help` | `/usage`, `/permissions`, `/context`, `/codesearch`, `/changelog`, `/plugin`, `/mcp`, `/feedback`, `/rewind`, `/add-dir` |

### 3.3 The picker's catalogue

The reference's picker lists **105** entries — the `/help` COMMANDS tab
reports the same count ("[1-33 of 105 items]"). The clone's catalogue holds
**108**: the reference's 105 plus the three documented clone-only commands
(`/feature-diff`, `/accounts`, `/provider`).

The 1.3.6 clone filtered the `chrome-devtools-plugin:*` rows out of the
picker. The 1.4.8 reference shows **every** row, filter included 【一手 — the
picker's tail reads "↓ 100 more" against the reference's 105]. The filter is
gone from the clone too.

### 3.4 `/codesearch`'s usage line

The reference's panel reads

    Usage: /codesearch [-F|--literal] <query>

The clone advertised two extra filters it *accepts* but the reference does not
mention. The visible line is now the reference's, byte for byte; the two
filters still work.

### 3.5 `/skills`: the icon travels out of band

The reference's skill catalogue carries the frontmatter's `metadata.icon` —
but **not** in the payload and **not** in the name:

* `agy -p /skills` prints `automation` (the bare frontmatter `name`);
* the JSON payload carries exactly `name`, `description`, `path`, `plugin`,
  `builtin`, `model_invocable` — **no** icon key;
* the **panel** renders `<icon> <name>` — `schedule automation` for the
  bundled automation skill;
* the skill's **detail view** heads with the same string.

The icon is rendered **verbatim**: the reference filters it against no table
of known glyphs, so a skill declaring `metadata.icon: zzznotanicon` lists as
`zzznotanicon zziconprobe` 【一手 — a probe skill, since removed】.

The clone therefore keeps the icon **next to** the name rather than inside it
(`printmode.SkillInfo.Icon` is `json:"-"`), and the built-in group is sorted
by the **bare** name — `agy-customizations`, `antigravity-guide`,
`automation`, `migrate-workflows`, `run-workflow` — which is why "automation"
sits between "antigravity-guide" and "migrate-workflows" even though its row
reads "schedule automation".

### 3.6 `/config`: the union and the 1.4.8 defaults

Two changes, both verified byte for byte against the reference in **three**
settings states (populated, empty `{}`, and a probe file carrying nulls and
file-only keys) — the text dump and the `--output-format json` payload alike.

**The union.** The dump carries the reference's 36 known keys **plus every key
present in `settings.json`**. A file-only key (`dismissedAnnouncements` on
the capture machine) sits at its alphabetical place, between
`disableSlashCommands` and `editor`.

**The defaults.** The clone carried the 1.3.6 defaults. The 1.4.8 defaults
are:

| key | 1.3.6 (clone) | 1.4.8 |
|---|---|---|
| `artifactReviewPolicy` | `always-proceed` | `asks-for-review` |
| `toolPermission` | `always-proceed` | `request-review` |
| `colorScheme` | `light` | `terminal` |

and the seven nullable keys — `customModelsConfig`, `gcp`, `permissions`,
`sandboxProxy`, `statusLine`, `title`, `trustedWorkspaces` — are **null** when
they are absent (not an empty object, not an empty list).

**A null in the file decodes to the zero value of the key's type**: a string
key becomes `""` (not its default), a number key `0`, a boolean key `false`; a
key the reference does not know keeps its `null`.

The official settings reference agrees with every default it lists — including
`toolPermission: request-review` and `artifactReviewPolicy:
asks-for-review`. It differs in one place: it lists `trustedWorkspaces`'s
default as `[]`, while the reference emits `null` when the key is absent
【一手】.

### 3.7 `init.permission_mode`

The init event's `permission_mode` reads the `toolPermission` setting and
falls back to its default. With an empty `settings.json` the reference
reports **`request-review`** 【一手 — probed on a machine whose
`settings.json` was emptied and restored】. The `always-proceed` the
cycle-113 capture carries came from a capture machine whose `settings.json`
set `toolPermission` explicitly; the file value wins. The 1.1.0 changelog
agrees ("Added `request-review` (default) mode").

### 3.8 `/usage`'s text shape

The reference's `/usage` answers with **one tab-separated record per line**
— `<group>\t<bucket>\t<pct>%\t<RFC3339 UTC>` — four columns per row. The
clone's text answer was a separate "Quota:" line; it is now the payload
rendered. The *figures* are live in both (the percentage moved by one point
between the two runs of the sweep), so a byte-for-byte diff of `/usage` is
unattainable by construction — the shape is what is pinned.

### 3.9 `/changelog`'s blob

The reference's `/changelog` prints its own embedded release blob: 176 788
bytes at 1.4.8, topping out at the `## 1.4.7` section. The clone's embedded
fixture was the 1.4.0 capture; it is now the 1.4.8 blob, and the sweep
confirms the two outputs are byte-identical.

### 3.10 `/credits`: the live answer

The reference answers `/credits` with two tab-separated records, both read
from the account's own code-assist state:

    Remaining credits	0
    Upgrade	https://antigravity.google/g1-upgrade

and its JSON payload carries `{"remaining_credits":0,"upgrade_uri":"…"}`.

The clone used to reject the command outright ("no credits info found") —
a claim that is simply false: the account's state **is** readable. The
answer now comes from `v1internal:loadCodeAssist`, the same endpoint the
clone already reads for the Web UI's code-assist state:

* the balance is the sum of the `paidTier.availableCredits` entries'
  `creditAmount` fields — the capture account's paid tier carries one entry
  with **no** amount, and the reference reports `0`;
* the purchase link is the paid tier's `upgradeSubscriptionUri`, verbatim.

The reference's own failure text for an account without a paid tier is
`GetG1Credits: paidTier is nil` 【strings】, and the clone now produces it.

Both the text answer and the JSON payload are byte-identical to the
reference's.

### 3.11 The TUI's `/credits` opens the panel

The reference opens the **credits panel** for `/credits` — on the picker path
*and* on the direct path (the composer holding "/credits" with the picker
dismissed) 【一手 — live in tmux】:

    Antigravity CLI  credits
    Model AI Credits

    Remaining AI Credits: AI Credits not enabled (enable in /settings)

    Actions

    > Get More AI Credits
      See Activity

    Keyboard: ↑/↓ Navigate  enter Select / Toggle  esc Close

The clone already carried that panel (its text matches the capture byte for
byte), but the *command* answered with an error line instead of opening it.
It opens the panel now.

---

## 4. The evidence

| what | how | grade |
|---|---|---|
| the documented command list | the docs' slash-command catalogue and CLI reference | 【一手】 (read) |
| the reference's `/teamwork` name | the picker's row; the string table's only `teamwork-preview` hits are a changelog line and a packed blob | 【一手】 + 【strings】 |
| `/teamwork-preview` is not a command | `agy -p /teamwork-preview` produced no local output in 100 s; `agy -p /teamwork` answered in 12.7 s | 【一手】 |
| the print-mode table (§2) | the 23-command sweep of both binaries | 【一手】 |
| `/statusline` toggles | the reference's four answers, live | 【一手】 |
| the exit-notice set | ten literal messages plus the generic format string in the binary's string table; the live captures | 【strings】 + 【一手】 |
| the picker's 105 rows | the picker's tail ("↓ 100 more") and the `/help` COMMANDS tab ("[1-33 of 105 items]") | 【一手】 |
| the `/skills` icon split | the print-mode text, the JSON payload, the panel capture and the detail heading; the probe skill's rendering | 【一手】 |
| `/config`'s union and defaults | three settings states, both surfaces, `diff` against the reference | 【一手】 |
| the defaults' corroboration | the docs' settings table | 【一手】 (read) |
| `init.permission_mode` | the init event with an emptied `settings.json`; the 1.1.0 changelog line | 【一手】 |
| `/credits`' two records and its payload | `agy -p /credits` and `--output-format json`; the live `loadCodeAssist` answer | 【一手】 |
| `GetG1Credits: paidTier is nil` | the binary's string table | 【strings】 |
| the credits panel's text | a live tmux capture of the reference's panel | 【一手】 |

---

## 硬约束 — hard constraints

1. **The binary is the authority for the command names, not the docs.**
   `/teamwork-preview` is the docs' primary name and is *not* a command in
   1.4.8; `/teamwork` is. The clone answers `/teamwork`.
2. **A documented command that the binary does not implement must stay
   text.** Both binaries forward `/teamwork-preview` to the model as literal
   text; making it a command in the clone would *break* the parity.
3. **The skill's icon never enters the payload or the name.** The payload has
   exactly six keys and the name is the bare frontmatter `name`; the icon
   travels next to it (`json:"-"`).
4. **The icon is rendered verbatim.** There is no glyph table and no
   filtering — an unknown icon name is shown as it is.
5. **The exit-notice set is the binary's own.** Ten literal messages plus one
   generic format string; the clone does not invent a notice for a command
   the reference keeps silent.
6. **`/config` is a union, and the defaults are 1.4.8's.** A key the user has
   set is reported even when the reference does not know it, and the built-in
   defaults are the ones the reference reports for an absent key.
7. **An explicit `null` in `settings.json` decodes to the zero value of the
   key's type**, not to the built-in default.
8. **`/credits` is live account state.** It is read from
   `v1internal:loadCodeAssist`; the clone does not fabricate a balance.
9. **The panels are opened, not printed, for `/credits`** — on the picker path
   and on the direct path alike.

---

## 存疑 — open questions

1. **`/usage`'s live figures.** The clone's percentages and reset times are
   placeholders (the quota service is not reachable from the clone), so the
   *shape* is pinned but the numbers are not. The reference's own numbers
   move as the quota is consumed.
2. **`/credits` on an account without a paid tier.** The reference's error
   text is known from the string table (`GetG1Credits: paidTier is nil`), but
   the way the reference *wraps* it in print mode was not observed — the clone
   wraps it in its own `"/credits failed: retrieving credits: %w"`, which is
   the wrapper the reference's own string table also carries.
3. **The credit balance's aggregation rule.** The sum over
   `availableCredits[].creditAmount` is inferred from a single observation
   (one entry, no amount, balance 0). An account with several credit types
   would settle it.
4. **The panel surface of the other panel commands on the *direct* path.**
   The reference opens a panel for `/skills`, `/usage`, `/context`,
   `/permissions`, `/diff`, `/effort`, `/model`, `/agents`, `/changelog`,
   `/codesearch` and `/plugin` when the command is submitted without the
   picker; the clone prints their content instead. This is a **structural**
   difference in how the clone renders panel commands, it predates this
   cycle, and it is not closed here — closing it means routing every panel
   command through `openSlashPanel`, which is a larger change than one
   cycle's alignment.
5. **`/credits`' panel with G1 credits enabled.** The capture account has
   `useG1Credits` off, so the panel reads "AI Credits not enabled (enable in
   /settings)". What the reference's panel shows when the credits *are*
   enabled was not observed.

---

## Limitations

1. **The print-mode sweep is a 14-second window.** A command whose answer
   takes longer (the eight model-backed ones) is recorded as "model-backed in
   both", not as byte-identical. The eight are `/boost`, `/browser`, `/goal`,
   `/grill-me`, `/learn`, `/plan`, `/schedule` and `/teamwork`.
2. **The TUI was probed on one account and one window size** (220×50), and
   only for the commands whose panel shape this cycle changed.
3. **The settings probes touched the user's `settings.json`.** Every probe
   was backed up first and the file restored afterwards; the backups are in
   the operator's scratch directory.
4. **The colour tests in `internal/cliapp` were red while this cycle ran** —
   a concurrent palette refactor in the shared tree changed the hard-coded
   colour expectations. None of the failures is in a file this cycle
   changed; the packages this cycle owns (`internal/printmode`,
   `internal/skillsui`, `internal/tuilib`) are green.
5. **`/usage`'s figures and `/credits`' balance are live.** A byte-for-byte
   diff of either is only meaningful at one instant.

---

## Files

| | |
|---|---|
| `internal/printmode/print_slash_credits.go` | **new** — the live `/credits` answer and the account-state parser |
| `internal/printmode/print_slash_config.go` | the typed defaults, the key-set union, the text dump |
| `internal/printmode/print_format_config.go` | the value resolver (absent → default, null → zero value, present → the file's value) |
| `internal/printmode/print_format.go` | `printPermissionMode` — the `request-review` fallback |
| `internal/printmode/print_slash_skills.go` | the out-of-band icon (`SkillInfo.Icon`, `skillFrontmatterFields`) |
| `internal/printmode/print_slash_usage.go` | `/usage`'s text answer — the payload rendered |
| `internal/printmode/print_slash.go` | the `/credits` dispatch |
| `internal/printmode/testdata/print_changelog.md` | the 1.4.8 blob (176 788 bytes) |
| `internal/printmode/print_slash_test.go` | the config shapes, the defaults, the icon, the credits parser |
| `internal/printmode/print_format_test.go` | the typed defaults, the null rule, the permission mode |
| `internal/skillsui/panel_row.go` | `DisplayName` — the panel's `<icon> <name>` |
| `internal/skillsui/panel_model.go` | the built-in order, keyed on the bare names |
| `internal/skillsui/panel_icon_test.go` | **new** — the display name and the row |
| `internal/cliapp/slash_commands_handlers.go` | the `/statusline` toggle, the `/credits` panel |
| `internal/cliapp/slash_panels.go` | the exit-notice set |
| `internal/cliapp/slash_codesearch_render.go` | the usage line |
| `internal/cliapp/slash_skills_panel_view.go` | the detail heading's display name |
| `internal/cliapp/slash_content_test.go` | the `/credits` panel |
| `internal/tuilib/commands.go` | the 108-row catalogue |
| `internal/tuilib/feedback.go` | `StatuslineToggledOffMessage` |
| `docs/guide/tui.md` | the cycle's guide section |
