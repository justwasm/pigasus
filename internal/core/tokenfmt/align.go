package tokenfmt

import (
	"fmt"
	"math"
	"sync"
)

// TokenAlignment captures the alignment metrics comparing local estimates against
// actual provider-reported token figures (CloudCode / Gateway / Gemini / Claude).
type TokenAlignment struct {
	EstimatedTokens      int     `json:"estimated_tokens"`
	ReportedInputTokens  int     `json:"reported_input_tokens"`
	ReportedCachedTokens int     `json:"reported_cached_tokens"`
	ReportedOutputTokens int     `json:"reported_output_tokens"`
	ActiveInputTokens    int     `json:"active_input_tokens"` // ReportedInputTokens - ReportedCachedTokens
	DriftTokens          int     `json:"drift_tokens"`        // ReportedInputTokens - EstimatedTokens
	DriftRatio           float64 `json:"drift_ratio"`         // DriftTokens / ReportedInputTokens
	IsDistorted          bool    `json:"is_distorted"`        // true if abs(DriftRatio) > distortionThreshold
	Formatted            string  `json:"formatted"`
}

// TokenReconciler maintains adaptive calibration parameters to align local token estimation
// with upstream provider tokenizers across long-running (200+ turns) conversations,
// preventing cumulative context calculation drift and distortion.
type TokenReconciler struct {
	mu                  sync.RWMutex
	baseDivisor         float64 // default 3.0 (reference agy bytes-per-token)
	calibratedFactor    float64 // dynamic multiplier, initial 1.0
	distortionThreshold float64 // threshold for distortion alarm, e.g. 0.25 (25%)
	sampleCount         int
	cumulativeEst       int
	cumulativeActual    int
	cumulativeCache     int
	cumulativeOutput    int
}

// NewTokenReconciler initializes a token reconciler with an optional base divisor (default 3.0).
func NewTokenReconciler(baseDivisor float64) *TokenReconciler {
	if baseDivisor <= 0 {
		baseDivisor = 3.0
	}
	return &TokenReconciler{
		baseDivisor:         baseDivisor,
		calibratedFactor:    1.0,
		distortionThreshold: 0.25,
	}
}

// SetDistortionThreshold sets the drift tolerance threshold (default 0.25).
func (r *TokenReconciler) SetDistortionThreshold(threshold float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if threshold > 0 {
		r.distortionThreshold = threshold
	}
}

// Estimate calculates an estimated token count for the given character count,
// taking into account calibrated adjustments.
func (r *TokenReconciler) Estimate(charCount int) int {
	if charCount <= 0 {
		return 0
	}
	r.mu.RLock()
	divisor := r.baseDivisor * r.calibratedFactor
	r.mu.RUnlock()
	if divisor <= 0 {
		divisor = 3.0
	}
	return int(float64(charCount) / divisor)
}

// Align evaluates an estimated token count against actual provider-reported figures.
// It tracks cumulative consumption, assesses drift, and returns an alignment summary.
func (r *TokenReconciler) Align(estimated, reportedInput, reportedOutput, reportedCached int) TokenAlignment {
	r.mu.Lock()
	defer r.mu.Unlock()

	activeInput := reportedInput - reportedCached
	if activeInput < 0 {
		activeInput = 0
	}

	drift := reportedInput - estimated
	var driftRatio float64
	if reportedInput > 0 {
		driftRatio = float64(drift) / float64(reportedInput)
	}

	isDistorted := math.Abs(driftRatio) > r.distortionThreshold

	r.sampleCount++
	r.cumulativeEst += estimated
	r.cumulativeActual += reportedInput
	r.cumulativeCache += reportedCached
	r.cumulativeOutput += reportedOutput

	align := TokenAlignment{
		EstimatedTokens:      estimated,
		ReportedInputTokens:  reportedInput,
		ReportedCachedTokens: reportedCached,
		ReportedOutputTokens: reportedOutput,
		ActiveInputTokens:    activeInput,
		DriftTokens:          drift,
		DriftRatio:           driftRatio,
		IsDistorted:          isDistorted,
	}
	align.Formatted = r.formatAlignmentLocked(align)
	return align
}

// Calibrate adjusts the reconciler's calibration factor based on observed
// character count and reported input tokens, reducing future drift.
func (r *TokenReconciler) Calibrate(charCount, reportedInput int) {
	if charCount <= 0 || reportedInput <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	actualDivisor := float64(charCount) / float64(reportedInput)
	ratio := actualDivisor / r.baseDivisor
	if ratio < 0.2 {
		ratio = 0.2
	} else if ratio > 5.0 {
		ratio = 5.0
	}

	// Smooth adjustment: 80% previous factor, 20% latest observation
	r.calibratedFactor = 0.8*r.calibratedFactor + 0.2*ratio
}

// CumulativeStats returns aggregated token consumption metrics.
func (r *TokenReconciler) CumulativeStats() (est int, actual int, cache int, output int, driftRatio float64) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var ratio float64
	if r.cumulativeActual > 0 {
		ratio = float64(r.cumulativeActual-r.cumulativeEst) / float64(r.cumulativeActual)
	}
	return r.cumulativeEst, r.cumulativeActual, r.cumulativeCache, r.cumulativeOutput, ratio
}

// CalibratedFactor returns the current adaptive calibration multiplier.
func (r *TokenReconciler) CalibratedFactor() float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.calibratedFactor
}

func (r *TokenReconciler) formatAlignmentLocked(a TokenAlignment) string {
	cachedStr := ""
	if a.ReportedCachedTokens > 0 {
		cachedStr = fmt.Sprintf(", cached %s", K(a.ReportedCachedTokens))
	}
	sign := "+"
	if a.DriftRatio < 0 {
		sign = ""
	}
	return fmt.Sprintf("%s (est %s%s, drift %s%.1f%%)",
		K(a.ReportedInputTokens),
		K(a.EstimatedTokens),
		cachedStr,
		sign,
		a.DriftRatio*100,
	)
}

// FormatAlignment returns a human-readable token alignment summary.
func FormatAlignment(a TokenAlignment) string {
	cachedStr := ""
	if a.ReportedCachedTokens > 0 {
		cachedStr = fmt.Sprintf(", cached %s", K(a.ReportedCachedTokens))
	}
	sign := "+"
	if a.DriftRatio < 0 {
		sign = ""
	}
	return fmt.Sprintf("%s (est %s%s, drift %s%.1f%%)",
		K(a.ReportedInputTokens),
		K(a.EstimatedTokens),
		cachedStr,
		sign,
		a.DriftRatio*100,
	)
}
