// since.go provides a flexible time parser that accepts Go durations,
// RFC3339 datetimes, or YYYY-MM-DD date strings.
package model

import (
	"fmt"
	"time"
)

// ParseSince parses a --since value into a time.Time.
// Accepts: "168h" (duration), "2024-01-15T00:00:00Z" (RFC3339), "2024-01-15" (date).
func ParseSince(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}

	// Try RFC3339 first
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}

	// Try YYYY-MM-DD
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}

	// Try Go duration
	dur, err := time.ParseDuration(s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid --since value %q (use duration like '168h', date like '2024-01-15', or RFC3339)", s)
	}

	return time.Now().Add(-dur), nil
}
