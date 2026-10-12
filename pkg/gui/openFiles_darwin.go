//go:build darwin

package gui

/*
#cgo darwin CFLAGS: -x objective-c
#cgo darwin LDFLAGS: -framework Foundation -framework AppKit -framework CoreServices
void proviewRegisterOpenDocumentsHandler(void);
*/
import "C"

import (
	"sync"
	"unsafe"
)

type finderFileInbox struct {
	sync.Mutex
	callback func([]string)
	pending  []string
}

var finderOpenHandler finderFileInbox

func (inbox *finderFileInbox) setCallback(callback func([]string)) {
	inbox.Lock()
	inbox.callback = callback
	var pending []string
	if callback != nil {
		pending, inbox.pending = inbox.pending, nil
	}
	inbox.Unlock()
	if len(pending) > 0 {
		go callback(pending)
	}
}

func (inbox *finderFileInbox) receive(files []string) {
	if len(files) == 0 {
		return
	}
	inbox.Lock()
	callback := inbox.callback
	if callback == nil {
		inbox.pending = append(inbox.pending, files...)
	}
	inbox.Unlock()
	if callback != nil {
		// Apple events arrive on the native main thread. The callback uses
		// fyne.Do to enqueue work, so invoke it from a Go worker instead.
		go callback(files)
	}
}

func registerOpenFilesHandler(callback func([]string)) {
	finderOpenHandler.setCallback(callback)
	C.proviewRegisterOpenDocumentsHandler()
}

//export goProviewOpenFiles
func goProviewOpenFiles(paths **C.char, count C.int) {
	if paths == nil || count <= 0 {
		return
	}
	items := unsafe.Slice(paths, int(count))
	files := make([]string, 0, len(items))
	for _, path := range items {
		if path != nil {
			files = append(files, C.GoString(path))
		}
	}
	if len(files) == 0 {
		return
	}

	finderOpenHandler.receive(files)
}
