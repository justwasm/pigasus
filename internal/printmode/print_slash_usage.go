package printmode

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/auth"
)

// jsonUsageBucket is one quota bucket of the `/usage` payload. Field order is
// the reference's wire order (a map would sort them alphabetically).
//
// `remaining_fraction` drives the text answer's percentage
// (`round(fraction*100)`), and `reset_time` is the RFC 3339 instant the bucket
// refills. Both are live figures in the reference.
type jsonUsageBucket struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	Description       string  `json:"description"`
	Window            string  `json:"window"`
	RemainingFraction float64 `json:"remaining_fraction"`
	ResetTime         string  `json:"reset_time"`
}

// jsonUsageGroup is one model group (a set of models that share a quota
// bucket).
type jsonUsageGroup struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Buckets     []jsonUsageBucket `json:"buckets"`
}

// jsonUsagePayload is the `/usage` command payload.
type jsonUsagePayload struct {
	Description string           `json:"description"`
	Groups      []jsonUsageGroup `json:"groups"`
}

// printSlashUsagePayload is the `/usage` answer.
//
// Placeholder for the live figures (【推测】): the reference resolves this from
// a live weekly-quota service (`agy -p /usage` calls it and blocks on the
// response), so the *text* answer's percentage and reset time change as quota
// is consumed and as the window rolls over — a byte-for-byte diff against the
// reference for `/usage` is therefore unattainable, not merely unimplemented.
// What is 【一手】 is the *shape*: captured from
// `agy -p=/usage --output-format json` on 1.4.0 —
//
//	{"description":"Within each group, models share a weekly limit. …",
//	 "groups":[{"name":"Gemini Models",
//	            "description":"Models within this group: Gemini Flash, Gemini Pro",
//	            "buckets":[{"id":"gemini-weekly","name":"Weekly Limit Remaining",
//	                        "description":"You have used some of your weekly limit, …",
//	                        "window":"weekly","remaining_fraction":0.92,
//	                        "reset_time":"2026-10-23T22:19:25Z"}]},
//	           {"name":"Claude and GPT models",
//	            "description":"Models within this group: Claude Opus, Claude Sonnet, GPT-OSS",
//	            "buckets":[{"id":"3p-weekly","name":"Weekly Limit Remaining",
//	                        "description":"You have hit your weekly limit, …",
//	                        "window":"weekly","remaining_fraction":0,
//	                        "reset_time":"2026-10-20T00:48:30Z"}]}]}
//
// The two group names, the bucket ids/names/window, the two group descriptions
// and the top-level description are recovered verbatim. The fractions, the
// relative "in N days, N hours" clauses inside the bucket descriptions, and
// both reset times are the values observed on the capture machine at one
// instant; they are frozen here as placeholders because nothing in the clone
// can recompute them.
func printSlashUsagePayload() jsonUsagePayload {
	return jsonUsagePayload{
		Description: "Within each group, models share a weekly limit. Quota is consumed proportionally to the cost of the tokens. Thus, limits will last longer with shorter tasks or using more cost-effective models. Your weekly limit is tied directly to your individual tier.",
		Groups: []jsonUsageGroup{
			{
				Name:        "Gemini Models",
				Description: "Models within this group: Gemini Flash, Gemini Pro",
				Buckets: []jsonUsageBucket{{
					ID:                "gemini-weekly",
					Name:              "Weekly Limit Remaining",
					Description:       "You have used some of your weekly limit, it will fully refresh in 6 days, 15 hours.",
					Window:            "weekly",
					RemainingFraction: 0.92,
					ResetTime:         "2026-10-23T22:19:25Z",
				}},
			},
			{
				Name:        "Claude and GPT models",
				Description: "Models within this group: Claude Opus, Claude Sonnet, GPT-OSS",
				Buckets: []jsonUsageBucket{{
					ID:                "3p-weekly",
					Name:              "Weekly Limit Remaining",
					Description:       "You have hit your weekly limit, it refreshes in 2 days, 17 hours. If on a supported paid plan, you can use AI credits in the interim or upgrade to a higher tier.",
					Window:            "weekly",
					RemainingFraction: 0,
					ResetTime:         "2026-10-20T00:48:30Z",
				}},
			},
		},
	}
}

func liveOrStaticUsagePayload() jsonUsagePayload {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	summary, err := auth.FetchQuotaSummary(ctx)
	if err == nil && summary != nil && len(summary.Groups) > 0 {
		payload := jsonUsagePayload{
			Description: summary.Description,
		}
		if payload.Description == "" {
			payload.Description = "Within each group, models share a weekly limit and a 5-hour limit. Quota is consumed proportionally to the cost of the tokens. Thus, limits will last longer with shorter tasks or using more cost-effective models. The 5-hour limit smooths out aggregate demand to fairly distribute global capacity across all users, while your weekly limit is tied directly to your individual tier."
		}
		for _, g := range summary.Groups {
			group := jsonUsageGroup{
				Name:        g.Name,
				Description: g.Description,
			}
			for _, b := range g.Buckets {
				resetStr := ""
				if !b.ResetTime.IsZero() {
					resetStr = b.ResetTime.UTC().Format(time.RFC3339)
				}
				group.Buckets = append(group.Buckets, jsonUsageBucket{
					ID:                b.ID,
					Name:              b.DisplayName,
					Description:       b.Description,
					Window:            b.Window,
					RemainingFraction: b.RemainingFraction,
					ResetTime:         resetStr,
				})
			}
			payload.Groups = append(payload.Groups, group)
		}
		return payload
	}
	return printSlashUsagePayload()
}

// printSlashUsage renders the `/usage` payload as the text answer: one
// "<group>\t<bucket>\t<pct>%\t<reset>" line per bucket, in group order. The
// percentage is the bucket's remaining fraction rounded to the nearest whole
// number, which is what the reference prints (fraction 0.8987 → "90%").
//
// The shape is the reference's, captured 【一手】 from agy 1.4.8 on 2026-10-28:
//
//	Gemini Models	Weekly Limit Remaining	97%	2026-11-02T06:18:32Z
//	Gemini Models	Five Hour Limit Remaining	96%	2026-10-27T22:08:47Z
//	Claude and GPT models	Weekly Limit Remaining	100%	2026-11-03T18:57:37Z
//	Claude and GPT models	Five Hour Limit Remaining	100%	2026-10-27T23:57:37Z
//
// The same bytes are the JSON envelope's `response` field — the reference
// renders one text form and carries it in both places. The earlier
// "Quota:\n%-23s%-27s%3.0f%%  %s" table is a 1.4.3-era shape with no
// counterpart in the 1.4.8 string table (no `%-23s`, no `%3.0f%%`), and it
// printed the reset time in local time rather than the reference's UTC
// RFC 3339.
func printSlashUsage() string {
	return printSlashUsageWithPayload(printSlashUsagePayload())
}

func printSlashUsageWithPayload(payload jsonUsagePayload) string {
	var b strings.Builder
	for _, g := range payload.Groups {
		for _, bk := range g.Buckets {
			fmt.Fprintf(&b, "%s\t%s\t%.0f%%\t%s\n",
				g.Name, bk.Name, bk.RemainingFraction*100, bk.ResetTime)
		}
	}
	return b.String()
}
