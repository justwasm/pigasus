// Package lsserver — flags_values.go: the reference's flag
// registrations.
//
// The usage strings are the reference's own, word for word: the
// reference's `--help` is Go's `flag` package default output, so the
// only way to reproduce it byte for byte is to declare the same flags
// with the same defaults and the same usage text.
//
// Evidence grade: 一手 — captured from the reference binary
// (out/langserver/help.txt).
package lsserver

import "flag"

// registerStringFlags declares the reference's string flags.
//
// The order here is the reference's alphabetical order; Go's `flag`
// package sorts the printed output anyway, so the order only matters
// for reading this file next to the reference's `--help`.
func registerStringFlags(fs *flag.FlagSet, f *Flags) {
	fs.StringVar(&f.AnalyticsServerURL, "analytics_server_url", "", "Analytics server host")
	fs.StringVar(&f.APIServerURL, "api_server_url", "http://0.0.0.0:50001", "API server host")
	fs.StringVar(&f.AppDataDir, "app_data_dir", "antigravity-ide", "Path where application data is stored, relative to GeminiDir")
	fs.StringVar(&f.BeyondQuotaBucket, "beyond_quota_bucket", "", "Quota bucket to use for Beyond API calls (format: team/bucket)")
	fs.StringVar(&f.BeyondServiceAddr, "beyond_service_addr", "", "Address of the Beyond generative service (Stubby client)")
	fs.StringVar(&f.CloudCodeEndpoint, "cloud_code_endpoint", "", "CCPA API URL")
	fs.StringVar(&f.ConfigDir, "config_dir", "config", "Path where user configuration files are stored, relative to GeminiDir.")
	fs.StringVar(&f.CSRFToken, "csrf_token", "", "CSRF token for language server")
	fs.StringVar(&f.ExtensionServerCSRFToken, "extension_server_csrf_token", "", "CSRF token for extension server")
	fs.StringVar(&f.GeminiDir, "gemini_dir", ".gemini", "Path where Gemini files are stored. If absolute path, will set directly. If relative path, will be resolved relative to HomeDir.")
	fs.StringVar(&f.GenerativeServiceAddr, "generative_service_addr", "blade:google.ai.generativelanguage.v1main.generativeservice-prod", "Address of the generative service")
	fs.StringVar(&f.GoogleCloudProject, "google_cloud_project", "", "Google Cloud Project ID to use for keyless auth")
	fs.StringVar(&f.InferenceAPIServerURL, "inference_api_server_url", "", "Inference API server host. If unset, uses default if not in enterprise mode")
	fs.StringVar(&f.JetboxReleaseChannel, "jetbox_release_channel", "stable", "Release channel (stable, insiders, autopush)")
	fs.StringVar(&f.LocalChromeUserDataDir, "local_chrome_user_data_dir", "", "Chrome user data directory.")
	fs.StringVar(&f.MockFixture, "mock_fixture", "", "Path to a JSON mock fixture, or generate:<name> (requires --use_mocked_data)")
	fs.StringVar(&f.ModelAPIClientType, "model_api_client_type", "ccpa", "Which model client to use: ccpa or gemini. Defaults to ccpa.")
	fs.StringVar(&f.OverrideBusinessOAuthClientID, "override_business_oauth_client_id", "", "Override Business OAuth client ID")
	fs.StringVar(&f.OverrideBusinessOAuthClientSecret, "override_business_oauth_client_secret", "", "Override Business OAuth client secret")
	fs.StringVar(&f.OverrideIDEName, "override_ide_name", "", "Override IDE name in metadata (e.g. 'antigravity')")
	fs.StringVar(&f.OverrideIDEVersion, "override_ide_version", "", "Override IDE version in metadata (e.g. '0.1.0')")
	fs.StringVar(&f.OverrideModelName, "override_model_name", "", "Model name to override default model")
	fs.StringVar(&f.OverrideOAuthClientID, "override_oauth_client_id", "", "Override OAuth client ID")
	fs.StringVar(&f.OverrideOAuthClientSecret, "override_oauth_client_secret", "", "Override OAuth client secret")
	fs.StringVar(&f.OverrideUserAgentName, "override_user_agent_name", "", "Override user agent name for HTTP requests (e.g. 'antigravity-dev')")
	fs.StringVar(&f.ParentPipePath, "parent_pipe_path", "", "Parent pipe path for monitoring whether the parent process is still running")
	fs.StringVar(&f.StartupWarning, "startup_warning", "", "Warning message to display at startup in the UI.")
	fs.StringVar(&f.StubbyAuthCloudLocation, "stubby_auth_cloud_location", "", "Google Cloud Region/Location to use for StubbyAuthProvider")
	fs.StringVar(&f.StubbyAuthCloudProject, "stubby_auth_cloud_project", "", "Cloud project ID to use for StubbyAuthProvider")
	fs.StringVar(&f.StubbyAuthMode, "stubby_auth_mode", "", "Auth mode to use when using StubbyAuthProvider (personal_consumer, business_licensed_enterprise, business_agent_platform_paygo)")
	fs.StringVar(&f.SubclientType, "subclient_type", "", "Subclient type for HTTP requests (e.g. 'sdk', 'cli', 'hub')")
	fs.StringVar(&f.TrafficStreamSelfID, "traffic_stream_selfid", "", "Census traffic_stream_selfid tag for model API traffic isolation")
	fs.StringVar(&f.WebBundlePath, "web_bundle_path", "", "Directory or .zip holding the compiled web UI to serve, in preference to the bundle compiled into this binary.")
	fs.StringVar(&f.WorkspaceID, "workspace_id", "", "Workspace ID")
	fs.StringVar(&f.ZerogHTTPSocketPath, "zerog_http_socket_path", "", "Path to the Unix Domain Socket for Zerog HTTP service.")
	fs.StringVar(&f.ZerogTLSCertPath, "zerog_tls_cert_path", "", "Path to write the TLS certificate for Zerog gRPC service.")
	fs.StringVar(&f.ZerogVMSshKeyPath, "zerog_vm_ssh_key_path", "", "Path to the SSH key for Zerog VM.")
	fs.StringVar(&f.ZerogVMSshSocketName, "zerog_vm_ssh_socket_name", "", "Name of the SSH socket for Zerog VM.")
}

