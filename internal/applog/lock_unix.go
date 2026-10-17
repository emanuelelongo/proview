//go:build !windows

package applog

import (
	"golang.org/x/sys/unix"
	"os"
)

func lockFile(file *os.File) error { return unix.Flock(int(file.Fd()), unix.LOCK_EX) }
func unlock(file *os.File)         { _ = unix.Flock(int(file.Fd()), unix.LOCK_UN) }
