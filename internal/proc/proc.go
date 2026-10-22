// Package proc starts the commands crew runs in the background (git, gh, acli, the agents, docker,
// kubectl) the one safe way.
//
// crew's terminal is the app's window, and a command started from crew would otherwise share it as
// its controlling terminal. A child that reads /dev/tty (a kubectl sign-in plugin, an ssh or git
// credential prompt) would then take the keys you type into crew, and would keep the window open
// after crew quits, since the app closes it only when nothing holds the terminal. So each command
// runs in its own session with no terminal, and a prompt there fails with its message instead.
// Interactive programs (a shell, a sign-in you take part in) run in crew's shell panel instead.
package proc

import (
	"context"
	"os/exec"
	"syscall"
	"time"
)

// Command is exec.CommandContext for a background command: its own session, no terminal. When ctx
// ends the whole group is killed, children included (Interrupt changes how), and Wait gives up on
// output a leftover child keeps open after two seconds.
func Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error { return group(cmd, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
	return cmd
}

// Interrupt makes stopping cmd a Ctrl+C to its whole group, so an agent can save its session, with a
// kill after grace if it lingers.
func Interrupt(cmd *exec.Cmd, grace time.Duration) {
	cmd.Cancel = func() error { return group(cmd, syscall.SIGINT) }
	cmd.WaitDelay = grace
}

// group signals cmd's process group (its session, which Command made it lead).
func group(cmd *exec.Cmd, sig syscall.Signal) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, sig)
}
