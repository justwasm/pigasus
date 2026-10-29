// Package endpoints — endpoints_test.go
//
// The resolver's contract: the compiled-in defaults, the configuration's
// overrides, and the list-shaped endpoint.
package endpoints

import (
	"os"
	"path/filepath"
	"testing"
)

// writeConfig writes a configuration document into a fresh temporary
// directory and points $OPENAGY_CONFIG at it.
//
// Every test goes through here: the resolver reads the *user's* real
// configuration file when the variable is unset, and a test that read
// it would depend on the machine it runs on.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	t.Setenv(ConfigPathEnvVar, path)
	return path
}

// TestCurrentReturnsTheCompiledInDefaults pins the answer when nothing
// overrides anything.
//
// The defaults are the URLs the clone used before the configuration file
// could say anything. They are the reference's own endpoints, and a
// configuration that does not exist must leave the clone talking to them.
func TestCurrentReturnsTheCompiledInDefaults(t *testing.T) {
	writeConfig(t, `{}`)

	got := Current()
	if got.CloudCode != DefaultCloudCode {
		t.Errorf("CloudCode = %q, want %q", got.CloudCode, DefaultCloudCode)
	}
	if got.CloudCodeProd != DefaultCloudCodeProd {
		t.Errorf("CloudCodeProd = %q, want %q", got.CloudCodeProd, DefaultCloudCodeProd)
	}
	if got.OAuth2Token != DefaultOAuth2Token {
		t.Errorf("OAuth2Token = %q, want %q", got.OAuth2Token, DefaultOAuth2Token)
	}
	if got.OAuth2Auth != DefaultOAuth2Auth {
		t.Errorf("OAuth2Auth = %q, want %q", got.OAuth2Auth, DefaultOAuth2Auth)
	}
	if got.OAuth2Device != DefaultOAuth2Device {
		t.Errorf("OAuth2Device = %q, want %q", got.OAuth2Device, DefaultOAuth2Device)
	}
	if got.OAuthRedirect != DefaultOAuthRedirect {
		t.Errorf("OAuthRedirect = %q, want %q", got.OAuthRedirect, DefaultOAuthRedirect)
	}
	if got.UserInfo != DefaultUserInfo {
		t.Errorf("UserInfo = %q, want %q", got.UserInfo, DefaultUserInfo)
	}
	if got.WebChannel != DefaultWebChannel {
		t.Errorf("WebChannel = %q, want %q", got.WebChannel, DefaultWebChannel)
	}
	if got.GenerativeLanguage != DefaultGenerativeLanguage {
		t.Errorf("GenerativeLanguage = %q, want %q", got.GenerativeLanguage, DefaultGenerativeLanguage)
	}
	if got.CompanionOrigin != DefaultCompanionOrigin {
		t.Errorf("CompanionOrigin = %q, want %q", got.CompanionOrigin, DefaultCompanionOrigin)
	}
	if got.AccountChooser != DefaultAccountChooser {
		t.Errorf("AccountChooser = %q, want %q", got.AccountChooser, DefaultAccountChooser)
	}
	if got.Terms != DefaultTerms {
		t.Errorf("Terms = %q, want %q", got.Terms, DefaultTerms)
	}
	if got.Privacy != DefaultPrivacy {
		t.Errorf("Privacy = %q, want %q", got.Privacy, DefaultPrivacy)
	}
	if got.Marketplace != DefaultMarketplace {
		t.Errorf("Marketplace = %q, want %q", got.Marketplace, DefaultMarketplace)
	}
	if len(got.OriginPatterns) != len(DefaultOriginPatterns) {
		t.Errorf("OriginPatterns = %v, want %v", got.OriginPatterns, DefaultOriginPatterns)
	}
}

// TestAMissingFileIsAnEmptyConfiguration pins that a user who never wrote
// a configuration file gets the defaults — not an error.
func TestAMissingFileIsAnEmptyConfiguration(t *testing.T) {
	t.Setenv(ConfigPathEnvVar, filepath.Join(t.TempDir(), "absent.json"))

	got := Current()
	if got.CloudCode != DefaultCloudCode {
		t.Errorf("CloudCode = %q, want %q", got.CloudCode, DefaultCloudCode)
	}
}

// TestAMalformedFileFallsBackToTheDefaults pins that a broken edit must
// not take the CLI down.
//
// The file is the user's; a half-written one is a mistake, not a reason
// to refuse to start.
func TestAMalformedFileFallsBackToTheDefaults(t *testing.T) {
	writeConfig(t, `{"endpoints": {`)

	got := Current()
	if got.CloudCode != DefaultCloudCode {
		t.Errorf("CloudCode = %q, want %q", got.CloudCode, DefaultCloudCode)
	}
}

