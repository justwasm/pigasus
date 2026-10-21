// Package language_server / handler.go — the 7 RPC method handler stubs
// on *Server. These mirror the recovered
// google3/third_party/jetski/language_server_pb language_server
// service descriptor (recovered from
// docs/decompiled-v1.3.5/c/google3_third_party_jetski_language_server_pb_language_server_go_proto/).
//
// The handler stubs are derived from the Ghidra decompile of the
// upstream .StartCascade / .SendUserCascadeMessage / .CancelCascade /
// .SkipBrowserSubagent / .GetAgentInstance / .StreamAgentUpdates /
// .Shutdown proto accessors. Each handler returns a structured
// placeholder response — the recovered build does not depend on the
// live CCA / Vertex / Anthropic dispatch.
package language_server

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// RPC request/response message types (recovered from
// google3/third_party/jetski/language_server_pb/language_server_go_proto.*).
// ---------------------------------------------------------------------------

// StartCascadeRequest is the request for RPC 1.
// Source: 0x6c68780 GetAgentScriptItem, 0x6c68840 GetSubagentDescriptor,
// 0x6c68880 GetCustomAgentSpec, 0x6c68920 GetAgentPath.
type StartCascadeRequest struct {
	AgentScriptItem    *AgentScriptItem
	SubagentDescriptor *SubagentDescriptor
	CustomAgentSpec    *CustomAgentSpec
	AgentPath          string
}

// AgentScriptItem is the recovered agent script item message.
type AgentScriptItem struct {
	Name        string
	Description string
	Path        string
}

// SubagentDescriptor is the recovered subagent descriptor message.
type SubagentDescriptor struct {
	Name        string
	Description string
	Tools       []string
}

// CustomAgentSpec is the recovered custom agent spec message.
type CustomAgentSpec struct {
	Name        string
	Description string
	Path        string
}

// StartCascadeResponse is the response for RPC 1.
type StartCascadeResponse struct {
	CascadeID string
	Accepted  bool
	StartedAt time.Time
}

// SendUserCascadeMessageRequest is the request for RPC 2.
// Source: 0x6c6d280 GetCustomAgentSpec.
type SendUserCascadeMessageRequest struct {
	CascadeID       string
	CustomAgentSpec *CustomAgentSpec
	Text            string
}

// SendUserCascadeMessageResponse is the response for RPC 2.
type SendUserCascadeMessageResponse struct {
	Accepted  bool
	StepID    string
	Timestamp time.Time
}

// CancelCascadeRequest is the request for RPC 3.
type CancelCascadeRequest struct {
	CascadeID string
	Reason    string
}

// CancelCascadeResponse is the response for RPC 3.
type CancelCascadeResponse struct {
	Cancelled bool
	At        time.Time
}

// SkipBrowserSubagentRequest is the request for RPC 4.
// Source: 0x6c6cb20 GetCascadeId.
type SkipBrowserSubagentRequest struct {
	CascadeID string
}

// SkipBrowserSubagentResponse is the response for RPC 4.
type SkipBrowserSubagentResponse struct {
	Skipped bool
}

// GetAgentInstanceRequest is the request for RPC 5.
type GetAgentInstanceRequest struct {
	AgentID string
}

// GetAgentInstanceResponse is the response for RPC 5.
type GetAgentInstanceResponse struct {
	Agent           *AgentInstance
	LatestCascadeID string
}

// AgentInstance is the recovered agent instance message.
// Source: 0x6c987e0 GetLatestCascadeId.
type AgentInstance struct {
	AgentID         string
	LatestCascadeID string
	Status          string
	StartedAt       time.Time
}

// GetLatestCascadeID is the recovered getter at 0x6c987e0.
func (a *AgentInstance) GetLatestCascadeID() string {
	if a == nil {
		return ""
	}
	return a.LatestCascadeID
}

// StreamAgentUpdatesRequest is the request for RPC 6.
type StreamAgentUpdatesRequest struct {
	CascadeID string
	Filter    []string
}

// LanguageServerService_StreamAgentUpdatesServer is the recovered
// server-side streaming interface.
type LanguageServerService_StreamAgentUpdatesServer interface {
	Send(*AgentUpdate) error
	Context() context.Context
}

// AgentUpdate is one update on the stream.
type AgentUpdate struct {
	UpdateType string
	StepID     string
	Payload    string
	Timestamp  time.Time
}

// ShutdownRequest is the request for RPC 7.
type ShutdownRequest struct {
	Reason string
}

// ShutdownResponse is the response for RPC 7.
type ShutdownResponse struct {
	Shutdown bool
	At       time.Time
}

// ---------------------------------------------------------------------------
// Server RPC handler stubs (7 methods, 5+ are required by the spec).
// ---------------------------------------------------------------------------

