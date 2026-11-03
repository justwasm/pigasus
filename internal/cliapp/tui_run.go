// tui_run.go — cycle 71 v1.3.5 binary 1:1 TUI (24-bit RGB logo +
// charm.land/bubbles/v2 textarea input component).
//
// user live run 反馈 (cycle 47): "tui 细节不一致, 自己在 tmux 里面对比两者
// tui 交互, 个别体验不一样, close the gap".
//
// user 反馈 (cycle 71): "这个输入组件的结构还是和 agy 不太一样, 它是放在
// 两条横线中间的, 而且是可以扩充大小的, 而当前实现不支持 ctrl+j 多行输入
// 而且光标的位置还是没有跟随输入的文字"
// → 之前是手写 []rune 数组: 单行, ctrl+j 不响应, 光标 X 用 1+len(buffer)
//
//	估算 (multi-byte rune / multi-line 都错位). 本 commit 改:
//	m.input 是 charm.land/bubbles/v2/textarea.Model, MaxHeight=8 +
//	DynamicHeight=true, 自带 ctrl+j / alt+←→ / ctrl+w / ctrl+a/e 等全套
//	编辑键, m.input.Cursor() 精确给出 v.Cursor.X/Y. prompt 行包在两条
//	──── 之间, 跟 /tmp/agy_real_idle.txt /tmp/agy_multiline.txt 1:1.
package cliapp

import (
	"context"
	"flag"
	"time"

	sp "charm.land/bubbles/v2/spinner"
	tas "charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/ui/foundations/history"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	"github.com/justwasm/pigasus/internal/ui/views/activeitemspanel"
	"github.com/justwasm/pigasus/internal/ui/views/announce"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/app/tuistore"
	subagent "github.com/justwasm/pigasus/internal/app/subagent/core"
	subagentquestions "github.com/justwasm/pigasus/internal/app/subagent/questions"
	subagentrouter "github.com/justwasm/pigasus/internal/app/subagent/router"
	"github.com/justwasm/pigasus/internal/ui/views/subagentquestionpanel"
	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/settings"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	corehooks "github.com/justwasm/pigasus/internal/core/policy/hooks"
	"github.com/justwasm/pigasus/internal/core/policy/sandbox"
	"github.com/justwasm/pigasus/internal/features/plugins/hooks"
	pluginwatch "github.com/justwasm/pigasus/internal/features/plugins/watch"
	"github.com/justwasm/pigasus/internal/features/tools/clipboard"
	"github.com/justwasm/pigasus/internal/ui/views/helppanel"
	"github.com/justwasm/pigasus/internal/ui/views/loginui"
	"github.com/justwasm/pigasus/internal/ui/views/modelpicker"
	"github.com/justwasm/pigasus/internal/ui/views/sidequestion"
	"github.com/justwasm/pigasus/internal/app/subagenttools"
	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/internal/ui/resumepicker"
	"github.com/justwasm/pigasus/internal/ui/views/mentionpicker"
	"github.com/justwasm/pigasus/internal/ui/views/slashpicker"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	"github.com/justwasm/pigasus/internal/ui/transcript"
	"github.com/justwasm/pigasus/internal/ui/editor/keybind"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/tuimodel"
	"github.com/justwasm/pigasus/internal/ui/views/seasonal"
	"github.com/justwasm/pigasus/internal/ui/views/statusline"
	"github.com/justwasm/pigasus/internal/ui/views/voice"
	"github.com/justwasm/pigasus/palette"
	"github.com/justwasm/pigasus/render"
	"github.com/justwasm/pigasus/vim"
)

