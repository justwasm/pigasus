# Tool result texts and the pclntab seed set, re-captured at agy 1.4.7 — deliverable

**Cycle:** `cycle-296-result-texts-and-seeds-1.4.7`
**Subject:** the 【一手】 tool-result transcript and the reverse-engineering
artefacts were still pinned to older releases
**Evidence:** every measurement below was taken against **agy 1.4.7**
(`~/.local/bin/agy`, SHA-256
`b7a13e53c9f49921692676ef8edc2e60ceb67064c00bc732d9a364ec3ad75b32`) on
2026-10-25, on this machine, and is reproducible with
`devutils/refcapture` + `out/local-macos/build_priority.py`

---

## 0. Summary

Two artefacts in this repository were stale against the live binary:

| Artefact | Was | Now |
|---|---|---|
| `internal/tools/testdata/result_texts_1.4.4.txt` | 32 sections, bodies elided with `…` | `result_texts_1.4.7.txt`, 32 sections, **full bodies** |
| `out/local-macos/seeds/{seeds,funcs}.txt` | 161,286 names (agy 1.4.6) | **165,692** names (agy 1.4.7) |
| `out/local-macos/priority-funcs.txt` | 2,290 funcs / 102 packages | **2,313** funcs / 104 packages |
| `out/local-macos/decompile-*.md` | 1.4.6 addresses | **1.4.7** addresses, 105 reports (103 rewritten, 2 new) |

The headline findings:

1. **Every file-tool result text is unchanged.** All the `read_file`,
   `write_file` and `edit_file` sections — plus `manage_task`, `send_message`,
   `define_subagent`, `manage_subagents`, `invoke_subagent` and
   `read_url_content` — came back **byte-identical** to the 1.4.4 capture
   (modulo the per-run conversation id and brain step number). The clone
   already matched; the fixture just could not prove it for the long bodies,
   because the 1.4.4 capture elided or dropped them.
2. **With the full bodies in the fixture, the clone matches the reference byte
   for byte** on all 26 replayed sections — including the 800-line and the
   46 080-byte `read_file` bodies, which the elided fixture could never check.
3. **No reference tool behaviour is proven to have changed.** The three
   sections that differ beyond per-run identifiers are the *capture harness*
   (`search_web`, `generate_image` — the 1.4.4 mock could not fake a grounded
   answer or an image, and said so at the time) and the *capture mode*
   (`ask_question` — TUI at 1.4.4, print at 1.4.7). §2.1 has the evidence,
   including how `generate_image` actually reaches the image service.
4. **The seed set moved by 4,406 names**, the priority list by 23 functions, and
   every address in the 105 committed decompile reports with it. The Ghidra
   project was rebuilt from the new seeds: 165,673 functions created, 0
   failed, 0 unmapped, and 2,313/2,313 priority functions decompiled
   (336,278 LOC).

---

## 1. The capture harness — `devutils/refcapture`

The 1.4.4 capture was taken with a mock CloudCode server that answered every
RPC. **That no longer works at 1.4.7**: before the first turn the reference
runs an eligibility check against `loadCodeAssist`, and a stubbed response
cannot satisfy it — the reference retries with

```
Eligibility check attempt 1 failed transiently
```

and never reaches the model. The new harness therefore **forwards every RPC
except `streamGenerateContent` to the real `https://daily-cloudcode-pa.googleapis.com`**
and only intercepts the agent turn:

1. the mock answers the agent turn with one scripted `functionCall`;
2. the reference executes the tool and replays the whole conversation in the
   follow-up request;
3. the mock reads `functionResponse.response.output` out of that request and
   records it.

**The forwarding is also what makes the 1.4.7 capture worth more than the
1.4.4 one.** The 1.4.4 mock answered *every* RPC, so the reference's own model
calls got fabricated answers — and for `search_web` and `generate_image` the
mock could not fabricate the right one, so the fixture ended up carrying the
reference's *failure* texts where there was no failure in the reference at all.
Forwarding removes that whole class of artefact.

Two details the harness had to get right:

* **Only `requestType == "agent"` consumes a scripted step.** The title
  generator sends `requestType: "checkpoint"` and must get a plain text reply,
  not a function call.
* **functionResponses are de-duplicated by call id.** The reference replays the
  whole conversation on every turn, so the same response arrives again and
  again; without de-duplication the capture would hold N copies of the same
  body.

The reference was driven in **print mode** (`agy -p "go"
--dangerously-skip-permissions`) with the workspace set to `/tmp`. The
`view_file`, `generate_image` and `read_url_content` sections need a **custom
agent** (a workspace `.agents/agents/test.md` plus `--agent test`), because
those three tools are only advertised to one.

Four sessions were needed to reach all 32 sections: the standard agent (19
steps), the custom agent (10), a mixed session (4) and a subagent session (1).

---

