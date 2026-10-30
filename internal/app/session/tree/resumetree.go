// Package resumetree rebuilds the workspace-grouped conversation tree the
// reference CLI (agy 1.4.8) shows in its /resume picker.
//
// 【反编译】 The rule was recovered from the reference binary's
// `google3/third_party/jetski/cli/model` package:
//
//	model.normalizeWorkspaceURI  0x102af5c30
//	model.workspaceSegments      0x102af5d00
//	model.isGenericPathSegment   0x102af5e80
//	model.baseWorkspaceLabel     0x102af5f80
//	model.joinLabelSegments      0x102af6070
//	model.workspaceLabels        0x102af6130
//	model.itemWorkspaceURI       0x102af67a0
//	model.groupConversations     0x102af68c0
//
// A conversation belongs to the workspace of its *first* non-empty workspace
// URI. Conversations with no workspace at all share the synthetic
// "[No Workspace]" group.
//
// The label of a group is the last path segment of its workspace that is not
// a "generic" segment (google3, src, repo, code, work, workspace, packages,
// projects, or anything ending in ".runfiles"). Two workspaces that share a
// base label are refined: the label grows one path segment at a time until
// the two labels differ.
//
// 【一手】 The live reference's picker confirms the labels, the group order
// and the cell layout; see docs/cycles/cycle-336-resume-workspace-tree/.
package tree

import (
	"sort"
	"strings"
	"time"
)

// NoWorkspaceLabel is the label the reference gives the group that holds
// the conversations that ran without a workspace.
//
// 【反编译】 The string lives at 0x10349ecdc in the reference binary and is
// passed to model.groupConversations as the empty group's label.
const NoWorkspaceLabel = "[No Workspace]"

// genericPathSegments are the path segments the reference skips when it
// picks a workspace's base label.
//
// 【反编译】 model.isGenericPathSegment (0x102af5e80).
var genericPathSegments = map[string]bool{
	"google3":   true,
	"src":       true,
	"repo":      true,
	"code":      true,
	"work":      true,
	"workspace": true,
	"packages":  true,
	"projects":  true,
}

// runfilesSuffix marks a Bazel runfiles directory, which is generic too.
const runfilesSuffix = ".runfiles"

// Item is one conversation in the tree.
type Item struct {
	// ID is the conversation id — the basename of its .db file.
	ID string
	// Title is the row label the picker prints.
	Title string
	// Steps is the conversation's step count ("17 steps").
	Steps int
	// Time is the conversation's last-modified time; the picker prints it
	// as the row's age and the group's age.
	Time time.Time
	// WorkspaceURIs are the workspaces the conversation ran in, in the
	// order the store lists them.
	WorkspaceURIs []string
}

// Group is one workspace group of the tree.
type Group struct {
	// Key is the group's normalised workspace URI. The group without a
	// workspace has the empty key.
	Key string
	// Label is the group's display label ("openagy", "tmp/agyprobe",
	// "[No Workspace]").
	Label string
	// Items are the group's conversations, newest first.
	Items []Item
	// LastActive is the newest item's time — the group's age.
	LastActive time.Time
}

// URIToLocalPath turns a workspace URI into a local path.
//
// 【反编译】 The reference calls types.URIToLocalPath; the subset the picker
// needs is "file://<path>" plus a bare path.
func URIToLocalPath(uri string) string {
	if rest, ok := strings.CutPrefix(uri, "file://"); ok {
		if strings.HasPrefix(rest, "/") {
			return rest
		}
		if i := strings.Index(rest, "/"); i >= 0 {
			// file://<host>/<path> — the host is not part of the path.
			return rest[i:]
		}
		return "/" + rest
	}
	if i := strings.Index(uri, "://"); i >= 0 {
		return uri[i+3:]
	}
	return uri
}

// NormalizeWorkspaceURI turns a workspace URI into the path the reference
// groups by.
//
// 【反编译】 model.normalizeWorkspaceURI (0x102af5c30): the empty URI stays
// empty (that is the "[No Workspace]" group), backslashes become slashes and
// a trailing slash is dropped.
func NormalizeWorkspaceURI(uri string) string {
	if uri == "" {
		return ""
	}
	path := strings.ReplaceAll(URIToLocalPath(uri), "\\", "/")
	path = strings.TrimRight(path, "/")
	if path == "" {
		return "/"
	}
	return path
}

// WorkspaceSegments splits a path into its non-empty segments.
//
// 【反编译】 model.workspaceSegments (0x102af5d00).
func WorkspaceSegments(path string) []string {
	var out []string
	for _, seg := range strings.Split(path, "/") {
		if seg != "" {
			out = append(out, seg)
		}
	}
	return out
}

// IsGenericPathSegment reports whether a path segment is one the reference
// ignores when it picks a base label.
//
// 【反编译】 model.isGenericPathSegment (0x102af5e80).
func IsGenericPathSegment(seg string) bool {
	low := strings.ToLower(seg)
	if genericPathSegments[low] {
		return true
	}
	return strings.HasSuffix(low, runfilesSuffix)
}

// BaseWorkspaceLabel is a path's label before any collision refinement.
//
// 【反编译】 model.baseWorkspaceLabel (0x102af5f80): the last segment that is
// not generic, else the last segment, else "/" for the root, else "".
func BaseWorkspaceLabel(path string) string {
	segs := WorkspaceSegments(path)
	for i := len(segs) - 1; i >= 0; i-- {
		if !IsGenericPathSegment(segs[i]) {
			return segs[i]
		}
	}
	if len(segs) > 0 {
		return segs[len(segs)-1]
	}
	if path == "/" {
		return "/"
	}
	return ""
}

