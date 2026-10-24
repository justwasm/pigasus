// Package skillsui provides UI rendering, markdown detail viewing,
// deduplication, and row formatting for the /skills panel.
package skillsui

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"

	"github.com/justwasm/pigasus/internal/printmode"
)

// SkillInfo is an alias for the canonical skill info struct.
type SkillInfo = printmode.SkillInfo

// SkillContentHash returns the SHA-256 hex digest of the file at path,
// or "" when the file cannot be read.
func SkillContentHash(path string) string {
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// DedupeSkillInfosByHash folds skills that share the same SKILL.md
// content into a single entry. First-encountered wins.
func DedupeSkillInfosByHash(skills []SkillInfo) []SkillInfo {
	seen := make(map[string]int, len(skills))
	out := make([]SkillInfo, 0, len(skills))
	for _, s := range skills {
		h := SkillContentHash(s.Path)
		if h == "" {
			out = append(out, s)
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = len(out)
		out = append(out, s)
	}
	return out
}

// VerbatimSkillArgs returns args unchanged. It is the single opt-out
// point for any future prompt-pre-processing layer.
func VerbatimSkillArgs(args string) string {
	return args
}
