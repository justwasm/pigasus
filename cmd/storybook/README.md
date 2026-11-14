# ❖ Antigravity TUI Component Storybook & Playground

> **Standalone Bubbletea Component Catalog & Interactive UI Harness.**  
> Provides isolated demonstration, state inspection, theme verification, and interactive testing for every UI component in `openagy` / `pigasus`.

---

## 1. Overview & Architecture

In large-scale terminal user interface (TUI) engineering with the Elm architecture (`Bubbletea`), UI components often suffer from regression when tested solely inside the full asynchronous agent loop. The **Antigravity TUI Storybook** introduces a dedicated Storybook harness modeled after modern web UI design systems, enabling every visual component to be rendered, styled, navigated, and typed into in pure isolation.

### Architectural Tier Placement (`AGENTS.md`)
The storybook strictly follows the repository's 5-tier architecture without upward edges or host violations:

```
Presentation / Entry Surfaces
  cmd/storybook (Standalone binary entry)
         │
  Tier 5: UI (internal/ui/storybook)
         ├── types.go               (Category, State, Story, StoryInstance interfaces)
         ├── layout.go              (Responsive dual-pane geometry & cell clamping)
         ├── catalog.go             (Story catalogue, filtering, category grouping)
         ├── sidebar.go             (Category tree & story item list view)
         ├── canvas.go              (Breadcrumb bar, upstream symbol badge, frame, controls)
         ├── style.go               (Recovered truecolor ANSI styles & helpers)
         ├── model.go               (Root Bubbletea v2 Model, focus management, theme switching)
         ├── default_catalog.go     (Central story registry factory)
         └── stories_*.go           (Domain story definitions & mock data factories)
```

- **Tier 5 Isolation**: `internal/ui/storybook` imports downward components in `internal/ui/views/`, `internal/ui/toolsteps/`, and lower tiers (`internal/app/task`, `palette`).
- **Zero Upward Edges**: Enforced 100% by `make arch` (`internal/core/architecture`).
- **Decoupled Entry**: `cmd/storybook/main.go` instantiates `tea.NewProgram(storybook.NewModel(storybook.DefaultCatalog())).Run()`.

---

## 2. Upstream Google AgY Decompilation Parity

All component names, types, and roles strictly align 1:1 with official reverse-engineered decompilation artifacts (`google3/third_party/jetski` and `cortex`):

