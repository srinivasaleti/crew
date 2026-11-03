package jira

import (
	"encoding/json"
	"testing"
)

// A Jira comment in Atlassian's document format comes out as Markdown the page can draw.
func TestADFMarkdown(t *testing.T) {
	doc := `{"type":"doc","content":[
	  {"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Plan"}]},
	  {"type":"paragraph","content":[
	    {"type":"mention","attrs":{"text":"@Sam Lee"}},{"type":"text","text":" see "},
	    {"type":"text","text":"this","marks":[{"type":"link","attrs":{"href":"https://x.io"}}]},
	    {"type":"text","text":" and "},{"type":"text","text":"bold","marks":[{"type":"strong"}]},
	    {"type":"text","text":" "},{"type":"text","text":"run()","marks":[{"type":"code"}]},
	    {"type":"hardBreak"},{"type":"text","text":"next line "},{"type":"status","attrs":{"text":"done"}}]},
	  {"type":"bulletList","content":[
	    {"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"one"}]},
	      {"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"inner"}]}]}]}]},
	    {"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"two"}]}]}]},
	  {"type":"codeBlock","attrs":{"language":"go"},"content":[{"type":"text","text":"x := 1\ny := 2"}]},
	  {"type":"blockquote","content":[{"type":"paragraph","content":[{"type":"text","text":"quoted"}]}]},
	  {"type":"rule"},
	  {"type":"table","content":[
	    {"type":"tableRow","content":[{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"A"}]}]},{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"B"}]}]}]},
	    {"type":"tableRow","content":[{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"1|2"}]}]},{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"3"}]}]}]}]},
	  {"type":"mediaSingle","content":[{"type":"media","attrs":{"alt":"shot.png"}}]}
	]}`
	want := "## Plan\n\n" +
		"@Sam Lee see [this](https://x.io) and **bold** `run()`\nnext line [DONE]\n\n" +
		"- one\n  - inner\n- two\n\n" +
		"```go\nx := 1\ny := 2\n```\n\n" +
		"> quoted\n\n" +
		"---\n\n" +
		"| A | B |\n|---|---|\n| 1\\|2 | 3 |\n\n" +
		"[attachment: shot.png]"
	if got := description(json.RawMessage(doc)); got != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
	}
	if got := description(json.RawMessage(`"plain text"`)); got != "plain text" {
		t.Errorf("a plain string: %q", got)
	}
}
