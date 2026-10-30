// Package remotecontrol — dispatch_languageserver.go: LanguageServerService
// Connect-RPC handlers for the Jetski / Antigravity Web UI.
//
// These handlers satisfy the Connect-RPC over WebSocket client in main.js
// (class G3a, class BHc, and associated store services):
//   - GetLocalUserInfo (username, homeDirUri, machineType)
//   - GetServerConfiguration (appDataDir, subclientType, sandboxAvailable, configDir)
//   - GetStatus, Heartbeat, GetRemoteControlInfo, GetCapabilities
//   - GetMendelFlags (developer experiments)
//   - GetDefaultProjectDir, GetStandaloneDir, GetAuthStatus, GetGrantedScopes
//   - ResolveFolder, ListProjects, ReadProjects, ReadProject
//   - StatUri, ReadDir, ReadFile, WriteFile, CreateDirectory
package remotecontrol

import (
	"context"
	"encoding/json"
	"github.com/justwasm/pigasus/endpoints"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// routeLanguageServerMethods handles LanguageServerService methods
// from the Web UI. Returns ok=true if method was handled.
func (d *Dispatcher) routeLanguageServerMethods(ctx context.Context, method string, params json.RawMessage) (any, *rpcErrorBody, bool) {
	// The Web UI's conversation loop has its own table (dispatch_ls.go).
	if r, e, ok := d.routeLanguageServerCascadeMethods(ctx, method, params); ok {
		return r, e, true
	}
	// The Web UI's agent↔browser bridge has its own table
	// (dispatch_agentbrowser.go).
	if r, e, ok := d.routeAgentBrowserMethods(method, params); ok {
		return r, e, true
	}
	switch method {
	case "HandleCascadeUserInteraction", "handleCascadeUserInteraction":
		// The Web UI's answer to a waiting tool: the question card
		// and the permission card both send it. The IDE dialect is
		// routed here, before the reference-surface alias turns
		// "HandleCascadeUserInteraction" into the clone's legacy
		// HandleRemoteAgentCascadeUserInteraction.
		return d.lsHandleCascadeUserInteraction(params)
	case "GetLocalUserInfo", "getLocalUserInfo":
		return d.handleGetLocalUserInfo()
	case "GetServerConfiguration", "getServerConfiguration":
		return d.handleGetServerConfiguration()
	case "GetStatus", "getStatus":
		return d.handleGetStatus()
	case "Heartbeat", "heartbeat":
		return d.handleHeartbeat()
	case "GetRemoteControlInfo", "getRemoteControlInfo":
		return d.handleGetRemoteControlInfo()
	case "GetCapabilities", "getCapabilities":
		return map[string]any{"supportsHookResultProtoBytes": false}, nil, true
	case "GetMendelFlags", "getMendelFlags":
		// The reference answers with the remote-control experiments
		// and a `webrtcMeshReady` boolean; the dashboard's host page
		// reads the answer with JSON.parse and decides from it
		// whether the WebRTC mesh is on. See mendel_flags.go.
		return mendelFlagsAnswer(ctx, d.cfg.MendelUserAgent), nil, true
	case "SetMendelFlagOverrides", "setMendelFlagOverrides":
		return map[string]any{}, nil, true
	case "GetDefaultProjectDir", "getDefaultProjectDir":
		home, _ := os.UserHomeDir()
		return map[string]any{"defaultProjectDirUri": "file://" + filepath.ToSlash(home)}, nil, true
	case "GetStandaloneDir", "getStandaloneDir":
		home, _ := os.UserHomeDir()
		return map[string]any{"standaloneDir": home}, nil, true
	case "HasAuthToken", "hasAuthToken":
		return map[string]any{
			"hasToken": true,
		}, nil, true
	case "GetAuthStatus", "getAuthStatus":
		// `is_gcp_tos` is false, and proto-JSON omits a zero scalar:
		// the reference's own answer is {"authResult":{"hasValidAuth":
		// true}}. The Web UI reads it as `k.isGcpTos ?? false`.
		return map[string]any{
			"authResult": map[string]any{
				"hasValidAuth":  true,
				"projectId":     "pigasus-local",
				"location":      "us-central1",
				"grantedScopes": []string{"https://www.googleapis.com/auth/userinfo.email"},
			},
		}, nil, true
	case "GetUserStatus", "getUserStatus":
		return d.handleGetUserStatus()
	case "SaveMediaAsArtifact", "saveMediaAsArtifact":
		// The Web UI's upload: it hands the picture over and expects
		// the artifact's uri back.
		return d.handleSaveMediaAsArtifact(params)
	case "DeleteMediaArtifact", "deleteMediaArtifact":
		return d.handleDeleteMediaArtifact(params)
	case "GetGrantedScopes", "getGrantedScopes":
		return map[string]any{"scopes": []string{"https://www.googleapis.com/auth/userinfo.email"}}, nil, true
	case "Login", "login", "LoginWithBrowser", "loginWithBrowser",
		"ValidateProject", "validateProject", "SelectLicense", "selectLicense":
		return map[string]any{
			"authResult": map[string]any{
				"hasValidAuth":  true,
				"projectId":     "pigasus-local",
				"location":      "us-central1",
				"grantedScopes": []string{"https://www.googleapis.com/auth/userinfo.email"},
			},
		}, nil, true
	case "SelfAssignLicense", "selfAssignLicense":
		// The answer is *not* an authResult: the message the clone
		// used to send here was the shape of LoginResponse and
		// SelectLicenseResponse. See handleLSSelfAssignLicense.
		return d.handleLSSelfAssignLicense(params)
	case "AuthLogout", "authLogout":
		return map[string]any{}, nil, true
	case "ListLicenses", "listLicenses":
		return map[string]any{"licenses": []any{}}, nil, true
	case "ListEnvironments", "listEnvironments",
		"ListResourceProviders", "listResourceProviders",
		"ListAvailableResources", "listAvailableResources":
		// The language server's own cloud surfaces. They are answered
		// here, not through the alias table: the alias sent them to
		// the clone's legacy dashboard methods, whose members the
		// language server's schema does not know. See
		// dispatch_ls_cloud.go.
		return d.routeLSCloudMethods(method, params)
	case "JetboxSubscribeToState", "jetboxSubscribeToState":
		// The stream carries the app state and the user config — the
		// Web UI hydrates both from its first message (the frontend's
		// subscribe() reads `appState` and `userConfig` off it and
		// ignores the two read RPCs below).
		return d.handleJetboxReadState()
	case "JetboxReadAppState", "jetboxReadAppState",
		"JetboxReadUserConfig", "jetboxReadUserConfig":
		// The reference's JetboxReadAppStateResponse and
		// JetboxReadUserConfigResponse carry no fields at all, so the
		// server answers them with an empty object.
		return map[string]any{}, nil, true
	case "JetboxWriteState", "jetboxWriteState":
		return d.handleJetboxWriteState(params)
	case "JetboxSubscribeToSummaries", "jetboxSubscribeToSummaries":
		// JetboxSubscribeToSummariesResponse carries `updates` (a map
		// from the conversation id to the summary) and `deletes`. The
		// clone answers it from the reference's own conversation index.
		return d.handleJetboxSubscribeToSummaries()
	case "GetDebugDiagnostics", "getDebugDiagnostics":
		return map[string]any{}, nil, true
	case "GetAllCascadeTrajectories", "getAllCascadeTrajectories":
		// The response is a map field, `trajectory_summaries`.
		return map[string]any{"trajectorySummaries": map[string]any{}}, nil, true
	case "GetSidecarEvents", "getSidecarEvents":
		// GetSidecarEventsResponse.events — `sidecars` is the field of
		// SubscribeToSidecarsResponse, and a key the decoder does not
		// know is dropped.
		return map[string]any{"events": []any{}}, nil, true
	case "SubscribeToSidecars", "subscribeToSidecars":
		return map[string]any{"sidecars": []any{}}, nil, true
	case "ListTriggers", "listTriggers":
		return map[string]any{"triggers": []any{}}, nil, true
	case "ListTriggerOptions", "listTriggerOptions":
		// ListTriggerOptionsResponse.options — not `triggers`.
		return map[string]any{"options": []any{}}, nil, true
	case "GetRepoInfos", "getRepoInfos":
		// GetRepoInfosResponse.repos — not `repoInfos`.
		return map[string]any{"repos": []any{}}, nil, true
	case "GetContribVariants", "getContribVariants":
		return map[string]any{"variants": []any{}}, nil, true
	case "GetUpdateStatus", "getUpdateStatus":
		return d.handleGetUpdateStatus()
	case "WatchDirectory", "watchDirectory":
		return map[string]any{}, nil, true
	case "ProjectUpdatesStream", "projectUpdatesStream":
		return d.handleProjectUpdatesStream()
	case "ResolveFolder", "resolveFolder":
		return map[string]any{}, nil, true
	case "ListProjects", "listProjects":
		return d.handleLanguageServerListProjects(ctx)
	case "ReadProjects", "readProjects":
		return d.handleLanguageServerReadProjects(ctx, params)
	case "ReadProject", "readProject":
		// `project` is an unset message — proto-JSON omits it.
		return map[string]any{"notFoundOnDisk": true}, nil, true
	case "StatUri", "statUri":
		return d.handleLanguageServerStatUri(params)
	case "ReadDir", "readDir":
		if strings.Contains(string(params), `"uri"`) {
			return d.handleLanguageServerReadDir(params)
		}
	case "ReadFile", "readFile":
		if strings.Contains(string(params), `"uri"`) {
			return d.handleLanguageServerReadFile(params)
		}
	case "WriteFile", "writeFile":
		return d.handleLanguageServerWriteFile(params)
	case "CreateDirectory", "createDirectory":
		return d.handleLanguageServerCreateDirectory(params)
	case "CreateTerminal", "createTerminal":
		// The Web UI's terminal surface — the reference's own
		// exa.language_server_pb messages. It is *not* the
		// dashboard's RemoteAgentTerminal* contract (that one
		// keeps its own name and its own snake_case params).
		//
		// 【一手】 2026-10-27, the reference's own answers; see
		// ls_terminals.go.
		r, e := d.lsCreateTerminal(params)
		return r, e, true
	case "ListTerminals", "listTerminals":
		r, e := d.lsListTerminals(params)
		return r, e, true
	case "CloseTerminal", "closeTerminal":
		r, e := d.lsCloseTerminal(params)
		return r, e, true
	case "SendTerminalInput", "sendTerminalInput":
		r, e := d.lsSendTerminalInput(params)
		return r, e, true
	case "SendAudioChunk", "sendAudioChunk":
		r, e := d.lsSendAudioChunk(params)
		return r, e, true
	case "EndAudioSession", "endAudioSession":
		r, e := d.lsEndAudioSession(params)
		return r, e, true
	case "GetPermissionSettings", "getPermissionSettings":
		// New at agy 1.4.8: the language server's own
		// permission read. The remote-agent proxy has a method
		// of the same name whose response carries two
		// `PermissionSettings` messages (global and project);
		// the language server's carries the three flat fields
		// this handler answers. The language-server table is
		// consulted first (dispatch.go), so the flat shape wins
		// — the clone serves the language server, not the cloud's
		// proxy.
		//
		// 【一手】 2026-10-27, the 1.4.8 descriptor; see
		// permissions_ls.go.
		r, e, ok := d.lsHandleGetPermissionSettings(ctx)
		return r, e, ok
	case "UpdatePermissionSettings", "updatePermissionSettings":
		// New at agy 1.4.8, and new to the clone: the language
		// server's own permission write.
		//
		// 【一手】 2026-10-27, the 1.4.8 descriptor; see
		// permissions_ls.go.
		r, e, ok := d.lsHandleUpdatePermissionSettings(ctx, params)
		return r, e, ok
	}
	return nil, nil, false
}

// defaultCompanionOrigin is the origin of the companion web app that
// serves the browser-side remote-control view.
//
// 【一手】 live capture, 2026-10-26: the companion app is served from
// antigravity.google.com — "https://antigravity.google.com/" answers
// with the "Remote Control Instances" dashboard and
// "https://antigravity.google.com/r/<instance id>" answers with the
// remote view ("Instance disconnected — Waiting for connection or
// Wi-Fi to resume…"), while "https://antigravity.google/r/<id>"
// answers 404 ("The requested URL /r/… was not found on this
// server."). The reference binary's own user-facing strings agree:
// "Open https://antigravity.google.com and look for %q." and
// "Instance name: %s (find it at https://antigravity.google.com)".
//
// It is the compiled-in default. The live origin is
// endpoints.Current().CompanionOrigin — the configuration file may point
// the companion at a local service.
const defaultCompanionOrigin = endpoints.DefaultCompanionOrigin

// handleGetStatus answers GetStatus and Heartbeat.
//
// The response shape is exa.language_server_pb.GetStatusResponse: a
// `status` block (exa.codeium_common_pb.Status) carrying a `level`
// (exa.codeium_common_pb.StatusLevel) and a `message`.
//
// The level enum has no OK value — UNSPECIFIED=0, ERROR=1,
// WARNING=2, INFO=3, DEBUG=4 — so a healthy server reports INFO.
// The shape matters: `status` is a *message* in the proto, so a
// plain string there breaks a proto-JSON client (the reference's
// own Connect client parses the response against the descriptor).
func (d *Dispatcher) handleGetStatus() (any, *rpcErrorBody, bool) {
	return map[string]any{
		"status": map[string]any{
			"level":   "STATUS_LEVEL_INFO",
			"message": "ready",
		},
	}, nil, true
}

// handleHeartbeat answers Heartbeat.
//
// The response shape is exa.language_server_pb.HeartbeatResponse: one
// field, `last_extension_heartbeat`, a google.protobuf.Timestamp — and
// a Timestamp is a proto-JSON *string* (RFC 3339 in UTC), not an
// object. The reference's own answer, captured first-hand, is
//
//	{"lastExtensionHeartbeat":"2026-10-25T19:31:11.993337Z"}
//
// so the clone answers the same message rather than the GetStatus one.
func (d *Dispatcher) handleHeartbeat() (any, *rpcErrorBody, bool) {
	return map[string]any{
		"lastExtensionHeartbeat": time.Now().UTC().Format("2006-01-02T15:04:05.000000Z"),
	}, nil, true
}

// handleGetUpdateStatus answers GetUpdateStatus.
//
// The response shape is exa.language_server_pb.GetUpdateStatusResponse:
// a `status` block (exa.host_bridge_pb.UpdateStatus) carrying
// `current_version`, `latest_version` and `update_available`.
//
// The Web UI's update check reads e.status.currentVersion and
// e.status.updateAvailable — a *message*. A plain string in `status`
// makes the frontend's decoder throw ("cannot decode message
// exa.host_bridge_pb.UpdateStatus from JSON: string"), and the check
// never settles.
func (d *Dispatcher) handleGetUpdateStatus() (any, *rpcErrorBody, bool) {
	// The clone's app version is the version the host advertised —
	// the same string the reference reports for its own build.
	version := ""
	if d != nil && d.cfg.Session != nil {
		d.cfg.Session.mu.RLock()
		version = d.cfg.Session.Version
		d.cfg.Session.mu.RUnlock()
	}
	return map[string]any{
		"status": map[string]any{
			"currentVersion": version,
			"latestVersion":  version,
		},
	}, nil, true
}

// handleProjectUpdatesStream answers ProjectUpdatesStream.
//
// The response shape is exa.language_server_pb.ProjectUpdatesStreamResponse:
// a oneof named `update`, whose members are `project_list`
// (exa.language_server_pb.ProjectList) and `project_updated_id`.
//
// proto-JSON spells a oneof by its *member*: the frame is
// {"projectList":{"projectIds":[…]}}, not {"update":{"case":…,"value":…}}.
//
// The {case, value} envelope is what protobuf-es *decodes* a frame
// into — the Web UI reads frame.update.case — so on the wire it is
// dropped: the decoder matches the JSON keys against the message's
// field names, "update" is not one of them, and the oneof stays unset.
// The frontend then sees an unset case and the project list never
// arrives.
func (d *Dispatcher) handleProjectUpdatesStream() (any, *rpcErrorBody, bool) {
	return map[string]any{
		"projectList": map[string]any{"projectIds": d.lsProjectIDs()},
	}, nil, true
}

// handleGetRemoteControlInfo answers GetRemoteControlInfo.
//
// The response shape is exa.language_server_pb.GetRemoteControlInfoResponse:
// `enabled`, `instance_id` and `companion_origin`. The Web UI needs
// all three — it builds the browser URL
// "{companionOrigin}/r/{instanceId}" and opens nothing at all when
// one of them is missing.
func (d *Dispatcher) handleGetRemoteControlInfo() (any, *rpcErrorBody, bool) {
	instanceID := d.cfg.RemoteControlInstanceID
	if instanceID == "" {
		instanceID = "pigasus-local"
	}
	origin := d.cfg.CompanionOrigin
	if origin == "" {
		origin = endpoints.Current().CompanionOrigin
	}
	return map[string]any{
		"enabled":         true,
		"instanceId":      instanceID,
		"companionOrigin": origin,
	}, nil, true
}

func (d *Dispatcher) handleGetLocalUserInfo() (any, *rpcErrorBody, bool) {
	user := os.Getenv("USER")
	if user == "" {
		user = "antigravity"
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "/tmp"
	}
	return map[string]any{
		"username":    user,
		"homeDirUri":  "file://" + filepath.ToSlash(home),
		"machineType": "local",
	}, nil, true
}

func (d *Dispatcher) handleGetServerConfiguration() (any, *rpcErrorBody, bool) {
	appDataDir := "antigravity-cli"
	configDir := "antigravity-cli"
	return map[string]any{
		"config": map[string]any{
			"appDataDir":       appDataDir,
			"subclientType":    "cli",
			"sandboxAvailable": false,
			"configDir":        configDir,
		},
	}, nil, true
}

func (d *Dispatcher) handleLanguageServerListProjects(ctx context.Context) (any, *rpcErrorBody, bool) {
	ids := d.lsProjectIDs()
	return map[string]any{
		"projectIds": ids,
	}, nil, true
}

func (d *Dispatcher) handleLanguageServerReadProjects(ctx context.Context, params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req struct {
		IDs []string `json:"ids"`
	}
	_ = json.Unmarshal(params, &req)
	projects, notFound := d.lsProjectObjects(ctx, req.IDs)
	return map[string]any{
		"projects":    projects,
		"notFoundIds": notFound,
	}, nil, true
}

func parseURIPath(rawURI string) string {
	if strings.HasPrefix(rawURI, "file://") {
		u, err := url.Parse(rawURI)
		if err == nil && u.Path != "" {
			return u.Path
		}
		return strings.TrimPrefix(rawURI, "file://")
	}
	return rawURI
}

func (d *Dispatcher) handleLanguageServerStatUri(params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(params, &req); err != nil || req.URI == "" {
		return nil, &rpcErrorBody{Code: -32602, Message: "uri is required"}, true
	}
	p := parseURIPath(req.URI)
	fi, err := os.Stat(p)
	if err != nil {
		return nil, &rpcErrorBody{Code: -32004, Message: "Not found", Data: err.Error()}, true
	}
	// The reference's own answer is
	// {"fileType":"FILE_TYPE_FILE","modTime":"2026-10-26T13:47:00.907136440Z",
	//  "normalizedUri":"file:///…","size":"9293"}: the enum is spelled
	// by its name, the timestamp is a proto-JSON string, and the int64
	// is a string. See ls_files.go.
	return map[string]any{
		"fileType":      lsFileTypeName(fi),
		"modTime":       lsTimestamp(fi.ModTime()),
		"normalizedUri": lsNormalizedURI(p),
		"size":          lsSize(fi.Size()),
	}, nil, true
}

func (d *Dispatcher) handleLanguageServerReadDir(params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(params, &req); err != nil || req.URI == "" {
		return nil, &rpcErrorBody{Code: -32602, Message: "uri is required"}, true
	}
	p := parseURIPath(req.URI)
	entries, err := os.ReadDir(p)
	if err != nil {
		return nil, &rpcErrorBody{Code: -32004, Message: "Not found", Data: err.Error()}, true
	}
	resEntries := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		childPath := filepath.Join(p, e.Name())
		fileType := fileTypeFile
		if e.IsDir() {
			fileType = fileTypeDirectory
		}
		resEntries = append(resEntries, map[string]any{
			"uri":      lsNormalizedURI(childPath),
			"fileType": fileType,
		})
	}
	if len(resEntries) == 0 {
		// proto-JSON omits an empty repeated field: the reference
		// answers an empty directory with {}.
		return map[string]any{}, nil, true
	}
	return map[string]any{
		"entries": resEntries,
	}, nil, true
}

