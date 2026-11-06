// hidden_surface_test.go — the inventory in
// docs/guide/hidden-commands-and-flags.md, pinned.
//
// The clone's `--help` is the reference's, byte for byte. Everything the
// clone accepts *beyond* that surface has to be hidden, or the help
// stops matching — and a hidden surface is invisible by construction.
// These tests are the other half of that document: they fail when a
// command or a flag is added to, or removed from, the hidden set.
package cli

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// TestHiddenRootSubcommands pins the hidden root subcommands.
func TestHiddenRootSubcommands(t *testing.T) {
	want := []string{
		"auth",
		"chat",
		"hooks",
		"http",
		"login",
		"logo",
		"logout",
		"migrate",
		"remote-control-foreground",
		"stats",
	}
	var got []string
	for _, s := range StandardSubcommands() {
		if s.Hidden {
			got = append(got, s.Name)
		}
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hidden root subcommands = %v\nwant %v\n(the inventory in docs/guide/hidden-commands-and-flags.md)", got, want)
	}
}

// TestHiddenStandardFlags pins the reference's own hidden flags — the
// four local-Chrome CDP flags, the recovered `-v N`, and the two clone
// side channels.
func TestHiddenStandardFlags(t *testing.T) {
	want := []string{
		"cdp_port",
		"local_chrome_headless",
		"local_chrome_user_data_dir",
		"print-partial-response",
		"use_local_chrome",
		"v",
		"verbose",
	}
	var got []string
	for _, f := range StandardFlags() {
		if f.Hidden {
			got = append(got, f.Name)
		}
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hidden standard flags = %v\nwant %v", got, want)
	}
}

// TestHiddenExtraFlags pins the openagy-only hidden flags.
func TestHiddenExtraFlags(t *testing.T) {
	want := []string{
		"alt-screen",
		"backend",
		"debug",
		"desktop",
		"diff-with-agy",
		"experimental-rc-v2",
		"max-thinking-budget",
		"no-alt-screen",
		"remote-control-socket",
		"remote-control-tcp",
		"trust-cwd",
	}
	var got []string
	for _, f := range ExtraFlags() {
		if f.Hidden {
			got = append(got, f.Name)
		}
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hidden extra flags = %v\nwant %v", got, want)
	}
	// Every extra flag is hidden: the clone's extras never reach the
	// reference's `--help` table.
	for _, f := range ExtraFlags() {
		if !f.Hidden {
			t.Errorf("ExtraFlags()[%q].Hidden = false; an openagy extra would leak into --help", f.Name)
		}
	}
}

// TestRemoteControlUsageListsOnlyTheReference pins the remote-control
// usage block: `serve`, `run` and `rename` are accepted, but the
// reference's block lists start, status and stop only — and that block
// is the reference's, byte for byte.
func TestRemoteControlUsageListsOnlyTheReference(t *testing.T) {
	usage := usageRemoteControl("pigasus")
	for _, listed := range []string{"start", "status", "stop"} {
		if !strings.Contains(usage, "\n  "+listed+" ") {
			t.Errorf("remote-control usage does not list %q:\n%s", listed, usage)
		}
	}
	for _, hidden := range []string{"serve", "run", "rename"} {
		if strings.Contains(usage, "\n  "+hidden+" ") {
			t.Errorf("remote-control usage lists %q; the block must stay the reference's", hidden)
		}
	}
}

// TestRemoteControlHiddenSubsStillParse pins that the three
// unadvertised sub-subcommands are registered.
func TestRemoteControlHiddenSubsStillParse(t *testing.T) {
	want := []string{"rename", "run", "serve", "start", "status", "stop"}
	var got []string
	for _, s := range standardRCSubs() {
		got = append(got, s.Name)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("remote-control sub-subcommands = %v\nwant %v", got, want)
	}
}

// TestRCStartUsageHidesTheCloneExtras pins the three flags
// `remote-control start` accepts but does not advertise.
func TestRCStartUsageHidesTheCloneExtras(t *testing.T) {
	usage := usageRCStart("pigasus")
	for _, listed := range []string{"--name", "--session"} {
		if !strings.Contains(usage, listed) {
			t.Errorf("remote-control start usage does not list %q:\n%s", listed, usage)
		}
	}
	for _, hidden := range []string{"--addr", "--log-file", "--desktop"} {
		if strings.Contains(usage, hidden) {
			t.Errorf("remote-control start usage lists %q; the block must stay the reference's", hidden)
		}
	}
}
