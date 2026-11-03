# Provenance — the reference's prompt and step-string templates

These files are the Antigravity CLI's own, recovered verbatim from the binary
and used here unmodified.

Two consumers share the bundle: `internal/agyprompt` renders the
`system_prompts/*.tmpl` sections, and `internal/agystep` renders the
`step_strings/*.tmpl` step strings.

## Source

| | |
|---|---|
| Binary | `~/.local/bin/agy` — the Antigravity CLI, agy **1.4.12** |
| Size | 197,589,952 B |
| SHA-256 | `34423fc9b0af3841e7646aa72a2d40ffeab8218b22d1cb99eac7355f9eba31e3` |
| Container | `google3/third_party/jetski/prompt/template_provider/templates/` inside the binary's `//go:embed` file system |

Re-verified at 1.4.12 (2026-11-01): all **18** template files are
**byte-identical** to the 1.4.7 copies recorded below — the templates have
not moved since 1.4.7. The digests below are therefore unchanged, but the
binary they were read from is now 1.4.12, not 1.4.7.

## How they were recovered

The binary's `//go:embed` file system is a flat array of 48-byte records
(name string header, data string header, 16-byte truncated hash) that no
ordinary tool can read. `devutils/embedextract` takes it apart:

```bash
go build -o /tmp/embedextract ./devutils/embedextract
/tmp/embedextract -bin ~/.local/bin/agy -embed-out /tmp/agy-embed
cp -R /tmp/agy-embed/google3/third_party/jetski/prompt/template_provider/templates/* \
      internal/agytemplates/templates/
```

The extractor only accepts a record whose content hash verifies, so a false
positive is not possible; re-running it reproduces this tree byte for byte.
The same tool and the same recipe are documented in
`docs/guide/reverse-engineering.md` and
`out/ide-language-server/embedded-assets.md`.

## Digests (SHA-256)

```
5d37ed29f71d8da88f2eea9a1ebf5db8cbee22be4bde63506f1887f7da0ff0ea  helpers/errors.tmpl
c9d1a48159f15ad0ebbac54129db898fcc8276ade3765e8ca3bd8bc29c560504  step_strings/helpers/default_status_output_string.tmpl
f9ded495eef28b0a90a50b02c2023724f4acb0cdfc9f5511ed63947b594513bc  step_strings/run_command.tmpl
704a1b6454a8b3d988ea62f0ac5aae50e0b5dc11b6e652d81844dea2880379b9  step_strings/send_command_input.tmpl
9a294feb3115fe931cfeafe5b19aa181d061868928ee9e636fafe15885f04009  system_prompts/artifacts.tmpl
3c72250feec1f1ade822a29f28fc1519a67c241b82f3bc8dff25a8c8617294c1  system_prompts/communication_style.tmpl
9c977fd7da47dde426c3f0f09b52ca4e62feea5cd621e99fcaa40c5a6470d7a4  system_prompts/conversation_transcript.tmpl
d4ced5dc63c1faad5adf01169cb1e52fd986d39fa2649f1e8a854b016eaa0ec4  system_prompts/ephemeral_message.tmpl
959cde80246a35b7042b42ddc2cfa55616aa7d60bf9bb8d6d2b3b7b26810ce58  system_prompts/function_call.tmpl
8b71f78b140f7d9707e54007383121cb6d3f3d5f32fdcebeb2092043c731ef9a  system_prompts/guidelines.tmpl
1314701504f2b452dee278583104b7e588ca094e05ae49d7f5af26314f1c1722  system_prompts/helpers/async_termination_rule.tmpl
a28e035ae6e1197db070be8e5a86361273d1a81824404191dc6c0351251b73b1  system_prompts/identity.tmpl
9d707bc641421bf7bfeef7c9b81b40d61a8c6dee3ffc93c049f0c73b04b39ff5  system_prompts/persistent_context.tmpl
6a65b6a2769d01e8928d4b3aa5326438b0109933d073ff1bd4b93b1301c2235b  system_prompts/planning_mode_artifacts.tmpl
8f76c5269140ee4b891f390b54d4d596925e4a390267ebb447a18c40e6de3402  system_prompts/plugins.tmpl
018695e12d99290bc5806555e4294f643384f6e92f912560e2e5fb7546530fac  system_prompts/skills.tmpl
c7a5f3bd2aa115ef09823dfa72d63cac350d408529ae3e1bb7ce1ac1d685b220  system_prompts/slash_commands.tmpl
07623bcaa7fb48a5a9ca99db3fc87b7080b845a280cc61227977bccceae2559c  system_prompts/test_funcmap.tmpl
```

## The IDE's copies differ — use the CLI's

The Antigravity IDE's `language_server` embeds the same directory but not the
same bytes. The IDE's 17 template files have been retired with the rest of the
rebuildable `out/` bulk — they were byte-identical to the CLI's, so the
surviving copy of the templates is this one
(`internal/app/prompt/agytemplates/templates/`); the rest of the IDE's
`embed-fs/` (the builtin skills and the JSON metaschemas) is still there. The
difference the retirement removed is the history below.

**The two binaries have converged.** At 1.4.7 two files differed and the
CLI's were the newer ones:

| File | CLI (1.4.7) | IDE (1.4.7) |
|---|---|---|
| `system_prompts/guidelines.tmpl` | 1,292 B | 688 B |
| `system_prompts/communication_style.tmpl` | 3,388 B | 3,329 B |

As of the IDE's 2.28.2 build (2026-10-28) **all 17 template files are
byte-identical between the two binaries** — re-verified 2026-11-01 by
extracting both and diffing. The clone renders the CLI's copies, which are
now also the IDE's.

The historical reason the CLI's copy is the one the clone has to render: the
CLI's `guidelines.tmpl` carries one bullet the IDE's did not:

```
- Never defeat or bypass an access control, permission check, credential
  requirement, network/proxy restriction, or sandbox/environment boundary …
```

That bullet is in the clone's captured 1.4.6 request body, so the CLI's copy
is the one the clone has to render. Taking the IDE's would have *dropped* a
line the reference sends.

The `communication_style.tmpl` difference was one rewording of the LaTeX
bullet ("file paths" → "file paths … but not URLs"), which the captured
request does not exercise (the LaTeX branch is off).
