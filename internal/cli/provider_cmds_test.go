// Tests for `agy provider …` and `agy model …`.
//
// The two commands are the clone's entry points into the provider
// configuration. Their contract:
//
//   - a write changes the field it was asked to change and nothing else
//     (a merge, not a replace);
//   - the credential is never printed;
//   - an error names the thing that is wrong and exits non-zero.
//
// `agy --help` is byte-pinned to the reference's twelve visible
// subcommands, so both commands are hidden; internal/cli/parser_test.go
// pins that count.
package cli

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/configtest"
	"github.com/justwasm/pigasus/internal/models"
)

// runProviderCmd runs a provider/model handler and returns stdout, stderr
// and the error.
func runProviderCmd(t *testing.T, fn func(*JetskiCLI, []string) error, args ...string) (string, string, error) {
	t.Helper()
	var out, errb bytes.Buffer
	j := newParityJetski(&out, &errb)
	err := fn(j, args)
	return out.String(), errb.String(), err
}

// readProviders reads the isolated provider configuration.
func readProviders(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(models.ProvidersPath())
	if err != nil {
		t.Fatalf("read providers: %v", err)
	}
	return string(raw)
}

func TestProviderAdd_WritesTheProviderSection(t *testing.T) {
	configtest.Isolate(t)
	out, _, err := runProviderCmd(t, runProviderAdd,
		"gateway", "--base-url", "https://gw.example/v1", "--api-key", "$GW_KEY",
		"--name", "Gateway", "--model", "minimax-m3=MiniMax-M3")
	if err != nil {
		t.Fatalf("provider add: %v", err)
	}
	if !strings.Contains(out, "added provider gateway") {
		t.Fatalf("out = %q", out)
	}
	body := readProviders(t)
	for _, want := range []string{
		`"gateway"`,
		`"base_url": "https://gw.example/v1"`,
		`"api_key": "$GW_KEY"`,
		`"id": "minimax-m3"`,
		`"model": "MiniMax-M3"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the file is missing %s:\n%s", want, body)
		}
	}
}

func TestProviderAdd_MergesIntoAnExistingProvider(t *testing.T) {
	configtest.Isolate(t)
	if _, _, err := runProviderCmd(t, runProviderAdd,
		"gateway", "--base-url", "https://gw.example/v1", "--api-key", "$GW_KEY",
		"--model", "minimax-m3=MiniMax-M3"); err != nil {
		t.Fatalf("provider add: %v", err)
	}
	// A second call that only changes the endpoint must leave the
	// credential and the models alone.
	if _, _, err := runProviderCmd(t, runProviderAdd,
		"gateway", "--base-url", "https://gw2.example/v1"); err != nil {
		t.Fatalf("provider add: %v", err)
	}
	body := readProviders(t)
	if !strings.Contains(body, "https://gw2.example/v1") {
		t.Fatalf("the endpoint was not changed:\n%s", body)
	}
	if !strings.Contains(body, `"api_key": "$GW_KEY"`) {
		t.Fatalf("the credential was lost:\n%s", body)
	}
	if !strings.Contains(body, `"id": "minimax-m3"`) {
		t.Fatalf("the model was lost:\n%s", body)
	}
}

func TestProviderAdd_RequiresAnEndpoint(t *testing.T) {
	configtest.Isolate(t)
	_, _, err := runProviderCmd(t, runProviderAdd, "gateway")
	if err == nil {
		t.Fatal("a provider without an endpoint was accepted")
	}
}

func TestProviderAdd_KeepsUnknownFields(t *testing.T) {
	configtest.WriteProviders(t, `{"oauth": {"refresh_token": "keep-me"},
	  "providers": {"gateway": {"id": "gateway", "base_url": "https://old/v1",
	  "future_field": "keep-me-too"}}}`)
	if _, _, err := runProviderCmd(t, runProviderAdd,
		"gateway", "--base-url", "https://new/v1"); err != nil {
		t.Fatalf("provider add: %v", err)
	}
	body := readProviders(t)
	if !strings.Contains(body, "keep-me") || !strings.Contains(body, "keep-me-too") {
		t.Fatalf("an unknown field was dropped:\n%s", body)
	}
}

func TestProviderList_PrintsTheProviders(t *testing.T) {
	configtest.Isolate(t)
	if _, _, err := runProviderCmd(t, runProviderAdd,
		"gateway", "--base-url", "https://gw.example/v1", "--name", "Gateway",
		"--model", "minimax-m3=MiniMax-M3"); err != nil {
		t.Fatalf("provider add: %v", err)
	}
	out, _, err := runProviderCmd(t, runProviderList)
	if err != nil {
		t.Fatalf("provider list: %v", err)
	}
	if !strings.Contains(out, "providers: "+models.ProvidersPath()) {
		t.Fatalf("out = %q", out)
	}
	if !strings.Contains(out, "gateway") || !strings.Contains(out, "MiniMax-M3") {
		t.Fatalf("out = %q", out)
	}
}

func TestProviderShow_HidesTheCredential(t *testing.T) {
	configtest.Isolate(t)
	if _, _, err := runProviderCmd(t, runProviderAdd,
		"gateway", "--base-url", "https://gw.example/v1", "--api-key", "sk-secret-value"); err != nil {
		t.Fatalf("provider add: %v", err)
	}
	out, _, err := runProviderCmd(t, runProviderShow, "gateway")
	if err != nil {
		t.Fatalf("provider show: %v", err)
	}
	if strings.Contains(out, "sk-secret-value") {
		t.Fatalf("the credential was printed:\n%s", out)
	}
	if !strings.Contains(out, "****") {
		t.Fatalf("the credential was not masked:\n%s", out)
	}
}

func TestProviderShow_ShowsAnEnvironmentReference(t *testing.T) {
	configtest.Isolate(t)
	if _, _, err := runProviderCmd(t, runProviderAdd,
		"gateway", "--base-url", "https://gw.example/v1", "--api-key", "$GW_KEY"); err != nil {
		t.Fatalf("provider add: %v", err)
	}
	out, _, err := runProviderCmd(t, runProviderShow, "gateway")
	if err != nil {
		t.Fatalf("provider show: %v", err)
	}
	if !strings.Contains(out, "$GW_KEY") {
		t.Fatalf("the environment reference was not shown:\n%s", out)
	}
}

func TestProviderShow_UnknownProvider(t *testing.T) {
	configtest.Isolate(t)
	_, _, err := runProviderCmd(t, runProviderShow, "nope")
	if err == nil {
		t.Fatal("an unknown provider was accepted")
	}
}

func TestProviderRemove_RemovesTheProvider(t *testing.T) {
	configtest.Isolate(t)
	if _, _, err := runProviderCmd(t, runProviderAdd,
		"gateway", "--base-url", "https://gw.example/v1"); err != nil {
		t.Fatalf("provider add: %v", err)
	}
	out, _, err := runProviderCmd(t, runProviderRemove, "gateway")
	if err != nil {
		t.Fatalf("provider remove: %v", err)
	}
	if !strings.Contains(out, "removed provider gateway") {
		t.Fatalf("out = %q", out)
	}
	if strings.Contains(readProviders(t), "gw.example") {
		t.Fatalf("the provider survived:\n%s", readProviders(t))
	}
	if _, _, err := runProviderCmd(t, runProviderRemove, "gateway"); err == nil {
		t.Fatal("removing a missing provider succeeded")
	}
}

func TestModelAdd_RequiresAKnownProvider(t *testing.T) {
	configtest.Isolate(t)
	_, _, err := runProviderCmd(t, runModelAdd, "m", "--provider", "nope")
	if err == nil {
		t.Fatal("a model under an unknown provider was accepted")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err = %v, want the provider named", err)
	}
}

func TestModelAdd_AndRemove(t *testing.T) {
	configtest.Isolate(t)
	if _, _, err := runProviderCmd(t, runProviderAdd,
		"gateway", "--base-url", "https://gw.example/v1"); err != nil {
		t.Fatalf("provider add: %v", err)
	}
	out, _, err := runProviderCmd(t, runModelAdd, "minimax-m3", "--provider", "gateway",
		"--model", "MiniMax-M3", "--name", "MiniMax M3", "--context-window", "1048576")
	if err != nil {
		t.Fatalf("model add: %v", err)
	}
	if !strings.Contains(out, "added minimax-m3 under gateway") {
		t.Fatalf("out = %q", out)
	}
	body := readProviders(t)
	if !strings.Contains(body, `"context_window": 1048576`) {
		t.Fatalf("the context window was not written:\n%s", body)
	}
	out, _, err = runProviderCmd(t, runModelRemove, "minimax-m3")
	if err != nil {
		t.Fatalf("model remove: %v", err)
	}
	if !strings.Contains(out, "removed minimax-m3") {
		t.Fatalf("out = %q", out)
	}
	if strings.Contains(readProviders(t), "minimax-m3") {
		t.Fatalf("the model survived:\n%s", readProviders(t))
	}
}

func TestModelAdd_MergesIntoAnExistingModel(t *testing.T) {
	configtest.Isolate(t)
	if _, _, err := runProviderCmd(t, runProviderAdd,
		"gateway", "--base-url", "https://gw.example/v1"); err != nil {
		t.Fatalf("provider add: %v", err)
	}
	if _, _, err := runProviderCmd(t, runModelAdd, "m", "--provider", "gateway",
		"--model", "Upstream", "--name", "Label", "--context-window", "1024"); err != nil {
		t.Fatalf("model add: %v", err)
	}
	if _, _, err := runProviderCmd(t, runModelAdd, "m", "--provider", "gateway",
		"--context-window", "2048"); err != nil {
		t.Fatalf("model add: %v", err)
	}
	body := readProviders(t)
	if !strings.Contains(body, `"context_window": 2048`) {
		t.Fatalf("the context window was not updated:\n%s", body)
	}
	if !strings.Contains(body, `"model": "Upstream"`) {
		t.Fatalf("the upstream id was lost:\n%s", body)
	}
	if !strings.Contains(body, `"name": "Label"`) {
		t.Fatalf("the display name was lost:\n%s", body)
	}
}

// TestModelAdd_CarriesTheSupportsImagesMarker pins the per-model
// marker that says a model takes pictures — the flag's three states:
// unstated (no key at all), stated yes, stated no.
func TestModelAdd_CarriesTheSupportsImagesMarker(t *testing.T) {
	configtest.Isolate(t)
	if _, _, err := runProviderCmd(t, runProviderAdd,
		"gateway", "--base-url", "https://gw.example/v1"); err != nil {
		t.Fatalf("provider add: %v", err)
	}
	if _, _, err := runProviderCmd(t, runModelAdd, "silent", "--provider", "gateway"); err != nil {
		t.Fatalf("model add: %v", err)
	}
	if body := readProviders(t); strings.Contains(body, "supports_images") {
		t.Fatalf("a model that says nothing must carry no marker:\n%s", body)
	}
	if _, _, err := runProviderCmd(t, runModelAdd, "yes", "--provider", "gateway",
		"--supports-images"); err != nil {
		t.Fatalf("model add: %v", err)
	}
	if _, _, err := runProviderCmd(t, runModelAdd, "no", "--provider", "gateway",
		"--supports-images=false"); err != nil {
		t.Fatalf("model add: %v", err)
	}
	body := readProviders(t)
	if !strings.Contains(body, `"supports_images": true`) {
		t.Errorf("the marker was not written:\n%s", body)
	}
	if !strings.Contains(body, `"supports_images": false`) {
		t.Errorf("the marker's no was not written:\n%s", body)
	}
	// A later write that does not mention the flag keeps the marker.
	if _, _, err := runProviderCmd(t, runModelAdd, "yes", "--provider", "gateway",
		"--name", "Yes"); err != nil {
		t.Fatalf("model add: %v", err)
	}
	if body := readProviders(t); !strings.Contains(body, `"supports_images": true`) {
		t.Errorf("the marker did not survive the write:\n%s", body)
	}
}

// TestModelAdd_CarriesTheModelAttributes pins the flags that write the
// attributes the Web UI's model picker reads.
//
// Each flag has the same three states as the image marker: a flag that
// is not passed keeps the file's value, a passed flag states its own.
func TestModelAdd_CarriesTheModelAttributes(t *testing.T) {
	configtest.Isolate(t)
	if _, _, err := runProviderCmd(t, runProviderAdd,
		"gateway", "--base-url", "https://gw.example/v1"); err != nil {
		t.Fatalf("provider add: %v", err)
	}
	if _, _, err := runProviderCmd(t, runModelAdd, "silent", "--provider", "gateway"); err != nil {
		t.Fatalf("model add: %v", err)
	}
	body := readProviders(t)
	for _, key := range []string{
		"supported_mime_types", "disabled", "quota_info",
		"supports_thought_circulation", "model_url", "tag_title",
		"tag_description", "effort_supported",
	} {
		if strings.Contains(body, key) {
			t.Errorf("a model that says nothing must carry no %q:\n%s", key, body)
		}
	}

	if _, _, err := runProviderCmd(t, runModelAdd, "full", "--provider", "gateway",
		"--supported-mime-types", "image/png,application/pdf",
		"--disabled",
		"--quota-reset-time", "2026-10-27T00:00:00Z",
		"--quota-remaining-fraction", "0.25",
		"--supports-thought-circulation",
		"--model-url", "https://example.com/models/full",
		"--tag-title", "Fast",
		"--tag-description", "A quick model",
		"--effort-supported=false"); err != nil {
		t.Fatalf("model add: %v", err)
	}
	body = readProviders(t)
	for _, want := range []string{
		`"supported_mime_types": [`,
		`"image/png"`,
		`"application/pdf"`,
		`"disabled": true`,
		`"quota_info": {`,
		`"remaining_fraction": 0.25`,
		`"reset_time": "2026-10-27T00:00:00Z"`,
		`"supports_thought_circulation": true`,
		`"model_url": "https://example.com/models/full"`,
		`"tag_title": "Fast"`,
		`"tag_description": "A quick model"`,
		`"effort_supported": false`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the file does not carry %s:\n%s", want, body)
		}
	}

	// A later write that does not mention the flags keeps them.
	if _, _, err := runProviderCmd(t, runModelAdd, "full", "--provider", "gateway",
		"--name", "Full"); err != nil {
		t.Fatalf("model add: %v", err)
	}
	body = readProviders(t)
	for _, want := range []string{`"disabled": true`, `"tag_title": "Fast"`, `"quota_info": {`} {
		if !strings.Contains(body, want) {
			t.Errorf("the attribute %s did not survive the write:\n%s", want, body)
		}
	}

	// A bad timestamp is caught at the flag, not at the picker's screen.
	if _, _, err := runProviderCmd(t, runModelAdd, "bad", "--provider", "gateway",
		"--quota-reset-time", "not-a-timestamp"); err == nil {
		t.Errorf("a broken timestamp was accepted")
	}
	// A fraction outside the share it is must be caught too.
	if _, _, err := runProviderCmd(t, runModelAdd, "bad", "--provider", "gateway",
		"--quota-remaining-fraction", "2"); err == nil {
		t.Errorf("a fraction above one was accepted")
	}
}

func TestModelList_FiltersByProvider(t *testing.T) {
	configtest.Isolate(t)
	if _, _, err := runProviderCmd(t, runProviderAdd,
		"gateway", "--base-url", "https://gw.example/v1"); err != nil {
		t.Fatalf("provider add: %v", err)
	}
	if _, _, err := runProviderCmd(t, runModelAdd, "one", "--provider", "gateway"); err != nil {
		t.Fatalf("model add: %v", err)
	}
	out, _, err := runProviderCmd(t, runModelList, "--provider", "gateway")
	if err != nil {
		t.Fatalf("model list: %v", err)
	}
	if !strings.Contains(out, "one") {
		t.Fatalf("out = %q", out)
	}
	out, _, err = runProviderCmd(t, runModelList, "--provider", "other")
	if err != nil {
		t.Fatalf("model list: %v", err)
	}
	if strings.Contains(out, "one") {
		t.Fatalf("the filter was ignored:\n%s", out)
	}
}

func TestProviderCommands_AreVisible(t *testing.T) {
	for _, sub := range StandardSubcommands() {
		switch sub.Name {
		case "provider", "model", "accounts":
			if sub.Hidden {
				t.Fatalf("%q must be visible in --help", sub.Name)
			}
		}
	}
}

func TestSplitPositionalID(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		takes    map[string]bool
		wantID   string
		wantArgs []string
	}{
		{
			name:     "id first",
			args:     []string{"gateway", "--base-url", "https://x/v1"},
			takes:    providerAddValueFlags,
			wantID:   "gateway",
			wantArgs: []string{"--base-url", "https://x/v1"},
		},
		{
			name:     "id last",
			args:     []string{"--base-url", "https://x/v1", "gateway"},
			takes:    providerAddValueFlags,
			wantID:   "gateway",
			wantArgs: []string{"--base-url", "https://x/v1"},
		},
		{
			name:     "a flag value is not the id",
			args:     []string{"--model", "a=b"},
			takes:    providerAddValueFlags,
			wantID:   "",
			wantArgs: []string{"--model", "a=b"},
		},
		{
			name:     "an equals form carries its value",
			args:     []string{"--base-url=https://x/v1", "gateway"},
			takes:    providerAddValueFlags,
			wantID:   "gateway",
			wantArgs: []string{"--base-url=https://x/v1"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, rest := splitPositionalID(tc.args, tc.takes)
			if id != tc.wantID {
				t.Fatalf("id = %q, want %q", id, tc.wantID)
			}
			if strings.Join(rest, " ") != strings.Join(tc.wantArgs, " ") {
				t.Fatalf("rest = %v, want %v", rest, tc.wantArgs)
			}
		})
	}
}

func TestProviderCommands_UseTheIsolatedConfiguration(t *testing.T) {
	// The commands must read the file the environment points at, not the
	// user's real configuration.
	configtest.Isolate(t)
	if _, _, err := runProviderCmd(t, runProviderAdd,
		"gateway", "--base-url", "https://gw.example/v1"); err != nil {
		t.Fatalf("provider add: %v", err)
	}
	if _, err := os.Stat(models.ProvidersPath()); err != nil {
		t.Fatalf("the configuration was not written to the isolated path: %v", err)
	}
}

// ---- provider test -----------------------------------------------------

// providerTestConfig points an isolated configuration at a test server.
func providerTestConfig(t *testing.T, baseURL string) {
	t.Helper()
	configtest.WriteProviders(t, fmt.Sprintf(`{"providers": {"gateway": {"id": "gateway",
	  "base_url": %q, "api_key": "sk-test", "models": [{"id": "m3", "model": "MiniMax-M3"}]}}}`, baseURL))
}

func TestProviderTest_ReportsAVerifiedProvider(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"pong"}}]}`))
	}))
	defer srv.Close()
	providerTestConfig(t, srv.URL+"/v1")
	out, _, err := runProviderCmd(t, runProviderTest, "gateway")
	if err != nil {
		t.Fatalf("provider test: %v", err)
	}
	if !strings.Contains(out, "✓") {
		t.Fatalf("out = %q, want a ✓", out)
	}
	if !strings.Contains(out, "gateway") {
		t.Fatalf("out = %q, want it to name the provider", out)
	}
}