func (d *Dispatcher) handleLanguageServerReadFile(params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(params, &req); err != nil || req.URI == "" {
		return nil, &rpcErrorBody{Code: -32602, Message: "uri is required"}, true
	}
	p := parseURIPath(req.URI)
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, &rpcErrorBody{Code: -32004, Message: "Not found", Data: err.Error()}, true
	}
	// `content` is a `bytes` field, and proto-JSON spells a bytes
	// field as base64. The raw text is not a legal answer: the Web
	// UI's decoder refuses it with "Cannot decode field
	// exa.language_server_pb.ReadFileResponse.content from JSON:
	// invalid base64 string", and the file never opens.
	answer := map[string]any{
		"content": lsEncodeBytes(data),
	}
	// `language` is exa.codeium_common_pb.Language, spelled by its
	// name. The reference reports one for the files it knows and none
	// at all for the files it does not.
	if lang := lsLanguageName(p); lang != "" {
		answer["language"] = lang
	}
	return answer, nil, true
}

func (d *Dispatcher) handleLanguageServerWriteFile(params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req struct {
		URI     string `json:"uri"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(params, &req); err != nil || req.URI == "" {
		return nil, &rpcErrorBody{Code: -32602, Message: "uri is required"}, true
	}
	p := parseURIPath(req.URI)
	// `content` is a `bytes` field on the request side too: the Web UI
	// sends it base64-encoded, and the reference refuses anything else
	// ("invalid value for bytes field content"). See ls_files.go.
	content, err := lsDecodeBytes(req.Content)
	if err != nil {
		return nil, &rpcErrorBody{
			Code:    -32602,
			Message: "content is not base64",
			Data:    err.Error(),
		}, true
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, &rpcErrorBody{Code: -32603, Message: "MkdirAll failed", Data: err.Error()}, true
	}
	if err := os.WriteFile(p, content, 0o644); err != nil {
		return nil, &rpcErrorBody{Code: -32603, Message: "WriteFile failed", Data: err.Error()}, true
	}
	return map[string]any{}, nil, true
}

func (d *Dispatcher) handleLanguageServerCreateDirectory(params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(params, &req); err != nil || req.URI == "" {
		return nil, &rpcErrorBody{Code: -32602, Message: "uri is required"}, true
	}
	p := parseURIPath(req.URI)
	if err := os.MkdirAll(p, 0o755); err != nil {
		return nil, &rpcErrorBody{Code: -32603, Message: "MkdirAll failed", Data: err.Error()}, true
	}
	return map[string]any{}, nil, true
}
