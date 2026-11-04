package reminders

import (
	"testing"
	"time"
)

// TestSnoozeWhen checks "10m", what the web UI's Snooze sends, makes a reminder 10 minutes on.
func TestSnoozeWhen(t *testing.T) {
	now := time.Date(2026, 11, 4, 10, 0, 0, 0, time.Local)
	r, err := Build(Reminder{Text: "stand-up"}, "10m", "", now)
	if err != nil || !r.At.Equal(now.Add(10*time.Minute)) {
		t.Errorf("%v %v", r.At, err)
	}
}
