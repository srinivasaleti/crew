package requests

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// OpenAPI: importing a spec (OpenAPI 3.x or Swagger 2.0, YAML or JSON) as an application's requests,
// and exporting an application (or one folder) as an OpenAPI 3.0 spec. Secrets never go into a spec:
// keys stay {{name}}.

// spec is the part of a spec importing reads. A map keeps it short: specs vary a lot.
type spec map[string]any

// Imported is what an import did: requests added (by folder), ones already there, and the spec's
// servers, for an environment's base URL.
type Imported struct {
	Added   int
	Skipped int
	Folders []string
	Servers []string
}

var pathParam = regexp.MustCompile(`\{([^{}/]+)\}`)

// Import adds a spec's operations to an application as requests. Each goes into the folder named after
// its first tag, or into folder when it's set ("" with byTag false is the default folder). A request
// the application already has (same method and URL) is skipped, so importing again after the spec
// changes adds only what's new.
func Import(dir, app string, text []byte, folder string, byTag bool) (Imported, error) {
	var out Imported
	if !slices.Contains(Apps(dir), app) {
		return out, fmt.Errorf("no application %q", app)
	}
	var raw map[string]any // a plain map: yaml.v3 gives nested maps the target's type
	if err := yaml.Unmarshal(text, &raw); err != nil || raw == nil {
		return out, fmt.Errorf("that isn't an OpenAPI spec (YAML or JSON): %v", err)
	}
	s := spec(raw)
	if s.str("openapi") == "" && s.str("swagger") == "" {
		return out, fmt.Errorf("that isn't an OpenAPI spec: it has no openapi (or swagger) version")
	}
	if folder != "" && !ValidName(folder) {
		return out, fmt.Errorf("a folder is one word, like orgs")
	}
	for _, sv := range s.list("servers") {
		if m, _ := sv.(map[string]any); str(m["url"]) != "" {
			u := str(m["url"])
			out.Servers = append(out.Servers, u)
		}
	}
	if h := s.str("host"); h != "" { // Swagger 2.0
		scheme := "https"
		if ss := s.list("schemes"); len(ss) > 0 {
			scheme, _ = ss[0].(string)
		}
		out.Servers = append(out.Servers, scheme+"://"+h+s.str("basePath"))
	}
	all, err := Load(dir)
	if err != nil {
		return out, err
	}
	have := map[string]bool{}
	for _, r := range all {
		if r.App == app {
			have[r.Method+" "+r.URL] = true
		}
	}
	folders := map[string]bool{}
	paths := s.obj("paths")
	for _, p := range sortedKeys(paths) {
		item, _ := paths[p].(map[string]any)
		shared := asList(item["parameters"])
		for _, m := range []string{"get", "post", "put", "patch", "delete", "head", "options"} {
			op, ok := item[m].(map[string]any)
			if !ok {
				continue
			}
			r := s.request(app, p, strings.ToUpper(m), op, shared)
			if have[r.Method+" "+r.URL] {
				out.Skipped++
				continue
			}
			r.Folder = folder
			if byTag {
				r.Folder = ""
				if tags := asList(op["tags"]); len(tags) > 0 {
					if t, _ := tags[0].(string); t != "" {
						r.Folder = folderName(t)
					}
				}
			}
			if r.Folder != "" && !folders[r.Folder] {
				if !slices.Contains(Folders(dir, app), r.Folder) {
					if err := AddFolder(dir, app, r.Folder); err != nil {
						return out, err
					}
				}
				folders[r.Folder] = true
				out.Folders = append(out.Folders, r.Folder)
			}
			if _, err := Save(dir, r); err != nil {
				return out, err
			}
			have[r.Method+" "+r.URL] = true
			out.Added++
		}
	}
	sort.Strings(out.Folders)
	return out, nil
}

