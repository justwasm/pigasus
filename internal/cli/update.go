package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/creativeprojects/go-selfupdate"
	"github.com/justwasm/pigasus/internal/branding"
)

// Release describes an available version update.
type Release interface {
	Version() string
	LessOrEqual(other string) bool
}

// Updater defines the self-update interface to query and stage releases.
type Updater interface {
	DetectLatest(ctx context.Context, repoSlug string) (release Release, found bool, err error)
	UpdateTo(ctx context.Context, release Release, cmdPath string) error
}

type defaultSelfUpdater struct {
	up *selfupdate.Updater
}

func (s *defaultSelfUpdater) DetectLatest(ctx context.Context, repoSlug string) (Release, bool, error) {
	rel, found, err := s.up.DetectLatest(ctx, selfupdate.ParseSlug(repoSlug))
	if err != nil || !found {
		return nil, found, err
	}
	return rel, true, nil
}

func (s *defaultSelfUpdater) UpdateTo(ctx context.Context, release Release, cmdPath string) error {
	rel, ok := release.(*selfupdate.Release)
	if !ok {
		return errors.New("unsupported release type for live selfupdate")
	}
	return s.up.UpdateTo(ctx, rel, cmdPath)
}

var activeUpdater Updater = &defaultSelfUpdater{up: selfupdate.DefaultUpdater()}

// SetDefaultUpdater allows tests to swap in a mock or custom updater.
func SetDefaultUpdater(u Updater) {
	activeUpdater = u
}

// ResetDefaultUpdater restores the default selfupdater.
func ResetDefaultUpdater() {
	activeUpdater = &defaultSelfUpdater{up: selfupdate.DefaultUpdater()}
}

// RunUpdate handles the `update` subcommand.
func RunUpdate(w io.Writer, args []string) error {
	fmt.Fprintf(w, "\u27f3 Checking for updates... (current version %s)\n", ReferenceVersion)

	cfg := branding.Current()
	// Antigravity reference parity / offline mode:
	if cfg.ID == branding.BrandAntigravity || cfg.RepoSlug == "" {
		fmt.Fprint(w, "\u2713 You are already on the latest version.\n")
		return nil
	}

	repoSlug := cfg.RepoSlug
	if env := os.Getenv("PIGASUS_UPDATE_REPO"); env != "" {
		repoSlug = env
	}

	return runSelfUpdate(context.Background(), w, activeUpdater, ReferenceVersion, repoSlug)
}

func runSelfUpdate(ctx context.Context, w io.Writer, updater Updater, currentVersion string, repoSlug string) error {
	if updater == nil {
		updater = activeUpdater
	}

	latest, found, err := updater.DetectLatest(ctx, repoSlug)
	if err != nil {
		return fmt.Errorf("error occurred while detecting version: %w", err)
	}
	if !found {
		return fmt.Errorf("latest version for %s/%s could not be found from github repository %s", runtime.GOOS, runtime.GOARCH, repoSlug)
	}

	if currentVersion != "" && latest.LessOrEqual(currentVersion) {
		fmt.Fprint(w, "\u2713 You are already on the latest version.\n")
		return nil
	}

	fmt.Fprintf(w, "\u2705 Found new version %s.\n", latest.Version())
	fmt.Fprintln(w, "\u27f3 Downloading update...")

	exe, err := os.Executable()
	if err != nil {
		return errors.New("could not locate executable path")
	}

	if err := updater.UpdateTo(ctx, latest, exe); err != nil {
		return fmt.Errorf("error occurred while updating binary: %w", err)
	}

	fmt.Fprintf(w, "\u2713 Successfully updated to version %s.\n", latest.Version())
	return nil
}