| Storybook Name | Package / Location | Upstream Reverse-Engineered Symbol |
|---|---|---|
| `ThemePreview` | `internal/ui/views/themepreview` | `jetski/cli.ThemePreview` |
| `MarkdownRenderer` | `internal/ui/mdrender` | `jetski/cortex.MarkdownRenderer` |
| `PromptGlyphs` | `internal/ui/storybook` | `jetski/cli/model.RuleBar` |
| `BoxBorders` | `internal/ui/storybook` | `jetski/cli.BorderStyles` |
| `PromptModel` | `internal/ui/editor/composer` | `google3/third_party/jetski/cli/model.PromptModel` |
| `CJKCursor` | `internal/ui/storybook` | `jetski/cli.CursorTracker` / `LineInfo` |
| `AttachmentCards` | `internal/ui/rows` | `jetski/cli.AttachmentRows` |
| `SlashPicker` | `internal/ui/views/slashpicker` | `jetski/cli.SlashPicker` |
| `MentionPicker` | `internal/ui/views/mentionpicker` | `jetski/cli.AtFilePicker` |
| `BashStep` | `internal/ui/toolsteps` | `jetski/cortex/step.BashStep` |
| `ThoughtStep` | `internal/ui/toolsteps` | `jetski/cortex/step.ThoughtStep` |
| `FileDiffStep` | `internal/ui/toolsteps` | `jetski/cortex/step.FileStep` |
| `ModelPicker` | `internal/ui/views/modelpicker` | `jetski/cli/panel.ModelPicker` |
| `EffortPicker` | `internal/ui/views/effortpanel` | `jetski/cli/panel.EffortPicker` |
| `TasksPanel` | `internal/ui/views/taskspanel` | `jetski/cli/panel.TasksPanel` |
| `HelpPanel` | `internal/ui/views/helppanel` | `jetski/cli/panel.HelpPanel` |
| `DiffPanel` | `internal/ui/views/diffpanel` | `jetski/cli/panel.DiffPanel` |
| `AskQuestionForm` | `internal/ui/views/askquestion` | `jetski/cli.AskQuestionForm` |
| `ContextPanel` | `internal/ui/views/contextpanel` | `jetski/cli/panel.ContextPanel` |
| `UsagePanel` | `internal/ui/views/usagepanel` | `jetski/cli/panel.UsagePanel` |
| `PluginsUI` | `internal/ui/views/pluginsui` | `jetski/cli/panel.PluginPicker` |
| `SkillsUI` | `internal/ui/views/skillsui` | `jetski/cli/panel.SkillsPicker` |
| `McpPanel` | `internal/ui/views/mcppanel` | `jetski/cli/panel.McpPanel` |
| `PermissionsPanel` | `internal/ui/views/permissionspanel` | `jetski/cli/panel.PermissionsPanel` |
| `ResumePicker` | `internal/ui/resumepicker` | `jetski/cli/panel.ResumePicker` |
| `RewindPanel` | `internal/ui/views/rewindpanel` | `jetski/cli/panel.RewindPanel` |
| `AccountsPanel` | `internal/ui/views/accountspanel` | `pigasus/accounts.AccountsPanel` |
| `OnboardingWizard` | `internal/ui/views/onboarding` | `jetski/cli/onboarding.Wizard` |
| `ChangelogPanel` | `internal/ui/views/changelog` | `jetski/cli/panel.ChangelogPanel` |
| `StatusLine` | `internal/ui/views/statusrow` | `jetski/cli/model.StatusLineModel` |
| `PermissionPrompt`| `internal/ui/views/permissionprompt`| `jetski/cli.PermissionPrompt` |
| `SideQuestionCard`| `internal/ui/views/sidequestion` | `jetski/cli.BtwCard` (`/btw`) |
| `SwarmBoard` | `internal/ui/views/swarmboard` | `jetski/cortex/native_swarm.Board` |
| `VoiceBanner` | `internal/ui/views/voice` | `jetski/cli/model.VoiceBanner` |
| `SeasonalBanner` | `internal/ui/views/seasonal` | `jetski/cli/seasonal.Banner` |
| `AnnouncementCard` | `internal/ui/views/announce` | `jetski/cli/notice.Card` |
| `ConfigPanel` | `internal/ui/views/configpanel` | `jetski/cli.ConfigPanel` |
| `CodeSearchPanel` | `internal/ui/views/codesearchpanel` | `jetski/cli.CodeSearchPanel` |
| `ProjectsPanel` | `internal/ui/views/projects` | `jetski/cli.ProjectsPanel` |
| `AddDirPanel` | `internal/ui/views/adddirpanel` | `jetski/cli.AddDirPanel` |
| `HooksPanel` | `internal/ui/views/hookspanel` | `jetski/cli.HooksPanel` |
| `ProviderPanel` | `internal/ui/views/providerpanel` | `jetski/cli.ProviderPanel` |
| `AgentsPanel` | `internal/ui/views/agentspanel` | `jetski/cli/panel.AgentsPanel` |
| `SubAgentQuestionPanel` | `internal/ui/views/subagentquestionpanel` | `jetski/cli/panel.SubAgentQuestionPanel` |
| `ActiveItems` | `internal/ui/views/activeitemspanel` | `jetski/cli.ActiveItems` |
| `RenamePanel` | `internal/ui/views/renamepanel` | `jetski/cli/panel.RenamePanel` |
| `QuoteTurn` | `internal/ui/views/slashquote` | `jetski/cli.QuoteTurn` |
| `LoginUI` | `internal/ui/views/loginui` | `jetski/cli/auth.LoginUI` |
| `FeedbackPanel` | `internal/ui/views/feedbackpanel` | `jetski/cli/panel.FeedbackPanel` |
| `CreditsPanel` | `internal/ui/views/creditspanel` | `jetski/cli/panel.CreditsPanel` |
| `ArtifactsPanel` | `internal/ui/views/artifactpanel` | `jetski/cli/panel.ArtifactsPanel` |
| `ViewerPanel` | `internal/ui/views/viewerpanel` | `jetski/cli/viewer.ViewerPanel` |
| `LineComments` | `internal/ui/views/linecomments` | `jetski/cli/review.LineComments` |
| `ModelArgPopup` | `internal/ui/views/modelargpopup` | `jetski/cli/popup.ModelArgPopup` |
| `BatchStep` | `internal/ui/toolsteps` | `jetski/cortex/step.BatchStep` |
| `ErrorRow` | `internal/ui/toolsteps` | `jetski/cortex/step.ErrorRow` |
| `Notifications` | `internal/ui/rows` | `jetski/cli/notice.Notifications` |
| `TranscriptDocument` | `internal/ui/transcript` | `jetski/cli/transcript.Document` |
| `TrajectoryToggle` | `internal/ui/toolsteps` | `jetski/cli.ToggleTrajectory` |
| `FeatDiffPanel` | `internal/ui/views/diff/feat` | `jetski/cli/panel.FeatDiff` |
| `StatusRow` | `internal/ui/views/statusrow` | `jetski/cli.StatusRow` |
| `Narration` | `internal/ui/views/narration` | `jetski/cli.Narration` |
| `ProgramStatus` | `internal/ui/views/programstatus` | `jetski/cli.ProgramStatus` |
| `SessionBanner` | `internal/ui/tuilib` | `jetski/cli.RenderBanner` |
| `StickyPrompt` | `internal/ui/foundations/viewport` | `jetski/cli/viewport.FormatVisibleRows` |
| `CanvasHighlights` | `internal/ui/views/canvas` | `vito/midterm.Canvas` |
| `TabAlignedRows` | `internal/ui/rows` | `jetski/cli.TabAlignedRows` |
| `PromptQueues` | `internal/ui/rows` | `jetski/cli.PromptQueues` |
| `AtomicDiffRow` | `internal/ui/views/diff/row` | `jetski/cortex/diff.Row` |
| `CodeSearchFeedback` | `internal/ui/views/codesearchaction` | `jetski/cli/codesearch.Feedback` |
| `EligibilityNotice` | `internal/ui/views/announce` | `jetski/cli/notice.RenderNoticeRow` |

---

## 3. Registered Stories & State Catalog

The default catalogue registers **73 stories** covering **195+ state variations** across 7 architectural domains:

### Category 1: Foundations
1. **`foundations-theme-preview`** (`ThemePreview`):
   - `solarized-light`: Ethan Schoonover's Solarized Light scheme.
   - `dark`: Dark background with light foreground.
   - `tokyo-night`: Modern Tokyo Night Moon palette.
   - `terminal`: Default 16-color host terminal palette.
