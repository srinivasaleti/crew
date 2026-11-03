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
	"time"

	"crew/internal/core"
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
	mux.HandleFunc("GET /api/tasks", func(w http.ResponseWriter, r *http.Request) {
		list, err := c.List()
		if list == nil && err != nil {
			fail(w, err, 500)
			return
		}
		reply(w, orEmpty(list)) // a broken task file is skipped, not fatal
	})
	mux.HandleFunc("POST /api/tasks", func(w http.ResponseWriter, r *http.Request) {
		var n core.NewTask
		if json.NewDecoder(r.Body).Decode(&n) != nil {
			fail(w, fmt.Errorf("bad request"), 400)
			return
		}
		t, err := c.Create(n)
		if err != nil {
			fail(w, err, 400)
			return
		}
		reply(w, t)
	})
	mux.HandleFunc("GET /api/tasks/{id}/chat", func(w http.ResponseWriter, r *http.Request) {
		msgs, err := c.Chat(r.PathValue("id"))
		if err != nil {
			fail(w, err, 500)
			return
		}
		reply(w, orEmpty(msgs))
	})
	mux.HandleFunc("POST /api/tasks/{id}/messages", func(w http.ResponseWriter, r *http.Request) {
		var m struct{ Text string }
		if json.NewDecoder(r.Body).Decode(&m) != nil || m.Text == "" {
			fail(w, fmt.Errorf("say something"), 400)
			return
		}
		if err := c.Send(r.PathValue("id"), m.Text); err != nil {
			fail(w, err, 400)
			return
		}
		reply(w, struct{}{})
	})
	mux.HandleFunc("GET /api/ui", func(w http.ResponseWriter, r *http.Request) { reply(w, map[string]string{"mode": c.UI()}) })
	mux.HandleFunc("POST /api/ui", func(w http.ResponseWriter, r *http.Request) {
		var m struct{ Mode string }
		json.NewDecoder(r.Body).Decode(&m)
		if err := c.SetUI(m.Mode); err != nil {
			fail(w, err, 400)
			return
		}
		reply(w, m)
		if m.Mode == "terminal" {
			go func() { time.Sleep(200 * time.Millisecond); quit() }() // after the reply is out
		}
	})
	mux.HandleFunc("POST /api/tasks/{id}/stop", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]bool{"stopped": c.Stop(r.PathValue("id"))})
	})
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
			fail(w, fmt.Errorf("no streaming"), 500)
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

func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, err error, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

// orEmpty makes a nil list [] in JSON, which the page can loop over.
func orEmpty[T any](l []T) []T {
	if l == nil {
		return []T{}
	}
	return l
}
