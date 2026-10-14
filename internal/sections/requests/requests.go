// Package requests is crew's HTTP client and its REQUESTS screens. Requests belong to applications
// and sit in folders; an application has environments (dev, staging, prod), each with a base URL,
// plain values, an auth method and its keys, which live in the macOS Keychain. Keys are filled in
// only when a request is sent, only to the environment's own hosts, and are blanked out of what's
// shown, so they never reach an AI chat.
//
// On disk (so Claude can read and write them, and you never have to):
//
//	<dir>/<app>/env/<env>.env        an environment (env.go)
//	<dir>/<app>/<folder>/*.http      its requests, in the VS Code / JetBrains REST Client format
//	<dir>/<app>/*.http               requests in no folder ("default")
//	<dir>/<app>/.folded              the folders you folded
//
// The screens: screen.go (the applications, an application's folders and environments), form.go
// (a modal form with buttons), forms.go (the application, folder, environment and request forms).
package requests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

// Request is one request in a .http file. A file holds one or more, each starting at a "###" line.
type Request struct {
	App     string // the application
	Folder  string // its folder in the application; "" is the default one
	File    string // relative to the requests folder: <app>/[<folder>/]<name>.http
	Index   int    // its place in the file, from 0
	Name    string // the "### name" title, else "METHOD url"
	Method  string
	URL     string      // with {{variables}} as written, usually {{baseUrl}}/path
	Headers [][2]string // as written
	Body    string
	NoAuth  bool     // "# @auth none": don't add the environment's auth
	Tags    []string // "# @tags smoke, orgs": for filtering
}

// ID names a request: <app>/<file>#index.
func (r Request) ID() string { return fmt.Sprintf("%s#%d", r.File, r.Index) }

// Path is the URL without the {{baseUrl}} in front, as you'd type it.
func (r Request) Path() string { return strings.TrimPrefix(r.URL, "{{baseUrl}}") }

// Text is the request as written (variables not filled in).
func (r Request) Text() string {
	var b strings.Builder
	if len(r.Tags) > 0 {
		b.WriteString("# @tags " + strings.Join(r.Tags, ", ") + "\n")
	}
	if r.NoAuth {
		b.WriteString("# @auth none\n")
	}
	b.WriteString(r.Method + " " + r.URL + "\n")
	for _, h := range r.Headers {
		b.WriteString(h[0] + ": " + h[1] + "\n")
	}
	if r.Body != "" {
		b.WriteString("\n" + r.Body + "\n")
	}
	return b.String()
}

// Methods are the methods crew offers, in order.
var Methods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}

// Parse reads the requests in a .http file's text.
// ponytail: no {{$dynamic}} variables, response handlers or file includes; add them when asked for.
func Parse(file, text string) []Request {
	var out []Request
	parts := strings.Split(filepath.ToSlash(file), "/")
	app, folder := parts[0], ""
	if len(parts) == 3 {
		folder = parts[1]
	}
	for _, block := range regexp.MustCompile(`(?m)^###`).Split("\n"+text, -1) {
		lines := strings.Split(block, "\n")
		r := Request{App: app, Folder: folder, File: file, Name: strings.TrimSpace(lines[0])}
		i := 1
		for ; i < len(lines); i++ { // comments and blank lines before the request line
			l := strings.TrimSpace(lines[i])
			if l == "# @auth none" {
				r.NoAuth = true
			}
			if tags, ok := strings.CutPrefix(l, "# @tags "); ok {
				r.Tags = SplitTags(tags)
			}
			if l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, "//") {
				continue
			}
			break
		}
		if i >= len(lines) {
			continue
		}
		f := strings.Fields(lines[i])
		if len(f) == 1 {
			f = []string{"GET", f[0]}
		}
		if len(f) < 2 || !slices.Contains(Methods, strings.ToUpper(f[0])) {
			continue
		}
		r.Method, r.URL = strings.ToUpper(f[0]), f[1]
		for i++; i < len(lines) && strings.TrimSpace(lines[i]) != ""; i++ {
			if k, v, ok := strings.Cut(lines[i], ":"); ok && !strings.HasPrefix(strings.TrimSpace(lines[i]), "#") {
				r.Headers = append(r.Headers, [2]string{strings.TrimSpace(k), strings.TrimSpace(v)})
			}
		}
		if i < len(lines) {
			r.Body = strings.TrimSpace(strings.Join(lines[i:], "\n"))
		}
		if r.Name == "" {
			r.Name = r.Method + " " + r.Path()
		}
		r.Index = len(out)
		out = append(out, r)
	}
	return out
}

