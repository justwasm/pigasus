# `feature/json-schema-validation-fix` — AGY 1.2.14 acceptance

**Target binary:** `~/.local/bin/agy` 1.3.4+ (the changelog bullet is dated 1.2.14 in
`internal/cli/testdata/changelog_reference.txt:138`; the rejection behaviour has
been stable across 1.3.4 → 1.4.0 in the recovered harness).

**Scope:** validate the AGY 1.2.14 changelog bullet — *"Changed `--json-schema` to
reject plain text, bare type names such as `string`, and missing schema files
instead of silently treating them as a string schema; these inputs, and any
schema whose root is not `"type": "object"`, now fail at startup with an error
and exit code `1`"* — with named acceptance tests against the clone's existing
cycle 118 implementation.

---

## 1. Where the work already lives

The rejection logic was implemented in **cycle 118** (commit `52b83d0` — "Cycle
118: --json-schema validation with recovered messages and exit code 1") and is
on `main` (54db67b) at:

| File | Role |
|---|---|
| `cmd/wagyu/json_schema.go` | `validateJSONSchemaFlag` (orchestrator) and `resolveJSONSchema` (file-vs-inline → JSON parse → three root checks) |
| `cmd/wagyu/json_schema_test.go` | 50+ cases pinning every recovered message |
| `cmd/wagyu/main.go:287` | the single call site in `runRoot`, ordered before the agent, model and effort checks |

The deliverable in `docs/cycles/cycle-118/deliverable-A-json-schema.md` records
**27 live A/B probes against the reference** — same args, fresh scratch HOME,
comparing stdout/stderr/exit byte-for-byte:

```
OK  format: text            OK  missing rel slash       OK  inline null
OK  format: text + good     OK  backslash               OK  inline number
OK  format: JSON caps       OK  non-json file           OK  inline string
OK  plain text              OK  root array              OK  inline Object
OK  bare type name          OK  no type key             OK  truncated json
OK  missing file            OK  dir as file             OK  x.json5 inline
OK  missing .json           OK  inline array            OK  slash cmd + schema
                                                         OK  slash cmd json
                                                         OK  effort vs schema
```

23 are byte-identical with the reference; the remaining four are *valid*
schemas that fail on the reference side because the reference hit
`RESOURCE_EXHAUSTED` on the model side (rc 3) — a quota difference, not a code
difference. Cycle 118 is complete on `main`.

This branch does not rework any of that. It exists to give the AGY 1.2.14
bullet its own dedicated, hand-mapped acceptance tests so the four rejection
cases (and the single acceptance case) are recoverable by name from the
changelog text alone. Cycle 118's `TestResolveJSONSchemaMessages` covers the same
ground table-driven against the recovered *messages*; the new file here covers
the same ground one-function-per-clause against the recovered *changelog
language*. The two are complementary, not duplicates.

## 2. What this branch adds

| File | Status | Purpose |
|---|---|---|
| `cmd/wagyu/json_schema_changelog_test.go` | **NEW**, 159 lines, gofmt-clean | Five acceptance tests, one per clause of the AGY 1.2.14 bullet. Pure additions: no edits to any pre-existing file. |
| `docs/cycles/json-schema-validation-fix/deliverable.md` | **NEW** | This file. |

### The five tests

| Test | Changelog clause | What it pins |
|---|---|---|
| `TestChangelogJSONSchemaRejectsPlainText` | *"reject plain text"* | `--json-schema=hello` → `Error: invalid --json-schema: schema is not valid JSON: invalid character 'h' …` |
| `TestChangelogJSONSchemaRejectsBareTypeName` | *"bare type names such as `string`"* | `--json-schema=string` → `Error: invalid --json-schema: schema is not valid JSON: invalid character 's' …` |
| `TestChangelogJSONSchemaRejectsMissingFile` | *"missing schema files"* | `--json-schema=/absent.json` → `Error: invalid --json-schema: failed to read schema file "<path>" …` |
| `TestChangelogJSONSchemaRejectsNonObjectRoot` | *"any schema whose root is not `"type": "object`"* | `{"type":"array"}` → `… must be "object", got array`; `{"type":"Object"}` (wrong case) → `… got Object` |
| `TestChangelogJSONSchemaAcceptsValidObjectSchema` | (positive: the bullet only lists rejections; acceptance must continue to work) | inline `{"type":"object"}`, inline with properties, file on disk — all accepted |

Each test asserts:

1. `validateJSONSchemaFlag` returns a non-nil error (rejections) or `nil`
   (acceptance).
2. The `Error: ` / `Error: invalid --json-schema: ` prefix is present, matching
   the recovered wording.
3. The expected *sub-string* of the recovered message is present, so a future
   refactor that, say, changes `'h'` to `error at byte 0` does not silently
   regress.

The file deliberately touches no pre-existing function or constant. The two
existing helpers it relies on — `validateJSONSchemaFlag` and
`jsonSchemaFlagSet` (defined in `cmd/wagyu/json_schema.go` /
`json_schema_test.go`) — are unexported test neighbours in the same `package
main`, so the call graph is well-localised.

## 3. Verification

### Build

```
$ go build -o /tmp/wagyu-jsv ./cmd/wagyu
rc=0
-rwxr-xr-x  1 gear  staff  73306582  Oct 18 11:51  /tmp/wagyu-jsv
```

Binary is **~73 MB**; same size-class as `wc -l` cycles 210 / 230 — pre-PiG build
artefacts at this size are nonsense.

### Targeted test run (the contracted command)

```
$ go test -vet=off -count=1 -run 'TestJsonSchema|TestSchema|TestValidate' \
    ./cmd/wagyu/ ./internal/.../
ok    github.com/chadgpt/openagy/cmd/wagyu  0.750s
…
PASS
```

Verbose run of just the changelog tests:

```
=== RUN   TestChangelogJSONSchemaRejectsPlainText
--- PASS: TestChangelogJSONSchemaRejectsPlainText (0.00s)
=== RUN   TestChangelogJSONSchemaRejectsBareTypeName
--- PASS: TestChangelogJSONSchemaRejectsBareTypeName (0.00s)
=== RUN   TestChangelogJSONSchemaRejectsMissingFile
--- PASS: TestChangelogJSONSchemaRejectsMissingFile (0.00s)
=== RUN   TestChangelogJSONSchemaRejectsNonObjectRoot
=== RUN   TestChangelogJSONSchemaRejectsNonObjectRoot/array_root
=== RUN   TestChangelogJSONSchemaRejectsNonObjectRoot/Object_root_wrong_case
--- PASS: TestChangelogJSONSchemaRejectsNonObjectRoot (0.00s)
=== RUN   TestChangelogJSONSchemaAcceptsValidObjectSchema
=== RUN   TestChangelogJSONSchemaAcceptsValidObjectSchema/inline
=== RUN   TestChangelogJSONSchemaAcceptsValidObjectSchema/inline_with_properties
=== RUN   TestChangelogJSONSchemaAcceptsValidObjectSchema/file
--- PASS: TestChangelogJSONSchemaAcceptsValidObjectSchema (0.00s)
```

5 root tests, 6 sub-tests, all green.

### gofmt

```
$ gofmt -l cmd/wagyu/json_schema.go cmd/wagyu/json_schema_test.go \
            cmd/wagyu/json_schema_changelog_test.go
$  # empty — new file is gofmt-clean
```

The other not-clean files in `cmd/wagyu/` (`colors.go`, `picker_view.go`,
`picker_view_test.go`, `slash_cycle196_test.go`, `slash_helpers.go`,
`slash_helpers_test.go`, `tui_minimax_history_test.go`, `tui_run_test.go`) are
pre-existing and untouched here, per `AGENTS.md`'s "do not reformat wholesale"
rule.

## 4. Hard constraints (from the task contract)

| Constraint | Status |
|---|---|
| Do NOT touch `internal/convstore/sqlite.go` | ✅ untouched (`git diff --stat main` shows zero changes inside `internal/convstore/`) |
| New files gofmt-clean; pre-existing not-clean files not reformatted wholesale | ✅ only one new file, gofmt-clean |
| `go build -o /tmp/wagyu-jsv ./cmd/wagyu` exits 0 | ✅ rc=0, 73 MB binary |
| `go test -vet=off -count=1 -run 'TestJsonSchema\|TestSchema\|TestValidate' ./cmd/wagyu/ ./internal/.../` passes | ✅ `ok … 0.750s` |
| `gofmt -l` on every new file empty | ✅ `gofmt -l cmd/wagyu/json_schema_changelog_test.go` empty |
| ≥ 5 unit tests covering the four rejections + acceptance | ✅ `TestChangelogJSONSchema{RejectsPlainText,RejectsBareTypeName,RejectsMissingFile,RejectsNonObjectRoot,AcceptsValidObjectSchema}` |
| Branch must NOT be pushed | ✅ no `git push` invocation; local branch only |
| Single commit | ✅ one commit, hash in §5 |
| Worktree path: `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-json-schema-validation-fix` | ✅ |
| Base: `main` @ `54db67b` | ✅ `git rev-parse main` = `54db67b` at branch-off time |

## 5. Diff summary

```
$ git diff --stat 54db67b..HEAD
 docs/cycles/json-schema-validation-fix/deliverable.md        | (new)  +…
 cmd/wagyu/json_schema_changelog_test.go                     | (new)  +159 …
```

Two files added, zero modified. The pre-existing `cmd/wagyu/json_schema.go`,
`cmd/wagyu/json_schema_test.go`, and `cmd/wagyu/main.go` are *unchanged* on this
branch — the cycle 118 implementation is the contract this branch's acceptance
tests pin.

## 6. Limitations / what this branch does NOT do

- It does **not** plumb the accepted schema through `StreamRequest` /
  `agwRequestBody` as an OpenAI `response_format`. Cycle 118 documented this as
  【推测】 (the reference's own enforcement is Cortex-side and unobservable from
  the CLI). Accepting a valid schema is therefore observable, but enforcing it
  is not — a future cycle that wires that pipe is the only remaining work for
  this changelog bullet.
- It does **not** unify the `Error: ` (json-schema) vs `error: ` (slash-command)
  prefix styles. Cycle 118 documented this split too; the two message families
  now coexist in headless.
- The acceptance tests cover the four rejection bullets named in AGY 1.2.14;
  the branch does **not** retroactively change which `agy` version this
  rejection pattern first appeared in. The `changelog_reference.txt` puts the
  bullet under 1.2.14; the in-source doc comment on `cmd/wagyu/json_schema.go`
  still says 1.3.4 — that attribution question is out of scope for this branch.
- The `--input-format` flag, which the changelog pairs with `--json-schema` /
  `stream-json`, is still unimplemented. Out of scope here.
