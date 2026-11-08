package mockremoteagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/internal/core/interaction"
	"github.com/justwasm/pigasus/internal/features/auth"
)

// Service implements the google.gca.aicode.v1main.JetskiRemoteAgentProxyService RPC surface.
type Service struct {
	cascades *CascadeManager
	mu       sync.Mutex
	projects map[string]ProjectConfig
	// CustomTurnHandler allows injecting custom agent turn logic (e.g. real LLM or test mock)
	CustomTurnHandler func(ctx context.Context, session *CascadeSession, userPrompt string)
	// RequestLogger logs incoming HTTP requests for debugging and testing.
	RequestLogger func(r *http.Request, method string, body []byte)
	// permissionPreset is the remote permission preset the proxy reports.
	// Guarded by mu.
	permissionPreset string
	// terminals owns the PTYs the terminal RPCs create.
	terminals *terminalManager
	// triggers is the in-memory trigger store.
	triggersMu sync.Mutex
	triggers   map[string]Trigger
}

// NewService creates a new Service instance backed by the given CascadeManager.
func NewService(cm *CascadeManager) *Service {
	s := &Service{
		cascades:         cm,
		projects:         make(map[string]ProjectConfig),
		permissionPreset: "AUTO_APPROVE",
		terminals:        newTerminalManager(),
		triggers:         make(map[string]Trigger),
	}
	s.projects["project-cli-mock"] = ProjectConfig{
		ProjectID:        "project-cli-mock",
		Title:            "CLI Project",
		UpdateTimeMicros: time.Now().UnixMicro(),
		Archived:         false,
		Resources:        []ProjectResource{},
	}
	s.CustomTurnHandler = s.defaultSimulatedTurn
	return s
}

// executeHostCommand runs a shell command on the host OS in the workspace directory.
func executeHostCommand(ctx context.Context, cmdStr, wsDir string) (stdout string, stderr string, exitCode int) {
	shell := "sh"
	if _, err := exec.LookPath("zsh"); err == nil {
		shell = "zsh"
	}
	cmd := exec.CommandContext(ctx, shell, "-c", cmdStr)
	if wsDir != "" {
		cmd.Dir = wsDir
	}
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}
	return outBuf.String(), errBuf.String(), exitCode
}

// defaultSimulatedTurn simulates or executes an agent turn.
// If the user requests a command (e.g. "run <cmd>", "exec <cmd>", or "list"), it executes it on the host in the workspace directory.
func (s *Service) defaultSimulatedTurn(_ context.Context, session *CascadeSession, userPrompt string) {
	turnCtx, _ := session.BeginTurn()

	go func() {
		// Wait briefly so StreamAgentStateUpdates connection has established
		select {
		case <-turnCtx.Done():
			return
		case <-time.After(50 * time.Millisecond):
		}

		trimmed := strings.TrimSpace(userPrompt)
		lowerPrompt := strings.ToLower(trimmed)

		// Determine target workspace directory
		wsDir := ""
		if len(session.WorkspaceURIs) > 0 {
			ws := session.WorkspaceURIs[0]
			ws = strings.TrimPrefix(ws, "file://")
			if info, err := os.Stat(ws); err == nil && info.IsDir() {
				wsDir = ws
			}
		}
		if wsDir == "" {
			wsDir, _ = os.Getwd()
		}

		// Detect explicit or implicit command execution
		cmdToRun := ""
		if strings.HasPrefix(lowerPrompt, "run ") {
			cmdToRun = strings.TrimSpace(trimmed[4:])
		} else if strings.HasPrefix(lowerPrompt, "exec ") {
			cmdToRun = strings.TrimSpace(trimmed[5:])
		} else if strings.HasPrefix(lowerPrompt, "! ") || strings.HasPrefix(lowerPrompt, "$ ") {
			cmdToRun = strings.TrimSpace(trimmed[2:])
		} else if strings.Contains(lowerPrompt, "list") {
			cmdToRun = "ls -la"
		}

		if cmdToRun != "" {
			// Thought step
			session.AddStep("PLANNER_RESPONSE", "", fmt.Sprintf("Executing command on remote workspace (%s):\n`%s`", wsDir, cmdToRun), nil)

			select {
			case <-turnCtx.Done():
				return
			case <-time.After(50 * time.Millisecond):
			}

			// Execute command in workspace directory
			stdout, stderr, exitCode := executeHostCommand(turnCtx, cmdToRun, wsDir)
			combined := stdout
			if stderr != "" {
				if combined != "" && !strings.HasSuffix(combined, "\n") {
					combined += "\n"
				}
				combined += stderr
			}

			toolCall := map[string]any{
				"commandLine": cmdToRun,
				"exitCode":    exitCode,
				"stdout":      stdout,
				"stderr":      stderr,
				"combined":    combined,
			}
			session.AddStep("RUN_COMMAND", cmdToRun, "", []any{toolCall})

			select {
			case <-turnCtx.Done():
				return
			case <-time.After(50 * time.Millisecond):
			}

			// Response step
			displayOut := strings.TrimRight(combined, "\n")
			if len(displayOut) > 4000 {
				displayOut = displayOut[:4000] + "\n...(truncated)"
			}
			var reply string
			if displayOut != "" {
				reply = fmt.Sprintf("Command `%s` completed with exit code %d:\n\n```\n%s\n```", cmdToRun, exitCode, displayOut)
			} else {
				reply = fmt.Sprintf("Command `%s` completed with exit code %d (no output).", cmdToRun, exitCode)
			}
			session.AddStep("PLANNER_RESPONSE", reply, "", nil)
		} else {
			// Step 1: Agent Generating (Thinking)
			step := session.AddStepWithStatus(
				"PLANNER_RESPONSE",
				"CORTEX_STEP_STATUS_GENERATING",
				"",
				"Analyzing user prompt: "+userPrompt,
				nil,
			)

			select {
			case <-turnCtx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}

			// Step 1 Updated in place: Agent Done with Response
			reply := fmt.Sprintf("Remote Agent processed prompt: %q. Remote environment and workspace are ready.", userPrompt)
			session.UpdateStep(
				step.StepIndex,
				"CORTEX_STEP_STATUS_DONE",
				reply,
				"Analyzing user prompt: "+userPrompt,
				nil,
			)
		}

		// Brief delay to ensure subscribers receive the completed step before transitioning to IDLE
		select {
		case <-turnCtx.Done():
			return
		case <-time.After(50 * time.Millisecond):
		}

		// Finish turn
		session.SetStatus("IDLE")
	}()
}

