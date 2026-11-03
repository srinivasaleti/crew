// Package web serves crew's web UI (docs/web-ui.md) on this Mac only: a loopback port with a token,
// so nothing else on the machine can drive crew. For now it's the spike: a page and a live event
// stream, to prove the app's window can load both (App Transport Security, a web view that buffers).
package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"time"
)

//go:embed static
var static embed.FS

// Serve listens on a random loopback port until ctx ends, and returns the URL to open (it carries
// the token the page needs; the first visit turns it into a cookie).
func Serve(ctx context.Context) (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	b := make([]byte, 16)
	rand.Read(b)
	tok := hex.EncodeToString(b)
	files, _ := fs.Sub(static, "static")
	mux := http.NewServeMux()
	mux.HandleFunc("/api/events", events)
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

// events streams a tick a second, as server-sent events, until the page goes away.
// ponytail: spike only; the real stream carries tasks, messages and turns.
func events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "no streaming", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	fl.Flush()
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case now := <-t.C:
			fmt.Fprintf(w, "data: %s\n\n", now.Format(time.TimeOnly))
			fl.Flush()
		}
	}
}
