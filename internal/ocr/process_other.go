//go:build !windows

package ocr

import "os/exec"

func hideConsole(*exec.Cmd) {}
