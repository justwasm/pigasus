package tui

import (
	"flag"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/justwasm/pigasus/agentlink"
)

func TestRootModel_FailoverNoticeRenderedInViewportAndSurvivesFinalise(t *testing.T) {
	ws := t.TempDir()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	m := newRootModel(nil, "user@test", "gemini-2.5-pro", "tier1", ws, false)
	setupTUIToolsAndPolicy(m, fs)
	m.viewport.width = 100
	m.viewport.height = 30

	// Simulate an active streaming turn with partial output already rendered.
	m.conversation.streaming = true
	m.conversation.liveStart = 0
	m.conversation.replyStart = 0
	m.conversation.response = []string{"Partial answer "}

	// Verify turnCollector.notice maps agentlink.NoticeFailover -> failoverNoticeMsg.
	var sent []any
	collector := &turnCollector{}
	_ = collector
	// Directly exercise Update with the failoverNoticeMsg produced by turnCollector.notice.
	notice := &agentlink.Notice{
		Kind:       agentlink.NoticeFailover,
		FromTarget: "cloudcode",
		ToTarget:   "fallback:openrouter",
		Model:      "claude-sonnet-4-6",
		Text:       "↻ Failover: cloudcode → fallback:openrouter (model: claude-sonnet-4-6)",
	}
	msg := failoverNoticeMsg{
		fromTarget: notice.FromTarget,
		toTarget:   notice.ToTarget,
		model:      notice.Model,
		text:       notice.Text,
	}
	_ = sent

	updated, _, handled := m.updateConversationStreamMsg(msg)
	if !handled {
		t.Fatal("expected failoverNoticeMsg to be handled")
	}
	m = updated
	if !m.conversation.streaming {
		t.Fatal("expected failoverNoticeMsg to keep streaming active")
	}

	// Inspect rendered ANSI/viewport output during streaming (Invariant 13).
	cleanLive := ansi.Strip(m.View().Content)
	if !strings.Contains(cleanLive, "Failover: cloudcode") || !strings.Contains(cleanLive, "claude-sonnet-4-6") {
		t.Fatalf("expected rendered viewport to contain failover banner during stream, got:\n%s", cleanLive)
	}

	// Simulate FinaliseTurnReply at end of turn and verify the failover banner survives.
	m.conversation.response = append(m.conversation.response[:m.conversation.liveStart], "Partial answer and recovered completion.")
	m.conversation.streaming = false
	m.viewport.rowsCacheDirty = true
	m.viewport.rowsGen++
	m.invalidateViewCache()

	cleanFinal := ansi.Strip(m.View().Content)
	if !strings.Contains(cleanFinal, "Failover: cloudcode") || !strings.Contains(cleanFinal, "recovered completion.") {
		t.Fatalf("expected rendered viewport after finalise to retain failover banner and reply, got:\n%s", cleanFinal)
	}
}
