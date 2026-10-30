// Package remotecontrol — dispatch_ls.go: the IDE's
// LanguageServerService adapter — the surface the Antigravity Web UI
// (the reference's own embedded frontend) actually speaks.
//
// The frontend talks to `exa.language_server_pb.LanguageServerService`
// over Connect-RPC; the clone's legacy JSON-RPC surface
// (`StartRemoteAgentCascade` and friends) is a different service
// (`exa.remote_agent_proxy_pb.JetskiRemoteAgentProxyService`). The
// two overlap in name but not in shape: the IDE's StartCascade mints
// the cascade id on the *client* side and hands it to the server,
// and its SendUserCascadeMessage carries the turn text in a repeated
// `items[].text` rather than a flat `text`.
//
// This file implements the conversation loop the Web UI needs:
//
//   - StartCascade            — registers the cascade under the id the
//     client minted.
//   - SendUserCascadeMessage  — runs one turn and appends the user /
//     assistant steps to the conversation's trajectory.
//   - StreamAgentStateUpdates — the server stream the Web UI renders
//     the conversation from (see ls_trajectory.go).
//   - GetSlashCommands and the trivially-answerable auxiliary calls.
//
// The response shapes are proto-JSON. The client parses unknown
// fields leniently (its Connect transport passes
// ignoreUnknownFields: true), so a response may carry extra keys —
// but a *known* key with the wrong JSON type still fails the parse.
package remotecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/convstore"
)

// lsStartCascadeParams is the LanguageServerService.StartCascade
// request shape (proto-JSON, camelCase).
type lsStartCascadeParams struct {
	CascadeID string `json:"cascadeId"`
	Source    string `json:"source"`
	// RequestedModel is the numeric Model value the Web UI's model
	// picker selected. The frontend sends the model it shows in the
	// picker button, so the cascade has to run on that model — the
	// clone used to ignore it and always answered with the host's
	// default, which is why picking another model changed nothing.
	RequestedModel   modelValue `json:"requestedModel"`
	ProjectEnvConfig struct {
		ProjectID string `json:"projectId"`
	} `json:"projectEnvConfig"`
}

// modelValue is the numeric exa.codeium_common_pb.Model value. The
// Web UI's picker sends it as proto-JSON spells an enum — by its
// NAME ("MODEL_PLACEHOLDER_M0") — because that is what it decoded
// the catalogue's `modelOrAlias` from. A number is accepted too.
type modelValue int

// UnmarshalJSON reads the number, a numeric string or an enum name.
func (m *modelValue) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if s == "" || s == "null" {
		*m = 0
		return nil
	}
	if n, err := strconv.Atoi(s); err == nil {
		*m = modelValue(n)
		return nil
	}
	if n, ok := modelEnumValue(s); ok {
		*m = modelValue(n)
		return nil
	}
	*m = 0
	return nil
}

// lsSendUserCascadeMessageParams is the
// LanguageServerService.SendUserCascadeMessage request shape
// (proto-JSON, camelCase). The turn text rides in `items[].text`
// (exa.codeium_common_pb.TextOrScopeItem).
type lsSendUserCascadeMessageParams struct {
	CascadeID string `json:"cascadeId"`
	Items     []struct {
		Text string `json:"text"`
	} `json:"items"`
	// Media are the pictures the user attached to the message.
	//
	// 【反编译】 exa.language_server_pb.SendUserCascadeMessageRequest
	// carries them in field 14 — a repeated
	// exa.codeium_common_pb.Media, whose payload oneof holds the
	// bytes inline (`inlineData`).
	Media []lsMediaItem `json:"media"`
	// CascadeConfig carries the model the Web UI's picker chose. A
	// conversation that is continued has no StartCascade to carry it,
	// so the message request is the only place the choice survives.
	CascadeConfig lsCascadeConfig `json:"cascadeConfig"`
	// DeliveryStrategy says what the server does with the message
	// when a turn is already in flight: hold it until that turn ends
	// (WHEN_IDLE), or interrupt the turn and send it now
	// (NEXT_INVOCATION).
	//
	// 【反编译】 exa.language_server_pb.SendUserCascadeMessageRequest:
	// delivery_strategy = 22, of type
	// exa.cortex_pb.MessageDeliveryStrategy.
	DeliveryStrategy lsDeliveryStrategy `json:"deliveryStrategy"`
}

