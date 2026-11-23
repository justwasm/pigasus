// Package failover implements multi-provider dynamic routing and fallback chains
// for backend.Client (Tier 3 Infra).
package failover

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/internal/core/apierror"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/clilog"
	"github.com/justwasm/pigasus/internal/infra/backend/retry"
)

// Target describes one endpoint or provider in the failover chain.
type Target struct {
	Name              string
	Client            backend.Client
	ModelMap          func(model string) string
	LogicalModel      string
	MaxTokens         int
	ReasoningEffort   string
	SupportsThinking  *bool
	SupportsToolCalls *bool
}

// Event reports a failover occurrence for telemetry, logging, and UI status updates.
type Event struct {
	FromTarget   string
	ToTarget     string
	Model        string
	LogicalModel string
	Err          error
}

// Notifier is called whenever an active target fails over to the next target.
type Notifier func(Event)

type notifierKey struct{}

// ContextWithNotifier attaches a per-turn failover Notifier to ctx.
func ContextWithNotifier(ctx context.Context, fn Notifier) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, notifierKey{}, fn)
}

// NotifierFromContext extracts a per-turn failover Notifier from ctx, or nil.
func NotifierFromContext(ctx context.Context) Notifier {
	if ctx == nil {
		return nil
	}
	fn, _ := ctx.Value(notifierKey{}).(Notifier)
	return fn
}

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

