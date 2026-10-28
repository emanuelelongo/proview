package main

import (
	"os"
	"os/exec"
)

func launchDetached(args []string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	command := exec.Command(executable, args...)
	command.Env = append(os.Environ(), "PROVIEW_DETACHED_CHILD=1")
	configureDetached(command)
	// Nil standard streams connect to the null device, never the caller's TTY.
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}
