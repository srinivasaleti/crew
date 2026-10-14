package app

import (
	"strings"

	"crew/internal/gh"
	"crew/internal/widgets/look"
	"crew/internal/widgets/markdown"
)

func quote(c gh.Comment) string {
	return "> @" + c.Author + ": " + strings.ReplaceAll(markdown.Slim(c.Body), "\n", "\n> ")
}

var wrap = look.Wrap
