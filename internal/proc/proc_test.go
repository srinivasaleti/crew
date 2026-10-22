package proc

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestNoTerminal(t *testing.T) {
	out, _ := Command(context.Background(), "sh", "-c", "exec 3</dev/tty && echo opened").CombinedOutput()
	if strings.Contains(string(out), "opened") {
		t.Fatal("a background command could open crew's terminal")
	}
}

func TestStopKillsChildren(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	Command(ctx, "sh", "-c", "sleep 30 & sleep 30").Run()
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("stopping took %v: a child kept it waiting", d)
	}
}
