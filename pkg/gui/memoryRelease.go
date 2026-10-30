package gui

import (
	"runtime/debug"
	"sync"
	"time"
)

// FreeOSMemory performs a full collection and scavenge, so it must not run on
// every eviction or page gesture. Coalesce requests into one delayed release.
const memoryReleaseDelay = 750 * time.Millisecond

var memoryRelease struct {
	mu        sync.Mutex
	scheduled bool
}

// requestMemoryRelease schedules at most one release. Repeated requests while a
// release is pending or running are ignored; the next request after completion
// schedules a fresh one.
func requestMemoryRelease() {
	memoryRelease.mu.Lock()
	if memoryRelease.scheduled {
		memoryRelease.mu.Unlock()
		return
	}
	memoryRelease.scheduled = true
	memoryRelease.mu.Unlock()
	time.AfterFunc(memoryReleaseDelay, func() {
		defer func() {
			memoryRelease.mu.Lock()
			memoryRelease.scheduled = false
			memoryRelease.mu.Unlock()
		}()
		debug.FreeOSMemory()
	})
}
