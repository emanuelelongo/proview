//go:build darwin

package gui

/*
#cgo darwin CFLAGS: -x objective-c
#cgo darwin LDFLAGS: -framework Foundation -framework AppKit -framework CoreServices
void proviewRegisterOpenDocumentsHandler(void);
*/
import "C"

import (
	"fyne.io/fyne/v2"
	"sync"
	"time"
	"unsafe"
)

const finderBatchDelay = 450 * time.Millisecond

type finderFileInbox struct {
	sync.Mutex
	callback       func([]string)
	pending        []string
	launchCallback func([]string)
	launchReady    bool
	timer          *time.Timer
	generation     uint64
}

var finderOpenHandler finderFileInbox

func (inbox *finderFileInbox) setCallback(callback func([]string)) {
	inbox.Lock()
	inbox.callback = callback
	if callback != nil && inbox.launchCallback == nil && len(inbox.pending) > 0 {
		inbox.scheduleLocked()
	}
	inbox.Unlock()
}

func (inbox *finderFileInbox) receive(files []string) {
	if len(files) == 0 {
		return
	}
	inbox.Lock()
	inbox.pending = append(inbox.pending, files...)
	if inbox.launchReady || inbox.launchCallback == nil && inbox.callback != nil {
		inbox.scheduleLocked()
	}
	inbox.Unlock()
}

func (inbox *finderFileInbox) scheduleLocked() {
	inbox.generation++
	generation := inbox.generation
	if inbox.timer != nil {
		inbox.timer.Stop()
	}
	inbox.timer = time.AfterFunc(finderBatchDelay, func() { inbox.deliver(generation) })
}

func (inbox *finderFileInbox) deliver(generation uint64) {
	inbox.Lock()
	if generation != inbox.generation {
		inbox.Unlock()
		return
	}
	callback := inbox.launchCallback
	if callback == nil {
		callback = inbox.callback
	}
	if callback == nil {
		inbox.Unlock()
		return
	}
	files := inbox.pending
	inbox.pending = nil
	inbox.timer = nil
	inbox.launchCallback = nil
	inbox.launchReady = false
	inbox.Unlock()
	// Apple events arrive on the native main thread. The callback uses
	// fyne.Do to enqueue work, so invoke it from a Go worker instead.
	go callback(files)
}

func (inbox *finderFileInbox) startLaunch(callback func([]string)) {
	inbox.Lock()
	inbox.launchCallback = callback
	inbox.launchReady = false
	inbox.Unlock()
}

func (inbox *finderFileInbox) finishLaunch() {
	inbox.Lock()
	if inbox.launchCallback == nil {
		inbox.Unlock()
		return
	}
	inbox.launchReady = true
	inbox.scheduleLocked()
	inbox.Unlock()
}

// Finder's launch documents arrive after the first native Show. Wait for
// AppKit's launch boundary before deciding whether the saved session applies.
func (g *Gui) deferSessionRestoreForFinder(paths []string) bool {
	if len(paths) != 0 {
		return false
	}
	finderOpenHandler.startLaunch(func(files []string) {
		fyne.Do(func() {
			if g.closed {
				return
			}
			if len(files) == 0 {
				files = g.launchPaths(nil)
			}
			g.openStartupFiles(files)
		})
	})
	g.prepareWindowOpening(nil)
	return true
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

//export goProviewDidFinishLaunching
func goProviewDidFinishLaunching() {
	finderOpenHandler.finishLaunch()
}
