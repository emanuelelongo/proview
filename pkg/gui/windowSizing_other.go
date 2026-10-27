//go:build wayland || (!darwin && !windows && !linux && !freebsd && !openbsd && !netbsd)

package gui

import "fyne.io/fyne/v2"

func nativeWindowWorkArea(any, float32) fyne.Size { return fyne.Size{} }