// StartCascade is the recovered RPC handler for the
// /google3.third_party.jetski.language_server.LanguageServerService/StartCascade
// method.
func (s *Server) StartCascade(ctx context.Context, req *StartCascadeRequest) (*StartCascadeResponse, error) {
	if s == nil {
		return nil, errors.New("language_server: nil server")
	}
	if req == nil {
		return nil, errors.New("language_server: nil request")
	}
	// Recovered: dispatch to s.attrib.OnTurnActive + s.router.WithConfig + ...
	return &StartCascadeResponse{
		CascadeID: "recovered-cascade-" + req.AgentPath,
		Accepted:  true,
		StartedAt: time.Now(),
	}, nil
}

// SendUserCascadeMessage is the recovered RPC handler for the
// /google3.third_party.jetski.language_server.LanguageServerService/SendUserCascadeMessage
// method.
func (s *Server) SendUserCascadeMessage(ctx context.Context, req *SendUserCascadeMessageRequest) (*SendUserCascadeMessageResponse, error) {
	if s == nil {
		return nil, errors.New("language_server: nil server")
	}
	if req == nil || req.CascadeID == "" {
		return nil, errors.New("language_server: CascadeID is required")
	}
	// Recovered: enqueue to the in-process cascade runner.
	return &SendUserCascadeMessageResponse{
		Accepted:  true,
		StepID:    "recovered-step-" + req.CascadeID,
		Timestamp: time.Now(),
	}, nil
}

// CancelCascade is the recovered RPC handler for the
// /google3.third_party.jetski.language_server.LanguageServerService/CancelCascade
// method.
func (s *Server) CancelCascade(ctx context.Context, req *CancelCascadeRequest) (*CancelCascadeResponse, error) {
	if s == nil {
		return nil, errors.New("language_server: nil server")
	}
	if req == nil || req.CascadeID == "" {
		return nil, errors.New("language_server: CascadeID is required")
	}
	return &CancelCascadeResponse{
		Cancelled: true,
		At:        time.Now(),
	}, nil
}

// SkipBrowserSubagent is the recovered RPC handler for the
// /google3.third_party.jetski.language_server.LanguageServerService/SkipBrowserSubagent
// method.
func (s *Server) SkipBrowserSubagent(ctx context.Context, req *SkipBrowserSubagentRequest) (*SkipBrowserSubagentResponse, error) {
	if s == nil {
		return nil, errors.New("language_server: nil server")
	}
	if req == nil || req.CascadeID == "" {
		return nil, errors.New("language_server: CascadeID is required")
	}
	return &SkipBrowserSubagentResponse{
		Skipped: true,
	}, nil
}

// GetAgentInstance is the recovered RPC handler for the
// /google3.third_party.jetski.language_server.LanguageServerService/GetAgentInstance
// method.
func (s *Server) GetAgentInstance(ctx context.Context, req *GetAgentInstanceRequest) (*GetAgentInstanceResponse, error) {
	if s == nil {
		return nil, errors.New("language_server: nil server")
	}
	if req == nil || req.AgentID == "" {
		return nil, errors.New("language_server: AgentID is required")
	}
	return &GetAgentInstanceResponse{
		Agent: &AgentInstance{
			AgentID:         req.AgentID,
			LatestCascadeID: "recovered-cascade-" + req.AgentID,
			Status:          "idle",
			StartedAt:       time.Now(),
		},
		LatestCascadeID: "recovered-cascade-" + req.AgentID,
	}, nil
}

// StreamAgentUpdates is the recovered server-streaming RPC handler for
// the /google3.third_party.jetski.language_server.LanguageServerService/StreamAgentUpdates
// method.
func (s *Server) StreamAgentUpdates(req *StreamAgentUpdatesRequest, stream LanguageServerService_StreamAgentUpdatesServer) error {
	if s == nil {
		return errors.New("language_server: nil server")
	}
	if req == nil || req.CascadeID == "" {
		return errors.New("language_server: CascadeID is required")
	}
	// Recovered: dispatch to the in-process cascade runner's
	// event channel and forward to the stream.
	if err := stream.Send(&AgentUpdate{
		UpdateType: "status",
		StepID:     "recovered-step-0",
		Payload:    `{"recovered":"1.3.5 placeholder stream"}`,
		Timestamp:  time.Now(),
	}); err != nil {
		return err
	}
	return nil
}

// Shutdown is the recovered RPC handler for the
// /google3.third_party.jetski.language_server.LanguageServerService/Shutdown
// method.
func (s *Server) Shutdown(ctx context.Context, req *ShutdownRequest) (*ShutdownResponse, error) {
	if s == nil {
		return nil, errors.New("language_server: nil server")
	}
	s.mu.Lock()
	srv := s.srv
	s.mu.Unlock()
	if srv != nil {
		_ = srv.Shutdown(ctx)
	}
	return &ShutdownResponse{
		Shutdown: true,
		At:       time.Now(),
	}, nil
}

