// Package remoteagentclient is the client half of the Antigravity remote
// agent proxy protocol (google.gca.aicode.v1main.JetskiRemoteAgentProxyService).
//
// The clone implements the server side in internal/infra/mockremoteagent; this
// package lets `pigasus --remote-agent` drive the same protocol against either
// that mock or a real remote agent. It speaks the REST/SSE dialect documented
// in docs/guide/remote-agents.md:
//
//	POST /v1alpha/remote:startCascade
//	POST /v1alpha/remote:sendUserCascadeMessage
//	POST /v1alpha/remote:streamAgentStateUpdates?alt=sse
//	POST /v1alpha/remote:cancelCascade
//	GET  /v1alpha/remote:listProjects
//	GET  /v1alpha/remote:listSlashCommands
//
// The wire is JSON. A real CloudCode proxy may speak Connect/gRPC-Web instead;
// this client is the local/mock and REST-compatible path.
package remoteagentclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/geminicode"
)

// DefaultEndpoint is the reference's production remote-agent host.
const DefaultEndpoint = "https://aicode.googleapis.com"

// StateUpdate is one frame from streamAgentStateUpdates.
type StateUpdate struct {
	Update      map[string]any `json:"update"`
	ResumeToken string         `json:"resumeToken,omitempty"`
}

// Project is one remote project returned by listProjects.
type Project struct {
	ProjectID string `json:"projectId"`
	Title     string `json:"title"`
}

// Command is one remote slash command.
type Command struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Client calls one remote agent proxy.
type Client struct {
	baseURL   string
	tokenFunc func(context.Context) (string, error)
	http      *http.Client
	projectID string
	userAgent string

	// token caching keeps one WebSocket-style listing (readDir walks) from
	// refreshing the credential once per request.
	tokenMu  sync.Mutex
	tokenVal string
	tokenExp time.Time
}

// Config configures a Client.
type Config struct {
	// BaseURL is AICODE_ENDPOINT_URL (or DefaultEndpoint).
	BaseURL string
	// TokenFunc returns the bearer token for every request.
	TokenFunc func(context.Context) (string, error)
	// ProjectID is the --project value, when it is already an ID.
	ProjectID string
	// UserAgent overrides the reference User-Agent (tests).
	UserAgent string
	// HTTPClient overrides the transport (tests).
	HTTPClient *http.Client
}

// New builds a client. A missing BaseURL uses DefaultEndpoint.
func New(cfg Config) *Client {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = DefaultEndpoint
	}
	ua := strings.TrimSpace(cfg.UserAgent)
	if ua == "" {
		ua = geminicode.UserAgent()
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Client{
		baseURL:   base,
		tokenFunc: cfg.TokenFunc,
		http:      httpClient,
		projectID: strings.TrimSpace(cfg.ProjectID),
		userAgent: ua,
	}
}

// ProjectID returns the configured project id.
func (c *Client) ProjectID() string { return c.projectID }

// SetProjectID records the project a later startCascade uses.
func (c *Client) SetProjectID(id string) { c.projectID = strings.TrimSpace(id) }

func (c *Client) methodURL(method string) string {
	return c.baseURL + "/v1alpha/remote:" + method
}

func (c *Client) token(ctx context.Context) (string, error) {
	if c.tokenFunc == nil {
		return "", nil
	}
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	if c.tokenVal != "" && time.Now().Before(c.tokenExp) {
		return c.tokenVal, nil
	}
	tok, err := c.tokenFunc(ctx)
	if err != nil {
		return "", err
	}
	c.tokenVal = tok
	c.tokenExp = time.Now().Add(5 * time.Minute)
	return tok, nil
}

func (c *Client) newRequest(ctx context.Context, method, target string, body []byte) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	tok, err := c.token(ctx)
	if err != nil {
		// Surface "not signed in" (or a refresh failure) directly. Sending
		// the request without a bearer only produces a less useful 401.
		return nil, fmt.Errorf("remote agent: %w", err)
	}
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	return req, nil
}

