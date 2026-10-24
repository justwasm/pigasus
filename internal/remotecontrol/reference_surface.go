// Package remotecontrol — reference_surface.go: the reference's own
// remote-agent RPC surface, recovered from the FileDescriptorProto
// embedded in the agy 1.4.7 binary.
//
// The clone's JSON-RPC method names were guessed in cycle 16 from
// decompiled Go symbols (`StartRemoteAgentCascade`, `ListRemoteAgentModels`,
// …). agy 1.4.7 answers no local JSON-RPC at all — remote control is a
// Google webchannel (see docs/cycles/rc-wire-live-verify/) — but its gRPC
// service descriptor *is* in the binary, uncompressed, at
// google/gca/aicode/v1main/remote_agent_proxy.proto. That descriptor is the
// only authoritative statement of what the reference can be asked to do and
// what the messages look like:
//
//	google.gca.aicode.v1main.JetskiRemoteAgentProxyService
//	  79 RPCs, 174 messages, 5 enums
//
// 1.4.5 against 1.4.4: one RPC and three messages were added —
// FetchSandboxPort (plus FetchSandboxPortRequest/Response and
// SandboxHttpHeader) — and ListSlashCommandsRequest gained a conversation_id
// field (【反编译】, both FileDescriptorProtos re-extracted from the
// binaries).
//
// 1.4.6 against 1.4.5: five RPCs were appended — SearchFiles,
// StartLocalPluginSignIn, CompleteLocalPluginSignIn, CreateAgent and
// GetAgent — together with a new AgentStatus enum and twelve messages
// (【反编译】, docs/cycles/reference-drift-146/). The internal
// (`exa.remote_agent_proxy_pb`) descriptor did not change at all.
//
// 1.4.7 against 1.4.6: two RPCs were appended — ListAgents and
// GetChangelistStatus — and four messages came with them
// (【反编译】, docs/cycles/reference-drift-147/). The internal descriptor
// grew two fields on one message instead: `is_cel_only` and `cel_type`
// (a CEL — Common Expression Language — evaluation hook).
//
// The evidence files live in out/local-macos/protos/. This file pins the RPC
// list (so drift is noticed) and maps each reference RPC onto the clone
// handler that implements the same operation, if one exists. The mapping is
// additive: legacy method names keep working, and the reference names are
// accepted as aliases.
package remotecontrol

// ReferenceRPCMethods is the method list of
// google.gca.aicode.v1main.JetskiRemoteAgentProxyService in declaration
// order, as recovered from agy 1.4.7. Do not reorder: the fixture in
// testdata/jetski_remote_agent_proxy_1.4.7.txt is compared line by line.
var ReferenceRPCMethods = []string{
	"StartCascade",
	"SendUserCascadeMessage",
	"StreamAgentStateUpdates",
	"GetConversation",
	"ListConversations",
	"ListWorkspaces",
	"CreateWorkspace",
	"ListAvailableModels",
	"ListSlashCommands",
	"ReadFile",
	"GetTurnDiff",
	"GetCodeActionDiff",
	"ListEnvironments",
	"ReadDir",
	"ListTriggerOptions",
	"CreateTrigger",
	"UpdateTrigger",
	"DeleteTrigger",
	"ListTriggers",
	"ListIdentities",
	"GetVersionControlState",
	"CancelCascade",
	"ListPlugins",
	"EnsureEnvironment",
	"CreateProject",
	"UpsertConversationConfig",
	"ListResourceProviders",
	"ListAvailableResources",
	"UpdateProjectResources",
	"ListProjectResources",
	"DisconnectPlugin",
	"ListProjects",
	"UpdateProject",
	"DeleteAgentMessage",
	"CreateTerminal",
	"ListTerminals",
	"SendTerminalInput",
	"StreamTerminalOutput",
	"CloseTerminal",
	"UploadProjectFile",
	"ReadProjectResource",
	"HandleCascadeUserInteraction",
	"DeleteProject",
	"GetPermissionSettings",
	"SetPermissionPreset",
	"SetArtifactReviewMode",
	"SetBrowserJsExecutionPolicy",
	"AddPermissionGrants",
	"RemovePermissionGrants",
	"RenewTriggerCredentials",
	"InstallPlugin",
	"UninstallPlugin",
	"ListPluginInstallations",
	"GetMendelFlags",
	"DeleteConversation",
	"GetUserQuotaSummary",
	"GetUserAccountSettings",
	"SetUserAccountSettings",
	"GetUserProfile",
	"RevertCascadeToStep",
	"GetCascadeNuxes",
	"GenerateConversationTitle",
	"AskSideQuestion",
	"DeleteAllUserData",
	"GetUserDataDeletionStatus",
	"StartBattleMode",
	"EndBattleMode",
	"EliminateBattleModeArm",
	"GetClientStorageItems",
	"UpdateClientStorageItems",
	"GeneratePluginAccessToken",
	"FetchSandboxPort",
	"SearchFiles",
	"StartLocalPluginSignIn",
	"CompleteLocalPluginSignIn",
	"CreateAgent",
	"GetAgent",
	"ListAgents",
	"GetChangelistStatus",
}

