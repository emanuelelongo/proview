package gui

import (
	"unsafe"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
	"golang.org/x/sys/windows"
)

func nativeWindowWorkArea(context any, scale float32) fyne.Size {
	window, ok := context.(driver.WindowsWindowContext)
	if !ok {
		return fyne.Size{}
	}
	user32 := windows.NewLazySystemDLL("user32.dll")
	if window.HWND == 0 {
		dpi := uintptr(96)
		if proc := user32.NewProc("GetDpiForSystem"); proc.Find() == nil {
			dpi, _, _ = proc.Call()
		}
		scale = initialWindowScale(float32(dpi) / 96)
	}
	monitor, _, _ := user32.NewProc("MonitorFromWindow").Call(window.HWND, 2)
	type rect struct{ left, top, right, bottom int32 }
	info := struct {
		size          uint32
		monitor, work rect
		flags         uint32
	}{}
	info.size = uint32(unsafe.Sizeof(info))
	result, _, _ := user32.NewProc("GetMonitorInfoW").Call(monitor, uintptr(unsafe.Pointer(&info)))
	if result == 0 {
		return fyne.Size{}
	}
	return fyne.NewSize(float32(info.work.right-info.work.left)/max(.1, scale), float32(info.work.bottom-info.work.top)/max(.1, scale))
}
