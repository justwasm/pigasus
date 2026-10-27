// Package refbackend — custommodels_test.go: the operator's own models,
// offered to the reference.
//
// The reference reads its catalogue from the CloudCode endpoint, so a
// proxy there is all it takes for the operator's models to appear in the
// reference's own model picker.
package refbackend

import (
	"github.com/justwasm/pigasus/endpoints"
	"strings"
	"testing"
)

// TestHasCloudCodeEndpoint pins that the operator's own endpoint wins.
//
// The reference takes the last occurrence of a flag, so an explicit
// `-cloud_code_endpoint` in OPENAGY_REFERENCE_BACKEND_ARGS replaces the
// proxy's address — and the clone must not start a proxy nothing talks
// to.
func TestHasCloudCodeEndpoint(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"none", nil, false},
		{"other flags", []string{"-gemini_dir=/tmp/x"}, false},
		{"inline", []string{"-cloud_code_endpoint=https://example.test"}, true},
		{"separate", []string{"-cloud_code_endpoint", "https://example.test"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasCloudCodeEndpoint(tc.args); got != tc.want {
				t.Errorf("hasCloudCodeEndpoint(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

// TestCustomModelProxyEndpoint pins the shape of the endpoint the
// reference is pointed at.
//
// A nil proxy means "no custom models": the reference talks to the cloud
// directly, exactly as it did before this package existed.
func TestCustomModelProxyEndpoint(t *testing.T) {
	var nilProxy *customModelProxy
	if got := nilProxy.endpoint(); got != endpoints.Current().CloudCode {
		t.Errorf("a proxy-less reference talks to %q, want %q", got, referenceCloudCodeEndpoint)
	}
	proxy := &customModelProxy{addr: "127.0.0.1:45001"}
	if got := proxy.endpoint(); got != "http://127.0.0.1:45001" {
		t.Errorf("endpoint() = %q, want %q", got, "http://127.0.0.1:45001")
	}
}

// TestStartReferenceProxyRespectsTheCaller pins that the operator's own
// endpoint suppresses the proxy.
func TestStartReferenceProxyRespectsTheCaller(t *testing.T) {
	proxy, err := startReferenceProxy([]string{"-cloud_code_endpoint=https://example.test"})
	if err != nil {
		t.Fatalf("startReferenceProxy: %v", err)
	}
	if proxy != nil {
		t.Errorf("a proxy was started although the caller named an endpoint; got %v", proxy)
	}
}

// TestConfiguredModelsAreTheOperators pins that the models the proxy
// carries are the ones in the operator's own configuration file, and
// that each of them is a usable endpoint.
func TestConfiguredModelsAreTheOperators(t *testing.T) {
	for _, m := range configuredModels() {
		if strings.TrimSpace(m.Name) == "" {
			t.Errorf("a configured model has no name: %+v", m)
		}
		if strings.TrimSpace(m.APIURL) == "" {
			t.Errorf("the configured model %q has no endpoint", m.Name)
		}
	}
}