// TestTheConfigurationOverridesTheNamedEndpoint pins the core of the
// feature: a named endpoint in the file replaces the compiled-in URL.
func TestTheConfigurationOverridesTheNamedEndpoint(t *testing.T) {
	writeConfig(t, `{
	  "endpoints": {
	    "cloudcode": "http://127.0.0.1:8080",
	    "oauth2_token": "http://127.0.0.1:8080/oauth/token",
	    "companion_origin": "http://localhost:5173"
	  }
	}`)

	got := Current()
	if got.CloudCode != "http://127.0.0.1:8080" {
		t.Errorf("CloudCode = %q, want the configured host", got.CloudCode)
	}
	if got.OAuth2Token != "http://127.0.0.1:8080/oauth/token" {
		t.Errorf("OAuth2Token = %q, want the configured endpoint", got.OAuth2Token)
	}
	if got.CompanionOrigin != "http://localhost:5173" {
		t.Errorf("CompanionOrigin = %q, want the configured origin", got.CompanionOrigin)
	}
	// The ones that were not named keep their defaults.
	if got.UserInfo != DefaultUserInfo {
		t.Errorf("UserInfo = %q, want the default %q", got.UserInfo, DefaultUserInfo)
	}
}

// TestTheAccountChooserURL pins the shape of the link that reaches an
// instance's remote view.
//
// The chooser names the account and carries the whole target in
// "continue", percent-encoded. The parameters come out in the order
// Google's own links carry them ("Email" first) because
// url.Values.Encode sorts the keys.
func TestTheAccountChooserURL(t *testing.T) {
	writeConfig(t, `{}`)

	got := Current().AccountChooserURL("naivgaid@gmail.com",
		"https://antigravity.google.com/r/d4465084-5df7-4817-84ee-355a7801b045-v2")
	want := "https://accounts.google.com/AccountChooser?Email=naivgaid%40gmail.com" +
		"&continue=https%3A%2F%2Fantigravity.google.com%2Fr%2Fd4465084-5df7-4817-84ee-355a7801b045-v2"
	if got != want {
		t.Errorf("AccountChooserURL =\n  %q\nwant\n  %q", got, want)
	}
}

// TestTheAccountChooserURLWithoutAnAccount pins the fallback: a chooser
// that names no account chooses nothing, so the target is the link.
func TestTheAccountChooserURLWithoutAnAccount(t *testing.T) {
	writeConfig(t, `{}`)

	got := Current().AccountChooserURL("", "https://antigravity.google.com/r/abc-v2")
	want := "https://antigravity.google.com/r/abc-v2"
	if got != want {
		t.Errorf("AccountChooserURL(no account) = %q, want %q", got, want)
	}
}

// TestTheAccountChooserURLFollowsTheConfiguration pins that the chooser
// is a named endpoint: the configuration may point it at a local
// service.
func TestTheAccountChooserURLFollowsTheConfiguration(t *testing.T) {
	writeConfig(t, `{"endpoints": {"account_chooser": "http://127.0.0.1:9999/chooser"}}`)

	got := Current().AccountChooserURL("a@b.com", "https://antigravity.google.com/r/abc-v2")
	want := "http://127.0.0.1:9999/chooser?Email=a%40b.com&continue=https%3A%2F%2Fantigravity.google.com%2Fr%2Fabc-v2"
	if got != want {
		t.Errorf("AccountChooserURL =\n  %q\nwant\n  %q", got, want)
	}
}

// TestAnUnknownNameIsIgnored pins that a name the resolver does not know
// is not an error — the file may carry endpoints a newer version of the
// clone knows about.
func TestAnUnknownNameIsIgnored(t *testing.T) {
	writeConfig(t, `{"endpoints": {"a_future_endpoint": "http://127.0.0.1:9999"}}`)

	got := Current()
	if got.CloudCode != DefaultCloudCode {
		t.Errorf("CloudCode = %q, want %q", got.CloudCode, DefaultCloudCode)
	}
}

// TestAnEmptyValueIsNotAnOverride pins that an empty string does not
// blank an endpoint.
//
// A blank endpoint is not a usable one — the caller would dial "" — so
// an empty value is treated as "not configured".
func TestAnEmptyValueIsNotAnOverride(t *testing.T) {
	writeConfig(t, `{"endpoints": {"cloudcode": "   "}}`)

	got := Current()
	if got.CloudCode != DefaultCloudCode {
		t.Errorf("CloudCode = %q, want %q", got.CloudCode, DefaultCloudCode)
	}
}

