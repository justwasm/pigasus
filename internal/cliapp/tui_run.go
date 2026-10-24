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
	"github.com/justwasm/pigasus/internal/announce"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/convstore"
	"github.com/justwasm/pigasus/internal/hooks"
	"github.com/justwasm/pigasus/internal/keybind"
	"github.com/justwasm/pigasus/internal/loginui"
	"github.com/justwasm/pigasus/internal/permissions"
	"github.com/justwasm/pigasus/internal/pluginwatch"
	"github.com/justwasm/pigasus/internal/render"
	"github.com/justwasm/pigasus/internal/sandbox"
	"github.com/justwasm/pigasus/internal/sidequestion"
	"github.com/justwasm/pigasus/internal/subagent"
	"github.com/justwasm/pigasus/internal/subagentquestions"
	"github.com/justwasm/pigasus/internal/subagentrouter"
	"github.com/justwasm/pigasus/internal/subagenttools"
	"github.com/justwasm/pigasus/internal/taskregistry"
	"github.com/justwasm/pigasus/internal/tuilib"
	"github.com/justwasm/pigasus/internal/vim"
	"github.com/justwasm/pigasus/internal/voice"
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
// runMode is the v1.3.5 binary 1:1 single-prompt TUI.
type runMode struct {
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
	backend     backend.Client
	// backendFor rebuilds the transport after a model change, so the
	backendFor func(model string) backend.Client
	// Cycle 86: per-session conversation UUID shown in the /help
	conversationID string
	// Cycle tasks-panel: the registry that backs /tasks and the
	taskReg *taskregistry.Registry
	// subagentDefs is the conversation's subagent definition store.
	//
	// The reference scopes a definition to the conversation ("Define a
	// subagent for the duration of this conversation"), so the store
	// belongs to the conversation, not to the turn: the TUI builds the
	// subagent tool set once per turn, and the definitions have to
	// survive that. A resumed conversation gets its definitions back
	// from the trajectory (see internal/rehydration).
	subagentDefs *subagenttools.Store
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
	altScreen bool
	// Cycle 174: in-app text selection state. MouseModeCellMotion
	selDragging bool
	selStartRow int
	selStartCol int
	selEndRow   int
	selEndCol   int
	// In-app input field text selection state.
	inputSelDragging bool
	inputSelStart    int // byte offset in m.input.Value()
	inputSelEnd      int // byte offset in m.input.Value()
	session []backend.Message
	// Cycle 229: the manual /compact slash command hooks into PiG's
	compactor manualCompactor
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
	response  []string
	streaming bool
	// Cycle 235: cancel function for the in-flight turn's stream
	turnCancel context.CancelFunc
	// bashMode arms the reference's "!" mode: the composer prompt becomes a
	bashMode bool
	// bashRowIdx is the m.response index of the in-flight bash head row, so
	bashRowIdx int
	// bashCancel cancels the running shell command (Esc). Distinct from
	bashCancel context.CancelFunc
	// bashRun is the running command's output sink; a preemption snapshots
	bashRun *bashOutput
	// bashCmd is the running command's text, needed to rebuild its head row
	bashCmd string
	// bashGen tags every bashDoneMsg; stale generations are ignored.
	bashGen int
	// bashQueue holds commands submitted while a turn streamed. They run
	bashQueue []string
	quitting  bool
	// Cycle 144: two-step Ctrl+C exit confirmation. exitConfirm=true
	exitConfirm   bool
	exitConfirmAt time.Time
	// Cycle 76: streaming spinner. The v1.3.5 binary renders a Braille
	spinner sp.Model
	history     []string
	historyIdx  int
	historyEdit string
	picker tuilib.PickerState
	// Cycle 129: picker scroll offset. The picker renders a fixed
	pickerOffset int
	// feature/at-file-dropdown-newline: the @ file-mention
	atFileDropdown atFileDropdown
	// Cycle 232: picker mouse-click support. pickerLayoutLocal is
	pickerLayoutLocal pickerLayout
	pickerBodyStart   int
	pickerGeom        pickerGeom
	// promptBodyStart is the line index in the pre-trim body
	promptBodyStart int
	promptScreenY   int
	// Cycle 99: interactive tool calling. tools is the recovered advertised
	tools      []backend.ToolDecl
	toolRunner agent.ToolRunner
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
	liveSteps  []toolStep
	liveStart  int
	activeTool string
	// liveStepRuns mirrors liveSteps with transcript coordinates, so
	liveStepRuns []liveStepRun
	// Cycle 229: replyStart marks the index in m.response where the model
	replyStart int
	// Cycle 285: liveTextLines is the row count of the current streaming
	liveTextLines int
	liveReplyBuf  string
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
	thinking      bool
	thinkingSince time.Time
	// expanded is the ctrl+o toggle for tool-step groups (recovered hints
	expanded bool
	// blocks records where each turn's step block lives in m.response so
	blocks []turnBlock
	// Cycle 83: 3-tab /help TUI menu overlay. When helpMode is true,
	helpMode bool
	helpTab  int // 0/1/2
	// Voice dictation state (matching AGY AudioModel and KeyToggleDictation).
	voiceMode      bool
	voicePhase     int
	voiceTrigger   string
	voiceRecorder  *voice.Recorder
	voiceCancel    bool
	voiceConnectFn func() error
	// Cycle 185: modal slash-command panel. The reference renders
	panel slashPanel
	// Cycle 182: /help list navigation state. The reference renders an
	helpWindow int
	helpCursor int
	// Cycle 167: /resume picker state. When resumeMode is true,
	resumeMode   bool
	resumeList   []convstore.ConversationMeta
	resumeCursor int
	// Cycle 184: the v1.4.0 picker's tab / search / window state.
	resumeTab    int
	resumeQuery  string
	resumeWindow int
	// resumeCursorOn is the blink phase of the search cursor. REF blinks
	resumeCursorOn bool
	// flags is the parsed top-level flag set. Cycle 102: the TUI reuses the
	flags *flag.FlagSet
	// mode is the agent execution mode for this session ("", "accept-edits",
	mode   string
	policy *permissions.Policy
	// Cycle 105: /statusline and /title toggles. statusLine hides the bottom
	statusLine string
	title      string
	// Cycle status-line-script: AGY 1.1.27 wires statusLine.command into
	statusLineExternal bool
	statusLineCommand  string
	// Cycle status-line-script: per-session token totals accumulated from
	sessionPromptTokens     int64
	sessionCompletionTokens int64
	// Cycle 292: the completion count the *last* streamed step reported.
	lastTurnCompletionTokens int64
	// Cycle 292: true on the first user turn of a conversation and again
	wireSettingsPending bool
	// Cycle 292: set when a mid-stream local command (/clear, /new, /fork,
	discardTurn bool
	// Cycle 292: transient text in the empty input's placeholder slot. The
	inputNotice string
	// statusLineExternalState is the cached rendered output / tick counter
	statusLineExternalState *statusLineExternalState
	// Cycle 106: /rename name (used by conversationTitle) and the
	conversationName string
	remoteControl    bool
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
	btwExpanded     map[string]bool
	// Cycle btw-runs-hooks: lifecycle-hook bus for the TUI session.
	hooks *hooks.Manager
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
	// Cycle 225: /usage quota spinner frame. usageSpinnerFrame picks
	quotaFrame int
	// Cycle accounts-tier-usage: the session's quota cache. The
	// /accounts panel fetches every account's quota and the /usage
	// panel reuses the active account's entry, so opening one after
	// the other does not query the same account twice.
	quotaCache map[string]quotaCacheEntry
	// Cycle 227: auto-compact spinner frame. compactSpinnerFrame
	compactSpinnerFrame int
	compactStart        time.Time
	compactEnd          time.Time
	// compactStartTokens / compactEndTokens are the upstream-reported
	compactTokensBefore int
	compactTokensAfter  int
	// compactReason is the upstream "manual" | "threshold" | "overflow"
	compactReason string
	// Cycle 228 (codesearch-panel): csCancel is the cancel function
	csCancel context.CancelFunc
	// streamingSearch gates the cycle-228 streaming path on or off.
	streamingSearch bool
	// feature/announcement-cards: in-TUI announcement card queue.
	announceVisible         []announce.Card
	announceSessionHiddenID string
	announceSaveError       error
	// Cycle 262: eligibility-error inline notice. The value is set
	eligibilityNotice eligibilityNotice
	// Cycle subagent-question-picker: per-subagent pending question
	subAgentQuestions *subagentquestions.Manager
	// Cycle 244: AGY 1.4.0 "Queued Messages" semantics. The user's
	queueMode bool
	// Cycle 244: the prompt the user submitted while in queue
	queuedPrompt string
	// streamQueue holds prompts submitted with Enter while a turn is
	streamQueue []string
	// modeHintDismissed records that the user has edited the input since
	modeHintDismissed bool
	// subAgentQuestionsActive is the ID the picker is currently
	subAgentQuestionsActive string
	// subAgentQuestionPickerOpen is true while the question
	subAgentQuestionPickerOpen bool
	// subAgentQuestionPickerCursor is the highlighted row index
	subAgentQuestionPickerCursor int
	// subAgentQuestionPickerOptionCursor is the highlighted option
	subAgentQuestionPickerOptionCursor int
	// subAgentQuestionAlert is the most recent alert message
	subAgentQuestionAlert string
	// subAgentQuestionAnswerBuffer is the in-flight freeform
	subAgentQuestionAnswerBuffer string
	// subAgentQuestionAnswerTarget is the question ID the
	subAgentQuestionAnswerTarget string
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
}
	// NewRunMode constructs the v1.3.5 binary 1:1 run mode.
func (m *runMode) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if mod, cmd, handled := m.updateLogin(msg); handled {
		return mod, cmd
	}
	if !isScrollMsg(msg) {
		m.rowsCacheDirty = true
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


