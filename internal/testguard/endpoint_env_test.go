package testguard

import (
	"os"
	"testing"
)

// The endpoint variables are the ones a developer's shell carries because the
// launcher exported them for the reference binary. The endpoint resolver reads
// them ahead of the configuration file this package pins, so a test that
// resolves an endpoint would resolve the machine's own proxy — which is how
// TestCloudCodeImageSandboxFailover came to report `calls primary/mirror = 0/1`
// on a developer machine while passing elsewhere.
//
// Install calls clearEndpointEnv; this asserts the contract it relies on
// directly, so a variable that loses its clearing is caught here rather than
// through a confusing failure in a transport's test.
func TestClearEndpointEnv(t *testing.T) {
	for _, name := range endpointEnvNames {
		t.Setenv(name, "http://127.0.0.1:51558")
	}
	clearEndpointEnv()
	for _, name := range endpointEnvNames {
		if got := os.Getenv(name); got != "" {
			t.Errorf("%s = %q after clearing, want it unset: a test would resolve the machine's endpoint", name, got)
		}
	}
}

// An endpoint variable the resolver grows but this list does not is a variable
// a test follows to the machine. The list is the guard's contract, so its
// membership is asserted rather than only its effect.
func TestEndpointEnvListCoversTheResolver(t *testing.T) {
	want := map[string]bool{
		"CLOUD_CODE_URL":                 true, // endpoints.cloudCode
		"OPENAGY_UPSTREAM":               true, // agyproxy.EnvUpstream
		"OPENAGY_VOICE_ALT_BACKEND":      true, // endpoints.voice
		"PIGASUS_VOICE_ALT_BACKEND":      true, // endpoints.voice
		"OPENAGY_VOICE_URL":              true, // endpoints.voice
		"GOOGLE_GEMINI_BASE_URL":         true, // endpoints.voice
		"OPENAGY_VOICE_AICODE":           true, // endpoints.voiceAICode
		"JETSKI_VOICE_ADDR":              true, // endpoints.voiceAICode
		"ANTIGRAVITY_RC_ORIGIN_PATTERNS": true, // originpolicy.OriginEnvVar
		"OPENAGY_WRAPPER_ADDR":           true, // agyproxy.EnvAddr
	}
	got := map[string]bool{}
	for _, name := range endpointEnvNames {
		got[name] = true
	}
	for name := range want {
		if !got[name] {
			t.Errorf("%s is missing from endpointEnvNames: endpoints would read the machine's value", name)
		}
	}
	for name := range got {
		if !want[name] {
			t.Errorf("%s is cleared but not listed here; add it with the reader it belongs to", name)
		}
	}
}
