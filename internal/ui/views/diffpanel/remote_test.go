package diffpanel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestRemoteSeamsOverrideLocalGit pins the remote session's data path: the
// panel reads the host's changes and body instead of the local repository,
// and names the source "remote".
func TestRemoteSeamsOverrideLocalGit(t *testing.T) {
	p := New(Seams{
		Cwd:   func() string { return "" },
		Size:  func() (int, int) { return 0, 0 },
		VCS:   func() string { return "remote" },
		Close: func() tea.Cmd { return nil },
		Changes: func() (tracked, untracked []Change, ok bool) {
			return []Change{{Path: "remote.go", Added: 2, Deleted: 1}}, nil, true
		},
		Body: func(target, kind string) ([]string, bool) {
			if target != "remote.go" || kind != "file" {
				t.Errorf("body(%q, %q)", target, kind)
			}
			return []string{"@@ -1 +1 @@", "-a", "+b"}, true
		},
	})

	if got := p.vcsName(); got != "remote" {
		t.Errorf("vcsName = %q", got)
	}
	tracked, untracked, ok := p.changes()
	if !ok || len(tracked) != 1 || tracked[0].Path != "remote.go" || tracked[0].Added != 2 || tracked[0].Deleted != 1 {
		t.Errorf("changes = %+v / %+v (ok=%v)", tracked, untracked, ok)
	}
	if body := p.body("remote.go", "file"); len(body) != 3 {
		t.Errorf("body = %v", body)
	}
	if got := p.commits(); got != nil {
		t.Errorf("remote panel exposed local commits: %+v", got)
	}

	raw := p.Lines()
	plain := make([]string, len(raw))
	for i, line := range raw {
		plain[i] = stripSGR(line)
	}
	joined := strings.Join(plain, "\n")
	if !strings.Contains(joined, "remote.go") || !strings.Contains(joined, "+2") || !strings.Contains(joined, "-1") {
		t.Errorf("rows do not show the remote change:\n%s", joined)
	}
	if !strings.Contains(joined, "remote") {
		t.Errorf("header does not name the remote VCS:\n%s", joined)
	}
}
