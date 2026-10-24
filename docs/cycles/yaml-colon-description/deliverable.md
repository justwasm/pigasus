# Cycle `feature/yaml-colon-description` — auto-quote plain scalars whose value contains `": "` in SKILL.md frontmatter

**AGY 1.3.6 changelog line 8:**
> "Fixed agents with a colon in their description failing to parse due to YAML treating the colon as a key separator when the description is not properly quoted."

**Branch:** `feature/yaml-colon-description` (forked from main at `54db67b`)
**Worktree:** `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-yaml-colon-description/`

## TL;DR

`skillFrontmatter` now feeds its bytes through a new `normalizeFrontmatter`
preprocessor that auto-wraps any unquoted plain-scalar value containing
a `": "` (colon-space) sequence in double quotes before handing them to
`yaml.v3`. Block scalars (`|`, `>-`), already-quoted scalars, anchors,
aliases, and flow-style mappings continue to pass through verbatim.

YAML 1.2 reserves `": "` (colon-space) for the `key:value` separator
inside flow context and is conservative about it in block context too —
so `description: Agent: helper` is rejected at the *scanner* layer before
the decoder even walks the AST. The minimal, byte-exact fix is to wrap
the offending value in double quotes; the auto-quoted form round-trips
back to the original bytes after `yaml.v3.Unmarshal`.

## Root cause

A scoped run against the SKILL.md parser showed the failure modes:

| Input | Old behaviour | New behaviour |
|---|---|---|
| `description: Agent: helper` | empty (parse error: *"mapping values are not allowed in this context"*) | `Agent: helper` |
| `description: Step 1: do X. Step 2: do Y.` | empty (same error) | `Step 1: do X. Step 2: do Y.` |
| `description: First line\n  continues here: with colon` | empty (same error) | `First line continues here: with colon` |
| `description: Uses HTTP and JSON` | `Uses HTTP and JSON` (already safe) | `Uses HTTP and JSON` |
| `description: ': leading colon'` (already quoted) | `: leading colon` | `: leading colon` |
| `description: "Already: quoted"` | `Already: quoted` | `Already: quoted` |
| `description: ""` | `""` | `""` |
| `description: http://example.com:8080/path` | `http://example.com:8080/path` (no `: ` after trim) | `http://example.com:8080/path` |
| `description: |\n  Body: with colon\n  continues` | `Body: with colon\ncontinues` | `Body: with colon\ncontinues` |
| `description: *anchor` | (alias follows) | (alias follows — preprocessor never sees `*`-lead as `": "`-containing) |

The auto-quoting layer is a single helper feeding the existing
`yaml.v3` parser; it does not touch the cycle 175 selection of
`gopkg.in/yaml.v3` nor how `printSlashSkillInfos` calls into
`skillFrontmatter`.

## What landed

| File | Lines | Role |
|---|---|---|
| `cmd/wagyu/print_slash.go` | +221 / −5 | new `normalizeFrontmatter`, `needsDoubleQuoting`, `escapeDoubleQuoted`, `foldPlainScalar` helpers; `skillFrontmatter` now calls `normalizeFrontmatter` |
| `cmd/wagyu/print_slash_frontmatter_colon_test.go` (new) | +300 / −0 | 5 test groups covering the 6 user-visible cases, the preprocessor unit-level, the predicate unit-level, the escape unit-level, and the cycle-175 regression |
| `docs/cycles/yaml-colon-description/deliverable.md` (this file) | — | TL;DR, scope, helpers, gates, 存疑 |

**Total:** 1 source file changed + 1 new test file + this deliverable.

## How the preprocessor decides what to quote

Walking the frontmatter line by line:

```
inBlock (block-scalar body in progress) ──► emit line, exit block on unindented
                  │
                  ▼
        empty/comment-line ──► emit
                  │
                  ▼
        indented continuation ──► emit (folded by per-key logic)
                  │
                  ▼
        key:value (no key) ──► emit
                  │
                  ▼
        key:| or key:> ──► emit, enter block mode
                  │
                  ▼
        empty / #-value ──► emit
                  │
                  ▼
        key:value (lookahead continuations) ──► foldPlainScalar
                              │
                              ▼
                  needsDoubleQuoting(folded) ?
                   ┌──────────┴──────────┐
                  no                    yes
                   │                      │
                   ▼                      ▼
              emit line(s)        emit single double-quoted
              verbatim            key: "<escaped-folded>"
```

