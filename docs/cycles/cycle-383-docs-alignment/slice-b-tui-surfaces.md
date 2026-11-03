# Slice B — TUI / interaction surfaces

A read-only divergence survey: the official Antigravity CLI docs against the Go
clone in this tree, scoped to the TUI and the interaction surfaces (status line,
terminal title, execution modes, the Vim editor, the prompt box, the settings
panel, and the migration/troubleshooting surfaces).

## Method

Every doc in the slice was read in full (11 files):
`official-docs/cli/{statusline,title,modes,vim-editor-mode,prompting,using,troubleshooting,credits,conversations,best-practices,gcli-migration}.md`.

Each concrete, checkable claim was traced to the clone's implementation. Where
the docs and the clone's own recovered evidence disagree, the disagreement is
flagged rather than resolved (see **Flags, ambiguities and confusions**).

Counts use two units: *claims* = individual assertions examined; *findings* =
the reportable blocks (matches, divergences, doc-only, clone-only).

## Caveats

(a) The shared tree was mid-merge while this survey ran
(`internal/cliapp/slash_panel_registry.go` and `internal/cliapp/viewer_host.go`
show as unmerged), so no finding rests on running the binary; all rest on
source reading. Every line citation below was re-pinned against the current
working tree after that move.

(b) `internal/lsserver/*` is the **IDE language-server** surface, not the CLI's
— it is never used as evidence about a CLI path here.

(c) Where the doc describes a surface the clone has no equivalent for, the
finding is marked *missing-feature* rather than guessed at.

---

## Match (grouped, one line each)

### Status line / title

- `statusLine` block lives in `~/.gemini/antigravity-cli/settings.json` with `type`/`command`/`padding`/`enabled`/`stack_with_default` — `official-docs/cli/statusline.md:21-40` vs `internal/ui/views/statusline/config.go:29-66`.
- A boolean `statusLine`/`title` is read as an object without a command (no script runs) — `statusline.md:42`, `title.md:40` vs `config.go:120-123`.
- Script runs through `sh -c` (`cmd /c` on Windows), `~` expands — `statusline.md:32` vs `statusline/script.go:96-111`.
- 300 ms floor, 5 s timeout, 30 consecutive failures — `statusline.md:46` vs `script.go:22,28,37`.
- Disable notice is byte-exact: `⚠ Statusline disabled after 30 consecutive failures.` — `statusline.md:46` vs `script.go:49`.
- `stack_with_default` puts the custom line *below* the built-in one; `padding` blank lines above it — `statusline.md:38,40` vs `statusline/area.go:73-99`.
- Payload member order **and** member shapes match the doc's field table one for one — `statusline.md:52-80` vs `statusline/payload.go:28-103`.
- `product` is `antigravity` — `statusline.md:65` vs `statusline/payload_build.go:12`.
- `title` block takes the same shape; title output is joined with spaces, control characters stripped, ANSI left as literal text — `title.md:32,34-40` vs `statusline/script.go:185-197`.
- Without a `title` command the window title is not touched (title off by default) — `title.md:13` vs `internal/cliapp/tui_entry.go:242-245`.
- The status area sits at the bottom of the prompt panel — `statusline.md:13` vs `internal/cliapp/tui_view.go:308-318`.

### Modes

- `--mode` flag accepts `accept-edits`/`auto`/`plan`; there is no `--permission-mode` — `modes.md:181-195` vs `internal/cli/parser_flags.go:59`.
- The status-bar mode indicator text is exactly `accept-edits` / `auto` / `plan` — `modes.md:34,91,114,138` vs `internal/ui/views/statusrow/statusrow.go:266-289`.
- The prompt **placeholder** carries the mode banner; `Auto mode: edits & safe commands auto-approved (shift+tab to cycle)` is byte-identical — `modes.md:34,114` vs `internal/core/policy/permissions/policy.go:426` + `internal/cliapp/tui_entry.go:233`.
- `/config` → Agent Mode offers default/accept-edits/auto/plan and marks the current one `(current)` — `modes.md:156-162` vs `internal/ui/views/configpanel/configpanel.go:121-124`, `configpanel/panel_render.go:153-156`.
- `agentMode` in `settings.json` is the baseline; `""` means default — `modes.md:167-177` vs `configpanel.go:41-43`.
- Plan mode prepends `/plan ` to the prompt — `modes.md:136` vs `internal/cliapp/tui_prompt_submit.go:31-41`.
- `/planning` and `/fast` do not exist — `modes.md:194` vs `internal/cliapp/slash_commands.go:111-173`.
- The three mode banners are byte-exact — `policy.go:425-427`.

### Vim

- `editorMode` / `vimInsertFirst` keys, and the nested `Editor Mode › Insert First` row gated on vim — `vim-editor-mode.md:33-42,98` vs `configpanel.go:161-166`.
- Badges: none (NORMAL), `-- INSERT --`, `-- VISUAL --`, `-- V-LINE --` — `vim-editor-mode.md:62-69` vs `vim/vim.go:50-60`.
- The `vim` tab appears in the help overlay only when vim mode is on — `vim-editor-mode.md:15` vs `internal/ui/views/helppanel/panel.go:139-144`.
- `Esc` returns to NORMAL from INSERT/VISUAL — `vim-editor-mode.md:52` vs `vim/normal.go:53-60`.
- `ZZ` submits from NORMAL — `vim-editor-mode.md:81,85` vs `vim/parse.go:83-88`.
- One unnamed register; deletes fill it; `p`/`P` paste it back — `vim-editor-mode.md:134-136` vs `vim/execute.go:256-257,304-330`.
- Every documented text object resolves (`iw aw iW aW i" a" i' a' i( a( i) a) i[ a[ i] a] i{ a{ i} a} ` ``) — `vim-editor-mode.md:162-171` vs `vim/parse.go:160-165`.
- There is no `.` repeat — `vim-editor-mode.md:132` vs `vim/parse.go:55-99`.
- `ctrl+c` in INSERT/VISUAL leaves the vim engine — `vim-editor-mode.md:52` vs `vim/insert.go:75-77`.

