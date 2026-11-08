// Package tui - substates.go: encapsulates the sub-state domains of RootModel.
//
// The TUI state machine is decomposed into cohesive sub-state domains:
//   - sessionState:  identities, credentials, active conversation metadata & config
//   - uiState:       viewport geometry, theme, tool step expansion & voice surface
//   - PromptModel:   text input buffer, history cursor, auto-complete & attachments
//   - OverlayModel:  modal panels, side-question cards, announcements & dialogs
//   - DisplayModel:  quit/exit flags, verbosity and the other display state
//   - CommandsModel: the slash-command dispatcher
package tui

import (
	"flag"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/app/session"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/app/tuistore"
	corehooks "github.com/justwasm/pigasus/internal/core/policy/hooks"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	"github.com/justwasm/pigasus/internal/features/plugins/hooks"
	pluginwatch "github.com/justwasm/pigasus/internal/features/plugins/watch"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/views/seasonal"
	"github.com/justwasm/pigasus/render"
)

// sessionState holds the domain state for sessionState.
type sessionState struct {
	// domain encapsulates the pure-Go session domain entity (identity, configuration, persistence).
	domain *session.Session
	email  string
	model  string
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
	// remoteAgent marks a `--remote-agent` session: turns run on the
	// remote agent proxy and the banner names the remote workspace.
	remoteAgent bool
	// Cycle 86: per-session conversation UUID shown in the /help
	conversationID string
	// initialConversationID is the UUID generated at RootModel creation.
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
	// Cycle 105: /statusline and /title toggles. statusLine hides the bottom
	// Cycle status-line-script: AGY 1.1.27 wires statusLine.command into
	// statusLineCfg is the cached `statusLine` block. The status area is
	// re-rendered on every frame, so the block is read from disk once at
	// startup and refreshed whenever /statusline changes it.
	// titleCfg is the cached `title` block, read the same way.
	// titleCommand is the configured window-title command.
	// titleStore holds the window title the title script produced. The
	// script runs on the tick goroutine and the renderer reads the title
	// on the model's goroutine, so the value needs its own lock.
	// Cycle 292: transient text in the empty input's placeholder slot. The
	inputNotice string
	// statusLineExternalState is the cached rendered output / tick counter
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
	// Cycle 122: per-width caches of the wrapped m.conversation.response rows. The
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
	// colorScheme is the name of the colour scheme the session wears.
	//
	// 【一手】 agy 1.4.8 resolves the interface's colours from the
	// selected scheme, so picking a scheme in /config repaints the
	// interface with that scheme's colours. The TUI pushes this name
	// around the work that paints a frame (Update and View), so the
	// frame is painted with the selected scheme's colours.
	colorScheme string
}
