// Package notes is NOTES: Markdown files in ~/.crew/notes (folders allowed), readable and editable
// outside crew, starred ones on top, and a Claude chat that may only read and write in that folder.
package notes

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ChatKind marks the NOTES chat in its task file (kind:).
const ChatKind = "notes"

// Note is one note as a list shows it.
type Note struct {
	Path    string    `json:"path"` // relative to the notes folder, "work/standup.md"
	Title   string    `json:"title"`
	Folder  string    `json:"folder"` // "" at the top
	Snippet string    `json:"snippet"`
	Mod     time.Time `json:"mod"`
}

// Dir is the notes folder.
type Dir string

// abs is p inside the folder, refusing anything that leaves it, is hidden (.trash) or isn't Markdown.
func (d Dir) abs(p string) (string, error) {
	p = filepath.ToSlash(p)
	ok := strings.HasSuffix(p, ".md") && !filepath.IsAbs(p)
	for _, part := range strings.Split(p, "/") {
		ok = ok && part != "" && !strings.HasPrefix(part, ".")
	}
	if !ok {
		return "", errors.New("not a note: " + p)
	}
	return filepath.Join(string(d), p), nil
}

// List is every note, the latest changed first. With q, only the notes whose title or text has it.
func (d Dir) List(q string) []Note {
	q = strings.ToLower(strings.TrimSpace(q))
	var out []Note
	filepath.WalkDir(string(d), func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if strings.HasPrefix(e.Name(), ".") && path != string(d) {
			if e.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			return nil
		}
		b, _ := os.ReadFile(path)
		rel, _ := filepath.Rel(string(d), path)
		n := Note{Path: filepath.ToSlash(rel), Title: Title(string(b), e.Name()), Snippet: snippet(string(b), q)}
		if dir := filepath.Dir(n.Path); dir != "." {
			n.Folder = dir
		}
		if q != "" && !strings.Contains(strings.ToLower(n.Title+"\n"+string(b)), q) {
			return nil
		}
		if fi, err := e.Info(); err == nil {
			n.Mod = fi.ModTime()
		}
		out = append(out, n)
		return nil
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].Mod.After(out[j].Mod) })
	return out
}

// Folders are the folders in the notes folder, empty ones too, sorted.
func (d Dir) Folders() []string {
	var out []string
	filepath.WalkDir(string(d), func(path string, e fs.DirEntry, err error) error {
		if err != nil || !e.IsDir() || path == string(d) {
			return nil
		}
		if strings.HasPrefix(e.Name(), ".") {
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(string(d), path)
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	return out
}

// Title is a note's first heading, else its first line, else its file name.
func Title(text, name string) string {
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSpace(strings.TrimLeft(l, "# ")); l != "" {
			return trim(l, 80)
		}
	}
	return strings.TrimSuffix(name, ".md")
}

// snippet is the line that has q (the first line after the title when q is empty), short.
func snippet(text, q string) string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		l = strings.TrimSpace(l)
		if (q != "" && strings.Contains(strings.ToLower(l), q)) || (q == "" && i > 0 && l != "" && !strings.HasPrefix(l, "#")) {
			return trim(strings.TrimLeft(l, "-*> "), 120)
		}
	}
	return ""
}

func trim(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// Read is a note's text.
func (d Dir) Read(p string) (string, error) {
	a, err := d.abs(p)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(a)
	return string(b), err
}

// Write saves a note's text, making its folder.
func (d Dir) Write(p, text string) error {
	a, err := d.abs(p)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a), 0o755); err != nil {
		return err
	}
	return os.WriteFile(a, []byte(text), 0o644)
}

// New makes a note in folder titled title, with a file name from the title that isn't taken, and
// returns its path.
func (d Dir) New(folder, title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Untitled"
	}
	name := slug(title)
	p := filepath.ToSlash(filepath.Join(folder, name+".md"))
	for i := 2; ; i++ {
		a, err := d.abs(p)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(a); errors.Is(err, fs.ErrNotExist) {
			break
		}
		p = filepath.ToSlash(filepath.Join(folder, fmt.Sprintf("%s-%d.md", name, i)))
	}
	return p, d.Write(p, "# "+title+"\n\n")
}

// slug is a file name for a title: lower case, letters and digits, dashes between words.
func slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r > 127 {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
		} else {
			dash = true
		}
	}
	if b.Len() == 0 {
		return "note"
	}
	return trim(b.String(), 60)
}

// Move moves a note to another path (a rename, or into another folder); it won't replace one.
func (d Dir) Move(from, to string) error {
	a, err := d.abs(from)
	if err != nil {
		return err
	}
	b, err := d.abs(to)
	if err != nil {
		return err
	}
	return move(a, b)
}

// Trash moves a note to .trash in the notes folder, where Restore brings it back.
// ponytail: the trash is never emptied; it's plain files you can delete in Finder.
func (d Dir) Trash(p string) error {
	a, err := d.abs(p)
	if err != nil {
		return err
	}
	return move(a, filepath.Join(string(d), ".trash", p))
}

// Restore brings a trashed note back to p.
func (d Dir) Restore(p string) error {
	b, err := d.abs(p)
	if err != nil {
		return err
	}
	return move(filepath.Join(string(d), ".trash", p), b)
}

// move renames a file to b, making b's folder; it won't replace a file.
func move(a, b string) error {
	if _, err := os.Stat(b); err == nil {
		return errors.New("there's already a note at " + filepath.Base(b))
	}
	if err := os.MkdirAll(filepath.Dir(b), 0o755); err != nil {
		return err
	}
	return os.Rename(a, b)
}

// ChatBrief is what the NOTES chat's Claude is told first.
func ChatBrief() string {
	return "You're in crew's NOTES chat. My notes are the Markdown files in this folder (sub-folders are groups). " +
		"Find, read, summarise, tidy, and write notes here when I ask; a new note is a .md file whose first line is a # heading, " +
		"its file name lower-case words with dashes. Don't delete a note or overwrite one I didn't name: say what you'd change and ask. " +
		"After changing anything, list the notes you changed. You can't run commands or reach anything outside this folder."
}

// ChatArgs lock the NOTES chat's Claude down to its folder: files only (no Bash, no web, no MCP
// servers or user settings), reads and edits only under the folder it runs in (./**), anything else
// refused, not asked.
func ChatArgs() []string {
	return []string{"--restricted", "--strict-mcp-config", "--permission-mode", "dontAsk", "--permission-prompts", "none",
		"--tools", "Read,Write,Edit,Glob,Grep",
		"--allowedTools", "Read(./**)", "Edit(./**)"} // Read rules cover Glob and Grep, Edit rules Write

}