// request is one operation as a request: {{baseUrl}} and the path with {param} as {{param}}, required
// query parameters and header ones as {{variables}}, and a JSON body from the spec's example or schema.
func (s spec) request(app, path, method string, op map[string]any, shared []any) Request {
	r := Request{App: app, Method: method, URL: "{{baseUrl}}" + pathParam.ReplaceAllString(path, "{{$1}}")}
	r.Name = firstOf(str(op["summary"]), str(op["operationId"]), method+" "+path)
	var query []string
	var body any
	for _, raw := range append(shared, asList(op["parameters"])...) {
		p := s.resolve(raw)
		name := str(p["name"])
		switch str(p["in"]) {
		case "query":
			if p["required"] == true {
				query = append(query, name+"={{"+name+"}}")
			}
		case "header":
			if !strings.EqualFold(name, "Authorization") {
				r.Headers = append(r.Headers, [2]string{name, "{{" + varName(name) + "}}"})
			}
		case "body": // Swagger 2.0
			body = s.example(p["schema"], 0)
		}
	}
	if len(query) > 0 {
		r.URL += "?" + strings.Join(query, "&")
	}
	if rb := s.resolve(op["requestBody"]); rb != nil {
		content, _ := rb["content"].(map[string]any)
		for _, ct := range sortedKeys(content) {
			if strings.Contains(ct, "json") {
				media, _ := content[ct].(map[string]any)
				body = s.mediaExample(media)
				break
			}
		}
	}
	if body != nil {
		b, _ := json.MarshalIndent(body, "", "  ")
		r.Body = string(b)
		r.Headers = append(r.Headers, [2]string{"Content-Type", "application/json"})
	}
	if sec, ok := op["security"]; ok && len(asList(sec)) == 0 {
		r.NoAuth = true // security: [] says this operation needs none
	}
	return r
}

// mediaExample is a media type's example: its own, the first of its examples, else one built from its schema.
func (s spec) mediaExample(media map[string]any) any {
	if ex, ok := media["example"]; ok {
		return ex
	}
	if exs, ok := media["examples"].(map[string]any); ok {
		for _, k := range sortedKeys(exs) {
			if e := s.resolve(exs[k]); e != nil {
				if v, ok := e["value"]; ok {
					return v
				}
			}
		}
	}
	return s.example(media["schema"], 0)
}

// example is a value shaped like a schema: its example or default, else an empty one of its type
// (objects with their properties, arrays with one item), following $refs a few levels deep.
func (s spec) example(raw any, depth int) any {
	sc := s.resolve(raw)
	if sc == nil || depth > 6 {
		return nil
	}
	if ex, ok := sc["example"]; ok {
		return ex
	}
	if d, ok := sc["default"]; ok {
		return d
	}
	if e := asList(sc["enum"]); len(e) > 0 {
		return e[0]
	}
	for _, k := range []string{"allOf", "oneOf", "anyOf"} {
		if parts := asList(sc[k]); len(parts) > 0 {
			if k != "allOf" {
				return s.example(parts[0], depth+1)
			}
			merged := map[string]any{}
			for _, p := range parts {
				if m, ok := s.example(p, depth+1).(map[string]any); ok {
					for n, v := range m {
						merged[n] = v
					}
				}
			}
			return merged
		}
	}
	switch str(sc["type"]) {
	case "string":
		return ""
	case "integer", "number":
		return 0
	case "boolean":
		return false
	case "array":
		if it := s.example(sc["items"], depth+1); it != nil {
			return []any{it}
		}
		return []any{}
	}
	props, _ := sc["properties"].(map[string]any)
	if props == nil && str(sc["type"]) != "object" {
		return nil
	}
	obj := map[string]any{}
	for _, n := range sortedKeys(props) {
		obj[n] = s.example(props[n], depth+1)
	}
	return obj
}

// resolve follows a {"$ref": "#/components/…"} to what it names; anything else comes back as it is.
func (s spec) resolve(raw any) map[string]any {
	m, _ := raw.(map[string]any)
	for i := 0; m != nil && i < 8; i++ {
		ref, ok := m["$ref"].(string)
		if !ok {
			return m
		}
		var cur any = map[string]any(s)
		for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
			c, _ := cur.(map[string]any)
			cur = c[strings.NewReplacer("~1", "/", "~0", "~").Replace(part)]
		}
		m, _ = cur.(map[string]any)
	}
	return m
}

