// Package markdown draws Markdown in the terminal: agent replies and GitHub comments, which carry
// HTML a terminal can't show, cleaned up first.
package markdown

import (
	"crew/internal/widgets/look"
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/x/ansi"
)

// Markdown clean-up: GitHub comments (bots' especially) carry HTML that a terminal can't show.
var (
	mdHTMLComment = regexp.MustCompile(`(?s)<!--.*?-->`)
	mdImage       = regexp.MustCompile(`(?is)<img[^>]*>`)
	mdLink        = regexp.MustCompile(`(?is)<a\s[^>]*href="([^"]*)"[^>]*>(.*?)</a>`)
	mdSummary     = regexp.MustCompile(`(?is)<summary>(.*?)</summary>`)
	mdBreak       = regexp.MustCompile(`(?i)<br\s*/?>`)
	mdTag         = regexp.MustCompile(`(?i)</?(details|summary|p|div|sub|sup|b|strong|i|em|code|kbd|span|blockquote|table|thead|tbody|tr|td|th|picture|source)(\s[^>]*)?>`)
	mdAnyTag      = regexp.MustCompile(`<[^>]+>`)
	mdBotTag      = regexp.MustCompile(`(?m)^([ \t]*)</?[a-z][a-z _-]{0,30}>`) // e.g. <comment>, <file context>
	mdDetails     = regexp.MustCompile(`(?is)<details>\s*<summary>(.*?)</summary>.*?</details>`)
	mdAlert       = regexp.MustCompile(`(?m)^(>\s*)\[!(\w+)\]`)
	mdEmptyQuotes = regexp.MustCompile(`(?m)(^>[ \t]*\n){2,}`)
	mdBlankLines  = regexp.MustCompile(`\n{3,}`)
)

// Clean turns a GitHub comment into Markdown a terminal can render.
func Clean(s string) string {
	s = mdHTMLComment.ReplaceAllString(s, "")
	s = mdImage.ReplaceAllString(s, "")
	s = mdLink.ReplaceAllStringFunc(s, func(a string) string {
		g := mdLink.FindStringSubmatch(a)
		if text := strings.TrimSpace(mdAnyTag.ReplaceAllString(g[2], "")); text != "" {
			return "[" + text + "](" + g[1] + ")"
		}
		return ""
	})
	s = mdSummary.ReplaceAllString(s, "**$1**\n")
	s = mdBotTag.ReplaceAllString(s, "$1")
	s = mdBreak.ReplaceAllString(s, "\n")
	s = mdTag.ReplaceAllString(s, "")
	s = mdAlert.ReplaceAllStringFunc(s, func(a string) string {
		g := mdAlert.FindStringSubmatch(a)
		return g[1] + "**" + strings.ToUpper(g[2][:1]) + strings.ToLower(g[2][1:]) + "**"
	})
	s = mdEmptyQuotes.ReplaceAllString(s, ">\n")
	s = mdBlankLines.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// Show is Clean for reading: <details> blocks (bots' long prompts and configs) fold to their
// title. Claude still gets them in full.
func Show(s string) string {
	s = mdHTMLComment.ReplaceAllString(s, "")
	s = mdDetails.ReplaceAllStringFunc(s, func(d string) string {
		title := strings.TrimSpace(mdAnyTag.ReplaceAllString(mdDetails.FindStringSubmatch(d)[1], ""))
		return "\n*▸ " + title + " (folded · o shows it on GitHub)*\n"
	})
	return Clean(s)
}

// FirstLine is the start of a comment for its row: the first line with words in it.
func FirstLine(s string) string {
	for _, l := range strings.Split(Show(s), "\n") {
		l = strings.TrimSpace(strings.TrimLeft(l, ">#*-_ \t"))
		if l = strings.ReplaceAll(l, "**", ""); l != "" {
			return l
		}
	}
	return ""
}

// Slim is Clean for what goes to Claude (and into quoting replies): <details> blocks, where bots
// keep configs, summaries and a "Prompt for AI agents" copy of the comment, are left out to save
// tokens.
func Slim(s string) string {
	return Clean(mdDetails.ReplaceAllString(mdHTMLComment.ReplaceAllString(s, ""), ""))
}

// Renderer draws agent replies, caching by text and width since the chat redraws often.
type Renderer struct {
	r     *glamour.TermRenderer
	width int
	cache map[string]string
}

// Render draws text w wide.
func (md *Renderer) Render(text string, w int) string {
	key := fmt.Sprintf("%d\x00%s", w, text)
	if out, ok := md.cache[key]; ok {
		return out
	}
	if md.cache == nil {
		md.cache = map[string]string{}
	}
	if md.r == nil || md.width != w {
		md.r, _ = glamour.NewTermRenderer(glamour.WithStandardStyle("dark"), glamour.WithWordWrap(max(w-4, 20)))
		md.width = w
	}
	out := look.Wrap(text, w)
	if md.r != nil {
		if r, err := md.r.Render(text); err == nil {
			var lines []string
			for _, l := range strings.Split(strings.Trim(r, "\n"), "\n") {
				lines = append(lines, ansi.Truncate(strings.TrimRight(l, " "), w, "…"))
			}
			out = strings.Join(lines, "\n")
		}
	}
	md.cache[key] = out
	return out
}