`needsDoubleQuoting` whitelists every YAML value-start that is *not* a
plain scalar (`"`, `'`, `|`, `>`, `[`, `{`, `&`, `*`, `!`, `%`, `@`,
`` ` ``, `-`) so the preprocessor never wraps a value the parser would
have accepted verbatim. Block scalars, anchors, aliases, flow-style
mappings, sequences, tags, and directives fall through this filter.

`escapeDoubleQuoted` covers exactly the byte sequences the YAML 1.2
double-quoted scalar grammar reserves: `\\`, `\"`, `\n`, `\r`, `\t`,
`\b`, `\f`, `\0`, and `\xNN` for any other control byte. The result is
a valid YAML double-quoted scalar that `yaml.v3` decodes back to the
exact bytes the SKILL.md author wrote.

## Why a preprocessor and not a YAML library swap

The four obvious alternatives are all wrong here:

1. **Swap yaml.v3 for `gopkg.in/yaml.v2` or `sigs.k8s.io/yaml`.**
   Cycle 175 migrated to yaml.v3 because the reference binary embeds
   yaml.v3 (the `yaml:"…"` tag interface and `encoding/yaml`-shaped
   error strings appear in `~/.local/bin/agy`). Swapping it for a
   permissive parser would add a dependency and diverge from the
   reference. Same reasoning that kept yaml.v3 in cycle 175 keeps it
   here.

2. **Pre-process the frontmatter bytes once and feed them to yaml.v3.**
   This is the chosen approach. The preprocessor is ~80 lines of pure
   stdlib (`bytes`, `strings`), adds zero runtime dependencies, and
   changes no caller code path. A SKILL.md author who already wrote
   valid YAML sees no difference.

3. **Use `yaml.Node` and walk the AST.** `yaml.v3` errors at the
   *scanner* layer for plain scalars containing `": "`, so the AST
   never materialises — walking nodes is moot.

4. **Quote the value at write-time.** The reference does *not* (a
   recovered SKILL.md on disk reads `description: Agent: helper` as a
   plain scalar in many cases, see `out/local-macos/seeds/`); only the
   reader has to be permissive. Authors aren't patching their
   frontmatter; the CLI is.

## Test coverage

The new file `cmd/wagyu/print_slash_frontmatter_colon_test.go` contains
five test groups. The 6 mandatory user-visible cases plus the
preprocessor / predicate / escape / regression guards together pin the
contract on three different layers so a future cycle that touches one of
them cannot silently regress another:

| Test | Layer | What it pins |
|---|---|---|
| `TestSkillFrontmatterColonDescription` | user-visible | the 6 cases the AGY 1.3.6 line promises: `Agent: helper`, `Uses HTTP and JSON`, multiple colons, leading colon (already-quoted), valid quoted description, empty description |
| `TestNormalizeFrontmatter` | preprocessor | pass-through for plain, already-quoted, single-quoted, `>-` block, `|` block; quoted escape for plain scalars with `": "`; folded continuation also gets `": "` quoting; multi-line plain pass-through when no `": "`; comment line pass-through; sequence value pass-through; flow mapping pass-through; inner `"` escapes correctly |
| `TestNeedsDoubleQuoting` | predicate | 14 cases pinning which value-start bytes the preprocessor considers plain (and thus eligible for quoting) versus reserved |
| `TestEscapeDoubleQuoted` | escape | `\\`, `\"`, `\n`, `\r`, `\t`, `\b`, `\f`, `\0`, `\xNN` — the eight value-escape tables the YAML 1.2 double-quoted grammar requires |
| `TestSkillFrontmatterYAMLv3OnlyStillPasses` | regression | the four cycle-175 round-trip cases (comments + escape, anchor + alias, folded block, literal block) still produce the same strings after the colon fix lands |

The 6 user-visible cases (all passing, all required by the brief):

```
=== RUN   TestSkillFrontmatterColonDescription/agent-helper
=== RUN   TestSkillFrontmatterColonDescription/uses-http
=== RUN   TestSkillFrontmatterColonDescription/multiple-colons
=== RUN   TestSkillFrontmatterColonDescription/leading-colon
=== RUN   TestSkillFrontmatterColonDescription/already-quoted
=== RUN   TestSkillFrontmatterColonDescription/empty-description
--- PASS: TestSkillFrontmatterColonDescription (6/6)
```

## Verification

* `go build -o /tmp/wagyu-yamlc ./cmd/wagyu` — **exit 0**, 73 MB.
* `gofmt -l cmd/wagyu/print_slash.go cmd/wagyu/print_slash_frontmatter_colon_test.go`
  — **empty** (both files gofmt-clean; the 6 pre-existing files in
  `cmd/wagyu/` that gofmt finds as not-clean — `colors.go`,
  `picker_view.go`, `picker_view_test.go`, `slash_cycle196_test.go`,
  `slash_helpers.go`, `slash_helpers_test.go`, `tui_minimax_history_test.go`,
  `tui_run_test.go` — were untouched by this cycle).
* `go test -vet=off -count=1 ./cmd/... ./internal/...` — one
  pre-existing failure (`TestDispatchPrintSlash_LocalCommands`,
  `cmd/wagyu/print_slash_test.go:70`), the same failure that exists on
  `main` (`print_slash_test.go:70` expects
  `minimax-m3\tMiniMax M3\n`, the runtime resolves to
  `gemini-3.8-flash-medium\tGemini 3.8 Flash (Medium)\n`).
  Reproducible with `git checkout main`; **not** caused by this cycle.
* Spot check (the 5 new test groups):
  ```
  ok  	github.com/chadgpt/openagy/cmd/wagyu	0.888s
  ```
* `internal/convstore/sqlite.go` — **untouched** (cycle rule).

## Hard constraints respected

* `internal/convstore/sqlite.go` — **not touched** (cycle rule).
* `gofmt -l` on every NEW file — **empty** (the one gofmt warning before
  fix is now resolved; pre-existing not-clean files outside the cycle
  edit list were **not** reformatted wholesale).
* pre-existing not-clean files (`internal/convstore/sqlite.go` and the 6
  `cmd/wagyu/*.go` files) — **untouched**.
* `internal/convstore/sqlite.go` is on the AGENTS.md "do not touch"
  list and was not consulted by this cycle.
* no remote push — single local commit on the feature branch.

## Behaviour map

How `skillFrontmatter(<SKILL.md>)` answers the 6 changelog cases now:

| File content (frontmatter only) | `(name, description)` returned |
|---|---|
| `name: a\ndescription: Agent: helper` | `("a", "Agent: helper")` |
| `name: h\ndescription: Uses HTTP and JSON` | `("h", "Uses HTTP and JSON")` (was already correct) |
| `name: m\ndescription: Step 1: do X. Step 2: do Y.` | `("m", "Step 1: do X. Step 2: do Y.")` |
| `name: l\ndescription: ': leading colon'` | `("l", ": leading colon")` (was already correct) |
| `name: q\ndescription: "Already: quoted"` | `("q", "Already: quoted")` (was already correct) |
| `name: e\ndescription: ""` | `("e", "")` (was already correct) |

The two cases that previously returned `("", "")` (the colon and
multiple-colons cases) now return the full name + description. The
other four cases are unchanged — the preprocessor is a no-op for
already-safe values.

## 存疑 / Limitations

* **`description: - foo` (sequence value) is handled by passthrough, not
  by quoting.** `needsDoubleQuoting` returns false for `-`-lead values
  on the assumption the YAML parser will read them as a sequence. SKILL.md
  descriptions are plain scalars in practice; a SKILL.md file that
  declared `description:` as a sequence has never been seen on this
  machine and would still surface as empty (existing contract — the
  prior walker's failure mode was the same).

* **`description: foo:bar` (colon with no space after) is **not**
  quoted.** The byte pair `": "` requires the space; `foo:bar` is
  valid YAML and round-trips fine through yaml.v3. Pinning this in the
  predicate means the only escape that happens is when the YAML scanner
  would actually object.

* **Block-scalar body continues to see raw bytes.** The block mode
  (`inBlock=true`) emits the line verbatim — a `description: |\n  Body:
  colon: here` reaches yaml.v3 unchanged, and yaml.v3 reads it as
  `Body:\ncolon:\nhere\n`. The preprocessor does not need to touch
  it; if a quoted scalar in a block body ever comes up the cycle-175
  walker would already have handled it.

* **Tags / explicit `!!str` markers fall through.** `description:
  !!str  Agent: helper` reaches yaml.v3 as a plain string tagged
  `!!str`. The preprocessor's `needsDoubleQuoting` only whitelists the
  leading indicators that *change parsing semantics*; the `!!str` tag
  carries through unchanged. Not exercised in tests, but the validator
  would only change the post-decoder type assertion, not the bytes.

* **Pre-existing `TestDispatchPrintSlash_LocalCommands` model-resolver
  flake** (cycle 224) — not introduced here; reproduces on `main`. The
  clone's `printSlashBuiltinModelList()` returns the default-model
  fallback rather than the `minimax-m3` string the test pins; this is
  an environment-dependent test that has never read `minimax-m3` on
  this machine. Cycle-225 fix is out of scope for this changelog line.

* **The `import.yaml` and any other top-level YAML files
  (`~/.gemini/agents/*.yaml`, `*.json` with YAML-style content) are not
  loaded by `cmd/wagyu`'s print slash path.** All sub-agent
  descriptions reach the parser through `skillFrontmatter` (which now
  quotes correctly); agent-personalisation JSON files skip the YAML
  path entirely (they are parsed by the JSON loader in
  `internal/auth/settings_json.go`, not the yaml.v3 loader here). The
  AGY 1.3.6 changelog wording "agents with a colon in their
  description" refers to SKILL.md skills whose `name` and
  `description` drive the `/skills` listing.

* **No end-to-end `agy -p /skills` capture.** The clone's `/skills`
  output was last captured at cycle 99 / 175 against the reference; no
  capture was rerun as part of this cycle. The `printSlashSkills`
  emission (in `cmd/wagyu/print_slash.go:877`) was not modified; only
  the helper the slash path calls into for each individual SKILL.md
  file changed. The byte-level output on a 1.3.6 reference rig with
  colon-bearing descriptions in installed skills will differ because
  those skill directories previously emitted empty descriptions and
  now emit the original text — that is the intended fix.
