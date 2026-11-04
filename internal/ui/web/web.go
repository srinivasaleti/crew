// Package web serves crew's web UI (docs/web-ui.md) on this Mac only: a loopback port with a token,
// so nothing else on the machine can drive crew. It's the pilot's JSON API and event stream over
// core, and the page in static/.
package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"time"

	"crew/internal/browser"
	"crew/internal/core"
	"crew/internal/ui/web/api"

	// Each section adds its routes in init (api.Register).
	_ "crew/internal/ui/web/sections/focus"
	_ "crew/internal/ui/web/sections/home"
	_ "crew/internal/ui/web/sections/jira"
	_ "crew/internal/ui/web/sections/mcp"
	_ "crew/internal/ui/web/sections/notes"
	_ "crew/internal/ui/web/sections/prs"
	_ "crew/internal/ui/web/sections/reminders"
	_ "crew/internal/ui/web/sections/reqs"
	_ "crew/internal/ui/web/sections/tasks"
	_ "crew/internal/ui/web/sections/todos"
)

//go:embed static
var static embed.FS

// Serve listens on a random loopback port until ctx ends, and returns the URL to open (it carries
// the token the page needs; the first visit turns it into a cookie). quit ends crew serve, which
// it calls after the page chose the terminal UI: the app then starts that one.
func Serve(ctx context.Context, c *core.Core, quit func()) (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	b := make([]byte, 16)
	rand.Read(b)
	tok := hex.EncodeToString(b)
	files, _ := fs.Sub(static, "static")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/events", events(c))
	mux.HandleFunc("GET /api/sections", func(w http.ResponseWriter, r *http.Request) {
		type sec struct {
			Key string `json:"key"`
			On  bool   `json:"on"`
		}
		var out []sec
		for _, k := range core.SectionOrder {
			out = append(out, sec{k, c.SectionOn(k)})
		}
		api.Reply(w, out)
	})
	mux.HandleFunc("GET /api/ui", func(w http.ResponseWriter, r *http.Request) { api.Reply(w, map[string]string{"mode": c.UI()}) })
	mux.HandleFunc("POST /api/ui", func(w http.ResponseWriter, r *http.Request) {
		var m struct{ Mode string }
		json.NewDecoder(r.Body).Decode(&m)
		if err := c.SetUI(m.Mode); err != nil {
			api.Fail(w, err, 400)
			return
		}
		api.Reply(w, m)
		if m.Mode == "terminal" {
			go func() { time.Sleep(200 * time.Millisecond); quit() }() // after the reply is out
		}
	})
	mux.HandleFunc("GET /api/theme", func(w http.ResponseWriter, r *http.Request) { api.Reply(w, map[string]string{"theme": c.Theme()}) })
	mux.HandleFunc("POST /api/theme", func(w http.ResponseWriter, r *http.Request) {
		var m struct{ Theme string }
		if !api.Body(w, r, &m) {
			return
		}
		if err := c.SetTheme(m.Theme); err != nil {
			api.Fail(w, err, 400)
			return
		}
		api.Reply(w, m)
	})
	mux.HandleFunc("POST /api/open", func(w http.ResponseWriter, r *http.Request) {
		var m struct{ URL string }
		if !api.Body(w, r, &m) {
			return
		}
		if !strings.HasPrefix(m.URL, "https://") && !strings.HasPrefix(m.URL, "http://") {
			api.Fail(w, fmt.Errorf("only web links open"), 400)
			return
		}
		browser.Open(m.URL) // in your browser: following it in the window would leave crew
		api.Reply(w, struct{}{})
	})
	for _, m := range api.Modules() {
		m.Routes(c, mux)
		if m.Run != nil {
			go m.Run(ctx, c) // each checks c.SectionOn itself, as the terminal's sections do
		}
	}
	mux.Handle("/", http.FileServer(http.FS(files)))
	srv := &http.Server{Handler: guard(tok, ln.Addr().String(), mux)}
	go srv.Serve(ln)
	go func() { <-ctx.Done(); srv.Close() }()
	return "http://" + ln.Addr().String() + "/?token=" + tok, nil
}

// guard lets a request through only with the token (in the cookie, or in the URL the first time)
// and for our own address, which stops another web page from reaching us through a rebound DNS name.
func guard(tok, addr string, next http.Handler) http.Handler {
	ok := func(got string) bool { return subtle.ConstantTimeCompare([]byte(got), []byte(tok)) == 1 }
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != addr {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if t := r.URL.Query().Get("token"); ok(t) {
			http.SetCookie(w, &http.Cookie{Name: "crew", Value: t, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
		} else if c, err := r.Cookie("crew"); err != nil || !ok(c.Value) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// events streams core's events as server-sent events, until the page goes away.
func events(c *core.Core) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fl, ok := w.(http.Flusher)
		if !ok {
			api.Fail(w, fmt.Errorf("no streaming"), 500)
			return
		}
		ch := c.Subscribe(r.Context())
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		fl.Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case ev := <-ch:
				b, _ := json.Marshal(ev)
				fmt.Fprintf(w, "data: %s\n\n", b)
				fl.Flush()
			}
		}
	}
}