// routeLanguageServerCascadeMethods handles the LanguageServerService
// methods the Web UI's conversation loop needs. Returns ok=true when
// the method was handled.
func (d *Dispatcher) routeLanguageServerCascadeMethods(ctx context.Context, method string, params json.RawMessage) (any, *rpcErrorBody, bool) {
	switch method {
	case "StartCascade":
		return d.handleLSStartCascade(params)
	case "SendUserCascadeMessage":
		return d.handleLSSendUserCascadeMessage(ctx, params)
	case "SendAllQueuedMessages":
		return d.handleLSSendAllQueuedMessages(params)
	case "DeleteQueuedUserInputStep":
		return d.handleLSDeleteQueuedUserInputStep(params)
	case "DeleteAgentMessage":
		// The IDE's own name for dropping a queued message: it
		// carries the pending message's id and the conversation it
		// waits in, not the dashboard's conversation_id + message_id.
		// It is routed here, before the alias table turns it into the
		// clone's legacy DeleteRemoteAgentMessage — that handler
		// rejects the IDE's shape with "conversation_id is required",
		// and the message the user deleted stayed in the queue.
		return d.handleLSDeleteAgentMessage(params)
	case "GetSlashCommands", "ListSlashCommands":
		return d.handleLSGetSlashCommands()
	case "GetCascadeTrajectory", "GetCascadeTrajectorySteps",
		"GetUserTrajectory",
		"GetUserTrajectoryDebug", "GetUserTrajectoryDescriptions":
		return d.routeLanguageServerTrajectoryMethods(method, params)
	case "SearchConversations", "GetConversationMetadata",
		"GetTrajectoryTurns", "GetAllCascadeTrajectories",
		"DeleteCascadeTrajectory":
		return d.routeLanguageServerConversationMethods(method, params)
	case "FetchUserInfo", "SetUserInfo":
		return d.handleLSUserInfo()
	case "JetboxWriteSummary":
		return d.handleLSJetboxWriteSummary(params)
	case "RequestAgentStatePageUpdate", "requestAgentStatePageUpdate":
		return d.handleLSRequestAgentStatePageUpdate(params)
	case "JetboxDeleteSummary",
		"UpdateConversationAnnotations", "RecordAnalyticsEvent":
		return map[string]any{}, nil, true
	case "RecordError":
		return d.handleLSRecordError(params)
	case "GetCascadeNuxes":
		return map[string]any{"nuxes": []any{}}, nil, true
	case "GetAllWorkflows":
		return map[string]any{"workflows": []any{}}, nil, true
	case "ListMcpPrompts":
		return map[string]any{"prompts": []any{}}, nil, true
	case "GetAllPlugins", "GetPlugin", "DeletePlugin",
		"ResolveCommandPlugin", "GetCustomizationStates",
		"GetAllSkills", "DeleteSkill",
		"SearchMarketplace", "SearchMarketplaceCustomizations",
		"GetMarketplaceCustomization", "GetMarketplaceBundles",
		"GetMarketplaceFeatured", "GetSkillMarketplaceLink",
		"GetAvailableCascadePlugins", "GetBuildWithGooglePlugins",
		"ListPluginAuthProviders", "InstallCustomization",
		"ListRemotePlugins", "DisconnectRemotePlugin",
		"DownloadBuildWithGooglePlugin", "StartPluginAuthSignIn",
		"SignInPluginAuthProvider", "SignOutPluginAuthProvider",
		"SetDefaultPluginAuthAccount", "SetPluginAuthAccountLabel",
		"SubscribeToPlugins":
		// The Web UI's plugin panels. See plugins_ls.go and
		// plugins_marketplace.go.
		return d.routeLanguageServerPluginMethods(method, params)
	}
	return nil, nil, false
}