// post sends one JSON request and decodes a JSON response into out (when non-nil).
func (c *Client) post(ctx context.Context, method string, in, out any) error {
	var body []byte
	var err error
	if in != nil {
		body, err = json.Marshal(in)
		if err != nil {
			return err
		}
	}
	req, err := c.newRequest(ctx, http.MethodPost, c.methodURL(method), body)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("remote agent %s: HTTP %d: %s", method, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("remote agent %s: decode response: %w", method, err)
		}
	}
	return nil
}

// get sends a GET request and decodes the JSON response into out.
func (c *Client) get(ctx context.Context, method string, query url.Values, out any) error {
	target := c.methodURL(method)
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := c.newRequest(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("remote agent %s: HTTP %d: %s", method, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("remote agent %s: decode response: %w", method, err)
		}
	}
	return nil
}

// StartCascade starts a remote conversation and returns its cascade id.
func (c *Client) StartCascade(ctx context.Context, model, workspaceURI, prompt string) (string, error) {
	in := map[string]any{
		"initialPrompt": prompt,
		"model":         model,
	}
	if c.projectID != "" {
		in["projectId"] = c.projectID
	}
	if workspaceURI != "" {
		in["workspaceUris"] = []string{workspaceURI}
	}
	var out struct {
		CascadeID string `json:"cascadeId"`
	}
	if err := c.post(ctx, "startCascade", in, &out); err != nil {
		return "", err
	}
	if out.CascadeID == "" {
		return "", fmt.Errorf("remote agent startCascade: empty cascade id")
	}
	return out.CascadeID, nil
}

// SendMessage sends one user message to an existing cascade.
func (c *Client) SendMessage(ctx context.Context, cascadeID, text string) error {
	return c.post(ctx, "sendUserCascadeMessage", map[string]any{
		"cascadeId":   cascadeID,
		"messageText": text,
	}, nil)
}

// Cancel stops the cascade's running turn.
func (c *Client) Cancel(ctx context.Context, cascadeID string) error {
	return c.post(ctx, "cancelCascade", map[string]any{"cascadeId": cascadeID}, nil)
}

// ListProjects lists the remote projects visible to the signed-in account.
func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	var out struct {
		Projects []Project `json:"projects"`
	}
	if err := c.get(ctx, "listProjects", nil, &out); err != nil {
		return nil, err
	}
	return out.Projects, nil
}

// ResolveProject maps --project (id or title) to a project id. An empty name
// keeps the client's configured id; an unknown name is an error.
func (c *Client) ResolveProject(ctx context.Context, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return c.projectID, nil
	}
	projects, err := c.ListProjects(ctx)
	if err != nil {
		// A server that cannot list projects may still accept the name
		// as an id; do not fail the session for that.
		c.projectID = name
		return name, nil
	}
	for _, p := range projects {
		if strings.EqualFold(p.ProjectID, name) || strings.EqualFold(p.Title, name) {
			c.projectID = p.ProjectID
			return p.ProjectID, nil
		}
	}
	return "", fmt.Errorf("remote agent: project %q not found", name)
}

// DirEntry is one remote directory entry.
type DirEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
	Size  int64  `json:"size,omitempty"`
}

// ReadDir lists one directory in the remote workspace.
func (c *Client) ReadDir(ctx context.Context, path string) ([]DirEntry, error) {
	var out struct {
		Entries []DirEntry `json:"entries"`
	}
	if err := c.post(ctx, "readDir", map[string]string{"path": path}, &out); err != nil {
		return nil, err
	}
	return out.Entries, nil
}

// ReadFile reads one file from the remote workspace. exists is false when the
// remote server reports that the path is not a file.
func (c *Client) ReadFile(ctx context.Context, path string) (content []byte, exists bool, err error) {
	var out struct {
		Content string `json:"content"`
		Exists  bool   `json:"exists"`
	}
	if err := c.post(ctx, "readFile", map[string]string{"path": path}, &out); err != nil {
		return nil, false, err
	}
	return []byte(out.Content), out.Exists, nil
}

