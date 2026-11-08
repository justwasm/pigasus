package remoteagentclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// proxyStub records the requests a test makes and answers with canned JSON.
type proxyStub struct {
	startReq       map[string]any
	sendReq        map[string]any
	cancelReq      map[string]any
	authHeader     string
	userAgent      string
	streamBody     string
	projects       []Project
	commands       []Command
	dirPath        string
	filePath       string
	entries        []DirEntry
	fileBody       string
	fileExists     bool
	setPreset      string
	interaction    map[string]any
	plugins        []PluginInfo
	installations  []PluginInstallation
	pluginReq      map[string]any
	turnDiff       TurnDiff
	codeDiff       any
	diffReq        map[string]any
	terminal       TerminalInfo
	terminals      []TerminalInfo
	terminalReq    map[string]any
	terminalOut    string
	customizations map[string]any
	uploadReq      map[string]any
	triggerReq     map[string]any
	triggers       []any
}

func (p *proxyStub) handler(w http.ResponseWriter, r *http.Request) {
	p.authHeader = r.Header.Get("Authorization")
	p.userAgent = r.Header.Get("User-Agent")
	switch {
	case strings.HasSuffix(r.URL.Path, ":startCascade"):
		_ = json.NewDecoder(r.Body).Decode(&p.startReq)
		_ = json.NewEncoder(w).Encode(map[string]any{"cascadeId": "cascade-1"})
	case strings.HasSuffix(r.URL.Path, ":sendUserCascadeMessage"):
		_ = json.NewDecoder(r.Body).Decode(&p.sendReq)
		_ = json.NewEncoder(w).Encode(map[string]any{"cascadeId": "cascade-1"})
	case strings.HasSuffix(r.URL.Path, ":cancelCascade"):
		_ = json.NewDecoder(r.Body).Decode(&p.cancelReq)
		_ = json.NewEncoder(w).Encode(map[string]any{})
	case strings.HasSuffix(r.URL.Path, ":listProjects"):
		_ = json.NewEncoder(w).Encode(map[string]any{"projects": p.projects})
	case strings.HasSuffix(r.URL.Path, ":ensureEnvironment"):
		_ = json.NewEncoder(w).Encode(map[string]any{"environment": map[string]any{"connectionState": "CONNECTION_STATE_CONNECTED"}})
	case strings.HasSuffix(r.URL.Path, ":getPermissionSettings"):
		_ = json.NewEncoder(w).Encode(map[string]any{"permissionPreset": "AUTO_APPROVE", "artifactReviewMode": "ALWAYS_APPROVE"})
	case strings.HasSuffix(r.URL.Path, ":setPermissionPreset"):
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		p.setPreset = body["preset"]
		_ = json.NewEncoder(w).Encode(map[string]any{})
	case strings.HasSuffix(r.URL.Path, ":getConversation"):
		_ = json.NewEncoder(w).Encode(map[string]any{"conversationId": "cascade-1", "modelDisplayName": "Gemini"})
	case strings.HasSuffix(r.URL.Path, ":generateConversationTitle"):
		_ = json.NewEncoder(w).Encode(map[string]any{"title": "Hi there"})
	case strings.HasSuffix(r.URL.Path, ":handleCascadeUserInteraction"):
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		p.interaction = body
		_ = json.NewEncoder(w).Encode(map[string]any{})
	case strings.HasSuffix(r.URL.Path, ":createTerminal"):
		_ = json.NewDecoder(r.Body).Decode(&p.terminalReq)
		_ = json.NewEncoder(w).Encode(map[string]any{"terminal": p.terminal})
	case strings.HasSuffix(r.URL.Path, ":listTerminals"):
		_ = json.NewEncoder(w).Encode(map[string]any{"terminals": p.terminals})
	case strings.HasSuffix(r.URL.Path, ":closeTerminal"):
		_ = json.NewDecoder(r.Body).Decode(&p.terminalReq)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
	case strings.HasSuffix(r.URL.Path, ":sendTerminalInput"):
		_ = json.NewDecoder(r.Body).Decode(&p.terminalReq)
		_ = json.NewEncoder(w).Encode(map[string]any{"ackedOffset": 5})
	case strings.HasSuffix(r.URL.Path, ":streamTerminalOutput"):
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: %s\n\n", `{"output":"aGkK","offset":3}`)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", `{"exitCode":0,"offset":3}`)
	case strings.HasSuffix(r.URL.Path, ":listTriggers"):
		_ = json.NewEncoder(w).Encode(map[string]any{"triggers": p.triggers})
	case strings.HasSuffix(r.URL.Path, ":listTriggerOptions"):
		_ = json.NewEncoder(w).Encode(map[string]any{"triggerOptions": []any{
			map[string]any{"id": "time", "displayName": "Scheduled", "category": "TRIGGER_CATEGORY_TIME"},
		}})
	case strings.HasSuffix(r.URL.Path, ":createTrigger"):
		_ = json.NewDecoder(r.Body).Decode(&p.triggerReq)
		var trigger map[string]any
		_ = json.Unmarshal([]byte(`{"triggerId":"trig-1","state":"TRIGGER_STATE_ACTIVE","displayName":"nightly"}`), &trigger)
		_ = json.NewEncoder(w).Encode(map[string]any{"trigger": trigger})
	case strings.HasSuffix(r.URL.Path, ":updateTrigger"):
		_ = json.NewDecoder(r.Body).Decode(&p.triggerReq)
		_ = json.NewEncoder(w).Encode(map[string]any{"trigger": map[string]any{"triggerId": "trig-1", "state": "TRIGGER_STATE_PAUSED"}})
	case strings.HasSuffix(r.URL.Path, ":deleteTrigger"):
		_ = json.NewDecoder(r.Body).Decode(&p.triggerReq)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
	case strings.HasSuffix(r.URL.Path, ":uploadProjectFile"):
		_ = json.NewDecoder(r.Body).Decode(&p.uploadReq)
		_ = json.NewEncoder(w).Encode(map[string]any{"resource": map[string]any{
			"type": "FILE", "resourceId": "a.txt", "displayName": "a.txt", "sizeBytes": 3, "projectId": "p1",
		}})
	case strings.HasSuffix(r.URL.Path, ":listCustomizations"):
		_ = json.NewDecoder(r.Body).Decode(&p.terminalReq)
		_ = json.NewEncoder(w).Encode(p.customizations)
	case strings.HasSuffix(r.URL.Path, ":getTurnDiff"):
		_ = json.NewDecoder(r.Body).Decode(&p.diffReq)
		_ = json.NewEncoder(w).Encode(p.turnDiff)
	case strings.HasSuffix(r.URL.Path, ":getCodeActionDiff"):
		_ = json.NewDecoder(r.Body).Decode(&p.diffReq)
		_ = json.NewEncoder(w).Encode(map[string]any{"diff": p.codeDiff})
	case strings.HasSuffix(r.URL.Path, ":listPlugins"):
		_ = json.NewEncoder(w).Encode(map[string]any{"plugins": p.plugins})
	case strings.HasSuffix(r.URL.Path, ":listPluginInstallations"):
		_ = json.NewEncoder(w).Encode(map[string]any{"installations": p.installations})
	case strings.HasSuffix(r.URL.Path, ":installPlugin"):
		_ = json.NewDecoder(r.Body).Decode(&p.pluginReq)
		_ = json.NewEncoder(w).Encode(map[string]any{"authorizeUri": "https://example.invalid/authorize"})
	case strings.HasSuffix(r.URL.Path, ":uninstallPlugin"):
		_ = json.NewDecoder(r.Body).Decode(&p.pluginReq)
		_ = json.NewEncoder(w).Encode(map[string]any{})
	case strings.HasSuffix(r.URL.Path, ":listSlashCommands"):
		_ = json.NewEncoder(w).Encode(map[string]any{"slashCommands": p.commands})
	case strings.HasSuffix(r.URL.Path, ":readDir"):
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		p.dirPath = body["path"]
		_ = json.NewEncoder(w).Encode(map[string]any{"entries": p.entries})
	case strings.HasSuffix(r.URL.Path, ":readFile"):
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		p.filePath = body["path"]
		_ = json.NewEncoder(w).Encode(map[string]any{"content": p.fileBody, "exists": p.fileExists})
	case strings.HasSuffix(r.URL.Path, ":streamAgentStateUpdates"):
		w.Header().Set("Content-Type", "text/event-stream")
		for _, line := range strings.Split(p.streamBody, "\n") {
			if line == "" {
				continue
			}
			_, _ = w.Write([]byte("data: " + line + "\n\n"))
		}
	default:
		http.NotFound(w, r)
	}
}

