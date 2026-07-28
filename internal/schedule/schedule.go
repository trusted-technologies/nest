// Package schedule decides whether a job is due to run.
//
// Two formats are supported:
//   - "HH:MM"          — run daily at that time
//   - 5-field cron expr — e.g. "30 3 * * 0" (every Sunday 03:30)
//
// An empty schedule means "always due" (runs on every timer tick).
package schedule

import (
	"fmt"
	"regexp"
	"time"

	"github.com/robfig/cron/v3"
)

var hhmmRe = regexp.MustCompile(`^([01]?\d|2[0-3]):([0-5]\d)$`)

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// Valid reports whether s is a supported schedule expression.
func Valid(s string) bool {
	if s == "" {
		return true
	}
	if hhmmRe.MatchString(s) {
		return true
	}
	_, err := cronParser.Parse(s)
	return err == nil
}

// Due reports whether a job with the given schedule should run at `now`,
// given that the scheduler ticks every `window` (systemd timer interval).
//
// A job is due when its scheduled activation falls into the half-open
// interval (now-window, now]: each activation belongs to exactly one tick,
// so jobs never run twice (or early) for a single scheduled time.
func Due(s string, now time.Time, window time.Duration) (bool, error) {
	if s == "" {
		return true, nil
	}
	if m := hhmmRe.FindStringSubmatch(s); m != nil {
		var hour, min int
		fmt.Sscanf(s, "%d:%d", &hour, &min)
		scheduled := time.Date(now.Year(), now.Month(), now.Day(), hour, min, 0, 0, now.Location())
		if now.Before(scheduled) {
			// The activation may belong to the previous day when the
			// tick lands just after midnight.
			scheduled = scheduled.Add(-24 * time.Hour)
		}
		diff := now.Sub(scheduled)
		return diff >= 0 && diff < window, nil
	}
	sched, err := cronParser.Parse(s)
	if err != nil {
		return false, fmt.Errorf("invalid schedule %q (want HH:MM or 5-field cron): %w", s, err)
	}
	// due if a cron activation happened within (now-window, now]
	prev := sched.Next(now.Add(-window))
	return !prev.After(now), nil
}
