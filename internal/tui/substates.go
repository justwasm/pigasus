// Package tui - substates.go: encapsulates the sub-state domains of runMode.
//
// The TUI state machine is decomposed into 5 cohesive sub-state domains:
//   - sessionState:  identities, credentials, active conversation metadata & config
//   - engineState:   backend stream channels, agent turn execution & step tracking
//   - uiState:       viewport geometry, theme, tool step expansion & voice surface
//   - composerState: text input buffer, history cursor, auto-complete & attachments
//   - overlayState:  modal panels, side-question cards, announcements & dialogs
package tui

import (
	"context"
	"flag"
	"time"

	sp "charm.land/bubbles/v2/spinner"
	tas "charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/app/session/supervisor"
	subagent "github.com/justwasm/pigasus/internal/app/subagent/core"
	subagentquestions "github.com/justwasm/pigasus/internal/app/subagent/questions"
	subagentrouter "github.com/justwasm/pigasus/internal/app/subagent/router"
	"github.com/justwasm/pigasus/internal/app/subagenttools"
	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/internal/app/tuistore"
	"github.com/justwasm/pigasus/internal/core/backend"
	corehooks "github.com/justwasm/pigasus/internal/core/policy/hooks"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	"github.com/justwasm/pigasus/internal/core/policy/sandbox"
	"github.com/justwasm/pigasus/internal/features/plugins/hooks"
	pluginwatch "github.com/justwasm/pigasus/internal/features/plugins/watch"
	"github.com/justwasm/pigasus/internal/features/tools/clipboard"
	"github.com/justwasm/pigasus/internal/ui/editor/composer"
	"github.com/justwasm/pigasus/internal/ui/editor/keybind"
	"github.com/justwasm/pigasus/internal/ui/foundations/history"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/resumepicker"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	"github.com/justwasm/pigasus/internal/ui/transcript"
	"github.com/justwasm/pigasus/internal/ui/views/activeitemspanel"
	"github.com/justwasm/pigasus/internal/ui/views/announce"
	"github.com/justwasm/pigasus/internal/ui/views/helppanel"
	"github.com/justwasm/pigasus/internal/ui/views/loginui"
	"github.com/justwasm/pigasus/internal/ui/views/mentionpicker"
	"github.com/justwasm/pigasus/internal/ui/views/modelpicker"
	"github.com/justwasm/pigasus/internal/ui/views/seasonal"
	"github.com/justwasm/pigasus/internal/ui/views/sidequestion"
	"github.com/justwasm/pigasus/internal/ui/views/slashpicker"
	"github.com/justwasm/pigasus/internal/ui/views/statusline"
	"github.com/justwasm/pigasus/internal/ui/views/subagentquestionpanel"
	"github.com/justwasm/pigasus/internal/ui/views/voice"
	"github.com/justwasm/pigasus/render"
	"github.com/justwasm/pigasus/vim"
)

