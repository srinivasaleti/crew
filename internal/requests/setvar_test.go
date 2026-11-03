package requests

import "testing"

// SetVar changes one plain value in place, and keeps keys out of the file and baseUrl an address.
func TestSetVar(t *testing.T) {
	dir := t.TempDir()
	if err := AddApp(dir, "acme"); err != nil {
		t.Fatal(err)
	}
	e := Env{App: "acme", Name: "dev", Vars: map[string]string{"baseUrl": "https://dev.acme.io"}, Keys: []string{"token"}}
	if err := SaveEnv(dir, e); err != nil {
		t.Fatal(err)
	}
	if err := SetVar(dir, "acme", "dev", "tenant", " blue ", false); err != nil {
		t.Fatal(err)
	}
	if err := SetVar(dir, "acme", "dev", "baseUrl", "https://dev2.acme.io/", false); err != nil {
		t.Fatal(err)
	}
	got, _ := LoadEnv(dir, "acme", "dev")
	if got.Vars["tenant"] != "blue" || got.BaseURL() != "https://dev2.acme.io" || len(got.Keys) != 1 {
		t.Errorf("after setting: %+v", got)
	}
	for _, bad := range []struct {
		name, value string
		remove      bool
	}{
		{"token", "x", false},        // a key
		{"baseUrl", "", true},        // removing the base
		{"baseUrl", "dev.io", false}, // not an address
		{"two words", "x", false},    // not a name
	} {
		if SetVar(dir, "acme", "dev", bad.name, bad.value, bad.remove) == nil {
			t.Errorf("SetVar(%q, %q, remove=%v) was allowed", bad.name, bad.value, bad.remove)
		}
	}
	if err := SetVar(dir, "acme", "dev", "tenant", "", true); err != nil {
		t.Fatal(err)
	}
	if got, _ := LoadEnv(dir, "acme", "dev"); got.Vars["tenant"] != "" {
		t.Errorf("tenant wasn't removed: %+v", got.Vars)
	}
}
