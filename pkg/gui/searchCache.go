package gui

import (
	"emanuelelongo/proview/internal/textselect"
	"sync"
)

const searchTextCacheBytes = 16 << 20
const searchTextCachePages = 128

type searchTextEntry struct {
	layout textselect.Layout
	bytes  int
	used   uint64
}

// Shared by successive query workers. Retain immutable layouts, not query
// results; source keys protect both canceled work and sanitation changes.
type searchTextCache struct {
	mu      sync.Mutex
	entries map[string]searchTextEntry
	bytes   int
	clock   uint64
}

func (c *searchTextCache) get(key string) (textselect.Layout, bool) {
	if c == nil {
		return textselect.Layout{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if ok {
		c.clock++
		entry.used = c.clock
		c.entries[key] = entry
	}
	return entry.layout, ok
}

func (c *searchTextCache) put(key string, layout textselect.Layout) {
	if c == nil {
		return
	}
	// Conservative accounting includes geometry, string headers and map overhead.
	size := 256 + len(key) + cap(layout.Words)*160
	for _, word := range layout.Words {
		size += len(word.Text) + cap(word.Characters)*80
	}
	if size > searchTextCacheBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]searchTextEntry{}
	}
	c.bytes -= c.entries[key].bytes
	c.clock++
	c.entries[key] = searchTextEntry{layout: layout, bytes: size, used: c.clock}
	c.bytes += size
	for c.bytes > searchTextCacheBytes || len(c.entries) > searchTextCachePages {
		oldest, selected := ^uint64(0), ""
		for key, entry := range c.entries {
			if entry.used < oldest {
				selected, oldest = key, entry.used
			}
		}
		c.bytes -= c.entries[selected].bytes
		delete(c.entries, selected)
	}
}
