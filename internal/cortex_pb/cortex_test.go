// Tests for the recovered internal/cortex_pb package.
//
// These tests verify that:
//   - the 5 RPCs of CortexService are exposed (CortexServiceClient + CortexServiceServer
//     interfaces + CortexServiceMethodNames slice)
//   - the proto message structs construct + return their getters correctly
//   - the 5 sub-tests of the placeholder gRPC client behave as expected
//   - the CortexService{} descriptor mirrors the v4 Service interface
package cortex_pb

import (
	"context"
	"testing"
)

func TestCortexService_Names(t *testing.T) {
	want := []string{
		"GetCascadeConfig",
		"SendCascadeMessage",
		"StreamCascadeUpdates",
		"OnboardingStateSync",
		"FetchToolCatalog",
	}
	if len(CortexServiceMethodNames) != len(want) {
		t.Fatalf("got %d methods, want %d", len(CortexServiceMethodNames), len(want))
	}
	for i, name := range want {
		if CortexServiceMethodNames[i] != name {
			t.Errorf("CortexServiceMethodNames[%d] = %q, want %q", i, CortexServiceMethodNames[i], name)
		}
	}
}

func TestDialCortexService_MissingToken(t *testing.T) {
	_, err := DialCortexService(DialOptions{Endpoint: "https://cloudcode-pa.googleapis.com"})
	if err == nil {
		t.Error("expected error when BearerToken func is missing")
	}
}

