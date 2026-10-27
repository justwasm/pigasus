package lsserver

import (
	"flag"
	"strings"
	"testing"
)

// The reference's `--help` carries 68 flags. The count is the
// contract: a flag added or dropped on the clone's side changes the
// help the IDE's operator reads.
//
// 【一手】 captured from the reference binary (out/langserver/help.txt).
const referenceFlagCount = 68

// newFlagSet builds a flag set with the reference's flags on it.
func newFlagSet(t *testing.T) (*flag.FlagSet, *Flags) {
	t.Helper()
	fs := flag.NewFlagSet("language_server", flag.ContinueOnError)
	fs.SetOutput(&strings.Builder{})
	return fs, Register(fs)
}

// TestFlagSurfaceIsTheReferenceSurface checks the flag count and the
// names the reference's help carries.
func TestFlagSurfaceIsTheReferenceSurface(t *testing.T) {
	fs, _ := newFlagSet(t)
	names := map[string]bool{}
	fs.VisitAll(func(f *flag.Flag) { names[f.Name] = true })
	if len(names) != referenceFlagCount {
		t.Fatalf("flag count = %d, want %d", len(names), referenceFlagCount)
	}
	for _, want := range []string{
		"cdp_port", "config_dir", "csrf_token", "enable_lsp", "gemini_dir",
		"http_server_port", "https_server_port", "lsp_port", "mock_fixture",
		"model_api_client_type", "override_ide_version", "persistent_mode",
		"sandbox_mode", "standalone", "stamp", "subclient_type",
		"use_mocked_data", "web_bundle_path", "workspace_id",
	} {
		if !names[want] {
			t.Errorf("flag %q is missing", want)
		}
	}
}

// TestHiddenFlagIsNotInTheHelp checks that the flag the IDE passes
// and the reference does not list stays out of the help.
func TestHiddenFlagIsNotInTheHelp(t *testing.T) {
	fs, _ := newFlagSet(t)
	var out strings.Builder
	PrintUsage(fs, &out, "")
	if strings.Contains(out.String(), "enable_sidecars") {
		t.Error("the help lists enable_sidecars, the reference's does not")
	}
}

// TestTheIDEsHiddenFlagsAreAccepted checks the three flags the IDE
// passes and the reference's help does not list.
//
// 【一手】 the live IDE's own spawn line carries all three
// (out/langserver/spawn-args.txt).
func TestTheIDEsHiddenFlagsAreAccepted(t *testing.T) {
	fs, _ := newFlagSet(t)
	args := []string{
		"--enable_sidecars",
		"--host_bridge_url=http://127.0.0.1:56307",
		"--host_bridge_token=f2205057",
		"--stamp",
	}
	got := InspectArgs(fs, args)
	if got.Complaint != "" {
		t.Fatalf("InspectArgs(%v) complained: %q", args, got.Complaint)
	}
	if len(got.Rest) != 1 || got.Rest[0] != "--stamp" {
		t.Errorf("Rest = %v, want [--stamp]", got.Rest)
	}
	if got.Hidden["enable_sidecars"] != "true" {
		t.Errorf("enable_sidecars = %q", got.Hidden["enable_sidecars"])
	}
	if got.Hidden["host_bridge_url"] != "http://127.0.0.1:56307" {
		t.Errorf("host_bridge_url = %q", got.Hidden["host_bridge_url"])
	}
	if got.Hidden["host_bridge_token"] != "f2205057" {
		t.Errorf("host_bridge_token = %q", got.Hidden["host_bridge_token"])
	}
}

// TestTheHiddenFlagsFollowTheReferenceRules pins the rules the
// reference's hidden flags follow.
//
// 【一手】 each case was observed against the reference binary:
//
//   - a boolean hidden flag takes no value: "--enable_sidecars true"
//     leaves "true" as a positional argument and stops the scan;
//   - a string hidden flag takes the next argument: "--host_bridge_url
//     http://x" consumes it, and "--host_bridge_url" alone needs an
//     argument;
//   - a boolean value that does not parse is the reference's
//     complaint, reported at the flag's position;
//   - a hidden flag behind a positional argument is never seen.
func TestTheHiddenFlagsFollowTheReferenceRules(t *testing.T) {
	cases := []struct {
		name      string
		args      []string
		complaint string
		hidden    map[string]string
	}{
		{
			name:   "a boolean hidden flag takes no value",
			args:   []string{"--enable_sidecars", "true", "--stamp"},
			hidden: map[string]string{"enable_sidecars": "true"},
		},
		{
			name:   "a string hidden flag takes the next argument",
			args:   []string{"--host_bridge_url", "http://127.0.0.1:1", "--stamp"},
			hidden: map[string]string{"host_bridge_url": "http://127.0.0.1:1"},
		},
		{
			name:      "a string hidden flag needs an argument",
			args:      []string{"--host_bridge_url"},
			complaint: "flag needs an argument: -host_bridge_url",
		},
		{
			name:      "a boolean hidden flag's value is checked",
			args:      []string{"--enable_sidecars=maybe", "--stamp"},
			complaint: `invalid boolean value "maybe" for  -enable_sidecars: strconv.ParseBool: parsing "maybe": invalid syntax`,
		},
		{
			name:   "a hidden flag behind a positional argument is never seen",
			args:   []string{"foo", "--enable_sidecars=maybe"},
			hidden: map[string]string{},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs, _ := newFlagSet(t)
			got := InspectArgs(fs, c.args)
			if got.Complaint != c.complaint {
				t.Errorf("complaint = %q, want %q", got.Complaint, c.complaint)
			}
			for name, want := range c.hidden {
				if got.Hidden[name] != want {
					t.Errorf("hidden %q = %q, want %q", name, got.Hidden[name], want)
				}
			}
		})
	}
}

// TestPrintDefaultsIsTheReferenceFormat checks the one-line format
// the reference uses and the quoting rule behind it.
func TestPrintDefaultsIsTheReferenceFormat(t *testing.T) {
	fs, _ := newFlagSet(t)
	var out strings.Builder
	PrintDefaults(fs, &out)
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != referenceFlagCount {
		t.Fatalf("printed %d lines, want %d", len(lines), referenceFlagCount)
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "  -") {
			t.Errorf("line %q does not start with two spaces and a dash", line)
		}
		if !strings.Contains(line, ": ") {
			t.Errorf("line %q carries no usage", line)
		}
	}
	want := []string{
		`  -analytics_server_url="": Analytics server host`,
		`  -cdp_port=9222: Port for Chrome DevTools Protocol`,
		`  -model_api_client_type=ccpa: Which model client to use: ccpa or gemini. Defaults to ccpa.`,
	}
	for _, w := range want {
		if !strings.Contains(out.String(), w+"\n") {
			t.Errorf("the flag list does not carry %q", w)
		}
	}
}
