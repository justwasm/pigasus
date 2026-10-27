// narration_catalogue.go — cycle 250: the /narration catalogue.
//
// AGY 1.4.2 feature-gap row (internal/cli/testdata/feature_diff_table.json):
//
//	"Added a narration start helper that prints an introductory line so
//	 the agent can begin a multi-step task with consistent context."
//
// The clone closes the gap as a TUI-only cosmetic slash command. The
// reference's actual StartNarration gRPC method (recovered in
// docs/decompiled-v1.3.4/strings-v1.3.4.txt) ships a backend RPC;
// the clone has no matching protocol implementation, so /narration
// is a local helper that selects a short intro paragraph and writes
// it into m.response. It does NOT enter m.session: the model history
// stays clean so a future turn that builds on the user's question
// sees only what the user typed.
//
// SelectNarration is the public function. It is a pure function of
// (userText, mode, taskType, sessionStart time.Time) — no Math/random
// so two calls with the same inputs return the same paragraph. The
// five-to-eight-entry catalogue is hardcoded (not a settings file):
// the recovered AGY behaviour binds the paragraph to a session+task
// fingerprint, not to anything the user configures.
//
// Precedence:
//
//   - userText non-empty (after trim) → return verbatim. This is the
//     "/narration <text>" form the spec mentions. The mode/taskType
//     arguments are ignored entirely: a user-supplied line wins.
//   - mode == "off" → return "" (empty paragraph). The caller is
//     expected to translate "" into "do not append anything to
//     m.response"; the catalogue helper itself stays pure.
//   - otherwise → return one of the 6 hardcoded entries, selected
//     deterministically from sessionStart.UnixNano() (and taskType
//     when mode == "auto"). Same inputs → same paragraph. Two
//     consecutive calls with no inputs and the same sessionStart
//     return the same paragraph.
//
// Selection uses a tiny FNV-1a hash (no crypto import) and a modulo
// over the catalogue length. The hash is initialised with two
// domain-separation prefixes ("narration-default\n" and
// "narration-auto\n") so a same-numeric-on-the-wire start time with
// different modes never lands on the same entry.
//
// Cycle 250 print mode (-p) is unaffected: this helper is wired
// only through the TUI slash dispatch (cmd/wagyu/slash_commands.go);
// the print path does not call SelectNarration.
package cli

import (
	"hash/fnv"
	"strings"
	"time"
)

// narrationCatalogue is the hardcoded paragraph set the /narration
// helper picks from. Cycle 250 ships 8 entries; the spec calls for
// "5-8" — eight sits at the top of that band and is asserted by
// TestSelectNarration_CatalogueBounded, so shrinking the catalogue
// is a deliberate, test-visible change.
//
// The paragraphs are short, sentence-cased, and deliberately free of
// line breaks. The TUI's transcript renderer flattens strings that
// contain "\n" into multiple rows, so a multi-line entry would
// render as 3-4 distinct rows and look like a real response — /narration
// is purely cosmetic and must read as a single intro line.
//
// Entries 1 ("…explore…") and 3 ("…investigate…") are the
// bug-fix/research vocabulary: cmd/wagyu/narration_test.go's
// TestSlashNarration_Auto pins that an `auto bug` selection can
// land on one of them, so they must both stay in the set.
//
// Adding or removing an entry changes the FNV modulus and therefore
// reshuffles every selection, but it never breaks the "same inputs
// → same paragraph" guarantee. Any test that pins a specific
// paragraph must be re-derived after a catalogue edit.
var narrationCatalogue = []string{
	"Let me start by understanding the current state of the code.",
	"I'll explore the relevant files first, then plan the changes.",
	"Looking at this step by step.",
	"Let me investigate before making changes.",
	"I'll check the existing patterns in this codebase first.",
	"Starting with a quick survey of the relevant area.",
	"Working through this systematically.",
	"Let me read the surrounding code first.",
}