// Export is an application's requests (or one folder's, when folder is set) as an OpenAPI 3.0 spec,
// in YAML or JSON: each request an operation tagged with its folder, its {{variables}} as parameters,
// its body as the example, the environments' base URLs as servers and their auth as security. Key
// values never go in: a spec is something you share.
func Export(dir, app, folder string, asJSON bool) ([]byte, error) {
	if !slices.Contains(Apps(dir), app) {
		return nil, fmt.Errorf("no application %q", app)
	}
	all, err := Load(dir)
	if err != nil {
		return nil, err
	}
	doc := map[string]any{"openapi": "3.0.3", "info": map[string]any{"title": app, "version": "1.0.0"}}
	var servers []any
	schemes := map[string]any{}
	for _, en := range Envs(dir, app) {
		e, err := LoadEnv(dir, app, en)
		if err != nil {
			continue
		}
		if e.BaseURL() != "" {
			servers = append(servers, map[string]any{"url": e.BaseURL(), "description": e.Name})
		}
		switch e.Auth.Kind {
		case "bearer":
			schemes["bearer"] = map[string]any{"type": "http", "scheme": "bearer"}
		case "header":
			schemes[e.Auth.Header] = map[string]any{"type": "apiKey", "in": "header", "name": e.Auth.Header}
		}
	}
	if len(servers) > 0 {
		doc["servers"] = servers
	}
	if len(schemes) > 0 {
		var sec []any
		for _, n := range sortedKeys(schemes) {
			sec = append(sec, map[string]any{n: []any{}})
		}
		doc["components"] = map[string]any{"securitySchemes": schemes}
		doc["security"] = sec
	}
	paths := map[string]any{}
	tags := map[string]bool{}
	for _, r := range all {
		if r.App != app || folder != "" && r.Folder != folder {
			continue
		}
		path, op := exportOp(r)
		if r.Folder != "" {
			op["tags"] = []any{r.Folder}
			tags[r.Folder] = true
		}
		item, _ := paths[path].(map[string]any)
		if item == nil {
			item = map[string]any{}
			paths[path] = item
		}
		item[strings.ToLower(r.Method)] = op
	}
	doc["paths"] = paths
	if len(tags) > 0 {
		var ts []any
		for _, t := range sortedKeys(tags) {
			ts = append(ts, map[string]any{"name": t})
		}
		doc["tags"] = ts
	}
	if asJSON {
		return json.MarshalIndent(doc, "", "  ")
	}
	return yaml.Marshal(doc)
}

// exportOp is a request as an operation: its path ({{x}} as {x}, a path parameter), its query and
// header values as parameters with their values as examples, and its body as the request's example.
func exportOp(r Request) (string, map[string]any) {
	u := strings.TrimPrefix(r.URL, "{{baseUrl}}")
	u, q, _ := strings.Cut(u, "?")
	vars := regexp.MustCompile(`\{\{\s*([\w.-]+)\s*\}\}`)
	var params []any
	path := vars.ReplaceAllStringFunc(u, func(m string) string {
		n := vars.FindStringSubmatch(m)[1]
		params = append(params, map[string]any{"name": n, "in": "path", "required": true, "schema": map[string]any{"type": "string"}})
		return "{" + n + "}"
	})
	if path == "" {
		path = "/"
	}
	for _, kv := range strings.Split(q, "&") {
		if k, v, _ := strings.Cut(kv, "="); k != "" {
			params = append(params, map[string]any{"name": k, "in": "query", "schema": map[string]any{"type": "string"}, "example": v})
		}
	}
	op := map[string]any{"summary": r.Name, "responses": map[string]any{"default": map[string]any{"description": "Response"}}}
	for _, h := range r.Headers {
		if !strings.EqualFold(h[0], "Content-Type") && !strings.EqualFold(h[0], "Authorization") {
			params = append(params, map[string]any{"name": h[0], "in": "header", "schema": map[string]any{"type": "string"}, "example": h[1]})
		}
	}
	if len(params) > 0 {
		op["parameters"] = params
	}
	if strings.TrimSpace(r.Body) != "" {
		var ex any = r.Body
		var v any
		if json.Unmarshal([]byte(r.Body), &v) == nil {
			ex = v // valid JSON goes in as JSON; one with {{variables}} in it stays as written
		}
		op["requestBody"] = map[string]any{"content": map[string]any{"application/json": map[string]any{"example": ex}}}
	}
	if r.NoAuth {
		op["security"] = []any{}
	}
	return path, op
}

func (s spec) str(k string) string         { return str(s[k]) }
func (s spec) list(k string) []any         { return asList(s[k]) }
func (s spec) obj(k string) map[string]any { m, _ := s[k].(map[string]any); return m }

func str(v any) string   { s, _ := v.(string); return s }
func asList(v any) []any { l, _ := v.([]any); return l }
func firstOf(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// sortedKeys is a map's keys in order, so imports and exports come out the same every time.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// folderName is a tag as a folder's name: one lowercase word ("Org Members" is org-members).
func folderName(tag string) string {
	f := strings.Trim(regexp.MustCompile(`[^a-z0-9_-]+`).ReplaceAllString(strings.ToLower(tag), "-"), "-")
	if f == "" || f == "env" {
		return ""
	}
	return f
}

// varName is a header's name as a {{variable}} (X-Tenant-Id is tenantId).
func varName(h string) string {
	parts := strings.FieldsFunc(strings.TrimPrefix(strings.TrimPrefix(h, "X-"), "x-"), func(r rune) bool { return r == '-' || r == '_' })
	for i, p := range parts {
		if i == 0 {
			parts[i] = strings.ToLower(p)
		} else if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
		}
	}
	return strings.Join(parts, "")
}
