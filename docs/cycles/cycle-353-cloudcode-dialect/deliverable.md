# Cycle 353 deliverable: the CloudCode dialect, extracted

**Cycle:** `cycle-353-cloudcode-dialect`
**Subject:** the CloudCode wire lived inside the application — the request
envelope in `internal/backend/cloudcode_request.go`, the frames in
`cloudcode_stream.go`, the schema conversion in `cloudcode_schema.go`, and a
second, near-identical copy of the wire types in `internal/geminiwire`. It is
now one **self-contained top-level package**, `geminicode/`, with a
dual-audience spec.
**Evidence:** 【一手】 first-hand against the working tree (the package, its
tests, the byte-level assertions) · 【反编译】 the reference's own
`streamGenerateContentFromCCPA` symbol · 【strings】 the User-Agent build
number · 【推测】 the `cascade_id` / `model_enum` labels

---

## 0. The short answer

| | before | after |
|---|---|---|
| where the dialect lives | `internal/backend` (three files) **and** `internal/geminiwire` (one more) | **one top-level package**, `geminicode/` |
| can it be imported on its own | no — it was inside `internal/` | **yes** — `go list -deps ./geminicode` shows the standard library only |
| how many wire type families | **two** near-identical ones (`cc*` and `geminiwire.*`) | **one** (`geminicode.Part`, `.FunctionCall`, `.Content`, …) |
| the spec | scattered across comments and a 2026-10 cycle report | **`docs/guide/cloudcode-dialect.md`** — the contract *and* the derivation |
| the live wire | — | **unchanged** |

The user's scope decision was **tree-internal, top-level package** (not a
separate module), and the spec's audience was **both**: an implementation
guide for a local-service author, and an internal note on where each field
came from.

## 1. What was moved

| from | to | what |
|---|---|---|
| `internal/geminiwire/geminiwire.go` | `geminicode/wire.go` | the wire types, merged with the transport's `cc*` types |
| `internal/backend/cloudcode_request.go` | `geminicode/request.go` | the envelope builder, the tool declarations, the labels, the ids |
| `internal/backend/cloudcode_schema.go` | `geminicode/schema.go` | the JSON Schema → Gemini `Schema` conversion |
| `internal/backend/cloudcode_stream.go` (the parsing half) | `geminicode/stream.go` | the SSE state machine, the frame parser |
| — | `geminicode/frame.go` | `ParseFrame` — one payload, wrapped or bare |
| — | `geminicode/client.go` | the POST, the token header, the 401 retry, `Stream` |
| — | `geminicode/errors.go` | `HTTPError` |
| `internal/backend/cloudcode_test.go` (the dialect assertions) | `geminicode/client_test.go` | the byte-level ground truth |

What **stayed** in `internal/backend` is the transport **policy**: the retry on
a dropped connection, the idle timeout, the token refresh, and the failure
classification (`backend.HTTPError`, `ClassifyTransportError`). That split is
the point — a dialect library that carries its host's retry policy is not
reusable, and the policy is not part of a wire format.

## 2. The two conflicts, and how they were resolved

The two type families disagreed in three places. Every conflict was resolved
**in favour of the wire**, because the wire is what a peer must reproduce
byte for byte.

| conflict | `internal/geminiwire` | the transport | resolution |
|---|---|---|---|
| `FunctionCall.Args` | `map[string]any` | `json.RawMessage` | **`json.RawMessage`** — the model's own bytes survive the round trip, key order included. The proxy re-encodes them for the provider-neutral shape, which is its own business. |
| `FunctionResponse.Response` | `any` | `map[string]any` | **`any`** — the transport always assigns a `map[string]any`, so the bytes are unchanged, and the proxy's type switch keeps working. |
| `GenerationConfig.Temperature` | `*float64` | `float64` + `omitempty` | **`*float64`** — "unset" and "0" are different requests. The transport sets the pointer only when the value is non-zero, which reproduces the old omission exactly. |

The **member order** was also unified on the transport's order, because that
is the direction the bytes are compared in: the envelope reads
`project, requestId, request, model, userAgent, requestType` and the inner
request reads `contents, systemInstruction, tools, labels, generationConfig,
sessionId` — the same order the reference's capture shows.

## 3. Why one type and not two

