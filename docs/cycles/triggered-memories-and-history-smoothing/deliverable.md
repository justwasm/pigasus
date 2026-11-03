# Cycle triggered-memories-and-history-smoothing — deliverable

**Cycle:** `triggered-memories-and-history-smoothing`  
**Touches:** `internal/app/prompt/agyprompt/`, `internal/app/tools/filesystem/`, `internal/app/tools/surface/`, `internal/piagentbridge/`  
**Report language:** English  

---

## 0. Summary

In multi-turn interactive sessions, Pigasus was observed launching runaway, sluggish tool-calling loops even on trivial conceptual prompts (such as *"什么是 Go 语言中的 interface？"*), whereas official Antigravity CLI (`agy` 1.4.12) returned direct, concise answers with zero tool calls.

Initial speculation considered imposing an artificial client-side ceiling on tool iterations (`defaultMaxToolIterations = 10`), but decompilation of official `agy` 1.4.12 disproved that hypothesis: the official Cortex engine leaves tool iterations uncapped (`MaxTurns = 0`), relying instead on context cleanliness and model convergence.

Reverse engineering of `agy` 1.4.12 (`google3/third_party/jetski/cortex/...`) uncovered the true architectural mechanism:

1. **Dynamic Triggered Memories (Tool Output Rule Injection)**:
   - The reference does not dump the entire workspace rule set into the system prompt. Oversized rules are demoted (`formatDemotedMemoriesAsPrompt`) to compact file paths.
   - When the model accesses workspace files via file tools (`view_file`, `read_file`, `edit_file`, `create_file`), matching rules are injected dynamically at the end of the tool result with the official 105-byte prefix.
   - This keeps the system prompt focused on general conversational instructions until real file editing work begins.

2. **History Smoothing (Few-Shot Loop Mitigation)**:
   - In long sessions, retaining tens of thousands of characters from past tool results creates extreme in-context demonstration inertia, causing the model's posterior probability of initiating tool calls on subsequent casual prompts to approach 100%.
   - Capping historical tool results (prior to the active `UserMessage`) to 2,000 characters with an ellipsis indicator eliminates this inertia while preserving complete fidelity for the active turn.

---

## 1. Reverse Engineering & Binary Evidence

### 1.1 Triggered Memories Header & Hooking
- **Function**: `google3/third_party/jetski/cortex/handlers/handlers.getTriggeredMemories` (Mach-O address `0x101cdac40`) 【一手】【反编译】
- **Format String**: `0x103577164` (105 bytes, length `0x69`):
  ```
  \n\nThe following text is not part of the file, it is a list of user-defined rules that you MUST follow:\n%s
  ```
- **File Matching**: Uses `doublestar.Match` (`github.com/bmatcuk/doublestar/v4`) to match the inspected file's path against rule triggers and workspace paths.
- **Protobuf Configuration**: `cortex_pb.ViewFileToolConfig.GetShowTriggeredMemories` and `cortex_pb.ReplaceContentToolConfig.GetShowTriggeredMemories` activate this mechanism on file read and replace operations.

### 1.2 User Rules Section Partitioning
- **Function**: `google3/third_party/jetski/cortex/mixins/mixins.(*UserRulesSection).Content` (Mach-O address `0x101c2b990`) 【一手】【反编译】
- **Structure**:
  - `formatMemoriesAsPrompt` (`0x101c2bf80`): Inlines rules within `<RULE[path]>` delimiters when under the customization budget.
  - `formatDemotedMemoriesAsPrompt` (`0x101c2c200`): Emits:
    ```
    The following user-defined rules exceeded the rules token budget and were excluded from inline injection. View the file for further instructions when relevant:
    - <path>
    ```
  - `formatConditionalMemoriesAsPrompt` (`0x101c2c530`): Formats conditional rules reserved for dynamic triggering.

### 1.3 Customization Token Budget
- **Function**: `google3/third_party/jetski/cortex/customizations/customizations.(*Manager).computeTruncatedCustomizations` (Mach-O address `0x10159aa70`) 【一手】【反编译】
- **Default Limit**: `uVar11 = 20000; uVar8 = 20000;` (20,000 tokens ≈ 80,000 characters).
- **Truncation**: Evaluated by `truncateFromBreakdown` (`0x10159b000`) and `truncateItemsToFit` (`0x10159b1f0`).
- Oversized skill bundles append:
  ```
  \n(Additional available skills omitted. Run /skills to view the full list.)
  ```

