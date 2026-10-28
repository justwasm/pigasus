# Cycle 362 deliverable: the clone against agy 1.4.9

**Cycle:** `cycle-362-agy-1.4.9-alignment`
**Subject:** the vendor's 1.4.9 release notes carry **fourteen** bullets.
This cycle maps every one of them onto the clone: six are implemented, five
are not applicable to the clone's architecture, and three are recorded as
open with the reason.
**Evidence:** 【一手】 first-hand against the 1.4.9 binary (its own stdout,
its own string table) · 【反编译】 decompiled output at confirmed addresses ·
【strings】 string-table hits only · 【推测】 hypothesis

---

## 0. The short answer

| # | the upstream bullet | the clone |
|---|---|---|
| 1 | image pasting (clipboard, dropped file, SSH, the "no image" sentence) | **done** (`e7e04707`) — and the chain's break fixed (`fe5e390c`, §7.2) |
| 2 | the `/plugin` Discover tab groups the marketplace into sections | **open** — the taxonomy is not in the binary; it must come from the marketplace response |
| 3 | `agy plugin validate` checks the name and the logo | **done** (`1daab67c`) — the manifest's `logo`; the note's `iconUri` clause has no landing point in `plugin.json` (probed, §4) |
| 4 | the `/diff` position indicator says what it counts | **open** — the clone's `/diff` has no window, so there is no "1-44" range to print |
| 5 | Auto mode's reviewer knows whether the cwd is a repository | **not applicable** — the clone has no model-driven reviewer |
| 6 | `execute_url` approvals no longer cover sibling Google apps | **not applicable** — the clone has no `execute_url` tool |
| 7 | always-allow rules for multi-line commands take effect | **open** — the clone has no path that derives an always-allow rule from a command |
| 8 | the onboarding screens fit a small terminal | **open** — no first-hand capture of the reference's wrapped output |
| 9 | Vim: `r<space>`, the empty last line, the carried operator | **done** (`86c2577c`) |
| 10 | the invisible U+FFF9 / U+FFFA never reach the terminal | **done** (`4fa00ea3`) |
| 11 | a corrupt image is rejected with the real header offset | **done** (`1c1c25aa`) |
| 12 | an unmatched tool call or result is left out of the request | **done** (`8801cef3`) |
| 13 | an outdated thinking block is dropped, not rejected | **not applicable** — the clone never replays a thinking block |
| 14 | the `/browser` subagent gets the page id with the snapshot | **not applicable** — the clone does not speak Chrome DevTools MCP |

Six of fourteen are done, four are not applicable, four are open.

---

## 1. Where the 1.4.9 notes came from

The *served* changelog feed — the one `agy changelog` prints — **lags**: the
feed re-captured first-hand against the 1.4.9 binary on 2026-10-28 is
180,524 bytes / 1,038 lines / 79 sections / 881 bullets and its **newest
section is still 1.4.8**. The 1.4.9 notes therefore live only in the
vendor's own repository, and they are recorded **verbatim** in
`internal/cli/testdata/changelog_upstream_1.4.9.md` (the section hashes to
`cdba835b4e50c310f45f07b84a538986acf85545a8cc2a22a87b2129383ea37b`), the
same way the 1.4.8 notes were. The clone's changelog still prints exactly
what the reference prints.

The version pin moved with the binary: `internal/cli.ReferenceVersion` is
**1.4.9** (sha256 `a66421b7…`), and `internal/referencedrift/reference.lock.json`
follows.

---

## 2. Item 12 — an unmatched tool call or result is left out

> "Fixed conversations getting stuck failing on every turn after a tool
> result was left without its matching tool call, such as after older
> history was trimmed; unmatched tool calls and results are now left out of
> the request."

【一手】 the API pairs the two halves itself, so a call without its result
(or a result without its call) fails the whole request.

The rule has **one** implementation, in the CloudCode dialect:
`geminicode.PairedToolCallIDs` walks the transcript, pairs each call with
its result, and `buildContents` drops both halves that have no partner.
The Gemini API-key dialect (`internal/backend/gemini.go`) applies the same
rule, so the clone does not send a request the server will reject.

---

## 3. Item 10 — the invisible U+FFF9 and U+FFFA

> "Fixed invisible U+FFF9 and U+FFFA characters reaching the terminal
> around wrapped code in agent replies, which made some terminals and
> multiplexers draw boxes or leave stale characters on screen."

The characters ride along with the model's own text, so they are dropped
where the reply is sanitised (`internal/agent/sanitize.go`) — the same place
the think blocks are dropped, and therefore on the interactive **and** the
print path. A stream chunk may end in the middle of one of the three-byte
sequences, so the trailing bytes are held back and prepended to the next
chunk: the halves are then recognised and dropped as one character instead
of reaching the terminal as two broken runes.

---

