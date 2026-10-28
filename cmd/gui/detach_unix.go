//go:build !windows

package main

import (
	"os"
	"os/exec"
	"runtime"
	"syscall"
)

func configureDetached(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// Launch Services delivers Finder Apple Events to the original application PID.
// Applications started by launchd are already independent of a terminal.
func shouldDetach() bool { return runtime.GOOS != "darwin" || os.Getppid() != 1 }
