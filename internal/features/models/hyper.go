// hyper.go — Charm Hyper provider and Prism router support.
package models

import (
	"os"
	"strings"
)

const (
	// HyperProviderID is the default handle for Charm Hyper.
	HyperProviderID = "hyper"
	// DefaultHyperBaseURL is the upstream endpoint for Charm Hyper.
	DefaultHyperBaseURL = "https://hyper.charm.land"
)

// HyperBaseURL returns the configured base URL for Hyper, respecting $HYPER_URL.
func HyperBaseURL() string {
	if u := strings.TrimSpace(os.Getenv("HYPER_URL")); u != "" {
		return strings.TrimRight(u, "/")
	}
	return DefaultHyperBaseURL
}
