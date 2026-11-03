package requests

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const testSpec = `
openapi: 3.0.3
servers: [{url: https://api.acme.io/v1}]
paths:
  /orgs/{orgId}/keys:
    parameters:
      - {name: orgId, in: path, required: true, schema: {type: string}}
    get:
      tags: [API Keys]
      summary: List keys
      parameters:
        - {name: limit, in: query, required: true, schema: {type: integer}}
        - {name: cursor, in: query, schema: {type: string}}
        - {name: X-Tenant-Id, in: header, schema: {type: string}}
    post:
      tags: [API Keys]
      operationId: createKey
      requestBody:
        content:
          application/json:
            schema: {$ref: '#/components/schemas/NewKey'}
  /health:
    get:
      summary: Health
      security: []
  /orgs:
    post:
      tags: [Orgs]
      requestBody:
        content:
          application/json:
            example: {name: Acme}
components:
  schemas:
    NewKey:
      type: object
      properties:
        name: {type: string, example: CI key}
        grants:
          type: array
          items: {$ref: '#/components/schemas/Grant'}
    Grant:
      type: object
      properties:
        entityId: {type: string}
        enabled: {type: boolean}
`

// A spec's operations come in as requests in folders by tag, with {{variables}}, bodies from examples
// and schemas, and no auth where the spec says so; importing again adds nothing; exporting gives a
// spec with the same operations back.
func TestOpenAPI(t *testing.T) {
	dir := t.TempDir()
	AddApp(dir, "acme")
	got, err := Import(dir, "acme", []byte(testSpec), "", true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Added != 4 || got.Skipped != 0 || strings.Join(got.Folders, ",") != "api-keys,orgs" || len(got.Servers) != 1 || got.Servers[0] != "https://api.acme.io/v1" {
		t.Fatalf("imported %+v", got)
	}
	reqs, _ := Load(dir)
	by := map[string]Request{}
	for _, r := range reqs {
		by[r.Method+" "+r.URL] = r
	}
	list := by["GET {{baseUrl}}/orgs/{{orgId}}/keys?limit={{limit}}"]
	if list.Name != "List keys" || list.Folder != "api-keys" || len(list.Headers) != 1 || list.Headers[0] != [2]string{"X-Tenant-Id", "{{tenantId}}"} {
		t.Errorf("the list request: %+v", list)
	}
	create := by["POST {{baseUrl}}/orgs/{{orgId}}/keys"]
	var body map[string]any
	if err := json.Unmarshal([]byte(create.Body), &body); err != nil || create.Name != "createKey" || body["name"] != "CI key" {
		t.Errorf("the create request: %+v (%v)", create, err)
	}
	if g, _ := body["grants"].([]any); len(g) != 1 || g[0].(map[string]any)["enabled"] != false {
		t.Errorf("grants from the schema: %v", body["grants"])
	}
	if h := by["GET {{baseUrl}}/health"]; !h.NoAuth || h.Folder != "" {
		t.Errorf("health: %+v", h)
	}
	if o := by["POST {{baseUrl}}/orgs"]; !strings.Contains(o.Body, `"Acme"`) {
		t.Errorf("orgs body from its example: %q", o.Body)
	}
	if again, _ := Import(dir, "acme", []byte(testSpec), "", true); again.Added != 0 || again.Skipped != 4 {
		t.Errorf("importing again: %+v", again)
	}
	if one, err := Import(dir, "acme", []byte(strings.Replace(testSpec, "/health", "/ready", 1)), "misc", false); err != nil || one.Added != 1 || strings.Join(one.Folders, ",") != "misc" {
		t.Errorf("into one folder: %+v %v", one, err)
	}

	out, err := Export(dir, "acme", "", false)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	paths := doc["paths"].(map[string]any)
	op := paths["/orgs/{orgId}/keys"].(map[string]any)["get"].(map[string]any)
	if op["summary"] != "List keys" || op["tags"].([]any)[0] != "api-keys" || len(op["parameters"].([]any)) != 3 {
		t.Errorf("exported list op: %v", op)
	}
	if h := paths["/health"].(map[string]any)["get"].(map[string]any); len(h["security"].([]any)) != 0 {
		t.Errorf("health keeps no auth: %v", h)
	}
	if one, _ := Export(dir, "acme", "orgs", true); !strings.Contains(string(one), `"/orgs"`) || strings.Contains(string(one), "/health") {
		t.Errorf("one folder's export: %s", one)
	}
	if _, err := Import(dir, "acme", []byte("name: not a spec"), "", true); err == nil {
		t.Error("a file that isn't a spec was imported")
	}
}
