// Package cortex_pb / cortex.go — service interface and gRPC client stub.
//
// Source-of-truth artifacts:
//   - google3/third_party/jetski/cortex_pb/cortex_go_proto (proto types)
//   - google3/third_party/jetski/cortex_pb/options_go_proto (Cortex options)
//   - google3/third_party/jetski/cortex_pb/custom_extensions_go_proto
//   - google3/third_party/jetski/cortex_pb/model_native_tool_type_go_proto
//   - google3/third_party/jetski/jetski_cortex_pb/jetski_cortex_go_proto
//
// This file defines:
//
//   - The 5-RPC CortexService service interface (CortexServiceServer + CortexServiceClient)
//   - The 3 request/response message types (GetCascadeConfigRequest/Response,
//     SendCascadeMessageRequest/Response, OnboardingStateSyncRequest/Response,
//     FetchToolCatalogRequest/Response)
//   - A placeholder gRPC client stub (cortexClient) that mirrors the wire
//     format but does not actually dial the live endpoint.
//
// The recovered build integrates with grpc/ (which has the
// exa.api_server_pb.ApiServerService entry) by exposing itself as
// another Service descriptor (CortexService{}) for the unified
// antigravity --services CLI surface.
package cortex_pb

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// gRPC service interface
// ---------------------------------------------------------------------------

// CortexServiceClient is the recovered gRPC client interface for
// CortexService. The 5 methods correspond 1:1 to the 5 RPCs in the proto.
type CortexServiceClient interface {
	GetCascadeConfig(ctx context.Context, req *GetCascadeConfigRequest) (*GetCascadeConfigResponse, error)
	SendCascadeMessage(ctx context.Context, req *SendCascadeMessageRequest) (*SendCascadeMessageResponse, error)
	StreamCascadeUpdates(ctx context.Context, req *StreamCascadeUpdatesRequest) (<-chan *CascadeUpdate, error)
	OnboardingStateSync(ctx context.Context, req *OnboardingStateSyncRequest) (*OnboardingStateSyncResponse, error)
	FetchToolCatalog(ctx context.Context, req *FetchToolCatalogRequest) (*FetchToolCatalogResponse, error)
	Close() error
}

// CortexServiceServer is the recovered gRPC server interface.
type CortexServiceServer interface {
	GetCascadeConfig(ctx context.Context, req *GetCascadeConfigRequest) (*GetCascadeConfigResponse, error)
	SendCascadeMessage(ctx context.Context, req *SendCascadeMessageRequest) (*SendCascadeMessageResponse, error)
	StreamCascadeUpdates(req *StreamCascadeUpdatesRequest, stream CortexService_StreamCascadeUpdatesServer) error
	OnboardingStateSync(ctx context.Context, req *OnboardingStateSyncRequest) (*OnboardingStateSyncResponse, error)
	FetchToolCatalog(ctx context.Context, req *FetchToolCatalogRequest) (*FetchToolCatalogResponse, error)
}

// CortexService_StreamCascadeUpdatesServer is the recovered
// server-side streaming interface.
type CortexService_StreamCascadeUpdatesServer interface {
	Send(*CascadeUpdate) error
	Context() context.Context
}

// ---------------------------------------------------------------------------
// Request / response message types
// ---------------------------------------------------------------------------

// GetCascadeConfigRequest is the request for RPC 1.
type GetCascadeConfigRequest struct {
	AgentID         string
	WorkspaceID     string
	IncludeDisabled bool
}

// GetCascadeConfigResponse is the response for RPC 1.
type GetCascadeConfigResponse struct {
	CascadeConfig *CascadeConfig
	Found         bool
}

// SendCascadeMessageRequest is the request for RPC 2.
type SendCascadeMessageRequest struct {
	CascadeID string
	Message   *AgentMessage
}

// SendCascadeMessageResponse is the response for RPC 2.
type SendCascadeMessageResponse struct {
	Accepted  bool
	StepID    string
	Timestamp time.Time
}

// StreamCascadeUpdatesRequest is the request for RPC 3.
type StreamCascadeUpdatesRequest struct {
	CascadeID string
	Filter    []string // event type filter
}

// CascadeUpdate is one update on the stream.
type CascadeUpdate struct {
	UpdateType string // "step" / "status" / "tool_call" / "approval"
	StepID     string
	Payload    string
	Timestamp  time.Time
}

// OnboardingStateSyncRequest is the request for RPC 4.
type OnboardingStateSyncRequest struct {
	UserID     string
	DeviceID   string
	AppVersion string
}

// OnboardingStateSyncResponse is the response for RPC 4.
type OnboardingStateSyncResponse struct {
	State     string // "first_run" / "configured" / "migrated"
	NextSteps []string
}

// FetchToolCatalogRequest is the request for RPC 5.
type FetchToolCatalogRequest struct {
	Tier ModelTier
}

// FetchToolCatalogResponse is the response for RPC 5.
type FetchToolCatalogResponse struct {
	Tools []*SkillMetadata
}