## 4. Item 3 — the plugin validator's name and logo checks

> "Improved `agy plugin validate` to warn when a plugin's `name` isn't
> lowercase kebab-case … and to suggest a valid name, failing under
> `--strict`; it now also checks the plugin's `logo`, noting when
> plugin.json has none and warning when the logo path is blank, uses
> backslashes, is absolute or points outside the plugin, sits in a folder
> the marketplace bundle leaves out such as `.github/`, or is a URL that
> won't be shown, with the same checks for context provider `iconUri`
> values."

【一手】 every message is the 1.4.9 binary's own, read off **its stdout**
with one crafted fixture per branch; a differential run over 108 fixtures
(three flag combinations each) is byte-identical to the reference. This
cycle re-ran five of those fixtures against the installed 1.4.9 binary
(`https://example.com/logo.png`, `../outside.png`, `logo.txt`,
`/abs/logo.png` and a blank logo) — the clone's output is byte-identical
to the reference's, warning for warning.

### The difference, before and after

| the manifest | before | after |
|---|---|---|
| `name: "Neo4j"` | accepted | warned: `name "Neo4j" isn't strict kebab-case (lowercase letters, digits and single hyphens), so it can't be published to the Antigravity Marketplace; try "neo4j"` |
| `name: "foo_bar"` | accepted | the same shape, `foo-bar` suggested |
| `name: "foo.bar"` | accepted | the same shape, plus "`plugin install` refuses it" |
| `name: "foo bar"` | accepted | the same shape, plus "`plugin install` refuses it, it isn't valid under the Agent Plugins spec" |
| `name` not kebab-case, `--strict` | exit 0 | exit non-zero |
| no `logo` at all | accepted | noted (not a warning): the manifest has no logo |
| `logo: ""` | accepted | warned: the path is blank |
| `logo: "a\\b.png"` | accepted | warned: the path uses backslashes |
| `logo: "/abs/logo.png"` | accepted | warned: the path is absolute |
| `logo: "../outside.png"` | accepted | warned: the path points outside the plugin |
| `logo: ".github/logo.png"` | accepted | warned: the Marketplace bundle leaves that folder out |
| `logo: "logo.txt"` | accepted | warned: the path is not an image path |
| `logo: "logo.png"` (missing file) | accepted | warned: the file does not exist |
| `logo: "https://example.com/logo.png"` | accepted | warned: the client will not show an image from that host |
| `logo: "https://www.google.com/logo.png"` | accepted | accepted — a Google domain is shown |
| a context provider's `iconUri` | unchecked | **still unchecked** — see the probe below |

### The `iconUri` clause, probed

The upstream note ends with "with the same checks for context provider
`iconUri` values". 【一手】 the 1.4.9 binary does **not** apply them to a
context provider declared in `plugin.json` — probed on 2026-10-29 with one
fixture per shape, and every one of them passed with only the manifest's own
"no logo" note:

| the fixture | the reference says |
|---|---|
| `"extensions": {"contextProviders": {"a": {"iconUri": "https://example.com/logo.png"}}}` | nothing |
| `"extensions": {"contextProviders": [{"name": "a", "iconUri": "https://example.com/logo.png"}]}` | nothing |
| the same, with the iconUri `/abs/logo.png`, `""`, `a\b.png`, `../outside.png`, `logo.txt` | nothing |
| `"extensions": {"iconUri": "https://example.com/logo.png"}` | nothing |
| `"contextProviders": {...}` at the top level | `⚠ plugin.json: unknown field "contextProviders" is ignored` |
| `"iconUri"` at the top level | `⚠ plugin.json: unknown field "iconUri" is ignored` |
| a `contextProviders/a.json`, `contexts/a.json`, `context_providers/a.json` or `providers/a.json` in the plugin directory | nothing, and the counter line (`skills 0 · agents 0 · rules 0 · hooks 0 · mcpServers 0 · sidecars 0`) does not change |

So the note's `iconUri` clause has **no landing point in the manifest**: the
shared checks exist in the reference's format templates (their leading `%s`
is the field's name — `logo`, `iconUri`), but the manifest is not where an
`iconUri` is read. The clone keeps the manifest's behaviour it verified, and
records the clause as open rather than inventing a manifest shape.

The leading `%s` of the reference's own format templates is the **field's**
name, so the clone's one implementation of the checks takes the field it is
checking. "extensions" joined the manifest's known fields, and a non-object
`extensions` is warned about the way the reference does
(`⚠ plugin.json: "extensions" isn't an object, so no context providers load
from it`).

---

## 5. Item 9 — Vim: `r<space>`, the empty last line, the carried operator

> "Fixed Vim mode's `r`, `f`, `F`, `t`, and `T` canceling when the target
> character is a space, so `r<space>`, `f<space>`, and `df<space>` now
> work, commands on an empty last line acting on the line above, and a
> half-typed operator such as `d` carrying over after you submit a prompt."

