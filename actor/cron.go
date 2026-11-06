package actor

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// NextCronTime calculates the next time after 'from' that satisfies the given cron expression.
// Supports standard 5-field cron ("min hour dom month dow"),
// shortcuts ("@hourly", "@daily", "@midnight", "@weekly", "@monthly", "@yearly"),
// and duration steps ("@every 10s", "@every 1m").
func NextCronTime(expr string, from time.Time) (time.Time, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return time.Time{}, fmt.Errorf("empty cron expression")
	}

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

	// Advance 1 minute from 'from', truncated to minute precision
	t := from.Truncate(time.Minute).Add(time.Minute)

	const maxScanMinutes = 525600 // 1 year limit
	for i := 0; i < maxScanMinutes; i++ {
		// Month (1-12)
		if !matchCronField(monthField, int(t.Month()), 1, 12) {
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, t.Location())
			continue
		}

		// Day of Month & Day of Week
		domMatch := matchCronField(domField, t.Day(), 1, 31)
		dow := int(t.Weekday())
		dowMatch := matchCronField(dowField, dow, 0, 7) // 0 and 7 are both Sunday

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

		// Hour (0-23)
		if !matchCronField(hourField, t.Hour(), 0, 23) {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, t.Location())
			continue
		}

		// Minute (0-59)
		if !matchCronField(minField, t.Minute(), 0, 59) {
			t = t.Add(time.Minute)
			continue
		}

		return t, nil
	}

	return time.Time{}, fmt.Errorf("no match found for cron expression %q within 1 year", expr)
}

func matchCronField(field string, val, minVal, maxVal int) bool {
	if field == "*" || field == "?" {
		return true
	}

	parts := strings.Split(field, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		step := 1
		rangePart := part
		if strings.Contains(part, "/") {
			subParts := strings.SplitN(part, "/", 2)
			rangePart = subParts[0]
			s, err := strconv.Atoi(subParts[1])
			if err != nil || s <= 0 {
				continue
			}
			step = s
		}

		var start, end int
		if rangePart == "*" {
			start = minVal
			end = maxVal
		} else if strings.Contains(rangePart, "-") {
			subParts := strings.SplitN(rangePart, "-", 2)
			s, err1 := strconv.Atoi(subParts[0])
			e, err2 := strconv.Atoi(subParts[1])
			if err1 != nil || err2 != nil {
				continue
			}
			start = s
			end = e
		} else {
			n, err := strconv.Atoi(rangePart)
			if err != nil {
				continue
			}
			if step == 1 {
				if n == val || (minVal == 0 && maxVal == 7 && ((n == 0 && val == 7) || (n == 7 && val == 0))) {
					return true
				}
				continue
			}
			start = n
			end = maxVal
		}

		for v := start; v <= end; v += step {
			if v == val || (minVal == 0 && maxVal == 7 && ((v == 0 && val == 7) || (v == 7 && val == 0))) {
				return true
			}
		}
	}

	return false
}
