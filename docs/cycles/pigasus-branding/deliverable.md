# Pigasus branding — the clone presents as `Pigasus CLI 1.4.3`

The clone emulates `agy` 1.4.3 and its **version string stays 1.4.3**. What
changes is the product name shown to the user: every rendered `Antigravity CLI`
now reads `Pigasus CLI`. The reference's own artifacts (captures, changelog
text, server-provided tiers) are untouched, so the parity evidence still
describes the same binary.

Scope was explicitly limited to **interface-visible branding**. Internal
identifiers, config discovery, the system prompt, and every string that comes
from the reference or the server stay as they are.

## Changed surfaces 【一手】

| Surface | File | Before → after |
|---|---|---|
| TUI banner, row 1 | `cmd/wagyu/tui_run.go:100` | `Antigravity CLI 1.4.3` → `Pigasus CLI 1.4.3` |
| Help tab bar brand | `cmd/wagyu/tui_run.go:594` | `Antigravity CLI` → `Pigasus CLI` |
| Help description | `cmd/wagyu/tui_run.go:729` | `Antigravity CLI understands …` → `Pigasus CLI understands …` |
| Credits panel header | `cmd/wagyu/slash_panels.go:965` | `Antigravity CLI  credits` → `Pigasus CLI  credits` |
| Telemetry toggle blurb | `cmd/wagyu/slash_panels.go:1811` | `… Antigravity CLI collects usage data …` → `… Pigasus CLI collects …` |
| Changelog title | `cmd/wagyu/slash_panels.go:2797`, `slash_changelog_wrap.go:63` | `Antigravity CLI Changelog` → `Pigasus CLI Changelog` |
| Changelog tagline | `cmd/wagyu/slash_panels.go:2799`, `slash_changelog_source.go:199` | `… interact with Antigravity agents …` → `… Pigasus agents …` |
| Business-account telemetry text | `cmd/wagyu/business_account.go:47` (`telemetryProductName`) | `Antigravity CLI` → `Pigasus CLI` |
| `/feedback` confirmation | `cmd/wagyu/slash_regression.go:367` | `Your feedback helps improve Antigravity CLI.` → `Pigasus CLI` |
| `--help` usage header | `cmd/wagyu/main.go:265-266` | `Recovered Pigasus CLI …` / `pigasus is a 1:1 parse-parity shell of Google Antigravity CLI v1.3.3.` |
| Parse-rig usage header | `internal/cli/parser.go:300-301` | same rename, `v1.3.4` wording |
| Login prompt heading | `internal/cli/login_prompt.go:26` | `# Sign in to Pigasus CLI` |
| Installer output | `internal/cli/install.go:75,81,94` | `Running Pigasus CLI setup...` / `✅ Pigasus CLI installed successfully …` |
| Onboarding terms checkbox | `internal/onboarding/wizard.go:383` | `Yes, I agree to help improve Pigasus CLI …` |
| Permission scope | `cmd/wagyu/slash_panels.go:856`, `slash_action_panels.go:58` | `Shared with Pigasus` / `Rules that apply to all Pigasus products` |

The usage description still names **Google Antigravity CLI** as the upstream it
reimplements — that is a statement about the reference, not about this build.

## Deliberately unchanged

- **System prompt** (`internal/agyprompt`): `<identity>You are Antigravity …`
  and the rest of the 34,350-byte block stay byte-identical to
  `internal/agyprompt/testdata/capture_1.4.2.txt`. Rebranding the persona was
  out of scope and would invalidate the byte-parity fixture.
- **Model-facing context** (`internal/models/agent_context.go`): still says
  `openagy is a Go-recovered build of the Antigravity CLI`, which is a true
  description of the project.
- **On-disk compatibility**: `auth.AntigravityConfigHome()`, `ANTIGRAVITY_HOME`,
  `~/.antigravity`, plugin paths, OAuth token path, and the `agy` binary name.
  Renaming these would make the clone unable to read the official `agy` state.
- **Server strings**: `Antigravity Starter Quota` is returned by the API; the
  clone renders what it receives.
- **Reference content**: changelog entries such as
  `· Initial release of the Antigravity CLI.` are recovered release notes. They
  are history, not this build's branding, and the golden file
  `internal/cli/testdata/changelog_reference.txt` keeps them.
- **External product references**: `/antigravity-guide` and the
  `google-antigravity-sdk` marketplace plugin describe Google's real product.

## Tests

The 12 failing assertions were all display expectations; each was updated to
the new brand string. Changelog-body assertions
(`slash_panels_test.go:1202`, `slash_changelog_source_test.go:94,156`,
`internal/cli/changelog_data_test.go:129`, `changelog_cycle_235_test.go:179`)
were **not** touched.

`go test ./cmd/... ./internal/...` is green (66 packages).

## 硬约束 / 存疑 / limitations

- 硬约束: the version number stays `1.4.3`; `internal/referencedrift` and
  `cli.ReferenceVersion` are unaffected.
- 硬约束: `TestBuildMatchesCapture` and the changelog golden file remain valid
  because neither surface was renamed.
- 存疑: the fork's name in `--help` ("Recovered Pigasus CLI") is a display
  decision; `Usage of <argv0>:` still follows argv[0] and is unchanged.
- limitations: no live A/B capture of the renamed UI was taken — the changes are
  literal substitutions covered by the existing render tests, not re-derived
  from a reference binary (there is no Pigasus reference binary).
- limitations: comments and docs that mention the reference as "Antigravity CLI"
  were left alone on purpose; they describe evidence, not the product.
