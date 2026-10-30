package bin

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
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/endpoints"
)

// EnvNoAutoDownload disables auto-download when set to truthy ("1", "true").
const EnvNoAutoDownload = "AGY_NO_AUTO_DOWNLOAD"

var (
	disableAutoDownload   bool
	disableAutoDownloadMu sync.RWMutex
)

// SetDisableAutoDownload sets the testing toggle for auto-download.
func SetDisableAutoDownload(disable bool) {
	disableAutoDownloadMu.Lock()
	defer disableAutoDownloadMu.Unlock()
	disableAutoDownload = disable
}

func autoDownloadAllowed() bool {
	disableAutoDownloadMu.RLock()
	defer disableAutoDownloadMu.RUnlock()
	if disableAutoDownload {
		return false
	}
	val := os.Getenv(EnvNoAutoDownload)
	if val == "1" || strings.EqualFold(val, "true") {
		return false
	}
	return true
}

type manifest struct {
	Version    string `json:"version"`
	URL        string `json:"url,omitempty"`
	PackageURL string `json:"package_url,omitempty"`
	SHA512     string `json:"sha512,omitempty"`
	SHA256     string `json:"sha256,omitempty"`
	Sha512Alt  string `json:"sha_512,omitempty"`
	Sha256Alt  string `json:"sha_256,omitempty"`
}

// CacheDir returns the persistent directory where downloaded binaries are placed.
// Priority: $XDG_CACHE_HOME/openagy/bin, $HOME/.openagy/bin, or $TEMP/openagy/bin.
func CacheDir() string {
	if cache := os.Getenv("XDG_CACHE_HOME"); cache != "" {
		return filepath.Join(cache, "openagy", "bin")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".openagy", "bin")
	}
	return filepath.Join(os.TempDir(), "openagy", "bin")
}

// CacheBinaryName returns "agy" ("agy.exe" on Windows).
func CacheBinaryName() string {
	if runtime.GOOS == "windows" {
		return "agy.exe"
	}
	return "agy"
}

// CacheBinaryPath returns the full path to the cached agy binary.
func CacheBinaryPath() string {
	return filepath.Join(CacheDir(), CacheBinaryName())
}