- `vim/empty_line.go` — the commands that act at the cursor (`x`, `X`, `D`,
  `C`, `r`, `~` and the `d`/`c`/`y` operators) leave an empty last line for
  the line above, at that line's last character.
- `vim/operator.go` — `deleteRange` no longer slices past the end of an
  empty line. **"dw" on an empty last line used to panic.** (The trap is
  Go's `[]rune("")`: its capacity is 32, so `rl[0:1]` does *not* panic and
  returns a NUL, while `rl[1:]` does.)
- `vim/vim.go` — `SetTextKeepingPending` keeps a half-typed operator across
  the text replacement a submit causes; a carried operator may only
  *complete*, so it never swallows the next key.
- `internal/cliapp/vim_mode.go` — the submit re-seeds the editor with the
  pending operator intact.
- `vim/space_target_test.go` — pins `r<space>`, `f<space>`, `F<space>`,
  `t<space>`, `T<space>` and `df<space>`, which already worked.

---

## 6. Item 11 — a corrupt image is rejected with the real offset

> "Fixed conversations failing every later model request with
> `INVALID_MEDIA` after the agent viewed a corrupt image or one with text
> in front of its header … such images are now rejected with an error that
> says where the real image header starts, so the agent can fix the
> command."

- `imagery/header.go` — `HeaderOffset` finds the real header of a payload
  whose first bytes are not the header; `HeaderOffsetError` carries the
  reference's own wording: `invalid %s: header found at byte offset %d
  after leading non-image bytes`.
- `internal/tools/image_probe.go` — `read_file` and `view_file` reject such
  an image, the same shape as the WebM probe.
- `internal/clipboard/paste.go` — a pasted payload with leading non-image
  bytes is rejected with the same error.

The rule is deliberately narrow: a payload whose header is **not** at byte 0
is rejected; a payload with **no** header at all keeps the old answer.

---

## 7. Item 1 — image pasting

> "Added image pasting through your terminal's own paste shortcut, such as
> `Cmd+V` or `Ctrl+Shift+V` with only an image copied, and by dragging
> image files into the prompt, which attaches them instead of inserting
> their paths; image paste now also works over SSH with X11 or Wayland
> forwarding, and the CLI's paste key now says `No image or video found in
> the clipboard` when there is nothing to attach and names the clipboard
> tool to install, such as `wl-clipboard` on Wayland, when one is missing."

### The reference's own strings 【strings】

Read off the 1.4.9 binary's string table:

| the string | address |
|---|---|
| `No image or video found in the clipboard` | `0x362eb55` |
| `Clipboard file URL read error for %s: %v` | `0x362ea15` |
| `Can't read your clipboard over SSH: connect with ssh -X (and install xclip on this host) to paste images` | `0x36d097f` |
| the file-URL AppleScript (below) | `0x36d4698` |
| the image AppleScript (below) | `0x36f8736` |
| `PNGfJPEGGIFfTIFFsips` — the class codes and `sips` | `0x340827f` |
| `Implicit clipboard media probe: result=%v err=%v` | `0x365aac9` |
| `cli_clipboard_paste` — the paste key's action name | `0x3543405` |

### The two AppleScript sources, verbatim

The file-URL one (the clipboard holds a **file** — a dragged or copied
image):

```
try
	set fileURL to the clipboard as «class furl»
	return POSIX path of fileURL
on error
	return ""
end try
```

The image one (the clipboard holds **image bytes**; `%q` is the temporary
file the caller chooses, `%s` is the AppleScript class):

```
set tempFile to %q
try
	set imageData to the clipboard as «class %s»
	set f to open for access POSIX file tempFile with write permission
	set eof f to 0
	write imageData to f
	close access f
	return tempFile
on error
	return ""
end try
```

The class codes are tried in the order the reference's own string table
carries them: **PNGf, JPEG, GIFf, TIFF**. TIFF and GIF are converted with
`sips` — the macOS image tool the same string names — because the
imagery pipeline decodes PNG, JPEG and WebP natively.

### What the clone does

```
paste (ctrl+v, or the terminal's own bracketed paste)
   │
   ├─ the text names an image file ─────► attach the image (the path is
   │                                       NOT inserted into the composer)
   ├─ the text is non-empty ────────────► paste it as text (unchanged)
   └─ the text is empty ────────────────► probe the platform clipboard
                                            │
                                            ├─ an image  ─► attach it
                                            ├─ nothing   ─► the composer says
                                            │                "No image or video
                                            │                 found in the clipboard"
                                            ├─ no helper  ─► the composer names
                                            │                the tool to install
                                            │                ("wl-clipboard")
                                            └─ no display over SSH ─► the
                                               reference's ssh sentence
```