func TestGetCascadeConfig(t *testing.T) {
	client, err := DialCortexService(DialOptions{
		Endpoint:    "https://cloudcode-pa.googleapis.com",
		BearerToken: func() (string, error) { return "test-token", nil },
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()
	resp, err := client.GetCascadeConfig(context.Background(), &GetCascadeConfigRequest{
		AgentID:     "agent-001",
		WorkspaceID: "ws-001",
	})
	if err != nil {
		t.Fatalf("GetCascadeConfig: %v", err)
	}
	if !resp.Found {
		t.Error("Found = false, want true (placeholder)")
	}
	if resp.CascadeConfig == nil {
		t.Error("CascadeConfig is nil")
	}
}

func TestSendCascadeMessage(t *testing.T) {
	client, _ := DialCortexService(DialOptions{
		Endpoint:    "https://cloudcode-pa.googleapis.com",
		BearerToken: func() (string, error) { return "test-token", nil },
	})
	defer client.Close()
	resp, err := client.SendCascadeMessage(context.Background(), &SendCascadeMessageRequest{
		CascadeID: "cascade-001",
		Message:   NewAgentMessage(),
	})
	if err != nil {
		t.Fatalf("SendCascadeMessage: %v", err)
	}
	if !resp.Accepted {
		t.Error("Accepted = false, want true (placeholder)")
	}
	if resp.StepID == "" {
		t.Error("StepID is empty")
	}
}

func TestStreamCascadeUpdates(t *testing.T) {
	client, _ := DialCortexService(DialOptions{
		Endpoint:    "https://cloudcode-pa.googleapis.com",
		BearerToken: func() (string, error) { return "test-token", nil },
	})
	defer client.Close()
	stream, err := client.StreamCascadeUpdates(context.Background(), &StreamCascadeUpdatesRequest{
		CascadeID: "cascade-001",
	})
	if err != nil {
		t.Fatalf("StreamCascadeUpdates: %v", err)
	}
	count := 0
	for update := range stream {
		count++
		if update.UpdateType == "" {
			t.Error("UpdateType is empty")
		}
	}
	if count == 0 {
		t.Error("expected at least 1 update")
	}
}

func TestFetchToolCatalog(t *testing.T) {
	client, _ := DialCortexService(DialOptions{
		Endpoint:    "https://cloudcode-pa.googleapis.com",
		BearerToken: func() (string, error) { return "test-token", nil },
	})
	defer client.Close()
	resp, err := client.FetchToolCatalog(context.Background(), &FetchToolCatalogRequest{
		Tier: ModelTierPro,
	})
	if err != nil {
		t.Fatalf("FetchToolCatalog: %v", err)
	}
	if len(resp.Tools) == 0 {
		t.Error("Tools is empty")
	}
}

func TestProtoMessageGetters(t *testing.T) {
	// CascadeConfig.GetAgentApiConfig at 0x68323a0
	cc := NewCascadeConfig()
	cc.AgentApiConfig = NewAgentApiConfig()
	if got := cc.GetAgentApiConfig(); got == nil {
		t.Error("GetAgentApiConfig returned nil")
	}

	// CascadeExecutorConfig.GetStoreGenSvcRequest at 0x6830d60
	ec := NewCascadeExecutorConfig()
	ec.StoreGenSvcRequest = true
	if got := ec.GetStoreGenSvcRequest(); !got {
		t.Error("GetStoreGenSvcRequest = false, want true")
	}

	// CascadeConversationalPlannerConfig.GetAgenticMode at 0x6832a60
	cpc := NewCascadeConversationalPlannerConfig()
	cpc.AgenticMode = true
	if got := cpc.GetAgenticMode(); !got {
		t.Error("GetAgenticMode = false, want true")
	}

	// CascadeToolConfig.GetInvokeSubagent at 0x683cca0
	tc := NewCascadeToolConfig()
	tc.InvokeSubagent = NewSubagentMetadata()
	if got := tc.GetInvokeSubagent(); got == nil {
		t.Error("GetInvokeSubagent returned nil")
	}

	// CascadeToolConfig.GetBrowserSubagent at 0x683cf80
	tc.BrowserSubagent = NewBrowserSubagentConfig()
	if got := tc.GetBrowserSubagent(); got == nil {
		t.Error("GetBrowserSubagent returned nil")
	}

	// BuiltinAgentConfig.GetCascadeConfig at 0x6842900
	bac := NewBuiltinAgentConfig()
	bac.CascadeConfig = NewCascadeConfig()
	if got := bac.GetCascadeConfig(); got == nil {
		t.Error("GetCascadeConfig returned nil")
	}

	// AgentConfig.GetCascadeConfig at 0x6842bc0
	ac := NewAgentConfig()
	ac.CascadeConfig = NewCascadeConfig()
	if got := ac.GetCascadeConfig(); got == nil {
		t.Error("GetCascadeConfig returned nil")
	}

	// CustomAgentSpec.GetCascadeConfig at 0x6842ee0
	cas := NewCustomAgentSpec()
	cas.CascadeConfig = NewCascadeConfig()
	if got := cas.GetCascadeConfig(); got == nil {
		t.Error("GetCascadeConfig returned nil")
	}

	// CascadePlannerConfig.GetCustomAgent at 0x68444e0
	pc := NewCascadePlannerConfig()
	pc.CustomAgent = NewCustomAgentSpec()
	if got := pc.GetCustomAgent(); got == nil {
		t.Error("GetCustomAgent returned nil")
	}

	// CascadePlannerConfig.GetDisableSubagentConfigAdjustment at 0x6844840
	pc.DisableSubagentConfigAdjustment = true
	if got := pc.GetDisableSubagentConfigAdjustment(); !got {
		t.Error("GetDisableSubagentConfigAdjustment = false, want true")
	}

	// AgentScriptItem.GetCascadeConfig at 0x6828520
	asi := NewAgentScriptItem()
	asi.CascadeConfig = NewCascadeConfig()
	if got := asi.GetCascadeConfig(); got == nil {
		t.Error("GetCascadeConfig returned nil")
	}
}

func TestEnums(t *testing.T) {
	// Spot-check a few of the recovered enums. The String() method
	// returns the reference's own enum member name, so the value is the
	// full proto spelling.
	if AgentModePlanning.String() != "AGENT_MODE_PLANNING" {
		t.Errorf("AgentModePlanning.String() = %q, want AGENT_MODE_PLANNING", AgentModePlanning.String())
	}
	if CascadeRunStatusRunning.String() != "CASCADE_RUN_STATUS_RUNNING" {
		t.Errorf("CascadeRunStatusRunning.String() = %q, want CASCADE_RUN_STATUS_RUNNING", CascadeRunStatusRunning.String())
	}
	if CortexStepTypeRunCommand.String() != "CORTEX_STEP_TYPE_RUN_COMMAND" {
		t.Errorf("CortexStepTypeRunCommand.String() = %q, want CORTEX_STEP_TYPE_RUN_COMMAND", CortexStepTypeRunCommand.String())
	}
	if PlanStatusPlanned.String() != "PLAN_STATUS_PLANNED" {
		t.Errorf("PlanStatusPlanned.String() = %q, want PLAN_STATUS_PLANNED", PlanStatusPlanned.String())
	}
	if GoalStateAwaitingApproval.String() != "AWAITING_APPROVAL" {
		t.Errorf("GoalStateAwaitingApproval.String() = %q, want AWAITING_APPROVAL", GoalStateAwaitingApproval.String())
	}
	if ModelTierPro.String() != "MODEL_TIER_PRO" {
		t.Errorf("ModelTierPro.String() = %q, want MODEL_TIER_PRO", ModelTierPro.String())
	}
}

func TestCortexServiceDescriptor(t *testing.T) {
	s := &CortexService{}
	if s.Name() != ServiceName {
		t.Errorf("Name = %q, want %q", s.Name(), ServiceName)
	}
	if s.Description() == "" {
		t.Error("Description is empty")
	}
	if methods := s.Methods(); len(methods) != 5 {
		t.Errorf("Methods() returned %d, want 5", len(methods))
	}
}
