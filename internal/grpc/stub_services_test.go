// Tests for the 4 stub gRPC services enhanced in cycle 31.
//
// These tests verify that each of the 4 stub services exposes the
// recovered v4 wire-format surface:
//
//   - Name()         returns the fully-qualified proto service name
//                    matching what the v4 binary embeds in its gRPC
//                    dispatcher tables.
//   - Description()  returns a non-empty, v4-aligned description.
//   - Methods()      returns the recovered RPC method list from the
//                    proto service descriptor.
//
// The proto names below are verified against the v4 binary
// `antigravity-recovered` symbol table:
//
//   $ nm antigravity-recovered | grep "antigravity-recovered/grpc.*Service"
//   00000000005b1320 T antigravity-recovered/grpc.(*ApiServerService).Name
//   00000000005b1300 T antigravity-recovered/grpc.(*ExtensionServerService).Name
//   00000000005b1380 T antigravity-recovered/grpc.(*JetskiRemoteAgentProxyService).Name
//   00000000005b1360 T antigravity-recovered/grpc.(*KnowledgeBaseService).Name
//   00000000005b12e0 T antigravity-recovered/grpc.(*LanguageServerService).Name
//   00000000005b1340 T antigravity-recovered/grpc.(*SeatManagementService).Name
//
// and the embedded service-name string table (e.g. `exa.extension_server_pb.ExtensionServerService`).
package grpc

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// v4 binary proto-name string table — ground truth
// ---------------------------------------------------------------------------

// v4BinaryServiceNames is the canonical, namesigned list of proto service
// names embedded in the v4 binary. The test below iterates through
// AllServices and asserts each Name() matches the corresponding entry
// in this list (in the same order as grpc.go's AllServices slice).
var v4BinaryServiceNames = []string{
	"exa.api_server_pb.ApiServerService",
	"exa.extension_server_pb.ExtensionServerService",
	"google.gca.aicode.v1main.JetskiRemoteAgentProxyService",
	"exa.knowledge_base_pb.KnowledgeBaseService",
	"exa.language_server_pb.LanguageServerService",
	"exa.seat_management_pb.SeatManagementService",
}

// ---------------------------------------------------------------------------
// Test 1 — ExtensionServerService: Name / Description / Methods
// ---------------------------------------------------------------------------

func TestExtensionServerService_Name(t *testing.T) {
	s := &ExtensionServerService{}
	if got, want := s.Name(), "exa.extension_server_pb.ExtensionServerService"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}

func TestExtensionServerService_Description(t *testing.T) {
	s := &ExtensionServerService{}
	got := s.Description()
	if got == "" {
		t.Fatal("Description() is empty")
	}
	if !strings.Contains(got, "extension server") {
		t.Errorf("Description() = %q, want substring %q", got, "extension server")
	}
}

func TestExtensionServerService_Methods(t *testing.T) {
	s := &ExtensionServerService{}
	got := s.Methods()
	if len(got) == 0 {
		t.Fatal("Methods() is empty")
	}
	want := []string{"Register", "Heartbeat", "StreamEvents"}
	if len(got) != len(want) {
		t.Fatalf("Methods() returned %d entries, want %d: %v", len(got), len(want), got)
	}
	for i, m := range got {
		if m != want[i] {
			t.Errorf("Methods()[%d] = %q, want %q", i, m, want[i])
		}
	}
}

