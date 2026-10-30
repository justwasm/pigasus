// dispatch_ls_cloud_test.go — the language server's cloud-backed
// surfaces.
//
// The four methods here answer with the *language server's* members,
// not the clone's legacy dashboard members. The difference is not
// cosmetic: proto-JSON drops a member the schema does not know, and
// drops the whole list with it when one of its rows carries one — so
// an answer in the wrong shape reaches the Web UI as an empty one,
// with no error anywhere.
//
// The three cloud registries are Google's, and the clone owns none of
// them: the lists are empty on purpose. An empty list is a true
// answer, and the Web UI expects it (`environments ?? []`).
package remotecontrol

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/justwasm/pigasus/lsproto"
)

// callLSRPC dispatches one language-server method and returns the
// decoded answer.
func callLSRPC(t *testing.T, d *Dispatcher, method, params string) map[string]any {
	t.Helper()
	res, rpcErr, ok := d.routeLanguageServerMethods(context.Background(), method, json.RawMessage(params))
	if !ok {
		t.Fatalf("%s is not routed", method)
	}
	if rpcErr != nil {
		t.Fatalf("%s: rpc error %+v", method, rpcErr)
	}
	out, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("%s: answer is %T, want a map", method, res)
	}
	return out
}

// lsCloudRPCCases is the list of the language server's cloud-backed
// methods and the parameters each one is called with.
func lsCloudRPCCases() []struct {
	method string
	params string
} {
	return []struct {
		method string
		params string
	}{
		{"SelfAssignLicense", `{"projectId":"proj-1","location":"us-central1"}`},
		{"ListEnvironments", `{"backend":2}`},
		{"ListResourceProviders", `{"backend":2}`},
		{"ListAvailableResources", `{"backend":2,"type":"github"}`},
	}
}

// TestTheCloudAnswersOnlyUseMembersTheSchemaKnows is the plugin
// answers' audit, run over the cloud surfaces.
//
// It found one: SelfAssignLicense answered an `authResult` — the shape
// of LoginResponse and SelectLicenseResponse — while the schema's
// SelfAssignLicenseResponse carries a `license`, an `assigned` flag
// and two strings. The Web UI's inspectAutoAssign() read `assigned`
// and `license` as undefined and reported "no licence" for an account
// that is entitled.
func TestTheCloudAnswersOnlyUseMembersTheSchemaKnows(t *testing.T) {
	d := pluginTestDispatcher(t)

	codec, err := lsproto.Shared()
	if err != nil {
		t.Fatalf("lsproto.Shared: %v", err)
	}
	for _, c := range lsCloudRPCCases() {
		md, ok := codec.OutputMessage(c.method)
		if !ok {
			t.Errorf("%s: the schema carries no answer for it", c.method)
			continue
		}
		answer := callLSRPC(t, d, c.method, c.params)
		t.Run(c.method, func(t *testing.T) {
			assertMembersKnown(t, md, answer, c.method)
		})
	}
}

// TestSelfAssignLicense_AnswersTheLicence pins the licence read.
//
// The answer carries the account's own tier under the project the
// caller named — the same tier the account chip reports.
func TestSelfAssignLicense_AnswersTheLicence(t *testing.T) {
	d := pluginTestDispatcher(t)

	answer := callLSRPC(t, d, "SelfAssignLicense", `{"projectId":"proj-1","location":"us-central1"}`)
	if _, ok := answer["authResult"]; ok {
		t.Errorf("the answer carries an authResult: %#v", answer)
	}
	licence, ok := answer["license"].(map[string]any)
	if !ok {
		t.Fatalf("the answer carries no license: %#v", answer)
	}
	if licence["userTier"] != lsPlanTierID {
		t.Errorf("the licence's tier is %v, want %s", licence["userTier"], lsPlanTierID)
	}
	if licence["tierDisplayName"] != lsPlanTierName {
		t.Errorf("the licence's display name is %v, want %s", licence["tierDisplayName"], lsPlanTierName)
	}
	if licence["projectId"] != "proj-1" {
		t.Errorf("the licence's project is %v, want proj-1", licence["projectId"])
	}
	if licence["location"] != "us-central1" {
		t.Errorf("the licence's location is %v, want us-central1", licence["location"])
	}
	if answer["assigned"] != true {
		t.Errorf("assigned = %v, want true", answer["assigned"])
	}
}

// TestTheCloudRegistriesAreEmpty pins the three cloud registries.
//
// The clone owns no capsule, no cloud resource provider and no cloud
// resource: those are Google's, and the clone runs on the user's own
// machine. The lists are empty — a true answer, and the one the Web UI
// expects.
func TestTheCloudRegistriesAreEmpty(t *testing.T) {
	d := pluginTestDispatcher(t)

	answer := callLSRPC(t, d, "ListEnvironments", `{"backend":2}`)
	environments, ok := answer["environments"].([]any)
	if !ok {
		t.Fatalf("the answer carries no environments list: %#v", answer)
	}
	if len(environments) != 0 {
		t.Errorf("got %d environments, want none: %#v", len(environments), environments)
	}

	answer = callLSRPC(t, d, "ListResourceProviders", `{"backend":2}`)
	providers, ok := answer["providers"].([]any)
	if !ok {
		t.Fatalf("the answer carries no providers list: %#v", answer)
	}
	if len(providers) != 0 {
		t.Errorf("got %d providers, want none: %#v", len(providers), providers)
	}

	answer = callLSRPC(t, d, "ListAvailableResources", `{"backend":2,"type":"github"}`)
	resources, ok := answer["resources"].([]any)
	if !ok {
		t.Fatalf("the answer carries no resources list: %#v", answer)
	}
	if len(resources) != 0 {
		t.Errorf("got %d resources, want none: %#v", len(resources), resources)
	}
}