// sessionState holds the domain state for sessionState.
type sessionState struct {
	email string
	model string
	// Cycle 80: status row model label is the base name only (no
	statusModel string
	tier        string
	// tierFetched is true once a plan tier was actually read from the
	// server for tierAccount.
	//
	// The tier is *not* filled with a placeholder while it is unknown:
	// the banner and the /help panel show the email alone, and the
	// /accounts panel shows a dash. A placeholder ("Antigravity
	// Starter Quota") would be a value that is not the account's, and
	// it would flash into the real tier under the user's eyes.
	tierFetched bool
	// tierAccount is the account the session's tier was read for.
	//
	// The tier belongs to an account, not to the session: a session
	// that signs in as another account, or follows a switch made
	// outside, must not keep showing the previous account's tier. The
	// /accounts panel renders a row's tier from the registry, and
	// falls back to the session's tier only for the account the
	// session actually asked about.
	tierAccount string
	cwd         string
	// Cycle 86: per-session conversation UUID shown in the /help
	conversationID string
	// initialConversationID is the UUID generated at runMode creation.
	// Used to distinguish a brand new unstarted session from an active conversation.
	initialConversationID string
	conversationStarted   bool
	// Cycle resume-continuation: auto-generated conversation title
	autoTitle string
	// Cycle 81: when the configured model (settings.json's
	fallbackFrom string
	fallbackTo   string
	// Cycle 382: extracted session state (selection, bash mode)
	store tuistore.Store
	// supervisor manages active chat messages, implicit trajectory tracking, and durable persistence.
	supervisor *supervisor.Supervisor
	// agentBody is the --agent custom agent's Markdown body. Non-empty swaps
	agentBody string
	// agentName is the active markdown agent's name, or "" for the built-in
	agentName string
	// agentWarning is the ⚠ Agent Not Found body when --agent named nothing
	agentWarning string
	// flags is the parsed top-level flag set. Cycle 102: the TUI reuses the
	flags *flag.FlagSet
	// mode is the agent execution mode for this session ("", "accept-edits",
	mode   string
	policy *permissions.Policy
	// activeGoal tracks an active /goal command and its verification loop.
	activeGoal *convstore.GoalState
	// Cycle 292: true on the first user turn of a conversation and again
	wireSettingsPending bool
	// Cycle 106: /rename name (used by conversationTitle) and the
	conversationName     string
	remoteControl        bool
	remoteControlUUID    string
	remoteControlDesktop bool
	// Cycle 250: /narration session state. narrationOff is true after
	narrationOff   bool
	narrationStart time.Time
	// Cycle 167: /goal sets the session goal text; /learn appends to
	goal      string
	learnings []string
	// pendingPermission is a test-only seam for the Blocked / Permission
	pendingPermission bool
	// pendingAuth is the test-only seam for Blocked / Auth — same shape
	pendingAuth bool

	// Session-level Lifecycle Hooks
	// hooks is the lifecycle-hook bus for the TUI session.
	hooks *hooks.Manager
	// hookRT is the session's documented hooks.json runtime
	// (internal/core/policy/hooks). It is built when the first
	// documented event fires, and it holds the documents the session
	// read: the global one and the workspace one.
	hookRT *corehooks.Runtime
	// hookSessionStarted is true once the SessionStart event has
	// fired. The event fires once per session.
	hookSessionStarted bool
	// hookInvocationNum is the number of the invocation the documented
	// events last saw. The first invocation of a session is 0.
	hookInvocationNum int
}

