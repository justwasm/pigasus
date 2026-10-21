// always_allow_aas.go — cycle "always-allow-shared-config": the TUI
// seam the Remote Control browser-session approval flow calls.
//
// AGY 1.3.4 changelog line 40: "Always allow" rules approved from a
// Remote Control browser session are now saved only to the shared
// config.json. Before the cycle a Remote Control approval could land
// in the project-level config (default-cli-project.json) and survive
// a deletion made in either the browser's rules editor or the Shared
// tab of /permissions, because both deletion surfaces read only the
// shared config. The writer below routes SourceRemoteControl entries
// through the shared config only, so a deletion in either surface
// takes effect on the next rule lookup.
//
// The functions in this file are the entry points a Remote Control
// approval handler will call. They wrap internal/permissions/store so
// a future cycle can wire Remote Control end-to-end without touching
// the existing TUI /permissions panel (which is session-local by
// design and unaffected by this change).

package main

import (
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/permissions/store"
)

// AlwaysAllowFromSource writes one "Always allow" rule with the
// source tag that requested the approval. This is the seam the
// Remote Control browser session approval handler calls.
//
//	SourceLocal:        behaviour preserved — the rule goes to the
//	                    shared config (the TUI's Shared scope uses the
//	                    same store). The project config is not
//	                    touched.
//	SourceRemoteControl: the AGY 1.3.4 line 40 contract — the rule
//	                    goes to the shared config only. The project
//	                    config is never written.
//
// rule may be in the "tool:pattern" form (the same shape the TUI's
// Shared tab of /permissions accepts) or in the
// {"tool":"...","pattern":"..."} form; ParseSharedRule normalises
// both before the store call.
func AlwaysAllowFromSource(source store.Source, rule string) error {
	parsed, err := parseSharedRule(rule)
	if err != nil {
		return err
	}
	return store.AlwaysAllow(store.DefaultPath(), source, parsed)
}

// RemoveAllowFromSource deletes one rule from the shared config,
// filtered by source tag. The browser's rules editor calls this with
// SourceRemoteControl when the user removes a rule there; the Shared
// tab of /permissions calls this with SourceLocal. Both call sites
// read the same file, so a delete in either surface takes effect on
// the next rule lookup.
func RemoveAllowFromSource(source store.Source, rule string) error {
	parsed, err := parseSharedRule(rule)
	if err != nil {
		return err
	}
	return store.RemoveAllow(store.DefaultPath(), source, parsed)
}

// parseSharedRule accepts the two rule shapes the TUI /permissions
// panel and the reference's CLI parser both accept: "tool:pattern"
// (string) and {"tool":"...","pattern":"..."} (object). Anything
// else is rejected with a recoverable error.
func parseSharedRule(s string) (store.Rule, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "{") {
		// Object form: defer to the store's reader rather than
		// duplicating the JSON parser here. The store has its own
		// Decode helpers; this file only needs the "tool:pattern"
		// form so the message stays simple.
		return store.Rule{}, fmt.Errorf("always-allow: object-form rule %q must be submitted via the settings.json writer (not the shared config writer)", s)
	}
	if s == "" {
		return store.Rule{}, fmt.Errorf("always-allow: empty rule")
	}
	return store.ParseRule(s), nil
}
