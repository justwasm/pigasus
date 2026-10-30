package agyupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/mod/semver"
	"github.com/justwasm/pigasus/atomicfile"
	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/internal/cli"
)

// Manifest represents the auto-updater release manifest JSON structure.
type Manifest struct {
	Version    string `json:"version"`
	PackageURL string `json:"package_url,omitempty"`
	URL        string `json:"url,omitempty"`
	Sha256     string `json:"sha256,omitempty"`
	Sha512     string `json:"sha512,omitempty"`
}

// Options configures the update check.
type Options struct {
	HTTPClient     *http.Client
	CurrentVersion string
	ManifestURL    string
	GOOS           string
	GOARCH         string
	TargetBinary   string
}

// FormatGlogError formats a log line with glog's header and the bootstrap prefix,
// matching the reference binary's logging before google.Init.
func FormatGlogError(now time.Time, loc, msg string) string {
	_, month, day := now.Date()
	hour, minute, second := now.Clock()
	return fmt.Sprintf("ERROR: logging before google.Init: E%02d%02d %02d:%02d:%02d.%06d       1 %s] %s\n",
		int(month), day,
		hour, minute, second, now.Nanosecond()/1000,
		loc, msg)
}

// IsNewerVersion returns true if remote is semver-greater than current,
// matching google3/third_party/jetski/cli/updater/updater.isNewerVersion.
func IsNewerVersion(remote, current string) bool {
	remote = strings.TrimSpace(remote)
	current = strings.TrimSpace(current)
	if !strings.HasPrefix(remote, "v") {
		remote = "v" + remote
	}
	if !strings.HasPrefix(current, "v") {
		current = "v" + current
	}
	return semver.Compare(remote, current) > 0
}

func matchBinName(base, binName string) bool {
	baseClean := strings.ToLower(strings.TrimSuffix(base, ".exe"))
	targetClean := strings.ToLower(strings.TrimSuffix(binName, ".exe"))
	if targetClean != "" && (baseClean == targetClean || baseClean == "antigravity" || baseClean == "agy" || baseClean == "cli") {
		return true
	}
	return baseClean == "antigravity" || baseClean == "agy" || baseClean == "cli"
}

func extractBinary(payload []byte, binName string) ([]byte, error) {
	if len(payload) >= 2 && payload[0] == 0x1f && payload[1] == 0x8b {
		gr, err := gzip.NewReader(bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		defer gr.Close()
		tr := tar.NewReader(gr)
		for {
			hdr, err := tr.Next()
			if err != nil {
				if err == io.EOF {
					break
				}
				return nil, err
			}
			if hdr.Typeflag == tar.TypeReg || hdr.Typeflag == tar.TypeRegA {
				base := filepath.Base(hdr.Name)
				if matchBinName(base, binName) {
					return io.ReadAll(tr)
				}
			}
		}
		return nil, fmt.Errorf("binary %q not found in archive", binName)
	}

	if len(payload) >= 4 && payload[0] == 'P' && payload[1] == 'K' && payload[2] == 0x03 && payload[3] == 0x04 {
		zr, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			base := filepath.Base(f.Name)
			if matchBinName(base, binName) {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(rc)
			}
		}
		return nil, fmt.Errorf("binary %q not found in zip archive", binName)
	}

	return payload, nil
}

