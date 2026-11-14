# TUI Storybook & Clean-Room UI Reconstruction Blueprint

> **Architectural Blueprint & Subsystem Manual.**  
> Documents the design rationale, engineering benefits, and from-scratch clean-room reconstruction methodology for the Antigravity TUI Component Storybook and Design System.

---

## 1. Executive Summary & Rationale

In modern AI agent engineering, terminal user interfaces (TUIs) built on the Elm architecture (`Bubbletea`) have historically suffered from high debugging friction and fragile regression testing. Unlike web applications that enjoy browser DevTools and DOM isolation, terminal interfaces interleave terminal escape sequences, byte/rune width calculations, hardware cursor placement, and complex asynchronous state machines.

The **Antigravity TUI Storybook** (`cmd/storybook`, `internal/ui/storybook`) decouples the presentation layer from the multi-model turn engine and network infrastructure. It provides:
1. **Deterministic Component Sandboxing**: Test and preview any visual component without spawning LLM backends or mocking network event streams.
2. **Terminal Metric Precision**: Isolate East Asian (CJK) double-width rune alignment, hardware cursor coordinates `(X, Y)`, and ANSI SGR escape boundaries.
3. **Multi-Palette Contrast QA**: Instant hot-switching between 4 canonical color schemes (`Solarized Light`, `Dark`, `Tokyo Night`, `Terminal 16-color`).
4. **Automated Visual Regression Harness**: Headless CI test suites verifying that no component panics, leaks unescaped ANSI bytes, or violates column clamps.

---

## 2. Engineering Evaluation & ROI

### 2.1 The Traditional TUI Agent Testing Bottleneck
In a standard agent loop, verifying a UI feature or bugfix (e.g. "verifying `/btw` floating card sits strictly above the top composer rule", "ensuring Braille spinner animation does not drift the hardware cursor during active CJK typing", or "verifying Diff line truncation") requires:
- Initializing the full `actor.Actor` / `agentturn.Engine` runtime;
- Mocking or calling real CloudCode / Gateway LLM endpoints;
- Feeding multi-turn event streams (`functionCall`, `chunk`, `turnDone`);
- Manually interacting with a live terminal to catch 1-pixel visual regressions.

This introduces huge turnaround latency (5–15 minutes per visual iteration) and makes automated CI verification nearly impossible.

### 2.2 The Storybook Solution
By encapsulating visual components behind deterministic `StoryInstance` interfaces (`View() string`, `SetState(int)`, `SetSize(w, h)`, `HandleKey(tea.KeyMsg)`):
- **Zero-Latency Iteration**: Components render instantly in isolation.
- **100% Automated Coverage**: 70 registered stories across 175+ states run in <2 seconds in `go test ./internal/ui/storybook/...`.
- **Clean Architecture Enforcement**: Strictly resides in Tier 5 (`internal/ui/storybook`), forbidden by `make arch` from importing entry hosts (`internal/tui`, `internal/cli`, `internal/printmode`).

---

## 3. Five-Layer Reconstruction Blueprint (From-Scratch Methodology)

If reconstructing the entire Antigravity TUI subsystem clean-room from upstream decompilation artifacts (`jetski/cortex 1.5.5`), this five-layer blueprint defines the optimal reverse-engineering and implementation sequence:

```
┌─────────────────────────────────────────────────────────────────┐
│ Layer 5: Terminal Emulation Matrix (Width / OSC 8 / OSC 7501)   │
├─────────────────────────────────────────────────────────────────┤
│ Layer 4: Mock Event Ticker & Dynamic Streaming Simulation       │
├─────────────────────────────────────────────────────────────────┤
│ Layer 3: Visual Golden Snapshot Pipeline (2D Screen Buffer)     │
├─────────────────────────────────────────────────────────────────┤
│ Layer 2: Atomic Design Hierarchy (Atoms → Molecules → Modals)   │
├─────────────────────────────────────────────────────────────────┤
│ Layer 1: AST-Driven Symbol Mapping (Reverse-Engineering Feed)   │
└─────────────────────────────────────────────────────────────────┘
```

---

### Layer 1: AST-Driven Symbol Mapping (反编译符号直驱管线)