// JoinLabelSegments joins path segments into one label.
//
// 【反编译】 model.joinLabelSegments (0x102af6070): up to three segments are
// joined as they are; a longer window is shortened to its last two segments
// with a "/…/" marker.
//
// 【存疑】 The "/…/" marker was read out of the disassembly but never
// observed in the reference's own output — no workspace in the live store
// needs more than three segments.
func JoinLabelSegments(segs []string, count int) string {
	if count > len(segs) {
		count = len(segs)
	}
	if count <= 0 {
		return ""
	}
	window := segs[:count]
	if count <= 3 {
		return strings.Join(window, "/")
	}
	return strings.Join(window[len(window)-2:], "/") + "/…/"
}

// WorkspaceLabels maps every workspace path to its display label.
//
// 【反编译】 model.workspaceLabels (0x102af6130). Paths that share a base
// label are refined together: at level L the label of a path is the window
// of segments that ends at its base segment and reaches L segments back. The
// level grows while two paths still produce the same label.
func WorkspaceLabels(paths []string) map[string]string {
	labels := make(map[string]string, len(paths))
	byBase := make(map[string][]string, len(paths))
	for _, path := range paths {
		if path == "" {
			labels[path] = NoWorkspaceLabel
			continue
		}
		base := BaseWorkspaceLabel(path)
		if base == "" {
			labels[path] = NoWorkspaceLabel
			continue
		}
		labels[path] = base
		byBase[base] = append(byBase[base], path)
	}
	for base, group := range byBase {
		if len(group) < 2 {
			continue
		}
		for level := 1; ; level++ {
			counts := make(map[string]int, len(group))
			grow := false
			for _, path := range group {
				segs := WorkspaceSegments(path)
				baseIdx := len(segs) - 1
				for i := len(segs) - 1; i >= 0; i-- {
					if segs[i] == base {
						baseIdx = i
						break
					}
				}
				drop := baseIdx - level
				if drop < 0 {
					drop = 0
				} else {
					grow = true
				}
				label := JoinLabelSegments(segs[drop:], baseIdx-drop+1)
				labels[path] = label
				counts[label]++
			}
			if !grow {
				break
			}
			collide := false
			for _, n := range counts {
				if n > 1 {
					collide = true
					break
				}
			}
			if !collide {
				break
			}
		}
	}
	return labels
}

// ItemWorkspaceURI is the workspace a conversation belongs to: its first
// non-empty workspace URI, normalised.
//
// 【反编译】 model.itemWorkspaceURI (0x102af67a0).
func ItemWorkspaceURI(uris []string) string {
	for _, uri := range uris {
		if uri == "" {
			continue
		}
		return NormalizeWorkspaceURI(uri)
	}
	return ""
}

// Build groups the conversations into the reference's workspace tree.
//
// The groups come back in the reference's order: the group without a
// workspace is last, the rest by their newest conversation, newest first,
// with the label as the tie-breaker.
//
// 【反编译】 The comparator lives at 0x102c0d850: the captured "[No
// Workspace]" label and the empty key both sort last, then the groups are
// ordered by LastActive descending, then by label ascending.
//
// 【存疑】 The reference's live picker keeps one group — the one that holds
// the current conversation — at the top whatever its timestamps say; that
// pinning could not be tied to a field of the reference's own group struct.
// Build takes the pinned key as an argument so the caller can reproduce it;
// an unknown key is ignored.
func Build(items []Item, pinnedKey string) []Group {
	order := make([]string, 0, len(items))
	groups := make(map[string]*Group, len(items))
	for _, item := range items {
		key := ItemWorkspaceURI(item.WorkspaceURIs)
		g, ok := groups[key]
		if !ok {
			g = &Group{Key: key}
			groups[key] = g
			order = append(order, key)
		}
		g.Items = append(g.Items, item)
		if item.Time.After(g.LastActive) {
			g.LastActive = item.Time
		}
	}
	paths := make([]string, 0, len(order))
	for _, key := range order {
		paths = append(paths, key)
	}
	labels := WorkspaceLabels(paths)
	out := make([]Group, 0, len(order))
	for _, key := range order {
		g := groups[key]
		g.Label = labels[key]
		// The reference lists a group's conversations newest first.
		sort.SliceStable(g.Items, func(i, j int) bool {
			return g.Items[i].Time.After(g.Items[j].Time)
		})
		out = append(out, *g)
	}
	sortGroups(out, pinnedKey)
	return out
}

// sortGroups puts the groups in the reference's order.
func sortGroups(groups []Group, pinnedKey string) {
	sort.SliceStable(groups, func(i, j int) bool {
		a, b := groups[i], groups[j]
		if pinnedKey != "" && (a.Key == pinnedKey) != (b.Key == pinnedKey) {
			return a.Key == pinnedKey
		}
		if (a.Key == "") != (b.Key == "") {
			// The group without a workspace sorts last.
			return b.Key == ""
		}
		if !a.LastActive.Equal(b.LastActive) {
			return a.LastActive.After(b.LastActive)
		}
		return a.Label < b.Label
	})
}
