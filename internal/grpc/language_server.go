// Package grpc / language_server.go — recovered v4 LanguageServerService.
//
// The recovered antigravity 1.3.3 binary defines the
// exa.language_server_pb.LanguageServerService as the gRPC service
// that fronts the language server RPCs. This file absorbs the cycle-25
// language_server_pb package (2 .go files) verbatim.
//
// API surface:
//
//   - exa.language_server_pb.LanguageServerService
//       .SelectLicense         →  license tier selection
//       .SelfAssignLicense     →  license tier self-assignment
//       .GetStatus             →  user tier + entitlement + eligibility
//       .StreamGenerateContent →  streaming GenerateContent
//
// The recovered binding is a placeholder: the real Cloud AI Companion
// gRPC endpoint requires Google's internal mTLS trust, which is not
// available in the sandbox. Calls return structured placeholders so
// the public surface can be exercised end-to-end.
package grpc

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
)

// ServiceName is the gRPC service name constant.
const ServiceName = "exa.language_server_pb.LanguageServerService"

// License tier values (recovered from the proto enum).
const (
	LicenseUnspecified = "LICENSE_UNSPECIFIED"
	LicenseFree        = "FREE_TIER"
	LicenseStandard    = "STANDARD_TIER"
	LicenseEnterprise  = "ENTERPRISE_TIER"
)

// SelectLicenseRequest — RPC 1 of LanguageServerService.
type SelectLicenseRequest struct {
	LicenseTier string
}

// SelectLicenseResponse is the response from SelectLicense.
type SelectLicenseResponse struct {
	LicenseTier string
	LicenseID   string
	GrantedAt   time.Time
}

// SelfAssignLicenseRequest — RPC 2.
type SelfAssignLicenseRequest struct {
	LicenseTier string
}

// SelfAssignLicenseResponse is the response from SelfAssignLicense.
type SelfAssignLicenseResponse struct {
	LicenseID string
	GrantedAt time.Time
}

// GetStatusRequest — RPC 3.
type GetStatusRequest struct{}

// GetStatusResponse is the response from GetStatus.
type GetStatusResponse struct {
	UserTier     string
	Entitlement  string
	Eligible     bool
	EndpointURL  string
	CAICProject  string
	GCPLocation  string
}

// GenerateContentRequest — RPC 4. Recovered proto type from
// cloudcode-pa.googleapis.com.
type GenerateContentRequest struct {
	Model    string
	Contents []Content
	Stream   bool
}

// Content is one part of a multi-part prompt.
type Content struct {
	Role  string // "user" / "model" / "system"
	Parts []Part
}

// Part is one piece of a Content (text / inline_data / function_call / ...).
type Part struct {
	Text         string
	InlineData   *Blob
	FunctionCall *FunctionCall
}

// Blob is a base64-encoded payload (image / audio / ...).
type Blob struct {
	MimeType string
	Data     []byte
}

// FunctionCall is a function invocation produced by the model.
type FunctionCall struct {
	Name string
	Args map[string]any
}

// GenerateContentResponse is a single chunk of the streaming
// StreamGenerateContent response.
type GenerateContentResponse struct {
	Candidates    []Candidate
	ModelVersion  string
	ResponseID    string
	UsageMetadata UsageMetadata
}

// Candidate is one model response candidate.
type Candidate struct {
	Index       int
	Content     Content
	FinishReason string
}

// UsageMetadata tracks token usage.
type UsageMetadata struct {
	PromptTokenCount       int
	CachedContentTokenCount int
	CandidatesTokenCount   int
	ThoughtsTokenCount     int
	TotalTokenCount        int
}

// ErrLSNotImplemented is returned by placeholder RPCs.
var ErrLSNotImplemented = errors.New("grpc: language_server RPC not implemented in recovered build")

// LanguageServerServiceClient is the recovered gRPC client interface.
// The four methods correspond 1:1 to the four RPCs in the proto.
type LanguageServerServiceClient interface {
	SelectLicense(ctx context.Context, req *SelectLicenseRequest) (*SelectLicenseResponse, error)
	SelfAssignLicense(ctx context.Context, req *SelfAssignLicenseRequest) (*SelfAssignLicenseResponse, error)
	GetStatus(ctx context.Context, req *GetStatusRequest) (*GetStatusResponse, error)
	StreamGenerateContent(ctx context.Context, req *GenerateContentRequest) (<-chan *GenerateContentResponse, error)
	Close() error
}

// LanguageServerServiceServer is the recovered gRPC server interface.
type LanguageServerServiceServer interface {
	SelectLicense(ctx context.Context, req *SelectLicenseRequest) (*SelectLicenseResponse, error)
	SelfAssignLicense(ctx context.Context, req *SelfAssignLicenseRequest) (*SelfAssignLicenseResponse, error)
	GetStatus(ctx context.Context, req *GetStatusRequest) (*GetStatusResponse, error)
	StreamGenerateContent(req *GenerateContentRequest, stream LanguageServerService_StreamGenerateContentServer) error
}