// handleLSRecordError answers the Web UI's error report.
//
// The Web UI reports what went wrong on its side — a decoder that
// refused a frame, a request that never settled — and the report is
// the only window into a failure the instance cannot see: the RPC
// itself is a one-way report, and the UI's own console is in the
// browser. The clone logs it.
//
// 【一手】 2026-10-26: the report is what showed the remote-control
// page's own "Connecting to instance…" spinner — the UI's bootstrap
// arrived, three errors were reported, and the page never became
// ready.
func (d *Dispatcher) handleLSRecordError(params json.RawMessage) (any, *rpcErrorBody, bool) {
	if len(params) > 0 {
		log.Printf("remotecontrol: web UI error report: %s", string(params))
	}
	return map[string]any{}, nil, true
}

// handleLSStartCascade registers the cascade under the id the Web UI
// minted. The frontend generates the uuid itself and sends it as
// `cascadeId`, then uses the same uuid as the conversation id — so the
// clone must keep the client's id rather than minting its own, or the
// follow-up SendUserCascadeMessage cannot find the cascade.
func (d *Dispatcher) handleLSStartCascade(params json.RawMessage) (any, *rpcErrorBody, bool) {
	if d == nil || d.cascades == nil {
		return nil, &rpcErrorBody{Code: -32603, Message: "Internal error", Data: "registry not initialised"}, true
	}
	var req lsStartCascadeParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}, true
		}
	}
	cascadeID := strings.TrimSpace(req.CascadeID)
	if cascadeID == "" {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "cascadeId is required"}, true
	}
	opts := d.buildLSCascadeOpts(&req)
	opts.ID = cascadeID
	c, err := NewCascade(opts)
	if err != nil {
		return nil, &rpcErrorBody{Code: -32603, Message: "Internal error", Data: err.Error()}, true
	}
	if err := d.cascades.Register(c); err != nil {
		return nil, &rpcErrorBody{Code: -32603, Message: "Internal error", Data: err.Error()}, true
	}
	// Seed the trajectory so the StreamAgentStateUpdates stream can
	// find it even when the client subscribes before it sends.
	d.trajectories.Get(c.ID)
	return map[string]any{
		"cascadeId":      c.ID,
		"cascade_id":     c.ID,
		"conversationId": c.ID,
		"model":          c.Model,
		"backend":        c.BackendLabel,
		"projectEnvInfo": map[string]any{},
	}, nil, true
}

// buildLSCascadeOpts derives the cascade's wiring from the host
// session — and from the model the Web UI's picker selected.
//
// The picker sends the numeric Model value of the selected entry in
// `requestedModel`; the catalogue turns it back into the model's id.
// Without that the cascade would always run on the host's default
// model and the picker's choice would be silently dropped.
func (d *Dispatcher) buildLSCascadeOpts(req *lsStartCascadeParams) CascadeOpts {
	opts := CascadeOpts{DispatchDefault: d.cfg.MessageBackend}
	if d.cfg.Session != nil {
		opts.Model = d.cfg.Session.Model
		opts.BackendLabel = d.cfg.Session.Backend
		opts.Workspace = d.cfg.Session.Workspace
	}
	if id := d.modelIDForRequestedModel(int(req.RequestedModel)); id != "" {
		opts.Model = id
	}
	opts.ProjectID = req.ProjectEnvConfig.ProjectID
	return opts
}

// modelIDForRequestedModel resolves the picker's Model value to a
// catalogue id.
func (d *Dispatcher) modelIDForRequestedModel(value int) string {
	if value == 0 {
		return ""
	}
	return modelIDForWireValue(value, d.userStatusModels())
}

