package chat

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// relativeRe matches the relative forms accepted by --since / --until.
var relativeRe = regexp.MustCompile(`^(\d+)([dhm])$`)

// parseWhen accepts a relative offset ("3d", "12h", "30m"), a bare date
// ("2026-07-25", read as local midnight), or a full RFC3339 timestamp. An empty
// string yields the zero time, meaning "unbounded".
func parseWhen(s string, now time.Time) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if m := relativeRe.FindStringSubmatch(s); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return time.Time{}, invalidWhen(s)
		}
		unit := map[string]time.Duration{
			"d": 24 * time.Hour,
			"h": time.Hour,
			"m": time.Minute,
		}[m[2]]
		return now.Add(-time.Duration(n) * unit), nil
	}
	// A bare date means midnight in the machine's zone, not UTC.
	if ts, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return ts, nil
	}
	if ts, err := time.Parse(time.RFC3339, s); err == nil {
		return ts, nil
	}
	return time.Time{}, invalidWhen(s)
}

func invalidWhen(s string) error {
	return fmt.Errorf("invalid time %q: use a relative offset (3d, 12h, 30m), "+
		"a date (2026-07-25), or a full RFC3339 timestamp", s)
}

// windowLabel describes the scanned window for the summary line.
func windowLabel(since, until, now time.Time) string {
	const stamp = "2006-01-02 15:04"
	switch {
	case since.IsZero() && until.IsZero():
		return "all time"
	case since.IsZero():
		return "until " + until.Format(stamp)
	case !until.IsZero():
		return since.Format(stamp) + " → " + until.Format(stamp)
	}
	d := now.Sub(since)
	switch {
	case d < 90*time.Minute:
		return plural(int(d.Round(time.Minute).Minutes()), "minute", "minutes")
	case d < 24*time.Hour:
		return plural(int(d.Round(time.Hour).Hours()), "hour", "hours")
	default:
		return plural(int(d.Round(24*time.Hour).Hours()/24), "day", "days")
	}
}
