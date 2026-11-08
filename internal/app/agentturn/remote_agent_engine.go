// remote_agent_engine.go — the Engine half of `pigasus --remote-agent`.
//
// The reference's `agy --remote-agent` is a thin client of the
// google.gca.aicode.v1main.JetskiRemoteAgentProxyService: it starts a cascade,
// sends the user's message, and renders the state-update SSE stream. This
// engine does the same for the clone's TUI, translating the remote trajectory
// steps back into the same agentlink frames the local engine produces, so the
// transcript, tool rows and status line are unchanged.
package agentturn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/features/models/catalog"
	"github.com/justwasm/pigasus/internal/features/tools/skills"
	"github.com/justwasm/pigasus/internal/infra/remoteagentclient"
)

// RemoteAgentConfig configures a RemoteAgentEngine.
type RemoteAgentConfig struct {
	// Client is the proxy client.
	Client *remoteagentclient.Client
	// Model is the Pigasus-side model the remote turn asks for.
	Model string
	// Workspace is the local workspace sent as the cascade's workspace URI.
	Workspace string
	// Project is the --project value (id or title). Empty leaves the
	// server's default project.
	Project string
}

// RemoteAgentEngine runs turns through a remote agent proxy.
type RemoteAgentEngine struct {
	client    *remoteagentclient.Client
	model     string
	workspace string
	project   string

	mu       sync.Mutex
	cascade  string
	stepBase int
	ensured  bool
	closed   bool
	cancel   context.CancelFunc
	turnSeq  uint64

	// codeSteps maps a workspace-relative path to the latest code-action
	// or file-change step that touched it. getCodeActionDiff is step
	// scoped, so the diff panel needs this to ask for a file's block.
	codeMu    sync.Mutex
	codeSteps map[string]int

	// interaction answers a WAITING step (ask_question / permission).
	// The TUI sets it; nil answers with a cancellation so a headless
	// session never leaves the remote turn parked.
	interactionMu sync.Mutex
	interaction   RemoteInteractionHandler

	// commands caches the remote slash-command menu. The TUI asks for it
	// while building the menu, so a failed or slow proxy must not make
	// every keystroke hit the network.
	commandsMu     sync.Mutex
	commands       []CommandInfo
	commandsLoaded bool

	// skills caches the remote ListCustomizations skills. The slash menu
	// and /skills both read it; a reload clears it.
	skillsMu     sync.Mutex
	skills       []skills.SkillInfo
	skillsLoaded bool
}

// NewRemoteAgentEngine builds the engine. It does no network I/O until the
// first turn.
func NewRemoteAgentEngine(cfg RemoteAgentConfig) *RemoteAgentEngine {
	return &RemoteAgentEngine{
		client:    cfg.Client,
		model:     strings.TrimSpace(cfg.Model),
		workspace: strings.TrimSpace(cfg.Workspace),
		project:   strings.TrimSpace(cfg.Project),
	}
}

// DefaultModel reports the model the engine asks the remote agent for.
func (e *RemoteAgentEngine) DefaultModel() string { return e.model }

// CascadeID returns the remote conversation this engine is attached to.
func (e *RemoteAgentEngine) CascadeID() string {
	if e == nil {
		return ""
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cascade
}

// ErrNoRemoteConversation reports a conversation id the proxy does not know
// (or one that carries no steps).
var ErrNoRemoteConversation = errors.New("remote agent: no such conversation")

// ResumeCascade attaches the engine to an existing remote conversation.
// The next turn sends into it instead of starting a new cascade, and the
// steps already on the server are not replayed as new frames.
func (e *RemoteAgentEngine) ResumeCascade(ctx context.Context, id string) (int, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return 0, ErrNoRemoteConversation
	}
	conv, err := e.client.GetConversation(ctx, id)
	if err != nil {
		return 0, err
	}
	steps := trajectorySteps(conv.Update)
	if len(steps) == 0 {
		return 0, ErrNoRemoteConversation
	}
	e.mu.Lock()
	e.cascade = id
	e.stepBase = len(steps)
	e.ensured = true
	e.mu.Unlock()
	return len(steps), nil
}

