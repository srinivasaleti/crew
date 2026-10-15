package requests

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"crew/internal/widgets/look"
)

// The screen's forms: a new application, a new folder, an environment (its base URL, auth and
// key), moving a request, and a request's popup (edit, send, see the response, save, delete).

func (s *Screen) appForm() *Form {
	f := newForm("New application", []field{text("Name", "One word, like accounts or billing", "", "e.g. accounts")})
	f.save = func(f *Form) error {
		name := strings.ToLower(f.val(0))
		switch {
		case !ValidName(name):
			return errors.New("One word, like accounts")
		case slices.Contains(s.apps, name):
			return fmt.Errorf("%s already exists", name)
		}
		if err := os.MkdirAll(filepath.Join(s.dir, name), 0o755); err != nil {
			return err
		}
		s.Load()
		s.Notice = "Added " + name + " · open it, then Environments · n adds its first environment"
		return nil
	}
	return f.start()
}

func (s *Screen) folderForm(app string) *Form {
	f := newForm(app+" · new folder", []field{text("Name", "One word, like orgs or locations", "", "e.g. orgs")})
	f.save = func(f *Form) error {
		name := strings.ToLower(f.val(0))
		if err := AddFolder(s.dir, app, name); err != nil {
			return err
		}
		s.Load()
		s.Notice = "Added the folder " + name + " · n in it makes a request there"
		return nil
	}
	return f.start()
}

func (s *Screen) moveForm(q Request) *Form {
	var folders, labels []string
	for _, f := range append(Folders(s.dir, q.App), "") {
		folders, labels = append(folders, f), append(labels, folderLabel(f))
	}
	f := newForm("Move "+q.Name, []field{choice("To", "", folders, labels, q.Folder)})
	f.save = func(f *Form) error {
		moved, err := Move(s.dir, q, f.val(0))
		if err != nil {
			return err
		}
		if r := s.runs[q.ID()]; r != nil {
			s.runs[moved.ID()] = r
		}
		s.Load()
		s.Notice = "Moved " + q.Name + " to " + folderLabel(moved.Folder)
		return nil
	}
	return f.start()
}

// Environment form fields.
const (
	efName = iota
	efBase
	efAuth
	efHeader
	efKey
	efValue
	efHosts
	efVars
	efTLS
)

// envForm is a new environment of app (env ""), or app's env to edit.
func (s *Screen) envForm(app, env string) *Form {
	e := Env{App: app, Vars: map[string]string{}, Auth: Auth{Kind: "bearer", Key: "token"}}
	title, keyHint := app+" · new environment", "Kept in the macOS Keychain; Claude never sees it"
	if env != "" {
		for _, x := range s.envs[app] {
			if x.Name == env {
				e = x
			}
		}
		title = app + " · " + env
		if s.keySet[app+"/"+env] {
			keyHint = "Set · leave it empty to keep it"
		}
	}
	var vars []string
	for k, v := range e.Vars {
		if k != "baseUrl" {
			vars = append(vars, k+"="+v)
		}
	}
	slices.Sort(vars)
	header := e.Auth.Header
	if header == "" {
		header = "X-Api-Key"
	}
	f := newForm(title, []field{
		text("Name", "dev, staging, prod…", env, "e.g. dev"),
		text("Base URL", "Request paths go after it", e.BaseURL(), "e.g. https://dev.accounts.example.com"),
		choice("Auth", "Added to every request", AuthKinds, []string{"none", "Bearer token", "API key header"}, e.Auth.Kind),
		text("Header", "The API key's header", header, "e.g. X-Api-Key"),
		text("Key name", "Requests refer to it as {{name}}", e.Auth.Key, "e.g. token"),
		secretText("Key value", keyHint),
		text("More hosts", "Optional: other hosts the key may go to, separated by commas", strings.Join(e.Hosts, ", "), "optional"),
		text("Values", "Optional: other {{values}}, as name=value; name=value", strings.Join(vars, "; "), "optional"),
		choice("Certificates", "Insecure is for a dev server with a self-signed certificate, like curl --insecure",
			[]string{"", "insecure"}, []string{"checked", "insecure"}, map[bool]string{true: "insecure"}[e.Insecure]),
	})
	f.fields[efName].hidden = env != ""
	f.change = func(f *Form) {
		kind := f.val(efAuth)
		f.fields[efHeader].hidden = kind != "header"
		f.fields[efKey].hidden = kind == ""
		f.fields[efValue].hidden = kind == ""
	}
	f.preview = func(f *Form) string {
		x := Env{Vars: map[string]string{"baseUrl": f.val(efBase)}, Auth: Auth{Kind: f.val(efAuth), Header: f.val(efHeader), Key: f.val(efKey)}}
		if h, v := x.AuthHeader(); h != "" && len(x.AllHosts()) > 0 {
			return look.Dim.Render(fmt.Sprintf("Every request gets %s: %s · the key goes only to %s", h, v, strings.Join(x.AllHosts(), ", ")))
		}
		return ""
	}
	f.save = func(f *Form) error {
		name := env
		if name == "" {
			name = strings.ToLower(f.val(efName))
		}
		ne := Env{App: app, Name: name, Vars: map[string]string{}, Keys: e.Keys, Insecure: f.val(efTLS) == "insecure"}
		for _, v := range strings.Split(f.val(efVars), ";") {
			if k, val, ok := strings.Cut(v, "="); ok && ValidName(strings.TrimSpace(k)) {
				ne.Vars[strings.TrimSpace(k)] = strings.TrimSpace(val)
			}
		}
		ne.Vars["baseUrl"] = strings.TrimRight(f.val(efBase), "/")
		for _, h := range strings.Split(f.val(efHosts), ",") {
			if h = strings.TrimSpace(h); h != "" {
				ne.Hosts = append(ne.Hosts, h)
			}
		}
		if kind := f.val(efAuth); kind != "" {
			ne.Auth = Auth{Kind: kind, Header: f.val(efHeader), Key: f.val(efKey)}
			if !ValidName(ne.Auth.Key) || kind == "header" && !ValidName(ne.Auth.Header) {
				return errors.New("The key name and header are one word, like token or X-Api-Key")
			}
			if !slices.Contains(ne.Keys, ne.Auth.Key) {
				ne.Keys = append(ne.Keys, ne.Auth.Key)
			}
		}
		switch {
		case !ValidName(ne.Name):
			return errors.New("The name is one word, like dev or staging")
		case !strings.HasPrefix(ne.BaseURL(), "http://") && !strings.HasPrefix(ne.BaseURL(), "https://"):
			return errors.New("The base URL starts with http:// or https://")
		case env == "" && slices.ContainsFunc(s.envs[app], func(x Env) bool { return x.Name == ne.Name }):
			return fmt.Errorf("%s already has %s", app, ne.Name)
		}
		if err := SaveEnv(s.dir, ne); err != nil {
			return err
		}
		if v := f.val(efValue); v != "" && ne.Auth.Key != "" {
			if err := SetKey(ne, ne.Auth.Key, v); err != nil {
				return err
			}
		}
		if s.cur[app] == "" {
			s.setEnv(app, ne.Name) // the first one is the one in use
		}
		s.Load()
		s.Notice = "Saved " + app + " · " + ne.Name
		return nil
	}
	return f.start()
}
