// Package reminders is REMINDERS: reminders that fire once or repeat (every day, weekday, week,
// month, year, every n days/weeks/months, or on chosen weekdays), kept in one JSON file, with
// `crew remind` to manage them (what the section's Claude chat runs), the new/edit form, and what
// that chat's Claude is told and allowed. No timers here: crew's ui fires what's due (Fire).
package reminders

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Reminder is one reminder.
type Reminder struct {
	ID      int       `json:"id"`
	Text    string    `json:"text"`
	At      time.Time `json:"at"`              // when it fires next (for a done one, when it fired)
	Every   string    `json:"every,omitempty"` // how it repeats (ParseEvery); "" fires once
	Paused  bool      `json:"paused,omitempty"`
	Until   time.Time `json:"until,omitempty"` // a pause ends then; zero pauses until you resume
	Done    bool      `json:"done,omitempty"`  // a one-time reminder that fired
	Deleted bool      `json:"deleted,omitempty"`
}

// Active reports whether it will still fire (not done, deleted or paused).
func (r Reminder) Active() bool { return !r.Done && !r.Deleted && !r.Paused }

// Store is the reminders file.
type Store struct{ Path string }

// Load reads every reminder, the next to fire first.
func (s Store) Load() []Reminder {
	var rs []Reminder
	if data, err := os.ReadFile(s.Path); err == nil {
		json.Unmarshal(data, &rs)
	}
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].At.Before(rs[j].At) })
	return rs
}

// Save writes them.
// ponytail: crew and `crew remind` (the chat) both load, change and save the file; two saves in
// the same instant would lose one. Lock the file if that ever happens.
func (s Store) Save(rs []Reminder) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(rs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.Path, data, 0o644)
}

// Change loads the reminders, lets f change them, and saves them.
func (s Store) Change(f func(rs []Reminder) ([]Reminder, error)) error {
	rs, err := f(s.Load())
	if err != nil {
		return err
	}
	return s.Save(rs)
}

// NextID is the id for a new reminder.
func NextID(rs []Reminder) int {
	id := 0
	for _, r := range rs {
		id = max(id, r.ID)
	}
	return id + 1
}

// Find is the reminder with this id, or nil.
func Find(rs []Reminder, id int) *Reminder {
	if i := slices.IndexFunc(rs, func(r Reminder) bool { return r.ID == id }); i >= 0 {
		return &rs[i]
	}
	return nil
}

// Fire marks what's due at now: a one-time reminder is done, a repeating one moves to its next time
// after now (times missed while crew was closed fire once, not once each). A pause that has ended is
// lifted first. It returns the reminders that fired, and whether anything changed.
func Fire(rs []Reminder, now time.Time) (fired []Reminder, changed bool) {
	for i := range rs {
		r := &rs[i]
		if r.Paused && !r.Until.IsZero() && !r.Until.After(now) {
			Resume(r, now) // what fell in the pause doesn't fire
			changed = true
		}
		if !r.Active() || r.At.After(now) {
			continue
		}
		fired, changed = append(fired, *r), true
		if r.Every == "" {
			r.Done = true
		} else {
			r.At = catchUp(*r, now)
		}
	}
	return fired, changed
}

// catchUp is a repeating reminder's first time after now.
func catchUp(r Reminder, now time.Time) time.Time {
	at := r.At
	for i := 0; !at.After(now) && i < 100000; i++ {
		at = Next(at, r.Every)
	}
	return at
}

// Resume lifts a pause; a repeating reminder skips the times that fell in it.
func Resume(r *Reminder, now time.Time) {
	r.Paused, r.Until = false, time.Time{}
	if r.Every != "" && !r.At.After(now) {
		r.At = catchUp(*r, now)
	}
}

var weekdays = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

// dayOf reads a weekday name ("fri", "friday").
func dayOf(s string) (time.Weekday, bool) {
	s = strings.ToLower(s)
	for i, d := range weekdays {
		if len(s) >= 3 && strings.HasPrefix(d+time.Weekday(i).String()[3:], s) || s == d {
			return time.Weekday(i), true
		}
	}
	return 0, false
}

var everyN = regexp.MustCompile(`^(\d+)\s*(day|week|month|year)s?$`)

