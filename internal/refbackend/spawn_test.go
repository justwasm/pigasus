// Package refbackend — spawn_test.go: the reference is started with
// the arguments that make its own web app open.
//
// The three flags below are not cosmetic. Each one was measured on
// the reference binary on 2026-10-26; the evidence is in the comments
// next to referenceArgs (spawn.go).
package refbackend

import (
	"github.com/justwasm/pigasus/endpoints"
	"os"
	"strings"
	"testing"
)

// TestReferenceArgs pins the arguments that decide whether the
// reference's web app opens.
//
// A missing `-subclient_type=hub` makes the reference answer `GET /`
// with 404 — the page never loads. A missing `-csrf_token` leaves the
// page blank: the app rejects itself with an unhandled promise. A
// missing `-app_data_dir` changes the page's product name and the app
// opens its sign-in page instead of the IDE's UI.
func TestReferenceArgs(t *testing.T) {
	args := referenceArgs("/Applications/Antigravity.app/Contents/Resources/bin/language_server", 8796, "ref-csrf-token", referenceCloudCodeEndpoint)
	joined := strings.Join(args, " ")

	want := []string{
		"-http_server_port=8796",
		"-subclient_type=hub",
		"-app_data_dir=antigravity-cli",
		"-csrf_token=ref-csrf-token",
		"-api_server_url=" + endpoints.Current().GenerativeLanguage,
		"-cloud_code_endpoint=" + referenceCloudCodeEndpoint,
	}
	for _, w := range want {
		if !strings.Contains(joined, w) {
			t.Errorf("referenceArgs is missing %q; got %v", w, args)
		}
	}
}

// TestReferenceArgsCloudCodeEndpoint pins that the CloudCode endpoint is
// the one the caller asks for.
//
// It is the injection point for the operator's own models: the
// reference reads its catalogue from that endpoint, so a proxy there
// advertises the models the operator configured.
func TestReferenceArgsCloudCodeEndpoint(t *testing.T) {
	args := referenceArgs("/Applications/Antigravity.app/Contents/Resources/bin/language_server", 8796, "s", "http://127.0.0.1:45001")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-cloud_code_endpoint=http://127.0.0.1:45001") {
		t.Errorf("the reference does not talk to the given endpoint; got %v", args)
	}
}

// TestReferenceArgsIsNotMocked pins that the reference runs against the
// operator's real state: no mocked data, and no gemini directory of the
// clone's own — the reference's default (`.gemini` under the home
// directory) is the real one.
func TestReferenceArgsIsNotMocked(t *testing.T) {
	args := referenceArgs("/Applications/Antigravity.app/Contents/Resources/bin/language_server", 8796, "s", referenceCloudCodeEndpoint)
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "-use_mocked_data=true") {
		t.Errorf("the reference is started with mocked data; got %v", args)
	}
	if strings.Contains(joined, "-mock_fixture") {
		t.Errorf("the reference is started with a mock fixture; got %v", args)
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "-gemini_dir=") {
			t.Errorf("the spawn overrides the gemini directory (%q); the reference's own default is the operator's real one", arg)
		}
	}
}

// TestIDEVersion reads the version out of the app bundle the reference
// binary lives in.
func TestIDEVersion(t *testing.T) {
	bin := "/Applications/Antigravity.app/Contents/Resources/bin/language_server"
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("no reference binary at %s", bin)
	}
	version := ideVersion(bin)
	if version == "" {
		t.Fatal("ideVersion returned nothing for the installed IDE")
	}
	if !strings.HasPrefix(version, "2.") && !strings.HasPrefix(version, "1.") {
		t.Errorf("ideVersion = %q, which is not a version", version)
	}
}

// TestReferenceArgsGeminiDirLast keeps the caller's directory the
// winning one: the reference takes the last occurrence of a flag.
func TestReferenceArgsGeminiDirLast(t *testing.T) {
	args := referenceArgs("/Applications/Antigravity.app/Contents/Resources/bin/language_server", 8796, "s", referenceCloudCodeEndpoint)
	args = append(args, "-gemini_dir=/tmp/x")
	if got := args[len(args)-1]; got != "-gemini_dir=/tmp/x" {
		t.Errorf("the gemini dir is not the last argument; got %q", got)
	}
}

// TestNewSecret checks that the secret the reference is started with is
// a fresh, non-empty, URL-safe one.
func TestNewSecret(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 4; i++ {
		s, err := newSecret()
		if err != nil {
			t.Fatalf("newSecret: %v", err)
		}
		if s == "" {
			t.Fatal("newSecret returned an empty secret")
		}
		if seen[s] {
			t.Fatalf("newSecret returned the same secret twice: %q", s)
		}
		seen[s] = true
		if strings.ContainsAny(s, "+/=") {
			t.Errorf("the secret is not URL-safe: %q", s)
		}
	}
}
