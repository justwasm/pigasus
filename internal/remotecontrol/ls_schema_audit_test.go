// ls_schema_audit_test.go — the schema audit over the whole language
// server.
//
// # What this test is
//
// The clone answers the language server's 358 methods with hand-written
// maps. Every member of every answer is either a member the reference's
// own descriptor knows — or it is *dropped*, silently, on the way to
// the Web UI:
//
//   - the Web UI's decoder ignores an unknown member, so the page
//     renders and the member's data is simply not there;
//   - proto-JSON (the clone's own encoder, lsproto/convert.go) rejects
//     a member whose *type* does not fit, and drops the whole list
//     with it when one of its rows carries one.
//
// Neither failure is visible in a log. This test walks every answer
// the clone gives for every method of the reference's own service, and
// reports a member the schema does not know — and an enum value the
// schema does not name.
//
// # What it found
//
// Four answers were in the wrong shape:
//
//   - the plugin's rules answered `{"textMemory":{"text":…}}`, and the
//     schema's CortexMemoryText has a single member, `content` (1) —
//     the rule's text was being dropped on the floor;
//   - the customization states named their type "PLUGIN" instead of
//     "REFRESH_CUSTOMIZATION_TYPE_PLUGIN", and an enum name the schema
//     does not know takes the whole list with it;
//   - SelfAssignLicense answered an `authResult`, which is the shape
//     of LoginResponse and SelectLicenseResponse, not of
//     SelfAssignLicenseResponse;
//   - ListEnvironments, ListResourceProviders and ListAvailableResources
//     were routed by the alias table onto the clone's legacy dashboard
//     methods, whose members the language server's schema does not
//     know.
//
// The answers are now in the schema's own members, and this test is
// what keeps them there.
package remotecontrol

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/lsproto"
)

// TestEveryLanguageServerAnswerUsesMembersTheSchemaKnows audits the
// whole service.
//
// The method list is the reference's own (the 1.4.8 fixture); the
// descriptor is the reference's own too (the FileDescriptorProto read
// out of the agy 1.4.8 binary, committed at
// lsproto/descriptor/language_server.protoset).
//
// A method the clone does not answer is skipped: there is no answer to
// audit. A method that refuses the empty parameters is skipped too —
// the audit is about the *shape* of an answer, not about which
// parameters a method insists on.
func TestEveryLanguageServerAnswerUsesMembersTheSchemaKnows(t *testing.T) {
	// The answers read the plugin tree; point it at a scratch
	// directory so the audit never sees the user's own plugins.
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	d := pluginTestDispatcher(t)

	codec, err := lsproto.Shared()
	if err != nil {
		t.Fatalf("lsproto.Shared: %v", err)
	}
	fixture, err := os.Open(filepath.Join("testdata", "language_server_rpcs_1.4.8.txt"))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer fixture.Close()

	sc := bufio.NewScanner(fixture)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		method := fields[0]
		md, ok := codec.OutputMessage(method)
		if !ok {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		answer, rpcErr := d.routeMethod(ctx, method, []byte(`{}`))
		cancel()
		if rpcErr != nil {
			continue
		}
		obj, ok := answer.(map[string]any)
		if !ok {
			continue
		}
		t.Run(method, func(t *testing.T) {
			assertMembersKnown(t, md, obj, method)
		})
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan fixture: %v", err)
	}
}
