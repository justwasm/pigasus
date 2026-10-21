package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/branding"
)

type mockRelease struct {
	version string
	isLess  bool
}

func (m *mockRelease) Version() string {
	return m.version
}

func (m *mockRelease) LessOrEqual(other string) bool {
	return m.isLess
}

type mockUpdater struct {
	release    Release
	found      bool
	detectErr  error
	updateErr  error
	updatedExe string
	calledRel  Release
}

func (m *mockUpdater) DetectLatest(ctx context.Context, repoSlug string) (Release, bool, error) {
	if m.detectErr != nil {
		return nil, false, m.detectErr
	}
	return m.release, m.found, nil
}

func (m *mockUpdater) UpdateTo(ctx context.Context, rel Release, cmdPath string) error {
	m.calledRel = rel
	m.updatedExe = cmdPath
	return m.updateErr
}

func TestUpdate_Pigasus_AlreadyLatest(t *testing.T) {
	defer branding.SetCurrent(branding.BrandAntigravity)
	defer ResetDefaultUpdater()

	branding.SetCurrent(branding.BrandPigasus)

	mock := &mockUpdater{
		release: &mockRelease{version: ReferenceVersion, isLess: true},
		found:   true,
	}
	SetDefaultUpdater(mock)

	var buf bytes.Buffer
	err := RunUpdate(&buf, nil)
	if err != nil {
		t.Fatalf("RunUpdate failed: %v", err)
	}

	got := buf.String()
	wantHeader := "\u27f3 Checking for updates... (current version " + ReferenceVersion + ")\n"
	wantLatest := "\u2713 You are already on the latest version.\n"

	if !strings.HasPrefix(got, wantHeader) {
		t.Errorf("output missing header: got %q", got)
	}
	if !strings.Contains(got, wantLatest) {
		t.Errorf("output missing latest line: got %q", got)
	}
}

func TestUpdate_Pigasus_FoundNewVersion(t *testing.T) {
	defer branding.SetCurrent(branding.BrandAntigravity)
	defer ResetDefaultUpdater()

	branding.SetCurrent(branding.BrandPigasus)

	mock := &mockUpdater{
		release: &mockRelease{version: "1.5.0", isLess: false},
		found:   true,
	}
	SetDefaultUpdater(mock)

	var buf bytes.Buffer
	err := RunUpdate(&buf, nil)
	if err != nil {
		t.Fatalf("RunUpdate failed: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "\u2705 Found new version 1.5.0.") {
		t.Errorf("missing new version notice: got %q", got)
	}
	if !strings.Contains(got, "\u27f3 Downloading update...") {
		t.Errorf("missing downloading notice: got %q", got)
	}
	if !strings.Contains(got, "\u2713 Successfully updated to version 1.5.0.") {
		t.Errorf("missing success notice: got %q", got)
	}
	if mock.calledRel == nil || mock.calledRel.Version() != "1.5.0" {
		t.Errorf("UpdateTo not called with 1.5.0 release")
	}
}

func TestUpdate_Pigasus_NotFound(t *testing.T) {
	defer branding.SetCurrent(branding.BrandAntigravity)
	defer ResetDefaultUpdater()

	branding.SetCurrent(branding.BrandPigasus)

	mock := &mockUpdater{
		found: false,
	}
	SetDefaultUpdater(mock)

	var buf bytes.Buffer
	err := RunUpdate(&buf, nil)
	if err == nil {
		t.Fatalf("expected error when release not found, got nil")
	}
	if !strings.Contains(err.Error(), "could not be found") {
		t.Errorf("error message mismatch: %v", err)
	}
}

func TestUpdate_Pigasus_DetectError(t *testing.T) {
	defer branding.SetCurrent(branding.BrandAntigravity)
	defer ResetDefaultUpdater()

	branding.SetCurrent(branding.BrandPigasus)

	mock := &mockUpdater{
		detectErr: errors.New("network failure"),
	}
	SetDefaultUpdater(mock)

	var buf bytes.Buffer
	err := RunUpdate(&buf, nil)
	if err == nil {
		t.Fatalf("expected error on detect failure, got nil")
	}
	if !strings.Contains(err.Error(), "network failure") {
		t.Errorf("error mismatch: %v", err)
	}
}

func TestUpdate_Antigravity_AlwaysOffline(t *testing.T) {
	defer branding.SetCurrent(branding.BrandAntigravity)
	branding.SetCurrent(branding.BrandAntigravity)

	// Even with a failing updater, Antigravity mode must never call the updater.
	mock := &mockUpdater{
		detectErr: errors.New("must not be called"),
	}
	SetDefaultUpdater(mock)
	defer ResetDefaultUpdater()

	var buf bytes.Buffer
	err := RunUpdate(&buf, nil)
	if err != nil {
		t.Fatalf("RunUpdate failed in Antigravity mode: %v", err)
	}
	got := buf.String()
	want := "\u27f3 Checking for updates... (current version " + ReferenceVersion + ")\n" +
		"\u2713 You are already on the latest version.\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
