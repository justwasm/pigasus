package convstore

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// scanSummaries is listFromSummaries plus the set of conversations the index
// marks as hidden (subagent rows). The disk scan needs that set: a subagent
// file that the index filtered out would otherwise be re-added by the
// file walk, which does not see the parent link.
func scanSummaries() ([]ConversationMeta, map[string]bool, error) {
	hidden := map[string]bool{}
	path := filepath.Join(StoreDir(), "conversation_summaries.db")
	if _, err := os.Stat(path); err != nil {
		return nil, hidden, nil
	}
	db, err := Open(path)
	if err != nil {
		if isUnreadableStore(err) {
			return nil, hidden, nil
		}
		return nil, hidden, err
	}
	defer db.Close()

	dir := filepath.Join(StoreDir(), "conversations")
	var out []ConversationMeta
	serr := db.Scan("conversation_summaries", func(r *Row) error {
		if meta, ok := parseSummaryRow(dir, r, hidden); ok {
			out = append(out, meta)
		}
		return nil
	})
	if serr != nil {
		return nil, hidden, serr
	}
	return out, hidden, nil
}

func parseSummaryRow(dir string, r *Row, hidden map[string]bool) (ConversationMeta, bool) {
	steps := int(r.Int("step_count"))
	if steps <= 0 {
		return ConversationMeta{}, false
	}
	// Cycle 282: subagent conversations never enter the reference's list
	// (its scan marks them internal and the picker filters those rows).
	// The index carries the parent link; drop them here too, including
	// on the merged path. Older stores have no such column — Row.Text
	// answers "" for a missing one.
	if r.Text("parent_conversation_id") != "" {
		if id := r.Text("conversation_id"); id != "" {
			hidden[id] = true
		}
		return ConversationMeta{}, false
	}
	id := r.Text("conversation_id")
	if id == "" || strings.ContainsAny(id, `/\`) {
		return ConversationMeta{}, false
	}
	p := filepath.Join(dir, id+".db")
	if _, err := os.Stat(p); err != nil {
		return ConversationMeta{}, false
	}
	return ConversationMeta{
		ID:        id,
		Path:      p,
		Title:     conversationLabel(r.Text("title"), r.Text("preview"), id),
		StepCount: steps,
		Project:   projectFromURIs(r.Text("workspace_uris")),
		ModTime:   parseSummaryTime(r.Text("last_modified_time")),
	}, true
}

// listFilesMissingFromIndex handles the files the two indexes know nothing
// about: a .db written by another client while this store's cache and
// summaries table were both in place. The reference reaches them through
// listConversationsFromDir, which stats every file in the conversations
// directory; this is the same rule, one file at a time.
//
// The title is the annotation file's AI title when the annotation exists
// (annotations/<id>.pbtxt, `title:"..."` — that is what the reference's
// scan reads through cortex.AnnotationsManager), otherwise the first user
// input, exactly like the label chain the picker draws.
func listFilesMissingFromIndex(seen, hidden map[string]bool) ([]ConversationMeta, error) {
	dir := filepath.Join(StoreDir(), "conversations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("convstore: list %s: %w", dir, err)
	}
	var out []ConversationMeta
	for _, e := range entries {
		if meta, ok := peekUnindexedEntry(dir, e, seen, hidden); ok {
			out = append(out, meta)
		}
	}
	return out, nil
}

func peekUnindexedEntry(dir string, e os.DirEntry, seen, hidden map[string]bool) (ConversationMeta, bool) {
	if e.IsDir() {
		return ConversationMeta{}, false
	}
	name := e.Name()
	if !strings.HasSuffix(name, ".db") {
		return ConversationMeta{}, false
	}
	id := strings.TrimSuffix(name, ".db")
	if id == "" || strings.ContainsAny(id, `/\`) || seen[id] || hidden[id] {
		return ConversationMeta{}, false
	}
	path := filepath.Join(dir, name)
	info, ierr := e.Info()
	if ierr != nil {
		return ConversationMeta{}, false
	}
	title, project, steps, lerr := peekConversation(path)
	if lerr != nil || steps == 0 {
		// Unreadable and empty stores are silently omitted, like the
		// reference's scan.
		return ConversationMeta{}, false
	}
	return ConversationMeta{
		ID:        id,
		Path:      path,
		Title:     conversationLabel(annotationTitle(id), title, id),
		StepCount: steps,
		Project:   project,
		ModTime:   info.ModTime(),
	}, true
}

// annotationTitle reads the AI-written title the reference's scan picks up
// from annotations/<id>.pbtxt. Returns "" when the file is absent or the
// payload carries no title.
func annotationTitle(id string) string {
	data, err := os.ReadFile(filepath.Join(StoreDir(), "annotations", id+".pbtxt"))
	if err != nil {
		return ""
	}
	const key = `title:"`
	i := bytes.Index(data, []byte(key))
	if i < 0 {
		return ""
	}
	rest := data[i+len(key):]
	var b strings.Builder
	for j := 0; j < len(rest); j++ {
		c := rest[j]
		if c == '\\' && j+1 < len(rest) {
			j++
			switch rest[j] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			default:
				b.WriteByte(rest[j])
			}
			continue
		}
		if c == '"' {
			break
		}
		b.WriteByte(c)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// listFromDir is the cycle-167 fallback: scan conversations/*.db directly.
// It is used only when the summaries index is absent, and cannot recover the
// project column.
func listFromDir() ([]ConversationMeta, error) {
	dir := filepath.Join(StoreDir(), "conversations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("convstore: list %s: %w", dir, err)
	}
	var out []ConversationMeta
	for _, e := range entries {
		if meta, ok := peekDirEntry(dir, e); ok {
			out = append(out, meta)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ModTime.After(out[j].ModTime)
	})
	return out, nil
}

func peekDirEntry(dir string, e os.DirEntry) (ConversationMeta, bool) {
	if e.IsDir() {
		return ConversationMeta{}, false
	}
	name := e.Name()
	if !strings.HasSuffix(name, ".db") {
		return ConversationMeta{}, false
	}
	// Strip ".db" for the ID; keep the original basename
	// for HasConversation (which expects <id>.db).
	id := strings.TrimSuffix(name, ".db")
	if id == "" || strings.ContainsAny(id, `/\`) {
		return ConversationMeta{}, false
	}
	path := filepath.Join(dir, name)
	info, ierr := e.Info()
	if ierr != nil {
		return ConversationMeta{}, false
	}
	mt := info.ModTime()
	title, project, steps, lerr := peekConversation(path)
	if lerr != nil || steps == 0 {
		// Skip unreadable stores — matches the reference's silent omission.
		// Cycle 184: the reference's picker lists only conversations that actually carry steps.
		return ConversationMeta{}, false
	}
	return ConversationMeta{
		ID:        id,
		Path:      path,
		Title:     title,
		StepCount: steps,
		Project:   project,
		ModTime:   mt,
	}, true
}

// peekConversation opens path just enough to read the first
// user-input step's text (used as the picker title), the workspace
// project name, and the step count. Returns ("", "", 0, nil) for an
// empty or unreadable store — the same conditions LoadConversation
// tolerates.
func peekConversation(path string) (string, string, int, error) {
	c, err := LoadConversation(path)
	if err != nil {
		return "", "", 0, err
	}
	if c == nil || len(c.Steps) == 0 {
		return "", "", 0, nil
	}
	title := ""
	for _, s := range c.Steps {
		if s.Role() == "user" {
			t := strings.TrimSpace(s.Text)
			if t == "" {
				continue
			}
			// First line only, capped at 60 chars to fit the
			// picker row without truncation ellipsis noise.
			if i := strings.IndexAny(t, "\n\r"); i >= 0 {
				t = t[:i]
			}
			t = strings.Join(strings.Fields(t), " ")
			if len(t) > 60 {
				t = t[:57] + "..."
			}
			title = t
			break
		}
	}
	return title, c.Project, len(c.Steps), nil
}
