package remotecontrol

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/lsproto"
	"github.com/justwasm/pigasus/remotetransport"
)

// TestLSAdditionalMethodsCoverage verifies that all secondary domain
// RPC methods are reachable and return valid responses.
func TestLSAdditionalMethodsCoverage(t *testing.T) {
	d := newLSConversationDispatcher(t, "conv-additional-test")
	ctx := context.Background()

	testCases := []struct {
		name     string
		checkKey string
	}{
		// VCS
		{name: "GitStage"},
		{name: "GitCommit", checkKey: "commitId"},
		{name: "GitPush", checkKey: "pushMode"},
		{name: "GetCommitDetails", checkKey: "changedFiles"},
		{name: "GenerateCommitMessage", checkKey: "commitMessage"},
		{name: "GetWorkspaceChangelists", checkKey: "workspaces"},

		// Code Search & Context
		{name: "SearchCode", checkKey: "results"},
		{name: "SearchContext", checkKey: "items"},
		{name: "ResolveContextUri", checkKey: "ok"},
		{name: "GetHover", checkKey: "contents"},
		{name: "GetInputCompletion", checkKey: "completionId"},
		{name: "GetCodeValidationStates", checkKey: "states"},

		// MCP Apps
		{name: "GetMcpPrompt", checkKey: "description"},
		{name: "ListMcpResources", checkKey: "resources"},
		{name: "CallMcpAppTool", checkKey: "callToolResultJson"},
		{name: "GetMcpAppResource", checkKey: "html"},

		// Sidecars
		{name: "ManageSidecar"},
		{name: "ListSidecarLogFiles", checkKey: "logFilenames"},
		{name: "ResolveSidecarConfig", checkKey: "resolvedConfig"},

		// Browser
		{name: "CaptureConsoleLogs", checkKey: "consoleLogs"},
		{name: "CheckDevToolsActivePort", checkKey: "exists"},
		{name: "ListPages", checkKey: "pages"},
		{name: "SetBrowserOpenConversation"},

		// Projects & Customizations
		{name: "CreateMagicProject", checkKey: "projectId"},
		{name: "GenerateEnvironmentName", checkKey: "name"},
		{name: "CreateScratchProjectFolder", checkKey: "isGitRepo"},
		{name: "GetAllCustomAgentConfigs", checkKey: "configMap"},
		{name: "ScanSkillsConfigFile", checkKey: "skills"},
		{name: "ListContextProviders", checkKey: "providers"},

		// Telemetry & Misc
		{name: "FetchAdminControls", checkKey: "adminControlsApplicable"},
		{name: "GetProfileData", checkKey: "profilePictureUrl"},
		{name: "GetTokenBase", checkKey: "tokenBase"},
		{name: "DetectBattleModeAutoTrigger", checkKey: "targetBattleN"},
		{name: "ApplyUpdate", checkKey: "notifiedHost"},
		{name: "GetVoiceCallDebug", checkKey: "sessions"},

		// Insiders / CL 1012028536 Additions
		{name: "DeleteAllUserData"},
		{name: "GetUserDataDeletionStatus", checkKey: "state"},
		{name: "GetGitHubPullRequests", checkKey: "statuses"},
		{name: "GetWorkspaceGitHubPullRequests", checkKey: "workspaces"},
		{name: "GetGoogleChatLinkInfo", checkKey: "infos"},
		{name: "RunTrigger"},

		// Antigravity 2.31.0 / CL 1017595969 Additions
		{name: "CancelLogin"},
		{name: "GetLoginSession", checkKey: "active"},
		{name: "SwitchLoginToAuthCode", checkKey: "active"},
		{name: "SubmitLoginAuthCode", checkKey: "accepted"},
		{name: "GetOnboardingPersonas", checkKey: "personas"},
		{name: "SearchGcpProjects", checkKey: "projects"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ans, rpcErr, ok := d.routeLanguageServerMethods(ctx, tc.name, nil)
			if !ok {
				t.Fatalf("method %s was not handled", tc.name)
			}
			if rpcErr != nil {
				t.Fatalf("method %s returned error: %v", tc.name, rpcErr)
			}
			if tc.checkKey != "" {
				m, ok := ans.(map[string]any)
				if !ok {
					t.Fatalf("method %s answer is %T, want map", tc.name, ans)
				}
				if _, exists := m[tc.checkKey]; !exists {
					t.Errorf("method %s answer missing key %q: %v", tc.name, tc.checkKey, m)
				}
			}
		})
	}
}

// TestTCPServer_EndToEndWiringForAdditionalMethods sends real HTTP POST requests
// over TCP to verify that the entire wiring from HTTP -> Connect -> Dispatcher ->
// routeLanguageServerAdditionalMethods -> lsproto -> HTTP response works end-to-end.
func TestTCPServer_EndToEndWiringForAdditionalMethods(t *testing.T) {
	d, err := NewDispatcher(Config{
		Session: NewSessionState("test-e2e"),
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	srv, err := remotetransport.NewTCPServer(context.Background(), d, remotetransport.TCPConfig{
		Addr: "127.0.0.1:0",
		WebUIHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	})
	if err != nil {
		t.Fatalf("NewTCPServer: %v", err)
	}
	defer srv.Shutdown(context.Background())

	codec, err := lsproto.Shared()
	if err != nil {
		t.Fatalf("lsproto.Shared: %v", err)
	}

	testMethods := []string{
		"GitStage",
		"GitCommit",
		"SearchCode",
		"GetHover",
		"CallMcpAppTool",
		"CaptureScreenshot",
		"CreateMagicProject",
		"FetchAdminControls",
		"GetRevertPreview",
		"OpenUrl",
		"DeleteAllUserData",
		"GetUserDataDeletionStatus",
		"GetGitHubPullRequests",
		"GetGoogleChatLinkInfo",
		"RunTrigger",
		"CancelLogin",
		"GetLoginSession",
		"SwitchLoginToAuthCode",
		"SubmitLoginAuthCode",
		"GetOnboardingPersonas",
		"SearchGcpProjects",
	}

	for _, m := range testMethods {
		t.Run(m+"/JSON", func(t *testing.T) {
			url := "http://" + srv.Addr().String() + "/exa.language_server_pb.LanguageServerService/" + m
			req, err := http.NewRequest(http.MethodPost, url, strings.NewReader("{}"))
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			req.Header.Set("X-Jetski-Via-Remote-Control", "true")
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("POST: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d, body = %s", resp.StatusCode, string(body))
			}
		})

		t.Run(m+"/Proto", func(t *testing.T) {
			url := "http://" + srv.Addr().String() + "/exa.language_server_pb.LanguageServerService/" + m
			req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(""))
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			req.Header.Set("X-Jetski-Via-Remote-Control", "true")
			req.Header.Set("Content-Type", "application/proto")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("POST: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d, body = %s", resp.StatusCode, string(body))
			}
			body, _ := io.ReadAll(resp.Body)
			if len(body) > 0 {
				if _, err := codec.DecodeResponse(m, body); err != nil {
					t.Fatalf("failed to decode binary proto response for %s: %v", m, err)
				}
			}
		})
	}
}
