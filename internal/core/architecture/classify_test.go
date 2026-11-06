package architecture

import (
	"os"
	"path/filepath"
	"testing"
)

// TestTierOf pins the tier table: the five prefixes, the packages under
// them, and the packages that belong to no tier at all.
func TestTierOf(t *testing.T) {
	cases := []struct {
		pkg  string
		want int
	}{
		{"internal/core", 1},
		{"internal/core/tokenfmt", 1},
		{"internal/core/architecture", 1},
		{"internal/features", 2},
		{"internal/features/models/catalog", 2},
		{"internal/infra", 3},
		{"internal/infra/backend/cloudcode", 3},
		{"internal/app", 4},
		{"internal/app/agentturn", 4},
		{"internal/ui", 5},
		{"internal/ui/views/helppanel", 5},
		// A host is not a tier, and neither is a public package.
		{"internal/tui", 0},
		{"internal/printmode", 0},
		{"internal", 0},
		{"internal/modular/engines", 0},
		{"render", 0},
		{"cmd/pigasus", 0},
		{"", 0},
	}
	for _, c := range cases {
		if got := tierOf(c.pkg); got != c.want {
			t.Errorf("tierOf(%q) = %d, want %d", c.pkg, got, c.want)
		}
	}
}

// TestIsHost pins the host set: the entry hosts, the packages
// under them, and the packages that are not hosts.
func TestIsHost(t *testing.T) {
	cases := []struct {
		pkg  string
		want bool
	}{
		{"internal/modular", true},
		{"internal/modular/engines/stream", true},
		{"internal/cli", true},
		{"internal/clirunner", true},
		{"internal/tui", true},
		{"internal/printmode", true},
		{"internal/core", false},
		{"internal/app/agentturn", false},
		{"render", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isHost(c.pkg); got != c.want {
			t.Errorf("isHost(%q) = %v, want %v", c.pkg, got, c.want)
		}
	}
}

// TestIsEngineHost pins the hosts the app tier may reach.
func TestIsEngineHost(t *testing.T) {
	cases := []struct {
		pkg  string
		want bool
	}{
		{"internal/modular", true},
		{"internal/cli", true},
		{"internal/cliapp", false},
		{"internal/printmode", false},
		{"internal/app/agentturn", false},
	}
	for _, c := range cases {
		if got := isEngineHost(c.pkg); got != c.want {
			t.Errorf("isEngineHost(%q) = %v, want %v", c.pkg, got, c.want)
		}
	}
}

// TestIsEntryHost pins the hosts that sit above every tier.
func TestIsEntryHost(t *testing.T) {
	cases := []struct {
		pkg  string
		want bool
	}{
		{"internal/clirunner", true},
		{"internal/tui", true},
		{"internal/printmode", true},
		{"internal/cli", false},
		{"internal/core", false},
	}
	for _, c := range cases {
		if got := isEntryHost(c.pkg); got != c.want {
			t.Errorf("isEntryHost(%q) = %v, want %v", c.pkg, got, c.want)
		}
	}
}

// TestIsPublic pins what counts as a public package: a package at the
// module's root, outside internal/ and cmd/.
func TestIsPublic(t *testing.T) {
	cases := []struct {
		pkg  string
		want bool
	}{
		{"render", true},
		{"palette", true},
		{"remotetransport", true},
		{"agentlink", true},
		{"internal", false},
		{"cmd", false},
		{"internal/core", false},
		{"cmd/pigasus", false},
		{"devutils/browserdemo", false},
		{"docs/cycles/cycle-266/repro", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isPublic(c.pkg); got != c.want {
			t.Errorf("isPublic(%q) = %v, want %v", c.pkg, got, c.want)
		}
	}
}

// TestFindModuleRoot pins the module-root discovery: the directory that
// holds the go.mod above a starting directory.
func TestFindModuleRoot(t *testing.T) {
	root := findModuleRoot(".")
	if root == "" {
		t.Fatal("findModuleRoot(\".\") found no go.mod")
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("findModuleRoot(\".\") = %q, which holds no go.mod: %v", root, err)
	}
	if nested := findModuleRoot(root + "/does-not-exist/deeper"); nested != root {
		t.Errorf("findModuleRoot on a missing directory = %q, want %q", nested, root)
	}
}
