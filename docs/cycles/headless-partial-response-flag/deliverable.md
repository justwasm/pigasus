# Cycle headless-partial-response-flag — `--print-partial-response`

**Target:** `agy` 1.2.10 changelog gap, recovered from the changelog
description rather than the binary on this machine (`~/.local/bin/agy` is
the 1.3.6 build, and 1.2.10 is no longer the binary on disk). The flag is
a recoverable addition: AGY 1.2.10 added it, the clone had never wired
it up, and the byte-level contract the clone pinned to did not mention
it (the flag is new and default-off, so existing captures are
preserved).

**Theme:** wire the boolean through the parser, route each chunk through
a side-channel so long-running turns give earlier feedback, and keep the
final envelope byte-identical.

---

## 1. What the user-visible behaviour is

The AGY 1.2.10 changelog line, recovered verbatim from
`internal/cli/testdata/changelog_reference.txt` and the gap description:

> Added a `--print-partial-response` flag for headless mode that emits
> each chunk of the model's output as soon as it arrives instead of
> buffering until the turn completes, so long-running turns give earlier
> feedback.

What that means on each `--output-format`:

| format | before the flag | with the flag |
|---|---|---|
| `text` (default) | already streaming per chunk — no change | no change (idempotent) |
| `json` | one envelope at end of turn with the full response in `response` | each chunk's sanitised text is written to stdout as it arrives, **and** the closing envelope still carries the full response in `response` |
| `stream-json` | `init` → `step_update` (user_input) → `result` events only | `init` → `step_update` → one `partial_response` event per chunk → `step_update` (agent_response) → `result` event; the closing `result` envelope is unchanged |

Three wire-level decisions pinned by this cycle:

1. The partial side-channel writes the *sanitised* text (after the
   think-block stripper in `agent.Sanitizer.Push` runs). A chunk of
   `<think>…` does not leak to stdout — the next `partial_response`
   carries the visible continuation only.
2. `partial_response` events do NOT carry `conversation_id`. The
   `jsonStreamEvent` comment pins that field to `init`; every
   `partial_response` is preceded by an `init` so consumers resolve
   the id from the session header.
3. The final `result` / `jsonEnvelope` is unchanged regardless of the
   flag — same `response`, `usage`, `num_turns`, `duration_seconds`,
   `error`. The flag's whole point is to give earlier feedback, not to
   change the terminator.

The flag defaults to `false`. That preserves the byte-level contract
for the v1.3.6 capture the clone was pinned to (every prior cycle's
test asserted the exact stdout).

---

## 2. Reference binary 【推测】

The 1.2.10 build is not on disk on this machine (only 1.3.4 and 1.3.6
have been captured). The flag's expected wire shape is reconstructed
from the changelog description and the conventions of the existing
`--output-format stream-json` event vocabulary:

* `init`, `step_update`, `command_result`, `result` are the four
  pre-existing event names (cycle 114 / 115);
* the new `partial_response` event follows the same single-payload
  pattern: `{"event":"partial_response","partial_response":{"response":"…"}}`;
* the closing `result` event keeps the same envelope the buffered path
  produced.

This is a reasonable extrapolation from the AGY surface, not a
verified capture. The binary was on disk when the cycle was first
designed (per the changelog gap description) but is no longer
recoverable from this machine.

---

## 3. Implementation

