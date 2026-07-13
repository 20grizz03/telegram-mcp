package tgclient

import (
	"testing"
	"time"
)

func TestParseRange(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 6, 16, 15, 30, 0, 0, loc)

	t.Run("defaults to today..now", func(t *testing.T) {
		r, err := ParseRange("", "", loc, now)
		if err != nil {
			t.Fatal(err)
		}
		want := time.Date(2026, 6, 16, 0, 0, 0, 0, loc)
		if !r.Min.Equal(want) {
			t.Errorf("min = %s, want %s", r.Min, want)
		}
		if !r.Max.Equal(now) {
			t.Errorf("max = %s, want %s", r.Max, now)
		}
	})

	t.Run("relative days ago", func(t *testing.T) {
		r, err := ParseRange("7d", "", loc, now)
		if err != nil {
			t.Fatal(err)
		}
		want := now.Add(-7 * 24 * time.Hour)
		if !r.Min.Equal(want) {
			t.Errorf("min = %s, want %s", r.Min, want)
		}
	})

	t.Run("yesterday whole day", func(t *testing.T) {
		r, err := ParseRange("yesterday", "yesterday", loc, now)
		if err != nil {
			t.Fatal(err)
		}
		if r.Min.Day() != 15 || r.Min.Hour() != 0 {
			t.Errorf("min = %s", r.Min)
		}
		if r.Max.Day() != 15 || r.Max.Hour() != 23 {
			t.Errorf("max = %s", r.Max)
		}
	})

	t.Run("explicit date range", func(t *testing.T) {
		r, err := ParseRange("2026-06-10", "2026-06-12", loc, now)
		if err != nil {
			t.Fatal(err)
		}
		if r.Min.Day() != 10 || r.Max.Day() != 12 {
			t.Errorf("got %s..%s", r.Min, r.Max)
		}
	})

	t.Run("from after to errors", func(t *testing.T) {
		if _, err := ParseRange("2026-06-12", "2026-06-10", loc, now); err == nil {
			t.Error("expected error")
		}
	})

	t.Run("garbage errors", func(t *testing.T) {
		if _, err := ParseRange("not-a-date", "", loc, now); err == nil {
			t.Error("expected error")
		}
	})
}

func TestParseFutureInstant(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, loc)

	// empty -> zero time (publish now)
	if got, err := parseFutureInstant("", loc, now); err != nil || !got.IsZero() {
		t.Fatalf(`parseFutureInstant("") = %v, %v; want zero, nil`, got, err)
	}

	// relative goes into the FUTURE
	got, err := parseFutureInstant("2h", loc, now)
	if err != nil {
		t.Fatal(err)
	}
	if want := now.Add(2 * time.Hour); !got.Equal(want) {
		t.Fatalf(`parseFutureInstant("2h") = %v, want %v`, got, want)
	}

	// "7d" days unit
	got, err = parseFutureInstant("7d", loc, now)
	if err != nil {
		t.Fatal(err)
	}
	if want := now.AddDate(0, 0, 7); !got.Equal(want) {
		t.Fatalf(`parseFutureInstant("7d") = %v, want %v`, got, want)
	}

	// RFC3339 absolute in the future
	got, err = parseFutureInstant("2026-06-26T09:30:00Z", loc, now)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 6, 26, 9, 30, 0, 0, loc); !got.Equal(want) {
		t.Fatalf("RFC3339 = %v, want %v", got, want)
	}

	// a moment in the past -> error
	if _, err := parseFutureInstant("2020-01-01T00:00:00Z", loc, now); err == nil {
		t.Fatal("past instant: want error, got nil")
	}

	// garbage -> error
	if _, err := parseFutureInstant("not-a-time", loc, now); err == nil {
		t.Fatal("garbage: want error, got nil")
	}
}