One type per concept was the requirement, and the alternative — two families
with a `Wire` prefix — would have left the package with `geminicode.Part` and
`geminicode.WirePart` side by side, differing only in `Args`' type. The merge
is what made the package coherent: one `Part`, one `FunctionCall`, one
`Content`, and one `GenerateContentRequest` that serves as both the envelope
and the inner request (it is self-recursive; the inner members are all
`omitempty`, so the envelope level never emits them).

## 4. The adapter

`internal/backend/cloudcode.go` is now the adapter and the policy:

- `backend.Message` → `geminicode.TurnMessage`, `backend.ToolCall` →
  `geminicode.TurnToolCall`, `backend.ToolDecl` → `geminicode.TurnTool`;
- `geminicode.Chunk` → `backend.StreamChunk`, including the tool calls and
  the usage;
- `geminicode.HTTPError` → `backend.HTTPError`, so the headless error
  reporter and the TUI keep their classification.

Two things deliberately did **not** change:

- **The tool-call arguments stay the model's own bytes** on the wire
  (`json.RawMessage`), and the proxy's *provider-neutral* rendering stays
  re-encoded — the proxy's output is unchanged, byte for byte.
- **The request id is minted once per turn**, so a retry of a dropped
  connection re-sends the same body.

## 5. A latent panic, closed on the way

The old retry loop set `resp = nil` and then dereferenced `resp.Body` on the
next iteration — a nil-pointer panic on the narrow path where *re-opening*
the request fails with the same connection drop that caused the retry. The
rewrite handles the missing response explicitly
(`internal/backend/cloudcode_stream.go`, `readBodyOnce`), so the retry budget
is spent and the failure is reported instead of the process dying.

## 6. The spec

`docs/guide/cloudcode-dialect.md` has two halves:

- **Part A — the contract.** The endpoint, the headers, the request envelope
  and every member of it, the `generationConfig`, the schema conversion, the
  ids, the SSE framing, the response members, the tool round trip (the
  `model`-role result, the `output`/`error` envelope, the `thoughtSignature`
  replay), the labels, the thinking trace, and a checklist for a local
  service.
- **Part B — the derivation.** The evidence table with grades, the package
  layout, what the dialect does *not* own, the field-by-field grades, the
  tests that pin each claim, the known differences from the capture, and the
  limitations.

## 7. Verification 【一手】

```
go build ./cmd/... ./internal/... ./geminicode/     # clean
go vet   ./internal/backend/ ./internal/agyproxy/ ./geminicode/   # clean
go test -count=1 ./cmd/... ./internal/... ./geminicode/          # all green
gofmt -l geminicode/                                             # clean
go list -deps ./geminicode | grep pigasus                        # empty
```

The last line is the load-bearing one: the package depends on nothing in the
tree.

The byte-level assertions moved rather than disappeared — `geminicode`'s nine
tests are the same claims the old `internal/backend` tests made, plus the
schema conversion and the tool-envelope test that used to live in
`internal/backend/tool_error_envelope_test.go`. The `internal/backend` tests
that remain pin the **adapter**.

## 8. Open questions

- **The spec does not cover the other five RPCs** field by field. Part A.10
  names them; a local service that wants to answer `loadCodeAssist` or the
  quota call needs more than that.
- **`cascade_id` and `model_enum`** appear on the reference's Gemini request
  and the clone does not send them. Their semantics are unknown 【推测】;
  deriving them would need a second capture with those labels pinned.
- **The `thoughtSignature` store is in-memory.** A long conversation that
  crosses a restart is unproven — the measured `-c` turn worked without the
  signatures, but the reference keeps them somewhere the clone does not model.

## 9. Limitations

- The live wire is unchanged, but **no live run against the real endpoint was
  performed in this cycle** — the move was validated by the byte-level tests
  and by the existing suite. The next live run is the real check.
- The **`User-Agent` build number** (`cl=1003595037`) and the
  `antigravity/cli/1.4.4` prefix are constants; they must move with the
  reference version, and the spec repeats that in two places.
- The **empty-turn `contents`** member is omitted rather than `null`. No real
  turn is empty; the difference is recorded in Part B.6.
- The **package is not published separately.** It is importable on its own
  (no `internal/` dependency), but it lives in this module — moving it to
  its own repository is a separate decision.

---

Reports: `docs/guide/cloudcode-dialect.md` (the current state),
[`cloudcode-model-switching`](../cloudcode-model-switching/deliverable.md)
(the original capture).
