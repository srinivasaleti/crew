// Package chat persists each task's conversation as JSON lines.
package chat

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Roles.
const (
	You   = "you"
	Agent = "agent"
	Tool  = "tool"
	Error = "error"
)

type Message struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// Log stores one <id>.jsonl file per task in Dir.
type Log struct{ Dir string }

func (l Log) path(id string) string { return filepath.Join(l.Dir, id+".jsonl") }

func (l Log) Append(id string, m Message) error {
	if err := os.MkdirAll(l.Dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(l.path(id), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(m)
}

func (l Log) Load(id string) ([]Message, error) {
	f, err := os.Open(l.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var msgs []Message
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 16<<20)
	for sc.Scan() {
		var m Message
		if json.Unmarshal(sc.Bytes(), &m) == nil {
			msgs = append(msgs, m)
		}
	}
	return msgs, sc.Err()
}

func (l Log) Delete(id string) error {
	if err := os.Remove(l.path(id)); !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
