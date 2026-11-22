// Package failover implements multi-provider dynamic routing and fallback chains
// for backend.Client (Tier 3 Infra).
package failover

import (
	"context"
	"errors"
	"strings"

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
				if c.notify != nil {
					c.notify(Event{
						FromTarget: target.Name,
						ToTarget:   c.targets[i+1].Name,
						Model:      curReq.Model,
						Err:        err,
					})
				}
				continue
			}
			out <- backend.StreamChunk{Error: err, Done: true}
			return
		}

		var hasEmittedContent bool
		var targetFailedEarly bool

		for chunk := range inCh {
			// If target fails on the very first chunk without having produced any
			// text or tool calls, check if failover is eligible.
			if !hasEmittedContent && chunk.Error != nil && IsFailoverEligible(chunk.Error) && i+1 < len(c.targets) {
				lastErr = chunk.Error
				targetFailedEarly = true
				if c.notify != nil {
					c.notify(Event{
						FromTarget: target.Name,
						ToTarget:   c.targets[i+1].Name,
						Model:      curReq.Model,
						Err:        chunk.Error,
					})
				}
				// Drain the rest of this channel to prevent leaking resources
				break
			}

			if chunk.Text != "" || len(chunk.ToolCalls) > 0 || len(chunk.ThinkingBlocks) > 0 {
				hasEmittedContent = true
			}

			select {
			case <-ctx.Done():
				out <- backend.StreamChunk{Error: ctx.Err(), Done: true}
				return
			case out <- chunk:
			}

			if chunk.Done {
				return
			}
		}

		if targetFailedEarly {
			continue
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
	var he *apierror.HTTPError
	if errors.As(err, &he) {
		switch he.StatusCode {
		case 401, 403, 429, 500, 502, 503, 504:
			return true
		}
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
		strings.Contains(msg, "service unavailable") {
		return true
	}
	return false
}
