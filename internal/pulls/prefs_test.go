package pulls

import (
	"testing"

	"crew/internal/gh"
)

// Hiding a repo or an org takes its PRs out, except review requests; stars and hides survive a save.
func TestPrefs(t *testing.T) {
	home := t.TempDir()
	p := LoadPrefs(home)
	p.Hide("acme/legacy", true)
	p.Hide("oss-org", true)
	p.Star("acme/api", true)
	p.Star("acme/api", true) // twice is once
	if err := p.Save(home); err != nil {
		t.Fatal(err)
	}
	p = LoadPrefs(home)
	if len(p.Starred) != 1 || !p.IsStarred("acme/api") {
		t.Errorf("starred = %v", p.Starred)
	}
	items := []gh.Item{
		{Repo: "acme/legacy", Number: 1},               // hidden repo
		{Repo: "oss-org/lib", Number: 2},               // hidden org
		{Repo: "oss-org/lib", Number: 3, Review: true}, // but someone waits on you
		{Repo: "acme/api", Number: 4},
		{Repo: "acme/legacy-2", Number: 5}, // a name that only starts like a hidden one
	}
	shown, hidden := p.Visible(items)
	if hidden != 2 || len(shown) != 3 || shown[0].Number != 3 || shown[2].Number != 5 {
		t.Errorf("shown %v, hidden %d", shown, hidden)
	}
	p.Hide("oss-org", false)
	if _, hidden = p.Visible(items); hidden != 1 {
		t.Errorf("after showing the org again, hidden = %d", hidden)
	}
}
