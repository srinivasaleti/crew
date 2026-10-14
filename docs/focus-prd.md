# PRD: FOCUS

> Reminders (F3) have moved to their own section, REMINDERS (`internal/reminders`), with repeats and a
> Claude chat. FOCUS keeps the timer, breaks and screen time.

## Problem

Developers spend most of the day at the screen, often in one long stretch. crew is where much of
that day now happens: tasks, PRs and requests. But nothing in crew helps you pace it. There's no
focus timer and no reminder to take a break or rest your eyes. You can't set a "remind me at 4pm",
and you can't see how long you were actually at it today, or how long your agents worked for you.

The research behind this:
- **Focus timers.** Pomodoro-style apps for developers all have the same core: focus and break
  lengths, break notifications, and daily and weekly session stats.
- **Coding-time trackers like WakaTime.** People like the breakdown by project and the daily goals.
  They dislike the price and the gaps when offline. So crew's version should be local and free.
- **Eye breaks.** A 2022 Aston University study tested the 20-20-20 rule: every 20 minutes, look 20
  feet away for 20 seconds. Software reminders cut digital eye strain, but the benefit was gone a
  week after the reminders stopped. So the reminder has to be built in, not left to habit.
- **Do Not Disturb.** macOS has no public command to turn it on, but `shortcuts run "<name>"` runs a
  Shortcut that sets a Focus mode.

## Goal

A FOCUS section that paces your day without getting in the way:
- start a focus session in one key, and see it ticking wherever you are;
- get gentle eye and stand-up breaks;
- set reminders in plain words ("30m stand-up", "4pm call");
- see honestly how long you were in crew today, on what, and how long your agents worked.

Everything is local, costs no tokens, and turns off with the section.

## Non-goals

- Tracking time outside crew (other apps, the editor). crew only counts its own window.
- Teams, leaderboards, invoicing (WakaTime's paid features).
- Blocking apps or websites.

## Requirements

### F1. Focus timer
- `⌘⇧F` (anywhere), `s` on the FOCUS page, or `/focus [minutes]` starts a session. The default
  length is 25 minutes (`CREW_FOCUS_MIN`). It's named after the task you're on, if any. Pressing
  the same key again stops it.
- While it runs, the help line starts with `● focus 18:42 · fix-login`, and the sidebar's FOCUS row
  shows the time left.
- When it ends, crew sends a macOS notification and starts a break (`CREW_BREAK_MIN`, default 5).
  When the break ends, another notification says "s starts the next one". `b` starts a break any
  time.
- The timer survives quitting crew (`~/.crew/focus/clock.json`).
- Optional Do Not Disturb: `CREW_FOCUS_ON` and `CREW_FOCUS_OFF` name Shortcuts that crew runs (with
  `shortcuts run`) when a session starts and ends.

### F2. Breaks
- **Eyes:** after every 20 minutes in crew (`CREW_EYE_BREAKS`, `0` turns it off), the help line
  says "look at something 20 feet away · 20s", counting down for 20 seconds. crew also sends a
  notification, since the study found reminders are what make it work.
- **Stand up:** after 60 minutes in crew without a 5-minute pause (`CREW_SIT_MIN`, `0` off), a
  notification says to stand up and stretch, with `b` for a break.

### F3. Reminders
- `r` on the FOCUS page, or `/remind <when> <what>` in any chat. `<when>` is a delay (`30m`,
  `1h30m`) or a time of day (`16:00`, `4pm`, `4:30pm`; today, or tomorrow if that time has passed).
- At that time, crew sends a notification and the help line shows it.
- Reminders are listed on the FOCUS page; `↑↓` picks one and `x` deletes it. They're saved in
  `~/.crew/focus/reminders.json`, so they survive quitting crew. One that came due while crew was
  closed fires when crew opens.

### F4. Screen time
- crew counts time while its window is in front and you've pressed a key or used the mouse in the
  last 5 minutes. A lunch break with crew open doesn't count.
- That time is split by what you were in: a task (by its title), PULL REQUESTS, REQUESTS or home.
- It also counts the time any agent was working, and the focus sessions and their minutes.
- The FOCUS page shows **Today** (total, per item with bars, agents, focus sessions) and **This
  week** (a bar per day). Days are saved in `~/.crew/focus/days/<date>.json`.

### F5. The section
- FOCUS is a section like the others: `f` on home turns it on or off. When it's off, there's no
  sidebar row, no counting, no breaks, no reminders and no timer.
- The FOCUS page (its sidebar row) shows the timer, Today, This week, reminders and the break
  settings. Its keys are `s` focus, `b` break, `r` remind, `↑↓` and `x` for reminders, and `+`/`−`
  to add or take off 5 minutes.

## Where it goes in the code

| Piece | Where |
|---|---|
| Settings, the clock, days, reminders, parsing "30m"/"4pm", storage | new `internal/focus` package, no UI |
| The FOCUS row and page, the ticker, the help-line status, breaks, notifications | `internal/sections/focus/section.go` |
| `⌘⇧F`, `/focus`, `/remind`, the palette | `shortcuts.go`, `keys.go` |
| Section on/off | `sections.go` (`sectionDefs`) |

A tick (`focusTickMsg`) arrives every second while a timer or eye break is on screen, and every
15 seconds otherwise. It counts time, fires reminders and breaks, and saves the day once a minute.
Nothing blocks `Update`: the Shortcuts for Do Not Disturb run in a `tea.Cmd`.

## Later
- "While you're on break": offer work to hand to agents (PR comments to fix, a plan step) when a
  break starts.
- Daily goals ("4 focus sessions"), streaks.
- A menu-bar timer (needs the app, `desktop/`, to change).

## How we'll know it works
- `⌘⇧F` starts 25 minutes and the help line counts down. At 0, a notification and a 5-minute break
  start, and a notification comes when the break ends.
- `/remind 1m test` notifies a minute later, even after quitting and reopening crew.
- After 20 minutes in crew, the eye-break line and a notification appear.
- The FOCUS page's Today grows while you work, doesn't grow while you're away or in another app,
  and splits time by task.
- Turning FOCUS off with `f` on home removes all of the above.