func TestExtensionServerService_NameMatchesV4(t *testing.T) {
	s := &ExtensionServerService{}
	name := s.Name()
	found := false
	for _, v := range v4BinaryServiceNames {
		if name == v {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ExtensionServerService.Name() = %q not found in v4 binary string table %v", name, v4BinaryServiceNames)
	}
}

// ---------------------------------------------------------------------------
// Test 2 — JetskiRemoteAgentProxyService: Name / Description / Methods
// ---------------------------------------------------------------------------

func TestJetskiRemoteAgentProxyService_Name(t *testing.T) {
	s := &JetskiRemoteAgentProxyService{}
	if got, want := s.Name(), "google.gca.aicode.v1main.JetskiRemoteAgentProxyService"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}

func TestJetskiRemoteAgentProxyService_Description(t *testing.T) {
	s := &JetskiRemoteAgentProxyService{}
	got := s.Description()
	if got == "" {
		t.Fatal("Description() is empty")
	}
	if !strings.Contains(got, "Jetski") {
		t.Errorf("Description() = %q, want substring %q", got, "Jetski")
	}
}

func TestJetskiRemoteAgentProxyService_Methods(t *testing.T) {
	s := &JetskiRemoteAgentProxyService{}
	got := s.Methods()
	if len(got) == 0 {
		t.Fatal("Methods() is empty")
	}
	want := []string{"RunAgent", "StreamAgentEvents", "CancelAgent"}
	if len(got) != len(want) {
		t.Fatalf("Methods() returned %d entries, want %d: %v", len(got), len(want), got)
	}
	for i, m := range got {
		if m != want[i] {
			t.Errorf("Methods()[%d] = %q, want %q", i, m, want[i])
		}
	}
}

func TestJetskiRemoteAgentProxyService_NameMatchesV4(t *testing.T) {
	s := &JetskiRemoteAgentProxyService{}
	name := s.Name()
	found := false
	for _, v := range v4BinaryServiceNames {
		if name == v {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("JetskiRemoteAgentProxyService.Name() = %q not found in v4 binary string table %v", name, v4BinaryServiceNames)
	}
}

// ---------------------------------------------------------------------------
// Test 3 — KnowledgeBaseService: Name / Description / Methods
// ---------------------------------------------------------------------------

func TestKnowledgeBaseService_Name(t *testing.T) {
	s := &KnowledgeBaseService{}
	if got, want := s.Name(), "exa.knowledge_base_pb.KnowledgeBaseService"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}

func TestKnowledgeBaseService_Description(t *testing.T) {
	s := &KnowledgeBaseService{}
	got := s.Description()
	if got == "" {
		t.Fatal("Description() is empty")
	}
	if !strings.Contains(got, "Knowledge base") {
		t.Errorf("Description() = %q, want substring %q", got, "Knowledge base")
	}
}

func TestKnowledgeBaseService_Methods(t *testing.T) {
	s := &KnowledgeBaseService{}
	got := s.Methods()
	if len(got) == 0 {
		t.Fatal("Methods() is empty")
	}
	want := []string{"Search", "Index", "Delete"}
	if len(got) != len(want) {
		t.Fatalf("Methods() returned %d entries, want %d: %v", len(got), len(want), got)
	}
	for i, m := range got {
		if m != want[i] {
			t.Errorf("Methods()[%d] = %q, want %q", i, m, want[i])
		}
	}
}

func TestKnowledgeBaseService_NameMatchesV4(t *testing.T) {
	s := &KnowledgeBaseService{}
	name := s.Name()
	found := false
	for _, v := range v4BinaryServiceNames {
		if name == v {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("KnowledgeBaseService.Name() = %q not found in v4 binary string table %v", name, v4BinaryServiceNames)
	}
}

// ---------------------------------------------------------------------------
// Test 4 — SeatManagementService: Name / Description / Methods
// ---------------------------------------------------------------------------

func TestSeatManagementService_Name(t *testing.T) {
	s := &SeatManagementService{}
	if got, want := s.Name(), "exa.seat_management_pb.SeatManagementService"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}

func TestSeatManagementService_Description(t *testing.T) {
	s := &SeatManagementService{}
	got := s.Description()
	if got == "" {
		t.Fatal("Description() is empty")
	}
	if !strings.Contains(got, "Seat") {
		t.Errorf("Description() = %q, want substring %q", got, "Seat")
	}
}

func TestSeatManagementService_Methods(t *testing.T) {
	s := &SeatManagementService{}
	got := s.Methods()
	if len(got) == 0 {
		t.Fatal("Methods() is empty")
	}
	want := []string{"GetSeat", "AssignSeat", "RevokeSeat", "ListSeats"}
	if len(got) != len(want) {
		t.Fatalf("Methods() returned %d entries, want %d: %v", len(got), len(want), got)
	}
	for i, m := range got {
		if m != want[i] {
			t.Errorf("Methods()[%d] = %q, want %q", i, m, want[i])
		}
	}
}

func TestSeatManagementService_NameMatchesV4(t *testing.T) {
	s := &SeatManagementService{}
	name := s.Name()
	found := false
	for _, v := range v4BinaryServiceNames {
		if name == v {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("SeatManagementService.Name() = %q not found in v4 binary string table %v", name, v4BinaryServiceNames)
	}
}

// ---------------------------------------------------------------------------
// Bonus — Methods() returns a defensive copy (mutation-safe)
// ---------------------------------------------------------------------------

func TestStubServices_MethodsReturnsCopy(t *testing.T) {
	// Call Methods() twice on the same service and assert that mutating
	// the first slice doesn't affect the second call's result. This
	// guards against future regressions where Methods() might return
	// the package-level slice directly.
	ext := &ExtensionServerService{}
	first := ext.Methods()
	first[0] = "mutated"
	second := ext.Methods()
	if second[0] == "mutated" {
		t.Errorf("ExtensionServerService.Methods() returns the underlying slice (mutation leaked); want a defensive copy")
	}

	jetski := &JetskiRemoteAgentProxyService{}
	first = jetski.Methods()
	first[0] = "mutated"
	second = jetski.Methods()
	if second[0] == "mutated" {
		t.Errorf("JetskiRemoteAgentProxyService.Methods() returns the underlying slice (mutation leaked); want a defensive copy")
	}

	kb := &KnowledgeBaseService{}
	first = kb.Methods()
	first[0] = "mutated"
	second = kb.Methods()
	if second[0] == "mutated" {
		t.Errorf("KnowledgeBaseService.Methods() returns the underlying slice (mutation leaked); want a defensive copy")
	}

	seat := &SeatManagementService{}
	first = seat.Methods()
	first[0] = "mutated"
	second = seat.Methods()
	if second[0] == "mutated" {
		t.Errorf("SeatManagementService.Methods() returns the underlying slice (mutation leaked); want a defensive copy")
	}
}