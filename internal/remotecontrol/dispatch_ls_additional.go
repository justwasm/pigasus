// Package remotecontrol — dispatch_ls_additional.go: additional
// LanguageServerService handlers covering VCS, Code Search, MCP Apps,
// Sidecars, Browser Companion, Projects and Telemetry.
package remotecontrol

import (
	"context"
	"encoding/json"

	"github.com/justwasm/pigasus/internal/remotecontrol/rpc"
)

// routeLanguageServerAdditionalMethods handles additional LanguageServerService
// methods from the Antigravity Web UI.
func (d *Dispatcher) routeLanguageServerAdditionalMethods(ctx context.Context, method string, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	if ans, errBody, ok := d.routeLSVCS(method, params); ok {
		return ans, errBody, true
	}
	if ans, errBody, ok := d.routeLSCodeSearch(method, params); ok {
		return ans, errBody, true
	}
	if ans, errBody, ok := d.routeLSMcpApps(method, params); ok {
		return ans, errBody, true
	}
	if ans, errBody, ok := d.routeLSSidecars(method, params); ok {
		return ans, errBody, true
	}
	if ans, errBody, ok := d.routeLSBrowser(method, params); ok {
		return ans, errBody, true
	}
	if ans, errBody, ok := d.routeLSProjects(method, params); ok {
		return ans, errBody, true
	}
	if ans, errBody, ok := d.routeLSTelemetryAndMisc(method, params); ok {
		return ans, errBody, true
	}
	return nil, nil, false
}

// routeLSVCS handles Git, Jujutsu and Repo source-control panel RPCs.
func (d *Dispatcher) routeLSVCS(method string, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	switch method {
	case "GitStage", "gitStage",
		"GitUnstage", "gitUnstage",
		"GitDiscard", "gitDiscard",
		"CheckoutCommit", "checkoutCommit",
		"DiscardFileChanges", "discardFileChanges",
		"JjAbandon", "jjAbandon",
		"JjSync", "jjSync",
		"RepoSync", "repoSync":
		return map[string]any{}, nil, true
	case "GitCommit", "gitCommit",
		"JjCommit", "jjCommit",
		"JjSquash", "jjSquash",
		"RepoCommit", "repoCommit",
		"RepoAmend", "repoAmend":
		return map[string]any{"commitId": "pigasus-local-commit"}, nil, true
	case "GitPush", "gitPush":
		return map[string]any{
			"reviewUrl":  "",
			"remoteKind": "",
			"transport":  "",
			"pushMode":   "",
		}, nil, true
	case "JjUpload", "jjUpload":
		return map[string]any{"clNumbers": []string{}}, nil, true
	case "RepoUpload", "repoUpload":
		return map[string]any{"changeId": ""}, nil, true
	case "GetCommitDetails", "getCommitDetails":
		return map[string]any{
			"changedFiles":    []any{},
			"parentCommitIds": []string{},
		}, nil, true
	case "GenerateCommitMessage", "generateCommitMessage":
		return map[string]any{"commitMessage": map[string]any{}}, nil, true
	case "GetVersionControlFileContent", "getVersionControlFileContent":
		return map[string]any{"content": ""}, nil, true
	case "GetWorkspaceChangelists", "getWorkspaceChangelists":
		return map[string]any{"workspaces": []any{}}, nil, true
	case "GetGitHubPullRequests", "getGitHubPullRequests":
		return map[string]any{"statuses": []any{}}, nil, true
	case "GetWorkspaceGitHubPullRequests", "getWorkspaceGitHubPullRequests":
		return map[string]any{"workspaces": []any{}}, nil, true
	}
	return nil, nil, false
}