// ParseEvery reads how a reminder repeats and returns its stored form: "" (never), "day",
// "weekday", "week", "month", "year", "2 weeks", or weekdays like "mon,wed,fri". It takes "every
// day", "daily", "weekdays", "weekly", "every 2 weeks", "mon wed fri", "never".
func ParseEvery(s string) (string, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimSpace(strings.TrimPrefix(s, "every"))
	switch s {
	case "", "never", "once", "no", "none":
		return "", nil
	case "day", "daily":
		return "day", nil
	case "weekday", "weekdays", "workday", "workdays":
		return "weekday", nil
	case "week", "weekly":
		return "week", nil
	case "month", "monthly":
		return "month", nil
	case "year", "yearly", "annually":
		return "year", nil
	case "other day":
		return "2 days", nil
	case "other week":
		return "2 weeks", nil
	}
	if m := everyN.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		if n < 1 {
			return "", fmt.Errorf("every %s isn't a repeat", s)
		}
		if n == 1 {
			return m[2], nil
		}
		return fmt.Sprintf("%d %ss", n, m[2]), nil
	}
	var days []string
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '/' }) {
		if f == "and" {
			continue
		}
		d, ok := dayOf(f)
		if !ok {
			return "", fmt.Errorf("repeats can be: never, day, weekday, week, month, year, 2 weeks, or days like mon,wed,fri")
		}
		if !slices.Contains(days, weekdays[d]) {
			days = append(days, weekdays[d])
		}
	}
	sort.Slice(days, func(i, j int) bool { return slices.Index(weekdays, days[i]) < slices.Index(weekdays, days[j]) })
	return strings.Join(days, ","), nil
}

// Next is the time after at that a reminder repeating every `every` fires.
func Next(at time.Time, every string) time.Time {
	switch every {
	case "day":
		return at.AddDate(0, 0, 1)
	case "weekday":
		at = at.AddDate(0, 0, 1)
		for at.Weekday() == time.Saturday || at.Weekday() == time.Sunday {
			at = at.AddDate(0, 0, 1)
		}
		return at
	case "week":
		return at.AddDate(0, 0, 7)
	case "month":
		return addMonths(at, 1)
	case "year":
		return addMonths(at, 12)
	}
	if n, unit, ok := strings.Cut(every, " "); ok {
		k, _ := strconv.Atoi(n)
		switch strings.TrimSuffix(unit, "s") {
		case "day":
			return at.AddDate(0, 0, k)
		case "week":
			return at.AddDate(0, 0, 7*k)
		case "month":
			return addMonths(at, k)
		case "year":
			return addMonths(at, 12*k)
		}
	}
	if days := strings.Split(every, ","); every != "" { // weekdays
		for i := 1; i <= 7; i++ {
			if d := at.AddDate(0, 0, i); slices.Contains(days, weekdays[d.Weekday()]) {
				return d
			}
		}
	}
	return at.AddDate(100, 0, 0) // not a repeat: never again
}

// addMonths adds months, keeping the day of the month or the month's last day (Jan 31 → Feb 28).
// ponytail: after a short month the day stays clamped (Feb 28 → Mar 28); keep the first day too if
// that matters.
func addMonths(t time.Time, n int) time.Time {
	first := time.Date(t.Year(), t.Month()+time.Month(n), 1, t.Hour(), t.Minute(), 0, 0, t.Location())
	last := first.AddDate(0, 1, -1).Day()
	return first.AddDate(0, 0, min(t.Day(), last)-1)
}

// Align moves a first time onto a day the repeat fires on: "every weekday" set for a Saturday
// starts on Monday.
func Align(at time.Time, every string) time.Time {
	switch {
	case every == "weekday" && (at.Weekday() == time.Saturday || at.Weekday() == time.Sunday):
		return Next(at, every)
	case strings.Contains(every, ",") || slices.Contains(weekdays, every):
		if !slices.Contains(strings.Split(every, ","), weekdays[at.Weekday()]) {
			return Next(at, every)
		}
	}
	return at
}

// Describe writes how a reminder repeats: "every weekday", "every week (Fri)", "every month
// (16th)", "every Mon, Wed, Fri", or "—" for once.
func Describe(every string, at time.Time) string {
	switch every {
	case "":
		return "—"
	case "day", "weekday":
		return "every " + every
	case "week":
		return "every week (" + at.Format("Mon") + ")"
	case "month":
		return "every month (" + ordinal(at.Day()) + ")"
	case "year":
		return "every year (" + at.Format("Jan 2") + ")"
	}
	if strings.Contains(every, " ") {
		return "every " + every
	}
	var names []string
	for _, d := range strings.Split(every, ",") {
		names = append(names, strings.ToUpper(d[:1])+d[1:])
	}
	return "every " + strings.Join(names, ", ")
}

func ordinal(n int) string {
	suffix := "th"
	switch {
	case n%100 >= 11 && n%100 <= 13:
	case n%10 == 1:
		suffix = "st"
	case n%10 == 2:
		suffix = "nd"
	case n%10 == 3:
		suffix = "rd"
	}
	return strconv.Itoa(n) + suffix
}

