# The prompt is rendered from the reference's own templates — deliverable

**Cycle:** `cycle-297-prompt-from-templates`
**Subject:** `internal/agyprompt` hardcoded the prompt's twelve sections; the
reference renders them from Go `text/template` files it embeds, and those
files are now recovered
**Evidence:** 【一手】 the templates, the builtin skill bundle and the
reference's own install were taken out of **agy 1.4.7**
(`~/.local/bin/agy`, SHA-256
`b7a13e53c9f49921692676ef8edc2e60ceb67064c00bc732d9a364ec3ad75b32`) on
2026-10-25, on this machine; the rendered prompt is compared byte for byte
against the captured 1.4.6 request body

---

## 0. Summary

`internal/agyprompt` used to carry the prompt as fourteen embedded text
files: `sections/identity.txt`, `sections/artifacts.txt` and so on, each the
frozen text of one XML section, plus two hand-written list renderers for
`<subagents>` and `<skills>`.

That was a guess about the reference's shape. It is wrong: **the reference
does not hold the prompt as a string.** It holds seventeen Go
`text/template` sources under
`google3/third_party/jetski/prompt/template_provider/templates/` and renders
the prompt from them at request time. The frozen sections were one
*instantiation* of those templates, with the flags set a particular way.

So the templates are now in the repo and the prompt is rendered from them:

| | before | now |
|---|---|---|
| sections from a template | 0 | **7** (identity, artifacts, conversation_transcript, guidelines, communication_style, slash_commands, skills) |
| sections still frozen | 12 | **5** — the ones the binary carries without a template next to them |
| hardcoded list renderers | 2 | 0 — the templates do the lists |
| builtin skill bundle | absent | **9 skills**, the reference's own |
| rendered prompt vs the 1.4.6 capture | byte-identical | **byte-identical** |

The five that stay frozen are not an oversight — see §4.

**The headline finding of this cycle is a divergence between the CLI and the
IDE**: the two binaries embed the *same directory* of templates but not the
*same bytes*. The CLI's `guidelines.tmpl` carries one bullet the IDE's does
not, and the captured CLI request contains that bullet. Rendering the IDE's
copy would have **dropped a line the reference sends**. The CLI's copies are
the ones committed here.

---

## 1. Where the templates came from

The binary's `//go:embed` file system is a flat array of 48-byte records
(name string header, data string header, 16-byte truncated content hash).
`devutils/embedextract` — committed in cycle 295 — takes it apart and
**accepts a record only when the content's hash verifies**, so a false
positive is not possible.

```bash
go build -o /tmp/embedextract ./devutils/embedextract
/tmp/embedextract -bin ~/.local/bin/agy -embed-out /tmp/agy-embed
cp -R /tmp/agy-embed/google3/third_party/jetski/prompt/template_provider/templates/* \
      internal/agyprompt/templates/
```

The CLI carries **472** embedded records; **17** of them are the prompt
templates. The IDE's `language_server` carries **118** records, 17 of them the
same templates.

`internal/agyprompt/templates/PROVENANCE.md` records the digests; they are
also pinned by `TestRecoveredTemplatesAreUnmodified`, so an accidental edit to
a recovered file is a test failure rather than a silent change in what the
clone sends.

## 2. The data model, reversed from the templates

The templates are the contract; the clone has to answer the field names they
read. The full list of references, recovered mechanically:

| Reference | Kind | Clone |
|---|---|---|
| `.IsAutonomous` | field | `Options.Autonomous` |
| `.CascadeConfig.GetPlannerConfig.GetSupportsLatexRendering` | method | `Options.SupportsLatex` |
| `.CascadeConfig.GetPlannerConfig.GetPlanModel.String` | field | `Options.PlanModel` |
| `.ExperimentEnabled "enable-url-artifacts"` | **method** | `Options.Experiments` |
| `{{ArtifactDirectoryPath}}` | **function** | `Options.ArtifactDirectoryPath` |
| `{{ SystemGeneratedLogsPath }}` | **function** | `Options.TranscriptLogsPath` |
| `{{KnowledgeDirectoryPath}}` | **function** | `Options.KnowledgeDirectoryPath` |
| `{{MetadataFileName}}` | **function** | `Options.MetadataFileName` |
| `.Data` | per template | `Options.SlashCommands`, `skillsData` |

Three things about that table cost real time:

