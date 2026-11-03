// Package marketinstall installs a plugin from a cached marketplace feed.
//
// The reference's marketplace feed is the `marketplace.json` the CLI
// caches under `<antigravity home>/cache/marketplaces/<marketplace>/`.
// Every entry names its own archive:
//
//	{"name": "firebase", "version": "1.0.3",
//	 "source": {"type": "archive",
//	            "url": "https://www.gstatic.com/antigravity/plugins/firebase/firebase.zip?v=f963859c6778",
//	            "sha256": "f963859c67781a59de295d55374bb26b48957cc660e1f7394b5a81469d0d24eb"}}
//
// 【一手】 the shape above is read off this machine's cached feed
// (`~/.gemini/antigravity-cli/cache/marketplaces/antigravity-plugins-official/marketplace.json`);
// the digest the feed carries is the digest of the file the URL serves.
//
// The archive is a zip whose root carries the plugin's own files — the
// `plugin.json`, the `agents/` and the `skills/` directories. The
// installer downloads it, verifies the digest the feed carries and
// unpacks it: the digest is the only integrity check the feed offers, so
// it is not optional.
//
// The package owns the *fetch*; the placement is the caller's, because
// the reference's installer kills the plugin's own MCP processes before
// it replaces an installed copy, and that step lives with the installer.
package marketinstall

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/features/plugins/core"
)

// DefaultMarketplace is the marketplace a bare plugin name resolves to.
//
// 【一手】 official-docs/marketplace.md:149-153 — "or pass a bare
// <plugin-name>, which defaults to <plugin-name>@antigravity-plugins-official".
const DefaultMarketplace = "antigravity-plugins-official"

// downloadTimeout bounds one archive download.
const downloadTimeout = 2 * time.Minute

// Entry is one marketplace entry, resolved for installation.
type Entry struct {
	// Marketplace is the marketplace the entry was found in — the
	// directory name under the cache root.
	Marketplace string
	// Name is the plugin's name — the directory it installs into.
	Name string
	// Version is the version the feed carries.
	Version string
	// Description is the feed's one-line description.
	Description string
	// URL is the archive's own URL.
	URL string
	// SHA256 is the archive's digest, in hex. The feed always carries
	// one, and the installer refuses an archive it cannot verify.
	SHA256 string
}

// ErrRepositoryURL is returned by SplitTarget for a target that names a
// repository rather than a plugin.
//
// The reference clones a GitHub URL and stages the repository; the clone
// does not implement that half, so the caller reports the limitation
// rather than pretending the target was a plugin name.
var ErrRepositoryURL = errors.New("the target names a repository")

// Target is one resolved install target.
type Target struct {
	// Name is the plugin's name — the entry the marketplace feed is
	// searched for.
	Name string
	// Marketplace is the marketplace the plugin is looked up in.
	Marketplace string
	// Explicit is true when the target named its marketplace after an
	// "@" of its own. A bare name is not explicit: it resolves to the
	// official marketplace by default.
	Explicit bool
}

// SplitTarget splits an install target into its plugin name and its
// marketplace.
//
// The documented form is `<plugin-name>@<marketplace>`; a bare name
// resolves to the official marketplace.
func SplitTarget(target string) (Target, error) {
	t := strings.TrimSpace(target)
	if t == "" {
		return Target{}, errors.New("the install target is empty")
	}
	if strings.Contains(t, "://") || strings.HasPrefix(t, "git@") {
		return Target{}, fmt.Errorf("%w: %s", ErrRepositoryURL, t)
	}
	out := Target{Name: t, Marketplace: DefaultMarketplace}
	if i := strings.LastIndex(t, "@"); i >= 0 {
		out.Name = t[:i]
		out.Marketplace = t[i+1:]
		out.Explicit = true
	}
	out.Name = strings.TrimSpace(out.Name)
	out.Marketplace = strings.TrimSpace(out.Marketplace)
	if out.Name == "" {
		return Target{}, errors.New("the install target names no plugin")
	}
	if out.Marketplace == "" {
		return Target{}, errors.New("the install target names no marketplace")
	}
	return out, nil
}

