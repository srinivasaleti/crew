package requests

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Env is one environment of an application. A file <app>/env/<name>.env, written by crew:
//
//	baseUrl = https://dev.accounts.example.com
//	auth = bearer token             (or: auth = header X-Api-Key apiKey)
//	key token                       (the value is in the Keychain, never here)
//	hosts = auth.dev.example.com    (more hosts its keys may go to; baseUrl's host always may)
//	tenant = acme                   (any other plain value, used as {{tenant}})
type Env struct {
	App, Name string
	Vars      map[string]string // plain values, baseUrl included; anyone may see them, the AI too
	Keys      []string          // the names of its keys
	Auth      Auth
	Hosts     []string // extra hosts its keys may be sent to
}

// Auth is how an environment signs requests: none, a bearer token, or a key in a header.
type Auth struct {
	Kind   string // "", "bearer" or "header"
	Header string // for "header", e.g. X-Api-Key
	Key    string // the key it uses
}

// AuthKinds are the auth methods, in the order crew offers them.
var AuthKinds = []string{"", "bearer", "header"}

// AuthHeader is the header the auth adds and its value (with the key as a variable), or "".
func (e Env) AuthHeader() (string, string) {
	switch {
	case e.Auth.Key == "":
		return "", ""
	case e.Auth.Kind == "bearer":
		return "Authorization", "Bearer {{" + e.Auth.Key + "}}"
	case e.Auth.Kind == "header" && e.Auth.Header != "":
		return e.Auth.Header, "{{" + e.Auth.Key + "}}"
	}
	return "", ""
}

// AuthLabel describes the auth in a few words.
func (e Env) AuthLabel() string {
	switch h, _ := e.AuthHeader(); {
	case h == "":
		return "no auth"
	case e.Auth.Kind == "bearer":
		return "Bearer " + e.Auth.Key
	default:
		return h + ": " + e.Auth.Key
	}
}

// BaseURL is the environment's base URL.
func (e Env) BaseURL() string { return e.Vars["baseUrl"] }

// AllHosts are the hosts its keys may go to: the base URL's, then the extra ones.
func (e Env) AllHosts() []string {
	var out []string
	if u, err := url.Parse(e.BaseURL()); err == nil && u.Hostname() != "" {
		out = append(out, u.Hostname())
	}
	for _, h := range e.Hosts {
		if !slices.Contains(out, h) {
			out = append(out, h)
		}
	}
	return out
}

// Resolve fills in the plain values (never keys), e.g. for showing the URL a request goes to.
func (e Env) Resolve(s string) string {
	return varRef.ReplaceAllStringFunc(s, func(ref string) string {
		if v, ok := e.Vars[varRef.FindStringSubmatch(ref)[1]]; ok {
			return v
		}
		return ref
	})
}

// MoveLoose puts what the first version of REQUESTS wrote at the top of dir (env/*.env, *.http,
// .current-env) into an application named "default", so it shows as one and nothing is lost.
func MoveLoose(dir string) error {
	envs, _ := filepath.Glob(filepath.Join(dir, "env", "*.env"))
	reqs, _ := filepath.Glob(filepath.Join(dir, "*.http"))
	if len(envs) == 0 && len(reqs) == 0 {
		return nil
	}
	app := filepath.Join(dir, "default")
	if err := os.MkdirAll(app, 0o755); err != nil {
		return err
	}
	if len(envs) > 0 {
		if err := os.Rename(filepath.Join(dir, "env"), filepath.Join(app, "env")); err != nil {
			return err
		}
	}
	for _, p := range append(reqs, filepath.Join(dir, ".current-env")) {
		if _, err := os.Stat(p); err == nil {
			if err := os.Rename(p, filepath.Join(app, filepath.Base(p))); err != nil {
				return err
			}
		}
	}
	return nil
}

// DefaultFolder is how the requests in no folder are shown.
const DefaultFolder = "default"

// Folders lists an application's folders, A to Z (the default one isn't a folder on disk).
func Folders(dir, app string) []string {
	entries, _ := os.ReadDir(filepath.Join(dir, app))
	var out []string
	for _, e := range entries {
		if e.IsDir() && e.Name() != "env" && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, e.Name())
		}
	}
	slices.SortFunc(out, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
	return out
}

// AddFolder makes a folder in an application.
func AddFolder(dir, app, name string) error {
	if !ValidName(name) || name == "env" || name == DefaultFolder {
		return fmt.Errorf("A folder name is one word, not env or %s", DefaultFolder)
	}
	p := filepath.Join(dir, app, name)
	if _, err := os.Stat(p); err == nil {
		return fmt.Errorf("%s already has %s", app, name)
	}
	return os.MkdirAll(p, 0o755)
}

// DeleteFolder deletes a folder and the requests in it.
func DeleteFolder(dir, app, name string) error {
	if name == "" || name == "env" || strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("can't delete %q", name)
	}
	return os.RemoveAll(filepath.Join(dir, app, name))
}

// Folded is the folders you folded in an application ("" is the default one).
func Folded(dir, app string) map[string]bool {
	data, _ := os.ReadFile(filepath.Join(dir, app, ".folded"))
	out := map[string]bool{}
	for _, l := range strings.Split(string(data), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out[strings.TrimPrefix(l, "/")] = true // "/" stands for the default folder
		}
	}
	return out
}

