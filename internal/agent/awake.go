package agent

import (
	"context"
	"os"
	"strconv"
	"sync"

	"crew/internal/proc"
)

// awake keeps the Mac from idle-sleeping while any agent turn runs, so work goes on behind a locked
// screen. caffeinate -i blocks only system sleep (the display still turns off and locks), and -w
// ends it with crew if crew dies without releasing it.
var awake struct {
	sync.Mutex
	n    int
	stop context.CancelFunc
}

// holdAwake counts a running turn in; the returned func counts it out. The first in starts
// caffeinate and the last out stops it.
func holdAwake() (release func()) {
	awake.Lock()
	defer awake.Unlock()
	if awake.n++; awake.n == 1 {
		ctx, stop := context.WithCancel(context.Background())
		cmd := proc.Command(ctx, "caffeinate", "-i", "-w", strconv.Itoa(os.Getpid()))
		if cmd.Start() == nil {
			go cmd.Wait() // reap it once stopped
			awake.stop = stop
		} else {
			stop() // ponytail: no caffeinate (not a Mac); turns still run, the Mac may sleep
		}
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			awake.Lock()
			defer awake.Unlock()
			if awake.n--; awake.n == 0 && awake.stop != nil {
				awake.stop()
				awake.stop = nil
			}
		})
	}
}