// ResetSession drops the remote cascade so the next turn starts a new
// conversation. The host calls it from /clear and /new.
func (e *RemoteAgentEngine) ResetSession() {
	e.mu.Lock()
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	e.cascade = ""
	e.stepBase = 0
	e.mu.Unlock()
}

// Close cancels the active turn.
func (e *RemoteAgentEngine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closed = true
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	return nil
}

// AvailableCommands lists the remote agent's slash commands. A failure is
// silent: the menu falls back to the local catalogue.
func (e *RemoteAgentEngine) AvailableCommands() []CommandInfo {
	if e == nil || e.client == nil {
		return nil
	}
	e.commandsMu.Lock()
	if e.commandsLoaded {
		out := append([]CommandInfo(nil), e.commands...)
		e.commandsMu.Unlock()
		return out
	}
	e.commandsMu.Unlock()

	// A menu fetch is best-effort: keep it short so an unreachable proxy
	// does not stall the slash picker.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmds, err := e.client.ListSlashCommands(ctx)
	if err != nil {
		return nil
	}
	out := make([]CommandInfo, 0, len(cmds))
	for _, c := range cmds {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			continue
		}
		if !strings.HasPrefix(name, "/") {
			name = "/" + name
		}
		out = append(out, CommandInfo{Name: name, Description: c.Description})
	}
	e.commandsMu.Lock()
	e.commands = append([]CommandInfo(nil), out...)
	e.commandsLoaded = true
	e.commandsMu.Unlock()
	return out
}

// Turn runs one remote turn and streams its frames.
func (e *RemoteAgentEngine) Turn(ctx context.Context, t Turn) (<-chan agentlink.Frame, error) {
	if e == nil || e.client == nil {
		return nil, errors.New("remote agent: no client configured")
	}
	prompt := lastUserPrompt(t.History)
	if prompt == "" {
		return nil, errors.New("remote agent: empty prompt")
	}
	// The TUI's /model writes the display label into the session; the
	// proxy expects the catalogue id ("gemini-3.8-flash-high"), not
	// "Gemini 3.8 Flash (High)".
	model := wireModel(t.Model)
	if model == "" {
		model = wireModel(e.model)
	}

	turnCtx, cancel := context.WithCancel(ctx)
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		cancel()
		return nil, errors.New("remote agent: engine is closed")
	}
	e.turnSeq++
	seq := e.turnSeq
	e.cancel = cancel
	e.mu.Unlock()

	out := make(chan agentlink.Frame, 64)
	go func() {
		defer close(out)
		defer func() {
			e.mu.Lock()
			if e.turnSeq == seq {
				e.cancel = nil
			}
			e.mu.Unlock()
			cancel()
		}()
		e.runTurn(turnCtx, model, prompt, out)
	}()
	return out, nil
}

// lastUserPrompt returns the newest user message in the turn history.
func lastUserPrompt(history []agentlink.Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == "user" && strings.TrimSpace(history[i].Content) != "" {
			return history[i].Content
		}
	}
	return ""
}