// AutoDownload fetches the manifest for the current GOOS/GOARCH, downloads
// the agy tar.gz archive, verifies its checksum, and extracts the binary to
// the persistent cache directory.
func AutoDownload(ctx context.Context) (string, error) {
	manifestURL := endpoints.Current().AutoUpdaterManifestURL(runtime.GOOS, runtime.GOARCH)
	if env := os.Getenv("OPENAGY_AUTO_UPDATER_URL"); env != "" {
		manifestURL = endpoints.Set{AutoUpdater: env}.AutoUpdaterManifestURL(runtime.GOOS, runtime.GOARCH)
	} else if env := os.Getenv("AGY_AUTO_UPDATER_URL"); env != "" {
		manifestURL = endpoints.Set{AutoUpdater: env}.AutoUpdaterManifestURL(runtime.GOOS, runtime.GOARCH)
	}
	if manifestURL == "" {
		manifestURL = fmt.Sprintf("%s/manifests/%s_%s.json", endpoints.DefaultAutoUpdater, runtime.GOOS, runtime.GOARCH)
	}

	client := &http.Client{Timeout: 10 * time.Minute}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return "", fmt.Errorf("manifest request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch manifest: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch manifest: HTTP %d", resp.StatusCode)
	}

	var m manifest
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return "", fmt.Errorf("decode manifest: %w", err)
	}

	downloadURL := m.URL
	if downloadURL == "" {
		downloadURL = m.PackageURL
	}
	if downloadURL == "" {
		return "", fmt.Errorf("manifest missing download url")
	}

	sha512Val := strings.TrimSpace(m.SHA512)
	if sha512Val == "" {
		sha512Val = strings.TrimSpace(m.Sha512Alt)
	}
	sha256Val := strings.TrimSpace(m.SHA256)
	if sha256Val == "" {
		sha256Val = strings.TrimSpace(m.Sha256Alt)
	}
	if sha512Val == "" && sha256Val == "" {
		return "", fmt.Errorf("manifest missing checksum (sha512 or sha256)")
	}

	// User feedback before starting download
	ver := strings.TrimSpace(m.Version)
	if ver != "" {
		fmt.Fprintf(os.Stderr, "⟳ Downloading agy %s...\n", ver)
	} else {
		fmt.Fprintf(os.Stderr, "⟳ Downloading agy...\n")
	}

	destDir := CacheDir()
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}

	tempArchive, err := os.CreateTemp(destDir, "agy-download-*.tar.gz")
	if err != nil {
		return "", fmt.Errorf("create temp archive: %w", err)
	}
	tempArchiveName := tempArchive.Name()
	defer os.Remove(tempArchiveName)

	reqDl, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		tempArchive.Close()
		return "", fmt.Errorf("archive request: %w", err)
	}
	respDl, err := client.Do(reqDl)
	if err != nil {
		tempArchive.Close()
		return "", fmt.Errorf("download archive: %w", err)
	}
	defer respDl.Body.Close()
	if respDl.StatusCode != http.StatusOK {
		tempArchive.Close()
		return "", fmt.Errorf("download archive: HTTP %d", respDl.StatusCode)
	}

	h512 := sha512.New()
	h256 := sha256.New()
	writer := io.MultiWriter(tempArchive, h512, h256)
	if _, err := io.Copy(writer, respDl.Body); err != nil {
		tempArchive.Close()
		return "", fmt.Errorf("write archive: %w", err)
	}
	tempArchive.Close()

	if sha512Val != "" {
		actual512 := hex.EncodeToString(h512.Sum(nil))
		if !strings.EqualFold(actual512, sha512Val) {
			return "", fmt.Errorf("checksum mismatch (sha512): got %s, want %s", actual512, sha512Val)
		}
	} else {
		actual256 := hex.EncodeToString(h256.Sum(nil))
		if !strings.EqualFold(actual256, sha256Val) {
			return "", fmt.Errorf("checksum mismatch (sha256): got %s, want %s", actual256, sha256Val)
		}
	}

	magic := make([]byte, 512)
	af, err := os.Open(tempArchiveName)
	if err != nil {
		return "", fmt.Errorf("open archive: %w", err)
	}
	n, _ := io.ReadFull(af, magic)
	af.Close()
	magic = magic[:n]

	isGzip := bytes.HasPrefix(magic, []byte{0x1f, 0x8b})
	isZip := bytes.HasPrefix(magic, []byte{'P', 'K', 0x03, 0x04})
	isExe := bytes.HasPrefix(magic, []byte{'M', 'Z'}) ||
		bytes.HasPrefix(magic, []byte{0x7f, 'E', 'L', 'F'}) ||
		bytes.HasPrefix(magic, []byte{0xcf, 0xfa, 0xed, 0xfe}) ||
		bytes.HasPrefix(magic, []byte{0xfe, 0xed, 0xfa, 0xcf}) ||
		bytes.HasPrefix(magic, []byte{0xca, 0xfe, 0xba, 0xbe}) ||
		bytes.HasPrefix(magic, []byte{0xbe, 0xba, 0xfe, 0xca}) ||
		bytes.HasPrefix(magic, []byte{'#', '!'}) ||
		strings.HasSuffix(strings.ToLower(downloadURL), ".exe")

	targetPath := CacheBinaryPath()
	if isGzip || strings.HasSuffix(strings.ToLower(downloadURL), ".tar.gz") || strings.HasSuffix(strings.ToLower(downloadURL), ".tgz") {
		if err := extractFromTar(tempArchiveName, destDir, targetPath); err != nil {
			return "", err
		}
	} else if isZip || strings.HasSuffix(strings.ToLower(downloadURL), ".zip") {
		if err := extractFromZip(tempArchiveName, destDir, targetPath); err != nil {
			return "", err
		}
	} else if isExe {
		if err := installRawExecutable(tempArchiveName, destDir, targetPath); err != nil {
			return "", err
		}
	} else {
		if err := extractFromTar(tempArchiveName, destDir, targetPath); err != nil {
			if errRaw := installRawExecutable(tempArchiveName, destDir, targetPath); errRaw != nil {
				return "", fmt.Errorf("extract archive: %w (fallback: %v)", err, errRaw)
			}
		}
	}
	return targetPath, nil
}

