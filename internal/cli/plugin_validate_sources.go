package cli

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/features/plugins/validate"
)

// maxPluginArchiveBytes is 100 MiB (104,857,600 bytes), matching upstream reference.
const maxPluginArchiveBytes = 100 * 1024 * 1024

// resolveValidateSource handles local dirs, local .zip files, and remote http(s) URLs
// (both standalone zip archives and GitHub repository URLs).
func resolveValidateSource(input string, out io.Writer) (dir string, manifestRel string, isArchive bool, archiveSource string, cleanup func(), errRes *validateResult) {
	lower := strings.ToLower(input)

	// Case 1: Remote URL (http:// or https://)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		if out != nil {
			fmt.Fprintf(out, "Fetching %s...\n", input)
		}
		u, err := url.Parse(input)
		if err != nil {
			return "", "", false, "", nil, validateFileError(input, fmt.Sprintf("invalid URL %q: %v", input, err))
		}

		if isGitHubRepoURL(u) {
			return resolveGitHubValidateSource(input, u)
		}
		return resolveRemoteZipValidateSource(input)
	}

	// Case 2: Local .zip file
	if strings.HasSuffix(lower, ".zip") {
		return resolveLocalZipValidateSource(input)
	}

	// Case 3: Standard local directory or file
	dir, manifestRel, errRes = resolveValidateDirAndManifest(input)
	return dir, manifestRel, false, "", nil, errRes
}