// EnsureEnvironment readies the remote execution environment (the ACE VM or
// capsule) before the first cascade. agy calls it once per session.
func (c *Client) EnsureEnvironment(ctx context.Context, identity string) (map[string]any, error) {
	in := map[string]any{}
	if strings.TrimSpace(identity) != "" {
		in["executionIdentity"] = strings.TrimSpace(identity)
	}
	var out struct {
		Environment map[string]any `json:"environment"`
	}
	if err := c.post(ctx, "ensureEnvironment", in, &out); err != nil {
		return nil, err
	}
	return out.Environment, nil
}

// PermissionSettings is the remote permission configuration.
type PermissionSettings struct {
	PermissionPreset   string `json:"permissionPreset"`
	ArtifactReviewMode string `json:"artifactReviewMode"`
}

// GetPermissionSettings reads the remote permission preset.
func (c *Client) GetPermissionSettings(ctx context.Context) (PermissionSettings, error) {
	var out PermissionSettings
	err := c.get(ctx, "getPermissionSettings", nil, &out)
	return out, err
}

// SetPermissionPreset updates the remote permission preset. The request
// field is `preset` (proto: SetPermissionPresetRequest.preset).
func (c *Client) SetPermissionPreset(ctx context.Context, preset string) error {
	in := map[string]string{"preset": strings.TrimSpace(preset)}
	if c.projectID != "" {
		in["projectId"] = c.projectID
	}
	return c.post(ctx, "setPermissionPreset", in, nil)
}

// Conversation is one remote conversation's snapshot.
type Conversation struct {
	ConversationID   string         `json:"conversationId"`
	Update           map[string]any `json:"update,omitempty"`
	ResumeToken      string         `json:"resumeToken,omitempty"`
	ModelDisplayName string         `json:"modelDisplayName,omitempty"`
}

