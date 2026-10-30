package agentmessage_test

import (
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/agentmessage"
)

func TestAgentmessageFacade(t *testing.T) {
	out := agentmessage.Render("sender1", "content1", time.Now(), "")
	if !strings.Contains(out, "<SYSTEM_MESSAGE>") {
		t.Fatalf("unexpected Render output: %s", out)
	}
}
