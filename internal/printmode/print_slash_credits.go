// print_slash_credits.go — the print-mode `/credits` answer.
//
// The reference answers `/credits` with two tab-separated records, both read
// from the account's own code-assist state:
//
//	Remaining credits	0
//	Upgrade	https://antigravity.google/g1-upgrade
//
// 【一手】 agy 1.4.8, `agy -p /credits`:
//
//	Remaining credits	0
//	Upgrade	https://antigravity.google/g1-upgrade
//
// and `agy -p /credits --output-format json`:
//
//	{"name":"credits","data":{"remaining_credits":0,
//	                          "upgrade_uri":"https://antigravity.google/g1-upgrade"}}
//
// Both figures come from `v1internal:loadCodeAssist` — the same answer the
// clone already reads for the Web UI's code-assist state
// (`internal/auth/code_assist_state.go`):
//
//   - the balance is the sum of the `paidTier.availableCredits` entries'
//     `creditAmount` fields. The capture account's paid tier carries one
//     entry (`{"creditType":"GOOGLE_ONE_AI","minimumCreditAmountForUsage":"50"}`)
//     with **no** amount, and the reference reports 0.
//   - the purchase link is the paid tier's `upgradeSubscriptionUri`
//     verbatim.
//
// The reference's own failure text is `GetG1Credits: paidTier is nil`
// 【strings】 — it is what an account without a paid tier gets.

package printmode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/auth"
)

// jsonCreditsPayload is the `/credits` command payload: the reference's two
// members, in the reference's order.
type jsonCreditsPayload struct {
	RemainingCredits int64  `json:"remaining_credits"`
	UpgradeURI       string `json:"upgrade_uri"`
}

// creditsFetchTimeout bounds the code-assist state read.
const creditsFetchTimeout = 10 * time.Second

// creditsStateFetcher is the seam the tests replace: it reads the account's
// code-assist state.
var creditsStateFetcher = auth.FetchCodeAssistState

// handlePrintSlashCredits answers `/credits` in print mode.
//
// The reference treats `/credits` as a read-only command: it answers locally,
// without a model turn, and exits 0. It is the one documented command whose
// answer is *live account state* rather than a local table.
func handlePrintSlashCredits() printSlashResult {
	remaining, uri, err := fetchG1Credits()
	if err != nil {
		return rejected(1, "/credits failed: retrieving credits: "+err.Error())
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Remaining credits\t%d\n", remaining)
	fmt.Fprintf(&b, "Upgrade\t%s\n", uri)
	return printSlashResult{
		Handled: true,
		Output:  b.String(),
		Name:    "credits",
		Data:    jsonCreditsPayload{RemainingCredits: remaining, UpgradeURI: uri},
	}
}

// fetchG1Credits reads the account's G1 credit balance and purchase link.
func fetchG1Credits() (int64, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), creditsFetchTimeout)
	defer cancel()
	raw, err := creditsStateFetcher(ctx)
	if err != nil {
		return 0, "", err
	}
	return parseG1Credits(raw)
}

// parseG1Credits extracts the credit balance and the upgrade link from a
// code-assist state answer.
func parseG1Credits(raw []byte) (int64, string, error) {
	var state map[string]any
	if err := json.Unmarshal(raw, &state); err != nil {
		return 0, "", fmt.Errorf("parsing code assist state: %w", err)
	}
	paid, ok := state["paidTier"].(map[string]any)
	if !ok {
		return 0, "", errors.New("GetG1Credits: paidTier is nil")
	}
	uri, _ := paid["upgradeSubscriptionUri"].(string)
	if uri == "" {
		uri, _ = paid["upgrade_subscription_uri"].(string)
	}
	var remaining int64
	if list, ok := paid["availableCredits"].([]any); ok {
		for _, item := range list {
			entry, ok := item.(map[string]any)
			if !ok {
				continue
			}
			remaining += creditAmount(entry)
		}
	}
	return remaining, uri, nil
}

// creditAmount reads one credit entry's amount. The proto field is an int64,
// so the JSON carries it as a string; a plain number is accepted too.
func creditAmount(entry map[string]any) int64 {
	for _, key := range []string{"creditAmount", "credit_amount"} {
		switch v := entry[key].(type) {
		case string:
			if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
				return n
			}
		case float64:
			return int64(v)
		case json.Number:
			if n, err := v.Int64(); err == nil {
				return n
			}
		}
	}
	return 0
}
