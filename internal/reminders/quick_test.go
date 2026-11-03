package reminders

import (
	"testing"
	"time"
)

// The quick line takes how people write a reminder: a repeat before the time, spelled-out durations,
// and "remind me to" in front.
func TestQuickPhrasings(t *testing.T) {
	now := time.Date(2026, 11, 3, 10, 0, 0, 0, time.Local) // a Tuesday
	for _, c := range []struct {
		in, text, every string
		at              time.Time
	}{
		{"standup every day at 10am", "standup", "day", time.Date(2026, 11, 4, 10, 0, 0, 0, time.Local)},
		{"stand-up at 4pm every weekday", "stand-up", "weekday", time.Date(2026, 11, 3, 16, 0, 0, 0, time.Local)},
		{"review PR in 1 hour", "review PR", "", now.Add(time.Hour)},
		{"stretch in 30 minutes", "stretch", "", now.Add(30 * time.Minute)},
		{"check deploy in an hour", "check deploy", "", now.Add(time.Hour)},
		{"renew cert in 2 days", "renew cert", "", now.Add(48 * time.Hour)},
		{"Remind me to call bob at 3pm", "call bob", "", time.Date(2026, 11, 3, 15, 0, 0, 0, time.Local)},
		{"stretch in 30m", "stretch", "", now.Add(30 * time.Minute)},
	} {
		r, err := ParseQuick(c.in, now)
		if err != nil || r.Text != c.text || r.Every != c.every || !r.At.Equal(c.at) {
			t.Errorf("%q: got %q at %v every %q (%v); want %q at %v every %q", c.in, r.Text, r.At, r.Every, err, c.text, c.at, c.every)
		}
	}
	if _, err := ParseQuick("drink water", now); err == nil {
		t.Error("a line with no time was taken")
	}
}