func isGitHubRepoURL(u *url.URL) bool {
	host := strings.ToLower(u.Host)
	if host != "github.com" && host != "www.github.com" {
		return false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	return len(parts) >= 2
}

func resolveGitHubValidateSource(rawURL string, u *url.URL) (string, string, bool, string, func(), *validateResult) {
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	owner := parts[0]
	repo := strings.TrimSuffix(parts[1], ".git")
	branch := ""
	subpath := ""

	// Check if URL specifies /tree/<branch>/<path>
	if len(parts) >= 4 && parts[2] == "tree" {
		branch = parts[3]
		if len(parts) > 4 {
			subpath = strings.Join(parts[4:], "/")
		}
	}

	tmpDir, err := os.MkdirTemp("", "plugin-validate-*")
	if err != nil {
		return "", "", false, "", nil, validateFileError(rawURL, fmt.Sprintf("failed to create temp directory: %v", err))
	}
	cleanup := func() { _ = os.RemoveAll(tmpDir) }

	cloneDir := filepath.Join(tmpDir, repo)
	repoURL := fmt.Sprintf("https://github.com/%s/%s.git", owner, repo)

	args := []string{"clone", "--depth", "1"}
	if branch != "" {
		args = append(args, "--branch", branch)
	}
	args = append(args, repoURL, cloneDir)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	cmdOut, err := cmd.CombinedOutput()
	if err != nil {
		cleanup()
		msg := fmt.Sprintf("can't clone %s: git clone failed: %v, output: %s", repoURL, err, strings.TrimSpace(string(cmdOut)))
		return "", "", false, "", nil, validateFileError(rawURL, msg)
	}

	targetDir := cloneDir
	if subpath != "" {
		targetDir = filepath.Join(cloneDir, filepath.FromSlash(subpath))
	}

	manifestRel := locateManifestInSubdirs(targetDir)
	if manifestRel == "" {
		cleanup()
		return "", "", false, "", nil, validateManifestNotFoundError(rawURL)
	}

	return targetDir, manifestRel, false, "", cleanup, nil
}

func resolveRemoteZipValidateSource(rawURL string) (string, string, bool, string, func(), *validateResult) {
	client := &http.Client{Timeout: 3 * time.Minute}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return "", "", false, "", nil, validateFileError(rawURL, fmt.Sprintf("failed to create request: %v", err))
	}
	req.Header.Set("User-Agent", "antigravity-cli/"+ReferenceVersion)

	resp, err := client.Do(req)
	if err != nil {
		return "", "", false, "", nil, validateFileError(rawURL, fmt.Sprintf("can't read %s: %v", rawURL, err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", false, "", nil, validateFileError(rawURL, fmt.Sprintf("archive %q returned status %d", rawURL, resp.StatusCode))
	}

	tmpZip, err := os.CreateTemp("", "plugin-archive-*.zip")
	if err != nil {
		return "", "", false, "", nil, validateFileError(rawURL, fmt.Sprintf("failed to create temp archive: %v", err))
	}
	tmpZipPath := tmpZip.Name()
	defer os.Remove(tmpZipPath)

	lr := io.LimitReader(resp.Body, maxPluginArchiveBytes+1)
	written, err := io.Copy(tmpZip, lr)
	_ = tmpZip.Close()
	if err != nil {
		return "", "", false, "", nil, validateFileError(rawURL, fmt.Sprintf("failed to download archive: %v", err))
	}
	if written > maxPluginArchiveBytes {
		return "", "", false, "", nil, validateFileError(rawURL, fmt.Sprintf("%s is larger than 100 MiB, the largest plugin zip that the plugin marketplace in Antigravity downloads", rawURL))
	}

	dir, manifestRel, _, _, cleanup, errRes := resolveLocalZipValidateSource(tmpZipPath)
	if errRes != nil {
		// Retain rawURL as header
		errRes.HeaderPath = rawURL
		return "", "", false, "", nil, errRes
	}
	return dir, manifestRel, true, rawURL, cleanup, nil
}

func resolveLocalZipValidateSource(zipPath string) (string, string, bool, string, func(), *validateResult) {
	fi, err := os.Stat(zipPath)
	if err != nil {
		return "", "", false, "", nil, validateDirStatError(zipPath)
	}
	if fi.Size() > maxPluginArchiveBytes {
		return "", "", false, "", nil, validateFileError(zipPath, fmt.Sprintf("%s is larger than 100 MiB, the largest plugin zip that the plugin marketplace in Antigravity downloads", zipPath))
	}

	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", "", false, "", nil, validateFileError(zipPath, "failed to read archive: "+err.Error())
	}
	defer zr.Close()

	tmpDir, err := os.MkdirTemp("", "openagy-plugin-validate-*")
	if err != nil {
		return "", "", false, "", nil, validateFileError(zipPath, fmt.Sprintf("failed to create temp directory: %v", err))
	}
	cleanup := func() { _ = os.RemoveAll(tmpDir) }

	// Extract zip entries safely
	for _, f := range zr.File {
		clean, ok := validate.ArchiveEntryPath(f.Name)
		if !ok {
			continue // skip illegal paths
		}
		target := filepath.Join(tmpDir, clean)
		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(target, 0755)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			cleanup()
			return "", "", false, "", nil, validateFileError(zipPath, fmt.Sprintf("failed to unpack archive: %v", err))
		}
		rc, err := f.Open()
		if err != nil {
			cleanup()
			return "", "", false, "", nil, validateFileError(zipPath, fmt.Sprintf("failed to unpack archive entry %s: %v", f.Name, err))
		}
		dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			cleanup()
			return "", "", false, "", nil, validateFileError(zipPath, fmt.Sprintf("failed to create unpacked file %s: %v", target, err))
		}
		_, copyErr := io.Copy(dst, rc)
		rc.Close()
		dst.Close()
		if copyErr != nil {
			cleanup()
			return "", "", false, "", nil, validateFileError(zipPath, fmt.Sprintf("failed to extract file %s: %v", f.Name, copyErr))
		}
	}

	// Find plugin.json. First check tmpDir directly.
	manifestRel := locateManifestInSubdirs(tmpDir)
	targetDir := tmpDir
	if manifestRel == "" {
		// Check if archive contains a single root folder
		entries, readErr := os.ReadDir(tmpDir)
		if readErr == nil && len(entries) == 1 && entries[0].IsDir() {
			sub := filepath.Join(tmpDir, entries[0].Name())
			if rel := locateManifestInSubdirs(sub); rel != "" {
				targetDir = sub
				manifestRel = rel
			}
		}
	}

	if manifestRel == "" {
		cleanup()
		return "", "", false, "", nil, validateManifestNotFoundError(zipPath)
	}

	return targetDir, manifestRel, true, zipPath, cleanup, nil
}

func validateFileError(input, message string) *validateResult {
	return &validateResult{
		Result: validate.Result{Report: validate.Report{
			Dir:      input,
			Findings: []validate.Finding{{Severity: validate.SeverityError, File: ".", Message: message}},
		}},
		HeaderPath: input,
	}
}
