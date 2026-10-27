# The per-model reasoning tiers — deliverable

**Cycle:** `cycle-335-model-reasoning-tiers`
**Subject:** the reference has exactly one model whose reasoning level can be
chosen (`gemini-3.8-flash`), and it spells the level into the model's own
catalogue id (`gemini-3.8-flash-high`). The operator's own model
(MiniMax M3.1 Flash) has no such id, so it needs a way to declare the levels
it offers — and the level has to be **settable** from the TUI and from the
Web UI **without touching the frontend**.
**Evidence:** 【一手】 the frontend bundle the clone serves
(`internal/rcfrontend/assets/main.js`) — the picker's one mapper, which
reads **no** tier field; 【一手】 the reference's own catalogue rows
(`internal/cli/subcommand_parity.go`) — one row per tier; 【一手】 the live
request bodies against a local mock upstream (`out/model-reasoning-tiers/wire-probe.txt`);
【一手】 the CLI's own resolution (`out/model-reasoning-tiers/resolution.txt`);
【一手】 the operator's **own** gateway (`https://agw.k0s.io`) — the clone's
request bodies as that gateway saw them
(`out/model-reasoning-tiers/real-gateway-wire.txt`), and the gateway's own
answer to a tier it does not know
(`out/model-reasoning-tiers/upstream-effort-probe.txt`).

---

## 0. Summary

| | before | after |
|---|---|---|
| **the levels a configured model offers** | not expressible — a configured model is one row | `reasoning_efforts` in `providers.json`: the closed set `low, medium, high, xhigh, max` |
| **the catalogue row of a tiered model** | one row: `minimax-m3.1-flash` | one row **per tier**: `minimax-m3.1-flash-xhigh` → `MiniMax M3.1 Flash (Xhigh)` |
| **the level on the wire** | the model's own `reasoning_effort` only, and only in the models package's provider | the tier rides the row's id → the model → the request, in **three** transports, as the OpenAI `reasoning_effort` hint |
| **`--model <name>` without `--effort`** | the base model | the model's **default tier** (its own `reasoning_effort`, else `medium`, else the first tier) |
| **`--effort` on a tiered model** | the reference's "conflicts" error | the tier, spelled onto the id — a tier the model does not offer is refused |
| **the frontend** | — | **unchanged, byte for byte** |

---

## 1. The reference's own mechanism

The reference's tier is not a slider position and not a field: it is **part
of the model's id**.

【一手】 `internal/cli/subcommand_parity.go` — the reference's catalogue:

```
gemini-3.8-flash-high     Gemini 3.8 Flash (High)
gemini-3.8-flash-medium  Gemini 3.8 Flash (Medium)
gemini-3.8-flash-low     Gemini 3.8 Flash (Low)
claude-sonnet-4-6        Claude Sonnet 4.6 (Thinking)
gpt-oss-120b-medium      GPT-OSS 120B (Medium)
```

One base model, one row per level, the level in the **id** and in the
**label**. The vocabulary is closed, and the Web UI's dispatcher validates
against the same five words 【一手】 `internal/remotecontrol/backend.go:208`:

```go
var DefaultEffortValues = []string{"low", "medium", "high", "xhigh", "max"}
```

So there are two facts to copy: **the spelling of a tier** (id suffix +
bracketed label) and **the vocabulary** (five words).

### 1.1 Why the tier cannot ride a field

【一手】 The frontend has exactly one mapper for the picker's options
(`internal/rcfrontend/assets/main.js`, verbatim in
`out/model-config-alignment/frontend-rk-mapper.js`):

```js
var Rk=a=>a.modelOrAlias?{label:a.label,value:…,modelAlias:…,disabled:a.disabled,
  supportedMimeTypes:new Map(Object.entries(a.supportedMimeTypes)),quotaInfo:a.quotaInfo,
  tagTitle:a.tagTitle,tagDescription:a.tagDescription,supportsThoughtCirculation:a.supportsThoughtCirculation,
  modelUrl:a.modelUrl,modelId:a.modelId||void 0}:null;
```

