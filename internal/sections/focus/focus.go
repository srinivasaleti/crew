// Package focus is FOCUS's data: the focus timer, screen time per day, and their settings.
// It's kept in ~/.crew/focus as small JSON files. No UI.
package focus

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Settings are FOCUS's settings, edited with e on its page and kept in settings.json. Until that file
// exists, the environment gives the defaults. A zero Eye, Move or Limit turns that off.
type Settings struct {
	Mode      string        `json:"mode"`       // "pomodoro" (a countdown) or "flow" (counts up)
	Focus     time.Duration `json:"focus"`      // a pomodoro session
	Break     time.Duration `json:"break"`      // the short break after it
	Long      time.Duration `json:"long"`       // the long break
	LongEvery int           `json:"long_every"` // a long break after this many sessions
	Move      time.Duration `json:"move"`       // active time between move breaks
	MoveFor   time.Duration `json:"move_for"`   // a move break's length
	Eye       time.Duration `json:"eye"`        // active time between eye breaks (20-20-20)
	Screen    bool          `json:"screen"`     // a break covers the main pane
	Limit     time.Duration `json:"limit"`      // daily screen time; warned at 80% and 100%
	EndOfDay  string        `json:"end_of_day"` // "19:00", or "" for none
	On        string        `json:"dnd_on"`     // Shortcut run when a session starts (Do Not Disturb)
	Off       string        `json:"dnd_off"`    // Shortcut run when it ends
}

// defaults are the settings before settings.json exists: CREW_FOCUS_MIN, CREW_BREAK_MIN,
// CREW_EYE_BREAKS, CREW_SIT_MIN (minutes) and CREW_FOCUS_ON / CREW_FOCUS_OFF (Shortcut names).
func defaults() Settings {
	min := func(key string, def int) time.Duration {
		n, err := strconv.Atoi(os.Getenv(key))
		if err != nil || n < 0 {
			n = def
		}
		return time.Duration(n) * time.Minute
	}
	return Settings{Mode: "pomodoro", Focus: min("CREW_FOCUS_MIN", 25), Break: min("CREW_BREAK_MIN", 5),
		Long: 15 * time.Minute, LongEvery: 4, Move: min("CREW_SIT_MIN", 30), MoveFor: 3 * time.Minute,
		Eye: min("CREW_EYE_BREAKS", 20), Screen: true, Limit: 6 * time.Hour,
		On: os.Getenv("CREW_FOCUS_ON"), Off: os.Getenv("CREW_FOCUS_OFF")}
}

// fix keeps settings usable: sessions and breaks at least a minute, a known mode.
func (st Settings) fix() Settings {
	if st.Mode != "flow" {
		st.Mode = "pomodoro"
	}
	st.Focus, st.Break, st.Long, st.MoveFor = max(st.Focus, time.Minute), max(st.Break, time.Minute), max(st.Long, time.Minute), max(st.MoveFor, time.Minute)
	st.LongEvery = max(st.LongEvery, 1)
	return st
}

// Settings reads settings.json over the defaults.
func (s Store) Settings() Settings {
	st := defaults()
	s.read("settings.json", &st)
	return st.fix()
}

// SaveSettings writes settings.json.
func (s Store) SaveSettings(st Settings) error { return s.write("settings.json", st) }

// EndOfDayAt is today's end of day, and whether one is set.
func (st Settings) EndOfDayAt(now time.Time) (time.Time, bool) {
	t, err := time.Parse("15:04", st.EndOfDay)
	if err != nil {
		return time.Time{}, false
	}
	return time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location()), true
}

// Clock is the timer: a focus session, a break, or nothing. A focus session with no Ends is a flow
// session, which counts up.
type Clock struct {
	Kind  string    `json:"kind"`            // "focus", "break" or ""
	Label string    `json:"label,omitempty"` // a break's name ("Move break")
	Start time.Time `json:"start"`
	Ends  time.Time `json:"ends"`
	Done  int       `json:"done,omitempty"` // pomodoro sessions finished since the last long break
}

// Running reports whether the clock is on.
func (c Clock) Running() bool { return c.Kind != "" }

// Flow reports whether it's a flow session, which counts up.
func (c Clock) Flow() bool { return c.Kind == "focus" && c.Ends.IsZero() }

// Left is the time left, never below zero.
func (c Clock) Left(now time.Time) time.Duration { return max(c.Ends.Sub(now), 0) }

// Day is one day's screen time and breaks.
type Day struct {
	Date     string        `json:"date"`              // 2006-01-02
	Active   time.Duration `json:"crew"`              // you active at the Mac (in crew, before it read the whole Mac)
	Focus    time.Duration `json:"focus"`             // in focus sessions
	Sessions int           `json:"sessions"`          // focus sessions finished
	Breaks   int           `json:"breaks,omitempty"`  // breaks taken (run out, or away long enough)
	Skipped  int           `json:"skipped,omitempty"` // breaks skipped or ended early
	Warned   int           `json:"warned,omitempty"`  // the limit warning given: 0, 80 or 100 (%)
	Wrapped  bool          `json:"wrapped,omitempty"` // the end-of-day notification was sent
}

// Store keeps FOCUS's files in Dir.
type Store struct{ Dir string }

// DateOf is a day's file name.
func DateOf(t time.Time) string { return t.Format("2006-01-02") }

// Day reads a day; a day with no file is empty.
func (s Store) Day(date string) Day {
	d := Day{Date: date}
	s.read(filepath.Join("days", date+".json"), &d)
	return d
}

// SaveDay writes a day.
func (s Store) SaveDay(d Day) error { return s.write(filepath.Join("days", d.Date+".json"), d) }

// Week is the seven days up to today, oldest first.
func (s Store) Week(today time.Time) []Day {
	var days []Day
	for i := 6; i >= 0; i-- {
		days = append(days, s.Day(DateOf(today.AddDate(0, 0, -i))))
	}
	return days
}

// Clock reads the timer as it was left.
func (s Store) Clock() Clock {
	var c Clock
	s.read("clock.json", &c)
	return c
}

// SaveClock writes the timer.
func (s Store) SaveClock(c Clock) error { return s.write("clock.json", c) }

// read decodes one of the files into v; a missing or broken file leaves v as it was.
func (s Store) read(name string, v any) {
	if data, err := os.ReadFile(filepath.Join(s.Dir, name)); err == nil {
		json.Unmarshal(data, v)
	}
}

// write saves v as one of the files.
func (s Store) write(name string, v any) error {
	p := filepath.Join(s.Dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

// Dur writes a duration the short way: "2h 05m", "6h", "25m", "40s".
func Dur(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d >= time.Hour && d%time.Hour == 0:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d >= time.Hour:
		return fmt.Sprintf("%dh %02dm", int(d.Hours()), int(d.Minutes())%60)
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}

// Countdown writes a time as "18:42".
func Countdown(d time.Duration) string {
	s := int(d.Round(time.Second).Seconds())
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}