// TestTheOriginPatternsAreAList pins the list-shaped endpoint: the value
// is comma-separated, and whitespace around each entry is trimmed.
func TestTheOriginPatternsAreAList(t *testing.T) {
	writeConfig(t, `{"endpoints": {"origin_patterns": "http://127.0.0.1:5173, http://localhost:5173"}}`)

	got := Current()
	want := []string{"http://127.0.0.1:5173", "http://localhost:5173"}
	if len(got.OriginPatterns) != len(want) {
		t.Fatalf("OriginPatterns = %v, want %v", got.OriginPatterns, want)
	}
	for i, w := range want {
		if got.OriginPatterns[i] != w {
			t.Errorf("OriginPatterns[%d] = %q, want %q", i, got.OriginPatterns[i], w)
		}
	}
}

// TestAnOriginListWithNoUsableEntryFallsBack pins that an allowlist
// cannot be emptied by accident.
//
// An empty allowlist means "allow every origin" in the remote-control
// host — the one outcome it must never produce because of a typo.
func TestAnOriginListWithNoUsableEntryFallsBack(t *testing.T) {
	writeConfig(t, `{"endpoints": {"origin_patterns": " , , "}}`)

	got := Current()
	if len(got.OriginPatterns) != len(DefaultOriginPatterns) {
		t.Errorf("OriginPatterns = %v, want the defaults %v", got.OriginPatterns, DefaultOriginPatterns)
	}
}

// TestTheDefaultOriginPatternsAreNotShared pins that the resolver hands
// out a copy: a caller that appends to the slice must not corrupt the
// default for the next reader.
func TestTheDefaultOriginPatternsAreNotShared(t *testing.T) {
	writeConfig(t, `{}`)

	first := Current()
	first.OriginPatterns[0] = "mutated"

	second := Current()
	if second.OriginPatterns[0] == "mutated" {
		t.Error("the resolver handed out the default slice itself")
	}
}

// TestWebChannelHostPort pins the host:port derivation the Mendel flag
// "remote-control-proxy-server-url" carries.
func TestWebChannelHostPort(t *testing.T) {
	cases := []struct {
		endpoint string
		want     string
	}{
		{DefaultWebChannel, "jetski-webchannel.googleapis.com:443"},
		{"http://127.0.0.1:9000/v1internal:connectInstanceV2", "127.0.0.1:9000"},
		{"http://127.0.0.1/v1internal:connectInstanceV2", "127.0.0.1:80"},
		{"https://example.test/tunnel", "example.test:443"},
		{"not a url", ""},
		{"", ""},
	}
	for _, tc := range cases {
		s := Set{WebChannel: tc.endpoint}
		if got := s.WebChannelHostPort(); got != tc.want {
			t.Errorf("Set{WebChannel: %q}.WebChannelHostPort() = %q, want %q",
				tc.endpoint, got, tc.want)
		}
	}
}

// TestTheResolverSeesAChangedFile pins the cache's invalidation: the
// configuration is read again once the file changed on disk.
//
// The TUI edits the provider configuration while it runs, so a resolver
// that cached forever would not see the edit.
func TestTheResolverSeesAChangedFile(t *testing.T) {
	path := writeConfig(t, `{"endpoints": {"cloudcode": "http://127.0.0.1:1111"}}`)

	if got := Current().CloudCode; got != "http://127.0.0.1:1111" {
		t.Fatalf("CloudCode = %q, want the configured host", got)
	}

	// A different size, so the file's identity changes even if the
	// modification time does not.
	if err := os.WriteFile(path, []byte(`{"endpoints": {"cloudcode": "http://127.0.0.1:2222"}}`), 0o600); err != nil {
		t.Fatalf("rewrite %s: %v", path, err)
	}

	if got := Current().CloudCode; got != "http://127.0.0.1:2222" {
		t.Errorf("CloudCode = %q, want the rewritten host", got)
	}
}

// TestNamesCoversEveryEndpoint pins that the table a front-end prints is
// the table the resolver fills.
//
// A name that is missing from one of the two is a name a user cannot
// discover.
func TestNamesCoversEveryEndpoint(t *testing.T) {
	writeConfig(t, `{}`)

	seen := map[string]bool{}
	for _, n := range Names() {
		if n.Key == "" {
			t.Error("a name has no key")
		}
		if n.Default == "" {
			t.Errorf("the name %q has no default", n.Key)
		}
		seen[n.Key] = true
	}
	for _, key := range []string{
		CloudCode, CloudCodeProd, OAuth2Token, OAuth2Auth, OAuth2Device,
		OAuthRedirect, UserInfo, WebChannel, GenerativeLanguage,
		CompanionOrigin, OriginPatterns, Terms, Privacy, Marketplace,
	} {
		if !seen[key] {
			t.Errorf("the name %q is not in the table", key)
		}
	}
}