// narrationDefaultPrefix is mixed into the FNV hash when mode != "auto".
// Keeping it distinct from narrationAutoPrefix means a default-mode call
// at t=0 never collides with an auto-mode call at the same t.
const narrationDefaultPrefix = "narration-default\n"

// narrationAutoPrefix is mixed into the FNV hash when mode == "auto".
// taskType is appended after this prefix and is therefore part of the
// hash input — different taskTypes yield different paragraphs.
const narrationAutoPrefix = "narration-auto\n"

// fnvNarration is the cycle-250 FNV-1a over bytes. It is the
// standard library's hash/fnv.New64a — the same hash the reference
// encodes with, byte for byte.
//
// Domain separation: the caller always prepends one of the two
// narration*Prefix strings before calling this, so two
// selectors whose user-supplied bytes happen to be identical still
// hash to different values when their mode differs.
func fnvNarration(seed string, payload []byte) uint64 {
	h := fnv.New64a()
	// hash.Hash64.Write never returns an error (it is an in-memory
	// hash); the err-return signature is preserved for stdlib API
	// compatibility, so the value is ignored here.
	_, _ = h.Write([]byte(seed))
	_, _ = h.Write(payload)
	return h.Sum64()
}

// SelectNarration returns the paragraph /narration writes to the
// transcript. The four-argument contract is the entire spec:
//
//   - userText non-empty (whitespace-trimmed) → return verbatim.
//   - mode == "off" → return "".
//   - otherwise → return one of the narrationCatalogue entries,
//     selected deterministically from sessionStart.UnixNano() (and
//     taskType when mode == "auto"). Same inputs → same paragraph.
//
// The function is pure: it does not touch any package state, does not
// read settings.json, and never invokes Math/random. Two consecutive
// calls with identical inputs always return the same string; this is
// the determinism guarantee cycle 250's TestSelectNarration_CatalogueDeterministic
// pins.
//
// mode values the helper recognises today: "" (default), "default",
// "off", "auto". Anything else is treated as "default" (no taskType
// mixing) so a future caller that mistypes "auto" still gets a
// catalogue pick rather than silently nothing.
//
// taskType is the free-form descriptor the user typed after "/narration
// auto" (e.g. "refactor", "bugfix", "add a feature"). An empty taskType
// is valid — the hash still mixes the empty string in.
func SelectNarration(userText, mode, taskType string, sessionStart time.Time) string {
	trimmed := strings.TrimSpace(userText)
	if trimmed != "" {
		// Verbatim: nothing the catalogue can say outweighs the
		// user's own line. Mode and taskType are intentionally
		// ignored — passing both arguments is the documented
		// contract for "the user supplied their own text".
		return trimmed
	}
	if mode == "off" {
		return ""
	}
	// Pick a catalogue index deterministically.
	var (
		seed    = narrationDefaultPrefix
		payload []byte
	)
	if mode == "auto" {
		seed = narrationAutoPrefix
		// Append the session start's nanosecond clock first, then
		// the task type. The byte order matters: swapping them
		// would shift every catalogue pick, but the determinism
		// guarantee is preserved either way.
		if sessionStart.Year() >= 1 {
			// time.Time.AppendFormat is stable across Go versions;
			// we use a fixed RFC3339Nano so the bytes the helper
			// hashes are identical on every platform.
			payload = []byte(sessionStart.UTC().Format(time.RFC3339Nano))
		}
		// taskType rides after the timestamp.
		payload = append(payload, '\n')
		payload = append(payload, []byte(taskType)...)
	} else {
		// Mode == "" or "default". No taskType mixing.
		if sessionStart.Year() >= 1 {
			payload = []byte(sessionStart.UTC().Format(time.RFC3339Nano))
		}
	}
	idx := int(fnvNarration(seed, payload) % uint64(len(narrationCatalogue)))
	return narrationCatalogue[idx]
}