// engineState holds the domain state for engineState.
type engineState struct {
	backend backend.Client
	// backendFor rebuilds the transport after a model change, so the
	backendFor func(model string) backend.Client
	// engine runs the session's turns.
	//
	// The local engine runs them in this process; the remote one runs
	// them on a pigasus server. Both answer with the same frames, so the
	// transcript renders a turn the same way whichever one it is.
	engine       agentturn.Engine
	activeBridge *EngineBridge
	// Cycle tasks-panel: the registry that backs /tasks and the
	taskReg *taskregistry.Registry
	// taskCtrl coordinates task registry subscriptions, dirty tracking, and the task inbox.
	taskCtrl *taskregistry.Controller
	// subagentDefs is the conversation's subagent definition store.
	//
	// The reference scopes a definition to the conversation ("Define a
	// subagent for the duration of this conversation"), so the store
	// belongs to the conversation, not to the turn: the TUI builds the
	// subagent tool set once per turn, and the definitions have to
	// survive that. A resumed conversation gets its definitions back
	// from the trajectory (see internal/app/session/rehydration).
	subagentDefs *subagenttools.Store
	// subagentRegistry is the conversation's live-subagent registry.
	//
	// A delegated subagent runs in the background: the reference's
	// `invoke_subagent` returns a launch acknowledgement and the
	// subagent reports back later, as a message to the parent
	// (【一手】 the 1.4.4 capture, docs/cycles/subagent-async-framing-capture/).
	// The registry is what makes that reachable — the parent messages a
	// live child by its conversation id, a child messages its parent the
	// same way, and `manage_subagents` lists and kills them.
	//
	// It belongs to the conversation, like the definition store: the
	// subagent tool set is rebuilt every turn, and the children have to
	// survive that.
	subagentRegistry *subagenttools.Registry
	// trajLoaded holds the steps of the conversation the run adopted,
	// trajCalls the tool calls of the turns that ran since, and
	// trajSeededLen the number of session messages the adopted
	// conversation contributed.
	//
	// The trajectory is the conversation's durable record — the user
	// inputs, the model responses and the tool calls, in order — and the
	// reference derives the agent's live state from it when the
	// conversation is loaded (【反编译】 the reference's
	// `cortex/rehydration` package; see out/ide-language-server/README.md
	// §10.9). The three fields together rebuild that record on save, so
	// a resumed conversation keeps the tool calls it made — and the ones
	// the reference had stored.
	trajLoaded    []convstore.Step
	trajCalls     []convstore.Step
	trajSeededLen int
	// tasksSubscribed is true after startTasksSubscription()
	tasksSubscribed bool
	// tasksSubCancel is the unsubscribe function returned by
	tasksSubCancel func()
	// tasksDirty is set by the subscriber goroutine on every
	tasksDirty int32
	// taskSubagentManager is the subagent manager the panel
	taskSubagentManager *subagent.Manager
	session             []backend.Message
	// implicitMsgs are the chat messages the cumulative prompt carries
	// from the implicit trajectory — the `!` commands the user ran outside
	// the conversation.
	//
	// They live here and not in m.session: the reference records a `!`
	// command in the implicit trajectory only, and the conversation store
	// holds no record of it (see tui_implicit.go). The cumulative prompt
	// carries the trajectory ahead of the current turn instead.
	implicitMsgs []backend.Message
	// implicitCursor is the number of trajectory steps already converted
	// into implicitMsgs.
	implicitCursor int
	// Cycle 309: the session's queue of task notifications — the
	// messages a background task hands to the model when it finishes.
	//
	// The queue lives on the run mode, not on the agent.Loop: the TUI
	// builds a fresh Loop for every turn (tui_stream_prompt.go), so a
	// per-loop queue would drop every notification that arrives between
	// turns — which is exactly when a background task finishes.
	taskInbox *agentturn.TaskInbox
	// Cycle 229: the manual /compact slash command hooks into PiG's
	compactor manualCompactor
	// catalog is the session's advertised tool registry. The
	// browser_subagent tool needs it: the browser agent's tool set is
	// the browser-control surface the registry carries (cycle 311).
	catalog   *tools.Registry
	response  []string
	streaming bool
	// Cycle 235: cancel function for the in-flight turn's stream
	turnCancel context.CancelFunc
	// turnInterrupted is true when the user cancelled the latest turn with
	// Esc. The reference answers a cancelled turn with a composer hint
	// (agent.InterruptedHintLine) and leaves no error row in the
	// transcript; this flag drives both. It is cleared when the next turn
	// starts.
	turnInterrupted bool
	// engineModelRowsCache caches the model rows discovered from an attached dynamic engine
	// (Codex, Claude, etc.) so rendering /model doesn't perform blocking IPC on every frame.
	engineModelRowsCache []modelpicker.Row
	quitting             bool
	// Cycle 144: two-step Ctrl+C exit confirmation. exitConfirm=true
	exitConfirm   bool
	exitConfirmAt time.Time
	// Cycle 76: streaming spinner. The v1.3.5 binary renders a Braille
	spinner sp.Model
	// Cycle 99: interactive tool calling. tools is the recovered advertised
	tools      []backend.ToolDecl
	toolRunner backend.ToolRunner
	// toolSandbox is the session sandbox installed on the tool gate. A
	toolSandbox sandbox.Sandbox
	// prog is the running *tea.Program. The streaming goroutine uses it to
	prog *tea.Program
	// compactDone carries the result of a generic /compact back to the
	// owner of the runMode when there is no *tea.Program to post it to
	// (the test path; in production the result travels as a tea.Msg and
	// is applied by the Update loop).
	//
	// The worker must not touch m.response or m.session itself: those
	// belong to the UI goroutine, and appending to them from the worker
	// races with every read the UI does.
	compactDone chan genericCompactDoneMsg
	// remoteControlStop stops the in-process remote-control server if one was started.
	remoteControlStop func()
	// inprocessDisp is the in-process remote control dispatcher if active.
	inprocessDisp any
	// turnLock arbitrates turn execution between local TUI and in-process Remote Control.
	turnLock any
	// attachedEngine marks whether the engine is an external/attached engine (ACP/AHP/Driver).
	attachedEngine bool
	// liveSteps accumulates the steps of the turn currently in flight;
	liveSteps  []toolsteps.Step
	liveStart  int
	activeTool string
	// liveStepRuns mirrors liveSteps with transcript coordinates, so
	liveStepRuns []transcript.LiveStepRun
	// Cycle 229: replyStart marks the index in m.response where the model
	replyStart int
	// Cycle 285: liveTextLines is the row count of the current streaming
	liveTextLines int
	liveReplyBuf  string
	// lastStreamSave throttles synchronous session persistence during streaming.
	lastStreamSave time.Time
	// streamedText records whether this turn rendered any text live. When
	streamedText bool
	// liveBatch is the batch id of the calls emitted by the current
	liveBatch  int
	batchBreak bool
	// spinFrame drives the animated spinner label (spinnerText). It
	spinFrame int
	// anyToolRun flips once a call has run in this turn; the reference's
	anyToolRun bool
	// Cycle 229: liveReplyIdx is the index in m.session where the
	liveReplyIdx int
	// Cycle 272 removed glamourThroughIndex (cycle 232) and fnvCache
	thinking            bool
	thinkingSince       time.Time
	liveThinking        string
	liveThinkingSnippet string
	liveThoughtRunIdx   int
	verbosity           string
	// blocks records where each turn's step block lives in m.response so
	blocks []transcript.Block
	// userBlocks records where each user prompt lives in m.response so reflow
	// and live colour scheme changes can re-render it.
	userBlocks []transcript.UserBlock
	// assistants records where each completed assistant markdown reply lives
	// in m.response so a live colour scheme change can re-render it.
	assistants []transcript.AssistantBlock
	// Cycle 292: set when a mid-stream local command (/clear, /new, /fork,
	discardTurn bool
	// Cycle 231: atRouter is the named-subagent registry the
	atRouter *subagentrouter.Manager
	// Cycle 225: /usage quota spinner frame. usagepanel.SpinnerFrame picks
	quotaFrame int
	// streamingSearch gates the cycle-228 streaming path on or off.
	streamingSearch bool
	// Cycle subagent-question-picker: per-subagent pending question
	subAgentQuestions *subagentquestions.Manager
	// Cycle 381: the picker's scope, cursors, staged answer and rows
	// live in the panel; the host keeps the Manager above.
	subAgentQuestionPanel *subagentquestionpanel.Panel
	// Cycle osc-7501: OSC 7501 (Program Status Protocol) state the TUI
	lastTurnOutcome tea.ProgramState
}