// logoLines are the four *text* rows of the v1.3.5 binary 1:1 banner —
// the colour matrix that forms the left side of each row now lives in
// logo_palette.go (the logoArt matrix plus the renderLogoLines
// renderer).
//
// Cycle 126: row 1 ("Antigravity CLI <ver>") uses 24-bit RGB
// (38;2;66;133;244) plus bold, matching the ref binary's literal
// output (`script -q /tmp/agycmp/ref-color.txt agy`). Rows 2-4
// (account, model, cwd) use 24-bit RGB (38;2;95;99;104) instead of
// the 256-colour 241 the clone previously emitted. Cycle 88 (and
// later cycles) was correctly paired with the ref binary's escape
// sequence *of that build* (some ref builds sampled through tmux
// emit 256-colour and some emit 24-bit; both decode to the same
// visual shade in tmux's 256-colour cube, but a real terminal
// distinguishes them). The 1.3.6 binary on this machine emits
// 24-bit RGB to a TTY, so the clone now matches.
//
// Cycle 290: the per-cell ANSI sequences used to live inline here as
// `logoBlock` plus a hardcoded `logoLines[4]`. Both were moved to
// logo_palette.go as a 5-row palette-indexed logoArt matrix plus a
// renderer, so this file now only formats the text half of each
// banner row. The `%s` placeholders receive the rendered art row
// from `renderLogoRow(logoArt[i])` (16 cols wide), and 2 spaces pad
// the gutter so banner text begins at column 18.
var logoLines = []string{
	"%s  " + cBlueBoldOpen + "Pigasus CLI " + cliVersion + "\x1b[0m",
	"%s  " + cDimGrayOpen + "%s" + cDimGrayClose,
	"%s  " + cDimGrayOpen + "%s" + cDimGrayClose,
	"%s  " + cDimGrayOpen + "%s" + cDimGrayClose,
}

// separatorWidth is the v1.3.5 binary 1:1 horizontal separator
// width. Cycle 80: ref capture showed 220 chars (cycle 78 used 200
// which was wrong). The v1.3.5 binary renders two full-width
// horizontal rules around the input box — matching the terminal width
// of 220 cols when captured in a 220-col tmux pane.
const separatorWidth = 220

// helpCommandsPageSize is the historical fixed /help window size. The
// live value is computed per terminal height by helpListWindowFor in
// history.go (18 rows at the 30-row geometry). Kept only so the
// cycle-132 constant has a named home; do not add new uses.
const helpCommandsPageSize = 18

