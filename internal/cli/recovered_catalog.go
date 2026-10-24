package cli

// RecoveredTools lists the tool names recovered from the reference binary.
var RecoveredTools = []string{
	"run_command", "create_file", "edit_file", "read_file", "delete_file",
	"list_dir", "search_files", "grep_search", "codesearch", "web_search",
	"web_fetch", "git_status", "git_diff", "git_commit", "git_push",
	"git_pull", "git_checkout", "git_branch", "git_merge", "git_log",
	"todo_write", "todo_read", "spawn_agent", "kill_agent", "list_agents",
	"mcp_call", "mcp_list_tools", "ask_user", "show_diff", "apply_patch",
	"run_tests", "lint_code",
}

// RecoveredServices lists the gRPC service names recovered from the reference binary.
var RecoveredServices = []string{
	"CortexService/GenerateChat", "CortexService/StreamGenerateChat",
	"CortexService/FetchConfig", "CortexService/FetchLicenses",
	"CortexService/RefreshToken", "LanguageServerService/SelectLicense",
	"LanguageServerService/SelfAssignLicense", "ToolsService/Invoke",
	"ToolsService/ListTools", "WorkspaceService/Init",
	"WorkspaceService/Status", "ProjectService/Get",
	"ProjectService/List", "OAuthService/StartDeviceFlow",
	"OAuthService/PollDeviceCode", "OAuthService/RefreshToken",
	"RemoteControlService/Start", "RemoteControlService/Status",
	"RemoteControlService/Stop", "PluginService/Install",
	"PluginService/Uninstall", "MCPService/AddServer",
}
