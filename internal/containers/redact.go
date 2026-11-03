package containers

import (
	"regexp"
	"strings"
)

// secretish matches the names of values that shouldn't be shown: env vars and keys like
// DB_PASSWORD, API_TOKEN, aws_secret_access_key.
var secretish = regexp.MustCompile(`(?i)(pass|secret|token|key|auth|credential|private|cookie|session)`)

// envLine is NAME=value (docker inspect's Env) or NAME: value (kubectl describe), with the quote
// and indent around it.
var envLine = regexp.MustCompile(`^(\s*"?)([A-Za-z_][A-Za-z0-9_.-]*)(=|:\s+)(.+?)("?,?)$`)

// Redact hides the values of secret-looking variables in inspect and describe output.
func Redact(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if m := envLine.FindStringSubmatch(l); m != nil && secretish.MatchString(m[2]) && !strings.HasPrefix(m[4], "<set to the key") {
			lines[i] = m[1] + m[2] + m[3] + "•••" + m[5]
		}
	}
	return strings.Join(lines, "\n")
}