// routeLSCodeSearch handles search, hover and input completion RPCs.
func (d *Dispatcher) routeLSCodeSearch(method string, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	switch method {
	case "SearchCode", "searchCode":
		return map[string]any{"results": []any{}}, nil, true
	case "SearchContext", "searchContext":
		return map[string]any{"ok": true, "items": []any{}}, nil, true
	case "ResolveContextUri", "resolveContextUri":
		return map[string]any{"ok": true}, nil, true
	case "GetHover", "getHover":
		return map[string]any{"contents": ""}, nil, true
	case "GetInputCompletion", "getInputCompletion":
		return map[string]any{"completionId": ""}, nil, true
	case "SendInputCompletionFeedback", "sendInputCompletionFeedback",
		"ProvideCompletionFeedback", "provideCompletionFeedback":
		return map[string]any{}, nil, true
	case "GetCodeValidationStates", "getCodeValidationStates":
		return map[string]any{"states": []any{}}, nil, true
	case "SearchFiles", "searchFiles":
		return map[string]any{"results": []any{}}, nil, true
	}
	return nil, nil, false
}

// routeLSMcpApps handles MCP app tools, resources and prompts.
func (d *Dispatcher) routeLSMcpApps(method string, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	switch method {
	case "GetMcpPrompt", "getMcpPrompt":
		return map[string]any{"description": "", "messages": []any{}}, nil, true
	case "ListMcpResources", "listMcpResources":
		return map[string]any{"resources": []any{}}, nil, true
	case "CallMcpAppTool", "callMcpAppTool",
		"LaunchMcpApp", "launchMcpApp":
		return map[string]any{"callToolResultJson": "{}"}, nil, true
	case "GetMcpAppResource", "getMcpAppResource":
		return map[string]any{
			"html":     "",
			"mimeType": "text/html",
			"metaJson": "{}",
		}, nil, true
	}
	return nil, nil, false
}

// routeLSSidecars handles sidecar management and log inspection.
func (d *Dispatcher) routeLSSidecars(method string, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	switch method {
	case "ManageSidecar", "manageSidecar":
		return map[string]any{}, nil, true
	case "ListSidecarLogFiles", "listSidecarLogFiles":
		return map[string]any{"logFilenames": []string{}}, nil, true
	case "GetSidecarLogs", "getSidecarLogs":
		return map[string]any{"logs": []any{}}, nil, true
	case "ResolveSidecarConfig", "resolveSidecarConfig":
		return map[string]any{"resolvedConfig": map[string]any{}}, nil, true
	}
	return nil, nil, false
}

// routeLSBrowser handles browser companion and media capture.
func (d *Dispatcher) routeLSBrowser(method string, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	switch method {
	case "CaptureScreenshot", "captureScreenshot",
		"HandleScreenRecording", "handleScreenRecording",
		"FocusUserPage", "focusUserPage",
		"SetBrowserOpenConversation", "setBrowserOpenConversation",
		"SkipBrowserSubagent", "skipBrowserSubagent":
		return map[string]any{}, nil, true
	case "CaptureConsoleLogs", "captureConsoleLogs":
		return map[string]any{"consoleLogs": []any{}}, nil, true
	case "CheckDevToolsActivePort", "checkDevToolsActivePort":
		return map[string]any{"exists": false}, nil, true
	case "ListPages", "listPages":
		return map[string]any{"pages": []any{}}, nil, true
	}
	return nil, nil, false
}