---

## 2. Implementation Architecture

### 2.1 File Tool Output Hooking
In [`internal/app/tools/filesystem/filesystem.go`](file:///Users/gear/GitHub/openagy/internal/app/tools/filesystem/filesystem.go) and [`filesystem_edit.go`](file:///Users/gear/GitHub/openagy/internal/app/tools/filesystem/filesystem_edit.go):
- Defined `TriggeredMemoriesHook func(workspace, path string) string`.
- Hooked into `ReadFile.Run`, `ViewFile.Run`, `CreateFile.Run`, and `EditFile.Run`.
- When non-empty, the returned triggered memories are appended to the tool output before returning to the model.
- Wired in [`internal/app/tools/surface/catalog.go`](file:///Users/gear/GitHub/openagy/internal/app/tools/surface/catalog.go) to `agyprompt.GetTriggeredMemories`.

### 2.2 Dynamic Memory Resolver
In [`internal/app/prompt/agyprompt/rules.go`](file:///Users/gear/GitHub/openagy/internal/app/prompt/agyprompt/rules.go):
- Added `TriggeredMemoriesHeader`.
- Implemented `GetTriggeredMemories(workspace, targetPath string) string`, matching target paths against demoted workspace rules.

### 2.3 Transcript History Smoothing
In [`internal/piagentbridge/stream.go`](file:///Users/gear/GitHub/openagy/internal/piagentbridge/stream.go) and [`gatewayprovider_request.go`](file:///Users/gear/GitHub/openagy/internal/piagentbridge/gatewayprovider_request.go):
- Defined `maxHistoricalToolResultChars = 2000`.
- Located `lastUserMessageIndex(messages)` in `transcript.Messages()`.
- For any `ToolResultMessage` appearing before the active user turn, truncated content at the last line break under 2,000 characters with `\n\n... [output truncated for conversation history]`.
- Active-turn tool results retain full, untruncated content.

### 2.4 Prompt Template Budget Alignment
In [`internal/app/prompt/agyprompt/template.go`](file:///Users/gear/GitHub/openagy/internal/app/prompt/agyprompt/template.go):
- Sized `maxSkillsTotalBudget = 32000` to accommodate the 63 reference skills (27,573 bytes in `capture_1.4.6.json`) while guarding against memory bloat.
- Preserved byte-exact parity in `TestBuildMatchesCapture` against `testdata/capture_1.4.6.txt` (70,853 bytes).

---

## 3. Verification & Test Matrix

| Test Target | Command | Result |
|---|---|---|
| Prompt byte capture & demoted rules | `go test -v ./internal/app/prompt/agyprompt/...` | PASS (70,853 B byte-for-byte exact) |
| Triggered memories hook in file tools | `go test -v ./internal/app/tools/filesystem/...` | PASS |
| Surface catalog binding | `go test -v ./internal/app/tools/surface/...` | PASS |
| History truncation in stream requests | `go test -v ./internal/piagentbridge/...` | PASS |
| End-to-end interactive sanity check | `/tmp/pigasus -p "什么是 Go 语言中的 interface？"` | PASS (0 tool calls, direct markdown response) |

---

## 4. Limitations & Invariants (硬约束 / 存疑)

1. **MaxTurns Constraint**: Client-side turn loops must remain unconstrained (`MaxTurns = 0`) to mirror official `agy`. Looping behavior must be controlled strictly via prompt structure and historical context hygiene.
2. **Capture Pinning**: `testdata/capture_1.4.6.txt` is an exact frozen capture of `agy 1.4.6`. Any adjustment to `userRulesTotalBudget` or `maxSkillsTotalBudget` must ensure `TestBuildMatchesCapture` continues to match 70,853 bytes without deviation.
3. **Workspace-Scoping**: `GetTriggeredMemories` resolves workspace rules relative to the file path. Paths outside the active workspace do not trigger demoted workspace memories.