// runTurn performs the start/send + SSE loop and writes frames to out.
func (e *RemoteAgentEngine) runTurn(ctx context.Context, model, prompt string, out chan<- agentlink.Frame) {
	e.mu.Lock()
	cascade := e.cascade
	base := e.stepBase
	e.mu.Unlock()

	if cascade == "" {
		if _, err := e.client.ResolveProject(ctx, e.project); err != nil {
			e.emitError(out, err)
			return
		}
		e.mu.Lock()
		ensured := e.ensured
		e.mu.Unlock()
		if !ensured {
			if _, err := e.client.EnsureEnvironment(ctx, ""); err != nil {
				e.emitError(out, err)
				return
			}
			e.mu.Lock()
			e.ensured = true
			e.mu.Unlock()
		}
		id, err := e.client.StartCascade(ctx, model, workspaceURI(e.workspace), prompt)
		if err != nil {
			e.emitError(out, err)
			return
		}
		e.mu.Lock()
		e.cascade = id
		e.stepBase = 0
		e.mu.Unlock()
		cascade = id
		base = 0
	} else if err := e.client.SendMessage(ctx, cascade, prompt); err != nil {
		e.emitError(out, err)
		return
	}

	updates, err := e.client.Stream(ctx, cascade)
	if err != nil {
		e.emitError(out, err)
		return
	}

	var (
		reply     strings.Builder
		reasoning strings.Builder
		steps     = make(map[int]*remoteStepState)
		maxIndex  = -1
	)
	emitText := func(s string) {
		if s != "" {
			reply.WriteString(s)
			select {
			case out <- agentlink.Frame{Kind: agentlink.KindText, Text: s}:
			case <-ctx.Done():
			}
		}
	}
	emitThought := func(s string) {
		if s != "" {
			reasoning.WriteString(s)
			select {
			case out <- agentlink.Frame{Kind: agentlink.KindThought, Text: s}:
			case <-ctx.Done():
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			e.cancelRemote(cascade)
			return
		case update, ok := <-updates:
			if !ok {
				// The stream ended without a final idle frame: report what
				// arrived, advance past these steps, and end the turn.
				e.mu.Lock()
				if maxIndex+1 > e.stepBase {
					e.stepBase = maxIndex + 1
				}
				e.mu.Unlock()
				e.finish(out, reply.String(), reasoning.String())
				return
			}
			if update.Update == nil {
				continue
			}
			if msg := trajectoryError(update.Update); msg != "" {
				select {
				case out <- agentlink.Frame{Kind: agentlink.KindResult, Error: msg}:
				case <-ctx.Done():
				}
				return
			}
			list := trajectorySteps(update.Update)
			for i, raw := range list {
				if i < base {
					continue
				}
				if i > maxIndex {
					maxIndex = i
				}
				e.mapStep(ctx, cascade, i, raw, steps, emitText, emitThought, out)
			}
			if idle, _ := update.Update["fullyIdle"].(bool); idle {
				e.mu.Lock()
				if maxIndex+1 > e.stepBase {
					e.stepBase = maxIndex + 1
				}
				e.mu.Unlock()
				e.finish(out, reply.String(), reasoning.String())
				return
			}
		}
	}
}

// remoteStepState tracks how much of one remote step has already been emitted.
type remoteStepState struct {
	reply          string
	thought        string
	toolStarted    bool
	toolEnded      bool
	waitingHandled bool
}

// mapStep translates one remote Cortex step into agentlink frames.
func (e *RemoteAgentEngine) mapStep(
	ctx context.Context,
	cascade string,
	index int,
	raw any,
	seen map[int]*remoteStepState,
	emitText func(string),
	emitThought func(string),
	out chan<- agentlink.Frame,
) {
	step, ok := raw.(map[string]any)
	if !ok {
		return
	}
	typ := stepType(step["type"])
	status := stepStatus(step["status"])
	state := seen[index]
	if state == nil {
		state = &remoteStepState{}
		seen[index] = state
	}

	// A code action / file change names the file it touched; the diff
	// panel asks getCodeActionDiff by that step.
	if path := e.codeActionPath(step); path != "" {
		e.codeMu.Lock()
		if e.codeSteps == nil {
			e.codeSteps = make(map[string]int)
		}
		e.codeSteps[path] = index
		e.codeMu.Unlock()
	}

	// A WAITING step is parked on the user. Publish it once and send the
	// answer through HandleCascadeUserInteraction.
	if req, ok := waitingInteraction(step, cascade, index); ok {
		if !state.waitingHandled {
			state.waitingHandled = true
			e.answerInteraction(ctx, req, out)
		}
		return
	}

	switch typ {
	case 15: // CORTEX_STEP_TYPE_PLANNER_RESPONSE
		planner, _ := step["plannerResponse"].(map[string]any)
		thought := stringField(planner, "thinking")
		if len(thought) > len(state.thought) && strings.HasPrefix(thought, state.thought) {
			emitThought(thought[len(state.thought):])
			state.thought = thought
		}
		response := stringField(planner, "response")
		if len(response) > len(state.reply) && strings.HasPrefix(response, state.reply) {
			emitText(response[len(state.reply):])
			state.reply = response
		}
	case 21: // CORTEX_STEP_TYPE_RUN_COMMAND
		run, _ := step["runCommand"].(map[string]any)
		cmd := stringField(run, "commandLine")
		if cmd == "" {
			cmd = stringField(run, "command")
		}
		args, _ := json.Marshal(map[string]any{"CommandLine": cmd})
		if !state.toolStarted {
			select {
			case out <- agentlink.Frame{Kind: agentlink.KindTool, Tool: &agentlink.Tool{
				Phase: agentlink.ToolStart,
				Name:  "run_command",
				Args:  string(args),
			}}:
			default:
			}
			state.toolStarted = true
		}
		if !state.toolEnded && status == 3 {
			result := runCommandOutput(run)
			exit := intField(run, "exitCode")
			select {
			case out <- agentlink.Frame{Kind: agentlink.KindTool, Tool: &agentlink.Tool{
				Phase:  agentlink.ToolEnd,
				Name:   "run_command",
				Args:   string(args),
				Result: result,
				Err:    exit != 0,
			}}:
			default:
			}
			state.toolEnded = true
		}
	case 14: // CORTEX_STEP_TYPE_USER_INPUT — the TUI already rendered it
		return
	default:
		// A generic remote tool step (view_file, write_file, search_web,
		// ...). The reference puts the tool name and arguments on
		// metadata.toolCall and the output under generic.result.
		name, args, result := genericTool(step)
		if !state.toolStarted {
			select {
			case out <- agentlink.Frame{Kind: agentlink.KindTool, Tool: &agentlink.Tool{
				Phase: agentlink.ToolStart,
				Name:  name,
				Args:  args,
			}}:
			default:
			}
			state.toolStarted = true
		}
		if !state.toolEnded && status == 3 {
			select {
			case out <- agentlink.Frame{Kind: agentlink.KindTool, Tool: &agentlink.Tool{
				Phase:  agentlink.ToolEnd,
				Name:   name,
				Args:   args,
				Result: result,
			}}:
			default:
			}
			state.toolEnded = true
		}
	}
}

// finish emits the turn result.
func (e *RemoteAgentEngine) finish(out chan<- agentlink.Frame, reply, reasoning string) {
	select {
	case out <- agentlink.Frame{Kind: agentlink.KindResult, Result: &agentlink.Result{
		Reply:     reply,
		Reasoning: reasoning,
	}}:
	default:
	}
}

func (e *RemoteAgentEngine) emitError(out chan<- agentlink.Frame, err error) {
	select {
	case out <- agentlink.Frame{Kind: agentlink.KindResult, Error: err.Error()}:
	default:
	}
}

func (e *RemoteAgentEngine) cancelRemote(cascade string) {
	if cascade == "" {
		return
	}
	ctx, cancel := remoteagentclient.WithUnaryTimeout(context.Background())
	defer cancel()
	_ = e.client.Cancel(ctx, cascade)
}

// wireModel maps a catalogue label back to the id the remote proxy expects.
// An unknown value (a configured model's own spelling) passes through.
func wireModel(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	for _, e := range catalog.ModelCatalogue() {
		if e.ID == model || e.Label == model {
			return e.ID
		}
	}
	return model
}

// workspaceURI turns a local directory into the file:// URI the proxy expects.
func workspaceURI(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return ""
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	u := url.URL{Scheme: "file", Path: abs}
	return u.String()
}

// trajectorySteps returns the steps array of a state update.
func trajectorySteps(update map[string]any) []any {
	mtu, _ := update["mainTrajectoryUpdate"].(map[string]any)
	if mtu == nil {
		return nil
	}
	su, _ := mtu["stepsUpdate"].(map[string]any)
	if su == nil {
		return nil
	}
	steps, _ := su["steps"].([]any)
	return steps
}

func trajectoryError(update map[string]any) string {
	mtu, _ := update["mainTrajectoryUpdate"].(map[string]any)
	if mtu == nil {
		return ""
	}
	if raw, ok := mtu["lastStepError"]; ok && raw != nil {
		if msg := nestedString(raw, "message"); msg != "" {
			return msg
		}
	}
	if msg := nestedString(mtu["lastStepError"], "description"); msg != "" {
		return msg
	}
	return ""
}

func stepType(raw any) int {
	switch v := raw.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		switch v {
		case "CORTEX_STEP_TYPE_USER_INPUT":
			return 14
		case "CORTEX_STEP_TYPE_PLANNER_RESPONSE":
			return 15
		case "CORTEX_STEP_TYPE_RUN_COMMAND":
			return 21
		case "CORTEX_STEP_TYPE_ASK_QUESTION":
			return 138
		}
	}
	return 0
}

