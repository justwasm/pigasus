package agyproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/internal/accounts"
	"github.com/justwasm/pigasus/internal/auth"
)

type aggregatedQuotaWire struct {
	Groups      []aggregatedGroupWire `json:"groups"`
	Description string                `json:"description"`
}

type aggregatedGroupWire struct {
	DisplayName string                 `json:"displayName"`
	Description string                 `json:"description"`
	Buckets     []aggregatedBucketWire `json:"buckets"`
}

type aggregatedBucketWire struct {
	BucketID          string  `json:"bucketId,omitempty"`
	DisplayName       string  `json:"displayName"`
	Window            string  `json:"window,omitempty"`
	Description       string  `json:"description,omitempty"`
	RemainingFraction float64 `json:"remainingFraction"`
	ResetTime         string  `json:"resetTime,omitempty"`
	Disabled          bool    `json:"disabled,omitempty"`
}

// handleAggregatedQuota intercepts retrieveUserQuotaSummary / retrieveUserQuota calls
// in aggregate mode and returns a unified quota representation across all pool accounts.
func (s *Server) handleAggregatedQuota(w http.ResponseWriter, r *http.Request, body []byte) {
	ctx := r.Context()
	candidateAccounts, err := s.router.CandidateAccounts(ctx)
	if err != nil || len(candidateAccounts) == 0 {
		s.passthrough(w, r, body)
		return
	}

	summaries := s.collectQuotaSummaries(ctx, candidateAccounts)
	if len(summaries) == 0 {
		s.passthrough(w, r, body)
		return
	}

	out := s.mergeQuotaSummaries(candidateAccounts, summaries)
	respBytes, err := json.Marshal(out)
	if err != nil {
		s.logf("aggregate quota: marshal error: %v", err)
		s.passthrough(w, r, body)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Openagy-Aggregate-Mode", "true")
	w.Header().Set("X-Openagy-Pool-Size", strconv.Itoa(len(candidateAccounts)))
	w.Header().Set("Content-Length", strconv.Itoa(len(respBytes)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(respBytes)
}

func (s *Server) collectQuotaSummaries(ctx context.Context, accs []accounts.Account) map[string]*auth.QuotaSummary {
	policy := s.effectivePolicy()
	refreshDuration := policy.RefreshDuration()

	resCh := make(chan struct {
		email   string
		summary *auth.QuotaSummary
	}, len(accs))

	var wg sync.WaitGroup
	for _, acc := range accs {
		acc := acc
		// 1. Check custom test fetcher
		if s.quotaFetcher != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sum, err := s.quotaFetcher(ctx, acc.Email)
				if err == nil && sum != nil {
					resCh <- struct {
						email   string
						summary *auth.QuotaSummary
					}{email: acc.Email, summary: sum}
				}
			}()
			continue
		}

		// 2. Check fresh cached snapshot
		if acc.Quota != nil && time.Since(acc.Quota.FetchedAt) < refreshDuration {
			sum := acc.Quota.Summary()
			if sum != nil {
				resCh <- struct {
					email   string
					summary *auth.QuotaSummary
				}{email: acc.Email, summary: sum}
				continue
			}
		}

		// 3. Background fetch with timeout
		wg.Add(1)
		go func() {
			defer wg.Done()
			fetchCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			sum, err := accounts.FetchQuota(fetchCtx, acc.Email)
			if err != nil {
				if acc.Quota != nil {
					sum = acc.Quota.Summary()
				}
			}
			if sum != nil {
				resCh <- struct {
					email   string
					summary *auth.QuotaSummary
				}{email: acc.Email, summary: sum}
			}
		}()
	}

	wg.Wait()
	close(resCh)

	out := make(map[string]*auth.QuotaSummary, len(accs))
	for item := range resCh {
		out[item.email] = item.summary
	}
	return out
}

func (s *Server) mergeQuotaSummaries(accs []accounts.Account, summaries map[string]*auth.QuotaSummary) aggregatedQuotaWire {
	totalPool := len(accs)
	if totalPool == 0 {
		return aggregatedQuotaWire{}
	}

	type bucketKey struct {
		groupName string
		bucketID  string
		window    string
	}

	type bucketAgg struct {
		groupName   string
		groupDesc   string
		bucketID    string
		displayName string
		window      string
		sumFraction float64
		activeCount int
		earliestReset time.Time
		latestReset   time.Time
	}

	order := make([]bucketKey, 0)
	groupsOrder := make([]string, 0)
	groupDescriptions := make(map[string]string)
	aggs := make(map[bucketKey]*bucketAgg)

	for _, acc := range accs {
		sum, ok := summaries[acc.Email]
		if !ok || sum == nil {
			continue
		}
		isExcluded := s.router.IsExcluded(acc.Email)

		for _, grp := range sum.Groups {
			if _, exists := groupDescriptions[grp.Name]; !exists {
				groupsOrder = append(groupsOrder, grp.Name)
				groupDescriptions[grp.Name] = grp.Description
			}

			for _, b := range grp.Buckets {
				bID := b.ID
				if bID == "" {
					bID = b.DisplayName
				}
				key := bucketKey{
					groupName: grp.Name,
					bucketID:  bID,
					window:    b.Window,
				}
				agg, exists := aggs[key]
				if !exists {
					order = append(order, key)
					agg = &bucketAgg{
						groupName:   grp.Name,
						groupDesc:   grp.Description,
						bucketID:    b.ID,
						displayName: b.DisplayName,
						window:      b.Window,
					}
					aggs[key] = agg
				}

				effectiveFraction := b.RemainingFraction
				if isExcluded {
					effectiveFraction = 0
				}
				if effectiveFraction < 0 {
					effectiveFraction = 0
				} else if effectiveFraction > 1 {
					effectiveFraction = 1
				}

				agg.sumFraction += effectiveFraction
				if effectiveFraction > 0 && !isExcluded {
					agg.activeCount++
				}

				if !b.ResetTime.IsZero() {
					if agg.earliestReset.IsZero() || b.ResetTime.Before(agg.earliestReset) {
						agg.earliestReset = b.ResetTime
					}
					if b.ResetTime.After(agg.latestReset) {
						agg.latestReset = b.ResetTime
					}
				}
			}
		}
	}

	// Group buckets into output structure
	groupBucketsMap := make(map[string][]aggregatedBucketWire)
	for _, key := range order {
		agg := aggs[key]
		avgFraction := agg.sumFraction / float64(totalPool)
		if avgFraction > 1 {
			avgFraction = 1
		}
		if agg.sumFraction > 0 && avgFraction < 0.01 {
			avgFraction = 0.01
		}

		disabled := (agg.activeCount == 0 || avgFraction <= 0)

		resetStr := ""
		if !agg.earliestReset.IsZero() {
			resetStr = agg.earliestReset.UTC().Format(time.RFC3339)
		}

		desc := fmt.Sprintf("Unified Pool: %d/%d accounts ready (%.0f%% capacity)",
			agg.activeCount, totalPool, avgFraction*100)
		if disabled && !agg.earliestReset.IsZero() {
			desc += fmt.Sprintf(". Quota refreshes at %s", agg.earliestReset.Format("15:04 UTC"))
		}

		bWire := aggregatedBucketWire{
			BucketID:          agg.bucketID,
			DisplayName:       agg.displayName,
			Window:            agg.window,
			Description:       desc,
			RemainingFraction: round2(avgFraction),
			ResetTime:         resetStr,
			Disabled:          disabled,
		}
		groupBucketsMap[agg.groupName] = append(groupBucketsMap[agg.groupName], bWire)
	}

	groupsWire := make([]aggregatedGroupWire, 0, len(groupsOrder))
	for _, gName := range groupsOrder {
		groupsWire = append(groupsWire, aggregatedGroupWire{
			DisplayName: gName,
			Description: groupDescriptions[gName],
			Buckets:     groupBucketsMap[gName],
		})
	}

	return aggregatedQuotaWire{
		Description: fmt.Sprintf("OpenAGY Unified Pool (%d accounts). Automatic load balancing and failover active.", totalPool),
		Groups:      groupsWire,
	}
}

func round2(val float64) float64 {
	return float64(int(val*100+0.5)) / 100
}

// handleAggregatedCodeAssist intercepts loadCodeAssist and provides the best plan tier
// across all accounts in the pool so that agy unlocks Pro/Ultra capabilities.
func (s *Server) handleAggregatedCodeAssist(w http.ResponseWriter, r *http.Request, body []byte) {
	ctx := r.Context()
	candidateAccounts, err := s.router.CandidateAccounts(ctx)
	if err != nil || len(candidateAccounts) == 0 {
		s.passthrough(w, r, body)
		return
	}

	bestTierName, bestTierID := s.bestPlanTier(candidateAccounts)

	// Fetch upstream response using the routed account token
	token, _ := s.router.TokenFunc(ctx)
	req, err := s.upstreamRequest(r, body)
	if err != nil {
		s.writeSynthesizedCodeAssist(w, len(candidateAccounts), bestTierName, bestTierID)
		return
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := s.client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			_ = resp.Body.Close()
		}
		s.writeSynthesizedCodeAssist(w, len(candidateAccounts), bestTierName, bestTierID)
		return
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxRequestBody))
	if err != nil {
		s.writeSynthesizedCodeAssist(w, len(candidateAccounts), bestTierName, bestTierID)
		return
	}

	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		s.writeSynthesizedCodeAssist(w, len(candidateAccounts), bestTierName, bestTierID)
		return
	}

	// Upgrade tier info in response if pool has Pro or Ultra
	if bestTierID != "free-tier" {
		root["paidTier"] = map[string]any{
			"id":          bestTierID,
			"name":        bestTierName,
			"description": bestTierName,
		}
		if cur, ok := root["currentTier"].(map[string]any); ok {
			cur["name"] = "Antigravity (" + bestTierName + ")"
			cur["description"] = fmt.Sprintf("OpenAGY Unified Pool (%d accounts)", len(candidateAccounts))
		}
	}

	mergedBytes, err := json.Marshal(root)
	if err != nil {
		s.writeSynthesizedCodeAssist(w, len(candidateAccounts), bestTierName, bestTierID)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Openagy-Aggregate-Mode", "true")
	w.Header().Set("X-Openagy-Aggregate-Tier", bestTierName)
	w.Header().Set("Content-Length", strconv.Itoa(len(mergedBytes)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(mergedBytes)
}

func (s *Server) bestPlanTier(accs []accounts.Account) (string, string) {
	hasUltra := false
	hasPro := false

	for _, a := range accs {
		low := strings.ToLower(a.PlanTier)
		if strings.Contains(low, "ultra") {
			hasUltra = true
		} else if strings.Contains(low, "pro") {
			hasPro = true
		}
	}

	if hasUltra {
		return "Google AI Ultra", "ultra-tier"
	}
	if hasPro {
		return "Google AI Pro", "pro-tier"
	}
	return "Antigravity Starter Quota", "free-tier"
}

func (s *Server) writeSynthesizedCodeAssist(w http.ResponseWriter, poolCount int, tierName, tierID string) {
	synthesized := map[string]any{
		"currentTier": map[string]any{
			"id":          tierID,
			"name":        "Antigravity (" + tierName + ")",
			"description": fmt.Sprintf("OpenAGY Unified Pool (%d accounts)", poolCount),
		},
		"allowedTiers": []map[string]any{
			{
				"id":          "free-tier",
				"name":        "Antigravity",
				"description": "Gemini-powered code suggestions and chat in multiple IDEs",
				"isDefault":   true,
			},
			{
				"id":                                 "standard-tier",
				"name":                               "Antigravity Pro",
				"description":                        "Unlimited coding assistant with the most powerful Gemini models",
				"userDefinedCloudaicompanionProject": true,
				"usesGcpTos":                         true,
			},
		},
		"cloudaicompanionProject": "aicode-consumers",
		"gcpManaged":              false,
		"paidTier": map[string]any{
			"id":          tierID,
			"name":        tierName,
			"description": tierName,
		},
	}

	b, _ := json.Marshal(synthesized)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Openagy-Aggregate-Mode", "true")
	w.Header().Set("X-Openagy-Aggregate-Tier", tierName)
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}
