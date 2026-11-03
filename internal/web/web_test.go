package web

import (
	"bufio"
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestServe checks the token and Host guard, and that the event stream delivers.
func TestServe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	u, err := Serve(ctx)
	if err != nil {
		t.Fatal(err)
	}
	pu, _ := url.Parse(u)
	status := func(path, host string, cookie bool) int {
		req, _ := http.NewRequest("GET", "http://"+pu.Host+path, nil)
		if host != "" {
			req.Host = host
		}
		if cookie {
			req.AddCookie(&http.Cookie{Name: "crew", Value: pu.Query().Get("token")})
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if c := status("/", "", false); c != 403 {
		t.Errorf("no token: %d, want 403", c)
	}
	if c := status("/?token="+pu.Query().Get("token"), "", false); c != 200 {
		t.Errorf("token in the URL: %d, want 200", c)
	}
	if c := status("/", "evil.example", true); c != 403 {
		t.Errorf("another Host: %d, want 403", c)
	}
	req, _ := http.NewRequest("GET", "http://"+pu.Host+"/api/events", nil)
	req.AddCookie(&http.Cookie{Name: "crew", Value: pu.Query().Get("token")})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	line, _ := bufio.NewReader(res.Body).ReadString('\n')
	if !strings.HasPrefix(line, "data: ") {
		t.Errorf("first event line %q, want data: …", line)
	}
}
