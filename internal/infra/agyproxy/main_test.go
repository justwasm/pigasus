package agyproxy

import (
	"os"
	"testing"

	"github.com/justwasm/pigasus/internal/testguard"
)

// TestMain points every configuration path at a scratch directory for the
// whole package.
//
// The proxy's state, the endpoint overrides and the pool's policy all live in
// files under the configuration home. A test that reads or writes one without
// an override would otherwise reach the user's own ~/.openagy — so the
// package pins them before any test runs, and a test that isolates further
// still overrides this.
func TestMain(m *testing.M) { os.Exit(testguard.Run(m)) }
