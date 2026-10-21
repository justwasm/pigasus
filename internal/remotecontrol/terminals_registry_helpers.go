// Package remotecontrol — terminals_registry_helpers.go:
// tiny ID-minting + sort + trim utilities used by
// terminals_registry.go and terminals_dispatcher.go.
// Pulled out of terminals_registry.go in cycle
// rc-filesize-internal-remotecontrol so the parent file
// focuses on the registry's data model + concurrency
// discipline and the dispatcher stays free of any
// inline trimming logic.
package remotecontrol

import (
	"crypto/rand"
	"encoding/hex"
)

// mintTerminalID returns a fresh terminal id formatted
// as "tml_<16hex>" (8 random bytes from crypto/rand
// hex-encoded; 64 bits of entropy). The prefix mirrors
// the cycle-5 mint pattern (cascade "cas_",
// conversation "conv_", terminal "tml_"). The helper
// exists so tests can pin the literal string format.
func mintTerminalID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "tml_" + hex.EncodeToString(b[:]), nil
}

// sortTerminalsByCreatedAt sorts rows in-place by
// CreatedAt ascending, with terminal_id ascending as a
// tie-breaker for deterministic ordering on tests.
// Mirrors the rc-triggers / rc-fastpush-toggle /
// rc-resources "sort + tiebreaker" precedent.
//
// Centralised so a future cycle that wants to expose a
// List method can reuse the canonical order without
// rewriting the helper.
func sortTerminalsByCreatedAt(rows []Terminal) {
	if len(rows) < 2 {
		return
	}
	// Insertion sort by CreatedAt. n is small per
	// dispatcher (the registry has at most a handful
	// of terminals at any time — every Create RPC
	// bumps the count) so insertion sort beats the
	// generic sort.Slice helper.
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0; j-- {
			if rows[j-1].CreatedAt.Before(rows[j].CreatedAt) {
				rows[j-1], rows[j] = rows[j], rows[j-1]
				continue
			}
			if rows[j-1].CreatedAt.Equal(rows[j].CreatedAt) &&
				rows[j-1].TerminalID > rows[j].TerminalID {
				rows[j-1], rows[j] = rows[j], rows[j-1]
				continue
			}
			break
		}
	}
}

// normalizeTerminalID is the trim helper the
// dispatcher's handler applies before the registry
// call. Mirrors the rc-triggers / rc-resources
// "normalise before validate" precedent. Empty /
// whitespace-only inputs land as the empty string; the
// dispatcher's "required, non-empty" guard fires before
// the registry call so this is just a centralised trim.
func normalizeTerminalID(s string) string {
	return trimSpaceASCII(s)
}

// trimSpaceASCII is a tiny helper that trims leading
// and trailing ASCII whitespace. Centralised so the
// dispatcher and registry agree on what counts as
// "whitespace" (no Unicode whitespace handling today —
// terminal_ids are ASCII per the brief's tml_<hex>
// contract).
func trimSpaceASCII(s string) string {
	start, end := 0, len(s)
	for start < end {
		c := s[start]
		if c != ' ' && c != '\t' && c != '\r' && c != '\n' {
			break
		}
		start++
	}
	for end > start {
		c := s[end-1]
		if c != ' ' && c != '\t' && c != '\r' && c != '\n' {
			break
		}
		end--
	}
	return s[start:end]
}