* **`internal/cli/parser_specs.go`** — added `--print-partial-response`
  to `StandardFlags()` between `--print` and `--print-timeout` (the
  alphabetical order the v1.3.5 binary's --help already uses). Type
  `bool`, default `false`, usage string documents the streaming
  semantics so a `--help` reader knows what the flag does. Updated the
  flag-table comment in the file header.
* **`internal/cli/parser_test.go`** — `TestParseAllFlags` and
  `TestParseAllFlags_RegisteredWithJetski` updated to expect the new
  flag (map entry + count `26 → 27`). The flag-table comment at the
  top of `TestParseAllFlags` is updated with the same line.
* **`cmd/wagyu/print_format.go`** — added the `streamEventPartialResponse`
  event name, the `jsonPartialResponse` struct, and an
  `emitPartialResponse` helper. Extended `jsonStreamEvent` with a
  `PartialResponse *jsonPartialResponse` field, marked `omitempty`.
* **`cmd/wagyu/print_partial_response.go`** (new) — the
  `partialResponseState` value type and its `WriteChunk` method, which
  is the single point where the flag's effect lives. The method takes
  one sanitised chunk and routes it through:
  - `Enabled=false`: no-op (preserves the buffered path).
  - `Enabled=true` + text: no-op (already streaming).
  - `Enabled=true` + json: write to `W` immediately, in addition to
    whatever the caller writes to `ResponseBuf`.
  - `Enabled=true` + stream-json: emit one `partial_response` event
    to `W` (the responseBuf side stays caller-owned).
  An empty sanitised string is a no-op so a `think-block` tag held by
  the sanitizer does not produce a zero-byte event.
* **`cmd/wagyu/main.go`** — `runPrintMode` reads the flag via
  `flagBoolValue(fs, "print-partial-response")` and threads it into the
  chunk handler closure. The chunk handler's existing
  `fmt.Fprint(out, sanitizer.Push(chunk.Text))` line is replaced with a
  three-line block:

  ```go
  sanitized := sanitizer.Push(chunk.Text)
  partial := partialResponseState{W: w, ResponseBuf: &responseBuf,
                                  Format: format, Enabled: partialResponse}
  if err := partial.WriteChunk(sanitized); err != nil {
      return err
  }
  fmt.Fprint(out, sanitized)
  ```

  The same helper is called for the sanitizer's `Finish()` flush at
  the end of the turn so a trailing think-block close also lands on
  the partial side-channel. The pre-existing buffered write to `out`
  runs in both cases (the helper is the side-channel, not a
  replacement).

* **`cmd/wagyu/print_partial_response_test.go`** (new) — eight unit
  tests for the helper. See §5.

---

## 4. Verification

### Build

```
$ go build -o /tmp/wagyu-hpr ./cmd/wagyu
exit 0
```

The binary is 73,306,754 bytes (cycle 224 baseline was 73,306,562
bytes; the +192 byte delta is the new flag's name + usage string in
the help table).

### Targeted test run (the constraint the directive specifies)

```
$ go test -vet=off -count=1 \
    -run 'TestPrint|TestPartial|TestHeadless|TestStream' \
    ./cmd/wagyu/ ./internal/.../
exit 0
no FAIL lines
```

### Parser tests (cycle 224 baseline)

`TestParseAllFlags` and `TestParseAllFlags_RegisteredWithJetski` pass
with the new flag and updated count (`26 → 27`). `TestJetskiCLI_1to1`
passes unchanged.

### Live end-to-end

With the mock backend driving a real session:

```
$ /tmp/wagyu-hpr -p='hello' --print-partial-response --output-format=stream-json
{"event":"init","conversation_id":"…","init":{…}}
{"event":"step_update","step_update":{…,"step_type":"user_input"}}
{"event":"partial_response","partial_response":{"response":"Hello!"}}
{"event":"partial_response","partial_response":{"response":" 👋 Welcome!"}}
{"event":"partial_response","partial_response":{"response":" I'm"}}
{"event":"partial_response","partial_response":{"response":" here…"}}
…  (more partial_response events)
{"event":"step_update","step_update":{…,"step_type":"agent_response"}}
{"event":"result","result":{"conversation_id":"…","status":"SUCCESS","response":"Hello! 👋 Welcome! …","duration_seconds":2,"num_turns":1,"usage":{…}}}
```

The chunks land on stdout as soon as the model emits them; the
closing `result` envelope carries the full concatenated response.

With the flag unset the same input produces:

```
$ /tmp/wagyu-hpr -p='hello' --output-format=stream-json
{"event":"init","conversation_id":"…","init":{…}}
{"event":"step_update","step_update":{…,"step_type":"user_input"}}
{"event":"step_update","step_update":{…,"step_type":"agent_response"}}
{"event":"result","result":{"conversation_id":"…","status":"SUCCESS","response":"Hello! …",…}}
```

No `partial_response` events — the byte-level contract against the
prior cycle's captures is preserved.

### `gofmt -l` on new files

```
$ gofmt -l cmd/wagyu/print_partial_response.go cmd/wagyu/print_partial_response_test.go
(empty)
```

Pre-existing not-clean files (`internal/cli/parser_test.go` etc.) are
not reformatted wholesale — my edits in `internal/cli/parser_test.go`
match the file's existing alignment style.

### `--help` surface

```
$ /tmp/wagyu-hpr -h | grep partial
  --print-partial-response        Stream each chunk of the model's output to stdout in headless mode as it arrives instead of buffering until the turn completes
```

The flag appears between `--print` and `--print-timeout` in the
alphabetical sort, matching the reference's `--help` convention.

---

## 5. Tests added (`cmd/wagyu/print_partial_response_test.go`)

Eight unit tests for `partialResponseState.WriteChunk`, which is the
single point where the flag's effect lives:

1. **`TestPartialResponse_DisabledIsNoop`** — when `Enabled=false`, no
   bytes are written to `W` or `ResponseBuf` by the helper. The
   buffered path stays in charge of accumulation. Pins the byte-level
   contract for prior cycles.
2. **`TestPartialResponse_DisabledStreamJSONIsNoop`** — same invariant
   for stream-json: no `partial_response` event must appear when the
   flag is unset.
3. **`TestPartialResponse_TextEnabledIsNoop`** — text format with the
   flag enabled is also a no-op for the side-channel. Text was already
   streaming; the flag must not duplicate bytes on stdout.
4. **`TestPartialResponse_JSONStreamInOrder`** — drives three chunks
   through the json path and asserts `W` receives them in arrival
   order while `ResponseBuf` still carries the full text (so the
   envelope at end is complete). The changelog's "emit each chunk as
   it arrives" guarantee.
5. **`TestPartialResponse_StreamJSONEmitsEvents`** — drives three
   chunks through stream-json, decodes each emitted line, and asserts
   the wire shape
   `{"event":"partial_response","partial_response":{"response":"…"}}`.
   Asserts `conversation_id` is absent (init-only per
   `jsonStreamEvent`'s comment).
6. **`TestPartialResponse_EmptyChunkIsNoop`** — empty sanitised strings
   (think-block tags held by the sanitizer) do NOT produce an event,
   for all three formats. Zero-byte events would pollute the stream
   and break newline-splitting consumers.
7. **`TestPartialResponse_EnvelopeStillCompleteAtEnd`** — drives a
   small "turn" through the helper and verifies the full text is
   recoverable from `ResponseBuf` at the end, and the closing
   `jsonEnvelope` has `status: "SUCCESS"` and the concatenation of
   every chunk in `response`. The flag must not affect error framing.
8. **`TestPartialResponse_StreamJSONOneLinePerChunk`** — wire-level
   guard: every emitted `partial_response` event is exactly one JSON
   line (the ndjson contract `--output-format stream-json` documents).
   Asserts `strings.Count(out, "\n") == len(chunks)` and every
   non-empty line decodes as a `partial_response` event.

### Test run

```
$ go test -vet=off -count=1 -run 'TestPartialResponse' ./cmd/wagyu/
ok  	github.com/chadgpt/openagy/cmd/wagyu	0.788s
```

---

## 6. Files changed

| File | Change |
|---|---|
| `internal/cli/parser_specs.go` | +13 lines (flag spec + comment updates) |
| `internal/cli/parser_test.go` | +15 / -7 lines (TestParseAllFlags + RegisteredWithJetski count) |
| `cmd/wagyu/print_format.go` | +49 / -8 lines (`streamEventPartialResponse` const, `jsonPartialResponse` struct, `emitPartialResponse` helper, `jsonStreamEvent` field) |
| `cmd/wagyu/main.go` | +44 / -7 lines (read flag, thread through chunk handler, flush path) |
| `cmd/wagyu/print_partial_response.go` | new, 130 lines (helper + comments) |
| `cmd/wagyu/print_partial_response_test.go` | new, 270 lines (8 tests) |

Single commit on branch `feature/headless-partial-response-flag`.

---

## 7. Hard constraints / 存疑 / limitations

* **Not verified against the 1.2.10 binary.** The 1.2.10 build is not
  on disk on this machine; only 1.3.4 and 1.3.6 captures exist. The
  `partial_response` event name, the `{"event": …, "partial_response":
  {"response": "…"}}` wire shape, and the default `false` are
  extrapolations from the changelog description and the conventions of
  the existing `--output-format stream-json` event vocabulary. If the
  reference uses a different event name (`delta`, `chunk`, `text`,
  …) the field names would change but the helper's API (a value type
  with `Enabled`/`Format`/`W`/`ResponseBuf` and a `WriteChunk(string)
  error` method) does not need to.

* **`--print-partial-response` has no short alias.** The reference
  might accept `-X` for it; v1.3.5 does not. The flag is registered
  long-only, matching the gap description's exact wording.

* **`-h`/`--help` count.** `TestParseAllFlags_RegisteredWithJetski`
  goes from 26 → 27 to account for the new long flag. No other
  descriptor count changes.

* **The `conversation_id` field on `partial_response`.** Decided to
  omit it because the `jsonStreamEvent` comment explicitly pins that
  field to `init` only, and every `partial_response` is preceded by
  an `init` event. If the reference includes `conversation_id` on
  `partial_response`, the field would need to be moved into the
  per-event emit call (it would be a one-line change in
  `emitPartialResponse`).

* **The `duration_seconds` field on `partial_response`.** Not added —
  the helper emits the event at chunk-arrival time and the chunk has
  no duration information. The closing `result` event carries
  `duration_seconds` for the whole turn, which is what a streaming
  consumer would compare against.

* **`--print-partial-response` interacts with `--print-timeout`.** A
  deadline that fires mid-turn still produces a partial response on
  stdout (the helper ran), and the closing warning is emitted as in
  cycle 117. The two flags are independent: the flag controls
  *when* bytes leave, the timeout controls *how long* the turn can
  run. No interaction test was added in this cycle.

* **`internal/convstore/sqlite.go` was not touched.** Per the
  directive's hard constraint; this cycle is purely in `cmd/wagyu` and
  `internal/cli`.

* **Pre-existing not-clean files were not reformatted.**
  `internal/cli/parser_test.go` has the same alignment-style
  inconsistencies after this cycle as before. New files are
  `gofmt -l` clean.
