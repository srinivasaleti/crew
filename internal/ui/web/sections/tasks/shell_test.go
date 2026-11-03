package tasks

import (
	"bytes"
	"testing"
	"time"
)

// A shell runs in its folder, takes what's typed, and its output reaches a reader.
func TestShell(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	dir := t.TempDir()
	s, err := startShell("t1", dir)
	if err != nil {
		t.Fatal(err)
	}
	ch := make(chan []byte, 256)
	s.mu.Lock()
	s.subs[ch] = true
	s.mu.Unlock()
	s.pty.Write([]byte("pwd; echo crew-$((40+2))\n"))
	var got []byte
	for deadline := time.After(5 * time.Second); !bytes.Contains(got, []byte("crew-42")); {
		select {
		case b := <-ch:
			got = append(got, b...)
		case <-deadline:
			t.Fatalf("no output; got %q", got)
		}
	}
	if !bytes.Contains(got, []byte(dir[len(dir)-8:])) {
		t.Errorf("not in its folder; got %q", got)
	}
	// Hanging up ends it: it leaves shells and its readers hear so.
	shells.Lock()
	shells.m["t1"] = s
	shells.Unlock()
	s.hangUp()
	if running("t1") != nil { // a page asks for the running shells right after closing one
		t.Fatal("hangUp returned before the shell left shells")
	}
	for deadline := time.After(5 * time.Second); ; {
		select {
		case _, open := <-ch:
			if open {
				continue
			}
			if running("t1") != nil {
				t.Error("still listed after it ended")
			}
			return
		case <-deadline:
			t.Fatal("the shell didn't end on hang-up")
		}
	}
}
