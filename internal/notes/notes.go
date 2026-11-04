// Package notes is NOTES: notes like Apple Notes' (a title, headings, checklists, bold…), each an HTML
// file in ~/.crew/notes (folders allowed) that opens in any browser, starred ones on top, and a Claude
// chat that may only read and write in that folder. A Markdown (.md) note from before is turned into
// HTML the first time it opens.
package notes

import (
	"errors"
	"fmt"
	"html"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ChatKind marks the NOTES chat in its task file (kind:).
const ChatKind = "notes"

// Note is one note as a list shows it.
type Note struct {
	Path    string    `json:"path"` // relative to the notes folder, "work/standup.html"
	Title   string    `json:"title"`
	Folder  string    `json:"folder"` // "" at the top
	Snippet string    `json:"snippet"`
	Mod     time.Time `json:"mod"`
}

// Dir is the notes folder.
type Dir string

// isNote reports whether a file name is a note's: .html, or .md from before.
func isNote(name string) bool {
	return strings.HasSuffix(name, ".html") || strings.HasSuffix(name, ".md")
}

// abs is p inside the folder, refusing anything that leaves it, is hidden (.trash) or isn't a note.
func (d Dir) abs(p string) (string, error) {
	p = filepath.ToSlash(p)
	ok := isNote(p) && !filepath.IsAbs(p)
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
		if e.IsDir() || !isNote(e.Name()) {
			return nil
		}
		b, _ := os.ReadFile(path)
		text := string(b)
		if strings.HasSuffix(path, ".html") {
			text = Plain(text)
		}
		rel, _ := filepath.Rel(string(d), path)
		n := Note{Path: filepath.ToSlash(rel), Title: Title(text), Snippet: snippet(text, q)}
		if dir := filepath.Dir(n.Path); dir != "." {
			n.Folder = dir
		}
		if q != "" && !strings.Contains(strings.ToLower(text), q) {
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

// Title is a note's first line with words, as Apple Notes names a note.
func Title(text string) string {
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSpace(strings.TrimLeft(l, "# ")); l != "" {
			return trim(l, 80)
		}
	}
	return "New note"
}

var (
	blockEnd = regexp.MustCompile(`(?i)<br\s*/?>|</(div|p|h[1-6]|li|pre|blockquote|tr)>`)
	tag      = regexp.MustCompile(`<[^>]*>`)
)

// Plain is a note's HTML as text, a line per block, for its title, snippet and search.
func Plain(h string) string {
	return html.UnescapeString(tag.ReplaceAllString(blockEnd.ReplaceAllString(h, "\n"), ""))
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

// Read is a note's HTML, and its path: a Markdown note is turned into HTML first, as a .html beside it.
func (d Dir) Read(p string) (string, string, error) {
	a, err := d.abs(p)
	if err != nil {
		return "", p, err
	}
	b, err := os.ReadFile(a)
	if err != nil || !strings.HasSuffix(p, ".md") {
		return string(b), p, err
	}
	h := FromMarkdown(string(b))
	np := d.free(strings.TrimSuffix(p, ".md"))
	if err := d.Write(np, h); err != nil {
		return h, p, err
	}
	return h, np, os.Remove(a)
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

// New makes an empty note in folder and returns its path. Its title is its first line, so the file
// is named by when it was made.
func (d Dir) New(folder string, now time.Time) (string, error) {
	p := d.free(filepath.ToSlash(filepath.Join(folder, "note-"+now.Format("2006-01-02-150405"))))
	return p, d.Write(p, "")
}

// free is base+".html", or base-2.html… when that's taken.
func (d Dir) free(base string) string {
	p := base + ".html"
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(string(d), p)); errors.Is(err, fs.ErrNotExist) {
			return p
		}
		p = fmt.Sprintf("%s-%d.html", base, i)
	}
}

var mdList = regexp.MustCompile(`^\s*[-*] (\[( |x|X)\] )?(.*)$`)

// FromMarkdown turns a Markdown note from before into the editor's HTML: headings, bullet lists and
// checklists; every other line a line of text.
// ponytail: line by line; inline marks (**bold**, links) stay as typed.
func FromMarkdown(md string) string {
	var b strings.Builder
	list := "" // the list open: "", "ul" or "checklist"
	for _, l := range strings.Split(strings.TrimRight(md, "\n"), "\n") {
		m := mdList.FindStringSubmatch(l)
		kind := ""
		if m != nil {
			kind = "ul"
			if m[1] != "" {
				kind = "checklist"
			}
		}
		if kind != list {
			if list != "" {
				b.WriteString("</ul>")
			}
			if kind == "checklist" {
				b.WriteString(`<ul class="checklist">`)
			} else if kind != "" {
				b.WriteString("<ul>")
			}
			list = kind
		}
		switch {
		case m != nil && m[2] != "" && m[2] != " ":
			b.WriteString(`<li class="done">` + html.EscapeString(m[3]) + "</li>")
		case m != nil:
			b.WriteString("<li>" + html.EscapeString(m[3]) + "</li>")
		case strings.HasPrefix(l, "### "):
			b.WriteString("<h3>" + html.EscapeString(l[4:]) + "</h3>")
		case strings.HasPrefix(l, "## "):
			b.WriteString("<h2>" + html.EscapeString(l[3:]) + "</h2>")
		case strings.HasPrefix(l, "# "):
			b.WriteString("<h1>" + html.EscapeString(l[2:]) + "</h1>")
		case strings.TrimSpace(l) == "":
			b.WriteString("<div><br></div>")
		default:
			b.WriteString("<div>" + html.EscapeString(l) + "</div>")
		}
	}
	if list != "" {
		b.WriteString("</ul>")
	}
	return b.String()
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
	return "You're in crew's NOTES chat. My notes are the .html files in this folder (sub-folders are groups), written in an editor like Apple Notes. " +
		"Find, read, summarise, tidy, and write notes here when I ask. A note is an HTML fragment, no <html> or <head>: its first line is its title (<h1>), " +
		"then <h2>/<h3> headings, <div> lines, <b> <i> <u> <s>, <pre> for code, <ul>/<ol> lists, and checklists as <ul class=\"checklist\"><li>to do</li><li class=\"done\">done</li></ul>. " +
		"No scripts, styles or images. A new note's file name is lower-case words with dashes, .html. " +
		"Don't delete a note or overwrite one I didn't name: say what you'd change and ask. " +
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
