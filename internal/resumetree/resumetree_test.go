package resumetree

import (
	"testing"
	"time"
)

// TestBaseWorkspaceLabelSkipsGenericSegments pins the label rule: the last
// segment that is not a generic path segment wins.
//
// 【反编译】 model.baseWorkspaceLabel (0x102af5f80).
func TestBaseWorkspaceLabelSkipsGenericSegments(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/Users/gear/GitHub/openagy", "openagy"},
		{"/tmp/agyprobe", "agyprobe"},
		{"/tmp/agyprobe/repo", "agyprobe"},
		{"/a/b/google3", "b"},
		{"/a/b/src", "b"},
		{"/a/b/workspace", "b"},
		{"/a/b/pkg.runfiles", "b"},
		{"/", "/"},
		{"", ""},
	}
	for _, c := range cases {
		if got := BaseWorkspaceLabel(c.path); got != c.want {
			t.Errorf("BaseWorkspaceLabel(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

// TestWorkspaceLabelsRefineCollisions pins the collision rule: two
// workspaces that share a base label grow their label one path segment at a
// time until they differ.
//
// 【一手】 The live reference labels /tmp/agyprobe and /tmp/agyprobe/repo
// both "tmp/agyprobe".
func TestWorkspaceLabelsRefineCollisions(t *testing.T) {
	labels := WorkspaceLabels([]string{
		"/tmp/agyprobe",
		"/tmp/agyprobe/repo",
		"/Users/gear/GitHub/openagy",
	})
	if got := labels["/tmp/agyprobe"]; got != "tmp/agyprobe" {
		t.Errorf("label of /tmp/agyprobe = %q, want %q", got, "tmp/agyprobe")
	}
	if got := labels["/tmp/agyprobe/repo"]; got != "tmp/agyprobe" {
		t.Errorf("label of /tmp/agyprobe/repo = %q, want %q", got, "tmp/agyprobe")
	}
	if got := labels["/Users/gear/GitHub/openagy"]; got != "openagy" {
		t.Errorf("label of the openagy workspace = %q, want %q", got, "openagy")
	}
}

// TestWorkspaceLabelsNoWorkspace pins the label of the group that holds the
// conversations without a workspace.
func TestWorkspaceLabelsNoWorkspace(t *testing.T) {
	labels := WorkspaceLabels([]string{""})
	if got := labels[""]; got != NoWorkspaceLabel {
		t.Errorf("label of the empty workspace = %q, want %q", got, NoWorkspaceLabel)
	}
}

// TestItemWorkspaceURI pins the rule that a conversation belongs to the
// workspace of its first non-empty workspace URI.
//
// 【反编译】 model.itemWorkspaceURI (0x102af67a0).
func TestItemWorkspaceURI(t *testing.T) {
	if got := ItemWorkspaceURI([]string{"", "file:///tmp/agyprobe", "file:///other"}); got != "/tmp/agyprobe" {
		t.Errorf("ItemWorkspaceURI = %q, want %q", got, "/tmp/agyprobe")
	}
	if got := ItemWorkspaceURI(nil); got != "" {
		t.Errorf("ItemWorkspaceURI(nil) = %q, want the empty key", got)
	}
}

// TestNormalizeWorkspaceURIPins the normalisation: a file URI loses its
// scheme, backslashes become slashes and a trailing slash is dropped.
//
// 【反编译】 model.normalizeWorkspaceURI (0x102af5c30).
func TestNormalizeWorkspaceURI(t *testing.T) {
	cases := []struct {
		uri  string
		want string
	}{
		{"", ""},
		{"file:///tmp/agyprobe", "/tmp/agyprobe"},
		{"file:///tmp/agyprobe/", "/tmp/agyprobe"},
		{"/tmp/agyprobe", "/tmp/agyprobe"},
		{"file:///C:\\tmp\\agyprobe", "/C:/tmp/agyprobe"},
	}
	for _, c := range cases {
		if got := NormalizeWorkspaceURI(c.uri); got != c.want {
			t.Errorf("NormalizeWorkspaceURI(%q) = %q, want %q", c.uri, got, c.want)
		}
	}
}

// TestBuildGroupOrder pins the reference's group order: the pinned group
// first, the group without a workspace last, the rest by their newest
// conversation, newest first.
//
// 【反编译】 The comparator lives at 0x102c0d850.
//
// 【一手】 The live reference's picker lists the group of the current
// conversation first, then the other workspaces by their newest
// conversation, and the "[No Workspace]" group last.
func TestBuildGroupOrder(t *testing.T) {
	base := time.Date(2026, 10, 26, 15, 0, 0, 0, time.UTC)
	items := []Item{
		{ID: "a", Time: base, WorkspaceURIs: []string{"file:///Users/gear/GitHub/openagy"}},
		{ID: "b", Time: base.Add(-time.Hour), WorkspaceURIs: []string{"file:///tmp/agyprobe"}},
		{ID: "c", Time: base.Add(-2 * time.Hour)},
		{ID: "d", Time: base.Add(-3 * time.Hour), WorkspaceURIs: []string{"file:///Users/gear"}},
	}
	groups := Build(items, "/Users/gear")
	want := []string{"/Users/gear", "/Users/gear/GitHub/openagy", "/tmp/agyprobe", ""}
	if len(groups) != len(want) {
		t.Fatalf("got %d groups, want %d", len(groups), len(want))
	}
	for i, key := range want {
		if groups[i].Key != key {
			t.Errorf("group %d = %q, want %q", i, groups[i].Key, key)
		}
	}
	if groups[len(groups)-1].Label != NoWorkspaceLabel {
		t.Errorf("the last group is %q, want the no-workspace group", groups[len(groups)-1].Label)
	}
	if groups[0].Label != "gear" {
		t.Errorf("the pinned group is labelled %q, want %q", groups[0].Label, "gear")
	}
}

// TestBuildGroupOrderWithoutPin pins that the group of the current
// conversation only wins when it is pinned.
func TestBuildGroupOrderWithoutPin(t *testing.T) {
	base := time.Date(2026, 10, 26, 15, 0, 0, 0, time.UTC)
	items := []Item{
		{ID: "a", Time: base, WorkspaceURIs: []string{"file:///Users/gear"}},
		{ID: "b", Time: base.Add(-time.Hour), WorkspaceURIs: []string{"file:///tmp/agyprobe"}},
	}
	groups := Build(items, "")
	if groups[0].Key != "/Users/gear" {
		t.Errorf("the first group = %q, want the newest group %q", groups[0].Key, "/Users/gear")
	}
}

// TestBuildSortsItemsNewestFirst pins that a group's conversations are
// listed newest first.
func TestBuildSortsItemsNewestFirst(t *testing.T) {
	base := time.Date(2026, 10, 26, 15, 0, 0, 0, time.UTC)
	items := []Item{
		{ID: "old", Time: base.Add(-time.Hour), WorkspaceURIs: []string{"file:///tmp/agyprobe"}},
		{ID: "new", Time: base, WorkspaceURIs: []string{"file:///tmp/agyprobe"}},
	}
	groups := Build(items, "")
	if len(groups) != 1 || len(groups[0].Items) != 2 {
		t.Fatalf("unexpected groups: %+v", groups)
	}
	if groups[0].Items[0].ID != "new" {
		t.Errorf("the first conversation is %q, want the newest one", groups[0].Items[0].ID)
	}
	if !groups[0].LastActive.Equal(base) {
		t.Errorf("the group's age = %v, want the newest conversation's time", groups[0].LastActive)
	}
}
