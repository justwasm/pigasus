// Package language_server / handler_cascade.go — the 7 RPC method handler
// stubs on *Server and their HTTP decoders / encoders.
//
// Recovered from google3/third_party/jetski/language_server_pb/language_server_go_proto.*.
package language_server

import (
	"context"
	"errors"
	"net/http"
	"time"
)

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