2. **`foundations-markdown`** (`MarkdownRenderer`):
   - `headings-code`: H1–H4 headers, fenced code blocks with syntax highlighting.
   - `lists-tables`: GFM markdown bullet lists, ordered numbers, aligned tables.
   - `quotes-links`: Blockquotes, clickable hyperlinks, and formatting emphasis.
   - `mermaid-flowchart`: Terminal ASCII flowchart box-and-arrow rendering (`graph TD`).
   - `mermaid-sequence`: Sequence diagram participant interaction flows (`sequenceDiagram`).
   - `latex-math`: Inline ($\alpha$) and display ($\sum, \sigma$) mathematical formulas rendered in Unicode.
   - `github-alerts`: Callout alerts: `[!NOTE]`, `[!TIP]`, `[!IMPORTANT]`, `[!WARNING]`, `[!CAUTION]`.
3. **`foundations-rules`** (`PromptGlyphs`):
   - `normal-rule`: Standard composer upper/lower bounding rules and normal `>` prompt glyph.
   - `bash-mode`: Vim bash mode with red `!` prompt glyph.
   - `separators`: Section divider lines (`───` and `┈┈┈`).
4. **`foundations-logo-transforms`** (`LogoTransforms`):
   - `pigasus-default`: Canonical brand logo with 180° rotated winged delta (`FlipBoth`).
   - `reference-agy`: Upstream Google Antigravity upward delta raster (`10×16` sub-pixels).
   - `flip-vert`: Vertical flip reflected across horizontal axis (`FlipVert`).
   - `flip-horiz`: Horizontal flip mirrored across vertical axis (`FlipHoriz`).
   - `invert-colors`: Sub-pixel color inversion swapping top (▀) and bottom (▄) cells within each terminal cell.
   - `simple-blocks-5x6`: Full background color blocks for Apple Terminal (`5×6` matrix).
   - `all-gallery`: Comprehensive side-by-side gallery comparing all 6 transformations.
5. **`foundations-box-borders`** (`BoxBorders`):
   - `normal-sharp`: Standard single-line box-drawing (`┌─┐`, `│`, `└─┘`) for `PermissionPrompt`, `AnnouncementCard`, and form dialogs.
   - `rounded-curved`: Modern curved arc borders (`╭─╮`, `│`, `╰─╯`) for notification popovers, floating cards, and menus.
   - `thick-heavy`: Heavy-weight bold framing (`┏━┓`, `┃`, `┗━┛`) for high-priority active containers and focus rings.
   - `double-line`: Classical double-ruled framing (`╔═╗`, `║`, `╚═╝`) for security boundary alerts and system dialogs.
   - `dotted-dashed`: Dashed & dotted lines (`┌┈┐`, `┊`, `└┈┘`) for subagent scratchpads, draft turns, and secondary drawers.
   - `left-accent`: Single-sided thick left callout accent (`┃` / `▌`) for `/btw` `SideQuestionCard`, markdown blockquotes, and streaming thought blocks.
   - `all-gallery`: Side-by-side gallery and Unicode glyph spec table comparing all 6 styles with interactive `[+/-]` width scaling.
6. **`foundations-transcript-flow`** (`TranscriptDocumentFlow`):
   - `standard-turn`: User prompt bubble, divider, tool execution step, and markdown response.
   - `slash-command-turn`: Highlighted warning user prompt for slash command with immediate reply.

### Category 2: Composer & Inputs
4. **`composer-prompt-model`** (`PromptModel`):
   - `empty-plan-mode`: Mode banner (`Plan Mode`) with empty textarea placeholder.
   - `single-line-token`: Colored command token `/model claude` with argument hint.
   - `multiline-input`: Multiline prompt editing.
   - `esc-clear-armed`: Double-escape input clearing hint (`[Esc again to clear input]`).
5. **`composer-cjk-cursor`** (`CJKCursor`):
   - `chinese-sentence`: Pure Chinese sentence (`在深思熟虑之后，我们决定构建一个专用的前端 Storybook...`).
   - `mixed-cjk-ascii`: Mixed CJK and ASCII alphanumeric characters with rune width metrics.
   - `cjk-multiline`: Multiline CJK text verifying hardware cursor coordinates `(curX, curY)`.
6. **`composer-attachments`** (`AttachmentCards`):
   - `collapsed`: Collapsed attachment pills.
   - `expanded`: Detailed file preview cards with dimensions, mime-types, and sizes.

### Category 3: Autocomplete Popups
7. **`popups-slash-picker`** (`SlashPicker`):
   - `all-commands`: Initial 5 rows with `↓ 100 more` indicator.
   - `filtered-m`: Filtered commands matching `/m` (`/model`, `/mcp`, `/models`).
   - `filtered-t`: Filtered commands matching `/t` (`/tasks`).
   - `no-matches`: Empty result state (`No matches`).
8. **`popups-mention-picker`** (`MentionPicker`):
   - `initial`: Workspace root file suggestions for `@`.
   - `filtered-in`: Workspace files filtered by prefix `@internal/...`.
9. **`popups-model-arg`** (`ModelArgPopup`):
   - `all-models`: Full catalogue popup with 30-column labels and IDs.
   - `filtered-claude`: Subsequence filtered models matching 'claude'.

### Category 4: Tool Steps & Execution
10. **`toolsteps-bash-step`** (`BashStep`):
    - `running`: Shell command execution in-flight with animated spinner.
    - `success`: Command finished with exit code 0.
    - `failure`: Command exited with non-zero exit code and error output.