func newStub(t *testing.T) (*proxyStub, *Client, func()) {
	t.Helper()
	stub := &proxyStub{}
	srv := httptest.NewServer(http.HandlerFunc(stub.handler))
	client := New(Config{
		BaseURL:   srv.URL,
		ProjectID: "project-from-flag",
		TokenFunc: func(context.Context) (string, error) { return "test-token", nil },
	})
	return stub, client, srv.Close
}

func TestStartCascadeSendsTheTurnAndBearer(t *testing.T) {
	stub, client, done := newStub(t)
	defer done()

	id, err := client.StartCascade(context.Background(), "gemini-3.8-flash-high", "file:///tmp/ws", "hi")
	if err != nil {
		t.Fatalf("StartCascade: %v", err)
	}
	if id != "cascade-1" {
		t.Fatalf("cascade id = %q", id)
	}
	if stub.authHeader != "Bearer test-token" {
		t.Errorf("Authorization = %q", stub.authHeader)
	}
	if stub.userAgent == "" {
		t.Error("User-Agent is empty")
	}
	if got := stub.startReq["model"]; got != "gemini-3.8-flash-high" {
		t.Errorf("model = %v", got)
	}
	if got := stub.startReq["initialPrompt"]; got != "hi" {
		t.Errorf("initialPrompt = %v", got)
	}
	if got := stub.startReq["projectId"]; got != "project-from-flag" {
		t.Errorf("projectId = %v", got)
	}
	uris, _ := stub.startReq["workspaceUris"].([]any)
	if len(uris) != 1 || uris[0] != "file:///tmp/ws" {
		t.Errorf("workspaceUris = %v", stub.startReq["workspaceUris"])
	}
}