func broadcastEvent(ctx context.Context, e Event, local Notifier) {
	if local != nil {
		local(e)
	}
	if ctxNotify := NotifierFromContext(ctx); ctxNotify != nil {
		ctxNotify(e)
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

// MidStreamFailoverError wraps a failover-eligible error that occurred after
// partial content was already emitted to the caller. The failing target has
// been marked unhealthy in HealthTracker so an outer turn loop (actor/agentturn)
// can auto-continue seamlessly on the next healthy target while preserving
// trajectory.
type MidStreamFailoverError struct {
	Target   string
	Model    string
	Err      error
	Eligible bool
}

func (e *MidStreamFailoverError) Error() string {
	if e == nil || e.Err == nil {
		return "mid-stream failover error"
	}
	return e.Err.Error()
}

func (e *MidStreamFailoverError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// IsMidStreamRecoverable reports whether err is a MidStreamFailoverError
// eligible for outer-loop auto-continue onto a healthy backup target.
func IsMidStreamRecoverable(err error) bool {
	var me *MidStreamFailoverError
	return errors.As(err, &me) && me.Eligible
}

// Option configures a failover Client.
type Option func(*Client)

// WithHealthTracker overrides the HealthTracker used by the Client (useful for tests).
func WithHealthTracker(ht *HealthTracker) Option {
	return func(c *Client) {
		c.health = ht
	}
}

// Client is a resilient backend.Client that wraps an ordered chain of Targets.
type Client struct {
	targets []Target
	notify  Notifier
	health  *HealthTracker
}

// New constructs a Client from a prioritized list of Targets using the shared
// DefaultHealthTracker.
func New(targets []Target, notify Notifier) *Client {
	return NewWithOptions(targets, notify)
}

// NewWithOptions constructs a Client with optional configuration overrides.
func NewWithOptions(targets []Target, notify Notifier, opts ...Option) *Client {
	c := &Client{
		targets: targets,
		notify:  notify,
		health:  DefaultHealthTracker(),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	return c
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

func targetModel(t Target, baseModel string) string {
	if t.ModelMap != nil {
		return t.ModelMap(baseModel)
	}
	return baseModel
}

func (c *Client) selectCandidates() []Target {
	if c.health == nil {
		out := make([]Target, len(c.targets))
		copy(out, c.targets)
		return out
	}
	healthy := make([]Target, 0, len(c.targets))
	for _, t := range c.targets {
		if c.health.ShouldAttempt(t.Name) {
			healthy = append(healthy, t)
		}
	}
	if len(healthy) > 0 {
		return healthy
	}
	// All targets are in Open cooldown: probe the target whose cooldown expires
	// soonest, breaking ties by configured order (primary first).
	type ranked struct {
		target Target
		rem    time.Duration
		idx    int
	}
	all := make([]ranked, len(c.targets))
	for i, t := range c.targets {
		all[i] = ranked{
			target: t,
			rem:    c.health.CooldownRemaining(t.Name),
			idx:    i,
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].rem != all[j].rem {
			return all[i].rem < all[j].rem
		}
		return all[i].idx < all[j].idx
	})
	out := make([]Target, len(all))
	for i, r := range all {
		out[i] = r.target
	}
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
		req.Model = targetModel(t, req.Model)
		ch, err := t.Client.Stream(ctx, req)
		if err != nil && c.health != nil && IsFailoverEligible(err) && ctx.Err() == nil {
			c.health.RecordFailure(t.Name, err)
		}
		return ch, err
	}

	out := make(chan backend.StreamChunk, 8)
	go c.runStream(ctx, req, out)
	return out, nil
}

func (c *Client) runStream(ctx context.Context, req backend.StreamRequest, out chan<- backend.StreamChunk) {
	defer close(out)

	candidates := c.selectCandidates()
	if len(candidates) == 0 {
		out <- backend.StreamChunk{Error: errors.New("failover: no targets available"), Done: true}
		return
	}

	// If the primary target was skipped because its circuit is Open, notify
	// listeners immediately so model/checkpoint state and UI reflect the active route.
	if candidates[0].Name != c.targets[0].Name {
		broadcastEvent(ctx, Event{
			FromTarget:   c.targets[0].Name,
			ToTarget:     candidates[0].Name,
			Model:        targetModel(candidates[0], req.Model),
			LogicalModel: candidates[0].LogicalModel,
			Err:          fmt.Errorf("target %s in cooldown", c.targets[0].Name),
		}, c.notify)
	}

	var lastErr error

	for i := 0; i < len(candidates); i++ {
		target := candidates[i]
		hasRemainingBackup := i+1 < len(candidates)

		curReq := req
		curReq.Model = targetModel(target, req.Model)
		if i > 0 || candidates[0].Name != c.targets[0].Name {
			if target.MaxTokens > 0 {
				curReq.MaxTokens = target.MaxTokens
			}
			if target.ReasoningEffort != "" {
				curReq.ReasoningEffort = target.ReasoningEffort
			}
			if target.SupportsThinking != nil {
				curReq.SupportsThinking = target.SupportsThinking
			}
			if target.SupportsToolCalls != nil {
				curReq.SupportsToolCalls = target.SupportsToolCalls
			}
		}

		attemptCtx := ctx
		if hasRemainingBackup {
			// Fast failover when healthy backup targets remain: avoid sleeping
			// through full transport retry backoff before switching targets.
			attemptCtx = backend.ContextWithMaxRetries(ctx, 0)
		}

		inCh, err := target.Client.Stream(attemptCtx, curReq)
		if err != nil {
			lastErr = err
			if IsFailoverEligible(err) && ctx.Err() == nil {
				if c.health != nil {
					c.health.RecordFailure(target.Name, err)
				}
				if hasRemainingBackup {
					next := candidates[i+1]
					clilog.Logf("failover", "%s -> %s (model=%s) err=%v", target.Name, next.Name, targetModel(next, req.Model), err)
					broadcastEvent(ctx, Event{
						FromTarget:   target.Name,
						ToTarget:     next.Name,
						Model:        targetModel(next, req.Model),
						LogicalModel: next.LogicalModel,
						Err:          err,
					}, c.notify)
					continue
				}
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
			if !hasEmittedContent && chunk.Error != nil {
				if IsFailoverEligible(chunk.Error) && ctx.Err() == nil {
					lastErr = chunk.Error
					if c.health != nil {
						c.health.RecordFailure(target.Name, chunk.Error)
					}
					if hasRemainingBackup {
						targetFailedEarly = true
						next := candidates[i+1]
						broadcastEvent(ctx, Event{
							FromTarget:   target.Name,
							ToTarget:     next.Name,
							Model:        targetModel(next, req.Model),
							LogicalModel: next.LogicalModel,
							Err:          chunk.Error,
						}, c.notify)
						// Drain the rest of inCh in background to prevent leaking upstream goroutines
						go func(drain <-chan backend.StreamChunk) {
							for range drain {
							}
						}(inCh)
						break
					}
				}
				out <- backend.StreamChunk{Error: chunk.Error, Done: true}
				return
			}

			// Mid-stream error after partial content was already emitted: mark target
			// unhealthy and surface a typed MidStreamFailoverError so the outer turn
			// loop can auto-continue on the next healthy target while preserving trajectory.
			if hasEmittedContent && chunk.Error != nil {
				errOut := chunk.Error
				if IsFailoverEligible(chunk.Error) && ctx.Err() == nil {
					if c.health != nil {
						c.health.RecordFailure(target.Name, chunk.Error)
					}
					errOut = &MidStreamFailoverError{
						Target:   target.Name,
						Model:    curReq.Model,
						Err:      chunk.Error,
						Eligible: true,
					}
				}
				out <- backend.StreamChunk{Error: errOut, Done: true}
				return
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
					if c.health != nil {
						c.health.RecordSuccess(target.Name)
					}
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
				if c.health != nil {
					c.health.RecordSuccess(target.Name)
				}
				return
			}
		}

		if targetFailedEarly {
			continue
		}

		// If the channel closed without ever producing content or marking done, treat as dropped stream
		if !hasEmittedContent && !seenDone {
			lastErr = errors.New("stream closed without content")
			if ctx.Err() == nil && c.health != nil {
				c.health.RecordFailure(target.Name, lastErr)
			}
			if hasRemainingBackup && ctx.Err() == nil {
				next := candidates[i+1]
				broadcastEvent(ctx, Event{
					FromTarget:   target.Name,
					ToTarget:     next.Name,
					Model:        targetModel(next, req.Model),
					LogicalModel: next.LogicalModel,
					Err:          lastErr,
				}, c.notify)
				continue
			}
		}

		// If partial content was emitted and the stream closed without Done, treat as mid-stream drop.
		if hasEmittedContent && !seenDone {
			dropErr := errors.New("stream closed mid-turn without done")
			if ctx.Err() == nil {
				if c.health != nil {
					c.health.RecordFailure(target.Name, dropErr)
				}
				out <- backend.StreamChunk{
					Error: &MidStreamFailoverError{
						Target:   target.Name,
						Model:    curReq.Model,
						Err:      dropErr,
						Eligible: true,
					},
					Done: true,
				}
				return
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
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if apierror.IsQuotaReachedErr(err) || apierror.IsAuthFailureErr(err) || apierror.IsRetryableStatusErr(err) {
		return true
	}
	var he *apierror.HTTPError
	if errors.As(err, &he) {
		switch he.StatusCode {
		case 401, 402, 403, 429, 500, 502, 503, 504, 529:
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