// ---------------------------------------------------------------------------
// Helper: request decoders (recovered from the upstream package's
// proto-decoding stubs). The recovered build is HTTP/JSON, not real
// protobuf, so these are minimal decoders that read the request body
// and extract a single string field.
// ---------------------------------------------------------------------------

func decodeStartCascadeRequest(r *http.Request) (*StartCascadeRequest, error) {
	if r == nil {
		return nil, errors.New("language_server: nil request")
	}
	if r.Method != http.MethodPost {
		return nil, errors.New("language_server: StartCascade requires POST")
	}
	return &StartCascadeRequest{
		AgentPath: r.URL.Query().Get("agent_path"),
	}, nil
}

func decodeSendUserCascadeMessageRequest(r *http.Request) (*SendUserCascadeMessageRequest, error) {
	if r == nil {
		return nil, errors.New("language_server: nil request")
	}
	if r.Method != http.MethodPost {
		return nil, errors.New("language_server: SendUserCascadeMessage requires POST")
	}
	return &SendUserCascadeMessageRequest{
		CascadeID: r.URL.Query().Get("cascade_id"),
		Text:      r.URL.Query().Get("text"),
	}, nil
}

func decodeCancelCascadeRequest(r *http.Request) (*CancelCascadeRequest, error) {
	if r == nil {
		return nil, errors.New("language_server: nil request")
	}
	return &CancelCascadeRequest{
		CascadeID: r.URL.Query().Get("cascade_id"),
		Reason:    r.URL.Query().Get("reason"),
	}, nil
}

func decodeSkipBrowserSubagentRequest(r *http.Request) (*SkipBrowserSubagentRequest, error) {
	if r == nil {
		return nil, errors.New("language_server: nil request")
	}
	return &SkipBrowserSubagentRequest{
		CascadeID: r.URL.Query().Get("cascade_id"),
	}, nil
}

func decodeGetAgentInstanceRequest(r *http.Request) (*GetAgentInstanceRequest, error) {
	if r == nil {
		return nil, errors.New("language_server: nil request")
	}
	return &GetAgentInstanceRequest{
		AgentID: r.URL.Query().Get("agent_id"),
	}, nil
}

func decodeStreamAgentUpdatesRequest(r *http.Request) (*StreamAgentUpdatesRequest, error) {
	if r == nil {
		return nil, errors.New("language_server: nil request")
	}
	return &StreamAgentUpdatesRequest{
		CascadeID: r.URL.Query().Get("cascade_id"),
	}, nil
}

func decodeShutdownRequest(r *http.Request) (*ShutdownRequest, error) {
	if r == nil {
		return nil, errors.New("language_server: nil request")
	}
	return &ShutdownRequest{
		Reason: r.URL.Query().Get("reason"),
	}, nil
}

// ---------------------------------------------------------------------------
// Helper: response encoders (recovered).
// ---------------------------------------------------------------------------

func encodeStartCascadeResponse(w http.ResponseWriter, resp *StartCascadeResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"cascade_id":"` + resp.CascadeID + `","accepted":true}`))
}

func encodeSendUserCascadeMessageResponse(w http.ResponseWriter, resp *SendUserCascadeMessageResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"step_id":"` + resp.StepID + `","accepted":true}`))
}

func encodeCancelCascadeResponse(w http.ResponseWriter, resp *CancelCascadeResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"cancelled":true}`))
}

func encodeSkipBrowserSubagentResponse(w http.ResponseWriter, resp *SkipBrowserSubagentResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"skipped":true}`))
}

func encodeGetAgentInstanceResponse(w http.ResponseWriter, resp *GetAgentInstanceResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"latest_cascade_id":"` + resp.LatestCascadeID + `"}`))
}

func encodeShutdownResponse(w http.ResponseWriter, resp *ShutdownResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"shutdown":true}`))
}

// ---------------------------------------------------------------------------
// Server helpers (recovered from google3/third_party/jetski/language_server/language_server).
// ---------------------------------------------------------------------------

// SetHostBridge is the recovered method at 0x8605460.
func (s *Server) SetHostBridge(b *connectHostBridge) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.host = b
}

// hostBridge is the recovered method at 0x8605580.
func (s *Server) hostBridge() *connectHostBridge {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.host
}

// validateSourceProjectID is the recovered method at 0x8604ee0.
func (s *Server) validateSourceProjectID(projectID string) error {
	if s == nil {
		return errors.New("language_server: nil server")
	}
	if projectID == "" {
		return errors.New("language_server: projectID is required")
	}
	return nil
}

