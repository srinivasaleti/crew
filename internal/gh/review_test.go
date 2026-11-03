package gh

import (
	"strings"
	"testing"
)

// A review with no line comments leaves the field out: GitHub refuses "comments": null.
func TestReviewPayload(t *testing.T) {
	if p := string(reviewPayload("APPROVE", "LGTM", nil)); strings.Contains(p, "comments") || !strings.Contains(p, `"event":"APPROVE"`) {
		t.Errorf("an approval: %s", p)
	}
	p := string(reviewPayload("COMMENT", "", []ReviewComment{{Path: "a.go", Line: 3, Side: "RIGHT", Body: "hm"}}))
	if !strings.Contains(p, `"comments":[{`) || !strings.Contains(p, `"path":"a.go"`) {
		t.Errorf("a line comment: %s", p)
	}
}