## 2. The 1.4.7 capture — what moved and what did not

The fixture is `internal/tools/testdata/result_texts_1.4.7.txt` (214,912 B, 32
sections). The 1.4.4 capture is **kept on disk** as the previous evidence, and
the drift lock holds only the current artefact.

Comparing section by section, after normalising the per-run identifiers
(conversation UUID, brain step number, wall-clock timestamps):

| Change | Sections | Detail |
|---|---|---|
| **none** | **20** | byte-identical to the 1.4.4 capture |
| body elided or dropped | 8 | the 1.4.4 capture did not carry the body (6 abbreviated it with `…`, 2 dropped it outright); the 1.4.7 capture carries it in full. The prose (range header, truncation note, closing sentence) is identical. |
| per-run id only | 1 (`schedule`) | `task-2` → `task-4`; the shape did not move |
| **capture method** | 2 (`search_web`, `generate_image`) | the 1.4.4 mock could not fake a grounded answer or an image; the 1.4.7 harness forwards the RPC to the real endpoint. **Not a proven reference change** — see §2.1. |
| **capture mode** | 1 (`ask_question`) | 1.4.4 was captured in the TUI (a human answered), 1.4.7 in print mode (nobody to answer). **Not a reference change** — see §2.1. |

The 20 unchanged sections are the four small `read_file` cases (whole file, a
one-line range, a zero-byte file, a `StartLine` past EOF), all three
`write_file` cases, all three `edit_file` cases, the three small `view_file`
cases, `manage_task`, `send_message`, `define_subagent`, `manage_subagents`,
`invoke_subagent "general"`, `invoke_subagent "self"` and `read_url_content`.
The last two matched only after the conversation UUID and the brain step
number are normalised — those are per-run identifiers, not behaviour.

### 2.1 What actually moved — and what only *looks* like it moved

**None of the three remaining sections is a proven change in the reference.**
Two of them are the **capture harness** and one is the **capture mode**:

* **`search_web` and `generate_image` — the harness moved.** The 1.4.4 mock
  answered *every* RPC itself, so the reference's own model calls got
  fabricated answers. Cycle 293 recorded this at the time, in its own words:

  > `search_web` and `generate_image` reproduce only the *failure* rows above.
  > The reference's success texts are unmeasured (a grounded GenerateContent
  > answer and an image response would both have to be **faked in the mock**),
  > so both tools carry a `Provider` seam and fall back to the captured error.

  So `Encountered error in tool execution: no candidates returned from
  GenerateContent` and `Encountered error in tool execution: no response in
  generate content result` were **the mock's limitation, recorded as if it were
  the reference's behaviour**. The 1.4.7 harness forwards every RPC except
  `streamGenerateContent` to the real endpoint, so the real answers arrive and
  the reference does what it always did.

  That the reference's *code* did not change is checkable: the string
  `no response in generate content result` is still in the 1.4.7 binary
  (【strings】), and the 1.4.6 and 1.4.7 pclntab seed sets have an identical
  distribution of image-related packages — no new image code appeared.

* **`ask_question` — the capture mode moved.** The 1.4.4 capture was taken in
  the TUI, where a human picked the first option (`A1: alpha`); the 1.4.7
  capture was taken in print mode, where there is nobody to answer
  (`A1: User Skipped`). Both are correct for their mode.

**How `generate_image` actually works** (【一手】, from the raw request body the
harness recorded):

```
POST /v1internal:generateContent            ← NOT streamGenerateContent
{"project":"aicode-consumers",
 "requestId":"image_gen/1792870947759/<uuid>/18",
 "request":{"contents":[{"role":"user","parts":[{"text":"a red cube"}]}],
            "generationConfig":{"candidateCount":1,
                                "imageConfig":{"aspectRatio":"1:1"}}},
 "model":"gemini-3.1-flash-image",
 "userAgent":"antigravity",
 "requestType":"image_gen"}
```

So the reference issues a **separate, non-streaming `generateContent` RPC**
with `requestType: "image_gen"`, a dedicated image model
(`gemini-3.1-flash-image`), and an `imageConfig.aspectRatio` of `1:1`. The
answer carries the image bytes; the reference writes them to
`~/.gemini/antigravity-cli/brain/<conversation-uuid>/<ImageName>_<ms>.jpg` and
returns the path:

```
Generated image is saved at /Users/gear/.gemini/antigravity-cli/brain/571fff91-bca9-4962-89d7-9a6b46494037/cube_1792870959414.jpg.
```

The `ImageName` argument becomes the file's stem and the millisecond timestamp
becomes the suffix. (The synthetic 680 KB `.jpg` was deleted after the
capture; the path string in the fixture is the evidence.)
result is the path of the saved file:

```
Generated image is saved at /Users/gear/.gemini/antigravity-cli/brain/<uuid>/cube_1792870959414.jpg.
```