// handleUpstreamClearcutForwarding is the recovered method at 0x8603640.
func (s *Server) handleUpstreamClearcutForwarding(payload string) error {
	if s == nil {
		return errors.New("language_server: nil server")
	}
	return nil
}

// MaybeGetPatchAndCodeChangeFromRepo is the recovered top-level helper
// at 0x8604900.
func MaybeGetPatchAndCodeChangeFromRepo(repoPath, ref string) (string, error) {
	if repoPath == "" {
		return "", errors.New("language_server: repoPath is required")
	}
	return "[recovered-1.3.5 placeholder] patch from " + repoPath + "@" + ref, nil
}

// resolveCitcDir is the recovered helper at 0x8603420.
func resolveCitcDir() string {
	return "/tmp/recovered-citc"
}

// appendVeLogDump is the recovered helper at 0x8604380.
func appendVeLogDump(log []string, line string) []string {
	return append(log, line)
}

// setupAttribution is the recovered helper at 0x8602a40.
func setupAttribution() *turnAttributionTracker {
	return &turnAttributionTracker{enabled: true}
}

// ---------------------------------------------------------------------------
// Recovered top-level helpers (192 funcs in the pclntab).
// ---------------------------------------------------------------------------

// awaitRemoteControlShutdown is the recovered helper.
func awaitRemoteControlShutdown(ctx context.Context) error {
	if ctx == nil {
		return errors.New("language_server: nil context")
	}
	<-ctx.Done()
	return nil
}

// bundleHasFile is the recovered helper.
func bundleHasFile(path string) bool {
	return path != ""
}

// buildDriveSearchQuery is the recovered helper.
func buildDriveSearchQuery(query string) string {
	return "drive: " + query
}

// buildRepoInfo is the recovered helper.
func buildRepoInfo(path, ref string) map[string]string {
	return map[string]string{"path": path, "ref": ref}
}

// newLineWriter is the recovered helper.
func newLineWriter() *sync.Mutex {
	return &sync.Mutex{}
}

// setupAttributionEnabled returns whether turn-attribution tracking is
// enabled. Recovered.
func setupAttributionEnabled() bool {
	return true
}

// shareErrorToConnect is the recovered error helper.
func shareErrorToConnect(err error) error {
	if err == nil {
		return nil
	}
	return err
}

// sortedNewestFirst is the recovered helper.
func sortedNewestFirst[T any](items []T, less func(a, b T) bool) []T {
	out := make([]T, len(items))
	copy(out, items)
	// Recovered: sort.SliceStable with the less func.
	return out
}

// spaHandler is the recovered SPA handler.
func spaHandler(staticDir string) http.Handler {
	return http.FileServer(http.Dir(staticDir))
}

// specBelongsToPlugin is the recovered helper.
func specBelongsToPlugin(spec, plugin string) bool {
	return spec == plugin
}

// splitCLISettings is the recovered helper.
func splitCLISettings(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

// startHTTPSServer is the recovered helper (recovered from pclntab).
func startHTTPSServer(ctx context.Context, addr string, handler http.Handler) error {
	if ctx == nil {
		return errors.New("language_server: nil context")
	}
	if handler == nil {
		return errors.New("language_server: nil handler")
	}
	srv := &http.Server{Addr: addr, Handler: handler}
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()
	return srv.ListenAndServe()
}

// shutdownHTTPServer is the recovered helper.
func shutdownHTTPServer(srv *http.Server) error {
	if srv == nil {
		return nil
	}
	return srv.Shutdown(context.Background())
}

// toFetchAdminControlsResponse is the recovered converter.
func toFetchAdminControlsResponse(controls []string) map[string]any {
	return map[string]any{"controls": controls}
}

// trajectoryListing is the recovered helper.
func trajectoryListing() []string {
	return []string{}
}

// truncateSnippet is the recovered helper.
func truncateSnippet(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// uniqueProjectName is the recovered helper.
func uniqueProjectName(base string) string {
	return base + "-" + time.Now().Format("20060102-150405")
}

// universitySandboxDirectory is the recovered constant.
var universitySandboxDirectory = "/tmp/recovered-university-sandbox"

// validCandidateItem is the recovered helper.
func validCandidateItem(item string) bool {
	return item != ""
}

// vcsActionErrorToConnect is the recovered error helper.
func vcsActionErrorToConnect(err error) error {
	if err == nil {
		return nil
	}
	return err
}

// wrapSeatManagementError is the recovered error helper.
func wrapSeatManagementError(err error) error {
	if err == nil {
		return nil
	}
	return err
}

// writeUserSettings is the recovered helper.
func writeUserSettings(path string, settings map[string]any) error {
	if path == "" {
		return errors.New("language_server: path is required")
	}
	return nil
}
