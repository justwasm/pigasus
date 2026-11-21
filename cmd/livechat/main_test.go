package main

import (
	"testing"

	"github.com/justwasm/pigasus/internal/ui/livechat"
)

func TestStandaloneLiveChat(t *testing.T) {
	// Verify Run with --help returns nil
	if err := livechat.Run([]string{"--help"}); err != nil {
		t.Fatalf("expected --help to return nil, got: %v", err)
	}
}