// handleLSSendUserCascadeMessage runs one turn for the Web UI. The
// turn text rides in `items[].text`; the reply comes back through the
// StreamAgentStateUpdates trajectory, so this response stays empty
// (the IDE's SendUserCascadeMessageResponse has no fields).
func (d *Dispatcher) handleLSSendUserCascadeMessage(ctx context.Context, params json.RawMessage) (any, *rpcErrorBody, bool) {
	if d == nil || d.cascades == nil {
		return nil, &rpcErrorBody{Code: -32603, Message: "Internal error", Data: "registry not initialised"}, true
	}
	var req lsSendUserCascadeMessageParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}, true
		}
	}
	cascadeID := strings.TrimSpace(req.CascadeID)
	if cascadeID == "" {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "cascadeId is required"}, true
	}
	text := lsJoinItemText(req.Items)
	images := messageImagesFromMedia(req.Media)
	if text == "" && len(images) == 0 {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "items[].text is required"}, true
	}
	c, ok := d.cascades.Get(cascadeID)
	if !ok {
		// The Web UI continues a conversation the daemon has no
		// cascade for: the frontend only calls StartCascade for a
		// *new* conversation. The reference's own server resolves the
		// conversation from its store; so does the clone.
		c, ok = d.lsResumeCascade(cascadeID, d.lsCascadeConfigModel(req.CascadeConfig))
	}
	if !ok {
		return nil, &rpcErrorBody{Code: ErrCodeCascadeNotFound, Message: "Cascade not found", Data: cascadeID}, true
	}
	return d.admitLSCascadeMessage(ctx, c, text, images, req.DeliveryStrategy)
}

// admitLSCascadeMessage puts a message into the conversation.
//
// A conversation runs one turn at a time. A message that arrives while
// a turn is in flight waits for it, and it waits at the **back** of
// the queue.
//
// 【反编译】 2026-10-26, the reference's own executor — the rule, in
// agentexecutor.(*AgentExecutor).scheduleUserCascadeMessageRequest
// (0x101e35620, the IDE language server):
//
//	queue = (executorLoopStatus == RUNNING) || (deliveryStrategy == WHEN_IDLE && !fullyIdle)
//
// While the executor loop is RUNNING *every* message waits, whatever
// the strategy says: the reference never interrupts a running turn, and
// the reference's own frontend agrees (its pending predicate answers
// "pending" for a NEXT_INVOCATION message exactly while the executor
// loop is RUNNING). The setting's own copy — "Interrupt the agent and
// send immediately." — promises more than the reference delivers.
//
// The message that finds the conversation idle runs right away, and the
// request waits for the turn — and for every message the queue hands
// it afterwards.
func (d *Dispatcher) admitLSCascadeMessage(ctx context.Context, c *Cascade, text string, images []MessageImage, strategy lsDeliveryStrategy) (any, *rpcErrorBody, bool) {
	traj := d.trajectories.Get(c.ID)
	if traj == nil {
		return nil, &rpcErrorBody{Code: -32603, Message: "Internal error", Data: "no trajectory store"}, true
	}
	msg := lsQueuedMessage{
		ID:       traj.nextPendingID(),
		Text:     text,
		Images:   images,
		Strategy: strategy,
		Step:     lsEncodeUserInputStep(text, true, time.Now()),
	}
	if !traj.Admit(msg) {
		log.Printf("remotecontrol: conversation %s: message queued (%s), %d waiting",
			c.ID, lsDeliveryStrategyName(strategy), len(traj.Pending()))
		return map[string]any{}, nil, true
	}
	return d.runLSTurnQueue(ctx, c, msg)
}