func TestProviderTest_ReportsARejectedCredential(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid api key"}`))
	}))
	defer srv.Close()
	providerTestConfig(t, srv.URL+"/v1")
	out, _, err := runProviderCmd(t, runProviderTest, "gateway")
	if err == nil {
		t.Fatal("provider test: no error, want a non-zero exit")
	}
	var codeErr *exitCodeError
	if !errors.As(err, &codeErr) || codeErr.code != 1 {
		t.Fatalf("err = %v, want exit code 1", err)
	}
	if !strings.Contains(out, "✗") {
		t.Fatalf("out = %q, want a ✗", out)
	}
	if !strings.Contains(out, "rejected") {
		t.Fatalf("out = %q, want it to say the credential was rejected", out)
	}
}

func TestProviderTest_NamesAnUnknownProvider(t *testing.T) {
	configtest.Isolate(t)
	_, _, err := runProviderCmd(t, runProviderTest, "nope")
	if err == nil {
		t.Fatal("provider test: no error, want a non-zero exit")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err = %v, want it to name the provider", err)
	}
}

func TestProviderTest_NeverPrintsTheCredential(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"pong"}}]}`))
	}))
	defer srv.Close()
	providerTestConfig(t, srv.URL+"/v1")
	out, errb, err := runProviderCmd(t, runProviderTest, "gateway")
	if err != nil {
		t.Fatalf("provider test: %v", err)
	}
	if strings.Contains(out, "sk-test") || strings.Contains(errb, "sk-test") {
		t.Fatalf("the credential was printed: out=%q err=%q", out, errb)
	}
}
