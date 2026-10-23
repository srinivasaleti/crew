package gh

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadDescription(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.pr.md")
	if _, _, ok := ReadDescription(p); ok {
		t.Fatal("no file should mean no description")
	}
	os.WriteFile(p, []byte("# Fix the login loop\n\nIt looped.\n\n## How it was tested\n- go test\n"), 0o644)
	title, body, ok := ReadDescription(p)
	if !ok || title != "Fix the login loop" || body != "It looped.\n\n## How it was tested\n- go test" {
		t.Fatalf("got %q / %q / %v", title, body, ok)
	}
}
