package focus

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"crew/internal/core"
	fc "crew/internal/focus"
	"crew/internal/ui/web/api"
)

// FOCUS: the focus service's timer (internal/focus), ticked here while the web UI is open, exactly as
// the terminal ticks its own. The page gets the state on each change and counts down itself between.
func init() { api.Register(api.Module{Key: "focus", Routes: focusRoutes, Run: focusRun}) }

// focusNow is the one timer crew serve runs, made with the routes.
var focusNow struct {
	sync.Mutex
	t      *fc.Timer
	c      *core.Core
	notice string    // the last note for you ("Focus for 25m · …"), shown by the page
	at     time.Time // when it was said
	sent   string    // the last state published, without what counts every second
	sentAt time.Time
}

// focusEnv is what the timer gets from the web UI.
type focusEnv struct{}

func (focusEnv) On() bool              { return focusNow.c.SectionOn("focus") }
func (focusEnv) Blurred() bool         { return false } // only used when the Mac's idle time can't be read
func (focusEnv) Names(s string) string { return s }
func (focusEnv) Screen(bool)           {} // the state says the screen's step; the page draws it
func (focusEnv) Notice(s string)       { focusNow.notice, focusNow.at = s, time.Now() }

// focusState is what the page draws. Durations are nanoseconds, times RFC 3339.
type focusState struct {
	On         bool          `json:"on"`
	Settings   fc.Settings   `json:"settings"`
	Clock      fc.Clock      `json:"clock"`
	Screen     string        `json:"screen"` // the break screen's step: "ask", "break", "back" or ""
	Due        fc.Clock      `json:"due"`    // the break "ask" offers
	Why        string        `json:"why"`
	Advice     string        `json:"advice"` // what to do on the break offered or running
	Asked      time.Time     `json:"asked"`
	BackAt     time.Time     `json:"backAt"`
	Here       bool          `json:"here"`
	ExtrasLeft int           `json:"extrasLeft"`
	Extras     []int         `json:"extras"` // minutes "keep going" adds
	AutoBreak  time.Duration `json:"autoBreak"`
	AutoFocus  time.Duration `json:"autoFocus"`
	Later      fc.Clock      `json:"later"`
	EyeUntil   time.Time     `json:"eyeUntil"`
	MoveDue    bool          `json:"moveDue"`
	MoveIn     time.Duration `json:"moveIn"`
	EyeIn      time.Duration `json:"eyeIn"`
	Whole      bool          `json:"whole"` // screen time counts the whole Mac
	Today      fc.Day        `json:"today"`
	Week       []fc.Day      `json:"week"`
	Notice     string        `json:"notice,omitempty"`
}

// focusSnapshot is the state now, the week included (it reads seven files) when week is set; the
// caller holds the lock.
func focusSnapshot(now time.Time, week bool) focusState {
	t := focusNow.t
	advice := fc.Advice(t.Clock.Label)
	if t.Screen == "ask" {
		advice = fc.Advice(t.Due.Label)
	}
	var extras []int
	for _, d := range fc.Extras {
		extras = append(extras, int(d.Minutes()))
	}
	var days []fc.Day
	if week {
		days = t.Store.Week(now)
		days[len(days)-1] = t.Day // today as counted, not as last saved
	}
	notice := ""
	if now.Sub(focusNow.at) < 10*time.Second {
		notice = focusNow.notice
	}
	return focusState{On: focusNow.c.SectionOn("focus"), Settings: t.Set, Clock: t.Clock, Screen: t.Screen, Due: t.Due, Why: t.Why,
		Advice: advice, Asked: t.Asked, BackAt: t.BackAt, Here: t.Here(focusEnv{}, now), ExtrasLeft: t.ExtrasLeft(), Extras: extras,
		AutoBreak: fc.AutoBreak, AutoFocus: fc.AutoFocus, Later: t.Later, EyeUntil: t.EyeUntil, MoveDue: t.MoveDue(),
		MoveIn: max(t.Set.Move-t.Move, 0), EyeIn: max(t.Set.Eye-t.Eye, 0), Whole: t.Whole(now), Today: t.Day, Week: days, Notice: notice}
}