// When writes a time the short way, relative to now: "Today 4:00pm", "Tomorrow 9:00am", "Fri
// 5:00pm" within a week, else "Nov 1 10:00am" (with the year when it isn't this year's).
func When(t, now time.Time) string {
	day := func(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()) }
	days := int(day(t).Sub(day(now)).Hours() / 24)
	clock := t.Format("3:04pm")
	switch {
	case days == 0:
		return "Today " + clock
	case days == 1:
		return "Tomorrow " + clock
	case days == -1:
		return "Yesterday " + clock
	case days > 1 && days < 7:
		return t.Format("Mon ") + clock
	case t.Year() == now.Year():
		return t.Format("Jan 2 ") + clock
	}
	return t.Format("Jan 2 2006 ") + clock
}

var (
	clockRe = regexp.MustCompile(`^(\d{1,2})(?::(\d{2}))?(am|pm)?$`)
	dateRe  = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	bad     = fmt.Errorf("when can be: 30m, 4pm, 16:30, tomorrow 9am, fri 5pm, nov 1 10am or 2026-11-01 10:00")
)

// monthOf reads a month name ("nov", "november").
func monthOf(s string) (time.Month, bool) {
	for m := time.January; m <= time.December; m++ {
		if len(s) >= 3 && strings.HasPrefix(strings.ToLower(m.String()), s) {
			return m, true
		}
	}
	return 0, false
}

// ParseWhen reads when a reminder fires: a delay ("30m", "1h30m", "in 2h"), a time of day ("4pm",
// "16:30", "4:30 pm"; tomorrow if it has passed), or a day and a time ("tomorrow 9am", "fri 5pm",
// "nov 1 10am", "2026-11-01 10:00"; a day alone means 9am). A weekday or a month day that has
// passed means the next one.
func ParseWhen(s string, now time.Time) (time.Time, error) {
	f := strings.Fields(strings.ToLower(s))
	for i := 0; i+1 < len(f); i++ { // "4 pm" → "4pm"
		if f[i+1] == "am" || f[i+1] == "pm" {
			f[i] += f[i+1]
			f = slices.Delete(f, i+1, i+2)
		}
	}
	f = slices.DeleteFunc(f, func(w string) bool { return w == "in" || w == "at" || w == "on" || w == "next" })
	if len(f) == 0 {
		return time.Time{}, bad
	}
	if d, err := time.ParseDuration(strings.Join(f, "")); err == nil && d > 0 {
		return now.Add(d).Truncate(time.Minute), nil
	}
	day, kind := now, "" // kind is how the day was given: "", "date", "weekday" or "month"
	switch {
	case f[0] == "today":
		kind, f = "date", f[1:]
	case f[0] == "tomorrow":
		day, kind, f = now.AddDate(0, 0, 1), "date", f[1:]
	case dateRe.MatchString(f[0]):
		t, err := time.ParseInLocation("2006-01-02", f[0], now.Location())
		if err != nil {
			return time.Time{}, bad
		}
		day, kind, f = t, "date", f[1:]
	default:
		if d, ok := dayOf(f[0]); ok && len(f[0]) >= 3 {
			day, kind, f = now.AddDate(0, 0, (int(d)-int(now.Weekday())+7)%7), "weekday", f[1:]
		} else if mo, ok := monthOf(f[0]); ok && len(f) >= 2 {
			n, err := strconv.Atoi(strings.TrimRight(f[1], "stndrh"))
			if err != nil || n < 1 || n > 31 {
				return time.Time{}, bad
			}
			day, kind, f = time.Date(now.Year(), mo, n, 0, 0, 0, 0, now.Location()), "month", f[2:]
		}
	}
	h, m := 9, 0
	switch {
	case len(f) == 0 && kind == "":
		return time.Time{}, bad
	case len(f) == 1:
		c := clockRe.FindStringSubmatch(f[0])
		if c == nil {
			return time.Time{}, bad
		}
		h, _ = strconv.Atoi(c[1])
		m, _ = strconv.Atoi(c[2])
		switch {
		case c[3] == "pm" && h < 12:
			h += 12
		case c[3] == "am" && h == 12:
			h = 0
		}
		if h > 23 || m > 59 {
			return time.Time{}, fmt.Errorf("%s isn't a time of day", f[0])
		}
	case len(f) > 1:
		return time.Time{}, bad
	}
	at := time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, now.Location())
	if !at.After(now) {
		switch kind {
		case "":
			at = at.AddDate(0, 0, 1) // 4pm has gone: tomorrow's
		case "weekday":
			at = at.AddDate(0, 0, 7) // fri 5pm has gone: next friday's
		case "month":
			at = at.AddDate(1, 0, 0) // jan 5 has gone: next year's
		default:
			return time.Time{}, fmt.Errorf("%s has already passed", strings.TrimSpace(s))
		}
	}
	return at, nil
}