// registerBoolFlags declares the reference's boolean flags.
func registerBoolFlags(fs *flag.FlagSet, f *Flags) {
	fs.BoolVar(&f.BrowserEvalEnv, "browser_eval_env", false, "Whether to enable browser eval environment setup (pre-installed playwright, CA certs, proxy/SSO)")
	fs.BoolVar(&f.DisableTelemetry, "disable_telemetry", false, "If true, disable telemetry logging")
	fs.BoolVar(&f.EnableLSP, "enable_lsp", false, "If true, enable LSP")
	fs.BoolVar(&f.ExitOnStdinClose, "exit_on_stdin_close", false, "If true, shut down gracefully when stdin reaches EOF. Used when the parent process cannot signal us across an OS boundary (e.g. WSL).")
	fs.BoolVar(&f.Headless, "headless", false, "Whether to run in headless mode")
	fs.BoolVar(&f.IsGoogle3Workspace, "is_google3_workspace", false, "Deprecated: no-op.")
	fs.BoolVar(&f.IsGoogleInternal, "is_google_internal", false, "Whether running in a Google internal environment.")
	fs.BoolVar(&f.LocalChromeHeadless, "local_chrome_headless", true, "Whether Chrome runs in headless mode.")
	fs.BoolVar(&f.MQueryForContextModule, "mquery_for_context_module", true, "Whether to enable mquery in the core context module.")
	fs.BoolVar(&f.PersistentMode, "persistent_mode", false, "If true, run in persistent daemon mode: writes discovery file and doesn't exit when extension closes")
	fs.BoolVar(&f.RunningInXbox, "running_in_xbox", false, "Whether running inside Xbox sandbox")
	fs.BoolVar(&f.SandboxMode, "sandbox_mode", false, "Whether to use the eval model API client (modelapieval)")
	fs.BoolVar(&f.Stamp, "stamp", false, "If true, print stamp information and exit")
	fs.BoolVar(&f.Standalone, "standalone", false, "Whether to run in standalone mode")
	fs.BoolVar(&f.UseCustomPageActions, "use_custom_page_actions", true, "Whether to enable the actuation overlay functionality")
	fs.BoolVar(&f.UseLocalChrome, "use_local_chrome", false, "Whether to use local chrome")
	fs.BoolVar(&f.UseLSChromeDevtoolsMCP, "use_ls_chrome_devtools_mcp", true, "Whether to start the Chrome DevTools MCP server")
	fs.BoolVar(&f.UseMockedData, "use_mocked_data", false, "Use mocked data for testing")
	fs.BoolVar(&f.UseStubbyAuth, "use_stubby_auth", false, "Use LOAS auth instead of OAuth browser flow. Only for use in Standalone + internal mode.")
	fs.BoolVar(&f.UseTestGaia, "use_test_gaia", false, "Whether to use Test Gaia for authentication. If true, overrides OAuth endpoints to use gaiastaging.")
}

// registerIntFlags declares the reference's integer flags.
func registerIntFlags(fs *flag.FlagSet, f *Flags) {
	fs.IntVar(&f.CDPPort, "cdp_port", 9222, "Port for Chrome DevTools Protocol")
	fs.IntVar(&f.ExtensionServerPort, "extension_server_port", 0, "Port to connect to the extension server. If unset, the extension server is not used.")
	fs.IntVar(&f.FileWatchMaxDirCount, "file_watch_max_dir_count", 0, "The max number of directories we will watch.")
	fs.IntVar(&f.HTTPServerPort, "http_server_port", 0, "Port for HTTP language server. 0 means random.")
	fs.IntVar(&f.HTTPSServerPort, "https_server_port", 0, "Port for HTTPS language server. 0 means random.")
	fs.IntVar(&f.LimitGoMaxProcs, "limit_go_max_procs", 4, "Cap GOMAXPROCS at this value")
	fs.IntVar(&f.LSPPort, "lsp_port", 0, "Port for LSP protocol. 0 means random.")
	fs.IntVar(&f.MaxNumTrackedWorkspaces, "max_num_tracked_workspaces", 10, "Maximum number of tracked workspaces")
	fs.IntVar(&f.XboxToolUsePort, "xbox_tool_use_port", 0, "Port for Xbox Tool Use gRPC server")
	fs.IntVar(&f.ZerogAppHostPort, "zerog_app_host_port", 0, "Port for the Zerog App host HTTP server.")
}