// focusPublish tells the page when the state changed (not what counts every second, which the page
// counts itself), and at least every 30 seconds for screen time; force sends it anyway.
// The caller holds the lock.
func focusPublish(now time.Time, force bool) {
	st := focusSnapshot(now, false)
	quiet := st
	quiet.Today, quiet.Week, quiet.MoveIn, quiet.EyeIn, quiet.Here = fc.Day{}, nil, 0, 0, false
	b, _ := json.Marshal(quiet)
	if !force && string(b) == focusNow.sent && now.Sub(focusNow.sentAt) < 30*time.Second {
		return
	}
	focusNow.sent, focusNow.sentAt = string(b), now
	st.Week = focusSnapshot(now, true).Week
	focusNow.c.Publish(core.Event{Kind: "focus", Data: st})
}

func focusRoutes(c *core.Core, mux *http.ServeMux) {
	focusNow.c, focusNow.t = c, fc.New(filepath.Join(c.Home(), "focus"))
	mux.HandleFunc("GET /api/focus", func(w http.ResponseWriter, r *http.Request) {
		focusNow.Lock()
		defer focusNow.Unlock()
		api.Reply(w, focusSnapshot(time.Now(), true))
	})
	// POST /api/focus/<action>: start {Minutes} (0: the settings'), stop, toggle, break, take (time's
	// up's break), extend {Index}, endearly, notyet, nudge {Minutes}, settings {Settings}.
	mux.HandleFunc("POST /api/focus/{action}", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Minutes, Index int
			Settings       *fc.Settings
		}
		if r.ContentLength != 0 && !api.Body(w, r, &in) {
			return
		}
		focusNow.Lock()
		defer focusNow.Unlock()
		t, e, now := focusNow.t, focusEnv{}, time.Now()
		t.Active = now // using the page counts as being at the Mac
		switch r.PathValue("action") {
		case "start":
			t.StartFocus(e, time.Duration(in.Minutes)*time.Minute)
		case "stop":
			t.StopClock(e)
		case "toggle":
			t.Toggle(e)
		case "break":
			t.BreakNow(e)
		case "take":
			t.TakeBreak(e)
		case "extend":
			if in.Index < 0 || in.Index >= len(fc.Extras) {
				api.Fail(w, fmt.Errorf("no such extension"), 400)
				return
			}
			t.Extend(e, fc.Extras[in.Index])
		case "endearly":
			if t.Clock.Kind == "break" {
				t.EndEarly()
			}
		case "notyet":
			t.NotYet(e)
		case "nudge":
			t.Nudge(time.Duration(in.Minutes) * time.Minute)
		case "settings":
			if in.Settings == nil {
				api.Fail(w, fmt.Errorf("no settings"), 400)
				return
			}
			if err := t.SaveSettings(*in.Settings); err != nil {
				api.Fail(w, err, 500)
				return
			}
			focusEnv{}.Notice("FOCUS settings saved")
		default:
			api.Fail(w, fmt.Errorf("no such action"), 404)
			return
		}
		focusPublish(now, true)
		api.Reply(w, focusSnapshot(now, true))
	})
}

// focusRun ticks the timer every second while FOCUS is on (the terminal ticks every 15 when nothing
// counts down; a second is cheap here), and reads the Mac's idle time every 10.
func focusRun(ctx context.Context, c *core.Core) {
	tk := time.NewTicker(time.Second)
	defer tk.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tk.C:
			if !c.SectionOn("focus") {
				continue
			}
			focusNow.Lock()
			if focusNow.t.PollIdle(now) {
				go func() {
					idle, ok := fc.ReadIdle()
					focusNow.Lock()
					focusNow.t.SetIdle(idle, ok, time.Now())
					focusNow.Unlock()
				}()
			}
			focusNow.t.Tick(focusEnv{}, now)
			focusPublish(now, false)
			focusNow.Unlock()
		}
	}
}
