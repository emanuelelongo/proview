package gui

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit
#import <AppKit/AppKit.h>
#import <dispatch/dispatch.h>
#include <stdint.h>

static void proviewWindowWorkArea(uintptr_t handle, double *width, double *height) {
    void (^readArea)(void) = ^{
        NSWindow *window = (__bridge NSWindow *)(void *)handle;
        NSScreen *screen = window.screen ?: NSScreen.mainScreen;
        *width = screen.visibleFrame.size.width;
        *height = screen.visibleFrame.size.height;
    };
    if ([NSThread isMainThread]) readArea();
    else dispatch_sync(dispatch_get_main_queue(), readArea);
}
*/
import "C"

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
)

func nativeWindowWorkArea(context any, scale float32) fyne.Size {
	window, ok := context.(driver.MacWindowContext)
	if !ok {
		return fyne.Size{}
	}
	var width, height C.double
	C.proviewWindowWorkArea(C.uintptr_t(window.NSWindow), &width, &height)
	if window.NSWindow == 0 {
		scale = initialWindowScale(1)
	}
	return fyne.NewSize(float32(width)/max(.1, scale), float32(height)/max(.1, scale))
}