// uiState holds the domain state for uiState.
type uiState struct {
	tuiviewport.TextSelection
	// seasonal is the seasonal event the session is themed with, or
	// nil when no event is in season or the user has not opted in.
	//
	// 【一手】 agy 1.4.7: the theme is a settings.json opt-in
	// (`seasonalThemesOptIn`), and the themed banner, the themed
	// prompt-box rules and the moon-phase spinner all follow it.
	seasonal  *seasonal.Event
	altScreen bool
	activeTip string
	// expanded is the ctrl+o toggle for tool-step groups (recovered hints
	expanded            bool
	expandedUserToggled bool
	// Cycle 83: the 3-tab /help TUI menu overlay. Cycle 381: the
	// overlay's tab, cursor and window live in the panel; the host
	// keeps the facts the panel asks for.
	helpPanel *helppanel.Panel
	// Cycle 381: the voice dictation state machine (matching AGY
	// AudioModel and KeyToggleDictation) moved to
	// internal/ui/views/voice — the phase, the trigger, the recorder
	// and the cancel flag live in the surface. The host keeps the
	// transport seam.
	voiceSurface   *voice.Surface
	voiceConnectFn func() error
	// Cycle 105: /statusline and /title toggles. statusLine hides the bottom
	statusLine string
	title      string
	// Cycle status-line-script: AGY 1.1.27 wires statusLine.command into
	statusLineExternal bool
	statusLineCommand  string
	// statusLineCfg is the cached `statusLine` block. The status area is
	// re-rendered on every frame, so the block is read from disk once at
	// startup and refreshed whenever /statusline changes it.
	statusLineCfg statusline.Config
	// titleCfg is the cached `title` block, read the same way.
	titleCfg statusline.Config
	// titleCommand is the configured window-title command.
	titleCommand string
	// titleStore holds the window title the title script produced. The
	// script runs on the tick goroutine and the renderer reads the title
	// on the model's goroutine, so the value needs its own lock.
	titleStore *statusline.TitleStore
	// Cycle 292: transient text in the empty input's placeholder slot. The
	inputNotice string
	// statusLineExternalState is the cached rendered output / tick counter
	statusLineExternalState *statusline.State
	// Cycle 119: terminal geometry and the alt-screen transcript viewport.
	width, height int
	// scroll is the index of the first transcript row shown (the viewport's
	scroll     int
	autoScroll bool
	// Cycle 179: row indices in the scrollable transcript whose first
	userPromptRows []int
	// Cycle 179: the sticky user-prompt bar text. Cached on every render
	stickyBarRow string
	// Cycle 253: AGY 1.3.3 pinned-prompt header. In altscreen mode the
	pinnedPromptRowIdx int
	pinnedPromptText   string
	// vpRows is the transcript viewport's height in rows, recorded on every
	vpRows int
	// Cycle 122: per-width caches of the wrapped m.response rows. The
	rowsCache      wrapSlot
	rowsCacheN1    wrapSlot
	rowsCacheDirty bool
	rowsGen        int
	// Cycle 281: the per-frame layout cache. Composing the scrollable rows
	rowsView composedRows
	// View and scroll caching for sub-microsecond scroll hotpath
	cachedView          tea.View
	viewCacheValid      bool
	cachedScroll        int
	cachedAutoScroll    bool
	cachedW             int
	cachedH             int
	cachedRespLen       int
	cachedRowsGen       int
	cachedPickerVis     bool
	cachedPanelKind     panelregistry.Kind
	cachedStreaming     bool
	cachedExtraRows     int
	cachedExtraLiveRows int
	cachedExtraW        int
	cachedExtraH        int
	extraRowsValid      bool
	cachedHeaderRows    []string
	cachedHeaderKey     headerCacheKey
	// Cycle osc-hyperlinks (cycle 2 of the OSC pair): terminal
	hyperlinkCaps render.HyperlinkCaps
	// Cycle plugin-fsnotify: pluginWatcher is the optional
	pluginWatcher *pluginwatch.Watcher
	// activeItems is the inline active subagents/tasks preview below the
	// input box. The panel owns the block; the host keeps the seams.
	activeItems *activeitemspanel.Panel
	// colorScheme is the name of the colour scheme the session wears.
	//
	// 【一手】 agy 1.4.8 resolves the interface's colours from the
	// selected scheme, so picking a scheme in /config repaints the
	// interface with that scheme's colours. The TUI pushes this name
	// around the work that paints a frame (Update and View), so the
	// frame is painted with the selected scheme's colours.
	colorScheme string
}