func installBinary(src io.Reader, destDir, targetPath string, isAntigravity bool) error {
	tempBinary, err := os.CreateTemp(destDir, "agy-bin-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp binary: %w", err)
	}
	tempBinaryName := tempBinary.Name()
	defer os.Remove(tempBinaryName)

	if _, err := io.Copy(tempBinary, src); err != nil {
		tempBinary.Close()
		return fmt.Errorf("extract binary: %w", err)
	}
	if err := tempBinary.Close(); err != nil {
		return fmt.Errorf("close temp binary: %w", err)
	}
	if err := os.Chmod(tempBinaryName, 0o755); err != nil {
		return fmt.Errorf("chmod binary: %w", err)
	}
	_ = os.Remove(targetPath)
	if err := os.Rename(tempBinaryName, targetPath); err != nil {
		return fmt.Errorf("install binary: %w", err)
	}
	if isAntigravity {
		ext := ""
		if runtime.GOOS == "windows" {
			ext = ".exe"
		}
		altPath := filepath.Join(destDir, "antigravity"+ext)
		if altPath != targetPath {
			linkOrCopy(targetPath, altPath)
		}
	}
	return nil
}

func extractFromTar(archivePath, destDir, targetPath string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	var fallbackTemp *os.File
	defer func() {
		if fallbackTemp != nil {
			fbName := fallbackTemp.Name()
			_ = fallbackTemp.Close()
			_ = os.Remove(fbName)
		}
	}()

	var regularFilesCount int
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA && hdr.Typeflag != 0 {
			continue
		}
		regularFilesCount++
		base := strings.ToLower(filepath.Base(hdr.Name))
		if base == "antigravity" || base == "antigravity.exe" || base == "agy" || base == "agy.exe" {
			isAntigravity := strings.HasPrefix(base, "antigravity")
			return installBinary(tr, destDir, targetPath, isAntigravity)
		}

		if regularFilesCount == 1 {
			tmp, err := os.CreateTemp(destDir, "agy-fallback-*.tmp")
			if err == nil {
				if _, err := io.Copy(tmp, tr); err == nil {
					fallbackTemp = tmp
				} else {
					_ = tmp.Close()
					_ = os.Remove(tmp.Name())
				}
			}
		}
	}

	if regularFilesCount == 1 && fallbackTemp != nil {
		fbName := fallbackTemp.Name()
		_ = fallbackTemp.Close()
		fallbackTemp = nil
		defer os.Remove(fbName)
		_ = os.Chmod(fbName, 0o755)
		if err := os.Rename(fbName, targetPath); err != nil {
			return fmt.Errorf("install fallback binary: %w", err)
		}
		return nil
	}

	return fmt.Errorf("no agy or antigravity binary found in downloaded archive")
}

func extractFromZip(archivePath, destDir, targetPath string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer zr.Close()

	var fallbackFile *zip.File
	var regularFilesCount int

	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			continue
		}
		regularFilesCount++
		base := strings.ToLower(filepath.Base(zf.Name))
		if base == "antigravity" || base == "antigravity.exe" || base == "agy" || base == "agy.exe" {
			rc, err := zf.Open()
			if err != nil {
				return fmt.Errorf("open zip file %s: %w", zf.Name, err)
			}
			defer rc.Close()
			isAntigravity := strings.HasPrefix(base, "antigravity")
			return installBinary(rc, destDir, targetPath, isAntigravity)
		}
		if regularFilesCount == 1 {
			fallbackFile = zf
		}
	}

	if regularFilesCount == 1 && fallbackFile != nil {
		rc, err := fallbackFile.Open()
		if err != nil {
			return fmt.Errorf("open fallback zip file: %w", err)
		}
		defer rc.Close()
		return installBinary(rc, destDir, targetPath, false)
	}

	return fmt.Errorf("no agy or antigravity binary found in downloaded zip")
}

func installRawExecutable(archivePath, destDir, targetPath string) error {
	if err := os.Chmod(archivePath, 0o755); err != nil {
		return fmt.Errorf("chmod binary: %w", err)
	}
	_ = os.Remove(targetPath)
	if err := os.Rename(archivePath, targetPath); err != nil {
		return fmt.Errorf("install binary: %w", err)
	}
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	altPath := filepath.Join(destDir, "antigravity"+ext)
	if altPath != targetPath {
		linkOrCopy(targetPath, altPath)
	}
	return nil
}

func linkOrCopy(src, dst string) {
	_ = os.Remove(dst)
	if err := os.Link(src, dst); err == nil {
		return
	}
	if err := os.Symlink(src, dst); err == nil {
		return
	}
	sf, err := os.Open(src)
	if err != nil {
		return
	}
	defer sf.Close()
	df, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return
	}
	defer df.Close()
	_, _ = io.Copy(df, sf)
}