// ServeHTTP handles routing for all JetskiRemoteAgentProxyService RPCs.
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Normalize path
	path := r.URL.Path
	method := path
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		method = path[idx+1:]
	}
	method = strings.TrimPrefix(method, "remote:")

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read body if POST
	var body []byte
	if r.Body != nil {
		body, _ = io.ReadAll(r.Body)
	}

	if s.RequestLogger != nil {
		s.RequestLogger(r, method, body)
	}

	cleanMethod := strings.ToLower(method)
	cleanMethod = strings.TrimPrefix(cleanMethod, "v1alpha:")
	cleanMethod = strings.TrimPrefix(cleanMethod, "v1internal:")

	switch cleanMethod {
	case "ensureenvironment", "ensureremoteagentacevm":
		s.handleEnsureEnvironment(w, body)
	case "listenvironments", "listremoteagentenvironments":
		s.handleListEnvironments(w)
	case "listavailablemodels", "listremoteagentmodels":
		s.handleListAvailableModels(w)
	case "listslashcommands", "listremoteagentslashcommands":
		s.handleListSlashCommands(w)
	case "startcascade", "startremoteagentcascade":
		s.handleStartCascade(w, r, body)
	case "sendusercascademessage", "sendremoteagentcascademessage":
		s.handleSendUserCascadeMessage(w, r, body)
	case "cancelcascade", "cancelremoteagentcascade":
		s.handleCancelCascade(w, body)
	case "streamagentstateupdates":
		s.handleStreamAgentStateUpdates(w, r, body)
	case "getconversation":
		s.handleGetConversation(w, r, body)
	case "listconversations", "listremoteagentconversations":
		s.handleListConversations(w)
	case "deleteconversation", "deleteremoteagentconversation":
		s.handleDeleteConversation(w, body)
	case "readfile", "readremoteagentfile":
		s.handleReadFile(w, body)
	case "readdir", "readremoteagentdir":
		s.handleReadDir(w, body)
	case "getturndiff", "getremoteagentturndiff":
		s.handleGetTurnDiff(w, body)
	case "getcodeactiondiff", "getremoteagentcodeactiondiff":
		s.handleGetCodeActionDiff(w, body)
	case "handlecascadeuserinteraction", "handleremoteagentcascadeuserinteraction":
		s.handleCascadeUserInteraction(w, body)
	case "getpermissionsettings":
		s.handleGetPermissionSettings(w)
	case "setpermissionpreset":
		s.handleSetPermissionPreset(w, body)
	case "setartifactreviewmode":
		s.handleSetArtifactReviewMode(w, body)
	case "addpermissiongrants":
		s.handleAddPermissionGrants(w, body)
	case "removepermissiongrants":
		s.handleRemovePermissionGrants(w, body)
	case "createterminal", "createremoteagentterminal":
		s.handleCreateTerminal(w, body)
	case "listterminals", "listremoteagentterminals":
		s.handleListTerminals(w, r, body)
	case "closeterminal", "closeremoteagentterminal":
		s.handleCloseTerminal(w, body)
	case "sendterminalinput", "sendremoteagentterminalinput":
		s.handleSendTerminalInput(w, body)
	case "streamterminaloutput", "streamremoteagentterminaloutput":
		s.handleStreamTerminalOutput(w, r, body)
	case "uploadprojectfile", "uploadremoteagentprojectfile":
		s.handleUploadProjectFile(w, body)
	case "fetchconfig":
		s.handleFetchConfig(w)
	case "listexperiments":
		s.handleListExperiments(w)
	case "loadcodeassist":
		s.handleLoadCodeAssist(w)
	case "onboarduser":
		s.handleOnboardUser(w)
	case "fetchavailablemodels":
		s.handleFetchAvailableModels(w)
	case "retrieveuserquotasummary", "getuserquotasummary":
		s.handleRetrieveUserQuotaSummary(w)
	case "generateconversationtitle":
		s.handleGenerateConversationTitle(w, body)
	case "cascadenuxes", "fetchadmincontrols", "fetchuserinfo", "getuserinfo":
		writeJSON(w, map[string]any{"name": "users/me"})
	case "fetchlicenses":
		writeJSON(w, map[string]any{"licenses": []any{}})
	case "getusersettings", "updateusersettings":
		writeJSON(w, map[string]any{"dataCollectionOptin": true})
	case "createproject":
		s.handleCreateProject(w, body)
	case "updateproject", "updateremoteagentproject":
		s.handleUpdateProject(w, body)
	case "deleteproject", "deleteremoteagentproject":
		s.handleDeleteProject(w, body)
	case "listprojects":
		s.handleListProjects(w, r)
	case "readproject", "getproject":
		s.handleReadProject(w, r, body)
	case "upsertconversationconfig":
		s.handleUpsertConversationConfig(w, body)
	case "listtriggers", "listremoteagenttriggers":
		s.handleListTriggers(w, body)
	case "listtriggeroptions", "listremoteagenttriggeroptions":
		s.handleListTriggerOptions(w)
	case "createtrigger", "createremoteagenttrigger":
		s.handleCreateTrigger(w, body)
	case "updatetrigger", "updateremoteagenttrigger":
		s.handleUpdateTrigger(w, body)
	case "deletetrigger", "deleteremoteagenttrigger":
		s.handleDeleteTrigger(w, body)
	case "listplugins":
		s.handleListPlugins(w)
	case "installplugin":
		s.handleInstallPlugin(w, body)
	case "uninstallplugin":
		s.handleUninstallPlugin(w, body)
	case "listplugininstallations":
		s.handleListPluginInstallations(w)
	case "disconnectplugin":
		s.handleDisconnectPlugin(w, body)
	case "listresourceproviders":
		s.handleListResourceProviders(w)
	case "listprojectresources":
		s.handleListProjectResources(w)
	case "listavailableresources":
		s.handleListAvailableResources(w)
	case "getversioncontrolstate":
		s.handleGetVersionControlState(w)
	case "listcustomizations", "listremoteagentcustomizations":
		s.handleListCustomizations(w, body)
	case "getuserprofile":
		writeJSON(w, map[string]any{"displayName": "Developer", "email": "dev@local"})
	default:
		// Unknown RPC: return standard JSON error or empty object
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "OK", "method": method})
	}
}

