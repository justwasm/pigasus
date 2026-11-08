package mockremoteagent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMockRemoteAgentServer_FullLifecycle(t *testing.T) {
	// 1. Boot local mock server
	srv, err := NewServer()
	if err != nil {
		t.Fatalf("failed to start mock server: %v", err)
	}
	defer srv.Close()

	client := NewClient(srv.URL())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 2. EnsureEnvironment Handshake
	envResp, err := client.EnsureEnvironment(ctx, EnsureEnvironmentRequest{
		ExecutionIdentity: "test-user@google.com",
	})
	if err != nil {
		t.Fatalf("EnsureEnvironment failed: %v", err)
	}
	if envResp.Environment.ConnectionState != "CONNECTION_STATE_CONNECTED" {
		t.Errorf("expected CONNECTION_STATE_CONNECTED, got %s", envResp.Environment.ConnectionState)
	}

	// 3. ListAvailableModels
	modelsResp, err := client.ListAvailableModels(ctx)
	if err != nil {
		t.Fatalf("ListAvailableModels failed: %v", err)
	}
	if len(modelsResp.Models) == 0 {
		t.Errorf("expected non-empty models list")
	}

	// 4. ListSlashCommands
	commandsResp, err := client.ListSlashCommands(ctx)
	if err != nil {
		t.Fatalf("ListSlashCommands failed: %v", err)
	}
	if len(commandsResp.SlashCommands) == 0 {
		t.Errorf("expected non-empty slash commands list")
	}

	// 5. StartCascade
	startResp, err := client.StartCascade(ctx, StartCascadeRequest{
		InitialPrompt: "Hello remote agent",
		ProjectID:     "test-project-123",
		WorkspaceURIs: []string{"file:///workspace/test"},
	})
	if err != nil {
		t.Fatalf("StartCascade failed: %v", err)
	}
	if startResp.CascadeID == "" {
		t.Fatalf("expected non-empty cascadeId")
	}
	cascadeID := startResp.CascadeID

	// 6. Connect to StreamAgentStateUpdates
	frames, errs, err := client.StreamAgentStateUpdates(ctx, cascadeID)
	if err != nil {
		t.Fatalf("StreamAgentStateUpdates connect failed: %v", err)
	}

	// First frame should arrive promptly (initial snapshot)
	select {
	case frame, ok := <-frames:
		if !ok {
			t.Fatalf("stream closed prematurely")
		}
		update, _ := frame.Update["mainTrajectoryUpdate"].(map[string]any)
		stepsUpdate, _ := update["stepsUpdate"].(map[string]any)
		totalLength, _ := stepsUpdate["totalLength"].(float64)
		if totalLength < 1 {
			t.Errorf("expected at least initial prompt step, got %v", totalLength)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for initial stream frame")
	}

	// 7. Send User Message driving simulated agent turn
	sendResp, err := client.SendUserCascadeMessage(ctx, cascadeID, "Please list the files")
	if err != nil {
		t.Fatalf("SendUserCascadeMessage failed: %v", err)
	}
	if sendResp.CascadeID != cascadeID {
		t.Errorf("expected cascadeId %s, got %s", cascadeID, sendResp.CascadeID)
	}

	// 8. Observe streaming frames for: User input -> Thought -> Tool Call -> Response -> Idle
	turnCompleted := false
	sawToolCall := false
	sawThought := false

	watchTimer := time.After(4 * time.Second)
	for !turnCompleted {
		select {
		case frame, ok := <-frames:
			if !ok {
				t.Fatalf("stream closed before turn finished")
			}
			update, _ := frame.Update["mainTrajectoryUpdate"].(map[string]any)
			stepsUpdate, _ := update["stepsUpdate"].(map[string]any)
			steps, _ := stepsUpdate["steps"].([]any)
			for _, st := range steps {
				stepMap, ok := st.(map[string]any)
				if !ok {
					continue
				}
				stepType, _ := stepMap["type"].(string)
				stepTypeNum, _ := stepMap["type"].(float64)
				pr, _ := stepMap["plannerResponse"].(map[string]any)
				thinking, _ := pr["thinking"].(string)

				if thinking != "" {
					sawThought = true
				}
				if stepType == "CORTEX_STEP_TYPE_TOOL_CALL" || stepType == "CORTEX_STEP_TYPE_GENERIC" || stepType == "CORTEX_STEP_TYPE_RUN_COMMAND" || int(stepTypeNum) == 132 || int(stepTypeNum) == 140 || int(stepTypeNum) == 21 || stepMap["runCommand"] != nil {
					sawToolCall = true
				}
			}

			status, _ := frame.Update["status"].(string)
			statusNum, _ := frame.Update["status"].(float64)
			isIdle := status == "IDLE" || status == "CASCADE_RUN_STATUS_IDLE" || int(statusNum) == 1
			if isIdle && sawThought && sawToolCall {
				turnCompleted = true
			}
		case err := <-errs:
			if err != nil {
				t.Fatalf("stream error: %v", err)
			}
		case <-watchTimer:
			t.Fatalf("timeout waiting for complete agent turn (sawThought=%v, sawToolCall=%v)", sawThought, sawToolCall)
		}
	}

	// 9. Verify GetConversation returns consistent steps
	convResp, err := client.GetConversation(ctx, cascadeID)
	if err != nil {
		t.Fatalf("GetConversation failed: %v", err)
	}
	if len(convResp.Steps()) < 4 {
		t.Errorf("expected at least 4 trajectory steps, got %d", len(convResp.Steps()))
	}

	// 10. Verify ListConversations
	listResp, err := client.ListConversations(ctx)
	if err != nil {
		t.Fatalf("ListConversations failed: %v", err)
	}
	found := false
	for _, c := range listResp.Conversations {
		if c.TrajectoryID == cascadeID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("conversation %s not found in ListConversations", cascadeID)
	}

	// 11. Verify ReadFile and ReadDir
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	_ = os.WriteFile(testFile, []byte("hello from mock remote"), 0644)

	readFileResp, err := client.ReadFile(ctx, testFile)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if !readFileResp.Exists || readFileResp.Content != "hello from mock remote" {
		t.Errorf("ReadFile unexpected content: %+v", readFileResp)
	}

	readDirResp, err := client.ReadDir(ctx, tmpDir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(readDirResp.Entries) != 1 || readDirResp.Entries[0].Name != "test.txt" {
		t.Errorf("ReadDir unexpected entries: %+v", readDirResp.Entries)
	}

	// 12. CancelCascade
	cancelResp, err := client.CancelCascade(ctx, cascadeID)
	if err != nil {
		t.Fatalf("CancelCascade failed: %v", err)
	}
	if !cancelResp.Success {
		t.Errorf("expected cancel success")
	}
}

func TestMockRemoteAgentServer_CustomTurnHandler(t *testing.T) {
	customReply := "Custom Turn Handler Replied Successfully"
	srv, err := NewServer(WithCustomTurnHandler(func(ctx context.Context, session *CascadeSession, userPrompt string) {
		turnCtx, _ := session.BeginTurn()
		go func() {
			select {
			case <-turnCtx.Done():
				return
			case <-time.After(10 * time.Millisecond):
			}
			session.AddStep("PLANNER_RESPONSE", customReply, "Deep Thought", nil)
			session.SetStatus("IDLE")
		}()
	}))
	if err != nil {
		t.Fatalf("failed to start mock server: %v", err)
	}
	defer srv.Close()

	client := NewClient(srv.URL())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	startResp, err := client.StartCascade(ctx, StartCascadeRequest{
		InitialPrompt: "Hello custom",
	})
	if err != nil {
		t.Fatalf("StartCascade: %v", err)
	}

	// Wait briefly for custom turn to complete
	time.Sleep(50 * time.Millisecond)

	convResp, err := client.GetConversation(ctx, startResp.CascadeID)
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	steps := convResp.Steps()
	if len(steps) < 2 {
		t.Fatalf("expected at least 2 steps, got %d", len(steps))
	}
	pr, _ := steps[1]["plannerResponse"].(map[string]any)
	if pr["response"] != customReply {
		t.Errorf("expected %q, got %q", customReply, pr["response"])
	}
}

func TestMockRemoteAgentServer_Cancellation(t *testing.T) {
	srv, err := NewServer(WithCustomTurnHandler(func(ctx context.Context, session *CascadeSession, userPrompt string) {
		turnCtx, _ := session.BeginTurn()
		go func() {
			// Long-running turn
			select {
			case <-turnCtx.Done():
				return
			case <-time.After(2 * time.Second):
				session.AddStep("PLANNER_RESPONSE", "Should not be reached", "", nil)
				session.SetStatus("IDLE")
			}
		}()
	}))
	if err != nil {
		t.Fatalf("failed to start mock server: %v", err)
	}
	defer srv.Close()

	client := NewClient(srv.URL())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	startResp, err := client.StartCascade(ctx, StartCascadeRequest{
		InitialPrompt: "Long task",
	})
	if err != nil {
		t.Fatalf("StartCascade: %v", err)
	}

	session, ok := srv.Cascades().GetSession(startResp.CascadeID)
	if !ok {
		t.Fatalf("session not found")
	}

	time.Sleep(20 * time.Millisecond)
	if session.Status != "RUNNING" {
		t.Errorf("expected RUNNING, got %s", session.Status)
	}

	// Cancel cascade
	_, err = client.CancelCascade(ctx, startResp.CascadeID)
	if err != nil {
		t.Fatalf("CancelCascade: %v", err)
	}

	time.Sleep(20 * time.Millisecond)
	if session.Status != "IDLE" {
		t.Errorf("expected IDLE after cancel, got %s", session.Status)
	}
}

func TestMockRemoteAgentServer_AICodeEndpointURLCompatibility(t *testing.T) {
	srv, err := NewServer()
	if err != nil {
		t.Fatalf("failed to start mock server: %v", err)
	}
	defer srv.Close()

	// agy with AICODE_ENDPOINT_URL constructs URLs formatted as:
	// ${AICODE_ENDPOINT_URL}/v1alpha/remote:<Method>
	rawURL := srv.URL() + "/v1alpha/remote:EnsureEnvironment"
	resp, err := http.Post(rawURL, "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("POST to /v1alpha/remote:EnsureEnvironment failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var envResp EnsureEnvironmentResponse
	if err := json.NewDecoder(resp.Body).Decode(&envResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envResp.Environment.ConnectionState != "CONNECTION_STATE_CONNECTED" {
		t.Errorf("expected CONNECTION_STATE_CONNECTED, got %s", envResp.Environment.ConnectionState)
	}
}

func TestMockRemoteAgentServer_MendelAndCloudCodeEndpoints(t *testing.T) {
	srv, err := NewServer()
	if err != nil {
		t.Fatalf("failed to start mock server: %v", err)
	}
	defer srv.Close()

	// 1. Test FetchConfig
	req, err := http.NewRequestWithContext(context.Background(), "GET", srv.URL()+"/v1alpha:fetchConfig", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /v1alpha:fetchConfig failed: %v", err)
	}
	defer resp.Body.Close()

	var fetchResp FetchConfigResponse
	if err := json.NewDecoder(resp.Body).Decode(&fetchResp); err != nil {
		t.Fatalf("decode FetchConfigResponse: %v", err)
	}

	foundRemoteAI := false
	for _, flag := range fetchResp.ExperimentConfig.ExperimentFlags {
		if flag.Name == "enable-remote-ai" && flag.BoolValue != nil && *flag.BoolValue {
			foundRemoteAI = true
			break
		}
	}
	if !foundRemoteAI {
		t.Errorf("expected enable-remote-ai: true in FetchConfigResponse")
	}

	// 2. Test ListExperiments
	resp2, err := http.Post(srv.URL()+"/v1internal:listExperiments", "application/json", strings.NewReader(`{"project":"aicode-consumers"}`))
	if err != nil {
		t.Fatalf("POST /v1internal:listExperiments failed: %v", err)
	}
	defer resp2.Body.Close()

	var listExpResp ListExperimentsResponse
	if err := json.NewDecoder(resp2.Body).Decode(&listExpResp); err != nil {
		t.Fatalf("decode ListExperimentsResponse: %v", err)
	}

	foundListExpRemoteAI := false
	for _, flag := range listExpResp.Flags {
		if flag.Name == "enable-remote-ai" && flag.BoolValue != nil && *flag.BoolValue {
			foundListExpRemoteAI = true
			break
		}
	}
	if !foundListExpRemoteAI {
		t.Errorf("expected enable-remote-ai: true in ListExperimentsResponse")
	}

	// 3. Test LoadCodeAssist
	resp3, err := http.Post(srv.URL()+"/v1internal:loadCodeAssist", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("POST /v1internal:loadCodeAssist failed: %v", err)
	}
	defer resp3.Body.Close()

	var loadResp map[string]any
	if err := json.NewDecoder(resp3.Body).Decode(&loadResp); err != nil {
		t.Fatalf("decode LoadCodeAssistResponse: %v", err)
	}
	if loadResp["currentTier"] == nil {
		t.Errorf("expected currentTier in LoadCodeAssistResponse")
	}
}

func TestMockRemoteAgentServer_ExtendedRPCs(t *testing.T) {
	srv, err := NewServer()
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	post := func(endpoint string, reqBody string) *http.Response {
		url := fmt.Sprintf("%s/google.gca.aicode.v1main.JetskiRemoteAgentProxyService/%s", srv.URL(), endpoint)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(reqBody))
		if err != nil {
			t.Fatalf("create request %s: %v", endpoint, err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("do request %s: %v", endpoint, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("endpoint %s status code: %d", endpoint, resp.StatusCode)
		}
		return resp
	}

	// 1. GetTurnDiff
	resp := post("GetTurnDiff", `{"conversationId":"conv-1","stepIndex":1}`)
	var turnDiffResp GetTurnDiffResponse
	_ = json.NewDecoder(resp.Body).Decode(&turnDiffResp)
	resp.Body.Close()
	if turnDiffResp.TurnStartIndex != 1 {
		t.Errorf("expected TurnStartIndex 1, got %d", turnDiffResp.TurnStartIndex)
	}

	// 2. GetCodeActionDiff
	resp = post("GetCodeActionDiff", `{"conversationId":"conv-1","stepIndex":1}`)
	var codeDiffResp GetCodeActionDiffResponse
	_ = json.NewDecoder(resp.Body).Decode(&codeDiffResp)
	resp.Body.Close()

	// 3. GenerateConversationTitle
	resp = post("GenerateConversationTitle", `{"prompt":"implement mock remote agent server"}`)
	var titleResp GenerateConversationTitleResponse
	_ = json.NewDecoder(resp.Body).Decode(&titleResp)
	resp.Body.Close()
	if !strings.Contains(titleResp.Title, "Implement") {
		t.Errorf("expected Title to contain 'Implement', got %q", titleResp.Title)
	}

	// 4. HandleCascadeUserInteraction
	resp = post("HandleCascadeUserInteraction", `{"conversationId":"conv-1","interaction":{"approved":true}}`)
	resp.Body.Close()

	// 5. CreateTerminal & ListTerminals
	resp = post("CreateTerminal", `{"conversationId":"conv-1"}`)
	var termResp CreateTerminalResponse
	_ = json.NewDecoder(resp.Body).Decode(&termResp)
	resp.Body.Close()
	if termResp.Terminal.TerminalID == "" {
		t.Errorf("expected non-empty TerminalID")
	}

	resp = post("ListTerminals", `{}`)
	var listTermsResp ListTerminalsResponse
	_ = json.NewDecoder(resp.Body).Decode(&listTermsResp)
	resp.Body.Close()
	if len(listTermsResp.Terminals) == 0 {
		t.Errorf("expected at least one terminal")
	}

	// 6. SetArtifactReviewMode
	resp = post("SetArtifactReviewMode", `{"artifactReviewMode":"ALWAYS_APPROVE"}`)
	var reviewResp SetArtifactReviewModeResponse
	_ = json.NewDecoder(resp.Body).Decode(&reviewResp)
	resp.Body.Close()
	if reviewResp.Settings["artifactReviewMode"] != "ALWAYS_APPROVE" {
		t.Errorf("expected ALWAYS_APPROVE, got %v", reviewResp.Settings["artifactReviewMode"])
	}

	// 7. Triggers
	resp = post("ListTriggerOptions", `{}`)
	var trigOptResp ListTriggerOptionsResponse
	_ = json.NewDecoder(resp.Body).Decode(&trigOptResp)
	resp.Body.Close()
	if len(trigOptResp.TriggerOptions) == 0 {
		t.Errorf("expected at least one trigger option")
	}

	resp = post("CreateTrigger", `{"trigger":{"name":"auto-test"}}`)
	var createTrigResp CreateTriggerResponse
	_ = json.NewDecoder(resp.Body).Decode(&createTrigResp)
	resp.Body.Close()
	if createTrigResp.Trigger.TriggerID == "" {
		t.Errorf("expected generated trigger ID")
	}

	// 8. UploadProjectFile
	resp = post("UploadProjectFile", `{"projectId":"proj-1","path":"main.go"}`)
	var uploadResp UploadProjectFileResponse
	_ = json.NewDecoder(resp.Body).Decode(&uploadResp)
	resp.Body.Close()
	if uploadResp.Resource.ResourceID != "main.go" {
		t.Errorf("expected resourceId 'main.go', got %q", uploadResp.Resource.ResourceID)
	}

	// 9. UpdateProject & DeleteProject
	resp = post("UpdateProject", `{"project":{"projectId":"proj-update","title":"Updated Title"}}`)
	resp.Body.Close()

	resp = post("DeleteProject", `{"projectId":"proj-update"}`)
	resp.Body.Close()

	// 10. Plugins
	resp = post("InstallPlugin", `{"pluginId":"test-plugin"}`)
	resp.Body.Close()
	resp = post("ListPluginInstallations", `{}`)
	resp.Body.Close()
	resp = post("UninstallPlugin", `{"pluginId":"test-plugin"}`)
	resp.Body.Close()
}

func TestMockRemoteAgentServer_RealCommandExecution(t *testing.T) {
	srv, err := NewServer()
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	defer srv.Close()

	client := NewClient(srv.URL())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start cascade with current workspace directory
	wd, _ := os.Getwd()
	wsURI := "file://" + wd
	startResp, err := client.StartCascade(ctx, StartCascadeRequest{
		InitialPrompt: "run echo 'TEST_EXECUTION_OK'",
		WorkspaceURIs: []string{wsURI},
	})
	if err != nil {
		t.Fatalf("StartCascade failed: %v", err)
	}
	cascadeID := startResp.CascadeID

	// Stream updates and verify command executed
	frames, errs, err := client.StreamAgentStateUpdates(ctx, cascadeID)
	if err != nil {
		t.Fatalf("StreamAgentStateUpdates failed: %v", err)
	}

	foundCommandOutput := false
	watchTimer := time.After(4 * time.Second)

	for !foundCommandOutput {
		select {
		case frame := <-frames:
			steps := frame.Steps()
			for _, stepMap := range steps {
				if rc, ok := stepMap["runCommand"].(map[string]any); ok {
					if comb, ok := rc["combinedOutput"].(map[string]any); ok {
						if full, ok := comb["full"].(string); ok && strings.Contains(full, "TEST_EXECUTION_OK") {
							foundCommandOutput = true
						}
					}
				}
			}
		case err := <-errs:
			if err != nil {
				t.Fatalf("stream error: %v", err)
			}
		case <-watchTimer:
			t.Fatalf("timeout waiting for command output with TEST_EXECUTION_OK")
		}
	}
}
