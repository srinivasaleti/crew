// Package focus is FOCUS's data: the focus timer, screen time per day, reminders, and their settings.
// It's kept in ~/.crew/focus as small JSON files. No UI.
package focus

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Settings are FOCUS's lengths, from the environment. A zero Eye or Sit turns that break off.
type Settings struct {
	Focus, Break time.Duration // a session, and the break after it
	Eye          time.Duration // time in crew between eye breaks (20-20-20)
	Sit          time.Duration // time in crew without a pause before "stand up"
	On, Off      string        // Shortcuts run when a session starts and ends (Do Not Disturb)
}

// LoadSettings reads CREW_FOCUS_MIN, CREW_BREAK_MIN, CREW_EYE_BREAKS, CREW_SIT_MIN (minutes) and
// CREW_FOCUS_ON / CREW_FOCUS_OFF (Shortcut names).
func LoadSettings() Settings {
	min := func(key string, def int) time.Duration {
		n, err := strconv.Atoi(os.Getenv(key))
		if err != nil || n < 0 {
			n = def
		}
		return time.Duration(n) * time.Minute
	}
	return Settings{Focus: max(min("CREW_FOCUS_MIN", 25), time.Minute), Break: max(min("CREW_BREAK_MIN", 5), time.Minute),
		Eye: min("CREW_EYE_BREAKS", 20), Sit: min("CREW_SIT_MIN", 60),
		On: os.Getenv("CREW_FOCUS_ON"), Off: os.Getenv("CREW_FOCUS_OFF")}
}

// Clock is the timer: a focus session, a break, or nothing.
type Clock struct {
	Kind  string    `json:"kind"` // "focus", "break" or ""
	Label string    `json:"label,omitempty"`
	Start time.Time `json:"start"`
	Ends  time.Time `json:"ends"`
}

// Running reports whether the clock is on.
func (c Clock) Running() bool { return c.Kind != "" }

// Left is the time left, never below zero.
func (c Clock) Left(now time.Time) time.Duration { return max(c.Ends.Sub(now), 0) }

// Day is one day's time in crew.
type Day struct {
	Date     string                   `json:"date"`     // 2006-01-02
	Crew     time.Duration            `json:"crew"`     // crew in front and you active
	Agents   time.Duration            `json:"agents"`   // any agent working
	Focus    time.Duration            `json:"focus"`    // in focus sessions
	Sessions int                      `json:"sessions"` // focus sessions finished
	By       map[string]time.Duration `json:"by"`       // Crew split by what you were in
}

// Reminder is something to tell you at a time.
type Reminder struct {
	At   time.Time `json:"at"`
	Text string    `json:"text"`
}

// Store keeps FOCUS's files in Dir.
type Store struct{ Dir string }

// DateOf is a day's file name.
func DateOf(t time.Time) string { return t.Format("2006-01-02") }

// Day reads a day; a day with no file is empty.
func (s Store) Day(date string) Day {
	d := Day{Date: date}
	s.read(filepath.Join("days", date+".json"), &d)
	if d.By == nil {
		d.By = map[string]time.Duration{}
	}
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

// Reminders reads the reminders, soonest first.
func (s Store) Reminders() []Reminder {
	var rs []Reminder
	s.read("reminders.json", &rs)
	sort.Slice(rs, func(i, j int) bool { return rs[i].At.Before(rs[j].At) })
	return rs
}

// SaveReminders writes the reminders.
func (s Store) SaveReminders(rs []Reminder) error { return s.write("reminders.json", rs) }

// Clock reads the timer as it was left.
func (s Store) Clock() Clock {
	var c Clock
	s.read("clock.json", &c)
	return c
}

// SaveClock writes the timer.
func (s Store) SaveClock(c Clock) error { return s.write("clock.json", c) }

func (s Store) read(name string, v any) {
	if data, err := os.ReadFile(filepath.Join(s.Dir, name)); err == nil {
		json.Unmarshal(data, v)
	}
}

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

// clockTime matches a time of day: 16:00, 4pm, 4:30pm, 4 pm.
var clockTime = regexp.MustCompile(`^(\d{1,2})(?::(\d{2}))?\s*(am|pm)?$`)

// ParseRemind reads "<when> <what>": a delay ("30m", "1h30m", "90s") or a time of day ("16:00",
// "4pm", "4:30pm"; tomorrow if it has passed today), then what to remind you of.
func ParseRemind(arg string, now time.Time) (Reminder, error) {
	f := strings.Fields(arg)
	if len(f) == 0 {
		return Reminder{}, fmt.Errorf("say when as 30m, 1h30m, 16:00 or 4pm, then what: /remind 30m stand-up")
	}
	when, rest := f[0], f[1:]
	if len(rest) > 0 && (strings.EqualFold(rest[0], "am") || strings.EqualFold(rest[0], "pm")) { // "4 pm call"
		when, rest = when+rest[0], rest[1:]
	}
	what := strings.Join(rest, " ")
	if what == "" {
		what = "Reminder"
	}
	if d, err := time.ParseDuration(when); err == nil && d > 0 {
		return Reminder{At: now.Add(d), Text: what}, nil
	}
	m := clockTime.FindStringSubmatch(strings.ToLower(when))
	if m == nil {
		return Reminder{}, fmt.Errorf("say when as 30m, 1h30m, 16:00 or 4pm, then what: /remind 30m stand-up")
	}
	h, _ := strconv.Atoi(m[1])
	mins, _ := strconv.Atoi(m[2])
	switch {
	case m[3] == "pm" && h < 12:
		h += 12
	case m[3] == "am" && h == 12:
		h = 0
	}
	if h > 23 || mins > 59 {
		return Reminder{}, fmt.Errorf("%s isn't a time of day", when)
	}
	at := time.Date(now.Year(), now.Month(), now.Day(), h, mins, 0, 0, now.Location())
	if !at.After(now) {
		at = at.AddDate(0, 0, 1)
	}
	return Reminder{At: at, Text: what}, nil
}

// Dur writes a duration the short way: "2h 05m", "25m", "40s".
func Dur(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d >= time.Hour:
		return fmt.Sprintf("%dh %02dm", int(d.Hours()), int(d.Minutes())%60)
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}

// Countdown writes time left as "18:42".
func Countdown(d time.Duration) string {
	s := int(d.Round(time.Second).Seconds())
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}
