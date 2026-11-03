---
name: crew-feature
description: Add or change a feature in crew itself. Use for any change to crew's behavior or screens, so it lands in both the terminal UI and the web UI and the rules are written once.
---

# A crew feature, in both UIs

crew has a terminal UI (`internal/ui/terminal`: Bubble Tea) and, being piloted, a web UI (`internal/ui/web`,
served by `crew serve`). Read `docs/web-ui.md` first: its table says which sections have a web screen.

## Order

1. **The rule goes in a service or `internal/core`**, never in `app/` or in JS. Anything a UI would
   have to decide (a check, a limit, a queue, what a status means) belongs below both UIs. Reuse what's
   there before adding.
2. **Expose it once:** a method on `core`, and, if the web UI needs it, an API call (`docs/web-ui.md`).
   Commands and shortcuts are added to their registries, not to a UI's own list.
3. **Terminal UI:** the key, popup or view, in the file for its area (CLAUDE.md, Layout). Never block
   `Update`.
4. **Web UI:** the screen for it, if its section has one. Draw and send only; no rule in JS.
5. **Parity:** if a section has a web screen, a feature isn't done until it works there too, or the row
   in `docs/web-ui.md` says it doesn't yet (and why). Don't leave a gap unwritten.

## Check

- Test the rule where it lives (`core`, a service, the API with `httptest`). A key or screen that
  broke gets a case in `app/smoke_test.go`.
- `gofmt -l internal && go build ./... && go vet ./... && go test ./...`
- Don't launch crew; the user tests by hand. Update `docs/next-tasks.md` and, if a section gained or
  lost a web screen, the table in `docs/web-ui.md`.

## Keep

Agents propose, you apply: nothing here lets an agent push, post or write task files. `desktop/` gets
no crew features. ⌘ keys, never ctrl, in anything shown.
