// Package agent runs coding-agent CLIs headless, one turn at a time, and streams their output.
package agent

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type Kind int

const (
	KindText  Kind = iota // assistant prose
	KindTool              // one-line summary of a tool call
	KindDone              // turn finished normally
	KindError             // turn failed
)

type Event struct {
	Kind Kind
	Text string
}

// Agent describes how to drive one CLI.
type Agent struct {
	Name    string
	Command string
	// Args builds argv for one turn; the prompt is sent on stdin.
	// resume is false on a session's first turn; an empty model means the CLI's default.
	Args  func(session string, resume bool, model string) []string
	Parse func(line []byte) []Event
}

// ponytail: "auto" lets Claude's classifier approve tool calls since nobody can answer prompts headless.
// Make it per-task if some tasks need "plan" or "acceptEdits".
var Claude = Agent{
	Name:    "claude",
	Command: "claude",
	Args: func(session string, resume bool, model string) []string {
		a := []string{"-p", "--output-format", "stream-json", "--verbose", "--permission-mode", "auto"}
		if model != "" {
			a = append(a, "--model", model)
		}
		if resume {
			return append(a, "--resume", session)
		}
		return append(a, "--session-id", session)
	},
	Parse: parseClaude,
}

// All lists the agents a task can use. Add more CLIs (codex, …) here with their own Args/Parse.
var All = []Agent{Claude}

func Get(name string) (Agent, bool) {
	for _, a := range All {
		if a.Name == name {
			return a, true
		}
	}
	return Agent{}, false
}

// NewSessionID returns a random UUID v4.
func NewSessionID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// Run executes one turn in dir. The channel closes when the process exits;
// the last event is always KindDone or KindError.
func (a Agent) Run(ctx context.Context, dir, session string, resume bool, model, prompt string) <-chan Event {
	ch := make(chan Event)
	send := func(ev Event) bool {
		select {
		case ch <- ev:
			return true
		case <-ctx.Done():
			return false
		}
	}
	go func() {
		defer close(ch)
		cmd := exec.CommandContext(ctx, a.Command, a.Args(session, resume, model)...)
		cmd.Dir = dir
		cmd.Stdin = strings.NewReader(prompt)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.StdoutPipe()
		if err == nil {
			err = cmd.Start()
		}
		if err != nil {
			send(Event{KindError, err.Error()})
			return
		}
		ended := false
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 0, 1<<20), 64<<20) // stream-json lines can be large
		for sc.Scan() {
			for _, ev := range a.Parse(sc.Bytes()) {
				ended = ended || ev.Kind == KindDone || ev.Kind == KindError
				if !send(ev) {
					return
				}
			}
		}
		err = cmd.Wait()
		switch {
		case ended:
		case err != nil:
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = err.Error()
			}
			send(Event{KindError, msg})
		default:
			send(Event{Kind: KindDone})
		}
	}()
	return ch
}

func parseClaude(line []byte) []Event {
	var m struct {
		Type    string
		Subtype string
		IsError bool `json:"is_error"`
		Result  string
		Message struct {
			Content []struct {
				Type, Text, Name string
				Input            map[string]any
			}
		}
	}
	if json.Unmarshal(line, &m) != nil {
		return nil
	}
	switch m.Type {
	case "assistant":
		var evs []Event
		for _, c := range m.Message.Content {
			switch c.Type {
			case "text":
				if t := strings.TrimSpace(c.Text); t != "" {
					evs = append(evs, Event{KindText, t})
				}
			case "tool_use":
				evs = append(evs, Event{KindTool, toolSummary(c.Name, c.Input)})
			}
		}
		return evs
	case "result":
		if m.IsError || m.Subtype != "success" {
			msg := m.Result
			if msg == "" {
				msg = m.Subtype
			}
			return []Event{{KindError, msg}}
		}
		return []Event{{Kind: KindDone}}
	}
	return nil
}

// toolSummary turns a tool call into "Bash: go test ./..." style one-liners.
func toolSummary(name string, input map[string]any) string {
	for _, k := range []string{"command", "file_path", "pattern", "path", "url", "description", "prompt"} {
		if v, ok := input[k].(string); ok && v != "" {
			v, _, _ = strings.Cut(v, "\n")
			if len(v) > 100 {
				v = v[:100] + "…"
			}
			return name + ": " + v
		}
	}
	return name
}