It reads **eleven** fields and **none** of them is a tier. The picker shows
`label` and sends back the row's id (`modelOrAlias.choice.value`). So a
tier that is to be visible in the Web UI has to ride the **id** and the
**label** — exactly as the reference's own rows do. Copying the reference's
spelling is therefore not a convenience: it is the only spelling the
frontend can carry.

That is why **no frontend byte changed** in this cycle.

---

## 2. The design

### 2.1 The configuration

Two keys on a model entry in `~/.config/openagy/providers.json`:

```json
{
  "id": "minimax-m3.1-flash",
  "name": "MiniMax M3.1 Flash",
  "model": "MiniMax-M3.1-Flash",
  "reasoning_efforts": ["low", "medium", "high", "xhigh", "max"],
  "reasoning_effort": "high"
}
```

| key | meaning |
|---|---|
| `reasoning_efforts` | the tiers the model **offers** — the closed set above |
| `reasoning_effort` | the tier it runs at when nothing else says — and, as before, the OpenAI `reasoning_effort` hint on the wire |

The canonical vocabulary lives in the leaf package `internal/models`
(`EffortLevels`, `IsEffortLevel`, `NormalizeEfforts`, `ValidateEfforts`),
and `internal/modelcatalog.ValidEfforts` is now that same list — one
vocabulary, two users.

A tier outside the set is a tier nobody can select: it is **refused when it
is written** (`agy model add --reasoning-efforts`, the `/provider` panel),
never at the picker's screen.

### 2.2 The expansion happens in the catalogue, not in the registry

The registry keeps **one entry per configured model** — `Registry.List()` is
unchanged, and `Registry.GetByName` still answers for the base model. The
expansion is a *catalogue* concern: a catalogue lists rows, and a tiered
model has several.

```go
// internal/models/tiers.go
func TierRows(list []Model) []Model
func EffortTierRows(m Model) []Model
```

Two catalogues are built from the registry, and both expand:

| catalogue | builder | consumer |
|---|---|---|
| the CLI's `--model` catalogue + the TUI's `/model` panel | `cli.ModelCatalogue()` | `modelpicker.ConfiguredRows()` |
| the Web UI's picker | `rchost.hostListModels()` | `remotecontrol.ModelInfo` |

The row is the reference's shape: id `<base>-<tier>`, label
`<Name> (<Tier>)` (`High`, `Xhigh`, `Max`), the row's own `ReasoningEffort`
= the tier, and the **upstream id unchanged** (`MiniMax-M3.1-Flash`) — the
tier is the clone's word, not the endpoint's.

### 2.3 The lookup is tier-aware

The row's id has to name a model when it travels: the picker sends it back,
the transport looks it up. So `Registry.GetByName` learned one thing — a
catalogue id that carries a tier names the base model **at that tier**:

```go
m, ok := reg.GetByName("minimax-m3.1-flash-xhigh")
// m.ExternalModelName == "MiniMax-M3.1-Flash"
// m.ReasoningEffort    == "xhigh"
```

A tier the model does not offer names **no** model (`-ultra` → not found),
and a model that declares no tiers is not reachable through a suffix either
— its id is its own. The tier therefore rides the row into the request
without any caller changing.

### 2.4 The resolution

`internal/modelcatalog/select.go` resolves the `(model, effort)` pair the
way the reference resolves it, with the configured models added:

| the user writes | the catalogue id |
|---|---|
| `--model minimax-m3.1-flash --effort xhigh` | `minimax-m3.1-flash-xhigh` |
| `--model minimax-m3.1-flash` | `minimax-m3.1-flash-high` (the default tier) |
| `--model minimax-m3.1-flash-max` | `minimax-m3.1-flash-max` |
| `--model minimax-m3.1-flash --effort minimal` | refused — `minimal` is not a tier |
| `--model minimax-m3 --effort high` (no tiers) | refused — the reference's own conflict error |