func (s *Service) handleEnsureEnvironment(w http.ResponseWriter, body []byte) {
	var req EnsureEnvironmentRequest
	_ = json.Unmarshal(body, &req)

	resp := EnsureEnvironmentResponse{
		Environment: Environment{
			IsDefault:       true,
			ConnectionState: "CONNECTION_STATE_CONNECTED",
			Ace: map[string]any{
				"endpoint": "ace-vm.internal.google:443",
			},
		},
	}
	writeJSON(w, resp)
}

func (s *Service) handleListEnvironments(w http.ResponseWriter) {
	resp := ListEnvironmentsResponse{
		Environments: []Environment{
			{
				IsDefault:       true,
				ConnectionState: "CONNECTION_STATE_CONNECTED",
				Ace: map[string]any{
					"endpoint": "default-ace-vm.internal.google:443",
				},
			},
		},
	}
	writeJSON(w, resp)
}

func (s *Service) handleListAvailableModels(w http.ResponseWriter) {
	resp := ListAvailableModelsResponse{
		Models: []ModelInfo{
			{Model: "gemini-3.8-flash-high", DisplayName: "Gemini 3.8 Flash (High)", Description: "Recommended high performance flash reasoning model"},
		},
	}
	writeJSON(w, resp)
}

func (s *Service) handleListSlashCommands(w http.ResponseWriter) {
	resp := ListSlashCommandsResponse{
		SlashCommands: []SlashCommandInfo{
			{Name: "/compact", Description: "Summarize and compact conversation trajectory"},
			{Name: "/clear", Description: "Clear active conversation"},
			{Name: "/model", Description: "Select or switch current model"},
			{Name: "/help", Description: "Show available commands"},
		},
	}
	writeJSON(w, resp)
}

func (s *Service) handleStartCascade(w http.ResponseWriter, r *http.Request, body []byte) {
	ctx := r.Context()
	var req StartCascadeRequest
	_ = json.Unmarshal(body, &req)

	cascadeID := fmt.Sprintf("cascade-%d", time.Now().UnixNano())
	model := "gemini-3.8-flash-high"

	projectID := req.ProjectID
	if projectID == "" && req.ProjectIdentifier != nil {
		projectID = req.ProjectIdentifier.ProjectID
	}

	session := s.cascades.CreateSession(cascadeID, projectID, model, req.WorkspaceURIs, req.Environment)
	if authHeader := r.Header.Get("Authorization"); authHeader != "" {
		session.AuthToken = strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	}

	prompt := req.InitialPrompt
	if prompt == "" {
		prompt = req.UserPrompt
	}
	if prompt == "" && len(req.Items) > 0 {
		var parts []string
		for _, item := range req.Items {
			if item.Text != "" {
				parts = append(parts, item.Text)
			}
		}
		prompt = strings.Join(parts, "\n")
	}

	if prompt != "" {
		session.AddStep("USER_INPUT", prompt, "", nil)
		if s.CustomTurnHandler != nil {
			s.CustomTurnHandler(ctx, session, prompt)
		}
	}

	resp := StartCascadeResponse{
		CascadeID: cascadeID,
		ExecutionStatus: ExecutionStatus{
			Status: 2, // RUNNING
		},
	}
	writeJSON(w, resp)
}

