package ui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"crew/internal/gh"
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

// cleanMD turns a GitHub comment into Markdown a terminal can render.
func cleanMD(s string) string {
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

// showMD is cleanMD for reading: <details> blocks (bots' long prompts and configs) fold to their
// title. Claude still gets them in full.
func showMD(s string) string {
	s = mdHTMLComment.ReplaceAllString(s, "")
	s = mdDetails.ReplaceAllStringFunc(s, func(d string) string {
		title := strings.TrimSpace(mdAnyTag.ReplaceAllString(mdDetails.FindStringSubmatch(d)[1], ""))
		return "\n*▸ " + title + " (folded · o shows it on GitHub)*\n"
	})
	return cleanMD(s)
}

// firstLine is the start of a comment for its row: the first line with words in it.
func firstLine(s string) string {
	for _, l := range strings.Split(showMD(s), "\n") {
		l = strings.TrimSpace(strings.TrimLeft(l, ">#*-_ \t"))
		if l = strings.ReplaceAll(l, "**", ""); l != "" {
			return l
		}
	}
	return ""
}

func quote(c gh.Comment) string {
	return "> @" + c.Author + ": " + strings.ReplaceAll(slimMD(c.Body), "\n", "\n> ")
}

// slimMD is cleanMD for what goes to Claude (and into quoting replies): <details> blocks, where bots
// keep configs, summaries and a "Prompt for AI agents" copy of the comment, are left out to save
// tokens.
func slimMD(s string) string {
	return cleanMD(mdDetails.ReplaceAllString(mdHTMLComment.ReplaceAllString(s, ""), ""))
}

// markdown renders an agent reply, caching by text and width since the chat redraws often.
func (m *Model) markdown(text string, w int) string {
	key := fmt.Sprintf("%d\x00%s", w, text)
	if out, ok := m.mdCache[key]; ok {
		return out
	}
	if m.md == nil || m.mdWidth != w {
		m.md, _ = glamour.NewTermRenderer(glamour.WithStandardStyle("dark"), glamour.WithWordWrap(max(w-4, 20)))
		m.mdWidth = w
	}
	out := wrap(text, w)
	if m.md != nil {
		if r, err := m.md.Render(text); err == nil {
			var lines []string
			for _, l := range strings.Split(strings.Trim(r, "\n"), "\n") {
				lines = append(lines, ansi.Truncate(strings.TrimRight(l, " "), w, "…"))
			}
			out = strings.Join(lines, "\n")
		}
	}
	m.mdCache[key] = out
	return out
}

func wrap(s string, w int) string { return lipgloss.NewStyle().Width(max(w, 10)).Render(s) }