**`ask_question` in print mode records `A1: User Skipped`.** The 1.4.4 capture
was taken in the TUI, where a human picked the first option and the body read
`A1: alpha`. In print mode there is nobody to answer, and the reference
records `A1: User Skipped`. Both are correct for their mode; the section is
skipped because it needs an interactive user.

### 2.2 A route worth recording: `read_url_content`

The reference **does not fetch the URL itself**. It asks CloudCode:

```
POST /v1internal:rewriteUri           {"originalUri":"https://go.dev/"}
POST /v1internal:fetchFromTrawlerCache {"url":"https://go.dev/","liveFetch":true}
```

and gets a Trawler-cache article back. The real server answers with a
synthetic article — `Title: Live Content`, `OG Description: Fetched live` —
**the same at 1.4.4 and 1.4.7**, so the shape did not move; only the
conversation UUID and the brain step number differ.

### 2.3 The clone's side

With the full bodies in the fixture, `internal/resultparity` compares **every**
section byte for byte. The eight long-file sections moved from `proseBody` to
the default `exactBody` mode, and the clone matched — including the 800-line
`read_file` body and the 46 080-byte `read_file` / `view_file` bodies, which
the elided 1.4.4 fixture could not check at all. `proseBody` survives only for
the 1.4.4 capture, which is still on disk.

`generate_image` moved from *replayed* to *skipped*: the 1.4.7 capture records
a successful generation, and the clone has no image backend wired
(`GenerateImage.Provider` is `nil`), so it still returns the 1.4.4 error text.
The two bodies are not comparable until a provider exists.

The plan is now **26 compared, 6 skipped**, each skip carrying a recorded
reason, and the bijection test still fails if a section is neither.

---

## 3. The seeds pipeline at 1.4.7

Every address moves on every release, so the seed set, the priority list, the
Ghidra project, `dec/` and the committed reports are **coupled by address** and
have to be regenerated together.

```bash
cd rea-go-seeds && mkdir -p <outdir> && go run . ~/.local/bin/agy <outdir>
python3 out/local-macos/build_priority.py out/local-macos/seeds/seeds.txt \
  > out/local-macos/priority-funcs.txt
# Ghidra: import + seed, then DecompilePriority.java, then build_reports.py
```

| Metric | agy 1.4.6 | agy 1.4.7 |
|---|---|---|
| Names recovered from pclntab | 161,286 | **165,692** |
| Seed file SHA-256 | `0844c0f1…` | **`dd73b4f2…`** |
| Priority functions | 2,290 | **2,313** |
| Packages covered | 102 | **104** |
| Ghidra functions created | — | **165,673** (0 failed, 0 unmapped) |
| Priority funcs decompiled | 2,290/2,290 | **2,313/2,313** |
| Decompiled LOC | 335,020 | **336,278** |
| Decompile wall time | 31 s | **26 s** |
| Committed reports | 103 | **105** (103 rewritten, 2 new) |

The two new package reports are
`decompile-google3_third_party_jetski_cli_store_store.reportedError.md` and
`decompile-main.withProfileFlush.md`.

A concrete address move, for the record: `codeassistclient.init` sat at
`0x1019c3bb0` in the 1.4.6 seed set and at **`0x101a7e3f0`** in the 1.4.7 one.

---

## 4. The two traps the pipeline has

Both of these bit during this cycle, and both fail **silently**, so they are
now written down in `docs/guide/reverse-engineering.md`.

**The decouple trap.** `build_reports.py` pairs `dec/<HEX>_*.c` with
`priority-funcs.txt` by the hex prefix. If the priority list was built from a
different seed set than the one the `dec/` files came from, the script prints
`0/N decompiled` and **rewrites all 105 reports with empty tables** — it does
not fail. Read the `N/N decompiled` line before committing anything under
`out/local-macos/`.

**The stale-seed trap.** `rea-go-seeds/seeds.txt` is **gitignored**, it is a
*different* seed set, and it is easy to mistake for the committed one — the
tool defaults its `outdir` to `.`, so a run without an explicit outdir drops a
`seeds.txt` next to the tool. The canonical seed set is
`out/local-macos/seeds/seeds.txt` (tracked, 165,692 names, SHA-256
`dd73b4f2…`). Always build the priority list from *that* file.

A third, milder trap: `dec/` is resume-safe (a function whose output file
already exists and is `> 100` bytes is skipped), so after a seed change the
old files stay in place and look like a good run. Clear `dec/` before
re-running.

---

## 5. The drift lock

`internal/referencedrift/reference.lock.json` now holds the 1.4.7 artefacts:

* `internal/tools/testdata/result_texts_1.4.7.txt` — `captured_at: "1.4.7"`,
  digest `7582b703…`. The `how` field records the harness, the four sessions,
  the forwarding requirement and the capture-method/mode differences.
