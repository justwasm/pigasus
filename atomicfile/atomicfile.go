// Package atomicfile writes a file in one step: a sibling temp file, a
// fsync, and a rename over the target. A reader either sees the old
// file or the new one, never a half-written one.
//
// On non-Windows platforms, it is a thin wrapper over github.com/google/renameio/v2,
// and the wrapper earns its keep on one point: the file's mode.
//
// renameio.WriteFile copies the *existing* file's mode onto the new
// file (WithExistingPermissions is in its default option set), and a
// static mode passed alongside is overwritten by that copy. The clone's
// own writers have always written 0600 — these files hold API keys,
// OAuth tokens and account records — so the mode must not depend on
// what a file happened to be before.
package atomicfile
