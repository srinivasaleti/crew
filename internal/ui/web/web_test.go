package web

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"testing"

	"crew/internal/core"
	"crew/internal/repo"
	"crew/internal/tasks"
	"crew/internal/ui/terminal/widgets/chat"
)

// TestServe checks the token and Host guard, and the API: a task made over HTTP shows in the list
// and reaches the event stream.
func TestServe(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", dir+"/proj").CombinedOutput(); err != nil {
		t.Skip("no git:", err, string(out))
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := core.New(ctx, core.Config{Tasks: tasks.Store{Dir: dir + "/tasks"}, Chats: chat.Log{Dir: dir + "/chats"},
		Repos: repo.Store{Path: dir + "/repos"}, Plans: dir + "/plans", Splits: dir + "/splits"})
	u, err := Serve(ctx, c, func() {})
	if err != nil {
		t.Fatal(err)
	}
	pu, _ := url.Parse(u)
	tok := pu.Query().Get("token")
	do := func(method, path, host, body string, cookie bool) *http.Response {
		req, _ := http.NewRequest(method, "http://"+pu.Host+path, strings.NewReader(body))
		if host != "" {
			req.Host = host
		}
		if cookie {
			req.AddCookie(&http.Cookie{Name: "crew", Value: tok})
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	for _, tc := range []struct {
		path, host string
		cookie     bool
		want       int
	}{
		{"/", "", false, 403},
		{"/?token=" + tok, "", false, 200},
		{"/", "evil.example", true, 403},
		{"/api/tasks", "", false, 403},
	} {
		res := do("GET", tc.path, tc.host, "", tc.cookie)
		res.Body.Close()
		if res.StatusCode != tc.want {
			t.Errorf("GET %s (host %q, cookie %v): %d, want %d", tc.path, tc.host, tc.cookie, res.StatusCode, tc.want)
		}
	}
	stream := do("GET", "/api/events", "", "", true)
	defer stream.Body.Close()
	res := do("POST", "/api/tasks", "", `{"Title":"Fix login","Repo":"`+dir+`/proj"}`, true)
	if res.StatusCode != 200 {
		t.Fatalf("create: %d", res.StatusCode)
	}
	res.Body.Close()
	line, _ := bufio.NewReader(stream.Body).ReadString('\n')
	var ev core.Event
	if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev) != nil || ev.Kind != "task" || ev.ID != "fix-login" {
		t.Errorf("first event %q, want the task fix-login", line)
	}
	var list []core.TaskView
	res = do("GET", "/api/tasks", "", "", true)
	json.NewDecoder(res.Body).Decode(&list)
	res.Body.Close()
	if len(list) != 1 || list[0].Title != "Fix login" || list[0].Status != tasks.Todo {
		t.Errorf("list %+v, want the new task as todo", list)
	}
	res = do("POST", "/api/tasks/fix-login/messages", "", `{"Text":""}`, true)
	res.Body.Close()
	if res.StatusCode != 400 {
		t.Errorf("empty message: %d, want 400", res.StatusCode)
	}
}
