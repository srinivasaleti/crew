package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSkills checks every skill in plugin/skills is one Claude can find as crew:<folder>: a SKILL.md
// whose front matter names the folder and says when to use it.
func TestSkills(t *testing.T) {
	dirs, _ := filepath.Glob("plugin/skills/*")
	if len(dirs) == 0 {
		t.Fatal("no skills found")
	}
	for _, dir := range dirs {
		b, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
		if err != nil {
			t.Errorf("%s: %v", dir, err)
			continue
		}
		head, _, _ := strings.Cut(strings.TrimPrefix(string(b), "---\n"), "\n---\n")
		if !strings.Contains(head, "\nname: "+filepath.Base(dir)+"\n") && !strings.HasPrefix(head, "name: "+filepath.Base(dir)+"\n") {
			t.Errorf("%s: its front matter should say name: %s", dir, filepath.Base(dir))
		}
		if !strings.Contains(head, "description: ") {
			t.Errorf("%s: its front matter needs a description (when Claude should use it)", dir)
		}
	}
}
