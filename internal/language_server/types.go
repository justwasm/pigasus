// Package language_server is a recovered (re-implemented) binding for
// google3/third_party/jetski/language_server/language_server — the
// v1.3.5 antigravity binary's umbrella language-server gRPC server
// implementation. This package hosts the gRPC server entry points
// (RegisterServer / Serve / Stream) and the ~5-10 RPC method handler
// stubs that the antigravity TUI / IDE / CLI dispatch to.
//
// Source-of-truth artifacts:
//   - docs/decompiled-v1.3.5/c/google3_third_party_jetski_language_server_code_assist_client_codeassistclient/  (4 .c files)
//   - docs/decompiled-v1.3.5/c/google3_third_party_jetski_language_server_pb_language_server_go_proto/      (7 .c files)
//   - docs/decompiled-v1.3.5/c/google3_third_party_jetski_language_server_jetbox_summaries_store_jetbox_summaries_store/ (1 .c file)
//   - docs/analysis-v1.3.5-official/funcs-v1.3.5-pclntab.txt
//     (192 top-level funcs + 636 (*Server).* method receivers in the
//     language_server package alone, plus 320+ in codeassistclient).
//
// The recovered build:
//
//   - Defines the Server type (mirrors 636 (*Server).* methods; the
//     skeleton exposes a representative subset as stubs).
//   - Defines the LanguageServerConfig and ModelAPIClientType types
//     (recovered from the .String / .Get / .Set accessors in the
//     pclntab).
//   - Defines RoutingClient, AudioSessionManager, NarrationSessionManager,
//     and the turnAttributionTracker types — all recovered from the
//     (*<Type>).* methods.
//   - Defines CreateLanguageServerAndServe, NewRoutingClient,
//     NewConnectHostBridge as constructor stubs (these exist in the
//     v1.3.5 binary at 0x8601700+, see the decompile for details).
//   - Provides RegisterServer / Serve / Stream entry points in
//     server.go that integrate with internal/grpc/.
//
// The package is intended to be 1:1 with the v1.3.5 binary at the
// type/function-name level. The actual server semantics are stubs:
// every method panics with "TODO: language_server.<method>" or
// returns a structured placeholder, so the v4 antigravity CLI
// surface (the `antigravity --services` list, plus the codeassistclient
// dispatch) can compile and run end-to-end without the live Cloud AI
// Companion backend.
package language_server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Service constants (recovered from
// google3/third_party/jetski/language_server_pb/language_server_go_grpc).
// ---------------------------------------------------------------------------

// ServiceName is the gRPC service name constant.
const ServiceName = "google3.third_party.jetski.language_server.LanguageServerService"

// LanguageServerMethodNames lists the 7 RPCs exposed by the
// language_server LanguageServerService, in declaration order.
var LanguageServerMethodNames = []string{
	"StartCascade",
	"SendUserCascadeMessage",
	"CancelCascade",
	"SkipBrowserSubagent",
	"GetAgentInstance",
	"StreamAgentUpdates",
	"Shutdown",
}

// ErrLSNotImplemented is returned by placeholder RPCs in the recovered
// build.
var ErrLSNotImplemented = errors.New("language_server: RPC not implemented in recovered build")

// ---------------------------------------------------------------------------
// Recovered types from google3/third_party/jetski/language_server/language_server
// ---------------------------------------------------------------------------

// ModelAPIClientType is the recovered enum-like string type that
// names the model API client (CCA / Vertex / Anthropic / OpenAI / ...).
// Recovered from .String / .Get / .Set accessors in pclntab.
type ModelAPIClientType string

const (
	ModelAPIClientUnspecified ModelAPIClientType = ""
	ModelAPIClientCCA         ModelAPIClientType = "cca"
	ModelAPIClientVertex      ModelAPIClientType = "vertex"
	ModelAPIClientAnthropic   ModelAPIClientType = "anthropic"
	ModelAPIClientOpenAI      ModelAPIClientType = "openai"
	ModelAPIClientAntigravity ModelAPIClientType = "antigravity"
)

// String returns the string representation of the ModelAPIClientType.
func (m ModelAPIClientType) String() string {
	if m == "" {
		return "ModelAPIClientUnspecified"
	}
	return string(m)
}

// Get returns the value (no-op accessor for the flag-binding style).
func (m *ModelAPIClientType) Get() ModelAPIClientType {
	if m == nil {
		return ModelAPIClientUnspecified
	}
	return *m
}

// Set sets the value (no-op accessor for the flag-binding style).
func (m *ModelAPIClientType) Set(v ModelAPIClientType) {
	if m != nil {
		*m = v
	}
}

// LanguageServerConfig is the recovered language-server config struct.
// Source: 0x8601700 (init), accessors in pclntab: .IsSessionScopedRemoteControl,
// .IsTerminalCLI.
type LanguageServerConfig struct {
	IsSessionScopedRemoteControl_ bool
	IsTerminalCLI_                bool
	Port                          int
	Host                          string
	UseTLS                        bool
	TLSCertFile                   string
	TLSKeyFile                    string
	MaxConcurrentRPCs             int
	RPCTimeout                    time.Duration
	EnableTelemetry               bool
	EnableTracing                 bool
}

// IsSessionScopedRemoteControl returns the recovered IsSessionScopedRemoteControl bit.
func (c *LanguageServerConfig) IsSessionScopedRemoteControl() bool {
	if c == nil {
		return false
	}
	return c.IsSessionScopedRemoteControl_
}

// IsTerminalCLI returns the recovered IsTerminalCLI bit.
func (c *LanguageServerConfig) IsTerminalCLI() bool {
	if c == nil {
		return false
	}
	return c.IsTerminalCLI_
}

// Server is the recovered language-server gRPC server. Source:
// google3/third_party/jetski/language_server/language_server.(*Server).*
// at 0x8603640+ (handleUpstreamClearcutForwarding, validateSourceProjectID,
// SetHostBridge, hostBridge, ...).
//
// The full v1.3.5 server has 636 methods on *Server. The skeleton
// exposes a representative subset: a handful of state-bearing fields
// plus a small set of method stubs that the antigravity TUI / IDE /
// CLI dispatch to.
type Server struct {
	mu        sync.Mutex
	config    *LanguageServerConfig
	router    *RoutingClient
	host      *connectHostBridge
	attrib    *turnAttributionTracker
	listener  net.Listener
	srv       *http.Server
	telemetry *BAICTelemetryClient
	audioMgr  *AudioSessionManager
	narration *NarrationSessionManager
}

// turnAttributionTracker is the recovered turn-attribution tracker.
// Source: 0x8601f00 .OnTurnActive, 0x8602260 .OnTurnIdle, 0x8602700 .Close,
// 0x8602a40 setupAttribution.
type turnAttributionTracker struct {
	enabled bool
}

// OnTurnActive is the recovered method at 0x8601f00.
func (t *turnAttributionTracker) OnTurnActive(turnID string) {
	if t == nil {
		return
	}
	t.enabled = true
}

// OnTurnIdle is the recovered method at 0x8602260.
func (t *turnAttributionTracker) OnTurnIdle(turnID string) {
	if t == nil {
		return
	}
	t.enabled = false
}

// Close is the recovered method at 0x8602700.
func (t *turnAttributionTracker) Close() error {
	if t == nil {
		return nil
	}
	t.enabled = false
	return nil
}

// RoutingClient is the recovered model-routing client. Source: 0x8605900+.
// Methods: ClassifyError, Generate, GetCascadeModelConfigData, GetChatMessage,
// GetCommitMessageModelEnum, GetDeprecatedModelReroute, GetImageGeneration,
// GetModelInfos, GetVideoSummarization, GetWebSearchResults, WithConfig,
// getClient, getInternalClient, resolveClient, resolveModelName.
type RoutingClient struct {
	mu       sync.Mutex
	endpoint string
	tier     string
}

// NewRoutingClient is the recovered constructor (pclntab: NewRoutingClient).
func NewRoutingClient(endpoint string) *RoutingClient {
	return &RoutingClient{endpoint: endpoint}
}

// ClassifyError is the recovered method.
func (r *RoutingClient) ClassifyError(err error) string {
	if err == nil {
		return "none"
	}
	return "transient"
}

// Generate is the recovered method.
func (r *RoutingClient) Generate(ctx context.Context, prompt string) (string, error) {
	if r == nil || r.endpoint == "" {
		return "", errors.New("language_server: routing client not initialized")
	}
	// Placeholder — the real binary dispatches to CCA / Vertex / Anthropic.
	return "[recovered-1.3.5 placeholder] " + prompt, nil
}

// GetCascadeModelConfigData is the recovered method.
func (r *RoutingClient) GetCascadeModelConfigData() string {
	if r == nil {
		return ""
	}
	return r.tier
}

// GetChatMessage is the recovered method.
func (r *RoutingClient) GetChatMessage(ctx context.Context, prompt string) (string, error) {
	return r.Generate(ctx, prompt)
}

// GetCommitMessageModelEnum is the recovered method.
func (r *RoutingClient) GetCommitMessageModelEnum() string {
	return "claude-sonnet-4"
}

// GetDeprecatedModelReroute is the recovered method.
func (r *RoutingClient) GetDeprecatedModelReroute(model string) string {
	return model
}

// GetImageGeneration is the recovered method.
func (r *RoutingClient) GetImageGeneration(ctx context.Context, prompt string) ([]byte, error) {
	return nil, errors.New("language_server: image generation not implemented in recovered build")
}

// GetModelInfos is the recovered method.
func (r *RoutingClient) GetModelInfos() []string {
	return []string{"claude-sonnet-4", "gemini-2.5-pro", "gpt-4o"}
}

// GetVideoSummarization is the recovered method.
func (r *RoutingClient) GetVideoSummarization(ctx context.Context, url string) (string, error) {
	return "[recovered-placeholder] video summary for " + url, nil
}

// GetWebSearchResults is the recovered method.
func (r *RoutingClient) GetWebSearchResults(ctx context.Context, query string) ([]string, error) {
	return []string{"[recovered-placeholder] result 1 for " + query}, nil
}

// WithConfig is the recovered method.
func (r *RoutingClient) WithConfig(tier string) *RoutingClient {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tier = tier
	return r
}

// getClient is the recovered internal accessor.
func (r *RoutingClient) getClient() *RoutingClient { return r }

// getInternalClient is the recovered internal accessor.
func (r *RoutingClient) getInternalClient() *RoutingClient { return r }

// resolveClient is the recovered internal accessor.
func (r *RoutingClient) resolveClient() *RoutingClient { return r }

// resolveModelName is the recovered internal accessor.
func (r *RoutingClient) resolveModelName(model string) string {
	if model == "" {
		return "claude-sonnet-4"
	}
	return model
}

// connectHostBridge is the recovered Connect-based host bridge.
// Source: 0x8605460 SetHostBridge, 0x8605580 hostBridge, 0x8605660 NewConnectHostBridge,
// 0x8605900 .UpdateStatus.
type connectHostBridge struct {
	endpoint string
	mu       sync.Mutex
}

// NewConnectHostBridge is the recovered constructor at 0x8605660.
func NewConnectHostBridge(endpoint string) *connectHostBridge {
	return &connectHostBridge{endpoint: endpoint}
}

// UpdateStatus is the recovered method at 0x8605900.
func (b *connectHostBridge) UpdateStatus(ctx context.Context, status string) error {
	if b == nil {
		return errors.New("language_server: host bridge is nil")
	}
	b.mu.Lock()
	b.endpoint = b.endpoint + ""
	b.mu.Unlock()
	return nil
}

// ---------------------------------------------------------------------------
// Audio session manager (recovered from
// google3/third_party/jetski/language_server/audiotranscribe/audiotranscribe.*)
// ---------------------------------------------------------------------------

// AudioSessionManager is the recovered audio session manager.
type AudioSessionManager struct {
	mu       sync.Mutex
	sessions map[string]*AudioSession
}

// AudioSession is the recovered audio session.
type AudioSession struct {
	ID        string
	StartedAt time.Time
}

// NewAudioSessionManager is the recovered constructor.
func NewAudioSessionManager() *AudioSessionManager {
	return &AudioSessionManager{sessions: make(map[string]*AudioSession)}
}

// Add is the recovered method.
func (m *AudioSessionManager) Add(id string) *AudioSession {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s := &AudioSession{ID: id, StartedAt: time.Now()}
	m.sessions[id] = s
	return s
}

// Get is the recovered method.
func (m *AudioSessionManager) Get(id string) *AudioSession {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}

// Remove is the recovered method.
func (m *AudioSessionManager) Remove(id string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
}

// GetVoiceContextCache is the recovered method.
func (m *AudioSessionManager) GetVoiceContextCache(id string) []byte {
	if s := m.Get(id); s != nil {
		return nil
	}
	return nil
}

// SetVoiceContextCache is the recovered method.
func (m *AudioSessionManager) SetVoiceContextCache(id string, cache []byte) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok {
		_ = s
		_ = cache
	}
}

