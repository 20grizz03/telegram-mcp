package tgclient

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Range is an inclusive [Min, Max] window over message dates.
type Range struct {
	Min time.Time // oldest message kept
	Max time.Time // newest message kept
}

// ParseRange turns user-facing "from"/"to" strings into a concrete time window
// in the given location. Empty "from" defaults to the start of today; empty
// "to" defaults to now.
//
// Each bound accepts:
//
//	"" "now"                  -> now (to) / start of today (from)
//	"today" / "yesterday"     -> day boundary in loc
//	"7d" "24h" "30m"          -> now minus that duration ("ago")
//	"2026-06-15"              -> day boundary (start for from, end for to)
//	RFC3339 ("2026-06-15T..") -> exact instant
func ParseRange(from, to string, loc *time.Location, now time.Time) (Range, error) {
	now = now.In(loc)

	min, err := parseInstant(from, loc, now, false)
	if err != nil {
		return Range{}, fmt.Errorf("parse from=%q: %w", from, err)
	}
	max, err := parseInstant(to, loc, now, true)
	if err != nil {
		return Range{}, fmt.Errorf("parse to=%q: %w", to, err)
	}
	if min.After(max) {
		return Range{}, fmt.Errorf("from (%s) is after to (%s)", min, max)
	}
	return Range{Min: min, Max: max}, nil
}

func parseInstant(s string, loc *time.Location, now time.Time, isEnd bool) (time.Time, error) {
	s = strings.TrimSpace(strings.ToLower(s))

	switch s {
	case "", "now":
		if isEnd {
			return now, nil
		}
		return startOfDay(now), nil // empty "from" => today
	case "today":
		if isEnd {
			return endOfDay(now), nil
		}
		return startOfDay(now), nil
	case "yesterday":
		y := now.AddDate(0, 0, -1)
		if isEnd {
			return endOfDay(y), nil
		}
		return startOfDay(y), nil
	}

	// Relative duration "ago": 7d / 24h / 30m.
	if d, ok := parseAgo(s); ok {
		return now.Add(-d), nil
	}

	// Date only.
	if t, err := time.ParseInLocation("2006-01-02", s, loc); err == nil {
		if isEnd {
			return endOfDay(t), nil
		}
		return startOfDay(t), nil
	}

	// Full timestamp.
	if t, err := time.ParseInLocation(time.RFC3339, strings.ToUpper(s), loc); err == nil {
		return t.In(loc), nil
	}

	return time.Time{}, fmt.Errorf("unrecognized time format")
}

// parseAgo parses "7d" / "24h" / "30m" into a duration. Days are supported on
// top of Go's native h/m/s units.
func parseAgo(s string) (time.Duration, bool) {
	if s == "" {
		return 0, false
	}
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, false
		}
		return time.Duration(n) * 24 * time.Hour, true
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, false
	}
	return d, true
}

// parseFutureInstant parses a scheduling time using the same vocabulary as the
// read side, but relative durations point into the FUTURE (now+d) instead of
// "ago". Empty string returns the zero time (meaning "publish immediately").
// A non-empty result must be strictly in the future.
//
//	""                         -> zero time (send now)
//	"2h" / "30m" / "7d"        -> now plus that duration
//	"2026-06-26"               -> start of that day
//	RFC3339 ("2026-06-26T..")  -> exact instant
func parseFutureInstant(s string, loc *time.Location, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	now = now.In(loc)

	var when time.Time
	switch {
	case isAgo(s):
		d, _ := parseAgo(strings.ToLower(s))
		when = now.Add(d)
	default:
		if t, err := time.ParseInLocation("2006-01-02", s, loc); err == nil {
			when = startOfDay(t)
		} else if t, err := time.ParseInLocation(time.RFC3339, strings.ToUpper(s), loc); err == nil {
			when = t.In(loc)
		} else {
			return time.Time{}, fmt.Errorf("unrecognized time format %q", s)
		}
	}

	if !when.After(now) {
		return time.Time{}, fmt.Errorf("scheduled time %s is not in the future", when.Format(time.RFC3339))
	}
	return when, nil
}

// ParseFutureInstant parses a scheduling time (see parseFutureInstant) using
// the current wall clock. Empty string yields the zero time (post now).
func ParseFutureInstant(s string, loc *time.Location) (time.Time, error) {
	return parseFutureInstant(s, loc, time.Now())
}

// isAgo reports whether s parses as a relative duration (Nd/Nh/Nm).
func isAgo(s string) bool {
	_, ok := parseAgo(strings.ToLower(s))
	return ok
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func endOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 23, 59, 59, int(time.Second-time.Nanosecond), t.Location())
}
