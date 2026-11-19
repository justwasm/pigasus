package atomicfile

import "github.com/justwasm/pigasus/configdir"

// guard keeps a test binary from replacing this machine's own configuration.
//
// Every configuration write in the clone goes through WriteFile — the account
// registry (plaintext credentials and the pool's policy), the configuration
// document, the provider registry, the permission store — so this is the one
// place where "a test may not touch the user's configuration" can be enforced
// for all of them at once.
//
// It is enforced here rather than only in the test helper because a path is
// resolved when the write happens: a test that repoints $HOME has that setting
// restored when it ends, and a deferred save or a background goroutine then
// resolves the user's real home again. See configdir's guard.go.
func guard(path string) error {
	return configdir.RefuseTestWrite(path)
}