// GetConversation reads one remote conversation by id.
func (c *Client) GetConversation(ctx context.Context, id string) (*Conversation, error) {
	var out Conversation
	if err := c.post(ctx, "getConversation", map[string]string{"conversationId": id}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GenerateConversationTitle asks the remote agent for a title.
func (c *Client) GenerateConversationTitle(ctx context.Context, prompt string) (string, error) {
	var out struct {
		Title string `json:"title"`
	}
	if err := c.post(ctx, "generateConversationTitle", map[string]string{"prompt": prompt}, &out); err != nil {
		return "", err
	}
	return out.Title, nil
}

// HandleCascadeUserInteraction answers a remote interaction (for example a
// tool permission question) that arrived as a WAITING step. The proxy's
// request field is `conversationId`; the answer rides under `interaction`.
func (c *Client) HandleCascadeUserInteraction(ctx context.Context, cascadeID string, interaction map[string]any) error {
	return c.post(ctx, "handleCascadeUserInteraction", map[string]any{
		"conversationId": cascadeID,
		"interaction":    interaction,
	}, nil)
}

// ListSlashCommands lists the remote agent's slash commands.
func (c *Client) ListSlashCommands(ctx context.Context) ([]Command, error) {
	var out struct {
		SlashCommands []Command `json:"slashCommands"`
	}
	if err := c.get(ctx, "listSlashCommands", nil, &out); err != nil {
		return nil, err
	}
	return out.SlashCommands, nil
}

// Stream opens the state-update SSE stream for a cascade. The returned channel
// closes when the stream ends or ctx is cancelled.
func (c *Client) Stream(ctx context.Context, cascadeID string) (<-chan StateUpdate, error) {
	body, _ := json.Marshal(map[string]any{
		"conversationId":      cascadeID,
		"trajectoryVerbosity": "CLIENT_TRAJECTORY_VERBOSITY_FULL",
	})
	target := c.methodURL("streamAgentStateUpdates")
	q := url.Values{}
	q.Set("alt", "sse")
	q.Set("conversationId", cascadeID)
	target += "?" + q.Encode()

	req, err := c.newRequest(ctx, http.MethodPost, target, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("remote agent stream: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	out := make(chan StateUpdate, 64)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 20*1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "" || payload == "[DONE]" {
				continue
			}
			var update StateUpdate
			if err := json.Unmarshal([]byte(payload), &update); err != nil {
				continue
			}
			select {
			case out <- update:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

// unaryTimeout bounds list/project calls (not the stream).
const unaryTimeout = 10 * time.Second

// WithUnaryTimeout returns a child context bounded for a unary call.
func WithUnaryTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, unaryTimeout)
}

// PluginInfo is one plugin offered by the remote environment.
type PluginInfo struct {
	PluginID     string `json:"pluginId"`
	DisplayName  string `json:"displayName"`
	Version      string `json:"version,omitempty"`
	Description  string `json:"description,omitempty"`
	AuthRequired bool   `json:"authRequired,omitempty"`
}

// PluginInstallation is one installed plugin.
type PluginInstallation struct {
	PluginID string `json:"pluginId"`
	Target   string `json:"target,omitempty"`
}

// ListPlugins lists the plugins the remote environment offers.
func (c *Client) ListPlugins(ctx context.Context) ([]PluginInfo, error) {
	var out struct {
		Plugins []PluginInfo `json:"plugins"`
	}
	if err := c.get(ctx, "listPlugins", nil, &out); err != nil {
		return nil, err
	}
	return out.Plugins, nil
}

// ListPluginInstallations lists the plugins installed in the remote environment.
func (c *Client) ListPluginInstallations(ctx context.Context) ([]PluginInstallation, error) {
	var out struct {
		Installations []PluginInstallation `json:"installations"`
	}
	if err := c.get(ctx, "listPluginInstallations", nil, &out); err != nil {
		return nil, err
	}
	return out.Installations, nil
}

// InstallPlugin installs a plugin in the remote environment. A non-empty
// authorize URI means the plugin still needs the user's browser consent.
func (c *Client) InstallPlugin(ctx context.Context, pluginID, target string) (string, error) {
	in := map[string]any{"pluginId": pluginID, "target": target}
	var out struct {
		AuthorizeURI string `json:"authorizeUri"`
	}
	if err := c.post(ctx, "installPlugin", in, &out); err != nil {
		return "", err
	}
	return out.AuthorizeURI, nil
}

// UninstallPlugin removes a plugin from the remote environment.
func (c *Client) UninstallPlugin(ctx context.Context, pluginID, target string) error {
	return c.post(ctx, "uninstallPlugin", map[string]any{"pluginId": pluginID, "target": target}, nil)
}

// FileDiff is one file's change in a remote turn.
//
// The proxy's FileDiffData carries the two full file versions and the
// line counts; the map key is the path.
type FileDiff struct {
	Additions        int32  `json:"additions,omitempty"`
	Deletions        int32  `json:"deletions,omitempty"`
	IsArtifactFile   bool   `json:"isArtifactFile,omitempty"`
	OriginalContents string `json:"originalContents,omitempty"`
	ModifiedContents string `json:"modifiedContents,omitempty"`
}

// TurnDiff is the proxy's diff for one turn.
type TurnDiff struct {
	FileDiffs             map[string]FileDiff `json:"fileDiffs"`
	TotalAdditions        int32               `json:"totalAdditions"`
	TotalDeletions        int32               `json:"totalDeletions"`
	TurnStartIndex        int32               `json:"turnStartIndex"`
	TurnEndIndexExclusive int32               `json:"turnEndIndexExclusive"`
}

// GetTurnDiff reads the diff of the turn that contains stepIndex.
func (c *Client) GetTurnDiff(ctx context.Context, cascadeID string, stepIndex int) (TurnDiff, error) {
	in := map[string]any{"conversationId": cascadeID, "stepIndex": stepIndex}
	var out TurnDiff
	if err := c.post(ctx, "getTurnDiff", in, &out); err != nil {
		return TurnDiff{}, err
	}
	return out, nil
}

// UnifiedDiffLineType is exa.diff_action_pb.UnifiedDiffLineType.
//
// The enum values are recovered from the reference's descriptor string
// table (out/local-macos/xref/strings-addrs.tsv, diff_action.proto):
// UNSPECIFIED / INSERT / DELETE / UNCHANGED, in that declaration order.
type UnifiedDiffLineType int32

const (
	UnifiedLineUnspecified UnifiedDiffLineType = 0
	UnifiedLineInsert      UnifiedDiffLineType = 1
	UnifiedLineDelete      UnifiedDiffLineType = 2
	UnifiedLineUnchanged   UnifiedDiffLineType = 3
)

// UnmarshalJSON accepts the proto-JSON enum name and the numeric form.
func (t *UnifiedDiffLineType) UnmarshalJSON(data []byte) error {
	raw := strings.Trim(string(data), `"`)
	switch strings.ToUpper(raw) {
	case "", "UNIFIED_DIFF_LINE_TYPE_UNSPECIFIED":
		*t = UnifiedLineUnspecified
	case "UNIFIED_DIFF_LINE_TYPE_INSERT":
		*t = UnifiedLineInsert
	case "UNIFIED_DIFF_LINE_TYPE_DELETE":
		*t = UnifiedLineDelete
	case "UNIFIED_DIFF_LINE_TYPE_UNCHANGED":
		*t = UnifiedLineUnchanged
	default:
		n, err := strconv.Atoi(raw)
		if err != nil {
			return fmt.Errorf("unified diff line type: unknown %q", raw)
		}
		*t = UnifiedDiffLineType(n)
	}
	return nil
}

// UnifiedDiffLine is one line of a DiffBlock's unified diff.
type UnifiedDiffLine struct {
	Type UnifiedDiffLineType `json:"type"`
	Text string              `json:"text"`
}

// UnifiedDiff is the line list under DiffBlock.unifiedDiff.
type UnifiedDiff struct {
	Lines []UnifiedDiffLine `json:"lines"`
}

// CodeActionDiff is exa.diff_action_pb.DiffBlock.
type CodeActionDiff struct {
	StartLine   int32        `json:"startLine"`
	EndLine     int32        `json:"endLine"`
	UnifiedDiff *UnifiedDiff `json:"unifiedDiff"`
}

// GetCodeActionDiff reads the diff block of one code-action step. The
// block's unifiedDiff carries the lines the reference renders.
func (c *Client) GetCodeActionDiff(ctx context.Context, cascadeID string, stepIndex int) (CodeActionDiff, error) {
	var out struct {
		Diff CodeActionDiff `json:"diff"`
	}
	if err := c.post(ctx, "getCodeActionDiff", map[string]any{
		"conversationId": cascadeID,
		"stepIndex":      stepIndex,
	}, &out); err != nil {
		return CodeActionDiff{}, err
	}
	return out.Diff, nil
}

// TerminalInfo is one remote terminal (exa.language_server_pb.TerminalInfo).
type TerminalInfo struct {
	TerminalID     string `json:"terminalId"`
	Title          string `json:"title"`
	Cwd            string `json:"cwd,omitempty"`
	Pid            int32  `json:"pid,omitempty"`
	ProjectID      string `json:"projectId,omitempty"`
	WorkspaceURI   string `json:"workspaceUri,omitempty"`
	ConversationID string `json:"conversationId,omitempty"`
	SplitGroupID   string `json:"splitGroupId,omitempty"`
	CreatedAt      string `json:"createdAt,omitempty"`
}

// TerminalEvent is one StreamTerminalOutput frame. Output is a bytes
// field on the wire, so proto-JSON carries it as base64.
type TerminalEvent struct {
	Output       []byte `json:"output,omitempty"`
	ExitCode     *int32 `json:"exitCode,omitempty"`
	Offset       int64  `json:"offset,omitempty"`
	Title        string `json:"title,omitempty"`
	Cwd          string `json:"cwd,omitempty"`
	IsScrollback bool   `json:"isScrollback,omitempty"`
}

// CreateTerminal spawns a terminal in the conversation's environment.
func (c *Client) CreateTerminal(ctx context.Context, cascadeID, requestID string) (TerminalInfo, error) {
	in := map[string]any{}
	if cascadeID != "" {
		in["conversationId"] = cascadeID
	}
	if requestID != "" {
		in["requestId"] = requestID
	}
	var out struct {
		Terminal TerminalInfo `json:"terminal"`
	}
	if err := c.post(ctx, "createTerminal", in, &out); err != nil {
		return TerminalInfo{}, err
	}
	return out.Terminal, nil
}

// ListTerminals lists the conversation's terminals.
func (c *Client) ListTerminals(ctx context.Context, cascadeID string) ([]TerminalInfo, error) {
	var out struct {
		Terminals []TerminalInfo `json:"terminals"`
	}
	if err := c.post(ctx, "listTerminals", map[string]string{"conversationId": cascadeID}, &out); err != nil {
		return nil, err
	}
	return out.Terminals, nil
}

// CloseTerminal ends a terminal.
func (c *Client) CloseTerminal(ctx context.Context, terminalID string) error {
	return c.post(ctx, "closeTerminal", map[string]string{"terminalId": terminalID}, nil)
}

// SendTerminalInput writes bytes (or a resize) to a terminal and returns
// the acknowledged offset.
func (c *Client) SendTerminalInput(ctx context.Context, terminalID string, input []byte, inputStreamID string, offset int64) (int64, error) {
	in := map[string]any{"terminalId": terminalID, "input": input, "inputOffset": offset}
	if inputStreamID != "" {
		in["inputStreamId"] = inputStreamID
	}
	var out struct {
		AckedOffset int64 `json:"ackedOffset"`
	}
	if err := c.post(ctx, "sendTerminalInput", in, &out); err != nil {
		return offset, err
	}
	return out.AckedOffset, nil
}

// StreamTerminalOutput streams a terminal's bytes from resumeOffset. The
// channel closes after the exit frame.
func (c *Client) StreamTerminalOutput(ctx context.Context, terminalID string, resumeOffset int64) (<-chan TerminalEvent, error) {
	in := map[string]any{"terminalId": terminalID, "resumeOffset": resumeOffset}
	body, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	target, err := url.Parse(c.methodURL("streamTerminalOutput"))
	if err != nil {
		return nil, err
	}
	target.RawQuery = "alt=sse"
	req, err := c.newRequest(ctx, http.MethodPost, target.String(), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("remote terminal stream: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	out := make(chan TerminalEvent, 64)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "" || payload == "[DONE]" {
				continue
			}
			var event TerminalEvent
			if err := json.Unmarshal([]byte(payload), &event); err != nil {
				continue
			}
			select {
			case out <- event:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

// CustomizationSkill is one exa.cortex_pb.WorkflowSpec, the skill shape
// ListCustomizations returns.
type CustomizationSkill struct {
	Path                   string `json:"path,omitempty"`
	Name                   string `json:"name,omitempty"`
	DisplayName            string `json:"displayName,omitempty"`
	Description            string `json:"description,omitempty"`
	Content                string `json:"content,omitempty"`
	IsBuiltin              bool   `json:"isBuiltin,omitempty"`
	DisableModelInvocation bool   `json:"disableModelInvocation,omitempty"`
	DisableSlashCommand    bool   `json:"disableSlashCommand,omitempty"`
	PluginName             string `json:"pluginName,omitempty"`
	DiscoveredIn           string `json:"discoveredIn,omitempty"`
	Icon                   string `json:"icon,omitempty"`
	Logo                   string `json:"logo,omitempty"`
}

// Customizations is the ListCustomizations response.
type Customizations struct {
	Skills         []CustomizationSkill `json:"skills,omitempty"`
	ShadowedSkills []CustomizationSkill `json:"shadowedSkills,omitempty"`
}

// ListCustomizations lists the remote environment's skills, rules and
// agents. The client only consumes the skills today.
func (c *Client) ListCustomizations(ctx context.Context, workspaceURI, identity string) (Customizations, error) {
	in := map[string]string{}
	if workspaceURI != "" {
		in["workspaceUri"] = workspaceURI
	}
	if identity != "" {
		in["executionIdentity"] = identity
	}
	var out Customizations
	if err := c.post(ctx, "listCustomizations", in, &out); err != nil {
		return Customizations{}, err
	}
	return out, nil
}

// ProjectResource is one google.gca.aicode.v1main.ProjectResource.
type ProjectResource struct {
	Type        string `json:"type,omitempty"`
	ResourceID  string `json:"resourceId,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	URL         string `json:"url,omitempty"`
	IconURL     string `json:"iconUrl,omitempty"`
	ProjectID   string `json:"projectId,omitempty"`
	MIMEType    string `json:"mimeType,omitempty"`
	SizeBytes   int64  `json:"sizeBytes,omitempty"`
	MountPath   string `json:"mountPath,omitempty"`
}

// UploadProjectFile uploads bytes as a project resource. The reference's
// request carries the inline `content`; the newer inlineContent/blobHandle
// oneof is not used for the small payloads this client sends.
func (c *Client) UploadProjectFile(ctx context.Context, projectID, fileName string, content []byte, mimeType string) (ProjectResource, error) {
	in := map[string]any{
		"fileName":  fileName,
		"content":   content,
		"sizeBytes": len(content),
	}
	if projectID != "" {
		in["projectId"] = projectID
	}
	if mimeType != "" {
		in["mimeType"] = mimeType
	}
	var out struct {
		Resource ProjectResource `json:"resource"`
	}
	if err := c.post(ctx, "uploadProjectFile", in, &out); err != nil {
		return ProjectResource{}, err
	}
	return out.Resource, nil
}

// TriggerState is exa.remote_agent_proxy_pb.TriggerState.
type TriggerState int32

const (
	TriggerStateUnspecified TriggerState = 0
	TriggerStateActive      TriggerState = 1
	TriggerStatePaused      TriggerState = 2
	TriggerStateExpired     TriggerState = 3
	TriggerStateCompleted   TriggerState = 4
	TriggerStateError       TriggerState = 5
)

// UnmarshalJSON accepts the proto-JSON enum name and the numeric form.
func (s *TriggerState) UnmarshalJSON(data []byte) error {
	raw := strings.Trim(string(data), `"`)
	switch strings.ToUpper(raw) {
	case "", "TRIGGER_STATE_UNSPECIFIED":
		*s = TriggerStateUnspecified
	case "TRIGGER_STATE_ACTIVE":
		*s = TriggerStateActive
	case "TRIGGER_STATE_PAUSED":
		*s = TriggerStatePaused
	case "TRIGGER_STATE_EXPIRED":
		*s = TriggerStateExpired
	case "TRIGGER_STATE_COMPLETED":
		*s = TriggerStateCompleted
	case "TRIGGER_STATE_ERROR":
		*s = TriggerStateError
	default:
		n, err := strconv.Atoi(raw)
		if err != nil {
			return fmt.Errorf("trigger state: unknown %q", raw)
		}
		*s = TriggerState(n)
	}
	return nil
}

// MarshalJSON emits the proto-JSON enum name, not the numeric value.
func (s TriggerState) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// String renders the reference's enum name.
func (s TriggerState) String() string {
	switch s {
	case TriggerStateActive:
		return "TRIGGER_STATE_ACTIVE"
	case TriggerStatePaused:
		return "TRIGGER_STATE_PAUSED"
	case TriggerStateExpired:
		return "TRIGGER_STATE_EXPIRED"
	case TriggerStateCompleted:
		return "TRIGGER_STATE_COMPLETED"
	case TriggerStateError:
		return "TRIGGER_STATE_ERROR"
	default:
		return "TRIGGER_STATE_UNSPECIFIED"
	}
}

// FieldValues is the RemoteTrigger.fields map value.
type FieldValues struct {
	Values []string `json:"values,omitempty"`
}

// RemoteTrigger is exa.remote_agent_proxy_pb.RemoteTrigger.
type RemoteTrigger struct {
	TriggerID                string                 `json:"triggerId,omitempty"`
	State                    TriggerState           `json:"state,omitempty"`
	DisplayName              string                 `json:"displayName,omitempty"`
	Prompt                   string                 `json:"prompt,omitempty"`
	WorkspaceURIs            []string               `json:"workspaceUris,omitempty"`
	Environment              map[string]any         `json:"environment,omitempty"`
	ConfigOverrides          map[string]any         `json:"configOverrides,omitempty"`
	CreateTime               string                 `json:"createTime,omitempty"`
	UpdateTime               string                 `json:"updateTime,omitempty"`
	OptionID                 string                 `json:"optionId,omitempty"`
	Fields                   map[string]FieldValues `json:"fields,omitempty"`
	Summary                  string                 `json:"summary,omitempty"`
	CredentialExpirationTime string                 `json:"credentialExpirationTime,omitempty"`
	ProjectID                string                 `json:"projectId,omitempty"`
	InteractionID            string                 `json:"interactionId,omitempty"`
}

// TriggerOption is one option from ListTriggerOptions.
type TriggerOption struct {
	ID          string `json:"id,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	Description string `json:"description,omitempty"`
	Category    string `json:"category,omitempty"`
}

// ListTriggers lists the remote environment's triggers.
func (c *Client) ListTriggers(ctx context.Context, pageSize int, pageToken string) ([]RemoteTrigger, string, error) {
	in := map[string]any{}
	if pageSize > 0 {
		in["pageSize"] = pageSize
	}
	if pageToken != "" {
		in["pageToken"] = pageToken
	}
	var out struct {
		Triggers      []RemoteTrigger `json:"triggers"`
		NextPageToken string          `json:"nextPageToken"`
	}
	if err := c.post(ctx, "listTriggers", in, &out); err != nil {
		return nil, "", err
	}
	return out.Triggers, out.NextPageToken, nil
}

// ListTriggerOptions lists the trigger options the environment supports.
func (c *Client) ListTriggerOptions(ctx context.Context) ([]TriggerOption, error) {
	var out struct {
		TriggerOptions []TriggerOption `json:"triggerOptions"`
	}
	if err := c.post(ctx, "listTriggerOptions", map[string]any{}, &out); err != nil {
		return nil, err
	}
	return out.TriggerOptions, nil
}

// CreateTrigger creates a remote trigger.
func (c *Client) CreateTrigger(ctx context.Context, trigger RemoteTrigger) (RemoteTrigger, error) {
	var out struct {
		Trigger RemoteTrigger `json:"trigger"`
	}
	if err := c.post(ctx, "createTrigger", map[string]any{"trigger": trigger}, &out); err != nil {
		return RemoteTrigger{}, err
	}
	return out.Trigger, nil
}

// UpdateTrigger updates a remote trigger; updateMask names the changed
// fields (empty means the server's default).
func (c *Client) UpdateTrigger(ctx context.Context, trigger RemoteTrigger, updateMask []string) (RemoteTrigger, error) {
	in := map[string]any{"trigger": trigger}
	if len(updateMask) > 0 {
		in["updateMask"] = updateMask
	}
	var out struct {
		Trigger RemoteTrigger `json:"trigger"`
	}
	if err := c.post(ctx, "updateTrigger", in, &out); err != nil {
		return RemoteTrigger{}, err
	}
	return out.Trigger, nil
}

// DeleteTrigger deletes a remote trigger.
func (c *Client) DeleteTrigger(ctx context.Context, triggerID string, abortActiveRuns bool) error {
	return c.post(ctx, "deleteTrigger", map[string]any{
		"triggerId":       triggerID,
		"abortActiveRuns": abortActiveRuns,
	}, nil)
}
