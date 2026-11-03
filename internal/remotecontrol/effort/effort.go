// Package effort — the closed set of reasoning-effort levels the
// remote-control surface accepts.
//
// The set is the dashboard's: a cascade's per-turn effort and a
// project's default effort are both drawn from it, and both
// surfaces answer -32602 ("Invalid params") when a caller sends
// a level outside the set.
//
// The vocabulary is shared, so it lives in its own leaf package
// rather than inside either surface: the cascade surface and the
// projects surface both consume it, and neither owns it.
//
// Stdlib-only. No third-party deps.
package effort

// Values is the closed set of effort strings the dashboard is
// allowed to send.
//
// Empty string is also valid ("use the surface's default") but
// is not in the set — callers MUST check IsValid against the
// resolved value (after a per-turn override is applied), not
// against the raw param value.
var Values = []string{"low", "medium", "high", "xhigh", "max"}

// IsValid reports whether s is one of the closed set of effort
// values. Empty is always valid (treated as "use the default"),
// so callers should check `s == "" || IsValid(s)`.
func IsValid(s string) bool {
	for _, v := range Values {
		if s == v {
			return true
		}
	}
	return false
}
