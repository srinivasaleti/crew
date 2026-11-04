package notes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDir checks notes stay in their folder, get free file names, are found by their text (not
// their tags), come back from the trash, and a Markdown note turns into HTML when it opens.
func TestDir(t *testing.T) {
	d := Dir(t.TempDir())
	for _, bad := range []string{"../x.html", "/etc/passwd", "a.txt", ".trash/a.html", ""} {
		if err := d.Write(bad, "x"); err == nil {
			t.Errorf("%q was written", bad)
		}
	}
	if _, _, err := d.Read("../../etc/hosts.html"); err == nil {
		t.Error("read outside the folder")
	}
	now := time.Date(2026, 11, 4, 9, 30, 0, 0, time.UTC)
	a, _ := d.New("work", now)
	b, _ := d.New("work", now)
	if a != "work/note-2026-11-04-093000.html" || b != "work/note-2026-11-04-093000-2.html" {
		t.Errorf("paths %q %q", a, b)
	}
	d.Write(b, `<h1>Retro &amp; plans</h1><div>ship the <b>parser</b> on friday</div><ul class="checklist"><li>tests</li></ul>`)
	if l := d.List("the parser"); len(l) != 1 || l[0].Title != "Retro & plans" || l[0].Folder != "work" || !strings.Contains(l[0].Snippet, "parser") {
		t.Errorf("search: %+v", l)
	}
	if l := d.List("checklist"); len(l) != 0 {
		t.Error("search matched a tag")
	}
	if err := d.Trash(a); err != nil || len(d.List("")) != 1 {
		t.Errorf("trash: %v, %d notes", err, len(d.List("")))
	}
	if err := d.Restore(a); err != nil || len(d.List("")) != 2 {
		t.Errorf("restore: %v", err)
	}
	os.WriteFile(filepath.Join(string(d), "old.md"), []byte("# Old <one>\n\n- [ ] milk\n- [x] eggs\ntext\n"), 0o644)
	h, p, err := d.Read("old.md")
	want := `<h1>Old &lt;one&gt;</h1><div><br></div><ul class="checklist"><li>milk</li><li class="done">eggs</li></ul><div>text</div>`
	if err != nil || p != "old.html" || h != want {
		t.Errorf("markdown: %v %q\n%s", err, p, h)
	}
	if _, err := os.Stat(filepath.Join(string(d), "old.md")); err == nil {
		t.Error("the .md is still there")
	}
}
