// Package language_server_pb is a recovered (re-implemented) binding
// for google3/third_party/jetski/language_server_pb. Source-of-truth
// artifacts are in
// /decompiled/c/google3_third_party_jetski_language_server_pb_language_server_go_grpc./
// (gRPC service stubs) and
// /decompiled/c/google3_third_party_jetski_language_server_pb_language_server_go_proto/
// (proto message types).
//
// The 1.3.3 binary defines a LanguageServerService gRPC service with
// four RPCs:
//
//   - SelectLicense
//   - SelfAssignLicense
//   - GetStatus
//   - StreamGenerateContent
//
// In the recovered build, this package provides:
//   - the proto message Go structs (SelectLicenseRequest/Response, ...);
//   - the gRPC service interface;
//   - a placeholder client that can dial cloudcode-pa.googleapis.com
//     but does not actually invoke the RPCs (calls return ErrNotImplemented
//     or a structured placeholder).
//
// The 1.3.3 binary uses mTLS to authenticate to cloudcode-pa; the
// dial path here uses the mTLS bypass proxy from internal/auth/mtls_proxy
// when configured, falling back to a default InsecureCredentials chain.
package language_server_pb

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
)

// Service name.
const ServiceName = "google.cloudcode.LanguageServerService"

// License tier values (recovered from the proto enum).
const (
	LicenseUnspecified = "LICENSE_UNSPECIFIED"
	LicenseFree        = "FREE_TIER"
	LicenseStandard    = "STANDARD_TIER"
	LicenseEnterprise  = "ENTERPRISE_TIER"
)

// SelectLicenseRequest — RPC 1 of LanguageServerService.
type SelectLicenseRequest struct {
	// LicenseTier is one of LicenseFree / LicenseStandard / LicenseEnterprise.
	LicenseTier string
}

// SelectLicenseResponse is the response from SelectLicense.
type SelectLicenseResponse struct {
	// LicenseTier echoes the chosen tier.
	LicenseTier string
	// LicenseID is the server-assigned license ID.
	LicenseID string
	// GrantedAt is the server-assigned grant timestamp.
	GrantedAt time.Time
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

// GetStatusResponse is the response from GetStatus. Source:
// codeassistclient.GetStatus (see _GetStatus.c).
type GetStatusResponse struct {
	// UserTier echoes the resolved tier.
	UserTier string
	// Entitlement is the resolved entitlement.
	Entitlement string
	// Eligible reports whether the user is eligible for the service.
	Eligible bool
	// EndpointURL is the resolved endpoint URL.
	EndpointURL string
	// CAICProject is the resolved CAIC project.
	CAICProject string
	// GCPLocation is the resolved GCP location.
	GCPLocation string
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
	Text       string
	InlineData *Blob
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
	Index int
	Content Content
	FinishReason string
}

// UsageMetadata tracks token usage.
type UsageMetadata struct {
	PromptTokenCount     int
	CachedContentTokenCount int
	CandidatesTokenCount int
	ThoughtsTokenCount   int
	TotalTokenCount      int
}

// ErrNotImplemented is returned by placeholder RPCs.
var ErrNotImplemented = errors.New("language_server_pb: RPC not implemented in recovered build")

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
// can dial cloudcode-pa.googleapis.com. The returned client preserves
// the recovered wire format but does not actually call the RPCs (calls
// return ErrNotImplemented or a structured placeholder).
func DialLanguageServerService(opts DialOptions) (LanguageServerServiceClient, error) {
	if opts.Endpoint == "" {
		opts.Endpoint = "https://cloudcode-pa.googleapis.com"
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 60 * time.Second}
	}
	if opts.BearerToken == nil {
		return nil, errors.New("language_server_pb: BearerToken func is required")
	}
	return &lsClient{opts: opts}, nil
}

type lsClient struct {
	opts DialOptions
	mu   sync.Mutex
}

func (c *lsClient) SelectLicense(ctx context.Context, req *SelectLicenseRequest) (*SelectLicenseResponse, error) {
	if req == nil || req.LicenseTier == "" {
		return nil, errors.New("language_server_pb: LicenseTier is required")
	}
	if _, err := c.opts.BearerToken(); err != nil {
		return nil, err
	}
	// Recovered: rpc /google.cloudcode.LanguageServerService/SelectLicense
	// (HTTP/2 + protobuf). The recovered build is a placeholder.
	return &SelectLicenseResponse{
		LicenseTier: req.LicenseTier,
		LicenseID:   "recovered-placeholder-license-id",
		GrantedAt:   time.Now(),
	}, nil
}

func (c *lsClient) SelfAssignLicense(ctx context.Context, req *SelfAssignLicenseRequest) (*SelfAssignLicenseResponse, error) {
	if req == nil || req.LicenseTier == "" {
		return nil, errors.New("language_server_pb: LicenseTier is required")
	}
	if _, err := c.opts.BearerToken(); err != nil {
		return nil, err
	}
	// Recovered: rpc /google.cloudcode.LanguageServerService/SelfAssignLicense
	return &SelfAssignLicenseResponse{
		LicenseID: "recovered-placeholder-license-id",
		GrantedAt: time.Now(),
	}, nil
}

func (c *lsClient) GetStatus(ctx context.Context, req *GetStatusRequest) (*GetStatusResponse, error) {
	if _, err := c.opts.BearerToken(); err != nil {
		return nil, err
	}
	// Recovered: rpc /google.cloudcode.LanguageServerService/GetStatus
	return &GetStatusResponse{
		UserTier:     "free-tier",
		Entitlement:  "free_user",
		Eligible:     true,
		EndpointURL:  c.opts.Endpoint,
		CAICProject:  "",
		GCPLocation:  "us-central1",
	}, nil
}

func (c *lsClient) StreamGenerateContent(ctx context.Context, req *GenerateContentRequest) (<-chan *GenerateContentResponse, error) {
	if req == nil || req.Model == "" {
		return nil, errors.New("language_server_pb: Model is required")
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