// routeLSProjects handles project environments, sharing and customization files.
func (d *Dispatcher) routeLSProjects(method string, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	switch method {
	case "AddEnvironmentToProject", "addEnvironmentToProject",
		"WarmUpEnvironment", "warmUpEnvironment",
		"UpdateCustomization", "updateCustomization",
		"UpdateCustomizationPathsFile", "updateCustomizationPathsFile":
		return map[string]any{}, nil, true
	case "GenerateEnvironmentName", "generateEnvironmentName":
		return map[string]any{"name": "env-default"}, nil, true
	case "CreateMagicProject", "createMagicProject":
		return map[string]any{"projectId": "local-magic-project"}, nil, true
	case "CreateScratchProjectFolder", "createScratchProjectFolder":
		return map[string]any{"folderUri": "", "isGitRepo": false}, nil, true
	case "ImportProjectFromUrl", "importProjectFromUrl":
		return map[string]any{"projectName": "", "projectRootUri": ""}, nil, true
	case "CopyBuiltinWorkflowToWorkspace", "copyBuiltinWorkflowToWorkspace":
		return map[string]any{"workflow": map[string]any{}}, nil, true
	case "ListCustomizationPathsByFile", "listCustomizationPathsByFile":
		return map[string]any{"pathsFiles": []any{}}, nil, true
	case "ListContextProviders", "listContextProviders":
		return map[string]any{"providers": []any{}}, nil, true
	case "CreateTrajectoryShare", "createTrajectoryShare":
		return map[string]any{"url": ""}, nil, true
	case "LoadSharedTrajectory", "loadSharedTrajectory":
		return map[string]any{"cascadeId": ""}, nil, true
	case "GetCascadeTrajectoryGeneratorMetadata", "getCascadeTrajectoryGeneratorMetadata":
		return map[string]any{"generatorMetadata": map[string]any{}}, nil, true
	case "GetAllCustomAgentConfigs", "getAllCustomAgentConfigs":
		return map[string]any{"configMap": map[string]any{}}, nil, true
	case "ScanSkillsConfigFile", "scanSkillsConfigFile":
		return map[string]any{"skills": []any{}}, nil, true
	case "GetAllRules", "getAllRules":
		return map[string]any{"memories": []any{}}, nil, true
	case "GetTurnDiff", "getTurnDiff":
		return map[string]any{"fileDiffs": []any{}}, nil, true
	}
	return nil, nil, false
}

// routeLSTelemetryAndMisc handles admin controls, voice debugging and telemetry.
func (d *Dispatcher) routeLSTelemetryAndMisc(method string, params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	switch method {
	case "FetchAdminControls", "fetchAdminControls":
		return map[string]any{"adminControlsApplicable": false}, nil, true
	case "GetProfileData", "getProfileData":
		return map[string]any{"profilePictureUrl": ""}, nil, true
	case "GetTokenBase", "getTokenBase":
		return map[string]any{"tokenBase": map[string]any{}}, nil, true
	case "GetUnleashData", "getUnleashData":
		return map[string]any{
			"context":          map[string]any{},
			"experimentConfig": map[string]any{},
		}, nil, true
	case "GetVoiceCallDebug", "getVoiceCallDebug":
		return map[string]any{"sessions": []any{}}, nil, true
	case "GetGoogleChatLinkInfo", "getGoogleChatLinkInfo":
		return map[string]any{"infos": []any{}}, nil, true
	case "RecordObservabilityData", "recordObservabilityData",
		"RegisterInteraction", "registerInteraction",
		"SendAgentMessage", "sendAgentMessage",
		"EndNarration", "endNarration",
		"SendNarrationTurn", "sendNarrationTurn":
		return map[string]any{}, nil, true
	case "DetectBattleModeAutoTrigger", "detectBattleModeAutoTrigger":
		return map[string]any{
			"targetBattleN": 0,
			"triggerTag":    "",
			"bannerText":    "",
		}, nil, true
	case "ApplyUpdate", "applyUpdate":
		return map[string]any{"notifiedHost": false}, nil, true
	case "GenerateSkillInstallationCL", "generateSkillInstallationCL":
		return map[string]any{"clNumber": 0, "clUrl": ""}, nil, true
	case "ListBackgroundTasks", "listBackgroundTasks":
		return map[string]any{"tasks": []any{}}, nil, true
	case "StartBattleMode", "startBattleMode",
		"EndBattleMode", "endBattleMode",
		"EliminateBattleModeArm", "eliminateBattleModeArm":
		return map[string]any{}, nil, true
	case "StartNarration", "startNarration":
		return map[string]any{}, nil, true
	}
	return nil, nil, false
}