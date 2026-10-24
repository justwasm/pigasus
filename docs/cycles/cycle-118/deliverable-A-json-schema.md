# Cycle 118 — `--json-schema` validation

**Target:** `agy` 1.3.6 (`~/.local/bin/agy`,
SHA-256 `827ba22b4aa33557614a57aac2cc13f386e17db6b802febd3932226906ac05ab`).

**Theme:** `--json-schema` was parsed and ignored. Changelog 1.3.4:

> Changed `--json-schema` to reject plain text, bare type names such as
> `string`, and missing schema files instead of silently treating them as a
> string schema; these inputs, and any schema whose root is not
> `"type": "object"`, now fail at startup with an error and exit code `1`.

This cycle implements that validation exactly.

---

## 1. The recovered contract 【一手】

**It also refuses the flag outside the structured formats.** That is not in the
changelog and is the first thing the reference checks:

```
$ agy -p=x --json-schema=hello                       # rc 1
Error: --json-schema can only be used when --output-format is 'json' or 'stream-json'
```

Note the capital `Error: ` — unlike the lowercase `error: ` the slash-command
rejections use. The comparison is exact: `--output-format JSON` (uppercase) is
still refused.

**The value is a file if it can be read, otherwise inline JSON.** The
discriminator is a *name-shape* test, not a stat:

| value | outcome |
|---|---|
| `{"type":"object"}` | inline JSON |
| `good.json` (exists) | file |
| `plainjson` (exists, no extension) | **file** |
| `hello` (missing, no separator) | inline JSON → parse error |
| `x.json5` (missing) | inline JSON → parse error |
| `h.json.bak` (missing) | inline JSON → parse error |
| `nope.json` (missing) | `failed to read schema file` |
| `a/b`, `./hello`, `x/`, `/` (missing) | `failed to read schema file` |
| `a\b`, `C:\x.json` (missing) | `failed to read schema file` |
| `/tmp` (a directory) | `failed to read schema file` |

So: read the value as a file; if that succeeds use its bytes; if it fails and the
value contains `/` or `\`, or ends in `.json` ignoring case, report the read
error; otherwise parse the value itself. `x.JSON` and `X.JsOn` count as `.json`;
`x.json5` and `.json/`-style oddities are handled by the two clauses above.

**Messages**, all on stderr with a trailing newline, all exit code 1:

```
Error: invalid --json-schema: schema is not valid JSON: invalid character 'h' looking for beginning of value
Error: invalid --json-schema: schema is not valid JSON: unexpected end of JSON input
Error: invalid --json-schema: failed to read schema file "/nonexistent/s.json": open /nonexistent/s.json: no such file or directory
Error: invalid --json-schema: failed to read schema file "/tmp": read /tmp: is a directory
Error: invalid --json-schema: schema root "type" must be "object", got array
Error: invalid --json-schema: schema root must specify "type": "object"
Error: invalid --json-schema: schema root must be a JSON object with "type": "object"
```

The last three are three distinct checks: the root must be a JSON object at all
(`[]`, `null`, `123`, `"object"`), it must have a `type` key (`{}`,
`{"properties":{}}`), and `type` must be the string `"object"`
(`{"type":"array"}`, `{"type":"Object"}`). The JSON parse errors are Go's
`encoding/json` text verbatim, so the reference uses the same package.

**Unset and whitespace-only are accepted**: `--json-schema=` and
`--json-schema='  '` both proceed to the model.

**Ordering**, recovered by combining flags:

* before `--model`/`--effort` validation —
  `-p=x --output-format json --json-schema=hello --effort=xhigh` reports the
  schema error, not the effort error;
* before the `--agent` check;
* before slash-command expansion (`-p=/help --json-schema=hello` reports the
  format error; `-p=/help --output-format json --json-schema=hello` reports the
  JSON error, never the command table);
* and on the **interactive** path too — `agy --json-schema=hello` with no `-p`
  exits 1 with the format error instead of starting the TUI.

## 2. Implementation

* `cmd/antigravity/json_schema.go` (new) — `validateJSONSchemaFlag` (the format
  gate plus resolution), `resolveJSONSchema` (file-vs-inline, parse, the three
  root checks), `jsonSchemaPathLike`, and the `jsonSchemaFormatError` constant.
* `cmd/antigravity/main.go` — one call in `runRoot`, immediately after
  `printMode` is resolved and before the agent and `--model`/`--effort` checks,
  so the recovered ordering falls out of the existing structure.

`os.ReadFile`'s errors already match the reference byte-for-byte (`open …: no
such file or directory`, `read …: is a directory`), and `%q` on the value
reproduces the quoting (including the doubled backslash in `"a\\b"`).

## 3. Verification

**27 live A/B cases** against the reference — same args, fresh scratch HOME,
comparing stdout, stderr and exit code byte-for-byte. 23 are identical,
including every rejection:

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

The four remaining cases are **valid** schemas where the reference fails with
`RESOURCE_EXHAUSTED` (rc 3, its quota is gone) and the clone answers normally —
a quota difference, not a code difference.

* `go build ./...`, `go vet`, `go test ./cmd/... ./internal/...` — clean;
  `gofmt` clean. Four new tests: `TestJSONSchemaRequiresStructuredFormat`,
  `TestJSONSchemaUnsetIsAccepted`, `TestResolveJSONSchemaMessages` (23 cases),
  `TestJSONSchemaPathLike` (24 cases).
* `harness136.sh` — **84/87**; `harness137.sh` — **23/25**. The extra harness136
  diffs are `124/…` reference timeouts (see cycle 117 §4: the reference is
  degraded and the A/B proves it environmental) plus the two permanent
  documented ones.

## 4. Divergences and limitations

* **The schema is validated but not enforced.** The reference feeds the schema
  into the turn so the model's answer conforms to it. The clone has no such
  plumbing: `StreamRequest` (internal/backend/client.go) has no schema field, so
  a *valid* `--json-schema` currently behaves exactly like no schema. Wiring it
  would mean threading it through `agent.TurnInput` → `StreamRequest` →
  `agwRequestBody` as an OpenAI-style `response_format`, which is 【推测】 — the
  reference's own enforcement is Cortex-side and cannot be observed from here.
  The rejection behaviour, which is what the changelog describes, is complete.
* **`--input-format` is still unimplemented**, and the changelog pairs it with
  `stream-json` in the same area.
* **The `Error: ` / `error: ` prefix split is reproduced per message**, not
  derived from a rule. Two prefixes now coexist in the clone's headless
  rejections; a future cycle that unifies them would break parity.
* **The path-shape rule is inferred from 30 probes**, not read from the binary.
  The two clauses (separator, `.json` suffix ignoring case) fit every
  observation, but a value with a separator in a non-path sense (say, a URL)
  would be treated as a path by the clone — and, on the evidence, by the
  reference too.
* **A schema file that is readable but not a regular file** (a FIFO, a device)
  is untested; `os.ReadFile`'s behaviour there is whatever the reference does
  only if it also uses `os.ReadFile`.
