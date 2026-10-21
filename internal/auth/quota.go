// quota.go — the live quota summary the /usage panel renders.
//
// agy 1.4.3 calls the CloudCode RPC
// google.internal.cloud.code.v1internal.PredictionService/RetrieveUserQuotaSummary
// through the same daily-cloudcode-pa JSON endpoint the chat transport uses.
// The wrapper captures (~/.openagy/captures/*-retrieveUserQuotaSummary.json)
// show the REST shape:
//
//	POST /v1internal:retrieveUserQuotaSummary
//	{"project":"aicode-consumers"}
//
// and a 200 body with two groups (Gemini, Claude and GPT), each carrying a
// weekly and a 5-hour bucket. The TUI needs exactly that payload; the
// reference's own account tier comes from loadCodeAssist (see plan_tier.go).
package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// QuotaBucket is one quota window in a group (weekly, 5h).
type QuotaBucket struct {
	ID                string    `json:"bucketId"`
	DisplayName       string    `json:"displayName"`
	Window            string    `json:"window"`
	Description       string    `json:"description"`
	RemainingFraction float64   `json:"remainingFraction"`
	ResetTime         time.Time `json:"-"`
}

// quotaBucketWire is the JSON form: resetTime is RFC3339 text.
type quotaBucketWire struct {
	ID                string  `json:"bucketId"`
	DisplayName       string  `json:"displayName"`
	Window            string  `json:"window"`
	Description       string  `json:"description"`
	RemainingFraction float64 `json:"remainingFraction"`
	ResetTime         string  `json:"resetTime"`
}

// QuotaGroup is one model group sharing a quota.
type QuotaGroup struct {
	Name        string        `json:"displayName"`
	Description string        `json:"description"`
	Buckets     []QuotaBucket `json:"-"`
}

type quotaGroupWire struct {
	Name        string            `json:"displayName"`
	Description string            `json:"description"`
	Buckets     []quotaBucketWire `json:"buckets"`
}

// QuotaSummary is the decoded retrieveUserQuotaSummary response.
type QuotaSummary struct {
	Description string       `json:"description"`
	Groups      []QuotaGroup `json:"-"`
}

type quotaSummaryWire struct {
	Description string           `json:"description"`
	Groups      []quotaGroupWire `json:"groups"`
}

// ParseQuotaSummary decodes a 200 retrieveUserQuotaSummary body. Timestamps
// that fail to parse stay zero; the renderer then omits the countdown.
func ParseQuotaSummary(body []byte) (*QuotaSummary, error) {
	var wire quotaSummaryWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, err
	}
	out := &QuotaSummary{Description: strings.TrimSpace(wire.Description)}
	for _, g := range wire.Groups {
		group := QuotaGroup{
			Name:        strings.TrimSpace(g.Name),
			Description: strings.TrimSpace(g.Description),
		}
		for _, b := range g.Buckets {
			bucket := QuotaBucket{
				ID:                b.ID,
				DisplayName:       strings.TrimSpace(b.DisplayName),
				Window:            b.Window,
				Description:       b.Description,
				RemainingFraction: b.RemainingFraction,
			}
			if t, err := time.Parse(time.RFC3339, b.ResetTime); err == nil {
				bucket.ResetTime = t
			}
			group.Buckets = append(group.Buckets, bucket)
		}
		out.Groups = append(out.Groups, group)
	}
	return out, nil
}

// FetchQuotaSummary calls retrieveUserQuotaSummary with the same token and
// endpoint the chat path uses. The reference prefers the login keychain; the
// on-disk token can belong to a different account, so keychain is tried first.
func FetchQuotaSummary(ctx context.Context) (*QuotaSummary, error) {
	tok, err := cloudCodeAccessToken()
	if err != nil {
		return nil, err
	}
	body := []byte(`{"project":"aicode-consumers"}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		CloudCodeEndpoint+"/v1internal:retrieveUserQuotaSummary", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent",
		"antigravity/cli/1.4.4 (aidev_client; os_type=darwin; arch=arm64; auth_method=consumer)")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("retrieveUserQuotaSummary: HTTP %d", resp.StatusCode)
	}
	return ParseQuotaSummary(raw)
}