// ---------------------------------------------------------------------------
// Placeholder HTTP/gRPC client
// ---------------------------------------------------------------------------

// DialOptions configures the placeholder client.
type DialOptions struct {
	// Endpoint is the cortex_pb endpoint (default: prod).
	Endpoint string
	// BearerToken is the OAuth access token used for auth.
	BearerToken func() (string, error)
	// HTTPClient is the underlying *http.Client. If nil, a default
	// 60s-timeout client is used.
	HTTPClient *http.Client
	// UseMTLSProxy is the address of the mTLS bypass proxy.
	UseMTLSProxy string
}

// DialCortexService creates a CortexServiceClient that can dial
// cloudcode-pa.googleapis.com. The returned client preserves the
// recovered wire format but does not actually call the RPCs (calls
// return ErrCortexPBNotImplemented or a structured placeholder).
func DialCortexService(opts DialOptions) (CortexServiceClient, error) {
	if opts.Endpoint == "" {
		opts.Endpoint = "https://cloudcode-pa.googleapis.com"
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 60 * time.Second}
	}
	if opts.BearerToken == nil {
		return nil, errors.New("cortex_pb: BearerToken func is required")
	}
	return &cortexClient{opts: opts}, nil
}

type cortexClient struct {
	opts DialOptions
	mu   sync.Mutex
}

func (c *cortexClient) GetCascadeConfig(ctx context.Context, req *GetCascadeConfigRequest) (*GetCascadeConfigResponse, error) {
	if req == nil || req.AgentID == "" {
		return nil, errors.New("cortex_pb: AgentID is required")
	}
	if _, err := c.opts.BearerToken(); err != nil {
		return nil, err
	}
	return &GetCascadeConfigResponse{
		CascadeConfig: NewCascadeConfig(),
		Found:         true,
	}, nil
}

func (c *cortexClient) SendCascadeMessage(ctx context.Context, req *SendCascadeMessageRequest) (*SendCascadeMessageResponse, error) {
	if req == nil || req.CascadeID == "" {
		return nil, errors.New("cortex_pb: CascadeID is required")
	}
	if _, err := c.opts.BearerToken(); err != nil {
		return nil, err
	}
	return &SendCascadeMessageResponse{
		Accepted:  true,
		StepID:    "recovered-placeholder-step-id",
		Timestamp: time.Now(),
	}, nil
}

func (c *cortexClient) StreamCascadeUpdates(ctx context.Context, req *StreamCascadeUpdatesRequest) (<-chan *CascadeUpdate, error) {
	if req == nil || req.CascadeID == "" {
		return nil, errors.New("cortex_pb: CascadeID is required")
	}
	if _, err := c.opts.BearerToken(); err != nil {
		return nil, err
	}
	out := make(chan *CascadeUpdate, 1)
	go func() {
		defer close(out)
		out <- &CascadeUpdate{
			UpdateType: "status",
			StepID:     "recovered-step-0",
			Payload:    `{"recovered":"1.3.5 placeholder stream"}`,
			Timestamp:  time.Now(),
		}
	}()
	return out, nil
}

func (c *cortexClient) OnboardingStateSync(ctx context.Context, req *OnboardingStateSyncRequest) (*OnboardingStateSyncResponse, error) {
	if req == nil || req.UserID == "" {
		return nil, errors.New("cortex_pb: UserID is required")
	}
	if _, err := c.opts.BearerToken(); err != nil {
		return nil, err
	}
	return &OnboardingStateSyncResponse{
		State:     "configured",
		NextSteps: []string{"open-workspace", "select-license"},
	}, nil
}

func (c *cortexClient) FetchToolCatalog(ctx context.Context, req *FetchToolCatalogRequest) (*FetchToolCatalogResponse, error) {
	if _, err := c.opts.BearerToken(); err != nil {
		return nil, err
	}
	return &FetchToolCatalogResponse{
		Tools: []*SkillMetadata{
			NewSkillMetadata(),
			{Name: "ripgrep", Description: "local code search", Path: "vendor/ripgrep"},
		},
	}, nil
}

func (c *cortexClient) Close() error { return nil }

// ---------------------------------------------------------------------------
// Service descriptor (for integration with grpc).
// ---------------------------------------------------------------------------

// CortexService is the recovered service descriptor for the
// cortex_pb CortexService. Mirrors the v4 Service interface from
// grpc/grpc.go.
type CortexService struct{}

// Name returns the fully-qualified proto service name.
func (s *CortexService) Name() string {
	return ServiceName
}

// Description returns a short human-readable description.
func (s *CortexService) Description() string {
	return ServiceName + " — cortex agent configuration service (GetCascadeConfig / SendCascadeMessage / StreamCascadeUpdates / OnboardingStateSync / FetchToolCatalog)"
}

// Methods returns the recovered RPC method names.
func (s *CortexService) Methods() []string {
	out := make([]string, len(CortexServiceMethodNames))
	copy(out, CortexServiceMethodNames)
	return out
}