Instead of manually discovering components across files, the reconstruction pipeline starts with an automated symbol extractor over decompiled Go symbol tables (`out/extracted-1.5.5/seeds/funcs.txt`, 178,920 functions):

1. **Symbol Identification Rule**:
   - Extract any type implementing `tea.Model` or defining a `View() string` / `View() tea.View` method (51 official models in 1.5.5: `google3/third_party/jetski/cli/model/model.*`, `onboarding`, `swarm`, `editing`).
   - Extract any function with signature `Render*(...) string` or `*Row(...) string`.
   - Map symbols to their canonical package origins (`jetski/cli/panel.*`, `jetski/cli/model.*`, `jetski/cli/swarm.*`, `jetski/cortex/step.*`).
2. **Automated Inventory Generation**:
   - Executable command: `go run ./cmd/tui-inventory` directly parses `internal/ui/...` AST and cross-references against `out/extracted-1.5.5/seeds/funcs.txt`.
   - Emits [`docs/guide/tui_component_inventory.json`](tui_component_inventory.json) detailing component name, package, upstream symbol, input props, and child components.
   - This ensures **zero implicit component omission** (e.g., catching `FormatVisibleRows` sticky prompt headers, `RenderNoticeRow` eligibility banners, and `codesearchaction.Feedback` instant trees).

---

### Layer 2: Atomic Design Hierarchy (终端原子设计分层规范)

Structure components following Brad Frost's **Atomic Design** principles adapted for terminal character cells:

| Level | Definition | Examples in OpenAGY / AgY |
|---|---|---|
| **Atoms** | Primitive visual elements that cannot be broken down further without losing meaning | `RuleBar` (`────`), Prompt glyphs (`>`, `!`), Unicode Logo art matrix, `TabAlignedRows`, Diff gutter line numbers, `vito/midterm.Canvas` highlight cell masks. |
| **Molecules** | Simple combinations of atoms functioning as a unit | Tool execution steps (`BashStep`, `ThoughtStep`, `FileDiffStep`), `/btw` side-question card, Queued prompt pills (`QueuedPromptRowWithHint`), attachment preview pills. |
| **Organisms** | Complex, distinct UI sections managing their own local input or state transitions | `PromptModel` (textarea with CJK East Asian double-width tracking and dynamic height), `StatusLineModel` (with Braille spinner, token usage, and OSC 7501 protocol status), `PermissionPrompt`. |
| **Templates & Modals** | High-level page layouts, full-screen dialogs, and overlays | All 26 official `panelregistry.Kind` modals (`MCP`, `Tasks`, `CodeSearch`, `Context`, `Plugins`, `Skills`), `Onboarding.Wizard`, `LoginUI`, `SwarmBoard`. |

---

### Layer 3: Visual Golden Snapshot Pipeline (2D 字符网格快照测试)

Move beyond "does not panic" tests by implementing **2D character buffer snapshot testing**:

1. **Virtual Screen Buffer Capture**:
   - Render the story view into a virtual terminal matrix (using `vito/midterm` or `charmbracelet/x/exp/vt`).
   - Capture two matrices per state:
     - **Text Matrix**: Pure character grid with ANSI escapes stripped;
     - **Attribute Matrix**: Foreground RGB, background RGB, bold, reverse, and underline flags per cell.
2. **Deterministic Sanitization**:
   - Replace wall-clock durations (`(12s)` → `(XXs)`), timestamps (`14:20:00` → `HH:MM:SS`), and absolute file paths (`/Users/foo` → `/workspace`).
3. **Golden Assertion**:
   - Store snapshots in `testdata/snapshots/<story_id>_<state>.golden`.
   - Any accidental whitespace insertion, rule miscalculation, or color bleeding triggers an instant, visual `git diff` during `go test`.

---

### Layer 4: Mock Event Ticker & Dynamic Streaming (流式与时钟动态步进)

To capture live interactive behaviors rather than static screenshots, equip Storybook with a virtual event pump:

1. **Token-by-Token Streaming Pump**:
   - Pressing `Space` feeds simulated assistant chunk tokens into `PromptModel` or `TranscriptFlow`.
   - Tests viewport auto-scrolling (`autoScroll = true`), sticky prompt detachment, and markdown word-wrapping in real-time.
