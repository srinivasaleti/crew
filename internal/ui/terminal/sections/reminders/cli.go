package reminders

import (
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// CLI is `crew remind …`, what the REMINDERS chat's Claude runs (and you can too):
//
//	crew remind list [--all]                                  what's set (--all: done and deleted too)
//	crew remind add --at <when> [--every <repeat>] <what>     a new reminder
//	crew remind edit <id> [--at …] [--every …|never] [--text …]
//	crew remind delete <id>…                                  (restore brings one back)
//	crew remind restore <id>…
//	crew remind pause <id>…|all [--until <when>]
//	crew remind resume <id>…|all
func CLI(path string, args []string, out io.Writer) int {
	s, now := Store{Path: path}, time.Now()
	usage := "usage: crew remind list [--all] | add --at <when> [--every <repeat>] <what> | edit <id> [--at …] [--every …] [--text …] | delete <id>… | restore <id>… | pause <id>…|all [--until <when>] | resume <id>…|all"
	if len(args) == 0 {
		fmt.Fprintln(out, usage)
		return 2
	}
	fs := flag.NewFlagSet("crew remind "+args[0], flag.ContinueOnError)
	fs.SetOutput(out)
	at := fs.String("at", "", "when it fires: 30m, 4pm, tomorrow 9am, fri 5pm, nov 1 10am")
	every := fs.String("every", "\x00", "how it repeats: never, day, weekday, week, month, year, 2 weeks, mon,wed,fri")
	text := fs.String("text", "", "what to remind you of")
	until := fs.String("until", "", "when a pause ends")
	all := fs.Bool("all", false, "include done and deleted reminders")
	cmd := args[0]
	// Flags may come after the id or the text too, so they're read from anywhere.
	var rest []string
	for a := args[1:]; len(a) > 0; {
		if err := fs.Parse(a); err != nil {
			return 2
		}
		if a = fs.Args(); len(a) > 0 {
			rest, a = append(rest, a[0]), a[1:]
		}
	}
	err := s.Change(func(rs []Reminder) ([]Reminder, error) {
		switch cmd {
		case "list":
			List(out, rs, now, *all)
			return rs, nil
		case "add":
			what := strings.Join(rest, " ")
			if what == "" {
				what = *text
			}
			r, err := build(Reminder{ID: NextID(rs), Text: what}, *at, *every, now)
			if err != nil {
				return nil, err
			}
			if r.Text == "" {
				return nil, fmt.Errorf("say what to remind you of: crew remind add --at 4pm stand-up")
			}
			fmt.Fprintln(out, "added", Line(r, now))
			return append(rs, r), nil
		case "edit":
			if len(rest) == 0 {
				return nil, fmt.Errorf("edit takes an id: crew remind edit 3 --at 5pm")
			}
			ids, err := idsOf(rest[:1], rs)
			if err != nil || len(ids) != 1 {
				return nil, fmt.Errorf("no reminder %s · crew remind list shows the ids", rest[0])
			}
			r := Find(rs, ids[0])
			if *text != "" {
				r.Text = *text
			} else if len(rest) > 1 {
				r.Text = strings.Join(rest[1:], " ")
			}
			if *r, err = build(*r, *at, *every, now); err != nil {
				return nil, err
			}
			r.Done = r.Done && *at == "" // a new time brings a done one back
			fmt.Fprintln(out, "edited", Line(*r, now))
			return rs, nil
		case "delete", "restore", "pause", "resume":
			ids, err := idsOf(rest, rs)
			if err != nil {
				return nil, err
			}
			var pauseEnd time.Time
			if *until != "" {
				if pauseEnd, err = ParseWhen(*until, now); err != nil {
					return nil, err
				}
			}
			for _, id := range ids {
				r := Find(rs, id)
				switch cmd {
				case "delete":
					r.Deleted = true
				case "restore":
					r.Deleted = false
				case "pause":
					r.Paused, r.Until = true, pauseEnd
				case "resume":
					Resume(r, now)
				}
				fmt.Fprintln(out, cmd+"d", Line(*r, now))
			}
			return rs, nil
		}
		return nil, fmt.Errorf("%s", usage)
	})
	if err != nil {
		fmt.Fprintln(out, err)
		return 1
	}
	return 0
}

// build sets a reminder's time and repeat from what was given ("" or "\x00" leaves them).
func build(r Reminder, at, every string, now time.Time) (Reminder, error) {
	if every != "\x00" {
		e, err := ParseEvery(every)
		if err != nil {
			return r, err
		}
		r.Every = e
	}
	if at != "" {
		t, err := ParseWhen(at, now)
		if err != nil {
			return r, err
		}
		r.At = t
	} else if r.At.IsZero() {
		return r, fmt.Errorf("say when: --at 4pm (or 30m, tomorrow 9am, fri 5pm)")
	}
	r.At = Align(r.At, r.Every)
	return r, nil
}

// idsOf reads ids ("3", "#3") or "all" (every reminder not deleted).
func idsOf(args []string, rs []Reminder) ([]int, error) {
	var ids []int
	for _, a := range args {
		if a == "all" {
			for _, r := range rs {
				if !r.Deleted {
					ids = append(ids, r.ID)
				}
			}
			continue
		}
		id, err := strconv.Atoi(strings.TrimPrefix(a, "#"))
		if err != nil || Find(rs, id) == nil {
			return nil, fmt.Errorf("no reminder %s · crew remind list shows the ids", a)
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("say which: an id from crew remind list, or all")
	}
	return ids, nil
}

// List writes the reminders, one a line.
func List(out io.Writer, rs []Reminder, now time.Time, all bool) {
	fmt.Fprintln(out, "now: "+now.Format("Mon Jan 2 2006 3:04pm"))
	n := 0
	for _, r := range rs {
		if all || !r.Done && !r.Deleted {
			fmt.Fprintln(out, Line(r, now))
			n++
		}
	}
	if n == 0 {
		fmt.Fprintln(out, "no reminders")
	}
}

// Line is one reminder as text: "#3 Fri 5:00pm · every week (Fri) · timesheet · paused".
func Line(r Reminder, now time.Time) string {
	s := fmt.Sprintf("#%d %s · %s · %s", r.ID, When(r.At, now), Describe(r.Every, r.At), r.Text)
	switch {
	case r.Deleted:
		s += " · deleted"
	case r.Done:
		s += " · done"
	case r.Paused && !r.Until.IsZero():
		s += " · paused until " + When(r.Until, now)
	case r.Paused:
		s += " · paused"
	}
	return s
}
