// Package language_server / cascade_types.go — RPC request/response
// message types for LanguageServerService.
//
// Recovered from google3/third_party/jetski/language_server_pb/language_server_go_proto.*.
package language_server

import (
	"context"
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
