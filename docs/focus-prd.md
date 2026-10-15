# PRD: FOCUS

> Reminders have moved to their own section, REMINDERS. FOCUS is now a screen-time and breaks tool
> for the whole Mac: it has nothing to do with tasks, agents or PRs.

## Problem

Developers spend most of the day at the screen, often in one long stretch, and a notification
that says "take a break" is easy to dismiss. Nothing helps you pace the day: there's no focus timer,
no break you actually take, and no honest count of how long you were at the Mac.

The research behind this:
- **Focus timers.** Pomodoro-style apps for developers all have the same core: focus and break
  lengths, break notifications, and daily and weekly session stats.
- **Coding-time trackers like WakaTime.** People like the breakdown by project and the daily goals.
  They dislike the price and the gaps when offline. So crew's version should be local and free.
- **Eye breaks.** A 2022 Aston University study tested the 20-20-20 rule: every 20 minutes, look 20
  feet away for 20 seconds. Software reminders cut digital eye strain, but the benefit was gone a
  week after the reminders stopped. So the reminder has to be built in, not left to habit.
- **Moving.** Sitting studies find that a few minutes of light movement every 30 minutes helps
  (blood sugar, energy, back), and that an hourly break doesn't do as much. So the move break is
  every 30 minutes, 3 minutes long.
- **Breaks people take.** Break apps that only notify are easy to ignore; the ones people keep
  (Stretchly, Time Out) show the break on screen with a countdown, and let you postpone or skip it
  rather than locking you out.
- **Screen time.** Counting only crew's own window misses most of the day. macOS's idle time
  (`HIDIdleTime`) tells whether you're at the Mac in any app, without tracking what you do.
- **Do Not Disturb.** macOS has no public command to turn it on, but `shortcuts run "<name>"` runs a
  Shortcut that sets a Focus mode.

## Goal

A FOCUS section that paces your day at the Mac without getting in the way:
- start a focus session in one key (pomodoro or flow), and see it ticking wherever you are;
- take move breaks that feel like breaks, and quiet eye breaks;
- see honestly how long you were at the Mac today, with a limit and an end of day.

Everything is local, costs no tokens, and turns off with the section.

## Non-goals

