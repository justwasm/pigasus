package atconversation_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/atconversation"
)

func TestAtConversationFacade(t *testing.T) {
	if atconversation.MentionPrefix != "@conversation:" {
		t.Fatalf("unexpected MentionPrefix: %s", atconversation.MentionPrefix)
	}
	ins := atconversation.MentionInsert("123")
	if ins != `@conversation:"123"` {
		t.Fatalf("unexpected MentionInsert: %s", ins)
	}
}
