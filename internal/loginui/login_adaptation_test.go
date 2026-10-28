package loginui_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/loginui"
	"github.com/justwasm/pigasus/internal/tuilib"
)

func TestLoginView_BannerOnlyShowsWhenWholeStepFits(t *testing.T) {
	logoFirstLine := tuilib.RenderLogoLines()[0]

	// Ample height: logo banner is present
	mLarge := loginui.New()
	mLarge.Width = 80
	mLarge.Height = 30
	viewLarge := mLarge.View().Content
	if !strings.Contains(viewLarge, logoFirstLine) {
		t.Errorf("large terminal should show the logo banner")
	}

	// Constrained height: logo banner is suppressed so the whole step fits
	mSmall := loginui.New()
	mSmall.Width = 80
	mSmall.Height = 16
	viewSmall := mSmall.View().Content
	if strings.Contains(viewSmall, logoFirstLine) {
		t.Errorf("constrained height (16) should suppress the logo banner")
	}
	// Content and buttons still visible
	if !strings.Contains(viewSmall, "Google OAuth") {
		t.Errorf("constrained height missing menu content")
	}
	if !strings.Contains(viewSmall, "Submit") {
		t.Errorf("constrained height missing footer submit button")
	}
}

func TestLoginUpdate_PageUpPageDown(t *testing.T) {
	m := loginui.New()
	m.State = loginui.StateOAuth
	m.AuthURL = testAuthURL
	m.Width = 40
	m.Height = 15

	// Scroll down with PgDn
	m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if m.Scroll == 0 {
		t.Errorf("expected Scroll > 0 after PgDn")
	}

	// Scroll back with PgUp
	m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	if m.Scroll != 0 {
		t.Errorf("expected Scroll == 0 after PgUp, got %d", m.Scroll)
	}
}