The **default tier** is the model's own `reasoning_effort` when that names a
tier, else `medium` when the model offers it, else the first tier — the
reference's own rule ("`--model <name>` without `--effort` picks the model's
server default", cycle `model-no-effort-default`).

### 2.5 The wire

The tier rides the request as the OpenAI dialect's `reasoning_effort` hint.
**Three** transports carry it:

| transport | where |
|---|---|
| the configured transport | `backend.GatewayConfig.ReasoningEffort` → `gatewayRequestBody` |
| the PiG-native provider (the TUI and `--print` run on this one) | `piagentbridge.ProviderConfig.ReasoningEffort` → `chatRequest` |
| the models package's provider | the model's own `ReasoningEffort` |

The **Anthropic** dialect has no such field, so the hint is dropped there:
the Messages API spells its thinking budget differently.

The PiG-native provider needed the field added — it builds its own request
body (`internal/piagentbridge/gatewayprovider_request.go`) and did not carry
the level at all. The print path additionally resolves the `(model, effort)`
pair before it asks the transport (`printmode.printBackendModel`), so
`--effort` reaches the wire; before this cycle the print path passed the
raw `--model` and no level rode for **any** model.

### 2.6 The TUI

- The `/model` panel lists the tier rows — it lists whatever the catalogue
  holds.
- The `/effort` **slider** is the reference's own geometry: three places.
  Five levels cannot ride it, so the tier is a **row**, and the row's label
  carries it. The per-turn runtime level reads the tier from the model's
  label (`cliapp.currentEffortName` → `models.TierFromLabel`), and the
  `--effort` flag still wins over it.
- The `/provider` panel can **declare** the tiers: the "Reasoning tiers"
  field (comma separated) writes `reasoning_efforts`, and the effort choice
  field now offers `xhigh` and `max` as well.

---

## 3. The evidence

### 3.1 The wire, live

【一手】 A local mock upstream records the request body verbatim
(`out/model-reasoning-tiers/wire-probe.txt`):

```
$ pigasus --model minimax-m3.1-flash --effort xhigh --print='say hi'
{"model": "MiniMax-M3.1-Flash", "reasoning_effort": "xhigh"}
$ pigasus --model minimax-m3.1-flash --print='say hi'          # the bare id = the default tier
{"model": "MiniMax-M3.1-Flash", "reasoning_effort": "high"}
$ pigasus --model minimax-m3.1-flash-max --print='say hi'
{"model": "MiniMax-M3.1-Flash", "reasoning_effort": "max"}
$ pigasus --model minimax-m3 --print='say hi'                  # a model with no tiers
{"model": "MiniMax-M3"}
```

The upstream id is untouched in every case; the tier is the extra field.

### 3.2 The resolution, live

【一手】 `out/model-reasoning-tiers/resolution.txt`:

```
$ pigasus -p=/model --model=minimax-m3.1-flash --effort=xhigh
minimax-m3.1-flash-xhigh	MiniMax M3.1 Flash (Xhigh)

$ pigasus -p=/model --model=minimax-m3.1-flash
minimax-m3.1-flash-high	MiniMax M3.1 Flash (High)

$ pigasus -p=/model --model=minimax-m3 --effort=xhigh
error: invalid model selection (--model "minimax-m3" --effort "xhigh"): --model minimax-m3 conflicts with --effort=xhigh

$ pigasus model show minimax-m3.1-flash
  effort:       high
  tiers:        low, medium, high, xhigh, max
```

### 3.3 The operator's own gateway, live

【一手】 The clone's requests through a logging proxy in front of the real
gateway (`https://agw.k0s.io`), which forwarded them
(`out/model-reasoning-tiers/real-gateway-wire.txt`):

```
$ pigasus --model minimax-m3.1-flash --effort xhigh --print='say hi'   # the gateway answered
{"model": "MiniMax-M3.1-Flash", "reasoning_effort": "xhigh"}
$ pigasus --model minimax-m3.1-flash --print='say hi'                  # the bare id = the default tier
{"model": "MiniMax-M3.1-Flash", "reasoning_effort": "medium"}
$ pigasus --model minimax-m3.1-flash --effort max --print='say hi'
{"model": "MiniMax-M3.1-Flash", "reasoning_effort": "max"}
```