- Anything about tasks, agents or PRs (that's what the other sections are for).
- Per-app tracking, teams, leaderboards (WakaTime's paid features).
- Blocking apps or websites, or locking the screen: every warning can be ignored.

## Requirements

### F1. Settings
- `e` on the FOCUS page opens a form; `⌘S` saves to `~/.crew/focus/settings.json`, esc cancels.
  Until the file exists, `CREW_FOCUS_MIN`, `CREW_BREAK_MIN`, `CREW_EYE_BREAKS`, `CREW_SIT_MIN`,
  `CREW_FOCUS_ON` and `CREW_FOCUS_OFF` are the defaults.
- The settings: the timer mode (pomodoro or flow); focus 25m, break 5m, long break 15m every 4
  sessions; a move break every 30m for 3m; an eye break every 20m (0 off); the break screen (full screen; or in crew, or off);
  a daily limit (6h, 0 off); an end of day (e.g. 19:00, empty off); Do Not Disturb Shortcuts.

### F2. Focus timer
- `⌘⇧F` (anywhere), `s` on the FOCUS page, or `/focus [minutes]` starts a session; the same key
  stops it. The help line starts with `● focus 18:42`, and the sidebar's FOCUS row shows the time.
- Pomodoro: at the end, a notification and a short break, or a long one every 4th session.
- Flow: it counts up; stopping it starts a break of a fifth of the time worked (at least 1 minute).
- The timer survives quitting crew (`clock.json`). The Do Not Disturb Shortcuts run (with
  `shortcuts run`) when a session starts and ends.

### F3. Breaks
- **Move:** after 30 minutes active (it waits for a focus session to end), a 3-minute move break.
- **Eyes:** every 20 minutes active, the help line says "look 20 ft away · 20s". No notification.
- **More time:** two minutes before a session ends, a notification and the help line say so; `⌘⇧E`
  adds 15 minutes from anywhere, and time's up offers 5, 15 or 25 more (`1` `2` `3`). Twice per
  session at most. A move break can be put off the same way.
- **Auto start** (a setting, on): time's up unanswered for 30 seconds starts the break, and the next
  session starts 15 seconds after the break, once you're back at the Mac. **Start when crew opens**
  (a setting, on) starts a session as crew opens.
- **The break screen:** when a focus session or a move break's time is up, crew's app brings its
  window in front of every app, filling the screen and staying on top (`notify.Window`), and FOCUS
  shows three steps: time's up (`⏎` starts the break, `1` `2` `3` keep going), the break
  counting down (`e` then `y` ends it early, counted as skipped), and break done (`⏎` starts the
  next focus and puts the window back, `esc` not yet). `crew` in settings keeps the steps inside
  crew's window; `off` only notifies and starts the break. `b` and a flow session's end start the
  break at once. macOS doesn't let an app lock you out: ⌘Tab and Force Quit still work.
- Being away as long as the break that's due counts as taking it. Breaks taken and skipped are
  counted per day.

### F4. Screen time
- crew reads the Mac's idle time (`ioreg -c IOHIDSystem`, `HIDIdleTime`) every 10 seconds, in the
  background. Active is less than a minute without input, in any app. Without it (not a Mac, or
  ioreg fails) crew counts only while its window is in front and you've used it in the last 5
  minutes.
- With a daily limit, a notification and the help line warn at 80% and 100%. At the end of day, a
  notification says to wrap up and the help line keeps saying so.
- The FOCUS page shows the timer, **Next** (move, eyes, end of day), **Today** (active time,
  sessions, breaks taken and skipped) and **This week** (a bar per day, marked over the limit).
  Days are saved in `~/.crew/focus/days/<date>.json`.

### F5. The section
- FOCUS is a section like the others: `f` on home turns it on or off. When it's off, there's no
  sidebar row, no counting, no breaks and no timer.
- Its keys: `s` start or stop, `b` break, `+`/`−` 5 minutes, `e` settings.

## Where it goes in the code

| Piece | Where |
|---|---|
| Settings, the clock, days, storage | `internal/sections/focus/focus.go` |
| The settings form | `internal/sections/focus/form.go` |
| The Mac's idle time | `internal/sections/focus/idle.go` |
| The page, the ticker, breaks and the break screen, the help-line status, `/focus`, ⌘⇧F | `internal/sections/focus/section.go` |

A tick arrives every second while a timer, the break screen or an eye break is on, and every 15
seconds otherwise. It counts time, fires breaks and warnings, and saves the day once a minute.
Nothing blocks `Update`: ioreg and the Shortcuts run in a `tea.Cmd`.

s, notifications | `internal/sections/focus/section.go` |
| `⌘⇧F`, `/focus`, `/remind`, the palette | `shortcuts.go`, `keys.go` |
| Section on/off | `sections.go` (`sectionDefs`) |

A tick (`focusTickMsg`) arrives every second while a timer or eye break is on screen, and every
15 seconds otherwise. It counts time, fires reminders and breaks, and saves the day once a minute.
Nothing blocks `Update`: the Shortcuts for Do Not Disturb run in a `tea.Cmd`.

## Later
- Daily goals ("4 focus sessions"), streaks.
- A menu-bar timer (needs the app, `desktop/`, to change).

## How we'll know it works
- `⌘⇧F` starts 25 minutes and the help line counts down. At 0 crew's window fills the screen above
  the app you're in and asks; `⏎` starts the break, which counts down; when it ends, `⏎` starts the
  next session and the window goes back.
- Thirty active minutes without a break bring a move break; walking away for 3 minutes resets it.
- Today grows while you use any app and stops while you're away.
- Turning FOCUS off with `f` on home removes all of the above.
