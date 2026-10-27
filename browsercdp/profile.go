package browsercdp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// sessionFiles are the files of a Chrome profile that carry a session.
//
// The cookies and the saved passwords are the reason a session survives:
// without them the browser starts at every site's login page. "Local
// State" holds the key they are encrypted with, so it has to come along
// as well — on Linux the key is in the file itself, on macOS it is
// encrypted with a key that lives in the keychain, and either way the
// file is what Chrome reads.
//
// The preferences are not part of it: they carry the chrome around the
// session — the theme, the extensions, the "restore the last session"
// setting — and a browser that opens the user's last twenty tabs is not
// what the clone's tools drive.
var sessionFiles = []string{
	"Network/Cookies",
	"Cookies",
	"Login Data",
}

// sqliteCompanions are the files that belong to a SQLite database next to
// it. A copy of the database without them can be missing what was written
// last.
var sqliteCompanions = []string{"-wal", "-shm", "-journal"}

// seedUserSession copies the user's own session into the profile the
// clone's Chrome uses.
//
// It is best effort, and it never fails the browser: a profile that
// cannot be read is skipped, and the clone starts with an empty session
// instead. That is not a rare case — on macOS the profile of another
// application is protected from an app without Full Disk Access, and the
// protection falls on the directories: reading a known file ("Local
// State") works, listing the directory does not 【一手 — measured on this
// machine: `ls ~/Library/Application Support/Google/Chrome` answers
// "Operation not permitted", `cat "…/Chrome/Local State"` answers】.
//
// The copy never touches the user's profile: it only reads from it.
func seedUserSession(dst string) string {
	for _, src := range chromeProfileDirs() {
		copied := false
		for _, profile := range profileNames(src) {
			for _, name := range sessionFiles {
				if copyIfReadable(filepath.Join(src, profile, name), filepath.Join(dst, profile, name)) {
					copied = true
				}
				for _, companion := range sqliteCompanions {
					copyIfReadable(
						filepath.Join(src, profile, name+companion),
						filepath.Join(dst, profile, name+companion),
					)
				}
			}
		}
		// The encryption key lives in the root of the user-data dir.
		if copyIfReadable(filepath.Join(src, "Local State"), filepath.Join(dst, "Local State")) {
			copied = true
		}
		if copied {
			return src
		}
	}
	return ""
}

// profileNames lists the profile dirs of a Chrome user-data dir.
//
// The names come from the "Local State" file, which holds the profile
// cache ("Default", "Profile 1", …). Reading that file is possible even
// where the profile dirs themselves are not: macOS protects the
// directories of another application's data, and the file in the root of
// the user-data dir is not one of them.
func profileNames(src string) []string {
	names := map[string]bool{}
	b, err := os.ReadFile(filepath.Join(src, "Local State"))
	if err == nil {
		var st struct {
			Profile struct {
				InfoCache map[string]json.RawMessage `json:"info_cache"`
			} `json:"profile"`
		}
		if err := json.Unmarshal(b, &st); err == nil {
			for name := range st.Profile.InfoCache {
				if name != "" {
					names[name] = true
				}
			}
		}
	}
	if len(names) == 0 {
		return []string{"Default"}
	}
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// copyIfReadable copies one file, and reports whether it did.
//
// A file that cannot be read is skipped: it is either not there, or it
// belongs to a profile the machine does not let the clone read. Neither
// is an error — the browser starts either way.
func copyIfReadable(src, dst string) bool {
	b, err := os.ReadFile(src)
	if err != nil {
		return false
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return false
	}
	// Write beside the target and move it into place, so a reader never
	// sees half a file.
	tmp := dst + ".pigasus-tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return false
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return false
	}
	return true
}