11. **`toolsteps-thought-step`** (`ThoughtStep`):
    - `live-snippet`: Real-time streaming thinking snippet.
    - `collapsed-thought`: Collapsed duration badge and token count.
    - `expanded-thought`: Full multi-line reasoning block.
12. **`toolsteps-file-diff`** (`FileDiffStep`):
    - `create-file`: Tool card for new file creation.
    - `edit-file-diff`: Unified diff hunk display with added/removed lines.
13. **`toolsteps-batch`** (`BatchStep`):
    - `batch-expanded`: Detailed multi-step cards showing concurrent command arguments and status.
    - `batch-collapsed`: Compact summary badge showing count of batched executions.
14. **`toolsteps-error-row`** (`ErrorRow`):
    - `command-failure`: Non-zero exit code error line formatted for transcript.
    - `permission-denied`: Security boundary rejection error callout.
15. **`toolsteps-trajectory-toggle`** (`TrajectoryToggle`):
    - `collapsed-hint`: Compact tool summary bullet with `(ctrl+o to expand)` hint.
    - `expanded-hint`: Full stdout execution lines with `(ctrl+o to collapse)` hint.
    - Interactive Playground Mode: Press `Ctrl+O` to dynamically toggle collapse/expansion!

### Category 5: Panels & Dialogs
15. **`panel-model-picker`** (`ModelPicker`):
    - `default`: Canonical reference model catalogue (Gemini 3.8 Flash, Claude Sonnet 4.6, GPT-OSS 120B).
    - `filtered-claude`: Subsequence query filtered to Claude models.
    - `custom-models`: Gateway-configured dynamic provider models.
    - `no-matches`: Empty query result.
16. **`panel-effort-picker`** (`EffortPicker`):
    - `low`: Low reasoning effort.
    - `medium`: Medium reasoning effort.
    - `high`: High reasoning effort.
    - `unadjustable`: Fixed model without reasoning tiers.
17. **`panel-tasks`** (`TasksPanel`):
    - `active`: Multiple running shell, subagent, and timer tasks.
    - `completed`: Mixed success and failed background work.
    - `empty`: Empty state (`No background tasks`).
18. **`panel-help`** (`HelpPanel`):
    - `general`: General documentation, version, and workspace info.
    - `commands`: Canonical commands catalogue table.
    - `shortcuts`: Keyboard hotkey matrix.
    - `vim`: Vim mode navigation and operator cheatsheet.
19. **`panel-diff`** (`DiffPanel`):
    - `vcs-files`: Tracked modified and untracked repository changes.
    - `inline-diff`: Interactive hunk-by-hunk file diff view.
    - `no-vcs`: Directory without git repository.
20. **`form-ask-question`** (`AskQuestionForm`):
    - `single-select`: Single-choice radio buttons.
    - `multi-select`: Multi-selection checkboxes.
    - `write-in`: Custom text input field.
21. **`panel-context`** (`ContextPanel`):
    - `empty`: Empty session awaiting first turn.
    - `streaming`: In-flight turn token usage updating dynamically.
    - `live`: Complete 44-cell usage grid, system prompt, rules, skills, checkpoint buffer, and related commands.
22. **`panel-usage`** (`UsagePanel`):
    - `summary`: Gemini 3.8 and Claude models rolling limits and weekly bars with reset countdowns.
    - `loading`: Braille spinner while retrieving quota summary.
    - `stub-groups`: Static fallback quota groups with frozen refresh times.
23. **`panel-plugins`** (`PluginsUI`):
    - `installed`: Installed plugins list with enable/disable badges and actions.
    - `discover`: Marketplace shelves (Featured, Popular, Tools).
    - `search`: Instant keyword search results across plugin catalog.
24. **`panel-skills`** (`SkillsUI`):
    - `catalog-list`: Grouped skills catalog across workspace, builtin, and plugin roots.
    - `skill-detail`: Interactive SKILL.md markdown documentation viewer.
25. **`panel-mcp`** (`McpPanel`):
    - `empty`: Empty frame ("No MCP servers configured.") with navigation legend.
26. **`panel-permissions`** (`PermissionsPanel`):
    - `scopes`: Scopes picker (Project, Shared with Pigasus, Global).
    - `lists`: Tab view of allowlist, denylist, asklist with rule counts.
    - `form`: Add rule form with action selection and target pattern input.
27. **`panel-resume`** (`ResumePicker`):
    - `tree`: Workspace tree view with `[CURRENT]` session, timestamps, and preview snippets.
    - `search`: Filtered query matching conversations across workspaces.
28. **`panel-rewind`** (`RewindPanel`):
    - `turns`: Turn checkpoints with prompt text and rollback targets.
    - `empty`: Initial turn before conversation starts.
29. **`panel-accounts`** (`AccountsPanel`):
    - `accounts-list`: Multi-account credentials list with active indicator and plan tiers.
    - `pool-policy`: Account pool routing policy editor (Round-Robin, Failover).
30. **`panel-onboarding`** (`OnboardingWizard`):
    - `color-scheme`: 1:1 Side-by-side layout with rounded preview card (`╭───╮`), diff line numbers, syntax highlighting, and live `[+/-]` width scaling.
    - `color-scheme-narrow`: Adaptive narrow terminal layout showing `(widen the terminal to see a preview)` hint when width < 70.
    - `terms`: Terms of Service and data use opt-in checkbox.
    - `migration`: Gemini CLI asset migration checklist.
31. **`panel-changelog`** (`ChangelogPanel`):
    - `formatted`: Wrapped release notes ladder formatted for current terminal dimensions.