func TestTokenIsCachedAcrossRequests(t *testing.T) {
	stub := &proxyStub{}
	srv := httptest.NewServer(http.HandlerFunc(stub.handler))
	defer srv.Close()
	calls := 0
	client := New(Config{
		BaseURL: srv.URL,
		TokenFunc: func(context.Context) (string, error) {
			calls++
			return "cached-token", nil
		},
	})
	if _, err := client.StartCascade(context.Background(), "m", "", "one"); err != nil {
		t.Fatalf("StartCascade: %v", err)
	}
	if _, err := client.ListSlashCommands(context.Background()); err != nil {
		t.Fatalf("ListSlashCommands: %v", err)
	}
	if calls != 1 {
		t.Errorf("TokenFunc calls = %d, want 1 (cached)", calls)
	}
}

func TestSendAndCancel(t *testing.T) {
	stub, client, done := newStub(t)
	defer done()

	if err := client.SendMessage(context.Background(), "cascade-1", "next"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if got := stub.sendReq["cascadeId"]; got != "cascade-1" {
		t.Errorf("cascadeId = %v", got)
	}
	if got := stub.sendReq["messageText"]; got != "next" {
		t.Errorf("messageText = %v", got)
	}
	if err := client.Cancel(context.Background(), "cascade-1"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if got := stub.cancelReq["cascadeId"]; got != "cascade-1" {
		t.Errorf("cancel cascadeId = %v", got)
	}
}

func TestResolveProjectByIDAndTitle(t *testing.T) {
	stub, client, done := newStub(t)
	defer done()
	stub.projects = []Project{
		{ProjectID: "p-1", Title: "CLI Project (a@b)"},
		{ProjectID: "p-2", Title: "CLI Project (c@d)"},
	}

	got, err := client.ResolveProject(context.Background(), "CLI Project (c@d)")
	if err != nil {
		t.Fatalf("ResolveProject by title: %v", err)
	}
	if got != "p-2" {
		t.Errorf("resolved by title = %q", got)
	}
	got, err = client.ResolveProject(context.Background(), "p-1")
	if err != nil {
		t.Fatalf("ResolveProject by id: %v", err)
	}
	if got != "p-1" {
		t.Errorf("resolved by id = %q", got)
	}
	if _, err := client.ResolveProject(context.Background(), "missing"); err == nil {
		t.Error("ResolveProject(missing) = nil error")
	}
}

func TestTurnDiffRPCs(t *testing.T) {
	stub, client, done := newStub(t)
	defer done()
	stub.turnDiff = TurnDiff{
		FileDiffs: map[string]FileDiff{
			"main.go": {Additions: 3, Deletions: 1, OriginalContents: "a\n", ModifiedContents: "b\n"},
		},
		TotalAdditions: 3,
		TotalDeletions: 1,
		TurnStartIndex: 4,
	}
	stub.codeDiff = map[string]any{
		"startLine": 3,
		"endLine":   4,
		"unifiedDiff": map[string]any{"lines": []any{
			map[string]any{"type": "UNIFIED_DIFF_LINE_TYPE_INSERT", "text": "new"},
			map[string]any{"type": float64(2), "text": "old"},
		}},
	}

	diff, err := client.GetTurnDiff(context.Background(), "cascade-1", 7)
	if err != nil {
		t.Fatalf("GetTurnDiff: %v", err)
	}
	if stub.diffReq["conversationId"] != "cascade-1" || stub.diffReq["stepIndex"] != float64(7) {
		t.Errorf("request = %+v", stub.diffReq)
	}
	fd := diff.FileDiffs["main.go"]
	if fd.Additions != 3 || fd.Deletions != 1 || fd.ModifiedContents != "b\n" {
		t.Errorf("fileDiff = %+v", fd)
	}
	if diff.TotalAdditions != 3 || diff.TotalDeletions != 1 || diff.TurnStartIndex != 4 {
		t.Errorf("totals = %+v", diff)
	}

	block, err := client.GetCodeActionDiff(context.Background(), "cascade-1", 9)
	if err != nil {
		t.Fatalf("GetCodeActionDiff: %v", err)
	}
	if block.StartLine != 3 || block.EndLine != 4 || block.UnifiedDiff == nil {
		t.Fatalf("block = %+v", block)
	}
	lines := block.UnifiedDiff.Lines
	if len(lines) != 2 || lines[0].Type != UnifiedLineInsert || lines[0].Text != "new" {
		t.Errorf("lines = %+v", lines)
	}
	if lines[1].Type != UnifiedLineDelete || lines[1].Text != "old" {
		t.Errorf("numeric enum line = %+v", lines[1])
	}
	if stub.diffReq["stepIndex"] != float64(9) {
		t.Errorf("req = %+v", stub.diffReq)
	}
}

func TestTriggerRPCs(t *testing.T) {
	stub, client, done := newStub(t)
	defer done()
	stub.triggers = []any{map[string]any{
		"triggerId": "trig-1", "state": "TRIGGER_STATE_ACTIVE", "displayName": "nightly",
		"prompt": "run it", "optionId": "time",
		"fields": map[string]any{"schedule": map[string]any{"values": []any{"0 0 * * *"}}},
	}}

	triggers, _, err := client.ListTriggers(context.Background(), 10, "")
	if err != nil {
		t.Fatalf("ListTriggers: %v", err)
	}
	if len(triggers) != 1 || triggers[0].State != TriggerStateActive || triggers[0].DisplayName != "nightly" {
		t.Fatalf("triggers = %+v", triggers)
	}
	if got := triggers[0].Fields["schedule"].Values; len(got) != 1 || got[0] != "0 0 * * *" {
		t.Errorf("fields = %+v", triggers[0].Fields)
	}

	options, err := client.ListTriggerOptions(context.Background())
	if err != nil || len(options) != 1 || options[0].ID != "time" {
		t.Fatalf("options = %+v, %v", options, err)
	}

	created, err := client.CreateTrigger(context.Background(), RemoteTrigger{DisplayName: "nightly", Prompt: "run it", OptionID: "time"})
	if err != nil || created.TriggerID != "trig-1" {
		t.Fatalf("CreateTrigger = %+v, %v", created, err)
	}
	if stub.triggerReq["trigger"] == nil {
		t.Errorf("create request = %+v", stub.triggerReq)
	}

	updated, err := client.UpdateTrigger(context.Background(), RemoteTrigger{TriggerID: "trig-1", State: TriggerStatePaused}, []string{"state"})
	if err != nil || updated.State != TriggerStatePaused {
		t.Fatalf("UpdateTrigger = %+v, %v", updated, err)
	}
	mask, _ := stub.triggerReq["updateMask"].([]any)
	if len(mask) != 1 || mask[0] != "state" {
		t.Errorf("updateMask = %+v", stub.triggerReq["updateMask"])
	}
	updatedTrigger, _ := stub.triggerReq["trigger"].(map[string]any)
	if updatedTrigger["state"] != "TRIGGER_STATE_PAUSED" {
		t.Errorf("update state = %v, want the enum name", updatedTrigger["state"])
	}

	if err := client.DeleteTrigger(context.Background(), "trig-1", true); err != nil {
		t.Fatalf("DeleteTrigger: %v", err)
	}
	if stub.triggerReq["triggerId"] != "trig-1" || stub.triggerReq["abortActiveRuns"] != true {
		t.Errorf("delete request = %+v", stub.triggerReq)
	}
}

func TestUploadProjectFile(t *testing.T) {
	stub, client, done := newStub(t)
	defer done()

	resource, err := client.UploadProjectFile(context.Background(), "p1", "a.txt", []byte("abc"), "text/plain")
	if err != nil {
		t.Fatalf("UploadProjectFile: %v", err)
	}
	if stub.uploadReq["projectId"] != "p1" || stub.uploadReq["fileName"] != "a.txt" || stub.uploadReq["mimeType"] != "text/plain" {
		t.Errorf("request = %+v", stub.uploadReq)
	}
	if stub.uploadReq["content"] != "YWJj" {
		t.Errorf("content = %v, want base64 YWJj", stub.uploadReq["content"])
	}
	if resource.ResourceID != "a.txt" || resource.SizeBytes != 3 || resource.ProjectID != "p1" {
		t.Errorf("resource = %+v", resource)
	}
}

func TestListCustomizations(t *testing.T) {
	stub, client, done := newStub(t)
	defer done()
	stub.customizations = map[string]any{"skills": []any{
		map[string]any{
			"path": "/skills/a/SKILL.md", "name": "a", "displayName": "A", "description": "desc",
			"isBuiltin": true, "pluginName": "plug", "icon": "i",
		},
		map[string]any{"name": "hidden", "disableSlashCommand": true},
	}}

	got, err := client.ListCustomizations(context.Background(), "file:///ws", "user@example.com")
	if err != nil {
		t.Fatalf("ListCustomizations: %v", err)
	}
	if stub.terminalReq["workspaceUri"] != "file:///ws" || stub.terminalReq["executionIdentity"] != "user@example.com" {
		t.Errorf("request = %+v", stub.terminalReq)
	}
	if len(got.Skills) != 2 || got.Skills[0].DisplayName != "A" || !got.Skills[0].IsBuiltin || got.Skills[0].PluginName != "plug" {
		t.Errorf("skills = %+v", got.Skills)
	}
}

func TestTerminalRPCs(t *testing.T) {
	stub, client, done := newStub(t)
	defer done()
	stub.terminal = TerminalInfo{TerminalID: "term-1", Title: "Terminal", Cwd: "/ws", Pid: 42}
	stub.terminals = []TerminalInfo{stub.terminal}

	info, err := client.CreateTerminal(context.Background(), "cascade-1", "req-1")
	if err != nil {
		t.Fatalf("CreateTerminal: %v", err)
	}
	if info.TerminalID != "term-1" || info.Pid != 42 || stub.terminalReq["conversationId"] != "cascade-1" {
		t.Errorf("info = %+v req = %+v", info, stub.terminalReq)
	}
	list, err := client.ListTerminals(context.Background(), "cascade-1")
	if err != nil || len(list) != 1 || list[0].TerminalID != "term-1" {
		t.Fatalf("ListTerminals = %+v, %v", list, err)
	}
	if err := client.CloseTerminal(context.Background(), "term-1"); err != nil {
		t.Fatalf("CloseTerminal: %v", err)
	}
	if stub.terminalReq["terminalId"] != "term-1" {
		t.Errorf("close req = %+v", stub.terminalReq)
	}
	if acked, err := client.SendTerminalInput(context.Background(), "term-1", []byte("hi\n"), "s1", 0); err != nil || acked != 5 {
		t.Fatalf("SendTerminalInput = %d, %v", acked, err)
	}

	ch, err := client.StreamTerminalOutput(context.Background(), "term-1", 0)
	if err != nil {
		t.Fatalf("StreamTerminalOutput: %v", err)
	}
	var got []TerminalEvent
	for event := range ch {
		got = append(got, event)
	}
	if len(got) != 2 || string(got[0].Output) != "hi\n" || got[1].ExitCode == nil || *got[1].ExitCode != 0 {
		t.Errorf("events = %+v", got)
	}
}

func TestPluginRPCs(t *testing.T) {
	stub, client, done := newStub(t)
	defer done()
	stub.plugins = []PluginInfo{{PluginID: "github", DisplayName: "GitHub", Version: "1.2", Description: "Issues and PRs"}}
	stub.installations = []PluginInstallation{{PluginID: "github", Target: "REMOTE"}}

	plugins, err := client.ListPlugins(context.Background())
	if err != nil {
		t.Fatalf("ListPlugins: %v", err)
	}
	if len(plugins) != 1 || plugins[0].PluginID != "github" || plugins[0].Version != "1.2" {
		t.Errorf("plugins = %+v", plugins)
	}
	installs, err := client.ListPluginInstallations(context.Background())
	if err != nil {
		t.Fatalf("ListPluginInstallations: %v", err)
	}
	if len(installs) != 1 || installs[0].Target != "REMOTE" {
		t.Errorf("installations = %+v", installs)
	}

	uri, err := client.InstallPlugin(context.Background(), "github", "REMOTE")
	if err != nil || uri != "https://example.invalid/authorize" {
		t.Fatalf("InstallPlugin = (%q, %v)", uri, err)
	}
	if stub.pluginReq["pluginId"] != "github" || stub.pluginReq["target"] != "REMOTE" {
		t.Errorf("install request = %+v", stub.pluginReq)
	}
	if err := client.UninstallPlugin(context.Background(), "github", "REMOTE"); err != nil {
		t.Fatalf("UninstallPlugin: %v", err)
	}
}

func TestSessionLifecycleRPCs(t *testing.T) {
	stub, client, done := newStub(t)
	defer done()
	ctx := context.Background()

	env, err := client.EnsureEnvironment(ctx, "user@example.com")
	if err != nil {
		t.Fatalf("EnsureEnvironment: %v", err)
	}
	if env["connectionState"] != "CONNECTION_STATE_CONNECTED" {
		t.Errorf("environment = %+v", env)
	}

	settings, err := client.GetPermissionSettings(ctx)
	if err != nil {
		t.Fatalf("GetPermissionSettings: %v", err)
	}
	if settings.PermissionPreset != "AUTO_APPROVE" {
		t.Errorf("settings = %+v", settings)
	}
	if err := client.SetPermissionPreset(ctx, "TURBO"); err != nil {
		t.Fatalf("SetPermissionPreset: %v", err)
	}
	if stub.setPreset != "TURBO" {
		t.Errorf("setPreset = %q", stub.setPreset)
	}

	conv, err := client.GetConversation(ctx, "cascade-1")
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if conv.ConversationID != "cascade-1" || conv.ModelDisplayName != "Gemini" {
		t.Errorf("conversation = %+v", conv)
	}

	title, err := client.GenerateConversationTitle(ctx, "hello world")
	if err != nil || title != "Hi there" {
		t.Fatalf("GenerateConversationTitle = (%q, %v)", title, err)
	}

	if err := client.HandleCascadeUserInteraction(ctx, "cascade-1", map[string]any{"decision": "allow"}); err != nil {
		t.Fatalf("HandleCascadeUserInteraction: %v", err)
	}
	if stub.interaction == nil {
		t.Error("HandleCascadeUserInteraction did not send a body")
	}
	if stub.interaction["conversationId"] != "cascade-1" {
		t.Errorf("conversationId = %v", stub.interaction["conversationId"])
	}
}

func TestReadDirAndReadFile(t *testing.T) {
	stub, client, done := newStub(t)
	defer done()
	stub.entries = []DirEntry{{Name: "go.mod", Size: 12}, {Name: "cmd", IsDir: true}}
	stub.fileBody = "module example"
	stub.fileExists = true

	entries, err := client.ReadDir(context.Background(), "/workspace")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if stub.dirPath != "/workspace" {
		t.Errorf("readDir path = %q", stub.dirPath)
	}
	if len(entries) != 2 || entries[0].Name != "go.mod" || !entries[1].IsDir {
		t.Fatalf("entries = %+v", entries)
	}

	content, exists, err := client.ReadFile(context.Background(), "/workspace/go.mod")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if stub.filePath != "/workspace/go.mod" {
		t.Errorf("readFile path = %q", stub.filePath)
	}
	if !exists || string(content) != "module example" {
		t.Errorf("ReadFile = (%q, %v)", content, exists)
	}
}

func TestListSlashCommands(t *testing.T) {
	stub, client, done := newStub(t)
	defer done()
	stub.commands = []Command{{Name: "compact", Description: "compact"}, {Name: "/clear", Description: "clear"}}

	cmds, err := client.ListSlashCommands(context.Background())
	if err != nil {
		t.Fatalf("ListSlashCommands: %v", err)
	}
	if len(cmds) != 2 || cmds[0].Name != "compact" || cmds[1].Name != "/clear" {
		t.Fatalf("commands = %+v", cmds)
	}
}

func TestStreamDecodesFramesAndKeepsResumeToken(t *testing.T) {
	stub, client, done := newStub(t)
	defer done()
	stub.streamBody = `{"update":{"fullyIdle":false},"resumeToken":"tok-1"}
{"update":{"fullyIdle":true},"resumeToken":"tok-2"}`

	updates, err := client.Stream(context.Background(), "cascade-1")
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var got []string
	for update := range updates {
		got = append(got, update.ResumeToken)
	}
	if len(got) != 2 || got[0] != "tok-1" || got[1] != "tok-2" {
		t.Fatalf("resume tokens = %v", got)
	}
}

func TestTokenErrorIsReported(t *testing.T) {
	client := New(Config{
		BaseURL: "http://127.0.0.1:1",
		TokenFunc: func(context.Context) (string, error) {
			return "", errors.New("no signed-in account")
		},
	})
	if _, err := client.StartCascade(context.Background(), "m", "", "hi"); err == nil {
		t.Fatal("StartCascade without a token = nil error")
	} else if !strings.Contains(err.Error(), "no signed-in account") {
		t.Fatalf("error = %v", err)
	}
}

func TestHTTPErrorIsReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer srv.Close()
	client := New(Config{BaseURL: srv.URL})

	if _, err := client.StartCascade(context.Background(), "m", "", "hi"); err == nil {
		t.Fatal("StartCascade on HTTP 403 = nil error")
	} else if !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("error = %v", err)
	}
}