// Run executes the agy update check replicating the exact behavior, formatting,
// and exit codes of the reference binary.
func Run(ctx context.Context, stdout, stderr io.Writer, opts Options) int {
	goos := opts.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	goarch := opts.GOARCH
	if goarch == "" {
		goarch = runtime.GOARCH
	}

	currentVersion := opts.CurrentVersion
	if currentVersion == "" {
		currentVersion = os.Getenv("AGY_CURRENT_VERSION")
		if currentVersion == "" {
			currentVersion = cli.ReferenceVersion
		}
	}

	manifestURL := opts.ManifestURL
	if manifestURL == "" {
		if env := os.Getenv("OPENAGY_AUTO_UPDATER_URL"); env != "" {
			manifestURL = endpoints.Set{AutoUpdater: env}.AutoUpdaterManifestURL(goos, goarch)
		} else {
			manifestURL = endpoints.Current().AutoUpdaterManifestURL(goos, goarch)
		}
	}

	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}

	fmt.Fprintf(stdout, "\u27f3 Checking for updates... (current version %s)\n", currentVersion)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		fetchErr := fmt.Errorf("failed to fetch manifest: %w", err)
		fmt.Fprint(stderr, FormatGlogError(time.Now(), "auto_updater.go:491", fmt.Sprintf("Failed to check for updates: %v", fetchErr)))
		fmt.Fprintf(stderr, "Error: failed to check for updates: %v\n", fetchErr)
		return 1
	}

	resp, err := client.Do(req)
	if err != nil {
		fetchErr := fmt.Errorf("failed to fetch manifest: %w", err)
		fmt.Fprint(stderr, FormatGlogError(time.Now(), "auto_updater.go:491", fmt.Sprintf("Failed to check for updates: %v", fetchErr)))
		fmt.Fprintf(stderr, "Error: failed to check for updates: %v\n", fetchErr)
		return 1
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fetchErr := fmt.Errorf("failed to fetch manifest: status %d", resp.StatusCode)
		fmt.Fprint(stderr, FormatGlogError(time.Now(), "auto_updater.go:491", fmt.Sprintf("Failed to check for updates: %v", fetchErr)))
		fmt.Fprintf(stderr, "Error: failed to check for updates: %v\n", fetchErr)
		return 1
	}

	var manifest Manifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		decodeErr := fmt.Errorf("failed to decode manifest: %w", err)
		fmt.Fprint(stderr, FormatGlogError(time.Now(), "auto_updater.go:491", fmt.Sprintf("Failed to check for updates: %v", decodeErr)))
		fmt.Fprintf(stderr, "Error: failed to check for updates: %v\n", decodeErr)
		return 1
	}

	if !IsNewerVersion(manifest.Version, currentVersion) {
		fmt.Fprintln(stdout, "\u2713 You are already on the latest version.")
		return 0
	}

	fmt.Fprintf(stdout, "\u2713 Found new version %s.\n", manifest.Version)
	fmt.Fprintln(stdout, "\u27f3 Downloading update...")

	downloadURL := manifest.PackageURL
	if downloadURL == "" {
		downloadURL = manifest.URL
	}
	if downloadURL == "" {
		dlErr := errors.New("no download URL in manifest")
		fmt.Fprint(stderr, FormatGlogError(time.Now(), "auto_updater.go:491", fmt.Sprintf("Failed to check for updates: %v", dlErr)))
		fmt.Fprintf(stderr, "Error: failed to check for updates: %v\n", dlErr)
		return 1
	}

	dlReq, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		dlErr := fmt.Errorf("failed to download update package: %w", err)
		fmt.Fprint(stderr, FormatGlogError(time.Now(), "auto_updater.go:491", fmt.Sprintf("Failed to check for updates: %v", dlErr)))
		fmt.Fprintf(stderr, "Error: failed to check for updates: %v\n", dlErr)
		return 1
	}
	dlResp, err := client.Do(dlReq)
	if err != nil {
		dlErr := fmt.Errorf("failed to download update package: %w", err)
		fmt.Fprint(stderr, FormatGlogError(time.Now(), "auto_updater.go:491", fmt.Sprintf("Failed to check for updates: %v", dlErr)))
		fmt.Fprintf(stderr, "Error: failed to check for updates: %v\n", dlErr)
		return 1
	}
	defer dlResp.Body.Close()
	if dlResp.StatusCode != http.StatusOK {
		dlErr := fmt.Errorf("failed to download update package: status %d", dlResp.StatusCode)
		fmt.Fprint(stderr, FormatGlogError(time.Now(), "auto_updater.go:491", fmt.Sprintf("Failed to check for updates: %v", dlErr)))
		fmt.Fprintf(stderr, "Error: failed to check for updates: %v\n", dlErr)
		return 1
	}
	payload, err := io.ReadAll(dlResp.Body)
	if err != nil {
		dlErr := fmt.Errorf("failed to read update package: %w", err)
		fmt.Fprint(stderr, FormatGlogError(time.Now(), "auto_updater.go:491", fmt.Sprintf("Failed to check for updates: %v", dlErr)))
		fmt.Fprintf(stderr, "Error: failed to check for updates: %v\n", dlErr)
		return 1
	}
	if manifest.Sha512 != "" {
		h := sha512.Sum512(payload)
		actual := hex.EncodeToString(h[:])
		if !strings.EqualFold(actual, manifest.Sha512) {
			cErr := fmt.Errorf("checksum mismatch: expected %s, got %s", manifest.Sha512, actual)
			fmt.Fprint(stderr, FormatGlogError(time.Now(), "auto_updater.go:491", fmt.Sprintf("Failed to check for updates: %v", cErr)))
			fmt.Fprintf(stderr, "Error: failed to check for updates: %v\n", cErr)
			return 1
		}
	} else if manifest.Sha256 != "" {
		h := sha256.Sum256(payload)
		actual := hex.EncodeToString(h[:])
		if !strings.EqualFold(actual, manifest.Sha256) {
			cErr := fmt.Errorf("checksum mismatch: expected %s, got %s", manifest.Sha256, actual)
			fmt.Fprint(stderr, FormatGlogError(time.Now(), "auto_updater.go:491", fmt.Sprintf("Failed to check for updates: %v", cErr)))
			fmt.Fprintf(stderr, "Error: failed to check for updates: %v\n", cErr)
			return 1
		}
	}

	targetName := ""
	if opts.TargetBinary != "" {
		targetName = filepath.Base(opts.TargetBinary)
	}
	extracted, err := extractBinary(payload, targetName)
	if err != nil {
		exErr := fmt.Errorf("failed to extract binary: %w", err)
		fmt.Fprint(stderr, FormatGlogError(time.Now(), "auto_updater.go:491", fmt.Sprintf("Failed to check for updates: %v", exErr)))
		fmt.Fprintf(stderr, "Error: failed to check for updates: %v\n", exErr)
		return 1
	}

	if opts.TargetBinary != "" {
		target := opts.TargetBinary
		if realTarget, err := filepath.EvalSymlinks(target); err == nil {
			target = realTarget
		}
		if err := atomicfile.WriteFile(target, extracted, 0o755); err != nil {
			wErr := fmt.Errorf("failed to replace binary: %w", err)
			fmt.Fprint(stderr, FormatGlogError(time.Now(), "auto_updater.go:491", fmt.Sprintf("Failed to check for updates: %v", wErr)))
			fmt.Fprintf(stderr, "Error: failed to check for updates: %v\n", wErr)
			return 1
		}
	}

	fmt.Fprintln(stdout, "\u2713 Update successful! Please restart agy.")
	return 0
}