// runLSTurnQueue runs the admitted message and then every message the
// queue hands it, until the queue is empty.
//
// A conversation runs one turn at a time: the turn that ends hands the
// queue its next message and runs that one in turn.
//
// The turn's context does not die with the request that started it: a
// queued message outlives the request that queued it, and the values
// the request carried — the websocket connection the notifications go
// to — are kept.
func (d *Dispatcher) runLSTurnQueue(ctx context.Context, c *Cascade, msg lsQueuedMessage) (any, *rpcErrorBody, bool) {
	traj := d.trajectories.Get(c.ID)
	turnCtx := context.WithoutCancel(ctx)
	var firstErr error
	for {
		steps, err := d.runLSTurn(turnCtx, c, msg)
		interrupted, next, hasNext := traj.FinishTurn()
		// An interrupted turn did not fail: the message that
		// interrupted it is why the turn ended. Reporting the
		// cancellation as an error is what made the Web UI toast
		// "Failed to send" over a message it had itself
		// interrupted. The stored turn says the same thing: the
		// reference marks an interrupted turn interrupted, not failed,
		// so the error step is left out.
		storeErr := err
		if interrupted {
			storeErr = nil
		} else if err != nil {
			log.Printf("remotecontrol: conversation %s: turn failed: %v", c.ID, err)
			if firstErr == nil {
				firstErr = err
			}
		}
		d.lsStoreTurn(c, msg.Text, steps, storeErr)
		if !hasNext {
			break
		}
		log.Printf("remotecontrol: conversation %s: running the next queued message (%s)",
			c.ID, lsDeliveryStrategyName(next.Strategy))
		msg = next
	}
	if firstErr != nil {
		return nil, &rpcErrorBody{Code: -32603, Message: "Internal error", Data: firstErr.Error()}, true
	}
	return map[string]any{}, nil, true
}

// runLSTurn drives one turn and records the trajectory steps the Web
// UI renders: the user's message, the tool calls, and the assistant's
// reply.
//
// The steps are published while the turn runs, not when it ends: the
// Web UI renders the conversation from the trajectory, so a turn that
// is only written at the end is a turn the user watches nothing
// happen through.
//
// It returns the steps to store and the turn's error. The error is
// nil-safe to swallow: the caller decides, because only the caller
// knows whether the turn was interrupted on purpose.
func (d *Dispatcher) runLSTurn(ctx context.Context, c *Cascade, msg lsQueuedMessage) ([]convstore.Step, error) {
	traj := d.trajectories.Get(c.ID)
	if traj != nil {
		traj.Append(lsUserInputStep(msg.Text))
	}
	// The turn's own cancel func: a message that interrupts the turn
	// fires it.
	turnCtx, cancel := context.WithCancel(ctx)
	if traj != nil {
		traj.BeginTurn(cancel)
	}
	defer cancel()
	p := &sendRemoteAgentCascadeMessageParams{Text: msg.Text}
	prompt := buildSendPrompt(c, p)
	// The pictures the user attached ride with the turn: they are
	// part of the message, not of the conversation around it.
	prompt.Images = msg.Images
	// The composer sends one message at a time, so the turns before
	// this one have to be replayed into the model: the conversation is
	// one conversation, not one context per message. The store is the
	// source of truth — the same file the sidebar reads — and the turn
	// that runs now is appended to it when it ends.
	prompt.History = lsConversationHistory(c.ID)
	opts := buildSendOptions(p)
	connSink := NewConnNotifierSink(d, connIDFromContext(ctx), c)
	// The trajectory sink publishes the turn's steps as the model
	// produces them; the thought sink keeps the model's reasoning
	// (the `<think>…</think>` block the CloudCode transport wraps
	// around it) out of the visible text and hands it to the
	// trajectory sink as it arrives.
	trajSink := newLSTrajectorySink(traj, connSink)
	thoughtSink := newLSThoughtSink(trajSink)
	res, err := d.cascades.SendStream(turnCtx, c.ID, prompt, thoughtSink, opts)
	thoughtSink.Flush(c)
	_, thinkingDuration := thoughtSink.Thought()
	trajSink.Finish(res, thinkingDuration)
	return trajSink.StoredSteps(), err
}

// lsJoinItemText concatenates the text of the request's items. The
// IDE's items are a repeated TextOrScopeItem, oneof {text | item};
// only the text arm is meaningful to the clone.
func lsJoinItemText(items []struct {
	Text string `json:"text"`
}) string {
	var b strings.Builder
	for _, item := range items {
		text := strings.TrimSpace(item.Text)
		if text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(text)
	}
	return b.String()
}