// Lookup resolves one plugin name in one marketplace's cached feed.
//
// The feed is read from `<antigravity home>/cache/marketplaces/<marketplace>/marketplace.json`.
func Lookup(marketplace, name string) (Entry, error) {
	marketplace = strings.TrimSpace(marketplace)
	name = strings.TrimSpace(name)
	if marketplace == "" {
		return Entry{}, errors.New("no marketplace was named")
	}
	if name == "" {
		return Entry{}, errors.New("no plugin was named")
	}
	doc, ok := core.MarketplaceDocByDir(marketplace)
	if !ok {
		return Entry{}, fmt.Errorf("marketplace %q is not available", marketplace)
	}
	for _, p := range doc.Plugins {
		if p.Name != name {
			continue
		}
		entry := Entry{
			Marketplace: marketplace,
			Name:        p.Name,
			Version:     p.Version,
			Description: p.Description,
		}
		if p.Source != nil {
			entry.URL = p.Source.URL
			entry.SHA256 = p.Source.SHA256
		}
		return entry, nil
	}
	return Entry{}, fmt.Errorf("plugin %q is not in marketplace %q", name, marketplace)
}

// Fetch downloads the entry's archive, verifies the digest the feed
// carries and unpacks it.
//
// It returns the directory that holds the plugin's own files, and a
// cleanup function that removes the temporary tree. The caller owns the
// directory and must call the cleanup.
func Fetch(entry Entry) (string, func(), error) {
	if strings.TrimSpace(entry.URL) == "" {
		return "", nil, fmt.Errorf("the marketplace entry for %q carries no archive", entry.Name)
	}
	if strings.TrimSpace(entry.SHA256) == "" {
		return "", nil, fmt.Errorf("the marketplace entry for %q carries no digest", entry.Name)
	}
	tmp, err := os.MkdirTemp("", "pigasus-marketplace-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	archive := filepath.Join(tmp, "archive.zip")
	if err := download(entry.URL, archive); err != nil {
		cleanup()
		return "", nil, err
	}
	if err := verifyDigest(archive, entry.SHA256); err != nil {
		cleanup()
		return "", nil, err
	}
	unpacked := filepath.Join(tmp, "unpacked")
	if err := os.MkdirAll(unpacked, 0700); err != nil {
		cleanup()
		return "", nil, err
	}
	if err := unzip(archive, unpacked); err != nil {
		cleanup()
		return "", nil, err
	}
	root, err := pluginRoot(unpacked)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	return root, cleanup, nil
}

// download writes the archive's bytes to path.
func download(url, path string) error {
	client := &http.Client{Timeout: downloadTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the archive download answered %s", resp.Status)
	}
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// verifyDigest checks the archive against the digest the feed carries.
func verifyDigest(path, want string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, strings.TrimSpace(want)) {
		return fmt.Errorf("the archive's digest is %s, the feed says %s", got, want)
	}
	return nil
}

// unzip unpacks the archive into dst.
//
// A path that escapes dst — an absolute path, or one that climbs out with
// ".." — is refused: an archive is remote input.
func unzip(src, dst string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		name := filepath.Clean(filepath.FromSlash(f.Name))
		if name == "." || filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
			return fmt.Errorf("the archive holds an unsafe path %q", f.Name)
		}
		target := filepath.Join(dst, name)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		if err := writeMember(f, target); err != nil {
			return err
		}
	}
	return nil
}

// writeMember writes one archive member to path.
func writeMember(f *zip.File, path string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	mode := f.Mode().Perm()
	if mode == 0 {
		mode = 0644
	}
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// pluginRoot finds the directory that holds the plugin's own files.
//
// The reference's archives carry the plugin's files at the archive's root
// — the plugin.json, the agents/ and the skills/ directories — but an
// archive that wraps them in a single directory is read through that
// directory too.
func pluginRoot(dir string) (string, error) {
	if _, err := os.Stat(filepath.Join(dir, "plugin.json")); err == nil {
		return dir, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	if len(dirs) == 1 {
		root := filepath.Join(dir, dirs[0])
		if _, err := os.Stat(filepath.Join(root, "plugin.json")); err == nil {
			return root, nil
		}
	}
	return "", errors.New("the archive does not hold a plugin.json")
}