2. **Virtual Clock Advancement**:
   - Advance animation frames for Braille spinners (`⠋ ⠙ ⠹ ⠸ ⠼ ⠴ ⠦ ⠧ ⠇ ⠏`), compaction timers, and pulsing recording dots (`● 00:14`).
3. **Interactive Step Execution**:
   - Support triggering synthetic tool outcomes (`Exit 0`, `Exit 1`, `Permission Denied`) to test error row transitions live.

---

### Layer 5: Terminal Emulation Matrix (跨终端能力仿真矩阵)

Terminal diversity is the greatest source of user-reported UI bugs. Storybook provides interactive emulation toggles:

1. **Responsive Viewport Clamping**:
   - Pressing `w` cycles terminal widths:
     - `40 columns`: Extreme narrow mobile/split pane (tests word-wrap and horizontal clipping).
     - `80 columns`: Classic standard terminal.
     - `120 columns`: Modern default terminal.
     - `200 columns`: Ultra-wide monitor (tests rule expansion and margin alignment).
2. **Protocol Capability Toggles**:
   - Pressing `c` toggles terminal protocol flags:
     - `OSC 8`: Clickable hyperlinks ↔ `text (url)` plain text fallback.
     - `OSC 7501`: Kitty/Ghostty tab status reporting ↔ silent no-op.
     - `AltScreen`: Primary buffer inline rendering ↔ full-screen alternate buffer.
3. **Color Depth Degradation**:
   - Toggle between **Truecolor (24-bit direct RGB)**, **256-color palette approximation**, and **16-color ANSI fallback** to verify legibility across all environments.

---

## 4. Current Repository Inventory (70 Component Stories)

The current implementation in `openagy` satisfies 100% of the visual surfaces identified in upstream `1.5.5`:

| Category | Count | Key Components Included |
|---|---|---|
| **Foundations** | 8 | `ThemePreview`, `MarkdownRenderer`, `PromptGlyphs`, `TranscriptFlow`, `SessionBanner`, `StickyPrompt`, `CanvasHighlights`, `TabAlignedRows` |
| **Composer & Inputs** | 4 | `PromptModel` (dynamic height), `CJKCursor` (IME/double-width), `AttachmentCards`, `PromptQueues` (sending/queued/cancelled) |
| **Autocomplete Popups** | 3 | `SlashPicker`, `MentionPicker` (`@file`), `ModelArgPopup` (30-col catalog) |
| **Tool Steps & Execution** | 8 | `BashStep`, `ThoughtStep`, `FileDiffStep`, `BatchStep`, `ErrorRow`, `TrajectoryToggle` (`Ctrl+O`), `AtomicDiffRow`, `CodeSearchFeedback` |
| **Panels & Dialogs** | 31 | All 26 `panelregistry.Kind` modals, `AskQuestionForm`, `SubagentQuestionPanel`, `OnboardingWizard`, `LoginUI`, `ResumePicker` |
| **Status & Overlays** | 16 | `StatusLine`, `StatusRow`, `Narration`, `ProgramStatus` (OSC 7501), `SideQuestion` (`/btw`), `SwarmBoard`, `VoiceBanner`, `SeasonalBanner` (5 Halloween scenes), `AnnouncementCard`, `ActiveItems`, `Notifications`, `SlashQuote`, `EligibilityNotice`, `ViewerPanel`, `LineComments`, `RewindPanel` |

---

## 5. Architectural Tier Rules & Boundaries

Storybook development strictly obeys the repository's 5-tier architecture (`AGENTS.md`):

1. **Tier 5 Placement**: `internal/ui/storybook` is presentation-only.
2. **Zero Host Imports**: Storybook **never** imports `internal/tui`, `internal/cli`, or `internal/printmode`. All host-level banners or rules are either composed from `internal/ui/tuilib` or isolated in mock factories.
3. **Decoupled Main Entry**: `cmd/storybook/main.go` acts solely as the launcher invoking `storybook.NewModel(storybook.DefaultCatalog())`.
4. **Validation Guard**: Verified on every cycle by `make arch` (`internal/core/architecture`).