32. **`panel-config`** (`ConfigPanel`):
    - `settings-list`: Full settings catalog with sections and currently active values.
    - `editing-row`: Cursor moved onto alternate screen / vim keys row with live toggles.
33. **`panel-codesearch`** (`CodeSearchPanel`):
    - `usage`: Initial empty search form prompting for query syntax.
    - `results`: Rendered match hits across multiple files with context lines.
    - `file-viewer`: Result opened in the built-in file viewer with line numbering.
34. **`panel-projects`** (`ProjectsPanel`):
    - `project-list`: List of configured projects with active workspace indicator.
    - `create-modal`: New project name prompt dialog.
    - `edit-folders`: Editing directory folders tied to selected project.
35. **`panel-adddir`** (`AddDirPanel`):
    - `trusted-dirs`: List of existing trusted workspace folders.
    - `input-path`: Path input buffer filled with new directory candidate.
36. **`panel-hooks`** (`HooksPanel`):
    - `hook-types`: List of builtin lifecycle and tool hook event triggers.
    - `matchers`: Matcher rules and active filters for selected hook type.
    - `form`: Input form to define a new tool matcher or lifecycle handler.
37. **`panel-provider`** (`ProviderPanel`):
    - `provider-list`: Configured model providers and endpoints list.
    - `search-filter`: Filtering providers by name or model tag.
    - `edit-form`: Interactive form for custom BaseURL, API Key, and wire format.
38. **`panel-agents`** (`AgentsPanel`):
    - `catalog`: List of workspace markdown agents and built-in defaults.
    - `running-subagents`: Active subagents with Kill (k) and Authorization (A/D) hotkeys.
39. **`panel-subagent-question`** (`SubagentQuestionPanel`):
    - `picker-overlay`: Subagent question overlay with options and arrow navigation.
    - `option-select`: Cursor moved to target option with active selection indicator.
    - `status-alert`: Compact alert notification in status line when agent has question.
40. **`panel-rename`** (`RenamePanel`):
    - `editing-title`: Textinput prompt pre-filled with candidate conversation title.
    - `empty-title`: Initial blank rename dialog with placeholder.
41. **`panel-login-ui`** (`LoginUI`):
    - `select-method`: Browser OAuth vs Application Default Credentials selector.
    - `oauth-code-box`: Verification URL and authorization code display.
    - `exchanging-token`: Polling for authorization completion with animated spinner.
    - `error-state`: Clear error callout with retry or cancel instructions.
42. **`panel-feedback`** (`FeedbackPanel`):
    - `category-picker`: 6 canonical feedback issue categories with numbered selection.
    - `details-form`: Echoes selected category and opens free-text input buffer.
43. **`panel-credits`** (`CreditsPanel`):
    - `balance-card`: Balance display with cursor on 'Get More AI Credits'.
    - `second-action`: Cursor moved onto 'See Activity' history action.
44. **`panel-artifacts`** (`ArtifactPanel`):
    - `empty-state`: Default empty artifacts list with keyboard navigation legend.
45. **`panel-viewer`** (`ViewerPanel`):
    - `document-top`: Initial viewport showing document header and line counter.
    - `document-scrolled`: Viewport scrolled downward with updated range indicators.
46. **`panel-line-comments`** (`LineComments`):
    - `unsent-screen`: File header, line annotations, and Send / Discard / Keep actions.
    - `formatted-bubble`: Transmitted line comment block formatted for model turn.
47. **`panel-featdiff`** (`FeatDiffPanel`):
    - `feature-matrix`: Clean-room parity audit table comparing OpenAGY against upstream jetski/cortex 1.5.5.

### Category 6: Status & Overlays
48. **`status-statusline`** (`StatusLine`):
    - `idle`: Default idle prompt status.
    - `narrow`: 50-column terminal with compacted segments.
    - `streaming`: Streaming turn with running task counter.
    - `compacting`: Context compaction label (`⠿  Compacting context… (12s)`).
    - `bash-mode`: Bash mode armed status.
49. **`status-permission-prompt`** (`PermissionPrompt`):
    - `command`: Command execution authorization box.
    - `write-file`: File modification authorization box.
    - `feedback`: Interactive rejection feedback text mode.
50. **`status-side-question`** (`SideQuestionCard`):
    - `streaming`: Animated Braille spinner with active tool call status.
    - `collapsed`: 30% preview card with `Tab to expand` hint.
    - `expanded`: Full markdown viewport with scroll indicators.
51. **`status-swarm-board`** (`SwarmBoard`):
    - `3-pane`: Roster, thread posts, and message panes.
    - `filtered`: Posts filtered by member.
    - `zoomed`: Full-screen message pane.
52. **`status-voice-banner`** (`VoiceBanner`):
    - `connecting`: Real-time speech transcription connecting state.
    - `recording`: Pulsing red recording dot with mm:ss duration counter and keyboard hints.
    - `finishing`: Processing speech and confirming final transcription text.
53. **`status-seasonal-banner`** (`SeasonalBanner`):
    - `halloween`: Spooky season 24-bit orange ASCII art, custom border, and greeting.
54. **`status-announcement`** (`AnnouncementCard`):
    - `update-notice`: New version release notification card rendered with full boxed frame (`┌─...─┐`, `│...│`, `└─...─┘`).
    - `feature-notice`: Experimental feature preview announcement card with boxed border.
    - `service-notice`: Daily service quota rollover and reset notice card.
55. **`status-active-items`** (`ActiveItemsPanel`):
    - `running-tray`: Background processes and subagents with status dots and elapsed time.
    - `focused-selection`: Focused active item with navigation indicator (>).