// runMode is the v1.3.5 binary 1:1 single-prompt TUI.
type runMode struct {
	tuimodel.Base

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
	// seasonal is the seasonal event the session is themed with, or
	// nil when no event is in season or the user has not opted in.
	//
	// 【一手】 agy 1.4.7: the theme is a settings.json opt-in
	// (`seasonalThemesOptIn`), and the themed banner, the themed
	// prompt-box rules and the moon-phase spinner all follow it.
	seasonal *seasonal.Event
	backend  backend.Client
	// backendFor rebuilds the transport after a model change, so the
	backendFor func(model string) backend.Client
	// engine runs the session's turns.
	//
	// The local engine runs them in this process; the remote one runs
	// them on a pigasus server. Both answer with the same frames, so the
	// transcript renders a turn the same way whichever one it is.
	engine agentturn.Engine
	// Cycle 86: per-session conversation UUID shown in the /help
	conversationID string
	// initialConversationID is the UUID generated at runMode creation.
	// Used to distinguish a brand new unstarted session from an active conversation.
	initialConversationID string
	conversationStarted   bool
	// Cycle tasks-panel: the registry that backs /tasks and the
	taskReg *taskregistry.Registry
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
	// Cycle resume-continuation: auto-generated conversation title
	autoTitle string
	// Cycle 81: when the configured model (settings.json's
	fallbackFrom string
	fallbackTo   string
	altScreen    bool
	// Cycle 382: extracted session state (selection, bash mode)
	store tuistore.Store
	session          []backend.Message
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
	taskInbox *agent.TaskInbox
	// Cycle 229: the manual /compact slash command hooks into PiG's
	compactor manualCompactor
	// catalog is the session's advertised tool registry. The
	// browser_subagent tool needs it: the browser agent's tool set is
	// the browser-control surface the registry carries (cycle 311).
	catalog *tools.Registry
	// Cycle 71: input component. charm.land/bubbles/v2 textarea
	input tas.Model
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
	response    []string
	streaming   bool
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
	quitting        bool
	// Cycle 144: two-step Ctrl+C exit confirmation. exitConfirm=true
	exitConfirm   bool
	exitConfirmAt time.Time
	// Cycle 383 (B-21): the two-step Esc that clears the composer.
	// escArmedFor holds the composer content the pair was armed on; the
	// empty string means "not armed". The reference clears the prompt box
	// on `esc esc` (official-docs/cli/using.md:22), so the first Esc only
	// arms the clear and the second one performs it.
	escArmedFor string
	// Cycle 76: streaming spinner. The v1.3.5 binary renders a Braille
	spinner sp.Model
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
	// Cycle 99: interactive tool calling. tools is the recovered advertised
	tools      []backend.ToolDecl
	toolRunner backend.ToolRunner
	// agentBody is the --agent custom agent's Markdown body. Non-empty swaps
	agentBody string
	// agentName is the active markdown agent's name, or "" for the built-in
	agentName string
	// toolSandbox is the session sandbox installed on the tool gate. A
	toolSandbox sandbox.Sandbox
	// agentWarning is the ⚠ Agent Not Found body when --agent named nothing
	agentWarning string
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
	activeTip           string
	// expanded is the ctrl+o toggle for tool-step groups (recovered hints
	expanded            bool
	expandedUserToggled bool
	verbosity           string
	// blocks records where each turn's step block lives in m.response so
	blocks []transcript.Block
	// userBlocks records where each user prompt lives in m.response so reflow
	// and live colour scheme changes can re-render it.
	userBlocks []transcript.UserBlock
	// assistants records where each completed assistant markdown reply lives
	// in m.response so a live colour scheme change can re-render it.
	assistants []transcript.AssistantBlock
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
	// Cycle 185: modal slash-command panel. The reference renders
	panel    slashPanel
	panelReg *panelregistry.Registry
	// Cycle 167: the /resume picker. The picker owns its own state — the
	// list, the cursor, the search query and the workspace tree — and the
	// session reaches it through the seams it was built with.
	resumePicker *resumepicker.Picker
	// flags is the parsed top-level flag set. Cycle 102: the TUI reuses the
	flags *flag.FlagSet
	// mode is the agent execution mode for this session ("", "accept-edits",
	mode   string
	policy *permissions.Policy
	// activeGoal tracks an active /goal command and its verification loop.
	activeGoal *convstore.GoalState
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
	// Cycle 292: true on the first user turn of a conversation and again
	wireSettingsPending bool
	// Cycle 292: set when a mid-stream local command (/clear, /new, /fork,
	discardTurn bool
	// Cycle 292: transient text in the empty input's placeholder slot. The
	inputNotice string
	// Cycle 362 (agy 1.4.9): the images the user attached to the next
	// turn. A paste of an image — from the clipboard or from a dropped
	// image file — lands here and rides along with the next user
	// message as an image part.
	pendingMedia  []clipboard.Media
	mediaExpanded bool
	// statusLineExternalState is the cached rendered output / tick counter
	statusLineExternalState *statusline.State
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
	// Cycle 230: /btw side-question cards. btwManager is constructed
	btwManager      *sidequestion.Manager
	btwCards        []*sidequestion.Card
	activeBtwID     string
	activeBtwIndex  int
	activeBtwCancel context.CancelFunc
	btwScroll       int
	asyncBtw        bool
	// Cycle btw-runs-hooks: lifecycle-hook bus for the TUI session.
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
	// Cycle 231: atRouter is the named-subagent registry the
	atRouter *subagentrouter.Manager
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
	cachedView       tea.View
	viewCacheValid   bool
	cachedScroll     int
	cachedAutoScroll bool
	cachedW          int
	cachedH          int
	cachedRespLen    int
	cachedRowsGen    int
	cachedPickerVis  bool
	cachedPanelKind  panelregistry.Kind
	cachedExtraRows  int
	cachedExtraW     int
	cachedExtraH     int
	extraRowsValid   bool
	cachedHeaderRows []string
	cachedHeaderKey  headerCacheKey
	// Cycle 225: /usage quota spinner frame. usagepanel.SpinnerFrame picks
	quotaFrame int
	// streamingSearch gates the cycle-228 streaming path on or off.
	streamingSearch bool
	// feature/announcement-cards: in-TUI announcement card queue. Cycle
	// 381: the queue's state lives in the announce package; the host
	// drives it (load, dismiss, hide-for-session) and paints the head
	// card between the separator and the prompt.
	announceQueue announce.Queue
	// Cycle 262: eligibility-error inline notice. The value is set
	// by the chunk handler and cleared when the user types.
	eligibilityNotice announce.Notice
	// Cycle subagent-question-picker: per-subagent pending question
	subAgentQuestions *subagentquestions.Manager
	// modeHintDismissed records that the user has edited the input since
	modeHintDismissed bool
	// Cycle 381: the picker's scope, cursors, staged answer and rows
	// live in the panel; the host keeps the Manager above.
	subAgentQuestionPanel *subagentquestionpanel.Panel
	// Cycle osc-7501: OSC 7501 (Program Status Protocol) state the TUI
	lastTurnOutcome tea.ProgramState
	// pendingPermission is a test-only seam for the Blocked / Permission
	pendingPermission bool
	// pendingAuth is the test-only seam for Blocked / Auth — same shape
	pendingAuth bool
	// In-session login model (when /login is triggered or auth is required).
	login *loginui.Model
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

type headerCacheKey struct {
	width        int
	seasonal     *seasonal.Event
	email        string
	model        string
	tier         string
	cwd          string
	agentWarning string
	fallbackFrom string
	fallbackTo   string
}

func (m *runMode) invalidateViewCache() {
	m.viewCacheValid = false
	m.extraRowsValid = false
}

func (m *runMode) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// The palette's colours follow the selected scheme while the
	// update runs: the rows this update paints (the streamed text, the
	// tool rows, the panels) wear the scheme's colours.
	restore := palette.PushScheme(m.colorScheme)
	defer restore()
	if mod, cmd, handled := m.updateLogin(msg); handled {
		return mod, cmd
	}
	if !tuiviewport.IsScrollMsg(msg) {
		m.rowsCacheDirty = true
		m.invalidateViewCache()
	}
	if mod, cmd, handled := m.updateMouseAndMiscMsg(msg); handled {
		return mod, cmd
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.updateKeyMsg(msg)
	default:
		if mod, cmd, handled := m.updateStreamingMsg(msg); handled {
			return mod, cmd
		}
		if mod, cmd, handled := m.updateTicksAndCompactMsg(msg); handled {
			return mod, cmd
		}
	}
	return m, nil
}

// currentVerbosity returns the active verbosity level.
func (m *runMode) currentVerbosity() toolsteps.Verbosity {
	if m == nil || m.verbosity == "" {
		v := settings.String("verbosity")
		if v == "" {
			return toolsteps.VerbosityMedium
		}
		return toolsteps.ParseVerbosity(v)
	}
	return toolsteps.ParseVerbosity(m.verbosity)
}

// effectiveExpanded returns whether tool-step blocks should be drawn expanded
// under the current verbosity setting and user ctrl+o toggle.
func (m *runMode) effectiveExpanded(verb toolsteps.Verbosity) bool {
	if m == nil {
		return false
	}
	if m.expandedUserToggled {
		return m.expanded
	}
	return verb == toolsteps.VerbosityHigh
}

func (m *runMode) isExpanded() bool {
	return m.effectiveExpanded(m.currentVerbosity())
}

var _ tuimodel.Model = (*runMode)(nil)
