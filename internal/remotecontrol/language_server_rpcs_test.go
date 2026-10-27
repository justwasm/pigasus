// language_server_rpcs_test.go — the language server's own method list,
// against the reference's descriptor.
//
// 【一手】 2026-10-27: the FileDescriptorProto of
// third_party/jetski/language_server_pb/language_server.proto, read out
// of the agy 1.4.8 binary (file offset 0x3c81c00, 164 508 bytes). The
// service carried 358 RPCs at 1.4.8; the fixture is committed verbatim
// at testdata/language_server_rpcs_1.4.8.txt.
//
// The 1.4.7 method list is not a fixture of its own: it is the capture
// the clone's coverage work was built on (out/langserver/coverage-351.json,
// 351 methods). Reading it here pins the *delta* — eight methods added,
// one removed — so a reference release that moves the service in a way
// nobody looked at fails here rather than silently.
package remotecontrol

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// lsRPCFixture reads the committed method list.
func lsRPCFixture(t *testing.T) map[string][2]string {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "language_server_rpcs_1.4.8.txt"))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()

	out := map[string][2]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 3 {
			t.Fatalf("malformed fixture line %q", line)
		}
		out[parts[0]] = [2]string{parts[1], parts[2]}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan fixture: %v", err)
	}
	return out
}

// TestLanguageServerRPCs_MatchTheFixture pins the method count and the
// two permission methods' shapes.
func TestLanguageServerRPCs_MatchTheFixture(t *testing.T) {
	rpcs := lsRPCFixture(t)
	if len(rpcs) != 358 {
		t.Errorf("the fixture has %d RPCs, want 358 (agy 1.4.8)", len(rpcs))
	}
	cases := map[string][2]string{
		"GetPermissionSettings": {
			".exa.language_server_pb.GetPermissionSettingsRequest",
			".exa.language_server_pb.GetPermissionSettingsResponse",
		},
		"UpdatePermissionSettings": {
			".exa.language_server_pb.UpdatePermissionSettingsRequest",
			".exa.language_server_pb.UpdatePermissionSettingsResponse",
		},
	}
	for name, want := range cases {
		got, ok := rpcs[name]
		if !ok {
			t.Errorf("the fixture has no %s", name)
			continue
		}
		if got != want {
			t.Errorf("%s: got %v, want %v", name, got, want)
		}
	}
}

// TestLanguageServerRPCs_DeltaFromThePreviousRelease pins what moved
// between the 1.4.7 service (351 methods) and the 1.4.8 one (358).
func TestLanguageServerRPCs_DeltaFromThePreviousRelease(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "out", "langserver", "coverage-351.json"))
	if err != nil {
		if os.IsNotExist(err) {
			// In snapshot distributions (e.g. pigasus), out/ is omitted to keep
			// the repository small and clean of large reverse-engineering
			// dumps; the previous release's capture is one of them.
			t.Skip("the 1.4.7 capture is not in this distribution")
		}
		t.Fatalf("read the 1.4.7 capture: %v", err)
	}
	var capture struct {
		Methods map[string]json.RawMessage `json:"方法"`
	}
	if err := json.Unmarshal(raw, &capture); err != nil {
		t.Fatalf("parse the 1.4.7 capture: %v", err)
	}
	if len(capture.Methods) != 351 {
		t.Fatalf("the 1.4.7 capture has %d methods, want 351", len(capture.Methods))
	}

	rpcs := lsRPCFixture(t)
	added := []string{}
	for name := range rpcs {
		if _, ok := capture.Methods[name]; !ok {
			added = append(added, name)
		}
	}
	removed := []string{}
	for name := range capture.Methods {
		if _, ok := rpcs[name]; !ok {
			removed = append(removed, name)
		}
	}
	wantAdded := []string{
		"CallMcpAppTool",
		"GetMarketplaceFeatured",
		"GetPermissionSettings",
		"GetVoiceCallDebug",
		"ListBackgroundTasks",
		"SetPluginAuthAccountLabel",
		"UpdatePermissionSettings",
		"WarmUpEnvironment",
	}
	if len(added) != len(wantAdded) {
		t.Fatalf("the service gained %d methods, want %d: %v", len(added), len(wantAdded), added)
	}
	for _, name := range wantAdded {
		if _, ok := rpcs[name]; !ok {
			t.Errorf("the 1.4.8 service has no %s", name)
		}
	}
	if len(removed) != 1 || removed[0] != "GetResolvedCascadeConfig" {
		t.Errorf("the service lost %v, want [GetResolvedCascadeConfig]", removed)
	}
}