56. **`status-slash-quote`** (`SlashQuote`):
    - `quoted-user-turn`: Resolved previous turn citation ready for inline quoting.
    - `multiline-quote`: Multi-line code snippet quoting without phantom trailing newlines.
57. **`status-notifications`** (`Notifications`):
    - `timeout-warning`: High-priority warning banner when background task times out.
    - `task-finished`: Success banner when background task completes with zero exit code.
58. **`status-statusrow`** (`StatusRow`):
    - `idle`: Compact single-line status bar with shortcuts hint.
    - `streaming`: In-flight turn state with Esc to cancel hint.
    - `compacting`: Live compaction label with elapsed duration.
    - `exit-confirm`: Double-Ctrl+C exit confirmation warning.
59. **`status-narration`** (`Narration`):
    - `intro-text`: Verbatim introductory voice narration text.
    - `disabled-notice`: Narration disabled confirmation notice.
    - `already-disabled`: Notice indicating narration is already disabled.
60. **`status-program-status`** (`ProgramStatus`):
    - `busy-working`: Active turn OSC 7501 terminal title badge.
    - `blocked-permission`: Blocked on permission approval badge.
    - `idle-done`: Turn completed status badge.

---

## 4. Keybindings & Interactive Controls

The Storybook operates with dual-pane focus and seamless switching:

### Sidebar Navigation Mode (Default)
| Key | Action |
|---|---|
| `↑` / `k` | Move selection up |
| `↓` / `j` | Move selection down |
| `Enter` / `l` | Focus **Playground Mode** to interact directly with the component |
| `Tab` | Cycle forward through component state variations |
| `1` .. `9` | Jump directly to state variation preset #1..#9 |
| `/` | Enter search filter mode to filter components by name or category |
| `Esc` | Clear search filter or exit search mode |
| `t` | Cycle active color scheme (Solarized Light → Dark → Tokyo Night → Terminal) |
| `q` / `Ctrl+C` | Quit Storybook |

### Playground Mode (Interactive Component Testing)
| Key | Action |
|---|---|
| Typing / Keys | Delivered straight to the component's `HandleKey()` method (e.g. typing in prompt, navigating sliders, checking options) |
| `Ctrl+O` | **Toggle trajectory / tool-step collapse**: Dynamically expands or collapses tool execution steps, thought blocks, batch execution cards, attachment pills, and transcript steps (toggling between `(ctrl+o to expand)` and `(ctrl+o to collapse)`). In `DiffPanel`, `Ctrl+O` folds/unfolds the hunk nearest to the cursor |
| `Esc` | Return keyboard focus back to the Storybook Sidebar |

---

## 5. Completed Implementation Plan & TODO Checklist

The entire 5-phase plan has been executed to completion:

- [x] **Phase 1: Architecture & Engine Scaffolding**
  - [x] Create Storybook types and interfaces (`Category`, `State`, `Story`, `StoryInstance`, `FocusArea`) in `internal/ui/storybook/types.go`
  - [x] Implement responsive dual-pane geometry and width clamping in `internal/ui/storybook/layout.go`
  - [x] Implement central registry `Catalog`, category grouping, and filter search in `internal/ui/storybook/catalog.go`
  - [x] Build sidebar tree renderer in `internal/ui/storybook/sidebar.go`
  - [x] Build preview canvas renderer in `internal/ui/storybook/canvas.go`
  - [x] Build root Bubbletea v2 Model with dual focus, theme cycling, and state dispatch in `internal/ui/storybook/model.go`

- [x] **Phase 2: Foundations & Inputs Stories**
  - [x] Implement `ThemePreview` (Solarized Light, Dark, Tokyo Night, Terminal) in `stories_foundations.go`
  - [x] Implement `MarkdownRenderer` (headings, code blocks, tables, blockquotes) in `stories_foundations.go`
  - [x] Implement `PromptGlyphs` (normal rules, bash glyph `!`, dividers) in `stories_foundations.go`
  - [x] Implement `PromptModel / Composer` (plan mode, token styling, multiline, esc-clear) in `stories_composer.go`
  - [x] Implement `CJKCursor` (East Asian double-width rune metrics & IME docking) in `stories_composer.go`
  - [x] Implement `AttachmentCards` (collapsed pills & expanded image preview cards) in `stories_composer.go`

- [x] **Phase 3: Autocomplete Popups & Tool Steps Stories**
  - [x] Implement `SlashPicker` (5 rows, filtering `/m` & `/t`, no matches) in `stories_popups.go`
  - [x] Implement `MentionPicker` (`@` file suggestions & prefix filtering) in `stories_popups.go`
  - [x] Implement `BashStep` (spinner running, exit 0 success, exit 1 failure) in `stories_toolsteps.go`
  - [x] Implement `ThoughtStep` (streaming snippet, collapsed badge, expanded block) in `stories_toolsteps.go`
  - [x] Implement `FileDiffStep` (file creation & unified diff hunks) in `stories_toolsteps.go`