// lsStoreTurn appends the turn's steps to the conversation file.
//
// 【一手】 2026-10-26: the reference's own language server appends to
// the conversation file when a message is sent into a conversation it
// did not start — the file's step count grew by two after the message
// went in.
//
// The clone appends too, so the conversation the Web UI continues is
// the conversation the next turn continues, and so the history the
// sidebar shows is the history that exists.
func (d *Dispatcher) lsStoreTurn(c *Cascade, text string, steps []convstore.Step, turnErr error) {
	if d == nil || c == nil {
		return
	}
	stored := make([]convstore.Step, 0, len(steps)+2)
	stored = append(stored, convstore.Step{
		Type:   convstore.StepUserInput,
		Status: lsStepStatusDone,
		Text:   text,
	})
	stored = append(stored, steps...)
	if turnErr != nil {
		stored = append(stored, convstore.Step{
			Type:   convstore.StepError,
			Status: lsStepStatusDone,
			Text:   turnErr.Error(),
		})
	}
	// The store keys a step by its index, so the indices have to be
	// the row numbers. A conversation the clone writes from scratch
	// starts at zero; a conversation the clone appends to continues
	// the rows that are already there — convstore.AppendConversationSteps
	// assigns those.
	for i := range stored {
		stored[i].Idx = int64(i)
	}
	workspace := ""
	if d.cfg.Session != nil {
		workspace = d.cfg.Session.Workspace
	}
	if err := convstore.AppendConversationSteps(c.ID, workspace, stored); err != nil {
		log.Printf("remotecontrol: store conversation %s: %v", c.ID, err)
	}
}

// handleLSGetSlashCommands answers the Web UI's slash-command
// autocomplete. The IDE's SlashCommandDefinition wraps the command
// info in an `info` field and repeats the name as `title`; the
// clone's own catalogue carries a different field set, so each entry
// is mapped explicitly.
func (d *Dispatcher) handleLSGetSlashCommands() (any, *rpcErrorBody, bool) {
	commands := []any{}
	if d != nil && d.cfg.ListBackend.SlashCommands != nil {
		if list, err := d.cfg.ListBackend.SlashCommands(); err == nil {
			for _, c := range list {
				commands = append(commands, map[string]any{
					"info": map[string]any{
						"name":        c.Name,
						"displayName": c.Name,
						"type":        1,
					},
					"title":       c.Name,
					"description": c.Description,
				})
			}
		}
	}
	return map[string]any{"commands": commands}, nil, true
}

// handleLSUserInfo answers the Web UI's CloudCodeService settings
// calls. The response's `user_settings` block is
// google.internal.cloud.code.v1internal.UserSettings; the clone
// reports telemetry as on and the rest off. The Web UI treats a
// missing `userSettings` as a hard failure ("fetchUserInfo
// returned no userSettings"), so the block is always present.
func (d *Dispatcher) handleLSUserInfo() (any, *rpcErrorBody, bool) {
	return map[string]any{
		"userSettings": map[string]any{
			"telemetryEnabled":                true,
			"userDataCollectionForceDisabled": false,
			"marketingEmailsEnabled":          false,
		},
	}, nil, true
}