- `internal/clipboard/media.go` — the `Media` shape, the sentinel errors
  (`ErrNoMedia`, `ErrSSHClipboard`, `*ToolMissingError`) and
  `MediaFromPath`, which recognises a dropped file's path (quoted,
  backslash-escaped, or a `file://` URL).
- `internal/clipboard/media_darwin.go` — the reference's two AppleScript
  sources, verbatim, plus the class list and the `sips` fallback.
- `internal/clipboard/media_linux.go` — `wl-paste` on Wayland, `xclip` on
  X11, and the reference's SSH sentence when the session has no display.
- `internal/clipboard/media_other.go` — the platforms with no reader answer
  with the reference's "no image" sentence.
- `internal/cliapp/tui_paste_media.go` — the one paste entry point
  (`handlePasteContent`), the attachment indicator, and the conversion to
  the transport's image parts.
- The attachment waits in `runMode.pendingMedia`, is shown above the
  composer, and rides the next user turn as `backend.Message.Images` — the
  shape the transports already carry (CloudCode's `inlineData`, the
  gateway's and Anthropic's content parts, PiG's `ImageContent`).

### The reference's own two routes 【反编译】

The 1.4.9 binary's function names were recovered with `rea-go-seeds`
(166,903 functions) and located with `llvm-objdump`; every address below is
the 1.4.9 macOS/arm64 image, and **every address moves on every release**.

| the reference's function | address | what it is |
|---|---|---|
| `model.ReadClipboardMedia` | `0x102afb810` | the clipboard's media entry point (it tail-calls `readClipboardDataDarwin`) |
| `model.readClipboardDataDarwin` | `0x102afaad0` | the AppleScript reader |
| `model.readClipboardFileDarwin` | `0x102afb540` | the file-URL reader |
| `model.PromptModel.handlePasteMedia` | `0x102bee4a0` | the **explicit** paste route |
| `model.PromptModel.handlePasteMedia.func1` | `0x102c561e0` | its deferred close |
| `model.PromptModel.handleImplicitPasteMedia.func1` | `0x102c56040` | the **implicit** probe's closure — it carries the log line `Implicit clipboard media probe: result=%v err=%v` |
| `model.pastedMediaPaths` | `0x102bae280` | the pasted text's path extraction |
| `model.splitPastedPaths` | `0x102bae460` | it splits **several** paths out of one paste |
| `model.resolvePastedPath` | `0x102bae870` | it resolves one of them |
| `model.readPastedMediaFiles.func1` | `0x102c4e180` | the per-file reader |
| `model.isITerm2` | `0x102afd2b0` | iTerm2 detection |
| `model.startITermUpload` | `0x102afc620` | iTerm2's own upload path |

`handlePasteMedia` is called from `PromptModel.Update` at `0x102be4bf0`, and
`handleImplicitPasteMedia`'s command is built in the same `Update` at
`0x102be19ac` — whose type switch's **first** case carries the type
descriptor `0x10929cc70` (size 16, pointer size 8, kind 25 = struct, one
string field), which is the shape of `tea.PasteMsg{Content string}`.

Two routes, therefore:

| the route | the trigger | the reference's function |
|---|---|---|
| **explicit** | the paste key | `handlePasteMedia` |
| **implicit** | the terminal's own bracketed paste delivered an **empty** content | `handleImplicitPasteMedia` |

The clone's single `handlePasteContent` covers both: it is reached from the
paste key (`ctrl+v`) and from the terminal's bracketed paste, and an empty
paste is what makes it probe the clipboard.

`handlePasteMedia` also carries the SSH sentence (`Can't read your clipboard
over SSH: …`, `0x1036d097f`) and calls `isITerm2` → `startITermUpload`, so
the reference has a **third** route for iTerm2 (where the terminal can
upload the picture itself). The clone has no iTerm2 upload.

### The break in the clone's chain, and the fix

The paste half of item 1 worked; the **transport** half did not. The TUI read
the clipboard, the picture went into `backend.Message.Images`, and the turn
then **dropped it on the way into PiG**: `internal/agent/pig_messages.go`
built every user turn as `pi.UserText(message.Content)`, so the attachment
stopped there. The model answered the text and never saw the image — the
feature was half-wired end to end.

The fix renders the user turn through one new function, `userContent`:

- a turn with **no** picture stays the union's plain-text arm — every byte
  the clone sent before it learned to attach images keeps going out the same
  way;
- a turn that **carries** pictures becomes a block list: the text first,
  then one `pi.ImageContent` per picture, the payload base64-encoded
  because PiG's image block holds it as a string.

That is the shape PiG's own `coding.BuildUserContent` builds, and it is the
shape the transports read back: `piagentbridge.userImages` (CloudCode,
Gemini, Anthropic) and `piagentbridge.userContentParts` (the gateway's
OpenAI-shaped array) read the image blocks and nothing else. A picture with
no bytes is dropped rather than sent as an empty one — the model would
answer a broken image with a guess.

`internal/agent/pig_messages_test.go` pins the five shapes: the plain-text
arm survives, the picture becomes an image block, an empty picture is
dropped, the picture survives the trip *through* PiG and back out as the
transport's image part, and it reaches the gateway request body as a
`data:image/png;base64,…` `image_url` part.

### The one behavioural difference

The reference **probes implicitly** — its own log line is "Implicit
clipboard media probe: result=%v err=%v" — and the reverse engineering above
says when: an **empty** bracketed paste. The clone probes exactly then too
(an empty paste is what makes it probe the clipboard), so the two routes
collapse into the clone's single `handlePasteContent`.

What is still different, in the reference's favour:

| the reference | the clone |
|---|---|
| registers the paste key's **action name** `cli_clipboard_paste` (`0x3543405`) | no action name — the key is wired directly |
| has an **`alt+v`** paste key — but that is the **1.0.15** bullet ("Added `alt+v` as an alternative paste shortcut on Windows to resolve issues where ctrl+v is intercepted by the terminal emulator"), not 1.4.9's | `ctrl+v` only; `alt+v` is left out deliberately, because it is not a 1.4.9 feature and it collides with the Vim engine's `alt` chords |
| splits **several** paths out of one paste (`splitPastedPaths`) and reads each (`readPastedMediaFiles.func1`) | `clipboard.MediaFromPath` recognises **one** path |
| has an **iTerm2** route (`isITerm2` → `startITermUpload`), where the terminal uploads the picture itself | no iTerm2 upload |
| reads a **file URL** off the clipboard (`readClipboardFileDarwin`) and names the file in the attachment | the clone reads the file URL too (`MediaFromPath` accepts a `file://` URL), but does not keep the file's name |

---

## 8. The eight bullets the clone does not model

### Item 2 — the `/plugin` Discover tab's sections

【strings】 the section names are **not** in the binary: "Developer Tools",
"Productivity", "DevOps" and "Data & Analytics" have **zero** hits in the
1.4.9 binary, and the marketplace's own generated types (from the 1.4.5
evidence) carry no section field. The grouping therefore comes from the
marketplace **response**, whose shape the clone has not captured. Listing a
plugin under *every* section it belongs to needs that taxonomy, and
inventing one would be guesswork.

