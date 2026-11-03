package focus

import (
	"context"
	"regexp"
	"runtime"
	"strconv"
	"time"

	"crew/internal/proc"
)

// hidIdle finds HIDIdleTime (nanoseconds since the last input) in ioreg's output.
var hidIdle = regexp.MustCompile(`"HIDIdleTime" = (\d+)`)

// ReadIdle is how long the Mac has had no keyboard or mouse input, read with ioreg (it waits for it);
// ok is false when it couldn't be read, or this isn't a Mac.
// ponytail: one short process every 10 seconds; an IOKit binding would avoid it if this ever shows up
// in Activity Monitor.
func ReadIdle() (idle time.Duration, ok bool) {
	if runtime.GOOS != "darwin" {
		return 0, false
	}
	out, err := proc.Command(context.Background(), "ioreg", "-c", "IOHIDSystem").Output()
	if err != nil {
		return 0, false
	}
	m := hidIdle.FindSubmatch(out)
	if m == nil {
		return 0, false
	}
	ns, err := strconv.ParseInt(string(m[1]), 10, 64)
	return time.Duration(ns), err == nil
}
