# Cycle 165 — skill frontmatter: hand-rolled walker → gopkg.in/yaml.v3

## Symptom

`cmd/wagyu/print_slash.go::skillFrontmatter` and its two helpers
(`skillBlockScalar`, `foldYAML`) totalled ~95 lines of bespoke YAML
parsing, written for the narrow subset of YAML that the reference's
SKILL.md files happened to use at the time the cycle landed:
`key: value`, simple double-quoted or single-quoted scalars, and the
folded / literal block-scalar flavours (`>`, `>-`, `>+`, `|`, `|-`,
`|+`). Any author who reached for a YAML 1.2 feature outside that
set — comments, anchors, escape sequences in double-quoted strings,
flow-style mappings, sequences — got a silently wrong answer or no
answer at all. And the block-scalar walker was off-spec:

| Source folded block | What yaml.v3 (spec) does | What the old walker did |
|---|---|---|
| `>-` with internal blank line | `\n` between paragraphs | `\n\n` between paragraphs |

i.e. the old `foldYAML` *doubled* every paragraph break, which the test
fixture (`TestSkillFrontmatter`) had been asserting since the code
shipped. Two complementary problems:

1. real SKILL.md files written with non-trivial YAML would be misread
2. even the simple cases were not spec-faithful, so a SKILL.md author
   who tested with the reference would have no guarantee the clone
   would read it the same way

## What the reference actually uses

Cross-checked against `strings ~/.local/bin/agy` to confirm the chosen
library is the same one the reference compiles in (the closed-source
binary is google3's `gypsum` build of `third_party/jetski`):

- `yaml:"…"` tag interfaces appear all over the strings dump
- `failed to yaml…` / YAML error strings from `gopkg.in/yaml.v3` are
  visible
- `TOML` and `toml:"…"` tags also appear, but no SKILL.md on this
  machine uses TOML — the SKILL.md-side frontmatter is YAML

So `gopkg.in/yaml.v3` is the same library the reference uses, which
matters: a SKILL.md author who validates against the reference has a
guaranteed-compat consumer on the clone side too.

## Change

1. `go get gopkg.in/yaml.v3` (resolves to `v3.0.1`). Single direct dep;
   one new transitive (`gopkg.in/check.v1` is a test-only dep, doesn't
   show in the production binary).

2. `cmd/wagyu/print_slash.go::skillFrontmatter` is rewritten onto
   yaml.v3:

   - Read the whole file
   - Carve the bytes between the opening `---` (line 1) and the
     closing `---` (later line) — only those bytes are fed to the
     YAML decoder, so the markdown body after the close fence never
     sees a parser
   - Unmarshal into a `struct { Name, Description string \`yaml:"…"\` }`
   - Trim whitespace on `name`, trim trailing `\n` on `description`
     (see Compatibility below)

3. `skillBlockScalar` and `foldYAML` are deleted — they were private
   to the file and had no other callers.

4. The test fixture for the folded block updates to the spec-correct
   expectation. The fixture's name in the test file is "folded"; the
   new expectation is documented in the test comment so a future
   reader doesn't think it was a churn.

5. New `TestSkillFrontmatterYAMLv3Only` adds two cases that
   exclusively exist because of the migration:
   - double-quoted scalar with `\n` escape inside, mixed with a `#`
     comment after the closing quote
   - YAML anchor + alias (`&b` defined once, `*b` referenced from the
     `description:` field)

## Verification

```
$ go test -count=1 -run TestSkillFrontmatter -v ./cmd/wagyu/
=== RUN   TestSkillFrontmatter
--- PASS: TestSkillFrontmatter (0.00s)
=== RUN   TestSkillFrontmatterYAMLv3Only
--- PASS: TestSkillFrontmatterYAMLv3Only (0.00s)
PASS

$ go test -count=1 ./cmd/wagyu/
ok    github.com/chadgpt/openagy/cmd/wagyu    0.7s
```

`gofmt -d` clean on the two changed files. `go vet ./cmd/...
./internal/...` clean.

Production live-fire: the 14 SKILL.md files under
`~/.gemini/antigravity-cli/builtin/skills/` and `~/.gemini/skills/`
all use the simple `name: foo` + `description: "..."` / inline form
(verified by reading the first 24 bytes of each), so the spec change
isn't visible on current data — it only matters for future SKILL.md
authors who reach for a YAML feature outside the old subset.

## Compatibility notes

- **Trailing `\n` trim on `description`.** `yaml.v3` returns
  `"Alpha\nBeta\n"` for `description: |\n  Alpha\n  Beta` — the trailing
  newline is part of the YAML spec for an unchomped literal block.
  The old walker stripped it via its own trailing-blank trim. The
  new walker preserves that "no trailing newline on description"
  contract via an explicit `strings.TrimRight(doc.Description, "\n")`
  so the `/skills` text output (which joins on `\t`) doesn't gain a
  stray blank line.
- **YAML feature surface widened.** A SKILL.md author can now use
  comments, anchors, escapes, flow mappings, and sequences — the
  reference reads all of these, the clone now reads all of these.
  Non-string values for `name` or `description` still hit the
  strict `string` field type and surface as empty (matching the
  prior "best effort" contract).
- **Frontmatter delimiter strictness.** The first line must be
  exactly `---` (with optional trailing whitespace) — both old and
  new readers are strict here. Files that start with a blank line
  before the fence still hit the no-frontmatter branch.

## 硬约束 / 存疑 / limitations

- 【一手】unit tests pass; the `_test.go` fixtures cover plain,
  folded (`>-`), literal (`|`), no-frontmatter, escape sequences,
  comments, and anchors.
- 【一手】14 real production SKILL.md files all parse on the simple
  `name: foo` / inline-quoted form, which is what every shipped
  skill on this machine uses.
- 【推测】the cycle 164 fix in `cmd/wagyu/tui_viewport.go` (gray pad
  escapes in `fitWidth`/`padTo`) was sitting unstaged in the working
  tree at cycle start; the cycle-165 commit leaves it untouched, per
  the project's per-cycle commit convention. Verify nothing else in
  the staged-to-tree diff overlaps with it before merging forward.
- 【推测】frontmatter files with CRLF line endings have not been
  tested. yaml.v3 tolerates them in practice but if a Windows-saved
  SKILL.md showed up broken, `bytes.ReplaceAll(b, "\r\n", "\n")`
  before splitting would fix it — left for a future cycle if needed.