// composerState holds the domain state for composerState.
type composerState struct {
	// Cycle 71: input component. charm.land/bubbles/v2 textarea
	input tas.Model
	// Undo / redo stack manager for composer input
	undoMgr *composer.UndoManager
	// Cycle 216: the Vim editing engine, created on first use when the
	vimEng *vim.State
	// vimOn / vimFirstOn mirror the "Editor Mode" and "Editor Mode ›
	vimOn      bool
	vimFirstOn bool
	// Cycle alt-keybinding-option-meta: the user's
	keybindings *keybind.Keybindings
	// altEscBuf holds one buffered bare Esc key while the TUI waits
	altEscBuf keybind.EscBuffer
	// altKeyFires records every alt+letter dispatch in chronological
	altKeyFires []altKeyFire
	// Cycle 383 (B-21): the two-step Esc that clears the composer.
	// escArmedFor holds the composer content the pair was armed on; the
	// empty string means "not armed". The reference clears the prompt box
	// on `esc esc` (official-docs/cli/using.md:22), so the first Esc only
	// arms the clear and the second one performs it.
	escArmedFor string
	// Reference agy: absorb rapid unbracketed Enter key bursts (pasting in terminals
	// lacking bracketed paste support). Spaced < 10ms, consecutive Enters become '\n'.
	lastEnterTime   time.Time
	enterBurstCount int
	// Pasted text blocks folded into placeholders ([Pasted text #N +M lines] or [Pasted text #N M chars]).
	pastedBlocks    map[string]string
	nextPasteID     int
	// Cycle 381: the prompt history is a list
	// (internal/ui/foundations/history). It owns the remembered prompts,
	// the navigation's index and the draft the navigation stashed; the
	// host owns the composer and the persistence to history.jsonl.
	promptHistory history.List
	// Cycle 381: the slash-command completion popup is a panel
	// (internal/ui/views/slashpicker). It owns the filtered rows, the
	// highlight, the scroll window, the on-screen rectangle, the rows
	// it writes and the keys it handles; the host routes the rest.
	picker *slashpicker.Panel
	// Cycle 381: the "@" file-mention dropdown is a panel
	// (internal/ui/views/mentionpicker). It owns the candidate list,
	// the highlight, the mention token's span, the rows it writes and
	// the keys it handles; the host routes the rest.
	atFilePicker *mentionpicker.Panel
	// promptBodyStart is the line index in the pre-trim body
	promptBodyStart int
	promptScreenY   int
	// Cycle 362 (agy 1.4.9): the images the user attached to the next
	// turn. A paste of an image — from the clipboard or from a dropped
	// image file — lands here and rides along with the next user
	// message as an image part.
	pendingMedia  []clipboard.Media
	mediaExpanded bool
	// modeHintDismissed records that the user has edited the input since
	modeHintDismissed bool
}

