package agentturn

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/core/apierror"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/features/models/modelctx"
	"github.com/justwasm/pigasus/internal/infra/backend/failover"
)

type stubBackendClient struct {
	fn func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error)
}

func (s *stubBackendClient) Stream(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
	return s.fn(ctx, req)
}

func TestLocalTurn_MidStreamFailoverUpdatesPolicyAndEmitsNotice(t *testing.T) {
	ht := failover.NewHealthTracker()

	var mu sync.Mutex
	var secondaryReqs []backend.StreamRequest

	primary := &stubBackendClient{
		fn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			ch := make(chan backend.StreamChunk, 2)
			ch <- backend.StreamChunk{Text: "Partial answer "}
			ch <- backend.StreamChunk{
				Error: &apierror.HTTPError{
					StatusCode: http.StatusTooManyRequests,
					Body:       []byte(`{"error":{"code":"insufficient_quota","message":"quota exceeded"}}`),
				},
				Done: true,
			}
			close(ch)
			return ch, nil
		},
	}
	secondary := &stubBackendClient{
		fn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			mu.Lock()
			secondaryReqs = append(secondaryReqs, req)
			mu.Unlock()
			ch := make(chan backend.StreamChunk, 2)
			ch <- backend.StreamChunk{Text: "and recovered completion."}
			ch <- backend.StreamChunk{Done: true}
			close(ch)
			return ch, nil
		},
	}

	wantLimit := modelctx.For("claude-sonnet-4-6")
	fc := failover.NewWithOptions([]failover.Target{
		{
			Name:         "cloudcode",
			LogicalModel: "gemini-2.5-pro",
			Client:       primary,
			ModelMap:     func(string) string { return "gemini-2.5-pro" },
		},
		{
			Name:         "fallback:openrouter",
			LogicalModel: "claude-sonnet-4-6",
			MaxTokens:    wantLimit.ReserveTokens,
			Client:       secondary,
			ModelMap:     func(string) string { return "anthropic/claude-sonnet-4-6" },
		},
	}, nil, failover.WithHealthTracker(ht))

	engine := NewLocal(func(_ context.Context, model string) (Env, func()) {
		return Env{
			Backend: fc,
			Model:   "gemini-2.5-pro",
		}, func() {}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	frames, err := engine.Turn(ctx, Turn{
		Model: "gemini-2.5-pro",
		History: []agentlink.Message{
			{Role: "user", Content: "Explain failover"},
		},
	})
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}

	var text strings.Builder
	var failoverNotices []*agentlink.Notice
	var resultFrame *agentlink.Frame
	for f := range frames {
		switch f.Kind {
		case agentlink.KindText:
			text.WriteString(f.Text)
		case agentlink.KindNotice:
			if f.Notice != nil && f.Notice.Kind == agentlink.NoticeFailover {
				cp := *f.Notice
				failoverNotices = append(failoverNotices, &cp)
			} else if f.Notice != nil && f.Notice.Kind == agentlink.NoticeError {
				t.Fatalf("unexpected error notice: %+v", f.Notice)
			}
		case agentlink.KindResult:
			cp := f
			resultFrame = &cp
		}
	}

	if resultFrame == nil {
		t.Fatal("expected KindResult frame")
	}
	if got := text.String(); got != "Partial answer and recovered completion." {
		t.Fatalf("combined text = %q, want 'Partial answer and recovered completion.'", got)
	}
	if len(failoverNotices) != 1 {
		t.Fatalf("expected 1 NoticeFailover frame, got %d (%+v)", len(failoverNotices), failoverNotices)
	}
	n := failoverNotices[0]
	if n.FromTarget != "cloudcode" || n.ToTarget != "fallback:openrouter" || n.Model != "anthropic/claude-sonnet-4-6" {
		t.Fatalf("unexpected NoticeFailover payload: %+v", n)
	}

	// Verify checkpoint policy dynamically switched to the backup model's context window.
	engine.mu.Lock()
	cpModel := engine.checkpointModel
	cpPolicy := engine.checkpoint
	engine.mu.Unlock()
	if cpModel != "claude-sonnet-4-6" {
		t.Fatalf("checkpointModel = %q, want claude-sonnet-4-6", cpModel)
	}
	if cpPolicy == nil || cpPolicy.Model() != "claude-sonnet-4-6" {
		t.Fatalf("checkpoint policy model = %v, want claude-sonnet-4-6", cpPolicy)
	}
	if cpPolicy.Limit().ContextWindow != wantLimit.ContextWindow {
		t.Fatalf("checkpoint policy ContextWindow = %d, want %d", cpPolicy.Limit().ContextWindow, wantLimit.ContextWindow)
	}

	// Verify secondary request preserved partial trajectory and updated MaxTokens for backup model.
	mu.Lock()
	defer mu.Unlock()
	if len(secondaryReqs) != 1 {
		t.Fatalf("expected 1 secondary request, got %d", len(secondaryReqs))
	}
	var sawPartial bool
	for _, m := range secondaryReqs[0].Messages {
		if m.Role == "assistant" && strings.Contains(m.Content, "Partial answer ") {
			sawPartial = true
		}
	}
	if !sawPartial {
		t.Fatalf("expected secondary request to preserve partial assistant trajectory, got %+v", secondaryReqs[0].Messages)
	}
	if wantLimit.ReserveTokens > 0 && secondaryReqs[0].MaxTokens != wantLimit.ReserveTokens {
		t.Fatalf("secondary MaxTokens = %d, want %d", secondaryReqs[0].MaxTokens, wantLimit.ReserveTokens)
	}
}