func stepStatus(raw any) int {
	switch v := raw.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		switch v {
		case "CORTEX_STEP_STATUS_DONE":
			return 3
		case "CORTEX_STEP_STATUS_RUNNING":
			return 2
		case "CORTEX_STEP_STATUS_GENERATING":
			return 8
		case "CORTEX_STEP_STATUS_ERROR":
			return 7
		case "CORTEX_STEP_STATUS_WAITING":
			return 9
		}
	}
	return 0
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

func boolField(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	v, _ := m[key].(bool)
	return v
}

func intField(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

func nestedString(raw any, key string) string {
	m, _ := raw.(map[string]any)
	return stringField(m, key)
}

func runCommandOutput(run map[string]any) string {
	if run == nil {
		return ""
	}
	if out, _ := run["combinedOutput"].(map[string]any); out != nil {
		if full := stringField(out, "full"); full != "" {
			return full
		}
	}
	if stdout := stringField(run, "stdout"); stdout != "" {
		return stdout
	}
	return stringField(run, "stderr")
}

// genericTool returns the name, arguments and output of a generic step.
//
// The reference carries a tool's identity on metadata.toolCall
// ({name, argumentsJson}) and its output under generic.result. Older
// shapes put the arguments in generic.args and the output in result.result
// or result.payload, so accept those too.
func genericTool(step map[string]any) (name, args, result string) {
	meta, _ := step["metadata"].(map[string]any)
	call, _ := meta["toolCall"].(map[string]any)
	name = stringField(call, "name")
	args = stringField(call, "argumentsJson")
	if name == "" {
		name = stringField(call, "id")
	}
	if name == "" {
		name = "remote_tool"
	}
	if args == "" {
		if generic, _ := step["generic"].(map[string]any); generic != nil {
			if raw, ok := generic["args"].(map[string]any); ok && len(raw) > 0 {
				if encoded, err := json.Marshal(raw); err == nil {
					args = string(encoded)
				}
			}
		}
	}
	return name, args, genericResult(step)
}

func genericResult(step map[string]any) string {
	generic, _ := step["generic"].(map[string]any)
	if generic == nil {
		return ""
	}
	result, _ := generic["result"].(map[string]any)
	if result == nil {
		return ""
	}
	if s := stringField(result, "result"); s != "" {
		return s
	}
	// result_struct / payload carry the structured output of tools whose
	// value is not a bare string.
	if s, ok := result["resultStruct"]; ok && s != nil {
		if encoded, err := json.Marshal(s); err == nil {
			return string(encoded)
		}
	}
	if s, ok := result["payload"]; ok && s != nil {
		if encoded, err := json.Marshal(s); err == nil {
			return string(encoded)
		}
	}
	return ""
}

// compile-time interface checks.
var _ Engine = (*RemoteAgentEngine)(nil)
var _ DefaultModeler = (*RemoteAgentEngine)(nil)
var _ CommandLister = (*RemoteAgentEngine)(nil)
var _ SessionResetter = (*RemoteAgentEngine)(nil)
var _ fmt.Stringer = (*RemoteAgentEngine)(nil)

// String names the engine for logs.
func (e *RemoteAgentEngine) String() string {
	if e == nil {
		return "remote-agent"
	}
	return "remote-agent(" + e.model + ")"
}

// PermissionSettings reads the remote permission preset and artifact-review
// mode. The TUI surfaces it through /permissions.
func (e *RemoteAgentEngine) PermissionSettings(ctx context.Context) (remoteagentclient.PermissionSettings, error) {
	if e == nil || e.client == nil {
		return remoteagentclient.PermissionSettings{}, errors.New("remote agent: no client configured")
	}
	return e.client.GetPermissionSettings(ctx)
}

// SetPermissionPreset writes the remote permission preset. The value is
// passed through verbatim: the proxy owns the enum.
func (e *RemoteAgentEngine) SetPermissionPreset(ctx context.Context, preset string) error {
	if e == nil || e.client == nil {
		return errors.New("remote agent: no client configured")
	}
	return e.client.SetPermissionPreset(ctx, preset)
}

// Plugins lists the remote plugins and their installations.
func (e *RemoteAgentEngine) Plugins(ctx context.Context) ([]remoteagentclient.PluginInfo, []remoteagentclient.PluginInstallation, error) {
	if e == nil || e.client == nil {
		return nil, nil, errors.New("remote agent: no client configured")
	}
	plugins, err := e.client.ListPlugins(ctx)
	if err != nil {
		return nil, nil, err
	}
	installations, err := e.client.ListPluginInstallations(ctx)
	if err != nil {
		return plugins, nil, err
	}
	return plugins, installations, nil
}

// InstallPlugin installs a plugin remotely and returns the consent URL, if any.
func (e *RemoteAgentEngine) InstallPlugin(ctx context.Context, pluginID string) (string, error) {
	if e == nil || e.client == nil {
		return "", errors.New("remote agent: no client configured")
	}
	return e.client.InstallPlugin(ctx, pluginID, "REMOTE")
}

// UninstallPlugin removes a remote plugin installation.
func (e *RemoteAgentEngine) UninstallPlugin(ctx context.Context, pluginID string) error {
	if e == nil || e.client == nil {
		return errors.New("remote agent: no client configured")
	}
	return e.client.UninstallPlugin(ctx, pluginID, "REMOTE")
}

// TurnDiff reads the remote diff for the latest turn.
//
// The latest turn's last step is stepBase-1 (stepBase advances past a
// turn's steps when it finishes), so that is the step whose containing
// turn the proxy should describe.
func (e *RemoteAgentEngine) TurnDiff(ctx context.Context) (remoteagentclient.TurnDiff, error) {
	if e == nil || e.client == nil {
		return remoteagentclient.TurnDiff{}, errors.New("remote agent: no client configured")
	}
	e.mu.Lock()
	cascade := e.cascade
	step := e.stepBase - 1
	e.mu.Unlock()
	if cascade == "" {
		return remoteagentclient.TurnDiff{}, errors.New("remote agent: no conversation")
	}
	if step < 0 {
		step = 0
	}
	return e.client.GetTurnDiff(ctx, cascade, step)
}

// CodeActionDiff reads the remote diff block of one code-action step.
func (e *RemoteAgentEngine) CodeActionDiff(ctx context.Context, stepIndex int) (remoteagentclient.CodeActionDiff, error) {
	if e == nil || e.client == nil {
		return remoteagentclient.CodeActionDiff{}, errors.New("remote agent: no client configured")
	}
	e.mu.Lock()
	cascade := e.cascade
	e.mu.Unlock()
	if cascade == "" {
		return remoteagentclient.CodeActionDiff{}, errors.New("remote agent: no conversation")
	}
	return e.client.GetCodeActionDiff(ctx, cascade, stepIndex)
}

// CodeSteps returns a copy of path → latest code-action step index.
func (e *RemoteAgentEngine) CodeSteps() map[string]int {
	if e == nil {
		return nil
	}
	e.codeMu.Lock()
	defer e.codeMu.Unlock()
	out := make(map[string]int, len(e.codeSteps))
	for path, step := range e.codeSteps {
		out[path] = step
	}
	return out
}

// codeActionPath extracts the workspace-relative path a CodeAction (5) or
// FileChange (86) step touched.
func (e *RemoteAgentEngine) codeActionPath(step map[string]any) string {
	typ := stepType(step["type"])
	var raw string
	switch typ {
	case 86: // CORTEX_STEP_TYPE_FILE_CHANGE
		change, _ := step["fileChange"].(map[string]any)
		raw = stringField(change, "absolutePathUri")
		if raw == "" {
			raw = stringField(change, "absoluteUri")
		}
	case 5: // CORTEX_STEP_TYPE_CODE_ACTION
		action, _ := step["codeAction"].(map[string]any)
		spec, _ := action["actionSpec"].(map[string]any)
		for _, member := range []string{"command", "createFile", "sed"} {
			branch, _ := spec[member].(map[string]any)
			if branch == nil {
				continue
			}
			pathScope, _ := branch["file"].(map[string]any)
			if pathScope == nil {
				pathScope, _ = branch["path"].(map[string]any)
			}
			if pathScope != nil {
				raw = stringField(pathScope, "absoluteUri")
				if raw != "" {
					break
				}
			}
		}
	}
	return e.workspaceRelative(raw)
}

// workspaceRelative turns a file URI or absolute path into the turn diff's
// relative key when it lives under the workspace.
func (e *RemoteAgentEngine) workspaceRelative(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || e.workspace == "" {
		return ""
	}
	path := raw
	if u, err := url.Parse(raw); err == nil && u.Scheme == "file" {
		path, _ = url.PathUnescape(u.Path)
	}
	rel, err := filepath.Rel(e.workspace, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(rel)
}

// CreateTerminal spawns a terminal in the remote conversation's
// environment.
func (e *RemoteAgentEngine) CreateTerminal(ctx context.Context) (remoteagentclient.TerminalInfo, error) {
	if e == nil || e.client == nil {
		return remoteagentclient.TerminalInfo{}, errors.New("remote agent: no client configured")
	}
	e.mu.Lock()
	cascade := e.cascade
	e.mu.Unlock()
	return e.client.CreateTerminal(ctx, cascade, "")
}

// Terminals lists the remote conversation's terminals.
func (e *RemoteAgentEngine) Terminals(ctx context.Context) ([]remoteagentclient.TerminalInfo, error) {
	if e == nil || e.client == nil {
		return nil, errors.New("remote agent: no client configured")
	}
	e.mu.Lock()
	cascade := e.cascade
	e.mu.Unlock()
	return e.client.ListTerminals(ctx, cascade)
}

// StreamTerminal opens a terminal's output stream.
func (e *RemoteAgentEngine) StreamTerminal(ctx context.Context, terminalID string, resumeOffset int64) (<-chan remoteagentclient.TerminalEvent, error) {
	if e == nil || e.client == nil {
		return nil, errors.New("remote agent: no client configured")
	}
	return e.client.StreamTerminalOutput(ctx, terminalID, resumeOffset)
}

// SendTerminalInput writes bytes to a remote terminal.
func (e *RemoteAgentEngine) SendTerminalInput(ctx context.Context, terminalID string, input []byte) error {
	if e == nil || e.client == nil {
		return errors.New("remote agent: no client configured")
	}
	_, err := e.client.SendTerminalInput(ctx, terminalID, input, "", 0)
	return err
}

// CloseTerminal ends a remote terminal.
func (e *RemoteAgentEngine) CloseTerminal(ctx context.Context, terminalID string) error {
	if e == nil || e.client == nil {
		return errors.New("remote agent: no client configured")
	}
	return e.client.CloseTerminal(ctx, terminalID)
}

// Skills lists the remote environment's skills (ListCustomizations).
//
// The first call performs the RPC; later calls return the cache. The
// workspace URI and the execution identity are the same values the
// cascade uses, so the server resolves the same skill roots.
func (e *RemoteAgentEngine) Skills(ctx context.Context) ([]skills.SkillInfo, error) {
	if e == nil || e.client == nil {
		return nil, errors.New("remote agent: no client configured")
	}
	e.skillsMu.Lock()
	if e.skillsLoaded {
		out := append([]skills.SkillInfo(nil), e.skills...)
		e.skillsMu.Unlock()
		return out, nil
	}
	e.skillsMu.Unlock()

	customizations, err := e.client.ListCustomizations(ctx, workspaceURI(e.workspace), "")
	if err != nil {
		return nil, err
	}
	out := make([]skills.SkillInfo, 0, len(customizations.Skills))
	for _, s := range customizations.Skills {
		if s.DisableSlashCommand {
			continue
		}
		name := strings.TrimSpace(s.DisplayName)
		if name == "" {
			name = strings.TrimSpace(s.Name)
		}
		if name == "" {
			continue
		}
		out = append(out, skills.SkillInfo{
			Name:           name,
			Description:    s.Description,
			Path:           s.Path,
			Plugin:         s.PluginName,
			Builtin:        s.IsBuiltin,
			ModelInvocable: !s.DisableModelInvocation,
			Icon:           s.Icon,
			Remote:         true,
		})
	}
	e.skillsMu.Lock()
	e.skills = out
	e.skillsLoaded = true
	e.skillsMu.Unlock()
	return append([]skills.SkillInfo(nil), out...), nil
}

// ReloadSkills drops the cache and fetches again.
func (e *RemoteAgentEngine) ReloadSkills(ctx context.Context) ([]skills.SkillInfo, error) {
	if e == nil {
		return nil, errors.New("remote agent: no engine")
	}
	e.skillsMu.Lock()
	e.skillsLoaded = false
	e.skills = nil
	e.skillsMu.Unlock()
	return e.Skills(ctx)
}

// CachedSkills returns the last fetched skill list without doing I/O, so
// the slash menu never blocks on the proxy.
func (e *RemoteAgentEngine) CachedSkills() []skills.SkillInfo {
	if e == nil {
		return nil
	}
	e.skillsMu.Lock()
	defer e.skillsMu.Unlock()
	return append([]skills.SkillInfo(nil), e.skills...)
}

// UploadProjectFile uploads a local file to the remote project and returns
// the created resource. It is the host path behind `/upload`.
func (e *RemoteAgentEngine) UploadProjectFile(ctx context.Context, path string) (remoteagentclient.ProjectResource, error) {
	if e == nil || e.client == nil {
		return remoteagentclient.ProjectResource{}, errors.New("remote agent: no client configured")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return remoteagentclient.ProjectResource{}, err
	}
	projectID := e.client.ProjectID()
	if projectID == "" {
		projectID = e.project
	}
	mimeType := mime.TypeByExtension(filepath.Ext(path))
	return e.client.UploadProjectFile(ctx, projectID, filepath.Base(path), data, mimeType)
}

// Triggers lists the remote environment's triggers.
func (e *RemoteAgentEngine) Triggers(ctx context.Context) ([]remoteagentclient.RemoteTrigger, error) {
	if e == nil || e.client == nil {
		return nil, errors.New("remote agent: no client configured")
	}
	triggers, _, err := e.client.ListTriggers(ctx, 100, "")
	return triggers, err
}

// TriggerOptions lists the trigger options the environment supports.
func (e *RemoteAgentEngine) TriggerOptions(ctx context.Context) ([]remoteagentclient.TriggerOption, error) {
	if e == nil || e.client == nil {
		return nil, errors.New("remote agent: no client configured")
	}
	return e.client.ListTriggerOptions(ctx)
}

// CreateTrigger creates a remote trigger.
func (e *RemoteAgentEngine) CreateTrigger(ctx context.Context, trigger remoteagentclient.RemoteTrigger) (remoteagentclient.RemoteTrigger, error) {
	if e == nil || e.client == nil {
		return remoteagentclient.RemoteTrigger{}, errors.New("remote agent: no client configured")
	}
	return e.client.CreateTrigger(ctx, trigger)
}

// UpdateTrigger updates a remote trigger.
func (e *RemoteAgentEngine) UpdateTrigger(ctx context.Context, trigger remoteagentclient.RemoteTrigger, mask []string) (remoteagentclient.RemoteTrigger, error) {
	if e == nil || e.client == nil {
		return remoteagentclient.RemoteTrigger{}, errors.New("remote agent: no client configured")
	}
	return e.client.UpdateTrigger(ctx, trigger, mask)
}

// DeleteTrigger deletes a remote trigger.
func (e *RemoteAgentEngine) DeleteTrigger(ctx context.Context, triggerID string) error {
	if e == nil || e.client == nil {
		return errors.New("remote agent: no client configured")
	}
	return e.client.DeleteTrigger(ctx, triggerID, true)
}
