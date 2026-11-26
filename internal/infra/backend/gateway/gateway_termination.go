// Package gateway termination diagnostics distinguish provider-declared partial
// results from successful completion. These are not retryable HTTP failures:
// replaying a filtered/refused or token-limited response cannot repair it.
package gateway

import "fmt"

func gatewayTerminationError(protocol, reason string) error {
	switch reason {
	case "length", "max_output_tokens", "max_tokens":
		return fmt.Errorf("gateway: %s response truncated (%s): output token limit reached; partial tool calls were discarded", protocol, reason)
	case "content_filter":
		return fmt.Errorf("gateway: %s response stopped by content filter (%s); partial tool calls were discarded", protocol, reason)
	case "refusal":
		return fmt.Errorf("gateway: %s response refused (%s); partial tool calls were discarded", protocol, reason)
	default:
		return fmt.Errorf("gateway: %s response incomplete (%s); partial tool calls were discarded", protocol, reason)
	}
}
