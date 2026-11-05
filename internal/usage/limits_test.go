package usage

import (
	"strconv"
	"testing"
	"time"
)

// A rate_limit_event's windows are kept and read back; a line without them changes nothing.
func TestLimits(t *testing.T) {
	t.Setenv("CREW_HOME", t.TempDir())
	future := time.Now().Add(time.Hour).Unix()
	SaveLimits([]byte(`{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","unifiedWindows":{` +
		`"five_hour":{"utilization":0.42,"resetsAt":` + strconv.FormatInt(future, 10) + `},"seven_day":{"utilization":0.18,"resetsAt":1}}}}`))
	SaveLimits([]byte(`{"type":"rate_limit_event","rate_limit_info":{"status":"allowed"}}`))
	l := LoadLimits()
	if got := l.String(); got != "session 42%" { // the week's reset (1) has passed, so it's dropped
		t.Fatalf("got %q", got)
	}
}