* `out/local-macos/seeds/seeds.txt` and `.../funcs.txt` — `captured_at:
  "1.4.7"`, digests `dd73b4f2…` / `86659088…`. The `how` field records the
  measured numbers and the address coupling.
* The `lag_note` no longer lists the tool result texts and the seeds/Ghidra
  project as unverified — they were re-captured first-hand.

`go test ./internal/referencedrift/` passes.

---

## 6. Verification

```
go build ./cmd/... ./internal/...     # clean
go test ./cmd/... ./internal/...      # clean
go test ./internal/resultparity/      # ok  (26 compared, 6 skipped)
go test ./internal/referencedrift/    # ok
python3 out/local-macos/build_reports.py
#   summary + 104 per-package reports -> …/out/local-macos
#   2313/2313 decompiled, 336,278 LOC
```

---

## 7. 硬约束 — hard constraints

* **`result_texts_1.4.7.txt` is the only current fixture.** The 1.4.4 file is
  kept as the previous evidence, but the drift lock points at the 1.4.7 one and
  `internal/resultparity` replays the 1.4.7 one. Do not "restore" the old
  path — the 1.4.4 bodies are elided and the clone's byte-for-byte match on the
  long sections cannot be proven against them.
* **The fixture's long bodies must stay full.** They are the only evidence that
  the 46 080-byte truncation boundary and the numbering are byte-identical to
  the reference's. Re-eliding them to keep the file small throws away the
  strongest statement this cycle produced.
* **`generate_image` is skipped, not passing.** The clone's no-provider path
  still returns `Encountered error in tool execution: no response in generate
  content result` — the reference's own error text, which it emits when the
  `generateContent` answer carries no image. Do not "fix" this by copying the
  new success text into the stub: that text is only true when an image was
  really produced, and the path in it is a per-run brain directory. Wire a
  provider instead.
* **The seed set, the priority list, `dec/` and the committed reports move
  together.** A report set built against a different seed set is wrong in every
  address, and `build_reports.py` will not tell you.
* **The canonical seed set is `out/local-macos/seeds/seeds.txt`.** Never
  `rea-go-seeds/seeds.txt`.
* **The capture harness forwards to the real CloudCode endpoint.** It must not
  be pointed at a stub-only server at 1.4.7 — the eligibility check will not
  pass.

## 8. 存疑 — open questions

* **Did the server side change too?** The 1.4.4 → 1.4.7 difference for
  `search_web` and `generate_image` is fully explained by the **harness**
  (the 1.4.4 mock could not fake the answer; the 1.4.7 harness forwards the
  RPC to the real endpoint — cycle 293 said so at the time). Whether the
  *server* also changed between 2026-10-22 and 2026-10-25 was not measured:
  the 1.4.4 capture never reached the real endpoint, so there is no
  server-side baseline to compare against. The binary is unchanged on this
  point (the error string is still in the 1.4.7 binary, and the seed sets
  show no new image code) — what is unverified is the server.
* **Is `A1: User Skipped` a print-mode-only artefact, or does the TUI also
  record it when the user dismisses the question?** The 1.4.7 capture is
  print-mode only; the TUI path was not re-measured. The 1.4.4 TUI capture
  recorded `A1: alpha` for an answered question, so the two are not directly
  comparable.
* **Was the 46 080-byte truncation boundary checked on the boundary itself?**
  The capture reads 46 080 bytes exactly, which suggests it is the limit, but a
  body of 46 081 bytes was not read, so the boundary is inferred, not measured.
* **Are the two new package reports (`store.reportedError`,
  `main.withProfileFlush`) new *functions*, or new *package buckets* for
  functions that existed at 1.4.6 under a different package split?** The
  heuristic in `build_reports.py` splits on the first `(` or the last `.`, so a
  package-bucket change can look like a new function.

## 9. Limitations

* **One reference version was measured**: agy 1.4.7. Nothing here was bisected
  across releases; the 1.4.4 comparison is against the *stored* capture, not
  against a live 1.4.4 binary.
* **`search_web`'s captured body is live web content.** It is a record of what
  the search route said on 2026-10-25 and is not reproducible.
* **The harness drove the reference in print mode**, not through a pty. The
  TUI paths (the `ask_question` answer, the `A1:` formatting) were therefore
  not exercised, and the `ask_question` difference above is a mode difference,
  not a proven behaviour change.
* **The Ghidra project and `dec/` are gitignored local artefacts.** The
  committed evidence is the 105 reports; the project has to be rebuilt from the
  committed seed set to re-derive them.
* **The user's `~/.gemini/antigravity-cli/settings.json` was restored to its
  pre-probe state** after the experiments. The synthetic conversation
  directories the reference created under `~/.gemini/antigravity-cli/brain/`
  were left in place (earlier capture cycles did the same); the one generated
  `.jpg` (680 KB) was deleted.
