package storybook_test

import (
	"os"
	"testing"

	"github.com/justwasm/pigasus/internal/core/branding"
	"github.com/justwasm/pigasus/internal/testguard"
)

func TestMain(m *testing.M) {
	dir := testguard.Install()
	defer func() {
		if dir != "" {
			_ = os.RemoveAll(dir)
		}
	}()

	os.Setenv("AGY_CLI_LOGO_STYLE", "half")
	os.Setenv("TERM_PROGRAM", "")
	os.Setenv("LC_TERMINAL", "")
	branding.SetCurrent(branding.BrandPigasus)

	os.Exit(m.Run())
}
