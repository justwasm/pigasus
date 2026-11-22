// Package failover implements multi-provider dynamic routing and fallback chains
// for backend.Client (Tier 3 Infra).
package failover

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"

	"github.com/justwasm/pigasus/internal/core/apierror"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/infra/backend/retry"
)

// Target describes one endpoint or provider in the failover chain.
type Target struct {
	Name     string
	Client   backend.Client
	ModelMap func(model string) string
}

// Event reports a failover occurrence for telemetry, logging, and UI status updates.
type Event struct {
	FromTarget string
	ToTarget   string
	Model      string
	Err        error
}

// Notifier is called whenever an active target fails over to the next target.
type Notifier func(Event)

var (
	globalNotifiersMu sync.RWMutex
	globalNotifiers   []Notifier
)

// RegisterGlobalNotifier registers a global notifier called on any failover event across all clients.
func RegisterGlobalNotifier(n Notifier) {
	if n == nil {
		return
	}
	globalNotifiersMu.Lock()
	defer globalNotifiersMu.Unlock()
	globalNotifiers = append(globalNotifiers, n)
}

// ResetGlobalNotifiers clears registered global notifiers (primarily for tests).
func ResetGlobalNotifiers() {
	globalNotifiersMu.Lock()
	defer globalNotifiersMu.Unlock()
	globalNotifiers = nil
}

func broadcastEvent(e Event, local Notifier) {
	if local != nil {
		local(e)
	}
	globalNotifiersMu.RLock()
	var notifiers []Notifier
	if len(globalNotifiers) > 0 {
		notifiers = append([]Notifier(nil), globalNotifiers...)
	}
	globalNotifiersMu.RUnlock()
	for _, n := range notifiers {
		n(e)
	}
}

// Client is a resilient backend.Client that wraps an ordered chain of Targets.
type Client struct {
	targets []Target
	notify  Notifier
}

// New constructs a Client from a prioritized list of Targets.
func New(targets []Target, notify Notifier) *Client {
	return &Client{
		targets: targets,
		notify:  notify,
	}
}

// Targets returns the configured failover targets.
func (c *Client) Targets() []Target {
	if c == nil {
		return nil
	}
	out := make([]Target, len(c.targets))
	copy(out, c.targets)
	return out
}

// Stream executes the request against the primary target, failing over to successive
// targets if an initial failure (429, 401, 503, connection drop) occurs before any content
// has been emitted.
func (c *Client) Stream(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
	if c == nil || len(c.targets) == 0 {
		return nil, errors.New("failover: no targets configured in chain")
	}
	if len(c.targets) == 1 {
		t := c.targets[0]
		if t.ModelMap != nil {
			req.Model = t.ModelMap(req.Model)
		}
		return t.Client.Stream(ctx, req)
	}

	out := make(chan backend.StreamChunk, 8)
	go c.runStream(ctx, req, out)
	return out, nil
}

func (c *Client) runStream(ctx context.Context, req backend.StreamRequest, out chan<- backend.StreamChunk) {
	defer close(out)

	var lastErr error

	for i := 0; i < len(c.targets); i++ {
		target := c.targets[i]
		curReq := req
		if target.ModelMap != nil {
			curReq.Model = target.ModelMap(req.Model)
		}

		inCh, err := target.Client.Stream(ctx, curReq)
		if err != nil {
			lastErr = err
			if IsFailoverEligible(err) && i+1 < len(c.targets) {
				broadcastEvent(Event{
					FromTarget: target.Name,
					ToTarget:   c.targets[i+1].Name,
					Model:      curReq.Model,
					Err:        err,
				}, c.notify)
				continue
			}
			out <- backend.StreamChunk{Error: err, Done: true}
			return
		}

		var hasEmittedContent bool
		var targetFailedEarly bool
		var seenDone bool
		var pending []backend.StreamChunk

		for chunk := range inCh {
			// If target fails on the initial chunk(s) before producing any text, tool calls,
			// or thinking blocks, check if failover is eligible.
			if !hasEmittedContent && chunk.Error != nil && IsFailoverEligible(chunk.Error) && i+1 < len(c.targets) {
				lastErr = chunk.Error
				targetFailedEarly = true
				broadcastEvent(Event{
					FromTarget: target.Name,
					ToTarget:   c.targets[i+1].Name,
					Model:      curReq.Model,
					Err:        chunk.Error,
				}, c.notify)
				// Drain the rest of inCh in background to prevent leaking upstream goroutines
				go func(drain <-chan backend.StreamChunk) {
					for range drain {
					}
				}(inCh)
				break
			}

			if chunk.Text != "" || len(chunk.ToolCalls) > 0 || len(chunk.ThinkingBlocks) > 0 {
				hasEmittedContent = true
				// Flush any buffered non-content chunks prior to real content
				if len(pending) > 0 {
					for _, p := range pending {
						select {
						case <-ctx.Done():
							out <- backend.StreamChunk{Error: ctx.Err(), Done: true}
							return
						case out <- p:
						}
					}
					pending = nil
				}
			}

			if !hasEmittedContent {
				pending = append(pending, chunk)
				if chunk.Done {
					seenDone = true
					break
				}
				continue
			}

			select {
			case <-ctx.Done():
				out <- backend.StreamChunk{Error: ctx.Err(), Done: true}
				return
			case out <- chunk:
			}

			if chunk.Done {
				seenDone = true
				return
			}
		}

		if targetFailedEarly {
			continue
		}

		// If the channel closed without ever producing content or marking done, treat as dropped stream
		if !hasEmittedContent && !seenDone {
			lastErr = errors.New("stream closed without content")
			if i+1 < len(c.targets) {
				broadcastEvent(Event{
					FromTarget: target.Name,
					ToTarget:   c.targets[i+1].Name,
					Model:      curReq.Model,
					Err:        lastErr,
				}, c.notify)
				continue
			}
		}

		// Flush any remaining clean empty response chunks if marked done
		if len(pending) > 0 {
			for _, p := range pending {
				select {
				case <-ctx.Done():
					out <- backend.StreamChunk{Error: ctx.Err(), Done: true}
					return
				case out <- p:
				}
			}
		}
		return
	}

	if lastErr != nil {
		out <- backend.StreamChunk{Error: lastErr, Done: true}
	}
}

// IsFailoverEligible reports whether err is an error that justifies failing over
// to another provider (e.g. 429 quota exhaustion, 401 unauthenticated, 403 forbidden,
// 5xx server errors, or transient connection drops before stream payload begins).
func IsFailoverEligible(err error) bool {
	if err == nil {
		return false
	}
	if apierror.IsQuotaReachedErr(err) {
		return true
	}
	var he *apierror.HTTPError
	if errors.As(err, &he) {
		switch he.StatusCode {
		case 401, 403, 429, 500, 502, 503, 504:
			return true
		}
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	if retry.IsConnectionDropped(err) {
		return true
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "quota") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "resource_exhausted") ||
		strings.Contains(msg, "insufficient_quota") ||
		strings.Contains(msg, "credit") ||
		strings.Contains(msg, "unauthorized") ||
		strings.Contains(msg, "overloaded") ||
		strings.Contains(msg, "service unavailable") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "timed out") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "network is unreachable") ||
		strings.Contains(msg, "stream closed without content") ||
		strings.Contains(msg, "server outage") ||
		strings.Contains(msg, "server error") {
		return true
	}
	return false
}