- [x] **Phase 4: Panels, Dialogs & Status Overlays Stories**
  - [x] Implement `ModelPicker` (canonical models, subsequence query, custom gateway models) in `stories_panels.go`
  - [x] Implement `EffortPicker` (low, medium, high, unadjustable slider) in `stories_panels.go`
  - [x] Implement `TasksPanel` (active tasks, completed/failed tasks, empty state) in `stories_panels.go`
  - [x] Implement `HelpPanel` (General, Commands, Shortcuts, Vim tabs) in `stories_panels.go`
  - [x] Implement `DiffPanel` (working-copy files, inline diff hunk view, no VCS) in `stories_panels.go`
  - [x] Implement `AskQuestionForm` (single radio choice, multi checkboxes, write-in) in `stories_panels.go`
  - [x] Implement `StatusLine` (idle, narrow 50 cols, streaming turn, context compacting, bash mode) in `stories_status.go`
  - [x] Implement `PermissionPrompt` (command approval, file write, feedback mode) in `stories_status.go`
  - [x] Implement `SideQuestionCard` (`/btw` spinner, collapsed card, expanded markdown) in `stories_status.go`
  - [x] Implement `SwarmBoard` (3-pane overview, member filtering, zoomed message) in `stories_status.go`
  - [x] Construct `DefaultCatalog()` central factory in `internal/ui/storybook/default_catalog.go`

- [x] **Phase 5: CLI Entry Surface, Verification & Documentation**
  - [x] Build standalone CLI entry `cmd/storybook/main.go` with interactive runner and `--help`
  - [x] Write comprehensive unit test suite in `internal/ui/storybook/storybook_test.go` covering all stories and state transitions
  - [x] Verify architecture layering rules with `make arch` (0 violations)
  - [x] Verify compilation and quick suite with `make quick`
  - [x] Compile finalized TODO list and documentation into `cmd/storybook/README.md`

- [x] **Phase 6: Component Catalog Expansion (14 Extended Stories)**
  - [x] Implement `ContextPanel` (empty, streaming context, live multi-turn context) in `stories_panels_extended.go`
  - [x] Implement `UsagePanel` (summary quota bars, loading, stub tier groups) in `stories_panels_extended.go`
  - [x] Implement `PluginsUI` (installed list, discover catalog, search filtering) in `stories_panels_extended.go`
  - [x] Implement `SkillsUI` (skills catalog list, skill detail inspection) in `stories_panels_extended.go`
  - [x] Implement `McpPanel` (configured servers & empty state) in `stories_panels_extended.go`
  - [x] Implement `PermissionsPanel` (active scopes, allow/deny lists, new rule form) in `stories_panels_extended.go`
  - [x] Implement `ResumePicker` (conversation session tree & search filter) in `stories_panels_extended.go`
  - [x] Implement `RewindPanel` (conversation turn checkpoints & initial state) in `stories_panels_extended.go`
  - [x] Implement `AccountsPanel` (multi-account credentials & pool routing policy) in `stories_panels_extended.go`
  - [x] Implement `OnboardingWizard` (8-theme palette picker, ToS consent, migration checklist) in `stories_panels_extended.go`
  - [x] Implement `ChangelogPanel` (formatted release notes ladder) in `stories_panels_extended.go`
  - [x] Implement `VoiceBanner` (connecting, recording pulse, finishing transcription) in `stories_status_extended.go`
  - [x] Implement `SeasonalBanner` (spooky season banner & greeting) in `stories_status_extended.go`
  - [x] Implement `AnnouncementCard` (update notice & experimental feature notice) in `stories_status_extended.go`

- [x] **Phase 7: Full Subsystem Coverage (22 New Stories — 100% Coverage Milestone)**
  - [x] Implement `ConfigPanel` (settings list, editing toggle rows) in `stories_management.go`
  - [x] Implement `CodeSearchPanel` (usage syntax, streaming ripgrep matches, inline file viewer) in `stories_management.go`
  - [x] Implement `ProjectsPanel` (project list, create project modal, edit folders) in `stories_management.go`
  - [x] Implement `AddDirPanel` (trusted directories list, new directory path input) in `stories_management.go`
  - [x] Implement `HooksPanel` (stage 1 types, stage 2 matchers, stage 3 rule form) in `stories_management.go`
  - [x] Implement `ProviderPanel` (provider list, search filter, add/edit endpoint form) in `stories_management.go`
  - [x] Implement `AgentsPanel` (markdown agents catalogue, active subagent supervisor) in `stories_collaboration.go`
  - [x] Implement `SubagentQuestionPanel` (picker overlay, arrow navigation, status alert) in `stories_collaboration.go`
  - [x] Implement `ActiveItemsPanel` (prompt active items tray, focused item selection) in `stories_collaboration.go`
  - [x] Implement `RenamePanel` (editing session title, blank input placeholder) in `stories_collaboration.go`
  - [x] Implement `SlashQuote` (turn quote resolution, clean citation insertion) in `stories_collaboration.go`
  - [x] Implement `LoginUI` (method selector, device code box, token exchange, error state) in `stories_auth_feedback.go`
  - [x] Implement `FeedbackPanel` (6 preset issue categories, free-text details form) in `stories_auth_feedback.go`
  - [x] Implement `CreditsPanel` (AI credit balance card, activity action) in `stories_auth_feedback.go`
  - [x] Implement `ArtifactPanel` (turn artifacts list, keyboard shortcuts legend) in `stories_auth_feedback.go`
  - [x] Implement `ViewerPanel` (scrollable documentary viewport, line range pager) in `stories_auth_feedback.go`
  - [x] Implement `LineComments` (unsent review comments screen, wire formatted annotation) in `stories_auth_feedback.go`
  - [x] Implement `ModelArgPopup` (30-column model completion popup, subsequence search) in `stories_advanced_steps.go`
  - [x] Implement `BatchStep` (parallel multi-tool execution grouping, collapsed/expanded) in `stories_advanced_steps.go`
  - [x] Implement `ErrorRow` (diagnostic error rows and failure callouts) in `stories_advanced_steps.go`
  - [x] Implement `Notifications` (task timeout warning banner, finished notification) in `stories_advanced_steps.go`
  - [x] Implement `TranscriptDocumentFlow` (standard assistant turn, slash command flow) in `stories_advanced_steps.go`

