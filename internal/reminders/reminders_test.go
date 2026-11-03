package reminders

import (
	"path/filepath"
	"testing"
	"time"
)

// TestParseQuick checks a one-line reminder splits into what, when and how it repeats.
func TestParseQuick(t *testing.T) {
	now := time.Date(2026, 11, 3, 10, 0, 0, 0, time.Local) // a Tuesday
	for _, tc := range []struct{ in, what, at, every string }{
		{"stand-up at 4pm every weekday", "stand-up", "2026-11-03 16:00", "weekday"},
		{"call mom tomorrow 9am", "call mom", "2026-11-04 09:00", ""},
		{"stretch in 30m", "stretch", "2026-11-03 10:30", ""},
		{"pay rent nov 1 every month", "pay rent", "2027-11-01 09:00", "month"},
	} {
		r, err := ParseQuick(tc.in, now)
		if err != nil || r.Text != tc.what || r.At.Format("2006-01-02 15:04") != tc.at || r.Every != tc.every {
			t.Errorf("%q: %+v %v, want %s at %s every %q", tc.in, r, err, tc.what, tc.at, tc.every)
		}
	}
	if _, err := ParseQuick("4pm", now); err == nil {
		t.Error("a time with nothing to remind of was accepted")
	}
}

// TestDue checks a due one-time reminder fires once and a repeating one moves to its next time.
func TestDue(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "r.json")}
	now := time.Date(2026, 11, 3, 10, 0, 0, 0, time.Local)
	s.Save([]Reminder{{ID: 1, Text: "once", At: now.Add(-time.Minute)}, {ID: 2, Text: "daily", At: now.Add(-time.Minute), Every: "day"}})
	fired, rs := s.Due(now)
	if len(fired) != 2 || !Find(rs, 1).Done || !Find(rs, 2).At.After(now) {
		t.Fatalf("fired %v, now %+v", fired, rs)
	}
	if fired, _ = s.Due(now); len(fired) != 0 {
		t.Errorf("fired again: %v", fired)
	}
}
