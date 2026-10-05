package task

import (
	"testing"
	"time"
)

func TestRoundTrip(t *testing.T) {
	in := Task{ID: "fix-login", Title: "Fix: login loop", Repo: "accounts", Status: Todo,
		Created: time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC), Body: "Line one\n\nLine two"}
	out, err := Parse(in.ID, in.String())
	if err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", out, in)
	}
	if _, err := Parse("x", "no front matter"); err == nil {
		t.Fatal("expected error for missing front matter")
	}
	if got, err := Parse("x", "---\ntitle: bare\n---"); err != nil || got.Title != "bare" {
		t.Fatalf("file without trailing newline: %+v %v", got, err)
	}
}

func TestSlugify(t *testing.T) {
	for in, want := range map[string]string{
		"Fix login redirect loop": "fix-login-redirect-loop",
		"  Add /roles API!! ":     "add-roles-api",
		"日本語":                     "task",
	} {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStoreNewIDAvoidsClash(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	if err := s.Save(Task{ID: s.NewID("Same title")}); err != nil {
		t.Fatal(err)
	}
	if got := s.NewID("Same title"); got != "same-title-2" {
		t.Fatalf("NewID = %q, want same-title-2", got)
	}
}
