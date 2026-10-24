// Package convstore provides conversation loading, workspace lookup, and cache parsing.
package convstore

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Default history caps when loading a conversation.
const (
	DefaultHistoryTurns = 40
	DefaultHistoryChars = 100_000
)

// Metadata models the ~/.gemini/antigravity-cli/cache/conversation_metadata.json structure.
type Metadata struct {
	Conversations map[string]struct {
		Summary struct {
			ID            string   `json:"ID"`
			Title         string   `json:"Title"`
			Preview       string   `json:"Preview"`
			NumSteps      int      `json:"NumSteps"`
			UpdatedAt     string   `json:"UpdatedAt"`
			WorkspaceURIs []string `json:"WorkspaceURIs"`
		} `json:"summary"`
		LastModifiedTime string `json:"last_modified_time"`
	} `json:"conversations"`
}

// Selection is the outcome of resolving --conversation / -c.
type Selection struct {
	// ID is the conversation the run continues, or "" for a fresh one.
	ID string
	// Source records how it was chosen: "flag", "continue", or "".
	Source string
	// Store is the loaded conversation, or nil when nothing was loaded.
	Store *Conversation
	// History is the model context to replay before the new prompt.
	History []ChatTurn
	// PriorUserTurns counts the user turns already in the conversation.
	PriorUserTurns int
}

// LoadLastConversations reads cache/last_conversations.json from StoreDir.
func LoadLastConversations() map[string]string {
	b, err := os.ReadFile(filepath.Join(StoreDir(), "cache", "last_conversations.json"))
	if err != nil {
		return nil
	}
	var m map[string]string
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	return m
}

// LoadMetadata reads cache/conversation_metadata.json from StoreDir.
func LoadMetadata() *Metadata {
	b, err := os.ReadFile(filepath.Join(StoreDir(), "cache", "conversation_metadata.json"))
	if err != nil {
		return nil
	}
	var m Metadata
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	return &m
}

// MostRecentConversation picks the conversation `-c` would continue from dir.
//
// Order: the recorded last conversation for dir or its nearest ancestor, then
// the most recently modified conversation whose workspace is dir or one of its
// ancestors. The ancestor rule is what makes a fresh subdirectory continue the
// conversation that owns its workspace.
func MostRecentConversation(dir string) string {
	if dir == "" {
		return ""
	}
	if id := LookupLastConversation(dir); id != "" {
		return id
	}
	return LookupMetadataConversation(dir)
}

// LookupLastConversation checks last_conversations.json for dir and ancestors.
func LookupLastConversation(dir string) string {
	last := LoadLastConversations()
	if len(last) == 0 {
		return ""
	}
	for d := filepath.Clean(dir); ; {
		if id := last[d]; id != "" && HasConversation(id) {
			return id
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ""
		}
		d = parent
	}
}

// LookupMetadataConversation checks conversation_metadata.json for dir and ancestors.
func LookupMetadataConversation(dir string) string {
	meta := LoadMetadata()
	if meta == nil {
		return ""
	}
	for d := filepath.Clean(dir); ; {
		uri := "file://" + d
		bestID, bestTime := "", ""
		for id, entry := range meta.Conversations {
			if !hasString(entry.Summary.WorkspaceURIs, uri) || !HasConversation(id) {
				continue
			}
			if entry.LastModifiedTime > bestTime {
				bestID, bestTime = id, entry.LastModifiedTime
			}
		}
		if bestID != "" {
			return bestID
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ""
		}
		d = parent
	}
}

// ResolveSelection decides what a run continues, printing warning on warn
// when an explicitly named conversation is missing.
func ResolveSelection(convID string, continueFlag bool, cwd string, warn io.Writer) Selection {
	if id := strings.TrimSpace(convID); id != "" {
		if path := FindConversation(id); path != "" {
			return LoadSelection(id, "flag", path)
		}
		fmt.Fprintf(warn, "warning: conversation %q not found\n", id)
		return Selection{}
	}
	if continueFlag {
		return resolveContinueSelection(cwd)
	}
	return Selection{}
}

func resolveContinueSelection(cwd string) Selection {
	if id := MostRecentConversation(cwd); id != "" {
		return LoadSelection(id, "continue", ConversationPath(id))
	}
	if id := MostRecentInWorkspace(cwd); id != "" {
		return LoadSelection(id, "continue", ConversationPath(id))
	}
	cand, ferr := FindMostRecent(cwd, ContinueParentMaxDepth)
	if ferr == nil && cand != nil {
		if id := MostRecentInWorkspace(cand.Dir); id != "" {
			return LoadSelection(id, "continue", ConversationPath(id))
		}
	}
	return Selection{}
}

// LoadSelection loads a conversation from path and sets up history budget.
func LoadSelection(id, source, path string) Selection {
	sel := Selection{ID: id, Source: source}
	c, err := LoadConversation(path)
	if err != nil {
		return sel
	}
	sel.Store = c
	sel.History = c.LastChatTurnsBudget(DefaultHistoryTurns, DefaultHistoryChars)
	for _, t := range c.ChatTurns() {
		if t.Role == "user" {
			sel.PriorUserTurns++
		}
	}
	return sel
}

func hasString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