// overlayState manages all non-primary screen overlays, modals,
// popups, cards, and notices (Side Questions, Modal Panels, Resume Picker,
// Announcements, Eligibility Notices, and Login Dialog).
type overlayState struct {
	// Modal Panels & Pickers
	panel        slashPanel
	panelReg     *panelregistry.Registry
	resumePicker *resumepicker.Picker

	// Side Question (/btw) Cards
	btwManager      *sidequestion.Manager
	btwCards        []*sidequestion.Card
	activeBtwID     string
	activeBtwIndex  int
	activeBtwCancel context.CancelFunc
	btwScroll       int
	asyncBtw        bool

	// Notice & Announcement Cards
	announceQueue     announce.Queue
	eligibilityNotice announce.Notice

	// In-session Login Overlay
	login *loginui.Model
}

// dismissActiveOverlay handles the Escape dismissal chain for overlays and modals.
// Returns handled=true and an optional tea.Cmd if an overlay was dismissed.
func (m *runMode) dismissActiveOverlay() (tea.Model, tea.Cmd, bool) {
	if m.overlays.login != nil {
		if m.overlays.login.State == loginui.StateSelectMethod {
			m.overlays.login = nil
			return m, nil, true
		}
	}
	if m.overlays.panel.kind != panelregistry.None {
		if handled, cmd := m.handlePanelKey(tea.Key{Code: tea.KeyEsc}); handled {
			return m, cmd, true
		}
		return m, m.closePanel(), true
	}
	if m.overlays.resumePicker != nil && m.overlays.resumePicker.Active() {
		cmd := m.overlays.resumePicker.HandleKey(tea.Key{Code: tea.KeyEsc}, m.viewport.height)
		return m, cmd, true
	}
	if m.isBtwCardVisible() {
		m.DismissBtwResponse()
		return m, nil, true
	}
	return m, nil, false
}
func (o *overlayState) HasActiveModal() bool {
	return o.login != nil || o.panel.kind != panelregistry.None || (o.resumePicker != nil && o.resumePicker.Active())
}

// ActiveCard reports whether a floating card (Side Question or Announcement) is currently visible.
func (o *overlayState) ActiveCard() bool {
	return (o.activeBtwIndex >= 0 && o.activeBtwIndex < len(o.btwCards)) || o.announceQueue.Active() != nil
}
