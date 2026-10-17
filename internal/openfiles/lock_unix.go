//go:build !windows

package openfiles

import (
	"golang.org/x/sys/unix"
	"os"
)

func tryLock(file *os.File) error { return unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB) }
func unlock(file *os.File)        { _ = unix.Flock(int(file.Fd()), unix.LOCK_UN) }