// processBuffer is the recovered method on AudioSession.
func (s *AudioSession) processBuffer(buf []byte) ([]byte, error) {
	if s == nil {
		return nil, errors.New("language_server: audio session is nil")
	}
	return buf, nil
}

// ---------------------------------------------------------------------------
// Narration session manager (recovered).
// ---------------------------------------------------------------------------

// NarrationSessionManager is the recovered narration session manager.
type NarrationSessionManager struct {
	mu       sync.Mutex
	sessions map[string]*NarrationSession
}

// NarrationSession is the recovered narration session.
type NarrationSession struct {
	ID        string
	StartedAt time.Time
}

// NewNarrationSessionManager is the recovered constructor.
func NewNarrationSessionManager() *NarrationSessionManager {
	return &NarrationSessionManager{sessions: make(map[string]*NarrationSession)}
}

// Add is the recovered method.
func (m *NarrationSessionManager) Add(id string) *NarrationSession {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s := &NarrationSession{ID: id, StartedAt: time.Now()}
	m.sessions[id] = s
	return s
}

// Get is the recovered method.
func (m *NarrationSessionManager) Get(id string) *NarrationSession {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}

// Remove is the recovered method.
func (m *NarrationSessionManager) Remove(id string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
}

// ---------------------------------------------------------------------------
// BAIC telemetry client (recovered).
// ---------------------------------------------------------------------------