// DispatchStream serves the Web UI's server-streaming RPCs. It
// returns handled=false for a method the dispatcher does not stream,
// so the transport can fall back to its one-frame behaviour.
//
// The emit callback is called once per frame; the dispatcher owns the
// frame cadence. DispatchStream returns when ctx is done.
func (d *Dispatcher) DispatchStream(ctx context.Context, method string, payload string, emit func(json.RawMessage) error) (bool, error) {
	switch method {
	case "StreamAgentStateUpdates":
		return d.streamAgentStateUpdates(ctx, payload, emit)
	case "JetboxSubscribeToState", "jetboxSubscribeToState":
		return d.streamJetboxSubscribeToState(ctx, payload, emit)
	case "JetboxSubscribeToSummaries", "jetboxSubscribeToSummaries":
		return d.streamJetboxSubscribeToSummaries(ctx, payload, emit)
	case "ProjectUpdatesStream", "projectUpdatesStream":
		return d.streamProjectUpdates(ctx, payload, emit)
	case "SubscribeToMcpServerStates", "subscribeToMcpServerStates":
		return d.streamSubscribeToMcpServerStates(ctx, payload, emit)
	case "SubscribeToPlugins", "subscribeToPlugins":
		return d.streamSubscribeToPlugins(ctx, payload, emit)
	case "StreamTerminalOutput", "streamTerminalOutput":
		// The Web UI's terminal output stream — the reference's
		// own StreamTerminalOutputResponse frames.
		//
		// 【一手】 2026-10-27; see ls_terminals_stream.go.
		return d.lsStreamTerminalOutput(ctx, payload, emit)
	case "StreamAudioTranscription", "streamAudioTranscription":
		// The Web UI's voice input stream — the reference's own
		// StreamAudioTranscriptionResponse frames.
		//
		// 【一手】 2026-10-27; see ls_audio.go.
		return d.lsStreamAudioTranscription(ctx, payload, emit)
	case "SubscribeToAgentBrowserRequests", "subscribeToAgentBrowserRequests":
		// The Web UI's half of the agent↔browser bridge: one frame
		// per browser action the agent asked for.
		//
		// 【一手】 2026-10-27; see dispatch_agentbrowser.go.
		return d.lsSubscribeToAgentBrowserRequests(ctx, payload, emit)
	}
	return false, nil
}

// streamAgentStateUpdates serves the trajectory stream. It emits the
// conversation's current state immediately, then one frame per
// trajectory change until the client goes away.
func (d *Dispatcher) streamAgentStateUpdates(ctx context.Context, payload string, emit func(json.RawMessage) error) (bool, error) {
	var req struct {
		ConversationID string `json:"conversationId"`
	}
	if len(payload) > 0 {
		if err := json.Unmarshal([]byte(payload), &req); err != nil {
			return true, err
		}
	}
	conversationID := strings.TrimSpace(req.ConversationID)
	if conversationID == "" {
		// The reference holds the connection open and says nothing
		// when the request carries no conversation — it does not
		// fail the stream.
		//
		// 【一手】 2026-10-27, measured against the reference binary:
		// a StreamAgentStateUpdates call with an empty body answers
		// HTTP 200 with the gRPC-Web content type and no frame at
		// all; the connection stays open.
		<-ctx.Done()
		return true, nil
	}
	traj := d.trajectories.Get(conversationID)
	if traj == nil {
		return true, errors.New("no trajectory store")
	}
	// A conversation the daemon has not run in this process still has
	// its steps on disk. The reference answers the Web UI's opening of
	// such a conversation with the stored steps in the first frame of
	// the state stream — the stream is the rehydration, the page
	// update is the pagination that follows it — and so does the
	// clone.
	traj.Seed(lsStoredSteps(conversationID))
	snap := traj.Snapshot()
	if err := emitLSFrame(emit, snap); err != nil {
		return true, err
	}
	for {
		version, err := traj.Wait(ctx, snap.Version)
		if err != nil {
			return true, nil
		}
		if version == snap.Version {
			continue
		}
		snap = traj.Snapshot()
		if err := emitLSFrame(emit, snap); err != nil {
			return true, err
		}
	}
}

// emitLSFrame renders the snapshot as one AgentStateUpdate frame and
// hands it to the transport's emit callback.
func emitLSFrame(emit func(json.RawMessage) error, snap lsSnapshot) error {
	raw, err := marshalLSMessage("exa.jetski_cortex_pb.StreamAgentStateUpdatesResponse", lsAgentStateFrame(snap))
	if err != nil {
		return err
	}
	return emit(raw)
}
