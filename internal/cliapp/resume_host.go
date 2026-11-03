// resume_host.go — the /resume picker's seam into the session.
//
// The picker owns its own state and its own rendering; it asks the session
// for the conversations and for the two actions it triggers (resume, rename).
// These are the answers it gets.

package cliapp

import (
	"github.com/justwasm/pigasus/internal/app/session/convstore"
)

// listResumeConversations returns the store's conversations, minus the
// session's own.
//
// The session's own conversation is not offered: the picker is the way back
// to a *different* conversation.
func (m *runMode) listResumeConversations() ([]convstore.ConversationMeta, error) {
	list, err := convstore.ListConversations()
	if err != nil {
		return nil, err
	}
	if m.conversationID == "" {
		return list, nil
	}
	filtered := list[:0]
	for _, c := range list {
		if c.ID != m.conversationID {
			filtered = append(filtered, c)
		}
	}
	return filtered, nil
}

// renameResumeConversation opens the rename panel for the conversation the
// user picked.
func (m *runMode) renameResumeConversation(meta convstore.ConversationMeta) {
	m.conversationName = meta.Title
	m.openRenamePanel()
}
