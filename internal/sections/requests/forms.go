package requests

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

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

// Request popup fields.
const (
	rfFolder = iota
	rfEnv
	rfName
	rfMethod
	rfURL
	rfHeaders
	rfBody
	rfTags
	rfAuth
)

// reqPopup opens a request (a new one in app's folder when edit is nil): its fields, what it
// sends, Send / Save / Delete / Close, and the response under them.
func (s *Screen) reqPopup(edit *Request, app, folder string) *Form {
	r := Request{App: app, Folder: folder, Method: "GET"}
	if edit != nil {
		r = *edit
	}
	var headers, envs, folders, folderLabels []string
	for _, h := range r.Headers {
		headers = append(headers, h[0]+": "+h[1])
	}
	for _, e := range s.envs[app] {
		envs = append(envs, e.Name)
	}
	for _, f := range append(Folders(s.dir, app), "") {
		folders, folderLabels = append(folders, f), append(folderLabels, folderLabel(f))
	}
	tagHint := "Tags, separated by commas, to filter the requests by"
	if tags := s.tags(app); len(tags) > 0 {
		tagHint += " · in use: " + strings.Join(tags, ", ")
	}
	auth := "env"
	if r.NoAuth {
		auth = "none"
	}
	f := newForm("", []field{
		choice("Folder", "Where it's kept; changing it moves it when you save", folders, folderLabels, r.Folder),
		choice("Environment", "Where it's sent: switching it changes the URL and the key", envs, nil, s.cur[app]),
		text("Name", "", r.Name, "e.g. List orgs"),
		choice("Method", "", Methods, nil, r.Method),
		text("URL", "A path after the environment's base URL, or a full URL", r.Path(), "e.g. /orgs?limit=20"),
		text("Headers", "Name: value; Name: value", strings.Join(headers, "; "), "optional"),
		text("Body", "", r.Body, `optional, e.g. {"name": "Acme"}`),
		text("Tags", tagHint, strings.Join(r.Tags, ", "), "optional, e.g. orgs, smoke"),
		choice("Auth", "", []string{"env", "none"}, []string{"the environment's", "none"}, auth),
	})
	f.fields[rfEnv].hidden = len(envs) == 0
	cur := edit // the saved request this popup edits, once there is one
	key := func() string {
		if cur != nil {
			return cur.ID()
		}
		return "new:" + app
	}
	f.change = func(f *Form) {
		f.title = app + " · new request"
		if cur != nil {
			f.title = app + " · " + folderLabel(cur.Folder) + " · " + cur.Name
		}
		if f.dirty {
			f.title += " · unsaved"
		}
		label := "the environment's"
		for _, e := range s.envs[app] {
			if e.Name == f.val(rfEnv) {
				label += " (" + e.AuthLabel() + ")"
			}
		}
		f.fields[rfAuth].labels = []string{label, "none"}
	}
	f.preview = func(f *Form) string { return s.preview(f, app) }
	f.body = func(f *Form, w, h int) string { return s.response(f, key(), w, h) }
	f.save = func(f *Form) error {
		q, err := s.fromForm(f, app, cur)
		if err != nil {
			return err
		}
		old := key()
		if cur != nil && q.Folder != cur.Folder { // moved: out of the old file, into the new folder
			if err := Delete(s.dir, *cur); err != nil {
				return err
			}
			q.File = ""
		}
		saved, err := Save(s.dir, q)
		if err != nil {
			return err
		}
		if r := s.runs[old]; r != nil {
			s.runs[saved.ID()] = r // the response stays with it
		}
		cur = &saved
		s.Load()
		return nil
	}
	send := func(f *Form) tea.Cmd {
		f.err, f.scroll = "", 0
		q, err := s.fromForm(f, app, cur)
		if err != nil {
			f.err = err.Error()
			return nil
		}
		if env := f.val(rfEnv); env != "" {
			s.setEnv(app, env) // sending with an environment makes it the one in use
		}
		return s.send(q, key())
	}
	save := func(f *Form) tea.Cmd {
		f.err = ""
		if err := f.save(f); err != nil {
			f.err = err.Error()
			return nil
		}
		f.dirty, f.note = false, "Saved ✓"
		f.change(f)
		return nil
	}
	del := func(f *Form) tea.Cmd {
		if cur == nil {
			return leave(f)
		}
		if !strings.HasPrefix(f.err, "Delete") {
			f.err = "Delete " + cur.Name + "? Press Delete again"
			return nil
		}
		if err := Delete(s.dir, *cur); err != nil {
			f.err = err.Error()
			return nil
		}
		s.Notice = "Deleted " + cur.Name
		s.Load()
		f.closed = true
		return nil
	}
	f.buttons = []button{{"Send", "ctrl+r", send}, {"Save", "ctrl+s", save}, {"Delete", "", del}, {"Close", "esc", leave}}
	f.start()
	f.at = rfName
	if edit != nil {
		f.at = len(f.fields) // on Send
	}
	f.focus()
	return f
}