// LanguageServerService_StreamGenerateContentServer is the recovered
// server-side streaming interface.
type LanguageServerService_StreamGenerateContentServer interface {
	Send(*GenerateContentResponse) error
	Context() context.Context
}

// ---------------------------------------------------------------------------
// Placeholder HTTP/gRPC client
// ---------------------------------------------------------------------------

// DialOptions configures the placeholder client.
type DialOptions struct {
	// Endpoint is the cloudcode-pa endpoint (default: prod).
	Endpoint string
	// BearerToken is the OAuth access token used for auth.
	BearerToken func() (string, error)
	// HTTPClient is the underlying *http.Client. If nil, a default
	// 60s-timeout client is used.
	HTTPClient *http.Client
	// UseMTLSProxy is the address of the mTLS bypass proxy (e.g.
	// 127.0.0.1:9999) used to terminate the mTLS handshake. If empty,
	// the client dials the endpoint directly.
	UseMTLSProxy string
}

// DialLanguageServerService creates a LanguageServerServiceClient that
// can dial cloudcode-pa.googleapis.com.
func DialLanguageServerService(opts DialOptions) (LanguageServerServiceClient, error) {
	if opts.Endpoint == "" {
		opts.Endpoint = "https://cloudcode-pa.googleapis.com"
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 60 * time.Second}
	}
	if opts.BearerToken == nil {
		return nil, errors.New("grpc: BearerToken func is required")
	}
	return &lsClient{opts: opts}, nil
}

type lsClient struct {
	opts DialOptions
	mu   sync.Mutex
}

func (c *lsClient) SelectLicense(ctx context.Context, req *SelectLicenseRequest) (*SelectLicenseResponse, error) {
	if req == nil || req.LicenseTier == "" {
		return nil, errors.New("grpc: LicenseTier is required")
	}
	if _, err := c.opts.BearerToken(); err != nil {
		return nil, err
	}
	// Recovered: rpc /exa.language_server_pb.LanguageServerService/SelectLicense
	return &SelectLicenseResponse{
		LicenseTier: req.LicenseTier,
		LicenseID:   "recovered-placeholder-license-id",
		GrantedAt:   time.Now(),
	}, nil
}

func (c *lsClient) SelfAssignLicense(ctx context.Context, req *SelfAssignLicenseRequest) (*SelfAssignLicenseResponse, error) {
	if req == nil || req.LicenseTier == "" {
		return nil, errors.New("grpc: LicenseTier is required")
	}
	if _, err := c.opts.BearerToken(); err != nil {
		return nil, err
	}
	return &SelfAssignLicenseResponse{
		LicenseID: "recovered-placeholder-license-id",
		GrantedAt: time.Now(),
	}, nil
}

func (c *lsClient) GetStatus(ctx context.Context, req *GetStatusRequest) (*GetStatusResponse, error) {
	if _, err := c.opts.BearerToken(); err != nil {
		return nil, err
	}
	return &GetStatusResponse{
		UserTier:    "free-tier",
		Entitlement: "free_user",
		Eligible:    true,
		EndpointURL: c.opts.Endpoint,
		CAICProject: "",
		GCPLocation: "us-central1",
	}, nil
}

func (c *lsClient) StreamGenerateContent(ctx context.Context, req *GenerateContentRequest) (<-chan *GenerateContentResponse, error) {
	if req == nil || req.Model == "" {
		return nil, errors.New("grpc: Model is required")
	}
	if _, err := c.opts.BearerToken(); err != nil {
		return nil, err
	}
	out := make(chan *GenerateContentResponse, 1)
	go func() {
		defer close(out)
		out <- &GenerateContentResponse{
			ModelVersion: req.Model,
			Candidates: []Candidate{{
				Index: 0,
				Content: Content{
					Role: "model",
					Parts: []Part{{
						Text: "[recovered-1.3.3 placeholder] stream response for " + req.Model,
					}},
				},
				FinishReason: "STOP",
			}},
		}
	}()
	return out, nil
}

func (c *lsClient) Close() error { return nil }

// ---------------------------------------------------------------------------
// LanguageServerService — v4 service descriptor
// ---------------------------------------------------------------------------

// LanguageServerService is the recovered v4 service descriptor for
// exa.language_server_pb.LanguageServerService.
type LanguageServerService struct{}

// Name returns the fully-qualified proto service name.
func (s *LanguageServerService) Name() string {
	return "exa.language_server_pb.LanguageServerService"
}

// Description returns a short human-readable description of the
// service for the CLI surface.
func (s *LanguageServerService) Description() string {
	return "exa.language_server_pb.LanguageServerService — language server (SelectLicense / SelfAssignLicense / GetStatus / StreamGenerateContent)"
}
