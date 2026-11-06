package shell

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// NextCronTime calculates the next time after 'from' that matches the cron expression.
// Supports standard 5-field cron: "minute hour dom month dow",
// as well as descriptors: "@hourly", "@daily", "@midnight", "@weekly", "@monthly", "@yearly",
// and "@every <duration>".
func NextCronTime(expr string, from time.Time) (time.Time, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return time.Time{}, fmt.Errorf("empty cron expression")
	}

	// Handle descriptors
	switch strings.ToLower(expr) {
	case "@hourly":
		expr = "0 * * * *"
	case "@daily", "@midnight":
		expr = "0 0 * * *"
	case "@weekly":
		expr = "0 0 * * 0"
	case "@monthly":
		expr = "0 0 1 * *"
	case "@yearly", "@annually":
		expr = "0 0 1 1 *"
	}

	if strings.HasPrefix(strings.ToLower(expr), "@every ") {
		durStr := strings.TrimSpace(expr[7:])
		dur, err := time.ParseDuration(durStr)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid @every duration %q: %w", durStr, err)
		}
		if dur <= 0 {
			return time.Time{}, fmt.Errorf("duration must be positive")
		}
		return from.Add(dur), nil
	}

	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return time.Time{}, fmt.Errorf("cron expression %q requires 5 fields, got %d", expr, len(fields))
	}

	minField, hourField, domField, monthField, dowField := fields[0], fields[1], fields[2], fields[3], fields[4]

	// Advance at least 1 minute forward from 'from', truncated to minute precision
	t := from.Truncate(time.Minute).Add(time.Minute)

	// Scan up to 525,600 minutes (1 year) to find the next matching wall-clock minute
	const maxMinutes = 525600
	for i := 0; i < maxMinutes; i++ {
		// Check Month (1-12)
		if !matchCronField(monthField, int(t.Month()), 1, 12) {
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, t.Location())
			continue
		}

		// Check Day of Month (1-31) and Day of Week (0-6)
		domMatch := matchCronField(domField, t.Day(), 1, 31)
		dow := int(t.Weekday())
		dowMatch := matchCronField(dowField, dow, 0, 7) // 0 and 7 are both Sunday

		// In standard cron, if both dom and dow are specified (neither is *), either matching suffices.
		// If either is *, then the other is restrictive.
		dayMatches := false
		if domField == "*" && dowField == "*" {
			dayMatches = true
		} else if domField == "*" {
			dayMatches = dowMatch
		} else if dowField == "*" {
			dayMatches = domMatch
		} else {
			dayMatches = domMatch || dowMatch
		}

		if !dayMatches {
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, t.Location())
			continue
		}

		// Check Hour (0-23)
		if !matchCronField(hourField, t.Hour(), 0, 23) {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, t.Location())
			continue
		}

		// Check Minute (0-59)
		if !matchCronField(minField, t.Minute(), 0, 59) {
			t = t.Add(time.Minute)
			continue
		}

		return t, nil
	}

	return time.Time{}, fmt.Errorf("no matching cron time found within 1 year for expression %q", expr)
}

func matchCronField(field string, val int, minVal, maxVal int) bool {
	if field == "*" || field == "?" {
		return true
	}

	// Comma-separated list: "1,5,10"
	if strings.Contains(field, ",") {
		parts := strings.Split(field, ",")
		for _, part := range parts {
			if matchCronField(strings.TrimSpace(part), val, minVal, maxVal) {
				return true
			}
		}
		return false
	}

	// Step values: "*/5" or "10-30/5"
	if strings.Contains(field, "/") {
		parts := strings.Split(field, "/")
		if len(parts) != 2 {
			return false
		}
		step, err := strconv.Atoi(parts[1])
		if err != nil || step <= 0 {
			return false
		}

		rangeStr := parts[0]
		startVal := minVal
		endVal := maxVal
		if rangeStr != "*" {
			if strings.Contains(rangeStr, "-") {
				sub := strings.Split(rangeStr, "-")
				s, err1 := strconv.Atoi(sub[0])
				e, err2 := strconv.Atoi(sub[1])
				if err1 == nil && err2 == nil {
					startVal, endVal = s, e
				}
			} else {
				if s, err := strconv.Atoi(rangeStr); err == nil {
					startVal = s
				}
			}
		}
		if val < startVal || val > endVal {
			return false
		}
		return (val-startVal)%step == 0
	}

	// Range values: "1-5"
	if strings.Contains(field, "-") {
		parts := strings.Split(field, "-")
		if len(parts) == 2 {
			start, err1 := strconv.Atoi(parts[0])
			end, err2 := strconv.Atoi(parts[1])
			if err1 == nil && err2 == nil {
				return val >= start && val <= end
			}
		}
		return false
	}

	// Single number
	num, err := strconv.Atoi(field)
	if err == nil {
		if (minVal == 0 && maxVal == 7) && (num == 7 && val == 0) {
			// Sunday can be 0 or 7
			return true
		}
		return num == val
	}

	return false
}
