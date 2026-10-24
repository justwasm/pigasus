# Cycle 101 — print-mode CLI argument semantics

**Target:** `agy` 1.3.6 (`~/.local/bin/agy`, 183,407,440 B,
SHA-256 `827ba22b4aa33557614a57aac2cc13f386e17db6b802febd3932226906ac05ab`).

**Theme:** the changelog-driven feature sweep, starting with the most basic gap:
the headless (`--print`) command line did not *parse* like the reference.

---

## 1. What was wrong

The clone treated `-p` / `--print` / `--prompt` as **boolean** flags with a
positional prompt. The 1.3.6 binary treats them as **string** flags whose *value*
is the prompt. Because Go's `flag` package stops parsing at the first non-flag
argument, every flag placed after the prompt was silently ignored:

```
$ agy -p /clear --disable-slash-commands      # clone: rejected /clear (flag unparsed)
$ agy -p /effort --effort low                 # clone: printed "high"
$ agy -p --agent nosuchagent hi               # clone: ran the model with prompt "hi"
```

`agy hi` (a bare positional prompt) is **not** accepted by the reference either —
but the clone accepted it in print mode.

## 2. Recovered reference behaviour 【一手】

All strings below were captured by running the 1.3.6 binary.

| Invocation | Reference | Exit |
|---|---|---|
| `agy -p` | `flag needs an argument: -p` + usage | 2 |
| `agy -p=""` | `error: Error: empty prompt. Usage: agy --print "your prompt here"` | 1 |
| `agy hi` | `Error: unexpected argument "hi".` + `Prompts are read only from -p/--print, -i/--prompt-interactive, or stdin, so this argument would have been ignored.` | 2 |
| `agy -p hi extra` | `Error: unexpected argument "extra".` + same second line | 2 |
| `agy -p --agent nosuchagent hi` | `Error: -p took "--agent" as its prompt, so the intended prompt was left as an argument and ignored.` + `Attach the prompt to the flag (-p='your prompt') and move --agent elsewhere on the command line.` | 2 |
| `agy -p -c` / `agy -p -v 2` | same "took … as its prompt" error | 2 |
| `agy -p -x` / `agy -p --zzz` / `agy -p --` | **accepted** — `-x` is the prompt | 0 |
| `agy -p /help -i bye` | `error: --print and --prompt-interactive are mutually exclusive` | 2 |
| `agy --input-format bogus -p /help` | `Error: unknown --input-format "bogus" (want text or stream-json)` | 2 |
| `agy -v foo` | `invalid value "foo" for flag -v: strconv.Atoi: parsing "foo": invalid syntax` | 2 |
| `agy -v 2` | accepted (hidden int flag) | — |
| `agy --agent nosuchagent -p /help` | `error: unknown agent "nosuchagent" (no custom agents are available)` | 1 |
| `agy --agent default -p …` | accepted | — |

The flag-shaped-prompt rule is **not** "starts with a dash": the reference rejects
only tokens that name a *registered* flag (`-c`, `-v`, `--effort`, `--agent`,
`--prompt-interactive`), and accepts unregistered dash tokens (`-x`, `--zzz`, `--`).