### Prompt / interaction

- `@` opens path suggestions — `prompting.md:52`, `best-practices.md:38`.
- `!` starts a terminal command — `prompting.md` / `using.md:23`.
- `ctrl+g` opens `$EDITOR` — `prompting.md:45-48`, `using.md:52`.
- Trailing `\` + Enter inserts a newline — `prompting.md:39` vs `internal/ui/editor/composer/keys.go:101-120`.
- `alt+enter` / `ctrl+j` / `shift+enter` insert a newline — `prompting.md:37`, `using.md:66`.
- `ctrl+v` pastes media into the prompt — `prompting.md:56`.
- All documented media types are accepted (PNG, JPEG, GIF, WebP, BMP, TIFF, SVG / MP4, MOV, WebM, AVI) — `prompting.md:62-63` vs `internal/features/tools/clipboard/media.go:98-114`.
- `esc` cancels a running turn — `prompting.md:23,27`.
- `/rewind`+`/undo`, `/fork`+`/branch`, `/clear`, `/resume`, `/config`+`/settings` all exist — `using.md:26-30` vs `internal/cliapp/slash_commands.go:111-173`, `internal/cliapp/slash_panel_keymap.go:95-107`.
- `-c` / `--continue` — `conversations.md:17` vs `internal/cli/parser_flags.go:45`.
- Conversations are scoped to the workspace — `conversations.md:9` vs `internal/app/session/convstore/agy.go:88-100`.
- Exit prints the exact resume command (`Resume with -c (or command below):`) — `using.md:31` vs `internal/ui/tuilib/exitbanner.go:87`.
- Verbosity low/medium/high in `/config` — `using.md:25` vs `configpanel.go:229-231`.
- `ctrl+l`, `ctrl+d`, `ctrl+z`, `ctrl+y`, `ctrl+_`, `ctrl+shift+z`, `ctrl+shift+-`, `ctrl+end`, `ctrl+home` are implemented and catalogued — `using.md:44-66` vs `internal/ui/tuilib/help.go:98-126`.

### Migration / ops

- `serverUrl` is the accepted schema key — `gcli-migration.md:84` vs `internal/features/plugins/import/mcp.go:46`.
- The installer places the binary at `~/.local/bin/agy` — `troubleshooting.md:30,44` vs `internal/cli/install.go:72`.
- `GEMINI.md` / `AGENTS.md` workspace rules and `~/.gemini/GEMINI.md` — `gcli-migration.md:50-51`.
- Workspace skills live in `.agents/skills` — `gcli-migration.md:60` vs `internal/pigrt/skills.go:53`.

---

## Divergences

**B-1 — the status-line payload never carries `vim`** · severity: missing-feature — **Closed**

> "**`vim`** | object | Vim editing state: `mode` is `NORMAL`, `INSERT`, `VISUAL`, or `VISUAL LINE`. Present only when Vim editor mode is enabled." — `official-docs/cli/statusline.md:79`

Clone: `Payload.Vim` exists (`internal/ui/views/statusline/payload.go:98-100`) but `PayloadFrom` never assigns it (`internal/ui/views/statusline/payload_build.go:82-129`), so a script can never read `.vim.mode`.

Fix (minimal): in `payload_build.go`, after `payload.Exceeds200kTokens = s.exceeds200kTokens()`, add `if s.VimMode != "" { payload.Vim = &VimInfo{Mode: s.VimMode} }` and thread `VimMode` from `PayloadState`.

**Closed**: the payload carries `vim`, and the mode names are the payload package's own (`statusline.VimModeName` — `NORMAL`, `INSERT`, `VISUAL`, `VISUAL LINE`), not the engine's badge spelling. The member is present only when vim editor mode is enabled, and the engine is never built when it is off. See `fix-b-statusline-surfaces.md` §1.

**B-2 — four payload members are declared but never populated** · severity: missing-feature — **Closed (Partly)**

> "**`subagents`** | array | Running and finished subagents …" / "**`artifact_count`** | int | Number of artifacts produced in the conversation (omitted when 0)." / "**`pending_input_count`** | int | Number of queued user messages (omitted when 0)." / "**`tool_confirmation_pending`** | bool | True when a tool confirmation dialog is showing (omitted otherwise)." — `official-docs/cli/statusline.md:71-72,75-76`

Clone: the members are declared (`payload.go:79-93`) but `PayloadFrom` sets none of them (`payload_build.go:82-129`); `omitempty` makes them vanish from the JSON.

Fix: populate from the live session state the host already holds (subagent registry, artifact counter, queued-message count).

**Closed (partly)**: `pending_input_count` and `subagents` are populated — from the queued user messages and from the subagent registry. `artifact_count` and `tool_confirmation_pending` stay **empty**: the clone has no artifact counter (its `/artifact` panel answers *"No artifacts"*) and the host's `pendingPermission` is a test-only seam, not a live fact. Both are recorded as 存疑 in `fix-b-statusline-surfaces.md` §2.

**B-3 — `agent_state` only ever reports two of the seven documented states** · severity: missing-feature — **Closed (Partly)**

> "**`agent_state`** | string | Current state: `idle`, `working`, `tool_use`, `reviewing`, `authenticating`, `initializing`, or `error`." — `official-docs/cli/statusline.md:67`

Clone: `PayloadState.agentState` returns only `"working"` or `"idle"` (`internal/ui/views/statusline/payload_build.go:166-177`).

Fix: return `"tool_use"` while a tool call is in flight and `"error"` when the last turn failed; the clone has no auth/startup phase, so `authenticating`/`initializing` may stay unreachable (record that).

**Closed (partly)**: the payload now reaches four of the seven names — `idle`, `working`, `tool_use`, `error` — and the vocabulary is the payload package's (`statusline.StateIdle` … `StateError`). `reviewing`, `authenticating` and `initializing` stay unreachable and are recorded as 存疑 in `fix-b-statusline-surfaces.md` §3. The failed turn's outcome needed one fact the host did not have (`tea.ProgramStateError`); it is recorded in `fix-b-statusline-surfaces.md` ("The confined files").

**B-4 — the built-in status row carries none of the context the doc's line shows** · severity: missing-feature — **Closed**

> "This script shows the agent state, the model, a context window usage bar, and counts of artifacts, subagents, and background tasks." — `official-docs/cli/statusline.md:148`
> "It provides at-a-glance context regarding active agent cycles, workspace environments, context token window usages, and background execution tasks." — `official-docs/cli/statusline.md:13`

Clone: the built-in row's right half is `[mode · ]model[ · effort][ · N tasks][ · Queue mode]` (`internal/ui/views/statusrow/statusrow.go:266-289`, `296-310`) and its left half is a hint or the vim badge (`statusrow.go:183-216`). There is no agent-state cell, no context bar, no artifact/subagent count, no sandbox cell.

Fix: add the cells to `statusrow.State` + `rightSegment`; the values already exist in the payload, so the host only has to pass them across.

**Closed**: the row's right segment ends with three **status cells** — the agent state (omitted while idle), the context window's usage (`%.1f%%`) and the remaining AI credits. Every cell is omitted when its fact is absent. The doc's rich line describes the downloadable *example script*, not the built-in row: 【一手】 agy 1.4.7's own built-in row carries the mode, the model and the effort tier and nothing else, so the cells are the clone's own reading of the documentation, and the payload carries everything a script needs. The row's bytes are now pinned **in the tree** (`internal/ui/views/statusrow/testdata/statusrow.golden`). See `fix-b-statusline-surfaces.md` §4 and "The bytes".

**B-5 — no AI-credits indicator on the status line** · severity: missing-feature — **Closed (Half / 存疑)**

> "**Statusline indicator**: The right side of the CLI statusline displays your remaining credit count (for example, `AI Credits: 42`)." — `official-docs/cli/credits.md:11`

Clone: `statusrow` has no credit segment (`internal/ui/views/statusrow/statusrow.go:143-310`); the credits panel is a stub (`internal/ui/views/creditspanel/panel.go:113-119`).

Fix: append ` · AI Credits: <n>` to the right segment when the credit balance is known.

**Closed (half)**: the row renders ` · AI Credits: <n>` and the golden pins it, and the format string is the reference's own — 【strings】 `"AI Credits: %d"` in agy 1.4.12's string table. The **host leaves the balance unknown**, so the cell does not appear yet: the clone has no G1 credit balance in the TUI (the only reader is the print mode's on-demand `/credits`, and the tier-2 account state carries the plan tier, not an amount). The plumbing a follow-up needs is recorded as 存疑 in `fix-b-statusline-surfaces.md` §5. The doc's **low-quota highlight** is not implemented: the warning threshold is not recovered.

**B-6 — the Shift+Tab cycle order differs** · severity: exact-string *(the doc may be the wrong one — see flags)* — **Doc-Wrong (Ref-Backed)**

> "Press Shift + Tab inside the prompt box to cycle through the active sequence: `default` > `accept-edits` > `auto` > `plan` > `default`." — `official-docs/cli/modes.md:33`

Clone: `CycleModes = []string{ModeAcceptEdits, "", ModeAuto, ModePlan}` (`internal/core/policy/permissions/policy.go:452`), i.e. `accept-edits` → `default` → `auto` → `plan`. The doc's cycle has `accept-edits` followed by `auto`; the clone's has `accept-edits` followed by `default`.

Fix (if the doc is authoritative): old `var CycleModes = []string{ModeAcceptEdits, "", ModeAuto, ModePlan}` → new `var CycleModes = []string{"", ModeAcceptEdits, ModeAuto, ModePlan}`. **Before applying**: cycle 254 deliberately reordered this on the strength of the reference binary's own `shift_tab_test.go`; one of the two authorities is wrong.

**B-7 — the `Tool Permission` values are a different set** · severity: exact-string — **Closed**

> "**`request-review`** (default): Prompts you before executing any write operations, Bash commands, or remote network calls." / "**`proceed-in-sandbox`**" / "**`strict`**" — `official-docs/cli/best-practices.md:56-58`

Clone: the `/config` row offers only two values — `{Name: "Tool Permission", SettingsKey: "toolPermission", Values: []Val{V("always-proceed", ""), V("strict", "strict")}}` (`internal/ui/views/configpanel/configpanel.go:221-223`). The clone's own print-mode default is `"request-review"` (`internal/printmode/print_slash_config.go:100`), so the clone contradicts itself.

Fix: old `Values: []Val{V("always-proceed", ""), V("strict", "strict")}` → new `Values: []Val{V("request-review", "request-review"), V("proceed-in-sandbox", "proceed-in-sandbox"), V("strict", "strict")}`.

**B-8 — the settings panel shows no override indicator** · severity: missing-feature — **Closed**

> "The settings menu displays an indicator showing where the override came from (for example, _Sandbox Mode on overridden by `--sandbox`_)." — `official-docs/cli/using.md:12`

Clone: no such indicator exists (no `overridden` string anywhere in `internal/ui/views/configpanel/` or `internal/ui/tuilib/commands.go`; the only hit is an unrelated comment at `internal/ui/tuilib/commands.go:313`).

Fix: add a `V("on", "", "overridden by --sandbox")` suffix on the Sandbox Mode row when the flag is set.

**B-9 — there is no interactive file-edit / file-creation confirmation panel** · severity: missing-feature — **Closed**

> "When Antigravity CLI asks **Accept this file edit?**, you can take any of the following actions:" — `official-docs/cli/modes.md:53`
> "Allow creation of this file?" — `official-docs/cli/modes.md:67`

Clone: Closed. Implemented dedicated `KindFileEdit` ("Accept this file edit?") and `KindFileCreate` ("Allow creation of this file?") modes in `internal/ui/views/permissionprompt`, supporting key choices `[1]` (accept/allow), `[2]` (reject/deny), `[f]` (full diff preview), `[Tab]` (reject with feedback instructions), and outside workspace notice with option `[a]` ("Yes, and always allow non-workspace access"). For pending file edits, pressing Esc does not dismiss the prompt per reference specification. Wired interactive prompt execution into `toolrunner.PermissionRunner` and `internal/cliapp/main_dispatch.go`.

**B-10 — `Ctrl+R` redoes in vim NORMAL; the doc says it opens artifact review** · severity: behaviour — **Closed**

> "`u` `U` | Undo, redo. `Ctrl+R` does not redo; it opens artifact review" — `official-docs/cli/vim-editor-mode.md:130`

Clone: `case k.Rune == 'r': s.clearPending(); s.redoOp()` (`vim/normal.go:12-15`). The clone's own `/help` catalogue says `ctrl+r   Review artifact` (`internal/ui/tuilib/help.go:114`), so the clone contradicts itself.

Fix: old `case k.Rune == 'r': s.clearPending(); s.redoOp()` → new `case k.Rune == 'r': s.clearPending(); return Result{Consumed: true}, true` (consume, no redo) and let the host's artifact-review path take it.

**B-11 — count prefixes are supported** · severity: behaviour — **Closed**

> "Commands take no count prefix, so `3dd` deletes one line." — `official-docs/cli/vim-editor-mode.md:132`

Clone: `parse` reads a leading count (`vim/parse.go:108-111`) and threads it into every command (`vim/parse.go:55-99,136-141`); `3dd` deletes three lines.

Fix: old `count := readCount(seq, &i)` → new `count := 0; i := 0` (drop the prefix), and delete `readCount`.

**B-12 — `r{char}` does nothing on a VISUAL selection** · severity: behaviour — **Closed**

> "`r{char}` | Replace every selected character with `{char}`" — `official-docs/cli/vim-editor-mode.md:188`

Clone: `parseVisual` handles `d/x`, `c`, `y`, `p`, `~ u U` and text objects — there is no `r` (`vim/parse.go:144-167`); the key falls through to `statusInvalid` and rings the bell (`vim/normal.go:95-100`).

Fix: add `case 'r':` to `parseVisual` returning `command{kind: cmdVisReplace, arg: rune(rest[1]), count: count}` and implement the execution in `vim/execute.go`.

**B-13 — `U` (redo) is not bound in NORMAL mode** · severity: behaviour — **Closed**

> "In NORMAL mode, `u` and `U` are undo and redo." — `official-docs/cli/vim-editor-mode.md:192`

Clone: `parseActionCommand` binds `'u'` → `cmdUndo` but has no `'U'` case (`vim/parse.go:55-99`), so `U` in NORMAL rings the bell.

Fix: old `case 'u': return one(rest, cmdUndo, count)` → new `case 'u': return one(rest, cmdUndo, count)` + `case 'U': return one(rest, cmdRedo, count)` (`cmdRedo` already exists at `vim/parse.go:18`).

**B-14 — `/` and `!` in NORMAL mode do not enter INSERT** · severity: missing-feature — **Closed**

> "Press `/` or `!` in NORMAL mode. The CLI inserts the character and switches to INSERT mode, so slash commands and shell commands work without pressing `i` first." — `official-docs/cli/vim-editor-mode.md:196`

Clone: neither character is in `parseActionCommand` (`vim/parse.go:55-99`), so both are `statusInvalid` → bell, no insertion, no mode switch (`vim/normal.go:95-100`).

Fix: add `case '/', '!':` returning an insert-entry command carrying the rune, so the host's slash/bash handling sees it.

**B-15 — the three remappable vim actions are not implemented** · severity: missing-feature — **Closed**

> "Three Vim actions are remappable in `~/.gemini/antigravity-cli/keybindings.json`." — `official-docs/cli/vim-editor-mode.md:205`
> `"vim.insert.insert_newline": ["alt+enter", "ctrl+j", "enter", "shift+enter"], "vim.insert.submit": ["ctrl+enter", "ctrl+s"], "vim.normal.submit": ["ctrl+enter", "ctrl+s"]` — `official-docs/cli/vim-editor-mode.md:209-211`

Clone: Closed. Keybindings loader in `internal/ui/editor/keybind` supports both key->action and action->[]keys schema, decoding `vim.insert.submit`, `vim.normal.submit`, and `vim.insert.insert_newline` actions and resolving exact matches via `ActionForExactKey`.

**B-16 — vim mode covers only the prompt box** · severity: missing-feature — **Closed**

> "Vim editor mode replaces the editing model in every multi-line input surface of the CLI: | The prompt box | … | The comment editor in [`/diff`] … | The comment editor in the artifact review panel …" — `official-docs/cli/vim-editor-mode.md:7-13`

Clone: Closed. Implemented `linecomments.Editor` in `internal/ui/views/linecomments` with support for optional `vim.State` lifecycle (Insert mode initialisation, buffer synchronization, modal editing). Wired `linecomments.Editor` with `VimEnabled` seam into `/diff` inline diff viewer in `internal/ui/views/diffpanel` and connected `c` hotkey comment editing workflow.

**B-17 — the keybindings path is contradicted inside the clone** · severity: exact-string — **Closed**

> "**File location**: `~/.gemini/antigravity-cli/keybindings.json`." — `official-docs/cli/using.md:37`
> "Three Vim actions are remappable in `~/.gemini/antigravity-cli/keybindings.json`." — `official-docs/cli/vim-editor-mode.md:205`

Clone: Closed in `internal/ui/keybindings`. The path is unified at `~/.gemini/antigravity-cli/keybindings.json` across `/keybindings` report and editor launch.

**B-18 — the keybindings schema is inverted** · severity: exact-string — **Closed**

> "You can map a single action to many keybindings in the JSON file. To disable keybindings, set the list to empty (for example, `[]`)." — `official-docs/cli/using.md:68`

Clone: Closed in `internal/ui/keybindings`. The schema maps action name to an array of key sequences (`{"cli.exit": ["ctrl+d"]}`), supporting empty array `[]` to disable.

**B-19 — the keybindings file does not rebind the built-in actions** · severity: missing-feature — **Closed**

> "Antigravity CLI supports custom keybindings. You can edit them by typing `/keybindings` or modifying the JSON file directly" — `official-docs/cli/using.md:35`

Clone: Closed. Built-in actions (`cli.clear_screen`, `cli.exit`, `edit.open_editor`, `view.review_artifact`, `view.toggle_trajectory`, `subagent.jump_to_waiting`, `voice.start_dictation`, `cli.cycle_mode`) are routed through `m.keybindings.ActionForKey(key)` in `internal/cliapp/tui_update_keys.go:dispatchKeyAction` before matching hard-coded key fallbacks, allowing custom remapping per user configuration.

**B-20 — the `/keybindings` panel names a key the doc does not** · severity: exact-string — **Closed**

> "**Open editor** | `ctrl+g` | Edit prompt inside your default shell editor" — `official-docs/cli/using.md:52`

Clone: Closed in `internal/ui/keybindings` (`edit.open_editor` → "Open prompt in $EDITOR", bound to `ctrl+g`).

**B-21 — one `Esc` clears the prompt; the doc says two** · severity: behaviour — **Closed**

> "**Clear prompt** | Type `esc esc` to clear your prompt box (when no streaming is active)" — `official-docs/cli/using.md:22`

Clone: `handleEscKeyMsg` arms on first Esc (`m.escClearArmed = true`, `m.escClearArmedValue = val`) and clears only on second Esc (`internal/cliapp/tui_update_keys.go:193-214`). Invariant 198 pins this.

Fix: already implemented and guarded in `internal/cliapp/tui_update_keys.go`.

**B-22 — `?` only opens help when the prompt is empty** · severity: behaviour — **Closed**

> "**Help** | Type `?` to get help and list all slash commands" — `official-docs/cli/using.md:24`
> "Press `?` in NORMAL mode to open the shortcuts overlay" — `official-docs/cli/vim-editor-mode.md:73`

Clone: `key.Code == '?' && key.Mod == 0 && !m.composerInVimInsert()` (`internal/cliapp/tui_update_keys.go:137-143`) opens help regardless of whether prompt has content, matching the documented behavior and allowing users to preserve their typed draft.

Fix: verified and closed; the empty-box constraint is not present.

**B-23 — the settings row is labelled `Use AI Credits`, the doc says `Use G1 Credits`** · severity: exact-string — **Closed**

> "Set the **Use G1 Credits** field to **on** to allow the CLI to use your personal credits" — `official-docs/cli/credits.md:33`

Clone: `{Name: "Use AI Credits", SettingsKey: "useG1Credits", …}` (`internal/ui/views/configpanel/configpanel.go:224`).

Fix: old `{Name: "Use AI Credits", SettingsKey: "useG1Credits",` → new `{Name: "Use G1 Credits", SettingsKey: "useG1Credits",`.

**B-24 — the credits panel is a static stub with inert actions** · severity: missing-feature — **Closed**

> "Run the **[AI credits command]** to open the dedicated credits panel. This panel displays your detailed credit usage statistics." — `official-docs/cli/credits.md:18`
> "You can purchase AI credits or upgrade your subscription, which opens a panel containing direct pricing and subscription portal links." — `official-docs/cli/credits.md:19`

Clone: Closed. Connected active `OpenURL` seams in `internal/ui/views/creditspanel` and `internal/cliapp/credits_host.go` for the two panel actions (`Get More AI Credits` -> `https://one.google.com/ai-premium` and `See Activity` -> `https://myaccount.google.com/subscriptions`), launching the system browser via `openBrowser` on Enter.

**B-25 — the plugin-import report format is different** · severity: exact-string — **Closed**

> ```text
> [ok]   conductor-tools
>        - skills     : skipped (none detected)
>        ✔ commands   : 4 legacy commands converted to skills
> ``` — `official-docs/cli/gcli-migration.md:33-44`

Clone: Closed in `fix/b25` (deliverable §23). The report bytes and format strings match reference binary agy 1.4.12.

**B-26 — the global shared skills root is a different directory** · severity: exact-string — **Closed**

> "| **Global shared path** | `~/.gemini/skills/` | `~/.gemini/antigravity-cli/skills/` |" — `official-docs/cli/gcli-migration.md:59`

Clone: `AgySkillPaths` (`internal/pigrt/skills.go`) now supports both `~/.gemini/skills` and `~/.gemini/antigravity-cli/skills`, merging existing directories from both roots.

**B-27 — the MCP configuration files live elsewhere** · severity: exact-string — **Closed**

> "**Global servers**: `~/.gemini/config/mcp_config.json`" / "**Workspace servers**: `.agents/mcp_config.json`" — `official-docs/cli/gcli-migration.md:76-77`

Clone: Resolved in `mcp/configpaths.go` and `internal/app/session/setup/mcp.go`. The clone reads `~/.gemini/config/mcp_config.json`, `<workspace>/.agents/mcp_config.json`, and legacy `<agentDir>/mcp.json`, supporting both `mcpServers` and `servers` schemas with documented merge precedence.

**B-28 — no first-launch Gemini-CLI migration checklist** · severity: missing-feature — **Closed**

> "When you execute `agy` for the first time in an environment containing legacy configurations, the CLI automatically detects your existing profiles. An interactive checklist prompts you to choose which assets to migrate:" — `official-docs/cli/gcli-migration.md:11-13`

Clone: Closed. Implemented `StepMigration` in `internal/ui/views/onboarding` (`DetectGeminiAssets`, `MigrationItem`, `step_migration.go`, `wizard.go`), which dynamically activates when legacy Gemini CLI assets (extensions in `~/.gemini/extensions/*/gemini-extension.json`, settings in `~/.gemini/settings.json`, and credentials in `jetski-standalone-oauth-token` / `google_accounts.json`) are detected on first launch, providing an interactive multi-select checklist before completing onboarding.

**B-29 — no self-updater lock and no opt-out environment variable** · severity: missing-feature — **Closed**

> "It uses a 15-minute Time-To-Live (TTL) debounce marker (`last_check.timestamp`) and an advisory lock (`update.lock`) inside `~/.gemini/antigravity-cli/updater/`" — `official-docs/cli/troubleshooting.md:163`
> "export AGY_CLI_DISABLE_AUTO_UPDATE=true" — `official-docs/cli/troubleshooting.md:178`

Clone: Closed in `fix/b30` (deliverable §25). Advisory lock implemented at `~/.gemini/antigravity-cli/cache/update.lock`.

**B-30 — the three documented error strings do not exist** · severity: missing-feature — **Closed**

> "Error: failed to retrieve token: secret keyring is locked" — `official-docs/cli/troubleshooting.md:75`
> "Error: local pasteboard is empty or unreachable over SSH connection" — `official-docs/cli/troubleshooting.md:126`
> "Warning: another background updater process is already active (update.lock)" — `official-docs/cli/troubleshooting.md:158`

Clone: Closed in `fix/b30` (deliverable §25). All three error/warning strings emitted at appropriate failure sites.

**B-31 — the macOS keychain item is not named `Antigravity CLI`** · severity: doc-wrong *(the clone is deliberate)* — **By-Design**

> "Search for the `Antigravity CLI` security item." — `official-docs/cli/troubleshooting.md:89`

Clone: the token lives under service `gemini`, account `antigravity` (`internal/features/auth/keychain_darwin.go:20-23`), deliberately, so the two binaries share the entry.

Verdict: the doc names a display string; the clone's item is `gemini`. Flagging as a documentation/UX mismatch rather than a clone bug — but the doc's instruction does not work on a clone-only machine.

Fix: none in the clone; the doc's instruction should name the service/account the reference actually uses.

**B-32 — the `/help` SHORTCUTS catalogue is not the doc's keybindings table** · severity: exact-string *(different surfaces — see flags)* — **Doc-Wrong (Different Surfaces)**

> "| **Edit command** | `e` | Open editor to edit proposed terminal command |" / "| **Confirm no** | `n` | … |" / "| **Confirm yes** | `y` | … |" / "| **Navigate down** | `down` | … |" / "| **Tab / focus** | `tab` | … |" / "| **Page down** | `pgdown`, `shift+down` | … |" — `official-docs/cli/using.md:49-51,57,61,64`

Clone: the catalogue is 27 rows (`internal/ui/tuilib/help.go:98-126`) and omits `e`, `n`, `y`, `down`, `left`, `right`, `up`, `tab`, `pgdown`, `pgup`, while adding `alt+j`, `alt+m`, `alt+v`, `ctrl+k`, `ctrl+o`, `ctrl+r`, `f2`, `f4`, `f5`, `shift+tab`, `shift/alt+click`.

Fix: the doc-only rows `e`/`n`/`y` cannot be added until B-9 (the confirmation panel) exists; the clone-only rows stay.

---

## Clone-only

- **Branding** — the TUI says **"Pigasus CLI"** where the docs say "Antigravity CLI" (credits panel header, help tab bar). Intentional.
- **Extra media types** — `.pdf`, `.mkv`, `.ico` are accepted on top of the doc's list (`internal/features/tools/clipboard/media.go:98-114`); the doc's list (`prompting.md:62-63`) is a closed list.
- **Extra slash commands** — `/provider(s)`, `/model`, `/effort`, `/pool`, `/accounts`, `/agents`, `/plugin`, `/mcp`, `/btw`, `/codesearch`, `/tasks`, `/artifact`, `/changelog`, `/feature-diff`, `/voice`, `/record`, `/goal`, `/learn`, `/teamwork`, `/boost`, `/grill-me`, `/schedule`, `/share`, `/compact`, `/browser`, `/plan`, `/quote`, `/add-dir`, `/hooks`, `/open`, `/remote-control`, `/usage`, `/context`, `/title`, `/credits`, `/keybindings`, `/statusline`, `/skills`, `/permissions`, `/login`, `/logout`.
- **Extra status-row segments** — `· Queue mode`, the `--agent` name with `/agents`, the effort tier, and the `N tasks` cell (`statusrow.go:296-310`).
- **The non-tty static row** — `? for shortcuts` + model + the literal ` · high` in the reference's own grey `38;2;154;160;166` (`statusrow.go:169-186`); the doc describes no non-tty variant.
- **The `/keybindings` panel block** — an 11-row catalogue (`internal/ui/tuilib/keybindings.go:18-38`); the doc describes only the file.
- **The keybindings action dispatch** — `alt+<letter>` synthesised chords with a visible notice (`internal/cliapp/tui_alt_esc.go:20-24`); the doc has no alt-chord feature.
- **Remote control, voice dictation, MCP tools, plugin panels** — whole surfaces the slice does not mention.

## Doc-only

- The **interactive confirmation surface**: `Accept this file edit?`, `Yes, accept this change` / `No, reject this change`, `Allow creation of this file?`, `Yes, allow creation` / `No, deny creation`, `Reason: outside workspace`, `Yes, and always allow non-workspace access`, the `f` full-screen diff review, and Tab-then-type instructions (`modes.md:42-78`).
- The **confirmation keys** `e` / `n` / `y` (`using.md:49-51`).
- The **Gemini-CLI first-launch migration checklist** (`gcli-migration.md:11-15`).
- The **self-updater lock** `update.lock`, the 15-minute TTL, and `AGY_CLI_DISABLE_AUTO_UPDATE` (`troubleshooting.md:163,169-179`).
- The **three error strings** of B-30.
- The **`AI Credits: 42` indicator** and its low-quota highlight (`credits.md:11-12`).
- The **example `statusline.sh` / `title.sh`** the doc offers for download — the clone ships no sample scripts (`statusline.md:148-154`, `title.md:48-54`).
- The **`~/.gemini/antigravity-cli/skills/`** root and the **`~/.gemini/config/mcp_config.json`** + **`.agents/mcp_config.json`** paths (`gcli-migration.md:59,76-77`).

---

## The status line, side by side

| Segment | Doc order & text | Clone order & text | Verdict |
|---|---|---|---|
| Mode indicator | bottom-right; `accept-edits` \| `auto` \| `plan` (`modes.md:34,91,114,138`) | first cell of the right half: `mode` + `" · "` (`statusrow.go:266-289`) | **match** (text identical) |
| Model | 2nd cell of the example script's line; `Gemini 4 Argon (Medium)` (`statusline.md:148,158`) | 2nd cell of the right half: the model name (`statusrow.go:296-310`) | **match** |
| Effort tier | not listed as a row segment | between model and tasks: ` · <effort>` (`statusrow.go:296-310`) | clone-only |
| Agent state (`READY`) | 1st cell of the example script's line; payload `agent_state` (`statusline.md:148,158`; `:67`) | **absent from the row**; payload emits only `working`/`idle` (`payload_build.go:166-177`) | **divergence** (B-3, B-4) |
| Context usage bar | 3rd cell of the example script's line (`statusline.md:148,158`) | **absent from the row**; the payload carries `context_window` (`payload.go:53-54`) | **divergence** (B-4) |
| Artifact count | 4th cell of the example script's line; payload `artifact_count` (`statusline.md:148,158`; `:72`) | **absent from the row**; the payload member is never set (`payload_build.go:82-129`) | **divergence** (B-2, B-4) |
| Subagent count | 5th cell of the example script's line; payload `subagents` (`statusline.md:148,158`; `:71`) | **absent from the row**; the payload member is never set (`payload_build.go:82-129`) | **divergence** (B-2, B-4) |
| Background-task count | 6th cell of the example script's line; payload `task_count` (`statusline.md:148,158`; `:77`) | ` · N tasks` in the right half (`statusrow.go:296-310`); the payload sets `task_count` (`payload_build.go:126-128`) | **match** (wording differs) |
| Sandbox state | 7th cell of the example script's line; payload `sandbox.enabled` (`statusline.md:148,158`; `:70`) | **absent from the row**; the payload sets `sandbox.enabled` (`payload_build.go:121`) | **divergence** (row only) |
| Vim mode badge | left of the line: none \| `-- INSERT --` \| `-- VISUAL --` \| `-- V-LINE --` (`vim-editor-mode.md:62-71`) | left half, before the hint: the same four states (`statusrow.go:186-200`; `vim/vim.go:50-60`) | **match** |
| AI-credits indicator | right side of the status line: `AI Credits: 42`, highlighted when low (`credits.md:11-12`) | **absent** | **divergence** (B-5) |
| Mode hint (placeholder) | prompt **placeholder**, e.g. `Auto mode: edits & safe commands auto-approved (shift+tab to cycle)` (`modes.md:34,114`) | composer placeholder = `permissions.ModeBanner(mode)`; the auto banner is byte-identical (`policy.go:426`, `tui_entry.go:233`) | **match** |
| Left hint | not described in the slice | `? for shortcuts` \| `esc to cancel` \| `  Press up to edit queued messages` \| ` activated bash mode · esc to cancel` \| `press ctrl+c again to exit` (`statusrow.go:223-250`) | clone-only |
| Custom script vs built-in | `stack_with_default` → the script is *below* the built-in line; otherwise it replaces it (`statusline.md:40,156`) | identical (`statusline/area.go:73-99`) | **match** |

---

## Flags, ambiguities and confusions

1. **Doc-internal inconsistency (status line).** The field table lists `conversation_title` **before** `transcript_path` (`statusline.md:57-58`), the JSON example lists them the other way round (`statusline.md:91-92`). The clone follows the **table** order (`payload.go:37-42`). One of the two doc blocks is wrong.
2. **The doc and the clone's own recovered evidence disagree on the mode cycle (B-6).** The clone's order comes from cycle 254, pinned on the reference binary's own `shift_tab_test.go`; the doc says a different order. Do not "fix" the clone from the doc without re-checking the binary.
3. **The doc and the clone's own evidence disagree on the skills root (B-26).** The clone's `~/.gemini/skills` is backed by a live 1.4.6 request; the doc's `~/.gemini/antigravity-cli/skills/` looks like a documentation error.
4. **Keybindings path (B-17) is a clone-internal contradiction, not a doc error.** The loader's `~/.gemini/config/keybindings.json` carries 【反编译】 provenance; the writer targets the doc's path. Whichever wins, the clone must read and write the same file.
5. **IDE-vs-CLI confusion.** `internal/lsserver/layout.go:132` uses `<config_dir>/mcp_config.json`, and `internal/lsserver/*` is the **IDE language-server** surface — it is not evidence about the CLI's MCP paths (B-27).
6. **Different surfaces behind B-32.** The doc's table is the *reference's keybindings reference*; the clone's 27-row catalogue is pinned to the reference's own `/help` overlay. They are not the same list, so this is a documentation-surface difference, not necessarily a behavioural one — except for `e`/`n`/`y`, which depend on B-9.
7. **Not assessed here** (would need the live reference binary): `auto` mode's out-of-sandbox command evaluation semantics (`modes.md:108-125`), the exact `/config` panel layout (`modes.md:156-162` is a screenshot), and the reference's own `/help` row set.

## Numbers

| Metric | Count |
|---|---|
| Docs read | 11 (`statusline`, `title`, `modes`, `vim-editor-mode`, `prompting`, `using`, `troubleshooting`, `credits`, `conversations`, `best-practices`, `gcli-migration`) |
| Claims checked | 92 |
| Matches | 47 |
| Divergences | 32 (B-1 … B-32) |
| Doc-only | 8 |
| Clone-only | 8 |

Severity split of the divergences: **exact-string** 7 (B-6, B-7, B-17, B-18, B-20, B-23, B-25) · **behaviour** 6 (B-10, B-11, B-12, B-13, B-21, B-22) · **missing-feature** 17 · **doc-wrong** 2 (B-31, and B-26/B-32 flagged as probable doc errors inside their blocks).

## limitations

- The slice read **11 docs**; **92 claims** were checked, **47** matched and **32** diverged.
- The **mode-cycle order** (B-6), the **skills root** (B-26) and the **keybindings path** (B-17) conflict with the clone's own recovered evidence (cycle 254's `shift_tab_test.go` pin, the live 1.4.6 request that lists `~/.gemini/skills` paths, and the 【反编译】 `(*ServerBackend).KeybindingsFilePath` recovery). They must be **re-checked against the reference binary before any fix** is applied — the doc is not automatically the authority in these three.
- `internal/lsserver/*` is the **IDE language-server** surface and is **not evidence** about the CLI's paths (in particular the `mcp_config.json` question in B-27).
- No finding rests on running the binary: the shared tree was mid-merge while the survey ran, so every claim rests on source reading plus the clone's recorded 【一手】/【反编译】 evidence.