func (s *Service) handleSendUserCascadeMessage(w http.ResponseWriter, r *http.Request, body []byte) {
	ctx := r.Context()
	var req SendUserCascadeMessageRequest
	_ = json.Unmarshal(body, &req)

	session, ok := s.cascades.GetSession(req.CascadeID)
	if !ok {
		// If not found, try adopting the latest session if available
		summaries := s.cascades.ListSummaries()
		if len(summaries) > 0 {
			session, ok = s.cascades.GetSession(summaries[len(summaries)-1].TrajectoryID)
		}
		if !ok {
			session = s.cascades.CreateSession(req.CascadeID, "default", "gemini-3.8-flash-high", nil, nil)
		}
	}
	if authHeader := r.Header.Get("Authorization"); authHeader != "" {
		session.AuthToken = strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	}

	text := req.Text
	if text == "" {
		text = req.MessageText
	}
	if text == "" && len(req.Items) > 0 {
		var parts []string
		for _, item := range req.Items {
			if item.Text != "" {
				parts = append(parts, item.Text)
			}
		}
		text = strings.Join(parts, "\n")
	}

	if text != "" {
		session.AddStep("USER_INPUT", text, "", nil)
		if s.CustomTurnHandler != nil {
			s.CustomTurnHandler(ctx, session, text)
		}
	}

	resp := SendUserCascadeMessageResponse{
		CascadeID: session.ID,
		ExecutionStatus: ExecutionStatus{
			Status: 2, // RUNNING
		},
	}
	writeJSON(w, resp)
}

func (s *Service) handleGenerateConversationTitle(w http.ResponseWriter, body []byte) {
	var req GenerateConversationTitleRequest
	_ = json.Unmarshal(body, &req)

	title := "Remote Agent Conversation"
	p := strings.TrimSpace(req.Prompt)
	if p != "" {
		words := strings.Fields(p)
		if len(words) > 5 {
			title = strings.Join(words[:5], " ")
		} else {
			title = strings.Join(words, " ")
		}
		if len(title) > 0 {
			title = strings.ToUpper(title[:1]) + title[1:]
		}
	}
	writeJSON(w, GenerateConversationTitleResponse{Title: title})
}

func (s *Service) handleRetrieveUserQuotaSummary(w http.ResponseWriter) {
	writeJSON(w, map[string]any{
		"userQuotas": map[string]any{},
	})
}

func (s *Service) handleCancelCascade(w http.ResponseWriter, body []byte) {
	var req CancelCascadeRequest
	_ = json.Unmarshal(body, &req)

	id := req.CascadeID
	if id == "" {
		id = req.ConversationID
	}

	session, ok := s.cascades.GetSession(id)
	if !ok {
		summaries := s.cascades.ListSummaries()
		if len(summaries) > 0 {
			session, ok = s.cascades.GetSession(summaries[len(summaries)-1].TrajectoryID)
		}
	}
	if ok && session != nil {
		session.CancelTurn()
	}

	writeJSON(w, CancelCascadeResponse{Success: true})
}

