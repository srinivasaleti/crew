package focus

import (
	"testing"
	"time"
)

// testEnv is an Env with FOCUS on and nothing drawn.
type testEnv struct{}

func (testEnv) On() bool              { return true }
func (testEnv) Blurred() bool         { return false }
func (testEnv) Notice(string)         {}
func (testEnv) Screen(bool)           {}
func (testEnv) Names(s string) string { return s }

// TestSkipBreak checks skipping a break counts it skipped, starts the counts over, and starts the
// next focus after a session but not after a move break.
func TestSkipBreak(t *testing.T) {
	t.Setenv("CREW_NOTIFY", "0")
	tm := New(t.TempDir())
	tm.Set.Screen, tm.Set.Mode = "crew", "pomodoro"
	e := testEnv{}

	// Time's up after a focus session: skip starts the next one.
	tm.StartFocus(e, time.Minute)
	tm.Clock.Ends = time.Now().Add(-time.Second)
	tm.Move, tm.Eye = 10*time.Minute, 5*time.Minute
	if !tm.checkClock(e, time.Now()) || tm.Screen != "ask" {
		t.Fatalf("time's up didn't ask: screen %q", tm.Screen)
	}
	if !tm.SkipBreak(e) {
		t.Fatal("skip on time's up did nothing")
	}
	if tm.Clock.Kind != "focus" || tm.Screen != "" || tm.Move != 0 || tm.Eye != 0 {
		t.Fatalf("after skip: clock %q screen %q move %v eye %v", tm.Clock.Kind, tm.Screen, tm.Move, tm.Eye)
	}
	if tm.Day.Sessions != 1 || tm.Day.Skipped != 1 {
		t.Fatalf("sessions %d skipped %d, want 1 and 1", tm.Day.Sessions, tm.Day.Skipped)
	}

	// A running move break: skip only ends it.
	tm.Clock = Clock{}
	tm.startBreak(e, "Move break", time.Minute)
	if !tm.SkipBreak(e) || tm.Clock.Running() || tm.Screen != "" || tm.Day.Skipped != 2 {
		t.Fatalf("move break skip: clock %q screen %q skipped %d", tm.Clock.Kind, tm.Screen, tm.Day.Skipped)
	}

	// Nothing to skip.
	if tm.SkipBreak(e) {
		t.Fatal("skip with no break said it changed something")
	}
}