`-v` is a **hidden int** flag — absent from `--help`, but it needs an argument and
validates via `strconv.Atoi`. Our registration therefore uses a custom
`flag.Value` (Go's own `IntVar` would report `parse error` instead of the
reference's `strconv.Atoi: …`).

## 3. `--model` / `--effort` selection 【一手】

Validated on the **headless path only** — `agy --model Gemini` starts the TUI
(it only fails there for lack of a TTY), while `agy -p /model --model Gemini`
exits 1.

Base slugs and their thinking levels:

| Base slug | levels | default |
|---|---|---|
| `gemini-3.8-flash` | low, medium, high | high |
| `gpt-oss-120b` | medium | medium |

| Invocation | Reference error (rc 1) |
|---|---|
| `--effort bogus` | `invalid --effort "bogus" (valid: low, medium, high, xhigh, max)` |
| `--effort xhigh` | `gemini-3.8-flash has no "xhigh" effort (available: low, medium, high)` |
| `--model gpt-oss-120b --effort high` | `gpt-oss-120b has no "high" effort (available: medium)` |
| `--model gemini-3.8-flash-high --effort low` | `--model gemini-3.8-flash-high conflicts with --effort=low` |
| `--model "Gemini 3.8 Flash (High)" --effort low` | `--effort is not supported for model "Gemini 3.8 Flash (High)"` |
| `--model Gemini` | `model Gemini is not recognized as a known model or custom model in settings` + `Available models:` list |

Every error is wrapped as
`error: invalid model selection (--model "<m>" --effort "<e>"): <reason>`.

Two distinctions the reference makes and the clone now reproduces:

* a **full slug** plus `--effort` → `conflicts with --effort=…`; a **display
  label** (or an unknown name) plus `--effort` → `is not supported for model …`;
* `xhigh` / `max` are *valid effort names* globally, so they fail as
  **unavailable** for a model rather than as unknown names.

A side effect: `modelSlug` now consults the catalogue first, because the
mechanical name transform disagrees with the reference for Claude —
`Claude Sonnet 4.6 (Thinking)` is `claude-sonnet-4-6`, not
`claude-sonnet-4.6-thinking`.

## 4. Files changed

| File | Change |
|---|---|
| `internal/cli/parser_specs.go` | `p`/`print`/`prompt`/`i`/`prompt-interactive` → `string`; new hidden `v` (`int`) |
| `internal/cli/jetski.go` | `intAtoiValue` flag.Value; `firstSetFlag`/`flagStrValue`/`isKnownFlagToken`/`flagDisplayName`; the whole print-mode validation block; `lookForSubcommand` now driven by flag presence |
| `cmd/antigravity/main.go` | `resolvePrintMode`/`printPromptSet`/`printPrompt`/`flagWasSet`/`knownAgent`; `--agent` and `--model`/`--effort` validation on the headless path |
| `cmd/antigravity/model_select.go` | **new** — `resolveModelSelection`, base-model effort table, catalogue helpers |
| `cmd/antigravity/print_slash.go` | `/model` prints the resolved catalogue ID; `modelSlug` catalogue-first; `/usage` message rebuilt from recovered vocabulary |
| `internal/cli/parser_test.go` | flag types + counts |
| `cmd/antigravity/print_slash_test.go` | Claude slug expectation; `/usage` + `/credits` failure text |
| `internal/cli/print_args_test.go` | **new** — 10 print-arg cases + 2 acceptance cases |
| `cmd/antigravity/model_select_test.go` | **new** — 25 selection cases |

## 5. Verification

* `go build ./cmd/antigravity` — clean.
* `go test ./cmd/... ./internal/...` — all packages pass.
* `agy --help` — **byte-identical** to the reference (the hidden `-v` and the
  flag-type change do not alter the custom help renderer).
* `harness136.sh` — **86/87** unchanged (the one diff is the documented
  `/plugin` pre-auth divergence).
* `harness137.sh` (changelog-derived) — **23/25**, up from 15/25.
* Live spot-checks: `-p /clear` rejection and the `--agent` error are
  byte-identical; `-p /clear --disable-slash-commands` now forwards to the model
  instead of tripping the slash rejection.

## 6. Divergences and limitations

* **`/usage` cannot be reproduced.** In the reference it is a live call
  (eligibility check → `GetUserQuota`); with the account quota exhausted the
  reference prints two cached/live lines (`Gemini Models  Weekly Limit Remaining
  0%  …`). Our clone emits the *recovered* failure vocabulary instead:
  `error: /usage failed: retrieving quota summary: failed to retrieve user quota
  summary: no quota summary is available for this account`.
  The wrapper fragments (`/%s failed: %w`, `retrieving quota summary: %w`,
  `failed to retrieve user quota summary: %w`,
  `no quota summary is available for this account`) are 【strings】-verified; the
  **nesting order is 【推测】**. This is the second harness137 diff.
* **`--disable-slash-commands` "diff" is a harness artefact.** Both binaries
  forward `/clear` to the model; the reference fails on quota exhaustion
  (`RESOURCE_EXHAUSTED`) while ours gets a reply from the OpenAI-shaped
  `agw.k0s.io` transport, so rc/stdout cannot match. The behaviour matches; only
  the transport differs.
* **The interactive `Agent Not Found` warning is 【推测】.** Recovered fragments
  are the banner title `Agent Not Found` and the tail `%v. Using the default
  agent instead.`; the exact banner layout could not be captured (no live
  reference TUI access). The headless message *is* 【一手】.
* **`-v`'s meaning is unrecovered.** It is registered and validated for parity;
  its value is otherwise unused.
* **Multi-alias prompts** (`-p hi --print bye`) are not byte-parity-tested: the
  reference's choice when several aliases are set is unrecovered. We prefer
  `print` → `prompt` → `p`.
* **Custom agents are not implemented**, so `knownAgent` only knows the built-in
  names (`""`, `default`, `Default`). The reference's `(available: %s)` variant
  of the error is therefore unreachable here.
* **Not touched this cycle:** the TUI slash-command stubs (cycle 77) — still the
  next most basic gap.
