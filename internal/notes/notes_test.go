package notes

import (
	"strings"
	"testing"
)

// TestDir checks notes stay in their folder, get free file names, are found by search, and come
// back from the trash.
func TestDir(t *testing.T) {
	d := Dir(t.TempDir())
	for _, bad := range []string{"../x.md", "/etc/passwd", "a.txt", ".trash/a.md", ""} {
		if err := d.Write(bad, "x"); err == nil {
			t.Errorf("%q was written", bad)
		}
	}
	if _, err := d.Read("../../etc/hosts.md"); err == nil {
		t.Error("read outside the folder")
	}
	a, _ := d.New("work", "Stand-up notes!")
	b, _ := d.New("work", "Stand-up notes!")
	if a != "work/stand-up-notes.md" || b != "work/stand-up-notes-2.md" {
		t.Errorf("paths %q %q", a, b)
	}
	d.Write(b, "# Retro\n\nship the parser on friday\n")
	if l := d.List("parser"); len(l) != 1 || l[0].Title != "Retro" || l[0].Folder != "work" || !strings.Contains(l[0].Snippet, "parser") {
		t.Errorf("search: %+v", l)
	}
	if err := d.Trash(a); err != nil || len(d.List("")) != 1 {
		t.Errorf("trash: %v, %d notes", err, len(d.List("")))
	}
	if err := d.Restore(a); err != nil || len(d.List("")) != 2 {
		t.Errorf("restore: %v", err)
	}
}
