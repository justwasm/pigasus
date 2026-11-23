# Knowledge Work, Personas & Dual Product Skins

> **Subsystem Manual.** This document specifies the dual-mode architecture of `pigasus`: **Software Engineering** (`PRODUCT_SKIN_DEVELOPER`) vs. **Knowledge Work** (`PRODUCT_SKIN_NON_TECHNICAL`), onboarding personas, and how they bridge across CLI flags, prompt generation, TUI disclosure, and the Remote Control Web UI (Project 4K).

---

## 1. Upstream Origin & Protocol Analysis

In official Antigravity (`jetski/cortex`, `language_server`, and `web-assets/main.js`):
1. **Product Skins (`jetbox_state_pb.ProductSkin`)**:
   - `PRODUCT_SKIN_DEVELOPER = 1`: The classic developer persona and skin. Tailored for software development, terminal commands, code editing, and Git workflows.
   - `PRODUCT_SKIN_NON_TECHNICAL = 2`: Knowledge Work skin (historically internally referenced as Project 4K). Tailored for document writing, slides/sheets production, structured research, and minimized terminal jargon.
2. **Onboarding Personas (`GetOnboardingPersonas`)**:
   Upstream defines 5 canonical personas returned during onboarding:
   - `engineering`: Software Engineering (default for developers).
   - `knowledge-work`: Knowledge Work (default for non-technical users).
   - `data`: Data Analysis & Science.
   - `science`: Scientific Research & Discovery.
   - `design`: Design & Prototyping.
3. **Mendel Flags & Web UI Skin Trigger**:
   - Web frontend activates the Project 4K skin when `enable-project-4k` is enabled in Mendel experiments and `userConfig.userSettings.project4kMode === 2` or `appState.productSkin === 2`.

---

## 2. Architectural Layers in `pigasus`

```
  CLI / Flag Surface:  --persona <name> | --skin <developer|non-technical>
                       /persona <name>  | /skin <developer|non-technical>
                                      │
  Tier 2 (Features):   internal/features/personas (Personas catalog & wire translation)
                       internal/features/auth (JetboxState.ProductSkin persistence)
                                      │
  Tier 4 (App):        internal/app/prompt/agyprompt (Generic system prompt, artifact rules)
                       internal/printmode (Headless flag wiring)
                                      │
  Tier 5 / Host:       internal/tui (Verbosity disclosure, session state)
                       internal/remotecontrol (GetOnboardingPersonas, Mendel flags, state store)
```

### Layer 1: Prompt & Persona Domain (`internal/features/personas`, `internal/app/prompt/agyprompt`)
- **Five Personas**:
  - `engineering` (Developer skin)
  - `knowledge-work` (Non-technical skin)
  - `data` (Non-technical skin)
  - `science` (Non-technical skin)
  - `design` (Non-technical skin)
- **Prompt Specialization**:
  When Knowledge Work is active (`productSkin == 2` or non-technical persona):
  - Switches to `generic-system-prompt` branch in `agyprompt`.
  - Injects document, spreadsheet, slide, and markdown artifact directives instead of source code repo assumptions.
  - Activates `.url.json` web link bookmarks guidance and artifact generation priority.

### Layer 2: TUI Presentation (`internal/tui`)
- **Verbosity & Disclosure**:
  - Automatically defaults to `VerbosityLow` in Knowledge Work mode to fold noisy shell command invocations and diff details.
  - Ephemeral tool steps provide clean disclosure of progress.
- **Slash Commands**:
  - `/persona [name]`: Query current persona or switch between the 5 personas.
  - `/skin [developer|non-technical]`: Query or toggle between technical and non-technical skins.

### Layer 3: Remote Control & Web UI (`internal/remotecontrol`)
- **Language Server RPC**:
  - `GetOnboardingPersonas`: returns the 5 canonical personas with suggestions and recommended plugin IDs (`google-workspace`, `science`, `chrome-devtools`, etc.).
- **Mendel Experiments**:
  - Lowering `enable-project-4k` and `project-4k-mode-copy` flags to enable Web UI Project 4K skin rendering.
- **State Store Synchronization**:
  - Bidirectional mapping between `appState.productSkin` and `userSettings.project4kMode` with wire enum conversion.

---

## 3. Configuration & Usage

### CLI Flags
```bash
# Launch pigasus directly in knowledge work mode
pigasus --skin non-technical

# Or specify a specialized persona
pigasus --persona knowledge-work
pigasus --persona science
pigasus --persona data

# Headless print mode with persona
pigasus -p "Draft a project report on AI agents" --persona knowledge-work
```

### TUI Commands
Inside an interactive session:
```text
/persona              # Show active persona and available choices
/persona science      # Switch to science persona
/skin non-technical   # Switch to non-technical knowledge work skin
/skin developer       # Switch back to developer skin
```

### Persistent Configuration
In `~/.openagy/settings.json`:
```json
{
  "persona": "knowledge-work",
  "productSkin": "non-technical",
  "project4kMode": 2
}
```
