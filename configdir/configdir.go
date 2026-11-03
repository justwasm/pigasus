// Package configdir resolves the clone's configuration home.
//
// # One home
//
// The clone used to keep its configuration in two places:
//
//   - $HOME/.config/openagy/ — the account registry, the provider registry,
//     the configuration document, the remote-control instance id;
//   - $HOME/.openagy/ — the proxy's capture directory, its log and its own
//     configuration file.
//
// Two homes means two things to back up, two things to remember, and a
// half-migrated installation when only one of them is copied. There is one
// home now: $HOME/.openagy.
//
// # The layout
//
//	config.json      the configuration document — the endpoint overrides, the
//	                 account-routing policy and the proxy's own state, under
//	                 a "wrapper" section
//	providers.json   the provider registry (internal/models)
//	accounts.json    the account registry (internal/features/accounts)
//	remote-control-instance-id
//	                 the installation uuid (internal/infra/remote/persistent)
//	captures/        the proxy's captured traffic (internal/agyproxy)
//	wrapper.log      the proxy's log (internal/agyproxy)
package configdir

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	// EnvHome overrides the configuration home. It is the one knob for
	// "put the clone's configuration somewhere else".
	EnvHome = "OPENAGY_CONFIG_HOME"

	// homeName is the configuration home's directory name under $HOME.
	homeName = ".openagy"
)

// Dir returns the configuration home:
//
//	$OPENAGY_CONFIG_HOME, or
//	$HOME/.openagy if HOME is set.
//
// It returns "" when neither is set — there is no home to write to, and
// every caller has to cope with that.
func Dir() string {
	if p := strings.TrimSpace(os.Getenv(EnvHome)); p != "" {
		return p
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, homeName)
	}
	return ""
}

// Path returns the path of a file in the configuration home. It returns ""
// when there is no home.
func Path(name string) string {
	dir := Dir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, name)
}