And the gateway **validates** the field — a tier it does not know is
refused outright, and the error names its own allowed set
【一手】 (`out/model-reasoning-tiers/upstream-effort-probe.txt`):

```
invalid params, invalid reasoning_effort: "ultra" (allowed: low, medium, high, xhigh, max)
```

**That allowed set is the reference's five words** — the same vocabulary the
clone's own closed set uses, and the same one the gateway's own peer
(`DefaultEffortValues`) accepts.

### 3.4 The tests

| package | what it pins |
|---|---|
| `internal/models` | the vocabulary, the normalisation, the validation, the expansion, the default tier, the id/label helpers, the registry's tier lookup |
| `internal/modelcatalog` | the resolution of a configured model with tiers; the refusal of a tier the model does not offer; the catalogue the unknown-model error advertises |
| `internal/cli` | the `--reasoning-efforts` flag (both spellings), the refusal of an unknown tier, the catalogue's rows and their order |
| `internal/backend` | the `reasoning_effort` hint on the request; its absence when the model has none |
| `internal/backendselect` | the tier's ride from the catalogue id into the request |
| `internal/rchost` | the Web UI picker's rows: one per tier, the upstream id unchanged |
| `internal/modelpicker` | the picker's rows; the label lookup of a configured tier row |
| `internal/cliapp` | the tier-aware effort word; the `--effort` flag still winning |
| `internal/providerpanel` | the "Reasoning tiers" field; the write; the refusal |

---

## 4. Hard constraints

1. **The frontend does not change.** `internal/rcfrontend/assets/main.js`
   is untouched — the tier rides the id and the label, which is what the
   picker's mapper reads.
2. **The vocabulary is closed.** Only `low`, `medium`, `high`, `xhigh`,
   `max`. A tier outside it is refused **at the write**, not at the picker.
3. **The registry keeps one entry per configured model.** `Registry.List()`
   is unchanged; the expansion is a catalogue-layer concern.
4. **The upstream id carries no tier.** `MiniMax-M3.1-Flash` stays
   `MiniMax-M3.1-Flash`; the tier rides the OpenAI `reasoning_effort` hint.
5. **The Anthropic dialect carries no tier.** That dialect has no such
   field, so the hint is dropped there.
6. **The reference's own rows are unchanged.** The clone's six reference
   models keep their ids, their labels and their default level.
7. **The `/effort` slider keeps its three places.** It is the reference's
   own geometry; the tiers are rows.
8. **The operator's own configuration now carries the tiers.**
   `minimax-m3.1-flash` declares `reasoning_efforts` (the five words) and
   `reasoning_effort: "medium"`; `minimax-m3` is untouched. The write went
   through the clone's own `model add`, so the file's shape is the clone's
   own writer's.

## 5. Open questions

- 【推测】 **How much the tier changes the model's thinking.** The gateway
  **reads** the word — it validates it against exactly `low, medium, high,
  xhigh, max` and refuses anything else with a 400 【一手】 — but the
  model's observable reasoning length and its answers did not separate
  cleanly by tier in small samples
  (`out/model-reasoning-tiers/upstream-tier-effect.txt`,
  `out/model-reasoning-tiers/upstream-tier-correctness.txt`). The word is
  carried faithfully; what it buys at the model is the upstream's business,
  and it was not measured here.
- 【推测】 **`reasoning_effort: "default"`.** `default` is not a tier, so a
  model whose level says `default` has no default tier of its own; the
  bare id then falls back to `medium` (when the model offers it). The word
  `default` is dropped on that path.
- 【推测】 **Whether the reference's own `gemini-3.8-flash` rows should
  become configuration.** They are hard-coded in the clone, as they were
  before this cycle. Moving them into `providers.json` would make the
  clone's own catalogue configurable in the same way — a different cycle.

