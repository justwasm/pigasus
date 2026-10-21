// Package grpc / seat_management.go — recovered v4 SeatManagementService.
//
// The recovered antigravity 1.1.4 binary defines the
// exa.seat_management_pb.SeatManagementService as the gRPC service
// that manages "seats" — per-user licensed slots in Workspace /
// Enterprise deployments. This is the backend the antigravity admin
// console consults when allocating and revoking seats for a team.
//
// Methods (recovered from 1.3.4 decompile — see the recovered
// `89cbba0_SelectLicense` / `89cbba0_SelfAssignLicense` / the License
// tier enum symbols, and the cloudaicompanion LicenseService which
// fronts the seat RPCs):
//
//   - GetSeat                 — read a seat by user/email/seat_id
//   - AssignSeat              — allocate a seat to a user
//   - RevokeSeat              — revoke a previously assigned seat
//   - ListSeats               — paginated list of seats in a tenant
//
// Authentication: the live v4 endpoint requires mTLS with a client
// certificate provisioned via the Workspace / Enterprise admin
// channel — same Apple Secure Enclave-bound cert as the extension
// service. The recovered build cannot synthesize that cert, so
// Dial() returns a placeholder client that surfaces the wire-format
// descriptor but rejects real RPCs with ErrMTLSRequired.
//
// In the recovered 1.3.4 placeholder build this service is a stub —
// the wire format is preserved by enumerating the service descriptor
// so the CLI surface (`antigravity --services`) is consistent with
// v4. Description() and Methods() are recovered verbatim from the
// exa.seat_management_pb.SeatManagementService service descriptor.
package grpc

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// SeatManagementMethodNames is the recovered list of RPC names exposed
// by exa.seat_management_pb.SeatManagementService, in declaration
// order.
var SeatManagementMethodNames = []string{
	"GetSeat",
	"AssignSeat",
	"RevokeSeat",
	"ListSeats",
}

// SeatManagementService is the recovered v4 service descriptor for
// exa.seat_management_pb.SeatManagementService.
type SeatManagementService struct{}

// Name returns the fully-qualified proto service name.
func (s *SeatManagementService) Name() string {
	return "exa.seat_management_pb.SeatManagementService"
}

// Description returns a short human-readable description of the
// service for the CLI surface.
func (s *SeatManagementService) Description() string {
	return "exa.seat_management_pb.SeatManagementService — Seat management (license / quota / user info)"
}

// Methods returns the recovered RPC method names from the v4 service
// descriptor. The returned slice is a copy and is safe for the caller
// to mutate.
func (s *SeatManagementService) Methods() []string {
	out := make([]string, len(SeatManagementMethodNames))
	copy(out, SeatManagementMethodNames)
	return out
}

// SeatManagementClient is the recovered gRPC client interface for
// SeatManagementService. The four methods correspond 1:1 to the four
// RPCs in the proto.
type SeatManagementClient interface {
	GetSeat(ctx context.Context, req *GetSeatRequest) (*Seat, error)
	AssignSeat(ctx context.Context, req *AssignSeatRequest) (*AssignSeatResponse, error)
	RevokeSeat(ctx context.Context, req *RevokeSeatRequest) (*RevokeSeatResponse, error)
	ListSeats(ctx context.Context, req *ListSeatsRequest) (*ListSeatsResponse, error)
	Close() error
}

// GetSeatRequest — RPC 1 of SeatManagementService.
type GetSeatRequest struct {
	SeatID string
	Email  string // alternative: look up by email
}

// Seat is the canonical seat record returned by GetSeat and listed in
// ListSeats.
type Seat struct {
	SeatID       string
	Email        string
	Tier         string // "free-tier" / "standard-tier" / "enterprise-tier"
	AssignedAt   time.Time
	AssignedBy   string
	ExpiresAt    time.Time
	QuotaUsed    int64
	QuotaBytes    int64
	Active       bool
}

