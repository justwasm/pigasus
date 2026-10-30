package cli

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/creativeprojects/go-selfupdate"
	"github.com/justwasm/pigasus/atomicfile"
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

func archFilterPattern(arch string) string {
	switch arch {
	case "amd64":
		return "(x86_64|amd64|64bit)"
	case "386":
		return "(i386|386|32bit)"
	case "arm64":
		return "(arm64|aarch64)"
	case "arm":
		return "(armv\\d+|arm)"
	default:
		return regexp.QuoteMeta(arch)
	}
}

func osFilterPattern(goos string) string {
	switch goos {
	case "darwin":
		return "(darwin|mac|macos)"
	default:
		return regexp.QuoteMeta(goos)
	}
}

func commandAssetFilter(cmd, goos, goarch string) string {
	cmd = strings.TrimSuffix(cmd, ".exe")
	cmdPattern := regexp.QuoteMeta(cmd)
	if cmd == "wagyu" {
		cmdPattern = "(pigasus|wagyu)"
	}
	return fmt.Sprintf("(?i)^%s_.*%s.*%s", cmdPattern, osFilterPattern(goos), archFilterPattern(goarch))
}

func resolveCommandName(cmd string) string {
	cmd = strings.ToLower(strings.TrimSuffix(filepath.Base(cmd), ".exe"))
	if cmd == "pigga" || cmd == "pigasus" || cmd == "wagyu" {
		return cmd
	}
	if len(os.Args) > 0 {
		arg0 := strings.ToLower(strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe"))
		if arg0 == "pigga" || arg0 == "pigasus" || arg0 == "wagyu" {
			return arg0
		}
		if strings.Contains(arg0, "pigga") {
			return "pigga"
		}
	}
	if strings.Contains(cmd, "pigga") {
		return "pigga"
	}
	return "pigasus"
}

func newSelfUpdater(cmd string) *selfupdate.Updater {
	filter := commandAssetFilter(cmd, runtime.GOOS, runtime.GOARCH)
	up, err := selfupdate.NewUpdater(selfupdate.Config{
		Validator: &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"},
		Filters:   []string{filter},
	})
	if err != nil {
		return selfupdate.DefaultUpdater()
	}
	return up
}

func (s *defaultSelfUpdater) DetectLatest(ctx context.Context, repoSlug string) (Release, bool, error) {
	up := s.up
	if up == nil {
		exe, _ := os.Executable()
		cmd := resolveCommandName(exe)
		s.up = newSelfUpdater(cmd)
		up = s.up
	}
	rel, found, err := up.DetectLatest(ctx, selfupdate.ParseSlug(repoSlug))
	if err == nil && !found {
		// Fallback to match combined pigasus archive for current OS & ARCH
		fallbackFilter := commandAssetFilter("pigasus", runtime.GOOS, runtime.GOARCH)
		fallbackUp, ferr := selfupdate.NewUpdater(selfupdate.Config{
			Validator: &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"},
			Filters:   []string{fallbackFilter},
		})
		if ferr == nil {
			rel, found, err = fallbackUp.DetectLatest(ctx, selfupdate.ParseSlug(repoSlug))
		}
	}
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

	data, err := downloadAsset(ctx, rel.AssetURL)
	if err != nil {
		return fmt.Errorf("failed to download asset %q: %w", rel.AssetName, err)
	}

	if rel.ValidationAssetURL != "" {
		if err := validateChecksum(ctx, rel.ValidationAssetURL, rel.AssetName, data); err != nil {
			return fmt.Errorf("checksum validation failed: %w", err)
		}
	}

	_, rawCmd := filepath.Split(cmdPath)
	cmd := strings.TrimSuffix(rawCmd, ".exe")
	candidates := []string{cmd}
	if cmd != "pigga" {
		candidates = append(candidates, "pigga")
	}
	if cmd != "pigasus" {
		candidates = append(candidates, "pigasus")
	}

	var binData []byte
	var extractErr error
	for _, targetCmd := range candidates {
		binReader, err := selfupdate.DecompressCommand(bytes.NewReader(data), rel.AssetName, targetCmd, runtime.GOOS, runtime.GOARCH)
		if err == nil {
			binData, extractErr = io.ReadAll(binReader)
			if extractErr == nil && len(binData) > 0 {
				break
			}
		} else if errors.Is(err, selfupdate.ErrCannotDecompressFile) {
			binData = data
			extractErr = nil
			break
		} else {
			extractErr = err
		}
	}
	if len(binData) == 0 {
		if extractErr != nil {
			return fmt.Errorf("error extracting %s from %s: %w", cmd, rel.AssetName, extractErr)
		}
		return fmt.Errorf("extracted binary %s from %s is empty", cmd, rel.AssetName)
	}

	if err := atomicfile.WriteFile(cmdPath, binData, 0o755); err != nil {
		return fmt.Errorf("error writing binary atomically to %s: %w", cmdPath, err)
	}
	return nil
}

func getGitHubToken() string {
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token
	}
	return os.Getenv("GH_TOKEN")
}