## 6. Limitations

- **The reference's binary was not consulted.** What is measured is the
  clone's behaviour; the reference side contributes its catalogue rows and
  the frontend's mapper 【一手】.
- **Nothing was clicked in a browser.** The Web UI's picker is verified by
  the rows the clone serves (`hostListModels`) and by the frontend's own
  mapper — not by a screenshot.
- **The request body's shape was measured against a local mock**
  (`wire-probe.txt`). The round trip through the operator's **real** gateway
  measured the two fields that matter (`model`, `reasoning_effort` —
  `real-gateway-wire.txt`); the proxy did not record the whole body.
- **The tier's effect on the model was not pinned down.** The gateway
  validates and forwards the word; the model's own behaviour did not
  separate by tier in the samples taken.
- **The TUI was not driven by hand.** The picker's rows and the tier-aware
  effort word are covered by tests; the panel was not screenshotted.
- **The tier does not reach the Anthropic dialect** — by design, and the
  same as the reference's own `--effort` there.
- **The `xhigh` / `max` words are the clone's vocabulary for the *runtime*
  level** (PiG's thinking level): PiG has its own set of levels, and this
  cycle did not extend it. The wire hint carries the reference's five words
  unchanged.

## 7. The files

### The evidence

| file | what |
|---|---|
| `out/model-reasoning-tiers/README.md` | the zh-CN walk-through |
| `out/model-reasoning-tiers/wire-probe.txt` | 【一手】 the request bodies the mock upstream recorded |
| `out/model-reasoning-tiers/resolution.txt` | 【一手】 the CLI's own resolution and `model show` |
| `out/model-reasoning-tiers/real-gateway-wire.txt` | 【一手】 the clone's request bodies as the operator's own gateway saw them |
| `out/model-reasoning-tiers/upstream-effort-probe.txt` | 【一手】 the gateway's own answer to a tier it does not know — and the allowed set it names |
| `out/model-reasoning-tiers/upstream-tier-effect.txt` | 【一手】 the reasoning tokens per tier, under a tight output cap |
| `out/model-reasoning-tiers/upstream-tier-correctness.txt` | 【一手】 the same trap question per tier |
| `out/model-reasoning-tiers/providers-tiers.json` | the scratch configuration the probes ran against |

### The code

| file | what |
|---|---|
| `internal/models/tiers.go` | **new** — the vocabulary, the normalisation, the validation, the expansion, the default tier, the id/label helpers |
| `internal/models/providers.go` | the configuration's `reasoning_efforts` |
| `internal/models/registry.go` | the registry's carrier; the tier-aware `GetByName` |
| `internal/models/edit.go` | the writer of `reasoning_efforts` |
| `internal/models/render.go` | `model show`'s `tiers:` line |
| `internal/modelcatalog/select.go` | the resolution of a configured model with tiers; `ValidEfforts` = the models package's set |
| `internal/cli/model_catalogue.go` | the catalogue's rows (the expansion point for the CLI and the TUI) |
| `internal/cli/model_actions.go` | the `--reasoning-efforts` flag |
| `internal/cli/provider_cmds.go` | the flag's usage text |
| `internal/rchost/remote_control_backends.go` | the Web UI picker's rows (the expansion point for the Web UI) |
| `internal/backend/gateway.go`, `internal/backend/gateway_request.go` | the configured transport's `reasoning_effort` hint |
| `internal/backendselect/backendselect.go` | the tier's ride from the catalogue id into the transport |
| `internal/piagentbridge/gatewayprovider.go`, `…_request.go`, `native_provider.go` | the PiG-native provider's `reasoning_effort` hint |
| `internal/printmode/main_print.go` | the print path's `(model, effort)` resolution |
| `internal/modelpicker/picker_render.go` | the label lookup over the whole catalogue |
| `internal/cliapp/slash_panel_routing.go` | the tier-aware effort word |
| `internal/providerpanel/providerpanel.go`, `actions.go` | the "Reasoning tiers" field and its validation |