func (s *Service) handleStreamAgentStateUpdates(w http.ResponseWriter, r *http.Request, body []byte) {
	conversationID := r.URL.Query().Get("conversationId")
	if conversationID == "" {
		conversationID = r.URL.Query().Get("conversation_id")
	}
	if conversationID == "" && len(body) > 0 {
		var req StreamAgentStateUpdatesRequest
		_ = json.Unmarshal(body, &req)
		conversationID = req.ConversationID
	}

	session, ok := s.cascades.GetSession(conversationID)
	if !ok {
		// If not found, try adopting the latest session if available
		summaries := s.cascades.ListSummaries()
		if len(summaries) > 0 {
			session, ok = s.cascades.GetSession(summaries[len(summaries)-1].TrajectoryID)
		}
		if !ok {
			session = s.cascades.CreateSession(conversationID, "default", "gemini-3.8-flash-high", nil, nil)
		}
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	isSSE := strings.Contains(r.Header.Get("Accept"), "text/event-stream") || r.URL.Query().Get("alt") == "sse"
	if isSSE {
		w.Header().Set("Content-Type", "text/event-stream")
	} else {
		w.Header().Set("Content-Type", "application/json")
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch := session.Subscribe()
	defer session.Unsubscribe(ch)

	notify := r.Context().Done()
	for {
		select {
		case <-notify:
			return
		case frame, open := <-ch:
			if !open {
				return
			}
			data, err := json.Marshal(frame)
			if err != nil {
				continue
			}
			log.Printf("[StreamAgentStateUpdates] Sending frame: %s", string(data))
			if isSSE {
				_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			} else {
				_, _ = fmt.Fprintf(w, "%s\n", data)
			}
			flusher.Flush()
		}
	}
}

func (s *Service) handleGetConversation(w http.ResponseWriter, r *http.Request, body []byte) {
	conversationID := r.URL.Query().Get("conversationId")
	if conversationID == "" {
		conversationID = r.URL.Query().Get("conversation_id")
	}
	if conversationID == "" && len(body) > 0 {
		var req GetConversationRequest
		_ = json.Unmarshal(body, &req)
		conversationID = req.ConversationID
	}

	session, ok := s.cascades.GetSession(conversationID)
	if !ok {
		// If not found, try adopting the latest session if available
		summaries := s.cascades.ListSummaries()
		if len(summaries) > 0 {
			session, ok = s.cascades.GetSession(summaries[len(summaries)-1].TrajectoryID)
		}
	}

	if !ok || session == nil {
		writeJSON(w, GetConversationResponse{
			ConversationID: conversationID,
		})
		return
	}

	session.mu.RLock()
	frame := session.buildStateFrameLocked()
	session.mu.RUnlock()

	writeJSON(w, GetConversationResponse{
		ConversationID:   session.ID,
		Update:           frame.Update,
		ResumeToken:      frame.ResumeToken,
		ModelDisplayName: "Gemini 3.8 Flash (High)",
	})
}

func (s *Service) handleListConversations(w http.ResponseWriter) {
	summaries := s.cascades.ListSummaries()
	writeJSON(w, ListConversationsResponse{
		Conversations: summaries,
	})
}

func (s *Service) handleDeleteConversation(w http.ResponseWriter, body []byte) {
	var req struct {
		ConversationID string `json:"conversationId"`
	}
	_ = json.Unmarshal(body, &req)
	s.cascades.DeleteSession(req.ConversationID)
	writeJSON(w, map[string]any{"success": true})
}

func (s *Service) handleReadFile(w http.ResponseWriter, body []byte) {
	var req ReadFileRequest
	_ = json.Unmarshal(body, &req)

	data, err := os.ReadFile(req.Path)
	if err != nil {
		writeJSON(w, ReadFileResponse{Exists: false})
		return
	}
	writeJSON(w, ReadFileResponse{Content: string(data), Exists: true})
}

func (s *Service) handleReadDir(w http.ResponseWriter, body []byte) {
	var req ReadDirRequest
	_ = json.Unmarshal(body, &req)

	entries, err := os.ReadDir(req.Path)
	if err != nil {
		writeJSON(w, ReadDirResponse{Entries: []DirEntry{}})
		return
	}

	result := make([]DirEntry, 0, len(entries))
	for _, e := range entries {
		info, _ := e.Info()
		size := int64(0)
		if info != nil {
			size = info.Size()
		}
		result = append(result, DirEntry{
			Name:  filepath.Base(e.Name()),
			IsDir: e.IsDir(),
			Size:  size,
		})
	}
	writeJSON(w, ReadDirResponse{Entries: result})
}

func (s *Service) handleGetPermissionSettings(w http.ResponseWriter) {
	s.mu.Lock()
	preset := s.permissionPreset
	s.mu.Unlock()
	if preset == "" {
		preset = "AUTO_APPROVE"
	}
	writeJSON(w, map[string]any{
		"permissionPreset":   preset,
		"artifactReviewMode": "ALWAYS_APPROVE",
	})
}

func (s *Service) handleSetPermissionPreset(w http.ResponseWriter, body []byte) {
	var req struct {
		Preset           string `json:"preset"`
		PermissionPreset string `json:"permissionPreset"`
	}
	if len(body) > 0 {
		_ = json.Unmarshal(body, &req)
	}
	preset := strings.TrimSpace(req.Preset)
	if preset == "" {
		preset = strings.TrimSpace(req.PermissionPreset)
	}
	if preset != "" {
		s.mu.Lock()
		s.permissionPreset = preset
		s.mu.Unlock()
	}
	writeJSON(w, map[string]any{"success": true})
}

func (s *Service) handleFetchConfig(w http.ResponseWriter) {
	bTrue := true
	v2 := "v2"
	resp := FetchConfigResponse{
		ExperimentConfig: ExperimentConfig{
			EnabledExperimentIds: []int64{106260793, 1714244},
			ExperimentFlags: []ExperimentFlag{
				{Name: "enable-remote-ai", BoolValue: &bTrue},
				{Name: "enable-enterprise-remote-ai", BoolValue: &bTrue},
				{Name: "enable-cloud-conversations", BoolValue: &bTrue},
				{Name: "enable-remote-standalone-conversations", BoolValue: &bTrue},
				{Name: "enable-remote-persistent-agents", BoolValue: &bTrue},
				{Name: "enable-remote-battle-mode", BoolValue: &bTrue},
				{Name: "enable-remote-conversation-terminal", BoolValue: &bTrue},
				{Name: "enable-remote-conversation-preview", BoolValue: &bTrue},
				{Name: "remote-control-setting-enabled", BoolValue: &bTrue},
				{Name: "remote-control-webrtc-mesh", BoolValue: &bTrue},
				{Name: "remote-control-version", StringValue: &v2},
			},
		},
		AvailableExperiences: []Experience{
			{
				Name: "agent",
				Model: ModelConfig{
					ModelID:     "gemini-3.8-flash-high",
					DisplayName: "Gemini 3.8 Flash (High)",
				},
			},
		},
		AvailableModels: []ModelConfig{
			{
				ModelID:     "gemini-3.8-flash-high",
				DisplayName: "Gemini 3.8 Flash (High)",
			},
		},
		AgySurfaceConfig: &SurfaceConfig{
			AgentExperiences: SurfaceConfigExperienceGroup{
				ExperienceNames:   []string{"agent"},
				DefaultExperience: "agent",
			},
		},
	}
	writeJSON(w, resp)
}

func (s *Service) handleListExperiments(w http.ResponseWriter) {
	bTrue := true
	resp := ListExperimentsResponse{
		ExperimentIDs: []int64{106260793, 1714244},
		Flags: []CloudCodeFlag{
			{Name: "enable-remote-ai", BoolValue: &bTrue},
			{Name: "enable-enterprise-remote-ai", BoolValue: &bTrue},
			{Name: "enable-cloud-conversations", BoolValue: &bTrue},
			{Name: "enable-remote-standalone-conversations", BoolValue: &bTrue},
			{Name: "enable-remote-persistent-agents", BoolValue: &bTrue},
			{Name: "enable-remote-battle-mode", BoolValue: &bTrue},
			{Name: "enable-remote-conversation-terminal", BoolValue: &bTrue},
			{Name: "enable-remote-conversation-preview", BoolValue: &bTrue},
			{Name: "remote-control-setting-enabled", BoolValue: &bTrue},
			{Name: "remote-control-webrtc-mesh", BoolValue: &bTrue},
			{Name: "remote-control-version", StringValue: "v2"},
		},
	}
	writeJSON(w, resp)
}

func (s *Service) handleLoadCodeAssist(w http.ResponseWriter) {
	resp := map[string]any{
		"currentTier": map[string]any{
			"id":       "tier-pro",
			"name":     "Google AI Pro",
			"tierType": "USER_TIER_PRO",
		},
		"allowedTiers": []map[string]any{
			{
				"id":       "tier-pro",
				"name":     "Google AI Pro",
				"tierType": "USER_TIER_PRO",
			},
		},
		"cloudaicompanionProject": "aicode-consumers",
		"gcpManaged":              false,
	}
	writeJSON(w, resp)
}

func (s *Service) handleOnboardUser(w http.ResponseWriter) {
	writeJSON(w, map[string]any{
		"status": map[string]any{
			"statusCode": "STATUS_CODE_SUCCESS",
		},
	})
}

func (s *Service) handleFetchAvailableModels(w http.ResponseWriter) {
	writeJSON(w, map[string]any{
		"models": map[string]any{
			"gemini-3.8-flash-high": map[string]any{
				"displayName":              "Gemini 3.8 Flash (High)",
				"supportsAdaptiveThinking": true,
			},
		},
		"defaultAgentModelId": "gemini-3.8-flash-high",
	})
}

func (s *Service) handleCreateProject(w http.ResponseWriter, body []byte) {
	var req CreateProjectRequest
	_ = json.Unmarshal(body, &req)

	project := req.Project
	if project.ProjectID == "" {
		if req.ProjectID != "" {
			project.ProjectID = req.ProjectID
		} else {
			project.ProjectID = fmt.Sprintf("project-%d", time.Now().UnixNano())
		}
	}
	if project.Title == "" {
		project.Title = "CLI Project"
	}
	project.UpdateTimeMicros = time.Now().UnixMicro()
	if project.Resources == nil {
		project.Resources = []ProjectResource{}
	}

	s.mu.Lock()
	s.projects[project.ProjectID] = project
	s.mu.Unlock()

	writeJSON(w, CreateProjectResponse{Project: project})
}

func (s *Service) seedProjectsLocked() {
	home, err := os.UserHomeDir()
	if err == nil {
		cacheFile := filepath.Join(home, ".gemini", "antigravity-cli", "cache", "default_cloud_project_ids.json")
		if data, err := os.ReadFile(cacheFile); err == nil {
			var m map[string]string
			if json.Unmarshal(data, &m) == nil {
				for email, pid := range m {
					pid = strings.TrimSpace(pid)
					if pid != "" {
						if _, ok := s.projects[pid]; !ok {
							s.projects[pid] = ProjectConfig{
								ProjectID:        pid,
								Title:            fmt.Sprintf("CLI Project (%s)", email),
								UpdateTimeMicros: time.Now().UnixMicro(),
								Archived:         false,
								Resources:        []ProjectResource{},
							}
						}
					}
				}
			}
		}
	}
	// Fallback project IDs
	for _, id := range []string{
		"f3168bbc-a23d-499d-b9c0-97971fc2ead1",
		"5375b8e0-06d4-4393-8580-2f833e111343",
		"8fd5b36d-dcf2-4085-b438-bc6005fffd88",
		"7054c718-c9a4-4f3b-8356-851bfa69de31",
	} {
		if _, ok := s.projects[id]; !ok {
			s.projects[id] = ProjectConfig{
				ProjectID:        id,
				Title:            "CLI Project",
				UpdateTimeMicros: time.Now().UnixMicro(),
				Archived:         false,
				Resources:        []ProjectResource{},
			}
		}
	}
}

// accountEmailForRequest resolves the caller's account. The bearer agy sends
// belongs to the signed-in account, which may not be the token
// auth.EffectiveUserEmail() picks when several accounts are cached on this
// machine. Match it against the local stores first, then fall back to the
// process's own account.
func accountEmailForRequest(r *http.Request) string {
	bearer := ""
	if r != nil {
		bearer = strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	}
	if bearer != "" {
		if tok, err := auth.LoadKeychainToken(); err == nil && tok != nil && tok.Token.AccessToken == bearer {
			if em := tok.UserEmail(); em != "" {
				return em
			}
		}
		for _, p := range auth.AllTokenPaths() {
			tok, err := auth.LoadAntigravityTokenFrom(p)
			if err == nil && tok != nil && tok.Token.AccessToken == bearer {
				if em := tok.UserEmail(); em != "" {
					return em
				}
			}
		}
	}
	return strings.TrimSpace(auth.EffectiveUserEmail())
}

// projectForEmailLocked returns the cached cloud project of email. The real
// service lists only the caller's projects.
func (s *Service) projectForEmailLocked(email string) (ProjectConfig, bool) {
	email = strings.TrimSpace(email)
	if email == "" {
		return ProjectConfig{}, false
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ProjectConfig{}, false
	}
	data, err := os.ReadFile(filepath.Join(home, ".gemini", "antigravity-cli", "cache", "default_cloud_project_ids.json"))
	if err != nil {
		return ProjectConfig{}, false
	}
	var m map[string]string
	if json.Unmarshal(data, &m) != nil {
		return ProjectConfig{}, false
	}
	pid := strings.TrimSpace(m[email])
	if pid == "" {
		return ProjectConfig{}, false
	}
	p, ok := s.projects[pid]
	if !ok {
		p = ProjectConfig{
			ProjectID:        pid,
			Title:            fmt.Sprintf("CLI Project (%s)", email),
			UpdateTimeMicros: time.Now().UnixMicro(),
			Archived:         false,
			Resources:        []ProjectResource{},
		}
		s.projects[pid] = p
	}
	return p, true
}

func (s *Service) handleListProjects(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.seedProjectsLocked()

	// agy picks a project from this list to resolve its cloud project. The
	// real service only returns the caller's projects; returning every
	// cached account here made agy pick another account's project (for
	// example f3168bbc instead of the signed-in 5375b8e0), which then
	// failed project resolution.
	if p, ok := s.projectForEmailLocked(accountEmailForRequest(r)); ok {
		writeJSON(w, ListProjectsResponse{Projects: []ProjectConfig{p}})
		return
	}

	list := make([]ProjectConfig, 0, len(s.projects))
	for _, p := range s.projects {
		list = append(list, p)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ProjectID < list[j].ProjectID })
	writeJSON(w, ListProjectsResponse{Projects: list})
}

func (s *Service) handleReadProject(w http.ResponseWriter, r *http.Request, body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.seedProjectsLocked()

	var req struct {
		ID        string `json:"id"`
		ProjectID string `json:"projectId"`
	}
	_ = json.Unmarshal(body, &req)

	projID := req.ID
	if projID == "" {
		projID = req.ProjectID
	}
	if projID == "" {
		projID = r.URL.Query().Get("id")
	}
	if projID == "" {
		projID = r.URL.Query().Get("projectId")
	}

	p, ok := s.projects[projID]
	if !ok {
		if projID == "" {
			projID = "project-default"
		}
		p = ProjectConfig{
			ProjectID:        projID,
			Title:            "CLI Project",
			UpdateTimeMicros: time.Now().UnixMicro(),
			Archived:         false,
			Resources:        []ProjectResource{},
		}
		s.projects[projID] = p
	}

	writeJSON(w, map[string]any{
		"project": p,
	})
}

func (s *Service) handleUpsertConversationConfig(w http.ResponseWriter, body []byte) {
	var req UpsertConversationConfigRequest
	_ = json.Unmarshal(body, &req)

	writeJSON(w, UpsertConversationConfigResponse{Config: req.Config})
}

func (s *Service) handleListPlugins(w http.ResponseWriter) {
	writeJSON(w, ListPluginsResponse{Plugins: []map[string]any{}})
}

func (s *Service) handleListResourceProviders(w http.ResponseWriter) {
	writeJSON(w, ListResourceProvidersResponse{ResourceProviders: []map[string]any{}})
}

func (s *Service) handleListProjectResources(w http.ResponseWriter) {
	writeJSON(w, ListProjectResourcesResponse{Resources: []ProjectResource{}})
}

func (s *Service) handleListAvailableResources(w http.ResponseWriter) {
	writeJSON(w, map[string]any{"resources": []any{}})
}

func (s *Service) handleGetVersionControlState(w http.ResponseWriter) {
	writeJSON(w, map[string]any{
		"branches":      []string{"main"},
		"currentBranch": "main",
	})
}

func (s *Service) handleGetTurnDiff(w http.ResponseWriter, body []byte) {
	var req GetTurnDiffRequest
	_ = json.Unmarshal(body, &req)

	session, ok := s.cascades.GetSession(req.ConversationID)
	if !ok {
		writeJSON(w, GetTurnDiffResponse{
			FileDiffs:      map[string]FileDiffData{},
			TurnStartIndex: int32(req.StepIndex),
		})
		return
	}
	files, additions, deletions := workingCopyDiff(sessionWorkspace(session))
	writeJSON(w, GetTurnDiffResponse{
		FileDiffs:      files,
		TotalAdditions: additions,
		TotalDeletions: deletions,
		TurnStartIndex: int32(req.StepIndex),
	})
}

func (s *Service) handleGetCodeActionDiff(w http.ResponseWriter, body []byte) {
	var req GetCodeActionDiffRequest
	if len(body) > 0 {
		_ = json.Unmarshal(body, &req)
	}
	session, ok := s.cascades.GetSession(req.ConversationID)
	if !ok || session == nil {
		writeJSON(w, GetCodeActionDiffResponse{Diff: map[string]any{}})
		return
	}
	steps := session.StepsSnapshot()
	if req.StepIndex < 0 || int(req.StepIndex) >= len(steps) {
		writeJSON(w, GetCodeActionDiffResponse{Diff: map[string]any{}})
		return
	}
	step := steps[req.StepIndex]
	if step.Type != "FILE_CHANGE" || len(step.DiffLines) == 0 {
		writeJSON(w, GetCodeActionDiffResponse{Diff: map[string]any{}})
		return
	}
	lines := make([]any, 0, len(step.DiffLines))
	for _, line := range step.DiffLines {
		lines = append(lines, map[string]any{"type": line.Type, "text": line.Text})
	}
	writeJSON(w, GetCodeActionDiffResponse{Diff: map[string]any{
		"unifiedDiff": map[string]any{"lines": lines},
	}})
}

// handleCascadeUserInteraction resolves the step a turn is parked on.
//
// The reference's own payload names the cascade, the trajectory and the
// step index, and carries one oneof member — `permission` or
// `askQuestion`. The legacy dashboard spelled the cascade field
// `conversationId`, so both are accepted.
func (s *Service) handleCascadeUserInteraction(w http.ResponseWriter, body []byte) {
	var req HandleCascadeUserInteractionRequest
	if len(body) > 0 {
		_ = json.Unmarshal(body, &req)
	}
	if req.Interaction == nil {
		writeJSON(w, HandleCascadeUserInteractionResponse{})
		return
	}

	id := strings.TrimSpace(req.CascadeID)
	if id == "" {
		id = strings.TrimSpace(req.ConversationID)
	}
	session, ok := s.cascades.GetSession(id)
	if !ok {
		// The answer names the cascade the waiting step belongs to; when
		// only one conversation is live, adopt it (the legacy dashboard
		// sent no id at all).
		if summaries := s.cascades.ListSummaries(); len(summaries) == 1 {
			session, ok = s.cascades.GetSession(summaries[0].TrajectoryID)
		}
	}

	answer, response := interactionAnswer(req.Interaction)
	if ok && session != nil {
		session.ResolveInteraction(req.Interaction.StepIndex, answer, response)
	}
	writeJSON(w, HandleCascadeUserInteractionResponse{})
}

// interactionAnswer turns the client's answer into the tool's own answer
// and into the response the completed step keeps.
func interactionAnswer(in *CascadeUserInteraction) (interaction.Answer, any) {
	if in == nil {
		return interaction.Answer{Cancelled: true}, nil
	}
	if in.Permission != nil {
		return interaction.Answer{Allowed: in.Permission.Allow}, in
	}
	if in.AskQuestion != nil {
		if in.AskQuestion.Cancelled {
			return interaction.Answer{Cancelled: true}, in
		}
		var parts []string
		for _, r := range in.AskQuestion.Responses {
			var picked []string
			for _, id := range r.SelectedOptionIDs {
				for _, opt := range r.Options {
					if opt.ID == id {
						picked = append(picked, opt.Text)
						break
					}
				}
			}
			text := strings.Join(picked, ", ")
			if r.WriteInResponse != "" {
				if text != "" {
					text += ", "
				}
				text += r.WriteInResponse + " (write-in)"
			}
			if text == "" && r.Skipped {
				text = "(skipped)"
			}
			parts = append(parts, text)
		}
		return interaction.Answer{Text: strings.Join(parts, "\n")}, in
	}
	return interaction.Answer{Cancelled: true}, in
}

func (s *Service) handleUploadProjectFile(w http.ResponseWriter, body []byte) {
	var req UploadProjectFileRequest
	_ = json.Unmarshal(body, &req)

	data := req.Content
	if len(data) == 0 {
		data = req.InlineContent
	}
	name := strings.TrimSpace(req.FileName)
	if name == "" {
		name = strings.TrimSpace(req.Title)
	}
	if name == "" {
		name = strings.TrimSpace(req.Path)
	}
	name = filepath.Base(name)
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "upload"
	}
	projectID := strings.TrimSpace(req.ProjectID)
	if projectID == "" {
		projectID = "default"
	}
	dir := filepath.Join(os.TempDir(), "pigasus-project-uploads", strings.NewReplacer("/", "_", "\\", "_").Replace(projectID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resource := ProjectResource{
		Type:        "FILE",
		ResourceID:  name,
		DisplayName: name,
		URL:         fileURI(path),
		ProjectID:   projectID,
		MIMEType:    req.MIMEType,
		SizeBytes:   int64(len(data)),
		MountPath:   path,
	}
	if project, ok := s.projects[projectID]; ok {
		project.Resources = append(project.Resources, resource)
		s.projects[projectID] = project
	}
	writeJSON(w, UploadProjectFileResponse{Resource: resource})
}

func (s *Service) handleUpdateProject(w http.ResponseWriter, body []byte) {
	var req struct {
		Project ProjectConfig `json:"project"`
	}
	_ = json.Unmarshal(body, &req)
	s.mu.Lock()
	if req.Project.ProjectID != "" {
		s.projects[req.Project.ProjectID] = req.Project
	}
	s.mu.Unlock()
	writeJSON(w, req.Project)
}

func (s *Service) handleDeleteProject(w http.ResponseWriter, body []byte) {
	var req struct {
		ProjectID string `json:"projectId"`
	}
	_ = json.Unmarshal(body, &req)
	s.mu.Lock()
	delete(s.projects, req.ProjectID)
	s.mu.Unlock()
	writeJSON(w, map[string]any{"success": true})
}

func (s *Service) handleSetArtifactReviewMode(w http.ResponseWriter, body []byte) {
	var req SetArtifactReviewModeRequest
	_ = json.Unmarshal(body, &req)
	writeJSON(w, SetArtifactReviewModeResponse{
		Settings: map[string]any{
			"artifactReviewMode": req.ArtifactReviewMode,
		},
	})
}

func (s *Service) handleAddPermissionGrants(w http.ResponseWriter, _ []byte) {
	writeJSON(w, map[string]any{"success": true})
}

func (s *Service) handleRemovePermissionGrants(w http.ResponseWriter, _ []byte) {
	writeJSON(w, map[string]any{"success": true})
}

func (s *Service) handleInstallPlugin(w http.ResponseWriter, _ []byte) {
	writeJSON(w, map[string]any{"success": true})
}

func (s *Service) handleUninstallPlugin(w http.ResponseWriter, _ []byte) {
	writeJSON(w, map[string]any{"success": true})
}

func (s *Service) handleListPluginInstallations(w http.ResponseWriter) {
	writeJSON(w, map[string]any{"installations": []any{}})
}

func (s *Service) handleDisconnectPlugin(w http.ResponseWriter, _ []byte) {
	writeJSON(w, map[string]any{"success": true})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(v)
}