func downloadAsset(ctx context.Context, assetURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, assetURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/octet-stream")
	if token := getGitHubToken(); token != "" {
		req.Header.Set("Authorization", "token "+token)
	}
	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func validateChecksum(ctx context.Context, checksumURL, assetName string, assetData []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumURL, nil)
	if err != nil {
		return err
	}
	if token := getGitHubToken(); token != "" {
		req.Header.Set("Authorization", "token "+token)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d fetching checksums", resp.StatusCode)
	}
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	actualHash := fmt.Sprintf("%x", sha256.Sum256(assetData))

	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			expectedHash := strings.ToLower(parts[0])
			filename := filepath.Base(strings.TrimPrefix(parts[1], "*"))
			if strings.EqualFold(filename, assetName) {
				if expectedHash != actualHash {
					return fmt.Errorf("hash mismatch for %s: expected %s, got %s", assetName, expectedHash, actualHash)
				}
				return nil
			}
		}
	}
	return fmt.Errorf("entry for %q not found in checksums", assetName)
}

var (
	activeUpdater Updater = &defaultSelfUpdater{}
	// ProgramVersion optionally records the compiled-in or configured program version.
	ProgramVersion string
)

// SetProgramVersion sets the running program's version string.
func SetProgramVersion(v string) {
	ProgramVersion = v
}

// SetDefaultUpdater allows tests to swap in a mock or custom updater.
func SetDefaultUpdater(u Updater) {
	activeUpdater = u
}

// ResetDefaultUpdater restores the default selfupdater.
func ResetDefaultUpdater() {
	activeUpdater = &defaultSelfUpdater{}
}

// RunUpdate handles the `update` subcommand.
func RunUpdate(w io.Writer, args []string) error {
	exe, _ := os.Executable()
	cmdName := resolveCommandName(exe)
	currentVer := ProgramVersion
	if currentVer == "" {
		currentVer = ReferenceVersion
	}
	if cmdName == "pigga" {
		if env := os.Getenv("PIGGA_CURRENT_VERSION"); env != "" {
			currentVer = env
		} else if env := os.Getenv("PIGASUS_CURRENT_VERSION"); env != "" {
			currentVer = env
		}
	} else {
		if env := os.Getenv("PIGASUS_CURRENT_VERSION"); env != "" {
			currentVer = env
		} else if env := os.Getenv("PIGGA_CURRENT_VERSION"); env != "" {
			currentVer = env
		}
	}
	fmt.Fprintf(w, "\u27f3 Checking for updates... (current version %s)\n", currentVer)

	cfg := branding.Current()
	// Antigravity reference parity / offline mode:
	if cfg.ID == branding.BrandAntigravity || cfg.RepoSlug == "" {
		fmt.Fprint(w, "\u2713 You are already on the latest version.\n")
		return nil
	}

	repoSlug := cfg.RepoSlug
	if env := os.Getenv("PIGGA_UPDATE_REPO"); env != "" && cmdName == "pigga" {
		repoSlug = env
	} else if env := os.Getenv("PIGASUS_UPDATE_REPO"); env != "" {
		repoSlug = env
	} else if env := os.Getenv("PIGGA_UPDATE_REPO"); env != "" {
		repoSlug = env
	}

	return runSelfUpdate(context.Background(), w, activeUpdater, strings.TrimSuffix(currentVer, "-wasm"), repoSlug)
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
