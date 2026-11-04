package core

import (
	"context"
	"strings"
	"testing"

	"crew/internal/tasks"
)

// TestSaveImage takes images only, keeps them in the chat's folder, and finds them only by name there.
func TestSaveImage(t *testing.T) {
	dir := t.TempDir()
	c := New(context.Background(), Config{Home: dir, Tasks: tasks.Store{Dir: dir + "/tasks"}})
	if err := c.cfg.Tasks.Save(tasks.Task{ID: "t1", Title: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SaveImage("t1", []byte("not an image")); err == nil {
		t.Error("took text as an image")
	}
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	p, err := c.SaveImage("t1", png)
	if err != nil || !strings.HasPrefix(p, dir+"/images/t1/") {
		t.Fatalf("SaveImage = %q, %v", p, err)
	}
	name := p[strings.LastIndex(p, "/")+1:]
	if c.ImagePath("t1", name) != p || c.ImagePath("t1", "../../tasks/t1.md") != "" {
		t.Error("ImagePath finds the wrong file")
	}
	if got := WithImages("look", []string{p}); !strings.HasSuffix(got, "\n[image: "+p+"]") {
		t.Errorf("WithImages = %q", got)
	}
}