- **A bare `{{Foo}}` is a function call; `{{.Foo}}` is a field.** The
  reference registers the four path names as *functions* in its template
  engine, so `newTemplateSet` does the same. Reading them as struct fields
  would have failed to parse.
- **`.ExperimentEnabled` has to be a method, not a field.** Go's template
  engine can only invoke a *field* of function type without arguments:
  `text/template/exec.go` raises *"has arguments but cannot be invoked as
  function"* for the field form. `artifacts.tmpl` calls it with one.
- **The section framing is `"<tag>\n" + body + "\n</tag>"`.** The templates
  render the body *without* the two newlines. The frozen bodies in
  `sections/` carry them, which is why every one of them starts with a
  newline — that is where the framing went when the sections were frozen.

## 3. What the flags do

The templates branch on values the reference fetches from its server. The
clone has no server, so they are inputs, and the defaults reproduce the
captured request:

| Option | Default | Effect |
|---|---|---|
| `Autonomous` | `false` | identity and communication_style switch to the "the USER is not available" wording |
| `SupportsLatex` | `false` | the LaTeX paragraphs in communication_style and artifacts |
| `PlanModel` | `""` | the "CRITICAL INSTRUCTION 1/2" block for the `MODEL_PLACEHOLDER_*` plan models |
| `Experiments` | empty | the `enable-url-artifacts` block in artifacts |
| `SlashCommands` | the reference's nine | the list in slash_commands |

`TestFlagsReachTheTemplates` walks every one of those branches, because a
template that silently ignores its flag is indistinguishable from one that
lost it.

## 4. The five sections that stay frozen — and why

| Section | Why there is no template |
|---|---|
| `<terminal_sandbox>` | literal prompt text in the reference binary; no `.tmpl` next to it |
| `<messaging>` | same |
| `<subagents>` | same — the head/tail text plus the list is literal, and the list is rendered by `renderSubagents` |
| `<user_rules>` | assembled from the host's rule files, not from a file |
| `<user_information>` | assembled from the host (OS, workspaces, cwd, app data dir, conversation id) |

The literal claim is checkable: the strings are in the binary. `strings -a`
on the CLI finds `<appDataDir>/brain/<conversation-id>/.system_generated/logs/transcript.jsonl`
and the `<terminal_sandbox>` paragraph verbatim, next to the templates.

That is also how the **`<appDataDir>` placeholder** was settled. The capture
carries `<appDataDir>/brain/<conversation-id>` in the artifacts and transcript
sections while `<user_information>` carries the *real* paths. That looked like
a sanitized capture — until the same literals turned up in the binary itself
next to the templates. They are the reference's own text: **the reference
names the artifact directory with a placeholder and gives the real path in
`<user_information>`.** The clone now does the same, and
`TestPlaceholderPathsAreTheReferences` pins it.

## 5. The builtin skills

The CLI embeds two asset trees under `assets/`:

```
assets/external/skills/{agy-customizations,antigravity_guide}/   13 files
assets/shared/skills/{automation,generative_ui,migrate-workflows,
                      permissioned-github,plugin,run-workflow,
                      ui-plugin-navigation}/                        7 files
```

and unpacks **all nine** into `<AppDataDir>/builtin/skills/` — a flat layout:
the external/shared split is an asset-path detail that does not survive the
unpacking.

【一手】 The reference's own install is on this machine, and it has exactly
those nine directories:

```
~/.gemini/antigravity-cli/builtin/
├── .checksum          builtin:3eb968c5db879786311097d37a19b81f7ce97d4e76a572864d84b1b6a2942eda
├── keep.txt
└── skills/            agy-customizations  antigravity_guide  automation
                       generative_ui  migrate-workflows  permissioned-github
                       plugin  run-workflow  ui-plugin-navigation
```

Both `SKILL.md` files the prompt advertises are byte-identical to the
embedded copies, frontmatter included, so the names and descriptions the
prompt prints come out the same.

**Which nine the prompt lists is a different question.** The CLI advertises
**two** of them — `agy-customizations` and `antigravity_guide`. The other
seven sit behind server-side feature flags the CLI does not turn on, so
`agyprompt.BuiltinSkillDirs` is the allowlist of those two. The clone ships
all nine and lists two, exactly like the reference.

The clone unpacks the bundle on startup (`internal/cliapp/run.go`) and **never
overwrites a file that is already there**: the reference and the clone share
one config home, and the reference's own install must win. `Install` returns
the number of files it wrote, so it is a no-op after the first run.