- [x] **Phase 8: Trajectory Toggle & Interactive `Ctrl+O` Collapsing Support**
  - [x] Implement `isCtrlO` chord detector in `internal/ui/storybook/style.go`
  - [x] Implement dedicated `TrajectoryToggle` (`toolsteps-trajectory-toggle`) story showing dynamic `(ctrl+o to expand)` ↔ `(ctrl+o to collapse)`
  - [x] Wire live `Ctrl+O` interactive toggling into `BashStep`, `ThoughtStep`, `FileDiffStep`, `BatchStep`, `AttachmentCards`, and `TranscriptDocumentFlow`
  - [x] Support `Ctrl+O` / `f` hunk folding in `DiffPanel`
  - [x] Implement `FeatDiffPanel` (`panel-featdiff`) clean-room parity audit matrix
  - [x] Implement compact `StatusRow`, `Narration`, and `ProgramStatus` OSC 7501 protocol status stories
  - [x] Add comprehensive `TestCtrlO_InteractiveToggle` unit tests verifying interactive state transitions

- [x] **Phase 9: Atomic Viewport & Pipeline Coverage (8 Atomic Components — 70 Total Stories)**
  - [x] Implement `SessionBanner` (`foundations-session-banner`: Pro tier, security agent warning, model fallback warning, compact mode) in `stories_atomic.go`
  - [x] Implement `StickyPrompt` (`foundations-sticky-prompt`: inline pinned prompt, deep transcript scroll rule, narrow width truncation) in `stories_atomic.go`
  - [x] Implement `CanvasHighlights` (`foundations-canvas-highlights`: single keyword match, multi-region highlights, CJK East Asian double-width runes) in `stories_atomic.go`
  - [x] Implement `TabAlignedRows` (`foundations-tab-aligned`: keybindings cheatsheet, mixed CJK tabular alignment, ragged row padding) in `stories_atomic.go`
  - [x] Implement `PromptQueues` (`composer-prompt-queues`: sending message in-flight, queued prompts with alt+m hint, queued commands, interrupted hint) in `stories_atomic.go`
  - [x] Implement `AtomicDiffRow` (`toolsteps-diff-row`: hunk header, line numbers, additions/deletions tint, truncation more marker) in `stories_atomic.go`
  - [x] Implement `CodeSearchFeedback` (`toolsteps-codesearch-action`: workspace match hits, MaxHits truncation tail, zero hits feedback) in `stories_atomic.go`
  - [x] Implement `EligibilityNotice` (`status-eligibility-notice`: OSC 8 clickable link, plain fallback, tier upgrade notice) in `stories_atomic.go`

- [x] **Phase 10: Interactive Component Enhancements, Boxed Framing & Foundations Expansion (72 Stories)**
  - [x] Fix Onboarding Wizard code preview indentation: replace raw `\t` with clean 4-space indentation for Go function bodies and expressions in `internal/ui/views/onboarding/step_colors.go`
  - [x] Fix Playground Mode keyboard navigation across panels: replace improper `rune` conversions with native Bubble Tea v2 `key.Key()`, restoring arrow keys and Enter across `PluginsUI`, `SkillsUI`, `PermissionsPanel`, `ResumePicker`, `RewindPanel`, and `AccountsPanel`
  - [x] Implement interactive keyboard handling for `OnboardingWizard` (`↑`/`↓` scheme selection, direct `1`..`8` pick, `Enter` to advance to terms, `Space` to toggle agreement)
  - [x] Implement viewport scrolling and window pagination for `ChangelogPanel`, `ContextPanel`, and `FeatDiffPanel` (`↑`/`↓`, `PgUp`/`PgDn`, `Home`/`End`)
  - [x] Standardize `AnnouncementCard` onto full closed-box framing (`RenderCard` directly outputs `┌─...─┐`, `│...│`, `└─...─┘` matching upstream `agy` dialog standards), removing legacy open-bracket code and updating test golden bytes
  - [x] Expand `foundations-markdown` with ASCII Mermaid diagrams (`graph TD` flowchart, `sequenceDiagram`), LaTeX mathematical notation ($\alpha, \sum, \sigma$), and GitHub alert callouts (`[!NOTE]`, `[!TIP]`, `[!WARNING]`, `[!CAUTION]`)
  - [x] Implement `foundations-logo-transforms` (`LogoTransforms`) displaying all 6 transformations: Pigasus 180° inverted delta, reference AgY upward delta, vertical flip, horizontal flip, sub-pixel color inversion, and Apple Terminal 5×6 block mode
  - [x] Implement `foundations-box-borders` (`BoxBorders`) displaying 6 terminal border styles (Normal/Sharp, Rounded, Thick/Heavy, Double-line, Dotted/Dashed, Left Accent Callout), complete Unicode glyph reference tables, and interactive `[+/-]` width scaling with `[Space]` title tag toggling

---

## 6. How to Run

### Run Standalone Binary
```bash
go run ./cmd/storybook
```

Or build and execute:
```bash
go build -o /tmp/storybook ./cmd/storybook
/tmp/storybook
```

### Run Storybook Unit Tests
```bash
go test -v ./internal/ui/storybook/...
```

### Verify Architecture Compliance
```bash
make arch
```