// fromForm builds the request the popup describes.
func (s *Screen) fromForm(f *Form, app string, edit *Request) (Request, error) {
	q := Request{App: app, Folder: f.val(rfFolder), Method: f.val(rfMethod), Name: f.val(rfName), Body: f.val(rfBody),
		NoAuth: f.val(rfAuth) == "none", Tags: SplitTags(f.val(rfTags))}
	if edit != nil {
		q.File, q.Index = edit.File, edit.Index
	}
	base := ""
	for _, e := range s.envs[app] {
		if e.Name == f.val(rfEnv) {
			base = e.BaseURL()
		}
	}
	switch u := f.val(rfURL); {
	case u == "":
		return q, errors.New("Type the URL: a path like /orgs, or a full URL")
	case strings.HasPrefix(u, "{{"):
		q.URL = u
	case base != "" && strings.HasPrefix(u, base):
		q.URL = "{{baseUrl}}" + strings.TrimPrefix(u, base) // a pasted full URL becomes a path
	case strings.Contains(u, "://"):
		q.URL = u
	default:
		q.URL = "{{baseUrl}}/" + strings.TrimPrefix(u, "/")
	}
	for _, h := range strings.Split(f.val(rfHeaders), ";") {
		if k, v, ok := strings.Cut(h, ":"); ok && strings.TrimSpace(k) != "" {
			q.Headers = append(q.Headers, [2]string{strings.TrimSpace(k), strings.TrimSpace(v)})
		}
	}
	if q.Name == "" {
		q.Name = q.Method + " " + q.Path()
	}
	return q, nil
}

// preview is where the popup's request goes, with its auth, in the picked environment.
func (s *Screen) preview(f *Form, app string) string {
	var env Env
	for _, e := range s.envs[app] {
		if e.Name == f.val(rfEnv) {
			env = e
		}
	}
	if env.Name == "" {
		return look.Warn.Render(app + " has no environment yet, so this can't be sent: add one in its Environments tab")
	}
	q, err := s.fromForm(f, app, nil)
	if err != nil {
		return look.Dim.Render("→ " + env.BaseURL() + "/…")
	}
	q = q.WithAuth(env)
	line := look.Dim.Render("→ ") + methodStyle(q.Method).Render(q.Method) + " " + env.Resolve(q.URL)
	for _, h := range q.Headers {
		line += "\n" + look.Dim.Render("  "+h[0]+": "+h[1])
	}
	return line
}

// response is the bottom of the popup: the last response, h lines from f.scroll.
func (s *Screen) response(f *Form, key string, w, h int) string {
	r := s.runs[key]
	var lines []string
	switch {
	case r == nil:
		return look.Dim.Render("Send (" + KeyName("ctrl+r") + ") shows the response here.")
	case r.running:
		return look.Sel.Render(look.SpinFrames[s.Spin%len(look.SpinFrames)]+" Sending") + look.Dim.Render(fmt.Sprintf(" to %s (%ds)…", r.env, int(time.Since(r.at).Seconds())))
	case r.err != "":
		lines = strings.Split(look.Warn.Render(look.Wrap(r.err, w)), "\n")
	default:
		text := Format(r.res, r.keys, false)
		head, rest, _ := strings.Cut(text, "\n")
		headers, body, _ := strings.Cut(rest, "\n\n")
		lines = append(lines, statusStyle(r.res.Code).Render(head)+look.Dim.Render(" · "+r.env+" · "+look.Ago(r.at)))
		for _, l := range strings.Split(headers, "\n") {
			lines = append(lines, look.Dim.Render(ansi.Truncate(l, w, "…")))
		}
		lines = append(lines, "")
		lines = append(lines, strings.Split(look.Wrap(body, w), "\n")...)
	}
	f.scroll = min(f.scroll, max(len(lines)-h, 0))
	end := min(f.scroll+h, len(lines))
	out := lines[f.scroll:end]
	if end < len(lines) {
		out[len(out)-1] = look.Dim.Render(fmt.Sprintf("… %d more lines · pgdn or the wheel scrolls", len(lines)-end))
	}
	return strings.Join(out, "\n")
}