// BAICTelemetryClient is the recovered telemetry client.
type BAICTelemetryClient struct {
	mu       sync.Mutex
	endpoint string
}

// NewBAICTelemetryClient is the recovered constructor.
func NewBAICTelemetryClient(endpoint string) *BAICTelemetryClient {
	return &BAICTelemetryClient{endpoint: endpoint}
}

// ProvideFeedback is the recovered method.
func (c *BAICTelemetryClient) ProvideFeedback(feedback string) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordAnalyticsEvent is the recovered method.
func (c *BAICTelemetryClient) RecordAnalyticsEvent(name string, props map[string]any) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordAnalyticsEventString is the recovered method.
func (c *BAICTelemetryClient) RecordAnalyticsEventString(name, value string) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordBattleModeMetadata is the recovered method.
func (c *BAICTelemetryClient) RecordBattleModeMetadata(meta string) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordCommandUsage is the recovered method.
func (c *BAICTelemetryClient) RecordCommandUsage(cmd string) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordCompletions is the recovered method.
func (c *BAICTelemetryClient) RecordCompletions(model string, count int) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordCortexExecutionMetadata is the recovered method.
func (c *BAICTelemetryClient) RecordCortexExecutionMetadata(meta string) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordCortexTrajectory is the recovered method.
func (c *BAICTelemetryClient) RecordCortexTrajectory(trajID string) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordError is the recovered method.
func (c *BAICTelemetryClient) RecordError(err error) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordFullTrajectoryAnalytics is the recovered method.
func (c *BAICTelemetryClient) RecordFullTrajectoryAnalytics(trajID string) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordHookExecution is the recovered method.
func (c *BAICTelemetryClient) RecordHookExecution(hook string) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordInputCompletion is the recovered method.
func (c *BAICTelemetryClient) RecordInputCompletion(prompt string) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordInputCompletionFeedback is the recovered method.
func (c *BAICTelemetryClient) RecordInputCompletionFeedback(feedback string) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordMonitorSignal is the recovered method.
func (c *BAICTelemetryClient) RecordMonitorSignal(signal string) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordObservabilityData is the recovered method.
func (c *BAICTelemetryClient) RecordObservabilityData(data string) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordRPCMetadata is the recovered method.
func (c *BAICTelemetryClient) RecordRPCMetadata(rpc, status string) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordReadUrlContent is the recovered method.
func (c *BAICTelemetryClient) RecordReadUrlContent(url string) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordRemoteControlEvent is the recovered method.
func (c *BAICTelemetryClient) RecordRemoteControlEvent(event string) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordStateInitializationData is the recovered method.
func (c *BAICTelemetryClient) RecordStateInitializationData(state string) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordTrajectorySegmentAnalytics is the recovered method.
func (c *BAICTelemetryClient) RecordTrajectorySegmentAnalytics(segID string) error {
	if c == nil {
		return nil
	}
	return nil
}

// RecordTrajectorySegmentEvents is the recovered method.
func (c *BAICTelemetryClient) RecordTrajectorySegmentEvents(segID, evt string) error {
	if c == nil {
		return nil
	}
	return nil
}

// Shutdown is the recovered method.
func (c *BAICTelemetryClient) Shutdown() error {
	if c == nil {
		return nil
	}
	return nil
}
