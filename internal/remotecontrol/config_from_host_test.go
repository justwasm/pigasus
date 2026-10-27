// config_from_host_test.go — the HostConfig → Config translation must not
// drop a field.
//
// Cycle 346: two translations existed. The Unix-socket host used the
// package-private one in host_helpers.go; the TCP host
// (internal/rchost, `--remote-control-tcp`) used its own, shorter copy.
// Each copy dropped something the other kept:
//
//   - the package-private one dropped `Browser` — the agent↔browser
//     bridge — so the Unix-socket daemon answered the three
//     agent-browser RPCs with a nil broker;
//   - the rchost one dropped `WebTerminals` and `WebAudio` — the Web
//     UI's terminal and voice surfaces — so the TCP daemon answered
//     CreateTerminal with `-32603 "terminals are not wired"` and the
//     voice stream with silence.
//
// There is now one translation (remotecontrol.ConfigFromHost) and this
// test pins the fields that were lost.
package remotecontrol

import (
	"testing"

	"github.com/justwasm/pigasus/agentbrowser"
	"github.com/justwasm/pigasus/internal/rcaudio"
	"github.com/justwasm/pigasus/internal/rcterminal"
)

func TestConfigFromHost_CarriesTheDroppedFields(t *testing.T) {
	broker := agentbrowser.NewBroker()
	terminals := rcterminal.NewRegistry()
	audio := rcaudio.NewRegistry(nil)

	hc := HostConfig{
		Commit:           "abc123",
		BuildLabel:       "build-1",
		Browser:          broker,
		WebTerminals:     terminals,
		WebAudio:         audio,
		PermissionsPath:  "/tmp/permissions.json",
		PluginsPath:      "/tmp/plugins.json",
		TriggersPath:     "/tmp/triggers.json",
		FastpushPath:     "/tmp/fastpush.json",
		ResourcePath:     "/tmp/resources.json",
		OriginPolicyPath: "/tmp/origin_policy.json",
	}

	cfg := ConfigFromHost(hc, "/tmp/remote-control.sock")

	if cfg.Browser != broker {
		t.Errorf("Browser = %v, want the host's broker", cfg.Browser)
	}
	if cfg.WebTerminals != terminals {
		t.Errorf("WebTerminals = %v, want the host's registry", cfg.WebTerminals)
	}
	if cfg.WebAudio != audio {
		t.Errorf("WebAudio = %v, want the host's registry", cfg.WebAudio)
	}
	if cfg.Commit != "abc123" {
		t.Errorf("Commit = %q, want %q", cfg.Commit, "abc123")
	}
	if cfg.BuildLabel != "build-1" {
		t.Errorf("BuildLabel = %q, want %q", cfg.BuildLabel, "build-1")
	}
	for _, tc := range []struct {
		name string
		got  string
		want string
	}{
		{"PermissionsPath", cfg.PermissionsPath, "/tmp/permissions.json"},
		{"PluginsPath", cfg.PluginsPath, "/tmp/plugins.json"},
		{"TriggersPath", cfg.TriggersPath, "/tmp/triggers.json"},
		{"FastpushPath", cfg.FastpushPath, "/tmp/fastpush.json"},
		{"ResourcePath", cfg.ResourcePath, "/tmp/resources.json"},
		{"OriginPolicyPath", cfg.OriginPolicyPath, "/tmp/origin_policy.json"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
	if cfg.SocketPath != "/tmp/remote-control.sock" {
		t.Errorf("SocketPath = %q, want the socket the caller passed", cfg.SocketPath)
	}
	// The TCP host passes no socket; the dispatcher must not invent one.
	if got := ConfigFromHost(hc, "").SocketPath; got != "" {
		t.Errorf("SocketPath (no socket asked for) = %q, want empty", got)
	}
}