// referenceRPCAliases maps a reference RPC name onto the clone's JSON-RPC
// method that performs the same operation. Only exact-behaviour pairs are
// listed: ListIdentities has no clone equivalent, ListWorkspaces is not
// ListRemoteAgentEnvironments, and GetConversation's paginated trajectory
// shape is not what ListRemoteAgentConversations returns.
var referenceRPCAliases = map[string]string{
	"StartCascade":                 "StartRemoteAgentCascade",
	"SendUserCascadeMessage":       "SendRemoteAgentCascadeMessage",
	"CancelCascade":                "CancelRemoteAgentCascade",
	"HandleCascadeUserInteraction": "HandleRemoteAgentCascadeUserInteraction",
	"ListConversations":            "ListRemoteAgentConversations",
	"ListAvailableModels":          "ListRemoteAgentModels",
	"ListSlashCommands":            "ListRemoteAgentSlashCommands",
	"ListEnvironments":             "ListRemoteAgentEnvironments",
	"ReadFile":                     "ReadRemoteAgentFile",
	"ReadDir":                      "ReadRemoteAgentDir",
	"ListTriggerOptions":           "ListRemoteAgentTriggerOptions",
	"CreateTrigger":                "CreateRemoteAgentTrigger",
	"UpdateTrigger":                "UpdateRemoteAgentTrigger",
	"DeleteTrigger":                "DeleteRemoteAgentTrigger",
	"ListTriggers":                 "ListRemoteAgentTriggers",
	"ListPlugins":                  "ListRemoteAgentPlugins",
	"DisconnectPlugin":             "DisconnectRemoteAgentPlugin",
	"CreateProject":                "CreateRemoteAgentProject",
	"ListProjects":                 "ListRemoteAgentProjects",
	"UpdateProject":                "UpdateRemoteAgentProject",
	"DeleteProject":                "DeleteRemoteAgentProject",
	"ListResourceProviders":        "ListRemoteAgentResourceProviders",
	"ListAvailableResources":       "ListRemoteAgentAvailableResources",
	"UpdateProjectResources":       "UpdateRemoteAgentProjectResources",
	"DeleteAgentMessage":           "DeleteRemoteAgentMessage",
	"DeleteConversation":           "DeleteRemoteAgentConversation",
	"CreateTerminal":               "CreateRemoteAgentTerminal",
	"ListTerminals":                "ListRemoteAgentTerminals",
	"SendTerminalInput":            "SendRemoteAgentTerminalInput",
	"CloseTerminal":                "CloseRemoteAgentTerminal",
	"SetPermissionPreset":          "SetRemoteAgentPermissionPreset",
	"SetArtifactReviewMode":        "SetRemoteAgentArtifactReviewMode",
	"AddPermissionGrants":          "AddRemoteAgentPermissionGrants",
	"RemovePermissionGrants":       "RemoveRemoteAgentPermissionGrants",
}

// canonicalMethod resolves a reference RPC name to the clone's internal
// method name. Every other name (including the clone's own legacy names) is
// returned unchanged, so alias resolution never shadows a real method.
func canonicalMethod(method string) string {
	if canonical, ok := referenceRPCAliases[method]; ok {
		return canonical
	}
	return method
}

// ReferenceRPCForCloneMethod is the inverse of canonicalMethod: it returns
// the reference RPC a clone method implements, or "" when the clone method
// has no counterpart in agy 1.4.4. Used by the parity report tooling.
func ReferenceRPCForCloneMethod(method string) string {
	for reference, canonical := range referenceRPCAliases {
		if canonical == method {
			return reference
		}
	}
	return ""
}