## 6. The CLI and the IDE do not ship the same templates

The IDE's `language_server` embeds the same directory. Two files differ:

| File | CLI (1.4.7) | IDE | Difference |
|---|---|---|---|
| `system_prompts/guidelines.tmpl` | 1,292 B | 688 B | the CLI's has the "Never defeat or bypass an access control…" bullet |
| `system_prompts/communication_style.tmpl` | 3,388 B | 3,329 B | one rewording of the LaTeX bullet |

The first one matters. The bullet is in the clone's **captured 1.4.6 request
body**, so the CLI's copy is the one that has to be rendered. The clone's
frozen `sections/guidelines.txt` already had the bullet — the divergence
between the two binaries explains where it came from, and it is why the
committed templates are the **CLI's**.

## 7. What was verified

| Check | Result |
|---|---|
| `TestBuildMatchesCapture` — the whole prompt vs the captured 1.4.6 request | **byte-identical**, 70,853 B |
| `TestRecoveredTemplatesAreUnmodified` — the 17 digests | match the binary |
| `TestEveryTemplateRenders` — all 17 parse and render | pass |
| `TestFlagsReachTheTemplates` — every branch reachable | pass |
| `TestSlashCommandsAreTheReferences` — the nine commands, in order | pass |
| `TestSkillsSectionRendersTheList` — `- name (path): description` | pass |
| `TestPlaceholderPathsAreTheReferences` | pass |
| `builtinskills`: bundle contents, idempotent install, no overwrite | pass |
| `go build ./cmd/... ./internal/...` | pass |
| `go test ./cmd/... ./internal/...` | pass, except the pre-existing `internal/rchost` `TestServeHandler_BindsAndShutsDown` failure (the other agent's `3e74b3cb`, unrelated) |

## 8. Hard constraints

1. **The seven templates are rendered, not re-implemented.** `sections/*.txt`
   for those seven are gone. A change to the prompt's wording is a change to
   the recovered template — and the digests make that loud.
2. **The reference's placeholders stay placeholders.** `<appDataDir>` and
   `<conversation-id>` in the artifacts and transcript sections are the
   reference's own text; substituting the real path there would diverge from
   the reference on the wire.
3. **The builtin bundle never overwrites.** The clone and the reference share
   `<AppDataDir>/builtin/skills`, and the reference's copy is authoritative.
4. **The CLI's templates, not the IDE's.** The IDE's copy is in
   `out/ide-language-server/embed-fs/` as evidence; it is not what the clone
   renders.

## 9. Open questions / next

- **The `step_strings/` templates are not wired yet.** The same bundle
  carries `step_strings/run_command.tmpl` and
  `step_strings/send_command_input.tmpl`, which are the reference's
  **tool-result** strings. The clone still builds the `run_command` result
  text by hand in `internal/tools/command.go`, and there is a live
  divergence there: the template renders

  ```
  The command exited with code 0.
  Output:
  ```

  while `internal/tools/command.go` renders

  ```
  Command exited with code 0.

  Command output:
  ```

  The clone's **TUI detail view** already uses the template's shape
  (`internal/tuitools/titles.go`, `internal/cliapp/tui_tools_test.go`), so the
  two disagree *inside the clone*. The template settles which one the
  reference sends. Wiring it needs a shim for the cortex step protos the
  template reads (`.Step.GetRunCommand.GetCombinedOutput`, `.GetStatus.String`,
  …) — a separate cycle, and the right next one.
- **`persistent_context.tmpl`, `plugins.tmpl`, `ephemeral_message.tmpl`,
  `function_call.tmpl`** are recovered and parse, but the clone has no
  feature that would emit them. They are kept so the bundle stays whole.
- **`system_prompts/helpers/async_termination_rule.tmpl`** is the background
  task rule; the clone's background-task guidance is its own text.

## 10. Limitations

- The section bodies of the five frozen sections were not re-derived from the
  binary in this cycle; they are the frozen texts of the previous cycle,
  which the 1.4.6 capture still pins.
- The reference's own `<appDataDir>` placeholder appears in the binary's
  *other* prompt fragments too (a bullet form of the transcript path, next to
  the template's sentence form). Which of the two the IDE sends was not
  measured — the CLI's capture answers the CLI's question.