// SplitTags reads a comma-separated list of tags: trimmed, lower case, no repeats.
func SplitTags(s string) []string {
	var out []string
	for _, t := range strings.Split(s, ",") {
		if t = strings.ToLower(strings.TrimSpace(t)); t != "" && !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	return out
}

// Load reads every application's requests, sorted by file.
func Load(dir string) ([]Request, error) {
	var out []Request
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(p) != ".http" {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if n := strings.Count(filepath.ToSlash(rel), "/"); n < 1 || n > 2 {
			return nil // not in an application, or deeper than a folder
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out = append(out, Parse(rel, string(data))...)
		return nil
	})
	return out, err
}

// Save writes a request: in its file at its index, or as a new file <app>/<name>.http when File is
// "". It returns the request as saved.
func Save(dir string, r Request) (Request, error) {
	if r.File == "" {
		base := slug(r.Name)
		r.File, r.Index = filepath.Join(r.App, r.Folder, base+".http"), 0
		for n := 2; exists(filepath.Join(dir, r.File)); n++ {
			r.File = filepath.Join(r.App, r.Folder, fmt.Sprintf("%s-%d.http", base, n))
		}
		return r, write(dir, r.File, []Request{r})
	}
	all := inFile(dir, r.File)
	if r.Index >= len(all) {
		r.Index = len(all)
		all = append(all, r)
	} else {
		all[r.Index] = r
	}
	return r, write(dir, r.File, all)
}

// Move puts a request in another folder of its application, and returns it as moved.
func Move(dir string, r Request, folder string) (Request, error) {
	if r.Folder == folder {
		return r, nil
	}
	if err := Delete(dir, r); err != nil {
		return r, err
	}
	r.Folder, r.File = folder, ""
	return Save(dir, r)
}

// Delete removes a request from its file, and the file when it was the last one.
func Delete(dir string, r Request) error {
	all := inFile(dir, r.File)
	if r.Index < len(all) {
		all = slices.Delete(all, r.Index, r.Index+1)
	}
	if len(all) == 0 {
		return os.Remove(filepath.Join(dir, r.File))
	}
	return write(dir, r.File, all)
}

func inFile(dir, file string) []Request {
	data, _ := os.ReadFile(filepath.Join(dir, file))
	return Parse(file, string(data))
}

func write(dir, file string, reqs []Request) error {
	var b strings.Builder
	for i, r := range reqs {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("### " + r.Name + "\n" + r.Text())
	}
	p := filepath.Join(dir, file)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(b.String()), 0o644)
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// slug is a file-safe name: lower case letters, digits and dashes.
func slug(s string) string {
	s = strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > 40 {
		s = strings.TrimRight(s[:40], "-")
	}
	if s == "" {
		s = "request"
	}
	return s
}

var varRef = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_.-]+)\s*\}\}`)

// Uses lists the variables a request refers to.
func (r Request) Uses() []string {
	var out []string
	for _, m := range varRef.FindAllStringSubmatch(r.Text(), -1) {
		if !slices.Contains(out, m[1]) {
			out = append(out, m[1])
		}
	}
	return out
}

// WithAuth is the request as it's sent in env: with the environment's auth header added, unless
// the request opts out (@auth none) or sets that header itself.
func (r Request) WithAuth(env Env) Request {
	h, v := env.AuthHeader()
	if r.NoAuth || h == "" || slices.ContainsFunc(r.Headers, func(x [2]string) bool { return strings.EqualFold(x[0], h) }) {
		return r
	}
	r.Headers = append(slices.Clone(r.Headers), [2]string{h, v})
	return r
}

// Response is what came back.
type Response struct {
	Status  string
	Code    int
	Headers http.Header
	Body    []byte
	Took    time.Duration
}

// maxBody is how much of a response body is kept; maxAIBody how much of it the AI is shown.
const (
	maxBody   = 2 << 20
	maxAIBody = 16 << 10
)

// Send fills in the request's variables from env and keys (the values of the keys it uses) and
// sends it with env's auth. A request that uses a key is refused unless it goes to one of env's hosts.
func Send(ctx context.Context, r Request, env Env, keys map[string]string) (*Response, error) {
	r = r.WithAuth(env)
	var missing []string
	fill := func(s string) string {
		return varRef.ReplaceAllStringFunc(s, func(ref string) string {
			name := varRef.FindStringSubmatch(ref)[1]
			if v, ok := keys[name]; ok {
				return v
			}
			if v, ok := env.Vars[name]; ok {
				return v
			}
			missing = append(missing, name)
			return ref
		})
	}
	u, err := url.Parse(env.Resolve(r.URL)) // plain values only, so the host check sees the real host
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("not a full URL: %s (is %s's base URL set?)", env.Resolve(r.URL), env.Name)
	}
	if used := r.keysUsed(env); len(used) > 0 && !slices.Contains(env.AllHosts(), u.Hostname()) {
		return nil, fmt.Errorf("refused: %s would send the key %s to %s, but %s's keys only go to %s",
			r.Name, strings.Join(used, ", "), u.Hostname(), env.Name, strings.Join(env.AllHosts(), ", "))
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, fill(r.URL), strings.NewReader(fill(r.Body)))
	if err != nil {
		return nil, err
	}
	for _, h := range r.Headers {
		req.Header.Add(h[0], fill(h[1]))
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%s has no value for %s", env.Name, strings.Join(missing, ", "))
	}
	start := time.Now()
	res, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	return &Response{Status: res.Status, Code: res.StatusCode, Headers: res.Header, Body: body, Took: time.Since(start)}, err
}

// keysUsed is the env's keys the request refers to (its auth included).
func (r Request) keysUsed(env Env) []string {
	var out []string
	for _, name := range r.WithAuth(env).Uses() {
		if slices.Contains(env.Keys, name) {
			out = append(out, name)
		}
	}
	return out
}

// KeysFor reads, from the Keychain, the values of the env's keys the request uses.
func KeysFor(r Request, env Env) (map[string]string, error) {
	out := map[string]string{}
	for _, name := range r.keysUsed(env) {
		v, err := GetKey(env, name)
		if err != nil {
			return nil, fmt.Errorf("the key %s isn't set for %s %s (set it in crew: a, then the environment)", name, env.App, env.Name)
		}
		out[name] = v
	}
	return out, nil
}

// sensitive is a header or JSON field whose value is blanked out of what the AI sees.
var sensitive = regexp.MustCompile(`(?i)(token|secret|password|passwd|api[_-]?key|authorization|cookie|session|credential|private[_-]?key)`)

// jsonField is a "name": "value" pair in JSON text.
var jsonField = regexp.MustCompile(`("([^"\\]*)"\s*:\s*)"((?:[^"\\]|\\.)*)"`)

// Redact blanks out the keys' values and, with fields, the values of sensitive-looking JSON fields
// (a token a login returns, say).
// ponytail: exact values only; a key that comes back encoded (base64, URL-escaped) isn't caught.
func Redact(s string, keys map[string]string, fields bool) string {
	for name, v := range keys {
		if len(v) >= 4 {
			s = strings.ReplaceAll(s, v, "«key "+name+"»")
		}
	}
	if fields {
		s = jsonField.ReplaceAllStringFunc(s, func(m string) string {
			p := jsonField.FindStringSubmatch(m)
			if sensitive.MatchString(p[2]) && p[3] != "" {
				return p[1] + `"«hidden»"`
			}
			return m
		})
	}
	return s
}

// Format writes a response as text: the status line, the headers, then the body (JSON indented).
// With forAI, sensitive headers and fields are hidden too, and the body is cut short.
func Format(res *Response, keys map[string]string, forAI bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s · %dms · %s\n", res.Status, res.Took.Milliseconds(), size(len(res.Body)))
	names := make([]string, 0, len(res.Headers))
	for k := range res.Headers {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		v := strings.Join(res.Headers[k], ", ")
		if forAI && sensitive.MatchString(k) {
			v = "«hidden»"
		}
		fmt.Fprintf(&b, "%s: %s\n", k, v)
	}
	body := res.Body
	var pretty bytes.Buffer
	if json.Indent(&pretty, body, "", "  ") == nil {
		body = pretty.Bytes()
	}
	switch {
	case forAI && len(body) > maxAIBody: // keeps Claude's context small; crew's Response pane has it all
		b.WriteString("\n" + string(body[:maxAIBody]) + fmt.Sprintf("\n… %s more (the Response pane in crew shows it all)", size(len(body)-maxAIBody)))
	default:
		b.WriteString("\n" + string(body))
		if len(res.Body) == maxBody {
			b.WriteString("\n… (cut at 2 MB)")
		}
	}
	return Redact(b.String(), keys, forAI)
}

func size(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f kB", float64(n)/1024)
}