// SetFolded remembers the folders you folded.
func SetFolded(dir, app string, folded map[string]bool) error {
	var b strings.Builder
	for f, on := range folded {
		if on {
			b.WriteString("/" + f + "\n")
		}
	}
	return os.WriteFile(filepath.Join(dir, app, ".folded"), []byte(b.String()), 0o644)
}

// Apps lists the applications: the folders under dir.
func Apps(dir string) []string {
	entries, _ := os.ReadDir(dir)
	var out []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, e.Name())
		}
	}
	return out
}

func envDir(dir, app string) string { return filepath.Join(dir, app, "env") }

// Envs lists an application's environments.
func Envs(dir, app string) []string {
	files, _ := filepath.Glob(filepath.Join(envDir(dir, app), "*.env"))
	var out []string
	for _, f := range files {
		out = append(out, strings.TrimSuffix(filepath.Base(f), ".env"))
	}
	return sortEnvs(out)
}

// sortEnvs puts the usual ones in their usual order (local, dev, staging, prod), then the rest.
func sortEnvs(envs []string) []string {
	rank := func(s string) int {
		if i := slices.Index([]string{"local", "dev", "development", "test", "qa", "staging", "stage", "uat", "prod", "production"}, s); i >= 0 {
			return i
		}
		return 99
	}
	slices.SortFunc(envs, func(a, b string) int {
		if ra, rb := rank(a), rank(b); ra != rb {
			return ra - rb
		}
		return strings.Compare(a, b)
	})
	return envs
}

// LoadEnv reads an environment.
func LoadEnv(dir, app, name string) (Env, error) {
	e := Env{App: app, Name: name, Vars: map[string]string{}}
	data, err := os.ReadFile(filepath.Join(envDir(dir, app), name+".env"))
	if err != nil {
		return e, fmt.Errorf("%s has no environment %q", app, name)
	}
	for _, l := range strings.Split(string(data), "\n") {
		l = strings.TrimSpace(l)
		switch {
		case l == "" || strings.HasPrefix(l, "#"):
		case strings.HasPrefix(l, "key "):
			e.Keys = append(e.Keys, strings.TrimSpace(strings.TrimPrefix(l, "key ")))
		default:
			k, v, _ := strings.Cut(l, "=")
			k, v = strings.TrimSpace(k), strings.TrimSpace(v)
			switch f := strings.Fields(v); {
			case k == "hosts":
				for _, h := range strings.Split(v, ",") {
					if h = strings.TrimSpace(h); h != "" {
						e.Hosts = append(e.Hosts, h)
					}
				}
			case k == "auth" && len(f) == 2 && f[0] == "bearer":
				e.Auth = Auth{Kind: "bearer", Key: f[1]}
			case k == "auth" && len(f) == 3 && f[0] == "header":
				e.Auth = Auth{Kind: "header", Header: f[1], Key: f[2]}
			case k != "" && k != "auth":
				e.Vars[k] = v
			}
		}
	}
	return e, nil
}

// SaveEnv writes an environment (its keys' values go to the Keychain separately, with SetKey).
func SaveEnv(dir string, e Env) error {
	var b strings.Builder
	b.WriteString("# Written by crew (REQUESTS · a). Plain values only: Claude can read this file.\n")
	fmt.Fprintf(&b, "baseUrl = %s\n", e.BaseURL())
	if h, _ := e.AuthHeader(); h != "" {
		if e.Auth.Kind == "bearer" {
			fmt.Fprintf(&b, "auth = bearer %s\n", e.Auth.Key)
		} else {
			fmt.Fprintf(&b, "auth = header %s %s\n", e.Auth.Header, e.Auth.Key)
		}
	}
	for _, k := range e.Keys {
		fmt.Fprintf(&b, "key %s\n", k)
	}
	if len(e.Hosts) > 0 {
		fmt.Fprintf(&b, "hosts = %s\n", strings.Join(e.Hosts, ", "))
	}
	var names []string
	for k := range e.Vars {
		if k != "baseUrl" {
			names = append(names, k)
		}
	}
	slices.Sort(names)
	for _, k := range names {
		fmt.Fprintf(&b, "%s = %s\n", k, e.Vars[k])
	}
	if err := os.MkdirAll(envDir(dir, e.App), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(envDir(dir, e.App), e.Name+".env"), []byte(b.String()), 0o644)
}

// DeleteEnv removes an environment and its keys.
func DeleteEnv(dir string, e Env) error {
	for _, k := range e.Keys {
		DeleteKey(e, k)
	}
	return os.Remove(filepath.Join(envDir(dir, e.App), e.Name+".env"))
}

// Current is the environment an application's requests are sent with (its first one if none is picked).
func Current(dir, app string) string {
	data, _ := os.ReadFile(filepath.Join(dir, app, ".current-env"))
	envs := Envs(dir, app)
	if c := strings.TrimSpace(string(data)); slices.Contains(envs, c) {
		return c
	}
	if len(envs) > 0 {
		return envs[0]
	}
	return ""
}

// SetCurrent picks the environment an application's requests are sent with.
func SetCurrent(dir, app, env string) error {
	return os.WriteFile(filepath.Join(dir, app, ".current-env"), []byte(env+"\n"), 0o644)
}

// ValidName reports whether s works as an application, environment or key name.
func ValidName(s string) bool {
	return s != "" && !strings.HasPrefix(s, ".") && !strings.ContainsAny(s, " /\\{}=#:")
}
