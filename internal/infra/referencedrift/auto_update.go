package referencedrift

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AutoUpdateOptions controls automated lock file update operations.
type AutoUpdateOptions struct {
	// BinaryPath specifies the reference binary to inspect. If empty, FindBinary() is used.
	BinaryPath string

	// UpdateVersion updates the lock's pinned Version to match the live binary version.
	UpdateVersion bool

	// UpdateBinarySHA256 stores or updates the reference binary's SHA-256 in the lock.
	UpdateBinarySHA256 bool

	// AcknowledgeLag sets LagAcknowledgedThrough to the live binary version.
	AcknowledgeLag bool

	// LagNote sets the explanation for acknowledged lag.
	LagNote string

	// SkipArtifactRefresh skips recomputing artifact digests from disk.
	SkipArtifactRefresh bool
}

// AutoUpdateResult describes the changes made by AutoUpdateLock.
type AutoUpdateResult struct {
	Updated          bool     `json:"updated"`
	PreviousVersion  string   `json:"previous_version"`
	NewVersion       string   `json:"new_version"`
	PreviousSHA256   string   `json:"previous_sha256,omitempty"`
	NewSHA256        string   `json:"new_sha256,omitempty"`
	DigestsRefreshed int      `json:"digests_refreshed"`
	ChangedArtifacts []string `json:"changed_artifacts,omitempty"`
	Report           string   `json:"report"`
}

// AutoUpdateLock inspects the reference binary, updates the lock file under repoRoot,
// and returns a summary of the updates made.
func AutoUpdateLock(repoRoot string, opts AutoUpdateOptions) (*AutoUpdateResult, error) {
	lockPath := filepath.Join(repoRoot, filepath.FromSlash(LockPath))
	l, err := LoadFrom(lockPath)
	if err != nil {
		return nil, fmt.Errorf("load lock from %s: %w", lockPath, err)
	}

	res := &AutoUpdateResult{
		PreviousVersion: l.Version,
		NewVersion:      l.Version,
		PreviousSHA256:  l.BinarySHA256,
		NewSHA256:       l.BinarySHA256,
	}

	binPath := opts.BinaryPath
	if binPath == "" {
		binPath = FindBinary()
	}

	var binInfo *BinaryInfo
	if binPath != "" {
		if b, err := InspectBinary(binPath); err == nil {
			binInfo = b
		}
	}

	// 1. Update Version if requested
	if opts.UpdateVersion && binInfo != nil && binInfo.Version != "" && binInfo.Version != "unknown" {
		if l.Version != binInfo.Version {
			l.Version = binInfo.Version
			res.NewVersion = binInfo.Version
			res.Updated = true
		}
	}

	// 2. Update BinarySHA256 if requested
	if opts.UpdateBinarySHA256 && binInfo != nil && binInfo.SHA256 != "" {
		if l.BinarySHA256 != binInfo.SHA256 {
			l.BinarySHA256 = binInfo.SHA256
			res.NewSHA256 = binInfo.SHA256
			res.Updated = true
		}
	}

	// 3. Acknowledge lag if requested
	if opts.AcknowledgeLag && binInfo != nil && binInfo.Version != "" {
		if CompareVersions(binInfo.Version, l.Version) > 0 {
			l.LagAcknowledgedThrough = binInfo.Version
			if opts.LagNote != "" {
				l.LagNote = opts.LagNote
			}
			res.Updated = true
		}
	}

	// 4. Refresh artifact digests on disk
	if !opts.SkipArtifactRefresh {
		for i := range l.Artifacts {
			a := &l.Artifacts[i]
			fullPath := filepath.Join(repoRoot, filepath.FromSlash(a.Path))
			data, err := os.ReadFile(fullPath)
			if err != nil {
				continue // missing files are reported, not failed
			}
			newDigest := Digest(data)
			if a.Digest != newDigest {
				res.ChangedArtifacts = append(res.ChangedArtifacts, a.Path)
				a.Digest = newDigest
				res.Updated = true
			}
			res.DigestsRefreshed++
		}
	}

	// 5. Write updated lock back
	if res.Updated {
		if err := WriteLock(repoRoot, l); err != nil {
			return nil, fmt.Errorf("write updated lock: %w", err)
		}
	}

	// 6. Build text report
	var rep strings.Builder
	rep.WriteString("=== Reference Lock Auto-Update Summary ===\n")
	if res.Updated {
		rep.WriteString("Status:           UPDATED\n")
	} else {
		rep.WriteString("Status:           NO CHANGES NEEDED\n")
	}
	rep.WriteString(fmt.Sprintf("Pinned Version:   %s -> %s\n", res.PreviousVersion, res.NewVersion))
	if res.NewSHA256 != "" || res.PreviousSHA256 != "" {
		rep.WriteString(fmt.Sprintf("Binary SHA-256:   %s -> %s\n", res.PreviousSHA256, res.NewSHA256))
	}
	rep.WriteString(fmt.Sprintf("Digests Checked:  %d\n", res.DigestsRefreshed))
	if len(res.ChangedArtifacts) > 0 {
		rep.WriteString(fmt.Sprintf("Updated Artifacts (%d):\n", len(res.ChangedArtifacts)))
		for _, p := range res.ChangedArtifacts {
			rep.WriteString(fmt.Sprintf("  - %s\n", p))
		}
	}
	res.Report = rep.String()

	return res, nil
}
