package chat

import (
	"strings"
	"testing"
	"time"
)

func TestParseWhen(t *testing.T) {
	now := time.Date(2026, 7, 26, 14, 30, 0, 0, time.FixedZone("ICT", 7*3600))

	cases := []struct {
		in   string
		want time.Time
	}{
		{"", time.Time{}},
		{"30m", now.Add(-30 * time.Minute)},
		{"12h", now.Add(-12 * time.Hour)},
		{"3d", now.Add(-72 * time.Hour)},
		// A bare date is local midnight, not UTC midnight.
		{"2026-07-25", time.Date(2026, 7, 25, 0, 0, 0, 0, time.Local)},
		{"2026-07-25T09:12:04+07:00", time.Date(2026, 7, 25, 9, 12, 4, 0, time.FixedZone("", 7*3600))},
	}
	for _, c := range cases {
		got, err := parseWhen(c.in, now)
		if err != nil {
			t.Errorf("parseWhen(%q) error: %v", c.in, err)
			continue
		}
		if !got.Equal(c.want) {
			t.Errorf("parseWhen(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseWhenRejectsGarbage(t *testing.T) {
	now := time.Now()
	for _, in := range []string{"yesterday", "3", "d3", "3w", "-2d", "2026-13-99"} {
		if _, err := parseWhen(in, now); err == nil {
			t.Errorf("parseWhen(%q) should have failed", in)
		}
	}
}

func TestParseWhenErrorMentionsAcceptedForms(t *testing.T) {
	_, err := parseWhen("yesterday", time.Now())
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"3d", "2026-07-25", "RFC3339"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

func TestWindowLabel(t *testing.T) {
	now := time.Date(2026, 7, 26, 14, 30, 0, 0, time.UTC)
	cases := []struct {
		since, until time.Time
		want         string
	}{
		{time.Time{}, time.Time{}, "all time"},
		{now.Add(-48 * time.Hour), time.Time{}, "2 days"},
		{now.Add(-24 * time.Hour), time.Time{}, "1 day"},
		{now.Add(-3 * time.Hour), time.Time{}, "3 hours"},
		{now.Add(-30 * time.Minute), time.Time{}, "30 minutes"},
		{
			time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
			"2026-07-01 00:00 → 2026-07-02 00:00",
		},
		{time.Time{}, time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC), "until 2026-07-02 00:00"},
	}
	for _, c := range cases {
		if got := windowLabel(c.since, c.until, now); got != c.want {
			t.Errorf("windowLabel(%v,%v) = %q, want %q", c.since, c.until, got, c.want)
		}
	}
}