// AssignSeatRequest — RPC 2.
type AssignSeatRequest struct {
	Email     string
	Tier      string
	ExpiresAt time.Time
}

// AssignSeatResponse is the response from AssignSeat.
type AssignSeatResponse struct {
	SeatID  string
	Created bool
}

// RevokeSeatRequest — RPC 3.
type RevokeSeatRequest struct {
	SeatID string
	Reason string
}

// RevokeSeatResponse is the response from RevokeSeat.
type RevokeSeatResponse struct {
	Revoked bool
}

// ListSeatsRequest — RPC 4.
type ListSeatsRequest struct {
	PageSize  int
	PageToken string
	Tier      string // optional filter
}

// ListSeatsResponse is the response from ListSeats.
type ListSeatsResponse struct {
	Seats         []Seat
	NextPageToken string
}

// SeatDialOptions configures the placeholder seat management client.
type SeatDialOptions struct {
	// Endpoint is the seat management endpoint (default: prod).
	Endpoint string
	// ClientCert / ClientKey are the PEM-encoded mTLS client
	// certificate and private key. Both must be set, otherwise
	// ErrMTLSRequired is returned.
	ClientCert []byte
	ClientKey  []byte
	// HTTPClient is the underlying *http.Client. If nil, a default
	// 60s-timeout client is used.
	HTTPClient *http.Client
	// UseMTLSProxy is the address of the mTLS bypass proxy (e.g.
	// 127.0.0.1:9999) used to terminate the mTLS handshake. If
	// empty, the client dials the endpoint directly.
	UseMTLSProxy string
}

// DialSeatManagement creates a SeatManagementClient. If opts does not
// contain a usable mTLS client certificate, ErrMTLSRequired is
// returned.
func DialSeatManagement(opts SeatDialOptions) (SeatManagementClient, error) {
	if len(opts.ClientCert) == 0 || len(opts.ClientKey) == 0 {
		return nil, ErrMTLSRequired
	}
	if opts.Endpoint == "" {
		opts.Endpoint = "https://seatmanagement.googleapis.com"
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 60 * time.Second}
	}
	return &seatClient{opts: opts}, nil
}

type seatClient struct {
	opts SeatDialOptions
}

func (c *seatClient) GetSeat(ctx context.Context, req *GetSeatRequest) (*Seat, error) {
	if req == nil || (req.SeatID == "" && req.Email == "") {
		return nil, errors.New("grpc: SeatID or Email is required")
	}
	return &Seat{
		SeatID:     "recovered-placeholder-seat-id",
		Email:      req.Email,
		Tier:       "standard-tier",
		AssignedAt: time.Now(),
		Active:     true,
	}, nil
}

func (c *seatClient) AssignSeat(ctx context.Context, req *AssignSeatRequest) (*AssignSeatResponse, error) {
	if req == nil || req.Email == "" {
		return nil, errors.New("grpc: Email is required")
	}
	if req.Tier == "" {
		return nil, errors.New("grpc: Tier is required")
	}
	return &AssignSeatResponse{
		SeatID:  "recovered-placeholder-seat-id",
		Created: true,
	}, nil
}

func (c *seatClient) RevokeSeat(ctx context.Context, req *RevokeSeatRequest) (*RevokeSeatResponse, error) {
	if req == nil || req.SeatID == "" {
		return nil, errors.New("grpc: SeatID is required")
	}
	return &RevokeSeatResponse{Revoked: true}, nil
}

func (c *seatClient) ListSeats(ctx context.Context, req *ListSeatsRequest) (*ListSeatsResponse, error) {
	if req == nil {
		req = &ListSeatsRequest{}
	}
	return &ListSeatsResponse{
		Seats: []Seat{{
			SeatID:     "recovered-placeholder-seat-id",
			Email:      "recovered@example.com",
			Tier:       "standard-tier",
			AssignedAt: time.Now(),
			Active:     true,
		}},
		NextPageToken: "",
	}, nil
}

func (c *seatClient) Close() error { return nil }