### Item 4 — the `/diff` position indicator

【strings】 the reference's three format strings are known — `  [%d-%d of
%d files]` (`0x3565cf0`), `[%d-%d of %d commits]` (`0x3565d03`) and
`  [%d-%d of %d rows]` (`0x3554999`) — and the upstream note names the
semantics ("turn headers count as rows", so the indicator no longer
disagrees with the file count). The clone's `/diff` has
**no window**: it renders every row of the list, so there is no "1-44"
range to print. Producing the indicator would first require inventing a
scroll model the clone does not have.

### Item 5 — Auto mode's reviewer

The reference runs a **model** that reviews a command which would leave the
terminal sandbox, and 1.4.9 teaches that reviewer whether the working
directory is inside a repository so it stops spending its time budget on
`git status` in folders that are not repositories. The clone's permission
engine is a **static policy** (`internal/permissions`) with no model in the
loop, so there is no reviewer to teach.

### Item 6 — `execute_url`

The clone has **no `execute_url` tool** (`grep -rn execute_url internal/`
returns nothing). Approving `www.google.com` cannot leak into
`mail.google.com` in a clone that never asks about a URL.

### Item 7 — always-allow rules for multi-line commands

The reference's always-allow entry is a *grant string* built from the
command, and the reference's own sentence is `Yes, and always allow for
commands that start with '%s'`. The clone has **no path that derives an
always-allow rule from a command** — its rules come from `settings.json`,
from the `/permissions` panel, or from the Remote Control dashboard — so the
rule the 1.4.9 note is about cannot be created in the clone, and the
failure it describes (the CLI asking again and piling up unusable entries)
cannot happen. The clone's own matcher is a prefix match on the whole
target, so a multi-line pattern that *is* in `settings.json` matches.

### Item 8 — the onboarding screens in a small terminal

The reference now wraps the onboarding text and links to the terminal width,
crops or hides the colour preview in a very narrow terminal, shows the
banner only when the whole step fits, and scrolls an over-tall step with
`Shift+↑`/`Shift+↓` or `PgUp`/`PgDn` while the buttons stay visible. The
clone's onboarding (`internal/onboarding`) is a **stdout wizard**: it
prints a screen and reads keys, so it has no screen to scroll and no
buttons to pin. Wrapping is possible, but the reference's wrapped output
was not captured first-hand, and the clone's parity tests assert the
*unwrapped* lines — so a guessed wrapping rule would replace a verified
rendering with an unverified one.

### Item 13 — an outdated thinking block

The failure needs a **thinking block in the history**: the server rejects a
conversation whose prefix no longer matches the block it holds. The clone
never replays a thinking block — `geminicode`'s `Part` carries the
`thought` flag, but `buildContents` only emits text and function-call
parts, and the model's `thoughtSignature` is replayed on the **function
call**, which is the shape the reference uses too. A stale thinking block
therefore cannot exist in a clone request.

### Item 14 — the `/browser` subagent's snapshot

The bug is in **Chrome DevTools MCP 1.11**: the snapshot request must carry
the page id, and the reference's request left it out, so the snapshot
failed silently. The clone does not speak Chrome DevTools MCP — its browser
surface is the in-Chrome bridge (`browserbridge`) and its own CDP client
(`browsercdp`) — so it has no such request to fix.

---

## 9. The `/model` effort slider — one row, N tiers (clone-side)

This section is **not** one of the fourteen upstream bullets. It is a
clone-side change this cycle carried, because the 1.4.9 alignment work
put the clone's own tiered models in front of the picker and the picker's
shape had to follow.

### 9.1 The reference's shape

The reference's `/model` picker shows **one row per model**, and a model
that offers more than one reasoning tier puts those tiers on the
**effort slider** underneath the rows — not on rows of their own. The
reference's own catalogue has exactly one such model (Gemini 3.8 Flash,
three tiers: `low`, `medium`, `high`); every other reference row is a
fixed model, and the whole slider block — together with the `←/→ Effort`
legend entry — disappears on it.

【一手】 the geometry was measured on the reference's own capture
(`fin_model_down.raw.txt`, cycle 208):

- the field between the two arrows is **45 cells wide**;
- the three tiers divide it into three groups of **15**;
- the group width is **odd**, so the margin on either side of the field
  stays symmetric (7 cells each);
- the marker of tier *i* sits at column `pad + i*w` inside the field
  (`pad = (w-1)/2`);
- the label of a tier is centred inside its own group, so the labels
  follow the markers;
- the first tier is green, the last is red-orange, and the middle one is
  orange.

The clone reproduces the three-tier bar and the three-tier label row
**byte for byte** — pinned by `TestEffortSliderBarMatchesTheReference` and
`TestEffortSliderLabelsMatchTheReference` in
`internal/modelpicker/slider_test.go`.

### 9.2 What the clone changed

The clone listed a configured tiered model as **one row per tier**
(`minimax-m3.1-flash-low`, `minimax-m3.1-flash-medium`, …), which is not
the reference's shape and made the five-tier model a five-row block with
no slider at all. The cycle changed it to the reference's shape:

| file | what it is |
|---|---|
| `internal/modelpicker/modelpicker.go` | `Row` carries `Tiers` and `DefaultTier`; `ConfiguredRows()` returns one row per model; `Row.CatalogueID(cursor)` spells the tier into the id |
| `internal/modelpicker/slider.go` | the N-tier geometry (`EffortGroupWidth`, `EffortColours`, `EffortBarSegmentsN`, `EffortSliderBarN`, `EffortSliderLabelsN`, `RebuildEffortSliderN`) |
| `internal/cli/model_catalogue.go` | `TieredModelEntry` and `ConfiguredModelEntries()` — the tier-aware view of the catalogue |
| `internal/cliapp/slash_panel_model.go` | `currentModelRow`, `modelEffortCursor`, `setModelCursor`; the footer's slider is the row's own |
| `internal/cliapp/tui_keymap_model.go` | every cursor move goes through `setModelCursor` |

Two views of the same catalogue are now kept, because they answer
different questions:

- `cli.ModelCatalogue()` still lists **one entry per tier** — that is the
  shape the id↔label lookup needs
  (`CatalogueEntry("minimax-m3.1-flash-xhigh")` → `MiniMax M3.1 Flash
  (Xhigh)`), and the Web UI's picker consumes it.
- `cli.ConfiguredModelEntries()` lists **one entry per model**, with the
  tiers spelled out — that is what the TUI's `/model` panel consumes.

The slider's position is an index **into the cursor row's own tier list**,
so a five-tier row and a three-tier row never share a number. The seeding
rule: the running model's own tier when the running model *is* that row,
the row's own default otherwise. A row **without** a slider leaves the
position alone, so the reference's behaviour — coming back to the Gemini
row restores its position — is unchanged.

### 9.3 The clone-side text

The reference's slider has three places, so it says neither `xhigh` nor
`max`. The blurbs for those two tiers are the **clone's own** words
(`modelpicker.EffortDescription`), written in the reference's shape (a
speed clause and a strength clause joined by an em dash):

- `xhigh` — *Near-maximum reasoning for very hard problems — slower, barely stronger*
- `max` — *Maximum reasoning depth — slowest, strongest results*

The three tiers the reference does publish carry the reference's own text,
byte for byte. **These two sentences are the clone's, not the
reference's** — the upstream licence is PolyForm Noncommercial 1.0.0, so
no reference text was copied.

The colours for a bar with more than three tiers repeat the middle
colour; the reference has no longer bar to copy.

---

## 硬约束

- **The clone's changelog must print what the reference prints.** The served
  feed's newest section is still 1.4.8, so the clone's `agy changelog` and
  the print-mode `/changelog` output stay on the 1.4.8 feed. The 1.4.9
  notes are **evidence about the release**, recorded verbatim in
  `internal/cli/testdata/changelog_upstream_1.4.9.md`; they are not
  printed as the feed.
- **A user-facing sentence in this cycle is the reference's own string.**
  The clipboard messages, the AppleScript sources, the class codes and the
  image-header error are copied verbatim from the binary. A paraphrase is
  a bug in this cycle.
- **An attachment is a transport concern, not a transcript concern.** The
  image rides as `backend.Message.Images`; the conversation store
  (`internal/convstore`) keeps the trajectory steps, and the attachment
  stays in memory. A resumed conversation therefore carries the images of
  the turn that is running, not of the turns before it.
- **An attachment must survive the trip into PiG's transcript.**
  `internal/agent/pig_messages.go`'s `userContent` is the **only** place
  where `backend.Message.Images` becomes PiG content, and it is where the
  chain was broken: a user turn built as `pi.UserText` silently loses every
  picture. A turn with no picture stays the plain-text arm; a turn with
  pictures becomes a block list (text first, then one `pi.ImageContent`
  per picture, base64-encoded). Any change to that function must keep both
  shapes — the transports read the image blocks and nothing else.
- **The paste key must never start a turn.** A paste attaches or inserts;
  it never submits, and the empty-paste probe must not touch the composer
  buffer (pinned by `TestPasteMsgEmptyProbesTheClipboard`).
- **A panel owns its own paste.** The `/rename` field pastes into its own
  input; the composer's paste path must not reach it.
- **A configured tiered model is ONE row in the picker, not one row per
  tier.** The tiers live on the effort slider, the reference's own shape.
  `cli.ModelCatalogue()` still lists one entry per tier — that is the
  shape the id↔label lookup needs — and `cli.ConfiguredModelEntries()`
  is the tier-aware view the TUI's panel consumes. Both views are kept
  on purpose.
- **The slider's position is an index into the cursor row's OWN tier
  list.** A five-tier row and a three-tier row never share a number, so
  every cursor move re-seeds the slider through `setModelCursor`. A row
  without a slider leaves the position alone — the reference keeps it,
  so coming back to the row restores it.
- **The reference's three-tier slider geometry must stay byte-for-byte.**
  The clone generalises it to N tiers inside the same 45-cell field, but
  the three-tier bar and the three-tier label row are pinned by
  `TestEffortSliderBarMatchesTheReference` and
  `TestEffortSliderLabelsMatchTheReference`.
- **The `xhigh` / `max` blurbs are the clone's own text.** The upstream
  licence is PolyForm Noncommercial 1.0.0, so only the three tiers the
  reference publishes carry the reference's own words; the two the
  reference never names carry the clone's, in the reference's shape.

## 存疑

- **The macOS class order.** The reference's string table carries
  `PNGfJPEGGIFfTIFFsips` as one blob, so the *order* is the blob's order —
  but a Go string table concatenates unrelated constants too. The clone
  tries PNGf, JPEG, GIFf, TIFF in that order; the reference's actual
  preference is 【推测】.
- **The SSH sentence's trigger.** The clone answers with the reference's
  SSH advice when the session has **no** display (`WAYLAND_DISPLAY` and
  `DISPLAY` are both unset) and `SSH_TTY`/`SSH_CONNECTION`/`SSH_CLIENT` say
  the session is remote. The reference may test the SSH variables first.
- **`Clipboard file URL read error for %s: %v`.** The clone uses this
  sentence when a file on the clipboard is not an image; the reference's
  `%v` may carry a different error value.
- **The pasted image's name.** The clone names a pasted image
  `pasted-image-<n>.<ext>`, the name the clone's own `AcceptPaste` pipeline
  already suggested; the reference's name is not in the string table.
- **Item 9's "commands on an empty last line".** The upstream note names
  the behaviour without listing the commands; the clone applies it to the
  commands that act at the cursor (`x`, `X`, `D`, `C`, `r`, `~` and the
  `d`/`c`/`y` operators). The set is 【推测】.
- **Item 11's "no header at all".** The upstream note says such images are
  rejected with an error that says where the real header starts. A payload
  with no header at all has no offset to report, so the clone keeps its
  old answer for it.
- **The `iconUri` half of item 3.** The reference's format templates take
  the field's name, so one implementation of the checks would serve the
  manifest's `logo` and a context provider's `iconUri` — but the probes in
  §4 show the manifest is **not** where an `iconUri` is read, so the clause
  has no landing point in `plugin.json`. Where such an `iconUri` *is* read
  (a context provider's own manifest, outside `plugin.json`) was not
  located, so the clone keeps the manifest behaviour it verified.
- **The implicit probe's gate.** The reference's implicit clipboard probe
  runs behind a global boolean (a `bool` bit behind the global at
  `0x10937f20`) that was **not** decoded — the probe's *timing* and
  *conditions* are therefore 【推测】. What is 【反编译】 is the route
  itself: an empty bracketed paste builds the probe command.
- **`tea.PasteMsg`'s name.** The type descriptor at `0x10929cc70` matches
  `tea.PasteMsg{Content string}` by **shape** (size 16, one string field),
  and the binary's own string table carries `*tea.PasteMsg` — but the
  descriptor's name could not be resolved through its `nameOff`, so the
  match is 【推测】.
- **The iTerm2 route's shape.** `handlePasteMedia` calls `isITerm2` and
  then `startITermUpload`, so the reference can ask the terminal to upload
  the picture itself; what that upload looks like on the wire was not
  recovered.
- **The clone's single-path paste.** The reference splits **several** paths
  out of one paste and reads each of them; the clone recognises one path.
  Which of the two the reference prefers when a paste carries several paths
  is not known.
- **The N-tier slider geometry beyond three tiers is 【推测】.** The
  45-cell field, the odd group width and the marker positions are
  【一手】 for the reference's **three** tiers; generalising them to five
  (group width 9, margins of 4) is the clone's own rule.
- **The colours of a longer bar.** The reference has no bar with more than
  three tiers, so the middle orange repeating across the extra tiers is
  the clone's extrapolation from the two extremes and the one middle.
- **Whether the reference would show a slider for a configured model at
  all.** The reference's own picker has exactly one tiered model, so the
  shape — one row, plus a slider sized for its own tiers — is the clone's
  extrapolation of the Gemini row.

## Limitations

- **No live agy session was available for this cycle.** Every statement
  about the 1.4.9 binary is 【strings】 or 【反编译】, with two exceptions
  that were run against the installed 1.4.9 binary's own stdout: the
  plugin validator's messages (item 3, one crafted fixture per branch) and
  the `iconUri` probes in §4.
- The 1.4.9 **binary's own behaviour** was not probed for the clipboard:
  the reference's routes were recovered from the binary's own function
  names and its disassembly (§7.1), but no image was pasted into a live
  1.4.9 session, so the *order* the classes are tried in, the implicit
  probe's gate and the iTerm2 route are unverified.
- **The `--print` path carries no picture.** The headless mode builds its
  turn with `pigrt.Runtime.RunWith` (a plain prompt string), not
  `RunWithContent`, so an attachment only travels on the interactive path.
  The reference's print mode was not compared.
- **The marketplace could not be reached** from this machine (the DNS
  lookup for `marketplace.antigravity.com` fails), so item 2's section
  taxonomy could not be captured live either.
- The **Windows** clipboard has no reader in the clone
  (`media_other.go` answers with the reference's "no image" sentence). The
  reference's Windows path is `powershell Get-Clipboard -Format Image`,
  which the clone does not implement.
- The **`/diff` indicator** (item 4) and the **onboarding layout** (item 8)
  are left open rather than approximated; both would need a first-hand
  capture of the reference's rendering before the clone may imitate it.
- `gofmt -l` is not clean for `internal/` and was not run over it; the
  files this cycle wrote are formatted. The two files this cycle edited
  that were already non-clean on `HEAD` (`internal/cliapp/tui_view.go`,
  `internal/cliapp/tui_update_keys_edit.go`) were left as they were.
- `internal/cliapp` carries **pre-existing failures** (colour assertions, from
  the palette change in `a06a4a39`; 29 at the time of writing, after the
  theme fix in `ea058aec`). The count is the same before and after this
  cycle's change; none of them is in a file this cycle wrote, except the
  one test this cycle **renamed** (the empty-paste no-op → the empty-paste
  probe).
- **No reference bar with more than three tiers was ever seen.** The
  reference's own catalogue has no such model, so the five-tier rendering
  in section 9 was verified against the clone's own geometry rules, not
  against a capture. The three-tier rendering is pinned against the
  capture.
- The **five-tier model itself** (`minimax-m3.1-flash` with
  `low, medium, high, xhigh, max`) is a model the user configured on this
  machine; the reference would not know it.

---

Reports: the upstream notes are in
[`internal/cli/testdata/changelog_upstream_1.4.9.md`](../../../internal/cli/testdata/changelog_upstream_1.4.9.md);
the plugin validator's fixture list is in this report's §4.
