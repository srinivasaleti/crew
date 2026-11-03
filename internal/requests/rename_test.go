package requests

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// Folders and applications rename and delete with their requests and environments; names are checked.
// (No keys here: those live in the Keychain, which a test doesn't touch.)
func TestRename(t *testing.T) {
	dir := t.TempDir()
	AddApp(dir, "acme")
	AddApp(dir, "other")
	AddFolder(dir, "acme", "orgs")
	SaveEnv(dir, Env{App: "acme", Name: "dev", Vars: map[string]string{"baseUrl": "https://dev.acme.io"}})
	r, err := Save(dir, Request{App: "acme", Folder: "orgs", Name: "List", Method: "GET", URL: "{{baseUrl}}/orgs"})
	if err != nil {
		t.Fatal(err)
	}
	if err := RenameFolder(dir, "acme", "orgs", "teams"); err != nil {
		t.Fatal(err)
	}
	all, _ := Load(dir)
	if len(all) != 1 || all[0].Folder != "teams" || all[0].Name != "List" || r.Folder != "orgs" {
		t.Errorf("after renaming the folder: %+v", all)
	}
	for _, to := range []string{"env", "two words", ""} {
		if RenameFolder(dir, "acme", "teams", to) == nil {
			t.Errorf("renaming a folder to %q was allowed", to)
		}
	}
	if RenameApp(dir, "acme", "other") == nil {
		t.Error("renaming onto another application was allowed")
	}
	if err := RenameApp(dir, "acme", "acme2"); err != nil {
		t.Fatal(err)
	}
	if apps := Apps(dir); !slices.Contains(apps, "acme2") || slices.Contains(apps, "acme") {
		t.Errorf("apps after renaming: %v", apps)
	}
	if _, err := LoadEnv(dir, "acme2", "dev"); err != nil {
		t.Errorf("the environment didn't move: %v", err)
	}
	if err := DeleteApp(dir, "acme2"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "acme2")); !os.IsNotExist(err) {
		t.Errorf("acme2 is still there: %v", err)
	}
}
